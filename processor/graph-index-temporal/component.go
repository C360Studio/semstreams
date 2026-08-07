// Package graphindextemporal provides the graph-index-temporal component for temporal indexing.
package graphindextemporal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Config holds configuration for graph-index-temporal component
type Config struct {
	Ports          *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	TimeResolution string                `json:"time_resolution" schema:"type:string,description:Time resolution (minute hour day),category:basic"`
	Workers        int                   `json:"workers" schema:"type:int,description:Number of worker goroutines,category:advanced"`
	BatchSize      int                   `json:"batch_size" schema:"type:int,description:Batch size for processing,category:advanced"`

	// Dependency startup configuration
	StartupAttempts int `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts to wait for dependencies at startup,category:advanced"`
	StartupInterval int `json:"startup_interval_ms,omitempty" schema:"type:int,description:Interval between startup attempts in milliseconds,category:advanced"`
}

// Validate implements component.Validatable interface
func (c *Config) Validate() error {
	if c.Ports == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "ports configuration required")
	}
	if len(c.Ports.Inputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one input port required")
	}
	if len(c.Ports.Outputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one output port required")
	}

	// Validate TEMPORAL_INDEX output exists
	hasTemporalIndex := false
	for _, output := range c.Ports.Outputs {
		if output.Subject == graph.BucketTemporalIndex {
			hasTemporalIndex = true
			break
		}
	}
	if !hasTemporalIndex {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", fmt.Sprintf("%s output required", graph.BucketTemporalIndex))
	}

	// Validate time resolution
	if c.TimeResolution == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "time_resolution required")
	}
	if c.TimeResolution != "minute" && c.TimeResolution != "hour" && c.TimeResolution != "day" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "time_resolution must be 'minute', 'hour', or 'day'")
	}

	// Validate workers
	if c.Workers <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "workers must be greater than 0")
	}

	// Validate batch size
	if c.BatchSize <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "batch_size must be greater than 0")
	}

	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	if c.TimeResolution == "" {
		c.TimeResolution = "hour"
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	if c.BatchSize == 0 {
		c.BatchSize = 100
	}

	// Dependency startup defaults
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 30 // ~15 seconds with 500ms interval
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 // milliseconds
	}

	if c.Ports == nil {
		// Apply full default port config
		defaultConf := DefaultConfig()
		c.Ports = defaultConf.Ports
	} else {
		// If ports exist but are empty, populate with defaults
		if len(c.Ports.Inputs) == 0 {
			c.Ports.Inputs = []component.PortDefinition{
				{
					Name: "entity_watch", Config: component.KVWatchPort{Bucket: graph.BucketEntityStates},
				},
			}
		}
		if len(c.Ports.Outputs) == 0 {
			c.Ports.Outputs = []component.PortDefinition{
				{
					Name: "temporal_index", Config: component.KVWritePort{Bucket: graph.BucketTemporalIndex},
				},
			}
		}
	}
}

// DefaultConfig returns a valid default configuration
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "entity_watch", Config: component.KVWatchPort{Bucket: graph.BucketEntityStates},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "temporal_index", Config: component.KVWritePort{Bucket: graph.BucketTemporalIndex},
				},
			},
		},
		TimeResolution: "hour",
		Workers:        4,
		BatchSize:      100,
	}
}

// schema defines the configuration schema for graph-index-temporal component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

