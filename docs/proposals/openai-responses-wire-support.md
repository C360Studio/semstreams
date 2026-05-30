# OpenAI Responses API — Wire Support Research

**Status:** Research record — 2026-05-30. **Superseded as the working
spec by [ADR-051](../adr/051-openai-responses-wire-support.md)** —
sister-agent implements to the ADR; this doc remains as the
research/working artifact that informed the decisions. Companion to
[ADR-037](../adr/037-self-hosted-llm-wire-package.md).

## Forcing function

semspec hit a hard wall on its hybrid registry path: **the OpenAI Chat
Completions endpoint refuses `tool_choice` + `reasoning_effort` together
on GPT-5.5 and the o-series**. The combination is explicitly supported
only on the `/v1/responses` endpoint. semspec needs both — forced tool
to bind dev-related calls to a specific schema, and `reasoning_effort`
to get useful behavior out of the model class — so the chat-completions
path is non-viable for that consumer regardless of how cleanly we wire it.

Codex-family models (`codex-mini`, `codex` previews, `gpt-5-codex` when
it lands) only ship on the Responses endpoint. semspec's hybrid registry
is OpenAI-routed for dev-related calls, so Codex is the natural target
model class for that path. Without Responses, semspec is forced down to
GPT-4o-class for the dev calls and loses the reasoning surface entirely.

This is **not anticipatory**. It is a named consumer with a named
constraint blocking a named call site. That satisfies the
"forcing-function check" gate from the prior turn —
[[feedback_reactive_patches_vs_engine_completion]] applies the other
direction: if we say yes to Responses, we should scope it as
"complete the wire layer's provider-shape support" rather than "patch
in one more thing," so we don't end up adding a third shape under
ad-hoc framing six months from now.

## What the Responses API actually is (delta from Chat Completions)

The Responses API is not a quirky variant of Chat Completions. It is a
different request/response shape with a different message model and
different streaming protocol. The transport is still HTTPS+JSON, but
the wire types do not overlap.

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
| Built-in tools | Function-only | Function + hosted tools (`file_search`, `web_search_preview`, `computer_use_preview`, `code_interpreter`, `image_generation`) — we wouldn't enable these initially |
| Errors | Top-level `error` object with `type/code/message` | Same envelope shape; same HTTP status semantics |

**Key insight: this is not "add an adapter."** The `model/wire` package
today is structurally OpenAI Chat Completions with an Extras carrier
that lets us thread Gemini-shape quirks (`thought_signature`) through
the same types. Responses changes the request/response top-level shape;
no amount of Extras carrying solves it. It needs its own request
struct, its own response struct, its own SSE event parser, its own
streaming accumulator, and its own adapter chain.

That is the architectural delta worth naming: **`model/wire` becomes a
multi-shape provider-wire package**, not "the ChatCompletion wire."

## Package-shape options

Three framings. Pick one in the ADR before any code lands.

### Option A — Sibling package: `model/wire/responses`

```
model/
  wire/
    client.go          ChatCompletion client (unchanged)
    types.go           ChatCompletion request/response (unchanged)
    types_message.go   shared Message/ToolCall (unchanged)
    stream.go          ChatCompletion SSE (unchanged)
    responses/
      doc.go
      client.go        Responses client
      types.go         InputItem, Item, ReasoningItem, FunctionCallItem, ...
      stream.go        Typed-event SSE parser, output accumulator
      errors.go        Re-uses model/wire.APIError? Or its own?
```

- **Pro**: Zero churn on existing wire callers. Gemini, OpenAI-ChatCompletion, and Ollama continue to work as-is. Responses lives behind an isolated import path; nothing about its shape leaks into ChatCompletion types.
- **Pro**: Mirrors the eventual reality. Anthropic's Messages API is a third top-level shape; if we ever take that on, it becomes `model/wire/anthropic` (or similar) by the same pattern. The package layout pre-declares that intent.
- **Con**: Two `Client` types in adjacent packages with similar surface area. The `agentic-model.Client` becomes a tagged union (`wireClient *wire.Client` OR `responsesClient *responses.Client`). Modest plumbing churn at the seam.

### Option B — Same package, two clients side-by-side

```
model/wire/
  client.go            ChatCompletion client
  client_responses.go  Responses client
  types.go             ChatCompletion types
  types_responses.go   Responses types
  stream.go            ChatCompletion SSE
  stream_responses.go  Responses SSE
```

