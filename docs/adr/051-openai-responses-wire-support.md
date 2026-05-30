# ADR-051: OpenAI Responses API Wire Support + Unified `ReasoningRecord`

## Status

**Proposed** — 2026-05-30. Companion research doc at
[`docs/proposals/openai-responses-wire-support.md`](../proposals/openai-responses-wire-support.md)
captures the option exploration; this ADR is the canonical decision
record. Extends [ADR-037](037-self-hosted-llm-wire-package.md) by
adding a second top-level wire shape to the `model/wire` package
layer. Sister-agent picks up implementation when this ADR is
Accepted; the proposal doc is the working/research artifact and stays
in `docs/proposals/` per the working-record convention.

## Context

### Forcing function

semspec hit a hard wall on its hybrid registry path: **the OpenAI
Chat Completions endpoint refuses `tool_choice` + `reasoning_effort`
together on GPT-5.5 and the o-series**. The combination is supported
only on the `/v1/responses` endpoint. semspec needs both — forced
tool to bind dev-related calls to a specific schema, and
`reasoning_effort` to get useful behavior out of the model class — so
the chat-completions path is non-viable for that consumer regardless
of how cleanly we wire it.

Codex-family models (`codex-mini`, `codex` previews, `gpt-5-codex`
when it lands) only ship on the Responses endpoint. semspec's hybrid
registry is OpenAI-routed for dev-related calls, so Codex is the
natural target model class for that path. Without Responses, semspec
is forced down to GPT-4o-class for dev calls and loses the reasoning
surface entirely.

This is **not anticipatory** scope. It is a named consumer with a
named constraint blocking a named call site. That satisfies the
forcing-function gate from [[feedback_reactive_patches_vs_engine_completion]]
— we are not adding wire-of-the-month, we are responding to a
concrete blocker.

### What the Responses API actually is

The Responses API is not a quirky variant of Chat Completions. It is
a different request/response shape with a different message model
and different streaming protocol. The transport is still HTTPS+JSON,
but the wire types do not overlap.

| Aspect | `/v1/chat/completions` | `/v1/responses` |
|---|---|---|
| Endpoint | `POST /v1/chat/completions` | `POST /v1/responses` |
| Input shape | `messages: [{role, content, tool_calls, tool_call_id, ...}]` | `input: string \| [InputItem...]` |
| Output shape | `choices: [{message: {role, content, tool_calls}, finish_reason}]` | `output: [Item...]` (heterogeneous typed array) |
| Assistant tool call | `{tool_calls: [{id, type:"function", function:{name, arguments}}]}` inside an assistant message | `{type:"function_call", call_id, name, arguments}` top-level output item |
| Tool result | `{role:"tool", tool_call_id, content}` message | `{type:"function_call_output", call_id, output}` top-level input item |
| Reasoning | Hidden, or surfaced as out-of-band `reasoning_content` (Gemini) / `thinking` blocks (Anthropic-style) | First-class `{type:"reasoning", summary:[...], content:[...], encrypted_content?}` items in both input and output |
| Reasoning carry-across-turn | Provider-specific carrier (we use the `extra_content.google.thought_signature` Extras trick for Gemini) | Echo whole `reasoning` items back as input items, including opaque `encrypted_content` when `store:false` |
| Streaming | OpenAI SSE: a sequence of `chat.completion.chunk` objects with `choices[].delta` slices | Typed SSE: `event: response.created`, `event: response.output_item.added`, `event: response.output_text.delta`, `event: response.function_call_arguments.delta`, `event: response.completed`, plus ~20 more event types |
| Server-side state | None | Optional via `previous_response_id` + `store:true` (default) |
| Forced tool + reasoning | Disallowed on GPT-5.5 / o-series | Supported |
| Built-in tools | Function-only | Function + hosted tools (`file_search`, `web_search_preview`, `computer_use_preview`, `code_interpreter`, `image_generation`) |
| Errors | Top-level `error` object with `type/code/message` | Same envelope shape; same HTTP status semantics |

The structural delta: **`model/wire` becomes a multi-shape
provider-wire package**, not "the ChatCompletion wire." The existing
package shape (ChatCompletion + `Extras` carrier for provider
quirks) is the right shape for Gemini OpenAI-compat / Ollama /
OpenAI ChatCompletion — it does not cover Responses.

