# Design: Rule Projection Contract Derivation

## Context

PR #704 made rule replacement contract-bound and fail closed. A pack currently authors:

1. `projection_contracts` on `processor/rule.Config`; and
2. `projection_contract`, `projection_group`, `predicate`, and target subject semantics on every
   `replace_owned` action.

The first surface is the static authorization envelope. The second is executable use of that envelope. They must
not drift, but requiring both for the common case creates avoidable product-level ceremony.

The current preflight already freezes one initial rule snapshot before binding and reuses it at start. Derivation
must run over that exact snapshot. Reading rule files again, deriving during hot reload, or deriving after one pack
has bound would reopen the time-of-check/time-of-use and partial-composition hazards closed by PR #704.

## Decisions

### 1. Derive a minimal effective contract set from the frozen initial actions

Derivation scans every authored initial definition, including disabled definitions and every action collection:

- `on_enter`;
- `on_exit`;
- `while_true`;
- `on_recovery`; and
- cron `actions`.

Disabled rules participate because enabling or replacing a rule through hot reload must remain inside the same
boot-time envelope. Conditional execution does not change static authority.

For the current implementation, only `replace_owned` participates. Each action contributes:

```text
contract name = action.projection_contract
group name    = action.projection_group
group mode    = replace-owned
predicate     = action.predicate
target scope  = statically resolved action target pattern
```

Predicates are unioned by `(contract name, group name, mode)`. Contracts, groups, and predicates are emitted in a
deterministic order so equivalent authoring produces byte-stable effective contracts and reproducible errors.

The derivation mechanism is an explicit action-kind table, not a generic "looks mutating" heuristic. A future
contract-bound action may add a reviewed mapping. Existing raw `add_triple`, `remove_triple`, and `update_triple`
actions do not derive groups because their ownership modes and retirement remain under #688.

If no action and no explicit contract produce an effective contract, the pack remains contract-free and receives
no mutation client, matching current behavior.

### 2. Derive entity scope only when it is statically provable

For each participating action, target scope is derived as follows:

| Action subject | Enclosing rule | Derived target pattern |
| --- | --- | --- |
| omitted | valid non-empty `entity.pattern` | that exact six-position pattern |
| exactly `$entity.id` | valid non-empty `entity.pattern` | that exact six-position pattern |
| canonical literal entity ID | any supported rule | that exact ID, represented as an exact pattern |
| omitted | message-path or cron rule without `entity.pattern` | unresolved |
| any other template or substituted subject | any rule | unresolved |

The framework does not inspect conditions, message payload schemas, or triple values to guess a target pattern.
`$entity.triple.*`, `$message.*`, `$related.*`, iteration variables, mixed literal/template subjects, and other
runtime substitutions are unresolved even when a human expects a particular type.

One non-empty subject containing no template token must be a canonical literal entity ID. A wildcard declaration
pattern or malformed static value is an authoring error, not an unresolved dynamic target, because it can never
become a valid runtime entity ID through substitution.

All statically derived patterns for one contract must be identical for the zero-ceremony path. The framework does
not compute a least-common wildcard because that can authorize entity IDs no action declared. Authors with
multiple target patterns must split the contract names or provide one explicit covering pattern.

An unresolved target is not treated as `*.*.*.*.*.*`. It creates an explicit-envelope obligation. Without a
matching declared contract, preflight fails. With one, boot may proceed only after the declared group and predicate
cover the action; the existing mutation client still validates each resolved runtime entity ID against the
declared pattern before transport.

### 3. Merge explicit-only metadata without pretending it was inferred

The following fields are never inferred:

- `BirthPredicates`;
- `ForeignEdges`;
- `IndexingProfile`; and
- optional `MessageType`.

When no explicit declaration exists, derived contracts leave these fields empty. When an explicit declaration is
present, its values are copied into the effective contract and validated by the existing projection derivation
path.

An explicit-only contract with no participating action remains supported for compatibility, including birth-only
and foreign-edge-only declarations. Its binding posture continues to follow #700.

### 4. Use declared-superset override semantics

The explicit contract is an authorization envelope, not merely a serialization of the current action inventory.
It may intentionally reserve authority for a later hot-reloaded rule. Therefore declared-superset is
contract-correct; exact equality is the degenerate case.

Field presence is meaningful. Omitted `projection_contracts` selects default derivation. An explicitly authored
empty array is an empty override and therefore fails when any action derives an obligation; it must not silently
switch back to derivation. Config decoding must preserve enough presence information to distinguish these cases.

Containment is asymmetric and structural:

- every derived contract name must exist in the declared set;
- every derived group must exist in that contract with the same non-empty name and exact write mode;
- every derived predicate must occur in the matching declared group;
- every statically derived target pattern must be a subset of the declared `EntityPattern`; and
- every unresolved target must reference a declared contract/group/predicate.