- **Pro**: One package, one Extras helper, one `APIError`, one set of cache types. Less file-tree ceremony.
- **Con**: Package doc has to start with "this package speaks two unrelated wire shapes," which is exactly the framing that lets ad-hoc additions sneak in. The boundary that prevents future shape mixing is just developer discipline.
- **Con**: Type names get prefix-y (`ResponsesRequest`, `ResponsesItem`, `ResponsesStream`) to avoid collisions. The OpenAI-canonical names (`Response`, `Item`) collide with intuitive ChatCompletion names.

### Option C — Top-level sibling: `model/openai_responses`

```
model/
  wire/               ChatCompletion (unchanged)
  openai_responses/   Responses-only package
```

- **Pro**: Names the provider in the package path. Honest about the fact that Responses today is OpenAI-only; nobody else implements that shape.
- **Con**: Implies the wire layer is organized by provider. Inconsistent with `model/wire` which is organized by *shape* (and is currently used by OpenAI, Gemini OpenAI-compat, and Ollama). Future provider-shape additions would have to pick provider-naming or shape-naming and the layout would drift.

**Recommendation: Option A.** Shape-organized layout, clean isolation,
honest about the multi-shape future. Cost is one extra directory and a
tagged-union seam in `agentic-model.Client` — small relative to the
clarity gain.

## Adapter strategy

Today's adapter interface is structured around a ChatCompletion lifecycle:

```go
type Adapter interface {
    Name() string
    NormalizeRequest(*wire.ChatCompletionRequest)
    NormalizeMessages([]wire.Message) []wire.Message
    NormalizeStreamDelta(wire.ToolCall, int) int
    NormalizeResponse(*wire.ChatCompletionResponse)
}
```

Responses needs a parallel hook set with different types
(`*responses.Request`, `[]responses.InputItem`, the typed-event stream).
Two reasonable options:

1. **Parallel `ResponsesAdapter` interface**: cleanest types, but
   forces every shape decision twice when an adapter wants to do the
   same thing on both. Realistically, OpenAI is the only provider on
   Responses today, so this is one adapter (`OpenAIResponsesAdapter`)
   that's almost entirely no-ops, similar to today's `OpenAIAdapter`
   for chat completions.
2. **Generic `Adapter` with shape-tagged hooks**: collapses the
   interface but mixes shapes in every adapter signature. Worse fit
   given that providers don't currently cross shapes (OpenAI on
   Responses, Gemini on ChatCompletion-compat, Ollama on
   ChatCompletion).

**Recommendation: parallel `ResponsesAdapter`.** One adapter for now
(`OpenAIResponsesAdapter`, mostly no-ops). Add a second only when a
second provider implements the Responses shape — likely never, given
OpenAI's API is the spec.

## Stateless vs stateful: pick stateless

The Responses API supports two modes:

- **Stateful** (`store: true`, default): the response is persisted on
  OpenAI's side. Next turn passes `previous_response_id`; OpenAI
  reconstructs the conversation. No need to echo reasoning items.
- **Stateless** (`store: false`): caller is responsible for the full
  message history. Reasoning items must be echoed back as input items,
  including the opaque `encrypted_content` field where present.

**Recommendation: stateless.** Two reasons:

1. **State ownership stays in semstreams.** Our loop state lives in
   the `AGENT_LOOPS` KV bucket, with trajectory in the graph and
   bulky content in ObjectStore. Adopting `previous_response_id`
   couples that state model to OpenAI's retention policy and breaks
   the KV-twofer architectural invariant (the write IS the event;
   replay from any revision). With server-side state we can't replay
   without a side-channel record of `previous_response_id` per turn,
   which we'd have to put in… KV. So stateless is a wash on storage
   and a win on coupling.
2. **Reasoning-item echo is a known pattern.** We already do this for
   Gemini via the `thought_signature` carrier. Responses formalizes
   it (echo whole reasoning items rather than a single field), but
   the per-step rule is the same: capture on response, attach on next
   turn's request.

Trade-off accepted: stateless mode requires echoing reasoning items on
every turn, which adds bytes to the request. This is the same cost we
pay for Gemini and we have not found it problematic.

## Tool-call shape mapping

The biggest fanout in the agentic-model adapter is the tool-call
translation. Today:

```
agentic.ToolCall ─→ wire.ToolCall ─→ {tool_calls:[{id, function:{name, arguments}}]}
                                        in an assistant message
```

For Responses:

```
agentic.ToolCall ─→ responses.FunctionCallItem ─→ {type:"function_call", call_id, name, arguments}
                                                    as a top-level output item
```

Tool result:

```
agentic.ToolResult ─→ wire.Message{Role:"tool", ToolCallID, Content}
agentic.ToolResult ─→ responses.FunctionCallOutputItem{type:"function_call_output", call_id, output}
```

The `agentic.ToolCall` and `agentic.ToolResult` types should stay
shape-neutral. The translation lives entirely inside the Responses
client path (mirroring the ChatCompletion path in `client_wire.go`).
No changes to the loop-side contracts here.

**However, the reasoning carry-across-turn carrier needs an
architectural change** — see the next section. The current
`MetadataKeyGoogleThoughtSignature` approach (provider-named key on
`ToolCall.Metadata`) does not survive a second provider, and Responses
is the second provider arriving.

## Reasoning carry-across-turn: unified `ReasoningRecord` (Phase 1)

This is a **breaking change** to `agentic.ChatMessage` and the only
non-additive piece of the Responses work. It must land in Phase 1
alongside the Responses package, not deferred.

### Why a unified type, decided up front

Today's carrier (`agentic.ToolCall.Metadata[MetadataKeyGoogleThoughtSignature]`)
has two structural problems that the addition of OpenAI Responses makes
unignorable:

1. **The provider-named key leaks wire-shape upward.** Every trajectory
   consumer, trace renderer, and replay tool currently has to know
   that Gemini-ness means looking for `google_thought_signature`. Add
   `openai_reasoning_item` and every consumer site has two
   provider-specific branches. Add a third provider (eventually) and
   it's three. The carrier is named after the wire field, not the
   semantic role.
2. **The attachment point doesn't generalize.** Gemini's signature
   attaches to a tool call. OpenAI's reasoning items are *standalone
   siblings in the output array* — there's no tool call to bolt them
   onto. Bolting them on anyway is a lie about the data model.
   Anthropic's `thinking` blocks (when we eventually take that on)
   are content parts inside the assistant message — a third
   attachment shape.

The semantic role *is* stable across providers: "opaque blob the
model wants echoed on the next turn for reasoning continuity." The
wire shape is not. The right abstraction is at the role layer.

### Why now is the right moment

We have N=1 carrier today (Gemini). Adding OpenAI as N=2 is the
highest-leverage moment to introduce the type: one shipping consumer
+ one being designed = both migrations happen in one architectural
decision. Wait for N=3 (Anthropic, Mistral reasoning, whatever lands
in 2027) and we've shipped two per-provider paths and have two
migrations to do.

This is the preventive form of [[feedback_class_of_bugs_to_invariant]]:
"same shape recurring across 3+ tags = default-flip + invariant"
becomes "same shape arriving for the 2nd time, when we know the 3rd
is coming = invariant now." It avoids the per-tag arrival sprawl that
[[feedback_reactive_patches_vs_engine_completion]] is the cure for.

### Type sketch

