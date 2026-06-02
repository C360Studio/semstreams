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
	// extra_content must be removed after extraction (M2/L7) — the
	// response object should not carry both keys for downstream debug-log
	// / trace-export consumers.
	if _, present := resp.Choices[0].Message.ToolCalls[0].Extras[wireKeyExtraContent]; present {
		t.Error("expected extra_content removed after extraction")
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

// TestGeminiAdapter_NormalizeMessages_AllToolCallsKeepSignature
// asserts that when an assistant message carries multiple tool_calls
// each with the framework-internal carrier, EVERY position emits
// extra_content with its corresponding thought_signature.
//
// Inverts the prior pinning (gh#188, 2026-06-02): the previous test
// pinned a docs-derived "first-call-per-step" interpretation that
// surfaces as HTTP 400 on Gemini 3.x preview when position 2+
// signatures get stripped on echo. The docs describe what Gemini
// PUTS INTO responses; the client must ECHO BACK every captured
// signature. Per [[feedback_live_gate_catches_doc_derived]] the
// docs-derived behavior should have been live-gated before pinning —
// this test now enforces the correct contract.
func TestGeminiAdapter_NormalizeMessages_AllToolCallsKeepSignature(t *testing.T) {
	adapter := &GeminiAdapter{}
	sigA, _ := json.Marshal("sig-A")
	sigB, _ := json.Marshal("sig-B")
	sigC, _ := json.Marshal("sig-C")
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
			{
				ID:       "call-3",
				Function: wire.Function{Name: "f3"},
				Extras:   map[string]json.RawMessage{wireKeyC360ThoughtSignature: sigC},
			},
		},
	}
	_ = msg.SetContentString("hi")

	out := adapter.NormalizeMessages([]wire.Message{msg})

	wantSigs := []string{"sig-A", "sig-B", "sig-C"}
	for i, want := range wantSigs {
		tc := out[0].ToolCalls[i]
		rawExtra, ok := tc.Extras[wireKeyExtraContent]
		if !ok {
			t.Errorf("tool_call[%d] (call-%d): missing extra_content; gh#188 regression — every captured signature must echo", i, i+1)
			continue
		}
		if _, stillCarrier := tc.Extras[wireKeyC360ThoughtSignature]; stillCarrier {
			t.Errorf("tool_call[%d] (call-%d): framework carrier should be deleted after rebuild", i, i+1)
		}
		var shape struct {
			Google struct {
				ThoughtSignature string `json:"thought_signature"`
			} `json:"google"`
		}
		if err := json.Unmarshal(rawExtra, &shape); err != nil {
			t.Errorf("tool_call[%d] decode extra_content: %v", i, err)
			continue
		}
		if shape.Google.ThoughtSignature != want {
			t.Errorf("tool_call[%d] thought_signature = %q, want %q", i, shape.Google.ThoughtSignature, want)
		}
	}
}

// TestGeminiAdapter_NormalizeMessages_MissingCarrierIsToleratedAtAnyPosition
// pins that a tool_call without a carrier at ANY position (not just
// non-first) cleanly skips without affecting siblings. Catches the
// "I deleted the gate but introduced an early-return" regression
// shape.
func TestGeminiAdapter_NormalizeMessages_MissingCarrierIsToleratedAtAnyPosition(t *testing.T) {
	adapter := &GeminiAdapter{}
	sigA, _ := json.Marshal("sig-A")
	sigC, _ := json.Marshal("sig-C")
	msg := wire.Message{
		Role: "assistant",
		ToolCalls: []wire.ToolCall{
			{ID: "call-1", Function: wire.Function{Name: "f1"}, Extras: map[string]json.RawMessage{wireKeyC360ThoughtSignature: sigA}},
			{ID: "call-2", Function: wire.Function{Name: "f2"}}, // no carrier
			{ID: "call-3", Function: wire.Function{Name: "f3"}, Extras: map[string]json.RawMessage{wireKeyC360ThoughtSignature: sigC}},
		},
	}
	_ = msg.SetContentString("hi")

	out := adapter.NormalizeMessages([]wire.Message{msg})

	if _, ok := out[0].ToolCalls[0].Extras[wireKeyExtraContent]; !ok {
		t.Error("tool_call[0]: extra_content missing (sig-A should have been emitted)")
	}
	// tool_call[1] supplied no carrier → no extra_content expected.
	// Verify only if Extras was somehow populated (nil is the clean
	// case for "no signature was present").
	if extras := out[0].ToolCalls[1].Extras; extras != nil {
		if _, ok := extras[wireKeyExtraContent]; ok {
			t.Error("tool_call[1]: should NOT have extra_content (no carrier was supplied)")
		}
	}
	if _, ok := out[0].ToolCalls[2].Extras[wireKeyExtraContent]; !ok {
		t.Error("tool_call[2]: extra_content missing (sig-C should have been emitted, gap at position 1 must not block position 2)")
	}
}

