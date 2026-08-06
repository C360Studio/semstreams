package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
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

type exactTodoReaderFunc func(context.Context, string) (*graph.ExactEntity, error)

func (fn exactTodoReaderFunc) ReadExactEntity(ctx context.Context, id string) (*graph.ExactEntity, error) {
	return fn(ctx, id)
}

func TestNATSTodoReaderOnlyTreatsEntityNotFoundAsEmpty(t *testing.T) {
	for _, tt := range []struct {
		name      string
		code      string
		wantError bool
	}{
		{name: "not found", code: graph.ErrorCodeEntityNotFound},
		{name: "invalid ID", code: graph.ErrorCodeInvalidRequest, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &natsTodoReader{reader: exactTodoReaderFunc(func(context.Context, string) (*graph.ExactEntity, error) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, tt.code, errors.New(tt.name))
			})}
			_, err := reader.ReadTodos(context.Background(), todoFixtureLoopEntityID)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %t", err, tt.wantError)
			}
		})
	}
}

func TestReconstructTodos_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	items := []TodoState{
		{ID: "1", Content: "Survey rules", Status: "completed"},
		{ID: "2", Content: "Draft new rule", Status: "in_progress"},
		{ID: "3", Content: "Wire e2e test", Status: "pending"},
	}
	triples := make([]message.Triple, 0, len(items))
	for i, item := range items {
		triples = append(triples, todoRecordTriple(i, item.ID, item.Content, item.Status, now))
	}
	triples[0], triples[2] = triples[2], triples[0]
	got := ReconstructTodos(triples)
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
		{Subject: todoFixtureLoopEntityID, Predicate: "agent.loop.outcome", Object: "success", Timestamp: now},
		{Subject: todoFixtureLoopEntityID, Predicate: "rule.task.spawned", Object: "x", Timestamp: now},
	}
	if got := ReconstructTodos(mixed); got != nil {
		t.Errorf("non-todo triples → got %v, want nil", got)
	}
}

func TestNATSTodoReaderRejectsMalformedCompleteList(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	valid := todoRecordTriple(0, "ok", "valid", "pending", now)
	tests := []struct {
		name   string
		triple message.Triple
	}{
		{name: "non-string object", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: map[string]any{}, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "wrong datatype", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: valid.Object, Datatype: "xsd:string"}},
		{name: "unknown field", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"pending","position":1,"updated_at":"2026-05-09T12:00:00Z","extra":true}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "missing required field", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"pending","position":1}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "empty ID", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"","content":"x","status":"pending","position":1,"updated_at":"2026-05-09T12:00:00Z"}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "empty content", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"","status":"pending","position":1,"updated_at":"2026-05-09T12:00:00Z"}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "invalid status", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"blocked","position":1,"updated_at":"2026-05-09T12:00:00Z"}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "negative position", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"pending","position":-1,"updated_at":"2026-05-09T12:00:00Z"}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "invalid timestamp", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"pending","position":1,"updated_at":"not-a-time"}`, Datatype: agvocab.TodoRecordJSONDatatype}},
		{name: "trailing JSON", triple: message.Triple{Predicate: agvocab.TodoRecord, Object: `{"id":"bad","content":"x","status":"pending","position":1,"updated_at":"2026-05-09T12:00:00Z"}{}`, Datatype: agvocab.TodoRecordJSONDatatype}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &natsTodoReader{reader: exactTodoReaderFunc(func(context.Context, string) (*graph.ExactEntity, error) {
				return &graph.ExactEntity{Entity: &graph.EntityState{Triples: []message.Triple{valid, tt.triple}}, KVRevision: 1}, nil
			})}
			got, err := reader.ReadTodos(context.Background(), todoFixtureLoopEntityID)
			if err == nil {
				t.Fatal("malformed record must invalidate the complete list")
			}
			if !errors.Is(err, ErrMalformedTodoRecord) {
				t.Fatalf("error = %v, want errors.Is ErrMalformedTodoRecord", err)
			}
			if got != nil {
				t.Fatalf("malformed record returned partial list: %#v", got)
			}
		})
	}
}

func TestNATSTodoReaderRejectsDuplicateIDsAndPositions(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		records []message.Triple
	}{
		{name: "duplicate ID", records: []message.Triple{
			todoRecordTriple(0, "same", "first", "pending", now),
			todoRecordTriple(1, "same", "second", "completed", now),
		}},
		{name: "duplicate position", records: []message.Triple{
			todoRecordTriple(0, "one", "first", "pending", now),
			todoRecordTriple(0, "two", "second", "completed", now),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &natsTodoReader{reader: exactTodoReaderFunc(func(context.Context, string) (*graph.ExactEntity, error) {
				return &graph.ExactEntity{Entity: &graph.EntityState{Triples: tt.records}, KVRevision: 1}, nil
			})}
			got, err := reader.ReadTodos(context.Background(), todoFixtureLoopEntityID)
			if err == nil || got != nil {
				t.Fatalf("got (%#v, %v), want nil list and error", got, err)
			}
			if !errors.Is(err, ErrMalformedTodoRecord) {
				t.Fatalf("error = %v, want errors.Is ErrMalformedTodoRecord", err)
			}
		})
	}
}

func todoRecordTriple(position int, id, content, status string, updatedAt time.Time) message.Triple {
	return message.Triple{
		Subject:    todoFixtureLoopEntityID,
		Predicate:  agvocab.TodoRecord,
		Object:     `{"id":"` + id + `","content":"` + content + `","status":"` + status + `","position":` + fmt.Sprint(position) + `,"updated_at":"` + updatedAt.Format(time.RFC3339Nano) + `"}`,
		Datatype:   agvocab.TodoRecordJSONDatatype,
		Timestamp:  updatedAt,
		Confidence: 1,
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

func TestMessageHandler_TodoReadFailureLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantLevel string
		wantMsg   string
	}{
		{
			name:      "malformed persisted state warns",
			err:       fmt.Errorf("%w: duplicate position", ErrMalformedTodoRecord),
			wantLevel: "WARN",
			wantMsg:   "malformed todo state",
		},
		{
			name:      "transient read remains debug",
			err:       errTodoReadFailed,
			wantLevel: "DEBUG",
			wantMsg:   "todo read failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			h := &MessageHandler{
				config:     Config{},
				platform:   todoTestPlatform(),
				todoReader: &fakeTodoReader{err: tt.err},
				logger: slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})),
			}

			if msg := h.maybeBuildTodoMessage(context.Background(), "loop-x"); msg.Role != "" || msg.Content != "" {
				t.Fatalf("message = %#v, want zero value", msg)
			}
			logLine := output.String()
			if !strings.Contains(logLine, "level="+tt.wantLevel) || !strings.Contains(logLine, tt.wantMsg) {
				t.Fatalf("log = %q, want level=%s and message %q", logLine, tt.wantLevel, tt.wantMsg)
			}
		})
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
