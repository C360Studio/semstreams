# Embedding derived-state convergence (#625 + #629)

## Why

Two graph-embedding integrity gaps are one missing invariant — the one graph-index already specs as
"Durable recovery" (gh#474): derived state must converge on authoritative `ENTITY_STATES` read at
execution time, through one serialized seam, with failed derived writes repaired in the background.
#629: the coalescer's batch flush interleaves with the watcher across two buckets, so a tombstoned
entity can be resurrected via the unguarded `SavePending` create lane — and contrary to the issue's
"dormant, opt-in" framing, **semsource enables coalescing by default at 200ms** (`run.go:742`), so the
race is production-reachable in our primary adopter. #625: a failed `DeleteEmbedding` leaves a dead
vector queryable until restart while readiness reports caught-up — graph-embedding shipped neither
graph-index's readiness-withhold nor its repair loop.

## What Changes

- **Single-writer hop-1 seam** (`hop1Mu` mutex): every hop-1 `EMBEDDING_INDEX` mutation — watcher
  update, watcher tombstone delete, coalesced flush, repair — serializes through it. New
  `reconcileEntity` re-reads authoritative `ENTITY_STATES` inside the lock and converges: absent →
  delete derived record; present → queue through the existing writers. Closes #629 structurally with
  zero new durable state; the coalescer (and semsource's debounce) is kept.
- **Repair loop** (12-line ticker, 30s, dedicated goroutine mirroring graph-index): re-drives entities
  marked failed for the three derived-write/read reasons via `reconcileEntity`. Failed deletes,
  failed pending writes, and failed source reads now mark the existing #613 in-memory failed map
  (`markStranded`, floor revision 0), so they surface as `degraded` readiness immediately and clear on
  repair. Embedder-side failure reasons stay out of the repair lane (no poison class, no hot loop).
  Closes #625 with zero durable evidence — restart recovery already exists via tombstone re-delivery.
- The #624 invariant is preserved verbatim: a failed delete never pins the readiness watermark; it now
  additionally reports `degraded` instead of `ready` (the truthfulness fix).
- Config-only e2e fixture edit: set `coalesce_ms` on graph-embedding in one statistical-tier config —
  the lane being fixed is currently uncovered by every semstreams e2e config while semsource defaults
  it on.
- One-line `Stop` hardening: cancel the component ctx before `entityCoalescer.Close()` so an in-flight
  flush aborts promptly instead of finishing KV ops on a live context.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-embedding` — one ADDED requirement (single-writer hop-1 seam + reconcile-at-execution +
  background repair; coalesced processing MUST NOT change outcomes vs immediate), and two MODIFIED
  requirements (newest-revision consistency gains the hop-1 create-lane scenario; failure
  classification/recovery extends to derived-write failures and recovery-by-repair).

## Impact

- **Code**: `processor/graph-embedding/component.go` only (mutex field, `reconcileEntity`,
  `markStranded`, `repairTargets`/`repairStranded`, repair ticker, three marking sites, `Stop`
  ordering). `graph/embedding` storage APIs unchanged (no signature changes; #638 IdentityText
  contract untouched; #614/#628 CAS lanes untouched — hop 2 does NOT take the seam).
- **Complexity ledger (the owner's constraint)**: net durable-state delta ZERO — no bucket, no key
  space, no `Record` field/status, no predicate/vocab, no config knob, no metric. Added: 1 mutex,
  1 goroutine, 1 interval const, 3 in-memory-only reason strings, 6 unexported methods
  (applyEntityTombstone, reconcileEntity, markStranded, repairTargets, repairStranded, repairLoop).
- **NOT BREAKING**: no exported API or record-shape change; changelog-worthy behavior changes are
  (1) failed derived writes report `degraded` (previously `ready`), (2) coalesced flush now runs on
  the watcher goroutine (quantified: cheaper than the shipped immediate mode when debounce dedup > 2).
- **sem\* consumers**: semsource is the beneficiary (its default-on coalesced lane is the racy one);
  no sister action required.

## Non-goals

- No revisioned delete-marker / CAS-create protocol (issue #629 options 1–2) — four durable-state
  questions (marker shape, GC, retention-guard interaction, rolling-upgrade) rejected in favor of the
  ordering fix.
- No shared repair-loop primitive extraction at N=2 (rule of three; the substance, `reconcileEntity`,
  is component-specific).
- No coalescer removal (semsource consumes it) and no coalescer value-carrying redesign (the fresh
  Get at execution IS the mechanism — graph-index independently confirms).
- No durable failed-delete markers (self-defeating: the marker write targets the bucket whose delete
  failed; restart recovery already exists).
- No extension of `normalizeFailureReason` / `SaveFailed` with the new reasons — they are in-memory
  only by design (a persisted-but-unreachable enum member is a phantom signal).
- No watermark semantics change (ADR-066 / #624 preserved), no new e2e tier.
