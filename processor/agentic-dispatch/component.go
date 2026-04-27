package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

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
	natsClient    *natsclient.Client
	logger        *slog.Logger
	loopTracker   *LoopTracker
	registry      *CommandRegistry
	metrics       *routerMetrics
	modelRegistry model.RegistryReader // Unified model registry for model selection

	// Lifecycle state
	mu        sync.RWMutex
	started   bool
	startTime time.Time

	// Ports
	inputPorts  []component.Port
	outputPorts []component.Port

	// Track consumers for cleanup
	consumerInfos []consumerInfo

	// sendResponseFn is a test hook; production leaves this nil. When non-nil
	// it replaces the NATS-publishing behavior of sendResponse.
	sendResponseFn func(agentic.UserResponse)
}

// consumerInfo tracks JetStream consumer details for cleanup
type consumerInfo struct {
	streamName   string
	consumerName string
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
	inputPorts := buildDefaultInputPorts()
	outputPorts := buildDefaultOutputPorts()

	if config.Ports != nil {
		if len(config.Ports.Inputs) > 0 {
			inputPorts = component.MergePortConfigs(inputPorts, config.Ports.Inputs, component.DirectionInput)
		}
		if len(config.Ports.Outputs) > 0 {
			outputPorts = component.MergePortConfigs(outputPorts, config.Ports.Outputs, component.DirectionOutput)
		}
	}

	logger := deps.GetLogger()
	comp := &Component{
		config:        config,
		deps:          deps,
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
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errs.ErrAlreadyStarted
	}
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	c.logger.Info("Starting router component")

	// Setup subscriptions
	if err := c.setupSubscriptions(ctx); err != nil {
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return errs.Wrap(err, "Component", "Start", "setup subscriptions")
	}

	c.logger.Info("Router component started",
		slog.Int("commands", c.registry.Count()))

	return nil
}

// Stop halts processing with graceful shutdown
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
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

	c.started = false
	c.mu.Unlock()

	c.logger.Info("Router component stopped")
	return nil
}

// setupSubscriptions sets up JetStream consumers for durable messaging
func (c *Component) setupSubscriptions(ctx context.Context) error {
	// Wait for streams to be available
	if err := c.waitForStream(ctx, c.config.StreamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", fmt.Sprintf("wait for stream %s", c.config.StreamName))
	}
	if err := c.waitForStream(ctx, "AGENT"); err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "wait for stream AGENT")
	}

	// Subscribe to user messages via JetStream
	// Use "last" policy to catch messages sent just before consumer starts
	userMsgCfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.consumerName("agentic-dispatch-user-message"),
		FilterSubject: c.inputPortSubject("user.message", "user.message.>"),
		DeliverPolicy: "last",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AutoCreate:    false,
	}
	err := c.natsClient.ConsumeStreamWithConfig(ctx, userMsgCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleUserMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack user message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to user.message")
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   userMsgCfg.StreamName,
		consumerName: userMsgCfg.ConsumerName,
	})

	// Subscribe to agent completions via JetStream
	agentCompleteCfg := natsclient.StreamConsumerConfig{
		StreamName:    c.inputPortStream("agent.complete", "AGENT"),
		ConsumerName:  c.consumerName("agentic-dispatch-agent-complete"),
		FilterSubject: c.inputPortSubject("agent.complete", "agent.complete.*"),
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AutoCreate:    false,
	}
	err = c.natsClient.ConsumeStreamWithConfig(ctx, agentCompleteCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleAgentComplete(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack agent complete message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.complete")
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   agentCompleteCfg.StreamName,
		consumerName: agentCompleteCfg.ConsumerName,
	})

	// Subscribe to loop created events for workflow context sync
	agentCreatedCfg := natsclient.StreamConsumerConfig{
		StreamName:    c.inputPortStream("agent.created", "AGENT"),
		ConsumerName:  c.consumerName("agentic-dispatch-agent-created"),
		FilterSubject: c.inputPortSubject("agent.created", "agent.created.*"),
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AutoCreate:    false,
	}
	err = c.natsClient.ConsumeStreamWithConfig(ctx, agentCreatedCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleAgentCreated(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack agent created message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.created")
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   agentCreatedCfg.StreamName,
		consumerName: agentCreatedCfg.ConsumerName,
	})

	// Subscribe to loop failed events
	agentFailedCfg := natsclient.StreamConsumerConfig{
		StreamName:    c.inputPortStream("agent.failed", "AGENT"),
		ConsumerName:  c.consumerName("agentic-dispatch-agent-failed"),
		FilterSubject: c.inputPortSubject("agent.failed", "agent.failed.*"),
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AutoCreate:    false,
	}
	err = c.natsClient.ConsumeStreamWithConfig(ctx, agentFailedCfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleAgentFailed(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack agent failed message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupSubscriptions", "subscribe to agent.failed")
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   agentFailedCfg.StreamName,
		consumerName: agentFailedCfg.ConsumerName,
	})

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

// handleUserMessage processes incoming user messages
func (c *Component) handleUserMessage(ctx context.Context, data []byte) {
	startTime := time.Now()

	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
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

	// Create task message
	task := agentic.TaskMessage{
		LoopID:           loopID,
		TaskID:           taskID,
		Role:             c.config.DefaultRole,
		Model:            c.resolveModel(),
		Prompt:           msg.Content,
		ContextRequestID: msg.ContextRequestID,
	}

	// Scope the initial agent's tools to DefaultTools when configured.
	// Nil DefaultTools leaves task.Tools unset so the spawned loop falls
	// back to global discovery (pre-existing behaviour). An explicit empty
	// slice (`"default_tools": []` in flow config) produces a non-nil empty
	// task.Tools, which the loop respects as "no tools for this role".
	// Names not in the agentictools registry are logged and dropped.
	if c.config.DefaultTools != nil {
		resolved := resolveDefaultTools(c.deps.ToolRegistry, c.config.DefaultTools, c.logger)
		if resolved == nil {
			resolved = []agentic.ToolDefinition{}
		}
		task.Tools = resolved
	}

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

	subject := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)
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

