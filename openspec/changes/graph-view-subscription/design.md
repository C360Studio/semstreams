# Design: Graph View Subscription

## Context

Full decision, alternatives, coherence contract, and the verified per-site
migration table live in **ADR-081** (`docs/adr/081-graph-view-subscription.md`).
This design records the composition and the build boundary. The build is
**owner-gated** — this change locks the contract (proposal + spec delta);
implementation is a deferred phase.

## Composition (reuse, not a new subsystem)

The reuse scan (ADR-081 Context, verified in code) established that every piece
already exists; the primitive is a thin composition + one genuinely-new seam:

| Concern | Reused from | New? |
|---|---|---|
| One `WatchAll` → projection map (LWW/key) | ~7 hand-rolls (e.g. `graph/query/client.go:221`) | compose |
| View-rate coalescing (newest-per-key/tick) | `processor/graph-index/revision_coalescer.go`, `pkg/cache/coalescing_set.go` | compose |
| Sequence watermark | `pkg/revlag/watermark.go` | compose |
| Read-after-write coherence (ABA gen guard) | `processor/graph-ingest/component.go:2943-3006` (ADR-079/PR #583) | apply the pattern to the fan-out seam |
| **Subscriber fan-out seam** (`SnapshotAndSubscribe`, per-subscriber coalesce/drop) | — | **NEW** — the only new surface |
| Trusted-decode fast path | #562 | reuse |

Home: `pkg/graphview` (domain-agnostic; bucket + decode func injected). NOT
`pkg/projection` — that name is taken by ADR-056 ownership binding.

## Coherence — the load-bearing seam

The snapshot/register and the delta-apply/broadcast must be mutually atomic under
the projection lock at one sequence S (G1), exactly analogous to graph-ingest's
`{bump+delete}` / `{gen-check+set}` atomicity under `cacheGenMu`. A materialized
view IS a cache; the stale-repopulation race fixed in PR #583 recurs here at the
attach seam if the snapshot can be taken at S while a delta at ≤ S races into the
broadcast set. The spec's coherence + snapshot-consistency requirements pin this;
implementation must carry a deterministic regression test (mirror
`cache_stale_repopulation_integration_test.go`: attach a subscriber mid-apply,
prove no gap/dup/inversion).

## Build boundary

- **In this change (design):** ADR-081, the capability spec (contract), the
  cluster-boundary decision.
- **Deferred (owner-gated build):** the `pkg/graphview` primitive, its coherence
  regression test, and the first consumer migration (#571 graph-query reading from
  the shared view is the natural first mover).
- **Explicitly out of scope:** cross-process re-publish; the two `natsclient`
  ergonomics (independent small fixes); the graph-ingest bootstrap sweep and
  lifecycle per-workflow watchers (different patterns — ADR-081 table).

## Open questions (for the build phase)

- Tick interval default (250ms vs 500ms) — measure against a real consumer.
- Snapshot delivery for a large bucket (streamed vs single payload) — bounded by
  live bucket cardinality; revisit if a view over a very large bucket is needed.
- Whether the first migration (#571) also retires the whole-client poison latch or
  keeps it layered — coordinate with the ADR-079 poison-scoping track.
