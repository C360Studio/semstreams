package graphresearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/google/uuid"
)

// fakeResearchKVWriter records the two writes the research_graph tool
// emits so tests can assert on key, value shape, and ordering without
// a real NATS connection.
type fakeResearchKVWriter struct {
	mu sync.Mutex

	loopEntityCalled bool
	loopEntityKey    string
	loopEntityValue  []byte
	loopEntityErr    error

	triggerCalled bool
	triggerKey    string
	triggerValue  []byte
	triggerErr    error

	// writeOrder records the sequence the tool used. Ordering matters:
	// the LoopEntity must land before the trigger key so R0's downstream
	// actions don't read an empty loop record.
	writeOrder []string
}

func (f *fakeResearchKVWriter) CreateLoopEntity(_ context.Context, loopID string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loopEntityCalled = true
	f.loopEntityKey = loopID
	f.loopEntityValue = append([]byte(nil), value...)
	f.writeOrder = append(f.writeOrder, "loop_entity")
	return f.loopEntityErr
}

func (f *fakeResearchKVWriter) PutResearchTrigger(_ context.Context, loopID string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggerCalled = true
	f.triggerKey = loopID
	f.triggerValue = append([]byte(nil), value...)
	f.writeOrder = append(f.writeOrder, "trigger")
	return f.triggerErr
}

func newTestExecutor(writer *fakeResearchKVWriter) *ResearchGraphExecutor {
	return NewResearchGraphExecutor(
		writer,
		component.PlatformMeta{Org: "c360", Platform: "ops"},
		WithResearchGraphClock(func() time.Time {
			return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
		}),
	)
}

