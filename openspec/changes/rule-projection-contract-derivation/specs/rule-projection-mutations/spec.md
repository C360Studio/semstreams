# Rule Projection Mutations Specification

## ADDED Requirements

### Requirement: Frozen initial actions derive rule projection contracts

Before the first rule-pack bind, the processor MUST derive a deterministic minimal effective contract set from
every `replace_owned` action in the same frozen initial rule snapshot that start will consume. Derivation MUST scan
enabled and disabled definitions and all `on_enter`, `on_exit`, `while_true`, `on_recovery`, and cron `actions`
collections.

Each participating action MUST contribute its explicit `projection_contract`, explicit `projection_group`, exact
literal `predicate`, and `replace-owned` mode. Predicates with the same contract and group MUST be unioned into one
group. Contract, group, and predicate order MUST be deterministic.

Raw `add_triple`, `remove_triple`, and `update_triple` actions MUST NOT derive projection groups until a separate
contract-bound design assigns their ownership and mutation semantics.

The effective contracts produced by successful preflight MUST be the exact contracts supplied to aggregate
overlap validation and `BindMutationClient`. Start MUST consume the same frozen rule snapshot; derivation MUST NOT
reread rule files or run after binding.

#### Scenario: Common entity-scoped pack omits explicit contracts

- **GIVEN** a pack has no `projection_contracts`
- **AND** all initial `replace_owned` actions use explicit contract/group selectors and statically provable targets
- **WHEN** composition preflight runs
- **THEN** it derives the minimal contracts and binds them through the existing public mutation client
- **AND** no authored config is rewritten

#### Scenario: Disabled rule reserves observed authority

- **GIVEN** an initially disabled rule contains a valid `replace_owned` action
- **WHEN** boot derivation runs
- **THEN** that action contributes to the frozen effective contract
- **AND** later enabling the same in-envelope rule does not require rebinding

#### Scenario: Rule file changes after preflight

- **WHEN** the initial rule files change after successful derivation but before start
- **THEN** start consumes the frozen preflight snapshot
- **AND** neither rules nor effective contracts are rederived from the changed files

### Requirement: Entity target scope is inferred only from statically provable inputs

An omitted action subject or exact `$entity.id` subject MUST derive the enclosing rule's valid non-empty
`entity.pattern`. A canonical literal entity ID subject MUST derive that exact ID as its pattern.

An omitted subject on a message-path or cron rule without `entity.pattern`, and every other dynamic or templated
subject, MUST be classified as unresolved. The framework MUST NOT infer match-all, inspect conditions or payload
schemas, or synthesize a wildcard cover for unresolved targets.

A non-empty subject containing no template token MUST be a canonical literal entity ID. A malformed static value
or wildcard pattern MUST fail authoring validation rather than be treated as an unresolved target.

Without an explicit declared contract covering its contract/group/predicate, an unresolved target MUST fail
preflight. With a covering declaration, the resolved runtime entity ID MUST still be validated by the existing
mutation client before transport.

When one derived contract receives multiple statically provable patterns, omission of an explicit contract MUST be
accepted only if those patterns are identical. Otherwise preflight MUST require one explicit covering pattern or
distinct contract names.

#### Scenario: Trigger entity pattern is provable

- **GIVEN** an entity-scoped rule has `entity.pattern: acme.ops.robotics.gcs.drone.*`
- **AND** its `replace_owned` action omits subject or uses exactly `$entity.id`
- **WHEN** contracts derive
- **THEN** the action contributes `acme.ops.robotics.gcs.drone.*` as its target pattern

#### Scenario: Literal target is provable

- **WHEN** a participating action names one canonical literal six-part entity ID as subject
- **THEN** that exact ID becomes its derived target pattern

#### Scenario: Static subject is not a runtime entity ID

- **WHEN** a participating action subject is a malformed literal or contains a wildcard
- **THEN** preflight fails authoring validation
- **AND** an explicit contract cannot convert that invalid runtime target into a dynamic obligation

#### Scenario: Dynamic target has no override

- **GIVEN** a participating action targets `$entity.triple.parent_id`
- **AND** no explicit contract covers the action
- **WHEN** preflight runs
- **THEN** boot fails before Registry, presence, heartbeat, injection, or mutation side effects
- **AND** the target is not converted to a wildcard pattern

#### Scenario: Dynamic target has an explicit envelope

- **GIVEN** a dynamic target references a declared contract/group/predicate
- **WHEN** the declaration passes normal contract and override validation
- **THEN** boot may bind the declared envelope
- **AND** any runtime target outside its entity pattern fails client validation before mutation transport

#### Scenario: Multiple patterns would require inferred widening

