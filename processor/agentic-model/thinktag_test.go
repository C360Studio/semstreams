package agenticmodel

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestExtractThinkBlocks_NoTagPassesThrough(t *testing.T) {
	t.Parallel()

	input := "Just a regular response with no thinking tags at all."
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != input {
		t.Errorf("cleaned = %q, want unchanged input", cleaned)
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty", reasoning)
	}
}

func TestExtractThinkBlocks_SingleBlock(t *testing.T) {
	t.Parallel()

	input := "<think>Let me reason about this step by step.</think>The answer is 42."
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "The answer is 42." {
		t.Errorf("cleaned = %q, want %q", cleaned, "The answer is 42.")
	}
	if reasoning != "Let me reason about this step by step." {
		t.Errorf("reasoning = %q, want %q", reasoning, "Let me reason about this step by step.")
	}
}

func TestExtractThinkBlocks_EmptyThenNonemptyBlockNoLeadingSeparator(t *testing.T) {
	t.Parallel()

	// Reviewer-flagged regression case (M1): empty first block must
	// not emit a leading "\n" before a subsequent non-empty block.
	input := "<think></think>middle<think>second</think>end"
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "middleend" {
		t.Errorf("cleaned = %q, want %q", cleaned, "middleend")
	}
	if reasoning != "second" {
		t.Errorf("reasoning = %q, want %q (no leading newline)", reasoning, "second")
	}
}

func TestExtractThinkBlocks_NonemptyThenEmptyBlockNoTrailingSeparator(t *testing.T) {
	t.Parallel()

	input := "<think>first</think>middle<think></think>end"
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "middleend" {
		t.Errorf("cleaned = %q, want %q", cleaned, "middleend")
	}
	if reasoning != "first" {
		t.Errorf("reasoning = %q, want %q (no trailing newline)", reasoning, "first")
	}
}

func TestExtractThinkBlocks_SurroundingSpacesCollapse(t *testing.T) {
	t.Parallel()

	// Reviewer-flagged regression case (M2): mid-string block with
	// spaces on both sides must clean to single-space separator,
	// not double-space.
	input := "Hello <think>x</think> world"
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "Hello world" {
		t.Errorf("cleaned = %q, want %q (single space, not double)", cleaned, "Hello world")
	}
	if reasoning != "x" {
		t.Errorf("reasoning = %q, want %q", reasoning, "x")
	}
}

func TestExtractThinkBlocks_AdjacentNoSpacesPreserved(t *testing.T) {
	t.Parallel()

	// No spaces around the tag → no spaces in cleaned. The
	// whitespace-collapse fix must NOT introduce a space when the
	// model emitted contiguous text.
	input := "a<think>x</think>b"
	cleaned, _ := extractThinkBlocks(input)

	if cleaned != "ab" {
		t.Errorf("cleaned = %q, want %q (no introduced space)", cleaned, "ab")
	}
}

func TestExtractThinkBlocks_MultipleBlocksWithSpaces(t *testing.T) {
	t.Parallel()

	input := "a <think>x</think> b <think>y</think> c"
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "a b c" {
		t.Errorf("cleaned = %q, want %q", cleaned, "a b c")
	}
	if reasoning != "x\ny" {
		t.Errorf("reasoning = %q, want %q", reasoning, "x\ny")
	}
}

func TestExtractThinkBlocks_MultipleBlocks(t *testing.T) {
	t.Parallel()

	input := "<think>First thought.</think>Some content.<think>Second thought.</think>More content."
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "Some content.More content." {
		t.Errorf("cleaned = %q, want %q", cleaned, "Some content.More content.")
	}
	want := "First thought.\nSecond thought."
	if reasoning != want {
		t.Errorf("reasoning = %q, want %q", reasoning, want)
	}
}

func TestExtractThinkBlocks_MultilineBlock(t *testing.T) {
	t.Parallel()

	input := "<think>Line one.\nLine two.\nLine three.</think>The answer."
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "The answer." {
		t.Errorf("cleaned = %q, want %q", cleaned, "The answer.")
	}
	want := "Line one.\nLine two.\nLine three."
	if reasoning != want {
		t.Errorf("reasoning = %q, want %q", reasoning, want)
	}
}

func TestExtractThinkBlocks_EmptyBlock(t *testing.T) {
	t.Parallel()

	input := "<think></think>Just the answer."
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "Just the answer." {
		t.Errorf("cleaned = %q, want %q", cleaned, "Just the answer.")
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty (block was empty)", reasoning)
	}
}

