// Package embedding provides vector embedding generation and caching for semantic search
// in the knowledge graph.
//
// # Overview
//
// The embedding package generates dense vector representations of text content,
// enabling semantic similarity search across entities. It supports multiple embedding
// strategies: neural embeddings via HTTP APIs (TEI, OpenAI, LocalAI) and pure-Go
// lexical embeddings using BM25 as a fallback.
//
// Embeddings are cached using content-addressed storage (SHA-256 hashes) to enable
// deduplication across entities with identical content. An async worker monitors
// pending embedding requests and processes them in the background.
//
// # Architecture
//
//	                     ┌─────────────────────────────────────────┐
//	                     │              Embedder                   │
//	                     │  (HTTPEmbedder or BM25Embedder)         │
//	                     └─────────────────────────────────────────┘
//	                                       ↓
//	┌────────────────────────────────────────────────────────────────┐
//	│                        Worker                                  │
//	│ Watches EMBEDDING_INDEX KV for status="pending" records        │
//	├──────────────────────────────┬─────────────────────────────────┤
//	│ Check dedup cache            │ Generate new embedding          │
//	│ (content hash lookup)        │ (call embedder)                 │
//	└──────────────────────────────┴─────────────────────────────────┘
//	                                       ↓
//	┌────────────────────────────────────────────────────────────────┐
//	│                     NATS KV Storage                            │
//	├───────────────────────────────┬────────────────────────────────┤
//	│  EMBEDDING_INDEX              │  EMBEDDING_DEDUP               │
//	│  entityID → Record            │  contentHash → DedupRecord     │
//	│  (vector, status, metadata)   │  (vector, entity IDs)          │
//	└───────────────────────────────┴────────────────────────────────┘
//
// # Usage
//
// Configure and use the HTTP embedder with an external service:
//
//	embedder, err := embedding.NewHTTPEmbedder(embedding.HTTPConfig{
//	    BaseURL: "http://tei:8082",
//	    Model:   "all-MiniLM-L6-v2",
//	    Cache:   embedding.NewNATSCache(cacheBucket),
//	})
//
//	// Generate embeddings for batch of texts
//	vectors, err := embedder.Generate(ctx, []string{
//	    "autonomous drone navigation system",
//	    "ground control station for UAV fleet",
//	})
//
// Use BM25 embedder as fallback when neural services unavailable:
//
//	embedder := embedding.NewBM25Embedder(embedding.BM25Config{
//	    Dimensions: 384,
//	    K1:         1.5,  // Term frequency saturation
//	    B:          0.75, // Length normalization
//	})
//
// Start the async worker to process pending embeddings:
//
//	worker := embedding.NewWorker(storage, embedder, indexBucket, logger).
//	    WithWorkers(5).
//	    WithContentStore(objectStore). // For ContentStorable entities
//	    WithOnGenerated(func(entityID string, vector []float32) {
//	        // Update vector index cache
//	    })
//
//	worker.Start(ctx)
//	defer worker.Stop()
//
// # Embedders
//
// HTTPEmbedder ([HTTPEmbedder]):
//
// Calls OpenAI-compatible embedding APIs. Compatible with:
//   - Hugging Face TEI (Text Embeddings Inference) - recommended for local inference
//   - OpenAI cloud API
//   - LocalAI, Ollama, vLLM, and other compatible services
//
// Supports content-addressed caching to avoid redundant API calls.
//
// BM25Embedder ([BM25Embedder]):
//
// Pure Go lexical embeddings using BM25 (Best Matching 25) algorithm:
//   - No external dependencies - works offline
//   - Feature hashing to fixed dimensions
//   - Stopword removal and simple stemming
//   - L2 normalization for cosine similarity
//
// Provides reasonable keyword matching but lacks semantic understanding.
// Use as fallback when neural services unavailable.
//
// # Storage
//
// The package uses two NATS KV buckets:
//
// EMBEDDING_INDEX: Primary storage for embedding records
//   - Key: entity ID
//   - Value: Record with vector, status, metadata
//   - Statuses: pending, generated, failed
//
// EMBEDDING_DEDUP: Content-addressed deduplication
//   - Key: SHA-256 content hash
//   - Value: DedupRecord with vector and entity ID list
//   - Enables sharing vectors across entities with identical content
//
// # ContentStorable Support
//
// For entities with large text content stored in ObjectStore, the worker
// can fetch content dynamically using StorageRef:
//
//	storage.SavePendingWithStorageRef(ctx, entityID, contentHash,
//	    &embedding.StorageRef{
//	        StorageInstance: "main",
//	        Key:             "content/papers/doc123",
//	    },
//	    map[string]string{
//	        message.ContentRoleBody:     "full_text",
//	        message.ContentRoleAbstract: "abstract",
//	        message.ContentRoleTitle:    "title",
//	    },
//	    sourceRevision, // ENTITY_STATES revision for the readiness watermark (0 if unknown)
//	)
//
// # Vector Operations
//
// The package provides common vector operations:
//
//	// Cosine similarity for semantic search
//	similarity := embedding.CosineSimilarity(vectorA, vectorB)
//	// Returns -1 to 1, where 1 = identical, 0 = orthogonal
//
// # Configuration
//
// HTTP embedder configuration:
//
//	BaseURL:  "http://localhost:8082"  # TEI endpoint
//	Model:    "all-MiniLM-L6-v2"       # 384 dimensions, fast
//	APIKey:   ""                       # Optional for local services
//	Timeout:  30s                      # HTTP timeout
//
// BM25 embedder configuration:
//
//	Dimensions: 384    # Match neural models for compatibility
//	K1:         1.5    # Term frequency saturation (1.2-2.0)
//	B:          0.75   # Length normalization (0.0-1.0)
//
// Worker configuration:
//
//	Workers: 5         # Concurrent worker goroutines
//
// # Thread Safety
//
// HTTPEmbedder, BM25Embedder, Storage, and Worker are safe for concurrent use.
// The Worker uses goroutines to process pending embeddings in parallel.
//
// # Metrics
//
// The worker accepts a WorkerMetrics interface for observability:
//   - IncDedupHits(): Embedding reused from dedup cache
//   - IncFailed(): Embedding generation failed
//   - SetPending(): Current pending embedding count
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/graph/indexmanager]: Uses embeddings for similarity search
//   - [github.com/c360studio/semstreams/graph/inference]: Semantic gap detection using similarities
//   - [github.com/c360studio/semstreams/storage/objectstore]: Content storage for large texts
package embedding
