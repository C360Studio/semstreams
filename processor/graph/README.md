# GraphProcessor README

**Last Updated**: 2025-09-02  
**Maintainer**: SemStreams Core Team

## Purpose & Scope

**What this component does**: Processes semantic messages from input components into entity states and manages the semantic graph through NATS KV storage.

**Key responsibilities**:
- Process BaseMessage payloads that implement the simplified Graphable interface (EntityID() + Triples()) into entity states
- Store entity states in NATS KV buckets with versioning and caching
- Extract and process entity relationships into graph edges
- Maintain spatial, temporal, and incoming indexes for efficient graph queries
- Provide single-writer consistency for entity state management
- Enable consumers to watch entity states via KV watch pattern

**NOT responsible for**: Message input handling, rule processing, UI presentation, or message routing outside the graph domain.

## Architecture Context

### Integration Points
- **Consumes from**: 
  - `process.>` NATS subjects (from RoboticsProcessor and other semantic processors)
  - `graph.events.>` NATS subjects (mutation requests from Rules, Admin, Import tools)
- **Provides to**: ENTITY_STATES KV bucket (watched by RuleProcessor, WebSocket output, monitoring systems)
- **External dependencies**: NATS client, ObjectStore for full message persistence

### Data Flow

#### Primary Flow: Message Processing
```
BaseMessage with Graphable payload → GraphProcessor → Entity States (KV)
                                                          ↓
                                              Consumers watch KV bucket
Example: MAVLink position message → process.robotics.position → ENTITY_STATES["c360.platform1.robotics.gcs1.drone.1"]
```

#### Mutation Flow: Event-Driven Updates
```
Rules/Admin/Import → graph.events → GraphProcessor → Entity States (KV)
                                                          ↓
                                              Consumers see updates via KV watch
Example: Rule infers alert → graph.events (ADD_TRIPLE) → GraphProcessor updates entity
```

### KV Watch Pattern
Consumers watch ENTITY_STATES bucket directly instead of subscribing to events:
```go
// Watch specific entities or patterns
watcher, _ := entityBucket.Watch("c360.platform1.robotics.*.drone.>")
for entry := range watcher.Updates() {
    // Process entity state changes
}
```

### Configuration
```json
{
  "entity_cache": {
    "enabled": true,
    "strategy": "lru",
    "max_size": 10000,
    "ttl": 0
  },
  "entity_states": {
    "ttl": 0,
    "history": 10,
    "replicas": 1
  },
  "spatial_index": {
    "ttl": "1h",
    "history": 5,
    "replicas": 1
  },
  "temporal_index": {
    "ttl": "24h",
    "history": 3,
    "replicas": 1
  },
  "incoming_index": {
    "ttl": 0,
    "history": 5,
    "replicas": 1
  },
  "index_monitoring": {
    "enabled": false,
    "interval": "1h",
    "auto_repair": false,
    "max_drift": 10
  },
  "input_subject": "process.>"
}
```

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work

1. **BaseMessage Processing with Graphable Payloads**: Receives semantic messages and extracts entities
   - **Input**: BaseMessage with payload implementing message.Graphable interface
   - **Expected**: Entity states created with Triples (properties and relationships unified)
   - **Verification**: Check entity stored in KV bucket with correct version

2. **Entity State Storage and Versioning**: Manages entity lifecycle with proper versioning
   - **Input**: New entity (version 1) or existing entity update
   - **Expected**: Version incremented for updates, edges preserved, properties merged by confidence
   - **Verification**: Retrieve entity from KV bucket, verify version number and merged properties

3. **KV Watch Compatibility**: Entity changes are observable via KV watch
   - **Input**: Entity state changes (create/update/delete)
   - **Expected**: KV watchers receive notifications with correct operation type and revision
   - **Verification**: Set up KV watch, verify updates received for entity changes

4. **KV Bucket Self-Management**: Initializes and manages four KV buckets
   - **Input**: Component startup with valid NATS client
   - **Expected**: ENTITY_STATES, SPATIAL_INDEX, TEMPORAL_INDEX, INCOMING_INDEX buckets created
   - **Verification**: Verify buckets exist with correct TTL and replica settings

