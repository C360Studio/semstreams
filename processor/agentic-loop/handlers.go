package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Subject patterns for NATS publishing (concrete subjects, no wildcards).
const (
	subjectAgentRequest  = "agent.request"
	subjectAgentCreated  = "agent.created"
	subjectAgentFailed   = "agent.failed"
	subjectToolExecute   = "tool.execute"
	subjectAgentComplete = "agent.complete"
)

// TaskMessage is an alias for agentic.TaskMessage for backward compatibility.
// This allows existing code to use agenticloop.TaskMessage without modification.
type TaskMessage = agentic.TaskMessage

// PublishedMessage represents a message published to NATS
type PublishedMessage struct {
	Subject string
	Data    []byte
}

// HandlerResult contains the results of a handler operation
type HandlerResult struct {
	LoopID               string
	State                agentic.LoopState
	PublishedMessages    []PublishedMessage
	PendingTools         []string
	TrajectorySteps      []agentic.TrajectoryStep
	ContextEvents        []agentic.ContextEvent
	RetryScheduled       bool
	MaxIterationsReached bool
	// CompletionState contains enriched completion data for KV persistence.
	// This is populated when a loop completes and is used by component.go
	// to write to the loops bucket with key pattern COMPLETE_{loopID}.
	CompletionState *agentic.LoopCompletedEvent
	// FailureState contains enriched failure data for graph emission.
	// Populated when a loop fails, mirrors CompletionState for the failure path.
	FailureState *agentic.LoopFailedEvent
}

// MessageHandler handles incoming messages and coordinates loop execution
type MessageHandler struct {
	config            Config
	loopManager       *LoopManager
	trajectoryManager *TrajectoryManager
	compactor         *Compactor
	toolCallFilter    agentic.ToolCallFilter
	modelRegistry     model.RegistryReader
	logger            *slog.Logger
}

// NewMessageHandler creates a new MessageHandler
func NewMessageHandler(config Config, loopManagerOpts ...LoopManagerOption) *MessageHandler {
	loopManager := NewLoopManagerWithConfig(config.Context, loopManagerOpts...)
	return &MessageHandler{
		config:            config,
		loopManager:       loopManager,
		trajectoryManager: NewTrajectoryManager(),
		compactor:         NewCompactor(config.Context),
		logger:            slog.Default(),
	}
}

// resolveProvider looks up the LLM provider for a model endpoint name.
func (h *MessageHandler) resolveProvider(endpointName string) string {
	if h.modelRegistry == nil || endpointName == "" {
		return ""
	}
	ep := h.modelRegistry.GetEndpoint(endpointName)
	if ep == nil {
		return ""
	}
	return ep.Provider
}

// SetSummarizer injects an LLM-backed summarizer into the compactor.
// When set, context compaction generates real summaries instead of stubs.
// modelName is the resolved endpoint name reported in CompactionResult.
func (h *MessageHandler) SetSummarizer(s Summarizer, modelName string) {
	h.compactor = NewCompactor(h.config.Context, WithSummarizer(s), WithModelName(modelName), WithCompactorLogger(h.logger))
}

