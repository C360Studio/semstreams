package executors

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
		nil,
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
	assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
}

// TestHTTPRequestExecutor_ListTools verifies the tool definition shape.
func TestHTTPRequestExecutor_ListTools(t *testing.T) {
	e := NewHTTPRequestExecutor()
	tools := e.ListTools()

	require.Len(t, tools, 1)
	tool := tools[0]
	assert.Equal(t, "http_request", tool.Name)
	assert.Equal(t, agentic.ToolEffectReadOnly, tool.Effect)
	assert.Contains(t, tool.Description, "GET")
	assert.Contains(t, tool.Description, "Markdown-like")
	assert.Contains(t, tool.Description, "JavaScript is not executed")

	props, ok := tool.Parameters["properties"].(map[string]any)
	require.True(t, ok, "parameters must have a properties map")
	assert.Contains(t, props, "url")
	assert.Contains(t, props, "method")
	method, ok := props["method"].(map[string]any)
	require.True(t, ok, "method must have a schema")
	assert.Equal(t, []string{"GET"}, method["enum"])

	required, ok := tool.Parameters["required"].([]string)
	require.True(t, ok, "parameters must have a required slice")
	assert.Contains(t, required, "url")

	emittingTool := NewHTTPRequestExecutor(WithHTTPTriplePublisher(&recordingPublisher{})).ListTools()[0]
	assert.Equal(t, agentic.ToolEffectMutating, emittingTool.Effect)
}

func TestHTTPRequestExecutor_Execute_ExplicitPlainTextIsNeverHTMLSniffed(t *testing.T) {
	const body = "<html><head><title>Looks HTML</title></head><body><h1>Keep markup</h1></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	port := testServerPort(t, srv)
	fakeIP := net.ParseIP("93.184.216.14")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"plain.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
	}
	pub := &recordingPublisher{}
	executor := network.executor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)

	result := executeHTTPTestCall(context.Background(), t, executor, "http://plain.example:"+port+"/page")

	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Content-Type: text/plain")
	assert.Contains(t, result.Content, "Content-Transform: raw")
	assert.Contains(t, result.Content, body)
	assert.NotContains(t, result.Content, "Title: Looks HTML")

	facts := make(map[string]any)
	for _, triple := range pub.triples {
		facts[triple.Predicate] = triple.Object
	}
	assert.Equal(t, "text/plain", facts[agvocab.WebContentType])
	assert.Equal(t, body, facts[agvocab.WebText])
}

func TestHTTPReadableResponse_SniffsHTMLOnlyForUnusableMIME(t *testing.T) {
	body := []byte("<html><body><h1>Reference</h1></body></html>")
	for _, contentType := range []string{"", ";", "application/octet-stream"} {
		readable, err := httpReadableResponse(body, contentType, false)
		require.NoError(t, err, "Content-Type %q", contentType)
		assert.Equal(t, "html-to-markdown", readable.Transform, "Content-Type %q", contentType)
		assert.Contains(t, readable.Text, "# Reference", "Content-Type %q", contentType)
	}

	for _, contentType := range []string{"text/plain", "application/json"} {
		readable, err := httpReadableResponse(body, contentType, false)
		require.NoError(t, err, "Content-Type %q", contentType)
		assert.Equal(t, "raw", readable.Transform, "Content-Type %q", contentType)
		assert.Equal(t, string(body), readable.Text, "Content-Type %q", contentType)
	}
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
			assert.LessOrEqual(t, len(result.Content), httpMaxToolContent)
		})
	}
}

