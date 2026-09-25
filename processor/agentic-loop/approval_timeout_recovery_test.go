package agenticloop

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// The approval-timeout sweeper's auto-reject rides the carrier (#1362 task
// 1.5, D39): the same publish → compare-and-swap order as an operator's
// rejection, and a terminal it produces goes through the terminal owner like
// every other. Before, the sweeper published and wrote through its own pair,
// and a terminal its rejection produced — a max_iterations failure — was
// published and written with no COMPLETE_<loopID>.
//
// These run against the recording bucket with no NATS client, so publication
// is not observable here; the KV order and what the record and the marker say
// are. The broker half (a failed publication leaves the record as it was) is
// approval_timeout_publish_failure_integration_test.go, unchanged.

// expiredGateOnALoop gates a held loop on one call and backdates the gate past
// its deadline, with the loop's record written through the birth path so the
// carrier holds the revision it compares against. budget is the loop's
// iteration budget; spent says whether the rejection's advance exhausts it.
func expiredGateOnALoop(t *testing.T, spent bool) (*Component, *recordingLoopBucket, string) {
	t.Helper()
	c, bucket, loopID := carrierLoop(t)
	entity, err := c.handler.GetLoop(loopID)
	require.NoError(t, err)
	if spent {
		entity.Iterations = entity.MaxIterations
	}
	require.NoError(t, entity.BeginAwaitingApproval(
		"call-gated", "delete_rule", map[string]any{"rule_id": "rule-42"},
		agentic.ApprovalRequiredPrefix+"needs a human", time.Minute, ""))
	entity.PendingApproval.RequestedAt = time.Now().UTC().Add(-time.Hour)
	require.NoError(t, c.handler.UpdateLoop(entity))
	require.Len(t, c.handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC()), 1,
		"fixture: the sweep must have exactly this loop to work on")
	return c, bucket, loopID
}

// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovalTimeoutTakesTheCarrier(t *testing.T) {
	t.Run("a rejection that ends the loop commits its terminal through the owner", func(t *testing.T) {
		c, bucket, loopID := expiredGateOnALoop(t, true)

		c.sweepExpiredApprovals(t.Context())

		require.Equal(t, []string{"COMPLETE_" + loopID, loopID}, bucket.written(),
			"the sweeper's terminal must create COMPLETE_ before it writes the record terminal")
		marker := terminalMarkerOf(t, bucket, loopID)
		require.Equal(t, agentic.OutcomeFailed, marker["outcome"])
		require.Equal(t, "max_iterations", marker["reason"])
		record := persistedLoop(t, bucket, loopID)
		require.Equal(t, agentic.LoopStateFailed, record.State)
		require.Nil(t, record.PendingApproval)
		_, held := c.handler.GetLoop(loopID)
		require.Error(t, held, "the terminal owner releases the loop once its terminal is committed")
	})

	t.Run("a rejection that advances the loop writes its record once, naming the request it minted", func(t *testing.T) {
		c, bucket, loopID := expiredGateOnALoop(t, false)
		before := persistedLoop(t, bucket, loopID)

		c.sweepExpiredApprovals(t.Context())

		require.Equal(t, []string{loopID}, bucket.written())
		record := persistedLoop(t, bucket, loopID)
		require.Nil(t, record.PendingApproval, "the auto-reject resolves the gate")
		require.NotEqual(t, agentic.LoopStateAwaitingApproval, record.State)
		require.Equal(t, before.Iterations+1, record.Iterations)
		require.NotEqual(t, before.PublishedRequestID, record.PublishedRequestID,
			"the advance names the request it minted (I3)")
	})
}

// TestTheSweepersOwnEchoIsNotCountedAsAnInapplicableAnswer (#1362 checkpoint 2
// re-review, M3): the sweeper publishes its auto-reject on
// agent.approval_response for wire observers, and the component's own
// consumer receives it after the gate is already resolved. That echo is not a
// human answer arriving too late, so it is acknowledged without being counted
// as approval_inapplicable, whether the loop is still held (the rejection
// advanced it) or already released (the rejection ended it).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTheSweepersOwnEchoIsNotCountedAsAnInapplicableAnswer(t *testing.T) {
	for name, spent := range map[string]bool{
		"the loop is still held":        false,
		"the loop was already released": true,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, _ := expiredGateOnALoop(t, spent)
			c.metrics = getMetrics(metric.NewMetricsRegistry())
			c.handler.SetMetrics(c.metrics)
			var echo []byte
			c.SetTestPublishHook(func(subject string, data []byte) {
				if strings.HasPrefix(subject, "agent.approval_response.") {
					echo = data
				}
			})
			c.sweepExpiredApprovals(t.Context())
			require.NotEmpty(t, echo, "fixture: the sweep published its auto-reject to the wire")
			inapplicable := c.metrics.toolResultsDropped.WithLabelValues("approval_inapplicable")
			before := testutil.ToFloat64(inapplicable)

			decision, err := c.handleApprovalResponseMessage(t.Context(), echo)

			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			require.Equal(t, before, testutil.ToFloat64(inapplicable),
				"the sweeper's own echo was counted as a human answer arriving too late")
		})
	}
}
