# graph-clustering

Community detection and anomaly detection component for the graph subsystem.

## Overview

The `graph-clustering` component performs community detection on the entity graph using Label Propagation
Algorithm (LPA) and detects anomalies within community contexts. When anomaly detection is enabled, it computes
K-core and pivot inputs in memory for that cycle. It optionally enhances community descriptions using an LLM.

## Architecture

```
                    ┌───────────────────┐
ENTITY_STATES ─────►│                   │
  (cycle read)      │  graph-clustering ├──► COMMUNITY_INDEX (KV)
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
- **Configurable Scheduling**: Timer-based detection cycles
- **LLM Enhancement**: Optional community summarization using LLM
- **Internal Structural Analysis**: Fresh K-core and pivot inputs for anomaly detectors
- **Anomaly Detection**: Core isolation and semantic gap detection within communities
- **Semantic Gap Detection**: Uses graph-embedding query path for similarity search

## Detection Cycle

When triggered, the component runs through these phases:

1. **Community Detection (LPA)** → COMMUNITY_INDEX
2. **Structural Computation** (when anomaly detection is enabled) → in-memory inputs
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
        {"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}
      ],
      "outputs": [
        {"name":"communities","config":{"kind":"kv-write","bucket":"COMMUNITY_INDEX"}},
        {"name":"anomalies","config":{"kind":"kv-write","bucket":"ANOMALY_INDEX"}}
      ]
    },
    "detection_interval": "30s",
    "batch_size": 100,
    "min_community_size": 2,
    "max_iterations": 100,
    "enable_llm": false,
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
| `batch_size` | int | 100 | Reserved configuration; detection is currently timer-driven |
| `min_community_size` | int | 2 | Minimum entities to form community |
| `max_iterations` | int | 100 | Max LPA iterations |
| `enable_llm` | bool | false | Enable LLM community summarization (requires model registry with `community_summary` capability) |
| `enable_anomaly_detection` | bool | false | Enable anomaly detection and its internal structural prerequisites |
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
| entity_watch | kv-watch | ENTITY_STATES | Dependency/discovery metadata for the authority bucket |

The declared `kv-watch` input is component discovery metadata. The runtime does
not start an `ENTITY_STATES` watcher from it; each scheduled cycle reads current
authority and topology state.

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| communities | kv | COMMUNITY_INDEX | Community detection results |
| anomalies | kv | ANOMALY_INDEX | Detected anomalies |

## Scheduling

Community detection runs when `detection_interval` elapses. Each tick reads the
current graph population; it does not depend on an entity-change delivery stream.

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

### Pivot Distances

Anomaly detectors use pivot distances to estimate structural separation:

1. Select k pivot nodes (high-degree or random)
2. Compute BFS distances from each pivot to all reachable nodes
3. Pass the distance vectors directly to the anomaly detectors for that cycle

These K-core and pivot results are internal prerequisites. They are not persisted
or exposed through a structural query contract.

### Anomaly Detection

**Core Isolation**: Detects entities at high k-core levels with few same-level peers within their community.

**Semantic Gap**: Detects entities that are semantically similar (high embedding similarity) but structurally distant (many hops apart). Uses graph-embedding query path.

## Dependencies

### Upstream (reads during detection)
- `graph-ingest` - owns `ENTITY_STATES`, which clustering reads on each scheduled cycle
- `graph-index` - reads OUTGOING_INDEX and INCOMING_INDEX for graph structure
- `graph-embedding` - queries for similar entities via NATS request/reply

### Downstream
- `graph-gateway` - queries community and anomaly data

### External
- LLM API service (if LLM enhancement enabled)

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_clustering_runs_total` | counter | Total detection runs |
| `graph_clustering_communities_detected` | gauge | Current community count |
| `graph_clustering_duration_seconds` | histogram | Detection run duration |
| `graph_clustering_llm_enhancements_total` | counter | LLM enhancement calls |
| `graph_clustering_anomalies_detected` | gauge | Current anomaly count |

## Health

The component reports healthy when:
- NATS and required KV dependencies initialized successfully
- Detection runs complete within timeout
- LLM API reachable (if enabled)
- NATS connection available for similarity queries (if semantic_gap enabled)
