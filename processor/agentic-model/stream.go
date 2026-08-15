package agenticmodel

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	openai "github.com/sashabaranov/go-openai"
)

// StreamChunk represents a single streaming delta for real-time monitoring.
// Chunks are ephemeral — published via core NATS (fire-and-forget), not JetStream.
type StreamChunk struct {
	RequestID      string `json:"request_id"`
	ContentDelta   string `json:"content_delta,omitempty"`
	ReasoningDelta string `json:"reasoning_delta,omitempty"`
	Done           bool   `json:"done,omitempty"`
}

// ChunkHandler is a callback for receiving streaming deltas.
// Implementations must be safe for concurrent use if the handler is shared.
type ChunkHandler func(ctx context.Context, chunk StreamChunk)

// streamAccumulator aggregates streaming deltas into a complete AgentResponse.
type streamAccumulator struct {
	content          strings.Builder
	reasoning        strings.Builder
	role             string
	finishReason     openai.FinishReason
	toolCalls        map[int]*openai.ToolCall
	promptTokens     int
	completionTokens int
	lastToolIndex    int             // tracks last assigned index for provider missing-index inference
	adapter          ProviderAdapter // normalizes stream deltas for the active provider
	logger           *slog.Logger

	// Inline-think extraction state. Mirrors the non-streaming
	// extraction in extractThinkBlocks but operates per-delta because
	// <think>/</think> tags can straddle chunk boundaries (a delta
	// might end with `<thi`, the next start with `nk>`).
	//
	//   inThink:    true while between an open and close tag.
	//   pendingTag: bytes peeled from the end of a delta that could
	//               complete a partial tag in the next delta. Always
	//               a (possibly-empty) prefix of the open tag when
	//               !inThink, or of the close tag when inThink.
	inThink    bool
	pendingTag string
}

// processDelta incorporates a single streaming choice delta and
// returns the (content, reasoning) pair that should be forwarded to
// the chunk handler — both routed through the inline-think state
// machine. Returning the routed split here keeps the state
// transition atomic with its observable effect: a delta that opens
// a <think> block appends to a.reasoning AND has its routed
// reasoningDelta visible to live UI consumers in the same call.
func (a *streamAccumulator) processDelta(choice openai.ChatCompletionStreamChoice) (contentDelta, reasoningDelta string) {
	delta := choice.Delta

	if delta.Role != "" {
		a.role = delta.Role
	}

	if delta.Content != "" {
		contentDelta, reasoningDelta = a.routeDelta(delta.Content)
		if contentDelta != "" {
			a.content.WriteString(contentDelta)
		}
		if reasoningDelta != "" {
			a.reasoning.WriteString(reasoningDelta)
		}
	}

	if delta.ReasoningContent != "" {
		// Channel-based reasoning is independent of the inline-tag
		// path — concatenate to the same reasoning buffer and surface
		// to the chunk handler alongside any inline-routed reasoning
		// from this delta.
		a.reasoning.WriteString(delta.ReasoningContent)
		reasoningDelta += delta.ReasoningContent
	}

	if choice.FinishReason != "" {
		a.finishReason = choice.FinishReason
	}

	// Aggregate tool call deltas by index
	for _, tc := range delta.ToolCalls {
		idx := a.inferToolIndex(tc)

		if a.toolCalls == nil {
			a.toolCalls = make(map[int]*openai.ToolCall)
		}

		existing, ok := a.toolCalls[idx]
		if !ok {
			existing = &openai.ToolCall{
				Type: openai.ToolTypeFunction,
			}
			a.toolCalls[idx] = existing
		}

		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Function.Name != "" {
			existing.Function.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			existing.Function.Arguments += tc.Function.Arguments
		}

		a.lastToolIndex = idx
	}

	return contentDelta, reasoningDelta
}

// openTag and closeTag are the inline-think marker pair the streaming
// state machine looks for. Hoisted to package level so peelPartial
// avoids reallocating the strings on every delta.
const (
	openTag  = "<think>"
	closeTag = "</think>"
)

