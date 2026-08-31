package mock

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenAIServer_Start(t *testing.T) {
	server := NewOpenAIServer()
	err := server.Start(":0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	if server.Addr() == "" {
		t.Error("expected non-empty address")
	}

	if server.URL() == "" {
		t.Error("expected non-empty URL")
	}
}

func TestOpenAIServer_HealthEndpoint(t *testing.T) {
	server := NewOpenAIServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp, err := http.Get(server.URL() + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %s", result["status"])
	}
}

func TestOpenAIServer_SimpleCompletion(t *testing.T) {
	server := NewOpenAIServer().
		WithCompletionContent("Test response content")

	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)

	if resp.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", resp.Model)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}

	choice := resp.Choices[0]
	if choice.Message.Role != "assistant" {
		t.Errorf("expected role assistant, got %s", choice.Message.Role)
	}

	if choice.Message.Content != "Test response content" {
		t.Errorf("expected 'Test response content', got %s", choice.Message.Content)
	}

	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %s", choice.FinishReason)
	}
}

func TestOpenAIServer_ToolCallFlow(t *testing.T) {
	server := NewOpenAIServer().
		WithToolArgs("query_entity", `{"entity_id": "test-entity-001"}`).
		WithCompletionContent("Analysis based on tool results")

	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// First request: with tools, should return tool_call
	req1 := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Analyze entity"},
		},
		Tools: []Tool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "query_entity",
				Description: "Query an entity",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	}

	resp1 := makeRequest(t, server.URL()+"/v1/chat/completions", req1)

	if len(resp1.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp1.Choices))
	}

	choice1 := resp1.Choices[0]
	if choice1.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason tool_calls, got %s", choice1.FinishReason)
	}

	if len(choice1.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice1.Message.ToolCalls))
	}

	toolCall := choice1.Message.ToolCalls[0]
	if toolCall.Function.Name != "query_entity" {
		t.Errorf("expected tool name query_entity, got %s", toolCall.Function.Name)
	}

	if toolCall.Function.Arguments != `{"entity_id": "test-entity-001"}` {
		t.Errorf("unexpected arguments: %s", toolCall.Function.Arguments)
	}

	if toolCall.ID == "" {
		t.Error("expected non-empty tool call ID")
	}

	// Second request: with tool results, should return completion
	req2 := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Analyze entity"},
			{Role: "assistant", ToolCalls: choice1.Message.ToolCalls},
			{Role: "tool", ToolCallID: toolCall.ID, Content: `{"id": "test-entity-001", "type": "sensor"}`},
		},
		Tools: []Tool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "query_entity",
				Description: "Query an entity",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	}

	resp2 := makeRequest(t, server.URL()+"/v1/chat/completions", req2)

	if len(resp2.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp2.Choices))
	}

	choice2 := resp2.Choices[0]
	if choice2.FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %s", choice2.FinishReason)
	}

	if choice2.Message.Content != "Analysis based on tool results" {
		t.Errorf("unexpected content: %s", choice2.Message.Content)
	}
}

func TestOpenAIServer_RequestTracking(t *testing.T) {
	server := NewOpenAIServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	if server.RequestCount() != 0 {
		t.Errorf("expected 0 requests, got %d", server.RequestCount())
	}

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	makeRequest(t, server.URL()+"/v1/chat/completions", req)

	if server.RequestCount() != 1 {
		t.Errorf("expected 1 request, got %d", server.RequestCount())
	}

	lastReq := server.LastRequest()
	if lastReq == nil {
		t.Fatal("expected last request to be set")
	}

	if lastReq.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", lastReq.Model)
	}
}

func TestOpenAIServer_RequestDelay(t *testing.T) {
	server := NewOpenAIServer().
		WithRequestDelay(100 * time.Millisecond)

	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	start := time.Now()
	makeRequest(t, server.URL()+"/v1/chat/completions", req)
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms delay, got %v", elapsed)
	}
}