- **GIVEN** one contract receives two different statically derived target patterns
- **AND** no explicit contract covers both
- **WHEN** preflight runs
- **THEN** boot fails with the conflicting action locations
- **AND** the framework does not manufacture a broader wildcard

### Requirement: Explicit projection contracts are validated supersets

When `projection_contracts` is supplied, it MUST be treated as an explicit authorization override. Every derived
contract MUST have a declared contract with the same name. Every derived group MUST have the same non-empty name
and exact mode in that contract, and every derived predicate MUST be present in that declared group.

Omission MUST select default derivation. An explicitly authored empty array MUST be treated as an empty override,
not as omission, and MUST fail when actions derive any contract obligation.

Every statically derived entity pattern MUST be contained by the declared pattern. For canonical six-position
patterns, derived pattern `D` is contained by declared pattern `A` only when each position of `A` is `*` or equals
the corresponding position of `D`. A literal declared position MUST NOT contain a derived wildcard.

Equality MUST be accepted as a valid superset. A declaration MAY add contracts, groups, predicates, or a wider
entity pattern as an explicit frozen hot-reload envelope. Declared extras MUST pass existing contract, claim,
posture, and overlap validation. Automatic derivation MUST NOT add equivalent extras.

An override MUST fail preflight when it omits a derived predicate/group/contract, changes a derived group's mode,
or narrows a derived target pattern.

#### Scenario: Exact declaration matches

- **WHEN** the explicit contracts equal the derived contracts
- **THEN** preflight accepts them as the effective contracts

#### Scenario: Explicit empty override does not mean omission

- **GIVEN** one or more actions derive contract obligations
- **WHEN** the author supplies `projection_contracts: []`
- **THEN** preflight fails because the override covers none of the obligations
- **AND** the framework does not silently substitute derived contracts

#### Scenario: Explicit hot-reload superset

- **GIVEN** a declaration contains every derived action target
- **AND** it adds a valid predicate or wider entity pattern for later hot reload
- **WHEN** preflight runs
- **THEN** the explicit superset becomes the frozen effective envelope
- **AND** its additional authority remains subject to normal ownership overlap validation

#### Scenario: Declared group is narrower

- **WHEN** an action-derived predicate is absent from its declared group
- **THEN** preflight fails before the first rule-pack bind

#### Scenario: Declared mode differs

- **WHEN** an action derives `replace-owned` but the matching declared group uses another mode
- **THEN** preflight fails instead of changing the action's authority

#### Scenario: Declared entity scope is narrower

- **GIVEN** an action derives `acme.*.robotics.gcs.drone.*`
- **AND** the declaration uses `acme.ops.robotics.gcs.drone.*`
- **WHEN** containment validation runs
- **THEN** preflight fails because the declaration does not cover every possible action target

### Requirement: Non-inferable contract metadata remains explicit

`BirthPredicates`, `ForeignEdges`, `IndexingProfile`, and optional `MessageType` MUST NOT be inferred from rule
actions. When declared, those values MUST be copied into the effective contract and pass existing projection
validation.

An explicit birth-only, append-only, foreign-edge-only, or mixed contract MUST retain its #700 posture. Derivation
MUST NOT add fake owning claims, presence, tokens, or heartbeat requirements.

#### Scenario: Derived contract has no explicit metadata

- **WHEN** a contract is derived without an explicit declaration
- **THEN** birth predicates, foreign edges, indexing profile, and message type remain empty

#### Scenario: Explicit birth and foreign metadata

- **WHEN** a valid declaration adds birth predicates or foreign edges to a derived contract
- **THEN** those fields remain explicit in the effective contract
- **AND** normal contract validation and complete-entry posture selection still apply

#### Scenario: Explicit-only non-owning contract

- **GIVEN** a declaration contains only append and/or foreign-edge claims
- **WHEN** it binds with a nil heartbeater
- **THEN** it remains a persistent zero-token registration with no owner presence or enrollment

### Requirement: Derivation preserves fail-closed composition and mutation semantics

Composition MUST finish every pack's derivation, override validation, target-index validation, dependency
validation, and pack-pack overlap validation before the first rule-pack ownership, presence, heartbeat, or
client-injection side effect.

Any ambiguity, unresolved target without an explicit envelope, invalid declaration, containment failure, or
overlap MUST abort boot. The framework MUST NOT fall back to raw mutation, observe-only binding, match-all
authority, or a partially derived contract.

Every configured, enabled component whose factory name is `rule-processor` MUST either be created and initialized
as a managed projection binder or cause component-manager service construction to fail. A rule-processor factory,
creation, or lifecycle-initialization error MUST NOT be reduced to the ordinary component log-and-continue policy,
because doing so would remove the invalid pack from whole-set binder discovery.

