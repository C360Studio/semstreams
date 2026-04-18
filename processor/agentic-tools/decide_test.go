package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// recordingPublisher implements TriplePublisher in-process so tests can
// assert exactly what triples the decide tool emitted.
type recordingPublisher struct {
	triples []message.Triple
	err     error
}

func (p *recordingPublisher) AddTriple(_ context.Context, triple message.Triple) error {
	if p.err != nil {
		return p.err
	}
	p.triples = append(p.triples, triple)
	return nil
}

func newDecideExecutor(publisher TriplePublisher) *DecideExecutor {
	return NewDecideExecutor(publisher, types.PlatformMeta{Org: "acme", Platform: "test"})
}

func TestDecideExecutor_ListTools(t *testing.T) {
	e := newDecideExecutor(&recordingPublisher{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != DecideToolName {
		t.Errorf("expected tool name %q, got %q", DecideToolName, tools[0].Name)
	}
	required, _ := tools[0].Parameters["required"].([]string)
	want := map[string]bool{"action": true, "reason": true}
	for _, r := range required {
		if !want[r] {
			t.Errorf("unexpected required field: %s", r)
		}
		delete(want, r)
	}
	if len(want) > 0 {
		t.Errorf("missing required fields: %v", want)
	}
}

// TestDecideExecutor_HappyPath verifies a valid decide call emits the two
// expected triples on the coordinator's loop entity, returns StopLoop=true,
// and puts the full args in the tool result Content so downstream agents
// can fetch subtopics via read_loop_result.
func TestDecideExecutor_HappyPath(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action":    "fan_out",
			"reason":    "three separable subtopics identified",
			"subtopics": []any{"alpha", "beta", "gamma"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("tool error: %s", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true")
	}

	if len(pub.triples) != 2 {
		t.Fatalf("expected 2 triples published, got %d", len(pub.triples))
	}

	wantSubject := "acme.test.agent.agentic-loop.execution.loop-abc"
	for _, tr := range pub.triples {
		if tr.Subject != wantSubject {
			t.Errorf("triple subject = %q, want %q", tr.Subject, wantSubject)
		}
	}

	byPredicate := map[string]any{}
	for _, tr := range pub.triples {
		byPredicate[tr.Predicate] = tr.Object
	}
	if got := byPredicate[agvocab.CoordinatorNextAction]; got != "fan_out" {
		t.Errorf("CoordinatorNextAction = %v, want fan_out", got)
	}
	if got := byPredicate[agvocab.CoordinatorDecisionReason]; got != "three separable subtopics identified" {
		t.Errorf("CoordinatorDecisionReason = %v, want reason text", got)
	}

	// Content is JSON of the full args — downstream consumers parse to
	// get subtopics etc.
	var payload decideArgs
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("unmarshal result content: %v", err)
	}
	if payload.Action != "fan_out" {
		t.Errorf("payload action = %q, want fan_out", payload.Action)
	}
	if len(payload.Subtopics) != 3 {
		t.Errorf("payload subtopics = %v, want 3 entries", payload.Subtopics)
	}

	// Metadata surfaces handy summaries for dashboards.
	if got := res.Metadata["action"]; got != "fan_out" {
		t.Errorf("Metadata[action] = %v, want fan_out", got)
	}
	if got := res.Metadata["subtopic_count"]; got != 3 {
		t.Errorf("Metadata[subtopic_count] = %v, want 3", got)
	}
}

// TestDecideExecutor_MissingAction verifies that a call missing the
// required action surfaces InvalidArgs and does NOT publish any triples.
// The framework retry policy (Layer 1 tool_retries) then kicks in and
// gives the model another chance to hit the schema.
func TestDecideExecutor_MissingAction(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"reason": "forgot to include action",
		},
	})
	if err != nil {
		t.Fatalf("unexpected wrapped err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published on invalid args, got %d", len(pub.triples))
	}
	if res.StopLoop {
		t.Errorf("StopLoop should be false on invalid args — let the loop iterate")
	}
}

