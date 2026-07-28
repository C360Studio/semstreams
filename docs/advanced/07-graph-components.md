# Graph Components Reference

Detailed specifications for the 8 graph processing components in SemStreams.

## Overview

The SemStreams graph subsystem decomposes into 8 specialized components with clear ownership boundaries:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                         SemStreams Components                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   JetStream ──► graph-ingest ──► ENTITY_STATES                      │
│                                       │                              │
│                    ┌──────────────────┼──────────────────┐          │
│                    ▼                  ▼                  ▼          │
│              graph-index       graph-clustering    graph-embedding  │
│                    │           graph-index-*                        │
│                    ▼                                                 │
│            Index Buckets ◄─────────────────────────────────────┐    │
│                    │                                            │    │
│                    └──────────────► graph-query ◄───────────────┘    │
│                                          │                           │
│                                          ▼                           │
│                                    graph-gateway                     │
│                                     HTTP/GraphQL                     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```


Each component:

- Owns specific NATS KV buckets (single writer per bucket)
- Watches other buckets via KV watchers (multiple readers)
- Exposes query capabilities via NATS request/reply
- Implements `Discoverable` and `LifecycleComponent` interfaces
- Scales independently

## Component Specifications

### 1. graph-ingest - Entity Ingestion

**Purpose**: Consumes entity events from JetStream, validates entity IDs, optionally infers hierarchical
relationships, and persists entities to `ENTITY_STATES`.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_stream` (jetstream): Consumes from `entity.>` subject

**Output Ports**:

- `entity_states` (kv-bucket): Writes to `ENTITY_STATES`

**Configuration**:

```json
{
  "enable_hierarchy": false,
  "ports": {
    "inputs": [
      {"name": "entity_stream", "type": "jetstream", "subject": "entity.>"}
    ],
    "outputs": [
      {"name": "entity_states", "type": "kv-bucket", "bucket": "ENTITY_STATES"}
    ]
  }
}
```

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.ingest.query.entity` | getEntity | Get single entity by ID |
| `graph.ingest.query.batch` | getBatch | Get multiple entities by IDs |
| `graph.ingest.query.prefix` | prefix | List entities by ID prefix (hierarchy) |
| `graph.ingest.capabilities` | capabilities | Discover query capabilities |

**Entity ID Validation**: Enforces 6-part format: `org.platform.domain.system.type.instance`

**Hierarchy Inference**: When `enable_hierarchy: true`, automatically infers parent-child relationships from
entity IDs (e.g., `acme.ops` is parent of `acme.ops.robotics`).

---

### 2. graph-index - Relationship Indexing

**Purpose**: Watches `ENTITY_STATES`, extracts relationships from triples, and maintains relationship indexes for
efficient graph traversal.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_states_watch` (kv-watch): Watches `ENTITY_STATES` bucket

**Output Ports**:

- `outgoing_index` (kv-bucket): Writes to `OUTGOING_INDEX`
- `incoming_index` (kv-bucket): Writes to `INCOMING_INDEX`
- `predicate_index` (kv-bucket): Writes to `PREDICATE_INDEX`
- `alias_index` (kv-bucket): Writes to `ALIAS_INDEX`
- `context_index` (kv-bucket): Writes to `CONTEXT_INDEX`

**Configuration**:

