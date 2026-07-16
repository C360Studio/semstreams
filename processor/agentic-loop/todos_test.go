package agenticloop

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// todoFixtureLoopEntityID is the canonical loop entity ID used by
// the todo unit tests. Avoids the name todoFixtureLoopEntityID which
// graph_writer_ops_test.go owns as a function.
var todoFixtureLoopEntityID = agentic.LoopExecutionEntityID(testOrg, testPlatform, "loop123")

func todoTestPlatform() types.PlatformMeta {
	return types.PlatformMeta{Org: testOrg, Platform: testPlatform}
}

func todoTestLogger() *slog.Logger { return slog.Default() }

var errTodoReadFailed = errors.New("graph-gateway transient unavailable")

// makeTodoBatch produces the canonical 5-triple sequence write_todos
// emits per item, in the same interleaved order. Tests use it to
// avoid hand-rolling the fixture format.
func makeTodoBatch(t *testing.T, items []TodoState, ts time.Time) []message.Triple {
	t.Helper()
	out := make([]message.Triple, 0, len(items)*todoTripleStride)
	for i, item := range items {
		out = append(out,
			message.Triple{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoID, Object: item.ID, Timestamp: ts, Confidence: 1.0},
			message.Triple{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoContent, Object: item.Content, Timestamp: ts, Confidence: 1.0},
			message.Triple{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoStatus, Object: item.Status, Timestamp: ts, Confidence: 1.0},
			message.Triple{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoPosition, Object: i, Timestamp: ts, Confidence: 1.0},
			message.Triple{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoUpdatedAt, Object: ts.Format(time.RFC3339Nano), Timestamp: ts, Confidence: 1.0},
		)
	}
	return out
}

func TestReconstructTodos_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	items := []TodoState{
		{ID: "1", Content: "Survey rules", Status: "completed"},
		{ID: "2", Content: "Draft new rule", Status: "in_progress"},
		{ID: "3", Content: "Wire e2e test", Status: "pending"},
	}
	got := ReconstructTodos(makeTodoBatch(t, items, now))
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range items {
		if got[i].ID != want.ID {
			t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, want.ID)
		}
		if got[i].Content != want.Content {
			t.Errorf("got[%d].Content = %q, want %q", i, got[i].Content, want.Content)
		}
		if got[i].Status != want.Status {
			t.Errorf("got[%d].Status = %q, want %q", i, got[i].Status, want.Status)
		}
		if got[i].Position != i {
			t.Errorf("got[%d].Position = %d, want %d", i, got[i].Position, i)
		}
	}
}

// TestReconstructTodos_EmptyAndIrrelevant verifies non-todo triples
// don't bleed in and an empty triple slice yields nil.
func TestReconstructTodos_EmptyAndIrrelevant(t *testing.T) {
	if got := ReconstructTodos(nil); got != nil {
		t.Errorf("nil triples → got %v, want nil", got)
	}
	now := time.Now()
	mixed := []message.Triple{
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.LoopOutcome, Object: "success", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: "rule.task.spawned", Object: "x", Timestamp: now},
	}
	if got := ReconstructTodos(mixed); got != nil {
		t.Errorf("non-todo triples → got %v, want nil", got)
	}
}

// TestReconstructTodos_FewerThanStride drops a partial group instead
// of emitting a half-formed item — matches the mid-write-failure
// recovery contract.
func TestReconstructTodos_FewerThanStride(t *testing.T) {
	now := time.Now()
	partial := []message.Triple{
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoID, Object: "1", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoContent, Object: "x", Timestamp: now},
		// status, position, updated_at missing — simulates a write that
		// got partially flushed before the loop entity was read.
	}
	if got := ReconstructTodos(partial); len(got) != 0 {
		t.Errorf("partial group → got %v, want empty", got)
	}
}

