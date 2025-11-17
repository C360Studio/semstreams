# QueryManager Service

The QueryManager service provides high-performance read operations for the GraphProcessor with multi-tier caching, KV Watch for cache invalidation, and query optimization. 

## Overview

QueryManager handles all read/query operations for GraphProcessor:

- **Entity Retrieval**: Single and batch entity operations with caching
- **Alias Resolution**: Entity lookup by aliases using IndexManager
- **Complex Queries**: Graph traversal, snapshots, and relationship queries
- **Multi-Tier Caching**: L1 (hot LRU), L2 (warm TTL), L3 (query results)
- **Cache Invalidation**: Real-time KV Watch for cache consistency
- **Query Optimization**: Result caching and query planning

## Architecture

### Multi-Tier Cache Architecture

```text
┌─────────────────────────────────────────────┐
│                QueryManager                  │
├─────────────────────────────────────────────┤
│  L1 Cache (Hot)     │  L2 Cache (Warm)      │
│  - LRU, 1K items    │  - TTL, 10K items     │
│  - Sub-ms latency   │  - 5min expiry        │
│                     │                       │
│  L3 Cache (Query Results)                   │
│  - Hybrid, 100 items, 1min TTL              │
├─────────────────────────────────────────────┤
│  KV Watch (Cache Invalidation)              │
│  - Selective patterns                       │
│  - Batched invalidation                     │
├─────────────────────────────────────────────┤
│  IndexManager (Dependencies)                │
│  - Predicate queries                        │
│  - Spatial/temporal queries                 │
│  - Alias resolution                         │
└─────────────────────────────────────────────┘
```

### Cache Hierarchy Flow

```text
Query Request
      │
      ▼
┌──────────┐  Hit   ┌──────────────────────────┐
│ L1 Cache │───────▶│ Return (sub-ms latency)  │
│   (Hot)  │        └──────────────────────────┘
└──────────┘
      │ Miss
      ▼
┌──────────┐  Hit   ┌──────────────────────────┐
│ L2 Cache │───────▶│ Promote to L1 + Return   │
│  (Warm)  │        └──────────────────────────┘
└──────────┘
      │ Miss  
      ▼
┌──────────┐        ┌──────────────────────────┐
│KV Bucket │───────▶│ Populate L1+L2 + Return  │
│ (Source) │        └──────────────────────────┘
└──────────┘
```

## Key Features

### 1. Multi-Tier Caching with pkg/cache

Uses the unified `pkg/cache` package for all cache tiers:

```go
// L1 Cache - Hot LRU for frequently accessed entities
l1Cache := cache.NewLRU[*EntityState](1000,
    cache.WithMetrics[*EntityState](registry, "query_l1"),
)

// L2 Cache - Warm TTL for broader entity set
l2Cache := cache.NewTTL[*EntityState](ctx, 5*time.Minute, 1*time.Minute,
    cache.WithMetrics[*EntityState](registry, "query_l2"), 
)

// L3 Cache - Query Results with hybrid eviction
l3Cache := cache.NewHybrid[*QueryResult](ctx, 100, 1*time.Minute, 30*time.Second,
    cache.WithMetrics[*QueryResult](registry, "query_l3"),
)
```

### 2. KV Watch for Cache Invalidation

Watches ENTITY_STATES KV bucket for real-time cache invalidation:

```go
// Selective watching of high-change patterns
patterns := []string{
    "c360.platform1.robotics.*.>",  // High-change robotics data
    "c360.platform1.sensors.*.>",   // Sensor data
}

// Batched invalidation for efficiency
invalidationConfig := InvalidationConfig{
    BatchSize:     100,
    BatchInterval: 10*time.Millisecond,
}
```

### 3. Query Operations with Result Caching

Supports complex graph operations with L3 result caching:

```go
// Path traversal with caching
result, err := queryManager.ExecutePath(ctx, startEntity, PathPattern{
    MaxDepth:  5,
    EdgeTypes: []string{"contains", "related_to"},
    Direction: DirectionBoth,
})

// Cached result returned on subsequent calls
```

## Interface

### Core Operations

