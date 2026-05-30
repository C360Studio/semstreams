package responses

import (
	"encoding/json"
)

// Response is the POST /v1/responses response body. Status reports
// the run's terminal state ("completed", "failed", "in_progress",
// "cancelled", "incomplete"); the Output array is the heterogeneous
// emitted items.
type Response struct {
	// ID is the provider-assigned response identifier ("resp_..."),
	// usable for previous_response_id threading when Store=true (not
	// used in stateless mode but echoed for trace correlation).
	ID string `json:"id"`

	// Object is the response object type, always "response" on
	// success. Carried for parity with OpenAI's documented shape.
	Object string `json:"object,omitempty"`

	// CreatedAt is the response creation Unix timestamp.
	CreatedAt int64 `json:"created_at,omitempty"`

	// Status is "completed", "failed", "in_progress", "cancelled",
	// or "incomplete".
	Status string `json:"status,omitempty"`

	// Error is populated when Status == "failed". The shape mirrors
	// APIError but is embedded in the response body rather than
	// raised as an HTTP error.
	Error json.RawMessage `json:"error,omitempty"`

	// IncompleteDetails is populated when Status == "incomplete".
	IncompleteDetails json.RawMessage `json:"incomplete_details,omitempty"`

	// Instructions echoes back the request's Instructions field.
	Instructions string `json:"instructions,omitempty"`

	// MaxOutputTokens echoes back the request's MaxOutputTokens.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`

	// Model is the served model identifier (may differ from requested
	// when the provider routes through a model alias).
	Model string `json:"model"`

	// Output is the heterogeneous emitted items array.
	Output []OutputItem `json:"output"`

	// PreviousResponseID is set when this response continued from a
	// prior response. Empty in stateless mode.
	PreviousResponseID string `json:"previous_response_id,omitempty"`

	// Reasoning echoes back the request's Reasoning settings.
	Reasoning *ReasoningParams `json:"reasoning,omitempty"`

	// Store reports whether server-side persistence is active.
	Store *bool `json:"store,omitempty"`

	// Temperature, TopP echo back the sampling controls.
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`

	// Text echoes back the request's Text settings.
	Text *TextParams `json:"text,omitempty"`

	// ToolChoice and Tools echo back the request configuration.
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
	Tools      []Tool          `json:"tools,omitempty"`

	// Truncation reports the truncation policy applied. Values
	// include "auto", "disabled". Carried as string for forward
	// compat.
	Truncation string `json:"truncation,omitempty"`

	// Usage reports token consumption.
	Usage *Usage `json:"usage,omitempty"`

	// User echoes back the request's User identifier.
	User string `json:"user,omitempty"`

	// Metadata echoes back the request's Metadata map.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Usage reports token consumption on a Response. Field names match
// the Responses API ("input_tokens", "output_tokens") — distinct
// from ChatCompletion's "prompt_tokens"/"completion_tokens".
type Usage struct {
	InputTokens        int                 `json:"input_tokens"`
	InputTokensDetails *InputTokensDetails `json:"input_tokens_details,omitempty"`

	OutputTokens        int                  `json:"output_tokens"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`

	TotalTokens int `json:"total_tokens"`
}

// InputTokensDetails breaks down prompt-side token attribution.
type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// OutputTokensDetails breaks down completion-side token attribution.
// ReasoningTokens accounts the o-series/GPT-5.5 internal reasoning
// budget separately from emitted output tokens.
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}
