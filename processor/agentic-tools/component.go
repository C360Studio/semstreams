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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// agenticToolsSchema defines the configuration schema
var agenticToolsSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the agentic-tools processor
type Component struct {
	name    string
	config  Config
	inputs  []component.Port
	outputs []component.Port
	// registry is this component's private executor registry —
	// populated via RegisterToolExecutor at construction time and
	// dispatched local-first.
	registry *ExecutorRegistry
	// shared is the process-wide tool registry plumbed through
	// component.Dependencies.ToolRegistry. Built and populated by
	// main.go via executors.RegisterBuiltins. May be nil for tests
	// that exercise the component in isolation.
	shared        component.ToolRegistryReader
	decoder       *message.Decoder
	natsClient    *natsclient.Client
	logger        *slog.Logger
	platform      component.PlatformMeta
	outcomes      completedOutcomeStore
	publishStream func(context.Context, string, []byte, string) error

	// Lifecycle management
	running        bool
	startTime      time.Time
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc

	// Metrics
	requestsProcessed int64
	errors            int64
	lastActivity      time.Time
	metrics           *toolsMetrics

	// Approval filter (nil when approval_required is empty)
	approvalFilter *ApprovalFilter

	// Subscriptions (for cleanup)
	toolListSub requestSubscription

	// Track consumers for cleanup
	consumers           []streamConsumerBinding
	waitForStreamInput  func(context.Context, string) error
	acquireOutcomeStore func(context.Context) (completedOutcomeStore, error)
	subscribeRequests   func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (requestSubscription, error)
	consumeStream       func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitConsumerClosed  func(context.Context, <-chan struct{}) error
}

type requestSubscription interface{ Drain(context.Context) error }

// consumerInfo tracks JetStream consumer details for cleanup
type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

type consumerSetup struct {
	port           component.Port
	streamName     string
	subject        string
	consumerConfig component.ConsumerConfig
}

// DeclarePorts is the component.PortDeclarer for agentic-tools: the ports
// NewComponent will report for rawConfig, computed without dependencies.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	_, inputs, outputs, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(inputs, outputs), nil
}

// resolveConfig parses rawConfig over the defaults, merges the port
// overrides, validates, and resolves the effective ports. It is the one
// derivation DeclarePorts and NewComponent share.
func resolveConfig(rawConfig json.RawMessage) (Config, []component.Port, []component.Port, error) {
	defaults := DefaultConfig()
	config := DefaultConfig()
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	mergedPorts, err := component.MergePortConfig(*defaults.Ports, *config.Ports)
	if err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "Component", "NewComponent", "merge port overrides")
	}
	config.Ports = &mergedPorts

	// Validate configuration
	if err := config.Validate(); err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve input port")
		}
		inputs = append(inputs, port)
	}
	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve output port")
		}
		outputs = append(outputs, port)
	}
	return config, inputs, outputs, nil
}

// NewComponent creates a new agentic-tools processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	config, inputs, outputs, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	comp := &Component{
		name:       "agentic-tools",
		config:     config,
		inputs:     inputs,
		outputs:    outputs,
		registry:   NewExecutorRegistry(),
		shared:     deps.ToolRegistry,
		decoder:    message.NewDecoder(deps.PayloadRegistry),
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
		metrics:    getMetrics(deps.MetricsRegistry),
	}
	if deps.NATSClient != nil {
		comp.publishStream = deps.NATSClient.PublishToStreamWithMsgID
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
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "cleanup authority already active")
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

	// Resolve and validate every required startup fact before allocating the
	// discovery subscription or any local JetStream consumer.
	toolListSubject, consumers, err := c.startupPlan()
	if err != nil {
		return err
	}
	if c.acquireOutcomeStore != nil {
		c.outcomes, err = c.acquireOutcomeStore(runCtx)
	} else {
		var bucket jetstream.KeyValue
		bucket, err = graph.EnsureCatalogBucket(runCtx, c.natsClient, graph.BucketToolCallOutcomes)
		c.outcomes = jetStreamCompletedOutcomeStore{bucket: bucket}
	}
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "acquire tool-call outcome ledger")
	}

	// tool.list request/reply discovery is bound exclusively by its resolved
	// typed input port. There is no subject fallback or legacy alias.
	subscribe := func(ctx context.Context, subject string, handler func(context.Context, []byte) ([]byte, error)) (requestSubscription, error) {
		return c.natsClient.SubscribeForRequests(ctx, subject, handler)
	}
	if c.subscribeRequests != nil {
		subscribe = c.subscribeRequests
	}
	sub, err := subscribe(runCtx, toolListSubject, c.handleToolListRequest)
	if err != nil {
		return errs.WrapTransient(err, "Component", "Start", "subscribe to tool.list discovery")
	}
	c.lifecycleMu.Lock()
	c.toolListSub = sub
	c.lifecycleMu.Unlock()
	c.logger.Info("Subscribed to tool.list", "subject", toolListSubject)

	for _, consumer := range consumers {
		if err := c.setupConsumer(runCtx, consumer); err != nil {
			return errs.Wrap(err, "Component", "Start", fmt.Sprintf("setup consumer for %s", consumer.port.Name))
		}
	}

	// Tool registration happens in main.go via executors.RegisterAll before
	// this component starts, so the global registry is already populated
	// by the time agent loops dispatch. Keeping registration out of the
	// component lets agentic-tools stay a pure tool-execution endpoint
	// (it reads from the global registry, never writes).

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()
	committed = true

	return nil
}

