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
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	"github.com/c360studio/semstreams/pkg/revlag"
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

	// strandedAt is the STRANDING revision of a derived-write/read failure
	// (#625, in-memory only — Codex #722 B1). Non-zero exactly for stranded
	// entries (the three embedding.Reason* consts). A stranded mark is cleared
	// ONLY by causal convergence: explicitly by a hop-1 convergence
	// (clearStranded on a successful delete/skip/queue), or by an external
	// terminal whose sourceRevision >= strandedAt. Hop 2 is deliberately outside
	// the hop-1 seam, so a worker already in flight for an OLDER revision can
	// reach its terminal AFTER the stranding — an OBSOLETE terminal must not
	// count as convergence (under the falsified floor-0 rule it cleared the
	// mark, leaving a dead vector queryable with FailedCount 0 and repair no
	// longer targeting it). Sites with no authoritative revision in hand
	// (a failed reconcile read, a failed reconcile-absence delete) strand at
	// ^uint64(0): only explicit convergence clears them, and repair's 30s
	// cadence bounds the extra degraded window.
	strandedAt uint64
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
	// The component writes its durable results (EMBEDDING_INDEX,
	// EMBEDDING_DEDUP) directly at Start, not through declared output ports. A
	// configured output is a stale declaration (the EMBEDDINGS_CACHE surface
	// was deleted) that would register false port topology — reject it loudly
	// rather than advertise a write the component never performs.
	if len(c.Ports.Outputs) > 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"graph-embedding declares no output ports; remove ports.outputs (see docs/operations/embeddings-cache-removal.md)")
	}

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
				Name: "entity_watch", Config: component.KVWatchPort{Bucket: graph.BucketEntityStates},
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
					Name: "entity_watch", Config: component.KVWatchPort{Bucket: graph.BucketEntityStates},
				},
				{
					Name: "content_store", Config: component.StoreReadPort{Bucket: "MESSAGES"},
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

type entityStatesReader interface {
	Get(context.Context, string) (jetstream.KeyValueEntry, error)
	WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	Status(context.Context) (jetstream.KeyValueStatus, error)
}

// Component implements the graph-embedding processor
type Component struct {
	// Component metadata
	name    string
	config  Config
	inputs  []component.Port
	outputs []component.Port

	// Dependencies
	natsClient    *natsclient.Client
	logger        *slog.Logger
	modelRegistry model.RegistryReader

	// Domain resources
	embedder embedding.Embedder
	storage  *embedding.Storage
	worker   *embedding.Worker
	// storeRegistry is the shared {StorageInstance → store} resolver (ADR-063).
	// Sole content-fetch path: resolves a StorageRef against its exact registered
	// storage instance. Nil when the deployment does not admit the store-read port.
	storeRegistry *storeregistry.Registry
	// noContentStoreWarn fires the unresolved StorageInstance warning exactly once
	// (gh#414/#875); the per-entity metric carries the full count.
	noContentStoreWarn sync.Once
	entityCoalescer    *cache.CoalescingSet
	entityStatesBucket entityStatesReader

	// hop1Mu is the single-writer hop-1 seam (#629): every hop-1 EMBEDDING_INDEX
	// mutation — the watcher's immediate-mode update, the watcher's tombstone
	// delete, a coalesced flush, and a repair re-drive — serializes through it,
	// and queued work re-reads authoritative ENTITY_STATES INSIDE the lock
	// (reconcileEntity). Without it the coalesced flush's fresh Get and the
	// watcher's tombstone delete interleave across two buckets, and the unguarded
	// SavePending create lane resurrects a tombstoned entity's record after its
	// delete (the create has no prior record to CAS against, so the #614 part-2
	// revision guards cannot cover it).
	//
	// A MUTEX, deliberately not a channel into the watcher and not a keyed pool:
	// CoalescingSet.Close blocks on the in-flight callback and Stop calls Close, so
	// a callback blocked sending to an exited watcher would deadlock Close; and
	// hop 1 is two KV metadata ops — a keyed pool's lanes/goroutines/lifecycle to
	// protect two round-trips is unwarranted machinery. Hop 2 (the worker's
	// SaveGenerated/SaveFailed) MUST NOT take this seam: its revision CAS +
	// ErrRecordGone guards are sufficient, and serializing embedding workers
	// behind hop-1 metadata operations would stall the pipeline.
	//
	// LOCK ORDER: hop1Mu → failedMu, never the reverse (repairTargets snapshots
	// under failedMu and RELEASES before dispatching reconcileEntity). Lifecycle
	// cleanup closes entityCoalescer under stopping authority and without c.mu;
	// in-flight flushes therefore finish without contending on lifecycle metadata.
	hop1Mu sync.Mutex

	// Lifecycle state
	mu                sync.RWMutex
	lifecycleMu       sync.Mutex
	running           bool
	initialized       bool
	startTime         time.Time
	wg                sync.WaitGroup
	lifecycleUsed     bool
	cleanupPending    bool
	lifecycleTerminal bool
	stopping          bool
	startDone         chan struct{}
	cancel            context.CancelFunc
	runtimeDone       chan struct{}

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

	// Query subscriptions (for cleanup)
	querySubscriptions   []*natsclient.Subscription
	subscribeForRequests func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (*natsclient.Subscription, error)

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
		// Targeted probe for the removed cache_ttl knob: plain json.Unmarshal
		// silently ignores unknown fields, so a stale config carrying it would
		// otherwise appear to work while the operator believes the knob is
		// live. Reject loudly instead (targeted — NOT DisallowUnknownFields).
		var removed struct {
			CacheTTL *json.RawMessage `json:"cache_ttl"`
		}
		if err := json.Unmarshal(rawConfig, &removed); err == nil && removed.CacheTTL != nil {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphEmbedding", "factory",
				"cache_ttl was removed from graph-embedding; delete it from the config (see docs/operations/embeddings-cache-removal.md)")
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
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "CreateGraphEmbedding", "factory", "resolve input port")
		}
		inputs = append(inputs, port)
	}
	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "CreateGraphEmbedding", "factory", "resolve output port")
		}
		outputs = append(outputs, port)
	}
	var storeRegistry = (*storeregistry.Registry)(nil)
	for _, input := range inputs {
		facts, err := input.Facts()
		if err != nil {
			return nil, errs.WrapInvalid(err, "CreateGraphEmbedding", "factory", "project input port")
		}
		if _, ok := facts.StoreReadBucket(); ok {
			storeRegistry = deps.StoreRegistry
			break
		}
	}

	// Create component
	comp := &Component{
		name:          "graph-embedding",
		config:        config,
		inputs:        inputs,
		outputs:       outputs,
		natsClient:    natsClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		metrics:       getMetrics(deps.MetricsRegistry),
		storeRegistry: storeRegistry, // ADR-063 resolver is admitted only by a declared store-read input.
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

	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]component.Port(nil), c.outputs...)
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
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
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
func (c *Component) Start(ctx context.Context) (startErr error) {
	// Validate before inspecting lifecycle state.
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}
	c.lifecycleMu.Lock()
	c.mu.RLock()
	initialized := c.initialized
	c.mu.RUnlock()
	// Check initialization
	if !initialized {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrInvalidConfig, "Component", "Start", "component not initialized")
	}

	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrInvalidConfig, "Component", "Start", "component instance already used")
	}

	// Create cancellable context
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	c.lifecycleUsed = true
	c.cancel = cancel
	c.startDone = startDone
	c.cleanupPending = true
	c.lifecycleMu.Unlock()

	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanupFailedStart)
			startErr = errors.Join(startErr, rollbackErr)
			c.lifecycleMu.Lock()
			if rollbackErr == nil {
				c.cleanupPending = false
				c.lifecycleTerminal = true
				c.clearLifecycleHandles()
			}
			close(startDone)
			c.startDone = nil
			c.lifecycleMu.Unlock()
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return
		}
		c.lifecycleMu.Lock()
		c.cleanupPending = false
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}()

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
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
		return err
	}

	// Create embedder based on config
	if err := c.createEmbedder(); err != nil {
		return err
	}

	// Create embedding storage buckets
	embeddingIndexBucket, embeddingDedupBucket, err := c.createEmbeddingBuckets(ctx)
	if err != nil {
		return err
	}

	// Readiness watermark (ADR-066 §3): must exist before the worker (whose
	// onTerminal completes it) and the watcher (which observes into it).
	c.watermark = revlag.New()

	// Create storage and worker
	if err := c.initStorageAndWorker(ctx, embeddingIndexBucket, embeddingDedupBucket); err != nil {
		return err
	}

	// Optionally wrap entity updates with coalescing to avoid redundant
	// re-embedding. Constructed and PUBLISHED BEFORE the watcher goroutine
	// launches (#722 HIGH 3): the watcher reads c.entityCoalescer unsynchronized,
	// so assigning after launch is a data race, and with a preloaded
	// ENTITY_STATES bucket the bootstrap replay would race past a nil pointer
	// onto the immediate lane despite coalesce_ms > 0. Safe this early: no
	// callback can fire with work before the watcher Adds entries (an empty
	// pending set short-circuits the tick), and every Start failure path below
	// closes it.
	if c.config.CoalesceMs > 0 {
		c.entityCoalescer = cache.NewCoalescingSet(ctx, time.Duration(c.config.CoalesceMs)*time.Millisecond, func(entityIDs []string) {
			c.processEntityBatch(ctx, entityIDs)
		})
	}

	// Wait for ENTITY_STATES bucket and start entity watcher
	if err := c.waitForDependenciesAndStartWatcher(ctx); err != nil {
		return err
	}
	c.runtimeDone = make(chan struct{})
	go func(done chan struct{}) {
		c.wg.Wait()
		close(done)
	}(c.runtimeDone)

	// Set up query handlers
	if err := c.setupQueryHandlers(ctx); err != nil {
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Mark as running
	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	startTime := c.startTime
	c.mu.Unlock()
	committed = true

	c.logger.Info("component started",
		slog.String("component", "graph-embedding"),
		slog.Time("start_time", startTime),
		slog.String("embedder_type", c.config.EmbedderType))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed = true
			c.lifecycleTerminal = true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.lifecycleTerminal {
			c.lifecycleMu.Unlock()
			return nil
		}
		startDone := c.startDone
		if startDone != nil {
			c.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "Component", "Stop", "concurrent Stop is unsupported")
		}
		retryable := c.cleanupPending
		c.stopping = true
		c.lifecycleMu.Unlock()

		stopErr := c.cleanup(ctx, retryable)
		c.lifecycleMu.Lock()
		c.stopping = false
		if retryable && stopErr != nil {
			c.lifecycleMu.Unlock()
			return stopErr
		}
		c.cleanupPending = false
		c.lifecycleTerminal = true
		c.clearLifecycleHandles()
		c.lifecycleMu.Unlock()
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-embedding"))
		return stopErr
	}
}

