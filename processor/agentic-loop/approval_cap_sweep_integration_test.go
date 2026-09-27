//go:build integration

package agenticloop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// T2 of #1377 at the iteration cap, over a real stream so the answer's
// publications are counted (review 1 LOW 9): the loop record and the evidence
// are the cold-approval fixture's (approval_restore_order_test.go).

// toolExecutionsOn counts every tool.execute message retained for loopID.
func toolExecutionsOn(t *testing.T, client *natsclient.Client, loopID string) int {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	info, err := stream.Info(t.Context())
	require.NoError(t, err)
	n := 0
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
		raw, err := stream.GetMsg(t.Context(), seq)
		if err != nil || !strings.HasPrefix(raw.Subject, "tool.execute.") {
			continue
		}
		var envelope struct {
			Payload agentic.ToolCall `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(raw.Data, &envelope))
		if envelope.Payload.LoopID == loopID {
			n++
		}
	}
	return n
}

// sweptAtTheCap is W2 of #1377 at the iteration cap: a gated loop whose record
// is at its iteration budget with no loop deadline, whose approval-timeout
// sweep auto-rejected, committed COMPLETE_<loopID> as a max_iterations failure
// and then failed to publish — so the record stays gated and no process holds
// the loop.
func sweptAtTheCap(t *testing.T, client *natsclient.Client) (coldApproval, []byte) {
	t.Helper()
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.MaxIterations = e.Iterations
		e.TimeoutAt = time.Time{}
		e.PendingApproval.RequestedAt = time.Now().Add(-2 * time.Hour)
	})
	rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), a.answer(agentic.ApprovalDecisionReject))
	require.NoError(t, err)
	require.True(t, rebuilt)

	a.c.natsClient = unpublishableClient(t)
	a.c.sweepExpiredApprovals(t.Context())

	require.Equal(t, "max_iterations", terminalMarkerOf(t, a.bucket, coldApprovalLoopID)["reason"],
		"fixture: the sweep committed a max_iterations failure before its publication failed")
	require.Equal(t, agentic.LoopStateAwaitingApproval, persistedLoop(t, a.bucket, coldApprovalLoopID).State,
		"the residual: the record stays gated")
	require.False(t, a.held(), "the failed commit released the loop")
	// From here every publication reaches a real stream, so what the next
	// answer dispatches is counted where an executor would read it.
	a.c.natsClient = client
	marker, ok := a.bucket.value(terminalMarkerKey(coldApprovalLoopID))
	require.True(t, ok)
	return a, marker
}

// requireAdoptedAtTheCap: the durable max_iterations failure was adopted, not
// replaced — the record is terminal with the gate cleared, the marker is the
// sweep's, byte for byte, and the terminal is counted once.
func requireAdoptedAtTheCap(t *testing.T, a coldApproval, marker []byte, failedBefore float64) {
	t.Helper()
	record := persistedLoop(t, a.bucket, coldApprovalLoopID)
	require.Equal(t, agentic.LoopStateFailed, record.State)
	require.Contains(t, record.Error, "max iterations")
	require.Nil(t, record.PendingApproval, "a durable terminal clears the approval gate")
	after, ok := a.bucket.value(terminalMarkerKey(coldApprovalLoopID))
	require.True(t, ok)
	require.Equal(t, string(marker), string(after), "the durable terminal is adopted, never overwritten")
	require.Equal(t, failedBefore+1, testutil.ToFloat64(a.c.metrics.loopsFailed.WithLabelValues("max_iterations")),
		"the adopted terminal is counted once")
	require.False(t, a.held(), "a committed terminal releases the loop")
}

// TestASweepAtTheIterationCapWhosePublishFailedSettlesOnTheNextAnswer is T2's
// cap arm and cancel arm (#1377, OQ2 (a), OQ7 (a)): the record converges on
// the next answer to the gate and on nothing else.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestASweepAtTheIterationCapWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {
	t.Run("a reject dispatches nothing and adopts the durable terminal", func(t *testing.T) {
		a, marker := sweptAtTheCap(t, newLoopNATS(t))
		failedBefore := testutil.ToFloat64(a.c.metrics.loopsFailed.WithLabelValues("max_iterations"))

		settled, err := a.deliver(t, a.answerOf(agentic.ApprovalDecisionReject))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, settled)
		require.Zero(t, toolExecutionsOn(t, a.c.natsClient, coldApprovalLoopID), "no tool.execute is published")
		requireAdoptedAtTheCap(t, a, marker, failedBefore)
		require.Equal(t, uint64(1), messagesOn(t, a.c.natsClient, "agent.failed."+coldApprovalLoopID),
			"the adopted durable terminal's saved event is published once")
	})

	t.Run("an approve dispatches the approved call once and adopts when its result completes the batch", func(t *testing.T) {
		a, marker := sweptAtTheCap(t, newLoopNATS(t))
		failedBefore := testutil.ToFloat64(a.c.metrics.loopsFailed.WithLabelValues("max_iterations"))

		settled, err := a.deliver(t, a.answerOf(agentic.ApprovalDecisionApprove))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, settled)
		require.Equal(t, 1, approvedToolCallsOn(t, a.c.natsClient, coldApprovalLoopID),
			"the bound: the approved call is published once, after the durable terminal")
		require.Equal(t, []string{a.gate.CallID}, a.c.handler.loopManager.GetPendingTools(coldApprovalLoopID))
		gated := persistedLoop(t, a.bucket, coldApprovalLoopID)
		require.Nil(t, gated.PendingApproval, "the answer cleared the gate")
		require.False(t, gated.State.IsTerminal(), "the record converges only when the call's result arrives")

		_, delivered := deliverToolResult(t, a.c, agentic.ToolResult{
			CallID: a.gate.CallID, Name: a.gate.ToolName, Content: "the approved call ran", LoopID: coldApprovalLoopID,
			RequestID: a.gate.RequestID, ExecutionID: a.gate.ExecutionID, CallOrdinal: a.gate.CallOrdinal,
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		requireAdoptedAtTheCap(t, a, marker, failedBefore)
		require.Equal(t, 1, toolExecutionsOn(t, a.c.natsClient, coldApprovalLoopID),
			"nothing further is dispatched once the batch completes at the cap")
	})

	// The approved TERMINAL tool at the cap (#1399; owner ruling #1146
	// issuecomment-5854830449, Q2): `decide` returns StopLoop: true, so the
	// result derives a completion before the iteration-cap check, and a
	// completion is a different kind from the saved failure. Before #1399 the
	// terminal owner refused it and the delivery was quarantined; now the
	// saved failure is the durable winner — adopted, republished, and the
	// record written from it. The approved call's result is SHAPED as the
	// terminal tool's (StopLoop, its content the decision); the real decide
	// executor lives in agentic-tools.
	t.Run("an approved terminal tool's completion adopts the saved failure as the durable winner", func(t *testing.T) {
		a, marker := sweptAtTheCap(t, newLoopNATS(t))
		completedBefore := testutil.ToFloat64(a.c.metrics.loopsCompleted)
		failedBefore := testutil.ToFloat64(a.c.metrics.loopsFailed.WithLabelValues("max_iterations"))

		settled, err := a.deliver(t, a.answerOf(agentic.ApprovalDecisionApprove))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, settled)
		require.Equal(t, 1, approvedToolCallsOn(t, a.c.natsClient, coldApprovalLoopID),
			"fixture: the approved call is published once")

		_, delivered := deliverToolResult(t, a.c, agentic.ToolResult{
			CallID: a.gate.CallID, Name: a.gate.ToolName, Content: "decided: delete the rule", StopLoop: true,
			LoopID: coldApprovalLoopID, RequestID: a.gate.RequestID, ExecutionID: a.gate.ExecutionID,
			CallOrdinal: a.gate.CallOrdinal,
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a completion meeting this loop's durable failure adopts it: an ordinary commit, never a quarantine")
		requireAdoptedAtTheCap(t, a, marker, failedBefore)
		require.Equal(t, "max_iterations", terminalMarkerOf(t, a.bucket, coldApprovalLoopID)["reason"])
		require.Equal(t, uint64(1), messagesOn(t, a.c.natsClient, "agent.failed."+coldApprovalLoopID),
			"the saved failure event is republished once")
		require.Zero(t, messagesOn(t, a.c.natsClient, "agent.complete."+coldApprovalLoopID),
			"no completion event is published")
		require.Empty(t, persistedLoop(t, a.bucket, coldApprovalLoopID).Result,
			"the losing completion's result is not written")
		require.Equal(t, completedBefore, testutil.ToFloat64(a.c.metrics.loopsCompleted),
			"the completion that lost is not counted")
	})

	t.Run("a cancel is retried and never settles the record", func(t *testing.T) {
		a, marker := sweptAtTheCap(t, newLoopNATS(t))
		before := persistedLoop(t, a.bucket, coldApprovalLoopID)

		settled, err := a.c.handleSignalMessage(t.Context(), baseMessageBytes(t, &agentic.UserSignal{
			SignalID: "signal-w2", Type: agentic.SignalCancel, LoopID: coldApprovalLoopID,
			UserID: "operator", Timestamp: time.Now().UTC(),
		}))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionRetry, settled,
			"the cold cancel arm adopts only a cancel marker (OQ7): retried until MaxDeliver")
		after := persistedLoop(t, a.bucket, coldApprovalLoopID)
		require.Equal(t, agentic.LoopStateAwaitingApproval, after.State, "the record stays gated")
		require.Equal(t, before, after)
		current, ok := a.bucket.value(terminalMarkerKey(coldApprovalLoopID))
		require.True(t, ok)
		require.Equal(t, string(marker), string(current))
		require.False(t, a.held())
	})
}

// The terminal tool at the cap on the production lanes (#1399, inverting the
// #1377 review 3 MEDIUM A observation; owner ruling #1146
// issuecomment-5854830449, Q2). The fourth arm above settles through
// deliverylane.Consume with no admission, so it cannot see what the lane does
// with the disposition. Here the W2 state is built on a startRaceLane
// process: a gated loop at its iteration cap, an approval-timeout sweep that
// commits its max_iterations failure and cannot publish it, and then a cold
// approve and the approved terminal tool's completion on the tool.result lane.
// The completion adopts the saved failure and is acknowledged; before #1399 it
// was quarantined, which latched loop health and drained the lane, and a valid
// tool result for another loop in the same process was then refused
// unsettled. That second loop's result is now consumed and applied.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovedTerminalToolAtTheCapAdoptsTheSavedFailureAndTheLaneKeepsConsuming(t *testing.T) {
	lane := startRaceLane(t)
	c, h := lane.c, lane.h
	loopID, gate := gatedLaneLoop(t, lane, "task-cap-terminal-tool")
	otherLoopID, otherCall := toolBatchLoop(t, lane, "task-cap-other", "search")

	// At the cap, with an approval deadline already past, written to the record
	// the way any carrier write is.
	entity, err := h.GetLoop(loopID)
	require.NoError(t, err)
	entity.MaxIterations = entity.Iterations
	entity.PendingApproval.Timeout = time.Minute
	entity.PendingApproval.RequestedAt = time.Now().Add(-2 * time.Hour)
	require.NoError(t, h.UpdateLoop(entity))
	require.NoError(t, c.persistLoopState(t.Context(), loopID))

	c.natsClient = unpublishableClient(t)
	c.sweepExpiredApprovals(t.Context())
	c.natsClient = lane.client
	marker, err := c.loopsBucket.Get(t.Context(), terminalMarkerKey(loopID))
	require.NoError(t, err, "fixture: the sweep committed its failure before its publication failed")
	_, heldErr := h.GetLoop(loopID)
	require.Error(t, heldErr, "fixture: the failed commit released the loop")
	gated := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, gated.entity.State, "fixture: the record stays gated")

	approveMsg, approveDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, approveDone, "cold approve returned")
	require.Equal(t, int32(1), approveMsg.acks.Load(), "fixture: the cold approve dispatches (%s)", dispositionOf(approveMsg))
	require.Equal(t, 1, approvedToolCallsOn(t, lane.client, loopID), "fixture: the approved call is published once")
	approved := loopRecordOf(t, c, loopID)

	resultMsg, resultDone := deliverHeartbeatOn(t, lane, "tool.result", baseMessageBytes(t, &agentic.ToolResult{
		CallID: gate.CallID, Name: gate.ToolName, Content: "decided: delete the rule", StopLoop: true,
		LoopID: loopID, RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
	}))
	waitFor(t, resultDone, "terminal tool result returned")
	t.Logf("terminal tool at the cap: %s; drains=%d", heartbeatDisposition(resultMsg),
		lane.handles["tool.result"].drains.Load())

	require.Equal(t, int32(1), resultMsg.acks.Load(),
		"the completion adopts the saved failure and is acknowledged (%s)", heartbeatDisposition(resultMsg))
	require.Zero(t, resultMsg.naks.Load()+resultMsg.terms.Load())
	after, err := c.loopsBucket.Get(t.Context(), terminalMarkerKey(loopID))
	require.NoError(t, err)
	require.Equal(t, string(marker.Value()), string(after.Value()), "the saved failure is kept byte for byte")
	require.Equal(t, uint64(1), messagesOn(t, lane.client, "agent.failed."+loopID),
		"the saved failure event is republished")
	require.Zero(t, messagesOn(t, lane.client, "agent.complete."+loopID), "no completion event is published")
	terminal := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateFailed, terminal.entity.State, "the record is terminal in the saved kind")
	require.Contains(t, terminal.entity.Error, "max iterations", "with the saved failure's error")
	require.Nil(t, terminal.entity.PendingApproval)
	require.Less(t, approved.revision, terminal.revision, "the record is written past the approve's revision")
	requireLaneNotLatched(t, lane, "tool.result")

	// Every other loop in this process: a valid tool result for another loop
	// is consumed and applied.
	otherBefore := loopRecordOf(t, c, otherLoopID)
	otherMsg, otherDone := deliverHeartbeatOn(t, lane, "tool.result", baseMessageBytes(t, &agentic.ToolResult{
		CallID: otherCall.ID, Name: otherCall.Name, Content: "searched", LoopID: otherLoopID,
		RequestID: otherCall.RequestID, ExecutionID: otherCall.ExecutionID, CallOrdinal: otherCall.CallOrdinal,
	}))
	waitFor(t, otherDone, "the other loop's tool result returned")
	require.Equal(t, int32(1), otherMsg.acks.Load(),
		"the lane still consumes another loop's result (%s)", heartbeatDisposition(otherMsg))
	otherAfter := loopRecordOf(t, c, otherLoopID)
	require.Less(t, otherBefore.revision, otherAfter.revision, "and applies it: the other loop's record moves")
	require.NotEqual(t, otherCall.RequestID, otherAfter.entity.PublishedRequestID,
		"its completed batch published the loop's next request")
	requireLaneNotLatched(t, lane, "tool.result")
}
