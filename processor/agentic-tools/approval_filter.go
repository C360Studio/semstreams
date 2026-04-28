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

// ApprovalFilter implements agentic.ToolCallFilter. It checks each tool call
// against a configured list of tool names that require human approval. If a
// tool is in the list, the call is rejected with an ApprovalRequiredPrefix
// reason so the loop can transition to awaiting_approval.
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

var _ agentic.ToolCallFilter = (*ApprovalFilter)(nil)

// FilterToolCalls checks each call against the configured approval list.
func (f *ApprovalFilter) FilterToolCalls(_ string, calls []agentic.ToolCall) (agentic.ToolCallFilterResult, error) {
	var result agentic.ToolCallFilterResult

	for _, call := range calls {
		if f.approvalSet[call.Name] {
			result.Rejected = append(result.Rejected, agentic.ToolCallRejection{
				Call:   call,
				Reason: fmt.Sprintf("%sTool '%s' requires human approval before execution", ApprovalRequiredPrefix, call.Name),
			})
		} else {
			result.Approved = append(result.Approved, call)
		}
	}

	return result, nil
}
