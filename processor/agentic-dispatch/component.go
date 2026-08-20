package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// scopeTaskTools applies c.config.DefaultTools to task.Tools when
// configured. Mirrors the bus-dispatch and HTTP-dispatch paths so
// both honor the same scoping contract: nil DefaultTools leaves
// task.Tools unset (loop falls back to global discovery); an
// explicit empty slice (`"default_tools": []`) produces a non-nil
// empty task.Tools (loop respects "no tools for this role"). Names
// not in the agentictools registry are logged and dropped.
//
// Called from both handleTaskSubmission (bus path) and
// processTaskSubmissionSync (HTTP path) to close the gap where
// HTTP-spawned coordinator loops would silently receive the full
// global tool registry regardless of DefaultTools configuration.
func (c *Component) scopeTaskTools(task *agentic.TaskMessage) {
	if c.config.DefaultTools == nil {
		return
	}
	resolved := resolveDefaultTools(c.deps.ToolRegistry, c.config.DefaultTools, c.logger)
	if resolved == nil {
		resolved = []agentic.ToolDefinition{}
	}
	task.Tools = resolved
}

// resolveDefaultTools looks up the named tools in the supplied tool
// registry, logging and dropping any name not found. Mirror of the
// resolver in processor/rule/actions.go — kept separate to avoid
// pulling one processor into the other, since both resolvers are
// short.
//
// nil registry returns nil (default_tools resolution disabled —
// matches deployments without agentic-tools wired in).
func resolveDefaultTools(reg component.ToolRegistryReader, names []string, logger *slog.Logger) []agentic.ToolDefinition {
	if len(names) == 0 || reg == nil {
		return nil
	}
	all := reg.ListTools()
	byName := make(map[string]agentic.ToolDefinition, len(all))
	for _, t := range all {
		byName[t.Name] = t
	}

	resolved := make([]agentic.ToolDefinition, 0, len(names))
	for _, name := range names {
		if def, ok := byName[name]; ok {
			resolved = append(resolved, def)
			continue
		}
		if logger != nil {
			logger.Warn("default_tools name not found in registry; dropped",
				slog.String("tool_name", name))
		}
	}
	return resolved
}

// Component implements the router processor
type Component struct {
	config        Config
	deps          component.Dependencies
	decoder       *message.Decoder
	natsClient    *natsclient.Client
	logger        *slog.Logger
	loopTracker   *LoopTracker
	registry      *CommandRegistry
	metrics       *routerMetrics
	modelRegistry model.RegistryReader // Unified model registry for model selection

	// Lifecycle state
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	started        bool
	startTime      time.Time

	// Ports
	inputPorts  []component.Port
	outputPorts []component.Port

	// Track consumers for cleanup
	consumers []streamConsumerBinding

	// Shared AGENT_LOOPS read view (ADR-081): ONE graphview.View serves every
	// /activity SSE client. Lazily created on the first request — bucket
	// absence stays a per-request condition, never a boot failure — and
	// stopped with the component (stopActivityView).
	activityCommands chan activityViewCommand
	activityDone     chan struct{}
	activityCancel   context.CancelFunc
	// activityViewSource overrides the AGENT_LOOPS bucket handle in tests;
	// production leaves it nil (resolved via natsClient.GetKeyValueBucket).
	activityViewSource graphview.WatcherSource
	// activityViewOpts appends extra view options in tests (e.g. a fast
	// tick); production leaves it nil.
	activityViewOpts []graphview.Option
	// activityTestHooks run after the production metric hooks so tests can
	// synchronize on view internals; production leaves the zero value.
	activityTestHooks graphview.Hooks

	// sendResponseFn is a test hook; production leaves this nil. When non-nil
	// it replaces the NATS-publishing behavior of sendResponse.
	sendResponseFn func(agentic.UserResponse)
	// Terminal-only seams preserve production settlement semantics in focused
	// tests without weakening the normal response API.
	sendTerminalResponseFn func(context.Context, agentic.UserResponse, string) error
	loadPersistedLoopFn    func(context.Context, string) (*agentic.LoopEntity, error)
	terminalDeliveryDoneFn func(error)
	waitForStreamInput     func(context.Context, string) error
	consumeStream          func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitConsumerClosed     func(context.Context, <-chan struct{}) error
}

