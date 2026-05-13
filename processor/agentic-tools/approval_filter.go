package agentictools

import (
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

// ApprovalRequiredPrefix is an alias for agentic.ApprovalRequiredPrefix
// retained so existing call sites in this package compile unchanged.
// The canonical declaration lives in the agentic package because both
// agentic-tools (filter producer) and agentic-loop (filter consumer)
// need to reference it; keeping it here would force agentic-loop to
// import a sibling component.
const ApprovalRequiredPrefix = agentic.ApprovalRequiredPrefix

// IsApprovalRequired delegates to agentic.IsApprovalRequired so this
// package's existing test suite keeps working without churn.
func IsApprovalRequired(reason string) bool {
	return agentic.IsApprovalRequired(reason)
}

// ApprovalFilter gates tool calls that require human approval before
// execution. It is internal to agentic-tools — the loop in this
// component's dispatch path consults it before publishing to NATS for
// actual execution. Calls for tools in the configured set are
// rejected with an ApprovalRequiredPrefix reason so the higher-level
// loop can transition to awaiting_approval.
//
// (Beta.70 retired the cross-component `agentic.ToolCallFilter`
// interface. ApprovalFilter no longer implements it; the
// agentic-tools-internal consumer below works against the concrete
// type directly.)
type ApprovalFilter struct {
	approvalSet map[string]bool
}

// NewApprovalFilter creates a filter from the configured list of tool names
// that require approval.
func NewApprovalFilter(approvalRequired []string) *ApprovalFilter {
	set := make(map[string]bool, len(approvalRequired))
	for _, name := range approvalRequired {
		set[name] = true
	}
	return &ApprovalFilter{approvalSet: set}
}

// ApprovalFilterResult is the outcome of evaluating a batch of tool
// calls against the approval gate. Approved calls flow through to
// execution; Rejected calls carry a reason for the awaiting_approval
// path.
type ApprovalFilterResult struct {
	Approved []agentic.ToolCall
	Rejected []ApprovalRejection
}

// ApprovalRejection pairs a rejected call with its approval-required
// reason. Internal to agentic-tools.
type ApprovalRejection struct {
	Call   agentic.ToolCall
	Reason string
}

// FilterToolCalls checks each call against the configured approval
// list. A call carrying a non-empty ApprovedBy is the explicit bypass
// token: the loop only sets it when re-dispatching after a human
// ApprovalResponse arrived, so we trust it and pass the call through
// regardless of the gating list. Empty ApprovedBy means the call has
// not been through approval and the gating list applies normally.
func (f *ApprovalFilter) FilterToolCalls(_ string, calls []agentic.ToolCall) ApprovalFilterResult {
	var result ApprovalFilterResult

	for _, call := range calls {
		if !f.approvalSet[call.Name] {
			result.Approved = append(result.Approved, call)
			continue
		}
		if call.ApprovedBy != "" {
			// Loop-injected bypass after an ApprovalResponse —
			// approver identity flows through into trajectory audit
			// via the dispatched ToolCall envelope.
			result.Approved = append(result.Approved, call)
			continue
		}
		result.Rejected = append(result.Rejected, ApprovalRejection{
			Call:   call,
			Reason: fmt.Sprintf("%sTool '%s' requires human approval before execution", ApprovalRequiredPrefix, call.Name),
		})
	}

	return result
}
