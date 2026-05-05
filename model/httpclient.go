package model

import (
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

// FrameworkDefaultIdleConnTimeout is the default idle-pool window used
// by NewHTTPClient when no operator-configured value is supplied.
// Sized to bound stale-pool risk against keepalive-hostile gateways
// (local LLM servers, some cloud routes) without disabling reuse for
// the typical agent flow where requests are seconds apart. Cloud
// providers (OpenAI, Anthropic) keep connections fresh anyway via
// HTTP/2 multiplexing + TLS session resumption, so the cost of
// occasional reconnects is negligible.
//
// Tightened from Go's net/http default (90s) in beta.43 after the
// long tail of stale-pooled-conn wedges semspec/semteams hit through
// beta.34-42. A higher value can still be set per-endpoint via
// EndpointConfig.IdleConnTimeout.
const FrameworkDefaultIdleConnTimeout = 30 * time.Second

// FrameworkDefaultMaxIdleConnsPerHost caps the idle pool per host.
// Tightened from Go's net/http default (2) so a wedged connection
// can't sit alongside a healthy one waiting for the LRU eviction.
// A higher value can still be set per-call via HTTPClientOptions.
const FrameworkDefaultMaxIdleConnsPerHost = 1

// FrameworkDefaultHTTP2ReadIdleTimeout configures HTTP/2 keepalive
// PINGs: every N seconds of read idleness, the client sends a PING
// frame; if the response doesn't arrive within HTTP2PingTimeout, the
// underlying connection is torn down and reissued. This catches the
// dead-pooled-conn class WITHOUT disabling keepalive entirely — the
// PING-failure path closes the dead conn and the next request gets
// a fresh one transparently.
//
// HTTP/2-only. HTTP/1.1 connections (most local LLM servers like
// Ollama, llama.cpp HTTP) can't carry PING frames; for those,
// IdleConnTimeout + DisableKeepAlives are the operative knobs.
const FrameworkDefaultHTTP2ReadIdleTimeout = 15 * time.Second

// FrameworkDefaultHTTP2PingTimeout is the per-PING reply deadline.
// Sized small because PING is a lightweight protocol-level message —
// a dead conn announces itself fast, and a long timeout here just
// delays the recovery.
const FrameworkDefaultHTTP2PingTimeout = 5 * time.Second

// HTTPClientOptions configures the *http.Client used by OpenAI-compatible
// clients in the framework.
//
// Beta.43 reframed the field semantics: framework defaults now
// preempt Go's net/http defaults on three fields (IdleConnTimeout,
// MaxIdleConnsPerHost, HTTP/2 PING) because the long tail of
// stale-pooled-conn wedges through beta.34-42 proved the Go defaults
// are too forgiving for the LLM-gateway class. Operator-configured
// values still override the framework defaults, so per-endpoint
// tuning continues to work — and the tightened defaults mean the
// per-endpoint config is a knob for unusual cases, not a workaround
// for systemic class-of-bugs.
//
// Empty / zero opt-out semantics: every field accepts a sentinel
// value (empty string for durations, 0 for ints, false for bools)
// that means "use the framework default." Operator overrides set
// the field explicitly; tests use the helper unchanged.
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

// NewHTTPClient builds an *http.Client with framework-default Transport
// tuning. Operator-supplied options override the framework defaults
// per-field; sentinel values (empty string, 0, false) select the
// framework default.
//
// Duration fields with invalid values are silently ignored (the
// framework default applies). Validation is the responsibility of the
// caller (EndpointConfig goes through validateEndpoint at config load
// time).
//
// HTTP/2 PINGs are enabled via http2.ConfigureTransport so dead pooled
// connections to HTTP/2-supporting upstreams (cloud LLM gateways,
// Cloudflare-fronted routes) are detected and torn down without the
// operator having to disable keepalive. HTTP/1.1 upstreams (local
// inference servers) ignore PING; for those, the tightened
// IdleConnTimeout default (30s) is the operative protection.
func NewHTTPClient(opts HTTPClientOptions) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.DefaultTransport.(*http.Transport).Proxy,
		DialContext:           http.DefaultTransport.(*http.Transport).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       FrameworkDefaultIdleConnTimeout,
		MaxIdleConnsPerHost:   FrameworkDefaultMaxIdleConnsPerHost,
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

	// Configure HTTP/2 PING-based liveness check. ConfigureTransport
	// is a no-op for HTTP/1.1 connections — the http2.Transport sits
	// inside http.Transport and only activates on h2 negotiation. The
	// ReadIdleTimeout / PingTimeout fields are stamped onto the
	// returned http2.Transport, which the http.Transport delegates to
	// when the upstream negotiates h2.
	//
	// Error path: ConfigureTransport returns err only if the transport
	// has already been used (we just constructed it, so safe). Ignored
	// here — failure to enable PINGs falls back to the IdleConnTimeout
	// path, which is still tighter than Go's defaults.
	if h2Transport, err := http2.ConfigureTransports(transport); err == nil && h2Transport != nil {
		h2Transport.ReadIdleTimeout = FrameworkDefaultHTTP2ReadIdleTimeout
		h2Transport.PingTimeout = FrameworkDefaultHTTP2PingTimeout
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
