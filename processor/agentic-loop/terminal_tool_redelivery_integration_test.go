//go:build integration

package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// Task 4.1 of #1362, over a real broker: a terminal tool result redelivered
// to a replacement process after its predecessor died inside the terminal
// owner.
//
// Case (i) — the approval lane's reject-minted W4 — is checkpoint 2's
// (tasks 1.1–1.6 build the cold branch it exercises).
// TODO(#1362, checkpoint 2): case (i).

// TestATerminalRedeliveredAfterItsPublicationAdoptsTheDurableTerminal is case
// (ii): the terminal lane crashes after its publication and before its record
// update, and the redelivery is settled through arm (b) — the replacement's
// marker Create is refused, the saved terminal is read back and adopted by loop
// ID and terminal kind, republished, and the record written to match.
//
// The replacement's own candidate differs in content from the saved terminal,
// by construction rather than by fixture: a loop rebuilt from its record and
// retained request publishes its terminal with an empty prompt (the record
// carries no prompt), and the predecessor's carried the task's. Whether the
// saved terminal or the candidate went out is therefore observable on the
// stream.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestATerminalRedeliveredAfterItsPublicationAdoptsTheDurableTerminal(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID, firstRequest := bornLoop(t, predecessor, handler, "task-terminal-adopt")
	completeSubject := "agent.complete." + loopID
	markerKey := "COMPLETE_" + loopID

	// One terminal tool call: its result ends the loop.
	batch := agentic.AgentResponse{
		RequestID:    firstRequest,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-terminal", Name: "submit_work"}},
		},
	}
	retainModelResponse(t, client, batch)
	dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))
	call, _ := dispatchedToolCall(t, dispatch)
	result := agentic.ToolResult{
		CallID: call.ID, Name: call.Name, Content: "the work, submitted", LoopID: loopID,
		RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		StopLoop: true,
	}

	// The predecessor dies between the terminal's publication and its record.
	predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
	_, died := deliverToolResult(t, predecessor, result)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision(),
		"a terminal record write of unknown durability is not an acknowledgement")

	// The residue, read off the server: the marker exists, the event is
	// published, and the record is not terminal. That is the window arm (b)
	// exists for.
	marker, err := predecessor.loopsBucket.Get(t.Context(), markerKey)
	require.NoError(t, err, "the marker is the terminal owner's FIRST step")
	savedMarker := append([]byte(nil), marker.Value()...)
	markerRevision := marker.Revision()
	require.Equal(t, uint64(1), messagesOn(t, client, completeSubject),
		"the event is published before the record is written")
	require.False(t, loopRecordOf(t, predecessor, loopID).entity.State.IsTerminal(),
		"the record is the terminal owner's LAST step")
	var saved agentic.LoopCompletedEvent
	require.NoError(t, json.Unmarshal(savedMarker, &saved))
	require.Equal(t, bornLoopPrompt, saved.Prompt, "fixture check: the predecessor's terminal carries the task prompt")

	// (b): the replacement rebuilds the loop cold, re-derives the terminal,
	// and adopts the one already durable.
	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	msg, delivered := deliverToolResult(t, replacement, result)

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
		"a content difference from the durable terminal is never a disposition")
	require.Equal(t, int32(1), msg.acks.Load())

	adoptedMarker, err := replacement.loopsBucket.Get(t.Context(), markerKey)
	require.NoError(t, err)
	require.Equal(t, markerRevision, adoptedMarker.Revision(),
		"the loop's durable terminal was overwritten instead of adopted")
	require.JSONEq(t, string(savedMarker), string(adoptedMarker.Value()))

	require.Equal(t, uint64(2), messagesOn(t, client, completeSubject),
		"the adopted terminal is republished — an accepted duplicate")
	republished := lastCompletionOn(t, client, completeSubject)
	require.Equal(t, saved.Prompt, republished.Prompt,
		"the replacement published its own candidate instead of the durable terminal")
	require.True(t, saved.CompletedAt.Equal(republished.CompletedAt),
		"the replacement published its own candidate instead of the durable terminal")

	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, agentic.LoopStateComplete, record.entity.State,
		"the record is written terminal to match the adopted terminal")
	require.Equal(t, saved.Result, record.entity.Result)

	// (a): once the record is terminal, a further redelivery is settled
	// without effect — by the lane's own classification, outside the owner.
	third, _ := startLoopProcess(t, client, DefaultConfig())
	recordRevision := record.revision
	msg, delivered = deliverToolResult(t, third, result)
	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, recordRevision, loopRecordOf(t, third, loopID).revision,
		"an effect-free ACK writes nothing")
	require.Equal(t, uint64(2), messagesOn(t, client, completeSubject),
		"an effect-free ACK publishes nothing")
}

// lastCompletionOn decodes the newest completion event the stream retains on
// subject, through the BaseMessage envelope it was published in.
func lastCompletionOn(t *testing.T, client *natsclient.Client, subject string) agentic.LoopCompletedEvent {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	raw, err := stream.GetLastMsgForSubject(t.Context(), subject)
	require.NoError(t, err)
	var envelope struct {
		Payload agentic.LoopCompletedEvent `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw.Data, &envelope))
	return envelope.Payload
}