Entity-pattern containment uses the canonical six-position pattern algebra. A derived pattern `D` is contained by
a declared pattern `A` only when, at every position, `A` is `*` or `A` equals `D`. A literal declared position
cannot contain a derived `*`. No sample IDs or regex approximations are used.

Declared extras are allowed and remain visible:

- additional predicates or groups reserve a frozen hot-reload envelope;
- a wider entity pattern explicitly grants wider authority;
- additional explicit-only contracts and metadata preserve existing configurations.

Every extra still passes normal `projection.Contract` and aggregate overlap validation. A declaration cannot
change a derived group's mode, omit a used predicate, narrow a used entity scope, or use duplicate/ambiguous
contract identities.

Automatic derivation remains least-authority: it emits exactly the observed groups/predicates and one proven
pattern. Only explicit authoring can broaden that result.

### 5. Preserve one immutable preflight snapshot and fail closed

Preflight order becomes:

1. load, parse, copy, and validate the complete initial rule snapshot;
2. validate pack IDs and action selectors;
3. derive minimal action contracts and unresolved-target obligations;
4. merge and validate optional explicit declarations;
5. build the action target index from the effective contracts;
6. validate every initial action against that index;
7. validate NATS, Registry, and heartbeater dependencies from the effective set;
8. detect pack-pack overlap across every effective set; and
9. bind each pack through the existing public mutation client.

Steps 1 through 8 complete for all packs before the first rule-pack Registry, presence, heartbeat, injection, or
mutation side effect. Any ambiguity, missing envelope, override mismatch, invalid contract, or overlap aborts
boot. Live Registry conflicts during step 9 retain PR #704's fail-closed process-abort behavior.

Rule-pack admission begins one lifecycle boundary earlier than binder discovery. `ComponentManager.Initialize`
currently creates and initializes enabled components before the composition root calls
`BindRulePackContracts`. Its general policy is intentionally best-effort: a component creation failure is logged
and other components remain available. Applying that generic policy to a rule processor is unsafe because
`Initialize` loads and validates the initial rules. A derivation or override failure can therefore remove the
invalid processor from the managed set, leaving only valid siblings for the binder to authorize.

Configured, enabled components whose factory name is `rule-processor` are a targeted exception. Any factory,
creation, or lifecycle-initialization failure for one of those entries is a boot-fatal rule-pack admission error.
The component manager continues the creation pass so it can collect all rule-pack admission failures, orders them
by configured instance name, preserves each wrapped root cause and rule/action location, and returns one
deterministic aggregate. The component-manager service constructor and both production composition roots already
propagate that error, so neither `BindRulePackContracts` nor `StartAll` runs.

Disabled rule-processor entries do not participate. Creation or initialization failure for an ordinary component
retains the established log-and-continue behavior. This exception is framework-aligned with the existing static
rule-pack policy: the component manager already retains rule-pack configs and prohibits rule-pack identity or
structural replacement after ownership is bound.

The binder interface/order must ensure `ProjectionBindings` returns the post-preflight effective snapshot rather
than the raw authoring declaration. It must not read raw contracts before preflight and then bind a different set.
The processor retains immutable copies and does not expose mutable slices.

### 6. Preserve #700 posture from the effective contract set

The service derives dependencies from the final effective contracts exactly as it does for explicit contracts:

- birth-only/no-claim: no Registry requirement, token, presence, or heartbeater enrollment;
- append-only and/or foreign-edge-only: Registry required, nil heartbeater allowed, persistent zero-token
  registration, and no presence;
- any `replace-owned` or `cas-transition` group: Registry and heartbeater required, with the complete entry
  liveness-managed.

A derived `replace-owned` action necessarily selects the owning posture. Explicit-only metadata or declared
superset groups can also affect posture through the existing projection derivation rules. Derivation does not
special-case or downgrade the resulting registration.

### 7. Keep hot reload inside the frozen effective envelope

Derivation occurs only at boot. Hot reload may add, remove, enable, or change rules only when every resulting
participating action resolves inside the frozen effective contract set.

Hot reload does not:

- expand a minimally derived group;
- widen an entity pattern;
- merge a new explicit override;
- alter explicit-only metadata;
- call the Registry or heartbeater; or
- bind a new client.

Authors who need later predicates or broader entity scopes must declare that superset before boot. Removing an
initial action does not shrink or rebind the running owner's contract.

### 8. Preserve config and schema compatibility

`projection_contracts` remains the existing optional JSON array. No derived contract is written back into
`Config.ProjectionContracts`, config KV, generated JSON, or operator-authored files. Runtime code keeps authored
and effective snapshots distinct.

The generated schema and field shape remain compatible. Descriptions should state:

- omission enables action derivation when every target scope is statically provable;
- a supplied array is an explicit superset override; and
- explicit-only fields are not inferred.

The `projection_contract`, `projection_group`, and literal `predicate` action selectors remain required for
`replace_owned`. Derivation removes the duplicate contract block, not stable action intent.

### 9. Preserve enforcement, execution, and wire behavior

The effective contracts flow through the same:

- `projection.Derive`;
- aggregate pack overlap preflight;
- `projection.BindMutationClient`;
- `projection.OwnedReplacer`; and
- graph-ingest mutation subjects and envelopes.

Complete selected-group replacement, token fencing, classified errors, retry policy, receipt tracking, and
authoritative verification do not change. There is no raw fallback when derivation fails.

## Alternatives Considered

### Require equality between explicit and derived contracts

Rejected. Exact equality would turn the explicit contract into duplicate current-state inventory and remove its
existing role as the immutable hot-reload authorization envelope. It would also make explicit-only contracts and
reserved in-envelope predicates unnecessarily difficult to express.

### Allow a declared superset without structural containment checks

Rejected. Matching only contract names would permit a mode change, missing predicate, or narrower entity pattern
to survive until execution. Every observed action must be covered before binding.

### Infer one wildcard cover for multiple target patterns

Rejected. The least syntactic wildcard cover can include unrelated entity IDs and silently broaden ownership.
Broadening requires explicit author intent.

### Treat every dynamic subject as match-all

Rejected. This turns an inability to prove scope into maximum authority. Dynamic targets require an explicit
envelope and remain runtime-validated.

### Derive contracts again on hot reload

Rejected. Registration is one-success-per-owner/Registry and cannot be safely expanded in place. Rebinding would
violate the frozen ownership and #700 liveness contracts.

### Infer raw Add/Remove/Update modes now

Rejected. Those actions still use a different mutation path and lack reviewed group/retry/ownership semantics.
Their migration remains under #688.

### Fail ComponentManager initialization for every enabled component error

Rejected. Best-effort isolation for ordinary components is an explicit framework behavior with existing tests and
runtime-reconciliation expectations. Rule packs require stricter admission because their authority is composed
once before start; extending that exception to unrelated components would broaden #706 and change platform
availability semantics.

### Defer rule loading until binder preflight

Rejected. `RuleProcessor.Initialize` is the established lifecycle phase that loads rules, and direct processor
consumers and integration tests rely on `Initialize` followed by `Start`. Moving materialization into the binder
would blur the binder's side-effect-free composition responsibility, require duplicate lifecycle guards, and
expand the change beyond the component-manager admission seam.

## Risks and Mitigations

- **Hidden authority widening:** automatic derivation is minimal; only explicit declarations may be supersets.
- **Dynamic target ambiguity:** unresolved targets require an explicit envelope and remain runtime-fenced.
- **Unexpected hot-reload rejection:** document that minimal derivation freezes only boot-observed authority.
- **Snapshot drift:** derive and bind from the same immutable preflight snapshot consumed at start.
- **Non-deterministic output:** sort effective contracts, groups, predicates, and diagnostic locations.
- **Partial composition:** finish all derivation/override/overlap checks across packs before the first bind.
- **Vanished invalid pack:** propagate configured, enabled rule-processor creation or initialization failures as a
  deterministic boot-fatal aggregate while preserving ordinary component isolation.
- **Posture drift:** derive Registry/heartbeat requirements only from the final effective contracts.
- **Schema confusion:** keep authored and effective contracts separate and update operator-facing descriptions.

## Architecture Sign-Off Gates

Implementation is conformant only when:

1. default derivation emits a deterministic minimal contract set from all initial `replace_owned` action sites;
2. explicit overrides are validated structural supersets, not unchecked replacement values;
3. dynamic or conflicting target scopes never become inferred match-all authority;
4. explicit-only fields remain explicit and pass existing contract validation;
5. every configured, enabled rule processor is successfully admitted or boot fails before binder discovery;
6. every admitted pack completes derivation and override validation before the first rule-pack bind;
7. binding still uses only the public mutation client and preserves #700 posture;
8. hot reload cannot expand or rebind the frozen effective envelope;
9. authored config round trips without injected derived contracts;
10. no public projection API, graph wire, or persisted-state change appears; and
11. mandatory Fable review approves the public rule-authoring contract before implementation acceptance.

## Fable Disposition

This change warrants mandatory Fable review. It does not change `pkg/projection`, but it changes the public
framework authoring contract consumed by SemDragon and other rule-pack authors: omission gains meaning,
declarations become superset assertions, dynamic targets acquire an explicit-envelope rule, and hot-reload
authority depends on whether a declaration was supplied. Fable must review least-authority behavior, ambiguity,
compatibility, and the public configuration contract before implementation acceptance.
