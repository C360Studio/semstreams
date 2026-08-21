// Package agenticmodel provides an OpenAI-compatible agentic model processor component
// that routes agent requests to configured LLM endpoints with retry logic and tool calling support.
package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
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
	// healthPolicy gates the fallback chain in getClientForRequest:
	// endpoints with an Open circuit breaker are skipped during
	// resolution. Defaults to an in-process RollingWindowBreaker;
	// callers can override via WithHealthPolicy for shared-state or
	// custom policies (e.g., NATS-KV-backed for cross-process state).
	healthPolicy model.HealthPolicy
	decoder      *message.Decoder
	natsClient   *natsclient.Client
	logger       *slog.Logger
	inputPorts   []component.Port
	outputPorts  []component.Port

	// Dynamic client cache — clients are created on-demand from registry endpoints
	clientCache map[string]*Client // cache key -> client
	clientMu    sync.Mutex

	// Parsed timeout for message processing
	messageTimeout time.Duration

	// Lifecycle management
	running            bool
	startTime          time.Time
	mu                 sync.RWMutex
	lifecycleMu        sync.Mutex
	lifecycleUsed      bool
	terminal           bool
	stopping           bool
	cleanupPending     bool
	startDone          chan struct{}
	cancel             context.CancelFunc
	consumers          []streamConsumerBinding
	waitForStreamInput func(context.Context, string) error
	consumeStream      func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitConsumerClosed func(context.Context, <-chan struct{}) error
	closeModelClient   func(*Client) error

	// Metrics
	requestsProcessed int64
	errors            int64
	lastActivity      time.Time
	metrics           *modelMetrics
}

// Option configures optional Component behavior at construction time.
// Pass to NewComponentWithOptions for non-default wiring.
type Option func(*Component)

// WithHealthPolicy injects a custom HealthPolicy. The default is an
// in-process RollingWindowBreaker tuned for typical LLM workloads
// (window=20, min=5, threshold=0.5, cooldown=30s). Override to share
// circuit-breaker state across processes (e.g., NATS-KV-backed) or to
// disable circuit breaking with model.NewAlwaysHealthyPolicy().
func WithHealthPolicy(p model.HealthPolicy) Option {
	return func(c *Component) {
		if p != nil {
			c.healthPolicy = p
		}
	}
}

// HealthPolicy returns the active health policy. Useful for callers
// that want to consult endpoint state from outside agentic-model
// (e.g., a sibling dispatcher consulting circuit-breaker state before
// routing) without going through the registry interface.
func (c *Component) HealthPolicy() model.HealthPolicy { return c.healthPolicy }

// consumerInfo tracks JetStream consumer details for cleanup
type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// NewComponent creates a new agentic-model processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
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

	// Require model registry
	if deps.ModelRegistry == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Component", "NewComponent", "model registry is required")
	}

	// Parse timeout for message processing
	messageTimeout := 110 * time.Second // default — kept 10s below consumer AckWait 120s so context.Done propagates before NATS redelivery (see resolveTimeout precedence and feedback_class_of_bugs_to_invariant.md)
	if config.Timeout != "" {
		var err error
		messageTimeout, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "parse timeout")
		}
	}

	inputPorts, outputPorts, err := resolveConfiguredPorts(config)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve ports")
	}
	return newComponent(config, deps, messageTimeout, inputPorts, outputPorts), nil
}

// NewComponentWithOptions is the option-aware constructor. The factory
// path (NewComponent) calls this with no options, yielding the default
// in-process RollingWindowBreaker for circuit breaking. Tests and
// callers wanting a custom HealthPolicy use this directly.
func NewComponentWithOptions(rawConfig json.RawMessage, deps component.Dependencies, opts ...Option) (component.Discoverable, error) {
	defaults := DefaultConfig()
	config := DefaultConfig()
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponentWithOptions", "unmarshal config")
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	mergedPorts, err := component.MergePortConfig(*defaults.Ports, *config.Ports)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponentWithOptions", "merge port overrides")
	}
	config.Ports = &mergedPorts
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponentWithOptions", "validate config")
	}
	if deps.ModelRegistry == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Component", "NewComponentWithOptions", "model registry is required")
	}

	messageTimeout := 110 * time.Second
	if config.Timeout != "" {
		var err error
		messageTimeout, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponentWithOptions", "parse timeout")
		}
	}

	inputPorts, outputPorts, err := resolveConfiguredPorts(config)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponentWithOptions", "resolve ports")
	}
	c := newComponent(config, deps, messageTimeout, inputPorts, outputPorts)
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// newComponent builds the Component value with default policies wired
// in. Shared between NewComponent (no options) and
// NewComponentWithOptions (caller-supplied options applied after).
func newComponent(config Config, deps component.Dependencies, messageTimeout time.Duration, inputPorts, outputPorts []component.Port) *Component {
	return &Component{
		name:           "agentic-model",
		config:         config,
		modelRegistry:  deps.ModelRegistry,
		healthPolicy:   model.NewRollingWindowBreaker(model.BreakerConfig{}),
		decoder:        message.NewDecoder(deps.PayloadRegistry),
		clientCache:    make(map[string]*Client),
		natsClient:     deps.NATSClient,
		logger:         deps.GetLogger(),
		inputPorts:     inputPorts,
		outputPorts:    outputPorts,
		messageTimeout: messageTimeout,
		metrics:        getMetrics(deps.MetricsRegistry),
	}
}