```json
{
  "ports": {
    "inputs": [
      {"name": "entity_states_watch", "type": "kv-watch", "bucket": "ENTITY_STATES"}
    ],
    "outputs": [
      {"name": "outgoing_index", "type": "kv-bucket", "bucket": "OUTGOING_INDEX"},
      {"name": "incoming_index", "type": "kv-bucket", "bucket": "INCOMING_INDEX"},
      {"name": "predicate_index", "type": "kv-bucket", "bucket": "PREDICATE_INDEX"},
      {"name": "alias_index", "type": "kv-bucket", "bucket": "ALIAS_INDEX"},
      {"name": "context_index", "type": "kv-bucket", "bucket": "CONTEXT_INDEX"}
    ]
  }
}
```

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.index.query.outgoing` | getOutgoing | Get entities this entity references |
| `graph.index.query.incoming` | getIncoming | Get entities that reference this entity |
| `graph.index.query.alias` | getAlias | Resolve alias to canonical entity ID |
| `graph.index.query.predicate` | getPredicate | Get entities with specific predicate |
| `graph.index.capabilities` | capabilities | Discover query capabilities |

**Index Structures**:

- **OUTGOING_INDEX**: `entity_id` → `[{to_entity_id, predicate}]`
- **INCOMING_INDEX**: `entity_id` → `[{from_entity_id, predicate}]`
- **PREDICATE_INDEX**: `predicate` → `[entity_ids]`
- **ALIAS_INDEX**: `alias` → `entity_id`

---

### 3. graph-clustering - Community Detection, Structural Analysis, Anomaly Detection

**Purpose**: Groups entities into communities using Label Propagation Algorithm (LPA), computes structural indices
(k-core decomposition, pivot distances), and detects anomalies within community contexts. Optionally enhances
community descriptions using LLM.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_states_watch` (kv-watch): Watches `ENTITY_STATES`

**Output Ports**:

- `community_index` (kv-bucket): Writes to `COMMUNITY_INDEX`
- `structural_index` (kv-bucket): Writes to `STRUCTURAL_INDEX`
- `anomaly_index` (kv-bucket): Writes to `ANOMALY_INDEX`

**Configuration**:

```json
{
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
    "core_anomaly": {"enabled": true, "min_core_for_hub_analysis": 2},
    "semantic_gap": {"enabled": false, "min_semantic_similarity": 0.7, "min_structural_distance": 3}
  },
  "ports": {
    "inputs": [
      {"name": "entity_states_watch", "type": "kv-watch", "bucket": "ENTITY_STATES"}
    ],
    "outputs": [
      {"name": "community_index", "type": "kv-bucket", "bucket": "COMMUNITY_INDEX"},
      {"name": "structural_index", "type": "kv-bucket", "bucket": "STRUCTURAL_INDEX"},
      {"name": "anomaly_index", "type": "kv-bucket", "bucket": "ANOMALY_INDEX"}
    ]
  }
}
```

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.clustering.query.community` | getCommunity | Get community by ID |
| `graph.clustering.query.members` | getMembers | Get member entities of a community |
| `graph.clustering.query.entity` | getEntityCommunity | Get community containing an entity |
| `graph.clustering.query.level` | getCommunitiesByLevel | List communities at hierarchy level |
| `graph.clustering.query.kcore` | getKCore | Get k-core number for entity |
| `graph.clustering.query.anomalies` | getAnomalies | Get detected anomalies |
| `graph.clustering.capabilities` | capabilities | Discover query capabilities |

**Detection Cycle**:

1. **Community Detection (LPA)** → COMMUNITY_INDEX
2. **Structural Computation** (if enabled) → STRUCTURAL_INDEX
3. **Anomaly Detection** (if enabled) → ANOMALY_INDEX

**Algorithms**:

- **Label Propagation Algorithm (LPA)**: Efficient community detection with convergence
- **K-core decomposition**: Identifies dense backbone (higher core = more central)
- **Pivot-based distances**: Pre-computes distances to landmark nodes for O(1) estimation
- **Core isolation**: Detects entities at high k-core levels with few same-level peers
- **Semantic gap**: Detects entities that are semantically similar but structurally distant

**Execution**: Triggered by threshold of entity changes or time interval

---

### 4. graph-embedding - Vector Embeddings

**Purpose**: Generates vector embeddings for entities using BM25 (statistical) or HTTP (neural) embedders,
enabling semantic similarity search.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_states_watch` (kv-watch): Watches `ENTITY_STATES`

**Output Ports**: none — graph-embedding writes `EMBEDDING_INDEX` and `EMBEDDING_DEDUP`
directly (durable bucket writes, not port-declared flows); config validation rejects any
`outputs` entry.

**Configuration**:

```json
{
  "embedder_type": "bm25",
  "embedder_config": {
    "dimensions": 384
  },
  "ports": {
    "inputs": [
      {"name": "entity_states_watch", "type": "kv-watch", "bucket": "ENTITY_STATES"}
    ]
  }
}
```

