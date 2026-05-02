package model

import (
	"net/http"
	"time"
)

// HTTPClientOptions configures the *http.Client used by OpenAI-compatible
// clients in the framework. Zero/empty values preserve Go's net/http defaults
// — every field is opt-in tightening, never automatic behaviour change.
//
// The fields here exist because Go's http.DefaultTransport has no
// ResponseHeaderTimeout and a 90s IdleConnTimeout, which is too forgiving for
// LLM gateways that silently drop idle TCP connections (Ollama, OpenRouter in
// some configurations). The next request reuses a stale pooled connection
// whose Read blocks until the request-level ctx fires — typically minutes
// away on production deployments tuned for slow models.
type HTTPClientOptions struct {
	// Timeout sets http.Client.Timeout (overall request deadline). Zero
	// preserves Go's default (no timeout) — appropriate when the caller
	// drives cancellation via context (the agentic-model pattern). Set
	// only when the caller does NOT manage ctx deadlines itself.
	Timeout time.Duration
	// IdleConnTimeout bounds idle pooled connection lifetime. Empty
	// preserves Go's default (90s).
	IdleConnTimeout string
	// ResponseHeaderTimeout caps how long to wait for response headers
	// after the request body is fully written. Empty preserves Go's
	// default (no timeout). Safe for streaming endpoints; risky for
	// non-streaming endpoints where the server sends headers only after
	// generation completes.
	ResponseHeaderTimeout string
	// DisableKeepAlives forces a fresh TCP/TLS connection per request.
	DisableKeepAlives bool
	// MaxIdleConnsPerHost caps the per-host idle pool. Zero preserves
	// Go's default (2).
	MaxIdleConnsPerHost int
}

// NewHTTPClient builds an *http.Client with optional Transport tuning.
// Returns nil-safe zero defaults; passing a zero HTTPClientOptions value
// returns a client equivalent to &http.Client{} (Go's default Transport
// behaviour — same as openai.DefaultConfig()).
//
// Duration fields with invalid values are silently ignored (Go's default
// applies). Validation is the responsibility of the caller (EndpointConfig
// goes through validateEndpoint at config load time).
func NewHTTPClient(opts HTTPClientOptions) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.DefaultTransport.(*http.Transport).Proxy,
		DialContext:           http.DefaultTransport.(*http.Transport).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     opts.DisableKeepAlives,
	}

	if opts.IdleConnTimeout != "" {
		if d, err := time.ParseDuration(opts.IdleConnTimeout); err == nil {
			transport.IdleConnTimeout = d
		}
	}
	if opts.ResponseHeaderTimeout != "" {
		if d, err := time.ParseDuration(opts.ResponseHeaderTimeout); err == nil {
			transport.ResponseHeaderTimeout = d
		}
	}
	if opts.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	}

	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}
}

// HTTPClientOptionsFromEndpoint builds HTTPClientOptions from an EndpointConfig,
// translating the three Transport-tuning fields. The Timeout field is left
// zero — agentic-model and graph/llm both manage request deadlines via context.
func HTTPClientOptionsFromEndpoint(ep *EndpointConfig) HTTPClientOptions {
	if ep == nil {
		return HTTPClientOptions{}
	}
	return HTTPClientOptions{
		IdleConnTimeout:       ep.IdleConnTimeout,
		ResponseHeaderTimeout: ep.ResponseHeaderTimeout,
		DisableKeepAlives:     ep.DisableKeepAlives,
	}
}
