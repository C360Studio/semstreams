# IndexManager Service

The IndexManager service is responsible for maintaining secondary indexes by watching ENTITY_STATES KV for changes and updating indexes asynchronously with event buffering and deduplication.

## Overview

IndexManager is part of the GraphProcessor that watches KV changes instead of consuming events directly, enabling eventual consistency and decoupled processing.

### Key Features

- **KV Watch Pattern**: Monitors ENTITY_STATES bucket for changes
- **Event Buffering**: Circular buffer with overflow policies
- **Deduplication**: Prevents duplicate processing of events
- **Batch Processing**: Efficient batch updates with configurable intervals
- **Multiple Index Types**: Predicate, incoming, alias, spatial, and temporal indexes
- **Async Processing**: Non-blocking index updates with worker pools
- **Observability**: Comprehensive metrics and health monitoring

## Architecture

```mermaid
graph TB
    subgraph "IndexManager Service"
        KW[KV Watcher]
        EB[Event Buffer]
        DC[Dedup Cache]
        WP[Worker Pool]
        
        subgraph "Indexes"
            PI[Predicate Index]
            II[Incoming Index]
            AI[Alias Index]
            SI[Spatial Index]
            TI[Temporal Index]
            EI[Embedding Index]
        end
    end
    
    subgraph "Storage"
        ES[ENTITY_STATES KV]
        PIB[PREDICATE_INDEX KV]
        IIB[INCOMING_INDEX KV]
        AIB[ALIAS_INDEX KV]
        SIB[SPATIAL_INDEX KV]
        TIB[TEMPORAL_INDEX KV]
        EIB[EMBEDDING_INDEX KV]
        EDB[EMBEDDING_DEDUP KV]
        CIB[COMMUNITY_INDEX KV]
    end
    
    ES -->|KV Watch| KW
    KW --> EB
    EB --> DC
    DC --> WP
    WP --> PI
    WP --> II
    WP --> AI
    WP --> SI
    WP --> TI
    WP --> EI

    PI --> PIB
    II --> IIB
    AI --> AIB
    SI --> SIB
    TI --> TIB
    EI --> EIB
    EI --> EDB

    Note: COMMUNITY_INDEX used by pkg/graphclustering
```

## Usage

### Basic Setup

```go
import "github.com/c360/semstreams/pkg/processor/graph/indexmanager"

// Create configuration
config := indexmanager.DefaultConfig()
config.Workers = 10
config.EventBuffer.Capacity = 50000
config.Indexes.Predicate = true
config.Indexes.Incoming = true

// Create KV buckets map
buckets := map[string]jetstream.KeyValue{
    "ENTITY_STATES":   entityBucket,
    "PREDICATE_INDEX": predicateBucket,
    "INCOMING_INDEX":  incomingBucket,
    // ... other buckets
}

// Create IndexManager
engine, err := indexmanager.NewIndexManager(config, buckets)
if err != nil {
    log.Fatal("Failed to create IndexManager:", err)
}

// Start the service
ctx := context.Background()
if err := engine.Start(ctx); err != nil {
    log.Fatal("Failed to start IndexManager:", err)
}
defer engine.Stop()
```

### Querying Indexes

```go
// Query by predicate
entities, err := engine.QueryPredicate(ctx, "robotics.sensor.temperature")
if err != nil {
    log.Printf("Query failed: %v", err)
} else {
    log.Printf("Found %d entities with temperature data", len(entities))
}

// Query spatial bounds
bounds := indexmanager.Bounds{
    North: 40.7829,
    South: 40.7489,
    East:  -73.9441,
    West:  -73.9901,
}
entities, err = engine.QuerySpatial(ctx, bounds)
if err != nil {
    log.Printf("Spatial query failed: %v", err)
}

// Query temporal range
start := time.Now().Add(-1 * time.Hour)
end := time.Now()
entities, err = engine.QueryTemporal(ctx, start, end)
if err != nil {
    log.Printf("Temporal query failed: %v", err)
}

// Resolve alias
entityID, err := engine.ResolveAlias(ctx, "drone-alpha")
if err != nil {
    log.Printf("Alias resolution failed: %v", err)
} else {
    log.Printf("Alias 'drone-alpha' resolves to: %s", entityID)
}

// Query incoming relationships
relationships, err := engine.QueryIncoming(ctx, "c360.platform1.robotics.gcs1.drone.1")
if err != nil {
    log.Printf("Incoming query failed: %v", err)
} else {
    log.Printf("Found %d incoming relationships", len(relationships))
}
```

