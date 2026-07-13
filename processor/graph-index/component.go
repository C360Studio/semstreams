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
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/c360studio/semstreams/pkg/worker"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

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
	outgoingBucket         *natsclient.KVStore
	incomingBucket         *natsclient.KVStore
	aliasBucket            *natsclient.KVStore
	predicateBucket        *natsclient.KVStore
	predicateCatalogBucket *natsclient.KVStore
	contextBucket          *natsclient.KVStore
	nameBucket             *natsclient.KVStore
	entityStatesBucket     jetstream.KeyValue // raw: read-only watcher, no CAS needed

	// Lifecycle state
	mu              sync.RWMutex
	running         bool
	initialized     bool
	startTime       time.Time
	wg              sync.WaitGroup
	cancel          context.CancelFunc
	indexPool       *worker.Pool[jetstream.KeyValueEntry]
	entityCoalescer *cache.CoalescingSet

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

	// Alias predicates from vocabulary (cached at startup for performance)
	aliasPredicates map[string]int

	// Label (display-name) predicates from vocabulary, cached at startup. Keys
	// the NAME_INDEX for graph.query.byName (gh#376); value = salience priority.
	namePredicates map[string]int

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
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

	// Optionally coalesce rapid entity updates to reduce KV write amplification
	if c.config.CoalesceMs > 0 {
		c.entityCoalescer = cache.NewCoalescingSet(ctx, time.Duration(c.config.CoalesceMs)*time.Millisecond, func(entityIDs []string) {
			c.processEntityBatch(ctx, entityIDs)
		})
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

	// Stop the entity coalescer before the worker pool so any pending keys are flushed
	if c.entityCoalescer != nil {
		_ = c.entityCoalescer.Close()
	}

	// Stop the index worker pool before cancelling the context so it can drain
	if c.indexPool != nil {
		if err := c.indexPool.Stop(timeout); err != nil {
			c.logger.Warn("index pool stop error", slog.Any("error", err))
		}
	}

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

	// Create PREDICATE_CATALOG bucket for predicate hash→name recovery
	// (ADR-065, gh#430). Internal like CONTEXT_INDEX/NAME_INDEX — not a
	// declared output port, so existing configs don't need to add it.
	predicateCatalogBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketPredicateCatalog,
		Description: "Predicate hash → name catalog for PREDICATE_INDEX",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "createOutputBuckets", fmt.Sprintf("KV bucket: %s", graph.BucketPredicateCatalog))
	}
	c.predicateCatalogBucket = c.natsClient.NewKVStore(predicateCatalogBucket)
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
				// nil entry indicates initial state enumeration complete
				c.logger.Debug("entity watcher initial sync complete")
				continue
			}

			// Record every delivered revision (update AND delete) as in-flight for
			// the readiness watermark before dispatch (ADR-066 §1). observedHigh
			// advances here; complete() drains on processing return / after delete.
			if c.watermark != nil {
				c.watermark.Observe(entry.Revision(), entry.Key())
			}

			if entry.Operation() == jetstream.KeyValueDelete {
				if c.entityCoalescer != nil {
					c.entityCoalescer.Remove(entry.Key())
				}
				c.handleEntityDelete(ctx, entry.Key())
				// Key-scoped completion: the tombstone's revision drains the delete
				// itself AND any earlier still-pending update for this key that the
				// delete supersedes (ADR-066 §1).
				if c.watermark != nil {
					c.watermark.Complete(entry.Key(), entry.Revision())
				}
				continue
			}

			if c.entityCoalescer != nil {
				c.entityCoalescer.Add(entry.Key())
			} else if c.indexPool != nil {
				if err := c.indexPool.SubmitBlocking(ctx, entry); err != nil {
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

	c.wg.Add(1)
	go c.watchEntityStates(ctx, c.entityStatesBucket)
	return nil
}

// startIndexPool creates and starts the entity index worker pool.
func (c *Component) startIndexPool(ctx context.Context) error {
	poolOpts := []worker.Option[jetstream.KeyValueEntry]{}
	if c.metricsRegistry != nil {
		poolOpts = append(poolOpts, worker.WithMetricsRegistry[jetstream.KeyValueEntry](c.metricsRegistry, "graph_index"))
	}
	c.indexPool = worker.NewPool[jetstream.KeyValueEntry](
		c.config.Workers,
		1000,
		c.processEntityUpdateWorker,
		poolOpts...,
	)
	if err := c.indexPool.Start(ctx); err != nil {
		return errs.Wrap(err, "Component", "Start", "start index worker pool")
	}
	return nil
}

// processEntityUpdateWorker is the worker pool adapter for processEntityUpdate.
func (c *Component) processEntityUpdateWorker(ctx context.Context, entry jetstream.KeyValueEntry) error {
	c.processEntityUpdate(ctx, entry)
	return nil
}

// processEntityUpdate indexes an entity's relationships from its triples.
// It is a thin wrapper around processEntityUpdateFromData for use with KV watcher
// entries. It is the single funnel for every update-completion path — the pool
// worker, the direct branch, and the coalescer batch all route through here — so it
// is the one place the readiness watermark's completion fires (ADR-066 §1).
func (c *Component) processEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	c.processEntityUpdateFromData(ctx, entry.Key(), entry.Value())
	// Completion fires on RETURN, not on indexing success: a malformed entry that
	// early-returns inside processEntityUpdateFromData must still complete or its
	// revision strands the watermark forever (ADR-066 §1 Scope boundary — Ready
	// means revision coverage, not per-index write success). Key-scoped <=rev drains
	// coalescer-collapsed lower revisions of this key that no worker sees alone.
	if c.watermark != nil {
		c.watermark.Complete(entry.Key(), entry.Revision())
	}
}

