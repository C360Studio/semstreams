# ADR-066: Honest graph-index readiness — revision-lag "caught-up" signal

## Status

**Accepted** (2026-07-03, GH #431). Cross-repo (semstreams owns the
index/embedding signals, semsource owns the `graph.query.status` aggregate).
Implementation follows the Migration path below on
`fix/gh431-honest-index-readiness`. Part of the beta.128 "honest graph signals"
QoL set (with GH #435).

**Adversarially reviewed (2026-07-03), twice.** The first (design) review confirmed
**revision-lag is the right signal** but caught that the first-draft concrete
invariants recreated the exact false-ready bug (and a worse never-ready deadlock)
under the configs semstreams actually ships: it folded in a **query-time `LastSeq`**
Target (not idle-gated) and a **distinct embedding definition** over eligible
entities. A second **code-grounded** review (against `nats.go` v1.48.0 and the
nats-server storage layer) then corrected the watermark *algorithm* itself: the
"contiguous, advance-from `IndexedRevision+1`" formulation **stalls permanently**
because ENTITY_STATES runs at **History=1** and `WatchAll` delivers a **sparse**
latest-per-key revision set (§1). The corrected watermark is **low-water-of-pending**
with a **single key-scoped completion rule**, sound precisely because `OrderedConsumer`
guarantees monotonic-ascending delivery. That review also fixed the completion wiring
(coalescer-collapse + delete-orphan) and pinned two honest-scope boundaries (§1
*Scope boundary*). Those corrections — not a redesign — are the substance below.

## Decision

Replace the sticky "indexing started = ready" signal with a **revision-lag /
caught-up** signal on `graph.index.query.status`:

- **`IndexedRevision`** — the **low-water-of-pending watermark**: the highest
  revision `R` such that every *delivered* ENTITY_STATES revision `≤ R` has been
  applied and nothing `≤ R` is still in flight, advanced across **both** processing
  paths (the worker pool AND the inline delete handler). Concretely
  `pending.empty() ? observedHigh : (minPending − 1)` — NOT "the highest revision a
  worker finished" (wrong under multi-worker pools / deletes) and NOT absolute
  revision contiguity (which stalls forever under History=1 sparse delivery — see §1).
- **`TargetRevision`** — the stream's current `LastSeq`, read **at query time,
  always** (not derived from the last `Delta()==0` watch entry, which is stale
  exactly when writes are committed-but-not-yet-delivered).
- **`Lag = TargetRevision − IndexedRevision`**; **`Ready = TargetRevision > 0 &&
  IndexedRevision >= TargetRevision`** (no lossy `max(0,…)` clamp — a stale or
  uninitialized Target must not read as caught-up).
- optional **`Phase`** = `ingesting | indexing | ready` — the friendly projection.

The mechanism is **(a) revision-lag**; the phase vocabulary is **(d)**, its
projection. **Embeddings do NOT share this exact definition** (see Design) — a
naive shared Target is a permanent deadlock. Add a **new
`graph.embedding.query.status`** handler with an embedding-specific
terminal-outcome watermark, and **no consumer gates on `embedding.ready` until
that definition lands**.

## Context

### The bug (verified)

`processor/graph-index/name_index.go` `nameIndexIsReady` flips a **sticky**
`nameIndexReady` atomic to `true` the instant the NAME_INDEX bucket has ≥1 key,
and `handleQueryStatusNATS` reports `Ready:true, State:"ready"` from it — "indexing
started," not "caught up." `Revision` and `LastSynced` exist on
`IndexStatusResponse` (`graph/index_status.go:18-19`) but are **never populated**.
There is **no** `graph.embedding.query.status` handler, so embeddings are absent
from every readiness signal.

### The impact (GH #431 dogfooding, ~21k Go entities)

`phase: ready` fired at ~30s; byName resolution of provably-existing symbols
climbed 2/15 → ~9–10/15 over minutes and plateaued with symbols still missing at
6–8 min; `total_entities` kept climbing after `ready`; embeddings were ~empty
throughout. A consumer that correctly waits for `ready` then queries gets **false
negatives**, unable to distinguish "absent" from "still indexing."

### Relationship to GH #430 (shipped, beta.127)

#430 fixed the O(N²) predicate-index path — indexes build *faster*, the lag window
shrinks. The readiness *contract* is separate: even a fast index must honestly say
"queryable," and #430 does not touch embeddings.

### The architecture (why this shape, and why the invariants are subtle)

graph-index is write-driven, lagging, async: a `kv-watch` over ENTITY_STATES
(`component.go:700-761`) feeds a **worker pool** (`pkg/worker/pool.go` — a single
buffered channel drained by N goroutines, **no completion ordering**), with an
optional coalescer that re-`Get`s the *latest* revision per key. **Reference
configs run `workers: 2–4`** (`configs/hello-world.json`, `e2e-structural.json`,
`statistical.json`). **Deletes are handled inline in the watcher**
(`component.go:740-746`), NOT through the pool. Every watch entry carries
`Revision()` (stream seq) and `Delta()` (distance from latest). graph-embedding
uses the same watch→pool pattern **plus a second async hop** (`pending` →
`generated`, default **5 workers**), and only *text-bearing* entities enter it.

These three facts — no ordering, inline deletes, text-only embeddings — are why the
naive "monotonic max over pool completions, Target from `Delta()==0`, same block
for embeddings" is wrong.

## Why revision-lag (a), and why not the alternatives

**Caught-up = `IndexedRevision >= TargetRevision`** is the only option the
architecture supports cheaply and honestly. It distinguishes cold-start
(`Indexed==0, Target>0` → not ready), mid-build (`Indexed < Target` → exact numeric
lag), and caught-up — the exact confusion #431 is about. Refuted:

- **(b) coverage-%** — no cheap "indexable" denominator (telemetry-only entities
  never index; NAME_INDEX keys are shared name-hashes, so key-count ≠ entity-count).
- **(c) pending-queue-depth** — reads `0` both when caught-up **and** when nothing
  has started; re-creates the cold-start ambiguity. Secondary velocity gauge only.
- **(d) phase-split** is the projection of (a)'s numbers, adopted as the vocabulary,
  not a competing mechanism.

## Design

### 1. graph-index: low-water-of-pending watermark (the corrected invariant)

`IndexedRevision` answers "every committed ENTITY_STATES revision ≤ R has been
dispatched through and returned from the indexer." The naive "advance from
`IndexedRevision+1` past every now-contiguous completed revision" **is wrong here
and stalls forever**: ENTITY_STATES is created with **no `History`** override →
NATS KV default **History=1** (`graph-ingest/component.go:804`), and `WatchAll`
binds an **`OrderedConsumer` + `DeliverLastPerSubject`** (`nats.go` `jetstream/kv.go:1273`).
Bootstrap therefore delivers only the *latest surviving revision per key* — a
**sparse** revision set (superseded revisions are purged, never delivered). Waiting
for revision `1, 2, …` that will never arrive pins the watermark at 0 permanently —
a false-*not*-ready worse than the false-ready this ADR fixes.

The correct watermark tracks quantities defined **entirely over delivered
revisions**, so purged gaps are never in play:

- **`observedHigh`** — the highest revision ever *delivered to the watcher*.
  Monotonic; updated on **every** watch entry, update AND delete.
- **`pending`** — per key, the set of delivered-but-not-yet-completed revisions.
- **`IndexedRevision = pending.empty() ? observedHigh : (minPending − 1)`**, where
  `minPending` is the smallest still-pending revision across all keys.

**One completion rule, every path — key-scoped, not exact-revision.**
`complete(key, rev)` removes from `pending[key]` **every** revision ≤ `rev`. Called
from (a) a **pool worker** on return, with the entry it processed
(`complete(entry.Key(), entry.Revision())`), and (b) the **inline delete handler**,
with the delete entry's own revision. Key-scoped-≤ (not exact) is load-bearing:

- The **coalescer** collapses several observed revisions of a key into one re-`Get`
  at the latest surviving revision `rg`; `complete(key, rg)` drains the collapsed
  lower revisions no worker will ever see individually. (Exact-revision completion
  strands them → permanent not-ready. This is why the first draft was wrong even
  ignoring History=1.)
- A **delete** of a key with an earlier still-pending update drains that update —
  its sequence is superseded by the tombstone — closing the delete-orphan hole.
- On the direct path it is a no-op beyond the exact revision unless two updates to
  the *same key* are in-flight, where the later-completing higher revision
  optimistically drains the lower — honest, because ENTITY_STATES stores full-state
  snapshots (the newer write already subsumes the older). See the *Scope boundary*.

**Why it is correct.** It rests on one property, which `OrderedConsumer` guarantees
at the nats-server storage layer for **both** the `DeliverLastPerSubject` bootstrap
replay and live updates: **delivery is monotonic ascending in `Revision()`** (the
skip-list of last-per-subject seqs is sorted server-side; the ordered-consumer reset
path only ever re-delivers `> lastGood`). Because delivery is ascending, every
revision ≤ `observedHigh` that will *ever* be delivered already has been; any
un-observed revision below `observedHigh` is purged and correctly skipped. This
yields three structural guarantees:

- `IndexedRevision ≤ TargetRevision` **always** (pending empty → `observedHigh ≤
  LastSeq`; else → `minPending−1 < observedHigh ≤ LastSeq`), so `Lag ≥ 0`
  structurally — the no-`max(0,…)` clamp is *safe*, not stylistic.
- `IndexedRevision` is monotonic non-decreasing (a newly observed revision exceeds
  `observedHigh`, so `minPending` never drops below a value already reported ready).
- `Ready` (`Indexed ≥ Target`) is reachable only when `pending` is empty AND
  `observedHigh` has climbed to `Target` — exactly "caught up."

`observedHigh`/`pending` are guarded by one mutex (touched by the watch goroutine,
N pool workers, the coalescer callback, and the query handler); `observe`
happens-before `complete` on every path (channel send precedes worker receive;
delete is single-goroutine), so there is no `complete`-before-`observe` underflow.
Complexity is bounded: `pending` ≤ the in-flight window; a per-key sorted structure
+ maintained minimum keeps observe/complete O(log n) and the query-time read O(1).

**Scope boundary — what `Ready` does and does NOT promise.** `Ready` means
**revision coverage**, not last-writer-wins **freshness** or per-index write
**success**. Two pre-existing pipeline adjacencies are out of this ADR's scope and
must not be silently inherited as stronger promises:

- *Multi-worker stale overwrite* (live under the shipped `workers: 2–4`): the pool
  has no completion ordering (`pkg/worker/pool.go`), so two rapid updates to the
  *same key* can land the older revision's index writes last, leaving stale data
  while the watermark honestly reports the revision *covered*. It does **not** fire
  on the one-shot bulk ingest #431 is about (distinct entities, one write each); it
  needs same-key churn. A per-key last-applied-revision guard that drops superseded
  entries is a **companion follow-up**, not bundled here.
- *Swallowed per-index write failures*: `processEntityUpdateFromData` logs sub-index
  write failures at Debug and returns (after CAS-retry); completion fires on worker
  **return**, so a persistently-failing sub-write still counts as covered. Making
  those loud (Warn + counter) is a **companion follow-up**.

A consumer that needs freshness (not just coverage) gates on the companions; this
ADR does not claim more than coverage.

### 2. graph-index: Target = query-time `LastSeq` (always)

`TargetRevision` is read from the ENTITY_STATES stream's current `LastSeq` **on
every status call**, not cached from the last `Delta()==0` watch entry. The stale
window (writes acked to the stream but not yet delivered to the watch) is precisely
when a cached Target lies. `entry.Revision()` and stream `LastSeq` share the stream
sequence number space, so `LastSeq` reflects every *committed* write; the only
residual window is an unacked in-flight write, which is not a durable fact and which
no consumer has been told about — the correct place to stop. This needs a
**net-new `natsclient` helper** to read a KV/stream `LastSeq` (none exists today);
status is polled (not hot), so one stream-info round-trip per call is acceptable.

### 3. graph-embedding: a DISTINCT terminal-outcome watermark (not the same block)

**Implemented** (reuses `pkg/revlag.Watermark` + `graph.ComputeIndexStatus`; new
`graph.embedding.query.status`). Reusing graph-index's semantics naively is a
**permanent deadlock**: only text-bearing entities reach `SaveGenerated`;
telemetry-only entities carry no text, so a Target = "latest ENTITY_STATES revision"
would leave their revisions permanently un-terminal and `Lag` never reaches 0. The
resolution — verified by a code-grounded adversarial review against the **real
two-hop / two-bucket** pipeline (the ADR's first draft cited a `worker.go` layout
that does not exist):

- The pipeline is **hop 1** (`processor/graph-embedding/component.go` watches
  ENTITY_STATES → `queueEntityForEmbedding`) and **hop 2** (`graph/embedding/worker.go`
  watches the *EMBEDDING_INDEX* bucket for pending `Record`s → generate).
- `IndexedRevision` advances on **every TERMINAL outcome** — `generated` OR
  deliberately-skipped-no-text OR `ineligible-skip` OR `failed` OR `delete` — not only
  `generated`. Because every observed ENTITY_STATES revision reaches exactly one
  terminal across the two hops, `Ready = Indexed >= Target(LastSeq)` **is** reachable
  even though only text-bearing entities embed. That is the deadlock-avoidance.
- Thread the **ENTITY_STATES revision** through both hops: `entry.Revision()` at hop-1
  → new `Record.SourceRevision` field → `SavePending(…, sourceRevision)` → hop-2 reads
  it and fires a single `WithOnTerminal(entityID, sourceRevision)` callback (a `defer`
  placed **after** the not-pending skip, so a re-delivered generated/failed record does
  not double-complete) → `watermark.Complete`. `SaveGenerated` rebuilds the record and
  drops `SourceRevision` — fine, those records only ever hit the not-pending skip.
- **Completion-wiring hazards the review caught (all fixed):** (D1) a `SavePending`
  **network Put** failure must `Complete(key, rev)` — unlike graph-index's shutdown-only
  `SubmitBlocking`, this fails transiently mid-run and would strand the bulk-ingest path
  permanently; (D2) `offloaded-excluded` is **NOT** a terminal — it falls through to the
  inline-text path, so completing there is a false-ready; (D3) a corrupt hop-2 record
  (`worker.go` unmarshal-fail) cannot yield its revision → **max-rev drain** (`^uint64(0)`)
  + loud Warn, so one poison record cannot wedge the whole signal.
- **`SourceRevision==0` (a legacy pending record) → no-op** (`Complete(key,0)` is a
  natural no-op; KV seqs start at 1). NOT a max-rev drain: hop-1's own bootstrap
  re-observe re-queues the entity with the real revision, so the genuine completion is
  guaranteed — a max-rev drain would instead cause a transient false-ready mid-replay.
- **Stuck detector is COMPLETIONS-based**, not IndexedRevision-based, with a longer
  threshold (embeddings add a slow external-LLM hop): a single slow call that pins
  `Indexed` while other workers finish out of order is healthy, not degraded; only a
  window with zero terminal completions (while lagging) is degraded.
- **Scope boundary:** like graph-index, `Ready` means terminal *coverage*, not embedding
  *success* — a backend outage that mass-`SaveFailed`s reads as caught-up (every
  revision terminal). Counter/failure-ratio degraded signal is a companion follow-up.
  Also: under **same-key churn + an ordered-consumer reset** (K written twice, the older
  pending superseded by a `generated` record that drops `SourceRevision`, and the newer
  pending's delivery lost to a reset), that revision can strand — read as `building`, not
  `degraded`, since other entities keep the completions detector healthy. It does NOT
  affect the gh#431 bulk-ingest path (distinct entities, one write each) and **self-heals
  on the key's next write** (`Complete(K, newRev)` drains it as `≤`); noted so it is not
  mistaken for a regression.
- Target is the same query-time `LastSeq`. **No consumer (mcp-gateway, fusion) gates on
  `embedding.ready`** until this is proven in the field; the handler is surfaced for
  observability only.

### 4. Degraded overrides (was punted; the review made it load-bearing)

If the backend faults mid-build, `IndexedRevision` **stalls** while query-time
Target keeps climbing → `Lag` grows unbounded. That must NOT read as "still
building normally." `State: degraded` **overrides** ready/building. Distinguish
"lag increasing because ingest is fast" (healthy, `building`) from "indexer stuck"
(`degraded`): flip to `degraded` when `IndexedRevision` is unchanged across `> K`
status intervals while Target advances (a stuck-watermark detector), in addition to
the existing backend-fault degraded triggers.

### 5. Wire shape

Enrich `IndexStatusResponse` (`graph/index_status.go`) and mirror
`pkg/fusion.IndexStatus` (`pkg/fusion/contract.go`) — field-identical, decoded
directly by the fusion `RetrievalClient`, so they change together:

```go
type IndexStatusResponse struct {
    Ready           bool   `json:"ready"`            // Target>0 && Indexed>=Target
    State           string `json:"state"`            // building | ready | degraded
    IndexedRevision uint64 `json:"indexed_revision,omitempty"` // contiguous watermark
    TargetRevision  uint64 `json:"target_revision,omitempty"`  // query-time LastSeq
    Lag             uint64 `json:"lag,omitempty"`    // Target - Indexed; 0 == caught up
    Phase           string `json:"phase,omitempty"`  // ingesting | indexing | ready
    Revision        string `json:"revision,omitempty"`     // now populated (string IndexedRevision)
    LastSynced      string `json:"last_synced,omitempty"`  // now populated
}
```

## semsource coordination (cross-repo)

1. **`mcp-gateway/tools.go` `source_status`** already fans out to
   `graph.query.status` + `graph.index.query.status` and carries a `readinessNote`
   that *explicitly names semstreams#431* ("poll until `index.ready` AND
   `total_entities` stopped climbing"). Once `index.ready` is honest: add a third
   fan-out to `graph.embedding.query.status` (surfaced but **not** gated on until §3
   lands), and **retire** the "stopped climbing" clause.
2. **`source-manifest` `graph.query.status phase=ready`** is a *separate*
   false-positive (fires when all *sources* reported, not when the graph is
   queryable). Whether it should itself gate on the caught-up signal is a
   **semsource ADR call**; this ADR only commits to exposing the honest signal.
3. **Version note:** `Revision`/`LastSynced` already ship (unused) → additive; the
   semantic change is `Ready`. Coordinate the tag — semsource pins semstreams.

## Consequences

### Positive

- `ready` finally means **queryable** — correctly-gating consumers stop getting
  false negatives. Exact numeric `Lag`/`IndexedRevision` also enable a **finer
  contract**: a consumer that knows its target revision can gate on
  `IndexedRevision >= myRev` instead of the global bool.
- Embeddings enter a readiness signal for the first time (safely, once §3 lands).
- The mcp-gateway "stopped climbing" workaround retires; dead wire fields filled.

### Negative / cost

- The low-water-of-pending watermark is **more than "one atomic"**: a per-key
  pending structure + `observedHigh`, wired into the single key-scoped completion
  rule on both paths (pool return + inline delete) per component. This is the bulk
  of the implementation.
- A query-time `LastSeq` read per status call (new natsclient helper).
- Embedding revision-threading through both hops + the pending `Record` gains a
  revision field.
- **`Ready` semantic change:** `pkg/fusion.Engine.Fuse` gates once per request on
  `Ready` (`engine_lens.go:82-85`) and returns empty when not ready (no spin — the
  eager→later flip is *safe*, callers fall back). But post-fix, `Fuse` returns empty
  for the **entire multi-minute build window even for already-indexed symbols** — a
  coarse global gate. That is correct per the honesty contract; the numeric fields
  are the intended escape (gate on `IndexedRevision >= myRev`), and the interim cost
  must be documented so it is not mistaken for a regression.

### Risks

- **Completion wiring across paths** — the delete handler and the pool workers must
  both call the *same* key-scoped `complete(key, rev)`, or a coalescer-collapsed
  lower revision / a delete-superseded pending update strands forever (permanent
  not-ready, which the §4 stuck-detector then reports as `degraded`). Unit-test:
  out-of-order pool completion, a coalescer collapse (several revisions of one key →
  one re-Get), a delete that supersedes a still-pending update, and a
  trailing-delete-only tail.
- **Sparse-delivery stall** — the reason the naive contiguous-from-`IndexedRevision+1`
  algorithm is rejected (§1): History=1 + `DeliverLastPerSubject` means most
  revisions ≤ `LastSeq` are purged and never delivered. Any watermark that waits on
  absolute revision contiguity deadlocks. The unit tests must feed a **sparse**
  ascending revision stream (e.g. `{50, 72, 100}`), not a dense `1..N`.
- **Embedding never-ready** — if Target isn't scoped to eligible-terminal outcomes,
  `embedding.ready` deadlocks. Gate consumers off it until §3 is proven.
- **Bootstrap replay** — `WatchAll` replays last-per-key in ascending revision to the
  nil marker; `observedHigh` climbs and `pending` drains as replay proceeds, so the
  watermark rebuilds correctly from a cold start (transient high lag / not-ready
  during replay is *correct*). Query-time Target avoids the Target=0 false-ready the
  idle-gated draft had. An **empty** ENTITY_STATES (`LastSeq==0`) reports not-ready
  until the first write — deliberate (`Target>0` guard), but a genuinely-empty
  deployment gating on `Ready` waits indefinitely.

## Migration path

1. ✅ `natsclient.BucketLastSeq` (query-time Target).
2. ✅ graph-index: low-water-of-pending watermark (`pkg/revlag.Watermark`) with the
   single key-scoped `Complete(key, rev)` on pool return + inline delete; query-time
   Target; honest `Ready`/`Lag`/`State`; degraded stuck-detector. Sparse-stream unit
   tests + a wired integration test. (commit `2ccffba3`)
3. ✅ Mirror on `pkg/fusion.IndexStatus` + fix the fusionnats client to decode all
   fields (was dropping the revision-lag fields).
4. ✅ graph-embedding: `Record.SourceRevision` threaded through both hops; terminal
   completion on every outcome (generated / failed / no-text / ineligible / delete);
   `WithOnTerminal` callback; new `graph.embedding.query.status`; completions-based
   stuck detector. Deadlock-avoidance integration test (text + telemetry-only both
   catch up). Watermark mechanism extracted to `pkg/revlag`, projection to
   `graph.ComputeIndexStatus`.
5. semsource (cross-repo, NOT in this repo): mcp-gateway third fan-out (surfaced, not
   gated) + retire the workaround note; decide source-manifest gating (their ADR).
6. Tag beta.128 (bundled with GH #435); coordinate the semsource pin.

## Open questions

- **Phase field now or later?** `Lag == 0` is load-bearing; `Phase` is sugar. Ship
  the numbers first.
- **source-manifest gating** — semstreams exposes the honest signal; whether
  semsource's `phase=ready` composes it is semsource's decision.
- **Embedding-ready consumer gating** — `graph.embedding.query.status` now ships
  (§3 implemented), but **no consumer gates on it** until it is proven in the field;
  the mcp-gateway third fan-out is surfaced-only meanwhile.

## Related decisions

- **GH #430 / ADR-065** — predicate-index O(N²) fix (shrinks the lag window).
- **GH #397 / ADR-062** — the index-status honesty envelope this enriches.
- **GH #420 / beta.125** — readiness-gated *reads* ("responder up," not "index
  caught up") — do not conflate.
- **GH #435** — the QoL-bundle sibling (a healthy path no longer over-reports as a
  reject).

### Companion follow-ups (surfaced by the code-grounded review, NOT bundled here)

- **Multi-worker stale overwrite** — the pool has no completion ordering, so under
  same-key churn an older revision's index writes can land last. `Ready` stays honest
  about *coverage*; a per-key last-applied-revision drop-guard would make it honest
  about *freshness*. To file. Does not affect the #431 bulk-ingest path.
- **Silent per-index write failures** — `processEntityUpdateFromData` swallows
  sub-index write errors at Debug; readiness trusts worker-return. Elevate to
  Warn + counter so `Ready` cannot over-report on persistent write failure. To file.
- **Embedding mass-failure reads as caught-up** — a backend outage that
  mass-`SaveFailed`s advances the watermark (every revision terminal) → `embedding.ready`
  true with embeddings missing. Add a failed-terminal counter / failure-ratio degraded
  signal. Sharper than the graph-index analog because it silently degrades search
  relevance. To file.

## References

- `processor/graph-index/name_index.go` (sticky bug, handler); `component.go`
  (watch/pool dispatch `722-761`; inline delete `740-746`; coalescer create
  `522-527` + re-Get `984-1010`; worker completion `820-980`; `Workers` floors to 1
  at `96`).
- `pkg/worker/pool.go` (single-channel N-worker pool, no completion ordering
  `301-338`).
- `pkg/cache/coalescing_set.go` (batches by KEY only, drops revisions; `Remove` on
  delete `60-64`).
- `graph/index_status.go` + `pkg/fusion/contract.go` (wire shapes); `engine_lens.go:83-85`
  (single-shot Ready gate); `pkg/fusion/retrieval.go:20` (`Status` decode site).
- `processor/graph-ingest/component.go:804` (ENTITY_STATES created with no `History`
  → default 1); `natsclient/client.go:970` (`CreateKeyValueBucket`, no history
  injection).
- `nats.go` v1.48.0 `jetstream/kv.go` (`WatchAll` = `OrderedConsumer` +
  `DeliverLastPerSubject` `1273-1275`; `*KeyValueBucketStatus.StreamInfo().State.LastSeq`
  `820`); ascending last-per-subject delivery guaranteed at the nats-server storage
  layer (skip-list sorted; ordered-consumer reset re-delivers `> lastGood`).
- graph-embedding is **two hops over two buckets**: hop-1
  `processor/graph-embedding/component.go` (`watchEntityStates`, `queueEntityForEmbedding`
  terminal-skips, `processEntityBatch` coalescer re-Get, `readiness.go` projection +
  completions-based stuck detector); hop-2 `graph/embedding/worker.go` (`handleKVEntry`
  watches EMBEDDING_INDEX, `SaveGenerated`/`markFailed`/no-text `DeleteEmbedding`,
  `WithOnTerminal` callback). `graph/embedding/storage.go` (`Record.SourceRevision`;
  `SavePending`/`SavePendingWithStorageRef` take the source revision).
- `configs/hello-world.json`, `e2e-structural.json`, `statistical.json` (`workers` 2/4;
  none set `coalesce_ms` → coalescer dormant in shipped configs).
- `semsource/processor/source-manifest/status.go`; `semsource/processor/mcp-gateway/tools.go`.
