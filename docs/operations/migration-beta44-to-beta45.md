# Migration Guide: beta.44 → beta.45

## Summary

Beta.45 closes the graph-query degraded-response gap semspec hit
under load: when LLM answer synthesis times out (or fails for any
other reason), the response now reaches the agent with a `degraded:
true` flag plus a useful template-synthesized answer + raw entity
hits + community summary text — instead of the agent receiving
nothing or seeing the request fail upstream.

| Surface | Status |
|---|---|
| New `GlobalSearchResponse.Degraded bool` + `DegradedReason string` | **Additive** — set only when LLM fell back |
| New `LocalSearchResponse.Degraded` + `DegradedReason` | **Additive** |
| `AnswerSynthesizer` interface returns `SynthesisOutcome` struct instead of `(answer, model, err)` triple | **BREAKING for external implementers** — only graph-query implements; semspec doesn't subclass the interface |
| Existing `Answer` + `AnswerModel` fields | **Unchanged** — still populated; degraded answers still carry useful template text |
| GraphQL schema: `GlobalSearchResult.degraded` + `degraded_reason` exposed | **Additive** |
| Existing globalSearch / localSearch callers that don't read `degraded` | **Unaffected** — flag is opt-in to surface |

**The simplest beta.44 → beta.45 upgrade is to do nothing.**
Existing callers that ignore the new fields keep working. Callers
that want to surface the degraded path to the agent / human consumer
read `response.degraded` and `response.degraded_reason`.

## The wedge that motivated this

semspec ran graph-query under sustained load with seminstruct as the
`answer_synthesis` model. The bounded sub-timeout from beta.38
correctly fired when seminstruct couldn't answer in time and the
synthesizer fell back to template synthesis — the "template fallback"
log line told us so. **But the template fallback path didn't surface
through to the tool result.** From the agent's view: the
`graph_search` tool call returned EOF; the chain propagated the
failure upstream as if the search had broken.

The fallback path was building a useful response (template-synthesized
text + entity hits + community summary text from COMMUNITY_INDEX) but
no signal told the agent "this is the degraded path; trust the data
but know the rich LLM synthesis didn't materialize." Beta.45 surfaces
that signal explicitly.

## What's new

### `Degraded` flag on response shapes

`GlobalSearchResponse` and `LocalSearchResponse` gain two fields:

```go
type GlobalSearchResponse struct {
    // ... existing fields ...
    Answer         string `json:"answer,omitempty"`
    AnswerModel    string `json:"answer_model,omitempty"`
    Degraded       bool   `json:"degraded,omitempty"`        // NEW
    DegradedReason string `json:"degraded_reason,omitempty"` // NEW
}
```

`Degraded=true` means: an LLM-configured answer synthesizer was
present, the LLM call failed (timeout or error), and the response
fell back to template synthesis. The `Answer` field still carries
useful text (template-summarised entity hits + community keywords),
plus the existing `Entities` / `EntityDigests` /
`CommunitySummaries` fields are populated as usual.

`Degraded=false` means: synthesis succeeded (LLM produced the
canonical answer) OR no LLM was ever configured (template is the
canonical path for that deployment, not a fallback).

### `DegradedReason` classification

Only populated when `Degraded=true`. Concrete values:

| Value | Meaning |
|---|---|
| `"answer_synthesis_timeout"` | LLM call exceeded the bounded sub-timeout (seminstruct-under-load case) |
| `"answer_synthesis_cancelled"` | Parent ctx cancelled before the LLM call could complete (gateway client disconnected, surrounding handler bailed) |
| `"answer_synthesis_error"` | Other LLM error: transport failure, provider rejection, malformed response |

Operators group dashboards by `degraded_reason` to distinguish "model
is overloaded" (`_timeout`) from "client gave up" (`_cancelled`) from
"misconfiguration / upstream API broken" (`_error`). The three classes
have different operational responses — scale, route, or fix config.

### `SynthesisOutcome` struct (interface change)

`AnswerSynthesizer.Synthesize` now returns `(SynthesisOutcome, error)`
instead of `(answer, model string, err error)`. The struct shape:

```go
type SynthesisOutcome struct {
    Answer   string  // natural-language answer (LLM or template)
    Model    string  // "" for template fallback or template-only deployment
    Degraded bool    // true only when LLM-configured synthesizer fell back
    Reason   string  // populated when Degraded=true
}
```