// maybeCompact checks if context compaction is needed and performs it,
// recording both a context event and a trajectory step.
func (h *MessageHandler) maybeCompact(ctx context.Context, cm *ContextManager, loopID string, iteration int, result *HandlerResult) {
	if !h.compactor.ShouldCompact(cm) {
		return
	}

	utilization := cm.Utilization()
	result.ContextEvents = append(result.ContextEvents, agentic.ContextEvent{
		Type:        "compaction_starting",
		LoopID:      loopID,
		Iteration:   iteration,
		Utilization: utilization,
	})

	h.logger.Info("context compaction triggered",
		slog.String("loop_id", loopID),
		slog.Float64("utilization", utilization),
		slog.Int("total_tokens", cm.TotalTokens()),
		slog.Int("model_limit", cm.ModelLimit()),
		slog.Int("headroom", cm.resolveHeadroom()))

	compactStart := time.Now()
	compactResult, compactErr := h.compactor.Compact(ctx, cm)
	if compactErr != nil {
		return
	}
	compactDuration := time.Since(compactStart).Milliseconds()

	tokensSaved := compactResult.EvictedTokens - compactResult.NewTokens
	result.ContextEvents = append(result.ContextEvents, agentic.ContextEvent{
		Type:        "compaction_complete",
		LoopID:      loopID,
		Iteration:   iteration,
		TokensSaved: tokensSaved,
		Summary:     compactResult.Summary,
	})

	// Record compaction in trajectory for observability
	compactionStep := agentic.TrajectoryStep{
		Timestamp:   time.Now(),
		StepType:    "context_compaction",
		Response:    compactResult.Summary,
		TokensIn:    compactResult.EvictedTokens,
		TokensOut:   compactResult.NewTokens,
		Model:       compactResult.Model,
		Utilization: utilization,
		Duration:    compactDuration,
	}
	result.TrajectorySteps = append(result.TrajectorySteps, compactionStep)
	if _, addErr := h.trajectoryManager.AddStep(loopID, compactionStep); addErr != nil {
		h.logger.Warn("failed to add compaction trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}
}

// SetLogger sets the logger for the handler
func (h *MessageHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// SetToolCallFilter sets a filter that intercepts tool calls before execution.
// When set, each tool call batch is passed through the filter. Rejected calls
// receive immediate error results; approved calls proceed to tool.execute.
func (h *MessageHandler) SetToolCallFilter(filter agentic.ToolCallFilter) {
	h.toolCallFilter = filter
}

// discoverTools retrieves available tool definitions from the global registry.
// This is called once per loop and cached for subsequent requests.
func (h *MessageHandler) discoverTools() []agentic.ToolDefinition {
	return agentictools.ListRegisteredTools()
}

// configureLoopMetadata sets optional metadata on a newly created loop.
// Logs warnings if any metadata configuration fails, but does not fail the loop creation.
func (h *MessageHandler) configureLoopMetadata(loopID string, task TaskMessage) {
	// Set depth tracking on the loop entity
	if task.Depth > 0 || task.MaxDepth > 0 {
		if err := h.loopManager.SetDepth(loopID, task.Depth+1, task.MaxDepth); err != nil {
			h.logger.Warn("failed to set depth",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
	}

	// Set parent loop ID if provided
	if task.ParentLoopID != "" {
		if err := h.loopManager.SetParentLoopID(loopID, task.ParentLoopID); err != nil {
			h.logger.Warn("failed to set parent loop ID",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
	}

	// Set workflow context if provided (for loops created by workflow commands)
	if task.WorkflowSlug != "" || task.WorkflowStep != "" {
		if err := h.loopManager.SetWorkflowContext(loopID, task.WorkflowSlug, task.WorkflowStep); err != nil {
			h.logger.Warn("failed to set workflow context",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
	}

	// Set user context if provided (for error notification routing)
	if task.ChannelType != "" || task.UserID != "" {
		if err := h.loopManager.SetUserContext(loopID, task.ChannelType, task.ChannelID, task.UserID); err != nil {
			h.logger.Warn("failed to set user context",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
	}

	// Set domain metadata if provided
	if len(task.Metadata) > 0 {
		if err := h.loopManager.SetMetadata(loopID, task.Metadata); err != nil {
			h.logger.Warn("failed to set metadata",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
	}

	// Set timeout if configured
	if h.config.Timeout != "" {
		timeout, parseErr := time.ParseDuration(h.config.Timeout)
		if parseErr == nil {
			if err := h.loopManager.SetTimeout(loopID, timeout); err != nil {
				h.logger.Warn("failed to set timeout",
					slog.String("loop_id", loopID),
					slog.String("error", err.Error()))
			}
		}
	}
}

// computeRequestDuration returns the elapsed milliseconds since TrackRequestStart was called.
func (h *MessageHandler) computeRequestDuration(requestID string) int64 {
	if start := h.loopManager.GetRequestStart(requestID); !start.IsZero() {
		return time.Since(start).Milliseconds()
	}
	h.logger.Warn("missing request start time for duration computation",
		slog.String("request_id", requestID))
	return 0
}

// computeToolDuration returns the elapsed milliseconds since TrackToolStart was called.
func (h *MessageHandler) computeToolDuration(callID string) int64 {
	if start := h.loopManager.GetToolStart(callID); !start.IsZero() {
		return time.Since(start).Milliseconds()
	}
	h.logger.Warn("missing tool start time for duration computation",
		slog.String("call_id", callID))
	return 0
}

// buildTaskTrajectoryStep creates the trajectory step for a HandleTask invocation.
func (h *MessageHandler) buildTaskTrajectoryStep(requestID string, task TaskMessage, messages []agentic.ChatMessage) agentic.TrajectoryStep {
	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "model_call",
		RequestID: requestID,
		Prompt:    task.Prompt,
	}
	if h.config.TrajectoryDetail == "full" {
		step.Messages = messages
		step.Model = task.Model
	}
	return step
}

// buildLoopCreatedData marshals a LoopCreatedEvent for publishing.
func (h *MessageHandler) buildLoopCreatedData(loopID string, task TaskMessage, entity agentic.LoopEntity) ([]byte, error) {
	created := agentic.LoopCreatedEvent{
		LoopID:           loopID,
		TaskID:           task.TaskID,
		Role:             task.Role,
		Model:            task.Model,
		WorkflowSlug:     task.WorkflowSlug,
		WorkflowStep:     task.WorkflowStep,
		ContextRequestID: task.ContextRequestID,
		MaxIterations:    entity.MaxIterations,
		CreatedAt:        time.Now(),
		Metadata:         task.Metadata,
	}
	createdMsg := message.NewBaseMessage(created.Schema(), &created, "agentic-loop")
	return json.Marshal(createdMsg)
}

// buildInitialMessages constructs the initial message list for an agent request.
func (h *MessageHandler) buildInitialMessages(task TaskMessage) []agentic.ChatMessage {
	var messages []agentic.ChatMessage

	// If embedded context exists, include it as system message first
	if task.Context != nil && task.Context.Content != "" {
		messages = append(messages, agentic.ChatMessage{
			Role:    "system",
			Content: fmt.Sprintf("[Context]\n%s", task.Context.Content),
		})
	}

	// Add user prompt
	messages = append(messages, agentic.ChatMessage{
		Role:    "user",
		Content: task.Prompt,
	})

	return messages
}

// BuildIterationBudgetMessage creates a system message informing the model of its
// iteration budget. Tone escalates as the budget is consumed: neutral at ≤50%,
// a nudge to wrap up at 51-75%, and urgent at >75%.
func BuildIterationBudgetMessage(iteration, maxIterations int) agentic.ChatMessage {
	pct := (iteration * 100) / maxIterations
	var content string
	switch {
	case pct > 75:
		content = fmt.Sprintf("[Iteration Budget] Iteration %d of %d (%d%% used). Budget nearly exhausted — finalize and submit your work now.", iteration, maxIterations, pct)
	case pct > 50:
		content = fmt.Sprintf("[Iteration Budget] Iteration %d of %d (%d%% used). Consider wrapping up — focus on completing the current objective.", iteration, maxIterations, pct)
	default:
		content = fmt.Sprintf("[Iteration Budget] Iteration %d of %d (%d%% used).", iteration, maxIterations, pct)
	}
	return agentic.ChatMessage{Role: "system", Content: content}
}

// HandleTask processes an incoming task message and creates a new loop
func (h *MessageHandler) HandleTask(ctx context.Context, task TaskMessage) (HandlerResult, error) {
	// Check for cancellation before starting work
	if err := ctx.Err(); err != nil {
		return HandlerResult{}, err
	}

	// Check depth limit before creating loop
	if task.MaxDepth > 0 && task.Depth >= task.MaxDepth {
		return HandlerResult{}, errs.WrapInvalid(
			fmt.Errorf("max agent depth (%d) reached, cannot spawn child agent", task.MaxDepth),
			"agentic-loop",
			"HandleTask",
			"check depth limit",
		)
	}

	// Dedup: if a non-terminal loop already exists for this task, skip.
	// This prevents duplicate LLM work when JetStream redelivers a message
	// (e.g. after a transient heartbeat failure).
	if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {
		h.logger.Warn("Duplicate task message — loop already active",
			slog.String("task_id", task.TaskID),
			slog.String("existing_loop_id", existingID))
		return HandlerResult{LoopID: existingID}, nil
	}

	// Use provided loop_id if present, otherwise create new one
	var loopID string
	var err error

	if task.LoopID != "" {
		loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, h.config.MaxIterations)
		if err != nil {
			return HandlerResult{}, err
		}
	} else {
		loopID, err = h.loopManager.CreateLoop(task.TaskID, task.Role, task.Model, h.config.MaxIterations)
		if err != nil {
			return HandlerResult{}, err
		}
	}

	// Configure optional loop metadata (depth, workflow context, user context, etc.)
	h.configureLoopMetadata(loopID, task)

	// Start trajectory
	_, err = h.trajectoryManager.StartTrajectory(loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	// Get loop entity
	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	// Add user prompt to context manager and cache for recovery.
	// If GC/repair later empties the context, we re-inject this prompt.
	cm := h.loopManager.GetContextManager(loopID)
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:    "user",
		Content: task.Prompt,
	})
	h.loopManager.CacheTaskPrompt(loopID, task.Prompt)

	// If embedded context is present, add it directly (skips hydration)
	if task.Context != nil && task.Context.Content != "" {
		_ = cm.AddMessage(RegionGraphEntities, agentic.ChatMessage{
			Role:    "system",
			Content: task.Context.Content,
		})
		h.logger.Debug("Using embedded context",
			slog.String("loop_id", loopID),
			slog.Int("token_count", task.Context.TokenCount),
			slog.Int("entity_count", len(task.Context.Entities)))
	}

	// Build messages for initial request with iteration budget
	messages := h.buildInitialMessages(task)
	budgetMsg := BuildIterationBudgetMessage(1, entity.MaxIterations)
	messages = append([]agentic.ChatMessage{budgetMsg}, messages...)

	// Use per-task tools if provided, otherwise discover from global registry
	var tools []agentic.ToolDefinition
	if len(task.Tools) > 0 {
		tools = task.Tools
	} else {
		tools = h.discoverTools()
	}
	h.loopManager.CacheTools(loopID, tools)

	// Cache tool choice strategy for all iterations in this loop
	if task.ToolChoice != nil {
		h.loopManager.CacheToolChoice(loopID, task.ToolChoice)
	}

	// Cache domain metadata for propagation to tool calls
	if len(task.Metadata) > 0 {
		h.loopManager.CacheMetadata(loopID, task.Metadata)
	}

	// Cache per-task request timeout so continuation iterations reuse it.
	if task.Timeout != "" {
		h.loopManager.CacheRequestTimeout(loopID, task.Timeout)
	}

	return h.buildTaskRequest(loopID, task, entity, messages, tools)
}

// buildTaskRequest creates the initial agent request, trajectory step, and loop-created
// event, returning the assembled HandlerResult.
func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {
	request := agentic.AgentRequest{
		RequestID:  h.loopManager.GenerateRequestID(loopID),
		LoopID:     loopID,
		Role:       task.Role,
		Model:      task.Model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: task.ToolChoice,
		Timeout:    task.Timeout,
	}

	h.loopManager.TrackRequest(request.RequestID, loopID)
	h.loopManager.TrackRequestStart(request.RequestID)

	requestMsg := message.NewBaseMessage(request.Schema(), &request, "agentic-loop")
	requestData, err := json.Marshal(requestMsg)
	if err != nil {
		return HandlerResult{}, err
	}

	step := h.buildTaskTrajectoryStep(request.RequestID, task, messages)

	createdData, err := h.buildLoopCreatedData(loopID, task, entity)
	if err != nil {
		return HandlerResult{}, err
	}

	return HandlerResult{
		LoopID: loopID,
		State:  entity.State,
		PublishedMessages: []PublishedMessage{
			{
				Subject: subjectAgentRequest + "." + loopID,
				Data:    requestData,
			},
			{
				Subject: subjectAgentCreated + "." + loopID,
				Data:    createdData,
			},
		},
		TrajectorySteps: []agentic.TrajectoryStep{step},
	}, nil
}

// HandleModelResponse processes a model response
func (h *MessageHandler) HandleModelResponse(ctx context.Context, loopID string, response agentic.AgentResponse) (HandlerResult, error) {
	// Check for cancellation before starting work
	if err := ctx.Err(); err != nil {
		return HandlerResult{}, err
	}

	// Check for timeout before processing
	if h.loopManager.IsTimedOut(loopID) {
		_ = h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed)
		if err := h.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", "loop timeout exceeded"); err != nil {
			h.logger.Warn("failed to update completion for timed out loop",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
		result := HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateFailed,
		}
		// Publish failure events for reactive workflows to observe
		if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, "timeout", "loop timeout exceeded"); fErr == nil {
			result.PublishedMessages = failMsgs
			result.FailureState = failure
		}
		return result, errs.WrapFatal(fmt.Errorf("loop timeout exceeded"), "agentic-loop", "HandleModelResponse", "check timeout")
	}

	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	// Reject responses for loops already in terminal state (defense-in-depth:
	// catches stale agent.request messages published before a parallel StopLoop
	// transition was visible).
	if entity.State.IsTerminal() {
		h.logger.Warn("ignoring model response for terminal loop",
			slog.String("loop_id", loopID),
			slog.String("state", entity.State.String()))
		return HandlerResult{LoopID: loopID, State: entity.State}, nil
	}

	// Check if max iterations reached
	if entity.Iterations >= entity.MaxIterations {
		return HandlerResult{}, errs.WrapFatal(
			fmt.Errorf("max iterations (%d) reached", entity.MaxIterations),
			"agentic-loop",
			"HandleModelResponse",
			"check max iterations",
		)
	}

	result := HandlerResult{
		LoopID:            loopID,
		State:             entity.State,
		PublishedMessages: []PublishedMessage{},
		TrajectorySteps:   []agentic.TrajectoryStep{},
		ContextEvents:     []agentic.ContextEvent{},
	}

	// Record trajectory step
	step := agentic.TrajectoryStep{
		Timestamp:  time.Now(),
		StepType:   "model_call",
		RequestID:  response.RequestID,
		Response:   response.Message.Content,
		TokensIn:   response.TokenUsage.PromptTokens,
		TokensOut:  response.TokenUsage.CompletionTokens,
		Duration:   h.computeRequestDuration(response.RequestID),
		Model:      entity.Model,
		Provider:   h.resolveProvider(entity.Model),
		Capability: entity.Role,
		RetryCount: response.RetryCount,
	}
	if h.config.TrajectoryDetail == "full" {
		step.ToolCalls = response.Message.ToolCalls
	}
	result.TrajectorySteps = append(result.TrajectorySteps, step)

	// Eagerly add step to trajectory manager so token totals are available
	// when handleCompleteResponse queries the trajectory for cost tracking.
	if _, addErr := h.trajectoryManager.AddStep(loopID, step); addErr != nil {
		h.logger.Warn("failed to add trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}

	// Add assistant response to context manager if enabled.
	// Must store tool_call messages even when content is empty — they are
	// required in the conversation history for the next model request.
	cm := h.loopManager.GetContextManager(loopID)
	hasContent := response.Message.Content != "" || response.Message.ReasoningContent != "" || len(response.Message.ToolCalls) > 0
	if hasContent {
		_ = cm.AddMessage(RegionRecentHistory, response.Message)
		h.maybeCompact(ctx, cm, loopID, entity.Iterations, &result)
	}

	switch response.Status {
	case agentic.StatusToolCall:
		if err := h.handleToolCallResponse(&result, loopID, response.Message.ToolCalls); err != nil {
			return result, err
		}

		// Edge case: if filtering (empty-name rejection or ToolCallFilter) removed ALL
		// calls, no tool.execute messages were published so no tool results will arrive.
		// Trigger tools-complete immediately.
		if h.loopManager.AllToolsComplete(loopID) {
			completionResult, err := h.handleToolsComplete(ctx, loopID, entity, cm, &result)
			if err != nil {
				return completionResult, err
			}
			return completionResult, nil
		}

	case agentic.StatusComplete:
		if err := h.handleCompleteResponse(&result, loopID, entity, response.Message.Content); err != nil {
			return result, err
		}

	case agentic.StatusLengthTruncated:
		h.logger.Warn("model response truncated (finish_reason=length), failing loop",
			slog.String("loop_id", loopID),
			slog.String("finish_reason", response.FinishReason),
			slog.Int("completion_tokens", response.TokenUsage.CompletionTokens))

		truncationError := "model response truncated: output hit max_tokens limit (finish_reason=length)"
		if err := h.failLoop(&result, loopID, agentic.OutcomeTruncated, "length_truncated", truncationError); err != nil {
			return result, err
		}

	case agentic.StatusError:
		if err := h.failLoop(&result, loopID, agentic.OutcomeFailed, "model_error", response.Error); err != nil {
			return result, err
		}
	}

	return result, nil
}

// handleToolCallResponse processes tool call responses.
// When a ToolCallFilter is set, calls are filtered before dispatch.
// Rejected calls receive immediate error results; approved calls are published.
// Domain metadata from the task is propagated to each approved tool call.
func (h *MessageHandler) handleToolCallResponse(result *HandlerResult, loopID string, toolCalls []agentic.ToolCall) error {
	// Reject tool calls with empty names — Gemini sometimes emits these as
	// acknowledgment non-responses. Store error results so the model gets a
	// nudge to call a real tool or respond with text.
	var valid []agentic.ToolCall
	for _, tc := range toolCalls {
		if tc.Name == "" {
			h.logger.Warn("dropping tool call with empty name",
				slog.String("loop_id", loopID),
				slog.String("call_id", tc.ID))
			errResult := agentic.ToolResult{
				CallID: tc.ID,
				Name:   "invalid_tool_call",
				Error:  "tool call had empty function name — call a specific tool by name or respond with text",
				LoopID: loopID,
			}
			if err := h.loopManager.StoreToolResult(loopID, errResult); err != nil {
				return err
			}
			continue
		}
		valid = append(valid, tc)
	}
	toolCalls = valid

	approved := toolCalls

	// Apply filter if configured
	if h.toolCallFilter != nil {
		filterResult, err := h.toolCallFilter.FilterToolCalls(loopID, toolCalls)
		if err != nil {
			return err
		}

		// Store immediate error results for rejected calls
		for _, rejection := range filterResult.Rejected {
			h.loopManager.TrackToolName(rejection.Call.ID, rejection.Call.Name)
			errResult := agentic.ToolResult{
				CallID: rejection.Call.ID,
				Name:   rejection.Call.Name,
				Error:  fmt.Sprintf("tool call rejected: %s", rejection.Reason),
				LoopID: loopID,
			}
			if err := h.loopManager.StoreToolResult(loopID, errResult); err != nil {
				return err
			}
		}

		approved = filterResult.Approved
	}

	// Propagate domain metadata to approved tool calls
	metadata := h.loopManager.GetCachedMetadata(loopID)
	for i := range approved {
		if len(metadata) > 0 && len(approved[i].Metadata) == 0 {
			approved[i].Metadata = metadata
		}
	}

	// Serial dispatch: execute one tool at a time. Queue remaining calls
	// for dispatch after each result arrives in HandleToolResult.
	// Parallelism is an app-level concern (parallel agents via semspec),
	// not a per-tool concern within a single agent loop.
	if len(approved) > 1 {
		h.loopManager.QueueToolCalls(loopID, approved[1:])
	}

	if len(approved) > 0 {
		if err := h.dispatchToolCall(result, loopID, approved[0]); err != nil {
			return err
		}
	}

	result.PendingTools = h.loopManager.GetPendingTools(loopID)
	return nil
}

// dispatchToolCall publishes a single tool call for execution and registers
// all tracking metadata (pending tools, call-to-loop mapping, timing).
func (h *MessageHandler) dispatchToolCall(result *HandlerResult, loopID string, tc agentic.ToolCall) error {
	if err := h.loopManager.AddPendingTool(loopID, tc.ID); err != nil {
		return err
	}
	h.loopManager.TrackToolCall(tc.ID, loopID)
	h.loopManager.TrackToolName(tc.ID, tc.Name)
	h.loopManager.TrackToolArguments(tc.ID, tc.Arguments)
	h.loopManager.TrackToolStart(tc.ID)

	toolMsg := message.NewBaseMessage(tc.Schema(), &tc, "agentic-loop")
	toolData, err := json.Marshal(toolMsg)
	if err != nil {
		return err
	}
	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: subjectToolExecute + "." + tc.Name,
		Data:    toolData,
	})
	return nil
}

// failLoop transitions a loop to failed state, records completion data, and builds failure events.
func (h *MessageHandler) failLoop(result *HandlerResult, loopID, outcome, reason, errorMsg string) error {
	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); err != nil {
		return err
	}
	result.State = agentic.LoopStateFailed

	if err := h.loopManager.UpdateCompletion(loopID, outcome, "", errorMsg); err != nil {
		h.logger.Warn("failed to update completion",
			slog.String("loop_id", loopID),
			slog.String("reason", reason),
			slog.String("error", err.Error()))
	}

	if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, reason, errorMsg); fErr == nil {
		result.PublishedMessages = failMsgs
		result.FailureState = failure
	}
	return nil
}

// handleCompleteResponse processes completion responses.
// It enriches the completion event with full context for rules-based orchestration.
func (h *MessageHandler) handleCompleteResponse(result *HandlerResult, loopID string, entity agentic.LoopEntity, responseContent string) error {
	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		return err
	}
	result.State = agentic.LoopStateComplete

	// Update entity with completion data for KV persistence (enables SSE delivery)
	if err := h.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, responseContent, ""); err != nil {
		return err
	}

	// Enriched completion event for rules-based orchestration.
	// Rules engine watches COMPLETE_* keys in KV and can trigger
	// follow-up actions (e.g., spawn editor when architect completes).
	completion := agentic.LoopCompletedEvent{
		LoopID:       loopID,
		TaskID:       entity.TaskID,
		Outcome:      agentic.OutcomeSuccess,
		Role:         entity.Role,
		Result:       responseContent,
		Model:        entity.Model,
		Iterations:   entity.Iterations,
		ParentLoopID: entity.ParentLoopID,
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		CompletedAt:  time.Now(),
		// User routing info for response delivery
		ChannelType: entity.ChannelType,
		ChannelID:   entity.ChannelID,
		UserID:      entity.UserID,
		Metadata:    entity.Metadata,
	}

	// Pull token totals from trajectory for cost tracking
	if traj, trajErr := h.trajectoryManager.GetTrajectory(loopID); trajErr == nil {
		completion.TokensIn = traj.TotalTokensIn
		completion.TokensOut = traj.TotalTokensOut
	} else {
		h.logger.Warn("trajectory unavailable for cost tracking",
			slog.String("loop_id", loopID),
			slog.String("error", trajErr.Error()))
	}

	completionMsg := message.NewBaseMessage(completion.Schema(), &completion, "agentic-loop")
	completionData, err := json.Marshal(completionMsg)
	if err != nil {
		return err
	}
	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: subjectAgentComplete + "." + loopID,
		Data:    completionData,
	})

	// Pass completion state to component for KV write.
	// Component will write this to COMPLETE_{loopID} key for rules engine.
	result.CompletionState = &completion

	return nil
}

// HandleToolResult processes a tool execution result
func (h *MessageHandler) HandleToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) (HandlerResult, error) {
	// Check for cancellation before processing
	if err := ctx.Err(); err != nil {
		return HandlerResult{}, err
	}

	// Check for timeout before processing
	if h.loopManager.IsTimedOut(loopID) {
		_ = h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed)
		if err := h.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", "loop timeout exceeded"); err != nil {
			h.logger.Warn("failed to update completion for timed out loop",
				slog.String("loop_id", loopID),
				slog.String("error", err.Error()))
		}
		result := HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateFailed,
		}
		// Publish failure events for reactive workflows to observe
		if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, "timeout", "loop timeout exceeded"); fErr == nil {
			result.PublishedMessages = failMsgs
			result.FailureState = failure
		}
		return result, errs.WrapFatal(fmt.Errorf("loop timeout exceeded"), "agentic-loop", "HandleToolResult", "check timeout")
	}

	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	// Store this tool result for accumulation
	err = h.loopManager.StoreToolResult(loopID, toolResult)
	if err != nil {
		return HandlerResult{}, err
	}

	// Remove from pending tools
	err = h.loopManager.RemovePendingTool(loopID, toolResult.CallID)
	if err != nil {
		return HandlerResult{}, err
	}

	result := HandlerResult{
		LoopID:            loopID,
		State:             entity.State,
		PendingTools:      h.loopManager.GetPendingTools(loopID),
		PublishedMessages: []PublishedMessage{},
		TrajectorySteps:   []agentic.TrajectoryStep{},
		ContextEvents:     []agentic.ContextEvent{},
	}

	// Record trajectory step
	step := agentic.TrajectoryStep{
		Timestamp:     time.Now(),
		StepType:      "tool_call",
		ToolName:      h.loopManager.GetToolName(toolResult.CallID),
		ToolArguments: h.loopManager.GetToolArguments(toolResult.CallID),
		ToolResult:    toolResult.Content,
		Duration:      h.computeToolDuration(toolResult.CallID),
		Provider:      h.resolveProvider(entity.Model),
		Capability:    entity.Role,
	}
	result.TrajectorySteps = append(result.TrajectorySteps, step)

	// Eagerly add step to trajectory manager so the tool_call is available
	// when finalizeTrajectory snapshots the trajectory for the TTL cache.
	if _, addErr := h.trajectoryManager.AddStep(loopID, step); addErr != nil {
		h.logger.Warn("failed to add tool_call trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}

	// Tool-initiated loop termination: the tool signals that no further iterations
	// are needed (e.g., a terminal action like decompose, submit, approve).
	// Content becomes the LoopCompletedEvent.Result.
	if toolResult.StopLoop {
		h.loopManager.ClearQueuedTools(loopID)
		if err := h.handleCompleteResponse(&result, loopID, entity, toolResult.Content); err != nil {
			return result, err
		}
		return result, nil
	}

	// Serial dispatch: if there are queued tool calls waiting, dispatch the
	// next one. Results accumulate in PendingToolResults until the queue is
	// drained and AllToolsComplete, then handleToolsComplete batches them
	// into context for the next model request.
	if next, ok := h.loopManager.DequeueToolCall(loopID); ok {
		if err := h.dispatchToolCall(&result, loopID, next); err != nil {
			return result, err
		}
		result.PendingTools = h.loopManager.GetPendingTools(loopID)
		return result, nil
	}

	// Context manager reference for handleToolsComplete (tool results are added
	// there in batch, not individually, to avoid double-adds with filter rejections).
	cm := h.loopManager.GetContextManager(loopID)

	// All tools dispatched and complete — proceed to next model request.
	if h.loopManager.AllToolsComplete(loopID) {
		if entity.State.IsTerminal() {
			return result, nil
		}
		return h.handleToolsComplete(ctx, loopID, entity, cm, &result)
	}

	return result, nil
}

// handleToolsComplete handles the case when all pending tools have completed
func (h *MessageHandler) handleToolsComplete(
	ctx context.Context,
	loopID string,
	entity agentic.LoopEntity,
	cm *ContextManager,
	result *HandlerResult,
) (HandlerResult, error) {
	// Check for cancellation before proceeding
	if err := ctx.Err(); err != nil {
		return *result, err
	}

	// Increment iteration counter
	err := h.loopManager.IncrementIteration(loopID)
	if err != nil {
		// Max iterations reached - mark as failed
		if transitionErr := h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); transitionErr != nil {
			return *result, errs.Wrap(transitionErr, "agentic-loop", "handleToolsComplete", fmt.Sprintf("transition loop to failed state (original error: %v)", err))
		}
		result.State = agentic.LoopStateFailed
		result.MaxIterationsReached = true

		// Update entity with completion data for KV persistence (enables SSE delivery)
		errorMsg := fmt.Sprintf("max iterations (%d) reached", entity.MaxIterations)
		if updateErr := h.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", errorMsg); updateErr != nil {
			h.logger.Warn("failed to update completion for max iterations",
				slog.String("loop_id", loopID),
				slog.String("error", updateErr.Error()))
		}

		// Publish failure events for reactive workflows to observe
		if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, "max_iterations", errorMsg); fErr == nil {
			result.PublishedMessages = failMsgs
			result.FailureState = failure
		}

		return *result, nil
	}

	// Get the new iteration count for GC
	newIteration := h.loopManager.GetCurrentIteration(loopID)

	// Get ALL accumulated tool results
	allResults := h.loopManager.GetAndClearToolResults(loopID)

	toolMessages := h.buildToolMessages(allResults)

	for _, tm := range toolMessages {
		_ = cm.AddMessage(RegionRecentHistory, tm)
	}

	messages := cm.GetContext()

	// Safety net: if budget slicing emptied the context, re-inject the task prompt
	// so the model has at least one user message (Gemini rejects system-only contexts).
	if !hasUserOrAssistantMessage(messages) {
		messages = h.recoverEmptyContext(loopID, cm, newIteration, 0)
	}

	// Prepend iteration budget so the model sees its budget early in context
	budgetMsg := BuildIterationBudgetMessage(newIteration, entity.MaxIterations)
	messages = append([]agentic.ChatMessage{budgetMsg}, messages...)

	// Check for cancellation before building request
	if err := ctx.Err(); err != nil {
		return *result, err
	}

	// Get cached tools and tool choice for this loop (set once at loop start)
	tools := h.loopManager.GetCachedTools(loopID)
	toolChoice := h.loopManager.GetCachedToolChoice(loopID)

	// All tools complete - send next agent request with full conversation
	request := agentic.AgentRequest{
		RequestID:  h.loopManager.GenerateRequestID(loopID),
		LoopID:     loopID,
		Role:       entity.Role,
		Model:      entity.Model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: toolChoice,
		Timeout:    h.loopManager.GetCachedRequestTimeout(loopID),
	}

	// Track request ID to loop ID mapping (cache for fast lookup)
	h.loopManager.TrackRequest(request.RequestID, loopID)
	h.loopManager.TrackRequestStart(request.RequestID)

	requestMsg := message.NewBaseMessage(request.Schema(), &request, "agentic-loop")
	requestData, err := json.Marshal(requestMsg)
	if err != nil {
		return *result, err
	}

	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: subjectAgentRequest + "." + loopID,
		Data:    requestData,
	})

	return *result, nil
}

