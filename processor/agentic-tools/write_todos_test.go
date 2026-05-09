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
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/require"
)

// recordingTodoWriter implements TodoWriter in-process so tests can
// assert on the exact remove/add operations write_todos emitted.
// Mirror of recordingPublisher in decide_test.go.
type recordingTodoWriter struct {
	removes      []removeOp
	addedBatches [][]message.Triple
	removeErr    error
	addErr       error
}

type removeOp struct {
	subject   string
	predicate string
}

func (w *recordingTodoWriter) RemoveByPredicate(_ context.Context, subject, predicate string) error {
	if w.removeErr != nil {
		return w.removeErr
	}
	w.removes = append(w.removes, removeOp{subject: subject, predicate: predicate})
	return nil
}

func (w *recordingTodoWriter) AddTriplesBatch(_ context.Context, triples []message.Triple) error {
	if w.addErr != nil {
		return w.addErr
	}
	cp := make([]message.Triple, len(triples))
	copy(cp, triples)
	w.addedBatches = append(w.addedBatches, cp)
	return nil
}

func newWriteTodosExecutor(writer TodoWriter) *WriteTodosExecutor {
	return NewWriteTodosExecutor(writer, types.PlatformMeta{Org: "acme", Platform: "test"})
}

func todosArg(items ...map[string]any) map[string]any {
	return map[string]any{"todos": items}
}

func TestWriteTodosExecutor_ListTools(t *testing.T) {
	e := newWriteTodosExecutor(&recordingTodoWriter{})
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

// TestWriteTodosExecutor_HappyPath pins the canonical write shape:
// one remove per todo predicate (order matches todoPredicates), then
// one batch add carrying 5 triples per todo on the loop entity.
func TestWriteTodosExecutor_HappyPath(t *testing.T) {
	w := &recordingTodoWriter{}
	e := newWriteTodosExecutor(w)

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

	// Removes happen first, in the canonical predicate order. Pinning
	// both order and contents keeps the load-bearing full-list-replace
	// invariant from drifting silently.
	if got, want := len(w.removes), len(todoPredicates); got != want {
		t.Fatalf("remove count = %d, want %d (one per predicate)", got, want)
	}
	wantLoopEntityID := agentic.LoopExecutionEntityID("acme", "test", "loop-abc")
	for i, op := range w.removes {
		if op.subject != wantLoopEntityID {
			t.Errorf("remove[%d].subject = %q, want %q", i, op.subject, wantLoopEntityID)
		}
		if op.predicate != todoPredicates[i] {
			t.Errorf("remove[%d].predicate = %q, want %q", i, op.predicate, todoPredicates[i])
		}
	}

	// Single batch carries 3 todos × 5 predicates = 15 triples.
	if got, want := len(w.addedBatches), 1; got != want {
		t.Fatalf("batch count = %d, want %d (single CAS for all triples)", got, want)
	}
	batch := w.addedBatches[0]
	if got, want := len(batch), 3*len(todoPredicates); got != want {
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
	}

	// Position is derived from array index, not user-supplied. Verify
	// the third todo has position=2.
	foundThirdPosition := false
	for _, tr := range batch {
		if tr.Predicate != agvocab.TodoPosition {
			continue
		}
		if tr.Object == 2 {
			foundThirdPosition = true
		}
	}
	if !foundThirdPosition {
		t.Errorf("expected one TodoPosition triple with object=2 (third todo); did not find it")
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
}

// TestWriteTodosExecutor_EmptyListClears verifies that submitting an
// empty array clears prior state without writing anything new — the
// agent's way to "delete the list."
func TestWriteTodosExecutor_EmptyListClears(t *testing.T) {
	w := &recordingTodoWriter{}
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
	if got, want := len(w.removes), len(todoPredicates); got != want {
		t.Errorf("remove count = %d, want %d", got, want)
	}
	if len(w.addedBatches) != 0 {
		t.Errorf("empty list must not add anything; got %d batch(es)", len(w.addedBatches))
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
			w := &recordingTodoWriter{}
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
			if len(w.removes) != 0 || len(w.addedBatches) != 0 {
				t.Errorf("validation rejection must not touch writer; saw removes=%d batches=%d",
					len(w.removes), len(w.addedBatches))
			}
		})
	}
}

