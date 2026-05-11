package agenticmodel

import "github.com/c360studio/semstreams/model/wire"

// ProviderAdapter normalizes request/response payloads for a specific
// LLM provider's OpenAI-compatible endpoint. Adapters handle quirks
// that would otherwise cause 400 errors or silent data corruption.
//
// As of ADR-037 chunk 6 the interface operates on wire types. Adapter
// implementations are wire-native; the SDK client path (chunk 6
// transition) translates SDK ↔ wire at adapter call boundaries via
// translate.go. Chunk 7 wires the wire-native client; from chunk 8
// onward `NormalizeResponse` gains real responsibility for provider
// blob translation (Gemini thought_signature, Anthropic, etc.).
type ProviderAdapter interface {
	// Name returns the provider identifier (e.g., "gemini", "openai").
	Name() string

	// NormalizeRequest adjusts the wire request before sending.
	// Called after the generic request is built, before the HTTP call.
	NormalizeRequest(req *wire.ChatCompletionRequest)

	// NormalizeMessages adjusts the message array before sending.
	// Called during request building for message-level fixes.
	NormalizeMessages(messages []wire.Message) []wire.Message

	// NormalizeStreamDelta adjusts a streaming tool call delta.
	// Returns the corrected tool call index, or -1 as a sentinel meaning
	// "allocate the next available index" (used when the provider omits it).
	NormalizeStreamDelta(delta wire.ToolCall, lastIndex int) int

	// NormalizeResponse adjusts the wire response after receiving.
	// Called before the response is converted to AgentResponse.
	NormalizeResponse(resp *wire.ChatCompletionResponse)
}

// defaultAdapter is the fallback used when no provider-specific adapter is set.
// Package-level singleton avoids repeated allocation of the stateless struct.
var defaultAdapter ProviderAdapter = &GenericAdapter{}

// AdapterFor returns the appropriate adapter for the given provider name.
// Falls back to GenericAdapter for unknown providers.
//
// Note: "openai" is the umbrella for any OpenAI-API-compatible runtime
// (vLLM, sparky, OpenRouter, LocalAI, llama.cpp server) — operators set
// `provider: "openai"` plus the appropriate URL. The two genuine outliers
// are "gemini" (distinct API surface) and "ollama" (separate adapter for
// future /api/chat native-format-field path; see ADR-034).
func AdapterFor(provider string) ProviderAdapter {
	switch provider {
	case "gemini":
		return &GeminiAdapter{}
	case "openai":
		return &OpenAIAdapter{}
	case "ollama":
		return &OllamaAdapter{}
	default:
		return &GenericAdapter{}
	}
}
