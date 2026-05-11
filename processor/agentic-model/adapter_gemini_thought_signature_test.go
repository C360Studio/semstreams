package agenticmodel

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire"
)

// TestGeminiAdapter_NormalizeResponse_ExtractsThoughtSignature asserts
// that a Gemini 3.x response shape with extra_content.google.thought_signature
// lands in the framework-internal carrier key for downstream pickup.
func TestGeminiAdapter_NormalizeResponse_ExtractsThoughtSignature(t *testing.T) {
	adapter := &GeminiAdapter{}
	extra, _ := json.Marshal(map[string]any{
		"google": map[string]any{
			"thought_signature": "sig-abc-123",
		},
	})
	msg := &wire.Message{
		Role: "assistant",
		ToolCalls: []wire.ToolCall{
			{
				ID:       "call-1",
				Function: wire.Function{Name: "f"},
				Extras: map[string]json.RawMessage{
					wireKeyExtraContent: extra,
				},
			},
		},
	}
	resp := &wire.ChatCompletionResponse{
		Choices: []wire.Choice{{Message: msg}},
	}

	adapter.NormalizeResponse(resp)

	gotRaw, ok := resp.Choices[0].Message.ToolCalls[0].Extras[wireKeyC360ThoughtSignature]
	if !ok {
		t.Fatalf("expected %q carrier key set; Extras=%v", wireKeyC360ThoughtSignature, resp.Choices[0].Message.ToolCalls[0].Extras)
	}
	var got string
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("decode carrier: %v", err)
	}
	if got != "sig-abc-123" {
		t.Errorf("carrier = %q, want %q", got, "sig-abc-123")
	}
}

// TestGeminiAdapter_NormalizeResponse_NoExtraContent_IsNoOp asserts
// that a response without extra_content (the common case on non-3.x
// Gemini builds) leaves Extras untouched.
func TestGeminiAdapter_NormalizeResponse_NoExtraContent_IsNoOp(t *testing.T) {
	adapter := &GeminiAdapter{}
	msg := &wire.Message{
		Role:      "assistant",
		ToolCalls: []wire.ToolCall{{ID: "call-1", Function: wire.Function{Name: "f"}}},
	}
	resp := &wire.ChatCompletionResponse{
		Choices: []wire.Choice{{Message: msg}},
	}

	adapter.NormalizeResponse(resp)

	if _, ok := resp.Choices[0].Message.ToolCalls[0].Extras[wireKeyC360ThoughtSignature]; ok {
		t.Error("expected no carrier key written when no extra_content present")
	}
}

// TestGeminiAdapter_NormalizeMessages_ReconstructsExtraContent asserts
// that an assistant message carrying the framework-internal signature
// emits Gemini's extra_content.google.thought_signature shape.
func TestGeminiAdapter_NormalizeMessages_ReconstructsExtraContent(t *testing.T) {
	adapter := &GeminiAdapter{}
	sigBytes, _ := json.Marshal("sig-reconstruct")
	msg := wire.Message{
		Role: "assistant",
		ToolCalls: []wire.ToolCall{
			{
				ID:       "call-1",
				Function: wire.Function{Name: "f"},
				Extras: map[string]json.RawMessage{
					wireKeyC360ThoughtSignature: sigBytes,
				},
			},
		},
	}
	_ = msg.SetContentString("calling")

	out := adapter.NormalizeMessages([]wire.Message{msg})

	tc := out[0].ToolCalls[0]
	if _, present := tc.Extras[wireKeyC360ThoughtSignature]; present {
		t.Error("framework-internal carrier should be deleted after rebuild")
	}
	rawExtra, ok := tc.Extras[wireKeyExtraContent]
	if !ok {
		t.Fatalf("expected %q to be set; Extras=%v", wireKeyExtraContent, tc.Extras)
	}
	var shape struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	}
	if err := json.Unmarshal(rawExtra, &shape); err != nil {
		t.Fatalf("decode extra_content: %v", err)
	}
	if shape.Google.ThoughtSignature != "sig-reconstruct" {
		t.Errorf("thought_signature = %q, want %q", shape.Google.ThoughtSignature, "sig-reconstruct")
	}
}

