package agenticmodel_test

import (
	"testing"

	"github.com/c360studio/semstreams/model/wire"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// contentOf returns the message's string content, or "" if not a string.
func contentOf(m wire.Message) string {
	s, _ := m.ContentString()
	return s
}

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
	messages := []wire.Message{
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
	messages := []wire.Message{
		{
			Role:      "assistant",
			ToolCalls: []wire.ToolCall{{ID: "call-1"}},
		},
		mustAssistantToolCall("existing", "call-2"),
		{Role: "assistant"}, // No tool calls — should NOT get space
	}

	result := adapter.NormalizeMessages(messages)

	if contentOf(result[0]) != " " {
		t.Errorf("empty content with tool_calls → %q, want space", contentOf(result[0]))
	}
	if contentOf(result[1]) != "existing" {
		t.Errorf("existing content → %q, want existing", contentOf(result[1]))
	}
	if s, ok := result[2].ContentString(); ok && s != "" {
		t.Errorf("no tool_calls, empty content → %q, want empty", s)
	}
}

func TestGenericAdapter_NormalizeMessages_SameAsGemini(t *testing.T) {
	// Generic adapter applies the same safe normalizations
	adapter := agenticmodel.AdapterFor("")
	messages := []wire.Message{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "assistant", ToolCalls: []wire.ToolCall{{ID: "call-1"}}},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Name != "unknown_tool" {
		t.Errorf("generic: empty tool name → %q, want unknown_tool", result[0].Name)
	}
	if contentOf(result[1]) != " " {
		t.Errorf("generic: empty assistant content with tool_calls → %q, want space", contentOf(result[1]))
	}
}

func TestGenericAdapter_NormalizeMessages_CollapsesConsecutiveSameRole(t *testing.T) {
	adapter := agenticmodel.AdapterFor("")
	messages := mustWireMessages([]roleContent{
		{Role: "system", Content: "rule one"},
		{Role: "system", Content: "rule two"},
		{Role: "user", Content: "hi"},
		{Role: "user", Content: "are you there?"},
		{Role: "assistant", Content: "yes"},
	})

	result := adapter.NormalizeMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages after collapse, got %d", len(result))
	}
	if result[0].Role != "system" || contentOf(result[0]) != "rule one\n\nrule two" {
		t.Errorf("merged system → role=%q content=%q", result[0].Role, contentOf(result[0]))
	}
	if result[1].Role != "user" || contentOf(result[1]) != "hi\n\nare you there?" {
		t.Errorf("merged user → role=%q content=%q", result[1].Role, contentOf(result[1]))
	}
	if result[2].Role != "assistant" || contentOf(result[2]) != "yes" {
		t.Errorf("standalone assistant → role=%q content=%q", result[2].Role, contentOf(result[2]))
	}
}

func TestGenericAdapter_NormalizeMessages_PreservesToolPairs(t *testing.T) {
	adapter := agenticmodel.AdapterFor("")
	messages := []wire.Message{
		{Role: "assistant", ToolCalls: []wire.ToolCall{{ID: "call-1"}}},
		{Role: "assistant", ToolCalls: []wire.ToolCall{{ID: "call-2"}}},
		mustToolResult("call-1", "read_file", "result one"),
		mustToolResult("call-2", "read_file", "result two"),
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
	messages := mustWireMessages([]roleContent{
		{Role: "user", Content: ""},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "assistant", Content: ""},
	})

	result := adapter.NormalizeMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if contentOf(result[0]) != "hello" {
		t.Errorf("empty + nonempty user → %q, want %q", contentOf(result[0]), "hello")
	}
	if contentOf(result[1]) != "hi" {
		t.Errorf("nonempty + empty assistant → %q, want %q", contentOf(result[1]), "hi")
	}
}

func TestOpenAIAdapter_NormalizeMessages_NoChanges(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	messages := []wire.Message{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "assistant", ToolCalls: []wire.ToolCall{{ID: "call-1"}}},
	}

	result := adapter.NormalizeMessages(messages)

	// OpenAI adapter does NOT apply Gemini workarounds
	if result[0].Name != "" {
		t.Errorf("openai: tool name should be unchanged, got %q", result[0].Name)
	}
	if s, ok := result[1].ContentString(); ok && s != "" {
		t.Errorf("openai: content should be unchanged, got %q", s)
	}
}

// --- NormalizeStreamDelta ---

func TestGeminiAdapter_NormalizeStreamDelta_WithExplicitIndex(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	idx := 3
	tc := wire.ToolCall{Index: &idx, ID: "call-1"}

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 3 {
		t.Errorf("explicit index → %d, want 3", got)
	}
}

func TestGeminiAdapter_NormalizeStreamDelta_NewToolCall(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	tc := wire.ToolCall{ID: "call-new"} // no index

	got := adapter.NormalizeStreamDelta(tc, 2)
	if got != -1 {
		t.Errorf("new tool call without index → %d, want -1 (sentinel)", got)
	}
}

func TestGeminiAdapter_NormalizeStreamDelta_Continuation(t *testing.T) {
	adapter := agenticmodel.AdapterFor("gemini")
	tc := wire.ToolCall{} // no index, no ID

	got := adapter.NormalizeStreamDelta(tc, 5)
	if got != 5 {
		t.Errorf("continuation → %d, want 5 (lastIndex)", got)
	}
}

func TestOpenAIAdapter_NormalizeStreamDelta_AlwaysExplicit(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	idx := 2
	tc := wire.ToolCall{Index: &idx, ID: "call-1"}

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 2 {
		t.Errorf("openai explicit index → %d, want 2", got)
	}
}

func TestOpenAIAdapter_NormalizeStreamDelta_MissingIndex(t *testing.T) {
	adapter := agenticmodel.AdapterFor("openai")
	tc := wire.ToolCall{ID: "call-1"} // no index (shouldn't happen with OpenAI)

	got := adapter.NormalizeStreamDelta(tc, 0)
	if got != 0 {
		t.Errorf("openai missing index → %d, want 0 (safe default)", got)
	}
}

// mustAssistantToolCall is a fixture helper for assistant messages with both
// content and tool_calls.
func mustAssistantToolCall(content, toolCallID string) wire.Message {
	m := wire.Message{
		Role:      "assistant",
		ToolCalls: []wire.ToolCall{{ID: toolCallID}},
	}
	if content != "" {
		if err := m.SetContentString(content); err != nil {
			panic(err)
		}
	}
	return m
}

// mustToolResult is a fixture helper for tool-result messages.
func mustToolResult(toolCallID, name, content string) wire.Message {
	m := wire.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Name:       name,
	}
	if content != "" {
		if err := m.SetContentString(content); err != nil {
			panic(err)
		}
	}
	return m
}
