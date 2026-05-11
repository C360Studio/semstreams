# Migration Guide: beta.43 → beta.44

## Summary

Beta.44 ships the paired feature semspec/semteams asked for: belt
(`Action.MaxIterations`) plus suspenders (decide-tool SAP), with
mandatory LOUD observability on every SAP coercion.

**Belt** — `rule.Action.MaxIterations`: cross-loop firing cap on a
single action, defaults to **3**, scoped to (rule, action, entity).
Stops the structured-output rule-level ping-pong shape from running
forever.

**Suspenders** — Schema-Aligned-Parsing on the decide tool's
`action_allowlist`: common drift shapes (lowercase, hyphens, leading
whitespace) coerce to the canonical allowlist member rather than
producing an InvalidArgs rejection. Reduces retry pressure for the
expected drift class.

| Surface | Status |
|---|---|
| New optional `Action.ID string` field | **Additive** |
| New optional `Action.MaxIterations *int` field | **Additive (default cap = 3)** |
| `MatchState.ActionIterations map[string]int` (per-action firing counters) | **Additive — persisted state shape gains a field** |
| Decide tool: SAP normalisation on `action_allowlist` | **Behavioural — coerces near-misses to canonical form** |
| New audit triple predicate `coordinator.decide_sap_coerced` | **Additive** |
| New Prometheus metric `semstreams_decide_tool_action_allowlist_sap_coerced_total{from_action,to_action}` | **Additive** |
| New ToolResult metadata keys `sap_coerced` / `sap_raw_action` | **Additive — only set when coercion fires** |

**The simplest beta.43 → beta.44 upgrade is to do nothing.** Every
publish_agent action automatically inherits the default cap of 3
fires per (rule, action, entity). The SAP layer activates only on
near-miss action values; exact-match flows pass through unchanged.

## The user-facing constraint that drove the design

> "i personally dislike SAP as a rule. BUT the reality is it is
> probably needed. if we do add a small SAP layer we need to be LOUD
> about when it triggers."

SAP is a smell that can mask serious problems with model choice or
persona prompting. The framework provides the runtime safety net
that lets flows keep running through expected drift, AND makes every
coercion impossible to miss in operator dashboards. **High coercion
rate is a signal to fix model fit / persona prompt, not a feature to
celebrate.** The five LOUD signals (see below) make the smell
visible by construction.

## Belt: `Action.MaxIterations`

### What's new

Each action in a rule's `on_enter` / `on_exit` / `while_true` /
`on_recovery` list now carries a per-action firing cap:

```jsonc
{
  "type": "publish_agent",
  "role": "dev-via-spec-planner",
  "subject": "agent.task.planner",
  "max_iterations": 3,
  "id": "optional-stable-key"
}
```

### Sentinel semantics

`max_iterations` is a **pointer** (`*int`) on the wire so we can
distinguish "unset" from "explicit 0":

| JSON config | Behaviour |
|---|---|
| field absent | framework default = 3 |
| `"max_iterations": 0` | unlimited (operator's explicit opt-out) |
| `"max_iterations": N` (N>0) | explicit cap of N |
| `"max_iterations": -1` | rejected at config load |

The default-of-3 reflects semspec/semteams's reality: structured-
output retries are the rule, not the exception. One corrective shot
plus a margin. Operators who hit the cap repeatedly should fix the
persona prompt or model choice rather than raise the cap.

### Action identity

The cap is keyed on the action's stable identifier:

- If `Action.ID` is set explicitly, that string is the key.
- Otherwise, a deterministic hash of `(rule_id, action_type,
  subject_or_predicate, role)` provides a stable auto-generated
  fingerprint.

The fingerprint changes only when the author meaningfully changes
the action shape — renaming a rule, swapping the role, retargeting
a publish subject, or switching action types resets the per-action
counter; minor edits to a publish_agent's prompt or a triple's TTL
do not.

The 95% case (one publish_agent per rule branch) needs zero author
boilerplate. Authors set `Action.ID` explicitly only when they want
stable counters across an action rename, or want multiple distinct
actions to share a counter.

### Per-entity scoping

The cap is scoped to (rule, action, entity). Two different entities
running through the same rule each get their own counter — the
"planner pinged twice for entity-A" cap doesn't block planner fires
for entity-B.

### When to set `Action.ID` explicitly

The auto-generated fingerprint reset behaviour is intentional: it
invalidates the counter when the action shape changes meaningfully.
Authors **must** set `Action.ID` explicitly in two cases:

1. **Rolling subject renames.** Renaming
   `agent.task.researcher` → `agent.task.research-001` for a
   deployment rotation resets the per-action counter. If the cap
   needs to survive the rename, set `Action.ID` to a stable string.
2. **Multiple distinct actions sharing a counter.** Two
   `publish_agent` actions in different `on_enter` / `on_recovery`
   branches that should share a budget (e.g., a primary + fallback
   shape) need the same explicit `Action.ID`.

Conversely, two distinct actions in the same rule with identical
type+role+subject (or predicate+object, depending on action type)
**share** a counter by default. Set distinct `Action.ID` values if
you want separate budgets.

### Cron rule caveat (deferred runtime gate)

`Action.MaxIterations` and `Action.ID` are **validated at config
load** for cron rules' `actions` list (negative values rejected
identically to non-cron rules), **but the runtime cap gate does NOT
yet apply to cron-fired actions** in this tag. Cron actions go
through the cron scheduler's dispatch path rather than
`StatefulEvaluator.runActions`, which is where the gate lives.

