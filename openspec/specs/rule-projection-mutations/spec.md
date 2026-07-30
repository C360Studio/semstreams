# rule-projection-mutations Specification

## Purpose

`rule-projection-mutations` governs **what a rule pack is allowed to write, and how that permission
is established**. Rule packs bind one public mutation client and bind it *fail closed*; the complete
pack set is preflighted before any binding, so a pack that cannot be satisfied is refused up front
rather than discovered mid-run. Replacement targets are exact and immutable, replace-owned actions
reconcile complete selected groups rather than partial ones, and the mutation envelopes a pack
derives are static across hot reload.

The through-line is that a rule's write authority is **derived statically from its frozen initial
actions**, not inferred at runtime from whatever it happens to do — entity target scope is inferred
only from statically provable inputs, and the raw replacement transport that bypassed this is
retired.

**What it does NOT cover.** Condition evaluation belongs to `rule-engine`. The mutation verbs
themselves and their merge semantics belong to `graph-ingest`. Built-in-owned rule consumption is
explicitly deferred, and that deferral is recorded here as a requirement rather than left implicit.
## Requirements
### Requirement: Rule packs bind one public mutation client fail closed

Before component start, the composition root MUST bind exactly one complete immutable public projection mutation
client for every enabled rule pack with a non-empty projection-contract set. It MUST use owner
`rule-pack.<packID>`, the runtime NATS client, and the pack's complete copied projection-contract set. The runtime
NATS client MUST be non-nil for every such client.

The ownership Registry MUST be non-nil only when the complete contract set derives a non-empty
`ownership.Registration` containing owner claims or foreign-edge claims. The static-owner heartbeater MUST be
non-nil only when an owning `replace-owned` or `cas-transition` claim requires liveness. A claimless/birth-only
contract set MUST be allowed to bind with nil Registry and nil heartbeater. A non-empty registration with no owning
claim MUST be allowed to bind with a nil heartbeater. Every derived claim MUST be disjoint from other packs and
from claims already registered by the #696 built-in aggregate.

The resulting client MUST be injected into the processor only as `projection.OwnedReplacer`. The rule-pack path
MUST NOT separately call `BindAndHeartbeat`, mint or expose an owner token, or retain a raw replacement publisher.

Invalid dependencies, contract validation, ownership overlap, binding failure, heartbeat failure, and injection
failure MUST abort boot before `StartAll`. They MUST NOT be downgraded to observe-only warnings.

The Registry-wide one-successful-registration invariant MUST apply when the complete contract set produces a
non-empty `ownership.Registration` with owner or foreign claims. A repeated helper invocation for that registered
`rule-pack.<packID>` MUST preserve `ErrOwnerAlreadyBound` and fail boot before heartbeat or claim mutation.

A contract-bearing pack with an empty registration, including a birth-only pack, MUST NOT promise
`ErrOwnerAlreadyBound`. Its repeated composition MUST fail through the one-time `SetOwnedReplacer`
client-injection boundary as a composition error. Repeated composition with identical contract-bearing inputs MUST
NOT succeed idempotently on either path.

#### Scenario: Two packs bind independently

- **GIVEN** two enabled rule packs pass preflight
- **WHEN** rule-pack mutation capabilities are composed
- **THEN** `BindMutationClient` is called exactly once for each pack
- **AND** each call uses that pack's owner and complete contract set
- **AND** each processor receives only its own `OwnedReplacer`

#### Scenario: Binding fails

- **WHEN** a client bind reports overlap, liveness, registry, or transport dependency failure
- **THEN** the original typed or classified cause is returned from composition
- **AND** `StartAll` is not called
- **AND** no rule action can publish a mutation

#### Scenario: Claimless client has nil ownership dependencies

- **GIVEN** a non-empty birth-only contract set derives an empty ownership registration
- **WHEN** the rule pack binds with a runtime NATS client and nil Registry and heartbeater
- **THEN** binding succeeds and the processor receives its `OwnedReplacer`

#### Scenario: Registration dependencies follow derived claims

- **GIVEN** a complete contract set derives a non-empty registration
- **WHEN** its Registry is nil
- **THEN** binding fails before client injection or mutation transport
- **AND** a nil heartbeater is allowed unless the registration contains owning replace/CAS claims