func resolveConfiguredPorts(config Config) ([]component.Port, []component.Port, error) {
	if config.Ports == nil {
		return nil, nil, errors.New("ports configuration is required")
	}
	inputs := make([]component.Port, len(config.Ports.Inputs))
	for index, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, nil, err
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, nil, err
		}
		if facts.Kind() != component.PortKindJetStream || len(facts.NATSSubjects()) != 1 {
			return nil, nil, fmt.Errorf("input port %q must declare exactly one JetStream subject", port.Name)
		}
		inputs[index] = port
	}
	outputs := make([]component.Port, len(config.Ports.Outputs))
	for index, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, nil, err
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, nil, err
		}
		if (facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream) || len(facts.NATSSubjects()) != 1 {
			return nil, nil, fmt.Errorf("output port %q must declare exactly one NATS or JetStream subject", port.Name)
		}
		outputs[index] = port
	}
	return inputs, outputs, nil
}

// Initialize prepares the component (no-op for this component)
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing agent requests
func (c *Component) Start(ctx context.Context) (startErr error) {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.ErrAlreadyStarted
	}

	if c.natsClient == nil {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	c.lifecycleUsed = true
	c.cleanupPending = true
	c.cancel = cancel
	c.startDone = startDone
	c.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanup)
			startErr = errors.Join(startErr, rollbackErr)
			c.lifecycleMu.Lock()
			if rollbackErr == nil {
				c.cleanupPending = false
				c.terminal = true
				c.clearLifecycleHandles()
			}
			close(startDone)
			c.startDone = nil
			c.lifecycleMu.Unlock()
			return
		}
		c.lifecycleMu.Lock()
		c.cleanupPending = false
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}()

	// Set up consumers for input ports
	for _, port := range c.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "Component", "Start", "project input port facts")
		}
		subject := facts.NATSSubjects()[0]
		if err := c.setupConsumer(runCtx, port); err != nil {
			return errs.Wrap(err, "Component", "Start", fmt.Sprintf("setup consumer for %s", subject))
		}
	}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()
	committed = true

	return nil
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok {
		return fmt.Errorf("input port %q does not declare JetStream facts", port.Name)
	}
	subject := facts.NATSSubjects()[0]
	streamName := stream.Name()

	// Wait for stream to be available
	waitForStream := c.waitForStream
	if c.waitForStreamInput != nil {
		waitForStream = c.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name (with optional suffix for uniqueness in tests)
	consumerName := fmt.Sprintf("agentic-model-%s", sanitizeSubject(subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new requests, don't replay old ones
	consumerCfg, consumerErr := agenticModelConsumerPolicy(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "agentic-model", "setupConsumer", "resolve consumer config")
	}

	// Per-component defaults for tunables that were previously hardcoded.
	// Operators tune via JetStreamPort.AckWait / .HeartbeatInterval; the
	// fallbacks here are the historical hardcoded values, preserved so a
	// zero-config deployment behaves identically to pre-port-config builds.
	// AckWait must comfortably exceed the longest legitimate per-task
	// LLM wallclock budget — see docs/operations/14-timeout-chain.md and
	// resolveTimeout's 110s fallback (10s strictly below the 120s default
	// here).
	const (
		defaultModelAckWait           = 2 * time.Minute
		defaultModelHeartbeatInterval = 90 * time.Second
	)
	ackWait := consumerCfg.AckWait
	if ackWait == 0 {
		ackWait = defaultModelAckWait
	}
	heartbeatInterval := consumerCfg.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = defaultModelHeartbeatInterval
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		// Honor consumerCfg.MaxDeliver (port-level tunable, default 3).
		// The previous hardcoded MaxDeliver: 2 was reading consumerCfg
		// three lines above and discarding the value — operators trying
		// to set MaxDeliver=1 (Posture B / fail-loud-once for paid LLMs
		// per docs/operations/14-timeout-chain.md) had no path to the
		// setting until now.
		MaxDeliver:     consumerCfg.MaxDeliver,
		AckWait:        ackWait,
		MaxAckPending:  1,
		AutoCreate:     false,
		MessageTimeout: 30 * time.Minute,
	}

	consume := c.natsClient.ConsumeStreamWithConfig
	if c.consumeStream != nil {
		consume = c.consumeStream
	}
	handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
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

	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	c.logger.Info("Subscribed to agent requests (JetStream)",
		"subject", subject,
		"stream", streamName,
		"consumer", consumerName)
	return nil
}

