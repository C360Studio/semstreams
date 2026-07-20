// Package graphindex provides the graph-index component for maintaining graph relationship indexes.
package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/c360studio/semstreams/pkg/revlag"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

const maxGraphIndexWorkers = 16

// Config holds configuration for graph-index component
type Config struct {
	Ports     *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	Workers   int                   `json:"workers" schema:"type:int,description:Number of worker goroutines,category:advanced"`
	BatchSize int                   `json:"batch_size" schema:"type:int,description:Batch size for index updates,category:advanced"`

	// Dependency startup configuration
	StartupAttempts int `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts to wait for dependencies at startup,category:advanced"`
	StartupInterval int `json:"startup_interval_ms,omitempty" schema:"type:int,description:Interval between startup attempts in milliseconds,category:advanced"`
	CoalesceMs      int `json:"coalesce_ms,omitempty" schema:"type:int,description:Debounce window for entity updates in ms. 0=immediate processing,category:advanced"`
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

	// Validate required output buckets exist
	requiredBuckets := map[string]bool{
		graph.BucketOutgoingIndex:  false,
		graph.BucketIncomingIndex:  false,
		graph.BucketAliasIndex:     false,
		graph.BucketPredicateIndex: false,
	}

	for _, output := range c.Ports.Outputs {
		if output.Subject != "" {
			if _, required := requiredBuckets[output.Subject]; required {
				requiredBuckets[output.Subject] = true
			}
		}
	}

	for bucket, found := range requiredBuckets {
		if !found {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				fmt.Sprintf("required output bucket missing: %s", bucket))
		}
	}

	// Validate workers
	if c.Workers < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "workers cannot be negative")
	}
	if c.Workers > maxGraphIndexWorkers {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			fmt.Sprintf("workers cannot exceed %d", maxGraphIndexWorkers))
	}

	// Validate batch size
	if c.BatchSize < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "batch_size cannot be negative")
	}

	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	if c.Workers == 0 {
		c.Workers = 1
	}
	if c.BatchSize == 0 {
		c.BatchSize = 50
	}
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 30 // ~15 seconds with 500ms interval
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 // 500ms
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
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			}
		}
		if len(c.Ports.Outputs) == 0 {
			c.Ports.Outputs = []component.PortDefinition{
				{
					Name:    "outgoing_index",
					Type:    "kv-write",
					Subject: graph.BucketOutgoingIndex,
				},
				{
					Name:    "incoming_index",
					Type:    "kv-write",
					Subject: graph.BucketIncomingIndex,
				},
				{
					Name:    "alias_index",
					Type:    "kv-write",
					Subject: graph.BucketAliasIndex,
				},
				{
					Name:    "predicate_index",
					Type:    "kv-write",
					Subject: graph.BucketPredicateIndex,
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
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:    "outgoing_index",
					Type:    "kv-write",
					Subject: graph.BucketOutgoingIndex,
				},
				{
					Name:    "incoming_index",
					Type:    "kv-write",
					Subject: graph.BucketIncomingIndex,
				},
				{
					Name:    "alias_index",
					Type:    "kv-write",
					Subject: graph.BucketAliasIndex,
				},
				{
					Name:    "predicate_index",
					Type:    "kv-write",
					Subject: graph.BucketPredicateIndex,
				},
			},
		},
		Workers:   1,
		BatchSize: 50,
	}
}

// schema defines the configuration schema for graph-index component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the graph-index processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient      *natsclient.Client
	logger          *slog.Logger
	metricsRegistry *metric.MetricsRegistry

	// Domain resources - KV buckets for index storage (wrapped for CAS + retry)
	outgoingBucket     *natsclient.KVStore
	incomingBucket     *natsclient.KVStore
	aliasBucket        *natsclient.KVStore
	predicateBucket    *natsclient.KVStore
	contextBucket      *natsclient.KVStore
	nameBucket         *natsclient.KVStore
	entityStatesBucket jetstream.KeyValue // raw: read-only watcher, no CAS needed
	// entityStatesStatusBucket is a separate JetStream KV handle used only for
	// Status/LastSeq reads. nats.go's KV handle caches stream info internally, and
	// concurrent Status and Get calls on one handle race under -race.
	entityStatesStatusBucket jetstream.KeyValue

	// Lifecycle state
	mu              sync.RWMutex
	running         bool
	initialized     bool
	startTime       time.Time
	wg              sync.WaitGroup
	cancel          context.CancelFunc
	indexPool       *keyedDispatcher[entityIndexWork]
	entityCoalescer *revisionCoalescer

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time

	// nameIndexReady is the sticky readiness signal for graph.index.query.status
	// (gh#397). Set once the NAME_INDEX is known non-empty; an index does not
	// un-build, so once true it stays true (O(1) steady state). Restart-safe: a
	// not-yet-true status read does a one-time bucket check (handleQueryStatus).
	//
	// DEPRECATED by ADR-066: readiness is now the revision-lag watermark below,
	// which means "caught up," not "indexing started." Retained only for the
	// nameIndexIsReady fallback used by the byName query path.
	nameIndexReady atomic.Bool

	// watermark is the ADR-066 low-water-of-pending "caught up" tracker feeding the
	// honest graph.index.query.status. Non-nil once Start wires the watcher.
	watermark *revlag.Watermark

	// Readiness stuck-detector state (ADR-066 §4), guarded by statusMu; touched only
	// by the polled status handler. lastProgressAt is the wall-clock of the last
	// observed IndexedRevision advance; a stall past degradedStuckAfter while not
	// caught-up flips State to degraded.
	statusMu        sync.Mutex
	statusTargetMu  sync.Mutex
	lastIndexedSeen uint64
	lastProgressAt  time.Time

	// Prometheus metrics
	metrics *indexMetrics

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Re-index no-op instrumentation (D6, design.md / gh#474).
	// lastProjections maps entityID → canonical projection string for change detection.
	// sync.Map provides safe concurrent access from worker-pool goroutines.
	lastProjections sync.Map
	// reindexTotal counts total entity re-index events; reindexUnchanged counts
	// events where the index-input projection was identical to the last-indexed one.
	// These are the L2 change-detection data gates — observe only, never skip writes.
	reindexTotal     int64 // atomic
	reindexUnchanged int64 // atomic

	// indexBootstrapped is a sticky flag (gh#474 Codex P1d): false until the index has
	// been observed CAUGHT UP to ENTITY_STATES at least once after Start (indexed >=
	// target). While false, the reverse-index query handlers return ErrorCodeIndexNotReady
	// rather than serving the partial keyset a cutover / cold replay is still building.
	// Set ONLY on an observed catch-up — NOT on the initial-enumeration sentinel, which
	// means "all pre-existing entries were DELIVERED to the worker pool," not "their
	// async writes completed" (gh#474 Codex #1). Once true it never flips back; steady-
	// state lag is surfaced via graph.index.query.status.
	indexBootstrapped atomic.Bool

	// initialEnumerationComplete flips when the WatchAll initial-sync sentinel fires: every
	// entity that existed at watch-start has been delivered. It authorizes ONLY the
	// authoritative-empty readiness exception (target==0/indexed==0), never the non-empty
	// case — a preloaded bucket's workers may still be writing (gh#474 Codex #1).
	initialEnumerationComplete atomic.Bool

	// failedEntities / failedCount track entities whose required index writes did not
	// all succeed after bounded retry (gh#474 Codex P1b). While failedCount > 0 the
	// index is NOT authoritative — computeIndexStatus withholds Ready so incoming/byName
	// queries return ErrorCodeIndexNotReady rather than serving adjacency that is known
	// to be missing. An entry is added on ultimate write failure and removed when that
	// same entity later indexes cleanly (or is deleted). failedCount mirrors the map
	// size as a cheap O(1) gate for the per-query readiness check.
	failedEntities sync.Map
	failedCount    atomic.Int64

	// resetState is a sticky poison state for unreadable or noncanonical
	// authoritative ENTITY_STATES. Repair retries must never clear it; only an
	// operator reset followed by process restart creates a clean component.
	resetState atomic.Pointer[graph.StateContractError]

	// Alias predicates from vocabulary (cached at startup for performance)
	aliasPredicates map[string]int

	// Label (display-name) predicates from vocabulary, cached at startup. Keys
	// the NAME_INDEX for graph.query.byName (gh#376); value = salience priority.
	namePredicates map[string]int

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

type entityIndexWork struct {
	entityID           string
	completionRevision uint64
}

// CreateGraphIndex is the factory function for creating graph-index components
func CreateGraphIndex(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphIndex", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphIndex", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphIndex", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-index")

	// Create component
	comp := &Component{
		name:            "graph-index",
		config:          config,
		natsClient:      natsClient,
		logger:          logger,
		metrics:         getMetrics(deps.MetricsRegistry),
		metricsRegistry: deps.MetricsRegistry,
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-index factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-index", &component.Registration{
		Name:        "graph-index",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Graph relationship index maintenance processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphIndex,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-index",
		Type:        "processor",
		Description: "Graph relationship index maintenance processor",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions.
// Reads directly from config so ports are available before Initialize().
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

// OutputPorts returns output port definitions.
// Reads directly from config so ports are available before Initialize().
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
		if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy:    c.running && errorCount == 0,
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
	c.logger.Info("component initialized", slog.String("component", "graph-index"))

	return nil
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

	// Cache alias predicates from vocabulary for fast lookup during indexing
	c.aliasPredicates = vocabulary.DiscoverAliasPredicates()
	c.namePredicates = vocabulary.DiscoverLabelPredicates()
	c.logger.Debug("cached alias predicates from vocabulary", slog.Int("count", len(c.aliasPredicates)))

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "context cancelled")
	}

	// Create output KV buckets (we are the writer)
	if err := c.createOutputBuckets(ctx); err != nil {
		cancel()
		return err
	}

	// Readiness watermark (ADR-066): must exist before the pool or the watcher so
	// the first completion/observation has somewhere to land.
	c.watermark = revlag.New()

	// Create and start the entity index worker pool
	if err := c.startIndexPool(ctx); err != nil {
		cancel()
		return err
	}

	// Initialize lifecycle reporter (throttled for high-throughput indexing)
	c.initLifecycleReporter(ctx)

	// Initialize the optional revision-aware coalescer before the watcher can
	// observe it. The callback submits a reconciliation key into the same ordered
	// dispatcher used by ordinary updates, deletes, and repair.
	if c.config.CoalesceMs > 0 {
		c.entityCoalescer = newRevisionCoalescer(
			ctx,
			time.Duration(c.config.CoalesceMs)*time.Millisecond,
			func(entities []coalescedEntity) { c.processEntityBatch(ctx, entities) },
		)
	}

	// Wait for input KV bucket (ENTITY_STATES) with bounded startup attempts
	js, err := c.natsClient.JetStream()
	if err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "JetStream connection")
	}

	// Wait for ENTITY_STATES bucket and start the watcher goroutine
	if err := c.waitAndWatchEntityStates(ctx, js); err != nil {
		cancel()
		return err
	}

	// Set up query handler subscriptions
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "query handler setup")
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("component started",
		slog.String("component", "graph-index"),
		slog.Time("start_time", c.startTime))

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

	// Cancel producers before stopping the ordered dispatcher. This prevents a
	// watcher submission from racing a closed execution lane.
	if c.cancel != nil {
		c.cancel()
	}

	// Stop the entity coalescer before the worker pool.
	if c.entityCoalescer != nil {
		c.entityCoalescer.Close()
	}

	// Wait for ordered execution lanes to observe cancellation and exit.
	if c.indexPool != nil {
		if err := c.indexPool.Stop(timeout); err != nil {
			c.logger.Warn("index pool stop error", slog.Any("error", err))
		}
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
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-index"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-index"))
		return fmt.Errorf("stop timeout after %v", timeout)
	}
}

