// Package rule provides a rule processing component that implements
// the Discoverable interface for processing message streams through rules
package rule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	message "github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Static interface checks - compile-time verification
var _ component.Discoverable = (*Processor)(nil)

// schema defines the configuration schema for rule processor component
// Generated from Config struct tags using reflection
var schema = buildRuleProcessorSchema()

// RuleMetrics and newRuleMetrics are in metrics.go
// Config and NewConfig are in config.go

// Processor is a component that processes messages through rules
type Processor struct {
	// Component interface implementation
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
	health      component.HealthStatus
	flowMetrics component.FlowMetrics

	// Rule processing resources
	natsClient          *natsclient.Client
	graphEventPublisher graphEventPublisher
	rules               map[string]Rule // Self-loaded rules; written by applyRuleChanges under mu.Lock, read under mu.RLock.
	// ruleDefinitions holds the parsed Definition for each rule. Writers:
	// loadRules (called only from Initialize, before any goroutines start) and
	// applyRuleChanges (hot-reload path). Both write under mu.Lock. Reads in
	// message_handler.go are snapshotted under mu.RLock alongside rp.rules so
	// that Definition and Rule are always in sync.
	ruleDefinitions map[string]Definition
	ruleConfigs     map[string]map[string]any // Original rule configurations for GetRuntimeConfig

	// matchCounters tracks per-rule match counts for FireEveryNEvents gating.
	// Keyed by ruleID. Each counter is an atomic.Int64 so increments and reads
	// are safe without holding mu. The map itself is only written under mu.Lock
	// (in loadRules and applyRuleChanges), so map reads may occur under mu.RLock
	// once the processor is running.
	matchCounters map[string]*atomic.Int64

	// Message cache
	messageCache cache.Cache[message.Message]

	// Configuration
	config *Config

	// Dependencies
	metricsRegistry *metric.MetricsRegistry

	// toolRegistry is the shared tool executor registry plumbed
	// through component.Dependencies.ToolRegistry. Forwarded to the
	// ActionExecutor at Initialize so publish_agent default_tools
	// resolves against it. Nil when no agentic-tools is wired into
	// the deployment (graph-only flows, etc.).
	toolRegistry component.ToolRegistryReader

	// lifecycleManager is the shared pkg/lifecycle.Manager plumbed
	// through component.Dependencies.LifecycleManager (or a wrapped
	// equivalent). Forwarded to the ActionExecutor at Initialize so
	// the lifecycle_* action family (ADR-047) can move Participants
	// through declared phases. Nil when no Lifecycle harness is wired
	// into the deployment — lifecycle_* actions surface an error
	// rather than silently no-op'ing.
	lifecycleManager LifecycleManager

	// decoder unmarshal incoming BaseMessage envelopes against the
	// shared payload registry. Set via SetDecoder after construction
	// (the existing NewProcessor signature predates Dependencies-based
	// wiring; SetDecoder mirrors the SetToolRegistry pattern).
	decoder *message.Decoder

	// Runtime state
	running            bool          // Tracks if processor is running (protected by mu)
	shutdown           chan struct{} // Closed to signal shutdown, never set to nil while running
	done               chan struct{}
	ready              chan struct{} // Closed when run() completes initialization
	startTime          time.Time
	messagesEvaluated  int64
	rulesTriggered     int64
	eventsPublished    int64 // New metric for event publishing
	errorCount         int64
	lastError          string
	lastActivity       time.Time
	lastEvaluationTime time.Time // Last time rules were evaluated
	mu                 sync.RWMutex

	// graphStateResetRequired is sticky for the lifetime of the process. Once
	// any ENTITY_STATES value violates the authoritative graph-state contract,
	// rule evaluation must stop rather than derive output from a partial view.
	graphStateResetRequired  atomic.Bool
	graphStateGuardRequired  atomic.Bool
	graphStateGuardReady     atomic.Bool
	graphStateGuardDegraded  atomic.Bool
	graphStateGuardReadyCh   chan struct{}
	graphStateGuardDone      chan struct{}
	graphStateGuardReadyOnce sync.Once
	graphStateGuardDoneOnce  sync.Once
	graphStateGuardRevision  atomic.Uint64
	graphStateProgressMu     sync.Mutex
	graphStateProgress       chan struct{}

	// Active subscriptions flag
	isSubscribed bool

	// NATS subscriptions for cleanup
	subscriptions []*natsclient.Subscription

	// JetStream consumer for entity events
	entityConsumer jetstream.Consumer

	// KV watchers for entity state changes
	// Maps pattern string to watcher for dynamic management
	entityWatchers        []jetstream.KeyWatcher
	entityWatcherMap      map[string]jetstream.KeyWatcher
	entityWatcherCancels  map[string]context.CancelFunc
	entityWatcherUpdateMu sync.Mutex
	entityDispatchGate    sync.RWMutex
	entityDispatchRecords map[string]managedEntityWatcher
	entityNextGeneration  uint64
	entityBeforeDispatch  func()
	entityBeforeEvalLock  func(string)
	watcherCtx            context.Context    // Context for watcher goroutines
	watcherCancelFunc     context.CancelFunc // Cancel function for stopping all watchers

	// Entity coalescer for batched rule evaluation
	entityCoalescer       *cache.CoalescingSet
	entityEvaluationFence entityEvaluationFence

	// Prometheus metrics
	metrics *Metrics

	// Stateful rule support. Both fields are set once in Initialize (before any
	// goroutines start) and never reassigned at runtime, so they may be read
	// without holding mu.
	stateTracker      *StateTracker
	statefulEvaluator *StatefulEvaluator

	// actionExecutor is shared between the StatefulEvaluator (message-path
	// and KV-watch firings) and the CronScheduler (time-driven firings).
	// Constructed in initializeStateTracker; read without holding mu since
	// it is set once before any goroutine that uses it can start.
	actionExecutor ActionExecutorInterface

	// cronRules holds parsed CronRule definitions, keyed by rule ID. Cron
	// rules live in a parallel registry from rp.rules because CronRule does
	// not implement the Rule interface (Subscribe/Evaluate/ExecuteEvents are
	// message-driven concepts that don't fit time-driven firing). Writers:
	// loadRules and applyRuleChanges, both under mu.Lock; reads only happen
	// from those same paths so the map needs no separate synchronization.
	cronRules map[string]*CronRule

	// cronScheduler dispatches CronRule actions on cron schedules. Created
	// in initializeCronScheduler (called from Start, after the state tracker
	// and ActionExecutor are ready). Started in run(), drained in Stop().
	// Nil when no cron rules are configured AND the scheduler hasn't been
	// pre-built — currently we always build it at Start so hot-reload can
	// register rules added after startup.
	cronScheduler *CronScheduler

	// scheduleTracker persists per-rule last-fired timestamps to the
	// RULE_SCHEDULES KV bucket. Created in initializeScheduleTracker
	// (called from Start before initializeCronScheduler so the scheduler
	// can hold a reference). Nil when bucket creation fails — the
	// scheduler degrades to in-memory-only firing (no missed-fire
	// detection across restarts) rather than refusing to start.
	scheduleTracker *ScheduleTracker

	// Revision tracking for per-rule feedback loop prevention.
	// Keyed by (ruleID, entityID) → KV revision → tracked-at timestamp.
	// A watcher update at one of those revisions skips only the rule that
	// generated it; other rules watching the same bucket still evaluate.
	// Using a struct key avoids separator-collision risk from arbitrary
	// characters in rule or entity IDs. The timestamp lets a background
	// sweeper prune entries the watcher never delivered (unwatched entities,
	// watcher downtime, cross-bucket writes) so the map stays bounded.
	ownRevisions map[ruleRevKey]map[uint64]time.Time
	revisionMu   sync.Mutex

	// revisionTTL is how long an untriggered tracked revision is kept before
	// the sweeper prunes it. Longer than typical watcher delivery latency,
	// short enough to bound memory on a busy processor.
	revisionTTL time.Duration

	// Logger
	logger *slog.Logger

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// kvConfigManager is the component-internal hot-reload manager. It owns a
	// KV watcher on semstreams_config:rules.* and calls ApplyConfigUpdate when
	// the watcher fires. Constructed in Start; nil when NATS is unavailable.
	// See also: cmd/semstreams/main.go buildRuleManager — a second ConfigManager
	// instance (processor=nil) for agent CRUD tools. Both share the same KV bucket.
	kvConfigManager *ConfigManager

	// projectionOwnerToken is the typed write-lease credential minted by the
	// ownership Registry (ADR-056 PR-3.5). Set via SetProjectionOwnerToken by
	// service.BindRulePackContracts (which holds the Registry) BEFORE
	// initializeStateTracker, which forwards it to the ActionExecutor. The
	// executor stamps token.Wire() on every replace_owned request — the
	// "<owner>#<incarnation>" format lives only in pkg/ownership.
	projectionOwnerToken ownership.OwnerToken
}

