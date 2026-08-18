# Operations

> **⚠️ DEPRECATED**: The JSON-based rules engine is superseded by the Reactive Workflow Engine
> (ADR-021). See [Reactive Workflows Guide](/docs/advanced/10-reactive-workflows.md).

Monitoring, debugging, and maintaining the rules engine in production.

## Prometheus Metrics

The rule processor exposes metrics under the `semstreams_rule_` prefix.

### Messages

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `messages_received_total` | Counter | `subject` | Messages received for evaluation |

### Evaluations

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `evaluations_total` | Counter | `rule_name`, `result` | Rule evaluations performed |
| `evaluation_duration_seconds` | Histogram | `rule_name` | Time spent evaluating rules |
| `triggers_total` | Counter | `rule_name`, `severity` | Successful rule triggers |

### State Transitions

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `state_transitions_total` | Counter | `rule_name`, `transition` | OnEnter/OnExit transitions |

Transition values: `entered`, `exited`

### Buffer

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `buffer_size` | Gauge | `rule_name` | Current message buffer size |
| `buffer_expired_total` | Counter | `rule_name` | Messages expired from buffer |

### Cooldown

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cooldown_active` | Gauge | `rule_name` | Rules currently in cooldown |

### Events

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `events_published_total` | Counter | `subject`, `event_type` | Events published to NATS |

### Errors

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `errors_total` | Counter | `rule_name`, `error_type` | Processing errors |
| `active_rules` | Gauge | (none) | Number of active rules |

## Key Metrics to Monitor

### Rule Health

```promql
# Rule evaluation rate
rate(semstreams_rule_evaluations_total[5m])

# Rule trigger rate
rate(semstreams_rule_triggers_total[5m])

# Rule error rate
rate(semstreams_rule_errors_total[5m])
```

### Performance

```promql
# P99 evaluation latency
histogram_quantile(0.99, rate(semstreams_rule_evaluation_duration_seconds_bucket[5m]))

# Mean evaluation latency per rule
rate(semstreams_rule_evaluation_duration_seconds_sum[5m])
  / rate(semstreams_rule_evaluation_duration_seconds_count[5m])
```

### State Transitions

```promql
# Entry rate per rule
rate(semstreams_rule_state_transitions_total{transition="entered"}[5m])

