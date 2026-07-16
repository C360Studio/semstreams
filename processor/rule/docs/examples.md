# Examples

Complete working rule examples for common use cases.

## Alert Rules

### Battery Low Alert

Fires when drone battery drops below threshold.

```json
{
  "id": "battery-low-alert",
  "type": "expression",
  "name": "Battery Low Alert",
  "description": "Alert when drone battery drops below 20%",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "drone",
      "required": true
    },
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 20,
      "required": true
    }
  ],
  "logic": "and",

  "cooldown": "5m",

  "entity": {
    "pattern": "*.*.robotics.*.drone.*"
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
    },
    {
      "type": "publish",
      "subject": "alerts.battery.recovered"
    }
  ]
}
```

**Behavior:**
1. Drone updates with `drone.telemetry.battery: 15`
2. Conditions match: type=drone AND battery<20
3. OnEnter fires: adds `alert.status=battery_low`, publishes alert
4. Later, battery updates to 25
5. Conditions no longer match
6. OnExit fires: removes alert.status, publishes recovery

### Temperature Threshold Alert

Fires when sensor temperature exceeds safe range.

```json
{
  "id": "temperature-critical",
  "type": "expression",
  "name": "Temperature Critical",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "sensor"
    },
    {
      "field": "sensor.measurement.celsius",
      "operator": "gt",
      "value": 100
    }
  ],
  "logic": "and",

  "entity": {
    "pattern": "*.*.environmental.*.sensor.*"
  },

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "alert.temperature",
      "object": "critical"
    },
    {
      "type": "publish",
      "subject": "alerts.temperature.critical"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "alert.temperature"
    }
  ]
}
```

### Status Change Alert

Fires when entity enters error state.

```json
{
  "id": "status-error",
  "type": "expression",
  "name": "Error Status Alert",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.status",
      "operator": "in",
      "value": ["error", "critical", "offline"]
    }
  ],

  "on_enter": [
    {
      "type": "publish",
      "subject": "alerts.status.error"
    }
  ],

  "on_exit": [
    {
      "type": "publish",
      "subject": "alerts.status.recovered"
    }
  ]
}
```

## Relationship Rules

### Fleet Assignment by Zone

Automatically groups entities by their zone.

```json
{
  "id": "fleet-by-zone",
  "type": "expression",
  "name": "Fleet Assignment by Zone",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "drone"
    },
    {
      "field": "entity.zone",
      "operator": "ne",
      "value": ""
    }
  ],
  "logic": "and",

  "entity": {
    "pattern": "*.*.robotics.*.drone.*"
  },

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "fleet.membership",
      "object": "fleet.${entity.zone}"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "fleet.membership"
    }
  ]
}
```

**Graph Impact:**
```
Before rule:
  drone-007 (isolated)
  drone-008 (isolated)

Rule fires: drone-007 enters zone "warehouse-7"
  drone-007.fleet.membership → fleet.warehouse-7

After community detection:
  Community: [drone-007, drone-008, fleet.warehouse-7]
```

### Equipment Hierarchy

Creates parent-child relationships between sensors and equipment.

```json
{
  "id": "sensor-equipment-hierarchy",
  "type": "expression",
  "name": "Sensor Equipment Attachment",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "sensor"
    },
    {
      "field": "sensor.equipment_id",
      "operator": "ne",
      "value": ""
    }
  ],
  "logic": "and",

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "equipment.parent",
      "object": "${entity.equipment_id}"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "equipment.parent"
    }
  ]
}
```

### Anomaly Signature Grouping

Groups entities with matching anomaly patterns.

```json
{
  "id": "anomaly-grouping",
  "type": "expression",
  "name": "Anomaly Signature Clustering",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.status",
      "operator": "eq",
      "value": "anomaly"
    },
    {
      "field": "entity.anomaly_signature",
      "operator": "ne",
      "value": ""
    }
  ],
  "logic": "and",

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "anomaly.signature_group",
      "object": "anomaly.${entity.anomaly_signature}"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "anomaly.signature_group"
    }
  ]
}
```

## State Machine Rules

### Equipment Maintenance State

Tracks equipment through maintenance lifecycle.

```json
{
  "id": "maintenance-state",
  "type": "expression",
  "name": "Maintenance State Machine",
  "enabled": true,

  "conditions": [
    {
      "field": "equipment.status",
      "operator": "eq",
      "value": "maintenance"
    }
  ],

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "ops.state",
      "object": "offline"
    },
    {
      "type": "add_triple",
      "predicate": "maintenance.started_at",
      "object": "${timestamp}"
    },
    {
      "type": "publish",
      "subject": "events.maintenance.started"
    }
  ],

  "on_exit": [
    {
      "type": "add_triple",
      "predicate": "ops.state",
      "object": "online"
    },
    {
      "type": "remove_triple",
      "predicate": "maintenance.started_at"
    },
    {
      "type": "publish",
      "subject": "events.maintenance.completed"
    }
  ]
}
```