func TestOpenAIServer_UnknownTool(t *testing.T) {
	server := NewOpenAIServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Do something"},
		},
		Tools: []Tool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "unknown_tool",
				Description: "Unknown tool",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	}

	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)

	// Should still return tool call with empty args
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}

	toolCall := resp.Choices[0].Message.ToolCalls[0]
	if toolCall.Function.Arguments != "{}" {
		t.Errorf("expected empty args for unknown tool, got %s", toolCall.Function.Arguments)
	}
}

func TestOpenAIServer_UsageStats(t *testing.T) {
	server := NewOpenAIServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)

	if resp.Usage.PromptTokens <= 0 {
		t.Error("expected positive prompt tokens")
	}

	if resp.Usage.CompletionTokens <= 0 {
		t.Error("expected positive completion tokens")
	}

	if resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
		t.Error("total tokens should equal prompt + completion")
	}
}

func TestOpenAIServer_StreamingCompletion(t *testing.T) {
	server := NewOpenAIServer().
		WithCompletionContent("Hello streaming world!")

	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}

	chunks := makeStreamingRequest(t, server.URL()+"/v1/chat/completions", req)

	// Should have content chunks + usage chunk (at least 3)
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Concatenate content from all chunks
	var content string
	var gotRole bool
	var gotFinishReason bool
	var gotUsage bool

	for _, chunk := range chunks {
		if chunk.Usage != nil {
			gotUsage = true
			if chunk.Usage.PromptTokens <= 0 {
				t.Error("expected positive prompt tokens in usage chunk")
			}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Role == "assistant" {
				gotRole = true
			}
			content += choice.Delta.Content
			if choice.FinishReason != nil {
				gotFinishReason = true
			}
		}
	}

	if content != "Hello streaming world!" {
		t.Errorf("concatenated content = %q, want %q", content, "Hello streaming world!")
	}
	if !gotRole {
		t.Error("expected at least one chunk with role=assistant")
	}
	if !gotFinishReason {
		t.Error("expected at least one chunk with finish_reason")
	}
	if !gotUsage {
		t.Error("expected a usage chunk")
	}
}

func TestOpenAIServer_StreamingToolCall(t *testing.T) {
	server := NewOpenAIServer().
		WithToolArgs("query_entity", `{"entity_id": "test-001"}`)

	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "test-model",
		Messages: []ChatMessage{
			{Role: "user", Content: "Analyze entity"},
		},
		Tools: []Tool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "query_entity",
				Description: "Query an entity",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		Stream: true,
	}

	chunks := makeStreamingRequest(t, server.URL()+"/v1/chat/completions", req)

	// Should have tool call delta chunks + finish reason + usage (at least 4)
	if len(chunks) < 4 {
		t.Fatalf("expected at least 4 chunks, got %d", len(chunks))
	}

	// Reconstruct tool call from deltas
	var toolName, toolArgs, toolID string
	var gotFinishReason bool

	for _, chunk := range chunks {
		for _, choice := range chunk.Choices {
			for _, tc := range choice.Delta.ToolCalls {
				if tc.ID != "" {
					toolID = tc.ID
				}
				toolName += tc.Function.Name
				toolArgs += tc.Function.Arguments
			}
			if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
				gotFinishReason = true
			}
		}
	}

	if toolName != "query_entity" {
		t.Errorf("tool name = %q, want query_entity", toolName)
	}
	if toolArgs != `{"entity_id": "test-001"}` {
		t.Errorf("tool args = %q, want %q", toolArgs, `{"entity_id": "test-001"}`)
	}
	if toolID == "" {
		t.Error("expected non-empty tool call ID")
	}
	if !gotFinishReason {
		t.Error("expected finish_reason=tool_calls")
	}
}

// streamChunkForTest is used to unmarshal SSE chunks in tests.
type streamChunkForTest struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

func makeStreamingRequest(t *testing.T, url string, req ChatCompletionRequest) []streamChunkForTest {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", contentType)
	}

	var chunks []streamChunkForTest
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk streamChunkForTest
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("failed to unmarshal chunk: %v\ndata: %s", err, data)
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}

func makeRequest(t *testing.T, url string, req ChatCompletionRequest) ChatCompletionResponse {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	return result
}

