package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/model/wire"
	"github.com/c360studio/semstreams/pkg/errs"
	openai "github.com/sashabaranov/go-openai"
)

// Client wraps OpenAI SDK for agentic model requests
type Client struct {
	client       *openai.Client
	wireClient   *wire.Client // populated lazily when endpoint.WireBackend = "wire" (ADR-037)
	endpoint     *model.EndpointConfig
	chunkHandler ChunkHandler
	metrics      *modelMetrics
	logger       *slog.Logger
	throttle     *EndpointThrottle
	retryCfg     RetryConfig
	adapter      ProviderAdapter
	// ollamaProbeOnce guards a lazy /api/show probe that warns when the
	// model is running with a num_ctx smaller than endpoint.MaxTokens —
	// Ollama's /v1/ compat layer can't override it at request time, so the
	// trap is silent prompt truncation. See processor/agentic-model/ollama_probe.go.
	ollamaProbeOnce sync.Once
}

// defaultClientRetryConfig is the retry configuration used when the component
// does not supply one. The short initial delay keeps unit tests fast.
var defaultClientRetryConfig = RetryConfig{
	MaxAttempts:         3,
	MaxRateLimitRetries: 5,
	Backoff:             "exponential",
	InitialDelay:        "100ms",
	MaxDelay:            "60s",
	RateLimitDelay:      "15s",
}

// NewClient creates a new client for the given endpoint configuration.
// The default retry config is suitable for unit tests (3 attempts, 100ms initial delay).
// Call SetRetryConfig before ChatCompletion to apply production settings.
func NewClient(endpoint *model.EndpointConfig) (*Client, error) {
	if endpoint == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Client", "NewClient", "endpoint is nil")
	}
	if endpoint.Model == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Client", "NewClient", "check model")
	}

	// Get API key from environment if specified
	apiKey := ""
	if endpoint.APIKeyEnv != "" {
		apiKey = os.Getenv(endpoint.APIKeyEnv)
	}

	// Create OpenAI client config
	config := openai.DefaultConfig(apiKey)
	if endpoint.URL != "" {
		config.BaseURL = endpoint.URL
	}
	// Replace the SDK's default &http.Client{} with one whose Transport
	// honours the EndpointConfig connection-hygiene fields. Empty/zero
	// fields preserve Go's net/http defaults (backward compatible).
	// See model/httpclient.go and operations/09-openai-client-keepalive.md
	// for the failure modes this addresses.
	httpClient := model.NewHTTPClient(model.HTTPClientOptionsFromEndpoint(endpoint))
	config.HTTPClient = httpClient

	client := openai.NewClientWithConfig(config)

	out := &Client{
		client:   client,
		endpoint: endpoint,
		retryCfg: defaultClientRetryConfig,
	}

	// ADR-037: eager wire client construction when the endpoint opts in.
	// Building lazily under sync.Mutex was the alternative; eager is
	// simpler and removes the data race on c.wireClient writes between
	// concurrent ChatCompletion calls (go-reviewer M1).
	if endpoint.WireBackend == "wire" {
		wc, err := buildWireClientForEndpoint(endpoint, httpClient)
		if err != nil {
			return nil, err
		}
		out.wireClient = wc
	}

	return out, nil
}

// buildWireClientForEndpoint constructs a *wire.Client for the given
// endpoint, sharing the same HTTP transport as the SDK client (the
// model.NewHTTPClient invariant from beta.43/47). Pulled out of
// ensureWireClient (now retired) so construction happens once at
// NewClient time, eliminating the concurrent-write race.
func buildWireClientForEndpoint(endpoint *model.EndpointConfig, httpClient *http.Client) (*wire.Client, error) {
	apiKey := ""
	if endpoint.APIKeyEnv != "" {
		apiKey = os.Getenv(endpoint.APIKeyEnv)
	}
	authHeader := ""
	if apiKey != "" {
		authHeader = "Bearer " + apiKey
	}
	wc, err := wire.NewClient(wire.ClientConfig{
		BaseURL:    endpoint.URL,
		HTTPClient: httpClient,
		AuthHeader: authHeader,
	})
	if err != nil {
		return nil, fmt.Errorf("agentic-model: build wire client: %w", err)
	}
	return wc, nil
}

