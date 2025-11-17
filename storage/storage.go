// Package storage provides pluggable backend interfaces for storage operations.
package storage

import "context"

// Store is the pluggable backend interface for storage operations.
//
// Each storage component instance creates its own Store implementation with its
// own configuration (bucket name, connection string, etc.). Multiple Store instances
// can run concurrently, each backing a different storage component.
//
// The Store interface uses a simple key-value pattern where:
//   - Keys are strings (hierarchical paths supported via "/" separators)
//   - Values are binary data ([]byte) - supports JSON, videos, images, any binary format
//   - Operations are context-aware for cancellation and timeouts
//
// Example implementations:
//   - objectstore.Store: NATS JetStream ObjectStore backend
//   - s3store.Store: AWS S3 or MinIO backend (future)
//   - sqlstore.Store: PostgreSQL with (key, data) table (future)
//
// Thread Safety:
// All Store implementations must be safe for concurrent use from multiple goroutines.
//
// Example Usage:
//
//	// Create a NATS ObjectStore backend
//	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
//
//	// Store video file
//	videoData := []byte{...}
//	err = store.Put(ctx, "video/sensor-123/2024-10-08.mp4", videoData)
//
//	// Retrieve video
//	data, err := store.Get(ctx, "video/sensor-123/2024-10-08.mp4")
//
//	// List all videos for a sensor
//	keys, err := store.List(ctx, "video/sensor-123/")
type Store interface {
	// Put stores binary data at the specified key.
	// If the key already exists, behavior is implementation-specific:
	//   - Immutable stores (NATS ObjectStore) may append a version/timestamp
	//   - Mutable stores (S3, SQL) will overwrite the existing value
	//
	// The data parameter accepts any binary format:
	//   - JSON-encoded messages
	//   - Video files (MP4, etc.)
	//   - Images (JPEG, PNG, etc.)
	//   - Any []byte data
	Put(ctx context.Context, key string, data []byte) error

	// Get retrieves binary data for the specified key.
	// Returns an error if the key does not exist.
	//
	// The returned []byte should be interpreted by the caller based on
	// their knowledge of what was stored (JSON, video, etc.).
	Get(ctx context.Context, key string) ([]byte, error)

	// List returns all keys matching the specified prefix.
	// The prefix parameter supports hierarchical key patterns:
	//   - "" (empty) lists all keys
	//   - "video/" lists all keys starting with "video/"
	//   - "video/sensor-123/" lists keys for a specific sensor
	//
	// Keys are returned in lexicographic order.
	// Returns an empty slice if no keys match the prefix.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes the data at the specified key.
	// Returns nil if the key doesn't exist (idempotent operation).
	//
	// For immutable stores that maintain versions, this may only mark
	// the latest version as deleted rather than removing historical versions.
	Delete(ctx context.Context, key string) error
}

// KeyGenerator generates storage keys for messages.
// This interface allows for pluggable key generation strategies,
// enabling SemStreams and other layers to provide custom key formats
// while keeping the core storage implementation generic.
//
// Example implementations:
//   - TimeBasedKeyGenerator: Uses timestamp-based keys
//   - EntityBasedKeyGenerator: Uses entity IDs from Identifiable payloads
//   - CompositeKeyGenerator: Combines multiple strategies
type KeyGenerator interface {
	// GenerateKey creates a storage key for the given message.
	// The msg parameter can be any type - the generator should handle
	// type assertions gracefully and provide sensible defaults.
	GenerateKey(msg any) string
}

// MetadataExtractor extracts metadata from messages for storage.
// This interface enables pluggable metadata extraction strategies,
// allowing SemStreams to add semantic metadata while keeping the
// core storage implementation generic.
//
// Metadata is stored as HTTP-style headers (map[string][]string)
// to support NATS JetStream ObjectStore and other header-based systems.
type MetadataExtractor interface {
	// ExtractMetadata returns metadata headers for the given message.
	// Returns nil if the message has no extractable metadata.
	// The msg parameter can be any type - the extractor should handle
	// type assertions gracefully.
	ExtractMetadata(msg any) map[string][]string
}
