package responses

import (
	"bytes"
	"encoding/json"
)

// InputItem is the discriminated-union element of Request.Input.
// The Type field selects the populated subset of fields. The closed
// set for Phase 1: "message", "function_call", "function_call_output",
// "reasoning". Unknown types decode without error (forward-compat)
// and re-emit verbatim through their tagged fields.
//
// Producer convention: callers construct via the New* helpers
// (NewInputMessage, NewInputFunctionCall, etc.) to avoid setting
// incoherent field combinations.
type InputItem struct {
	// Type is the variant discriminator. Required.
	Type string `json:"type"`

	// ID echoes the provider-assigned identifier for items echoed
	// from a prior response (reasoning, function_call). Optional on
	// items the caller constructs locally.
	ID string `json:"id,omitempty"`

	// Role is set when Type == "message". Values: "user", "system",
	// "developer", "assistant". The translator should emit "developer"
	// for system-prompt-class messages on Responses (system → developer)
	// per ADR-051 open question 2.
	Role string `json:"role,omitempty"`

	// Content is the typed content-parts array on message items.
	Content []ContentPart `json:"content,omitempty"`

	// CallID is the function-call correlation token shared between
	// function_call (echo) and function_call_output items. Set on
	// both types.
	CallID string `json:"call_id,omitempty"`

	// Name is the function name on function_call items.
	Name string `json:"name,omitempty"`

	// Arguments is the JSON-encoded arguments string on function_call
	// items. Wire-shape mirrors ChatCompletion's
	// tool_calls[].function.arguments — opaque string holding a JSON
	// object.
	Arguments string `json:"arguments,omitempty"`

	// Output is the tool-result body on function_call_output items.
	// Opaque string — typically a JSON-encoded object, but the API
	// accepts any string the caller wants the model to consume.
	Output string `json:"output,omitempty"`

	// Summary is the reasoning summary on reasoning items. Carried
	// as typed parts (mirror of the response shape).
	Summary []SummaryPart `json:"summary,omitempty"`

	// EncryptedContent is the opaque blob echoed on reasoning items
	// in stateless mode. Treated as bytes; do not parse.
	EncryptedContent string `json:"encrypted_content,omitempty"`

	// Status is optionally present on items echoed from a prior
	// response. Possible values include "completed", "in_progress",
	// "incomplete". Producers leave empty on items they construct.
	Status string `json:"status,omitempty"`
}

// MarshalJSON ensures the `summary` field is always present on
// reasoning input items even when the local slice is nil or empty.
// The OpenAI Responses API rejects echoed reasoning items without
// a summary field with HTTP 400 missing_required_parameter, even
// though `[]` is acceptable. Caught by the ADR-051 PR 4 live
// reasoning-echo test before tag.
//
// Non-reasoning items marshal through the default path (omitempty
// honored on Summary).
func (i InputItem) MarshalJSON() ([]byte, error) {
	type alias InputItem
	if i.Type != ItemTypeReasoning {
		return json.Marshal(alias(i))
	}
	b, err := json.Marshal(alias(i))
	if err != nil {
		return nil, err
	}
	if bytes.Contains(b, []byte(`"summary"`)) {
		return b, nil
	}
	// Inject `"summary":[]` before the closing brace.
	idx := bytes.LastIndexByte(b, '}')
	if idx < 0 {
		return b, nil
	}
	var sep []byte
	if idx > 1 {
		sep = []byte(`,"summary":[]`)
	} else {
		sep = []byte(`"summary":[]`)
	}
	out := make([]byte, 0, len(b)+len(sep))
	out = append(out, b[:idx]...)
	out = append(out, sep...)
	out = append(out, b[idx:]...)
	return out, nil
}

// OutputItem is the discriminated-union element of Response.Output.
// Variant set parallels InputItem with output-side extensions:
// "message" assistant replies, "function_call" tool calls,
// "reasoning" emitted blobs.
type OutputItem struct {
	// Type is the variant discriminator. Required.
	Type string `json:"type"`

	// ID is the provider-assigned identifier. Required on all output
	// items.
	ID string `json:"id"`

	// Status reports the item's lifecycle state. Common values:
	// "completed", "in_progress", "incomplete".
	Status string `json:"status,omitempty"`

	// Role applies to message items. "assistant" in practice.
	Role string `json:"role,omitempty"`

	// Content is the typed content-parts array on message items.
	Content []ContentPart `json:"content,omitempty"`

	// CallID is the function-call correlation token. Set on
	// function_call items.
	CallID string `json:"call_id,omitempty"`

	// Name is the function name on function_call items.
	Name string `json:"name,omitempty"`

	// Arguments is the JSON-encoded arguments string on function_call
	// items.
	Arguments string `json:"arguments,omitempty"`

	// Summary is the reasoning summary on reasoning items.
	Summary []SummaryPart `json:"summary,omitempty"`

	// EncryptedContent is the opaque reasoning blob the API emits in
	// stateless mode. The caller must echo it back verbatim on the
	// next turn's InputItem to maintain reasoning continuity.
	EncryptedContent string `json:"encrypted_content,omitempty"`
}

// ContentPart is a typed content element inside a message item.
// Variants:
//   - input_text: user-supplied text on InputItem messages
//   - output_text: assistant text on OutputItem messages
//   - refusal: assistant refusal text
//
// Input image/file/audio parts are out of scope for Phase 1.
type ContentPart struct {
	// Type is one of "input_text", "output_text", "refusal".
	Type string `json:"type"`

	// Text carries the body for text-class parts.
	Text string `json:"text,omitempty"`

	// Refusal carries the refusal body when Type == "refusal".
	Refusal string `json:"refusal,omitempty"`

	// Annotations are inline citations/URL refs the model may attach
	// to output_text. Carried as RawMessage; this package does not
	// model the shape, which has evolved across SDK versions.
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// SummaryPart is one segment of a reasoning item's Summary array.
// Today the only documented variant is type:"summary_text"; the
// field set is open for future additions.
type SummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
