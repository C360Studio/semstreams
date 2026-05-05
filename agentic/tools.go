package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// ErrToolNotFound is the sentinel returned by tool registries when a
// requested tool name has no executor. Callers use errors.Is to detect
// the miss without parsing error strings — the previous string-match
// fallback in agentic-tools/component.go was the source of repeated
// extension friction. Lives here (alongside ToolCall/ToolResult)
// rather than in agentic-tools so consumers in other processor
// packages can check it without importing the executor package.
var ErrToolNotFound = errors.New("tool not found")

// ToolDefinition represents the definition of a tool that can be called
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Validate checks if the ToolDefinition is valid
func (t ToolDefinition) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tool name required")
	}
	if len(t.Parameters) == 0 {
		return fmt.Errorf("tool parameters required")
	}
	return nil
}

// ToolCall represents a request to call a tool
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"` // Domain context, propagated from task
	LoopID    string         `json:"loop_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	// ApprovedBy is set by the loop when re-dispatching a previously
	// gated tool call after receiving an ApprovalResponse. The
	// agentic-tools approval filter recognises a non-empty ApprovedBy
	// as the explicit bypass token (see C5). Empty means the call has
	// not been through human approval — normal filter rules apply.
	ApprovedBy string `json:"approved_by,omitempty"`
}

// Validate checks if the ToolCall is valid
func (t ToolCall) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("tool call id required")
	}
	if t.Name == "" {
		return fmt.Errorf("tool call function name required")
	}
	return nil
}

// Schema implements message.Payload
func (t *ToolCall) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryToolCall, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (t *ToolCall) MarshalJSON() ([]byte, error) {
	type Alias ToolCall
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	type Alias ToolCall
	return json.Unmarshal(data, (*Alias)(t))
}

// ToolErrorKind classifies the source or nature of a tool execution failure.
// It is the structured counterpart to ToolResult.Error and feeds the
// MetadataKeyDecideActionAllowlist is the TaskMessage.Metadata /
// ToolCall.Metadata key under which a closed action vocabulary for
// the decide tool flows from the spawning rule down to the executor.
//
// When set, the decide executor validates its `action` argument
// against the contained []string and rejects non-members with
// ToolErrorInvalidArgs (the message names the valid set so the LLM
// can correct on retry).
//
// Empty/missing leaves decide free-form (back-compat).
//
// Set by rule.executePublishAgent from rule.Action.ActionAllowlist.
// See semteams smoke #7 (project_smoke7_findings.md F2 slice) for
// the planner-wedge incident this enforces against.
const MetadataKeyDecideActionAllowlist = "agent.decide.action_allowlist"

// agent.step.error_category graph predicate for queryable failure analysis.
type ToolErrorKind string

const (
	// ToolErrorTimeout means the tool exceeded its execution deadline
	// (context.DeadlineExceeded observed after the executor returned).
	ToolErrorTimeout ToolErrorKind = "timeout"

	// ToolErrorNotFound means the tool was not registered, the requested
	// resource did not exist, or the caller was not permitted to invoke it
	// via the component allowlist.
	ToolErrorNotFound ToolErrorKind = "not_found"

	// ToolErrorInvalidArgs means tool arguments failed validation
	// (missing required field, wrong type, schema violation).
	ToolErrorInvalidArgs ToolErrorKind = "invalid_args"

	// ToolErrorPermission means an external system refused the request
	// due to authentication or authorization (e.g., HTTP 401/403).
	ToolErrorPermission ToolErrorKind = "permission"

	// ToolErrorNetwork means a transport-level failure occurred
	// (dial error, connection reset, DNS failure).
	ToolErrorNetwork ToolErrorKind = "network"

	// ToolErrorExternal means an external service returned a failure
	// that does not fall into the other categories (5xx, 429 rate limit,
	// operation-specific failure from an upstream API).
	ToolErrorExternal ToolErrorKind = "external"

	// ToolErrorInternal means an executor-internal bug
	// (marshal/unmarshal failure, unexpected nil, invariant violation).
	ToolErrorInternal ToolErrorKind = "internal"

	// ToolErrorUnknown means the failure was not classified. Used as the
	// default when a ToolResult has a non-empty Error but no ErrorKind.
	ToolErrorUnknown ToolErrorKind = "unknown"
)

// ToolResult represents the result of a tool call
type ToolResult struct {
	CallID    string         `json:"call_id"`
	Name      string         `json:"name,omitempty"` // Tool function name (required by Gemini on tool result messages)
	Content   string         `json:"content,omitempty"`
	Error     string         `json:"error,omitempty"`
	ErrorKind ToolErrorKind  `json:"error_kind,omitempty"` // Structured classification of the failure
	Metadata  map[string]any `json:"metadata,omitempty"`
	LoopID    string         `json:"loop_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	StopLoop  bool           `json:"stop_loop,omitempty"` // Signal loop termination; Content becomes the completion result
}

// Validate checks if the ToolResult is valid
func (t ToolResult) Validate() error {
	if t.CallID == "" {
		return fmt.Errorf("tool result call_id required")
	}
	return nil
}

// Schema implements message.Payload
func (t *ToolResult) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryToolResult, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (t *ToolResult) MarshalJSON() ([]byte, error) {
	type Alias ToolResult
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *ToolResult) UnmarshalJSON(data []byte) error {
	type Alias ToolResult
	return json.Unmarshal(data, (*Alias)(t))
}

// ValidateToolsAllowed validates that all tool calls are in the allowed list
func ValidateToolsAllowed(calls []ToolCall, allowed []string) error {
	if len(calls) == 0 {
		return nil
	}

	// Build allowed set for fast lookup
	allowedSet := make(map[string]bool)
	for _, name := range allowed {
		allowedSet[name] = true
	}

	// Check each call
	var disallowed []string
	for _, call := range calls {
		if !allowedSet[call.Name] {
			disallowed = append(disallowed, call.Name)
		}
	}

	if len(disallowed) > 0 {
		return fmt.Errorf("disallowed tools: %s", strings.Join(disallowed, ", "))
	}

	return nil
}
