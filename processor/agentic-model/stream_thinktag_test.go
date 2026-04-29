package agenticmodel

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

// drive feeds each delta through processDelta and concatenates the
// routed (content, reasoning) splits so tests can assert against
// the visible-to-handler stream as a whole.
func drive(acc *streamAccumulator, deltas []string) (chunkContent, chunkReasoning string) {
	var c, r strings.Builder
	for _, d := range deltas {
		cd, rd := acc.processDelta(openai.ChatCompletionStreamChoice{
			Delta: openai.ChatCompletionStreamChoiceDelta{Content: d},
		})
		c.WriteString(cd)
		r.WriteString(rd)
	}
	return c.String(), r.String()
}

func TestRouteDelta_TagFullyWithinSingleDelta(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	cd, rd := drive(acc, []string{"<think>quick</think>answer"})

	if cd != "answer" {
		t.Errorf("contentDelta = %q, want %q", cd, "answer")
	}
	if rd != "quick" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "quick")
	}
	if acc.inThink {
		t.Error("inThink should be false at end")
	}
	if acc.pendingTag != "" {
		t.Errorf("pendingTag = %q, want empty", acc.pendingTag)
	}
}

func TestRouteDelta_OpenTagSplitAcrossBoundary(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	// "<thi" + "nk>reasoning</think>final"
	cd, rd := drive(acc, []string{"<thi", "nk>reasoning</think>final"})

	if cd != "final" {
		t.Errorf("contentDelta = %q, want %q", cd, "final")
	}
	if rd != "reasoning" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "reasoning")
	}
}

func TestRouteDelta_CloseTagSplitAcrossBoundary(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	// "<think>reasoning</thi" + "nk>final"
	cd, rd := drive(acc, []string{"<think>reasoning</thi", "nk>final"})

	if cd != "final" {
		t.Errorf("contentDelta = %q, want %q", cd, "final")
	}
	if rd != "reasoning" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "reasoning")
	}
}

func TestRouteDelta_ByteByByteDeltas(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	// Hostile worst case: every character is its own delta.
	full := "<think>r</think>c"
	deltas := make([]string, 0, len(full))
	for _, ch := range full {
		deltas = append(deltas, string(ch))
	}
	cd, rd := drive(acc, deltas)

	if cd != "c" {
		t.Errorf("contentDelta = %q, want %q", cd, "c")
	}
	if rd != "r" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "r")
	}
	if acc.inThink {
		t.Error("inThink should be false at end")
	}
	if acc.pendingTag != "" {
		t.Errorf("pendingTag = %q, want empty", acc.pendingTag)
	}
}

func TestRouteDelta_EmptyThinkBlock(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	cd, rd := drive(acc, []string{"<think></think>answer"})

	if cd != "answer" {
		t.Errorf("contentDelta = %q, want %q", cd, "answer")
	}
	if rd != "" {
		t.Errorf("reasoningDelta = %q, want empty (block was empty)", rd)
	}
}

func TestRouteDelta_UnmatchedOpenTagFlushedAtStreamEnd(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	acc := &streamAccumulator{logger: logger}

	// Model started thinking and got cut off mid-stream.
	cd, rd := drive(acc, []string{"<think>partial reasoning, no close"})

	if cd != "" {
		t.Errorf("contentDelta during stream = %q, want empty (still inside think)", cd)
	}
	if rd != "partial reasoning, no close" {
		t.Errorf("reasoningDelta during stream = %q", rd)
	}
	if !acc.inThink {
		t.Error("inThink should still be true (no close tag seen)")
	}

	// Build response — flushPending fires inside.
	resp := acc.toAgentResponse("req-1")

	if resp.Message.Content != "" {
		t.Errorf("Content = %q, want empty (no closed think block)", resp.Message.Content)
	}
	if resp.Message.ReasoningContent != "partial reasoning, no close" {
		t.Errorf("ReasoningContent = %q", resp.Message.ReasoningContent)
	}
	if got := buf.String(); !strings.Contains(got, "ended mid-<think>") {
		t.Errorf("expected truncation warning, got log: %q", got)
	}
}

func TestRouteDelta_PartialCloseTagAtEOFInsideThinkFlushedAsReasoning(t *testing.T) {
	t.Parallel()

	// Reviewer-asked test: stream cuts off mid-close-tag while
	// still inside the think block. The partial close-tag bytes
	// were buffered; flushPending must route them to reasoning
	// (where the rest of the block was already going) and emit
	// the truncation warning.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	acc := &streamAccumulator{logger: logger}

	// "<think>thinking</thi" — close tag started but didn't finish.
	cd, rd := drive(acc, []string{"<think>thinking</thi"})

	if cd != "" {
		t.Errorf("contentDelta during stream = %q, want empty", cd)
	}
	// "thinking" should have been routed; "</thi" buffered.
	if rd != "thinking" {
		t.Errorf("reasoningDelta during stream = %q, want %q", rd, "thinking")
	}
	if !acc.inThink {
		t.Error("inThink should still be true (close tag not seen)")
	}
	if acc.pendingTag != "</thi" {
		t.Errorf("pendingTag = %q, want %q", acc.pendingTag, "</thi")
	}

	resp := acc.toAgentResponse("req-1")

	if resp.Message.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Message.Content)
	}
	want := "thinking</thi"
	if resp.Message.ReasoningContent != want {
		t.Errorf("ReasoningContent = %q, want %q", resp.Message.ReasoningContent, want)
	}
	if got := buf.String(); !strings.Contains(got, "ended mid-<think>") {
		t.Errorf("expected truncation warning, got log: %q", got)
	}
}

