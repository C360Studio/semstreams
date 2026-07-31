package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/builtinprojection"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// WriteTodosToolName is the name agents use to invoke the write_todos
// tool. ADR-036 §First Instance.
const WriteTodosToolName = "write_todos"

// writeTodosToolSource is the Source field on triples this tool
// writes. Lets operators distinguish todo writes from rule-driven or
// coordinator-decision triples in graph queries.
const writeTodosToolSource = "agent-write-todos"

const todoTriplesPerItem = 5

// validTodoStatuses enumerates the ADR-036 status enum. The executor
// rejects any other value with ToolErrorInvalidArgs so the LLM can
// self-correct rather than emitting silently-broken state.
var validTodoStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"completed":   {},
}

// WriteTodosExecutor implements the write_todos tool from ADR-036.
// It writes agent-private todo state onto the calling loop's entity
// — the agent is the sole writer and sole interpreter of content
// (the persona's discipline, not enforced at the executor level).
//
// Each call has full-list-replace semantics: every existing
// agent.todo.* triple on the loop entity is cleared before the new
// triples are written. The agent submits the entire desired list on
// every call.
//
// All public methods are safe for concurrent use across loops; the
// executor holds no per-call mutable state. Within a single loop,
// the agentic-loop already serialises tool calls.
type WriteTodosExecutor struct {
	writer   projection.OwnedReplacer
	platform types.PlatformMeta
	logger   *slog.Logger
	now      func() time.Time
}

// NewWriteTodosExecutor constructs the executor given a writer and
// the platform identity used to resolve loop entity IDs. The clock
// defaults to time.Now; tests inject a frozen clock via SetClock.
func NewWriteTodosExecutor(writer projection.OwnedReplacer, platform types.PlatformMeta) *WriteTodosExecutor {
	return &WriteTodosExecutor{
		writer:   writer,
		platform: platform,
		logger:   slog.Default(),
		now:      time.Now,
	}
}

// SetLogger replaces the default logger. nil-safe.
func (e *WriteTodosExecutor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
	}
}

// SetClock replaces the time source the executor stamps onto
// triples (Triple.Timestamp and the agent.todo.updated_at value).
// nil-safe — passing nil preserves the existing clock. Used by
// tests for deterministic timestamp assertions; production should
// not call this.
func (e *WriteTodosExecutor) SetClock(now func() time.Time) {
	if now != nil {
		e.now = now
	}
}

// ListTools returns the write_todos tool definition. The schema is
// strict-mode-compliant (ADR-035): every property listed in required,
// every nested object closes with additionalProperties:false, the
// status field is an enum so providers that honour function.strict
// constrain the model's sample to canonical values.
func (e *WriteTodosExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: WriteTodosToolName,
		Description: "Maintain a working list of items to track for yourself across this loop's iterations. " +
			"Submit the entire current list on each call — every call replaces the prior list, so include items already completed if you want them remembered. " +
			"Use this when work has multiple steps that span iterations, when steps depend on prior results, or when context compaction may evict your plan from the conversation. " +
			"Skip it for single-step lookups or one-shot tool calls where there is nothing to track.",
		Effect: agentic.ToolEffectMutating,
		Parameters: map[string]any{
			"type":                 "object",
			"required":             []string{"todos"},
			"additionalProperties": false,
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "The complete current list. Order is preserved. Provide an empty array to clear the list.",
					"items": map[string]any{
						"type":                 "object",
						"required":             []string{"id", "content", "status"},
						"additionalProperties": false,
						"properties": map[string]any{
							"id": map[string]any{
								"type":        "string",
								"description": "Stable identifier you assign — keep it stable across calls so progress is observable.",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "What the item is. Free-form, owner-interpretable; only you read this.",
							},
							"status": map[string]any{
								"type":        "string",
								"enum":        []string{"pending", "in_progress", "completed"},
								"description": "Mark items completed in the same iteration the work happened — never batch at the end.",
							},
						},
					},
				},
			},
		},
		Strict: true,
	}}
}

// todoArg is the parsed shape of one todo item from the tool args.
type todoArg struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// writeTodosArgs is the parsed shape of the write_todos arguments.
type writeTodosArgs struct {
	Todos []todoArg `json:"todos"`
}