// TestGeminiAdapter_NormalizeMessages_OnlyFirstToolCallKeepsSignature
// asserts that when an assistant message carries multiple tool_calls
// each with the framework-internal carrier, only the FIRST emits
// extra_content. Gemini's "first call per step" contract: sending the
// signature on every tool_call duplicates information.
func TestGeminiAdapter_NormalizeMessages_OnlyFirstToolCallKeepsSignature(t *testing.T) {
	adapter := &GeminiAdapter{}
	sigA, _ := json.Marshal("sig-A")
	sigB, _ := json.Marshal("sig-B")
	msg := wire.Message{
		Role: "assistant",
		ToolCalls: []wire.ToolCall{
			{
				ID:       "call-1",
				Function: wire.Function{Name: "f1"},
				Extras:   map[string]json.RawMessage{wireKeyC360ThoughtSignature: sigA},
			},
			{
				ID:       "call-2",
				Function: wire.Function{Name: "f2"},
				Extras:   map[string]json.RawMessage{wireKeyC360ThoughtSignature: sigB},
			},
		},
	}
	_ = msg.SetContentString("hi")

	out := adapter.NormalizeMessages([]wire.Message{msg})

	first := out[0].ToolCalls[0]
	if _, ok := first.Extras[wireKeyExtraContent]; !ok {
		t.Error("first tool_call should carry extra_content")
	}
	if _, ok := first.Extras[wireKeyC360ThoughtSignature]; ok {
		t.Error("first tool_call: carrier should be deleted after rebuild")
	}

	second := out[0].ToolCalls[1]
	if _, ok := second.Extras[wireKeyExtraContent]; ok {
		t.Error("second tool_call: extra_content should NOT be set (Gemini first-call-per-step)")
	}
	if _, ok := second.Extras[wireKeyC360ThoughtSignature]; ok {
		t.Error("second tool_call: carrier should be stripped")
	}
}

// TestSignature_RoundTripThroughAgentic asserts the full agentic ↔ wire
// round-trip: response signature → agentic.Metadata → next request's
// wire extra_content. Mirrors what the loop does across turns.
func TestSignature_RoundTripThroughAgentic(t *testing.T) {
	// Step 1: simulate Gemini response with thought_signature.
	extra, _ := json.Marshal(map[string]any{
		"google": map[string]any{"thought_signature": "sig-rt"},
	})
	resp := &wire.ChatCompletionResponse{
		Choices: []wire.Choice{
			{
				Message: &wire.Message{
					Role: "assistant",
					ToolCalls: []wire.ToolCall{
						{
							ID:       "call-1",
							Function: wire.Function{Name: "f", Arguments: "{}"},
							Extras:   map[string]json.RawMessage{wireKeyExtraContent: extra},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	// Step 2: adapter extracts to carrier.
	(&GeminiAdapter{}).NormalizeResponse(resp)

	// Step 3: convertWireResponse lifts to agentic.Metadata.
	c := &Client{endpoint: nil, adapter: &GeminiAdapter{}}
	agentResp := c.convertWireResponse(resp, "req-rt")
	if len(agentResp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(agentResp.Message.ToolCalls))
	}
	sig, _ := agentResp.Message.ToolCalls[0].Metadata[agentic.MetadataKeyGoogleThoughtSignature].(string)
	if sig != "sig-rt" {
		t.Fatalf("agentic Metadata sig = %q, want sig-rt", sig)
	}

	// Step 4: next-turn translation puts signature back on the wire.
	wireMsgs := agenticMessagesToWire([]agentic.ChatMessage{
		{Role: "assistant", ToolCalls: agentResp.Message.ToolCalls},
	})
	carrier, ok := wireMsgs[0].ToolCalls[0].Extras[wireKeyC360ThoughtSignature]
	if !ok {
		t.Fatal("agenticToolCallsToWire should write framework-internal carrier")
	}
	var carrierStr string
	_ = json.Unmarshal(carrier, &carrierStr)
	if carrierStr != "sig-rt" {
		t.Errorf("carrier value = %q, want sig-rt", carrierStr)
	}

	// Step 5: NormalizeMessages reconstructs extra_content.
	out := (&GeminiAdapter{}).NormalizeMessages(wireMsgs)
	rawExtra, ok := out[0].ToolCalls[0].Extras[wireKeyExtraContent]
	if !ok {
		t.Fatal("NormalizeMessages should reconstruct extra_content")
	}
	var shape struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	}
	_ = json.Unmarshal(rawExtra, &shape)
	if shape.Google.ThoughtSignature != "sig-rt" {
		t.Errorf("reconstructed signature = %q, want sig-rt", shape.Google.ThoughtSignature)
	}
}

// TestStripC360KeysFromRequest_LeaksProtection asserts that any
// remaining framework-internal carrier keys are stripped before send,
// even if the adapter chain didn't consume them (defense in depth for
// non-Gemini fallback paths).
func TestStripC360KeysFromRequest_LeaksProtection(t *testing.T) {
	sig, _ := json.Marshal("leftover")
	req := &wire.ChatCompletionRequest{
		Messages: []wire.Message{
			{
				Role: "assistant",
				ToolCalls: []wire.ToolCall{
					{
						ID:     "call-1",
						Extras: map[string]json.RawMessage{wireKeyC360ThoughtSignature: sig},
					},
				},
			},
		},
	}
	stripC360KeysFromRequest(req)
	if _, ok := req.Messages[0].ToolCalls[0].Extras[wireKeyC360ThoughtSignature]; ok {
		t.Error("expected framework-internal carrier stripped before send")
	}
}
