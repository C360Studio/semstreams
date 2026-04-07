package agenticmodel_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// sseServer creates an httptest.Server that serves SSE chunks then [DONE].
func sseServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// newStreamingClient creates a Client with Stream=true pointing at the given URL.
func newStreamingClient(t *testing.T, url string) *agenticmodel.Client {
	t.Helper()
	ep := &model.EndpointConfig{
		URL:       url,
		Model:     "test-model",
		MaxTokens: 32768,
		Stream:    true,
	}
	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	return client
}

func simpleRequest(requestID string) agentic.AgentRequest {
	return agentic.AgentRequest{
		RequestID: requestID,
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "Hello"}},
		Model:     "test-model",
	}
}

func TestStreamAccumulator_ContentAggregation(t *testing.T) {
	chunks := []string{
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" world"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-acc"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "complete" {
		t.Errorf("Status = %q, want %q", resp.Status, "complete")
	}
	if resp.Message.Content != "Hello world!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello world!")
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("Role = %q, want %q", resp.Message.Role, "assistant")
	}
	if resp.TokenUsage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", resp.TokenUsage.PromptTokens)
	}
	if resp.TokenUsage.CompletionTokens != 3 {
		t.Errorf("CompletionTokens = %d, want 3", resp.TokenUsage.CompletionTokens)
	}
	if resp.RequestID != "req-acc" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req-acc")
	}
}

func TestStreamAccumulator_LengthTruncation(t *testing.T) {
	chunks := []string{
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" response"},"finish_reason":"length"}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4096,"total_tokens":4106}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-trunc"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "length_truncated" {
		t.Errorf("Status = %q, want %q", resp.Status, "length_truncated")
	}
	if resp.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "length")
	}
	if resp.Message.Content != "Partial response" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Partial response")
	}
}

func TestStreamAccumulator_ToolCallAggregation(t *testing.T) {
	chunks := []string{
		// First tool call starts (index 0)
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		// First tool call arguments
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"London\"}"}}]}}]}`,
		// Second tool call starts (index 1)
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"get_time","arguments":"{\"tz\":\"UTC\"}"}}]}}]}`,
		// Finish
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":15,"completion_tokens":20,"total_tokens":35}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-tools"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "tool_call" {
		t.Errorf("Status = %q, want %q", resp.Status, "tool_call")
	}

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("ToolCalls count = %d, want 2", len(resp.Message.ToolCalls))
	}

	// Tool calls should be sorted by index
	tc0 := resp.Message.ToolCalls[0]
	if tc0.ID != "call_1" || tc0.Name != "get_weather" {
		t.Errorf("ToolCall[0] = {ID:%q, Name:%q}, want {ID:call_1, Name:get_weather}", tc0.ID, tc0.Name)
	}
	loc, ok := tc0.Arguments["location"]
	if !ok || loc != "London" {
		t.Errorf("ToolCall[0].Arguments[location] = %v, want London", loc)
	}

	tc1 := resp.Message.ToolCalls[1]
	if tc1.ID != "call_2" || tc1.Name != "get_time" {
		t.Errorf("ToolCall[1] = {ID:%q, Name:%q}, want {ID:call_2, Name:get_time}", tc1.ID, tc1.Name)
	}
}

func TestStreamChatCompletion_SimpleSuccess(t *testing.T) {
	chunks := []string{
		`{"id":"s1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"}}]}`,
		`{"id":"s1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":"stop"}]}`,
		`{"id":"s1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-simple"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "complete" {
		t.Errorf("Status = %q, want complete", resp.Status)
	}
	if resp.Message.Content != "Hi there" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hi there")
	}
}

func TestStreamChatCompletion_WithToolCalls(t *testing.T) {
	chunks := []string{
		`{"id":"t1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"search","arguments":"{\"q\":"}}]}}]}`,
		`{"id":"t1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hello\"}"}}]}}]}`,
		`{"id":"t1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"t1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":12,"total_tokens":20}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-tc"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "tool_call" {
		t.Errorf("Status = %q, want tool_call", resp.Status)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCall name = %q, want search", resp.Message.ToolCalls[0].Name)
	}
}