5. **Entity Caching Performance**: Uses LRU cache for frequently accessed entities
   - **Input**: Repeated access to same entity ID
   - **Expected**: Second access served from cache, not KV bucket
   - **Verification**: Monitor cache hit/miss ratios and access patterns

6. **Relationship Edge Processing**: Processes RelationshipHints into graph edges
   - **Input**: Graphable payload with RelationshipHints
   - **Expected**: Edges added to source entities, incoming index updated for target entities
   - **Verification**: Check entity edges array, verify incoming index entries

7. **Property Confidence Merging**: Intelligently merges entity properties based on confidence scores
   - **Input**: Existing entity with confidence 0.8, new update with confidence 0.9
   - **Expected**: Higher confidence properties replace lower confidence ones
   - **Verification**: Check final entity properties reflect highest confidence values

### Error Conditions - What Should Fail Gracefully

1. **Invalid BaseMessage Format**: Handles malformed JSON messages
   - **Trigger**: Send invalid JSON to semantic.> subject
   - **Expected**: Error logged, message processing skipped, component continues
   - **Recovery**: Component remains healthy, processes next valid message

2. **Non-Graphable Payload Processing**: Handles legacy payloads without Graphable interface
   - **Trigger**: BaseMessage with payload not implementing Graphable
   - **Expected**: Falls back to basic entity extraction using EntityID interface or map[string]any
   - **Recovery**: Creates entity with default confidence and properties

3. **NATS KV Bucket Unavailable**: Handles temporary KV storage failures
   - **Trigger**: NATS KV bucket becomes unavailable during entity storage
   - **Expected**: Retry with exponential backoff, error recorded but component continues
   - **Recovery**: Resumes normal operation when KV bucket restored

4. **Graph Event Processing Failure**: Handles invalid mutation requests
   - **Trigger**: Malformed graph.events message or invalid mutation type
   - **Expected**: Error logged, invalid request skipped, processing continues
   - **Recovery**: Component continues processing valid mutation requests

5. **Entity Validation Failures**: Rejects invalid entity states
   - **Trigger**: Entity state with empty ID or nil state
   - **Expected**: Validation error returned, entity not stored, error logged
   - **Recovery**: Component continues processing other valid entities

6. **ObjectStore Failure During Message Persistence**: Handles ObjectStore unavailability
   - **Trigger**: ObjectStore unavailable during full message storage
   - **Expected**: Error returned, entity processing skipped, component continues
   - **Recovery**: Resumes when ObjectStore restored

### Edge Cases - Boundary Conditions
- **High Message Volume**: Maintains sub-100ms processing latency under sustained load
- **Large Entity States**: Handles entities with extensive property maps and edge lists
- **Rapid Entity Updates**: Manages high-frequency updates to same entity with proper versioning
- **Memory Pressure**: Cache eviction under memory constraints maintains system stability
- **Network Partitions**: Graceful degradation when NATS components temporarily unavailable

## Usage Patterns

### Typical Usage (How Other Code Uses This)
```go
// Create GraphProcessor
config := &Config{
    EntityCache: cache.Config{
        Enabled:  true,
        Strategy: cache.StrategyLRU,
        MaxSize:  10000,
        TTL:      0,
    },
    InputSubject:  "process.>",  // Domain processors send here
    // Note: No OutputSubject - consumers use KV watch instead
}

processor, err := NewProcessor("graph-processor", natsClient, objectStore, config)
if err != nil {
    return fmt.Errorf("create processor: %w", err)
}

// Start processing (implements LifecycleComponent)
ctx := context.Background()
err = processor.Start(ctx)
if err != nil {
    return fmt.Errorf("start processor: %w", err)
}

// Component will subscribe to semantic.> and begin processing messages
// Stop when done
err = processor.Stop(ctx)
```

### Common Integration Patterns
- **Pipeline Component**: Used as middle stage in processing pipeline between input and output
- **Single Writer Pattern**: ONLY component that writes to ENTITY_STATES KV bucket
- **Graph Database Alternative**: Provides entity-relationship storage without external graph database
- **Mutation Request Handler**: Processes graph.events for entity mutations from Rules, Admin, etc.

## Testing Strategy

