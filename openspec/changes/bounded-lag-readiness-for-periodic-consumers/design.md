# Design: Bounded-lag readiness for periodic consumers

## Context

Full decision in **ADR-082** (to be written in task 1). This design records the
seam choice, the interpretation, and the consumer wiring. The root cause is
confirmed against code (see proposal.md) and is a pre-existing property of the
ADR-066 honest-readiness gate, not a `.155` regression.

## The seam — consumer-local, not the shared status

`ComputeIndexStatus` (`graph/index_status.go:63`) is a shared projection over
`revlag.Watermark`, produced by graph-index (`processor/graph-index/watermark.go`)
and graph-embedding (`processor/graph-embedding/readiness.go`). Its `Ready` bool
is consumed as an **"empty = authoritative not-found"** license by:

| Consumer | Site | `Ready` licenses | Tolerance-safe? |
|---|---|---|---|
| fusion honesty envelope | `pkg/fusion/engine_lens.go` `Fuse()` | empty result = symbol genuinely absent | **NO** — false not-found |
| direct reverse-index reads | `graph/query/client.go` `indexNotReadyErr` | INCOMING_INDEX read = complete topology | **NO** — point reads |
| community detection | `processor/graph-clustering/component.go:1035` `graphIndexReady` | safe to re-derive the whole partition | **YES** — self-correcting each tick |

Only the third column tolerates staleness. Therefore the tolerance lives at the
**graph-clustering consumer**, gating on the already-published numeric `Lag` —
NOT at `ComputeIndexStatus` (which would flip `Ready` for fusion + reverse-index
reads and reintroduce the false-negative ADR-066 exists to kill). This is the
one place semboids' proposal must be narrowed: "configurable, default strict" is
safe consumer-scoped, unsafe at the shared projection.

## The interpretation (single home for the hard-stop rule)

```go
// graph/index_status.go, next to ComputeIndexStatus
func (r IndexStatusResponse) ReadyWithinLag(n uint64) bool {
    if r.State == IndexStateDegraded || r.State == IndexStateResetRequired {
        return false // hard stops survive any tolerance
    }
    // TargetRevision > 0 guard: an empty graph has Lag == 0 but Ready == false,
    // so Lag == 0 does NOT imply caught-up. Without this guard ReadyWithinLag(0)
    // would return true on an empty graph while Ready is false (5-lens F1).
    return r.Ready || (r.TargetRevision > 0 && r.Lag <= n)
}
```

- `degraded` and `reset_required` (contract poison) remain **hard defers** under
  any `n`. `degraded` must cover EVERY known-incomplete index — which requires the
  `failedCount → degraded` projection to be **unconditional** (see below);
  otherwise a failed required write presents as `building` + small lag and slips
  through. Bounded lag ≠ the gh#474 format-cutover case — that surfaces as
  `degraded` or building-with-large-lag. NOTE the watermark tracks ENTITY_STATES
  coverage, not INCOMING_INDEX *format* completeness, so ADR-082 scopes this to
  "no in-place index-format migration in flight."
- `n = 0` ⟹ `Ready || (target>0 && Lag<=0)` ⟹ `Ready` for every state (the
  `target>0 && Lag==0` case is exactly `Ready`, and `target==0` gives false ==
  `Ready`): exact parity with today. The earlier "`Lag == 0 ⟺ Ready`" premise was
  **false** and is dropped.

## The known-incomplete hole and its fix (5-lens BLOCKING)

The hard-stop set only protects clustering if `degraded` truly covers every
known-incomplete index. It does not, as scoped:

- A failed required index write increments `failedCount` (`component.go:1301`),
  but `Complete()` still drains that revision (`component.go:1065` — "readiness is
  gated on failedCount, not on completing this revision"), so `Lag` does not
  reflect the missing entity.
- The `failedCount → degraded` override (`watermark.go:90`) is gated
  `&& status.Ready`. Under continuous write `Ready` is already false (`Lag>0`), so
  the override is **skipped** and `State` stays `building`.
- `ReadyWithinLag(N)` then sees `building` + small `Lag` → runs clustering on an
  INCOMING_INDEX genuinely missing that entity's edges — the gh#474 harm. The old
  exact gate was protected here by `Lag>0 ⟹ !Ready`, which bounded-lag removes.

**Fix (a deliberate, minimal relaxation of the wire non-goal):** drop the
`&& status.Ready` guard so `failedCount > 0 ⟹ degraded` unconditionally. This is
`State`-label-only for existing consumers — fusion/reverse-index reads gate on
`Ready` (already false in that window) and reverse-index reads additionally check
`failedCount` directly (`query.go:165`). It makes the shared status strictly more
honest and lets the existing hard-stop catch the case. Task-time: grep for any
consumer branching on `State == "building"` specifically (none expected) before
landing.
- Lag-only, not time-based: `LastSynced` refreshes on every batch under a
  firehose (measures movement, not staleness). Revision-count is a valid
  staleness proxy at clustering's node granularity ("≤ N entity-writes behind" =
  "≤ N nodes possibly stale"); it would be wrong for a byte-budget consumer,
  which this is not.

## Consumer wiring

`processor/graph-clustering/component.go`:
- `Config` gains `IndexLagTolerance uint64` (`json:"index_lag_tolerance"`,
  `schema:"type:int,description:...,category:advanced"`, default 0), adjacent to
  `AllowUngatedReads` (:68).
- `graphIndexReady()` (:1035): the parsed-status branch returns
  `status.ReadyWithinLag(c.config.IndexLagTolerance)` instead of `status.Ready`.
  The unreachable (:1041) and unparseable (:1046) branches — which fail open/closed
  on `AllowUngatedReads` — are **unchanged**.

## Soak dependency — none

A fixed `N` self-resolves semboids' bounded-vs-unbounded question:
- Bounded lag → `Lag` oscillates below `N` → clustering runs. #590 fixed.
- Unbounded lag → `Lag` grows past `N` → clustering still defers → correctly
  surfaces "not running" as the throughput signal (#480 family), unmasked.

The soak only tunes the operator's chosen `N` and may flag a separate throughput
issue to file. Not a design or implementation blocker.

## Cross-cutting (ADR-081 graphview G5)

graphview's G5 "caught-up watermark" will hit the identical unreachable-under-
continuous-write wall for any continuous-write view consumer. But graphview runs
its own watermark over a WatchAll projection and produces no `IndexStatusResponse`,
so `ReadyWithinLag` has exactly one consumer today. Do NOT extract a shared
`pkg/revlag` policy primitive now (N=1 speculative-layer anti-pattern). Note in
ADR-082's follow-ups that graphview G5 should adopt the same
`state-not-degraded AND (caught-up OR lag <= n)` rule and, at that point,
consider lifting the helper to `pkg/revlag`.

## Resolved (owner, 2026-07-20)

Code default stays `0` (contract-preserving; a product embedding graph-clustering
without config gets today's exact behavior). The shipped continuous-load
reference configs (e.g. `configs/statistical.json`) **carry a modest
`index_lag_tolerance`** so the default deployment experience clusters under
continuous write. The exact value is chosen at implementation, informed by
semboids' soak (a few-hundred-revisions starting point, tuned). Note for the
review: a non-zero *reference-config* value is a shipped-behavior change (the
firehose demo now clusters) but NOT a code-contract change — embedders without
the field still get strict `Ready`.