// consumerInfo tracks JetStream consumer details for cleanup
type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

type subscriptionInputBinding struct {
	portName       string
	streamName     string
	subject        string
	consumerConfig component.ConsumerConfig
}

type subscriptionInputBindings struct {
	userMessage     subscriptionInputBinding
	agentComplete   subscriptionInputBinding
	agentCreated    subscriptionInputBinding
	agentFailed     subscriptionInputBinding
	approvalPending subscriptionInputBinding
}

// NewComponent creates a new router component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Parse configuration
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "parse config")
	}

	// Require model registry
	if deps.ModelRegistry == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Component", "NewComponent", "deps.ModelRegistry is required")
	}

	// Apply defaults for empty values. Permissions get the same treatment
	// because JSON unmarshal of a config without a "permissions" block
	// leaves a zero-value PermissionConfig where every allow-list is nil —
	// silently denying all requests, including submit_task. Without this
	// fill-in, a minimal dispatch config (no "permissions" key) would
	// receive user messages and then log nothing because the permission
	// check fails before any task is published.
	if config.DefaultRole == "" {
		config.DefaultRole = DefaultConfig().DefaultRole
	}
	if config.StreamName == "" {
		config.StreamName = DefaultConfig().StreamName
	}
	if config.Permissions.View == nil && config.Permissions.SubmitTask == nil &&
		config.Permissions.CancelAny == nil && config.Permissions.Approve == nil &&
		!config.Permissions.CancelOwn {
		config.Permissions = DefaultConfig().Permissions
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}

	// Build ports
	merged := *DefaultConfig().Ports
	if config.Ports != nil {
		var err error
		merged, err = component.MergePortConfig(merged, *config.Ports)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "merge ports")
		}
	}
	config.Ports = &merged
	inputPorts := make([]component.Port, 0, len(merged.Inputs))
	for _, definition := range merged.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve input port")
		}
		inputPorts = append(inputPorts, port)
	}
	outputPorts := make([]component.Port, 0, len(merged.Outputs))
	for _, definition := range merged.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve output port")
		}
		outputPorts = append(outputPorts, port)
	}

	logger := deps.GetLogger()
	comp := &Component{
		config:        config,
		deps:          deps,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		natsClient:    deps.NATSClient,
		logger:        logger,
		loopTracker:   NewLoopTrackerWithLogger(logger),
		registry:      NewCommandRegistry(),
		metrics:       getMetrics(deps.MetricsRegistry),
		modelRegistry: deps.ModelRegistry,
		inputPorts:    inputPorts,
		outputPorts:   outputPorts,
	}

	// Register built-in commands
	comp.registerBuiltinCommands()

	// Load globally registered commands
	comp.loadGlobalCommands()

	return comp, nil
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "router",
		Type:        "processor",
		Description: "Routes user messages to agentic loops with command parsing and permissions",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	return c.inputPorts
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	return c.outputPorts
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := c.started
	uptime := time.Duration(0)
	if c.started {
		uptime = time.Since(c.startTime)
	}

	status := "stopped"
	if healthy {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Uptime:    uptime,
		Status:    status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing
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
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	activityCtx, activityCancel := context.WithCancel(runCtx)
	activityCommands := make(chan activityViewCommand)
	activityDone := make(chan struct{})
	c.lifecycleUsed = true
	c.cleanupPending = true
	c.cancel = cancel
	c.startDone = startDone
	c.activityCommands = activityCommands
	c.activityDone = activityDone
	c.activityCancel = activityCancel
	c.lifecycleMu.Unlock()
	go c.runActivityViewControl(activityCtx, activityCommands, activityDone)
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

	c.logger.Info("Starting router component")

	// Setup subscriptions
	if err := c.setupSubscriptions(runCtx); err != nil {
		return errs.Wrap(err, "Component", "Start", "setup subscriptions")
	}
	c.mu.Lock()
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()
	committed = true

	c.logger.Info("Router component started",
		slog.Int("commands", c.registry.Count()))

	return nil
}

