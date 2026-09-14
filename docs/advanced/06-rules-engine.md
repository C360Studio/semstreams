# Rules Engine

> **Status: current.** The JSON rules engine (`processor/rule/`) is the blessed
> orchestration path in SemStreams — *rules trigger, components execute*. See
> [Orchestration Layers](../concepts/14-orchestration-layers.md) for the full
> pattern catalog and the Rule Engine / Component boundary.
>
> An earlier revision of this page marked the rules engine DEPRECATED in favor of
> a "Reactive Workflow Engine." That is backwards and has been corrected: the
> `processor/reactive` engine was **retired** (2026-03-12) and *replaced by*
> coordinated rules over lifecycle-managed graph entities
> ([ADR-047](../adr/047-lifecycle-harness-substrate.md)). The
> [Reactive Workflows](./10-reactive-workflows.md) page is a legacy reference only.

---

The rules engine evaluates conditions against entities and executes actions when conditions match. Rules can
add or remove triples, publish messages, and build dynamic relationships that affect community detection.

## What Rules Do

Rules are stateful evaluators that:

- Watch entity state changes in NATS KV
- Evaluate conditions against entity triples
- Execute actions on state transitions (enter/exit/while)
- Create and remove relationships dynamically

## Quick Example

A battery alert rule:

```json
{
  "id": "battery-low",
  "name": "Battery Low Alert",
  "enabled": true,
  "conditions": [
    {"field": "drone.telemetry.battery", "operator": "lt", "value": 20}
  ],
  "on_enter": [
    {"type": "add_triple", "predicate": "alert.status", "object": "battery_low"},
    {"type": "publish", "subject": "alerts.battery"}
  ],
  "on_exit": [
    {"type": "remove_triple", "predicate": "alert.status"}
  ]
}
```

When `drone-007` reports battery at 15%:

1. Condition evaluates: `15 < 20` = true
2. State transition: none -> entered
3. Actions: triple added, message published

When battery recovers to 25%:

1. Condition: `25 < 20` = false
2. State transition: entered -> exited
3. Exit action: alert triple removed

## State Transitions

| Transition | Trigger | Actions |
|------------|---------|---------|
| **Entered** | false -> true | `on_enter` |
| **Exited** | true -> false | `on_exit` |
| **While True** | true -> true | `while_true` |

State is persisted in `RULE_STATE` KV bucket.

## Action Types

Every entry in `on_enter`, `on_exit` or `while_true` carries a `type`. The twelve supported values are the
`ActionType*` constants in [`processor/rule/actions.go:31-76`](../../processor/rule/actions.go):

| `type` | What it does |
|--------|--------------|
| `publish` | Publishes a message to a NATS subject |
| `add_triple` | Creates a relationship triple in the graph |
| `remove_triple` | Removes a relationship triple from the graph |
| `update_triple` | Updates metadata on an existing triple |
| `reconcile_predicates` | Replaces or clears a named projection group's predicates atomically |
| `publish_agent` | Triggers an agentic loop by publishing a task message |
| `update_kv` | Writes JSON to a named KV bucket, with optional CAS merge |
| `deny` | Issues a deny verdict and short-circuits the remaining actions |
| `approve` | Issues an approve verdict; unlike `deny`, later actions still run |
| `lifecycle_transition` | Moves a lifecycle-managed entity to a new phase (ADR-047) |
| `lifecycle_complete` | Moves it to the first reachable terminal phase |
| `lifecycle_fail` | Moves it to the declared failed phase, carrying a reason |

### publish_agent

`publish_agent` is how a rule hands work to an agent. It publishes a task message that `agentic-loop` picks up
and runs as a loop.

```json
{
  "type": "publish_agent",
  "subject": "agent.task.research",
  "role": "researcher",
  "model": "general",
  "tools": ["read_loop_result", "web_search"],
  "prompt": "Research the question submitted to loop $entity.id, then submit plain-text findings."
}
```

**Required.** All four are validated before the action runs; a missing one fails it with, for example,
`subject is required for publish_agent action`
([`processor/rule/actions.go:1687-1698`](../../processor/rule/actions.go)).

| Field | Meaning |
|-------|---------|
| `subject` | NATS subject the task is published on. Must match `agentic-loop`'s `agent.task` port, which subscribes to `agent.task.*` — one token after the prefix ([`processor/agentic-loop/config.go:396`](../../processor/agentic-loop/config.go)) |
| `role` | Agent role, e.g. `general`, `researcher`, `editor` |
| `model` | Model endpoint name, as configured on `agentic-model` |
| `prompt` | Task prompt. Substitution applies — see below |

**Optional.** The rest of the `publish_agent` surface, from the `Action` struct's JSON tags in
[`processor/rule/actions.go:89-444`](../../processor/rule/actions.go):

