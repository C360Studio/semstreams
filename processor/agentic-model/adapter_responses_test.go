package agenticmodel

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// TestOpenAIResponsesAdapter_Name pins the provider identifier.
func TestOpenAIResponsesAdapter_Name(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	if a.Name() != "openai" {
		t.Errorf("Name = %q, want openai", a.Name())
	}
}

// TestOpenAIResponsesAdapter_CaptureReasoningRecords pins the
// capture path: reasoning output items in a Response become
// ReasoningRecord entries with Provider=openai,
// CarrierKind=StandaloneItem, the item's encrypted_content as
// Opaque, and the first summary text as SummaryText.
func TestOpenAIResponsesAdapter_CaptureReasoningRecords(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	resp := &responses.Response{
		Output: []responses.OutputItem{
			{Type: responses.ItemTypeMessage, ID: "msg_1", Role: responses.RoleAssistant},
			{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_1",
				EncryptedContent: "opaque-blob-1",
				Summary: []responses.SummaryPart{
					{Type: responses.SummaryTypeText, Text: "thinking 1"},
				},
			},
			{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_2",
				EncryptedContent: "opaque-blob-2",
			},
		},
	}
	got := a.CaptureReasoningRecords(resp)
	if len(got) != 2 {
		t.Fatalf("captured %d records, want 2", len(got))
	}
	for _, rec := range got {
		if rec.Provider != "openai" {
			t.Errorf("Provider = %q, want openai", rec.Provider)
		}
		if rec.CarrierKind != agentic.ReasoningCarrierStandaloneItem {
			t.Errorf("CarrierKind = %q, want standalone_item", rec.CarrierKind)
		}
	}
	if got[0].ItemID != "rs_1" || string(got[0].Opaque) != "opaque-blob-1" {
		t.Errorf("rec[0] mismatch: %+v", got[0])
	}
	if got[0].SummaryText != "thinking 1" {
		t.Errorf("rec[0].SummaryText = %q, want %q", got[0].SummaryText, "thinking 1")
	}
	if got[1].ItemID != "rs_2" || string(got[1].Opaque) != "opaque-blob-2" {
		t.Errorf("rec[1] mismatch: %+v", got[1])
	}
	if got[1].SummaryText != "" {
		t.Errorf("rec[1].SummaryText = %q, want empty (no summary)", got[1].SummaryText)
	}
}

// TestOpenAIResponsesAdapter_CaptureReasoningRecords_NilResp pins
// graceful handling of nil input.
func TestOpenAIResponsesAdapter_CaptureReasoningRecords_NilResp(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	if got := a.CaptureReasoningRecords(nil); got != nil {
		t.Errorf("expected nil on nil response, got %+v", got)
	}
}

// TestOpenAIResponsesAdapter_EchoReasoningRecords pins the echo
// path: a ReasoningRecord becomes a reasoning InputItem with id,
// encrypted_content, and a summary part when SummaryText is set.
func TestOpenAIResponsesAdapter_EchoReasoningRecords(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	records := []agentic.ReasoningRecord{
		{
			Provider:    "openai",
			CarrierKind: agentic.ReasoningCarrierStandaloneItem,
			ItemID:      "rs_1",
			Opaque:      []byte("blob-1"),
			SummaryText: "summary one",
		},
		{
			Provider:    "openai",
			CarrierKind: agentic.ReasoningCarrierStandaloneItem,
			ItemID:      "rs_2",
			Opaque:      []byte("blob-2"),
		},
	}
	got := a.EchoReasoningRecords(records)
	if len(got) != 2 {
		t.Fatalf("echoed %d items, want 2", len(got))
	}
	if !got[0].IsReasoning() {
		t.Errorf("got[0].Type = %q, want reasoning", got[0].Type)
	}
	if got[0].ID != "rs_1" || got[0].EncryptedContent != "blob-1" {
		t.Errorf("got[0] mismatch: %+v", got[0])
	}
	if len(got[0].Summary) != 1 || got[0].Summary[0].Text != "summary one" {
		t.Errorf("got[0].Summary = %+v, want one summary_text part with %q", got[0].Summary, "summary one")
	}
	if len(got[1].Summary) != 0 {
		t.Errorf("got[1].Summary = %+v, want empty (no SummaryText)", got[1].Summary)
	}
}

// TestOpenAIResponsesAdapter_EchoReasoningRecords_FiltersOtherProviders
// pins that records from other providers (e.g. Gemini ToolCall-
// carrier records) are NOT echoed by this adapter — they belong to
// a different wire shape and surface on a different request path.
func TestOpenAIResponsesAdapter_EchoReasoningRecords_FiltersOtherProviders(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	records := []agentic.ReasoningRecord{
		{
			Provider:    "google",
			CarrierKind: agentic.ReasoningCarrierToolCall,
			ToolCallID:  "call-1",
			Opaque:      []byte("gemini-sig"),
		},
		{
			Provider:    "openai",
			CarrierKind: agentic.ReasoningCarrierStandaloneItem,
			ItemID:      "rs_1",
			Opaque:      []byte("openai-blob"),
		},
	}
	got := a.EchoReasoningRecords(records)
	if len(got) != 1 {
		t.Fatalf("echoed %d items, want 1 (Gemini record should be filtered)", len(got))
	}
	if got[0].ID != "rs_1" {
		t.Errorf("got[0].ID = %q, want rs_1 (openai-only filter)", got[0].ID)
	}
}

// TestResponsesAdapterFor_Defaults pins the lookup contract: known
// providers and unknowns both return the OpenAIResponsesAdapter
// (the safe default for any Responses-compat surface).
func TestResponsesAdapterFor_Defaults(t *testing.T) {
	cases := []string{"openai", "unknown", ""}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			a := ResponsesAdapterFor(p)
			if _, ok := a.(*OpenAIResponsesAdapter); !ok {
				t.Errorf("ResponsesAdapterFor(%q) = %T, want *OpenAIResponsesAdapter", p, a)
			}
		})
	}
}

// TestOpenAIResponsesAdapter_RoundTrip pins capture+echo as inverse
// operations: a Response with reasoning items captures to records
// that, when echoed back, reconstruct equivalent reasoning
// InputItems (id + encrypted_content preserved; summary recovered
// when present).
func TestOpenAIResponsesAdapter_RoundTrip(t *testing.T) {
	a := &OpenAIResponsesAdapter{}
	original := &responses.Response{
		Output: []responses.OutputItem{
			{
				Type:             responses.ItemTypeReasoning,
				ID:               "rs_rt",
				EncryptedContent: "round-trip-blob",
				Summary: []responses.SummaryPart{
					{Type: responses.SummaryTypeText, Text: "round-trip summary"},
				},
			},
		},
	}
	records := a.CaptureReasoningRecords(original)
	echoed := a.EchoReasoningRecords(records)
	if len(echoed) != 1 {
		t.Fatalf("echoed %d items, want 1", len(echoed))
	}
	if echoed[0].ID != "rs_rt" {
		t.Errorf("ID drift: %q -> %q", "rs_rt", echoed[0].ID)
	}
	if echoed[0].EncryptedContent != "round-trip-blob" {
		t.Errorf("EncryptedContent drift: %q -> %q", "round-trip-blob", echoed[0].EncryptedContent)
	}
	if len(echoed[0].Summary) != 1 || echoed[0].Summary[0].Text != "round-trip summary" {
		t.Errorf("Summary drift: %+v", echoed[0].Summary)
	}
}
