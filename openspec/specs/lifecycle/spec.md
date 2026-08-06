# lifecycle Specification

## Purpose

Define the reusable named-instance lifecycle surface over canonical graph exact reads and mutations. Lifecycle records
domain phases and transitions; it does not arbitrate graph writers or create missing entities for transitions.

## Requirements

### Requirement: Workflow registration validates projectability and local contract shape

Workflow registration MUST reject a schema type that cannot implement `Participant`, an invalid entity-ID pattern,
invalid lifecycle predicates, or an incompatible local projection contract. Registration records workflow capability;
it MUST NOT claim semantic predicates or reject cross-component overlap. Runtime conflicts are observed through CAS.

#### Scenario: Two workflows have overlapping predicates

- **GIVEN** two workflows project an overlapping lifecycle predicate
- **WHEN** each valid workflow registers
- **THEN** registration may succeed
- **AND** no claim, lease, token, heartbeat, or global overlap record is created

### Requirement: Lifecycle creation is strict and discoverable

Creation MUST validate that the entity ID matches the workflow pattern before mutation. When the authority entity is
absent, lifecycle MUST use strict canonical `entity.create`. When an exact authority read finds an existing entity with
no lifecycle phase for this workflow, lifecycle MUST attach the lifecycle dimension with a revision-fenced canonical
`entity.reconcile`, preserving unrelated predicates. An existing entity that already carries the workflow's lifecycle
phase MUST return the classified conflict. Success MUST be causally downstream of the committed entity mutation. The
operator lifecycle surface returns that committed instance in `CreateResult`; it does not expose a KV revision.
Revision exposure remains the responsibility of the canonical mutation client's `Create` result.

#### Scenario: Out-of-pattern birth is refused

- **GIVEN** an entity ID outside the workflow pattern
- **WHEN** creation is attempted
- **THEN** creation fails before graph mutation
- **AND** no direct-get-only orphan is created

#### Scenario: Existing authority entity receives the lifecycle dimension

- **GIVEN** the authority entity exists without the workflow's lifecycle phase
- **WHEN** lifecycle creation is attempted
- **THEN** lifecycle exact-reads the entity and revision-fenced reconciles the initial lifecycle predicates
- **AND** unrelated predicates on the authority entity are preserved

#### Scenario: Existing lifecycle instance remains a conflict

- **GIVEN** the authority entity already carries the workflow's lifecycle phase
- **WHEN** creation is attempted again
- **THEN** the caller receives the classified exists outcome
- **AND** no automatic retry or matching-state success conversion occurs

### Requirement: Operator birth attribution is explicit

A lifecycle birth initiated through an operator surface MUST record the operator as the transition source. Framework
code MAY record the framework as source only for framework-initiated birth. Provenance MUST be supplied by the caller
that knows it.

#### Scenario: Operator creates an instance

- **WHEN** the instance history is read after operator creation
- **THEN** the first event attributes the operator

### Requirement: Must-exist lifecycle operations never create state

Every transition or reconcile against an absent entity MUST return typed `entity_not_found`. It MUST NOT create state,
a placeholder target, or a pending operation. The component decides whether later retry is meaningful.

#### Scenario: Transition races birth

- **GIVEN** a lifecycle transition arrives before entity birth
- **WHEN** graph-ingest evaluates it
- **THEN** it returns typed not-found
- **AND** no placeholder entity is created

### Requirement: Lifecycle transitions use exact revision evidence

A transition MUST exact-read the entity and reconcile the complete lifecycle predicate group using the same-entry
nonzero KV revision. A definite revision mismatch MAY be handled only by a bounded component policy that re-reads and
recomputes desired state. `commit_unknown` MUST NOT be automatically retried.

The same reconcile MUST replace the current phase while retaining a fixed window of the 64 most recent transition
occurrences in the current entity. Each occurrence MUST use the framework `lifecycle.transition.*` predicate family
with one nonempty transition ID in `Triple.Context`. `History` MUST exact-read and strictly decode that current window;
it MUST NOT replay KV revisions, create another store, return a partial result from malformed records, expose a history
size knob, or imply that the bounded operator window is an unbounded audit log.

#### Scenario: Ambiguous transition remains visible

- **GIVEN** a transition request may have committed and its reply is lost
- **WHEN** lifecycle receives `commit_unknown`
- **THEN** it returns the ambiguity to its caller
- **AND** it does not infer authorship from a later matching read

#### Scenario: Transition history survives History one

- **GIVEN** `ENTITY_STATES` retains one KV revision
- **WHEN** an entity is created, transitions, and the stack restarts
- **THEN** `History` returns the retained transition occurrences from the current entity
- **AND** current phase uses replace semantics while occurrence records retain distinct contexts

### Requirement: Lifecycle entity reclamation is conditional

Lifecycle delete MUST exact-read the entity, submit its nonzero KV revision to `entity.delete`, and propagate typed
not-found, revision-mismatch, poison, unavailable, and `commit_unknown` outcomes. Delete has no relationship cascade,
target cleanup, or automatic retry.

#### Scenario: A newer transition cannot be deleted by stale reclaim

- **GIVEN** lifecycle read entity A at revision R and A advanced to R+1
- **WHEN** reclamation submits expected revision R
- **THEN** delete returns typed revision mismatch
- **AND** A at R+1 remains authoritative

### Requirement: Lifecycle relationship references are source-derived

`Manager.References` MUST return the target entity ID and predicate recorded on each matching source triple. It MUST
NOT read or hydrate the target, report a target workflow or phase, fabricate a placeholder, or imply target existence.
An unresolved object ID remains a valid relationship fact under eventual consistency.

#### Scenario: Reference target is absent

- **GIVEN** lifecycle-managed source A contains a declared relationship to absent B
- **WHEN** `Manager.References` reads A
- **THEN** it returns B's ID and the source predicate
- **AND** it performs no target birth or target-state read

### Requirement: Transition-then-reclaim preserves causal revision

Transition-then-reclaim MUST preserve the transition's exact committing revision and use it for conditional delete. A
lost mutation reply MUST return `commit_unknown` and stop before deletion.

#### Scenario: Ambiguous transition stops reclamation

- **GIVEN** the transition reply is lost after possible delivery
- **WHEN** transition-then-reclaim handles the outcome
- **THEN** it returns `commit_unknown`
- **AND** it does not issue delete against a predicted revision

### Requirement: Delete-visible observation is explicit

The lifecycle observation surface MUST offer an explicit delete-visible watch when callers need tombstones. The
existing upsert-only watch MUST retain its behavior so callers do not silently begin receiving deletion events.

#### Scenario: Delete-visible watcher observes removal

- **GIVEN** a lifecycle entity is deleted
- **WHEN** a caller consumes the delete-visible watch
- **THEN** it receives one removal observation for that entity

#### Scenario: Existing watch remains upsert-only

- **GIVEN** a caller consumes the existing upsert-only watch
- **WHEN** an entity is deleted
- **THEN** no tombstone is delivered on that channel

### Requirement: Operator surfaces preserve every reachable condition

The operator gateway MUST preserve typed create, reconcile, delete, exact-read, and `commit_unknown` outcomes. It MUST
map invalid input to a client error, revision or exists conflicts to conflict responses, missing state to not-found,
and unavailable or internal failure without collapsing them into success.

#### Scenario: Operator sees revision conflict

- **GIVEN** an operator acts from a stale exact read
- **WHEN** lifecycle returns revision mismatch
- **THEN** the gateway exposes the conflict without converting it to success
