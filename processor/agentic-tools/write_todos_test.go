package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/require"
)

type recordingTodoReconciler struct {
	requests []projection.ReconcileMutation
	err      error
}

func (w *recordingTodoReconciler) Reconcile(_ context.Context, req projection.ReconcileMutation) (projection.MutationReceipt, error) {
	if w.err != nil {
		return projection.MutationReceipt{}, w.err
	}
	req.Desired = append([]message.Triple(nil), req.Desired...)
	w.requests = append(w.requests, req)
	return projection.MutationReceipt{Commit: projection.CommitVerified}, nil
}

func newWriteTodosExecutor(writer projection.PredicateReconciler) *WriteTodosExecutor {
	return NewWriteTodosExecutor(writer, types.PlatformMeta{Org: "acme", Platform: "test"})
}

func todosArg(items ...map[string]any) map[string]any {
	return map[string]any{"todos": items}
}

func TestWriteTodosExecutor_ListTools(t *testing.T) {
	e := newWriteTodosExecutor(&recordingTodoReconciler{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != WriteTodosToolName {
		t.Errorf("name = %q, want %q", tools[0].Name, WriteTodosToolName)
	}
	if !tools[0].Strict {
		t.Errorf("ADR-035 says write_todos should ship Strict=true; got false")
	}
	// Description must teach when to call vs skip — pin both shapes.
	desc := tools[0].Description
	if !strings.Contains(desc, "replaces the prior list") {
		t.Errorf("description should clarify full-list-replace semantics; got: %s", desc)
	}
	if !strings.Contains(desc, "Skip it") || !strings.Contains(desc, "single-step") {
		t.Errorf("description should call out skip-for-trivial cases; got: %s", desc)
	}
}

// TestWriteTodosExecutor_HappyPath pins the canonical write shape: one atomic
// group reconcile carrying one deterministic JSON record triple per todo.
func TestWriteTodosExecutor_HappyPath(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)
	frozen := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	e.SetClock(func() time.Time { return frozen })

	args := todosArg(
		map[string]any{"id": "1", "content": "Survey rules", "status": "completed"},
		map[string]any{"id": "2", "content": "Draft new rule", "status": "in_progress"},
		map[string]any{"id": "3", "content": "Wire e2e test", "status": "pending"},
	)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		Name:      WriteTodosToolName,
		LoopID:    "loop-abc",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("ToolResult.Error = %q, want empty", res.Error)
	}
	if res.StopLoop {
		t.Errorf("write_todos must NOT stop the loop (it is called repeatedly)")
	}

	if got := len(w.requests); got != 1 {
		t.Fatalf("reconcile count = %d, want 1", got)
	}
	wantLoopEntityID := agentic.LoopExecutionEntityID("acme", "test", "loop-abc")
	req := w.requests[0]
	if req.EntityID != wantLoopEntityID || req.Contract != "agentic.loop-execution" || req.Group != "todos" {
		t.Fatalf("reconcile target = %#v", req)
	}
	if req.Metadata.RequestID != "write-todos:loop-abc:call-1" {
		t.Errorf("request ID = %q", req.Metadata.RequestID)
	}

	batch := req.Desired
	if got, want := len(batch), 3; got != want {
		t.Fatalf("triple count = %d, want %d", got, want)
	}

	// Every triple lands on the loop entity, not anywhere else.
	for i, tr := range batch {
		if tr.Subject != wantLoopEntityID {
			t.Errorf("batch[%d].subject = %q, want %q", i, tr.Subject, wantLoopEntityID)
		}
		if tr.Source != writeTodosToolSource {
			t.Errorf("batch[%d].source = %q, want %q (operator distinguishes write_todos from other writers)",
				i, tr.Source, writeTodosToolSource)
		}
		if tr.Predicate != agvocab.TodoRecord {
			t.Errorf("batch[%d].predicate = %q, want %q", i, tr.Predicate, agvocab.TodoRecord)
		}
		if tr.Datatype != agvocab.TodoRecordJSONDatatype {
			t.Errorf("batch[%d].datatype = %q, want %q", i, tr.Datatype, agvocab.TodoRecordJSONDatatype)
		}
	}

	wantRecords := []string{
		`{"id":"1","content":"Survey rules","status":"completed","position":0,"updated_at":"2026-05-09T12:00:00Z"}`,
		`{"id":"2","content":"Draft new rule","status":"in_progress","position":1,"updated_at":"2026-05-09T12:00:00Z"}`,
		`{"id":"3","content":"Wire e2e test","status":"pending","position":2,"updated_at":"2026-05-09T12:00:00Z"}`,
	}
	for i, tr := range batch {
		if got, ok := tr.Object.(string); !ok || got != wantRecords[i] {
			t.Errorf("batch[%d].object = %#v, want deterministic record %q", i, tr.Object, wantRecords[i])
		}
	}

	// ToolResult content is informational — the agent reads back via
	// trajectory. Pin the count and the position-derivation in the
	// summary.
	var summary writeTodosSummary
	if err := json.Unmarshal([]byte(res.Content), &summary); err != nil {
		t.Fatalf("decode result content: %v", err)
	}
	if summary.Count != 3 {
		t.Errorf("summary.count = %d, want 3", summary.Count)
	}
	if len(summary.Todos) != 3 || summary.Todos[2].Position != 2 || summary.Todos[2].ID != "3" {
		t.Errorf("summary.todos[2] = %+v, want id=3 position=2", summary.Todos[2])
	}

	// Metadata surfaces the loop entity ID for debug consumers.
	if got, _ := res.Metadata["loop_entity_id"].(string); got != wantLoopEntityID {
		t.Errorf("metadata.loop_entity_id = %q, want %q", got, wantLoopEntityID)
	}
	if got, _ := res.Metadata["todo_count"].(int); got != 3 {
		t.Errorf("metadata.todo_count = %d, want 3", got)
	}
	if _, exists := res.Metadata["triple_count"]; exists {
		t.Error("metadata.triple_count must be removed; it exposes the storage representation")
	}
}