**Embedder Types**:

- **bm25**: Statistical text similarity (384 dimensions, no external dependencies)
- **http**: Neural embeddings via external service (configurable endpoint)

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.embedding.query.similar` | findSimilar | Find entities similar to given entity |
| `graph.embedding.query.search` | search | Semantic search by text query |
| `graph.embedding.capabilities` | capabilities | Discover query capabilities |

---

### 5. graph-index-spatial - Geospatial Indexing

**Purpose**: Indexes entities with geolocation data using geohash for efficient spatial queries.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_states_watch` (kv-watch): Watches `ENTITY_STATES`

**Output Ports**:

- `spatial_index` (kv-bucket): Writes to `SPATIAL_INDEX`

**Configuration**:

```json
{
  "geohash_precision": 6,
  "ports": {
    "inputs": [
      {"name": "entity_states_watch", "type": "kv-watch", "bucket": "ENTITY_STATES"}
    ],
    "outputs": [
      {"name": "spatial_index", "type": "kv-bucket", "bucket": "SPATIAL_INDEX"}
    ]
  }
}
```

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.spatial.query.bounds` | spatial | Find entities within geographic bounds |
| `graph.spatial.capabilities` | capabilities | Discover query capabilities |

**Index Structure**: Geohash prefix → entity IDs with coordinates

---

### 6. graph-index-temporal - Time-Based Indexing

**Purpose**: Indexes entities by temporal properties for efficient time-range queries.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- `entity_states_watch` (kv-watch): Watches `ENTITY_STATES`

**Output Ports**:

- `temporal_index` (kv-bucket): Writes to `TEMPORAL_INDEX`

**Configuration**:

```json
{
  "bucket_size": "1h",
  "ports": {
    "inputs": [
      {"name": "entity_states_watch", "type": "kv-watch", "bucket": "ENTITY_STATES"}
    ],
    "outputs": [
      {"name": "temporal_index", "type": "kv-bucket", "bucket": "TEMPORAL_INDEX"}
    ]
  }
}
```

**Query Operations**:

| Subject | Operation | Description |
|---------|-----------|-------------|
| `graph.temporal.query.range` | temporal | Find entities within time range |
| `graph.temporal.capabilities` | capabilities | Discover query capabilities |

**Index Structure**: Time bucket → entity IDs with timestamps

---

### 7. graph-gateway - Query API

**Purpose**: Exposes HTTP/GraphQL/MCP APIs for querying the knowledge graph. Reads from all index buckets but
writes to none.

**Interfaces**: `Discoverable`, `LifecycleComponent`

**Input Ports**: None (HTTP server, not NATS consumer)

**Output Ports**: None (read-only component)

**Configuration**:

```json
{
  "http_port": 8080,
  "enable_playground": true,
  "query_timeout": "30s",
  "max_results": 1000,
  "max_depth": 10
}
```

**Read Access**:

- `ENTITY_STATES` (via graph-ingest queries)
- `OUTGOING_INDEX`, `INCOMING_INDEX`, `PREDICATE_INDEX`, `ALIAS_INDEX` (via graph-index queries)
- `COMMUNITY_INDEX`, `STRUCTURAL_INDEX`, `ANOMALY_INDEX` (via graph-clustering queries)
- `EMBEDDING_INDEX` (via graph-embedding queries)
- `SPATIAL_INDEX` (via graph-index-spatial queries)
- `TEMPORAL_INDEX` (via graph-index-temporal queries)

**Endpoints**:

- `POST /graphql` - GraphQL query endpoint
- `GET /` - GraphQL Playground (when enabled)
- `GET /health` - Health check

---

### 8. graph-query - Query Coordinator

**Purpose**: Orchestrates queries across other graph components, provides PathRAG traversal, and aggregates
capabilities for discovery.

**Interfaces**: `Discoverable`, `LifecycleComponent`, `QueryCapabilityProvider`

**Input Ports**:

- Multiple NATS request/reply subjects for coordinated operations

**Output Ports**: None (returns data via request/reply)

**Configuration**:

```json
{
  "query_timeout": "5s",
  "max_depth": 10,
  "ports": {
    "inputs": [
      {"name": "query_entity", "type": "nats-request", "subject": "graph.query.entity"},
      {"name": "query_relationships", "type": "nats-request", "subject": "graph.query.relationships"},
      {"name": "query_pathSearch", "type": "nats-request", "subject": "graph.query.pathSearch"},
      {"name": "query_hierarchyStats", "type": "nats-request", "subject": "graph.query.hierarchyStats"},
      {"name": "query_prefix", "type": "nats-request", "subject": "graph.query.prefix"},
      {"name": "query_spatial", "type": "nats-request", "subject": "graph.query.spatial"},
      {"name": "query_temporal", "type": "nats-request", "subject": "graph.query.temporal"},
      {"name": "query_semantic", "type": "nats-request", "subject": "graph.query.semantic"},
      {"name": "query_similar", "type": "nats-request", "subject": "graph.query.similar"},
      {"name": "query_capabilities", "type": "nats-request", "subject": "graph.query.capabilities"}
    ],
    "outputs": []
  }
}
```

**Query Operations**:

| Subject | Operation | Routes To |
|---------|-----------|-----------|
| `graph.query.entity` | getEntity | graph-ingest |
| `graph.query.relationships` | getRelationships | graph-index |
| `graph.query.pathSearch` | pathSearch | PathSearcher (internal) |
| `graph.query.hierarchyStats` | hierarchyStats | graph-ingest (with aggregation) |
| `graph.query.prefix` | prefix | graph-ingest |
| `graph.query.spatial` | spatial | graph-index-spatial |
| `graph.query.temporal` | temporal | graph-index-temporal |
| `graph.query.semantic` | semantic | graph-embedding |
| `graph.query.similar` | similar | graph-embedding |
| `graph.query.capabilities` | capabilities | Aggregates all components |

**PathRAG Implementation**: Orchestrates bounded graph traversal by coordinating entity and relationship queries
across graph-ingest and graph-index components.

**Capabilities Aggregation**: Discovers and consolidates query capabilities from all graph components into a
unified response.

---

## KV Bucket Ownership Table

Each NATS KV bucket has exactly one writer component (single-owner pattern) and zero or more readers.

| Bucket | Writer | Readers | Purpose |
|--------|--------|---------|---------|
| `ENTITY_STATES` | graph-ingest | graph-index, graph-clustering, graph-embedding, graph-index-spatial, graph-index-temporal, graph-gateway | Primary entity storage |
| `CONTEXT_INDEX` | graph-ingest | graph-query | Context/provenance tracking for hierarchy inference |
| `OUTGOING_INDEX` | graph-index | graph-clustering, graph-gateway | Entity → referenced entities |
| `INCOMING_INDEX` | graph-index | graph-clustering, graph-gateway | Entity → referencing entities |
| `PREDICATE_INDEX` | graph-index | graph-gateway | Predicate → entity IDs |
| `ALIAS_INDEX` | graph-index | graph-gateway | Alias → canonical entity ID |
| `SPATIAL_INDEX` | graph-index-spatial | graph-gateway | Geohash → entities |
| `TEMPORAL_INDEX` | graph-index-temporal | graph-gateway | Time bucket → entities |
| `COMMUNITY_INDEX` | graph-clustering | graph-gateway | Community records with members |
| `STRUCTURAL_INDEX` | graph-clustering | graph-gateway | K-core levels and pivot distances |
| `ANOMALY_INDEX` | graph-clustering | graph-query, graph-gateway | Anomaly detection results |
| `EMBEDDING_INDEX` | graph-embedding | graph-clustering, graph-gateway | Entity ID → embedding vector |
| `EMBEDDING_DEDUP` | graph-embedding | graph-embedding | Deduplication tracking |

---

## Query Capabilities by Component

Each component exposes query operations via NATS request/reply. Components implementing `QueryCapabilityProvider`
advertise their capabilities for runtime discovery.

| Component | Queries | Capability Subject |
|-----------|---------|-------------------|
| **graph-ingest** | getEntity, getBatch, prefix | `graph.ingest.capabilities` |
| **graph-index** | getOutgoing, getIncoming, getAlias, getPredicate | `graph.index.capabilities` |
| **graph-clustering** | getCommunity, getMembers, getEntityCommunity, getCommunitiesByLevel, getKCore, getAnomalies | `graph.clustering.capabilities` |
| **graph-embedding** | findSimilar, search | `graph.embedding.capabilities` |
| **graph-index-spatial** | spatial | `graph.spatial.capabilities` |
| **graph-index-temporal** | temporal | `graph.temporal.capabilities` |
| **graph-query** | (coordinator - routes to above) | `graph.query.capabilities` |

**Usage Example**:

```bash
# Discover graph-ingest capabilities
nats req graph.ingest.capabilities '{}'

