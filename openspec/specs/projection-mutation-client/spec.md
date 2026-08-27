# projection-mutation-client Specification

## Purpose

Define the local contract-validating graph mutation client used by framework components.
## Requirements
### Requirement: Projection contracts are local schemas

`pkg/projection` MUST validate copied local contracts containing an entity pattern, an optional structured message type
(`message.Type`, serialised as `{"domain","category","version"}` exactly like `EntityState.message_type`; never a dotted
string), an optional indexing profile, create-time birth predicates, and named predicate groups. Groups MUST use only `reconcile` or `append` mode.
A contract MUST NOT register or imply an owner, claim, lease, heartbeat, token, presence record, foreign-edge mode, or
global overlap rule. Contracts in different components MAY overlap. The contract data types (`Contract`, `PredicateGroup`,
`WriteMode`, the modes, `ErrInvalidContract`, `Validate`, `ValidateContracts`) MUST live in the leaf package
`pkg/projection/contract` so the payload registry can hold them; `pkg/projection` MUST re-export them as aliases so existing
literals compile unchanged. The indexing profile MUST be validated against the vocabulary's profile set, not a private copy.
A contract registered with a payload type inherits that type's key as its message type.

#### Scenario: Two components describe the same predicate group

- **GIVEN** two valid local contracts overlap on an entity pattern and predicate
- **WHEN** both clients are constructed
- **THEN** construction succeeds without global registration
- **AND** runtime conflicts are observed through Create/CAS outcomes
- **AND** the test that verifies this is `TestOverlappingLocalContractsConstruct`

#### Scenario: An existing contract literal compiles against the aliases

- **WHEN** a product constructs `projection.Contract{MessageType: message.Type{...}, Groups: []projection.PredicateGroup{{Mode: projection.ModeReconcile}}}`
- **THEN** it compiles and validates exactly as before the leaf split (a dotted-string `MessageType` no longer compiles; a
  rule pack declares `"message_type": {"domain": …, "category": …, "version": …}`)
- **AND** the test that verifies this is `TestContractLiteralCompilesAgainstAliases`

### Requirement: Contract construction is side-effect free and immutable

Client construction MUST validate and copy the complete contract set before returning. Contract names and group names
MUST be unique within their scopes. Predicates MUST be canonical declared predicates and MUST appear at most once in a
contract. Construction MUST perform no registration or graph mutation.

#### Scenario: Caller input changes after construction

- **WHEN** a caller mutates its source contract slice after client construction
- **THEN** the client's validated contract snapshot remains unchanged

### Requirement: Narrow public capabilities

Public interfaces MUST expose only create, reconcile, append, delete, or exact-read capabilities needed by a consumer.
The client MUST use the declared graph mutation request port and one operation-specific exact reader. It MUST NOT
expose raw subjects, a general graph client, or raw KV.

#### Scenario: A birth-only consumer receives no reconcile capability

- **GIVEN** a consumer only creates entities
- **WHEN** dependencies are wired
- **THEN** it receives the narrow creator interface

### Requirement: Create validates the complete birth

Create MUST validate the entity and all initial triples against one local contract, require each triple to use the
new entity ID as its subject, and call strict `entity.create`. `CreateMutation.Triples` MUST be the sole initial-fact
source.
Existing authority MUST return a classified entity-exists outcome; no hierarchy or stub side effect applies.

#### Scenario: Existing authority is not converted into success

- **GIVEN** an entity already exists
- **WHEN** a contract-bound create is attempted
- **THEN** the client returns the classified conflict
- **AND** it does not read back matching content and claim its request committed

### Requirement: Reconcile replaces one complete selected group

Reconcile MUST select one named `reconcile` group, exact-read the entity once, and submit the same-entry nonzero KV
revision with the complete desired predicate set. Omitted predicates in the selected group MUST be removed; predicates
outside it MUST remain untouched. Equality MUST include the persisted `Timestamp`, `Confidence`, and `ExpiresAt`
annotations as desired state. A caller seeking `unchanged` MUST preserve observed annotations rather than regenerate a
timestamp. The client MUST NOT retry revision mismatch automatically.

#### Scenario: Component owns retry policy

- **GIVEN** reconcile returns a definite revision mismatch
- **WHEN** the client returns to its caller
- **THEN** no automatic second request has been sent
- **AND** the component may exact-read, recompute, and retry according to its own policy

#### Scenario: Annotation-only desired state commits once

- **GIVEN** a current selected triple and desired state that changes only one persisted annotation
- **WHEN** the caller reconciles that desired state and then repeats it exactly from the committed revision
- **THEN** the annotation-only change is applied with a new KV revision
- **AND** the exact repeat is unchanged without advancing authority

### Requirement: Append preserves explicit per-subject outcomes

