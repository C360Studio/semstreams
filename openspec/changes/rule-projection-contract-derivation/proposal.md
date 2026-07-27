# Rule Projection Contract Derivation

## Why

Issue [#706](https://github.com/C360Studio/semstreams/issues/706) follows the merged rule mutation-client migration
from PR #704. Enforcement is correct: every rule pack binds one immutable public mutation client before start,
`replace_owned` actions select a named contract/group, and ownership overlap or dependency failures abort boot.

The remaining authoring surface duplicates the same facts in two places. Initial rule actions already name their
projection contract, group, predicate, and target entity scope, while `projection_contracts` repeats the group and
entity pattern. SemDragon would otherwise hand-maintain that duplication across roughly twenty processors.

## What Changes

- Derive the minimal `replace-owned` predicate groups and entity pattern for each contract from the pack's frozen
  initial action snapshot.
- Keep action selectors explicit: `projection_contract`, `projection_group`, and literal `predicate` remain the
  stable atomic-target identity.
- Treat `projection_contracts` as an optional explicit override. A declaration must be a validated superset of the
  derived authority; equality is accepted but not required.
- Permit explicit supersets because a static contract is also the immutable hot-reload authorization envelope.
  Automatic derivation never invents a broader wildcard or unused predicate.
- Keep `BirthPredicates`, `ForeignEdges`, `IndexingProfile`, and optional `MessageType` explicit-only.
- Require an explicit contract envelope when an action's target entity pattern cannot be proven statically.
- Treat creation or initialization failure for any configured, enabled `rule-processor` as a boot-fatal rule-pack
  admission failure. An invalid pack must not disappear from binder discovery while valid siblings continue.
- Continue to bind only through the existing fail-closed `BindMutationClient` path. Ownership, heartbeat, token,
  retry, wire, and persisted-state semantics do not change.

## Impact

### Framework

- `processor/rule` gains a side-effect-free authoring derivation and override-validation phase over the same frozen
  initial rules already used by preflight and start.
- `service.BindRulePackContracts` consumes only the effective immutable contracts produced by successful preflight.
- `service.ComponentManager` retains its best-effort creation policy for ordinary components, but propagates a
  deterministic aggregate when configured, enabled rule processors fail factory creation or initialization.
- No `pkg/projection`, `pkg/ownership`, graph mutation subject, request envelope, or graph-ingest behavior changes.

### Authors

- A common entity-scoped `replace_owned` pack may omit `projection_contracts`.
- Existing explicit contracts remain valid when they cover every derived action target and pass existing contract
  validation.
- Explicit declarations may reserve additional in-envelope predicates or groups for later hot reload, but this
  broader authority is visible authoring input rather than an inferred widening.
- Dynamic or message-derived target entities still require an explicit entity-pattern envelope.

### Compatibility

- The JSON field `projection_contracts` remains optional and retains its existing shape.
- Existing explicit configurations require no rewrite when they are valid supersets.
- An explicit declaration narrower than the actions now fails before ownership or heartbeat side effects.
- Disabled invalid rule-processor configurations remain ignored. Ordinary component creation failures retain their
  existing isolated log-and-continue behavior.
- Effective derived contracts are runtime composition state; derivation does not rewrite the authored config or
  add generated contracts to config round trips.

## Non-Goals

- Changing the #700 owning/non-owning posture matrix.
- Inferring birth predicates, foreign edges, indexing profiles, or message types from rule behavior.
- Inferring contracts for raw `add_triple`, `remove_triple`, or `update_triple` actions before those lanes receive a
  reviewed contract-bound mutation design.
- Adding a raw mutation fallback, lazy binding, or hot-reload rebinding.
- Inferring a broad entity wildcard from unrelated target patterns.
- Changing complete selected-group replacement semantics.