### Why the carrier abstraction needs to change at the same time

Today's reasoning carry-across-turn carrier is
`agentic.ToolCall.Metadata[MetadataKeyGoogleThoughtSignature]`. Two
structural problems make this unsustainable as soon as a second
provider arrives:

1. **The provider-named key leaks wire-shape upward.** Every
   trajectory consumer, trace renderer, and replay tool currently
   has to know that Gemini-ness means looking for
   `google_thought_signature`. Add `openai_reasoning_item` and every
   consumer site has two provider-specific branches. Add a third
   provider and it's three. The carrier is named after the wire
   field, not the semantic role.
2. **The attachment point doesn't generalize.** Gemini's signature
   attaches to a tool call. OpenAI's reasoning items are *standalone
   siblings in the output array* — there's no tool call to bolt
   them onto. Anthropic's `thinking` blocks (future) are content
   parts inside the assistant message — a third attachment shape.

The semantic role *is* stable across providers: "opaque blob the
model wants echoed on the next turn for reasoning continuity." The
wire shape is not. The right abstraction is at the role layer.

This change is **breaking**. We are pre-1.0 and own every consumer
in the C360Studio org (semspec, semteams, semconnect), so the
calculus per [[feedback_greenfield_cross_product_break_now]] is:
take the break now, when N=2 is arriving and the leverage is one
architectural decision across both providers. Wait for N=3 and we
ship two per-provider paths and have two migrations to do.

## Decision

### D1. Add a sibling package `model/wire/responses/`

Package layout:

```
model/wire/
  client.go          (unchanged) — ChatCompletion client
  types.go           (unchanged) — ChatCompletion types
  types_message.go   (unchanged) — shared Message/ToolCall (used by ChatCompletion path only)
  stream.go          (unchanged) — ChatCompletion SSE
  errors.go          (unchanged) — APIError (reused by Responses)
  responses/
    doc.go           NEW — package doc
    client.go        NEW — Responses client (/v1/responses)
    types.go         NEW — Request, Response, ~10 InputItem variants, ~10 Item variants
    types_reasoning.go NEW — ReasoningItem subshape + helpers
    stream.go        NEW — typed-event SSE parser + output accumulator
    errors.go        NEW or re-export — Responses-side error decoding
```

The package is shape-organized (matches existing `model/wire`
convention) and pre-declares the multi-shape future. If a third
shape (Anthropic Messages) ever lands, it becomes
`model/wire/anthropic/` (or similar) by the same pattern.

### D2. Stateless mode: `store: false` + echo reasoning items per turn

The Responses API supports two modes:
- **Stateful** (`store: true`, default): the response is persisted
  on OpenAI's side. Next turn passes `previous_response_id`; OpenAI
  reconstructs the conversation.
- **Stateless** (`store: false`): caller is responsible for the
  full message history. Reasoning items must be echoed back as
  input items, including the opaque `encrypted_content` field where
  present.

**Decision: stateless.**

Two reasons:

1. **State ownership stays in semstreams.** Loop state lives in
   the `AGENT_LOOPS` KV bucket, with trajectory in the graph and
   bulky content in ObjectStore. Adopting `previous_response_id`
   couples that state model to OpenAI's retention policy and
   breaks the KV-twofer architectural invariant (the write IS the
   event; replay from any revision). With server-side state we
   can't replay without a side-channel record of
   `previous_response_id` per turn, which we'd have to put in… KV.
   So stateless is a wash on storage and a win on coupling.
2. **Reasoning-item echo is a known pattern.** We already do this
   for Gemini via the `thought_signature` carrier. Responses
   formalizes it (echo whole reasoning items rather than a single
   field), but the per-step rule is the same: capture on response,
   attach on next turn's request.

Cost accepted: stateless mode adds bytes to every request (echoed
reasoning items). Same cost we pay for Gemini and we have not found
it problematic. A metric on `bytes-per-request` lands as part of
Phase 3 so operators can observe the cost in production.

### D3. Unified `agentic.ReasoningRecord` carrier, decided now

Introduce a provider-neutral reasoning-record type and retire the
provider-named key `MetadataKeyGoogleThoughtSignature`. Land both
changes in the same BREAKING phase as the Responses package
non-streaming client (D1).

