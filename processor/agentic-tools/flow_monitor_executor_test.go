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
	flows.states["my-flow"] = FlowState{RuntimeState: "running"}

	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	// 3 loops for "my-flow".
	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "loop-1",
		TaskID:       "t1",
		WorkflowSlug: "my-flow",
		Outcome:      "success",
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
		Outcome:      "success",
		Role:         "synthesizer",
		Iterations:   2,
		TokensIn:     200,
		TokensOut:    80,
		CompletedAt:  now.Add(-1 * time.Minute),
	})
	seedCompletedLoop(t, kv, agentic.LoopCompletedEvent{
		LoopID:       "loop-3",
		TaskID:       "t3",
		WorkflowSlug: "my-flow",
		Outcome:      "failure",
		Role:         "researcher",
		Iterations:   5,
		TokensIn:     300,
		TokensOut:    0,
		CompletedAt:  now.Add(-2 * time.Minute),
	})
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
	if r.RuntimeState != "running" {
		t.Errorf("runtime_state: want 'running', got %q", r.RuntimeState)
	}
	if r.TotalLoops != 3 {
		t.Errorf("total_loops: want 3, got %d", r.TotalLoops)
	}
	if r.ByOutcome["success"] != 2 {
		t.Errorf("by_outcome.success: want 2, got %d", r.ByOutcome["success"])
	}
	if r.ByOutcome["failure"] != 1 {
		t.Errorf("by_outcome.failure: want 1, got %d", r.ByOutcome["failure"])
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
	flows.states["trim-flow"] = FlowState{RuntimeState: "running"}

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
	if result.Error == "" && err == nil {
		t.Fatal("expected an error when KV scan fails")
	}
}