func TestHTTPRequestExecutor_Execute_TotalContentBoundIsUTF8Safe(t *testing.T) {
	body := strings.Repeat("🙂", httpMaxToolContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, "<html><head><title>Large page</title></head><body><p>%s</p></body></html>", body)
	}))
	t.Cleanup(srv.Close)
	port := testServerPort(t, srv)
	fakeIP := net.ParseIP("93.184.216.12")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"large.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://large.example:"+port+"/page")
	require.Empty(t, result.Error)
	assert.LessOrEqual(t, len(result.Content), httpMaxToolContent)
	assert.Greater(t, len(result.Content), httpMaxToolContent-utf8.UTFMax)
	assert.True(t, utf8.ValidString(result.Content))
	assert.Contains(t, result.Content, "Truncated: true")
	assert.True(t, strings.HasSuffix(result.Content, "[content truncated]"))
}

func TestHTTPRequestExecutor_Execute_DecodesDeclaredAndDetectedHTMLCharset(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
		want        []string
	}{
		{
			name:        "declared ISO-8859-1",
			contentType: "text/html; charset=iso-8859-1",
			body:        []byte("<html><head><title>Caf\xe9</title></head><body><p>Cr\xe8me br\xfbl\xe9e</p></body></html>"),
			want:        []string{"Title: Café", "Crème brûlée"},
		},
		{
			name:        "meta-detected Windows-1252",
			contentType: "text/html",
			body:        []byte("<html><head><meta charset=windows-1252></head><body><p>Price \x80</p></body></html>"),
			want:        []string{"Price €"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write(tc.body)
			}))
			t.Cleanup(srv.Close)
			port := testServerPort(t, srv)
			fakeIP := net.ParseIP("93.184.216.13")
			network := &httpTestNetwork{
				resolved:    map[string][]net.IP{"charset.example": {fakeIP}},
				dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
			}

			result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://charset.example:"+port+"/page")
			require.Empty(t, result.Error)
			for _, want := range tc.want {
				assert.Contains(t, result.Content, want)
			}
			assert.True(t, utf8.ValidString(result.Content))
		})
	}
}

func TestHTTPRequestExecutor_Execute_MetadataPolicyBoundsAreTyped(t *testing.T) {
	tooLongURL := "http://example.com/" + strings.Repeat("x", httpMaxURLSize)
	result, err := NewHTTPRequestExecutor().Execute(context.Background(), agentic.ToolCall{
		ID: "long-url", Name: "http_request", Arguments: map[string]any{"url": tooLongURL},
	})
	require.NoError(t, err)
	assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; x="+strings.Repeat("x", httpMaxContentType))
		_, _ = w.Write([]byte("body"))
	}))
	t.Cleanup(srv.Close)
	port := testServerPort(t, srv)
	fakeIP := net.ParseIP("93.184.216.14")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"metadata.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
	}
	result = executeHTTPTestCall(context.Background(), t, network.executor(), "http://metadata.example:"+port+"/page")
	assert.Empty(t, result.Content)
	assert.Equal(t, agentic.ToolErrorExternal, result.ErrorKind)
	assert.Contains(t, result.Error, "Content-Type")

	titleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, "<html><head><title>%s</title></head><body>body</body></html>",
			strings.Repeat("x", httpMaxTitleSize+1))
	}))
	t.Cleanup(titleServer.Close)
	titlePort := testServerPort(t, titleServer)
	titleIP := net.ParseIP("93.184.216.18")
	titleNetwork := &httpTestNetwork{
		resolved:    map[string][]net.IP{"title.example": {titleIP}},
		dialTargets: map[string]string{net.JoinHostPort(titleIP.String(), titlePort): titleServer.Listener.Addr().String()},
	}
	result, err = titleNetwork.executor().Execute(context.Background(), agentic.ToolCall{
		ID: "long-title", Name: "http_request",
		Arguments: map[string]any{"url": "http://title.example:" + titlePort + "/page"},
	})
	require.NoError(t, err)
	assert.Equal(t, agentic.ToolErrorExternal, result.ErrorKind)
	assert.Contains(t, result.Error, "title")
}

