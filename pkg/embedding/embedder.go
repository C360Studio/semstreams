// Package embedding provides embedding generation and caching for semantic search.
//
// This package contains interfaces and implementations for generating vector embeddings
// from text, which are used by the indexmanager for semantic similarity search.
package embedding

import "context"

// Embedder generates vector embeddings for text.
//
// Implementations can use different providers (HTTP APIs, BM25, etc.) while
// maintaining a consistent interface. All providers support batch operations
// natively, following OpenAI API patterns.
type Embedder interface {
	// Generate creates embeddings for the given texts.
	//
	// This is the primary method - batch operations are natural for all providers.
	// For single text, pass a slice with one element.
	// Returns a slice of float32 slices, where each inner slice is an embedding vector.
	Generate(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of embeddings produced by this embedder.
	//
	// For example, all-MiniLM-L6-v2 produces 384-dimensional vectors.
	Dimensions() int

	// Model returns the model identifier used by this embedder.
	//
	// This is useful for debugging and logging which model is being used.
	Model() string

	// Close releases any resources held by the embedder.
	//
	// Must be called when the embedder is no longer needed. For HTTP providers
	// this is typically a no-op, but for local ONNX models this releases GPU/CPU resources.
	Close() error
}

// Cache provides content-addressed caching for embeddings.
//
// Implementations should use a hash of the text content as the key to enable
// deduplication and fast lookups.
type Cache interface {
	// Get retrieves a cached embedding for the given content hash.
	//
	// Returns an error if the embedding is not found in the cache.
	Get(ctx context.Context, contentHash string) ([]float32, error)

	// Put stores an embedding in the cache with the given content hash.
	//
	// The cache should be content-addressed using a cryptographic hash
	// (e.g., SHA-256) of the text content.
	Put(ctx context.Context, contentHash string, embedding []float32) error
}
