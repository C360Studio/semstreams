package agenticmodel_test

import (
	"testing"

	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	openai "github.com/sashabaranov/go-openai"
)

// --- AdapterFor ---

func TestAdapterFor_Gemini(t *testing.T) {
	a := agenticmodel.AdapterFor("gemini")
	if a.Name() != "gemini" {
		t.Errorf("AdapterFor(gemini).Name() = %q, want gemini", a.Name())
	}
}

func TestAdapterFor_OpenAI(t *testing.T) {
	a := agenticmodel.AdapterFor("openai")
	if a.Name() != "openai" {
		t.Errorf("AdapterFor(openai).Name() = %q, want openai", a.Name())
	}
}

func TestAdapterFor_Unknown(t *testing.T) {
	// Use "anthropic" as the unknown example; it is intentionally not in
	// the AdapterFor switch and falls through to GenericAdapter (the ADR-034
	// audit flagged this — Anthropic structured-output translation is
	// deferred). "ollama" was the previous example string here but is now
	// a known provider with its own adapter.
	a := agenticmodel.AdapterFor("anthropic")
	if a.Name() != "generic" {
		t.Errorf("AdapterFor(anthropic).Name() = %q, want generic", a.Name())
	}
}

func TestAdapterFor_Empty(t *testing.T) {
	a := agenticmodel.AdapterFor("")
	if a.Name() != "generic" {
		t.Errorf("AdapterFor(\"\").Name() = %q, want generic", a.Name())
	}
}

// --- NormalizeMessages (shared quirks) ---

func TestGeminiAdapter_NormalizeMessages_ToolNameFallback(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	messages := []openai.ChatCompletionMessage{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "tool", ToolCallID: "call-2", Name: "read_file"},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Name != "unknown_tool" {
		t.Errorf("empty name → %q, want unknown_tool", result[0].Name)
	}
	if result[1].Name != "read_file" {
		t.Errorf("existing name → %q, want read_file", result[1].Name)
	}
}

func TestGeminiAdapter_NormalizeMessages_AssistantContentFallback(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	messages := []openai.ChatCompletionMessage{
		{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []openai.ToolCall{{ID: "call-1"}},
		},
		{
			Role:      "assistant",
			Content:   "existing",
			ToolCalls: []openai.ToolCall{{ID: "call-2"}},
		},
		{
			Role:    "assistant",
			Content: "",
			// No tool calls — should NOT get space
		},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Content != " " {
		t.Errorf("empty content with tool_calls → %q, want space", result[0].Content)
	}
	if result[1].Content != "existing" {
		t.Errorf("existing content → %q, want existing", result[1].Content)
	}
	if result[2].Content != "" {
		t.Errorf("no tool_calls, empty content → %q, want empty", result[2].Content)
	}
}

func TestGenericAdapter_NormalizeMessages_SameAsGemini(t *testing.T) {
	// Generic adapter applies the same safe normalizations
	adapter := agenticmodel.AdapterFor("")
	messages := []openai.ChatCompletionMessage{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "assistant", Content: "", ToolCalls: []openai.ToolCall{{ID: "call-1"}}},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Name != "unknown_tool" {
		t.Errorf("generic: empty tool name → %q, want unknown_tool", result[0].Name)
	}
	if result[1].Content != " " {
		t.Errorf("generic: empty assistant content with tool_calls → %q, want space", result[1].Content)
	}
}

func TestGenericAdapter_NormalizeMessages_CollapsesConsecutiveSameRole(t *testing.T) {
	adapter := agenticmodel.AdapterFor("")
	messages := []openai.ChatCompletionMessage{
		{Role: "system", Content: "rule one"},
		{Role: "system", Content: "rule two"},
		{Role: "user", Content: "hi"},
		{Role: "user", Content: "are you there?"},
		{Role: "assistant", Content: "yes"},
	}

	result := adapter.NormalizeMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages after collapse, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "rule one\n\nrule two" {
		t.Errorf("merged system → role=%q content=%q", result[0].Role, result[0].Content)
	}
	if result[1].Role != "user" || result[1].Content != "hi\n\nare you there?" {
		t.Errorf("merged user → role=%q content=%q", result[1].Role, result[1].Content)
	}
	if result[2].Role != "assistant" || result[2].Content != "yes" {
		t.Errorf("standalone assistant → role=%q content=%q", result[2].Role, result[2].Content)
	}
}

func TestGenericAdapter_NormalizeMessages_PreservesToolPairs(t *testing.T) {
	adapter := agenticmodel.AdapterFor("")
	messages := []openai.ChatCompletionMessage{
		{Role: "assistant", ToolCalls: []openai.ToolCall{{ID: "call-1"}}},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{ID: "call-2"}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: "result one"},
		{Role: "tool", ToolCallID: "call-2", Name: "read_file", Content: "result two"},
	}

	result := adapter.NormalizeMessages(messages)

	if len(result) != 4 {
		t.Fatalf("expected 4 messages preserved (assistant w/ tool_calls + tool results), got %d", len(result))
	}
	if len(result[0].ToolCalls) != 1 || result[0].ToolCalls[0].ID != "call-1" {
		t.Errorf("first assistant tool_calls lost: %+v", result[0].ToolCalls)
	}
	if len(result[1].ToolCalls) != 1 || result[1].ToolCalls[0].ID != "call-2" {
		t.Errorf("second assistant tool_calls lost: %+v", result[1].ToolCalls)
	}
	if result[2].ToolCallID != "call-1" || result[3].ToolCallID != "call-2" {
		t.Errorf("tool result IDs collapsed: %q, %q", result[2].ToolCallID, result[3].ToolCallID)
	}
}

