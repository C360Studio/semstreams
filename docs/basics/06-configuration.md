# Configuration: Progressive Enhancement

SemStreams capabilities are controlled entirely by JSON configuration. Enable features as needed—start simple, add capabilities progressively.

## Common Configurations

These are typical configuration presets. Mix and match features based on your needs.

| Config | Name | Capabilities | Requirements |
|--------|------|--------------|--------------|
| **Rules-Only** | Tier 0 | Stateful rules, explicit relationships | NATS only |
| **Native** | Tier 1 | + BM25 search, statistical communities | Same as above |
| **LLM** | Tier 2 | + Neural embeddings, LLM summaries | + semembed + LLM service |

> **Default**: Native configuration. The Graph processor enables BM25 embeddings by default. For Rules-Only
(no search), explicitly disable embeddings in your config.

## Static Config and UI Flows

SemStreams supports two operational modes that share the same underlying infrastructure.

### Headless Mode (Static Config)

Start with a JSON config file for automated deployments:

```bash
semstreams --config config.json
```

Components defined in the config start automatically. Ideal for production and CI/CD.

### UI Mode (Visual Flow Builder)

> **WIP**: The visual flow builder UI is under active development in the `semstreams-ui` repository, planned for beta release. Backend APIs (Flow CRUD, component lifecycle, live metrics) are available now.

Design flows visually with drag-and-drop components, real-time validation, and live metrics. The UI
connects to the same APIs that power headless mode.

### Static Config → Flow Bridge

When you start with a static config, SemStreams automatically creates a Flow in the flows bucket.
This makes your static configuration visible and controllable through the UI:

- **First boot**: Static config → Flow created in KV
- **Subsequent boots**: KV wins (UI customizations preserved)
- **Reset**: Delete flow from KV to restore static config

This allows you to start in headless mode for deployment, then connect the UI later to monitor
and adjust the running flow.

See [Flow Architecture](../concepts/12-flow-architecture.md) for details on the dual-bucket design
and lifecycle operations.

## Component-Based Deployment

SemStreams uses a component-based architecture where capabilities are provided by specialized components
rather than a monolithic processor. Each tier requires a specific set of components.

### Component to Tier Mapping

| Component | Tier | Purpose | Output Buckets |
|-----------|------|---------|----------------|
| **graph-ingest** | Core (All) | Entity ingestion | `ENTITY_STATES` |
| **graph-index** | Core (All) | Relationship indexing | `OUTGOING_INDEX`, `INCOMING_INDEX`, `ALIAS_INDEX`, `PREDICATE_INDEX` |
| **graph-query** | Core (All) | Query coordinator | N/A (read-only) |
| **graph-gateway** | Core (All) | Query gateway | N/A (read-only) |
| **graph-clustering** | Tier 1+ | Communities; optional anomalies | `COMMUNITY_INDEX`, `ANOMALY_INDEX` |
| **graph-embedding** | Statistical/Semantic (Tier 1+) | Vector embeddings | `EMBEDDING_INDEX`, `EMBEDDING_DEDUP` |
| **graph-index-spatial** | Core (All) | Geospatial indexing | `SPATIAL_INDEX` |
| **graph-index-temporal** | Core (All) | Temporal indexing | `TEMPORAL_INDEX` |

### Deployment Configurations by Tier

**Rules-Only (Tier 0)**:

```text
graph-ingest → graph-index → graph-query → graph-gateway
```

**Statistical (Tier 1)**:

```text
graph-ingest → graph-index → graph-query → graph-gateway
              ↓            ↓
         graph-clustering  graph-embedding (BM25)
```

**Semantic (Tier 2)**:

```text
graph-ingest → graph-index → graph-query → graph-gateway
              ↓            ↓            ↓
         graph-clustering  graph-embedding (HTTP)
              ↓
     graph-index-spatial, graph-index-temporal
```

### Component Dependencies

Components must start in the correct order based on their bucket dependencies:

| Component | Depends On | Reason |
|-----------|------------|--------|
| graph-ingest | None | Creates `ENTITY_STATES` |
| graph-index | graph-ingest | Watches `ENTITY_STATES` |
| graph-query | graph-ingest, graph-index | Routes queries to all components |
| graph-gateway | All others | Reads all buckets |
| graph-clustering | graph-ingest, graph-index | Watches `ENTITY_STATES`, reads indexes |
| graph-embedding | graph-ingest | Watches `ENTITY_STATES` |
| graph-index-spatial | graph-ingest | Watches `ENTITY_STATES` |
| graph-index-temporal | graph-ingest | Watches `ENTITY_STATES` |

**Recommended Startup Order**:

1. graph-ingest
2. graph-index
3. graph-clustering, graph-embedding, graph-index-spatial, graph-index-temporal (parallel)
4. graph-query (after all processors)
5. graph-gateway (last)

### Port Configuration

Each component exposes query capabilities via NATS request-reply on configurable ports:

