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

	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	message "github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	rtypes "github.com/c360/semstreams/types/rule"
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
	natsClient  *natsclient.Client
	rules       map[string]rtypes.Rule   // Self-loaded rules
	ruleConfigs map[string]map[string]any // Original rule configurations for GetRuntimeConfig

	// Message cache
	messageCache cache.Cache[message.Message]

	// Configuration
	config *Config
	
	// Dependencies
	metricsRegistry *metric.MetricsRegistry

	// Runtime state
	shutdown          chan struct{}
	done              chan struct{}
	startTime         time.Time
	messagesEvaluated int64
	rulesTriggered    int64
	eventsPublished   int64 // New metric for event publishing
	errorCount        int64
	lastError         string
	lastActivity      time.Time
	mu                sync.RWMutex

	// Active subscriptions flag
	isSubscribed bool

	// JetStream consumer for entity events
	entityConsumer jetstream.Consumer
	
	// KV watchers for entity state changes
	entityWatchers []jetstream.KeyWatcher
	
	// Prometheus metrics
	metrics *RuleMetrics
	
	// Logger
	logger *slog.Logger
}

// NewProcessor creates a new rule processor
func NewProcessor(natsClient *natsclient.Client, config *Config) *Processor {
	return NewProcessorWithMetrics(natsClient, config, nil)
}

// NewProcessorWithMetrics creates a new rule processor with optional metrics
func NewProcessorWithMetrics(natsClient *natsclient.Client, config *Config, metricsRegistry *metric.MetricsRegistry) *Processor {
	if config == nil {
		defaultConfig := DefaultConfig()
		config = &defaultConfig
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
		natsClient:      natsClient,
		rules:           make(map[string]rtypes.Rule),
		ruleConfigs:     make(map[string]map[string]any),
		messageCache:    msgCache,
		config:          config,
		metricsRegistry: metricsRegistry,
		entityWatchers:  make([]jetstream.KeyWatcher, 0),
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

	return rp
}

// setupPorts initializes input and output port definitions
func (rp *Processor) setupPorts() {
	// Use ports from config if available
	if rp.config.Ports != nil {
		rp.inputPorts = make([]component.Port, len(rp.config.Ports.Inputs))
		for i, portDef := range rp.config.Ports.Inputs {
			rp.inputPorts[i] = convertDefinitionToPort(portDef, component.DirectionInput)
		}

		rp.outputPorts = make([]component.Port, len(rp.config.Ports.Outputs))
		for i, portDef := range rp.config.Ports.Outputs {
			rp.outputPorts[i] = convertDefinitionToPort(portDef, component.DirectionOutput)
		}
		return
	}

	// All configs should have Ports configured - no fallback needed in greenfield
	panic("Rule processor config missing required Ports configuration")
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
		return errors.Wrap(err, "RuleProcessor", "initialize", "load rules")
	}

	rp.logger.Info("Rule processor initialized", "rule_count", len(rp.rules))
	return nil
}

// watchEntityStates, handleEntityUpdates, and entityStateToMessage are in entity_watcher.go
// loadRuleDefinitionsFromFiles and loadRules are in rule_loader.go

// run is the main background goroutine that handles processor lifecycle
func (rp *Processor) run(ctx context.Context) {
	defer close(rp.done)
	
	// Start KV watchers for entity state changes
	if err := rp.watchEntityStates(ctx); err != nil {
		rp.logger.Warn("Failed to start entity state watching", "error", err)
		// Don't fail - rules can still process semantic messages
	}

	// Subscribe to input subjects
	if err := rp.setupSubscriptions(ctx); err != nil {
		rp.logger.Error("Failed to setup subscriptions", "error", err)
		return
	}
	
	// Wait for shutdown signal or context cancellation
	select {
	case <-rp.shutdown:
		rp.logger.Info("Rule processor shutdown requested")
	case <-ctx.Done():
		rp.logger.Info("Rule processor context cancelled", "error", ctx.Err())
	}
}

// Start begins processing messages through rules
func (rp *Processor) Start(ctx context.Context) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.shutdown != nil {
		return errors.WrapInvalid(errors.ErrAlreadyStarted, "RuleProcessor", "Start", "check processor state")
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

	// Create shutdown and done channels for coordination
	rp.shutdown = make(chan struct{})
	rp.done = make(chan struct{})
	rp.startTime = time.Now()
	rp.health.Healthy = true

	// Start background goroutine with context
	go rp.run(ctx)

	rp.isSubscribed = true

	// Count subjects for logging
	subjectCount := 0
	for _, port := range rp.config.Ports.Inputs {
		if port.Type == "nats" && port.Subject != "" {
			subjectCount++
		}
	}

	rp.logger.Info("Rule processor started", "subject_count", subjectCount)
	return nil
}

// setupSubscriptions creates NATS subscriptions for input subjects
func (rp *Processor) setupSubscriptions(ctx context.Context) error {
	if !rp.natsClient.IsHealthy() {
		return errors.WrapFatal(errors.ErrNoConnection, "RuleProcessor", "Start", "check NATS health")
	}

	// Get subjects from ports
	var subjects []string
	for _, port := range rp.config.Ports.Inputs {
		if port.Type == "nats" && port.Subject != "" {
			subjects = append(subjects, port.Subject)
		}
	}

	for _, subject := range subjects {
		// Skip entity.events subjects since we use KV watch for entity states
		if strings.HasPrefix(subject, "events.graph.entity") {
			rp.logger.Info("Skipping NATS subscription - using KV watch for entity states", "subject", subject)
			continue
		}

		err := rp.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, data []byte) {
			rp.handleMessage(msgCtx, subject, data)
		})
		if err != nil {
			return errors.Wrap(err, "RuleProcessor", "Start", fmt.Sprintf("subscribe to %s", subject))
		}

		rp.logger.Info("Rule processor subscribed", "subject", subject)
	}

	return nil
}

// Message handling functions (handleMessage, handleSemanticMessage, evaluateRulesForMessage,
// matchesRuleSubject, recordError) are in message_handler.go

// Stop stops the processor and cleans up resources
func (rp *Processor) Stop(timeout time.Duration) error {
	rp.mu.Lock()
	if rp.shutdown == nil {
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

	// Clean up resources
	rp.mu.Lock()
	defer rp.mu.Unlock()
	
	// Stop all entity watchers
	for _, watcher := range rp.entityWatchers {
		if err := watcher.Stop(); err != nil {
			rp.logger.Error("Error stopping entity watcher", "error", err)
		}
	}
	rp.entityWatchers = nil

	// Clean up all rules
	rp.rules = nil

	// Legacy JetStream consumer cleanup (if still exists)
	if rp.entityConsumer != nil {
		rp.logger.Info("Legacy JetStream consumer stopped")
	}

	// Note: NATS client handles unsubscription during context cancellation
	rp.isSubscribed = false

	// Close message cache
	if rp.messageCache != nil {
		if err := rp.messageCache.Close(); err != nil {
			rp.logger.Warn("Failed to close message cache", "error", err)
		}
	}

	rp.shutdown = nil
	rp.done = nil
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
