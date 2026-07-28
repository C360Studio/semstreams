// Package graphembedding provides the graph-embedding component for generating entity embeddings.
package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// failureInfo is the per-entity value of the current-failed map (#613): the bounded
// reason of the entity's current failed embedding, the time it was first observed failed
// in this process lifetime, and the source revision the failure was recorded at. reason
// feeds the envelope's failed_reasons histogram; at feeds first_failure_at (the minimum
// across the map). On a re-failure the reason is updated but at is preserved, so
// first_failure_at stays the earliest.
//
// rev is the ENTITY_STATES source revision of the current failure. It makes the map
// revision-aware so a SUPERSEDED older terminal cannot mutate a newer failure (#613 F1):
// the durable EMBEDDING_INDEX record is ordered by the same revision (storage's revision
// CAS), so an older completion arriving after a newer failure — which storage correctly
// drops as superseded — must be a no-op here too, not an unconditional map clear.
type failureInfo struct {
	reason string
	at     time.Time
	rev    uint64
}

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Config holds configuration for graph-embedding component
type Config struct {
	Ports        *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	EmbedderType string                `json:"embedder_type" schema:"type:string,description:Embedder type (bm25 or http). HTTP requires model registry with embedding capability,category:basic"`
	BatchSize    int                   `json:"batch_size" schema:"type:int,description:Batch size for embedding generation,category:advanced"`

	// Workers is the number of concurrent embedding worker goroutines.
	//
	// This used to be derived as batch_size/10, which yielded ZERO workers for any
	// batch_size under 10 and left the component silently processing nothing
	// (gh#620). Worker count is its own operator knob because it is a concurrency
	// setting, not a batching one — no graph processor batches.
	Workers int `json:"workers,omitempty" schema:"type:int,description:Number of concurrent embedding worker goroutines,category:advanced"`

	// MaxTextLen is the source-text truncation cap (in characters) applied before
	// embedding. Text beyond it is truncated at a word boundary and the truncation is
	// counted (text_truncated_total), so the bytes actually embedded are discoverable
	// rather than silently dropped. The cap is part of what the vector depends on, so
	// it participates in the dedup key (#602): changing it re-embeds affected
	// entities. 0 selects a per-embedder-type default (4000 bm25 / 8000 neural).
	MaxTextLen int `json:"max_text_len,omitempty" schema:"type:int,description:Max characters of source text embedded per entity; text beyond is truncated at a word boundary. 0 uses a per-embedder default (4000 bm25 / 8000 neural),min:0,max:1000000,category:advanced"`

	// TextSuffixes controls which triple predicates are extracted for embedding.
	// Predicates ending with any of these suffixes will have their text values embedded.
	// When empty, defaults to: .title, .content, .description, .summary, .text, .name, .body, .abstract, .subject
	TextSuffixes []string `json:"text_suffixes,omitempty" schema:"type:array,description:Predicate suffixes to extract for embedding (e.g. .source_code .signature). Defaults to common text predicates,category:advanced"`

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
	// No output port is required: the component writes its durable results
	// (EMBEDDING_INDEX, EMBEDDING_DEDUP) directly at Start, not through a
	// declared output port.

	// Validate embedder type
	if c.EmbedderType == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "embedder_type required")
	}
	if c.EmbedderType != "bm25" && c.EmbedderType != "http" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "embedder_type must be 'bm25' or 'http'")
	}

	// Validate batch size
	if c.BatchSize < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "batch_size cannot be negative")
	}

	// Validate worker count. Reject rather than silently floor: an operator who
	// typed a negative worker count has a broken config, not a preference.
	if c.Workers < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "workers cannot be negative")
	}

	// Reject a negative cap for the same reason as Workers: a negative value is a
	// broken config, not a request for the default. 0 explicitly means "per-embedder
	// default"; a negative would otherwise silently fall through to it.
	if c.MaxTextLen < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "max_text_len cannot be negative")
	}

	// Upper-bound the cap. An unbounded value overflows the offloaded lane's byte
	// budget (utf8.UTFMax*limit+1) into a negative io.LimitReader bound — an empty body
	// that hop 2 reads as "no source text" and DELETES the pending embedding — and a
	// merely huge value permits a correspondingly huge io.ReadAll allocation.
	// MaxSourceTextLenCeiling (1_000_000 characters) is far past any real embedding
	// input (neural context caps are ~8k) while keeping the worst-case offloaded read
	// bounded (#628 FIX 2).
	if c.MaxTextLen > embedding.MaxSourceTextLenCeiling {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			fmt.Sprintf("max_text_len exceeds the maximum of %d characters", embedding.MaxSourceTextLenCeiling))
	}

	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	if c.EmbedderType == "" {
		c.EmbedderType = "bm25"
	}
	if c.BatchSize == 0 {
		c.BatchSize = 50
	}
	if c.Workers == 0 {
		c.Workers = defaultWorkers
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
	} else if len(c.Ports.Inputs) == 0 {
		// If ports exist but inputs are empty, populate with defaults. Outputs
		// stay as declared: the component requires none.
		c.Ports.Inputs = []component.PortDefinition{
			{
				Name:    "entity_watch",
				Type:    "kv-watch",
				Subject: graph.BucketEntityStates,
			},
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
				{
					Name:   "content_store",
					Type:   "store-read",
					Bucket: "MESSAGES",
				},
			},
		},
		EmbedderType: "bm25",
		BatchSize:    50,
		Workers:      defaultWorkers,
	}
}

// defaultWorkers matches embedding.NewWorker's own built-in default, so an
// unset `workers` behaves exactly as the worker package intends.
const defaultWorkers = 5

// Source-text truncation caps. BM25 feature hashing gets noisier with very long
// text; neural models have context limits. These are part of the dedup key
// (EmbedderIdentity.MaxTextLen) because switching embedder type flips the cap,
// and therefore changes the text that actually gets embedded.
const (
	maxSourceTextLenBM25   = 4000
	maxSourceTextLenNeural = 8000
)