// TestReconstructTodos_OutOfOrderGroupSkipped pins that the parser
// tolerates a desynchronised triple stream rather than emitting
// scrambled state. If a future graph-ingest change reorders the
// stored slice, the assembler degrades gracefully.
func TestReconstructTodos_OutOfOrderGroupSkipped(t *testing.T) {
	now := time.Now()
	scrambled := []message.Triple{
		// Group 1 — order matches contract (id, content, status, position, updated_at).
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoID, Object: "1", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoContent, Object: "ok", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoStatus, Object: "pending", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoPosition, Object: 0, Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoUpdatedAt, Object: now.Format(time.RFC3339Nano), Timestamp: now},
		// Group 2 — predicate order scrambled (status before id). Skipped.
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoStatus, Object: "completed", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoID, Object: "2", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoContent, Object: "skipped", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoPosition, Object: 1, Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: agvocab.TodoUpdatedAt, Object: now.Format(time.RFC3339Nano), Timestamp: now},
	}
	got := ReconstructTodos(scrambled)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only the first group is in canonical order)", len(got))
	}
	if got[0].ID != "1" {
		t.Errorf("got[0].ID = %q, want %q", got[0].ID, "1")
	}
}

func TestBuildTodoStateMessage_FormatAndStatusMarkers(t *testing.T) {
	msg := BuildTodoStateMessage([]TodoState{
		{ID: "1", Content: "done thing", Status: "completed", Position: 0},
		{ID: "2", Content: "in flight", Status: "in_progress", Position: 1},
		{ID: "3", Content: "next up", Status: "pending", Position: 2},
		{ID: "4", Content: "weird", Status: "wat", Position: 3},
	})
	if msg.Role != "system" {
		t.Errorf("Role = %q, want system", msg.Role)
	}
	for _, want := range []string{
		"[Working list",  // header
		"[x] done thing", // completed
		"[~] in flight",  // in_progress
		"[ ] next up",    // pending
		"[?] weird",      // unrecognised
	} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("content missing %q\n--- content ---\n%s", want, msg.Content)
		}
	}
}

func TestBuildTodoStateMessage_EmptyReturnsZeroValue(t *testing.T) {
	msg := BuildTodoStateMessage(nil)
	if msg.Role != "" || msg.Content != "" {
		t.Errorf("empty list should return zero ChatMessage, got %+v", msg)
	}
}

