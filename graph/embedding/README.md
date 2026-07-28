# Embedding Package

Vector embedding generation and semantic similarity utilities for the SemStreams graph system.

## Purpose

This package provides interfaces and implementations for generating vector embeddings from text, enabling
semantic similarity search across entities. Supports multiple embedding providers (HTTP APIs, BM25) with
content-addressed deduplication (`EMBEDDING_DEDUP`, keyed via `DedupKey`).

## Key Interfaces

### Embedder

Primary interface for generating vector embeddings from text:

```go
type Embedder interface {
    // Generate creates embeddings for the given texts (batch operation)
    Generate(ctx context.Context, texts []string) ([][]float32, error)

    // Dimensions returns the dimensionality of embedding vectors
    Dimensions() int

    // Model returns the model identifier (for logging/debugging)
    Model() string

    // Close releases any resources held by the embedder
    Close() error
}
```

**Design note**: All providers natively support batch operations following OpenAI API patterns. For single
text embedding, pass a slice with one element.

## Available Implementations

### HTTP Embedder

Connects to remote embedding services via HTTP:

```go
embedder, err := embedding.NewHTTPEmbedder(embedding.HTTPConfig{
    BaseURL: "http://embedding-service:8080",
    Model:   "all-MiniLM-L6-v2",
})
if err != nil {
    return fmt.Errorf("failed to create embedder: %w", err)
}
defer embedder.Close()
```

Typically used with models like `all-MiniLM-L6-v2` (384 dimensions) or similar sentence transformers.

### BM25 Embedder

Statistical keyword-based embeddings for baseline similarity without external dependencies:

```go
embedder := embedding.NewBM25Embedder(corpus)
defer embedder.Close()
```

Useful for development, testing, or scenarios where ML-based embeddings are unavailable.

## Usage Example

```go
// Initialize embedder
embedder, err := embedding.NewHTTPEmbedder(embedding.HTTPConfig{
    BaseURL: "http://embedding-service:8080",
    Model:   "all-MiniLM-L6-v2",
})
if err != nil {
    return fmt.Errorf("failed to create embedder: %w", err)
}
defer embedder.Close()

// Generate embeddings (batch operation)
texts := []string{
    "drone navigation system failure",
    "autonomous vehicle sensor malfunction",
    "robotic arm calibration error",
}

embeddings, err := embedder.Generate(ctx, texts)
if err != nil {
    return fmt.Errorf("embedding generation failed: %w", err)
}

// Process results
for i, emb := range embeddings {
    log.Printf("Text %d: %d-dimensional embedding", i, len(emb))
}
```

## Similarity Functions

### Cosine Similarity

Primary similarity metric for comparing embedding vectors:

```go
func CosineSimilarity(a, b []float32) float64
```

Returns value between -1 and 1:

- **1**: Vectors are identical (maximum similarity)
- **0**: Vectors are orthogonal (unrelated)
- **-1**: Vectors are opposite (maximum dissimilarity)

Formula: `cos(θ) = (A · B) / (||A|| × ||B||)`

**Example**:

```go
sim := embedding.CosineSimilarity(embedding1, embedding2)
if sim > 0.8 {
    log.Println("High semantic similarity detected")
}
```

## Integration with IndexManager

The `processor/graph/indexmanager/` package uses embeddings for semantic search:

```go
// Index entity content
text := extractTextFromEntity(entity)
embeddings, _ := embedder.Generate(ctx, []string{text})
indexManager.IndexEmbedding(ctx, entity.ID, embeddings[0])

// Semantic search
queryEmbeddings, _ := embedder.Generate(ctx, []string{query})
results := indexManager.SearchBySimilarity(ctx, queryEmbeddings[0], threshold)
```

## Performance Considerations

### Batch Operations

Always use batch operations when embedding multiple texts:

```go
// Good: Single batch call
embeddings, _ := embedder.Generate(ctx, texts)

// Bad: Multiple single calls (slower, more overhead)
for _, text := range texts {
    emb, _ := embedder.Generate(ctx, []string{text})
}
```

### Resource Management

Always close embedders when done to release resources:

```go
embedder, err := embedding.NewHTTPEmbedder(embedding.HTTPConfig{
    BaseURL: "http://embedding-service:8080",
    Model:   "all-MiniLM-L6-v2",
})
if err != nil {
    return fmt.Errorf("failed to create embedder: %w", err)
}
defer embedder.Close() // Essential for cleanup
```

For HTTP providers this is typically a no-op, but for local ONNX models this releases GPU/CPU resources.

## Package Location

Previously located at `pkg/embedding/`, this package was moved to `processor/graph/embedding/` per
ADR-PACKAGE-RESPONSIBILITIES-CONSOLIDATION to clarify that embedding generation is graph processing
functionality, not a standalone reusable library.

All graph processing capabilities now live under `processor/graph/`:

- `processor/graph/` - Main processor and mutations
- `processor/graph/querymanager/` - Query execution
- `processor/graph/indexmanager/` - Indexing operations (uses this package)
- `processor/graph/clustering/` - Community detection
- `processor/graph/embedding/` - Vector embeddings (this package)
