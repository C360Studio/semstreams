package agentic

// Domain and version constants for message type identification.
const (
	Domain        = "agentic"
	SchemaVersion = "v1"
)

// Category constants for message types.
const (
	CategoryTask          = "task"
	CategoryUserMessage   = "user_message"
	CategorySignal        = "signal"
	CategoryUserResponse  = "user_response"
	CategoryRequest       = "request"
	CategoryResponse      = "response"
	CategoryToolCall      = "tool_call"
	CategoryToolResult    = "tool_result"
	CategoryLoopCreated   = "loop_created"
	CategoryLoopCompleted = "loop_completed"
	CategoryLoopFailed    = "loop_failed"
	CategoryLoopCancelled = "loop_cancelled"
	CategoryContextEvent  = "context_event"
	CategorySignalMessage = "signal_message"
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
