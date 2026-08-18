# Configuration

Complete configuration reference for the rule processor.

## Config Structure

```go
type Config struct {
    Ports                  *component.PortConfig
    RulesFiles             []string
    InlineRules            []Definition
    MessageCache           cache.Config
    BufferWindowSize       string
    AlertCooldownPeriod    string
    EnableGraphIntegration bool
    EntityWatchBuckets     map[string][]string
    Consumer               ConsumerConfig
}
```

## Basic Configuration

### rules_files

Paths to JSON files containing rule definitions.

```json
{
  "rules_files": [
    "/etc/semstreams/rules/alerts.json",
    "/etc/semstreams/rules/relationships.json"
  ]
}
```

**Type:** `[]string`
**Default:** `[]`

### inline_rules

Rule definitions embedded directly in config (alternative to files).

```json
{
  "inline_rules": [
    {
      "id": "battery-low",
      "conditions": [
        {"field": "drone.telemetry.battery", "operator": "lt", "value": 20}
      ],
      "on_enter": [
        {"type": "publish", "subject": "alerts.battery"}
      ]
    }
  ]
}
```

**Type:** `[]Definition`
**Default:** `[]`

### enable_graph_integration

When true, graph events and triple mutations are emitted. The default is `false`; omission disables graph-event
publication. Set the field explicitly in every deployment. A stable `pack_id` is required in both modes.

```json
{
  "pack_id": "drone-operations-rules",
  "enable_graph_integration": true
}
```

**Type:** `bool`
**Default:** `false`

The safe default is disabled because SemStreams cannot invent a stable producer identity. When disabled, triple
actions are logged but not executed. Useful for testing.

### pack_id

Stable identity for the composed rule pack. It serves both as the `rule-pack.<pack_id>` projection owner and as the
producer identity in deterministic graph rule-trigger IDs. Replicas of the same pack use the same exact value;
independently composed packs use different values. It is required regardless of `enable_graph_integration` and has
no default.

## Advanced Configuration

### entity_watch_buckets

