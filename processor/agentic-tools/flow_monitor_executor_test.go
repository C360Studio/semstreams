package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// fakeLoopKV is an in-memory LoopKVScanner for unit tests.
type fakeLoopKV struct {
	entries map[string][]byte
	keysErr error
	getErr  map[string]error
}

func newFakeLoopKV() *fakeLoopKV {
	return &fakeLoopKV{
		entries: map[string][]byte{},
		getErr:  map[string]error{},
	}
}

func (f *fakeLoopKV) putEvent(key string, evt agentic.LoopCompletedEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	f.entries[key] = data
	return nil
}

func (f *fakeLoopKV) putRaw(key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.entries[key] = data
	return nil
}

func (f *fakeLoopKV) Keys(_ context.Context) ([]string, error) {
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	keys := make([]string, 0, len(f.entries))
	for k := range f.entries {
		keys = append(keys, k)
	}
	return keys, nil
}

func (f *fakeLoopKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	if err := f.getErr[key]; err != nil {
		return nil, err
	}
	v, ok := f.entries[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: v}, nil
}

// fakeFlowStateReader is an in-memory FlowStateReader.
type fakeFlowStateReader struct {
	states map[string]FlowState
	getErr error
}

func newFakeFlowStateReader() *fakeFlowStateReader {
	return &fakeFlowStateReader{states: map[string]FlowState{}}
}

func (f *fakeFlowStateReader) Get(_ context.Context, id string) (FlowState, error) {
	if f.getErr != nil {
		return FlowState{}, f.getErr
	}
	s, ok := f.states[id]
	if !ok {
		return FlowState{}, errors.New("not found")
	}
	return s, nil
}

// seedCompletedLoop adds a COMPLETE_ entry to the fake KV.
func seedCompletedLoop(t *testing.T, kv *fakeLoopKV, evt agentic.LoopCompletedEvent) {
	t.Helper()
	if err := kv.putEvent(completeKeyPrefix+evt.LoopID, evt); err != nil {
		t.Fatalf("seedCompletedLoop: %v", err)
	}
}

// makeMonitorExecutor builds the executor with fakes, ready for assertions.
func makeMonitorExecutor(kv LoopKVScanner, flows FlowStateReader) *FlowMonitorExecutor {
	return NewFlowMonitorExecutor(kv, flows, slog.Default())
}

// parseMonitorResult decodes the tool result content into the flowMonitorResult shape.
func parseMonitorResult(t *testing.T, content string) flowMonitorResult {
	t.Helper()
	var r flowMonitorResult
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		t.Fatalf("parse monitor result: %v\ncontent: %s", err, content)
	}
	return r
}

func TestFlowMonitorExecutor_ListToolsShape(t *testing.T) {
	t.Parallel()

	e := makeMonitorExecutor(newFakeLoopKV(), newFakeFlowStateReader())
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != FlowMonitorToolName {
		t.Errorf("expected tool name %q, got %q", FlowMonitorToolName, tools[0].Name)
	}
	// flow_id must be required.
	required, _ := tools[0].Parameters["required"].([]string)
	if len(required) == 0 || required[0] != "flow_id" {
		t.Errorf("flow_id should be in required list; got: %v", required)
	}
}

func TestFlowMonitorExecutor_FlowIDRequired(t *testing.T) {
	t.Parallel()

	e := makeMonitorExecutor(newFakeLoopKV(), newFakeFlowStateReader())
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "m1",
		Name: FlowMonitorToolName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected ToolResult.Error when flow_id missing")
	}
	if !strings.Contains(result.Error, "flow_id") {
		t.Errorf("error should mention flow_id, got: %s", result.Error)
	}
}

func TestFlowMonitorExecutor_UnknownToolName(t *testing.T) {
	t.Parallel()

	e := makeMonitorExecutor(newFakeLoopKV(), newFakeFlowStateReader())
	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "m2",
		Name: "wrong_tool",
	})
	if err == nil {
		t.Fatal("expected error for unknown tool name")
	}
}

