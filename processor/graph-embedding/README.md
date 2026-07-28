# graph-embedding

Vector embedding generation component for the graph subsystem.

## Overview

The `graph-embedding` component watches the `ENTITY_STATES` KV bucket and generates vector embeddings for entities. These embeddings enable semantic similarity search and are used by the clustering component for community detection.

## Architecture

```
                    ┌──────────────────┐
ENTITY_STATES ─────►│                  ├──► EMBEDDING_INDEX (KV)
   (KV watch)       │  graph-embedding ├──► EMBEDDING_DEDUP (KV)
                    │                  ├──► GRAPH_STATUS (KV, readiness)
                    └────────┬─────────┘
                             │
                             ▼
                    ┌──────────────────┐
                    │  Embedding API   │
                    │  (HTTP/BM25)     │
                    └──────────────────┘
```

## Features

- **Multiple Embedder Types**: HTTP API (OpenAI-compatible) or BM25 sparse vectors
- **Batch Processing**: Efficient bulk embedding generation
- **Configurable Text Extraction**: Extract text from multiple entity fields
- **Dedup**: unchanged entity text is not re-embedded (EMBEDDING_DEDUP)

## Configuration

```json
{
  "type": "processor",
  "name": "graph-embedding",
  "enabled": true,
  "config": {
    "ports": {
      "inputs": [
        {
          "name": "entity_watch",
          "subject": "ENTITY_STATES",
          "type": "kv-watch"
        }
      ]
    },
    "embedder_type": "http",
    "batch_size": 50
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ports` | object | required | Port configuration for inputs and outputs |
| `embedder_type` | string | "bm25" | Embedder type: "http" or "bm25". HTTP requires model registry with `embedding` capability |
| `batch_size` | int | 50 | Batch size for embedding requests |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| entity_watch | kv-watch | ENTITY_STATES | Watch entity state changes |

### Outputs

None declared. The component writes its durable results (`EMBEDDING_INDEX`,
`EMBEDDING_DEDUP`) and its readiness envelope (`GRAPH_STATUS`) directly at
Start, not through output ports.

## Embedder Types

### HTTP Embedder

Uses an OpenAI-compatible embedding API:

```json
{
  "embedder_type": "http"
}
```

The HTTP embedder URL, model, and API key are resolved from the model registry's `embedding` capability.

Compatible with:
- OpenAI Embeddings API
- Local embedding servers (llama.cpp, text-embeddings-inference)
- Any OpenAI-compatible embedding service

### BM25 Embedder

Uses BM25 sparse vectors for lightweight deployments:

```json
{
  "embedder_type": "bm25"
}
```

No external service required. Suitable for:
- Development environments
- Offline deployments
- Resource-constrained environments

## Embedding Storage

Durable per-entity embedding records are stored in EMBEDDING_INDEX with entity ID as key:

```json
{
  "entity_id": "c360.logistics.warehouse.sensor.temperature.temp-001",
  "vector": [0.123, -0.456, 0.789, ...],
  "model": "BAAI/bge-small-en-v1.5",
  "created_at": "2024-01-15T10:30:00Z",
  "text_hash": "abc123..."
}
```

## Dependencies

### Upstream
- `graph-ingest` - produces ENTITY_STATES that this component watches

### Downstream
- semantic queries are served by this component over NATS
  (`graph.embedding.query.similar` / `.search` / `.status`); consumers such as
  `graph-gateway` and `graph-clustering` request them rather than reading the
  embedding buckets directly

### External
- Embedding API service (if using HTTP embedder)

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_embedding_generated_total` | counter | Total embeddings generated |
| `graph_embedding_dedup_hits_total` | counter | Dedup hits (unchanged entity text not re-embedded) |
| `graph_embedding_api_latency_seconds` | histogram | Embedding API latency |
| `graph_embedding_errors_total` | counter | Total embedding errors |

## Health

The component reports healthy when:
- KV watch subscription is active
- Embedding API is reachable (if using HTTP embedder)
- Error rate is below threshold