// SetChunkHandler sets the callback for receiving streaming deltas.
func (c *Client) SetChunkHandler(handler ChunkHandler) {
	c.chunkHandler = handler
}

// SetMetrics sets the metrics instance for recording streaming metrics.
func (c *Client) SetMetrics(m *modelMetrics) {
	c.metrics = m
}

// SetLogger sets the logger for debug-level request/response logging.
func (c *Client) SetLogger(l *slog.Logger) {
	c.logger = l
}

// SetThrottle attaches a rate/concurrency limiter to this client.
func (c *Client) SetThrottle(t *EndpointThrottle) {
	c.throttle = t
}

// SetRetryConfig replaces the default retry configuration.
// Call this after NewClient to apply production settings.
func (c *Client) SetRetryConfig(cfg RetryConfig) {
	c.retryCfg = cfg
}

// SetAdapter sets the provider-specific adapter for normalizing requests and responses.
// When not set, buildChatRequest falls back to GenericAdapter.
func (c *Client) SetAdapter(a ProviderAdapter) {
	c.adapter = a
}

// getAdapter returns the configured adapter, defaulting to the package-level
// GenericAdapter singleton to avoid repeated allocation of stateless structs.
func (c *Client) getAdapter() ProviderAdapter {
	if c.adapter != nil {
		return c.adapter
	}
	return defaultAdapter
}

// applyResponseFormat plumbs req.ResponseFormat onto chatReq (provider-agnostic;
// SDK exposes response_format natively on v1.41+) and logs the outcome. Adapters
// can rewrite or clear chatReq.ResponseFormat in NormalizeRequest for providers
// whose wire shape diverges (e.g. Ollama's native /api/chat format field).
// See ADR-034.
func (c *Client) applyResponseFormat(chatReq *openai.ChatCompletionRequest, req agentic.AgentRequest) {
	if req.ResponseFormat == nil {
		return
	}
	sdkRF, marshalErr := toOpenAIResponseFormat(req.ResponseFormat)
	chatReq.ResponseFormat = sdkRF
	if c.logger == nil {
		return
	}
	attrs := []any{
		slog.String("request_id", req.RequestID),
		slog.String("type", req.ResponseFormat.Type),
		slog.String("name", req.ResponseFormat.Name),
	}
	if marshalErr != nil {
		attrs = append(attrs, slog.Any("err", marshalErr))
		c.logger.Warn("response_format schema failed to marshal; upstream will reject with a generic schema error", attrs...)
		return
	}
	c.logger.Debug("response_format plumbed to outgoing request", attrs...)
}

// toOpenAIResponseFormat translates an agentic.ResponseFormat to the OpenAI
// SDK's ChatCompletionResponseFormat. Returns nil when rf is nil.
//
// Schema is wrapped in json.RawMessage to satisfy the SDK's json.Marshaler
// contract — we marshal the caller's map[string]any once here and pass the
// raw bytes through. A non-nil marshal error means the caller's schema map
// contained an unmarshalable value (channel, func, cyclic struct); the
// returned format still has JSONSchema allocated (so SDK serialization
// proceeds) but Schema is nil and the caller is expected to log Warn.
// Without that signal the upstream 400 surfaces a generic "schema must be
// object" rather than the real root cause.
func toOpenAIResponseFormat(rf *agentic.ResponseFormat) (*openai.ChatCompletionResponseFormat, error) {
	if rf == nil {
		return nil, nil
	}
	out := &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatType(rf.Type),
	}
	if rf.Type != agentic.ResponseFormatJSONSchema {
		return out, nil
	}
	var (
		schemaPayload json.Marshaler
		marshalErr    error
	)
	if len(rf.Schema) > 0 {
		schemaBytes, err := json.Marshal(rf.Schema)
		if err != nil {
			marshalErr = err
		} else {
			schemaPayload = json.RawMessage(schemaBytes)
		}
	}
	// JSONSchema is allocated even when schemaPayload is nil — the SDK
	// will serialize "schema": null and OpenAI returns a generic schema
	// error. The non-nil marshalErr is the operator-facing signal that
	// the actual root cause is the caller's schema map, not the upstream.
	out.JSONSchema = &openai.ChatCompletionResponseFormatJSONSchema{
		Name:   rf.Name,
		Schema: schemaPayload,
		Strict: rf.Strict,
	}
	return out, marshalErr
}