func (c *Component) startupPlan() (string, []consumerSetup, error) {
	var toolListSubject string
	consumers := make([]consumerSetup, 0, len(c.inputs))
	for _, port := range c.inputs {
		facts, err := port.Facts()
		if err != nil {
			return "", nil, errs.WrapInvalid(err, "Component", "Start", fmt.Sprintf("resolve facts for port %s", port.Name))
		}
		if port.Name == "tool.list" {
			if port.Direction != component.DirectionInput ||
				facts.Kind() != component.PortKindNATSRequest ||
				facts.InteractionPattern() != component.PatternRequest {
				return "", nil, errs.WrapInvalid(
					fmt.Errorf("port %q expected input kind %q with request interaction, observed direction %q kind %q interaction %q",
						port.Name, component.PortKindNATSRequest, port.Direction, facts.Kind(), facts.InteractionPattern()),
					"Component", "Start", "resolve tool.list request port",
				)
			}
			subjects := facts.NATSSubjects()
			if len(subjects) != 1 || subjects[0] == "" {
				return "", nil, errs.WrapInvalid(
					fmt.Errorf("tool.list must declare exactly one NATS request subject"),
					"Component", "Start", "resolve tool.list request subject",
				)
			}
			toolListSubject = subjects[0]
			continue
		}
		if facts.Kind() != component.PortKindJetStream {
			continue
		}
		stream, ok := facts.Stream()
		if !ok || len(stream.Subjects()) != 1 {
			return "", nil, errs.WrapInvalid(
				fmt.Errorf("port %s must declare one JetStream subject", port.Name),
				"Component", "Start", "validate consumer facts",
			)
		}
		consumerConfig, err := agenticToolsConsumerPolicy(port)
		if err != nil {
			return "", nil, errs.WrapInvalid(err, "Component", "Start", fmt.Sprintf("validate consumer config for %s", port.Name))
		}
		consumers = append(consumers, consumerSetup{
			port:           port,
			streamName:     stream.Name(),
			subject:        stream.Subjects()[0],
			consumerConfig: consumerConfig,
		})
	}
	if toolListSubject == "" {
		return "", nil, errs.WrapInvalid(
			fmt.Errorf("tool.list input port is required"),
			"Component", "Start", "resolve tool.list request port",
		)
	}
	return toolListSubject, consumers, nil
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, setup consumerSetup) error {
	streamName := setup.streamName
	subject := setup.subject

	// Wait for stream to be available
	waitForStream := c.waitForStream
	if c.waitForStreamInput != nil {
		waitForStream = c.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name (with optional suffix for uniqueness in tests)
	consumerName := fmt.Sprintf("agentic-tools-%s", sanitizeSubject(subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new tool calls, don't replay old ones
	consumerCfg := setup.consumerConfig

	// Per-component defaults preserved as fallbacks so zero-config
	// deployments behave identically to pre-port-config builds. The 5m
	// AckWait tolerates long-running tools (sandbox bash, deep_research);
	// HeartbeatInterval new in this PR — without it, a tool taking longer
	// than AckWait would be redelivered even on a healthy execution.
	const (
		defaultToolsAckWait           = 5 * time.Minute
		defaultToolsHeartbeatInterval = 2 * time.Minute
	)
	ackWait := consumerCfg.AckWait
	if ackWait == 0 {
		ackWait = defaultToolsAckWait
	}
	heartbeatInterval := consumerCfg.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = defaultToolsHeartbeatInterval
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		// Honor consumerCfg.MaxDeliver — was hardcoded to 3 even when
		// operators set max_deliver via the port config (already-read
		// consumerCfg.MaxDeliver was discarded).
		MaxDeliver:     consumerCfg.MaxDeliver,
		AckWait:        ackWait,
		MaxAckPending:  3,
		BackOff:        []time.Duration{15 * time.Second, 60 * time.Second},
		AutoCreate:     false,
		MessageTimeout: 10 * time.Minute,
	}

	// Wrap handler in ConsumeWithHeartbeat so long-running tools fire
	// msg.InProgress() at heartbeatInterval and reset the AckWait clock.
	// Without heartbeat, any tool exceeding AckWait gets redelivered
	// while the original handler is still working — duplicate work +
	// potential duplicate publishes. ConsumeWithHeartbeat owns ack/nak;
	// The handler's error is the delivery disposition contract: nil ACKs,
	// transient failures delayed-NAK, and PermanentDeliveryError Terms.
	consume := c.natsClient.ConsumeStreamWithConfig
	if c.consumeStream != nil {
		consume = c.consumeStream
	}
	handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: setup.port.Name, ComponentOwned: true}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,
			func(workCtx context.Context) error {
				return c.handleToolCall(workCtx, msg.Data())
			},
		); hbErr != nil {
			c.recordHandlerError(msgCtx, hbErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("consumer setup for stream %s", streamName))
	}

	// Track consumer for cleanup in Stop()
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	c.logger.Info("Subscribed to tool calls (JetStream)",
		"subject", subject,
		"stream", streamName,
		"consumer", consumerName)
	return nil
}