// routeDelta walks a single delta's content through the inline-think
// state machine, splitting it into (contentDelta, reasoningDelta)
// pairs and updating a.inThink + a.pendingTag for the next delta.
//
// Algorithm: prepend any pending partial tag from the prior delta,
// then walk by alternating between "outside think" and "inside
// think" states. At each pass, locate the next state-transition
// marker (full tag) — when found, emit everything before it to the
// current destination and switch state. When not found, peel a
// trailing partial-tag candidate into pendingTag and emit the rest
// to the current destination.
//
// Edge cases:
//   - Tag fully within one delta: handled by full-tag Index hits.
//   - Tag split across boundary: handled by peelPartial.
//   - Byte-by-byte deltas: pendingTag accumulates incrementally
//     until the full tag forms, then transitions atomically.
//   - Nested <think> tags: not handled. Once inThink, the state
//     machine watches only for </think>; any inner <think> stays
//     as literal bytes in the reasoning buffer, and the FIRST
//     </think> closes the outer block (orphaning any trailing
//     </think> as content). Real models don't emit nested tags,
//     so the asymmetry is operationally moot — but if a model
//     ever does, the resulting AgentResponse will look mildly
//     surprising rather than crash.
//   - Stream ending with unmatched open tag (model truncated mid-
//     think): handled by flushPending at toAgentResponse time.
func (a *streamAccumulator) routeDelta(text string) (contentDelta, reasoningDelta string) {
	if text == "" && a.pendingTag == "" {
		return "", ""
	}

	buf := a.pendingTag + text
	a.pendingTag = ""

	var (
		contentB, reasoningB strings.Builder
		i                    int
	)

	for i < len(buf) {
		if !a.inThink {
			j := strings.Index(buf[i:], openTag)
			if j < 0 {
				partial := peelPartial(buf[i:], openTag)
				safeEnd := len(buf) - len(partial)
				contentB.WriteString(buf[i:safeEnd])
				a.pendingTag = partial
				break
			}
			contentB.WriteString(buf[i : i+j])
			i += j + len(openTag)
			a.inThink = true
			continue
		}
		// inThink branch
		j := strings.Index(buf[i:], closeTag)
		if j < 0 {
			partial := peelPartial(buf[i:], closeTag)
			safeEnd := len(buf) - len(partial)
			reasoningB.WriteString(buf[i:safeEnd])
			a.pendingTag = partial
			break
		}
		reasoningB.WriteString(buf[i : i+j])
		i += j + len(closeTag)
		a.inThink = false
	}

	return contentB.String(), reasoningB.String()
}

// peelPartial returns the longest non-empty proper prefix of target
// that the input string ends with, or "" if no such prefix exists.
// Used to buffer partial tag bytes that could complete in the next
// streaming delta.
//
// Walks from longest possible prefix down to length 1; returns at
// the first match. Cap is len(target)-1 because a full match is a
// completed tag, handled separately by the caller.
func peelPartial(s, target string) string {
	maxK := len(target) - 1
	if len(s) < maxK {
		maxK = len(s)
	}
	for k := maxK; k >= 1; k-- {
		if strings.HasSuffix(s, target[:k]) {
			return target[:k]
		}
	}
	return ""
}

// flushPending drains end-of-stream state. Two separate concerns:
//
//  1. pendingTag carries bytes that could have completed a tag in
//     the next delta. When the stream ends, route them: inThink →
//     reasoning buffer; outside → content buffer. The latter case
//     is e.g. a model that emitted a literal "<thi" character
//     sequence at end of stream that wasn't actually a tag.
//
//  2. inThink at stream end means the model started a <think> block
//     and got truncated mid-reasoning. Log a warning regardless of
//     pendingTag state — the truncation itself is what operators
//     need to see, not whether bytes happened to be buffered.
func (a *streamAccumulator) flushPending() {
	if a.pendingTag != "" {
		if a.inThink {
			a.reasoning.WriteString(a.pendingTag)
		} else {
			a.content.WriteString(a.pendingTag)
		}
		a.pendingTag = ""
	}
	if a.inThink && a.logger != nil {
		a.logger.Warn("streaming response ended mid-<think>: reasoning block was not closed",
			"in_think", true)
	}
}

// inferToolIndex determines the correct index for a tool call delta.
// Delegates to the provider adapter for quirk-specific index inference.
// A return value of -1 from the adapter is a sentinel meaning "allocate
// the next available index", which is resolved here via nextToolIndex.
//
// ADR-037 chunk 6: adapter speaks wire types; translate the single SDK
// ToolCall at this seam. The wire-native streaming path (chunk 7+) will
// call NormalizeStreamDelta directly without translation.
func (a *streamAccumulator) inferToolIndex(tc openai.ToolCall) int {
	var adapter ProviderAdapter
	if a.adapter != nil {
		adapter = a.adapter
	} else {
		adapter = defaultAdapter
	}

	idx := adapter.NormalizeStreamDelta(sdkToolCallToWire(tc), a.lastToolIndex)
	if idx == -1 {
		return a.nextToolIndex()
	}
	return idx
}