Type sketch (canonical — sister-agent implements to this shape):

```go
// agentic/reasoning.go

// ReasoningRecord is the provider-neutral carrier for opaque
// reasoning state that must be echoed back on the next turn.
// Captured on response, attached to the next request. Provider-
// specific reshape happens at the adapter seam — loop code stays
// shape-neutral.
type ReasoningRecord struct {
    // Provider names the carrier provider ("google", "openai",
    // future). Used by the adapter to decide on-the-wire
    // reconstruction.
    Provider string `json:"provider"`

    // ItemID is the provider-assigned identity for this record.
    // Used for cross-turn echo when the provider needs it (OpenAI
    // uses; Gemini does not — its signatures are carried per-
    // tool-call, not by id).
    ItemID string `json:"item_id,omitempty"`

    // SummaryText is a human-readable description of the
    // reasoning, when the provider exposes one. Safe to log. Used
    // for trajectory and operator-facing trace.
    SummaryText string `json:"summary_text,omitempty"`

    // Opaque is the provider-specific blob that must be echoed
    // back verbatim. Treat as bytes; do not parse, do not log in
    // full.
    //  - Gemini: the base64 thought_signature string (bytes of
    //    UTF-8)
    //  - OpenAI: encrypted_content blob (when store:false)
    //  - Anthropic (future): thinking block content
    Opaque []byte `json:"opaque,omitempty"`

    // CarrierKind names the structural attachment constraint on
    // the wire. Adapters use this to reshape correctly. The set
    // is closed: unknown values are an authoring error and must
    // fail validation, not silently pass.
    CarrierKind ReasoningCarrierKind `json:"carrier_kind"`

    // ToolCallID is set when CarrierKind == ReasoningCarrierToolCall.
    // The signature belongs with this specific tool call; the
    // adapter re-binds them on send.
    ToolCallID string `json:"tool_call_id,omitempty"`
}

type ReasoningCarrierKind string

const (
    // ReasoningCarrierToolCall: blob attaches to a specific tool
    // call on the wire. Used by Gemini (thought_signature per
    // tool_call).
    ReasoningCarrierToolCall ReasoningCarrierKind = "tool_call"

    // ReasoningCarrierStandaloneItem: blob is a sibling output
    // item with no attachment to messages or tool calls. Used by
    // OpenAI Responses (reasoning items in the output array).
    ReasoningCarrierStandaloneItem ReasoningCarrierKind = "standalone_item"

    // ReasoningCarrierAssistantContent: blob is a content part
    // inside the assistant message. Reserved for Anthropic's
    // thinking blocks when/if we take that on.
    ReasoningCarrierAssistantContent ReasoningCarrierKind = "assistant_content"
)
```

Field add on `agentic.ChatMessage`:

```go
type ChatMessage struct {
    // ... existing fields ...

    // ReasoningRecords are provider-opaque reasoning blobs
    // captured from the model's response, to be echoed back on
    // subsequent turns. Loop owns cross-turn propagation;
    // adapters reshape into wire format at the seam. See
    // ReasoningRecord.
    ReasoningRecords []ReasoningRecord `json:"reasoning_records,omitempty"`
}
```

Adapter responsibilities (capture/echo asymmetry by provider):

| Adapter | Capture (response → ReasoningRecord) | Echo (ReasoningRecord → wire) |
|---|---|---|
| Gemini (ChatCompletion + Extras) | Read `extra_content.google.thought_signature` from each tool_call in the response; emit one `ReasoningRecord{Provider:"google", CarrierKind:ToolCall, ToolCallID:..., Opaque:[]byte(sig)}` per tool call that has one. | On each outgoing tool_call, look up matching `ReasoningRecord` by ToolCallID; write `extra_content.google.thought_signature` back into the wire ToolCall.Extras. |
| OpenAI Responses | For each `{type:"reasoning"}` output item, emit one `ReasoningRecord{Provider:"openai", CarrierKind:StandaloneItem, ItemID:item.id, Opaque:item.encrypted_content, SummaryText:summary}`. | On request build, emit each `ReasoningRecord` (filtered to `Provider:"openai"`) as a `{type:"reasoning", id, encrypted_content, ...}` input item, preserving order relative to other input items per the provider's per-step echo rule. |
| OpenAI ChatCompletion | No-op — endpoint does not surface reasoning items. | No-op. |
| Ollama | No-op (currently). | No-op (currently). |