func agenticToolsConsumerPolicy(port component.Port) (component.ConsumerConfig, error) {
	consumerConfig, err := component.GetConsumerConfig(port)
	if err != nil {
		return component.ConsumerConfig{}, err
	}
	if consumerConfig.MaxAckPending != 0 {
		return component.ConsumerConfig{}, errs.WrapInvalid(
			fmt.Errorf("port %q max_ack_pending is component-owned at 3", port.Name),
			"agentic-tools", "consumerPolicy", "component-owned consumer policy")
	}
	return consumerConfig, nil
}

func (c *Component) recordHandlerError(ctx context.Context, err error) {
	switch {
	case errors.Is(err, natsclient.ErrHeartbeatFailed):
		if c.metrics != nil {
			c.metrics.recordAmbiguous(ambiguousCauseHeartbeat)
		}
		c.logger.Error("Tool delivery heartbeat failed", "error", err, "ambiguous_effect", true)
	case ctx.Err() != nil:
		if c.metrics != nil {
			c.metrics.recordAmbiguous(ambiguousCauseShutdown)
		}
		c.logger.Error("Tool delivery interrupted by shutdown", "error", err, "ambiguous_effect", true)
	default:
		c.logger.Error("Tool handler error", "error", err)
	}
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
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanup(ctx context.Context) error {
	c.lifecycleMu.Lock()
	toolListSub := c.toolListSub
	c.lifecycleMu.Unlock()
	var cleanupErr error
	// Unsubscribe from tool.list request handler
	if toolListSub != nil {
		err := toolListSub.Drain(ctx)
		if err != nil {
			c.logger.Warn("tool list subscription unsubscribe error", slog.Any("error", err))
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			c.lifecycleMu.Lock()
			if c.toolListSub == toolListSub {
				c.toolListSub = nil
			}
			c.lifecycleMu.Unlock()
		}
	}

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
	return cleanupErr
}

func (c *Component) clearLifecycleHandles() {
	c.toolListSub = nil
	c.consumers = nil
	c.cancel = nil
}

// handleToolCall processes a tool call request
func (c *Component) handleToolCall(ctx context.Context, data []byte) error {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	// Parse BaseMessage envelope
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		c.incrementErrors()
		return natsclient.TerminateDelivery(fmt.Errorf("decode tool call: %w", err))
	}

	// Extract ToolCall from payload
	callPtr, ok := baseMsg.Payload().(*agentic.ToolCall)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		c.incrementErrors()
		return natsclient.TerminateDelivery(fmt.Errorf("unexpected tool-call payload type %T", baseMsg.Payload()))
	}
	call := *callPtr
	if err := call.Validate(); err != nil {
		c.logger.Error("Invalid tool call", "error", err)
		c.incrementErrors()
		return natsclient.TerminateDelivery(fmt.Errorf("validate tool call: %w", err))
	}

	c.logger.Debug("Processing tool call",
		slog.String("tool", call.Name),
		slog.String("call_id", call.ID))

	if c.outcomes == nil {
		return fmt.Errorf("tool-call outcome ledger is not initialized")
	}
	if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {
		return err
	} else if found {
		return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)
	}

	// Check admission: global allowlist + per-loop advertised set (gh#551)
	if rejection := c.admitToolCall(call); rejection != nil {
		c.logger.Warn("Tool call rejected",
			"tool", call.Name,
			"reason", rejection.filterReason)

		if c.metrics != nil {
			c.metrics.recordToolFiltered(call.Name, rejection.filterReason)
		}

		result := agentic.ToolResult{
			CallID:    call.ID,
			Error:     rejection.message,
			ErrorKind: rejection.kind,
			LoopID:    call.LoopID,
			TraceID:   call.TraceID,
		}
		err := c.persistAndPublishOutcome(ctx, call, result, outcomePathRejection, false)
		c.incrementErrors()
		return err
	}

	// Check approval filter
	if c.approvalFilter != nil {
		filterResult := c.approvalFilter.FilterToolCalls(call.LoopID, []agentic.ToolCall{call})
		if len(filterResult.Rejected) > 0 {
			c.logger.Info("Tool requires approval", "tool", call.Name)

			if c.metrics != nil {
				c.metrics.recordToolFiltered(call.Name, "approval_required")
			}

			result := agentic.ToolResult{
				CallID:    call.ID,
				Name:      call.Name,
				Error:     filterResult.Rejected[0].Reason,
				ErrorKind: agentic.ToolErrorPermission,
				LoopID:    call.LoopID,
				TraceID:   call.TraceID,
			}
			// Approval-required is a pause signal, not a terminal outcome. The
			// loop deliberately re-dispatches the SAME CallID with ApprovedBy set;
			// persisting this gate as COMPLETED would collide with that request.
			// It also gets a distinct message ID so stream dedup cannot suppress
			// the later terminal result.
			err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ID))
			if err == nil && c.metrics != nil {
				c.metrics.recordOutcome(outcomePathRejection)
			}
			return err
		}
	}

	// Execute tool with timeout
	startTime := time.Now()
	result, err := c.executeWithPanicRecovery(ctx, call)
	duration := time.Since(startTime).Seconds()

	c.classifyToolOutcome(ctx, call, &result, err, duration)

	// Propagate trace correlation fields from call to result
	result.LoopID = call.LoopID
	result.TraceID = call.TraceID
	result.CallID = call.ID
	if result.Name == "" {
		result.Name = call.Name
	}

	// Persist the immutable completed outcome before publishing. Only the
	// result's synchronous PubAck permits the request delivery to ACK.
	if err := c.persistAndPublishOutcome(ctx, call, result, outcomePathNew, true); err != nil {
		c.logger.Error("Failed to publish result", "error", err)
		c.incrementErrors()
		return err
	}

	c.mu.Lock()
	c.requestsProcessed++
	c.mu.Unlock()
	return nil
}

