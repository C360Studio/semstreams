package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// NATSCache implements Cache using NATS KV for storage.
//
// Embeddings are stored with content-addressed keys (SHA-256 hash of text)
// to enable deduplication and fast lookups.
type NATSCache struct {
	bucket jetstream.KeyValue
}

// NewNATSCache creates a new NATS KV-backed embedding cache.
func NewNATSCache(bucket jetstream.KeyValue) *NATSCache {
	return &NATSCache{bucket: bucket}
}

// Get retrieves a cached embedding by content hash.
func (c *NATSCache) Get(ctx context.Context, contentHash string) ([]float32, error) {
	entry, err := c.bucket.Get(ctx, contentHash)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, fmt.Errorf("cache miss: %w", err)
		}
		return nil, fmt.Errorf("failed to get from cache: %w", err)
	}

	var embedding []float32
	if err := json.Unmarshal(entry.Value(), &embedding); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached embedding: %w", err)
	}

	return embedding, nil
}

// Put stores an embedding in the cache with the given content hash.
func (c *NATSCache) Put(ctx context.Context, contentHash string, embedding []float32) error {
	data, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("failed to marshal embedding: %w", err)
	}

	if _, err := c.bucket.Put(ctx, contentHash, data); err != nil {
		return fmt.Errorf("failed to put in cache: %w", err)
	}

	return nil
}

// ContentHash generates a SHA-256 hash of text content for use as a cache key.
//
// This function provides consistent hashing across the codebase for
// content-addressed storage.
func ContentHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
