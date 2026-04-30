# Migration Guide: beta.29 → beta.30

## Summary

Beta.30 closes a silent **deterministic-16384-truncation** gap by
adding an opt-in `max_output_tokens` field to model registry
endpoints. When `AgentRequest.MaxTokens` is unset, the endpoint
default fills it in. Both unset preserves prior behaviour exactly —
the field is omitted from the wire and the provider's own implicit
cap applies.

| Surface | Status |
|---|---|
| New `EndpointConfig.MaxOutputTokens` field | **Additive** — opt-in per endpoint |
| Existing `MaxTokens` (context window) field | **Unchanged** |
| `AgentRequest.MaxTokens` per-request override | **Unchanged** — still wins |
| Wire output when both unset | **Unchanged** — `max_tokens` omitted |
| Validation | **New** — rejects negative values |
| Streaming path | **Covered** — same `buildChatRequest` entry point |

**The simplest beta.29 → beta.30 upgrade is to do nothing.** Existing
deployments see bit-identical wire output. The fix activates only
when an operator sets `max_output_tokens` on an endpoint or a
component passes `MaxTokens` on its `AgentRequest`.

## What problem does this solve

Some OpenAI-compatible providers (notably gateway services that
proxy multiple upstreams) apply a low implicit `max_tokens` cap when
the client omits the field — most commonly **16384**. The model
itself may produce far more output when given an explicit cap, but
without one the response is silently truncated at the gateway's
default. agentic-loop sees `finish_reason=length` and (post-beta.21)
either fails the loop or compacts and retries, but the underlying
cause — the gateway, not the model — was invisible to operators.

A direct curl probe with explicit `max_tokens` against the same
endpoint produced full-length output, isolating the cap to the
gateway's omit-default behaviour.

## What changes

### `model.EndpointConfig.MaxOutputTokens`

New `int` field, JSON-tagged `max_output_tokens,omitempty`. Sits next
to the existing `MaxTokens` (context window) field with a doc
comment calling out the distinction:

```jsonc
{
  "endpoints": {
    "claude-via-gateway": {
      "provider": "openai",
      "url": "https://gateway.example.com/v1",
      "model": "claude-sonnet-4-6",
      "max_tokens": 200000,         // context window — already existed
      "max_output_tokens": 64000,   // NEW — per-request output cap
      "api_key_env": "GATEWAY_KEY",
      "supports_tools": true
    }
  }
}
```

Validation rejects negative values, mirroring `max_tokens`. Zero
(default) means "no endpoint default — let the provider decide,"
which is bit-identical to pre-beta.30 behaviour.

### `processor/agentic-model/client.go` precedence

`buildChatRequest` adds one fallback line:

```go
if req.MaxTokens > 0 {
    chatReq.MaxTokens = req.MaxTokens
} else if c.endpoint.MaxOutputTokens > 0 {     // NEW
    chatReq.MaxTokens = c.endpoint.MaxOutputTokens
}
```

Precedence order:

1. **Per-request `AgentRequest.MaxTokens`** (existing, unchanged) — a
   non-zero value always wins. Components that need explicit per-call
   control keep working exactly as before.
2. **Endpoint `MaxOutputTokens`** (new) — applied when (1) is unset.
3. **Field omitted from wire** — when both are unset. Provider's own
   default applies.

The fix flows through both streaming and non-streaming code paths
because both call `buildChatRequest`.

## What you should do

**For most deployments: nothing.** Pull beta.30 and existing
behaviour is bit-stable.

**If you've hit truncation at exactly 16384 completion tokens** on
an OpenAI-compatible gateway:

1. Identify the offending endpoint by name.
2. Set `max_output_tokens` to a value within the model's actual
   output capability (often 32000–64000 for current frontier models).
3. Restart the affected component or rely on the model registry
   hot-reload path (`config.Manager.WatchModelRegistry`, beta.14+) to
   pick up the change without a full restart.
4. Verify by checking that completion responses pass 16384 tokens
   when the prompt warrants it.

**If you author components that issue `AgentRequest`s directly**,
nothing required — `req.MaxTokens` continues to win whenever set.
Only the empty path now consults the endpoint default.

## What didn't change

- `MaxTokens` on `EndpointConfig` is still the **context window**
  field used for routing (largest-window summarization picks) and
  context-budget math in agentic-loop. Do not conflate.
- `AgentRequest.MaxTokens` semantics are identical: per-request
  output cap, wins when non-zero.
- Anthropic, Ollama, OpenAI, and OpenRouter providers all consume
  the same OpenAI-compatible `max_tokens` request field, so the
  fallback applies uniformly.
- Existing model registry JSON round-trips byte-stable (the new
  field is `omitempty`).
- Schema generation (`task schema:generate`) produces no diff —
  `model.Registry` is loaded directly via JSON and is not part of
  the generated component schema set.

## Verification

After upgrading:

- `go build ./...` succeeds.
- `go test -race ./model/ ./processor/agentic-model/` passes —
  including the four new tests:
  - `TestValidate/endpoint_negative_max_output_tokens` (table-driven)
  - `TestBuildChatRequest_MaxOutputTokens_RequestWinsOverEndpoint`
  - `TestBuildChatRequest_MaxOutputTokens_EndpointDefaultApplied`
  - `TestBuildChatRequest_MaxOutputTokens_NeitherSetOmitsField`
- `task lint` reports 0 revive warnings.
- For the gateway-truncation case, completion responses on the
  affected endpoint now exceed 16384 tokens when the model produces
  longer output.

## Related

- [migration-beta20-to-beta21.md](migration-beta20-to-beta21.md) —
  the truncation handling overhaul that pre-supposed an explicit
  cap was being sent. Beta.30 closes the missing-cap path that
  beta.21 couldn't see.
- [docs/operations/08-llm-truncation-handling.md](08-llm-truncation-handling.md)
  — context utilization branch (Case A vs Case B) when
  `finish_reason=length` fires.
- [docs/operations/04-ollama-setup.md](04-ollama-setup.md) — context
  for the existing `MaxTokens` field's Ollama-specific note (which
  is about `num_ctx`, not the per-request output cap addressed
  here). Ollama's OpenAI-compatible layer maps `max_tokens` →
  `num_predict`, so `max_output_tokens` works as expected on Ollama
  endpoints.