func (c *Component) executeWithPanicRecovery(ctx context.Context, call agentic.ToolCall) (
	result agentic.ToolResult, err error,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.logger.Error("Tool executor panicked", "tool", call.Name, "ambiguous_effect", true)
			c.incrementErrors()
			if c.metrics != nil {
				c.metrics.recordAmbiguous(ambiguousCausePanic)
			}
			result = compactPanicResult(call)
			err = nil
		}
	}()
	return c.executeWithTimeout(ctx, call)
}

func (c *Component) loadCompletedOutcome(
	ctx context.Context, call agentic.ToolCall, operation outcomeStoreOperation,
) (completedOutcome, bool, error) {
	data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ID))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return completedOutcome{}, false, nil
	}
	if err != nil {
		if c.metrics != nil {
			c.metrics.recordStoreFailure(operation, storeReasonTransport)
		}
		return completedOutcome{}, false, fmt.Errorf("read tool-call outcome: %w", err)
	}
	outcome, err := decodeCompletedOutcome(data, call)
	if err != nil {
		if c.metrics != nil {
			var collision *outcomeCollisionError
			if errors.As(err, &collision) {
				c.metrics.recordCollision()
			} else {
				c.metrics.recordStoreFailure(operation, storeReasonCorrupt)
			}
		}
		c.logger.Error("Irrecoverable tool-call outcome", "error", err)
		return completedOutcome{}, false, natsclient.TerminateDelivery(err)
	}
	return outcome, true, nil
}

