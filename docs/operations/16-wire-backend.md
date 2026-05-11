# Wire-Format Client Backend (ADR-037)

This guide covers the per-endpoint `wire_backend` opt-in flag added in
ADR-037 — the framework-owned ChatCompletion / Embeddings client that
replaces the `sashabaranov/go-openai` SDK on a per-endpoint schedule.

## Why a self-hosted client

The SDK ships a fixed schema. The OpenAI ChatCompletion wire shape
keeps adding provider-specific fields (Gemini's
`extra_content.google.thought_signature`, Anthropic's `cache_control`,
Ollama's native `format`) that the SDK either doesn't model or
silently drops. The wire package preserves every unmodeled field via
typed `Extras map[string]json.RawMessage` carriers so adapters can
translate provider quirks without losing data on round-trip.

See `docs/adr/037-self-hosted-llm-wire-package.md` for the full
rationale, design alternatives, and migration plan.

## Configuration

Each endpoint in the model registry can opt into the wire client:

```yaml
endpoints:
  gemini-3x:
    provider: gemini
    url: https://generativelanguage.googleapis.com/v1beta/openai
    model: gemini-3.0-pro-preview
    api_key_env: GEMINI_API_KEY
    wire_backend: wire   # opt-in; default is "sdk"
```

Valid values:

| Value     | Behaviour                                                            |
| --------- | -------------------------------------------------------------------- |
| `""`      | Default — uses the `sashabaranov/go-openai` SDK client.              |
| `"sdk"`   | Explicit SDK selection. Same as `""`.                                |
| `"wire"`  | Uses `model/wire` client. Required for Gemini 3.x thought_signature. |

Any other value fails registry validation at config-load time.

## Rollout discipline

Per ADR-037 §"Calendar soak":

1. Flip ONE endpoint to `wire_backend: wire` per beta tag.
2. Soak for **≥7 calendar days** before flipping the next endpoint.
3. After every adapter has run on `wire` in production for ≥7 days,
   the framework default flips from `"sdk"` to `"wire"`.
4. **≥30 calendar days** post-last-flip before the SDK is retired from
   `go.mod` (Phase 3).

Rollback is a one-line config change — flip `wire_backend` back to
`"sdk"` and the SDK client takes over on the next request.

## Gemini 3.x thought_signature flow

Gemini 3.x preview models emit a per-tool_call signature at
`response.choices[].message.tool_calls[].extra_content.google.thought_signature`
and require the same signature echoed back on the assistant
`tool_calls` of subsequent requests in the same conversation. Without
the echo, multi-turn tool flows fail.

The wire backend handles this automatically when both conditions hold:

1. `endpoint.provider: gemini` (selects `GeminiAdapter`).
2. `endpoint.wire_backend: wire` (selects the wire client).

The carrier path:

```text
Gemini response
  → wire.ToolCall.Extras["extra_content"]
  → GeminiAdapter.NormalizeResponse:
        extract google.thought_signature
        → wire.ToolCall.Extras["c360_thought_signature"]
  → convertWireResponse:
        carrier → agentic.ToolCall.Metadata[MetadataKeyGoogleThoughtSignature]

(loop stores response in conversation history)

Next request
  → agenticToolCallsToWire:
        Metadata → wire.ToolCall.Extras["c360_thought_signature"]
  → GeminiAdapter.NormalizeMessages:
        carrier (first tool_call only) → wire.ToolCall.Extras["extra_content"]
  → stripC360KeysFromRequest:
        delete any remaining framework-internal carrier (defense in depth)
  → wire HTTP send
```

Gemini's "first call per step" contract is enforced by
`rebuildGeminiThoughtSignature`: when an assistant message contains
multiple tool_calls, only the first carries the signature.

The carrier key `c360_thought_signature` is framework-internal and
never escapes onto the wire — `stripC360KeysFromRequest` runs after
`NormalizeRequest` to ensure it's removed if a fallback path didn't
consume it.

## Live test

A `live_llm`-tagged test exercises the full round-trip against the
Gemini 3.x preview:

```bash
GEMINI_API_KEY=... \
go test -tags live_llm \
  -run TestGemini3x_ThoughtSignature_RoundTrip \
  ./processor/agentic-model/...
```

Optional environment variables:

- `GEMINI_TEST_MODEL` — override the preview model name
  (default: `gemini-3.0-pro-preview`).

The test:

1. Sends a tool-using prompt.
2. Asserts the response carries a non-empty thought_signature in
   `agentic.ToolCall.Metadata`.
3. Sends a follow-up turn with the tool result and the replayed
   assistant tool_call.
4. Asserts the follow-up succeeds (Gemini 3.x rejects multi-turn tool
   flows with missing or stale signatures).

If step 2 reports an empty signature, the model under test is not a
3.x preview build — the rest of the round-trip still exercises the
generic wire path correctness.

## Operator runbook

### Symptom: tool flows fail on Gemini 3.x after upgrading

Check:

```bash
# Confirm wire_backend is set on the gemini endpoint.
yq '.endpoints[] | select(.provider == "gemini") | .wire_backend' \
  model-registry.yaml
```

If empty or `"sdk"`, flip to `"wire"` and reload. The framework only
implements the signature flow on the wire path.

### Symptom: framework-internal `c360_*` key appears in upstream provider logs

Should not happen — `stripC360KeysFromRequest` removes them
unconditionally. If observed, file a bug with the endpoint config and
the captured request body; this likely means a code path skipped
`buildWireRequest` and constructed the request directly.

### Rollback

```yaml
# In model-registry.yaml:
endpoints:
  gemini-3x:
    wire_backend: sdk   # or remove the line entirely
```

Reload the config. The SDK client takes over on the next request.
Note: SDK path does NOT implement the thought_signature flow —
multi-turn tool flows on Gemini 3.x preview WILL fail after rollback.

## Related

- ADR-037 — Self-hosted LLM wire-format package.
- `processor/agentic-model/adapter_gemini.go` — Gemini adapter.
- `processor/agentic-model/client_wire.go` — Wire client wiring.
- `model/wire/` — Wire types, client, streaming, error decoding.
- `docs/operations/10-provider-adapter-normalization.md` — Adapter
  layer overview.
