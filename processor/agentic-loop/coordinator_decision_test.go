package agenticloop_test

// gh#1094 / ADR-101 — agentic-loop observes that its terminal StopLoop tool
// was `decide` and carries the typed decision on the completion event.
//
// The tool is identified through the loop's EXISTING name-fallback chain:
// the tracked name for the call ID first, then the tool result's own Name
// (C3) — a process restart or LoopManager cache loss must not demote a
// decide terminal to a no-decision terminal. Every assertion decodes the
// published completion envelope through the PRODUCTION decoder into a fresh
// value.

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/stretchr/testify/require"
)

// decodeCompletionEvent finds the agent.complete message among the handler's
// published messages and decodes it through the production registry decoder
// into a fresh LoopCompletedEvent.
func decodeCompletionEvent(t *testing.T, msgs []agenticloop.PublishedMessage) *agentic.LoopCompletedEvent {
	t.Helper()
	decoder := payloadbuiltins.NewTestDecoder(t)
	for _, msg := range msgs {
		if !strings.Contains(strings.ToLower(msg.Subject), "agent.complete") {
			continue
		}
		var decoded message.Message
		decoded, err := decoder.Decode(msg.Data)
		require.NoError(t, err, "decode completion envelope")
		completion, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
		require.True(t, ok, "expected *agentic.LoopCompletedEvent, got %T", decoded.Payload())
		return completion
	}
	t.Fatalf("no agent.complete message published; got %d messages", len(msgs))
	return nil
}

func startDecisionTestLoop(t *testing.T, handler *agenticloop.MessageHandler) string {
	t.Helper()
	taskResult, err := handler.HandleTask(context.Background(), agenticloop.TaskMessage{
		TaskID: "task-decision",
		Role:   "coordinator",
		Model:  "qwen-32b",
		Prompt: "Coordinate",
	})
	require.NoError(t, err)
	return taskResult.LoopID
}

func TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()
	loopID := startDecisionTestLoop(t, handler)

	// Model calls decide; the loop tracks the call ID under the tool name.
	_, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-decide",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-decide", Name: agentic.DecideToolName}},
		},
	})
	require.NoError(t, err)

	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:   "call-decide",
		Name:     agentic.DecideToolName,
		Content:  `{"action":"respond_direct","reason":"Optimized the flight plan."}`,
		StopLoop: true,
		Metadata: map[string]any{
			agentic.MetadataKeyDecideAction: agentic.DecideActionRespondDirect,
			agentic.MetadataKeyDecideReason: "Optimized the flight plan.",
			"loop_entity_id":                "acme.ops.agent.loop.execution." + loopID,
		},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, result.State)

	completion := decodeCompletionEvent(t, result.PublishedMessages)
	require.NotNil(t, completion.Decision, "decide terminal must carry a typed decision")
	require.Equal(t, agentic.DecideActionRespondDirect, completion.Decision.Action)
	require.Equal(t, "Optimized the flight plan.", completion.Decision.Reason)
	require.Equal(t, `{"action":"respond_direct","reason":"Optimized the flight plan."}`, completion.Result,
		"Result stays the tool result content")
}

func TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()
	loopID := startDecisionTestLoop(t, handler)

	// No HandleModelResponse: nothing ever tracked a name for this call ID,
	// exactly as after a process restart. The result envelope's own Name is
	// the only surviving evidence that the terminal tool was decide.
	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:   "call-after-restart",
		Name:     agentic.DecideToolName,
		Content:  `{"action":"ask_user","reason":"Which airframe?"}`,
		StopLoop: true,
		Metadata: map[string]any{
			agentic.MetadataKeyDecideAction: agentic.DecideActionAskUser,
			agentic.MetadataKeyDecideReason: "Which airframe?",
		},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, result.State)

	completion := decodeCompletionEvent(t, result.PublishedMessages)
	require.NotNil(t, completion.Decision, "tracked-name loss must not demote a decide terminal")
	require.Equal(t, agentic.DecideActionAskUser, completion.Decision.Action)
	require.Equal(t, "Which airframe?", completion.Decision.Reason)
}

func TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	ctx := context.Background()
	loopID := startDecisionTestLoop(t, handler)

	_, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-submit",
		Status:    "tool_call",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-submit", Name: "submit_work"}},
		},
	})
	require.NoError(t, err)

	// Metadata carrying action/reason is present: the guard is the TOOL
	// NAME, never the shape of the metadata or of Result.
	result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:   "call-submit",
		Name:     "submit_work",
		Content:  `{"action":"respond_direct","reason":"not a decision"}`,
		StopLoop: true,
		Metadata: map[string]any{
			agentic.MetadataKeyDecideAction: agentic.DecideActionRespondDirect,
			agentic.MetadataKeyDecideReason: "not a decision",
		},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, result.State)

	completion := decodeCompletionEvent(t, result.PublishedMessages)
	require.Nil(t, completion.Decision, "only a decide terminal carries a typed decision")
	require.Equal(t, `{"action":"respond_direct","reason":"not a decision"}`, completion.Result)
}

// TestHandleCompleteResponseLeavesDecisionNilWhenDecideMetadataIsUnusable pins
// the fail-safe branch of the stamping guard: a decide terminal whose typed
// metadata is missing or empty leaves Decision nil rather than stamping a
// half-decision. A present Decision with an empty field fails
// LoopCompletedEvent.Validate (C4) and would be Termed by the terminal
// normalizer — losing the terminal entirely — so the loop declines to stamp
// and the terminal keeps the pre-gh#1094 route-ownership behaviour.
func TestHandleCompleteResponseLeavesDecisionNilWhenDecideMetadataIsUnusable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata map[string]any
	}{
		{name: "no metadata", metadata: nil},
		{name: "empty action", metadata: map[string]any{
			agentic.MetadataKeyDecideAction: "",
			agentic.MetadataKeyDecideReason: "a reason",
		}},
		{name: "empty reason", metadata: map[string]any{
			agentic.MetadataKeyDecideAction: agentic.DecideActionRespondDirect,
			agentic.MetadataKeyDecideReason: "",
		}},
		{name: "non-string action", metadata: map[string]any{
			agentic.MetadataKeyDecideAction: 7,
			agentic.MetadataKeyDecideReason: "a reason",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := agenticloop.NewMessageHandler(createTestConfig())
			ctx := context.Background()
			loopID := startDecisionTestLoop(t, handler)

			result, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
				CallID:   "call-decide-unusable",
				Name:     agentic.DecideToolName,
				Content:  `{"action":"respond_direct"}`,
				StopLoop: true,
				Metadata: tc.metadata,
			})
			require.NoError(t, err)

			completion := decodeCompletionEvent(t, result.PublishedMessages)
			require.Nil(t, completion.Decision, "an unusable decide payload must not stamp a half-decision")
			require.NoError(t, completion.Validate(), "the completion must stay deliverable")
		})
	}
}

// TestHandleCompleteResponseLeavesDecisionNilForSynthesizedDecide pins the
// delta scenario "synthesized decision does not populate the field": the
// framework's needs_clarification recovery (#133/gh#158) is a GRAPH TRIPLE
// written after completion, never a tool result, so a text-only coordinator
// completion still decodes with a nil Decision and keeps today's
// route-ownership behaviour. Making it user-facing would publish a prompt for
// every text-only coordinator completion.
func TestHandleCompleteResponseLeavesDecisionNilForSynthesizedDecide(t *testing.T) {
	config := createTestConfig()
	config.SynthesizeTerminalOnCompletion = true
	handler := agenticloop.NewMessageHandler(config)
	ctx := context.Background()
	loopID := startDecisionTestLoop(t, handler)

	// Text-only completion: no terminal tool call at all.
	result, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "req-text",
		Status:    "complete",
		Message: agentic.ChatMessage{
			Role:    "assistant",
			Content: "I need more information about the airframe.",
		},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, result.State)
	require.NotNil(t, result.SyntheticDecide,
		"the synthesis path must actually fire, or this test proves nothing")
	require.Equal(t, loopID, result.SyntheticDecide.LoopID)

	completion := decodeCompletionEvent(t, result.PublishedMessages)
	require.Nil(t, completion.Decision, "a synthesized decision never rides the completion event")
	require.Equal(t, "I need more information about the airframe.", completion.Result)
}
