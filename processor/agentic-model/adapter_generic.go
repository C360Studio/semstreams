package agenticmodel

import openai "github.com/sashabaranov/go-openai"

// GenericAdapter applies cross-provider safe normalizations that are either
// required by multiple providers or harmless for all known providers.
// It is the fallback when no provider-specific adapter is registered.
type GenericAdapter struct{}

// Name returns "generic".
func (a *GenericAdapter) Name() string { return "generic" }

// NormalizeRequest is a no-op for the generic adapter.
func (a *GenericAdapter) NormalizeRequest(_ *openai.ChatCompletionRequest) {}

// NormalizeMessages applies normalizations that are safe across all providers:
//
//  1. Tool result messages get a non-empty name field. The name field is optional
//     in the OpenAI spec but required by Gemini. Setting it universally is harmless.
//
//  2. Assistant messages with tool_calls get a non-empty content field. Gemini rejects
//     absent content; setting it to a single space is a widely-used convention
//     (LiteLLM, OpenAI proxy, etc.) and accepted by all known providers.
//
//  3. Consecutive messages with the same role are collapsed into one by joining
//     their content with "\n\n". Several providers (Anthropic and some Gemini
//     compatibility layers, including OpenRouter routes that bridge to them)
//     require strict role alternation and reject consecutive same-role messages.
//     Tool messages and assistant messages with tool_calls are preserved as-is —
//     each carries identity-bearing fields (ToolCallID, ToolCalls) that cannot
//     be merged without breaking tool-pair invariants.
//
// reasoning_content omission is handled structurally during message conversion
// (the field is never copied into the outgoing openai.ChatCompletionMessage).
func (a *GenericAdapter) NormalizeMessages(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].Name == "" {
			messages[i].Name = "unknown_tool"
		}
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 && messages[i].Content == "" {
			messages[i].Content = " "
		}
	}
	return collapseConsecutiveSameRole(messages)
}

// collapseConsecutiveSameRole merges adjacent messages with the same role by
// joining their Content with "\n\n". Tool messages and any message carrying
// tool_calls are excluded from merging because their identity-bearing fields
// (ToolCallID, ToolCalls) would be lost.
func collapseConsecutiveSameRole(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	if len(messages) < 2 {
		return messages
	}
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		last := len(out) - 1
		canMerge := last >= 0 &&
			out[last].Role == m.Role &&
			m.Role != "tool" &&
			len(m.ToolCalls) == 0 && len(out[last].ToolCalls) == 0
		if !canMerge {
			out = append(out, m)
			continue
		}
		switch {
		case out[last].Content == "":
			out[last].Content = m.Content
		case m.Content != "":
			out[last].Content = out[last].Content + "\n\n" + m.Content
		}
	}
	return out
}

// NormalizeStreamDelta infers the tool call index when the provider omits it.
// When an explicit index is provided, it is used directly. When absent, a
// non-empty ID signals a new tool call (return -1 sentinel so the accumulator
// allocates the next index), and an empty ID is an argument continuation
// (reuse lastIndex). This matches the behavior required by Gemini and is
// harmless for providers that always supply an explicit index.
func (a *GenericAdapter) NormalizeStreamDelta(delta openai.ToolCall, lastIndex int) int {
	if delta.Index != nil {
		return *delta.Index
	}
	if delta.ID != "" {
		return -1 // sentinel: caller must allocate next index via nextToolIndex
	}
	return lastIndex
}

// NormalizeResponse is a no-op for the generic adapter.
func (a *GenericAdapter) NormalizeResponse(_ *openai.ChatCompletionResponse) {}
