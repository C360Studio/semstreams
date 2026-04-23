// Package agenticmodel provides an OpenAI-compatible agentic model processor component
// that routes agent requests to configured LLM endpoints with retry logic and tool calling support.
package agenticmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// agenticModelSchema defines the configuration schema
var agenticModelSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the agentic-model processor
type Component struct {
	name          string
	config        Config
	modelRegistry model.RegistryReader
	natsClient    *natsclient.Client
	logger        *slog.Logger

	// Dynamic client cache — clients are created on-demand from registry endpoints
	clientCache map[string]*Client // cache key -> client
	clientMu    sync.Mutex

	// Parsed timeout for message processing
	messageTimeout time.Duration

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Track consumers for cleanup
	consumerInfos []consumerInfo

	// Metrics
	requestsProcessed int64
	errors            int64
	lastActivity      time.Time
	metrics           *modelMetrics
}

// consumerInfo tracks JetStream consumer details for cleanup
type consumerInfo struct {
	streamName   string
	consumerName string
}

// NewComponent creates a new agentic-model processor component
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

	// Require model registry
	if deps.ModelRegistry == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Component", "NewComponent", "model registry is required")
	}

	// Parse timeout for message processing
	messageTimeout := 120 * time.Second // default
	if config.Timeout != "" {
		var err error
		messageTimeout, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "parse timeout")
		}
	}

	return &Component{
		name:           "agentic-model",
		config:         config,
		modelRegistry:  deps.ModelRegistry,
		clientCache:    make(map[string]*Client),
		natsClient:     deps.NATSClient,
		logger:         deps.GetLogger(),
		messageTimeout: messageTimeout,
		metrics:        getMetrics(deps.MetricsRegistry),
	}, nil
}

// Initialize prepares the component (no-op for this component)
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing agent requests
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
		return errs.ErrAlreadyStarted
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}

	// Set up consumers for input ports
	for _, port := range c.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		if err := c.setupConsumer(ctx, port); err != nil {
			return errs.Wrap(err, "Component", "Start", fmt.Sprintf("setup consumer for %s", port.Subject))
		}
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
	consumerName := fmt.Sprintf("agentic-model-%s", sanitizeSubject(port.Subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new requests, don't replay old ones
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:     streamName,
		ConsumerName:   consumerName,
		FilterSubject:  port.Subject,
		DeliverPolicy:  consumerCfg.DeliverPolicy,
		AckPolicy:      consumerCfg.AckPolicy,
		MaxDeliver:     2,
		AckWait:        2 * time.Minute,
		MaxAckPending:  1,
		AutoCreate:     false,
		MessageTimeout: 30 * time.Minute,
	}

	heartbeatInterval := 90 * time.Second
	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,
			func(workCtx context.Context) error {
				c.handleRequest(workCtx, msg.Data())
				return nil
			},
		); hbErr != nil {
			c.logger.Error("Model handler error", "error", hbErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	// Track consumer for cleanup in Stop()
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   streamName,
		consumerName: consumerName,
	})

	c.logger.Info("Subscribed to agent requests (JetStream)",
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

	for i := range maxRetries {
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
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Create context with timeout for any cleanup operations
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Stop all JetStream consumers
	for _, info := range c.consumerInfos {
		if c.config.DeleteConsumerOnStop {
			// Delete consumer from server (for test cleanup)
			if err := c.natsClient.StopAndDeleteConsumer(ctx, info.streamName, info.consumerName); err != nil {
				c.logger.Debug("Failed to delete consumer", "stream", info.streamName, "consumer", info.consumerName, "error", err)
			} else {
				c.logger.Debug("Stopped and deleted consumer", "stream", info.streamName, "consumer", info.consumerName)
			}
		} else {
			// Just stop local consumption (keep durable consumer for resume)
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
			c.logger.Debug("Stopped consumer", "stream", info.streamName, "consumer", info.consumerName)
		}
	}
	c.consumerInfos = nil

	// Close all cached clients
	c.clientMu.Lock()
	for key, client := range c.clientCache {
		if err := client.Close(); err != nil {
			c.logger.Warn("Failed to close client", "key", key, "error", err)
		}
	}
	c.clientCache = make(map[string]*Client)
	c.clientMu.Unlock()

	// Check if we completed within timeout
	select {
	case <-ctx.Done():
		c.logger.Warn("Stop timed out", "timeout", timeout)
	default:
		// Completed within timeout
	}

	c.running = false
	return nil
}

// handleRequest processes an agent request
func (c *Component) handleRequest(ctx context.Context, data []byte) {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	req, err := c.parseAgentRequest(data)
	if err != nil {
		c.logger.Error("Failed to parse agent request", "error", err)
		c.incrementErrors()
		return
	}

	// Strip tools if endpoint doesn't support them
	if len(req.Tools) > 0 {
		ep := c.modelRegistry.GetEndpoint(req.Model)
		if ep != nil && !ep.SupportsTools {
			c.logger.Warn("Stripping tools from request: endpoint does not support tool calling",
				"model", req.Model,
				"tool_count", len(req.Tools))
			req.Tools = nil
		}
	}

	c.logger.Debug("Processing agent request",
		slog.String("request_id", req.RequestID),
		slog.String("model", req.Model),
		slog.String("role", req.Role))

	client, endpoint, capability, err := c.getClientForRequest(req)
	if err != nil {
		c.logger.Error("Failed to resolve endpoint", "error", err, "model", req.Model)
		c.publishErrorResponse(ctx, req.RequestID, err.Error())
		c.incrementErrors()
		return
	}

	startTime := time.Now()
	if c.metrics != nil {
		c.metrics.recordRequestStart(req.Model)
	}

	resp, err := c.executeRequest(ctx, client, req, endpoint, capability)
	duration := time.Since(startTime).Seconds()

	if err != nil || resp.Status == "error" {
		c.handleModelError(ctx, req, resp, err, duration)
		return
	}

	c.handleModelSuccess(ctx, req, resp, duration)
}

// parseAgentRequest extracts an AgentRequest from raw message data
func (c *Component) parseAgentRequest(data []byte) (agentic.AgentRequest, error) {
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		return agentic.AgentRequest{}, errs.WrapInvalid(err, "Component", "parseAgentRequest", "unmarshal BaseMessage")
	}

	reqPtr, ok := baseMsg.Payload().(*agentic.AgentRequest)
	if !ok {
		return agentic.AgentRequest{}, errs.WrapInvalid(fmt.Errorf("unexpected payload type: %T", baseMsg.Payload()), "Component", "parseAgentRequest", "check payload type")
	}

	return *reqPtr, nil
}