// NewProcessor creates a new rule processor
func NewProcessor(natsClient *natsclient.Client, config *Config) (*Processor, error) {
	return NewProcessorWithMetrics(natsClient, config, nil)
}

// NewProcessorWithMetrics creates a new rule processor with optional metrics
func NewProcessorWithMetrics(natsClient *natsclient.Client, config *Config, metricsRegistry *metric.MetricsRegistry) (*Processor, error) {
	if config == nil {
		return nil, fmt.Errorf("rule processor config is required")
	}

	// Validate required configuration
	if config.Ports == nil {
		return nil, fmt.Errorf("rule processor config missing required Ports configuration")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Create message cache - will be initialized with context in Start()
	msgCache := cache.NewNoop[message.Message]()

	rp := &Processor{
		metadata: component.Metadata{
			Name:        "rule-processor",
			Type:        "processor",
			Description: "Processes messages through configurable rules and generates alerts",
			Version:     "1.0.0",
		},
		natsClient:             natsClient,
		rules:                  make(map[string]Rule),
		ruleDefinitions:        make(map[string]Definition),
		ruleConfigs:            make(map[string]map[string]any),
		matchCounters:          make(map[string]*atomic.Int64),
		cronRules:              make(map[string]*CronRule),
		messageCache:           msgCache,
		config:                 config,
		metricsRegistry:        metricsRegistry,
		entityWatchers:         make([]jetstream.KeyWatcher, 0),
		entityWatcherMap:       make(map[string]jetstream.KeyWatcher),
		entityWatcherCancels:   make(map[string]context.CancelFunc),
		entityDispatchRecords:  make(map[string]managedEntityWatcher),
		graphStateGuardReadyCh: make(chan struct{}),
		graphStateGuardDone:    make(chan struct{}),
		graphStateProgress:     make(chan struct{}),
		ownRevisions:           make(map[ruleRevKey]map[uint64]time.Time),
		revisionTTL:            defaultRevisionTTL,
		health: component.HealthStatus{
			Healthy:    true,
			LastCheck:  time.Now(),
			ErrorCount: 0,
			Uptime:     0,
		},
		flowMetrics: component.FlowMetrics{
			MessagesPerSecond: 0,
			BytesPerSecond:    0,
			ErrorRate:         0,
			LastActivity:      time.Now(),
		},
		isSubscribed: false,
		metrics:      newRuleMetrics(metricsRegistry, "rule"),
		logger:       slog.Default().With("component", "rule-processor"),
	}
	if natsClient != nil {
		rp.graphEventPublisher = natsClient
	}

	// Set up input and output ports
	rp.setupPorts()

	// Note: entityCoalescer will be initialized in Start() when we have a context

	return rp, nil
}

// SetToolRegistry installs the shared tool registry. Called by the
// component factory after construction with deps.ToolRegistry. The
// registry flows from here to ActionExecutor at Initialize time so
// publish_agent's default_tools resolution sees the right tools.
//
// A nil arg is allowed and disables tool name resolution (deployments
// without agentic-tools).
func (rp *Processor) SetToolRegistry(r component.ToolRegistryReader) {
	rp.toolRegistry = r
}

// SetDecoder installs the payload Decoder used to unmarshal incoming
// BaseMessage envelopes. Called by the component factory after
// construction with message.NewDecoder(deps.PayloadRegistry). Mirrors
// SetToolRegistry. A nil arg leaves the processor unable to handle
// semantic messages — handleSemanticMessage will fail-fast.
func (rp *Processor) SetDecoder(d *message.Decoder) {
	rp.decoder = d
}

// SetLifecycleManager installs the pkg/lifecycle.Manager used by the
// lifecycle_* action family (ADR-047). Mirrors SetToolRegistry/
// SetDecoder. A nil arg disables the actions — lifecycle_* dispatch
// surfaces a wiring-error rather than silently succeeding.
func (rp *Processor) SetLifecycleManager(m LifecycleManager) {
	rp.lifecycleManager = m
}

// setupPorts initializes input and output port definitions. Ports
// configuration is validated in the constructor, so config.Ports is
// guaranteed non-nil.
func (rp *Processor) setupPorts() {
	rp.inputPorts = make([]component.Port, len(rp.config.Ports.Inputs))
	for i, portDef := range rp.config.Ports.Inputs {
		rp.inputPorts[i] = convertDefinitionToPort(portDef, component.DirectionInput)
	}

	rp.outputPorts = make([]component.Port, len(rp.config.Ports.Outputs))
	for i, portDef := range rp.config.Ports.Outputs {
		rp.outputPorts[i] = convertDefinitionToPort(portDef, component.DirectionOutput)
	}
}

// Meta returns component metadata
func (rp *Processor) Meta() component.Metadata {
	return rp.metadata
}

// InputPorts returns declared input ports
func (rp *Processor) InputPorts() []component.Port {
	return rp.inputPorts
}

// OutputPorts returns declared output ports
func (rp *Processor) OutputPorts() []component.Port {
	return rp.outputPorts
}

// ConfigSchema returns configuration schema for component interface
func (rp *Processor) ConfigSchema() component.ConfigSchema {
	return schema
}

// ProjectionBindings returns the pack-level ownership declaration for this rule
// processor: the pack id (owner becomes "rule-pack.<packID>") and the
// projection contracts the pack owns.
//
// INVARIANT (ADR-056 #278 inc 2): this is read ONCE at the composition root,
// BEFORE manager.StartAll, and the binding is NEVER re-derived on hot-reload.
// Rule-pack contracts are PACK-LEVEL and STATIC — they are not per-rule and do
// not change when the rule set hot-reloads. Adding a re-bind call anywhere
// downstream of StartAll would violate the ownership-epoch invariant.
//
// The processor is substrate-agnostic: it returns a declaration and never
// touches the ownership registry. All binding happens main-side.
func (rp *Processor) ProjectionBindings() (packID string, contracts []projection.Contract) {
	return rp.config.PackID, rp.config.ProjectionContracts
}

// SetProjectionOwnerToken stores the typed write-lease credential minted by the
// ownership Registry so initializeStateTracker can forward it to the
// ActionExecutor. Must be called by service.BindRulePackContracts BEFORE Start
// (which calls initializeStateTracker). Mirrors SetProjectionOwner's timing
// contract. A zero token is tolerated — test paths and unowned packs stamp an
// empty wire string, which the lease check skips.
func (rp *Processor) SetProjectionOwnerToken(token ownership.OwnerToken) {
	rp.projectionOwnerToken = token
}

// Health returns current health status
func (rp *Processor) Health() component.HealthStatus {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	rp.health.LastCheck = time.Now()
	rp.health.ErrorCount = int(atomic.LoadInt64(&rp.errorCount))
	if !rp.startTime.IsZero() {
		rp.health.Uptime = time.Since(rp.startTime)
	}

	return rp.health
}

// DataFlow returns current data flow metrics
func (rp *Processor) DataFlow() component.FlowMetrics {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	// Calculate messages per second based on recent activity
	evaluated := atomic.LoadInt64(&rp.messagesEvaluated)
	if !rp.startTime.IsZero() && evaluated > 0 {
		duration := time.Since(rp.startTime).Seconds()
		if duration > 0 {
			rp.flowMetrics.MessagesPerSecond = float64(evaluated) / duration
		}
	}

	// Error rate calculation
	if evaluated > 0 {
		rp.flowMetrics.ErrorRate = float64(atomic.LoadInt64(&rp.errorCount)) / float64(evaluated)
	}

	rp.flowMetrics.LastActivity = rp.lastActivity

	return rp.flowMetrics
}

// Initialize loads rules and prepares the processor
func (rp *Processor) Initialize() error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Load rules based on configuration
	if err := rp.loadRules(); err != nil {
		return errs.Wrap(err, "RuleProcessor", "initialize", "load rules")
	}

	rp.logger.Info("Rule processor initialized", "rule_count", len(rp.rules))
	return nil
}