// buildChatRequest converts an AgentRequest into an OpenAI ChatCompletionRequest.
func (c *Client) buildChatRequest(req agentic.AgentRequest) openai.ChatCompletionRequest {
	if len(req.Messages) == 0 {
		// Return a minimal request — ChatCompletion will get an API error rather
		// than a panic or cryptic "contents is not specified" from Gemini.
		if c.logger != nil {
			c.logger.Warn("buildChatRequest called with empty messages",
				slog.String("request_id", req.RequestID),
				slog.String("loop_id", req.LoopID))
		}
		return openai.ChatCompletionRequest{Model: c.endpoint.Model}
	}

	messages := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
			// ReasoningContent is intentionally omitted from outgoing requests.
			// Providers return it in responses but reject it in request messages
			// (e.g., Gemini returns 400 INVALID_ARGUMENT).
		}

		// Handle tool results - include tool_call_id (required by OpenAI API)
		if msg.Role == "tool" && msg.ToolCallID != "" {
			messages[i].ToolCallID = msg.ToolCallID
		}

		// Copy tool result name; provider-specific normalization (e.g., empty name
		// fallback for Gemini) is applied below by the adapter.
		if msg.Role == "tool" {
			messages[i].Name = msg.Name
		}

		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]openai.ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				args := tc.Arguments
				if args == nil {
					args = make(map[string]any)
				}
				argsJSON, _ := json.Marshal(args)
				toolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				}
			}
			messages[i].ToolCalls = toolCalls
		}
	}

	// Apply provider-specific message normalization (e.g., Gemini requires a
	// non-empty name on tool results and non-empty content on assistant tool_call messages).
	// ADR-037 chunk 6: the adapter speaks wire types; round-trip at this seam
	// until the wire-native client path lands.
	adapter := c.getAdapter()
	wireMessages := adapter.NormalizeMessages(sdkMessagesToWire(messages))
	messages = wireMessagesToSDK(wireMessages)

	chatReq := openai.ChatCompletionRequest{
		Model:    c.endpoint.Model,
		Messages: messages,
	}

	if req.MaxTokens > 0 {
		chatReq.MaxTokens = req.MaxTokens
	} else if c.endpoint.MaxOutputTokens > 0 {
		chatReq.MaxTokens = c.endpoint.MaxOutputTokens
	}
	if req.Temperature > 0 {
		chatReq.Temperature = float32(req.Temperature)
	}
	if len(c.endpoint.Options) > 0 {
		chatReq.ChatTemplateKwargs = c.endpoint.Options
	}
	if c.endpoint.ReasoningEffort != "" {
		chatReq.ReasoningEffort = c.endpoint.ReasoningEffort
	}

	c.applyResponseFormat(&chatReq, req)

	// Apply provider-specific request normalization. ADR-037 chunk 6:
	// adapter operates on wire types; round-trip the relevant SDK fields
	// for adapter inspection and copy back any mutations. Today all
	// adapters are no-op on NormalizeRequest; chunk 8 wires the Gemini
	// thought_signature Extras carrier through this seam.
	wireReq := sdkRequestToWire(&chatReq)
	adapter.NormalizeRequest(&wireReq)
	applyWireRequestToSDK(wireReq, &chatReq)

	// Convert tools if present. Strict is forwarded to the SDK's native
	// FunctionDefinition.Strict field (go-openai v1.41+); adapters that
	// run on no-op providers clear it in NormalizeRequest. See ADR-035.
	if len(req.Tools) > 0 {
		tools := make([]openai.Tool, len(req.Tools))
		for i, tool := range req.Tools {
			tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
					Strict:      tool.Strict,
				},
			}
		}
		chatReq.Tools = tools
	}

	// Convert tool choice if present
	if req.ToolChoice != nil {
		switch req.ToolChoice.Mode {
		case "auto", "required", "none":
			chatReq.ToolChoice = req.ToolChoice.Mode
		case "function":
			chatReq.ToolChoice = openai.ToolChoice{
				Type:     openai.ToolTypeFunction,
				Function: openai.ToolFunction{Name: req.ToolChoice.FunctionName},
			}
		}
	}

	return chatReq
}

