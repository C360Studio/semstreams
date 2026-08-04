# Embeddings

Embeddings transform text into dense numerical vectors, enabling semantic similarity search without explicit relationships.

## The Problem Embeddings Solve

Consider a warehouse with sensor telemetry AND documentation (equipment manuals, maintenance logs). The telemetry entities have explicit relationships (sensors → zones → equipment). But how do you find documents related to a specific sensor without manually linking them?

**Without embeddings:** Only explicit triples create graph edges. Documents are isolated unless you manually add relationships.

**With embeddings:** The system computes vector representations of document content. Semantic search finds relevant content even without exact keyword matches.

## What Embeddings Are

An embedding is a fixed-length vector (typically 384 dimensions) representing semantic meaning:

```text
"Temperature sensor calibration procedure"  →  [0.12, -0.34, 0.56, ..., 0.78]
"Thermal measurement device maintenance"    →  [0.11, -0.32, 0.58, ..., 0.75]
"Drone battery replacement guide"           →  [-0.45, 0.23, -0.12, ..., 0.34]
```

Similar concepts produce similar vectors. The calibration and maintenance docs have nearby vectors; the drone guide is distant.

## Telemetry vs Documents

SemStreams handles two distinct entity patterns with different embedding behavior:

| Aspect | Telemetry | Documents |
|--------|-----------|-----------|
| **Interface** | `Graphable` only | `ContentStorable` (extends Graphable) |
| **Data** | Numeric triples | Text content (title, body, abstract) |
| **Text storage** | None | ObjectStore |
| **Embeddings** | None generated | Generated from stored text |
| **Clustering** | Explicit relationships | Explicit + semantic similarity |
| **Volume** | High (streaming) | Lower (batch or event) |

### Telemetry (Graphable Only)

Streaming sensor readings with numeric triples. No text content means no embeddings—these entities cluster via explicit relationships defined in their triples.

Example: A temperature sensor has predicates like `sensor.reading.celsius: 23.5` and `sensor.location.zone: zone-A`. The zone relationship creates graph edges for clustering.

### Documents (ContentStorable)

Rich-text content like manuals, logs, or observations. Text is stored in ObjectStore (not triples), and embeddings are generated from that stored content.

The `ContentStorable` interface extends `Graphable` with:
- **StorageRef**: Points to content in ObjectStore
- **ContentFields**: Maps semantic roles (body, abstract, title) to field names
- **RawContent**: Provides the actual text to store

See `ContentStorable` for implementation details.

## How Text Gets to Embeddings

The `ContentStorable` pattern separates metadata (triples) from content (ObjectStore):

```text
1. Processor creates Document payload
          │
2. Processor calls contentStore.StoreContent(ctx, doc)
          │
3. ObjectStore stores body/abstract/title, returns StorageRef
          │
4. Processor calls doc.SetStorageRef(ref)
          │
5. Payload published with StorageRef attached
          │
6. EmbeddingWorker sees entity needs embedding
          │
7. Worker fetches text via StorageRef + ContentFields
          │
8. Text extracted (priority: body > abstract > title)
          │
9. Embedding generated and stored in EMBEDDING_INDEX
```

**Key point:** Text for embeddings comes from the `ContentStorable` interface via ObjectStore.

## Two Embedding Methods

SemStreams supports two embedding providers, both producing compatible vectors:

### BM25 (Tier 1 - Statistical)

Pure Go implementation using lexical matching. Tokenizes text, computes term frequencies, applies BM25 weighting (TF-IDF variant), and normalizes to a 384-dimension vector.

**Characteristics:**
- Lexical only (exact term matching)
- No semantic understanding ("car" ≠ "automobile")
- Fast, pure Go, no external service
- Good baseline, automatic fallback

### Neural (Tier 2 - Semantic)

External HTTP service providing semantic embeddings. Sends text to a neural model which encodes semantic meaning into a dense vector.

SemStreams provides **semembed** as a default embedding service for Tier 2 deployments. It exposes an OpenAI-compatible `/v1/embeddings` endpoint and supports models like `BAAI/bge-small-en-v1.5`. You can also use Ollama, OpenAI, or any compatible service.

**Characteristics:**
- Semantic understanding ("car" ≈ "automobile")
- Requires external service (semembed provided)
- Higher quality clustering
- Automatic BM25 fallback if service unavailable

See [Configuration](../basics/06-configuration.md) for setup details.

### Both Create Vectors

| Aspect | BM25 | Neural |
|--------|------|--------|
| Output | 384-dim vector | 384+ dim vector |
| Similarity | Cosine compatible | Cosine compatible |
| Semantics | Lexical (terms) | Semantic (meaning) |
| Speed | Fast (local) | Slower (network) |
| Dependencies | None | HTTP service |

Both produce vectors that work with the same similarity search and clustering algorithms.

## How SemStreams Uses Embeddings

### 1. Embedding Index (EMBEDDING_INDEX)

Entities with embeddings are indexed for similarity search. Query with natural language to find semantically similar entities—even without exact keyword matches.

### 2. GraphRAG Context

When answering questions, embeddings help find relevant entities beyond keyword matching:

- Query: "How do I maintain temperature sensors?"
- Matches: Calibration guides, maintenance logs, sensor manuals
- Even if none contain the exact phrase "maintain temperature sensors"

## Configuration

### Similarity Threshold

