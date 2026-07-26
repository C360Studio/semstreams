# Design: Rule Contract-Bound Replace-Owned

## Context

The public projection mutation client now owns contract validation, owner-token fencing, bounded retry,
authoritative verification, and commit-aware outcomes. The rule engine still duplicates the narrower predecessor:

- `service/rule_pack_bind.go` calls `BindAndHeartbeat`, mints `OwnerToken`, and duck-types token setters;
- `Processor` and `ActionExecutor` carry owner identity and token state;
- `TripleMutator.ReplaceOwned` constructs and publishes raw `UpdateEntityWithTriplesRequest` values;
- action validation checks only whether a literal predicate appears in any replace-owned group;
- execution removes only that predicate, even if its declared group contains atomic sibling predicates;
- error wrapping can discard the public mutation taxonomy and commit outcome.

The migration must replace that path, not layer the public client underneath the existing token and raw transport
abstractions.

## Decision

### 1. Bind one immutable mutation client per contract-bearing rule pack

`BindRulePackContracts` remains the single composition-root operation before `Manager.StartAll`. For every enabled
rule pack with a non-empty projection-contract set, it constructs exactly one `projection.MutationClientConfig`
with:

- the manager's runtime NATS client;
- the ownership registry and static-owner heartbeater;
- owner `rule-pack.<packID>`;
- the pack's complete copied projection-contract set.

It calls `projection.BindMutationClient` exactly once for that pack and injects the returned client only as
`projection.OwnedReplacer`. It does not call `BindAndHeartbeat`, `Registry.OwnerToken`, or any token setter.

The service-level duck-typed composition interface becomes:

```go
type ProjectionBinder interface {
	ProjectionBindings() (packID string, contracts []projection.Contract)
	PreflightProjectionMutations() error
	SetOwnedReplacer(projection.OwnedReplacer) error
}
```

`PreflightProjectionMutations` is a local, repeat-safe preparation step with no NATS, registry, heartbeat, or
mutation side effects. `SetOwnedReplacer` is a one-time injection that must finish before processor start. A nil
client for a contract-bearing pack, unavailable NATS dependency, failed injection, or a second inconsistent
injection is a composition error.

A pack with no contracts receives no mutation client because the public constructor intentionally rejects an empty
contract set. It can run actions that do not require owned replacement, while any `replace_owned` action fails
preflight because no target exists. This preserves exactly one client per pack that has a mutation envelope and
does not invent a rule-local no-op replacer.

### 2. Preflight the whole composition before the first bind

`BindRulePackContracts` performs every side-effect-free check for all enabled packs before the first call to
`BindMutationClient`:

1. validate every pack ID and reject duplicate IDs;
2. copy and validate every complete contract set;
3. build each processor's exact action-target index;
4. validate all initially loaded and inline `replace_owned` actions against that index;
5. reject duplicate or ambiguous contract/group/predicate targets;
6. reject a missing NATS client or any contract-required registry or heartbeater;
7. detect overlaps among the enabled pack contract sets.

This prevents avoidable partial composition. A registry conflict introduced externally between preflight and bind
can still fail a later bind; that error aborts boot, no processor starts, and no mutation is published. The design
does not invent rollback for an ownership registry that does not provide transactional multi-owner registration.

Binding and overlap errors are never observe-only warnings. The original typed or classified cause is wrapped with
`%w` and returned to the binary composition root.

The preflight parser and the start-time rule loader must share one implementation. Preflight retains an immutable
validated initial-rule snapshot, and initialization consumes that snapshot instead of rereading rule files. This
closes the gap where a file could validate before binding and change before `StartAll`. Hot reload remains a
separate post-start path validated against the same frozen target index.

### 3. Keep pack identity and contracts static

`Config.PackID` and `Config.ProjectionContracts` are boot-time composition inputs. The processor copies them and
builds the target index once before start. The index and injected replacer are immutable after successful
composition.

