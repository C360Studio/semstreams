package agenticmodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
)

// fakeChatCompletionResponse is a fixed JSON body the stub HTTP server
// returns to both SDK and wire clients during differential testing.
const fakeChatCompletionResponse = `{
  "id": "chatcmpl-test-1",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "test-model",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "hello from the model"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 7,
    "total_tokens": 12
  }
}`

// fakeToolCallResponse exercises the tool_call path.
const fakeToolCallResponse = `{
  "id": "chatcmpl-test-2",
  "object": "chat.completion",
  "created": 1700000001,
  "model": "test-model",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "",
        "tool_calls": [
          {
            "id": "call-1",
            "type": "function",
            "function": {
              "name": "read_file",
              "arguments": "{\"path\":\"/etc/hosts\"}"
            }
          }
        ]
      },
      "finish_reason": "tool_calls"
    }
  ],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 8,
    "total_tokens": 20
  }
}`

// newStubServer returns an httptest.Server that returns body on every
// POST /chat/completions. recorded captures the most recent request
// body so tests can assert on the wire shape sent.
func newStubServer(t *testing.T, body string, recorded *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if recorded != nil {
			b, _ := io.ReadAll(r.Body)
			*recorded = b
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newWireDifferentialClient builds a Client pointed at the stub server. backend
// selects "sdk" or "wire".
func newWireDifferentialClient(t *testing.T, baseURL, backend string) *Client {
	t.Helper()
	ep := &model.EndpointConfig{
		Provider:    "openai",
		URL:         baseURL + "/v1",
		Model:       "test-model",
		WireBackend: backend,
	}
	c, err := NewClient(ep)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.SetRetryConfig(RetryConfig{
		MaxAttempts:         1,
		MaxRateLimitRetries: 1,
		InitialDelay:        "10ms",
		MaxDelay:            "10ms",
		RateLimitDelay:      "10ms",
		Backoff:             "exponential",
	})
	return c
}

// TestWireBackend_BasicResponse asserts that the wire backend returns
// an AgentResponse equivalent to the SDK backend for a simple
// non-tool-call completion.
func TestWireBackend_BasicResponse(t *testing.T) {
	srv := newStubServer(t, fakeChatCompletionResponse, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := agentic.AgentRequest{
		RequestID: "req-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}

	sdkClient := newWireDifferentialClient(t, srv.URL, "sdk")
	wireClient := newWireDifferentialClient(t, srv.URL, "wire")

	sdkResp, sdkErr := sdkClient.ChatCompletion(ctx, req)
	wireResp, wireErr := wireClient.ChatCompletion(ctx, req)

	if sdkErr != nil || wireErr != nil {
		t.Fatalf("SDK err=%v, Wire err=%v", sdkErr, wireErr)
	}

	if sdkResp.Status != wireResp.Status {
		t.Errorf("Status differs: sdk=%q wire=%q", sdkResp.Status, wireResp.Status)
	}
	if sdkResp.FinishReason != wireResp.FinishReason {
		t.Errorf("FinishReason differs: sdk=%q wire=%q", sdkResp.FinishReason, wireResp.FinishReason)
	}
	if sdkResp.Message.Content != wireResp.Message.Content {
		t.Errorf("Content differs: sdk=%q wire=%q", sdkResp.Message.Content, wireResp.Message.Content)
	}
	if sdkResp.Message.Role != wireResp.Message.Role {
		t.Errorf("Role differs: sdk=%q wire=%q", sdkResp.Message.Role, wireResp.Message.Role)
	}
	if sdkResp.TokenUsage != wireResp.TokenUsage {
		t.Errorf("TokenUsage differs: sdk=%+v wire=%+v", sdkResp.TokenUsage, wireResp.TokenUsage)
	}
}

// TestWireBackend_ToolCallResponse asserts that wire and SDK paths
// produce equivalent AgentResponse on the tool_call path including
// argument parsing.
func TestWireBackend_ToolCallResponse(t *testing.T) {
	srv := newStubServer(t, fakeToolCallResponse, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := agentic.AgentRequest{
		RequestID: "req-2",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "use a tool"}},
		Tools: []agentic.ToolDefinition{
			{Name: "read_file", Description: "read a file"},
		},
	}

	sdkClient := newWireDifferentialClient(t, srv.URL, "sdk")
	wireClient := newWireDifferentialClient(t, srv.URL, "wire")

	sdkResp, _ := sdkClient.ChatCompletion(ctx, req)
	wireResp, _ := wireClient.ChatCompletion(ctx, req)

	if sdkResp.Status != wireResp.Status {
		t.Errorf("Status differs: sdk=%q wire=%q", sdkResp.Status, wireResp.Status)
	}
	if len(sdkResp.Message.ToolCalls) != len(wireResp.Message.ToolCalls) {
		t.Fatalf("ToolCalls count differs: sdk=%d wire=%d", len(sdkResp.Message.ToolCalls), len(wireResp.Message.ToolCalls))
	}
	if sdkResp.Message.ToolCalls[0].ID != wireResp.Message.ToolCalls[0].ID {
		t.Errorf("ToolCall ID differs")
	}
	if sdkResp.Message.ToolCalls[0].Name != wireResp.Message.ToolCalls[0].Name {
		t.Errorf("ToolCall Name differs")
	}
	sdkArg, _ := sdkResp.Message.ToolCalls[0].Arguments["path"].(string)
	wireArg, _ := wireResp.Message.ToolCalls[0].Arguments["path"].(string)
	if sdkArg != wireArg || sdkArg != "/etc/hosts" {
		t.Errorf("ToolCall Arguments differ: sdk=%q wire=%q", sdkArg, wireArg)
	}
}

// TestWireBackend_RequestShape_MatchesSDK asserts that the JSON body
// the wire backend sends carries the same observable fields as the
// SDK backend for the same AgentRequest. Field set, not byte-equal —
// SDK and wire serialize differently (field ordering, omit behaviour),
// but the operator-visible shape must be equivalent.
func TestWireBackend_RequestShape_MatchesSDK(t *testing.T) {
	var sdkBody, wireBody []byte
	sdkSrv := newStubServer(t, fakeChatCompletionResponse, &sdkBody)
	wireSrv := newStubServer(t, fakeChatCompletionResponse, &wireBody)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := agentic.AgentRequest{
		RequestID: "req-3",
		Messages: []agentic.ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hello"},
		},
	}

	sdk := newWireDifferentialClient(t, sdkSrv.URL, "sdk")
	w := newWireDifferentialClient(t, wireSrv.URL, "wire")

	if _, err := sdk.ChatCompletion(ctx, req); err != nil {
		t.Fatalf("sdk: %v", err)
	}
	if _, err := w.ChatCompletion(ctx, req); err != nil {
		t.Fatalf("wire: %v", err)
	}

	var sdkMap, wireMap map[string]any
	if err := json.Unmarshal(sdkBody, &sdkMap); err != nil {
		t.Fatalf("decode sdk body: %v", err)
	}
	if err := json.Unmarshal(wireBody, &wireMap); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}

	if sdkMap["model"] != wireMap["model"] {
		t.Errorf("model differs: sdk=%v wire=%v", sdkMap["model"], wireMap["model"])
	}
	if sdkMsgs, _ := sdkMap["messages"].([]any); sdkMsgs != nil {
		wireMsgs, _ := wireMap["messages"].([]any)
		if len(sdkMsgs) != len(wireMsgs) {
			t.Errorf("messages length differs: sdk=%d wire=%d", len(sdkMsgs), len(wireMsgs))
		}
	}
}

// TestUseWireBackend_DefaultIsSDK verifies the gate defaults to SDK by
// constructing a Client per backend selection and asserting the gate
// reflects the eagerly-constructed wireClient.
func TestUseWireBackend_DefaultIsSDK(t *testing.T) {
	build := func(backend string) *Client {
		c, err := NewClient(&model.EndpointConfig{
			Provider:    "openai",
			URL:         "http://example.invalid/v1",
			Model:       "test",
			WireBackend: backend,
		})
		if err != nil {
			t.Fatalf("NewClient(%q): %v", backend, err)
		}
		return c
	}
	if build("").useWireBackend() {
		t.Error("empty WireBackend should not select wire path")
	}
	if build("sdk").useWireBackend() {
		t.Error("explicit sdk should not select wire path")
	}
	if !build("wire").useWireBackend() {
		t.Error("wire should select wire path")
	}
}

// TestNewClient_ConcurrentChatCompletion_NoRace exercises the wire
// backend under -race with concurrent ChatCompletion calls. With the
// pre-M1-fix lazy ensureWireClient, this trips a data race; with the
// eager NewClient construction, it passes cleanly.
func TestNewClient_ConcurrentChatCompletion_NoRace(t *testing.T) {
	srv := newStubServer(t, fakeChatCompletionResponse, nil)
	c := newWireDifferentialClient(t, srv.URL, "wire")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const N = 16
	errs := make(chan error, N)
	for range N {
		go func() {
			_, err := c.ChatCompletion(ctx, agentic.AgentRequest{
				RequestID: "req-conc",
				Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
			})
			errs <- err
		}()
	}
	for range N {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ChatCompletion: %v", err)
		}
	}
}
