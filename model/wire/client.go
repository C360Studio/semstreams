package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// chatCompletionsPath is the OpenAI-compat ChatCompletion route. Used
// when ClientConfig.ChatCompletionsURL is empty (the typical case
// where BaseURL points to .../v1).
const chatCompletionsPath = "/chat/completions"

// embeddingsPath is the OpenAI-compat Embeddings route.
const embeddingsPath = "/embeddings"

// ClientConfig configures a Client. BaseURL and HTTPClient are
// required; everything else is optional.
type ClientConfig struct {
	// BaseURL is the OpenAI-compat root, e.g. "https://api.openai.com/v1".
	// If ChatCompletionsURL or EmbeddingsURL are set, they take
	// precedence for their respective endpoints.
	BaseURL string

	// HTTPClient is the transport. Callers should use
	// model.NewHTTPClient to construct one with the framework's
	// connection-hygiene defaults.
	HTTPClient *http.Client

	// AuthHeader, if non-empty, is set as the Authorization header on
	// every request. Typically "Bearer <key>".
	AuthHeader string

	// ExtraHeaders are added to every request before send. Useful for
	// provider-specific headers like "x-api-key" or "anthropic-version".
	ExtraHeaders http.Header

	// ChatCompletionsURL overrides BaseURL+/chat/completions. Empty
	// means derive from BaseURL.
	ChatCompletionsURL string

	// EmbeddingsURL overrides BaseURL+/embeddings.
	EmbeddingsURL string

	// MaxFrameSize caps the per-SSE-frame buffer. 0 uses
	// DefaultMaxFrameSize (16 MiB). Bump for providers that emit
	// outsized tool_call argument blobs in a single frame.
	MaxFrameSize int
}

// Client is the OpenAI-compat ChatCompletion / Embeddings client. It
// is safe for concurrent use; callers typically construct one Client
// per endpoint and reuse it across requests.
type Client struct {
	cfg ClientConfig
}

// NewClient validates cfg and returns a Client. Returns an error if
// HTTPClient or BaseURL is missing.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.HTTPClient == nil {
		return nil, fmt.Errorf("wire: ClientConfig.HTTPClient is required")
	}
	if cfg.BaseURL == "" && cfg.ChatCompletionsURL == "" && cfg.EmbeddingsURL == "" {
		return nil, fmt.Errorf("wire: ClientConfig.BaseURL is required when no per-endpoint URL is set")
	}
	return &Client{cfg: cfg}, nil
}

// ChatCompletion executes a non-streaming ChatCompletion call. Returns
// a decoded response on HTTP 2xx, or *APIError with the status code
// recorded on any other status. Network and decode errors return as
// regular errors.
func (c *Client) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wire: marshal request: %w", err)
	}
	httpReq, err := c.buildHTTPRequest(ctx, http.MethodPost, c.chatCompletionsURL(), body, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wire: ChatCompletion: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wire: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, DecodeError(resp.StatusCode, bodyBytes)
	}

	var out ChatCompletionResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return nil, fmt.Errorf("wire: decode response: %w", err)
	}
	return &out, nil
}

// ChatCompletionStream executes a streaming ChatCompletion call.
// Returns a *Stream the caller drives via Recv until io.EOF; callers
// MUST call Close when done. On non-2xx the response body is read
// fully and returned as *APIError; the *Stream is nil in that case.
func (c *Client) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (*Stream, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wire: marshal request: %w", err)
	}
	httpReq, err := c.buildHTTPRequest(ctx, http.MethodPost, c.chatCompletionsURL(), body, "application/json")
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wire: ChatCompletionStream: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, DecodeError(resp.StatusCode, bodyBytes)
	}

	return newStream(resp.Body, c.cfg.MaxFrameSize), nil
}

// Embeddings executes a non-streaming Embeddings call.
func (c *Client) Embeddings(ctx context.Context, req *EmbeddingsRequest) (*EmbeddingsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wire: marshal embeddings request: %w", err)
	}
	httpReq, err := c.buildHTTPRequest(ctx, http.MethodPost, c.embeddingsURL(), body, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wire: Embeddings: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wire: read embeddings response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, DecodeError(resp.StatusCode, bodyBytes)
	}

	var out EmbeddingsResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return nil, fmt.Errorf("wire: decode embeddings response: %w", err)
	}
	return &out, nil
}

// buildHTTPRequest constructs an *http.Request with auth + extras
// headers applied. body may be nil.
func (c *Client) buildHTTPRequest(ctx context.Context, method, url string, body []byte, contentType string) (*http.Request, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, fmt.Errorf("wire: build request: %w", err)
	}
	if contentType != "" && body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", c.cfg.AuthHeader)
	}
	for k, vals := range c.cfg.ExtraHeaders {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	return req, nil
}

// chatCompletionsURL returns the configured ChatCompletions URL or
// derives it from BaseURL.
func (c *Client) chatCompletionsURL() string {
	if c.cfg.ChatCompletionsURL != "" {
		return c.cfg.ChatCompletionsURL
	}
	return joinURL(c.cfg.BaseURL, chatCompletionsPath)
}

// embeddingsURL returns the configured Embeddings URL or derives it.
func (c *Client) embeddingsURL() string {
	if c.cfg.EmbeddingsURL != "" {
		return c.cfg.EmbeddingsURL
	}
	return joinURL(c.cfg.BaseURL, embeddingsPath)
}

// joinURL concatenates a base and a path, normalizing the slash join.
func joinURL(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
