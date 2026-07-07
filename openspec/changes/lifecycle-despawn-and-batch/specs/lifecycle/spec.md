## ADDED Requirements

### Requirement: Lifecycle entity reclamation

The Manager SHALL provide `Despawn(ctx, workflow, entityID)` that reclaims a
lifecycle entity by deleting it from `ENTITY_STATES` through the graph-ingest
`graph.mutation.entity.delete` mutation, so no consumer hand-rolls the raw
delete. `Despawn` reclaims only; it does not itself transition the entity to a
terminal phase. The operation is idempotent — reclaiming an already-absent entity
succeeds. The `workflow` argument SHALL be a registered workflow whose
`EntityIDPattern` matches `entityID`; a mismatch is an error and emits no delete.

#### Scenario: Reclaim a terminal entity

- **GIVEN** an entity that a caller has moved to a terminal phase via `Complete`/`Fail`
- **WHEN** the caller invokes `Despawn(ctx, workflow, entityID)`
- **THEN** the Manager emits `graph.mutation.entity.delete` for `entityID`
- **AND** the entity is removed from `ENTITY_STATES`

#### Scenario: Despawn is idempotent

- **GIVEN** an entity that is already absent from `ENTITY_STATES`
- **WHEN** the caller invokes `Despawn(ctx, workflow, entityID)`
- **THEN** the call returns success without error

#### Scenario: Despawn rejects an unregistered or non-matching workflow

- **WHEN** `Despawn` is called with a `workflow` that is not registered, or whose `EntityIDPattern` does not match `entityID`
- **THEN** the Manager returns an error
- **AND** emits no delete mutation

### Requirement: Transition-then-reclaim convenience

The Manager SHALL provide `DespawnWith(ctx, workflow, entityID, source, note)`
that transitions the entity to its workflow's terminal phase and then reclaims
it, for the common cull path. The two graph-ingest operations are NOT atomic;
the behavior on partial failure SHALL be documented and recoverable: if the
terminal transition succeeds but the delete fails (or the process dies between
them), the entity is left terminal-but-present and a subsequent `Despawn`
reclaims it — no partial or corrupt state results.

#### Scenario: Cull in one call

- **WHEN** the caller invokes `DespawnWith(ctx, workflow, entityID, source, note)` on a non-terminal entity
- **THEN** the Manager transitions the entity to its terminal phase (producing the phase write and audit `TransitionEvent`)
- **AND** then emits `graph.mutation.entity.delete` for `entityID`

#### Scenario: Partial failure is recoverable

- **GIVEN** the terminal transition has committed but the delete failed
- **WHEN** the caller retries with `Despawn(ctx, workflow, entityID)`
- **THEN** the entity is reclaimed
- **AND** no partial or corrupt state remains

### Requirement: Delete-visible lifecycle observation

The Manager SHALL provide `WatchEvents(ctx, workflow)` returning a channel of
lifecycle events, each carrying an operation (`Upserted` or `Deleted`), the
`EntityID`, and — for `Upserted` — the projected `Participant`. A reclaim
(`KeyValueDelete`/`KeyValuePurge`) whose key matches the workflow
`EntityIDPattern` SHALL be delivered as a `Deleted` event, so an observer learns
of reclaims without running a parallel raw KV watch. The existing
`Watch(ctx, workflow) <-chan Participant` SHALL remain unchanged (upsert-only),
so current callers are not affected.

#### Scenario: Observer sees a reclaim

- **GIVEN** an observer subscribed via `WatchEvents(ctx, workflow)`
- **WHEN** an entity matching the workflow pattern is deleted from `ENTITY_STATES`
- **THEN** the observer receives an event with `Op == Deleted` and the deleted `EntityID`

#### Scenario: Observer sees an upsert with projected state

- **GIVEN** an observer subscribed via `WatchEvents(ctx, workflow)`
- **WHEN** an entity matching the workflow pattern is created or its phase changes
- **THEN** the observer receives an event with `Op == Upserted`, the `EntityID`, and the projected `Participant`

#### Scenario: Existing Watch is unaffected

- **GIVEN** a caller using the existing `Watch(ctx, workflow) <-chan Participant`
- **WHEN** an entity is deleted
- **THEN** the delete is not delivered on that channel (upsert-only behavior preserved)
