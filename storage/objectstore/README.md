# ObjectStore

NATS JetStream ObjectStore-based storage component for immutable message storage with time-bucketed keys and caching.

## Purpose

ObjectStore provides persistent, versioned storage for messages and binary data using NATS JetStream's ObjectStore
feature. It implements the storage.Store interface with optional LRU/TTL/Hybrid caching for high-performance reads,
time-bucketed key organization for efficient temporal queries, ordinary NATS/JetStream write inputs, and registered
Store access for authorized readers.

## Configuration

```yaml
components:
  - type: objectstore
    config:
      ports:
        inputs:
          - name: write
            type: nats
            subject: storage.objectstore.write
            description: Fire-and-forget async writes
        outputs:
          - name: events
            type: nats
            subject: storage.objectstore.events
            description: Storage events (stored, retrieved)
          - name: stored
            type: jetstream
            subject: storage.objectstore.stored
            interface: storage.stored.v1
            description: StoredMessage with StorageRef for downstream processing
      bucket_name: MESSAGES
      data_cache:
        enabled: true
        max_size: 1000
        ttl: 300  # 5 minutes in seconds
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `bucket_name` | string | `MESSAGES` | NATS ObjectStore bucket name |
| `data_cache.enabled` | bool | `true` | Enable in-memory caching |
| `data_cache.max_size` | int | `1000` | Maximum cache entries (LRU/Hybrid) |
| `data_cache.ttl` | int | `300` | Cache TTL in seconds (TTL/Hybrid) |

### Caching Strategies

Choose caching strategy based on workload characteristics:

**LRU (Least Recently Used)**

- Best for immutable data with fixed working set
- Example: Video archive, historical events
- Memory: `max_size * avg_message_size`

**TTL (Time-To-Live)**

- Best for fresh data requirements
- Example: Sensor readings, real-time events
- Memory: `write_rate * ttl * avg_message_size`

**Hybrid (LRU + TTL)**

- Best for read-heavy workloads with freshness requirements
- Example: Entity states, recent events
- Memory: `min(lru_memory, ttl_memory)`

**No Cache**

- Best for write-heavy, large messages, or unique reads
- Example: Video ingestion, one-time analytics
- Set `data_cache.enabled: false`

## Input/Output Ports

### Input Ports

**Ordinary write inputs** (NATS/JetStream)

- Pattern: Fire-and-forget async writes
- Name: Any local flow-graph label; every declared ordinary input is active, and the name does not select behavior
- Subject: `storage.objectstore.write`
- Payload: Raw message data (any format)
- Behavior: Stores message and publishes storage event
- Auto-detects `ContentStorable` payloads for enhanced storage

### Output Ports

**events** (NATS)

- Pattern: Publish
- Subject: `storage.objectstore.events`
- Payload: JSON Event (`stored`, `retrieved`)
- Purpose: Monitoring and audit trail

**stored** (JetStream)

- Pattern: Publish
- Subject: `storage.objectstore.stored`
- Interface: `storage.stored.v1`
- Payload: `StoredMessage` with `StorageReference`
- Purpose: Semantic processing of stored content

## Storage Operations

### Put Operation

Stores data as immutable versioned object:

```go
err := store.Put(ctx, "events/2024/10/08/14/abc123-def456", data)
```

**Performance:**

- Latency: 10-50ms (NATS RTT dependent)
- Memory: O(message_size)
- Versioning: Automatic, preserves history

### Get Operation

Retrieves latest version with optional caching:

```go
data, err := store.Get(ctx, "events/2024/10/08/14/abc123-def456")
```

**Performance:**

- Cache hit: ~100μs (in-memory)
- Cache miss: 10-50ms (NATS ObjectStore read)
- Memory: O(message_size) + cache overhead

### List Operation

Returns all keys matching prefix:

```go
keys, err := store.List(ctx, "events/2024/10/08/")
```

**Performance:**

- Latency: O(n) where n = total objects in bucket
- Network: Fetches metadata for all objects
- Client-side filtering (NATS limitation)
- Memory: O(num_objects)

### Delete Operation

Marks latest version as deleted (preserves history):

```go
err := store.Delete(ctx, "events/2024/10/08/14/abc123-def456")
```

## Key Generation

### Time-Bucketed Keys (Default)

Format: `{prefix}/year/month/day/hour/{unique-id}`

Example: `events/2024/10/08/14/abc123-def456`

**Benefits:**

- Chronological organization for range queries
- Distributes objects across hierarchy
- Efficient cleanup by time bucket
- Mirrors time-series access patterns

### Custom Key Generation

Implement `storage.KeyGenerator` interface for entity-based keys:

```go
type EntityKeyGenerator struct {
    prefix string
}

func (g *EntityKeyGenerator) GenerateKey(msg any) string {
    entity := msg.(Identifiable)
    timestamp := time.Now().Format(time.RFC3339)
    return fmt.Sprintf("%s/%s/%s", g.prefix, entity.GetID(), timestamp)
}
```

## Example Use Cases

### Video Archive Storage

Store large binary files with LRU caching for frequently accessed content:

```yaml
components:
  - type: objectstore
    config:
      bucket_name: video-archive
      data_cache:
        enabled: true
        max_size: 100
        ttl: 3600
