# The Graphable Interface

The Graphable interface is how your domain messages declare themselves as graph entities. Instead of SemStreams guessing what entities exist, your payload explicitly states its identity and facts.

## Interface Definition

```go
// graph/graphable.go
type Graphable interface {
    EntityID() string          // 6-part federated identifier
    Triples() []message.Triple // Facts about this entity
}
```

Two methods. That's it.

## EntityID: Identity

Returns a deterministic 6-part identifier:

```text
org.platform.domain.system.type.instance
```

**Requirements:**

- Exactly six dot-separated positions: `org.platform.domain.system.type.instance`
- At most 256 serialized bytes, including the five separators
- Each position matches `[A-Za-z0-9][A-Za-z0-9_-]*`
- Deterministic: the same input always produces the same ID
- Hierarchical: literal prefixes such as `acme.ops` enable scoped queries; query prefixes never contain wildcards
- Unique: no collisions within your system

Use `pkg/types.ValidateEntityID` or `pkg/types.ParseEntityID` at an authoring boundary. SemStreams never trims,
normalizes, hashes, or repairs an invalid ID.

**Example:**

```go
func (s *SensorReading) EntityID() string {
    return fmt.Sprintf("%s.%s.environmental.sensor.%s.%s",
        s.OrgID,      // "acme"
        s.Platform,   // "logistics"
        s.SensorType, // "temperature"
        s.DeviceID,   // "sensor-042"
    )
}
// Result: "acme.logistics.environmental.sensor.temperature.sensor-042"
```

## Triples: Facts

Returns all facts about this entity as Subject-Predicate-Object triples.

```go
type Triple struct {
    Subject    string    // Entity ID (usually this entity)
    Predicate  string    // domain.category.property
    Object     any       // Value or entity reference
    Source     string    // Origin system
    Timestamp  time.Time // When fact was observed
    Confidence float64   // 0.0-1.0 certainty
}
```

**Example:**

```go
func (s *SensorReading) Triples() []message.Triple {
    id := s.EntityID()
    return []message.Triple{
        // Measurement with unit-specific predicate
        {
            Subject:   id,
            Predicate: fmt.Sprintf("sensor.measurement.%s", s.Unit),
            Object:    s.Value,
            Source:    "iot_sensor",
            Timestamp: s.ObservedAt,
        },
        // Classification
        {
            Subject:   id,
            Predicate: "sensor.classification.type",
            Object:    s.SensorType,
            Source:    "iot_sensor",
            Timestamp: s.ObservedAt,
        },
        // Entity reference (creates an edge)
        {
            Subject:   id,
            Predicate: "geo.location.zone",
            Object:    s.ZoneEntityID, // Another entity ID
            Datatype:  message.EntityReferenceDatatype,
            Source:    "iot_sensor",
            Timestamp: s.ObservedAt,
        },
    }
}
```

## Properties vs Relationships

The Object field determines whether a triple is a property or a relationship:

```go
// Property: Object is a value
{Predicate: "sensor.measurement.celsius", Object: 23.5}

// Relationship: @id marks the object as a canonical entity reference
{
    Predicate: "geo.location.zone",
    Object: "acme.logistics.facility.zone.area.warehouse-7",
    Datatype: message.EntityReferenceDatatype,
}
```

A string that merely resembles an entity ID remains a scalar property unless its datatype is explicitly `@id`.
An `@id` object must be a canonical entity-ID string; malformed or non-string references reject the whole candidate.

Relationships create edges in the graph. These edges are:

- Indexed in INCOMING_INDEX and OUTGOING_INDEX
- Traversed by community detection
- Queryable via relationship queries

## Predicate Design

Predicates follow `domain.category.property` format:

```go
// Good: Consistent, specific
"sensor.measurement.celsius"
"sensor.measurement.humidity"
"geo.location.zone"
"fleet.membership.current"

// Bad: Inconsistent, generic
"temperature"
"value"
"zone"
"fleet"
```

Benefits of dotted notation:

- NATS wildcard queries: `sensor.measurement.*`
- Self-documenting: The predicate explains itself
- Collision-free: Different domains don't overlap

## Complete Implementation Example