func TestFlowMonitorExecutor_AggregationAndFiltering(t *testing.T) {
	t.Parallel()

	kv := newFakeLoopKV()
	flows := newFakeFlowStateReader()
	flows.states["my-flow"] = FlowState{DesiredState: "enabled"}

	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	// 3 loops for "my-flow".
	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "loop-1",
		TaskID:       "t1",
		WorkflowSlug: "my-flow",
		Outcome:      agentic.OutcomeSuccess,
		Role:         "researcher",
		Iterations:   3,
		TokensIn:     100,
		TokensOut:    50,
		CompletedAt:  now,
	})
	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "loop-2",
		TaskID:       "t2",
		WorkflowSlug: "my-flow",
		Outcome:      agentic.OutcomeSuccess,
		Role:         "synthesizer",
		Iterations:   2,
		TokensIn:     200,
		TokensOut:    80,
		CompletedAt:  now.Add(-1 * time.Minute),
	})
	// loop-3 is a genuine LoopFailedEvent — uses FailedAt, not CompletedAt.
	if err := kv.putRaw(completeKeyPrefix+"loop-3", agentic.LoopFailedEvent{
		LoopID:       "loop-3",
		TaskID:       "t3",
		WorkflowSlug: "my-flow",
		Outcome:      agentic.OutcomeFailed,
		Role:         "researcher",
		Iterations:   5,
		TokensIn:     300,
		TokensOut:    0,
		FailedAt:     now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("seed loop-3: %v", err)
	}
	// 1 loop for a different flow — must be excluded.
	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "loop-other",
		TaskID:       "tX",
		WorkflowSlug: "other-flow",
		Outcome:      "success",
		Role:         "researcher",
		Iterations:   1,
		TokensIn:     999,
		TokensOut:    999,
		CompletedAt:  now,
	})

	e := makeMonitorExecutor(kv, flows)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "m3",
		Name:      FlowMonitorToolName,
		Arguments: map[string]any{"flow_id": "my-flow"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute tool error: %s", result.Error)
	}

	r := parseMonitorResult(t, result.Content)

	if r.FlowID != "my-flow" {
		t.Errorf("flow_id: want 'my-flow', got %q", r.FlowID)
	}
	if r.DesiredState != "enabled" {
		t.Errorf("desired_state: want 'enabled', got %q", r.DesiredState)
	}
	if r.TotalLoops != 3 {
		t.Errorf("total_loops: want 3, got %d", r.TotalLoops)
	}
	if r.ByOutcome["success"] != 2 {
		t.Errorf("by_outcome.success: want 2, got %d", r.ByOutcome["success"])
	}
	if r.ByOutcome[agentic.OutcomeFailed] != 1 {
		t.Errorf("by_outcome.failed: want 1, got %d", r.ByOutcome[agentic.OutcomeFailed])
	}
	if r.ByRole["researcher"] != 2 {
		t.Errorf("by_role.researcher: want 2, got %d", r.ByRole["researcher"])
	}
	if r.ByRole["synthesizer"] != 1 {
		t.Errorf("by_role.synthesizer: want 1, got %d", r.ByRole["synthesizer"])
	}
	wantTokIn := 100 + 200 + 300
	if r.TotalTokensIn != wantTokIn {
		t.Errorf("total_tokens_in: want %d, got %d", wantTokIn, r.TotalTokensIn)
	}
	wantTokOut := 50 + 80 + 0
	if r.TotalTokensOut != wantTokOut {
		t.Errorf("total_tokens_out: want %d, got %d", wantTokOut, r.TotalTokensOut)
	}

	// recent should be sorted newest-first and include all 3.
	if len(r.Recent) != 3 {
		t.Fatalf("recent: want 3 entries, got %d", len(r.Recent))
	}
	if r.Recent[0].LoopID != "loop-1" {
		t.Errorf("recent[0] should be newest (loop-1), got %q", r.Recent[0].LoopID)
	}
	if r.Recent[2].LoopID != "loop-3" {
		t.Errorf("recent[2] should be oldest (loop-3), got %q", r.Recent[2].LoopID)
	}
}