### D4. Adapter strategy: parallel `ResponsesAdapter` interface

Today's adapter interface is structured around a ChatCompletion
lifecycle:

```go
type Adapter interface {
    Name() string
    NormalizeRequest(*wire.ChatCompletionRequest)
    NormalizeMessages([]wire.Message) []wire.Message
    NormalizeStreamDelta(wire.ToolCall, int) int
    NormalizeResponse(*wire.ChatCompletionResponse)
}
```

Responses gets its own interface with corresponding hooks operating
on `*responses.Request` / `[]responses.InputItem` / typed-event
stream / `*responses.Response`. One implementation:
`OpenAIResponsesAdapter`. The hooks are mostly no-ops; the
non-trivial work lives in capture/echo of `ReasoningRecord` per the
adapter responsibilities table above.

Rationale: OpenAI is the only provider on Responses today. Adding
a second `ResponsesAdapter` only makes sense when a second provider
implements that shape (unlikely — the API is the spec). Generic
shape-tagged hooks would mix shapes in every adapter signature; the
parallel-interface approach keeps each adapter's types clean.

### D5. Backend dispatch: `WireBackend = "responses"` alongside existing values

The `model.EndpointConfig.WireBackend` field already supports
discriminator values (`""` / `"sdk"` defaults to SDK, `"wire"`
selects the wire-native ChatCompletion path per ADR-037). Add
`"responses"` as a third value that selects the Responses path.

The `agentic-model.Client` becomes a tagged dispatch:

```go
// In NewClient
switch endpoint.WireBackend {
case "responses":
    // build c.responsesClient *responses.Client
case "wire":
    // build c.wireClient *wire.Client (existing)
case "", "sdk":
    // SDK path (existing)
}

// In chatCompletion dispatch
switch {
case c.useResponsesBackend():
    return c.chatCompletionResponses(ctx, req)
case c.useWireBackend():
    return c.chatCompletionWire(ctx, req)
default:
    return c.chatCompletionSDK(ctx, req)
}
```

Selection is per-endpoint, operator-configurable. semspec opts in
for the OpenAI hybrid-registry endpoints; other endpoints (Gemini,
non-codex OpenAI, Ollama) stay on their current backends.

## Phasing

| Phase | Scope | Tag class | Gate |
|---|---|---|---|
| **1** | `agentic.ReasoningRecord` + `ChatMessage.ReasoningRecords` field + Gemini adapter migration off `MetadataKeyGoogleThoughtSignature` (delete the constant) + `model/wire/responses/` package types + non-streaming client + round-trip tests against captured fixtures. | **BREAKING** | Full e2e green per CLAUDE.md hard rule; pre-tag sweep with `-tags=integration` AND `-tags=live_llm` per [[feedback_pre_tag_sweep_includes_build_tags]]; sister-repo coordination — semspec / semteams / semconnect bump together on tag land. |
| **2** | Responses streaming: typed-event SSE parser + output-index accumulator + per-item dispatch + chunk-handler parity with the ChatCompletion path. Golden-fixture tests. | ADDITIVE | Standard test gates; live_llm tag for the captured-fixture parity test. |
| **3** | agentic-model integration: `WireBackend = "responses"` dispatch + `OpenAIResponsesAdapter` (capture/echo of `ReasoningRecord{CarrierKind:StandaloneItem}`) + `client_responses.go` mirroring `client_wire.go` (retry, throttle, metrics, chunk-handler). Per-endpoint opt-in. `bytes-per-request` metric for echo-cost observation. | ADDITIVE | Pre-tag e2e on agentic tier; live_llm parity test against captured fixtures. |
| **4** | Multi-turn parity verification: cross-turn `ReasoningRecord` echo works in the loop's actual multi-turn flow against both Gemini (regression: same behavior through the new type) and OpenAI (new path). Documentation: update [ADR-037](037-self-hosted-llm-wire-package.md) soak clock with Responses entry. | ADDITIVE | live_llm test pass for both providers; soak clock entry. |
| **5 (deferred until justified)** | Hosted tools: `file_search`, `web_search_preview`, `code_interpreter`, etc. Not opened until a concrete consumer asks. | — | Out of scope of this ADR. |