```go
type QueryManager interface {
    // Lifecycle
    Start(ctx context.Context) error
    Stop() error

    // Entity operations (multi-tier cached)
    GetEntity(ctx context.Context, id string) (*EntityState, error)
    GetEntities(ctx context.Context, ids []string) ([]*EntityState, error)
    GetEntityByAlias(ctx context.Context, aliasOrID string) (*EntityState, error)

    // Complex queries (L3 result cached)
    ExecutePath(ctx context.Context, start string, pattern PathPattern) (*QueryResult, error)
    GetGraphSnapshot(ctx context.Context, bounds QueryBounds) (*GraphSnapshot, error)
    QueryRelationships(ctx context.Context, entityID string, direction Direction) ([]*Relationship, error)

    // IndexManager delegation
    QueryByPredicate(ctx context.Context, predicate string) ([]string, error)
    QuerySpatial(ctx context.Context, bounds SpatialBounds) ([]string, error)
    QueryTemporal(ctx context.Context, start, end time.Time) ([]string, error)

    // Cache management
    InvalidateEntity(entityID string) error
    WarmCache(ctx context.Context, entityIDs []string) error

    // Observability
    GetCacheStats() CacheStats
    Health() QueryManagerHealth
}
```

### Configuration

```yaml
query_manager:
  cache:
    l1_hot:
      type: lru
      size: 1000
      metrics: true
      component: "query_l1"
    
    l2_warm:
      type: ttl
      size: 10000
      ttl: 5m
      cleanup_interval: 1m
      component: "query_l2"
    
    l3_results:
      type: hybrid
      size: 100
      ttl: 1m
      cleanup_interval: 30s
      component: "query_l3"

  invalidation:
    patterns:
      - "c360.platform1.robotics.*.>"
      - "c360.platform1.sensors.*.>"
    batch_size: 100
    batch_interval: 10ms

  query:
    max_query_depth: 10
    max_query_results: 10000
    cache_query_results: true
    result_cache_ttl: 1m

  timeouts:
    entity_get: 2s
    query_timeout: 30s
    kv_get: 1s
```

## Usage Examples

### Basic Entity Operations

```go
// Single entity with caching
entity, err := queryManager.GetEntity(ctx, "c360.platform1.robotics.gcs1.drone.1")
if err != nil {
    return err
}

// Batch entities (mixed cache hits/misses)
entities, err := queryManager.GetEntities(ctx, []string{
    "c360.platform1.robotics.gcs1.drone.1",
    "c360.platform1.robotics.gcs1.drone.2", 
    "c360.platform1.sensors.gcs1.gps.1",
})

// Alias resolution via IndexManager
entity, err := queryManager.GetEntityByAlias(ctx, "N42") // Resolves to actual entity ID
```

### Complex Queries

```go
// Graph path traversal
result, err := queryManager.ExecutePath(ctx, "c360.platform1.robotics.gcs1.drone.1", PathPattern{
    MaxDepth:    3,
    EdgeTypes:   []string{"contains", "related_to"},
    Direction:   DirectionOutgoing,
    IncludeSelf: true,
})

// Spatial graph snapshot
snapshot, err := queryManager.GetGraphSnapshot(ctx, QueryBounds{
    Spatial: &SpatialBounds{
        North: 40.0, South: 30.0,
        East: -70.0, West: -80.0,
    },
    MaxEntities: 1000,
})

// Entity relationships
relationships, err := queryManager.QueryRelationships(ctx,
    "c360.platform1.robotics.gcs1.drone.1", DirectionBoth)
```

## NATS Request/Reply Interface

QueryManager is exposed via NATS request/reply for flow graph communication:

### Query Subjects

```go
// Available NATS subjects for queries
graph.query.entity        // Single entity lookup
graph.query.entities      // Batch entity lookup
graph.query.alias         // Alias resolution
graph.query.path          // Path traversal with resource limits
graph.query.relationships // Entity relationships
graph.query.spatial       // Geospatial queries
graph.query.temporal      // Time-based queries
graph.query.predicate     // Property-based queries
```

### Example NATS Request

