package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/model/wire"
)

// useWireBackend reports whether this endpoint is configured for the
// ADR-037 wire-native client path.
func (c *Client) useWireBackend() bool {
	return c.endpoint != nil && c.endpoint.WireBackend == "wire"
}

// wireClientOnce builds a *wire.Client lazily for the endpoint, reusing
// it across requests. Returns nil if the endpoint is misconfigured;
// caller falls back to the SDK path.
func (c *Client) ensureWireClient() (*wire.Client, error) {
	if c.wireClient != nil {
		return c.wireClient, nil
	}
	if c.endpoint == nil {
		return nil, errors.New("agentic-model: wire backend requires endpoint config")
	}
	apiKey := ""
	if c.endpoint.APIKeyEnv != "" {
		apiKey = os.Getenv(c.endpoint.APIKeyEnv)
	}
	httpClient := c.httpClientOrDefault()
	authHeader := ""
	if apiKey != "" {
		authHeader = "Bearer " + apiKey
	}
	cfg := wire.ClientConfig{
		BaseURL:    c.endpoint.URL,
		HTTPClient: httpClient,
		AuthHeader: authHeader,
	}
	wc, err := wire.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("agentic-model: build wire client: %w", err)
	}
	c.wireClient = wc
	return wc, nil
}

// httpClientOrDefault returns the same HTTP client used by the SDK path
// for this endpoint. Both backends consume model.NewHTTPClient — the
// connection-hygiene invariant from beta.43/47 holds for both.
func (c *Client) httpClientOrDefault() *http.Client {
	return model.NewHTTPClient(model.HTTPClientOptionsFromEndpoint(c.endpoint))
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
		Messages: adapter.NormalizeMessages(agenticMessagesToWire(req.Messages)),
	}

	c.applyWireRequestParams(&chatReq, req)
	if req.ResponseFormat != nil {
		chatReq.ResponseFormat = agenticResponseFormatToWire(req.ResponseFormat)
	}
	if len(req.Tools) > 0 {
		chatReq.Tools = agenticToolsToWire(req.Tools)
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

func agenticToolsToWire(in []agentic.ToolDefinition) []wire.Tool {
	out := make([]wire.Tool, len(in))
	for i, tool := range in {
		params, _ := json.Marshal(tool.Parameters)
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
// flow as json.RawMessage in both directions.
func agenticResponseFormatToWire(rf *agentic.ResponseFormat) *wire.ResponseFormat {
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
		if b, err := json.Marshal(rf.Schema); err == nil {
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
		}
		response.Message.ToolCalls = toolCalls
	}

	if resp.Usage != nil {
		response.TokenUsage = agentic.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}
	}

	return response
}

// doSingleAttemptWire is the wire-native equivalent of doSingleAttempt.
// Returns (response, nil) on success or (zero, error) on failure.
func (c *Client) doSingleAttemptWire(ctx context.Context, chatReq wire.ChatCompletionRequest, requestID string) (agentic.AgentResponse, error) {
	wc, err := c.ensureWireClient()
	if err != nil {
		return agentic.AgentResponse{}, err
	}

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
		if c.metrics != nil && (contentDelta != "" || reasoningDelta != "") {
			c.metrics.recordStreamChunk(chatReq.Model)
			if !firstTokenRecorded {
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