// watchEntityStates and handleEntityUpdates are in entity_watcher.go
// loadRuleDefinitionsFromFiles and loadRules are in rule_loader.go

// run is the main background goroutine that handles processor lifecycle
func (rp *Processor) run(ctx context.Context) {
	defer close(rp.done)

	// Use sync.Once to safely close ready channel - handles both happy path
	// (explicit close after coalescer init) and error paths (defer on early return)
	var readyOnce sync.Once
	signalReady := func() { readyOnce.Do(func() { close(rp.ready) }) }
	defer signalReady() // Ensure ready is closed if run() exits early

	// Initialize entity coalescer BEFORE spawning watchers to avoid race condition.
	// Watchers read entityCoalescer, so it must be set before any watcher goroutine starts.
	// Only create coalescer if debounce delay is non-zero.
	// When debounce is 0, entities are evaluated immediately without batching.
	if rp.config.DebounceDelayMs > 0 {
		rp.entityCoalescer = cache.NewCoalescingSet(ctx, rp.config.DebounceDelayMs, func(entityIDs []string) {
			rp.evaluateEntitiesInBatch(ctx, entityIDs)
		})
	}

	// Signal that initialization is complete - entityCoalescer is now safe to read
	signalReady()

	// Start KV watchers for entity state changes FIRST
	if err := rp.watchEntityStates(ctx); err != nil {
		rp.logger.Warn("Failed to start entity state watching", "error", err)
		// Don't fail - rules can still process semantic messages
	}

	// Subscribe to input subjects
	if err := rp.setupSubscriptions(ctx); err != nil {
		rp.logger.Error("Failed to setup subscriptions", "error", err)
		return
	}

	// Start the cron scheduler now that watchers and subscriptions are up.
	// Doing this after setupSubscriptions guarantees the publisher's NATS
	// subjects are ready before the first cron tick can dispatch a publish
	// action. The deferred Stop drains in-flight fires when run() returns
	// (either via shutdown or ctx cancellation) before the rest of the
	// processor's resources are torn down in Stop().
	if rp.cronScheduler != nil {
		if err := rp.cronScheduler.Start(ctx); err != nil {
			rp.logger.Warn("Failed to start cron scheduler", "error", err)
			// Drop the scheduler reference so RegisteredCount and any
			// future metrics gauge cannot read a stale never-started
			// instance. Subsequent hot-reload Register/Deregister calls
			// fall through their `!= nil` guards and become no-ops, which
			// matches the "scheduler unavailable" semantics.
			rp.mu.Lock()
			rp.cronScheduler = nil
			rp.mu.Unlock()
		} else {
			defer rp.drainCronScheduler()
		}
	}

	// NOW mark healthy - watchers established, subscriptions ready
	rp.mu.Lock()
	rp.health.Healthy = true
	rp.health.LastCheck = time.Now()
	rp.mu.Unlock()
	rp.logger.Info("Rule processor ready - watchers and subscriptions established")

	// Wait for shutdown signal or context cancellation
	select {
	case <-rp.shutdown:
		rp.logger.Info("Rule processor shutdown requested")
	case <-ctx.Done():
		rp.logger.Info("Rule processor context cancelled", "error", ctx.Err())
	}
}

// cronSchedulerDrainTimeout bounds how long run() will wait for in-flight
// cron fires to complete on shutdown. Keeping it under the processor's own
// 5-second Stop grace period avoids stacking timeouts; the worst case is a
// single fire that's still running when the timer expires, which is logged
// and abandoned (its action errors are already best-effort).
const cronSchedulerDrainTimeout = 3 * time.Second

