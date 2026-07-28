# Design: embedding derived-state convergence

Full analysis by the architect (2026-07-28), verified at `f7965f0e`; decisions condensed here.
The unifying precedent is graph-index's "Durable recovery" requirement
(`openspec/specs/graph-index/spec.md:184-191`) — updates, deletes, coalesced events, and repair share
ONE dispatch that reconciles authoritative `ENTITY_STATES` at execution. graph-embedding gets the same
contract with a simpler realization: its hop-1 work is two KV metadata ops, so the seam is a **mutex,
not a keyed pool** (adopting `pkg/dispatch.KeyedPool` would buy N lanes/goroutines/metrics/lifecycle to
protect two round-trips — the ratchet the owner forbade).

## Decision 1 — #629: mutex-serialized hop-1 seam (`hop1Mu`), reconcile-at-execution

- `reconcileEntity(ctx, entityID)`: under `hop1Mu`, Get `ENTITY_STATES[entityID]`;
  `ErrKeyNotFound`/`ErrKeyDeleted` (full sentinel set via `errors.Is` — never `==`) → converge by
  `DeleteEmbedding` + `completeEmbedding(k, ^uint64(0), OutcomeDeleted)` (max-rev drain is existing
  idiom, `component.go:1459-1464`; `revlag.Watermark.Complete` drains only currently-pending revisions,
  records no future floor); transient error → `OutcomeSkipped` drain + `markStranded(ReasonEntityReadFailed)`;
  present → `queueEntityForEmbedding` (the SOLE hop-1 record writer — preserves the #638 IdentityText
  rolling-upgrade contract by construction).
- `processEntityBatch` collapses to a loop over `reconcileEntity` (ctx guard retained). The watcher's
  own update/tombstone paths take the same lock.
- Correctness: tombstone before the Get → absence branch deletes; tombstone after the Put → its delete
  serializes after and wins; the between-Get-and-Put interleaving (the bug) is structurally removed.
- **Why mutex, not channel-into-watcher**: `CoalescingSet.Close()` blocks on the in-flight callback
  (`pkg/cache/coalescing_set.go:114-125,145-175`) and `Stop` calls it while holding `c.mu` — a
  callback blocked sending to an exited watcher deadlocks `Close`. Mutex has no such hazard, avoids
  batch head-of-line blocking, and Go's starvation-mode FIFO handoff bounds watcher stalls to ~one
  entity's KV latency.
- **Rejected**: revisioned delete-marker / CAS-create (4 durable-state questions incl. retention-guard
  interaction and rolling-upgrade); value-carrying coalescer (snapshot goes stale again post-drain —
  the fresh Get IS the mechanism; graph-index re-Gets at lane head despite carrying revisions);
  in-memory recently-deleted set (window patch + GC, strictly more machinery).
- **semsource cost at 200ms**: coalesced-serialized = 2E round-trips per window on the watcher
  goroutine vs the shipped immediate mode's N; cheaper whenever dedup N/E > 2 (semsource's reason for
  the knob), at worst ~2× at low dedup. Heavy embedding work is hop 2 (worker pool, own WatchAll) —
  verified not on this path.
- **Lock-order invariant (stated in code): `hop1Mu` → `failedMu`, never reverse.** `repairTargets()`
  snapshots under `failedMu`, releases, THEN dispatches `reconcileEntity`. No path under `hop1Mu` may
  take `c.mu` (asserted in review — `Stop` holds `c.mu` across `Close`).
- `Stop` hardening: move `c.cancel()` above `entityCoalescer.Close()` so an in-flight flush aborts on
  ctx instead of finishing KV ops.

## Decision 2 — #625: repair from the existing #613 failed map; NO durable evidence

- Restart recovery already exists: `WatchAll` (no options) delivers tombstones under
  last-per-subject; no `PurgeDeletes` anywhere in repo; `ENTITY_STATES` is retention-guard-asserted.
  The gap is one process lifetime → in-memory re-drive closes it completely. graph-index's precedent
  is likewise in-memory (`failedEntities sync.Map`). A durable marker would be self-defeating (writes
  to the bucket whose delete failed).
- **Stranded marks clear ONLY by causal convergence** (Codex #722 round, replacing the original
  floor-revision-0 rule — **FALSIFIED**: hop 2 is deliberately outside `hop1Mu`, so a worker already
  in flight for an OLDER revision can reach its terminal AFTER a stranding; under floor 0 that
  obsolete terminal cleared the mark — dead vector queryable, FailedCount 0, ready, repair no longer
  targeting. An obsolete terminal must not count as convergence). The in-memory entry carries a
  `strandedAt` revision (in-memory only — ledger still zero durable state);
  `markStranded(entityID, reason, strandedAt)` writes it directly; `applyTerminalOutcome` keeps its
  embedder-side semantics but refuses to clear (or overwrite-with-failure) a stranded entry when
  `sourceRevision < strandedAt`. Clearing happens (a) explicitly via `clearStranded` on every hop-1
  convergence — successful delete/skip/queue under the seam, plus reconcile's absence drain at
  max-rev — or (b) by an external terminal with `sourceRevision >= strandedAt`. The explicit clear is
  what prevents the unclearable-pin trap the floor-0 rule feared, while restoring causality.