func (c *Component) persistAndPublishOutcome(
	ctx context.Context, call agentic.ToolCall, result agentic.ToolResult, path outcomePath, effectful bool,
) error {
	winner, finalPath, err := c.persistCompletedOutcome(ctx, call, result, path, false, effectful)
	if err != nil {
		return err
	}
	return c.publishCompletedResult(ctx, call, winner.Result, finalPath)
}

func (c *Component) persistCompletedOutcome(
	ctx context.Context, call agentic.ToolCall, result agentic.ToolResult, path outcomePath, compact, effectful bool,
) (completedOutcome, outcomePath, error) {
	record, err := newCompletedOutcome(call, result)
	if err != nil {
		return completedOutcome{}, path, natsclient.TerminateDelivery(err)
	}
	data, err := marshalCompletedOutcome(record)
	if err != nil {
		return completedOutcome{}, path, natsclient.TerminateDelivery(fmt.Errorf("marshal tool outcome: %w", err))
	}
	err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ID), data)
	if err == nil {
		if compact {
			path = outcomePathCompact
		}
		return record, path, nil
	}
	if errors.Is(err, jetstream.ErrKeyExists) {
		winner, found, readErr := c.loadCompletedOutcome(ctx, call, storeOperationReadWinner)
		if readErr != nil {
			return completedOutcome{}, path, readErr
		}
		if !found {
			if c.metrics != nil {
				c.metrics.recordStoreFailure(storeOperationReadWinner, storeReasonTransport)
			}
			return completedOutcome{}, path, fmt.Errorf("read winning tool-call outcome after create collision")
		}
		return winner, outcomePathReplay, nil
	}
	if isObservedOversize(err) {
		if c.metrics != nil {
			c.metrics.recordStoreFailure(storeOperationCreate, storeReasonOversize)
		}
		if compact {
			c.logger.Error("Compact tool outcome exceeded transport bound", "error", err)
			return completedOutcome{}, outcomePathCompact, natsclient.TerminateDelivery(fmt.Errorf("compact tool outcome exceeds bound: %w", err))
		}
		return c.persistCompletedOutcome(ctx, call, compactTooLargeResult(call), outcomePathCompact, true, effectful)
	}
	if c.metrics != nil {
		c.metrics.recordStoreFailure(storeOperationCreate, storeReasonTransport)
	}
	if effectful {
		if c.metrics != nil {
			c.metrics.recordAmbiguous(ambiguousCauseStoreFailure)
		}
		c.logger.Error("Tool outcome persistence failed after execution", "error", err, "ambiguous_effect", true)
	}
	// A failed Create after external execution is intentionally transient. The
	// next delivery cannot know whether an external effect happened; executors
	// use ToolCall.ID for downstream idempotency across this ambiguity window.
	return completedOutcome{}, path, fmt.Errorf("create tool-call outcome: %w", err)
}

func (c *Component) publishCompletedResult(
	ctx context.Context, call agentic.ToolCall, result agentic.ToolResult, path outcomePath,
) error {
	err := c.publishResult(ctx, result)
	if err == nil {
		if c.metrics != nil {
			c.metrics.recordOutcome(path)
		}
		return nil
	}
	if !isObservedOversize(err) {
		return err
	}
	// The full immutable authority stays in KV. A publication-only bound gets
	// exactly one compact transport surrogate using the same call-derived MsgID.
	compact := compactTooLargeResult(call)
	if compactErr := c.publishResult(ctx, compact); compactErr != nil {
		c.logger.Error("Compact tool result publication failed", "error", compactErr)
		return natsclient.TerminateDelivery(fmt.Errorf("publish compact tool result: %w", compactErr))
	}
	if c.metrics != nil {
		c.metrics.recordOutcome(outcomePathCompact)
	}
	return nil
}

