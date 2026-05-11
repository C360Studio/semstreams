package agenticmodel

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire"
)

// extractThinkBlocksFromWireMessage is the wire-typed analog of
// extractThinkBlocksFromResponse. Inline <think>...</think> blocks
// (qwen3 / deepseek-r1 family) are routed out of message content into
// ReasoningContent so the AgentResponse shape is symmetric with the
// channel-based reasoning_content path.
func extractThinkBlocksFromWireMessage(m *wire.Message) {
	if m == nil {
		return
	}
	contentStr, ok := m.ContentString()
	if !ok || contentStr == "" {
		return
	}
	cleaned, extracted := extractThinkBlocks(contentStr)
	if extracted == "" {
		return
	}
	_ = m.SetContentString(cleaned)
	if m.ReasoningContent != "" {
		m.ReasoningContent += "\n" + extracted
	} else {
		m.ReasoningContent = extracted
	}
}

// wireStreamAccumulator merges wire streaming chunks into a final
// agentic.AgentResponse. Wraps wire.Accumulator with the agentic-side
// concerns the wire layer is intentionally structural-only about:
//
//   - Inline-think state machine (qwen3 / deepseek-r1 family), driven
//     per-delta via the same routeDelta logic the SDK path uses.
//   - Adapter NormalizeStreamDelta dispatch for index inference,
//     translating wire ToolCall to the adapter's wire-shape contract.
//   - Chunk handler callbacks (live-monitoring stream).
//   - Final AgentResponse construction with finish_reason mapping.
//
// The wire.Accumulator is not reused here because its mergeToolCall is
// adapter-agnostic; the adapter-driven path matches the SDK accumulator's
// quirk handling exactly.
type wireStreamAccumulator struct {
	content       strings.Builder
	reasoning     strings.Builder
	role          string
	finishReason  string
	toolCalls     map[int]*wire.ToolCall
	promptTokens  int
	completion    int
	lastToolIndex int
	adapter       ProviderAdapter
	logger        *slog.Logger
	chunkHandler  ChunkHandler

	inThink    bool
	pendingTag string
}

func newWireStreamAccumulator(adapter ProviderAdapter, logger *slog.Logger, handler ChunkHandler) *wireStreamAccumulator {
	return &wireStreamAccumulator{
		adapter:      adapter,
		logger:       logger,
		chunkHandler: handler,
	}
}

