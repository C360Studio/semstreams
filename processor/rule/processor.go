// Package rule provides a rule processing component that implements
// the Discoverable interface for processing message streams through rules
package rule

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Static interface checks - compile-time verification
var _ component.Discoverable = (*Processor)(nil)

// schema defines the configuration schema for rule processor component
// Generated from Config struct tags using reflection
var schema = func() component.ConfigSchema {
	s := component.GenerateConfigSchema(reflect.TypeOf(Config{}))

	// Add "rules" property for runtime-only dynamic rule definitions
	// This field is not in Config struct since it's only used for runtime updates
	s.Properties["rules"] = component.PropertySchema{
		Type:        "object",
		Description: "Dynamic rule definitions (rules.{rule_id} pattern)",
		Default:     map[string]interface{}{},
		Category:    "advanced",
	}

	return s
}()

// RuleMetrics and newRuleMetrics are in metrics.go
// Config and DefaultConfig are in config.go

// Processor is a component that processes messages through rules
type Processor struct {
	// Component interface implementation
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
	health      component.HealthStatus
	flowMetrics component.FlowMetrics

	// Rule processing resources
	natsClient *natsclient.Client
	rules      map[string]Rule // Self-loaded rules; written by applyRuleChanges under mu.Lock, read under mu.RLock.
	// ruleDefinitions holds the parsed Definition for each rule. Writers:
	// loadRules (called only from Initialize, before any goroutines start).
	// applyRuleChanges (hot-reload) does NOT write ruleDefinitions — it only
	// writes rp.rules and rp.ruleConfigs. Reads in message_handler.go are
	// snapshotted under mu.RLock alongside the rp.rules snapshot so that any
	// future hot-reload path that does update definitions is safe by default.
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

	// Active subscriptions flag
	isSubscribed bool

	// NATS subscriptions for cleanup
	subscriptions []*natsclient.Subscription

	// JetStream consumer for entity events
	entityConsumer jetstream.Consumer

	// KV watchers for entity state changes
	// Maps pattern string to watcher for dynamic management
	entityWatchers    []jetstream.KeyWatcher
	entityWatcherMap  map[string]jetstream.KeyWatcher
	watcherCtx        context.Context    // Context for watcher goroutines
	watcherCancelFunc context.CancelFunc // Cancel function for stopping all watchers

	// Entity coalescer for batched rule evaluation
	entityCoalescer *cache.CoalescingSet

	// Prometheus metrics
	metrics *Metrics

	// Stateful rule support. Both fields are set once in Initialize (before any
	// goroutines start) and never reassigned at runtime, so they may be read
	// without holding mu.
	stateTracker      *StateTracker
	statefulEvaluator *StatefulEvaluator

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
}

// NewProcessor creates a new rule processor
func NewProcessor(natsClient *natsclient.Client, config *Config) (*Processor, error) {
	return NewProcessorWithMetrics(natsClient, config, nil)
}

// NewProcessorWithMetrics creates a new rule processor with optional metrics
func NewProcessorWithMetrics(natsClient *natsclient.Client, config *Config, metricsRegistry *metric.MetricsRegistry) (*Processor, error) {
	if config == nil {
		defaultConfig := DefaultConfig()
		config = &defaultConfig
	}

	// Validate required configuration
	if config.Ports == nil {
		return nil, fmt.Errorf("rule processor config missing required Ports configuration")
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
		natsClient:       natsClient,
		rules:            make(map[string]Rule),
		ruleDefinitions:  make(map[string]Definition),
		ruleConfigs:      make(map[string]map[string]any),
		matchCounters:    make(map[string]*atomic.Int64),
		messageCache:     msgCache,
		config:           config,
		metricsRegistry:  metricsRegistry,
		entityWatchers:   make([]jetstream.KeyWatcher, 0),
		entityWatcherMap: make(map[string]jetstream.KeyWatcher),
		ownRevisions:     make(map[ruleRevKey]map[uint64]time.Time),
		revisionTTL:      defaultRevisionTTL,
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

	// Set up input and output ports
	rp.setupPorts()

	// Note: entityCoalescer will be initialized in Start() when we have a context

	return rp, nil
}

// setupPorts initializes input and output port definitions.
// Ports configuration is validated in the constructor, so config.Ports is guaranteed non-nil.
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

	// Create StatefulEvaluator
	rp.statefulEvaluator = NewStatefulEvaluator(rp.stateTracker, actionExecutor, rp.logger)

	rp.logger.Info("State tracker initialized for stateful ECA rules")
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

	// Cancel watcher context to signal all watcher goroutines to stop
	if rp.watcherCancelFunc != nil {
		rp.watcherCancelFunc()
	}

	// Stop all entity watchers
	for _, watcher := range rp.entityWatchers {
		if err := watcher.Stop(); err != nil {
			rp.logger.Error("Error stopping entity watcher", "error", err)
		}
	}
	rp.entityWatchers = nil
	rp.entityWatcherMap = nil

	// Close entity coalescer
	if rp.entityCoalescer != nil {
		if err := rp.entityCoalescer.Close(); err != nil {
			rp.logger.Warn("Failed to close entity coalescer", "error", err)
		}
	}

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