```go
// Path traversal via NATS
request := PathQueryRequest{
    StartEntity: entityID,
    Pattern: PathPattern{
        MaxDepth:    3,
        MaxNodes:    100,        // Resource limit
        MaxTime:     100*time.Millisecond,
        EdgeTypes:   []string{"MEMBER_OF", "NEAR"},
        DecayFactor: 0.8,
        Direction:   DirectionOutgoing,
    },
}

// Send request and await response
msg, err := natsClient.Request(ctx, "graph.query.path", request, 5*time.Second)

// Parse response
var response QueryResponse
json.Unmarshal(msg.Data, &response)
```

This interface enables components like the context processor to query the graph without direct dependencies.

### IndexManager Delegation

```go
// Predicate queries (delegates to IndexManager)
entityIDs, err := queryManager.QueryByPredicate(ctx, "type:drone")

// Spatial queries  
entityIDs, err := queryManager.QuerySpatial(ctx, SpatialBounds{
    North: 40.0, South: 30.0, East: -70.0, West: -80.0,
})

// Temporal queries
now := time.Now()
entityIDs, err := queryManager.QueryTemporal(ctx, now.Add(-1*time.Hour), now)
```

## Performance Characteristics

### Cache Performance

- **L1 Hit**: < 1ms (in-memory LRU)
- **L2 Hit**: < 5ms (in-memory TTL with promotion)  
- **KV Miss**: < 50ms (NATS KV fetch + cache population)
- **Cache Promotion**: L2 → L1 on access

### Query Performance

- **Simple Entity**: P95 < 10ms (cached)
- **Batch Entities**: P95 < 50ms (mixed cache)
- **Path Traversal**: P95 < 500ms (depends on depth/complexity)
- **Graph Snapshot**: P95 < 2s (depends on bounds/size)

### Invalidation Performance

- **Single Invalidation**: < 1ms
- **Batch Invalidation**: < 10ms (100 entities)
- **Watch Lag**: < 100ms (KV event → cache invalidation)

## Metrics

### Cache Metrics

```text
# L1 Cache (Hot LRU)
semstreams_querymanager_l1_cache_hits_total
semstreams_querymanager_l1_cache_misses_total
semstreams_querymanager_l1_cache_size
semstreams_querymanager_l1_cache_evictions_total

# L2 Cache (Warm TTL)  
semstreams_querymanager_l2_cache_hits_total
semstreams_querymanager_l2_cache_misses_total
semstreams_querymanager_l2_cache_size
semstreams_querymanager_l2_cache_evictions_total
semstreams_querymanager_l2_cache_expired_total

# L3 Cache (Query Results)
semstreams_querymanager_l3_cache_hits_total
semstreams_querymanager_l3_cache_misses_total
semstreams_querymanager_l3_cache_size
semstreams_querymanager_l3_cache_evictions_total
```

### Query Metrics

```text
# Entity operations
semstreams_querymanager_entity_get_total{status}
semstreams_querymanager_entity_get_duration_seconds{cache_layer}
semstreams_querymanager_entity_batch_total{status}

# Query operations
semstreams_querymanager_query_total{query_type,status}
semstreams_querymanager_query_duration_seconds{query_type}
semstreams_querymanager_query_result_size{query_type}

# Cache invalidation
semstreams_querymanager_invalidations_total
semstreams_querymanager_invalidations_batched_total
semstreams_querymanager_invalidation_duration_seconds
```

### KV Watch Metrics

```text
# Watch events
semstreams_querymanager_watch_events_total{pattern,operation}
semstreams_querymanager_watch_events_duration_seconds
semstreams_querymanager_watch_errors_total
semstreams_querymanager_watch_lag_seconds
semstreams_querymanager_active_watchers
```

## Testing

### Unit Tests

QueryManager has comprehensive isolated unit tests:

```bash
# Run QueryManager tests in isolation
task test ./pkg/processor/graph/querymanager

# With race detection
task test:race ./pkg/processor/graph/querymanager

# With coverage
go test -cover ./pkg/processor/graph/querymanager
```

### Test Structure