## Monitoring Rules

### Continuous Battery Monitoring

Publishes status while battery remains low.

```json
{
  "id": "battery-monitoring",
  "type": "expression",
  "name": "Low Battery Monitoring",
  "enabled": true,

  "conditions": [
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 30
    }
  ],

  "cooldown": "1m",

  "on_enter": [
    {
      "type": "publish",
      "subject": "monitoring.battery.low.entered"
    }
  ],

  "while_true": [
    {
      "type": "publish",
      "subject": "monitoring.battery.low.status"
    }
  ],

  "on_exit": [
    {
      "type": "publish",
      "subject": "monitoring.battery.low.recovered"
    }
  ]
}
```

## Temporary State Rules

### Temporary Alert Suppression

Creates alert suppression that expires after TTL.

```json
{
  "id": "alert-suppression",
  "type": "expression",
  "name": "Alert Suppression",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.suppress_alerts",
      "operator": "eq",
      "value": true
    }
  ],

  "on_enter": [
    {
      "type": "add_triple",
      "predicate": "alert.suppressed",
      "object": "true",
      "ttl": "1h"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "alert.suppressed"
    }
  ]
}
```

The `alert.suppressed` triple automatically expires after 1 hour.

## Complex Condition Rules

### Multi-Condition Alert

Requires multiple conditions using AND logic.

```json
{
  "id": "multi-condition-alert",
  "type": "expression",
  "name": "Multi-Factor Alert",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.type",
      "operator": "eq",
      "value": "drone"
    },
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 20
    },
    {
      "field": "drone.telemetry.altitude",
      "operator": "gt",
      "value": 100
    },
    {
      "field": "entity.mission_active",
      "operator": "eq",
      "value": true
    }
  ],
  "logic": "and",

  "on_enter": [
    {
      "type": "publish",
      "subject": "alerts.critical.battery-altitude"
    }
  ]
}
```

### OR Logic Alert

Fires when any condition matches.

```json
{
  "id": "any-critical",
  "type": "expression",
  "name": "Any Critical Condition",
  "enabled": true,

  "conditions": [
    {
      "field": "drone.telemetry.battery",
      "operator": "lt",
      "value": 5
    },
    {
      "field": "entity.status",
      "operator": "eq",
      "value": "critical"
    },
    {
      "field": "entity.emergency",
      "operator": "eq",
      "value": true
    }
  ],
  "logic": "or",

  "on_enter": [
    {
      "type": "publish",
      "subject": "alerts.emergency"
    }
  ]
}
```

## Configuration Example

Complete configuration file with multiple rules:

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": [
      "acme.*.robotics.*.drone.*",
      "acme.*.environmental.*.sensor.*"
    ]
  },

  "pack_id": "drone-alert-rules",
  "enable_graph_integration": true,
  "alert_cooldown_period": "2m",
  "buffer_window_size": "10m",

  "inline_rules": [
    {
      "id": "battery-low",
      "type": "expression",
      "name": "Battery Low Alert",
      "enabled": true,
      "conditions": [
        {"field": "drone.telemetry.battery", "operator": "lt", "value": 20}
      ],
      "on_enter": [
        {"type": "add_triple", "predicate": "alert.status", "object": "battery_low"},
        {"type": "publish", "subject": "alerts.battery.low"}
      ],
      "on_exit": [
        {"type": "remove_triple", "predicate": "alert.status"}
      ]
    },
    {
      "id": "temp-high",
      "type": "expression",
      "name": "Temperature High",
      "enabled": true,
      "conditions": [
        {"field": "sensor.measurement.celsius", "operator": "gt", "value": 100}
      ],
      "on_enter": [
        {"type": "publish", "subject": "alerts.temperature.high"}
      ]
    },
    {
      "id": "fleet-zone",
      "type": "expression",
      "name": "Fleet Zone Assignment",
      "enabled": true,
      "conditions": [
        {"field": "entity.zone", "operator": "ne", "value": ""}
      ],
      "on_enter": [
        {"type": "add_triple", "predicate": "fleet.membership", "object": "fleet.${entity.zone}"}
      ],
      "on_exit": [
        {"type": "remove_triple", "predicate": "fleet.membership"}
      ]
    }
  ]
}
```

## Testing Rules

### Verify Rule Loaded

```bash
# Check active rules count
curl -s http://localhost:9090/metrics | grep semstreams_rule_active_rules
```

### Trigger Rule Manually

Update entity state to trigger rule:

```bash
# Put entity with low battery
nats kv put ENTITY_STATES "acme.prod.robotics.fleet.drone.drone-007" '{
  "id": "acme.prod.robotics.fleet.drone.drone-007",
  "triples": [
    {"predicate": "entity.type", "object": "drone"},
    {"predicate": "drone.telemetry.battery", "object": 15}
  ],
  "version": 1
}'
```

### Verify State Transition

```bash
# Check rule state after trigger
nats kv get RULE_STATE "battery-low.acme.prod.robotics.fleet.drone.drone-007"