### Test Categories
1. **Unit Tests**: Test individual methods like entity validation, property merging, index updates
2. **Integration Tests**: Test with real NATS client, KV buckets, and JetStream
3. **Error Tests**: Test failure modes, retry logic, circuit breaker behavior, graceful degradation

### Test Quality Standards
- ✅ Tests MUST create real GraphProcessor instances with actual dependencies
- ✅ Tests MUST verify entity storage in KV buckets and mutation handling from graph.events
- ✅ Tests MUST be able to fail when entity processing, caching, or mutation logic is broken
- ❌ NO signature-only tests that just verify method calls without checking behavior
- ❌ NO tests that return success without verifying entity states in KV buckets

### Mock vs Real Dependencies
- **Use real dependencies for**: NATS client, KV buckets, JetStream, ObjectStore (core storage/messaging)
- **Use mocks for**: External HTTP APIs, hardware interfaces, slow external services
- **Testcontainers for**: NATS server with KV and JetStream enabled

## Metrics

This component exposes the following Prometheus metrics to monitor graph processing performance and identify storage bottlenecks:

| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| semstreams_graph_messages_received_total | Counter | Total semantic messages received | subject |
| semstreams_graph_entities_processed_total | Counter | Total entities created or updated | entity_type, action |
| semstreams_graph_mutations_processed_total | Counter | Mutation requests from graph.events | mutation_type |
| semstreams_graph_kv_operation_duration_seconds | Histogram | KV bucket operation latency | operation, bucket |
| semstreams_graph_entity_cache_requests_total | Counter | Entity cache access requests | result |
| semstreams_graph_entity_cache_hit_ratio | Gauge | Cache hit ratio (0-1) | - |
| semstreams_graph_jetstream_publish_duration_seconds | Histogram | JetStream event publishing latency | subject |
| semstreams_graph_processing_duration_seconds | Histogram | End-to-end message processing latency | message_type |
| semstreams_graph_active_entities | Gauge | Number of entities currently in cache | - |
| semstreams_graph_bucket_size_bytes | Gauge | Approximate KV bucket storage size | bucket_name |
| semstreams_graph_relationship_edges_total | Counter | Graph edges created or updated | relationship_type |
| semstreams_graph_errors_total | Counter | Processing errors encountered | error_type |
| semstreams_graph_cas_conflicts_total | Counter | CAS version conflicts encountered | entity_type |
| semstreams_graph_cas_retries_total | Counter | CAS retry attempts | entity_type |
| semstreams_graph_cas_retry_exhausted_total | Counter | CAS max retries exceeded | entity_type |
| semstreams_graph_worker_queue_depth | Gauge | Current worker pool queue depth | - |
| semstreams_graph_worker_utilization | Gauge | Worker pool utilization (0-1) | - |

### Key Performance Indicators
- **Throughput**: `rate(semstreams_graph_messages_received_total[1m])` - Messages/sec being processed
- **Processing Latency**: `histogram_quantile(0.95, semstreams_graph_processing_duration_seconds)` - P95 end-to-end latency
- **KV Performance**: `histogram_quantile(0.95, semstreams_graph_kv_operation_duration_seconds)` - KV storage latency
- **Cache Efficiency**: `semstreams_graph_entity_cache_hit_ratio` - Higher values indicate better cache performance
- **Mutation Rate**: `rate(semstreams_graph_mutations_processed_total[1m])` - Mutation requests/sec from graph.events

### Example Prometheus Queries
```promql
# Current graph processing throughput
rate(semstreams_graph_messages_received_total[1m])

# Entity creation/update breakdown
sum(rate(semstreams_graph_entities_processed_total[1m])) by (action)

# KV storage performance by operation
histogram_quantile(0.99, sum(semstreams_graph_kv_operation_duration_seconds) by (operation, le))

# Cache performance monitoring
semstreams_graph_entity_cache_hit_ratio

# Identify slow message types
histogram_quantile(0.95, sum(semstreams_graph_processing_duration_seconds) by (message_type, le)) > 0.1

# Storage utilization by bucket
sum(semstreams_graph_bucket_size_bytes) by (bucket_name)

# CAS retry rate monitoring (should be < 5%)
rate(semstreams_graph_cas_conflicts_total[1m]) / rate(semstreams_graph_kv_operation_duration_seconds_count{operation="put"}[1m]) * 100

# Worker pool saturation
semstreams_graph_worker_queue_depth / 1000 * 100  # Queue depth as percentage
```