Phase 1 is the only BREAKING phase. Phases 2-4 are additive on top.
The deliberate scope expansion at Phase 1 (carrier abstraction
introduced in the same tag as the second consumer arriving) is the
leverage point per [[feedback_greenfield_cross_product_break_now]].

## Consequences

### Positive

- **Trajectory consumers stay provider-neutral.** Trace renderers,
  replay tools, debug surfaces iterate `ReasoningRecord` without
  `if-google-then... if-openai-then...` branches. New providers
  slot in by adding a third `Provider` string and a third adapter,
  not by editing consumer sites.
- **semspec unblocked.** Hybrid-registry OpenAI calls get Codex /
  GPT-5.5 / o-series with `tool_choice` + `reasoning_effort`
  combined, which the ChatCompletion path forbids.
- **Multi-shape wire layer is honest about its future.** The
  `model/wire/responses/` package layout pre-declares that the
  package family is organized by wire shape, not by provider. A
  third shape (Anthropic Messages, eventual SSE-streaming
  variants) lands by the same pattern without a package-layout
  redesign.
- **One architectural decision spans two consumers.** Gemini
  migration to `ReasoningRecord` + OpenAI adoption of
  `ReasoningRecord` happen in one PR cycle. No second migration
  later.

### Negative / accepted costs

- **Phase 1 is BREAKING.** Sister-repo bump required across
  semspec / semteams / semconnect on the same tag. Workflow
  precedent: ADR-042 publisher-mode rollout (six landings in one
  cycle).
- **Saved trajectories from pre-cut tags will not reload after the
  change.** Trajectories are operator debug artifacts, not durable
  contract — we have zero consumers that replay archived
  trajectories across `agentic` schema versions. Discipline: tag
  changelog notes the schema bump; no shim code carried.
- **`store: false` mode adds bytes to every Responses request.**
  Echoed reasoning items can be sizable. Phase 3 lands a metric
  for operator observability; if cost surfaces as a real concern,
  Phase 5+ can revisit `store: true` per-endpoint opt-in.
- **Two `Client` types in the agentic-model package** (wire +
  responses + SDK = three dispatch paths). Tagged-union plumbing
  at `agentic-model.NewClient` and the chatCompletion dispatch.
  Modest churn at one seam; the cost is bounded.

### Discipline gates this ADR triggers

- [[feedback_polymorphic_config_needs_json_roundtrip_test]] — JSON
  round-trip test for `ReasoningRecord` and the new `ChatMessage`
  shape (Phase 1 acceptance criterion).
- [[feedback_pre_tag_sweep_includes_build_tags]] — `go vet
  -tags=integration` AND `-tags=live_llm` before the Phase 1 tag.
- [[feedback_e2e_required_for_breaking_changes]] — full e2e green
  before Phase 1 tag; both `cmd/semstreams/main.go` and
  `cmd/e2e-semstreams/main.go` migrated to the new shape.
- [[feedback_greenfield_cross_product_break_now]] — applied at
  decision D3 (skip the compat shim, one tag flip across sister
  repos).
- [[feedback_reactive_patches_vs_engine_completion]] — framing
  applies at Phase 1 scope expansion (one deliberate completion
  cycle vs three per-provider arrivals).
- [[feedback_verify_main_go_wire_for_sister_asks]] — verify
  `OpenAIResponsesAdapter` is wired in both binaries before tag.
- [[feedback_never_retag]] — applies to the Phase 1 tag like any
  other; module proxy pins on first fetch.

## Alternatives Considered

### A. Same-package multi-shape (`model/wire/types_responses.go`)

Add the Responses types to `model/wire` alongside `types.go` rather
than as a sibling package.

- **Why rejected.** Package doc would have to start with "this
  package speaks two unrelated wire shapes," which is exactly the
  framing that lets ad-hoc additions sneak in. Type names get
  prefix-y (`ResponsesRequest`, `ResponsesItem`, `ResponsesStream`)
  to avoid collisions with intuitive ChatCompletion names. The
  boundary that prevents future shape mixing is just developer
  discipline; the sibling-package layout makes the boundary
  structural.

