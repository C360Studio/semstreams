package agenticmodel

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
)

func minimalAgentRequest() agentic.AgentRequest {
	return agentic.AgentRequest{
		RequestID: "probe-test",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "ping"}},
		Model:     "qwen3:8b",
	}
}

func TestOllamaBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://localhost:11434/v1", "http://localhost:11434"},
		{"http://localhost:11434/v1/", "http://localhost:11434"},
		{"http://localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ollamaBaseURL(tc.in); got != tc.want {
				t.Fatalf("ollamaBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseOllamaNumCtx(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		present bool
	}{
		{"explicit", "num_ctx 32768\nstop \"<|eot|>\"", 32768, true},
		{"first line", "num_ctx 8192", 8192, true},
		{"trailing whitespace", "  num_ctx   65536  ", 65536, true},
		{"absent", "stop \"<|eot|>\"\ntemperature 0.7", 0, false},
		{"empty", "", 0, false},
		{"unparsable value", "num_ctx abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseOllamaNumCtx(tc.in)
			if ok != tc.present || got != tc.want {
				t.Fatalf("parseOllamaNumCtx(%q) = (%d, %v), want (%d, %v)",
					tc.in, got, ok, tc.want, tc.present)
			}
		})
	}
}

// recordingHandler captures slog records by level for assertion. slog.Handler
// calls are safe from multiple goroutines because sync.Once and test goroutines
// serialize via the mutex.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) warnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			n++
		}
	}
	return n
}

func (h *recordingHandler) warnMessages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			out = append(out, r.Message)
		}
	}
	return out
}

// fakeShowServer returns an httptest server that responds to /api/show with
// the given "parameters" text block. status controls the HTTP status code.
func fakeShowServer(t *testing.T, status int, params string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if hits != nil {
			hits.Add(1)
		}
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{"parameters": params})
		}
	}))
}

func newTestClient(t *testing.T, url, modelName string, maxTokens int, logger *slog.Logger) *Client {
	t.Helper()
	c, err := NewClient(&model.EndpointConfig{
		Provider:  "ollama",
		URL:       url,
		Model:     modelName,
		MaxTokens: maxTokens,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.SetLogger(logger)
	return c
}

func TestProbeOllamaNumCtx_ExplicitAdequate(t *testing.T) {
	hits := &atomic.Int64{}
	srv := fakeShowServer(t, 200, "num_ctx 32768\nstop \"<|eot|>\"\n", hits)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "qwen3:8b", 32768, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if hits.Load() != 1 {
		t.Fatalf("expected /api/show hit once, got %d", hits.Load())
	}
	if h.warnCount() != 0 {
		t.Fatalf("expected no WARN, got: %v", h.warnMessages())
	}
}

func TestProbeOllamaNumCtx_ExplicitBelowMax(t *testing.T) {
	srv := fakeShowServer(t, 200, "num_ctx 4096\n", nil)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "qwen3:8b", 32768, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 1 {
		t.Fatalf("expected one WARN, got %d: %v", h.warnCount(), h.warnMessages())
	}
	msg := h.warnMessages()[0]
	if !strings.Contains(msg, "silently truncate") {
		t.Errorf("WARN message missing 'silently truncate': %q", msg)
	}
}

func TestProbeOllamaNumCtx_AbsentParamAboveDefault(t *testing.T) {
	// parameters block has no num_ctx line → model runs on Ollama's 4096
	// default. endpoint.MaxTokens 32768 > 4096 → WARN with explicit=false.
	srv := fakeShowServer(t, 200, "stop \"<|eot|>\"\ntemperature 0.7\n", nil)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "qwen3:8b", 32768, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 1 {
		t.Fatalf("expected one WARN, got %d: %v", h.warnCount(), h.warnMessages())
	}
}

func TestProbeOllamaNumCtx_AbsentParamAtOrBelowDefault(t *testing.T) {
	// endpoint.MaxTokens == default → no warn (model default is sufficient).
	srv := fakeShowServer(t, 200, "stop \"<|eot|>\"\n", nil)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "qwen3:8b", ollamaDefaultNumCtx, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 0 {
		t.Fatalf("expected no WARN at default threshold, got: %v", h.warnMessages())
	}
}

func TestProbeOllamaNumCtx_ShowReturns404(t *testing.T) {
	srv := fakeShowServer(t, 404, "", nil)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "missing-model", 32768, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 0 {
		t.Fatalf("404 should not WARN, got: %v", h.warnMessages())
	}
}

func TestProbeOllamaNumCtx_Unreachable(t *testing.T) {
	h := &recordingHandler{}
	// Use a URL nothing is listening on. The probe's 3s timeout caps this
	// test; net.Dial to a closed port returns quickly.
	c := newTestClient(t, "http://127.0.0.1:1/v1", "qwen3:8b", 32768, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 0 {
		t.Fatalf("unreachable host should not WARN, got: %v", h.warnMessages())
	}
}

func TestProbeOllamaNumCtx_MissingMaxTokens(t *testing.T) {
	// endpoint.MaxTokens = 0 means "don't care" — no comparison, no WARN.
	srv := fakeShowServer(t, 200, "num_ctx 2048\n", nil)
	defer srv.Close()

	h := &recordingHandler{}
	c := newTestClient(t, srv.URL, "qwen3:8b", 0, slog.New(h))
	c.probeOllamaNumCtx(context.Background())

	if h.warnCount() != 0 {
		t.Fatalf("endpoint.MaxTokens=0 should skip comparison, got: %v", h.warnMessages())
	}
}

// TestOllamaProbeFiresOncePerClient verifies sync.Once gating: probe hits
// /api/show at most once across multiple ChatCompletion calls. This is the
// invariant protecting us from per-request overhead and WARN spam.
func TestOllamaProbeFiresOncePerClient(t *testing.T) {
	var showHits atomic.Int64
	var chatHits atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/show":
			showHits.Add(1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{"parameters": "num_ctx 32768\n"})
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			chatHits.Add(1)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "1", "object": "chat.completion", "model": "qwen3:8b",
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	// /v1 suffix exercises the URL-stripping path.
	h := &recordingHandler{}
	c := newTestClient(t, srv.URL+"/v1", "qwen3:8b", 32768, slog.New(h))

	for i := range 3 {
		_, err := c.ChatCompletion(context.Background(), minimalAgentRequest())
		if err != nil {
			t.Fatalf("ChatCompletion #%d: %v", i, err)
		}
	}
	if got := showHits.Load(); got != 1 {
		t.Fatalf("expected /api/show hit exactly once across 3 ChatCompletion calls, got %d", got)
	}
	if got := chatHits.Load(); got != 3 {
		t.Fatalf("expected 3 /chat/completions calls, got %d", got)
	}
}
