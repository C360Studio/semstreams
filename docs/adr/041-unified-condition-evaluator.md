# ADR-041: Unified Condition Evaluator — Rule-Level + Action-When Share Field Resolution

## Status

**Proposed — 2026-05-14.** Tag scope: beta.72 BREAKING (bundled with the
ADR-040 boid retirement so semspec migrates once).

Forcing function: semspec filed an ask
(`.semspec/semstreams-ask-action-when-message-payload.md`, 2026-05-13)
to extend `Action.When` to see message payloads. Investigation found
that the underlying cause is a structural debt — two condition
evaluators with different field-resolution semantics — that semspec's
governance use case is the visible symptom of, not the root.

This ADR records the **full unification** rather than the surgical
patch semspec requested. Rationale below; bundle of one BREAKING tag
is the cost saver.

## Context

### Two evaluators today

| | Rule-level (message-path) | Action-When |
|---|---|---|
| Path | `ExpressionRule.evaluateConditions(data)` (`expression_factory.go`, pre-unification) | `evaluateWhen → EvaluateWithStateFields` (`stateful_evaluator.go`) |
| Field source | Walks message data map via own `extractNestedValue` | Entity triples + `$state.*`, nothing else |
| `$state.*` | Not supported | Supported |
| `$message.*` (explicit) | Not supported | Not supported |
| Bare name | Message payload (with deep-walk) | Entity triples |
| Operators | Inline switch in `expression_factory.go` (`eq/ne/lt/...`) | Registered `OperatorFunc` map in `expression/evaluator.go` |
| Numeric coerce | `toNumeric`/`compareNumeric`/`compareValues` (rule package) | `toFloat64`/`compareValuesWithError` (expression package) |

Same author writing one rule that uses both rule-level `conditions` and
action-level `when` had to learn **two different field-resolution
models**, two different operator coverage matrices, and two different
numeric-coercion edge case sets. The split was historical: the
expression package was added later for entity-state rules; the original
`ExpressionRule.evaluateConditions` predated it and was never migrated.

### How the asymmetry blocked semspec

ADR-039 introduced subject-mode tool-call governance with the canonical
reject pattern `publish` + `deny`. In enforce mode every call needs an
explicit verdict within `timeout` or the dispatcher fails closed. The
race-free shape for "block N patterns, approve everything else" is a
single rule with `when`-guarded publish/deny pairs followed by an
unconditional approve fallback. semspec hit "take-20 of hybrid @hard
2026-05-13" wedged on this because `when` couldn't see `$message.*` —
the planner's `submit_work` call matched no rule, got no verdict,
timed out fail-closed.

