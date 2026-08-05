package executors

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	httpMaxResponseSize = 100 * 1024 // 100KB
	httpMaxTextSize     = 20000      // chars
	httpRequestTimeout  = 30 * time.Second

	// httpRequestTripleSource is the Source field on triples this tool
	// publishes; mirrors agent-web-search / coordinator-decide.
	httpRequestTripleSource = "agent-http-request"
)

// HTTPRequestExecutor handles http_request tool calls.
//
// Triple emission is optional (mirrors WebSearchExecutor): when a non-nil
// TriplePublisher is supplied via WithHTTPTriplePublisher, each
// successful 2xx/3xx fetch additionally emits a fixed set of predicates
// onto an agent.web.observation entity plus a back-link triple onto the
// calling loop entity. Non-2xx responses (≥400) do not emit — the
// graph claim is "we observed this URL's content" and a 4xx/5xx isn't
// that observation. Per-triple failures log + counter + continue
// (semstreams.agentic_tool_web.emit_failures_total).
type HTTPRequestExecutor struct {
	timeout time.Duration

	publisher agentictools.TriplePublisher
	platform  component.PlatformMeta
	logger    *slog.Logger
	now       func() time.Time
}

// HTTPRequestOption configures the executor.
type HTTPRequestOption func(*HTTPRequestExecutor)

// WithHTTPTimeout overrides the default request timeout (30s).
func WithHTTPTimeout(d time.Duration) HTTPRequestOption {
	return func(e *HTTPRequestExecutor) { e.timeout = d }
}

// WithHTTPTriplePublisher enables graph emission. nil disables emission
// (default).
func WithHTTPTriplePublisher(p agentictools.TriplePublisher) HTTPRequestOption {
	return func(e *HTTPRequestExecutor) { e.publisher = p }
}

// WithHTTPPlatform supplies the platform identity used to build
// observation entity IDs and resolve the calling loop's entity ID.
// Required when publisher is non-nil; ignored otherwise.
func WithHTTPPlatform(p component.PlatformMeta) HTTPRequestOption {
	return func(e *HTTPRequestExecutor) { e.platform = p }
}

// WithHTTPLogger replaces the default logger (slog.Default()). nil-safe.
func WithHTTPLogger(l *slog.Logger) HTTPRequestOption {
	return func(e *HTTPRequestExecutor) {
		if l != nil {
			e.logger = l
		}
	}
}

// WithHTTPClock replaces the time source the executor stamps onto
// fetched_at / triple timestamps. nil-safe.
func WithHTTPClock(now func() time.Time) HTTPRequestOption {
	return func(e *HTTPRequestExecutor) {
		if now != nil {
			e.now = now
		}
	}
}

// NewHTTPRequestExecutor creates an HTTP request executor.
func NewHTTPRequestExecutor(opts ...HTTPRequestOption) *HTTPRequestExecutor {
	e := &HTTPRequestExecutor{
		logger: slog.Default(),
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *HTTPRequestExecutor) effectiveTimeout() time.Duration {
	if e.timeout > 0 {
		return e.timeout
	}
	return httpRequestTimeout
}

// ListTools returns the http_request tool definition.
func (e *HTTPRequestExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "http_request",
			Description: "Fetch a URL and return its content. HTML is converted to readable text. Use for reading documentation, APIs, and web content.",
			Effect:      agentic.ToolEffectExternal,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Full URL including scheme, e.g. https://pkg.go.dev/net/http",
					},
					"method": map[string]any{
						"type":        "string",
						"description": "HTTP method: GET or POST (default: GET)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// Execute handles an http_request tool call.
func (e *HTTPRequestExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	rawURL, ok := call.Arguments["url"].(string)
	if !ok || rawURL == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "url is required"}, nil
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "url must start with http:// or https://",
		}, nil
	}

	// Single DNS resolution: resolve, validate (SSRF), and pin in one step.
	// This eliminates the TOCTOU window between SSRF check and HTTP dial.
	pinnedIP, err := httpResolveAndValidate(rawURL)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}

	method := "GET"
	if m, ok := call.Arguments["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}
	if method != "GET" && method != "POST" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "method must be GET or POST",
		}, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, nil)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("create request: %v", err),
		}, nil
	}
	req.Header.Set("User-Agent", "semstreams-agent/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	client := httpBuildPinnedClientFromIP(rawURL, pinnedIP, e.effectiveTimeout())
	resp, err := client.Do(req)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxResponseSize+1))
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("read response: %v", err),
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, httpTruncate(string(body), 500)),
		}, nil
	}

	content := string(body)
	truncated := false
	if len(content) > httpMaxTextSize {
		content = content[:httpMaxTextSize] + "\n[content truncated]"
		truncated = true
	}

	// Opportunistic graph emission. Detach from the caller's ctx so an
	// upstream cancellation doesn't half-write the batch, and so the
	// reqCtx's deferred cancel() doesn't kill the emission path the
	// moment Execute returns. Bounded timeout prevents a wedged
	// graph-ingest from leaking the goroutine.
	if e.publisher != nil {
		emitCtx, emitCancel := context.WithTimeout(context.WithoutCancel(ctx), webEmitTimeout)
		defer emitCancel()
		e.emitObservation(emitCtx, call, rawURL, resp, content, truncated)
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, content),
	}, nil
}

