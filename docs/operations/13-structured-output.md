# Structured Output via `response_format`

## What this gives you

A reliable JSON-schema-constrained output primitive on small/local LLM
deployments. Pass `agentic.AgentRequest.ResponseFormat` and the
framework plumbs it to the provider's structured-output mechanism.
Nil keeps the current behavior (tool-calling remains the
structured-output primitive when no `ResponseFormat` is set). See
[ADR-034](../adr/034-structured-output-response-format.md) for the
full design.

## Provider-string umbrella

Operators routing to **vLLM, sparky, LocalAI, llama.cpp server,
LiteLLM, anything OpenAI-API-compatible** set `provider: "openai"`
plus the appropriate URL. The framework deliberately does not
fragment the provider enum by runtime flavor — every OpenAI-compat
runtime takes the same wire shape.

The two genuine outliers worth keeping distinct from `openai`:

| `provider:` | Runtime | Why distinct |
|-------------|---------|--------------|
| `openai` | OpenAI proper, vLLM, sparky, LocalAI, llama.cpp server, OpenRouter-compat routes, anything OpenAI-API-shaped | Default. `response_format` rides the SDK's native field. |
| `ollama` | Ollama (`localhost:11434`) | Inherits `GenericAdapter` cross-provider message normalizations; `response_format` flows through `/v1` (Ollama's documented workaround for the missing native-format field on `/v1`). |
| `gemini` | Google Gemini OpenAI-compat endpoint | Distinct API surface; `response_format` is no-op'd today. Stretch goal: native Gemini client + `responseSchema` translation. |

## Wire shapes by provider

| Provider | `response_format` honored on the wire? | Notes |
|---|---|---|
| OpenAI proper | Yes | Reference implementation; `json_schema` strict-mode is the canonical guarantee. |
| vLLM, sparky, OpenRouter, LocalAI | Yes (via `provider: "openai"`) | All hit `/v1/chat/completions`; xgrammar/outlines under vLLM enforce schema during decoding. |
| Ollama (`provider: "ollama"`) | Yes — but model-dependent | `/v1` accepts the OpenAI shape (Ollama's documented workaround). gh#10001 reports gemma3 ignores it; live integration test confirms qwen3:1.7b honors it. |
| Gemini | Not yet | OpenAI-compat endpoint doesn't honor `response_format`. Future Gemini-native adapter would translate to `responseSchema`. |
| Anthropic (default fallback) | Not yet | Tool-calling with strict argument schemas is the recommended primitive; future translation to forced single-tool-call. |

## Building a `ResponseFormat`

Two helpers in the `agentic` package:

```go
// JSON-schema-constrained — the strict mode. Strict defaults to true
// (the OpenAI Structured Outputs guarantee).
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "action": map[string]any{
            "type": "string",
            "enum": []any{"fan_out", "synthesize", "done"},
        },
        "args": map[string]any{"type": "object"},
    },
    "required":             []any{"action", "args"},
    "additionalProperties": false,
}
rf := agentic.NewJSONSchemaFormat("decide_action", schema)

// Bare JSON validity, no schema. Less reliable on small models;
// prefer the schema variant when a schema is available.
rf := agentic.NewJSONObjectFormat()
```

Then pass to the request:

```go
req := agentic.AgentRequest{
    RequestID: "...",
    Model:     "...",
    Messages:  []agentic.ChatMessage{ /* ... */ },
    ResponseFormat: rf,
}
resp, err := client.ChatCompletion(ctx, req)
// resp.Message.Content parses as schema-conformant JSON.
```

`AgentRequest.Validate()` rejects an invalid `ResponseFormat` (missing
`Name`, missing `Schema`, unknown `Type`) at the boundary; downstream
plumbing trusts the caller.

## Schema constraints (OpenAI strict-mode subset)

The schema you pass must satisfy OpenAI's strict-mode subset of JSON
Schema:

- No `$ref` to external schemas.
- No `anyOf` at the root.
- Every property listed in `required`.
- `additionalProperties: false` at every object level.
- Supported keywords: `type`, `properties`, `items`, `required`,
  `enum`, `description`, `additionalProperties`, `minimum`, `maximum`.

The framework does not validate the schema locally — invalid schemas
return HTTP 400 from the provider with a schema validation error. If
the caller passes an unmarshalable Go value (channel, func, cyclic
struct) inside the schema map, the framework drops the schema and
emits a `Warn` log naming the request_id; the upstream returns a
generic schema error and the `Warn` is the proximate root-cause
signal.

## When to opt into `ResponseFormat`

- **Yes**: small-model deployments (qwen3 7B/14B, deepseek-r1, gemma3,
  any sub-30B model) where tool-call argument JSON occasionally drifts
  off the persona's `action_allowlist`. Schema-constrained decoding
  via `response_format` is more reliable than the
  schema-aligned-parsing (SAP) coercion in beta.44.
- **Yes**: sub-agent handoffs where a downstream rule must match on
  structured fields (semspec's coordinator → sub-agent → ops layer
  pattern). The schema's `required` + `additionalProperties: false`
  keep the field set stable across model revisions.
- **No**: cloud-provider deployments (Anthropic Claude, OpenAI
  proper) where tool-calling already returns reliable structured
  arguments. The current tool-call path is fine; adding
  `response_format` is overkill.

## Failure modes

The framework's response-parsing layer stays defensive even when
`response_format` is set:

- Schemas that violate strict-mode get HTTP 400 from the provider.
- Older OpenAI-compat servers (vLLM <0.6, very old Ollama) may
  silently emit unconstrained output; the existing malformed-JSON
  fallback at `processor/agentic-model/client.go:602` covers this.
- Ollama models without grammar support (rare finetunes) silently
  ignore the constraint; same fallback applies.

If the model returns `Status: length_truncated`, the budget was too
small for the schema-conformant output to fit. Bump `MaxTokens`.
Thinking models (qwen3, deepseek-r1) easily consume 500+ tokens of
`<think>` reasoning before producing the user-visible JSON; budget
for that.

## Verifying it works

There's a build-tagged live integration test against Ollama:

```bash
# Requires Ollama running at localhost:11434.
go test -tags=live_llm \
   -run TestResponseFormat_Integration \
   ./processor/agentic-model/...

# Override the model:
OLLAMA_TEST_MODEL=qwen3:14b go test -tags=live_llm \
   -run TestResponseFormat_Integration \
   ./processor/agentic-model/...
```

Three subtests cover: schema-constrained output, bare-JSON-object
mode, and the no-`ResponseFormat` baseline (regression guard for the
`OllamaAdapter` no-op path). Skips with a clear message when Ollama is
not running. Use this when adding a new model size to confirm `/v1`
honors `response_format` for it before deploying.
