// Package rule provides a rule processing component that implements
// the Discoverable interface for processing message streams through rules
package rule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	message "github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/types"
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
	platform            types.PlatformMeta // deployment authority for every minted identity (ADR-102)
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
	running            bool // Tracks if processor is running (protected by mu)
	lifecycleMu        sync.Mutex
	lifecycleUsed      bool
	terminal           bool
	stopping           bool
	cleanupPending     bool
	startDone          chan struct{}
	cancel             context.CancelFunc
	runtimeDone        chan struct{}
	runtimeWG          *sync.WaitGroup
	commandMu          sync.Mutex
	commandFenced      bool
	commands           []ruleRuntimeCommand
	commandWake        chan struct{}
	coordinatorDone    chan struct{}
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
	// a consumed ENTITY_STATES value violates the authoritative graph-state
	// contract, rule evaluation must stop rather than derive output from a
	// partial view. Contract validation rides the entity-watch input path —
	// the canonical decode of every value rules actually consume — not a
	// dedicated ENTITY_STATES guard watcher; with zero configured entity-watch
	// patterns the processor holds no ENTITY_STATES watcher at all.
	graphStateResetRequired atomic.Bool
	// graphStateGuardDegraded latches when a configured entity-watch lane
	// cannot be trusted (a pattern watcher failed to start or closed
	// unexpectedly). Sticky like reset-required, with a distinct error code.
	graphStateGuardDegraded atomic.Bool

	// Readiness producer state (ADR-083 envelope on the rule GRAPH_STATUS key).
	// See readiness.go: bootstrap completion is tracked PER WATCHER GENERATION,
	// not per process, because the watcher set is runtime-mutable.
	statusPublisher *readiness.Publisher
	readinessGauges *readiness.Gauges
	// statusLoopDone closes when the readiness tick has exited, so Stop can join it
	// before tearing down the state the tick reads.
	statusLoopDone chan struct{}
	// statusFenced is raised BEFORE teardown and makes any in-flight or late tick
	// return without publishing. The join is best-effort; this is the guarantee.
	statusFenced atomic.Bool
	// statusInterval is a TEST SEAM only, so an integration test can observe
	// successive heartbeats without sleeping through production cadence.
	statusInterval time.Duration
	// graphStateGuardDone closes when either sticky latch above fires so
	// entity-watch loops unwind promptly instead of draining dead updates.
	graphStateGuardDone     chan struct{}
	graphStateGuardDoneOnce sync.Once

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
	entityDispatchGate    sync.RWMutex
	entityDispatchRecords map[string]managedEntityWatcher
	entityNextGeneration  uint64
	entityBorrowMu        sync.Mutex
	entityBorrowFenced    bool
	entityBorrowCount     int
	entityBorrowDone      chan struct{}
	entityWatcherPrepare  entityWatcherFactory
	entityBeforeDispatch  func()
	entityBeforeEvalLock  func(string)

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

	// kvConfigManager is the component-internal hot-reload manager. It owns a
	// KV watcher on semstreams_config:rules.* and calls ApplyConfigUpdate when
	// the watcher fires. Constructed in Start; nil when NATS is unavailable.
	// See also: cmd/semstreams/main.go buildRuleManager — a second ConfigManager
	// instance (processor=nil) for agent CRUD tools. Both share the same KV bucket.
	kvConfigManager *ConfigManager
	streamConsumers []ruleStreamConsumer

	projectionTargets    *projectionTargetIndex
	effectiveContracts   []projection.Contract
	initialRules         []Definition
	initialRulesReady    bool
	reconciler           projection.PredicateReconciler
	reconcilerConfigured bool
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
	configCopy := *config
	configCopy.RulesFiles = append([]string(nil), config.RulesFiles...)
	configCopy.ProjectionContracts = cloneProjectionContracts(config.ProjectionContracts)
	inlineRules, err := cloneRuleDefinitions(config.InlineRules)
	if err != nil {
		return nil, err
	}
	configCopy.InlineRules = inlineRules
	targets, err := buildProjectionTargetIndex(nil)
	if err != nil {
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
		natsClient:            natsClient,
		rules:                 make(map[string]Rule),
		ruleDefinitions:       make(map[string]Definition),
		ruleConfigs:           make(map[string]map[string]any),
		matchCounters:         make(map[string]*atomic.Int64),
		cronRules:             make(map[string]*CronRule),
		messageCache:          msgCache,
		config:                &configCopy,
		metricsRegistry:       metricsRegistry,
		entityWatchers:        make([]jetstream.KeyWatcher, 0),
		entityWatcherMap:      make(map[string]jetstream.KeyWatcher),
		entityDispatchRecords: make(map[string]managedEntityWatcher),
		graphStateGuardDone:   make(chan struct{}),
		ownRevisions:          make(map[ruleRevKey]map[uint64]time.Time),
		revisionTTL:           defaultRevisionTTL,
		projectionTargets:     targets,
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

	// Readiness gauges: the scrapeable half of the ADR-066 envelope. Built at
	// construction so the series exist before the first status tick.
	rp.readinessGauges = initReadinessGauges(metricsRegistry)

	// Set up input and output ports
	if err := rp.setupPorts(); err != nil {
		return nil, err
	}

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

// SetPlatform installs the deployment's own authority (deps.Platform at the
// composition root). Every entity the rule engine mints — trigger identities
// today, run-scope mints under #1096 — carries it in positions 1-2; nothing is
// read back from a firing entity (ADR-102 d2). Called by the component factory
// after construction, before rules load.
func (rp *Processor) SetPlatform(platform types.PlatformMeta) {
	rp.platform = platform
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
func (rp *Processor) setupPorts() error {
	inputs, outputs, err := resolvePorts(*rp.config.Ports)
	if err != nil {
		return err
	}
	rp.inputPorts, rp.outputPorts = inputs, outputs
	return nil
}

// resolvePorts resolves the configured port declarations. It is the one port
// derivation DeclarePorts and setupPorts share.
func resolvePorts(ports component.PortConfig) ([]component.Port, []component.Port, error) {
	inputs := make([]component.Port, len(ports.Inputs))
	for i, portDef := range ports.Inputs {
		port, err := portDef.Resolve(component.DirectionInput)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve input port %q: %w", portDef.Name, err)
		}
		inputs[i] = port
	}
	outputs := make([]component.Port, len(ports.Outputs))
	for i, portDef := range ports.Outputs {
		port, err := portDef.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve output port %q: %w", portDef.Name, err)
		}
		outputs[i] = port
	}
	return inputs, outputs, nil
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
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	if !rp.initialRulesReady {
		return rp.config.PackID, nil
	}
	return rp.config.PackID, cloneProjectionContracts(rp.effectiveContracts)
}

