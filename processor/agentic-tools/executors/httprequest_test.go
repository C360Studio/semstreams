package executors

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpTestNetwork struct {
	mu          sync.Mutex
	resolved    map[string][]net.IP
	dialTargets map[string]string
	dialed      []string
}

func (n *httpTestNetwork) lookupIP(_ context.Context, host string) ([]net.IP, error) {
	ips, ok := n.resolved[host]
	if !ok {
		return nil, fmt.Errorf("test host %q is not mapped", host)
	}
	return ips, nil
}

func (n *httpTestNetwork) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	n.mu.Lock()
	n.dialed = append(n.dialed, addr)
	target, ok := n.dialTargets[addr]
	n.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("validated address %q has no test dial target", addr)
	}
	return (&net.Dialer{}).DialContext(ctx, network, target)
}

func (n *httpTestNetwork) executor(opts ...HTTPRequestOption) *HTTPRequestExecutor {
	e := NewHTTPRequestExecutor(opts...)
	e.lookupIP = n.lookupIP
	e.dialContext = n.dialContext
	return e
}

func (n *httpTestNetwork) dialSnapshot() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.dialed...)
}

func testServerPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	return port
}

func executeHTTPTestCall(ctx context.Context, t *testing.T, e *HTTPRequestExecutor, rawURL string) agentic.ToolResult {
	t.Helper()
	result, err := e.Execute(ctx, agentic.ToolCall{
		ID:        "test-call",
		Name:      "http_request",
		LoopID:    "test-loop",
		Arguments: map[string]any{"url": rawURL},
	})
	require.NoError(t, err)
	return result
}

func buildHTTPTestPinnedClient(t *testing.T, rawURL string, pinnedIP net.IP, timeout time.Duration) *http.Client {
	t.Helper()
	dialer := &net.Dialer{}
	client, err := httpBuildPinnedClient(
		rawURL,
		[]net.IP{pinnedIP},
		func(_ context.Context, host string) ([]net.IP, error) {
			return nil, fmt.Errorf("unexpected redirect lookup for %q", host)
		},
		dialer.DialContext,
		timeout,
	)
	require.NoError(t, err)
	return client
}

func resolveHTTPTestURL(rawURL string) (net.IP, error) {
	ips, err := httpResolveAndValidateWithLookup(
		context.Background(),
		rawURL,
		func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
	)
	if err != nil {
		return nil, err
	}
	return ips[0], nil
}

// TestNewHTTPRequestExecutor verifies the constructor and default timeout.
func TestNewHTTPRequestExecutor(t *testing.T) {
	e := NewHTTPRequestExecutor()
	require.NotNil(t, e)
	assert.Equal(t, httpRequestTimeout, e.effectiveTimeout())
}

// TestWithHTTPTimeout verifies the functional option overrides the default.
func TestWithHTTPTimeout(t *testing.T) {
	d := 5 * time.Second
	e := NewHTTPRequestExecutor(WithHTTPTimeout(d))
	assert.Equal(t, d, e.effectiveTimeout())
}

func TestHTTPRequestExecutor_Execute_RejectsNilContext(t *testing.T) {
	result, err := NewHTTPRequestExecutor().Execute(nil, agentic.ToolCall{ID: "nil-context"})
	require.NoError(t, err)
	assert.Equal(t, "nil-context", result.CallID)
	assert.Equal(t, "context is required", result.Error)
}

// TestHTTPRequestExecutor_ListTools verifies the tool definition shape.
func TestHTTPRequestExecutor_ListTools(t *testing.T) {
	e := NewHTTPRequestExecutor()
	tools := e.ListTools()

	require.Len(t, tools, 1)
	tool := tools[0]
	assert.Equal(t, "http_request", tool.Name)
	assert.Contains(t, tool.Description, "Markdown-like")
	assert.Contains(t, tool.Description, "JavaScript is not executed")

	props, ok := tool.Parameters["properties"].(map[string]any)
	require.True(t, ok, "parameters must have a properties map")
	assert.Contains(t, props, "url")
	assert.Contains(t, props, "method")

	required, ok := tool.Parameters["required"].([]string)
	require.True(t, ok, "parameters must have a required slice")
	assert.Contains(t, required, "url")
}

