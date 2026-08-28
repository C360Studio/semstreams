# Architecture

SemStreams processes event streams into a semantic knowledge graph stored in NATS KV buckets. This document explains the core components and data flow.

## System Overview

SemStreams uses a distributed component architecture where specialized processors
watch or periodically read state in NATS KV buckets:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                         SemStreams Components                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   JetStream ──► graph-ingest ──► ENTITY_STATES                      │
│                                       │                              │
│                    ┌──────────────────┼──────────────────┐          │
│                    ▼                  ▼                  ▼          │
│              graph-index      graph-clustering    graph-embedding   │
│                    │          graph-index-*                         │
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

Solid arrows represent writes, dashed arrows represent watches/reads.

## Components

SemStreams uses a component-based architecture. Components are self-describing units that connect via NATS.

### General Component Types

| Type | Examples | Role |
|------|----------|------|
| Input | UDP, WebSocket, File | Ingest data from external sources |
| Processor | Graph, JSONMap, Rule | Transform and enrich data |
| Output | File, HTTPPost, WebSocket | Export data to external systems |
| Storage | ObjectStore | Persist data to NATS JetStream |
| Gateway | HTTP, GraphQL | Expose implemented remote APIs |

### Graph Processing Components

The graph system decomposes into 8 specialized components with clear responsibilities:

| Component | Purpose | Writes To | Reads/Observes |
|-----------|---------|-----------|---------|
| **graph-ingest** | Entity ingestion from event streams | ENTITY_STATES | - |
| **graph-index** | Indexing | OUTGOING_INDEX, INCOMING_INDEX, ALIAS_INDEX, PREDICATE_INDEX | ENTITY_STATES |
| **graph-query** | Query coordinator, PathRAG | - | - (request/reply) |
| **graph-clustering** | Community and anomaly detection | COMMUNITY_INDEX, ANOMALY_INDEX | ENTITY_STATES (scheduled) |
| **graph-embedding** | Vector embeddings (BM25 or HTTP) | EMBEDDING_INDEX, EMBEDDING_DEDUP | ENTITY_STATES |
| **graph-index-spatial** | Geospatial indexing (geohash) | SPATIAL_INDEX | ENTITY_STATES |
| **graph-index-temporal** | Time-based indexing | TEMPORAL_INDEX | ENTITY_STATES |
| **graph-gateway** | Provisional HTTP query facade | - | Classified NATS query/index responders |

Each component owns exactly one set of output buckets and declares its input
dependencies, making data ownership explicit. Durable owners remain single-active
until they prove active/active safety. See
[Graph Components Reference](../advanced/07-graph-components.md) for detailed
configuration and deployment information.

### Provisional HTTP Graph Facade

SemStreams provides a hand-written GraphQL-shaped facade for admitted remote
operations. It is not a general schema executor. Embedded services use a named
operation-specific typed adapter only when one is admitted. There is no MCP
graph-read contract; the registered `/mcp` route is a placeholder.

### Flow-Based Design

Components connect through NATS subjects and KV bucket watches rather than direct calls:

- **Loose coupling**: Components react to bucket changes via watchers—no direct dependencies
- **Hook points**: Framework owners declare dependencies. There is no general
  adopter reactive seam; a measured consumer may propose a named typed operation.
  Raw watches remain a declared owner/dependency or operator seam, not a fallback.
- **Configuration-driven**: Flows are JSON configs declaring which components to use and how to connect them
- **Single ownership**: Each KV bucket has one declared owner. Deployment still
  owes single-active enforcement until that owner proves active/active safety.

The graph processing components work together to build a semantic knowledge graph, but you can build simpler flows
with just protocol-layer components (UDP → JSONMap → File) or add semantic processing with the graph suite.

## Processing Flow

The component-based architecture processes entities through multiple stages.
`graph-query` provides operation-specific, hand-wired request/reply routing for
entity, batch, relationship, and PathRAG operations. Other current query fronts
remain separate and provisional; this is not unified access to every capability.

### 1. Message Arrival

Messages arrive via NATS JetStream on the `entity.>` subject. Each message contains a payload implementing the
`Graphable` interface:

```go
type Graphable interface {
    EntityID() string      // 6-part federated identifier
    Triples() []Triple     // Facts about this entity
}
```

Your domain processor transforms raw data into this format—this is where domain knowledge lives.

### 2. Entity Ingestion (graph-ingest)

The `graph-ingest` component:

- Consumes messages from the `entity.>` JetStream subject
- Validates entity IDs against the 6-part format
- Optionally infers hierarchical relationships (if `enable_hierarchy: true`)
- Stores entities in `ENTITY_STATES` with version tracking

Example entity state:

```json
{
  "id": "acme.logistics.sensor.environmental.temperature.sensor-042",
  "triples": [
    {"predicate": "sensor.measurement.celsius", "object": 23.5},
    {"predicate": "geo.location.zone", "object": "acme.logistics.zone.facility.area.warehouse-7"}
  ],
  "version": 5
}
```