// schema defines the configuration schema for graph-embedding component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the graph-embedding processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient    *natsclient.Client
	logger        *slog.Logger
	modelRegistry model.RegistryReader

	// Domain resources
	embedder     embedding.Embedder
	storage      *embedding.Storage
	worker       *embedding.Worker
	contentStore *objectstore.Store // ContentStorable access (owned lifecycle; fallback only)
	// storeRegistry is the shared {StorageInstance → store} resolver (ADR-063).
	// Primary content-fetch path: resolves a StorageRef against ANY registered
	// storage instance. Nil when the deployment wires no registry; the worker then
	// falls back to contentStore (single store-read bucket).
	storeRegistry *storeregistry.Registry
	// noContentStoreWarn fires the "offloaded content excluded, no content store
	// wired" warning exactly once (gh#414) — the per-entity metric carries the
	// count; the log stays a single actionable line rather than a flood.
	noContentStoreWarn sync.Once
	entityCoalescer    *cache.CoalescingSet
	entityStatesBucket jetstream.KeyValue

	// Lifecycle state
	mu          sync.RWMutex
	running     bool
	initialized bool
	startTime   time.Time
	wg          sync.WaitGroup
	cancel      context.CancelFunc

	// Metrics (atomic for internal tracking)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time

	// Prometheus metrics
	metrics *embeddingMetrics

	// Current-failed tracking (#613). failed maps entityID → the reason and first-seen
	// time of its CURRENT failed embedding; FailedCount = len(failed). A terminal Failed
	// outcome adds; every other terminal outcome removes. It is seeded at Start from the
	// durable EMBEDDING_INDEX and mutated on every terminal via completeEmbedding, so it
	// mirrors durable failed records net of regeneration/deletion. failedGauge and
	// failuresVec are PER-REGISTRY (register-or-get, no process-global singleton): the
	// gauge tracks len(failed); failuresVec is the reason-labelled cumulative counter the
	// worker increments through its metrics adapter.
	failedMu    sync.Mutex
	failed      map[string]failureInfo
	failedGauge prometheus.Gauge
	failuresVec *prometheus.CounterVec

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription

	// watermark is the ADR-066 §3 low-water-of-pending "caught up" tracker for the
	// two-hop embedding pipeline. hop-1 (this component's ENTITY_STATES watcher)
	// Observes every delivered revision; the terminal is Completed from hop-1
	// immediate skips (delete / no-text / ineligible / SavePending failure) AND the
	// hop-2 worker's onTerminal callback. Non-nil once Start wires the watcher.
	watermark            *revlag.Watermark
	embeddingCompletions atomic.Uint64 // total terminal completions (stuck-detector)
	resetState           atomic.Pointer[graph.StateContractError]
	watchUnavailable     atomic.Bool
	bootstrapStarted     atomic.Bool
	// bootstrapComplete means the ENTITY_STATES snapshot has been delivered and
	// validated. It gates queries (ensureBootstrapReady) and is deliberately NOT the
	// public wire bit: delivery is not application, and embedding generation is async.
	bootstrapComplete atomic.Bool
	// bootstrapTarget is the enumeration-time target — the highest revision delivered
	// when the initial-sync sentinel fired. Written BEFORE bootstrapComplete so any
	// reader that sees the flag also sees the target.
	//
	// It is a FIXED value, which is the point: comparing against the live stream
	// target would make the applied latch unreachable under continuous write, since
	// that target advances as fast as the pipeline does (gh#590 F1).
	bootstrapTarget atomic.Uint64
	// buildApplied is the public bootstrap_complete bit: the initial snapshot has not
	// merely been delivered, it has reached a TERMINAL embedding outcome up to the
	// enumeration-time target. Separate from bootstrapComplete because an unbounded
	// health consumer that trusted delivery would serve a partially built cold index.
	buildApplied atomic.Bool

	// Readiness stuck-detector state (ADR-066 §3), guarded by statusMu. Keyed off
	// COMPLETIONS, not IndexedRevision: a slow single external-LLM call can pin
	// Indexed for minutes while other workers complete higher revisions, which is
	// healthy, not stuck. Only a window with zero terminal completions is degraded.
	statusMu            sync.Mutex
	lastCompletionsSeen uint64
	lastProgressAt      time.Time

	// statusPublisher writes the readiness envelope to this producer's GRAPH_STATUS
	// key on every status tick (ADR-083). Non-nil once Start has created the bucket.
	statusPublisher *readiness.Publisher

	// statusInterval overrides the status heartbeat. It is a TEST SEAM only: the
	// production interval is pinned to readiness.DefaultHeartbeat because consumers
	// derive their freshness window from that same constant, so a configurable
	// producer cadence would silently mis-set every consumer's unknown threshold.
	statusInterval time.Duration
}

