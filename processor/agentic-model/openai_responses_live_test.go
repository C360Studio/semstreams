//go:build live_llm

package agenticmodel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// ADR-051 live_llm parity test against OpenAI's /v1/responses
// endpoint. Doubles as the fixture-capture mechanism for the
// model/wire/responses/testdata/ golden round-trip tests.
//
// Requires:
//   - OPENAI_API_KEY environment variable
//   - Model defaults to "gpt-5.5"; override with OPENAI_RESPONSES_MODEL
//
// Run: go test -tags live_llm -run TestOpenAIResponses \
//              ./processor/agentic-model/...
//
// Set CAPTURE_FIXTURES=1 to write request/response bodies to
// model/wire/responses/testdata/ (overwrites existing fixtures).

const openAIResponsesDefaultModel = "gpt-5.5"

// openAIResponsesReasoningModel picks the model used for the
// reasoning-explicit test. Defaults to gpt-5.5 (which supports
// reasoning_effort when wired through /v1/responses); override with
// OPENAI_RESPONSES_REASONING_MODEL for o-series / Codex / GPT-5.5-mini.
func openAIResponsesReasoningModel() string {
	if m := os.Getenv("OPENAI_RESPONSES_REASONING_MODEL"); m != "" {
		return m
	}
	return "gpt-5.5"
}

func requireOpenAIAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set; skipping OpenAI Responses live_llm test")
	}
	return key
}

func openAIResponsesTestModel() string {
	if m := os.Getenv("OPENAI_RESPONSES_MODEL"); m != "" {
		return m
	}
	return openAIResponsesDefaultModel
}

func newOpenAIResponsesLiveClient(t *testing.T) *agenticmodel.Client {
	t.Helper()
	requireOpenAIAPIKey(t)
	client, err := agenticmodel.NewClient(&model.EndpointConfig{
		Provider:    "openai",
		URL:         "https://api.openai.com/v1",
		Model:       openAIResponsesTestModel(),
		APIKeyEnv:   "OPENAI_API_KEY",
		WireBackend: "responses",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SetResponsesAdapter(agenticmodel.ResponsesAdapterFor("openai"))
	return client
}

// TestOpenAIResponses_SingleTurn pins a simple end-to-end call:
// user prompt → assistant text response. Smoke test for dispatch
// + translation + adapter wiring.
func TestOpenAIResponses_SingleTurn(t *testing.T) {
	client := newOpenAIResponsesLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := client.ChatCompletion(ctx, agentic.AgentRequest{
		RequestID: "req-resp-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "Reply with only the word 'pong'."},
		},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Status == "error" {
		t.Fatalf("status=error: %s", resp.Error)
	}
	t.Logf("single-turn response: status=%q content=%q", resp.Status, resp.Message.Content)
	if !strings.Contains(strings.ToLower(resp.Message.Content), "pong") {
		t.Errorf("expected 'pong' in response, got %q", resp.Message.Content)
	}
}

// TestOpenAIResponses_ToolFlow_WithReasoningEcho exercises the full
// multi-turn tool flow: turn 1 elicits a function_call + reasoning
// echo; turn 2 replays the assistant message (with ReasoningRecords)
// plus the tool result and expects a coherent terminal response.
//
// This is the parity gate for the carrier abstraction: if echo is
// broken (records not flowing through ChatMessage.ReasoningRecords,
// adapter not rebuilding the wire shape), turn 2 fails or the model
// re-thinks instead of continuing.
func TestOpenAIResponses_ToolFlow_WithReasoningEcho(t *testing.T) {
	client := newOpenAIResponsesLiveClient(t)
	ctx := context.Background()

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
	}

	turn1 := agentic.AgentRequest{
		RequestID: "req-resp-tool-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 17 * 23? Use the multiply tool."},
		},
		Tools: tools,
	}
	ctx1, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	resp1, err := client.ChatCompletion(ctx1, turn1)
	cancel1()
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if resp1.Status == "error" {
		t.Fatalf("turn 1 error: %s", resp1.Error)
	}
	t.Logf("turn 1: status=%q toolcalls=%d reasoning_records=%d",
		resp1.Status, len(resp1.Message.ToolCalls), len(resp1.Message.ReasoningRecords))
	if len(resp1.Message.ToolCalls) == 0 {
		t.Fatalf("turn 1: expected tool_call, got status=%q content=%q",
			resp1.Status, resp1.Message.Content)
	}
	tc := resp1.Message.ToolCalls[0]
	t.Logf("turn 1 tool_call: id=%q name=%q args=%v", tc.ID, tc.Name, tc.Arguments)
	t.Logf("turn 1 reasoning_records: %d items", len(resp1.Message.ReasoningRecords))
	for i, rec := range resp1.Message.ReasoningRecords {
		t.Logf("  record[%d]: provider=%q carrier=%q item_id=%q opaque_len=%d",
			i, rec.Provider, rec.CarrierKind, rec.ItemID, len(rec.Opaque))
	}

	turn2 := agentic.AgentRequest{
		RequestID: "req-resp-tool-2",
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
				Content:    `{"product":391}`,
			},
		},
		Tools: tools,
	}
	ctx2, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	resp2, err := client.ChatCompletion(ctx2, turn2)
	cancel2()
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if resp2.Status == "error" {
		t.Fatalf("turn 2 error: %s — likely reasoning_records not echoed correctly", resp2.Error)
	}
	t.Logf("turn 2: status=%q content=%q", resp2.Status, resp2.Message.Content)
	if !strings.Contains(resp2.Message.Content, "391") {
		t.Errorf("expected 391 in turn 2 response, got %q", resp2.Message.Content)
	}
}

