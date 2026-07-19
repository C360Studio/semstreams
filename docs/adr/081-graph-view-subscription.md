# ADR-081: Graph View Subscription — Shared Read-Side Fan-Out Primitive

## Status

Accepted — 2026-07-19. Contract-locking decision, revised the same day after
the 5-lens adversarial review (all lenses READY-WITH-CHANGES; upheld findings
folded — corrected site sweep, validating decode, degraded-path contract,
four-class migration scope) and confirmed against the originating #579
evidence (in-process per-client SSE watchers). **Build green-lit 2026-07-19**
(owner approval; tasks §2 of the `graph-view-subscription` change). Complements
the write-side hardening (#480 ingest ceiling, #562 write-path fan-out,
ADR-079 per-entity poison). Addresses #579.

## Context

SemStreams has a hardened write side (graph-ingest is the sole `ENTITY_STATES`
writer) and **no equivalent read side.** A component serving *many clients* a
live view of a busy bucket has exactly one tool today: open a `WatchAll` per
client — N independent JetStream consumers tailing the *identical* current
state, every write serialized, pushed, decoded N times, buffered in N pending
queues, each able to independently trip `nats: slow consumer`. Cost is
O(N × writeRate); under load it manifests as operator-visible staleness. This
trap is live in this repo today, not hypothetical:

- `processor/agentic-dispatch/http.go:902` — one AGENT_LOOPS `WatchAll` **per
  connected SSE client** (activity stream);
- `service/message_logger_kv_watch.go:216` — one watcher **per dashboard SSE
  client**, on any bucket the URL selects;
- semboids' app-side `graphstream` (the #579 evidence) is the same shape in a
  sister repo.

This is not a NATS defect — sharing one consumer's delivery across many local
readers is, by design, an application/framework concern.

The corrected reuse sweep (re-verified against code, 2026-07-19) found **12
steady-state single-watcher hand-rolls plus the 2 per-client surfaces above**,
in four distinct shapes:

- **Serving projections** (hold current state to answer reads):
  `processor/graph-query/community_cache.go:49` (COMMUNITY_INDEX),
  `graph/embedding/storage.go:387` (EMBEDDING_INDEX vector cache);
- **Per-revision pipelines** (validate every revision, barrier on raw-rate
  watermarks, write derived KV): `processor/graph-index/component.go:775`,
  `processor/graph-index-spatial/component.go:601`,
  `processor/graph-index-temporal/component.go:624`,
  `processor/graph-embedding/component.go:919` (all ENTITY_STATES), and the
  lifecycle validation guard `pkg/lifecycle/manager_query.go:246`;
- **Work-triggers** (change → do work, re-read on demand):
  `graph/clustering/enhancement_worker.go:216` (COMMUNITY_INDEX),
  `graph/embedding/worker.go:214` (EMBEDDING_INDEX),
  `graph/inference/review_worker.go:125` (ANOMALY_INDEX);