Their workaround: three separate rules with three separate publish/deny
pairs, racing each other on enforce-mode firing order. That works only
in audit mode (where verdicts don't gate dispatch). Not viable for
production governance.

### The surgical fix vs. the structural fix

semspec's filing proposed extending `evaluateWhen` to receive
`messageFields` and adding `EvaluateWithStateAndMessage` — a targeted
~150-300 LOC patch that closes the When-clause asymmetry while leaving
the rule-level evaluator's separate code path intact. This works.

The structural alternative: route both paths through one evaluator,
delete the duplicate. Adds ~50-100 LOC but **deletes ~200 LOC** of
duplicate (rule-package `compareValues`/`compareNumeric`/`toNumeric` +
`ExpressionRule.evaluateConditions`/`extractNestedValue`/`evaluateCondition`
+ `TestRule` equivalents). Net smaller. Fixes the cognitive split for
rule authors. Forecloses a class of "rule-level vs when-level
behaviour drift" bugs that would otherwise accumulate over time.

We are greenfield (beta tag train, ≤2 external consumers — semspec and
semteams), already shipping at least one BREAKING change (ADR-040 boid
retirement + Go version bump). The marginal cost of bundling the
structural fix is near zero; the carrying cost of leaving the duplicate
evaluators in place compounds every time the framework gains a new
substitution namespace or operator.

## Decision

**Unify rule-level and action-When evaluation behind a single
`Evaluator.EvaluateWithStateAndMessage(entity, stateFields, messageFields, expr)`
method.** All callers (message-path rule evaluation, entity-path rule
evaluation, action-When guards, transition re-evaluation) route through
this method. Delete the duplicate evaluators in `expression_factory.go`
and `test_rule_factory.go`.

### Field-resolution precedence (single rule)

Applied uniformly by `evaluateConditionWithStateAndMessage`:

1. **`$state.<field>` / `$prev.<field>`** → resolves from `stateFields`
2. **`$message.<dotted.path>`** → resolves from `messageFields` via
   deep-walk (shared `expression.ExtractMessageValue` helper, also
   used by `$message.*` substitution in
   `processor/rule/message_substitution.go`)
3. **`OpTransition` operator** → uses `$prev.<field>` from stateFields
   against current entity triple (entity-state-only by design)
4. **Bare field name** → entity triples first (when entity is non-nil),
   falls through to `messageFields` if not present on the entity. When
   entity is nil (message-path rules), bare names resolve from
   `messageFields` directly.

### Documentation guidance

**`$message.<field>` is the recommended form** when the resolution
source matters — matches the `$message.*` substitution namespace used
in `subject`, `properties`, and `reason` strings, and avoids the "wait,
which `command` did this resolve to?" debugging path.

Bare names remain valid for terse authoring; resolution source depends
on rule type (entity-path resolves entity-first, message-path resolves
message-only). Acceptable when the author is writing rules for one rule
type and knows the context.

## What ships in beta.72

| File | Net change |
|---|---|
| `processor/rule/expression/types.go` | +`MessageFields` type alias |
| `processor/rule/expression/message_path.go` | New file: `ExtractMessageValue` helper (shared with substitution layer) |
| `processor/rule/expression/evaluator.go` | New `EvaluateWithStateAndMessage` (now single resolution entry point); `Evaluate` delegates; `EvaluateWithStateFields` deleted; `evaluateCondition` deleted; new `applyOperator` helper |
| `processor/rule/stateful_evaluator.go` | `runActions`/`evaluateWhen` gain `messageFields` parameter, threaded from `ec.MessageData`; transition re-eval uses `EvaluateWithStateAndMessage` with nil messageFields |
| `processor/rule/expression_factory.go` | `ExpressionRule.evaluateConditions`, `extractNestedValue`, `evaluateCondition` deleted; package-level `compareValues`/`compareNumeric`/`toNumeric` deleted; `Evaluate` routes through unified evaluator |
| `processor/rule/test_rule_factory.go` | Same migration as ExpressionRule |
| `processor/rule/message_substitution.go` | Local `extractMessageValue` removed; delegates to `expression.ExtractMessageValue` |
| `processor/rule/expression/evaluator_test.go` | New test matrix: explicit `$message.*`, deep paths, bare-name message-path fallback, entity-path backward compat, precedence ordering, state+message composition |
| `processor/rule/stateful_evaluator_test.go` | `TestStatefulEvaluator_WhenMessagePayloadAccess` (ADR-041 acceptance), `TestStatefulEvaluator_WhenConsolidatedGovernancePattern` (semspec's canonical use case) |
| `processor/rule/actions.go` | `Action.When` doc comment rewritten for new precedence |
| `docs/operations/17-tool-call-governance.md` | New "Consolidated blocklist with fallback approve" example showing the canonical race-free pattern |

**Net LOC:** approximately +400 / -250 → +150 net (test matrix dominates).

## BREAKING surface

1. **`Evaluator.EvaluateWithStateFields` removed.** Was previously a
   deprecated shim; all callers updated to `EvaluateWithStateAndMessage`.
   External callers (none identified outside semstreams) must rename.

2. **Bare-name field resolution semantics widened.** On message-path
   rules, bare names now resolve from message payload (was: silently
   skipped because entity was nil). On entity-path rules, bare names
   now fall through to message payload after checking entity triples
   (was: only entity triples). The fall-through is additive — no
   existing test or production config relies on the old "field not
   found" behaviour for bare names that match a message field, because
   no existing path had access to message data at the When-clause level
   anyway.

3. **Operator behaviour merged.** `ExpressionRule.evaluateCondition`'s
   inline operator implementations had subtle differences from the
   expression package's operators (e.g., `compareValues` numeric-then-
   string fallback vs `compareValuesWithError`). Diff'd line-by-line;
   the expression package's behaviour was kept where they diverged
   because (a) it has better error returns, (b) it matches the
   `OperatorFunc` registry used by entity-state evaluation, (c) edge
   cases are documented in test cases. Behaviour delta is invisible
   for any input both implementations agreed on; only edge cases like
   "compare string to nil" potentially differ.

4. **Bundle with ADR-040 + Go version bump in beta.72.** Single
   BREAKING tag; release notes call out the unification, the boid
   retirement, and the Go bump together. semspec migrates once.

## Migration

**For semspec:**

```diff
-"on_enter": [
-  {"type": "publish", "subject": "...rejected...", "properties": {...}},
-  {"type": "deny", "reason": "..."}
-]
+"on_enter": [
+  {
+    "type": "publish",
+    "when": [{"field": "$message.command", "operator": "contains", "value": "cd /workspace"}],
+    "subject": "...rejected...",
+    "properties": {...}
+  },
+  {
+    "type": "deny",
+    "when": [{"field": "$message.command", "operator": "contains", "value": "cd /workspace"}],
+    "reason": "..."
+  },
+  {"type": "publish", "subject": "...approved...", "properties": {...}}  // unconditional fallback
+]
```

Collapse three rules into one. Drops the multi-rule firing race in
enforce mode.

**For framework consumers calling the evaluator directly:**

```diff
-result, err := evaluator.EvaluateWithStateFields(entity, stateFields, expr)
+result, err := evaluator.EvaluateWithStateAndMessage(entity, stateFields, nil, expr)
```

Or pass `messageFields` if you have an inbound payload in scope.

## Why not just the surgical patch

semspec's filing proposed the surgical patch and explicitly offered to
accept the secondary "`default_decision_on_timeout`" knob as a fallback.
Both work for the immediate use case. We picked the structural fix
because:

1. **Cognitive surface for rule authors.** Two evaluators with
   different field-resolution rules means authors must remember which
   path they're on when reading a rule. Unification removes that.

2. **Future namespace additions get one home.** When (not if) the
   framework adds `$caller.*` to condition evaluation (currently it's
   only substitution-side), `$schedule.*` to non-cron rules, or any
   future namespace — the surgical patch would require duplicating the
   addition into both evaluators. Unification means one place.

3. **Duplicate operators were a latent divergence risk.** Subtle
   differences in `compareValues` numeric handling, missing operators
   in `ExpressionRule.evaluateCondition` (no `in`/`not_in`/`between`/
   `regex` — only `eq/ne/lt/lte/gt/gte/contains/starts_with/ends_with`)
   meant rule-level and When-level rules **already had different
   operator coverage** before this change. Unification gives both paths
   the full operator set.

4. **Cost asymmetry.** Surgical patch: ~+200 LOC. Structural fix:
   ~+150 LOC net (more new code, more deleted). The structural fix is
   strictly smaller and strictly more general.

5. **We control both consumers.** semspec is willing to migrate;
   semteams pinning track is close behind. Greenfield bias is the right
   default for any change that's correct-but-breaking.

## Alternative considered: `default_decision_on_timeout` knob

semspec proposed this as a secondary ask: a config knob on
`tool_call_governance` that flips enforce semantics from implicit-deny
to implicit-allow. Rejected:

- One-purpose patch — only fixes the governance dispatcher; nothing
  else benefits
- Encodes a policy that real security gates would disagree with
  (implicit-allow on a security path is a footgun)
- Doesn't fix the underlying evaluator asymmetry — same class of bug
  resurfaces wherever per-action gating wants payload context

Not shipping. The unified evaluator is strictly more general.

## Operator coverage parity

Pre-unification, the two evaluators had different operator sets:

| Operator | Rule-level | Action-When |
|---|---|---|
| eq, ne | ✓ | ✓ |
| lt, lte, gt, gte | ✓ | ✓ |
| contains, starts_with, ends_with | ✓ | ✓ |
| in, not_in | ✗ (rule-level had no support) | ✓ |
| between | ✗ | ✓ |
| regex | ✗ | ✓ |
| transition | ✗ | ✓ (entity-state only) |
| array length_eq/gt/lt | ✗ | ✓ |
| array_contains | ✗ | ✓ |

Post-unification both paths get the full set. semspec's existing
rule-level conditions (using `in` and other ops via separate code paths)
keep working; their new When clauses gain access to the same set.

## Related ADRs

- [ADR-028 Orchestration Architecture](028-orchestration-architecture.md)
  — rules vs workflows; the basis for "rules engine is the evaluation
  primitive"
- [ADR-031 Time-Trigger Primitive](031-time-trigger-primitive.md) —
  `$schedule.*` namespace; precedent for namespace-scoped resolution
- [ADR-032 Policy/Tenancy/Cluster](032-policy-tenancy-cluster.md) —
  `$caller.*` namespace + `deny` action
- [ADR-039 Tool-Call Governance Rule-Driven](039-tool-call-governance-rule-driven.md)
  — the consumer that surfaced the asymmetry; this ADR completes
  ADR-039's vision of "rules engine is the policy DSL" by making the
  policy DSL composable on action-level guards
- [ADR-040 Retire Boid Subsystem](040-retire-boid-subsystem.md) —
  bundled in the same beta.72 BREAKING tag

## Open questions deferred

- **Whether to expose `$caller.*` in condition `Field` paths** (not
  just in substitution strings). Today `$caller.*` works in `subject`,
  `reason`, etc. via substitution but not as a `Field` in a condition.
  Could be added under the new evaluator with a small extension to the
  precedence rule. Defer to a future ADR if a use case appears.
- **Whether to add a `$entity.<part>` form for condition `Field`** —
  per-segment entity ID access (`$entity.org`, `$entity.platform`,
  etc.) currently works in substitution but not condition. Same
  pattern; defer until needed.
- **Operator chaining / nested logic expressions** — the current
  `LogicalExpression` has flat `Conditions` with one `Logic` op.
  Nested AND/OR/NOT trees would need a different shape. Out of scope
  here.