```json
{
  "type": "graph-ingest",
  "config": {
    "ports": {
      "nats_request_port": "graph.ingest.query",
      "inputs": [
        {"name": "entity_stream", "type": "jetstream", "subject": "entity.>"}
      ],
      "outputs": [
        {"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"}
      ]
    }
  }
}
```

**Default Query Ports**:

| Component | Query Subject | Operations |
|-----------|---------------|------------|
| graph-ingest | `graph.ingest.query.*` | `getEntity`, `getBatch` |
| graph-index | `graph.index.query.*` | `getOutgoing`, `getIncoming`, `getAlias`, `getPredicate` |
| graph-query | `graph.query.*` | `entity`, `relationships`, `pathSearch`, `capabilities` |

### graph-query Component

The graph-query component provides unified query routing and orchestration across all graph components:

- **Unified Query Routing**: Routes queries to appropriate components (graph-ingest, graph-index, etc.)
- **PathRAG Traversal**: Orchestrates multi-hop graph traversal for path-based retrieval
- **Capability Aggregation**: Collects and exposes capabilities from all available components

**Example Configuration**:

```json
{
  "type": "graph-query",
  "config": {
    "query_timeout": "5s",
    "max_depth": 10,
    "ports": {
      "inputs": [
        {"name": "query_entity", "type": "nats-request", "subject": "graph.query.entity"},
        {"name": "query_relationships", "type": "nats-request", "subject": "graph.query.relationships"},
        {"name": "query_path_search", "type": "nats-request", "subject": "graph.query.pathSearch"},
        {"name": "query_capabilities", "type": "nats-request", "subject": "graph.query.capabilities"}
      ]
    }
  }
}
```

For full component details, see [Graph Components Reference](../advanced/07-graph-components.md).

## Rules-Only Configuration

Deterministic processing with stateful rules. No search, no external services.

### Required Components

- **graph-ingest** - Entity ingestion
- **graph-index** - Relationship indexing
- **graph-query** - Query coordinator
- **graph-gateway** - Query interface

### Capabilities

- Stateful rules (OnEnter/OnExit/WhileTrue)
- Graph actions: `add_triple`, `remove_triple`, `publish`
- Index queries: predicate, alias, outgoing, incoming
- PathRAG: Traverse explicit edges

### Not Available

- Embeddings (no vectors)
- Community detection
- In-memory structural inputs for anomaly detection
- Anomaly detection
- Semantic search
- GraphRAG

### Example Configuration

**graph-embedding component** (disabled or not deployed):

```json
{
  "type": "graph-embedding",
  "enabled": false
}
```

**graph-clustering component** (disabled or not deployed):

```json
{
  "type": "graph-clustering",
  "enabled": false
}
```

### When to Use

- Edge deployments with limited compute
- Environments requiring full auditability
- Low-latency alerting
- Deterministic state machines

## Native Inference Configuration

Statistical capabilities that run locally. No external services required.

### Required Components

All Rules-Only components, plus:

- **graph-clustering** - Label Propagation Algorithm (LPA) community detection
- **graph-embedding** - BM25 statistical embeddings

### Native Capabilities

Everything in Rules-Only, plus:

- BM25 embeddings (384-D vectors)
- LPA clustering (community detection)
- Statistical summaries (keywords, top terms)
- Semantic search (BM25 fallback)
- GraphRAG with statistical summaries

### Not Available

- Neural embeddings
- LLM summaries

### Example Configuration

**graph-embedding component**:

```json
{
  "type": "graph-embedding",
  "config": {
    "embedder_type": "bm25",
    "batch_size": 50,
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ]
    }
  }
}
```

graph-embedding declares no output ports — it writes `EMBEDDING_INDEX` and
`EMBEDDING_DEDUP` directly, and config validation rejects any `outputs` entry.

**graph-clustering component**:

```json
{
  "type": "graph-clustering",
  "config": {
    "detection_interval": "30s",
    "min_community_size": 3,
    "max_iterations": 100,
    "enable_llm": false,
    "enable_anomaly_detection": true,
    "anomaly_config": {
      "enabled": true,
      "core_anomaly": {"enabled": true, "min_core_for_hub_analysis": 2},
      "semantic_gap": {"enabled": false}
    },
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ],
      "outputs": [
        {"name": "communities", "type": "kv-write", "subject": "COMMUNITY_INDEX"},
        {"name": "anomalies", "type": "kv-write", "subject": "ANOMALY_INDEX"}
      ]
    }
  }
}
```

### When to Use

- CI/CD pipelines (no external dependencies)
- Air-gapped environments
- Cost-sensitive deployments
- Keyword search sufficient for your domain

## LLM Inference Configuration

Full semantic capabilities with external ML services.

### Required Components

All Native components, plus:

- **graph-embedding** - HTTP-based neural embeddings (not BM25)
- **graph-index-spatial** - Geospatial indexing
- **graph-index-temporal** - Temporal indexing

**Updated components**:

- **graph-clustering** - With LLM summarization enabled

### Capabilities

Everything in Native, plus:

- Neural embeddings (dense vectors via semembed)
- LLM summaries (semantic community descriptions)
- Hybrid search (neural + BM25 + filters)
- GraphRAG with LLM-enhanced summaries
- Geospatial queries (geohash-based)
- Temporal queries (time-bucketed)

### Example Configuration

**graph-embedding component** (HTTP mode):

```json
{
  "type": "graph-embedding",
  "config": {
    "embedder_type": "http",
    "batch_size": 50,
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ]
    }
  }
}
```

**graph-clustering component** (with LLM and semantic gap detection):

```json
{
  "type": "graph-clustering",
  "config": {
    "detection_interval": "30s",
    "min_community_size": 3,
    "max_iterations": 100,
    "enable_llm": true,
    "enable_anomaly_detection": true,
    "anomaly_config": {
      "enabled": true,
      "core_anomaly": {"enabled": true, "min_core_for_hub_analysis": 2},
      "semantic_gap": {"enabled": true, "min_semantic_similarity": 0.7, "min_structural_distance": 3}
    },
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ],
      "outputs": [
        {"name": "communities", "type": "kv-write", "subject": "COMMUNITY_INDEX"},
        {"name": "anomalies", "type": "kv-write", "subject": "ANOMALY_INDEX"}
      ]
    }
  }
}
```

**graph-index-spatial component**:

```json
{
  "type": "graph-index-spatial",
  "config": {
    "geohash_precision": 6,
    "workers": 4,
    "batch_size": 100,
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ],
      "outputs": [
        {"name": "spatial_index", "type": "kv-write", "subject": "SPATIAL_INDEX"}
      ]
    }
  }
}
```

**graph-index-temporal component**:

```json
{
  "type": "graph-index-temporal",
  "config": {
    "time_resolution": "hour",
    "workers": 4,
    "batch_size": 100,
    "ports": {
      "inputs": [
        {"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}
      ],
      "outputs": [
        {"name": "temporal_index", "type": "kv-write", "subject": "TEMPORAL_INDEX"}
      ]
    }
  }
}
```

### External Services Required

| Service | Port | Purpose |
|---------|------|---------|
| semembed | 8081 | Neural embedding generation (BAAI/bge-small-en-v1.5) |
| semshimmy | 8080 | LLM inference backend |
| seminstruct | 8083 | OpenAI-compatible proxy |

### When to Use

- Production with full semantic capabilities
- Dense vector search required for your domain
- LLM-enhanced summaries for knowledge graph enrichment
- Geospatial or temporal queries needed

## Processing: Hotpath vs Async

### Hotpath (Per-Message)

These affect processing latency:

| Operation | Tier |
|-----------|------|
| Rule evaluation | 0+ |
| Entity extraction | 0+ |
| Triple creation | 0+ |
| KV storage | 0+ |

### Async (Background)

These run independently:

| Operation | Tier | Trigger |
|-----------|------|---------|
| Index maintenance | 0+ | KV watcher |
| BM25 embedding | 1+ | Entity arrival |
| Neural embedding | 2 | Entity arrival |
| Clustering (LPA) | 1+ | Periodic |
| Statistical summaries | 1+ | After LPA |
| LLM summaries | 2 | After LPA |

### Timeline (Tier 2)

```text
T+0:      Entity arrives (hotpath ~10ms)
T+0-5s:   Neural embedding (async ~100ms each)
T+10s:    Clustering starts (initial_delay)
T+10-12s: LPA runs on explicit relationship edges
T+12-20s: LLM summarization (async ~1-2s each)
T+20s:    Enhanced communities available
```

## Queries Depend on Inference

| Inference | Retrieval Enabled |
|-----------|-------------------|
| Explicit triples | PathRAG traversal |
| BM25 embeddings | Semantic search (keyword-based) |
| Neural embeddings | Semantic search (dense vectors) |
| Communities | GraphRAG LocalSearch |
| Statistical summaries | GraphRAG GlobalSearch (statistical) |
| LLM summaries | GraphRAG GlobalSearch (LLM-enhanced) |

No embeddings = no semantic search. No communities = no GraphRAG.

## Graceful Fallback

Higher configurations fall back automatically:

```go
// GraphRAG GlobalSearch
summary := community.LLMSummary
if summary == "" {
    summary = community.StatisticalSummary  // Native config fallback
}
```

## Choosing a Configuration

### Decision Flowchart

```text
Need deterministic only? → Rules-Only
Need semantic search? → Do you have ML infrastructure?
  Yes → LLM
  No  → Native
Need LLM summaries? → LLM
```

### Cost/Benefit

| Config | Compute | Dependencies | Search Type |
|--------|---------|--------------|-------------|
| Rules-Only | Low | None | Explicit edges only |
| Native | Medium | None | Keyword-based (BM25) |
| LLM | High | semembed, LLM | Semantic + LLM summaries |

## Next Steps

- [Rules](../advanced/06-rules-engine.md) - Stateful rules engine
- [Community Detection](../concepts/07-community-detection.md) - How clustering works
- [Advanced: LLM Enhancement](../advanced/02-llm-enhancement.md) - LLM details
