# ADR-066: Honest graph-index readiness — revision-lag "caught-up" signal

## Status

**Proposed** (2026-07-03, GH #431). Design-first; cross-repo (semstreams owns the
index/embedding signals, semsource owns the `graph.query.status` aggregate). No
code lands with this document. Part of the beta.128 "honest graph signals" QoL set
(with GH #435).

**Adversarially reviewed (2026-07-03).** The review confirmed **revision-lag is the
right signal** but caught that the first-draft concrete invariants recreated the
exact false-ready bug (and a worse never-ready deadlock) under the configs
semstreams actually ships. This revision folds in the three required corrections:
a **contiguous** high-water mark (not a monotonic max), a **query-time `LastSeq`**
Target (not idle-gated), and a **distinct embedding definition** over eligible
entities. Those corrections — not a redesign — are the substance below.

## Decision

Replace the sticky "indexing started = ready" signal with a **revision-lag /
caught-up** signal on `graph.index.query.status`:

- **`IndexedRevision`** — the **contiguous high-water mark**: the highest
  ENTITY_STATES revision `R` such that *every* revision `≤ R` has been fully
  applied to the indexes, advanced across **both** processing paths (the worker
  pool AND the inline delete handler). NOT "the highest revision a worker
  finished" — that is wrong under multi-worker pools and deletes.
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

### 1. graph-index: contiguous high-water mark (the corrected invariant)

`IndexedRevision` is a **commit-offset / contiguous watermark**, not a max. The
pool completes revisions out of order (N>1) and deletes commit inline, so the
signal must answer "all revisions ≤ R applied," which requires tracking gaps:

- Maintain `{pending set of in-flight revisions}` and `IndexedRevision`. On the
  watch delivery of revision `r`, add `r` to pending. On **completion** of `r` —
  whether by a **pool worker** finishing `processEntityUpdateFromData` OR by the
  **inline delete handler** finishing a delete — remove `r` from pending and, if
  `r == IndexedRevision + 1`, advance `IndexedRevision` past every now-contiguous
  completed revision. This advances the watermark only past the *lowest incomplete*
  revision, so a fast late worker cannot report a gap as done, and a delete's
  sequence participates rather than leaving a permanent hole.
- The coalescer (which re-`Get`s the latest per key) must still register each
  ENTITY_STATES revision it collapses as completed, or contiguity stalls even at
  `workers:1`.
- Complexity is bounded: pending is small (≤ in-flight window), advance is
  amortized O(1). This is the standard out-of-order-ack commit-offset problem.

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

Reusing graph-index's Target for embeddings is a **permanent deadlock**: only
text-bearing entities reach `SaveGenerated`; telemetry-only entities never enter
the pipeline, empty-text records are deleted without generating, failures go to
`SaveFailed`. So a Target = "latest ENTITY_STATES revision" leaves permanent gaps
and `Lag` never reaches 0. Instead:

- `IndexedRevision` (embedding) advances on **every TERMINAL outcome of an eligible
  entity** — `generated` OR deliberately-skipped-no-text OR `failed` — contiguously,
  not only on `generated`. "Processed to a terminal state," not "embedded."
- Thread the **ENTITY_STATES revision** through both hops: capture `entry.Revision()`
  at ingest → store on the pending `Record` (which has no revision field today,
  `storage.go:37-53`) → read it back at the terminal transition. `SaveGenerated`
  today takes no revision (`storage.go:164`) — this plumbing is required, not
  optional.
- Target is the same query-time `LastSeq`, but `Ready` means "every eligible
  revision ≤ Indexed reached a terminal state." Until this is implemented and
  tested, **no consumer (mcp-gateway, fusion) gates on `embedding.ready`**.

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

- The contiguous watermark is **more than "one atomic"**: a small pending-set +
  advance logic, wired into two completion paths (pool + inline delete) per
  component. This is the bulk of the implementation.
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

- **Contiguity across two paths** — the delete handler and the pool must both
  register completions into the same watermark, or a delete/late-worker leaves a
  gap (false-not-ready) or is skipped (false-ready). Unit-test out-of-order
  completion AND a trailing-delete-only tail.
- **Embedding never-ready** — if Target isn't scoped to eligible-terminal outcomes,
  `embedding.ready` deadlocks. Gate consumers off it until §3 is proven.
- **Bootstrap replay** — WatchAll replays keys in ascending revision to the nil
  marker; the contiguous watermark rebuilds from 0 correctly (transient high lag /
  not-ready during replay is *correct*). Query-time Target avoids the Target=0
  false-ready the idle-gated draft had.

## Migration path

1. `natsclient`: a `LastSeq` (stream/KV) helper.
2. graph-index: contiguous watermark across pool completion + inline delete +
   coalescer; query-time Target; honest `Ready`/`Lag`/`State` on
   `IndexStatusResponse` (fill `Revision`/`LastSynced`); degraded stuck-detector.
   Unit-test cold-start / mid-build / caught-up / out-of-order completion /
   trailing-delete / stuck-watermark.
3. Mirror on `pkg/fusion.IndexStatus`.
4. graph-embedding: revision-threading (both hops + `Record` field) + new
   `graph.embedding.query.status` with the terminal-outcome watermark.
5. semsource: mcp-gateway third fan-out (surfaced, not gated) + retire the workaround
   note; decide source-manifest gating (their ADR).
6. Tag beta.128 (bundled with GH #435); coordinate the semsource pin.

## Open questions

- **Phase field now or later?** `Lag == 0` is load-bearing; `Phase` is sugar. Ship
  the numbers first.
- **source-manifest gating** — semstreams exposes the honest signal; whether
  semsource's `phase=ready` composes it is semsource's decision.
- **Embedding-ready consumer gating** — deferred until §3 is proven; the third
  fan-out is surfaced-only meanwhile.

## Related decisions

- **GH #430 / ADR-065** — predicate-index O(N²) fix (shrinks the lag window).
- **GH #397 / ADR-062** — the index-status honesty envelope this enriches.
- **GH #420 / beta.125** — readiness-gated *reads* ("responder up," not "index
  caught up") — do not conflate.
- **GH #435** — the QoL-bundle sibling (a healthy path no longer over-reports as a
  reject).

## References

- `processor/graph-index/name_index.go` (sticky bug, handler); `component.go`
  (watch/pool `700-812`; inline delete `740-746`; coalescer re-Get `984-1010`;
  worker completion `833-980`).
- `pkg/worker/pool.go` (single-channel N-worker pool, no ordering).
- `graph/index_status.go` + `pkg/fusion/contract.go` (wire shapes); `engine_lens.go:82-85`
  (single-shot Ready gate).
- `processor/graph-embedding/component.go` (revision dropped at watch `921`; no-text
  skip `1019-1022`; 5-worker default); `graph/embedding/storage.go` (`SaveGenerated`
  no revision `164`; `Record` no revision field `37-53`); `worker.go` (second hop,
  no-text delete `310-317`, `markFailed` `496`).
- `configs/hello-world.json`, `e2e-structural.json`, `statistical.json` (`workers` 2/4).
- `semsource/processor/source-manifest/status.go`; `semsource/processor/mcp-gateway/tools.go`.