// Execute routes the tool call. Any name other than write_todos is a
// routing bug at the dispatcher layer.
func (e *WriteTodosExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != WriteTodosToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "WriteTodosExecutor", "Execute", "route tool")
	}
	return e.write(ctx, call)
}

func (e *WriteTodosExecutor) write(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseWriteTodosArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "write_todos invoked without a loop_id; cannot resolve the loop entity",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "WriteTodosExecutor", "write", "resolve loop entity")
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "WriteTodosExecutor", "write", "construct loop entity ID")
	}

	now := e.now()
	triples := buildTodoTriples(loopEntityID, args.Todos, now)
	_, err = e.writer.ReplaceOwned(ctx, projection.ReplaceOwnedMutation{
		Contract: builtinprojection.LoopExecutionContractName,
		Group:    builtinprojection.TodoGroupName,
		EntityID: loopEntityID,
		Desired:  triples,
		Metadata: projection.MutationMetadata{
			RequestID: "write-todos:" + call.LoopID + ":" + call.ID,
			Source:    writeTodosToolSource,
			Timestamp: now,
		},
	})
	if err != nil {
		errorKind, classifiedErr := classifyTodoReplaceFailure(err)
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("replace todos: %v", err),
			ErrorKind: errorKind,
		}, classifiedErr
	}

	// Build a compact summary for ToolResult.Content. The agent reads
	// this back via the trajectory; the prompt assembler reconstructs
	// the full list each iteration from the loop-entity triples (ADR-036
	// Stage 4) so this content is informational, not load-bearing.
	summary := buildWriteTodosSummary(args.Todos)
	payload, err := json.Marshal(summary)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal summary: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "WriteTodosExecutor", "write", "marshal summary")
	}

	e.logger.Debug("write_todos applied",
		"loop_id", call.LoopID,
		"loop_entity_id", loopEntityID,
		"todo_count", len(args.Todos),
		"triple_count", len(triples))

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(payload),
		Metadata: map[string]any{
			"loop_entity_id": loopEntityID,
			"todo_count":     len(args.Todos),
			"triple_count":   len(triples),
		},
	}, nil
}

// classifyTodoReplaceFailure preserves the projection client's commit-aware
// taxonomy at the tool retry boundary. Only a typed unavailable outcome that is
// known not to have committed may enter the network/transient retry lane.
func classifyTodoReplaceFailure(err error) (agentic.ToolErrorKind, error) {
	const (
		component = "WriteTodosExecutor"
		method    = "write"
		action    = "replace todos"
	)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return agentic.ToolErrorUnknown, errs.WrapFatal(err, component, method, action)
	}

	var mutationErr *projection.MutationError
	if !errors.As(err, &mutationErr) {
		return agentic.ToolErrorInternal, errs.WrapFatal(err, component, method, action)
	}
	if mutationErr.Commit != projection.CommitNotCommitted {
		if mutationErr.Commit == projection.CommitUnknown ||
			mutationErr.Kind == projection.MutationCommitUnknown {
			return agentic.ToolErrorUnknown, errs.WrapFatal(err, component, method, action)
		}
		return agentic.ToolErrorInternal, errs.WrapFatal(err, component, method, action)
	}

	switch mutationErr.Kind {
	case projection.MutationUnavailable:
		if mutationErr.Class == errs.ErrorTransient {
			return agentic.ToolErrorNetwork, errs.WrapTransient(err, component, method, action)
		}
		return agentic.ToolErrorInternal, errs.WrapFatal(err, component, method, action)
	case projection.MutationStaleOwnerToken:
		return agentic.ToolErrorPermission, errs.WrapInvalid(err, component, method, action)
	case projection.MutationInvalid,
		projection.MutationConflict,
		projection.MutationRevisionConflict:
		return agentic.ToolErrorInvalidArgs, errs.WrapInvalid(err, component, method, action)
	case projection.MutationNotFound:
		return agentic.ToolErrorNotFound, errs.WrapInvalid(err, component, method, action)
	case projection.MutationCommitUnknown:
		return agentic.ToolErrorUnknown, errs.WrapFatal(err, component, method, action)
	case projection.MutationCommittedUnverified,
		projection.MutationInternal:
		return agentic.ToolErrorInternal, errs.WrapFatal(err, component, method, action)
	default:
		return agentic.ToolErrorInternal, errs.WrapFatal(err, component, method, action)
	}
}

