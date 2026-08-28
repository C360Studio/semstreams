package executors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	httpMaxResponseSize = 100 * 1024 // 100KB
	httpMaxTextSize     = 20000      // bytes, before the truncation sentinel
	httpMaxTitleSize    = 512        // bytes
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

	lookupIP    func(context.Context, string) ([]net.IP, error)
	dialContext func(context.Context, string, string) (net.Conn, error)

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
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	e := &HTTPRequestExecutor{
		lookupIP: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
		dialContext: dialer.DialContext,
		logger:      slog.Default(),
		now:         time.Now,
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
			Description: "Fetch one URL and return bounded content with final-URL, content-type, and truncation metadata. Static HTML is converted to Markdown-like readable text; JavaScript is not executed.",
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
	if ctx == nil {
		return agentic.ToolResult{CallID: call.ID, Error: "context is required"}, nil
	}
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

	reqCtx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()

	// Resolve, validate, and retain the exact address set for the first dial.
	// Redirect targets repeat this operation in CheckRedirect before their dial.
	pinnedIPs, err := httpResolveAndValidateWithLookup(reqCtx, rawURL, e.lookupIP)
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

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, nil)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("create request: %v", err),
		}, nil
	}
	req.Header.Set("User-Agent", "semstreams-agent/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	client, err := httpBuildPinnedClient(rawURL, pinnedIPs, e.lookupIP, e.dialContext, e.effectiveTimeout())
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
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

	rawTruncated := len(body) > httpMaxResponseSize
	if rawTruncated {
		body = body[:httpMaxResponseSize]
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("HTTP %d: %s", resp.StatusCode, httpTruncate(string(body), 500)),
		}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	readable := httpReadableResponse(body, contentType, rawTruncated)
	finalURL := resp.Request.URL.String()

	// Opportunistic graph emission. Detach from the caller's ctx so an
	// upstream cancellation doesn't half-write the batch, and so the
	// reqCtx's deferred cancel() doesn't kill the emission path the
	// moment Execute returns. Bounded timeout prevents a wedged
	// graph-ingest from leaking the goroutine.
	if e.publisher != nil {
		emitCtx, emitCancel := context.WithTimeout(context.WithoutCancel(ctx), webEmitTimeout)
		defer emitCancel()
		e.emitObservation(emitCtx, call, finalURL, resp, readable.Text, readable.Truncated)
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: httpFormatReadableResult(resp.StatusCode, finalURL, contentType, readable),
	}, nil
}

type httpReadable struct {
	Text      string
	Title     string
	Transform string
	Truncated bool
}

func httpReadableResponse(body []byte, contentType string, rawTruncated bool) httpReadable {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if strings.EqualFold(mediaType, "text/html") || strings.EqualFold(mediaType, "application/xhtml+xml") {
		text, textTruncated := httpHTMLToMarkdown(bytes.NewReader(body), httpMaxTextSize)
		if rawTruncated || textTruncated {
			text = httpWithTruncationSentinel(text)
		}
		return httpReadable{
			Text:      text,
			Title:     httpTruncate(httpExtractHTMLTitle(bytes.NewReader(body)), httpMaxTitleSize),
			Transform: "html-to-markdown",
			Truncated: rawTruncated || textTruncated,
		}
	}

	text := string(body)
	textTruncated := len(text) > httpMaxTextSize
	if textTruncated {
		text = text[:httpMaxTextSize]
	}
	if rawTruncated || textTruncated {
		text = httpWithTruncationSentinel(text)
	}
	return httpReadable{
		Text:      text,
		Transform: "raw",
		Truncated: rawTruncated || textTruncated,
	}
}

func httpWithTruncationSentinel(text string) string {
	if text == "" {
		return "[content truncated]"
	}
	return strings.TrimRight(text, "\n") + "\n[content truncated]"
}

func httpFormatReadableResult(statusCode int, finalURL, contentType string, readable httpReadable) string {
	var result strings.Builder
	fmt.Fprintf(&result, "HTTP %d\nFinal-URL: %s\nContent-Type: %s\nContent-Transform: %s\nTruncated: %t\n",
		statusCode, finalURL, contentType, readable.Transform, readable.Truncated)
	if readable.Title != "" {
		fmt.Fprintf(&result, "Title: %s\n", readable.Title)
	}
	result.WriteByte('\n')
	result.WriteString(readable.Text)
	return result.String()
}