### Alerting Rules
```yaml
# Low cache hit rate
- alert: GraphProcessorLowCacheHitRate
  expr: semstreams_graph_entity_cache_hit_ratio < 0.7
  for: 5m
  annotations:
    summary: "Graph processor cache hit rate below 70%"

# High KV operation latency
- alert: GraphProcessorSlowKVOperations
  expr: histogram_quantile(0.95, semstreams_graph_kv_operation_duration_seconds) > 0.1
  for: 3m
  annotations:
    summary: "Graph processor KV operations P95 latency >100ms"

# Processing errors
- alert: GraphProcessorErrors
  expr: rate(semstreams_graph_errors_total[5m]) > 1
  for: 2m
  annotations:
    summary: "Graph processor error rate >1 error/sec"

# Message processing latency too high
- alert: GraphProcessorSlowProcessing
  expr: histogram_quantile(0.95, semstreams_graph_processing_duration_seconds) > 0.5
  for: 3m
  annotations:
    summary: "Graph processor P95 processing latency >500ms"

# High mutation request failures
- alert: GraphProcessorMutationFailures
  expr: rate(semstreams_graph_errors_total{error_type="mutation"}[5m]) > 1
  for: 2m
  annotations:
    summary: "Graph processor mutation request failure rate >1/sec"

# High CAS retry rate
- alert: GraphProcessorHighCASRetryRate
  expr: rate(semstreams_graph_cas_conflicts_total[5m]) / rate(semstreams_graph_kv_operation_duration_seconds_count{operation="put"}[5m]) > 0.05
  for: 3m
  annotations:
    summary: "Graph processor CAS retry rate >5% - investigate hot entities"

# Worker pool saturation
- alert: GraphProcessorWorkerPoolSaturated
  expr: semstreams_graph_worker_queue_depth > 800
  for: 2m
  annotations:
    summary: "Graph processor worker pool queue >80% full - consider scaling workers"
```

## Implementation Notes

### Thread Safety
- **Concurrency model**: Multi-goroutine message processing with sync.RWMutex for shared state
- **Shared state**: Message processing statistics, error counters, circuit breaker state
- **Critical sections**: Entity cache access, statistics updates, circuit breaker state changes

### Performance Considerations
- **Expected throughput**: 1000+ messages/second with sub-100ms processing latency
- **Memory usage**: Configurable LRU cache (default 10,000 entities) + processing overhead
- **Bottlenecks**: NATS KV PUT operations, JSON marshaling/unmarshaling, cache eviction

#### Known Performance Issue: KV Cold-Start Latency
- **Problem**: First KV GET operation for each entity takes ~307ms due to NATS KV bucket initialization
- **Impact**: With 10 unique entities, cold-start adds 3+ seconds to E2E latency (P95: 900ms vs <100ms target)
- **Root Cause**: NATS KV bucket connection initialization happens on first operation
- **Solution**: Cache warming during processor.Start() to pre-initialize KV connection

#### Cache Warming Strategy
- **Implementation**: During Start(), list up to 100 keys from KV bucket and load into cache
- **Benefits**: 
  - Eliminates cold-start penalty by initializing KV connection at startup
  - Pre-populates cache with recently used entities for warm restarts
  - No dummy operations - all loads are useful data

#### Concurrent Processing & Race Condition Prevention

##### The Problem: Concurrent Entity Updates
When multiple workers process messages for the same entity concurrently:
1. Worker A and B both read entity version 1 from cache/KV
2. Both process and increment to version 2
3. Both write version 2 to KV (should be version 3!)
4. **Result**: Lost update, incorrect version sequence

##### The Solution: CAS (Compare-And-Swap) with NATS KV
NATS KV provides optimistic concurrency control via revision checking:
- **Get**: Returns entity data + revision number
- **Update**: Only succeeds if revision hasn't changed
- **Create**: Only succeeds if key doesn't exist
- **Automatic retry**: On revision conflict, re-read and retry