# Exit rate per rule
rate(semstreams_rule_state_transitions_total{transition="exited"}[5m])
```

### Alerts

```yaml
groups:
  - name: rule_processor
    rules:
      - alert: RuleProcessorHighErrorRate
        expr: rate(semstreams_rule_errors_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High rule processing error rate

      - alert: RuleEvaluationSlow
        expr: histogram_quantile(0.99, rate(semstreams_rule_evaluation_duration_seconds_bucket[5m])) > 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: Rule evaluation latency > 500ms
```

## Debugging

### Check Rule State

View rule state for a specific entity:

```bash
# Get rule state
nats kv get RULE_STATE "battery-low.acme.prod.robotics.fleet.drone.d007"

# Output:
# {
#   "rule_id": "battery-low",
#   "entity_key": "acme.prod.robotics.fleet.drone.d007",
#   "is_matching": true,
#   "last_transition": "entered",
#   "transition_at": "2024-01-15T10:30:00Z",
#   "last_checked": "2024-01-15T10:35:00Z"
# }
```

### List All States for a Rule

```bash
nats kv ls RULE_STATE | grep "battery-low"
```

### List All States for an Entity

```bash
nats kv ls RULE_STATE | grep "drone.d007"
```

### View Entity State

```bash
nats kv get ENTITY_STATES "acme.prod.robotics.fleet.drone.d007"
```

### Check Index Updates

Verify relationships were indexed after `add_triple`:

```bash
# Check outgoing relationships
nats kv get OUTGOING_INDEX "acme.prod.robotics.fleet.drone.d007"

# Check incoming relationships
nats kv get INCOMING_INDEX "fleet.rescue"
```

### Watch Entity Changes

Monitor entity updates in real-time:

```bash
nats kv watch ENTITY_STATES "acme.*.robotics.*.*.*"
```

### View Published Messages

Subscribe to rule output subjects:

```bash
nats sub "alerts.>"
```

## Runtime Configuration

### Get Current Config

```go
config := processor.GetRuntimeConfig()
```

Returns:

```json
{
  "buffer_window_size": "10m",
  "alert_cooldown_period": "2m",
  "pack_id": "operations-rules",
  "enable_graph_integration": true,
  "entity_watch_buckets": {"ENTITY_STATES": ["acme.*.robotics.*.*.*"]},
  "rules": {...},
  "rule_count": 5,
  "is_running": true
}
```

### Update Rules at Runtime

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

Only `rules` may change live. `pack_id`, graph integration, entity-watch
patterns, ports, and dependencies are selected at boot. Persist those edits as
desired configuration and restart the process.

## Common Issues

### Rule Not Firing

**Symptoms:** Rule conditions match but no actions execute.

**Check:**
1. Is the rule enabled?
   ```bash
   # In rule definition
   "enabled": true
   ```

2. Is entity matching the watch pattern?
   ```bash
   # Check config entity_watch_buckets["ENTITY_STATES"]
   ```

3. Is cooldown active?
   ```promql
   semstreams_rule_cooldown_active{rule_name="battery-low"} > 0
   ```

4. Check previous state - may already be "entered":
   ```bash
   nats kv get RULE_STATE "battery-low.entity-id"
   ```

### Actions Not Persisting

**Symptoms:** `add_triple` actions don't show in entity state.

**Check:**
1. Is graph integration enabled?
   ```json
   {
     "pack_id": "operations-rules",
     "enable_graph_integration": true
   }
   ```

2. Check for action errors:
   ```promql
   semstreams_rule_errors_total{error_type="action_failed"}
   ```

3. Verify NATS KV connectivity.

### High Evaluation Latency

**Symptoms:** `evaluation_duration_seconds` P99 > 100ms.

**Check:**
1. Number of conditions per rule (reduce complexity)
2. Entity watch pattern breadth (make more specific)
3. Number of entities being evaluated
4. Regex conditions (expensive - use simpler operators)

### State Accumulation

**Symptoms:** `RULE_STATE` bucket growing unbounded.

**Causes:**
- Entities deleted without state cleanup
- Rule IDs changed without cleanup

**Fix:**
1. Configure TTL on RULE_STATE bucket:
   ```go
   cfg := jetstream.KeyValueConfig{
       Bucket: "RULE_STATE",
       TTL:    24 * time.Hour,
   }
   ```

2. Clean up manually:
   ```bash
   # Delete states for removed entities
   nats kv rm RULE_STATE "old-rule.old-entity"
   ```

### Flapping States

**Symptoms:** High `state_transitions_total` with alternating entered/exited.

**Causes:**
- Condition at boundary value
- Rapidly changing entity data

**Fix:**
- Add cooldown to prevent rapid transitions:
  ```json
  {"cooldown": "5m"}
  ```

- Add hysteresis (different thresholds for enter/exit) if supported

## Logging

### Enable Debug Logging

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
```

### Log Output Examples

```
DEBUG Rule entered rule_id=battery-low entity_id=drone-007 action_count=2
DEBUG Rule exited rule_id=battery-low entity_id=drone-007 action_count=1
DEBUG ExpressionRule: evaluation error rule=battery-low entity_id=drone-007 error="field not found"
WARN Failed to persist rule state rule_id=battery-low entity_key=drone-007 error="kv timeout"
```

## Performance Tuning

### Reduce Evaluation Scope

More specific patterns = fewer evaluations:

```json
// Broad (evaluates all entities)
"entity_watch_buckets": {"ENTITY_STATES": ["*.*.*.*.*.*"]}

// Specific (evaluates only drones)
"entity_watch_buckets": {"ENTITY_STATES": ["*.*.robotics.*.drone.*"]}
```

### Optimize Conditions

1. Put cheap conditions first (equality before regex)
2. Use `required: true` to fail fast on missing fields
3. Avoid expensive regex patterns

### Buffer Window

Reduce buffer if windowed analysis not needed:

```json
"buffer_window_size": "1m"
```

### Cooldown

Add cooldown to reduce action frequency:

```json
"cooldown": "5m"
```

## Health Checks

### Basic Health

```go
// Check if processor is running
config := processor.GetRuntimeConfig()
if !config["is_running"].(bool) {
    // Processor stopped
}

// Check active rules
ruleCount := config["rule_count"].(int)
if ruleCount == 0 {
    // No rules loaded
}
```

### Deep Health

```go
// Verify RULE_STATE bucket accessible
_, err := stateTracker.Get(ctx, "health-check", "probe")
if err != nil && !errors.Is(err, ErrStateNotFound) {
    // NATS KV connectivity issue
}
```

## Next Steps

- [Examples](10-examples.md) - Working rule examples
- [Configuration](07-configuration.md) - Full config reference
