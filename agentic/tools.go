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

	// Strict enables OpenAI's strict-mode tool calling: the model is
	// constrained to emit tool_calls[].function.arguments that conform to
	// Parameters. Symmetric to ResponseFormat.Strict — same provider table
	// applies (see ADR-034): honored on OpenAI proper / vLLM / OpenRouter /
	// sparky / any OpenAI-compat runtime under provider:"openai"; silently
	// ignored on Anthropic and Gemini OpenAI-compat (adapters clear it +
	// Warn). Best-effort on Ollama /v1 (model-dependent — gemma3 ignores
	// per ADR-034 §gh#10001).
	//
	// Requires Parameters to satisfy OpenAI's strict-mode subset:
	// additionalProperties:false at every object level, every property
	// listed in required, no $ref/anyOf at the root, max nesting 5. A
	// non-conforming schema returns 400 from the upstream — caller bug,
	// not a framework concern.
	Strict bool `json:"strict,omitempty"`
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
// Belt-and-suspenders for persona prose: the persona enumerates the
// vocabulary in the LLM's system prompt; this allowlist enforces it
// structurally on the wire.
const MetadataKeyDecideActionAllowlist = "agent.decide.action_allowlist"

// MetadataKeyRelatedLoops is the TaskMessage.Metadata /
// ToolCall.Metadata key under which cross-arc loop-ID lineage flows
// from a spawning rule down to the spawned loop and into its tool
// calls. The value is a map[string]string keyed by role name (or any
// product-specific lineage label) where the value is the related
// loop ID.
//
// Use case: a downstream role needs to read_loop_result against an
// upstream loop without the loop ID being baked into the spawn
// prompt. Architect needing the researcher's loop ID for harness
// selection (semteams smoke #8 run-2 wedge cause); challenger
// cross-grounding back to planner; ops-agent / ADR-033 chain_id
// stability.
//
// String-to-string only by design. Non-string values are out of
// scope: if a future case needs structured data, it earns a
// dedicated typed field (the Tools / ToolChoice / Timeout
// precedent), not a generalized escape hatch through this map.
//
// Empty/missing leaves no lineage threaded (back-compat).
//
// Set by rule.executePublishAgent from rule.Action.RelatedLoops.
// JSON round-trip note: like ActionAllowlist, the value comes back
// from BaseMessage decode as map[string]any (with each value still
// a Go string) — readers coerce on access.
const MetadataKeyRelatedLoops = "agent.related_loops"

// MetadataKeyGoogleThoughtSignature is the ToolCall.Metadata key under
// which the per-tool_call thought signature emitted by Gemini 3.x
// preview models flows across loop turns.
//
// Gemini 3.x emits the signature at
// `response.choices[].message.tool_calls[].extra_content.google.thought_signature`
// and requires the same signature echoed back on the assistant
// tool_calls of the next request (only the first tool_call per step
// needs the signature; subsequent calls in the same step inherit).
// agentic-model's wire backend extracts the signature during
// convertWireResponse and writes it under this key; on the next
// request, GeminiAdapter.NormalizeMessages reconstructs the
// `extra_content.google.thought_signature` shape on the outgoing
// wire ToolCall.
//
// Value type: string. Non-Gemini providers ignore the carrier.
const MetadataKeyGoogleThoughtSignature = "google_thought_signature"

// LineageTriplePrefix is the predicate prefix for cross-arc loop-ID
// lineage triples stamped on a spawned loop's entity at loop-creation
// time. Each entry in TaskMessage.Metadata[MetadataKeyRelatedLoops]
// becomes a triple of the form:
//
//	subject:   <spawned loop entity ID>
//	predicate: lineage.<roleKey>          // e.g. lineage.researcher
//	object:    <upstream loop ID string>
//
// Downstream rules that fire on the spawned entity read these via the
// existing $entity.triple.<predicate> substitution, e.g.
// $entity.triple.lineage.researcher resolves to the upstream loop ID
// without any new substitution-token, tool, or persona-driven echo
// forwarding.
//
// Stable namespace: ops-agent (ADR-027) and the operating-curve
// observability primitives (ADR-033) aggregate cross-arc lineage by
// scanning predicates with this prefix. Codifying as a public constant
// keeps producers and consumers aligned without string-literal drift.
const LineageTriplePrefix = "lineage."

// LineageTriplePredicate returns the canonical predicate for a
// RelatedLoops role key. Centralises the prefix join so producers
// (rule.executePublishAgent / agentic-loop loop-creation path) and
// consumers (rule authors using $entity.triple.lineage.<key>,
// ops-agent aggregations) cannot drift on the format.
func LineageTriplePredicate(roleKey string) string {
	return LineageTriplePrefix + roleKey
}

// ToolErrorKind classifies the source or nature of a tool execution failure.
// It is the structured counterpart to ToolResult.Error and feeds the
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