// drainCronScheduler stops the scheduler and waits for in-flight fires to
// complete, bounded by cronSchedulerDrainTimeout. Called from run() via a
// deferred call so it executes after the shutdown select returns.
func (rp *Processor) drainCronScheduler() {
	if rp.cronScheduler == nil {
		return
	}
	stopCtx := rp.cronScheduler.Stop()
	select {
	case <-stopCtx.Done():
		rp.logger.Debug("Cron scheduler drained cleanly")
	case <-time.After(cronSchedulerDrainTimeout):
		rp.logger.Warn("Cron scheduler drain timeout — abandoning in-flight fires",
			"timeout", cronSchedulerDrainTimeout)
	}
}

// initializeStateTracker creates the RULE_STATE KV bucket and initializes state tracking components.
// This enables stateful ECA rules with OnEnter/OnExit/WhileTrue actions.
func (rp *Processor) initializeStateTracker(ctx context.Context) error {
	// Get or create the RULE_STATE KV bucket
	const bucketName = "RULE_STATE"

	js, err := rp.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}

	// Try to get existing bucket first
	bucket, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		// Bucket doesn't exist - create it
		kvConfig := jetstream.KeyValueConfig{
			Bucket:      bucketName,
			Description: "Rule match state tracking for stateful ECA rules",
			TTL:         0,  // No expiration by default
			MaxBytes:    -1, // No size limit
			History:     1,  // Keep only current state
		}

		bucket, err = js.CreateKeyValue(ctx, kvConfig)
		if err != nil {
			return fmt.Errorf("create RULE_STATE bucket: %w", err)
		}

		rp.logger.Info("Created RULE_STATE KV bucket for stateful rules")
	} else {
		rp.logger.Info("Using existing RULE_STATE KV bucket")
	}

	// Create StateTracker
	rp.stateTracker = NewStateTracker(bucket, rp.logger)

	// Create ActionExecutor with triple mutation support
	// The tripleMutator uses NATS request/response to persist triples and tracks
	// KV revisions to prevent feedback loops in rule evaluation.
	// The publisher enables publish actions to send messages to NATS subjects.
	var actionExecutor ActionExecutorInterface
	if rp.natsClient != nil {
		publisher := newActionPublisher(rp)
		kvWriter := newNATSKVWriter(rp.natsClient, rp.logger)
		if rp.config.EnableGraphIntegration {
			mutator := newTripleMutator(rp.natsClient, rp)
			actionExecutor = NewActionExecutorComplete(rp.logger, mutator, publisher, kvWriter)
			rp.logger.Info("ActionExecutor initialized with triple mutation, publishing, and KV write support")
		} else {
			actionExecutor = NewActionExecutorComplete(rp.logger, nil, publisher, kvWriter)
			rp.logger.Info("ActionExecutor initialized with publishing and KV write support (graph integration disabled)")
		}
	} else {
		actionExecutor = NewActionExecutor(rp.logger)
		rp.logger.Info("ActionExecutor initialized without NATS support")
	}

	// Propagate the shared tool registry down so publish_agent's
	// default_tools resolution uses it. Type-asserts because
	// ActionExecutorInterface is the test-friendly minimum surface;
	// the concrete *ActionExecutor implementation owns the field.
	if setter, ok := actionExecutor.(interface {
		SetToolRegistry(component.ToolRegistryReader)
	}); ok {
		setter.SetToolRegistry(rp.toolRegistry)
	}

	// Propagate the Lifecycle harness Manager (ADR-047) so the
	// lifecycle_* action family can dispatch transitions, AND so the
	// stateful evaluator can resolve `$entity.lifecycle.*` condition
	// fields against the trigger entity's Participant. Same
	// type-assert pattern as SetToolRegistry — keeps concrete-field
	// knowledge in the implementations.
	if rp.lifecycleManager != nil {
		if setter, ok := actionExecutor.(interface {
			SetLifecycleManager(LifecycleManager)
		}); ok {
			setter.SetLifecycleManager(rp.lifecycleManager)
		}
	}

	// ADR-055 §3a: wire the framework verdict auditor so deny/approve actions
	// record governance verdicts to the append-only GOVERNANCE_VERDICT_AUDIT
	// stream. Wired independently of the operator Publisher — audit is a
	// framework guarantee, not an operator opt-in, so config drift cannot
	// silently disable it. Only when a NATS client is present (the auditor
	// publishes to a stream).
	if rp.natsClient != nil {
		if setter, ok := actionExecutor.(interface {
			SetVerdictAuditor(VerdictAuditor)
		}); ok {
			setter.SetVerdictAuditor(newVerdictAuditor(rp))
		}
	}

	// ADR-056 Decision 3: wire the rule pack's projection-owner identity onto
	// the executor so replace_owned actions can thread it to the mutator
	// boundary. The owner is "rule-pack.<PackID>" — the SAME identity the
	// composition root binds the pack's ProjectionContracts under (inc 2).
	// Config validation makes PackID universally present before construction.
	// Same type-assert setter pattern as SetToolRegistry / SetLifecycleManager.
	if setter, ok := actionExecutor.(interface {
		SetProjectionOwner(string)
	}); ok {
		setter.SetProjectionOwner("rule-pack." + rp.config.PackID)
	}
	// ADR-056 PR-3.5: forward the typed OwnerToken so the executor can stamp
	// its wire form on every replace_owned request. The token is minted by
	// the ownership Registry and set by service.BindRulePackContracts (which
	// holds the Registry) BEFORE Start is called. A zero token (Registry not
	// wired) yields an empty wire string — correct for test paths where the
	// ownership registry is intentionally absent.
	if !rp.projectionOwnerToken.IsZero() {
		if setter, ok := actionExecutor.(interface {
			SetProjectionOwnerToken(ownership.OwnerToken)
		}); ok {
			setter.SetProjectionOwnerToken(rp.projectionOwnerToken)
		}
	}

	// Persist the executor on the processor so the cron scheduler
	// (initializeCronScheduler) can dispatch through the same instance
	// the StatefulEvaluator uses. Single shared executor keeps publishing
	// semantics, triple-mutation feedback-loop tracking, and tool-registry
	// resolution identical across the message-path, KV-watch, and
	// time-driven firing paths.
	rp.actionExecutor = actionExecutor

	// Create StatefulEvaluator
	rp.statefulEvaluator = NewStatefulEvaluator(rp.stateTracker, actionExecutor, rp.logger)
	// ADR-047: same setter pattern as ActionExecutor so the evaluator
	// can resolve $entity.lifecycle.* condition fields.
	if rp.lifecycleManager != nil {
		rp.statefulEvaluator.SetLifecycleManager(rp.lifecycleManager)
	}

	rp.logger.Info("State tracker initialized for stateful ECA rules")
	return nil
}

