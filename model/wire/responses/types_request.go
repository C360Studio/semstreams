package responses

import (
	"encoding/json"
)

// Request is the POST /v1/responses request body. Phase 1 scope:
// non-streaming, stateless (store=false), function-calling and
// reasoning. Hosted tools and image inputs are out of scope.
//
// The Input field holds the heterogeneous typed array that replaces
// ChatCompletion's messages array. See InputItem for the variant set.
type Request struct {
	// Model is the OpenAI model identifier, e.g. "gpt-5.5", "o4-mini",
	// "codex-mini-latest".
	Model string `json:"model"`

	// Input is the heterogeneous input items array. Each element is a
	// typed item (message, function_call echo, function_call_output,
	// reasoning echo). See InputItem.
	Input []InputItem `json:"input"`

	// Instructions optionally pins a system-prompt-class instruction.
	// Equivalent in role to ChatCompletion's "system" message but
	// passed as a top-level field. Recommended for stable persona
	// prose that doesn't belong inline with conversational input.
	Instructions string `json:"instructions,omitempty"`

	// Tools enumerates the function tools available to the model.
	// Hosted tools (file_search, web_search_preview, etc.) are out of
	// scope for Phase 1; future addition extends the Tool type with a
	// discriminator.
	Tools []Tool `json:"tools,omitempty"`

	// ToolChoice controls tool-selection behavior. Accepts the same
	// string values as ChatCompletion ("auto", "required", "none")
	// or an object {"type":"function","name":"..."} forcing a
	// specific tool. json.RawMessage carries the variant.
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`

	// Reasoning controls reasoning-effort and summary-emission for
	// the o-series and GPT-5.5-class models. The Responses endpoint
	// is the only endpoint where reasoning_effort combines with
	// tool_choice on these model classes.
	Reasoning *ReasoningParams `json:"reasoning,omitempty"`

	// Text controls the output text format. The Responses API uses
	// text.format here rather than the ChatCompletion top-level
	// response_format. The two shapes are not 1:1; the adapter layer
	// translates.
	Text *TextParams `json:"text,omitempty"`

	// Temperature, TopP, MaxOutputTokens carry the standard sampling
	// controls. Names match the Responses API ("max_output_tokens",
	// not "max_tokens" — the two endpoints differ).
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`

	// Store enables OpenAI-side conversation state persistence.
	// Stateless mode (store=false) is what this client uses per
	// ADR-051 D2 — caller owns full history and echoes reasoning
	// items per turn. The field is a pointer so the zero value is
	// distinguishable from explicit-false (the API default is true).
	Store *bool `json:"store,omitempty"`

	// PreviousResponseID threads server-side state when Store=true.
	// Unused in stateless mode; carried here for completeness.
	PreviousResponseID string `json:"previous_response_id,omitempty"`

	// Stream selects SSE streaming mode. Phase 1 client only supports
	// false; streaming lands in Phase 2.
	Stream bool `json:"stream,omitempty"`

	// User is the optional end-user identifier for abuse monitoring,
	// per OpenAI's documented usage.
	User string `json:"user,omitempty"`

	// Metadata is the optional operator-provided key/value pairs the
	// API echoes back on the response. Capped at 16 keys, 64-char
	// keys, 512-char values per OpenAI's docs.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Include opts into response fields the API otherwise omits.
	// Most consequential for our path: "reasoning.encrypted_content"
	// — without this opt-in, the API does NOT emit encrypted_content
	// on reasoning output items in stateless mode, breaking
	// cross-turn echo silently. The translator sets this
	// automatically when reasoning is configured (ADR-051 D2).
	//
	// Other documented values (out of scope for Phase 1):
	// "message.input_image.image_url", "file_search_call.results",
	// "code_interpreter_call.outputs".
	Include []string `json:"include,omitempty"`
}

// ReasoningParams configures reasoning behavior on o-series and
// GPT-5.5-class models. Carried on both Request (input) and Response
// (echo); the same shape applies in both directions.
type ReasoningParams struct {
	// Effort is "minimal", "low", "medium", or "high". Empty falls
	// through to the model default.
	Effort string `json:"effort,omitempty"`

	// Summary controls reasoning summary emission. "auto", "concise",
	// "detailed", or empty for none.
	Summary string `json:"summary,omitempty"`
}

// TextParams controls the output text format on Responses requests.
// Distinct from ChatCompletion's top-level ResponseFormat — the
// Responses API nests format under text. Carried on both Request
// (input) and Response (echo).
type TextParams struct {
	// Format is the JSON-schema or json_object envelope. Carried as
	// RawMessage so callers can pass through the shape OpenAI
	// documents without this package taking a hard dependency on
	// the schema layout (which has evolved across SDK versions).
	Format json.RawMessage `json:"format,omitempty"`
}

// Tool is a function tool declaration. Responses-shape uses a flat
// {type, name, description, parameters, strict} layout — distinct
// from ChatCompletion's nested {type:"function", function:{...}}.
// Hosted tools (file_search, web_search_preview, code_interpreter,
// computer_use_preview, image_generation) are not modeled for Phase 1.
// Carried on both Request (input) and Response (echo).
type Tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}
