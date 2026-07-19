# ADR-081: Graph View Subscription — Shared Read-Side Fan-Out Primitive

## Status

Proposed — 2026-07-19. Contract-locking decision; **build DEFERRED (owner-gated).**
Complements the write-side hardening (#480 ingest ceiling, #562 write-path
fan-out, ADR-079 per-entity poison). Addresses #579.

## Context

SemStreams has a hardened write side (graph-ingest is the sole `ENTITY_STATES`
writer) and **no equivalent read side.** A component serving *many clients* a
live view of a busy graph has exactly one tool today: open a `WatchAll` per
client. That is N independent JetStream consumers for N clients tailing the
*identical* projection — every write is serialized, pushed, and decoded N times
and buffered in N pending queues, each able to independently trip `nats: slow
consumer`. Cost is O(N × writeRate) of redundant work; under load it manifests
as operator-visible staleness (semboids evidence in #579). This is not a NATS
defect — sharing one consumer's delivery across many local readers is, by
design, an application/framework concern.

The reuse scan (verified in code) found the framework has already hand-rolled
every *piece* of the fix, repeatedly, with no shared seam:

- **one-watcher → in-memory-projection** at `graph/query/client.go:221`
  (`observeEntityStates`, ENTITY_STATES — this is #571),
  `processor/graph-query/community_cache.go:49` (COMMUNITY_INDEX),
  `graph/clustering/enhancement_worker.go:216` (COMMUNITY_INDEX),
  `graph/embedding/storage.go:387` (EMBEDDING_INDEX),
  `graph/inference/storage.go:429` (ANOMALY_INDEX),
  `processor/graph-index/component.go` (index buckets),
  `pkg/lifecycle/manager.go:90` (ENTITY_STATES process-lifetime guard);
- **view-rate coalescing** at `pkg/cache/coalescing_set.go` and
  `processor/graph-index/revision_coalescer.go` (retains greatest-revision-per-key);
- an honest revision watermark at `pkg/revlag/watermark.go`;
- the **read-after-write coherence guard** we just landed at
  `processor/graph-ingest/component.go:2943-3006` (ADR-079 wave).

What is missing everywhere is the **subscriber fan-out seam**: register a new
consumer with a snapshot consistent with the delta stream it then tails, with
per-subscriber backpressure.

## Decision

Introduce ONE minimal shared primitive — a **Graph View Subscription** (home
`pkg/graphview`; domain-agnostic — bucket name + a decode func injected; NOT
`pkg/projection`, which is already taken by ADR-056 ownership binding). A view:

1. Owns **one** `WatchAll` per bucket and maintains an authoritative in-memory
   current-state projection (last-writer-wins per key), using the #562
   trusted-decode fast path so the lone watcher never slow-consumes.
2. **Coalesces at the view, ahead of fan-out** — a ticker (e.g. 250–500ms)
   emits the newest value per changed key per window (reusing revision-coalescer
   semantics), turning `writeRate × N` into `deltaRate × N`.
3. Lets N **local** consumers `SnapshotAndSubscribe`: the initial snapshot and
   registration into the broadcast set are taken **atomically under one lock at
   one view sequence number S** — the subscriber gets every key at seq ≤ S in the
   snapshot and every delta at seq > S in the stream (no gap, no dup, no
   stale-snapshot-over-newer-delta inversion).
4. Applies **per-subscriber at-most-once backpressure**: a slow subscriber's
   pending delta coalesces (last-writer-wins per key) into a bounded buffer and
   degrades to staleness; it never blocks the view watcher or any other
   subscriber.

The primitive is a **composition** of existing substrate (`revlag.Watermark` +
coalescer + projection map + the ADR-079 ABA coherence guard), not a new
subsystem. It **coexists with raw `WatchAll`** — genuinely independent consumers
(distinct filters, historical replay, independent ack) still use real consumers.
Cross-process fan-out (re-publish coalesced deltas to `graph.view.<name>.delta`)
is a **deferred, optional** extension — a non-goal for v1, not built until a
cross-process consumer exists (YAGNI).

## Coherence contract (the load-bearing part)

A materialized view IS a cache; the read-after-write coherence class that bit the
graph-ingest read-through cache applies identically.

- **G1 (snapshot atomicity):** `{snapshot + register}` is mutually atomic with
  `{apply-delta + broadcast}` under the projection lock at one sequence S — the
  direct analogue of graph-ingest's `{bump+delete}` / `{gen-check+set}` atomicity
  under `cacheGenMu`.
- **G2/G3 (read-your-view-writes):** deltas apply in KV Watch order; per key only
  the greatest revision survives; no subscriber may observe, for any key, a value
  older than one the view already applied at or before its snapshot sequence.
- **G4 (coalescing loses intermediates, never reorders):** dropping intermediate
  revisions within a window is allowed (current-state view, not an event log);
  delivering an older value after a newer one is forbidden.

## Alternatives rejected

1. **Status quo — N independent `WatchAll`.** The O(N × writeRate) trap; already
   causing operator-visible staleness. Rejected.
2. **Extend an existing projection** (graph-query's client cache or the lifecycle
   Manager). Rejected: graph-query's cache is pull-only with a whole-client poison
   sticky-latch (#571 — wrong backpressure, wrong coupling); the lifecycle Manager
   is workflow-scoped, per-caller (ADR-047/049). Neither has a fan-out seam, and
   bolting one on entangles the view with unrelated concerns.
3. **Push fan-out to NATS as the primary mechanism** (durable per-view consumer +
   core re-publish). Rejected as primary — adds a stream/subject + ack/replay
   surface for the common *in-process* many-reader case. Kept as a deferred
   optional extension.
4. **A full CQRS read-model framework** (pluggable backpressure policies, query
   language, persistence tiers, multi-region). Rejected as over-engineering (YAGNI).
   The confirmed consumer set is "many local readers tail one current-state
   projection."

## Consequences

Decode drops from O(N × writeRate) to O(writeRate); fan-out is O(deltaRate × N)
with deltaRate ≪ writeRate on a busy bucket. One place to get read-after-write
coherence right, versus the hand-rolls each re-risking the stale-repopulation
race just fixed.

**Hand-rolled-site cleanup (verified per site — honest, not aspirational):**

| Site | Bucket | Migration verdict |
|---|---|---|
| `graph/query/client.go:221` | ENTITY_STATES | Clean fit — reads from shared view (#571 watcher-tax); keeps its own poison-latch |
| `graph-query/community_cache.go:49` | COMMUNITY_INDEX | Fit + **consolidation** with clustering worker (2 watchers → 1) |
| `graph/clustering/enhancement_worker.go:216` | COMMUNITY_INDEX | Fit + **consolidation** (same bucket as above) |
| `pkg/lifecycle/manager.go:90` | ENTITY_STATES | Partial — process-lifetime guard fits; per-workflow watchers are a different pattern, stay |
| `graph/embedding/storage.go:387` | EMBEDDING_INDEX | Fit — cleaner + coherent impl, single-consumer (no fan-out win) |
| `graph/inference/storage.go:429` | ANOMALY_INDEX | Fit — cleaner + coherent impl, single-consumer |
| `graph-index/component.go` | index buckets | Fit-ish — cleaner impl, single-consumer |
| `graph-ingest/component.go:1225` | ENTITY_STATES | Does NOT fit — a writer-side one-time bootstrap sweep, not a serve projection |

So the payoff is asymmetric and worth stating plainly: the **pattern
de-duplication + coherence-correctness** win is broad (every fit-site drops its
hand-rolled watch/projection/coalesce and inherits the tested coherence guard);
the raw **fan-out / watcher-consolidation** win is concentrated on the two
multiply-watched buckets (COMMUNITY_INDEX, ENTITY_STATES). Two sites (the
graph-ingest bootstrap sweep, the lifecycle per-workflow watchers) are different
patterns and are out of scope. Migration is incremental and opt-in.

Cost: a new shared primitive to maintain; the coherence contract re-proven for
the fan-out seam; memory = one projection copy per view (bounded by live bucket
cardinality — the cost each hand-roll already pays). **Build is deferred:** the
contract is locked now; consumers keep hand-rolling until it ships.

## Cluster boundary (#579 relatives)

- **#571** — SUBSUMED (watcher-tax half): graph-query reads from the shared view.
  The whole-client poison sticky-latch (D10 carve-out) is a separate concern and
  stays with #571.
- **#340** (raw-lane + projection guidance) — ADJACENT docs task; the view is its
  concrete referent (intermediate revisions live in the raw KV lane; see G4).
- **#176** (bulk-reads/pagination) — ORTHOGONAL: one-shot bounded reads, a
  different axis. Not subsumed.
- **#211** (read-only MCP) — DOWNSTREAM consumer: a query gateway that *may* attach
  to the view later; built independently.
- Split out of #579: two independent small `natsclient` ergonomics — attribute the
  `nats: slow consumer` log with subject/subscription, and expose watcher
  pending-limit config.
