// Package agenticgovernance provides a governance layer processor component
// that enforces content policies, PII redaction, injection detection,
// and rate limiting for agentic message flows.
package agenticgovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// agenticGovernanceSchema defines the configuration schema
var agenticGovernanceSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the agentic-governance processor
type Component struct {
	name       string
	config     Config
	inputs     []component.Port
	outputs    []component.Port
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Filter chain
	chain *FilterChain

	// Violation handler
	violations *ViolationHandler

	// Metrics
	metrics *governanceMetrics

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Counters
	messagesProcessed int64
	violationsCount   int64
	errors            int64
	lastActivity      time.Time
}

// NewComponent creates a new agentic-governance processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	if err := rejectRetiredNotifyUser(rawConfig); err != nil {
		return nil, err
	}
	defaults := DefaultConfig()
	config := DefaultConfig()
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	mergedPorts, err := component.MergePortConfig(*defaults.Ports, *config.Ports)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "merge port overrides")
	}
	config.Ports = &mergedPorts

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve input port")
		}
		inputs = append(inputs, port)
	}
	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve output port")
		}
		outputs = append(outputs, port)
	}

	logger := deps.GetLogger()
	if logger == nil {
		logger = slog.Default()
	}

	// Get metrics registry
	var metricsRegistry = deps.MetricsRegistry
	metrics := getMetrics(metricsRegistry)

	// Build filter chain
	chain, err := BuildFromConfig(config.FilterChain, metrics)
	if err != nil {
		return nil, errs.Wrap(err, "Component", "NewComponent", "build filter chain")
	}

	// Post-build dependency wiring for tool_call_governance. createFilter
	// can't resolve the live PIIFilter sibling pointer at chain-build
	// time, so config-declared tool_call_governance filters are built
	// with piiFilter=nil and patched here.
	//
	// Implicit ordering invariant: this patch happens between chain
	// construction and component lifecycle Start. Any future code that
	// mutates chain.Filters after NewComponent returns would skip this
	// loop — add filters via this codepath or extend the patch logic.
	//
	// The nil-piiFilter check below intentionally overrides any
	// *ToolCallFilter whose piiFilter was nil at construction. Today
	// every code path lands here with nil (createFilter passes nil
	// explicitly). A future caller that programmatically constructs a
	// ToolCallFilter with deliberately-nil piiFilter and adds it to the
	// chain would be silently rewired — construct outside this codepath
	// or use a sentinel if that's not desired.
	var piiFilter *PIIFilter
	var hasToolGovernance bool
	for _, f := range chain.Filters {
		switch v := f.(type) {
		case *PIIFilter:
			if piiFilter == nil {
				piiFilter = v
			}
		case *ToolCallFilter:
			hasToolGovernance = true
		}
	}
	for _, f := range chain.Filters {
		if tcf, ok := f.(*ToolCallFilter); ok && tcf.piiFilter == nil {
			tcf.piiFilter = piiFilter
		}
	}

	// Legacy EnableToolGovernance: only auto-append when the chain
	// doesn't already include a config-declared tool_call_governance
	// filter, to avoid running the filter twice.
	if config.EnableToolGovernance && !hasToolGovernance {
		chain.AddFilter(NewToolCallFilter(piiFilter))
		logger.Info("Tool call governance filter enabled (legacy EnableToolGovernance)")
	}

	// Create violation handler. Pass the component's output port defs so
	// the handler can resolve the violation event subject via the accepted
	// port declaration.
	var outputDefs []component.PortDefinition
	if config.Ports != nil {
		outputDefs = config.Ports.Outputs
	}
	violationHandler := NewViolationHandler(config.Violations, deps.NATSClient, logger, metrics, outputDefs)

	return &Component{
		name:       "agentic-governance",
		config:     config,
		inputs:     inputs,
		outputs:    outputs,
		natsClient: deps.NATSClient,
		logger:     logger,
		chain:      chain,
		violations: violationHandler,
		metrics:    metrics,
	}, nil
}

