// Package agentictools provides a tool executor processor component
// that routes tool calls to registered tool executors with filtering and timeout support.
package agentictools

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
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// agenticToolsSchema defines the configuration schema
var agenticToolsSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the agentic-tools processor
type Component struct {
	name       string
	config     Config
	registry   *ExecutorRegistry
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Metrics
	requestsProcessed int64
	errors            int64
	lastActivity      time.Time
	metrics           *toolsMetrics

	// Approval filter (nil when approval_required is empty)
	approvalFilter *ApprovalFilter

	// Subscriptions (for cleanup)
	toolListSub *natsclient.Subscription

	// Track consumers for cleanup
	consumerInfos []consumerInfo
}

// consumerInfo tracks JetStream consumer details for cleanup
type consumerInfo struct {
	streamName   string
	consumerName string
}

// NewComponent creates a new agentic-tools processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}

	comp := &Component{
		name:       "agentic-tools",
		config:     config,
		registry:   NewExecutorRegistry(),
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		metrics:    getMetrics(deps.MetricsRegistry),
	}

	if len(config.ApprovalRequired) > 0 {
		comp.approvalFilter = NewApprovalFilter(config.ApprovalRequired)
	}

	return comp, nil
}

// Initialize prepares the component (no-op for this component)
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing tool calls
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "check running state")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}

	// Set up consumers for input ports
	for _, port := range c.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		// Only set up JetStream consumers for jetstream ports
		// Plain NATS subscriptions are handled separately below
		if port.Type == "jetstream" {
			if err := c.setupConsumer(ctx, port); err != nil {
				return errs.Wrap(err, "Component", "Start", fmt.Sprintf("setup consumer for %s", port.Subject))
			}
		}
	}

	// tool.list for request/reply discovery
	// Use subject from port config if available, otherwise default to "tool.list"
	// Note: If your JetStream stream subjects include "tool.>" or similar patterns,
	// you must configure a different subject (e.g., "discovery.tool.list") to avoid
	// JetStream capturing the request/reply message before the handler responds.
	toolListSubject := "tool.list"
	for _, port := range c.config.Ports.Inputs {
		if port.Name == "tool_list_request" {
			if port.Subject != "" {
				toolListSubject = port.Subject
			}
			break
		}
	}
	sub, err := c.natsClient.SubscribeForRequests(ctx, toolListSubject, c.handleToolListRequest)
	if err != nil {
		c.logger.Warn("Failed to subscribe to tool.list", "error", err, "subject", toolListSubject)
	} else {
		c.toolListSub = sub
		c.logger.Info("Subscribed to tool.list", "subject", toolListSubject)
	}

	c.running = true
	c.startTime = time.Now()

	return nil
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, port component.PortDefinition) error {
	// Determine stream name
	streamName := port.StreamName
	if streamName == "" {
		streamName = c.config.StreamName
	}
	if streamName == "" {
		streamName = "AGENT"
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name (with optional suffix for uniqueness in tests)
	consumerName := fmt.Sprintf("agentic-tools-%s", sanitizeSubject(port.Subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new tool calls, don't replay old ones
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:     streamName,
		ConsumerName:   consumerName,
		FilterSubject:  port.Subject,
		DeliverPolicy:  consumerCfg.DeliverPolicy,
		AckPolicy:      consumerCfg.AckPolicy,
		MaxDeliver:     3,
		AckWait:        5 * time.Minute,
		MaxAckPending:  3,
		BackOff:        []time.Duration{15 * time.Second, 60 * time.Second},
		AutoCreate:     false,
		MessageTimeout: 10 * time.Minute,
	}

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleToolCall(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("consumer setup for stream %s", streamName))
	}

	// Track consumer for cleanup in Stop()
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   streamName,
		consumerName: consumerName,
	})

	c.logger.Info("Subscribed to tool calls (JetStream)",
		"subject", port.Subject,
		"stream", streamName,
		"consumer", consumerName)
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

	return errs.WrapTransient(errs.ErrStorageUnavailable, "Component", "waitForStream", fmt.Sprintf("stream %s not found after %d retries", streamName, maxRetries))
}

