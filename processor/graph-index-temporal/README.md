# graph-index-temporal

Temporal indexing component for the graph subsystem.

## Overview

The `graph-index-temporal` component watches the `ENTITY_STATES` KV bucket and maintains a time-based index for entities. This enables efficient time-range queries.

## Architecture

```
                    ┌──────────────────────┐
ENTITY_STATES ─────►│                      │
   (KV watch)       │  graph-index-temporal├──► TEMPORAL_INDEX (KV)
                    │                      │
                    └──────────────────────┘
```

## Features

- **Configurable Resolution**: minute, hour, or day granularity
- **Automatic Timestamp Extraction**: Extracts timestamps from entity data
- **Time Bucket Keys**: Efficient range queries using bucket keys
- **Batch Processing**: Efficient bulk index updates

## Configuration

```json
{
  "type": "processor",
  "name": "graph-index-temporal",
  "enabled": true,
  "config": {
    "ports": {
      "inputs": [
        {
          "name": "entity_watch",
          "subject": "ENTITY_STATES",
          "type": "kv-watch"
        }
      ],
      "outputs": [
        {
          "name": "temporal_index",
          "subject": "TEMPORAL_INDEX",
          "type": "kv"
        }
      ]
    },
    "time_resolution": "hour",
    "workers": 4,
    "batch_size": 100
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration |
| `time_resolution` | string | "hour" | Resolution: "minute", "hour", or "day" |
| `workers` | int | 4 | Number of worker goroutines |
| `batch_size` | int | 100 | Batch size for index updates |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| entity_watch | kv-watch | ENTITY_STATES | Watch entity state changes |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| temporal_index | kv | TEMPORAL_INDEX | Temporal index |

## Time Resolution Guide

| Resolution | Key Format | Use Case |
|------------|------------|----------|
| minute | 2024-01-15T10:30 | Real-time monitoring |
| hour | 2024-01-15T10 | Operational dashboards |
| day | 2024-01-15 | Historical analysis |

## Index Structure

The TEMPORAL_INDEX uses time bucket as key:

```json
{
  "time_bucket": "2024-01-15T10",
  "entities": [
    {
      "entity_id": "c360.logistics.warehouse.sensor.temperature.temp-001",
      "timestamp": "2024-01-15T10:30:00Z",
      "event_type": "updated"
    }
  ]
}
```

## Timestamp Resolution

The component keys each entity on a timestamp chosen by explicit precedence:

1. `time.observation.recorded` triple — **event-time** (the latest value when several are present). This
   is the canonical predicate; emit it to index an entity by *when it was observed*.
2. Entity `UpdatedAt` field — **processing-time** (last write), used only as a fallback when no
   observation predicate is present.

The `entities_indexed_total{source="observed"|"write_fallback"}` metric reports how many entities use
each path, so the fallback is observable and can be retired as producers adopt the predicate.

Re-observation moves an entity to its new time bucket (the prior bucket entry is cleaned up via the
`TEMPORAL_INDEX_REVERSE` map), and entity deletion removes it — so range queries never return an entity
from a window it has since left.

## Upgrading (event-time flip, gh#370)

This index now keys on event-time (`time.observation.recorded`) instead of write-time
(`UpdatedAt`), and tracks each entity's current bucket in `TEMPORAL_INDEX_REVERSE` for cleanup.
Entities indexed by an **earlier** version of this component have no reverse-map entry, so the
stale-entry cleanup cannot locate their old (write-time) bucket — those entries are orphaned.

`TEMPORAL_INDEX` is a derived, rebuildable index. On upgrade, **purge `TEMPORAL_INDEX` and
`TEMPORAL_INDEX_REVERSE`**; the component re-delivers every entity via `WatchAll` on start and
rebuilds both cleanly into event-time buckets. Skipping the purge is non-fatal (queries still
return current entities) but leaves pre-upgrade orphans in old buckets until the purge is done.

## Temporal Queries

The gateway component uses this index for:

- **Time range**: Find entities modified between two timestamps
- **Time bucket**: Find entities in specific hour/day
- **Recent**: Find entities modified in last N hours

## Dependencies

### Upstream
- `graph-ingest` - produces ENTITY_STATES

### Downstream
- `graph-gateway` - queries temporal index

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `semstreams_graph_index_temporal_entities_indexed_total{source}` | counter | Entities indexed, labelled `observed` (event-time) or `write_fallback` (UpdatedAt). A rising `write_fallback` ratio means producers have not adopted `time.observation.recorded`. |
| `semstreams_graph_index_temporal_stale_bucket_removals_total` | counter | Entity events removed from a prior bucket on re-index/delete. |
| `semstreams_graph_index_temporal_reverse_index_errors_total` | counter | Reverse-map write/delete failures (forward/reverse drift). |

## Health

The component reports healthy when:
- KV watch subscription is active
- TEMPORAL_INDEX bucket is accessible
- Index updates completing successfully
