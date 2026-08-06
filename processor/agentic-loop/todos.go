package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// readTodosTimeout bounds the per-iteration todo-reconstruction read.
// A transient failure omits the todo block for one iteration; the next
// iteration retries. Persistent malformed records are warned by the handler. ADR-036
// §Stage 4 favours availability over consistency: a one-iteration
// omission is better than blocking the whole loop on a transient
// graph-gateway blip.
const readTodosTimeout = 2 * time.Second

// ErrMalformedTodoRecord classifies a TodoReader failure caused by any invalid
// agent.todo.record triple. The reader returns no partial list with this error.
var ErrMalformedTodoRecord = errors.New("malformed todo record")

// TodoState is the reconstructed logical shape of one todo item. Its storage
// encoding is private to TodoReader.
type TodoState struct {
	ID        string
	Content   string
	Status    string
	Position  int
	UpdatedAt string
}

// TodoReader is the narrow surface MessageHandler uses to fetch the
// current todo list for a loop. Production satisfies it via a NATS
// adapter against graph.ingest.query.entity; tests substitute an
// in-memory implementation.
type TodoReader interface {
	ReadTodos(ctx context.Context, loopEntityID string) ([]TodoState, error)
}

// natsTodoReader adapts natsclient.Client to TodoReader by issuing a
// graph.ingest.query.entity request and reconstructing TodoState
// values from the entity's triples. Treats "entity not found" as an
// empty list (the loop has never written todos).
type natsTodoReader struct {
	reader graph.ExactEntityReader
}

// NewNATSTodoReader builds a TodoReader backed by the
// graph.ingest.query.entity NATS surface.
func NewNATSTodoReader(client *natsclient.Client) TodoReader {
	return &natsTodoReader{reader: graph.NewExactEntityReader(client, readTodosTimeout)}
}

func (r *natsTodoReader) ReadTodos(ctx context.Context, loopEntityID string) ([]TodoState, error) {
	exact, err := r.reader.ReadExactEntity(ctx, loopEntityID)
	if err != nil {
		var classified *errs.ClassifiedError
		if errors.As(err, &classified) && classified.Code == graph.ErrorCodeEntityNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("read todo entity %s: %w", loopEntityID, err)
	}
	todos, err := reconstructTodos(exact.Entity.Triples)
	if err != nil {
		return nil, fmt.Errorf("%w on entity %s: %v", ErrMalformedTodoRecord, loopEntityID, err)
	}
	return todos, nil
}

// ReconstructTodos rebuilds the ordered todo list from record triples. It
// preserves its historical helper signature for package callers; malformed
// storage returns no list. Production TodoReader uses the checked decoder below
// and returns the corresponding error.
func ReconstructTodos(triples []message.Triple) []TodoState {
	todos, err := reconstructTodos(triples)
	if err != nil {
		return nil
	}
	return todos
}

func reconstructTodos(triples []message.Triple) ([]TodoState, error) {
	out := make([]TodoState, 0, len(triples))
	seenIDs := make(map[string]struct{})
	seenPositions := make(map[int]struct{})
	for index, triple := range triples {
		if triple.Predicate != agvocab.TodoRecord {
			continue
		}
		state, err := decodeTodoRecord(triple)
		if err != nil {
			return nil, fmt.Errorf("record triple %d: %w", index, err)
		}
		if _, duplicate := seenIDs[state.ID]; duplicate {
			return nil, fmt.Errorf("record triple %d: duplicate todo id %q", index, state.ID)
		}
		if _, duplicate := seenPositions[state.Position]; duplicate {
			return nil, fmt.Errorf("record triple %d: duplicate todo position %d", index, state.Position)
		}
		seenIDs[state.ID] = struct{}{}
		seenPositions[state.Position] = struct{}{}
		out = append(out, state)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

type storedTodoRecord struct {
	ID        *string `json:"id"`
	Content   *string `json:"content"`
	Status    *string `json:"status"`
	Position  *int    `json:"position"`
	UpdatedAt *string `json:"updated_at"`
}

func decodeTodoRecord(triple message.Triple) (TodoState, error) {
	if triple.Datatype != agvocab.TodoRecordJSONDatatype {
		return TodoState{}, fmt.Errorf("datatype %q, want %q", triple.Datatype, agvocab.TodoRecordJSONDatatype)
	}
	encoded, ok := triple.Object.(string)
	if !ok {
		return TodoState{}, fmt.Errorf("object has type %T, want JSON string", triple.Object)
	}

	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var record storedTodoRecord
	if err := decoder.Decode(&record); err != nil {
		return TodoState{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return TodoState{}, errors.New("decode JSON: multiple values")
		}
		return TodoState{}, fmt.Errorf("decode JSON trailing data: %w", err)
	}
	if record.ID == nil || record.Content == nil || record.Status == nil ||
		record.Position == nil || record.UpdatedAt == nil {
		return TodoState{}, errors.New("record requires id, content, status, position, and updated_at")
	}
	if *record.ID == "" {
		return TodoState{}, errors.New("id must be non-empty")
	}
	if *record.Content == "" {
		return TodoState{}, errors.New("content must be non-empty")
	}
	switch *record.Status {
	case "pending", "in_progress", "completed":
	default:
		return TodoState{}, fmt.Errorf("status %q is invalid", *record.Status)
	}
	if *record.Position < 0 {
		return TodoState{}, errors.New("position must be non-negative")
	}
	if _, err := time.Parse(time.RFC3339Nano, *record.UpdatedAt); err != nil {
		return TodoState{}, fmt.Errorf("updated_at must be RFC3339: %w", err)
	}

	return TodoState{
		ID: *record.ID, Content: *record.Content, Status: *record.Status,
		Position: *record.Position, UpdatedAt: *record.UpdatedAt,
	}, nil
}

// tripleObjectAsString is shared by other agentic-loop graph projections.
func tripleObjectAsString(object any) (string, bool) {
	switch value := object.(type) {
	case string:
		return value, true
	case fmt.Stringer:
		return value.String(), true
	default:
		return "", false
	}
}

// BuildTodoStateMessage formats the current todo list as a system
// message the agent sees at the top of its iteration. Empty list
// returns the zero ChatMessage (handler skips appending).
//
// Format is compact and mirrors Claude Code's TodoWrite display:
// per-status marker + content, one per line. Operators reading the
// prompt see what the agent sees.
func BuildTodoStateMessage(todos []TodoState) agentic.ChatMessage {
	if len(todos) == 0 {
		return agentic.ChatMessage{}
	}

	var b strings.Builder
	b.WriteString("[Working list — your private working memory; you maintain this via write_todos]\n")
	for _, t := range todos {
		fmt.Fprintf(&b, "%s %s\n", todoStatusMarker(t.Status), t.Content)
	}
	return agentic.ChatMessage{Role: "system", Content: strings.TrimRight(b.String(), "\n")}
}

func todoStatusMarker(status string) string {
	switch status {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	case "pending":
		return "[ ]"
	}
	return "[?]"
}