The similarity threshold determines the minimum cosine similarity for semantic search results:

| Threshold | Effect |
|-----------|--------|
| 0.8+ | Strict: Only very similar entities returned |
| 0.6 | Balanced: Related concepts returned (default) |
| 0.4 | Loose: Weakly related entities returned |

See [Configuration](../basics/06-configuration.md) for how to set thresholds.

### Tier Requirements

| Tier | Embeddings | Effect |
|------|------------|--------|
| Tier 0 (Rules-Only) | None | No similarity search; explicit relationships only |
| Tier 1 (Native) | BM25 | Lexical similarity search (term matching) |
| Tier 2 (LLM) | Neural | Semantic similarity search (meaning matching) |

**Graceful degradation:** If the neural embedding service is unavailable, the system falls back to BM25. If embeddings are disabled entirely, communities still form via explicit relationships.

## When Embeddings Matter Most

**High value:**
- Document-heavy domains (manuals, logs, reports)
- Discovery use cases ("find related documents")
- Natural language queries via GraphRAG
- Mixed content where explicit relationships are incomplete

**Lower value:**
- Telemetry-only flows (numeric readings, no text)
- Highly structured domains with complete explicit relationships
- Pure traversal queries (PathRAG)

## Graph Connectivity Without Embeddings

Entities without embeddings (telemetry, structured data) connect through other mechanisms:

### 1. Explicit Triples

Relationships defined in your entity's `Triples()` method create graph edges. A sensor with `sensor.location.zone: zone-A` connects to the zone entity. Entities sharing common neighbors cluster together via LPA.

See [Knowledge Graphs](04-knowledge-graphs.md) for triple patterns.

### 2. Stateful Rules

The rules engine can add triples dynamically based on conditions. When a sensor exceeds a threshold, a rule can add `alert.related: zone-A`—creating runtime relationships without manual coding.

See [Rules Engine](../advanced/06-rules-engine.md) for rule configuration.

### 3. Structural Analysis

Patterns discovered from relationship topology:

- **K-core analysis**: Find densely connected subgraphs
- **Pivot distances**: Estimate structural separation for anomaly detection
- **Anomaly detection**: Spot entities with unusual connectivity patterns

Graph-clustering computes these values in memory only when anomaly detection is
enabled. They are internal detector inputs, not a durable query index.

**Bottom line:** Design meaningful predicates for entities without embeddings. Explicit triples, rules, and structural analysis provide rich graph connectivity—embeddings add semantic similarity on top, but aren't required for a connected graph.

## ObjectStore Requirement

ObjectStore is a separate component that stores text content for embedding generation:

```text
┌─────────────┐      store       ┌─────────────┐
│  Processor  │─────────────────►│ ObjectStore │
│             │                  │   (MinIO)   │
└─────────────┘                  └──────┬──────┘
       │                                │
       │ publish                        │ fetch
       ▼                                │
┌─────────────┐                         │
│    NATS     │                         │
│  (events)   │                         │
└──────┬──────┘                         │
       │                                │
       │ consume                        │
       ▼                                │
┌─────────────┐      retrieve    ┌──────┴──────┐
│  Embedding  │◄─────────────────│ StorageRef  │
│   Worker    │                  │ (in event)  │
└──────┬──────┘                  └─────────────┘
       │
       │ store vector
       ▼
┌─────────────┐
│  EMBEDDING  │
│    INDEX    │
└─────────────┘
```

**Flow:**
1. Processor stores document content to ObjectStore, receives StorageRef
2. Processor publishes event with StorageRef attached
3. EmbeddingWorker consumes event, extracts StorageRef
4. EmbeddingWorker fetches text content from ObjectStore using StorageRef
5. EmbeddingWorker generates vector and stores in EMBEDDING_INDEX

**Deployment implications:**
- **Telemetry-only flows**: ObjectStore not needed, no embeddings generated
- **Document flows**: ObjectStore required for ContentStorable pattern

## Common Issues

### "My documents aren't clustering semantically"

1. Verify entity implements `ContentStorable` (not just `Graphable`)
2. Check `StorageRef` is set (content stored to ObjectStore)
3. Verify embedding service is running (`provider: "http"`)
4. Add explicit relationship triples — community detection clusters on explicit edges only

### "Telemetry entities aren't getting embeddings"

This is expected. Telemetry entities implement only `Graphable`—they have no text content. They cluster via explicit relationships in their triples, not via embeddings.

### "Embedding generation is slow"

1. Embeddings generate asynchronously—they don't block entity updates
2. Use a local embedding service (Ollama) for lower latency
3. Consider BM25 for faster (but less semantic) embeddings

## Related

**Concepts (mental models):**
- [Real-Time Inference](00-real-time-inference.md) - How tiers affect embeddings
- [Knowledge Graphs](04-knowledge-graphs.md) - Triple patterns for explicit relationships
- [Similarity Metrics](06-similarity-metrics.md) - Cosine similarity tuning
- [Community Detection](07-community-detection.md) - How LPA groups entities into communities
- [GraphRAG Pattern](09-graphrag-pattern.md) - Semantic search over communities

**Basics (how to implement):**
- `ContentStorable` - Document entity pattern
- [Configuration](../basics/06-configuration.md) - Tier and threshold settings
- [Rules Engine](../advanced/06-rules-engine.md) - Dynamic relationship creation
