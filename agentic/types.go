package agentic

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// ToolChoice controls how the model selects tools.
// Mode is one of: "auto" (default), "required", "none", or "function".
// When Mode is "function", FunctionName specifies which function to call.
type ToolChoice struct {
	Mode         string `json:"mode"`                    // "auto", "required", "none", "function"
	FunctionName string `json:"function_name,omitempty"` // required when Mode is "function"
}

// Validate checks if the ToolChoice has a valid mode and required fields.
func (tc ToolChoice) Validate() error {
	switch tc.Mode {
	case "auto", "required", "none":
		return nil
	case "function":
		if tc.FunctionName == "" {
			return fmt.Errorf("function_name required when tool_choice mode is \"function\"")
		}
		return nil
	default:
		return fmt.Errorf("invalid tool_choice mode: %q (must be auto, required, none, or function)", tc.Mode)
	}
}

// ResponseFormat constrains the model's output to a JSON object or
// JSON-schema-conformant structure. Maps to OpenAI's response_format on
// OpenAI-compatible providers; translated to provider-specific
// equivalents on others (Ollama: native format field; Gemini: stubbed
// for v1; Anthropic: stubbed for v1, future translation to forced
// single-tool-call). See ADR-034.
//
// Nil on AgentRequest means no structuring constraint — current behavior;
// tool-calling remains the structured-output primitive for personas that
// work fine on cloud providers without response_format.
type ResponseFormat struct {
	// Type: ResponseFormatJSONObject (legacy, bare JSON validity) or
	// ResponseFormatJSONSchema (current standard, strict-mode schema
	// adherence). Empty is invalid; callers must set one.
	Type string `json:"type"`

	// Schema is the JSON Schema document. Required when Type ==
	// ResponseFormatJSONSchema; ignored when Type ==
	// ResponseFormatJSONObject. Must be a valid OpenAI Structured Outputs
	// schema (subset of JSON Schema — no $ref to external schemas, no
	// anyOf at root, every property in required, additionalProperties:
	// false). Adapters do not validate locally; trust the caller.
	Schema map[string]any `json:"schema,omitempty"`

	// Name is required by OpenAI when Type == ResponseFormatJSONSchema.
	// Should describe the output (e.g. "decide_action_args"). Adapters
	// that don't need a name ignore it.
	Name string `json:"name,omitempty"`

	// Strict enables OpenAI's strict mode (response is guaranteed
	// schema-conformant; sampling is constrained). Defaults true via
	// NewJSONSchemaFormat. Set false to opt into permissive mode (rare;
	// mostly for compat testing).
	Strict bool `json:"strict"`
}

// NewJSONSchemaFormat constructs a strict-mode JSON-schema ResponseFormat.
// Strict defaults to true — the OpenAI Structured Outputs guarantee. Pass
// the schema directly; do not wrap in an outer json_schema envelope (the
// adapter layer handles wire-shape translation per provider).
func NewJSONSchemaFormat(name string, schema map[string]any) *ResponseFormat {
	return &ResponseFormat{
		Type:   ResponseFormatJSONSchema,
		Name:   name,
		Schema: schema,
		Strict: true,
	}
}

// NewJSONObjectFormat constructs a ResponseFormat that requires valid JSON
// output without imposing a schema. Less reliable than JSON-schema mode on
// small models; prefer NewJSONSchemaFormat when a schema is available.
func NewJSONObjectFormat() *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONObject}
}

// Validate checks if the ResponseFormat has a valid type and required fields.
func (rf *ResponseFormat) Validate() error {
	switch rf.Type {
	case ResponseFormatJSONObject:
		return nil
	case ResponseFormatJSONSchema:
		if rf.Name == "" {
			return fmt.Errorf("name required when response_format type is %q", ResponseFormatJSONSchema)
		}
		if len(rf.Schema) == 0 {
			return fmt.Errorf("schema required when response_format type is %q", ResponseFormatJSONSchema)
		}
		return nil
	default:
		return fmt.Errorf("invalid response_format type: %q (must be %q or %q)", rf.Type, ResponseFormatJSONObject, ResponseFormatJSONSchema)
	}
}