// handleModelError processes and publishes error responses with metrics.
// It preserves any partial token usage from the response for cost tracking.
func (c *Component) handleModelError(ctx context.Context, req agentic.AgentRequest, resp agentic.AgentResponse, err error, duration float64) {
	errorMsg := resp.Error
	if err != nil {
		errorMsg = err.Error()
	}

	c.logger.Error("Failed to complete chat", "error", errorMsg, "model", req.Model)

	errorType := classifyError(ctx, errorMsg)
	if c.metrics != nil {
		c.metrics.recordRequestError(req.Model, errorType, duration)
		// Record partial token usage even on errors
		if resp.TokenUsage.PromptTokens > 0 || resp.TokenUsage.CompletionTokens > 0 {
			c.metrics.recordTokenUsage(req.Model, resp.TokenUsage.PromptTokens, resp.TokenUsage.CompletionTokens)
		}
	}

	errorCtx, cancel := natsclient.DetachContextWithTrace(ctx, 5*time.Second)
	defer cancel()
	c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)
	c.incrementErrors()
}

// classifyError determines the error type for metrics categorization
func classifyError(ctx context.Context, errorMsg string) string {
	if ctx.Err() != nil {
		return "timeout"
	}
	if strings.Contains(errorMsg, "connection") {
		return "connection"
	}
	if strings.Contains(errorMsg, "rate limit") {
		return "rate_limit"
	}
	return "unknown"
}

// handleModelSuccess processes successful responses with metrics and publishing
func (c *Component) handleModelSuccess(ctx context.Context, req agentic.AgentRequest, resp agentic.AgentResponse, duration float64) {
	toolCallCount := len(resp.Message.ToolCalls)

	if c.metrics != nil {
		c.metrics.recordRequestComplete(req.Model, duration, toolCallCount)
		if resp.TokenUsage.PromptTokens > 0 || resp.TokenUsage.CompletionTokens > 0 {
			c.metrics.recordTokenUsage(req.Model, resp.TokenUsage.PromptTokens, resp.TokenUsage.CompletionTokens)
		}
		if resp.FinishReason == agentic.FinishReasonLength {
			c.metrics.recordLengthTruncation(req.Model)
		}
	}

	c.logger.Debug("Model request completed",
		slog.String("request_id", req.RequestID),
		slog.String("model", req.Model),
		slog.Float64("duration_seconds", duration),
		slog.Int("tool_calls", toolCallCount),
		slog.Int("prompt_tokens", resp.TokenUsage.PromptTokens),
		slog.Int("completion_tokens", resp.TokenUsage.CompletionTokens))

	if err := c.publishResponse(ctx, resp); err != nil {
		c.logger.Error("Failed to publish response", "error", err)
		c.incrementErrors()
		return
	}

	c.mu.Lock()
	c.requestsProcessed++
	c.mu.Unlock()
}