### B. Top-level sibling package (`model/openai_responses/`)

Put Responses in a provider-named top-level package rather than a
shape-named subdirectory.

- **Why rejected.** Implies the wire layer is organized by
  provider. Inconsistent with `model/wire` which is organized by
  *shape* (currently used by OpenAI, Gemini OpenAI-compat, and
  Ollama). Future provider-shape additions would have to pick
  provider-naming or shape-naming and the layout would drift.

### C. Server-side state (`store: true` + `previous_response_id`)

Use OpenAI's server-managed conversation state instead of echoing
reasoning items per turn.

- **Why rejected.** Couples loop state ownership to OpenAI's
  retention policy. Breaks the KV-twofer invariant (the write IS
  the event; replay from any revision). We'd still have to record
  `previous_response_id` per turn in our own KV, so the storage
  cost is the same — and we'd add coupling that doesn't exist on
  the stateless path. The bytes-per-request cost of echo is the
  same cost we already pay for Gemini and have not found
  problematic.

### D. Per-provider `MetadataKey` (defer unified type)

Add `MetadataKeyOpenAIReasoningItem` alongside
`MetadataKeyGoogleThoughtSignature`; don't introduce
`ReasoningRecord` yet.

- **Why rejected.** Defers the architectural problem to N=3
  arrival, when we'll have shipped two per-provider paths and need
  to migrate both. The leverage of one architectural decision
  across N=1 existing carrier + N=2 new carrier is unique to this
  moment. Also the attachment-point structural problem doesn't
  cleanly resolve: OpenAI reasoning items aren't attached to tool
  calls, so the `ToolCall.Metadata` carrier is structurally wrong
  for them — bolting them on requires inventing a separate
  attachment location, which proliferates the inconsistency.

### E. Generic shape-tagged adapter interface

Collapse the existing `Adapter` and new `ResponsesAdapter`
interfaces into a single generic interface with shape-discriminator
hooks.

- **Why rejected.** Mixes shapes in every adapter signature. Most
  adapters today (Gemini, OpenAI ChatCompletion, Ollama) only
  speak one shape; forcing them to implement Responses-shape
  no-ops adds noise without clarity. The parallel-interface
  approach keeps each adapter's types clean.

## Open implementation questions

These need answers in the Phase 1 implementation cut, but are not
load-bearing on the architectural decision:

1. **What does `response_format` do in Responses?** Responses has
   its own `text.format` parameter that supersedes
   `response_format`. Confirm by experiment whether the same
   JSONSchema we send to ChatCompletion works under Responses; if
   not, the `agenticResponseFormatToResponses` translator needs to
   reshape. Likely a translator-side concern; doesn't affect the
   adapter or carrier design.
2. **Embedded `developer` role.** Responses input items support a
   `role: "developer"` for system-prompt-class messages. Our
   agentic loop emits `role: "system"`. **Decision direction
   (preferred): explicit translation in the adapter
   (`system → developer`) with a test that asserts the wire
   body**, rather than relying on OpenAI's compat. Avoid silent
   semantic drift.
3. **`store: false` echo cost telemetry.** Phase 3 ships a
   `bytes-per-request` metric. Decide on the metric name and label
   set as part of Phase 3 — recommend
   `agentic_model_request_bytes{backend="responses",model=...}`
   with a corresponding histogram for distribution.
4. **Sister-repo coordination cadence.** Phase 1 is BREAKING and
   requires semspec / semteams / semconnect to bump together. Same
   one-flip workflow as ADR-042 publisher-mode. Phase 3 (the
   actual `WireBackend = "responses"` config surface) lands
   additive on top — that's the cut semspec opts in at
   per-endpoint.

## Sister-agent handoff

The proposal doc at
[`docs/proposals/openai-responses-wire-support.md`](../proposals/openai-responses-wire-support.md)
has the research-record version of this ADR with more discursive
treatment of options and rationale. It is **redundant with this ADR
as a working spec** — implement to this ADR. The proposal stays as
the working artifact for posterity.

### Reading list (in order)

1. This ADR (canonical decision record).
2. [ADR-037](037-self-hosted-llm-wire-package.md) end-to-end. The
   forcing-function structure, the Extras-carrier rationale, and
   the soak gate are all reusable framing.
