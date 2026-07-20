# Readiness distribution and staleness contract

## Why

The post-close evidence on gh#590 (semboids coalescer sensitivity table + observer
discrepancy; semsource finite-burst control case) shows the remaining readiness
problems are structural, not tunable: the readiness signal is distributed by
request/reply polling that fails closed under exactly the load that makes readiness
interesting (a saturated shared NATS connection can starve the status request while
the index is fine); the ADR-082 revision-count tolerance is load- and
coalesce-dependent (the "right" number shifts 2–4× with `coalesce_ms` alone); four
consumers hand-roll four different gate policies around one envelope; and the
clustering defer log carries no evidence, making the failure modes
indistinguishable in the field.

## What Changes

- **Readiness becomes watchable state (KV twofer).** Every ADR-066 envelope
  producer (graph-index, graph-embedding) publishes its `IndexStatusResponse` to
  its key in the dedicated `GRAPH_STATUS` KV bucket (History 3) on a heartbeat
  tick. Consumers watch and hold last-known state instead of issuing per-decision
  status requests with a 5s timeout. **BREAKING**: the `graph.index.query.status`
  request/reply subject is REMOVED — clean break, no dual distribution paths; the
  twofer's `Get` face replaces ad-hoc probing (`nats kv get GRAPH_STATUS
  graph-index`). The read-query subjects (`graph.index.query.incoming`, `byName`,
  …) and their retry contract are untouched.
- **Consumers distinguish "not ready" from "status unknown".** A watched status
  older than a bounded heartbeat multiple is *unknown* (fail-closed, today's
  `AllowUngatedReads` escape preserved), while a fresh not-ready status defers on
  its merits. Today a transport timeout and a genuine not-ready are the same
  fail-closed branch and the same log line.
- **Staleness tolerance is recast in time.** The envelope gains an age-of-view
  signal, `staleness_ms` = `now − commit-time of the newest fully-covered
  revision` (from the watermark's KV entry COMMIT timestamps — NOT `last_synced`
  recency, which measures indexer movement and stays rejected).
  graph-clustering's gate takes `max_staleness` (duration). **BREAKING**: the
  `index_lag_tolerance` (revisions) config field shipped in v1.0.0-beta.156 is
  replaced; no known deployment sets it (semboids and semsource both run the
  default), so this follows the pre-1.0 greenfield break-now rule.
- **One canonical gate, declared modes.** The four hand-rolled consumer policies —
  sticky-bootstrap (graph-index's own query gate), per-tick bounded (clustering),
  per-call exact with no knob (`graph/query` client), degrade-to-honest (fusion) —
  are consolidated behind a single helper in `graph` with an explicit per-consumer
  mode. Exact/point-query consumers keep the exact `Ready` contract bit-for-bit
  (the ADR-066 authoritative-absence license is untouched).
- **Defers become diagnosable.** The clustering defer log gains structured fields
  (status known/unknown, state, lag, staleness, transport error) and a defer-reason
  counter metric, so the next gh#590-shaped investigation is one grep, not three
  comment cycles.
- **New ADR** (candidate ADR-083) recording the two genuine decisions: readiness is
  distributed as state (KV) with request/reply retained as a query convenience, and
  the view-rate staleness unit is time (age-of-oldest-unapplied-write), superseding
  ADR-082's revision-count unit for the clustering gate.

## Capabilities

### New Capabilities

None — everything here is readiness-contract behavior owned by the existing
capability.

### Modified Capabilities

- `graph-index-readiness`:
  - ADDED: readiness envelope published as watchable KV state with heartbeat
    freshness; consumers gate on last-known + age, distinguishing not-ready from
    unknown.
  - ADDED: the envelope carries an oldest-unapplied-write age; consumers with a
    view-rate mode gate on `max_staleness` time tolerance with the same hard stops
    (degraded / reset_required / empty always defer).
  - ADDED: consumers gate through the canonical readiness gate with a declared
    mode (exact | bounded-staleness | sticky-bootstrap | degrade-honest).
  - MODIFIED: "Community detection runs under bounded lag" — the tolerance unit
    becomes time (`max_staleness`), replacing `index_lag_tolerance` revisions
    (**BREAKING** config surface); hard-stop and observability semantics carry
    over.
  - MODIFIED: "Clustering under lag is observable" — defer paths (not only runs)
    become observable with reason granularity.
  - MODIFIED: "Ready reports exact revision coverage" — semantics identical
    bit-for-bit; the requirement is reworded off the removed status subject onto
    the `GRAPH_STATUS` envelope (**BREAKING** wire surface: the subject is gone).
  - MODIFIED: "The readiness envelope is exposed as Prometheus metrics" —
    wording only: gauges complement the KV status key rather than the removed
    subject; gauge names/values unchanged.
  - Unchanged and load-bearing: "A known-incomplete index defers regardless of
    lag", "Read consumers retry the readiness transient" (read-query subjects are
    NOT removed), "Fusion degrades consistently on the readiness transient".

## Impact

- **Code**: `graph/index_status.go` (envelope field + gate helper);
  `pkg/revlag` (track the KV entry commit time of each observed revision and
  expose the commit time of the current `Indexed()` floor);
  `processor/graph-index` and `processor/graph-embedding` (status KV publisher +
  heartbeat; bucket wiring MUST be gated so resourceless deploys keep working);
  `processor/graph-clustering` (watch-based gate, `max_staleness`, structured
  defer log, defer-reason metric); `graph/query/client.go` (canonical gate
  adoption). Component config schema regeneration (`task schema:generate`).
- **Storage**: one new small bounded KV bucket, `GRAPH_STATUS` (History 3, one
  key per producer; ownership defended in design against the bucket-ownership
  rubric).
- **Wire**: **BREAKING** — the `graph.index.query.status` subject and its
  handler are removed; `pkg/fusion/fusionnats`'s `Status` moves to the KV key;
  the envelope JSON gains one additive field. All other query subjects unchanged.
- **Breaking summary**: (1) status subject removed, (2) `index_lag_tolerance`
  config field removed (shipped .156, unset by all known adopters). Clean break,
  no deprecated code paths — sem\* is the only consumer family and all of it is
  house-managed; migration ships as documentation, not compatibility shims.
  Requires relevant e2e tiers green before tag (`task e2e:statistical` for
  clustering; `task e2e:semantic` for the fusion path).
- **sem\* consumers** (all house-managed, migrate in lockstep): semboids
  (primary beneficiary — flock-community coloring starves today; adopts
  `max_staleness`, retargets its probe to `nats kv get GRAPH_STATUS
  graph-index`); semsource (upgrades pkg/fusion in lockstep; burst-window
  behavior unchanged); semteams/semconnect (sweep for status-subject requesters
  at implementation time — task 5.4).
- **Docs**: new ADR-083 (decision record only; mechanics live in the spec);
  gh#590 follow-up comment when the change lands. The still-owed semboids soak
  is deliberately held until this change decides the staleness unit, so it
  validates a time bound rather than measuring a revision count.

## Non-goals

- No change to the exact `Ready` semantics or the ADR-066 "empty result is an
  authoritative not-found" license; fusion and reverse-index reads stay exact.
- No indexer throughput work (the #480 keyed-ingest family) — this change makes
  lag survivable and observable, not smaller.
- No removal of the read-query subjects (`graph.index.query.incoming`, `byName`,
  `outgoing`, …) or their classified-transient retry contract — only the status
  subject goes.
- No generalization of the graphview G5 wall (ADR-081 follow-up stays parked; if
  it later adopts the KV status key, that is its own change).
- No reopening of the #592 read-path retry contract — retry-the-transient remains
  the read contract; this change only improves how consumers *observe* readiness.
- No product-domain semantics: readiness is substrate state; what a product does
  while degraded (e.g. semboids' neutral node coloring) stays product-side.
