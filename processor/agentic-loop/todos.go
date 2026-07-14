package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
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

// queryEntitySubject is the NATS subject for single-entity reads from
// graph-ingest. Mirrors processor/graph-ingest/query.go.
const queryEntitySubject = "graph.ingest.query.entity"

// readTodosTimeout bounds the per-iteration todo-reconstruction read.
// Failure surfaces silently — the model just doesn't see the todo
// block for this iteration; the next iteration retries. ADR-036
// §Stage 4 favours availability over consistency: a one-iteration
// omission is better than blocking the whole loop on a transient
// graph-gateway blip.
const readTodosTimeout = 2 * time.Second

// todoTripleStride is the count of agent.todo.* triples per todo
// item. write_todos writes [id, content, status, position,
// updated_at] interleaved per item in array order; the assembler
// reads them back in the same stride.
const todoTripleStride = 5

// TodoState is the reconstructed shape of one todo item, assembled
// from the five agent.todo.* triples on a loop entity.
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
	client *natsclient.Client
}

// NewNATSTodoReader builds a TodoReader backed by the
// graph.ingest.query.entity NATS surface.
func NewNATSTodoReader(client *natsclient.Client) TodoReader {
	return &natsTodoReader{client: client}
}

func (r *natsTodoReader) ReadTodos(ctx context.Context, loopEntityID string) ([]TodoState, error) {
	req := struct {
		ID string `json:"id"`
	}{ID: loopEntityID}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal query request: %w", err)
	}

	respData, err := r.client.RequestClassified(ctx, queryEntitySubject, reqData, readTodosTimeout)
	if err != nil {
		// gh#93: RequestClassified unifies transport AND handler
		// errors behind one classified return. Handler-side
		// "not found" classifies as Invalid (graph-ingest's
		// handleQueryEntityNATS returns errs.Classified(ErrorInvalid,
		// "not found: <id>") for missing entities) — fail-open with
		// an empty list so a fresh loop's first iteration doesn't
		// emit Debug noise. Transient/transport errors propagate.
		if errs.IsInvalid(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("request %s: %w", queryEntitySubject, err)
	}

	return parseQueryEntityTodos(respData)
}

// parseQueryEntityTodos decodes a graph.ingest.query.entity response
// payload into TodoState values. As of gh#93 Phase 2 the caller
// (ReadTodos) routes through natsclient.RequestClassified, which
// surfaces handler-side errors via err return — this function only
// sees success-path bytes.
func parseQueryEntityTodos(respData []byte) ([]TodoState, error) {
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(respData, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity: %w", err)
	}

	return ReconstructTodos(entity.Triples), nil
}

// ReconstructTodos rebuilds the ordered todo list from a loop
// entity's triples. Filters to agent.todo.* predicates, groups them
// into todo items, and returns the items sorted by Position (the
// agent's original array order).
//
// Stage 3's write_todos appends triples in the interleaved order
// [id, content, status, position, updated_at] for each item in array
// order, then graph-ingest preserves arrival order in the entity's
// triple slice. So filtered triples come back in groups of five with
// a known per-group field order. We parse in stride and skip any
// malformed group (partial mid-write state, e.g. remove-then-add
// failure with stale triples on disk).
func ReconstructTodos(triples []message.Triple) []TodoState {
	filtered := filterTodoTriples(triples)
	if len(filtered) < todoTripleStride {
		return nil
	}

	out := make([]TodoState, 0, len(filtered)/todoTripleStride)
	for start := 0; start+todoTripleStride <= len(filtered); start += todoTripleStride {
		group := filtered[start : start+todoTripleStride]
		state, ok := projectTodoGroup(group)
		if !ok {
			continue
		}
		out = append(out, state)
	}

	// Sort by Position for the canonical output order. The triple
	// order on disk is usually already in position order (write_todos
	// emits them that way), but sorting defends against append-after-
	// partial-clear states where the second batch's triples follow
	// stale ones from a half-cleared first batch.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// filterTodoTriples drops any triple whose Predicate is outside the
// agent.todo.* family, preserving slice order.
func filterTodoTriples(triples []message.Triple) []message.Triple {
	out := make([]message.Triple, 0, len(triples))
	for _, t := range triples {
		switch t.Predicate {
		case agvocab.TodoID, agvocab.TodoContent, agvocab.TodoStatus, agvocab.TodoPosition, agvocab.TodoUpdatedAt:
			out = append(out, t)
		}
	}
	return out
}

// projectTodoGroup maps a group of five triples in canonical order
// (id, content, status, position, updated_at) onto a TodoState.
// Returns ok=false when the group's predicate sequence doesn't match
// the canonical order — caller skips that group.
func projectTodoGroup(group []message.Triple) (TodoState, bool) {
	if len(group) != todoTripleStride {
		return TodoState{}, false
	}
	expected := [todoTripleStride]string{
		agvocab.TodoID,
		agvocab.TodoContent,
		agvocab.TodoStatus,
		agvocab.TodoPosition,
		agvocab.TodoUpdatedAt,
	}
	for i, t := range group {
		if t.Predicate != expected[i] {
			return TodoState{}, false
		}
	}
	id, ok := tripleObjectAsString(group[0].Object)
	if !ok || id == "" {
		return TodoState{}, false
	}
	content, ok := tripleObjectAsString(group[1].Object)
	if !ok || content == "" {
		return TodoState{}, false
	}
	status, ok := tripleObjectAsString(group[2].Object)
	if !ok || status == "" {
		return TodoState{}, false
	}
	position, ok := tripleObjectAsInt(group[3].Object)
	if !ok {
		return TodoState{}, false
	}
	updatedAt, _ := tripleObjectAsString(group[4].Object)

	return TodoState{
		ID:        id,
		Content:   content,
		Status:    status,
		Position:  position,
		UpdatedAt: updatedAt,
	}, true
}

// tripleObjectAsString coerces a triple Object to string.
func tripleObjectAsString(o any) (string, bool) {
	switch v := o.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	}
	return "", false
}

// tripleObjectAsInt coerces a triple Object to int. Position
// triples are written as int but JSON round-trips through float64.
func tripleObjectAsInt(o any) (int, bool) {
	switch v := o.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n), true
		}
	}
	return 0, false
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
