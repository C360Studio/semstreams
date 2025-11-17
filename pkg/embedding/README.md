# Embedding Package

## Overview

The `embedding` package provides asynchronous vector embedding generation for semantic search using a KV-watch pattern. This decouples entity processing from embedding generation, enabling high-throughput entity ingestion without blocking on slow ML service calls.

## Architecture

### Components

```
┌──────────────────────┐
│  EmbeddingStorage    │  ← KV persistence layer
└──────────────────────┘
          │
          ├─────────────┐
          ▼             ▼
  ┌───────────────┐ ┌────────────────┐
  │EMBEDDING_INDEX│ │EMBEDDING_DEDUP │
  │  (per-entity) │ │(content-hash)  │
  └───────────────┘ └────────────────┘
          │
          ▼
    [KV Watcher]  ← Channel-based watch
          │
          ▼
┌──────────────────────┐
│  EmbeddingWorker     │  ← Async worker pool
└──────────────────────┘
          │
          ├─────────────┐
          ▼             ▼
  ┌────────────┐  ┌────────────┐
  │  Embedder  │  │BM25/HTTP   │
  └────────────┘  └────────────┘
```

### Async Processing Flow

```
Entity Creation → IndexManager queues embedding
                       │
                       ├─ Write to EMBEDDING_INDEX with status="pending"
                       │
                       ▼
                [NATS KV Bucket]
                       │
                       ▼ (KV Watch detects change)
                 EmbeddingWorker
                       │
                       ├─ Check EMBEDDING_DEDUP (content-hash)
                       ├─ Generate embedding (if not cached)
                       ├─ Save to EMBEDDING_DEDUP
                       └─ Update EMBEDDING_INDEX with status="generated"
```

## Usage

### Basic Setup

```go
import (
    "context"
    "github.com/c360/semstreams/pkg/embedding"
    "github.com/nats-io/nats.go/jetstream"
)

// 1. Create KV buckets
indexBucket, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket: "EMBEDDING_INDEX",
})
dedupBucket, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket: "EMBEDDING_DEDUP",
})

// 2. Create storage
storage := embedding.NewEmbeddingStorage(indexBucket, dedupBucket)

// 3. Create embedder (HTTP or BM25)
embedder := embedding.NewHTTPEmbedder("http://localhost:8080")
// OR: embedder := embedding.NewBM25Embedder()

// 4. Create worker
worker := embedding.NewEmbeddingWorker(
    storage,
    embedder,
    indexBucket,
    logger,
).WithWorkers(5)  // 5 concurrent workers

// 5. Start worker
if err := worker.Start(ctx); err != nil {
    log.Fatal(err)
}
defer worker.Stop()
```

### Queue Embedding for Generation

```go
// Queue a pending embedding
entityID := "c360.platform1.robotics.drone.001"
text := "autonomous drone with LiDAR sensor"
contentHash := embedding.ContentHash(text)

err := storage.SavePending(ctx, entityID, contentHash, text)
// Worker picks it up automatically via KV watch
```

### Check Embedding Status

```go
record, err := storage.GetEmbedding(ctx, entityID)
if err != nil {
    return err
}

switch record.Status {
case embedding.StatusPending:
    fmt.Println("Embedding generation in progress...")
case embedding.StatusGenerated:
    fmt.Println("Embedding ready:", record.Vector)
case embedding.StatusFailed:
    fmt.Println("Embedding failed:", record.ErrorMsg)
}
```

### Content-Addressed Deduplication

```go
// Check if embedding already exists for this content
contentHash := embedding.ContentHash("drone battery low")
dedupRecord, err := storage.GetByContentHash(ctx, contentHash)

if dedupRecord != nil {
    // Reuse existing embedding
    vector := dedupRecord.Vector
    entityIDs := dedupRecord.EntityIDs  // All entities sharing this content
} else {
    // Need to generate new embedding
}
```

## Interfaces

### Embedder

Abstracts embedding generation (HTTP or BM25):

```go
type Embedder interface {
    Generate(ctx context.Context, texts []string) ([][]float32, error)
    Model() string
    Dimensions() int
    Close() error
}
```

**Implementations**:
- `HTTPEmbedder`: Calls external embedding service
- `BM25Embedder`: Pure Go lexical embeddings (fallback)

## Storage

### EMBEDDING_INDEX

**Bucket**: `EMBEDDING_INDEX`
**Key**: Entity ID (flat, e.g., `c360.platform1.robotics.drone.001`)
**Value**: JSON embedding record

```json
{
  "entity_id": "c360.platform1.robotics.drone.001",
  "vector": [0.123, -0.456, ...],
  "content_hash": "a1b2c3d4e5f6...",
  "source_text": "autonomous drone...",
  "model": "all-MiniLM-L6-v2",
  "dimensions": 384,
  "generated_at": "2024-01-15T14:30:00Z",
  "status": "generated",
  "error_msg": ""
}
```

**Status Progression**:
- `pending` → Awaiting generation
- `generated` → Successfully generated
- `failed` → Generation failed (see error_msg)

### EMBEDDING_DEDUP

**Bucket**: `EMBEDDING_DEDUP`
**Key**: Content hash (SHA-256 of source text)
**Value**: JSON dedup record

```json
{
  "vector": [0.123, -0.456, ...],
  "entity_ids": ["drone.001", "drone.002"],
  "first_generated": "2024-01-15T14:30:00Z"
}
```

**Purpose**: Avoid regenerating embeddings for identical content

## Worker Configuration

### WithWorkers(n int)

Set number of concurrent embedding generators:

```go
worker.WithWorkers(10)  // 10 concurrent workers
```