// ChatCompletion sends a chat completion request with retry and throttling.
//
// Retry strategy uses two independent backoff curves:
//   - Transient errors (5xx, network): exponential from InitialDelay, up to MaxAttempts
//   - Rate limits (429): exponential from RateLimitDelay, up to MaxRateLimitRetries
//
// Both curves cap at MaxDelay and respect ctx cancellation at every wait point.
//
// When endpoint.WireBackend = "wire" (ADR-037), the call routes through
// the wire-native path (chatCompletionWire); otherwise the SDK path runs.
func (c *Client) ChatCompletion(ctx context.Context, req agentic.AgentRequest) (agentic.AgentResponse, error) {
	if c.useWireBackend() {
		return c.chatCompletionWire(ctx, req)
	}
	c.runProviderStartupProbes(ctx)
	chatReq := c.buildChatRequest(req)
	c.debugLogRequest(req.RequestID, chatReq.Model, len(chatReq.Messages), &chatReq)
	if c.throttle != nil {
		if err := c.throttle.Acquire(ctx); err != nil {
			return errorResponse(req.RequestID, err.Error()), nil
		}
		defer c.throttle.Release()
	}
	return c.runRetryLoop(ctx, req.RequestID, chatReq.Model, func() (agentic.AgentResponse, error) {
		return c.doSingleAttempt(ctx, chatReq, req.RequestID)
	})
}

// debugLogRequest emits a debug log of the outgoing chat completion
// payload when logger debug is enabled. Generic across SDK and wire
// payload shapes via json.Marshal.
func (c *Client) debugLogRequest(requestID, model string, messageCount int, payload any) {
	if c.logger == nil || !c.logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	c.logger.Debug("OpenAI API request payload",
		slog.String("request_id", requestID),
		slog.String("model", model),
		slog.Int("message_count", messageCount),
		slog.String("payload", string(body)))
}

// runRetryLoop drives the two-curve backoff for transient errors and
// rate-limits. attemptFn is invoked once per attempt; it must return
// (response, nil) on success or (_, err) for retry classification.
// Backend-agnostic — works for both SDK and wire paths via
// isRetryableAny / isRateLimitedAny.
func (c *Client) runRetryLoop(ctx context.Context, requestID, model string, attemptFn func() (agentic.AgentResponse, error)) (agentic.AgentResponse, error) {
	maxAttempts := c.retryCfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 3
	}
	maxRLRetries := c.retryCfg.maxRateLimitRetriesOrDefault(5)
	genericDelay := c.retryCfg.initialDelayDuration(100 * time.Millisecond)
	rlDelay := c.retryCfg.rateLimitDelayDuration(15 * time.Second)
	maxDelay := c.retryCfg.maxDelayDuration(60 * time.Second)

	genericAttempt, rlAttempt := 0, 0
	for {
		if ctx.Err() != nil {
			return errorResponse(requestID, ctx.Err().Error()), nil
		}
		resp, err := attemptFn()
		if err == nil {
			return c.withRetryCount(resp, genericAttempt+rlAttempt), nil
		}
		switch {
		case isRateLimitedAny(err):
			done, newDelay, halt := c.handleRateLimit(ctx, requestID, model, err, &rlAttempt, maxRLRetries, rlDelay, maxDelay)
			if halt {
				return done, nil
			}
			rlDelay = newDelay
		case !isRetryableAny(err):
			return errorResponse(requestID, err.Error()), nil
		default:
			done, newDelay, halt := c.handleTransient(ctx, requestID, model, err, &genericAttempt, maxAttempts, genericDelay, maxDelay)
			if halt {
				return done, nil
			}
			genericDelay = newDelay
		}
	}
}