Runtime configuration updates may add, remove, or change rule definitions only when every resulting
`replace_owned` action validates against the frozen index. A hot-reload payload that changes `pack_id`,
`projection_contracts`, target group membership, or mutation-client identity is rejected atomically. Hot reload
never calls the service binder, ownership registry, heartbeater, or mutation-client constructor.

### 4. Resolve an exact contract, named group, and predicate

`Action` adds two rule-authoring fields for `replace_owned`:

```go
ProjectionContract string `json:"projection_contract,omitempty"`
ProjectionGroup    string `json:"projection_group,omitempty"`
```

Every `replace_owned` action must provide both fields. `projection_contract` resolves exactly one contract by its
case-sensitive name. `projection_group` resolves exactly one named `replace-owned` group in that contract. The
literal `predicate` must be an exact member of that group.

The processor builds an immutable target index equivalent to:

```text
contract name -> group name -> literal predicate -> {
  contract name,
  group name,
  complete group predicate set
}
```

Contract names, group names, and exact predicates must be unique enough to produce one target. Missing, unnamed,
wrong-mode, out-of-contract, or ambiguous targets fail during initial load or hot reload. Predicate substitution
remains forbidden.

Explicit naming is required even for a single-predicate or otherwise unambiguous group. This makes action intent
stable when contracts later gain groups and prevents rule execution from depending on the public client's optional
single-group selector compatibility. A multi-predicate group without an explicit selector is therefore a hard
authoring error, never a one-predicate patch.

### 5. Make selected-group replacement complete

At execution, the action resolves its prevalidated target and constructs one
`projection.ReplaceOwnedMutation`:

- `Contract` is the selected contract name;
- `Group` is the selected group name;
- `EntityID` is the resolved action subject or trigger entity;
- `Desired` is empty for a clear, otherwise one canonical triple for the action predicate and resolved typed
  object;
- metadata preserves stable rule/action correlation and provenance.

The public client derives the removal set from the complete selected group. Therefore:

- every selected-group predicate omitted from `Desired` is removed;
- a non-empty action writes its one desired predicate and clears every omitted sibling in that group;
- an empty object clears the entire selected group;
- sibling groups, birth predicates, append-only predicates, foreign predicates, and unrelated facts are untouched.

This intentionally changes the old one-predicate patch semantics. Rule authors who need independent preservation
must declare independent named groups. The engine must not read current state to synthesize omitted siblings,
because doing so would restore patch behavior and race the authoritative writer.

Typed substitution remains unchanged: resolved numeric, boolean, structured, and string objects are carried as the
canonical `message.Triple.Object` value. Replace-owned remains non-CAS; the action exposes no expected revision.

### 6. Preserve receipts and typed failures

On success, the executor passes `MutationReceipt.KVRevision` to the existing per-rule revision tracker when the
revision is non-zero. This preserves feedback-loop suppression without keeping raw mutation transport in the rule
package.

On failure, the action returns an error that wraps the original cause with `%w`. It must preserve:

- `*projection.MutationError`;
- `MutationErrorKind`;
- `MutationError.Code`;
- `MutationError.Class`;
- `MutationError.Commit`;
- the underlying classified or sentinel cause exposed by `Unwrap`.

The executor does not map all failures to transient/network errors, discard a receipt's commit state, or add a
blind caller-level retry. Retry and authoritative verification remain owned by `MutationClient.ReplaceOwned`.
In particular, stale owner tokens and committed-unverified or commit-unknown outcomes retain their distinct
operator meaning.

### 7. Delete the duplicate replacement lane

After all production callers use `projection.OwnedReplacer`:

- remove `Processor.projectionOwnerToken` and `Processor.SetProjectionOwnerToken`;
- remove `ActionExecutor.ownerToken` and `ActionExecutor.SetProjectionOwnerToken`;
- remove `ActionExecutor.ownerID` and `ActionExecutor.SetProjectionOwner` if the caller audit shows no other use;
- remove owner-token minting and pass-through from `service/rule_pack_bind.go`;
- remove `ReplaceOwned` from `TripleMutator`;
- delete `tripleMutator.ReplaceOwned`;
- delete the rule-local `SubjectEntityUpdateWithTriples` constant and raw update request/response handling.

