# graph-index

Core relationship indexing component for the graph subsystem.

## Overview

The `graph-index` component watches the `ENTITY_STATES` KV bucket and maintains four relationship indexes that enable efficient graph traversal and querying.

## Architecture

```
                    ┌─────────────────┐
                    │                 ├──► OUTGOING_INDEX (KV)
ENTITY_STATES ─────►│   graph-index   ├──► INCOMING_INDEX (KV)
   (KV watch)       │                 ├──► ALIAS_INDEX (KV)
                    │                 ├──► PREDICATE_INDEX (KV)
                    └─────────────────┘
```

## Indexes

| Index | Key | Value | Purpose |
|-------|-----|-------|---------|
| OUTGOING_INDEX | entity_id | relationships[] | Find what an entity points to |
| INCOMING_INDEX | entity_id | relationships[] | Find what points to an entity |
| ALIAS_INDEX | alias_string | entity_id | Resolve aliases to entity IDs |
| PREDICATE_INDEX | predicate | entity_ids[] | Find entities by relationship type |

## Configuration

```json
{
  "type": "processor",
  "name": "graph-index",
  "enabled": true,
  "config": {
    "ports": {
      "inputs": [
        {
          "name": "entity_watch",
          "config": {"kind":"kv-watch","bucket":"ENTITY_STATES"}
        }
      ],
      "outputs": [
        {"name":"outgoing_index","config":{"kind":"kv-write","bucket":"OUTGOING_INDEX"}},
        {"name":"incoming_index","config":{"kind":"kv-write","bucket":"INCOMING_INDEX"}},
        {"name":"alias_index","config":{"kind":"kv-write","bucket":"ALIAS_INDEX"}},
        {"name":"predicate_index","config":{"kind":"kv-write","bucket":"PREDICATE_INDEX"}}
      ]
    },
    "workers": 4,
    "batch_size": 50
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration for inputs and outputs |
| `workers` | int | 4 | Number of worker goroutines for index updates |
| `batch_size` | int | 50 | Batch size for index operations |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| entity_watch | kv-watch | ENTITY_STATES | Watch entity state changes |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| outgoing_index | kv | OUTGOING_INDEX | Outgoing relationship index |
| incoming_index | kv | INCOMING_INDEX | Incoming relationship index |
| alias_index | kv | ALIAS_INDEX | Entity alias lookup |
| predicate_index | kv | PREDICATE_INDEX | Predicate-based index |

## Index Structure

### OUTGOING_INDEX

Maps entity ID to its outgoing relationships:

```json
{
  "entity_id": "c360.logistics.warehouse.sensor.temperature.temp-001",
  "relationships": [
    {"predicate": "located_in", "object": "c360.logistics.warehouse.zone.cold-storage"},
    {"predicate": "type", "object": "temperature_sensor"}
  ]
}
```

### INCOMING_INDEX

Maps entity ID to its incoming relationships:

```json
{
  "entity_id": "c360.logistics.warehouse.zone.cold-storage",
  "relationships": [
    {"predicate": "located_in", "subject": "c360.logistics.warehouse.sensor.temperature.temp-001"},
    {"predicate": "located_in", "subject": "c360.logistics.warehouse.sensor.humidity.hum-001"}
  ]
}
```

### ALIAS_INDEX

Maps alias strings to entity IDs:

```json
{
  "temp-001": "c360.logistics.warehouse.sensor.temperature.temp-001",
  "cold-storage-temp": "c360.logistics.warehouse.sensor.temperature.temp-001"
}
```

### PREDICATE_INDEX

Maps predicates to entity IDs that have that predicate:

```json
{
  "located_in": ["c360.logistics...temp-001", "c360.logistics...hum-001"],
  "alert.active": ["c360.logistics...temp-001"]
}
```

## Dependencies

### Upstream
- `graph-ingest` - produces ENTITY_STATES that this component watches

### Downstream
- `graph-structural` - watches OUTGOING_INDEX and INCOMING_INDEX
- `graph-gateway` - reads indexes for query resolution

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_index_updates_total` | counter | Total index updates processed |
| `graph_index_latency_seconds` | histogram | Index update latency |
| `graph_index_batch_size` | histogram | Batch sizes processed |
| `graph_index_errors_total` | counter | Total indexing errors |

## Health

The component reports healthy when:
- KV watch subscription is active
- All output KV buckets are accessible
- Index update latency is within acceptable bounds