func rejectRetiredNotifyUser(rawConfig json.RawMessage) error {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &outer); err != nil {
		return nil // The production config decode below reports malformed JSON.
	}
	rawViolations, ok := outer["violations"]
	if !ok {
		return nil
	}
	var violations map[string]json.RawMessage
	if err := json.Unmarshal(rawViolations, &violations); err != nil {
		return nil // The production config decode below reports the type error.
	}
	if _, present := violations["notify_user"]; present {
		return errs.WrapInvalid(
			fmt.Errorf("violations.notify_user was removed; delete the key before adopting this breaking version"),
			"Component", "NewComponent", "reject retired governance user notification")
	}
	return nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing governance events
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errs.ErrAlreadyStarted
	}
	c.running = true
	c.mu.Unlock()

	// NATS client is optional for unit tests
	if c.natsClient != nil {
		if err := c.setupInputConsumers(ctx); err != nil {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return errs.Wrap(err, "Component", "Start", "setup input consumers")
		}
	}

	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()

	c.logger.Info("Agentic governance component started",
		"filters", len(c.chain.Filters),
		"policy", c.chain.Policy,
	)

	return nil
}

// setupInputConsumers sets up JetStream consumers for all input ports
func (c *Component) setupInputConsumers(ctx context.Context) error {
	for _, port := range c.inputs {
		var msgType MessageType
		var outputPortName string

		// Route to appropriate handler based on port name
		switch port.Name {
		case "task_validation":
			msgType = MessageTypeTask
			outputPortName = "agent.task.validated"
		case "request_validation":
			msgType = MessageTypeRequest
			outputPortName = "agent.request.validated"
		case "response_validation":
			msgType = MessageTypeResponse
			outputPortName = "agent.response.validated"
		default:
			c.logger.Debug("Skipping unknown input port", "port", port.Name)
			continue
		}

		handler := c.createHandler(msgType, outputPortName)
		if err := c.setupConsumer(ctx, port, handler); err != nil {
			return errs.Wrap(err, "Component", "setupInputConsumers", fmt.Sprintf("setup consumer for %s", port.Name))
		}
	}

	return nil
}

// createHandler creates a message handler for a specific message type.
// outputPortName is the output port name used to resolve the publish subject via port config.
func (c *Component) createHandler(msgType MessageType, outputPortName string) func(context.Context, []byte) {
	return func(ctx context.Context, data []byte) {
		c.handleMessage(ctx, data, msgType, outputPortName)
	}
}