// CreateGraphEmbedding is the factory function for creating graph-embedding components
func CreateGraphEmbedding(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphEmbedding", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphEmbedding", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphEmbedding", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-embedding")

	// Create component
	comp := &Component{
		name:          "graph-embedding",
		config:        config,
		natsClient:    natsClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		metrics:       getMetrics(deps.MetricsRegistry),
		storeRegistry: deps.StoreRegistry, // ADR-063 shared resolver (may be nil)
		// Current-failed metrics resolved PER-REGISTRY (register-or-get), not through the
		// process-global getMetrics singleton (#613).
		failed:      make(map[string]failureInfo),
		failedGauge: resolveEmbeddingFailedGauge(deps.MetricsRegistry),
		failuresVec: resolveEmbeddingFailuresVec(deps.MetricsRegistry),
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-embedding factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-embedding", &component.Registration{
		Name:         "graph-embedding",
		Type:         "processor",
		Protocol:     "nats",
		Domain:       "graph",
		Description:  "Graph entity embedding generation processor",
		Version:      "1.0.0",
		Schema:       schema,
		Factory:      CreateGraphEmbedding,
		Dependencies: []string{component.DepModelRegistry},
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-embedding",
		Type:        "processor",
		Description: "Graph entity embedding generation processor",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ports := make([]component.Port, 0)
	hasStoreRead := false
	if c.config.Ports != nil {
		for _, portDef := range c.config.Ports.Inputs {
			p := component.BuildPortFromDefinition(portDef, component.DirectionInput)
			if _, ok := p.Config.(component.StoreReadPort); ok {
				hasStoreRead = true
			}
			ports = append(ports, p)
		}
	}

	// ADR-063: graph-embedding always consumes the shared store registry
	// (deps.StoreRegistry), resolving offloaded bodies from ANY registered
	// storage instance. Declare a federation store-read port for flowgraph
	// visibility when the config doesn't already wire one — advisory only, no
	// runtime effect (the worker resolves via the injected registry regardless).
	if !hasStoreRead {
		ports = append(ports, component.Port{
			Name:        "store-federation",
			Direction:   component.DirectionInput,
			Description: "Reads offloaded entity content from the store federation (ADR-063)",
			Config:      component.StoreReadPort{},
		})
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
			lastErr = graph.ErrorCodeGraphStateResetRequired + ": " + c.graphStateResetReason()
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
	c.logger.Info("component initialized", slog.String("component", "graph-embedding"))

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
		return errs.WrapFatal(errs.ErrInvalidConfig, "Component", "Start", "component not initialized")
	}

	// Idempotent - already running
	if c.running {
		return nil
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "context cancelled")
	}

	// Readiness status bucket (ADR-083). Created EAGERLY — first bucket touched
	// in Start, long before the status tick loop — so a consumer that binds its
	// watch the instant this component appears finds a bucket rather than
	// permanent status_unknown. Fatal on failure, like every other bucket this
	// component writes: it cannot Start without JetStream anyway, and a silently
	// absent status bucket would fail every downstream gate closed forever with
	// no producer-side evidence.
	if err := c.createStatusBucket(ctx); err != nil {
		cancel()
		return err
	}

	// Create embedder based on config
	if err := c.createEmbedder(); err != nil {
		cancel()
		return err
	}

	// Create embedding storage buckets
	embeddingIndexBucket, embeddingDedupBucket, err := c.createEmbeddingBuckets(ctx)
	if err != nil {
		cancel()
		return err
	}

	// Initialize lifecycle reporter
	c.initLifecycleReporter(ctx)

	// Readiness watermark (ADR-066 §3): must exist before the worker (whose
	// onTerminal completes it) and the watcher (which observes into it).
	c.watermark = revlag.New()

	// Create storage and worker
	if err := c.initStorageAndWorker(ctx, embeddingIndexBucket, embeddingDedupBucket); err != nil {
		cancel()
		return err
	}

	// Wait for ENTITY_STATES bucket and start entity watcher
	if err := c.waitForDependenciesAndStartWatcher(ctx); err != nil {
		cancel()
		return err
	}

	// Optionally wrap entity updates with coalescing to avoid redundant re-embedding
	if c.config.CoalesceMs > 0 {
		c.entityCoalescer = cache.NewCoalescingSet(ctx, time.Duration(c.config.CoalesceMs)*time.Millisecond, func(entityIDs []string) {
			c.processEntityBatch(ctx, entityIDs)
		})
	}

	// Set up query handlers
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("component started",
		slog.String("component", "graph-embedding"),
		slog.Time("start_time", c.startTime),
		slog.String("embedder_type", c.config.EmbedderType))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}

	// Stop coalescer so no new batch callbacks fire during teardown
	if c.entityCoalescer != nil {
		_ = c.entityCoalescer.Close()
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

	// Stop worker first
	if c.worker != nil {
		if err := c.worker.Stop(); err != nil {
			c.logger.Warn("worker stop error", slog.Any("error", err))
		}
	}

	// Close content store (releases ObjectStore handle + cache)
	if c.contentStore != nil {
		if err := c.contentStore.Close(); err != nil {
			c.logger.Warn("content store close error", slog.Any("error", err))
		}
	}

	// Close embedder
	if c.embedder != nil {
		if err := c.embedder.Close(); err != nil {
			c.logger.Warn("embedder close error", slog.Any("error", err))
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
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-embedding"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-embedding"))
		return errs.WrapTransient(errs.ErrConnectionTimeout, "Component", "Stop", fmt.Sprintf("stop timeout after %v", timeout))
	}
}

// createEmbedder creates the embedder based on configuration.
func (c *Component) createEmbedder() error {
	switch c.config.EmbedderType {
	case "bm25":
		c.embedder = embedding.NewBM25Embedder(embedding.BM25Config{
			Dimensions: 384,
			K1:         1.5,
			B:          0.75,
		})
		c.logger.Info("using BM25 embedder", slog.Int("dimensions", 384))

	case "http":
		// Aligned with graph-clustering / graph-query (beta.47) — capability
		// timeout via model.ResolveCapabilityTimeout, endpoint connection-
		// hygiene via ResolveEndpointWithConfig. Embedding's per-call budget
		// is distinct from inference (no token streaming, fixed-size body),
		// hence its own DefaultEmbeddingTimeout constant rather than reusing
		// the synthesis or summary defaults.
		resolved, epCfg, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityEmbedding)
		if err != nil {
			return errs.Wrap(err, "Component", "createEmbedder", "resolve embedding endpoint")
		}
		const defaultEmbeddingTimeout = 30 * time.Second
		embedTimeout := model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityEmbedding, defaultEmbeddingTimeout, c.logger)
		httpCfg := embedding.HTTPConfig{
			BaseURL: resolved.URL,
			Model:   resolved.Model,
			APIKey:  resolved.APIKey,
			Timeout: embedTimeout,
			Logger:  c.logger,
		}
		if epCfg != nil {
			httpCfg.IdleConnTimeout = epCfg.IdleConnTimeout
			httpCfg.ResponseHeaderTimeout = epCfg.ResponseHeaderTimeout
			httpCfg.DisableKeepAlives = epCfg.DisableKeepAlives
			httpCfg.QueryPrefix = epCfg.QueryPrefix // asymmetric query embedding (gh#438)
		}
		httpEmbedder, err := embedding.NewHTTPEmbedder(httpCfg)
		if err != nil {
			return errs.Wrap(err, "Component", "createEmbedder", "HTTP embedder creation")
		}
		c.embedder = httpEmbedder
		c.logger.Info("using HTTP embedder", slog.String("url", resolved.URL), slog.String("model", resolved.Model))

	default:
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "createEmbedder",
			fmt.Sprintf("unknown embedder type: %s", c.config.EmbedderType))
	}

	if c.metrics != nil {
		c.metrics.setEmbedderType(c.config.EmbedderType)
	}
	return nil
}

// createEmbeddingBuckets creates the embedding index and dedup buckets.
func (c *Component) createEmbeddingBuckets(ctx context.Context) (jetstream.KeyValue, jetstream.KeyValue, error) {
	indexBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEmbeddingIndex,
		Description: "Entity embedding index",
	})
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "createEmbeddingBuckets",
			fmt.Sprintf("KV bucket: %s", graph.BucketEmbeddingIndex))
	}

	dedupBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEmbeddingDedup,
		Description: "Entity embedding deduplication",
	})
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "createEmbeddingBuckets",
			fmt.Sprintf("KV bucket: %s", graph.BucketEmbeddingDedup))
	}

	return indexBucket, dedupBucket, nil
}

