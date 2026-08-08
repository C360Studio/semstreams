# graph-ingest

Entity and triple ingestion component for the graph subsystem.

## Overview

The `graph-ingest` component is the entry point for all entity data flowing into the graph system. It subscribes to JetStream subjects for incoming entity events and stores them in the `ENTITY_STATES` KV bucket.

## Architecture

```
                    ┌─────────────────┐
objectstore.stored ─┤                 │
                    │  graph-ingest   ├──► ENTITY_STATES (KV)
sensor.processed   ─┤                 │
                    └─────────────────┘
```

## Features

- **Entity CRUD**: Create, read, update, delete entity operations
- **Triple Mutations**: Add and remove triples on entities
- **Hierarchy Inference**: Automatically creates container entities based on 6-part entity ID structure
- **At-Least-Once Delivery**: JetStream subscription with proper acknowledgment

## Configuration

```json
{
  "type": "processor",
  "name": "graph-ingest",
  "enabled": true,
  "config": {
    "ports": {
      "inputs": [
        {
          "name": "objectstore_in",
          "config": {"kind":"jetstream","subjects":["objectstore.stored.entity"],"interface":{"type":"storage.stored.v1"}}
        },
        {
          "name": "sensor_in",
          "config": {"kind":"jetstream","subjects":["sensor.processed.entity"],"interface":{"type":"iot.sensor.v1"}}
        }
      ],
      "outputs": [
        {
          "name": "entity_states",
          "config": {"kind":"kv-write","bucket":"ENTITY_STATES"}
        }
      ]
    },
    "enable_hierarchy": true
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration for inputs and outputs |
| `enable_hierarchy` | bool | false | Enable automatic hierarchy inference |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| objectstore_in | jetstream | objectstore.stored.entity | Stored document entity events |
| sensor_in | jetstream | sensor.processed.entity | Sensor entity events |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| entity_states | kv | ENTITY_STATES | Entity state storage |

## Hierarchy Inference

When `enable_hierarchy` is enabled, the component automatically creates container entities based on the 6-part entity ID structure:

```
org.platform.domain.system.type.instance
 │      │       │      │     │      │
 └──────┴───────┴──────┴─────┴──────┴─► Real entity
        │       │      │     │
        └───────┴──────┴─────┴─► Type container (*.group)
                │      │
                └──────┴─► System container (*.container)
                       │
                       └─► Domain container (*.level)
```

This creates edges that enable:
- Efficient traversal by type, system, or domain
- Automatic grouping without explicit relationship creation
- Query-time aggregation by hierarchy level

## Dependencies

### Upstream
- None (entry point component)

### Downstream
- `graph-index` - watches ENTITY_STATES for index updates
- `graph-embedding` - watches ENTITY_STATES for embedding generation
- `graph-clustering` - watches ENTITY_STATES for community detection triggers

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_ingest_entities_processed_total` | counter | Total entities processed |
| `graph_ingest_triples_added_total` | counter | Total triples added |
| `graph_ingest_errors_total` | counter | Total processing errors |
| `graph_ingest_processing_duration_seconds` | histogram | Processing time per entity |

## Health

The component reports healthy when:
- JetStream subscription is active
- KV bucket is accessible
- No sustained error rate above threshold