// classifyToolOutcome records metrics and updates the result's ErrorKind based
// on the execution outcome. Framework-level timeout takes precedence over any
// executor-set kind (a deeper cause like "network" may be masking a context
// deadline). Mutates result in place.
func (c *Component) classifyToolOutcome(
	ctx context.Context,
	call agentic.ToolCall,
	result *agentic.ToolResult,
	err error,
	duration float64,
) {
	if err != nil {
		c.logger.Error("Failed to execute tool", "tool", call.Name, "error", err)
		isTimeout := errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, context.Canceled) || ctx.Err() != nil
		if isTimeout {
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
		return
	}

	if result.Error != "" {
		// Tool executed but returned an error result.
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
		return
	}

	if c.metrics != nil {
		c.metrics.recordExecutionSuccess(call.Name, duration)
	}
	c.logger.Debug("Tool executed successfully",
		slog.String("tool", call.Name),
		slog.Float64("duration_seconds", duration))
}

// isToolAllowed checks if a tool is in the allowed list.
// Returns true if AllowedTools is empty (allow all) or if tool is in the list.
func (c *Component) isToolAllowed(toolName string) bool {
	if len(c.config.AllowedTools) == 0 {
		return true
	}
	return slices.Contains(c.config.AllowedTools, toolName)
}

// toolAdmissionRejection describes why a tool call was refused admission,
// shaped for the shared rejection path (ToolResult error + metrics label).
type toolAdmissionRejection struct {
	// message is the error text placed on the ToolResult. The per-loop
	// rejection text is deliberately DISTINCT from the global "tool %q is
	// not allowed" so callers (semdev's routing rules, gh#551) can tell
	// "not in this deployment" apart from "not advertised to this loop".
	message string
	// kind classifies the rejection: ToolErrorNotFound for the global
	// allowlist (pre-existing contract), ToolErrorPermission for the
	// per-loop advertised set (the tool exists and is deployed; this loop
	// lacks permission).
	kind agentic.ToolErrorKind
	// filterReason is the metrics label for recordToolFiltered.
	filterReason string
}

// admitToolCall is the single admission seam for BOTH executor entry points
// (handleToolCall off the wire and the direct Execute path). Two layers:
//
//  1. Global component allowlist (config.AllowedTools) — unchanged contract.
//  2. Per-loop advertised tool set (gh#551): when the dispatching loop
//     advertised a tool set (ToolCall.Metadata[MetadataKeyAdvertisedTools],
//     stamped authoritatively by agentic-loop's dispatchToolCall), the call's
//     name must be a member. Key ABSENT → no per-loop check (back-compat:
//     loops without an advertised set stay unrestricted). Key PRESENT but
//     empty/malformed → fail closed with a Warn naming the loop and raw
//     value — a broken security-control value must not degrade to permissive
//     (the IsKnownFilesystemPolicy precedent).
//
// Returns nil when the call is admitted.
func (c *Component) admitToolCall(call agentic.ToolCall) *toolAdmissionRejection {
	if !c.isToolAllowed(call.Name) {
		return &toolAdmissionRejection{
			message:      fmt.Sprintf("tool %q is not allowed", call.Name),
			kind:         agentic.ToolErrorNotFound,
			filterReason: "not_allowed",
		}
	}

	advertised, present := agentic.AdvertisedToolsFromMetadata(call.Metadata)
	if !present {
		return nil
	}
	if len(advertised) == 0 {
		c.logger.Warn("advertised tool set present but empty or malformed; failing closed",
			slog.String("tool", call.Name),
			slog.String("loop_id", call.LoopID),
			slog.Any("raw_value", call.Metadata[agentic.MetadataKeyAdvertisedTools]))
		return &toolAdmissionRejection{
			message:      fmt.Sprintf("tool %q is not permitted for this loop (advertised tool set)", call.Name),
			kind:         agentic.ToolErrorPermission,
			filterReason: "not_advertised",
		}
	}
	if !slices.Contains(advertised, call.Name) {
		return &toolAdmissionRejection{
			message:      fmt.Sprintf("tool %q is not permitted for this loop (advertised tool set)", call.Name),
			kind:         agentic.ToolErrorPermission,
			filterReason: "not_advertised",
		}
	}
	return nil
}

