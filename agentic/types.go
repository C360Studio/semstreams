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
	validRoles := map[string]bool{
		RoleArchitect: true, RoleEditor: true, RoleGeneral: true,
		RoleQualifier: true, RoleDeveloper: true, RoleReviewer: true,
	}
	if !validRoles[r.Role] {
		return fmt.Errorf("invalid role: %s", r.Role)
	}
	if r.ToolChoice != nil {
		if err := r.ToolChoice.Validate(); err != nil {
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
