package oasfgenerator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// componentSchema defines the configuration schema
var componentSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Component implements the OASF generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	mapper     *Mapper
	generator  *Generator
	metrics    *Metrics

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// ENTITY_STATES has two distinct watch responsibilities. contractWatcher is
	// always graph-wide and owns fail-closed validation; kvWatcher applies the
	// configured pattern only to selecting entities for OASF generation.
	contractWatcher             jetstream.KeyWatcher
	kvWatcher                   jetstream.KeyWatcher
	graphStatePoison            atomic.Pointer[graph.StateContractError]
	entityWatchLost             atomic.Bool
	bootstrapStarted            atomic.Bool
	bootstrapComplete           atomic.Bool
	outputMu                    sync.RWMutex
	contractRevision            atomic.Uint64
	guardProgressMu             sync.Mutex
	guardProgressCh             chan struct{}
	selectionRevisionComparable bool
	// Test seams are nil in production. They expose the selection barrier and
	// final queue boundary without weakening either contract.
	beforeSelectionBarrier func(uint64)
	queueGeneration        func(string)

	// Metrics tracking
	recordsGenerated int64
	errors           int64
	lastActivity     time.Time

	// Context for background operations
	ctx    context.Context
	cancel context.CancelFunc
}

// NewComponent creates a new OASF generator component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}

	// Create mapper with config values
	mapper := NewMapper(
		config.DefaultAgentVersion,
		config.DefaultAuthors,
		config.IncludeExtensions,
	)

	return &Component{
		name:       "oasf-generator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		mapper:     mapper,
		metrics:    newMetrics(deps.MetricsRegistry),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	// Create generator (depends on mapper and NATS client)
	c.generator = NewGenerator(c.mapper, c.natsClient, c.config, c.logger)
	c.generator.readiness = c.outputReadinessError
	c.generator.beginOutput = c.beginOutput
	return nil
}

// Start begins watching for entity changes and generating OASF records.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "check running state")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}

	// Create cancellable context for background operations
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initialize generator (sets up KV stores)
	if err := c.generator.Initialize(c.ctx); err != nil {
		c.cancel()
		return errs.Wrap(err, "Component", "Start", "initialize generator")
	}

	// Start the graph-wide contract guard and configured selection watcher.
	if err := c.startKVWatcher(c.ctx); err != nil {
		c.cancel()
		return errs.Wrap(err, "Component", "Start", "start KV watcher")
	}

	c.running = true
	c.startTime = time.Now()

	c.logger.Info("OASF generator started",
		slog.String("entity_kv_bucket", c.config.EntityKVBucket),
		slog.String("oasf_kv_bucket", c.config.OASFKVBucket),
		slog.String("watch_pattern", c.config.WatchPattern))

	return nil
}

// startKVWatcher starts watching the entity KV bucket for changes.
func (c *Component) startKVWatcher(ctx context.Context) error {
	// Get the configured selection bucket. ENTITY_STATES remains the
	// authoritative graph-wide contract source even when a deployment selects
	// generation work from a legacy/custom bucket.
	selectionKV, err := c.natsClient.GetKeyValueBucket(ctx, c.config.EntityKVBucket)
	if err != nil {
		return errs.Wrap(err, "Component", "startKVWatcher", "get entity KV bucket")
	}
	contractKV := selectionKV
	if c.config.EntityKVBucket != graph.BucketEntityStates {
		contractKV, err = c.natsClient.GetKeyValueBucket(ctx, graph.BucketEntityStates)
		if err != nil {
			return errs.Wrap(err, "Component", "startKVWatcher", "get authoritative ENTITY_STATES bucket")
		}
	}

	return c.startEntityWatches(ctx, contractKV, selectionKV,
		c.config.EntityKVBucket == graph.BucketEntityStates)
}

