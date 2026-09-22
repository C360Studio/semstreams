package agenticloop_test

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/stretchr/testify/require"
)

// TestEveryMintedRequestIsNamedOnTheLoopRecord walks the three sites that mint
// an agent.request — birth, the compaction retry, and the advance after a tool
// batch — and asserts each one leaves its RequestID on the loop entity the
// carrier is about to write.
//
// This is invariant I1's producer half (#1330). A mint that published a
// request the record did not name would leave a replacement process unable to
// tell the response for that request from a response for a superseded one, and
// the failure is silent: the record still validates, the loop still runs, and
// the hole only opens when a process is replaced.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestEveryMintedRequestIsNamedOnTheLoopRecord(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	taskResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-published-identity",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "Test",
	})
	require.NoError(t, err)
	loopID := taskResult.LoopID

	birth := mintedRequestIDs(t, taskResult)
	require.Len(t, birth, 1, "birth mints exactly one request")
	require.Equal(t, loopID+":req:1:0", birth[0])
	requirePublishedRequest(t, handler, loopID, birth[0])

	// The compaction retry: a second request for the SAME iteration.
	fillContextToHighUtilization(t, handler, loopID, 80000)
	retryResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID:    handler.OutstandingRequestForTest(loopID),
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "truncated"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	})
	require.NoError(t, err)
	retry := mintedRequestIDs(t, retryResult)
	require.Len(t, retry, 1, "the self-heal mints exactly one request")
	require.Equal(t, loopID+":req:1:1", retry[0])
	requirePublishedRequest(t, handler, loopID, retry[0])

	// The advance: a tool batch completes and the loop moves to iteration 2.
	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID:    handler.OutstandingRequestForTest(loopID),
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-1", Name: "test_tool"}},
		},
	})
	require.NoError(t, err)
	require.Empty(t, mintedRequestIDs(t, dispatchResult), "a tool-call response mints nothing yet")
	requirePublishedRequest(t, handler, loopID, retry[0],
		"a dispatch publishes no request, so the record still names the last one")

	dispatched := dispatchedToolCallFromResult(t, dispatchResult)
	advanced, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     "tool answered",
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
	})
	require.NoError(t, err)
	advance := mintedRequestIDs(t, advanced)
	require.Len(t, advance, 1, "the completed batch mints exactly one request")
	require.Equal(t, loopID+":req:2:0", advance[0])
	requirePublishedRequest(t, handler, loopID, advance[0])
}

func requirePublishedRequest(t *testing.T, handler *agenticloop.MessageHandler, loopID, want string, msgAndArgs ...any) {
	t.Helper()
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	require.Equal(t, want, entity.PublishedRequestID, msgAndArgs...)
}
