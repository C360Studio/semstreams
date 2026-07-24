package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/model"
)

// newBodyCapturingServer returns a test server that records the last
// request body it received under /chat/completions and replies with the
// canned graph-LLM response. It uses io.ReadAll — a single Body.Read can
// legally short-read a chunked/large body and silently drop the tail.
func newBodyCapturingServer(t *testing.T, captured *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		*captured = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeGraphLLMResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGraphLLM_ReasoningEffort_Forwarded proves that EndpointConfig.ReasoningEffort
// is serialized onto the outbound chat completion request as reasoning_effort,
// mirroring the agentic-model path. The client is built through the REAL
// translator (OpenAIConfigFromEndpoint) rather than by setting the field on
// OpenAIConfig directly — the translator is the exact seam that originally dropped
// the field, so bypassing it would let the regression pass. Both the default
// go-openai backend and the wire backend must carry the setting.
func TestGraphLLM_ReasoningEffort_Forwarded(t *testing.T) {
	for _, backend := range []string{"", "wire"} {
		backend := backend
		name := "sdk"
		if backend == "wire" {
			name = "wire"
		}
		t.Run(name, func(t *testing.T) {
			var body string
			srv := newBodyCapturingServer(t, &body)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Build the config the way production does: an endpoint from the
			// registry, resolved, then translated. Exercises the dropped seam.
			ep := &model.EndpointConfig{
				Provider:        "gemini",
				URL:             srv.URL + "/v1",
				Model:           "test-model",
				ReasoningEffort: "none",
				WireBackend:     backend,
			}
			resolved := &model.ResolvedEndpoint{URL: ep.URL, Model: ep.Model}
			cfg := OpenAIConfigFromEndpoint(resolved, ep, nil)
			cfg.MaxRetries = 1

			c, err := NewOpenAIClient(cfg)
			if err != nil {
				t.Fatalf("NewOpenAIClient: %v", err)
			}

			if _, err := c.ChatCompletion(ctx, ChatRequest{UserPrompt: "hi"}); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}

			if !strings.Contains(body, `"reasoning_effort":"none"`) {
				t.Errorf("request body missing reasoning_effort: %s", body)
			}
		})
	}
}

// TestGraphLLM_ReasoningEffort_OmittedWhenEmpty proves that an endpoint with no
// ReasoningEffort produces no reasoning_effort field (omitempty), so providers
// that do not understand it are unaffected. Also routed through the translator.
func TestGraphLLM_ReasoningEffort_OmittedWhenEmpty(t *testing.T) {
	var body string
	srv := newBodyCapturingServer(t, &body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ep := &model.EndpointConfig{Provider: "openai", URL: srv.URL + "/v1", Model: "test-model"}
	resolved := &model.ResolvedEndpoint{URL: ep.URL, Model: ep.Model}
	cfg := OpenAIConfigFromEndpoint(resolved, ep, nil)
	cfg.MaxRetries = 1

	c, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	if _, err := c.ChatCompletion(ctx, ChatRequest{UserPrompt: "hi"}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if strings.Contains(body, "reasoning_effort") {
		t.Errorf("request body unexpectedly contains reasoning_effort: %s", body)
	}
}
