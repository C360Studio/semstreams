package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// WriteTodosToolName is the name agents use to invoke the write_todos
// tool. ADR-036 §First Instance.
const WriteTodosToolName = "write_todos"

// writeTodosBatchSubject is the NATS subject for the batched
// add-triple path. Stage 2 of ADR-036; matches
// processor/graph-ingest/mutations.go SubjectTripleAddBatch.
const writeTodosBatchSubject = "graph.mutation.triple.add_batch"

// writeTodosRemoveSubject is the NATS subject the executor uses to
// clear prior todo state. Mirrors decide.go's mutation surface.
const writeTodosRemoveSubject = "graph.mutation.triple.remove"

// writeTodosTimeout bounds each individual round-trip to graph-ingest.
// The tool issues one remove per todo predicate (len(todoPredicates))
// plus one batch add per call; the per-call worst case is roughly
// (len(todoPredicates)+1) * writeTodosTimeout * RetryAttempts and is
// otherwise only bounded by the caller's outer context deadline.
const writeTodosTimeout = 5 * time.Second

// writeTodosToolSource is the Source field on triples this tool
// writes. Lets operators distinguish todo writes from rule-driven or
// coordinator-decision triples in graph queries.
const writeTodosToolSource = "agent-write-todos"

// todoPredicates lists the predicate names that compose one todo
// item. write_todos clears all triples on the loop entity matching
// any of these before writing the new set, implementing full-list
// replace semantics.
var todoPredicates = []string{
	agvocab.TodoID,
	agvocab.TodoContent,
	agvocab.TodoStatus,
	agvocab.TodoPosition,
	agvocab.TodoUpdatedAt,
}

// validTodoStatuses enumerates the ADR-036 status enum. The executor
// rejects any other value with ToolErrorInvalidArgs so the LLM can
// self-correct rather than emitting silently-broken state.
var validTodoStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"completed":   {},
}

// TodoWriter is the narrow surface WriteTodosExecutor uses to mutate
// the graph. Production satisfies it with a NATS-backed adapter; tests
// substitute an in-memory recorder.
//
// RemoveByPredicate clears all triples on `subject` whose Predicate
// matches `predicate` — the canonical full-list-replace primitive at
// the predicate granularity. Idempotent on repeat or empty subjects.
//
// AddTriplesBatch atomically appends a slice of triples grouped per
// Subject (single CAS per entity at the graph-ingest end, ADR-036
// Stage 2). For write_todos every triple shares the loop entity ID,
// so the call is fully atomic in practice.
type TodoWriter interface {
	RemoveByPredicate(ctx context.Context, subject, predicate string) error
	AddTriplesBatch(ctx context.Context, triples []message.Triple) error
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
	writer   TodoWriter
	platform types.PlatformMeta
	logger   *slog.Logger
}

// NewWriteTodosExecutor constructs the executor given a writer and
// the platform identity used to resolve loop entity IDs.
func NewWriteTodosExecutor(writer TodoWriter, platform types.PlatformMeta) *WriteTodosExecutor {
	return &WriteTodosExecutor{writer: writer, platform: platform, logger: slog.Default()}
}

// SetLogger replaces the default logger. nil-safe.
func (e *WriteTodosExecutor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
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
	loopEntityID := agentic.LoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)

	// Step 1 — clear prior todo state. Each predicate is one CAS on the
	// loop entity. A future Stage 3.5 may collapse these to a single
	// atomic update_entity_with_triples handler; for now sequential is
	// correct and the IO cost is bounded to len(todoPredicates) round-
	// trips per call.
	for _, predicate := range todoPredicates {
		if err := e.writer.RemoveByPredicate(ctx, loopEntityID, predicate); err != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("clear prior %s: %v", predicate, err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "WriteTodosExecutor", "write", "clear prior todos")
		}
	}

	// Step 2 — write the new set as a single batch (one CAS on the
	// loop entity for all 5N triples thanks to ADR-036 Stage 2).
	triples := buildTodoTriples(loopEntityID, args.Todos, time.Now())
	if len(triples) > 0 {
		if err := e.writer.AddTriplesBatch(ctx, triples); err != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("batch write todos: %v", err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "WriteTodosExecutor", "write", "batch add triples")
		}
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
	triples := make([]message.Triple, 0, len(todos)*len(todoPredicates))
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

// natsTodoWriter adapts natsclient.Client to TodoWriter using the
// graph.mutation.triple.add_batch (ADR-036 Stage 2) and
// graph.mutation.triple.remove subjects. Kept local because
// write_todos is the only current caller; adopting it elsewhere
// would justify hoisting into a shared package.
type natsTodoWriter struct {
	client *natsclient.Client
}

// NewNATSTodoWriter builds a TodoWriter backed by the
// graph-ingest mutation surfaces.
func NewNATSTodoWriter(client *natsclient.Client) TodoWriter {
	return &natsTodoWriter{client: client}
}

func (w *natsTodoWriter) RemoveByPredicate(ctx context.Context, subject, predicate string) error {
	req := graph.RemoveTripleRequest{Subject: subject, Predicate: predicate}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal remove-triple request: %w", err)
	}
	respData, err := w.client.RequestWithRetry(ctx, writeTodosRemoveSubject, reqData, writeTodosTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", writeTodosRemoveSubject, err)
	}
	var resp graph.RemoveTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal remove response: %w", err)
	}
	if !resp.Success {
		// "entity not found" is not an error for our use case — first
		// write_todos call on a fresh loop has nothing to clear. The
		// graph-ingest handler returns Success=true for missing
		// entities (RemoveTriple returns nil); other failures
		// (network, marshal) surface the error string.
		return fmt.Errorf("graph-ingest rejected remove: %s", resp.Error)
	}
	return nil
}

func (w *natsTodoWriter) AddTriplesBatch(ctx context.Context, triples []message.Triple) error {
	req := graph.AddTriplesBatchRequest{Triples: triples}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal batch-add request: %w", err)
	}
	respData, err := w.client.RequestWithRetry(ctx, writeTodosBatchSubject, reqData, writeTodosTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", writeTodosBatchSubject, err)
	}
	var resp graph.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("graph-ingest rejected batch (written=%d, failed=%v): %s",
			resp.WrittenCount, resp.FailedSubjects, resp.Error)
	}
	return nil
}
