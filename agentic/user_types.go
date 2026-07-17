package agentic

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/types"
)

// Signal type constants for user control signals
const (
	SignalCancel   = "cancel"   // Stop execution immediately
	SignalPause    = "pause"    // Pause at next checkpoint
	SignalResume   = "resume"   // Continue paused loop
	SignalApprove  = "approve"  // Approve pending result
	SignalReject   = "reject"   // Reject with optional reason
	SignalFeedback = "feedback" // Add feedback without decision
	SignalRetry    = "retry"    // Retry failed loop
)

// UserMessage represents normalized input from any channel (CLI, Slack, Discord, web)
type UserMessage struct {
	// Identity
	MessageID   string `json:"message_id"`
	ChannelType string `json:"channel_type"` // cli, slack, discord, web
	ChannelID   string `json:"channel_id"`   // specific conversation/channel
	UserID      string `json:"user_id"`

	// Content
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`

	// Context
	ReplyTo          string            `json:"reply_to,omitempty"`           // loop_id if continuing
	ThreadID         string            `json:"thread_id,omitempty"`          // for threaded channels
	Metadata         map[string]string `json:"metadata,omitempty"`           // channel-specific
	ContextRequestID string            `json:"context_request_id,omitempty"` // links to assembled context

	// Resumable-reply context (gh#256). These are distinct from ReplyTo:
	// ReplyTo routes the message to a loop to continue; the two below let a
	// reply re-enter and resume a *paused run*.
	//
	// RunID is the bare run anchor the reply should re-attach to. A client
	// resuming a paused run (ADR-053) echoes the RunID it held from the pause
	// state so the resumed loop carries agent.loop.run / agent.run.entity-id even
	// when the prior loop entity was evicted during the pause. Empty for
	// non-run submissions.
	RunID string `json:"run_id,omitempty"`
	// InReplyTo marks this message as a reply to a specific loop's question
	// (e.g. an ask_user clarification), stamped onto the resumed loop as the
	// agent.loop.reply_to triple so a rule can fire on it. Deliberately
	// separate from ReplyTo so ordinary continuations are NOT marked as
	// replies. Empty for non-reply submissions.
	InReplyTo string `json:"in_reply_to,omitempty"`

	// Timing
	Timestamp time.Time `json:"timestamp"`
}

// Validate checks if the UserMessage is valid
func (m UserMessage) Validate() error {
	if m.MessageID == "" {
		return fmt.Errorf("message_id required")
	}
	if m.ChannelType == "" {
		return fmt.Errorf("channel_type required")
	}
	if m.ChannelID == "" {
		return fmt.Errorf("channel_id required")
	}
	if m.UserID == "" {
		return fmt.Errorf("user_id required")
	}
	if m.Content == "" && len(m.Attachments) == 0 {
		return fmt.Errorf("either content or attachments must be present")
	}
	return nil
}

// Schema implements message.Payload
func (m *UserMessage) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryUserMessage, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (m *UserMessage) MarshalJSON() ([]byte, error) {
	type Alias UserMessage
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler
func (m *UserMessage) UnmarshalJSON(data []byte) error {
	type Alias UserMessage
	return json.Unmarshal(data, (*Alias)(m))
}

// Attachment represents a file or other media attached to a message
type Attachment struct {
	Type     string `json:"type"`              // file, image, code, url
	Name     string `json:"name"`              // filename or title
	URL      string `json:"url,omitempty"`     // URL to fetch content
	Content  string `json:"content,omitempty"` // inline content if small
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// UserSignal represents a control signal from user to affect loop execution
type UserSignal struct {
	SignalID    string    `json:"signal_id"`
	Type        string    `json:"type"` // cancel, pause, resume, approve, reject, feedback, retry
	LoopID      string    `json:"loop_id"`
	UserID      string    `json:"user_id"`
	ChannelType string    `json:"channel_type"`
	ChannelID   string    `json:"channel_id"`
	Payload     any       `json:"payload,omitempty"` // signal-specific data (e.g., rejection reason)
	Timestamp   time.Time `json:"timestamp"`
}

// Validate checks if the UserSignal is valid
func (s UserSignal) Validate() error {
	if s.SignalID == "" {
		return fmt.Errorf("signal_id required")
	}
	if s.Type == "" {
		return fmt.Errorf("type required")
	}
	if !isValidSignalType(s.Type) {
		return fmt.Errorf("type must be one of: cancel, pause, resume, approve, reject, feedback, retry")
	}
	if s.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if s.UserID == "" {
		return fmt.Errorf("user_id required")
	}
	return nil
}

// Schema implements message.Payload
func (s *UserSignal) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategorySignal, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (s *UserSignal) MarshalJSON() ([]byte, error) {
	type Alias UserSignal
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler
func (s *UserSignal) UnmarshalJSON(data []byte) error {
	type Alias UserSignal
	return json.Unmarshal(data, (*Alias)(s))
}

func isValidSignalType(t string) bool {
	switch t {
	case SignalCancel, SignalPause, SignalResume, SignalApprove, SignalReject, SignalFeedback, SignalRetry:
		return true
	default:
		return false
	}
}

// Response type constants
const (
	ResponseTypeText   = "text"   // Plain text response
	ResponseTypeStatus = "status" // Status update
	ResponseTypeResult = "result" // Final result
	ResponseTypeError  = "error"  // Error message
	ResponseTypePrompt = "prompt" // Awaiting user input (approval, etc.)
	ResponseTypeStream = "stream" // Streaming partial content
)

// UserResponse is sent back to users via their channel
type UserResponse struct {
	ResponseID  string `json:"response_id"`
	ChannelType string `json:"channel_type"`
	ChannelID   string `json:"channel_id"`
	UserID      string `json:"user_id"` // who to respond to

	// What we're responding to
	InReplyTo string `json:"in_reply_to,omitempty"` // message_id or loop_id
	ThreadID  string `json:"thread_id,omitempty"`

	// Content
	Type    string `json:"type"` // text, status, result, error, prompt, stream
	Content string `json:"content"`

	// Rich content (optional)
	Blocks  []ResponseBlock  `json:"blocks,omitempty"`
	Actions []ResponseAction `json:"actions,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// Validate checks if the UserResponse is valid
func (r UserResponse) Validate() error {
	if r.ResponseID == "" {
		return fmt.Errorf("response_id required")
	}
	if r.ChannelType == "" {
		return fmt.Errorf("channel_type required")
	}
	if r.ChannelID == "" {
		return fmt.Errorf("channel_id required")
	}
	if r.Type == "" {
		return fmt.Errorf("type required")
	}
	if !isValidResponseType(r.Type) {
		return fmt.Errorf("type must be one of: text, status, result, error, prompt, stream")
	}
	return nil
}

// Schema implements message.Payload
func (r *UserResponse) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryUserResponse, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (r *UserResponse) MarshalJSON() ([]byte, error) {
	type Alias UserResponse
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler
func (r *UserResponse) UnmarshalJSON(data []byte) error {
	type Alias UserResponse
	return json.Unmarshal(data, (*Alias)(r))
}

func isValidResponseType(t string) bool {
	switch t {
	case ResponseTypeText, ResponseTypeStatus, ResponseTypeResult, ResponseTypeError, ResponseTypePrompt, ResponseTypeStream:
		return true
	default:
		return false
	}
}

// ResponseBlock represents a block of content in a rich response
type ResponseBlock struct {
	Type    string `json:"type"` // text, code, diff, file, progress
	Content string `json:"content"`
	Lang    string `json:"lang,omitempty"` // for code blocks
}

// ResponseAction represents an interactive action in a response
type ResponseAction struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // button, reaction
	Label  string `json:"label"`
	Signal string `json:"signal"` // signal to send if clicked
	Style  string `json:"style"`  // primary, danger, secondary
}

// TaskMessage represents a task to be executed by an agentic loop
type TaskMessage struct {
	LoopID string `json:"loop_id,omitempty"` // loop to continue, or empty for new
	TaskID string `json:"task_id"`
	Role   string `json:"role"`
	Model  string `json:"model"`
	Prompt string `json:"prompt"`

	// Workflow context (optional, set by workflow commands)
	WorkflowSlug string `json:"workflow_slug,omitempty"` // e.g., "add-user-auth"
	WorkflowStep string `json:"workflow_step,omitempty"` // e.g., "design"

	// User routing info (optional, for error notifications)
	ChannelType string `json:"channel_type,omitempty"` // e.g., "http", "cli", "slack"
	ChannelID   string `json:"channel_id,omitempty"`   // session/channel identifier
	UserID      string `json:"user_id,omitempty"`      // user who initiated the request

	// Multi-agent hierarchy (optional, for parallel/nested agents)
	ParentLoopID string `json:"parent_loop_id,omitempty"` // Parent loop ID for nested agents
	// RunID is the 6-part-derived run anchor: the run loop-id this loop belongs to.
	// Empty for loops not in a run. Inherited at spawn (ADR-053 D7).
	RunID    string `json:"run_id,omitempty"`
	Depth    int    `json:"depth,omitempty"`     // Current depth in agent tree (0 = root)
	MaxDepth int    `json:"max_depth,omitempty"` // Maximum allowed depth

	// MaxIterations is an optional per-spawn iteration budget (gh#528). Nil
	// means "use the component default" (agentic-loop's Config.MaxIterations).
	// A non-nil value narrows the spawned loop's budget: agentic-loop computes
	// the effective ceiling as min(*MaxIterations, component ceiling) at loop
	// creation — a spawn may narrow its budget, never widen it past the
	// operator-configured ceiling. Validate rejects a non-nil value below 1.
	// The publish_agent rule action exposes this as loop_max_iterations
	// (distinct from the action's own firing-cap max_iterations field).
	MaxIterations *int `json:"max_iterations,omitempty"`

	// InReplyTo is the bare loop-id this task is a reply to (gh#256). When
	// set, agentic-loop stamps an agent.loop.reply_to triple (a 6-part loop
	// entity reference, mirroring agent.loop.parent) on the spawned loop so a
	// rule can detect a reply via $entity.triple.agent.loop.reply_to. Empty
	// for non-reply tasks. Distinct from ParentLoopID (tree ancestry) — a
	// reply re-enters a paused run rather than nesting under a parent.
	InReplyTo string `json:"in_reply_to,omitempty"`

	// Pre-constructed context (optional, skips discovery if present)
	// When set, the agent loop uses this context directly instead of hydrating
	Context *types.ConstructedContext `json:"context,omitempty"`

	// Context assembly reference (links to assembled context)
	ContextRequestID string `json:"context_request_id,omitempty"`

	// Tools is a per-task tool override. The spawner sets this to scope
	// which tools the agent may call; the loop consumes the field with
	// nil-vs-empty semantics:
	//  - nil       → no override, loop falls back to global discovery.
	//  - non-nil   → explicit allowlist. Empty slice means "no tools".
	// `omitempty` is deliberately omitted so an explicit empty slice
	// round-trips as `"tools": []` and the receiver can distinguish it
	// from an absent field.
	Tools []ToolDefinition `json:"tools"`

	// ToolChoice controls how the model selects tools for this task.
	// Nil means "auto" (model decides). Cached for all iterations in the loop.
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`

	// Domain context propagated to all tool calls in this loop
	Metadata map[string]any `json:"metadata,omitempty"`

	// Timeout caps LLM calls issued for this task. Go duration string
	// (e.g. "30s"). Empty means fall through to endpoint, capability, or
	// component-level timeout. Highest precedence when set.
	Timeout string `json:"timeout,omitempty"`

	// ResponseFormat constrains the model's output to a JSON object or
	// JSON-schema-conformant JSON for this task. ADR-034. The agentic-loop
	// caches it on initial build and threads it onto every AgentRequest in
	// the loop. Nil means tool-calling behaviour is unchanged. Set this
	// from a rule.Action (publish_agent path) or from a dispatcher when
	// the task needs structured output.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ConstructedContext is an alias for types.ConstructedContext.
// The canonical type is defined in pkg/types/context.go.
type ConstructedContext = types.ConstructedContext

// ContextSource is an alias for types.ContextSource.
// The canonical type is defined in pkg/types/context.go.
type ContextSource = types.ContextSource

// GraphContextSpec is an alias for types.GraphContextSpec.
// The canonical type is defined in pkg/types/context.go.
type GraphContextSpec = types.GraphContextSpec

// Validate checks if the TaskMessage is valid
func (t TaskMessage) Validate() error {
	if t.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	if t.Role == "" {
		return fmt.Errorf("role required")
	}
	if t.Model == "" {
		return fmt.Errorf("model required")
	}
	if t.Prompt == "" {
		return fmt.Errorf("prompt required")
	}
	if t.MaxIterations != nil && *t.MaxIterations < 1 {
		return fmt.Errorf("max_iterations must be >= 1, got %d", *t.MaxIterations)
	}
	if t.ToolChoice != nil {
		if err := t.ToolChoice.Validate(); err != nil {
			return err
		}
	}
	if t.ResponseFormat != nil {
		if err := t.ResponseFormat.Validate(); err != nil {
			return err
		}
	}
	if raw, ok := t.Metadata[MetadataKeyRelatedLoops]; ok {
		if err := validateRelatedLoopsMetadata(raw); err != nil {
			return fmt.Errorf("metadata %q: %w", MetadataKeyRelatedLoops, err)
		}
	}
	return nil
}

func validateRelatedLoopsMetadata(raw any) error {
	values := make(map[string]any)
	switch related := raw.(type) {
	case map[string]any:
		values = related
	case map[string]string:
		for key, value := range related {
			values[key] = value
		}
	default:
		return fmt.Errorf("must be an object of role keys to loop ID strings, got %T", raw)
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := LineageTriplePredicate(key); err != nil {
			return fmt.Errorf("role key %q: %w", key, err)
		}
		loopID, ok := values[key].(string)
		if !ok {
			return fmt.Errorf("role key %q loop ID must be a string, got %T", key, values[key])
		}
		if loopID == "" {
			return fmt.Errorf("role key %q loop ID must not be empty", key)
		}
	}
	return nil
}

// Schema implements message.Payload
func (t *TaskMessage) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryTask, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
	type Alias TaskMessage
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *TaskMessage) UnmarshalJSON(data []byte) error {
	type Alias TaskMessage
	return json.Unmarshal(data, (*Alias)(t))
}