func (c *Component) cleanupFailedStart(ctx context.Context) error {
	return c.cleanup(ctx, true)
}

func (c *Component) cleanup(ctx context.Context, retryable bool) error {
	var cleanupErr error
	unresolved := c.querySubscriptions[:0]
	for _, sub := range c.querySubscriptions {
		if sub == nil {
			continue
		}
		if err := sub.Drain(ctx); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			unresolved = append(unresolved, sub)
		}
	}
	if retryable {
		c.querySubscriptions = unresolved
	} else {
		c.querySubscriptions = nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.runtimeDone != nil {
		select {
		case <-c.runtimeDone:
			if retryable {
				c.runtimeDone = nil
				c.cancel = nil
			}
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
		}
	}
	if c.entityCoalescer != nil {
		if err := c.entityCoalescer.Close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else if retryable {
			c.entityCoalescer = nil
		}
	}
	if c.worker != nil {
		if err := c.worker.Stop(); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else if retryable {
			c.worker = nil
		}
	}
	if c.embedder != nil {
		if err := c.embedder.Close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else if retryable {
			c.embedder = nil
		}
	}
	return cleanupErr
}

func (c *Component) clearLifecycleHandles() {
	c.querySubscriptions = nil
	c.cancel = nil
	c.runtimeDone = nil
	c.entityCoalescer = nil
	c.worker = nil
	c.embedder = nil
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

// createEmbeddingBuckets acquires the embedding index and dedup buckets
// through the catalog owner seam, which reconciles an adopted bucket to the
// declared policy (a foreign TTL is stripped or this Start fails closed).
func (c *Component) createEmbeddingBuckets(ctx context.Context) (jetstream.KeyValue, jetstream.KeyValue, error) {
	indexBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketEmbeddingIndex)
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "createEmbeddingBuckets",
			fmt.Sprintf("KV bucket: %s", graph.BucketEmbeddingIndex))
	}

	dedupBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketEmbeddingDedup)
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
		WithOnGenerated(func(_ context.Context, entityID string, _ []float32) {
			if c.metrics != nil {
				c.metrics.recordEmbeddingGenerated()
			}
			c.logger.Debug("embedding generated", "entity_id", entityID)
		}).
		WithOnTerminal(func(_ context.Context, entityID string, sourceRevision uint64, outcome embedding.TerminalOutcome, reason string) {
			// hop-2 reached a terminal (generated / failed / no-text skip). Complete the
			// hop-1 readiness watermark for this entity (ADR-066 §3) and route the
			// current-failed map by outcome (#613).
			c.completeEmbedding(entityID, sourceRevision, outcome, reason)
		})

	// Wire the shared store resolver (ADR-063) as the SOLE content-fetch path:
	// resolves a StorageRef against its exact registered storage instance. Guard the nil
	// case explicitly — passing a nil *storeregistry.Registry through the interface
	// would be a non-nil interface over a nil pointer and panic on use.
	if c.storeRegistry != nil {
		c.worker = c.worker.WithStoreResolver(c.storeRegistry)
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
		// Do not let an OLDER failure override a newer one already held, and do
		// not let an OBSOLETE in-flight failure (below the stranding revision)
		// overwrite a stranded mark — that would silently drop the entity out of
		// the repair scope (#722 B1, same masking class as the clear below).
		if present && (sourceRevision < held.rev || sourceRevision < held.strandedAt) {
			return
		}
		held.reason = reason
		held.rev = sourceRevision
		// A causally-newer embedder failure supersedes any stranding: the new
		// authoritative revision's pipeline owns the entity now, and its recovery
		// path is re-delivery, not repair.
		held.strandedAt = 0
		if held.at.IsZero() {
			held.at = time.Now()
		}
		c.failed[entityID] = held
	} else {
		// A superseded older completion must NOT clear a newer failure (the
		// durable record is still failed at held.rev), and an OBSOLETE in-flight
		// terminal must NOT clear a stranding (#722 B1): hop 2 runs outside the
		// hop-1 seam, so a worker started before the stranding can complete after
		// it — its terminal is not convergence. A stranded mark clears only
		// causally: sourceRevision >= strandedAt, or explicitly via clearStranded
		// on a hop-1 convergence.
		if present && (sourceRevision < held.rev || sourceRevision < held.strandedAt) {
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

// waitForDependenciesAndStartWatcher waits for ENTITY_STATES bucket and starts the entity watcher.
func (c *Component) waitForDependenciesAndStartWatcher(ctx context.Context) error {
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
		return errs.WrapTransient(
			fmt.Errorf("bucket %s not available after %d attempts", graph.BucketEntityStates, c.config.StartupAttempts),
			"Component", "waitForDependenciesAndStartWatcher", "dependency not available",
		)
	}

	entityBucket, err := graph.OpenCatalogReader(ctx, c.natsClient, graph.BucketEntityStates)
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

	// Durable repair loop (#625, mirroring graph-index's gh#474 repairLoop):
	// re-drives entities whose derived writes/reads failed so a transient KV
	// outage self-heals without waiting for another entity event or a restart.
	c.wg.Add(1)
	go c.repairLoop(ctx)

	// Readiness-envelope metrics loop (ADR-066 §3): republishes readiness/lag/watermark
	// as scrapeable gauges independent of NATS status traffic (#579 at the source).
	c.wg.Add(1)
	go c.statusMetricsLoop(ctx)
	return nil
}

// embeddingRepairInterval is how often the repair loop re-drives entities whose
// derived writes/reads failed (#625; mirrors graph-index's indexRepairInterval,
// gh#474).
const embeddingRepairInterval = 30 * time.Second

// repairLoop periodically re-drives the repair-scoped current-failed entities
// through reconcileEntity so a failed derived delete/write converges instead of
// leaking until restart (#625). Runs until ctx is cancelled (Stop); drained by
// the existing wg.Wait.
//
// A DEDICATED ticker goroutine, deliberately not piggybacked on the ADR-083
// status heartbeat: repair KV I/O on that goroutine would delay heartbeat
// publication, whose freshness is a consumer-visible liveness contract
// (FreshnessMultiplier × DefaultHeartbeat), and statusTickInterval is
// test-overridable — a millisecond test override would hot-loop repair.
// graph-index's own repairLoop makes the same call.
//
// Retry is unbounded and flat: the repair set is reason-scoped to KV-transport
// faults on self-owned buckets (see repairTargets), so there is no poison class
// to give up on — a bounded give-up would recreate the #625 leak. FailedCount>0
// → degraded is the operator signal while any entity remains stranded.
func (c *Component) repairLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(embeddingRepairInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.repairStranded(ctx)
		}
	}
}

