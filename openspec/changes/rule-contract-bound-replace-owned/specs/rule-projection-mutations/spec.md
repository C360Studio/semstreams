# Rule Projection Mutations Specification

## ADDED Requirements

### Requirement: Rule packs bind one public mutation client fail closed

Before component start, the composition root MUST bind exactly one public projection mutation client for every
enabled rule pack with a non-empty projection-contract set. It MUST use owner `rule-pack.<packID>`, the runtime NATS
client, the ownership registry, the static-owner heartbeater, and the pack's complete projection-contract set.

The resulting client MUST be injected into the processor only as `projection.OwnedReplacer`. The rule-pack path
MUST NOT separately call `BindAndHeartbeat`, mint or expose an owner token, or retain a raw replacement publisher.

Invalid dependencies, contract validation, ownership overlap, binding failure, heartbeat failure, and injection
failure MUST abort boot before `StartAll`. They MUST NOT be downgraded to observe-only warnings.

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

#### Scenario: Pack has no contract

- **GIVEN** an enabled pack declares no projection contract
- **WHEN** the pack is composed
- **THEN** no mutation client is bound for that pack
- **AND** any `replace_owned` action is rejected during preflight

### Requirement: The complete pack set is preflighted before binding

The composition root MUST perform all side-effect-free validation for every enabled pack before the first mutation
client bind. Preflight MUST validate pack IDs, duplicate IDs, complete contract sets, required dependencies,
enabled-pack overlaps, exact target indexes, and every initially configured `replace_owned` action.

No ownership registration, heartbeat enrollment, owner-token minting, client injection, or mutation transport MAY
occur before preflight succeeds for the complete enabled set.

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

A non-empty action object MUST produce one desired triple for the action predicate. An empty object MUST produce an
empty desired set. Every predicate in the selected group that is omitted from desired state MUST be removed.
Predicates in sibling groups, birth predicates, append-only predicates, foreign predicates, and unrelated facts
MUST remain untouched.

The executor MUST NOT read current state to restore omitted selected-group siblings. It MUST NOT provide an
arbitrary removal list or expected revision.

#### Scenario: Omitted siblings clear

- **GIVEN** group `lifecycle` contains `status`, `superseded-by`, and `retired-at`
- **WHEN** an action desires only `status=retired`
- **THEN** the replacement desires that status triple
- **AND** existing `superseded-by` and `retired-at` facts are removed

#### Scenario: Empty object clears the group

- **WHEN** a replacement action resolves an empty object
- **THEN** it sends an empty desired set for the selected group
- **AND** every existing predicate in that group is removed

#### Scenario: Sibling group remains isolated

- **GIVEN** the same contract contains another replace-owned group
- **WHEN** the selected group is replaced
- **THEN** no predicate in the sibling group is removed, added, or included in verification

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

### Requirement: Lesson lifecycle is the exact reference migration

The lesson lifecycle reference contract MUST be named `agentic.lesson-record`, use message type
`agentic.agent_lesson.v1`, and match entity pattern `*.*.agent.lesson.record.*`.

It MUST define named replace-owned group `lesson-lifecycle` with exactly `agent.lesson.status`,
`agent.lesson.superseded-by`, and `agent.lesson.retired-at`.

It MUST declare exactly these birth predicates: `agent.lesson.category`, `agent.lesson.polarity`,
`agent.lesson.severity`, `agent.lesson.created-at`, `agent.lesson.summary`, `agent.lesson.detail`,
`agent.lesson.injection-form`, `agent.lesson.evidence`, `agent.lesson.applies-to`,
`agent.lesson.observed-role`, and `agent.action.executed-by`.

Its illustrative birth-time replacement action MUST name contract `agentic.lesson-record` and group
`lesson-lifecycle`.

#### Scenario: Birth-time status migration

- **GIVEN** a newly born proposed lesson has no superseded-by or retired-at facts
- **WHEN** the illustrative rule replaces status with `retired`
- **THEN** it selects `agentic.lesson-record` and `lesson-lifecycle`
- **AND** the lesson's exact birth predicates remain untouched
- **AND** the output matches the intended prior birth-time result
