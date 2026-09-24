//go:build integration

package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// An approval gate leaves its approval_required result in the record's
// pending_tool_results under the gated execution's ID, and an approval does not
// remove it: ResolveApproval clears the state and the gate only. The executor's
// REAL result for the approved call carries that same execution ID. So a
// replacement that counted any entry under that ID as "applied" acknowledged
// the approved call's real result without effect, the model never saw it, and
// the loop hung (#1362 checkpoint 2 review, BLOCKING).
//
// A stored approval_required result is a placeholder, not an answer: it counts
// as applied only against another approval_required result.

// approvedResult is the approved call's real result, as agentic-tools
// publishes it once the call has run.
func approvedResult(gate *agentic.PendingApprovalState, loopID string) agentic.ToolResult {
	return agentic.ToolResult{
		LoopID: loopID, CallID: gate.CallID, Name: gate.ToolName, Content: "rule-42 deleted",
		RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
	}
}

func approveAnswer(t *testing.T, gate *agentic.PendingApprovalState, loopID string) []byte {
	t.Helper()
	return baseMessageBytes(t, &agentic.ApprovalResponse{
		LoopID: loopID, CallID: gate.CallID, ExecutionID: gate.ExecutionID, RequestID: gate.RequestID,
		Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "operator",
	})
}

// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovedCallsResultIsAppliedByAProcessThatDoesNotHoldTheLoop(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, predecessor, handler, "task-approved-result-cold")
	requestSubject := "agent.request." + loopID
	gated := loopRecordOf(t, predecessor, loopID)
	gate := gated.entity.PendingApproval
	require.NotNil(t, gate)

	decision, err := predecessor.handleApprovalResponseMessage(t.Context(), approveAnswer(t, gate, loopID))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	approved := loopRecordOf(t, predecessor, loopID)
	require.Nil(t, approved.entity.PendingApproval)
	require.Contains(t, approved.entity.PendingToolResults, gate.ExecutionID,
		"fixture: the approval leaves the gate's placeholder in the applied set")

	// The executor's real result reaches a process that does not hold the loop.
	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	msg, delivered := deliverToolResult(t, replacement, approvedResult(gate, loopID))

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, gated.entity.Iterations+1, record.entity.Iterations,
		"the approved call's real result was acknowledged without being applied: the loop is stranded")
	require.NotEqual(t, gated.entity.PublishedRequestID, record.entity.PublishedRequestID)
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
		"the applied result completes the batch and publishes the loop's next request")
}

// The approve-W2 order the publish → write flip opens: the predecessor
// publishes tool.execute and dies before its record update, so the record is
// still gated. The executor's real result reaches the replacement FIRST, and
// the redelivered answer after it. agentic-tools replays a duplicate
// tool.execute under the same message ID, which the stream's duplicate window
// suppresses, so no second result is coming: the first one must not be lost.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovedResultThatOutrunsTheApprovalsRecordConverges(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, predecessor, handler, "task-approved-result-w2")
	requestSubject := "agent.request." + loopID
	gated := loopRecordOf(t, predecessor, loopID)
	gate := gated.entity.PendingApproval
	require.NotNil(t, gate)
	answer := approveAnswer(t, gate, loopID)

	predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
	died, err := predecessor.handleApprovalResponseMessage(t.Context(), answer)
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, died)
	require.Equal(t, agentic.LoopStateAwaitingApproval, loopRecordOf(t, predecessor, loopID).entity.State,
		"fixture: the record is the step the crash lost")

	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	result := approvedResult(gate, loopID)

	// 1. The real result first: the approval is not durable yet, so it is
	// retried rather than dropped.
	_, early := deliverToolResult(t, replacement, result)
	require.Equal(t, natsclient.DeliveryDecisionRetry, early.Decision(),
		"a real result for a gate still pending on the record is owed, not applied work")

	// 2. The redelivered answer rebuilds the loop and applies the approval.
	decision, err := replacement.handleApprovalResponseMessage(t.Context(), answer)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)

	// 3. The real result's redelivery is applied.
	msg, applied := deliverToolResult(t, replacement, result)
	require.Equal(t, natsclient.DeliveryDecisionAck, applied.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	record := loopRecordOf(t, replacement, loopID)
	require.Equal(t, gated.entity.Iterations+1, record.entity.Iterations, "the loop hung on its approved call")
	require.Nil(t, record.entity.PendingApproval)
	require.Equal(t, uint64(2), messagesOn(t, client, requestSubject))
}

