package agenticmodel

import "github.com/c360studio/semstreams/model/wire"

// GeminiAdapter normalizes payloads for Google's Gemini OpenAI-compatible endpoint.
// Gemini's endpoint is broadly compatible but has several quirks that cause 400 errors.
type GeminiAdapter struct{}

// Name returns "gemini".
func (a *GeminiAdapter) Name() string { return "gemini" }

// NormalizeRequest is a no-op for Gemini in chunk 6; chunk 8 wires the
// thought_signature Extras carrier for Gemini 3.x preview models.
func (a *GeminiAdapter) NormalizeRequest(_ *wire.ChatCompletionRequest) {}

// NormalizeMessages fixes two Gemini-specific message constraints:
//
//  1. Tool result messages require a non-empty name field.
//     Without it: 400 INVALID_ARGUMENT: function_response.name cannot be empty.
//
//  2. Assistant messages with tool_calls require a non-empty content field.
//     Gemini rejects a completely absent content — single space is the
//     conventional workaround (used by LiteLLM, OpenAI proxy, etc.).
func (a *GeminiAdapter) NormalizeMessages(messages []wire.Message) []wire.Message {
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].Name == "" {
			messages[i].Name = "unknown_tool"
		}
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			s, ok := messages[i].ContentString()
			if !ok || s == "" {
				_ = messages[i].SetContentString(" ")
			}
		}
	}
	return messages
}

// NormalizeStreamDelta infers the tool call index when Gemini omits it.
// Gemini streaming deltas never include an index field. Instead:
//   - A non-empty ID signals the start of a new tool call → return -1 (sentinel:
//     caller must allocate the next available index via nextToolIndex).
//   - An empty ID is an argument continuation → reuse lastIndex.
func (a *GeminiAdapter) NormalizeStreamDelta(delta wire.ToolCall, lastIndex int) int {
	if delta.Index != nil {
		return *delta.Index
	}
	if delta.ID != "" {
		// New tool call — signal the accumulator to allocate the next index.
		return -1
	}
	return lastIndex
}

// NormalizeResponse is a no-op for Gemini in chunk 6. Chunk 8 wires the
// thought_signature capture from Extras into agentic.ToolCall.Metadata.
func (a *GeminiAdapter) NormalizeResponse(_ *wire.ChatCompletionResponse) {}
