# Proposal: Bounded-lag readiness for periodic, approximate consumers

## Why

`graph-clustering` community detection never runs under continuous write (#590,
filed by semboids as a load instrument): 0 detections / 254 deferrals over
8.5 min, `COMMUNITY_INDEX` empty. Root cause is the readiness gate
`graph/index_status.go` `ComputeIndexStatus`: `ready := target > 0 && indexed >= target`.
Under a firehose there is always a just-delivered, not-yet-processed revision in
the `revlag.Watermark` pending set, so `indexed < target` essentially always and
`Ready` never flips. Confirmed at the watermark level: `Indexed()` returns
`observedHigh` only when the pending map drains — a quiescent instant a firehose
never provides. This is NOT a `.155`/graphview regression (git-confirmed the gate
predates it: #439/gh#431 + ADR-066 + gh#474) and NOT a coalescing bug
(`Watermark.Complete` drains all pending `<= rev` per key, so lag tracks ~2%
behind, not frozen). It is structural: exact catch-up is unreachable under
continuous write.

The gate's `Ready` bool is a load-bearing **"empty = authoritative not-found"**
license for two consumers where a missing tail entity is a false negative — the
fusion honesty envelope (`pkg/fusion/engine_lens.go` `Fuse()`) and direct
reverse-index reads (`graph/query/client.go` `indexNotReadyErr`). So `Ready`
MUST stay exact. But community detection is a categorically different consumer:
it re-derives the whole partition every tick and is self-correcting, so a graph
tracking a few writes behind out of thousands shifts community boundaries
negligibly and heals next tick. Gating an approximate, whole-result-re-deriving
consumer on exact catch-up is the mismatch.

ADR-066 already documented the escape hatch: *"a consumer that knows its target
revision can gate on `IndexedRevision >= myRev` instead of the coarse global
`Ready` bool."* The status wire already publishes `Lag`. This change names and
generalizes that hatch for periodic consumers, **consumer-local**, without
touching `Ready` or the shared status wire.

## What Changes

- Add a canonical bounded-lag interpretation `IndexStatusResponse.ReadyWithinLag(n)`:
  true iff `State ∉ {degraded, reset_required}` AND (`Ready` OR `Lag <= n`). The
  `degraded`/`reset_required` hard stops remain hard defers under any tolerance —
  only `building`-with-bounded-lag becomes runnable. `n = 0` is bit-identical to
  the current exact `Ready` gate.
- `graph-clustering` gates its detection tick on a configurable
  `index_lag_tolerance` (revisions, **default 0** = today's behavior), via
  `ReadyWithinLag`. The unreachable/unparseable fail-closed branches
  (`AllowUngatedReads`, default false) are unchanged.
- **Make the `failedCount → degraded` projection unconditional** (drop the
  `&& status.Ready` guard at `processor/graph-index/watermark.go:90`). Today a
  known-incomplete index (a failed required write, its revision already drained
  from the watermark) reads `building` + small lag whenever `Lag > 0`, because
  the degraded override only fires at `Lag == 0`. Bounded-lag would run clustering
  on that partial topology (the gh#474 harm). Making the projection unconditional
  makes `degraded` reliably mean "known-incomplete" so the hard-stop catches it.
  Zero behavior change for exact consumers (they gate on `Ready`, already false in
  that window — only the `State` label moves `building → degraded`).
- Surface the lag a detection ran at (metric + info-level / stage) so
  bounded-staleness clustering is observable, not silent (the #579 lesson).
- ADR-082 records the decision (taxonomy): periodic/approximate,
  whole-result-re-deriving consumers MAY gate on bounded `Lag`; exact/point-query
  consumers (fusion honesty, reverse-index reads) MUST gate on `Ready`. `Ready`
  and the `graph.index.query.status` wire fields are unchanged for every consumer;
  the only shared change is the `State` label honesty fix above.

## Capabilities

### New Capabilities

- `graph-index-readiness`: the exact-`Ready` contract (reaffirmed), the
  bounded-lag `ReadyWithinLag` interpretation with hard stops, and community
  detection running under bounded lag. (Seeded lazily — first change to touch
  this capability as a spec; distilled from ADR-066 + code, verified against
  `graph/index_status.go` + `pkg/revlag/watermark.go`.)

## Impact

- `graph/index_status.go` (+`ReadyWithinLag` method, ~5 lines, single home for
  the hard-stop rule); `processor/graph-clustering/component.go` (+`IndexLagTolerance`
  config, rewire `graphIndexReady`). ADR-082.
- No behavior change at `tolerance = 0` (regression-safe). `Ready`/`State`/`Lag`
  wire unchanged → fusion honesty and reverse-index reads see the exact envelope
  they see today.
- semboids fixes #590 by setting a tolerance in their config and re-soaking.

## Non-goals

- NOT loosening `ComputeIndexStatus.Ready` or any status wire FIELD — a global
  loosening would corrupt fusion honesty (a symbol written in the last N
  revisions would resolve to an authoritative miss). The one shared change is
  making `failedCount → degraded` unconditional (a `State`-label honesty fix that
  leaves `Ready` and every wire field bit-identical for existing consumers).
- NOT time-based (`lastSynced within T`): `LastSynced` measures indexer *movement*
  (always true under a firehose — the stuck-detector's job), not staleness.
- NOT a shared `pkg/revlag` tolerance primitive: graphview G5 (ADR-081) will hit
  the same wall but is N=1 today (YAGNI); filed as a follow-up in ADR-082.
- The code default stays 0 (contract-safe fallback: a product embedding
  graph-clustering without config gets today's exact behavior, no surprise on
  upgrade). The shipped continuous-load reference configs DO carry a modest
  `index_lag_tolerance` (owner-decided 2026-07-20), so the default *deployment*
  experience clusters under continuous write. The exact modest value is set at
  implementation, informed by semboids' soak.
- NOT resolving whether semboids' lag is bounded or unbounded: a fixed `N` is
  correct either way (bounded → runs; unbounded → still defers, surfacing a
  throughput problem (#480 family) rather than masking it). The soak only tunes
  the operator's `N` and may flag a separate throughput issue.
