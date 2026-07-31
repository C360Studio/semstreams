# graph-state-contract Specification

## Purpose
TBD - created by archiving change poison-response-scoping. Update Purpose after archive.
## Requirements
### Requirement: Poison response scope is defined per reader class

Every ENTITY_STATES reader MUST apply the poison response of its class. The **authoritative
read surface** — graph-ingest's query lanes, which serve ENTITY_STATES values directly
per-request — MUST scope its poison response to exactly the poisoned entities: every unaffected
entity keeps serving, each request enforced by the canonical decode of the bytes it returns,
and repair recovers the surface without a process restart. A **projection owner** — a component
serving a derived view built from watched or replayed entity state, including a
watch-maintained derived-view reader whose response cache depends on its own watcher — MUST
enter sticky whole-view reset-required state on observed poison, MUST NOT serve its derived
view, and recovers only by operator reset and process restart. The typed
`graph_state_reset_required` code and its bounded reasons are identical across classes.

#### Scenario: unaffected authoritative reads serve during a poison incident

- **GIVEN** one ENTITY_STATES value fails the canonical decode
- **WHEN** the authoritative read surface serves a different, valid entity
- **THEN** the read serves that entity's state
- **AND** a read of the poisoned entity returns `graph_state_reset_required` with its bounded
  reason

#### Scenario: projection owners keep the sticky whole-view response

- **GIVEN** a projection owner observes the same typed graph-state poison in its watched or
  replayed input
- **WHEN** it evaluates readiness
- **THEN** it enters sticky reset-required state and serves no derived view until operator
  reset and restart

### Requirement: Contract validation rides existing read points, never dedicated guard watchers

A component MUST NOT hold an ENTITY_STATES watcher whose sole purpose is contract
re-validation of writes it does not otherwise consume for work. Detection MUST ride the read
points that already exist: the sole writer's boot snapshot sweep, per-request validating reads,
a consumer's own input watcher or replay path. A component that consumes no entity state holds
no ENTITY_STATES watcher at all.

#### Scenario: a steady-state write fans out only to working consumers

- **GIVEN** the framework components are running at steady state
- **WHEN** an entity write commits to ENTITY_STATES
- **THEN** the write is delivered only to watchers that consume entity state for their own work
- **AND** no dedicated contract-guard watcher receives it

#### Scenario: a rule processor with no entity patterns pays no entity fan-out

- **GIVEN** a rule processor configured with zero entity-watch patterns
- **WHEN** entity writes commit at any rate
- **THEN** the rule processor receives none of them
- **AND** its evaluation kill switch still latches if a value it actually consumes is poisoned

### Requirement: Typed poison identifies the poisoned entity when known

A typed graph-state poison classification MUST carry the poisoned entity's ID whenever the
classification site knows it (snapshot sweep entry key, queried entity ID, RMW target, mutation
read seam), stamped at the closure or goroutine where the identity is in scope, so operator
surfaces, aggregate-read failures, and the poison inventory can name the entity without
re-deriving it. The field is additive; only the error class and code cross the wire, so
existing consumers are unaffected.

#### Scenario: classification sites stamp the entity ID

- **GIVEN** a query read of entity A fails the canonical decode
- **WHEN** the typed poison classification is produced
- **THEN** it carries entity A's ID alongside the bounded reason

#### Scenario: aggregate failures name entities through the stamp

- **GIVEN** a batch read encounters poisoned entities A and C
- **WHEN** the aggregate failure is produced
- **THEN** the stamped IDs A and C are both present in the typed error