# Response includes query subjects, schemas, and descriptions
```

---

## Data Flow Patterns

### Write Path (Entity Ingestion)

```text
JetStream    graph-ingest   ENTITY_STATES   graph-index   Index Buckets
    │             │              │               │              │
    ├─ entity ───►│              │               │              │
    │             ├─ validate ──►│               │              │
    │             ├─ PUT (CAS) ─►│               │              │
    │             │              ├─ watch ──────►│              │
    │             │              │               ├─ update ────►│
```


### Read Path (Direct Query)

```text
Client           graph-ingest        ENTITY_STATES
  │                   │                    │
  ├─ query.entity ───►│                    │
  │                   ├─── Get ───────────►│
  │                   │◄── Data ───────────┤
  │◄─ entity JSON ────┤                    │
```


### Read Path (Coordinated Query via graph-query)

```text
Client     graph-query   graph-ingest   graph-index
  │            │              │              │
  ├─ pathSearch►│              │              │
  │            ├─ query ─────►│              │
  │            │◄─ entity ────┤              │
  │            ├─ outgoing ──────────────────►│
  │            │◄─ rels ─────────────────────┤
  │◄─ paths ───┤              │              │
```


---

## Deployment Strategies

### Minimal Deployment (Core Graph Only)

Deploy essential components for basic graph operations:

```yaml
components:
  - graph-ingest
  - graph-index
  - graph-gateway
