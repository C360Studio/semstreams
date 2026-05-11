package agenticmodel

import "github.com/c360studio/semstreams/model/wire"

// GenericAdapter applies cross-provider safe normalizations that are either
// required by multiple providers or harmless for all known providers.
// It is the fallback when no provider-specific adapter is registered.
type GenericAdapter struct{}

// Name returns "generic".
func (a *GenericAdapter) Name() string { return "generic" }

// NormalizeRequest is a no-op for the generic adapter.
func (a *GenericAdapter) NormalizeRequest(_ *wire.ChatCompletionRequest) {}

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
// (the field is never copied into the outgoing wire.Message on the request path).
func (a *GenericAdapter) NormalizeMessages(messages []wire.Message) []wire.Message {
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
	return collapseConsecutiveSameRole(messages)
}

// collapseConsecutiveSameRole merges adjacent messages with the same role by
// joining their Content with "\n\n". Tool messages and any message carrying
// tool_calls are excluded from merging because their identity-bearing fields
// (ToolCallID, ToolCalls) would be lost. Messages with non-string (array)
// Content also fall through unmerged — the SDK path never produces array
// content, and the wire-native path (chunk 7+) will treat multi-content as
// its own boundary class.
func collapseConsecutiveSameRole(messages []wire.Message) []wire.Message {
	if len(messages) < 2 {
		return messages
	}
	out := make([]wire.Message, 0, len(messages))
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
		prevContent, prevIsString := contentForMerge(out[last])
		curContent, curIsString := contentForMerge(m)
		// Array content (typed parts) on either side means the merge can't
		// safely happen — "\n\n" concatenation would discard the typed
		// semantics. Keep the boundary. Nil/missing content reads as the
		// empty string, mirroring the SDK shape's `Content == ""` branch.
		if !prevIsString || !curIsString {
			out = append(out, m)
			continue
		}
		switch {
		case prevContent == "":
			_ = out[last].SetContentString(curContent)
		case curContent != "":
			_ = out[last].SetContentString(prevContent + "\n\n" + curContent)
		}
	}
	return out
}

// contentForMerge returns the message's content viewed as a mergeable
// string. A nil/missing Content reads as ("", true) — empty but
// mergeable, matching the SDK-side `Content == ""` semantics. Array
// content reads as ("", false) — non-mergeable typed parts.
func contentForMerge(m wire.Message) (string, bool) {
	if len(m.Content) == 0 {
		return "", true
	}
	return m.ContentString()
}

// NormalizeStreamDelta infers the tool call index when the provider omits it.
// When an explicit index is provided, it is used directly. When absent, a
// non-empty ID signals a new tool call (return -1 sentinel so the accumulator
// allocates the next index), and an empty ID is an argument continuation
// (reuse lastIndex). This matches the behavior required by Gemini and is
// harmless for providers that always supply an explicit index.
func (a *GenericAdapter) NormalizeStreamDelta(delta wire.ToolCall, lastIndex int) int {
	if delta.Index != nil {
		return *delta.Index
	}
	if delta.ID != "" {
		return -1 // sentinel: caller must allocate next index via nextToolIndex
	}
	return lastIndex
}

// NormalizeResponse is a no-op for the generic adapter.
func (a *GenericAdapter) NormalizeResponse(_ *wire.ChatCompletionResponse) {}