`TripleMutator` retains only the raw Add/Remove capabilities still used by rule actions. Their retirement remains
tracked in #688 and must not be smuggled into this PR.

Deletion is gated by an `rg` audit showing zero production callers and by replacement tests covering the public
client path. Token-shape tests and raw transport tests are deleted or rewritten only after their assertions are
represented at the new boundary.

### 8. Migrate the lesson lifecycle reference pack exactly

`configs/rules/lessons/lesson-lifecycle-rulepack.json` keeps:

- contract `agentic.lesson-record`;
- message type `agentic.agent_lesson.v1`;
- entity pattern `*.*.agent.lesson.record.*`.

It names one `replace-owned` group `lesson-lifecycle` containing exactly:

- `agent.lesson.status`;
- `agent.lesson.superseded-by`;
- `agent.lesson.retired-at`.

It declares these exact immutable birth predicates:

- `agent.lesson.category`;
- `agent.lesson.polarity`;
- `agent.lesson.severity`;
- `agent.lesson.created-at`;
- `agent.lesson.summary`;
- `agent.lesson.detail`;
- `agent.lesson.injection-form`;
- `agent.lesson.evidence`;
- `agent.lesson.applies-to`;
- `agent.lesson.observed-role`;
- `agent.action.executed-by`.

The illustrative birth-time status action names `agentic.lesson-record` and `lesson-lifecycle`. Because the lesson
is newly born and has no lifecycle siblings yet, the example keeps its intended output while demonstrating
complete-group semantics.

The adjacent README and generated schema/config round-trip evidence are updated with the explicit selectors and
delete-on-omission warning.

## Alternatives Considered

### Keep raw `TripleMutator.ReplaceOwned` behind the public client

Rejected. It would preserve two replacement abstractions, owner-token plumbing, and unclear retry ownership.

### Infer the contract and group from the predicate

Rejected. The inference becomes ambiguous as contracts grow, and it silently changes meaning when another group
uses the same predicate under a different entity envelope.

### Preserve omitted sibling predicates by reading current state

Rejected. This recreates patch semantics, introduces a read-modify-write race, and defeats the declared atomic
group boundary.

### Bind lazily on first action or hot reload

Rejected. Mutation authorization is a static composition invariant. Lazy binding permits partially started packs
and makes overlap failure runtime-dependent.

### Remove Add/Remove in the same PR

Rejected. Their consumers and semantic envelopes need a separate bounded migration. #688 remains open for that
work.

## Risks and Mitigations

- **Configuration breakage:** strict selectors reject existing actions. Migrate every in-repository action and add
  load/hot-reload contract tests before deleting fallback code.
- **Unexpected sibling deletion:** document complete-group behavior and test delete-on-omission and group isolation.
- **Partial binding:** preflight every pack first and fail boot on any later bind error.
- **Reload drift:** freeze pack identity, contracts, client, and target index; reject attempted changes atomically.
- **Error semantic loss:** assert `errors.As`, `errors.Is`, code, class, kind, and commit state at the action boundary.
- **Half-migrated binaries:** update and test both `cmd/semstreams` and `cmd/e2e-semstreams` composition call sites.

## Architecture Sign-Off Gates

Implementation is conformant only when:

1. exactly one public mutation client is bound per enabled contract-bearing pack before `StartAll`;
2. no rule-owned owner token or raw ReplaceOwned transport path remains;
3. every action selects one explicit contract and named group;
4. selected-group omission clears siblings and never touches another group;
5. binding and overlap failures fail closed;
6. hot reload cannot change or rebind the static mutation envelope;
7. typed commit-aware errors and receipt revisions cross the action boundary intact;
8. the lesson contract and action match the exact predicates and selectors above;
9. raw Add/Remove remain explicitly deferred under #688.
