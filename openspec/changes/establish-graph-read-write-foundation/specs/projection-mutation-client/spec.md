# projection-mutation-client — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Contract-bound mutation client

`pkg/projection` MUST expose narrow create, reconcile, append, delete, and exact-read capabilities over a copied local
`projection.Contract`. A contract validates entity patterns, message type, birth predicates, named predicate groups,
indexing profile, and operation shape. It grants no global write permission and MUST NOT register an owner, heartbeat,
presence lease, incarnation token, foreign-edge mode, or overlap claim. Overlapping contracts in different components
are valid.

#### Scenario: Two components may describe the same predicate group

- **GIVEN** two valid local contracts overlap on an entity pattern and predicate
- **WHEN** both clients are constructed
- **THEN** construction succeeds without global registration
- **AND** runtime conflicts are observed through revision outcomes

### Requirement: Narrow public capabilities

Public interfaces MUST expose only the operations each consumer needs. The client MUST use the declared typed
`nats-request` port and one operation-specific exact reader; it MUST NOT expose raw subjects, a general graph client,
ownership infrastructure, or raw KV.

#### Scenario: A birth-only consumer receives no reconcile capability

- **GIVEN** a consumer only creates entities
- **WHEN** its dependency is constructed
- **THEN** it receives the narrow creator interface
- **AND** no registry, token, heartbeat, or raw transport handle is returned

### Requirement: Create-only birth predicates are not graph-enforced immutable facts

`Contract` MUST expose optional `BirthPredicates`. Every birth predicate MUST be a registered canonical exact
predicate. Duplicates and overlap with a reconcile or append group in the same contract MUST be rejected. Birth
predicates grant create-time validation only; they create no owner, claim, foreign-edge permission, or write-once graph
rule. A contract containing only birth predicates MUST be valid.

#### Scenario: Valid birth-only contract

- **WHEN** a contract declares valid birth predicates and no mutable groups
- **THEN** local validation succeeds
- **AND** no registration or transport side effect occurs

### Requirement: Contract-validated atomic creation

Create MUST validate the complete birth against local contract predicates and call strict `CreateEntity`. Existing
authority returns typed `entity_already_exists`; no stub exception or hierarchy side effect applies.

#### Scenario: Existing authority is never restamped

- **GIVEN** an entity already exists
- **WHEN** a contract-bound create is attempted
- **THEN** the call returns typed exists
- **AND** no ownership or stub metadata is written

### Requirement: Duplicate-resistant append evidence

Append MUST validate canonical tuples against an `append` group and consume per-subject partial results. The client MUST
make one request and MUST NOT retry automatically. A caller MAY submit a new request for selected failed or unknown
subjects according to its own operation-specific policy; neither side may infer a cross-subject transaction.

#### Scenario: Partial append remains explicit

- **GIVEN** one subject exists and another is absent
- **WHEN** one append request targets both
- **THEN** the existing subject may apply while the absent subject reports not-found
- **AND** the client does not roll back or hide the applied subject

### Requirement: Authoritative read-back

Verification MUST use the exact entity reader and its same-entry KV revision. A value-only read or logical entity
Version MUST NOT supply reconcile/delete evidence.

#### Scenario: Verification carries storage evidence

- **GIVEN** a mutation client verifies entity A
- **WHEN** the exact read succeeds
- **THEN** it returns the canonical entity and nonzero KV revision together

### Requirement: Commit-aware classified outcomes

The client MUST preserve classified server errors and distinguish definite precondition failure from
`commit_unknown`. It MUST NOT automatically retry an ambiguously delivered mutation or translate matching current
content into proof that its request committed.

#### Scenario: Matching content does not prove request authorship

- **GIVEN** a mutation reply is lost and a later read observes the desired state
- **WHEN** the client reports the observation
- **THEN** it may report desired state currently observed
- **AND** it does not claim that request produced it

### Requirement: Stable mutation provenance

Create and append requests MUST carry a non-empty stable request ID and source. The client MUST copy input triples,
fill only missing source, timestamp, and request-ID context fields, and reject conflicting non-zero provenance. A
caller-selected retry MUST reuse the same canonical provenance; no owner token exists or participates in request
identity.

#### Scenario: Retry keeps request identity

- **WHEN** a caller safely retries an operation
- **THEN** its request ID, source, timestamp, trace ID, and triples are unchanged