func (c *Client) handleRateLimit(ctx context.Context, requestID, model string, err error, attempt *int, maxAttempts int, delay, maxDelay time.Duration) (agentic.AgentResponse, time.Duration, bool) {
	*attempt++
	if c.metrics != nil {
		c.metrics.recordRateLimitHit(model)
	}
	if *attempt >= maxAttempts {
		c.logWarn("rate limit retries exhausted", requestID, model,
			slog.Int("attempts", *attempt), slog.Int("max_attempts", maxAttempts))
		return errorResponse(requestID, err.Error()), delay, true
	}
	wait := addJitter(delay)
	c.logWarn("rate limited by provider, backing off", requestID, model,
		slog.Int("attempt", *attempt), slog.Int("max_attempts", maxAttempts),
		slog.Duration("wait", wait))
	if c.metrics != nil {
		c.metrics.recordRateLimitRetry(model)
	}
	if !sleepWithContext(ctx, wait) {
		return errorResponse(requestID, ctx.Err().Error()), delay, true
	}
	return agentic.AgentResponse{}, min(time.Duration(float64(delay)*2), maxDelay), false
}

func (c *Client) handleTransient(ctx context.Context, requestID, model string, err error, attempt *int, maxAttempts int, delay, maxDelay time.Duration) (agentic.AgentResponse, time.Duration, bool) {
	*attempt++
	if *attempt >= maxAttempts {
		c.logWarn("transient error retries exhausted", requestID, model,
			slog.Int("attempts", *attempt), slog.Int("max_attempts", maxAttempts),
			slog.String("last_error", err.Error()))
		return errorResponse(requestID, err.Error()), delay, true
	}
	wait := addJitter(delay)
	if c.logger != nil {
		c.logger.Debug("transient error, retrying",
			slog.String("request_id", requestID), slog.String("model", model),
			slog.Int("attempt", *attempt), slog.Int("max_attempts", maxAttempts),
			slog.Duration("wait", wait), slog.String("error", err.Error()))
	}
	if !sleepWithContext(ctx, wait) {
		return errorResponse(requestID, ctx.Err().Error()), delay, true
	}
	return agentic.AgentResponse{}, min(time.Duration(float64(delay)*2), maxDelay), false
}

// logWarn logs a warning if a logger is set. Reduces boilerplate in ChatCompletion.
func (c *Client) logWarn(msg, requestID, model string, attrs ...slog.Attr) {
	if c.logger != nil {
		allAttrs := append([]slog.Attr{
			slog.String("request_id", requestID),
			slog.String("model", model),
		}, attrs...)
		args := make([]any, len(allAttrs))
		for i, a := range allAttrs {
			args[i] = a
		}
		c.logger.Warn(msg, args...)
	}
}

// withRetryCount sets the retry count on a response if retries occurred.
func (c *Client) withRetryCount(resp agentic.AgentResponse, count int) agentic.AgentResponse {
	resp.RetryCount = count
	return resp
}

// errorResponse builds an AgentResponse with status "error".
func errorResponse(requestID, errMsg string) agentic.AgentResponse {
	return agentic.AgentResponse{
		RequestID: requestID,
		Status:    "error",
		Error:     errMsg,
	}
}

// doSingleAttempt executes one request attempt (streaming or non-streaming).
// Returns (response, nil) on success, or (zero, error) on failure.
func (c *Client) doSingleAttempt(ctx context.Context, chatReq openai.ChatCompletionRequest, requestID string) (agentic.AgentResponse, error) {
	if c.endpoint.Stream {
		resp, err := c.streamChatCompletion(ctx, chatReq, requestID)
		if err != nil {
			if c.logger != nil {
				c.logger.Debug("stream connection failed",
					slog.String("request_id", requestID),
					slog.String("model", chatReq.Model),
					slog.String("error", err.Error()))
			}
			return agentic.AgentResponse{}, err
		}
		// Mid-stream errors are returned as AgentResponse{Status:"error"} — not retried.
		return resp, nil
	}

	resp, err := c.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("API request failed",
				slog.String("request_id", requestID),
				slog.String("model", chatReq.Model),
				slog.String("error", err.Error()))
		}
		return agentic.AgentResponse{}, err
	}

	return c.convertResponse(resp, requestID), nil
}