// PreflightProjectionMutations validates and freezes the initial rules and
// projection target index without performing mutation, ownership, heartbeat,
// or NATS side effects.
func (rp *Processor) PreflightProjectionMutations() error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.prepareInitialRules()
}

// SetPredicateReconciler injects the pack's reconcile capability. It is a
// one-time composition operation and must follow successful preflight.
func (rp *Processor) SetPredicateReconciler(reconciler projection.PredicateReconciler) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if !rp.initialRulesReady {
		return fmt.Errorf("rule pack %q mutation preflight has not completed", rp.config.PackID)
	}
	if len(rp.effectiveContracts) == 0 {
		return fmt.Errorf("rule pack %q has no projection contracts", rp.config.PackID)
	}
	if reconciler == nil {
		return fmt.Errorf("rule pack %q predicate reconciler is nil", rp.config.PackID)
	}
	if rp.reconcilerConfigured {
		return fmt.Errorf("rule pack %q predicate reconciler is already configured", rp.config.PackID)
	}
	rp.reconciler = reconciler
	rp.reconcilerConfigured = true
	return nil
}

// Health returns current health status. Pure getter: the derived fields
// (LastCheck, ErrorCount, Uptime) are computed into a local copy — mutating
// the shared rp.health cache here raced under the read lock, which admits
// concurrent holders (gh#566: the ComponentManager health-publish loop and
// any health query call this concurrently).
func (rp *Processor) Health() component.HealthStatus {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	health := rp.health
	health.LastCheck = time.Now()
	health.ErrorCount = int(atomic.LoadInt64(&rp.errorCount))
	if !rp.startTime.IsZero() {
		health.Uptime = time.Since(rp.startTime)
	}

	// The two sticky entity-watch latches are health facts, and this surface must
	// not contradict the readiness envelope that already reports them
	// (readiness.go computeReadinessStatus). Before this, run() set Healthy = true
	// once watchers were established and NOTHING lowered it again: a processor whose
	// entity-watch lane had latched degraded — or which had stopped evaluating rules
	// entirely under reset-required — still answered Healthy from here, while its
	// KV envelope said degraded. An operator reconciling the two had no way to tell
	// which was right.
	switch {
	case rp.graphStateResetRequired.Load():
		health.Healthy = false
		health.LastError = "entity-state contract violation: rule evaluation halted (reset required)"
	case rp.graphStateGuardDegraded.Load():
		health.Healthy = false
		health.LastError = "entity-watch lane degraded"
	}

	return health
}

