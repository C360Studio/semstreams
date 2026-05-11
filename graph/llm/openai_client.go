package llm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/model/wire"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/sashabaranov/go-openai"
)

const (
	// MaxSystemPromptLength is the maximum allowed system prompt length in characters.
	MaxSystemPromptLength = 8000

	// MaxUserPromptLength is the maximum allowed user prompt length in characters.
	MaxUserPromptLength = 32000

	// DefaultTemperature is used when Temperature is nil.
	DefaultTemperature = 0.7
)

// OpenAIClient implements Client using the OpenAI SDK.
//
// This implementation works with:
//   - seminstruct (local llama.cpp inference server) - recommended
//   - OpenAI (cloud)
//   - Any OpenAI-compatible API (Ollama, vLLM, LocalAI, etc.)
//
// Uses the standard OpenAI SDK for consistency with the embedding package.
type OpenAIClient struct {
	client     *openai.Client
	wireClient *wire.Client // ADR-037: populated when WireBackend = "wire"
	model      string
	maxRetries int
	logger     *slog.Logger
}

// OpenAIConfig configures the OpenAI client.
type OpenAIConfig struct {
	// BaseURL is the base URL of the LLM service.
	// Examples:
	//   - "http://seminstruct:8083/v1" (seminstruct local inference)
	//   - "http://localhost:8083/v1" (local development)
	//   - "https://api.openai.com/v1" (OpenAI cloud)
	BaseURL string

	// Model is the model to use for chat completions.
	// Examples:
	//   - "qwen3-0.6b" (seminstruct hot tier)
	//   - "gpt-4" (OpenAI)
	//   - "llama2" (Ollama)
	Model string

	// APIKey for authentication (optional for local services).
	// Required for OpenAI, optional for seminstruct/Ollama.
	APIKey string

	// Timeout for HTTP requests (default: 60s for LLM inference).
	Timeout time.Duration

	// MaxRetries for transient failures (default: 3).
	MaxRetries int

	// Logger for error logging (optional, defaults to slog.Default()).
	Logger *slog.Logger

	// IdleConnTimeout, ResponseHeaderTimeout, and DisableKeepAlives mirror
	// the same fields on model.EndpointConfig. Empty/false preserves Go
	// defaults. See model/httpclient.go for behaviour and the migration
	// guide for tuning recommendations per provider.
	IdleConnTimeout       string
	ResponseHeaderTimeout string
	DisableKeepAlives     bool

	// WireBackend selects the HTTP client implementation. "" or "sdk"
	// (default) uses the sashabaranov/go-openai client; "wire" uses the
	// framework-owned model/wire client (ADR-037). Mirrors
	// EndpointConfig.WireBackend.
	WireBackend string
}

// OpenAIConfigFromEndpoint builds an OpenAIConfig from a resolved capability
// endpoint, plumbing the connection-hygiene fields that mirror EndpointConfig
// (IdleConnTimeout, ResponseHeaderTimeout, DisableKeepAlives). Timeout is
// intentionally NOT set here — callers pick it via
// model.ResolveCapabilityTimeout (or their own precedence chain) and assign
// the result on the returned config before passing to NewOpenAIClient.
//
// This is the canonical translator from model.* to OpenAIConfig — every
// graph-query / graph-clustering site that constructs an LLM client off a
// resolved capability should use it. Hand-rolling the field plumbing has
// historically dropped fields silently (semspec smoke-#10 keepalive bug;
// community_summary 300s dead config).
//
// A nil resolved returns an empty config with only the logger set — caller
// must check for that and avoid passing the result to NewOpenAIClient (which
// will reject the missing BaseURL/Model). A nil ep skips the
// connection-hygiene fields and returns a config with just the resolved
// trio plus the logger — matches the behaviour of a pre-EndpointConfig caller.
func OpenAIConfigFromEndpoint(resolved *model.ResolvedEndpoint, ep *model.EndpointConfig, logger *slog.Logger) OpenAIConfig {
	if resolved == nil {
		return OpenAIConfig{Logger: logger}
	}
	cfg := OpenAIConfig{
		BaseURL: resolved.URL,
		Model:   resolved.Model,
		APIKey:  resolved.APIKey,
		Logger:  logger,
	}
	if ep != nil {
		cfg.IdleConnTimeout = ep.IdleConnTimeout
		cfg.ResponseHeaderTimeout = ep.ResponseHeaderTimeout
		cfg.DisableKeepAlives = ep.DisableKeepAlives
		cfg.WireBackend = ep.WireBackend
	}
	return cfg
}