func httpHTMLToMarkdown(r io.Reader, maxBytes int) (string, bool) {
	tokenizer := html.NewTokenizer(r)
	var result strings.Builder
	skipDepth := 0
	truncated := false

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if result.Len() >= maxBytes {
			truncated = true
			break
		}

		switch tokenType {
		case html.StartTagToken:
			tagName, _ := tokenizer.TagName()
			tag := atom.Lookup(tagName)
			if httpSkippedHTMLTag(tag) {
				skipDepth++
				continue
			}
			if skipDepth > 0 {
				continue
			}
			switch tag {
			case atom.H1:
				result.WriteString("\n# ")
			case atom.H2:
				result.WriteString("\n## ")
			case atom.H3:
				result.WriteString("\n### ")
			case atom.H4:
				result.WriteString("\n#### ")
			case atom.H5:
				result.WriteString("\n##### ")
			case atom.H6:
				result.WriteString("\n###### ")
			case atom.Li:
				result.WriteString("\n- ")
			case atom.Br:
				result.WriteByte('\n')
			default:
				if httpBlockHTMLTag(tag) {
					result.WriteByte('\n')
				}
			}
		case html.EndTagToken:
			tagName, _ := tokenizer.TagName()
			tag := atom.Lookup(tagName)
			if httpSkippedHTMLTag(tag) && skipDepth > 0 {
				skipDepth--
				continue
			}
			if skipDepth == 0 && httpBlockHTMLTag(tag) {
				result.WriteByte('\n')
			}
		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := httpNormalizeWhitespace(strings.TrimSpace(string(tokenizer.Text())))
			if text == "" {
				continue
			}
			remaining := maxBytes - result.Len()
			if remaining <= 0 {
				truncated = true
				return httpCollapseNewlines(strings.TrimSpace(result.String())), truncated
			}
			if len(text)+1 > remaining {
				result.WriteString(text[:remaining])
				truncated = true
				return httpCollapseNewlines(strings.TrimSpace(result.String())), truncated
			}
			result.WriteString(text)
			result.WriteByte(' ')
		}
	}

	text := httpCollapseNewlines(strings.TrimSpace(result.String()))
	if len(text) > maxBytes {
		text = text[:maxBytes]
		truncated = true
	}
	return text, truncated
}

func httpExtractHTMLTitle(r io.Reader) string {
	tokenizer := html.NewTokenizer(r)
	inTitle := false
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			tagName, _ := tokenizer.TagName()
			inTitle = atom.Lookup(tagName) == atom.Title
		case html.TextToken:
			if inTitle {
				return httpNormalizeWhitespace(strings.TrimSpace(string(tokenizer.Text())))
			}
		case html.EndTagToken:
			if inTitle {
				return ""
			}
		}
	}
}

func httpSkippedHTMLTag(tag atom.Atom) bool {
	switch tag {
	case atom.Script, atom.Style, atom.Nav, atom.Footer, atom.Header, atom.Noscript:
		return true
	default:
		return false
	}
}

func httpBlockHTMLTag(tag atom.Atom) bool {
	switch tag {
	case atom.P, atom.Div, atom.Br, atom.Tr, atom.Blockquote, atom.Pre, atom.Section, atom.Article, atom.Li,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return true
	default:
		return false
	}
}

func httpNormalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func httpCollapseNewlines(text string) string {
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
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

	// The registered observation entity is the one builder of its triples
	// (ADR-103); Tool selects the http_request source and predicate set.
	observation := &agentic.WebObservationEntity{
		Org: e.platform.Org, Platform: e.platform.Platform, CanonicalURL: canon,
		Tool: agentic.WebObservationToolHTTPRequest, LoopEntityID: loopEntityID,
		FetchedAt: fetchedAt, ContentType: contentType, StatusCode: resp.StatusCode,
		Text: body, Truncated: truncated,
	}
	if err := observation.Validate(); err != nil {
		webEmitFailuresTotal.WithLabelValues("http_request", "validate").Inc()
		e.logger.Warn("http_request emission skipped: observation fails its contract",
			"call_id", call.ID, "url", canon, "error", err)
		return
	}
	if err := publishWebObservation(ctx, e.publisher, urlEntity, observation.Triples()); err != nil {
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

func httpResolveAndValidateWithLookup(
	ctx context.Context,
	rawURL string,
	lookupIP func(context.Context, string) ([]net.IP, error),
) ([]net.IP, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL policy: unsupported scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("invalid URL: host is required")
	}

	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		ips, err = lookupIP(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for %s", host)
	}

	return httpValidateResolvedIPs(host, ips)
}

func httpValidateResolvedIPs(host string, ips []net.IP) ([]net.IP, error) {
	validated := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if httpBlockedIP(ip) {
			return nil, fmt.Errorf("blocked: %s resolves to private/reserved IP %s", host, ip)
		}
		validated = append(validated, ip)
	}
	return validated, nil
}

func httpBlockedIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return true
	}
	for _, network := range httpReservedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

var httpReservedNetworks = []*net.IPNet{
	httpMustCIDR("0.0.0.0/8"),
	httpMustCIDR("100.64.0.0/10"),
	httpMustCIDR("192.0.0.0/24"),
	httpMustCIDR("192.0.2.0/24"),
	httpMustCIDR("192.88.99.0/24"),
	httpMustCIDR("198.18.0.0/15"),
	httpMustCIDR("198.51.100.0/24"),
	httpMustCIDR("203.0.113.0/24"),
	httpMustCIDR("240.0.0.0/4"),
	httpMustCIDR("64:ff9b::/96"),
	httpMustCIDR("64:ff9b:1::/48"),
	httpMustCIDR("100::/64"),
	httpMustCIDR("2001::/23"),
	httpMustCIDR("2001:db8::/32"),
	httpMustCIDR("2002::/16"),
}

func httpMustCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid HTTP SSRF CIDR %q: %v", cidr, err))
	}
	return network
}

type httpPinnedAddresses struct {
	mu        sync.Mutex
	addresses map[string][]net.IP
}

func (p *httpPinnedAddresses) put(authority string, ips []net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addresses[authority] = append([]net.IP(nil), ips...)
}

func (p *httpPinnedAddresses) get(authority string) []net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]net.IP(nil), p.addresses[authority]...)
}

func httpBuildPinnedClient(
	rawURL string,
	pinnedIPs []net.IP,
	lookupIP func(context.Context, string) ([]net.IP, error),
	dialContext func(context.Context, string, string) (net.Conn, error),
	timeout time.Duration,
) (*http.Client, error) {
	initialURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}
	initialAuthority, err := httpURLAuthority(initialURL)
	if err != nil {
		return nil, err
	}
	pins := &httpPinnedAddresses{addresses: make(map[string][]net.IP)}
	pins.put(initialAuthority, pinnedIPs)

	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			canonicalAddr, canonicalErr := httpCanonicalAuthority(addr)
			if canonicalErr != nil {
				return nil, fmt.Errorf("request policy: %w", canonicalErr)
			}
			ips := pins.get(canonicalAddr)
			if len(ips) == 0 {
				return nil, fmt.Errorf("request policy: no validated address for %s", canonicalAddr)
			}
			_, port, err := net.SplitHostPort(canonicalAddr)
			if err != nil {
				return nil, fmt.Errorf("request policy: invalid dial authority %q: %w", addr, err)
			}
			var lastErr error
			for _, ip := range ips {
				validatedAddr := net.JoinHostPort(ip.String(), port)
				conn, dialErr := dialContext(ctx, network, validatedAddr)
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("redirect policy: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect policy: unsupported scheme %q", req.URL.Scheme)
			}
			redirectIPs, resolveErr := httpResolveAndValidateWithLookup(req.Context(), req.URL.String(), lookupIP)
			if resolveErr != nil {
				return fmt.Errorf("redirect policy: %w", resolveErr)
			}
			authority, authorityErr := httpURLAuthority(req.URL)
			if authorityErr != nil {
				return fmt.Errorf("redirect policy: %w", authorityErr)
			}
			pins.put(authority, redirectIPs)
			return nil
		},
	}, nil
}

func httpURLAuthority(parsed *url.URL) (string, error) {
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("invalid URL: host is required")
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
		}
	}
	return net.JoinHostPort(strings.ToLower(host), port), nil
}

func httpCanonicalAuthority(authority string) (string, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		return "", fmt.Errorf("invalid dial authority %q: %w", authority, err)
	}
	return net.JoinHostPort(strings.ToLower(host), port), nil
}

func httpTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
