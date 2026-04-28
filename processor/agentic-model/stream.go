package agenticmodel

import (
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
type ChunkHandler func(chunk StreamChunk)

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
}

// processDelta incorporates a single streaming choice delta.
func (a *streamAccumulator) processDelta(choice openai.ChatCompletionStreamChoice) {
	delta := choice.Delta

	if delta.Role != "" {
		a.role = delta.Role
	}

	if delta.Content != "" {
		a.content.WriteString(delta.Content)
	}

	if delta.ReasoningContent != "" {
		a.reasoning.WriteString(delta.ReasoningContent)
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
}

// inferToolIndex determines the correct index for a tool call delta.
// Delegates to the provider adapter for quirk-specific index inference.
// A return value of -1 from the adapter is a sentinel meaning "allocate
// the next available index", which is resolved here via nextToolIndex.
func (a *streamAccumulator) inferToolIndex(tc openai.ToolCall) int {
	var adapter ProviderAdapter
	if a.adapter != nil {
		adapter = a.adapter
	} else {
		adapter = defaultAdapter
	}

	idx := adapter.NormalizeStreamDelta(tc, a.lastToolIndex)
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
	max := -1
	for idx := range a.toolCalls {
		if idx > max {
			max = idx
		}
	}
	return max + 1
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