// nextToolIndex returns the next available tool call index.
func (a *streamAccumulator) nextToolIndex() int {
	if len(a.toolCalls) == 0 {
		return 0
	}
	maxIdx := -1
	for idx := range a.toolCalls {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx + 1
}

// setUsage records token counts from the final stream chunk.
func (a *streamAccumulator) setUsage(usage *openai.Usage) {
	if usage == nil {
		return
	}
	a.promptTokens = usage.PromptTokens
	a.completionTokens = usage.CompletionTokens
}

// toAgentResponse builds the complete response from accumulated deltas.
func (a *streamAccumulator) toAgentResponse(requestID string) agentic.AgentResponse {
	// Drain any unfinished partial-tag bytes before building the
	// response — handles the truncation-mid-tag case where the model
	// stopped streaming with bytes still buffered in pendingTag.
	a.flushPending()

	resp := agentic.AgentResponse{
		RequestID: requestID,
		Message: agentic.ChatMessage{
			Role:             a.role,
			Content:          a.content.String(),
			ReasoningContent: a.reasoning.String(),
		},
		TokenUsage: agentic.TokenUsage{
			PromptTokens:     a.promptTokens,
			CompletionTokens: a.completionTokens,
		},
	}

	// Map finish reason to status. The "length" branch handles
	// truncation in one place: set Status, log, and bail BEFORE the
	// tool_calls conversion below — a truncated response cannot be
	// trusted to have complete tool-call arguments (the JSON may
	// have been cut mid-string, mid-escape, or mid-multi-byte UTF-8),
	// and the malformed-arguments fallback would otherwise silently
	// substitute an empty {} for unparseable args, turning a
	// "truncated" signal into a "tool with empty input" silent
	// dispatch. Beta.2 wired finish_reason=length to fail loudly for
	// content responses; this preserves that contract for the tool-
	// call path. The accumulated raw arguments live only in logs from
	// here on; the loop's truncation handler decides whether to retry
	// with compaction or fail with a diagnostic message.
	resp.FinishReason = string(a.finishReason)
	switch a.finishReason {
	case openai.FinishReasonToolCalls:
		resp.Status = agentic.StatusToolCall
	case openai.FinishReasonLength:
		resp.Status = agentic.StatusLengthTruncated
		if a.logger != nil {
			a.logger.Warn("streaming response truncated: finish_reason=length (max_tokens hit)",
				"request_id", requestID)
			if len(a.toolCalls) > 0 {
				a.logger.Warn("streaming response truncated mid-tool-call: dropping potentially-incomplete tool_calls",
					"request_id", requestID,
					"tool_calls_accumulated", len(a.toolCalls),
					"completion_tokens", a.completionTokens)
			}
		}
		return resp
	default:
		resp.Status = agentic.StatusComplete
	}

	if len(a.toolCalls) > 0 {
		resp.Status = "tool_call"

		indices := make([]int, 0, len(a.toolCalls))
		for idx := range a.toolCalls {
			indices = append(indices, idx)
		}
		sort.Ints(indices)

		toolCalls := make([]agentic.ToolCall, 0, len(indices))
		for _, idx := range indices {
			tc := a.toolCalls[idx]
			args := make(map[string]any)
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					// Malformed arguments — fall back to empty object so the
					// replay path never serializes "null" for tool_use input.
					// This branch is now reserved for the genuine "model
					// emitted broken JSON without truncation" case; the
					// truncation case bails earlier above.
					if a.logger != nil {
						a.logger.Warn("malformed tool call arguments, falling back to empty object",
							slog.String("tool_name", tc.Function.Name),
							slog.String("tool_id", tc.ID),
							slog.String("raw_arguments", tc.Function.Arguments),
							slog.String("error", err.Error()))
					}
					args = make(map[string]any)
				}
			}
			toolCalls = append(toolCalls, agentic.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
		resp.Message.ToolCalls = toolCalls
	}

	return resp
}
