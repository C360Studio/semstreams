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
	"github.com/c360studio/semstreams/governance"
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
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		defaultCfg := DefaultConfig()
		config.Ports = defaultCfg.Ports

		// Also set filter chain defaults if not specified
		if len(config.FilterChain.Filters) == 0 {
			config.FilterChain = defaultCfg.FilterChain
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
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

	if config.EnableToolGovernance {
		var piiFilter *PIIFilter
		for _, f := range chain.Filters {
			if pf, ok := f.(*PIIFilter); ok {
				piiFilter = pf
				break
			}
		}
		chain.AddFilter(NewToolCallFilter(piiFilter))
		logger.Info("Tool call governance filter enabled")
	}

	// Create violation handler. Pass the component's output port defs so
	// the handler can resolve publish subjects via component.ResolveSubject
	// (honors any per-deployment port overrides on violations/user_errors).
	var outputs []component.PortDefinition
	if config.Ports != nil {
		outputs = config.Ports.Outputs
	}
	violationHandler := NewViolationHandler(config.Violations, deps.NATSClient, logger, metrics, outputs)

	return &Component{
		name:       "agentic-governance",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		chain:      chain,
		violations: violationHandler,
		metrics:    metrics,
	}, nil
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
	for _, port := range c.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

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

	// Build a governance.Context from the ChainResult and attach it to ctx
	// so natsclient.Publish auto-injects X-Gov-* headers onto the outbound message.
	// The filter-name → bool mapping uses governance.FilterName* constants (stable
	// wire-format identifiers matching Filter.Name() return values).
	gc := chainResultToGovernanceContext(result)
	ctx = governance.WithContext(ctx, gc)

	// Publish validated message
	if c.natsClient != nil {
		outputMsg := result.ModifiedMessage
		if outputMsg == nil {
			outputMsg = &msg
		}

		// Build output subject from port config, falling back to portName + "." + msg.ID
		outputSubject := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)

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

// chainResultToGovernanceContext converts a filter chain ChainResult into the
// top-level governance.Context that gets propagated on NATS message headers.
//
// Filter-name → bool field mapping uses governance.FilterName* constants
// (stable wire-format identifiers that match each Filter.Name() return value).
// MaxSeverity is derived from ChainResult.HighestSeverity; the agentic-governance
// Severity type is a string alias, so the cast to string is safe and avoids an
// import cycle (agentic-governance imports governance, not the reverse).
// Allowed mirrors ChainResult.Allowed so downstream rules can gate on
// $message.allowed without re-inspecting individual violation flags.
func chainResultToGovernanceContext(result *ChainResult) *governance.Context {
	gc := &governance.Context{
		Allowed:        result.Allowed,
		ViolationCount: len(result.Violations),
	}

	// Map per-filter outcomes to boolean fields via stable FilterName* constants.
	for _, v := range result.Violations {
		switch v.FilterName {
		case governance.FilterNamePii:
			gc.HasPii = true
		case governance.FilterNameInjection:
			gc.HasInjection = true
		case governance.FilterNameContentModeration:
			gc.HasContentModeration = true
		case governance.FilterNameRateLimiting:
			gc.HasRateLimiting = true
		}
	}

	// MaxSeverity: derive from HighestSeverity() rather than walking violations
	// again. Severity is a string-based type, so string() cast is safe.
	if s := result.HighestSeverity(); s != "" {
		gc.MaxSeverity = string(s)
	} else {
		gc.MaxSeverity = governance.SeverityNone
	}

	return gc
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, port component.PortDefinition, handler func(context.Context, []byte)) error {
	streamName := c.config.StreamName
	if streamName == "" {
		streamName = "AGENT"
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name
	consumerName := fmt.Sprintf("agentic-governance-%s", sanitizeSubject(port.Subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject,
		"port", port.Name)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new messages, don't replay old ones
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

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handler(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	c.logger.Info("Subscribed (JetStream)",
		"subject", port.Subject,
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
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = buildInputPort(portDef)
	}
	return ports
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = buildOutputPort(portDef)
	}
	return ports
}

// buildInputPort creates a Port from a PortDefinition for input
func buildInputPort(portDef component.PortDefinition) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   component.DirectionInput,
		Required:    portDef.Required,
		Description: portDef.Description,
	}

	switch portDef.Type {
	case "jetstream":
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	case "kv-watch", "kvwatch":
		port.Config = component.KVWatchPort{
			Bucket: portDef.Bucket,
		}
	default:
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}

	return port
}

// buildOutputPort creates a Port from a PortDefinition for output
func buildOutputPort(portDef component.PortDefinition) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   component.DirectionOutput,
		Required:    portDef.Required,
		Description: portDef.Description,
	}

	switch portDef.Type {
	case "jetstream":
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	default:
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}

	return port
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
