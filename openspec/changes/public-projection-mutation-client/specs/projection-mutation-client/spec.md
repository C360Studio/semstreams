# Projection Mutation Client Specification

## ADDED Requirements

### Requirement: Contract-bound mutation client

The framework MUST expose a concurrency-safe projection mutation client bound from an owner, one or more
projection contracts, an ownership Registry when registration is derived, an optional heartbeater, and a NATS
client.

Owner presence MUST represent lease liveness only. It MUST NOT serve as registration identity or control whether a
same-owner registration is allowed. Registration identity MUST remain a Registry-lifetime invariant independent of
presence.

An ownership `Registry` MUST permit at most one successful registration for an owner during that Registry's
lifetime. The rule MUST be enforced by `RegisterOwner` and therefore apply equally to direct `RegisterOwner`,
`projection.Bind`, `BindAndHeartbeat`, and `BindMutationClient`. A concurrent or later same-owner attempt MUST
return an error matching `ErrOwnerAlreadyBound` before owner-presence heartbeat, ownership-claim mutation, or
heartbeater enrollment. The rejection MUST apply when the second registration is identical, overlapping, or
disjoint.

The composition root MUST aggregate the complete contract set intended for a registered owner before its first
registration. All static built-in contracts for one owner MUST be validated together and bound in one call. A
failed first registration MUST release the in-progress owner identity so a corrected first attempt can proceed
against the same Registry. After a successful registration, correction or revival MUST use a new Registry and
incarnation.

When a contract collection derives neither an ownership claim nor a foreign-edge claim, binding MUST skip
`RegisterOwner`, return a zero owner token, and MUST NOT consume the owner registration identity. It MUST create no
owner-presence record and MUST NOT enroll a heartbeater. This exception includes a birth-only client.

When the complete contract collection derives only foreign-edge claims, only append-evidence claims, or both,
binding MUST register one persistent atomic owner entry. It MUST return a zero owner token, create no owner-presence
record, and enroll no heartbeater. Missing presence MUST NOT make that persistent registration dead or eligible for
lease reaping.

Valid append-only and combined foreign-edge/append registrations MUST be treated as first-class persistent
postures. The Registry MUST NOT emit a misconfiguration warning solely because either posture lacks a
replace-owned or CAS claim.

If any supplied contract derives an owning claim from a `replace-owned` or `cas-transition` group, the client MUST
require a non-nil heartbeater before registration, heartbeat, or transport side effects. It MUST bind that
complete collection as one liveness-managed atomic owner entry, create owner presence, retain a non-zero opaque
owner token, and enroll the owner exactly once. Liveness management MUST apply to every owner, foreign-edge, and
append claim in that entry. The client MUST NOT expose or refresh the token per request.

The same-Registry guard MUST reject a second registration for both persistent and liveness-managed owners even when
no owner-presence record exists. Birth-only/no-claim binding MUST remain outside that guard because it creates no
registration.

Permanent foreign-edge cross-type conflicts MUST remain outside #700. This amendment MUST NOT invent presence,
expiry, selective reaping, or conflict precedence for persistent foreign-edge registrations.

#### Scenario: Successful liveness-managed binding

- **WHEN** a caller binds a valid owner set containing any replace-owned or CAS claim
- **THEN** the client registers the contracts, starts owner liveness through the supplied heartbeater, and returns a
  client with a non-zero owner token ready for mutation
- **AND** the complete atomic owner entry is liveness-managed

#### Scenario: Same owner registers a second time through any entry point

- **GIVEN** one owner has registered successfully against an ownership Registry
- **WHEN** direct `RegisterOwner`, `Bind`, `BindAndHeartbeat`, or `BindMutationClient` attempts the same owner again
- **THEN** the attempt fails with `ErrOwnerAlreadyBound` before heartbeat, claim mutation, or enrollment
- **AND** identical contracts do not make the second attempt idempotent
- **AND** `BindMutationClient` reports a not-committed mutation conflict while preserving the sentinel

#### Scenario: Failed first registration releases the identity

- **GIVEN** an owner's first registration fails before a clean commit
- **WHEN** the caller corrects the cause and retries against the same Registry
- **THEN** the corrected registration may become the owner's one successful registration

#### Scenario: Static contracts are bound as one owner set

- **GIVEN** multiple built-in projection contracts share one static owner
- **WHEN** the composition root performs ownership wiring
- **THEN** it validates and aggregates the contracts before one owner registration

#### Scenario: Successful owner needs correction or revival

- **GIVEN** an owner has registered successfully or its token has become stale
- **WHEN** the composition root needs to change its contracts or revive the writer
- **THEN** it creates a new Registry and incarnation instead of registering the owner again on the old Registry

#### Scenario: Owning contract has no heartbeater

- **WHEN** a contract collection derives an owning claim and the heartbeater is nil
- **THEN** binding returns an invalid error before registry, heartbeat, or transport side effects

#### Scenario: Append-only binding has no heartbeater

- **WHEN** every supplied group is `append-evidence` and the heartbeater is nil
- **THEN** binding may return a client authorized for append and authoritative read-back
- **AND** one persistent owner entry is registered with zero token, no presence, and no enrollment
- **AND** create and replace operations fail validation before transport or registry mutation

#### Scenario: Foreign-edge-only binding has no heartbeater

- **WHEN** the complete owner set contains foreign-edge claims and no owning or append claim
- **THEN** one persistent owner entry is registered with zero token, no presence, and no enrollment

#### Scenario: Foreign-edge and append binding has no heartbeater

- **WHEN** the complete owner set contains foreign-edge and append claims but no replace-owned or CAS claim
- **THEN** all claims register as one persistent atomic entry
- **AND** the client retains zero token and creates no presence or enrollment

#### Scenario: Valid persistent posture does not warn

