package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// mintedRequestIDs returns the RequestID of every agent.request the handler
// published in one result, in publication order.
func mintedRequestIDs(t *testing.T, result agenticloop.HandlerResult) []string {
	t.Helper()
	var ids []string
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
		ids = append(ids, envelope.Payload.RequestID)
	}
	return ids
}

// oneMintedRequestID asserts the result published exactly one agent.request and
// returns its RequestID.
func oneMintedRequestID(t *testing.T, stage string, result agenticloop.HandlerResult) string {
	t.Helper()
	ids := mintedRequestIDs(t, result)
	if len(ids) != 1 {
		t.Fatalf("%s published %d agent.request messages, want exactly 1 (%v)", stage, len(ids), ids)
	}
	return ids[0]
}

// TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath drives the identity
// through the three sites that actually mint it, asserting on the RequestIDs
// that reach the wire rather than on LoopManager.GenerateRequestID.
//
// The Q4 grammar is <loopID>:req:<iteration>:<retry>, and the manager derives
// both ordinals from state it already holds rather than from the caller's
// locals — because the callers' locals disagree. handleToolsComplete's
// newIteration is 1 for the SECOND request (IncrementIteration runs before the
// mint) while birth already minted :1:0, so passing that local through would
// name two different logical requests identically; agentic-model would then
// answer the second from the first's retained response and the loop would stall
// with no error anywhere. Only an assertion at the mint SITES can see that.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	// Birth: iteration 1, retry 0.
	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-mint-identity",
		Role:   "general",
		Model:  "test-model",
		Prompt: "Mint identity",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	loopID := taskResult.LoopID
	birth := oneMintedRequestID(t, "birth", taskResult)
	if want := loopID + ":req:1:0"; birth != want {
		t.Fatalf("birth RequestID = %q, want %q", birth, want)
	}

	// Continuation: the model calls a tool, the tool answers, the loop sends
	// its next request. That is iteration 2, retry 0 — NOT iteration 1 again.
	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-mint-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call): %v", err)
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
	continuation := oneMintedRequestID(t, "tools complete", toolsComplete)
	if want := loopID + ":req:2:0"; continuation != want {
		t.Fatalf("continuation RequestID = %q, want %q", continuation, want)
	}
	if continuation == birth {
		t.Fatalf("birth and continuation minted the same RequestID %q — two logical requests share one name", birth)
	}

	// Truncation retry: within-iteration recovery. The iteration ordinal is
	// unchanged and the retry ordinal advances, so the retry is a request of
	// its own without claiming to be the next turn.
	fillContextToHighUtilization(t, handler, loopID, 80000)
	retryResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID:    continuation,
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "cut off"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(length_truncated): %v", err)
	}
	if retryResult.State == agentic.LoopStateFailed {
		t.Fatalf("truncation failed the loop instead of retrying; state=%s", retryResult.State)
	}
	retry := oneMintedRequestID(t, "truncation retry", retryResult)
	if want := loopID + ":req:2:1"; retry != want {
		t.Fatalf("truncation retry RequestID = %q, want %q", retry, want)
	}
	if retry == continuation {
		t.Fatalf("the retry reused the truncated request's name %q", continuation)
	}

	// Forward progress after a retry: the NEXT iteration starts at retry
	// ordinal 0 again. Without the reset the mint would read :3:1 — still
	// injective, so no collision and no stall, but it would name a first
	// attempt a retry, and an operator reading request names would count
	// retries that never happened. The manager-level test covers the reset;
	// this is the only assertion that it reaches the wire.
	secondDispatch, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: retry,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-mint-2", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call, after retry): %v", err)
	}
	secondCall := dispatchedToolCallFromResult(t, secondDispatch)

	afterRetry, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      secondCall.ID,
		Name:        secondCall.Name,
		Content:     "tool answered again",
		RequestID:   secondCall.RequestID,
		ExecutionID: secondCall.ExecutionID,
		CallOrdinal: secondCall.CallOrdinal,
	})
	if err != nil {
		t.Fatalf("HandleToolResult(after retry): %v", err)
	}
	nextTurn := oneMintedRequestID(t, "tools complete after retry", afterRetry)
	if want := loopID + ":req:3:0"; nextTurn != want {
		t.Fatalf("post-retry continuation RequestID = %q, want %q", nextTurn, want)
	}
}
