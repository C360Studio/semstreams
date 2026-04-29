# Migration Guide: beta.27 → beta.28

## Summary

Beta.28 closes the v1-stub gap in `/onboard`'s answer
normalization (issue #11). Pre-beta.28, `NormalizeLayerAnswer`
was a deterministic stub that produced exactly one entry per
freeform answer (`title=first-line, summary=full-text`). All the
structured Entry fields the schema supported — `cadence`,
`trigger`, `inputs`, `stakeholders`, `constraints` — were
plumbed for nothing.

Beta.28 adds an LLM-assisted extraction path with stub fallback:
the dispatch component now calls a wired `LayerNormalizer` for
each user answer, and falls back to the original deterministic
stub on any failure (timeout, parse error, endpoint down,
truncated response, empty result). The end-to-end shape is now:

> "Mondays 9-10am planning, daily standups at 10, biweekly
> Friday reviews" → 3 structured entries with cadence + trigger
> populated, instead of one collapsed `title=Mondays 9-10am
> planning, summary=<all of it>` blob.

Additive surface; no API breakage. No data migration. Existing
deployments that don't wire a normalizer keep getting stub
behaviour identical to beta.27.

## What changes

### New `LayerNormalizer` function type

```go
type LayerNormalizer func(ctx context.Context, layer, answer string) ([]operatingmodel.Entry, error)
```

The dispatch component holds a `normalizerFn LayerNormalizer`
field. The default seeded by `NewComponent` is
`(*Component).normalizeLayerAnswerWithLLM` — an LLM-backed
extractor that mirrors `LLMIntentClassifier`'s pattern (resolve
endpoint via `model.RegistryReader`, call `agenticmodel.Client.
ChatCompletion`, parse JSON response).

A normalizer that returns `(nil, err)` or `(nil, nil)` triggers
fallback to the deterministic `NormalizeLayerAnswer` stub. The
fallback contract is intentionally generous — coarse stub entries
beat rejecting the user's answer because the LLM hiccupped.

### `(*Component).SetLayerNormalizer(fn)`

Public hook for tests and deployments that want to inject a
custom normalizer. Passing nil disables LLM normalization
entirely (forces stub fallback).

### LLM extraction prompt

Per-layer system prompts focus the model on the relevant Entry
fields:

| Layer | Focus fields |
|---|---|
| `operating_rhythms` | cadence, trigger |
| `recurring_decisions` | cadence, trigger, inputs |
| `dependencies` | stakeholders, inputs |
| `institutional_knowledge` | constraints |
| `friction` | constraints, stakeholders |

Response shape is a single root object (`{"entries": [...]}`)
rather than a bare array — friendlier to the existing
`extractJSON` helper for prose-wrapped model output.

### Truncation handling

A `finish_reason=length` response is treated as failure (forces
stub fallback) rather than parsing potentially malformed JSON.
This matches the beta.2 / beta.21 truncation-handling discipline.

### EntryID generation

The LLM is not trusted to produce dot-free EntryIDs. After
parsing, `finalizeExtractedEntries` always assigns a fresh
`om-{layer}-{uuid}` ID via the existing `entryID(layer)` helper,
applies SourceConfidence/Status defaults, and drops any entry
that fails `Entry.Validate()` (missing title or summary).

## What is NOT changing

- **`NormalizeLayerAnswer(layer, answer)` function signature** —
  unchanged. It remains the deterministic fallback. The pre-beta.28
  test (`TestNormalizeLayerAnswer_StubShape`) still passes.
- **`LayerApproved` payload shape** — unchanged. The new
  structured fields fit into the existing
  `operatingmodel.Entry` struct that was always plumbed for
  them.
- **`onboardingApproveLayer` write path** — unchanged. Whatever
  entries land in metadata get serialised through
  `LayerTriples` exactly as before; the only difference is
  there can now be more of them per layer with richer fields.
- **`/onboard` rejection-while-active behaviour** — unchanged.

## Operational impact

### Without configuration

A deployment that has a model registry wired (most do — the
intent classifier already requires one) automatically gets
LLM-backed normalization. The default capability name is
`"default"` and the registry's standard fallback chain handles
endpoint resolution.

A deployment with no model registry (or no resolvable
endpoint) silently falls back to the stub on every call. This
is logged at `Warn` so deployment-time miswire is observable.

### Cost / latency

One model call per layer (5 layers per onboarding interview).
`MaxTokens=1024`, `Timeout=30s`, `Temperature=0.1` —
parameters tuned for terse JSON extraction, not generation.

If the configured endpoint is health-gated by the
`endpoint_health_state` circuit breaker (beta.15), the
normalizer call respects the breaker and falls back to the
stub.

### Test ergonomics

Use `SetLayerNormalizer` to inject deterministic fixtures:

```go
c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
    return []operatingmodel.Entry{
        {Title: "Weekly planning", Summary: "Mondays 9-10am", Cadence: "weekly"},
    }, nil
})
```

Tests that don't call `SetLayerNormalizer` and don't go
through `NewComponent` get `c.normalizerFn == nil`, which
short-circuits to the stub — preserving existing test
behaviour.

## Verification

```bash
# Unit tests (includes new normalize_extractor_test.go)
go test -race ./processor/agentic-dispatch/...

# Lint
task lint

# Schema regen unchanged
task schema:generate
git diff schemas/ specs/openapi.v3.yaml
```

Manual: with a real Ollama endpoint wired, run `/onboard` and
answer with a multi-fact response (e.g. "I plan weekly on
Mondays, run daily standups at 10am, and have a biweekly
Friday review"). The checkpoint approval prompt should list
three distinct entries with `cadence`/`trigger` populated, not
one collapsed entry.

## Related

- GitHub issue: #11 (semteams)
- Plan: `~/.claude/plans/semteams-just-moved-6-playful-rose.md`
- Sibling fix: beta.27 ProfileVersion bump
  (`migration-beta26-to-beta27.md`)
- ADR-024 layered LLM timeouts
  (`docs/adr/024-layered-llm-timeouts.md`)
- Source:
  - `processor/agentic-dispatch/normalize_extractor.go` (new)
  - `processor/agentic-dispatch/component.go` (`LayerNormalizer`
    field + `SetLayerNormalizer`)
  - `processor/agentic-dispatch/onboarding_interview.go` (call
    site moved from `NormalizeLayerAnswer` to
    `c.normalizeLayerAnswer`)