// Stop halts processing with graceful shutdown
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
		c.started = false
		c.mu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanup(ctx context.Context) error {
	var stopErr error
	for i := range c.consumers {
		binding := &c.consumers[i]
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
		stopErr = errors.Join(stopErr, c.awaitConsumerClosed(ctx, binding.handle.Closed()))
	}
	c.stopActivityView()
	if c.cancel != nil {
		c.cancel()
	}
	if err := ctx.Err(); err != nil {
		stopErr = errors.Join(stopErr, err)
	}
	return stopErr
}

func (c *Component) awaitConsumerClosed(ctx context.Context, closed <-chan struct{}) error {
	if c.waitConsumerClosed != nil {
		return c.waitConsumerClosed(ctx, closed)
	}
	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Component) consumeStreamHandle(ctx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
	if c.consumeStream != nil {
		return c.consumeStream(ctx, owner, cfg, handler)
	}
	return c.natsClient.ConsumeStreamWithConfigHandle(ctx, owner, cfg, handler)
}

func (c *Component) clearLifecycleHandles() {
	c.consumers = nil
	c.cancel = nil
	c.activityCommands = nil
	c.activityDone = nil
	c.activityCancel = nil
}

// setupSubscriptions sets up JetStream consumers for durable messaging
func (c *Component) setupSubscriptions(ctx context.Context) error {
	waitForStream := c.waitForStream
	if c.waitForStreamInput != nil {
		waitForStream = c.waitForStreamInput
	}
	bindings, err := c.resolveAndWaitForSubscriptionBindings(ctx, waitForStream)
	if err != nil {
		return err
	}

	// Subscribe to user messages via JetStream
	// Use "last" policy to catch messages sent just before consumer starts
	userMsgCfg := natsclient.StreamConsumerConfig{
		StreamName:    bindings.userMessage.streamName,
		ConsumerName:  c.consumerName("agentic-dispatch-user-message"),
		FilterSubject: bindings.userMessage.subject,
		DeliverPolicy: "last",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		MaxAckPending: bindings.userMessage.consumerConfig.MaxAckPending,
		AutoCreate:    false,
	}
	handle, err := c.consumeStreamHandle(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: bindings.userMessage.portName}, userMsgCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleUserMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack user message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to user.message")
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	// Subscribe to agent completions via JetStream
	agentCompleteCfg := natsclient.StreamConsumerConfig{
		StreamName:    bindings.agentComplete.streamName,
		ConsumerName:  c.consumerName("agentic-dispatch-agent-complete"),
		FilterSubject: bindings.agentComplete.subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    0,
		MaxAckPending: bindings.agentComplete.consumerConfig.MaxAckPending,
		AutoCreate:    false,
	}
	handle, err = c.consumeStreamHandle(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: bindings.agentComplete.portName}, agentCompleteCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handleErr := c.handleTerminalDelivery(msgCtx, msg)
		c.observeTerminalDelivery(handleErr)
		if handleErr != nil {
			c.logger.Warn("Agent completion settlement failed", slog.Any("error", handleErr))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.complete")
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	// Subscribe to loop created events for workflow context sync
	agentCreatedCfg := natsclient.StreamConsumerConfig{
		StreamName:    bindings.agentCreated.streamName,
		ConsumerName:  c.consumerName("agentic-dispatch-agent-created"),
		FilterSubject: bindings.agentCreated.subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		MaxAckPending: bindings.agentCreated.consumerConfig.MaxAckPending,
		AutoCreate:    false,
	}
	handle, err = c.consumeStreamHandle(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: bindings.agentCreated.portName}, agentCreatedCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleAgentCreated(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack agent created message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.created")
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	// Subscribe to loop failed events
	agentFailedCfg := natsclient.StreamConsumerConfig{
		StreamName:    bindings.agentFailed.streamName,
		ConsumerName:  c.consumerName("agentic-dispatch-agent-failed"),
		FilterSubject: bindings.agentFailed.subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    0,
		MaxAckPending: bindings.agentFailed.consumerConfig.MaxAckPending,
		AutoCreate:    false,
	}
	handle, err = c.consumeStreamHandle(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: bindings.agentFailed.portName}, agentFailedCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handleErr := c.handleTerminalDelivery(msgCtx, msg)
		c.observeTerminalDelivery(handleErr)
		if handleErr != nil {
			c.logger.Warn("Agent failure settlement failed", slog.Any("error", handleErr))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.failed")
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	// Subscribe to approval-pending events so the HTTP approval
	// handler has the loop's CallID + tool args available locally
	// (no KV.Get round-trip per request). Optional port — the
	// dispatch surface continues to function without it; only the
	// approval HTTP endpoint requires the cache populated.
	//
	// MaxDeliver is intentionally finite while terminal sibling subscriptions
	// use unlimited delivery: a missed approval-pending event has
	// asymmetric blast radius — it leaves the HTTP approval handler
	// returning 400 forever for that loop until the next approval
	// cycle. Terminal siblings instead use unlimited, retention-bounded
	// settlement. Combined with the LoopTracker's early-arrival
	// buffer (drains on the matching agent.created), 10 retries gives
	// generous slack for race resolution without unbounded redelivery.
	agentApprovalPendingCfg := natsclient.StreamConsumerConfig{
		StreamName:    bindings.approvalPending.streamName,
		ConsumerName:  c.consumerName("agentic-dispatch-agent-approval-pending"),
		FilterSubject: bindings.approvalPending.subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    10,
		MaxAckPending: bindings.approvalPending.consumerConfig.MaxAckPending,
		AutoCreate:    false,
	}
	handle, err = c.consumeStreamHandle(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: bindings.approvalPending.portName}, agentApprovalPendingCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleAgentApprovalPending(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack agent approval-pending message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.approval_pending")
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	return nil
}

func (c *Component) observeTerminalDelivery(err error) {
	if c.terminalDeliveryDoneFn != nil {
		c.terminalDeliveryDoneFn(err)
	}
}

// handleTerminalDelivery is the shared production callback for both physical
// terminal lanes. Semantic category authority remains in agentterminal.Decode.
func (c *Component) handleTerminalDelivery(msgCtx context.Context, msg jetstream.Msg) error {
	return natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {
		err := c.settleAgentTerminal(workCtx, msg.Data())
		if isPermanentTerminal(err) {
			return natsclient.TerminateDelivery(err)
		}
		return err
	})
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

// handleUserMessage processes incoming user messages
func (c *Component) handleUserMessage(ctx context.Context, data []byte) {
	startTime := time.Now()

	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	userMsg, ok := baseMsg.Payload().(*agentic.UserMessage)
	if !ok {
		c.logger.Error("Unexpected payload type", slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}
	msg := *userMsg

	// Record message received
	c.metrics.recordMessageReceived(msg.ChannelType)

	c.logger.Debug("Received user message",
		slog.String("message_id", msg.MessageID),
		slog.String("user_id", msg.UserID),
		slog.String("channel", msg.ChannelType))

	// Check if it's a command
	if strings.HasPrefix(msg.Content, "/") {
		c.handleCommand(ctx, msg)
	} else {
		// It's a task submission
		c.handleTaskSubmission(ctx, msg)
	}

	// Record routing duration
	duration := time.Since(startTime).Seconds()
	c.metrics.recordRoutingDuration(duration)
}

// handleCommand processes command messages
func (c *Component) handleCommand(ctx context.Context, msg agentic.UserMessage) {
	name, cmd, args, found := c.registry.Match(msg.Content)
	if !found {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Unknown command. Type /help for available commands.",
			Timestamp:   time.Now(),
		})
		return
	}

	// Check permission
	if cmd.Config.Permission != "" && !c.hasPermission(msg.UserID, cmd.Config.Permission) {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Permission denied: requires '%s'", cmd.Config.Permission),
			Timestamp:   time.Now(),
		})
		return
	}

	// Resolve loop ID
	loopID := ""
	if len(args) > 0 && args[0] != "" {
		loopID = args[0]
	} else if c.config.AutoContinue {
		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	}

	// Check if loop is required
	if cmd.Config.RequireLoop && loopID == "" {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "No active loop. Specify a loop_id or start a task first.",
			Timestamp:   time.Now(),
		})
		return
	}

	// Execute handler
	resp, err := cmd.Handler(ctx, msg, args, loopID)
	if err != nil {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Command failed: %s", err.Error()),
			Timestamp:   time.Now(),
		})
		return
	}

	c.sendResponse(ctx, resp)

	// Record command executed
	c.metrics.recordCommandExecuted(name)

	c.logger.Debug("Command executed",
		slog.String("command", name),
		slog.String("user_id", msg.UserID))
}

