package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire"
)

// useWireBackend reports whether this endpoint is configured for the
// ADR-037 wire-native client path. The wire client is eagerly built in
// NewClient when WireBackend == "wire"; this gate then dispatches to it.
func (c *Client) useWireBackend() bool {
	return c.wireClient != nil
}

// buildWireRequest builds the wire-shape ChatCompletionRequest from an
// AgentRequest, applying provider-specific normalization. Mirrors
// buildChatRequest (SDK builder) feature-for-feature; the two builders
// will converge once chunk 11 retires the SDK path.
//
// Adapter calls happen on wire types directly here — no translation
// cost at the seam, unlike the chunk-6 SDK path.
func (c *Client) buildWireRequest(req agentic.AgentRequest) wire.ChatCompletionRequest {
	if len(req.Messages) == 0 {
		if c.logger != nil {
			c.logger.Warn("buildWireRequest called with empty messages",
				slog.String("request_id", req.RequestID),
				slog.String("loop_id", req.LoopID))
		}
		return wire.ChatCompletionRequest{Model: c.endpoint.Model}
	}

	adapter := c.getAdapter()
	chatReq := wire.ChatCompletionRequest{
		Model:    c.endpoint.Model,
		Messages: adapter.NormalizeMessages(attachReasoningRecordsToWire(agenticMessagesToWire(req.Messages), req.Messages)),
	}

	c.applyWireRequestParams(&chatReq, req)
	if req.ResponseFormat != nil {
		chatReq.ResponseFormat = c.agenticResponseFormatToWire(req.ResponseFormat, req.RequestID)
	}
	if len(req.Tools) > 0 {
		chatReq.Tools = c.agenticToolsToWire(req.Tools, req.RequestID)
	}
	if req.ToolChoice != nil {
		switch req.ToolChoice.Mode {
		case "auto", "required", "none":
			if b, err := json.Marshal(req.ToolChoice.Mode); err == nil {
				chatReq.ToolChoice = b
			}
		case "function":
			tc := map[string]any{
				"type":     "function",
				"function": map[string]any{"name": req.ToolChoice.FunctionName},
			}
			if b, err := json.Marshal(tc); err == nil {
				chatReq.ToolChoice = b
			}
		}
	}

	adapter.NormalizeRequest(&chatReq)
	stripC360KeysFromRequest(&chatReq)
	return chatReq
}

// agenticMessagesToWire translates agentic.ChatMessage slice to the
// wire-shape message slice. Tool result messages preserve ToolCallID
// and Name (provider-specific adapter normalization may rewrite empty
// Names downstream).
func agenticMessagesToWire(in []agentic.ChatMessage) []wire.Message {
	out := make([]wire.Message, len(in))
	for i, msg := range in {
		out[i] = wire.Message{Role: msg.Role}
		if msg.Content != "" {
			_ = out[i].SetContentString(msg.Content)
		}
		if msg.Role == "tool" {
			out[i].ToolCallID = msg.ToolCallID
			out[i].Name = msg.Name
		}
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = agenticToolCallsToWire(msg.ToolCalls)
		}
	}
	return out
}

func agenticToolCallsToWire(in []agentic.ToolCall) []wire.ToolCall {
	out := make([]wire.ToolCall, len(in))
	for j, tc := range in {
		args := tc.Arguments
		if args == nil {
			args = make(map[string]any)
		}
		argsJSON, _ := json.Marshal(args)
		out[j] = wire.ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: wire.Function{
				Name:      tc.Name,
				Arguments: string(argsJSON),
			},
		}
	}
	return out
}

// attachReasoningRecordsToWire writes each agentic.ChatMessage's
// ReasoningRecords into the matching wire shape. Walks records once
// per message and dispatches by CarrierKind:
//
//   - ReasoningCarrierToolCall (Gemini): matches by ToolCallID and
//     writes the framework-internal carrier wireKeyC360ThoughtSignature
//     onto the wire ToolCall.Extras. GeminiAdapter.NormalizeMessages
//     reconstructs the extra_content.google.thought_signature shape
//     downstream; buildWireRequest's hygiene step strips any remaining
//     framework-internal keys before send.
//   - Other CarrierKinds: no-op at this layer (OpenAI Responses uses
//     a different wire shape entirely — handled by the Responses
//     client per ADR-051 D4, not by ChatCompletion translation).
//
// in is the wire-shape slice produced by agenticMessagesToWire (same
// length, same order); src is the original agentic slice carrying
// ReasoningRecords. Returns in for fluent chaining.
func attachReasoningRecordsToWire(in []wire.Message, src []agentic.ChatMessage) []wire.Message {
	for i := range src {
		if i >= len(in) {
			break
		}
		for _, rec := range src[i].ReasoningRecords {
			if rec.CarrierKind != agentic.ReasoningCarrierToolCall {
				continue
			}
			if rec.ToolCallID == "" || len(rec.Opaque) == 0 {
				continue
			}
			for j := range in[i].ToolCalls {
				if in[i].ToolCalls[j].ID != rec.ToolCallID {
					continue
				}
				b, err := json.Marshal(string(rec.Opaque))
				if err != nil {
					continue
				}
				if in[i].ToolCalls[j].Extras == nil {
					in[i].ToolCalls[j].Extras = make(map[string]json.RawMessage, 1)
				}
				in[i].ToolCalls[j].Extras[wireKeyC360ThoughtSignature] = b
				break
			}
		}
	}
	return in
}