// hasUserOrAssistantMessage returns true if the messages contain at least one
// user or assistant message. System-only messages are insufficient for Gemini
// which requires conversation content in the contents array.
func hasUserOrAssistantMessage(messages []agentic.ChatMessage) bool {
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			return true
		}
	}
	return false
}

// buildToolMessages converts tool results into ChatMessages for the conversation context.
// Falls back to Error when Content is empty — Gemini rejects tool result messages
// with no content (400 INVALID_ARGUMENT).
func (h *MessageHandler) buildToolMessages(results []agentic.ToolResult) []agentic.ChatMessage {
	messages := make([]agentic.ChatMessage, len(results))
	for i, r := range results {
		content := r.Content
		isError := r.Error != ""
		if content == "" && isError {
			content = fmt.Sprintf("Tool error: %s", r.Error)
		}
		if content == "" {
			content = "(empty result)"
		}
		name := r.Name
		if name == "" {
			name = h.loopManager.GetToolName(r.CallID)
		}
		messages[i] = agentic.ChatMessage{
			Role:       "tool",
			ToolCallID: r.CallID,
			Name:       name,
			Content:    content,
			IsError:    isError,
		}
	}
	return messages
}

// recoverEmptyContext handles the case where GC/repair has removed all conversation
// content. Instead of failing the loop, it re-injects the original task prompt as a
// synthetic user message so the agent can continue. Returns the recovered messages.
func (h *MessageHandler) recoverEmptyContext(loopID string, cm *ContextManager, iteration, evicted int) []agentic.ChatMessage {
	prompt := h.loopManager.GetTaskPrompt(loopID)
	if prompt == "" {
		prompt = "Continue with the task."
	}

	h.logger.Warn("context empty after GC/repair — recovering with task prompt",
		slog.String("loop_id", loopID),
		slog.Int("iteration", iteration),
		slog.Int("evicted", evicted))

	synthetic := agentic.ChatMessage{
		Role:    "user",
		Content: fmt.Sprintf("[Context recovered after tool pair cleanup]\n\nOriginal task: %s\n\nPrevious tool calls encountered errors. Please continue or try a different approach.", prompt),
	}
	_ = cm.AddMessage(RegionRecentHistory, synthetic)
	return cm.GetContext()
}

