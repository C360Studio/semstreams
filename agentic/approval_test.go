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
				LoopID:      "loop-001",
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
				LoopID:   "loop-001",
				ToolName: "delete_rule",
			},
			wantErr: "call_id required",
		},
		{
			name: "missing tool_name fails",
			event: agentic.ApprovalPendingEvent{
				LoopID: "loop-001",
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
				LoopID:     "loop-001",
				CallID:     "call-001",
				Decision:   agentic.ApprovalDecisionApprove,
				ApprovedBy: "alice@example.com",
				DecidedAt:  time.Now(),
			},
		},
		{
			name: "reject without approver passes",
			response: agentic.ApprovalResponse{
				LoopID:    "loop-001",
				CallID:    "call-001",
				Decision:  agentic.ApprovalDecisionReject,
				Reason:    "policy violation",
				DecidedAt: time.Now(),
			},
		},
		{
			name: "modify with approver and arguments passes",
			response: agentic.ApprovalResponse{
				LoopID:            "loop-001",
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
				LoopID:   "loop-001",
				CallID:   "call-001",
				Decision: agentic.ApprovalDecisionApprove,
			},
			wantErr: "approved_by required",
		},
		{
			name: "modify without approver fails",
			response: agentic.ApprovalResponse{
				LoopID:   "loop-001",
				CallID:   "call-001",
				Decision: agentic.ApprovalDecisionModify,
			},
			wantErr: "approved_by required",
		},
		{
			name: "unknown decision fails",
			response: agentic.ApprovalResponse{
				LoopID:   "loop-001",
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
				LoopID:   "loop-001",
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
		LoopID:      "loop-001",
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
		LoopID:            "loop-001",
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