// wireKeyC360ThoughtSignature is the framework-internal carrier key on
// wire.ToolCall.Extras that bridges agentic.ToolCall.Metadata across
// the wire layer until the provider adapter reshapes it.
const wireKeyC360ThoughtSignature = "c360_thought_signature"

// wireKeyExtraContent is the Gemini-shape key for the per-tool_call
// extra fields, including google.thought_signature.
const wireKeyExtraContent = "extra_content"

// agenticToolsToWire translates the agentic tool definitions to wire
// shape. A marshal failure on tool.Parameters (caller authored an
// unmarshalable type — channel, func, cyclic map) is surfaced via Warn
// so callers see "tool schema dropped" in operator logs instead of an
// upstream "missing parameters" 400. Parity with the SDK-side
// applyResponseFormat Warn (beta.48 lesson).
func (c *Client) agenticToolsToWire(in []agentic.ToolDefinition, requestID string) []wire.Tool {
	out := make([]wire.Tool, len(in))
	for i, tool := range in {
		params, err := json.Marshal(tool.Parameters)
		if err != nil && c.logger != nil {
			c.logger.Warn("tool parameters failed to marshal; upstream will reject with a generic schema error",
				slog.String("request_id", requestID),
				slog.String("tool", tool.Name),
				slog.Any("err", err))
		}
		out[i] = wire.Tool{
			Type: "function",
			Function: wire.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
				Strict:      tool.Strict,
			},
		}
	}
	return out
}

// applyWireRequestParams writes the simple top-level params from the
// AgentRequest + endpoint config onto the wire request. Separates the
// scalar-field plumbing from buildWireRequest's structural translation
// so each function stays under the revive function-length cap.
func (c *Client) applyWireRequestParams(chatReq *wire.ChatCompletionRequest, req agentic.AgentRequest) {
	switch {
	case req.MaxTokens > 0:
		chatReq.MaxTokens = req.MaxTokens
	case c.endpoint.MaxOutputTokens > 0:
		chatReq.MaxTokens = c.endpoint.MaxOutputTokens
	}
	if req.Temperature > 0 {
		t := req.Temperature
		chatReq.Temperature = &t
	}
	if c.endpoint.ReasoningEffort != "" {
		chatReq.ReasoningEffort = c.endpoint.ReasoningEffort
	}
	if len(c.endpoint.Options) > 0 {
		if b, err := json.Marshal(c.endpoint.Options); err == nil {
			if chatReq.Extras == nil {
				chatReq.Extras = make(map[string]json.RawMessage, 1)
			}
			chatReq.Extras["chat_template_kwargs"] = b
		}
	}
}

// agenticResponseFormatToWire translates agentic.ResponseFormat to the
// wire shape. Mirrors toOpenAIResponseFormat (SDK side); schema bytes
// flow as json.RawMessage in both directions. A marshal failure on
// rf.Schema is surfaced via Warn — silent drop here would surface as
// an upstream generic 400 with "schema must be object" instead of the
// real root cause (caller's schema map contains an unmarshalable
// value). Parity with the SDK-side applyResponseFormat Warn.
func (c *Client) agenticResponseFormatToWire(rf *agentic.ResponseFormat, requestID string) *wire.ResponseFormat {
	if rf == nil {
		return nil
	}
	out := &wire.ResponseFormat{Type: rf.Type}
	if rf.Type != agentic.ResponseFormatJSONSchema {
		return out
	}
	out.JSONSchema = &wire.JSONSchema{
		Name:   rf.Name,
		Strict: rf.Strict,
	}
	if len(rf.Schema) > 0 {
		b, err := json.Marshal(rf.Schema)
		switch {
		case err != nil:
			if c.logger != nil {
				c.logger.Warn("response_format schema failed to marshal; upstream will reject with a generic schema error",
					slog.String("request_id", requestID),
					slog.String("name", rf.Name),
					slog.Any("err", err))
			}
		default:
			out.JSONSchema.Schema = b
		}
	}
	return out
}