// sanitizeSubject converts a subject pattern to a valid consumer name suffix
func sanitizeSubject(subject string) string {
	s := strings.ReplaceAll(subject, ".", "-")
	s = strings.ReplaceAll(s, ">", "all")
	s = strings.ReplaceAll(s, "*", "any")
	return s
}

// Stop gracefully stops the component within the given timeout
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Unsubscribe from tool.list request handler
	if c.toolListSub != nil {
		if err := c.toolListSub.Unsubscribe(); err != nil {
			c.logger.Warn("tool list subscription unsubscribe error", slog.Any("error", err))
		}
		c.toolListSub = nil
	}

	// Stop all JetStream consumers
	for _, info := range c.consumerInfos {
		if c.config.DeleteConsumerOnStop {
			// Delete consumer from server (for test cleanup)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			if err := c.natsClient.StopAndDeleteConsumer(ctx, info.streamName, info.consumerName); err != nil {
				c.logger.Debug("Failed to delete consumer", "stream", info.streamName, "consumer", info.consumerName, "error", err)
			} else {
				c.logger.Debug("Stopped and deleted consumer", "stream", info.streamName, "consumer", info.consumerName)
			}
			cancel()
		} else {
			// Just stop local consumption (keep durable consumer for resume)
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
			c.logger.Debug("Stopped consumer", "stream", info.streamName, "consumer", info.consumerName)
		}
	}
	c.consumerInfos = nil

	c.running = false
	return nil
}

// handleToolCall processes a tool call request
func (c *Component) handleToolCall(ctx context.Context, data []byte) {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	// Parse BaseMessage envelope
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		c.incrementErrors()
		return
	}

	// Extract ToolCall from payload
	callPtr, ok := baseMsg.Payload().(*agentic.ToolCall)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		c.incrementErrors()
		return
	}
	call := *callPtr

	c.logger.Debug("Processing tool call",
		slog.String("tool", call.Name),
		slog.String("call_id", call.ID))

	// Check if tool is allowed
	if !c.isToolAllowed(call.Name) {
		c.logger.Warn("Tool not allowed", "tool", call.Name)

		if c.metrics != nil {
			c.metrics.recordToolFiltered(call.Name, "not_allowed")
		}

		result := agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("tool %q is not allowed", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
			LoopID:    call.LoopID,
			TraceID:   call.TraceID,
		}
		c.publishResult(ctx, result)
		c.incrementErrors()
		return
	}

	// Check approval filter
	if c.approvalFilter != nil {
		filterResult, filterErr := c.approvalFilter.FilterToolCalls(call.LoopID, []agentic.ToolCall{call})
		if filterErr != nil {
			c.logger.Error("Approval filter error", "tool", call.Name, "error", filterErr)
		} else if len(filterResult.Rejected) > 0 {
			c.logger.Info("Tool requires approval", "tool", call.Name)

			if c.metrics != nil {
				c.metrics.recordToolFiltered(call.Name, "approval_required")
			}

			result := agentic.ToolResult{
				CallID:    call.ID,
				Error:     filterResult.Rejected[0].Reason,
				ErrorKind: agentic.ToolErrorPermission,
				LoopID:    call.LoopID,
				TraceID:   call.TraceID,
			}
			c.publishResult(ctx, result)
			return
		}
	}

	// Execute tool with timeout
	startTime := time.Now()
	result, err := c.executeWithTimeout(ctx, call)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		c.logger.Error("Failed to execute tool", "tool", call.Name, "error", err)

		// Classify. Framework-level timeout takes precedence over any
		// executor-set kind (a deeper cause like "network" may be masking
		// a context deadline).
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			result.ErrorKind = agentic.ToolErrorTimeout
			if result.Error == "" {
				result.Error = err.Error()
			}
			if c.metrics != nil {
				c.metrics.recordExecutionTimeout(call.Name, duration)
			}
		} else {
			if result.ErrorKind == "" {
				result.ErrorKind = agentic.ToolErrorUnknown
			}
			if result.Error == "" {
				result.Error = err.Error()
			}
			if c.metrics != nil {
				c.metrics.recordExecutionError(call.Name, string(result.ErrorKind), duration)
			}
		}

		c.incrementErrors()
	} else if result.Error != "" {
		// Tool executed but returned an error result
		if result.ErrorKind == "" {
			result.ErrorKind = agentic.ToolErrorUnknown
		}
		if c.metrics != nil {
			c.metrics.recordExecutionError(call.Name, string(result.ErrorKind), duration)
		}
		c.logger.Debug("Tool returned error",
			slog.String("tool", call.Name),
			slog.String("error_kind", string(result.ErrorKind)),
			slog.String("error", result.Error))
	} else {
		// Successful execution
		if c.metrics != nil {
			c.metrics.recordExecutionSuccess(call.Name, duration)
		}
		c.logger.Debug("Tool executed successfully",
			slog.String("tool", call.Name),
			slog.Float64("duration_seconds", duration))
	}

	// Propagate trace correlation fields from call to result
	result.LoopID = call.LoopID
	result.TraceID = call.TraceID

	// Publish result
	if err := c.publishResult(ctx, result); err != nil {
		c.logger.Error("Failed to publish result", "error", err)
		c.incrementErrors()
		return
	}

	c.mu.Lock()
	c.requestsProcessed++
	c.mu.Unlock()
}