// TestAColdGatedResultReEchoesItsGateOnTheStream: the gated result redelivered
// to a replacement while the record still awaits that gate re-publishes the
// ApprovalPendingEvent, read back off the stream, and is acknowledged.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdGatedResultReEchoesItsGateOnTheStream(t *testing.T) {
	client := newLoopNATS(t)
	predecessor, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, predecessor, handler, "task-cold-reecho")
	pendingSubject := "agent.approval_pending." + loopID
	gated := loopRecordOf(t, predecessor, loopID)
	gate := gated.entity.PendingApproval
	require.NotNil(t, gate)
	require.Equal(t, uint64(1), messagesOn(t, client, pendingSubject), "fixture: the gate echoed once")

	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	msg, delivered := deliverToolResult(t, replacement, agentic.ToolResult{
		LoopID: loopID, CallID: gate.CallID, Name: gate.ToolName, Error: gate.Reason,
		RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
	})

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, uint64(2), messagesOn(t, client, pendingSubject), "the pending gate was not re-echoed")
	require.Equal(t, gated.revision, loopRecordOf(t, replacement, loopID).revision, "a re-echo writes nothing")
	_, heldErr := replacement.handler.GetLoop(loopID)
	require.Error(t, heldErr, "a re-echo seats nothing")
}

// TestAWarmGatedResultReEchoesItsGateThroughTheLane is task 1.3 at the lane
// seam: the gated result redelivered through handleToolResultMessage to the
// process that holds the gated loop re-publishes the ApprovalPendingEvent,
// counted on the stream, and is acknowledged.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAWarmGatedResultReEchoesItsGateThroughTheLane(t *testing.T) {
	client := newLoopNATS(t)
	holder, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, holder, handler, "task-warm-reecho")
	pendingSubject := "agent.approval_pending." + loopID
	gate := loopRecordOf(t, holder, loopID).entity.PendingApproval
	require.NotNil(t, gate)
	require.Equal(t, uint64(1), messagesOn(t, client, pendingSubject), "fixture: the gate echoed once")
	_, heldErr := handler.GetLoop(loopID)
	require.NoError(t, heldErr, "fixture: this process holds the gated loop")

	msg, delivered := deliverToolResult(t, holder, agentic.ToolResult{
		LoopID: loopID, CallID: gate.CallID, Name: gate.ToolName, Error: gate.Reason,
		RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
	})

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, uint64(2), messagesOn(t, client, pendingSubject), "the warm lane did not re-echo the gate")
	require.Equal(t, agentic.LoopStateAwaitingApproval, loopRecordOf(t, holder, loopID).entity.State)
}

// TestAWarmGatedResultRedeliveredAfterItsApprovalIsAcknowledged (#1362
// checkpoint 2 re-review, M2b): the gated result is redelivered to the process
// holding the loop AFTER its gate was answered. The loop is running again, so
// the gated result used to re-gate it — a second gate and a second event for
// an already-approved call — while the cold arm acknowledges the same input.
// Warm and cold now agree: an approval_required result for an execution the
// record already holds is applied work.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAWarmGatedResultRedeliveredAfterItsApprovalIsAcknowledged(t *testing.T) {
	client := newLoopNATS(t)
	holder, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, holder, handler, "task-warm-regate")
	pendingSubject := "agent.approval_pending." + loopID
	gate := loopRecordOf(t, holder, loopID).entity.PendingApproval
	require.NotNil(t, gate)
	decision, err := holder.handleApprovalResponseMessage(t.Context(), approveAnswer(t, gate, loopID))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	approved := loopRecordOf(t, holder, loopID)
	require.Nil(t, approved.entity.PendingApproval)
	dropped := holder.metrics.toolResultsDropped.WithLabelValues("already_applied")
	before := testutil.ToFloat64(dropped)

	msg, delivered := deliverToolResult(t, holder, agentic.ToolResult{
		LoopID: loopID, CallID: gate.CallID, Name: gate.ToolName, Error: gate.Reason,
		RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
	})

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	require.Equal(t, int32(1), msg.acks.Load())
	require.Equal(t, uint64(1), messagesOn(t, client, pendingSubject), "the answered gate was gated a second time")
	after := loopRecordOf(t, holder, loopID)
	require.Equal(t, approved.revision, after.revision, "an effect-free ACK writes nothing")
	require.Nil(t, after.entity.PendingApproval)
	require.Equal(t, before+1, testutil.ToFloat64(dropped))
}

// TestAWarmResultThatIsNotAGateDoesNotReEchoIt (M2a): only a redelivered
// approval_required result re-echoes a pending gate, as on the cold arm; a
// result that merely shares the gate's identity does not.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAWarmResultThatIsNotAGateDoesNotReEchoIt(t *testing.T) {
	client := newLoopNATS(t)
	holder, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, holder, handler, "task-warm-no-reecho")
	pendingSubject := "agent.approval_pending." + loopID
	gate := loopRecordOf(t, holder, loopID).entity.PendingApproval
	require.NotNil(t, gate)

	_, _ = deliverToolResult(t, holder, approvedResult(gate, loopID))

	require.Equal(t, uint64(1), messagesOn(t, client, pendingSubject),
		"a result that is not approval_required re-echoed the gate")
}
