# Rule Contract-Bound Replace-Owned

## Why

The rule engine still owns a bespoke `replace_owned` transport path even though the public projection mutation
client now provides the required framework primitive. The current path mints and passes owner tokens through rule
internals, constructs raw `update_with_triples` requests, derives a one-predicate removal set, and flattens
classified mutation outcomes.

That duplication weakens the framework boundary established by
[#313](https://github.com/C360Studio/semstreams/issues/313) and
[PR #687](https://github.com/C360Studio/semstreams/pull/687). It also gives rule actions patch-like semantics that
conflict with the public client's complete predicate-group reconciliation contract.

This change defines the rule-engine migration tracked by
[#688](https://github.com/C360Studio/semstreams/issues/688). It follows the bounded internal migration in
[PR #696](https://github.com/C360Studio/semstreams/pull/696): the composition root binds the framework client,
the processor receives only `projection.OwnedReplacer`, and rule execution names a contract and atomic group.

## What Changes

- Replace rule-pack `BindAndHeartbeat` and owner-token injection with one complete immutable `MutationClient` per
  disjoint, enabled, contract-bearing owner `rule-pack.<packID>` before `StartAll`.
- Preflight every enabled rule pack, its immutable contracts, and all `replace_owned` action targets before the
  first rule-pack bind. The built-in aggregate from #696 intentionally binds earlier and is an incumbent during
  rule-pack binding.
- Enforce the Registry-wide one-successful-registration invariant when a pack produces non-empty owner or foreign
  claims. Repeated claim-bearing binding returns `ErrOwnerAlreadyBound`; repeated claimless/birth-only composition
  fails through one-time client injection instead. Identical contract-bearing inputs never succeed idempotently.
- Make every bind, heartbeat, dependency, client-injection, pack-pack overlap, and pack-vs-built-in overlap error
  abort boot.
- Require NATS for every contract-bearing client, an ownership Registry only when the complete contract set derives
  a non-empty owner/foreign registration, and a heartbeater only when replace-owned or CAS claims require liveness.
- Inject the narrow `projection.OwnedReplacer` capability into each rule processor.
- Build one immutable exact target index from the pack-level contracts and use it for initial load and hot reload.
- Require every `replace_owned` action to name its projection contract and named `replace-owned` group.
- Reconcile the selected group as complete desired state: omitted sibling predicates clear, while other groups and
  non-group predicates remain untouched.
- Preserve typed object substitution and feed `MutationReceipt.KVRevision` to the existing per-rule feedback-loop
  tracker.
- Preserve `projection.MutationError`, its classified cause, commit state, code, and retry meaning across the action
  boundary.
- Remove rule-owned owner-token state and the raw `TripleMutator.ReplaceOwned` implementation.
- Treat contract birth predicates as create-only authorization through the public client, not graph-enforced
  immutable facts. Replacement preserves them, but a nonconforming writer can still change them.
- Defer the lesson lifecycle reference-pack migration. Its `agentic.lesson-record` / `lesson-lifecycle` claims
  overlap the #696 built-in owner, so PR2 must not bind them again as `rule-pack.lesson-lifecycle`.
- Keep built-in-owned rule consumption under #688/shared follow-up until a separate prebound-client authorization
  design defines how a rule may use an already-bound built-in capability without registering the owner again.
- Keep raw `AddTriple` and `RemoveTriple` temporarily; their retirement remains a separate unfinished part of
  [#688](https://github.com/C360Studio/semstreams/issues/688).

## Impact

### Framework

- `service/rule_pack_bind.go` becomes the sole rule-pack mutation-client composition point.
- `processor/rule` depends on the public projection mutation interface rather than NATS mutation wire details for
  owned replacement.
- Existing public projection contracts, graph mutation subjects, graph-ingest handlers, and persisted state remain
  unchanged.
- `replace_owned` action configuration gains explicit `projection_contract` and `projection_group` fields.

### Consumers

SemDragon and every product composing SemStreams rules receive one framework-owned, contract-validated replacement
path for disjoint rule-pack-owned groups. Product rule packs must migrate each `replace_owned` action to explicit
contract and group selectors. Rules targeting groups already owned by the #696 built-in client remain deferred.

### Compatibility

- This is an intentional rule configuration migration: old `replace_owned` actions without both selectors fail
  validation instead of silently retaining one-predicate patch behavior.
- NATS subjects and graph mutation wire envelopes do not change.
- Hot reload may change rules only inside the contracts bound at boot; it cannot alter pack identity, projection
  contracts, or mutation-client binding.
- Both `cmd/semstreams` and `cmd/e2e-semstreams` already call the same pre-`StartAll` rule-pack helper; PR2 verifies
  those call sites and does not churn them unless the helper signature must change.
- The change completes only the rule-engine `ReplaceOwned` slice of #688. It does not close the issue while raw
  `AddTriple` and `RemoveTriple` remain.

## Non-goals

- Retiring raw `add_triple`, `remove_triple`, or their `TripleMutator` methods.
- Changing the public `projection.OwnedReplacer` API or its retry and verification policy.
- Adding a new graph mutation subject, wire envelope, graph-ingest handler, or persisted representation.
- Allowing a rule action to provide an arbitrary removal list or a caller-driven expected revision.
- Rebinding contracts during hot reload.
- Migrating the lesson lifecycle reference pack or any rule that consumes a #696 built-in-owned group.
- Designing or injecting a shared/prebound built-in mutation client; that remains a #688/shared follow-up.
- Moving SemDragon product semantics, rule policy, or vocabulary into SemStreams.
