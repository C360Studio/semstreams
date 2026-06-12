# Tool-Call Governance (ADR-039)

`agentic-loop` supports subject-mode tool-call governance via the
rules engine. When enabled, every tool call the model proposes is
published to `agent.toolcall.proposed.<loop_id>`, rules subscribe and
emit verdicts on `agent.toolcall.approved.*` / `.rejected.*`, and the
loop dispatches only approved calls.

This is the rule-driven replacement for the in-process `ToolCallFilter`
wiring shipped in beta.67+68. **Beta.70 retired the in-process filter
wiring**; subject-mode is now the sole tool-call governance path. The
beta.68 → beta.69 → beta.70 migration is documented in the [migration
guide][migration].

[migration]: migration-beta68-to-beta70.md

## When to use this

Use subject-mode governance when policy needs to:

- Compose across signals (a deny condition depending on caller role
  AND tool name AND time-of-day).
- Be operator-editable without a recompile.
- Record an append-only audit trail of every deny/approve verdict
  (the `GOVERNANCE_VERDICT_AUDIT` stream, ADR-055 §3a).
- Vary across rule-engine deployments without code changes in
  agentic-loop.

The in-process `ToolCallFilter` (beta.67+68) handled fixed
command-pattern blocklists with no rule-engine dependency. Beta.70
retired that wiring; subject-mode rules are now the only path.

## Modes

Configure on the agentic-loop component:

```json
{
  "tool_call_governance": {
    "mode": "disabled",
    "timeout": "1s"
  }
}
```

| Mode | Behavior |
|---|---|
| `disabled` (default) | No publish, no wait. No governance gate active. |
| `audit` | Publishes every proposed call to `agent.toolcall.proposed.*`. Dispatches **immediately** without waiting. Verdicts that arrive are logged for observability. Use during rule development. |
| `enforce` | Publishes proposed call, waits up to `timeout` for a per-call verdict, dispatches on approve / fails on reject / fail-closed on timeout. Production posture. |

The default `1s` timeout is deliberately generous to surface real p99
latency in the `tool_call_governance_verdict_duration_seconds`
histogram. Operators measuring sub-100ms p99 in audit mode can drop
the default with no functional change.

## Subject Topology

```text
agent.toolcall.proposed.<loop_id>          (out from loop, in to rules)
agent.toolcall.approved.<loop_id>.<call_id>  (in to loop, out from rules)
agent.toolcall.rejected.<loop_id>.<call_id>  (in to loop, out from rules)
```

The loop binds a single wildcard subscription to
`agent.toolcall.approved.>` and `.rejected.>` at startup, before any
task arrives. Verdicts demux per-call by the `call_id` field on the
payload — both approve-action (top-level) and publish-action (nested
`properties`) shapes are supported.

## Proposed-Call Payload

```json
{
  "loop_id": "abc-123",
  "parent_loop_id": "parent-uuid",
  "call_id": "call-001",
  "tool_name": "bash",
  "command": "ls /tmp",
  "url": "",
  "arguments": { ... }
}
```

- `parent_loop_id` is empty for top-level loops. It rides along from
  day one so rules can match across spawn hierarchies (sub-agent
  governance inheritance) without a wire format change.
- `command` and `url` are lifted out of `arguments` when present, so
  rule conditions can match `$message.command` and `$message.url`
  directly instead of digging through `$message.arguments.command`.

## Verdict Payload

The loop accepts two payload shapes for the verdict:

```json
// shape A — emitted by `approve` action (top-level)
{
  "decision": "approved",
  "call_id": "call-001",
  "loop_id": "abc-123",
  "rule_id": "allow-readonly-tools",
  "reason": "tool is read-only",
  "entity_id": "...",
  "timestamp": "..."
}
```

```json
// shape B — emitted by `publish` action (nested under properties)
{
  "subject": "agent.toolcall.rejected.abc-123.call-001",
  "source": "rule_engine",
  "properties": {
    "decision": "rejected",
    "call_id": "call-001",
    "reason": "bash disallowed by policy"
  }
}
```

`VerdictPayload.EffectiveDecision` / `EffectiveCallID` / `EffectiveReason`
fall through both shapes so consumers don't write the dispatch logic
twice. The ADR-039 canonical pattern uses `publish` + `deny` for
rejections; the loop accepts the publish-action shape produced by that
pair.

## Writing Rules

### Block bash patterns

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

The `publish` action emits the rejection to the loop's verdict
subject. `deny` short-circuits the rule's remaining actions and
emits a deny verdict event to the append-only `GOVERNANCE_VERDICT_AUDIT`
stream (ADR-055 §3a; subject `governance.verdict.deny.{rule_token}`).

