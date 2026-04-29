package agenticmodel

import (
	"regexp"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// thinkTagPattern matches <think>...</think> blocks emitted inline
// in content by qwen3 / deepseek-r1 / qwen3-coder reasoning models
// when served via OpenAI-compatible endpoints (Ollama compat path
// in particular). (?s) so . matches newlines; non-greedy so multiple
// blocks parse independently. Case-sensitive lowercase tags only —
// known reasoning models all emit lowercase; <THINK> would be
// novel enough to warrant a separate decision.
//
// Distinct from channel-based reasoning where providers populate the
// separate `reasoning_content` field — that path is handled
// structurally by the OpenAI SDK and the streaming accumulator.
// Inline tags are the same signal in a different wire shape; the
// extract here routes them into the same downstream field
// (ChatMessage.ReasoningContent) so consumers don't have to know
// which provider/model emitted them.
var thinkTagPattern = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

// extractThinkBlocks splits content into the user-visible portion
// and the inline-tag reasoning portion. Multiple complete blocks are
// extracted in order and joined with "\n" in the returned reasoning;
// content outside the blocks is preserved with leading/trailing
// whitespace trimmed.
//
// Hot path optimization: when the literal substring "<think>" is
// absent, the original content is returned with empty reasoning and
// no allocation. This matters because every non-streaming response
// flows through here regardless of provider.
//
// Edge cases:
//   - Empty block (<think></think>): block is removed; reasoning is
//     empty (still appended with the "\n" separator if other blocks
//     are present).
//   - Unmatched open tag (<think> with no closing </think>): pattern
//     does NOT match, so the dangling tag stays in content. Streaming
//     handles the truncation case; non-streaming callers shouldn't
//     see this unless the upstream response is malformed.
//   - Tags inside a code fence are not specially handled — if a model
//     emits prose code containing literal <think> tags, the regex
//     will misfire. False-positive risk is low for production
//     reasoning models, which use the tags structurally.
func extractThinkBlocks(content string) (cleaned, reasoning string) {
	if !strings.Contains(content, "<think>") {
		return content, ""
	}

	matches := thinkTagPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, ""
	}

	var (
		cleanedBuilder   strings.Builder
		reasoningBuilder strings.Builder
		prev             int
	)

	for _, m := range matches {
		// m = [matchStart, matchEnd, group1Start, group1End].
		// Trim trailing whitespace from the pre-tag segment so a
		// "Hello <think>x</think> world" input cleans to
		// "Hello world" rather than "Hello  world" (two spaces).
		// The post-tag segment's leading whitespace then provides
		// the single separator between the surviving halves.
		cleanedBuilder.WriteString(strings.TrimRight(content[prev:m[0]], " \t"))

		// Separator between consecutive non-empty reasoning blocks
		// only — empty blocks don't generate a leading "\n" on the
		// next non-empty block.
		if reasoningBuilder.Len() > 0 && m[3] > m[2] {
			reasoningBuilder.WriteByte('\n')
		}
		reasoningBuilder.WriteString(content[m[2]:m[3]])
		prev = m[1]
	}
	cleanedBuilder.WriteString(content[prev:])

	return strings.TrimSpace(cleanedBuilder.String()), reasoningBuilder.String()
}

// extractThinkBlocksFromResponse applies extractThinkBlocks to every
// choice's assistant message in a ChatCompletionResponse. Inline
// reasoning is appended to the choice's existing ReasoningContent
// (rare but possible if a provider sends both inline tags and the
// channel-based field).
//
// Called unconditionally in convertResponse after the provider
// adapter's NormalizeResponse — see commentary there for why this
// lives universally rather than on a single adapter (inline-think
// is a model-family quirk, not a provider quirk; qwen3-via-Ollama
// and qwen3-via-OpenAI-compat both emit it).
func extractThinkBlocksFromResponse(resp *openai.ChatCompletionResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Choices {
		msg := &resp.Choices[i].Message
		cleaned, extracted := extractThinkBlocks(msg.Content)
		if extracted == "" {
			// extractThinkBlocks contract: empty extracted means no
			// complete block matched, and cleaned is the original
			// string unchanged. Skip without writing back.
			continue
		}
		msg.Content = cleaned
		if msg.ReasoningContent != "" {
			msg.ReasoningContent += "\n" + extracted
		} else {
			msg.ReasoningContent = extracted
		}
	}
}
