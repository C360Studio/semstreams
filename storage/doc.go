// Package storage provides pluggable backend interfaces for storage operations.
//
// # Overview
//
// The storage package defines the core Store interface and related abstractions
// for persisting binary data with hierarchical key-value semantics. It provides
// a clean, implementation-agnostic API that supports multiple storage backends:
//   - NATS JetStream ObjectStore (immutable, versioned)
//   - AWS S3 or MinIO (mutable, object storage) - future
//   - PostgreSQL (mutable, relational) - future
//
// # Core Concepts
//
// Store Interface:
//
// The Store interface uses a simple key-value pattern where:
//   - Keys are strings (hierarchical paths supported via "/" separators)
//   - Values are binary data ([]byte) - supports any format
//   - Operations are context-aware for cancellation and timeouts
//
// This simplicity enables a wide range of use cases: JSON messages, video files,
// images, or any binary data can be stored with the same interface.
//
// Pluggable Abstractions:
//
// The package defines two pluggable interfaces to enable framework composition:
//   - KeyGenerator: Generates storage keys from messages
//   - MetadataExtractor: Extracts metadata headers from messages
//
// These abstractions allow semantic layers (like SemStreams) to provide custom
// key generation and metadata extraction strategies without modifying core storage.
//
// # Architecture Decisions
//
// Simple Key-Value Model:
//
// The Store interface intentionally uses a simple key-value model rather than
// richer abstractions like queries, indexes, or transactions. This decision:
//   - Keeps implementations simple and focused
//   - Allows diverse backends (object stores, databases, filesystems)
//   - Pushes complex logic to higher layers where it belongs
//   - Enables easy testing with mock implementations
//
// Alternative considered: Query-based interface with filtering
// Rejected because: Too complex, limits backend options, semantic queries
// belong in semantic layers not generic storage.
//
// Hierarchical Keys via "/" Convention:
//
// Keys support hierarchical organization using "/" separators:
//   - "video/sensor-123/2024-10-08.mp4"
//   - "events/robotics/entity-456/2024-10-08T10:30:00Z"
//
// This convention (not enforced by interface):
//   - Works naturally with object stores (S3, NATS ObjectStore)
//   - Enables prefix-based listing and filtering
//   - Mirrors filesystem-like organization users expect
//
// No Forced Immutability:
//
// The Store interface allows implementations to choose mutability semantics:
//   - Immutable stores (NATS ObjectStore): Put() may append version/timestamp
//   - Mutable stores (S3, SQL): Put() overwrites existing values
//
// This flexibility enables diverse backends while documenting behavior per
// implementation.
//
// Context Everywhere:
//
// All Store operations accept context.Context as the first parameter. This
// enables:
//   - Cancellation of long-running operations
//   - Timeout enforcement per operation
//   - Request-scoped tracing and logging
//   - Graceful shutdown of in-flight requests
//
// # Usage Examples
//
// Basic Store Usage:
//
//	// Create NATS ObjectStore backend
//	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
//
//	// Store JSON message
//	msgJSON := []byte(`{"sensor": "temp-123", "value": 22.5}`)
//	key := "events/sensors/temp-123/2024-10-08T10:30:00Z"
//	err = store.Put(ctx, key, msgJSON)
//
//	// Retrieve message
//	data, err := store.Get(ctx, key)
//
//	// List all sensor events
//	keys, err := store.List(ctx, "events/sensors/")
//
//	// Delete old data
//	err = store.Delete(ctx, key)
//
// Implementing Custom KeyGenerator:
//
//	type TimestampKeyGenerator struct {
//	    prefix string
//	}
//
//	func (g *TimestampKeyGenerator) GenerateKey(msg any) string {
//	    // Extract timestamp from message (handle type assertion)
//	    timestamp := time.Now().Format(time.RFC3339)
//	    return fmt.Sprintf("%s/%s", g.prefix, timestamp)
//	}
//
//	// Use with objectstore Component
//	component := objectstore.NewComponent(config, deps)
//	component.SetKeyGenerator(&TimestampKeyGenerator{prefix: "events"})
//
// Implementing Custom MetadataExtractor:
//
//	type EntityMetadataExtractor struct{}
//
//	func (e *EntityMetadataExtractor) ExtractMetadata(msg any) map[string][]string {
//	    // Extract entity ID from message
//	    if entity, ok := msg.(Identifiable); ok {
//	        return map[string][]string{
//	            "entity-id":   {entity.GetID()},
//	            "entity-type": {entity.GetType()},
//	        }
//	    }
//	    return nil
//	}
//
// # Performance Characteristics
//
// The performance of Store operations depends entirely on the backend implementation:
//
// NATS ObjectStore (storage/objectstore):
//   - Put: O(1) - direct write to JetStream
//   - Get: O(1) - direct read with optional caching
//   - List: O(n) - fetches all objects, filters client-side
//   - Delete: O(1) - marks latest version as deleted
//
// Future S3/MinIO Backend:
//   - Put: O(1) - direct S3 upload
//   - Get: O(1) - direct S3 download with caching
//   - List: O(n) - S3 ListObjects with pagination
//   - Delete: O(1) - S3 DeleteObject
//
// Future PostgreSQL Backend:
//   - Put: O(1) - INSERT or UPDATE
//   - Get: O(log n) - indexed key lookup
//   - List: O(n) - prefix query with index scan
//   - Delete: O(log n) - indexed DELETE
//
// Memory:
//
// Store implementations should have bounded memory usage:
//   - Get operations: O(message_size) for returned data
//   - List operations: O(num_matching_keys) for key list
//   - Put operations: O(message_size) during write
//
// Caching (when enabled) adds:
//   - O(cache_size * avg_message_size) persistent memory
//
// # Thread Safety
//
// All Store implementations MUST be safe for concurrent use from multiple
// goroutines. This is a contract requirement of the Store interface.
//
// Example implementations demonstrate thread safety:
//   - objectstore.Store: NATS ObjectStore is thread-safe, cache is thread-safe
//
// # Integration with Components
//
// The storage/objectstore package provides a Component wrapper that:
//   - Exposes Store operations via NATS ports (Request/Response pattern)
//   - Publishes storage events to NATS for observability
//   - Integrates with component discovery and lifecycle
//   - Supports configurable key generation and metadata extraction
//
// This enables storage to participate in NATS-based flows without requiring
// direct Go API access.
//
// # Error Handling
//
// Store implementations should return errors classified by the framework's
// error package:
//   - errors.WrapInvalid: Invalid keys, malformed input
//   - errors.WrapTransient: Network timeouts, temporary failures
//   - errors.WrapFatal: Programming errors, nil pointers
//
// Callers can distinguish error types for appropriate retry/recovery strategies.
//
// # Testing
//
// The storage package emphasizes testing with real backends:
//   - Use testcontainers for NATS JetStream
//   - Avoid mocks - test actual storage behavior
//   - Test with race detector enabled
//   - Test context cancellation and timeout behavior
//
// Example test pattern:
//
//	func TestStore_PutGet(t *testing.T) {
//	    natsClient := getSharedNATSClient(t) // Real NATS
//	    store, err := objectstore.NewStore(ctx, natsClient, "test-bucket")
//	    require.NoError(t, err)
//	    defer store.Close()
//
//	    // Test actual storage operations
//	    data := []byte("test data")
//	    err = store.Put(ctx, "test-key", data)
//	    require.NoError(t, err)
//
//	    retrieved, err := store.Get(ctx, "test-key")
//	    require.NoError(t, err)
//	    assert.Equal(t, data, retrieved)
//	}
//
// # See Also
//
//   - storage/objectstore: NATS ObjectStore implementation
//   - component: Component interface and lifecycle
//   - message: Message types for semantic storage
package storage
