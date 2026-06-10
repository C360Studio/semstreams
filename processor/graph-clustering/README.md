# graph-clustering

Community detection, structural analysis, and anomaly detection component for the graph subsystem.

## Overview

The `graph-clustering` component performs community detection on the entity graph using Label Propagation Algorithm (LPA), computes structural indices (k-core decomposition, pivot distances), and detects anomalies within community contexts. Optionally enhances community descriptions using LLM.

## Architecture

```
                    ┌───────────────────┐
ENTITY_STATES ─────►│                   │
   (KV watch)       │  graph-clustering ├──► COMMUNITY_INDEX (KV)
                    │                   ├──► STRUCTURAL_INDEX (KV)
                    │                   ├──► ANOMALY_INDEX (KV)
                    └─────────┬─────────┘
                              │ (reads/queries)
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
       OUTGOING_INDEX  INCOMING_INDEX  graph-embedding
                                       (query path)
```

## Features

- **Label Propagation Algorithm (LPA)**: Efficient community detection
- **Configurable Scheduling**: Timer-based or event-count triggered
- **LLM Enhancement**: Optional community summarization using LLM
- **Structural Analysis**: K-core decomposition and pivot distance indexing
- **Anomaly Detection**: Core isolation and semantic gap detection within communities
- **Semantic Gap Detection**: Uses graph-embedding query path for similarity search

## Detection Cycle

When triggered, the component runs through these phases:

1. **Community Detection (LPA)** → COMMUNITY_INDEX
2. **Structural Computation** (if enabled) → STRUCTURAL_INDEX
3. **Anomaly Detection** (if enabled) → ANOMALY_INDEX

## Configuration