Patterns for the rule processor's typed `EntityState` watcher. `ENTITY_STATES`
is the only supported bucket and every value must be an exact six-position
entity ID pattern.

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": [
      "acme.*.robotics.*.drone.*",
      "acme.*.environmental.*.sensor.*"
    ]
  }
}
```

**Type:** `map[string][]string`
**Default:** `{}`

See [Entity Watching](06-entity-watching.md) for pattern syntax.

Operational KV records need a separately designed typed rule adapter with their
own decoder and evaluator. They are intentionally rejected here rather than
being decoded as graph entities.

### buffer_window_size

Time window for message buffering and analysis.

```json
{
  "buffer_window_size": "10m"
}
```

**Type:** `string` (duration)
**Default:** `"10m"`

Format: Go duration string (e.g., `"30s"`, `"5m"`, `"1h"`)

### alert_cooldown_period

Minimum time between repeated alerts for the same entity-rule combination.

```json
{
  "alert_cooldown_period": "2m"
}
```

**Type:** `string` (duration)
**Default:** `"2m"`

Prevents alert spam when conditions flap rapidly.

## Port Configuration

### ports.inputs

Define input sources for rule evaluation.

```json
{
  "ports": {
    "inputs": [
      {
        "name": "entity_states",
        "config": {"kind":"kv-watch","bucket":"ENTITY_STATES"},
        "required": true,
        "description": "Watch entity state changes"
      }
    ]
  }
}
```

### ports.outputs

Define output destinations for rule actions.

```json
{
  "ports": {
    "outputs": [
      {
        "name": "control_commands",
        "config": {"kind":"nats","subject":"control.*.commands"},
        "required": false,
        "description": "Control commands based on rules"
      }
    ]
  }
}
```

## Consumer Configuration

Internal JetStream consumer settings.

```json
{
  "consumer": {
    "enabled": true,
    "ack_wait_seconds": 30,
    "max_deliver": 3,
    "replay_policy": "instant"
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable JetStream consumer |
| `ack_wait_seconds` | int | `30` | Acknowledgment timeout |
| `max_deliver` | int | `3` | Max delivery attempts |
| `replay_policy` | string | `"instant"` | `"instant"` or `"original"` |

## Message Cache Configuration

Internal message caching for windowed analysis.

```json
{
  "message_cache": {
    "enabled": true,
    "strategy": "ttl",
    "max_size": 1000,
    "ttl": "30s",
    "cleanup_interval": "15s",
    "stats_interval": "30s"
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable message caching |
| `strategy` | string | `"ttl"` | Cache eviction strategy |
| `max_size` | int | `1000` | Maximum cached messages |
| `ttl` | duration | `"30s"` | Time-to-live for entries |
| `cleanup_interval` | duration | `"15s"` | Cache cleanup frequency |
| `stats_interval` | duration | `"30s"` | Stats logging frequency |

## Configuration Defaults

```go
config, err := rule.NewConfig("my-stable-rule-pack")
if err != nil {
    return err
}
```

`NewConfig` applies the framework defaults but requires the caller to provide the
pack's stable identity. Every rule processor requires `pack_id`, whether graph
publication is enabled or disabled. The identity is static for the processor's
lifetime and has no implicit default or runtime fallback.

## Runtime Configuration

Rule definitions are the only live configuration exception. Every other field
is part of the immutable boot envelope.

| Setting | Activation |
|---------|------------|
| `rules` (individual) | Live add/update/remove |
| `enable_graph_integration` | Next successful boot |
| `entity_watch_buckets` | Next successful boot |
| `pack_id`, ports, dependencies, consumer settings | Next successful boot |

### ApplyConfigUpdate

```go
changes := map[string]any{
    "rules": map[string]any{
        "battery-low": map[string]any{
            "type": "expression",
            "conditions": [...],
        },
    },
}

err := processor.ApplyConfigUpdate(ctx, changes)
```

### GetRuntimeConfig

```go
config := processor.GetRuntimeConfig()
// Returns:
// {
//   "buffer_window_size": "10m",
//   "alert_cooldown_period": "2m",
//   "pack_id": "drone-operations-rules",
//   "enable_graph_integration": true,
//   "entity_watch_buckets": {...},
//   "rules": {...},
//   "rule_count": 5,
//   "is_running": true
// }
```

## Complete Example

```json
{
  "ports": {
    "inputs": [
      {"name":"entity_states","required":true,"config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}
    ],
    "outputs": [
      {"name":"alerts","config":{"kind":"nats","subject":"alerts.>"}}
    ]
  },

  "rules_files": [
    "/etc/semstreams/rules/alerts.json",
    "/etc/semstreams/rules/fleet.json"
  ],

  "entity_watch_buckets": {
    "ENTITY_STATES": [
      "acme.*.robotics.*.drone.*",
      "acme.*.environmental.*.sensor.*"
    ]
  },

  "buffer_window_size": "10m",
  "alert_cooldown_period": "5m",
  "pack_id": "drone-operations-rules",
  "enable_graph_integration": true,

  "consumer": {
    "enabled": true,
    "ack_wait_seconds": 30,
    "max_deliver": 3,
    "replay_policy": "instant"
  }
}
```

## Environment Variables

Configuration can be overridden via environment variables:

| Variable | Config Field |
|----------|--------------|
| `SEMSTREAMS_RULES_FILES` | `rules_files` (comma-separated) |
| `SEMSTREAMS_ENABLE_GRAPH_INTEGRATION` | `enable_graph_integration` |
| `SEMSTREAMS_ALERT_COOLDOWN` | `alert_cooldown_period` |

## Validation

Configuration is validated on load:

- `rules_files` paths must exist and be readable
- `inline_rules` must have valid structure
- `buffer_window_size` must be valid Go duration
- `alert_cooldown_period` must be valid Go duration
- `pack_id` must always be non-empty, static, 1-246 ASCII bytes, and use only `A-Z a-z 0-9 . _ = -`
- `entity_watch_buckets["ENTITY_STATES"]` values must be canonical six-position entity ID patterns

Invalid entity watch declarations fail before any watcher is created.

## Next Steps

- [Custom Rules](08-custom-rules.md) - Extending the rule system
- [Operations](09-operations.md) - Monitoring and debugging
- [Examples](10-examples.md) - Working configurations