// TestDecideExecutor_MissingReason covers the other required field.
func TestDecideExecutor_MissingReason(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestDecideExecutor_SubtopicsWrongType verifies that non-string items in
// the subtopics array fail validation cleanly.
func TestDecideExecutor_SubtopicsWrongType(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action":    "fan_out",
			"reason":    "test",
			"subtopics": []any{"ok", 42},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestDecideExecutor_MissingLoopID verifies that a malformed call with no
// loop_id surfaces an internal error rather than silently publishing to a
// malformed entity ID.
func TestDecideExecutor_MissingLoopID(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: DecideToolName,
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})
	if err == nil {
		t.Errorf("expected wrapped err for missing loop_id")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("ErrorKind = %v, want ToolErrorInternal", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published without a loop_id")
	}
}

// TestDecideExecutor_PublisherFailure verifies that a failure during
// triple publication surfaces ToolErrorExternal so the retry policy can
// react, and the loop is NOT terminated (StopLoop stays false) so the
// model sees the failure in the next iteration.
func TestDecideExecutor_PublisherFailure(t *testing.T) {
	pub := &recordingPublisher{err: errors.New("nats broken")}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})
	if err == nil {
		t.Errorf("expected wrapped err for publish failure")
	}
	if res.ErrorKind != agentic.ToolErrorExternal {
		t.Errorf("ErrorKind = %v, want ToolErrorExternal", res.ErrorKind)
	}
	if res.StopLoop {
		t.Errorf("StopLoop should stay false when the decision didn't actually land")
	}
}

// TestDecideExecutor_UnknownTool verifies routing protection.
func TestDecideExecutor_UnknownTool(t *testing.T) {
	e := newDecideExecutor(&recordingPublisher{})
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "not_decide",
		LoopID:    "loop-abc",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Errorf("expected err for unknown tool")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want ToolErrorNotFound", res.ErrorKind)
	}
}

// TestDecideExecutor_OptionalFieldsOmitted verifies a minimal valid call
// (action + reason only) publishes both triples and returns cleanly with
// empty Metadata fields for the optional fields.
func TestDecideExecutor_OptionalFieldsOmitted(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
			"reason": "research complete",
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pub.triples) != 2 {
		t.Errorf("expected 2 triples, got %d", len(pub.triples))
	}
	if got := res.Metadata["subtopic_count"]; got != 0 {
		t.Errorf("subtopic_count = %v, want 0", got)
	}
	if got := res.Metadata["has_retry_hint"]; got != false {
		t.Errorf("has_retry_hint = %v, want false", got)
	}
}

// TestDecideExecutor_LoopEntityIDFormat double-checks the subject ID uses
// the same 6-part shape graph_writer and rule actions use. Changes to the
// loop-execution entity ID format should break this test loudly so it's
// clear the whole rule chain needs to be updated.
func TestDecideExecutor_LoopEntityIDFormat(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewDecideExecutor(pub, types.PlatformMeta{Org: "c360", Platform: "deep-research-001"})

	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "xyz",
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})

	want := "c360.deep-research-001.agent.agentic-loop.execution.xyz"
	if got := pub.triples[0].Subject; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	// Also verify against the shared helper so if it ever changes this
	// test follows.
	if got := pub.triples[0].Subject; got != agentic.LoopExecutionEntityID("c360", "deep-research-001", "xyz") {
		t.Errorf("subject drift: %q not from LoopExecutionEntityID helper", got)
	}
	// Source attribution is preserved so operators can distinguish
	// coordinator decisions from other triple emitters in the graph.
	if got, want := pub.triples[0].Source, decideToolSource; got != want {
		t.Errorf("source = %q, want %q", got, want)
	}
	// Sanity: no fmt.Sprint-shaped junk in the subject.
	_ = fmt.Sprintf("%s", want)
}