#### Scenario: Rule-pack overlaps the built-in aggregate

- **GIVEN** #696 already bound a built-in-owned predicate group
- **WHEN** a rule pack attempts to bind the same ownership cells under `rule-pack.<packID>`
- **THEN** `ErrOwnershipOverlap` remains inspectable and boot fails
- **AND** the conflict is not downgraded to an observe-only warning

#### Scenario: Claim-bearing binder is invoked twice

- **GIVEN** a rule-pack owner registered a non-empty owner or foreign claim set in one Registry
- **WHEN** rule-pack composition is invoked again with the identical pack and contract set
- **THEN** `ErrOwnerAlreadyBound` remains inspectable and boot fails before heartbeat or claim mutation

#### Scenario: Claimless binder is invoked twice

- **GIVEN** a birth-only pack has an empty registration and received its `OwnedReplacer`
- **WHEN** rule-pack composition is invoked again with the identical pack and contract set
- **THEN** one-time `SetOwnedReplacer` injection rejects the repeat as a composition error
- **AND** boot fails without requiring `ErrOwnerAlreadyBound`

#### Scenario: Pack has no contract

- **GIVEN** an enabled pack declares no projection contract
- **WHEN** the pack is composed
- **THEN** no mutation client is bound for that pack
- **AND** any `replace_owned` action is rejected during preflight

### Requirement: The complete pack set is preflighted before binding

The composition root MUST perform all side-effect-free validation for every enabled rule pack, including packs with
no contracts, before the first rule-pack mutation-client bind. Preflight MUST validate pack IDs, duplicate IDs,
complete contract sets, required dependencies, enabled-pack overlaps, exact target indexes, and every initially
configured `replace_owned` action.

The #696 built-in aggregate MUST remain bound earlier and outside this rule-pack preflight batch. Every
claim-bearing rule-pack bind MUST still arbitrate against live Registry claims, and any pack-vs-built-in overlap
MUST fail closed.

No rule-pack ownership registration, rule-pack heartbeat enrollment, rule-pack owner-token minting, rule-pack
client injection, or rule-pack mutation transport MAY occur before preflight succeeds for the complete enabled
rule-pack set.

Preflight and initialization MUST consume the same immutable validated initial-rule snapshot. Initialization MUST
NOT reread rule files into a different mutation target after contracts bind.

#### Scenario: Later pack is invalid

- **GIVEN** the first pack is valid and a later enabled pack has an invalid contract or action target
- **WHEN** composition preflight runs
- **THEN** composition fails before the first mutation-client bind
- **AND** no processor receives a replacer

#### Scenario: Duplicate pack identity

- **WHEN** two enabled processors declare the same pack ID
- **THEN** preflight returns a composition error before any binding side effect

#### Scenario: Two packs overlap during preflight

- **WHEN** two enabled rule packs claim the same owned cell
- **THEN** preflight fails before the first rule-pack bind
- **AND** neither pack receives a mutation client

#### Scenario: Rule file changes after preflight

- **GIVEN** preflight has retained a validated initial-rule snapshot
- **WHEN** the source file changes before processor start
- **THEN** initialization consumes the validated snapshot
- **AND** it does not activate an unbound replacement target

#### Scenario: Registry changes after preflight

- **GIVEN** all enabled packs pass side-effect-free preflight
- **WHEN** an external registry change causes a later bind to fail
- **THEN** boot fails and no processor starts
- **AND** the framework does not claim transactional rollback that the ownership registry does not provide

### Requirement: Replacement targets are exact and immutable

The processor MUST build one immutable target index from the copied boot-time projection contracts. Each target
MUST resolve a case-sensitive contract name, a case-sensitive named `replace-owned` group, an exact literal
predicate in that group, and the group's complete predicate set.

Every `replace_owned` action MUST provide non-empty `projection_contract` and `projection_group` selectors. Its
predicate MUST be literal and MUST belong to the selected group. The processor MUST reject missing, unnamed,
unknown, wrong-mode, out-of-contract, or ambiguous targets during initial load or hot reload.