// TestWriteTodosExecutor_EmptyListClears verifies that submitting an
// empty array clears prior state without writing anything new — the
// agent's way to "delete the list."
func TestWriteTodosExecutor_EmptyListClears(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		Name:      WriteTodosToolName,
		LoopID:    "loop-clear",
		Arguments: todosArg(),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("ToolResult.Error = %q, want empty", res.Error)
	}
	if len(w.requests) != 1 || len(w.requests[0].Desired) != 0 {
		t.Errorf("empty list must issue one empty desired reconcile; got %#v", w.requests)
	}
}

// TestWriteTodosExecutor_ValidationRejection covers the per-arg
// rejection shapes — each must surface ToolErrorInvalidArgs without
// touching the writer (LLM gets to self-correct).
func TestWriteTodosExecutor_ValidationRejection(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantSub string
	}{
		{
			name:    "missing todos arg",
			args:    map[string]any{},
			wantSub: `missing required argument "todos"`,
		},
		{
			name: "todo missing id",
			args: todosArg(
				map[string]any{"content": "x", "status": "pending"},
			),
			wantSub: ".id must be a non-empty string",
		},
		{
			name: "todo missing content",
			args: todosArg(
				map[string]any{"id": "1", "status": "pending"},
			),
			wantSub: ".content must be a non-empty string",
		},
		{
			name: "invalid status",
			args: todosArg(
				map[string]any{"id": "1", "content": "x", "status": "blocked"},
			),
			wantSub: "must be one of pending|in_progress|completed",
		},
		{
			name: "duplicate id",
			args: todosArg(
				map[string]any{"id": "1", "content": "x", "status": "pending"},
				map[string]any{"id": "1", "content": "y", "status": "pending"},
			),
			wantSub: "is a duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &recordingTodoReconciler{}
			e := newWriteTodosExecutor(w)
			res, err := e.Execute(context.Background(), agentic.ToolCall{
				ID:        "c",
				Name:      WriteTodosToolName,
				LoopID:    "loop-x",
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("Execute returned Go error (validation should surface as ToolResult.Error): %v", err)
			}
			if res.ErrorKind != agentic.ToolErrorInvalidArgs {
				t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorInvalidArgs)
			}
			if !strings.Contains(res.Error, tt.wantSub) {
				t.Errorf("Error = %q, want substring %q", res.Error, tt.wantSub)
			}
			if len(w.requests) != 0 {
				t.Errorf("validation rejection must not touch writer; saw %d requests", len(w.requests))
			}
		})
	}
}

// TestWriteTodosExecutor_MissingLoopIDIsInternal surfaces a
// dispatcher-layer bug rather than a self-correctable args mistake.
func TestWriteTodosExecutor_MissingLoopIDIsInternal(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c",
		Name:      WriteTodosToolName,
		Arguments: todosArg(map[string]any{"id": "1", "content": "x", "status": "pending"}),
	})
	if err == nil {
		t.Fatal("expected Go error for missing loop_id (dispatcher bug)")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorInternal)
	}
	if len(w.requests) != 0 {
		t.Errorf("dispatcher-bug rejection must not touch writer")
	}
}

