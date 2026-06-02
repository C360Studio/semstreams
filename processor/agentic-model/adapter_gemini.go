package agenticmodel

import (
	"encoding/json"

	"github.com/c360studio/semstreams/model/wire"
)

// GeminiAdapter normalizes payloads for Google's Gemini OpenAI-compatible endpoint.
// Gemini's endpoint is broadly compatible but has several quirks that cause 400 errors.
//
// As of ADR-037 chunk 8, GeminiAdapter is Extras-aware for the Gemini
// 3.x preview thought_signature flow:
//   - NormalizeResponse extracts each tool_call's
//     extra_content.google.thought_signature into the framework-internal
//     carrier key wire.ToolCall.Extras[wireKeyC360ThoughtSignature]
//     for convertWireResponse to lift into agentic.ToolCall.Metadata.
//   - NormalizeMessages reconstructs the extra_content shape on the
//     outgoing wire ToolCall for the FIRST tool_call per assistant
//     message (Gemini's "first call per step" contract).
//
// The adapter only runs when endpoint.Provider == "gemini" (gated by
// AdapterFor). Non-Gemini paths leave the carrier untouched; the
// stripC360KeysFromRequest hygiene step in buildWireRequest removes it
// before send to avoid leaking the framework-internal key.
type GeminiAdapter struct{}

// Name returns "gemini".
func (a *GeminiAdapter) Name() string { return "gemini" }

// NormalizeRequest is a no-op for Gemini today. NormalizeMessages owns
// the thought_signature reconstruction so it runs per-message during
// request building.
func (a *GeminiAdapter) NormalizeRequest(_ *wire.ChatCompletionRequest) {}

// NormalizeMessages fixes Gemini-specific message constraints and
// reconstructs the per-tool_call thought_signature shape on outgoing
// assistant messages:
//
//  1. Tool result messages require a non-empty name field.
//     Without it: 400 INVALID_ARGUMENT: function_response.name cannot be empty.
//
//  2. Assistant messages with tool_calls require a non-empty content field.
//     Gemini rejects a completely absent content — single space is the
//     conventional workaround.
//
//  3. Assistant messages with the framework-internal
//     wireKeyC360ThoughtSignature carrier on their tool_calls get the
//     Gemini-shape extra_content reconstructed. Only the FIRST tool_call
//     per message carries the signature (Gemini 3.x contract — subsequent
//     calls in the same step inherit). The carrier key is deleted in place
//     to avoid double-send.
func (a *GeminiAdapter) NormalizeMessages(messages []wire.Message) []wire.Message {
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].Name == "" {
			messages[i].Name = "unknown_tool"
		}
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			s, ok := messages[i].ContentString()
			if !ok || s == "" {
				_ = messages[i].SetContentString(" ")
			}
			rebuildGeminiThoughtSignature(&messages[i])
		}
	}
	return messages
}

// rebuildGeminiThoughtSignature walks an assistant message's tool_calls
// and converts the framework-internal carrier on EVERY tool_call that
// has one into Gemini's extra_content.google.thought_signature shape.
//
// History (gh#188, 2026-06-02): the prior implementation gated emit
// on the FIRST tool_call only, citing Google's "the model only sets
// the signature on the first call in a parallel response" docs
// (ADR-037 chunk 8). That was a misreading — the docs describe what
// Gemini PUTS INTO responses, not what the client must ECHO BACK.
// semteams smoke #13 hit HTTP 400 from Gemini 3.x preview ("Function
// call is missing a thought_signature in functionCall parts. ...
// position 2.") because position-2's captured signature was being
// stripped on echo. The capture path correctly stores N signatures
// into N ReasoningRecords; the echo path now correctly re-attaches
// all N. Multi-tool-call cases (parallel-in-one-step on Gemini's
// side, AND sequential-step-2-bundled-into-tool_calls on our wire
// model) both want every signature preserved per
// [[feedback_live_gate_catches_doc_derived]].
func rebuildGeminiThoughtSignature(msg *wire.Message) {
	if msg == nil {
		return
	}
	for j := range msg.ToolCalls {
		tc := &msg.ToolCalls[j]
		rawSig, ok := tc.Extras[wireKeyC360ThoughtSignature]
		if !ok {
			continue
		}
		var sig string
		if err := json.Unmarshal(rawSig, &sig); err != nil || sig == "" {
			delete(tc.Extras, wireKeyC360ThoughtSignature)
			continue
		}
		// Build extra_content = {"google": {"thought_signature": "<sig>"}}.
		// Use a typed builder to avoid hand-marshaling nested maps —
		// matches Gemini's documented shape exactly.
		extra := map[string]any{
			"google": map[string]any{
				"thought_signature": sig,
			},
		}
		if b, err := json.Marshal(extra); err == nil {
			if tc.Extras == nil {
				tc.Extras = make(map[string]json.RawMessage, 1)
			}
			tc.Extras[wireKeyExtraContent] = b
		}
		delete(tc.Extras, wireKeyC360ThoughtSignature)
	}
}