// initializeScheduleTracker creates the RULE_SCHEDULES KV bucket and binds
// a ScheduleTracker to it. The bucket holds per-rule last-fired timestamps
// used by the cron scheduler for missed-fire detection on restart and by
// out-of-process readers (governance startup hooks) that need to issue
// catch-up sweeps.
//
// Bucket creation is best-effort: failure leaves rp.scheduleTracker nil
// and the scheduler runs without persistence, matching the same
// degrade-gracefully posture initializeStateTracker uses for RULE_STATE.
// History=1 (only current state matters), no TTL (last-fired records
// outlive any operationally meaningful interval), no size cap.
func (rp *Processor) initializeScheduleTracker(ctx context.Context) error {
	if rp.natsClient == nil {
		// Test paths and graph-only deployments construct the processor
		// without NATS. The scheduler tolerates a nil tracker; logging
		// at Debug avoids noise in those paths.
		rp.logger.Debug("Skipping schedule tracker init: no NATS client")
		return nil
	}

	js, err := rp.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}

	bucket, err := js.KeyValue(ctx, ScheduleBucketName)
	if err != nil {
		bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:      ScheduleBucketName,
			Description: "Per-rule last-fired timestamps for cron rule missed-fire detection",
			TTL:         0,  // Records persist for the rule's lifecycle.
			MaxBytes:    -1, // No size cap; one small record per cron rule.
			History:     1,  // Only the most recent fire matters.
		})
		if err != nil {
			return fmt.Errorf("create %s bucket: %w", ScheduleBucketName, err)
		}
		rp.logger.Info("Created RULE_SCHEDULES KV bucket for cron rule fire tracking")
	} else {
		rp.logger.Info("Using existing RULE_SCHEDULES KV bucket")
	}

	rp.scheduleTracker = NewScheduleTracker(bucket, rp.logger)
	return nil
}

// initializeCronScheduler builds the CronScheduler against the shared
// ActionExecutor and registers all cron rules already loaded by Initialize.
// It is called from Start under rp.mu.Lock so the registration loop sees a
// stable rp.cronRules snapshot.
//
// The scheduler is built unconditionally — even if rp.cronRules is empty —
// so hot-reloaded cron rules added after startup have a registry to land
// in. Returns an error if the executor isn't ready (the state tracker init
// failed and left rp.actionExecutor nil).
func (rp *Processor) initializeCronScheduler() error {
	if rp.actionExecutor == nil {
		return fmt.Errorf("cannot initialize cron scheduler: action executor not initialized")
	}

	scheduler, err := NewCronScheduler(CronSchedulerConfig{
		Executor: rp.actionExecutor,
		Tracker:  rp.scheduleTracker,
		Metrics:  getCronMetrics(rp.metricsRegistry),
		Logger:   rp.logger,
		Ready:    rp.graphRuleEvaluationReady,
	})
	if err != nil {
		return fmt.Errorf("create cron scheduler: %w", err)
	}

	for ruleID, rule := range rp.cronRules {
		if err := scheduler.Register(rule); err != nil {
			rp.logger.Warn("Failed to register cron rule, skipping",
				"rule_id", ruleID,
				"error", err)
			continue
		}
	}

	rp.cronScheduler = scheduler
	rp.logger.Info("Cron scheduler initialized",
		"registered_rules", scheduler.RegisteredCount())
	return nil
}

// Start begins processing messages through rules
func (rp *Processor) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "RuleProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "RuleProcessor", "Start", "context already cancelled")
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.running {
		return errs.WrapInvalid(errs.ErrAlreadyStarted, "RuleProcessor", "Start", "check processor state")
	}

	// Initialize message cache with context and metrics
	msgCache, err := cache.NewFromConfig[message.Message](ctx, rp.config.MessageCache,
		cache.WithMetrics[message.Message](rp.metricsRegistry, "rule_processor"),
	)
	if err != nil {
		rp.logger.Warn("Failed to create message cache, using noop cache", "error", err)
		msgCache = cache.NewNoop[message.Message]()
	}
	rp.messageCache = msgCache

	// Initialize StateTracker for stateful ECA rules
	if err := rp.initializeStateTracker(ctx); err != nil {
		rp.logger.Warn("Failed to initialize state tracker, stateful rules will be disabled", "error", err)
		// Don't fail - processor can still work with stateless rules
	}

	// Initialize ScheduleTracker for cron rule missed-fire detection.
	// Must precede initializeCronScheduler so the scheduler can hold a
	// reference. Failure leaves rp.scheduleTracker nil; the scheduler
	// then runs without persistence (no missed-fire detection across
	// restarts), which is the same posture as stateful-rule degradation.
	if err := rp.initializeScheduleTracker(ctx); err != nil {
		rp.logger.Warn("Failed to initialize schedule tracker, cron missed-fire detection disabled",
			"error", err)
	}

	// Build the cron scheduler and register any cron rules already loaded by
	// Initialize. The scheduler is constructed even when the cronRules map is
	// empty so that hot-reloaded cron rules added after startup have a place
	// to land. Failure here is non-fatal: cron rules are skipped, expression
	// rules continue to work.
	if err := rp.initializeCronScheduler(); err != nil {
		rp.logger.Warn("Failed to initialize cron scheduler, cron rules will be disabled", "error", err)
	}

	// Note: entityCoalescer is initialized in run() before spawning watchers
	// to avoid race between Start() setting it and watcher goroutines reading it

	// Initialize lifecycle reporter for observability
	if rp.natsClient != nil {
		statusBucket, err := rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
			Bucket:      "COMPONENT_STATUS",
			Description: "Component lifecycle status tracking",
		})
		if err != nil {
			rp.logger.Warn("Failed to create COMPONENT_STATUS bucket, lifecycle reporting disabled",
				slog.Any("error", err))
			rp.lifecycleReporter = component.NewNoOpLifecycleReporter()
		} else {
			rp.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
				KV:               statusBucket,
				ComponentName:    rp.metadata.Name,
				Logger:           rp.logger,
				EnableThrottling: true,
			})
		}
	} else {
		rp.lifecycleReporter = component.NewNoOpLifecycleReporter()
	}

	// Create shutdown, done, and ready channels for coordination
	rp.shutdown = make(chan struct{})
	rp.done = make(chan struct{})
	rp.ready = make(chan struct{})
	rp.running = true
	rp.startTime = time.Now()
	// Note: health.Healthy is set in run() after watchers and subscriptions are established

	// Start background goroutine with context
	go rp.run(ctx)

	// Start the revision tracker sweeper so tracked self-writes that the
	// watcher never delivers (unwatched entities, cross-bucket writes,
	// watcher downtime) don't leak memory. Capture shutdown here so a
	// subsequent Start() reassigning rp.shutdown doesn't race with this
	// goroutine's channel read.
	go rp.runRevisionSweeper(rp.shutdown, revisionSweepInterval, rp.revisionTTL)

	// Wait for run() to complete initialization (coalescer setup, watchers started)
	// This ensures entityCoalescer is set before Start() returns
	select {
	case <-rp.ready:
		// Initialization complete
	case <-ctx.Done():
		// Context cancelled during startup - trigger shutdown and return error
		close(rp.shutdown)
		return ctx.Err()
	}

	rp.isSubscribed = true

	// Wire hot-reload: component-internal ConfigManager owns a KV watcher on
	// semstreams_config:rules.* and applies changes without restart. This is
	// distinct from the Pattern-B CRUD manager in cmd/semstreams/main.go
	// (buildRuleManager, processor=nil) which handles agent tool writes.
	//
	// Launched as a goroutine so that InitializeKVStore and Watch run after
	// Start() returns and releases rp.mu. reconcileFromKV (debounced 250ms)
	// will only fire after the lock is free.
	if rp.natsClient != nil {
		go rp.startHotReloadManager(ctx)
	}

	// Count subjects for logging
	subjectCount := 0
	for _, port := range rp.config.Ports.Inputs {
		if (port.Type == "nats" || port.Type == "jetstream") && port.Subject != "" {
			subjectCount++
		}
	}

	// Report idle state after startup
	if rp.lifecycleReporter != nil {
		if err := rp.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
			rp.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
		}
	}

	rp.logger.Info("Rule processor started", "subject_count", subjectCount)
	return nil
}

