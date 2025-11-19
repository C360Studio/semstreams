# Graph Types README

**Last Updated**: 2024-08-29  
**Maintainer**: SemStreams Core Team

## Purpose & Scope

**What this component does**: Provides core type definitions for entity state storage in the graph system, including entity properties, relationships, and operational status.

**Key responsibilities**:
- Define EntityState structure for complete entity graph representation
- Define NodeProperties for entity identification and properties
- Define Edge structure for directed relationships between entities
- Provide EntityStatus enumeration for operational monitoring
- Support graph storage operations and relationship management

**NOT responsible for**: Graph storage implementation, NATS operations, entity processing logic

## Design Decisions

### Architectural Choices
- **Outgoing edges only**: EntityState stores only outgoing relationships
  - **Rationale**: Avoids bi-directional storage complexity and inconsistency issues
  - **Trade-offs**: Requires reverse index for incoming edge queries vs simpler storage
  - **Alternatives considered**: Bi-directional storage (rejected due to consistency issues)

- **Explicit versioning**: EntityState includes version field for conflict resolution
  - **Rationale**: Enables optimistic locking and conflict detection in concurrent updates
  - **Trade-offs**: Additional storage overhead vs data consistency guarantees
  - **Alternatives considered**: Timestamp-based versioning (rejected for precision issues)

- **ObjectRef pattern**: Links to complete message data in ObjectStore
  - **Rationale**: Separates frequently-queried properties from complete message context
  - **Trade-offs**: Additional lookup for full context vs optimized query performance
  - **Alternatives considered**: Embedding full message data (rejected for storage efficiency)

### API Contracts
- **Edge replacement**: AddEdge() replaces existing edges of same type to same entity
  - **Example**: Adding "NEAR" relationship twice updates distance, doesn't duplicate
  - **Enforcement**: AddEdge() method checks existing edges before adding
  - **Exceptions**: Different edge types to same entity are allowed (NEAR + POWERED_BY)

- **Expiration handling**: Edges with ExpiresAt are automatically filtered
  - **Example**: NEAR relationships expire after 5 minutes if not refreshed
  - **Enforcement**: RemoveExpiredEdges() method called during processing
  - **Exceptions**: Permanent relationships have nil ExpiresAt

### Anti-Patterns to Avoid
- **Storing incoming edges**: Only store outgoing edges, use reverse index for incoming
- **Manual version management**: Always increment version on EntityState updates
- **Property map pollution**: Keep Properties focused on query-essential data only

## Architecture Context

### Integration Points
- **Consumes from**: EntityExtractor via message processing pipeline
- **Provides to**: GraphProcessor for NATS KV storage operations
- **External dependencies**: ObjectStore for complete message references

### Data Flow
```
Message → EntityExtractor → EntityState → GraphProcessor → NATS KV Storage
                                       ↘ Edge → Relationship indices
```

### Configuration
No direct configuration - types are used by GraphProcessor with NATS KV configuration.

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work
1. **EntityState edge management**: Add, update, and remove relationships
   - **Input**: EntityState with edges, call AddEdge() with new relationship
   - **Expected**: Edge added or existing edge of same type updated
   - **Verification**: Edges slice contains expected edge with correct properties

2. **Edge expiration cleanup**: Remove expired relationships automatically
   - **Input**: EntityState with expired edges (ExpiresAt < now)
   - **Expected**: RemoveExpiredEdges() removes expired, keeps current edges
   - **Verification**: Only non-expired edges remain in Edges slice

3. **Property updates**: Merge new properties with existing
   - **Input**: NodeProperties with existing data, UpdateProperties() with new data
   - **Expected**: New properties added, existing properties updated, others preserved
   - **Verification**: Properties map contains merged data

### Error Conditions - What Should Fail Gracefully
1. **Invalid EntityStatus**: Non-standard status values
   - **Trigger**: EntityStatus("invalid_status")
   - **Expected**: IsValid() returns false, methods handle gracefully
   - **Recovery**: Default to StatusUnknown for invalid values