// createOutputBuckets creates all output KV buckets for the indexes.
func (c *Component) createOutputBuckets(ctx context.Context) error {
	for _, portDef := range c.config.Ports.Outputs {
		bucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
			Bucket:      portDef.Subject,
			Description: fmt.Sprintf("Graph index bucket: %s", portDef.Name),
		})
		if err != nil {
			if ctx.Err() != nil {
				return errs.Wrap(ctx.Err(), "Component", "createOutputBuckets", "context cancelled")
			}
			return errs.Wrap(err, "Component", "createOutputBuckets", fmt.Sprintf("KV bucket: %s", portDef.Subject))
		}
		c.assignBucket(portDef.Subject, bucket)
	}

	// Create CONTEXT_INDEX bucket for triple provenance tracking
	contextBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketContextIndex,
		Description: "Triple context provenance index",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "createOutputBuckets", fmt.Sprintf("KV bucket: %s", graph.BucketContextIndex))
	}
	c.contextBucket = c.natsClient.NewKVStore(contextBucket)

	// Create NAME_INDEX bucket for name→ranked-IDs lookup (gh#376). Internal like
	// CONTEXT_INDEX — not a declared output port, so existing configs don't need
	// to add it.
	nameBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketNameIndex,
		Description: "Name/title → entities index for deterministic name lookup",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "createOutputBuckets", fmt.Sprintf("KV bucket: %s", graph.BucketNameIndex))
	}
	c.nameBucket = c.natsClient.NewKVStore(nameBucket)

	return nil
}

// assignBucket wraps a raw jetstream.KeyValue bucket with natsclient.KVStore
// for CAS support, retry, and consistent error handling, then assigns it.
func (c *Component) assignBucket(subject string, bucket jetstream.KeyValue) {
	kvStore := c.natsClient.NewKVStore(bucket)
	switch subject {
	case graph.BucketOutgoingIndex:
		c.outgoingBucket = kvStore
	case graph.BucketIncomingIndex:
		c.incomingBucket = kvStore
	case graph.BucketAliasIndex:
		c.aliasBucket = kvStore
	case graph.BucketPredicateIndex:
		c.predicateBucket = kvStore
	}
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMPONENT_STATUS",
		Description: "Component lifecycle status tracking",
	})
	if err != nil {
		c.logger.Warn("Failed to create COMPONENT_STATUS bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
		KV:               statusBucket,
		ComponentName:    "graph-index",
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// ============================================================================
// Entity State Watcher
// ============================================================================

// watchEntityStates watches the ENTITY_STATES KV bucket and indexes entity updates
func (c *Component) watchEntityStates(ctx context.Context, bucket jetstream.KeyValue) {
	defer c.wg.Done()

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
		return
	}
	// NOTE: watcher.Stop() is called explicitly before each return, not via defer.
	// This avoids a race condition in nats.go where Stop() can race with the
	// internal message handler goroutine when using defer.

	c.logger.Info("entity watcher started", slog.String("bucket", graph.BucketEntityStates))

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("entity watcher stopping", slog.String("reason", "context cancelled"))
			watcher.Stop()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				// Channel closed, watcher stopped externally
				watcher.Stop()
				return
			}
			if entry == nil {
				// nil entry indicates initial state enumeration complete: every entity that
				// existed at watch-start has been DELIVERED (0 or more) — NOT that their
				// async worker-pool writes finished. So this authorizes only the
				// authoritative-empty 0/0 readiness exception (via computeIndexStatus),
				// which the revision-lag check (target>0) can never confirm; it must NOT
				// flip the sticky bootstrap flag, or a large non-empty cold replay would
				// serve partial state before its workers complete (gh#474 Codex #1).
				c.logger.Debug("entity watcher initial sync complete")
				c.initialEnumerationComplete.Store(true)
				continue
			}

			// Record every delivered revision (update AND delete) as in-flight for
			// the readiness watermark before dispatch (ADR-066 §1). observedHigh
			// advances here; complete() drains on processing return / after delete.
			if c.watermark != nil {
				c.watermark.Observe(entry.Revision(), entry.Key())
			}

			if graph.IsKVTombstone(entry.Operation()) {
				if c.entityCoalescer != nil {
					c.entityCoalescer.Remove(entry.Key())
				}
				if err := c.submitEntityWork(ctx, entityIndexWork{
					entityID:           entry.Key(),
					completionRevision: entry.Revision(),
				}); err != nil {
					c.logger.Warn("failed to submit entity delete",
						slog.String("entity", entry.Key()), slog.Any("error", err))
				}
				continue
			}

			if c.entityCoalescer != nil {
				c.entityCoalescer.Add(entry.Key(), entry.Revision())
			} else if c.indexPool != nil {
				if err := c.submitEntityWork(ctx, entityIndexWork{
					entityID:           entry.Key(),
					completionRevision: entry.Revision(),
				}); err != nil {
					c.logger.Warn("failed to submit entity for indexing",
						slog.String("entity", entry.Key()),
						slog.Any("error", err))
				}
			} else {
				c.processEntityUpdate(ctx, entry)
			}
		}
	}
}