func TestExtractThinkBlocks_UnmatchedOpenTagPreserved(t *testing.T) {
	t.Parallel()

	// Streaming path handles mid-think truncation differently; non-
	// streaming callers shouldn't reach this state, but if they do
	// the dangling tag stays in content rather than getting silently
	// stripped.
	input := "<think>Started reasoning but"
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != input {
		t.Errorf("cleaned = %q, want unchanged input (no closing tag)", cleaned)
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty (no complete block)", reasoning)
	}
}

func TestExtractThinkBlocks_WhitespaceTrimmed(t *testing.T) {
	t.Parallel()

	input := "  <think>reasoning</think>  answer  "
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != "answer" {
		t.Errorf("cleaned = %q, want %q (whitespace should be trimmed)", cleaned, "answer")
	}
	if reasoning != "reasoning" {
		t.Errorf("reasoning = %q, want %q", reasoning, "reasoning")
	}
}

func TestExtractThinkBlocks_HotPathNoAllocation(t *testing.T) {
	t.Parallel()

	// Confirms the no-think path returns the original string unchanged
	// (no .ReplaceAll call, no Builder allocation). This isn't a perf
	// benchmark — just a smoke test that the early-return path is
	// taken when there's no <think> substring. We rely on the
	// strings.Contains short-circuit at the top of extractThinkBlocks.
	input := strings.Repeat("normal content with no tags. ", 100)
	cleaned, reasoning := extractThinkBlocks(input)

	if cleaned != input {
		t.Error("cleaned string should be identical to input on no-think path")
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty", reasoning)
	}
}

func TestExtractThinkBlocksFromResponse_Nil(t *testing.T) {
	t.Parallel()

	// Defensive — should never get a nil response in production but
	// the signature accepts a pointer, so guard it.
	extractThinkBlocksFromResponse(nil)
}

func TestExtractThinkBlocksFromResponse_NoChoices(t *testing.T) {
	t.Parallel()

	resp := &openai.ChatCompletionResponse{}
	extractThinkBlocksFromResponse(resp)

	if len(resp.Choices) != 0 {
		t.Errorf("choices count changed: got %d, want 0", len(resp.Choices))
	}
}

func TestExtractThinkBlocksFromResponse_ExtractsInlineTags(t *testing.T) {
	t.Parallel()

	resp := &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "<think>I should compute 6*7.</think>The answer is 42.",
				},
			},
		},
	}

	extractThinkBlocksFromResponse(resp)

	got := resp.Choices[0].Message
	if got.Content != "The answer is 42." {
		t.Errorf("Content = %q, want %q", got.Content, "The answer is 42.")
	}
	if got.ReasoningContent != "I should compute 6*7." {
		t.Errorf("ReasoningContent = %q, want %q", got.ReasoningContent, "I should compute 6*7.")
	}
}

func TestExtractThinkBlocksFromResponse_PreservesExistingReasoningContent(t *testing.T) {
	t.Parallel()

	// A provider that emits BOTH the channel-based reasoning_content
	// AND inline <think> tags (rare; may happen in mixed deployments).
	// Inline-tag content appends, doesn't replace.
	resp := &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Role:             "assistant",
					Content:          "<think>inline reasoning</think>Final answer.",
					ReasoningContent: "channel-based reasoning",
				},
			},
		},
	}

	extractThinkBlocksFromResponse(resp)

	got := resp.Choices[0].Message
	if got.Content != "Final answer." {
		t.Errorf("Content = %q, want %q", got.Content, "Final answer.")
	}
	want := "channel-based reasoning\ninline reasoning"
	if got.ReasoningContent != want {
		t.Errorf("ReasoningContent = %q, want %q", got.ReasoningContent, want)
	}
}

func TestExtractThinkBlocksFromResponse_MultipleChoices(t *testing.T) {
	t.Parallel()

	resp := &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{Role: "assistant", Content: "<think>a</think>answer A"}},
			{Message: openai.ChatCompletionMessage{Role: "assistant", Content: "no tags here"}},
			{Message: openai.ChatCompletionMessage{Role: "assistant", Content: "<think>c</think>answer C"}},
		},
	}

	extractThinkBlocksFromResponse(resp)

	if resp.Choices[0].Message.Content != "answer A" {
		t.Errorf("choice 0 Content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.ReasoningContent != "a" {
		t.Errorf("choice 0 ReasoningContent = %q", resp.Choices[0].Message.ReasoningContent)
	}
	if resp.Choices[1].Message.Content != "no tags here" {
		t.Errorf("choice 1 Content (unchanged) = %q", resp.Choices[1].Message.Content)
	}
	if resp.Choices[1].Message.ReasoningContent != "" {
		t.Errorf("choice 1 ReasoningContent = %q, want empty", resp.Choices[1].Message.ReasoningContent)
	}
	if resp.Choices[2].Message.Content != "answer C" {
		t.Errorf("choice 2 Content = %q", resp.Choices[2].Message.Content)
	}
}