3. `model/wire/client.go`, `model/wire/types.go`,
   `model/wire/stream.go`, `model/wire/types_message.go`. The
   structural template for the new `model/wire/responses/` files.
4. `processor/agentic-model/client_wire.go` end-to-end. The
   template for `client_responses.go`: same retry loop, same
   throttle, same metric hooks, same chunk-handler contract — only
   the per-attempt dispatch differs.
5. `processor/agentic-model/adapter_gemini.go` to understand the
   current `thought_signature` capture/echo flow (you will rewrite
   it as part of Phase 1).

### Pre-Phase-1 prep checklist

- [ ] Run `grep -rn MetadataKeyGoogleThoughtSignature --include="*.go"`
  and inventory every call site. Each is a Phase 1 deletion +
  replacement with the new `ReasoningRecord` path.
- [ ] Capture live OpenAI Responses fixtures against at least one
  Codex-class model and one GPT-5.5 endpoint. Golden-fixture
  round-trips are how we verified Gemini's quirks and they're the
  only way to ground the Phase 1 / Phase 2 unit tests against the
  real wire shape. Stash under `model/wire/responses/testdata/`.
- [ ] Confirm `cmd/semstreams/main.go` AND
  `cmd/e2e-semstreams/main.go` both register the new adapter and
  honor the new `WireBackend = "responses"` value before tagging
  Phase 1. Per
  [[feedback_e2e_required_for_breaking_changes]] — the
  registry-singleton story is the cautionary tale on
  missing-binary migration.
- [ ] Coordinate the sister-repo bump (semspec / semteams /
  semconnect) on the Phase 1 tag landing. Workflow precedent is the
  ADR-042 publisher-mode rollout (six landings in one cycle). Open
  a tracking issue in each repo before the Phase 1 PR lands so the
  bump is queued.

### Phase 1 acceptance criteria

- New types: `agentic.ReasoningRecord`, `agentic.ReasoningCarrierKind`
  (with three constants), `agentic.ChatMessage.ReasoningRecords`
  field.
- `MetadataKeyGoogleThoughtSignature` constant deleted.
- Gemini adapter (`adapter_gemini.go` + capture/echo sites in
  `client_wire.go`) migrated to the new carrier. Existing
  multi-turn Gemini tests pass without modification to their loop
  expectations (only test setup changes — the production behavior
  is identical).
- `model/wire/responses/` package implements non-streaming
  `Client.Responses(ctx, *Request) (*Response, error)` + the full
  Request/Response/Item/InputItem type set with golden-fixture
  round-trip tests.
- `agentic.ReasoningRecord` JSON round-trip test passes per
  [[feedback_polymorphic_config_needs_json_roundtrip_test]].
- Pre-tag sweep clean: `go vet -tags=integration`, `go vet
  -tags=live_llm`, `task lint`, `task test` (with `-race`),
  `task test:integration`, `task schema:generate` no diffs.
- Full e2e tier set green: `task e2e:core`, `task e2e:structural`,
  `task e2e:statistical`, `task e2e:semantic`, `task e2e:agentic`.
- Both binaries (`cmd/semstreams`, `cmd/e2e-semstreams`) wired and
  verified by `grep` for the new adapter registration.
- Tag changelog notes the BREAKING change explicitly with
  migration guidance for the sister repos.

## References

- [ADR-037: Self-Hosted LLM Wire-Format Package](037-self-hosted-llm-wire-package.md) — parent ADR; same package family.
- [ADR-042: OASF Taxonomy Adoption](042-oasf-taxonomy-adoption.md) — sister-repo rollout workflow precedent.
- [`docs/proposals/openai-responses-wire-support.md`](../proposals/openai-responses-wire-support.md) — research record.
- [`feedback_greenfield_cross_product_break_now.md`](../../../../.claude/projects/-Users-coby-Code-c360-semstreams/memory/feedback_greenfield_cross_product_break_now.md) — discipline memory grounding the Phase 1 BREAKING scope decision.
- OpenAI Responses API documentation (external) — capture fixtures
  from the real endpoint per the prep checklist before Phase 1
  starts; the docs evolve and the fixtures are the source of truth
  for unit-test parity.