##### Implementation Pattern
```go
// Instead of blind PUT:
bucket.Put(ctx, key, data)  // ❌ Last writer wins

// Use CAS operations:
entry, _ := bucket.Get(ctx, key)
revision := entry.Revision()
// ... process entity ...
err := bucket.Update(ctx, key, newData, revision)  // ✅ Fails if version changed
if err != nil && isRevisionConflict(err) {
    // Retry with fresh data
}
```

##### Worker Pool Architecture
With CAS handling concurrency at the storage layer, the worker pool becomes simple:
- **No entity routing needed** - Any worker can process any entity
- **No locking required** - NATS KV handles version conflicts
- **Full parallelism** - Workers process independently
- **Automatic retry** - On conflict, re-read and retry (max 3 attempts)

##### Concurrency Metrics
Monitor retry rates to ensure system health:
- `semstreams_graph_cas_conflicts_total` - Count of version conflicts
- `semstreams_graph_cas_retries_total` - Count of retry attempts
- `semstreams_graph_cas_retry_exhausted_total` - Count of max retries exceeded
- **Alert threshold**: If retry rate > 5%, investigate hot entities

##### Worker Pool Configuration
```json
{
  "worker_pool": {
    "enabled": true,
    "workers": 10,        // Number of concurrent workers
    "queue_size": 1000,   // Message queue buffer size
    "max_retries": 3      // Max CAS retry attempts per operation
  }
}
```

### Error Handling Philosophy
- **Error propagation**: Errors logged and recorded but don't stop component processing
- **Retry strategy**: Exponential backoff for transient failures (KV, JetStream operations)
- **Circuit breaking**: Protects JetStream from repeated publishing failures

## Troubleshooting

### Common Issues
1. **Messages not processed**: Check semantic.> subscription and BaseMessage JSON format
   - **Cause**: Invalid JSON structure or missing required BaseMessage fields
   - **Solution**: Verify message format matches BaseMessage specification

2. **Mutations not being processed**: Check graph.events subscription and message format
   - **Cause**: Invalid GraphEvent structure or missing required fields
   - **Solution**: Verify mutation event format matches GraphEvent specification

3. **High memory usage**: Check entity cache size and eviction policy
   - **Cause**: Cache size too large or cache not evicting properly under pressure
   - **Solution**: Reduce cache size or tune eviction parameters

4. **Slow processing**: Check KV bucket performance and message processing latency
   - **Cause**: NATS KV operations slow or large message processing overhead
   - **Solution**: Monitor KV bucket performance, optimize message structure

### Debug Information
- **Logs to check**: Entity processing logs, KV storage operations, graph.events mutation handling
- **Metrics to monitor**: Messages processed/sec, mutation requests/sec, entity cache hit ratio, KV operation latency
- **Health checks**: Component health status, NATS connectivity, KV bucket availability

## Development Workflow

### Before Making Changes
1. Read this README to understand GraphProcessor responsibilities and behaviors
2. Check integration points: process.> messages, graph.events mutations, and KV watch consumers
3. Identify which behaviors need testing: entity processing, storage, mutation handling
4. Update tests BEFORE changing code (TDD approach)

### After Making Changes
1. Verify all existing tests still pass (especially entity processing and mutation handling)
2. Add tests for new behaviors (new entity types, processing logic, error handling)
3. Update this README if responsibilities or integration points changed
4. Check integration flows still work: 
   - RoboticsProcessor → process.> → GraphProcessor → ENTITY_STATES
   - RuleProcessor → graph.events → GraphProcessor → ENTITY_STATES
5. Update configuration examples if new config options added

## Related Documentation
- [CANONICAL Message Flow](/docs/architecture/CANONICAL_MESSAGE_FLOW.md) - Single source of truth for architecture
- [BaseMessage and Graphable Interface Specification](/pkg/message/README.md)
- [Entity State and Graph Types](/pkg/types/graph/README.md) 
- [NATS KV and JetStream Integration](/pkg/nats/README.md)
- [Component Lifecycle Interface](/pkg/component/README.md)
- [Graph Events Design](/docs/architecture/GRAPH_EVENTS_DESIGN.md) - Mutation request patterns