// buildFailureEvent constructs an enriched LoopFailedEvent with token counts from trajectory.
// This is the single source of truth for failure event construction — all failure paths
// (publishing, graph emission) derive from this.
func (h *MessageHandler) buildFailureEvent(loopID, reason, errorMsg string) (*agentic.LoopFailedEvent, error) {
	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return nil, err
	}

	failure := &agentic.LoopFailedEvent{
		LoopID:       loopID,
		TaskID:       entity.TaskID,
		Outcome:      agentic.OutcomeFailed,
		Reason:       reason,
		Error:        errorMsg,
		Role:         entity.Role,
		Model:        entity.Model,
		Iterations:   entity.Iterations,
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		FailedAt:     time.Now(),
		ChannelType:  entity.ChannelType,
		ChannelID:    entity.ChannelID,
		UserID:       entity.UserID,
		Metadata:     entity.Metadata,
	}

	// Pull token totals from trajectory for cost tracking
	if traj, trajErr := h.trajectoryManager.GetTrajectory(loopID); trajErr == nil {
		failure.TokensIn = traj.TotalTokensIn
		failure.TokensOut = traj.TotalTokensOut
	} else {
		h.logger.Warn("trajectory unavailable for cost tracking",
			slog.String("loop_id", loopID),
			slog.String("error", trajErr.Error()))
	}

	return failure, nil
}

