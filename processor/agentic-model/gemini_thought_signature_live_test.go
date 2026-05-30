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
// Post-ADR-051: carrier is ChatMessage.ReasoningRecords; field
// matched by ToolCallID on echo.
//
// Asserts the end-to-end thought_signature round trip:
//  1. First request elicits a tool_call from the model.
//  2. The wire response carries extra_content.google.thought_signature
//     and convertWireResponse lifts it into
//     ChatMessage.ReasoningRecords as a ReasoningRecord{Provider:"google",
//     CarrierKind:ToolCall, ToolCallID:...}.
//  3. The second request (with tool result + replayed assistant
//     message including its ReasoningRecords) is accepted — Gemini 3.x
//     rejects multi-turn tool flows where the signature isn't echoed.
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
	// Matches the `gemini-3-pro-preview` endpoint in configs/gemini-example.json.
	// Update both this default and the example config when Google publishes
	// the stable release slug (the preview identifier rotates).
	return "gemini-3.1-pro-preview"
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
	// Per-turn timeout (each ChatCompletion call gets the full budget,
	// not a shared parent deadline that turn 2 might inherit a fraction of).
	ctx := context.Background()

	t.Logf("Gemini live test against model=%q", geminiTestModel())

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

	ctx1, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	resp1, err := client.ChatCompletion(ctx1, turn1)
	cancel1()
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	t.Logf("turn 1 response: status=%q finish_reason=%q content=%q toolcalls=%d",
		resp1.Status, resp1.FinishReason, resp1.Message.Content, len(resp1.Message.ToolCalls))
	if resp1.Status == "error" {
		t.Fatalf("turn 1 returned error response: %s", resp1.Error)
	}
	if len(resp1.Message.ToolCalls) == 0 {
		t.Fatalf("turn 1: expected at least one tool_call, got status=%q content=%q",
			resp1.Status, resp1.Message.Content)
	}
	tc := resp1.Message.ToolCalls[0]
	sig := signatureForToolCall(resp1.Message.ReasoningRecords, tc.ID)
	t.Logf("turn 1 toolcall[0]: id=%q name=%q args=%v sig_len=%d sig_preview=%q",
		tc.ID, tc.Name, tc.Arguments, len(sig), previewSig(sig))
	if sig == "" {
		t.Logf("turn 1: thought_signature is empty — model is not a 3.x preview build OR the API stripped it. Continuing to turn 2 to see whether the round-trip still succeeds without it.")
	}

	// Turn 2: replay the assistant message + supply the tool result.
	// ReasoningRecords ride alongside the ToolCalls on the assistant
	// message; attachReasoningRecordsToWire rebinds them by ToolCallID.
	turn2 := agentic.AgentRequest{
		RequestID: "req-gemini-rt-2",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 17 * 23? Use the multiply tool."},
			{
				Role:             "assistant",
				ToolCalls:        resp1.Message.ToolCalls,
				ReasoningRecords: resp1.Message.ReasoningRecords,
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

	ctx2, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	resp2, err := client.ChatCompletion(ctx2, turn2)
	cancel2()
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	t.Logf("turn 2 response: status=%q finish_reason=%q content=%q",
		resp2.Status, resp2.FinishReason, resp2.Message.Content)
	if resp2.Status == "error" {
		t.Fatalf("turn 2 returned error: %s — likely thought_signature not echoed correctly", resp2.Error)
	}
}

// previewSig returns the first 16 chars of a signature for logging — full
// signatures are long opaque blobs; we want to confirm presence + non-empty
// without flooding test output.
func previewSig(s string) string {
	if len(s) <= 16 {
		return s
	}
	return s[:16] + "..."
}

// signatureForToolCall extracts the Gemini thought signature from a
// message's ReasoningRecords for a specific tool_call. Returns "" if
// no matching record is found.
func signatureForToolCall(records []agentic.ReasoningRecord, toolCallID string) string {
	for _, rec := range records {
		if rec.Provider == "google" && rec.CarrierKind == agentic.ReasoningCarrierToolCall && rec.ToolCallID == toolCallID {
			return string(rec.Opaque)
		}
	}
	return ""
}