// AgentRequest represents a request to an agentic service
type AgentRequest struct {
	RequestID   string           `json:"request_id"`
	LoopID      string           `json:"loop_id"`
	Role        string           `json:"role"`
	Messages    []ChatMessage    `json:"messages"`
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  *ToolChoice      `json:"tool_choice,omitempty"`
	// ResponseFormat constrains output to JSON or JSON-schema-conformant
	// structure. Optional; nil means no structuring constraint (current
	// behavior; tool-calling remains the structured-output primitive when
	// nil). Honored on OpenAI-compat providers and Ollama; stubbed
	// (warn-once log) on Gemini and generic adapters for v1. See ADR-034.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	// Timeout caps this specific request. Go duration string (e.g. "30s").
	// Empty means fall through to endpoint, capability, or component-level
	// timeout. Takes precedence over all other timeout sources when set.
	Timeout string `json:"timeout,omitempty"`
}

// Validate checks if the AgentRequest is valid
func (r AgentRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages cannot be empty")
	}
	if r.ToolChoice != nil {
		if err := r.ToolChoice.Validate(); err != nil {
			return err
		}
	}
	if r.ResponseFormat != nil {
		if err := r.ResponseFormat.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Schema implements message.Payload
func (r *AgentRequest) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryRequest, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (r *AgentRequest) MarshalJSON() ([]byte, error) {
	type Alias AgentRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler
func (r *AgentRequest) UnmarshalJSON(data []byte) error {
	type Alias AgentRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// AgentResponse represents a response from an agentic service
type AgentResponse struct {
	RequestID    string      `json:"request_id"`
	Status       string      `json:"status"`
	FinishReason string      `json:"finish_reason,omitempty"` // Raw finish_reason from provider (stop, length, tool_calls)
	Message      ChatMessage `json:"message,omitempty"`
	Error        string      `json:"error,omitempty"`
	TokenUsage   TokenUsage  `json:"token_usage,omitempty"`
	RetryCount   int         `json:"retry_count,omitempty"`
}

// Validate checks if the AgentResponse is valid
func (r AgentResponse) Validate() error {
	switch r.Status {
	case StatusComplete, StatusToolCall, StatusError, StatusLengthTruncated:
		return nil
	default:
		return fmt.Errorf("status must be one of: %s, %s, %s, %s", StatusComplete, StatusToolCall, StatusError, StatusLengthTruncated)
	}
}

// Schema implements message.Payload
func (r *AgentResponse) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryResponse, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (r *AgentResponse) MarshalJSON() ([]byte, error) {
	type Alias AgentResponse
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler
func (r *AgentResponse) UnmarshalJSON(data []byte) error {
	type Alias AgentResponse
	return json.Unmarshal(data, (*Alias)(r))
}

// ChatMessage represents a message in a conversation
type ChatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	Name             string     `json:"name,omitempty"`              // Function name for tool role messages (required by Gemini)
	ReasoningContent string     `json:"reasoning_content,omitempty"` // Thinking model chain-of-thought
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"` // Required for tool role messages
	IsError          bool       `json:"is_error,omitempty"`     // Tool result contains an error — preserved during context GC
}

// UnmarshalJSON accepts both "reasoning" (Ollama) and "reasoning_content" (DeepSeek/canonical).
// If both are present, reasoning_content wins.
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type Alias ChatMessage
	aux := &struct {
		*Alias
		Reasoning string `json:"reasoning,omitempty"`
	}{Alias: (*Alias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if m.ReasoningContent == "" && aux.Reasoning != "" {
		m.ReasoningContent = aux.Reasoning
	}
	return nil
}

// Validate checks if the ChatMessage is valid
func (m ChatMessage) Validate() error {
	if m.Role != "system" && m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
		return fmt.Errorf("role must be one of: system, user, assistant, tool")
	}
	if m.Content == "" && m.ReasoningContent == "" && len(m.ToolCalls) == 0 {
		return fmt.Errorf("either content, reasoning_content, or tool_calls must be present")
	}
	return nil
}

// ModelConfig represents configuration for a language model
type ModelConfig struct {
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

// WithDefaults returns a copy of the config with default values applied
func (c ModelConfig) WithDefaults() ModelConfig {
	result := c
	if result.Temperature == 0 {
		result.Temperature = 0.2
	}
	if result.MaxTokens == 0 {
		result.MaxTokens = 4096
	}
	return result
}

// TokenUsage tracks token consumption for a request
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Total returns the total number of tokens used
func (u TokenUsage) Total() int {
	return u.PromptTokens + u.CompletionTokens
}