// emitObservation writes the URL-side observation entity plus a
// LoopFetchedWeb back-link onto the calling loop. Per-triple publish
// failures log + counter + continue; emission is additive observation,
// not the LLM-facing contract. Non-2xx responses never reach this code
// path — the executor returned earlier with an Error result and the
// graph claim ("we observed this URL's content") doesn't apply.
func (e *HTTPRequestExecutor) emitObservation(ctx context.Context, call agentic.ToolCall, rawURL string, resp *http.Response, body string, truncated bool) {
	if call.LoopID == "" {
		e.logger.Warn("http_request emission skipped: tool call missing loop_id",
			"call_id", call.ID, "url", rawURL)
		return
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		e.logger.Warn("http_request emission skipped: cannot resolve loop entity",
			"call_id", call.ID, "loop_id", call.LoopID, "error", err)
		return
	}
	urlEntity, canon, err := agentic.TryWebObservationEntityID(e.platform.Org, e.platform.Platform, rawURL)
	if err != nil {
		webEmitFailuresTotal.WithLabelValues("http_request", "entity_id").Inc()
		e.logger.Warn("http_request emission skipped: cannot build observation entity",
			"call_id", call.ID, "url", rawURL, "error", err)
		return
	}

	now := e.now()
	fetchedAt := now.UTC().Format(time.RFC3339Nano)
	contentType := resp.Header.Get("Content-Type")

	observationTriples := []message.Triple{
		{Subject: urlEntity, Predicate: agvocab.WebURL, Object: canon, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebFetchedAt, Object: fetchedAt, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebFetchedBy, Object: loopEntityID, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebContentType, Object: contentType, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebStatusCode, Object: resp.StatusCode, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebText, Object: body, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
		{Subject: urlEntity, Predicate: agvocab.WebTruncated, Object: truncated, Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0},
	}
	if err := publishWebObservation(ctx, e.publisher, urlEntity, observationTriples); err != nil {
		webEmitFailuresTotal.WithLabelValues("http_request", "publish").Inc()
		e.logger.Warn("http_request observation emission failed",
			"call_id", call.ID, "url", canon, "error", err)
		return
	}
	backlink := message.Triple{
		Subject: loopEntityID, Predicate: agvocab.LoopFetchedWeb, Object: urlEntity,
		Source: httpRequestTripleSource, Timestamp: now, Confidence: 1.0,
	}
	if err := e.publisher.Append(ctx, []message.Triple{backlink}); err != nil {
		webEmitFailuresTotal.WithLabelValues("http_request", "publish").Inc()
		e.logger.Warn("http_request backlink emission failed",
			"call_id", call.ID, "url", canon, "error", err)
	}
}

// httpResolveAndValidate performs DNS resolution and SSRF validation in a single
// step. Returns the validated IP to pin for the HTTP connection. This eliminates
// the TOCTOU window between SSRF check and HTTP dial that would allow DNS rebinding.
func httpResolveAndValidate(rawURL string) (net.IP, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}

	host := parsed.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for %s", host)
	}

	// Validate ALL resolved IPs (not just the one we'll use).
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("blocked: %s resolves to private/reserved IP %s", host, ip)
		}
	}

	// Pin the first validated IP.
	pinnedIP := ips[0]
	if v4 := pinnedIP.To4(); v4 != nil {
		pinnedIP = v4
	}
	return pinnedIP, nil
}

// httpBuildPinnedClientFromIP constructs an HTTP client that uses the pre-validated
// and pinned IP address, preventing DNS rebinding after validation.
func httpBuildPinnedClientFromIP(rawURL string, pinnedIP net.IP, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DisableKeepAlives: true, // per-request client, no connection reuse
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil || port == "" {
				port = "443"
				if strings.HasPrefix(rawURL, "http://") {
					port = "80"
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedIP.String(), port))
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Validate redirect targets via fresh DNS resolution + SSRF check.
			_, err := httpResolveAndValidate(req.URL.String())
			if err != nil {
				return err
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func httpTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
