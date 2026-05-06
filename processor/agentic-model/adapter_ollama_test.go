package agenticmodel_test

import (
	"testing"

	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	openai "github.com/sashabaranov/go-openai"
)

func TestAdapterFor_Ollama_ReturnsOllamaAdapter(t *testing.T) {
	a := agenticmodel.AdapterFor("ollama")
	if a.Name() != "ollama" {
		t.Errorf("AdapterFor(\"ollama\").Name() = %q, want \"ollama\"", a.Name())
	}
	if _, ok := a.(*agenticmodel.OllamaAdapter); !ok {
		t.Errorf("AdapterFor(\"ollama\") = %T, want *agenticmodel.OllamaAdapter", a)
	}
}

func TestOllamaAdapter_InheritsToolNameFallback(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	messages := []openai.ChatCompletionMessage{
		{Role: "tool", ToolCallID: "call-1", Name: ""},
		{Role: "tool", ToolCallID: "call-2", Name: "read_file"},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Name != "unknown_tool" {
		t.Errorf("empty tool name should be filled with 'unknown_tool'; got %q", result[0].Name)
	}
	if result[1].Name != "read_file" {
		t.Errorf("non-empty tool name should be preserved; got %q", result[1].Name)
	}
}

func TestOllamaAdapter_InheritsAssistantContentFallback(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	messages := []openai.ChatCompletionMessage{
		{
			Role: "assistant",
			ToolCalls: []openai.ToolCall{
				{ID: "call-1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "f", Arguments: "{}"}},
			},
		},
	}

	result := adapter.NormalizeMessages(messages)

	if result[0].Content != " " {
		t.Errorf("assistant message with tool_calls should have single-space content fallback; got %q", result[0].Content)
	}
}

func TestOllamaAdapter_InheritsSameRoleCollapse(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	messages := []openai.ChatCompletionMessage{
		{Role: "user", Content: "first"},
		{Role: "user", Content: "second"},
	}

	result := adapter.NormalizeMessages(messages)

	if len(result) != 1 {
		t.Fatalf("consecutive same-role messages should collapse to 1; got %d", len(result))
	}
	if result[0].Content != "first\n\nsecond" {
		t.Errorf("collapsed content = %q, want %q", result[0].Content, "first\n\nsecond")
	}
}

func TestOllamaAdapter_NormalizeRequest_NoOp(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	// chunk 3a: NormalizeRequest is a no-op for response_format. The /v1
	// path receives the field unchanged from chunk 2's plumbing. chunk 3b
	// (deferred) would translate to Ollama's native /api/chat format field.
	req := openai.ChatCompletionRequest{
		Model: "qwen3:30b",
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "test",
				Strict: true,
			},
		},
	}

	adapter.NormalizeRequest(&req)

	if req.ResponseFormat == nil {
		t.Error("OllamaAdapter chunk-3a should not clear response_format; got nil")
	}
	if req.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONSchema {
		t.Errorf("response_format.type = %q, want unchanged", req.ResponseFormat.Type)
	}
	if req.ResponseFormat.JSONSchema == nil || req.ResponseFormat.JSONSchema.Name != "test" {
		t.Error("response_format.json_schema should be unchanged")
	}
}
