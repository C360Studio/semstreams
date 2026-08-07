package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/c360studio/semstreams/types"
)

// ErrMaxIterationsReached is the typed sentinel returned by
// HandleModelResponse when a loop's iteration counter has already met or
// exceeded its configured budget when the next model response arrives
// (gh#529). Component.handleResponseMessage matches it via errors.Is —
// never string matching — and maps it to the uniform loop-terminal
// failure reason "max_iterations", the same reason handleToolsComplete's
// tool-drain path already publishes when the budget is exhausted while
// tools are still in flight. Before this sentinel, the model-response
// path fell through to the generic "handler_error" reason, forcing
// reactive rules to string-match error text to detect budget exhaustion.
//
// Aliases agentic.ErrMaxIterationsReached — LoopEntity.IncrementIteration
// (agentic/state.go) returns that same sentinel on the tool-drain path
// (handleToolsComplete below), and the import direction only works
// agentic → agenticloop, so this package re-exports the canonical value
// instead of declaring its own distinct error. Both detection paths
// therefore compare against the exact same sentinel via errors.Is.
var ErrMaxIterationsReached = agentic.ErrMaxIterationsReached

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
	// Created is true only when HandleTask actually created a new loop.
	// False on the dedup short-circuit path (TaskMessage redelivered for
	// an already-active task). Component uses this to gate
	// recordLoopCreated so the active_loops gauge does not drift on
	// JetStream redelivery.
	Created bool
	// CompletionState contains enriched completion data for KV persistence.
	// This is populated when a loop completes and is used by component.go
	// to write to the loops bucket with key pattern COMPLETE_{loopID}.
	CompletionState *agentic.LoopCompletedEvent
	// FailureState contains enriched failure data for graph emission.
	// Populated when a loop fails, mirrors CompletionState for the failure path.
	FailureState *agentic.LoopFailedEvent
	// SyntheticDecide is populated when handleCompleteResponse detects a
	// terminal-tool-less completion AND Config.SynthesizeTerminalOnCompletion
	// is true (#133). Component reads this and routes through graphWriter
	// to stamp coordinator.next_action="needs_clarification",
	// coordinator.decision.reason="[synthetic-no-terminal] ...", and
	// coordinator.decision.synthetic="true" atomically on the loop entity.
	// Nil means either the loop terminated through a real decide call or
	// the synthesis opt-in is off; either way, no synthetic triples.
	SyntheticDecide *SyntheticDecideRequest
}

// SyntheticDecideRequest carries the data needed for graphWriter to stamp
// the synthetic decide triples (#133). LoopID identifies the loop entity;
// Reason is the model's final text content, used as the
// coordinator.decision.reason payload (prefixed by graphWriter so the
// "synthetic" provenance is impossible to miss in operator dashboards).
type SyntheticDecideRequest struct {
	LoopID string
	Reason string
}

// MessageHandler handles incoming messages and coordinates loop execution
type MessageHandler struct {
	config            Config
	loopManager       *LoopManager
	trajectoryManager *TrajectoryManager
	compactor         *Compactor
	// governanceDispatcher implements subject-mode tool-call governance
	// (ADR-039). Set via SetGovernanceDispatcher at component boot. Nil
	// is treated as disabled-mode pass-through (no governance gate).
	// Beta.70 retired the in-process ToolCallFilter wiring that
	// previously co-existed with this dispatcher; subject-mode is now
	// the sole governance path.
	governanceDispatcher GovernanceDispatcher
	modelRegistry        model.RegistryReader
	toolRegistry         component.ToolRegistryReader
	logger               *slog.Logger

	// todoReader fetches the current write_todos list for a loop entity
	// at iteration-build time (ADR-036 Stage 4). Nil disables the
	// per-iteration todo block — fragments and trajectory still work.
	// Set via SetTodoReader at component boot.
	todoReader TodoReader

	// lessonReader lists active lesson-record entities for brief-assembly
	// injection (ADR-080 push-based memory). Nil disables lesson injection
	// entirely (back-compat) — the assembled prompt is unchanged. Set via
	// SetLessonReader at component boot.
	lessonReader LessonReader

	// metrics records brief-assembly observability (lesson matched-vs-included
	// counts). Nil-safe: unset in unit tests, wired at component boot.
	metrics *loopMetrics

	// platform identifies the org/platform pair used to construct loop
	// entity IDs. Empty Org/Platform skips the todo read silently —
	// the loop entity ID is unrecoverable without it.
	platform types.PlatformMeta

	// promptRegistry is the static fragment registry seeded with
	// prompt.DefaultFragments at Component.Start. Nil means no assembler
	// step — buildInitialMessages emits only the existing
	// task.Context.Content + user-prompt pair.
	promptRegistry *prompt.Registry

	// personaFragments loads KV-backed persona overrides on demand.
	// Nil means no overrides (unit tests, or deployments without a
	// PersonaManager). When non-nil, the handler refreshes the merged
	// registry on every task so runtime persona edits (create / update
	// via the CRUD tools) take effect immediately on the next loop
	// without a component restart.
	personaFragments PersonaFragmentSource
}