// TestOpenAIResponses_ReasoningEcho is the ADR-051 forcing-function
// gate: exercises tool_choice + reasoning_effort combined (the combo
// ChatCompletion forbids on GPT-5.5 / o-series, which is the entire
// reason this work exists). Asserts:
//
//  1. Turn 1 elicits BOTH a tool_call AND at least one
//     ReasoningRecord{Provider:"openai", CarrierKind:StandaloneItem}
//     with non-empty Opaque bytes (the encrypted_content blob).
//  2. Turn 2 replays the assistant message WITH ReasoningRecords +
//     supplies the tool result, and the API accepts it. If the echo
//     is broken (records not flowing through ChatMessage.ReasoningRecords,
//     adapter not rebuilding the reasoning InputItem shape, or the
//     translator not interleaving them with messages correctly),
//     turn 2 either fails outright or the model re-thinks instead
//     of continuing — both are failure modes this test catches.
//
// Override model with OPENAI_RESPONSES_REASONING_MODEL when the
// default doesn't emit reasoning items in your account.
func TestOpenAIResponses_ReasoningEcho(t *testing.T) {
	requireOpenAIAPIKey(t)

	chosenModel := openAIResponsesReasoningModel()
	client, err := agenticmodel.NewClient(&model.EndpointConfig{
		Provider:        "openai",
		URL:             "https://api.openai.com/v1",
		Model:           chosenModel,
		APIKeyEnv:       "OPENAI_API_KEY",
		WireBackend:     "responses",
		ReasoningEffort: "medium", // forcing-function combo with tool_choice
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SetResponsesAdapter(agenticmodel.ResponsesAdapterFor("openai"))

	ctx := context.Background()
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
	}

	t.Logf("Reasoning-explicit test against model=%q with reasoning.effort=medium", chosenModel)

	turn1 := agentic.AgentRequest{
		RequestID: "req-resp-reasoning-1",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 47 * 89? Think carefully, then use the multiply tool."},
		},
		Tools: tools,
	}
	ctx1, cancel1 := context.WithTimeout(ctx, 90*time.Second)
	resp1, err := client.ChatCompletion(ctx1, turn1)
	cancel1()
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if resp1.Status == "error" {
		t.Fatalf("turn 1 error: %s", resp1.Error)
	}
	t.Logf("turn 1: status=%q toolcalls=%d reasoning_records=%d",
		resp1.Status, len(resp1.Message.ToolCalls), len(resp1.Message.ReasoningRecords))

	if len(resp1.Message.ToolCalls) == 0 {
		t.Fatalf("turn 1: expected at least one tool_call, got status=%q content=%q",
			resp1.Status, resp1.Message.Content)
	}
	if len(resp1.Message.ReasoningRecords) == 0 {
		t.Fatalf("turn 1: expected at least one ReasoningRecord (model=%s, reasoning_effort=medium); "+
			"the API didn't emit reasoning items. Try a Codex-class or o-series model via "+
			"OPENAI_RESPONSES_REASONING_MODEL.", chosenModel)
	}

	// Inspect the first ReasoningRecord: provider, carrier kind,
	// non-empty Opaque, non-empty ItemID.
	rec := resp1.Message.ReasoningRecords[0]
	t.Logf("turn 1 reasoning_record[0]: provider=%q carrier=%q item_id=%q opaque_len=%d summary=%q",
		rec.Provider, rec.CarrierKind, rec.ItemID, len(rec.Opaque), rec.SummaryText)
	if rec.Provider != "openai" {
		t.Errorf("ReasoningRecord[0].Provider = %q, want openai", rec.Provider)
	}
	if string(rec.CarrierKind) != "standalone_item" {
		t.Errorf("ReasoningRecord[0].CarrierKind = %q, want standalone_item", rec.CarrierKind)
	}
	if rec.ItemID == "" {
		t.Error("ReasoningRecord[0].ItemID empty (capture path broken?)")
	}
	if len(rec.Opaque) == 0 {
		t.Error("ReasoningRecord[0].Opaque empty (encrypted_content not captured?)")
	}

	tc := resp1.Message.ToolCalls[0]
	t.Logf("turn 1 tool_call: id=%q name=%q args=%v", tc.ID, tc.Name, tc.Arguments)

	// Turn 2: replay the assistant message INCLUDING ReasoningRecords,
	// supply the tool result. If the echo path is correctly threading
	// the encrypted_content blob back as an input reasoning item,
	// OpenAI accepts the request and continues; if not, it errors or
	// the model re-thinks visibly.
	turn2 := agentic.AgentRequest{
		RequestID: "req-resp-reasoning-2",
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "What is 47 * 89? Think carefully, then use the multiply tool."},
			{
				Role:             "assistant",
				ToolCalls:        resp1.Message.ToolCalls,
				ReasoningRecords: resp1.Message.ReasoningRecords,
			},
			{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    `{"product":4183}`,
			},
		},
		Tools: tools,
	}
	ctx2, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	resp2, err := client.ChatCompletion(ctx2, turn2)
	cancel2()
	if err != nil {
		t.Fatalf("turn 2 (reasoning echo): %v", err)
	}
	if resp2.Status == "error" {
		t.Fatalf("turn 2 reasoning echo error: %s — likely ReasoningRecord echo path broken "+
			"(records didn't reconstruct on the wire correctly)", resp2.Error)
	}
	t.Logf("turn 2 (reasoning echo): status=%q content=%q", resp2.Status, resp2.Message.Content)
	if !strings.Contains(resp2.Message.Content, "4183") {
		t.Errorf("expected 4183 in turn 2 response, got %q", resp2.Message.Content)
	}
}