// waitAndWatchEntityStates waits for the ENTITY_STATES bucket with bounded retries,
// then starts the watcher goroutine that feeds entity updates to the worker pool.
func (c *Component) waitAndWatchEntityStates(ctx context.Context, js jetstream.JetStream) error {
	if err := c.lifecycleReporter.ReportStage(ctx, "waiting_for_"+graph.BucketEntityStates); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "waiting_for_"+graph.BucketEntityStates), slog.Any("error", err))
	}

	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = time.Duration(c.config.StartupInterval) * time.Millisecond
	watcherCfg.Logger = c.logger

	entityWatcher := resource.NewWatcher(
		graph.BucketEntityStates,
		func(checkCtx context.Context) error {
			_, err := js.KeyValue(checkCtx, graph.BucketEntityStates)
			return err
		},
		watcherCfg,
	)

	if !entityWatcher.WaitForStartup(ctx) {
		return errs.WrapTransient(
			fmt.Errorf("bucket %s not available after %d attempts", graph.BucketEntityStates, c.config.StartupAttempts),
			"Component", "Start", "dependency not available",
		)
	}

	var err error
	if c.entityStatesBucket, err = js.KeyValue(ctx, graph.BucketEntityStates); err != nil {
		return errs.Wrap(err, "Component", "Start", "get entity bucket after availability check")
	}
	if c.entityStatesStatusBucket, err = js.KeyValue(ctx, graph.BucketEntityStates); err != nil {
		return errs.Wrap(err, "Component", "Start", "get entity status bucket after availability check")
	}

	c.wg.Add(1)
	go c.watchEntityStates(ctx, c.entityStatesBucket)

	// Durable repair loop (gh#474 Codex #3): retries entities whose index writes/deletes
	// failed so a transient outage self-heals without waiting for another entity event.
	c.wg.Add(1)
	go c.repairLoop(ctx)

	// Readiness-envelope metrics loop (ADR-066): republishes readiness/lag/watermark as
	// scrapeable gauges independent of NATS status traffic (#579 at the source).
	c.wg.Add(1)
	go c.statusMetricsLoop(ctx)
	return nil
}

// startIndexPool creates and starts the entity index worker pool.
func (c *Component) startIndexPool(ctx context.Context) error {
	c.indexPool = newKeyedDispatcher(
		c.config.Workers,
		1000,
		func(work entityIndexWork) string { return work.entityID },
		c.processEntityWork,
	)
	c.indexPool.Start(ctx)
	return nil
}

func (c *Component) submitEntityWork(ctx context.Context, work entityIndexWork) error {
	if c.indexPool == nil {
		c.processEntityWork(ctx, work)
		return nil
	}
	return c.indexPool.Submit(ctx, work)
}

// processEntityWork is the only mutation entry point used by the watcher,
// coalescer, and repair loop. The keyed dispatcher guarantees FIFO per entity.
func (c *Component) processEntityWork(ctx context.Context, work entityIndexWork) {
	// Every operation reconciles authoritative ENTITY_STATES at execution. This
	// handles the inverse-submission race where repair observes R2 before the
	// watcher submits a captured R1: regardless of queue order, each lane item
	// applies current truth rather than its stale event snapshot. It also avoids an
	// unbounded per-entity generation ledger.
	completionAllowed := c.reconcileEntity(ctx, work.entityID)
	if completionAllowed && c.watermark != nil && work.completionRevision > 0 {
		c.watermark.Complete(work.entityID, work.completionRevision)
	}
}

// retryIndexWrites runs writeAll up to indexWriteMaxAttempts times, returning nil on
// the first success and the last error otherwise (gh#474 P1b). writeAll is idempotent,
// so retrying the whole set recovers a transient KV blip; a cancelled context aborts
// immediately.
func (c *Component) retryIndexWrites(ctx context.Context, writeAll func() error) error {
	var writeErr error
	for attempt := 0; attempt < indexWriteMaxAttempts; attempt++ {
		if writeErr = writeAll(); writeErr == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if attempt < indexWriteMaxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
			}
		}
	}
	return writeErr
}

// markEntityFailed records that entityID's required index writes did not all succeed
// after retry (gh#474 P1b). Idempotent: failedCount increments only on the first mark
// so it mirrors the set size. While failedCount > 0 the index withholds readiness.
func (c *Component) markEntityFailed(entityID string) {
	if _, loaded := c.failedEntities.LoadOrStore(entityID, struct{}{}); !loaded {
		c.failedCount.Add(1)
		if c.metrics != nil {
			c.metrics.recordIndexWriteFailure()
		}
	}
}

// clearEntityFailed removes entityID from the failed set after a clean re-index or a
// delete, decrementing failedCount only if it was present.
func (c *Component) clearEntityFailed(entityID string) {
	if _, loaded := c.failedEntities.LoadAndDelete(entityID); loaded {
		c.failedCount.Add(-1)
	}
}

// indexRepairInterval is how often the repair loop retries entities whose required
// index writes/deletes failed (gh#474 Codex #3).
const indexRepairInterval = 30 * time.Second

// repairLoop periodically re-drives entities marked failed so a transient KV outage
// self-heals without waiting for another entity event or a process restart (gh#474
// Codex #3 — the durable recovery path). Runs until ctx is cancelled (Stop).
func (c *Component) repairLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(indexRepairInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.repairFailedEntities(ctx)
		}
	}
}

// statusMetricsInterval is how often the readiness envelope (ADR-066) is republished
// as Prometheus gauges. A dedicated cadence (not the repair loop's 30s repair backoff)
// keeps metric freshness independent of the repair schedule. Kept well above
// sub-second because each refresh does a BucketLastSeq NATS read for the target; a few
// seconds is ample for dashboards/alerts while never freezing at a stale value.
const statusMetricsInterval = 5 * time.Second

// statusMetricsLoop periodically republishes the readiness envelope as gauges so an
// operator can scrape readiness/lag/watermark without issuing a NATS status request —
// the #579 silent-staleness class at the source. Runs until ctx is cancelled (Stop).
func (c *Component) statusMetricsLoop(ctx context.Context) {
	defer c.wg.Done()
	// Publish once up front so the gauges are populated before the first tick rather
	// than reading zero for the first interval.
	c.refreshReadinessMetrics(ctx)
	ticker := time.NewTicker(statusMetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshReadinessMetrics(ctx)
		}
	}
}

// refreshReadinessMetrics computes the current readiness envelope and publishes it as
// gauges. Extracted from statusMetricsLoop so tests drive the real compute+set path
// with no goroutine, sleep, or NATS status query. Tolerates the not-yet-ready compute
// (nil watermark/bucket): computeIndexStatus returns {Ready:false, State:building} and
// the gauges are set from that.
func (c *Component) refreshReadinessMetrics(ctx context.Context) {
	if c.metrics == nil {
		return
	}
	c.metrics.setReadinessGauges(c.computeIndexStatus(ctx))
}