type entityStatesWatcher interface {
	WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// Component implements the graph-index-temporal processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Domain resources
	temporalBucket jetstream.KeyValue
	// reverseBucket maps entityID -> current temporal bucket key so a re-indexed
	// or deleted entity can be removed from its prior bucket (gh#370 stale-entry
	// cleanup). Nil-safe: cleanup is skipped when this is unset.
	reverseBucket jetstream.KeyValue

	// Prometheus metrics (event-time vs write-fallback split, stale removals)
	metrics *temporalMetrics

	// Lifecycle state
	mu          sync.RWMutex
	running     bool
	initialized bool
	startTime   time.Time
	wg          sync.WaitGroup
	cancel      context.CancelFunc

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time
	resetState        atomic.Pointer[graph.StateContractError]
	watchUnavailable  atomic.Bool
	bootstrapStarted  atomic.Bool
	bootstrapComplete atomic.Bool

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

// CreateGraphIndexTemporal is the factory function for creating graph-index-temporal components
func CreateGraphIndexTemporal(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphIndexTemporal", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphIndexTemporal", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphIndexTemporal", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-index-temporal")

	// Create component
	comp := &Component{
		name:       "graph-index-temporal",
		config:     config,
		natsClient: natsClient,
		logger:     logger,
		metrics:    getMetrics(deps.MetricsRegistry),
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-index-temporal factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-index-temporal", &component.Registration{
		Name:        "graph-index-temporal",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Graph temporal indexing processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphIndexTemporal,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-index-temporal",
		Type:        "processor",
		Description: "Graph temporal indexing processor",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, 0, len(c.config.Ports.Inputs))
	for _, portDef := range c.config.Ports.Inputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionInput))
	}
	return ports
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, 0, len(c.config.Ports.Outputs))
	for _, portDef := range c.config.Ports.Outputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionOutput))
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}

	errorCount := int(atomic.LoadInt64(&c.errors))
	lastErr := ""
	status := "stopped"

	if c.running {
		status = "running"
		if c.resetState.Load() != nil {
			status = graph.IndexStateResetRequired
			lastErr = graph.ErrorCodeGraphStateResetRequired + ": " + string(c.graphStateResetReason())
		} else if c.watchUnavailable.Load() {
			status = graph.IndexStateDegraded
			lastErr = graph.ErrorCodeIndexNotReady + ": ENTITY_STATES watcher unavailable"
		}
		if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy: c.running && errorCount == 0 && c.resetState.Load() == nil &&
			!c.watchUnavailable.Load(),
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
		LastError:  lastErr,
		Uptime:     uptime,
		Status:     status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	messages := atomic.LoadInt64(&c.messagesProcessed)
	bytes := atomic.LoadInt64(&c.bytesProcessed)
	errorCount := atomic.LoadInt64(&c.errors)

	c.mu.RLock()
	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}
	c.mu.RUnlock()

	// Calculate rates
	var messagesPerSec, bytesPerSec, errorRate float64
	if uptime > 0 {
		seconds := uptime.Seconds()
		messagesPerSec = float64(messages) / seconds
		bytesPerSec = float64(bytes) / seconds
		if messages > 0 {
			errorRate = float64(errorCount) / float64(messages)
		}
	}

	lastAct := time.Now()
	if stored := c.lastActivity.Load(); stored != nil {
		if t, ok := stored.(time.Time); ok {
			lastAct = t
		}
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSec,
		BytesPerSecond:    bytesPerSec,
		ErrorRate:         errorRate,
		LastActivity:      lastAct,
	}
}

// ============================================================================
// LifecycleComponent Interface (3 methods)
// ============================================================================

// Initialize validates configuration and sets up ports (no I/O)
func (c *Component) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil // Idempotent
	}

	// Validate configuration
	if err := c.config.Validate(); err != nil {
		return errs.Wrap(err, "Component", "Initialize", "config validation")
	}

	c.initialized = true
	c.logger.Info("component initialized", slog.String("component", "graph-index-temporal"))

	return nil
}