```

**Capabilities**: Entity storage, relationship traversal, GraphQL queries

**Buckets Required**: `ENTITY_STATES`, `OUTGOING_INDEX`, `INCOMING_INDEX`, `PREDICATE_INDEX`, `ALIAS_INDEX`

### Tiered Deployment

**Tier 0 (Rules-Only)**:

```yaml
components:
  - graph-ingest
  - graph-index
  - graph-query
  - graph-gateway
```

**Provides**: Entity storage, relationship traversal, coordinated queries

**Tier 1 (Statistical)**:

```yaml
components:
  - graph-ingest
  - graph-index
  - graph-clustering  # with enable_structural: true, enable_anomaly_detection: true
  - graph-embedding   # with BM25 embedder
  - graph-query
  - graph-gateway
```

**Adds**: Community detection, structural analysis, anomaly detection, BM25 semantic similarity

**Tier 2 (Semantic)**:

```yaml
components:
  - graph-ingest
  - graph-index
  - graph-clustering  # with enable_llm: true, enable_structural: true, enable_anomaly_detection: true
  - graph-embedding   # with HTTP neural embedder
  - graph-query
  - graph-gateway
```

**Adds**: Neural embeddings, LLM-powered summarization, semantic gap detection

### Specialized Deployments

Add optional indexing components based on query patterns:

```yaml
# Geolocation queries
components:
  - graph-index-spatial

# Time-range queries
components:
  - graph-index-temporal