// convertWireResponse translates a wire response into an agentic
// AgentResponse. Mirrors convertResponse (SDK side); the inline-think
// extraction is shared via extractThinkBlocksFromWireMessage.
func (c *Client) convertWireResponse(resp *wire.ChatCompletionResponse, requestID string) agentic.AgentResponse {
	c.getAdapter().NormalizeResponse(resp)

	response := agentic.AgentResponse{RequestID: requestID}
	if resp == nil || len(resp.Choices) == 0 {
		response.Status = "error"
		response.Error = "no choices in response"
		return response
	}

	choice := resp.Choices[0]
	if choice.Message == nil {
		response.Status = "error"
		response.Error = "no message in choice"
		return response
	}

	extractThinkBlocksFromWireMessage(choice.Message)

	response.FinishReason = choice.FinishReason
	switch choice.FinishReason {
	case "stop":
		response.Status = agentic.StatusComplete
	case "length":
		response.Status = agentic.StatusLengthTruncated
		if c.logger != nil {
			c.logger.Warn("model response truncated: finish_reason=length (max_tokens hit)",
				"request_id", requestID)
		}
	case "tool_calls":
		response.Status = agentic.StatusToolCall
	default:
		response.Status = agentic.StatusComplete
	}

	contentStr, _ := choice.Message.ContentString()
	response.Message = agentic.ChatMessage{
		Role:             choice.Message.Role,
		Content:          contentStr,
		ReasoningContent: choice.Message.ReasoningContent,
	}

	// Tool calls — drop on truncation per beta.2 contract.
	if choice.FinishReason == "length" {
		if len(choice.Message.ToolCalls) > 0 && c.logger != nil {
			completion := 0
			if resp.Usage != nil {
				completion = resp.Usage.CompletionTokens
			}
			c.logger.Warn("response truncated mid-tool-call: dropping potentially-incomplete tool_calls",
				"request_id", requestID,
				"tool_calls_accumulated", len(choice.Message.ToolCalls),
				"completion_tokens", completion)
		}
	} else if len(choice.Message.ToolCalls) > 0 {
		response.Status = "tool_call"
		toolCalls := make([]agentic.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			args := make(map[string]any)
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					if c.logger != nil {
						c.logger.Warn("malformed tool_call arguments, using empty object",
							"tool", tc.Function.Name, "error", err)
					}
					args = make(map[string]any)
				}
			}
			toolCalls[i] = agentic.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			}
			if sig := readC360ThoughtSignature(tc); sig != "" {
				response.Message.ReasoningRecords = append(response.Message.ReasoningRecords, agentic.ReasoningRecord{
					Provider:    "google",
					CarrierKind: agentic.ReasoningCarrierToolCall,
					ToolCallID:  tc.ID,
					Opaque:      []byte(sig),
				})
			}
		}
		response.Message.ToolCalls = toolCalls
	}

	if resp.Usage != nil {
		response.TokenUsage = agentic.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}
	}

	stripC360KeysFromResponse(resp)
	return response
}

// doSingleAttemptWire is the wire-native equivalent of doSingleAttempt.
// Returns (response, nil) on success or (zero, error) on failure.
// c.wireClient is eagerly built in NewClient — no nil check needed
// because useWireBackend gates the dispatch on a non-nil c.wireClient.
func (c *Client) doSingleAttemptWire(ctx context.Context, chatReq wire.ChatCompletionRequest, requestID string) (agentic.AgentResponse, error) {
	wc := c.wireClient

	if c.endpoint.Stream {
		resp, err := c.streamChatCompletionWire(ctx, wc, chatReq, requestID)
		if err != nil {
			if c.logger != nil {
				c.logger.Debug("wire stream connection failed",
					slog.String("request_id", requestID),
					slog.String("model", chatReq.Model),
					slog.String("error", err.Error()))
			}
			return agentic.AgentResponse{}, err
		}
		return resp, nil
	}

	resp, err := wc.ChatCompletion(ctx, &chatReq)
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("wire API request failed",
				slog.String("request_id", requestID),
				slog.String("model", chatReq.Model),
				slog.String("error", err.Error()))
		}
		return agentic.AgentResponse{}, err
	}
	return c.convertWireResponse(resp, requestID), nil
}