// DataFlow returns current data flow metrics. Pure getter for the same
// gh#566 reason as Health: derived rates land in a local copy, never the
// shared rp.flowMetrics cache.
func (rp *Processor) DataFlow() component.FlowMetrics {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	metrics := rp.flowMetrics

	// Calculate messages per second based on recent activity
	evaluated := atomic.LoadInt64(&rp.messagesEvaluated)
	if !rp.startTime.IsZero() && evaluated > 0 {
		duration := time.Since(rp.startTime).Seconds()
		if duration > 0 {
			metrics.MessagesPerSecond = float64(evaluated) / duration
		}
	}

	// Error rate calculation
	if evaluated > 0 {
		metrics.ErrorRate = float64(atomic.LoadInt64(&rp.errorCount)) / float64(evaluated)
	}

	metrics.LastActivity = rp.lastActivity

	return metrics
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

type ruleRuntimeCommand struct {
	run    func(context.Context) error
	result chan error
}

type ruleStreamConsumer struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

func (rp *Processor) runRuntimeCoordinator(ctx context.Context) {
	defer close(rp.coordinatorDone)
	for {
		select {
		case <-ctx.Done():
			rp.failQueuedRuntimeCommands(ctx.Err())
			return
		case <-rp.commandWake:
			for {
				rp.commandMu.Lock()
				if len(rp.commands) == 0 {
					rp.commandMu.Unlock()
					break
				}
				command := rp.commands[0]
				rp.commands = rp.commands[1:]
				rp.commandMu.Unlock()
				command.result <- command.run(ctx)
				close(command.result)
			}
		}
	}
}

func (rp *Processor) failQueuedRuntimeCommands(err error) {
	rp.commandMu.Lock()
	commands := rp.commands
	rp.commands = nil
	rp.commandMu.Unlock()
	for _, command := range commands {
		command.result <- err
		close(command.result)
	}
}

func (rp *Processor) submitRuntimeCommand(run func(context.Context) error) error {
	command := ruleRuntimeCommand{run: run, result: make(chan error, 1)}
	rp.commandMu.Lock()
	if rp.commandFenced || rp.commandWake == nil {
		rp.commandMu.Unlock()
		return errs.WrapInvalid(errors.New("runtime command admission is closed"), "RuleProcessor", "runtimeCommand", "processor is not accepting runtime updates")
	}
	if rp.coordinatorDone != nil {
		select {
		case <-rp.coordinatorDone:
			rp.commandMu.Unlock()
			return errs.WrapInvalid(errors.New("runtime coordinator stopped"), "RuleProcessor", "runtimeCommand", "processor runtime has ended")
		default:
		}
	}
	rp.commands = append(rp.commands, command)
	wake := rp.commandWake
	rp.commandMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return <-command.result
}

func (rp *Processor) fenceRuntimeCommands() <-chan error {
	barrier := ruleRuntimeCommand{run: func(context.Context) error { return nil }, result: make(chan error, 1)}
	rp.commandMu.Lock()
	rp.commandFenced = true
	if rp.coordinatorDone != nil {
		select {
		case <-rp.coordinatorDone:
			rp.commandMu.Unlock()
			barrier.result <- nil
			close(barrier.result)
			return barrier.result
		default:
		}
	}
	rp.commands = append(rp.commands, barrier)
	wake := rp.commandWake
	rp.commandMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return barrier.result
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
			actionExecutor = NewActionExecutorComplete(rp.logger, mutator, publisher, kvWriter, rp.platform)
			rp.logger.Info("ActionExecutor initialized with triple mutation, publishing, and KV write support")
		} else {
			actionExecutor = NewActionExecutorComplete(rp.logger, nil, publisher, kvWriter, rp.platform)
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

	// Wire the immutable replacement envelope and the one public mutation
	// capability before any evaluator or scheduler can dispatch an action.
	if setter, ok := actionExecutor.(interface {
		SetPredicateReconciler(projection.PredicateReconciler)
	}); ok {
		setter.SetPredicateReconciler(rp.reconciler)
	}
	if executor, ok := actionExecutor.(*ActionExecutor); ok {
		executor.setProjectionTargets(rp.projectionTargets, rp)
		// The collectors that count what the executor deliberately did not write.
		// The deployment authority is NOT set here: it is a constructor
		// parameter of NewActionExecutorComplete above, so it cannot be
		// forgotten the way a setter can (#1096).
		executor.setMetrics(rp.metrics)
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
	// Wire the rule-processor Metrics so runActions can count non-deny
	// action-execution failures (actionFailuresTotal). rp.metrics is
	// nil-safe (newRuleMetrics returns nil when metricsRegistry is nil)
	// and SetMetrics tolerates a nil argument.
	rp.statefulEvaluator.SetMetrics(rp.metrics)

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
func (rp *Processor) Start(ctx context.Context) (startErr error) {
	runCtx, startDone, err := rp.beginStartAuthority(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		startErr = rp.finishStartAttempt(ctx, startDone, committed, startErr)
	}()

	// Publish the coordinator before any fallible startup work so failed-Start
	// rollback can always fence and join the same exact authority record.
	rp.runtimeWG.Add(1)
	go func() {
		defer rp.runtimeWG.Done()
		rp.runRuntimeCoordinator(runCtx)
	}()
	go func(wg *sync.WaitGroup, done chan struct{}) {
		wg.Wait()
		close(done)
	}(rp.runtimeWG, rp.runtimeDone)

	rp.mu.Lock()
	if err := rp.prepareInitialRules(); err != nil {
		rp.mu.Unlock()
		return errs.WrapInvalid(err, "RuleProcessor", "Start", "preflight mutation projection")
	}
	if len(rp.effectiveContracts) > 0 && !rp.reconcilerConfigured {
		rp.mu.Unlock()
		return errs.WrapInvalid(
			fmt.Errorf("rule pack %q has projection contracts but no owned replacer", rp.config.PackID),
			"RuleProcessor",
			"Start",
			"validate mutation composition",
		)
	}
	rp.mu.Unlock()

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

	if rp.config.DebounceDelayMs > 0 {
		rp.entityCoalescer = cache.NewCoalescingSet(runCtx, rp.config.DebounceDelayMs, func(entityIDs []string) {
			rp.evaluateEntitiesInBatch(runCtx, entityIDs)
		})
	}
	if err := rp.watchEntityStates(runCtx); err != nil {
		rp.logger.Warn("Failed to start entity state watching", "error", err)
	}
	if err := rp.setupSubscriptions(runCtx); err != nil {
		return err
	}
	if rp.cronScheduler != nil {
		if err := rp.cronScheduler.Start(runCtx); err != nil {
			return fmt.Errorf("start cron scheduler: %w", err)
		}
	}
	if err := rp.createStatusBucket(runCtx); err != nil {
		rp.logger.Warn("rule readiness publisher unavailable; consumers will read this producer as unknown", "error", err)
	} else {
		rp.statusLoopDone = make(chan struct{})
		go rp.statusMetricsLoop(runCtx)
	}
	if rp.natsClient != nil {
		rcm := NewConfigManager(rp, nil, rp.logger)
		if err := rcm.InitializeKVStore(runCtx, rp.natsClient); err != nil {
			rp.logger.Warn("Failed to initialize KV store for rule hot-reload; running with file rules only", slog.Any("error", err))
		} else if err := rcm.Start(runCtx); err != nil {
			rp.logger.Warn("Failed to start rule hot-reload watcher; running with file rules only", slog.Any("error", err))
		} else {
			rp.kvConfigManager = rcm
		}
	}
	rp.runtimeWG.Add(1)
	go func() {
		defer rp.runtimeWG.Done()
		rp.runRevisionSweeper(runCtx, revisionSweepInterval, rp.revisionTTL)
	}()

	rp.mu.Lock()
	rp.running = true
	rp.isSubscribed = true
	rp.statusFenced.Store(false)
	rp.startTime = time.Now()
	rp.health.Healthy = true
	rp.health.LastCheck = time.Now()
	rp.mu.Unlock()

	// Count subjects for logging
	subjectCount := 0
	for _, port := range rp.inputPorts {
		facts, err := port.Facts()
		if err == nil && (facts.Kind() == component.PortKindNATS || facts.Kind() == component.PortKindJetStream) && len(facts.NATSSubjects()) == 1 {
			subjectCount++
		}
	}

	rp.logger.Info("Rule processor started", "subject_count", subjectCount)
	committed = true
	return nil
}

func (rp *Processor) beginStartAuthority(ctx context.Context) (context.Context, chan struct{}, error) {
	if ctx == nil {
		return nil, nil, errs.WrapInvalid(errs.ErrInvalidConfig, "RuleProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, errs.WrapInvalid(err, "RuleProcessor", "Start", "context already cancelled")
	}

	rp.lifecycleMu.Lock()
	if rp.lifecycleUsed {
		rp.lifecycleMu.Unlock()
		return nil, nil, errs.WrapInvalid(errs.ErrAlreadyStarted, "RuleProcessor", "Start", "cleanup authority already active")
	}
	rp.lifecycleUsed = true
	rp.cleanupPending = true
	rp.startDone = make(chan struct{})
	startDone := rp.startDone
	runCtx, cancel := context.WithCancel(ctx)
	rp.cancel = cancel
	rp.runtimeWG = &sync.WaitGroup{}
	rp.runtimeDone = make(chan struct{})
	rp.commandWake = make(chan struct{}, 1)
	rp.coordinatorDone = make(chan struct{})
	rp.commandFenced = false
	rp.entityBorrowMu.Lock()
	rp.entityBorrowFenced = false
	rp.entityBorrowCount = 0
	rp.entityBorrowDone = nil
	rp.entityBorrowMu.Unlock()
	rp.lifecycleMu.Unlock()
	return runCtx, startDone, nil
}

func (rp *Processor) finishStartAttempt(ctx context.Context, startDone chan struct{}, committed bool, startErr error) error {
	if !committed {
		rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, rp.cleanup)
		startErr = errors.Join(startErr, rollbackErr)
		rp.lifecycleMu.Lock()
		if rollbackErr == nil {
			rp.cleanupPending = false
			rp.terminal = true
			rp.clearLifecycleHandles()
		}
		close(startDone)
		rp.startDone = nil
		rp.lifecycleMu.Unlock()
		return startErr
	}
	rp.lifecycleMu.Lock()
	rp.cleanupPending = false
	close(startDone)
	rp.startDone = nil
	rp.lifecycleMu.Unlock()
	return startErr
}

// setupSubscriptions creates subscriptions for input subjects based on port type
func (rp *Processor) setupSubscriptions(ctx context.Context) error {
	if !rp.natsClient.IsHealthy() {
		return errs.WrapFatal(errs.ErrNoConnection, "RuleProcessor", "Start", "check NATS health")
	}

	for _, port := range rp.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return err
		}

		switch facts.Kind() {
		case component.PortKindKVWatch:
			// Entity state watches are owned by the dedicated watcher lifecycle.
			rp.logger.Debug("Skipping input subscription - using dedicated KV watcher", "port", port.Name)
			continue

		case component.PortKindJetStream:
			subjects := facts.NATSSubjects()
			if len(subjects) != 1 {
				return fmt.Errorf("input port %q must declare one JetStream subject", port.Name)
			}
			subject := subjects[0]
			// JetStream subscription - use durable consumer
			if err := rp.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.Wrap(err, "RuleProcessor", "setupSubscriptions",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			subjects := facts.NATSSubjects()
			if len(subjects) != 1 {
				return fmt.Errorf("input port %q must declare one NATS subject", port.Name)
			}
			subject := subjects[0]
			// Core NATS subscription
			sub, err := rp.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, msg *nats.Msg) {
				rp.handleMessage(msgCtx, msg.Subject, msg.Data)
			})
			if err != nil {
				return errs.Wrap(err, "RuleProcessor", "Start", fmt.Sprintf("subscribe to %s", subject))
			}
			rp.subscriptions = append(rp.subscriptions, sub)
			rp.logger.Info("Rule processor subscribed (NATS)", "subject", subject)

		default:
			return fmt.Errorf("unsupported input port %q kind %q", port.Name, facts.Kind())
		}
	}

	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (rp *Processor) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok || len(stream.Subjects()) != 1 {
		return fmt.Errorf("port %q must declare one JetStream subject", port.Name)
	}
	streamName := stream.Name()
	subject := stream.Subjects()[0]

	// Wait for stream to be available
	if err := rp.waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("rule-processor-%s", sanitizedSubject)

	rp.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return fmt.Errorf("resolve JetStream consumer config for port %q: %w", port.Name, consumerErr)
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending,
		AutoCreate:    false,
	}

	handle, err := rp.natsClient.ConsumeStreamWithConfig(ctx, natsclient.PortConsumerContext{Component: rp.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		rp.handleMessage(msgCtx, subject, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			rp.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}
	rp.lifecycleMu.Lock()
	rp.streamConsumers = append(rp.streamConsumers, ruleStreamConsumer{handle: handle})
	rp.lifecycleMu.Unlock()

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

// Message handling functions (handleMessage, handleSemanticMessage, evaluateRulesForMessage,
// matchesRuleSubject, recordError) are in message_handler.go

// Stop stops the processor and cleans up resources
func (rp *Processor) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	for {
		rp.lifecycleMu.Lock()
		if !rp.lifecycleUsed {
			rp.lifecycleUsed, rp.terminal = true, true
			rp.lifecycleMu.Unlock()
			return nil
		}
		if rp.terminal {
			rp.lifecycleMu.Unlock()
			return nil
		}
		if rp.startDone != nil {
			done := rp.startDone
			rp.lifecycleMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if rp.stopping {
			rp.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "RuleProcessor", "Stop", "concurrent Stop")
		}
		retryable := rp.cleanupPending
		rp.stopping = true
		rp.lifecycleMu.Unlock()

		stopErr := rp.cleanup(ctx)
		rp.lifecycleMu.Lock()
		rp.stopping = false
		if retryable && stopErr != nil {
			rp.lifecycleMu.Unlock()
			return stopErr
		}
		rp.cleanupPending, rp.terminal = false, true
		rp.clearLifecycleHandles()
		rp.lifecycleMu.Unlock()
		rp.mu.Lock()
		rp.running = false
		rp.isSubscribed = false
		rp.health.Healthy = false
		rp.mu.Unlock()
		rp.logger.Info("Rule processor stopped")
		return stopErr
	}
}

func (rp *Processor) cleanup(ctx context.Context) error {
	// 1. Fence readiness and contextless runtime mutation admission.
	rp.statusFenced.Store(true)
	barrier := rp.fenceRuntimeCommands()

	rp.lifecycleMu.Lock()
	cancel := rp.cancel
	coordinatorDone := rp.coordinatorDone
	rp.lifecycleMu.Unlock()

	stopErrors := []error{settleRuntimeCommandFence(ctx, barrier, cancel, coordinatorDone)}

	// Every admitted runtime mutation has now settled. Resource snapshots taken
	// below are therefore final for this owner lifetime.
	rp.lifecycleMu.Lock()
	cronScheduler := rp.cronScheduler
	hotReloadMgr := rp.kvConfigManager
	consumers := rp.streamConsumers
	subscriptions := append([]*natsclient.Subscription(nil), rp.subscriptions...)
	runtimeDone := rp.runtimeDone
	statusLoopDone := rp.statusLoopDone
	rp.lifecycleMu.Unlock()

	// 2. Fence cron scheduling and obtain the native in-flight completion.
	var cronDone <-chan struct{}
	if cronScheduler != nil {
		cronDone = cronScheduler.Stop().Done()
	}

	// 3. Fence every message input while its callback authority is still live.
	for i := range consumers {
		if !consumers[i].drainIssued {
			consumers[i].handle.Drain()
			consumers[i].drainIssued = true
		}
	}
	for _, subscription := range subscriptions {
		if subscription != nil {
			stopErrors = append(stopErrors, subscription.Drain(ctx))
		}
	}

	// 4. Deactivate watcher generations before stopping exact native watchers.
	watchers, watcherRecords, entityBorrowDone := rp.fenceAndSnapshotEntityWatchers()
	for _, watcher := range watchers {
		if err := watcher.Stop(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {
			stopErrors = append(stopErrors, fmt.Errorf("stop entity watcher: %w", err))
		}
	}

	// 5. Join every admitted callback, reconcile, and cron fire before
	// canceling the run authority they still need to finish cleanly.
	for i := range consumers {
		select {
		case <-consumers[i].handle.Closed():
		case <-ctx.Done():
			stopErrors = append(stopErrors, ctx.Err())
		}
	}
	for _, record := range watcherRecords {
		if record.done == nil {
			continue
		}
		select {
		case <-record.done:
		case <-ctx.Done():
			stopErrors = append(stopErrors, ctx.Err())
		}
	}
	if err := awaitEntityBorrowSettlement(ctx, entityBorrowDone, cancel); err != nil {
		stopErrors = append(stopErrors, err)
	}
	if hotReloadMgr != nil {
		stopErrors = append(stopErrors, hotReloadMgr.Stop())
	}
	for _, done := range []<-chan struct{}{cronDone} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			stopErrors = append(stopErrors, ctx.Err())
		}
	}
	// 6. Watcher admission is closed; now no new work can enter the coalescer.
	if err := rp.closeEntityEvaluationQueue(); err != nil {
		stopErrors = append(stopErrors, fmt.Errorf("close entity evaluation queue: %w", err))
	}

	// 7-8. Signal all continuing loops and join the exact owner runtime.
	if cancel != nil {
		cancel()
	}
	for _, done := range []<-chan struct{}{statusLoopDone, runtimeDone} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			stopErrors = append(stopErrors, ctx.Err())
		}
	}

	// 9. Terminal resources are closed only after all users have joined.
	rp.mu.RLock()
	messageCache := rp.messageCache
	rp.mu.RUnlock()
	if messageCache != nil {
		if err := messageCache.Close(); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("close message cache: %w", err))
		}
	}
	rp.lifecycleMu.Lock()
	rp.streamConsumers = consumers
	rp.lifecycleMu.Unlock()
	return errors.Join(stopErrors...)
}