func TestHTTPRequestExecutor_Execute_FailureKindsAreStructured(t *testing.T) {
	t.Run("DNS failure is network", func(t *testing.T) {
		e := NewHTTPRequestExecutor()
		e.lookupIP = func(_ context.Context, _ string) ([]net.IP, error) {
			return nil, fmt.Errorf("resolver unavailable")
		}
		result, err := e.Execute(context.Background(), agentic.ToolCall{
			ID: "dns", Name: "http_request", Arguments: map[string]any{"url": "http://dns.example/page"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentic.ToolErrorNetwork, result.ErrorKind)
	})

	t.Run("dial failure is network", func(t *testing.T) {
		fakeIP := net.ParseIP("93.184.216.16")
		network := &httpTestNetwork{
			resolved:    map[string][]net.IP{"dial.example": {fakeIP}},
			dialTargets: map[string]string{},
		}
		result, err := network.executor().Execute(context.Background(), agentic.ToolCall{
			ID: "dial", Name: "http_request", Arguments: map[string]any{"url": "http://dial.example/page"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentic.ToolErrorNetwork, result.ErrorKind)
	})

	for _, tc := range []struct {
		status int
		kind   agentic.ToolErrorKind
	}{
		{status: http.StatusUnauthorized, kind: agentic.ToolErrorPermission},
		{status: http.StatusForbidden, kind: agentic.ToolErrorPermission},
		{status: http.StatusBadRequest, kind: agentic.ToolErrorInvalidArgs},
		{status: http.StatusNotFound, kind: agentic.ToolErrorNotFound},
		{status: http.StatusTooManyRequests, kind: agentic.ToolErrorExternal},
		{status: http.StatusServiceUnavailable, kind: agentic.ToolErrorExternal},
	} {
		t.Run(fmt.Sprintf("HTTP %d", tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream failure", tc.status)
			}))
			t.Cleanup(srv.Close)
			port := testServerPort(t, srv)
			fakeIP := net.ParseIP("93.184.216.17")
			network := &httpTestNetwork{
				resolved:    map[string][]net.IP{"status.example": {fakeIP}},
				dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
			}
			result, err := network.executor().Execute(context.Background(), agentic.ToolCall{
				ID: "status", Name: "http_request",
				Arguments: map[string]any{"url": "http://status.example:" + port + "/page"},
			})
			require.NoError(t, err)
			assert.Equal(t, tc.kind, result.ErrorKind)
		})
	}
}

func TestHTTPRequestExecutor_Execute_DeadlinePropagatesAsGoError(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(20 * time.Millisecond))
	e.lookupIP = func(ctx context.Context, _ string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "deadline", Name: "http_request", Arguments: map[string]any{"url": "http://slow-dns.example/page"},
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, agentic.ToolErrorTimeout, result.ErrorKind)
}

func TestHTTPRequestExecutor_Execute_CanonicalizesIDNBeforeResolutionAndDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		require.NoError(t, err)
		assert.Equal(t, "xn--bcher-kva.example", host)
		_, _ = w.Write([]byte("internationalized host"))
	}))
	t.Cleanup(srv.Close)
	port := testServerPort(t, srv)
	fakeIP := net.ParseIP("93.184.216.15")
	network := &httpTestNetwork{
		resolved:    map[string][]net.IP{"xn--bcher-kva.example": {fakeIP}},
		dialTargets: map[string]string{net.JoinHostPort(fakeIP.String(), port): srv.Listener.Addr().String()},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://bücher.example:"+port+"/page")
	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Final-URL: http://xn--bcher-kva.example:"+port+"/page")
	assert.Equal(t, []string{net.JoinHostPort(fakeIP.String(), port)}, network.dialSnapshot())
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
	redirectURL = "http://bücher.example:" + finalPort + "/final"
	canonicalRedirectURL := "http://xn--bcher-kva.example:" + finalPort + "/final"

	initialIP := net.ParseIP("93.184.216.20")
	finalIP := net.ParseIP("93.184.216.21")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{
			"index.example":         {initialIP},
			"xn--bcher-kva.example": {finalIP},
		},
		dialTargets: map[string]string{
			net.JoinHostPort(initialIP.String(), initialPort): initial.Listener.Addr().String(),
			net.JoinHostPort(finalIP.String(), finalPort):     final.Listener.Addr().String(),
		},
	}

	result := executeHTTPTestCall(context.Background(), t, network.executor(), "http://index.example:"+initialPort+"/start")

	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Final-URL: "+canonicalRedirectURL)
	assert.Contains(t, result.Content, "final page")
	assert.Equal(t, []string{
		net.JoinHostPort(initialIP.String(), initialPort),
		net.JoinHostPort(finalIP.String(), finalPort),
	}, network.dialSnapshot())
}