### Batch Operations

```go
// Batch predicate queries
predicates := []string{
    "robotics.sensor.temperature",
    "robotics.sensor.pressure",
    "robotics.status.armed",
}
results, err := engine.QueryPredicates(ctx, predicates)
if err != nil {
    log.Printf("Batch query failed: %v", err)
}

// Batch alias resolution
aliases := []string{"drone-alpha", "drone-beta", "sensor-1"}
resolved, err := engine.ResolveAliases(ctx, aliases)
if err != nil {
    log.Printf("Batch alias resolution failed: %v", err)
}
```

## Configuration

### Complete Configuration Example

```yaml
index_engine:
  # Event buffer configuration
  event_buffer:
    capacity: 50000
    overflow_policy: drop_oldest  # drop_oldest, drop_newest, block
    metrics: true

  # Deduplication settings
  deduplication:
    enabled: true
    window: 1s
    cache_size: 10000
    ttl: 5s

  # Batch processing configuration
  batch_processing:
    size: 500
    interval: 500ms
    parallel_indexes: true

  # Worker pool
  workers: 10

  # Index enable/disable flags
  indexes:
    predicate: true
    incoming: true
    alias: true
    spatial: true
    temporal: true

  # Operation timeouts
  process_timeout: 5s
  query_timeout: 2s

  # KV bucket names
  buckets:
    entity_states: "ENTITY_STATES"
    predicate: "PREDICATE_INDEX"
    incoming: "INCOMING_INDEX"
    alias: "ALIAS_INDEX"
    spatial: "SPATIAL_INDEX"
    temporal: "TEMPORAL_INDEX"
    embedding: "EMBEDDING_INDEX"
    embedding_dedup: "EMBEDDING_DEDUP"
    community: "COMMUNITY_INDEX"

  # Health check settings
  health_check:
    interval: 30s
    max_lag: 5s
    max_backlog: 1000
    max_errors: 10
```

### Configuration Validation

```go
config := indexmanager.DefaultConfig()
config.Workers = 5
config.EventBuffer.Capacity = 10000

// Apply defaults and validate
config.ApplyDefaults()
if err := config.Validate(); err != nil {
    log.Fatal("Invalid configuration:", err)
}

// Check enabled indexes
enabled := config.GetEnabledIndexes()
log.Printf("Enabled indexes: %v", enabled)

// Check if specific index is enabled
if config.IsIndexEnabled("predicate") {
    log.Println("Predicate index is enabled")
}
```

## Index Types

### 1. Predicate Index

Indexes entities by their triple predicates for efficient predicate-based queries.

**CRITICAL: Key Structure**
```
NATS KV Key: The predicate itself (e.g., "robotics.battery.level")
NATS KV Value: JSON object containing entity ID array
```

**Storage Example:**
```
Key:   "robotics.battery.level"
Value: {
  "entities": ["c360.platform1.robotics.gcs1.drone.1", 
               "c360.platform1.robotics.gcs2.drone.1"],
  "count": 2,
  "last_update": 1640995200
}
```

**Query Pattern:**
```go
// CORRECT: Get predicate key directly
entry, _ := bucket.Get(ctx, "robotics.battery.level")

// WRONG: Do NOT expect keys like "robotics.battery.level.entityID"
// The entity IDs are in the VALUE, not the KEY
```

**Use Cases:**
- Find all entities with temperature data: Query key `robotics.sensor.temperature`
- Find all armed drones: Query key `robotics.flight.armed`
- Check if entity has predicate: Get predicate key, check if entity in value array

### 2. Incoming Index

Maintains bidirectional relationship references for efficient reverse lookups.

**Key Structure:**
```
NATS KV Key: The target entity ID (e.g., "c360.platform1.robotics.gcs1.drone.1")
NATS KV Value: JSON object with array of source entity IDs
```

**Storage Example:**
```
Key:   "c360.platform1.robotics.gcs1.drone.1"
Value: {
  "incoming": ["c360.platform1.robotics.mav1.battery.0",
               "c360.platform1.robotics.gcs1.controller.1"]
}
```