// parseWriteTodosArgs decodes and validates the tool arguments.
// Validation errors return ToolErrorInvalidArgs strings so the LLM
// can self-correct without surfacing as fatal Go errors.
func parseWriteTodosArgs(raw map[string]any) (writeTodosArgs, error) {
	var args writeTodosArgs
	if raw == nil {
		return args, fmt.Errorf("missing required argument %q", "todos")
	}

	rawTodos, ok := raw["todos"]
	if !ok {
		return args, fmt.Errorf("missing required argument %q", "todos")
	}

	// Round-trip through JSON so the array-of-object shape is
	// canonical even if the caller passed map[string]any inside an []any.
	encoded, err := json.Marshal(rawTodos)
	if err != nil {
		return args, fmt.Errorf("encode todos: %w", err)
	}
	if err := json.Unmarshal(encoded, &args.Todos); err != nil {
		return args, fmt.Errorf("decode todos: must be an array of {id, content, status}")
	}

	seenIDs := make(map[string]struct{}, len(args.Todos))
	for i, t := range args.Todos {
		if t.ID == "" {
			return args, fmt.Errorf("todos[%d].id must be a non-empty string", i)
		}
		if t.Content == "" {
			return args, fmt.Errorf("todos[%d].content must be a non-empty string", i)
		}
		if _, ok := validTodoStatuses[t.Status]; !ok {
			return args, fmt.Errorf("todos[%d].status %q is invalid; must be one of pending|in_progress|completed", i, t.Status)
		}
		if _, dup := seenIDs[t.ID]; dup {
			return args, fmt.Errorf("todos[%d].id %q is a duplicate; ids must be unique within a call", i, t.ID)
		}
		seenIDs[t.ID] = struct{}{}
	}

	return args, nil
}

// buildTodoTriples produces the five triples per todo item that the
// prompt assembler reconstructs the list from. Position is derived
// from the array index (0-based); updated_at is the caller-supplied
// wall-clock time so tests can pin it deterministically.
func buildTodoTriples(loopEntityID string, todos []todoArg, now time.Time) []message.Triple {
	if len(todos) == 0 {
		return nil
	}
	triples := make([]message.Triple, 0, len(todos)*todoTriplesPerItem)
	updatedAt := now.UTC().Format(time.RFC3339Nano)
	for i, t := range todos {
		triples = append(triples,
			message.Triple{
				Subject: loopEntityID, Predicate: agvocab.TodoID,
				Object: t.ID, Source: writeTodosToolSource, Timestamp: now, Confidence: 1.0,
			},
			message.Triple{
				Subject: loopEntityID, Predicate: agvocab.TodoContent,
				Object: t.Content, Source: writeTodosToolSource, Timestamp: now, Confidence: 1.0,
			},
			message.Triple{
				Subject: loopEntityID, Predicate: agvocab.TodoStatus,
				Object: t.Status, Source: writeTodosToolSource, Timestamp: now, Confidence: 1.0,
			},
			message.Triple{
				Subject: loopEntityID, Predicate: agvocab.TodoPosition,
				Object: i, Source: writeTodosToolSource, Timestamp: now, Confidence: 1.0,
			},
			message.Triple{
				Subject: loopEntityID, Predicate: agvocab.TodoUpdatedAt,
				Object: updatedAt, Source: writeTodosToolSource, Timestamp: now, Confidence: 1.0,
			},
		)
	}
	return triples
}

// writeTodosSummary is the compact JSON the executor returns in
// ToolResult.Content. Not load-bearing for compaction survival —
// Stage 4's prompt assembler reads triples directly from the loop
// entity. This is informational for the trajectory.
type writeTodosSummary struct {
	Count int           `json:"count"`
	Todos []todoSummary `json:"todos,omitempty"`
}

type todoSummary struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Position int    `json:"position"`
}

func buildWriteTodosSummary(todos []todoArg) writeTodosSummary {
	if len(todos) == 0 {
		return writeTodosSummary{Count: 0}
	}
	out := writeTodosSummary{
		Count: len(todos),
		Todos: make([]todoSummary, 0, len(todos)),
	}
	for i, t := range todos {
		out.Todos = append(out.Todos, todoSummary{ID: t.ID, Status: t.Status, Position: i})
	}
	return out
}
