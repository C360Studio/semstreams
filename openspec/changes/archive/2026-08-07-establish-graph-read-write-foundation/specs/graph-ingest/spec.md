# graph-ingest — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: The create-time indexing profile is not overridden by a re-arrival

A genuine entity birth establishes its indexing profile. A later Graphable merge MUST preserve that profile. There is
no profile-less referential stub exception because missing relationship targets do not create entities.

#### Scenario: A re-arrival cannot change the create-time profile

- **GIVEN** an entity created with indexing profile `content`
- **WHEN** a later Graphable arrival declares profile `trace`
- **THEN** the stored profile remains `content`

### Requirement: The structural gate MUST be unconditionally fail-closed with no bypass configuration

Every Graphable ingest and each of the four mutation operations MUST validate structural entity IDs and predicates
before KV I/O. The authoritative persistence seam MUST independently validate each complete final candidate. No
configuration may weaken either layer, and one rejected request MUST be metered exactly once.

#### Scenario: No mutation operation bypasses the gate

- **WHEN** create, reconcile, append, or delete carries structurally invalid input
- **THEN** it returns the classified structural error before persistence
- **AND** rejection is metered exactly once

### Requirement: Deduplication MUST be atomic against concurrent identical requests

Append deduplication MUST run inside the same revision-checked CAS evaluation as the write. A loser MUST re-evaluate
against the winner's committed state. Canonical tuple equality uses subject, predicate, persisted object encoding,
datatype, source, and context; duplicates within one request collapse in first-input order.

#### Scenario: Two concurrent identical appends store one tuple

- **GIVEN** an existing entity lacks a tuple
- **WHEN** two identical appends race
- **THEN** exactly one copy is stored
- **AND** both per-subject results fully account for their input

### Requirement: A no-op mutation MUST report that it committed nothing

An append containing only existing canonical tuples or a reconcile already equal to desired selected state MUST return
`unchanged`. It MUST NOT advance the KV revision, entity version, or update timestamp and MUST NOT be described as the
caller's commit.

#### Scenario: A duplicate-only append is unchanged

- **GIVEN** an entity already contains every submitted tuple
- **WHEN** append is evaluated
- **THEN** the response reports unchanged with the live revision as observation
- **AND** no write occurs

### Requirement: Suppressed duplicates MUST be observable

Deduplicated append tuples MUST contribute to the bounded mutation-outcome counter under operation `append` and outcome
`unchanged`. Suppression MUST NOT create an entity-ID metric label or one log per tuple.

#### Scenario: Duplicate traffic is visible without unbounded labels

- **WHEN** append suppresses existing tuples
- **THEN** the bounded append/unchanged counter rises

### Requirement: Stored duplicate triples MUST remain readable and MUST NOT be removed

Entities containing historical duplicate tuples MUST remain readable. No startup sweep or migration removes them, and
stored-state validation MUST NOT newly poison such entities. A new append of the same tuple remains unchanged.

#### Scenario: A pre-existing duplicate remains readable

- **GIVEN** an entity stores two historical copies of one tuple
- **WHEN** the entity is read
- **THEN** it serves normally with both stored copies

### Requirement: Poison refusal is scoped to the poisoned entity

A poisoned entity MUST refuse exact reads, Graphable resident-state merges, and create, reconcile, append, or delete
operations that encounter its invalid bytes with `graph_state_reset_required`. Other entities continue normally, and
detection invalidates that entity's query-cache entry. Poison state is surfaced; it does not trigger global startup
failure or automatic recovery.

#### Scenario: Mutation operations preserve poison classification

- **GIVEN** entity A's resident state is poisoned
- **WHEN** reconcile, append, or delete targets A
- **THEN** the reply carries `graph_state_reset_required`
- **AND** no reply invites an automatic retry

### Requirement: Every ENTITY_STATES commit validates the complete final candidate

Graph-ingest MUST apply the canonical state contract at one authoritative persistence seam used by Graphable Create/CAS,
RPC create/reconcile/append, and hierarchy Create/CAS writes. Validation MUST inspect the complete candidate after
normalization and framework-triple injection and before persistence. Handler validation MAY reject earlier but is not
the correctness boundary.

#### Scenario: A direct mutation cannot bypass validation

- **WHEN** an internal caller submits an invalid candidate through any admitted operation
- **THEN** the final persistence seam returns the same typed rejection as the NATS request handler

## ADDED Requirements

### Requirement: The mutation API is a declared typed component port

Graph-ingest MUST declare one required input `nats-request` port with interface
`semstreams.graph.mutation` version `v1` and family `graph.mutation.>`. Handler setup MUST resolve the four admitted
operation leaves from that declaration and MUST NOT subscribe through hidden constants or fallback subjects. A validated
flow MUST contain exactly one compatible provider input and MAY contain many compatible requester outputs. This is
static
composition validation, not runtime process discovery, election, or fencing.

#### Scenario: An undeclared mutation side channel cannot boot

- **GIVEN** graph-ingest has no compatible mutation provider input port
- **WHEN** the flow is validated
- **THEN** validation fails before mutation subscriptions are installed
- **AND** graph-ingest does not fall back to hardcoded subjects

### Requirement: Authority writes share one atomic Create and CAS discipline

A genuine `ENTITY_STATES` birth MUST use atomic KV `Create`. Every write to an existing key—Graphable merge, RPC
mutation, or hierarchy inverse—MUST commit by CAS against state read at a specific revision. No production write path
MAY retain unconditional `Put`-as-upsert semantics. The keyed ingest pool MAY reduce local contention but MUST NOT be a
correctness precondition or coordinate RPC handlers.

