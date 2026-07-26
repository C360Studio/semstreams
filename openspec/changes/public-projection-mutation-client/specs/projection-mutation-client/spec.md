# Projection Mutation Client Specification

## ADDED Requirements

### Requirement: Contract-bound mutation client

The framework MUST expose a concurrency-safe projection mutation client bound from an owner, one or more
projection contracts, the ownership registry, an optional heartbeater, and a NATS client.

If any supplied contract derives an owning claim from a `replace-owned` or `cas-transition` group, the client MUST
require a non-nil heartbeater before registration, heartbeat, or transport side effects. It MUST bind that
collection through the existing ownership registry and heartbeat path, retain the returned opaque owner token, and
MUST NOT expose or refresh the token per request.

#### Scenario: Successful binding

- **WHEN** a caller binds a valid owner and non-conflicting contracts
- **THEN** the client registers the contracts, starts owner liveness through the supplied heartbeater, and returns a
  client ready for mutation

#### Scenario: Owning contract has no heartbeater

- **WHEN** a contract collection derives an owning claim and the heartbeater is nil
- **THEN** binding returns an invalid error before registry, heartbeat, or transport side effects

#### Scenario: Append-only binding has no heartbeater

- **WHEN** every supplied group is `append-evidence` and the heartbeater is nil
- **THEN** binding may return a client authorized for append and authoritative read-back
- **AND** create and replace operations fail validation before transport or registry mutation

#### Scenario: Birth-only binding has no heartbeater

- **WHEN** a contract has birth predicates but no mutable groups or foreign edges
- **THEN** binding may return a client authorized for create and authoritative read-back
- **AND** the client does not start a heartbeat or mint an owner token

#### Scenario: Stale owner token

- **WHEN** graph-ingest rejects a create or replace request with `owner_lease_stale`
- **THEN** the client returns a stale-owner-token error with a not-committed state
- **AND** the client does not retry or automatically rebind

### Requirement: Narrow public capabilities

The framework MUST expose narrow interfaces for authoritative entity creation, owned replacement, evidence append,
and authoritative read-back. The concrete client MAY satisfy all four interfaces.

The public API MUST NOT expose rule identifiers, rule action types, arbitrary removal predicates, raw owner-token
strings, or lifecycle expected revisions.

#### Scenario: Least-privilege consumer

- **WHEN** a component only appends evidence
- **THEN** it can accept the evidence-appender interface without receiving create or replace capabilities

### Requirement: Backward-compatible predicate-group names

`PredicateGroup` MUST expose an optional stable `Name`. A non-empty name MUST be one case-sensitive NATS subject
token with no `.`, whitespace, `*`, or `>`, and names MUST be unique across all groups in one contract.

Existing unnamed groups MUST remain valid. An unnamed group MUST NOT be selectable by name.

#### Scenario: Existing unnamed contract

- **WHEN** an existing valid contract omits every predicate-group name
- **THEN** contract validation continues to succeed

#### Scenario: Duplicate or unsafe group name

- **WHEN** two groups have the same name or a name contains a forbidden subject character
- **THEN** contract validation fails before ownership registration

### Requirement: Immutable create-only birth predicates

`Contract` MUST expose optional `BirthPredicates`. Every birth predicate MUST be a registered canonical exact
predicate. Duplicates and overlap with any `replace-owned`, `cas-transition`, or `append-evidence` group in the same
contract MUST be rejected.

Birth predicates MUST derive no ownership or foreign-edge claim, MUST NOT participate in a replacement removal set,
and MUST NOT authorize append. A contract containing only birth predicates MUST be valid.

A birth predicate MAY equal a foreign-edge predicate because foreign edges apply to a different subject lane.

#### Scenario: Valid birth-only contract

- **WHEN** a contract declares at least one valid birth predicate and no groups or foreign edges
- **THEN** contract validation succeeds and ownership derivation produces no claim for those predicates

#### Scenario: Birth predicate overlaps mutable group

- **WHEN** a predicate appears in both `BirthPredicates` and any predicate group
- **THEN** contract validation fails before ownership registration

#### Scenario: Birth predicate is not canonical

- **WHEN** a birth predicate is undeclared, empty, duplicated, or contains a predicate wildcard
- **THEN** contract validation fails before ownership registration

### Requirement: Contract-validated atomic creation

The creator MUST publish the existing create-with-triples request and MUST write the entity and its
primary-subject initial triples atomically.

Before transport, it MUST validate the named contract, entity pattern, message type, birth predicates, predicate
groups, subject, and indexing profile. Every supplied triple MUST use the primary entity ID as its subject.