// repairFailedEntities enqueues reconciliation keys into the same ordered lanes as
// watcher updates and deletes. The authoritative KV value is fetched only when the
// key reaches the head of its lane; repair therefore cannot submit a stale snapshot
// that later clobbers a newer watcher operation.
func (c *Component) repairFailedEntities(ctx context.Context) {
	if c.failedCount.Load() == 0 || c.entityStatesBucket == nil {
		return
	}
	c.failedEntities.Range(func(k, _ any) bool {
		if ctx.Err() != nil {
			return false
		}
		entityID, ok := k.(string)
		if !ok {
			return true
		}
		if err := c.submitEntityWork(ctx, entityIndexWork{
			entityID: entityID,
		}); err != nil {
			c.logger.Debug("repair: ordered submit failed",
				slog.String("entity", entityID), slog.Any("error", err))
		}
		return true
	})
}

func (c *Component) reconcileEntity(ctx context.Context, entityID string) bool {
	if err := validateEntityLiteralKey(entityID); err != nil {
		c.markGraphStateResetRequired(string(graph.GraphStateReasonNoncanonicalEntityID))
		c.markEntityFailed(entityID)
		c.logger.Error("authoritative entity key is noncanonical; graph reset required",
			slog.String("entity", entityID), slog.Any("error", err))
		// Unlike a transient partial write, this revision has no safe owner filter and
		// can never be applied. Keep it pending so the watermark cannot advertise that
		// the malformed authoritative delete/update was processed.
		return false
	}
	entry, err := c.entityStatesBucket.Get(ctx, entityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			if delErr := c.DeleteFromIndexes(ctx, entityID); delErr != nil {
				c.logger.Debug("reconcile: delete still failing",
					slog.String("entity", entityID), slog.Any("error", delErr))
			}
			return true
		}
		c.markEntityFailed(entityID)
		c.logger.Warn("reconcile: authoritative entity read failed; readiness withheld",
			slog.String("entity", entityID), slog.Any("error", err))
		return true
	}
	if err := c.processEntityUpdateFromData(ctx, entityID, entry.Value()); err != nil {
		c.logger.Debug("reconcile: re-index still failing",
			slog.String("entity", entityID), slog.Any("error", err))
		return !errors.Is(err, errAuthoritativeIdentityMismatch)
	}
	return true
}

func (c *Component) processEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	completionAllowed := c.processEntityUpdateResult(ctx, entry)
	if completionAllowed && c.watermark != nil {
		c.watermark.Complete(entry.Key(), entry.Revision())
	}
}

// processEntityUpdateResult applies one captured watcher snapshot. Ordered
// production work completes its watermark in processEntityWork; the wrapper above
// retains direct-call completion semantics for unit callers.
func (c *Component) processEntityUpdateResult(ctx context.Context, entry jetstream.KeyValueEntry) bool {
	if err := c.processEntityUpdateFromData(ctx, entry.Key(), entry.Value()); err != nil {
		// The failedCount side-effect (inside processEntityUpdateFromData) already
		// withheld readiness; log for operators. Completion below still fires — a
		// stranded revision would pin the watermark forever, and readiness is gated on
		// failedCount, not on completing this revision (ADR-066 §1 stays intact).
		c.logger.Warn("entity index write failed; readiness withheld until re-index",
			slog.String("entity", entry.Key()), slog.Any("error", err))
		return !errors.Is(err, errAuthoritativeIdentityMismatch)
	}
	return true
}

// indexWriteMaxAttempts bounds the in-place retry of an entity's required index
// writes (gh#474 P1b). Writes are idempotent, so retrying the whole set is safe;
// this recovers a transient KV blip so it does not mark the entity failed (and
// withhold readiness) unnecessarily.
const indexWriteMaxAttempts = 3

type aliasIndexWrite struct {
	alias string
}

type entityIndexPlan struct {
	entityID         string
	outgoingTargets  []map[string]interface{}
	predicatesUsed   map[string]bool
	incomingByTarget map[string][]graph.IncomingEntry
	aliasWrites      []aliasIndexWrite
	nameWrites       []nameIndexWrite
	contextTriples   []message.Triple
	indexed          int
}

// buildEntityIndexPlan collects and validates the derived-index candidate without
// lifecycle or store I/O. It may emit diagnostics when an optional row is skipped;
// every required row is validated before the caller applies the plan.
func (c *Component) buildEntityIndexPlan(state graph.EntityState, entityID string) (entityIndexPlan, error) {
	plan := entityIndexPlan{
		entityID:         entityID,
		outgoingTargets:  make([]map[string]interface{}, 0),
		predicatesUsed:   make(map[string]bool),
		incomingByTarget: make(map[string][]graph.IncomingEntry),
		contextTriples:   state.Triples,
	}
	for _, triple := range state.Triples {
		plan.predicatesUsed[triple.Predicate] = true
		if triple.IsRelationship() {
			targetID, _ := triple.Object.(string)
			plan.outgoingTargets = append(plan.outgoingTargets, map[string]interface{}{
				"id": targetID, "predicate": triple.Predicate,
			})
			plan.incomingByTarget[targetID] = append(plan.incomingByTarget[targetID], graph.IncomingEntry{
				FromEntityID: entityID, Predicate: triple.Predicate,
			})
			plan.indexed++
		}
		_, isVocabAlias := c.aliasPredicates[triple.Predicate]
		if isVocabAlias || triple.Predicate == "core.identity.alias" {
			if alias, ok := triple.Object.(string); ok && alias != "" {
				if err := natsclient.ValidateKVLiteralKey(alias); err != nil {
					c.logger.Warn("alias index: KV-unsafe alias skipped",
						slog.String("entity_id", entityID),
						slog.String("predicate", triple.Predicate),
						slog.Any("error", err))
					continue
				}
				plan.aliasWrites = append(plan.aliasWrites, aliasIndexWrite{alias: alias})
			}
		}
		if priority, isName := c.namePredicates[triple.Predicate]; isName {
			if name, ok := triple.Object.(string); ok && name != "" {
				plan.nameWrites = append(plan.nameWrites, nameIndexWrite{
					name: name, predicate: triple.Predicate, priority: priority,
				})
			}
		}
	}

	for _, filter := range []string{
		incomingIndexSourceFilter(entityID), predicateIndexEntityFilter(entityID),
		nameIndexEntityFilter(entityID), contextIndexEntityFilter(entityID),
	} {
		if err := natsclient.ValidateKVWildcardFilter(filter); err != nil {
			return entityIndexPlan{}, err
		}
	}
	if err := validateKVPrefix(contextIndexEntityPrefix(entityID)); err != nil {
		return entityIndexPlan{}, err
	}
	for targetID, entries := range plan.incomingByTarget {
		if err := validateEntityLiteralKey(targetID); err != nil {
			return entityIndexPlan{}, err
		}
		for _, entry := range entries {
			if err := natsclient.ValidateKVLiteralKey(incomingIndexKey(targetID, entityID, entry.Predicate)); err != nil {
				return entityIndexPlan{}, err
			}
		}
	}
	for predicate := range plan.predicatesUsed {
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			return entityIndexPlan{}, err
		}
		if err := natsclient.ValidateKVLiteralKey(predicateIndexKey(predicate, entityID)); err != nil {
			return entityIndexPlan{}, err
		}
	}
	for _, write := range plan.nameWrites {
		if !validateNameKeyInputs(entityID, write.predicate, c.logger) {
			return entityIndexPlan{}, errs.WrapInvalid(errs.ErrInvalidData, "Component", "buildEntityIndexPlan", "invalid name membership")
		}
		if err := natsclient.ValidateKVLiteralKey(nameCompositeKey(nameIndexKey(write.name), entityID, write.predicate)); err != nil {
			return entityIndexPlan{}, err
		}
		if _, err := json.Marshal(nameCompositeValue{Name: write.name, Priority: write.priority}); err != nil {
			return entityIndexPlan{}, err
		}
	}
	for _, triple := range plan.contextTriples {
		if triple.Context == "" {
			continue
		}
		if !validateContextKeyInputs(entityID, triple.Predicate, c.logger) {
			return entityIndexPlan{}, errs.WrapInvalid(errs.ErrInvalidData, "Component", "buildEntityIndexPlan", "invalid context membership")
		}
		if err := natsclient.ValidateKVLiteralKey(contextIndexKey(entityID, contextHashHex(triple.Context), triple.Predicate)); err != nil {
			return entityIndexPlan{}, err
		}
		if _, err := json.Marshal(contextIndexValue{Context: triple.Context}); err != nil {
			return entityIndexPlan{}, err
		}
	}
	if _, err := json.Marshal(plan.outgoingTargets); err != nil {
		return entityIndexPlan{}, err
	}
	return plan, nil
}