// NewOpenAIClient creates a new OpenAI-compatible LLM client.
func NewOpenAIClient(cfg OpenAIConfig) (*OpenAIClient, error) {
	if cfg.BaseURL == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "OpenAIClient",
			"NewOpenAIClient", "base_url is required")
	}
	if cfg.Model == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "OpenAIClient",
			"NewOpenAIClient", "model is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second // LLM inference takes longer than embeddings
	}

	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	// Create OpenAI client config
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = "not-needed" // Local services don't require a key
	}

	httpClient := model.NewHTTPClient(model.HTTPClientOptions{
		Timeout:               timeout,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		DisableKeepAlives:     cfg.DisableKeepAlives,
		MaxIdleConnsPerHost:   10,
	})

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = cfg.BaseURL
	config.HTTPClient = httpClient
	client := openai.NewClientWithConfig(config)

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	out := &OpenAIClient{
		client:     client,
		model:      cfg.Model,
		maxRetries: maxRetries,
		logger:     logger,
	}

	// ADR-037: per-endpoint wire backend opt-in. The wire client shares
	// the same HTTP transport (model.NewHTTPClient invariant from
	// beta.43/47). A misconfigured WireBackend value would have failed
	// the EndpointConfig validation upstream; we treat any non-"wire"
	// value as the SDK path.
	if cfg.WireBackend == "wire" {
		authHeader := ""
		if cfg.APIKey != "" {
			authHeader = "Bearer " + cfg.APIKey
		}
		wc, err := wire.NewClient(wire.ClientConfig{
			BaseURL:    cfg.BaseURL,
			HTTPClient: httpClient,
			AuthHeader: authHeader,
		})
		if err != nil {
			return nil, fmt.Errorf("graph/llm: build wire client: %w", err)
		}
		out.wireClient = wc
	}

	return out, nil
}

// ChatCompletion sends a chat completion request to the LLM service.
func (c *OpenAIClient) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Validate prompt lengths to prevent exceeding model context windows
	if len(req.SystemPrompt) > MaxSystemPromptLength {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "OpenAIClient",
			"ChatCompletion", fmt.Sprintf("system prompt too long: %d chars (max %d)", len(req.SystemPrompt), MaxSystemPromptLength))
	}
	if len(req.UserPrompt) > MaxUserPromptLength {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "OpenAIClient",
			"ChatCompletion", fmt.Sprintf("user prompt too long: %d chars (max %d)", len(req.UserPrompt), MaxUserPromptLength))
	}

	// Build messages array
	messages := []openai.ChatCompletionMessage{}

	if req.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: req.UserPrompt,
	})

	// Set defaults
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 256
	}

	// Handle Temperature pointer: nil means use default, otherwise use provided value
	temperature := DefaultTemperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}

	// Build request
	chatReq := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
	}

	// Execute with retry logic. The wire backend gates inside the loop
	// so retry semantics stay identical across backends.
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		if c.wireClient != nil {
			resp, err := c.doWireChat(ctx, chatReq)
			if err == nil {
				return resp, nil
			}
			lastErr = err
		} else {
			resp, err := c.client.CreateChatCompletion(ctx, chatReq)
			if err == nil {
				if len(resp.Choices) == 0 {
					return nil, errs.WrapInvalid(errs.ErrInvalidData, "OpenAIClient",
						"ChatCompletion", "no choices in response")
				}
				return &ChatResponse{
					Content:          resp.Choices[0].Message.Content,
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
					Model:            resp.Model,
					FinishReason:     string(resp.Choices[0].FinishReason),
				}, nil
			}
			lastErr = err
		}

		c.logger.Warn("LLM request failed, retrying",
			"attempt", attempt+1,
			"max_retries", c.maxRetries,
			"error", lastErr)
	}

	return nil, errs.WrapTransient(lastErr, "OpenAIClient", "ChatCompletion",
		fmt.Sprintf("failed after %d retries", c.maxRetries+1))
}

// doWireChat is the wire-native ChatCompletion path used when the
// client was configured with WireBackend = "wire". Builds a
// wire.ChatCompletionRequest from the SDK chatReq (lift-and-shift
// translation; graph/llm has no tool/streaming surface) and decodes
// the wire response into a ChatResponse with the same field set the
// SDK path produces.
func (c *OpenAIClient) doWireChat(ctx context.Context, chatReq openai.ChatCompletionRequest) (*ChatResponse, error) {
	wreq := &wire.ChatCompletionRequest{
		Model:     chatReq.Model,
		MaxTokens: chatReq.MaxTokens,
		Messages:  make([]wire.Message, len(chatReq.Messages)),
	}
	if chatReq.Temperature > 0 {
		t := float64(chatReq.Temperature)
		wreq.Temperature = &t
	}
	for i, msg := range chatReq.Messages {
		wreq.Messages[i] = wire.Message{Role: msg.Role}
		if msg.Content != "" {
			_ = wreq.Messages[i].SetContentString(msg.Content)
		}
	}

	resp, err := c.wireClient.ChatCompletion(ctx, wreq)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "OpenAIClient",
			"ChatCompletion", "no choices in response")
	}
	out := &ChatResponse{
		Model:        resp.Model,
		FinishReason: resp.Choices[0].FinishReason,
	}
	if resp.Choices[0].Message != nil {
		if s, ok := resp.Choices[0].Message.ContentString(); ok {
			out.Content = s
		}
	}
	if resp.Usage != nil {
		out.PromptTokens = resp.Usage.PromptTokens
		out.CompletionTokens = resp.Usage.CompletionTokens
		out.TotalTokens = resp.Usage.TotalTokens
	}
	return out, nil
}

// Model returns the model identifier.
func (c *OpenAIClient) Model() string {
	return c.model
}

// Close releases resources (no-op for HTTP client).
func (c *OpenAIClient) Close() error {
	return nil
}
