# ADR-082: Bounded-Lag Readiness for Periodic, Approximate Consumers

## Status

**Staleness unit superseded by ADR-083 — 2026-07-20.** Only the *quantity* changed —
the bound is wall time (`max_staleness`), not revisions (`index_lag_tolerance`),
because the correct revision count shifts with write rate and `coalesce_ms`.
`ReadyWithinLag` is removed.

**Bounded-lag tolerance retired entirely by ADR-085 — 2026-07-21.** ADR-084
reparameterized the bound (consumer-declared rather than consumer-class-derived);
ADR-085 deletes it. `max_staleness` and the `Freshness` type are gone, readiness
gates on health alone, and community detection — the one consumer this ADR was
written for — now runs whenever the index is healthy and stamps the view age it ran
at onto its output. What survives from this ADR is the unconditional
`failedCount → degraded` honesty fix and the observation that a periodic
whole-result re-deriver can act on a stale view; what does not survive is the
inference that such a consumer therefore wants a *tolerance*.

**Consumer-class split superseded by ADR-084 — 2026-07-20.** The split below —
exact/point-query consumers gate on `Ready`, periodic ones accept bounded staleness —
was a symptom of the conflation ADR-084 names, not a design: it made freshness a
property of the CONSUMER CLASS rather than a requirement each consumer declares. There
are now two orthogonal questions (health, which nobody opts out of; freshness, which
is a parameter), and the exact/point-query class does not exist. What survives is the
observation that drove this ADR: a periodic whole-result consumer can act on a
slightly-stale view, and community detection remains the one consumer for which
coverage is genuinely a correctness input.