func TestResearchGraphExecutor_ListTools(t *testing.T) {
	e := newTestExecutor(&fakeResearchKVWriter{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != ResearchGraphToolName {
		t.Errorf("name = %q, want %q", tool.Name, ResearchGraphToolName)
	}
	required, _ := tool.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "topic" {
		t.Errorf("required = %v, want [\"topic\"]", required)
	}
	if !tool.Strict {
		t.Errorf("Strict = false, want true (ADR-035 strict tool calling)")
	}
}

// runHappyPath dispatches a fully-specified research_graph call and
// returns the tool result for the caller to drill into. Lives outside
// the test bodies so individual t.Run subtests can focus on one
// observable each, keeping the per-function statement count under the
// revive function-length limit.
func runHappyPath(t *testing.T) (*ResearchGraphExecutor, *fakeResearchKVWriter, agentic.ToolResult) {
	t.Helper()
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "call-1",
		Name:   ResearchGraphToolName,
		LoopID: "parent_loop_42",
		Arguments: map[string]any{
			"topic": "drone hover anomalies",
			"hints": map[string]any{
				"entity_kind": "drone",
				"domain":      "robotics",
			},
			"budget_tokens":  float64(8000),
			"max_iterations": float64(6),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}
	return e, writer, res
}

func TestResearchGraphExecutor_HappyPath_ToolResult(t *testing.T) {
	_, writer, res := runHappyPath(t)

	if !res.StopLoop {
		t.Errorf("StopLoop = false, want true (the chain takes over from here)")
	}
	if !writer.loopEntityCalled {
		t.Fatalf("expected CreateLoopEntity to be called")
	}
	if !writer.triggerCalled {
		t.Fatalf("expected PutResearchTrigger to be called")
	}
	// The loop ID is framework-minted (ADR-105), so the test reads it back from
	// the KV key the tool just wrote rather than injecting a known one — there is
	// no generator seam to inject through, by design.
	if res.Metadata["loop_id"] != writer.loopEntityKey {
		t.Errorf("metadata loop_id = %v, want the minted key %q", res.Metadata["loop_id"], writer.loopEntityKey)
	}
	if res.Metadata["parent_loop_id"] != "parent_loop_42" {
		t.Errorf("metadata parent_loop_id = %v, want parent_loop_42", res.Metadata["parent_loop_id"])
	}
	if !strings.Contains(res.Content, writer.loopEntityKey) {
		t.Errorf("Content missing loop_id %q: %q", writer.loopEntityKey, res.Content)
	}
}

func TestResearchGraphExecutor_HappyPath_WriteOrdering(t *testing.T) {
	// R0 watches the trigger key and may dispatch immediately on it;
	// the LoopEntity record must already be in place when R0 fires,
	// otherwise downstream actions read a missing loop. Lock the
	// ordering in here so a future refactor can't reverse it
	// silently.
	_, writer, _ := runHappyPath(t)

	if len(writer.writeOrder) != 2 || writer.writeOrder[0] != "loop_entity" || writer.writeOrder[1] != "trigger" {
		t.Errorf("write order = %v, want [loop_entity, trigger]", writer.writeOrder)
	}
	if writer.triggerKey != writer.loopEntityKey {
		t.Errorf("trigger key = %q, want the loop entity key %q", writer.triggerKey, writer.loopEntityKey)
	}
}

func TestResearchGraphExecutor_HappyPath_LoopEntityShape(t *testing.T) {
	_, writer, _ := runHappyPath(t)

	var loopEntity agentic.LoopEntity
	if err := json.Unmarshal(writer.loopEntityValue, &loopEntity); err != nil {
		t.Fatalf("unmarshal loop entity: %v", err)
	}
	if loopEntity.Role != research.PipelineRole {
		t.Errorf("loop role = %q, want %q", loopEntity.Role, research.PipelineRole)
	}
	if loopEntity.ParentLoopID != "parent_loop_42" {
		t.Errorf("parent_loop_id = %q, want %q", loopEntity.ParentLoopID, "parent_loop_42")
	}
	if loopEntity.MaxIterations != 6 {
		t.Errorf("max_iterations: got %d, want 6", loopEntity.MaxIterations)
	}
	if loopEntity.State != agentic.LoopStateExecuting {
		t.Errorf("state = %q, want %q", loopEntity.State, agentic.LoopStateExecuting)
	}
}

func TestResearchGraphExecutor_HappyPath_TriggerIntent(t *testing.T) {
	// Trigger value decodes through the production registry as a
	// *research.Intent — proves the registry wiring landed
	// (feedback_production_decoder_round_trip_required).
	_, writer, _ := runHappyPath(t)

	decoder := newResearchDecoder(t)
	decoded, err := decoder.Decode(writer.triggerValue)
	if err != nil {
		t.Fatalf("decode trigger payload: %v\nwire: %s", err, writer.triggerValue)
	}
	intent, ok := decoded.Payload().(*research.Intent)
	if !ok {
		t.Fatalf("trigger payload = %T, want *research.Intent", decoded.Payload())
	}
	if intent.Topic != "drone hover anomalies" {
		t.Errorf("intent topic = %q, want %q", intent.Topic, "drone hover anomalies")
	}
	if intent.Hints["entity_kind"] != "drone" || intent.Hints["domain"] != "robotics" {
		t.Errorf("intent hints drifted: %v", intent.Hints)
	}
	if intent.BudgetTokens != 8000 {
		t.Errorf("intent budget_tokens = %d, want 8000", intent.BudgetTokens)
	}
	if intent.MaxIterations != 6 {
		t.Errorf("intent max_iterations = %d, want 6", intent.MaxIterations)
	}
}

func TestResearchGraphExecutor_DefaultsFillWhenOmitted(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		LoopID:    "parent_loop_7",
		Arguments: map[string]any{"topic": "x"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}

	if res.Metadata["budget_tokens"] != research.DefaultBudgetTokens {
		t.Errorf("budget_tokens default = %v, want %d", res.Metadata["budget_tokens"], research.DefaultBudgetTokens)
	}
	if res.Metadata["max_iterations"] != research.DefaultMaxIterations {
		t.Errorf("max_iterations default = %v, want %d", res.Metadata["max_iterations"], research.DefaultMaxIterations)
	}

	var loop agentic.LoopEntity
	if err := json.Unmarshal(writer.loopEntityValue, &loop); err != nil {
		t.Fatalf("unmarshal loop: %v", err)
	}
	if loop.MaxIterations != research.DefaultMaxIterations {
		t.Errorf("loop max_iterations = %d, want %d", loop.MaxIterations, research.DefaultMaxIterations)
	}
}

// TestResearchGraphExecutor_DefaultsPersistOnTriggerPayload locks in
// the post-go-reviewer-pass fix: zero-valued budget/max_iterations
// from the caller resolve to defaults BEFORE the intent envelope is
// marshalled, so downstream components reading the trigger key see
// the same values the LoopEntity and ToolResult metadata carry. The
// pre-fix shape persisted zeros into the trigger payload — a
// five-PR footgun (every consumer would have to remember
// ResolvedXxx()).
func TestResearchGraphExecutor_DefaultsPersistOnTriggerPayload(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		LoopID:    "parent_loop_7",
		Arguments: map[string]any{"topic": "x"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decoder := newResearchDecoder(t)
	decoded, err := decoder.Decode(writer.triggerValue)
	if err != nil {
		t.Fatalf("decode trigger: %v", err)
	}
	intent, ok := decoded.Payload().(*research.Intent)
	if !ok {
		t.Fatalf("trigger payload = %T, want *research.Intent", decoded.Payload())
	}
	if intent.BudgetTokens != research.DefaultBudgetTokens {
		t.Errorf("persisted budget_tokens = %d, want default %d (caller omitted; defaults must resolve before persistence)", intent.BudgetTokens, research.DefaultBudgetTokens)
	}
	if intent.MaxIterations != research.DefaultMaxIterations {
		t.Errorf("persisted max_iterations = %d, want default %d", intent.MaxIterations, research.DefaultMaxIterations)
	}
}

func TestResearchGraphExecutor_RejectsMissingTopic(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		LoopID:    "parent_loop_7",
		Arguments: map[string]any{},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorInvalidArgs)
	}
	if writer.loopEntityCalled {
		t.Errorf("CreateLoopEntity must not fire on invalid args")
	}
}

func TestResearchGraphExecutor_RejectsMalformedHints(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   ResearchGraphToolName,
		LoopID: "parent_loop_7",
		Arguments: map[string]any{
			"topic": "x",
			"hints": map[string]any{
				"entity_kind": 42, // wrong type
			},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorInvalidArgs)
	}
}