// waitForEntityBucket waits for the ENTITY_STATES bucket to be available and returns it
func (c *Component) waitForEntityBucket(ctx context.Context) (entityStatesWatcher, error) {
	// Configure resource watcher for bounded startup attempts
	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = time.Duration(c.config.StartupInterval) * time.Millisecond
	watcherCfg.Logger = c.logger

	entityWatcher := resource.NewWatcher(
		graph.BucketEntityStates,
		func(checkCtx context.Context) error {
			_, err := graph.OpenCatalogReader(checkCtx, c.natsClient, graph.BucketEntityStates)
			return err
		},
		watcherCfg,
	)

	if !entityWatcher.WaitForStartup(ctx) {
		return nil, errs.WrapTransient(
			fmt.Errorf("bucket %s not available after %d attempts", graph.BucketEntityStates, c.config.StartupAttempts),
			"Component", "waitForEntityBucket", "dependency not available",
		)
	}

	reader, err := graph.OpenCatalogReader(ctx, c.natsClient, graph.BucketEntityStates)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// Start begins processing (must be initialized first)
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	// Check initialization
	if !c.initialized {
		return errs.WrapFatal(fmt.Errorf("component not initialized"), "Component", "Start", "initialization check")
	}

	// Idempotent - already running
	if c.running {
		return nil
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// TEMPORAL_INDEX bucket (we are the WRITER) — acquired through the catalog
	// owner seam, which reconciles an adopted bucket to the declared policy.
	temporalBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketTemporalIndex)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during bucket creation")
		}
		return errs.Wrap(err, "Component", "Start", fmt.Sprintf("KV bucket creation: %s", graph.BucketTemporalIndex))
	}
	c.temporalBucket = temporalBucket

	// The reverse index bucket (entityID -> current bucket key) used for
	// stale-entry cleanup on re-index and delete.
	reverseBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketTemporalIndexReverse)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during reverse bucket creation")
		}
		return errs.Wrap(err, "Component", "Start", fmt.Sprintf("KV bucket creation: %s", graph.BucketTemporalIndexReverse))
	}
	c.reverseBucket = reverseBucket

	// Queries must fail closed from the moment they are exposed until the
	// WatchAll bootstrap has been validated and fully projected.
	c.watchUnavailable.Store(false)
	c.bootstrapComplete.Store(false)
	c.bootstrapStarted.Store(true)

	// Set up query handlers
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Wait for entity states bucket
	entityBucket, err := c.waitForEntityBucket(ctx)
	if err != nil {
		cancel()
		return err
	}

	// Start entity watcher goroutine
	c.wg.Add(1)
	go c.watchEntityStates(ctx, entityBucket)

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	c.logger.Info("component started",
		slog.String("component", "graph-index-temporal"),
		slog.Time("start_time", c.startTime),
		slog.String("time_resolution", c.config.TimeResolution),
		slog.Int("workers", c.config.Workers))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}

	// Unsubscribe from query handlers
	for _, sub := range c.querySubscriptions {
		if sub != nil {
			if err := sub.Unsubscribe(); err != nil {
				c.logger.Warn("query subscription unsubscribe error", slog.Any("error", err))
			}
		}
	}
	c.querySubscriptions = nil

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-index-temporal"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-index-temporal"))
		return errs.WrapTransient(fmt.Errorf("timeout after %v", timeout), "Component", "Stop", "graceful shutdown timeout")
	}
}

// ============================================================================
// Entity State Watcher
// ============================================================================

// watchEntityStates watches the ENTITY_STATES KV bucket and indexes entities with temporal data
func (c *Component) watchEntityStates(ctx context.Context, bucket entityStatesWatcher) {
	defer c.wg.Done()
	c.bootstrapStarted.Store(true)

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		// Context cancellation during shutdown is expected, not an error
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("entity watcher stopped due to context cancellation",
				slog.String("bucket", graph.BucketEntityStates))
			return
		}
		c.logger.Error("failed to start entity watcher",
			slog.String("bucket", graph.BucketEntityStates),
			slog.Any("error", err))
		c.watchUnavailable.Store(true)
		return
	}
	// NOTE: watcher.Stop() is called explicitly before each return, not via defer.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine when using defer.

	c.logger.Info("entity watcher started", slog.String("bucket", graph.BucketEntityStates))

	// Build the private projection incrementally in constant space. Query handlers
	// remain gated until nil proves the complete WatchAll snapshot valid.
	validating := true
	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("entity watcher stopping", slog.String("reason", "context cancelled"))
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				watcher.Stop()
				if ctx.Err() == nil {
					c.watchUnavailable.Store(true)
				}
				return
			}
			if entry == nil {
				if !validating {
					continue
				}
				validating = false
				if c.resetState.Load() != nil {
					c.logger.Error("entity watcher bootstrap rejected",
						slog.String("reason", string(c.graphStateResetReason())))
					continue
				}
				c.bootstrapComplete.Store(true)
				c.logger.Debug("entity watcher initial sync complete")
				continue
			}

			if validating {
				if !graph.IsKVTombstone(entry.Operation()) {
					c.validateEntityStateEntry(entry)
				}
				if c.resetState.Load() == nil {
					c.applyEntityWatchEntry(ctx, entry)
				}
				continue
			}
			if c.resetState.Load() == nil {
				c.applyEntityWatchEntry(ctx, entry)
			}
		}
	}
}

func (c *Component) validateEntityStateEntry(entry jetstream.KeyValueEntry) {
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.latchGraphStateReset(stateErr.Reason)
			return
		}
		c.latchGraphStateReset(graph.GraphStateReasonUnreadableEntity)
	}
}