func (c *Component) applyEntityIndexPlan(ctx context.Context, plan entityIndexPlan) error {
	var errList []error
	if err := c.reconcileIncomingIndex(ctx, plan.entityID, plan.incomingByTarget); err != nil {
		errList = append(errList, fmt.Errorf("incoming: %w", err))
	}
	if err := c.updateOutgoingIndexBatch(ctx, plan.entityID, plan.outgoingTargets); err != nil {
		errList = append(errList, fmt.Errorf("outgoing: %w", err))
	}
	if err := c.reconcilePredicateIndex(ctx, plan.entityID, plan.predicatesUsed); err != nil {
		errList = append(errList, fmt.Errorf("predicate: %w", err))
	}
	if err := c.UpdateContextIndex(ctx, plan.entityID, plan.contextTriples); err != nil {
		errList = append(errList, fmt.Errorf("context: %w", err))
	}
	// ALIAS_INDEX has no owner-complete axis and is intentionally outside
	// replacement reconciliation. Skipping an unsafe candidate does not retract a
	// previously stored alias for this entity; alias retirement remains explicit.
	for _, write := range plan.aliasWrites {
		if err := c.UpdateAliasIndex(ctx, write.alias, plan.entityID); err != nil {
			errList = append(errList, fmt.Errorf("alias[%s]: %w", write.alias, err))
		}
	}
	if err := c.reconcileNameIndex(ctx, plan.entityID, plan.nameWrites); err != nil {
		errList = append(errList, fmt.Errorf("name: %w", err))
	}
	return errors.Join(errList...)
}

// processEntityUpdateFromData indexes an entity's relationships from its triples using
// raw data. It is the core implementation used by both processEntityUpdate and
// processEntityBatch. Returns nil on success and an error for incompatible authoritative
// state or when one or more REQUIRED index writes ultimately fail after retry (gh#474
// P1b). Required-write failure marks the entity failed and withholds authoritative
// readiness until it re-indexes; incompatible state latches the reset-required contract.
func (c *Component) processEntityUpdateFromData(ctx context.Context, entityID string, data []byte) error {
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(data, &state); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.markGraphStateResetRequired(string(stateErr.Reason))
		} else {
			c.markGraphStateResetRequired(string(graph.GraphStateReasonUnreadableEntity))
		}
		return fmt.Errorf("ENTITY_STATES %q is incompatible; graph reset and canonical reingest required: %w", entityID, err)
	}
	if err := validateEntityLiteralKey(entityID); err != nil || state.ID != entityID {
		identityErr := err
		if identityErr == nil {
			identityErr = fmt.Errorf("ENTITY_STATES key %q does not match value id %q", entityID, state.ID)
		}
		c.markGraphStateResetRequired(string(graph.GraphStateReasonNoncanonicalEntityID))
		c.markEntityFailed(entityID)
		return fmt.Errorf("%w: %w", errAuthoritativeIdentityMismatch, &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalEntityID,
			Err:    identityErr,
		})
	}
	resolvedID := entityID
	plan, err := c.buildEntityIndexPlan(state, resolvedID)
	if err != nil {
		return err
	}

	if err := c.lifecycleReporter.ReportStage(ctx, "indexing"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "indexing"), slog.Any("error", err))
	}

	// Re-index no-op instrumentation (D6, design.md / gh#474): compute the index-input
	// projection and compare to the last-indexed one (OBSERVE ONLY). The baseline is
	// stored only AFTER a successful write below (P2b) — a failed projection must not
	// become the comparison baseline, or a later retry would be suppressed as a no-op.
	projection := computeIndexProjection(state, c.namePredicates, c.aliasPredicates)
	atomic.AddInt64(&c.reindexTotal, 1)
	prev, loaded := c.lastProjections.Load(resolvedID)
	unchanged := loaded && prev.(string) == projection
	if unchanged {
		atomic.AddInt64(&c.reindexUnchanged, 1)
	}
	if c.metrics != nil {
		c.metrics.recordReindex(unchanged)
	}

	// Bounded retry: writes are idempotent, so a transient KV blip is retried rather
	// than immediately marking the entity failed.
	writeErr := c.retryIndexWrites(ctx, func() error { return c.applyEntityIndexPlan(ctx, plan) })

	if writeErr != nil {
		// Required writes did not all land — withhold readiness until re-index (P1b).
		// Do NOT store the projection baseline, so the next delivery re-attempts rather
		// than being suppressed as a no-op.
		c.markEntityFailed(resolvedID)
		return errs.WrapTransient(writeErr, "Component", "processEntityUpdateFromData",
			fmt.Sprintf("index writes failed for %s after %d attempts", resolvedID, indexWriteMaxAttempts))
	}

	// Success — the entity is fully indexed. Clear any prior failure and record the
	// baseline for no-op detection.
	c.clearEntityFailed(resolvedID)
	c.lastProjections.Store(resolvedID, projection)
	if c.metrics != nil {
		// Record semantic completion once at the entity boundary. The idempotent
		// write closure may run more than once during retry, but those are physical
		// attempts rather than additional completed index updates.
		for _, indexType := range []string{"name", "predicate", "incoming", "context", "outgoing"} {
			c.metrics.recordIndexUpdate(indexType)
		}
	}

	c.logger.Debug("indexed entity",
		slog.String("entity", resolvedID),
		slog.Int("triples", len(state.Triples)),
		slog.Int("relationships", plan.indexed))

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())
	if c.metrics != nil {
		c.metrics.recordEventProcessed()
		c.metrics.recordWatchEvent("update")
	}
	return nil
}