// resolveModel returns the default model from the model registry.
func (c *Component) resolveModel() string {
	return c.modelRegistry.GetDefault()
}

// buildTaskMessage assembles the TaskMessage published to agentic-loop from an
// inbound UserMessage. Shared by the bus path (handleTaskSubmission) and the
// HTTP sync path (processTaskSubmissionSync) so the propagation contract is
// identical regardless of dispatch surface — a single place that can't drift on
// which fields a reply carries (the gh#256 defect class: the reply path silently
// dropping RunID + the reply marker the other path would have carried).
func (c *Component) buildTaskMessage(ctx context.Context, msg agentic.UserMessage, loopID, taskID string) agentic.TaskMessage {
	task := agentic.TaskMessage{
		LoopID:           loopID,
		TaskID:           taskID,
		Role:             c.config.DefaultRole,
		Model:            c.resolveModel(),
		Prompt:           msg.Content,
		ContextRequestID: msg.ContextRequestID,
		// Resumable-reply anchors (gh#256). Both omitempty and client-set, so an
		// ordinary submission carries neither: RunID re-attaches the resumed loop
		// to its paused run (→ agent.loop.run / agent.run.entity-id), InReplyTo marks
		// it as a reply (→ agent.loop.reply_to) so a resume rule can fire on it.
		RunID:     msg.RunID,
		InReplyTo: msg.InReplyTo,
	}

	// Propagate inbound trace_id onto TaskMessage.Metadata so the downstream
	// LoopEntity.Metadata surfaces it for wedge investigation
	// (curl /loops/<id> → metadata.trace_id → message-logger lookup).
	stampTraceIDFromCtx(ctx, &task)

	// Scope the initial agent's tools to DefaultTools when configured, so an
	// HTTP- or bus-spawned loop honors the operator default_tools contract
	// instead of falling back to global discovery.
	c.scopeTaskTools(&task)

	return task
}