// executeWithTimeout executes a tool call with the configured timeout.
// It first checks the component's local registry, then falls back to the
// global registry. When the tool has an entry in config.ToolRetries the
// call is retried up to MaxAttempts on transient tool-level errors
// (default: timeout + external + network) with exponential backoff. Per-attempt
// timeout is applied to each try so a slow first call does not consume
// the whole budget.
func (c *Component) executeWithTimeout(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	timeout := 60 * time.Second
	if c.config.Timeout != "" {
		if d, err := time.ParseDuration(c.config.Timeout); err == nil {
			timeout = d
		}
	}

	policy := effectiveRetryPolicy(c.config.ToolRetries, call.Name)

	var (
		result agentic.ToolResult
		err    error
	)
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		result, err = c.executeOnce(ctx, call, timeout)
		if !shouldRetry(err, result, policy) || attempt == policy.MaxAttempts {
			break
		}

		// Record the retry before backing off so dashboards see it even if
		// the next attempt hangs.
		if c.metrics != nil {
			c.metrics.recordToolRetry(call.Name, string(result.ErrorKind))
		}

		wait := backoffFor(attempt, policy)
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(wait):
		}
	}

	// Surface exhaustion so ops can tune the policy.
	if c.metrics != nil && policy.MaxAttempts > 1 && shouldRetry(err, result, policy) {
		c.metrics.recordToolRetryExhausted(call.Name)
	}

	return result, err
}

// executeOnce performs a single attempt: per-attempt timeout, local
// registry with fallback to the shared (deps-injected) registry on a
// typed not-found miss. Replaces the previous string-match fallback,
// which broke whenever the underlying error text drifted.
func (c *Component) executeOnce(ctx context.Context, call agentic.ToolCall, timeout time.Duration) (agentic.ToolResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := c.registry.Execute(callCtx, call)
	if err != nil && errors.Is(err, agentic.ErrToolNotFound) && c.shared != nil {
		return c.shared.Execute(callCtx, call)
	}
	return result, err
}

// effectiveRetryPolicy returns the policy to apply for the named tool,
// filling in defaults for unset fields. Tools without an entry in the
// policies map get a no-retry default (MaxAttempts=1).
func effectiveRetryPolicy(policies map[string]RetryPolicy, toolName string) RetryPolicy {
	raw, ok := policies[toolName]
	if !ok {
		return RetryPolicy{MaxAttempts: 1}
	}
	p := raw
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.BackoffInitialMs <= 0 {
		p.BackoffInitialMs = 100
	}
	if p.BackoffMaxMs <= 0 {
		p.BackoffMaxMs = 2000
	}
	if p.RetryOnKinds == nil {
		// nil means "use defaults"; an explicit empty slice is respected.
		p.RetryOnKinds = []string{string(agentic.ToolErrorTimeout), string(agentic.ToolErrorExternal), string(agentic.ToolErrorNetwork)}
	}
	return p
}

// shouldRetry reports whether the outcome warrants another attempt under
// the given policy. Retries are confined to tool-level errors whose kind
// is listed in the policy. Raw executor errors (err != nil with no
// structured ErrorKind) are treated as framework faults and not retried
// here — those belong on the caller's side of the boundary.
func shouldRetry(err error, result agentic.ToolResult, policy RetryPolicy) bool {
	if policy.MaxAttempts <= 1 {
		return false
	}
	kind := string(result.ErrorKind)
	if kind == "" {
		// Tool executor returned err without classification. Not our retry.
		_ = err
		return false
	}
	return slices.Contains(policy.RetryOnKinds, kind)
}

// backoffFor returns the wait before the (attempt+1)th try, capped by
// policy.BackoffMaxMs. Attempt is 1-based.
func backoffFor(attempt int, policy RetryPolicy) time.Duration {
	// 2^(attempt-1) * initial, clamped to max.
	mult := 1 << (attempt - 1)
	ms := min(policy.BackoffInitialMs*mult, policy.BackoffMaxMs)
	return time.Duration(ms) * time.Millisecond
}