### Requirement: Child-entity model remains separate

The child-entity model MUST remain separate from the mutation client. The client MAY provide create, reconcile,
append, delete, and exact read to a separately specified child model, but MUST NOT define child identifiers,
predicates, linkage, ordering, cardinality, query behavior, or structured-literal encoding.

#### Scenario: A child model adopts the mutation client

- **WHEN** a separately specified child model uses this client
- **THEN** that model remains responsible for its own entity and lifecycle semantics

### Requirement: Todo and lesson writers use narrow atomic replacement

Todo and lesson writers MUST use local contract-bound reconcile with an exact-read revision. Each operation performs one
exact read and one mutation request and surfaces classified outcomes without automatic retry. Ownership bootstrap,
binding, and heartbeat failures are not part of their startup path.

#### Scenario: A lesson reconcile has bounded conflict handling

- **GIVEN** the lesson changed after the writer's exact read
- **WHEN** reconcile returns revision mismatch
- **THEN** the writer applies its declared caller policy or returns the conflict
- **AND** no lease or owner token is consulted

### Requirement: Todo reconciliation preserves explicit record boundaries

The todo writer MUST encode each logical item as one deterministic JSON object in an `agent.todo.record` triple. The
object MUST contain `id`, `content`, `status`, `position`, and `updated_at` with the existing logical validation. Each
call MUST reconcile the complete desired record set; omitted items are removed and an empty list clears the group. The
predicate MUST be rule-opaque, and successful tool metadata MUST expose logical `todo_count` but no storage-shaped
`triple_count`.

The exact reader MUST decode all record triples and return the logical list ordered by position. A missing entity or an
entity with no record triples MUST produce an empty list. Any malformed record MUST fail the complete list with no
partial result. The five legacy todo predicates, positional grouping, aliases, dual writes, and compatibility reads MUST
NOT remain.

#### Scenario: A malformed item cannot shear the list

- **GIVEN** an exact entity read contains valid todo records and one malformed `agent.todo.record`
- **WHEN** `TodoReader` decodes the current list
- **THEN** it returns an error and no todo items
- **AND** it never combines fields across records or silently skips the malformed item

#### Scenario: Complete-list reconcile removes omitted items

- **GIVEN** the current entity contains records A and B
- **WHEN** `write_todos` reconciles a complete desired list containing only B
- **THEN** record A is removed and B remains
- **AND** the result reports `todo_count` equal to one without `triple_count`

## ADDED Requirements

### Requirement: Local groups use birth, reconcile, and append semantics

`projection.Contract` MUST retain birth predicates and named predicate groups while replacing ownership modes with local
`reconcile` and `append` operations. Reconcile names a complete desired predicate set; append names exact evidence
tuples. Foreign-edge declarations and owner derivation MUST NOT remain in the contract.

#### Scenario: A reconcile omission deletes only selected predicates

- **GIVEN** a reconcile group names predicates P and Q
- **WHEN** desired state supplies P but omits Q
- **THEN** Q is removed and P is reconciled
- **AND** predicates outside the group are untouched

## REMOVED Requirements

### Requirement: Backward-compatible predicate-group names

**Reason**: this is a clean pre-v1 break; old ownership mode names and aliases are removed.

**Migration**: configurations use `reconcile` or `append` only.

### Requirement: Schema-derived owned replacement

**Reason**: local reconcile replaces globally owned predicate groups.

**Migration**: callers select a named local reconcile group and provide an exact-read revision.

### Requirement: Owner-bound rollout requires fail-closed lease enforcement

**Reason**: owner claims, leases, tokens, and enforcement are deleted.

**Migration**: storage safety comes from explicit operation semantics and CAS outcomes.

### Requirement: Existing NATS wire compatibility

**Reason**: old subjects and wire shapes receive no compatibility handler.

**Migration**: all in-repo callers move in the coordinated cutover.

### Requirement: First internal migration is aggregate, fail-closed, and bounded

**Reason**: the historical ownership rollout requirement is replaced by the coordinated foundation cutover.

**Migration**: implementation follows this change's task order and final gates.

### Requirement: PR #696 does not absorb deferred mutation lanes

**Reason**: the approved foundation deliberately absorbs and replaces the complete mutation surface.

**Migration**: all admitted mutation lanes conform to the four-operation algebra.