// sleepWithContext waits for the given duration or until ctx is cancelled.
// Returns true if the sleep completed, false if ctx was cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		timer.Stop()
		return false
	case <-timer.C:
		return true
	}
}

// addJitter adds up to 25% jitter to a duration to prevent thundering herd.
func addJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	jitter := time.Duration(rand.Int63n(int64(d / 4)))
	return d + jitter
}

// streamChatCompletion handles the streaming path. Connection errors return
// a Go error (retryable). Mid-stream errors return AgentResponse{Status: "error"}
// (not retryable — partial state can't be replayed).
func (c *Client) streamChatCompletion(ctx context.Context, chatReq openai.ChatCompletionRequest, requestID string) (agentic.AgentResponse, error) {
	chatReq.Stream = true
	chatReq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}

	stream, err := c.client.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		return agentic.AgentResponse{}, err // retryable connection error
	}
	defer stream.Close()

	acc := &streamAccumulator{adapter: c.getAdapter(), logger: c.logger}
	streamStart := time.Now()
	firstTokenRecorded := false

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Mid-stream error — not retryable. Preserve any partial tokens
			// the accumulator received before the connection died.
			// Precedence: a transport error overrides any prior status
			// the accumulator may have set, including length_truncated.
			// This is intentional — partial state cannot be replayed once
			// the connection died, and the caller needs to know the
			// stream itself failed (Status="error") rather than that the
			// model finished gracefully short.
			resp := acc.toAgentResponse(requestID)
			resp.Status = "error"
			resp.Error = err.Error()
			return resp, nil
		}

		// Record usage from the final chunk (has empty choices)
		if chunk.Usage != nil {
			acc.setUsage(chunk.Usage)
		}

		// Process choice deltas
		for _, choice := range chunk.Choices {
			// processDelta returns the routed (content, reasoning)
			// split — inline <think>...</think> blocks are split out
			// of the raw content so the chunk handler sees clean
			// content + reasoning even on inline-tag providers (qwen3,
			// deepseek-r1, qwen3-coder via OpenAI-compat). Channel-
			// based reasoning_content is merged into reasoningDelta
			// here so consumers see one stream regardless of provider.
			contentDelta, reasoningDelta := acc.processDelta(choice)

			// Build chunk for handler
			if c.chunkHandler != nil {
				sc := StreamChunk{
					RequestID:      requestID,
					ContentDelta:   contentDelta,
					ReasoningDelta: reasoningDelta,
				}
				c.chunkHandler(sc)
			}

			// Record streaming metrics. TTFT is gated on either side
			// having content — first reasoning byte counts as a token
			// for time-to-first-token, same as before.
			if c.metrics != nil {
				c.metrics.recordStreamChunk(chatReq.Model)
				if !firstTokenRecorded && (contentDelta != "" || reasoningDelta != "") {
					c.metrics.recordStreamTTFT(chatReq.Model, time.Since(streamStart).Seconds())
					firstTokenRecorded = true
				}
			}
		}
	}

	// Send done signal to handler
	if c.chunkHandler != nil {
		c.chunkHandler(StreamChunk{RequestID: requestID, Done: true})
	}

	return acc.toAgentResponse(requestID), nil
}

