package agentic

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
)

// ApprovalPendingEvent is published by agentic-loop when a tool call
// is gated by Config.ApprovalRequired and the loop has transitioned to
// LoopStateAwaitingApproval. Product-layer approval UIs subscribe to
// agent.approval_pending.<loop_id> and surface the request for human
// review. The corresponding response is published as
// agent.approval_response.<loop_id> with an ApprovalResponse payload.
type ApprovalPendingEvent struct {
	LoopID      string         `json:"loop_id"`
	CallID      string         `json:"call_id"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Reason      string         `json:"reason"` // Original "approval_required: ..." rejection reason
	RequestedAt time.Time      `json:"requested_at"`
	// Timeout is the duration after which the loop will auto-reject if
	// no response arrives. Zero means wait indefinitely.
	Timeout time.Duration `json:"timeout,omitempty"`
	// TraceID propagates from the original ToolCall so consumers can
	// correlate the approval prompt with the originating loop trace.
	TraceID string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (e *ApprovalPendingEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryApprovalPending, Version: SchemaVersion}
}

// Validate implements message.Payload.
func (e *ApprovalPendingEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.CallID == "" {
		return fmt.Errorf("call_id required")
	}
	if e.ToolName == "" {
		return fmt.Errorf("tool_name required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (e *ApprovalPendingEvent) MarshalJSON() ([]byte, error) {
	type Alias ApprovalPendingEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler.
func (e *ApprovalPendingEvent) UnmarshalJSON(data []byte) error {
	type Alias ApprovalPendingEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// ApprovalResponse is published by a product-layer approval UI in
// reply to an ApprovalPendingEvent. The agentic-loop subscribes to
// agent.approval_response.<loop_id>; the matching loop transitions
// out of LoopStateAwaitingApproval and either re-dispatches the tool
// (Decision == approve | modify) or synthesizes a rejection result
// for the LLM (Decision == reject).
type ApprovalResponse struct {
	LoopID   string `json:"loop_id"`
	CallID   string `json:"call_id"`
	Decision string `json:"decision"` // ApprovalDecisionApprove | ApprovalDecisionReject | ApprovalDecisionModify
	// ModifiedArguments replaces the original tool-call arguments when
	// Decision == ApprovalDecisionModify. Ignored for approve/reject.
	ModifiedArguments map[string]any `json:"modified_arguments,omitempty"`
	// Reason is an optional human-readable explanation for the
	// decision. Carried into the audit trail and into the synthesized
	// rejection result when Decision == reject.
	Reason string `json:"reason,omitempty"`
	// ApprovedBy identifies the approver. Required for approve and
	// modify decisions so the audit trail knows who said yes; optional
	// for reject. Propagated to ToolCall.ApprovedBy on re-dispatch so
	// the agentic-tools approval filter recognises the bypass.
	ApprovedBy string    `json:"approved_by,omitempty"`
	DecidedAt  time.Time `json:"decided_at"`
}

// Schema implements message.Payload.
func (r *ApprovalResponse) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryApprovalResponse, Version: SchemaVersion}
}

// Validate implements message.Payload.
func (r *ApprovalResponse) Validate() error {
	if r.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if r.CallID == "" {
		return fmt.Errorf("call_id required")
	}
	switch r.Decision {
	case ApprovalDecisionApprove, ApprovalDecisionModify:
		if r.ApprovedBy == "" {
			return fmt.Errorf("approved_by required for decision=%q", r.Decision)
		}
	case ApprovalDecisionReject:
		// approved_by is optional on reject; "rejected by anonymous"
		// is permissible (e.g., timeout-driven auto-reject).
	default:
		return fmt.Errorf("decision must be one of %q, %q, %q (got %q)",
			ApprovalDecisionApprove, ApprovalDecisionReject, ApprovalDecisionModify, r.Decision)
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ApprovalResponse) MarshalJSON() ([]byte, error) {
	type Alias ApprovalResponse
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ApprovalResponse) UnmarshalJSON(data []byte) error {
	type Alias ApprovalResponse
	return json.Unmarshal(data, (*Alias)(r))
}
