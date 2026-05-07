# ADR-035: Strict-Mode Tool Calling via `function.strict`

## Status

**Accepted — 2026-05-07.** Companion to [ADR-034](034-structured-output-response-format.md).
Originated as an upstream ask from semspec after their beta.50 work
landed structural strict-mode subset (`additionalProperties: false`
across deliverable schemas). Tooled and adopted within the same week
because the gap is exactly symmetric to the response_format work
ADR-034 already shipped.

## Context

ADR-034 plumbed `response_format` strict-mode end-to-end:
`agentic.ResponseFormat.Strict` → `applyResponseFormat` →
`openai.ChatCompletionResponseFormatJSONSchema.Strict` →
`json_schema.strict: true` on the wire. **That field constrains the
content of `chat.completion.message.content` only.** When the caller
sets `ToolChoice.Mode == "required"` (semspec's overwhelming case —
every persona dispatch forces `submit_work`), the structured payload
arrives via `tool_calls[].function.arguments` — a different wire
field that `response_format` does not constrain.

Today `agentic.ToolDefinition` has three fields:

```go
type ToolDefinition struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}
```

`processor/agentic-model/client.go` constructs
`openai.FunctionDefinition` from this struct and never sets
`Function.Strict`. The SDK has supported the field since
`go-openai v1.41` (line `chat.go:369`); we just don't expose it.

So tool-call argument schema conformance — the canonical OpenAI
guarantee for sampling-constrained tool args — is unreachable from
the framework. semspec's structural prep (`additionalProperties:false`
at every object level) buys nothing over the wire until the upstream
flag propagates.

## Decision

Add `Strict bool` to `agentic.ToolDefinition` with `json:"strict,omitempty"`,
plumb it to `openai.FunctionDefinition.Strict` in the request builder,
and document the provider matrix as a 1:1 mirror of ADR-034's table.

```go
type ToolDefinition struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`

    // Strict enables OpenAI's strict-mode tool calling: the model is
    // constrained to emit tool_calls[].function.arguments that conform
    // to Parameters. Symmetric to ResponseFormat.Strict — same provider
    // table applies. Requires Parameters to satisfy OpenAI's strict-mode
    // subset (additionalProperties:false at every object level, every
    // property in required, no $ref/anyOf at the root, max nesting 5).
    Strict bool `json:"strict,omitempty"`
}
```

`omitempty` keeps the field absent from the wire for callers that
don't set it — existing tool-using sites see no behavior change.

## Provider matrix (mirrors ADR-034 §Decision)

| Provider | Behavior |
|---|---|
| `provider:"openai"` umbrella (OpenAI proper / vLLM / sparky / OpenRouter / LocalAI / llama.cpp `/v1` / LiteLLM / any OpenAI-compat) | honors `function.strict: true`; samples are constrained by xgrammar/outlines or equivalent. **Principal value of this ADR.** |
| `provider:"ollama"` (`/v1`) | best-effort; model-dependent. Per ADR-034 §gh#10001, gemma3 ignores. We pass through; operators verify on their model. |
| `provider:"gemini"` (OpenAI-compat) | silent no-op. Setting Strict produces no error and no sampling guarantee — operators get false confidence. |
| `provider:"anthropic"` | silent no-op. Anthropic's *native* structured-output primitive IS forced tool-calling with the tool schema as the constraint, accessed via the standard tools array — but the OpenAI-compat shim doesn't surface a strict flag. Operators relying on strict on Anthropic should switch to a native Anthropic client when one is added. |

semstreams routes everything through the OpenAI shim (`go-openai`).
Providers that don't honor `function.strict` silently drop it on
their side; there's no functional difference between sending and
clearing it. This ADR keeps the framework's wire-side handling
provider-agnostic — adapters (`NormalizeRequest` hook) MAY clear or
warn for known no-op providers, but we ship without that for v1
because the asymmetry would be jarring (`applyResponseFormat` itself
doesn't warn on no-op providers either).

A future observability ADR can add the warn-on-no-op-provider pattern
symmetrically across `response_format` and tool `Strict` if operators
ask for it.

## Per-adapter v1 behavior

| Adapter | Behavior | Notes |
|---|---|---|
| `OpenAIAdapter` | No-op for `Function.Strict`. Plumbing happens in `client.go` and the SDK's native `Strict bool` serializes to OpenAI wire shape correctly. | Covers the umbrella set. |
| `OllamaAdapter` | No-op for `Function.Strict`. Ollama `/v1` accepts the field and forwards model-dependently. | Same posture as `response_format` per ADR-034. |
| `GeminiAdapter` | No-op for `Function.Strict`. Field is silently dropped by Gemini's OpenAI-compat layer. | Operator caveat: setting Strict provides false confidence on Gemini. |
| `GenericAdapter` | No-op. | Anthropic-bound traffic falls here today (no `AdapterFor("anthropic")` case); see future-work note below. |

## Caller responsibilities

The strict-mode subset is enforced server-side, not by the framework:

- `additionalProperties: false` at every object level
- Every declared property listed in `required` (or marked nullable in
  the schema for optional fields)
- No `$ref` or `anyOf` at the root (per OpenAI's documented constraints)
- Max nesting depth 5

Schemas that don't satisfy the subset return a 400 from upstream when
Strict is set. This is a caller-side bug — surfaces clearly to the
operator. The framework does not validate the subset because the
constraint set evolves on OpenAI's side; pinning a validator would
drift.

semspec commit `d3a3b56` lands `additionalProperties:false` across
the eight deliverable schemas. The required-completeness half is
gated behind `TestSchemasRequiredCompleteness` (currently `t.Skip`'d)
until live-LLM validation flips. semspec opts the field on once that
gate is green.

## Implementation

Three changes, ~15 lines:

1. `agentic/tools.go` — add `Strict bool` field with doc.
2. `processor/agentic-model/client.go` — set `Function.Strict: tool.Strict`
   in the tool conversion loop.
3. `processor/agentic-model/tool_strict_test.go` — three wire-shape tests
   (true propagates, false omitted, mixed-per-tool).

No constructor (e.g. `NewStrictToolDefinition`) — `ToolDefinition` is
struct-literal-initialized everywhere, and a constructor only for
strict-mode would be inconsistent. semspec wires Strict through their
own builder.

No migration: `omitempty` makes it purely additive. No CHANGELOG entry
needed beyond the regular tag note.

## Future work

- **Native Anthropic adapter.** Anthropic's tool-calling is the
  default structured-output primitive there; an explicit adapter
  could translate `Function.Strict` to Anthropic's native tool schema
  conformance (which is always strict in their API). Out of scope for
  v1.
- **Schema subset validator.** If callers repeatedly hit 400s from
  non-conforming schemas, a framework-side preflight validator could
  catch obvious violations before the request goes upstream. Tradeoff:
  the OpenAI subset evolves; a pinned validator drifts. Defer until
  the failure rate justifies it.
- **Observability for no-op providers.** Warn-once when Strict is set
  on `gemini`/`anthropic`-bound traffic. Should land symmetrically
  with the same pattern for `response_format`. Separate ADR.

## References

- ADR-034 — companion `response_format` decision; provider table this
  one mirrors
- semspec commit d3a3b56 — strict-mode structural subset
  (`additionalProperties:false` across schemas)
- semspec commit 133b5ea — `terminal.EndpointSupportsResponseFormat`
  discriminator (downstream concern; extends 1:1 to gate
  `ToolDefinition.Strict` per-endpoint without further framework
  changes)
- go-openai `chat.go:369` — SDK `FunctionDefinition.Strict` (v1.41+)
- OpenAI Structured Outputs docs — strict-mode subset constraints