# Expected output:
# {
#   "is_matching": true,
#   "last_transition": "entered",
#   ...
# }
```

### Verify Triple Added

```bash
# Check entity has new triple
nats kv get ENTITY_STATES "acme.prod.robotics.fleet.drone.drone-007" | jq '.triples'

# Should include:
# {"predicate": "alert.status", "object": "battery_low"}
```

### Subscribe to Published Messages

```bash
# Watch for alerts
nats sub "alerts.>"
```

## State Machine with KV Twofer

### Workflow Plan Status Gating

Uses the `transition` operator to enforce valid state progressions and `update_kv` to publish
the new state so all watchers receive it immediately — no separate pub/sub step needed.

```json
{
  "id": "plan-enter-drafting",
  "type": "expression",
  "name": "Plan: Enter Drafting",
  "description": "Allow drafting only from created or rejected states",
  "enabled": true,

  "conditions": [
    {
      "field": "workflow.plan.status",
      "operator": "transition",
      "from": ["created", "rejected"],
      "value": "drafting"
    }
  ],

  "entity": {
    "pattern": "*.*.workflow.*.plan.*"
  },

  "on_enter": [
    {
      "type": "update_kv",
      "bucket": "PLAN_STATES",
      "key": "$entity.triple.workflow.plan.slug",
      "payload": {
        "status": "drafting",
        "updated_at": "$now",
        "transitioned_by": "rules-engine"
      },
      "merge": true
    },
    {
      "type": "add_triple",
      "predicate": "workflow.plan.state",
      "object": "drafting"
    }
  ]
}
```

**Behavior:**

1. Entity arrives with `workflow.plan.status = "created"`. Rule evaluates — first evaluation has no
   prior history, returns false. State saved.
2. Entity updates to `workflow.plan.status = "drafting"`. Rule evaluates:
   - Previous value: `"created"` (from `RULE_STATE.FieldValues`)
   - `"created"` is in `from` list, current value matches `value` → **transition matches**
   - `on_enter` fires: `PLAN_STATES["my-plan"]` is updated with `status=drafting` and `updated_at`
   - All watchers of `PLAN_STATES` receive the change event automatically
3. If status had jumped from `"created"` to `"approved"` (skipping `"drafting"`), this rule
   would not fire. The invalid transition is silently ignored.

### Detect Any Transition to Offline

Fires whenever an entity's status becomes `offline`, regardless of prior state:

```json
{
  "id": "device-went-offline",
  "type": "expression",
  "name": "Device Went Offline",
  "enabled": true,

  "conditions": [
    {
      "field": "entity.status",
      "operator": "transition",
      "value": "offline"
    }
  ],

  "on_enter": [
    {
      "type": "update_kv",
      "bucket": "DEVICE_EVENTS",
      "key": "$entity.id",
      "payload": {
        "event": "went_offline",
        "at": "$now"
      },
      "merge": false
    },
    {
      "type": "add_triple",
      "predicate": "ops.connectivity",
      "object": "offline"
    }
  ],

  "on_exit": [
    {
      "type": "remove_triple",
      "predicate": "ops.connectivity"
    }
  ]
}
```

## Common Patterns Summary

| Pattern | Key Features |
|---------|--------------|
| Alert | OnEnter publishes, OnExit recovers |
| Relationship | OnEnter adds triple, OnExit removes |
| State Machine | OnEnter/OnExit change state triples |
| Monitoring | WhileTrue publishes periodically |
| Temporary | Uses TTL for auto-expiring triples |
| Complex | Multiple conditions with AND/OR logic |
| Transition Gate | `transition` operator enforces valid progressions |
| KV Twofer | `update_kv` write IS the event — state + watchers + history in one write |

## Next Steps

- [Rule Syntax](02-rule-syntax.md) - Complete definition reference
- [Conditions](03-conditions.md) - All operators
- [Actions](04-actions.md) - Action types
- [Operations](09-operations.md) - Debugging and monitoring