// isToolAllowed checks if a tool is in the allowed list
// Returns true if AllowedTools is nil/empty (allow all) or if tool is in the list
func (c *Component) isToolAllowed(toolName string) bool {
	if c.config.AllowedTools == nil || len(c.config.AllowedTools) == 0 {
		return true // Allow all tools
	}

	for _, allowed := range c.config.AllowedTools {
		if allowed == toolName {
			return true
		}
	}

	return false
}

// executeWithTimeout executes a tool call with the configured timeout.
// It first checks the component's local registry, then falls back to the global registry.
func (c *Component) executeWithTimeout(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	timeout := 60 * time.Second
	if c.config.Timeout != "" {
		if d, err := time.ParseDuration(c.config.Timeout); err == nil {
			timeout = d
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Try local registry first
	result, err := c.registry.Execute(callCtx, call)
	if err != nil && strings.Contains(err.Error(), "not found") {
		// Fallback to global registry
		return GetGlobalRegistry().Execute(callCtx, call)
	}
	return result, err
}

// publishResult publishes a tool result to JetStream
func (c *Component) publishResult(ctx context.Context, result agentic.ToolResult) error {
	resultMsg := message.NewBaseMessage(result.Schema(), &result, "agentic-tools")
	data, err := json.Marshal(resultMsg)
	if err != nil {
		return errs.Wrap(err, "Component", "publishResult", "marshal result")
	}

	// Publish to output subjects
	for _, port := range c.config.Ports.Outputs {
		if port.Subject == "" {
			continue
		}

		// Replace wildcard with call ID for specific routing
		subject := port.Subject
		if len(subject) > 0 && subject[len(subject)-1] == '*' {
			subject = subject[:len(subject)-1] + result.CallID
		}

		// Use JetStream for publishing to ensure delivery
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return errs.WrapTransient(err, "Component", "publishResult", fmt.Sprintf("publish to %s", subject))
		}
	}

	return nil
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// RegisterToolExecutor registers a tool executor with the component
// This method extracts all tools from the executor and registers them individually
func (c *Component) RegisterToolExecutor(executor ToolExecutor) error {
	// Get all tools from the executor
	tools := executor.ListTools()

	// Register each tool
	for _, tool := range tools {
		if err := c.registry.RegisterTool(tool.Name, executor); err != nil {
			return err
		}
	}

	// Record total registered tools
	if c.metrics != nil {
		allTools := c.registry.ListTools()
		c.metrics.recordToolsRegistered(len(allTools))
		c.logger.Info("Tools registered",
			slog.Int("count", len(tools)),
			slog.Int("total", len(allTools)))
	}

	return nil
}

// ListTools returns all tool definitions for discovery.
// Combines tools from both the component's local registry and the global registry.
func (c *Component) ListTools() []ToolDefinition {
	// Get tools from component's local registry
	localTools := c.registry.ListTools()

	// Get tools from global registry
	globalTools := ListRegisteredTools()

	// Combine and convert to ToolDefinition format
	// Use a map to deduplicate by name (local registry takes precedence)
	toolMap := make(map[string]ToolDefinition)

	// Add global tools first
	for _, tool := range globalTools {
		toolMap[tool.Name] = ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Provider:    "internal",
			Available:   true,
		}
	}

	// Add local tools (overwrites global if same name)
	for _, tool := range localTools {
		toolMap[tool.Name] = ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Provider:    "internal",
			Available:   true,
		}
	}

	// Convert map to slice and sort for deterministic ordering
	tools := make([]ToolDefinition, 0, len(toolMap))
	for _, tool := range toolMap {
		tools = append(tools, tool)
	}

	// Sort by name for consistent discovery responses
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	return tools
}