// streamChatCompletionWire handles the wire-native streaming path.
// Connection errors return a Go error (retryable); mid-stream errors
// return AgentResponse{Status:"error"} (not retryable).
func (c *Client) streamChatCompletionWire(ctx context.Context, wc *wire.Client, chatReq wire.ChatCompletionRequest, requestID string) (agentic.AgentResponse, error) {
	chatReq.Stream = true
	chatReq.StreamOptions = &wire.StreamOptions{IncludeUsage: true}

	stream, err := wc.ChatCompletionStream(ctx, &chatReq)
	if err != nil {
		return agentic.AgentResponse{}, err
	}
	defer stream.Close()

	acc := newWireStreamAccumulator(c.getAdapter(), c.logger, c.chunkHandler)
	streamStart := time.Now()
	firstTokenRecorded := false

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			resp := acc.toAgentResponse(requestID)
			resp.Status = "error"
			resp.Error = err.Error()
			return resp, nil
		}

		contentDelta, reasoningDelta := acc.processChunk(chunk, requestID)
		// Metric parity with the SDK streaming path: record one chunk per
		// choice unconditionally; TTFT fires on the first non-empty delta
		// (content or reasoning), matching SDK behaviour at client.go:617.
		if c.metrics != nil {
			c.metrics.recordStreamChunk(chatReq.Model)
			if !firstTokenRecorded && (contentDelta != "" || reasoningDelta != "") {
				c.metrics.recordStreamTTFT(chatReq.Model, time.Since(streamStart).Seconds())
				firstTokenRecorded = true
			}
		}
	}

	if c.chunkHandler != nil {
		c.chunkHandler(StreamChunk{RequestID: requestID, Done: true})
	}

	return acc.toAgentResponse(requestID), nil
}

// chatCompletionWire is the wire-native equivalent of ChatCompletion.
// Shares the retry / throttle / error-classification scaffolding with
// the SDK path via runRetryLoop; only the per-attempt dispatch differs.
func (c *Client) chatCompletionWire(ctx context.Context, req agentic.AgentRequest) (agentic.AgentResponse, error) {
	c.runProviderStartupProbes(ctx)
	chatReq := c.buildWireRequest(req)
	c.debugLogRequest(req.RequestID, chatReq.Model, len(chatReq.Messages), &chatReq)
	if c.throttle != nil {
		if err := c.throttle.Acquire(ctx); err != nil {
			return errorResponse(req.RequestID, err.Error()), nil
		}
		defer c.throttle.Release()
	}
	return c.runRetryLoop(ctx, req.RequestID, chatReq.Model, func() (agentic.AgentResponse, error) {
		return c.doSingleAttemptWire(ctx, chatReq, req.RequestID)
	})
}

// isRetryableAny is the backend-agnostic version of isRetryable —
// recognizes wire.APIError as well as openai.APIError.
func isRetryableAny(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var wireErr *wire.APIError
	if errors.As(err, &wireErr) {
		return isRetryableStatusCode(wireErr.StatusCode)
	}
	return isRetryable(err)
}

// isRateLimitedAny is the backend-agnostic version of isRateLimited.
func isRateLimitedAny(err error) bool {
	var wireErr *wire.APIError
	if errors.As(err, &wireErr) && wireErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return isRateLimited(err)
}

// readC360ThoughtSignature reads the framework-internal carrier key
// from a wire.ToolCall.Extras. Returns "" if absent or non-string.
// Called by convertWireResponse to lift the signature into
// Message.ReasoningRecords (ADR-051) for cross-turn propagation — NOT
// ToolCall.Metadata. The model must never reach ToolCall.Metadata; it
// carries framework enforcement facts (e.g. the read-only exec policy,
// ADR-067) that dispatch stamps authoritatively.
func readC360ThoughtSignature(tc wire.ToolCall) string {
	raw, ok := tc.Extras[wireKeyC360ThoughtSignature]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// stripC360KeysFromRequest drops any framework-internal carrier keys
// from a wire request that the adapter chain didn't consume. Defense
// in depth: if a Gemini-sourced thought_signature flows into a
// non-Gemini endpoint (e.g. cross-model fallback), the carrier is
// silently stripped rather than escaping into the request body.
func stripC360KeysFromRequest(req *wire.ChatCompletionRequest) {
	if req == nil {
		return
	}
	for i := range req.Messages {
		for j := range req.Messages[i].ToolCalls {
			delete(req.Messages[i].ToolCalls[j].Extras, wireKeyC360ThoughtSignature)
		}
	}
}

// stripC360KeysFromResponse removes the framework-internal carrier key
// from a wire response after convertWireResponse has lifted its values
// into Message.ReasoningRecords (ADR-051). Keeps the response object clean
// for downstream trace exports / debug logs — the canonical representation
// of a processed Gemini response is "no extra_content, no carrier; signature
// lives only in Message.ReasoningRecords."
func stripC360KeysFromResponse(resp *wire.ChatCompletionResponse) {
	if resp == nil {
		return
	}
	for ci := range resp.Choices {
		msg := resp.Choices[ci].Message
		if msg == nil {
			continue
		}
		for ti := range msg.ToolCalls {
			delete(msg.ToolCalls[ti].Extras, wireKeyC360ThoughtSignature)
		}
	}
}