// NormalizeStreamDelta infers the tool call index when Gemini omits it.
// Gemini streaming deltas never include an index field. Instead:
//   - A non-empty ID signals the start of a new tool call → return -1 (sentinel:
//     caller must allocate the next available index via nextToolIndex).
//   - An empty ID is an argument continuation → reuse lastIndex.
func (a *GeminiAdapter) NormalizeStreamDelta(delta wire.ToolCall, lastIndex int) int {
	if delta.Index != nil {
		return *delta.Index
	}
	if delta.ID != "" {
		return -1
	}
	return lastIndex
}

// NormalizeResponse extracts the per-tool_call thought_signature from
// Gemini's extra_content.google.thought_signature shape into the
// framework-internal carrier key wireKeyC360ThoughtSignature. The
// generic convertWireResponse then lifts the carrier into
// agentic.ChatMessage.ReasoningRecords as a
// ReasoningRecord{Provider:"google", CarrierKind:ToolCall, ToolCallID:...}
// so the signature flows across loop turns without Gemini-specific
// code in the agentic layer (ADR-051).
//
// Safe to call universally — adapters that don't see extra_content
// (every non-Gemini provider) read no carrier and write nothing.
func (a *GeminiAdapter) NormalizeResponse(resp *wire.ChatCompletionResponse) {
	if resp == nil {
		return
	}
	for ci := range resp.Choices {
		msg := resp.Choices[ci].Message
		if msg == nil {
			continue
		}
		for ti := range msg.ToolCalls {
			extractGeminiThoughtSignature(&msg.ToolCalls[ti])
		}
	}
}

// extractGeminiThoughtSignature MOVES extra_content.google.thought_signature
// from a wire.ToolCall.Extras into the framework-internal carrier key.
// Both the original wireKeyExtraContent entry and the carrier key are
// deleted after the agentic-side read in convertWireResponse (via the
// stripC360KeysFromResponse hygiene step), so the response object never
// carries the Gemini-shape blob alongside the carrier — debug logs and
// trace exports see a single normalized representation.
//
// Logging is intentionally absent here — the absence of a signature on
// a non-3.x preview Gemini response is the common case, not a warning
// condition.
func extractGeminiThoughtSignature(tc *wire.ToolCall) {
	if tc == nil {
		return
	}
	rawExtra, ok := tc.Extras[wireKeyExtraContent]
	if !ok {
		return
	}
	var shape struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	}
	if err := json.Unmarshal(rawExtra, &shape); err != nil {
		return
	}
	sig := shape.Google.ThoughtSignature
	if sig == "" {
		return
	}
	b, err := json.Marshal(sig)
	if err != nil {
		return
	}
	if tc.Extras == nil {
		tc.Extras = make(map[string]json.RawMessage, 1)
	}
	tc.Extras[wireKeyC360ThoughtSignature] = b
	delete(tc.Extras, wireKeyExtraContent)
}