// Execute executes a tool call (for testing and direct invocation)
func (c *Component) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	// Check if tool is allowed
	if !c.isToolAllowed(call.Name) {
		result := agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("tool %q is not allowed", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
			LoopID:    call.LoopID,
			TraceID:   call.TraceID,
		}
		return result, errs.WrapInvalid(fmt.Errorf("tool %q is not allowed", call.Name), "Component", "Execute", "check tool allowed")
	}

	// Execute with timeout. If the (internal) deadline fired OR the
	// outer ctx was canceled by the caller, tag the result as a timeout
	// so downstream graph writers emit the right error_category. Timeout
	// takes precedence over any executor-set kind.
	result, err := c.executeWithTimeout(ctx, call)
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil) {
		result.ErrorKind = agentic.ToolErrorTimeout
		if result.Error == "" {
			result.Error = err.Error()
		}
	}
	// Propagate trace correlation fields
	result.LoopID = call.LoopID
	result.TraceID = call.TraceID
	return result, err
}

// handleToolListRequest handles tool.list request/reply for tool discovery
func (c *Component) handleToolListRequest(_ context.Context, _ []byte) ([]byte, error) {
	tools := c.ListTools() // Uses combined listing (internal + external)
	c.logger.Debug("Handling tool.list request", "tool_count", len(tools))
	response := ToolListResponse{Tools: tools}
	return json.Marshal(response)
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "agentic-tools",
		Type:        "processor",
		Description: "Tool executor processor with filtering and timeout support",
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
		port := component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
		}
		// Use JetStreamPort for jetstream type ports, NATSPort for others (e.g., request/reply)
		if portDef.Type == "jetstream" {
			port.Config = component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			}
		} else {
			port.Config = component.NATSPort{
				Subject: portDef.Subject,
			}
		}
		ports[i] = port
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
		port := component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
		}
		// Use JetStreamPort for jetstream type ports, NATSPort for others
		if portDef.Type == "jetstream" {
			port.Config = component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			}
		} else {
			port.Config = component.NATSPort{
				Subject: portDef.Subject,
			}
		}
		ports[i] = port
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return agenticToolsSchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     c.getStatus(),
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
	c.mu.RLock()
	defer c.mu.RUnlock()

	var errorRate float64
	total := c.requestsProcessed + c.errors
	if total > 0 {
		errorRate = float64(c.errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}