func TestResearchGraphExecutor_RejectsFractionalBudget(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   ResearchGraphToolName,
		LoopID: "parent_loop_7",
		Arguments: map[string]any{
			"topic":         "x",
			"budget_tokens": 4000.5,
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorInvalidArgs)
	}
}

func TestResearchGraphExecutor_RejectsMissingLoopID(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		Arguments: map[string]any{"topic": "x"},
		// LoopID intentionally omitted
	})
	if err == nil {
		t.Errorf("expected error return when loop_id is missing")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorInternal)
	}
	if writer.loopEntityCalled {
		t.Errorf("CreateLoopEntity must not fire when loop_id is missing")
	}
}

func TestResearchGraphExecutor_RoutesUnknownTool(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   "not_research_graph",
		LoopID: "parent",
		Arguments: map[string]any{
			"topic": "x",
		},
	})
	if err == nil {
		t.Errorf("expected error return for unknown tool name")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorNotFound)
	}
}

func TestResearchGraphExecutor_LoopEntityWriteFailureSurfaces(t *testing.T) {
	writer := &fakeResearchKVWriter{
		loopEntityErr: errors.New("kv unreachable"),
	}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		LoopID:    "parent",
		Arguments: map[string]any{"topic": "x"},
	})
	if err == nil {
		t.Errorf("expected error return on LoopEntity write failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorNetwork)
	}
	if writer.triggerCalled {
		t.Errorf("PutResearchTrigger must not fire after LoopEntity write fails — would orphan the trigger key")
	}
}

func TestResearchGraphExecutor_TriggerWriteFailureSurfaces(t *testing.T) {
	writer := &fakeResearchKVWriter{
		triggerErr: errors.New("kv unreachable"),
	}
	e := newTestExecutor(writer)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ResearchGraphToolName,
		LoopID:    "parent",
		Arguments: map[string]any{"topic": "x"},
	})
	if err == nil {
		t.Errorf("expected error return on trigger write failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("error kind = %q, want %q", res.ErrorKind, agentic.ToolErrorNetwork)
	}
}

