package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// useResponsesBackend reports whether this endpoint is configured for
// the ADR-051 Responses-native client path. The Responses client is
// eagerly built in NewClient when WireBackend == "responses"; this
// gate then dispatches to it.
func (c *Client) useResponsesBackend() bool {
	return c.responsesClient != nil
}

// chatCompletionResponses is the Responses-native equivalent of
// ChatCompletion. Shares the retry / throttle / error-classification
// scaffolding with the SDK and wire-ChatCompletion paths via
// runRetryLoop; only the per-attempt dispatch and the request /
// response translation differ.
func (c *Client) chatCompletionResponses(ctx context.Context, req agentic.AgentRequest) (agentic.AgentResponse, error) {
	c.runProviderStartupProbes(ctx)
	respReq := c.buildResponsesRequest(req)
	c.debugLogRequest(req.RequestID, respReq.Model, len(respReq.Input), &respReq)
	c.recordResponsesRequestBytes(respReq.Model, &respReq)
	if c.throttle != nil {
		if err := c.throttle.Acquire(ctx); err != nil {
			return errorResponse(req.RequestID, err.Error()), nil
		}
		defer c.throttle.Release()
	}
	return c.runRetryLoop(ctx, req.RequestID, respReq.Model, func() (agentic.AgentResponse, error) {
		return c.doSingleAttemptResponses(ctx, respReq, req.RequestID)
	})
}

// doSingleAttemptResponses executes one Responses request attempt
// (streaming or non-streaming). Returns (response, nil) on success
// or (zero, error) on failure.
func (c *Client) doSingleAttemptResponses(ctx context.Context, respReq responses.Request, requestID string) (agentic.AgentResponse, error) {
	rc := c.responsesClient
	if c.endpoint.Stream {
		resp, err := c.streamResponses(ctx, rc, respReq, requestID)
		if err != nil {
			if c.logger != nil {
				c.logger.Debug("responses stream connection failed",
					slog.String("request_id", requestID),
					slog.String("model", respReq.Model),
					slog.String("error", err.Error()))
			}
			return agentic.AgentResponse{}, err
		}
		return resp, nil
	}

	resp, err := rc.Responses(ctx, &respReq)
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("responses API request failed",
				slog.String("request_id", requestID),
				slog.String("model", respReq.Model),
				slog.String("error", err.Error()))
		}
		return agentic.AgentResponse{}, err
	}
	return c.convertResponsesResponse(resp, requestID), nil
}

// convertResponsesResponse translates a responses.Response into an
// agentic AgentResponse. The Responses output is a heterogeneous
// typed array; we walk it once, building the assistant message's
// content/tool-calls/reasoning-records as we go.
//
// Status mapping (Responses → agentic):
//   - "completed" + tool calls present → "tool_call"
//   - "completed" + only text/reasoning → "complete"
//   - "incomplete" with reason="max_output_tokens" → "length_truncated"
//   - "failed"/"cancelled"/other → "error"
func (c *Client) convertResponsesResponse(resp *responses.Response, requestID string) agentic.AgentResponse {
	response := agentic.AgentResponse{RequestID: requestID}
	if resp == nil {
		response.Status = "error"
		response.Error = "no response from API"
		return response
	}

	response.FinishReason = resp.Status

	switch resp.Status {
	case "failed", "cancelled":
		response.Status = "error"
		response.Error = "responses status: " + resp.Status
		return response
	case "incomplete":
		response.Status = agentic.StatusLengthTruncated
		if c.logger != nil {
			c.logger.Warn("responses incomplete",
				"request_id", requestID,
				"details", string(resp.IncompleteDetails))
		}
	default:
		response.Status = agentic.StatusComplete
	}

	msg := agentic.ChatMessage{Role: "assistant"}
	var toolCalls []agentic.ToolCall
	for i := range resp.Output {
		item := &resp.Output[i]
		switch {
		case item.IsMessage():
			msg.Content += item.OutputText()
		case item.IsFunctionCall():
			args := make(map[string]any)
			if item.Arguments != "" {
				if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
					if c.logger != nil {
						c.logger.Warn("malformed responses function_call arguments, using empty object",
							"item_id", item.ID, "error", err)
					}
					args = make(map[string]any)
				}
			}
			toolCalls = append(toolCalls, agentic.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: args,
			})
		case item.IsReasoning():
			// Reasoning capture lives on the adapter so future
			// providers (Anthropic-shape, etc.) can swap in.
			// The default OpenAIResponsesAdapter handles the
			// standalone-item shape.
		}
	}

	msg.ToolCalls = toolCalls
	msg.ReasoningRecords = c.getResponsesAdapter().CaptureReasoningRecords(resp)
	response.Message = msg
	if len(toolCalls) > 0 && response.Status == agentic.StatusComplete {
		response.Status = agentic.StatusToolCall
	}
	if resp.Usage != nil {
		response.TokenUsage = agentic.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
		}
	}
	return response
}

// recordResponsesRequestBytes emits the per-request bytes-on-wire
// metric for operator observability of the stateless-echo cost
// (ADR-051 D2 cost-observability gate). Best-effort: failure to
// marshal silently skips the metric so a metrics issue doesn't
// kill the request.
func (c *Client) recordResponsesRequestBytes(model string, req *responses.Request) {
	if c.metrics == nil {
		return
	}
	b, err := json.Marshal(req)
	if err != nil {
		return
	}
	c.metrics.recordResponsesRequestBytes(model, float64(len(b)))
}

// isResponsesRetryableAny is the Responses-aware variant of
// isRetryableAny. Recognizes responses.APIError (which is a type
// alias for wire.APIError) and the same network classifications.
// The retry loop already handles wire.APIError via isRetryableAny;
// since responses.APIError = wire.APIError, no extra dispatch is
// needed — the existing classifier already covers both. This
// function exists as a documentation seam.
func isResponsesRetryableAny(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *wire.APIError
	if errors.As(err, &apiErr) {
		return isRetryableStatusCode(apiErr.StatusCode)
	}
	return isRetryable(err)
}

// isResponsesRateLimitedAny matches isRateLimitedAny for the
// Responses path.
func isResponsesRateLimitedAny(err error) bool {
	var apiErr *wire.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return isRateLimited(err)
}