func TestFlowMonitorExecutor_RecentLimitTrimming(t *testing.T) {
	t.Parallel()

	kv := newFakeLoopKV()
	flows := newFakeFlowStateReader()
	flows.states["trim-flow"] = FlowState{DesiredState: "enabled"}

	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	// Seed 5 loops but request only 2 recent.
	for i := 0; i < 5; i++ {
		seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
			LoopID:       "trim-loop-" + string(rune('A'+i)),
			TaskID:       "t",
			WorkflowSlug: "trim-flow",
			Outcome:      "success",
			Role:         "worker",
			CompletedAt:  base.Add(time.Duration(i) * time.Minute),
		})
	}

	e := makeMonitorExecutor(kv, flows)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "m4",
		Name: FlowMonitorToolName,
		Arguments: map[string]any{
			"flow_id":      "trim-flow",
			"recent_limit": 2,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute tool error: %s", result.Error)
	}

	r := parseMonitorResult(t, result.Content)

	// total_loops should still count all 5.
	if r.TotalLoops != 5 {
		t.Errorf("total_loops: want 5, got %d", r.TotalLoops)
	}
	// recent must be capped at limit=2.
	if len(r.Recent) != 2 {
		t.Errorf("recent: want 2 entries (capped), got %d", len(r.Recent))
	}
	// Most recent first: loop-E (index 4) then loop-D (index 3).
	if r.Recent[0].LoopID != "trim-loop-E" {
		t.Errorf("recent[0] should be most recent (trim-loop-E), got %q", r.Recent[0].LoopID)
	}
}

func TestFlowMonitorExecutor_OtherFlowExcluded(t *testing.T) {
	t.Parallel()

	kv := newFakeLoopKV()
	flows := newFakeFlowStateReader()

	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "unrelated",
		TaskID:       "tx",
		WorkflowSlug: "other-flow",
		Outcome:      "success",
		Role:         "worker",
		CompletedAt:  time.Now(),
	})

	e := makeMonitorExecutor(kv, flows)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "m5",
		Name:      FlowMonitorToolName,
		Arguments: map[string]any{"flow_id": "my-flow"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("tool error: %s", result.Error)
	}

	r := parseMonitorResult(t, result.Content)
	if r.TotalLoops != 0 {
		t.Errorf("total_loops should be 0 when no matching loops, got %d", r.TotalLoops)
	}
	if len(r.Recent) != 0 {
		t.Errorf("recent should be empty, got %d entries", len(r.Recent))
	}
}

func TestFlowMonitorExecutor_KVScanError_ReturnsToolError(t *testing.T) {
	t.Parallel()

	kv := newFakeLoopKV()
	kv.keysErr = errors.New("nats disconnected")

	e := makeMonitorExecutor(kv, newFakeFlowStateReader())
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "m6",
		Name:      FlowMonitorToolName,
		Arguments: map[string]any{"flow_id": "any-flow"},
	})
	// KV scan errors propagate as both ToolResult.Error and a returned error.
	if result.Error == "" {
		t.Fatal("expected ToolResult.Error when KV scan fails")
	}
	if err == nil {
		t.Fatal("expected non-nil error when KV scan fails")
	}
	if result.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind: want %q, got %q", agentic.ToolErrorNetwork, result.ErrorKind)
	}
}