// createStatusBucket creates-or-opens GRAPH_STATUS and wires this producer's publisher
// (ADR-083). Creation is idempotent across producers: graph-index runs the same
// EnsureBucket in the same binary, in either order, and natsclient's
// CreateKeyValueBucket resolves both the already-exists and the concurrent-create race
// to the existing handle.
func (c *Component) createStatusBucket(ctx context.Context) error {
	bucket, err := readiness.EnsureBucket(ctx, c.natsClient)
	if err != nil {
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "createStatusBucket", "context cancelled")
		}
		return errs.Wrap(err, "Component", "createStatusBucket",
			fmt.Sprintf("KV bucket: %s", readiness.BucketGraphStatus))
	}
	c.statusPublisher = readiness.NewPublisher(bucket, readiness.KeyGraphEmbedding)
	return nil
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
		ComponentName:    "graph-embedding",
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// initStorageAndWorker initializes storage and starts the embedding worker.
func (c *Component) initStorageAndWorker(ctx context.Context, indexBucket, dedupBucket jetstream.KeyValue) error {
	c.storage = embedding.NewStorage(indexBucket, dedupBucket)

	// Seed the current-failed map from the durable EMBEDDING_INDEX BEFORE the worker
	// starts, so FailedCount (and the degraded verdict) is accurate immediately rather
	// than depending on re-delivery timing (#613). Best-effort: a scan error leaves the
	// map empty and is corrected by live terminals, so it must not fail Start.
	c.seedFailedMap(ctx)

	// Start the in-memory vector cache before the worker so it is warm as
	// quickly as possible. Errors here are non-fatal: similarity queries
	// fall back to the KV scan path until the watcher is ready.
	if err := c.storage.StartVectorCache(ctx); err != nil {
		c.logger.Warn("vector cache watcher failed to start, similarity queries will use KV scan",
			slog.Any("error", err))
	}

	c.worker = embedding.NewWorker(c.storage, c.embedder, indexBucket, c.logger).
		WithWorkers(c.config.Workers).
		WithMaxSourceTextLen(c.maxSourceTextLen()).
		WithEmbedderType(c.config.EmbedderType).
		WithMetrics(newWorkerMetricsAdapter(c.metrics, c.failuresVec)).
		WithOnGenerated(func(entityID string, _ []float32) {
			if c.metrics != nil {
				c.metrics.recordEmbeddingGenerated()
			}
			c.logger.Debug("embedding generated", "entity_id", entityID)
		}).
		WithOnTerminal(func(entityID string, sourceRevision uint64, outcome embedding.TerminalOutcome, reason string) {
			// hop-2 reached a terminal (generated / failed / no-text skip). Complete the
			// hop-1 readiness watermark for this entity (ADR-066 §3) and route the
			// current-failed map by outcome (#613).
			c.completeEmbedding(entityID, sourceRevision, outcome, reason)
		})

	// Wire the shared store resolver (ADR-063) as the PRIMARY content-fetch path:
	// resolves a StorageRef against any registered storage instance. Guard the nil
	// case explicitly — passing a nil *storeregistry.Registry through the interface
	// would be a non-nil interface over a nil pointer and panic on use.
	if c.storeRegistry != nil {
		c.worker = c.worker.WithStoreResolver(c.storeRegistry)
	}

	// Wire the OWNED fallback content store (single store-read bucket) for
	// deployments without a registry entry for the ref's instance. Reads bucket
	// name from the store-read port config; owned by Component, closed in Stop().
	// A fatal retention error here (#600/#616) must abort Start closed — Start
	// returns this error before starting the entity watcher or query handlers.
	contentStore, err := c.createContentStore(ctx)
	if err != nil {
		return err
	}
	c.contentStore = contentStore
	if c.contentStore != nil {
		c.worker = c.worker.WithContentStore(c.contentStore)
	}

	if err := c.worker.Start(ctx); err != nil {
		return errs.Wrap(err, "Component", "initStorageAndWorker", "worker start")
	}
	return nil
}

// maxSourceTextLen is the effective truncation cap the worker applies before
// generation: the operator-set MaxTextLen when positive, otherwise a per-embedder
// default. It is the cap folded into the dedup-key identity (EmbedderIdentity.MaxTextLen),
// so the vector, the truncated bytes, and the key all agree on one value (#602).
func (c *Component) maxSourceTextLen() int {
	if c.config.MaxTextLen > 0 {
		return c.config.MaxTextLen
	}
	if c.config.EmbedderType == "bm25" {
		return maxSourceTextLenBM25
	}
	return maxSourceTextLenNeural
}

// seedFailedMap populates the current-failed map from the durable EMBEDDING_INDEX at
// Start (#613), so FailedCount is accurate immediately after bootstrap. Best-effort: a
// scan error is logged and leaves the map as-is (live terminals will populate it), never
// failing Start. `at` is set to now for every seeded entry — this process cannot know
// the original failure time, so first_failure_at reports "at least since this restart",
// an honest lower bound.
func (c *Component) seedFailedMap(ctx context.Context) {
	if c.storage == nil {
		return
	}
	entries, err := c.storage.ScanFailed(ctx)
	if err != nil {
		c.logger.Warn("current-failed seed scan failed; FailedCount will populate from live terminals",
			slog.Any("error", err))
		return
	}
	if len(entries) == 0 {
		return
	}
	now := time.Now()
	c.failedMu.Lock()
	if c.failed == nil {
		c.failed = make(map[string]failureInfo, len(entries))
	}
	for _, e := range entries {
		// e.Reason is already normalized to the bounded enum by ScanFailed (#613 F5), and
		// e.SourceRevision seeds the revision-CAS baseline so a stale older completion after
		// restart cannot clear this failure (#613 F1). `at` is a lower bound (this process
		// cannot know the original failure time).
		c.failed[e.EntityID] = failureInfo{reason: e.Reason, at: now, rev: e.SourceRevision}
	}
	n := len(c.failed)
	c.failedMu.Unlock()
	c.setFailedGauge(n)
	c.logger.Info("seeded current-failed map from durable EMBEDDING_INDEX",
		slog.Int("failed_count", n))
}

// applyTerminalOutcome routes a terminal outcome into the current-failed map (#613),
// REVISION-AWARE so a superseded older terminal never overrides a newer state (#613 F1).
// sourceRevision is the ENTITY_STATES revision this terminal completes:
//
//   - Failed: record the failure only when sourceRevision >= the held failure's revision.
//     A first failure (absent entry, held rev 0) is always recorded; a re-failure at the
//     same-or-newer revision updates the reason and rev but preserves the earliest at; an
//     OLDER failure arriving after a newer one is a no-op.
//   - Non-failed (Generated/Skipped/Deleted): clear the failure only when sourceRevision
//     >= the held failure's revision. A SUPERSEDED older completion (its revision below the
//     current failure's) is a NO-OP — the durable EMBEDDING_INDEX record is still failed at
//     the newer revision (storage's revision CAS dropped the older write), so readiness must
//     keep counting it. This mirrors the storage-side revision CAS exactly.
//
// The gauge is set to len(failed) under the same critical section (Set is a non-blocking
// atomic store, so holding the lock across it adds no meaningful contention, and it keeps
// gauge ordering consistent with the map mutation order). Nil-map safe for the pre-Start /
// unit-test path.
func (c *Component) applyTerminalOutcome(entityID string, sourceRevision uint64, outcome embedding.TerminalOutcome, reason string) {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	if c.failed == nil {
		c.failed = make(map[string]failureInfo)
	}
	held, present := c.failed[entityID]
	if outcome == embedding.OutcomeFailed {
		// Do not let an OLDER failure override a newer one already held.
		if present && sourceRevision < held.rev {
			return
		}
		held.reason = reason
		held.rev = sourceRevision
		if held.at.IsZero() {
			held.at = time.Now()
		}
		c.failed[entityID] = held
	} else {
		// A superseded older completion must NOT clear a newer failure; the durable record
		// is still failed at held.rev.
		if present && sourceRevision < held.rev {
			return
		}
		delete(c.failed, entityID)
	}
	c.setFailedGauge(len(c.failed))
}

