package agenticmodel

import (
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// ResponsesAdapter is the parallel of ProviderAdapter for the
// Responses path. It normalizes request/response payloads and
// streaming events for providers that speak the OpenAI Responses
// wire shape.
//
// Today OpenAI is the only provider on this surface; the interface
// exists per ADR-051 D4 to keep the door open for future Responses-
// compatible providers without reshaping the call sites. Hooks are
// mostly no-ops; the non-trivial work lives in capture/echo of
// agentic.ReasoningRecord{CarrierKind:StandaloneItem}.
type ResponsesAdapter interface {
	// Name returns the provider identifier (e.g. "openai").
	Name() string

	// NormalizeRequest adjusts the wire request before sending.
	// Called after translation, before the HTTP call.
	NormalizeRequest(req *responses.Request)

	// CaptureReasoningRecords walks the response's output items,
	// extracts any provider-opaque reasoning blobs, and returns
	// them as ReasoningRecord{CarrierKind:StandaloneItem} entries
	// the caller appends to ChatMessage.ReasoningRecords.
	//
	// Returns nil if the response carries no reasoning items.
	CaptureReasoningRecords(resp *responses.Response) []agentic.ReasoningRecord

	// EchoReasoningRecords reverses CaptureReasoningRecords: walks
	// the caller-supplied records (filtered by Provider), emits
	// corresponding InputItem reasoning entries the request builder
	// inserts into Request.Input alongside the regular items.
	//
	// Per OpenAI's per-step echo rule, reasoning items are echoed
	// in order relative to other input items the caller already
	// authored.
	EchoReasoningRecords(records []agentic.ReasoningRecord) []responses.InputItem
}

// OpenAIResponsesAdapter is the OpenAI implementation of
// ResponsesAdapter. Capture/echo cover the
// {type:"reasoning", id, encrypted_content} item shape that OpenAI's
// /v1/responses surfaces in stateless (store=false) mode.
type OpenAIResponsesAdapter struct{}

// Name returns "openai".
func (a *OpenAIResponsesAdapter) Name() string { return "openai" }

// NormalizeRequest is a no-op today. Placeholder for future per-
// request shape fixes (the OpenAI Responses surface is stable
// enough today that no normalization is required).
func (a *OpenAIResponsesAdapter) NormalizeRequest(_ *responses.Request) {}

// CaptureReasoningRecords extracts {type:"reasoning"} output items
// from the Response into ReasoningRecord entries. Each captured
// record carries the encrypted_content blob as Opaque bytes and
// the summary's first text part as SummaryText (for trajectory /
// trace consumers; safe to log).
//
// Non-reasoning output items are ignored. The slice is preserved
// in OutputItem order so cross-turn echo can preserve relative
// position.
func (a *OpenAIResponsesAdapter) CaptureReasoningRecords(resp *responses.Response) []agentic.ReasoningRecord {
	if resp == nil {
		return nil
	}
	var out []agentic.ReasoningRecord
	for i := range resp.Output {
		item := &resp.Output[i]
		if !item.IsReasoning() {
			continue
		}
		rec := agentic.ReasoningRecord{
			Provider:    "openai",
			CarrierKind: agentic.ReasoningCarrierStandaloneItem,
			ItemID:      item.ID,
			Opaque:      []byte(item.EncryptedContent),
		}
		if len(item.Summary) > 0 && item.Summary[0].Text != "" {
			rec.SummaryText = item.Summary[0].Text
		}
		out = append(out, rec)
	}
	return out
}

// EchoReasoningRecords reverses CaptureReasoningRecords: each
// matching record becomes a reasoning InputItem with id and
// encrypted_content set. Records carrying a non-empty SummaryText
// emit a single summary_text part — providers tolerate a missing
// summary, so empty SummaryText omits the field entirely.
//
// Records with Provider != "openai" are skipped; the adapter only
// echoes its own provider's blobs, leaving cross-provider records
// (e.g. Gemini ToolCall-carrier records) untouched for the
// ChatCompletion adapter to handle on a different request.
func (a *OpenAIResponsesAdapter) EchoReasoningRecords(records []agentic.ReasoningRecord) []responses.InputItem {
	if len(records) == 0 {
		return nil
	}
	var out []responses.InputItem
	for _, rec := range records {
		if rec.Provider != "openai" {
			continue
		}
		if rec.CarrierKind != agentic.ReasoningCarrierStandaloneItem {
			continue
		}
		var summary []responses.SummaryPart
		if rec.SummaryText != "" {
			summary = []responses.SummaryPart{
				{Type: responses.SummaryTypeText, Text: rec.SummaryText},
			}
		}
		out = append(out, responses.NewInputReasoning(rec.ItemID, string(rec.Opaque), summary))
	}
	return out
}

// defaultResponsesAdapter is the fallback used when no provider-
// specific Responses adapter is set on the Client. Package-level
// singleton avoids repeated allocation of the stateless struct.
var defaultResponsesAdapter ResponsesAdapter = &OpenAIResponsesAdapter{}

// ResponsesAdapterFor returns the appropriate ResponsesAdapter for
// the given provider name. Today OpenAI is the only Responses-
// compatible provider; unknown providers fall through to the OpenAI
// adapter (safe default — the wire shape IS the OpenAI spec, so
// any third-party Responses-compat provider should accept the same
// capture/echo).
func ResponsesAdapterFor(provider string) ResponsesAdapter {
	switch provider {
	case "openai":
		return &OpenAIResponsesAdapter{}
	default:
		return &OpenAIResponsesAdapter{}
	}
}