// processEntityUpdateFromData indexes an entity's relationships from its triples using raw data.
// It is the core implementation used by both processEntityUpdate and processEntityBatch.
func (c *Component) processEntityUpdateFromData(ctx context.Context, entityID string, data []byte) {
	// Report indexing stage (throttled to avoid KV spam)
	if err := c.lifecycleReporter.ReportStage(ctx, "indexing"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "indexing"), slog.Any("error", err))
	}

	var state graph.EntityState
	if err := json.Unmarshal(data, &state); err != nil {
		c.logger.Warn("failed to unmarshal entity state",
			slog.String("entity", entityID),
			slog.Any("error", err))
		return
	}

	// Determine entity ID: use state.ID if set, otherwise entityID arg, otherwise first triple subject
	resolvedID := state.ID
	if resolvedID == "" {
		resolvedID = entityID
	}
	if resolvedID == "" || resolvedID == "test-key" {
		// Fallback to triple subject for test compatibility
		if len(state.Triples) > 0 {
			resolvedID = state.Triples[0].Subject
		}
	}

	// Collect all outgoing relationships for this entity
	type RelationshipTarget struct {
		ID        string `json:"id"`
		Predicate string `json:"predicate"`
	}
	outgoingTargets := make([]map[string]interface{}, 0)

	// Collect predicates for this entity
	predicatesUsed := make(map[string]bool)

	// Track indexed relationships for this entity
	var indexed int

	// Collect incoming entries per target for per-edge writes
	incomingByTarget := make(map[string][]graph.IncomingEntry)

	// Index each triple
	for _, triple := range state.Triples {
		// Track predicate usage
		predicatesUsed[triple.Predicate] = true

		// Check if this is a relationship (object is an entity ID)
		if triple.IsRelationship() {
			targetID, _ := triple.Object.(string)

			// Collect outgoing relationship
			outgoingTargets = append(outgoingTargets, map[string]interface{}{
				"id":        targetID,
				"predicate": triple.Predicate,
			})

			// Collect incoming entry; will be written after the loop
			incomingByTarget[targetID] = append(incomingByTarget[targetID], graph.IncomingEntry{
				FromEntityID: resolvedID,
				Predicate:    triple.Predicate,
			})

			indexed++
		}

		// Check for alias predicate and index it
		// Supports both the canonical core.identity.alias predicate AND vocabulary-registered alias predicates
		_, isVocabAlias := c.aliasPredicates[triple.Predicate]
		isCoreAlias := triple.Predicate == "core.identity.alias"
		if isVocabAlias || isCoreAlias {
			if alias, ok := triple.Object.(string); ok && alias != "" {
				if err := c.UpdateAliasIndex(ctx, alias, resolvedID); err != nil {
					c.logger.Debug("failed to update alias index",
						slog.String("alias", alias),
						slog.String("entity", resolvedID),
						slog.String("predicate", triple.Predicate),
						slog.Any("error", err))
				}
			}
		}

		// Index display-name (label) predicates into NAME_INDEX for
		// graph.query.byName (gh#376). These are exactly the predicates the alias
		// index excludes (AliasTypeLabel).
		if priority, isName := c.namePredicates[triple.Predicate]; isName {
			if name, ok := triple.Object.(string); ok && name != "" {
				if err := c.UpdateNameIndex(ctx, name, resolvedID, triple.Predicate, priority); err != nil {
					c.logger.Debug("failed to update name index",
						slog.String("name", name),
						slog.String("entity", resolvedID),
						slog.String("predicate", triple.Predicate),
						slog.Any("error", err))
				}
			}
		}
	}

	// Re-index no-op instrumentation (D6, design.md / gh#474).
	// Compute the index-input projection for this entity: the set of facts that
	// actually drive index writes. Compare to the last-indexed projection; if
	// identical, increment the unchanged counter. OBSERVE ONLY — all writes proceed
	// regardless. This is the L2 change-detection data gate.
	projection := computeIndexProjection(state, c.namePredicates)
	atomic.AddInt64(&c.reindexTotal, 1)
	if prev, loaded := c.lastProjections.Load(resolvedID); loaded && prev.(string) == projection {
		atomic.AddInt64(&c.reindexUnchanged, 1)
	}
	c.lastProjections.Store(resolvedID, projection)

	// Write incoming index — one unconditional Put per edge (no CAS)
	for targetID, entries := range incomingByTarget {
		if err := c.updateIncomingIndexBatch(ctx, targetID, entries); err != nil {
			c.logger.Debug("failed to update incoming index",
				slog.String("target", targetID),
				slog.String("source", resolvedID),
				slog.Any("error", err))
		}
	}

	// Write outgoing index with all targets
	if len(outgoingTargets) > 0 {
		if err := c.updateOutgoingIndexBatch(ctx, resolvedID, outgoingTargets); err != nil {
			c.logger.Debug("failed to update outgoing index",
				slog.String("entity", resolvedID),
				slog.Any("error", err))
		}
	}

	// Update predicate index for all predicates used by this entity
	for predicate := range predicatesUsed {
		if err := c.UpdatePredicateIndex(ctx, resolvedID, predicate); err != nil {
			c.logger.Debug("failed to update predicate index",
				slog.String("entity", resolvedID),
				slog.String("predicate", predicate),
				slog.Any("error", err))
		}
	}

	// Update context index for triples with provenance (e.g., "inference.hierarchy")
	if err := c.UpdateContextIndex(ctx, resolvedID, state.Triples); err != nil {
		c.logger.Debug("failed to update context index",
			slog.String("entity", resolvedID),
			slog.Any("error", err))
	}

	c.logger.Debug("indexed entity",
		slog.String("entity", resolvedID),
		slog.Int("triples", len(state.Triples)),
		slog.Int("relationships", indexed))

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	// Record Prometheus metrics
	if c.metrics != nil {
		c.metrics.recordEventProcessed()
		c.metrics.recordWatchEvent("update")
	}
}