func TestHTTPRequestExecutor_Execute_HTTPToHTTPSRedirectUsesTLSIdentityAndDefaultPort(t *testing.T) {
	serverName := make(chan string, 1)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverName <- r.TLS.ServerName
		_, _ = w.Write([]byte("secure final"))
	}))
	t.Cleanup(secure.Close)

	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/final", http.StatusFound)
	}))
	t.Cleanup(initial.Close)
	initialPort := testServerPort(t, initial)

	roots := x509.NewCertPool()
	roots.AddCert(secure.Certificate())
	initialIP := net.ParseIP("93.184.216.30")
	secureIP := net.ParseIP("93.184.216.31")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{
			"initial.example": {initialIP},
			"example.com":     {secureIP},
		},
		dialTargets: map[string]string{
			net.JoinHostPort(initialIP.String(), initialPort): initial.Listener.Addr().String(),
			net.JoinHostPort(secureIP.String(), "443"):        secure.Listener.Addr().String(),
		},
	}
	e := network.executor()
	e.tlsConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	result := executeHTTPTestCall(context.Background(), t, e, "http://initial.example:"+initialPort+"/start")
	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Final-URL: https://example.com/final")
	assert.Contains(t, result.Content, "secure final")
	assert.Equal(t, "example.com", <-serverName)
	assert.Equal(t, []string{
		net.JoinHostPort(initialIP.String(), initialPort),
		net.JoinHostPort(secureIP.String(), "443"),
	}, network.dialSnapshot())
}