// handleTaskSubmission creates a new agent task
func (c *Component) handleTaskSubmission(ctx context.Context, msg agentic.UserMessage) {
	// Check submit permission
	if !c.hasPermission(msg.UserID, "submit_task") {
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Permission denied: cannot submit tasks",
			Timestamp:   time.Now(),
		})
		return
	}

	// Determine loop ID (continue existing or create new)
	loopID := ""
	if msg.ReplyTo != "" {
		loopID = msg.ReplyTo
	} else if c.config.AutoContinue {
		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	}

	// Create new loop if needed
	if loopID == "" {
		loopID = "loop_" + uuid.New().String()[:8]
	}

	taskID := uuid.New().String()

	// Create task message (shared builder — see buildTaskMessage; gh#256).
	task := c.buildTaskMessage(ctx, msg, loopID, taskID)

	// Track the loop
	c.loopTracker.Track(&LoopInfo{
		LoopID:           loopID,
		TaskID:           taskID,
		Role:             task.Role,
		UserID:           msg.UserID,
		ChannelType:      msg.ChannelType,
		ChannelID:        msg.ChannelID,
		State:            "pending",
		MaxIterations:    20,
		ContextRequestID: msg.ContextRequestID,
		CreatedAt:        time.Now(),
	})

	// Record loop started
	c.metrics.recordLoopStarted()

	// Wrap task in BaseMessage envelope (required by agentic-loop)
	baseMsg := message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch")
	taskData, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal task", slog.String("error", err.Error()))
		return
	}

	subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)
	if err != nil {
		c.logger.Error("Failed to resolve task subject", slog.String("error", err.Error()))
		return
	}
	if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {
		c.logger.Error("Failed to publish task", slog.String("error", err.Error()))
		c.sendResponse(ctx, agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Failed to submit task. Please try again.",
			Timestamp:   time.Now(),
		})
		return
	}

	// Record task submitted
	c.metrics.recordTaskSubmitted()

	// Send acknowledgment
	c.sendResponse(ctx, agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   loopID,
		Type:        agentic.ResponseTypeStatus,
		Content:     fmt.Sprintf("Task submitted. Loop: %s", loopID),
		Timestamp:   time.Now(),
	})

	c.logger.Debug("Task submitted",
		slog.String("loop_id", loopID),
		slog.String("task_id", taskID),
		slog.String("user_id", msg.UserID))
}