// repairTargets snapshots the repair-scoped entity IDs under failedMu and
// RELEASES the lock before the caller dispatches reconcileEntity. LOCK-ORDER
// INVARIANT: hop1Mu → failedMu, never the reverse — reconcileEntity takes
// hop1Mu and its terminal accounting takes failedMu inside it, so dispatching
// while still holding failedMu would invert the order and deadlock.
//
// Scope: ONLY the three derived-write/read reasons. Embedder-side failure
// reasons (connection_refused, timeout, …) stay OUT of the repair lane — their
// recovery path is re-delivery (restart or a new revision), unchanged — so no
// permanently-failing content can enter repair and hot-loop it.
func (c *Component) repairTargets() []string {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	var targets []string
	for entityID, info := range c.failed {
		switch info.reason {
		case embedding.ReasonDeleteFailed, embedding.ReasonPendingWriteFailed, embedding.ReasonEntityReadFailed:
			targets = append(targets, entityID)
		}
	}
	return targets
}

// repairStranded is the repair tick body: converge every repair-scoped stranded
// entity through the hop-1 seam. The empty-set short-circuit keeps the healthy
// steady state at one mutex-guarded map scan per tick with zero KV traffic
// (graph-index precedent).
func (c *Component) repairStranded(ctx context.Context) {
	if c.entityStatesBucket == nil {
		return
	}
	targets := c.repairTargets()
	if len(targets) == 0 {
		return
	}
	for _, entityID := range targets {
		if ctx.Err() != nil {
			return
		}
		c.reconcileEntity(ctx, entityID)
	}
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
func (c *Component) watchEntityStates(ctx context.Context, bucket entityStatesReader) {
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
		c.applyEntityTombstone(ctx, entry.Key(), entry.Revision())
		return
	}

	if c.entityCoalescer != nil {
		c.entityCoalescer.Add(entry.Key())
	} else {
		// Immediate mode: the watcher's own update is a hop-1 mutation and takes
		// the seam, so it cannot interleave a concurrent repair re-drive (#629).
		c.hop1Mu.Lock()
		c.queueEntityForEmbedding(ctx, entry.Key(), entry.Revision(), entry.Value())
		c.hop1Mu.Unlock()
	}
}

