//go:build live_llm

package agenticmodel_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/model/wire/responses"
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

// TestOpenAIResponses_Streaming exercises the typed-event SSE
// parser + Accumulator end-to-end against real /v1/responses with
// Stream=true. Closes the skeleton-from-docs gap on the ~21 SSE
// event types: until this runs, the parser and accumulator are
// doc-derived modeling. A wire-shape mismatch on any event type
// breaks the streaming path silently in production. The test
// asserts:
//
//  1. Stream connects and yields a sequence of events (no decode
//     errors mid-stream).
//  2. At least one response.created and one response.completed
//     event arrive (lifecycle markers present).
//  3. At least one output_text.delta event carries non-empty
//     Delta text (the accumulator's primary input).
//  4. The Accumulator's Final() produces a Response whose Output
//     contains the final text — proving incremental + terminal
//     paths agree.
func TestOpenAIResponses_Streaming(t *testing.T) {
	apiKey := requireOpenAIAPIKey(t)
	wireClient, err := buildLiveResponsesClient(apiKey)
	if err != nil {
		t.Fatalf("build responses client: %v", err)
	}

	chosenModel := openAIResponsesTestModel()
	t.Logf("Streaming test against model=%q", chosenModel)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := &responses.Request{
		Model: chosenModel,
		Input: []responses.InputItem{
			responses.NewInputUserMessage("Count from 1 to 5, one number per line."),
		},
		MaxOutputTokens: 256,
	}
	stream, err := wireClient.ResponsesStream(ctx, req)
	if err != nil {
		t.Fatalf("ResponsesStream: %v", err)
	}
	defer stream.Close()

	acc := responses.NewAccumulator()
	var sawCreated, sawCompleted bool
	var deltaCount int
	var totalDeltaLen int
	eventCount := 0

	for {
		ev, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatalf("stream.Recv: %v", recvErr)
		}
		if ev == nil {
			break
		}
		eventCount++

		switch ev.Type {
		case responses.EventTypeResponseCreated:
			sawCreated = true
		case responses.EventTypeResponseCompleted:
			sawCompleted = true
		case responses.EventTypeOutputTextDelta:
			if ev.Delta != "" {
				deltaCount++
				totalDeltaLen += len(ev.Delta)
			}
		}

		if accErr := acc.Add(ev); accErr != nil {
			t.Logf("accumulator skip on %s: %v", ev.Type, accErr)
		}
	}

	t.Logf("streamed %d events: created=%v completed=%v deltas=%d total_delta_bytes=%d",
		eventCount, sawCreated, sawCompleted, deltaCount, totalDeltaLen)

	if !sawCreated {
		t.Error("did not see response.created — lifecycle missing")
	}
	if !sawCompleted {
		t.Error("did not see response.completed — terminal event missing")
	}
	if deltaCount == 0 {
		t.Error("no output_text.delta events with text — accumulator can't build incrementally")
	}

	final := acc.Final()
	if final == nil {
		t.Fatal("Accumulator.Final() returned nil")
	}
	if final.Status != "completed" {
		t.Errorf("Final.Status = %q, want completed (terminal event promoted)", final.Status)
	}
	var assembledText string
	for i := range final.Output {
		if final.Output[i].IsMessage() {
			assembledText += final.Output[i].OutputText()
		}
	}
	if assembledText == "" {
		t.Error("Final response has no assembled text — accumulator/terminal-promote disagree")
	}
	t.Logf("Final assembled text: %q (%d chars)", assembledText, len(assembledText))

	// Sanity: the model was asked to count to 5; check for digits.
	for _, want := range []string{"1", "2", "3", "4", "5"} {
		if !strings.Contains(assembledText, want) {
			t.Errorf("expected %q in counted output, got %q", want, assembledText)
		}
	}
}

