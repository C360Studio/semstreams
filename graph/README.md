# Graph Package

Core graph types and interfaces for the SemStreams entity graph system.

## Purpose

This package defines the fundamental data structures and contracts for representing entities and their
relationships in the graph. It contains storage types and interfaces but no runtime processing logic.

## Key Types

### EntityState

Complete local graph state for a single entity, stored in NATS KV as the canonical representation:

```go
type EntityState struct {
    ID          string          // 6-part federated ID: org.platform.system.domain.type.instance
    Triples     []message.Triple // All semantic facts about this entity
    StorageRef  *message.StorageReference // Optional reference to full message storage
    MessageType message.Type     // Provenance: which message type created/updated this entity
    Version     uint64           // Optimistic concurrency control
    UpdatedAt   time.Time        // Last modification timestamp
}
```

**Triples are the single source of truth** for all semantic properties. Use helper methods to access:

```go
// Get first matching triple
triple := state.GetTriple("geo.location.latitude")

// Get property value directly
lat, found := state.GetPropertyValue("geo.location.latitude")
```

### Entity Identification

All entity IDs use the 6-part federated format defined in `message.EntityID` (canonical order, ADR-102:
`org.platform` = the minting deployment authority, `system` = the source, `domain.type` = a delegated taxonomy):

```
org.platform.system.domain.type.instance
```

Example: `c360.telemetry.mavlink.robotics.drone.42`

To parse entity IDs and extract type information:

```go
eid, err := message.ParseEntityID(state.ID)
if err != nil {
    return fmt.Errorf("invalid entity ID: %w", err)
}

entityType := eid.Type      // "drone"
system := eid.System        // "mavlink"
instance := eid.Instance    // "42"
```

## Graphable Interface

The `Graphable` interface enables domain payloads to self-declare their entities and relationships:

```go
type Graphable interface {
    EntityID() string                  // Returns 6-part federated ID
    Triples() []message.Triple        // Returns all facts about this entity
}
```

**Why it's here**: Components implementing graph integration naturally import `graph/` to find contracts.
This interface defines how payloads transform into graph-compatible data.

### Example Implementation

```go
type PositionPayload struct {
    SystemID  uint8   `json:"system_id"`
    Latitude  float64 `json:"latitude"`
    Longitude float64 `json:"longitude"`
    Altitude  float32 `json:"altitude"`
}

func (p *PositionPayload) EntityID() string {
    return fmt.Sprintf("acme.telemetry.robotics.mavlink.drone.%d", p.SystemID)
}

func (p *PositionPayload) Triples() []message.Triple {
    entityID := p.EntityID()
    return []message.Triple{
        {Subject: entityID, Predicate: "geo.location.latitude", Object: p.Latitude},
        {Subject: entityID, Predicate: "geo.location.longitude", Object: p.Longitude},
        {Subject: entityID, Predicate: "geo.location.altitude", Object: p.Altitude},
    }
}
```

## Design Principles

### One-Way Transformation

`Graphable` → `EntityState` is a one-way transformation at message ingestion time:

1. Domain payload implements `Graphable` interface
2. `Graphable.Triples()` generates triples at runtime from payload fields
3. `EntityState` persists those triples in NATS KV storage
4. No reverse transformation (storage → payload) is supported or needed

### Triple-Based Storage

Properties, relationships, and domain-specific data are all stored as RDF-like triples:

```go
Triple{
    Subject:   "c360.telemetry.robotics.mavlink.drone.42",
    Predicate: "geo.location.latitude",
    Object:    34.052235,
}
```

This provides maximum flexibility while maintaining semantic clarity.

## Relationship to processor/graph/

This package contains **types and interfaces only**. For graph processing, indexing, querying, and
mutations, see the `processor/graph/` package hierarchy:

- `processor/graph/` - GraphProcessor runtime and mutations
- `processor/graph/querymanager/` - Query execution and caching
- `processor/graph/indexmanager/` - Index operations for semantic search
- `processor/graph/clustering/` - Community detection and graph clustering
- `processor/graph/embedding/` - Vector embeddings for semantic similarity

## Package Ownership

Per ADR-PACKAGE-RESPONSIBILITIES-CONSOLIDATION:

- **graph/** owns: Graph contracts, storage types, and interfaces
- **message/** owns: Transport primitives (EntityID, Triple, FederationMeta)
- **processor/graph/** owns: All runtime graph processing logic

Federation information is embedded in the EntityID 6-part format itself - no separate federation
layer exists in this package.
