//go:build integration

package agenticloop

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// Task 4.1 of #1362, over a real broker: a delivery redelivered to a
// replacement process after its predecessor died between a publication and
// the record update that should have followed it. Case (i) is the approval
// lane's reject-minted W4; case (ii) is a terminal tool result dying inside the
// terminal owner.

// TestARejectionRedeliveredAfterItsNextRequestWasPublished is case (i): the
// approval lane publishes before it writes (task 1.4), so a rejection that
// completes its batch publishes the loop's next request R(N+1) and only then
// compare-and-swaps the record. The predecessor dies between the two. The
// record is still gated at R(N) while the stream retains R(N+1).
//
// The redelivered answer reaches a replacement that does not hold the loop.
// Its cold branch (task 1.1) runs step 0 first, which adopts R(N+1) and clears
// the gate in the same compare-and-swap, so the answer then finds no gate and
// is acknowledged as inapplicable: the rejection is not applied a second time
// and R(N+1) is not published a second time.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARejectionRedeliveredAfterItsNextRequestWasPublished(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, predecessor, handler, "task-approval-reject-w4")
	requestSubject := "agent.request." + loopID

	gated := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, gated.entity.State)
	gate := gated.entity.PendingApproval
	require.NotNil(t, gate)
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject), "fixture: the gate mints no request")
	answer := baseMessageBytes(t, &agentic.ApprovalResponse{
		LoopID: loopID, CallID: gate.CallID, ExecutionID: gate.ExecutionID, RequestID: gate.RequestID,
		Decision: agentic.ApprovalDecisionReject, ApprovedBy: "operator", Reason: "not this rule",
	})

	// The predecessor dies between the carrier's publication and its Update.
	predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
	died, err := predecessor.handleApprovalResponseMessage(t.Context(), answer)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, died,
		"a record write of unknown durability is not an acknowledgement")

	// The residue, read off the server: R(N+1) retained, the record gated at R(N).
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"the rejection's next request is published before the record update")
	next := retainedRequestIdentity(t, client, requestSubject)
	require.NotEqual(t, gated.entity.PublishedRequestID, next)
	residue := loopRecordOf(t, predecessor, loopID)
	require.Equal(t, gated.revision, residue.revision, "the record is the step the crash lost")
	require.Equal(t, agentic.LoopStateAwaitingApproval, residue.entity.State)

	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	var logs lockedLogBuffer
	replacement.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// A delta: the metrics collector is shared by every process in the test
	// binary, so an absolute count depends on what ran before.
	inapplicable := replacement.metrics.toolResultsDropped.WithLabelValues("approval_inapplicable")
	counted := testutil.ToFloat64(inapplicable)
	decision, err := replacement.handleApprovalResponseMessage(t.Context(), answer)
	require.Equal(t, counted+1, testutil.ToFloat64(inapplicable), "the inapplicable answer is counted")

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision,
		"after step 0 adopts R(N+1) the answer is inapplicable, not retried to MaxDeliver")
	require.Contains(t, logs.String(), "approval response ignored",
		"the inapplicable answer is declared at its audit line")
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"R(N+1) is counted once: nothing was republished and the rejection was not re-applied")
	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, next, record.entity.PublishedRequestID)
	require.Equal(t, gated.entity.Iterations+1, record.entity.Iterations)
	require.False(t, record.entity.State.IsTerminal())
	require.NotEqual(t, agentic.LoopStateAwaitingApproval, record.entity.State,
		"the record is running at R(N+1)")
	require.Nil(t, record.entity.PendingApproval, "the gate is cleared by the adopt that advanced past it")
	_, heldErr := replacement.handler.GetLoop(loopID)
	require.Error(t, heldErr, "an inapplicable answer rebuilds nothing")
}

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
	require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch))
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

