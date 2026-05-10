// Package wire is the self-hosted JSON wire-format layer for
// LLM ChatCompletion calls. It owns request/response/streaming/error
// types and JSON round-tripping; transport, retry, registry, and
// provider-specific normalization remain in their existing homes.
//
// The defining feature is the Extras map[string]json.RawMessage
// carrier on Message, ToolCall, Choice, and Function: any JSON field
// the typed structs don't know about is preserved on unmarshal and
// re-emitted on marshal. This is what lets us thread provider-specific
// fields like Gemini's extra_content.google.thought_signature through
// a multi-turn round-trip without losing them — the SDK's fixed types
// drop unknown fields silently.
//
// The package is structurally OpenAI-shape (the lingua franca for
// ChatCompletion); adapters in processor/agentic-model/ translate
// agentic.ChatMessage to wire.Message and handle provider quirks.
//
// See ADR-037 for the full motivation and migration plan.
package wire
