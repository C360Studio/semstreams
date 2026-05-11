package agenticmodel

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/model/wire"
)

// driveWire feeds each delta through wireStreamAccumulator.processChunk
// and concatenates the routed (content, reasoning) splits so tests can
// assert against the visible-to-handler stream as a whole. Mirrors
// `drive` for the SDK accumulator in stream_thinktag_test.go.
func driveWire(acc *wireStreamAccumulator, deltas []string) (chunkContent, chunkReasoning string) {
	var c, r strings.Builder
	for _, d := range deltas {
		delta := &wire.Message{}
		_ = delta.SetContentString(d)
		cd, rd := acc.processChunk(&wire.StreamChunk{
			Choices: []wire.Choice{{Delta: delta}},
		}, "req-think-test")
		c.WriteString(cd)
		r.WriteString(rd)
	}
	return c.String(), r.String()
}

func TestWireRouteDelta_TagFullyWithinSingleDelta(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"<think>quick</think>answer"})

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

func TestWireRouteDelta_OpenTagSplitAcrossBoundary(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"<thi", "nk>reasoning</think>final"})

	if cd != "final" {
		t.Errorf("contentDelta = %q, want %q", cd, "final")
	}
	if rd != "reasoning" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "reasoning")
	}
}

func TestWireRouteDelta_CloseTagSplitAcrossBoundary(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"<think>reasoning</thi", "nk>final"})

	if cd != "final" {
		t.Errorf("contentDelta = %q, want %q", cd, "final")
	}
	if rd != "reasoning" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "reasoning")
	}
}

func TestWireRouteDelta_ByteByByteDeltas(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	full := "<think>r</think>c"
	deltas := make([]string, 0, len(full))
	for _, ch := range full {
		deltas = append(deltas, string(ch))
	}
	cd, rd := driveWire(acc, deltas)

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

func TestWireRouteDelta_EmptyThinkBlock(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"<think></think>answer"})

	if cd != "answer" {
		t.Errorf("contentDelta = %q, want %q", cd, "answer")
	}
	if rd != "" {
		t.Errorf("reasoningDelta = %q, want empty", rd)
	}
}

func TestWireRouteDelta_UnmatchedOpenTagFlushedAtStreamEnd(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	acc := newWireStreamAccumulator(nil, logger, nil)
	driveWire(acc, []string{"prefix<think>middle"})

	// At this point the accumulator is mid-think with reasoning so far = "middle".
	// flushPending+toAgentResponse should warn and drain any pendingTag remainder.
	resp := acc.toAgentResponse("req-unmatched")
	if !strings.Contains(resp.Message.Content, "prefix") {
		t.Errorf("content should include pre-think prefix, got %q", resp.Message.Content)
	}
	if resp.Message.ReasoningContent != "middle" {
		t.Errorf("reasoning = %q, want %q", resp.Message.ReasoningContent, "middle")
	}
	if !strings.Contains(buf.String(), "streaming response ended mid-<think>") {
		t.Errorf("expected mid-think warning, got %q", buf.String())
	}
}

func TestWireRouteDelta_NoThinkPassesContentThroughUnchanged(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"hello world"})

	if cd != "hello world" {
		t.Errorf("contentDelta = %q, want %q", cd, "hello world")
	}
	if rd != "" {
		t.Errorf("reasoningDelta = %q, want empty", rd)
	}
}

func TestWireRouteDelta_MultipleThinkBlocks(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	cd, rd := driveWire(acc, []string{"<think>first</think>middle<think>second</think>end"})

	if cd != "middleend" {
		t.Errorf("contentDelta = %q, want %q", cd, "middleend")
	}
	if rd != "firstsecond" {
		t.Errorf("reasoningDelta = %q, want %q", rd, "firstsecond")
	}
}

func TestWireRouteDelta_ChannelReasoningStillSurfacedAlongsideInline(t *testing.T) {
	t.Parallel()

	acc := newWireStreamAccumulator(nil, nil, nil)
	// Inline-think delta + a separate channel-reasoning delta in the same chunk.
	delta := &wire.Message{ReasoningContent: "channel"}
	_ = delta.SetContentString("<think>inline</think>visible")
	cd, rd := acc.processChunk(&wire.StreamChunk{
		Choices: []wire.Choice{{Delta: delta}},
	}, "req-channel")

	if cd != "visible" {
		t.Errorf("contentDelta = %q, want %q", cd, "visible")
	}
	if !strings.Contains(rd, "inline") || !strings.Contains(rd, "channel") {
		t.Errorf("reasoningDelta = %q, want both inline+channel", rd)
	}
}

func TestWirePeelPartial_SharedWithSDK(t *testing.T) {
	t.Parallel()
	// peelPartial is package-level (shared between SDK and wire
	// accumulators). Spot-check that the wire path's routeDelta
	// uses the same one.
	if peelPartial("foo<thi", openTag) != "<thi" {
		t.Errorf("peelPartial regression — wire accumulator depends on this")
	}
	if peelPartial("foo", openTag) != "" {
		t.Error("peelPartial should return empty for no suffix match")
	}
}