// TestWriteTodosExecutor_MissingLoopIDIsInternal surfaces a
// dispatcher-layer bug rather than a self-correctable args mistake.
func TestWriteTodosExecutor_MissingLoopIDIsInternal(t *testing.T) {
	w := &recordingTodoWriter{}
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
	if len(w.removes) != 0 {
		t.Errorf("dispatcher-bug rejection must not touch writer")
	}
}

// TestWriteTodosExecutor_RemoveErrorSurfaces verifies the network
// error from clearing prior state surfaces with ToolErrorNetwork so
// the loop's retry policy applies the right strategy.
func TestWriteTodosExecutor_RemoveErrorSurfaces(t *testing.T) {
	w := &recordingTodoWriter{removeErr: errors.New("graph-ingest unavailable")}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   WriteTodosToolName,
		LoopID: "loop-x",
		Arguments: todosArg(
			map[string]any{"id": "1", "content": "x", "status": "pending"},
		),
	})
	if err == nil {
		t.Fatal("expected wrapped Go error for transient network failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorNetwork)
	}
	if !strings.Contains(res.Error, "graph-ingest unavailable") {
		t.Errorf("Error should propagate underlying message; got %q", res.Error)
	}
}

// TestWriteTodosExecutor_AddErrorSurfaces verifies post-clear batch
// failures also surface as ToolErrorNetwork — the retry policy
// shouldn't differ based on which round-trip failed.
func TestWriteTodosExecutor_AddErrorSurfaces(t *testing.T) {
	w := &recordingTodoWriter{addErr: errors.New("CAS conflict, exhausted retries")}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   WriteTodosToolName,
		LoopID: "loop-x",
		Arguments: todosArg(
			map[string]any{"id": "1", "content": "x", "status": "pending"},
		),
	})
	if err == nil {
		t.Fatal("expected wrapped Go error for batch failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %q, want %q", res.ErrorKind, agentic.ToolErrorNetwork)
	}
	// Removes ran before the add failed — that's fine, full-list-replace
	// semantics mean a retry is safe.
	if len(w.removes) != len(todoPredicates) {
		t.Errorf("removes should have completed before the failed add; got %d", len(w.removes))
	}
}

// TestWriteTodosExecutor_UnknownToolNameIsRoutingBug protects the
// dispatcher contract: the executor should never see a name other
// than WriteTodosToolName.
func TestWriteTodosExecutor_UnknownToolNameIsRoutingBug(t *testing.T) {
	w := &recordingTodoWriter{}
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
// time onto Triple.Timestamp and the agent.todo.updated_at value, so
// assertions about timestamp equality become deterministic instead of
// "today, ish" tolerances.
func TestWriteTodosExecutor_DeterministicClock(t *testing.T) {
	w := &recordingTodoWriter{}
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
	require.Len(t, w.addedBatches, 1)
	for i, tr := range w.addedBatches[0] {
		if !tr.Timestamp.Equal(frozen) {
			t.Errorf("triple[%d].Timestamp = %v, want %v", i, tr.Timestamp, frozen)
		}
		if tr.Predicate == agvocab.TodoUpdatedAt {
			got, _ := tr.Object.(string)
			if got != frozen.Format(time.RFC3339Nano) {
				t.Errorf("TodoUpdatedAt object = %q, want %q", got, frozen.Format(time.RFC3339Nano))
			}
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
	w := &recordingTodoWriter{}
	e := newWriteTodosExecutor(w)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   WriteTodosToolName,
		LoopID: "acme.test.agent.agentic-loop.execution.abc123", // 6-part ID, has dots
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
	if len(w.removes) != 0 || len(w.addedBatches) != 0 {
		t.Errorf("malformed loop_id rejection must not touch writer; saw removes=%d batches=%d",
			len(w.removes), len(w.addedBatches))
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
