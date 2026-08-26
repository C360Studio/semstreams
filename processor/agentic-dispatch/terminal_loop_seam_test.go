package agenticdispatch

// gh#1094 — the one seam no other in-repo test crosses: a REAL agentic-loop
// completion envelope, produced by the loop's own handler, settled by the real
// dispatch settlement path.
//
// Forced omission A (deleting the loop's `completion.Decision` assignment) was
// caught only by the agentic-loop tests, because every dispatch unit test
// builds the payload itself. This test closes that hole in process: the bytes
// under assertion are the ones agentic-loop publishes, so a carrier defect on
// either side of the seam fails here. It is NOT a substitute for the e2e gap
// (#1105), which additionally covers ingest, rules, and the NATS wire.
//
// No import cycle: agentic-loop does not import agentic-dispatch.

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/stretchr/testify/require"
)

// loopCompletionEnvelope drives a real loop to a decide terminal and returns
// the agent.complete envelope it published.
func loopCompletionEnvelope(t *testing.T, loopAction, loopReason string) []byte {
	t.Helper()
	handler := agenticloop.NewMessageHandler(agenticloop.DefaultConfig())
	ctx := context.Background()

	task, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "seam-task", Role: "coordinator", Model: "qwen-32b", Prompt: "coordinate",
	})
	require.NoError(t, err)

	_, err = handler.HandleModelResponse(ctx, task.LoopID, agentic.AgentResponse{
		RequestID: "seam-req", Status: "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "seam-call", Name: agentic.DecideToolName}},
		},
	})
	require.NoError(t, err)

	result, err := handler.HandleToolResult(ctx, task.LoopID, agentic.ToolResult{
		CallID: "seam-call", Name: agentic.DecideToolName, StopLoop: true,
		Content: `{"action":"` + loopAction + `","reason":"` + loopReason + `"}`,
		Metadata: map[string]any{
			agentic.MetadataKeyDecideAction: loopAction,
			agentic.MetadataKeyDecideReason: loopReason,
		},
	})
	require.NoError(t, err)

	for _, msg := range result.PublishedMessages {
		if strings.Contains(msg.Subject, "agent.complete") {
			return msg.Data
		}
	}
	t.Fatalf("loop published no agent.complete message (%d messages)", len(result.PublishedMessages))
	return nil
}

func TestSettleAgentTerminalConsumesARealLoopDecideCompletion(t *testing.T) {
	t.Run("reply decision is delivered to the resolved origin", func(t *testing.T) {
		c := terminalTestComponent(t)
		var terminalLoopID string
		data := loopCompletionEnvelope(t, agentic.DecideActionRespondDirect, "Optimized the flight plan.")
		terminalLoopID = loopIDFromEnvelope(t, c, data)

		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: terminalLoopID, TaskID: "seam-task", State: agentic.LoopStateComplete,
				ParentLoopID: "seam-root",
			},
			agentic.LoopEntity{
				ID: "seam-root", State: agentic.LoopStateComplete,
				ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
			},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(), data))

		response, _, count := get()
		require.Equal(t, 1, count, "the loop's own envelope must carry the decision across the seam")
		require.Equal(t, agentic.ResponseTypeResult, response.Type)
		require.Equal(t, "Optimized the flight plan.", response.Content)
		require.Equal(t, "origin-1", response.ChannelID)
		require.Equal(t, terminalLoopID, response.InReplyTo)
		requireOneTerminalReason(t, c, "response_settled", before)
	})

	t.Run("handoff decision publishes nothing", func(t *testing.T) {
		c := terminalTestComponent(t)
		data := loopCompletionEnvelope(t, "autoresearch", "hand off to the chain")
		terminalLoopID := loopIDFromEnvelope(t, c, data)

		loader := newAncestryLoader(agentic.LoopEntity{
			ID: terminalLoopID, TaskID: "seam-task", State: agentic.LoopStateComplete,
			ChannelType: "http", ChannelID: "origin-1",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(), data))

		_, _, count := get()
		require.Zero(t, count)
		requireOneTerminalReason(t, c, "handoff_settled", before)
	})
}

// loopIDFromEnvelope decodes the loop id through the production decoder, so the
// fixture never guesses the identity the loop minted.
func loopIDFromEnvelope(t *testing.T, c *Component, data []byte) string {
	t.Helper()
	decoded, err := c.decoder.Decode(data)
	require.NoError(t, err)
	completion, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
	require.True(t, ok, "expected *agentic.LoopCompletedEvent, got %T", decoded.Payload())
	require.NotNil(t, completion.Decision, "the loop must stamp the decision at its own end of the seam")
	return completion.LoopID
}
