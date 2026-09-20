package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// continuationPrompt is long enough to be unmistakable inside a request body.
const continuationPrompt = "and also summarise the second thing"

// requestBodyContains reports whether any agent.request in the result carries a
// message with the given content.
func requestBodyContains(t *testing.T, result agenticloop.HandlerResult, content string) bool {
	t.Helper()
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(strings.ToLower(msg.Subject), "agent.request") {
			continue
		}
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode agent.request on %s: %v", msg.Subject, err)
		}
		for _, m := range envelope.Payload.Messages {
			if strings.Contains(m.Content, content) {
				return true
			}
		}
	}
	return false
}

// startLoopAndAdmitContinuation births a loop, leaves its first request
// outstanding, and admits a second task naming the same loop. Returns the loop
// ID, the birth RequestID, and the continuation's handler result.
func startLoopAndAdmitContinuation(t *testing.T, handler *agenticloop.MessageHandler) (string, string, agenticloop.HandlerResult) {
	t.Helper()
	ctx := context.Background()

	birthResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-defer-1",
		Role:   "general",
		Model:  "test-model",
		Prompt: "summarise the first thing",
	})
	if err != nil {
		t.Fatalf("HandleTask (birth): %v", err)
	}
	loopID := birthResult.LoopID
	birth := oneMintedRequestID(t, "birth", birthResult)

	// No model response has arrived, so the loop is waiting on `birth`.
	continuation, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-defer-2",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: continuationPrompt,
	})
	if err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}
	return loopID, birth, continuation
}

// A continuation admitted while the loop's model request is still outstanding
// must publish NOTHING. The request it would mint carries this iteration's
// name — `<loopID>:req:<iteration>:<retry>` does not move until the loop
// advances — so it would go out with the outstanding request's RequestID and
// Nats-Msg-Id but different bytes, and the stream's duplicate window would drop
// it. The turn is not refused either: refusing throws away a user's message for
// typing while the agent was thinking.
//
// Asserted on the published messages rather than on the marker, because the
// marker is the mechanism and "no second request goes out" is the obligation.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestContinuationBehindAnOutstandingRequestPublishesNothing(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))

	_, birth, continuation := startLoopAndAdmitContinuation(t, handler)

	if ids := mintedRequestIDs(t, continuation); len(ids) != 0 {
		t.Fatalf("the deferred continuation published %d agent.request messages (%v); the first would reuse %q",
			len(ids), ids, birth)
	}
	if len(continuation.PublishedMessages) != 0 {
		t.Fatalf("the deferred continuation published %d messages, want none", len(continuation.PublishedMessages))
	}
	if !continuation.Deferred {
		t.Fatal("the continuation result is not marked Deferred; the delivery cannot tell it apart from a dedup")
	}
}

// The half the previous behaviour got wrong. `design.md` claimed a deferred
// turn "rides the next iteration's request", which is true on a tool-call
// response and FALSE on a completion: the loop completes and there is no next
// request. The completion response must carry the turn instead of settling.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	completion, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the first thing is done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete): %v", err)
	}

	if completion.State.IsTerminal() {
		t.Fatalf("the loop completed with a turn admitted and never sent; state=%s", completion.State)
	}
	if completion.CompletionState != nil {
		t.Fatal("a completion record was built for a loop that has an unanswered turn")
	}

	next := oneMintedRequestID(t, "deferred carry", completion)
	if want := loopID + ":req:2:0"; next != want {
		t.Fatalf("carried RequestID = %q, want %q", next, want)
	}
	if next == birth {
		t.Fatalf("the carried request reused the outstanding request's name %q", birth)
	}
	if !requestBodyContains(t, completion, continuationPrompt) {
		t.Fatalf("the carried request does not contain the continuation's turn %q", continuationPrompt)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.PendingContinuation {
		t.Fatal("the pending-continuation marker survived the request that carries the turn")
	}
}

// The tool-call path was already sound — the turn rides iteration N+1 — but
// nothing pinned that it clears the marker. Without the clear, the NEXT
// completion on this loop would defer again forever against a turn that was
// already sent.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationRidesTheToolCallPath(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-defer-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call): %v", err)
	}
	if ids := mintedRequestIDs(t, dispatchResult); len(ids) != 0 {
		t.Fatalf("a tool-call response minted %d requests (%v); the tools have not answered yet", len(ids), ids)
	}

	dispatched := dispatchedToolCallFromResult(t, dispatchResult)
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
	if want := loopID + ":req:2:0"; next != want {
		t.Fatalf("tools-complete RequestID = %q, want %q", next, want)
	}
	if !requestBodyContains(t, toolsComplete, continuationPrompt) {
		t.Fatalf("the tools-complete request does not contain the continuation's turn %q", continuationPrompt)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.PendingContinuation {
		t.Fatal("the pending-continuation marker survived the request that carries the turn")
	}
}
