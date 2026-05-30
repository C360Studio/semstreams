package agentic_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestReasoningRecord_JSONRoundTrip pins that every field on
// ReasoningRecord survives a marshal/unmarshal cycle. Required by
// feedback_polymorphic_config_needs_json_roundtrip_test — operator-
// reachable shapes that traverse JSON boundaries (KV writes, NATS
// payloads, trajectory exports) need explicit coverage so a future
// field add can't silently lose data.
func TestReasoningRecord_JSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   agentic.ReasoningRecord
	}{
		{
			name: "tool_call carrier (Gemini)",
			in: agentic.ReasoningRecord{
				Provider:    "google",
				CarrierKind: agentic.ReasoningCarrierToolCall,
				ToolCallID:  "call-1",
				Opaque:      []byte("opaque-sig-bytes"),
			},
		},
		{
			name: "standalone_item carrier (OpenAI Responses)",
			in: agentic.ReasoningRecord{
				Provider:    "openai",
				CarrierKind: agentic.ReasoningCarrierStandaloneItem,
				ItemID:      "rs_abc123",
				SummaryText: "thinking about the problem",
				Opaque:      []byte("encrypted-content-blob"),
			},
		},
		{
			name: "assistant_content carrier (Anthropic, reserved)",
			in: agentic.ReasoningRecord{
				Provider:    "anthropic",
				CarrierKind: agentic.ReasoningCarrierAssistantContent,
				Opaque:      []byte("thinking-block-content"),
			},
		},
		{
			name: "minimum populated",
			in: agentic.ReasoningRecord{
				Provider:    "google",
				CarrierKind: agentic.ReasoningCarrierToolCall,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got agentic.ReasoningRecord
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tc.in, got) {
				t.Errorf("round-trip mismatch\n  in:  %#v\n  got: %#v\n  json: %s", tc.in, got, string(b))
			}
			// Opaque must survive byte-for-byte — providers treat it as
			// a verbatim blob and any encoding drift breaks echo.
			if !bytes.Equal(tc.in.Opaque, got.Opaque) {
				t.Errorf("Opaque mismatch: in=%q got=%q", tc.in.Opaque, got.Opaque)
			}
		})
	}
}

// TestChatMessage_ReasoningRecords_JSONRoundTrip pins that the new
// ChatMessage.ReasoningRecords field survives a JSON round-trip
// alongside the existing ChatMessage fields.
func TestChatMessage_ReasoningRecords_JSONRoundTrip(t *testing.T) {
	in := agentic.ChatMessage{
		Role:    "assistant",
		Content: "calling tools",
		ToolCalls: []agentic.ToolCall{
			{ID: "call-1", Name: "f"},
			{ID: "call-2", Name: "g"},
		},
		ReasoningRecords: []agentic.ReasoningRecord{
			{
				Provider:    "google",
				CarrierKind: agentic.ReasoningCarrierToolCall,
				ToolCallID:  "call-1",
				Opaque:      []byte("sig-1"),
			},
			{
				Provider:    "openai",
				CarrierKind: agentic.ReasoningCarrierStandaloneItem,
				ItemID:      "rs_x",
				Opaque:      []byte("blob-x"),
			},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got agentic.ChatMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, got) {
		t.Errorf("round-trip mismatch\n  in:  %#v\n  got: %#v\n  json: %s", in, got, string(b))
	}
}

// TestChatMessage_NoReasoningRecords_OmitsField pins that messages
// without ReasoningRecords don't emit an empty array on the wire.
// JSON shape stability matters for sister-repo and trajectory
// consumers that introspect fields presence/absence.
func TestChatMessage_NoReasoningRecords_OmitsField(t *testing.T) {
	in := agentic.ChatMessage{Role: "user", Content: "hi"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("reasoning_records")) {
		t.Errorf("expected reasoning_records omitted when nil/empty; got %s", string(b))
	}
}