// TestOpenAIServer_RoleResponses verifies that WithRoleResponses routes the
// completion body based on prompt content, picking the first marker match in
// declaration order. This is how multi-agent scenarios (researcher vs
// synthesizer vs partial-synthesizer) get deterministic per-role outputs.
func TestOpenAIServer_RoleResponses(t *testing.T) {
	server := NewOpenAIServer().WithRoleResponses([]RoleResponse{
		{Marker: "Research the following", Content: "findings with SUBTOPICS:\n- subtopic A\n- subtopic B"},
		{Marker: "focused research agent", Content: "focused sub-findings"},
		{Marker: "research synthesizer", Content: "synthesis report"},
	})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	tests := []struct {
		name       string
		systemMsg  string
		wantSubstr string
	}{
		{"researcher routing", "You are a researcher. Research the following question thoroughly.", "SUBTOPICS:"},
		{"sub-researcher routing", "You are a focused research agent", "focused sub-findings"},
		{"synthesizer routing", "You are a research synthesizer", "synthesis report"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := ChatCompletionRequest{
				Model: "mock",
				Messages: []ChatMessage{
					{Role: "system", Content: tc.systemMsg},
					{Role: "user", Content: "go"},
				},
			}
			resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
			if len(resp.Choices) == 0 {
				t.Fatal("expected at least one choice")
			}
			got := resp.Choices[0].Message.Content
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("expected content to contain %q, got %q", tc.wantSubstr, got)
			}
		})
	}
}

// TestOpenAIServer_RoleResponses_NoMatchFallsBack verifies that when no
// marker matches the mock falls back to the default completion content, so
// callers can mix role-routed markers with default behaviour.
func TestOpenAIServer_RoleResponses_NoMatchFallsBack(t *testing.T) {
	server := NewOpenAIServer().
		WithCompletionContent("default fallback").
		WithRoleResponses([]RoleResponse{
			{Marker: "never-matches", Content: "should not be returned"},
		})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model:    "mock",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	if resp.Choices[0].Message.Content != "default fallback" {
		t.Errorf("expected default fallback, got %q", resp.Choices[0].Message.Content)
	}
}

// TestOpenAIServer_SubmitAfter verifies that WithSubmitAfter(n) causes the
// mock to emit a submit_work tool call after n completed tool rounds,
// provided submit_work is advertised in the request. Without submit_work in
// the tools list the mock proceeds with normal behaviour.
func TestOpenAIServer_SubmitAfter(t *testing.T) {
	server := NewOpenAIServer().WithSubmitAfter(1)
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	tools := []Tool{
		{Type: "function", Function: FunctionDef{Name: "graph_query"}},
		{Type: "function", Function: FunctionDef{Name: "submit_work"}},
	}

	// Turn 1: no tool results yet → normal tool-call (first tool = graph_query).
	req1 := ChatCompletionRequest{
		Model:    "mock",
		Tools:    tools,
		Messages: []ChatMessage{{Role: "user", Content: "go"}},
	}
	resp1 := makeRequest(t, server.URL()+"/v1/chat/completions", req1)
	if len(resp1.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("turn 1: expected one tool call, got %d", len(resp1.Choices[0].Message.ToolCalls))
	}
	if resp1.Choices[0].Message.ToolCalls[0].Function.Name != "graph_query" {
		t.Errorf("turn 1: expected graph_query call, got %q", resp1.Choices[0].Message.ToolCalls[0].Function.Name)
	}

	// Turn 2: one tool result now in history → submitAfter threshold met → submit_work.
	req2 := ChatCompletionRequest{
		Model: "mock",
		Tools: tools,
		Messages: []ChatMessage{
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "graph_query"}}}},
			{Role: "tool", ToolCallID: "c1", Content: "results"},
		},
	}
	resp2 := makeRequest(t, server.URL()+"/v1/chat/completions", req2)
	if len(resp2.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("turn 2: expected submit_work call, got %d tool calls", len(resp2.Choices[0].Message.ToolCalls))
	}
	if resp2.Choices[0].Message.ToolCalls[0].Function.Name != "submit_work" {
		t.Errorf("turn 2: expected submit_work, got %q", resp2.Choices[0].Message.ToolCalls[0].Function.Name)
	}
}

