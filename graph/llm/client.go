// Package llm provides LLM client abstractions for OpenAI-compatible APIs.
//
// This package enables semstreams to use any OpenAI-compatible LLM service
// (seminstruct, OpenAI, vLLM, Ollama, etc.) for:
//   - Community summarization
//   - Search answer generation
//   - General inference tasks
//
// The package follows the same patterns as the embedding package, using
// the OpenAI SDK for consistency and compatibility.
package llm

import (
	"context"
)

// Client defines the interface for LLM operations.
// Implementations can connect to any OpenAI-compatible API.
type Client interface {
	// ChatCompletion sends a chat completion request and returns the response.
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// Model returns the model identifier being used.
	Model() string

	// Close releases any resources held by the client.
	Close() error
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	// SystemPrompt is the system message that sets the assistant's behavior.
	SystemPrompt string

	// UserPrompt is the user's message/question.
	UserPrompt string

	// MaxTokens limits the response length (default: 256).
	MaxTokens int

	// Temperature controls randomness (0.0-2.0, default: 0.7).
	// Use nil for default, or pointer to 0.0 for deterministic output.
	Temperature *float64
}

// ChatResponse represents a chat completion response.
type ChatResponse struct {
	// Content is the generated text response.
	Content string

	// PromptTokens is the number of tokens in the prompt.
	PromptTokens int

	// CompletionTokens is the number of tokens in the response.
	CompletionTokens int

	// TotalTokens is the total tokens used.
	TotalTokens int

	// Model is the model that generated the response.
	Model string

	// FinishReason indicates why generation stopped ("stop", "length", etc.).
	FinishReason string
}
