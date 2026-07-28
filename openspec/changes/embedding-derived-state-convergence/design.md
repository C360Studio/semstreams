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
- `markStranded(entityID, reason)` = `applyTerminalOutcome(entityID, 0, OutcomeFailed, reason)` —
  **floor revision 0 is load-bearing**: the guard is `present && sourceRevision < held.rev`, so a
  marker at the observed revision (or ^uint64(0)) would be unclearable — a silent permanent-degraded
  trap. Comment this at the helper.
- Three marking sites: watcher tombstone delete failure (complete at the TRUE revision R first —
  watermark must drain, #624 — then mark at 0: two calls, different revisions, both load-bearing);
  hop-1 `SavePending` failure (watermark stays uncompleted, #613 F2, plus mark); flush source-read
  transient failure (keep drain, plus mark). Reasons (in-memory ONLY, never `SaveFailed`):
  `ReasonDeleteFailed`, `ReasonPendingWriteFailed`, `ReasonEntityReadFailed`.
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
changelog). #614/#628 CAS: untouched; hop 2 must NOT take `hop1Mu` (would serialize workers behind
metadata ops); SaveGenerated racing a tombstone already safe via CAS + `ErrRecordGone`; the seam means
hop 1 can never write a revision below one hop 2 committed. #719 boot transaction: repairLoop starts
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
