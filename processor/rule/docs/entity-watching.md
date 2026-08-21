# Entity Watching

Rules evaluate against entity state changes in NATS KV. This document covers how entity watching works and how to configure it.

## Overview

```
ENTITY_STATES bucket (NATS KV)
        │
        ▼
    KV Watcher (pattern: "acme.*.robotics.*.*.*")
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

If no bucket patterns are configured, pattern-specific evaluation is disabled;
the authoritative `ENTITY_STATES` guard remains active:

```go
if len(rp.config.EntityWatchBuckets) == 0 {
    rp.logger.Info("No rule entity patterns configured; authoritative graph guard remains active")
}
```

## Pattern Syntax

`ENTITY_STATES` patterns have exactly six positions. Each position is a canonical
literal segment or the single-token wildcard `*`. The terminal wildcard `>` is
rejected because it hides arity mistakes. This watcher decodes every value as an
`EntityState`, so `ENTITY_STATES` is the only supported bucket.

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

### Safety and Ordering Model

Entity watching uses four distinct mechanisms. They solve different problems and must not be treated as one retention
or delivery feature:

| Mechanism | Responsibility |
|-----------|----------------|
| Authoritative `WatchAll` guard | Validates `ENTITY_STATES` values and advances the revision barrier |
| Pattern watcher generation | Grants dispatch authority to one exact `(bucket, pattern, generation)` registration |
| Coalescing window | Groups live work and collapses overlapping active patterns to one current-state fetch per entity |
| Per-entity evaluation fence | Orders entity work and suppresses stale revisions |

Pattern bootstrap waits for the authoritative guard and bypasses coalescing so recovery semantics keep
`Bootstrap=true`. Live managed work records its watcher key and generation. Removing and later re-adding the same
pattern creates a new generation; queued work from the retired generation is rejected before fetching current state.

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

When an entity is deleted, its canonical KV key is matched against each rule's
`entity.pattern`. Matching stateful rules evaluate `CurrentlyMatching=false`,
which allows `on_exit` to fire before tracked state is removed. The deleted
value itself is never decoded or passed to condition evaluation.

Revision admission is serialized per entity; fetch, evaluation, delete transition,
and cleanup may proceed concurrently. Revision and watcher-generation admission
suppress stale completion: a newer delete can complete without waiting for a
blocked older fetch, and that older fetch cannot publish a stale transition after
release. The same delete revision delivered by overlapping watchers is deduplicated.
Fence entries are retained while work is queued or in flight and cannot be evicted
in that state. After
the last active reference leaves, the revision watermark remains in a bounded
recent-watermark cache for 15 minutes, capped at 65,536 idle entities with LRU
eviction. These constants establish a fixed memory ceiling while covering
normal watcher overlap and reconnect replay; they are a dedupe horizon, not an
operator retention setting or replacement for KV history. Shutdown drains
queued references and clears idle watermarks.

```go
matches, err := types.MatchEntityIDPattern(rule.Entity.Pattern, entityKey)
// Matching stateful rules evaluate false, then StateTracker cleanup runs.
```

## Rule Evaluation Path

Entity updates follow the direct evaluation path (more efficient than message-based):

```go
func (rp *Processor) evaluateRulesForEntityState(ctx context.Context, entityKey, action string, entityState *gtypes.EntityState) {
    for ruleName, ruleInstance := range rp.rules {
        // Direct EntityState evaluation (preferred)
        if entityEval, ok := ruleInstance.(EntityStateEvaluator); ok {
            triggered := entityEval.EvaluateEntityState(ctx, entityState)
            // Handle state transitions...
        }
    }
}
```

Rules must implement `EntityStateEvaluator` interface:

```go
type EntityStateEvaluator interface {
    EvaluateEntityState(ctx context.Context, entityState *gtypes.EntityState) bool
}
```

The context is the exact watcher operation context. Implementations may use it for
lifecycle-backed condition resolution, but must not retain it after evaluation.

## Bootstrap Recovery and `on_recovery`-Only Rules

When the processor restarts, each pattern watcher replays `ENTITY_STATES` with
`Bootstrap=true` until it observes the nil sentinel that marks initial-state replay
complete. A rule whose persisted `MatchState.IsMatching` was `true` before the
restart, and whose entity still matches, is promoted to a synthetic `entered`
transition on that replay so its recovery leg can re-run — see
[State Recovery](state-tracking.md#state-recovery) for the transition mechanics.

A rule may declare **only** `on_recovery` — no `on_enter`, `on_exit`, or
`while_true` at all (gh#530). This expresses a pure fail-closed recovery park: the
rule does nothing on ordinary matches (no actions to fire — `on_enter`/`while_true`
are empty) but still persists `MatchState` on every live match, and fires its
`on_recovery` actions exactly once for any entity that was matching across a
restart. Entities that never matched before the restart do not fire recovery
actions, even if they happen to match on the bootstrap replay itself — recovery
requires prior persisted state (`hadPrevState`), not just a current match.

```json
{
  "id": "in-flight-work-recovery",
  "entity": {"pattern": "*.*.*.*.*.*"},
  "conditions": [{"field": "work.task.status", "operator": "eq", "value": "in_progress"}],
  "on_recovery": [
    {"type": "publish", "subject": "work.recovery.needed"}
  ]
}
```

## Entity Pattern vs Rule Pattern

Two levels of filtering exist:

### 1. Entity Watch Buckets (Config Level)

Which entities trigger rule evaluation:

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": ["acme.*.robotics.*.*.*"]
  }
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

`entity.pattern` is not a NATS subscription subject. Entity-scoped rules are
selected only on the typed `ENTITY_STATES` path; message rules use their NATS
input subjects and do not derive subscriptions from this field. The declaration
is also the lane discriminator: an omitted or empty `entity.pattern` defines a
message-path rule and is never evaluated for ENTITY_STATES bootstrap, live
updates, or deletes. Use `*.*.*.*.*.*` when an entity rule intentionally applies
to every canonical entity ID.

### Example

```json
// Config
{
  "entity_watch_buckets": {
    "ENTITY_STATES": ["acme.*.*.*.*.*"]
  }
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

Graph-ingest exclusively creates and owns the authoritative `ENTITY_STATES`
bucket. Its catalog contract is current state with history 1 and no TTL. The Rule
processor is a read-only consumer: during Start it waits for the existing bucket,
opens it without changing its configuration, and fails that watcher acquisition if
the owner does not provision the bucket within the configured startup budget.

Do not create, update, or apply alternate retention to `ENTITY_STATES` from Rule
configuration or custom rule code. Start graph-ingest before Rule, and treat a
missing bucket as an owner-provisioning or startup-order failure.

Rule-level declarations may repeat the typed bucket explicitly:

```json
{
  "entity": {
    "pattern": "*.*.robotics.*.drone.*",
    "watch_buckets": ["ENTITY_STATES"]
  }
}
```

Operational KV records such as agent-loop results are not `EntityState` values
and cannot use this watcher. A rule surface for those records requires a
separately designed typed decoder and evaluator adapter; the entity watcher does
not offer an untyped multi-bucket fallback.

## Graceful Shutdown

Shutdown first closes runtime-update admission and settles every update already
admitted. Only then does it snapshot, retire, and stop watcher generations. This
prevents a late `ApplyConfigUpdate` from publishing a watcher after the teardown
snapshot. The processor then drains admitted evaluations before canceling the Start
authority and clearing terminal state.

Watcher goroutines observe their Start-derived context:

```go
case <-ctx.Done():
    return // Exit goroutine
```

Each watcher retains only its cancel function, native handle, and completion signal;
the context itself remains lexical to the goroutine.

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
INFO Started KV watcher bucket="ENTITY_STATES" pattern="acme.*.robotics.*.*.*"
```

### Monitor Updates

```bash
# Watch for entity changes
nats kv watch ENTITY_STATES "acme.*.robotics.*.*.*"
```

## Dynamic Pattern Updates

Entity watch patterns can be updated at runtime without restarting the processor. When patterns are changed via `ApplyConfigUpdate()`:

1. **Prepare additions**: Every new transport is created before the desired set is committed; preparation failure
   stops all prepared additions and leaves the old set intact
2. **Commit authority**: New generations are published and removed generations are retired atomically under the
   dispatch gate
3. **Stop removals**: Retired physical transports are stopped after they lose authority; stop failures are reported
   but cannot revive them
4. **Unchanged patterns**: Existing watchers continue uninterrupted

```go
// Example: Update patterns dynamically
changes := map[string]any{
    "entity_watch_buckets": map[string][]string{
        "ENTITY_STATES": {
            "acme.*.robotics.*.drone.*",
            "acme.*.logistics.*.*.*", // New pattern
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
- `>` is not part of the entity-pattern language; use exactly six positions
  with `*` as a complete position when wildcard matching is required
- High cardinality patterns can cause CPU pressure
- Matching stateful rules can fire exit actions before deleted-state cleanup

## Next Steps

- [Configuration](07-configuration.md) - Full configuration reference
- [Operations](09-operations.md) - Monitoring and debugging
- [Examples](10-examples.md) - Working configurations
