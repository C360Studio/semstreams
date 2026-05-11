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
)

// newScratchpadExecutor builds a fully-mocked executor with a frozen
// clock and pinned ID so tests can assert deterministic triples.
func newScratchpadExecutor(pub TriplePublisher) *ScratchpadExecutor {
	e := NewScratchpadExecutor(pub, types.PlatformMeta{Org: "acme", Platform: "ops"})
	e.SetClock(func() time.Time { return time.Date(2026, 5, 12, 9, 15, 0, 0, time.UTC) })
	e.SetIDGenerator(func() string { return "scratch-uuid-fixed" })
	return e
}

func TestScratchpadExecutor_ListTools(t *testing.T) {
	e := newScratchpadExecutor(&recordingPublisher{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != ScratchpadToolName {
		t.Errorf("name = %q, want %q", tool.Name, ScratchpadToolName)
	}
	// Strict:false per semspec 2026-05-12 decision — locked in here so
	// a future drive-by toggle gets a flag from the test suite.
	if tool.Strict {
		t.Errorf("Strict = true, want false (per semspec design decision)")
	}
	// Required arg present and schema closed.
	params := tool.Parameters
	if params["additionalProperties"] != false {
		t.Errorf("additionalProperties should be false")
	}
	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "text" {
		t.Errorf("required = %v, want [\"text\"]", required)
	}
}

func TestScratchpadExecutor_HappyPath_EmitsFourTriples(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		Name:      ScratchpadToolName,
		LoopID:    "loop-abc",
		Arguments: map[string]any{"text": "Let me think: handle empty retry_hint."},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}

	// Four triples, all on the loop entity, all keyed to the fixed UUID
	// via co-emission (graph is a set-of-triples; downstream readers
	// group by timestamp / scratch_id to reconstruct one entry).
	if got := len(pub.triples); got != 4 {
		t.Fatalf("triples emitted = %d, want 4", got)
	}

	loopEntityID, err := agentic.TryLoopExecutionEntityID("acme", "ops", "loop-abc")
	if err != nil {
		t.Fatalf("loop entity id: %v", err)
	}
	byPredicate := map[string]any{}
	for _, tr := range pub.triples {
		if tr.Subject != loopEntityID {
			t.Errorf("triple subject = %q, want loop entity %q", tr.Subject, loopEntityID)
		}
		if tr.Source != scratchpadToolSource {
			t.Errorf("triple source = %q, want %q", tr.Source, scratchpadToolSource)
		}
		byPredicate[tr.Predicate] = tr.Object
	}
	if byPredicate[agvocab.ScratchID] != "scratch-uuid-fixed" {
		t.Errorf("ScratchID = %v, want fixed uuid", byPredicate[agvocab.ScratchID])
	}
	if byPredicate[agvocab.ScratchText] != "Let me think: handle empty retry_hint." {
		t.Errorf("ScratchText = %v, want literal", byPredicate[agvocab.ScratchText])
	}
	if byPredicate[agvocab.ScratchChars] != 38 {
		t.Errorf("ScratchChars = %v, want 38", byPredicate[agvocab.ScratchChars])
	}

	// Co-emission invariant: all four triples for one call share the
	// same Timestamp so downstream consumers can group by timestamp
	// (alternative to grouping by ScratchID) when reconstructing entries.
	// The eventual batch-publisher migration (reviewer's class-of-bug
	// note) preserves this property; pinning it in a test makes the
	// future change a visible delta.
	want := pub.triples[0].Timestamp
	for i, tr := range pub.triples {
		if !tr.Timestamp.Equal(want) {
			t.Errorf("triple %d timestamp = %v, want %v (co-emission invariant)", i, tr.Timestamp, want)
		}
	}

	// Confirmation payload — semspec asked for "short fixed confirmation"
	// rather than echo. Asserts the contract surface.
	var confirm scratchpadResult
	if err := json.Unmarshal([]byte(res.Content), &confirm); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if confirm.Status != "ok" {
		t.Errorf("status = %q, want ok", confirm.Status)
	}
	if confirm.ScratchID != "scratch-uuid-fixed" {
		t.Errorf("scratch_id = %q, want fixed uuid", confirm.ScratchID)
	}
	if confirm.Chars != 38 {
		t.Errorf("chars = %d, want 38", confirm.Chars)
	}
}

// TestScratchpadExecutor_AppendOnlyAcrossCalls confirms multiple
// scratchpad calls within one loop accumulate (NOT full-list-replace
// like write_todos). Each call mints a fresh scratch.id; the graph
// retains all four triples per call.
func TestScratchpadExecutor_AppendOnlyAcrossCalls(t *testing.T) {
	ids := []string{"id-1", "id-2"}
	idx := 0
	pub := &recordingPublisher{}
	e := NewScratchpadExecutor(pub, types.PlatformMeta{Org: "acme", Platform: "ops"})
	e.SetIDGenerator(func() string {
		out := ids[idx]
		idx++
		return out
	})
	// Drive the clock forward between calls so the test exercises the
	// "ordering recoverable via ScratchCreatedAt" claim. A frozen clock
	// would pass even if the implementation cached e.now() at boot.
	clocks := []time.Time{
		time.Date(2026, 5, 12, 9, 15, 0, 0, time.UTC),
		time.Date(2026, 5, 12, 9, 16, 30, 0, time.UTC),
	}
	clockIdx := 0
	e.SetClock(func() time.Time {
		t := clocks[clockIdx]
		clockIdx++
		return t
	})

	for _, text := range []string{"first thought", "second thought"} {
		if _, err := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{"text": text},
		}); err != nil {
			t.Fatalf("execute: %v", err)
		}
	}
	if got := len(pub.triples); got != 8 {
		t.Errorf("triples after two calls = %d, want 8 (4 per call, append-only)", got)
	}
	scratchIDs := map[string]int{}
	timestamps := map[time.Time]int{}
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.ScratchID {
			scratchIDs[tr.Object.(string)]++
		}
		timestamps[tr.Timestamp]++
	}
	if len(scratchIDs) != 2 {
		t.Errorf("distinct scratch IDs = %d, want 2", len(scratchIDs))
	}
	if len(timestamps) != 2 {
		t.Errorf("distinct timestamps = %d, want 2 (each call advances the clock)", len(timestamps))
	}
}