- **WHEN** a valid append-only or combined foreign-edge/append owner registers without an owning claim
- **THEN** registration succeeds as a persistent posture
- **AND** no misconfiguration warning is emitted solely because an owning claim is absent

#### Scenario: Birth-only binding has no heartbeater

- **WHEN** a contract has birth predicates but no mutable groups or foreign edges
- **THEN** binding may return a client authorized for create and authoritative read-back
- **AND** the client creates no registration, presence, owner token, or heartbeater enrollment

#### Scenario: Mixed owner entry contains an owning claim

- **GIVEN** one complete owner set contains replace-owned or CAS plus append and/or foreign-edge claims
- **WHEN** binding succeeds
- **THEN** the entire owner entry is liveness-managed with one non-zero token and one heartbeater enrollment
- **AND** no subset is registered persistently outside that atomic entry

#### Scenario: Persistent owner has no presence

- **GIVEN** a foreign-edge-only or append-only owner registered successfully without presence
- **WHEN** the same owner attempts a second registration in the same Registry
- **THEN** the attempt fails with `ErrOwnerAlreadyBound`
- **AND** missing presence does not reopen or reap the persistent registration

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

### Requirement: Create-only birth predicates are not graph-enforced immutable facts

`Contract` MUST expose optional `BirthPredicates`. Every birth predicate MUST be a registered canonical exact
predicate. Duplicates and overlap with any `replace-owned`, `cas-transition`, or `append-evidence` group in the same
contract MUST be rejected.

Birth predicates MUST derive no ownership or foreign-edge claim, MUST NOT participate in a replacement removal set,
and MUST NOT authorize append. A contract containing only birth predicates MUST be valid.

A birth predicate MAY equal a foreign-edge predicate because foreign edges apply to a different subject lane.
Create-only MUST describe authorization through this client only. Graph-ingest MUST NOT be represented as enforcing
write-once behavior for these predicates. A nonconforming writer using another accepted mutation lane MAY change or
remove them.

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
- **THEN** graph-ingest receives one existing create-with-triples request using the client's zero or non-zero token
  posture
- **AND** every supplied triple has the primary entity as its subject
- **AND** the receipt reports the commit state and authoritative entity when available

#### Scenario: Birth-only creation

- **WHEN** all create triples are declared birth predicates and the contract has no owning group
- **THEN** the existing create-with-triples request carries no owner token

#### Scenario: Another writer changes a birth predicate

- **GIVEN** an entity was created through a birth predicate
- **WHEN** a nonconforming writer changes that predicate through another accepted mutation lane
- **THEN** this client contract provides no lease or write-once enforcement against that change

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
- **AND** an absent read does not prove the original request cannot commit late

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

### Requirement: Duplicate-resistant append evidence

The evidence appender MUST accept only triples in the named contract's `append-evidence` groups and MUST limit an
operation to one entity subject.

It MUST NOT unconditionally retry an ambiguous append. It MUST first read authoritative state and search for the
exact canonical evidence tuple of subject, predicate, object, datatype, source, and request-ID context. This
six-field append key MUST remain intentionally narrower than complete create and replace triple equality.

The client MUST NOT claim exactly-once behavior or prove absence after a timeout. If read-back reports the tuple
absent and the client retries, the original request MAY commit after that read and the retry MAY append a duplicate.
Generic outer retry loops MUST be prohibited. Deployments requiring strict no-retry behavior MUST configure
`Retry.MaxRetries=0` until the server-side idempotency primitive tracked by
[issue #697](https://github.com/C360Studio/semstreams/issues/697) exists.

#### Scenario: Append response is lost after commit

- **WHEN** the append commits but its response is lost
- **THEN** authoritative read-back finds the exact evidence tuple
- **AND** the client reports verified success without issuing another append

#### Scenario: Append read-back is absent

- **WHEN** an ambiguous append is followed by authoritative read-back that does not contain the exact tuple
- **THEN** the client may retry within the configured budget using identical provenance
- **AND** the result remains vulnerable to the original attempt committing late and double-applying

#### Scenario: Append cannot be verified

- **WHEN** both append outcome and authoritative read-back are unavailable
- **THEN** the client returns commit-unknown and does not retry

#### Scenario: No responders is distinct from timeout

- **WHEN** NATS reports no responders for a mutation attempt
- **THEN** the client may retry within its configured budget because no serving handler accepted that attempt
- **AND** a timeout or lost response remains ambiguous and MUST NOT be classified as no responders

### Requirement: Owner-bound rollout requires fail-closed lease enforcement

Every graph-ingest instance serving token-fenced create or replace traffic for a liveness-managed client MUST
enable `enforce_owner_lease=true`. Before that traffic is enabled, rollout evidence MUST prove claim-reader wiring
on every serving instance, a live owner heartbeat, and zero owner-lease mismatch metrics during a bounded
observation window.

Semdragon issue #313 MUST remain gated until that evidence exists. A configuration containing a non-enforcing
serving instance MUST fail deployment readiness rather than rely on routing affinity.

#### Scenario: One serving instance does not enforce leases

- **GIVEN** a graph-ingest fleet serving the same mutation subjects
- **AND** one instance does not enable owner-lease enforcement
- **WHEN** owner-bound client rollout is evaluated
- **THEN** readiness fails and Semdragon #313 remains blocked

#### Scenario: Enforcement rollout is proven

- **GIVEN** every serving instance enables owner-lease enforcement and has a claim reader
- **WHEN** the owner heartbeat remains live and mismatch metrics remain zero for the rollout window
- **THEN** owner-bound mutation traffic may be enabled

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

PR #696 MUST remain a later internal-adoption change and MUST NOT be treated as part of this public API change or
as evidence that Semdragon #313 has passed its serving-fleet enforcement gate.

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