Every create triple predicate MUST be declared in `BirthPredicates` or in a `replace-owned` or `cas-transition`
group. An `append-evidence` group by itself MUST NOT authorize creation. A primary-subject outbound relationship
triple MAY be included when its predicate is in one of the create-authorized lanes.

The creator MUST reject every cross-subject triple, including one matching a `ForeignEdgeClaim`. Cross-subject
writes MUST use the existing reconciliation path and are not part of this client's atomicity or verification
guarantee. The creator MUST attach the bound owner token when the creation contains owned predicates.

`CreateMutation.Triples` MUST be the sole birth-fact source. The creator MUST reject a non-empty
`CreateMutation.Entity.Triples` before mutation transport side effects. It MUST NOT send a mutation RPC or
authoritative read-back, modify caller input, merge the two fields, or choose one by precedence.
Authoritative create verification MUST compare every field of each canonical `message.Triple`, including
`Confidence` and `ExpiresAt`.

#### Scenario: Valid authoritative birth

- **WHEN** a caller creates a conforming entity with its declared birth facts
- **THEN** graph-ingest receives one existing create-with-triples request containing the opaque owner token
- **AND** every supplied triple has the primary entity as its subject
- **AND** the receipt reports the commit state and authoritative entity when available

#### Scenario: Immutable birth-only creation

- **WHEN** all create triples are declared birth predicates and the contract has no owning group
- **THEN** the existing create-with-triples request carries no owner token

#### Scenario: Append-only contract attempts creation

- **WHEN** a contract declares only append-evidence predicates and a caller requests creation
- **THEN** the client returns an invalid error without sending a mutation request

#### Scenario: Predicate outside the contract

- **WHEN** a creation contains a primary-entity predicate not permitted by the named contract
- **THEN** the client returns an invalid error before publishing

#### Scenario: Cross-subject birth fact

- **WHEN** creation contains a triple whose subject differs from the primary entity ID
- **THEN** the client returns an invalid error before publishing
- **AND** it does so even when the triple could match a declared `ForeignEdgeClaim`

#### Scenario: Entity embeds birth facts

- **WHEN** `CreateMutation.Entity.Triples` is non-empty
- **THEN** the client returns an invalid error without sending a mutation RPC or authoritative read-back
- **AND** it does not merge those triples with `CreateMutation.Triples`
- **AND** caller input remains unchanged

#### Scenario: Create response is lost

- **WHEN** creation has an ambiguous transport result
- **THEN** the client performs authoritative read-back before any retry
- **AND** it reports verified success only if entity identity, message type, and every requested primary-subject
  birth fact match as a complete canonical `message.Triple`
- **AND** it returns commit-unknown without retry when authoritative verification is unavailable

#### Scenario: Existing entity is divergent

- **WHEN** create receives `entity_already_exists` and authoritative state differs from requested birth facts
- **THEN** the client returns a conflict and does not treat existence alone as idempotent success

### Requirement: Schema-derived owned replacement

`ReplaceOwnedMutation` MUST expose an optional `Group` selector. A non-empty selector MUST resolve exactly one named
`replace-owned` group in the named contract.

An omitted selector MUST be accepted only when the contract has exactly one `replace-owned` group. It MUST be
rejected when the contract has none or more than one. Existing unnamed groups remain usable through this
single-group omission rule but cannot be selected by name.

The owned replacer MUST derive the complete removal set from only the selected group. Desired triples MUST be
limited to that group. Omitted predicates in the selected group MUST be removed, and sibling groups, birth
predicates, foreign predicates, and append-only predicates MUST be preserved.

Authoritative replace verification MUST compare complete canonical `message.Triple` values for the selected group,
including `Confidence` and `ExpiresAt`, and MUST prove omitted selected-group facts absent. It MUST ignore sibling
groups.

#### Scenario: Delete on omission

- **WHEN** desired state omits a predicate declared in the selected replace-owned group
- **THEN** the existing update-with-triples request includes that predicate in its removal set

#### Scenario: Named group preserves siblings

- **WHEN** a caller selects one named replace-owned group in a contract with multiple groups
- **THEN** the removal set contains every predicate in only that selected group
- **AND** predicates in sibling groups are not removed or considered during verification

#### Scenario: Selector omitted for one group

- **WHEN** a contract has exactly one replace-owned group and the caller omits `Group`
- **THEN** that group is selected whether or not it has a name

#### Scenario: Selector omitted for multiple groups

- **WHEN** a contract has multiple replace-owned groups and the caller omits `Group`
- **THEN** the client returns an invalid error before publishing

#### Scenario: Unknown or non-replace group

- **WHEN** `Group` names no group, an unnamed group, or a group in another write mode
- **THEN** the client returns an invalid error before publishing