// TestOpenAIResponses_CaptureFixtures harvests live request/response
// bodies into model/wire/responses/testdata/ for the unit-level
// round-trip parity tests. Skips by default; enable with
// CAPTURE_FIXTURES=1. Overwrites existing files unconditionally
// when enabled — caller decides when to re-capture.
func TestOpenAIResponses_CaptureFixtures(t *testing.T) {
	if os.Getenv("CAPTURE_FIXTURES") != "1" {
		t.Skip("CAPTURE_FIXTURES != 1; skipping fixture capture")
	}
	requireOpenAIAPIKey(t)

	testdataDir, err := filepath.Abs(filepath.Join("..", "..", "model", "wire", "responses", "testdata"))
	if err != nil {
		t.Fatalf("resolve testdata dir: %v", err)
	}
	if _, err := os.Stat(testdataDir); err != nil {
		t.Fatalf("testdata dir not found: %v", err)
	}

	// We can't easily intercept the wire body from inside the
	// agentic-model client; instead, we drive the responses.Client
	// directly here and serialize the request/response pair.
	// This is the same shape the agentic-model client constructs;
	// the captured fixtures back the round-trip tests in
	// model/wire/responses/.

	// NOTE: the implementation of this capture is intentionally a
	// future seam. Set CAPTURE_FIXTURES=1 to run, but the test will
	// emit a TODO log line until the seam is filled. The TODO does
	// not fail the test — it's a known gap tracked in the PR.
	t.Logf("TODO: fixture capture implementation pending — see ADR-051 pre-tag gate")
	_ = testdataDir
}

// outputJSONToFile is a helper for fixture capture (kept for the
// scaffold even if TestOpenAIResponses_CaptureFixtures defers the
// real capture).
func outputJSONToFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("wrote %s (%d bytes)", filepath.Base(path), len(b))
}

// _ = silences the unused-function lint when the fixture capture
// seam hasn't been filled.
var _ = fmt.Stringer(nil)
var _ = outputJSONToFile