```go
// agentic/reasoning.go

// ReasoningRecord is the provider-neutral carrier for opaque reasoning
// state that must be echoed back on the next turn. Captured on response,
// attached to the next request. Provider-specific reshape happens at
// the adapter seam — loop code stays shape-neutral.
type ReasoningRecord struct {
    // Provider names the carrier provider ("google", "openai", future).
    // Used by the adapter to decide on-the-wire reconstruction.
    Provider string `json:"provider"`

    // ItemID is the provider-assigned identity for this record. Used
    // for cross-turn echo when the provider needs it (OpenAI uses;
    // Gemini does not — its signatures are carried per-tool-call).
    ItemID string `json:"item_id,omitempty"`

    // SummaryText is a human-readable description of the reasoning,
    // when the provider exposes one. Safe to log. Used for trajectory
    // and operator-facing trace.
    SummaryText string `json:"summary_text,omitempty"`

    // Opaque is the provider-specific blob that must be echoed back
    // verbatim. Treat as bytes; do not parse, do not log in full.
    //  - Gemini: the base64 thought_signature string (bytes of UTF-8)
    //  - OpenAI: encrypted_content blob (when store:false)
    //  - Anthropic (future): thinking block content
    Opaque []byte `json:"opaque,omitempty"`

    // CarrierKind names the structural attachment constraint on the
    // wire. Adapters use this to reshape correctly. The set is closed:
    // unknown values are an authoring error.
    CarrierKind ReasoningCarrierKind `json:"carrier_kind"`

    // ToolCallID is set when CarrierKind == ReasoningCarrierToolCall.
    // The signature belongs with this specific tool call; the adapter
    // re-binds them on send.
    ToolCallID string `json:"tool_call_id,omitempty"`
}

type ReasoningCarrierKind string

const (
    // ReasoningCarrierToolCall: blob attaches to a specific tool call
    // on the wire. Used by Gemini (thought_signature per tool_call).
    ReasoningCarrierToolCall ReasoningCarrierKind = "tool_call"

    // ReasoningCarrierStandaloneItem: blob is a sibling output item
    // with no attachment to messages or tool calls. Used by OpenAI
    // Responses (reasoning items in the output array).
    ReasoningCarrierStandaloneItem ReasoningCarrierKind = "standalone_item"

    // ReasoningCarrierAssistantContent: blob is a content part inside
    // the assistant message. Reserved for Anthropic's thinking blocks
    // when/if we take that on.
    ReasoningCarrierAssistantContent ReasoningCarrierKind = "assistant_content"
)
```

And on the message:

```go
type ChatMessage struct {
    // ... existing fields ...

    // ReasoningRecords are provider-opaque reasoning blobs captured
    // from the model's response, to be echoed back on subsequent
    // turns. Loop owns cross-turn propagation; adapters reshape into
    // wire format at the seam. See ReasoningRecord.
    ReasoningRecords []ReasoningRecord `json:"reasoning_records,omitempty"`
}
```

### Migration path: clean break, no compat shim

semstreams is pre-1.0 and we own every consumer in the C360Studio
org (semspec, semteams, semconnect). That changes the calculus:

- **Delete `MetadataKeyGoogleThoughtSignature` outright.** No
  deprecation alias, no one-cycle compat read. The constant name +
  the read sites in `client_wire.go` + the test fixtures all go in
  the same PR.
- **No trajectory-replay compat shim.** Saved trajectories from
  pre-cut tags won't reload after the change. Trajectories are
  operator debug artifacts, not durable contract — we have zero
  consumers that replay archived trajectories across `agentic`
  schema versions. The discipline is to publish migration notes in
  the tag changelog, not to carry shim code.
- **Sister-repo coordination is one tag flip.** semspec, semteams,
  semconnect pull the new `agentic` module version together. The
  cross-repo move is the same workflow as ADR-042 publisher-mode
  (six landings in one rollout).

What we DO carry from greenfield-with-breaking-changes discipline:

- **JSON round-trip test** for `ReasoningRecord` and the new
  `ChatMessage` shape per [[feedback_polymorphic_config_needs_json_roundtrip_test]].
- **Pre-tag sweep with build tags** per [[feedback_pre_tag_sweep_includes_build_tags]].
- **Full e2e gate** per [[feedback_e2e_required_for_breaking_changes]] —
  this is a BREAKING change and the framework binary + e2e binary
  both need the new schema before tag.

### Adapter responsibilities

| Adapter | Capture (response → ReasoningRecord) | Echo (ReasoningRecord → wire) |
|---|---|---|
| Gemini (ChatCompletion + Extras) | Read `extra_content.google.thought_signature` from each tool_call in the response; emit one `ReasoningRecord{Provider:"google", CarrierKind:ToolCall, ToolCallID:..., Opaque:[]byte(sig)}` per tool call that has one. | On each outgoing tool_call, look up matching `ReasoningRecord` by ToolCallID; write `extra_content.google.thought_signature` back into the wire ToolCall.Extras. |
| OpenAI Responses | For each `{type:"reasoning"}` output item, emit one `ReasoningRecord{Provider:"openai", CarrierKind:StandaloneItem, ItemID:item.id, Opaque:item.encrypted_content, SummaryText:summary}`. | On request build, emit each `ReasoningRecord` (filtered to `Provider:"openai"`) as a `{type:"reasoning", id, encrypted_content, ...}` input item, preserving order relative to other input items per the provider's per-step echo rule. |
| OpenAI ChatCompletion | No-op — endpoint does not surface reasoning items. | No-op. |
| Ollama | No-op (currently). | No-op (currently). |