// TestACancelRedeliveredAfterItsPublicationAdoptsTheDurableCancel covers the
// cancel lane's cold window (owner ruling 2026-09-23, #1362
// issuecomment-5802753726): cancel creates COMPLETE_<loopID> and publishes its
// event before it writes the record, and the process dies before the record.
// The redelivered cancel reaches a replacement that does not hold the loop;
// the record is live, so before the ruling it retried to MaxDeliver while the
// loop's other lanes carried on past a published cancel. It now adopts the
// cancel marker: republish, record cancelled under compare-and-swap, ACK.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACancelRedeliveredAfterItsPublicationAdoptsTheDurableCancel(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID, _ := bornLoop(t, predecessor, handler, "task-cancel-adopt")
	completeSubject := "agent.complete." + loopID
	markerKey := "COMPLETE_" + loopID
	signal := baseMessageBytes(t, &agentic.UserSignal{
		SignalID: "signal-cancel-adopt", Type: agentic.SignalCancel, LoopID: loopID, UserID: "operator",
	})

	predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
	died, err := predecessor.handleSignalMessage(t.Context(), signal)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, died,
		"a cancelled record write of unknown durability is not an acknowledgement")

	marker, err := predecessor.loopsBucket.Get(t.Context(), markerKey)
	require.NoError(t, err, "cancel's marker precedes its event")
	savedMarker := append([]byte(nil), marker.Value()...)
	require.Equal(t, uint64(1), messagesOn(t, client, completeSubject))
	require.False(t, loopRecordOf(t, predecessor, loopID).entity.State.IsTerminal(),
		"fixture check: the record is the step the crash lost")
	var saved agentic.LoopCancelledEvent
	require.NoError(t, json.Unmarshal(savedMarker, &saved))

	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	decision, err := replacement.handleSignalMessage(t.Context(), signal)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision,
		"a cancel whose marker and event are durable is adopted, not retried to MaxDeliver")

	adopted, err := replacement.loopsBucket.Get(t.Context(), markerKey)
	require.NoError(t, err)
	require.Equal(t, marker.Revision(), adopted.Revision(), "the durable cancel was overwritten")
	require.Equal(t, uint64(2), messagesOn(t, client, completeSubject),
		"the adopted cancel is republished — an accepted duplicate")
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	raw, err := stream.GetLastMsgForSubject(t.Context(), completeSubject)
	require.NoError(t, err)
	var envelope struct {
		Payload agentic.LoopCancelledEvent `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw.Data, &envelope))
	require.Equal(t, agentic.OutcomeCancelled, envelope.Payload.Outcome)
	require.True(t, saved.CancelledAt.Equal(envelope.Payload.CancelledAt),
		"the republished cancel is the saved one")

	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, agentic.LoopStateCancelled, record.entity.State)
	require.Equal(t, saved.CancelledBy, record.entity.CancelledBy)
	require.Nil(t, record.entity.PendingApproval)
}

// TestAResponseRedeliveredFirstDoesNotStrandACrashedCancel is review H1 of
// #1362: P1's cancel creates its marker and publishes, then dies before its
// record. On P2 the loop's model response is redelivered FIRST: it rebuilds the
// loop cold and completes it in memory, and the terminal owner refuses that
// completion against the cancel marker (a durable terminal of another kind).
// The cancel's redelivery must still be adopted. Before the fix the refused
// completion stayed terminal in memory, so CancelLoop answered "already
// terminal", the cancel was acknowledged without effect, and the record stayed
// live under a published cancellation.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAResponseRedeliveredFirstDoesNotStrandACrashedCancel(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID, firstRequest := bornLoop(t, predecessor, handler, "task-cancel-then-response")
	completion := agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusComplete, FinishReason: "stop",
		Message: agentic.ChatMessage{Role: "assistant", Content: "an answer that arrived after the cancel"},
	}
	retainModelResponse(t, client, completion)
	signal := baseMessageBytes(t, &agentic.UserSignal{
		SignalID: "signal-cancel-first", Type: agentic.SignalCancel, LoopID: loopID, UserID: "operator",
	})

	predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
	died, err := predecessor.handleSignalMessage(t.Context(), signal)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, died)
	require.False(t, loopRecordOf(t, predecessor, loopID).entity.State.IsTerminal(),
		"fixture check: the cancel's record is the step the crash lost")

	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	var logs lockedLogBuffer
	replacement.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, answered := deliverResponse(t, replacement, completion)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, answered.Decision(),
		"a completion against a durable cancel is conflicting evidence: the first terminal wins")
	_, heldErr := replacement.handler.GetLoop(loopID)
	require.Error(t, heldErr, "the refused completion stayed terminal in memory")

	decision, err := replacement.handleSignalMessage(t.Context(), signal)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, agentic.LoopStateCancelled, record.entity.State,
		"the cancel was acknowledged as already terminal and never adopted: the record is still live "+
			"under a published cancellation")
	require.Contains(t, logs.String(), "Cancel adopted the loop's durable cancel terminal",
		"the cold cancel adoption is declared at its audit line")
}

// lockedLogBuffer is a log sink safe for the goroutines a component logs from.
type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