// computeIndexProjection computes a canonical, sorted string representing the
// index-input projection for an entity state. The projection covers:
//   - relationship (predicate, targetID) pairs — driving INCOMING/OUTGOING writes
//   - the full distinct predicate set — driving PREDICATE_INDEX writes
//   - (namePredicate, name) pairs — driving NAME_INDEX writes
//   - (context, predicate) pairs — driving CONTEXT_INDEX writes (NOT raw object values)
//
// Two entities with the same projection will produce identical index writes on
// re-index. The projection is intentionally NOT excluding literal-only predicates
// (community.member_of is a literal but still a predicate-index membership; excluding
// it silently drops memberships from the no-op signal).
func computeIndexProjection(state graph.EntityState, namePredicates map[string]int) string {
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
		// 4. (context, predicate) pairs — NOT raw object values
		if t.Context != "" {
			parts = append(parts, "ctx:"+t.Context+":"+t.Predicate)
		}
	}

	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// processEntityBatch is the CoalescingSet callback. It re-fetches each entity from KV
// by ID and routes it through the worker pool (or direct processing if no pool).
func (c *Component) processEntityBatch(ctx context.Context, entityIDs []string) {
	c.logger.Debug("processing coalesced entity batch", slog.Int("count", len(entityIDs)))

	for _, entityID := range entityIDs {
		if ctx.Err() != nil {
			return
		}

		entry, err := c.entityStatesBucket.Get(ctx, entityID)
		if err != nil {
			c.logger.Debug("entity not found during batch processing",
				slog.String("entity", entityID),
				slog.Any("error", err))
			// The coalescer discarded the source revision(s), so there is no exact
			// revision to complete here. Drain the key's pending (complete at max
			// revision) rather than strand it: a transient Get failure would
			// otherwise pin the watermark and flip the whole index to degraded
			// forever (ADR-066 §1). Draining fails toward caught-up (within the
			// Scope boundary) — the next update to this key re-observes it.
			if c.watermark != nil {
				c.watermark.Complete(entityID, ^uint64(0))
			}
			continue
		}

		if c.indexPool != nil {
			if err := c.indexPool.SubmitBlocking(ctx, entry); err != nil {
				c.logger.Warn("failed to submit coalesced entity for indexing",
					slog.String("entity", entityID),
					slog.Any("error", err))
			}
		} else {
			c.processEntityUpdate(ctx, entry)
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

	// Convert targets to graph.OutgoingEntry array (matching graph/indexmanager expected format)
	entries := make([]graph.OutgoingEntry, 0, len(targets))
	for _, target := range targets {
		targetID, _ := target["id"].(string)
		predicate, _ := target["predicate"].(string)
		if targetID != "" && predicate != "" {
			entries = append(entries, graph.OutgoingEntry{
				ToEntityID: targetID,
				Predicate:  predicate,
			})
		}
	}

	// Serialize as raw array (matching graph/indexmanager expected format)
	data, err := json.Marshal(entries)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "updateOutgoingIndexBatch", "entry serialization")
	}

	// Store in KV bucket using entity ID as key
	if _, err := c.outgoingBucket.Put(ctx, entityID, data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "updateOutgoingIndexBatch", "KV store")
	}

	// Update metrics
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())

	// Record Prometheus metrics
	if c.metrics != nil {
		c.metrics.recordIndexUpdate("outgoing")
		c.metrics.recordKVOperation("put", "outgoing")
	}

	c.logger.Debug("outgoing index batch updated",
		slog.String("entity_id", entityID),
		slog.Int("target_count", len(entries)))

	return nil
}