2. **Nil property maps**: Operations on uninitialized Properties
   - **Trigger**: UpdateProperties() on NodeProperties with nil Properties map
   - **Expected**: Map initialized automatically, properties added successfully
   - **Recovery**: Lazy initialization in UpdateProperties()

### Edge Cases - Boundary Conditions
- **Empty edges slice**: Edge operations on EntityState with no existing edges
- **Duplicate edge types**: Multiple edges of same type to same entity
- **Simultaneous expiration**: Multiple edges expiring at exact same time

## Common Patterns

### Standard Implementation Patterns
- **Entity state creation**: Initialize with version 1 and current timestamp
  - **When to use**: Creating new EntityState from message extraction
  - **Implementation**: `EntityState{Version: 1, UpdatedAt: time.Now(), ...}`
  - **Pitfalls**: Forgetting to set Version or UpdatedAt fields

- **Edge lifecycle management**: Use expiration for temporary relationships
  - **When to use**: Proximity relationships, temporary operational states
  - **Implementation**: `Edge{ExpiresAt: &time.Time{}, ...}` for temporary edges
  - **Pitfalls**: Not calling RemoveExpiredEdges() regularly

### Optional Feature Patterns
- **Position tracking**: Use NodeProperties.Position for spatial relationships
- **Status monitoring**: Leverage EntityStatus for operational awareness
- **Property optimization**: Keep Properties map focused on frequently-queried data

### Integration Patterns
- **Graph storage**: GraphProcessor uses these types for NATS KV operations
- **Relationship indexing**: Edge data used to build reverse relationship indices
- **Context retrieval**: ObjectRef used to fetch complete message context

## Usage Patterns

### Typical Usage (How Other Code Uses This)
```go
// Create entity state from extraction
state := &gtypes.EntityState{
    Node: gtypes.NodeProperties{
        ID:         "drone_001",
        Type:       "robotics:Drone",  // Will be refactored to structured type
        Properties: map[string]any{
            "battery_level": 85.0,
            "armed": true,
        },
        Status:   gtypes.StatusActive,
        Position: &gtypes.Position{
            Latitude:  37.7749,
            Longitude: -122.4194,
            Altitude:  100.0,
        },
    },
    ObjectRef: "robotics.battery.v1:20240315-103045:drone_001",
    Version:   1,
    UpdatedAt: time.Now(),
}

// Add relationship
powerEdge := gtypes.Edge{
    ToEntityID: "battery_001",
    EdgeType:   "POWERED_BY",
    Weight:     1.0,
    Confidence: 0.95,
    CreatedAt:  time.Now(),
}
state.AddEdge(powerEdge)

// Update properties
state.Node.UpdateProperties(map[string]any{
    "last_heartbeat": time.Now(),
})

// Clean up expired edges
state.RemoveExpiredEdges()
```

### Common Integration Patterns
- **Storage operations**: GraphProcessor serializes EntityState to NATS KV
- **Relationship queries**: Edge data used to build relationship graphs
- **Status monitoring**: EntityStatus used for health checks and alerting

## Testing Strategy

### Test Categories
1. **Unit Tests**: Individual type methods (AddEdge, RemoveExpiredEdges, etc.)
2. **State Management Tests**: EntityState lifecycle operations
3. **Serialization Tests**: JSON marshaling/unmarshaling of all types

### Test Quality Standards
- ✅ Tests MUST verify actual state changes (not just method calls)
- ✅ Tests MUST cover edge expiration and cleanup behavior
- ✅ Tests MUST validate EntityStatus enumeration values
- ❌ NO tests that don't verify state mutations
- ❌ NO tests that ignore time-based behavior (expiration)

### Mock vs Real Dependencies
- **Use real types for**: All graph type testing (no external dependencies)
- **Use mocks for**: Not applicable (pure value types)
- **Testcontainers for**: Not applicable (no external services)

## Implementation Notes

