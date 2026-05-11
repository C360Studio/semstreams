package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fakeGraphLLMResponse = `{
  "id": "chatcmpl-graph-1",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "test-model",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "graph answer"},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 4, "completion_tokens": 6, "total_tokens": 10}
}`

// newGraphStubServer returns a test server that returns the canned
// response to any POST under /chat/completions.
func newGraphStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeGraphLLMResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGraphLLM_WireBackend_MatchesSDK asserts that for the same
// ChatRequest, the SDK and wire backends produce equivalent
// ChatResponse fields.
func TestGraphLLM_WireBackend_MatchesSDK(t *testing.T) {
	srv := newGraphStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	build := func(backend string) *OpenAIClient {
		c, err := NewOpenAIClient(OpenAIConfig{
			BaseURL:     srv.URL + "/v1",
			Model:       "test-model",
			MaxRetries:  1,
			WireBackend: backend,
		})
		if err != nil {
			t.Fatalf("NewOpenAIClient(%s): %v", backend, err)
		}
		return c
	}

	sdk := build("")
	w := build("wire")

	req := ChatRequest{
		SystemPrompt: "Answer briefly.",
		UserPrompt:   "what time is it?",
		MaxTokens:    32,
	}

	sdkResp, sdkErr := sdk.ChatCompletion(ctx, req)
	wireResp, wireErr := w.ChatCompletion(ctx, req)
	if sdkErr != nil || wireErr != nil {
		t.Fatalf("sdk err=%v, wire err=%v", sdkErr, wireErr)
	}

	if sdkResp.Content != wireResp.Content {
		t.Errorf("Content differs: sdk=%q wire=%q", sdkResp.Content, wireResp.Content)
	}
	if sdkResp.Model != wireResp.Model {
		t.Errorf("Model differs: sdk=%q wire=%q", sdkResp.Model, wireResp.Model)
	}
	if sdkResp.FinishReason != wireResp.FinishReason {
		t.Errorf("FinishReason differs: sdk=%q wire=%q", sdkResp.FinishReason, wireResp.FinishReason)
	}
	if sdkResp.PromptTokens != wireResp.PromptTokens ||
		sdkResp.CompletionTokens != wireResp.CompletionTokens ||
		sdkResp.TotalTokens != wireResp.TotalTokens {
		t.Errorf("Token usage differs: sdk=%+v wire=%+v", sdkResp, wireResp)
	}
}

// TestGraphLLM_WireBackend_NotSet_UsesSDK verifies that an empty
// WireBackend defaults to the SDK path (wireClient is nil).
func TestGraphLLM_WireBackend_NotSet_UsesSDK(t *testing.T) {
	c, err := NewOpenAIClient(OpenAIConfig{
		BaseURL: "http://example.invalid/v1",
		Model:   "m",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	if c.wireClient != nil {
		t.Error("wireClient should be nil when WireBackend is empty")
	}
}