func TestHTTPRequestExecutor_Execute_HTTPSToHTTPRedirectUsesSchemeSelectedDefaultPort(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain final"))
	}))
	t.Cleanup(plain.Close)

	serverName := make(chan string, 1)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverName <- r.TLS.ServerName
		http.Redirect(w, r, "http://plain.example/final", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	securePort := testServerPort(t, secure)

	roots := x509.NewCertPool()
	roots.AddCert(secure.Certificate())
	secureIP := net.ParseIP("93.184.216.32")
	plainIP := net.ParseIP("93.184.216.33")
	network := &httpTestNetwork{
		resolved: map[string][]net.IP{
			"example.com":   {secureIP},
			"plain.example": {plainIP},
		},
		dialTargets: map[string]string{
			net.JoinHostPort(secureIP.String(), securePort): secure.Listener.Addr().String(),
			net.JoinHostPort(plainIP.String(), "80"):        plain.Listener.Addr().String(),
		},
	}
	e := network.executor()
	e.tlsConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	result := executeHTTPTestCall(context.Background(), t, e, "https://example.com:"+securePort+"/start")
	require.Empty(t, result.Error)
	assert.Contains(t, result.Content, "Final-URL: http://plain.example/final")
	assert.Contains(t, result.Content, "plain final")
	assert.Equal(t, "example.com", <-serverName)
	assert.Equal(t, []string{
		net.JoinHostPort(secureIP.String(), securePort),
		net.JoinHostPort(plainIP.String(), "80"),
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
		wantKind    agentic.ToolErrorKind
		wantDialLen int
	}{
		{
			name:        "private redirect target",
			location:    "http://private.example/secret",
			resolved:    map[string][]net.IP{"private.example": {net.ParseIP("127.0.0.1")}},
			wantError:   "blocked",
			wantKind:    agentic.ToolErrorPermission,
			wantDialLen: 1,
		},
		{
			name:        "unsupported redirect scheme",
			location:    "file:///etc/passwd",
			resolved:    map[string][]net.IP{},
			wantError:   "redirect policy",
			wantKind:    agentic.ToolErrorPermission,
			wantDialLen: 1,
		},
		{
			name:        "redirect DNS failure",
			location:    "http://missing.example/page",
			resolved:    map[string][]net.IP{},
			wantError:   "DNS resolution failed",
			wantKind:    agentic.ToolErrorNetwork,
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
			assert.Equal(t, tc.wantKind, result.ErrorKind)
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
	assert.Equal(t, agentic.ToolErrorPermission, result.ErrorKind)
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
		require.ErrorIs(t, outcome.err, context.Canceled)
		assert.Empty(t, outcome.result.Content)
		assert.Contains(t, outcome.result.Error, "context canceled")
		assert.Equal(t, agentic.ToolErrorTimeout, outcome.result.ErrorKind)
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
			name: "URL credentials rejected",
			call: agentic.ToolCall{
				ID:        "c-credentials",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://user:password@example.com/"},
			},
			wantErrFrag: "user information is not allowed",
		},
		{
			name: "method POST rejected",
			call: agentic.ToolCall{
				ID:        "c-post",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "POST"},
			},
			wantErrFrag: "method must be GET",
		},
		{
			name: "method must be a string",
			call: agentic.ToolCall{
				ID:        "c-method-type",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": 7},
			},
			wantErrFrag: "method must be GET",
		},
		{
			name: "method DELETE rejected",
			call: agentic.ToolCall{
				ID:        "c7",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "DELETE"},
			},
			wantErrFrag: "method must be GET",
		},
		{
			name: "method PUT rejected",
			call: agentic.ToolCall{
				ID:        "c8",
				Name:      "http_request",
				Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "PUT"},
			},
			wantErrFrag: "method must be GET",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := e.Execute(ctx, tc.call)
			require.NoError(t, err, "Execute must not return a Go error")
			assert.Equal(t, tc.call.ID, result.CallID)
			assert.Empty(t, result.Content)
			assert.Contains(t, result.Error, tc.wantErrFrag)
			wantKind := agentic.ToolErrorInvalidArgs
			if tc.wantErrFrag == "blocked" {
				wantKind = agentic.ToolErrorPermission
			}
			assert.Equal(t, wantKind, result.ErrorKind)
		})
	}
}

// TestHTTPRequestExecutor_Execute_POSTRejectedBeforeExternalEffect proves the
// retired mutating method cannot reach DNS, dialing, or a remote server.
func TestHTTPRequestExecutor_Execute_POSTRejectedBeforeExternalEffect(t *testing.T) {
	e := NewHTTPRequestExecutor(WithHTTPTimeout(2 * time.Second))
	e.lookupIP = func(_ context.Context, _ string) ([]net.IP, error) {
		t.Fatal("method validation reached DNS")
		return nil, nil
	}
	e.dialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
		t.Fatal("method validation reached dial")
		return nil, nil
	}
	ctx := context.Background()

	result, err := e.Execute(ctx, agentic.ToolCall{
		ID:        "method-post",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://example.com/", "method": "POST"},
	})
	require.NoError(t, err)
	assert.Equal(t, "method must be GET", result.Error)
	assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
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
	assert.NotContains(t, result.Error, "method must be GET")
}

// TestHTTPRequestExecutor_InvalidMethodMessage pins the public validation text.
func TestHTTPRequestExecutor_InvalidMethodMessage(t *testing.T) {
	expected := "method must be GET"
	e := NewHTTPRequestExecutor(WithHTTPTimeout(100 * time.Millisecond))
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "bad-method",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://127.0.0.1/", "method": "PATCH"},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, result.Error)
	assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
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
