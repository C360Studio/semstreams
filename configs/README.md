# SemStreams Configurations

This directory contains configuration files for SemStreams deployment tiers and external services.

## Deployment Tiers

SemStreams supports a 3-tier deployment model with progressively more capable component configurations:

| Tier | Name | Description | External Services |
|------|------|-------------|-------------------|
| 0 | **Structural** | Base tier - rules-only, deterministic behavior | None |
| 1 | **Statistical** | Adds BM25 embeddings, statistical clustering, anomaly detection | None |
| 2 | **Semantic** | Full ML with external embedding and LLM services | semembed, seminstruct |

### Component-to-Tier Matrix

| Component | Tier 0 (Structural) | Tier 1 (Statistical) | Tier 2 (Semantic) | Description |
|-----------|:-------------------:|:--------------------:|:-----------------:|-------------|
| `graph-ingest` | Required | Required | Required | Entity/triple ingestion |
| `graph-index` | Required | Required | Required | Core relationship indexes |
| `graph-gateway` | Required | Required | Required | HTTP API (GraphQL/MCP) |
| `graph-embedding` | - | BM25 | HTTP | Vector embeddings |
| `graph-clustering` | - | + Structural | + LLM + Semantic | Community detection, structural analysis, anomaly detection |
| `graph-index-spatial` | Optional | Optional | Optional | Geospatial queries |
| `graph-index-temporal` | Optional | Optional | Optional | Time-based queries |

### Tier 0: Structural (`structural.json`)

Rules-only deployment without ML inference. Deterministic and predictable.

**Components:**
- `graph-ingest` - Entity/triple ingestion from streams
- `graph-index` - Core relationship indexing
- `graph-gateway` - GraphQL/MCP HTTP gateway
- `rule-processor` - Stateful rule evaluation

### Tier 1: Statistical (`statistical.json`)

Adds statistical algorithms for embeddings and clustering without external ML services.

**Additional Components:**
- `graph-embedding` (BM25) - Statistical term-frequency embeddings
- `graph-clustering` - Community detection with structural analysis and core anomaly detection

### Tier 2: Semantic (`semantic.json`)

Full-featured deployment with external ML services for embeddings and LLM enhancement.

**Enhanced Components:**
- `graph-embedding` (HTTP) - ML-based vector embeddings via external service
- `graph-clustering` (+ LLM) - Community detection with LLM summarization, structural analysis, and semantic gap detection
- `graph-index-spatial` - Geospatial indexing
- `graph-index-temporal` - Time-based indexing

**Required External Services:**
- `semembed:8081` - Embedding service
- `seminstruct:8083` - LLM instruction service

## Graph-Backend Deployments (`graph-backend.json`)

Some downstream consumers — typically gateways or sister-repos that publish mutations directly via `graph.mutation.triple.*` request/reply and read via `graph.index.query.*` / `graph.query.entity` / `graph.spatial.query.*` — need the framework as a **graph backend**, not as a full source-data pipeline.

`configs/graph-backend.json` wires only the five graph processors plus the minimum services:

- `graph-ingest` — mutation responder (auto-wires `graph.mutation.triple.{add,add_batch,remove}` request handlers regardless of input port declarations)
- `graph-index` — entity-state KV watcher → predicate / outgoing / incoming / alias index responders
- `graph-index-spatial` — entity-state KV watcher → spatial query responder
- `graph-index-temporal` — entity-state KV watcher → temporal query responder
- `graph-query` — `graph.query.>` request/reply responder

Plus `service-manager` + `component-manager` only. No `udp`, `iot_sensor`, `document_processor`, `objectstore`, `rule`, `graph-gateway`, file inputs/outputs — those are all part of the source-data pipeline that a gateway-backed deployment does not use.

### Dummy input port on `graph-ingest`

`graph-ingest.Config.Validate()` requires `len(Ports.Inputs) >= 1` even when the component is responding to mutation handlers rather than reading from a JetStream pipeline. The example uses a benign dummy:

```json
{"name": "unused_in", "subject": "_graph_backend.unused.ingest", "type": "nats"}
```

The `nats` port type (vs `jetstream`) is the key — declaring a `jetstream` input would force `EnsureStream` for a stream that never receives publishes.

### Port types — when to use which

| Type | Purpose | Stream / subject requirement |
|------|---------|------------------------------|
| `jetstream` | Durable JetStream subscription; messages persisted to a stream | Stream must exist (auto-created by `EnsureStream`); messages are durable |
| `nats` | Core NATS subscription; no persistence, ephemeral fan-out | Subject can be any; no stream lifecycle |
| `kv-watch` | Watch a NATS KV bucket for changes; receives every put + delete | Bucket must exist (auto-created when listed in `services`) |
| `kv` / `kv-write` | Write into a NATS KV bucket | Bucket must exist |
| `nats-request` | Reply to NATS request/reply messages on the subject | No stream; subject is a request endpoint |

`graph-ingest`'s `graph.mutation.*` handlers are wired automatically by `setupMutationHandlers` — they do not need to be declared as ports. Same for `graph-query`'s `graph.query.>` handlers (these DO appear as `nats-request` ports for documentation, but the component would still respond if the port were absent).

## Graph Component Architecture

The graph processing layer uses a modular component architecture with KV-watch based event flow:

```
Entity Streams ──► graph-ingest ──► ENTITY_STATES KV
                                          │
                                          ▼ (kv-watch)
                                    graph-index
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    ▼                     ▼                     ▼
             OUTGOING_INDEX        INCOMING_INDEX        ALIAS_INDEX

Tier 1+ Only:
        ENTITY_STATES
              │
              ▼ (kv-watch)
        graph-embedding ──► EMBEDDINGS_CACHE
              │
              ▼ (reads KV)
        graph-clustering ──┬──► COMMUNITY_INDEX
                          ├──► STRUCTURAL_INDEX (k-core, pivot)
                          └──► ANOMALY_INDEX
```

## Graph Component Configuration

### graph-ingest

Ingests entities and triples from JetStream.

**Tier:** All tiers (required)

```json
{
  "type": "processor",
  "name": "graph-ingest",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_in", "subject": "entity.>", "type": "jetstream"}
      ],
      "outputs": [
        {"name": "entity_states", "subject": "ENTITY_STATES", "type": "kv"}
      ]
    },
    "enable_hierarchy": true
  }
}
```

### graph-index

Maintains relationship indexes from entity state changes.

**Tier:** All tiers (required)

```json
{
  "type": "processor",
  "name": "graph-index",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "outgoing_index", "subject": "OUTGOING_INDEX", "type": "kv"},
        {"name": "incoming_index", "subject": "INCOMING_INDEX", "type": "kv"},
        {"name": "alias_index", "subject": "ALIAS_INDEX", "type": "kv"},
        {"name": "predicate_index", "subject": "PREDICATE_INDEX", "type": "kv"}
      ]
    },
    "workers": 4,
    "batch_size": 50
  }
}
```

### graph-embedding

Generates vector embeddings for entities.

**Tier:** Statistical (BM25), Semantic (HTTP)

```json
{
  "type": "processor",
  "name": "graph-embedding",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "embeddings", "subject": "EMBEDDINGS_CACHE", "type": "kv"}
      ]
    },
    "embedder_type": "bm25",
    "batch_size": 50,
    "cache_ttl": "1h"
  }
}
```

For Semantic tier, use `"embedder_type": "http"` with a model registry `embedding` capability configured.

### graph-clustering

Performs community detection with optional structural analysis, anomaly detection, and LLM enhancement.

**Tier:** Statistical, Semantic

```json
{
  "type": "processor",
  "name": "graph-clustering",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "communities", "subject": "COMMUNITY_INDEX", "type": "kv"}
      ]
    },
    "detection_interval": "30s",
    "batch_size": 100,
    "min_community_size": 2,
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
        "enabled": false
      }
    }
  }
}
```