```

**Workflow:**

1. Receive video file on any declared ordinary input
2. Store to ObjectStore with time-bucketed key
3. Publish `stored` event to `events` port
4. Cache popular videos for fast retrieval

### Sensor Data Storage

Store real-time sensor readings with TTL caching for fresh data:

```yaml
components:
  - type: objectstore
    config:
      bucket_name: sensor-data
      data_cache:
        enabled: true
        max_size: 5000
        ttl: 300
```

**Workflow:**

1. Ingest sensor readings via a declared ordinary input
2. Store with entity-based keys (sensor ID + timestamp)
3. Authorized readers resolve the store through StoreRegistry and list recent readings
4. Expire old cache entries after 5 minutes

### Event Sourcing

Store immutable event history with hybrid caching:

```yaml
components:
  - type: objectstore
    config:
      bucket_name: event-store
      data_cache:
        enabled: true
        max_size: 10000
        ttl: 600
```

**Workflow:**

1. Append events via a declared ordinary input (fire-and-forget)
2. Automatic versioning preserves complete history
3. Authorized readers replay events using the registered Store's List operation
4. Recent events cached for fast access

### Content-Addressable Storage

Store ContentStorable payloads with automatic key generation and StorageRef emission:

```yaml
components:
  - type: objectstore
    config:
      ports:
        outputs:
          - name: stored
            type: jetstream
            subject: storage.objectstore.stored
      bucket_name: content-store
```

**Workflow:**

1. Send message implementing `ContentStorable` to a declared ordinary input
2. ObjectStore detects interface and calls `StoreContent(ctx, ContentStorable)`
3. Generates key from `EntityID()` and `ContentType()`
4. Wraps content in `StoredContent` envelope with metadata
5. Emits `StoredMessage` with `StorageReference` to downstream processors

## Architecture Patterns

### Immutable Storage with Versioning

NATS ObjectStore creates new versions on each Put rather than overwriting:

**Benefits:**

- Preserves complete history for audit/replay
- Enables time-travel queries
- Prevents accidental data loss
- Supports append-only semantics

**Trade-off:** Cannot modify data in-place. Use Delete + Put to "update".

### Client-Side List Filtering

List operation fetches all objects from NATS, then filters by prefix client-side:

**Limitations:**

- O(n) performance where n = total objects
- May be slow with >1000 objects
- Consider pagination or external indexing for large datasets

**Rationale:** NATS ObjectStore doesn't support server-side prefix filtering (current version).

### Registered Store Access

The component implements `component.StoreProvider`. ComponentManager registers its live `StreamableStore` under the
logical instance stamped into each `StorageReference`. Authorized internal consumers resolve that instance through
StoreRegistry and use `Get`, `List`, or `Open`; large bodies do not cross a Core NATS request/reply message.

## Thread Safety

Store and message-processing operations are safe for concurrent use:

- Store methods: Thread-safe via NATS ObjectStore and cache concurrency
- Component handlers: Each NATS message processed in separate goroutine
- Metrics: Atomic counters
- Cache: Thread-safe by cache implementation contract

No explicit locks are required for Store calls or message handling. Lifecycle transitions are different: the
composition owner must serialize Start and Stop. Each component instance is one-shot; a completed repeated Stop is a
nil no-op, same-instance restart is rejected, and reuse requires a fresh component instance.

## Performance Characteristics

| Operation | Latency | Throughput | Memory |
|-----------|---------|------------|--------|
| Put | 10-50ms | NATS-limited | O(message_size) |
| Get (cache hit) | ~100μs | Very high | O(message_size) + cache |
| Get (cache miss) | 10-50ms | NATS-limited | O(message_size) + cache |
| List | O(n) objects | Network-limited | O(num_objects) |

**Caching Impact:**

- Read latency: 100x faster (50ms → 100μs)
- Memory overhead: O(cache_size * avg_message_size)
- Write latency: Unaffected (write-through caching)

## Known Limitations

1. **List performance:** O(n) with all objects, may be slow with >1000 objects
2. **No batch operations:** Must Put/Get one at a time
3. **No version retrieval:** Can only access latest version
4. **No server-side filtering:** List filters client-side
5. **Cache invalidation:** No automatic invalidation on external updates

## Error Handling

Store operations return errors for:

- Network failures: NATS connection lost, timeouts
- Not found: Key doesn't exist (Get, Delete)
- Invalid input: Empty keys, nil data
- Resource limits: Bucket quota exceeded

Write-input failures are classified by the component's ordinary Core NATS or JetStream delivery contract. Direct
registered-Store operations return errors to their caller.

## Testing

Integration tests use testcontainers for real NATS instances:

```bash
task test:integration  # Run integration tests with testcontainers
task test:race         # Run with race detector
```

**Example test:**

```go
func TestStore_Integration(t *testing.T) {
    natsClient := getSharedNATSClient(t)
    store, err := objectstore.NewStore(ctx, natsClient, "test-bucket")
    require.NoError(t, err)
    defer store.Close()

    data := []byte("test data")
    err = store.Put(ctx, "test-key", data)
    require.NoError(t, err)

    retrieved, err := store.Get(ctx, "test-key")
    require.NoError(t, err)
    assert.Equal(t, data, retrieved)
}
```

## See Also

- `/storage` - Core storage interfaces
- `/pkg/cache` - Caching implementations (LRU, TTL, Hybrid)
- `/component` - Component interface and lifecycle
- `/natsclient` - NATS client wrapper