func TestStreamChatCompletion_WithReasoningContent(t *testing.T) {
	chunks := []string{
		`{"id":"r1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Let me think"}}]}`,
		`{"id":"r1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":" about this..."}}]}`,
		`{"id":"r1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"The answer is 42."}}]}`,
		`{"id":"r1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"r1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-reasoning"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "complete" {
		t.Errorf("Status = %q, want complete", resp.Status)
	}
	if resp.Message.ReasoningContent != "Let me think about this..." {
		t.Errorf("ReasoningContent = %q, want %q", resp.Message.ReasoningContent, "Let me think about this...")
	}
	if resp.Message.Content != "The answer is 42." {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "The answer is 42.")
	}
}

func TestStreamChatCompletion_MidStreamError(t *testing.T) {
	// Server sends a chunk then abruptly kills the TCP connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		fmt.Fprintf(w, "data: %s\n\n", `{"id":"e1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`)
		flusher.Flush()

		// Hijack and close the raw TCP connection to simulate mid-stream failure
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Hijacker")
		}
		conn, _, _ := hijacker.Hijack()
		conn.Close()
	}))
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-mid-err"))
	// Mid-stream error returns AgentResponse{Status: "error"}, not Go error
	if err != nil {
		t.Fatalf("ChatCompletion() returned Go error: %v (expected AgentResponse with error status)", err)
	}
	if resp.Status != "error" {
		t.Errorf("Status = %q, want error", resp.Status)
	}
	if resp.Error == "" {
		t.Error("Error field should not be empty for mid-stream error")
	}
}

func TestStreamChatCompletion_MidStreamError_PreservesPartialTokens(t *testing.T) {
	// Server sends a usage chunk before killing the connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send a content chunk
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"pt1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`)
		flusher.Flush()

		// Send a usage chunk (some providers send usage incrementally)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"pt1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":42,"completion_tokens":7,"total_tokens":49}}`)
		flusher.Flush()

		// Kill the connection mid-stream
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Hijacker")
		}
		conn, _, _ := hijacker.Hijack()
		conn.Close()
	}))
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-partial-tokens"))
	if err != nil {
		t.Fatalf("ChatCompletion() returned Go error: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("Status = %q, want error", resp.Status)
	}
	// Partial tokens from the usage chunk should be preserved
	if resp.TokenUsage.PromptTokens != 42 {
		t.Errorf("PromptTokens = %d, want 42", resp.TokenUsage.PromptTokens)
	}
	if resp.TokenUsage.CompletionTokens != 7 {
		t.Errorf("CompletionTokens = %d, want 7", resp.TokenUsage.CompletionTokens)
	}
}

func TestStreamChatCompletion_ConnectionError(t *testing.T) {
	// Server that is immediately closed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return error status to simulate connection failure
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("Bad Gateway"))
	}))
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-conn-err"))
	// Connection errors are retried; after max attempts, returns error response
	if err != nil {
		t.Fatalf("ChatCompletion() returned Go error: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("Status = %q, want error", resp.Status)
	}
}