```go
package robotics

import (
    "fmt"
    "time"
    "github.com/c360studio/semstreams/message"
)

// DroneTelemetry implements Graphable for drone position data.
type DroneTelemetry struct {
    DroneID   string
    Battery   int
    Altitude  float64
    Latitude  float64
    Longitude float64
    FleetID   string
    OrgID     string
    Platform  string
    Timestamp time.Time
}

func (d *DroneTelemetry) EntityID() string {
    return fmt.Sprintf("%s.%s.robotics.gcs.drone.%s",
        d.OrgID,
        d.Platform,
        d.DroneID,
    )
}

func (d *DroneTelemetry) Triples() []message.Triple {
    id := d.EntityID()
    ts := d.Timestamp

    triples := []message.Triple{
        // Properties
        {Subject: id, Predicate: "drone.telemetry.battery", Object: d.Battery, Timestamp: ts},
        {Subject: id, Predicate: "drone.telemetry.altitude", Object: d.Altitude, Timestamp: ts},
        {Subject: id, Predicate: "geo.location.latitude", Object: d.Latitude, Timestamp: ts},
        {Subject: id, Predicate: "geo.location.longitude", Object: d.Longitude, Timestamp: ts},
    }

    // Relationship (only if fleet is assigned)
    if d.FleetID != "" {
        fleetEntityID := fmt.Sprintf("%s.%s.operations.fleet.cargo.%s",
            d.OrgID, d.Platform, d.FleetID)
        triples = append(triples, message.Triple{
            Subject:   id,
            Predicate: "fleet.membership.current",
            Object:    fleetEntityID,
            Datatype:  message.EntityReferenceDatatype,
            Timestamp: ts,
        })
    }

    return triples
}
```

## What Graphable Does NOT Include

The core Graphable interface intentionally excludes:

- **TextContent()**: For embedding generation (handled by processors)
- **Embedding()**: Pre-computed vectors (handled by embedding service)
- **Validate()**: Payload validation (separate interface)
- **Schema()**: Message type identification (separate interface)

This keeps the interface minimal. Additional capabilities are added via separate interfaces when needed.

Graphable also does not grant mutation authority. A payload says "these are the triples I emit"; components that later
change current graph state use the typed mutation port's strict create, complete-set reconcile, set-valued append, or
revision-fenced delete operation. See [ADR-091](../adr/091-graph-mutation-authority-without-semantic-ownership.md).

## Interface Composition

Graphable is the foundation of a composable interface hierarchy:

**Semantic Interfaces** (what the entity IS):

| Interface | Adds | Use When |
|-----------|------|----------|
| `Graphable` | EntityID + Triples | All semantic entities |
| `Storable` | StorageRef | Entity has external storage reference |
| `ContentStorable` | ContentFields + RawContent | Large content stored in ObjectStore |

**Behavioral Interfaces** (optional capabilities):

| Interface | Provides | Discovered By |
|-----------|----------|---------------|
| `Locatable` | Location coordinates | Spatial indexing |
| `Timeable` | Timestamp | Temporal indexing |
| `Observable` | Observation semantics | Sensor processing |
| `Correlatable` | Correlation ID | Distributed tracing |
| `RuleReadable` | Declared rule-readable fields | Rule conditions and `$message.*` substitution |

Implement only what you need. Services discover capabilities at runtime via type assertions.

`RuleReadable` is the one whose ABSENCE is load-bearing: a payload that does not
implement it cannot be read by the rule engine at all, so every `$message.*`
condition against it is false forever. The engine reports the rule/payload-type
pairing once rather than failing silently. Expose structural facts; withhold
LLM-authored and user content (ADR-036). See `message.RuleReadable` for the full
contract.

## Common Patterns

### Multiple Entity Types

One payload can produce multiple entities:

```go
func (p *FleetUpdate) ProcessEntities() []Graphable {
    entities := []Graphable{p.Fleet}
    for _, drone := range p.Drones {
        entities = append(entities, drone)
    }
    return entities
}
```

### Conditional Triples

Add triples only when data is present:

```go
func (s *Sensor) Triples() []message.Triple {
    triples := []message.Triple{...}

    if s.SerialNumber != "" {
        triples = append(triples, message.Triple{
            Predicate: "sensor.identifier.serial",
            Object:    s.SerialNumber,
        })
    }

    return triples
}
```

### Entity References

Always generate full entity IDs for references:

```go
// Good: Full entity ID
fleetID := fmt.Sprintf("%s.%s.ops.fleet.cargo.%s", orgID, platform, fleetName)
{Predicate: "fleet.membership", Object: fleetID}

// Bad: Partial or ambiguous
{Predicate: "fleet.membership", Object: fleetName}
```

## Next Steps

- [Vocabulary](04-vocabulary.md) - Designing your predicates
- [First Processor](05-first-processor.md) - Building a complete processor
- [Index Reference](../advanced/05-index-reference.md) - How triples become indexed
- [Testing Guide](../contributing/01-testing.md#testing-graphable-implementations) - Testing your Graphable implementations