// setFailedGauge sets the per-registry failed gauge to n. Nil-safe for the unit-test
// path where no registry (and hence no gauge) is wired.
func (c *Component) setFailedGauge(n int) {
	if c.failedGauge != nil {
		c.failedGauge.Set(float64(n))
	}
}

// failedSnapshot returns the bounded failure detail for the readiness envelope (#613):
// the current failed count, a reason→count histogram (bounded by the reason enum), and
// the earliest first-failure time across the map (zero when there are no failures). It
// takes one lock and copies out, so the compute path never holds the lock across I/O.
func (c *Component) failedSnapshot() (count uint64, reasons map[string]uint64, firstAt time.Time) {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	count = uint64(len(c.failed))
	if count == 0 {
		return 0, nil, time.Time{}
	}
	reasons = make(map[string]uint64, len(c.failed))
	for _, info := range c.failed {
		reason := info.reason
		if reason == "" {
			reason = "unknown" // a seeded pre-#613 record with no stored reason
		}
		reasons[reason]++
		if firstAt.IsZero() || info.at.Before(firstAt) {
			firstAt = info.at
		}
	}
	return count, reasons, firstAt
}

// createContentStore creates an ObjectStore handle from the store-read port config.
// Returns (nil, nil) if no store-read port is configured or if the store is merely
// unavailable (unconfigured / not reachable) — the content store is legitimately
// OPTIONAL and ContentStorable degrades gracefully without it. It returns a FATAL
// error only when the D2 retention guard (#600/#616) refuses a backing stream that
// keeps lifecycle eviction it cannot strip; that must fail Start closed rather than
// silently disable content, so the caller must propagate it.
func (c *Component) createContentStore(ctx context.Context) (*objectstore.Store, error) {
	// Find store-read port bucket name
	bucket := ""
	for _, port := range c.config.Ports.Inputs {
		if port.Type == "store-read" {
			bucket = port.Bucket
			if bucket == "" {
				bucket = port.Subject
			}
			break
		}
	}
	if bucket == "" {
		return nil, nil
	}

	store, err := objectstore.NewStoreWithConfig(ctx, c.natsClient, objectstore.Config{
		BucketName: bucket,
	})
	resolved, resErr := contentStoreOutcome(store, err, bucket)
	switch {
	case resErr != nil:
		// Fatal retention violation (#600/#616): fail Start closed.
		return nil, resErr
	case resolved == nil:
		// Non-fatal / unavailable: the content store is OPTIONAL, so disable it and
		// continue (BM25 / no-store tiers boot without it).
		c.logger.Debug("content store not available, ContentStorable disabled",
			slog.String("bucket", bucket),
			slog.Any("error", err))
		return nil, nil
	default:
		c.logger.Info("content store wired for embedding text extraction",
			slog.String("bucket", bucket))
		return resolved, nil
	}
}

// contentStoreOutcome maps a content-store constructor result to createContentStore's
// return, PRESERVING the D2 retention guard's fail-closed semantics (#600/#616). The
// content store is legitimately OPTIONAL, so a non-fatal "unavailable" error resolves
// to a disabled store (nil, nil) and the component degrades gracefully; a FATAL
// retention violation returns (nil, WrapFatal) so Start fails CLOSED. The IsFatal
// branch is load-bearing — folding a fatal into the graceful nil path re-introduces
// the #632 swallowed-fatal defect. Extracted so the decision is directly testable.
func contentStoreOutcome(store *objectstore.Store, err error, bucket string) (*objectstore.Store, error) {
	switch {
	case err == nil:
		return store, nil
	case errs.IsFatal(err):
		return nil, errs.WrapFatal(err, "Component", "createContentStore",
			fmt.Sprintf("content store %q retention", bucket))
	default:
		return nil, nil // non-fatal: content store optional, disable and continue
	}
}

// waitForDependenciesAndStartWatcher waits for ENTITY_STATES bucket and starts the entity watcher.
func (c *Component) waitForDependenciesAndStartWatcher(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.Wrap(err, "Component", "waitForDependenciesAndStartWatcher", "JetStream connection")
	}

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
			"Component", "waitForDependenciesAndStartWatcher", "dependency not available",
		)
	}

	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	if err != nil {
		return errs.Wrap(err, "Component", "waitForDependenciesAndStartWatcher", "get entity bucket")
	}
	c.entityStatesBucket = entityBucket

	// From this point query handlers must fail closed until the same WatchAll
	// watcher has validated its complete bootstrap snapshot.
	c.watchUnavailable.Store(false)
	c.bootstrapComplete.Store(false)
	c.buildApplied.Store(false)
	c.bootstrapTarget.Store(0)
	c.bootstrapStarted.Store(true)
	c.wg.Add(1)
	go c.watchEntityStates(ctx, entityBucket)

	// Readiness-envelope metrics loop (ADR-066 §3): republishes readiness/lag/watermark
	// as scrapeable gauges independent of NATS status traffic (#579 at the source).
	c.wg.Add(1)
	go c.statusMetricsLoop(ctx)
	return nil
}

// statusMetricsInterval is the readiness heartbeat: how often the envelope (ADR-066 §3)
// is republished as Prometheus gauges AND written to the GRAPH_STATUS KV key
// (ADR-083). Kept well above sub-second because each refresh does a BucketLastSeq NATS
// read for the target; a few seconds is ample for dashboards/alerts while never
// freezing at a stale value.
//
// It is DERIVED from readiness.DefaultHeartbeat rather than restated, because every
// consumer's status-unknown threshold is FreshnessMultiplier x that constant: two
// independent 5s literals would let a producer cadence change silently make consumers
// declare a healthy producer dead.
const statusMetricsInterval = readiness.DefaultHeartbeat

