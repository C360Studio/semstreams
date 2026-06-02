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
	// HARD-FAIL on empty signature for Gemini 3.x preview models —
	// the whole point of this test is to gate the capture contract.
	// Soft-warn here was the gap that let #194 ship undetected
	// after #188 fixed the echo path. See
	// [[feedback_live_gate_catches_doc_derived]] (sharpened 2026-06-02).
	//
	// Escape hatch: GEMINI_ALLOW_EMPTY_SIG=1 turns the assertion
	// back into a warn for operators investigating regressions
	// against non-3.x test models or known-stripped endpoints.
	if sig == "" && os.Getenv("GEMINI_ALLOW_EMPTY_SIG") == "" {
		t.Fatalf("turn 1: thought_signature is empty — capture path is broken on this model/endpoint. "+
			"Set GEMINI_ALLOW_EMPTY_SIG=1 to downgrade to warn for non-3.x test runs. "+
			"model=%q wire endpoint=%q gh#194 regression gate", geminiTestModel(), "openai-compat")
	}
	if sig == "" {
		t.Logf("turn 1: thought_signature empty; GEMINI_ALLOW_EMPTY_SIG override active — turn 2 will likely fail with HTTP 400")
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

// TestGemini3x_ThoughtSignature_ParallelToolCalls_RoundTrip
// exercises the multi-tool-call response shape that triggered
// #194 in production. The test was a deliberate gap in the
// single-tool TestGemini3x_ThoughtSignature_RoundTrip — Gemini's
// validator only rejects the multi-tool path when position 2+
// lacks a signature, so the single-tool test couldn't catch the
// capture-side strip.
//
// Asks the model to call two tools in parallel (calculator-shaped
// because Gemini's persona naturally emits parallel calls for
// independent arithmetic problems). Asserts:
//
//  1. Turn 1 returns 2+ ToolCalls.
//  2. EVERY captured ToolCall has a non-empty thought_signature
//     in ReasoningRecords. This is the contract #188's echo fix
//     can only honor if capture surfaced the signatures in the
//     first place.
//  3. Turn 2 (echo + tool results) is accepted. Gemini 3.x rejects
//     with HTTP 400 if position 2+ is missing its signature; this
//     turn implicitly tests the round-trip the validator gates.
//
// Per [[feedback_live_gate_catches_doc_derived]] (sharpened
// 2026-06-02), each leg of a round-trip wire fix needs a HARD-FAIL
// contract assertion under production-realistic conditions.
func TestGemini3x_ThoughtSignature_ParallelToolCalls_RoundTrip(t *testing.T) {
	client := newGeminiLiveClient(t)
	ctx := context.Background()
	t.Logf("Gemini live multi-tool test against model=%q", geminiTestModel())

	tools := []agentic.ToolDefinition{
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
		{
			Name:        "add",
			Description: "Adds two integers and returns the sum.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "integer"},
					"b": map[string]any{"type": "integer"},
				},
				"required": []string{"a", "b"},
			},
		},
	}

	turn1 := agentic.AgentRequest{
		RequestID: "req-gemini-parallel-1",
		Messages: []agentic.ChatMessage{
			// Prompt designed to elicit two INDEPENDENT tool calls
			// in a single response (no causal dependency between
			// the two operations).
			{Role: "user", Content: "Compute 17 * 23 and ALSO 41 + 19. Make BOTH tool calls in your first response — don't wait for either result before issuing the other."},
		},
		Tools: tools,
	}

	ctx1, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	resp1, err := client.ChatCompletion(ctx1, turn1)
	cancel1()
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if resp1.Status == "error" {
		t.Fatalf("turn 1 returned error: %s", resp1.Error)
	}
	t.Logf("turn 1: tool_calls=%d records=%d", len(resp1.Message.ToolCalls), len(resp1.Message.ReasoningRecords))

	// If the model only emitted one tool call (model behavior we
	// can't fully control), SKIP — we want this test to fire
	// specifically on the multi-tool path, not pass by accident
	// on a single-tool response.
	if len(resp1.Message.ToolCalls) < 2 {
		t.Skipf("model emitted %d tool_calls; need 2+ to exercise the #194 multi-tool capture contract — try GEMINI_TEST_MODEL=gemini-3-pro-preview or re-run", len(resp1.Message.ToolCalls))
	}

	// Capture contract per Google's docs (verified empirically
	// 2026-06-02 on gemini-3-flash-preview): the first
	// function-call part in each step carries thought_signature;
	// subsequent parallel calls in the same step don't need to.
	// So the hard-fail line is:
	//
	//   - position 0 MUST have a non-empty signature (otherwise
	//     capture is broken — this is the gh#194 production state
	//     where semteams hit zero captured sigs)
	//   - positions 1+ MAY have signatures (allowed but not required)
	//   - turn 2 MUST succeed (the actual round-trip contract)
	//
	// GEMINI_ALLOW_EMPTY_SIG=1 escape hatch downgrades the
	// position-0 assertion for operators investigating non-3.x
	// models or known-stripped endpoints.
	allowEmpty := os.Getenv("GEMINI_ALLOW_EMPTY_SIG") != ""
	for i, tc := range resp1.Message.ToolCalls {
		sig := signatureForToolCall(resp1.Message.ReasoningRecords, tc.ID)
		t.Logf("turn 1 toolcall[%d]: id=%q name=%q sig_len=%d sig_preview=%q",
			i, tc.ID, tc.Name, len(sig), previewSig(sig))
	}
	firstSig := signatureForToolCall(resp1.Message.ReasoningRecords, resp1.Message.ToolCalls[0].ID)
	if firstSig == "" && !allowEmpty {
		t.Fatalf("turn 1: position-0 tool_call %q missing thought_signature — capture path is broken on this model/endpoint. "+
			"Per Google's 'first call per step' docs (and empirical verification), Gemini 3.x preview MUST emit a sig on the first call; "+
			"missing here = gh#194 reproduction state. Set GEMINI_ALLOW_EMPTY_SIG=1 to downgrade.",
			resp1.Message.ToolCalls[0].ID)
	}

	// Turn 2: replay the assistant message + supply BOTH tool
	// results. Gemini 3.x's validator will HTTP 400 if any
	// echoed tool_call lacks its signature — that's the
	// downstream contract this test gates.
	toolResults := make([]agentic.ChatMessage, 0, len(resp1.Message.ToolCalls))
	for _, tc := range resp1.Message.ToolCalls {
		toolResults = append(toolResults, agentic.ChatMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    `{"result": 0}`, // value doesn't matter for the wire contract
		})
	}
	msgs := []agentic.ChatMessage{
		turn1.Messages[0],
		{
			Role:             "assistant",
			ToolCalls:        resp1.Message.ToolCalls,
			ReasoningRecords: resp1.Message.ReasoningRecords,
		},
	}
	msgs = append(msgs, toolResults...)
	turn2 := agentic.AgentRequest{
		RequestID: "req-gemini-parallel-2",
		Messages:  msgs,
		Tools:     tools,
	}

	ctx2, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	resp2, err := client.ChatCompletion(ctx2, turn2)
	cancel2()
	if err != nil {
		t.Fatalf("turn 2 failed: %v — likely a thought_signature echo regression. gh#194 + gh#188", err)
	}
	if resp2.Status == "error" {
		t.Fatalf("turn 2 returned error: %s — likely a thought_signature echo regression. gh#194 + gh#188", resp2.Error)
	}
	t.Logf("turn 2: status=%q finish_reason=%q — multi-tool round-trip OK", resp2.Status, resp2.FinishReason)
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
