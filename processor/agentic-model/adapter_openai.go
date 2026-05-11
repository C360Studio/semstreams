package agenticmodel

import "github.com/c360studio/semstreams/model/wire"

// OpenAIAdapter handles OpenAI-specific features.
// OpenAI's endpoint is the reference implementation — most fields behave
// as documented, so this adapter is mostly a no-op extension point.
type OpenAIAdapter struct{}

// Name returns "openai".
func (a *OpenAIAdapter) Name() string { return "openai" }

// NormalizeRequest is a no-op for OpenAI; reasoning_effort is set directly from
// endpoint config in buildChatRequest and handled natively by OpenAI.
func (a *OpenAIAdapter) NormalizeRequest(_ *wire.ChatCompletionRequest) {}

// NormalizeMessages returns the messages unchanged; OpenAI requires no quirk fixes.
func (a *OpenAIAdapter) NormalizeMessages(messages []wire.Message) []wire.Message {
	return messages
}

// NormalizeStreamDelta uses the explicit index OpenAI always provides.
func (a *OpenAIAdapter) NormalizeStreamDelta(delta wire.ToolCall, _ int) int {
	if delta.Index != nil {
		return *delta.Index
	}
	return 0
}

// NormalizeResponse is a no-op for OpenAI.
func (a *OpenAIAdapter) NormalizeResponse(_ *wire.ChatCompletionResponse) {}