// handleAgentComplete processes agent completion events
func (c *Component) handleAgentComplete(ctx context.Context, data []byte) {
	// Parse BaseMessage envelope
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	// Extract LoopCompletedEvent from payload
	completionPtr, ok := baseMsg.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		c.logger.Error("Unexpected payload type", slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}
	completion := *completionPtr

	// Get loop info
	loopInfo := c.loopTracker.Get(completion.LoopID)
	if loopInfo == nil {
		c.logger.Warn("Completion for unknown loop", slog.String("loop_id", completion.LoopID))
		return
	}

	// Update loop state via UpdateCompletion so Outcome → State mapping
	// goes through outcomeToState (e.g., outcome="success" → state="complete").
	// UpdateState would have written the raw outcome string into State,
	// breaking isTerminalState() for the success path. Result/CompletedAt
	// also get populated for /loops + SSE consumers.
	if err := c.loopTracker.UpdateCompletion(
		completion.LoopID,
		completion.Outcome,
		completion.Result,
		"",
	); err != nil {
		c.logger.Error("failed to record loop completion",
			slog.String("loop_id", completion.LoopID),
			slog.String("outcome", completion.Outcome),
			slog.Any("error", err))
		// Continue: response building below should still happen so the
		// user sees something even if our internal tracker is out of sync.
	}

	// Record loop ended
	c.metrics.recordLoopEnded()

	// Record completion received
	c.metrics.recordCompletionReceived(completion.Outcome)

	// Build response content
	var content string
	var respType string
	switch completion.Outcome {
	case "complete":
		respType = agentic.ResponseTypeResult
		content = fmt.Sprintf("Loop %s completed.", completion.LoopID)
		if completion.Result != "" {
			content = completion.Result
		}
	case "cancelled":
		respType = agentic.ResponseTypeStatus
		content = fmt.Sprintf("Loop %s cancelled.", completion.LoopID)
	case "failed":
		// Note: Failed loops are handled by handleAgentFailed, but handle gracefully
		respType = agentic.ResponseTypeError
		content = fmt.Sprintf("Loop %s failed", completion.LoopID)
	default:
		respType = agentic.ResponseTypeStatus
		content = fmt.Sprintf("Loop %s: %s", completion.LoopID, completion.Outcome)
	}

	// Send response to user (skipped for workflow-initiated loops without user routing)
	c.sendUserResponseForLoop(ctx, loopInfo, respType, content)

	c.logger.Info("Loop completed",
		slog.String("loop_id", completion.LoopID),
		slog.String("outcome", completion.Outcome))
}

// handleAgentCreated processes loop creation events for workflow context sync
func (c *Component) handleAgentCreated(_ context.Context, data []byte) {
	// Parse BaseMessage envelope
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
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
	// Parse BaseMessage envelope
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	// Extract LoopFailedEvent from payload
	failurePtr, ok := baseMsg.Payload().(*agentic.LoopFailedEvent)
	if !ok {
		c.logger.Error("Unexpected payload type", slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}
	failure := *failurePtr

	// Update loop state via UpdateCompletion so failure.Error and
	// CompletedAt are recorded too — UpdateState alone would only set
	// the State field and discard everything else from the failure event.
	if err := c.loopTracker.UpdateCompletion(
		failure.LoopID,
		failure.Outcome,
		"",
		failure.Error,
	); err != nil {
		c.logger.Error("failed to record loop failure",
			slog.String("loop_id", failure.LoopID),
			slog.String("outcome", failure.Outcome),
			slog.Any("error", err))
	}

	// Record metrics
	c.metrics.recordLoopEnded()
	c.metrics.recordCompletionReceived("failed")

	// Get loop info for response routing
	loopInfo := c.loopTracker.Get(failure.LoopID)
	if loopInfo == nil {
		c.logger.Warn("Failure for unknown loop", slog.String("loop_id", failure.LoopID))
		return
	}

	// Send error response to user (skipped for workflow-initiated loops without user routing)
	errorContent := fmt.Sprintf("Loop %s failed: %s", failure.LoopID, failure.Error)
	c.sendUserResponseForLoop(ctx, loopInfo, agentic.ResponseTypeError, errorContent)

	c.logger.Info("Loop failed",
		slog.String("loop_id", failure.LoopID),
		slog.String("reason", failure.Reason),
		slog.String("error", failure.Error))
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

	subject := component.ResolveSubject(c.outputPortDefs(), "user.response", resp.ChannelType+"."+resp.ChannelID)
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

// inputPortSubject returns the configured subject for the named input port,
// falling back to the provided default when no matching port is found.
func (c *Component) inputPortSubject(portName, fallback string) string {
	if c.config.Ports != nil {
		for _, p := range c.config.Ports.Inputs {
			if p.Name == portName {
				return p.Subject
			}
		}
	}
	return fallback
}

// inputPortStream returns the configured stream name for the named input port,
// falling back to the provided default when no matching port is found or the
// port declares no stream. Mirrors inputPortSubject so subscriptions can pull
// both subject and stream from the same port definition.
func (c *Component) inputPortStream(portName, fallback string) string {
	if c.config.Ports != nil {
		for _, p := range c.config.Ports.Inputs {
			if p.Name == portName && p.StreamName != "" {
				return p.StreamName
			}
		}
	}
	return fallback
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

// outputPortDefs returns the output port definitions slice, or nil when Ports is unset.
// This lets ResolveSubject fall back gracefully to portName + "." + suffix.
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
