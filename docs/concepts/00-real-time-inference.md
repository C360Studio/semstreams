# Real-Time Inference

How SemStreams combines streaming data with continuous inference.

## The Hybrid Model

Traditional ML systems separate training from serving:

```text
Traditional ML:
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Collect    │────►│   Train     │────►│   Serve     │
│   Data      │     │   Model     │     │  (frozen)   │
└─────────────┘     └─────────────┘     └─────────────┘
      │                   │                   │
   Hours              Hours/Days           Stale
```

SemStreams continuously updates its graph while serving queries:

```text
SemStreams:
                    ┌─────────────────────────────────┐
                    │         Graph + Indexes         │
┌─────────┐         │  ┌───────┐  ┌───────────────┐  │  ┌─────────┐
│ Events  │────────►│  │Entity │  │  Communities  │  │◄─│ Queries │
│ Stream  │         │  │States │  │  (periodic)   │  │  │         │
└─────────┘         │  └───────┘  └───────────────┘  │  └─────────┘
                    └─────────────────────────────────┘
                              │
                         Always fresh
```

Events flow continuously. Queries see the current state, not a snapshot from training time.

## Query Freshness

SemStreams is **eventually consistent**:

| Operation | Latency |
|-----------|---------|
| Entity update → queryable | Milliseconds |
| Clustering update | Seconds (batch interval) |
| LLM summary | Seconds-minutes (async) |

Entity updates propagate via KV watch and cache invalidation. Queries return the latest committed state.

## Inference Modes

SemStreams supports multiple inference patterns with different timing characteristics.

### Rules (Sync, Per-Event)

```text
Entity arrives → Rule evaluates → Action fires (if triggered)
                      │
                 Synchronous
                 (sub-millisecond)
```

- Evaluates immediately on entity change
- Stateful: tracks OnEnter/OnExit transitions
- Actions: create alerts, add/remove triples, update state
- Deterministic, no external dependencies

### Embeddings (Async, Per-Entity)

```text
Entity arrives → Embedding generated → Stored for similarity search
                      │
                 Asynchronous
                 (Tier 1+)
```

- BM25 (Tier 1): Pure Go, lexical vectors
- Neural (Tier 2): External service, semantic vectors
- Enables semantic similarity search

### Clustering (Periodic Batch)

```text
Timer fires → LPA clustering → Communities updated → Inferred triples created
                   │                                        │
             Every 30s (configurable)              Confidence: 0.5-0.8
```

- Runs periodically or after N entity changes
- Uses explicit edges from entity triples
- Generates hierarchical community levels

## Progressive Tiers (Embedding Strategy)

Tiers define the embedding **method**—not what data gets embedded. Embeddings require text content.

| Tier | Name | Embedding Method | Similarity Search | Requires Text |
|------|------|------------------|-------------------|---------------|
| **0** | Structural | Disabled | None | No |
| **1** | Statistical | BM25 (pure Go) | Lexical (term matching) | Yes |
| **2** | Semantic | Neural (external) | Semantic (meaning matching) | Yes |

All tiers include rules and clustering on explicit edges. The difference is whether similarity search is available for entities **with text content**.

> **Important:** Embeddings extract text from fields like `title`, `content`, `description`, `summary`, `text`, `name`. Entities with only numeric triples (telemetry) generate no embeddings regardless of tier—they cluster via explicit relationships.

### Tier 0: Structural

Build graphs using explicit relationships and rules—no embeddings required.

Tier 0 provides two complementary capabilities for graph construction:

| Capability | Description | Timing |
|------------|-------------|--------|
| **Explicit Triples** | Relationships from source data (SPO format) | Sync, per-event |
| **Rules Engine** | Pattern-based inference (IF A→B AND B→C THEN A→C) | Sync, per-event |

#### Explicit Triples

Entities connect via triples like `sensor-001 located_in zone-A`. Communities form from graph structure only. Summaries derive from entity types and predicate names.

#### Rules Engine

Evaluates patterns on entity changes. Can create new triples, fire alerts, or update state. Deterministic, no external dependencies.

**Use case:** Any deployment where explicit relationships are sufficient—telemetry, network topology, highly structured domains.

### Tier 1: Statistical

BM25 embeddings for entities with text content—pure Go, no external services.

- Text entities support lexical similarity search via BM25
- TF-IDF keyword extraction from text fields
- Clustering: explicit edges only for all entities
- Telemetry entities cluster via explicit relationships only
- **Anomaly detection**: Topology analysis suggests missing relationships (see [Anomaly Detection](08-anomaly-detection.md))