The capture/echo asymmetry between providers is exactly why the
unified type carries `CarrierKind` rather than trying to be a single
flat shape: the adapter knows how to convert in both directions for
its own provider, and the loop never has to.

## Streaming events

Chat Completions streams a sequence of `chat.completion.chunk` objects
with `choices[].delta` slices. Responses streams a typed event sequence
where each frame carries an `event:` line plus a JSON payload:

```
event: response.created
data: {"type":"response.created", ...}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"..."}}

event: response.reasoning_summary_text.delta
data: {"type":"...","delta":"Looking at the user's request"}

event: response.output_item.added
data: {"output_index":1,"item":{"type":"message","role":"assistant"}}

event: response.output_text.delta
data: {"delta":"Sure, "}

event: response.output_text.delta
data: {"delta":"I can help."}

event: response.output_item.added
data: {"output_index":2,"item":{"type":"function_call","name":"search"}}

event: response.function_call_arguments.delta
data: {"delta":"{\"query\":\""}

event: response.function_call_arguments.delta
data: {"delta":"foo\"}"}

event: response.completed
data: {"response": {...full response object...}}
```

A streaming accumulator for Responses needs to:

1. Parse SSE frames preserving the `event:` field (the wire package's
   current stream parser drops it because Chat Completions doesn't use
   typed events meaningfully).
2. Maintain an `output_index → Item` map and merge deltas into the
   right item by index.
3. Emit per-item completion events to the existing `chunkHandler`
   contract so trajectory capture and metrics ports stay parity with
   the SDK / ChatCompletion-wire paths.
4. Synthesize a `responses.Response` (the full non-streaming shape)
   from the accumulated state at `response.completed`, so the upward
   conversion to `agentic.AgentResponse` runs through the same path
   as the non-streaming call.

The `response.completed` frame includes the full final response object;
in principle the accumulator can defer to that and ignore the per-token
deltas for correctness — but we want the deltas for chunk-level
metrics and trajectory streaming (first-token-time, etc.), so a real
accumulator with per-event dispatch is the right shape.

## Embeddings

No change. The Embeddings endpoint is shape-compatible across
ChatCompletion-shape providers and is not affected by the Responses
migration. The current `model/wire.Client.Embeddings` path stays. The
Responses client is request/response only.

## Effort scope (rough)

Counting from comparable Gemini-wire shipping work as a baseline.
ADR-037's wire package landed at ~2.8K LoC (production + tests, ADR-037
beta.60 numbers) for the ChatCompletion shape including streaming.
Responses is structurally similar but has more event types and more
output-item variants.

| Area | Estimate | Notes |
|---|---|---|
| `agentic.ReasoningRecord` + carrier kinds | ~150 LoC | Type, kind constants, godoc, JSON tags, validation helpers. Greenfield — no compat shim. |
| `agentic.ChatMessage.ReasoningRecords` field | ~10 LoC | Field add + godoc. |
| Gemini adapter migration off `MetadataKeyGoogleThoughtSignature` | ~100 LoC | Capture from `extra_content.google.thought_signature` into `ReasoningRecord{CarrierKind:ToolCall}`; echo back. Delete the old MetadataKey constant + read sites. |
| `agentic.ReasoningRecord` tests | ~300 LoC | JSON round-trip per [[feedback_polymorphic_config_needs_json_roundtrip_test]], CarrierKind exhaustiveness lint, Gemini capture/echo parity vs the old MetadataKey path. |
| `model/wire/responses` types | ~500 LoC | Request, Response, ~10 InputItem variants, ~10 Item variants, reasoning sub-types, error types (reuse `wire.APIError`). |
| `model/wire/responses` client | ~200 LoC | Mirrors `wire.Client` but with `/v1/responses` and the Responses request/response types. |
| `model/wire/responses` stream | ~500 LoC | Typed-event SSE parser + output-index accumulator + per-item dispatch. Largest single chunk. |
| `model/wire/responses` tests | ~1000 LoC | Golden-fixture round-trips per OpenAI doc example, streaming accumulator tests, error-decode tests, reasoning-item echo tests. |
| `processor/agentic-model/adapter_openai_responses.go` | ~150 LoC | Capture reasoning items into `ReasoningRecord{CarrierKind:StandaloneItem}`; echo back as input items. Other hooks mostly no-ops. |
| `processor/agentic-model/client_responses.go` | ~500 LoC | Mirror of `client_wire.go`: `buildResponsesRequest`, `convertResponsesResponse`, `doSingleAttemptResponses`, `streamResponses`, agentic ↔ responses translators (tool calls, reasoning, response_format). |
| `processor/agentic-model/client.go` plumbing | ~50 LoC | `WireBackend = "responses"` dispatch alongside `"wire"` and `"sdk"`. |
| Tests (full agentic-model path) | ~1500 LoC | `client_responses_test.go` mirrors `client_wire_test.go`; live test gated behind `live_llm` build tag; round-trip parity tests against an OpenAI mock. |
| `cmd/semstreams` + `cmd/e2e-semstreams` wire | ~20 LoC | Per [[feedback_e2e_required_for_breaking_changes]] verify both binaries see the new backend before tagging. |
| Sister-repo migration notes | ~200 LoC docs | semspec / semteams / semconnect changelog entries + the cross-cut module bump procedure. |
| **Total estimate** | **~5.2K LoC** | ~1.8× the Gemini wire effort. The bump from prior ~4.4K estimate is the `ReasoningRecord` introduction + the Gemini-adapter migration. Bundled in Phase 1 deliberately — the leverage comes from doing both migrations as one architectural decision. |

