# graph-index-spatial

Geospatial indexing component for the graph subsystem.

## Overview

The `graph-index-spatial` component watches the `ENTITY_STATES` KV bucket and maintains a geospatial index for entities with location data. This enables efficient spatial queries.

## Architecture

```
                    ┌─────────────────────┐
ENTITY_STATES ─────►│                     │
   (KV watch)       │  graph-index-spatial├──► SPATIAL_INDEX (KV)
                    │                     │
                    └─────────────────────┘
```

## Features

- **Geohash Indexing**: Efficient spatial indexing using geohash algorithm
- **Configurable Precision**: 1-12 precision levels for different use cases
- **Automatic Location Extraction**: Extracts lat/lon from entity triples
- **Batch Processing**: Efficient bulk index updates

## Configuration

```json
{
  "type": "processor",
  "name": "graph-index-spatial",
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
        {
          "name": "spatial_index",
          "config": {"kind":"kv-write","bucket":"SPATIAL_INDEX"}
        }
      ]
    },
    "geohash_precision": 6,
    "workers": 4,
    "batch_size": 100
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration |
| `geohash_precision` | int | 6 | Geohash precision (1-12) |
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
| spatial_index | kv | SPATIAL_INDEX | Geospatial index |

## Geohash Precision Guide

| Precision | Cell Size | Use Case |
|-----------|-----------|----------|
| 1 | ~5,000 km | Continental |
| 2 | ~1,250 km | Large country |
| 3 | ~156 km | State/province |
| 4 | ~39 km | City |
| 5 | ~4.9 km | Neighborhood |
| 6 | ~1.2 km | Street (default) |
| 7 | ~153 m | Block |
| 8 | ~38 m | Building |
| 9+ | <10 m | Precise location |

## Index Structure

The SPATIAL_INDEX uses geohash as key:

```json
{
  "geohash": "9q8yy",
  "entities": [
    {
      "entity_id": "c360.logistics.warehouse.sensor.gps.gps-001",
      "lat": 37.7749,
      "lon": -122.4194,
      "precision": 6
    }
  ]
}
```

## Location Extraction

The component extracts location from these predicates:

- `geo.location.latitude` / `geo.location.longitude`
- `location.lat` / `location.lon`
- `coordinates.latitude` / `coordinates.longitude`

## Spatial Queries

The gateway component uses this index for:

- **Radius search**: Find entities within N km of a point
- **Bounding box**: Find entities within lat/lon bounds
- **Geohash prefix**: Find entities in geohash cell

## Dependencies

### Upstream
- `graph-ingest` - produces ENTITY_STATES

### Downstream
- `graph-gateway` - queries spatial index

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_spatial_indexed_total` | counter | Total entities indexed |
| `graph_spatial_updates_total` | counter | Total index updates |
| `graph_spatial_errors_total` | counter | Indexing errors |

## Health

The component reports healthy when:
- KV watch subscription is active
- SPATIAL_INDEX bucket is accessible
- Index updates completing successfully