Practical impact: cron rules with `"max_iterations": N` on their
actions silently get unlimited firing in beta.44. The validation
catches obvious config errors (e.g., negative values), but the
firing-cap semantics are deferred to a future tag that hoists the
gate into a shared helper both stateful and cron dispatch paths
call.

If you need a firing cap on a cron action TODAY, the workaround is
to use a `cooldown` or schedule expression that bounds the rate
rather than the absolute count.

## Suspenders: SAP at the decide tool

### What's new

When `action_allowlist` is set on a decide tool call (per beta.41),
the validator now does normalised matching as a fallback to exact
matching:

1. **Pass 1: exact match.** Hot path; no signals.
2. **Pass 2: normalised match.** If the input matches an allowlist
   member after normalisation (lowercase, hyphen→underscore, trim),
   coerce the action to the canonical form and fire the LOUD signals.
3. **Pass 3: rejection.** No exact, no normalised match → return
   `ToolErrorInvalidArgs` (existing behaviour).

V1 normalisation rules are deliberately conservative:

| Input | Coerced to | Rule |
|---|---|---|
| `"fan-out"` | `"fan_out"` | hyphen → underscore |
| `"FAN_OUT"` | `"fan_out"` | lowercase |
| `"  fan_out  "` | `"fan_out"` | trim whitespace |
| `"Fan-Out"` | `"fan_out"` | combined |
| `"fanout"` | (rejected) | no implicit space-insertion |
| `"fan_outs"` | (rejected) | no plural stripping |
| `"branch_out"` | (rejected) | no edit-distance fuzzy match |

V1 explicitly does NOT do edit-distance / Levenshtein matching.
Adding that would start to mask genuinely broken outputs and is out
of scope per the 2026-05-05 design discussion.

### The five LOUD signals

Every SAP coercion fires all five so the smell is impossible to miss:

| # | Signal | Where to look |
|---|---|---|
| 1 | `slog.Warn` log line | stdout / log aggregator |
| 2 | Prometheus counter `semstreams_decide_tool_action_allowlist_sap_coerced_total{from_action, to_action}` | Grafana — alert on rate-of-change |
| 3 | Audit triple `coordinator.decide_sap_coerced` on the loop entity, Object = `{raw}|{canonical}` | graph queries — group by Object to find recurring drift patterns |
| 4 | `ToolResult.Metadata["sap_coerced"] = true` + `["sap_raw_action"] = <original>` | tracing / replay / dashboards consuming raw tool results |
| 5 | This migration guide and `docs/operations/12-openai-client-keepalive.md` sibling doc | operator onboarding — frame coercion as smell, not feature |

### Operator alert recipe

Add a Grafana alert: any role with
`rate(semstreams_decide_tool_action_allowlist_sap_coerced_total[5m]) > 0`
**sustained** is a model/persona fit problem. Investigate the role's
system prompt, model choice, or allowlist values before raising
`MaxIterations`.

## What's NOT in this tag (deferred follow-ons)

- **Retry classification** (transient parse-error vs structural
  drift). semteams flagged probably premature; revisit if findings
  warrant tighter caps on the structural class.
- **Edit-distance / Levenshtein SAP**. Hides genuinely broken
  outputs; only add if V1's exact-and-normalised pair leaves real
  drift uncaught.

## Migration steps

### Existing operators

No action required. Existing rules inherit the default cap of 3 per
action. If a flow legitimately needs unbounded firing for some
specific action, set `"max_iterations": 0` on it explicitly.

### Existing decide tool consumers

No action required. SAP coercion is opt-in per allowlist (the layer
only activates when `action_allowlist` is set on the decide call,
which already means the operator opted into structural enforcement
in beta.41). Flows that don't use `action_allowlist` see no SAP
behaviour at all.

### Watch the SAP metric

Add `semstreams_decide_tool_action_allowlist_sap_coerced_total` to
your operator dashboard. The first time it ticks for a role, you have
data on which model + persona pairing is producing drift —
actionable before the next retry budget review.

## Backward compatibility

- `MatchState`: shape extended (new field), JSON omitempty, existing
  state files round-trip cleanly.
- `Action`: shape extended (two new optional fields), JSON omitempty.
- `decide` tool: action accepted as before for exact matches; new
  coercion path only fires on near-misses that previously rejected.
- No breaking changes to function signatures.

## Cross-references

- `processor/rule/actions.go:Action` — the new fields
- `processor/rule/action_id.go` — fingerprint, effectiveID,
  effectiveMaxIterations, isUnlimited
- `processor/rule/state_tracker.go:MatchState.ActionIterations` —
  persisted per-action firing counters
- `processor/rule/stateful_evaluator.go:runActions` — the gate
- `processor/agentic-tools/decide.go:resolveActionAllowlist` —
  exact + SAP allowlist resolver
- `processor/agentic-tools/decide.go:normaliseActionForSAP` — V1
  normalisation rules
- `vocabulary/agentic/predicates.go:CoordinatorDecisionSAPCoerced` —
  audit triple predicate (renamed from `CoordinatorDecideSAPCoerced`
  alongside the 3-segment predicate convention sweep; same underlying
  audit semantics)
- `processor/rule/action_id_test.go` —
  fingerprint stability + cap resolution tests
- `processor/rule/action_maxiterations_test.go` — end-to-end cap
  gate behaviour (default-3, explicit-1, explicit-0-unlimited,
  per-entity, shared explicit-ID)
- `processor/agentic-tools/decide_test.go` — SAP coerce + LOUD
  signals + reject-on-no-normalised-match
- `project_action_maxiterations_design.md` (memory) — full design
  decision history
- semspec/semteams 2026-05-05 design discussion (the empirical
  motivation for the bundle)
