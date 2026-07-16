# Rule Processor

Processes message streams through configurable rules and evaluates conditions against semantic messages and entity state changes.

## What It Does

- Watches ENTITY_STATES KV bucket for entity changes
- Evaluates rules against semantic messages and entity state
- Publishes rule events and graph mutation requests
- Provides time-window analysis with message buffering

## Quick Start

```go
config, err := rule.NewConfig("example-rule-pack")
if err != nil {
    log.Fatal(err)
}
config.Ports = &component.PortsDefinition{
    Inputs: []component.PortDefinition{{Subject: "process.>"}},
    Outputs: []component.PortDefinition{{Subject: "rule.events.>"}},
}

processor, err := rule.NewProcessor(natsClient, &config)
if err != nil {
    log.Fatal(err)
}

if err := processor.Initialize(); err != nil {
    log.Fatal(err)
}

if err := processor.Start(ctx); err != nil {
    log.Fatal(err)
}
```

## Rule Definition

Rules are defined in JSON:

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

## Documentation

| Topic | Location |
|-------|----------|
| **Conceptual Overview** | [docs/advanced/06-rules-engine.md](../../docs/advanced/06-rules-engine.md) |
| **Rule Syntax** | [docs/syntax.md](docs/syntax.md) |
| **Conditions** | [docs/conditions.md](docs/conditions.md) |
| **Actions** | [docs/actions.md](docs/actions.md) |
| **State Tracking** | [docs/state-tracking.md](docs/state-tracking.md) |
| **Entity Watching** | [docs/entity-watching.md](docs/entity-watching.md) |
| **Configuration** | [docs/configuration.md](docs/configuration.md) |
| **Custom Rules** | [docs/custom-rules.md](docs/custom-rules.md) |
| **Operations** | [docs/operations.md](docs/operations.md) |
| **Examples** | [docs/examples.md](docs/examples.md) |

## Package Structure

| File | Purpose |
|------|---------|
| `processor.go` | Core processor, lifecycle, ports |
| `config.go` | Config struct and defaults |
| `factory.go` | Component registration |
| `entity_watcher.go` | KV entity state watching |
| `message_handler.go` | Message processing and rule evaluation |
| `rule_loader.go` | Rule loading from JSON files |
| `publisher.go` | Event publishing to NATS |
| `metrics.go` | Prometheus metrics |

## Metrics

Key metrics exposed:

| Metric | Type | Description |
|--------|------|-------------|
| `semstreams_rule_evaluations_total` | Counter | Rule evaluations performed |
| `semstreams_rule_triggers_total` | Counter | Successful rule triggers |
| `semstreams_rule_evaluation_duration_seconds` | Histogram | Evaluation latency |
| `semstreams_rule_active_rules` | Gauge | Active rules count |
| `semstreams_rule_errors_total` | Counter | Processing errors |

## Design Decisions

### Entity ID Format
Uses 6-part hierarchical dotted notation for global uniqueness:
```
<org>.<platform>.<system>.<domain>.<type>.<instance>
Example: c360.platform1.gcs1.robotics.drone.1
```

### Nil Safety Pattern
Metrics use "nil input = nil feature" pattern - zero overhead when disabled.

### Graph Integration
When enabled (default), rule actions directly affect the graph via `add_triple` and `remove_triple` actions.