// TestOpenAIServer_SubmitAfter_NoSubmitToolFallsThrough verifies that if
// submit_work isn't advertised by the request the cadence is ignored and the
// mock falls through to normal completion behaviour. Prevents the mock from
// emitting a tool call the caller can't handle.
func TestOpenAIServer_SubmitAfter_NoSubmitToolFallsThrough(t *testing.T) {
	server := NewOpenAIServer().WithSubmitAfter(1)
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Tools list without submit_work.
	tools := []Tool{{Type: "function", Function: FunctionDef{Name: "graph_query"}}}

	req := ChatCompletionRequest{
		Model: "mock",
		Tools: tools,
		Messages: []ChatMessage{
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "graph_query"}}}},
			{Role: "tool", ToolCallID: "c1", Content: "results"},
		},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	// One tool round already completed and submit_work unavailable → completion.
	if len(resp.Choices[0].Message.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %q", resp.Choices[0].FinishReason)
	}
}

// TestOpenAIServer_RoleToolCallSequence verifies the coordinator-style
// deterministic tool-call injection: marker-matching requests return the
// scripted tool call with the exact args the test asks for, and the cursor
// advances so "first coordinator call → fan_out, second → synthesize"
// works without more machinery.
func TestOpenAIServer_RoleToolCallSequence(t *testing.T) {
	server := NewOpenAIServer().WithRoleToolCallSequence([]RoleToolCall{
		{
			Marker:   "research coordinator",
			ToolName: "decide",
			Args:     map[string]any{"action": "fan_out", "reason": "split topics", "subtopics": []string{"A", "B"}},
		},
		{
			Marker:   "research coordinator",
			ToolName: "decide",
			Args:     map[string]any{"action": "synthesize", "reason": "enough evidence"},
		},
	})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	tools := []Tool{{Type: "function", Function: FunctionDef{Name: "decide"}}}
	coordReq := ChatCompletionRequest{
		Model:    "mock",
		Tools:    tools,
		Messages: []ChatMessage{{Role: "system", Content: "You are a research coordinator. decide wisely."}},
	}

	// Call 1: fan_out decision.
	resp1 := makeRequest(t, server.URL()+"/v1/chat/completions", coordReq)
	if len(resp1.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("call 1: expected one tool call, got %d", len(resp1.Choices[0].Message.ToolCalls))
	}
	call1 := resp1.Choices[0].Message.ToolCalls[0]
	if call1.Function.Name != "decide" {
		t.Errorf("call 1: tool name = %q, want decide", call1.Function.Name)
	}
	if !strings.Contains(call1.Function.Arguments, `"action":"fan_out"`) {
		t.Errorf("call 1: expected fan_out in args, got %q", call1.Function.Arguments)
	}
	if !strings.Contains(call1.Function.Arguments, `"subtopics"`) {
		t.Errorf("call 1: expected subtopics in args, got %q", call1.Function.Arguments)
	}

	// Call 2: synthesize decision.
	resp2 := makeRequest(t, server.URL()+"/v1/chat/completions", coordReq)
	call2 := resp2.Choices[0].Message.ToolCalls[0]
	if !strings.Contains(call2.Function.Arguments, `"action":"synthesize"`) {
		t.Errorf("call 2: expected synthesize in args, got %q", call2.Function.Arguments)
	}

	// Call 3+: sequence exhausted, sticky on last entry.
	resp3 := makeRequest(t, server.URL()+"/v1/chat/completions", coordReq)
	call3 := resp3.Choices[0].Message.ToolCalls[0]
	if !strings.Contains(call3.Function.Arguments, `"action":"synthesize"`) {
		t.Errorf("call 3: expected sticky synthesize, got %q", call3.Function.Arguments)
	}
}

// TestOpenAIServer_RoleToolCall_NoMarkerFallsThrough verifies that a
// request not matching any marker uses the normal (tool-call or completion)
// routing, and the cursor doesn't burn on non-matching requests.
func TestOpenAIServer_RoleToolCall_NoMarkerFallsThrough(t *testing.T) {
	server := NewOpenAIServer().
		WithCompletionContent("regular completion").
		WithRoleToolCallSequence([]RoleToolCall{
			{Marker: "ONLY_COORDINATOR", ToolName: "decide", Args: map[string]any{"action": "done", "reason": "x"}},
		})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "mock",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello, no coordinator marker here"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "whatever"}}}},
			{Role: "tool", ToolCallID: "c1", Content: "result"},
		},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	if resp.Choices[0].Message.Content != "regular completion" {
		t.Errorf("expected regular completion fallthrough, got %q", resp.Choices[0].Message.Content)
	}
}

