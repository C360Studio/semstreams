package service

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingMiddleware appends a label to the trace slice on entry
// and exit. Lets tests assert outermost-first ordering by reading
// the trace after the handler runs.
func recordingMiddleware(label string, trace *[]string) HTTPMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*trace = append(*trace, "enter:"+label)
			next.ServeHTTP(w, r)
			*trace = append(*trace, "exit:"+label)
		})
	}
}

// shortCircuitMiddleware writes a status and never calls next. Used
// to verify that an auth-style middleware can foreclose the inner
// handler.
func shortCircuitMiddleware(status int, body string) HTTPMiddleware {
	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
	}
}

func TestChainMiddleware_OrderIsOutermostFirst(t *testing.T) {
	t.Parallel()

	var trace []string
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		trace = append(trace, "handler")
		w.WriteHeader(http.StatusOK)
	})

	chain := chainMiddleware(inner, []HTTPMiddleware{
		recordingMiddleware("A", &trace),
		recordingMiddleware("B", &trace),
		recordingMiddleware("C", &trace),
	})

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	want := []string{
		"enter:A", "enter:B", "enter:C",
		"handler",
		"exit:C", "exit:B", "exit:A",
	}
	if got := fmt.Sprint(trace); got != fmt.Sprint(want) {
		t.Fatalf("middleware order wrong:\n got: %v\nwant: %v", trace, want)
	}
}

func TestChainMiddleware_EmptyAndNilSlicePassThrough(t *testing.T) {
	t.Parallel()

	cases := map[string][]HTTPMiddleware{
		"nil slice":   nil,
		"empty slice": {},
		"all nils":    {nil, nil},
	}

	for name, mws := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var ran bool
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				ran = true
				w.WriteHeader(http.StatusTeapot)
			})

			chain := chainMiddleware(inner, mws)

			rec := httptest.NewRecorder()
			chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			if !ran {
				t.Fatal("inner handler did not run")
			}
			if rec.Code != http.StatusTeapot {
				t.Fatalf("status: got %d, want %d", rec.Code, http.StatusTeapot)
			}
		})
	}
}

func TestChainMiddleware_ShortCircuitForeclosesInnerHandler(t *testing.T) {
	t.Parallel()

	var ran bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ran = true
		w.WriteHeader(http.StatusOK)
	})

	// Outer auth-style middleware that 401s before delegating.
	chain := chainMiddleware(inner, []HTTPMiddleware{
		shortCircuitMiddleware(http.StatusUnauthorized, "no token"),
	})

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if ran {
		t.Fatal("inner handler ran despite short-circuit middleware")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "no token") {
		t.Fatalf("body: got %q, want substring 'no token'", got)
	}
}

func TestManager_UseHTTPMiddleware_AppendsBeforeBoot(t *testing.T) {
	t.Parallel()

	m := NewServiceManager(NewServiceRegistry())

	noop := HTTPMiddleware(func(next http.Handler) http.Handler { return next })

	m.UseHTTPMiddleware(noop, noop)
	m.UseHTTPMiddleware(noop)

	if got, want := len(m.httpMiddleware), 3; got != want {
		t.Fatalf("middleware count: got %d, want %d", got, want)
	}
}

func TestManager_UseHTTPMiddleware_IgnoredAfterServerStarts(t *testing.T) {
	t.Parallel()

	m := NewServiceManager(NewServiceRegistry())

	// Capture warning log to assert the operator gets feedback when
	// they misuse the API. Using slog.NewTextHandler so we can grep
	// the buffered output without coupling to JSON shape.
	var buf bytes.Buffer
	m.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Simulate "HTTP server already started" — the same guard
	// startHTTPServer / completeHTTPSetup use.
	m.httpServer = &http.Server{}

	called := false
	mw := HTTPMiddleware(func(next http.Handler) http.Handler {
		called = true
		return next
	})

	m.UseHTTPMiddleware(mw)

	if len(m.httpMiddleware) != 0 {
		t.Fatalf("expected no middleware appended after server start, got %d", len(m.httpMiddleware))
	}
	if called {
		t.Fatal("middleware factory should not be invoked when registration is rejected")
	}
	if log := buf.String(); !strings.Contains(log, "called after HTTP server started") {
		t.Fatalf("expected warning log about late registration, got: %q", log)
	}
}

func TestManager_UseHTTPMiddleware_EmptyAndNilArgsAreNoOps(t *testing.T) {
	t.Parallel()

	m := NewServiceManager(NewServiceRegistry())

	m.UseHTTPMiddleware()                      // no args
	m.UseHTTPMiddleware([]HTTPMiddleware{}...) // empty variadic expansion

	if len(m.httpMiddleware) != 0 {
		t.Fatalf("expected zero middleware after no-op calls, got %d", len(m.httpMiddleware))
	}
}

func TestManager_BuildHTTPHandler_AppliesRegisteredMiddleware(t *testing.T) {
	t.Parallel()

	// This is the wired-path regression guard. It catches the case
	// where `Handler: m.buildHTTPHandler()` at the http.Server
	// construction sites in completeHTTPSetup and startHTTPServer
	// gets typo'd back to a bare m.httpMux and silently drops the
	// chain. The only way we observe the wiring is by routing a
	// real request through the wrapped handler.

	m := NewServiceManager(NewServiceRegistry())

	if err := m.initializeHTTPInfrastructure(); err != nil {
		t.Fatalf("initializeHTTPInfrastructure: %v", err)
	}

	// Register a route on the framework mux the same way a
	// component would in RegisterHTTPHandlers.
	m.httpMux.HandleFunc("GET /probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("probe"))
	})

	var trace []string
	m.UseHTTPMiddleware(
		recordingMiddleware("outer", &trace),
		recordingMiddleware("inner", &trace),
	)

	handler := m.buildHTTPHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "probe" {
		t.Fatalf("body: got %q, want %q", got, "probe")
	}
	want := []string{"enter:outer", "enter:inner", "exit:inner", "exit:outer"}
	if got := fmt.Sprint(trace); got != fmt.Sprint(want) {
		t.Fatalf("middleware did not run through the wired handler:\n got: %v\nwant: %v", trace, want)
	}
}

func TestManager_BuildHTTPHandler_PassesThroughWhenNoMiddleware(t *testing.T) {
	t.Parallel()

	m := NewServiceManager(NewServiceRegistry())
	if err := m.initializeHTTPInfrastructure(); err != nil {
		t.Fatalf("initializeHTTPInfrastructure: %v", err)
	}

	// System endpoint registered by initializeHTTPInfrastructure
	// (e.g., /health) — we hit it through the unwrapped chain to
	// verify the no-middleware path is still functional.
	m.httpMux.HandleFunc("GET /probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler := m.buildHTTPHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusTeapot)
	}
}

func TestChainMiddleware_NilEntriesAreSkipped(t *testing.T) {
	t.Parallel()

	var trace []string
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		trace = append(trace, "handler")
		w.WriteHeader(http.StatusOK)
	})

	chain := chainMiddleware(inner, []HTTPMiddleware{
		recordingMiddleware("A", &trace),
		nil,
		recordingMiddleware("B", &trace),
	})

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	want := []string{"enter:A", "enter:B", "handler", "exit:B", "exit:A"}
	if got := fmt.Sprint(trace); got != fmt.Sprint(want) {
		t.Fatalf("nil entries should be skipped:\n got: %v\nwant: %v", trace, want)
	}
}