// getClientForRequest resolves an endpoint from the registry and returns a cached or new client.
//
// Resolution order:
//  1. Capability chain — GetFallbackChain(req.Model) walks preferred+fallback endpoints.
//  2. Direct endpoint — GetEndpoint(req.Model) treats the name as an endpoint key.
//  3. Default fallback — GetDefault() -> GetEndpoint.
//  4. Error.
//
// This allows callers to set req.Model to either a capability name (e.g. "fast")
// or a concrete endpoint name (e.g. "ollama-qwen32b") and get the right client.
//
// Returns the resolved client, the endpoint config, and the capability name (empty
// when req.Model was a concrete endpoint name). Callers use the capability name to
// look up per-capability timeouts.
func (c *Component) getClientForRequest(req agentic.AgentRequest) (*Client, *model.EndpointConfig, string, error) {
	var ep *model.EndpointConfig
	var capability string

	// 1. Try capability-based resolution
	chain := c.modelRegistry.GetFallbackChain(req.Model)
	if len(chain) > 0 {
		capability = req.Model
	}
	for _, name := range chain {
		if candidate := c.modelRegistry.GetEndpoint(name); candidate != nil {
			ep = candidate
			break
		}
	}

	// 2. Try direct endpoint name
	if ep == nil {
		ep = c.modelRegistry.GetEndpoint(req.Model)
	}

	// 3. Fall back to default
	if ep == nil {
		defaultName := c.modelRegistry.GetDefault()
		if defaultName != "" {
			ep = c.modelRegistry.GetEndpoint(defaultName)
		}
	}

	if ep == nil {
		return nil, nil, "", errs.WrapInvalid(
			fmt.Errorf("no endpoint found for model %q in registry", req.Model),
			"Component", "getClientForRequest", "resolve endpoint",
		)
	}

	// Cache key: URL + model name
	cacheKey := ep.URL + "|" + ep.Model

	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if client, ok := c.clientCache[cacheKey]; ok {
		return client, ep, capability, nil
	}

	client, err := NewClient(ep)
	if err != nil {
		return nil, nil, "", errs.Wrap(err, "Component", "getClientForRequest", fmt.Sprintf("create client for model %q", req.Model))
	}

	// Wire provider adapter for request/response normalization.
	client.SetAdapter(AdapterFor(ep.Provider))

	// Wire logger, streaming chunk handler, and metrics
	client.SetLogger(c.logger)
	if ep.Stream {
		client.SetChunkHandler(c.makeChunkHandler())
	}
	if c.metrics != nil {
		client.SetMetrics(c.metrics)
	}

	// Apply production retry settings from component config.
	client.SetRetryConfig(c.config.Retry)

	// Attach rate/concurrency throttle when the endpoint declares limits.
	if ep.RequestsPerMinute > 0 || ep.MaxConcurrent > 0 {
		client.SetThrottle(NewEndpointThrottle(ep.RequestsPerMinute, ep.MaxConcurrent))
	}

	c.clientCache[cacheKey] = client
	return client, ep, capability, nil
}

// makeChunkHandler returns a ChunkHandler that publishes chunks to core NATS.
// Chunks are fire-and-forget (core NATS Publish, not JetStream) since they are
// ephemeral — dropped if no subscriber is listening.
func (c *Component) makeChunkHandler() ChunkHandler {
	return func(chunk StreamChunk) {
		data, err := json.Marshal(chunk)
		if err != nil {
			c.logger.Warn("Failed to marshal stream chunk", "error", err)
			return
		}

		subject := component.ResolveSubject(c.outputPortDefs(), "agent.stream", chunk.RequestID)
		if err := c.natsClient.Publish(context.Background(), subject, data); err != nil {
			c.logger.Debug("Failed to publish stream chunk", "subject", subject, "error", err)
		}
	}
}

// executeRequest executes the chat completion with a resolved timeout.
// Timeout precedence is applied by resolveTimeout.
func (c *Component) executeRequest(
	ctx context.Context,
	client *Client,
	req agentic.AgentRequest,
	endpoint *model.EndpointConfig,
	capability string,
) (agentic.AgentResponse, error) {
	timeout := c.resolveTimeout(req, endpoint, capability)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return client.ChatCompletion(reqCtx, req)
}