Updates use compare-and-swap with version numbers (optimistic concurrency).

### 3. Relationship Indexing (graph-index)

The `graph-index` component watches `ENTITY_STATES` and maintains relationship indexes:

| Index | Question Answered | Updated By |
|-------|-------------------|------------|
| `OUTGOING_INDEX` | "What does this entity reference?" | graph-index |
| `INCOMING_INDEX` | "Who references this entity?" | graph-index |
| `PREDICATE_INDEX` | "All entities with this property" | graph-index |
| `ALIAS_INDEX` | "Resolve friendly name to entity ID" | graph-index |

Indexes update asynchronously after entity saves. Lag is not bounded to
milliseconds: failure can leave a view stale until its owner repairs/redrives it.
GS-03 through GS-10 add lifecycle and status conformance.

### 4. Specialized Indexing (Optional)

Additional indexing components run independently:

| Index | Question Answered | Updated By | Input |
|-------|-------------------|------------|-------|
| `SPATIAL_INDEX` | "Entities near this location" | graph-index-spatial | ENTITY_STATES |
| `TEMPORAL_INDEX` | "Entities in this time range" | graph-index-temporal | ENTITY_STATES |

These components watch `ENTITY_STATES` and maintain their indexes in parallel with relationship indexing.

### 5. Rules Evaluation

Stateful rules evaluate conditions against entity state:

```json
{
  "id": "battery-low-alert",
  "expression": "drone.telemetry.battery < 20",
  "on_enter": [
    {"action": "add_triple", "predicate": "alert.status", "object": "battery_low"},
    {"action": "publish", "subject": "alerts.battery"}
  ],
  "on_exit": [
    {"action": "remove_triple", "predicate": "alert.status"}
  ]
}
```

Rules can add/remove triples and publish messages, creating derived facts dynamically.

### 6. Structural Analysis and Anomaly Detection (graph-clustering)

When anomaly detection is enabled, `graph-clustering` computes structural inputs after community detection:

- **K-core decomposition**: Identifies the dense backbone of the graph. Each entity gets a core number indicating
  how central and well-connected it is. Higher core = more central.
- **Pivot-based distances**: Estimates structural separation from landmark nodes for anomaly detection.
- **Anomaly detection**: Detects core isolation and semantic gaps within community contexts.

K-core and pivot results are passed directly to anomaly detectors in memory for that cycle. Only anomaly records are
stored in `ANOMALY_INDEX`; there is no durable structural query surface.

The internal structural analysis supports core demotion, isolation detection, and semantic-gap detection.

Structural analysis requires only NATS—no external services. Semantic gap detection optionally queries graph-embedding.

### 7. Community Detection (graph-clustering)

The `graph-clustering` component periodically reads current `ENTITY_STATES` and
topology state, then groups entities into communities using the Label Propagation
Algorithm (LPA). Its declared `kv-watch` input is discovery metadata, not an
active runtime watcher. Detection runs at the configured interval (for example,
every 30 seconds).

Communities are stored in `COMMUNITY_INDEX` and enable GraphRAG-style queries at different granularity levels.

### 8. Embedding Generation (graph-embedding)

The `graph-embedding` component watches `ENTITY_STATES` and generates vector embeddings for semantic similarity:

- **BM25 embedder**: Statistical text similarity (384 dimensions, no external dependencies)
- **HTTP embedder**: Neural embeddings via external service (e.g., all-MiniLM-L6-v2)

Embeddings are stored in `EMBEDDING_INDEX` (with `EMBEDDING_DEDUP` tracking unchanged text so it is not
re-embedded). This enables semantic search and detection of semantic-structural gaps (entities that are
semantically similar but lack graph connections).

## State: NATS persistence

Current and derived state commonly lives in NATS KV. Work uses JetStream streams,
and bulky content may live in ObjectStore. Each bucket has one declared owner;
deployment still owes single-active enforcement until that owner proves
active/active safety.

**Core buckets** (required for basic graph operations):

| Bucket | Writer Component | Contents |
|--------|------------------|----------|
| `ENTITY_STATES` | graph-ingest | Entity records with triples and version |
| `OUTGOING_INDEX` | graph-index | Entity ID → referenced entities |
| `INCOMING_INDEX` | graph-index | Entity ID → referencing entities |
| `PREDICATE_INDEX` | graph-index | Predicate → entity IDs |
| `ALIAS_INDEX` | graph-index | Alias → entity ID |

**Optional buckets** (created when specific components are deployed):

| Bucket | Writer Component | Contents | Required For |
|--------|------------------|----------|--------------|
| `SPATIAL_INDEX` | graph-index-spatial | Geohash → entity IDs | Location queries |
| `TEMPORAL_INDEX` | graph-index-temporal | Time bucket → entity IDs | Time-range queries |
| `COMMUNITY_INDEX` | graph-clustering | Community records with members | Community detection |
| `ANOMALY_INDEX` | graph-clustering | Anomaly detection results | Anomaly detection |
| `EMBEDDING_INDEX` | graph-embedding | Entity ID → embedding vector | Semantic similarity |
| `EMBEDDING_DEDUP` | graph-embedding | Deduplication tracking | Embedding efficiency |