**Configuration Options:**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enable_llm` | bool | `false` | Enable LLM-based community summarization (requires model registry `community_summary` capability) |
| `enable_structural` | bool | `false` | Enable k-core and pivot distance computation |
| `pivot_count` | int | `16` | Number of pivot nodes for distance indexing |
| `max_hop_distance` | int | `10` | Maximum BFS traversal depth |
| `enable_anomaly_detection` | bool | `false` | Enable anomaly detection (requires enable_structural) |
| `anomaly_config.core_anomaly.enabled` | bool | `true` | Detect core isolation anomalies |
| `anomaly_config.semantic_gap.enabled` | bool | `false` | Detect semantic-structural gaps (requires embeddings) |

For Semantic tier, enable LLM and semantic gap detection:
```json
{
  "enable_llm": true,
  "anomaly_config": {
    "semantic_gap": {
      "enabled": true,
      "min_semantic_similarity": 0.7,
      "min_structural_distance": 3
    }
  }
}
```

### graph-index-spatial (Optional)

Provides geospatial indexing for location-aware queries.

**Tier:** Any tier (optional)

```json
{
  "type": "processor",
  "name": "graph-index-spatial",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "spatial_index", "subject": "SPATIAL_INDEX", "type": "kv"}
      ]
    },
    "geohash_precision": 6,
    "workers": 4
  }
}
```

### graph-index-temporal (Optional)

Provides time-based indexing for temporal queries.

**Tier:** Any tier (optional)

```json
{
  "type": "processor",
  "name": "graph-index-temporal",
  "config": {
    "ports": {
      "inputs": [
        {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
      ],
      "outputs": [
        {"name": "temporal_index", "subject": "TEMPORAL_INDEX", "type": "kv"}
      ]
    },
    "time_resolution": "hour",
    "workers": 4
  }
}
```

### graph-gateway

HTTP gateway for GraphQL and MCP access.

**Tier:** All tiers (required)

```json
{
  "type": "gateway",
  "name": "graph-gateway",
  "config": {
    "ports": {
      "inputs": [
        {"name": "http", "subject": ":8084", "type": "http"}
      ],
      "outputs": [
        {"name": "mutations", "subject": "graph.mutation.*", "type": "nats-request"}
      ]
    },
    "graphql_path": "/graphql",
    "mcp_path": "/mcp",
    "bind_address": ":8084",
    "enable_playground": true
  }
}
```

## KV Buckets

| Bucket | Written By | Watched By | Tier |
|--------|------------|------------|------|
| `ENTITY_STATES` | graph-ingest | graph-index, graph-embedding, graph-clustering | All |
| `OUTGOING_INDEX` | graph-index | graph-clustering | All |
| `INCOMING_INDEX` | graph-index | graph-clustering | All |
| `ALIAS_INDEX` | graph-index | - | All |
| `PREDICATE_INDEX` | graph-index | - | All |
| `EMBEDDINGS_CACHE` | graph-embedding | graph-clustering | 1+ |
| `COMMUNITY_INDEX` | graph-clustering | - | 1+ |
| `STRUCTURAL_INDEX` | graph-clustering | - | 1+ |
| `ANOMALY_INDEX` | graph-clustering | - | 1+ |
| `SPATIAL_INDEX` | graph-index-spatial | - | Optional |
| `TEMPORAL_INDEX` | graph-index-temporal | - | Optional |

## Observability Stack

### Directory Structure

```
configs/
├── semantic.json               # Tier 2: Semantic configuration
├── statistical.json            # Tier 1: Statistical configuration
├── structural.json             # Tier 0: Structural configuration
├── prometheus/
│   └── prometheus.yml          # Prometheus scraping configuration
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yml  # Auto-configure Prometheus datasource
│   │   └── dashboards/
│   │       └── default.yml     # Dashboard provider configuration
│   └── dashboards/
│       └── semstreams-overview.json  # Overview dashboard
└── README.md
```

### Usage

Start observability stack:
```bash
task services:start:observability
```

Access:
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)

See `docs/OPTIONAL_SERVICES.md` for complete documentation.
