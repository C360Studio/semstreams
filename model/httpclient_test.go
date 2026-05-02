package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClient_ZeroOptionsPreservesGoDefaults(t *testing.T) {
	c := NewHTTPClient(HTTPClientOptions{})
	if c == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	if c.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0 (Go default)", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s (Go default)", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 0 {
		t.Errorf("ResponseHeaderTimeout = %v, want 0 (Go default)", tr.ResponseHeaderTimeout)
	}
	if tr.DisableKeepAlives {
		t.Error("DisableKeepAlives = true, want false")
	}
}

func TestNewHTTPClient_HonoursAllFields(t *testing.T) {
	c := NewHTTPClient(HTTPClientOptions{
		Timeout:               5 * time.Second,
		IdleConnTimeout:       "15s",
		ResponseHeaderTimeout: "20s",
		DisableKeepAlives:     true,
		MaxIdleConnsPerHost:   7,
	})
	if c.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", c.Timeout)
	}
	tr := c.Transport.(*http.Transport)
	if tr.IdleConnTimeout != 15*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 15s", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 20*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 20s", tr.ResponseHeaderTimeout)
	}
	if !tr.DisableKeepAlives {
		t.Error("DisableKeepAlives = false, want true")
	}
	if tr.MaxIdleConnsPerHost != 7 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 7", tr.MaxIdleConnsPerHost)
	}
}

func TestNewHTTPClient_InvalidDurationsFallToDefaults(t *testing.T) {
	c := NewHTTPClient(HTTPClientOptions{
		IdleConnTimeout:       "not-a-duration",
		ResponseHeaderTimeout: "thirty seconds",
	})
	tr := c.Transport.(*http.Transport)
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s default after invalid string", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 0 {
		t.Errorf("ResponseHeaderTimeout = %v, want 0 default after invalid string", tr.ResponseHeaderTimeout)
	}
}

func TestHTTPClientOptionsFromEndpoint(t *testing.T) {
	t.Run("nil endpoint returns zero opts", func(t *testing.T) {
		got := HTTPClientOptionsFromEndpoint(nil)
		if got != (HTTPClientOptions{}) {
			t.Errorf("got %+v, want zero value", got)
		}
	})
	t.Run("translates the three fields", func(t *testing.T) {
		got := HTTPClientOptionsFromEndpoint(&EndpointConfig{
			IdleConnTimeout:       "10s",
			ResponseHeaderTimeout: "30s",
			DisableKeepAlives:     true,
		})
		want := HTTPClientOptions{
			IdleConnTimeout:       "10s",
			ResponseHeaderTimeout: "30s",
			DisableKeepAlives:     true,
		}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("does not propagate Timeout (caller manages ctx)", func(t *testing.T) {
		got := HTTPClientOptionsFromEndpoint(&EndpointConfig{
			RequestTimeout: "60s", // unrelated field — caller manages ctx
		})
		if got.Timeout != 0 {
			t.Errorf("Timeout = %v, want 0 — endpoint RequestTimeout is ctx-level, not http.Client-level", got.Timeout)
		}
	})
}

// TestNewHTTPClient_ResponseHeaderTimeoutFiresFast proves the operator-set
// ResponseHeaderTimeout cancels a hung request. Simulates the failure mode
// semspec hit on openrouter-qwen3-moe (beta.33 post-mortem): server takes
// the connection but never sends headers. Without the timeout the client
// would block until the request-level ctx fires (minutes); with it set,
// the client returns within the timeout window.
func TestNewHTTPClient_ResponseHeaderTimeoutFiresFast(t *testing.T) {
	// Server that accepts the connection but never writes a response.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Hold the connection until the test-side ctx cancellation kills
		// the handler; never send headers.
		<-r.Context().Done()
	}))
	defer server.Close()

	c := NewHTTPClient(HTTPClientOptions{
		ResponseHeaderTimeout: "200ms",
	})

	// Outer ctx is generous — the ResponseHeaderTimeout should fire first.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = c.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from ResponseHeaderTimeout, got nil")
	}
	// Bound proves it was the 200ms header timeout, not the 10s outer ctx —
	// the failure semspec saw was a multi-minute wedge with an in-flight ctx.
	if elapsed > 2*time.Second {
		t.Errorf("request took %v, want < 2s — ResponseHeaderTimeout did not fire", elapsed)
	}
	// Net/http wraps the timeout in a *url.Error whose underlying error
	// message identifies the source. Anchor the assertion on that text so a
	// regression that switches to a different timeout source surfaces here.
	if !strings.Contains(err.Error(), "response headers") {
		t.Errorf("error = %q, want it to mention \"response headers\"", err.Error())
	}
}