// startHotReloadManager constructs and starts the component-internal KV
// hot-reload manager. It is called as a goroutine from Start() so that
// InitializeKVStore, SeedFromRuntime, and Watch run after rp.mu is released
// (all three ultimately call rp.mu.RLock or rp.mu.Lock). reconcileFromKV
// fires after a 250ms debounce, well past the point where Start() returns.
//
// If Stop() races with this goroutine and the processor is no longer running
// by the time we store the manager, we immediately stop the manager so it
// does not outlive the processor.
func (rp *Processor) startHotReloadManager(ctx context.Context) {
	rcm := NewConfigManager(rp, nil, rp.logger)
	if err := rcm.InitializeKVStore(rp.natsClient); err != nil {
		rp.logger.Warn("Failed to initialize KV store for rule hot-reload; running with file rules only",
			slog.Any("error", err))
		return
	}
	if err := rcm.Start(ctx); err != nil {
		rp.logger.Warn("Failed to start rule hot-reload watcher; running with file rules only",
			slog.Any("error", err))
		return
	}
	rp.mu.Lock()
	if rp.running {
		rp.kvConfigManager = rcm
		rp.mu.Unlock()
	} else {
		// Processor already stopped — clean up the manager we just started.
		rp.mu.Unlock()
		if err := rcm.Stop(); err != nil {
			rp.logger.Debug("Hot-reload manager stop after race with Stop() (ignored)", slog.Any("error", err))
		}
	}
}

// setupSubscriptions creates subscriptions for input subjects based on port type
func (rp *Processor) setupSubscriptions(ctx context.Context) error {
	if !rp.natsClient.IsHealthy() {
		return errs.WrapFatal(errs.ErrNoConnection, "RuleProcessor", "Start", "check NATS health")
	}

	for _, port := range rp.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		// Skip entity.events subjects since we use KV watch for entity states
		if strings.HasPrefix(port.Subject, "events.graph.entity") {
			rp.logger.Debug("Skipping subscription - using KV watch for entity states", "subject", port.Subject)
			continue
		}

		switch port.Type {
		case "jetstream":
			// JetStream subscription - use durable consumer
			if err := rp.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.Wrap(err, "RuleProcessor", "setupSubscriptions",
					fmt.Sprintf("JetStream consumer for %s", port.Subject))
			}

		case "nats":
			// Core NATS subscription
			sub, err := rp.natsClient.Subscribe(ctx, port.Subject, func(msgCtx context.Context, msg *nats.Msg) {
				rp.handleMessage(msgCtx, msg.Subject, msg.Data)
			})
			if err != nil {
				return errs.Wrap(err, "RuleProcessor", "Start", fmt.Sprintf("subscribe to %s", port.Subject))
			}
			rp.subscriptions = append(rp.subscriptions, sub)
			rp.logger.Info("Rule processor subscribed (NATS)", "subject", port.Subject)

		default:
			rp.logger.Warn("Unknown port type, skipping", "port", port.Name, "type", port.Type)
		}
	}

	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (rp *Processor) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	// Derive stream name from subject or use explicit stream name
	streamName := port.StreamName
	if streamName == "" {
		streamName = deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return fmt.Errorf("could not derive stream name for subject %s", port.Subject)
	}

	// Wait for stream to be available
	if err := rp.waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(port.Subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("rule-processor-%s", sanitizedSubject)

	rp.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: port.Subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	subject := port.Subject // capture for closure
	err := rp.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		rp.handleMessage(msgCtx, subject, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			rp.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}

	rp.logger.Info("Rule processor subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (rp *Processor) waitForStream(ctx context.Context, streamName string) error {
	js, err := rp.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			rp.logger.Debug("Stream available", "stream", streamName)
			return nil
		}

		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}

	return fmt.Errorf("stream %s not available after %d retries", streamName, maxRetries)
}

// deriveStreamName extracts stream name from subject convention.
// Convention: subject "component.action.type" → stream "COMPONENT"
func deriveStreamName(subject string) string {
	// Handle wildcard subjects
	subject = strings.TrimPrefix(subject, "*.")
	subject = strings.TrimSuffix(subject, ".>")
	subject = strings.TrimSuffix(subject, ".*")

	parts := strings.Split(subject, ".")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "*" || parts[0] == ">" {
		return ""
	}
	return strings.ToUpper(parts[0])
}

// Message handling functions (handleMessage, handleSemanticMessage, evaluateRulesForMessage,
// matchesRuleSubject, recordError) are in message_handler.go