func TestResearchGraphExecutor_TriggerPayloadDecodesViaProductionRegistry(t *testing.T) {
	// Belt-and-suspenders for feedback_production_decoder_round_trip_required:
	// the trigger value is the operator-reachable payload R0 reads, so
	// shape drift must surface in unit tests, not in PR 6's smoke.
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   ResearchGraphToolName,
		LoopID: "parent",
		Arguments: map[string]any{
			"topic": "voltage anomaly across battery packs",
			"hints": map[string]any{"recency": "last_24h"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	decoder := newResearchDecoder(t)
	decoded, err := decoder.Decode(writer.triggerValue)
	if err != nil {
		t.Fatalf("production decoder rejected trigger payload: %v", err)
	}
	if decoded.Type() != (message.Type{
		Domain:   research.Domain,
		Category: research.CategoryIntent,
		Version:  research.SchemaVersion,
	}) {
		t.Errorf("trigger type discriminator wrong: %+v", decoded.Type())
	}
}

func newResearchDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	registry := payloadregistry.New()
	if err := RegisterPayloads(registry); err != nil {
		t.Fatalf("register graph research payloads: %v", err)
	}
	return message.NewDecoder(registry)
}

// TestResearchLoopIDIsCanonicalUUID pins the research pipeline's mint to the
// loop-token contract (ADR-105, #1192). It used to mint "rg_" + uuid[:8] — 32
// bits, whose own comment conceded the odds — and offered an injectable
// generator that could author any shape at all. Both are gone: the test reads
// the token back from the AGENT_LOOPS key the tool wrote, because there is no
// longer any way to inject one.
func TestResearchLoopIDIsCanonicalUUID(t *testing.T) {
	writer := &fakeResearchKVWriter{}
	e := newTestExecutor(writer)

	// The calling (parent) loop's token is itself framework-minted.
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-uuid",
		Name:      ResearchGraphToolName,
		LoopID:    uuid.NewString(),
		Arguments: map[string]any{"topic": "drone hover anomalies"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}

	minted := writer.loopEntityKey
	if len(minted) != 36 {
		t.Fatalf("minted loop ID %q is %d bytes, want a 36-byte canonical UUID", minted, len(minted))
	}
	parsed, err := uuid.Parse(minted)
	if err != nil {
		t.Fatalf("minted loop ID %q does not parse as a UUID: %v", minted, err)
	}
	if parsed.String() != minted {
		t.Errorf("minted loop ID %q is not in canonical form (want %q)", minted, parsed.String())
	}
	// looptoken.Valid ignores the version bits by design, so this mint site is
	// the only place the spec's "framework-minted v4 UUID" clause can be pinned.
	if parsed.Version() != uuid.Version(4) {
		t.Errorf("minted loop ID %q is version %d, want a version 4 UUID", minted, parsed.Version())
	}
	if strings.HasPrefix(minted, "rg_") {
		t.Errorf("minted loop ID %q still carries the retired rg_ prefix", minted)
	}

	// Every place the tool publishes the token carries the same canonical value:
	// the AGENT_LOOPS record key, the trigger key R0 fires on, and the result the
	// model reads back.
	if writer.triggerKey != minted {
		t.Errorf("trigger key = %q, want the minted loop ID %q", writer.triggerKey, minted)
	}
	if res.Metadata["loop_id"] != minted {
		t.Errorf("metadata loop_id = %v, want the minted loop ID %q", res.Metadata["loop_id"], minted)
	}

	var loopEntity agentic.LoopEntity
	if err := json.Unmarshal(writer.loopEntityValue, &loopEntity); err != nil {
		t.Fatalf("unmarshal loop entity: %v", err)
	}
	if loopEntity.ID != minted {
		t.Errorf("loop entity ID = %q, want the minted loop ID %q", loopEntity.ID, minted)
	}

	// Two mints in a row must not collide — the whole point of retiring 32 bits.
	second := &fakeResearchKVWriter{}
	if _, err := newTestExecutor(second).Execute(context.Background(), agentic.ToolCall{
		ID:        "call-uuid-2",
		Name:      ResearchGraphToolName,
		LoopID:    uuid.NewString(),
		Arguments: map[string]any{"topic": "drone hover anomalies"},
	}); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if second.loopEntityKey == minted {
		t.Errorf("two mints produced the same loop ID %q", minted)
	}
}