// handleEntityDelete removes an entity from all indexes
func (c *Component) handleEntityDelete(ctx context.Context, entityID string) {
	c.logger.Debug("removing entity from indexes", slog.String("entity", entityID))

	if err := c.DeleteFromIndexes(ctx, entityID); err != nil {
		c.logger.Warn("failed to delete entity from indexes",
			slog.String("entity", entityID),
			slog.Any("error", err))
	}
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
	if predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateIncomingIndex", "predicate cannot be empty")
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
	for _, entry := range newEntries {
		if !validateIncomingKeyInputs(targetID, entry.FromEntityID, entry.Predicate, c.logger) {
			continue
		}
		key := incomingIndexKey(targetID, entry.FromEntityID, entry.Predicate)
		if _, err := c.incomingBucket.Put(ctx, key, incomingIndexMarker); err != nil {
			atomic.AddInt64(&c.errors, 1)
			c.logger.Debug("failed to write incoming index key",
				slog.String("key", key),
				slog.Any("error", err))
			continue
		}
		written++
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
	if predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdatePredicateIndex", "predicate cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdatePredicateIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdatePredicateIndex", "context cancelled")
	}

	// Unconditional Put, no CAS (ADR-065). The key already encodes full
	// membership identity (hash(predicate) + entityID), so no two entities
	// ever write the same key — there is nothing a concurrent writer could
	// clobber. If this bucket's writes ever need CAS again, that means the
	// key-uniqueness invariant above no longer holds; don't "fix" this back
	// to UpdateWithRetry without re-establishing it (e.g. gh#433's
	// entity-delete GC will introduce a Delete against this same key from a
	// different code path, which needs its own ordering analysis).
	if _, err := c.predicateBucket.Put(ctx, predicateIndexKey(predicate, entityID), predicateIndexMarker); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdatePredicateIndex", "KV store")
	}

	// Membership write above is the source of truth and already succeeded;
	// a catalog-write failure here doesn't corrupt membership, but it does
	// silently drop this predicate from predicateList's output (both
	// unfiltered and namespace-filtered — both enumerate via the catalog,
	// not the membership bucket) until some other entity's write retries
	// the same catalog Put. That's a real, if narrow, visibility gap —
	// Warn + count it rather than the Debug-and-forget treatment a merely
	// cosmetic failure would get.
	if err := c.updatePredicateCatalog(ctx, predicate); err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Warn("failed to update predicate catalog",
			slog.String("predicate", predicate),
			slog.Any("error", err))
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
// INCOMING uses composite-key sharding (gh#474): the entity is the key PREFIX
// for its own incoming aggregate (all keys "entityID.sourceID.predicate"). A clean
// prefix scan enumerates the whole keyset; each key is deleted individually.
// This removes all incoming edges where this entity is the TARGET. Reciprocal
// cleanup (removing this entity from other entities' incoming entries, where it
// appears as the sourceID middle-token) is the pre-existing gh#433 gap — unchanged
// here (design.md D3).
//
// NAME and CONTEXT have no delete path today (never touched by this function) —
// adding one is gh#433, out of scope; nothing regresses.
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

	// Delete from outgoing index (single key, entity-as-owner format unchanged)
	if err := c.outgoingBucket.Delete(ctx, entityID); err != nil && !natsclient.IsKVNotFoundError(err) {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Warn("failed to delete from outgoing index", slog.String("entity_id", entityID), slog.Any("error", err))
	}

	// LEGACY HARD-DELETE — do NOT reuse for logical retirement (gh#474 Codex P1e,
	// gh#527). This removes every INCOMING row where entityID is the TARGET
	// ("entityID.sourceID.predicate"). Those rows are the SOURCES' evidence that they
	// still point at this entity — semantic support belongs to the source, not the
	// target — so deleting them on the target's death discards still-live assertions
	// and makes a later incoming-reference check falsely empty. That is acceptable only
	// for a full hard delete of a leaf entity; the retention increment (#527) replaces
	// this with source-owned retraction + a durable reverse manifest. The reciprocal
	// rows (this entity as a mid-key SOURCE on other targets) are not reachable by a
	// bare-key delete and are left for #527.
	incomingKeys, err := c.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(entityID))
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Warn("failed to list incoming index keys for delete",
			slog.String("entity_id", entityID),
			slog.Any("error", err))
	} else {
		for _, key := range incomingKeys {
			if delErr := c.incomingBucket.Delete(ctx, key); delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
				atomic.AddInt64(&c.errors, 1)
				c.logger.Warn("failed to delete incoming index key",
					slog.String("key", key),
					slog.Any("error", delErr))
			}
		}
	}

	// Delete from context index — entity-as-prefix (gh#474 P1f): enumerate the
	// entity's own composite keys "entityID.hash(context).hex(predicate)" and delete
	// each. Unlike INCOMING, these rows ARE owned by this entity (its own provenance
	// memberships), so removing them on its death is correct, not legacy behavior.
	contextKeys, ctxErr := c.contextBucket.KeysByPrefix(ctx, contextIndexEntityPrefix(entityID))
	if ctxErr != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Warn("failed to list context index keys for delete",
			slog.String("entity_id", entityID),
			slog.Any("error", ctxErr))
	} else {
		for _, key := range contextKeys {
			if delErr := c.contextBucket.Delete(ctx, key); delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
				atomic.AddInt64(&c.errors, 1)
				c.logger.Warn("failed to delete context index key",
					slog.String("key", key),
					slog.Any("error", delErr))
			}
		}
	}

	// Also evict from the no-op projection cache (stale projection on re-add
	// would incorrectly suppress a real first-index event).
	c.lastProjections.Delete(entityID)

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

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
	if predicate == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromPredicateIndex", "predicate cannot be empty")
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
	if err := c.predicateBucket.Delete(ctx, predicateIndexKey(predicate, entityID)); err != nil && !natsclient.IsKVNotFoundError(err) {
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

// DeleteFromIncomingIndex deletes all incoming edges from sourceID to targetID
// (all predicates). Under composite-key sharding, this is a prefix scan for
// "targetID.sourceID." + delete-each.
func (c *Component) DeleteFromIncomingIndex(ctx context.Context, targetID, sourceID string) error {
	if targetID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromIncomingIndex", "target ID cannot be empty")
	}
	if sourceID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromIncomingIndex", "source ID cannot be empty")
	}

	// Check context - nil check first to prevent panic
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "DeleteFromIncomingIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteFromIncomingIndex", "context cancelled")
	}

	// Prefix: all edges from sourceID to targetID (all predicates)
	prefix := incomingIndexPrefix(targetID) + sourceID + "."
	keys, err := c.incomingBucket.KeysByPrefix(ctx, prefix)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "DeleteFromIncomingIndex", "list keys")
	}

	for _, key := range keys {
		if delErr := c.incomingBucket.Delete(ctx, key); delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
			atomic.AddInt64(&c.errors, 1)
			c.logger.Warn("failed to delete incoming index key",
				slog.String("key", key),
				slog.Any("error", delErr))
		}
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	c.logger.Debug("incoming index entries deleted",
		slog.String("target_id", targetID),
		slog.String("source_id", sourceID),
		slog.Int("deleted_count", len(keys)))

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

	// Build the entity's DESIRED current context memberships (gh#474 P1f: keys are
	// entity-prefixed). Marshal failure is a bug, not a transient — count it.
	desired := make(map[string][]byte, len(triples))
	for _, t := range triples {
		if t.Context == "" {
			continue
		}
		if !validateContextKeyInputs(entityID, t.Predicate, c.logger) {
			continue
		}
		key := contextIndexKey(entityID, contextHashHex(t.Context), t.Predicate)
		value, marshalErr := json.Marshal(contextIndexValue{Context: t.Context})
		if marshalErr != nil {
			c.logger.Debug("context index: failed to marshal value",
				slog.String("context", t.Context),
				slog.Any("error", marshalErr))
			continue
		}
		desired[key] = value
	}

	// Reconcile against what the entity currently owns: prefix-scan "entityID.",
	// RETRACT superseded keys, then Put the desired set (P1f — this is why per-key
	// Puts alone leaked; the entity prefix makes the retraction findable). Failures
	// are aggregated and returned so the caller can withhold readiness (P1b).
	existing, listErr := c.contextBucket.KeysByPrefix(ctx, contextIndexEntityPrefix(entityID))
	if listErr != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.WrapTransient(listErr, "Component", "UpdateContextIndex", "list existing context keys")
	}

	var failures int
	for _, key := range existing {
		if _, keep := desired[key]; keep {
			continue // still current — leave it (idempotent)
		}
		if delErr := c.contextBucket.Delete(ctx, key); delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
			atomic.AddInt64(&c.errors, 1)
			failures++
			c.logger.Debug("context index: failed to retract superseded entry",
				slog.String("key", key), slog.Any("error", delErr))
		}
	}
	for key, value := range desired {
		if _, putErr := c.contextBucket.Put(ctx, key, value); putErr != nil {
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