### Allow read-only tools

```json
{
  "name": "auto-approve-readonly-tools",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "tool_name", "operator": "in",
     "value": ["query_entity", "query_relationships", "read_loop_result", "graph_search"]}
  ],
  "on_enter": [
    {
      "type": "approve",
      "subject": "agent.toolcall.approved.$message.loop_id.$message.call_id",
      "reason": "read-only tool $message.tool_name"
    }
  ]
}
```

`approve` emits an approve verdict event to the `GOVERNANCE_VERDICT_AUDIT`
stream (subject `governance.verdict.approve.{rule_token}`, ADR-055 §3a)
AND publishes the routing verdict to the loop in one action. It does NOT
short-circuit subsequent actions — operators may want to add metric-counter
actions or downstream notifications on the same firing.

### Consolidated blocklist with fallback approve (recommended)

The canonical race-free shape for enforce-mode governance: one rule
that pairs `publish` + `deny` for each bad pattern, then a trailing
unconditional approve. Exactly one verdict per call, no multi-rule
race condition. Requires action-`when` clauses with `$message.*`
access (ADR-041, beta.72+).

```json
{
  "name": "bash-workspace-guard",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "$message.tool_name", "operator": "eq", "value": "bash"}
  ],
  "on_enter": [
    {
      "type": "publish",
      "when": [{"field": "$message.command", "operator": "contains", "value": "cd /workspace"}],
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "bash 'cd /workspace' blocked — stay in worktree"
      }
    },
    {
      "type": "deny",
      "when": [{"field": "$message.command", "operator": "contains", "value": "cd /workspace"}],
      "reason": "bash 'cd /workspace' blocked"
    },
    {
      "type": "publish",
      "when": [{"field": "$message.command", "operator": "contains", "value": "> /workspace/"}],
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "bash redirect into /workspace blocked"
      }
    },
    {
      "type": "deny",
      "when": [{"field": "$message.command", "operator": "contains", "value": "> /workspace/"}],
      "reason": "bash redirect into /workspace blocked"
    },
    {
      "type": "publish",
      "subject": "agent.toolcall.approved.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "approved",
        "call_id": "$message.call_id",
        "reason": "bash command passed blocklist"
      }
    }
  ]
}
```

Sequencing invariants this pattern relies on:

- Actions inside a rule run sequentially in declared order
- `deny` short-circuits remaining actions in the same firing (returns
  `*DenyVerdict`, which the executor handles by breaking the loop —
  see `processor/rule/stateful_evaluator.go:357-366`)
- Actions without a `when` clause always run if reached
- Therefore the trailing unconditional `publish approved` only fires
  when **no earlier `deny` matched** → exactly one verdict per call

Without the `when` clause's access to `$message.*`, this pattern is
impossible to express in a single rule. Pre-ADR-041 the only option
was N separate rules (one per bad pattern), which introduced a
multi-rule firing race in enforce mode.

### Role-based allowlist with caller context

```json
{
  "name": "operator-role-allowlist",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "tool_name", "operator": "in",
     "value": ["bash", "http_request"]},
    {"field": "$caller.role", "operator": "ne", "value": "operator"}
  ],
  "logic": "all",
  "on_enter": [
    {
      "type": "publish",
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "$caller.role role cannot use $message.tool_name"
      }
    },
    {"type": "deny", "reason": "role $caller.role denied for $message.tool_name"}
  ]
}
```

## $message.* Substitution

Rule action templates accept `$message.<field_path>` tokens that
resolve against the proposed-call payload:

| Token | Resolves to |
|---|---|
| `$message.loop_id` | The loop's bare UUID |
| `$message.call_id` | The tool-call ID — required for verdict subjects |
| `$message.tool_name` | The tool name (`bash`, `http_request`, etc.) |
| `$message.command` | The bash command (top-level convenience) |
| `$message.url` | The HTTP URL (top-level convenience) |
| `$message.arguments.<key>` | Deep path into the arguments map |
| `$message.parent_loop_id` | Spawn-tree parent's UUID (empty for top-level) |

An unresolved token (field missing from the payload) survives
substitution and trips the existing unresolved-template warning at
`processor/rule/execution_context.go:27` — operators see the
silent-pass loudly instead of empty strings landing in routing
subjects.

## Observability

Three Prometheus metrics drive the operational view:

| Metric | Labels | Use |
|---|---|---|
| `semstreams_agentic_loop_tool_call_governance_verdict_duration_seconds` | `decision`, `mode` | Buckets 1ms → 5s. Drives the timeout-tuning decision in beta.70 — set `timeout` after measuring p99 from this histogram. |
| `semstreams_agentic_loop_tool_call_governance_verdict_total` | `decision`, `mode` | Sum of approved+rejected+timeout per mode. Sustained `decision=timeout` rate signals undersized timeout or stuck rule engine. |
| `semstreams_agentic_loop_tool_call_governance_subscribe_before_publish_failures_total` | (none) | Increments when a verdict arrives without a registered waiter. Non-zero rate is the canonical signal that the subscribe-before-publish race-fix regressed; investigate immediately. |
| `semstreams_rule_governance_verdict_audit_failures_total` | `decision` | A deny/approve verdict was applied but its append-only audit record failed to publish (ADR-055 §3a). The verdict still holds — but a non-zero value is a compliance-visibility gap, critical in `enforce` mode. |

### Verdict audit trail

Every explicit `deny`/`approve` verdict is recorded as a registered
event on the append-only `GOVERNANCE_VERDICT_AUDIT` JetStream stream
(ADR-055 §3a), subject `governance.verdict.{decision}.{rule_token}`
(`rule_token` is a subject-safe hash of the rule ID; the canonical
`rule_id` travels in the payload). This replaces the former
`rule.deny`/`rule.approve` audit triples. To review verdict history,
replay/filter the stream (e.g. `governance.verdict.deny.>` for all
denials); the payload carries `{decision, rule_id, reason, entity_id,
timestamp, loop_id?, call_id?}`. The audit emit is best-effort — a
failed emit never flips a verdict — and lost records are surfaced via
`semstreams_rule_governance_verdict_audit_failures_total`.

## Troubleshooting

### All calls fail with `governance verdict timeout`

The loop is in enforce mode but no rule is firing a verdict. Check:

1. Is the rule engine running and subscribed to `agent.toolcall.proposed.>`?
2. Does at least one rule's `subscribe` field include
   `agent.toolcall.proposed.>` (or a more specific match)?
3. Are rule actions writing to
   `agent.toolcall.approved.$message.loop_id.$message.call_id` or
   `.rejected.…`?
4. Is the rule's evaluation triggering? Check
   `semstreams_rule_evaluations_total{rule_name=...,result=triggered}`.

The deliberately-generous 1s default timeout means a healthy rule
engine fires well inside the window. If p99 is consistently >500ms,
something's wrong with the rule path (slow rule, big condition set,
contended NATS).

### `subscribe_before_publish_failures` rate is non-zero

Verdicts are arriving for `call_id`s that no longer have a registered
waiter. Either:

- The race-fix regressed (verdict reached the loop before Propose's
  pre-register — should be impossible given the in-process buffered
  channel pattern, but worth checking against a recent diff).
- The verdict is from a different loop component (multi-component
  deployment sharing the AGENT stream). Filter by inspecting the
  proposed `loop_id` in the verdict payload — if it doesn't match a
  known loop, route via a different subject or stream.

### `audit` mode shows verdicts but `enforce` mode times out

Switch back to `audit` and verify the verdict subject matches what the
loop is subscribed to. The loop subscribes to wildcards
`agent.toolcall.approved.>` and `.rejected.>` — rules that publish to
`agent.toolcall.allow.…` or similar custom subjects will be invisible
to the loop. Restore the canonical subject names.

### Mass-rejection wedge

If every tool call rejects, the loop can't make forward progress.
This is the rule engine honoring operator policy — not a bug. To
debug:

1. Switch to `audit` mode temporarily so the loop dispatches anyway
   but verdicts are still recorded.
2. Inspect `rule_evaluations_total{result=triggered}` to identify
   the rule(s) firing.
3. Once the cause is clear, fix the rule, switch back to `enforce`.

## Sub-Agent Governance Inheritance

`parent_loop_id` rides along in every proposed-call payload — top-level
loops emit empty, child loops emit their spawn-tree parent. Rules can
match on `$message.parent_loop_id` to apply policy across loop
hierarchies. Example:

```json
{
  "name": "sub-agents-cannot-shell",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "parent_loop_id", "operator": "ne", "value": ""},
    {"field": "tool_name", "operator": "eq", "value": "bash"}
  ],
  "logic": "all",
  "on_enter": [
    {
      "type": "publish",
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "decision": "rejected",
        "call_id": "$message.call_id",
        "reason": "sub-agents cannot execute bash"
      }
    },
    {"type": "deny", "reason": "sub-agents cannot execute bash"}
  ]
}
```

Top-level loops match `parent_loop_id == ""` and are exempt; spawned
sub-agents match `parent_loop_id != ""` and get the bash deny.
