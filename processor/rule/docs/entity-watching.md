# Entity Watching

Rules evaluate against entity state changes in NATS KV. This document covers how entity watching works and how to configure it.

## Overview

```
ENTITY_STATES bucket (NATS KV)
        │
        ▼
    KV Watcher (pattern: "acme.*.robotics.>")
        │
        ▼
    handleEntityUpdates()
        │
        ▼
    evaluateRulesForEntityState()
        │
        ▼
    Rule conditions checked
        │
        ▼
    State transition detected → Actions executed
```

## Configuration

Enable entity watching by specifying patterns:

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

If no patterns are configured, pattern-specific evaluation is disabled; the authoritative graph-state guard remains
active:

```go
if len(rp.config.EntityWatchBuckets) == 0 {
    rp.logger.Info("No rule entity patterns configured; authoritative graph guard remains active")
}
```

## Pattern Syntax

Patterns have exactly six entity-ID positions. Each position is a canonical literal segment or the complete wildcard
token `*`. `ENTITY_STATES` is the only supported bucket.

### Wildcards

| Wildcard | Meaning | Position |
|----------|---------|----------|
| `*` | Match single segment | Any |

### Examples

```text
# All drones from any org/platform
*.*.robotics.*.drone.*

# All sensors under logistics
acme.logistics.environmental.*.sensor.*

# Everything under robotics
acme.*.robotics.*.*.*

# Specific platform, any entity type
acme.logistics.*.fleet.*.*

# All canonical entities (use sparingly)
*.*.*.*.*.*
```

### Pattern to Entity Matching

| Pattern | Matches | Doesn't Match |
|---------|---------|---------------|
| `*.*.robotics.*.*.*` | `acme.platform1.robotics.fleet.drone.d007` | `acme.platform1.logistics.fleet.drone.d007` |
| `acme.*.*.*.drone.*` | `acme.prod.robotics.fleet.drone.d007` | `acme.prod.robotics.fleet.sensor.s001` |
| `acme.logistics.*.*.*.*` | `acme.logistics.environmental.sensor.temperature.s042` | `acme.production.environmental.sensor.temperature.s042` |

## KV Watcher Setup

For each configured pattern, a NATS KV watcher is created:

```go
for _, pattern := range rp.config.EntityWatchBuckets["ENTITY_STATES"] {
    watcher, err := entityBucket.Watch(ctx, pattern)
    if err != nil {
        return err
    }

    rp.entityWatchers = append(rp.entityWatchers, watcher)
    go rp.handleEntityUpdates(ctx, watcher)
}
```

Each watcher runs in its own goroutine.

## Update Handling

When an entity changes, the watcher receives a KV entry:

```go
func (rp *Processor) handleEntityUpdates(ctx context.Context, watcher jetstream.KeyWatcher) {
    for {
        select {
        case <-ctx.Done():
            return
        case entry, ok := <-watcher.Updates():
            if !ok {
                return // Channel closed
            }
            if entry == nil {
                continue // Initial state complete
            }

            // Determine action
            action := "UPDATED"
            if entry.Operation() == jetstream.KeyValueDelete {
                action = "DELETED"
            } else if entry.Revision() == 1 {
                action = "CREATED"
            }

            // Unmarshal and evaluate
            var entityState *gtypes.EntityState
            if action != "DELETED" {
                json.Unmarshal(entry.Value(), &entityState)
            }

            rp.evaluateRulesForEntityState(ctx, entry.Key(), action, entityState)
        }
    }
}
```

## Entity Actions

| Action | When | EntityState |
|--------|------|-------------|
| `CREATED` | First write (revision = 1) | Available |
| `UPDATED` | Subsequent writes (revision > 1) | Available |
| `DELETED` | Entity removed from KV | nil |

### Handling Deletions

When an entity is deleted, rule evaluation is skipped:

```go
if entityState == nil {
    rp.logger.Debug("Skipping rule evaluation for deleted entity")
    return
}
```

However, state cleanup occurs via `StateTracker.DeleteAllForEntity()` if configured.

## Rule Evaluation Path

Entity updates follow the direct evaluation path (more efficient than message-based):

```go
func (rp *Processor) evaluateRulesForEntityState(ctx context.Context, entityKey, action string, entityState *gtypes.EntityState) {
    for ruleName, ruleInstance := range rp.rules {
        // Direct EntityState evaluation (preferred)
        if entityEval, ok := ruleInstance.(EntityStateEvaluator); ok {
            triggered := entityEval.EvaluateEntityState(entityState)
            // Handle state transitions...
        }
    }
}
```

Rules must implement `EntityStateEvaluator` interface:

```go
type EntityStateEvaluator interface {
    EvaluateEntityState(entityState *gtypes.EntityState) bool
}
```

## Entity Pattern vs Rule Pattern

Two levels of filtering exist:

### 1. Entity Watch Buckets (Config Level)

