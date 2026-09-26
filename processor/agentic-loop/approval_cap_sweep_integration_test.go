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
