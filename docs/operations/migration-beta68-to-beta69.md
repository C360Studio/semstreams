# Migration Guide: beta.68 → beta.69

## Summary

beta.69 ships **ADR-039 Phase 2** — subject-mode tool-call governance.
The rule engine gains a `$message.*` substitution namespace and an
`approve` action; agentic-loop gains a config flag
`tool_call_governance.mode` that turns on a published-then-subscribed
verdict flow.

The beta.67+68 in-process `ToolCallFilter` wiring is **retained** as
an escape hatch. Operators migrate at their own pace by flipping the
mode flag; beta.70 retires the in-process surface.

This tag is marked **BREAKING** because:

- New config schema field `tool_call_governance` on agentic-loop.
  External config validators MUST tolerate the new section (default
  `disabled` preserves all pre-beta.69 behavior).
- New input/output ports on agentic-loop (`agent.toolcall.proposed.*`,
  `agent.toolcall.approved.>`, `agent.toolcall.rejected.>`). Stream
  provisioning and port-aware tooling must accept them.
- New rule engine action type `approve` and substitution namespace
  `$message.*`. Config validators that enumerate accepted action types
  or substitution tokens must be updated.

No wire-format breaking changes to existing subjects. semspec on
beta.68 keeps working unchanged at beta.69 with `mode=disabled`.

## For semspec consumers

semspec currently wires the in-process filter:

```go
gov := governanceComponent.(*agenticgovernance.Component)
loop := loopComponent.(*agenticloop.Component)
if f := gov.ToolCallFilter(); f != nil {
    loop.SetToolCallFilter(f)
}
```

This **continues to work at beta.69** with `tool_call_governance.mode = "disabled"`
(the default). No code change is required to upgrade from beta.68 to
beta.69 — only the new config section needs to be accepted by your
config validator.

### Recommended migration path

Two-stage cutover, controlled by config alone:

#### Stage 1 (beta.69): turn on audit mode

```json
{
  "agentic-loop": {
    "tool_call_governance": {
      "mode": "audit",
      "timeout": "1s"
    }
  }
}
```

In `audit` mode, every proposed call is published to
`agent.toolcall.proposed.*` AND dispatched immediately. Verdicts that
arrive are logged for observability but do NOT gate dispatch. Rules
can be developed and tested without affecting production tool-call
flow.

Verify in metrics:

- `tool_call_governance_verdict_total{decision,mode="audit"}` shows
  approve/reject verdicts firing as expected.
- `tool_call_governance_verdict_duration_seconds{mode="audit"}` —
  inspect p99 to size the enforce-mode timeout.

#### Stage 2 (beta.69, when rules are vetted): switch to enforce

```json
{
  "agentic-loop": {
    "tool_call_governance": {
      "mode": "enforce",
      "timeout": "1s"
    }
  }
}
```

In `enforce` mode the loop **waits** for a per-call verdict and
**fails-closed on timeout** (treats absent verdict as a reject). The
in-process `ToolCallFilter` continues to run AFTER the governance
layer — both can coexist during the migration window. Operators
typically retire the in-process filter wiring once subject-mode rules
cover the same policy surface.

#### Stage 3 (beta.70): in-process filter surface retired

When semspec's subject-mode rules are confirmed end-to-end, beta.70
will retire the in-process filter wiring. Beta.70 will:

- Remove `*ToolCallFilter` interface from `agentic` package.
- Remove `agentic-loop.Component.SetToolCallFilter` /
  `agentic-loop.Component.ToolCallFilter` accessors.
- Remove the `agentic-governance.Component.ToolCallFilter()` accessor
  (beta.67+68 wiring point).
- Tighten the `tool_call_governance.timeout` default from `1s` to a
  measured p99 from beta.69 deployments.

There is no behavior change for deployments already on
`mode=enforce` at beta.69 — beta.70 only removes the pre-migration
escape hatch.

### Translating semspec's filter config to rules

semspec's filter config (beta.68):

```json
{
  "filter_chain": {
    "filters": [
      {
        "name": "tool_call_governance",
        "tool_call_config": {
          "blocked_command_patterns": ["cd /workspace "]
        }
      }
    ]
  }
}
```

Equivalent subject-mode rule (load into the rule engine):

```json
{
  "name": "block-bash-workspace-leak",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "tool_name", "operator": "eq", "value": "bash"},
    {"field": "command", "operator": "contains", "value": "cd /workspace"}
  ],
  "logic": "all",
  "on_enter": [
    {
      "type": "publish",
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "writes outside worktree blocked"
      }
    },
    {"type": "deny", "reason": "writes outside worktree blocked"}
  ]
}
```

Each `blocked_command_patterns` entry becomes one rule. The
`publish` + `deny` pair is the canonical ADR-039 rejection pattern:
`publish` emits the verdict to the loop's subscribed subject; `deny`
short-circuits subsequent actions and writes the audit triple.

See `docs/operations/17-tool-call-governance.md` for the full operator
guide, including `approve` action usage, `$caller.*` integration,
sub-agent governance inheritance via `parent_loop_id`, and
troubleshooting.

## Behavior changes

- **New default ports on agentic-loop.** `DefaultConfig` now includes
  `agent.toolcall.proposed`, `agent.toolcall.approved`, and
  `agent.toolcall.rejected` in the port set. Tests pinning the port
  count must update from 6→8 inputs and 7→8 outputs.
- **`ExecutionContext.MessageData`** is a new field on the rule
  engine's execution context. Populated automatically on the
  message-path (NATS-subscribed) rules from `GenericJSONPayload.Data`.
  Nil for entity-state-driven and cron-fired rules; `$message.*`
  substitution silent-passes + warns when nil.
- **`Evaluation.MessageData`** is a new field on the stateful
  evaluator's `Evaluation` struct. Populated by the rule processor's
  message-path. Callers constructing `Evaluation` manually (rare —
  almost always the rule processor's job) should populate this for
  message-path evaluations.

## New public API

| Symbol | Purpose |
|---|---|
| `agenticloop.ToolCallGovernanceConfig` | Config struct (Mode + Timeout) |
| `agenticloop.ToolCallGovernanceMode{Disabled,Audit,Enforce}` | Mode constants |
| `agenticloop.GovernanceDispatcher` | Interface; constructed automatically in NewComponent |
| `agenticloop.NewGovernanceDispatcher(cfg, publisher, logger, metrics)` | Manual construction for tests |
| `agenticloop.ProposedToolCallPayload` | Wire shape published to `agent.toolcall.proposed.*` |
| `agenticloop.VerdictPayload` | Wire shape accepted on `agent.toolcall.approved/rejected.>` |
| `rule.ActionTypeApprove` (constant `"approve"`) | New action type |
| `rule.PredicateRuleApprove` (constant `"rule.approve"`) | Audit-triple predicate |

## Tag-readiness checklist (semstreams)

Per the CLAUDE.md breaking-change rule, at least one relevant e2e
tier must run green before tag:

```bash
task e2e:agentic   # required — exercises the rule-driven governance path
```

The e2e config under `test/e2e/agentic/` should be updated to include
at least one rule-driven governance scenario in `audit` and `enforce`
modes before tagging beta.69. If the existing fixtures don't cover
this path, file a coverage gap and address before tag (HARD RULE per
`feedback_e2e_required_for_breaking_changes.md`).
