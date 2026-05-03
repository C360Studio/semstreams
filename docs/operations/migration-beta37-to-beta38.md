# Migration Guide: beta.37 → beta.38

## Summary

Beta.38 closes a class of wedge in graph-query where a slow upstream LLM
could consume the entire HTTP gateway request budget, causing the
existing template / keyword fallback path to land after the gateway had
already returned an error to the client. semspec hit this running
seminstruct as the `answer_synthesis` model. The fix adds a bounded
sub-timeout around each affected LLM call so the fallback stays
transparent to the HTTP layer.

| Surface | Status |
|---|---|
| `LLMAnswerSynthesizer` per-call sub-timeout (15s default) | **Behavioural — transparent to callers** |
| `LLMClientAdapter` (query classifier) per-call sub-timeout (5s default) | **Behavioural — transparent to callers** |
| `NewLLMAnswerSynthesizer` signature: `+timeout time.Duration` | **BREAKING** — only consumed in-tree (zero external callers detected) |
| `NewLLMClientAdapter` signature: `+timeout time.Duration` | **BREAKING** — only consumed in-tree (zero external callers detected) |
| `DefaultAnswerSynthesisTimeout` exported (was unexported) | **API addition** |
| `DefaultClassificationTimeout` already exported | **API addition** |
| Operator-tunable via `capability.timeout` and `endpoint.request_timeout` in the model registry | **Additive** — existing fields wired into a new resolution path |

**The simplest beta.37 → beta.38 upgrade is to do nothing.** Existing
operators see the bounded sub-timeout applied with sensible defaults;
the existing fallback paths (template synthesis, keyword classifier)
remain unchanged. No config changes are required to benefit from the
fix.

## The bug

`processor/graph-query/answer.go:Synthesize` and
`graph/query/classifier_llm_adapter.go:ClassifyQuery` both passed the
inherited request `ctx` directly to `ChatCompletion`. Both call sites
sit inside `handleGlobalSearch` (synchronous request-reply). With a
slow upstream LLM (e.g. seminstruct on heavy `answer_synthesis`
prompts), the LLM call could consume the entire gateway HTTP request
budget. The existing fallback path (template synthesis or keyword
classifier) then ran after the gateway had already returned an error
to the HTTP client — the fallback was locally correct, end-to-end
broken.

## The fix

Both LLM calls now run under a sub-context bounded by an
operator-configurable timeout, with sensible defaults sized to leave
substantial budget for the rest of the response path under typical
30-60s gateway HTTP request deadlines:

- **answer_synthesis**: default 15s (`DefaultAnswerSynthesisTimeout`)
- **query_classification**: default 5s (`DefaultClassificationTimeout`)

When the sub-timeout fires, the existing fallback path runs and the
response reaches the HTTP layer cleanly. No errors propagate; the
caller-facing contract (nil error on fallback) is unchanged.

## Audit scope

The fix is **scoped to graph-query's two LLM call sites**. An audit
across every `ChatCompletion` call in the codebase confirmed:

- `processor/agentic-dispatch/intent_classifier.go` and
  `normalize_extractor.go` already wrap with `context.WithTimeout`.
- `processor/agentic-loop/summarizer.go` (compaction),
  `graph/clustering/summarizer.go` (community summary worker), and
  `graph/inference/review_worker.go` (anomaly review worker) are not
  on the synchronous gateway path — they operate inside the agent
  loop or as background batch workers with their own budgets.

So this is not a systemic pattern across the codebase; agentic-dispatch
already got it right, and the background workers have a different
shape. The gap was specifically graph-query's two call sites.

## Configuration

To override the defaults per-endpoint or per-capability, set in the
model registry:

```yaml
capabilities:
  answer_synthesis:
    preferred: [seminstruct]
    timeout: "30s"          # overrides DefaultAnswerSynthesisTimeout
  query_classification:
    preferred: [qwen-fast]
    timeout: "10s"          # overrides DefaultClassificationTimeout

endpoints:
  seminstruct:
    provider: openai
    url: http://seminstruct:8080/v1
    model: gpt-oss-120b
    request_timeout: "20s"  # most-specific; overrides capability.timeout
```

Resolution order (matches `processor/agentic-model/component.go:resolveTimeout`):

1. `endpoint.request_timeout` — most specific
2. `capability.timeout` — applies to anything routed via the capability
3. Framework default (`DefaultAnswerSynthesisTimeout` /
   `DefaultClassificationTimeout`)

Invalid duration strings at any level log a warning and fall through
to the next level — a malformed config never blocks startup.

## Migration steps

### Existing operators

No action required. Existing deployments inherit the framework
defaults and the bounded fallback automatically. If your upstream LLM
legitimately takes longer than the default (e.g. a reasoning model
synthesizing complex graph state), raise `capability.timeout` for the
relevant capability; the existing field is now wired into the new
resolution path.

### External callers of the affected constructors

`NewLLMAnswerSynthesizer` and `NewLLMClientAdapter` both gained a new
trailing `time.Duration` argument. Existing call sites in this repo
have been updated. If your code constructs these directly (rare —
`graph-query` is the only consumer in production), pass `0` to select
the framework default:

```go
// Before:
synth := graphquery.NewLLMAnswerSynthesizer(client, modelName, logger)
adapter := query.NewLLMClientAdapter(client)

// After:
synth := graphquery.NewLLMAnswerSynthesizer(client, modelName, logger, 0)
adapter := query.NewLLMClientAdapter(client, 0)
```

`0` selects the framework default. Pass an explicit duration if you
want a different bound.

## Backward compatibility

- Existing configs: unchanged behaviour with default sub-timeouts
  applied automatically.
- Existing rule prompts and tool calls: unchanged.
- The fallback contract (nil error on LLM failure) is preserved —
  callers see no observable change in error semantics; only the
  end-to-end HTTP latency on slow-LLM cases is bounded.

## Cross-references

- `processor/graph-query/answer.go:Synthesize` — answer synthesis
  bounded sub-timeout
- `graph/query/classifier_llm_adapter.go:ClassifyQuery` —
  classification bounded sub-timeout
- `processor/graph-query/component.go:resolveCapabilityLLMTimeout` —
  shared resolution helper for both call sites
- `processor/graph-query/answer_test.go:TestLLMAnswerSynthesizer_SubTimeout_FallsBackTransparently`
  — regression test asserting parent-ctx budget is preserved
- `graph/query/classifier_llm_adapter_test.go:TestLLMClientAdapter_SubTimeout_BoundsLLMCall`
  — same regression for the classifier path
- semspec post-mortem: seminstruct as `answer_synthesis` model on slow
  graph-query responses (the empirical case study)