This is a breaking change for anyone implementing the
`AnswerSynthesizer` interface externally. graph-query's two
implementations (`LLMAnswerSynthesizer`, `TemplateAnswerSynthesizer`)
are the only known consumers.

### GraphQL schema additions

`GlobalSearchResult` and `LocalSearchResult` gain `degraded` and
`degraded_reason` fields in the introspection schema. GraphQL clients
selecting these fields receive the new flags; clients that don't
select them are unaffected.

## How agents should consume the degraded flag

semspec's `graph_search` tool (and any equivalent agent-facing
wrapper) should:

1. Return the response to the agent regardless of `degraded` —
   the data IS useful.
2. Surface `degraded: true` in the tool result metadata so the
   agent's next iteration sees the signal.
3. Optionally annotate the answer text: e.g., wrap with "*(degraded
   response — answer is template-synthesized; no LLM available
   under load)*" so the agent treats the text as a summary rather
   than a definitive answer.

The framework pattern: **degraded responses are always preferable to
errors.** Returning useful-but-flagged data lets the agent keep
making progress; returning an error wedges the chain.

## Operator dashboards

Two metric joins worth setting up:

1. Group `coordinator.next_action` triples by source loop's
   `degraded` flag (via `metadata.degraded` propagated from the
   tool result) — measures how often agent decisions are made on
   degraded data.
2. Alert on `rate(degraded responses with reason="answer_synthesis_timeout") > 5%`
   for any role — signals model overload, time to scale or
   reroute.

(Both depend on agent-side propagation of the `degraded` flag onto
ToolCall metadata; semspec is the canonical consumer.)

## Backward compatibility

- Existing graph-query API consumers (HTTP / GraphQL / NATS): unchanged.
  Old clients ignore the new fields; new clients opt in to read them.
- Existing operators with no LLM configured (pure-template
  deployments): unchanged. `Degraded` stays false because the
  template IS the canonical answer for that deployment.
- Existing `AnswerSynthesizer` implementations OUTSIDE this repo
  (none known): break on the interface change. Migration: change
  return values from `(answer, model, err)` to
  `(SynthesisOutcome{Answer: answer, Model: model}, err)`.

## Migration steps

### graph-query API consumers

If your tool wraps `graph_search` / globalSearch / localSearch and
you want to surface degraded responses to the LLM agent: read
`response.degraded` and `response.degraded_reason` from the JSON
response body and add them to the ToolResult metadata.

```go
// Pseudocode — adapt to your actual wrapper.
result := agentic.ToolResult{
    Content: jsonResponse.Answer,
    Metadata: map[string]any{
        "entity_count":    len(jsonResponse.Entities),
        "duration_ms":     jsonResponse.DurationMs,
        "degraded":        jsonResponse.Degraded,
        "degraded_reason": jsonResponse.DegradedReason,
    },
}
```

### GraphQL clients

If you query `globalSearch { ... }` and want the new fields, add
them to the selection set:

```graphql
query Search($q: String!) {
  globalSearch(query: $q) {
    answer
    answer_model
    degraded            # NEW
    degraded_reason     # NEW
    entities { ... }
    community_summaries { ... }
  }
}
```

### Operators with no upstream changes

Set up the dashboard metric joins above and watch for sustained
`degraded=true` rates per role/model. The metric tells you which
upstream LLMs are struggling; the framework absorbs the failure
without wedging the chain.

## Cross-references

- `processor/graph-query/answer.go:SynthesisOutcome` — the new type
- `processor/graph-query/answer.go:LLMAnswerSynthesizer.Synthesize` —
  produces `Degraded=true` on LLM error/timeout, classifies via
  `errors.Is(err, context.DeadlineExceeded)`
- `processor/graph-query/graphrag.go:GlobalSearchResponse` /
  `LocalSearchResponse` — new fields on response shapes
- `processor/graph-query/graphrag.go:synthesizeQueryAnswer` — returns
  the outcome; four call sites (handleStrategyEntityLookup,
  handleStrategyGraphRAG semantic path, globalSearchTextBased,
  enrichGlobalResponse) propagate Degraded onto the response
- `gateway/graph-gateway/component.go:1518` — GraphQL schema
  `GlobalSearchResult` + `LocalSearchResult` gain `degraded` /
  `degraded_reason`
- semspec post-mortem: graph_search returning EOF under load
  (the empirical case study)
