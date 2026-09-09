package agentic

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/nats-io/nats.go/jetstream"
)

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalRestartCannotPassWithoutReplacementAndRecovery(t *testing.T) {
	complete := map[string]any{
		"completion_method": "target_trajectory", "approval_outcome": agentic.OutcomeSuccess,
		"signal_outcome":                        agentic.OutcomeCancelled,
		"approval_refusal_non_canonical_status": http.StatusBadRequest,
		"signal_refusal_non_canonical_count":    float64(1),
		"approval_restart_process_before":       float64(100), "approval_restart_process_after": float64(110),
		"approval_restart_loop_id": pagedLoopToken, "approval_restart_execution_id": "execution",
		"approval_restart_source_sequence": uint64(17), "approval_restart_source_ack_floor": uint64(17),
		"approval_restart_result_sequence": uint64(18), "approval_restart_tool_executions": float64(1),
		"approval_restart_outcome":            agentic.OutcomeSuccess,
		"approval_restart_tool_call_verified": true,
	}
	for _, lane := range []string{"created", "pending"} {
		complete["approval_restart_"+lane+"_sequence"] = uint64(21)
		complete["approval_restart_"+lane+"_ack_floor"] = uint64(21)
		complete["approval_restart_"+lane+"_pending"] = 0
		complete["approval_restart_"+lane+"_queued"] = uint64(0)
	}
	s := NewScenario(nil, DefaultConfig())
	if err := s.validateResults(t.Context(), &scenarios.Result{Details: maps.Clone(complete)}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		key   string
		value any
	}{
		{name: "omitted restart", key: "approval_restart_process_after"},
		{name: "same process", key: "approval_restart_process_after", value: float64(100)},
		{name: "unsettled boundary", key: "approval_restart_source_ack_floor", value: uint64(16)},
		{name: "old approval-required result", key: "approval_restart_result_sequence", value: uint64(17)},
		{name: "no executor effect", key: "approval_restart_tool_executions", value: float64(0)},
		{name: "omitted recovery", key: "approval_restart_outcome"},
		{name: "wrong outcome", key: "approval_restart_outcome", value: agentic.OutcomeFailed},
		{name: "missing created source", key: "approval_restart_created_sequence"},
		{name: "unacked created source", key: "approval_restart_created_ack_floor", value: uint64(20)},
		{name: "created delivery pending", key: "approval_restart_created_pending", value: 1},
		{name: "created source queued", key: "approval_restart_created_queued", value: uint64(1)},
		{name: "missing pending notification", key: "approval_restart_pending_sequence"},
		{name: "unacked pending notification", key: "approval_restart_pending_ack_floor", value: uint64(20)},
		{name: "pending notification outstanding", key: "approval_restart_pending_pending", value: 1},
		{name: "pending notification queued", key: "approval_restart_pending_queued", value: uint64(1)},
		{name: "unverified approved arguments", key: "approval_restart_tool_call_verified"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			details := maps.Clone(complete)
			details[tt.key] = tt.value
			if err := s.validateResults(t.Context(), &scenarios.Result{Details: details}); err == nil {
				t.Fatalf("approval restart accepted %s", tt.name)
			}
		})
	}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalRestartNotificationMustBeFullySettled(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ack     uint64
		pending int
		queued  uint64
		settled bool
	}{
		{name: "settled", ack: 21, settled: true},
		{name: "source not acknowledged", ack: 20},
		{name: "delivery outstanding", ack: 21, pending: 1},
		{name: "notification still queued", ack: 21, queued: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := &jetstream.ConsumerInfo{NumAckPending: tt.pending, NumPending: tt.queued}
			info.Delivered.Consumer, info.AckFloor.Consumer = 9, 9
			info.Delivered.Stream, info.AckFloor.Stream = 21, tt.ack
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if !tt.settled {
				cancel()
			}
			_, err := waitForApprovalNotificationSettled(ctx, stageAConsumerFixture{info: info}, 21, time.Second)
			if (err == nil) != tt.settled {
				t.Fatalf("notification settlement=%v, want settled=%v", err, tt.settled)
			}
		})
	}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalRestartChecksApprovedCallArgumentsAndIdentity(t *testing.T) {
	expected := agentic.ToolCall{
		ID: "provider-call", Name: approvalGatedTool, LoopID: pagedLoopToken,
		RequestID: "request", ExecutionID: "execution", CallOrdinal: 1, TraceID: "trace",
		Arguments: map[string]any{"entity_type": "fixture-type", "limit": float64(5)},
	}
	for _, tt := range []struct {
		name   string
		mutate func(*agentic.ToolCall)
	}{
		{name: "matching with production metadata"},
		{name: "different valid arguments", mutate: func(c *agentic.ToolCall) { c.Arguments["limit"] = float64(10) }},
		{name: "wrong approver", mutate: func(c *agentic.ToolCall) { c.ApprovedBy = "other" }},
		{name: "wrong request", mutate: func(c *agentic.ToolCall) { c.RequestID = "other" }},
		{name: "wrong execution", mutate: func(c *agentic.ToolCall) { c.ExecutionID = "other" }},
		{name: "wrong ordinal", mutate: func(c *agentic.ToolCall) { c.CallOrdinal++ }},
		{name: "wrong trace", mutate: func(c *agentic.ToolCall) { c.TraceID = "other" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			approved := expected
			approved.Arguments = maps.Clone(expected.Arguments)
			// dispatchToolCall always adds loop_id; the approval checkpoint does
			// not retain this transport metadata as continuation identity.
			approved.Metadata = map[string]any{"loop_id": expected.LoopID}
			approved.ApprovedBy = approvalRequester
			if tt.mutate != nil {
				tt.mutate(&approved)
			}
			wire, err := json.Marshal(message.NewBaseMessage(approved.Schema(), &approved, "agentic-loop"))
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := approvalRestartCheckpoint{call: expected, source: 17, stream: replayFixtureStream{
				wantSubject: "tool.execute." + approvalGatedTool,
				stored:      &jetstream.RawStreamMsg{Data: wire, Sequence: 18},
			}}
			s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t), config: DefaultConfig()}
			err = s.verifyApprovedRestartCall(t.Context(), checkpoint)
			if (err == nil) != (tt.mutate == nil) {
				t.Fatalf("approved call validation=%v, want match=%v", err, tt.mutate == nil)
			}
		})
	}
}