// TestParseQueryEntityTodos_UnmarshalFailureSurfaces pins the
// remaining contract for parseQueryEntityTodos after the gh#93
// Phase 2 migration: ReadTodos now routes through
// natsclient.RequestClassified, so handler errors arrive via the err
// return and the fresh-loop fail-open contract is enforced one level
// up. parseQueryEntityTodos only sees success-path bytes; if they
// aren't valid EntityState JSON, return the unmarshal error.
//
// The not-found / fresh-loop fail-open contract is now wire-driven
// by the integration test in todos_wire_integration_test.go.
func TestParseQueryEntityTodos_UnmarshalFailureSurfaces(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantError bool
	}{
		{"empty body", "", true},
		{"non-JSON", "garbage data", true},
		{"empty triples", `{"id":"c360.ops.agent.loop.todo.test-001","triples":[]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseQueryEntityTodos([]byte(tt.payload))
			if tt.wantError && err == nil {
				t.Errorf("payload %q: expected error, got nil", tt.payload)
			}
			if !tt.wantError && err != nil {
				t.Errorf("payload %q: got err %v, want nil", tt.payload, err)
			}
		})
	}
}

// fakeTodoReader is the minimal in-process TodoReader for handler-level tests.
type fakeTodoReader struct {
	todos []TodoState
	err   error
}

func (f *fakeTodoReader) ReadTodos(_ context.Context, _ string) ([]TodoState, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.todos, nil
}

// TestMessageHandler_PrependIterationContext_TodoBlock verifies that
// when a TodoReader is wired and returns todos, the handler's
// per-iteration prefix carries both the budget message AND the
// working-list block (in that order).
func TestMessageHandler_PrependIterationContext_TodoBlock(t *testing.T) {
	h := &MessageHandler{
		config:   Config{},
		platform: todoTestPlatform(),
		todoReader: &fakeTodoReader{
			todos: []TodoState{{ID: "1", Content: "track me", Status: "in_progress"}},
		},
	}
	h.logger = todoTestLogger()

	prefixed := h.prependIterationContext(context.Background(), "loop-x", 1, 10, []agentic.ChatMessage{
		{Role: "user", Content: "go"},
	})
	if len(prefixed) != 3 {
		t.Fatalf("len = %d, want 3 (budget + todo + user)", len(prefixed))
	}
	if !strings.Contains(prefixed[0].Content, "Iteration Budget") {
		t.Errorf("prefix[0] should be the iteration budget; got %q", prefixed[0].Content)
	}
	if !strings.Contains(prefixed[1].Content, "[~] track me") {
		t.Errorf("prefix[1] should be the working-list block; got %q", prefixed[1].Content)
	}
	if prefixed[2].Role != "user" {
		t.Errorf("prefix[2] should be the original user message; got %+v", prefixed[2])
	}
}

// TestMessageHandler_PrependIterationContext_NoTodoReader verifies
// that without a TodoReader (NATS-less test path), the handler still
// prepends only the budget message and doesn't crash.
func TestMessageHandler_PrependIterationContext_NoTodoReader(t *testing.T) {
	h := &MessageHandler{
		config:   Config{},
		platform: todoTestPlatform(),
	}
	h.logger = todoTestLogger()
	prefixed := h.prependIterationContext(context.Background(), "loop-x", 1, 10, []agentic.ChatMessage{
		{Role: "user", Content: "go"},
	})
	if len(prefixed) != 2 {
		t.Fatalf("len = %d, want 2 (budget + user)", len(prefixed))
	}
}

// TestMessageHandler_PrependIterationContext_EmptyTodos verifies the
// empty-list case — TodoReader returns no items, handler skips the
// block.
func TestMessageHandler_PrependIterationContext_EmptyTodos(t *testing.T) {
	h := &MessageHandler{
		config:     Config{},
		platform:   todoTestPlatform(),
		todoReader: &fakeTodoReader{todos: nil},
	}
	h.logger = todoTestLogger()
	prefixed := h.prependIterationContext(context.Background(), "loop-x", 1, 10, nil)
	if len(prefixed) != 1 {
		t.Fatalf("len = %d, want 1 (just the budget; no todo block when list is empty)", len(prefixed))
	}
	if !strings.Contains(prefixed[0].Content, "Iteration Budget") {
		t.Errorf("prefix[0] should be the iteration budget; got %q", prefixed[0].Content)
	}
}

// TestMessageHandler_PrependIterationContext_ReadFailure_FailsOpen
// verifies that read errors are swallowed silently — the handler
// just doesn't see a todo block this iteration; the next iteration
// retries.
func TestMessageHandler_PrependIterationContext_ReadFailure_FailsOpen(t *testing.T) {
	h := &MessageHandler{
		config:     Config{},
		platform:   todoTestPlatform(),
		todoReader: &fakeTodoReader{err: errTodoReadFailed},
	}
	h.logger = todoTestLogger()
	prefixed := h.prependIterationContext(context.Background(), "loop-x", 1, 10, nil)
	if len(prefixed) != 1 {
		t.Fatalf("len = %d, want 1 (failure surfaces as no todo block, not as a panic)", len(prefixed))
	}
}

// TestMessageHandler_PrependIterationContext_MissingPlatformSkipsRead
// verifies that an unset platform (e.g. unit test that didn't call
// SetPlatform) doesn't attempt to construct a loop entity ID — the
// helper returns the budget message only.
func TestMessageHandler_PrependIterationContext_MissingPlatformSkipsRead(t *testing.T) {
	h := &MessageHandler{
		config: Config{},
		// platform intentionally zero-valued
		todoReader: &fakeTodoReader{
			todos: []TodoState{{ID: "1", Content: "should not appear", Status: "pending"}},
		},
	}
	h.logger = todoTestLogger()
	prefixed := h.prependIterationContext(context.Background(), "loop-x", 1, 10, nil)
	if len(prefixed) != 1 {
		t.Fatalf("len = %d, want 1 (zero platform skips the todo read)", len(prefixed))
	}
	if strings.Contains(prefixed[0].Content, "should not appear") {
		t.Errorf("zero-platform path must not invoke the reader; got %q", prefixed[0].Content)
	}
}
