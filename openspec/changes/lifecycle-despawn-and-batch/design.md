## Context

`pkg/lifecycle.Manager` (ADR-047) is the substrate for KV-backed `Participant`
entities in `ENTITY_STATES`. It birth (`Create`), advances (`Transition`/
`TransitionWith`), and terminalizes (`Complete`/`Fail`) entities, and offers
`Watch(ctx, workflow) <-chan Participant` for observation. Writes go through the
`graphEmitter` seam (`graph_emit.go`), which today exposes only `create` and
`update` targeting `graph.mutation.entity.{create,update}_with_triples`.

Two gaps (semboids, first at-scale `pkg/lifecycle` consumer):

- **No reclamation.** Terminal entities persist in `ENTITY_STATES` forever.
  Reclaiming requires the caller to publish `graph.mutation.entity.delete`
  directly, and `Manager.Watch` `continue`s past `KeyValueDelete`/`KeyValuePurge`
  (`manager_query.go`) so no observer sees the reclaim without a parallel raw KV
  watch (gh#497).
- **No wave form.** `Create`/`Transition` are one-RTT-each; a wave of N is N
  synchronous graph-ingest round-trips — a measured 7–15× throughput gap versus
  batched publishing on the same path (gh#498).

Constraint: graph-ingest is the **sole writer** to `ENTITY_STATES`; the Manager
only emits mutation requests. The `graph.mutation.entity.delete` handler already
exists and is idempotent (`DeleteEntityRequest`/`Response`).

## Goals / Non-Goals

**Goals:**
- A harness-level despawn primitive so no consumer hand-rolls `entity.delete`.
- Delete-visible observation so a watcher sees reclaims without a raw KV watch.
- A wave (batch) path so lifecycle churn is not strictly N×RTT bound.
- Additive only — no change to existing `Create`/`Transition`/`Complete`/`Fail`/
  `Watch` behavior or signatures.

**Non-Goals:**
- ADR-068 GC worker / tombstones / derived-index cleanup on delete (gh#433) —
  despawn issues the existing idempotent delete; index-leak remediation is
  separate, design-gated work.
- Auto-expiry / TTL on the live graph (ADR-068 guardrail).
- Changing terminal-phase semantics — terminal and reclaimed stay distinct.

## Decisions

### D1. `Despawn` is a bare delete; `DespawnWith` is the transition-then-delete convenience

- `Manager.Despawn(ctx, workflow, entityID) error` — emits **only** the delete
  (via the new emitter `delete` method → `graph.mutation.entity.delete`). The
  caller is responsible for any terminal transition beforehand (`Complete`/`Fail`)
  if an audit trail is wanted. Idempotent: deleting an already-absent entity is a
  no-op success (mirrors `handleEntityDelete`).
- `Manager.DespawnWith(ctx, workflow, entityID, source, note) error` — the common
  cull: transitions the entity to its workflow's terminal phase (reusing the
  existing `Transition` path, so the phase write + audit `TransitionEvent` are
  produced), **then** deletes. Two graph-ingest ops, **not atomic** — documented.
  If the transition succeeds and the delete fails, the entity is terminal-but-
  present (a later `Despawn` reclaims it); if the process dies between, the same
  recovery applies. No new atomicity machinery.
- *Rationale:* keeps the minimal primitive (`Despawn`) free of a forced terminal
  write (a caller that already `Complete`d pays no extra RTT), while giving the
  cull path one call. Rejected "always transition-then-delete" (forces a terminal
  write + a phase arg on every reclaim) and "delete-only, no convenience" (every
  cull becomes two explicit calls — the exact boilerplate semboids reported).
- `workflow` is required (not just `entityID`) for symmetry with the rest of the
  Manager API and so the delete is scoped/validated against a registered
  workflow's `EntityIDPattern` before emitting.

### D2. Delete-visibility via a new `WatchEvents` surface, existing `Watch` untouched

- Add `Manager.WatchEvents(ctx, workflow) (<-chan LifecycleEvent, error)` where
  `LifecycleEvent{ Op LifecycleOp; EntityID string; Participant Participant }`
  and `Op ∈ {Upserted, Deleted}`. On `Upserted`, `Participant` is the projected
  state; on `Deleted`, `Participant` is nil and only `EntityID` is meaningful.
- Existing `Watch(ctx, workflow) <-chan Participant` is **unchanged** (still
  upsert-only) — no break for current callers.
- Internally `WatchEvents` shares the KV-watch/projection path; on a
  `KeyValueDelete`/`KeyValuePurge` whose key matches the workflow
  `EntityIDPattern`, it emits `{Deleted, key, nil}` instead of skipping. Refactor
  the projection/dispatch so `Watch` and `WatchEvents` don't duplicate it.
- *Rationale:* a `<-chan Participant` cannot represent a delete without a
  contradictory "deleted Participant" sentinel; a second delete-only channel
  complicates fan-out and cancellation. An event variant is the idiomatic
  KV-twofer shape and lets one loop carry both ops in order. Rejected the
  sentinel (overloads `Participant`) and the tuple-of-channels (harder lifecycle,
  two goroutines to cancel).

### D3. Batch (increment 2) is gated on a graph-ingest batch-entity-write handler

- `CreateBatch([]Participant)` / `TransitionBatch(...)` only *coalesce* if
  graph-ingest accepts a multi-entity write in one request. graph-ingest has
  `graph.mutation.triple.add_batch` but no batch **entity** create/update. So
  increment 2 adds a `graph.mutation.entity.write_batch` (name TBD) handler that
  applies N `create_with_triples`/`update_with_triples` ops under the same
  single-writer + ADR-055 envelope + CAS discipline, all-or-nothing per the
  existing `AddTriples` batch convention, and the emitter gains a `writeBatch`
  method.
- *Rationale / sequencing:* this is graph-ingest write-path work adjacent to
  gh#480, larger and independently reviewable. Despawn + WatchEvents (increment 1)
  reuse existing handlers and ship first; the batch spec delta against
  `graph-ingest` is authored when increment 2 is scoped, so gh#497 is not blocked
  on it. A `Manager.CreateBatch` that merely loops `create` would add API surface
  with none of the promised coalescing — explicitly not shipped as a stand-in.

## Risks / Trade-offs

- **[Despawn leaves derived-index rows behind (gh#433)]** → Out of scope by
  design (Non-Goals); despawn uses the same `entity.delete` semboids already
  calls, so it introduces no new leak — it *centralizes* the existing one.
  Documented on `Despawn` so consumers know reclaim ≠ index GC until gh#433/ADR-068
  land.
- **[`DespawnWith` non-atomic — crash between transition and delete]** →
  Idempotent recovery: the entity is at worst terminal-but-present, reclaimable by
  a later `Despawn`; no partial/corrupt state. Documented.
- **[`WatchEvents` delete events for entities the observer never saw upserted]** →
  Consumers must treat `Deleted{EntityID}` as "ensure absent", not "remove a
  known row" (deletes can arrive for entities filtered out or seen before the
  watch started). Documented on the event type.
- **[Batch all-or-nothing rejects a whole wave on one bad entity]** → Mirrors the
  existing `AddTriples` batch contract; increment-2 design will decide
  partial-success reporting (per-entity error slice) vs strict all-or-nothing.

## Migration Plan

- Purely additive; no migration. Increment 1 (`Despawn`/`DespawnWith`/
  `WatchEvents`) ships independently. semboids then retires its raw KV watch +
  hand-rolled delete; its wave loops migrate when increment 2 lands.
- Rollback: new methods are unused by existing code; reverting is a straight
  removal.

## Open Questions

- Increment-2 batch handler: partial-success (per-entity error slice) vs strict
  all-or-nothing? Deferred to increment-2 scoping.
- Should `WatchEvents` emit an initial `Upserted` for existing entities on
  subscribe (like `Watch`'s bootstrap), or only live events? Lean: mirror
  `Watch`'s bootstrap for `Upserted`, deletes live-only. Confirm in specs.