// BuildFailureEvent creates a failure event (public wrapper for component.go).
func (h *MessageHandler) BuildFailureEvent(loopID, reason, errorMsg string) (*agentic.LoopFailedEvent, error) {
	return h.buildFailureEvent(loopID, reason, errorMsg)
}

// BuildFailureMessages creates a failure event and serializes it for NATS publishing.
// Returns the event (for graph emission) and published messages (for reactive workflows).
func (h *MessageHandler) BuildFailureMessages(loopID, reason, errorMsg string) (*agentic.LoopFailedEvent, []PublishedMessage, error) {
	failure, err := h.buildFailureEvent(loopID, reason, errorMsg)
	if err != nil {
		return nil, nil, err
	}

	failureMsg := message.NewBaseMessage(failure.Schema(), failure, "agentic-loop")
	data, err := json.Marshal(failureMsg)
	if err != nil {
		return nil, nil, err
	}

	return failure, []PublishedMessage{{
		Subject: subjectAgentFailed + "." + loopID,
		Data:    data,
	}}, nil
}

// GetLoop retrieves a loop entity (for testing)
func (h *MessageHandler) GetLoop(loopID string) (agentic.LoopEntity, error) {
	return h.loopManager.GetLoop(loopID)
}

// UpdateLoop updates a loop entity
func (h *MessageHandler) UpdateLoop(entity agentic.LoopEntity) error {
	return h.loopManager.UpdateLoop(entity)
}

// CancelLoop atomically cancels a loop and populates completion data.
func (h *MessageHandler) CancelLoop(loopID, cancelledBy string) (agentic.LoopEntity, error) {
	return h.loopManager.CancelLoop(loopID, cancelledBy)
}

// GetTrajectory retrieves a trajectory snapshot for a given loop ID.
func (h *MessageHandler) GetTrajectory(loopID string) (agentic.Trajectory, error) {
	return h.trajectoryManager.GetTrajectory(loopID)
}

// GetContextManager returns the ContextManager for a given loop ID.
// Used by BoidHandler to apply steering signals to context.
func (h *MessageHandler) GetContextManager(loopID string) *ContextManager {
	return h.loopManager.GetContextManager(loopID)
}
