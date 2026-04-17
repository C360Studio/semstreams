package agentictools

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// ApprovalRequiredPrefix is prepended to rejection reasons when a tool requires
// human approval. The agentic-loop detects this prefix and transitions the loop
// to LoopStateAwaitingApproval instead of storing a normal error result.
const ApprovalRequiredPrefix = "approval_required: "

// IsApprovalRequired checks whether a rejection reason indicates the tool
// needs human approval rather than being a genuine error.
func IsApprovalRequired(reason string) bool {
	return strings.HasPrefix(reason, ApprovalRequiredPrefix)
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
