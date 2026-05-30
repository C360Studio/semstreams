// Package responses is the self-hosted JSON wire-format layer for
// OpenAI's Responses API (POST /v1/responses). It is a sibling of
// model/wire (which speaks ChatCompletion) and the second top-level
// wire shape carried in the wire-package family.
//
// The Responses API is structurally distinct from ChatCompletions —
// it uses a typed-array input/output model (InputItem / OutputItem)
// rather than the message/choices model, dispatches polymorphism
// through a "type" discriminator on each item, and surfaces reasoning
// as a first-class item kind rather than an out-of-band field. The
// ChatCompletion wire types do not apply; this package owns its own
// Request, Response, item variants, and SSE streaming protocol.
//
// Phase 1 (this package's initial cut) implements the non-streaming
// request/response path covering function-calling and reasoning echo
// in stateless mode (store=false). Hosted tools (file_search,
// web_search_preview, code_interpreter, computer_use_preview,
// image_generation) are out of scope and not modeled here; the
// surface stays narrow until a concrete consumer asks. Streaming
// (typed-event SSE) lands in Phase 2 as a sibling file.
//
// Reasoning-record echo (the "ReasoningRecord opaque blob round-trip"
// per ADR-051 D3) is handled at the higher agentic-model adapter
// layer; this package merely models the wire shape of reasoning
// items and exposes their fields. See ADR-051 for the architectural
// motivation and ADR-037 for the parent wire-package shape.
package responses
