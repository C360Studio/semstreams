## Why

The Lifecycle harness (`pkg/lifecycle.Manager`, ADR-047) can birth an entity
(`Create`), move it through phases (`Transition`), and mark it terminal
(`Complete`/`Fail`) — but it has **no way to reclaim** a terminal entity, and no
way to move a *wave* of entities efficiently. semboids — the first downstream to
exercise `pkg/lifecycle` at scale (per-boid `flock.boid` Participants with
predator-cull spawn/despawn waves) — hit both gaps and had to reach around the
abstraction:

- To reclaim a culled boid it hand-rolls a raw `ENTITY_STATES` KV watch (because
  `Manager.Watch` silently drops delete events) **and** publishes
  `graph.mutation.entity.delete` itself — two app-side workarounds for a missing
  Manager primitive (gh#497).
- A spawn/cull wave of N boids is N sequential `Manager.Create`/`Transition`
  round-trips: measured **~150–340 create/s** versus the **~2,331 entity/s** the
  batched snapshot publisher sustains on the same graph-ingest write path — a
  7–15× gap attributable entirely to single-entity synchronous request/reply
  (gh#498).

A terminated entity that lingers forever in `ENTITY_STATES`, and a reclaim no
observer can see, are substrate gaps, not product concerns: any lifecycle
consumer with entity churn (missions, sensors, scenarios, boids) hits them.

## What Changes

- **Add a despawn primitive to `Manager`** — a first-class reclamation op that
  deletes a lifecycle entity from `ENTITY_STATES` via the existing
  `graph.mutation.entity.delete` handler, so callers never hand-roll the raw
  mutation. Covers the transition-to-terminal-then-delete sequence a cull needs.
- **Surface reclaims on `Manager.Watch`** — an observer must be able to learn an
  entity was reclaimed. Today `Watch` `continue`s past `KeyValueDelete`/
  `KeyValuePurge`; the change adds a delete-visible watch surface (event variant
  carrying op + entity ID) so a watcher sees culls without a parallel raw KV
  watch.
- **Add wave (batch) primitives** — `CreateBatch` / `TransitionBatch` (names TBD
  in design) that coalesce a spawn/cull wave so lifecycle churn is not strictly
  N×RTT bound. Real coalescing requires a graph-ingest batch-entity-write handler
  (graph-ingest owns `ENTITY_STATES`); this is the larger, second increment and
  is gated on that handler landing. Sequenced after despawn so gh#497 ships
  first.
- No breaking changes: all additions are new methods / new watch surface;
  existing `Create`/`Transition`/`Complete`/`Fail`/`Watch` behavior is unchanged.

## Capabilities

### New Capabilities
- `lifecycle`: the Lifecycle harness substrate (ADR-047) — `Manager` birth /
  transition / terminal / query / watch over KV-backed `Participant` entities in
  `ENTITY_STATES`. Seeded now (first change to touch it), distilled from code +
  ADR-047 and verified against source. The seed captures the reclamation
  (despawn), watch-delete-visibility, and wave (batch) requirements this change
  establishes — not a backfill of the entire Manager surface.

### Modified Capabilities
- None. `graph-ingest` gains a batch-entity-write handler in the second
  increment; that will be recorded as a `graph-ingest` spec delta **at the time
  that increment is scoped**, not here (this proposal's shippable increment is
  despawn + watch-visibility, which reuse the existing
  `graph.mutation.entity.delete` handler and add no graph-ingest surface).

## Impact

- **New API** on `pkg/lifecycle.Manager`: `Despawn` (+ possible `DespawnWith`),
  a delete-visible watch surface, and (increment 2) `CreateBatch`/
  `TransitionBatch`.
- **`pkg/lifecycle/graph_emit.go`**: the `graphEmitter` seam gains a `delete`
  method targeting the existing `graph.mutation.entity.delete` subject
  (`DeleteEntityRequest`/`Response` already defined; handler idempotent).
- **graph-ingest** (increment 2 only): a new batch-entity-write handler on the
  mutation API (write-path work adjacent to gh#480); does not touch the
  single-writer invariant (graph-ingest remains the sole `ENTITY_STATES` writer)
  or the ADR-055 envelope contract.
- **Consumers**: semboids (immediate — retires its raw-KV-watch + hand-rolled
  delete and its per-entity wave loops); any lifecycle consumer with churn
  (mission-planner, calibration-orchestrator, requirement-executor) benefits.
- **Interacts with ADR-068** (retention/deletion/GC): a Despawn is an explicit
  delete. This change provides the *harness-level* reclaim primitive; the
  ADR-068 derived-index cleanup (gh#433) is the graph-ingest-side integrity work
  under that delete. Design will state the boundary so despawn does not silently
  depend on unbuilt GC.

## Non-goals

- **Not** implementing the ADR-068 GC worker, tombstones, or derived-index
  cleanup on delete (gh#433) — despawn issues the existing idempotent
  `entity.delete`; index-leak remediation is separate, design-gated work.
- **Not** changing `Complete`/`Fail` semantics — terminal phase and reclamation
  stay distinct (an entity can be terminal without being despawned; despawn is
  the explicit reclaim).
- **Not** adding TTL/MaxBytes-based auto-expiry to the live graph (ADR-068
  guardrail) — reclamation is an explicit caller-driven op, never a bucket
  policy.
- **Not** a general graph-ingest batch API redesign — the batch-entity-write
  handler (increment 2) is the minimum needed for lifecycle wave coalescing.