// handleMessage processes a message through the filter chain.
// outputPortName identifies the output port in config whose subject pattern is used to publish validated messages.
func (c *Component) handleMessage(ctx context.Context, data []byte, msgType MessageType, outputPortName string) {
	// Parse the incoming message
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Error("Failed to unmarshal message", "error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	msg.Type = msgType
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Process through filter chain
	result, err := c.chain.Process(ctx, &msg)
	if err != nil {
		c.logger.Error("Filter chain error",
			"error", err,
			"message_id", msg.ID,
			"user_id", msg.UserID,
		)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Update activity
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	atomic.AddInt64(&c.messagesProcessed, 1)

	// Record metrics
	if c.metrics != nil {
		c.metrics.recordMessageProcessed(msgType, result.Allowed)
	}

	// Handle violations
	if result.HasViolations() {
		atomic.AddInt64(&c.violationsCount, int64(len(result.Violations)))
		for _, violation := range result.Violations {
			if err := c.violations.Handle(ctx, violation); err != nil {
				c.logger.Error("Failed to handle violation",
					"error", err,
					"violation_id", violation.ID,
				)
			}
		}
	}

	// If blocked, don't forward
	if !result.Allowed {
		c.logger.Info("Message blocked by governance",
			"message_id", msg.ID,
			"user_id", msg.UserID,
			"filters", result.FiltersApplied,
			"violations", len(result.Violations),
		)
		return
	}

	// Add governance metadata
	result.AddGovernanceMetadata()

	// Publish validated message
	if c.natsClient != nil {
		outputMsg := result.ModifiedMessage
		if outputMsg == nil {
			outputMsg = &msg
		}

		// Build output subject from port config, falling back to portName + "." + msg.ID
		outputSubject, resolveErr := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)
		if resolveErr != nil {
			c.logger.Error("Failed to resolve validated output subject", "error", resolveErr)
			atomic.AddInt64(&c.errors, 1)
			return
		}

		outputData, err := json.Marshal(outputMsg)
		if err != nil {
			c.logger.Error("Failed to marshal output message", "error", err)
			atomic.AddInt64(&c.errors, 1)
			return
		}

		if err := c.natsClient.Publish(ctx, outputSubject, outputData); err != nil {
			c.logger.Error("Failed to publish validated message",
				"error", err,
				"subject", outputSubject,
			)
			atomic.AddInt64(&c.errors, 1)
		}
	}
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, port component.Port, handler func(context.Context, []byte)) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok || len(stream.Subjects()) != 1 {
		return fmt.Errorf("port %s must declare one JetStream subject", port.Name)
	}
	streamName := stream.Name()
	subject := stream.Subjects()[0]

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name
	consumerName := fmt.Sprintf("agentic-governance-%s", sanitizeSubject(subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject,
		"port", port.Name)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new messages, don't replay old ones
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "Component", "setupConsumer", "resolve consumer config")
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	err = c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handler(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	c.logger.Info("Subscribed (JetStream)",
		"subject", subject,
		"stream", streamName,
		"consumer", consumerName,
		"port", port.Name)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Component", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
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

	return errs.WrapTransient(fmt.Errorf("stream %s not found after %d retries", streamName, maxRetries), "Component", "waitForStream", "find stream")
}

// sanitizeSubject converts a subject pattern to a valid consumer name suffix
func sanitizeSubject(subject string) string {
	s := strings.ReplaceAll(subject, ".", "-")
	s = strings.ReplaceAll(s, ">", "all")
	s = strings.ReplaceAll(s, "*", "any")
	return s
}

// Stop gracefully stops the component within the given timeout
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false
	c.logger.Info("Agentic governance component stopped")
	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "agentic-governance",
		Type:        "processor",
		Description: "Content governance layer for agentic systems with PII redaction, injection detection, and rate limiting",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputs...)
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return agenticGovernanceSchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	errors := atomic.LoadInt64(&c.errors)

	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	status := c.getStatus()
	c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(errors),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// getStatus returns a status string
func (c *Component) getStatus() string {
	if c.running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	messagesProcessed := atomic.LoadInt64(&c.messagesProcessed)
	errors := atomic.LoadInt64(&c.errors)

	c.mu.RLock()
	lastActivity := c.lastActivity
	c.mu.RUnlock()

	var errorRate float64
	total := messagesProcessed + errors
	if total > 0 {
		errorRate = float64(errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      lastActivity,
	}
}

// outputPortDefs returns the output port definitions slice, or nil when Ports is unset.
// This lets ResolveSubject fall back gracefully to portName + "." + suffix.
func (c *Component) outputPortDefs() []component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	return c.config.Ports.Outputs
}

// ProcessMessage is a convenience method for testing filter chain processing
func (c *Component) ProcessMessage(ctx context.Context, msg *Message) (*ChainResult, error) {
	return c.chain.Process(ctx, msg)
}

// (Beta.70 retired the ToolCallFilter() accessor. The
// `*ToolCallFilter` struct remains as a chain-internal Filter; cross-
// component governance now flows through subject-mode rules per
// ADR-039. The chain instance is reachable via ProcessMessage above
// if a future caller needs to run the chain manually.)
