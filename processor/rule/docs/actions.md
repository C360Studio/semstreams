# Actions

Actions execute when rule conditions match. They can modify the graph, publish messages, or both.

## Action Structure

```go
type Action struct {
    Type      string                 // Action type: add_triple, remove_triple, update_triple, publish, update_kv
    Subject   string                 // NATS subject for publish actions
    Predicate string                 // Relationship type for triple actions
    Object    string                 // Target entity or value for triple actions
    TTL       string                 // Optional expiration for triples
    Bucket    string                 // KV bucket name for update_kv actions
    Key       string                 // KV key for update_kv actions
    Payload   map[string]interface{} // JSON document for update_kv actions
    Merge     bool                   // Merge into existing document (update_kv)
}
```

## Action Types

| Action | Effect | Graph Impact |
|--------|--------|--------------|
| `add_triple` | Creates relationship/property | Adds edge, affects clustering |
| `remove_triple` | Removes relationship | Removes edge, may split communities |
| `update_triple` | Replaces existing triple value | Updates edge, may affect clustering |
| `publish` | Sends NATS message | No direct graph impact |
| `update_kv` | Writes JSON to a NATS KV bucket | No direct graph impact |

## add_triple

Creates a triple on the entity:

```json
{
  "type": "add_triple",
  "predicate": "alert.status",
  "object": "battery_low"
}
```

**Result:** `entity.alert.status = "battery_low"`

### Creating Relationships

When `object` is an entity ID, it creates a graph edge:

```json
{
  "type": "add_triple",
  "predicate": "fleet.membership",
  "object": "acme.ops.fleet.warehouse-7"
}
```

**Result:**
- Triple: `drone-007.fleet.membership → acme.ops.fleet.warehouse-7`
- Edge indexed in OUTGOING_INDEX (drone-007) and INCOMING_INDEX (fleet)
- Community detection will traverse this edge

### Template Variables

Use entity data in object values:

```json
{
  "type": "add_triple",
  "predicate": "zone.membership",
  "object": "zone.${entity.zone}"
}
```

If entity has `entity.zone = "warehouse-7"`, result is:
`entity.zone.membership → zone.warehouse-7`

### TTL (Time to Live)

Create temporary triples that expire:

```json
{
  "type": "add_triple",
  "predicate": "alert.active",
  "object": "true",
  "ttl": "5m"
}
```

Triple will be automatically removed after 5 minutes.

TTL format: `"30s"`, `"5m"`, `"1h"`, `"24h"`

## remove_triple

Removes a triple from the entity:

```json
{
  "type": "remove_triple",
  "predicate": "alert.status"
}
```

**Result:** Any triple with predicate `alert.status` is removed from the entity.

### Removing Relationships

When removing relationship triples:

```json
{
  "type": "remove_triple",
  "predicate": "fleet.membership"
}
```

**Result:**
- Triple removed from entity
- Edge removed from OUTGOING_INDEX and INCOMING_INDEX
- Next community detection: entity may move to different community

## update_triple

Updates an existing triple by removing the old value and adding a new one. This is an atomic remove+add operation:

```json
{
  "type": "update_triple",
  "predicate": "status.level",
  "object": "critical"
}
```

**Result:** 
1. Any existing triple with predicate `status.level` is removed
2. New triple `entity.status.level = "critical"` is added

### Use Cases

Update triple is useful when you want to change a value without creating duplicates:

```json
{
  "type": "update_triple",
  "predicate": "battery.state",
  "object": "low"
}
```

If the entity already has `battery.state = "normal"`, this will:
1. Remove `battery.state = "normal"`
2. Add `battery.state = "low"`

### Template Variables

Like `add_triple`, you can use entity data in values:

```json
{
  "type": "update_triple",
  "predicate": "last.zone",
  "object": "$entity.current_zone"
}
```

### TTL Support

Update triple supports TTL for temporary values:

```json
{
  "type": "update_triple",
  "predicate": "alert.level",
  "object": "warning",
  "ttl": "10m"
}
```

### Behavior Notes

