## ADDED Requirements

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

## MODIFIED Requirements

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

