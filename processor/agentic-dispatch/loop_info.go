package agenticdispatch

import (
	"github.com/c360studio/semstreams/agentic"
	"time"
)

// LoopInfo is an immutable response projection of validated current loop authority.
type LoopInfo struct {
	LoopID string `json:"loop_id"`
	TaskID string `json:"task_id"`
	// Role is the agent role assigned to the loop (e.g., "coordinator",
	// "ops", "research"). Propagated from TaskMessage.Role at creation so
	// operators, UIs, and test harnesses inspecting /loops can filter by
	// role without needing a graph query back to the loop entity.
	Role          string    `json:"role,omitempty"`
	UserID        string    `json:"user_id"`
	ChannelType   string    `json:"channel_type"`
	ChannelID     string    `json:"channel_id"`
	State         string    `json:"state"`
	Iterations    int       `json:"iterations"`
	MaxIterations int       `json:"max_iterations"`
	CreatedAt     time.Time `json:"created_at"`

	// Workflow context (for loops created by workflow commands)
	WorkflowSlug string `json:"workflow_slug,omitempty"` // e.g., "add-user-auth"
	WorkflowStep string `json:"workflow_step,omitempty"` // e.g., "design"

	// Context assembly reference (links to assembled context)
	ContextRequestID string `json:"context_request_id,omitempty"`

	// Domain context propagated from TaskMessage
	Metadata map[string]any `json:"metadata,omitempty"`

	// Completion data (populated when loop completes)
	Outcome     string    `json:"outcome,omitempty"`      // success, failed, cancelled
	Result      string    `json:"result,omitempty"`       // LLM response content
	Error       string    `json:"error,omitempty"`        // Error message on failure
	CompletedAt time.Time `json:"completed_at,omitempty"` // When the loop completed

	// PendingApproval exposes the exact persisted gate for approval callers to echo.
	PendingApproval *PendingApprovalInfo `json:"pending_approval,omitempty"`
}

// PendingApprovalInfo projects the reviewed call; the loop alone enforces its deadline.
type PendingApprovalInfo struct {
	CallID      string         `json:"call_id"`
	ExecutionID string         `json:"execution_id"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	RequestedAt time.Time      `json:"requested_at"`
	TraceID     string         `json:"trace_id,omitempty"`
}

func loopInfoFromEntity(e *agentic.LoopEntity, createdAt time.Time) *LoopInfo {
	return &LoopInfo{
		LoopID: e.ID, TaskID: e.TaskID, Role: e.Role, UserID: e.UserID,
		ChannelType: e.ChannelType, ChannelID: e.ChannelID, State: e.State.String(),
		Iterations: e.Iterations, MaxIterations: e.MaxIterations, CreatedAt: createdAt,
		WorkflowSlug: e.WorkflowSlug, WorkflowStep: e.WorkflowStep, Metadata: e.Metadata,
		Outcome: e.Outcome, Result: e.Result, Error: e.Error, CompletedAt: e.CompletedAt,
		PendingApproval: pendingApprovalInfo(e.PendingApproval),
	}
}

func pendingApprovalInfo(pending *agentic.PendingApprovalState) *PendingApprovalInfo {
	if pending == nil {
		return nil
	}
	return &PendingApprovalInfo{
		CallID: pending.CallID, ExecutionID: pending.ExecutionID, ToolName: pending.ToolName,
		Arguments: pending.Arguments, Reason: pending.Reason, RequestedAt: pending.RequestedAt,
		TraceID: pending.TraceID,
	}
}
