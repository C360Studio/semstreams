package agenticmodel_test

import (
	"testing"

	"github.com/c360studio/semstreams/model/wire"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
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
	messages := []wire.Message{
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
	messages := []wire.Message{
		{
			Role: "assistant",
			ToolCalls: []wire.ToolCall{
				{ID: "call-1", Type: "function", Function: wire.Function{Name: "f", Arguments: "{}"}},
			},
		},
	}

	result := adapter.NormalizeMessages(messages)

	s, ok := result[0].ContentString()
	if !ok || s != " " {
		t.Errorf("assistant message with tool_calls should have single-space content fallback; got %q (ok=%v)", s, ok)
	}
}

func TestOllamaAdapter_InheritsSameRoleCollapse(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	messages := mustWireMessages([]roleContent{
		{Role: "user", Content: "first"},
		{Role: "user", Content: "second"},
	})

	result := adapter.NormalizeMessages(messages)

	if len(result) != 1 {
		t.Fatalf("consecutive same-role messages should collapse to 1; got %d", len(result))
	}
	s, _ := result[0].ContentString()
	if s != "first\n\nsecond" {
		t.Errorf("collapsed content = %q, want %q", s, "first\n\nsecond")
	}
}

func TestOllamaAdapter_NormalizeRequest_NoOp(t *testing.T) {
	adapter := agenticmodel.AdapterFor("ollama")
	// chunk 3a: NormalizeRequest is a no-op for response_format. The /v1
	// path receives the field unchanged from chunk 2's plumbing. chunk 3b
	// (deferred) would translate to Ollama's native /api/chat format field.
	req := wire.ChatCompletionRequest{
		Model: "qwen3:30b",
		ResponseFormat: &wire.ResponseFormat{
			Type: "json_schema",
			JSONSchema: &wire.JSONSchema{
				Name:   "test",
				Strict: true,
			},
		},
	}

	adapter.NormalizeRequest(&req)

	if req.ResponseFormat == nil {
		t.Error("OllamaAdapter chunk-3a should not clear response_format; got nil")
	}
	if req.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type = %q, want unchanged", req.ResponseFormat.Type)
	}
	if req.ResponseFormat.JSONSchema == nil || req.ResponseFormat.JSONSchema.Name != "test" {
		t.Error("response_format.json_schema should be unchanged")
	}
}

// roleContent is a compact builder for wire.Message test fixtures that only
// need a role and a plain string content.
type roleContent struct {
	Role    string
	Content string
}

func mustWireMessages(in []roleContent) []wire.Message {
	out := make([]wire.Message, len(in))
	for i, rc := range in {
		out[i].Role = rc.Role
		if rc.Content != "" {
			if err := out[i].SetContentString(rc.Content); err != nil {
				panic(err)
			}
		}
	}
	return out
}
