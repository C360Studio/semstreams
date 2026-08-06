# rule-projection-mutations Specification

## Purpose

Define fail-closed local graph mutation contracts for rule packs without semantic ownership infrastructure.

## Requirements

### Requirement: Complete rule-pack snapshots are preflighted

The composition root MUST perform side-effect-free validation for every enabled rule pack before constructing any
rule-pack mutation client. Preflight MUST validate pack IDs, duplicate IDs, copied local contracts, required narrow
dependencies, exact target indexes, and every configured reconcile or append action. Local overlap between packs is
valid. Preflight and initialization MUST consume the same immutable validated snapshot.

#### Scenario: A later pack is invalid

- **GIVEN** the first pack is valid and a later enabled pack is invalid
- **WHEN** composition preflight runs
- **THEN** composition fails before any client is constructed or processor starts

### Requirement: Rule-pack envelopes are static across hot reload

Pack ID, local projection contracts, mutation-client dependencies, and exact target indexes MUST be immutable within
one running snapshot. Hot reload MUST preflight the complete replacement snapshot and switch atomically only when every
target is valid. It MUST NOT mutate a running client or action envelope in place.

#### Scenario: Reload changes a contract

- **WHEN** a runtime update changes pack identity, contracts, group membership, or target selection
- **THEN** the complete replacement snapshot is preflighted before activation
- **AND** the previous snapshot remains active if validation fails

### Requirement: Explicit contracts are validated supersets

When `projection_contracts` is supplied, every statically selected reconcile or append action MUST be covered by a
declared contract with the same case-sensitive contract and group names, compatible operation, predicates, and entity
pattern. Omission MAY select default local derivation. An explicitly empty array MUST fail when actions require a
contract. Declared extras remain local validation scope and grant no global authorization.

#### Scenario: Declared group is narrower than an action

- **WHEN** an action-selected predicate is absent from its declared group
- **THEN** preflight fails before processor start

### Requirement: Non-inferable metadata remains explicit

Birth predicates, indexing profile, and optional message type MUST NOT be inferred from rule actions. When declared,
they MUST be copied into the effective local contract and pass projection validation. Owner claims, presence, tokens,
heartbeat posture, and foreign-edge modes MUST NOT exist.

#### Scenario: Derived contract has no explicit metadata

- **WHEN** a local contract is derived without an explicit declaration
- **THEN** birth predicates, indexing profile, and message type remain empty

### Requirement: Rule packs construct one local contract-bound client

Construction MUST validate contract names, group names, entity patterns, predicates, and narrow dependencies. It MUST
NOT register pack ownership, presence, heartbeat, tokens, or cross-pack claims.

#### Scenario: Local overlap is not a boot conflict

- **GIVEN** two packs reconcile the same locally valid predicate group
- **WHEN** boot preflight runs
- **THEN** both contracts validate
- **AND** no global overlap registry rejects startup

### Requirement: Rule reconcile targets are exact and immutable

A `reconcile_predicates` action MUST name a case-sensitive projection contract and reconcile group and MUST resolve to
one statically provable literal entity target or `$entity.id`. The frozen action envelope consumed at runtime MUST
equal the preflighted envelope. Dynamic contract/group selection and arbitrary predicates are invalid.

#### Scenario: Hot reload cannot change a running target

- **GIVEN** a running rule snapshot has a validated reconcile action
- **WHEN** a new configuration is loaded
- **THEN** the replacement snapshot is fully preflighted before activation
- **AND** the running action envelope remains unchanged

### Requirement: Rule reconcile makes one exact read and one mutation attempt

A rule reconcile action MUST resolve its complete desired group once from the `ExecutionContext` that caused the
evaluation and invoke the contract-bound reconciler once. That call MUST perform exactly one exact authority read and
one mutation request. A definite `revision_mismatch` MUST remain a visible classified action failure; the action MUST
NOT replay or recompute the old `ExecutionContext`. `commit_unknown` MUST NOT be automatically retried. Successful
receipts MUST retain the exact committing revision. No retry helper, knob, loop, or coordinator is part of this
contract.

#### Scenario: A racing writer produces one visible conflict

- **GIVEN** another writer commits after the rule action's exact read
- **WHEN** reconcile returns `revision_mismatch`
- **THEN** the rule action returns the classified conflict visibly
- **AND** it sends no second exact read, no second mutation, and no third request of any kind
- **AND** it does not replay or recompute the old `ExecutionContext`

### Requirement: Rule mutations use reconcile and append semantics

Complete desired predicate sets MUST use `reconcile_predicates`; exact evidence MUST use append. The retired
`replace_owned` action and legacy mutation wire shapes MUST NOT appear in rule schemas, execution, examples, or
generated configuration. Rules continue to carry references rather than bulky payloads, and components execute work.

#### Scenario: Removed action cannot survive generated schema

- **GIVEN** a rule configuration uses `replace_owned`
- **WHEN** generated-schema validation runs
- **THEN** configuration is rejected
- **AND** no alias maps it to reconcile

### Requirement: Receipts preserve exact mutation evidence

A successful rule mutation MUST preserve entity ID, operation, request ID, trace ID, and committing KV revision in its
receipt. A failed action MUST expose the classified outcome and commit state without fabricating a successful receipt.

#### Scenario: Ambiguous delivery is visible

- **GIVEN** a reconcile request may have committed but its reply was lost
- **WHEN** the rule action receives `commit_unknown`
- **THEN** the action fails visibly without retry
- **AND** no later matching read is reported as proof of authorship