// applyEntityTombstone is the watcher's tombstone-delete hop-1 mutation, under
// the single-writer seam (#629).
//
// The entity is gone, so its vector must go too (gh#614). Leaving it in
// EMBEDDING_INDEX keeps semantic search returning a dead entity ID that
// graph-query then cannot resolve, which drops the query onto its text
// fallback — deletions would silently degrade search AND push queries off
// the semantic path. The Storage vector cache only evicts on a KV delete of
// the embedding key, so this is also what makes that eviction path live.
//
// A delete failure is logged, never fatal, and never skips the completion:
// the watermark must still drain or one failed delete pins embedding
// readiness forever (ADR-066 §3, #624). On failure this site makes TWO calls
// at DIFFERENT revisions, both load-bearing: completeEmbedding at the TRUE
// tombstone revision (the watermark drains exactly the revisions the tombstone
// supersedes) and THEN markStranded at floor revision 0 (so the entity counts
// degraded and the repair loop re-drives the delete until the key is absent —
// previously the leak reported ready until restart, #625).
func (c *Component) applyEntityTombstone(ctx context.Context, key string, revision uint64) {
	c.hop1Mu.Lock()
	defer c.hop1Mu.Unlock()

	var delErr error
	if c.storage != nil {
		if delErr = c.storage.DeleteEmbedding(ctx, key); delErr != nil {
			c.logger.Warn("failed to delete embedding for tombstoned entity",
				slog.String("entity", key),
				slog.Any("error", delErr))
		}
	}
	// OutcomeDeleted: a tombstoned entity is not a current failure — clear it from the
	// current-failed map so a previously-failed entity that is deleted stops holding
	// the producer degraded (#613). Ordering matters: this drain runs BEFORE the
	// failure mark below (drain-then-mark), so the mark is not clobbered by its own
	// revision's completion.
	c.completeEmbedding(key, revision, embedding.OutcomeDeleted, "")
	if delErr != nil {
		// Stranded at the tombstone's revision: only a terminal at/above it (or
		// explicit repair convergence) clears — an in-flight hop-2 terminal for an
		// older revision cannot mask the obligation (#722 B1).
		c.markStranded(key, embedding.ReasonDeleteFailed, revision)
		return
	}
	// Successful delete = hop-1 convergence: discharge any stranding whose
	// causal floor sits above this tombstone's revision (a ^uint64(0)-stranded
	// read/absence failure the revision-guarded drain above could not clear).
	c.clearStranded(key)
}