func TestWriteTodosExecutor_ReconcileFailureClassification(t *testing.T) {
	t.Parallel()

	mutationFailure := func(
		kind projection.MutationErrorKind,
		commit projection.CommitState,
		class errs.ErrorClass,
		cause error,
	) error {
		return &projection.MutationError{
			Operation: projection.MutationOperationReconcile,
			Kind:      kind,
			Class:     class,
			Commit:    commit,
			Err:       cause,
		}
	}
	unavailableCause := errors.New("transport unavailable")
	unknownCause := errors.New("response lost after write")
	verifiedErrorCause := errors.New("impossible post-verification error")
	invalidCause := errors.New("invalid replacement")
	conflictCause := errors.New("replacement conflict")
	notFoundCause := errors.New("owned projection not found")
	internalCause := errors.New("projection client invariant failed")
	genericCause := errors.New("unexpected writer failure")

	tests := []struct {
		name          string
		cause         error
		failure       error
		wantKind      agentic.ToolErrorKind
		wantMutation  bool
		wantTransient bool
		wantRetry     bool
	}{
		{
			name:  "unavailable not committed is retryable",
			cause: unavailableCause,
			failure: mutationFailure(
				projection.MutationUnavailable,
				projection.CommitNotCommitted,
				errs.ErrorTransient,
				unavailableCause,
			),
			wantKind:      agentic.ToolErrorNetwork,
			wantMutation:  true,
			wantTransient: true,
			wantRetry:     true,
		},
		{
			name:  "commit unknown is non-retryable",
			cause: unknownCause,
			failure: mutationFailure(
				projection.MutationCommitUnknown,
				projection.CommitUnknown,
				errs.ErrorTransient,
				unknownCause,
			),
			wantKind:     agentic.ToolErrorUnknown,
			wantMutation: true,
		},
		{
			name:  "verified error is internal and non-retryable",
			cause: verifiedErrorCause,
			failure: mutationFailure(
				projection.MutationInternal,
				projection.CommitVerified,
				errs.ErrorFatal,
				verifiedErrorCause,
			),
			wantKind:     agentic.ToolErrorInternal,
			wantMutation: true,
		},
		{
			name:  "invalid mutation is invalid arguments and non-retryable",
			cause: invalidCause,
			failure: mutationFailure(
				projection.MutationInvalid,
				projection.CommitNotCommitted,
				errs.ErrorInvalid,
				invalidCause,
			),
			wantKind:     agentic.ToolErrorInvalidArgs,
			wantMutation: true,
		},
		{
			name:  "conflict is invalid arguments and non-retryable",
			cause: conflictCause,
			failure: mutationFailure(
				projection.MutationConflict,
				projection.CommitNotCommitted,
				errs.ErrorInvalid,
				conflictCause,
			),
			wantKind:     agentic.ToolErrorInvalidArgs,
			wantMutation: true,
		},
		{
			name:  "not found is non-retryable",
			cause: notFoundCause,
			failure: mutationFailure(
				projection.MutationNotFound,
				projection.CommitNotCommitted,
				errs.ErrorInvalid,
				notFoundCause,
			),
			wantKind:     agentic.ToolErrorNotFound,
			wantMutation: true,
		},
		{
			name:  "internal mutation is non-retryable",
			cause: internalCause,
			failure: mutationFailure(
				projection.MutationInternal,
				projection.CommitNotCommitted,
				errs.ErrorFatal,
				internalCause,
			),
			wantKind:     agentic.ToolErrorInternal,
			wantMutation: true,
		},
		{
			name:  "context cancellation is non-retryable",
			cause: context.Canceled,
			failure: mutationFailure(
				projection.MutationUnavailable,
				projection.CommitNotCommitted,
				errs.ErrorTransient,
				context.Canceled,
			),
			wantKind:     agentic.ToolErrorUnknown,
			wantMutation: true,
		},
		{
			name:     "generic error is internal and non-retryable",
			cause:    genericCause,
			failure:  genericCause,
			wantKind: agentic.ToolErrorInternal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reconciler := &recordingTodoReconciler{err: test.failure}
			executor := newWriteTodosExecutor(reconciler)
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID:     "c",
				Name:   WriteTodosToolName,
				LoopID: "loop-x",
				Arguments: todosArg(
					map[string]any{"id": "1", "content": "x", "status": "pending"},
				),
			})
			if err == nil {
				t.Fatal("expected wrapped reconcile error")
			}
			if result.ErrorKind != test.wantKind {
				t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, test.wantKind)
			}
			if !strings.Contains(result.Error, test.cause.Error()) {
				t.Errorf("ToolResult.Error = %q, want cause %q", result.Error, test.cause)
			}
			if !errors.Is(err, test.cause) {
				t.Errorf("returned error does not preserve errors.Is cause %v: %v", test.cause, err)
			}
			var mutationErr *projection.MutationError
			if got := errors.As(err, &mutationErr); got != test.wantMutation {
				t.Errorf("errors.As(*MutationError) = %v, want %v (err=%v)", got, test.wantMutation, err)
			}
			if got := errs.IsTransient(err); got != test.wantTransient {
				t.Errorf("errs.IsTransient = %v, want %v (err=%v)", got, test.wantTransient, err)
			}
			policy := RetryPolicy{
				MaxAttempts: 2,
				RetryOnKinds: []string{
					string(agentic.ToolErrorTimeout),
					string(agentic.ToolErrorExternal),
					string(agentic.ToolErrorNetwork),
				},
			}
			if got := shouldRetry(err, result, policy); got != test.wantRetry {
				t.Errorf("shouldRetry = %v, want %v", got, test.wantRetry)
			}
		})
	}
}