// resolveTimeout picks the effective request timeout using precedence:
//  1. req.Timeout (per-task override; propagated from TaskMessage.Timeout)
//  2. endpoint.RequestTimeout
//  3. capability.Timeout (looked up via registry when capability is non-empty)
//  4. component messageTimeout (cached from config.Timeout at construction)
//  5. hardcoded 120s safety default
//
// Invalid duration strings at levels 1-3 log a warning and fall through to the
// next level, so a malformed per-message override never blocks a task. The
// selected source is logged at debug level as timeout_source.
func (c *Component) resolveTimeout(req agentic.AgentRequest, endpoint *model.EndpointConfig, capability string) time.Duration {
	const fallback = 120 * time.Second

	tryParse := func(value, source string) (time.Duration, bool) {
		if value == "" {
			return 0, false
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			c.logger.Warn("Invalid timeout value; falling through",
				"source", source,
				"value", value,
				"error", err,
				"request_id", req.RequestID)
			return 0, false
		}
		return d, true
	}

	if d, ok := tryParse(req.Timeout, "task"); ok {
		c.logger.Debug("Resolved request timeout", "timeout_source", "task", "timeout", d, "request_id", req.RequestID)
		return d
	}

	if endpoint != nil {
		if d, ok := tryParse(endpoint.RequestTimeout, "endpoint"); ok {
			c.logger.Debug("Resolved request timeout", "timeout_source", "endpoint", "timeout", d, "request_id", req.RequestID)
			return d
		}
	}

	if capability != "" && c.modelRegistry != nil {
		if capCfg := c.modelRegistry.GetCapability(capability); capCfg != nil {
			if d, ok := tryParse(capCfg.Timeout, "capability"); ok {
				c.logger.Debug("Resolved request timeout",
					"timeout_source", "capability",
					"capability", capability,
					"timeout", d,
					"request_id", req.RequestID)
				return d
			}
		}
	}

	if c.messageTimeout > 0 {
		c.logger.Debug("Resolved request timeout", "timeout_source", "component", "timeout", c.messageTimeout, "request_id", req.RequestID)
		return c.messageTimeout
	}

	c.logger.Debug("Resolved request timeout", "timeout_source", "default", "timeout", fallback, "request_id", req.RequestID)
	return fallback
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// publishResponse publishes an agent response to JetStream via the
// agent.response output port. Earlier versions iterated every output port
// and re-published the same payload — an unintentional fanout that
// misbehaves once the component declares a second output (agent.stream).
// Responses belong on agent.response; streaming deltas belong on
// agent.stream; each output carries its own payload type.
func (c *Component) publishResponse(ctx context.Context, resp agentic.AgentResponse) error {
	respMsg := message.NewBaseMessage(resp.Schema(), &resp, "agentic-model")
	data, err := json.Marshal(respMsg)
	if err != nil {
		return errs.WrapInvalid(err, "Component", "publishResponse", "marshal response")
	}

	subject := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return errs.WrapTransient(err, "Component", "publishResponse", fmt.Sprintf("publish to %s", subject))
	}

	return nil
}

// outputPortDefs returns the configured output port definitions slice, or
// nil when Ports is unset. Lets ResolveSubject fall back gracefully to
// portName + "." + suffix for test configs without port wiring.
func (c *Component) outputPortDefs() []component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	return c.config.Ports.Outputs
}

// publishErrorResponse publishes an error response with zero token usage.
// Used for errors where no model call occurred (e.g., endpoint not found).
func (c *Component) publishErrorResponse(ctx context.Context, requestID string, errMsg string) {
	c.publishErrorResponseWithTokens(ctx, requestID, errMsg, agentic.TokenUsage{})
}

// publishErrorResponseWithTokens publishes an error response preserving partial token usage.
func (c *Component) publishErrorResponseWithTokens(ctx context.Context, requestID string, errMsg string, tokens agentic.TokenUsage) {
	resp := agentic.AgentResponse{
		RequestID:  requestID,
		Status:     "error",
		Error:      errMsg,
		TokenUsage: tokens,
	}

	if err := c.publishResponse(ctx, resp); err != nil {
		c.logger.Error("Failed to publish error response", "error", err)
	}
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "agentic-model",
		Type:        "processor",
		Description: "OpenAI-compatible agentic model processor with tool calling support",
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
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			},
		}
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
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return agenticModelSchema
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