Which entities trigger rule evaluation:

```json
{
  "entity_watch_buckets": {"ENTITY_STATES": ["acme.*.robotics.*.*.*"]}
}
```

Only entities matching these patterns are sent to rules.

### 2. Rule Entity Patterns (Rule Level)

Which entities this specific rule applies to:

```json
{
  "entity": {
    "pattern": "*.*.robotics.*.drone.*"
  }
}
```

An entity must match both:
1. Config pattern (to trigger evaluation)
2. Rule pattern (for rule to apply)

### Example

```json
// Config
{
  "entity_watch_buckets": {"ENTITY_STATES": ["acme.*.*.*.*.*"]}
}

// Rule 1: Drone battery
{
  "entity": {"pattern": "*.*.robotics.*.drone.*"},
  "conditions": [{"field": "drone.telemetry.battery", "operator": "lt", "value": 20}]
}

// Rule 2: Sensor temperature
{
  "entity": {"pattern": "*.*.environmental.*.sensor.*"},
  "conditions": [{"field": "sensor.measurement.celsius", "operator": "gt", "value": 100}]
}
```

Entity `acme.prod.robotics.fleet.drone.d007`:
- Matches config pattern: Yes
- Matches Rule 1 pattern: Yes → Evaluate conditions
- Matches Rule 2 pattern: No → Skip

## Performance Considerations

### Pattern Specificity

More specific patterns = fewer entities to evaluate:

```json
// Bad: Evaluates ALL entities
{"entity_watch_buckets": {"ENTITY_STATES": ["*.*.*.*.*.*"]}}

// Good: Only robotics entities
{"entity_watch_buckets": {"ENTITY_STATES": ["*.*.robotics.*.*.*"]}}

// Better: Only drones
{"entity_watch_buckets": {"ENTITY_STATES": ["*.*.robotics.*.drone.*"]}}
```

### Multiple Patterns

Each pattern creates a separate watcher and goroutine:

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": [
      "acme.prod.robotics.*.drone.*",
      "acme.prod.environmental.*.sensor.*"
    ]
  }
}
```

Two watchers, two goroutines. Keep patterns to minimum needed.

### High-Volume Entities

For high-update-rate entities, consider:
- Cooldown on rules to limit action frequency
- Specific patterns to reduce evaluation scope
- Efficient conditions (simple operators first)

## Watch Buckets

Entity watching uses the `ENTITY_STATES` bucket by default. The bucket is created if it doesn't exist:

```go
entityBucket, err := rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
    Bucket:      "ENTITY_STATES",
    Description: "Entity state storage",
    History:     10,
    TTL:         7 * 24 * time.Hour,
    MaxBytes:    -1,
})
```

Rules can also reference other buckets in their entity configuration:

```json
{
  "entity": {
    "pattern": "*.*.robotics.*.drone.*",
    "watch_buckets": ["ENTITY_STATES", "PREDICATE_INDEX"]
  }
}
```

## Graceful Shutdown

Watchers are cleaned up on processor shutdown:

```go
case <-rp.shutdown:
    return // Exit goroutine
```

The `shutdown` channel signals all watcher goroutines to exit cleanly.

## Debugging

### Verify Pattern Matching

```bash
# List all keys in ENTITY_STATES
nats kv ls ENTITY_STATES

# Check if entity matches pattern
nats kv get ENTITY_STATES "acme.prod.robotics.fleet.drone.d007"
```

### Check Watcher Status

Look for log entries:

```
INFO Started KV watcher pattern="acme.*.robotics.>"
```

### Monitor Updates

```bash
# Watch for entity changes
nats kv watch ENTITY_STATES "acme.*.robotics.*.*.*"
```

## Dynamic Pattern Updates

Entity watch patterns can be updated at runtime without restarting the processor. When patterns are changed via `ApplyConfigUpdate()`:

1. **Removed patterns**: Watchers for patterns no longer in the list are stopped
2. **New patterns**: New watchers are started for patterns not previously watched
3. **Unchanged patterns**: Existing watchers continue uninterrupted

```go
// Example: Update patterns dynamically
changes := map[string]any{
    "entity_watch_buckets": map[string][]string{
        "ENTITY_STATES": {
            "acme.*.robotics.*.drone.*",
            "acme.*.logistics.*.*.*",
        },
    },
}
processor.ApplyConfigUpdate(changes)
```

This enables:
- Adding monitoring for new entity types without downtime
- Removing patterns for decommissioned systems
- Adjusting scope based on operational needs

## Limitations

- Patterns only match entity IDs, not triple contents
- `>` and embedded wildcards are rejected
- High cardinality patterns can cause CPU pressure
- Deleted entities don't trigger exit actions (state cleaned up separately)

## Next Steps

- [Configuration](07-configuration.md) - Full configuration reference
- [Operations](09-operations.md) - Monitoring and debugging
- [Examples](10-examples.md) - Working configurations
