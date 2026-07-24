package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newBodyCapturingServer returns a test server that records the last
// request body it received under /chat/completions and replies with the
// canned graph-LLM response.
func newBodyCapturingServer(t *testing.T, captured *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		*captured = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeGraphLLMResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGraphLLM_ReasoningEffort_Forwarded proves that
// OpenAIConfig.ReasoningEffort is serialized onto the outbound chat
// completion request as reasoning_effort, mirroring the agentic-model
// path (EndpointConfig.ReasoningEffort). Both the default go-openai
// backend and the wire backend must carry the setting.
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

			c, err := NewOpenAIClient(OpenAIConfig{
				BaseURL:         srv.URL + "/v1",
				Model:           "test-model",
				MaxRetries:      1,
				WireBackend:     backend,
				ReasoningEffort: "none",
			})
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

// TestGraphLLM_ReasoningEffort_OmittedWhenEmpty proves that an empty
// ReasoningEffort is not serialized (omitempty), so providers that do
// not understand the field are unaffected.
func TestGraphLLM_ReasoningEffort_OmittedWhenEmpty(t *testing.T) {
	var body string
	srv := newBodyCapturingServer(t, &body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := NewOpenAIClient(OpenAIConfig{
		BaseURL:    srv.URL + "/v1",
		Model:      "test-model",
		MaxRetries: 1,
	})
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