**Use Cases:**
- Find what references this entity
- Traverse graph backwards
- Dependency analysis

### 3. Alias Index

Maps human-readable aliases to canonical entity IDs.

**Key Structure:**
```
NATS KV Key: The alias string (e.g., "drone-alpha")
NATS KV Value: The canonical entity ID string
```

**Storage Example:**
```
Key:   "drone-alpha"
Value: "c360.platform1.robotics.gcs1.drone.1"
```

**Use Cases:**
- Resolve "drone-alpha" → "c360.platform1.robotics.gcs1.drone.1"
- Human-friendly entity references
- Legacy system integration

### 4. Spatial Index

Geospatial indexing using simplified geohash approach (Phase 1).

**Key Structure:**
```
NATS KV Key: Geohash prefix (e.g., "dr5r" for NYC area)
NATS KV Value: JSON object with entity locations
```

**Storage Example:**
```
Key:   "dr5r"  // Geohash for Manhattan area
Value: {
  "entities": {
    "c360.platform1.robotics.gcs1.drone.1": {
      "lat": 40.7589,
      "lon": -73.9851,
      "alt": 100.0,
      "updated": 1640995200
    }
  },
  "last_update": 1640995200
}
```

**Use Cases:**
- Location-based queries
- Proximity searches
- Geofencing

### 5. Temporal Index

Time-based indexing with hourly precision buckets.

**Key Structure:**
```
NATS KV Key: Time bucket (e.g., "2024.01.15.14" for Jan 15, 2024, 2PM hour)
NATS KV Value: JSON object with events in that time period
```

**Storage Example:**
```
Key:   "2024.01.15.14"  // YYYY.MM.DD.HH format
Value: {
  "events": [
    {
      "entity": "c360.platform1.robotics.gcs1.drone.1",
      "type": "update",
      "timestamp": "2024-01-15T14:30:00Z"
    }
  ],
  "entity_count": 1
}
```

**Use Cases:**
- Time range queries
- Activity tracking
- Historical analysis

### 6. Embedding Index

**MANAGED BY**: `pkg/embedding` (separate async worker)

Vector embeddings for semantic search with async generation.

**Key Structure:**
```
NATS KV Key: Entity ID (e.g., "c360.platform1.robotics.gcs1.drone.1")
NATS KV Value: JSON object with embedding metadata and status
```

**Storage Example:**
```
Key:   "c360.platform1.robotics.gcs1.drone.1"
Value: {
  "entity_id": "c360.platform1.robotics.gcs1.drone.1",
  "vector": [0.123, -0.456, ...],  // 384-dim for pending/generated
  "content_hash": "a1b2c3d4...",
  "source_text": "drone battery low...",  // Only for pending
  "model": "all-MiniLM-L6-v2",
  "dimensions": 384,
  "generated_at": "2024-01-15T14:30:00Z",
  "status": "generated",  // "pending" | "generated" | "failed"
  "error_msg": ""  // Only if status=failed
}
```

**Async Processing Flow:**
1. IndexManager queues embedding with `status: "pending"`
2. EmbeddingWorker watches EMBEDDING_INDEX via KV watcher
3. Worker processes `status: "pending"` records
4. Checks EMBEDDING_DEDUP for content-hash deduplication
5. Generates embedding (or reuses deduplicated vector)
6. Updates record with `status: "generated"` and vector

**Deduplication (EMBEDDING_DEDUP):**
```
Key:   "a1b2c3d4..."  // SHA-256 hash of source text
Value: {
  "vector": [0.123, -0.456, ...],
  "entity_ids": ["drone.1", "drone.2"],  // Entities sharing this content
  "first_generated": "2024-01-15T14:30:00Z"
}
```

**Performance:**
- **Before (Sync)**: 100 entities/sec (blocking HTTP calls)
- **After (Async)**: 10,000+ entities/sec (non-blocking queue)
- **Worker Pool**: 5 concurrent generators (configurable)

**Use Cases:**
- Semantic similarity search
- Content-based clustering
- Duplicate detection via content-hash

**See**: `pkg/embedding/README.md` for detailed architecture

### 7. Community Index

**MANAGED BY**: `pkg/graphclustering` (Label Propagation Algorithm)