// reconcileEntity converges the derived EMBEDDING_INDEX state for entityID on
// the authoritative ENTITY_STATES value read at execution time, under the hop-1
// seam. It is the ONE dispatch shared by the coalesced flush and the repair
// loop (the graph-index "Durable recovery" contract, gh#474): authoritative
// absence deletes the derived record; presence re-queues through
// queueEntityForEmbedding, the sole hop-1 record writer. Reading INSIDE the
// lock is the mechanism — whatever this Get returns, the watcher's tombstone
// delete either already ran (the absence branch deletes) or serializes after
// this reconcile and wins (#629); the between-Get-and-Put interleaving is
// structurally removed.
func (c *Component) reconcileEntity(ctx context.Context, entityID string) {
	c.hop1Mu.Lock()
	defer c.hop1Mu.Unlock()

	entry, err := c.entityStatesBucket.Get(ctx, entityID)
	if err != nil {
		// Full JetStream absence sentinel set via errors.Is — ErrKeyNotFound AND
		// ErrKeyDeleted, never `==` (natsclient.IsKVNotFoundError covers both;
		// graph-index's reconcileEntity precedent).
		if natsclient.IsKVNotFoundError(err) {
			// Authoritative absence: converge the derived record to absent (not a
			// silent drain — the pre-#629 batch path skipped the delete and left a
			// dead vector queryable).
			var delErr error
			if c.storage != nil {
				if delErr = c.storage.DeleteEmbedding(ctx, entityID); delErr != nil {
					c.logger.Warn("reconcile: derived-record delete still failing",
						slog.String("entity", entityID),
						slog.Any("error", delErr))
				}
			}
			// Max-rev drain (existing idiom): the coalescer discarded the exact
			// source revisions, and revlag.Watermark.Complete drains only this key's
			// CURRENTLY-PENDING revisions — it records no future floor, so ^uint64(0)
			// is safe and is a no-op on a repair re-drive with nothing pending.
			// The max-rev OutcomeDeleted also clears ANY current-failed entry (no
			// revision, stranding floor included, sits above ^uint64(0)) — this IS
			// the explicit convergence clear for the absence branch, and it is what
			// decrements FailedCount when a repair converges. So on a FAILED delete
			// the mark below must re-add, or one failed repair pass would permanently
			// clear the mark and recreate the #625 leak.
			c.completeEmbedding(entityID, ^uint64(0), embedding.OutcomeDeleted, "")
			if delErr != nil {
				// No authoritative revision exists for an absent key: strand at
				// ^uint64(0), cleared only by explicit convergence (#722 B1) —
				// repair's 30s cadence bounds the extra degraded window.
				c.markStranded(entityID, embedding.ReasonDeleteFailed, ^uint64(0))
			}
			return
		}
		// Transient authoritative-read failure. Drain FIRST (fails toward
		// caught-up — a transient Get failure must not pin the watermark, ADR-066
		// §3; OutcomeSkipped: a drain is not a failure), THEN mark: the drain's
		// map-clear runs at max-rev and would remove a mark made before it.
		// Previously this branch only drained; now the entity also counts degraded
		// and the repair loop re-reads until the source answers (#625). A failed
		// Get yields no authoritative revision, so the stranding floor is
		// ^uint64(0): no external terminal can clear it — only a later reconcile
		// convergence does (#722 B1).
		c.logger.Debug("reconcile: authoritative entity read failed; will repair",
			slog.String("entity", entityID),
			slog.Any("error", err))
		c.completeEmbedding(entityID, ^uint64(0), embedding.OutcomeSkipped, "")
		c.markStranded(entityID, embedding.ReasonEntityReadFailed, ^uint64(0))
		return
	}

	c.queueEntityForEmbedding(ctx, entry.Key(), entry.Revision(), entry.Value())
}

