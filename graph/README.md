# Graph Federation README

**Last Updated**: 2024-08-29  
**Maintainer**: SemStreams Core Team

## Purpose & Scope

**What this component does**: Provides federation utilities for creating globally unique entity identifiers and managing entity metadata across distributed SemStreams deployments.

**Key responsibilities**:
- Generate structured global IDs for entities with platform scoping
- Parse global IDs to extract platform, region, and local components
- Create and manage FederatedEntity metadata
- Enrich EntityState with federation properties
- Support cross-platform entity correlation and RDF export

**NOT responsible for**: Network communication between platforms, entity synchronization, or conflict resolution algorithms

## Design Decisions

### Architectural Choices
- **Colon-delimited global IDs**: Uses "platform:region:local" format
  - **Rationale**: Human-readable structure with clear component separation
  - **Trade-offs**: Parsing complexity vs readability and debuggability
  - **Alternatives considered**: UUID-based IDs (rejected for human readability)

- **Optional region support**: Global IDs work with or without region component
  - **Rationale**: Supports both simple and complex deployment topologies
  - **Trade-offs**: Format variability vs deployment flexibility
  - **Alternatives considered**: Required region (rejected for simple deployments)

- **EntityState enrichment**: Adds federation metadata as properties
  - **Rationale**: Preserves federation info without changing core EntityState structure
  - **Trade-offs**: Properties map usage vs dedicated federation fields
  - **Alternatives considered**: Separate federation wrapper (rejected for storage efficiency)

### API Contracts
- **BuildGlobalID format**: Returns structured global identifier or empty string
  - **Example**: platform "us-west", region "gulf", local "drone_1" → "us-west:gulf:drone_1"
  - **Enforcement**: Requires non-empty platform.ID and localID
  - **Exceptions**: Returns empty string for missing required components

- **ParseGlobalID components**: Extracts platform, region, localID from global ID
  - **Example**: "us-west:gulf:drone_1" → ("us-west", "gulf", "drone_1", true)
  - **Enforcement**: Validates component count and non-empty required parts
  - **Exceptions**: Returns (_, _, localID, false) for non-global IDs

### Anti-Patterns to Avoid
- **Manual global ID construction**: Always use BuildGlobalID() function
- **Assuming region presence**: Check ParseGlobalID return value for region handling
- **Ignoring empty returns**: Validate BuildGlobalID() returns before use

## Architecture Context

### Integration Points
- **Consumes from**: Platform configuration, entity local IDs, message metadata
- **Provides to**: GraphProcessor for federated entity storage, vocabulary for IRIs
- **External dependencies**: Platform configuration system, message federation metadata

### Data Flow
```
LocalID + Platform → BuildGlobalID() → Global ID
Message → BuildFederatedEntity() → FederatedEntity → EnrichEntityState() → Enhanced EntityState
```

### Configuration
Depends on platform configuration structure:
```go
type PlatformConfig struct {
    ID     string  // Required for federation
    Region string  // Optional regional scoping
}
```

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work
1. **Global ID generation**: Create structured global identifiers
   - **Input**: PlatformConfig{ID: "us-west", Region: "gulf"}, localID: "drone_1"
   - **Expected**: "us-west:gulf:drone_1"
   - **Verification**: Correct format with all components present

2. **Global ID parsing**: Extract components from global identifiers
   - **Input**: "us-west:gulf:drone_1"
   - **Expected**: ("us-west", "gulf", "drone_1", true)
   - **Verification**: All components extracted correctly, success flag true

3. **Federation enrichment**: Add federation metadata to EntityState
   - **Input**: EntityState and FederatedEntity
   - **Expected**: Properties map contains federation keys (global_id, platform_id, etc.)
   - **Verification**: All federation properties present with correct values

### Error Conditions - What Should Fail Gracefully
1. **Missing platform data**: Empty platform ID or local ID
   - **Trigger**: BuildGlobalID(PlatformConfig{}, "id") or BuildGlobalID(platform, "")
   - **Expected**: Returns empty string
   - **Recovery**: Caller should validate non-empty return before use