- **Bounded-cache invalidation feed**: `graph/query/client.go:221`
  (ENTITY_STATES; MaxSize-1000 read-through cache whose watcher invalidates
  and feeds the poison latch — NOT a projection; this is #571);
- (dead: `graph/inference/storage.go:429` `Watch` has no production caller.)

Multiply-watched buckets in the standard single-process semantic stack:
ENTITY_STATES ×4 permanent (the four pipelines), COMMUNITY_INDEX ×2,
EMBEDDING_INDEX ×2 (cache + worker, same component), AGENT_LOOPS ×N SSE
clients. The pieces of a fix also already exist, hand-rolled with no shared
seam: view-rate coalescing (`processor/graph-index/revision_coalescer.go` —
unexported, processor-layer; `pkg/cache/coalescing_set.go` — importable but
keys-only), an honest revision watermark (`pkg/revlag/watermark.go`), and the
read-after-write coherence guard (`processor/graph-ingest/component.go:2943`,
ADR-079 wave). What is missing everywhere is the **subscriber fan-out seam**:
register a consumer with a snapshot consistent with the delta stream it then
tails, with per-subscriber backpressure and honest degraded-path signals.

## Decision

Introduce ONE minimal shared primitive — a **Graph View Subscription** (home
`pkg/graphview`; domain-agnostic — bucket name + a decode func injected; NOT
`pkg/projection`, which is taken by ADR-056 ownership binding; admission per
ADR-075 test (2): substrate usability with the recorded correctness gap —
#579/#571 plus the sweep above). A view:

1. Owns **one** `WatchAll` per bucket and maintains an authoritative in-memory
   current-state projection (last-writer-wins per key, tombstones included).
   It **decodes and contract-validates each write exactly once** — the fan-out
   win is decode-once-amortized-across-N, never validation skipped. Owner-only
   trusted decode (`UnmarshalEntityStateTrusted`, #562) is forbidden here by
   that API's own contract ("every other reader MUST keep
   `UnmarshalEntityState`", naming graph-view consumers); it exists only for
   ENTITY_STATES anyway. Decode/contract failures surface as a **typed per-key
   poison signal** (ADR-079 authoritative-surface semantics) — never a silent
   skip, never whole-view halt for unrelated keys.
2. **Coalesces at the view, ahead of fan-out** — a ticker (250–500ms, build
   phase measures) emits the greatest-revision operation per changed key per
   window. The delta alphabet is `upsert(key, value, rev) | delete(key, rev)`;
   tombstones ride the same ordered lane (no out-of-band delete path that can
   race the coalesced stream).
3. Lets N **local** consumers `SnapshotAndSubscribe`: snapshot + registration
   are atomic at one view sequence S (every key at ≤ S in the snapshot, every
   delta > S in the stream — no gap, no dup, no inversion). Snapshot *capture*
   may hold the projection lock (bounded copy); snapshot *delivery* must not.
   Trigger-shaped consumers may attach **delta-only** (skip the snapshot).
4. Applies **per-subscriber at-most-once backpressure**: a slow subscriber's
   pending deltas coalesce last-writer-wins per key into a buffer bounded by
   live changed-key cardinality (values shared, not copied; no count-eviction;
   slowness never disconnects); it never blocks the view watcher or peers.
5. Is **honest when degraded**: attach before the initial replay completes is
   gated (block, typed not-ready, or explicitly-marked snapshot — readiness =
   caught-up, not started); the view exposes its caught-up watermark; loss of
   the shared watcher fails closed with an explicit staleness signal to every
   subscriber (one shared watcher is a new single failure domain — its loss
   must never silently serve a frozen projection as live); re-bootstrap
   reconciles the projection (ghost-key removal) before reporting caught-up.
6. Offers a **coherent point-read surface** (`Get`/bounded list) over the same
   projection, honoring the same readiness and poison semantics.

The primitive **composes** existing substrate where the layering permits: the
revision-coalescer *semantics* are lifted into `pkg/` (the graph-index
implementation is unexported and processor-layer — not importable;
`pkg/cache/coalescing_set.go` is keys-only, insufficient); the G1 sequence is
a plain apply-counter under the projection mutex (`revlag.Watermark` has its
own lock and solves async-completion tracking — at most an optional lag gauge
here, not the sequencing mechanism); the coherence discipline is the ADR-079
ABA-guard pattern applied to the fan-out seam. It **coexists with raw
`WatchAll`** — consumers needing independent filters, historical replay,
per-revision delivery, or independent ack keep real consumers. Cross-process
fan-out (re-publish coalesced deltas to `graph.view.<name>.delta`) is a
**deferred, optional** extension — a non-goal for v1; this lock does NOT
pre-approve that subject shape.

## Coherence contract (the load-bearing part)

A materialized view IS a cache; the read-after-write coherence class that bit
the graph-ingest read-through cache applies identically — but at different
seams than the original draft claimed.

- **G1 (snapshot atomicity):** `{snapshot + register}` is atomic with delta
  application at one sequence S under the projection lock. The critical pair
  extends through fan-out: value-capture, subscriber-set iteration, and
  per-subscriber enqueue are one critical section — or every enqueue is
  revision-guarded against the subscriber's per-key high-water. (The naive
  composition is wrong: both existing coalescers fire callbacks OUTSIDE their
  lock, so a batch captured at R5 can be enqueued to a subscriber that
  attached with a snapshot at R6 — the PR #583 stale-delivery shape recurring
  at the **tick seam**, not the attach seam.)
- **G2/G3 (read-your-view-writes):** deltas apply in KV watch order (ordered
  consumer, monotonic ascending revisions — a stated assumption); per key only
  the greatest revision survives; no subscriber may observe, for any key, a
  value older than one the view already applied at or before its snapshot
  sequence.
- **G4 (coalescing loses intermediates, never reorders):** dropping
  intermediate revisions within a window is allowed (current-state view, not
  an event log); delivering an older operation after a newer one is forbidden.
  A tombstone counts as the operation at its revision.
- **G5 (fail closed, never silently stale):** bootstrap gating, caught-up
  watermark exposure, watcher-loss staleness signaling, and re-bootstrap
  reconciliation per Decision §5.
- **G6 (poison surfaces, never launders):** validating decode with typed
  per-key poison signals per Decision §1; projection-owner consumers (query
  client latch, lifecycle guard) can implement their ADR-079 reset-required
  semantics from the signal alone.

## Alternatives rejected

1. **Status quo — N independent `WatchAll`.** The O(N × writeRate) trap; live
   in-repo at the two SSE surfaces; the #579 staleness evidence. Rejected.
2. **Extend an existing projection** (graph-query's client cache or the
   lifecycle Manager). Rejected: graph-query's cache is a bounded read-through
   with a whole-client poison latch (#571 — wrong shape, wrong coupling); the
   lifecycle Manager is workflow-scoped (ADR-047/049). Neither has a fan-out
   seam, and bolting one on entangles unrelated concerns.
3. **Push fan-out to NATS as the primary mechanism** (durable per-view
   consumer + core re-publish). Rejected as primary — adds a stream/subject +
   ack/replay surface for the common in-process many-reader case. Kept as a
   deferred optional extension; shape not pre-approved.
4. **A full CQRS read-model framework** (pluggable backpressure policies,
   query language, persistence tiers). Rejected as over-engineering (YAGNI).
5. **A shared raw-rate feed for the per-revision pipelines** (one watcher
   fanning every revision, unvalidated-coalesced, to graph-index/spatial/
   temporal/embedding). Rejected for v1: those consumers need every-revision
   delivery, their own validation posture, and barrier watermarks — a
   *different* primitive; forcing them onto a coalesced view breaks their
   semantics, and instantiating a view in their process adds a full decoded
   projection nobody holds today (problem-shifting, not problem-solving).
   They keep raw `WatchAll`.

## Consequences

Where the view applies, decode+validate drops from O(N × writeRate) to
O(writeRate) and fan-out is O(deltaRate × N) with deltaRate ≪ writeRate. One
place to get read-after-write coherence, readiness honesty, and poison
surfacing right, versus each hand-roll re-risking the stale-repopulation race.

**Memory honesty:** the view holds one full decoded projection per (bucket,
process). That is a *win* only where a projection already exists (serving
caches) or where N per-client copies collapse to one (SSE surfaces). In a
process holding no projection today it is NEW resident memory — which is why
the pipelines and single-reader bounded-cache processes do not convert.
Transient costs: one bounded map copy per attach (capture under lock); one
pointer-map per slow subscriber bounded by changed-key cardinality (values
shared).

**Migration scope (four classes, re-verified per site):**

| Class | Sites | Verdict |
|---|---|---|
| Per-client serving surfaces | agentic-dispatch AGENT_LOOPS SSE (`http.go:902`); message-logger KV-watch SSE; semboids graphstream (sister repo); #211 MCP gateway (future) | **Convert — the primary win.** N per-client watchers → 1 view. First mover: the AGENT_LOOPS activity stream (in-repo, purest #579 shape) |
| Serving projections | community_cache (COMMUNITY_INDEX); embedding vector cache (EMBEDDING_INDEX) | **Convert.** Projection dedup + inherited coherence/poison/readiness. Work-triggers on the same buckets (enhancement worker, embedding worker) may attach delta-only → COMMUNITY_INDEX 2→1, EMBEDDING_INDEX 2→1 |
| Bounded-cache invalidation feed | graph/query client (`client.go:221`, #571) | **Convert only in multi-reader processes** (delta feed drives invalidation + poison latch via G6). Single-reader embedder processes DO NOT convert — full projection would raise their memory floor from ≤1000 entries to bucket cardinality. #571 is *partially* subsumed |
| Per-revision pipelines & guards | graph-index / spatial / temporal / graph-embedding watchers; lifecycle guard (`manager_query.go:246`) | **Stay raw `WatchAll`** (Alternative 5). Every-revision validation + barrier watermarks are not view semantics |
| Out of scope | graph-ingest boot sweep (`component.go:1225`); lifecycle per-workflow pattern watches; rule/gated-dag pattern triggers; dead `inference/storage.go:429` (cleanup) | Different patterns; unchanged |

Cost: a new shared primitive to maintain; the coherence contract re-proven at
the fan-out AND tick seams; the coalescer semantics lifted to `pkg/` (the
processor-layer implementation is not importable). **Build is deferred:** the
contract is locked now; consumers keep hand-rolling until it ships.

## Cluster boundary (#579 relatives)

- **#571** — PARTIALLY SUBSUMED (multi-reader watcher-tax half): the query
  client reads via a shared view only where one exists. Single-reader
  processes keep today's shape (memory floor). The whole-client poison
  sticky-latch (D10 carve-out) stays with #571 / the ADR-079 track, now
  drivable from the view's G6 signal.
- **#340** (raw-lane + projection guidance) — ADJACENT docs task; the view is
  its concrete referent (intermediate revisions live in the raw KV lane; G4).
- **#176** (bulk-reads/pagination) — ORTHOGONAL: one-shot bounded reads.
- **#211** (read-only MCP) — DOWNSTREAM consumer of the view when built.
- Split out of #579: two independent small `natsclient` ergonomics — attribute
  the `nats: slow consumer` log with subject/subscription, and expose watcher
  pending-limit config.
