package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

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
//
// It keys on text ALONE, so it is only correct for a cache whose lifetime is
// scoped to a single embedder instance (the HTTPEmbedder request-local cache).
// It must NOT be used for the durable EMBEDDING_DEDUP bucket, which outlives
// any one embedder configuration — use DedupKey for that (gh#612).
func ContentHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

// dedupKeySchema domain-separates DedupKey from every other SHA-256 in the
// system and lets a future key-layout change invalidate old keys wholesale by
// bumping the version, rather than silently colliding with them.
const dedupKeySchema = "semstreams/embedding/dedup/v1"

// EmbedderIdentity captures everything a stored vector depends on BESIDES the
// input text. Two embedders that disagree on any of these fields produce
// vectors in different, incomparable vector spaces.
//
// It exists because EMBEDDING_DEDUP is durable, untimed, and never cleared: an
// operator who switches embedder_type bm25 -> http against the same NATS state
// would otherwise get every already-embedded entity's OLD vector back, stamped
// with the NEW model's name. When both spaces happen to share a dimension count
// (384 is the default for both BM25 here and TEI's all-MiniLM-L6-v2), cosine
// similarity returns a plausible-looking score across unrelated spaces and no
// health signal fires. Folding identity into the key makes that impossible:
// stale keys simply never match again and age out.
type EmbedderIdentity struct {
	// Type is the configured embedder kind ("bm25", "http").
	Type string
	// Model is the embedder's model identifier.
	Model string
	// Dimensions is the vector width the embedder produces.
	Dimensions int
	// MaxTextLen is the source-text truncation cap applied before generation.
	// It belongs in the key because the cap is derived from the embedder type
	// (4000 for bm25, 8000 otherwise): flipping type changes the TEXT that gets
	// embedded, not only the model that embeds it.
	MaxTextLen int
}

// DedupKey derives the EMBEDDING_DEDUP key for text under a given embedder
// identity, or "" when the identity is not yet resolved.
//
// Every field is length-prefixed before hashing so that no two distinct field
// tuples can serialize to the same byte stream (without prefixes, {"ab","c"}
// and {"a","bc"} would collide).
//
// An identity with a non-positive Dimensions has not learned its vector width
// yet (HTTPEmbedder resolves it from the first API response) and gets NO key.
// "" is the package's established "dedup disabled for this record" signal, which
// both hops already honour: it is stored as the pending record's ContentHash and
// getOrGenerateEmbedding generates unconditionally for it. The alternative of
// keying on a placeholder width is what split EMBEDDING_DEDUP into two disjoint
// keyspaces per embedder; omitting Dimensions from the key instead would be worse
// still, since keys would then collide across models of different width — exactly
// what folding identity into the key was meant to prevent. The cost is redundant
// embeds during the brief unresolved window, once per process.
func DedupKey(id EmbedderIdentity, text string) string {
	if id.Dimensions <= 0 {
		return ""
	}

	h := sha256.New()
	writeField := func(s string) {
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(s)))
		_, _ = h.Write(lenBuf[:])
		_, _ = h.Write([]byte(s))
	}

	writeField(dedupKeySchema)
	writeField(id.Type)
	writeField(id.Model)
	writeField(strconv.Itoa(id.Dimensions))
	writeField(strconv.Itoa(id.MaxTextLen))
	writeField(text)

	return hex.EncodeToString(h.Sum(nil))
}