**Tuning**:
- **Low workers** (1-2): Lower resource usage, slower throughput
- **Default** (5): Balanced for most use cases
- **High workers** (10-20): Faster throughput, higher resource usage

**Bottleneck**: Embedding service capacity (not worker count)

## Performance

### Throughput Comparison

| Metric | Synchronous (Old) | Asynchronous (New) |
|--------|-------------------|-------------------|
| Entity processing | 100 entities/sec | 10,000+ entities/sec |
| Embedding generation | Blocking | Non-blocking queue |
| Improvement | - | **10-100x** |

### Latency

| Operation | Latency |
|-----------|---------|
| Queue pending embedding | < 1ms (KV write) |
| Worker picks up | < 100ms (KV watch) |
| Generate embedding | ~50-200ms (HTTP service) |
| Total (async) | Entity processing continues immediately |

### Memory Usage

- **Per embedding**: ~1.5KB (384-dim float32 vector)
- **Worker overhead**: ~5MB (goroutines, buffers)
- **Dedup cache**: Reduces storage by 30-50% for duplicate content

## Error Handling

### Failed Embeddings

Worker marks embeddings as `failed` with error message:

```go
record, _ := storage.GetEmbedding(ctx, entityID)
if record.Status == embedding.StatusFailed {
    log.Printf("Embedding failed: %s", record.ErrorMsg)
    // Retry logic here if needed
}
```

### Worker Recovery

Worker uses panic recovery:

```go
defer func() {
    if r := recover(); r != nil {
        logger.Error("Worker panic recovered", "panic", r)
    }
}()
```

### Graceful Shutdown

```go
// Context cancellation stops worker gracefully
cancel()
worker.Stop()  // Waits for in-flight operations
```

## Integration

### IndexManager Integration

IndexManager queues embeddings during entity processing:

```go
// In manager.go
if m.embeddingStorage != nil {
    if err := m.queueEmbeddingGeneration(ctx, entityID, entityState); err != nil {
        m.logger.Error("Failed to queue embedding", "error", err)
    }
}
```

### QueryManager Integration

QueryManager reads generated embeddings for semantic search:

```go
record, err := storage.GetEmbedding(ctx, entityID)
if record != nil && record.Status == embedding.StatusGenerated {
    vector := record.Vector
    // Use vector for similarity search
}
```

## Testing

### Unit Tests

```bash
go test ./pkg/embedding -v
```

Tests cover:
- Storage operations (pending, generated, failed)
- Deduplication logic
- Worker lifecycle (start, stop)
- Error handling

### Integration Tests

```bash
INTEGRATION_TESTS=1 go test ./pkg/embedding -v
```

**Requirements**:
- NATS server with JetStream
- Embedding service (or use BM25 fallback)

## Future Enhancements

### Phase 1 (MVP) ✅

- [x] KV-watch pattern
- [x] Async worker pool
- [x] Content-addressed deduplication
- [x] Status progression (pending → generated → failed)
- [x] Graceful shutdown

### Phase 2 (Optimization)

- [ ] Batch embedding generation (reduce HTTP round-trips)
- [ ] Retry logic for failed embeddings
- [ ] Worker health monitoring
- [ ] Metrics (embeddings/sec, queue depth, error rate)

### Phase 3 (Advanced)

- [ ] Priority queue (high-priority entities first)
- [ ] TTL for failed embeddings (auto-retry after delay)
- [ ] Multi-model support (different embedders per entity type)

## Design Decisions

### Why KV Watch Instead of NATS Events?

**KV Watch**:
- ✅ Simpler (no extra event subjects)
- ✅ Built-in persistence (KV bucket stores state)
- ✅ Natural status tracking (pending → generated)
- ✅ Consistent with IndexManager pattern

**NATS Events**:
- ❌ Extra event overhead
- ❌ Requires separate state tracking
- ❌ More complex error recovery

### Why Flat Keys Instead of Dotted?

**Flat keys** (`entity_id`):
- ✅ Simple lookups (direct Get by entity ID)
- ✅ No hierarchy needed
- ✅ Matches other indexes (PREDICATE_INDEX, SPATIAL_INDEX)

**Dotted keys** (`graph.embedding.{id}`):
- ❌ Only useful for hierarchical wildcards
- ❌ Embeddings don't have hierarchy
- ❌ Adds unnecessary complexity

### Why Content-Addressed Deduplication?

**Benefits**:
- Avoid redundant embedding generation
- Reduce storage (30-50% for duplicate content)
- Faster processing (cache hit vs. ML inference)

**Implementation**:
- SHA-256 hash of source text
- Separate EMBEDDING_DEDUP bucket
- Multiple entities can share same vector

## Troubleshooting

### Embeddings Stuck in Pending

**Symptoms**: Records remain `status: "pending"` indefinitely

**Causes**:
- Worker not started
- Worker crashed
- Embedding service unavailable

**Fix**:
```bash
# Check worker logs
grep "Embedding worker" service.log

# Restart worker
worker.Stop()
worker.Start(ctx)
```

### High Memory Usage

**Symptoms**: Worker memory grows unbounded

**Causes**:
- Too many pending embeddings
- Embedding service bottleneck

**Fix**:
```bash
# Increase workers
worker.WithWorkers(10)

# OR: Scale embedding service
```

### Failed Embeddings

**Symptoms**: Many records with `status: "failed"`

**Causes**:
- Embedding service errors
- Invalid input text
- Network issues

**Fix**:
```bash
# Check error messages
for record in failed_records {
    log.Printf("Failed: %s - %s", record.EntityID, record.ErrorMsg)
}

# Requeue failed embeddings
storage.SavePending(ctx, entityID, contentHash, sourceText)
```

## License

Part of the SemStreams framework. See repository LICENSE for details.