// markStranded records entityID as holding a failed DERIVED write/read (#625):
// it enters the same current-failed accounting as embedder-side failures (so
// the producer reports degraded, never ready, while stranded) under one of the
// in-memory-only Reason* constants — never persisted to a stored record, never
// passed to SaveFailed.
//
// strandedAt is the CAUSAL floor of the mark (#722 B1, replacing the falsified
// floor-0 rule): an external terminal clears the mark only when its
// sourceRevision >= strandedAt, so an OBSOLETE hop-2 terminal already in
// flight for an older revision — hop 2 runs outside the hop-1 seam — cannot
// masquerade as convergence. Sites pass the revision the stranding is causally
// bound to (the tombstone revision, the delivered revision) or ^uint64(0) when
// no authoritative revision is in hand (a failed reconcile read/absence
// delete), which makes explicit clearStranded convergence the ONLY clear.
// The mark is never a pin: every hop-1 convergence path (successful
// delete/skip/queue, and reconcile's absence drain at max-rev) discharges it.
//
// Writes directly rather than through applyTerminalOutcome: a stranding at the
// current delivery/tombstone revision must overwrite an older embedder-side
// entry (delivery is monotonic, so the stranding is the newer fact), while
// held.rev — the embedder-side revision-CAS baseline — is preserved.
func (c *Component) markStranded(entityID, reason string, strandedAt uint64) {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	if c.failed == nil {
		c.failed = make(map[string]failureInfo)
	}
	held := c.failed[entityID]
	held.reason = reason
	held.strandedAt = strandedAt
	if held.at.IsZero() {
		held.at = time.Now()
	}
	c.failed[entityID] = held
	c.setFailedGauge(len(c.failed))
}