// TestOpenAIResponses_CaptureFixtures harvests live request/response
// bodies into model/wire/responses/testdata/ for the unit-level
// round-trip parity tests in model/wire/responses/. Skips by default;
// enable with CAPTURE_FIXTURES=1.
//
// Captured pairs (filenames stable so
// TestResponses_GoldenFixture_Parity can iterate them):
//   - request_simple_text.json + response_simple_text.json
//   - request_function_call_round.json +
//     response_function_call_with_reasoning.json
//
// We drive responses.Client directly here rather than going through
// the agentic-model client — keeps the captured shape the wire-level
// truth without translator artifacts.
func TestOpenAIResponses_CaptureFixtures(t *testing.T) {
	if os.Getenv("CAPTURE_FIXTURES") != "1" {
		t.Skip("CAPTURE_FIXTURES != 1; skipping fixture capture")
	}
	apiKey := requireOpenAIAPIKey(t)
	testdataDir, err := filepath.Abs(filepath.Join("..", "..", "model", "wire", "responses", "testdata"))
	if err != nil {
		t.Fatalf("resolve testdata dir: %v", err)
	}
	if _, err := os.Stat(testdataDir); err != nil {
		t.Fatalf("testdata dir not found: %v", err)
	}

	wireClient, err := buildLiveResponsesClient(apiKey)
	if err != nil {
		t.Fatalf("build responses client: %v", err)
	}

	chosenModel := openAIResponsesReasoningModel()
	t.Logf("Capturing fixtures against model=%q", chosenModel)

	// Fixture 1: minimal text request + response.
	simpleReq := &responses.Request{
		Model: chosenModel,
		Input: []responses.InputItem{
			responses.NewInputUserMessage("Reply with only the word 'pong'."),
		},
		MaxOutputTokens: 64,
	}
	writeFixture(t, testdataDir, "request_simple_text.json", simpleReq)
	simpleResp, err := wireClient.Responses(context.Background(), simpleReq)
	if err != nil {
		t.Fatalf("simple-text Responses: %v", err)
	}
	writeFixture(t, testdataDir, "response_simple_text.json", simpleResp)

	// Fixture 2: function-call round with reasoning echo.
	// Step A: drive a tool-use turn with reasoning_effort to produce
	// a reasoning item + function_call output.
	tools := []responses.Tool{
		{
			Type:        "function",
			Name:        "multiply",
			Description: "Multiplies two integers.",
			Parameters: json.RawMessage(`{
				"type":"object",
				"properties":{
					"a":{"type":"integer"},
					"b":{"type":"integer"}
				},
				"required":["a","b"],
				"additionalProperties":false
			}`),
			Strict: true,
		},
	}
	storeFalse := false
	turnAReq := &responses.Request{
		Model: chosenModel,
		Input: []responses.InputItem{
			responses.NewInputUserMessage("What is 47 * 89? Think, then use the multiply tool."),
		},
		Tools:           tools,
		Reasoning:       &responses.ReasoningParams{Effort: "medium"},
		Include:         []string{"reasoning.encrypted_content"},
		Store:           &storeFalse,
		MaxOutputTokens: 1024,
	}
	turnAResp, err := wireClient.Responses(context.Background(), turnAReq)
	if err != nil {
		t.Fatalf("turn A (reasoning + tool): %v", err)
	}
	// Capture turn A's response — this is the
	// "response with reasoning + function_call" fixture.
	writeFixture(t, testdataDir, "response_function_call_with_reasoning.json", turnAResp)

	// Step B: build the echo request that replays the reasoning items
	// + function_call + supplies the tool result. THIS is the input-side
	// request fixture worth capturing — the round-trip shape that
	// exercises echo correctness.
	turnBInput := []responses.InputItem{
		responses.NewInputUserMessage("What is 47 * 89? Think, then use the multiply tool."),
	}
	var toolCallID string
	for i := range turnAResp.Output {
		item := &turnAResp.Output[i]
		switch {
		case item.IsReasoning():
			turnBInput = append(turnBInput, responses.NewInputReasoning(
				item.ID, item.EncryptedContent, item.Summary,
			))
		case item.IsFunctionCall():
			turnBInput = append(turnBInput, responses.NewInputFunctionCall(
				item.CallID, item.Name, item.Arguments,
			))
			toolCallID = item.CallID
		}
	}
	if toolCallID != "" {
		turnBInput = append(turnBInput, responses.NewInputFunctionCallOutput(
			toolCallID, `{"product":4183}`,
		))
	}
	turnBReq := &responses.Request{
		Model:           chosenModel,
		Input:           turnBInput,
		Tools:           tools,
		Reasoning:       &responses.ReasoningParams{Effort: "medium"},
		Include:         []string{"reasoning.encrypted_content"},
		Store:           &storeFalse,
		MaxOutputTokens: 1024,
	}
	writeFixture(t, testdataDir, "request_function_call_round.json", turnBReq)

	t.Logf("captured 4 fixtures into %s", testdataDir)
}

// buildLiveResponsesClient constructs a raw responses.Client against
// the real /v1/responses endpoint. Returns ready-to-use client.
func buildLiveResponsesClient(apiKey string) (*responses.Client, error) {
	return responses.NewClient(responses.ClientConfig{
		BaseURL:    "https://api.openai.com/v1",
		HTTPClient: http.DefaultClient,
		AuthHeader: "Bearer " + apiKey,
	})
}

// writeFixture marshals v as compact JSON and writes to testdataDir/name.
// Overwrites unconditionally; caller decides when to re-capture.
// Compact (not indented) so RawMessage fields (Parameters, Annotations)
// round-trip byte-for-byte through TestResponses_GoldenFixture_Parity
// — the wire representation is compact anyway. Use `jq .` for review.
func writeFixture(t *testing.T, testdataDir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	path := filepath.Join(testdataDir, name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	t.Logf("wrote %s (%d bytes)", name, len(b))
}