// handleAgentComplete is the focused-test entry point for the shared terminal
// settlement path used by both physical terminal consumers.
func (c *Component) handleAgentComplete(ctx context.Context, data []byte) {
	if err := c.settleAgentTerminal(ctx, data); err != nil {
		c.logger.Warn("Agent terminal settlement failed", slog.Any("error", err))
	}
}

// handleAgentCreated processes loop creation events for workflow context sync
func (c *Component) handleAgentCreated(_ context.Context, data []byte) {
	// Parse BaseMessage envelope
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	// Extract LoopCreatedEvent from payload
	createdPtr, ok := baseMsg.Payload().(*agentic.LoopCreatedEvent)
	if !ok {
		c.logger.Error("Unexpected payload type", slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}
	created := *createdPtr

	// Check if we already track this loop (we originated it)
	if existing := c.loopTracker.Get(created.LoopID); existing != nil {
		// Atomically update workflow context if missing
		c.loopTracker.UpdateWorkflowContext(created.LoopID, created.WorkflowSlug, created.WorkflowStep)
		// Atomically update context request ID if missing
		c.loopTracker.UpdateContextRequestID(created.LoopID, created.ContextRequestID)
		return
	}

	// New loop we didn't originate - track it
	c.loopTracker.Track(&LoopInfo{
		LoopID:           created.LoopID,
		TaskID:           created.TaskID,
		Role:             created.Role,
		State:            "executing",
		MaxIterations:    created.MaxIterations,
		WorkflowSlug:     created.WorkflowSlug,
		WorkflowStep:     created.WorkflowStep,
		ContextRequestID: created.ContextRequestID,
		Metadata:         created.Metadata,
		CreatedAt:        created.CreatedAt,
	})

	// Record external loop for metrics (will be decremented by handleAgentComplete)
	c.metrics.recordLoopStarted()

	c.logger.Debug("Tracked external loop",
		slog.String("loop_id", created.LoopID),
		slog.String("workflow_slug", created.WorkflowSlug),
		slog.String("workflow_step", created.WorkflowStep))
}

// handleAgentFailed processes loop failure events
func (c *Component) handleAgentFailed(ctx context.Context, data []byte) {
	if err := c.settleAgentTerminal(ctx, data); err != nil {
		c.logger.Warn("Agent terminal settlement failed", slog.Any("error", err))
	}
}

// handleAgentApprovalPending records the gated tool-call info on the
// loop tracker so the HTTP approval handler has the loop's CallID +
// tool args available locally without a KV.Get round-trip per
// request. The framework's agentic-loop emits this event when a tool
// call hits config.approval_required and the loop transitions to
// LoopStateAwaitingApproval; dispatch is one of several subscribers
// (the others being product-layer approval UIs).
func (c *Component) handleAgentApprovalPending(_ context.Context, data []byte) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	pending, ok := baseMsg.Payload().(*agentic.ApprovalPendingEvent)
	if !ok {
		c.logger.Error("Unexpected payload type",
			slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}

	if pending.LoopID == "" || pending.CallID == "" {
		c.logger.Warn("approval-pending event missing required fields",
			slog.String("loop_id", pending.LoopID),
			slog.String("call_id", pending.CallID))
		return
	}

	// SetPendingApproval handles the unknown-loop race internally by
	// buffering until the matching agent.created arrives. Returns
	// false on miss-or-buffered; either way the framework's loop
	// state is canonical and the HTTP handler degrades gracefully.
	c.loopTracker.SetPendingApproval(pending.LoopID, &PendingApprovalInfo{
		CallID:      pending.CallID,
		ToolName:    pending.ToolName,
		Arguments:   pending.Arguments,
		Reason:      pending.Reason,
		RequestedAt: pending.RequestedAt,
		TraceID:     pending.TraceID,
	})
}

// sendResponse publishes a response to the user's channel
func (c *Component) sendResponse(ctx context.Context, resp agentic.UserResponse) {
	if c.sendResponseFn != nil {
		c.sendResponseFn(resp)
		return
	}
	respMsg := message.NewBaseMessage(resp.Schema(), &resp, "agentic-dispatch")
	data, err := json.Marshal(respMsg)
	if err != nil {
		c.logger.Error("Failed to marshal response", slog.String("error", err.Error()))
		return
	}

	subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", resp.ChannelType+"."+resp.ChannelID)
	if err != nil {
		c.logger.Error("Failed to resolve response subject", slog.String("error", err.Error()))
		return
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to publish response", slog.String("error", err.Error()))
	}
}

// sendUserResponseForLoop sends a response only if the loop has a user channel.
// This prevents invalid NATS subjects like "user.response.." for loops without user routing.
// Workflow-initiated loops that lack user routing are silently skipped.
func (c *Component) sendUserResponseForLoop(ctx context.Context, loopInfo *LoopInfo, respType, content string) {
	if loopInfo.ChannelType == "" || loopInfo.ChannelID == "" {
		c.logger.Debug("Skipping user response for loop without user routing",
			slog.String("loop_id", loopInfo.LoopID),
			slog.String("channel_type", loopInfo.ChannelType),
			slog.String("channel_id", loopInfo.ChannelID),
			slog.String("workflow_slug", loopInfo.WorkflowSlug))
		return
	}

	c.sendResponse(ctx, agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: loopInfo.ChannelType,
		ChannelID:   loopInfo.ChannelID,
		UserID:      loopInfo.UserID,
		InReplyTo:   loopInfo.LoopID,
		Type:        respType,
		Content:     content,
		Timestamp:   time.Now(),
	})
}