- If no existing triple with the predicate exists, `update_triple` behaves like `add_triple`
- The remove step is best-effort; if it fails (e.g., triple doesn't exist), the add still proceeds
- This ensures the final state is always set, even on first execution

## publish

Publishes a message to a NATS subject:

```json
{
  "type": "publish",
  "subject": "alerts.battery.low"
}
```

**Message payload includes:**
- Entity ID
- Rule ID
- Timestamp
- Entity triples snapshot

### Subject Templates

```json
{
  "type": "publish",
  "subject": "alerts.${entity.type}.critical"
}
```

## update_kv

Writes a JSON document to a named NATS KV bucket. This is the primary way rules participate in the
**KV Twofer** pattern: a single write simultaneously updates state, emits a change event to all
watchers, and appends to the revision history.

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

| Field | Required | Description |
|-------|----------|-------------|
| `bucket` | yes | Name of the NATS KV bucket to write to |
| `key` | yes | Key within the bucket (supports template variables) |
| `payload` | yes | JSON document to write (supports template variables) |
| `merge` | no | `true` = CAS read-modify-write; `false` = overwrite (default: `false`) |

### Merge vs Overwrite

**`merge: true`** performs a compare-and-swap read-modify-write cycle:

1. Read the existing document at `key`.
2. Merge `payload` fields into the existing document (top-level keys only; nested maps are merged
   one level deep).
3. Write back with CAS. On conflict, retry automatically.

Use this when multiple writers may update different fields of the same document.

**`merge: false`** overwrites the entire document unconditionally (last writer wins). Use this when
the rule owns the document exclusively.

### Template Variables in update_kv

Variable substitution applies to `bucket`, `key`, and all string values in `payload`
(including nested maps). See [Syntax: Template Variables](02-rule-syntax.md#template-variables)
for the full variable reference. The `$now` variable is particularly useful here:

```json
{
  "type": "update_kv",
  "bucket": "DEVICE_STATUS",
  "key": "$entity.id",
  "payload": {
    "state": "offline",
    "since": "$now",
    "source": "$entity.triple.entity.type"
  },
  "merge": true
}
```

### KV Twofer: The Write IS the Event

Because NATS KV delivers every write to all active watchers, an `update_kv` action functions as
both a state update and an event notification — no separate pub/sub step required.

```
Rule fires update_kv → PLAN_STATES["plan-001"] written
                     ├─ kv.Get("plan-001")  → current state (any time)
                     ├─ kv.Watch("plan-*")  → fires to all watchers immediately
                     └─ Revision history    → full audit trail
```

Typed component adapters or external services watching the bucket receive the
update automatically. The rule processor's entity watcher accepts only
`ENTITY_STATES`; operational KV rule adapters need their own typed decoder and
evaluator surface.

## How Actions Shape Communities

### Creating Communities

```
Before rule:
  drone-007 (isolated)
  drone-008 (isolated)

Rule fires add_triple: fleet.membership → fleet.rescue

After rule:
  drone-007 ──fleet.membership──> fleet.rescue
  drone-008 ──fleet.membership──> fleet.rescue

Next community detection:
  Community: [drone-007, drone-008, fleet.rescue]
```

### Splitting Communities

```
Before rule exit:
  Community: [drone-007, drone-008, sensor-alpha]

drone-007 leaves zone (rule fires remove_triple)

After rule:
  drone-008 ──fleet.membership──> fleet.rescue
  sensor-alpha ──equipment.parent──> drone-008
  drone-007 (no fleet relationship)

Next community detection:
  Community A: [drone-008, sensor-alpha]
  Community B: [drone-007] (isolated)
```

### Merging Communities

```
Before rule:
  Community A: [drone-007, drone-008]
  Community B: [sensor-alpha, sensor-beta]

Rule fires: sensor-alpha.equipment.parent → drone-007

After rule:
  sensor-alpha ──equipment.parent──> drone-007

Next community detection:
  Community: [drone-007, drone-008, sensor-alpha, sensor-beta]
```

## OnEnter vs OnExit

| Trigger | When | Typical Actions |
|---------|------|-----------------|
| `on_enter` | Condition becomes true | add_triple, publish alert |
| `on_exit` | Condition becomes false | remove_triple, publish recovery |
| `while_true` | Condition remains true | publish monitoring data |

### Balanced Enter/Exit

Every `on_enter` that adds a triple should have a corresponding `on_exit`:

```json
{
  "on_enter": [
    {"type": "add_triple", "predicate": "alert.status", "object": "active"}
  ],
  "on_exit": [
    {"type": "remove_triple", "predicate": "alert.status"}
  ]
}
```

Without `on_exit`, relationships accumulate and never clean up.

## Best Practices

### 1. Use Predictable Object Values

```json
// Good: Deterministic
{"object": "fleet.${entity.zone}"}

// Risky: Creates many unique values
{"object": "alert.${timestamp}"}
```

### 2. Balance Enter/Exit

Always pair add_triple with remove_triple.

### 3. Avoid Cycles

Don't create rules that trigger each other:

```
Rule A: If X → add relationship to Y
Rule B: If Y → add relationship to X
```

This can create infinite loops.

### 4. Use Meaningful Predicates

```json
// Good: Domain-specific
{"predicate": "fleet.membership"}
{"predicate": "equipment.parent"}

// Bad: Generic
{"predicate": "relationship"}
{"predicate": "link"}
```

### 5. Consider TTL for Temporary States

```json
{
  "type": "add_triple",
  "predicate": "alert.suppressed",
  "object": "true",
  "ttl": "1h"
}
```

## Debugging Actions

Check if actions executed:

```bash
# View entity triples
nats kv get ENTITY_STATES "drone-007" | jq '.triples'

# Check relationship index
nats kv get OUTGOING_INDEX "drone-007"
nats kv get INCOMING_INDEX "fleet.rescue"

# View rule state
nats kv get RULE_STATE "battery-low.drone-007"
```

## Limitations

- Cannot query other entities in action templates
- Cannot conditionally execute actions (all or nothing)
- Cannot chain actions (action A triggers action B)
- Object templates only support simple field references

## Next Steps

- [State Tracking](05-state-tracking.md) - When actions fire
- [Entity Watching](06-entity-watching.md) - How entities are monitored
- [Examples](10-examples.md) - Complete working rules
