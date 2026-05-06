package agenticmodel

// OllamaAdapter routes through Ollama's OpenAI-compatible /v1/chat/completions
// endpoint and inherits GenericAdapter's safe cross-provider message
// normalizations (tool name/content fallbacks, same-role collapse). When
// agentic.AgentRequest.ResponseFormat is non-nil, chunk 2's applyResponseFormat
// has already plumbed it onto chatReq.ResponseFormat — Ollama's /v1 layer
// accepts the OpenAI shape and is the documented structured-output workaround
// (https://docs.ollama.com/capabilities/structured-outputs).
//
// CHUNK-3B DEFERRED — Ollama's NATIVE /api/chat endpoint accepts a top-level
// `format` field that constrains decoding via grammar rather than translating
// `response_format`, and per gh#10001 is more reliable on small/local models
// (gemma3 reportedly ignores `response_format` on /v1). The native path
// requires a separate HTTP client and request-shape translation that does not
// fit the SDK adapter pattern. Chunk 5 (integration test against real Ollama)
// gates whether chunk 3b ships:
//
//   - If integration confirms /v1 + response_format is reliable for the models
//     semspec actually deploys (qwen, deepseek), chunk 3b is unnecessary.
//   - If integration shows wedge classes the OpenAI-shape can't address, chunk
//     3b becomes a separate ADR — likely a new EndpointConfig.Mode flag
//     ("openai-compat" | "ollama-native") with a /api/chat client behind it.
//
// See ADR-034 "What's deferred" for the full rationale.
type OllamaAdapter struct {
	GenericAdapter
}

// Name returns "ollama".
func (a *OllamaAdapter) Name() string { return "ollama" }
