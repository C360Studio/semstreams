# Design: Graph View Subscription

## Context

Full decision, alternatives, coherence contract (G1–G6), and the four-class
migration scope live in **ADR-081** (`docs/adr/081-graph-view-subscription.md`),
revised 2026-07-19 after the 5-lens adversarial review. This design records the
composition and the build boundary. The contract locked first; the build was
**green-lit 2026-07-19** after owner review and confirmation against the
originating #579 evidence.

## Composition (corrected by the review)

| Concern | Source | Verdict |
|---|---|---|
| One `WatchAll` → projection map (LWW/key, tombstones in-lane) | pattern from the serving-projection hand-rolls | compose |
| Decode + validate ONCE, poison surfaced per-key (G6) | `UnmarshalEntityState` + ADR-079 semantics | compose — **trusted decode (#562) is owner-only by its own contract; forbidden here** |
| View-rate coalescing (greatest-rev op per key/tick) | `processor/graph-index/revision_coalescer.go` semantics | **lift into `pkg/graphview`** — unexported + processor-layer, not importable; `pkg/cache/coalescing_set.go` is keys-only (no revision retention), insufficient |
| G1 sequence S | plain apply-counter under the projection mutex | new (trivial) — `revlag.Watermark` has its own lock; optional lag gauge only |
| Subscriber fan-out seam (`SnapshotAndSubscribe`, delta-only attach, per-subscriber LWW buffer) | — | **NEW — the only new surface** |
| Readiness gate + caught-up watermark + watcher-loss fail-closed (G5) | doctrine from ADR-066 / `markEntityWatchLost` / lifecycle guard degrade | apply at the view |
| Coherence discipline | ADR-079 ABA guard (`graph-ingest/component.go:2943`, PR #583) | apply the pattern at BOTH seams (attach + tick) |

Home: `pkg/graphview` (domain-agnostic; bucket + decode func injected — decode
returns `(T, keep bool, err error)` so consumers can skip non-record keys, map
present-but-not-ready records to absent, and surface contract errors). NOT
`pkg/projection` — taken by ADR-056. jetstream-in-pkg is precedented
(`pkg/lifecycle`).

## Coherence — the load-bearing seams

Two seams, not one:

- **Attach seam:** `{snapshot + register}` atomic with delta application at one
  sequence S under the projection lock.
- **Tick seam (the one the naive composition gets wrong):** both existing
  coalescers fire callbacks OUTSIDE their lock, so a detached batch holding
  K@R5 can be enqueued to a subscriber that attached with a snapshot at K@R6 —
  stale delivery, the PR #583 shape one seam over. Either value-capture +
  subscriber-set iteration + enqueue form one critical section with the
  projection lock, or every enqueue is revision-guarded against the
  subscriber's per-key high-water.

The regression suite must drive both (mirror
`cache_stale_repopulation_integration_test.go`): racing attach; tick-detach →
newer apply → attach → resume delivery; bootstrap-attach gating; watcher-loss
fail-closed + re-bootstrap ghost-key reconciliation; poison surfacing.

## Consumer modes (why the scope holds)

- **snapshot + delta** — serving surfaces (SSE streams, caches, #211 later).
- **delta-only** — work-triggers (enhancement/embedding workers): a trigger
  feed, no snapshot, no second projection copy.
- **invalidation + poison feed** — the bounded graph-query cache in
  multi-reader processes: deltas drive `cache.Delete` + the poison latch (G6);
  the client keeps its bounded read-through cache.
- **NOT consumers** — per-revision pipelines and the lifecycle guard (ADR-081
  Alternative 5): every-revision validation + barrier watermarks are not view
  semantics; they keep raw `WatchAll`.

## Build boundary

- **In this change (design):** ADR-081, the capability spec (contract), the
  cluster-boundary decision.
- **Build (green-lit):** `pkg/graphview`, its coherence + degraded-
  path regression suite, observability, and the first consumer migration —
  **first mover: the agentic-dispatch AGENT_LOOPS SSE activity stream**
  (in-repo, true per-client O(N×writeRate), serving-shaped; the purest #579
  instance we own). The graph-query (#571) migration is follow-up, multi-reader
  processes only.
- **Explicitly out of scope:** cross-process re-publish (shape not
  pre-approved); the two `natsclient` ergonomics (independent small fixes);
  the per-revision pipelines; the graph-ingest bootstrap sweep; lifecycle
  per-workflow watchers.

## Open questions (for the build phase)

- Tick interval default (250ms vs 500ms) — measure against the SSE first mover.
- Large-bucket snapshot delivery: single payload (bounded copy, delivered
  outside the lock) vs streamed. Streamed delivery of a consistent-at-S
  snapshot from a live projection needs copy-on-write/versioning that exists
  nowhere in the composed prior art — treat streamed as a real design fork,
  not a tuning knob; default to single-payload copy.
- Whether the multi-reader #571 migration retires the whole-client poison
  latch or keeps it layered over the G6 signal — coordinate with the ADR-079
  poison-scoping track.
