# ADR-034: Structured Output via `response_format` for Local-Runtime Reliability

## Status

**Proposed — 2026-05-06.** Sign-off pending from semspec (the consuming
product). Implementation deferred to a follow-up session; this ADR
captures the design call so the next picker-up has the context.

Audit findings, web research, and architectural decisions documented
below. Implementation landed nowhere yet — last shipped tag is
`v1.0.0-beta.47` (graph-clustering capability timeout).

## Context

semspec runs sub-agent handoffs that require structured output at
every boundary (a coordinator emits an `action` plus typed `args`; a
sub-agent's terminal tool emits a structured triple a downstream rule
matches on). The framework's structured-output primitive today is
**tool-calling with strict argument schemas**, parsed on the wire by
the agentic-loop and validated by `agentic-tools` (e.g. the decide
executor's `action_allowlist`).

This works on cloud providers — Anthropic, OpenAI proper, Gemini Pro
— but breaks unevenly on **local runtimes** (Ollama, vLLM, sparky)
hosting smaller models like Qwen 30B or DeepSeek 16B. Failure modes
observed:

- Tool-call argument JSON parses but doesn't match the persona's
  `action_allowlist` (covered by beta.41's per-spawn allowlist + SAP
  coercion, but the SAP "save" is itself a signal of model/persona
  fit problems per the LOUD constraint).
- Tool-call arguments arrive as malformed JSON; agentic-model silently
  substitutes an empty object (`processor/agentic-model/client.go:602-608`)
  with a warn log; the model never sees the parse failure to retry.
- Model returns plain text instead of a tool call entirely (no recovery
  path beyond the next iteration's persona reminder).

The proximate forcing function: semspec's small-model deployments need
a *more reliable* structured-output primitive than tool-calling. The
OpenAI-compatible `response_format` field with strict JSON Schema is
that primitive on most local runtimes — vLLM uses xgrammar/outlines to
constrain decoding to schema-valid tokens; the model cannot emit
malformed JSON because the sampler won't let it.

Stretch motivation: smaller Gemini variants (Flash) occasionally wedge
in similar ways and could benefit if/when we add a Gemini native
adapter.

## The wire-shape problem

The `response_format` field has been a moving target since 2024. Three+
shapes are in active use across providers semstreams supports today or
might support tomorrow:

| Wire shape | Provider | Status (May 2026) |
|---|---|---|
| `response_format: {"type": "json_object"}` | OpenAI / OpenAI-compat | Legacy — bare JSON validity, no schema adherence guarantee |
| `response_format: {"type": "json_schema", "json_schema": {"name", "strict": true, "schema": ...}}` | OpenAI / vLLM / OpenRouter / sparky-if-OpenAI-compatible | **Current standard.** Strict-mode constrains decoding |
| `text.format` | OpenAI Responses API | New surface (separate from chat completions); we don't use it |
| `format: <schema>` (native) | Ollama `/api/chat` | Reliable; what Ollama tooling actually uses |
| `response_format: {...}` on Ollama `/v1/chat/completions` | Ollama OpenAI-compat | Partial — model-dependent; gemma3 ignores it ([gh#10001](https://github.com/ollama/ollama/issues/10001)) |
| `extra_body: {"guided_json": <schema>}` | vLLM legacy | Phasing out in favor of OpenAI-shape `response_format` |
| `responseMimeType` + `responseSchema` | Gemini native | Subset of OpenAPI 3.0 |
| (nothing — use forced tool-choice with output-as-tool-schema) | Anthropic | Anthropic's documented pattern |

The takeaway: there is no single shape we can pass through unchanged
across providers. Every adapter has to either honor the OpenAI shape,
translate it to a provider-specific equivalent, or no-op silently with
a warn.

## Audit of current semstreams state

Full audit in session log; key findings:

**Provider validation vs adapter dispatch are inconsistent today:**

- `model.Registry.Validate` (model/registry.go:440) accepts only
  `anthropic / ollama / openai / openrouter`. No `vllm`, no `sparky`,
  no `gemini`, no `localai`.
- `processor/agentic-model/adapter.go:36-45` (`AdapterFor`) branches on
  `gemini` and `openai`, defaulting unknowns to `GenericAdapter`.
- Gap: `gemini` is dispatched-on but registry-rejected (an operator
  literally cannot deploy `provider: "gemini"` today). `anthropic` is
  registry-allowed but has no adapter case (falls through to generic).

**Existing structured-output plumbing in agentic-model:** zero. No
`response_format`, `responseSchema`, `responseMimeType`, `json_mode`,
or `JSONMode` field anywhere. Tool-calling is the sole structured-output
mechanism. `AgentRequest.ToolChoice` exists but only routes the
tool-call decision (auto/required/none/function), not output shape.

**Adapter inventory:** three adapters (`adapter_openai.go`,
`adapter_gemini.go`, `adapter_generic.go`). Each owns a
`NormalizeMessages` hook (run at `client.go:187`); a `NormalizeRequest`
hook exists in the interface (`client.go:210`) but is currently unused
by all three.

## Decision

### 1. Treat `provider: "openai"` as the explicit umbrella for any OpenAI-API-compatible runtime

Operators routing to **vLLM, sparky, LocalAI, llama.cpp server,
LiteLLM, anything-OpenAI-compat** set `provider: "openai"` plus the
appropriate URL. This is already de facto the practice; the registry
doc-comment will be updated to make it explicit.

**We do not add `vllm`, `sparky`, `localai` as distinct providers.**
Doing so fragments the adapter chain without buying anything — every
OpenAI-compat runtime takes the same wire shape. The cost of "support
every flavor" is a maintenance tax that scales with deployment
diversity; the framework-neutrality memory entry
(`feedback_framework_boundary.md`) argues against it.

The two genuine outliers worth keeping distinct:

- **`provider: "ollama"`** — Ollama's native `/api/chat` accepts a
  schema-shaped `format` field that's more reliable than its
  OpenAI-compat `/v1` `response_format` translation (see gh#10001).
  Worth a dedicated adapter so `response_format` translates to `format`
  natively.
- **`provider: "gemini"`** — distinct API surface (`responseSchema`).
  Already has a dedicated adapter for message-level quirks; can absorb
  schema translation later if/when we wire a native Gemini client.

### 2. Add `AgentRequest.ResponseFormat` as an optional new field

Shape (in `agentic/types.go`, alongside `ToolChoice`):

```go
// ResponseFormat constrains the model's output to a JSON object or
// JSON-schema-conformant structure. Maps to OpenAI's response_format on
// OpenAI-compatible providers; translated to provider-specific
// equivalents on others (Ollama: native format field; Gemini: stubbed
// for v1; Anthropic: stubbed for v1, future translation to forced
// single-tool-call).
//
// Nil means no structuring constraint — current behavior; tool-calling
// remains the structured-output primitive for personas that work fine
// on cloud providers without response_format.
type ResponseFormat struct {
    // Type: "json_object" (legacy, bare JSON validity) or "json_schema"
    // (current standard, strict-mode schema adherence). Empty string
    // is invalid; callers must set one.
    Type string

    // Schema is the JSON Schema document. Required when Type ==
    // "json_schema"; ignored when Type == "json_object". Must be a
    // valid OpenAI Structured Outputs schema (subset of JSON Schema —
    // no $ref to external schemas, no anyOf at root, every property
    // in required, additionalProperties: false).
    Schema map[string]any

    // Name is required by OpenAI when Type == "json_schema". Should
    // describe the output (e.g. "decide_action_args"). Adapters that
    // don't need a name ignore it.
    Name string

    // Strict enables OpenAI's strict mode (response is guaranteed
    // schema-conformant; sampling is constrained). Default true on
    // construction by helper functions. Caller can set false to opt
    // into permissive mode (rare; mostly for compat testing).
    Strict bool
}
```

The field is optional; existing callers passing nil see no behavior
change. New callers (semspec, future graph-query handoffs that want
strict structured output) opt in.

### 3. Per-adapter v1 behavior

| Adapter | v1 behavior | Notes |
|---|---|---|
| `OpenAIAdapter` | Plumb `ResponseFormat` directly into `response_format` on the wire | Single hook in `NormalizeRequest`. Covers OpenAI proper, vLLM, OpenRouter, sparky. **The principal value of this ADR.** |
| `OllamaAdapter` (NEW) | Translate `ResponseFormat.Schema` into Ollama's native `format` field | Bypasses Ollama's buggy `/v1/chat/completions` response_format translation. Single new file. |
| `GeminiAdapter` | No-op for v1 with warn-once log. Translation to `responseSchema` deferred until we have a Gemini native client (today we hit Gemini's OpenAI-compat endpoint, which doesn't honor `response_format`). | Stretch goal acknowledged; not v1 scope. |
| `GenericAdapter` | No-op for v1 with warn-once log | Anthropic and unknown providers fall here. |

### 4. SDK plumbing

The `sashabaranov/go-openai` SDK's `ChatCompletionRequest` does not
expose `response_format` as a typed field directly. v1 plumbing
strategies, in preference order:

1. **Extend the SDK via composition.** Wrap `openai.ChatCompletionRequest`
   with a thin shim type that adds `ResponseFormat any`, marshal via
   custom `MarshalJSON`. Lowest blast radius.
2. **Patch the SDK fork.** Heavier; only justified if the SDK becomes
   load-bearing to several open features.
3. **Bypass the SDK for the request build path.** Marshal raw JSON to
   the upstream URL ourselves. Highest cost; only justified if the SDK
   is fundamentally unable to express a needed shape.

v1 picks (1).

### 5. Defer the registry hygiene fix

The `gemini` validator/adapter mismatch (gemini is rejected by
validation but special-cased by the adapter) is real but orthogonal.
A separate commit lands either bringing `gemini` into the validator's
allow-set or removing the unused adapter case. Tracking but not
gating.

## Options considered

**Option A: Wait for industry convergence.** Don't add anything; rely
on tool-calling. Rejected — semspec's small-model wedge is real today,
not a hypothetical, and waiting for Ollama's gh#10001 to close (or for
OpenAI's Responses API to be the universal default) leaves semspec
without the primitive it needs.

**Option B (chosen): Additive `ResponseFormat` field, OpenAI umbrella,
provider-specific translators where the wire shape genuinely diverges
(Ollama only for v1).** Smallest viable change that closes the
small-model reliability gap on the runtimes that matter.

**Option C: Full provider-agnostic translation matrix (every adapter
handles every shape).** Includes Anthropic translation to forced
single-tool-call, Gemini translation to `responseSchema`, etc. Rejected
for v1 scope — Anthropic and large-Gemini already work fine via
tool-calling (cloud providers handle structured output well); the
translation layer is real engineering with provider-specific testing
needs that doesn't pay back the small-model deployment.

## Consequences

### What changes

- `agentic.AgentRequest` gains an optional `ResponseFormat *ResponseFormat`
  field. Backward compatible — nil is the no-constraint case (current
  behavior).
- `processor/agentic-model/adapter.go` gains an `OllamaAdapter` case.
  `AdapterFor("ollama")` returns it instead of falling to generic.
- Personas/products that opt in see strict structured output on
  OpenAI-compat runtimes (vLLM, sparky, OpenRouter, OpenAI proper) and
  on Ollama natively. semspec's small-model handoffs become reliable.
- Operators with `provider: "openai"` continue working unchanged. New
  documentation calls out the umbrella explicitly.

### What's deferred

- **Anthropic structured-output translation.** Not added; tool-calling
  remains the recommended primitive for Anthropic. If a future need
  emerges, the translation shim lives at adapter level
  (`AnthropicAdapter.NormalizeRequest` translates `ResponseFormat` →
  forced single-tool-call where the tool's parameter schema IS the
  output schema; response parsing extracts the tool-call args as the
  output).
- **Gemini native adapter.** Today we hit Gemini's OpenAI-compat
  endpoint; the adapter handles message-level quirks but not
  structured-output translation. A future Gemini native client (using
  `genai-go` or equivalent) would translate `ResponseFormat.Schema` →
  `responseSchema`.
- **OpenAI Responses API surface.** Migration from `chat/completions`
  to the new `/v1/responses` endpoint is a separate, larger
  decision that affects the entire request/response shape, not just
  structured output. Out of scope for this ADR.
- **Registry validator/adapter `gemini` reconciliation.** Tracked for a
  separate commit.

### What might break

Nothing in v1 if callers don't opt in. For callers who DO set
`ResponseFormat`:

- Schemas that violate OpenAI's strict-mode subset (external `$ref`,
  root `anyOf`, optional properties) get rejected by the OpenAI
  endpoint with HTTP 400. Adapters do not validate locally — we trust
  the caller to ship a strict-compatible schema. Future work could
  add a pre-flight validator.
- Older OpenAI-compat servers (vLLM <0.6, very old Ollama) may not
  honor `response_format` and silently emit unconstrained output. The
  caller's response-parsing layer must still be defensive (the existing
  malformed-JSON handling at client.go:602 stays as the safety net).
- Ollama models without grammar support (rare but exists for some
  finetunes) silently ignore `format`. Same caller-side defense
  applies.

### Observability

Adapters log at Debug when `ResponseFormat` is honored on the wire and
at Warn (once-per-process via `sync.Once` or rate limiter) when an
adapter no-ops a non-nil `ResponseFormat`. Callers can grep logs to
see whether their structured-output intent is reaching the upstream.

A future `agentic_model_response_format_no_op_total{provider}`
Prometheus counter would surface silent no-ops at scale; out of scope
for v1.

## Implementation notes (for the next session)

Suggested chunking:

1. **`agentic.ResponseFormat` type** + helper constructors
   (`NewJSONSchemaFormat(name, schema, strict)`,
   `NewJSONObjectFormat()`). Tests for constructor field defaults.
2. **`OpenAIAdapter.NormalizeRequest`** plumbs `ResponseFormat` into
   the wire request. Includes the SDK-shim type for the marshalled
   field. Unit test covering the wire shape against an `httptest`
   recorder.
3. **`OllamaAdapter` (new file)** — `NormalizeRequest` translates
   `ResponseFormat.Schema` → Ollama native `format`. Unit test against
   recorded fixtures from a real Ollama instance.
4. **Registry doc update** — `model/registry.go` `Provider` field
   doc-comment explicitly enumerates `openai` as the umbrella for
   OpenAI-compat runtimes. No code change beyond the comment.
5. **Integration test** against a real Ollama in the local-dev tier.
   Probably gated behind a build tag matching the existing
   ollama_probe pattern.
6. **Documentation update** — `docs/operations/04-ollama-setup.md` (or
   a new `docs/operations/11-structured-output.md`) explains the
   OpenAI-vs-Ollama-vs-Gemini matrix and which provider strings to
   use for which runtimes.
7. **semspec migration** — semspec personas opt into `ResponseFormat`
   for their decide-tool boundaries. (semspec's repo, not this one.)

Estimated cost: 1-2 sessions including review. The adapter change is
small; the SDK-shim and integration testing are where the real
engineering sits.

## References

- Audit findings (Explore agent, 2026-05-06 session)
- Web research (2026-05-06 session) — confirms the multi-shape state
  of `response_format` in May 2026
- [Introducing Structured Outputs in the API — OpenAI (Aug 2024)](https://openai.com/index/introducing-structured-outputs-in-the-api/)
- [Structured Outputs — vLLM docs](https://docs.vllm.ai/en/latest/features/structured_outputs/)
- [Structured Outputs — Ollama docs](https://docs.ollama.com/capabilities/structured-outputs)
- [Improve compatibility with OpenAI structured outputs json_schema response format — gh#10001](https://github.com/ollama/ollama/issues/10001)
- [Structured Outputs — OpenRouter docs](https://openrouter.ai/docs/guides/features/structured-outputs)
- [ADR-024: Layered LLM Timeouts](024-layered-llm-timeouts.md) — closest precedent for a per-call LLM-shape knob
- `processor/agentic-model/adapter.go` — adapter dispatch
- `model/registry.go:440` — provider validation set
- `processor/agentic-model/client.go:602-608` — current malformed-JSON
  fallback (the safety net this ADR's strict mode reduces dependence on)