#### Scenario: RPC reconcile survives a racing Graphable merge

- **GIVEN** ingest and reconcile both read entity A at revision R
- **WHEN** reconcile commits R to R+1 before ingest attempts its write
- **THEN** ingest cannot overwrite R+1 from its stale candidate
- **AND** it re-evaluates its retry-safe merge against R+1 or returns a classified failure

### Requirement: Four explicit mutation operations replace the eight-subject surface

The admitted operations MUST be `entity.create`, `entity.reconcile`, `triple.append`, and `entity.delete`. Create MUST
atomically birth an absent entity and return typed `entity_already_exists` otherwise. Reconcile MUST require a nonzero
expected revision and replace the complete desired set for named predicates. Append MUST deduplicate canonical exact
tuples and report one result per subject, with no cross-subject transaction claim. Delete MUST require a nonzero
expected
revision and conditionally delete exactly that entity. Every absent non-create target MUST return typed
`entity_not_found`.

#### Scenario: A stale reconcile does not silently retry into authority

- **GIVEN** a reconcile request names revision R and the entity is now at R+1
- **WHEN** graph-ingest evaluates the request
- **THEN** it returns typed `revision_mismatch`
- **AND** it does not switch to an unconditional retry or overwrite

#### Scenario: Conditional delete reports only evidence NATS supplies

- **GIVEN** entity A exists at the request's expected revision
- **WHEN** the conditional KV delete is acknowledged
- **THEN** the response reports `applied`, A's ID, and the matched expected revision
- **AND** it does not claim a delete-marker revision that NATS KV did not return

#### Scenario: Append deduplicates without rewriting unchanged state

- **GIVEN** an existing entity already stores every submitted canonical tuple
- **WHEN** append evaluates that subject
- **THEN** the per-subject result reports unchanged and the observed live revision
- **AND** no KV revision, entity version, or update timestamp advances

### Requirement: Exact authority read returns value and same-entry revision

The exact entity read MUST return one validated entity and the nonzero KV revision from the same KV entry. Absence MUST
return typed `entity_not_found`; poison MUST retain the graph-state classification. `EntityState.Version` MUST NOT be
accepted as a KV revision. The read MUST NOT mutate, repair, create a stub, or change `GRAPH_STATUS`.

#### Scenario: A reconciler receives usable CAS evidence

- **GIVEN** entity A is resident and valid
- **WHEN** the exact read succeeds
- **THEN** its entity and revision come from one KV entry
- **AND** the returned revision is nonzero

### Requirement: Relationship target absence creates no entity

Mutation MUST validate relationship syntax without requiring the object entity to exist. Graph-ingest MUST NOT create a
relationship-target stub, pending record, inverse repair, or delayed drain because an object is absent. A later real
birth makes future dereference resolve; no source-edge replay is required.

#### Scenario: A relationship may precede its object

- **GIVEN** entity A contains a valid relationship to absent entity B
- **WHEN** A commits
- **THEN** A's edge remains current authority
- **AND** no `ENTITY_STATES` key for B is created

### Requirement: Hierarchy inference is Graphable-lane-only and uses Create/CAS

Opt-in hierarchy inference MUST run only for Graphable ingest. RPC create MUST produce no hierarchy side effects.
Hierarchy containers are real inferred entities, not referential stubs; their birth MUST use atomic `Create`, and
container/sibling inverse edges MUST update must-exist targets through CAS. A failed companion write MAY leave a
dangling
relationship, which remains valid eventual graph state and MUST NOT trigger rollback or repair machinery.

#### Scenario: RPC create does not manufacture hierarchy

- **GIVEN** hierarchy is enabled
- **WHEN** a caller creates an entity through the RPC mutation API
- **THEN** only the caller-supplied entity birth commits
- **AND** no container or inverse hierarchy write is attempted

### Requirement: Mutation outcomes are bounded and honest about lost replies

Server replies MUST classify applied, unchanged, not-found, exists, revision-mismatch, and invalid outcomes. A typed
client MUST distinguish unavailable, deadline, and `commit_unknown`; a timeout or lost reply after possible delivery
MUST NOT be called not-applied. `graph_mutation_outcomes_total{operation,outcome}` is the bounded command counter.
Revision-mismatch logs MAY name entity ID, operation, and expected revision, but entity ID MUST NOT be a metric label.

#### Scenario: An ambiguous reply is not automatically retried

- **GIVEN** a request may have reached graph-ingest but its reply is lost
- **WHEN** the typed client deadline expires
- **THEN** it returns `commit_unknown`
- **AND** it does not automatically retry or claim exactly-once behavior

## REMOVED Requirements

### Requirement: A fully-duplicate add request MUST advance no ENTITY_STATES revision

**Reason**: the `triple.add` operation is retired. Exact-tuple no-op behavior is carried by `AppendTriples`.

**Migration**: callers use `graph.mutation.triple.append` and read the per-subject deduplicated result.

### Requirement: Add-lane responses MUST count only newly appended tuples

**Reason**: the old add-lane response shape is retired in favor of explicit per-subject append outcomes.

**Migration**: callers consume each subject's applied/deduplicated/failed result.

### Requirement: The add lane MUST NOT append a triple already stored with an identical tuple

**Reason**: the old add lane and its multiple entry points are removed.

**Migration**: atomic canonical-tuple deduplication remains under append and the modified deduplication requirement.

### Requirement: Duplicate suppression MUST NOT suppress redelivery side effects

**Reason**: this requirement preserves retired relationship-target creation and foreign-edge routing side effects.

**Migration**: append has no hidden post-commit entity creation or routing side effects.