func agenticModelConsumerPolicy(port component.Port) (component.ConsumerConfig, error) {
	consumerConfig, err := component.GetConsumerConfig(port)
	if err != nil {
		return component.ConsumerConfig{}, err
	}
	if consumerConfig.MaxAckPending != 0 {
		return component.ConsumerConfig{}, errs.WrapInvalid(
			fmt.Errorf("port %q max_ack_pending is component-owned at 1", port.Name),
			"agentic-model", "consumerPolicy", "component-owned consumer policy")
	}
	return consumerConfig, nil
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
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed, c.terminal = true, true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.terminal {
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.startDone != nil {
			done := c.startDone
			c.lifecycleMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "Component", "Stop", "concurrent Stop")
		}
		retryable := c.cleanupPending
		c.stopping = true
		c.lifecycleMu.Unlock()
		stopErr := c.cleanup(ctx)
		c.lifecycleMu.Lock()
		c.stopping = false
		if retryable && stopErr != nil {
			c.lifecycleMu.Unlock()
			return stopErr
		}
		c.cleanupPending, c.terminal = false, true
		c.clearLifecycleHandles()
		c.lifecycleMu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanup(ctx context.Context) error {
	var cleanupErr error
	for i := range c.consumers {
		binding := &c.consumers[i]
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
		closed := binding.handle.Closed()
		if c.waitConsumerClosed != nil {
			cleanupErr = errors.Join(cleanupErr, c.waitConsumerClosed(ctx, closed))
		} else {
			select {
			case <-closed:
			case <-ctx.Done():
				cleanupErr = errors.Join(cleanupErr, ctx.Err())
			}
		}
	}
	if c.cancel != nil {
		c.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		cleanupErr = errors.Join(cleanupErr, ctxErr)
	}

	// Close cached clients outside clientMu. Native client shutdown is child
	// lifecycle work and may block or re-enter component state.
	c.clientMu.Lock()
	clients := make(map[string]*Client, len(c.clientCache))
	for key, client := range c.clientCache {
		clients[key] = client
	}
	c.clientMu.Unlock()
	for key, client := range clients {
		closeClient := client.Close
		if c.closeModelClient != nil {
			closeClient = func() error { return c.closeModelClient(client) }
		}
		if err := closeClient(); err != nil {
			c.logger.Warn("Failed to close client", "key", key, "error", err)
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		c.clientMu.Lock()
		if c.clientCache[key] == client {
			delete(c.clientCache, key)
		}
		c.clientMu.Unlock()
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return cleanupErr
}

func (c *Component) clearLifecycleHandles() { c.consumers = nil; c.cancel = nil }

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

	c.logger.Debug("Processing agent request",
		slog.String("request_id", req.RequestID),
		slog.String("model", req.Model),
		slog.String("role", req.Role))

	client, endpoint, capability, endpointName, err := c.getClientForRequest(req)
	if err != nil {
		c.logger.Error("Failed to resolve endpoint", "error", err, "model", req.Model)
		c.publishErrorResponse(ctx, req.RequestID, err.Error())
		c.incrementErrors()
		return
	}

	// Strip tools the served endpoint can't handle. req.Model can be a
	// capability (coordinator/developer/reviewer); getClientForRequest already
	// resolved it (capability chain + health failover), so deciding on the
	// returned endpoint honors the resolved-endpoint contract (doc.go Endpoint
	// Resolution) and matches the endpoint that actually serves the request —
	// where a pre-resolution GetEndpoint(req.Model) would miss on a capability
	// and skip the strip, and a ResolveEndpointName probe could diverge from a
	// health-failed-over served endpoint.
	stripUnsupportedTools(&req, endpoint, c.logger)

	startTime := time.Now()
	if c.metrics != nil {
		c.metrics.recordRequestStart(req.Model)
	}

	resp, err := c.executeRequest(ctx, client, req, endpoint, capability)
	duration := time.Since(startTime).Seconds()
	latency := time.Since(startTime)

	if err != nil || resp.Status == "error" {
		c.recordHealthResult(ctx, endpointName, false, err, resp.Error, latency)
		c.handleModelError(ctx, req, resp, err, duration)
		return
	}

	c.recordHealthResult(ctx, endpointName, true, nil, "", latency)
	c.handleModelSuccess(ctx, req, resp, duration)
}

// stripUnsupportedTools clears req.Tools when the served endpoint cannot handle
// tool calls, honoring the resolved-endpoint contract (doc.go Endpoint
// Resolution). endpoint is the endpoint getClientForRequest resolved and will
// serve, so a capability request whose resolved endpoint lacks tool support is
// stripped correctly. Returns true when tools were stripped.
func stripUnsupportedTools(req *agentic.AgentRequest, endpoint *model.EndpointConfig, logger *slog.Logger) bool {
	if len(req.Tools) == 0 || endpoint == nil || endpoint.SupportsTools {
		return false
	}
	if logger != nil {
		logger.Warn("Stripping tools from request: endpoint does not support tool calling",
			"model", req.Model,
			"tool_count", len(req.Tools))
	}
	req.Tools = nil
	return true
}

// recordHealthResult forwards a request outcome to the health policy.
// Centralizes the success/error → model.Result translation so the
// breaker accounting and metrics agree on classification.
func (c *Component) recordHealthResult(ctx context.Context, endpointName string, success bool, err error, respErr string, latency time.Duration) {
	if c.healthPolicy == nil || endpointName == "" {
		return
	}
	kind := model.ErrorKindNone
	if !success {
		errMsg := respErr
		if err != nil {
			errMsg = err.Error()
		}
		kind = mapErrorKind(ctx, errMsg)
	}
	c.healthPolicy.RecordResult(endpointName, model.Result{
		Success: success,
		Kind:    kind,
		Latency: latency,
	})
	if c.metrics != nil {
		c.metrics.recordEndpointHealthState(endpointName, c.healthPolicy.EndpointStatus(endpointName).String())
	}
}

// mapErrorKind classifies an error string for HealthPolicy accounting.
// Mirrors classifyError (used by metrics) so circuit-breaker math and
// Prometheus error_type labels stay aligned.
func mapErrorKind(ctx context.Context, errorMsg string) model.ErrorKind {
	if ctx != nil && ctx.Err() != nil {
		return model.ErrorKindTimeout
	}
	switch {
	case strings.Contains(errorMsg, "rate limit"), strings.Contains(errorMsg, "429"):
		return model.ErrorKindRateLimit
	case strings.Contains(errorMsg, "connection"), strings.Contains(errorMsg, "dial"):
		return model.ErrorKindNetwork
	case strings.Contains(errorMsg, "500"), strings.Contains(errorMsg, "502"),
		strings.Contains(errorMsg, "503"), strings.Contains(errorMsg, "504"):
		return model.ErrorKindServerError
	default:
		return model.ErrorKindUnknown
	}
}

// parseAgentRequest extracts an AgentRequest from raw message data
func (c *Component) parseAgentRequest(data []byte) (agentic.AgentRequest, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
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

// resolveEndpoint walks the chain → direct → default → last-ditch
// resolution path, returning the first endpoint that maps to a real
// EndpointConfig under the active health policy. Splitting this out
// of getClientForRequest keeps the caller below the revive
// function-length limit and makes the resolution logic separately
// testable.
func (c *Component) resolveEndpoint(req agentic.AgentRequest) (*model.EndpointConfig, string, string) {
	chain := c.modelRegistry.GetFallbackChain(req.Model)
	capability := ""
	if len(chain) > 0 {
		capability = req.Model
	}

	// 1. Capability chain — first healthy endpoint wins.
	for _, name := range chain {
		candidate := c.modelRegistry.GetEndpoint(name) // modelresolveaudit:allow call-path resolution (resolveEndpoint chain names)
		if candidate == nil {
			continue
		}
		if !c.healthPolicy.IsHealthy(name) {
			c.logger.Info("skipping unhealthy endpoint in fallback chain",
				slog.String("endpoint", name),
				slog.String("status", c.healthPolicy.EndpointStatus(name).String()))
			continue
		}
		return candidate, name, capability
	}

	// 2. Direct endpoint name — health-gated for the same reason.
	if candidate := c.modelRegistry.GetEndpoint(req.Model); candidate != nil { // modelresolveaudit:allow call-path resolution (resolveEndpoint direct probe; capability handled by chain above)
		if c.healthPolicy.IsHealthy(req.Model) {
			return candidate, req.Model, capability
		}
		c.logger.Info("requested endpoint unhealthy; falling through to default",
			slog.String("endpoint", req.Model),
			slog.String("status", c.healthPolicy.EndpointStatus(req.Model).String()))
	}

	// 3. Default — NOT health-gated. Last guaranteed responder; we'd
	// rather attempt and let the breaker re-record than queue dead air.
	if defaultName := c.modelRegistry.GetDefault(); defaultName != "" {
		if ep := c.modelRegistry.GetEndpoint(defaultName); ep != nil { // modelresolveaudit:allow call-path resolution (resolveEndpoint default is a real endpoint)
			return ep, defaultName, capability
		}
	}

	// 4. Last-ditch: chain unhealthy AND no default. Retry chain
	// ignoring health so the next request result reaches the breaker.
	for _, name := range chain {
		if candidate := c.modelRegistry.GetEndpoint(name); candidate != nil { // modelresolveaudit:allow call-path resolution (resolveEndpoint chain retry)
			c.logger.Warn("all chain endpoints unhealthy; attempting anyway",
				slog.String("endpoint", name))
			return candidate, name, capability
		}
	}

	return nil, "", capability
}

func (c *Component) getClientForRequest(req agentic.AgentRequest) (*Client, *model.EndpointConfig, string, string, error) {
	ep, endpointName, capability := c.resolveEndpoint(req)
	if ep == nil {
		return nil, nil, "", "", errs.WrapInvalid(
			fmt.Errorf("no endpoint found for model %q in registry", req.Model),
			"Component", "getClientForRequest", "resolve endpoint",
		)
	}

	// Cache key: URL + model name
	cacheKey := ep.URL + "|" + ep.Model

	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if client, ok := c.clientCache[cacheKey]; ok {
		return client, ep, capability, endpointName, nil
	}

	client, err := NewClient(ep)
	if err != nil {
		return nil, nil, "", "", errs.Wrap(err, "Component", "getClientForRequest", fmt.Sprintf("create client for model %q", req.Model))
	}

	// Wire provider adapter for request/response normalization.
	client.SetAdapter(AdapterFor(ep.Provider))
	// ADR-051: when the endpoint opts into the Responses path,
	// also wire the Responses-side adapter so capture/echo of
	// ReasoningRecord{CarrierKind:StandaloneItem} runs at the seam.
	if ep.WireBackend == "responses" {
		client.SetResponsesAdapter(ResponsesAdapterFor(ep.Provider))
	}

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
	return client, ep, capability, endpointName, nil
}

// makeChunkHandler returns a ChunkHandler that publishes chunks to core NATS.
// Chunks are fire-and-forget (core NATS Publish, not JetStream) since they are
// ephemeral — dropped if no subscriber is listening.
func (c *Component) makeChunkHandler() ChunkHandler {
	return func(ctx context.Context, chunk StreamChunk) {
		data, err := json.Marshal(chunk)
		if err != nil {
			c.logger.Warn("Failed to marshal stream chunk", "error", err)
			return
		}

		subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.stream", chunk.RequestID)
		if err != nil {
			c.logger.Warn("Failed to resolve stream chunk subject", "error", err)
			return
		}
		if err := c.natsClient.Publish(ctx, subject, data); err != nil {
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
//  5. hardcoded 110s safety default
//
// The 110s fallback sits 10s below the agentic-model consumer AckWait
// (120s, component.go:269). When every override is unset, we still want
// the LLM call to cancel cleanly — context.Done propagating to the SDK's
// in-flight HTTP request — before NATS would otherwise redeliver. The
// dedup short-circuit catches the redelivery if it happens, but a
// strictly-less-than budget means redelivery doesn't fire on a healthy
// long-tail call. See docs/operations/14-timeout-chain.md.
//
// Invalid duration strings at levels 1-3 log a warning and fall through to the
// next level, so a malformed per-message override never blocks a task. The
// selected source is logged at debug level as timeout_source.
func (c *Component) resolveTimeout(req agentic.AgentRequest, endpoint *model.EndpointConfig, capability string) time.Duration {
	const fallback = 110 * time.Second

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

	subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)
	if err != nil {
		return errs.WrapInvalid(err, "Component", "publishResponse", "resolve response subject")
	}
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
	return append([]component.Port(nil), c.inputPorts...)
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputPorts...)
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
