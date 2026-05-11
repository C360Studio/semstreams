//go:build live_llm

package agenticmodel_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// ADR-037 chunk 8 live_llm smoke against Gemini 3.x preview.
//
// Asserts the end-to-end thought_signature round trip:
//  1. First request elicits a tool_call from the model.
//  2. The wire response carries extra_content.google.thought_signature
//     and convertWireResponse lifts it into agentic.ToolCall.Metadata.
//  3. The second request (with tool result + replayed assistant tool_call)
//     is accepted — Gemini 3.x rejects multi-turn tool flows where the
//     signature isn't echoed.
//
// Requires:
//   - GEMINI_API_KEY environment variable (sourced from secrets manager).
//   - Model defaults to "gemini-3.0-pro-preview" but can be overridden
//     with GEMINI_TEST_MODEL.
//
// Run: go test -tags live_llm -run TestGemini3x_ThoughtSignature_RoundTrip \
//              ./processor/agentic-model/...

func requireGeminiAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set; skipping Gemini 3.x live_llm test")
	}
	return key
}

func geminiTestModel() string {
	if m := os.Getenv("GEMINI_TEST_MODEL"); m != "" {
		return m
	}
	return "gemini-3.0-pro-preview"
}

func newGeminiLiveClient(t *testing.T) *agenticmodel.Client {
	t.Helper()
	requireGeminiAPIKey(t)
	t.Setenv("GEMINI_API_KEY", os.Getenv("GEMINI_API_KEY"))
	client, err := agenticmodel.NewClient(&model.EndpointConfig{
		Provider:    "gemini",
		URL:         "https://generativelanguage.googleapis.com/v1beta/openai",
		Model:       geminiTestModel(),
		APIKeyEnv:   "GEMINI_API_KEY",
		WireBackend: "wire",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SetAdapter(agenticmodel.AdapterFor("gemini"))
	return client
}

// TestGemini3x_ThoughtSignature_RoundTrip exercises the multi-turn
// tool flow with signature echo. The model is asked to call a tool;
// we replay the tool result + assistant tool_call (with signature)
// and expect the second call to succeed.
func TestGemini3x_ThoughtSignature_RoundTrip(t *testing.T) {
	client := newGeminiLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Turn 1: ask the model to use a tool.
	turn1 := agentic.AgentRequest{
		RequestID: "req-gemini-rt-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 17 * 23? Use the multiply tool."},
		},
		Tools: []agentic.ToolDefinition{
			{
				Name:        "multiply",
				Description: "Multiplies two integers and returns the product.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"a": map[string]any{"type": "integer"},
						"b": map[string]any{"type": "integer"},
					},
					"required": []string{"a", "b"},
				},
			},
		},
	}

	resp1, err := client.ChatCompletion(ctx, turn1)
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if resp1.Status == "error" {
		t.Fatalf("turn 1 returned error response: %s", resp1.Error)
	}
	if len(resp1.Message.ToolCalls) == 0 {
		t.Fatalf("turn 1: expected at least one tool_call, got status=%q content=%q",
			resp1.Status, resp1.Message.Content)
	}
	tc := resp1.Message.ToolCalls[0]
	sig, _ := tc.Metadata[agentic.MetadataKeyGoogleThoughtSignature].(string)
	if sig == "" {
		t.Errorf("turn 1: expected non-empty thought_signature in metadata, got empty (model may not be 3.x preview)")
	}

	// Turn 2: replay the assistant tool_call + supply the tool result.
	turn2 := agentic.AgentRequest{
		RequestID: "req-gemini-rt-2",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 17 * 23? Use the multiply tool."},
			{
				Role:      "assistant",
				ToolCalls: resp1.Message.ToolCalls, // carries signature in Metadata
			},
			{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    `{"product": 391}`,
			},
		},
		Tools: turn1.Tools,
	}

	resp2, err := client.ChatCompletion(ctx, turn2)
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if resp2.Status == "error" {
		t.Fatalf("turn 2 returned error: %s — likely thought_signature not echoed correctly", resp2.Error)
	}
}
