# Migration Guide: beta.32 → beta.33

## Summary

Beta.33 is a **hygiene release** that publishes the LLM capability baseline work
(PRs #21 + #22) on its own tag so that downstream consumers can pin to the
capability surface without inheriting the auth / identity propagation breaking
changes that ship in beta.34.

| Surface | Status |
|---|---|
| New capability constant: `intent_classification` | **Additive** — opt-in |
| New capability constant: `layer_normalization` | **Additive** — opt-in |
| New capability constant: `anomaly_review` | **Additive** — opt-in |
| Existing capability constants (`embedding`, `community_summary`, `entity_digest`, `query_planning`, `answer_synthesis`) | **Unchanged** |
| `model.Registry` API | **Unchanged** |
| Public payload schemas | **No breakage** |

**The simplest beta.32 → beta.33 upgrade is to do nothing.** Existing
deployments behave bit-for-bit identically because each of the three lifted
sites keeps its prior fallback path when the new capability is unbound.

## What's new

### Three capability constants for previously-hidden LLM call sites

Three sites that used to bypass the model registry are now first-class
capabilities. Each is **opt-in**: binding the capability is a config-only
change that routes the call to a dedicated endpoint; leaving it unbound
preserves the pre-beta.33 routing.

| Constant | Wire string | Used by | Fallback when unbound |
|---|---|---|---|
| `model.CapabilityIntentClassification` | `intent_classification` | `agentic-dispatch` intent classifier (every incoming user message) | `defaults.model` via the `resolveEndpoint` chain |
| `model.CapabilityLayerNormalization` | `layer_normalization` | `agentic-dispatch` onboarding-answer extractor | `defaults.model` via the `resolveEndpoint` chain |
| `model.CapabilityAnomalyReview` | `anomaly_review` | `graph-clustering` `ReviewWorker` (suggested-relationship classification) | The `community_summary` endpoint (legacy piggyback) |

The first two follow the existing fallback semantics — empty `modelName`
resolves to the capability constant, then the registry's resolution chain
falls through to `defaults.model` on miss. The third is special: a dedicated
`llm.Client` is created only when `anomaly_review` is **explicitly bound**
(checked via `GetCapability`, not `ResolveEndpoint`, to avoid spinning up a
redundant client on the default-fallthrough path). Otherwise the worker
reuses the existing `community_summary` client, preserving the legacy
behaviour bit-for-bit.

### Documentation refresh

- `docs/operations/05-model-registry.md` is now the canonical model-registry
  guide (renamed from `05-model-registry-runtime-updates.md`). It covers
  registry structure, `EndpointConfig` / `CapabilityConfig` field references,
  and the full 8-capability inventory.
- `docs/operations/11-llm-routing-and-model-selection.md` folds the bypass
  spec sheets into the main capability list and updates the workload-tier
  matrix to use capability names. Includes a recommended-bindings example
  (intent classification co-located with the agentic backend;
  anomaly review and layer normalization on the concurrent seminstruct
  backend).

## Recommended bindings

If you want to take advantage of the new capabilities, add bindings to your
model registry config alongside the existing ones. Example:

```yaml
capabilities:
  intent_classification:
    endpoint: agentic-fast      # co-locate with the agentic backend
  layer_normalization:
    endpoint: seminstruct       # concurrent throughput backend
  anomaly_review:
    endpoint: seminstruct       # concurrent throughput backend
```

See `docs/operations/11-llm-routing-and-model-selection.md` for the full
configuration reference and the rationale behind the recommended placements.

## Backward compatibility

Non-breaking. The wiring on each lifted site falls back to the prior routing
when the capability is unbound:

- `processor/agentic-dispatch/intent_classifier.go` — empty `modelName`
  defaults to `CapabilityIntentClassification`; `resolveEndpoint` falls
  through to `defaults.model` on miss.
- `processor/agentic-dispatch/normalize_extractor.go` — `extractionModelName`
  bound to `CapabilityLayerNormalization`; same fallback chain.
- `processor/graph-clustering/component.go` — the new `resolveReviewLLMClient`
  helper returns the existing `community_summary` client when
  `anomaly_review` is not explicitly bound.

## Why a dedicated tag

PRs #21 and #22 landed on `main` after the beta.32 cut. Beta.34 introduces
the auth / identity propagation work (new `auth` package, NATS
`X-Caller-*` header propagation, `$caller.*` rule conditions) and carries
breaking changes for the framework-internal identity API and the rule
`$`-prefix reserved namespace. Tagging the capability baseline separately
lets sister projects (semspec, semteams, semdragon) pick up the hidden
LLM call site lift without pulling in the auth-flow breaking changes
until they are ready.

## Programme context

| Tag | Beta | Status |
|---|---|---|
| 1 | beta.32 | Shipped — `$caller.*` substitution + `deny` action |
| 1.5 | beta.33 | **This tag** — LLM capability baseline (hygiene) |
| 2 | beta.34 | Pending — auth / identity / `$caller.*` end-to-end |
| 3 | beta.35 | Pending — shadow mode + `count_in_window` + `negate` + filter-output bridge |
| 4 | beta.36 | Pending — org wiring pass (HARD BREAKING) |
| 5 | beta.37 | Pending — JetStream cluster docs + reconnect defaults |
| 6 | beta.38 | Pending — cert-based auth + per-org-account hardening |