// convertResponse converts OpenAI response to AgentResponse.
// NormalizeResponse is called before conversion so provider adapters can
// adjust the raw response (e.g., strip provider-specific fields).
//
// After provider-specific normalization, extractThinkBlocksFromResponse
// runs unconditionally — inline <think>...</think> blocks (qwen3 /
// deepseek-r1 / qwen3-coder pattern) are routed out of Content and
// into ReasoningContent so the AgentResponse shape is symmetric with
// the channel-based reasoning_content path. The hot path is allocation-
// free for responses without inline tags (a single strings.Contains).
// Universal rather than per-adapter because inline-think is a
// model-family quirk, not a provider quirk — qwen3-via-Ollama (generic
// adapter) and qwen3-via-OpenAI-compat (openai adapter) both emit it.
func (c *Client) convertResponse(resp openai.ChatCompletionResponse, requestID string) agentic.AgentResponse {
	// ADR-037 chunk 6: NormalizeResponse operates on wire types; round-trip
	// so adapters can begin extracting provider blobs (chunk 8 Gemini
	// thought_signature is the first non-no-op consumer).
	wireResp := sdkResponseToWire(&resp)
	c.getAdapter().NormalizeResponse(&wireResp)
	applyWireResponseToSDK(wireResp, &resp)
	extractThinkBlocksFromResponse(&resp)

	response := agentic.AgentResponse{
		RequestID: requestID,
	}

	if len(resp.Choices) == 0 {
		response.Status = "error"
		response.Error = "no choices in response"
		return response
	}

	choice := resp.Choices[0]

	// Map finish reason to status. Uses the typed openai constants
	// rather than string literals so future SDK churn / typo drift
	// breaks at compile time.
	response.FinishReason = string(choice.FinishReason)
	switch choice.FinishReason {
	case openai.FinishReasonStop:
		response.Status = agentic.StatusComplete
	case openai.FinishReasonLength:
		response.Status = agentic.StatusLengthTruncated
		if c.logger != nil {
			c.logger.Warn("model response truncated: finish_reason=length (max_tokens hit)",
				"request_id", requestID)
		}
	case openai.FinishReasonToolCalls:
		response.Status = agentic.StatusToolCall
	default:
		response.Status = agentic.StatusComplete
	}

	// Convert message
	response.Message = agentic.ChatMessage{
		Role:             choice.Message.Role,
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
	}

	// Convert tool calls if present — but ONLY when the response
	// wasn't truncated. Same rationale as the streaming path's
	// toAgentResponse: a truncated response cannot be trusted to have
	// complete tool-call arguments, and the malformed-args fallback
	// below would silently substitute {} for unparseable args. Beta.2
	// wired finish_reason=length to fail loudly for content responses;
	// this preserves that contract for the tool-call path so the
	// loop's truncation handler can decide whether to retry with
	// compaction or fail with a diagnostic message.
	if choice.FinishReason == openai.FinishReasonLength {
		if len(choice.Message.ToolCalls) > 0 && c.logger != nil {
			c.logger.Warn("response truncated mid-tool-call: dropping potentially-incomplete tool_calls",
				"request_id", requestID,
				"tool_calls_accumulated", len(choice.Message.ToolCalls),
				"completion_tokens", resp.Usage.CompletionTokens)
		}
	} else if len(choice.Message.ToolCalls) > 0 {
		response.Status = "tool_call"
		toolCalls := make([]agentic.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			// Parse arguments JSON — must never be nil or the replay
			// path will marshal it as "null" (a string), which the
			// Anthropic API rejects ("Input should be a valid dictionary").
			// This branch is reserved for the genuine "model emitted
			// broken JSON without truncation" case; truncation bails
			// earlier above.
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

	// Convert token usage
	response.TokenUsage = agentic.TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}

	return response
}

// Close releases resources held by the client
func (c *Client) Close() error {
	// OpenAI SDK client doesn't require explicit cleanup
	// HTTP connections are managed by http.Client's connection pooling
	return nil
}

// isRetryable checks if an error should trigger a retry.
// Context errors and unrecoverable 4xx responses are never retried.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return isRetryableStatusCode(apiErr.HTTPStatusCode)
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return isRetryableStatusCode(reqErr.HTTPStatusCode)
	}

	// Network-level errors (no HTTP status) are retryable.
	return true
}

// isRetryableStatusCode returns true for HTTP status codes that are worth retrying.
func isRetryableStatusCode(code int) bool {
	if code == 0 {
		// No status code — treat as network error, retryable.
		return true
	}
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		// All other 4xx errors (400, 401, 403, 404) are client errors — not retryable.
		return false
	}
}

// isRateLimited reports whether the error is an HTTP 429 Too Many Requests.
func isRateLimited(err error) bool {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode == http.StatusTooManyRequests {
		return true
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode == http.StatusTooManyRequests {
		return true
	}
	return false
}
