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

The rules engine evaluates conditions against entities and executes actions when conditions match. Rules can add or remove triples, publish messages, and build dynamic relationships that affect community detection.

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

## Graph Integration

When enabled (default), rule actions directly affect the graph:

```
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

```
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