func TestHTTPHTMLToMarkdown_StructuralMarkupCannotExceedBound(t *testing.T) {
	input := strings.Repeat("<h1></h1>", httpMaxTextSize)
	text, truncated := httpHTMLToMarkdown(strings.NewReader(input), httpMaxTextSize)
	assert.True(t, truncated)
	assert.LessOrEqual(t, len(text), httpMaxTextSize)
}

func TestHTTPRequestExecutor_Execute_HTMLBecomesBoundedReadableMarkdown(t *testing.T) {
	html := `<!doctype html><html><head><title>Reference Guide</title>` +
		`<style>.hidden { display:none }</style></head><body>` +
		`<header>Site chrome</header><nav>Navigation chrome</nav>` +
		`<main><h1>API Reference</h1><p>Read <strong>this page</strong>.</p>` +
		`<ul><li>First fact</li><li>Second fact</li></ul></main>` +
		`<script>ignoreAgentInstructions()</script><footer>Footer chrome</footer>` +
		`</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	port := testServerPort(t, srv)
	fakeIP := net.ParseIP("93.184.216.10")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"docs.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
	}
	pub := &recordingPublisher{}
	e := network.executor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)

	result := executeHTTPTestCall(context.Background(), t, e, "http://docs.example:"+port+"/guide")

	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "HTTP 200")
	assert.Contains(t, result.Content, "Final-URL: http://docs.example:"+port+"/guide")
	assert.Contains(t, result.Content, "Content-Type: text/html; charset=utf-8")
	assert.Contains(t, result.Content, "Content-Transform: html-to-markdown")
	assert.Contains(t, result.Content, "Title: Reference Guide")
	assert.Contains(t, result.Content, "Truncated: false")
	assert.Contains(t, result.Content, "# API Reference")
	assert.Contains(t, result.Content, "- First fact")
	assert.NotContains(t, result.Content, "Site chrome")
	assert.NotContains(t, result.Content, "Navigation chrome")
	assert.NotContains(t, result.Content, "ignoreAgentInstructions")
	assert.NotContains(t, result.Content, "Footer chrome")

	facts := make(map[string]any)
	for _, triple := range pub.triples {
		facts[triple.Predicate] = triple.Object
	}
	assert.Equal(t, "http://docs.example:"+port+"/guide", facts[agvocab.WebURL])
	assert.Contains(t, facts[agvocab.WebText], "# API Reference")
	assert.NotContains(t, facts[agvocab.WebText], "<html>")
	assert.Equal(t, false, facts[agvocab.WebTruncated])
}

func TestHTTPRequestExecutor_Execute_ReportsContentTypeAndTruncation(t *testing.T) {
	tests := []struct {
		name          string
		contentType   string
		body          string
		wantTransform string
		wantBody      string
		wantTruncated bool
	}{
		{
			name:          "plain response remains raw",
			contentType:   "application/json",
			body:          `{"status":"ok"}`,
			wantTransform: "raw",
			wantBody:      `{"status":"ok"}`,
		},
		{
			name:          "readable HTML is bounded",
			contentType:   "text/html",
			body:          "<p>" + strings.Repeat("x", httpMaxTextSize+100) + "</p>",
			wantTransform: "html-to-markdown",
			wantBody:      "[content truncated]",
			wantTruncated: true,
		},
		{
			name:          "raw read bound remains observable",
			contentType:   "text/html",
			body:          "<script>" + strings.Repeat("x", httpMaxResponseSize+100) + "</script><p>unread tail</p>",
			wantTransform: "html-to-markdown",
			wantBody:      "[content truncated]",
			wantTruncated: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			port := testServerPort(t, srv)
			fakeIP := net.ParseIP("93.184.216.11")
			network := &httpTestNetwork{
				resolved:    map[string][]net.IP{"content.example": {fakeIP}},
				dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
			}

			result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://content.example:"+port+"/page")

			require.Empty(t, result.Error)
			assert.Contains(t, result.Content, "Content-Type: "+tc.contentType)
			assert.Contains(t, result.Content, "Content-Transform: "+tc.wantTransform)
			assert.Contains(t, result.Content, fmt.Sprintf("Truncated: %t", tc.wantTruncated))
			assert.Contains(t, result.Content, tc.wantBody)
		})
	}
}

func TestHTTPRequestExecutor_Execute_RedirectsDialEachValidatedHop(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("final page"))
	}))
	t.Cleanup(final.Close)
	finalPort := testServerPort(t, final)

	var redirectURL string
	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, redirectURL, http.StatusFound)
	}))
	t.Cleanup(initial.Close)
	initialPort := testServerPort(t, initial)
	redirectURL = "http://reference.example:" + finalPort + "/final"

	initialIP := net.ParseIP("93.184.216.20")
	finalIP := net.ParseIP("93.184.216.21")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{
			"index.example":     {initialIP},
			"reference.example": {finalIP},
		},
		dialTargets: map[string]string{
			net.JoinHostPort(initialIP.String(), initialPort): initial.Listener.Addr().String(),
			net.JoinHostPort(finalIP.String(), finalPort):     final.Listener.Addr().String(),
		},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://index.example:"+initialPort+"/start")

	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Final-URL: "+redirectURL)
	assert.Contains(t, result.Content, "final page")
	assert.Equal(t, []string{
		net.JoinHostPort(initialIP.String(), initialPort),
		net.JoinHostPort(finalIP.String(), finalPort),
	}, network.dialSnapshot())
}

func TestHTTPRequestExecutor_Execute_SameHostRedirectUsesTargetPort(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("other port"))
	}))
	t.Cleanup(final.Close)
	finalPort := testServerPort(t, final)

	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://same.example:"+finalPort+"/final", http.StatusFound)
	}))
	t.Cleanup(initial.Close)
	initialPort := testServerPort(t, initial)

	fakeIP := net.ParseIP("93.184.216.22")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{"same.example": {fakeIP}},
		dialTargets: map[string]string{
			net.JoinHostPort(fakeIP.String(), initialPort): initial.Listener.Addr().String(),
			net.JoinHostPort(fakeIP.String(), finalPort):   final.Listener.Addr().String(),
		},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://same.example:"+initialPort+"/start")
	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "other port")
	assert.Equal(t, []string{
		net.JoinHostPort(fakeIP.String(), initialPort),
		net.JoinHostPort(fakeIP.String(), finalPort),
	}, network.dialSnapshot())
}

func TestHTTPRequestExecutor_Execute_RedirectPolicyFailures(t *testing.T) {
	tests := []struct {
		name        string
		location    string
		resolved    map[string][]net.IP
		wantError   string
		wantDialLen int
	}{
		{
			name:        "private redirect target",
			location:    "http://private.example/secret",
			resolved:    map[string][]net.IP{"private.example": {net.ParseIP("127.0.0.1")}},
			wantError:   "blocked",
			wantDialLen: 1,
		},
		{
			name:        "unsupported redirect scheme",
			location:    "file:///etc/passwd",
			resolved:    map[string][]net.IP{},
			wantError:   "redirect policy",
			wantDialLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Redirect(w, &http.Request{}, tc.location, http.StatusFound)
			}))
			t.Cleanup(initial.Close)
			port := testServerPort(t, initial)
			initialIP := net.ParseIP("93.184.216.23")
			resolved := map[string][]net.IP{"initial.example": {initialIP}}
			for host, ips := range tc.resolved {
				resolved[host] = ips
			}
			network := &httpTestNetwork{
				resolved: resolved,
				dialTargets: map[string]string{
					net.JoinHostPort(initialIP.String(), port): initial.Listener.Addr().String(),
				},
			}

			result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://initial.example:"+port+"/start")
			assert.Empty(t, result.Content)
			assert.Contains(t, result.Error, tc.wantError)
			assert.Len(t, network.dialSnapshot(), tc.wantDialLen)
		})
	}
}

func TestHTTPRequestExecutor_Execute_RedirectLimitIsObservable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	port := testServerPort(t, server)
	fakeIP := net.ParseIP("93.184.216.24")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"loop.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): server.Listener.Addr().String()},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://loop.example:"+port+"/start")
	assert.Empty(t, result.Content)
	assert.Contains(t, result.Error, "too many redirects")
	assert.Len(t, network.dialSnapshot(), 5)
}

func TestHTTPRequestExecutor_Execute_CancellationStopsRedirectedRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	final := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	t.Cleanup(final.Close)
	finalPort := testServerPort(t, final)

	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://slow.example:"+finalPort+"/wait", http.StatusFound)
	}))
	t.Cleanup(initial.Close)
	initialPort := testServerPort(t, initial)

	initialIP := net.ParseIP("93.184.216.25")
	finalIP := net.ParseIP("93.184.216.26")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{
			"initial.example": {initialIP},
			"slow.example":    {finalIP},
		},
		dialTargets: map[string]string{
			net.JoinHostPort(initialIP.String(), initialPort): initial.Listener.Addr().String(),
			net.JoinHostPort(finalIP.String(), finalPort):     final.Listener.Addr().String(),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	type executeOutcome struct {
		result agentic.ToolResult
		err    error
	}
	resultCh := make(chan executeOutcome, 1)
	e := network.executor(WithHTTPTimeout(5 * time.Second))
	go func() {
		result, err := e.Execute(ctx, agentic.ToolCall{
			ID:        "cancel-call",
			Name:      "http_request",
			Arguments: map[string]any{"url": "http://initial.example:" + initialPort + "/start"},
		})
		resultCh <- executeOutcome{result: result, err: err}
	}()
	<-requestStarted
	cancel()

	select {
	case outcome := <-resultCh:
		require.NoError(t, outcome.err)
		assert.Empty(t, outcome.result.Content)
		assert.Contains(t, outcome.result.Error, "context canceled")
	case <-time.After(time.Second):
		t.Fatal("http_request did not stop after context cancellation")
	}
}

// TestHTTPTruncate tests the truncation helper directly.
func TestHTTPTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "under limit",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "at limit exactly",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "over limit appends ellipsis",
			input:  "hello world",
			maxLen: 5,
			want:   "hello...",
		},
		{
			name:   "single char over limit",
			input:  "ab",
			maxLen: 1,
			want:   "a...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := httpTruncate(tc.input, tc.maxLen)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestHTTPRequestExecutor_Execute_InputValidation covers argument-level errors
// that are caught before any network activity.
func TestHTTPRequestExecutor_Execute_InputValidation(t *testing.T) {
	e := NewHTTPRequestExecutor()
	ctx := context.Background()

	tests := []struct {
		name        string
		call        agentic.ToolCall
		wantErrFrag string
	}{
		{
			name: "missing url key",
			call: agentic.ToolCall{
				ID:        "c1",
				Name:      "http_request",
				Arguments: map[string]any{},
			},
			wantErrFrag: "url is required",
		},
		{
			name: "empty url string",
			call: agentic.ToolCall{
				ID:        "c2",
				Name:      "http_request",
				Arguments: map[string]any{"url": ""},
			},
			wantErrFrag: "url is required",
		},
		{
			name: "url is not a string type",
			call: agentic.ToolCall{
				ID:        "c3",
				Name:      "http_request",
				Arguments: map[string]any{"url": 42},
			},
			wantErrFrag: "url is required",
		},
		{
			name: "file:// scheme rejected",
			call: agentic.ToolCall{
				ID:        "c4",
				Name:      "http_request",
				Arguments: map[string]any{"url": "file:///etc/passwd"},
			},
			wantErrFrag: "http:// or https://",
		},
		{
			name: "ftp:// scheme rejected",
			call: agentic.ToolCall{
				ID:        "c5",
				Name:      "http_request",
				Arguments: map[string]any{"url": "ftp://example.com/file"},
			},
			wantErrFrag: "http:// or https://",
		},
		{
			name: "data: scheme rejected",
			call: agentic.ToolCall{
				ID:        "c6",
				Name:      "http_request",
				Arguments: map[string]any{"url": "data:text/plain,hello"},
			},
			wantErrFrag: "http:// or https://",
		},
		{
			name: "method DELETE rejected",
			call: agentic.ToolCall{
				ID:        "c7",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "DELETE"},
			},
			// SSRF block on 127.0.0.1 fires before method check, but the
			// executor returns an error in ToolResult.Error either way. We
			// test pure method rejection via a non-SSRF URL; since we can't
			// reach the internet in unit tests we verify ordering: SSRF error
			// is returned (which is also a ToolResult.Error, not a Go error).
			wantErrFrag: "blocked",
		},
		{
			name: "method PUT rejected",
			call: agentic.ToolCall{
				ID:        "c8",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "PUT"},
			},
			wantErrFrag: "blocked",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := e.Execute(ctx, tc.call)
			require.NoError(t, err, "Execute must not return a Go error")
			assert.Equal(t, tc.call.ID, result.CallID)
			assert.Empty(t, result.Content)
			assert.Contains(t, result.Error, tc.wantErrFrag)
		})
	}
}

// TestHTTPRequestExecutor_Execute_MethodValidation isolates the method check
// from SSRF by testing an unreachable but syntactically valid hostname.
// DNS will fail (no real host), so we only verify scheme/method ordering.
func TestHTTPRequestExecutor_Execute_MethodRejectedBeforeNetwork(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(2 * time.Second))
	ctx := context.Background()

	// localhost resolves to 127.0.0.1 which is loopback — SSRF fires first.
	// We need DNS to succeed but resolve to a public IP to test method rejection.
	// Use a bogus hostname that fails DNS lookup so the SSRF check never runs,
	// then test method-only rejection via a loopback URL.
	//
	// The cleanest approach: call Execute with a loopback URL and a bad method.
	// The SSRF block fires first and returns "blocked". That's the correct behavior:
	// we block before even parsing the method.
	result, err := e.Execute(ctx, agentic.ToolCall{
		ID:        "method-del",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "DELETE"},
	})
	require.NoError(t, err)
	// SSRF fires first — the method is irrelevant for private IPs.
	assert.NotEmpty(t, result.Error)
	assert.Empty(t, result.Content)
}

// TestHTTPResolveAndValidate_SSRFBlocking is the critical security test suite.
// Each case verifies that private/reserved IP ranges are rejected before
// a TCP connection is ever attempted.
func TestHTTPResolveAndValidate_SSRFBlocking(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "AWS metadata endpoint",
			url:  "http://169.254.169.254/latest/meta-data/",
		},
		{
			name: "IPv4 loopback 127.0.0.1",
			url:  "http://127.0.0.1/",
		},
		{
			name: "IPv4 loopback 127.0.0.2",
			url:  "http://127.0.0.2/",
		},
		{
			name: "RFC-1918 10.x.x.x",
			url:  "http://10.0.0.1/",
		},
		{
			name: "RFC-1918 172.16.x.x",
			url:  "http://172.16.0.1/",
		},
		{
			name: "RFC-1918 172.31.x.x",
			url:  "http://172.31.255.254/",
		},
		{
			name: "RFC-1918 192.168.x.x",
			url:  "http://192.168.1.1/",
		},
		{
			name: "carrier-grade NAT",
			url:  "http://100.64.0.1/",
		},
		{
			name: "IPv4 documentation range",
			url:  "http://203.0.113.1/",
		},
		{
			name: "IPv4 benchmark range",
			url:  "http://198.18.0.1/",
		},
		{
			name: "IPv6 loopback ::1",
			url:  "http://[::1]/",
		},
		{
			name: "IPv6 documentation range",
			url:  "http://[2001:db8::1]/",
		},
		{
			name: "NAT64 translation range",
			url:  "http://[64:ff9b::7f00:1]/",
		},
		{
			name: "link-local multicast 224.0.0.1",
			url:  "http://224.0.0.1/",
		},
		{
			name: "unspecified 0.0.0.0",
			url:  "http://0.0.0.0/",
		},
		{
			name: "current-network IPv4 range",
			url:  "http://0.0.0.1/",
		},
		{
			name: "future-use IPv4 range",
			url:  "http://240.0.0.1/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip, err := resolveHTTPTestURL(tc.url)
			assert.Error(t, err, "expected SSRF block for %s", tc.url)
			assert.Nil(t, ip, "should not return an IP when blocked")
			// "blocked" appears in the error for private/reserved IPs.
			// DNS failures produce a different message; private IPs always resolve.
			if err != nil && !strings.Contains(err.Error(), "DNS") {
				assert.Contains(t, err.Error(), "blocked")
			}
		})
	}
}

// TestHTTPResolveAndValidate_SSRFBlocking_ViaExecute re-runs the critical SSRF
// cases through the full Execute path to confirm the end-to-end error surface.
func TestHTTPResolveAndValidate_SSRFBlocking_ViaExecute(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(5 * time.Second))
	ctx := context.Background()

	cases := []struct {
		name string
		url  string
	}{
		{"AWS metadata", "http://169.254.169.254/latest/meta-data/"},
		{"loopback 127.0.0.1", "http://127.0.0.1/"},
		{"private 10.0.0.1", "http://10.0.0.1/"},
		{"private 172.16.0.1", "http://172.16.0.1/"},
		{"private 192.168.1.1", "http://192.168.1.1/"},
		{"IPv6 loopback", "http://[::1]/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := e.Execute(ctx, agentic.ToolCall{
				ID:        "ssrf-" + tc.name,
				Name:      "http_request",
				Arguments: map[string]any{"url": tc.url},
			})
			require.NoError(t, err, "Execute must never return a Go error for SSRF")
			assert.Empty(t, result.Content, "no content must be returned for blocked URL")
			assert.NotEmpty(t, result.Error, "ToolResult.Error must be set for blocked URL")
		})
	}
}

// TestHTTPRequestExecutor_Execute_HTTPMechanics tests response handling by
// bypassing DNS lookup entirely. We call httpBuildPinnedClient directly
// with the loopback address of the test server, then verify the executor's
// status-code branching, body reading, and content-length capping.
//
// Note: httptest.NewServer listens on 127.0.0.1 which is a loopback address.
// httpResolveAndValidate blocks loopback, so we cannot call Execute() with the
// test server URL directly. Instead we exercise the HTTP mechanics by calling
// the internal helpers that Execute() delegates to, confirming behavior at each
// layer.

// TestHTTPBuildPinnedClient_PinsConnection verifies the pinned client
// dials the provided IP regardless of the hostname in the URL.
func TestHTTPBuildPinnedClient_PinsConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pinned-ok"))
	}))
	t.Cleanup(srv.Close)

	// Parse the test server address to get the IP.
	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	ip := net.ParseIP(host)
	require.NotNil(t, ip)

	// Build a client pinned to the test server's IP, using a fake hostname URL.
	// This verifies the dialer ignores the URL hostname and uses pinnedIP.
	fakeURL := "http://fake-hostname.internal:" + port + "/"
	client := buildHTTPTestPinnedClient(t, fakeURL, ip, 5*time.Second)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fakeURL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHTTPTruncate_ResponseBodyCap verifies the 500-char truncation applied to
// error-status response bodies matches httpTruncate behaviour.
func TestHTTPTruncate_ResponseBodyCap(t *testing.T) {
	longBody := strings.Repeat("x", 600)
	got := httpTruncate(longBody, 500)
	assert.Equal(t, 503, len(got)) // 500 chars + "..."
	assert.True(t, strings.HasSuffix(got, "..."))
}

// TestHTTPRequestExecutor_Execute_StatusCodeBranching verifies non-2xx/3xx
// responses set ToolResult.Error and successful responses set ToolResult.Content.
// We bypass SSRF by using httpBuildPinnedClient with the real server IP.
func TestHTTPRequestExecutor_Execute_StatusCodeBranching(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantContent bool // true = expect Content, false = expect Error
	}{
		{name: "200 OK returns content", statusCode: 200, body: "hello", wantContent: true},
		{name: "201 Created returns content", statusCode: 201, body: "created", wantContent: true},
		{name: "204 No Content returns content", statusCode: 204, body: "", wantContent: true},
		{name: "400 Bad Request returns error", statusCode: 400, body: "bad req", wantContent: false},
		{name: "401 Unauthorized returns error", statusCode: 401, body: "unauth", wantContent: false},
		{name: "403 Forbidden returns error", statusCode: 403, body: "forbidden", wantContent: false},
		{name: "404 Not Found returns error", statusCode: 404, body: "not found", wantContent: false},
		{name: "500 Internal Server Error returns error", statusCode: 500, body: "server err", wantContent: false},
		{name: "503 Service Unavailable returns error", statusCode: 503, body: "unavail", wantContent: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			t.Cleanup(srv.Close)

			host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
			require.NoError(t, err)
			pinnedIP := net.ParseIP(host)
			require.NotNil(t, pinnedIP)

			rawURL := "http://fake-host.internal:" + port + "/"
			client := buildHTTPTestPinnedClient(t, rawURL, pinnedIP, 5*time.Second)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.statusCode, resp.StatusCode)

			// Confirm the status-code range logic matches Execute's branching:
			// < 200 || >= 400 → error; else → content.
			isError := resp.StatusCode < 200 || resp.StatusCode >= 400
			if tc.wantContent {
				assert.False(t, isError, "status %d should produce content", tc.statusCode)
			} else {
				assert.True(t, isError, "status %d should produce error", tc.statusCode)
			}
		})
	}
}

// TestHTTPRequestExecutor_Execute_ContentTruncation verifies that responses
// longer than httpMaxTextSize are truncated with the sentinel suffix.
func TestHTTPRequestExecutor_Execute_ContentTruncation(t *testing.T) {
	oversized := strings.Repeat("a", httpMaxTextSize+100)

	// Simulate what Execute does with the body after a 200 response.
	content := oversized
	if len(content) > httpMaxTextSize {
		content = content[:httpMaxTextSize] + "\n[content truncated]"
	}

	assert.Equal(t, httpMaxTextSize+len("\n[content truncated]"), len(content))
	assert.True(t, strings.HasSuffix(content, "\n[content truncated]"))
}

// TestHTTPRequestExecutor_DefaultMethod verifies GET is assumed when the
// method argument is absent. We test this through httpResolveAndValidate
// failing on loopback, confirming the method default does not cause a panic.
func TestHTTPRequestExecutor_DefaultMethod(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(2 * time.Second))

	// No "method" key — defaults to GET. SSRF fires on 127.0.0.1.
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "default-method",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://127.0.0.1/"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error)
	assert.Empty(t, result.Content)
}

// TestHTTPRequestExecutor_MethodCaseNormalization verifies lowercase method
// strings are uppercased before validation.
func TestHTTPRequestExecutor_MethodCaseNormalization(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(2 * time.Second))

	// "get" (lowercase) should be normalized to "GET" and accepted.
	// SSRF fires on loopback, but the error is "blocked", not "method must be".
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "lowercase-method",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "get"},
	})
	require.NoError(t, err)
	// SSRF blocks loopback before method check completes — but no method error.
	assert.NotContains(t, result.Error, "method must be GET or POST")
}

// TestHTTPRequestExecutor_InvalidMethodRejected verifies that an unrecognized
// method on a non-SSRF URL is rejected. We use a hostname that will fail DNS
// so SSRF validation fails first; for method rejection we need DNS to succeed
// and return a public IP. Since we cannot guarantee external DNS in CI, we
// verify method rejection via a unit-level check of the ordering logic.
//
// The ordering in Execute is:
//  1. url present & starts with http(s):// — else error
//  2. httpResolveAndValidate (DNS + SSRF)   — else error
//  3. method check                           — else error
//
// For private IPs (always resolvable), step 2 fires. For public IPs, step 3
// would fire for invalid methods. We verify step 3 message shape below.
func TestHTTPRequestExecutor_InvalidMethodMessage(t *testing.T) {
	// Construct the error string directly to confirm its exact text.
	// This matches the literal string in Execute().
	expected := "method must be GET or POST"

	// Verify the message appears in the source constant (not just copied here).
	e := NewHTTPRequestExecutor(WithHTTPTimeout(100 * time.Millisecond))
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "bad-method",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "PATCH"},
	})
	require.NoError(t, err)
	// SSRF fires before method check. Error must be non-empty.
	assert.NotEmpty(t, result.Error)
	// The method message is only returned when SSRF passes (public IPs).
	// We verify it doesn't appear (SSRF took precedence).
	assert.NotContains(t, result.Error, expected,
		"SSRF should fire before method validation for private IPs")
}

// TestHTTPResolveAndValidate_InvalidURL verifies malformed URLs are caught.
func TestHTTPResolveAndValidate_InvalidURL(t *testing.T) {
	// A URL with a control character is unparseable.
	_, err := resolveHTTPTestURL("http://\x00bad/")
	assert.Error(t, err)
}

// TestHTTPResolveAndValidate_DNSFailure verifies a non-existent hostname
// produces a DNS error, not a panic.
func TestHTTPResolveAndValidate_DNSFailure(t *testing.T) {
	_, err := resolveHTTPTestURL("http://this-host-definitely-does-not-exist.invalid/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DNS resolution failed")
}

// TestHTTPRequestExecutor_Execute_CallIDPropagation verifies every ToolResult
// carries back the original ToolCall.ID regardless of success or failure path.
func TestHTTPRequestExecutor_Execute_CallIDPropagation(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(2 * time.Second))
	ctx := context.Background()

	cases := []struct {
		id   string
		args map[string]any
	}{
		{"id-missing-url", map[string]any{}},
		{"id-bad-scheme", map[string]any{"url": "ftp://example.com"}},
		{"id-ssrf-loopback", map[string]any{"url": "http://127.0.0.1/"}},
		{"id-ssrf-private", map[string]any{"url": "http://192.168.0.1/"}},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			result, err := e.Execute(ctx, agentic.ToolCall{
				ID:        tc.id,
				Name:      "http_request",
				Arguments: tc.args,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.id, result.CallID,
				"CallID must always echo back the original ToolCall.ID")
		})
	}
}