When anomaly detection is enabled, graph-clustering computes structural inputs in
memory after community detection:
- **K-core decomposition**: Identifies graph backbone vs periphery
- **Pivot-based distances**: Estimates structural distance between entities
- **Gap detection**: Finds entities that are semantically similar but structurally distant

The K-core and pivot results are not persisted or exposed as a structural query
index.

**Use case:** Mixed deployments with documents AND telemetry—documents cluster by topic, telemetry by explicit relationships. Anomaly detection helps identify missing connections.

### Tier 2: Semantic

Neural embeddings for entities with text content—requires external embedding service.

- Text entities support semantic similarity search via neural embeddings
- "Machine" matches "equipment" and "device"
- Better search quality for natural language queries
- Telemetry entities still cluster via explicit relationships only

**Use case:** Document-heavy domains needing semantic search, natural language queries.

### What This Means in Practice

| Entity Type | Has Text? | Tier 0 | Tier 1 | Tier 2 |
|-------------|-----------|--------|--------|--------|
| Sensor reading | No | Explicit edges | Explicit edges | Explicit edges |
| Equipment record | Maybe (name) | Explicit edges | + BM25 search if text | + Neural search if text |
| Document/manual | Yes | Explicit edges | + BM25 search | + Neural search |

**Telemetry-only deployments:** Tier setting doesn't affect clustering—all tiers behave like Tier 0 because there's no text to embed. Choose Tier 0 to skip embedding overhead.

**Mixed deployments:** Tier 1/2 adds value for document entities while telemetry continues clustering via explicit relationships.

## Completion Service (Optional)

The completion service is **independent of tier choice**. It provides text generation via any OpenAI-compatible API.

| Capability | Without Completion | With Completion |
|------------|-------------------|-----------------|
| Community summaries | TF-IDF keywords | + LLM-enhanced narratives |
| Query answers | Structured results | + Natural language answers |

**Requirements:**
- Tier 1+ (needs text content to work with)
- External service (OpenAI-compatible API)

**Combinations:**
- Tier 1 + Completion = Statistical embeddings + LLM summaries
- Tier 2 + Completion = Neural embeddings + LLM summaries  
- Tier 2 alone = Neural embeddings + statistical summaries (no LLM cost)

Falls back gracefully to statistical summaries if completion service unavailable.

## Comparison: Batch ML vs SemStreams

| Aspect | Batch ML | SemStreams |
|--------|----------|------------|
| **Data freshness** | Stale (last training) | Real-time (KV watch) |
| **Model updates** | Retrain (hours/days) | Continuous (streaming) |
| **Cold start** | Full retrain | Incremental from zero |
| **Inference modes** | Single | Multiple (rules/cluster/LLM) |
| **Failure mode** | Model unavailable | Graceful degradation by tier |

## When Each Mode Runs

```text
┌─────────────────────────────────────────────────────────────┐
│                        Time →                               │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Entity    ●────────●────────●────────●────────●           │
│  Arrives        │        │        │        │               │
│                 ▼        ▼        ▼        ▼               │
│  Rules     ════════════════════════════════════            │
│  (sync)    Evaluates on every entity                       │
│                                                             │
│  Embed          ░──░     ░──░     ░──░     ░──░            │
│  (async)        Generate vectors (Tier 1+)                 │
│                                                             │
│  Cluster        ┌────┐              ┌────┐                 │
│  (batch)        │ Run│              │ Run│                 │
│                 └────┘              └────┘                 │
│                 Every 30s or N changes                     │
│                                                             │
│  Complete           ░░░░░                ░░░░░             │
│  (async)            Enhance             Enhance            │
│                     summaries           summaries          │
│                                                             │
│  Query      ?           ?                    ?             │
│  (on-demand)│           │                    │             │
│             ▼           ▼                    ▼             │
│         Hit cache   Compute              Hit cache         │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

Rules provide real-time responsiveness. Embeddings enable similarity. Clustering provides structure. Completion provides understanding. They work together, not sequentially.

## Related

- [Event-Driven Basics](01-event-driven-basics.md) - How events flow through the system
- [Similarity Metrics](06-similarity-metrics.md) - Cosine, Jaccard, TF-IDF fundamentals
- [Community Detection](07-community-detection.md) - How clustering works
- [Anomaly Detection](08-anomaly-detection.md) - Tier 1+ topology analysis and gap detection
- [GraphRAG Pattern](09-graphrag-pattern.md) - How communities enable RAG
- [PathRAG Pattern](10-pathrag-pattern.md) - Structural traversal queries
