## Context

`hash(predicate)` and PREDICATE_CATALOG are artifacts of the random-strings era: predicates could contain NATS
metacharacters and vary in arity, so #524 hashed them defensively and added a catalog to recover names. PR #532's
enforced three-part grammar killed that rationale. The layout is not *wrong*, but it is no longer *motivated*, and
its costs (second bucket, join, consistency repair, opaque keys, no direct namespace filters) are permanent.

## The default flip, stated plainly

The predecessor spec framed this as "retain hash+catalog unless raw crosses a pre-registered material-improvement
threshold." That places the burden of proof on removing a defense whose threat no longer exists, and it contradicts
the recorded intent of the predicate contract (raw keys and wildcard queries were its named payoff). This change
flips the default: **raw keys ship unless a named absolute gate fails.** Gates are absolute (budgets, conformance,
lifecycle correctness, churn) because the question is "is raw safe and adequate?", not "is raw X% better than a
layout we no longer have a reason to keep?"

Two facts make the flip low-risk:

- PREDICATE_CATALOG has used raw canonical predicates as production KV keys since #524 — the token safety of raw
  predicates is not hypothetical.
- The worst-case raw key (451 bytes) is already unit-proven against the shared budgets; only pinned real-NATS
  conformance remains.

## Economics

The pre-v1 wipe/reseed is already mandated and scheduled by the contract changes; `graph-index-replacement-semantics`
activates inside it. Deciding representation inside the same window costs one bucket initialization. Deferring past
the window converts a free layout change into a post-v1 migration SemStreams has declared it will not support —
which in practice means never. Hence the explicit schedule rule: land in the window or halt and re-file honestly.

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
- **Gate failure late in the window** — the fallback (hash+catalog) is the shipped state; failure costs only the
  benchmark effort, not a revert.