See [Graph Components Reference](../advanced/07-graph-components.md#current-roles)
for current ownership evidence.

## Data Flow Example

A sensor reading flows through the component architecture:

```text
JetStream    graph-ingest   ENTITY_STATES   graph-index   Index Buckets   gateway
    │             │              │               │              │            │
    ├─ entity ───►│              │               │              │            │
    │             ├─ PUT ───────►│               │              │            │
    │             │              │               │              │            │
    │             │              ├─ watch ──────►│              │            │
    │             │              │               ├─ update ────►│            │
    │             │              │               │              │            │
    │             │              │               │              ├── reads ──►│
    │             │              │               │              │            │
```

Step-by-step breakdown:

1. **Message arrives**: JetStream receives entity message on `entity.sensor.temperature`
2. **graph-ingest**: Transforms message into EntityState, validates ID format
3. **ENTITY_STATES**: Entity stored with version 6 (optimistic concurrency)
4. **graph-index**: Watches ENTITY_STATES, extracts triples, updates relationship indexes
5. **graph-clustering**: Reads current state on its timer and runs community and optional anomaly analysis
6. **graph-gateway**: Uses its implemented query paths to compose responses

Components use different processing models. Do not infer one latency or
consistency promise from the protocol used to read them.

## Consistency Model

Current consistency evidence is incomplete. The GS program adds it one owner at
a time; this table does not describe a guarantee the current runtime already
provides.

| Component | Current evidence | Scheduled target |
|---|---|---|
| graph-ingest (`ENTITY_STATES`) | Entity value, without revision in the query reply | GS-01: value plus revision |
| graph-index | Implementation-specific readiness | GS-04: declared authority coverage |
| graph-index-spatial | No uniform lifecycle/status contract | GS-06: owner-declared status |
| graph-index-temporal | No uniform lifecycle/status contract | GS-07: owner-declared status |
| graph-embedding | Capability behavior is operation-specific | GS-08: work and capability state |
| graph-clustering | Cycle behavior is operation-specific | GS-09: cycle and staleness evidence |

GS-12 requires each admitted query operation to declare whether authority or a
named view answers and what status evidence accompanies that answer.

## Component Deployment Patterns

The component architecture supports flexible deployment strategies:

### Minimal Deployment (Core Graph Only)

Deploy just the essential components for basic graph operations:

- `graph-ingest` - Entity ingestion
- `graph-index` - Relationship indexing
- `graph-query` - Query coordination (optional, for PathRAG)
- `graph-gateway` - Provisional HTTP query facade

This provides entity storage, relationship traversal, and the facade's admitted
HTTP operations without advanced features. `graph-query` is needed only for its
implemented coordinated operations such as PathRAG.

### Tiered Deployment

Add components incrementally based on capability requirements:

**Tier 1 (Statistical)**:

- Add `graph-clustering` for community detection; optionally enable anomaly detection and its internal analysis
- Add `graph-embedding` with BM25 for statistical similarity

**Tier 2 (Semantic)**:

- Configure `graph-embedding` with HTTP embedder for neural embeddings
- Enable LLM summarization in `graph-clustering`

### Specialized Deployments

Add optional indexing components based on query patterns:

- `graph-index-spatial` for geolocation queries
- `graph-index-temporal` for time-range queries

See [Configuration](06-configuration.md) for complete deployment examples and [Graph Components
Reference](../advanced/07-graph-components.md) for detailed component specifications.

## What SemStreams Is Not

- **Not a database replacement**: No arbitrary SQL or ACID transactions. Use an
  admitted prefix/predicate operation when available; raw subject/KV wildcards
  are owner/operator seams, not adopter query basics.
- **Hybrid streaming/batch**: Entity updates flow continuously, but analysis components (anomalies, clustering) run
  periodically (configurable intervals)
- **Not a time-series DB**: Use InfluxDB/Prometheus for metrics
- **Not full-text search**: Use Elasticsearch for document search

## Background Concepts

New to knowledge graphs or event-driven systems? See [Concepts](../concepts/) for background on:

- [Real-Time Inference](../concepts/00-real-time-inference.md) - Tier system (Structural → Statistical → Semantic)
- [Event-Driven Basics](../concepts/01-event-driven-basics.md) - Pub/sub, streams, NATS
- [Knowledge Graphs](../concepts/04-knowledge-graphs.md) - Triples, SPO model
- [Community Detection](../concepts/07-community-detection.md) - LPA algorithm details
- [GraphRAG Pattern](../concepts/09-graphrag-pattern.md) - Community-based RAG

## Next Steps

- [Graphable Interface](03-graphable-interface.md) - Implement entity transformation
- [Vocabulary](04-vocabulary.md) - Design your predicates
- [Configuration](06-configuration.md) - Choose your capability level
- [Graph Components Reference](../advanced/07-graph-components.md) - Detailed component specifications and
  deployment guidance
