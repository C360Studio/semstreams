package agenticmodel_test

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// TestBuildChatRequest_ToolStrict_True_PropagatesToWire verifies that
// ToolDefinition.Strict=true surfaces as tools[].function.strict=true on
// the outgoing OpenAI wire format. This is the symmetric counterpart of
// TestBuildChatRequest_ResponseFormatJSONSchema_WireShape — same SDK
// version (go-openai v1.41+), different field. See ADR-035.
func TestBuildChatRequest_ToolStrict_True_PropagatesToWire(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "tool-strict-true",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "do it"}},
		Model:     "test",
		Tools: []agentic.ToolDefinition{{
			Name:        "submit_work",
			Description: "submit final answer",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"answer": map[string]any{"type": "string"}},
				"required":             []any{"answer"},
				"additionalProperties": false,
			},
			Strict: true,
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	toolsRaw, ok := getCaptured()["tools"]
	if !ok {
		t.Fatal("tools missing from captured request")
	}
	tools, ok := toolsRaw.([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %T len=%d", toolsRaw, len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if got := fn["strict"]; got != true {
		t.Errorf("tools[0].function.strict = %v (%T), want true", got, got)
	}
	if got := fn["name"]; got != "submit_work" {
		t.Errorf("tools[0].function.name = %v, want \"submit_work\"", got)
	}
}

// TestBuildChatRequest_ToolStrict_False_OmitsFieldOnWire verifies the
// omitempty contract — when Strict is unset (zero value), the field is
// dropped from the wire entirely. Existing callers that don't set Strict
// see no behavior change; their tools serialize identically to before.
func TestBuildChatRequest_ToolStrict_False_OmitsFieldOnWire(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "tool-strict-default",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "do it"}},
		Model:     "test",
		Tools: []agentic.ToolDefinition{{
			Name:        "noop",
			Description: "no-op",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			// Strict not set → false → omitempty drops it
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	tools := getCaptured()["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if _, present := fn["strict"]; present {
		t.Errorf("tools[0].function.strict should be omitted when false, got %v", fn["strict"])
	}
}

// TestBuildChatRequest_ToolStrict_MixedTools_IndependentPerTool verifies
// that Strict is per-ToolDefinition, not a request-wide flag. Mixed
// definitions surface independently on the wire so callers can opt only
// the tool whose schema satisfies the strict-mode subset.
func TestBuildChatRequest_ToolStrict_MixedTools_IndependentPerTool(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "tool-strict-mixed",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "do it"}},
		Model:     "test",
		Tools: []agentic.ToolDefinition{
			{
				Name:        "strict_tool",
				Description: "schema satisfies strict subset",
				Parameters:  map[string]any{"type": "object", "additionalProperties": false},
				Strict:      true,
			},
			{
				Name:        "loose_tool",
				Description: "permissive schema",
				Parameters:  map[string]any{"type": "object"},
				// Strict false
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	tools := getCaptured()["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	fn0 := tools[0].(map[string]any)["function"].(map[string]any)
	fn1 := tools[1].(map[string]any)["function"].(map[string]any)

	if got := fn0["strict"]; got != true {
		t.Errorf("tools[0] (strict_tool) strict = %v, want true", got)
	}
	if _, present := fn1["strict"]; present {
		t.Errorf("tools[1] (loose_tool) strict should be omitted, got %v", fn1["strict"])
	}
}