Accepted — 2026-07-20. Decision-recording; build green-lit the same day (owner
approval; tasks §2 of the `bounded-lag-readiness-for-periodic-consumers` change).
Shaped by an architect decision and a 5-lens adversarial review
(READY-WITH-CHANGES; two blocking findings folded — see the change's design.md).
Addresses #590 (semboids as instrument). Builds on — does not amend — ADR-066
(honest graph-index readiness).

## Context

`graph-clustering` community detection never runs under continuous write (#590):
its readiness gate is the shared `ComputeIndexStatus.Ready`, which is
`target > 0 && indexed >= target` (exact revision coverage, ADR-066). Under a
firehose the `revlag.Watermark` always holds a just-delivered, not-yet-processed
revision, so `indexed < target` essentially always and `Ready` never flips —
detection defers forever. This is a pre-existing property of the ADR-066 gate
(git-confirmed unrelated to the .155/graphview work), surfaced by a load profile
with no write lulls.

`Ready` is deliberately exact because it is consumed as an "empty result = the
symbol is genuinely absent" license by point-query consumers where a missing tail
entity is a false negative — the fusion honesty envelope and direct reverse-index
reads. It MUST stay exact for them.

But community detection is a categorically different consumer: it re-derives the
whole partition every interval and is self-correcting, so a graph tracking a
bounded number of writes behind shifts community boundaries by a bounded fraction
and the next tick re-derives from fresher input. Gating an approximate,
whole-result-re-deriving consumer on exact catch-up is the mismatch. ADR-066
already anticipated the escape hatch: *"a consumer that knows its target revision
can gate on the numeric lag instead of the coarse global `Ready` bool."*

## Decision

**Readiness is a per-consumer property, split by consumer class:**

- **Exact/point-query consumers** (fusion honesty envelope, reverse-index reads)
  MUST gate on `Ready`. `Ready` continues to mean exact revision coverage.
- **Periodic/approximate, whole-result-re-deriving consumers** (community
  detection today) MAY gate on **bounded lag**: ready when the index is not
  broken and the revision lag is within a consumer-chosen tolerance.

The tolerance is a **consumer** policy, not a producer one: the shared status
publishes one honest signal; how much staleness a given reader accepts is that
reader's decision, defaulting to strict (`tolerance = 0` ≡ exact `Ready`).

**The shared status wire fields (`Ready`, `IndexedRevision`, `TargetRevision`,
`Lag`) are unchanged.** The one shared correction this decision requires is a
`State`-label honesty fix: a known-incomplete index (a failed required write,
`failedCount > 0`) must report `degraded` regardless of lag, so that
"index broken / incomplete" is a reliable hard stop for any bounded-lag consumer.
This is `State`-label-only — `Ready` is already false in that window, so no exact
consumer's behavior changes.

**Scope:** bounded lag counts ENTITY_STATES revision coverage. It does NOT track
INCOMING_INDEX *format* completeness — so this decision assumes no in-place
index-format migration is in flight (such a migration must surface as `degraded`
or reset-required, per gh#474, not as bounded lag).

The mechanics — the `ReadyWithinLag(n)` predicate, the empty-graph guard, the
config surface, and the observability of clustering-under-lag — live in the
`graph-index-readiness` capability spec, not this ADR.

## Alternatives rejected

1. **Loosen the shared `Ready`/`ComputeIndexStatus` globally.** Rejected: `Ready`
   is a load-bearing not-found license for fusion honesty and reverse-index reads;
   a global tolerance would return a symbol written in the last N revisions as an
   authoritative miss — the exact false-negative ADR-066 exists to prevent.
   semboids' "configurable, default-strict" is only safe *consumer-scoped*.
2. **Time-based staleness** (`lastSynced within T`). Rejected: `LastSynced`
   measures indexer *movement*, which is always-true under a firehose (that is the
   stuck-detector's job), not staleness. Lag is the honest, published, cheap
   signal.
3. **A shared bounded-ready primitive now** (e.g. in `pkg/revlag`, reused by
   graphview G5). Rejected as premature: there is one consumer today. Filed as a
   follow-up — when graphview G5's caught-up watermark lands it should adopt the
   same `state-not-degraded AND (caught-up OR (target>0 AND lag<=n))` rule, at
   which point lifting the predicate is warranted.

## Consequences

Community detection runs on a continuously-written graph within a bounded,
operator-visible staleness; a broken, reset-required, empty, or over-lagged index
still defers. The bound trades a bounded fraction of stale edges for liveness —
it does not converge to zero under sustained write, and one revision may carry
many relationships, so the tolerance is a coarse node-count proxy, not an
edge-exact one. Because a non-zero tolerance means clustering on bounded-stale
topology *by design*, the lag a partition ran at MUST be operator-visible
(metric / info-level), or bounded-staleness becomes silent staleness (the #579
lesson).

Whether semboids' lag is bounded or unbounded is not a design input: a fixed
tolerance is correct either way — bounded lag → clustering runs; unbounded lag →
clustering still defers, correctly surfacing a throughput problem (#480 family)
rather than masking it. The soak only tunes the operator's chosen tolerance and
may flag a separate throughput issue. semsource independently corroborated this
with a **finite-burst control case** (#590): a test ingests a fixed set of
entities, writes stop, and a query arriving right after still reads not-ready.
Lag cannot climb without limit there (nothing left to write), which settles the
"does lag plateau" caveat — the gate opens a reachability window after ANY
ordinary write burst, not only under a firehose, so the design does not depend on
the soak either way.

**Read-path staleness is a separate, deferred question.** semsource's own
manifestation is on the EXACT consumers this ADR keeps strict — the fusion
honesty envelope and reverse-index reads erroring `IndexNotReady` right after a
burst. Those consumers correctly retry the classified transient (readiness is
sticky, so it converges fast); serving them bounded-stale would risk the
false-negative ADR-066 guards (a just-written symbol resolving as absent).
Whether a read path should offer an opt-in bounded-stale *read* variant despite
that tension is a genuine but distinct decision, out of scope here and tracked
separately; it is NOT closed by this change.

Cost: one shared `State`-label honesty fix (`failedCount → degraded`
unconditional); a per-consumer tolerance the operator must set to opt in (code
default 0; shipped continuous-load reference configs carry a modest value); the
`ReadyWithinLag` predicate as the single home for the hard-stop rule so it cannot
drift per consumer.