#### Scenario: Caller attempts foreign removal

- **WHEN** desired state or a requested operation would replace a foreign or append-only predicate
- **THEN** the client returns an invalid error before publishing

#### Scenario: Replace transport retry

- **WHEN** replacement encounters a retryable transport failure within its context and retry budget
- **THEN** the client may resend the identical replacement request
- **AND** the owner token and schema-derived removal set remain unchanged

### Requirement: Duplicate-safe append evidence

The evidence appender MUST accept only triples in the named contract's `append-evidence` groups and MUST limit an
operation to one entity subject.

It MUST NOT unconditionally retry an ambiguous append. It MUST first read authoritative state and search for the
exact canonical evidence tuple of subject, predicate, object, datatype, source, and request-ID context. This
six-field append key MUST remain intentionally narrower than complete create and replace triple equality.

#### Scenario: Append response is lost after commit

- **WHEN** the append commits but its response is lost
- **THEN** authoritative read-back finds the exact evidence tuple
- **AND** the client reports verified success without appending a duplicate

#### Scenario: Append absence is proven

- **WHEN** an ambiguous append is followed by authoritative read-back that proves the exact tuple absent
- **THEN** the client may retry within the configured budget using identical provenance

#### Scenario: Append cannot be verified

- **WHEN** both append outcome and authoritative read-back are unavailable
- **THEN** the client returns commit-unknown and does not retry

### Requirement: Stable mutation provenance

Create and append requests MUST carry a non-empty stable request ID and source. The client MUST use copies of input
triples and fill only missing source, timestamp, and request-ID context fields.

The client MUST reject conflicting non-zero provenance and MUST reuse identical canonical values for retry and
read-back comparison.

#### Scenario: Conflicting triple provenance

- **WHEN** a triple source or context conflicts with mutation metadata
- **THEN** the client returns an invalid error before publishing

#### Scenario: Retry keeps identity

- **WHEN** an operation is safely retried
- **THEN** its request ID, source, timestamp, trace ID, triples, and owner token are unchanged

### Requirement: Authoritative read-back

The framework MUST expose authoritative entity read-back using the existing graph-ingest entity query subject and
entity representation.

Mutation verification MUST use this authoritative path rather than watcher, index, or cache state.

#### Scenario: Verification bypasses projection cache

- **WHEN** a mutation result requires verification
- **THEN** the client queries graph-ingest authoritative state even if a local watcher contains the entity

### Requirement: Commit-aware classified outcomes

The client MUST return a receipt containing not-committed, unknown, committed, or verified commit state. It MUST
return typed mutation errors for invalid, not-found, conflict, revision-conflict, stale-owner-token, unavailable,
commit-unknown, committed-unverified, and internal outcomes.

Typed mutation errors MUST unwrap their underlying classified or sentinel error.

#### Scenario: Existing error inspection remains valid

- **WHEN** a caller receives a typed mutation error backed by an existing classified response
- **THEN** `errors.As` and `errors.Is` can still inspect the existing classified and sentinel causes

#### Scenario: Handler reports degraded success

- **WHEN** a mutation response reports `degraded=true`
- **THEN** the client treats the mutation as committed and never retries it
- **AND** it performs authoritative read-back
- **AND** failed verification returns a committed receipt with a committed-unverified error

#### Scenario: Context expires

- **WHEN** the caller context is cancelled or reaches its deadline
- **THEN** the client stops retry and read-back work and returns a classified outcome preserving commit knowledge

### Requirement: Existing NATS wire compatibility

The client MUST use the existing create-with-triples, update-with-triples, add-batch, and authoritative entity query
subjects and their existing JSON request/response types.

The change MUST NOT add an envelope, use `BaseMessage`, require a new graph-ingest handler, or alter a persisted
representation. Predicate-group names, birth predicates, and the replace group selector MUST remain local client
and contract inputs and MUST NOT be added to graph mutation requests.

#### Scenario: Existing graph-ingest deployment

- **WHEN** a client built with this capability talks to a compatible existing graph-ingest deployment
- **THEN** requests decode through the existing handlers without a wire or schema migration

### Requirement: Child-entity model remains separate

The child-entity model MUST remain separate from the mutation client. The client MAY provide atomic creation, owned
replacement, and read-back to issue #683 if that issue selects canonical child entities.

The mutation client MUST NOT define child identifiers, child predicates, parent linkage, ordering, cardinality,
query behavior, or structured-literal encoding.

#### Scenario: Issue 683 selects child entities

- **WHEN** issue #683 adopts canonical child entities
- **THEN** its implementation can depend on the public mutation interfaces
- **AND** its own specification remains responsible for the child-entity model and lifecycle