// processChunk folds one stream chunk into the accumulator and returns
// the (content, reasoning) deltas routed through the inline-think
// state machine. If a chunk handler is registered, the deltas are
// dispatched in the same call to keep ordering deterministic.
func (a *wireStreamAccumulator) processChunk(chunk *wire.StreamChunk, requestID string) (contentDelta, reasoningDelta string) {
	if chunk == nil {
		return "", ""
	}
	if chunk.Usage != nil {
		a.promptTokens = chunk.Usage.PromptTokens
		a.completion = chunk.Usage.CompletionTokens
	}
	if len(chunk.Choices) == 0 {
		return "", ""
	}

	choice := chunk.Choices[0]
	if choice.FinishReason != "" {
		a.finishReason = choice.FinishReason
	}
	delta := choice.Delta
	if delta == nil {
		return "", ""
	}

	if delta.Role != "" {
		a.role = delta.Role
	}

	if s, ok := delta.ContentString(); ok && s != "" {
		contentDelta, reasoningDelta = a.routeDelta(s)
		if contentDelta != "" {
			a.content.WriteString(contentDelta)
		}
		if reasoningDelta != "" {
			a.reasoning.WriteString(reasoningDelta)
		}
	}

	if delta.ReasoningContent != "" {
		a.reasoning.WriteString(delta.ReasoningContent)
		reasoningDelta += delta.ReasoningContent
	}

	for _, tc := range delta.ToolCalls {
		idx := a.inferToolIndex(tc)
		if a.toolCalls == nil {
			a.toolCalls = make(map[int]*wire.ToolCall)
		}
		existing, ok := a.toolCalls[idx]
		if !ok {
			existing = &wire.ToolCall{Type: "function"}
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

	// Mirror the SDK streamAccumulator: fire the chunkHandler per choice
	// unconditionally so consumers see one tick per delta, even when the
	// delta contained only tool_call updates (empty content + reasoning).
	if a.chunkHandler != nil {
		a.chunkHandler(StreamChunk{
			RequestID:      requestID,
			ContentDelta:   contentDelta,
			ReasoningDelta: reasoningDelta,
		})
	}

	return contentDelta, reasoningDelta
}

// routeDelta drives the inline-think state machine. Identical algorithm
// to streamAccumulator.routeDelta — duplicated here to keep the wire
// path free of SDK type dependencies. Will converge with the SDK
// version when chunk 11 retires the SDK path.
func (a *wireStreamAccumulator) routeDelta(text string) (contentDelta, reasoningDelta string) {
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

// flushPending drains pendingTag at stream end. Mirrors streamAccumulator's
// flushPending; see comments there.
func (a *wireStreamAccumulator) flushPending() {
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

// inferToolIndex delegates to the adapter for index inference. -1 means
// "allocate the next available index"; resolved via nextToolIndex.
func (a *wireStreamAccumulator) inferToolIndex(tc wire.ToolCall) int {
	adapter := a.adapter
	if adapter == nil {
		adapter = defaultAdapter
	}
	idx := adapter.NormalizeStreamDelta(tc, a.lastToolIndex)
	if idx == -1 {
		return a.nextToolIndex()
	}
	return idx
}

func (a *wireStreamAccumulator) nextToolIndex() int {
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

// toAgentResponse builds the complete AgentResponse from accumulated
// deltas. Mirrors streamAccumulator.toAgentResponse: drops tool_calls
// on length-truncation (beta.2 contract), falls back to {} on malformed
// tool_call arguments.
func (a *wireStreamAccumulator) toAgentResponse(requestID string) agentic.AgentResponse {
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
			CompletionTokens: a.completion,
		},
	}

	resp.FinishReason = a.finishReason
	switch a.finishReason {
	case "tool_calls":
		resp.Status = agentic.StatusToolCall
	case "length":
		resp.Status = agentic.StatusLengthTruncated
		if a.logger != nil {
			a.logger.Warn("streaming response truncated: finish_reason=length (max_tokens hit)",
				"request_id", requestID)
			if len(a.toolCalls) > 0 {
				a.logger.Warn("streaming response truncated mid-tool-call: dropping potentially-incomplete tool_calls",
					"request_id", requestID,
					"tool_calls_accumulated", len(a.toolCalls),
					"completion_tokens", a.completion)
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
		sortInts(indices)

		// Synthesize a wire response and run adapter.NormalizeResponse so
		// the streaming path matches non-streaming (convertWireResponse)
		// for provider-blob extraction — Gemini 3.x thought_signature
		// echo on multi-turn flows depends on this hook running on the
		// streaming path too. Without it, the carrier never lands on
		// agentic.ToolCall.Metadata.
		synth := a.synthesizeWireResponse(indices)
		adapter := a.adapter
		if adapter == nil {
			adapter = defaultAdapter
		}
		adapter.NormalizeResponse(synth)

		toolCalls := make([]agentic.ToolCall, 0, len(indices))
		for i, idx := range indices {
			tc := a.toolCalls[idx]
			args := make(map[string]any)
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
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
			out := agentic.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			}
			// Read the carrier off the post-NormalizeResponse wire shape
			// (synth.Choices[0].Message.ToolCalls[i] mirrors the SDK-side
			// extraction in convertWireResponse).
			if sig := readC360ThoughtSignature(synth.Choices[0].Message.ToolCalls[i]); sig != "" {
				if out.Metadata == nil {
					out.Metadata = make(map[string]any, 1)
				}
				out.Metadata[agentic.MetadataKeyGoogleThoughtSignature] = sig
			}
			toolCalls = append(toolCalls, out)
		}
		resp.Message.ToolCalls = toolCalls
	}

	return resp
}

// synthesizeWireResponse builds a wire.ChatCompletionResponse covering
// the tool_calls the accumulator collected, in index-sorted order.
// Returned as a single Choice with the accumulator's Message snapshot —
// enough surface for adapter NormalizeResponse to walk tool_call Extras
// and write framework-internal carriers. Indices argument is the
// already-sorted slice so we can iterate deterministically.
func (a *wireStreamAccumulator) synthesizeWireResponse(indices []int) *wire.ChatCompletionResponse {
	msg := &wire.Message{Role: a.role}
	if a.content.Len() > 0 {
		_ = msg.SetContentString(a.content.String())
	}
	if a.reasoning.Len() > 0 {
		msg.ReasoningContent = a.reasoning.String()
	}
	if len(indices) > 0 {
		msg.ToolCalls = make([]wire.ToolCall, len(indices))
		for i, idx := range indices {
			msg.ToolCalls[i] = *a.toolCalls[idx]
		}
	}
	return &wire.ChatCompletionResponse{
		Choices: []wire.Choice{{Message: msg, FinishReason: a.finishReason}},
	}
}

// sortInts is a tiny ascending-sort helper that keeps stream_wire.go
// free of an extra import for a single use site.
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
