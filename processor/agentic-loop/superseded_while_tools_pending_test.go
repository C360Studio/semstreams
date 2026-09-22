package agenticloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// The window the outstanding mark cannot see. A tool-call response SETTLES its
// request — the loop is owed no model answer while its executors run — so for
// the whole tool phase "the loop is waiting on nothing" is the ordinary state
// rather than evidence that a response belongs here. An identity guard keyed on
// that emptiness therefore reopens the bug it was written for one step later:
// the carrying request answers with a tool call, and the EARLIER request's
// redelivered completion then walks straight in and settles the loop.
//
// What it settles is the wrong task. The carried request is the one asking the
// user's second turn, so the loop completes task two with task one's answer,
// the tool still running underneath it, and the user's turn is lost with a
// plausible-looking result on top of it. Reproduced sequentially by the owner's
// round-2 review — no concurrency, no restart, no redelivery of anything but
// the ordinary at-least-once kind.
//
// The fix is to compare against the loop's CURRENT request — the newest one
// minted — which stays true across the tool phase. Asserted on the durable
// record and on the tool result still landing, because "the redelivery changed
// nothing AND the live iteration still finishes" is the obligation.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestRedeliveredCompletionIsRefusedWhileTheCarrierWaitsOnTools(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	handler.EnableDropCountingForTest()
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	// Request one completes and carries the deferred turn into request two.
	firstCompletion := agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the first thing is done"},
	}
	carry, err := handler.HandleModelResponse(ctx, loopID, firstCompletion)
	if err != nil {
		t.Fatalf("HandleModelResponse(complete birth): %v", err)
	}
	carrier := oneMintedRequestID(t, "carry", carry)
	if !requestBodyContains(t, carry, continuationPrompt) {
		t.Fatalf("the carrying request does not contain the deferred turn %q", continuationPrompt)
	}

	// Request two answers with a tool call: its request is settled, the tool is
	// dispatched, and the loop now waits on an executor rather than a model.
	dispatch, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: carrier,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-superseded-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call carrier): %v", err)
	}
	dispatched := dispatchedToolCallFromResult(t, dispatch)
	if outstanding := handler.OutstandingRequestForTest(loopID); outstanding != "" {
		t.Fatalf("fixture: the loop must be waiting on no model request here, got %q", outstanding)
	}

	before, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (before redelivery): %v", err)
	}
	drops := handler.ModelResponseDropsForTest("superseded_request")

	// The exact bytes of request one's completion, redelivered by the lane.
	replay, err := handler.HandleModelResponse(ctx, loopID, firstCompletion)
	if !errors.Is(err, agenticloop.ErrResponseSupersededForTest) {
		t.Fatalf("HandleModelResponse(redelivered birth completion) = %v, want a superseded refusal", err)
	}
	// The refusal hands the carrier NOTHING. An empty result is still a result:
	// it flows on to persistLoopState, whose compare-and-swap moves the loop
	// record's revision for a delivery that changed nothing (#1330).
	if replay.LoopID != "" || replay.State != "" || replay.CompletionState != nil || len(replay.PublishedMessages) != 0 {
		t.Fatalf("the refusal handed the carrier a result to persist: %+v", replay)
	}
	if got := handler.ModelResponseDropsForTest("superseded_request"); got != drops+1 {
		t.Fatalf("superseded_request drops = %v, want %v: the refusal must be counted under the reason it happened",
			got, drops+1)
	}

	after, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (after redelivery): %v", err)
	}
	if after.State != before.State {
		t.Fatalf("state moved on a redelivery: %s -> %s", before.State, after.State)
	}
	if after.Iterations != before.Iterations {
		t.Fatalf("iterations moved on a redelivery: %d -> %d", before.Iterations, after.Iterations)
	}
	if after.TaskID != before.TaskID {
		t.Fatalf("task identity moved on a redelivery: %q -> %q", before.TaskID, after.TaskID)
	}
	if after.Outcome != "" || !after.CompletedAt.IsZero() {
		t.Fatalf("the redelivery settled the loop: outcome=%q completed_at=%v", after.Outcome, after.CompletedAt)
	}

	// The live iteration is untouched: its tool answers and the loop advances.
	toolsComplete, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     "tool answered",
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
	})
	if err != nil {
		t.Fatalf("HandleToolResult: %v", err)
	}
	next := oneMintedRequestID(t, "tools complete", toolsComplete)
	if want := loopID + ":req:3:0"; next != want {
		t.Fatalf("tools-complete RequestID = %q, want %q", next, want)
	}
}
