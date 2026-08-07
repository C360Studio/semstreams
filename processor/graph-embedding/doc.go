// Package graphembedding provides the graph-embedding component for generating entity embeddings.
//
// # Overview
//
// The graph-embedding component watches the ENTITY_STATES KV bucket and generates
// vector embeddings for entities with text content, storing durable per-entity
// embedding records in the EMBEDDING_INDEX KV bucket (with EMBEDDING_DEDUP
// carrying the dedup keys). These embeddings enable semantic similarity search
// and clustering.
//
// # Tier
//
// Tier: STATISTICAL (Tier 1) with BM25, SEMANTIC (Tier 2) with HTTP embeddings.
// Not used in Structural (Tier 0) deployments.
//
// # Architecture
//
// graph-embedding is a Tier 1+ component. It is not used in Structural-only
// deployments but required for semantic search and community detection features.
//
//	                    ┌──────────────────┐
//	ENTITY_STATES ─────►│                  ├──► EMBEDDING_INDEX (KV)
//	   (KV watch)       │  graph-embedding ├──► EMBEDDING_DEDUP (KV)
//	                    │                  ├──► GRAPH_STATUS (KV, readiness)
//	                    └────────┬─────────┘
//	                             │
//	                             ▼
//	                    ┌──────────────────┐
//	                    │  Embedding API   │
//	                    │  (HTTP/BM25)     │
//	                    └──────────────────┘
//
// # Features
//
//   - Entity text extraction from configurable fields
//   - HTTP embedding API integration (OpenAI-compatible)
//   - BM25 fallback for offline/lightweight deployments
//   - Batch processing for efficiency
//
// # Configuration
//
// The component is configured via JSON with the following structure:
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}
//	    ]
//	  },
//	  "embedder_type": "http",
//	  "batch_size": 50
//	}
//
// # Port Definitions
//
// Inputs:
//   - KV watch: ENTITY_STATES - watches for entity state changes
//
// Outputs: none declared. The component writes its durable results
// (EMBEDDING_INDEX, EMBEDDING_DEDUP) and its readiness envelope (GRAPH_STATUS)
// directly at Start, not through output ports.
//
// # Embedder Types
//
//   - http: Uses HTTP API (OpenAI-compatible) for embedding generation
//   - bm25: Uses BM25 sparse vectors for lightweight deployments
//
// # Usage
//
// Register the component with the component registry:
//
//	import graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
//
//	func init() {
//	    graphembedding.Register(registry)
//	}
//
// # Dependencies
//
// Upstream:
//   - graph-ingest: produces ENTITY_STATES that this component watches
//
// Downstream:
//   - semantic queries are served by this component over NATS
//     (graph.embedding.query.similar / .search / .status); consumers such as
//     graph-gateway and graph-clustering request them rather than reading the
//     embedding buckets directly
package graphembedding
