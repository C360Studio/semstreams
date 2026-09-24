package agenticloop

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// A governance verdict whose waiter is gone is classified against the loop
// record (#1362 task 2.1, design § 5.6, D16). Before this, every verdict naming
// a live record was Retried, including one whose execution the record already
// shows applied. A re-proposal does re-register the same derived execution ID,
// so a verdict that arrives while its call is still being proposed finds a
// waiter; the stuck case is a verdict that reaches no waiter AFTER its call has
// moved on — Propose already timed out, or the second verdict of the duplicate
// proposed/verdict pair design § 5.6 declares as a residual. Nothing will ever
// wait for it again, so its Retry could only end at MaxDeliver.
//
// Each case feeds the verdict's wire bytes to handleToolCallVerdictMessage
// through deliverylane.Settle with the delayed retry, mirroring the closure
// setupConsumer builds for agent.toolcall.* (it does not drive setupConsumer
// itself), and reads the settlement off the message and the reason off the
// production counter.
//
// Deliberately NOT parallel: loopMetrics is a process-global singleton, so a
// before/after delta on it only means something while nothing else moves it.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestVerdictWithoutWaiterIsClassifiedAgainstTheLoopRecord(t *testing.T) {
	const (
		liveLoopID     = "d2f9c3a4-4e6f-4c7d-8a81-3f4e5d6c7a82"
		awaitingLoopID = "e3a0d4b5-5f70-4d8e-9b92-4a5f6e7d8b93"
		terminalLoopID = "f4b1e5c6-6a81-4e9f-8ca3-5b6a7f8e9ca4"
		absentLoopID   = "a5c2f6d7-7b92-4fa0-9db4-6c7b8a9fadb5"
	)
	executionID := "tool-exec-v1-" + strings.Repeat("b", 52)
	otherExecutionID := "tool-exec-v1-" + strings.Repeat("c", 52)
	request := func(loopID string, iteration int) string {
		return loopID + ":req:" + string(rune('0'+iteration)) + ":0"
	}

	bucket := recordLoopBucket{records: map[string]agentic.LoopEntity{
		liveLoopID: {
			ID: liveLoopID, State: agentic.LoopStateExploring,
			PublishedRequestID: request(liveLoopID, 3),
			PendingToolResults: map[string]agentic.ToolResult{
				executionID: {ExecutionID: executionID, RequestID: request(liveLoopID, 3), Content: "applied"},
			},
		},
		// A gated call: the tool answered approval_required, so the record
		// holds that placeholder for the execution and waits on a human.
		// The placeholder exists only because the call was dispatched, and a
		// call is dispatched only after its verdict was consumed.
		awaitingLoopID: {
			ID: awaitingLoopID, State: agentic.LoopStateAwaitingApproval,
			PublishedRequestID: request(awaitingLoopID, 3),
			PendingToolResults: map[string]agentic.ToolResult{
				executionID: {
					ExecutionID: executionID, RequestID: request(awaitingLoopID, 3),
					Error: agentic.ApprovalRequiredPrefix + "delete_rule needs a human",
				},
			},
			PendingApproval: &agentic.PendingApprovalState{
				ExecutionID: executionID, RequestID: request(awaitingLoopID, 3), CallID: "call-1",
			},
		},
		terminalLoopID: {ID: terminalLoopID, State: agentic.LoopStateComplete},
	}}

	cases := []struct {
		name     string
		fields   map[string]any
		wireRaw  bool // the publish-action shape: raw JSON with fields under properties
		want     natsclient.DeliveryDecision
		reason   string // the settle reason expected to move; "" for a Retry
		guidance string
	}{
		{
			name: "a verdict naming a request older than the record's is acknowledged",
			fields: map[string]any{
				"loop_id": liveLoopID, "request_id": request(liveLoopID, 2), "execution_id": otherExecutionID,
			},
			want: natsclient.DeliveryDecisionAck, reason: verdictDropOlderRequest,
			guidance: "the loop cannot pass a request until its batch is in, so the verdict was consumed",
		},
		{
			name:    "the publish-action shape is ordered by its nested request_id",
			wireRaw: true,
			fields: map[string]any{
				"properties": map[string]any{
					"decision": "rejected", "request_id": request(liveLoopID, 2), "execution_id": otherExecutionID,
				},
			},
			want: natsclient.DeliveryDecisionAck, reason: verdictDropOlderRequest,
			guidance: "the canonical reject rule carries request_id only under properties",
		},
		{
			name: "a current verdict whose execution the record holds is acknowledged",
			fields: map[string]any{
				"loop_id": liveLoopID, "request_id": request(liveLoopID, 3), "execution_id": executionID,
			},
			want: natsclient.DeliveryDecisionAck, reason: verdictDropAlreadyApplied,
		},
		{
			name: "an approval_required placeholder is membership too",
			fields: map[string]any{
				"loop_id": awaitingLoopID, "request_id": request(awaitingLoopID, 3), "execution_id": executionID,
			},
			want: natsclient.DeliveryDecisionAck, reason: verdictDropAlreadyApplied,
			guidance: "a gate exists only for a call that was dispatched, so its verdict was consumed",
		},
		{
			name:   "an empty request_id is classified on membership: held acknowledges",
			fields: map[string]any{"loop_id": liveLoopID, "execution_id": executionID},
			want:   natsclient.DeliveryDecisionAck, reason: verdictDropAlreadyApplied,
		},
		{
			name:     "an empty request_id is classified on membership: unheld retries",
			fields:   map[string]any{"loop_id": liveLoopID, "execution_id": otherExecutionID},
			want:     natsclient.DeliveryDecisionRetry,
			guidance: "with no request name there is no ordering, and an unheld execution may still be owed",
		},
		{
			name: "a current, unseen verdict is still owed and retries",
			fields: map[string]any{
				"loop_id": liveLoopID, "request_id": request(liveLoopID, 3), "execution_id": otherExecutionID,
			},
			want: natsclient.DeliveryDecisionRetry,
		},
		{
			name: "a verdict ahead of the record retries",
			fields: map[string]any{
				"loop_id": liveLoopID, "request_id": request(liveLoopID, 4), "execution_id": otherExecutionID,
			},
			want: natsclient.DeliveryDecisionRetry,
		},
		{
			name: "a verdict naming another loop's request is quarantined, even when its execution is held",
			fields: map[string]any{
				"loop_id": liveLoopID, "request_id": request(awaitingLoopID, 3), "execution_id": executionID,
			},
			want: natsclient.DeliveryDecisionQuarantine, reason: verdictDropForeignRequest,
			guidance: "a request that is not the loop's is quarantined on every lane, and is decided before membership",
		},
		{
			name:   "a verdict for a loop with no record is acknowledged",
			fields: map[string]any{"loop_id": absentLoopID, "execution_id": executionID},
			want:   natsclient.DeliveryDecisionAck, reason: verdictDropLoopAbsent,
		},
		{
			name:   "a verdict for a terminal loop is acknowledged",
			fields: map[string]any{"loop_id": terminalLoopID, "execution_id": executionID},
			want:   natsclient.DeliveryDecisionAck, reason: verdictDropLoopTerminal,
		},
	}

	settleReasons := []string{
		verdictDropOlderRequest, verdictDropAlreadyApplied, verdictDropLoopAbsent, verdictDropLoopTerminal,
		verdictDropForeignRequest, verdictDropUnrecoverableIdentity,
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.ToolCallGovernance.Mode = ToolCallGovernanceModeEnforce
			config.ToolCallGovernance.Timeout = "1s"
			metrics := getMetrics(nil)
			handler := NewMessageHandler(config)
			// The production dispatcher is built with the same metrics, so the
			// waiter miss itself is counted too, as it is in a real process.
			handler.SetGovernanceDispatcher(NewGovernanceDispatcher(
				config.ToolCallGovernance, nil, discardLogger(), metrics))
			c := releaseTestComponent(t, handler)
			c.config = config
			c.loopsBucket = bucket
			c.metrics = metrics

			count := func(reason string) float64 {
				return testutil.ToFloat64(metrics.governanceSubscribeBeforePublishFailures.WithLabelValues(reason))
			}
			before := make(map[string]float64, len(settleReasons)+1)
			for _, reason := range append([]string{verdictDropMissingWaiter}, settleReasons...) {
				before[reason] = count(reason)
			}

			msg := &loopDeliveryOwnerMsg{data: verdictWire(t, tc.wireRaw, tc.fields)}
			retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
			require.NoError(t, err)
			result, admitted := deliverylane.Settle(
				t.Context(), msg, retry, deliverylane.NewAdmission(nil, nil), "loop", c.handleToolCallVerdictMessage)
			require.True(t, admitted)

			require.Equal(t, tc.want, result.Decision(), tc.guidance)
			require.Equal(t, before[verdictDropMissingWaiter]+1, count(verdictDropMissingWaiter),
				"every verdict here reached no waiter")
			switch tc.want {
			case natsclient.DeliveryDecisionAck:
				require.Equal(t, int32(1), msg.acks.Load())
				require.Zero(t, msg.naks.Load()+msg.terms.Load())
			case natsclient.DeliveryDecisionQuarantine:
				require.True(t, result.OwnerStopRequired(), "a quarantine stops the lane's owner")
				require.Zero(t, msg.acks.Load(), "a quarantined verdict is never acknowledged as applied")
				require.ErrorIs(t, result.Err(), ErrNoGovernanceWaiter)
				require.ErrorContains(t, result.Err(), "is not a request of this loop")
			default:
				require.Equal(t, int32(1), msg.naks.Load(), "a retry is a delayed Nak")
				require.Zero(t, msg.acks.Load()+msg.terms.Load())
				require.ErrorIs(t, result.Err(), ErrNoGovernanceWaiter)
			}
			for _, reason := range settleReasons {
				want := before[reason]
				if reason == tc.reason {
					want++
				}
				require.Equal(t, want, count(reason), "reason %q", reason)
			}
		})
	}
}

// verdictWire renders a verdict in one of the two shapes the rule engine
// produces: the approve action's core.json.v1 BaseMessage, or the publish
// action's raw map with the fields nested under properties.
func verdictWire(t *testing.T, raw bool, fields map[string]any) []byte {
	t.Helper()
	if raw {
		data, err := json.Marshal(fields)
		require.NoError(t, err)
		return data
	}
	payload := map[string]any{"decision": "approved"}
	maps.Copy(payload, fields)
	return baseMessageBytes(t, message.NewGenericJSON(payload))
}
