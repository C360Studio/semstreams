package agentic

// Domain and version constants for message type identification.
const (
	Domain        = "agentic"
	SchemaVersion = "v1"
)

// Category constants for message types.
const (
	CategoryTask             = "task"
	CategoryUserMessage      = "user_message"
	CategorySignal           = "signal"
	CategoryUserResponse     = "user_response"
	CategoryRequest          = "request"
	CategoryResponse         = "response"
	CategoryToolCall         = "tool_call"
	CategoryToolResult       = "tool_result"
	CategoryLoopCreated      = "loop_created"
	CategoryLoopCompleted    = "loop_completed"
	CategoryLoopFailed       = "loop_failed"
	CategoryLoopCancelled    = "loop_cancelled"
	CategoryContextEvent     = "context_event"
	CategoryApprovalPending  = "approval_pending"
	CategoryApprovalResponse = "approval_response"
)

// Decision values for ApprovalResponse — what the human-in-the-loop
// approver decided about a tool call gated by approval_required.
const (
	ApprovalDecisionApprove = "approve"
	ApprovalDecisionReject  = "reject"
	ApprovalDecisionModify  = "modify"
)

// Outcome values for loop completion events.
const (
	OutcomeSuccess   = "success"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
	OutcomeTruncated = "truncated"
)

// Response status values from model responses.
const (
	StatusComplete        = "complete"
	StatusToolCall        = "tool_call"
	StatusError           = "error"
	StatusLengthTruncated = "length_truncated"
)

// Finish reason values from model responses (OpenAI-compatible).
const (
	FinishReasonStop      = "stop"
	FinishReasonLength    = "length"
	FinishReasonToolCalls = "tool_calls"
)

// ContextEvent type values.
const (
	ContextEventCompactionStarting = "compaction_starting"
	ContextEventCompactionComplete = "compaction_complete"
	// ContextEventCompactionRetry is emitted when a length-truncated
	// response triggers an inline compaction + within-iteration retry
	// (see beta.21). Carries pre-compaction Utilization and TokensSaved
	// so operators can see the recovery in the loop's history.
	ContextEventCompactionRetry = "compaction_retry"
)

// Role values for agent loops.
const (
	RoleArchitect = "architect"
	RoleEditor    = "editor"
	RoleGeneral   = "general"
	RoleQualifier = "qualifier"
	RoleDeveloper = "developer"
	RoleReviewer  = "reviewer"
)

// Response format type values for AgentRequest.ResponseFormat. See ADR-034.
const (
	// ResponseFormatJSONObject — legacy bare JSON validity (no schema).
	ResponseFormatJSONObject = "json_object"
	// ResponseFormatJSONSchema — strict-mode schema-constrained output (current standard).
	ResponseFormatJSONSchema = "json_schema"
)