// statusMetricsLoop periodically republishes the readiness envelope as gauges and as
// GRAPH_STATUS KV state so an operator can scrape readiness/lag/watermark, and a
// consumer can hold it, without issuing a NATS status request — the #579
// silent-staleness class at the source. The entity watcher is event-driven, not
// periodic, so a dedicated tick is required to stay fresh when no writes or queries
// arrive. Runs until ctx is cancelled (Stop).
func (c *Component) statusMetricsLoop(ctx context.Context) {
	defer c.wg.Done()
	// Publish once up front so the gauges and the KV key are populated before the
	// first tick rather than reading zero (or absent) for the first interval.
	c.refreshReadinessMetrics(ctx)
	ticker := time.NewTicker(c.statusTickInterval())
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

// statusTickInterval is the heartbeat period, overridable only by tests (see
// statusInterval) so an integration test can observe successive heartbeats without
// sleeping through production cadence.
func (c *Component) statusTickInterval() time.Duration {
	if c.statusInterval > 0 {
		return c.statusInterval
	}
	return statusMetricsInterval
}

// refreshReadinessMetrics computes the current readiness envelope ONCE and fans it out
// to both distribution channels: the Prometheus gauges and the GRAPH_STATUS KV key.
// The single compute is the contract, not an optimization — two computes at slightly
// different instants would let the scraped gauges and the watched key disagree about
// readiness, and a consumer reconciling them would have no way to tell which was
// right. Extracted from statusMetricsLoop so tests drive the real path with no
// goroutine or sleep. Tolerates the not-yet-ready compute (nil watermark/bucket):
// computeEmbeddingStatus returns {Ready:false, State:building} and both channels carry
// that.
func (c *Component) refreshReadinessMetrics(ctx context.Context) {
	status := c.computeEmbeddingStatus(ctx)
	if c.metrics != nil {
		c.metrics.setReadinessGauges(status)
	}
	c.publishReadinessStatus(ctx, status)
}

// publishReadinessStatus writes the envelope to the GRAPH_STATUS key (ADR-083).
//
// A failed write must never kill the tick loop or the component: the next heartbeat is
// the recovery path, and consumers already fail closed (status_unknown) after three
// missed heartbeats, so the correct behavior here is to leave evidence and keep
// ticking. The warn is per-tick rather than rate-limited because the heartbeat is slow
// enough (one line per interval) that the log stays readable, and the counter carries
// the aggregate.
func (c *Component) publishReadinessStatus(ctx context.Context, status graph.IndexStatusResponse) {
	if c.statusPublisher == nil {
		return
	}
	if err := c.statusPublisher.Publish(ctx, status); err != nil {
		if ctx.Err() != nil {
			// Shutdown, not a failure: Stop cancels mid-Put on the way out.
			return
		}
		if c.metrics != nil {
			c.metrics.recordStatusPublishFailure()
		}
		c.logger.Warn("readiness status publish failed",
			slog.String("bucket", readiness.BucketGraphStatus),
			slog.String("key", c.statusPublisher.Key()),
			slog.Any("error", err))
	}
}

// ============================================================================
// Entity State Watcher
// ============================================================================

// watchEntityStates watches the ENTITY_STATES KV bucket and queues entities for embedding
func (c *Component) watchEntityStates(ctx context.Context, bucket jetstream.KeyValue) {
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

	// Build the private projection incrementally in constant space while queries
	// remain gated. nil proves the complete snapshot valid. The same watcher stays
	// attached for all later live updates, so there is no list/watch gap.
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
						slog.String("reason", c.graphStateResetReason()))
					continue
				}
				// Capture the enumeration-time target BEFORE raising the flag: every
				// pre-existing entity has now been delivered, so observedHigh bounds
				// their revisions. An empty graph gives 0, which the applied check
				// satisfies immediately.
				if c.watermark != nil {
					c.bootstrapTarget.Store(c.watermark.Observed())
				}
				c.bootstrapComplete.Store(true)
				c.logger.Debug("entity watcher initial sync complete",
					slog.Uint64("bootstrap_target", c.bootstrapTarget.Load()))
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
	// Record each valid delivered revision before dispatch. During bootstrap this
	// advances only the private projection; queries remain gated until nil proves
	// the complete snapshot valid. entry.Created() is the KV COMMIT time, the
	// age-of-view input to staleness_ms (ADR-083 D3) — never local arrival time.
	if c.watermark != nil {
		c.watermark.Observe(entry.Revision(), entry.Key(), entry.Created())
	}

	if graph.IsKVTombstone(entry.Operation()) {
		if c.entityCoalescer != nil {
			c.entityCoalescer.Remove(entry.Key())
		}
		// The entity is gone, so its vector must go too (gh#614). Leaving it in
		// EMBEDDING_INDEX keeps semantic search returning a dead entity ID that
		// graph-query then cannot resolve, which drops the query onto its text
		// fallback — deletions would silently degrade search AND push queries off
		// the semantic path. The Storage vector cache only evicts on a KV delete of
		// the embedding key, so this is also what makes that eviction path live.
		//
		// A delete failure is logged, never fatal, and never skips the completion
		// below: the watermark must still drain or one failed delete pins embedding
		// readiness forever (ADR-066 §3).
		if c.storage != nil {
			if err := c.storage.DeleteEmbedding(ctx, entry.Key()); err != nil {
				c.logger.Warn("failed to delete embedding for tombstoned entity",
					slog.String("entity", entry.Key()),
					slog.Any("error", err))
			}
		}
		// OutcomeDeleted: a tombstoned entity is not a current failure — clear it from the
		// current-failed map so a previously-failed entity that is deleted stops holding
		// the producer degraded (#613).
		c.completeEmbedding(entry.Key(), entry.Revision(), embedding.OutcomeDeleted, "")
		return
	}

	if c.entityCoalescer != nil {
		c.entityCoalescer.Add(entry.Key())
	} else {
		c.queueEntityForEmbedding(ctx, entry.Key(), entry.Revision(), entry.Value())
	}
}

func (c *Component) latchGraphStateReset(reason graph.StateResetReason) {
	c.resetState.CompareAndSwap(nil, &graph.StateContractError{Reason: reason})
}

func (c *Component) graphStateResetReason() string {
	if state := c.resetState.Load(); state != nil {
		return string(state.Reason)
	}
	return string(graph.GraphStateReasonUnreadableEntity)
}

func (c *Component) ensureBootstrapReady() error {
	if state := c.resetState.Load(); state != nil {
		return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
			state)
	}
	if c.watchUnavailable.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("embedding index not ready: ENTITY_STATES watcher is unavailable"))
	}
	if c.bootstrapStarted.Load() && !c.bootstrapComplete.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("embedding index not ready: ENTITY_STATES bootstrap is still validating"))
	}
	return nil
}