// TestSignature_RoundTripThroughAgentic asserts the full agentic ↔ wire
// round-trip: response signature → agentic.ReasoningRecords →
// next request's wire extra_content. Mirrors what the loop does across
// turns. Post-ADR-051 the carrier is the message-level ReasoningRecords
// sibling field, not ToolCall.Metadata.
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

	// Step 3: convertWireResponse lifts the signature into the
	// message-level ReasoningRecords field, attributed to call-1.
	c := &Client{endpoint: nil, adapter: &GeminiAdapter{}}
	agentResp := c.convertWireResponse(resp, "req-rt")
	if len(agentResp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(agentResp.Message.ToolCalls))
	}
	if len(agentResp.Message.ReasoningRecords) != 1 {
		t.Fatalf("expected 1 ReasoningRecord, got %d", len(agentResp.Message.ReasoningRecords))
	}
	rec := agentResp.Message.ReasoningRecords[0]
	if rec.Provider != "google" {
		t.Errorf("Provider = %q, want google", rec.Provider)
	}
	if rec.CarrierKind != agentic.ReasoningCarrierToolCall {
		t.Errorf("CarrierKind = %q, want %q", rec.CarrierKind, agentic.ReasoningCarrierToolCall)
	}
	if rec.ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want call-1", rec.ToolCallID)
	}
	if string(rec.Opaque) != "sig-rt" {
		t.Fatalf("Opaque = %q, want sig-rt", string(rec.Opaque))
	}

	// Step 4: next-turn translation puts signature back on the wire.
	// attachReasoningRecordsToWire reads ReasoningRecords off the
	// assistant message and writes the framework-internal carrier onto
	// matching wire ToolCalls by ToolCallID.
	srcMsgs := []agentic.ChatMessage{
		{
			Role:             "assistant",
			ToolCalls:        agentResp.Message.ToolCalls,
			ReasoningRecords: agentResp.Message.ReasoningRecords,
		},
	}
	wireMsgs := attachReasoningRecordsToWire(agenticMessagesToWire(srcMsgs), srcMsgs)
	carrier, ok := wireMsgs[0].ToolCalls[0].Extras[wireKeyC360ThoughtSignature]
	if !ok {
		t.Fatal("attachReasoningRecordsToWire should write framework-internal carrier")
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

// TestStreamingPath_LiftsThoughtSignature asserts that the streaming
// wire path runs NormalizeResponse and lifts the thought_signature
// carrier into ChatMessage.ReasoningRecords. Without this, multi-turn
// tool flows on Gemini 3.x via wire+streaming silently fail to echo
// the signature on subsequent requests. This is the test that would
// have caught H1.
func TestStreamingPath_LiftsThoughtSignature(t *testing.T) {
	extra, _ := json.Marshal(map[string]any{
		"google": map[string]any{"thought_signature": "sig-streamed"},
	})
	acc := newWireStreamAccumulator(&GeminiAdapter{}, nil, nil)
	idx := 0
	acc.toolCalls = map[int]*wire.ToolCall{
		idx: {
			ID:       "call-1",
			Type:     "function",
			Function: wire.Function{Name: "f", Arguments: "{}"},
			Extras:   map[string]json.RawMessage{wireKeyExtraContent: extra},
		},
	}
	acc.lastToolIndex = idx
	acc.finishReason = "tool_calls"
	acc.role = "assistant"

	resp := acc.toAgentResponse("req-stream-sig")
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(resp.Message.ToolCalls))
	}
	if len(resp.Message.ReasoningRecords) != 1 {
		t.Fatalf("expected 1 ReasoningRecord on streamed response, got %d (NormalizeResponse not called?)",
			len(resp.Message.ReasoningRecords))
	}
	rec := resp.Message.ReasoningRecords[0]
	if rec.ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want call-1", rec.ToolCallID)
	}
	if string(rec.Opaque) != "sig-streamed" {
		t.Errorf("streaming path Opaque = %q, want sig-streamed", string(rec.Opaque))
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