```shell
pkg/processor/graph/querymanager/
├── engine_test.go      # Core QueryManager functionality
├── cache_test.go       # Multi-tier cache behavior  
├── watcher_test.go     # KV Watch invalidation
├── query_test.go       # Complex query operations
└── integration_test.go # End-to-end scenarios
```

### Mock Dependencies

Tests use comprehensive mocks for isolation:

- `MockKeyValue`: NATS KV bucket simulation
- `MockIndexManager`: IndexManager dependency simulation
- `MockWatcher`: KV Watch simulation

## Implementation Status

### ✅ Completed

- [x] Complete package structure and interfaces
- [x] Multi-tier cache using pkg/cache (L1 LRU, L2 TTL, L3 Hybrid)
- [x] KV Watch for cache invalidation with selective patterns
- [x] Entity operations (GetEntity, GetEntities, GetEntityByAlias)
- [x] IndexManager delegation (predicate, spatial, temporal queries)
- [x] Comprehensive unit tests with mocks
- [x] Configuration validation and defaults
- [x] Prometheus metrics integration
- [x] Health monitoring and error handling

### 🚧 TODO (Complex Queries)

- [ ] Full ExecutePath implementation (graph traversal algorithm)
- [ ] Full GetGraphSnapshot implementation (bounded queries)
- [ ] Full QueryRelationships implementation (bidirectional relationships)

### ❌ Expected Broken (Phase 2)

- [ ] GraphProcessor tests (missing query methods)
- [ ] E2E tests (no service wiring)
- [ ] Integration tests (services not connected)

## Extracted from GraphProcessor

These methods were moved from GraphProcessor to QueryManager:

```go
// MOVED: GetEntityByAlias + resolveEntityAlias
// Location: /pkg/processor/graph/processor.go:834-845 (commented out)
func (gp *Processor) GetEntityByAlias(ctx context.Context, aliasOrID string) (*gtypes.EntityState, error)
func (gp *Processor) resolveEntityAlias(ctx context.Context, id string) string

// MOVED: All read/query functionality 
// Cache operations, entity retrieval, complex queries
```

## Phase 2 Integration

After QueryManager extraction, Phase 2 will:

1. **Wire Services**: Connect QueryManager to GraphProcessor
2. **Fix Tests**: Update GraphProcessor tests for new architecture
3. **E2E Recovery**: Restore end-to-end functionality
4. **Performance Validation**: Ensure P95 < 100ms target met

## Error Handling

QueryManager uses the unified error handling package:

```go
// Entity not found
if IsEntityNotFound(err) {
    // Handle missing entity
}

// Cache errors (transient, retryable)
if IsCacheError(err) {
    // Retry or degrade gracefully
}

// KV errors (transient)
if IsKVError(err) {
    // Retry with backoff
}

// Query errors (invalid, don't retry)
if IsQueryError(err) {
    // Return error to user
}
```

## Dependencies

### Required Dependencies

- **EntityStates KV Bucket**: Direct access for cache misses
- **IndexManager**: Alias resolution and complex queries
- **pkg/cache**: Multi-tier cache implementation

### Optional Dependencies

- **Metrics Registry**: Prometheus metrics (can be nil)

### No Dependencies On

- **EntityStore**: Only GraphProcessor orchestrates writes
- **Event Streams**: Uses KV Watch instead

## Deployment Considerations

### Memory Usage

- **L1 Cache**: ~100MB (1K entities × ~100KB each)
- **L2 Cache**: ~1GB (10K entities × ~100KB each)
- **L3 Cache**: ~10MB (100 query results × ~100KB each)
- **Total**: ~1.1GB memory footprint

### Watch Patterns

- Use specific patterns, not `WatchAll()` for efficiency
- Monitor watch lag metrics
- Configure appropriate batch sizes for invalidation

### Cache Warming

- Enable cache warming for frequently accessed entities
- Configure warmup patterns based on access patterns
- Monitor cache hit ratios and tune sizes accordingly

---

**QueryManager Status**: ✅ COMPLETE (Core functionality)  
**Phase**: 1c/3 (Final service extraction)  
**Next**: Phase 2 - Service integration and wiring