// processEntityBatch re-fetches each entity from KV and queues it for embedding.
// It is invoked by the CoalescingSet after the debounce window elapses.
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
			// revision to complete. Max-rev-drain the key rather than strand it:
			// a transient Get failure would otherwise pin the watermark forever
			// (ADR-066 §3). Fails toward caught-up; the next update re-observes it.
			// OutcomeSkipped: a drain is not a failure.
			c.completeEmbedding(entityID, ^uint64(0), embedding.OutcomeSkipped, "")
			continue
		}

		c.queueEntityForEmbedding(ctx, entry.Key(), entry.Revision(), entry.Value())
	}
}

// queueEntityForEmbedding queues an entity for async embedding generation
// indexingEligible reports whether an entity should be indexed for embeddings
// based on its ADR-054 indexing profile (entity.indexing.profile).
//
// Phase 1 is LENIENT: every entity is eligible regardless of profile, so this
// always returns true (provable no-op, zero behavior change). It reads the
// profile and emits a Debug observation for the profiles Phase 3 will exclude
// (trace, raw signal) — that is the dry-run signal, not an action.
//
// Strict enforcement (actually skipping ineligible entities + emitting
// embedding_skipped_total) is ADR-054 Phase 3, and MUST NOT ship as a bare
// toggle: it is gated on the cost-ledger preconditions (dry-run report,
// skipped metric, golden-corpus regression test, backfill) per
// gate-silent-exclusion-flips-with-cost-ledger. So Phase 1 never excludes.
func (c *Component) indexingEligible(es *graph.EntityState) bool {
	if v, ok := es.GetPropertyValue(vocabulary.EntityIndexingProfile); ok {
		if profile, _ := v.(string); profile == vocabulary.IndexingProfileTrace || profile == vocabulary.IndexingProfileSignal {
			c.logger.Debug("entity has a non-embedding indexing profile; indexing anyway (ADR-054 Phase 1 lenient)",
				slog.String("entity", es.ID),
				slog.String("indexing_profile", profile))
		}
	}
	return true
}

// queueEntityForEmbedding is hop 1 of the two-hop embedding pipeline: parse the
// entity, decide eligibility, and either terminate immediately (no text to embed) or
// SavePending a record for hop 2. sourceRevision is the ENTITY_STATES revision that
// produced this entry (ADR-066 §3); every IMMEDIATE terminal here completes the
// readiness watermark, and SavePending threads the revision so hop 2 completes it at
// the true terminal.
func (c *Component) queueEntityForEmbedding(ctx context.Context, entityID string, sourceRevision uint64, data []byte) {
	// Report embedding stage (throttled to avoid KV spam)
	if err := c.lifecycleReporter.ReportStage(ctx, "embedding"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "embedding"), slog.Any("error", err))
	}

	// Parse entity state
	var entityState graph.EntityState
	if err := graph.UnmarshalEntityState(data, &entityState); err != nil {
		var stateErr *graph.StateContractError
		if errors.As(err, &stateErr) {
			c.latchGraphStateReset(stateErr.Reason)
		}
		c.logger.Warn("failed to unmarshal entity state",
			slog.String("entity", entityID),
			slog.Any("error", err))
		// Do not complete this revision. Incompatible authoritative state is a
		// sticky reset requirement, not a successful terminal skip.
		return
	}

	// ADR-054 Phase 1 (lenient): consult the entity's indexing profile but
	// never exclude — strict enforcement is Phase 3, gated on the cost-ledger
	// preconditions. indexingEligible always returns true here, so this is a
	// provable no-op (zero behavior change); it is the seam Phase 3 turns live.
	if !c.indexingEligible(&entityState) {
		// terminal: deliberately skipped (OutcomeSkipped — not a failure).
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		return
	}

	// ContentStorable path: prefer the StorageRef → ObjectStore fetch, but ONLY
	// when a content store is actually wired (a store-read port is configured).
	//
	// #264 (ADR-055 Wave 0) began lifting StorageRef onto the EntityState at the
	// ingest seam. Without a content store, the worker's fetchTextFromStorage
	// hard-fails ("content store not configured") and markFailed fires — which
	// silently collapses ALL embedding (and therefore BM25/search) for every
	// entity whose content was offloaded. Configs without a content store (e.g.
	// the BM25 statistical tier, which wires no store-read port) have no way to
	// honour a StorageRef, so degrade gracefully to inline text extraction from
	// the entity's content triples instead of failing. Configs WITH a content
	// store keep the offloaded-content fetch path unchanged.
	if c.shouldFetchViaStorageRef(&entityState) {
		c.queueEmbeddingWithStorageRef(ctx, entityID, sourceRevision, &entityState)
		return
	}
	// NOT a terminal: reporting the excluded offload falls THROUGH to the inline-text
	// path below, whose no-text return / SavePending owns the true terminal. Completing
	// here would drain the watermark before hop 2 finishes (false-ready — ADR-066 §3 D2).
	if entityState.StorageRef != nil {
		c.reportOffloadedContentExcluded(entityID, entityState.StorageRef.StorageInstance)
	}

	// Legacy path: Extract text from triples
	text := c.extractTextForEmbedding(&entityState)
	if text == "" {
		c.logger.Debug("no text content found, skipping embedding", slog.String("entity", entityID))
		// Remove any DURABLE record before clearing readiness (#613 F2): an entity that
		// previously FAILED and now has no text still holds a StatusFailed record in
		// EMBEDDING_INDEX. Clearing the in-memory failed map (via the Skipped completion
		// below) without deleting that record leaves durable and in-memory state divergent —
		// a restart's seed scan would re-add it as failed. Deleting it makes the no-text
		// terminal consistent with the worker's own no-text path, which also deletes. A
		// delete failure is logged, never fatal, and never skips the completion below (a
		// stranded delete must not pin the watermark — ADR-066 §3); the residual durable
		// record self-corrects on the next restart's seed scan.
		if c.storage != nil {
			if err := c.storage.DeleteEmbedding(ctx, entityID); err != nil {
				c.logger.Debug("failed to delete stale embedding for no-text entity",
					slog.String("entity", entityID), slog.Any("error", err))
			}
		}
		// Terminal: telemetry-only / no-text entities never reach hop 2. Completing
		// here is what makes Target=LastSeq reachable (else embedding.ready deadlocks
		// on every text-less entity — ADR-066 §3). OutcomeSkipped: no-text is not a
		// failure, and it clears any prior failed-map entry for this entity.
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		return
	}

	// Hop 1 no longer derives the dedup key. Hop 2 derives it over the resolved and
	// truncated bytes it is about to embed (#623), which keys the inline and offloaded
	// lanes identically and folds in the effective cap for free. The pending record is
	// a reference, not a key, so its ContentHash is written empty.
	//
	// Queue for embedding generation. A transient SavePending failure wrote NO durable
	// record — the entity is neither generated nor durably failed, just un-queued at this
	// revision. Do NOT complete the watermark or touch the current-failed map here (#613
	// F2): completing would report this revision done (and clear any older durable failed
	// record for the entity) over work that never persisted. Leave the revision
	// uncompleted; the ENTITY_STATES watcher re-delivers the entity on its next write, and
	// a sustained KV outage degrades honestly via the completions-based stuck detector
	// rather than reporting a false-ready. This is the SAME non-terminal treatment hop 2
	// gives a SaveFailed/CAS persistence miss.
	if err := c.storage.SavePending(ctx, entityID, "", text, sourceRevision); err != nil {
		c.logger.Error("failed to queue embedding; leaving revision uncompleted for re-delivery",
			slog.String("entity", entityID),
			slog.Any("error", err))
		return
	}

	c.logger.Debug("queued embedding for generation",
		slog.String("entity", entityID),
		slog.Int("text_length", len(text)))
}

