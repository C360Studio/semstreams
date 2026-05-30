package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// responsesPath is the OpenAI Responses route. Used when
// ClientConfig.ResponsesURL is empty (the typical case where BaseURL
// points to .../v1).
const responsesPath = "/responses"

// ClientConfig configures a Responses Client. Mirror of
// wire.ClientConfig; the two clients share auth/transport plumbing
// but speak different wire shapes. BaseURL and HTTPClient are
// required; everything else is optional.
type ClientConfig struct {
	// BaseURL is the OpenAI-compat root, e.g. "https://api.openai.com/v1".
	// If ResponsesURL is set, it takes precedence.
	BaseURL string

	// HTTPClient is the transport. Callers should use
	// model.NewHTTPClient to construct one with the framework's
	// connection-hygiene defaults.
	HTTPClient *http.Client

	// AuthHeader, if non-empty, is set as the Authorization header
	// on every request. Typically "Bearer <key>".
	AuthHeader string

	// ExtraHeaders are added to every request before send. Useful for
	// provider-specific headers.
	ExtraHeaders http.Header

	// ResponsesURL overrides BaseURL+/responses. Empty means derive
	// from BaseURL.
	ResponsesURL string

	// MaxFrameSize caps the per-SSE-frame buffer in streaming mode.
	// Phase 1 client is non-streaming; the field is carried for the
	// Phase 2 streaming addition.
	MaxFrameSize int
}

// Client is the OpenAI Responses API client. Safe for concurrent
// use; callers typically construct one Client per endpoint and reuse
// it across requests.
type Client struct {
	cfg ClientConfig
}

// NewClient validates cfg and returns a Client. Returns an error if
// HTTPClient or BaseURL is missing.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.HTTPClient == nil {
		return nil, fmt.Errorf("responses: ClientConfig.HTTPClient is required")
	}
	if cfg.BaseURL == "" && cfg.ResponsesURL == "" {
		return nil, fmt.Errorf("responses: ClientConfig.BaseURL is required when ResponsesURL is not set")
	}
	return &Client{cfg: cfg}, nil
}

// Responses executes a non-streaming Responses call. Returns a
// decoded response on HTTP 2xx, or *APIError with the status code
// recorded on any other status. Network and decode errors return as
// regular errors.
//
// Streaming callers should use ResponsesStream instead; passing
// req.Stream=true here returns an error to make the misuse loud
// (Responses always forces Stream=false on the wire).
func (c *Client) Responses(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, fmt.Errorf("responses: nil request")
	}
	if req.Stream {
		return nil, fmt.Errorf("responses: req.Stream=true; use ResponsesStream for streaming")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}
	httpReq, err := c.buildHTTPRequest(ctx, http.MethodPost, c.responsesURL(), body, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("responses: Responses: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("responses: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, DecodeError(resp.StatusCode, bodyBytes)
	}

	var out Response
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return nil, fmt.Errorf("responses: decode response: %w", err)
	}
	return &out, nil
}

// ResponsesStream executes a streaming Responses call. Returns a
// *Stream the caller drives via Recv until io.EOF; callers MUST
// call Close when done. On non-2xx the response body is read fully
// and returned as *APIError; the *Stream is nil in that case.
//
// The request's Stream flag is forced to true before send — the
// caller does not need to set it.
func (c *Client) ResponsesStream(ctx context.Context, req *Request) (*Stream, error) {
	if req == nil {
		return nil, fmt.Errorf("responses: nil request")
	}
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}
	httpReq, err := c.buildHTTPRequest(ctx, http.MethodPost, c.responsesURL(), body, "application/json")
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("responses: ResponsesStream: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, DecodeError(resp.StatusCode, bodyBytes)
	}

	return newStream(resp.Body, c.cfg.MaxFrameSize), nil
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
		return nil, fmt.Errorf("responses: build request: %w", err)
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

// responsesURL returns the configured Responses URL or derives it
// from BaseURL.
func (c *Client) responsesURL() string {
	if c.cfg.ResponsesURL != "" {
		return c.cfg.ResponsesURL
	}
	return joinURL(c.cfg.BaseURL, responsesPath)
}

// joinURL concatenates a base and a path, normalizing the slash join.
func joinURL(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