func (c *Component) applyEntityWatchEntry(ctx context.Context, entry jetstream.KeyValueEntry) {
	if graph.IsKVTombstone(entry.Operation()) {
		c.handleEntityDelete(ctx, entry.Key())
		return
	}
	c.processEntityUpdate(ctx, entry)
}

func (c *Component) latchGraphStateReset(reason graph.StateResetReason) {
	c.resetState.CompareAndSwap(nil, &graph.StateContractError{Reason: reason})
}

// processEntityUpdate indexes an entity's temporal data if it has timestamps
func (c *Component) processEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.latchGraphStateReset(stateErr.Reason)
		}
		c.logger.Warn("failed to unmarshal entity state",
			slog.String("entity", entry.Key()),
			slog.Any("error", err))
		return
	}

	// Determine entity ID
	entityID := state.ID
	if entityID == "" {
		entityID = entry.Key()
	}
	if entityID == "" && len(state.Triples) > 0 {
		entityID = state.Triples[0].Subject
	}

	// Resolve the index timestamp by explicit precedence: the observation
	// timestamp (event-time) when present, else UpdatedAt (processing-time).
	ts, source, ok := resolveIndexTimestamp(state)
	if !ok {
		// No usable timestamp — entity is not temporally indexable.
		return
	}

	// Calculate time bucket based on resolution
	timeBucket := c.calculateTimeBucket(ts)

	// Look up the entity's prior bucket before mutating anything.
	prevBucket, hadPrev := c.getReverseBucket(ctx, entityID)

	// Add/refresh the entity in its (new) bucket FIRST. If this fails we return
	// before touching the prior bucket, so the entity stays queryable at its old
	// location rather than disappearing — the safe failure mode for an index
	// feeding "what's in this window" (upsert by entity within the bucket).
	if err := c.updateTemporalIndex(ctx, timeBucket, entityID, ts); err != nil {
		c.logger.Warn("failed to update temporal index",
			slog.String("entity", entityID),
			slog.String("bucket", timeBucket),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Then remove the stale entry from the prior bucket if the entity moved. A
	// failure here degrades to a transient duplicate (range queries dedup by
	// entity), not a disappearance.
	if hadPrev && prevBucket != timeBucket {
		c.removeEntityFromBucket(ctx, prevBucket, entityID)
	}

	// Record the entity's current bucket for future cleanup, and the source split.
	c.setReverseBucket(ctx, entityID, timeBucket)
	c.metrics.recordIndexed(source)

	c.logger.Debug("indexed entity temporal data",
		slog.String("entity", entityID),
		slog.String("bucket", timeBucket),
		slog.String("timestamp_source", source),
		slog.Time("timestamp", ts))

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())
}

func (c *Component) graphStateResetReason() graph.StateResetReason {
	if state := c.resetState.Load(); state != nil {
		return state.Reason
	}
	return graph.GraphStateReasonUnreadableEntity
}

func (c *Component) graphStateResetError() error {
	state := c.resetState.Load()
	if state == nil {
		state = &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity}
	}
	return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		state)
}

func (c *Component) ensureBootstrapReady() error {
	if c.resetState.Load() != nil {
		return c.graphStateResetError()
	}
	if c.watchUnavailable.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("temporal index not ready: ENTITY_STATES watcher is unavailable"))
	}
	if c.bootstrapStarted.Load() && !c.bootstrapComplete.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("temporal index not ready: ENTITY_STATES bootstrap is still validating"))
	}
	return nil
}

// predicateObservationRecorded is the canonical event-time (observation) predicate.
// When present it is the PRIMARY temporal index key; UpdatedAt is the fallback.
const predicateObservationRecorded = "time.observation.recorded"

// resolveIndexTimestamp picks the timestamp to index an entity under, by explicit
// precedence (gh#370):
//
//  1. time.observation.recorded — event-time (latest value when several present)
//  2. EntityState.UpdatedAt     — processing-time (last-write) fallback
//
// The returned source is indexSourceObserved or indexSourceWriteFallback. ok is
// false only when neither a parseable observation timestamp nor a non-zero
// UpdatedAt is available.
func resolveIndexTimestamp(state graph.EntityState) (ts time.Time, source string, ok bool) {
	if observed, found := latestObservationTime(state.Triples); found {
		return observed, indexSourceObserved, true
	}
	if !state.UpdatedAt.IsZero() {
		return state.UpdatedAt, indexSourceWriteFallback, true
	}
	return time.Time{}, "", false
}