// hasPermission checks if a user has a specific permission
func (c *Component) hasPermission(userID, permission string) bool {
	switch permission {
	case "view":
		return c.inList(userID, c.config.Permissions.View)
	case "submit_task":
		return c.inList(userID, c.config.Permissions.SubmitTask)
	case "cancel_own":
		return c.config.Permissions.CancelOwn
	case "cancel_any":
		return c.inList(userID, c.config.Permissions.CancelAny)
	case "approve":
		return c.inList(userID, c.config.Permissions.Approve)
	default:
		return false
	}
}

// inList checks if a user is in a permission list
func (c *Component) inList(userID string, list []string) bool {
	for _, allowed := range list {
		if allowed == "*" || allowed == userID {
			return true
		}
	}
	return false
}

// canUserControlLoop checks if a user can control a specific loop
func (c *Component) canUserControlLoop(userID, loopID string) bool {
	// Can always control if has cancel_any
	if c.inList(userID, c.config.Permissions.CancelAny) {
		return true
	}

	// Check if user owns the loop
	loopInfo := c.loopTracker.Get(loopID)
	if loopInfo == nil {
		return false
	}

	return loopInfo.UserID == userID && c.config.Permissions.CancelOwn
}

// CommandRegistry returns the command registry for external registration
func (c *Component) CommandRegistry() *CommandRegistry {
	return c.registry
}

// LoopTracker returns the loop tracker
func (c *Component) LoopTracker() *LoopTracker {
	return c.loopTracker
}