// computeIndexProjection computes a canonical, sorted string representing the
// index-input projection for an entity state. The projection covers:
//   - relationship (predicate, targetID) pairs — driving INCOMING/OUTGOING writes
//   - the full distinct predicate set — driving PREDICATE_INDEX writes
//   - (namePredicate, name) pairs — driving NAME_INDEX writes
//   - (aliasPredicate, alias) pairs — driving ALIAS_INDEX writes (gh#474 P2b)
//   - (context, predicate) pairs — driving CONTEXT_INDEX writes (NOT raw object values)
//
// Two entities with the same projection will produce identical index writes on
// re-index. The projection is intentionally NOT excluding literal-only predicates
// (community.member_of is a literal but still a predicate-index membership; excluding
// it silently drops memberships from the no-op signal). It MUST cover every indexed
// axis, including alias values — otherwise an alias-only change reads as "unchanged"
// and miscounts the no-op rate (P2b).
func computeIndexProjection(state graph.EntityState, namePredicates, aliasPredicates map[string]int) string {
	parts := make([]string, 0, len(state.Triples)*3)
	predicateSeen := make(map[string]bool, len(state.Triples))

	for _, t := range state.Triples {
		// 1. Relationship (predicate, target) pairs
		if t.IsRelationship() {
			if targetID, ok := t.Object.(string); ok {
				parts = append(parts, "rel:"+t.Predicate+":"+targetID)
			}
		}
		// 2. Full distinct predicate set
		if !predicateSeen[t.Predicate] {
			predicateSeen[t.Predicate] = true
			parts = append(parts, "pred:"+t.Predicate)
		}
		// 3. (namePredicate, name) pairs
		if _, isName := namePredicates[t.Predicate]; isName {
			if name, ok := t.Object.(string); ok && name != "" {
				parts = append(parts, "name:"+t.Predicate+":"+name)
			}
		}
		// 4. (aliasPredicate, alias) pairs
		_, isVocabAlias := aliasPredicates[t.Predicate]
		if isVocabAlias || t.Predicate == "core.identity.alias" {
			if alias, ok := t.Object.(string); ok && alias != "" {
				parts = append(parts, "alias:"+t.Predicate+":"+alias)
			}
		}
		// 5. (context, predicate) pairs — NOT raw object values
		if t.Context != "" {
			parts = append(parts, "ctx:"+t.Context+":"+t.Predicate)
		}
	}

	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// processEntityBatch is the revision-aware coalescer callback. Each key is submitted
// as reconciliation work; the execution lane re-fetches current state only when the
// work reaches the head of the entity's FIFO.
func (c *Component) processEntityBatch(ctx context.Context, entities []coalescedEntity) {
	c.logger.Debug("processing coalesced entity batch", slog.Int("count", len(entities)))

	for _, entity := range entities {
		if ctx.Err() != nil {
			return
		}
		if err := c.submitEntityWork(ctx, entityIndexWork{
			entityID:           entity.entityID,
			completionRevision: entity.revision,
		}); err != nil {
			c.logger.Warn("failed to submit coalesced entity reconciliation",
				slog.String("entity", entity.entityID), slog.Any("error", err))
		}
	}
}

// updateOutgoingIndexBatch writes all outgoing relationships for an entity
func (c *Component) updateOutgoingIndexBatch(ctx context.Context, entityID string, targets []map[string]interface{}) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateOutgoingIndexBatch", "entity ID cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateOutgoingIndexBatch", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "updateOutgoingIndexBatch", "context cancelled")
	}
	if err := validateEntityLiteralKey(entityID); err != nil {
		return err
	}

	// Convert targets to graph.OutgoingEntry array (matching graph/indexmanager expected format)
	entries := make([]graph.OutgoingEntry, 0, len(targets))
	for _, target := range targets {
		targetID, _ := target["id"].(string)
		predicate, _ := target["predicate"].(string)
		if err := validateEntityLiteralKey(targetID); err != nil {
			return err
		}
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			return err
		}
		entries = append(entries, graph.OutgoingEntry{ToEntityID: targetID, Predicate: predicate})
	}

	// Serialize as raw array (matching graph/indexmanager expected format)
	data, err := json.Marshal(entries)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "updateOutgoingIndexBatch", "entry serialization")
	}

	// Store in KV bucket using entity ID as key
	_, putErr := c.outgoingBucket.Put(ctx, entityID, data)
	if c.metrics != nil {
		c.metrics.recordKVOperation("put", "outgoing")
	}
	if putErr != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(putErr, "Component", "updateOutgoingIndexBatch", "KV store")
	}

	// Update metrics
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())

	c.logger.Debug("outgoing index batch updated",
		slog.String("entity_id", entityID),
		slog.Int("target_count", len(entries)))

	return nil
}

// handleEntityDelete removes an entity from all indexes
func (c *Component) handleEntityDelete(ctx context.Context, entityID string) error {
	c.logger.Debug("removing entity from indexes", slog.String("entity", entityID))

	if err := c.DeleteFromIndexes(ctx, entityID); err != nil {
		c.logger.Warn("failed to delete entity from indexes",
			slog.String("entity", entityID),
			slog.Any("error", err))
		return err
	}
	return nil
}

// ============================================================================
// Index Update Operations
// ============================================================================

// UpdateOutgoingIndex updates the outgoing index for an entity relationship
func (c *Component) UpdateOutgoingIndex(ctx context.Context, entityID, targetID, predicate string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateOutgoingIndex", "entity ID cannot be empty")
	}
	if targetID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateOutgoingIndex", "target ID cannot be empty")
	}
	if predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateOutgoingIndex", "predicate cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateOutgoingIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateOutgoingIndex", "context cancelled")
	}
	if err := validateEntityLiteralKey(entityID); err != nil {
		return err
	}
	if err := validateEntityLiteralKey(targetID); err != nil {
		return err
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return errs.WrapInvalid(err, "Component", "UpdateOutgoingIndex", "invalid predicate")
	}

	// Read existing entries (raw array format matching graph/indexmanager)
	var entries []graph.OutgoingEntry
	existingEntry, err := c.outgoingBucket.Get(ctx, entityID)
	if err != nil && !natsclient.IsKVNotFoundError(err) {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateOutgoingIndex", "KV get")
	}

	if err == nil {
		// Parse existing array
		if unmarshalErr := json.Unmarshal(existingEntry.Value, &entries); unmarshalErr != nil {
			// If unmarshal fails, start fresh (backward compatibility with old format)
			entries = []graph.OutgoingEntry{}
		}
	}

	// Check if this target already exists (avoid duplicates)
	targetExists := false
	for _, entry := range entries {
		if entry.ToEntityID == targetID && entry.Predicate == predicate {
			targetExists = true
			break
		}
	}

	// Append new target if it doesn't exist
	if !targetExists {
		entries = append(entries, graph.OutgoingEntry{
			ToEntityID: targetID,
			Predicate:  predicate,
		})
	}

	// Serialize array (matching graph/indexmanager expected format)
	data, err := json.Marshal(entries)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateOutgoingIndex", "entry serialization")
	}

	// Store in KV bucket using entity ID as key
	if _, err := c.outgoingBucket.Put(ctx, entityID, data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateOutgoingIndex", "KV store")
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())

	c.logger.Debug("outgoing index updated",
		slog.String("entity_id", entityID),
		slog.String("target_id", targetID),
		slog.String("predicate", predicate))

	return nil
}

// UpdateIncomingIndex updates the incoming index for a single relationship.
// Writes one composite-key entry at targetID.sourceID.predicate with an empty
// marker value (ADR-065 footgun comment: key uniqueness → CAS unnecessary).
func (c *Component) UpdateIncomingIndex(ctx context.Context, targetID, sourceID, predicate string) error {
	if targetID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateIncomingIndex", "target ID cannot be empty")
	}
	if sourceID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateIncomingIndex", "source ID cannot be empty")
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return errs.WrapInvalid(err, "Component", "UpdateIncomingIndex", "invalid predicate")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateIncomingIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateIncomingIndex", "context cancelled")
	}

	return c.updateIncomingIndexBatch(ctx, targetID, []graph.IncomingEntry{
		{FromEntityID: sourceID, Predicate: predicate},
	})
}