Two non-LoC budget items:

- **Live-test stub corpus**: we need recorded streaming sessions from
  the real OpenAI Responses endpoint to drive the unit tests. Plan one
  round-trip capture session per supported model class (codex, GPT-5,
  o-series).
- **Soak**: ADR-037 mandates a ≥7d soak before the next adapter
  migration and ≥30d before SDK retirement. Responses doesn't compete
  with the SDK path; it's a new endpoint. But the soak gate should
  still apply per [[feedback_e2e_required_for_breaking_changes]] before
  semspec switches its hybrid registry to use it in production.

## Phasing

Mirror ADR-037's phase structure, with one shift: Phase 1 grows to
absorb the `ReasoningRecord` introduction, because the leverage of
deciding once across both providers depends on doing it together.

- **Phase 1 (BREAKING): `ReasoningRecord` + Responses package
  non-streaming.** Introduce `agentic.ReasoningRecord` and the
  `ChatMessage.ReasoningRecords` field. Migrate the Gemini adapter
  off `MetadataKeyGoogleThoughtSignature` to capture/echo through the
  new type. Delete the old constant. Land `model/wire/responses` with
  request, response, error, non-streaming client + round-trip tests
  against captured fixtures. **Single tag, single PR, single
  architectural decision.** Sister-repo coordination is the one tag
  flip — semspec, semteams, semconnect bump together. Full e2e gate.
- **Phase 2 (ADDITIVE): Responses streaming.** Typed-event SSE parser
  + accumulator. Pure package work with golden fixtures. No
  agentic-model wiring.
- **Phase 3 (ADDITIVE): agentic-model integration.** `WireBackend =
  "responses"` routes endpoint config through the new client.
  `OpenAIResponsesAdapter` for capture/echo of reasoning items as
  `ReasoningRecord{CarrierKind:StandaloneItem}`. Live-test gated.
  semspec can opt in per-endpoint after this lands.
- **Phase 4 (ADDITIVE): multi-turn parity verification.** Verify
  cross-turn reasoning echo works in the loop's actual multi-turn
  flow against both Gemini (regression: same behavior through the new
  type) and OpenAI (new path). This is the equivalent of Gemini's
  `thought_signature` work but with the unified carrier already in
  place.
- **Phase 5 (deferred until justified): hosted tools.** `file_search`,
  `web_search_preview`, `code_interpreter`, etc. None of our current
  consumers need these and they have their own state/cost model.

Phase 1 is the only BREAKING phase. Phases 2-4 are additive on top of
it. Phase 1's deliberate scope expansion is justified by the
[[feedback_reactive_patches_vs_engine_completion]] framing — we ship
the carrier abstraction once, not in three per-arrival increments.

## Open questions

1. **What does response_format do in Responses?** Responses has its
   own `text.format` parameter that supersedes `response_format`.
   Need to confirm by experiment whether the same JSONSchema we send
   to ChatCompletion works under Responses; if not, the
   `agenticResponseFormatToResponses` translator needs to reshape.
