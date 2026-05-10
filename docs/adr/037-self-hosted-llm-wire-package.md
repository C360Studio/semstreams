# ADR-037: Self-Hosted LLM Wire-Format Package (`model/wire`)

## Status

**Proposed — 2026-05-10.** Retires the dependency on
`sashabaranov/go-openai` in favor of a self-hosted wire-format package
scoped to the ChatCompletion surface we actually use. Forcing function
is the Gemini 2.x → 3.x sunset trajectory: Gemini 3.x preview requires
`thought_signature` echo on multi-turn tool flows, the SDK's typed
`ToolCall` struct cannot carry the field, and the open SDK PR (#1069)
solves a different layer. We have a runway window before 2.5 sunsets;
this ADR uses that window to take ownership rather than ship a
RoundTripper workaround that compounds with every future provider
quirk.

Last shipped tag is `v1.0.0-beta.50` (ResponseFormat threading +
RelatedLoops).

## Context

### The forcing functions

Three signals point the same direction:

**(1) Gemini 2.5 has a finite runway.** semspec deploys against the
Gemini OpenAI-compat endpoint at `/v1beta/openai`. The 2.5 lineup
(pro/flash/flash-lite) works through our existing adapter chain
because thought signatures aren't required. Google has telegraphed that
3.x will be the production lineup; 2.5 will sunset on Google's
timeline, not ours. Once 3.x is the default, every multi-turn tool
flow against Gemini hits HTTP 400 because the SDK strips
`thought_signature` from the response and there is no path through the
SDK's type system to put it back. See
`project_gemini_3x_thought_signature_research.md` for the exact wire
shape and the per-step echo rule.

**(2) Adapter-quirk debt is structural, not transient.** The beta log
records the pattern:

| Tag | Quirk addressed | Provider |
|---|---|---|
| beta.5 | Anthropic capability surface | Anthropic |
| beta.28 | Consecutive same-role message collapse | OpenRouter |
| beta.34/43/47 | Connection hygiene + capability timeout wiring | All LLM-class HTTP |
| beta.42 | Silent stripping of `EndpointConfig` fields in `ResolveEndpoint` | graph-query path |
| beta.46 | Anthropic-only duplicate tool-result class | Anthropic |
| beta.48 | `ResponseFormat` plumbing (gated on SDK v1.41+) | All |
| beta.49 | Hardcoded `WriteTimeout: 10s` cut LLM responses mid-write | Front-door HTTP |

Every adapter we add finds another wire-shape gap that the SDK's
"OpenAI exactly" type system can't express. Each is solved with
in-tree workarounds (NormalizeRequest hooks, post-processing,
side-channel metadata). The cost compounds.

**(3) Upstream velocity does not match ours.** semstreams ships a beta
roughly every 12 hours; `sashabaranov/go-openai` ships ~quarterly. PR
#1069 (`extra_body` parameter support) has been open with standing
review comments and no merge ETA. We cannot pin our roadmap to that
cadence — especially with a known Gemini-shape forcing function on the
horizon.

### What we actually use the SDK for

Audit of `processor/agentic-model/client.go` and `graph/llm/openai_client.go`:

| SDK surface | We use it? |
|---|---|
| `ChatCompletionRequest` / `ChatCompletionResponse` | Yes — central |
| Streaming (`CreateChatCompletionStream`) | Yes |
| `ToolCall` / `Function` / `ToolChoice` types | Yes |
| `ChatCompletionResponseFormat` (beta.48) | Yes |
| `APIError` / `RequestError` | Yes |
| Embeddings API | **Yes** — `graph/embedding/http_embedder.go:24,91,105,157` uses `openai.Client` / `openai.EmbeddingRequest` / `openai.EmbeddingModel`. Small surface (~10 LoC) but it's a second SDK call site and must migrate before Phase 3 can retire the dep. (An earlier audit revision claimed this path was already SDK-free; that was wrong — caught by ADR review 2026-05-10.) |
| Files / Images / Audio / Moderation | **No** |
| Assistants / Threads / Runs / Batch | **No** |
| Fine-tuning | **No** |
| Retry / backoff | **No** — we own this (`RequestWithRetry` framework, beta.20) |
| HTTP transport | **No** — we own this (`model.NewHTTPClient` invariant, beta.43/47) |

The dependency is doing two jobs: marshaling a request struct and
unmarshaling a response struct. Both stable, well-documented JSON
shapes. We re-export none of its other ~40 endpoints.

### What we already own that the SDK was supposedly providing

The unified HTTP client invariant (beta.43/47,
`project_unified_http_client_invariant.md`) already moved transport
ownership in-house. The provider-adapter pattern (ADR-023) already
moved cross-provider normalization in-house. Layered timeouts
(ADR-024) moved bounded context in-house. Each prior step was
narrowing what the SDK contributes to the stack. This ADR finishes
that arc.

## Decision

### 1. Build `model/wire` — a tightly scoped wire-format package

New package at `model/wire/` (lives next to `model/registry.go` and
`model/http_client.go`; signals it's part of the model surface, not a
top-level concern). Naming alternatives considered: `llmwire/`,
`openaiwire/`, `internal/wire/`. `model/wire` chosen because the
package is conceptually "the wire format the model registry produces
requests for" and shares ownership with the HTTP client invariant
already in `model/`.

The package owns:

- **Wire structs** for ChatCompletion request, response, streaming
  delta, error envelope. Every level carries an `Extras
  map[string]json.RawMessage` field for unknown JSON, including
  `Message`, `ToolCall`, `Function`, `Choice`. This is the lever the
  SDK doesn't give us.
- **Streaming SSE parser** for `text/event-stream` chunked responses.
  ~100-200 LoC; we already understand the chunker (beta.21 length-truncation
  fix).
- **Error envelope handler** that tolerates both the standard
  object-shaped error and the array-wrapped variants emerging from
  Gemini 3.x and others. Pre-unmarshal sniff: if body[0] == '[', peel.
- **HTTP client integration**: the package consumes `*http.Client`
  via constructor injection so `model.NewHTTPClient` remains the
  single source for transport. The wire package does not construct
  its own transport.

The package does NOT own:

- Embeddings, audio, files, images, assistants, threads, batch, fine-tuning, moderation
- Retry / backoff (caller's responsibility)
- Provider-specific normalization (adapter layer's responsibility)
- Model registry, capability resolution, endpoint resolution (existing `model/` surface)

### 1a. What `model/wire` is, and what it stands on

To set expectations honestly: **this is independent re-implementation
against public wire-format specs, leaning on Go stdlib for everything
stdlib already solves.** Not a clean-room rewrite of an LLM platform.
Not copy-paste from the SDK. Not a heroic effort.

What we write ourselves (small, idiomatic Go):

- **Typed structs with JSON tags** matching OpenAI's
  publicly-documented chat-completions shape (plus Anthropic / Gemini
  for native sibling clients if/when added). Struct definitions and
  JSON tags ARE the design surface; we own them because the SDK's
  fixed types are what blocks us. Authored against OpenAI's public API
  documentation, not against SDK source.
- **`Extras` carriers + MarshalJSON/UnmarshalJSON** using stdlib
  `encoding/json` and `json.RawMessage` — the well-known idiom we
  already use in `payload_registry.go`. Not novel.
- **SSE parser** built on `bufio.Scanner` with a custom split function.
  Standard Go pattern (used widely; not LLM-specific). ~100-200 LoC.
- **Error envelope sniff**: `bytes.TrimSpace` + first-byte check, then
  `json.Unmarshal`. ~30 LoC.

What we lean on (stdlib + existing semstreams invariants — not
rewritten):

- `net/http` transport — via `model.NewHTTPClient` (beta.43/47 invariant).
- `encoding/json` marshaling — including the `RawMessage` extension idiom.
- `bufio` for the SSE byte-stream framing.
- `context` for cancellation + deadlines (ADR-024 layered timeouts).
- `RequestWithRetry` (beta.20) for retry/backoff. Wire calls go
  through it the same way SDK calls do today.
- Existing `model.Registry`, capability resolution, health policy,
  HTTP client invariant. All unchanged.

What we explicitly do not do:

- **No copy-paste from `sashabaranov/go-openai`**, or any other
  third-party SDK source. License compliance considerations aside,
  the structural problem that motivates this ADR (no extension
  carriers on the SDK's types) is solved by writing types we own from
  the start, not by adapting types we didn't author.
- **No novel framing protocols, no custom retry semantics, no
  bespoke connection pooling.** The SDK already pays for none of
  those (we own retry + transport). The wire package is JSON-on-HTTP
  with `Extras` carriers, nothing fancier.
- **No SDK feature porting we don't use.** Files, batch, assistants,
  threads, fine-tuning, moderation, audio, images — none get re-implemented.
  If a future need emerges, that's a new chunk; today's chunks don't
  pre-bake them.

The honest size estimate: ~600-1000 LoC including tests, of which
maybe 400 is non-test Go. Comparable to a single well-scoped
processor in this codebase (`processor/agentic-memory/` is 1,200 LoC
non-test). Not a tower of new abstractions.

### 2. Structural wire types; adapters do the domain translation

The current SDK-bridge architecture forces translation at two
boundaries: `agentic.ChatMessage` ⇆ `openai.ChatCompletionMessage` ⇆
wire JSON. The middle hop adds no value — it strips fields the SDK
doesn't know about and forces us to invent side channels (Metadata
maps, RoundTripper hacks, etc.) for everything provider-specific.

**Design choice (load-bearing):** `model/wire` types are
*structural* — they mirror the JSON shape with `Extras` carriers, and
they import only stdlib. They do **not** import `agentic/`. The
translation `agentic.ChatMessage` ⇆ `wire.ChatMessage` lives in the
adapter layer (`processor/agentic-model/`). This keeps `model/wire`
as a leaf package with no domain knowledge — pure JSON manipulation,
testable without any agentic context, no risk of an
`agentic/` → `model/` cycle emerging later (verified clean today:
`grep -r "semstreams/model" agentic/` returns nothing).

The alternative considered — `wire.ChatMessage` *is*
`agentic.ChatMessage` with JSON tags — collapses the boundary at the
cost of binding the wire format to whatever the domain wants. The
domain type evolves at framework speed; the wire format evolves at
provider speed. Keeping them independent isolates churn.

Shape sketch:

```go
package wire

// ChatMessage is the wire-shape JSON projection. Fields use the
// OpenAI-compat names because that's the lingua franca; providers that
// diverge translate at the adapter layer.
type ChatMessage struct {
    Role       string                     `json:"role"`
    Content    json.RawMessage            `json:"content,omitempty"`
    Name       string                     `json:"name,omitempty"`
    ToolCalls  []ToolCall                 `json:"tool_calls,omitempty"`
    ToolCallID string                     `json:"tool_call_id,omitempty"`
    Extras     map[string]json.RawMessage `json:"-"`
}

func (m ChatMessage) MarshalJSON() ([]byte, error) { /* merge Extras */ }
func (m *ChatMessage) UnmarshalJSON(b []byte) error { /* split known/unknown */ }

type ToolCall struct {
    ID       string                     `json:"id"`
    Type     string                     `json:"type"`
    Function Function                   `json:"function"`
    Extras   map[string]json.RawMessage `json:"-"`  // extra_content for Gemini, etc.
}
```

The `Extras` MarshalJSON/UnmarshalJSON pattern is a small, well-known
idiom (we already use it in `payload_registry.go` for type-discriminated
envelopes). Two test cases gate it: round-trip preservation of unknown
fields, and known-field-collision rejection.

### 2a. What `Extras` cannot express (and how that's handled)

The carrier is right for **leaf-attached scalar/object fields at a
known JSON path** (the Gemini `thought_signature` case). It is
structurally inadequate for three classes of divergence — those
remain adapter-layer problems, not wire-layer problems:

- **Array reordering.** If a provider returns `tool_calls` in a
  different order than the assistant emitted them, ordering is encoded
  in the slice itself, not in `Extras`. Adapter rebuilds the order in
  `NormalizeResponse`.
- **Different nesting depth for the same semantic field.** Gemini puts
  thought signatures at `tool_calls[i].extra_content.google.thought_signature`.
  If a future provider puts the same semantic value at
  `tool_calls[i].metadata.signature`, both round-trip through `Extras`
  unchanged, but the adapter's `NormalizeResponse` has to know *where
  to look*. `Extras` is structurally tolerant, semantically blind. The
  adapter remains the place where provider-specific knowledge lives —
  the wire layer's job is only to not throw the field away.
- **Content-array polymorphism.** OpenAI's `content` field is
  `string | []ContentPart`. `wire.ChatMessage.Content json.RawMessage`
  sidesteps the discrimination at marshal time; helpers
  `Content.IsString() / Content.AsParts() ([]ContentPart, error)` live
  on the wire side so consumers don't reimplement the sniff. Adapters
  reach for these helpers when they need to operate on parts.

### 2b. Streaming-delta `Extras` merge semantics

Streaming responses arrive as a sequence of partial `Choice` deltas
that accumulate into a final message. The wire layer must define how
`Extras` keys on partial deltas combine into the final accumulated
message. Rule:

- **Last-writer-wins on key collision.** If delta-N's `Extras["foo"]`
  and delta-(N+1)'s `Extras["foo"]` both exist, the later one wins
  unconditionally. Debug log on every collision
  (`wire.stream: extras key foo overwritten in delta N`).
- **No deep-merge.** If `Extras["extra_content"]` exists on both
  delta-N and delta-(N+1), delta-(N+1) replaces wholesale — we do not
  merge inside the `json.RawMessage`. Deep-merge introduces ambiguity
  and invents semantics the provider doesn't guarantee.
- **Per-`tool_call` `Extras` accumulate alongside the existing
  `toolCalls map[int]*ToolCall` accumulator** (current pattern at
  `processor/agentic-model/stream.go:31-101`). When a delta brings a
  new chunk for `tool_calls[i]`, its `Extras` map merges into the
  accumulator's `Extras` under the same last-writer-wins rule.

This rule is load-bearing for Gemini specifically — `thought_signature`
arrives on a single delta partway through the tool-call stream, and
the accumulator must preserve it on the final message.
Streaming-permutation tests (§Testing below) gate the implementation.

### 3. Migration strategy: vendor, migrate, retire

The SDK does not get ripped out in one commit. Three phases:

**Phase 1 — Vendor `model/wire` alongside `sashabaranov/go-openai`.**
Both packages compile. Tests cover wire-package round-tripping against
recorded fixtures (real OpenAI, Anthropic-via-adapter, Gemini, Ollama,
OpenRouter, vLLM, sparky responses captured under `model/wire/testdata/`).
No production code changes. **No behavior change ships in Phase 1.**

**Phase 2 — Migrate adapters, one at a time, behind a per-endpoint
flag.** `EndpointConfig.WireBackend` (string: `"sdk"` default,
`"wire"` opt-in). `processor/agentic-model/client.go` branches on the
flag at the request-construction site. Same for
`graph/llm/openai_client.go`. Each adapter migration ships as its own
beta tag with explicit before/after fixture diff. Order by blast
radius: Gemini first (most adapter quirks; biggest payoff), then Ollama,
then OpenRouter, then the OpenAI umbrella (vLLM/sparky/OpenAI proper) last.

**Phase 3 — Flip the default, then retire.** Once every adapter has
soaked on `wire` for at least one release cycle with no regressions,
flip `WireBackend` default to `"wire"`. Two releases later, remove the
SDK branch entirely. `go.mod` loses `sashabaranov/go-openai`.

This sequencing makes any phase reversible. If Phase 2 finds that the
wire package is missing a behavior the SDK was silently providing, we
add it without rolling back the migration (rollback is just per-endpoint
config). The retirement step in Phase 3 is the only one-way commit.

### 4. Gemini 3.x is the migration's first user

The `extra_content.google.thought_signature` field is the proof-of-shape
for the `Extras` carrier:

- On response unmarshal: `ToolCall.Extras["extra_content"]` captures
  the raw `{"google": {"thought_signature": "..."}}` blob.
- `GeminiAdapter.NormalizeResponse` (new hook addition) copies the blob
  into `agentic.ToolCall.Metadata["gemini_thought_signature"]` for
  domain-level access.
- On the next request: `GeminiAdapter.NormalizeRequest` reads metadata
  and writes back into `ToolCall.Extras["extra_content"]` for the first
  tool_call per step (per Google's "first-call-per-step" rule).
- Wire marshal merges `Extras` into the JSON.

Same idiom handles the `[{...}]` error-array wrapper at the wire level:
`wire.DecodeError(body)` sniffs the leading byte and peels.

No special-case code paths. No RoundTripper. The `Extras` carrier is
the framework's answer to every future "provider-X adds field Y the
SDK doesn't model."

### 5. Adapter interface evolves slightly

Today's `ProviderAdapter` interface (`processor/agentic-model/adapter.go:8-28`)
operates on `openai.ChatCompletionMessage` and `openai.ToolCall`. The
migration replaces those with `wire.ChatMessage` and `wire.ToolCall`.
Signatures stay parallel; the type swap is the substantive change.
`NormalizeResponse` gains real responsibility — today it's a near no-op
because the SDK already dropped the interesting fields. With `Extras`
preserved, adapters do real work translating provider-specific blobs
into domain metadata.

## Options considered

**Option A: Status quo + RoundTripper for Gemini 3.x specifically.**
Two-day fix. Gates to `provider="gemini"`. Compounds — every future
provider quirk wants its own RoundTripper or its own NormalizeRequest
hack. The known list of upcoming quirks (Anthropic structured-output
translation, Ollama native `/api/chat`, OpenAI Responses API migration)
suggests this would not be the last one. Rejected.

**Option B (chosen): `model/wire` package, scoped to ChatCompletion.**
Two weeks of focused work. Owns the wire format we actually use,
preserves all unknown fields by design, retires a dependency whose
velocity doesn't match ours. The forcing function (Gemini 3.x) gives
us a concrete first user that proves the design rather than testing
hypothetical extensibility.

**Option C: Full SDK clone (every OpenAI surface).** Months of work.
Replicates batch / files / assistants / threads / etc. that we don't
use. Rejected — pure cost with no payback. The right scope is *what we
use*, not *what the SDK provides*.

**Option D: Fork `sashabaranov/go-openai` and add `Extras` carriers
upstream-of-our-fork.** Tempting because it inherits the SDK's other
work for free. Rejected because (a) we'd be carrying a long-term fork
with no merge path back (PR #1069's status suggests our changes won't
land upstream quickly either), (b) we'd still be carrying the bulk of
SDK code we don't use, (c) the SDK's type system encodes OpenAI-shape
assumptions that bite us on Anthropic and Gemini regardless of whether
we have `Extras` — the fork doesn't fix the structural issue.

**Option E: Codec-wrapper for response-side only.** Wrap the SDK's
response decode to preserve `Extras` on response, leave the request
path untouched. Would solve Gemini 3.x in days, not weeks. Rejected
because the case is bidirectional: thought signatures must be echoed
*back* on subsequent requests, which requires `Extras` on the
request-side `ToolCall` too, which means modifying the SDK structs —
which is Option D. E collapses into D once you walk the round-trip.
Called out explicitly so future readers don't relitigate.

## Consequences

### What changes

- New package `model/wire/` (~600-1000 LoC including tests). Owns
  request/response/streaming/error types and JSON round-tripping for
  the ChatCompletion surface.
- `processor/agentic-model/adapter.go` interface evolves to operate on
  `wire.*` types instead of `openai.*` types. Three adapters
  (`OpenAIAdapter`, `GeminiAdapter`, `GenericAdapter`, `OllamaAdapter`)
  refactor their `Normalize*` methods. Migration order documented in
  Phase 2.
- `processor/agentic-model/client.go` and `graph/llm/openai_client.go`
  refactor to use `wire.Client` (or equivalent) for ChatCompletion calls.
  Both already use `model.NewHTTPClient` for transport; that stays.
- `graph/embedding/http_embedder.go` migrates to `wire.EmbeddingsClient`
  (in scope; small surface — single request shape, no streaming, no tool
  calls). This is a non-negotiable dependency of Phase 3: the SDK
  cannot drop out of `go.mod` while embeddings still imports it.
  Migration happens in its own chunk after the ChatCompletion adapters
  are stable.
- `EndpointConfig.WireBackend string` (Phase 2 only; retired in Phase 3)
  gates the migration per-endpoint. Operators can roll forward or back
  via config-only change.
- `agentic.ToolCall.Metadata` grows a documented well-known key
  `gemini_thought_signature` (the metadata map already exists; this is
  a documentation addition, not a schema change).
- Gemini 3.x preview multi-turn tool flows work. semspec migrates its
  Gemini deployments to 3.x preview when ready.

### What stays the same

- `model.NewHTTPClient` is still the only HTTP client constructor for
  LLM-class calls. The wire package consumes a client; it does not
  construct one.
- `model.Registry`, `model.EndpointConfig`, `model.HealthPolicy`,
  capability timeouts (ADR-024), provider adapter dispatch
  (ADR-023), `ResponseFormat` (ADR-034), strict tool calling
  (ADR-035) — all unchanged in API surface. The wire package is below
  these.
- Retry/backoff (`RequestWithRetry`) is unchanged. Wire calls go through
  it the same way SDK calls do today.
- `agentic.ChatMessage`, `agentic.ToolCall`, `agentic.AgentRequest` —
  unchanged in shape. Adapters translate them to `wire.*` types inside
  `processor/agentic-model/` (see §2 — wire is structural, adapters
  translate). The translation replaces today's
  `agentic → openai.* → JSON` two-hop with a single
  `agentic → wire → JSON` hop.

### What's deferred

- **OpenAI Responses API surface** (`/v1/responses`). Different shape
  from chat completions; out of scope. If/when we adopt it, lives as a
  sibling `wire.ResponsesClient` in the same package.
- **Native Gemini transport** (`:generateContent`). Stays an option for
  a future ADR if/when the `customtools` variant or 1M context becomes
  load-bearing. The `model/wire` decision does not foreclose it —
  `wire.GeminiNativeClient` would be a sibling under the same package.
- **Anthropic native transport.** Same logic — we currently hit
  Anthropic via OpenAI-compat through OpenRouter or similar. A native
  client lives as a sibling if needed.
- **Phase 3 retirement.** Tied to **calendar-based soak time, not
  release-count.** semstreams ships ~2 betas/day; "one release cycle"
  is meaningless as a soak floor. Concrete rule:
  **each adapter must run on `wire` in production for ≥7 calendar days
  with no regression before its `WireBackend` default flips to `wire`.
  Phase 3 (SDK removal from `go.mod`) requires ≥30 calendar days after
  the last adapter's default flip.** This matches the discipline
  established in the beta.49 lesson (3 months of bad releases shipped
  on top of a half-migration; calendar time is what catches
  unobservable wedges).

### What might break

During Phase 2 (per-endpoint migration), each adapter cutover is the
risk point. Mitigations:

- Fixture-based round-trip tests in `model/wire/testdata/` covering
  real captured responses from every provider before Phase 2 starts.
- `EndpointConfig.WireBackend` defaults to `"sdk"` until each adapter
  is explicitly migrated. Migration is opt-in per endpoint.
- Phase 2 ships one adapter per beta tag. Regressions surface in
  isolation, not as a bulk migration.

For end-of-Phase-3 retirement:

- `go.mod` removes `sashabaranov/go-openai`. **Hard blockers** that
  must be cleared first:
  - **`graph/embedding/http_embedder.go`** must be migrated (see "What
    changes"). The SDK cannot drop while this file uses it.
  - **`graph/llm/openai_client.go`** depends on SDK error types
    (`*openai.APIError`, `*openai.RequestError`) for retry
    classification. `errors.As` / `errors.Is` call sites must have
    `wire`-equivalent error types in place before retirement. Audit
    every such site as part of the migration chunk for this file.
  - **Cross-tree audit.** `grep -rn "sashabaranov\|openai\." --include="*.go"`
    reports ~88 non-test references across 14 files (per
    architect review 2026-05-10). Every one must be either migrated
    or proven dead before Phase 3.
- Any external consumer importing semstreams as a library and reaching
  into `processor/agentic-model` for SDK types breaks. Mitigation:
  `processor/agentic-model` already exposes domain types
  (`agentic.ChatMessage` etc.); SDK types were never public API.
  Confirmed by `grep -r "openai\." docs/` (no documented references
  outside the audit-internal commentary).

### Testing

The wire layer is the JSON-correctness floor; fixture replay alone
won't catch the failure modes that matter. Required test classes
before Phase 2 starts:

- **Round-trip fixture tests.** Every captured fixture
  (`model/wire/testdata/`) must round-trip
  `bytes → struct → bytes` with byte-equality after normalization
  (fixture-normalizer regex-strips `id`, `created`, request-id-style
  fields so wallclock drift doesn't flake tests). Per provider, per
  shape (request, response, streaming-frame, error).
- **Fuzz tests on `UnmarshalJSON`.** Every wire struct gets a
  `FuzzUnmarshalX` that asserts no panic on malformed input and that
  unknown fields always land in `Extras` rather than being dropped or
  routed wrong. The unknown-field handling is exactly the kind of
  code that breaks on adversarial input.
- **Differential tests during Phase 2.** While both SDK and wire
  backends compile, a parallel test pass marshals the same
  `agentic.AgentRequest` through both backends and asserts byte-equal
  JSON output (modulo normalized fields). Drift surfaces as a test
  failure, not a production wedge. Test is removed alongside the SDK
  in Phase 3.
- **Streaming-permutation tests.** Given a captured stream's
  delta-chunk sequence, randomly re-split byte boundaries (with
  fixed seed for reproducibility) and assert the accumulated final
  message is identical across permutations. Catches off-by-one
  accumulator bugs that single-fixture replay misses. Gates the
  §2b `Extras` merge-semantics rule.
- **`task fixtures:refresh`.** A make-target that re-captures all
  fixtures against live providers. Run on demand when a provider
  rev's wire shape is suspected. Diff review before commit catches
  silent provider shape changes.

### Observability

- New Prom counter `model_wire_extras_preserved_total{level,key}`
  surfaces every unknown field round-tripped through the `Extras`
  carrier. Spikes on a previously-unseen key flag a new provider
  feature worth investigating.
- New Prom counter `model_wire_error_envelope_total{shape}` distinguishes
  standard-shape from array-wrapped errors. Useful for tracking which
  providers ship which shapes.
- Debug log on every adapter `NormalizeRequest` / `NormalizeResponse`
  call that touches `Extras` — the silent-chain bug class (beta.47) is
  the cautionary tale; log when the wire layer is doing work.

### Compounding benefits beyond the immediate Gemini fix

- Anthropic native adapter becomes a 1-2 day add (wire types are already
  domain-shaped; Anthropic's Messages API shape is a translation
  problem, not a struct redesign).
- Ollama native `/api/chat` (ADR-034 chunk 3b deferral) becomes a
  sibling client in `model/wire/ollamanative.go`. Same package, same
  patterns, no second HTTP client invariant to maintain.
- Future provider additions (Cerebras, Groq, Fireworks, future Google
  surfaces) have a clear template: write the wire variant, add to
  adapter dispatch.

## Implementation notes (for the next session)

Suggested chunking. Each chunk is a single PR / beta tag.

1. **Audit + fixture capture.** Run every existing E2E flow against
   every provider we deploy (OpenAI, Anthropic-via-OpenRouter, Gemini
   2.5, Ollama, vLLM-sparky). Capture request/response JSON to
   `model/wire/testdata/`. Two gotchas:
   - **Capture chunked SSE frames, not raw bytes.** Network-imposed
     chunk boundaries are non-deterministic; replaying byte-streams
     would flake. Frame-level capture is stable.
   - **Build a fixture-normalizer.** Regex-strip / struct-zero
     non-deterministic fields (`id`, `created`, request-id headers,
     timing timestamps) before assertion. Without this, tests flake on
     trivial provider drift.
   - Ship a `task fixtures:refresh` Taskfile target alongside the
     captured set so operators can re-capture on demand and diff-review
     before commit when a provider rev'd shape is suspected.
2. **`model/wire/types.go`** — request, response, streaming delta,
   error envelope types with `Extras` carriers. MarshalJSON /
   UnmarshalJSON implementations. Round-trip tests + `FuzzUnmarshal*`
   tests against Chunk 1 fixtures. No client code yet.
3. **`model/wire/client.go`** — `Client.ChatCompletion(ctx, req) (Response, error)`
   and `Client.ChatCompletionStream(ctx, req) (Stream, error)`. Consumes
   `*http.Client` via constructor (caller passes
   `model.NewHTTPClient(...)`). No retry, no provider awareness.
4. **`model/wire/stream.go`** — SSE parser. Tested against captured
   streaming fixtures from at least three providers (OpenAI streams
   slightly differently from Anthropic-via-OR; Gemini differs again).
   Streaming-permutation tests (§Testing) gate the
   §2b merge-semantics rule.
5. **`model/wire/errors.go`** — error envelope sniff + decode, including
   array-wrap peel. Tested against captured error fixtures.
6. **Adapter interface migration** — `ProviderAdapter` operates on
   `wire.*` types. The four existing adapters (`OpenAIAdapter`,
   `GeminiAdapter`, `GenericAdapter`, `OllamaAdapter`) refactor their
   `Normalize*` methods and gain the
   `agentic.ChatMessage` ⇆ `wire.ChatMessage` translation that §2
   places in the adapter layer. Compile-only change at first; behavior
   unchanged because the production path still uses the SDK client.
   Existing tests still pass.
7. **`EndpointConfig.WireBackend` flag** — operator-facing config to
   choose `"sdk"` (default) or `"wire"` per endpoint. Wired into
   `processor/agentic-model/client.go` and `graph/llm/openai_client.go`.
   Default `"sdk"` means zero behavior change ships in this chunk.
   Differential tests (§Testing) wired in alongside.
8. **Gemini migration** — `GeminiAdapter` gains `Extras`-aware
   `NormalizeRequest` (inject `extra_content.google.thought_signature`)
   and `NormalizeResponse` (extract). Test against Gemini 3.x preview
   live (`live_llm` build tag). Migration guide written for
   operators flipping `WireBackend: "wire"` on Gemini endpoints.
9. **Ollama / OpenRouter / OpenAI umbrella migration** — one chunk
   each, same shape as chunk 8. Each ships its own beta tag.
10. **Embeddings migration** — `wire.EmbeddingsClient` sibling under
    `model/wire/`. `graph/embedding/http_embedder.go` swaps its
    `openai.Client` for the wire equivalent. Lower complexity than
    ChatCompletion (no streaming, no tool calls); can land in parallel
    with chunk 9 once the wire package shape is stable. **Required for
    Phase 3** — SDK cannot drop from `go.mod` until this is done.
11. **Phase 3 default flip** — `WireBackend` default becomes `"wire"`.
    **Calendar-based soak: each adapter ≥7 days at default-`wire`
    before this flip is committed for that adapter.** No release-count
    shortcut.
12. **SDK retirement** — remove `sashabaranov/go-openai` from `go.mod`.
    `WireBackend` field deprecated; removed in a later release. **≥30
    calendar days after the last adapter's default flip.** Cross-tree
    audit (`grep -rn sashabaranov`) must return zero non-test hits.

Estimated cost: 2-3 weeks for chunks 1-7 (foundation). Each migration
chunk (8, 9, 10) is 1-2 days. Total wallclock 4-5 weeks across
multiple sessions before the Phase 3 soak begins, then ≥30 calendar
days of soak before retirement. No single chunk forces a "big bang."

## References

- `project_gemini_3x_thought_signature_research.md` — wire shape, PR
  #1069 status, "first-call-per-step" rule.
- `project_unified_http_client_invariant.md` — precedent for ownership
  of an LLM-class concern.
- `feedback_class_of_bugs_to_invariant.md` — pattern for promoting
  fixes into framework invariants.
- ADR-023: Provider Adapters and Tool Choice — the adapter dispatch
  layer this ADR builds on.
- ADR-024: Layered LLM Timeouts — companion invariant on the
  context.Context side of LLM calls.
- ADR-034: Structured Output via `response_format` — the most recent
  shape-translation work; informed the `Extras` carrier design.
- [PR #1069 — Support extra_body parameters (sashabaranov/go-openai)](https://github.com/sashabaranov/go-openai/pull/1069)
- [Thought signatures — Google AI for Developers](https://ai.google.dev/gemini-api/docs/thought-signatures)
- `processor/agentic-model/adapter.go` — current adapter interface
- `model/registry.go` — `EndpointConfig` struct that gains `WireBackend`
- `model/http_client.go` — `NewHTTPClient` invariant that the wire
  package consumes.