// latestObservationTime returns the most recent parseable value of the
// observation predicate across the triples, if any. Observation timestamps
// should be authored single-valued (replace-on-update); when more than one is
// present the latest wins rather than an arbitrary first-match.
func latestObservationTime(triples []message.Triple) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, triple := range triples {
		if triple.Predicate != predicateObservationRecorded {
			continue
		}
		if t, ok := parseTripleTime(triple.Object); ok {
			if !found || t.After(latest) {
				latest = t
				found = true
			}
		}
	}
	return latest, found
}

// parseTripleTime parses a triple object into a time.Time, accepting time.Time,
// RFC3339 / RFC3339Nano / common-ISO strings, and Unix-seconds numerics.
func parseTripleTime(obj any) (time.Time, bool) {
	switch v := obj.(type) {
	case time.Time:
		return v, true
	case string:
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t, true
			}
		}
	case float64:
		return time.Unix(int64(v), 0).UTC(), true
	case int64:
		return time.Unix(v, 0).UTC(), true
	case int:
		return time.Unix(int64(v), 0).UTC(), true
	}
	return time.Time{}, false
}

// calculateTimeBucket calculates the time bucket key based on configured resolution
// Uses dot-separated format to match indexmanager/manager.go:1518 QueryTemporal expectations
func (c *Component) calculateTimeBucket(ts time.Time) string {
	t := ts.UTC()
	switch c.config.TimeResolution {
	case "minute":
		return fmt.Sprintf("%04d.%02d.%02d.%02d.%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
	case "hour":
		return fmt.Sprintf("%04d.%02d.%02d.%02d", t.Year(), t.Month(), t.Day(), t.Hour())
	case "day":
		return fmt.Sprintf("%04d.%02d.%02d", t.Year(), t.Month(), t.Day())
	default:
		return fmt.Sprintf("%04d.%02d.%02d.%02d", t.Year(), t.Month(), t.Day(), t.Hour()) // Default to hour
	}
}

// updateTemporalIndex updates the temporal index bucket for a time bucket
// Uses events array format to match indexmanager/indexes.go:1520-1552 and QueryTemporal expectations
func (c *Component) updateTemporalIndex(ctx context.Context, timeBucket, entityID string, ts time.Time) error {
	// Get current data for this time bucket
	entry, err := c.temporalBucket.Get(ctx, timeBucket)

	var temporalData map[string]interface{}
	if err == nil {
		if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
			temporalData = map[string]interface{}{
				"events":       []interface{}{},
				"entity_count": 0,
			}
		}
	} else {
		temporalData = map[string]interface{}{
			"events":       []interface{}{},
			"entity_count": 0,
		}
	}

	// Get or create events array
	events, _ := temporalData["events"].([]interface{})

	// Upsert by entity: drop any existing event for this entity in this bucket,
	// then append the current one. Keeps re-indexing the same entity to the same
	// bucket idempotent (e.g. restart WatchAll re-delivery) and one-event-per-entity.
	filtered := make([]interface{}, 0, len(events)+1)
	for _, evt := range events {
		if m, ok := evt.(map[string]interface{}); ok {
			if e, _ := m["entity"].(string); e == entityID {
				continue
			}
		}
		filtered = append(filtered, evt)
	}
	newEvent := map[string]interface{}{
		"entity":    entityID,
		"type":      "update",
		"timestamp": ts.Format(time.RFC3339),
	}
	events = append(filtered, newEvent)
	temporalData["events"] = events

	// Track unique entity count
	uniqueEntities := make(map[string]bool)
	for _, evt := range events {
		if eventMap, ok := evt.(map[string]interface{}); ok {
			if entity, ok := eventMap["entity"].(string); ok {
				uniqueEntities[entity] = true
			}
		}
	}
	temporalData["entity_count"] = len(uniqueEntities)

	// Serialize and write
	data, err := json.Marshal(temporalData)
	if err != nil {
		return errs.Wrap(err, "Component", "updateTemporalIndex", "marshal temporal data")
	}

	if entry != nil {
		_, err = c.temporalBucket.Update(ctx, timeBucket, data, entry.Revision())
	} else {
		_, err = c.temporalBucket.Create(ctx, timeBucket, data)
	}

	if err != nil {
		return errs.Wrap(err, "Component", "updateTemporalIndex", "write temporal data")
	}

	return nil
}