// startEntityWatches first drains a graph-wide WatchAll snapshot through the
// canonical decoder, then leaves that same watcher live. Only after a clean
// sentinel does it start the configured pattern watcher that selects work.
// This keeps validation independent of selection without buffering entity IDs.
func (c *Component) startEntityWatches(
	ctx context.Context,
	contractKV, selectionKV jetstream.KeyValue,
	revisionsComparable bool,
) error {
	c.bootstrapStarted.Store(true)
	c.selectionRevisionComparable = revisionsComparable
	c.guardProgressMu.Lock()
	if c.guardProgressCh == nil {
		c.guardProgressCh = make(chan struct{})
	}
	c.guardProgressMu.Unlock()
	watcher, err := contractKV.WatchAll(ctx)
	if err != nil {
		c.markEntityWatchLost()
		return nil
	}
	c.contractWatcher = watcher

	if err := c.drainContractBootstrap(ctx, watcher); err != nil {
		_ = watcher.Stop()
		c.contractWatcher = nil
		if ctx.Err() != nil {
			return err
		}
		return nil
	}
	go c.contractWatchLoop(ctx, watcher)

	// A poisoned authoritative snapshot remains watched, but no selector is
	// needed: output is process-lifetime blocked until reset and restart.
	if c.graphStatePoison.Load() != nil {
		return nil
	}
	if !revisionsComparable {
		c.logger.Warn("OASF selection bucket differs from authoritative ENTITY_STATES; using bootstrap-only contract gate because revisions are incomparable",
			slog.String("selection_bucket", selectionKV.Bucket()),
			slog.String("contract_bucket", contractKV.Bucket()))
	}

	// Create watcher with pattern.
	pattern := c.config.WatchPattern
	if pattern == "" {
		pattern = ">"
	}

	selectionWatcher, err := selectionKV.Watch(ctx, pattern, jetstream.IgnoreDeletes())
	if err != nil {
		return errs.Wrap(err, "Component", "startKVWatcher", "create KV watcher")
	}
	c.kvWatcher = selectionWatcher

	// Start background goroutine to process updates
	go c.watchLoop(ctx, selectionWatcher)

	return nil
}

func (c *Component) drainContractBootstrap(ctx context.Context, watcher jetstream.KeyWatcher) error {
	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			return errs.Wrap(ctx.Err(), "Component", "drainContractBootstrap", "context cancelled")
		case entry, ok := <-updates:
			if !ok {
				c.markEntityWatchLost()
				return errs.Wrap(errors.New("ENTITY_STATES contract watcher closed during bootstrap"),
					"Component", "drainContractBootstrap", "watch transport")
			}
			if entry == nil {
				c.bootstrapComplete.Store(true)
				return nil
			}
			c.observeContractEntry(entry)
		}
	}
}

func (c *Component) contractWatchLoop(ctx context.Context, watcher jetstream.KeyWatcher) {
	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-updates:
			if !ok {
				if ctx.Err() == nil {
					c.markEntityWatchLost()
				}
				return
			}
			if entry != nil {
				c.observeContractEntry(entry)
			}
		}
	}
}

func (c *Component) observeContractEntry(entry jetstream.KeyValueEntry) {
	c.validateContractEntry(entry)
	for {
		observed := c.contractRevision.Load()
		if entry.Revision() <= observed || c.contractRevision.CompareAndSwap(observed, entry.Revision()) {
			break
		}
	}
	c.signalGuardProgress()
}

func (c *Component) validateContractEntry(entry jetstream.KeyValueEntry) {
	if graph.IsKVTombstone(entry.Operation()) {
		return
	}
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var contractErr *graph.StateContractError
		if !errors.As(err, &contractErr) {
			contractErr = &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity, Err: err}
		}
		c.outputMu.Lock()
		latched := c.graphStatePoison.CompareAndSwap(nil, contractErr)
		c.outputMu.Unlock()
		if latched {
			c.logger.Error("authoritative graph state requires reset; OASF output blocked",
				slog.String("code", graph.ErrorCodeGraphStateResetRequired),
				slog.String("reason", string(contractErr.Reason)))
		}
	}
}

func (c *Component) beginOutput() (func(), error) {
	c.outputMu.RLock()
	if err := c.outputReadinessError(); err != nil {
		c.outputMu.RUnlock()
		return nil, err
	}
	return c.outputMu.RUnlock, nil
}

func (c *Component) markEntityWatchLost() {
	c.outputMu.Lock()
	c.entityWatchLost.Store(true)
	c.outputMu.Unlock()
	c.signalGuardProgress()
}

func (c *Component) signalGuardProgress() {
	c.guardProgressMu.Lock()
	if c.guardProgressCh != nil {
		close(c.guardProgressCh)
	}
	c.guardProgressCh = make(chan struct{})
	c.guardProgressMu.Unlock()
}

func (c *Component) guardProgress() <-chan struct{} {
	c.guardProgressMu.Lock()
	if c.guardProgressCh == nil {
		c.guardProgressCh = make(chan struct{})
	}
	progress := c.guardProgressCh
	c.guardProgressMu.Unlock()
	return progress
}