Hierarchical community detection for GraphRAG capabilities.

**Key Structure:**
```
NATS KV Key: "graph.community.{level}.{id}" or "graph.community.entity.{level}.{entityID}"
NATS KV Value: JSON object with community data or entity → community mapping
```

**Storage Example:**
```
Key:   "graph.community.0.comm-0-A1"
Value: {
  "id": "comm-0-A1",
  "level": 0,
  "members": ["doc1", "doc2", "doc3"],
  "statistical_summary": "Community of 3 technical documents...",
  "llm_summary": "This community represents...",
  "summary_status": "llm-enhanced",
  "keywords": ["architecture", "system", "design"],
  "rep_entities": ["doc1", "doc3"]  // PageRank representatives
}

Key:   "graph.community.entity.0.doc1"
Value: "comm-0-A1"  // Entity → Community mapping
```

**Use Cases:**
- Global vs. local search (GraphRAG patterns)
- Semantic clustering
- Representative entity selection
- Community-based caching

**See**: `pkg/graphclustering/README.md` for detailed architecture

## Event Processing

### KV Watch Flow

1. **Watch Setup**: IndexManager creates KV watcher for ENTITY_STATES bucket
2. **Event Detection**: KV changes trigger watch notifications
3. **Operation Classification**: Determine create/update/delete from KV metadata
4. **Event Buffering**: Changes queued in circular buffer
5. **Deduplication**: Skip recently processed events
6. **Batch Processing**: Collect events into batches for efficiency
7. **Index Updates**: Worker pool processes batches and updates indexes

### Operation Types

```go
type Operation string

const (
    OperationCreate Operation = "create"  // KV Put with revision=1
    OperationUpdate Operation = "update"  // KV Put with revision>1
    OperationDelete Operation = "delete"  // KV Delete
)
```

### Deduplication Strategy

Events are deduplicated using a key combining entity ID, revision, and operation:
```
dedup_key = "entityID:revision:operation"
```

TTL-based cache prevents duplicate processing within the deduplication window.

## Error Handling

### Error Types

- **Configuration Errors**: Invalid config, missing buckets
- **Lifecycle Errors**: Start/stop state management
- **Watch Errors**: KV watch failures, disconnections
- **Processing Errors**: Event validation, timeouts
- **Query Errors**: Invalid parameters, index not ready
- **Index-Specific Errors**: Alias not found, invalid bounds

### Error Classification

```go
// Check error types
if indexmanager.IsRetryable(err) {
    // Retry the operation
}

if indexmanager.IsConfigError(err) {
    // Fix configuration
}

if indexmanager.IsQueryError(err) {
    // Handle query failure
}

// Get error severity
severity := indexmanager.GetErrorSeverity(err)
// Returns: "none", "info", "warning", "critical", "fatal"
```

## Metrics and Observability

### Prometheus Metrics

IndexManager exports comprehensive Prometheus metrics:

```go
// Event processing metrics
indexmanager_events_total
indexmanager_events_processed_total  
indexmanager_events_failed_total
indexmanager_events_dropped_total
indexmanager_process_latency_seconds

// Index operation metrics  
indexmanager_index_updates_total{index_type, operation}
indexmanager_index_updates_failed_total{index_type, operation}
indexmanager_index_update_latency_seconds{index_type}

// Query metrics
indexmanager_queries_total{query_type}
indexmanager_queries_failed_total{query_type}
indexmanager_query_latency_seconds{query_type}

// Health metrics
indexmanager_health_status
indexmanager_processing_lag_seconds
indexmanager_backlog_size
```

### Health Monitoring

```go
// Check health status
health := engine.GetIndexHealth()
if !health.IsHealthy {
    log.Printf("IndexManager unhealthy: %s", health.LastError)
    log.Printf("Processing lag: %v", health.ProcessingLag)
    log.Printf("Backlog size: %d", health.BacklogSize)
}

// Get deduplication stats
stats := engine.GetDeduplicationStats()
log.Printf("Deduplication rate: %.2f%%", stats.DeduplicationRate*100)
log.Printf("Cache hit rate: %.2f%%", stats.CacheHitRate*100)
```

### Logging

IndexManager uses structured logging with consistent prefixes:

```
[IndexManager] Started successfully with 10 workers
[IndexManager] KV Watcher started for bucket: ENTITY_STATES
[IndexManager] Processed batch: 450 successful, 2 failed, duration: 15ms
[PredicateIndex] Entity deleted: c360.platform1.robotics.gcs1.drone.1 (cleanup not fully implemented in Phase 1)
```

## Performance Considerations

### Optimization Settings

```yaml
# High throughput configuration
workers: 20
event_buffer:
  capacity: 100000
  overflow_policy: drop_oldest
batch_processing:
  size: 1000
  interval: 100ms
  parallel_indexes: true
deduplication:
  cache_size: 50000
  window: 500ms
```

### Memory Usage

- **Event Buffer**: ~100KB per 1000 events
- **Deduplication Cache**: ~1MB per 10,000 entries
- **Index Data**: Varies by entity count and relationships

### Latency Targets

- **Event Processing**: P95 < 100ms
- **Query Response**: P95 < 10ms
- **Index Update**: P95 < 50ms

## Testing

### Unit Tests

```bash
# Run IndexManager unit tests
go test ./pkg/processor/graph/indexmanager -v

# Run with race detection
go test ./pkg/processor/graph/indexmanager -race

# Run benchmarks
go test ./pkg/processor/graph/indexmanager -bench=.
```

### Integration Tests

IndexManager unit tests use mocks for KV operations and can run in isolation without external dependencies.

### Test Coverage

```bash
# Generate coverage report
go test ./pkg/processor/graph/indexmanager -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Deployment

### Phase 1b Status

- ✅ **Service Extraction**: Complete and functional
- ✅ **KV Watch Implementation**: Fully implemented
- ✅ **Event Processing**: Buffering and deduplication working
- ✅ **All Index Types**: Basic implementations complete
- ✅ **Query Operations**: All query methods implemented
- ✅ **Metrics**: Comprehensive observability
- ❌ **GraphProcessor Integration**: Phase 2 task
- ❌ **Production Optimization**: Future phases

### Known Phase 1 Limitations

1. **Simplified Spatial Index**: Basic geohash implementation
2. **Cleanup Operations**: Delete operations not fully implemented
3. **Index Rebuilding**: Not yet implemented
4. **Advanced Deduplication**: Basic TTL-based approach

### Migration Path

1. **Phase 1b**: IndexManager works in isolation ✅
2. **Phase 2**: Wire IndexManager into GraphProcessor
3. **Phase 3**: Production optimization and advanced features

## Troubleshooting

### Common Issues

**Service Won't Start**
```
Error: failed to create KV watcher: bucket not found
```
- Solution: Ensure all required KV buckets exist
- Check bucket configuration in config.yaml

**High Memory Usage**
```
IndexManager buffer utilization: 95%
```
- Solution: Increase buffer capacity or reduce batch interval
- Monitor deduplication cache size

**Processing Lag**
```
IndexManager processing lag: 2.5s (max: 5s)
```
- Solution: Increase worker count or batch size
- Check for slow index operations

### Debug Commands

```bash
# Check IndexManager status
curl http://localhost:8080/metrics | grep indexmanager

# View health status
curl http://localhost:8080/health/indexmanager

# Monitor processing lag
watch "curl -s http://localhost:8080/metrics | grep processing_lag"
```

## Contributing

When modifying IndexManager:

1. **Update Tests**: Ensure comprehensive unit test coverage
2. **Update Metrics**: Add relevant Prometheus metrics
3. **Update Documentation**: Keep README current
4. **Performance Testing**: Benchmark critical paths
5. **Error Handling**: Follow established error patterns

## Future Enhancements

### Phase 2 (Integration)
- Wire IndexManager into GraphProcessor
- Remove commented GraphProcessor index methods
- End-to-end testing

### Phase 3 (Optimization)
- Advanced spatial indexing (proper geospatial libraries)
- Index rebuilding from entity state
- Bulk operations API
- Query caching
- Index compaction

### Phase 4 (Advanced Features)
- Real-time index streaming
- Cross-index query optimization
- Index statistics and analytics
- Automated index health checks

---

**Status**: Phase 1b Complete ✅  
**Next Phase**: GraphProcessor Integration (IMPL-003)  
**Last Updated**: 2025-09-03