When multiple enabled rule processors fail admission, the returned aggregate MUST be deterministic by configured
instance name and MUST preserve each instance name and wrapped root cause, including rule/action location when
available. Disabled rule-processor entries MUST remain excluded from admission. Creation or initialization
failure for components other than `rule-processor` MUST retain the existing best-effort behavior.

Effective contracts MUST continue through `projection.Derive`, `BindMutationClient`, and `OwnedReplacer`.
Complete selected-group replacement, #700 liveness posture, token fencing, retry, classified error, receipt,
authoritative verification, graph wire, and persisted-state behavior MUST remain unchanged.

#### Scenario: Later pack derivation fails

- **GIVEN** multiple enabled packs
- **WHEN** any pack fails derivation or override validation
- **THEN** no rule-pack client has bound and no processor starts

#### Scenario: Invalid pack fails during component initialization

- **GIVEN** one enabled rule processor is valid
- **AND** another enabled rule processor fails initial rule loading, derivation, or override validation
- **WHEN** the production component-manager service is constructed
- **THEN** service construction fails with the invalid configured instance and wrapped rule/action cause
- **AND** the valid sibling is not bound or started
- **AND** no rule-pack Registry, presence, heartbeat, client-injection, or mutation side effect occurs

#### Scenario: Invalid pack fails in its factory

- **GIVEN** an enabled `rule-processor` has a missing or malformed required pack identity
- **WHEN** its factory rejects component creation
- **THEN** component-manager service construction fails
- **AND** absence from binder discovery is not treated as successful omission

#### Scenario: Multiple admission failures are reproducible

- **GIVEN** multiple configured, enabled rule processors fail factory creation or initialization
- **WHEN** the component-manager creation pass completes
- **THEN** one aggregate error reports every failure in configured-instance-name order
- **AND** each entry retains its wrapped root cause

#### Scenario: Disabled invalid pack remains excluded

- **GIVEN** an invalid `rule-processor` configuration is disabled
- **WHEN** component-manager initialization runs
- **THEN** the disabled entry does not participate in rule-pack admission
- **AND** it does not prevent valid enabled packs from reaching composition preflight

#### Scenario: Ordinary component failure remains isolated

- **GIVEN** an enabled component other than `rule-processor` fails creation or initialization
- **WHEN** component-manager initialization runs
- **THEN** the failure retains the established log-and-continue behavior
- **AND** successfully created siblings remain available

#### Scenario: Derived owning group selects liveness

- **WHEN** derivation produces any `replace-owned` group
- **THEN** the complete effective owner registration requires the existing Registry and heartbeater posture
- **AND** one non-zero token and enrollment cover the complete entry

#### Scenario: No alternate write path

- **WHEN** derivation fails
- **THEN** the pack cannot start or publish a replacement through a raw or legacy path

### Requirement: Derived contracts are boot-time state, not authored configuration

`projection_contracts` MUST retain its existing optional JSON shape. Derived effective contracts MUST NOT be
written into authored `Config.ProjectionContracts`, config KV, rule files, or config marshal output.

Generated schema descriptions MUST explain omission-based derivation, explicit-superset behavior, and
explicit-only metadata without changing the graph mutation wire contract.

Hot reload MUST validate replacement actions against the frozen effective envelope. It MUST NOT derive new
authority, change explicit-only fields, mutate owner posture, or rebind.

#### Scenario: Omitted contracts round trip

- **GIVEN** authored config omits `projection_contracts`
- **WHEN** it is decoded, preflighted, and encoded
- **THEN** the authored field remains omitted
- **AND** the effective runtime contracts remain available only to composition

#### Scenario: Hot reload stays inside minimal derivation

- **GIVEN** boot used minimal derived contracts
- **WHEN** hot reload adds a predicate not observed at boot
- **THEN** the update fails atomically without rebinding

#### Scenario: Hot reload uses explicit reserve

- **GIVEN** boot supplied a declared superset containing a reserved predicate
- **WHEN** hot reload adds an action targeting that predicate
- **THEN** the update may pass target validation without changing the bound contract

### Requirement: Public rule-authoring semantics receive independent review

Implementation acceptance MUST require Fable review because omission and explicit overrides change the public rule
configuration contract. The review covers least-authority derivation, dynamic-target handling, override
containment, schema compatibility, and hot-reload behavior.

#### Scenario: Implementation is otherwise green

- **WHEN** implementation and verification gates pass but mandatory Fable review is unresolved
- **THEN** the change remains unaccepted