// shouldFetchViaStorageRef reports whether an entity's offloaded content should
// be fetched via its StorageRef. It requires a StorageRef AND a store that can
// serve it — either (ADR-063) the shared registry can resolve the ref's owning
// StorageInstance (federated: ANY registered instance), or the legacy single
// wired content store is present as a fallback. Without either, the worker's
// fetchTextFromStorage hard-fails ("content store not configured") and markFailed
// fires, silently collapsing all embedding/search for offloaded entities; so we
// fall back to inline text extraction instead and report the exclusion loudly
// (gh#414, see queueEntityForEmbedding). Configs that can serve the ref are
// unaffected.
func (c *Component) shouldFetchViaStorageRef(state *graph.EntityState) bool {
	if state.StorageRef == nil {
		return false
	}
	if c.storeRegistry != nil {
		if _, ok := c.storeRegistry.Streamable(state.StorageRef.StorageInstance); ok {
			return true
		}
	}
	return c.contentStore != nil
}

// reportOffloadedContentExcluded makes the silent-loss case observable (gh#414):
// an entity carries a StorageRef (its BODY is offloaded to a store, NOT inline)
// but no content store is wired, so the inline fallback cannot see that body and
// it is EXCLUDED from the embedding (and thus BM25/search). The entity may still
// be embedded from any inline text triples it carries — only the offloaded body
// is lost. A per-entity metric carries the count; the warning fires once so the
// log is a single actionable line, not a flood. Fix on the operator side: wire a
// store-read port for the StorageInstance that produced the content.
func (c *Component) reportOffloadedContentExcluded(entityID, storageInstance string) {
	if c.metrics != nil {
		c.metrics.recordContentUnresolved()
	}
	c.noContentStoreWarn.Do(func() {
		c.logger.Warn("offloaded body EXCLUDED from embeddings: entity carries a "+
			"StorageRef but no content store is wired — wire a store-read port for its "+
			"StorageInstance to embed offloaded bodies (inline text, if any, is still "+
			"embedded) (gh#414)",
			slog.String("entity", entityID),
			slog.String("storage_instance", storageInstance))
	})
	c.logger.Debug("entity has StorageRef but no content store; inline fallback",
		slog.String("entity", entityID))
}

// queueEmbeddingWithStorageRef queues an embedding using ContentStorable pattern.
// sourceRevision threads through to hop 2 for the readiness watermark (ADR-066 §3).
func (c *Component) queueEmbeddingWithStorageRef(ctx context.Context, entityID string, sourceRevision uint64, state *graph.EntityState) {
	// Create StorageRef for embedding record
	storageRef := &embedding.StorageRef{
		StorageInstance: state.StorageRef.StorageInstance,
		Key:             state.StorageRef.Key,
	}

	// Hop 1 writes an EMPTY ContentHash for the offloaded lane, exactly as it does for
	// the inline lane. The key was never derivable here: this ENTITY_STATES watcher
	// holds only the StorageRef, and message.StorageReference carries no content
	// digest — hashing the address served the OLD body's vector forever whenever a
	// producer overwrote a stable key. Hop 2 now derives the key over the fetched,
	// truncated body it is about to embed (#623), which is content-addressed and keys
	// this lane identically to the inline one, so the offloaded lane deduplicates
	// again without ever hashing an address.
	const contentHash = ""

	// Extract the entity's INLINE identity text with the SAME extractor the inline lane
	// uses (title/.signature/.comment, per Config.TextSuffixes). For an offloaded entity
	// the body is NOT an inline triple, so this returns exactly the inline identity
	// triples and never the body. Thread it to hop 2, which embeds it AHEAD of the
	// fetched body (identity-first, one vector) so text_suffixes takes effect on
	// offloaded entities too (D1/D2). Empty when the entity has no inline text →
	// hop 2 embeds body-only, unchanged.
	identityText := c.extractTextForEmbedding(state)

	// Queue for embedding generation with storage reference. A transient SavePending
	// failure wrote NO durable record, so leave the revision uncompleted and untouched in
	// the current-failed map — the ENTITY_STATES watcher re-delivers on the next write
	// (#613 F2, same non-terminal treatment as the inline path and hop 2's persistence
	// miss). Completing here would report the revision done over work that never persisted.
	if err := c.storage.SavePendingWithStorageRef(ctx, entityID, contentHash, identityText, storageRef, nil, sourceRevision); err != nil {
		c.logger.Error("failed to queue embedding with storage ref; leaving revision uncompleted for re-delivery",
			slog.String("entity", entityID),
			slog.Any("error", err))
		return
	}

	c.logger.Debug("queued embedding with storage reference",
		slog.String("entity", entityID),
		slog.String("storage_key", state.StorageRef.Key))
}

// defaultTextSuffixes is the fallback list when Config.TextSuffixes is empty.
var defaultTextSuffixes = []string{".title", ".content", ".description", ".summary", ".text", ".name", ".body", ".abstract", ".subject"}

// extractTextForEmbedding extracts text from entity state for embedding generation
func (c *Component) extractTextForEmbedding(state *graph.EntityState) string {
	var parts []string

	textSuffixes := c.config.TextSuffixes
	if len(textSuffixes) == 0 {
		textSuffixes = defaultTextSuffixes
	}

	// Look through all triples for text-like predicates
	for _, triple := range state.Triples {
		if triple.IsRelationship() {
			continue
		}

		predicate := strings.ToLower(triple.Predicate)

		// Check if predicate ends with any text suffix
		for _, suffix := range textSuffixes {
			if strings.HasSuffix(predicate, suffix) {
				if str, ok := triple.Object.(string); ok && str != "" {
					parts = append(parts, str)
				}
				break
			}
		}
	}

	return strings.Join(parts, " ")
}