// scratchpadPartialFailurePublisher returns success for the first N
// AddTriple calls then surfaces an error on the (N+1)th. Records every
// triple it sees so tests can assert exactly which writes landed.
// Lives here (not next to recordingPublisher) because the existing
// shared recorder bails BEFORE recording on error and the partial-write
// invariant needs the opposite.
type scratchpadPartialFailurePublisher struct {
	failAfter int
	calls     int
	triples   []message.Triple
	failWith  error
}

func (p *scratchpadPartialFailurePublisher) AddTriple(_ context.Context, tr message.Triple) error {
	p.calls++
	if p.calls > p.failAfter {
		return p.failWith
	}
	p.triples = append(p.triples, tr)
	return nil
}

// TestScratchpadExecutor_PartialWrite_LeavesOrphanID locks in the
// documented partial-write behavior: a publish failure on triple 3
// leaves the first 2 triples in the graph (orphan ScratchID +
// ScratchText with no companions). The tool returns ToolErrorNetwork
// so the LLM sees the failure and retries with a fresh UUID; the
// orphan is harmless (no duplication). The eventual batch-publisher
// migration (go-reviewer follow-up) eliminates orphans entirely —
// this test makes that change a visible delta.
func TestScratchpadExecutor_PartialWrite_LeavesOrphanID(t *testing.T) {
	pub := &scratchpadPartialFailurePublisher{failAfter: 2, failWith: errors.New("nats degraded")}
	e := NewScratchpadExecutor(pub, types.PlatformMeta{Org: "acme", Platform: "ops"})
	e.SetIDGenerator(func() string { return "orphan-id" })

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{"text": "draft"},
	})
	if err == nil {
		t.Fatalf("expected wrapped transient error")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want network", res.ErrorKind)
	}
	if len(pub.triples) != 2 {
		t.Errorf("recorded triples = %d, want 2 (first two land, third fails)", len(pub.triples))
	}
	// The first two predicates emitted are ScratchID and ScratchText
	// (per scratchpad.go emission order). Locking that order in here
	// makes any future reorder a visible test delta.
	if pub.triples[0].Predicate != agvocab.ScratchID {
		t.Errorf("first emitted predicate = %q, want %q", pub.triples[0].Predicate, agvocab.ScratchID)
	}
	if pub.triples[1].Predicate != agvocab.ScratchText {
		t.Errorf("second emitted predicate = %q, want %q", pub.triples[1].Predicate, agvocab.ScratchText)
	}
}

func TestScratchpadExecutor_RejectsEmptyText(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{"text": ""},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want invalid_args", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "non-empty") {
		t.Errorf("Error = %q, want substring 'non-empty'", res.Error)
	}
	if len(pub.triples) != 0 {
		t.Errorf("rejected call should emit no triples, got %d", len(pub.triples))
	}
}

func TestScratchpadExecutor_RejectsMissingText(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want invalid_args", res.ErrorKind)
	}
}

func TestScratchpadExecutor_RejectsNonStringText(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{"text": 42},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want invalid_args", res.ErrorKind)
	}
}

func TestScratchpadExecutor_MissingLoopIDIsInternal(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, Arguments: map[string]any{"text": "hi"},
	})
	if err == nil {
		t.Errorf("expected wrapped Go error (dispatcher invariant violation)")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("ErrorKind = %v, want internal", res.ErrorKind)
	}
}

// TestScratchpadExecutor_PublishFailureSurfacesAsNetworkError confirms
// scratchpad follows the decide/write_todos discipline (fail the tool
// on publish error) rather than the web-tools log+continue discipline.
// Rationale: the trajectory alone is not durable across compaction, so
// the graph is the audit/recovery contract for this tool.
func TestScratchpadExecutor_PublishFailureSurfacesAsNetworkError(t *testing.T) {
	pub := &recordingPublisher{err: errors.New("nats degraded")}
	e := newScratchpadExecutor(pub)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: ScratchpadToolName, LoopID: "loop", Arguments: map[string]any{"text": "draft"},
	})
	if err == nil {
		t.Errorf("expected wrapped transient error")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want network", res.ErrorKind)
	}
}

func TestScratchpadExecutor_WrongNameIsRoutingBug(t *testing.T) {
	pub := &recordingPublisher{}
	e := newScratchpadExecutor(pub)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "not_scratchpad", LoopID: "loop",
	})
	if err == nil {
		t.Errorf("expected dispatcher routing error")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want not_found", res.ErrorKind)
	}
}
