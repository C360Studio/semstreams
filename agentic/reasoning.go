package agentic

// ReasoningRecord is the provider-neutral carrier for opaque reasoning
// state that must be echoed back on the next turn. Captured on
// response, attached to the next request. Provider-specific reshape
// happens at the adapter seam — loop code stays shape-neutral.
//
// Replaces the per-provider MetadataKeyGoogleThoughtSignature carrier
// retired in ADR-051 Phase 1. The semantic role ("opaque blob the
// model wants echoed on the next turn for reasoning continuity") is
// stable across providers; the wire shape is not, so the abstraction
// lives at the role layer.
type ReasoningRecord struct {
	// Provider names the carrier provider. The adapter uses this to
	// decide on-the-wire reconstruction. Known values: "google",
	// "openai". Future providers extend the set.
	Provider string `json:"provider"`

	// ItemID is the provider-assigned identity for this record.
	// Used for cross-turn echo when the provider needs it. OpenAI
	// Responses uses it (id of the reasoning item); Gemini does not
	// (signatures are carried per-tool-call, not by id).
	ItemID string `json:"item_id,omitempty"`

	// SummaryText is a human-readable description of the reasoning,
	// when the provider exposes one. Safe to log. Used for trajectory
	// and operator-facing trace.
	SummaryText string `json:"summary_text,omitempty"`

	// Opaque is the provider-specific blob that must be echoed back
	// verbatim. Treat as bytes; do not parse, do not log in full.
	//   - Gemini: the base64 thought_signature string (bytes of UTF-8)
	//   - OpenAI: encrypted_content blob (when store:false)
	//   - Anthropic (future): thinking block content
	Opaque []byte `json:"opaque,omitempty"`

	// CarrierKind names the structural attachment constraint on the
	// wire. Adapters use this to reshape correctly. The set is closed:
	// unknown values are an authoring error and must fail validation,
	// not silently pass.
	CarrierKind ReasoningCarrierKind `json:"carrier_kind"`

	// ToolCallID is set when CarrierKind == ReasoningCarrierToolCall.
	// The signature belongs with this specific tool call; the adapter
	// re-binds them on send.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ReasoningCarrierKind enumerates the structural attachment shapes a
// provider's reasoning blob can take on the wire. Closed set: adapters
// must handle every value, and unknown values are an authoring error.
type ReasoningCarrierKind string

const (
	// ReasoningCarrierToolCall indicates the blob attaches to a
	// specific tool call on the wire. Used by Gemini (thought_signature
	// per tool_call).
	ReasoningCarrierToolCall ReasoningCarrierKind = "tool_call"

	// ReasoningCarrierStandaloneItem indicates the blob is a sibling
	// output item with no attachment to messages or tool calls. Used
	// by OpenAI Responses (reasoning items in the output array).
	ReasoningCarrierStandaloneItem ReasoningCarrierKind = "standalone_item"

	// ReasoningCarrierAssistantContent indicates the blob is a content
	// part inside the assistant message. Reserved for Anthropic's
	// thinking blocks when/if we take that on.
	ReasoningCarrierAssistantContent ReasoningCarrierKind = "assistant_content"
)
