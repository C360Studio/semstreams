# rule-projection-mutations — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: The complete pack set is preflighted before binding

The composition root MUST perform side-effect-free validation for every enabled rule pack before constructing any
rule-pack mutation client. Preflight MUST validate pack IDs, duplicate IDs, complete local contracts, required narrow
dependencies, exact target indexes, and every initially configured reconcile or append action. Local overlap between
packs is valid. Preflight and initialization MUST consume the same immutable validated snapshot.

#### Scenario: A later pack is invalid

- **GIVEN** the first pack is valid and a later enabled pack is invalid
- **WHEN** composition preflight runs
- **THEN** composition fails before any client is constructed or processor starts

### Requirement: Rule-pack mutation envelopes are static across hot reload

Pack ID, local projection contracts, narrow mutation-client dependencies, and the exact target index MUST be immutable
within one running snapshot. Hot reload MUST preflight the complete replacement snapshot and switch atomically only
when every target is valid. It MUST NOT mutate a running client or action envelope in place.

#### Scenario: Reload changes a contract

- **WHEN** a runtime update changes pack identity, contracts, group membership, or target selection
- **THEN** the complete replacement snapshot is preflighted before activation
- **AND** the previous snapshot remains active if validation fails

### Requirement: Explicit projection contracts are validated supersets

When `projection_contracts` is supplied, every statically selected reconcile or append action MUST be covered by a
declared contract with the same case-sensitive contract and group names, compatible operation, predicates, and entity
pattern. Omission MAY select default local derivation; an explicitly empty array MUST fail when actions require a
contract. Declared extras remain local validation scope and grant no global authorization.

#### Scenario: Declared group is narrower

- **WHEN** an action-selected predicate is absent from its declared group
- **THEN** preflight fails before processor start

### Requirement: Non-inferable contract metadata remains explicit

Birth predicates, indexing profile, and optional message type MUST NOT be inferred from rule actions. When declared,
they MUST be copied into the effective local contract and pass projection validation. Foreign-edge metadata, owner
claims, presence, tokens, and heartbeat posture MUST NOT exist.

#### Scenario: Derived contract has no explicit metadata

- **WHEN** a local contract is derived without an explicit declaration
- **THEN** birth predicates, indexing profile, and message type remain empty

## ADDED Requirements

### Requirement: Rule packs construct one local contract-bound mutation client

The complete rule-pack set MUST be preflighted before start or hot reload against copied local projection contracts.
Construction MUST validate contract names, group names, entity patterns, predicates, and required narrow dependencies.
It MUST NOT register rule-pack ownership, presence, heartbeat, tokens, or cross-pack claims.

#### Scenario: Local overlap is not a boot conflict

- **GIVEN** two rule packs reconcile the same locally valid predicate group
- **WHEN** boot preflight runs
- **THEN** both contracts may validate
- **AND** no ownership overlap registry rejects startup

### Requirement: Rule reconcile targets remain exact and immutable

A rule reconcile action MUST name a case-sensitive projection contract and reconcile group and MUST resolve to one
statically provable literal entity target or `$entity.id`. The frozen action envelope consumed at runtime MUST equal the
preflighted envelope; dynamic contract/group selection and arbitrary predicates are invalid.

#### Scenario: Hot reload cannot change a running action's authority shape

- **GIVEN** a running rule snapshot has a validated reconcile action
- **WHEN** a new configuration is loaded
- **THEN** the replacement snapshot is fully preflighted before activation
- **AND** the running snapshot is not mutated in place

### Requirement: Rule reconcile uses exact revision with one bounded retry

Before reconcile, the rule executor MUST exact-read the entity and submit the returned nonzero revision. On definite
`revision_mismatch`, it MUST perform one fresh exact read and one retry. A second mismatch is a visible action failure.
`commit_unknown` MUST NOT be automatically retried. Successful receipts MUST retain the exact committing revision.

#### Scenario: Fighting writers do not spin forever

- **GIVEN** another writer wins both the initial reconcile and the single retry
- **WHEN** the second revision mismatch is returned
- **THEN** the rule action fails visibly
- **AND** no retry knob, loop, or ownership arbitration is introduced

### Requirement: Rule mutation names use reconcile and append semantics

`replace_owned` and raw replacement transport MUST be removed from rule schemas and execution. Complete desired
predicate sets use `reconcile`; exact evidence uses `append`. Rules continue to carry references rather than bulky
payloads, and components continue to execute work.

#### Scenario: Removed rule action cannot survive generated schema

- **GIVEN** a rule configuration uses `replace_owned`
- **WHEN** generated-schema validation runs after the cutover
- **THEN** configuration is rejected
- **AND** no alias maps it to reconcile

## REMOVED Requirements

### Requirement: Rule packs bind one public mutation client fail closed

**Reason**: ownership binding is removed; the surviving fail-closed behavior is re-specified as local preflight above.

**Migration**: construct the local narrow client after complete preflight.

### Requirement: Replacement targets are exact and immutable

**Reason**: the old requirement is coupled to `replace_owned`; the surviving guarantee is re-stated for reconcile.

**Migration**: use the exact reconcile requirement above.

### Requirement: Replace-owned actions reconcile complete selected groups

**Reason**: the action and ownership term are deleted.

**Migration**: use contract-bound `reconcile`.

### Requirement: Rule replacement preserves receipt and error semantics

**Reason**: the replacement wire is retired.

**Migration**: reconcile preserves receipts and classified outcomes under the added bounded-retry requirement.

### Requirement: The raw rule replacement transport is retired

**Reason**: retirement completes in this cutover; no continuing migration requirement remains.

**Migration**: no compatibility transport is shipped.

### Requirement: Built-in-owned rule consumption remains deferred

**Reason**: built-in and rule-pack ownership collision is no longer a framework concept.

**Migration**: components may overlap; revision outcomes expose actual write conflicts.

### Requirement: Frozen initial actions derive rule projection contracts

**Reason**: contracts remain explicit local schema and are not derived into ownership claims.

**Migration**: preflight validates the complete copied contract/action set directly.

### Requirement: Entity target scope is inferred only from statically provable inputs

**Reason**: the surviving constraint is consolidated into the added exact-target reconcile requirement.

**Migration**: no behavior is relaxed.

### Requirement: Derivation preserves fail-closed composition and mutation semantics

**Reason**: ownership derivation is deleted.

**Migration**: local contract validation and typed port composition provide the surviving failures.

### Requirement: Derived contracts are boot-time state, not authored configuration

**Reason**: ownership-derived contracts are deleted; local projection contracts remain ordinary copied configuration.

**Migration**: rule packs provide or reference local contracts explicitly.

### Requirement: Public rule-authoring semantics receive independent review

**Reason**: a named reviewer and historical PR acceptance gate do not belong in enduring runtime requirements.

**Migration**: the coordinated cutover uses the repository's normal mandatory implementation review gate.