// updateIncomingIndexBatch writes one composite-key entry per incoming edge in
// newEntries. Replaces the old CAS read-modify-write approach (gh#474): each key
// encodes the full (targetID, sourceID, predicate) triple, so writes are O(edges),
// idempotent, and never contend (ADR-065 footgun comment in incoming_index.go).
func (c *Component) updateIncomingIndexBatch(ctx context.Context, targetID string, newEntries []graph.IncomingEntry) error {
	if targetID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateIncomingIndexBatch", "target ID cannot be empty")
	}
	if len(newEntries) == 0 {
		return nil
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateIncomingIndexBatch", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "updateIncomingIndexBatch", "context cancelled")
	}

	written := 0
	failures := 0
	for _, entry := range newEntries {
		if !validateIncomingKeyInputs(targetID, entry.FromEntityID, entry.Predicate, c.logger) {
			return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateIncomingIndexBatch",
				"incoming membership contains an invalid target, source, or predicate")
		}
		if err := natsclient.ValidateKVLiteralKey(incomingIndexKey(targetID, entry.FromEntityID, entry.Predicate)); err != nil {
			return err
		}
	}
	for _, entry := range newEntries {
		key := incomingIndexKey(targetID, entry.FromEntityID, entry.Predicate)
		if _, err := c.incomingBucket.Put(ctx, key, incomingIndexMarker); err != nil {
			atomic.AddInt64(&c.errors, 1)
			failures++
			c.logger.Debug("failed to write incoming index key",
				slog.String("key", key),
				slog.Any("error", err))
			continue
		}
		written++
	}

	// A failed edge write must propagate so readiness is withheld (gh#474 P1b) — a
	// silently-dropped incoming edge is exactly the missing-adjacency this closes.
	if failures > 0 {
		return errs.WrapTransient(errIndexWritePartial, "Component", "updateIncomingIndexBatch",
			fmt.Sprintf("%d of %d incoming edge writes failed for target %s", failures, len(newEntries), targetID))
	}

	if written == 0 {
		return nil
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	// Record Prometheus metrics
	if c.metrics != nil {
		c.metrics.recordIndexUpdate("incoming")
		c.metrics.recordKVOperation("put", "incoming")
	}

	c.logger.Debug("incoming index batch updated",
		slog.String("target_id", targetID),
		slog.Int("written_entries", written))

	return nil
}

// UpdateAliasIndex updates the alias index for an entity
func (c *Component) UpdateAliasIndex(ctx context.Context, alias, entityID string) error {
	if alias == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateAliasIndex", "alias cannot be empty")
	}
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateAliasIndex", "entity ID cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateAliasIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateAliasIndex", "context cancelled")
	}
	if err := validateEntityLiteralKey(entityID); err != nil {
		return err
	}
	if err := natsclient.ValidateKVLiteralKey(alias); err != nil {
		return err
	}

	// Store alias mapping (value is just the entity ID as string)
	if _, err := c.aliasBucket.Put(ctx, alias, []byte(entityID)); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateAliasIndex", "KV store")
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(entityID)))
	c.lastActivity.Store(time.Now())

	// Record Prometheus metrics
	if c.metrics != nil {
		c.metrics.recordIndexUpdate("alias")
		c.metrics.recordKVOperation("put", "alias")
	}

	c.logger.Debug("alias index updated",
		slog.String("alias", alias),
		slog.String("entity_id", entityID))

	return nil
}

// UpdatePredicateIndex updates the predicate index for an entity
func (c *Component) UpdatePredicateIndex(ctx context.Context, entityID, predicate string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdatePredicateIndex", "entity ID cannot be empty")
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return errs.WrapInvalid(err, "Component", "UpdatePredicateIndex", "invalid predicate")
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return err
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdatePredicateIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdatePredicateIndex", "context cancelled")
	}

	// Unconditional Put, no CAS (ADR-065). The key already encodes full
	// membership identity (predicate3 + entity6), so no two entities
	// ever write the same key — there is nothing a concurrent writer could
	// clobber. If this bucket's writes ever need CAS again, that means the
	// key-uniqueness invariant above no longer holds; don't "fix" this back
	// to UpdateWithRetry without re-establishing it (e.g. gh#433's
	// entity-delete GC will introduce a Delete against this same key from a
	// different code path, which needs its own ordering analysis).
	key := predicateIndexKey(predicate, entityID)
	if err := natsclient.ValidateKVLiteralKey(key); err != nil {
		return err
	}
	if _, err := c.predicateBucket.Put(ctx, key, predicateIndexMarker); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdatePredicateIndex", "KV store")
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	// Record Prometheus metrics
	if c.metrics != nil {
		c.metrics.recordIndexUpdate("predicate")
		c.metrics.recordKVOperation("put", "predicate")
	}

	c.logger.Debug("predicate index updated",
		slog.String("entity_id", entityID),
		slog.String("predicate", predicate))

	return nil
}

// ============================================================================
// Index Deletion Operations
// ============================================================================

// DeleteFromIndexes deletes an entity from all indexes.
//
// PREDICATE, NAME, CONTEXT, and OUTGOING memberships are owned by the entity.
// INCOMING memberships are owned by their source entity, even though their physical
// key begins with the target. Deletion therefore uses the fixed-position source
// filter and deliberately preserves assertions from live sources that still point
// at the retired target. ALIAS still has no owner-key axis.
func (c *Component) DeleteFromIndexes(ctx context.Context, entityID string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromIndexes", "entity ID cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromIndexes", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteFromIndexes", "context cancelled")
	}
	// Validate the complete owner before the first Delete/List call. Besides rejecting
	// malformed IDs, this pins the wildcard-filter byte/token budgets before any
	// replacement operation can retract a live row.
	if err := validateEntityLiteralKey(entityID); err != nil {
		return err
	}
	outgoingKey := entityID
	incomingFilter := incomingIndexSourceFilter(entityID)
	predicateFilter := predicateIndexEntityFilter(entityID)
	nameFilter := nameIndexEntityFilter(entityID)
	contextPrefix := contextIndexEntityPrefix(entityID)
	for _, ownerFilter := range []string{incomingFilter, predicateFilter, nameFilter} {
		if err := natsclient.ValidateKVWildcardFilter(ownerFilter); err != nil {
			return err
		}
	}
	if err := validateKVPrefix(contextPrefix); err != nil {
		return err
	}

	// Resolve and validate every owned physical key before the first Delete. A
	// failed late list or a poisoned key must not leave an earlier family already
	// retracted; retries always begin from the same complete candidate plan.
	type deleteSet struct {
		name   string
		bucket *natsclient.KVStore
		filter string
		prefix bool
		keys   []string
	}
	deleteSets := []deleteSet{
		{name: "incoming", bucket: c.incomingBucket, filter: incomingFilter},
		{name: "predicate", bucket: c.predicateBucket, filter: predicateFilter},
		{name: "name", bucket: c.nameBucket, filter: nameFilter},
		{name: "context", bucket: c.contextBucket, filter: contextPrefix, prefix: true},
	}
	preflightFailures := 0
	for i := range deleteSets {
		set := &deleteSets[i]
		var listErr error
		if set.prefix {
			set.keys, listErr = set.bucket.KeysByPrefix(ctx, set.filter)
		} else {
			set.keys, listErr = set.bucket.KeysByFilter(ctx, set.filter)
		}
		if c.metrics != nil {
			c.metrics.recordKVOperation("list", set.name)
			c.metrics.recordReconcileOperation(set.name, "list", listErr)
		}
		if listErr != nil {
			atomic.AddInt64(&c.errors, 1)
			preflightFailures++
			c.logger.Warn("failed to list owned index rows for delete",
				slog.String("index", set.name), slog.String("entity_id", entityID), slog.Any("error", listErr))
			continue
		}
		set.keys = uniqueSortedStrings(set.keys)
		for _, key := range set.keys {
			if keyErr := natsclient.ValidateKVLiteralKey(key); keyErr != nil {
				preflightFailures++
				c.logger.Warn("invalid owned index key returned for delete",
					slog.String("index", set.name), slog.String("key", key), slog.Any("error", keyErr))
			}
		}
	}
	if preflightFailures > 0 {
		c.markEntityFailed(entityID)
		return errs.WrapTransient(errIndexWritePartial, "Component", "DeleteFromIndexes",
			fmt.Sprintf("%d delete-plan operations failed for %s (no rows were deleted)", preflightFailures, entityID))
	}

	failures := 0
	// Delete from outgoing index (single key, entity-as-owner format unchanged).
	outgoingDeleteErr := c.outgoingBucket.Delete(ctx, outgoingKey)
	if c.metrics != nil {
		c.metrics.recordKVOperation("delete", "outgoing")
	}
	if outgoingDeleteErr != nil && !natsclient.IsKVNotFoundError(outgoingDeleteErr) {
		atomic.AddInt64(&c.errors, 1)
		failures++
		c.logger.Warn("failed to delete from outgoing index",
			slog.String("entity_id", entityID), slog.Any("error", outgoingDeleteErr))
	}
	for i := range deleteSets {
		set := &deleteSets[i]
		for _, key := range set.keys {
			delErr := set.bucket.Delete(ctx, key)
			if natsclient.IsKVNotFoundError(delErr) {
				delErr = nil
			}
			if c.metrics != nil {
				c.metrics.recordKVOperation("delete", set.name)
				c.metrics.recordReconcileOperation(set.name, "delete", delErr)
			}
			if delErr != nil {
				atomic.AddInt64(&c.errors, 1)
				failures++
				c.logger.Warn("failed to delete owned index row",
					slog.String("index", set.name), slog.String("key", key), slog.Any("error", delErr))
			}
		}
	}

	// A failed list/delete means stale rows may remain — the index is known-incomplete,
	// so withhold readiness (mark failed, gh#474 Codex #4) and do NOT clear the projection
	// cache or the failure marker. The repair loop re-runs the delete until it succeeds.
	if failures > 0 {
		c.markEntityFailed(entityID)
		return errs.WrapTransient(errIndexWritePartial, "Component", "DeleteFromIndexes",
			fmt.Sprintf("%d delete/list operations failed for %s (stale index rows may remain)", failures, entityID))
	}

	// Clean delete — evict the no-op projection cache (a stale projection on re-add would
	// suppress a real first-index event) and clear any prior write/delete failure.
	c.lastProjections.Delete(entityID)
	c.clearEntityFailed(entityID)
	if c.metrics != nil {
		for _, indexType := range []string{"name", "predicate", "incoming", "context", "outgoing"} {
			c.metrics.recordIndexUpdate(indexType)
		}
	}

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())
	if c.metrics != nil {
		c.metrics.recordEventProcessed()
		c.metrics.recordWatchEvent("delete")
	}
	c.logger.Debug("entity deleted from indexes", slog.String("entity_id", entityID))
	return nil
}