The processor MUST NOT use the public client's omitted-group compatibility to infer an action target.

#### Scenario: Explicit target resolves

- **GIVEN** an action names an existing contract and named replace-owned group
- **AND** its literal predicate is an exact member of that group
- **WHEN** the rule is loaded
- **THEN** the action resolves to one immutable target

#### Scenario: Multi-predicate group omits its selector

- **GIVEN** a contract has a replace-owned group containing multiple predicates
- **WHEN** an action omits `projection_group`
- **THEN** rule loading fails
- **AND** execution does not fall back to one-predicate patch behavior

#### Scenario: Single-predicate group omits selectors

- **GIVEN** the action's predicate would otherwise identify one single-predicate group
- **WHEN** either explicit selector is omitted
- **THEN** rule loading still fails

#### Scenario: Selected predicate is outside the group

- **WHEN** an action names a valid contract and group but its predicate is not in that group
- **THEN** rule loading fails before mutation transport

### Requirement: Replace-owned actions reconcile complete selected groups

The executor MUST issue one `projection.ReplaceOwnedMutation` through the injected `OwnedReplacer`. It MUST name
the resolved contract and group, target the resolved entity ID, and provide the action's complete desired state.

An omitted or raw-empty `Action.Object` MUST produce an empty desired set. A raw-non-empty `Action.Object` MUST
produce one desired triple for the action predicate, even when substitution resolves the object to an empty value.
Every predicate in the selected group that is omitted from desired state MUST be removed. Predicates in sibling
groups, birth predicates, append-only predicates, foreign predicates, and unrelated facts MUST remain untouched.

Birth predicates MUST be treated as create-only authorization through the public client. They MUST derive no
ownership claim and MUST NOT enter a replacement removal set. The rule layer MUST NOT represent them as
graph-enforced immutable facts; another accepted, nonconforming write lane MAY change or remove them.

The executor MUST NOT read current state to restore omitted selected-group siblings. It MUST NOT provide an
arbitrary removal list or expected revision.

#### Scenario: Omitted siblings clear

- **GIVEN** group `lifecycle` contains `status`, `superseded-by`, and `retired-at`
- **WHEN** an action desires only `status=retired`
- **THEN** the replacement desires that status triple
- **AND** existing `superseded-by` and `retired-at` facts are removed

#### Scenario: Omitted or raw-empty object clears the group

- **WHEN** a replacement action omits `Action.Object` or supplies it as raw-empty
- **THEN** it sends an empty desired set for the selected group
- **AND** every existing predicate in that group is removed

#### Scenario: Non-empty object resolves empty

- **GIVEN** a replacement action has a raw-non-empty `Action.Object`
- **WHEN** substitution resolves that object to an empty value
- **THEN** desired state contains one triple for the action predicate with that empty resolved object
- **AND** the operation is not treated as a complete-group clear

#### Scenario: Sibling group remains isolated

- **GIVEN** the same contract contains another replace-owned group
- **WHEN** the selected group is replaced
- **THEN** no predicate in the sibling group is removed, added, or included in verification

#### Scenario: Create-only birth predicate remains outside replacement

- **GIVEN** the entity contains a predicate declared only in `BirthPredicates`
- **WHEN** a rule replaces one selected owned group
- **THEN** the birth predicate is excluded from desired and removal sets and remains untouched by that operation
- **AND** no graph-wide immutability guarantee is inferred

#### Scenario: Typed desired value

- **WHEN** object substitution resolves a numeric, boolean, structured, or string value
- **THEN** the desired triple preserves that canonical typed object value

### Requirement: Rule replacement preserves receipt and error semantics

On a successful replacement, the executor MUST pass a non-zero `MutationReceipt.KVRevision` to the existing
per-rule feedback-loop revision tracker.

On failure, the executor MUST preserve the original `projection.MutationError`, mutation kind, classified code and
class, commit state, and unwrap chain. Contextual wrapping MUST use `%w`.

The rule layer MUST NOT flatten every failure to a generic transient or network error. It MUST NOT blindly retry a
replacement. Retry and authoritative verification MUST remain the public mutation client's responsibility.

#### Scenario: Revision is tracked

