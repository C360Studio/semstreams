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

### 1. Bind one complete immutable mutation client per disjoint rule-pack owner

`BindRulePackContracts` remains the single composition-root operation before `Manager.StartAll`. For every enabled
rule pack with a non-empty projection-contract set, it constructs exactly one `projection.MutationClientConfig`
with:

- the manager's runtime NATS client;
- the ownership registry and static-owner heartbeater;
- owner `rule-pack.<packID>`;
- the pack's complete copied projection-contract set.

It calls `projection.BindMutationClient` exactly once for that pack and injects the returned client only as
`projection.OwnedReplacer`. It does not call `BindAndHeartbeat`, `Registry.OwnerToken`, or any token setter. The
pack's contracts must be disjoint from every other pack and from claims already bound by the #696 built-in owner
`agentic-loop-graph-writer`.

The Registry-wide invariant permits one successful registration for an owner and Registry. The first successful
bind consumes `rule-pack.<packID>` for that Registry lifetime. A concurrent or repeated invocation fails with
`ownership.ErrOwnerAlreadyBound` before heartbeat or claim mutation, even when it supplies the identical complete
contract set. Correction after a successful bind requires a new Registry/incarnation; this helper never rebinds.

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
contract set. It is still preflighted and can run actions that do not require owned replacement, while any
`replace_owned` action fails preflight because no target exists. This preserves exactly one client per pack that
has a mutation envelope and does not invent a rule-local no-op replacer.

### 2. Preflight every rule pack before the first rule-pack bind

`BindRulePackContracts` performs every side-effect-free check for all enabled packs before the first call to
`BindMutationClient`:

1. validate every pack ID and reject duplicate IDs;
2. copy and validate every complete contract set;
3. build each processor's exact action-target index;
4. validate all initially loaded and inline `replace_owned` actions against that index;
5. reject duplicate or ambiguous contract/group/predicate targets;
6. reject a missing NATS client or any contract-required registry or heartbeater;
7. detect overlaps among the enabled pack contract sets.

The #696 built-in aggregate intentionally binds before `BindRulePackContracts`; it is not part of this rule-pack
preflight batch. Preflight detects pack-pack conflicts before rule-pack binding. Each later client bind arbitrates
against the live Registry, so a pack-vs-built-in overlap, a stale external claim, or another conflict introduced
between preflight and bind fails at that bind.

This prevents avoidable partial composition. Any Registry conflict aborts boot, no processor starts, and no
mutation is published. The design does not invent rollback for an ownership registry that does not provide
transactional multi-owner registration. If an earlier pack registered before a later bind failed, process boot
still fails; the process must discard that Registry rather than continue with a partial rule-pack composition.

Binding and overlap errors are never observe-only warnings. This includes `ErrOwnershipOverlap`,
`ErrOwnerAlreadyBound`, missing dependencies, heartbeat/liveness failure, and client injection failure. The
original typed or classified cause is wrapped with `%w` and returned to the binary composition root.

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

Both `cmd/semstreams` and `cmd/e2e-semstreams` already make the same `BindRulePackContracts` call before
`StartAll`. PR2 verifies those call sites and does not churn them unless the helper signature changes.

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

Birth predicates are create-only authorization through the public client. They derive no owner claim, are not part
of a replacement removal set, and remain untouched by conforming rule replacement. They are not graph-enforced
immutable facts: another accepted, nonconforming write lane can still change or remove them.

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

### 8. Defer built-in-owned rule consumption

PR #696 already binds contract `agentic.lesson-record` and group `lesson-lifecycle` under built-in owner
`agentic-loop-graph-writer`. Binding the lesson reference pack as owner `rule-pack.lesson-lifecycle` would claim the
same cells and must fail with `ErrOwnershipOverlap`.

PR2 therefore removes the lesson reference-pack migration from its diff. It does not change the lesson contract,
config, README, or lifecycle action as a demonstration of rule-pack binding. The overlap is a design boundary, not
a coordination waiver or observe-only exception.

A rule that legitimately consumes a built-in-owned group needs a separate design for receiving an already-bound,
least-privilege client without registering the same claims under another owner. That prebound-client/shared
capability design remains under #688 and its shared follow-up. PR2 neither invents that API nor borrows the #696
client implicitly.

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
- **Built-in overlap:** keep lesson/built-in-owned consumption out of PR2 and test pack-vs-built-in conflict as a
  fatal boot error.
- **Repeated composition:** rely on the Registry-wide one-bind invariant and preserve `ErrOwnerAlreadyBound`.
- **Reload drift:** freeze pack identity, contracts, client, and target index; reject attempted changes atomically.
- **Error semantic loss:** assert `errors.As`, `errors.Is`, code, class, kind, and commit state at the action boundary.
- **Binary drift:** verify both existing identical helper calls remain aligned;
  do not churn either main unless the helper signature changes.

## Architecture Sign-Off Gates

Implementation is conformant only when:

1. exactly one public mutation client is bound per enabled contract-bearing pack before `StartAll`;
2. no rule-owned owner token or raw ReplaceOwned transport path remains;
3. every action selects one explicit contract and named group;
4. selected-group omission clears siblings and never touches another group;
5. binding and overlap failures fail closed;
6. hot reload cannot change or rebind the static mutation envelope;
7. typed commit-aware errors and receipt revisions cross the action boundary intact;
8. lesson/built-in-owned rule consumption is absent and remains deferred to a prebound-client design under #688;
9. both binaries retain the same existing helper call without unnecessary churn;
10. raw Add/Remove remain explicitly deferred under #688;
11. Fable review is requested only if implementation exposes a public-contract/framework issue or materially
    expands that reviewed surface.