// waitForContractRevision closes the same-bucket selection/validation race.
// Custom selection buckets deliberately receive bootstrap-only semantics: their
// revision space cannot be compared with authoritative ENTITY_STATES.
func (c *Component) waitForContractRevision(ctx context.Context, revision uint64) error {
	if !c.selectionRevisionComparable {
		return c.outputReadinessError()
	}
	if c.beforeSelectionBarrier != nil {
		c.beforeSelectionBarrier(revision)
	}
	for {
		if err := c.outputReadinessError(); err != nil {
			return err
		}
		if c.contractRevision.Load() >= revision {
			return nil
		}
		progress := c.guardProgress()
		if c.contractRevision.Load() >= revision {
			return nil
		}
		select {
		case <-ctx.Done():
			return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady, ctx.Err())
		case <-progress:
		}
	}
}

func (c *Component) outputReadinessError() error {
	if contractErr := c.graphStatePoison.Load(); contractErr != nil {
		return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired, contractErr)
	}
	if c.entityWatchLost.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("OASF output not ready: ENTITY_STATES contract watcher unavailable"))
	}
	if c.bootstrapStarted.Load() && !c.bootstrapComplete.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("OASF output not ready: ENTITY_STATES bootstrap validating"))
	}
	return nil
}

// watchLoop processes KV updates in a background goroutine.

func (c *Component) watchLoop(ctx context.Context, watcher jetstream.KeyWatcher) {
	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-updates:
			if !ok {
				if ctx.Err() == nil {
					c.markEntityWatchLost()
				}
				return
			}
			if entry == nil {
				// Initial values complete
				continue
			}

			c.handleEntityChange(ctx, entry)
		}
	}
}

// handleEntityChange processes a single entity change from KV.
func (c *Component) handleEntityChange(ctx context.Context, entry jetstream.KeyValueEntry) {
	if err := c.waitForContractRevision(ctx, entry.Revision()); err != nil {
		return
	}
	// Decode at the selection seam too. The graph-wide watcher is authoritative,
	// but this closes the delivery race where a selected poison reaches this
	// watcher first.
	c.validateContractEntry(entry)
	if c.outputReadinessError() != nil {
		return
	}
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	if c.metrics != nil {
		c.metrics.EntityChanged()
	}

	entityID := entry.Key()
	c.logger.Debug("Entity changed, queuing OASF generation",
		slog.String("entity_id", entityID))

	// Queue for generation (with debouncing)
	if c.queueGeneration != nil {
		c.queueGeneration(entityID)
	} else {
		c.generator.QueueGeneration(entityID)
	}
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cancel background context
	if c.cancel != nil {
		c.cancel()
	}

	c.stopWatchers()

	// Stop generator
	if c.generator != nil {
		c.generator.Stop()
	}

	c.running = false
	c.logger.Info("OASF generator stopped")

	return nil
}

func (c *Component) stopWatchers() {
	if c.kvWatcher != nil {
		if err := c.kvWatcher.Stop(); err != nil {
			c.logger.Warn("Failed to stop selection watcher", slog.Any("error", err))
		}
		c.kvWatcher = nil
	}
	if c.contractWatcher != nil {
		if err := c.contractWatcher.Stop(); err != nil {
			c.logger.Warn("Failed to stop contract watcher", slog.Any("error", err))
		}
		c.contractWatcher = nil
	}
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "oasf-generator",
		Type:        "processor",
		Description: "Generates OASF records from agent entity capabilities",
		Version:     "1.0.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		port := component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
		}
		if portDef.Type == "jetstream" {
			port.Config = component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			}
		} else {
			port.Config = component.NATSPort{
				Subject: portDef.Subject,
			}
		}
		ports[i] = port
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return componentSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "stopped"
	if c.running {
		status = "running"
		if c.graphStatePoison.Load() != nil {
			status = graph.IndexStateResetRequired
		} else if c.entityWatchLost.Load() {
			status = graph.IndexStateDegraded
		}
	}

	return component.HealthStatus{
		Healthy:    c.running && c.graphStatePoison.Load() == nil && !c.entityWatchLost.Load() && c.bootstrapComplete.Load(),
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var errorRate float64
	total := c.recordsGenerated + c.errors
	if total > 0 {
		errorRate = float64(c.errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}

// GenerateForEntity manually triggers OASF generation for an entity.
// This is useful for testing and on-demand generation.
func (c *Component) GenerateForEntity(ctx context.Context, entityID string) error {
	return c.generator.GenerateForEntity(ctx, entityID)
}