- **WHEN** `ReplaceOwned` returns a successful receipt with a non-zero KV revision
- **THEN** the executor records that revision for the firing rule and entity

#### Scenario: Stale token remains typed

- **WHEN** the public client returns a stale-owner-token mutation error
- **THEN** `errors.As` still finds the same `*projection.MutationError`
- **AND** its kind, code, class, not-committed state, and underlying classified cause remain inspectable
- **AND** the rule layer does not retry

#### Scenario: Target entity is not found

- **WHEN** the public client returns a not-found mutation error
- **THEN** the rule action preserves its kind, code, not-committed state, and unwrap chain
- **AND** the rule layer does not auto-vivify the entity

#### Scenario: Commit outcome is uncertain

- **WHEN** the public client returns commit-unknown or committed-unverified
- **THEN** the action error preserves that distinct commit outcome
- **AND** the rule layer does not convert it to a retryable not-committed failure

### Requirement: Rule-pack mutation envelopes are static across hot reload

Pack ID, projection contracts, mutation-client identity, and the exact target index MUST be immutable after boot.
Hot reload MUST validate replacement actions against the frozen index and apply a rule update atomically only when
all resulting targets are valid.

Hot reload MUST NOT add, remove, or change a projection contract, rebind an owner, start a heartbeater, replace the
mutation client, or alter a target group's predicate set.

Both `cmd/semstreams` and `cmd/e2e-semstreams` MUST retain the same existing `BindRulePackContracts` call before
`StartAll`. No call-site change is required unless the helper signature changes.

#### Scenario: In-envelope rule reload

- **WHEN** a hot-reloaded rule names a target present in the frozen index
- **THEN** the rule update may be applied without rebinding

#### Scenario: Reload changes a contract

- **WHEN** a runtime update changes pack ID, contracts, group membership, or a replacement selector outside the
  frozen index
- **THEN** the entire update is rejected
- **AND** the previous rules and mutation binding remain active

### Requirement: The raw rule replacement transport is retired

After the public-client migration, `TripleMutator` MUST NOT expose `ReplaceOwned`, and the rule package MUST NOT
construct raw update-with-triples replacement requests or retain a rule-local update-with-triples subject.

Processor and action-executor state MUST NOT carry projection owner tokens or token setters. Raw `AddTriple` and
`RemoveTriple` MAY remain temporarily and MUST remain tracked as unfinished work in #688.

#### Scenario: Replacement deletion audit

- **WHEN** the migration is ready for review
- **THEN** a production-code audit finds no `TripleMutator.ReplaceOwned`, raw replacement request construction,
  `projectionOwnerToken`, or `SetProjectionOwnerToken`
- **AND** action replacement tests exercise `projection.OwnedReplacer`

#### Scenario: Add and remove remain

- **WHEN** the replacement migration is merged
- **THEN** existing raw Add/Remove behavior remains unchanged
- **AND** #688 remains open for its later bounded retirement

### Requirement: Built-in-owned rule consumption remains deferred

PR2 MUST NOT migrate the lesson lifecycle reference pack. PR #696 already binds
`agentic.lesson-record` / `lesson-lifecycle` claims under built-in owner `agentic-loop-graph-writer`; attempting the
same claims under `rule-pack.lesson-lifecycle` MUST fail with `ErrOwnershipOverlap`.

PR2 MUST NOT add a coordination waiver, downgrade that overlap, implicitly borrow the #696 client, or introduce an
unreviewed prebound-client API. A separate least-privilege design for rules consuming built-in-owned groups MUST
remain under #688/shared follow-up.

#### Scenario: Lesson reference pack is evaluated for PR2

- **GIVEN** the #696 built-in owner already holds the lesson lifecycle group
- **WHEN** PR2 scope is assembled
- **THEN** lesson config, README, and reference-pack migration changes are absent
- **AND** built-in-owned rule consumption remains linked to #688/shared follow-up

#### Scenario: Lesson rule-pack bind is attempted

- **WHEN** `rule-pack.lesson-lifecycle` attempts to claim the built-in-owned lesson lifecycle cells
- **THEN** composition returns `ErrOwnershipOverlap` and fails boot
- **AND** no lesson rule processor starts

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