func TestRouteDelta_InThinkAtEOFWithEmptyPendingTagStillWarns(t *testing.T) {
	t.Parallel()

	// Reviewer-asked test: inThink with no buffered partial bytes
	// (stream ended exactly at a clean point but never closed).
	// flushPending should still emit the truncation warning so
	// operators see the unclosed think block.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	acc := &streamAccumulator{logger: logger}

	// "<think>complete reasoning text " — note no partial close tag.
	drive(acc, []string{"<think>complete reasoning text "})

	if !acc.inThink {
		t.Error("inThink should be true")
	}
	if acc.pendingTag != "" {
		t.Errorf("pendingTag = %q, want empty", acc.pendingTag)
	}

	resp := acc.toAgentResponse("req-1")

	if resp.Message.ReasoningContent != "complete reasoning text " {
		t.Errorf("ReasoningContent = %q", resp.Message.ReasoningContent)
	}
	if got := buf.String(); !strings.Contains(got, "ended mid-<think>") {
		t.Errorf("expected truncation warning, got log: %q", got)
	}
}

func TestRouteDelta_TrailingPartialTagOutsideThinkPreservedAsContent(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	// Stream ends with "<thi" buffered (could have been a tag, but
	// stream truncated). Outside think → flush as content.
	cd, _ := drive(acc, []string{"hello<thi"})

	if cd != "hello" {
		t.Errorf("contentDelta during stream = %q, want %q", cd, "hello")
	}
	if acc.pendingTag != "<thi" {
		t.Errorf("pendingTag = %q, want %q", acc.pendingTag, "<thi")
	}

	resp := acc.toAgentResponse("req-1")
	if resp.Message.Content != "hello<thi" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "hello<thi")
	}
	if resp.Message.ReasoningContent != "" {
		t.Errorf("ReasoningContent = %q, want empty", resp.Message.ReasoningContent)
	}
}

func TestRouteDelta_MultipleThinkBlocks(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	cd, rd := drive(acc, []string{"<think>first</think>middle<think>second</think>end"})

	if cd != "middleend" {
		t.Errorf("contentDelta = %q, want %q", cd, "middleend")
	}
	if rd != "firstsecond" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "firstsecond")
	}
}

func TestRouteDelta_NoThinkPassesContentThroughUnchanged(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	cd, rd := drive(acc, []string{"hello ", "world"})

	if cd != "hello world" {
		t.Errorf("contentDelta = %q, want %q", cd, "hello world")
	}
	if rd != "" {
		t.Errorf("reasoningDelta = %q, want empty", rd)
	}
}

func TestRouteDelta_ChannelReasoningStillSurfacedAlongsideInline(t *testing.T) {
	t.Parallel()

	acc := &streamAccumulator{}
	// Hybrid: a delta carries BOTH inline-tag content AND channel-
	// based reasoning_content. Both should surface in reasoningDelta;
	// content should be cleaned.
	cd, rd := acc.processDelta(openai.ChatCompletionStreamChoice{
		Delta: openai.ChatCompletionStreamChoiceDelta{
			Content:          "<think>inline</think>final",
			ReasoningContent: "channel",
		},
	})

	if cd != "final" {
		t.Errorf("contentDelta = %q, want %q", cd, "final")
	}
	// reasoningDelta is "inline" + "channel" — order isn't strictly
	// guaranteed but the implementation today appends channel after
	// inline routing.
	if rd != "inlinechannel" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "inlinechannel")
	}
}

func TestPeelPartial_Suffixes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		s, target, want string
	}{
		{"hello<thi", "<think>", "<thi"},
		{"x<", "<think>", "<"},
		{"abc", "<think>", ""},
		{"", "<think>", ""},
		{"<<", "<think>", "<"}, // longest matching proper-prefix suffix
		{"</thin", "</think>", "</thin"},
		{"</think", "</think>", "</think"}, // 7 chars = max proper prefix
		{"</think>", "</think>", ""},       // full tag is not a proper-prefix suffix of itself; caller handles complete tags via Index
	}

	for _, tc := range cases {
		got := peelPartial(tc.s, tc.target)
		if got != tc.want {
			t.Errorf("peelPartial(%q, %q) = %q, want %q", tc.s, tc.target, got, tc.want)
		}
	}
}