func (rp *Processor) fenceAndSnapshotEntityWatchers() (
	[]jetstream.KeyWatcher,
	[]managedEntityWatcher,
	<-chan struct{},
) {
	rp.entityDispatchGate.Lock()
	defer rp.entityDispatchGate.Unlock()
	entityBorrowDone := rp.fenceEntityBorrowsLocked()
	rp.mu.Lock()
	defer rp.mu.Unlock()
	watchers := append([]jetstream.KeyWatcher(nil), rp.entityWatchers...)
	watcherRecords := make([]managedEntityWatcher, 0, len(rp.entityDispatchRecords))
	for key, record := range rp.entityDispatchRecords {
		watcherRecords = append(watcherRecords, record)
		if record.cancel != nil {
			record.cancel()
		}
		delete(rp.entityDispatchRecords, key)
	}
	rp.entityWatchers = nil
	rp.entityWatcherMap = make(map[string]jetstream.KeyWatcher)
	return watchers, watcherRecords, entityBorrowDone
}

func awaitEntityBorrowSettlement(
	ctx context.Context,
	done <-chan struct{},
	cancel context.CancelFunc,
) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// An admitted evaluation owns the Start authority, not the expired Stop
		// context. Cancel it so context-aware fetches and actions can unwind;
		// terminal Stop never waits on an owner mutex.
		if cancel != nil {
			cancel()
		}
		return ctx.Err()
	}
}

