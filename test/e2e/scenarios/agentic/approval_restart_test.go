package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"testing"

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
		for _, phase := range []string{"before", "after"} {
			complete["approval_restart_"+lane+"_sequence_"+phase] = uint64(21)
			complete["approval_restart_"+lane+"_consumer_absent_"+phase] = true
		}
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
		{name: "missing created source before", key: "approval_restart_created_sequence_before"},
		{name: "missing created source after", key: "approval_restart_created_sequence_after"},
		{name: "missing pending source before", key: "approval_restart_pending_sequence_before"},
		{name: "missing pending source after", key: "approval_restart_pending_sequence_after"},
		{name: "created absence omitted before", key: "approval_restart_created_consumer_absent_before"},
		{name: "created absence omitted after", key: "approval_restart_created_consumer_absent_after"},
		{name: "pending absence omitted before", key: "approval_restart_pending_consumer_absent_before"},
		{name: "pending absence omitted after", key: "approval_restart_pending_consumer_absent_after"},
		{name: "created consumer exists before", key: "approval_restart_created_consumer_absent_before", value: false},
		{name: "created consumer exists after", key: "approval_restart_created_consumer_absent_after", value: false},
		{name: "pending consumer exists before", key: "approval_restart_pending_consumer_absent_before", value: false},
		{name: "pending consumer exists after", key: "approval_restart_pending_consumer_absent_after", value: false},
		{name: "unverified approved arguments", key: "approval_restart_tool_call_verified"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			details := maps.Clone(complete)
			if tt.value == nil {
				delete(details, tt.key)
			} else {
				details[tt.key] = tt.value
			}
			if err := s.validateResults(t.Context(), &scenarios.Result{Details: details}); err == nil {
				t.Fatalf("approval restart accepted %s", tt.name)
			}
		})
	}
}

type approvalRestartConsumerFixture struct {
	jetstream.Stream
	err     error
	queried string
}

func (s *approvalRestartConsumerFixture) Consumer(_ context.Context, name string) (jetstream.Consumer, error) {
	s.queried = name
	return nil, s.err
}

// spec: agentic-dispatch / Dispatch is exclusively an edge gateway
func TestApprovalRestartNotificationConsumerAbsenceRequiresExactNotFound(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		absent bool
	}{
		{name: "retired consumer absent", err: jetstream.ErrConsumerNotFound, absent: true},
		{name: "wrapped consumer absent", err: fmt.Errorf("lookup: %w", jetstream.ErrConsumerNotFound), absent: true},
		{name: "consumer still exists"},
		{name: "stream absent is not consumer absence", err: jetstream.ErrStreamNotFound},
		{name: "cancelled read", err: context.Canceled},
		{name: "unavailable read", err: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"agentic-dispatch-agent-created", "agentic-dispatch-agent-approval-pending"} {
				stream := &approvalRestartConsumerFixture{err: tt.err}
				err := verifyApprovalNotificationConsumerAbsent(t.Context(), stream, name)
				if (err == nil) != tt.absent {
					t.Fatalf("consumer absence=%v, want absent=%v", err, tt.absent)
				}
				if stream.queried != name {
					t.Fatalf("queried consumer %q, want exact retired owner %q", stream.queried, name)
				}
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