// Stop stops the processor and cleans up resources
func (rp *Processor) Stop(_ time.Duration) error {
	rp.mu.Lock()
	if !rp.running {
		rp.mu.Unlock()
		return nil // Already stopped
	}
	close(rp.shutdown)
	rp.mu.Unlock()

	// Wait for graceful shutdown with timeout
	select {
	case <-rp.done:
		// Clean shutdown
	case <-time.After(5 * time.Second):
		rp.logger.Warn("Rule processor shutdown timeout after 5 seconds")
	}

	// Mark as stopping and extract the hot-reload manager under one lock
	// acquisition. Setting rp.running=false here (before the final cleanup
	// lock) ensures startHotReloadManager sees the stopped state and cleans
	// up its own manager if it races. The hot-reload manager is then stopped
	// outside the lock to avoid a deadlock with reconcileFromKV, which also
	// tries to acquire rp.mu.
	rp.mu.Lock()
	rp.running = false // Set early so startHotReloadManager's race check works.
	hotReloadMgr := rp.kvConfigManager
	rp.kvConfigManager = nil
	rp.mu.Unlock()

	if hotReloadMgr != nil {
		if err := hotReloadMgr.Stop(); err != nil {
			rp.logger.Debug("Rule hot-reload manager stop error (ignored)", slog.Any("error", err))
		}
	}

	// Retire every watcher generation before stopping transports. The dedicated
	// dispatch gate prevents callbacks that already decoded an entry from
	// evaluating after shutdown retirement, while NATS Stop runs without the
	// processor config mutex held.
	rp.entityWatcherUpdateMu.Lock()
	rp.entityDispatchGate.Lock()
	rp.mu.Lock()
	watcherCancel := rp.watcherCancelFunc
	watchers := append([]jetstream.KeyWatcher(nil), rp.entityWatchers...)
	rp.entityWatchers = nil
	rp.entityWatcherMap = nil
	rp.entityWatcherCancels = nil
	rp.entityDispatchRecords = nil
	rp.watcherCtx = nil
	rp.watcherCancelFunc = nil
	rp.mu.Unlock()
	rp.entityDispatchGate.Unlock()
	rp.entityWatcherUpdateMu.Unlock()

	if watcherCancel != nil {
		watcherCancel()
	}
	for _, watcher := range watchers {
		if err := watcher.Stop(); err != nil {
			rp.logger.Error("Error stopping entity watcher", "error", err)
		}
	}
	// Close outside rp.mu: an in-flight debounce callback snapshots rules under
	// rp.mu.RLock, and Close waits for that callback to finish.
	if err := rp.closeEntityEvaluationQueue(); err != nil {
		rp.logger.Warn("Failed to close entity evaluation queue cleanly", "error", err)
	}

	// Clean up resources
	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Unsubscribe from all NATS subjects
	for _, sub := range rp.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			rp.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	rp.subscriptions = nil

	// Clean up all rules
	rp.rules = nil

	// Legacy JetStream consumer cleanup (if still exists)
	if rp.entityConsumer != nil {
		rp.logger.Debug("Legacy JetStream consumer stopped")
	}

	// Note: NATS client handles unsubscription during context cancellation
	rp.isSubscribed = false

	// Close message cache
	if rp.messageCache != nil {
		if err := rp.messageCache.Close(); err != nil {
			rp.logger.Warn("Failed to close message cache", "error", err)
		}
	}

	// rp.running was already set to false before stopping the hot-reload manager
	// to ensure clean races; set health to false here under the cleanup lock.
	rp.health.Healthy = false

	rp.logger.Info("Rule processor stopped")
	return nil
}

// publishGraphEvents and publishRuleEvent are in publisher.go

// GetRuleMetrics returns metrics for all rules
func (rp *Processor) GetRuleMetrics() map[string]any {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	metrics := make(map[string]any)

	for name, ruleInstance := range rp.rules {
		metrics[name] = map[string]any{
			"subjects": ruleInstance.Subscribe(),
		}
	}

	metrics["total_evaluated"] = atomic.LoadInt64(&rp.messagesEvaluated)
	metrics["total_triggered"] = atomic.LoadInt64(&rp.rulesTriggered)
	metrics["events_published"] = atomic.LoadInt64(&rp.eventsPublished)
	metrics["error_count"] = atomic.LoadInt64(&rp.errorCount)

	return metrics
}

// Register, CreateRuleProcessor, and convertDefinitionToPort are in factory.go

// Validation functions (ValidateConfigUpdate, validateSingleRuleConfig, validateExpressionRule,
// isKnownRuleType, isValidOperator, createRuleFromConfig, and helper functions) are in config_validation.go

// Runtime configuration functions (ApplyConfigUpdate, applyRuleChanges, GetRuntimeConfig,
// extractConditions, RuntimeConfigWrapper, and related methods) are in runtime_config.go

// Variable substitution functions are in variables.go

// DebugStatus returns extended debug information for the rule processor.
// Implements component.DebugStatusProvider.
func (rp *Processor) DebugStatus() any {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	pendingCount := 0
	if rp.entityCoalescer != nil {
		pendingCount = rp.entityCoalescer.PendingCount()
	}

	// Get total evaluations and triggers from atomic counters
	totalEvaluations := atomic.LoadInt64(&rp.messagesEvaluated)
	totalTriggers := atomic.LoadInt64(&rp.rulesTriggered)

	// Coalesced count - for now return 0, could track in future if needed
	coalescedCount := 0

	debounceDelayMs := 0
	if rp.config != nil && rp.config.DebounceDelayMs > 0 {
		debounceDelayMs = int(rp.config.DebounceDelayMs.Milliseconds())
	}

	return Status{
		DebounceDelayMs:    debounceDelayMs,
		PendingEvaluations: pendingCount,
		TotalEvaluations:   int(totalEvaluations),
		TotalTriggers:      int(totalTriggers),
		DebouncedCount:     coalescedCount,
		RulesLoaded:        len(rp.rules),
		LastEvaluationTime: rp.lastEvaluationTime,
		TrackedRevisions:   rp.trackedRevisionCount(),
	}
}

// ownedReplacePredicates returns the union of every predicate this rule pack
// declares in a ModeReplaceOwned group across all of its ProjectionContracts
// (ADR-056 Decision 3). This is the envelope a replace_owned action must stay
// inside: a replace_owned naming a predicate NOT in this set is reaching outside
// the pack's owned-current-state claim and is rejected at load/hot-reload time.
//
// Validation runs against the processor's OWN contracts — there is no registry
// injection. The contracts are pack-level and static, so the set is stable for
// the processor lifetime; computing it per-validation-call is cheap (contracts
// are a handful of predicates) and avoids any cache-invalidation question.
func (rp *Processor) ownedReplacePredicates() map[string]struct{} {
	owned := make(map[string]struct{})
	for _, c := range rp.config.ProjectionContracts {
		for _, g := range c.Groups {
			if g.Mode != ownership.ModeReplaceOwned {
				continue
			}
			for _, p := range g.Predicates {
				owned[p] = struct{}{}
			}
		}
	}
	return owned
}

