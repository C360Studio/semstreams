package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestDispatchToolCall_StampsLoopID is the regression for the wedge
// semteams reported against beta.37: dispatchToolCall accepted a
// loopID argument but never put it on the outgoing ToolCall, so
// downstream executors (notably bash via sandbox) fell through to
// taskID="default" and every concurrent chain collided on a single
// workspace. After the fix, both tc.LoopID (typed top-level) and
// tc.Metadata["loop_id"] (legacy soft-fallback) are stamped before
// publish.
func TestDispatchToolCall_StampsLoopID(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	good := agentic.ToolCall{
		ID:        "call-stamp",
		Name:      "test_tool",
		Arguments: map[string]any{"q": "hello"},
		// Intentionally NO LoopID, NO Metadata — proves dispatch
		// populates them rather than relying on upstream.
	}

	result := &HandlerResult{}
	dispatched, storeErr := handler.tryDispatchOrSynthesize(result, loopID, good)
	if storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}
	if !dispatched {
		t.Fatal("dispatched = false, want true")
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	if got := payload["loop_id"]; got != loopID {
		t.Errorf("payload.loop_id = %v, want %q", got, loopID)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("payload.metadata not a map: %T", payload["metadata"])
	}
	if got := metadata["loop_id"]; got != loopID {
		t.Errorf("payload.metadata.loop_id = %v, want %q", got, loopID)
	}
}

// TestDispatchToolCall_PreservesExplicitMetadata pins the don't-clobber
// guards: an upstream caller (LLM, governance filter, future
// experimental tool layer) that explicitly populated either field on
// the ToolCall keeps its value through dispatch. Replacing an explicit
// value would silently break any flow that depends on routing a tool
// to a worktree DIFFERENT from its current loop.
func TestDispatchToolCall_PreservesExplicitMetadata(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	const explicitLoopID = "explicit-routing-id"
	good := agentic.ToolCall{
		ID:        "call-preserve",
		Name:      "test_tool",
		Arguments: map[string]any{"q": "hello"},
		LoopID:    explicitLoopID,
		Metadata:  map[string]any{"loop_id": explicitLoopID, "extra": "preserved"},
	}

	result := &HandlerResult{}
	if _, storeErr := handler.tryDispatchOrSynthesize(result, loopID, good); storeErr != nil {
		t.Fatalf("storeErr = %v, want nil", storeErr)
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	if got := payload["loop_id"]; got != explicitLoopID {
		t.Errorf("payload.loop_id = %v, want explicit %q (don't-clobber violated)", got, explicitLoopID)
	}
	metadata, _ := payload["metadata"].(map[string]any)
	if got := metadata["loop_id"]; got != explicitLoopID {
		t.Errorf("payload.metadata.loop_id = %v, want explicit %q", got, explicitLoopID)
	}
	if got := metadata["extra"]; got != "preserved" {
		t.Errorf("payload.metadata.extra = %v, want \"preserved\" (unrelated keys must survive)", got)
	}
}

// TestDispatchToolCall_ApprovalRedispatchStampsLoopID exercises the
// approval re-dispatch path (approval_response_handler.go:dispatchApprovedCall),
// which constructs a fresh ToolCall with NEITHER LoopID NOR Metadata.
// This is the only production caller that bypasses the upstream
// LoopID stamp at handlers.go:875-882 and lands directly on the new
// dispatchToolCall stamp. semteams's headline wedge — every concurrent
// agent chain colliding on sandbox taskID="default" — would re-emerge
// for sandbox-routed bash calls *only on the approval path* if a
// future refactor regressed the dispatchToolCall stamp; this test
// guards that surface.
func TestDispatchToolCall_ApprovalRedispatchStampsLoopID(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-approve", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	pending := agentic.PendingApprovalState{
		CallID:   "call-approved",
		ToolName: "test_tool",
	}
	args := map[string]any{"q": "from-approval"}

	result := &HandlerResult{}
	if err := handler.dispatchApprovedCall(loopID, pending, args, "alice@example.com", result); err != nil {
		t.Fatalf("dispatchApprovedCall: %v", err)
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	if got := payload["loop_id"]; got != loopID {
		t.Errorf("payload.loop_id = %v, want %q", got, loopID)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("payload.metadata not a map: %T", payload["metadata"])
	}
	if got := metadata["loop_id"]; got != loopID {
		t.Errorf("payload.metadata.loop_id = %v, want %q", got, loopID)
	}
	// ApprovedBy must survive the stamping pass — it's the bypass
	// token the agentic-tools approval filter looks for.
	if got := payload["approved_by"]; got != "alice@example.com" {
		t.Errorf("payload.approved_by = %v, lost during dispatch stamp", got)
	}
}

// TestDispatchToolCall_DequeuedCallStampsLoopID exercises the queue
// dequeue path (handlers.go:dispatchedFromQueue). Queued calls came
// in via QueueToolCalls AFTER the upstream LoopID stamp, so they
// already have the typed field set — but Metadata["loop_id"] is NOT
// in that upstream stamp (only the typed field is). Queued calls
// reaching the bash sandbox via dequeue therefore depend on the
// dispatchToolCall metadata stamp to populate the legacy soft-fallback.
// This is most likely the path semteams's >1-concurrent-chain repro
// hit, since concurrent flows queue tail calls.
func TestDispatchToolCall_DequeuedCallStampsLoopID(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-queue", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Simulate the production state: typed LoopID stamped upstream (per
	// handlers.go:879-880) but Metadata is empty. The dequeue path
	// MUST stamp Metadata["loop_id"] for sandbox routing to work.
	queued := []agentic.ToolCall{
		{
			ID:        "call-queued",
			Name:      "test_tool",
			Arguments: map[string]any{"q": "from-queue"},
			LoopID:    loopID,
		},
	}
	handler.loopManager.QueueToolCalls(loopID, queued)

	result := &HandlerResult{}
	dispatched, sErr := handler.dispatchedFromQueue(result, loopID)
	if sErr != nil {
		t.Fatalf("dispatchedFromQueue: %v", sErr)
	}
	if !dispatched {
		t.Fatal("dispatched = false, want true (queue had one call)")
	}

	payload := findPublishedToolCallPayload(t, result, "test_tool")
	if got := payload["loop_id"]; got != loopID {
		t.Errorf("payload.loop_id = %v, want %q (typed field already set upstream)", got, loopID)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("payload.metadata not a map: %T (dispatch must initialise)", payload["metadata"])
	}
	if got := metadata["loop_id"]; got != loopID {
		t.Errorf("payload.metadata.loop_id = %v, want %q (the bash-sandbox routing key)", got, loopID)
	}
}

// findPublishedToolCallPayload locates the tool.execute.<name> message
// in result.PublishedMessages and returns its decoded payload map.
// Fails the test if no matching message is found.
func findPublishedToolCallPayload(t *testing.T, result *HandlerResult, toolName string) map[string]any {
	t.Helper()
	for _, msg := range result.PublishedMessages {
		if msg.Subject != "tool.execute."+toolName {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("envelope.payload not a map: %T", envelope["payload"])
		}
		return payload
	}
	t.Fatalf("no tool.execute.%s message found in %d published messages", toolName, len(result.PublishedMessages))
	return nil
}