```json
{
  "type": "processor",
  "name": "graph-clustering",
  "enabled": true,
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "communities", "subject": "COMMUNITY_INDEX", "type": "kv"},
        {"name": "structural", "subject": "STRUCTURAL_INDEX", "type": "kv"},
        {"name": "anomalies", "subject": "ANOMALY_INDEX", "type": "kv"}
      ]
    },
    "detection_interval": "30s",
    "batch_size": 100,
    "min_community_size": 2,
    "max_iterations": 100,
    "enable_llm": false,
    "enable_structural": true,
    "pivot_count": 16,
    "max_hop_distance": 10,
    "enable_anomaly_detection": true,
    "anomaly_config": {
      "enabled": true,
      "max_anomalies_per_run": 100,
      "core_anomaly": {
        "enabled": true,
        "min_core_for_hub_analysis": 2
      },
      "semantic_gap": {
        "enabled": false,
        "min_semantic_similarity": 0.7
      },
      "virtual_edges": {
        "auto_apply": {
          "enabled": false,
          "min_confidence": 0.95,
          "predicate_template": "inferred.semantic.{band}"
        },
        "review_queue": {
          "enabled": false,
          "min_confidence": 0.7,
          "max_confidence": 0.95
        }
      }
    }
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration |
| `detection_interval` | duration | "30s" | Time between detection runs |
| `batch_size` | int | 100 | Entity change count to trigger detection |
| `min_community_size` | int | 2 | Minimum entities to form community |
| `max_iterations` | int | 100 | Max LPA iterations |
| `enable_llm` | bool | false | Enable LLM community summarization (requires model registry with `community_summary` capability) |
| `enable_structural` | bool | false | Enable k-core and pivot computation |
| `pivot_count` | int | 16 | Number of pivot nodes for distance indexing |
| `max_hop_distance` | int | 10 | Maximum BFS traversal depth |
| `enable_anomaly_detection` | bool | false | Enable anomaly detection (requires enable_structural) |
| `anomaly_config` | object | {} | Anomaly detection configuration |

### Anomaly Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Master enable for anomaly detection |
| `max_anomalies_per_run` | int | 100 | Limit anomalies per detection cycle |
| `core_anomaly.enabled` | bool | true | Detect core isolation anomalies |
| `core_anomaly.min_core_for_hub_analysis` | int | 2 | Minimum k-core level to analyze |
| `semantic_gap.enabled` | bool | false | Detect semantic-structural gaps |
| `semantic_gap.min_semantic_similarity` | float | 0.7 | Minimum similarity for semantic gaps |
| `virtual_edges.auto_apply.enabled` | bool | false | Auto-create edges for high-confidence gaps |
| `virtual_edges.auto_apply.min_confidence` | float | 0.95 | Confidence threshold for auto-apply |
| `virtual_edges.review_queue.enabled` | bool | false | Queue uncertain gaps for review |
| `virtual_edges.review_queue.min_confidence` | float | 0.7 | Lower bound for review queue |
| `virtual_edges.review_queue.max_confidence` | float | 0.95 | Upper bound (below auto-apply) |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| entity_watch | kv-watch | ENTITY_STATES | Watch for entity changes |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| communities | kv | COMMUNITY_INDEX | Community detection results |
| structural | kv | STRUCTURAL_INDEX | K-core levels and pivot distances |
| anomalies | kv | ANOMALY_INDEX | Detected anomalies |

## Scheduling

Community detection triggers when either condition is met:

1. **Timer**: `detection_interval` elapsed since last run
2. **Batch**: `batch_size` entity changes accumulated

This ensures:
- Regular detection even with low activity
- Responsive detection during high activity

## Index Structures

### Community Index

```json
{
  "community_id": "comm-abc123",
  "members": ["entity-1", "entity-2", "entity-3"],
  "centroid": "entity-1",
  "size": 3,
  "density": 0.85,
  "summary": "Cold storage environmental sensors",
  "keywords": ["temperature", "humidity", "sensor"],
  "level": 0
}
```

### Structural Index

```json
{
  "structural.kcore._meta": {
    "entity_count": 123,
    "max_core": 15,
    "computed_at": "2024-01-15T10:30:00Z"
  },
  "structural.kcore.entity-1": {
    "core_level": 3
  },
  "structural.pivot._meta": {
    "pivot_count": 16,
    "entity_count": 123
  },
  "structural.pivot.entity-1": {
    "distances": {"pivot-1": 2, "pivot-2": 3}
  }
}
```

### Anomaly Index

```json
{
  "anomaly-uuid": {
    "id": "anomaly-uuid",
    "type": "core_isolation",
    "entity_id": "entity-1",
    "community_id": "comm-abc123",
    "severity": 0.75,
    "description": "Entity isolated at k-core level 3",
    "detected_at": "2024-01-15T10:30:00Z"
  }
}
```

## Algorithms

### Label Propagation Algorithm (LPA)

LPA works by:

1. Initialize each entity with unique label
2. Iteratively update labels to match most common neighbor label
3. Stop when labels stabilize or max_iterations reached
4. Entities with same label form a community

The algorithm considers:
- Structural edges (from OUTGOING/INCOMING indexes)

### K-Core Decomposition

K-core decomposition identifies the "coreness" of each node:

1. Iteratively remove nodes with degree < k
2. Remaining nodes form the k-core
3. Each node's core number is the maximum k for which it belongs to the k-core

Higher core numbers indicate more densely connected nodes.

### Pivot Distance Indexing

Pivot indexing enables efficient approximate shortest path queries:

1. Select k pivot nodes (high-degree or random)
2. Compute BFS distances from each pivot to all reachable nodes
3. Store distances for triangle inequality bounds

### Anomaly Detection

**Core Isolation**: Detects entities at high k-core levels with few same-level peers within their community.

**Semantic Gap**: Detects entities that are semantically similar (high embedding similarity) but structurally distant (many hops apart). Uses graph-embedding query path.

## Dependencies

### Upstream (reads during detection)
- `graph-ingest` - watches ENTITY_STATES for triggers
- `graph-index` - reads OUTGOING_INDEX and INCOMING_INDEX for graph structure
- `graph-embedding` - queries for similar entities via NATS request/reply

### Downstream
- `graph-gateway` - queries community, structural, and anomaly data

### External
- LLM API service (if LLM enhancement enabled)

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_clustering_runs_total` | counter | Total detection runs |
| `graph_clustering_communities_detected` | gauge | Current community count |
| `graph_clustering_duration_seconds` | histogram | Detection run duration |
| `graph_clustering_llm_enhancements_total` | counter | LLM enhancement calls |
| `graph_clustering_structural_runs_total` | counter | Structural computation runs |
| `graph_clustering_anomalies_detected` | gauge | Current anomaly count |

## Health

The component reports healthy when:
- KV watch subscription is active
- Detection runs complete within timeout
- LLM API reachable (if enabled)
- NATS connection available for similarity queries (if semantic_gap enabled)