### Thread Safety
- **Concurrency model**: Value types, no inherent thread safety required
- **Shared state**: EntityState mutations not thread-safe, callers must synchronize
- **Critical sections**: Edge slice operations in concurrent environments

### Performance Considerations
- **Expected throughput**: High-frequency entity updates in graph processing
- **Memory usage**: Properties map and Edges slice can grow with entity complexity
- **Bottlenecks**: Edge removal with linear search through Edges slice

### Error Handling Philosophy
- **Error propagation**: Methods use boolean returns for success/failure
- **Retry strategy**: Not applicable (pure data types)
- **Circuit breaking**: Not applicable

## Troubleshooting

### Investigation Workflow
1. **First steps** when debugging entity state issues:
   - **Check version consistency**: Verify version increments on updates
   - **Validate edge state**: Use RemoveExpiredEdges() to clean stale relationships
   - **Verify property updates**: Check Properties map for expected key-value pairs
   - **Inspect ObjectRef**: Ensure reference points to valid ObjectStore entry

2. **Common debugging commands**:
   ```bash
   # Find EntityState usage patterns
   rg "EntityState" --type go -A 5
   
   # Check edge management
   rg "AddEdge|RemoveEdge" --type go
   
   # Verify status usage
   rg "Status\w+" pkg/types/graph/ --type go
   ```

### Decision Trees for Common Issues

#### When entity updates fail:
```
Entity state updates not persisting
├── Version conflict? → Check version increment on updates
├── Edge not adding? → Verify AddEdge() logic for duplicates
├── Properties not updating? → Check UpdateProperties() merge logic
└── Status not changing? → Verify EntityStatus.IsValid()
```

#### When relationships missing:
```
Expected relationships not found
├── Edge expired? → Call RemoveExpiredEdges(), check ExpiresAt
├── Wrong direction? → Remember only outgoing edges stored
├── Duplicate type? → AddEdge() replaces existing edges of same type
└── Index out of sync? → Rebuild relationship indices
```

### Common Issues
1. **Edge duplication**: Multiple edges of same type to same entity
   - **Cause**: Not using AddEdge() method, manually appending to Edges slice
   - **Investigation**: Check Edges slice for duplicate entries
   - **Solution**: Always use AddEdge() which handles replacement logic
   - **Prevention**: Use AddEdge() method exclusively for edge management

2. **Stale relationships**: Expired edges not cleaned up
   - **Cause**: RemoveExpiredEdges() not called during processing
   - **Investigation**: Check ExpiresAt timestamps on edges
   - **Solution**: Call RemoveExpiredEdges() regularly in processing pipeline
   - **Prevention**: Include expiration cleanup in standard processing flow

### Debug Information
- **Logs to check**: GraphProcessor logs for entity state operations
- **Metrics to monitor**: Entity state storage and retrieval rates
- **Health checks**: EntityStatus distribution across entities
- **Config validation**: Properties map size and content patterns
- **Integration verification**: ObjectRef validity and accessibility

## Alpha Week 1 Refactor Impact

### **CRITICAL: Type field uses colon notation - will be refactored**
- **NodeProperties.Type**: Currently string with "robotics:Drone" format
- **Breaking change**: Will be replaced with structured EntityType using dotted keys
- **Migration required**: All code referencing Type field will need updates

### Refactor Scope
- Replace `Type string` field with structured type implementing `Key()` method
- Update JSON serialization to use dotted notation output
- Maintain all other fields unchanged (ID, Properties, Status, Position)
- Update documentation examples to show new structured type usage

### Migration Impact
- **High**: All graph storage and retrieval operations will need type field updates
- **Medium**: JSON serialization format will change for Type field
- **Low**: Edge and Position types unchanged, no migration needed

## Related Documentation
- [SEMSTREAMS_NATS_ARCHITECTURE.md](../../../docs/architecture/SEMSTREAMS_NATS_ARCHITECTURE.md) - Graph storage patterns
- [Message types](../../message/README.md) - Entity type definitions
- [GraphProcessor](../../processor/graph/) - Graph processing implementation