// handleEntityDelete removes a deleted entity from its temporal bucket and its
// reverse mapping, so range queries no longer return it.
func (c *Component) handleEntityDelete(ctx context.Context, entityID string) {
	prevBucket, found := c.getReverseBucket(ctx, entityID)
	if !found {
		c.logger.Debug("entity deleted - no temporal reverse entry to clean",
			slog.String("entity", entityID))
		return
	}
	c.removeEntityFromBucket(ctx, prevBucket, entityID)
	c.deleteReverseBucket(ctx, entityID)
	c.logger.Debug("entity deleted - temporal cleanup done",
		slog.String("entity", entityID),
		slog.String("bucket", prevBucket))
}

// removeEntityFromBucket removes all events for entityID from the given time
// bucket, deleting the bucket key if it becomes empty. Best-effort: missing
// buckets and transient errors are logged at debug and swallowed.
func (c *Component) removeEntityFromBucket(ctx context.Context, bucket, entityID string) {
	entry, err := c.temporalBucket.Get(ctx, bucket)
	if err != nil {
		return // bucket already gone — nothing to remove
	}

	var temporalData map[string]interface{}
	if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
		return
	}

	events, _ := temporalData["events"].([]interface{})
	filtered := make([]interface{}, 0, len(events))
	removed := false
	for _, evt := range events {
		if m, ok := evt.(map[string]interface{}); ok {
			if e, _ := m["entity"].(string); e == entityID {
				removed = true
				continue
			}
		}
		filtered = append(filtered, evt)
	}
	if !removed {
		return
	}

	if len(filtered) == 0 {
		// No events left — drop the bucket key entirely.
		if err := c.temporalBucket.Delete(ctx, bucket); err != nil {
			c.logger.Debug("failed to delete empty temporal bucket",
				slog.String("bucket", bucket), slog.Any("error", err))
		}
		c.metrics.recordStaleRemoval()
		return
	}

	// Recompute unique entity count and write back.
	uniqueEntities := make(map[string]bool)
	for _, evt := range filtered {
		if m, ok := evt.(map[string]interface{}); ok {
			if e, ok := m["entity"].(string); ok {
				uniqueEntities[e] = true
			}
		}
	}
	temporalData["events"] = filtered
	temporalData["entity_count"] = len(uniqueEntities)

	data, err := json.Marshal(temporalData)
	if err != nil {
		return
	}
	if _, err := c.temporalBucket.Update(ctx, bucket, data, entry.Revision()); err != nil {
		c.logger.Debug("failed to write temporal bucket after stale removal",
			slog.String("bucket", bucket), slog.Any("error", err))
		return
	}
	c.metrics.recordStaleRemoval()
}

// getReverseBucket returns the time bucket the entity is currently indexed in.
func (c *Component) getReverseBucket(ctx context.Context, entityID string) (string, bool) {
	if c.reverseBucket == nil {
		return "", false
	}
	entry, err := c.reverseBucket.Get(ctx, entityID)
	if err != nil {
		return "", false
	}
	return string(entry.Value()), true
}

// setReverseBucket records the time bucket the entity is currently indexed in.
func (c *Component) setReverseBucket(ctx context.Context, entityID, bucket string) {
	if c.reverseBucket == nil {
		return
	}
	if _, err := c.reverseBucket.Put(ctx, entityID, []byte(bucket)); err != nil {
		// Drift risk: a missing reverse entry means a later delete/re-index can't
		// clean this entity's bucket. Warn + count so it is observable.
		c.logger.Warn("failed to write temporal reverse index (forward/reverse may drift)",
			slog.String("entity", entityID), slog.Any("error", err))
		c.metrics.recordReverseError()
	}
}

// deleteReverseBucket removes the entity's reverse mapping.
func (c *Component) deleteReverseBucket(ctx context.Context, entityID string) {
	if c.reverseBucket == nil {
		return
	}
	if err := c.reverseBucket.Delete(ctx, entityID); err != nil {
		c.logger.Warn("failed to delete temporal reverse index",
			slog.String("entity", entityID), slog.Any("error", err))
		c.metrics.recordReverseError()
	}
}