2. **Embedded `developer` role.** Responses input items support a
   `role: "developer"` for system-prompt-class messages. Our agentic
   loop emits `role: "system"`. Decision: silently translate
   `system → developer` at the seam, or leave `system` and rely on
   OpenAI's compat? Pick the loud option (explicit translation in
   the adapter, with a test that asserts the wire body) to avoid
   silent semantic drift.
3. **`store: false` echo cost.** Reasoning items can be sizable. We
   should land a metric on the bytes-per-request before deciding
   whether to ever opt some endpoints into `store: true` mode (which
   would be a Phase 5+ decision; default stays stateless).
4. **Sister-repo coordination cadence.** Phase 1 is BREAKING and
   requires semspec / semteams / semconnect to bump together. Same
   one-flip workflow as ADR-042 publisher-mode. Phase 3 (the actual
   `WireBackend = "responses"` config surface) lands additive on top
   — that's the cut semspec opts in at per-endpoint.

(Open question #1 from the prior draft — unified `ReasoningRecord` vs
per-provider `MetadataKey` — is **answered**: unified type, decided
now per the discipline at "Reasoning carry-across-turn" above.)

## Recommendation

Yes, build Responses support. The forcing function is real (semspec is
blocked today on a specific call site), the scope is bounded
(~5.2K LoC, one BREAKING phase + three ADDITIVE), and the package
reshape can be clean if we name it correctly up front (Option A:
shape-organized sibling package).

Frame this as **"complete the wire layer's provider-shape support"**
in the ADR: ChatCompletion shape (today) + Responses shape (this work),
plus the unified `ReasoningRecord` carrier that lets the loop stay
provider-neutral as new shapes arrive. Anthropic Messages shape stays
explicitly out of scope unless and until a consumer pulls on it. The
deliberate-completion framing matters per
[[feedback_reactive_patches_vs_engine_completion]]; without it this
reads as another wire-of-the-month and the package will accrete
shapes per-quirk for the next year.

The Phase 1 breaking change (delete `MetadataKeyGoogleThoughtSignature`,
introduce `ReasoningRecord`) is the right call to make at this exact
moment. We are pre-1.0 and own every consumer in the org — that gate
won't be open again. Doing it later means a Phase 1 PR with three
provider-specific carriers + a migration matrix; doing it now means
one carrier + one adapter migration. Breaking changes are never fun,
but the long-term sanity of "trajectory consumers iterate `ReasoningRecord`
without provider-specific branches, forever" is worth one tag of
coordinated cross-repo bumps.

Sister-agent handoff items if they pick this up:

- Read [ADR-037](../adr/037-self-hosted-llm-wire-package.md) end-to-end.
  The forcing-function structure, the Extras-carrier rationale, and
  the soak gate are all reusable framing.
- Read `model/wire/client.go`, `model/wire/types.go`,
  `model/wire/stream.go`. Mirror their structure under
  `model/wire/responses/`.
- Read `processor/agentic-model/client_wire.go` end-to-end. This is
  the template for `client_responses.go`: same retry loop, same
  throttle, same metric hooks, same chunk-handler contract — only the
  per-attempt dispatch differs.
- Read every `MetadataKeyGoogleThoughtSignature` reference site
  before Phase 1 starts. Each one is a Phase 1 deletion + replacement
  with the new `ReasoningRecord` path. Grep:
  `grep -rn MetadataKeyGoogleThoughtSignature --include="*.go"`.
- Capture live OpenAI Responses fixtures before Phase 1 starts;
  golden-fixture round-trips are how we verified Gemini's quirks and
  they're the only way to ground the unit tests against the real
  wire shape.
- Update `cmd/semstreams/main.go` AND `cmd/e2e-semstreams/main.go`
  per [[feedback_e2e_required_for_breaking_changes]] — the
  registry-singleton story is the cautionary tale on missing-binary
  migration.
- Coordinate the sister-repo bump (semspec / semteams / semconnect)
  on the Phase 1 tag landing. Workflow precedent is the ADR-042
  publisher-mode rollout.

When the ADR draft is ready, it should explicitly answer the four
remaining open questions above — particularly the `text.format` vs
`response_format` question, which determines whether the Phase 3
translator layer reshapes or passes through.