```

---

## Consistency Guarantees

Different components provide different consistency levels based on their processing model:

| Component | Consistency | Latency | Processing Model |
|-----------|-------------|---------|-----------------|
| graph-ingest | Immediate | <1ms | Synchronous (CAS) |
| graph-index | Eventually consistent | <10ms | Asynchronous (KV watch) |
| graph-index-spatial | Eventually consistent | <10ms | Asynchronous (KV watch) |
| graph-index-temporal | Eventually consistent | <10ms | Asynchronous (KV watch) |
| graph-clustering | Batch | Configurable (default: 30s) | Threshold + interval |
| graph-embedding | Eventually consistent | <100ms (BM25), varies (HTTP) | Asynchronous (KV watch) |
| graph-query | N/A (coordinator) | Depends on routed component | Request/reply |
| graph-gateway | N/A (read-only) | Reads current state | Request/reply |

**Trade-offs**:

- **Write throughput**: Optimistic concurrency (CAS) enables high-throughput writes
- **Query consistency**: Queries may see entities before their indexes are complete (milliseconds)
- **Component independence**: Asynchronous processing prevents cascading failures

---

## Scaling Patterns

### Horizontal Scaling

Each component scales independently:

- **graph-ingest**: Scale to handle JetStream message volume
- **graph-index**: Scale based on entity update rate
- **graph-clustering**: Scale for community detection, structural analysis, and anomaly workload
- **graph-embedding**: Scale for embedding generation throughput
- **graph-query**: Scale for query orchestration throughput
- **graph-gateway**: Scale for HTTP query load

### Resource Allocation

**CPU-intensive components**:

- graph-clustering (LPA iterations, k-core decomposition, anomaly detection)
- graph-embedding (BM25 computation)

**Memory-intensive components**:

- graph-clustering (community membership tracking, in-memory graph for analysis)
- graph-embedding (vector cache)

**I/O-intensive components**:

- graph-ingest (JetStream consumer + KV writes)
- graph-index (KV watch + multiple index writes)
- graph-query (NATS request coordination)

---

## Health and Monitoring

All components implement health checks via the `LifecycleComponent` interface.

### Health Status Levels

- **Healthy**: Component operational, all dependencies connected
- **Degraded**: Component operational but experiencing errors (tracked count)
- **Unhealthy**: Component cannot fulfill its role (e.g., NATS disconnected)

### Metrics

Each component exposes Prometheus metrics:

- `entities_updated_total` (graph-ingest)
- `entities_indexed_total` (graph-index)
- `communities_detected_total` (graph-clustering)
- `structural_runs_total` (graph-clustering)
- `anomalies_detected` (graph-clustering)
- `embeddings_generated_total` (graph-embedding)
- `queries_handled_total` (graph-query)
- `http_requests_total` (graph-gateway)

---

## Error Handling

### Component-Level Errors

Components follow the return-errors-don't-log pattern:

- Operations return `error` values to callers
- Callers decide whether to log, retry, or escalate
- Health tracking accumulates error counts

### Query Error Responses

NATS query handlers return structured errors:

```json
{
  "error": "not found"
}
```

**Standard error messages**:

- `"invalid request"` - Malformed JSON or missing required fields
- `"not found"` - Entity or resource not found
- `"timeout"` - Query exceeded timeout
- `"internal error"` - Component-side failure

### Failure Modes

| Component | Failure Impact | Recovery |
|-----------|---------------|----------|
| graph-ingest | New entities not persisted | JetStream replay on reconnect |
| graph-index | Relationships not indexed | KV watch resumption processes backlog |
| graph-clustering | Communities, structural data, anomalies stale | Next detection cycle updates |
| graph-embedding | Embeddings stale | Next embedding generation updates |
| graph-query | Coordinated queries fail | Retry with exponential backoff |
| graph-gateway | HTTP queries unavailable | Load balancer routes to healthy instance |

**Graceful degradation**: Query components return partial results when dependencies are unavailable
(e.g., graph-query returns entity data even if relationship indexes are temporarily unavailable).

---

## Related Documentation

- [Architecture Overview](../basics/02-architecture.md) - High-level system design
- [Query Access Patterns](../concepts/11-query-access.md) - GraphQL and NATS query usage
- [Configuration Guide](../basics/06-configuration.md) - Component configuration examples
- [PathRAG Pattern](../concepts/10-pathrag-pattern.md) - Structural traversal details
- [GraphRAG Pattern](../concepts/09-graphrag-pattern.md) - Community-based search details