// TestOpenAIServer_RoleToolCall_ToolNotAdvertisedFallsThrough verifies
// that a marker match without the named tool in the request's Tools list
// falls through to normal routing rather than injecting an unadvertised
// tool call (which would cause the agentic-tools component to reject the
// call and mask the scenario bug).
func TestOpenAIServer_RoleToolCall_ToolNotAdvertisedFallsThrough(t *testing.T) {
	server := NewOpenAIServer().
		WithCompletionContent("fallback").
		WithRoleToolCallSequence([]RoleToolCall{
			{Marker: "coordinator_marker", ToolName: "decide", Args: map[string]any{"action": "done", "reason": "x"}},
		})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Tool list does NOT include "decide". Request should fall through
	// even though the marker matches.
	req := ChatCompletionRequest{
		Model: "mock",
		Tools: []Tool{{Type: "function", Function: FunctionDef{Name: "some_other_tool"}}},
		Messages: []ChatMessage{
			{Role: "user", Content: "something something coordinator_marker something"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "some_other_tool"}}}},
			{Role: "tool", ToolCallID: "c1", Content: "result"},
		},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	if resp.Choices[0].Message.Content != "fallback" {
		t.Errorf("expected fallback content, got %q", resp.Choices[0].Message.Content)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 0 {
		t.Errorf("unexpected tool calls injected: %+v", resp.Choices[0].Message.ToolCalls)
	}
}

// TestOpenAIServer_ObserveEntityIDSuffix verifies the mock resolves the
// placeholder from the entity ID the REQUEST carries. Positions 1-2 of a
// deployment's entity IDs carry an entropy suffix minted at first boot
// (ADR-104), so a scripted fixture cannot spell one; it observes instead.
func TestOpenAIServer_ObserveEntityIDSuffix(t *testing.T) {
	server := NewOpenAIServer().WithRoleResponses([]RoleResponse{{
		Marker:                "synthesis stage",
		Content:               `{"evidence_refs":["` + ObservedEntityIDPlaceholder + `"]}`,
		ObserveEntityIDSuffix: "seed.research.document.controlled",
	}})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "mock",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are the synthesis stage of a graph-search pipeline."},
			{Role: "user", Content: "Evidence:\n  [0] c360.rg-e2e-9f3a71.seed.research.document.controlled (tier=0 src=walk_seeds.entity_state)\n"},
		},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	want := `{"evidence_refs":["c360.rg-e2e-9f3a71.seed.research.document.controlled"]}`
	if got := resp.Choices[0].Message.Content; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// TestOpenAIServer_ObserveEntityIDSuffix_MissLeavesPlaceholder pins the
// fail-loud half: when the request carries no matching entity ID the mock
// leaves the placeholder verbatim rather than inventing an ID, so the
// scenario fails on the artifact that depended on it.
func TestOpenAIServer_ObserveEntityIDSuffix_MissLeavesPlaceholder(t *testing.T) {
	server := NewOpenAIServer().WithRoleResponses([]RoleResponse{{
		Marker:                "synthesis stage",
		Content:               `{"evidence_refs":["` + ObservedEntityIDPlaceholder + `"]}`,
		ObserveEntityIDSuffix: "seed.research.document.controlled",
	}})
	if err := server.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	req := ChatCompletionRequest{
		Model: "mock",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are the synthesis stage of a graph-search pipeline."},
			{Role: "user", Content: "Evidence:\n  (none)\n"},
		},
	}
	resp := makeRequest(t, server.URL()+"/v1/chat/completions", req)
	got := resp.Choices[0].Message.Content
	if !strings.Contains(got, ObservedEntityIDPlaceholder) {
		t.Errorf("content = %q, want the unresolved placeholder %q", got, ObservedEntityIDPlaceholder)
	}
}