2. **Malformed global IDs**: Invalid component count or format
   - **Trigger**: ParseGlobalID("invalid"), ParseGlobalID("")
   - **Expected**: Returns (_, _, input, false) with ok=false
   - **Recovery**: Caller should check ok return value and handle accordingly

### Edge Cases - Boundary Conditions
- **No region**: Global IDs without region component ("platform:local")
- **Empty region**: Platform config with empty Region field
- **Unicode characters**: Non-ASCII characters in platform or local IDs

## Common Patterns

### Standard Implementation Patterns
- **Federated entity creation**: Generate federation metadata from messages
  - **When to use**: Processing messages from federated sources
  - **Implementation**: `fed := BuildFederatedEntity(localID, msg)`
  - **Pitfalls**: Not checking for federation metadata presence in message

- **Entity state enrichment**: Add federation properties to graph storage
  - **When to use**: Storing entities that may need cross-platform correlation
  - **Implementation**: `EnrichEntityState(entityState, federatedEntity)`
  - **Pitfalls**: Overwriting existing properties without checking

### Optional Feature Patterns
- **IRI generation**: Create semantic web identifiers for federated entities
- **Platform detection**: Check if entity has federation metadata
- **Cross-platform correlation**: Use global IDs for entity matching

### Integration Patterns
- **Message processing**: Extract federation metadata during entity creation
- **Graph storage**: Enrich entities with federation properties before storage
- **RDF export**: Generate IRIs for federated entities using vocabulary module

## Usage Patterns

### Typical Usage (How Other Code Uses This)
```go
// Create global ID for entity
platform := config.PlatformConfig{
    ID:     "us-west-prod",
    Region: "gulf_mexico",
}
globalID := graph.BuildGlobalID(platform, "drone_001")
// Result: "us-west-prod:gulf_mexico:drone_001"

// Parse global ID components
platformID, region, localID, ok := graph.ParseGlobalID(globalID)
if ok {
    // Use components: "us-west-prod", "gulf_mexico", "drone_001"
}

// Build federated entity from message
federatedEntity := graph.BuildFederatedEntity("drone_001", message)

// Enrich entity state with federation metadata
var entityState gtypes.EntityState
graph.EnrichEntityState(&entityState, federatedEntity)

// Check if entity is federated
if graph.IsFederatedEntity(&entityState) {
    fedInfo := graph.GetFederationInfo(&entityState)
    // Use federation metadata
}

// Generate IRI for federated entity
iri := federatedEntity.GetEntityIRI("robotics.drone")
// Result: "https://semstreams.c360.io/entities/us-west-prod/gulf_mexico/robotics/drone/drone_001"
```

### Common Integration Patterns
- **Entity storage**: Enrich EntityState with federation data before NATS KV storage
- **Cross-platform queries**: Use global IDs for entity correlation across deployments
- **RDF export**: Generate IRIs for entities with federation metadata

## Testing Strategy

### Test Categories
1. **Unit Tests**: Global ID generation and parsing functions
2. **Integration Tests**: Federation metadata handling with real EntityState
3. **Format Tests**: Global ID format compliance and edge cases

### Test Quality Standards
- ✅ Tests MUST verify actual global ID format (not just non-empty)
- ✅ Tests MUST cover parsing edge cases (missing components, malformed input)
- ✅ Tests MUST validate enrichment behavior with existing properties
- ❌ NO tests that don't verify global ID structure
- ❌ NO tests that ignore parsing return values

### Mock vs Real Dependencies
- **Use real types for**: All federation functions (no external dependencies)
- **Use mocks for**: Message federation metadata in unit tests
- **Testcontainers for**: Not applicable (pure data transformation)

## Implementation Notes

### Thread Safety
- **Concurrency model**: Pure functions and value types, thread-safe by design
- **Shared state**: No shared state, all operations on input parameters
- **Critical sections**: None