func TestGenericAdapter_NormalizeMessages_MergesEmptyContent(t *testing.T) {
	adapter := agenticmodel.AdapterFor("")
	messages := []openai.ChatCompletionMessage{
		{Role: "user", Content: ""},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "assistant", Content: ""},
	}

	result := adapter.NormalizeMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "hello" {
		t.Errorf("empty + nonempty user → %q, want %q", result[0].Content, "hello")
	}
	if result[1].Content != "hi" {
		t.Errorf("nonempty + empty assistant → %q, want %q", result[1].Content, "hi")
	}
}

func TestOpenAIAdapter_NormalizeMessages_NoChanges(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	messages := []openai.ChatCompletionMessage{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "assistant", Content: "", ToolCalls: []openai.ToolCall{{ID: "call-1"}}},
	}

	result := adapter.NormalizeMessages(messages)

	// OpenAI adapter does NOT apply Gemini workarounds
	if result[0].Name != "" {
		t.Errorf("openai: tool name should be unchanged, got %q", result[0].Name)
	}
	if result[1].Content != "" {
		t.Errorf("openai: content should be unchanged, got %q", result[1].Content)
	}
}

// --- NormalizeStreamDelta ---

func TestGeminiAdapter_NormalizeStreamDelta_WithExplicitIndex(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	idx := 3
	tc := openai.ToolCall{Index: &idx, ID: "call-1"}

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 3 {
		t.Errorf("explicit index → %d, want 3", got)
	}
}

func TestGeminiAdapter_NormalizeStreamDelta_NewToolCall(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	tc := openai.ToolCall{ID: "call-new"} // no index

	got := adapter.NormalizeStreamDelta(tc, 2)
	if got != -1 {
		t.Errorf("new tool call without index → %d, want -1 (sentinel)", got)
	}
}

func TestGeminiAdapter_NormalizeStreamDelta_Continuation(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	tc := openai.ToolCall{} // no index, no ID

	got := adapter.NormalizeStreamDelta(tc, 5)
	if got != 5 {
		t.Errorf("continuation → %d, want 5 (lastIndex)", got)
	}
}

func TestOpenAIAdapter_NormalizeStreamDelta_AlwaysExplicit(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	idx := 2
	tc := openai.ToolCall{Index: &idx, ID: "call-1"}

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 2 {
		t.Errorf("openai explicit index → %d, want 2", got)
	}
}

func TestOpenAIAdapter_NormalizeStreamDelta_MissingIndex(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	tc := openai.ToolCall{ID: "call-1"} // no index (shouldn't happen with OpenAI)

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 0 {
		t.Errorf("openai missing index → %d, want 0 (safe default)", got)
	}
}
