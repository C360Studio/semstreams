# Rule Syntax

Rules are defined in JSON format. This document covers the complete structure of a rule definition.

## Definition Structure

```go
type Definition struct {
    ID              string                 // Unique rule identifier
    Type            string                 // Rule type (default: "expression")
    Name            string                 // Human-readable name
    Description     string                 // Rule description
    Enabled         bool                   // Whether rule is active
    Conditions      []ConditionExpression  // Conditions to evaluate
    Logic           string                 // "and" or "or" (default: "and")
    Cooldown        string                 // Minimum time between triggers (e.g., "5m")
    Entity          EntityConfig           // Entity pattern and watch configuration
    Metadata        map[string]interface{} // Custom metadata
    OnEnter         []Action               // Actions on false→true transition
    OnExit          []Action               // Actions on true→false transition
    WhileTrue       []Action               // Actions while condition remains true
    RelatedPatterns []string               // For pair rules (advanced)
}
```

## Complete Example

```json
{
  "id": "battery-low-alert",
  "type": "expression",
  "name": "Battery Low Alert",
  "description": "Alert when drone battery drops below 20%",
  "enabled": true,

  "conditions": [
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 20,
      "required": true
    },
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "drone",
      "required": true
    }
  ],
  "logic": "and",

  "cooldown": "5m",

  "entity": {
    "pattern": "*.*.robotics.*.drone.*",
    "watch_buckets": ["ENTITY_STATES"]
  },

  "metadata": {
    "severity": "warning",
    "team": "operations"
  },

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "alert.status",
      "object": "battery_low"
    },
    {
      "type": "publish",
      "subject": "alerts.battery.low"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "alert.status"
    }
  ],

  "while_true": [
    {
      "type": "publish",
      "subject": "monitoring.battery.critical"
    }
  ]
}
```

## Field Reference

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the rule |
| `conditions` | array | At least one condition expression |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | `"expression"` | Rule type |
| `name` | string | `""` | Human-readable name |
| `description` | string | `""` | Rule description |
| `enabled` | bool | `false` | Whether rule is active |
| `logic` | string | `"and"` | How to combine conditions |
| `cooldown` | string | `""` | Minimum time between triggers |
| `entity` | object | `{}` | Entity pattern configuration |
| `metadata` | object | `{}` | Custom metadata |
| `on_enter` | array | `[]` | Actions on entering true state |
| `on_exit` | array | `[]` | Actions on exiting true state |
| `while_true` | array | `[]` | Actions while condition is true |

## Entity Configuration

```json
{
  "entity": {
    "pattern": "acme.*.robotics.*.drone.*",
    "watch_buckets": ["ENTITY_STATES"]
  }
}
```

| Field | Description |
|-------|-------------|
| `pattern` | Exact six-position entity ID pattern |
| `watch_buckets` | Optional typed bucket declaration; only `ENTITY_STATES` is accepted |

**Pattern Syntax:**

- `*` - matches single segment
- `>` - rejected; it hides entity-ID arity mistakes

**Examples:**

```text
acme.*.robotics.*.drone.*     # Drones from any platform/system
*.*.*.*.sensor.*              # All sensors
acme.logistics.*.*.*.*        # Everything in logistics
```

## Condition Syntax

See [Conditions](03-conditions.md) for complete operator reference.

```json
{
  "conditions": [
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 20,
      "required": true
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `field` | string | Predicate path to evaluate |
| `operator` | string | Comparison operator |
| `value` | any | Value to compare against |
| `from` | string or array | Allowed previous values (`transition` operator only) |
| `required` | bool | Fail if field is missing |

## Action Syntax

See [Actions](04-actions.md) for complete action reference.

```json
{
  "on_enter": [
    {"type": "add_triple", "predicate": "alert.status", "object": "active"},
    {"type": "publish", "subject": "alerts.new"}
  ]
}
```

### Action Types

| Type | Description | Required Fields |
|------|-------------|-----------------|
| `add_triple` | Create relationship/property | `predicate`, `object` |
| `remove_triple` | Remove relationship | `predicate` |
| `update_triple` | Replace existing triple value | `predicate`, `object` |
| `publish` | Send NATS message | `subject` |
| `update_kv` | Write JSON to a NATS KV bucket | `bucket`, `key`, `payload` |

## Logic Operators

```json
{"logic": "and"}  // ALL conditions must match (default)
{"logic": "or"}   // ANY condition must match
```

## Cooldown

Prevents rule from firing repeatedly:

```json
{"cooldown": "5m"}   // 5 minutes
{"cooldown": "30s"}  // 30 seconds
{"cooldown": "1h"}   // 1 hour
```

When cooldown is active, the rule won't fire `on_enter` again until the cooldown expires, even if conditions are met.

## Template Variables

Object, predicate, and `update_kv` fields support template variables:

```json
{
  "object": "zone.${entity.zone}",
  "predicate": "status.${entity.status_type}"
}
```

| Variable | Description |
|----------|-------------|
| `${entity.field}` | Field from entity triples |
| `$entity.id` | Primary entity ID |
| `$entity.triple.<predicate>` | Specific triple value from the entity |
| `$related.id` | Related entity ID (pair rules) |
| `$now` | Current UTC timestamp in RFC3339 format |

`$now` is particularly useful in `update_kv` payloads to timestamp state changes:

```json
{
  "type": "update_kv",
  "bucket": "PLAN_STATES",
  "key": "$entity.triple.workflow.plan.slug",
  "payload": {
    "status": "drafting",
    "updated_at": "$now"
  },
  "merge": true
}
```

## Loading Rules

### From Files

```json
{
  "rules_files": [
    "/etc/semstreams/rules/alerts.json",
    "/etc/semstreams/rules/relationships.json"
  ]
}
```

### Inline

```json
{
  "inline_rules": [
    {
      "id": "rule-1",
      "conditions": [...],
      "on_enter": [...]
    }
  ]
}
```

## Validation

Rules are validated on load:

- `id` must be non-empty and unique
- `conditions` must have at least one entry
- `logic` must be "and" or "or"
- `cooldown` must be valid duration format
- Action `type` must be recognized
- Required action fields must be present

## Next Steps

- [Conditions](03-conditions.md) - Operator reference
- [Actions](04-actions.md) - Action types and graph impact
- [State Tracking](05-state-tracking.md) - OnEnter/OnExit behavior