// clearStranded discharges a stranding obligation after a hop-1 convergence
// (#722 B1): a successful delete, skip, or queue under the seam means the
// derived record now reflects authoritative state, so the repair obligation is
// complete regardless of revision arithmetic — this is the explicit half of
// the causal-clear invariant (the half that keeps a ^uint64(0)-stranded mark
// clearable). It never touches an embedder-side failure entry (strandedAt 0):
// those clear only through applyTerminalOutcome's revision-guarded terminals.
func (c *Component) clearStranded(entityID string) {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	held, present := c.failed[entityID]
	if !present || held.strandedAt == 0 {
		return
	}
	delete(c.failed, entityID)
	c.setFailedGauge(len(c.failed))
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

// processEntityBatch reconciles each coalesced entity against authoritative
// ENTITY_STATES at execution time. It is invoked by the CoalescingSet after the
// debounce window elapses; each entity converges through the hop-1 seam
// (reconcileEntity), so a stale queued flush cannot clobber a newer write or
// resurrect a tombstoned entity (#629).
func (c *Component) processEntityBatch(ctx context.Context, entityIDs []string) {
	c.logger.Debug("processing coalesced entity batch", slog.Int("count", len(entityIDs)))

	for _, entityID := range entityIDs {
		if ctx.Err() != nil {
			return
		}
		c.reconcileEntity(ctx, entityID)
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
//
// It is the SOLE hop-1 record writer, and every caller holds hop1Mu (#629): the
// immediate-mode watcher update and reconcileEntity's presence branch. Keeping
// creation behind this one writer is also what preserves the #638 IdentityText
// rolling-upgrade contract by construction — no other path can shape a pending
// record.
func (c *Component) queueEntityForEmbedding(ctx context.Context, entityID string, sourceRevision uint64, data []byte) {
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
		// terminal: deliberately skipped (OutcomeSkipped — not a failure). A
		// deliberate skip is a hop-1 convergence, so it also discharges any
		// stranding (the revision-guarded completion alone cannot clear a
		// ^uint64(0)-stranded mark).
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		c.clearStranded(entityID)
		return
	}

	// ContentStorable path: take the StorageRef fetch only when its exact owning
	// StorageInstance is currently registered through the admitted store-read port.
	//
	// #264 (ADR-055 Wave 0) began lifting StorageRef onto the EntityState at the
	// ingest seam. An unresolved owner used to fall through to an unrelated owned
	// store and could read the wrong body. Exact membership now decides whether to
	// queue the reference; a miss explicitly excludes the body and continues with
	// inline text rather than creating a content failure (#875).
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
		// EMBEDDING_INDEX, and — the harmful case — an entity with a previously
		// GENERATED vector that transitions to no-text still holds a served
		// StatusGenerated record: leaving it makes semantic search keep returning a
		// stale vector for a LIVE entity. Clearing the in-memory failed map (via the
		// Skipped completion below) without deleting the record leaves durable and
		// in-memory state divergent. Deleting makes the no-text terminal consistent
		// with the worker's own no-text path, which also deletes. A delete failure is
		// logged, never fatal, and never skips the completion below (a stranded delete
		// must not pin the watermark — ADR-066 §3).
		var delErr error
		if c.storage != nil {
			if delErr = c.storage.DeleteEmbedding(ctx, entityID); delErr != nil {
				c.logger.Debug("failed to delete stale embedding for no-text entity",
					slog.String("entity", entityID), slog.Any("error", delErr))
			}
		}
		// Terminal: telemetry-only / no-text entities never reach hop 2. Completing
		// here is what makes Target=LastSeq reachable (else embedding.ready deadlocks
		// on every text-less entity — ADR-066 §3). OutcomeSkipped: no-text is not a
		// failure, and it clears any prior failed-map entry for this entity.
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		if delErr != nil {
			// This delete is a MEMBER of the failed-derived-delete class (#625) —
			// reached by the immediate watcher, the coalesced reconcile, AND repair —
			// so a failure must strand like the tombstone site's, or (a) a live
			// entity's stale GENERATED vector stays queryable while readiness reports
			// ready, unrepaired because unmarked, and (b) on a repair re-drive the
			// fresh-revision Skipped above CLEARS any prior mark it causally covers,
			// so degraded would clear WITHOUT convergence (repair-masking).
			// Drain-THEN-mark ordering is what lets this re-mark survive that clear;
			// stranded at the delivered revision, so an obsolete in-flight terminal
			// cannot clear it (#722 B1) — a later successful pass discharges it.
			c.markStranded(entityID, embedding.ReasonDeleteFailed, sourceRevision)
			return
		}
		// Successful delete/skip = hop-1 convergence: discharge any stranding the
		// revision-guarded completion above could not causally clear.
		c.clearStranded(entityID)
		return
	}

	// Hop 1 no longer derives the dedup key. Hop 2 derives it over the resolved and
	// truncated bytes it is about to embed (#623), which keys the inline and offloaded
	// lanes identically and folds in the effective cap for free. The pending record is
	// a reference, not a key, so its ContentHash is written empty.
	//
	// Queue for embedding generation through the GUARDED writer (#722 B2): a
	// generated record at a same-or-newer source revision must not be downgraded
	// to pending by a stale repair re-drive or a restart's re-delivery — the
	// guarded skip below is that lane's terminal.
	//
	// A transient save failure wrote NO durable record — the entity is neither
	// generated nor durably failed, just un-queued at this revision. Do NOT
	// complete the watermark here (#613 F2): completing would report this
	// revision done over work that never persisted. Leave the revision
	// uncompleted; the ENTITY_STATES watcher re-delivers the entity on its next
	// write, and a sustained KV outage degrades honestly via the
	// completions-based stuck detector rather than reporting a false-ready. This
	// is the SAME non-terminal treatment hop 2 gives a SaveFailed/CAS
	// persistence miss.
	saved, err := c.storage.SavePendingGuarded(ctx, &embedding.Record{
		EntityID:       entityID,
		ContentHash:    "",
		SourceText:     text,
		SourceRevision: sourceRevision,
	})
	if err != nil {
		c.logger.Error("failed to queue embedding; leaving revision uncompleted for re-delivery",
			slog.String("entity", entityID),
			slog.Any("error", err))
		// #625: also enter the current-failed accounting (stranded at the
		// delivered revision — see markStranded) so the failure surfaces as
		// degraded immediately and the repair loop re-queues from authoritative
		// state. The watermark stays uncompleted above, unchanged.
		c.markStranded(entityID, embedding.ReasonPendingWriteFailed, sourceRevision)
		return
	}
	if !saved {
		// Guarded skip: a generated vector at a same-or-newer source revision
		// already stands (#722 B2 — a stale repair re-drive, or a restart's
		// last-per-subject re-delivery). This delivered revision's work is
		// already reflected, so it is TERMINAL here: complete it (the watermark
		// drains; OutcomeSkipped — not a failure) and discharge any stranding
		// (the skip is a hop-1 convergence).
		c.logger.Debug("pending write skipped: generated record at same-or-newer revision stands",
			slog.String("entity", entityID))
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		c.clearStranded(entityID)
		return
	}
	// Queue success = hop-1 convergence: the pending record now reflects
	// authoritative state and hop 2 owns the terminal. Discharge any stranding —
	// FailedCount accounting returns to the ordinary pipeline semantics (a
	// pending record is not a failure; readiness is carried by the watermark).
	c.clearStranded(entityID)

	c.logger.Debug("queued embedding for generation",
		slog.String("entity", entityID),
		slog.Int("text_length", len(text)))
}

// shouldFetchViaStorageRef reports whether an entity's offloaded content should
// be fetched via its StorageRef. It requires a StorageRef AND a store that can
// serve it. Another registered instance is not equivalent: StorageInstance is
// the logical owner identity, not a hint from which a reader may choose a bucket.
// A miss excludes only the offloaded body, continues through inline extraction,
// and reports the exclusion loudly (gh#414/#875).
func (c *Component) shouldFetchViaStorageRef(state *graph.EntityState) bool {
	if state.StorageRef == nil {
		return false
	}
	if c.storeRegistry != nil {
		if _, ok := c.storeRegistry.Streamable(state.StorageRef.StorageInstance); ok {
			return true
		}
	}
	return false
}

// reportOffloadedContentExcluded makes the silent-loss case observable (gh#414):
// an entity carries a StorageRef (its BODY is offloaded to a store, NOT inline)
// but its exact StorageInstance is not registered, so the inline path cannot see
// that body and it is EXCLUDED from the embedding (and thus BM25/search). The
// entity may still be embedded from any inline text triples it carries — only the
// offloaded body is lost. A per-entity metric carries the count; the warning fires
// once so the log is a single actionable line, not a flood. Fix on the operator
// side: start or restore the owning storage component.
func (c *Component) reportOffloadedContentExcluded(entityID, storageInstance string) {
	if c.metrics != nil {
		c.metrics.recordContentUnresolved()
	}
	c.noContentStoreWarn.Do(func() {
		c.logger.Warn("offloaded body EXCLUDED from embeddings: no live store is registered "+
			"for the StorageRef's exact StorageInstance — start or restore that storage "+
			"component (inline text, if any, is still embedded) (gh#414/#875)",
			slog.String("entity", entityID),
			slog.String("storage_instance", storageInstance))
	})
	c.logger.Debug("entity StorageInstance is unresolved; continuing with inline text",
		slog.String("entity", entityID), slog.String("storage_instance", storageInstance))
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

	// Queue for embedding generation with storage reference, through the SAME
	// guarded writer as the inline lane (#722 B2 — SourceText stays EMPTY on the
	// offloaded record, the #635 rolling-upgrade contract; see Record.IdentityText).
	// A transient save failure wrote NO durable record, so leave the revision
	// uncompleted — the ENTITY_STATES watcher re-delivers on the next write
	// (#613 F2, same non-terminal treatment as the inline path and hop 2's
	// persistence miss). Completing here would report the revision done over work
	// that never persisted.
	saved, err := c.storage.SavePendingGuarded(ctx, &embedding.Record{
		EntityID:       entityID,
		ContentHash:    contentHash,
		IdentityText:   identityText,
		StorageRef:     storageRef,
		SourceRevision: sourceRevision,
	})
	if err != nil {
		c.logger.Error("failed to queue embedding with storage ref; leaving revision uncompleted for re-delivery",
			slog.String("entity", entityID),
			slog.Any("error", err))
		// #625: same stranding as the inline lane — degraded now, repaired by the
		// background loop; watermark untouched.
		c.markStranded(entityID, embedding.ReasonPendingWriteFailed, sourceRevision)
		return
	}
	if !saved {
		// Guarded skip (#722 B2): a generated vector at a same-or-newer source
		// revision stands. Terminal for this delivered revision; discharge any
		// stranding (hop-1 convergence).
		c.logger.Debug("offloaded pending write skipped: generated record at same-or-newer revision stands",
			slog.String("entity", entityID))
		c.completeEmbedding(entityID, sourceRevision, embedding.OutcomeSkipped, "")
		c.clearStranded(entityID)
		return
	}
	c.clearStranded(entityID) // queue success = hop-1 convergence (see inline lane)

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
