package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// approvalRegistry returns a registry with the agentic payload set
// installed — enough for ApprovalPendingEvent / ApprovalResponse
// round-trip tests without pulling in payloadbuiltins (which would
// create a cycle since payloadbuiltins imports agentic).
func approvalRegistry(t *testing.T) *payloadregistry.Registry {
	t.Helper()
	return payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
}

// approvalFixtureLoopID is a canonical framework-minted loop token. Every
// approval fixture below carries one: since #1228 both approval payloads refuse
// a non-canonical loop_id, so a fixture spelling approvalFixtureLoopID would be asserting a
// shape the framework no longer accepts (ADR-105).
const approvalFixtureLoopID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

func TestApprovalPendingEvent_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		event   agentic.ApprovalPendingEvent
		wantErr string
	}{
		{
			name: "complete event passes",
			event: agentic.ApprovalPendingEvent{
				LoopID:      approvalFixtureLoopID,
				CallID:      "call-001",
				ToolName:    "delete_rule",
				Reason:      "approval_required: Tool 'delete_rule' requires human approval",
				RequestedAt: time.Now(),
			},
		},
		{
			name: "missing loop_id fails",
			event: agentic.ApprovalPendingEvent{
				CallID:   "call-001",
				ToolName: "delete_rule",
			},
			wantErr: "loop_id required",
		},
		{
			name: "missing call_id fails",
			event: agentic.ApprovalPendingEvent{
				LoopID:   approvalFixtureLoopID,
				ToolName: "delete_rule",
			},
			wantErr: "call_id required",
		},
		{
			name: "missing tool_name fails",
			event: agentic.ApprovalPendingEvent{
				LoopID: approvalFixtureLoopID,
				CallID: "call-001",
			},
			wantErr: "tool_name required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.event.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate: want error %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestApprovalResponse_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response agentic.ApprovalResponse
		wantErr  string
	}{
		{
			name: "approve with approver passes",
			response: agentic.ApprovalResponse{
				LoopID:     approvalFixtureLoopID,
				CallID:     "call-001",
				Decision:   agentic.ApprovalDecisionApprove,
				ApprovedBy: "alice@example.com",
				DecidedAt:  time.Now(),
			},
		},
		{
			name: "reject without approver passes",
			response: agentic.ApprovalResponse{
				LoopID:    approvalFixtureLoopID,
				CallID:    "call-001",
				Decision:  agentic.ApprovalDecisionReject,
				Reason:    "policy violation",
				DecidedAt: time.Now(),
			},
		},
		{
			name: "modify with approver and arguments passes",
			response: agentic.ApprovalResponse{
				LoopID:            approvalFixtureLoopID,
				CallID:            "call-001",
				Decision:          agentic.ApprovalDecisionModify,
				ApprovedBy:        "alice@example.com",
				ModifiedArguments: map[string]any{"path": "/tmp/safe"},
				DecidedAt:         time.Now(),
			},
		},
		{
			name: "approve without approver fails",
			response: agentic.ApprovalResponse{
				LoopID:   approvalFixtureLoopID,
				CallID:   "call-001",
				Decision: agentic.ApprovalDecisionApprove,
			},
			wantErr: "approved_by required",
		},
		{
			name: "modify without approver fails",
			response: agentic.ApprovalResponse{
				LoopID:   approvalFixtureLoopID,
				CallID:   "call-001",
				Decision: agentic.ApprovalDecisionModify,
			},
			wantErr: "approved_by required",
		},
		{
			name: "unknown decision fails",
			response: agentic.ApprovalResponse{
				LoopID:   approvalFixtureLoopID,
				CallID:   "call-001",
				Decision: "abstain",
			},
			wantErr: "decision must be one of",
		},
		{
			name: "missing loop_id fails",
			response: agentic.ApprovalResponse{
				CallID:   "call-001",
				Decision: agentic.ApprovalDecisionReject,
			},
			wantErr: "loop_id required",
		},
		{
			name: "missing call_id fails",
			response: agentic.ApprovalResponse{
				LoopID:   approvalFixtureLoopID,
				Decision: agentic.ApprovalDecisionReject,
			},
			wantErr: "call_id required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.response.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate: want error %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestApprovalPendingEvent_BaseMessageRoundTrip(t *testing.T) {
	t.Parallel()

	original := &agentic.ApprovalPendingEvent{
		LoopID:      approvalFixtureLoopID,
		CallID:      "call-001",
		ToolName:    "delete_rule",
		Arguments:   map[string]any{"rule_id": "rule-42"},
		Reason:      "approval_required: Tool 'delete_rule' requires human approval before execution",
		RequestedAt: time.Now().UTC().Truncate(time.Second),
		Timeout:     30 * time.Second,
		TraceID:     "trace-xyz",
	}

	wire, err := message.NewBaseMessage(original.Schema(), original, "approval-test").MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	dec := message.NewDecoder(approvalRegistry(t))
	decoded, err := dec.Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got, ok := decoded.Payload().(*agentic.ApprovalPendingEvent)
	if !ok {
		t.Fatalf("payload = %T, want *ApprovalPendingEvent", decoded.Payload())
	}

	if got.LoopID != original.LoopID || got.CallID != original.CallID ||
		got.ToolName != original.ToolName || got.Reason != original.Reason ||
		got.Timeout != original.Timeout || got.TraceID != original.TraceID {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", got, original)
	}
	if !got.RequestedAt.Equal(original.RequestedAt) {
		t.Errorf("RequestedAt mismatch: got=%v want=%v", got.RequestedAt, original.RequestedAt)
	}
	if v, ok := got.Arguments["rule_id"].(string); !ok || v != "rule-42" {
		t.Errorf("Arguments[rule_id] = %v, want rule-42", got.Arguments["rule_id"])
	}
}

func TestApprovalResponse_BaseMessageRoundTrip(t *testing.T) {
	t.Parallel()

	original := &agentic.ApprovalResponse{
		LoopID:            approvalFixtureLoopID,
		CallID:            "call-001",
		Decision:          agentic.ApprovalDecisionModify,
		ModifiedArguments: map[string]any{"path": "/tmp/safe"},
		Reason:            "narrowed scope before approval",
		ApprovedBy:        "alice@example.com",
		DecidedAt:         time.Now().UTC().Truncate(time.Second),
	}

	wire, err := message.NewBaseMessage(original.Schema(), original, "approval-test").MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	dec := message.NewDecoder(approvalRegistry(t))
	decoded, err := dec.Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got, ok := decoded.Payload().(*agentic.ApprovalResponse)
	if !ok {
		t.Fatalf("payload = %T, want *ApprovalResponse", decoded.Payload())
	}

	if got.LoopID != original.LoopID || got.CallID != original.CallID ||
		got.Decision != original.Decision || got.Reason != original.Reason ||
		got.ApprovedBy != original.ApprovedBy {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", got, original)
	}
	if !got.DecidedAt.Equal(original.DecidedAt) {
		t.Errorf("DecidedAt mismatch: got=%v want=%v", got.DecidedAt, original.DecidedAt)
	}
	if v, ok := got.ModifiedArguments["path"].(string); !ok || v != "/tmp/safe" {
		t.Errorf("ModifiedArguments[path] = %v, want /tmp/safe", got.ModifiedArguments["path"])
	}

	// Confirm raw wire format includes the new category so consumers can
	// route by it without re-decoding.
	if !strings.Contains(string(wire), `"category":"approval_response"`) {
		t.Errorf("wire envelope missing approval_response category: %s", wire)
	}
	// Ensure a stable envelope JSON shape by re-marshaling and confirming
	// the decoded payload still validates.
	if err := got.Validate(); err != nil {
		t.Errorf("decoded ApprovalResponse failed Validate: %v", err)
	}
	// Quick sanity: marshal one more time and ensure deterministic
	// re-decode (no nil/zero-value drift on the second pass).
	re, err := json.Marshal(message.NewBaseMessage(got.Schema(), got, "approval-test"))
	if err != nil {
		t.Fatalf("re-MarshalJSON: %v", err)
	}
	if _, err := dec.Decode(re); err != nil {
		t.Fatalf("re-Decode: %v", err)
	}
}

// spec: entity-id-contract / A loop instance token is a framework-minted UUID
// Scenario: every remaining loop-token carrier refuses a non-canonical token.
func TestApprovalPendingEventRefusesNonCanonicalLoopID(t *testing.T) {
	t.Parallel()

	event := agentic.ApprovalPendingEvent{
		LoopID:      "loop_ab12cd34",
		CallID:      "call-001",
		ToolName:    "delete_rule",
		RequestedAt: time.Now(),
	}

	err := event.Validate()
	if err == nil {
		t.Fatal("Validate: want a refusal for a non-canonical loop_id, got nil")
	}
	if !strings.Contains(err.Error(), "loop_id") {
		t.Errorf("refusal must name the offending field, got %q", err)
	}
	if !strings.Contains(err.Error(), "loop_ab12cd34") {
		t.Errorf("refusal must quote the offending value, got %q", err)
	}

	// The refusal reaches the wire boundary too: a malformed token cannot be
	// published, not merely cannot be constructed.
	if _, marshalErr := message.NewBaseMessage(event.Schema(), &event, "approval-test").MarshalJSON(); marshalErr == nil {
		t.Fatal("MarshalJSON: want a refusal for a non-canonical loop_id, got nil")
	}
}

// spec: entity-id-contract / A loop instance token is a framework-minted UUID
// Scenario: every remaining loop-token carrier refuses a non-canonical token.
func TestApprovalResponseRefusesNonCanonicalLoopID(t *testing.T) {
	t.Parallel()

	response := agentic.ApprovalResponse{
		LoopID:     "loop_ab12cd34",
		CallID:     "call-001",
		Decision:   agentic.ApprovalDecisionApprove,
		ApprovedBy: "alice@example.com",
		DecidedAt:  time.Now(),
	}

	err := response.Validate()
	if err == nil {
		t.Fatal("Validate: want a refusal for a non-canonical loop_id, got nil")
	}
	if !strings.Contains(err.Error(), "loop_id") {
		t.Errorf("refusal must name the offending field, got %q", err)
	}

	if _, marshalErr := message.NewBaseMessage(response.Schema(), &response, "approval-test").MarshalJSON(); marshalErr == nil {
		t.Fatal("MarshalJSON: want a refusal for a non-canonical loop_id, got nil")
	}
}

// spec: entity-id-contract / A loop instance token is a framework-minted UUID
// Scenario: every remaining loop-token carrier refuses a non-canonical token —
// "no payload type carrying a loop token remains whose validation accepts every
// input".
//
// The table IS the census the requirement closes, and it is deliberately not a
// reflective sweep over every registered payload with a loop_id field. The
// framework-published loop events (loop-created, loop-completed, loop-failed,
// loop-cancelled) and the execution trace payloads carry a token the FRAMEWORK
// authored on a stream it published; they are recorded carve-outs in the
// agentic-dispatch spec, not omissions. A new carrier of a CALLER-supplied loop
// token joins this table.
//
// One census member is out of reach from this package: agenticdispatch's own
// control-signal message, whose Validate returned nil unconditionally. It is
// RETIRED rather than validated (loop-scoped-request-seams task 9.3) — a
// carrier with no reader is deleted — and agenticdispatch imports this package,
// so the check cannot live here. Until that task lands, the census is closed
// here and open there.
func TestNoLoopTokenCarrierAcceptsEveryInput(t *testing.T) {
	t.Parallel()

	const nonCanonical = "loop_ab12cd34"

	carriers := []struct {
		name    string
		payload message.Payload
	}{
		{
			name: "TaskMessage",
			payload: &agentic.TaskMessage{
				LoopID: nonCanonical, TaskID: "task-1", Role: "general",
				Model: "test-model", Prompt: "hello",
			},
		},
		{
			name: "UserSignal",
			payload: &agentic.UserSignal{
				SignalID: "sig-1", Type: agentic.SignalCancel,
				LoopID: nonCanonical, UserID: "user-1", Timestamp: time.Now(),
			},
		},
		{
			name: "ApprovalPendingEvent",
			payload: &agentic.ApprovalPendingEvent{
				LoopID: nonCanonical, CallID: "call-1", ToolName: "delete_rule",
				RequestedAt: time.Now(),
			},
		},
		{
			name: "ApprovalResponse",
			payload: &agentic.ApprovalResponse{
				LoopID: nonCanonical, CallID: "call-1",
				Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "alice",
				DecidedAt: time.Now(),
			},
		},
	}

	for _, carrier := range carriers {
		t.Run(carrier.name, func(t *testing.T) {
			t.Parallel()
			err := carrier.payload.Validate()
			if err == nil {
				t.Fatalf("%s.Validate accepted a non-canonical loop token", carrier.name)
			}
			if !strings.Contains(err.Error(), nonCanonical) {
				t.Errorf("%s refusal must quote the offending token, got %q", carrier.name, err)
			}
		})
	}
}