Append MUST validate exact canonical tuples against an `append` group. The wire response MUST retain one discriminated
result per subject with outcome `applied`, `unchanged`, `entity_not_found`, or `failed`. Applied and unchanged results
MUST carry their observed KV revision; failed results MUST carry a classified error. No cross-subject transaction or
automatic retry MAY be inferred.

#### Scenario: Partial append remains explicit

- **GIVEN** one subject exists and another is absent
- **WHEN** one append request targets both
- **THEN** the existing subject may apply while the absent subject reports not-found
- **AND** the client does not roll back or hide the applied subject

### Requirement: Delete uses caller-supplied exact revision

Delete MUST require a nonzero expected KV revision and submit one conditional delete request. It MUST NOT hide an
authority read or retry. A successful receipt MUST report the matched expected revision and MUST NOT claim a
delete-marker revision unavailable from NATS KV.

#### Scenario: Stale delete is definite

- **GIVEN** an entity changed after the caller's exact read
- **WHEN** delete is attempted with the older revision
- **THEN** the client returns a definite revision conflict
- **AND** the entity remains present

### Requirement: Exact read carries storage evidence

The authoritative reader MUST return one validated entity and the nonzero KV revision from the same `ENTITY_STATES`
entry. A value-only read or logical entity `Version` MUST NOT supply reconcile or delete evidence.

#### Scenario: Verification carries authority revision

- **GIVEN** entity A is current at KV revision R
- **WHEN** the exact reader returns A
- **THEN** it returns A and R together

### Requirement: Classified outcomes preserve commit knowledge

The client MUST preserve classified server failures. A no-responder result MUST be `unavailable`, and a context already
done before send MUST be `deadline`; both are definite non-commits. A post-send timeout or disconnect, malformed reply,
or semantically invalid success reply MUST be `commit_unknown`. The client MUST send each mutation call once. It MUST
NOT retry an ambiguous request or translate matching current content into proof that its request committed.

#### Scenario: Lost reply remains ambiguous

- **GIVEN** a request was sent and may have reached graph-ingest
- **WHEN** its reply is lost, malformed, or semantically invalid
- **THEN** the client returns `commit_unknown`
- **AND** it sends no automatic retry

### Requirement: Mutation provenance is stable

Create and append MUST require a non-empty request ID and source. The client MUST copy input triples, fill only missing
source, timestamp, and request-ID context fields, and reject conflicting nonzero provenance. A caller-selected retry
MUST reuse the same logical request provenance.

#### Scenario: Caller retry keeps identity

- **WHEN** a component retries a logical operation
- **THEN** request ID, source, timestamp, trace ID, and tuple provenance remain unchanged

### Requirement: Missing relationship targets remain eventual state

The client MUST allow a valid source relationship whose object entity is absent. It MUST NOT create a target stub,
pending record, rollback, or repair workflow. Exact dereference MAY report the missing object independently.

#### Scenario: Object arrives later

- **GIVEN** source A references absent B
- **WHEN** B is created later
- **THEN** the next dereference resolves B without rewriting A

### Requirement: Child-entity models remain separate

The mutation client MAY support separately specified child models but MUST NOT define their identifiers, predicates,
linkage, ordering, cardinality, query behavior, or structured-literal encoding.

#### Scenario: A child model adopts the client

- **WHEN** a separately specified child model uses mutation capabilities
- **THEN** that model remains responsible for its own entity and lifecycle semantics

### Requirement: Create fills an empty entity message type from the bound contract

`MutationClient.Create` MUST fill a zero `entity.MessageType` from the bound contract's structured `MessageType` before
validation and before the request is built — no key is parsed back into components — and MUST reject a non-zero stamp that
differs from the contract's with a classified invalid error naming both keys. A contract that declares no `MessageType` together with an entity that carries no stamp MUST still be
rejected — the type is required at birth. The caller predicts nothing the contract already holds: a product using
`CreateMutation` may omit the stamp entirely.

#### Scenario: an empty stamp is filled from the contract

- **GIVEN** a contract bound to `agentic.agent_lesson.v1`
- **WHEN** `Create` receives an entity whose `MessageType` is empty
- **THEN** the `entity.create` request carries `agentic.agent_lesson.v1`
- **AND** the test that verifies this is `TestCreateFillsMessageTypeFromContract`

#### Scenario: the fill comes from the registry's structured type

- **GIVEN** a contract set derived from the payload registry (`Contracts()`), whose types are structured
- **WHEN** `Create` receives an entity with a zero stamp
- **THEN** the request carries the registered type, and a registration whose key could not round-trip is refused at
  `Register`, never deferred to the first `Create`
- **AND** the test that verifies this is `TestCreateFillsFromRegisteredContract`

#### Scenario: a conflicting stamp is rejected

- **GIVEN** the same contract
- **WHEN** `Create` receives an entity stamped `agentic.loop_execution.v1`
- **THEN** the client returns a classified invalid error naming both keys and sends no request
- **AND** the test that verifies this is `TestCreateRejectsConflictingMessageType`

