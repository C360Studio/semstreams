# Migration Guide: beta.41 → beta.42

## Summary

Beta.42 bundles three small, disjoint observability + connection-hygiene
fixes asked for by semspec and semteams:

1. **Trace ID propagation** — `agentic-dispatch` stamps the inbound
   request's trace_id onto `TaskMessage.Metadata`, which `agentic-loop`
   propagates onto `LoopEntity.Metadata`. Wedge investigation collapses
   to: `curl /loops/<id>` → `metadata.trace_id` →
   `curl /message-logger/trace/<id>`.
2. **graph-query keepalive propagation** — `model.ResolveEndpoint`
   silently strips the `EndpointConfig`'s connection-hygiene fields
   (`DisableKeepAlives`, `IdleConnTimeout`, `ResponseHeaderTimeout`).
   graph-query's two LLM clients now read the full `*EndpointConfig`
   so operator-configured keepalive settings actually take effect.
3. **sparky/vLLM keepalive doc** — added a recommended-bindings row
   for local inference servers (sparky, vLLM, llama.cpp) that idle-kill
   connections silently. semspec observed wedge-after-8-turns shape
   against these backends; `disable_keepalives: true` is the right
   default for the local-inference class.

| Surface | Status |
|---|---|
| `LoopEntity.Metadata["trace_id"]` (via `TaskMessage.Metadata`) | **Additive** — empty when no trace context on inbound ctx |
| New constant `MetadataKeyTraceID = "trace_id"` in agentic-dispatch | **Additive** |
| graph-query's `LLMClassifier` and `LLMAnswerSynthesizer` honour `disable_keepalives` etc. | **Bug fix** — was silently dropped |
| `docs/operations/12-openai-client-keepalive.md` gains local-inference recommendation | **Doc** |

**The simplest beta.41 → beta.42 upgrade is to do nothing.** Existing
deployments inherit the trace_id stamp automatically (when the inbound
ctx carries trace context — most production paths do, instrumented at
every NATS Subscribe site). graph-query operators who already had
`disable_keepalives: true` in their config now actually get the
behaviour. semspec users running sparky/vLLM should add
`disable_keepalives: true` to those endpoints.

## What's new

### 1. Trace ID propagation (semspec ask)

`agentic-dispatch` extracts the inbound trace_id via
`natsclient.TraceContextFromContext(ctx)` and writes it to
`TaskMessage.Metadata["trace_id"]` before publishing. The downstream
`agentic-loop` already copies `TaskMessage.Metadata` onto
`LoopEntity.Metadata` at creation time, so `/loops/{id}` responses
include the trace_id automatically.

**Investigation use case**:

```bash
# Find the wedged loop
curl http://agentic-loop/loops/loop_a1b2c3d4
# → { ..., "metadata": { "trace_id": "0af7651916cd43dd8448eb211c80319c", ... } }

# Pull every NATS message in that trace
curl http://message-logger/trace/0af7651916cd43dd8448eb211c80319c
# → full per-message history that led to the wedge
```

**Don't-clobber semantics**: a workflow command that explicitly sets
`task.Metadata["trace_id"]` (e.g., to attribute spawned work to a
parent trace rather than the inbound submission's trace) keeps its
value through the dispatch path.

**No-op semantics**: if the inbound ctx has no trace context (synthetic
in-process dispatch, or an upstream that hasn't been instrumented),
the stamp is silently skipped. Metadata stays nil; `/loops/{id}` simply
won't carry a trace_id field.

### 2. graph-query keepalive propagation (semspec ask)

`processor/graph-query/component.go:initLLMClassifier` and
`initAnswerSynthesizer` previously called `model.ResolveEndpoint`
which returns a minimal `*ResolvedEndpoint` (URL, Model, APIKey only).
The `*EndpointConfig`'s `DisableKeepAlives`, `IdleConnTimeout`, and
`ResponseHeaderTimeout` fields were silently dropped on the way to
`llm.NewOpenAIClient`.

The fix: a new local helper `resolveEndpointWithConfig` that returns
both the resolved trio (preserving the env-key resolution that lives
in `model.ResolveEndpoint`) AND the full `*EndpointConfig`, so the
LLM client builder gets the keepalive fields.

**Operator impact**: any graph-query deployment that had
`disable_keepalives: true` configured on its `query_classification` or
`answer_synthesis` endpoints now actually honours it. Sustained-load
runs against keepalive-hostile gateways stop wedging on stale-pooled
connections.

### 3. sparky/vLLM keepalive recommendation (semspec ask)

`docs/operations/12-openai-client-keepalive.md` gained a row in the
recommended-bindings section for local inference servers:

```yaml
endpoints:
  sparky-qwen:
    provider: openai
    url: http://sparky:8080/v1
    model: qwen3-coder:30b
    disable_keepalives: true
```

Rationale: local inference servers (sparky, vLLM, llama.cpp) idle-kill
connections aggressively and often silently. `disable_keepalives: true`
sidesteps the entire wedge class at sub-millisecond cost on the
loopback/private-network path. Operators who want to preserve keepalive
(HTTP/2-supporting backends with proper FIN handling) leave the field
unset and tune `idle_conn_timeout` to 5-10s instead.

## Migration steps

### Existing operators

No required action. Behavioural changes are all additive or bug-fix:

- trace_id stamp activates automatically when inbound ctx has trace
  context. Production paths instrumented at NATS Subscribe sites get
  it for free.
- graph-query keepalive fields now actually work as documented.
  Operators who had configured them already see the intended behaviour;
  operators who hadn't are unaffected.
- sparky/vLLM users: add `disable_keepalives: true` to those endpoints
  if you've been seeing wedge-after-N-turns shape.

### External consumers of `LoopEntity.Metadata`

`metadata.trace_id` is now a stable field (when present). Consumers
that surface metadata via UI / dashboard can add a "View trace" link
keyed on this field.

## Backward compatibility

- `LoopEntity` schema: unchanged. `Metadata` was already `map[string]any`;
  the new key is additive.
- `TaskMessage` schema: unchanged. Same situation.
- graph-query endpoint config: unchanged. Existing fields now actually
  work.
- No breaking changes anywhere.

## Cross-references

- `processor/agentic-dispatch/trace_metadata.go` — trace_id stamp helper
- `processor/agentic-dispatch/component.go:handleTaskSubmission` —
  call site (NATS-originated dispatch)
- `processor/agentic-dispatch/http.go:processTaskSubmissionSync` —
  call site (HTTP-originated dispatch)
- `processor/agentic-dispatch/trace_metadata_test.go` — five regression
  tests covering happy path, no-trace, empty-trace, don't-clobber,
  nil-Metadata-init
- `processor/graph-query/component.go:resolveEndpointWithConfig` —
  the helper that plumbs keepalive fields past `ResolveEndpoint`
- `docs/operations/12-openai-client-keepalive.md` — sparky/vLLM
  recommendation
- semspec ask: trace_id in LoopEntity.Metadata (the empirical case
  study)
- semspec ask #10: graph-query DisableKeepAlives propagation
- semspec ask #7: sparky/vLLM keepalive
