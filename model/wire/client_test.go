package wire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ChatCompletion_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"gpt-4o"`) {
			t.Errorf("request body: %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"r1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
		AuthHeader: "Bearer k",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := c.ChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.ID != "r1" || len(resp.Choices) != 1 {
		t.Errorf("decoded shape wrong: %+v", resp)
	}
	if got, ok := resp.Choices[0].Message.ContentString(); !ok || got != "hi" {
		t.Errorf("content drift: %q", got)
	}
}

func TestClient_ChatCompletion_ErrorEnvelope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`[{"error":{"message":"missing thought_signature","status":"INVALID_ARGUMENT"}}]`))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	_, err := c.ChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "gemini",
		Messages: []Message{{Role: "user"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 || !strings.Contains(apiErr.Message, "thought_signature") {
		t.Errorf("api err shape wrong: %+v", apiErr)
	}
}

func TestClient_ChatCompletionStream(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"}}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	stream, err := c.ChatCompletionStream(context.Background(), &ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}
	defer stream.Close()

	acc := NewAccumulator()
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(chunk); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, finish, _ := acc.Final()
	if got, ok := msg.ContentString(); !ok || got != "Hello" {
		t.Errorf("streamed content: got (%q,%v), want (Hello,true)", got, ok)
	}
	if finish != "stop" {
		t.Errorf("finish: %q", finish)
	}
}

func TestClient_RejectsMissingHTTPClient(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(ClientConfig{BaseURL: "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Embeddings(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	c, _ := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", HTTPClient: srv.Client()})
	resp, err := c.Embeddings(context.Background(), &EmbeddingsRequest{
		Model: "text-embedding-3-small",
		Input: json.RawMessage(`"hello"`),
	})
	if err != nil {
		t.Fatalf("Embeddings: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 3 {
		t.Errorf("decoded shape wrong: %+v", resp)
	}
}