// TestFlowMonitorExecutor_PolymorphicEventShapes verifies that COMPLETE_* keys
// holding LoopCompletedEvent, LoopFailedEvent, and LoopCancelledEvent are all
// decoded correctly. The three shapes share the same bucket but differ in which
// timestamp field is present; the executor must sort by the correct terminal
// timestamp for each shape and must not accumulate an empty by_role key for
// events that carry no role.
func TestFlowMonitorExecutor_PolymorphicEventShapes(t *testing.T) {
	t.Parallel()

	kv := newFakeLoopKV()
	flows := newFakeFlowStateReader()
	flows.states["poly-flow"] = FlowState{DesiredState: "idle"}

	// T1 < T2 < T3 — cancelled is most recent, success is oldest.
	t1 := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(5 * time.Minute)
	t3 := t2.Add(5 * time.Minute)

	// Seed success (oldest).
	if err := kv.putRaw(completeKeyPrefix+"poly-success", agentic.LoopCompletedEvent{
		LoopID:       "poly-success",
		TaskID:       "ts1",
		WorkflowSlug: "poly-flow",
		Outcome:      agentic.OutcomeSuccess,
		Role:         "researcher",
		Iterations:   4,
		TokensIn:     200,
		TokensOut:    80,
		CompletedAt:  t1,
	}); err != nil {
		t.Fatalf("seed success: %v", err)
	}

	// Seed failure (middle).
	if err := kv.putRaw(completeKeyPrefix+"poly-failed", agentic.LoopFailedEvent{
		LoopID:       "poly-failed",
		TaskID:       "ts2",
		WorkflowSlug: "poly-flow",
		Outcome:      agentic.OutcomeFailed,
		Role:         "researcher",
		Reason:       "max_iterations",
		FailedAt:     t2,
	}); err != nil {
		t.Fatalf("seed failure: %v", err)
	}

	// Seed cancellation (most recent, no role).
	if err := kv.putRaw(completeKeyPrefix+"poly-cancelled", agentic.LoopCancelledEvent{
		LoopID:       "poly-cancelled",
		TaskID:       "ts3",
		WorkflowSlug: "poly-flow",
		Outcome:      agentic.OutcomeCancelled,
		CancelledBy:  "user",
		CancelledAt:  t3,
	}); err != nil {
		t.Fatalf("seed cancellation: %v", err)
	}

	e := makeMonitorExecutor(kv, flows)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "poly-call",
		Name: FlowMonitorToolName,
		Arguments: map[string]any{
			"flow_id":      "poly-flow",
			"recent_limit": 3,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute tool error: %s", result.Error)
	}

	r := parseMonitorResult(t, result.Content)

	// Total count.
	if r.TotalLoops != 3 {
		t.Errorf("total_loops: want 3, got %d", r.TotalLoops)
	}

	// by_outcome — one of each.
	if r.ByOutcome[agentic.OutcomeSuccess] != 1 {
		t.Errorf("by_outcome.success: want 1, got %d", r.ByOutcome[agentic.OutcomeSuccess])
	}
	if r.ByOutcome[agentic.OutcomeFailed] != 1 {
		t.Errorf("by_outcome.failed: want 1, got %d", r.ByOutcome[agentic.OutcomeFailed])
	}
	if r.ByOutcome[agentic.OutcomeCancelled] != 1 {
		t.Errorf("by_outcome.cancelled: want 1, got %d", r.ByOutcome[agentic.OutcomeCancelled])
	}

	// by_role — only the success loop carries a role; failed has role field but
	// LoopCancelledEvent has no role. by_role must have exactly 1 key.
	// (LoopFailedEvent does carry Role="researcher" in this seed, so by_role
	//  researcher=2, but the empty-key guard is what matters: no "" key.)
	if _, hasEmptyKey := r.ByRole[""]; hasEmptyKey {
		t.Error("by_role must not contain an empty-string key (failed/cancelled with no role)")
	}

	// Token sums — only from the success entry (failed/cancelled carry zero).
	if r.TotalTokensIn != 200 {
		t.Errorf("total_tokens_in: want 200 (success only), got %d", r.TotalTokensIn)
	}
	if r.TotalTokensOut != 80 {
		t.Errorf("total_tokens_out: want 80 (success only), got %d", r.TotalTokensOut)
	}

	// recent sorted newest-first: cancelled(T3) > failed(T2) > success(T1).
	if len(r.Recent) != 3 {
		t.Fatalf("recent: want 3 entries, got %d", len(r.Recent))
	}
	if r.Recent[0].LoopID != "poly-cancelled" {
		t.Errorf("recent[0]: want poly-cancelled (most recent), got %q", r.Recent[0].LoopID)
	}
	if r.Recent[1].LoopID != "poly-failed" {
		t.Errorf("recent[1]: want poly-failed, got %q", r.Recent[1].LoopID)
	}
	if r.Recent[2].LoopID != "poly-success" {
		t.Errorf("recent[2]: want poly-success (oldest), got %q", r.Recent[2].LoopID)
	}
}