// TestWriteTodosExecutor_UnknownToolNameIsRoutingBug protects the
// dispatcher contract: the executor should never see a name other
// than WriteTodosToolName.
func TestWriteTodosExecutor_UnknownToolNameIsRoutingBug(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   "not_write_todos",
		LoopID: "loop-x",
	})
	if err == nil {
		t.Fatal("expected Go error for unknown tool name")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorNotFound)
	}
}

// TestWriteTodosExecutor_DeterministicClock pins the SetClock contract
// (Stage 3.7). Tests inject a frozen clock; the executor stamps that
// time onto Triple.Timestamp and the record's updated_at value, so
// assertions about timestamp equality become deterministic instead of
// "today, ish" tolerances.
func TestWriteTodosExecutor_DeterministicClock(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)
	frozen := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	e.SetClock(func() time.Time { return frozen })

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   WriteTodosToolName,
		LoopID: "loop-x",
		Arguments: todosArg(
			map[string]any{"id": "1", "content": "x", "status": "pending"},
		),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	require.Len(t, w.requests, 1)
	for i, tr := range w.requests[0].Desired {
		if !tr.Timestamp.Equal(frozen) {
			t.Errorf("triple[%d].Timestamp = %v, want %v", i, tr.Timestamp, frozen)
		}
		var record struct {
			UpdatedAt string `json:"updated_at"`
		}
		encoded, ok := tr.Object.(string)
		if !ok {
			t.Fatalf("triple[%d].Object = %#v, want JSON string", i, tr.Object)
		}
		if err := json.Unmarshal([]byte(encoded), &record); err != nil {
			t.Fatalf("decode triple[%d] record: %v", i, err)
		}
		if record.UpdatedAt != frozen.Format(time.RFC3339Nano) {
			t.Errorf("record.updated_at = %q, want %q", record.UpdatedAt, frozen.Format(time.RFC3339Nano))
		}
	}
}

// TestWriteTodosExecutor_MalformedLoopIDSurfacesAsInternal pins
// ADR-036 Stage 3.8: a loop_id that contains dots (the beta.36-class
// bug where an upstream stamps a 6-part entity ID into call.LoopID)
// must surface as ToolErrorInternal with a descriptive Go error,
// NOT crash the dispatch goroutine via panic from
// LoopExecutionEntityID.
func TestWriteTodosExecutor_MalformedLoopIDSurfacesAsInternal(t *testing.T) {
	w := &recordingTodoReconciler{}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   WriteTodosToolName,
		LoopID: "acme.test.agentic-loop.agent.execution.abc123", // 6-part ID, has dots
		Arguments: todosArg(
			map[string]any{"id": "1", "content": "x", "status": "pending"},
		),
	})
	if err == nil {
		t.Fatal("expected Go error for dotted loop_id (panic-class regression)")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorInternal)
	}
	if !strings.Contains(res.Error, "loop entity ID") {
		t.Errorf("Error %q should reference the loop entity ID construction", res.Error)
	}
	if len(w.requests) != 0 {
		t.Errorf("malformed loop_id rejection must not touch writer; saw %d requests", len(w.requests))
	}
}

// TestWriteTodos_CategoryIsCore pins the per-loop-not-per-role
// discipline from ADR-036: write_todos lives in CategoryCore so it's
// available to every role; persona authors opt out per-deployment.
// If a future refactor moves it to CategoryOrchestration or similar,
// it changes the access semantics — this test catches the drift.
func TestWriteTodos_CategoryIsCore(t *testing.T) {
	if got := GetToolCategory("write_todos"); got != CategoryCore {
		t.Errorf("write_todos category = %q, want %q (ADR-036 §First Instance — available to any role)",
			got, CategoryCore)
	}
}