func settleRuntimeCommandFence(
	ctx context.Context,
	barrier <-chan error,
	cancel context.CancelFunc,
	coordinatorDone <-chan struct{},
) error {
	select {
	case barrierErr := <-barrier:
		return barrierErr
	case <-ctx.Done():
		// A runtime command already admitted before the fence owns the Start
		// context. If the Stop deadline wins, cancel that authority and join the
		// coordinator before taking any teardown snapshot; otherwise the command
		// could publish a watcher or cron registration after the snapshot.
		if cancel != nil {
			cancel()
		}
		barrierErr := <-barrier
		if coordinatorDone != nil {
			<-coordinatorDone
		}
		if errors.Is(barrierErr, context.Canceled) {
			barrierErr = nil
		}
		return errors.Join(ctx.Err(), barrierErr)
	}
}

func (rp *Processor) clearLifecycleHandles() {
	rp.cancel = nil
	rp.runtimeDone = nil
	rp.runtimeWG = nil
	rp.commandWake = nil
	rp.coordinatorDone = nil
	rp.streamConsumers = nil
	rp.kvConfigManager = nil
	rp.subscriptions = nil
	rp.cronScheduler = nil
	rp.statusLoopDone = nil
	rp.messageCache = nil
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

func (rp *Processor) validateReconcileAction(ruleID string, a Action) error {
	return validateReconcileAction(rp.projectionTargets, ruleID, a)
}

func validateReconcileAction(index *projectionTargetIndex, ruleID string, a Action) error {
	if a.Type != ActionTypeReconcilePredicates {
		return nil
	}
	if _, err := index.resolve(a.ProjectionContract, a.ProjectionGroup, a.Predicate); err != nil {
		return errs.WrapInvalid(
			fmt.Errorf("rule %s reconcile_predicates target: %w", ruleID, err),
			"RuleProcessor",
			"validateReconcileAction",
			"resolve projection target",
		)
	}
	return nil
}

// validateRuleReconcileActions walks every action list on a Definition
// (OnEnter, OnExit, WhileTrue, OnRecovery, Actions) and runs the ADR-056
// target check on each reconcile_predicates action. Called from the
// file-load path (loadRules) at PROCESSOR level — not in the stateless factory
// — because the envelope is the PROCESSOR's ProjectionContracts, which the
// factory does not see. A violation HARD-FAILS the load (returns the error,
// aborting boot) rather than skipping the rule, so a broken owned-write claim
// can never silently ship.
func (rp *Processor) validateRuleReconcileActions(def Definition) error {
	return validateRuleReconcileActions(rp.projectionTargets, def)
}

func validateRuleReconcileActions(index *projectionTargetIndex, def Definition) error {
	for label, actions := range map[string][]Action{
		"on_enter":    def.OnEnter,
		"on_exit":     def.OnExit,
		"while_true":  def.WhileTrue,
		"on_recovery": def.OnRecovery,
		"actions":     def.Actions,
	} {
		for i, a := range actions {
			if err := validateReconcileAction(index, def.ID, a); err != nil {
				return errs.Wrap(err, "RuleProcessor", "validateRuleReconcileActions",
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

// runRevisionSweeper prunes stale revision entries until its runtime ends.
func (rp *Processor) runRevisionSweeper(ctx context.Context, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
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