### Performance Considerations
- **Expected throughput**: Federation operations on every federated message
- **Memory usage**: String allocations for global ID generation
- **Bottlenecks**: String splitting and parsing operations

### Error Handling Philosophy
- **Error propagation**: Uses boolean returns and empty strings for invalid states
- **Retry strategy**: Not applicable (deterministic operations)
- **Circuit breaking**: Not applicable

## Troubleshooting

### Investigation Workflow
1. **First steps** when debugging federation issues:
   - **Check platform config**: Verify platform ID and region are properly set
   - **Validate global IDs**: Use ParseGlobalID() to verify format correctness
   - **Inspect federation properties**: Check EntityState properties for federation keys
   - **Verify message metadata**: Ensure messages have proper federation metadata

2. **Common debugging commands**:
   ```bash
   # Find federation usage
   rg "BuildGlobalID|ParseGlobalID" --type go -B 2 -A 3
   
   # Check platform configurations
   rg "PlatformConfig" --type go -B 2 -A 5
   
   # Verify federation enrichment
   rg "EnrichEntityState|IsFederatedEntity" --type go
   ```

### Decision Trees for Common Issues

#### When global IDs are empty:
```
BuildGlobalID returns empty string
├── Platform.ID empty? → Set platform identifier in configuration
├── LocalID empty? → Provide non-empty entity local identifier
└── Function not called? → Check federation flow integration
```

#### When parsing fails:
```
ParseGlobalID returns ok=false
├── Wrong format? → Check for colon-separated components
├── Missing components? → Verify all required parts present
├── Empty parts? → Check for empty platform or local ID
└── Not a global ID? → Input may be local ID only (expected case)
```

### Common Issues
1. **Platform configuration missing**: Global IDs not generated for entities
   - **Cause**: Platform config not initialized or missing ID field
   - **Investigation**: Check PlatformConfig initialization and population
   - **Solution**: Ensure platform config has non-empty ID before federation calls
   - **Prevention**: Validate platform config during system startup

2. **Federation metadata lost**: Entities stored without federation properties
   - **Cause**: EnrichEntityState() not called or federation entity nil
   - **Investigation**: Check federation metadata extraction from messages
   - **Solution**: Ensure BuildFederatedEntity() called before enrichment
   - **Prevention**: Include federation enrichment in standard entity processing flow

### Debug Information
- **Logs to check**: Federation metadata extraction and global ID generation
- **Metrics to monitor**: Federation success/failure rates
- **Health checks**: Platform configuration validity
- **Config validation**: Platform ID and region configuration
- **Integration verification**: Federation properties in stored EntityState

## Migration to Dotted Notation (COMPLETED)

### **Migration from colon notation to dotted notation - COMPLETE**
- **GetEntityIRI() comments**: ✅ Updated to use "robotics.drone" format
- **Test cases**: ✅ All tests updated to dotted notation
- **Documentation**: ✅ Examples updated to show dotted notation
- **Vocabulary module**: ✅ IRI functions now accept dotted notation

### Completed Changes
- ✅ Updated code comments and examples to use dotted notation
- ✅ Updated GetEntityIRI() documentation with correct examples
- ✅ Updated all test cases in federation_iri_test.go
- ✅ Vocabulary module IRI functions refactored for dotted notation

### Migration Impact
- **Low**: No breaking changes to function signatures
- **Low**: Vocabulary module handles input format changes
- **Low**: Documentation and examples need updating only

## Related Documentation
- [SEMSTREAMS_NATS_ARCHITECTURE.md](../../docs/architecture/SEMSTREAMS_NATS_ARCHITECTURE.md) - Federation architecture
- [Vocabulary](../vocabulary/README.md) - IRI generation for federated entities  
- [Graph types](../types/graph/README.md) - EntityState structure and properties
- [Message types](../message/README.md) - Message federation metadata