## Context

`hash(predicate)` and PREDICATE_CATALOG are artifacts of the random-strings era: predicates could contain NATS
metacharacters and vary in arity, so #524 hashed them defensively and added a catalog to recover names. PR #532's
enforced three-part grammar killed that rationale. The layout is not *wrong*, but it is no longer *motivated*, and
its costs (second bucket, join, consistency repair, opaque keys, no direct namespace filters) are permanent.

## The selected representation, stated plainly

The predecessor spec framed this as "retain hash+catalog unless raw crosses a pre-registered material-improvement
threshold." That placed the burden of proof on removing a defense whose threat no longer exists and contradicted
the predicate contract's named payoff: raw keys and wildcard queries. The raw layout has now passed its absolute
representation gates and is selected by
[ADR-078](../../../docs/adr/078-raw-canonical-predicate-membership-keys.md). The remaining lifecycle and activation
checks gate deployment, not representation selection.

Two facts support the selection:

- PREDICATE_CATALOG has used raw canonical predicates as production KV keys since #524 — the token safety of raw
  predicates is not hypothetical.
- The 451-byte worst-case raw key and its filters passed the shared validators and pinned real-NATS conformance.

## Recorded representation evidence

The supervised 5k CI profile passed its 3-second operation bound, exact match sets, restart parity, churn
convergence, and temporary-consumer cleanup. The supervised 21k raw profile passed with exact-filter p95
31.920 ms/p99 47.825 ms, churn p95 31.245 ms across 2,000 converged mutations, RSS 18.2 MB to 46.9 MB,
subscriptions 68 to 68, and zero slow consumers. The maximum raw key was 451 bytes and seed throughput was
83,557 rows/s.

The complete exact, owner, maximum-owner, namespace, and resource record is in
[operations/32](../../../docs/operations/32-predicate-layout-smoke-harness.md). It is pinned to
`nats:2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea`
and `github.com/nats-io/nats.go v1.48.0`. Hash-plus-catalog figures are informational comparison evidence only and
never a selection threshold.

## Economics

The pre-v1 wipe/reseed is already mandated and scheduled by the contract changes; `graph-index-replacement-semantics`
activates inside it. Activating the selected representation inside the same window costs one bucket initialization.
Deferring past the window converts a clean cutover into a post-v1 migration SemStreams has declared it will not
support — which in practice means never. Hence the explicit schedule rule: land in the window or halt and re-file
honestly.

## What is deliberately NOT changed

- NAME/CONTEXT hashing: their axes are open product content; the contract does not bound them. Still motivated.
- `hex(predicate)` in NAME/CONTEXT/INCOMING: rationale (token safety) is dead, but the codec is reversible, keeps
  those layouts single-token/fixed-arity, and re-keying three stores has real churn cost with no identified query
  need. Kept consciously, recorded in the ADR, revisited on demonstrated need.
- Watch semantics: decision evidence only unless a current consumer is identified; no public watch API is added to
  favor a representation.

## Risks / Trade-offs

- **Raw keys couple storage to the predicate grammar** — that coupling is the point of having an enforced contract;
  the grammar is the durable decision. A future grammar change is a breaking change with or without raw keys.
- **Namespace filter over-match** (`domain.category.*` vs `domain.categoryx.*`) — token-anchored filters prevent
  this; pinned by an explicit scenario.
- **A pin or contract change invalidates evidence** — rerun the maximum, correctness, performance, churn, and
  resource gates before release; do not inherit the recorded result across a server, SDK, grammar, or layout change.
