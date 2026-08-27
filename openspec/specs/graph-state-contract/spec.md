# graph-state-contract Specification

## Purpose

Defines the ENTITY_STATES **canonical-decode contract** — where a stored value is validated, and
how a component responds when one violates it ("poison").

**Validation rides existing read points.** No component holds an ENTITY_STATES watcher whose sole
purpose is re-validating writes it does not otherwise consume for work. Detection belongs to the
reads that already happen: the sole writer's boot snapshot sweep, per-request validating reads, and
a consumer's own input or replay path. A dedicated contract-guard watcher is self-observation of an
already-gated path, taxed per write on every deployment — gh#562 measured the bill at a −9.1%
ingest-ceiling regression from three such watchers on one connection.

**Poison response is scoped by reader class, not globally.** The authoritative read surface serves
ENTITY_STATES values per request, and every response is enforced by the canonical decode of the
bytes it returns — so correctness never depended on a global latch. It refuses exactly the poisoned
entities, keeps serving every unaffected one, and recovers without a process restart. A derived-view
component serves a projection it cannot re-validate per request, so it latches the whole view sticky
on observed poison and recovers only by operator reset and restart. The typed
`graph_state_reset_required` code and its bounded reasons are identical across both classes: the
scope of the refusal differs, the vocabulary does not.

Poison is a repairable condition rather than a terminal one, so typed errors name the poisoned
entity whenever identity is known, and an aggregate read fails as a whole naming **every** poisoned
entity encountered in that attempt — never silent omission, never one-per-round-trip discovery.

This capability does NOT cover the marshal-site write gate that keeps poison out of ENTITY_STATES
in the first place (`graph-ingest`), predicate vocabulary legality
(`predicate-contract-enforcement`), or the beta-cutover reader-class reconciliation still owed by
that change (gh#772).
## Requirements
### Requirement: Poison response scope is defined per reader class

Every ENTITY_STATES reader MUST apply the poison response of its class. The **authoritative
read surface** — graph-ingest's query lanes, which serve ENTITY_STATES values directly
per-request — MUST scope its poison response to exactly the poisoned entities: every unaffected
entity keeps serving, each request enforced by the canonical decode of the bytes it returns,
and repair recovers the surface without a process restart. A **derived-view component** — a component
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

#### Scenario: derived-view components keep the sticky whole-view response

- **GIVEN** a derived-view component observes the same typed graph-state poison in its watched or
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

### Requirement: ENTITY_STATES has one canonical codec and typed poison contract

Every in-process ENTITY_STATES writer MUST use the canonical complete-candidate encoder and every component that
interprets ENTITY_STATES MUST use the canonical decoder. The decoder MUST distinguish unreadable entity JSON from
a noncanonical predicate with bounded reasons and the shared `graph_state_reset_required` code. Ordinary JSON in
other KV buckets is outside this contract.

#### Scenario: unreadable authoritative state is typed poison

- **GIVEN** an ENTITY_STATES value cannot decode as EntityState
- **WHEN** any authoritative reader interprets it
- **THEN** the reader returns `graph_state_reset_required`
- **AND** the bounded reason is `unreadable_entity_state`

#### Scenario: noncanonical authoritative state is typed poison

- **GIVEN** an ENTITY_STATES value contains a predicate outside the canonical grammar
- **WHEN** any authoritative reader interprets it
- **THEN** the reader returns `graph_state_reset_required`
- **AND** the bounded reason is `noncanonical_predicate`

### Requirement: Watch transport, tombstones, and state poison are distinct

Every ENTITY_STATES watch consumer MUST classify CREATE/PUT values, DEL/PURGE tombstones, and watch transport
failures before decoding. CREATE and PUT values MUST use the canonical decoder. DEL and PURGE MUST be treated as
equivalent valid entity-removal tombstones, MUST drive the consumer's cleanup path, and MUST count as terminal
replay work without attempting to decode their empty payload. A watch transport failure MUST follow the ordinary
retry, degraded-health, or not-ready path and MUST NOT latch `graph_state_reset_required`.

#### Scenario: delete and purge are valid tombstones

- **GIVEN** an ENTITY_STATES watch delivers DEL or PURGE for an entity key
- **WHEN** an authoritative consumer handles the entry
- **THEN** it performs the same entity-removal cleanup for either operation
- **AND** it does not classify the empty payload as unreadable entity state
- **AND** replay progress may advance after that tombstone's cleanup completes

#### Scenario: transport loss is not stored-state poison

- **GIVEN** an ENTITY_STATES watch closes or returns a transport error
- **WHEN** a consumer has not observed malformed authoritative state
- **THEN** the consumer remains degraded or not-ready according to its ordinary recovery contract
- **AND** it does not latch `graph_state_reset_required`
- **AND** transport recovery does not require an operator graph reset

### Requirement: Projection poison is sticky until operator reset and restart

A projection owner that observes typed graph-state poison MUST enter sticky reset-required state, MUST NOT treat the
poisoned revision as successfully terminal, and MUST NOT serve its derived view. Repair or a later valid update MUST
NOT clear the process-lifetime poison. Action/evaluation consumers MUST emit no derived output from poisoned state.

#### Scenario: a later valid event cannot hide poison

- **GIVEN** a projection owner has observed incompatible ENTITY_STATES
- **WHEN** a later valid entity event arrives
- **THEN** readiness remains reset-required
- **AND** no query returns a partial derived view

#### Scenario: clean reset starts a new readiness lifetime

- **GIVEN** the operator exported if needed and deleted incompatible graph/index buckets
- **WHEN** the process restarts and canonical sources are reingested
- **THEN** the new process has no inherited poison flag
- **AND** ordinary replay watermarks decide readiness

### Requirement: Generic KV actions cannot bypass graph ownership

The graph package MUST publish the complete set of framework-owned authoritative and derived graph buckets.
Generic rule `update_kv` actions MUST reject those buckets both during literal configuration validation and after
runtime variable substitution. Domain application KV buckets remain valid `update_kv` targets.

#### Scenario: substituted graph bucket is rejected

- **GIVEN** an update_kv action resolves its bucket from message data
- **WHEN** the result is a framework-owned graph bucket
- **THEN** the action fails before any KV write
- **AND** the caller is directed to the graph mutation API

### Requirement: The canonical codec and the boot sweep never consult the payload registry

`message_type` on a stored entity MUST be recorded from a registered key at write time and MUST be interpreted as provenance
only: the canonical decoder, `ValidateEntityStateContract`, the boot snapshot sweep, the Graphable merge path, and every
authoritative reader MUST NOT consult the payload registry, so an entity persisted under a key that is later unregistered stays readable, is never
inventoried as poison, and remains mutable through must-exist operations.

#### Scenario: a resident entity with an unregistered stamp is not poison

- **GIVEN** an `ENTITY_STATES` value whose `message_type` is registered in no binary
- **WHEN** graph-ingest boots and sweeps
- **THEN** the entity has no poison inventory entry
- **AND** an exact read returns it with the stamp unchanged
- **AND** the test that verifies this is `TestResidentUnregisteredStampIsNotPoison`

#### Scenario: must-exist mutations ignore the stamp

- **GIVEN** the same entity
- **WHEN** a `triple.append` targets it
- **THEN** the append is evaluated on the entity's current revision without consulting the registry
- **AND** the test that verifies this is `TestResidentUnregisteredStampIsNotPoison`