// inputPortBinding returns the configured stream and sole subject for a named JetStream input.
func (c *Component) inputPortBinding(portName string) (string, string, error) {
	for _, port := range c.inputPorts {
		if port.Name != portName {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		stream, ok := facts.Stream()
		if !ok || len(stream.Subjects()) != 1 {
			return "", "", fmt.Errorf("input port %q must declare one JetStream subject", portName)
		}
		return stream.Name(), stream.Subjects()[0], nil
	}
	return "", "", fmt.Errorf("input port %q not found", portName)
}

// resolveAndWaitForSubscriptionBindings resolves the complete dispatch input
// topology before waiting once for each distinct configured backing stream.
// The returned bindings are the same values setupSubscriptions uses to create
// consumers, so resolved port facts remain the only stream/subject authority.
func (c *Component) resolveAndWaitForSubscriptionBindings(
	ctx context.Context,
	wait func(context.Context, string) error,
) (subscriptionInputBindings, error) {
	resolve := func(portName string) (subscriptionInputBinding, error) {
		streamName, subject, err := c.inputPortBinding(portName)
		if err != nil {
			return subscriptionInputBinding{}, err
		}
		for _, port := range c.inputPorts {
			if port.Name == portName {
				consumerConfig, configErr := component.GetConsumerConfig(port)
				if configErr != nil {
					return subscriptionInputBinding{}, configErr
				}
				return subscriptionInputBinding{portName: portName, streamName: streamName, subject: subject, consumerConfig: consumerConfig}, nil
			}
		}
		return subscriptionInputBinding{}, fmt.Errorf("input port %q not found", portName)
	}

	var bindings subscriptionInputBindings
	var err error
	bindings.userMessage, err = resolve("user.message")
	if err != nil {
		return subscriptionInputBindings{}, err
	}
	bindings.agentComplete, err = resolve("agent.complete")
	if err != nil {
		return subscriptionInputBindings{}, err
	}
	bindings.agentCreated, err = resolve("agent.created")
	if err != nil {
		return subscriptionInputBindings{}, err
	}
	bindings.agentFailed, err = resolve("agent.failed")
	if err != nil {
		return subscriptionInputBindings{}, err
	}
	bindings.approvalPending, err = resolve("agent.approval_pending")
	if err != nil {
		return subscriptionInputBindings{}, err
	}

	streamNames := []string{
		bindings.userMessage.streamName,
		bindings.agentComplete.streamName,
		bindings.agentCreated.streamName,
		bindings.agentFailed.streamName,
		bindings.approvalPending.streamName,
	}
	seen := make(map[string]struct{}, len(streamNames))
	for _, streamName := range streamNames {
		if _, ok := seen[streamName]; ok {
			continue
		}
		seen[streamName] = struct{}{}
		if err := wait(ctx, streamName); err != nil {
			return subscriptionInputBindings{}, errs.WrapTransient(
				err,
				"Component",
				"setupSubscriptions",
				fmt.Sprintf("wait for stream %s", streamName),
			)
		}
	}

	return bindings, nil
}

// consumerName appends ConsumerNameSuffix to the base name when set, allowing
// multiple dispatch instances to coexist in one process without colliding on
// JetStream consumer names. Empty suffix preserves today's hardcoded names.
func (c *Component) consumerName(base string) string {
	if c.config.ConsumerNameSuffix == "" {
		return base
	}
	return base + "-" + c.config.ConsumerNameSuffix
}

// outputPortDefs returns the validated output port definitions.
func (c *Component) outputPortDefs() []component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	return c.config.Ports.Outputs
}

// loadGlobalCommands loads globally registered commands into the component
func (c *Component) loadGlobalCommands() {
	cmdCtx := &CommandContext{
		NATSClient:    c.natsClient,
		LoopTracker:   c.loopTracker,
		Logger:        c.logger,
		HasPermission: c.hasPermission,
	}

	for name, executor := range ListRegisteredCommands() {
		config := executor.Config()

		// Wrap executor in handler function
		handler := func(exec CommandExecutor) CommandHandler {
			return func(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error) {
				return exec.Execute(ctx, cmdCtx, msg, args, loopID)
			}
		}(executor)

		if err := c.registry.Register(name, config, handler); err != nil {
			c.logger.Warn("Failed to register global command",
				slog.String("command", name),
				slog.String("error", err.Error()))
		}
	}
}