// PersonaFragmentSource is the minimum surface the handler needs to pull
// KV-backed persona overrides at prompt-assembly time. persona.Manager
// satisfies it via its Fragments method; tests can stub with a trivial
// in-memory implementation. Kept as an interface here so the handler
// package doesn't carry a compile-time dependency on persona.
type PersonaFragmentSource interface {
	Fragments(ctx context.Context) ([]prompt.Fragment, error)
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

// truncateToolResult caps a tool result string at maxBytes, preserving UTF-8
// validity and appending a marker showing the original size.
func truncateToolResult(s string, maxBytes int) string {
	marker := fmt.Sprintf("\n…[truncated: %d bytes → %d]", len(s), maxBytes)
	budget := maxBytes - len(marker)
	if budget <= 0 {
		return marker
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

// resolveProvider looks up the LLM provider for a model endpoint name or a
// capability. Spawned loops carry a capability (coordinator/developer/reviewer)
// in task.Model / entity.Model, so the name is resolved to its endpoint via
// model.ResolveEndpointName first — a raw GetEndpoint on a capability misses and
// returns "" (the #584/#594 accounting/aux-path resolution class). A real
// endpoint name resolves to itself, unchanged.
func (h *MessageHandler) resolveProvider(endpointName string) string {
	if h.modelRegistry == nil || endpointName == "" {
		return ""
	}
	ep := h.modelRegistry.GetEndpoint(model.ResolveEndpointName(h.modelRegistry, endpointName))
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

// SetPromptRegistry installs the base fragment registry that composes the
// per-task system prompt. Passing nil clears the registry — callers get
// the legacy behaviour where buildInitialMessages emits only
// task.Context.Content + user prompt. The registry is expected to be
// preloaded with prompt.DefaultFragments. Product-supplied overrides
// (KV-backed personas) are merged at assembly time via the source passed
// to SetPersonaFragments.
func (h *MessageHandler) SetPromptRegistry(r *prompt.Registry) {
	h.promptRegistry = r
}

// SetPersonaFragments installs a source for KV-backed persona overrides.
// On each task the handler calls src.Fragments(ctx) and UpsertAll's the
// result onto the registry before assembly — this is what makes runtime
// persona edits take effect on the next loop without a restart. Passing
// nil disables the per-task refresh; the registry is used as-is.
func (h *MessageHandler) SetPersonaFragments(src PersonaFragmentSource) {
	h.personaFragments = src
}

// SetTodoReader installs the reader the handler uses to fetch each
// loop's write_todos list at iteration-build time (ADR-036 Stage 4).
// Passing nil disables the per-iteration todo block.
func (h *MessageHandler) SetTodoReader(r TodoReader) {
	h.todoReader = r
}

// SetLessonReader installs the reader the handler uses to list active
// lesson-record entities for brief-assembly injection (ADR-080). Passing nil
// disables lesson injection — the assembled system prompt is left unchanged.
func (h *MessageHandler) SetLessonReader(r LessonReader) {
	h.lessonReader = r
}

// SetMetrics installs the component metrics used to record brief-assembly
// observability (lesson matched-vs-included counts). Nil-safe.
func (h *MessageHandler) SetMetrics(m *loopMetrics) {
	h.metrics = m
}

// SetPlatform installs the org/platform pair used to construct loop
// entity IDs for todo reads. The same identity is also used by other
// graph paths in this package (graph_writer); typically wired once
// from the same source at component boot.
func (h *MessageHandler) SetPlatform(p types.PlatformMeta) {
	h.platform = p
}

// maybeBuildTodoMessage reads the loop's current todo list and
// formats it as a system message. Returns the zero ChatMessage on
// disabled reader, missing platform, empty list, or read failure. Persistent
// malformed records are warned; transient read failures remain debug-level.
// Either way the model omits the block and the next iteration retries.
func (h *MessageHandler) maybeBuildTodoMessage(ctx context.Context, loopID string) agentic.ChatMessage {
	if h.todoReader == nil || h.platform.Org == "" || h.platform.Platform == "" || loopID == "" {
		return agentic.ChatMessage{}
	}
	loopEntityID := agentic.LoopExecutionEntityID(h.platform.Org, h.platform.Platform, loopID)
	todos, err := h.todoReader.ReadTodos(ctx, loopEntityID)
	if err != nil {
		attrs := []any{
			slog.String("loop_id", loopID),
			slog.String("loop_entity_id", loopEntityID),
			slog.Any("error", err),
		}
		if errors.Is(err, ErrMalformedTodoRecord) {
			h.logger.Warn("malformed todo state; iteration omits the todo block", attrs...)
		} else {
			h.logger.Debug("todo read failed; iteration omits the todo block", attrs...)
		}
		return agentic.ChatMessage{}
	}
	return BuildTodoStateMessage(todos)
}

// prependIterationContext is the canonical prefix the loop attaches
// to every iteration's message slice: the iteration-budget warning
// (mandatory) followed by the optional working-list block. Both go
// at the top so the model sees them before any prior conversation.
func (h *MessageHandler) prependIterationContext(ctx context.Context, loopID string, iteration, maxIterations int, messages []agentic.ChatMessage) []agentic.ChatMessage {
	prefix := []agentic.ChatMessage{BuildIterationBudgetMessage(iteration, maxIterations)}
	if todoMsg := h.maybeBuildTodoMessage(ctx, loopID); todoMsg.Content != "" {
		prefix = append(prefix, todoMsg)
	}
	return append(prefix, messages...)
}

// lookupLoopUserID resolves the owning user for a loop, returning "" when the
// loop is unknown or has no UserID. Stamped onto ContextEvents as a provenance
// hook so a downstream consumer can scope extracted artifacts to a user without
// a separate KV round-trip. No in-tree consumer reads it today (the
// operating-model personalization layer that did was moved to semteams); kept
// as a forward hook on the generic compaction event.
func (h *MessageHandler) lookupLoopUserID(loopID string) string {
	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return ""
	}
	return entity.UserID
}

// maybeCompact checks if context compaction is needed and performs it,
// recording both a context event and a trajectory step.
func (h *MessageHandler) maybeCompact(ctx context.Context, cm *ContextManager, loopID string, iteration int, result *HandlerResult) {
	if !h.compactor.ShouldCompact(cm) {
		return
	}

	utilization := cm.Utilization()
	userID := h.lookupLoopUserID(loopID)
	result.ContextEvents = append(result.ContextEvents, agentic.ContextEvent{
		Type:        "compaction_starting",
		LoopID:      loopID,
		UserID:      userID,
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
		UserID:      userID,
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

// resolveParentLoopID reads the loop entity's ParentLoopID for the
// proposed-call payload so rule conditions can match across the
// spawn-tree from day one (ADR-039). Returns empty string when the
// loop is top-level, when the entity isn't found (race with loop
// creation, ignored intentionally — top-level is the safe default),
// or when the field is unset.
func (h *MessageHandler) resolveParentLoopID(loopID string) string {
	if h.loopManager == nil {
		return ""
	}
	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return ""
	}
	return entity.ParentLoopID
}

// SetGovernanceDispatcher installs the subject-mode tool-call
// governance dispatcher (ADR-039). The dispatcher field is not
// synchronized once the JetStream consumer callbacks start reading
// it — the setter must be called between NewComponent and Start.
//
// Nil clears any previously installed dispatcher (effectively
// reverting to disabled-mode pass-through). The Component wires this
// automatically in NewComponent based on Config.ToolCallGovernance.Mode;
// tests stub via this setter.
func (h *MessageHandler) SetGovernanceDispatcher(d GovernanceDispatcher) {
	h.governanceDispatcher = d
}

// GovernanceDispatcher returns the installed dispatcher (or nil). Used
// by the Component's verdict subscription handlers to route inbound
// verdicts via HandleVerdict.
func (h *MessageHandler) GovernanceDispatcher() GovernanceDispatcher {
	return h.governanceDispatcher
}

// SetToolRegistry installs the shared tool registry used by discoverTools.
// Production wiring lives in component.go via deps.ToolRegistry; tests use
// this to inject a per-test registry.
func (h *MessageHandler) SetToolRegistry(r component.ToolRegistryReader) {
	h.toolRegistry = r
}

// discoverTools retrieves available tool definitions from the
// shared registry plumbed through component.Dependencies.ToolRegistry.
// Returns nil when no registry is wired (e.g., tests that exercise
// the loop without an agentic-tools sibling).
func (h *MessageHandler) discoverTools() []agentic.ToolDefinition {
	if h.toolRegistry == nil {
		return nil
	}
	return h.toolRegistry.ListTools()
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

	// Set run anchor if provided (ADR-053 D7). Inherited from firing entity's RunID.
	if task.RunID != "" {
		if err := h.loopManager.SetRunID(loopID, task.RunID); err != nil {
			h.logger.Warn("failed to set run ID",
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

// resolveRunEntityID returns the full 6-part ChainExecutionEntityID for a run
// when runID is non-empty and the handler has a valid platform identity.
// Returns empty string when runID is empty, or when org/platform are missing
// (logs Warn in that case so operators notice the gap without breaking the path).
//
// Uses TryChainExecutionEntityID (never the panicking form) because this is
// called from event-construction goroutines — a panic there would silently
// kill the publish path (feedback_try_loop_entity_id_for_runtime discipline).
func (h *MessageHandler) resolveRunEntityID(runID string) string {
	if runID == "" {
		return ""
	}
	if h.platform.Org == "" || h.platform.Platform == "" {
		h.logger.Warn("resolveRunEntityID: platform identity missing, RunEntityID will be empty",
			slog.String("run_id", runID))
		return ""
	}
	id, err := agentic.TryChainExecutionEntityID(h.platform.Org, h.platform.Platform, runID)
	if err != nil {
		h.logger.Warn("resolveRunEntityID: failed to build chain execution entity ID",
			slog.String("run_id", runID),
			slog.String("org", h.platform.Org),
			slog.String("platform", h.platform.Platform),
			slog.Any("error", err))
		return ""
	}
	return id
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
		RunID:            task.RunID,
		RunEntityID:      h.resolveRunEntityID(task.RunID),
	}
	createdMsg := message.NewBaseMessage(created.Schema(), &created, "agentic-loop")
	return json.Marshal(createdMsg)
}

// buildInitialMessages constructs the initial message list for an agent request.
//
// Order: assembled system prompt (if any) → task.Context.Content (if any) →
// user prompt. The assembler consults h.promptRegistry and any live
// KV-backed overrides from h.personaFragments so runtime persona edits
// take effect on the next loop. When the registry is nil or assembly
// yields empty content, the legacy "context + prompt" pair is emitted
// unchanged.
//
// Callers that have already assembled the system prompt should use
// buildInitialMessagesWithPrompt to avoid a redundant assembly call.
func (h *MessageHandler) buildInitialMessages(ctx context.Context, task TaskMessage) []agentic.ChatMessage {
	return h.buildInitialMessagesWithPrompt(task, h.assembleSystemPrompt(ctx, task))
}

// buildInitialMessagesWithPrompt constructs the initial message list using a
// pre-assembled system prompt string. Passing an empty string skips the system
// message entirely (equivalent to a nil registry or an assembly that yields no
// content). Split from buildInitialMessages so HandleTask can assemble once,
// store in RegionSystemPrompt, and pass the same result here without a second
// assembly call.
func (h *MessageHandler) buildInitialMessagesWithPrompt(task TaskMessage, assembled string) []agentic.ChatMessage {
	var messages []agentic.ChatMessage

	if assembled != "" {
		messages = append(messages, agentic.ChatMessage{
			Role:    "system",
			Content: assembled,
		})
	}

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

// assembleSystemPrompt returns the composed system prompt for a task, or
// "" when the registry is unset or produces no content. Kept as its own
// method so the handler test can exercise the translation from
// TaskMessage to prompt.AssemblyContext without building a full loop.
//
// KV-backed personas are merged on every call via h.personaFragments —
// the additional KV list is negligible next to the LLM round-trip and
// it guarantees that `create_persona` tool calls affect the very next
// loop. Deletions take effect on registry rebuild (component restart);
// add-then-update is the common runtime edit and works in-place.
func (h *MessageHandler) assembleSystemPrompt(ctx context.Context, task TaskMessage) string {
	base := h.assembleBasePrompt(ctx, task)
	lessons := h.assembleLessonBlock(ctx, task)
	return joinPromptSections(base, lessons)
}

// assembleBasePrompt composes the persona/fragment system prompt, or "" when
// the registry is unset or produces no content. This is the pre-ADR-080
// assembleSystemPrompt body, extracted so the lesson-injection step can be
// appended around it.
func (h *MessageHandler) assembleBasePrompt(ctx context.Context, task TaskMessage) string {
	if h.promptRegistry == nil {
		return ""
	}
	if h.personaFragments != nil {
		fragments, err := h.personaFragments.Fragments(ctx)
		if err != nil {
			h.logger.Warn("failed to refresh persona overrides; falling back to registry state",
				slog.Any("error", err),
				slog.String("task_id", task.TaskID))
		} else if len(fragments) > 0 {
			h.promptRegistry.UpsertAll(fragments)
		}
	}
	actx := &prompt.AssemblyContext{
		Role:          task.Role,
		LoopID:        task.LoopID,
		Depth:         task.Depth,
		MaxDepth:      task.MaxDepth,
		Prompt:        task.Prompt,
		WorkflowSlug:  task.WorkflowSlug,
		WorkflowStep:  task.WorkflowStep,
		Tools:         toolNames(task.Tools),
		Iteration:     0,
		MaxIterations: effectiveLoopMaxIterations(task, h.config.MaxIterations),
		ParentLoopID:  task.ParentLoopID,
		Provider:      h.resolveProvider(task.Model),
	}
	return prompt.Assemble(h.promptRegistry, actx).SystemMessage
}

// assembleLessonBlock fetches active lessons, runs the deterministic matcher
// over the loop's scope, and renders a bounded, observable injection block
// (ADR-080 push-based memory). Returns "" when no reader is wired (back-compat),
// the platform is unknown, the read fails (best-effort — availability over
// consistency, mirroring the todo read), or no active lesson matches the scope.
//
// Ordering is replay-stable: the matcher sorts on the lesson's IMMUTABLE
// agent.lesson.created-at birth triple, never a KV revision or re-stamped update
// time, so the same scope over unchanged graph state — including after an
// ADR-073 from-zero reingest — injects the same lessons in the same order.
func (h *MessageHandler) assembleLessonBlock(ctx context.Context, task TaskMessage) string {
	if h.lessonReader == nil || h.platform.Org == "" || h.platform.Platform == "" {
		return ""
	}
	scope := deriveLoopScope(task)
	if len(scope.Tags) == 0 && len(scope.EntityIDs) == 0 {
		return "" // empty scope matches nothing — skip the read entirely
	}

	recordPrefix := agentic.AgentLessonRecordPrefix(h.platform.Org, h.platform.Platform)
	candidates, err := h.lessonReader.ReadLessons(ctx, recordPrefix)
	if err != nil {
		h.logger.Warn("lesson read failed; brief carries no lesson block this dispatch",
			slog.Any("error", err),
			slog.String("task_id", task.TaskID))
		return ""
	}

	res := lessonmatch.Match(candidates, scope, lessonmatch.Opts{})
	if h.metrics != nil {
		h.metrics.recordLessonInjection(res.MatchedCount, res.IncludedCount)
	}
	return renderLessonBlock(res)
}

// joinPromptSections concatenates non-empty prompt sections with a blank-line
// separator. Preserves the pre-ADR-080 contract: an empty base with an empty
// lesson block yields "" (buildInitialMessagesWithPrompt then omits the system
// message). A lesson block is delivered even when the base is empty (a
// registry-less deployment still gets lessons if a reader is wired).
func joinPromptSections(sections ...string) string {
	nonEmpty := make([]string, 0, len(sections))
	for _, s := range sections {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// toolNames projects ToolDefinition.Name out of a task's tool allowlist so
// prompt fragments that branch on available tools (none today, but
// ContentFunc can reference ctx.Tools) see a plain []string.
func toolNames(defs []agentic.ToolDefinition) []string {
	if len(defs) == 0 {
		return nil
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
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

// effectiveLoopMaxIterations resolves the per-spawn iteration budget: a
// spawn-supplied task.MaxIterations may only narrow the
// component-configured ceiling, never widen it — min(spawn, component).
// Nil task.MaxIterations uses the component default unchanged (gh#528).
//
// Shared by HandleTask (the ceiling actually enforced on the created
// LoopEntity) and assembleSystemPrompt (AssemblyContext.MaxIterations, the
// value prompt fragments see) so the two can never diverge — pre-merge
// review N1 caught assembleSystemPrompt passing the unclamped component
// ceiling instead of the effective budget.
func effectiveLoopMaxIterations(task TaskMessage, componentCeiling int) int {
	if task.MaxIterations != nil {
		return min(*task.MaxIterations, componentCeiling)
	}
	return componentCeiling
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

	effectiveMaxIterations := effectiveLoopMaxIterations(task, h.config.MaxIterations)

	if task.LoopID != "" {
		loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)
		if err != nil {
			return HandlerResult{}, err
		}
	} else {
		loopID, err = h.loopManager.CreateLoop(task.TaskID, task.Role, task.Model, effectiveMaxIterations)
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

	// Assemble the system prompt once. It is stored in RegionSystemPrompt so
	// cm.GetContext() includes it on every continuation iteration (iteration 2+).
	// Without this, the assembled persona is absent from the context manager and
	// omitted from all continuation requests — real LLMs lose their instructions
	// and the mock's marker-based preset doesn't fire after the first tool round.
	assembled := h.assembleSystemPrompt(ctx, task)

	// Add user prompt to context manager and cache for recovery.
	// If GC/repair later empties the context, we re-inject this prompt.
	cm := h.loopManager.GetContextManager(loopID)
	if assembled != "" {
		_ = cm.AddMessage(RegionSystemPrompt, agentic.ChatMessage{
			Role:    "system",
			Content: assembled,
		})
	}

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

	// Build messages for initial request with iteration budget. Pass the
	// already-assembled system prompt so we avoid assembling a second time.
	// prependIterationContext also injects the working-list block from
	// write_todos when the loop has any todos (ADR-036 Stage 4).
	messages := h.buildInitialMessagesWithPrompt(task, assembled)
	messages = h.prependIterationContext(ctx, loopID, 1, entity.MaxIterations, messages)

	// Per-task tools: if the spawner set task.Tools (including an explicit
	// empty slice from e.g. `"default_tools": []`), respect it. Only fall
	// back to global discovery when the field is truly unset (nil). This is
	// the gate that lets a flow say "this role gets no tools at all" —
	// critical for role scoping done at the product layer.
	var tools []agentic.ToolDefinition
	if task.Tools != nil {
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

	// Cache per-task response_format (ADR-034) so continuation iterations
	// reuse the structured-output constraint. Nil leaves tool-calling
	// behaviour unchanged across the loop.
	if task.ResponseFormat != nil {
		h.loopManager.CacheResponseFormat(loopID, task.ResponseFormat)
	}

	return h.buildTaskRequest(loopID, task, entity, messages, tools)
}

// buildTaskRequest creates the initial agent request, trajectory step, and loop-created
// event, returning the assembled HandlerResult.
func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {
	request := agentic.AgentRequest{
		RequestID:      h.loopManager.GenerateRequestID(loopID),
		LoopID:         loopID,
		Role:           task.Role,
		Model:          task.Model,
		Messages:       messages,
		Tools:          tools,
		ToolChoice:     task.ToolChoice,
		Timeout:        task.Timeout,
		ResponseFormat: task.ResponseFormat,
	}

	h.loopManager.TrackRequest(request.RequestID, loopID)
	h.loopManager.TrackRequestStart(request.RequestID)

	requestMsg := message.NewBaseMessage(request.Schema(), &request, "agentic-loop")
	requestData, err := json.Marshal(requestMsg)
	if err != nil {
		return HandlerResult{}, err
	}

	step := h.buildTaskTrajectoryStep(request.RequestID, task, messages)

	// Persist the task-initiation step eagerly so its `messages` survive into
	// the trajectory. Every OTHER handler in this file calls AddStep right
	// after building its step; HandleTask was the lone outlier — its
	// TrajectorySteps return field is never consumed by the component
	// (persistHandlerResult only forwards published messages + final state),
	// so the request-side audit trail was silently dropped. Caught
	// 2026-05-21 while wiring trajectory_detail:full into the UI.
	if _, addErr := h.trajectoryManager.AddStep(loopID, step); addErr != nil {
		h.logger.Warn("failed to add task trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}

	createdData, err := h.buildLoopCreatedData(loopID, task, entity)
	if err != nil {
		return HandlerResult{}, err
	}
	requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)
	if err != nil {
		return HandlerResult{}, err
	}
	createdSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.created", loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	return HandlerResult{
		LoopID:  loopID,
		State:   entity.State,
		Created: true,
		PublishedMessages: []PublishedMessage{
			{
				Subject: requestSubject,
				Data:    requestData,
			},
			{
				Subject: createdSubject,
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
			fmt.Errorf("%w: loop %s at %d/%d iterations", ErrMaxIterationsReached, loopID, entity.Iterations, entity.MaxIterations),
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
	//
	// Truncated responses are EXCLUDED from this path: a partial
	// assistant turn must not pollute the context (the next request
	// would see the orphan and either continue from a half-finished
	// draft or, with tool_calls present in the partial turn, break
	// tool_use/tool_result pair integrity). Compaction is also
	// deliberately skipped here for the truncated case —
	// handleLengthTruncation owns the compact-and-retry decision below
	// and reads pre-compaction utilization to make it; running
	// maybeCompact here first would post-compact the context before
	// the decision and make the diagnostic / retry-vs-fail branch
	// see stale numbers.
	cm := h.loopManager.GetContextManager(loopID)
	hasContent := response.Message.Content != "" || response.Message.ReasoningContent != "" || len(response.Message.ToolCalls) > 0
	if hasContent && response.Status != agentic.StatusLengthTruncated {
		_ = cm.AddMessage(RegionRecentHistory, response.Message)
		h.maybeCompact(ctx, cm, loopID, entity.Iterations, &result)
	}

	switch response.Status {
	case agentic.StatusToolCall:
		// Forward progress — clear the truncation retry counter so a
		// future truncation can self-heal once.
		h.loopManager.ResetTruncationRetry(loopID)

		if err := h.handleToolCallResponse(&result, loopID, response.Message.ToolCalls); err != nil {
			return result, err
		}

		// Edge case: if filtering (empty-name rejection or governance
		// reject) removed ALL calls, no tool.execute messages were
		// published so no tool results will arrive. Trigger tools-
		// complete immediately.
		if h.loopManager.AllToolsComplete(loopID) {
			completionResult, err := h.handleToolsComplete(ctx, loopID, entity, cm, &result)
			if err != nil {
				return completionResult, err
			}
			return completionResult, nil
		}

	case agentic.StatusComplete:
		// Forward progress — clear the truncation retry counter.
		h.loopManager.ResetTruncationRetry(loopID)

		// gh#158: fall back to ReasoningContent when Content is empty so
		// the LLM's terminal text always lands on Result + downstream
		// read_loop_result, even when the provider adapter routed it to
		// the non-canonical field.
		completionText := resolveCompletionText(response.Message)
		if err := h.handleCompleteResponse(&result, loopID, entity, completionText); err != nil {
			return result, err
		}

	case agentic.StatusLengthTruncated:
		if err := h.handleLengthTruncation(ctx, loopID, entity, cm, response, &result); err != nil {
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
// When a GovernanceDispatcher is wired (audit or enforce mode), each batch is
// proposed via the dispatcher; rejected calls receive immediate error results
// and approved calls are dispatched. Domain metadata from the task is
// propagated to each approved tool call.
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

	// Subject-mode governance (ADR-039) runs BEFORE the in-process
	// filter. When mode=disabled (the default), Propose passes through
	// unchanged. When mode=audit, every call is published to
	// agent.toolcall.proposed.* but Approved is still the full set —
	// audit doesn't gate. When mode=enforce, the dispatcher waits for
	// per-call verdicts and produces an Approved/Rejected split.
	//
	// Rejections at this layer materialize as immediate error results,
	// same shape as the in-process filter's rejection path below.
	if h.governanceDispatcher != nil {
		parentLoopID := h.resolveParentLoopID(loopID)
		govResult, gErr := h.governanceDispatcher.Propose(context.Background(), loopID, parentLoopID, toolCalls)
		if gErr != nil {
			// Governance-layer failure that isn't a per-call rejection
			// is currently reserved (ErrGovernancePublishFailed) — the
			// dispatcher returns per-call rejections instead. Surface
			// any such error to the caller for visibility.
			return gErr
		}
		for _, rejection := range govResult.Rejected {
			h.loopManager.TrackToolName(rejection.Call.ID, rejection.Call.Name)
			errResult := agentic.ToolResult{
				CallID: rejection.Call.ID,
				Name:   rejection.Call.Name,
				Error:  fmt.Sprintf("tool call rejected by governance: %s", rejection.Reason),
				LoopID: loopID,
			}
			if err := h.loopManager.StoreToolResult(loopID, errResult); err != nil {
				return err
			}
		}
		approved = govResult.Approved
	}

	// Propagate domain metadata and loop correlation onto each approved
	// call. LoopID is required by stateful tools like `decide` that need
	// to resolve the originating loop entity; without it the tool returns
	// an invalid-args error at execute time. Metadata is flow-specific
	// context the dispatcher attached to the task.
	//
	// Merge semantics: cached TaskMessage metadata fills in keys that
	// the ToolCall doesn't already carry. Any per-call pre-population
	// on ToolCall.Metadata must NOT block propagation of cached domain
	// metadata like action_allowlist / related_loops / trace_id —
	// pre-fix the guard was `len(approved[i].Metadata) == 0` and any
	// non-empty pre-population silently dropped the entire cached map.
	// (The original failure case was the Gemini-wire backend writing a
	// thought signature here; ADR-051 moved that carrier off Metadata
	// onto ChatMessage.ReasoningRecords, but the merge invariant guards
	// every future case of pre-populated ToolCall.Metadata.)
	//
	// Metadata["loop_id"] is intentionally NOT stamped here — the
	// canonical stamp lives in dispatchToolCall so every dispatch path
	// (this main path, approval re-dispatch, and queue dequeue) gets
	// it consistently. Stamping in two places would risk drift; only
	// the typed LoopID field is written upstream so the cached domain
	// metadata stays untouched until dispatch.
	metadata := h.loopManager.GetCachedMetadata(loopID)
	for i := range approved {
		if len(metadata) > 0 {
			if approved[i].Metadata == nil {
				approved[i].Metadata = make(map[string]any, len(metadata))
			}
			for k, v := range metadata {
				if _, exists := approved[i].Metadata[k]; !exists {
					approved[i].Metadata[k] = v
				}
			}
		}
		if approved[i].LoopID == "" {
			approved[i].LoopID = loopID
		}
	}

	// Serial dispatch with synth-on-failure fallback. Try each approved
	// call in order; emit a synth-result for any whose dispatch fails
	// and continue to the next. The first successful dispatch claims
	// the in-flight slot; remaining calls are queued for serial
	// dispatch via HandleToolResult's dequeue path.
	//
	// Mode (a) of the orphan-tool-call recovery: if all dispatches
	// fail, every approved call gets a synth-result, the queue stays
	// empty, AllToolsComplete is true, and the caller's edge-case
	// branch (HandleModelResponse handling tool_call) routes through
	// to handleToolsComplete with a clean tool-pair set instead of
	// failing terminal with orphans.
	idx := 0
	for idx < len(approved) {
		dispatched, storeErr := h.tryDispatchOrSynthesize(result, loopID, approved[idx])
		if storeErr != nil {
			return storeErr
		}
		idx++
		if dispatched {
			// Queue any remaining calls for serial dispatch by
			// HandleToolResult's dequeue path.
			if idx < len(approved) {
				h.loopManager.QueueToolCalls(loopID, approved[idx:])
			}
			break
		}
		// Synth-result emitted; loop to try the next approved call.
	}

	result.PendingTools = h.loopManager.GetPendingTools(loopID)
	return nil
}

// synthesizeToolFailure stores a synthetic failure result for a single
// tool call so the assistant message's tool_calls field has a matching
// tool_result on the next agent.request. Mirrors the existing pattern
// used for empty-name calls (handleToolCallResponse:813), filter
// rejections (:840), and explicit approval rejections (beta.19's
// approval_response_handler.go:122). Reason is the diagnostic surfaced
// to the model so it can decide how to recover.
//
// Idempotent at the loopManager.StoreToolResult layer — duplicate
// call_ids dedupe naturally.
func (h *MessageHandler) synthesizeToolFailure(loopID, callID, name, reason string) error {
	if name == "" {
		// Best-effort recovery — the call may have been registered via
		// TrackToolName at dispatch time even if the dispatch later
		// failed. Falls back to a sentinel only if no name was tracked.
		if tracked := h.loopManager.GetToolName(callID); tracked != "" {
			name = tracked
		} else {
			name = "unknown_tool"
		}
	}
	synth := agentic.ToolResult{
		CallID: callID,
		Name:   name,
		Error:  reason,
		LoopID: loopID,
	}
	return h.loopManager.StoreToolResult(loopID, synth)
}

// drainPendingToolFailures emits a synthetic failure result for every
// currently-pending tool call on the loop, with the supplied reason as
// the diagnostic. Used by terminal-transition paths (failLoop,
// max-iterations, cancel signal) so an interrupted loop's KV-persisted
// context doesn't carry orphan tool_calls that would 400 the model API
// if the loop is later restored or its state is replayed.
//
// No-op when no pending tools exist. Best-effort: errors from
// individual StoreToolResult calls are logged but don't stop the drain
// — partial cleanup beats no cleanup.
//
// Concurrency note: callers run on the loop's owning goroutine. A
// concurrent real result on a different goroutine could overwrite a
// just-written synth via StoreToolResult's CallID-keyed map (state.go
// PendingToolResults), or be overwritten by it depending on
// interleaving. The race is benign because the loop is transitioning
// to terminal — HandleToolResult's AllToolsComplete branch is gated
// on !entity.State.IsTerminal() at handlers.go:1411, so neither write
// triggers a new agent.request regardless of which won.
func (h *MessageHandler) drainPendingToolFailures(loopID, reason string) {
	// Drop any queued-but-not-yet-dispatched calls unconditionally —
	// they'd never get a real result on a terminating loop, and
	// leaving them queued risks a future dispatch attempt against a
	// terminal loop. Cheap when the queue is empty (single map lookup).
	h.loopManager.ClearQueuedTools(loopID)

	pending := h.loopManager.GetPendingTools(loopID)
	if len(pending) == 0 {
		return
	}
	h.logger.Info("draining pending tool calls with synthetic failures",
		slog.String("loop_id", loopID),
		slog.Int("count", len(pending)),
		slog.String("reason", reason))
	for _, callID := range pending {
		if err := h.synthesizeToolFailure(loopID, callID, "", reason); err != nil {
			h.logger.Warn("failed to store synthetic failure during drain",
				slog.String("loop_id", loopID),
				slog.String("call_id", callID),
				slog.String("error", err.Error()))
		}
		// Remove from pending so a late-arriving real result is dropped
		// silently (the synth-result already filled the slot).
		_ = h.loopManager.RemovePendingTool(loopID, callID)
	}
}

// tryDispatchOrSynthesize attempts to dispatch a tool call. On
// dispatch failure, emits a synthetic failure result so the
// assistant tool_call has a matching result on the next agent.request,
// and returns dispatched=false so the caller can try the next queued
// call instead of returning the original error and dying.
//
// Mode (a) of the orphan-tool-call recovery work. dispatchToolCall's
// real failure modes are: AddPendingTool returning loop-not-found
// (the loop isn't tracked, e.g., racing a cancel), and json.Marshal
// failing on un-marshalable arguments (a chan or func value sneaking
// through the model's tool_call). Both manifest pre-publish — the
// "NATS publish error" path doesn't exist here because the
// publication is queued via result.PublishedMessages and serviced
// downstream, not synchronously.
func (h *MessageHandler) tryDispatchOrSynthesize(result *HandlerResult, loopID string, tc agentic.ToolCall) (dispatched bool, storeErr error) {
	err := h.dispatchToolCall(result, loopID, tc)
	if err == nil {
		return true, nil
	}
	h.logger.Warn("tool dispatch failed; emitting synthetic failure result",
		slog.String("loop_id", loopID),
		slog.String("call_id", tc.ID),
		slog.String("tool_name", tc.Name),
		slog.String("error", err.Error()))
	// Best-effort cleanup of any partial pending bookkeeping —
	// dispatchToolCall registers pending before the publish, so a
	// publish-failure leaves a phantom pending entry that the
	// drain helper would later see.
	_ = h.loopManager.RemovePendingTool(loopID, tc.ID)
	reason := fmt.Sprintf("tool dispatch failed: %s", err.Error())
	return false, h.synthesizeToolFailure(loopID, tc.ID, tc.Name, reason)
}

// dispatchedFromQueue drains the loop's queued tool calls until one
// dispatches successfully or the queue is empty. Synth-result
// recovery (mode a) absorbs individual dispatch failures so a string
// of bad calls doesn't drop the loop into a terminal state with
// orphan tool_calls. Returns dispatched=true on the first successful
// publish (caller should return early to await the result), false
// when the queue drained without a success (caller should fall
// through to AllToolsComplete).
func (h *MessageHandler) dispatchedFromQueue(result *HandlerResult, loopID string) (dispatched bool, storeErr error) {
	// Defensive cap: queue length at entry. DequeueToolCall is the only
	// queue-shrinking op the loop runs, so under any sane LoopManager
	// state the queue is bounded by entry-time length. The cap stops
	// a future bug in DequeueToolCall (e.g., one that returned ok=true
	// without consuming) from infinite-looping the dispatch path.
	maxIter := len(h.loopManager.GetPendingTools(loopID)) + 64
	for i := 0; i < maxIter; i++ {
		next, ok := h.loopManager.DequeueToolCall(loopID)
		if !ok {
			return false, nil
		}
		ok, sErr := h.tryDispatchOrSynthesize(result, loopID, next)
		if sErr != nil {
			return false, sErr
		}
		if ok {
			return true, nil
		}
		// Synth-result emitted for this call; loop to try the next.
	}
	h.logger.Warn("dispatchedFromQueue iteration cap hit; queue may have leaked",
		slog.String("loop_id", loopID),
		slog.Int("max_iter", maxIter))
	return false, nil
}

// dispatchToolCall publishes a single tool call for execution and registers
// all tracking metadata (pending tools, call-to-loop mapping, timing).
//
// Stamps loopID onto the published ToolCall in two places before
// marshalling so downstream executors can route per-loop:
//
//   - tc.LoopID — typed top-level field, the canonical contract.
//   - tc.Metadata["loop_id"] — the legacy soft-fallback the
//     processor/agentic-tools/executors/bash.go sandbox path reads to
//     map a call to a worktree taskID. Without this, every sandbox-
//     routed bash call landed on taskID="default", causing concurrent
//     chains to collide on a single workspace and verify_clean to
//     report cross-chain pollution. semteams reproduced the wedge
//     against beta.37; cmd/semteams/sandbox/integration_test.go had
//     to manually populate Metadata["task_id"] to get past 404s.
//
// Both loop_id writes are conditional ("don't clobber") so an LLM or
// upstream caller that explicitly set either field keeps its value. New
// callers should not need to set either — dispatch propagates loopID
// naturally.
//
// When the loop belongs to a run (ADR-053), dispatch additionally stamps
// the run anchor onto tc.Metadata so tool executors read the run/chain
// identity directly rather than walking ancestry (issue #250):
//
//   - tc.Metadata[MetadataKeyRunID] — the bare run loop-id.
//   - tc.Metadata[MetadataKeyRunEntityID] — the resolved 6-part chain
//     execution entity ID (present only when a platform identity is set).
//
// Unlike the loop_id writes these are authoritative (overwrite): the run
// anchor is a framework fact derived from the loop's typed RunID, not a
// caller-routable hint. Both keys stay absent for a standalone loop.
//
// Also stamps tc.Metadata[MetadataKeyAdvertisedTools] (gh#551) — the names of
// the loop's cached tool definitions — authoritatively (overwrite), so the
// tool executor enforces the loop's advertised tool set on every dispatch
// path. Absent when the loop has no cached tools.
func (h *MessageHandler) dispatchToolCall(result *HandlerResult, loopID string, tc agentic.ToolCall) error {
	if err := h.loopManager.AddPendingTool(loopID, tc.ID); err != nil {
		return err
	}
	h.loopManager.TrackToolCall(tc.ID, loopID)
	h.loopManager.TrackToolName(tc.ID, tc.Name)
	h.loopManager.TrackToolArguments(tc.ID, tc.Arguments)
	h.loopManager.TrackToolStart(tc.ID)

	if tc.LoopID == "" {
		tc.LoopID = loopID
	}
	if tc.Metadata == nil {
		tc.Metadata = map[string]any{}
	}
	if _, ok := tc.Metadata["loop_id"]; !ok {
		tc.Metadata["loop_id"] = loopID
	}

	// Stamp the loop's run anchor (ADR-053 D7/D8) so tool executors read
	// the run/chain identity directly instead of walking agent.loop.parent
	// ancestry from LoopID back to the chain root (issue #250, semteams
	// ADR-053 Phase 5). Only stamped when the loop belongs to a run; a
	// standalone loop leaves both keys absent (back-compat).
	//
	// Authoritative, unlike the loop_id soft-fallback above: the run anchor
	// is a framework fact derived from the loop's typed RunID, so dispatch
	// overwrites rather than yielding to any caller-supplied value.
	if runID := h.loopManager.GetRunID(loopID); runID != "" {
		tc.Metadata[agentic.MetadataKeyRunID] = runID
		if runEntityID := h.resolveRunEntityID(runID); runEntityID != "" {
			tc.Metadata[agentic.MetadataKeyRunEntityID] = runEntityID
		}
	}

	// Stamp task-scoped ENFORCEMENT metadata (ADR-067) authoritatively from the
	// loop's cached task metadata, so it reaches EVERY dispatch path — this main
	// path, approval re-dispatch (which rebuilds a bare ToolCall,
	// approval_response_handler.go), and queue dequeue — not just the fill-only
	// merge in handleToolCallResponse. These are framework enforcement facts (the
	// read-only execution policy; the decide action allowlist), so dispatch
	// OVERWRITES rather than yielding: a fill-only merge would let a stray
	// pre-existing value defeat the control. Absent keys stay absent (back-compat).
	if cached := h.loopManager.GetCachedMetadata(loopID); len(cached) > 0 {
		for _, key := range agentic.DispatchEnforcedMetadataKeys {
			if v, ok := cached[key]; ok {
				tc.Metadata[key] = v
			}
		}
	}

	// Stamp the loop's ADVERTISED tool set (gh#551) authoritatively from the
	// tools CACHE — not from cached task metadata, hence not a
	// DispatchEnforcedMetadataKeys member — so the tool executor can enforce
	// advertise-and-enforce per loop. OVERWRITE, exactly like the RunID stamp
	// above: the advertised set is a framework enforcement fact and a
	// caller/model-supplied value must never widen it. Living at this shared
	// seam it reaches every dispatch path (main, approval re-dispatch, queue
	// dequeue). Absent when the loop has no cached tools (back-compat: loops
	// spawned without an advertised set stay unrestricted at the executor).
	if tools := h.loopManager.GetCachedTools(loopID); len(tools) > 0 {
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
		}
		tc.Metadata[agentic.MetadataKeyAdvertisedTools] = names
	} else {
		// No cached set: drop any stale caller-supplied value so absent-key
		// semantics hold end-to-end (a leftover key could only spuriously
		// reject, never widen — but the executor's fail-closed empty check
		// would turn a malformed leftover into a hard reject).
		delete(tc.Metadata, agentic.MetadataKeyAdvertisedTools)
	}

	// Stamp the emitting loop's ROLE (agentic.MetadataKeyAgentRole) authoritatively
	// from the loop entity, so tool executors can DERIVE role attribution (e.g.
	// emit_lesson's agent.lesson.observed-role) without the model supplying a
	// spoofable identity argument (ADR-080: attribution is derived, not supplied).
	// OVERWRITE when the loop has a role, DELETE when roleless — exactly like the
	// run anchor above: the role is a framework fact, so a caller/model-injected
	// value must never survive. Living at this shared seam it reaches every
	// dispatch path (main, approval re-dispatch, queue dequeue).
	if role := h.loopManager.GetRole(loopID); role != "" {
		tc.Metadata[agentic.MetadataKeyAgentRole] = role
	} else {
		delete(tc.Metadata, agentic.MetadataKeyAgentRole)
	}

	toolMsg := message.NewBaseMessage(tc.Schema(), &tc, "agentic-loop")
	toolData, err := json.Marshal(toolMsg)
	if err != nil {
		return err
	}
	toolSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "tool.execute", tc.Name)
	if err != nil {
		return err
	}
	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: toolSubject,
		Data:    toolData,
	})
	return nil
}

// handleLengthTruncation is the StatusLengthTruncated arm of
// HandleModelResponse. It branches on context utilization and the
// per-loop retry counter:
//
//   - Utilization < CompactThreshold OR already retried this loop:
//     fail with OutcomeTruncated and a diagnostic message that names
//     the actual numbers (utilization, model_limit, completion_tokens,
//     compaction_attempted boolean). Operator reads the message and
//     decides: switch models, raise max_tokens, or tune compaction.
//   - Utilization >= CompactThreshold AND first retry: call the
//     compactor inline, capture pre/post utilization, append a
//     compaction_retry ContextEvent for trajectory visibility, and
//     emit a fresh agent.request from the now-smaller context. Do NOT
//     fail and do NOT increment the loop's iteration counter — this is
//     within-iteration recovery. The per-loop retry counter caps the
//     budget at exactly one self-heal attempt; a second consecutive
//     truncation falls through to the failure branch above.
func (h *MessageHandler) handleLengthTruncation(ctx context.Context, loopID string, entity agentic.LoopEntity, cm *ContextManager, response agentic.AgentResponse, result *HandlerResult) error {
	preUtilization := cm.Utilization()
	completionTokens := response.TokenUsage.CompletionTokens
	modelLimit := cm.ModelLimit()

	// Branch 1: structurally can't recover. Either we already tried
	// (the prior retry hit the same wall) or there's nothing for the
	// compactor to compact away.
	retryCount := h.loopManager.IncrementTruncationRetry(loopID)
	canRetry := preUtilization >= h.config.Context.CompactThreshold && retryCount == 1

	if !canRetry {
		compactionAttempted := retryCount > 1 // we got here through a retry that also truncated
		message := h.buildTruncationDiagnostic(modelLimit, completionTokens, preUtilization, 0, compactionAttempted)

		h.logger.Warn("model response truncated, failing loop",
			slog.String("loop_id", loopID),
			slog.String("finish_reason", response.FinishReason),
			slog.Int("completion_tokens", completionTokens),
			slog.Float64("pre_compact_utilization", preUtilization),
			slog.Int("model_limit", modelLimit),
			slog.Bool("compaction_attempted", compactionAttempted),
			slog.Int("retry_count", retryCount))

		// Reset so a parent retry (new loop) starts fresh.
		h.loopManager.ResetTruncationRetry(loopID)
		return h.failLoop(result, loopID, agentic.OutcomeTruncated, "length_truncated", message)
	}

	// Branch 2: high utilization, first retry. Compact and re-request.
	h.logger.Info("length-truncated response triggered compaction retry",
		slog.String("loop_id", loopID),
		slog.Float64("pre_compact_utilization", preUtilization),
		slog.Int("completion_tokens", completionTokens),
		slog.Int("model_limit", modelLimit))

	compactStart := time.Now()
	compactResult, compactErr := h.compactor.Compact(ctx, cm)
	if compactErr != nil {
		// Compaction failed — fall through to the failure branch with a
		// diagnostic that names the failure. Don't burn another retry
		// attempt; the parent decides whether a fresh-loop retry helps.
		h.loopManager.ResetTruncationRetry(loopID)
		message := fmt.Sprintf("truncated and compaction failed (model_limit=%d, utilization=%.0f%%, completion_tokens=%d, compactor_error=%s) — try a larger model or tune CompactThreshold/HeadroomTokens",
			modelLimit, preUtilization*100, completionTokens, compactErr.Error())
		return h.failLoop(result, loopID, agentic.OutcomeTruncated, "length_truncated", message)
	}
	compactDuration := time.Since(compactStart).Milliseconds()
	postUtilization := cm.Utilization()
	tokensSaved := compactResult.EvictedTokens - compactResult.NewTokens

	// Trajectory + ContextEvent for observability. Step type is
	// distinct from the routine-pre-iteration "context_compaction" so
	// operators reading a trajectory can tell retry-driven compaction
	// from threshold-driven compaction at a glance.
	result.ContextEvents = append(result.ContextEvents, agentic.ContextEvent{
		Type:        agentic.ContextEventCompactionRetry,
		LoopID:      loopID,
		UserID:      h.lookupLoopUserID(loopID),
		Iteration:   entity.Iterations,
		Utilization: preUtilization,
		TokensSaved: tokensSaved,
		Summary:     compactResult.Summary,
	})
	retryStep := agentic.TrajectoryStep{
		Timestamp:   time.Now(),
		StepType:    "context_compaction_retry",
		Response:    compactResult.Summary,
		TokensIn:    compactResult.EvictedTokens,
		TokensOut:   compactResult.NewTokens,
		Model:       compactResult.Model,
		Utilization: preUtilization,
		Duration:    compactDuration,
	}
	result.TrajectorySteps = append(result.TrajectorySteps, retryStep)
	if _, addErr := h.trajectoryManager.AddStep(loopID, retryStep); addErr != nil {
		h.logger.Warn("failed to add compaction-retry trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}

	// Build and emit the retry agent.request from the freshly-compacted
	// context. We do NOT increment the iteration counter — this is a
	// within-iteration self-heal. The truncationRetryAttempts counter
	// (now ==1) prevents a second truncation from re-entering this
	// branch.
	return h.emitRetryRequest(ctx, loopID, entity, cm, result, postUtilization)
}

// emitRetryRequest builds a fresh agent.request from the current
// context (post-compaction) and appends it to result.PublishedMessages.
// Mirrors the request-construction tail of handleToolsComplete but
// without the tool-result drain or iteration increment.
func (h *MessageHandler) emitRetryRequest(ctx context.Context, loopID string, entity agentic.LoopEntity, cm *ContextManager, result *HandlerResult, postUtilization float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Pre-request integrity audit — same defensive sweep as
	// handleToolsComplete. The retry-after-truncation path doesn't
	// add new tool results (the truncation drops them) but the
	// existing context may carry orphans from an earlier failed
	// iteration that compaction didn't catch.
	if removed := cm.RepairToolPairs(); removed > 0 {
		h.logger.Warn("retry-request tool-pair audit removed orphan messages",
			slog.String("loop_id", loopID),
			slog.Int("removed", removed))
	}

	messages := cm.GetContext()
	if !hasUserOrAssistantMessage(messages) {
		messages = h.recoverEmptyContext(loopID, cm, entity.Iterations, 0)
	}

	messages = h.prependIterationContext(ctx, loopID, entity.Iterations, entity.MaxIterations, messages)

	tools := h.loopManager.GetCachedTools(loopID)
	toolChoice := h.loopManager.GetCachedToolChoice(loopID)

	request := agentic.AgentRequest{
		RequestID:      h.loopManager.GenerateRequestID(loopID),
		LoopID:         loopID,
		Role:           entity.Role,
		Model:          entity.Model,
		Messages:       messages,
		Tools:          tools,
		ToolChoice:     toolChoice,
		Timeout:        h.loopManager.GetCachedRequestTimeout(loopID),
		ResponseFormat: h.loopManager.GetCachedResponseFormat(loopID),
	}

	h.loopManager.TrackRequest(request.RequestID, loopID)
	h.loopManager.TrackRequestStart(request.RequestID)

	requestMsg := message.NewBaseMessage(request.Schema(), &request, "agentic-loop")
	requestData, err := json.Marshal(requestMsg)
	if err != nil {
		return err
	}
	requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)
	if err != nil {
		return err
	}
	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: requestSubject,
		Data:    requestData,
	})

	h.logger.Info("emitted compaction-retry agent.request",
		slog.String("loop_id", loopID),
		slog.String("request_id", request.RequestID),
		slog.Float64("post_compact_utilization", postUtilization))
	return nil
}

// buildTruncationDiagnostic formats the operator-facing message that
// accompanies a OutcomeTruncated failure. compactionAttempted=false +
// low pre_utilization means "output budget too small for task";
// compactionAttempted=true means "compaction did its job but the
// response still doesn't fit." Both messages are actionable: the
// operator reads the numbers and chooses to switch models, raise
// max_tokens, or tune CompactThreshold/HeadroomTokens.
func (h *MessageHandler) buildTruncationDiagnostic(modelLimit, completionTokens int, preUtilization, postUtilization float64, compactionAttempted bool) string {
	if compactionAttempted {
		return fmt.Sprintf("truncated after compaction (model_limit=%d, pre-compact utilization=%.0f%%, post-compact utilization=%.0f%%, completion_tokens=%d) — response exceeds available budget even after freeing context; try a larger model or raise max_tokens",
			modelLimit, preUtilization*100, postUtilization*100, completionTokens)
	}
	return fmt.Sprintf("truncated at %.0f%% utilization without compaction attempted (model_limit=%d, completion_tokens=%d) — output budget too small for task; raise max_tokens or use a model with larger output capacity",
		preUtilization*100, modelLimit, completionTokens)
}

// failLoop transitions a loop to failed state, records completion data, and builds failure events.
func (h *MessageHandler) failLoop(result *HandlerResult, loopID, outcome, reason, errorMsg string) error {
	// Drain any pending tool calls into synth-results before transitioning
	// to terminal state. Modes (c) and (d) of orphan-tool-call recovery —
	// late-arriving results dropped silently; KV-restored loops won't
	// carry orphan tool_calls. No-op when no pending tools exist.
	h.drainPendingToolFailures(loopID, fmt.Sprintf("loop failed: %s", reason))

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

// hasDecideToolCall scans a trajectory's steps for any tool_call to
// `decide`. Narrow by design — `submit_work` and other terminal tools
// also drive completion via StopLoop, but synthesis is specifically the
// coordinator-decide recovery path (the wedge shape is "coordinator
// completed without emitting a coordinator.next_action triple"). Used
// by the terminal-tool-less synthesis path (#133): when this returns
// false on a completed loop and the synthesis trigger fires (see
// shouldSynthesizeDecide), the framework synthesizes a
// needs_clarification decide. Broadening to a wider terminal-tool set
// is a separate decision (the rule pack opting into synthesis should
// not implicitly assume any tool that calls StopLoop is a
// decide-equivalent — a submit_work or future emit_diagnosis terminal
// would be a content-bearing terminal, not a routing decision, and
// would not want a synth decide overwriting its successful completion).
func hasDecideToolCall(steps []agentic.TrajectoryStep) bool {
	for _, s := range steps {
		if s.StepType == "tool_call" && s.ToolName == "decide" {
			return true
		}
	}
	return false
}

// decideToolAvailable reports whether `decide` is in the loop's
// allowed tool set. When true, the rule pack explicitly granted the
// loop the routing-decision primitive — a text-only completion under
// that condition is a wedge shape regardless of the operator's
// Config.SynthesizeTerminalOnCompletion opt-in (gh#158).
func decideToolAvailable(tools []agentic.ToolDefinition) bool {
	for _, td := range tools {
		if td.Name == "decide" {
			return true
		}
	}
	return false
}

// resolveCompletionText picks the LLM's terminal output for surfacing
// into Result + the synthesized decide reason. Provider adapters route
// model output into either Content (canonical) or ReasoningContent
// (thinking models / some Gemini paths). gh#158 surfaced two-of-three
// parallel gather loops stranding 219 tokens of model output because
// the framework only read Content. Fall back to ReasoningContent so
// the text always lands on the read path even when the adapter chose
// the non-canonical field.
func resolveCompletionText(msg agentic.ChatMessage) string {
	if msg.Content != "" {
		return msg.Content
	}
	return msg.ReasoningContent
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
		Prompt:       h.loopManager.GetTaskPrompt(loopID),
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
		RunID:       entity.RunID,
		RunEntityID: h.resolveRunEntityID(entity.RunID),
	}

	// Pull token totals from trajectory for cost tracking. Also doubles as
	// the source-of-truth for the #133 terminal-tool-less check below —
	// scanning the same trajectory snapshot here avoids a second
	// trajectoryManager.GetTrajectory round-trip and keeps the cost +
	// synthesis decisions consistent on the same step set.
	var trajectorySteps []agentic.TrajectoryStep
	if traj, trajErr := h.trajectoryManager.GetTrajectory(loopID); trajErr == nil {
		completion.TokensIn = traj.TotalTokensIn
		completion.TokensOut = traj.TotalTokensOut
		trajectorySteps = traj.Steps
	} else {
		h.logger.Warn("trajectory unavailable for cost tracking",
			slog.String("loop_id", loopID),
			slog.String("error", trajErr.Error()))
	}

	// Terminal-tool-less completion synthesis (#133, widened by gh#158).
	// When the loop reaches state=complete WITHOUT a `decide` tool_call
	// in the trajectory, surface a SyntheticDecideRequest on the
	// result so Component routes it through graphWriter. Two trigger
	// conditions, either is sufficient:
	//
	//  1. Config.SynthesizeTerminalOnCompletion — explicit operator
	//     opt-in. Original #133 surface.
	//  2. `decide` is in the loop's allowed tool set — the rule pack
	//     granted the routing-decision primitive but the model didn't
	//     call it. gh#158: this is a wedge shape regardless of the
	//     operator's opt-in; downstream rules expecting
	//     coordinator.next_action will block forever waiting for a
	//     decide that was never going to come.
	//
	// Cheap-model substrate recovery: small models occasionally return
	// text-only at completion despite persona prose enforcing a decide
	// call; the synth keeps downstream rules matching on
	// coordinator.next_action rather than wedging the chain.
	if !hasDecideToolCall(trajectorySteps) {
		loopTools := h.loopManager.GetCachedTools(loopID)
		triggerFlag := h.config.SynthesizeTerminalOnCompletion
		triggerToolset := decideToolAvailable(loopTools)
		if triggerFlag || triggerToolset {
			result.SyntheticDecide = &SyntheticDecideRequest{
				LoopID: loopID,
				Reason: responseContent,
			}
		}
	}

	completionMsg := message.NewBaseMessage(completion.Schema(), &completion, "agentic-loop")
	completionData, err := json.Marshal(completionMsg)
	if err != nil {
		return err
	}
	completionSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.complete", loopID)
	if err != nil {
		return err
	}
	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: completionSubject,
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

	// Truncate oversized tool results before they enter the context window.
	if h.config.ToolResultMaxBytes > 0 && len(toolResult.Content) > h.config.ToolResultMaxBytes {
		original := len(toolResult.Content)
		toolResult.Content = truncateToolResult(toolResult.Content, h.config.ToolResultMaxBytes)
		h.logger.Warn("tool result truncated",
			slog.String("loop_id", loopID),
			slog.String("tool", toolResult.Name),
			slog.Int("original_bytes", original),
			slog.Int("max_bytes", h.config.ToolResultMaxBytes))
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
	step := h.buildToolTrajectoryStep(toolResult, entity)
	result.TrajectorySteps = append(result.TrajectorySteps, step)

	// Eagerly add step to trajectory manager so the tool_call is available
	// when finalizeTrajectory snapshots the trajectory for the TTL cache.
	if _, addErr := h.trajectoryManager.AddStep(loopID, step); addErr != nil {
		h.logger.Warn("failed to add tool_call trajectory step",
			slog.String("loop_id", loopID),
			slog.String("error", addErr.Error()))
	}

	// Handle approval gating: pause on first approval_required result,
	// absorb sibling results that land afterward.
	if h.checkApprovalGate(loopID, &entity, toolResult, &result) {
		return result, nil
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

	// Serial dispatch: drain the queue until one call dispatches
	// successfully or the queue is empty. dispatchedFromQueue handles
	// the synth-on-failure recovery (mode a) so a string of dispatch
	// failures still preserves tool-pair integrity.
	if dispatched, storeErr := h.dispatchedFromQueue(&result, loopID); storeErr != nil {
		return result, storeErr
	} else if dispatched {
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

// checkApprovalGate implements the awaiting-approval branch logic
// extracted from HandleToolResult. Returns true when the caller
// should stop processing this result (either we just paused, or the
// loop is already paused and this is a sibling result). Mutates
// result.State and result.PublishedMessages when transitioning.
func (h *MessageHandler) checkApprovalGate(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult, result *HandlerResult) bool {
	// If the loop is already awaiting approval, store the result and
	// the trajectory step (done by caller) but stop here. Sibling
	// tool results from the same batch can land after we paused on
	// the first approval_required hit — they must not advance the
	// loop or trigger the next model request. The pending approval
	// handler drains PendingToolResults when the loop resumes.
	if entity.State == agentic.LoopStateAwaitingApproval {
		return true
	}
	// Approval-gated rejection: the agentic-tools approval filter
	// returned an "approval_required: ..." error. Pause the loop,
	// snapshot the call, and emit ApprovalPendingEvent so a
	// product-layer UI can surface the request.
	if !agentic.IsApprovalRequired(toolResult.Error) {
		return false
	}
	pubMsg, err := h.gateForApproval(loopID, entity, toolResult)
	if err != nil {
		h.logger.Warn("failed to gate loop for approval",
			slog.String("loop_id", loopID),
			slog.String("call_id", toolResult.CallID),
			slog.String("error", err.Error()))
	} else if pubMsg != nil {
		result.PublishedMessages = append(result.PublishedMessages, *pubMsg)
	}
	result.State = agentic.LoopStateAwaitingApproval
	return true
}

// gateForApproval transitions the loop into LoopStateAwaitingApproval,
// persists the pending call on the entity, clears any queued tool
// calls (they'll be re-deliberated by the LLM after the human
// responds), and builds the ApprovalPendingEvent for publication.
// Returns the published message (or nil if event construction fails)
// alongside any non-fatal error so the caller can decide whether to
// surface it.
func (h *MessageHandler) gateForApproval(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult) (*PublishedMessage, error) {
	toolName := h.loopManager.GetToolName(toolResult.CallID)
	if toolName == "" {
		// Fall back to the tool name on the result envelope when the
		// LoopManager cache has been cleared (e.g., process restart).
		toolName = toolResult.Name
	}
	args := h.loopManager.GetToolArguments(toolResult.CallID)

	if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {
		return nil, fmt.Errorf("begin awaiting approval: %w", err)
	}

	// Clear sibling tool calls queued behind this one. Once the human
	// responds, the LLM will get a fresh round-trip with the
	// approve/reject result and can decide whether to re-issue the
	// other calls.
	h.loopManager.ClearQueuedTools(loopID)

	if err := h.loopManager.UpdateLoop(*entity); err != nil {
		return nil, fmt.Errorf("persist awaiting-approval state: %w", err)
	}

	pending := &agentic.ApprovalPendingEvent{
		LoopID:      loopID,
		CallID:      toolResult.CallID,
		ToolName:    toolName,
		Arguments:   args,
		Reason:      toolResult.Error,
		RequestedAt: time.Now().UTC(),
		Timeout:     h.config.ApprovalTimeout(),
		TraceID:     toolResult.TraceID,
	}
	envelope := message.NewBaseMessage(pending.Schema(), pending, "agentic-loop")
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal approval pending event: %w", err)
	}
	subject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.approval_pending", loopID)
	if err != nil {
		return nil, err
	}
	return &PublishedMessage{
		Subject: subject,
		Data:    data,
	}, nil
}

// buildToolTrajectoryStep constructs the tool_call trajectory step for a
// completed tool result. ErrorKind is normalised to ToolErrorUnknown when
// the executor returned an error without classifying it, so downstream
// analytics always see a non-empty category.
func (h *MessageHandler) buildToolTrajectoryStep(toolResult agentic.ToolResult, entity agentic.LoopEntity) agentic.TrajectoryStep {
	toolStatus := "success"
	var errCategory string
	if toolResult.Error != "" {
		toolStatus = "failed"
		errCategory = string(toolResult.ErrorKind)
		if errCategory == "" {
			errCategory = string(agentic.ToolErrorUnknown)
		}
	}
	toolName := h.loopManager.GetToolName(toolResult.CallID)
	toolArgs := h.loopManager.GetToolArguments(toolResult.CallID)
	return agentic.TrajectoryStep{
		Timestamp:     time.Now(),
		StepType:      "tool_call",
		ToolName:      toolName,
		ToolArguments: toolArgs,
		ToolResult:    toolResult.Content,
		Duration:      h.computeToolDuration(toolResult.CallID),
		Provider:      h.resolveProvider(entity.Model),
		Capability:    entity.Role,
		ToolStatus:    toolStatus,
		ErrorMessage:  toolResult.Error,
		ErrorCategory: errCategory,
		// gh#146: surface external URL fetches from bash steps as a first-class
		// attribute for citation/audit/governance dashboards.
		URLsFetched: agentic.BashStepURLs(toolName, toolArgs),
	}
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
		// LoopManager.IncrementIteration can fail for two structurally
		// different reasons: budget exhaustion (agentic.ErrMaxIterationsReached,
		// returned by LoopEntity.IncrementIteration) or an unrelated
		// operational failure such as "loop not found". Only the sentinel
		// means exhaustion — anything else must propagate as an ordinary
		// handler error instead of being misreported as max_iterations
		// (pre-merge review M1).
		if !errors.Is(err, agentic.ErrMaxIterationsReached) {
			return *result, errs.Wrap(err, "agentic-loop", "handleToolsComplete", "increment iteration")
		}

		// Max iterations reached - mark as failed.
		// Drain any pending tools first so KV-persisted context for this
		// loop carries clean tool-pair structure (mode d of orphan
		// recovery — iteration limit hit while tools still in-flight).
		h.drainPendingToolFailures(loopID, "max iterations reached before tool results returned")

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

	// Pre-request integrity audit. Belt-and-suspenders for any orphan
	// tool_calls C1's synth-result wiring missed (KV-restored loops
	// with corrupt context, future failure paths added without the
	// synth-emission, manual state mutations). No-op on well-formed
	// contexts — single linear scan of recent history.
	if removed := cm.RepairToolPairs(); removed > 0 {
		h.logger.Warn("pre-request tool-pair audit removed orphan messages",
			slog.String("loop_id", loopID),
			slog.Int("removed", removed))
	}

	messages := cm.GetContext()

	// Safety net: if budget slicing emptied the context, re-inject the task prompt
	// so the model has at least one user message (Gemini rejects system-only contexts).
	if !hasUserOrAssistantMessage(messages) {
		messages = h.recoverEmptyContext(loopID, cm, newIteration, 0)
	}

	// Prepend iteration budget + working list so the model sees both
	// at the top of context.
	messages = h.prependIterationContext(ctx, loopID, newIteration, entity.MaxIterations, messages)

	// Check for cancellation before building request
	if err := ctx.Err(); err != nil {
		return *result, err
	}

	// Get cached tools and tool choice for this loop (set once at loop start)
	tools := h.loopManager.GetCachedTools(loopID)
	toolChoice := h.loopManager.GetCachedToolChoice(loopID)

	// All tools complete - send next agent request with full conversation
	request := agentic.AgentRequest{
		RequestID:      h.loopManager.GenerateRequestID(loopID),
		LoopID:         loopID,
		Role:           entity.Role,
		Model:          entity.Model,
		Messages:       messages,
		Tools:          tools,
		ToolChoice:     toolChoice,
		Timeout:        h.loopManager.GetCachedRequestTimeout(loopID),
		ResponseFormat: h.loopManager.GetCachedResponseFormat(loopID),
	}

	// Track request ID to loop ID mapping (cache for fast lookup)
	h.loopManager.TrackRequest(request.RequestID, loopID)
	h.loopManager.TrackRequestStart(request.RequestID)

	requestMsg := message.NewBaseMessage(request.Schema(), &request, "agentic-loop")
	requestData, err := json.Marshal(requestMsg)
	if err != nil {
		return *result, err
	}
	requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)
	if err != nil {
		return *result, err
	}

	result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{
		Subject: requestSubject,
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
//
// Decoration order, when ResultHint and pagination metadata are present:
//  1. base content (Content or formatted Error fallback or "(empty result)")
//  2. hint preamble prepended (decorateContentWithHint) — top of message,
//     so small/mid-tier models pick up the structured cue first
//  3. pagination continuation appended (decorateContentWithPagination) —
//     bottom of message, immediately before the next-turn boundary so
//     the model has the continuation token in close proximity to its
//     own response decision
//
// Composes with the ApprovalRequiredPrefix in-band signaling pattern
// (which fires before this function via gateForApproval in
// HandleToolResult). New executors should prefer ResultHint over
// magic-string sniffing.
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
		content = decorateContentWithHint(content, r.ResultHint)
		content = decorateContentWithPagination(content, r.Metadata)
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
		Prompt:       h.loopManager.GetTaskPrompt(loopID),
		Model:        entity.Model,
		Iterations:   entity.Iterations,
		ParentLoopID: entity.ParentLoopID,
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		FailedAt:     time.Now(),
		ChannelType:  entity.ChannelType,
		ChannelID:    entity.ChannelID,
		UserID:       entity.UserID,
		Metadata:     entity.Metadata,
		RunID:        entity.RunID,
		RunEntityID:  h.resolveRunEntityID(entity.RunID),
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
	failureSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.failed", loopID)
	if err != nil {
		return nil, nil, err
	}

	return failure, []PublishedMessage{{
		Subject: failureSubject,
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
func (h *MessageHandler) GetContextManager(loopID string) *ContextManager {
	return h.loopManager.GetContextManager(loopID)
}