// DeleteFromPredicateIndex removes one entity's membership in one
// predicate. Not currently called from the production entity-delete path
// (DeleteFromIndexes) — see gh#433: a KV-watch delete event carries only
// the deleted key, not the entity's former triples, so the caller doesn't
// yet know which predicates to clean up. Exists for callers that already
// know the predicate (tests today, gh#433's eventual fix).
func (c *Component) DeleteFromPredicateIndex(ctx context.Context, entityID, predicate string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromPredicateIndex", "entity ID cannot be empty")
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return errs.WrapInvalid(err, "Component", "DeleteFromPredicateIndex", "invalid predicate")
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return err
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromPredicateIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteFromPredicateIndex", "context cancelled")
	}

	// Delete only this entity's membership in this predicate — a single
	// composite-key delete (ADR-065), not the whole predicate's membership.
	key := predicateIndexKey(predicate, entityID)
	if err := natsclient.ValidateKVLiteralKey(key); err != nil {
		return err
	}
	if err := c.predicateBucket.Delete(ctx, key); err != nil && !natsclient.IsKVNotFoundError(err) {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "DeleteFromPredicateIndex", "KV delete")
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	c.logger.Debug("predicate index entry deleted",
		slog.String("entity_id", entityID),
		slog.String("predicate", predicate))

	return nil
}

// DeleteFromAliasIndex deletes an alias from the alias index
func (c *Component) DeleteFromAliasIndex(ctx context.Context, alias string) error {
	if alias == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromAliasIndex", "alias cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromAliasIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteFromAliasIndex", "context cancelled")
	}
	if err := natsclient.ValidateKVLiteralKey(alias); err != nil {
		return err
	}

	// Delete from alias index
	if err := c.aliasBucket.Delete(ctx, alias); err != nil && !natsclient.IsKVNotFoundError(err) {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "DeleteFromAliasIndex", "KV delete")
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	c.logger.Debug("alias index entry deleted", slog.String("alias", alias))

	return nil
}

// ============================================================================
// Context Index Operations (Triple Provenance Tracking)
// ============================================================================

// UpdateContextIndex updates the context index for triples with a context value.
// This enables provenance queries like "all triples from hierarchy inference".
//
// After composite-key sharding (gh#474): one unconditional Put per (entityID,
// predicate, contextValue) triple, keyed as hash(contextValue).entityID.predicate.
// This replaces the former non-CAS Get+Put of a list, which had a lost-update
// race AND used the raw context value as the key (collision-prone for dotted
// hierarchies like "inference.hierarchy" vs "inference.hierarchy.deep" — ADR-065).
//
// CONTEXT_INDEX has no production reader (design.md D2, no query consumer);
// the write path is the sole migration target. e2e readers are in scope for
// task 5 (the breaking-change gate), not here.
func (c *Component) UpdateContextIndex(ctx context.Context, entityID string, triples []message.Triple) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateContextIndex", "entity ID cannot be empty")
	}

	// Check context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateContextIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateContextIndex", "context cancelled")
	}
	if err := validateEntityLiteralKey(entityID); err != nil {
		return err
	}
	prefix := contextIndexEntityPrefix(entityID)
	if err := validateKVPrefix(prefix); err != nil {
		return err
	}

	// Build the entity's DESIRED current context memberships (gh#474 P1f: keys are
	// entity-prefixed). Marshal failure is a bug, not a transient — count it.
	desired := make(map[string][]byte, len(triples))
	for _, t := range triples {
		if t.Context == "" {
			continue
		}
		if !validateContextKeyInputs(entityID, t.Predicate, c.logger) {
			return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateContextIndex",
				"context membership contains an invalid entity ID or predicate")
		}
		key := contextIndexKey(entityID, contextHashHex(t.Context), t.Predicate)
		if err := natsclient.ValidateKVLiteralKey(key); err != nil {
			return err
		}
		value, marshalErr := json.Marshal(contextIndexValue{Context: t.Context})
		if marshalErr != nil {
			c.logger.Debug("context index: failed to marshal value",
				slog.String("context", t.Context),
				slog.Any("error", marshalErr))
			continue
		}
		desired[key] = value
	}

	// CONTEXT_INDEX already has entityID in the leading key position, so its
	// shipped prefix reconciliation remains the production path. The generic
	// filtered-owner candidate stays in the fixed-arity spike until the
	// registered real-NATS benchmark selects it.
	existing, listErr := c.contextBucket.KeysByPrefix(ctx, prefix)
	if c.metrics != nil {
		c.metrics.recordKVOperation("list", "context")
	}
	if listErr != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.WrapTransient(listErr, "Component", "UpdateContextIndex", "list existing context keys")
	}
	for _, key := range existing {
		if err := natsclient.ValidateKVLiteralKey(key); err != nil {
			return err
		}
	}

	var failures int
	for _, key := range existing {
		if _, keep := desired[key]; keep {
			continue
		}
		delErr := c.contextBucket.Delete(ctx, key)
		if c.metrics != nil {
			c.metrics.recordKVOperation("delete", "context")
		}
		if delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
			atomic.AddInt64(&c.errors, 1)
			failures++
			c.logger.Debug("context index: failed to retract superseded entry",
				slog.String("key", key), slog.Any("error", delErr))
		}
	}
	for key, value := range desired {
		_, putErr := c.contextBucket.Put(ctx, key, value)
		if c.metrics != nil {
			c.metrics.recordKVOperation("put", "context")
		}
		if putErr != nil {
			atomic.AddInt64(&c.errors, 1)
			failures++
			c.logger.Debug("context index: failed to write entry",
				slog.String("key", key), slog.Any("error", putErr))
		}
	}
	if failures > 0 {
		return errs.WrapTransient(errIndexWritePartial, "Component", "UpdateContextIndex",
			fmt.Sprintf("%d of %d context writes/retractions failed for %s", failures, len(desired)+len(existing), entityID))
	}

	return nil
}
