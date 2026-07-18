package agenticloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestSynthesizeToolFailure_StoresResultWithProvidedName verifies the
// helper writes a synthetic ToolResult into the loop's result map with
// the supplied tool name and reason. Mirrors the existing pattern at
// handlers.go:813 (empty-name) and :840 (filter rejection).
func TestSynthesizeToolFailure_StoresResultWithProvidedName(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	if err := handler.synthesizeToolFailure(loopID, "call-1", "delete_rule", "tool dispatch failed: boom"); err != nil {
		t.Fatalf("synthesizeToolFailure: %v", err)
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	got := results[0]
	if got.CallID != "call-1" {
		t.Errorf("CallID = %q, want %q", got.CallID, "call-1")
	}
	if got.Name != "delete_rule" {
		t.Errorf("Name = %q, want %q", got.Name, "delete_rule")
	}
	if !strings.Contains(got.Error, "boom") {
		t.Errorf("Error = %q, want to contain %q", got.Error, "boom")
	}
}

// TestSynthesizeToolFailure_FallsBackToTrackedName confirms the helper
// recovers a tool name from the loopManager's TrackToolName state when
// the caller passes empty name (mode c/d/e drain path doesn't carry
// the name; pulls from the tracker).
func TestSynthesizeToolFailure_FallsBackToTrackedName(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	handler.loopManager.TrackToolName("call-1", "search_index")

	if err := handler.synthesizeToolFailure(loopID, "call-1", "", "loop cancelled by signal"); err != nil {
		t.Fatalf("synthesizeToolFailure: %v", err)
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 || results[0].Name != "search_index" {
		t.Fatalf("expected tracked name; got %+v", results)
	}
}

// TestSynthesizeToolFailure_UnknownToolFallback confirms the sentinel
// when neither caller nor tracker has the name.
func TestSynthesizeToolFailure_UnknownToolFallback(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	if err := handler.synthesizeToolFailure(loopID, "call-orphan", "", "max iterations reached"); err != nil {
		t.Fatalf("synthesizeToolFailure: %v", err)
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 || results[0].Name != "unknown_tool" {
		t.Fatalf("expected unknown_tool fallback; got %+v", results)
	}
}

// TestDrainPendingToolFailures_StoresSynthForEveryPending verifies the
// drain helper synthesizes a result per pending tool and clears the
// pending list. The drain is the chokepoint that protects modes c/d/e
// from leaving orphan tool_calls in KV-persisted context.
func TestDrainPendingToolFailures_StoresSynthForEveryPending(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Set up three pending tools with tracked names.
	for _, tc := range []struct{ id, name string }{
		{"c1", "search_index"},
		{"c2", "read_file"},
		{"c3", "write_kv"},
	} {
		if err := handler.loopManager.AddPendingTool(loopID, tc.id); err != nil {
			t.Fatalf("AddPendingTool %s: %v", tc.id, err)
		}
		handler.loopManager.TrackToolName(tc.id, tc.name)
	}

	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 3 {
		t.Fatalf("setup: pending count = %d, want 3", len(pending))
	}

	handler.drainPendingToolFailures(loopID, "loop cancelled by user-1")

	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Errorf("after drain: pending count = %d, want 0", len(pending))
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 3 {
		t.Fatalf("synth results count = %d, want 3", len(results))
	}

	got := map[string]agentic.ToolResult{}
	for _, r := range results {
		got[r.CallID] = r
	}
	for _, tc := range []struct{ id, name string }{
		{"c1", "search_index"},
		{"c2", "read_file"},
		{"c3", "write_kv"},
	} {
		r, ok := got[tc.id]
		if !ok {
			t.Errorf("missing synth-result for %s", tc.id)
			continue
		}
		if r.Name != tc.name {
			t.Errorf("Name for %s = %q, want %q", tc.id, r.Name, tc.name)
		}
		if !strings.Contains(r.Error, "loop cancelled by user-1") {
			t.Errorf("Error for %s = %q, want to contain %q", tc.id, r.Error, "loop cancelled by user-1")
		}
	}
}

// TestDrainPendingToolFailures_NoOpWhenEmpty confirms the drain path
// short-circuits when no pending tools exist (hot path for completed
// loops calling failLoop).
func TestDrainPendingToolFailures_NoOpWhenEmpty(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	handler.drainPendingToolFailures(loopID, "no-op test")

	if results := handler.loopManager.GetAndClearToolResults(loopID); len(results) != 0 {
		t.Errorf("expected no synth results on empty drain; got %d", len(results))
	}
}

// TestDrainPendingToolFailures_ClearsQueuedCalls confirms the drain
// also discards queued-but-not-yet-dispatched calls so a terminal
// loop doesn't leak queue state into a future restart.
func TestDrainPendingToolFailures_ClearsQueuedCalls(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	handler.loopManager.QueueToolCalls(loopID, []agentic.ToolCall{
		{ID: "q1", Name: "tool_a"},
		{ID: "q2", Name: "tool_b"},
	})
	if !handler.loopManager.HasQueuedTools(loopID) {
		t.Fatal("setup: queued tools should be present")
	}

	handler.drainPendingToolFailures(loopID, "test drain")

	if handler.loopManager.HasQueuedTools(loopID) {
		t.Error("after drain: queued tools should be cleared")
	}
}

// TestTryDispatchOrSynthesize_ForcedMarshalFailure verifies mode (a)
// recovery: when dispatchToolCall fails (here forced via a non-
// marshalable Arguments value), the helper emits a synth-result and
// returns dispatched=false so the caller can try the next queued call
// instead of failing the loop terminal.
func TestTryDispatchOrSynthesize_ForcedMarshalFailure(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// chan values can't be JSON-marshaled — forces dispatchToolCall's
	// json.Marshal to fail, which triggers the synth-result path.
	bad := agentic.ToolCall{
		ID:        "call-bad",
		Name:      "test_tool",
		Arguments: map[string]any{"unmarshalable": make(chan int)},
	}

	result := &HandlerResult{}
	dispatched, storeErr := handler.tryDispatchOrSynthesize(result, loopID, bad)
	if storeErr != nil {
		t.Fatalf("storeErr = %v, want nil (synth-result store should succeed)", storeErr)
	}
	if dispatched {
		t.Error("dispatched = true, want false (marshal should have failed)")
	}

	// Pending should be cleaned up — dispatchToolCall registers pending
	// before marshal, so the synth path must have removed it.
	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Errorf("pending count after failed dispatch = %d, want 0", len(pending))
	}

	// Synth-result should be stored.
	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 {
		t.Fatalf("synth results = %d, want 1", len(results))
	}
	if results[0].CallID != "call-bad" {
		t.Errorf("synth CallID = %q", results[0].CallID)
	}
	if !strings.Contains(results[0].Error, "tool dispatch failed") {
		t.Errorf("synth Error = %q, want to contain 'tool dispatch failed'", results[0].Error)
	}
}

// TestTryDispatchOrSynthesize_HappyPath verifies a successful dispatch
// returns dispatched=true with no synth-result. Sanity-check that the
// synth path doesn't fire on the no-error case.
func TestTryDispatchOrSynthesize_HappyPath(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	good := agentic.ToolCall{
		ID:        "call-ok",
		Name:      "test_tool",
		Arguments: map[string]any{"q": "hello"},
	}

	result := &HandlerResult{}
	dispatched, storeErr := handler.tryDispatchOrSynthesize(result, loopID, good)
	if storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}
	if !dispatched {
		t.Error("dispatched = false, want true (no marshal failure expected)")
	}

	// No synth-result should be stored on the happy path.
	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 0 {
		t.Errorf("synth results on happy path = %d, want 0", len(results))
	}

	// Pending should now contain the dispatched call.
	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 1 {
		t.Errorf("pending count = %d, want 1", len(pending))
	}
}

// TestFailLoop_DrainsPendingTools verifies modes (c) and (d) of orphan
// recovery: when failLoop transitions a loop to terminal, any pending
// tools have synth-results emitted before the transition completes.
// Without this, KV-persisted context for the failed loop would carry
// orphan tool_calls and 400 the model API on any replay.
func TestFailLoop_DrainsPendingTools(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Two pending tools; loop is about to fail.
	if err := handler.loopManager.AddPendingTool(loopID, "c1"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	if err := handler.loopManager.AddPendingTool(loopID, "c2"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	handler.loopManager.TrackToolName("c1", "search_index")
	handler.loopManager.TrackToolName("c2", "read_file")

	result := &HandlerResult{}
	if err := handler.failLoop(result, loopID, agentic.OutcomeFailed, "model_error", "test failure"); err != nil {
		t.Fatalf("failLoop: %v", err)
	}

	if result.State != agentic.LoopStateFailed {
		t.Errorf("State = %s, want failed", result.State)
	}

	// Pending should be drained.
	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Errorf("pending count after failLoop = %d, want 0", len(pending))
	}

	// Synth-results should be present for both pending calls.
	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 2 {
		t.Fatalf("synth results = %d, want 2", len(results))
	}
	for _, r := range results {
		if !strings.Contains(r.Error, "loop failed") {
			t.Errorf("synth-result error %q missing 'loop failed' diagnostic", r.Error)
		}
	}
}

// TestCancelLoop_AfterDrain_PreservesPairs verifies the helper-level
// contract for mode (e): drain followed by CancelLoop leaves clean
// tool-pair state on the loop. The component-level wiring at
// component.go:handleCancelSignal calls drainPendingToolFailures
// immediately before c.handler.CancelLoop with the same shape — this
// test exercises the helper in the same order without spinning a full
// component, so a regression where the drain is omitted from the
// component would not be caught here. Component-level integration
// coverage is left to model_integration_test.go in C4.
func TestCancelLoop_AfterDrain_PreservesPairs(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	if err := handler.loopManager.AddPendingTool(loopID, "c1"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	handler.loopManager.TrackToolName("c1", "search_index")

	// Mirror what component.handleCancelSignal does: drain before cancel.
	handler.drainPendingToolFailures(loopID, "loop cancelled by user-1")

	if _, err := handler.CancelLoop(loopID, "user-1"); err != nil {
		t.Fatalf("CancelLoop: %v", err)
	}

	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Errorf("pending count after cancel drain = %d, want 0", len(pending))
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 {
		t.Fatalf("synth results = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Error, "cancelled") {
		t.Errorf("synth Error = %q, want to contain 'cancelled'", results[0].Error)
	}
}

// TestMaxIterations_DrainsPendingTools confirms mode (d) of orphan
// recovery: when handleToolsComplete hits max iterations, it drains
// pending tools before transitioning to LoopStateFailed.
//
// Drives through HandleModelResponse + HandleToolResult to set up the
// state, then calls IncrementIteration to force the limit on the next
// handleToolsComplete call.
func TestMaxIterations_DrainsPendingTools(t *testing.T) {
	cfg := DefaultConfig()
	handler := NewMessageHandler(cfg)
	ctx := context.Background()

	// Create a loop with maxIterations=1 so we hit the limit fast.
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 1)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Burn the only iteration so the next IncrementIteration in
	// handleToolsComplete returns ErrMaxIterations.
	if err := handler.loopManager.IncrementIteration(loopID); err != nil {
		t.Fatalf("setup IncrementIteration: %v", err)
	}

	// Add a pending tool that simulates an in-flight call when we hit
	// the iteration ceiling.
	if err := handler.loopManager.AddPendingTool(loopID, "c1"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	handler.loopManager.TrackToolName("c1", "search_index")

	// Build a minimal entity for handleToolsComplete (the function reads
	// MaxIterations from it for the diagnostic message).
	entity := agentic.LoopEntity{
		ID:            loopID,
		MaxIterations: 1,
		Role:          "general",
		Model:         "m",
	}
	cm := handler.loopManager.GetContextManager(loopID)
	result := &HandlerResult{}

	_, err = handler.handleToolsComplete(ctx, loopID, entity, cm, result)
	if err != nil {
		t.Fatalf("handleToolsComplete: %v", err)
	}
	if result.State != agentic.LoopStateFailed {
		t.Errorf("State = %s, want failed", result.State)
	}
	if !result.MaxIterationsReached {
		t.Error("MaxIterationsReached should be true")
	}
	// gh#529 / pre-merge review M2: assert the published failure REASON
	// directly, not just the bool. Both exhaustion-detection paths
	// (tool-drain here, and the model-response guard covered elsewhere)
	// must agree on the wire-visible reason string "max_iterations".
	if result.FailureState == nil {
		t.Fatal("FailureState should be populated on the tool-drain max-iterations path")
	}
	if result.FailureState.Reason != "max_iterations" {
		t.Errorf("FailureState.Reason = %q, want %q", result.FailureState.Reason, "max_iterations")
	}

	if pending := handler.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Errorf("pending count after max-iterations = %d, want 0", len(pending))
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 1 {
		t.Fatalf("synth results = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Error, "max iterations") {
		t.Errorf("synth Error = %q, want to contain 'max iterations'", results[0].Error)
	}
}

// TestHandleToolsComplete_NonExhaustionIncrementErrorPropagates is the M1
// pre-merge review regression lock: handleToolsComplete's drain-path
// previously inferred "max iterations reached" from ANY error
// LoopManager.IncrementIteration returned, but that call can also fail
// with an unrelated "loop not found" operational error — which must
// propagate as an ordinary handler error, not be misreported as
// max_iterations. Deletes the loop out from under IncrementIteration
// (entity/cm captured beforehand) to force the non-sentinel branch.
func TestHandleToolsComplete_NonExhaustionIncrementErrorPropagates(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	ctx := context.Background()

	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	entity, err := handler.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	cm := handler.loopManager.GetContextManager(loopID)

	// Remove the loop so the next IncrementIteration call inside
	// handleToolsComplete hits the "loop not found" branch instead of
	// the max-iterations sentinel — entity/cm above were already
	// captured while the loop existed, mirroring a caller racing a
	// concurrent deletion.
	if err := handler.loopManager.DeleteLoop(loopID); err != nil {
		t.Fatalf("DeleteLoop: %v", err)
	}

	result := &HandlerResult{}
	_, err = handler.handleToolsComplete(ctx, loopID, entity, cm, result)
	if err == nil {
		t.Fatal("handleToolsComplete should propagate the non-exhaustion increment error")
	}
	if errors.Is(err, agentic.ErrMaxIterationsReached) {
		t.Errorf("handleToolsComplete misreported a non-exhaustion error as max_iterations: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("handleToolsComplete error = %v, want it to surface the underlying 'loop not found' cause", err)
	}
	if result.MaxIterationsReached {
		t.Error("MaxIterationsReached should remain false when increment failed for a non-exhaustion reason")
	}
}

// TestHandleToolCallResponse_AllDispatchesFail confirms the
// implicit-AllToolsComplete contract: when every approved call's
// dispatch fails (here forced via non-marshalable arguments), every
// call gets a synth-result, the queue stays empty, and
// AllToolsComplete is true so the caller's edge-case branch
// (HandleModelResponse:769) routes through to handleToolsComplete
// with all-synth results in tow rather than the loop dying terminal.
func TestHandleToolCallResponse_AllDispatchesFail(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Three tool calls, all with un-marshalable arguments so every
	// dispatchToolCall fails at json.Marshal.
	bad := []agentic.ToolCall{
		{ID: "c1", Name: "tool_a", Arguments: map[string]any{"x": make(chan int)}},
		{ID: "c2", Name: "tool_b", Arguments: map[string]any{"x": make(chan int)}},
		{ID: "c3", Name: "tool_c", Arguments: map[string]any{"x": make(chan int)}},
	}

	result := &HandlerResult{}
	if err := handler.handleToolCallResponse(result, loopID, bad); err != nil {
		t.Fatalf("handleToolCallResponse: %v", err)
	}

	// All three should have synth-results stored.
	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 3 {
		t.Fatalf("synth results = %d, want 3", len(results))
	}
	// No pending, no queued — caller can fire AllToolsComplete branch.
	if !handler.loopManager.AllToolsComplete(loopID) {
		t.Error("AllToolsComplete should be true after all dispatches synth")
	}
	if handler.loopManager.HasQueuedTools(loopID) {
		t.Error("queue should be empty after all dispatches synth")
	}
}

// TestDispatchedFromQueue_AllFailingDispatchesReturnsFalseNoError
// confirms the dispatchedFromQueue contract: when every queued call's
// dispatch fails, the helper synth-emits each and returns
// (false, nil) so the caller falls through to AllToolsComplete.
func TestDispatchedFromQueue_AllFailingDispatchesReturnsFalseNoError(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	handler.loopManager.QueueToolCalls(loopID, []agentic.ToolCall{
		{ID: "q1", Name: "tool_a", Arguments: map[string]any{"x": make(chan int)}},
		{ID: "q2", Name: "tool_b", Arguments: map[string]any{"x": make(chan int)}},
	})

	result := &HandlerResult{}
	dispatched, storeErr := handler.dispatchedFromQueue(result, loopID)
	if storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}
	if dispatched {
		t.Error("dispatched = true, want false (all queued calls should have synth-failed)")
	}

	results := handler.loopManager.GetAndClearToolResults(loopID)
	if len(results) != 2 {
		t.Fatalf("synth results = %d, want 2", len(results))
	}
	if handler.loopManager.HasQueuedTools(loopID) {
		t.Error("queue should be drained")
	}
}

// TestSynthesizeToolFailure_LoopNotFoundReturnsError confirms the
// failure mode where the loop doesn't exist in the manager (e.g.,
// a race against cancel-and-cleanup). StoreToolResult bubbles the
// loop-not-found error up; callers should treat this as fatal.
func TestSynthesizeToolFailure_LoopNotFoundReturnsError(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())

	err := handler.synthesizeToolFailure("ghost-loop", "call-1", "tool_x", "test reason")
	if err == nil {
		t.Fatal("expected error for non-existent loop, got nil")
	}
}

// TestRepairToolPairs_StripsOrphanFromInjectedContext is the C3
// safety-net test: simulate a KV-restored loop with corrupt context
// (assistant message carrying tool_calls but no matching tool
// results) by injecting messages directly into the ContextManager,
// then call the public RepairToolPairs to assert the orphan group is
// removed. This is what defends against any future failure path that
// adds an assistant tool_call to context without ensuring matched
// results — even if every C1-wired terminal-transition path is
// correct, this audit is the belt-and-suspenders.
func TestRepairToolPairs_StripsOrphanFromInjectedContext(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	cm := handler.loopManager.GetContextManager(loopID)

	// Inject an assistant message with two tool_calls, but only one
	// matching tool_result. The pair (call-2) is orphaned; the
	// repair must drop the entire group (assistant + the call-1
	// result) because partial pairs cause provider 400s.
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:    "user",
		Content: "do something",
	})
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "assistant",
		ToolCalls: []agentic.ToolCall{
			{ID: "call-1", Name: "search"},
			{ID: "call-2", Name: "read_file"},
		},
	})
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:       "tool",
		ToolCallID: "call-1",
		Content:    "search result",
	})
	// call-2 has no matching tool result — the orphan.

	// Pre-audit: 3 messages (user + assistant-with-tool_calls +
	// only-one-tool-result; the second tool_call's result is
	// deliberately missing).
	if got := cm.GetContext(); len(got) != 3 {
		t.Fatalf("setup: GetContext returned %d messages, want 3", len(got))
	}

	removed := cm.RepairToolPairs()
	if removed == 0 {
		t.Error("expected RepairToolPairs to remove orphan group; got 0")
	}

	// Post-audit: only the user message survives. The orphan group
	// (assistant + call-1 result) is dropped together because partial
	// pairs are invalid for the next agent.request.
	post := cm.GetContext()
	for _, msg := range post {
		if len(msg.ToolCalls) > 0 {
			t.Errorf("post-audit context still has assistant message with tool_calls: %+v", msg)
		}
		if msg.ToolCallID != "" {
			t.Errorf("post-audit context still has tool result message: %+v", msg)
		}
	}
}

// TestRepairToolPairs_NoOpOnWellFormedContext confirms the hot path
// is a no-op when every tool_call has a matching result. Linear scan,
// no allocations, no warning logged.
func TestRepairToolPairs_NoOpOnWellFormedContext(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	cm := handler.loopManager.GetContextManager(loopID)

	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:    "user",
		Content: "do something",
	})
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "assistant",
		ToolCalls: []agentic.ToolCall{
			{ID: "call-1", Name: "search"},
		},
	})
	_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:       "tool",
		ToolCallID: "call-1",
		Content:    "search result",
	})

	pre := len(cm.GetContext())
	removed := cm.RepairToolPairs()
	post := len(cm.GetContext())

	if removed != 0 {
		t.Errorf("expected zero removed on well-formed context; got %d", removed)
	}
	if pre != post {
		t.Errorf("context length changed: %d → %d (well-formed should be unchanged)", pre, post)
	}
}