| Field | Meaning |
|-------|---------|
| `tools` | Per-agent tool allowlist. Unset falls back to global tool discovery |
| `properties` | Metadata stamped onto the task and onto every tool call it spawns. Reserved `agent.*` keys are skipped with a warning |
| `action_allowlist` | Closed set of values the spawned loop's `decide` tool will accept |
| `response_format` | Constrains the spawned loop's output to JSON or a JSON schema (ADR-034) |
| `tool_choice` | Constrains tool selection per iteration (ADR-023) |
| `related_loops` | Loop-ID lineage threaded onto the task so a role can read upstream results |
| `filesystem_policy` / `scratch_paths` | Task-scoped read-only execution policy and its in-worktree exemptions (ADR-067) |
| `run_scope` | `new`, `inherit`, `none`; controls agent-run association. Empty means `inherit` (ADR-053) |
| `workflow_slug` / `workflow_step` | Names the workflow and the step within it |
| `loop_max_iterations` | Iteration budget for the **spawned loop** |
| `max_iterations` | Firing cap for **this action** per rule+entity match cycle. Omitted means 3; `0` means unlimited |
| `id` | Stable identifier for that firing cap, so it survives action renames |
| `when` | Guard conditions; all must match for this action to run |
| `for_each` / `for_each_var` | Iterate the action over a triple list, binding each item to `$<for_each_var>` (ADR-046) |

**Substitution is `$`-prefixed.** `$entity.id`, the entity-ID segments (`$entity.org` … `$entity.instance`),
`$entity.triple.<predicate>`, `$message.<field>`, `$state.iteration` and `$now` are the tokens; the
authoritative list is the doc comment on `SubstituteVariables`
([`processor/rule/execution_context.go:207-252`](../../processor/rule/execution_context.go)). On a
`publish_agent` action they are resolved in `subject`, `prompt`, `role`, `workflow_slug`, `workflow_step`,
`loop_max_iterations`, string-valued `properties`, and `related_loops` values
([`processor/rule/actions.go:1498,1562,1768-1787`](../../processor/rule/actions.go)). **`model` is not
substituted** — it is passed through as written, so a `$`-token there reaches the model registry as a literal
endpoint name.

There is no `text/template` engine on this path, so `{{...}}` and `${...}` are not substitution syntax. A token
the engine does not recognize survives into the output verbatim and logs an unresolved-template warning.

Worked examples: the [agentic quickstart](../basics/07-agentic-quickstart.md#rule-triggered-agents) and the
shipped rule packs under [`configs/rules/deep-research/`](../../configs/rules/deep-research/).

## Graph Integration

When enabled (default), rule actions directly affect the graph:

```text
add_triple(predicate: "fleet.membership", object: "fleet-123")
     |
     v
Entity Triple: drone-007.fleet.membership -> fleet-123
     |
     v
Indexes Updated: OUTGOING_INDEX, INCOMING_INDEX
     |
     v
Community Detection: drone-007 clusters with fleet-123
```

Rules don't just alert - they build graph structure.

## Common Use Cases

**Alerting:**

```json
{"conditions": [{"field": "sensor.celsius", "operator": "gt", "value": 100}],
 "on_enter": [{"type": "publish", "subject": "alerts.temperature"}]}
```

**Dynamic Relationships:**

```json
{"conditions": [{"field": "drone.zone", "operator": "ne", "value": ""}],
 "on_enter": [{"type": "add_triple", "predicate": "zone.membership", "object": "zone.${entity.zone}"}],
 "on_exit": [{"type": "remove_triple", "predicate": "zone.membership"}]}
```

**State Machines:**

```json
{"conditions": [{"field": "equipment.status", "operator": "eq", "value": "maintenance"}],
 "on_enter": [{"type": "add_triple", "predicate": "ops.state", "object": "offline"}],
 "on_exit": [{"type": "add_triple", "predicate": "ops.state", "object": "online"}]}
```

## Architecture

```text
RuleProcessor
├── EntityWatcher (KV Watch) ───┐
├── MessageHandler              ├──> StatefulEvaluator
│                               │    ├── ExprRule 1
│                               │    ├── ExprRule 2
│                               │    └── RULE_STATE bucket
│                               │
└── ActionExecutor <────────────┘
    ├── GraphIntegration (add_triple, remove_triple)
    └── Publisher (publish)
```

## Limitations

- Rules evaluate single entity state only (no cross-entity conditions)
- No access to community membership from rules
- Each entity evaluated individually (no bulk operations)

## Detailed Reference

For complete documentation, see the package reference:

- [processor/rule/docs/](../../processor/rule/docs/) - Full reference documentation
- [processor/rule/README.md](../../processor/rule/README.md) - Package overview and API
