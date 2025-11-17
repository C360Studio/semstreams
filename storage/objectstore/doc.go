// Package objectstore provides a NATS JetStream ObjectStore-based storage
// component for immutable message storage with time-bucketed keys and caching.
//
// # Overview
//
// The objectstore package implements the storage.Store interface using NATS
// JetStream's ObjectStore feature. It provides:
//   - Immutable storage with automatic versioning
//   - Time-bucketed key generation for efficient organization
//   - Optional caching (LRU, TTL, or Hybrid)
//   - Component wrapper for NATS-based integration
//   - Configurable metadata extraction
//
// # Core Components
//
// Store:
//
// The Store type implements storage.Store using NATS ObjectStore:
//   - Put: Stores data as immutable objects (versioned)
//   - Get: Retrieves latest version with optional caching
//   - List: Returns all keys matching prefix (client-side filter)
//   - Delete: Marks latest version as deleted (preserves history)
//
// Component:
//
// The Component wrapper exposes Store via NATS ports:
//   - "api" port: Request/Response for Get/Put/List operations
//   - "write" port: Fire-and-forget async writes
//   - "events" port: Publishes storage events (stored, retrieved, deleted)
//
// # Architecture Decisions
//
// Immutable Storage with Versioning:
//
// NATS ObjectStore is immutable - each Put creates a new version rather than
// overwriting. This design:
//   - Preserves complete history for audit/replay
//   - Enables time-travel queries
//   - Prevents accidental data loss
//   - Supports append-only semantics naturally
//
// Trade-off: Cannot modify existing data in-place. Use Delete + Put to "update".
//
// Time-Bucketed Keys:
//
// The DefaultKeyGenerator creates hierarchical keys by timestamp:
//   - Format: "{prefix}/year/month/day/hour/{unique-id}"
//   - Example: "events/2024/10/08/14/abc123-def456"
//
// This bucketing:
//   - Organizes data chronologically for range queries
//   - Distributes objects across hierarchy to avoid hot-spots
//   - Enables efficient cleanup by time bucket
//   - Mirrors common time-series access patterns
//
// Alternative considered: Flat keys with embedded timestamps
// Rejected because: Harder to navigate, less efficient for time-range queries,
// doesn't leverage hierarchical storage systems.
//
// Optional Caching Layer:
//
// The Store can optionally use cache.Cache for retrieved objects:
//   - LRU: Fixed-size cache, evicts least recently used
//   - TTL: Time-based expiration for fresh data
//   - Hybrid: Combines LRU size limits with TTL expiration
//
// Caching decisions:
//   - Read-heavy workloads: Use Hybrid cache (size + TTL)
//   - Write-heavy workloads: Disable cache or use small TTL
//   - Immutable data: Use LRU (data never changes)
//
// Client-Side List Filtering:
//
// The List() operation fetches all objects from NATS ObjectStore, then filters
// client-side by prefix. This is required because NATS ObjectStore doesn't
// support server-side prefix filtering (as of current version).
//
// Performance implications:
//   - O(n) where n = total objects in bucket
//   - May be slow with thousands of objects
//   - Consider pagination or external indexing for large datasets
//
// Alternative considered: Maintain separate index in KV store
// Rejected because: Adds complexity, consistency challenges, NATS may add
// native filtering in future.
//
// Request/Response API Port:
//
// The Component exposes a Request/Response API via NATS:
//   - Client sends Request JSON to "api" subject
//   - Component processes and replies with Response JSON
//   - Enables remote storage access without direct Go API
//
// This enables web clients, other services, or non-Go systems to use storage
// without linking against the Go library.
//
// # Usage Examples
//
// Direct Store Usage:
//
//	// Create store with configuration
//	config := objectstore.DefaultConfig()
//	config.BucketName = "video-storage"
//	config.EnableCache = true
//	config.CacheStrategy = "hybrid"
//	config.CacheTTL = 5 * time.Minute
//
//	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
//
//	// Store video file
//	videoData, _ := os.ReadFile("sensor-123.mp4")
//	key := "video/sensor-123/2024-10-08T14:30:00Z"
//	err = store.Put(ctx, key, videoData)
//
//	// Retrieve with caching
//	data, err := store.Get(ctx, key) // First call: fetches from NATS
//	data, err = store.Get(ctx, key) // Second call: served from cache
//
//	// List all videos for sensor
//	keys, err := store.List(ctx, "video/sensor-123/")
//
// Component Usage (NATS Integration):
//
//	// Configure component
//	config := objectstore.Config{
//	    InstanceName: "video-storage",
//	    Enabled:      true,
//	    BucketName:   "video-storage",
//	    EnableCache:  true,
//	}
//
//	// Create component with dependencies
//	deps := component.Dependencies{
//	    NATSClient:      natsClient,
//	    MetricsRegistry: metricsRegistry,
//	}
//
//	comp, err := objectstore.NewComponent(config, deps)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Start component
//	err = comp.Start(ctx)
//	defer comp.Stop(5 * time.Second)
//
//	// Clients can now use NATS Request/Response:
//	// nats.Request("storage.api", `{"action":"get","key":"video/..."}`)
//
// Custom Key Generation:
//
//	// Implement storage.KeyGenerator
//	type EntityKeyGenerator struct {
//	    prefix string
//	}
//
//	func (g *EntityKeyGenerator) GenerateKey(msg any) string {
//	    entity, ok := msg.(Identifiable)
//	    if !ok {
//	        return g.prefix + "/" + uuid.New().String()
//	    }
//	    timestamp := time.Now().Format(time.RFC3339)
//	    return fmt.Sprintf("%s/%s/%s", g.prefix, entity.GetID(), timestamp)
//	}
//
//	// Use with Component
//	store.SetKeyGenerator(&EntityKeyGenerator{prefix: "entities"})
//
// # Performance Characteristics
//
// Put Operation:
//   - Latency: ~10-50ms (depends on NATS RTT)
//   - Throughput: Limited by NATS ObjectStore write rate
//   - Memory: O(message_size) during write
//
// Get Operation:
//   - Cache hit: ~100μs (in-memory cache lookup)
//   - Cache miss: ~10-50ms (NATS ObjectStore read)
//   - Memory: O(message_size) + cache overhead
//
// List Operation:
//   - Latency: O(n) where n = total objects in bucket
//   - Network: Fetches metadata for all objects
//   - Memory: O(num_objects) for key list
//   - Performance degrades with >1000 objects - consider pagination
//
// Caching Impact:
//   - Read latency: 100x faster (50ms → 100μs)
//   - Memory: O(cache_size * avg_message_size)
//   - Write latency: Unaffected (write-through caching)
//
// # Caching Strategy Guidelines
//
// Choose cache strategy based on workload:
//
// LRU (Least Recently Used):
//   - Best for: Immutable data, fixed working set
//   - Example: Video archive, historical events
//   - Config: CacheStrategy="lru", MaxCacheSize=1000
//
// TTL (Time-To-Live):
//   - Best for: Fresh data requirements, cache invalidation
//   - Example: Sensor readings, real-time events
//   - Config: CacheStrategy="ttl", CacheTTL=5*time.Minute
//
// Hybrid (LRU + TTL):
//   - Best for: Read-heavy with freshness requirements
//   - Example: Entity states, recent events
//   - Config: CacheStrategy="hybrid", MaxCacheSize=1000, CacheTTL=5*time.Minute
//
// No Cache:
//   - Best for: Write-heavy, large messages, unique reads
//   - Example: Video ingestion, one-time analytics
//   - Config: EnableCache=false
//
// Memory usage formula:
//   - LRU: memory = MaxCacheSize * avg_message_size
//   - TTL: memory = write_rate * CacheTTL * avg_message_size
//   - Hybrid: memory = min(LRU_calc, TTL_calc)
//
// # NATS ObjectStore Semantics
//
// The NATS JetStream ObjectStore provides:
//
// Immutability:
//   - Each Put creates a new version
//   - Previous versions preserved (configurable retention)
//   - Get retrieves latest version by default
//
// Versioning:
//   - Automatic version tracking
//   - Can retrieve specific versions (not currently exposed)
//   - Supports rollback and audit trails
//
// Metadata:
//   - HTTP-style headers (map[string][]string)
//   - Automatically tracked: size, chunks, digest
//   - Custom metadata via MetadataExtractor
//
// Storage Model:
//   - Large objects chunked automatically
//   - Efficient for video, images, binary data
//   - Not optimized for tiny messages (<1KB)
//
// # Component Port Configuration
//
// The Component exposes three NATS ports:
//
// API Port (Request/Response):
//   - Subject: "{namespace}.{instance}.api" (default: "storage.{instance}.api")
//   - Pattern: Request/Response
//   - Payload: JSON Request → JSON Response
//   - Operations: Get, Put, List
//   - Timeout: 2 seconds
//
// Write Port (Fire-and-Forget):
//   - Subject: "{namespace}.{instance}.write" (default: "storage.{instance}.write")
//   - Pattern: Fire-and-forget
//   - Payload: Raw binary data
//   - Key: Generated via KeyGenerator
//   - No response (async)
//
// Events Port (Publish-Only):
//   - Subject: "{namespace}.{instance}.events" (default: "storage.{instance}.events")
//   - Pattern: Publish
//   - Payload: JSON Event (action, key, success, error)
//   - Published after: Put, Get, Delete operations
//
// # Configuration Options
//
// See Config struct for all options:
//
//	type Config struct {
//	    InstanceName  string // Component instance name
//	    Enabled       bool   // Enable/disable component
//	    BucketName    string // NATS ObjectStore bucket
//	    EnableCache   bool   // Enable caching
//	    CacheStrategy string // "lru", "ttl", "hybrid"
//	    MaxCacheSize  int    // LRU/Hybrid: max entries
//	    CacheTTL      time.Duration // TTL/Hybrid: expiration
//	}
//
// # Thread Safety
//
// All operations are safe for concurrent use:
//   - Store methods: Thread-safe via NATS ObjectStore and cache concurrency
//   - Component handlers: Each NATS message processed in separate goroutine
//   - Metrics: Atomic counters (atomic.AddUint64)
//   - Cache: Thread-safe by cache implementation contract
//
// No explicit locks required in application code.
//
// # Error Handling
//
// Store operations return errors for:
//   - Network failures: NATS connection lost, timeouts
//   - Not found: Key doesn't exist (Get, Delete)
//   - Invalid input: Empty keys, nil data
//   - Resource limits: Bucket quota exceeded
//
// Component operations:
//   - Invalid requests: Malformed JSON, unknown actions
//   - Store errors: Wrapped with operation context
//   - Timeout errors: Operation exceeded deadline
//
// # Testing
//
// Test with real NATS using testcontainers:
//
//	func TestStore_Integration(t *testing.T) {
//	    natsClient := getSharedNATSClient(t)
//	    store, err := objectstore.NewStore(ctx, natsClient, "test-bucket")
//	    require.NoError(t, err)
//	    defer store.Close()
//
//	    // Test with real NATS ObjectStore
//	    data := []byte("test data")
//	    err = store.Put(ctx, "test-key", data)
//	    require.NoError(t, err)
//
//	    retrieved, err := store.Get(ctx, "test-key")
//	    require.NoError(t, err)
//	    assert.Equal(t, data, retrieved)
//	}
//
// Run with race detector:
//
//	go test -race ./storage/objectstore
//
// # Known Limitations
//
//  1. List() performance: O(n) with all objects - may be slow with >1000 objects
//  2. No batch operations: Must Put/Get one at a time
//  3. No version retrieval: Can only access latest version
//  4. No server-side filtering: List() filters client-side
//  5. Cache invalidation: No automatic invalidation on external updates
//
// # See Also
//
//   - storage: Core storage interfaces
//   - cache: Caching implementations
//   - component: Component interface and lifecycle
//   - natsclient: NATS client wrapper
package objectstore