// validateReplaceOwnedAction enforces the ADR-056 Decision 3 envelope on a
// single action. It is a no-op for any non-replace_owned action. For a
// replace_owned action it requires:
//
//   - a non-empty Predicate;
//   - a LITERAL Predicate (no `$` substitution tokens — the predicate names the
//     owned cell and must be statically checkable against the contract);
//   - the Predicate to fall inside one of the pack's ModeReplaceOwned groups.
//
// A violation returns an error; both load paths treat that as a HARD-FAIL
// (file-load aborts boot; hot-reload rejects the change). The owned-predicate
// set is the processor's OWN ProjectionContracts — no registry injection.
func (rp *Processor) validateReplaceOwnedAction(ruleID string, a Action) error {
	if a.Type != ActionTypeReplaceOwned {
		return nil
	}
	if a.Predicate == "" {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s replace_owned action requires a non-empty predicate", ruleID),
			"RuleProcessor", "validateReplaceOwnedAction", "check predicate present")
	}
	if strings.Contains(a.Predicate, "$") {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s replace_owned predicate %q must be a literal (no `$` substitution); the predicate names the owned cell and must be statically checkable against the projection contract (ADR-056 Decision 3)", ruleID, a.Predicate),
			"RuleProcessor", "validateReplaceOwnedAction", "check predicate literal")
	}
	owned := rp.ownedReplacePredicates()
	if _, ok := owned[a.Predicate]; !ok {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s replace_owned predicate %q is outside this pack's owned-replace projection contracts; declare it in a replace-owned group of a projection_contract (owner rule-pack.%s) or use add_triple/update_triple (ADR-056 Decision 3)", ruleID, a.Predicate, rp.config.PackID),
			"RuleProcessor", "validateReplaceOwnedAction", "check predicate in envelope")
	}
	return nil
}

// validateRuleReplaceOwnedActions walks every action list on a Definition
// (OnEnter, OnExit, WhileTrue, OnRecovery, Actions) and runs the ADR-056
// Decision 3 envelope check on each replace_owned action. Called from the
// file-load path (loadRules) at PROCESSOR level — not in the stateless factory
// — because the envelope is the PROCESSOR's ProjectionContracts, which the
// factory does not see. A violation HARD-FAILS the load (returns the error,
// aborting boot) rather than skipping the rule, so a broken owned-write claim
// can never silently ship.
func (rp *Processor) validateRuleReplaceOwnedActions(def Definition) error {
	for label, actions := range map[string][]Action{
		"on_enter":    def.OnEnter,
		"on_exit":     def.OnExit,
		"while_true":  def.WhileTrue,
		"on_recovery": def.OnRecovery,
		"actions":     def.Actions,
	} {
		for i, a := range actions {
			if err := rp.validateReplaceOwnedAction(def.ID, a); err != nil {
				return errs.Wrap(err, "RuleProcessor", "validateRuleReplaceOwnedActions",
					fmt.Sprintf("rule %s %s[%d]", def.ID, label, i))
			}
		}
	}
	return nil
}

// Per-rule revision tracking for feedback loop prevention.
//
// When a rule action writes to an entity, we record the resulting KV revision
// against the rule that caused the write. When the KV watcher later delivers
// that revision, only the originating rule skips evaluation — sibling rules
// watching the same bucket still evaluate, enabling cross-rule orchestration.
//
// Each tracked revision carries its insertion timestamp. The sweeper
// goroutine (started from Start) prunes entries older than revisionTTL so
// the map stays bounded even for writes to unwatched entities, writes
// during watcher downtime, or cross-bucket writes whose revisions never
// reach the watcher.

const (
	// defaultRevisionTTL bounds how long an un-delivered tracked revision
	// stays in the map before the sweeper discards it. Long enough to cover
	// any reasonable watcher delivery latency or bootstrap replay, short
	// enough to bound memory on busy processors.
	defaultRevisionTTL = 5 * time.Minute

	// revisionSweepInterval is how often the background sweeper runs.
	revisionSweepInterval = time.Minute
)

// ruleRevKey is the composite key used to scope tracked revisions to a
// specific (ruleID, entityID) pair. Using a struct key avoids any risk of
// collision from separator characters appearing inside rule or entity IDs.
type ruleRevKey struct {
	ruleID   string
	entityID string
}

// trackRuleRevision records a KV revision generated by the given rule for the
// given entity. Each revision is stored with its insertion timestamp so the
// sweeper can prune unmatched entries. Multiple writes in quick succession
// are each tracked independently — only the one the watcher delivers is
// consumed.
func (rp *Processor) trackRuleRevision(ruleID, entityID string, revision uint64) {
	if ruleID == "" || entityID == "" || revision == 0 {
		return
	}
	rp.revisionMu.Lock()
	defer rp.revisionMu.Unlock()
	key := ruleRevKey{ruleID: ruleID, entityID: entityID}
	set, ok := rp.ownRevisions[key]
	if !ok {
		set = make(map[uint64]time.Time)
		rp.ownRevisions[key] = set
	}
	set[revision] = time.Now()
}

// shouldSkipRule reports whether the given rule/entity/revision tuple was
// generated by that rule. If so, the revision is removed from the tracking
// set (one-time skip) and true is returned.
func (rp *Processor) shouldSkipRule(ruleID, entityID string, revision uint64) bool {
	if ruleID == "" || entityID == "" || revision == 0 {
		return false
	}
	rp.revisionMu.Lock()
	defer rp.revisionMu.Unlock()
	key := ruleRevKey{ruleID: ruleID, entityID: entityID}
	set, ok := rp.ownRevisions[key]
	if !ok {
		return false
	}
	if _, found := set[revision]; !found {
		return false
	}
	delete(set, revision)
	if len(set) == 0 {
		delete(rp.ownRevisions, key)
	}
	return true
}

// pruneStaleRevisions removes tracked revisions older than maxAge, cleans
// empty (rule,entity) entries, and returns the number of revisions pruned.
// Called periodically by the sweeper goroutine and available to tests.
func (rp *Processor) pruneStaleRevisions(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	rp.revisionMu.Lock()
	defer rp.revisionMu.Unlock()
	pruned := 0
	for key, set := range rp.ownRevisions {
		for rev, trackedAt := range set {
			if trackedAt.Before(cutoff) {
				delete(set, rev)
				pruned++
			}
		}
		if len(set) == 0 {
			delete(rp.ownRevisions, key)
		}
	}
	return pruned
}

// trackedRevisionCount returns the total number of tracked revisions across
// all (rule,entity) pairs. Exposed for observability (Status) and tests.
func (rp *Processor) trackedRevisionCount() int {
	rp.revisionMu.Lock()
	defer rp.revisionMu.Unlock()
	total := 0
	for _, set := range rp.ownRevisions {
		total += len(set)
	}
	return total
}

// runRevisionSweeper prunes stale revision entries on a ticker until the
// supplied shutdown channel closes. The channel is captured at goroutine
// creation time so Start() can safely reassign rp.shutdown on restart
// without racing against this loop.
func (rp *Processor) runRevisionSweeper(shutdown <-chan struct{}, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-shutdown:
			return
		case <-ticker.C:
			if pruned := rp.pruneStaleRevisions(maxAge); pruned > 0 {
				rp.logger.Debug("Pruned stale tracked revisions",
					"pruned", pruned,
					"remaining", rp.trackedRevisionCount())
			}
		}
	}
}