// publishResult publishes a tool result to JetStream.
//
// The constructor merges and resolves the required tool.result declaration;
// publication fails closed if that declaration is absent or malformed.
func (c *Component) publishResult(ctx context.Context, result agentic.ToolResult) error {
	return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.CallID))
}

func (c *Component) publishResultWithMsgID(ctx context.Context, result agentic.ToolResult, msgID string) error {
	data, err := marshalToolResult(result)
	if err != nil {
		if c.metrics != nil {
			c.metrics.recordPublishFailure(publishReasonMarshal)
		}
		return errs.Wrap(err, "Component", "publishResult", "marshal result")
	}

	subject, err := component.ResolveSubject(c.outputPortDefs(), "tool.result", result.CallID)
	if err != nil {
		return errs.WrapInvalid(err, "Component", "publishResult", "resolve output subject")
	}
	if c.publishStream == nil {
		return errs.WrapTransient(errors.New("tool result publisher is not initialized"), "Component", "publishResult", "publish result")
	}
	if err := c.publishStream(ctx, subject, data, msgID); err != nil {
		if c.metrics != nil {
			reason := publishReasonTransport
			if isObservedOversize(err) {
				reason = publishReasonOversize
			}
			c.metrics.recordPublishFailure(reason)
		}
		return errs.WrapTransient(err, "Component", "publishResult", fmt.Sprintf("publish to %s", subject))
	}
	return nil
}

// outputPortDefs returns the effective canonical output declarations used by
// publishResult to resolve the required tool.result subject.
func (c *Component) outputPortDefs() []component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	return c.config.Ports.Outputs
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// RegisterToolExecutor registers a tool executor with the component.
// Delegates to (*ExecutorRegistry).RegisterExecutor, which maps every name
// returned by executor.ListTools() to the executor atomically. Atomicity
// matters because partial commits on collision would leave dispatch in a
// half-wired state — a name that earlier in the slice succeeded would
// dispatch correctly while later names that collided would 400-not-found,
// and the caller has no programmatic way to roll back.
func (c *Component) RegisterToolExecutor(executor ToolExecutor) error {
	if err := c.registry.RegisterExecutor(executor); err != nil {
		return err
	}

	if c.metrics != nil {
		toolCount := len(executor.ListTools())
		allTools := c.registry.ListTools()
		c.metrics.recordToolsRegistered(len(allTools))
		c.logger.Info("Tools registered",
			slog.Int("count", toolCount),
			slog.Int("total", len(allTools)))
	}

	return nil
}

// ListTools returns all tool definitions for discovery.
// Combines tools from both the component's local registry and the
// shared (deps-injected) registry. Local entries override shared
// entries with the same name.
func (c *Component) ListTools() []ToolDefinition {
	// Get tools from component's local registry
	localTools := c.registry.ListTools()

	// Get tools from shared registry (nil-safe for tests that build
	// the component without injecting a shared registry).
	var globalTools []agentic.ToolDefinition
	if c.shared != nil {
		globalTools = c.shared.ListTools()
	}

	// Combine and convert to ToolDefinition format
	// Use a map to deduplicate by name (local registry takes precedence)
	toolMap := make(map[string]ToolDefinition)

	// Add global tools first. Effect is served RESOLVED (Canonical) so
	// discovery consumers read a declared value rather than
	// re-implementing the absent-means-unknown rule; the registry
	// normalizes too, and applying it here as well keeps the guarantee
	// independent of which registry supplied the definition.
	for _, tool := range globalTools {
		toolMap[tool.Name] = ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Provider:    "internal",
			Available:   true,
			Effect:      string(tool.Effect.Canonical()),
		}
	}

	// Add local tools (overwrites global if same name)
	for _, tool := range localTools {
		toolMap[tool.Name] = ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Provider:    "internal",
			Available:   true,
			Effect:      string(tool.Effect.Canonical()),
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
	// Check admission: global allowlist + per-loop advertised set (gh#551)
	if rejection := c.admitToolCall(call); rejection != nil {
		result := agentic.ToolResult{
			CallID:    call.ID,
			Error:     rejection.message,
			ErrorKind: rejection.kind,
			LoopID:    call.LoopID,
			TraceID:   call.TraceID,
		}
		return result, errs.WrapInvalid(errors.New(rejection.message), "Component", "Execute", "check tool admission")
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
	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputs...)
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