- Marking sites and their stranding revisions: watcher tombstone delete failure (complete at the TRUE
  revision R first — watermark must drain, #624 — THEN mark at `strandedAt = R`: two calls,
  drain-then-mark, both load-bearing); hop-1 pending-write failure, both lanes (watermark stays
  uncompleted, #613 F2; `strandedAt =` the delivered revision); no-text-transition delete failure
  (`strandedAt =` the delivered revision; reviewer round 1); reconcile source-read / absence-delete
  failure (`strandedAt = ^uint64(0)` — a failed Get/absent key yields no authoritative revision, so
  ONLY explicit convergence clears; repair's 30s cadence bounds the extra degraded window). Reasons
  (in-memory ONLY, never `SaveFailed`): `ReasonDeleteFailed`, `ReasonPendingWriteFailed`,
  `ReasonEntityReadFailed`.
- **Guarded pending write** (Codex #722 B2): `repairTargets` snapshots then releases `failedMu`, so
  hop 2 can generate and causally clear an entry before the dispatch loop reaches it; the stale
  re-drive's unconditional `SavePending` Put then DOWNGRADED the fresh `StatusGenerated` record to
  pending (vector dropped from the cache until regeneration, readiness already ready). A failed-map
  recheck is insufficient (hop 2 can complete after it). Fixed at the SOLE hop-1 writer so all lanes
  harden uniformly: ONE additive storage method `SavePendingGuarded(ctx, *Record) (saved bool, err)`
  (both pending lanes) reads the current record inside the seam, SKIPS when
  `StatusGenerated && SourceRevision >=` the authoritative revision being queued (also turns a
  restart's re-delivered already-generated revision into a cheap skip), and writes conditionally —
  CAS-create when absent, `Update` at the read revision when present — re-reading and re-deciding on
  conflict. A guarded SKIP is terminal for the delivered revision (Skipped completion; discharge).
- **Coalescer publication ordering** (Codex #722 H3): `Start` constructs and publishes
  `entityCoalescer` BEFORE the watcher goroutine launches — assigning after launch was a data race on
  the pointer, and with a preloaded ENTITY_STATES bucket the bootstrap replay took the immediate lane
  despite `coalesce_ms > 0`. Every Start failure path after construction closes it
  (`closeCoalescerAfterFailedStart`, after cancel so Close cannot block).
- **Repair loop**: dedicated 12-line 30s ticker (mirrors `indexRepairInterval` + empty-set
  short-circuit), launched with the existing goroutines in `waitForDependenciesAndStartWatcher`
  region, `c.wg`-registered, ctx-cancelled, drained by existing `wg.Wait`. Piggybacking the ADR-083
  heartbeat REJECTED: repair I/O on that goroutine delays heartbeat publication whose freshness is a
  consumer-visible liveness contract (`FreshnessMultiplier × DefaultHeartbeat`); graph-index's own
  comment makes the same call; and `statusTickInterval` is test-overridable (a 10ms test would
  hot-loop repair).
- **Unbounded flat retry** justified: the repair set is reason-scoped to KV-transport faults on
  self-owned buckets — no poison class (embedder-side reasons stay out, preserving current recovery
  semantics). Bounded-give-up would recreate the #625 leak; per-entity counters are new state for no
  gain. `FailedCount>0 → degraded` is the operator signal (existing).
- "Repaired" contract: key absent + `FailedCount` decremented + watermark UNCHANGED by repair.
  Observable transition `degraded → ready` via the existing projection. Repair of a PRESENT entity
  re-queues pending → bumps the key → hop-2 WatchAll re-drives — persistence misses repaired free.

## Compose-check (condensed; full table in the architect analysis)

ADR-066 watermark: preserved; intended change = failed derived writes report degraded (spec delta +
changelog). #614/#628 hop-2 CAS lanes: untouched; hop 2 must NOT take `hop1Mu` (would serialize
workers behind metadata ops); SaveGenerated racing a tombstone already safe via CAS + `ErrRecordGone`;
and the hop-1 create lane is now itself revision-guarded (`SavePendingGuarded`, Codex #722 B2), so
hop 1 can neither interleave a delete (the seam) nor downgrade a committed generated vector (the
guard). #719 boot transaction: repairLoop starts
inside component Start post-dependency, returns no error, health-visible via degraded readiness —
satisfies framework-composition spec. Epic C constraint: zero new buckets/keys/fields/vocab; guard
test asserts the new reasons never reach `SaveFailed` (`normalizeFailureReason` NOT extended).
Retention guard: not engaged (the point of rejecting the marker). No collision with in-flight changes
(no active change carries a graph-embedding delta).

## E2E + coverage honesty

Gate: `e2e:statistical` (exercises hop-1→hop-2→search; semantic adds nothing this diff reaches).
The coalesced lane is uncovered by every semstreams e2e config while semsource defaults it on —
the deterministic integration tests are the REAL gate (say so in tasks), and one statistical config
gains `coalesce_ms` as a config-only fixture edit inside this change.