func TestStreamChatCompletion_ChunkHandler(t *testing.T) {
	chunks := []string{
		`{"id":"h1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"A"}}]}`,
		`{"id":"h1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"B"}}]}`,
		`{"id":"h1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
		`{"id":"h1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"h1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	var mu sync.Mutex
	var received []agenticmodel.StreamChunk
	client.SetChunkHandler(func(chunk agenticmodel.StreamChunk) {
		mu.Lock()
		received = append(received, chunk)
		mu.Unlock()
	})

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-handler"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}
	if resp.Status != "complete" {
		t.Errorf("Status = %q, want complete", resp.Status)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have received chunks for content deltas + reasoning + done
	// Empty deltas (finish_reason only) also trigger handler but with empty content
	if len(received) == 0 {
		t.Fatal("ChunkHandler received no chunks")
	}

	// Last chunk should be Done=true
	last := received[len(received)-1]
	if !last.Done {
		t.Error("Last chunk should have Done=true")
	}

	// Check that content deltas were received
	var contentParts, reasoningParts []string
	for _, c := range received {
		if c.ContentDelta != "" {
			contentParts = append(contentParts, c.ContentDelta)
		}
		if c.ReasoningDelta != "" {
			reasoningParts = append(reasoningParts, c.ReasoningDelta)
		}
	}
	if len(contentParts) != 2 {
		t.Errorf("Content deltas = %d, want 2 (A, B)", len(contentParts))
	}
	if len(reasoningParts) != 1 {
		t.Errorf("Reasoning deltas = %d, want 1 (think)", len(reasoningParts))
	}

	// All chunks should have the correct request ID
	for i, c := range received {
		if c.RequestID != "req-handler" {
			t.Errorf("Chunk[%d].RequestID = %q, want req-handler", i, c.RequestID)
		}
	}
}

func TestStreamChatCompletion_GeminiMissingIndex(t *testing.T) {
	// Gemini omits the index field on streaming tool call deltas.
	// Two tool calls without index fields should be preserved as separate calls.
	chunks := []string{
		// First tool call: ID present, no index
		`{"id":"g1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"call_aaa","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		// First tool call arguments continuation: no ID, no index
		`{"id":"g1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"London\"}"}}]}}]}`,
		// Second tool call: new ID, no index
		`{"id":"g1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_bbb","type":"function","function":{"name":"get_time","arguments":"{\"tz\":\"UTC\"}"}}]}}]}`,
		// Finish
		`{"id":"g1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"g1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-gemini-idx"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "tool_call" {
		t.Errorf("Status = %q, want tool_call", resp.Status)
	}

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("ToolCalls count = %d, want 2 (Gemini missing index collapsed parallel calls)", len(resp.Message.ToolCalls))
	}

	tc0 := resp.Message.ToolCalls[0]
	if tc0.ID != "call_aaa" || tc0.Name != "get_weather" {
		t.Errorf("ToolCall[0] = {ID:%q, Name:%q}, want {ID:call_aaa, Name:get_weather}", tc0.ID, tc0.Name)
	}
	loc, ok := tc0.Arguments["location"]
	if !ok || loc != "London" {
		t.Errorf("ToolCall[0].Arguments[location] = %v, want London", loc)
	}

	tc1 := resp.Message.ToolCalls[1]
	if tc1.ID != "call_bbb" || tc1.Name != "get_time" {
		t.Errorf("ToolCall[1] = {ID:%q, Name:%q}, want {ID:call_bbb, Name:get_time}", tc1.ID, tc1.Name)
	}
}

func TestStreamChatCompletion_GeminiSameNameParallelTools(t *testing.T) {
	// Regression: Gemini parallel tool calls with the same function name
	// had names concatenated ("run_commandrun_commandrun_command") because
	// processDelta used += instead of = for Function.Name.
	chunks := []string{
		`{"id":"g2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"call_111","type":"function","function":{"name":"run_command","arguments":""}}]}}]}`,
		`{"id":"g2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"cmd\":\"ls\"}"}}]}}]}`,
		`{"id":"g2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_222","type":"function","function":{"name":"run_command","arguments":"{\"cmd\":\"pwd\"}"}}]}}]}`,
		`{"id":"g2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_333","type":"function","function":{"name":"run_command","arguments":"{\"cmd\":\"date\"}"}}]}}]}`,
		`{"id":"g2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"g2","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`,
	}

	server := sseServer(t, chunks)
	defer server.Close()

	client := newStreamingClient(t, server.URL)

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-gemini-same"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if len(resp.Message.ToolCalls) != 3 {
		t.Fatalf("ToolCalls count = %d, want 3", len(resp.Message.ToolCalls))
	}

	for i, tc := range resp.Message.ToolCalls {
		if tc.Name != "run_command" {
			t.Errorf("ToolCall[%d].Name = %q, want %q (name was concatenated)", i, tc.Name, "run_command")
		}
	}
}

func TestStreamChatCompletion_NonStreamEndpoint(t *testing.T) {
	// Non-streaming endpoint should use the regular path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id": "ns1",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "test-model",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "non-stream"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
		}`)
	}))
	defer server.Close()

	ep := &model.EndpointConfig{
		URL:       server.URL,
		Model:     "test-model",
		MaxTokens: 32768,
		Stream:    false, // explicitly non-streaming
	}
	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	resp, err := client.ChatCompletion(context.Background(), simpleRequest("req-nostream"))
	if err != nil {
		t.Fatalf("ChatCompletion() error: %v", err)
	}

	if resp.Status != "complete" {
		t.Errorf("Status = %q, want complete", resp.Status)
	}
	if resp.Message.Content != "non-stream" {
		t.Errorf("Content = %q, want non-stream", resp.Message.Content)
	}
}
