# Vocabulary: Designing Your Predicates

Predicates are how you name facts in your graph. Consistent predicate naming determines whether your graph is queryable or a mess of unrelated data.

## Predicate Format

All predicates follow three-level dotted notation:

```text
domain.category.property
```

**Examples:**

- `sensor.measurement.celsius`
- `geo.location.latitude`
- `fleet.membership.current`
- `drone.telemetry.battery`

## Why Dotted Notation?

1. **NATS wildcard queries**: `sensor.measurement.*` finds all measurement predicates
2. **Self-documenting**: `drone.telemetry.battery` explains itself
3. **No collisions**: `sensor.temperature` vs `hvac.temperature` are distinct
4. **Consistent**: Matches entity ID patterns

## Naming Conventions

| Level | Rules | Examples |
|-------|-------|----------|
| domain | lowercase, business area | `sensor`, `geo`, `fleet`, `drone` |
| category | lowercase, groups related | `measurement`, `location`, `membership` |
| property | lowercase, specific name | `celsius`, `latitude`, `current` |

**Avoid:**

- Underscores: `sensor_temperature` (use `sensor.temperature`)
- camelCase: `sensorTemperature` (use `sensor.temperature`)
- Abbreviations: `sens.temp.c` (use `sensor.measurement.celsius`)

## Defining Your Vocabulary

Define predicates as package constants:

```go
package robotics

// Drone telemetry predicates
const (
    DroneBattery   = "drone.telemetry.battery"
    DroneAltitude  = "drone.telemetry.altitude"
    DroneStatus    = "drone.telemetry.status"
    DroneHeading   = "drone.telemetry.heading"
)

// Fleet relationship predicates
const (
    FleetMembership = "fleet.membership.current"
    FleetAssignment = "fleet.assignment.mission"
)

// Geospatial predicates
const (
    GeoLatitude  = "geo.location.latitude"
    GeoLongitude = "geo.location.longitude"
    GeoAltitude  = "geo.location.altitude"
    GeoZone      = "geo.location.zone"
)
```

## Registering Predicates (Optional)

For discoverability and standards compliance, register predicates with metadata:

```go
import "github.com/c360studio/semstreams/vocabulary"

func init() {
    vocabulary.Register(DroneBattery,
        vocabulary.WithDescription("Battery charge level percentage"),
        vocabulary.WithDataType("int"),
        vocabulary.WithUnits("percent"),
        vocabulary.WithRange("0-100"),
    )

    vocabulary.Register(GeoLatitude,
        vocabulary.WithDescription("Latitude in decimal degrees"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithRange("-90 to 90"),
        vocabulary.WithIRI(vocabulary.GeoLatitude), // Optional: RDF mapping
    )
}
```

Registration is optional but enables:

- API introspection of available predicates
- Data type validation
- RDF/Turtle export with standard IRIs via the [`export`](../../vocabulary/export/doc.go) package

## Standard Vocabularies

SemStreams supports mapping predicates to established vocabularies like Dublin Core, PROV-O, and Schema.org. This enables interoperability with existing semantic systems.

Standard vocabulary predicates should be defined in the `vocabulary/` package so all processors can import them. The package provides:
- **IRI constants** (`vocabulary.DcTitle` = `http://purl.org/dc/terms/title`)
- **Predicate constants** for dotted notation (e.g., `vocabulary.DCTermsTitle` = `dc.terms.title`)

**Using Standard Predicates**

```go
import "github.com/c360studio/semstreams/vocabulary"

// Use standard Dublin Core predicates
func (d *Document) Triples() []message.Triple {
    return []message.Triple{
        {Subject: d.EntityID(), Predicate: vocabulary.DCTermsTitle, Object: d.Title},
        {Subject: d.EntityID(), Predicate: vocabulary.DCTermsCreator, Object: d.Author},
    }
}
```

Internally, predicates use dotted notation (`dc.terms.title`). At API boundaries (RDF export), they translate to standard IRIs (`http://purl.org/dc/terms/title`).

See [vocabulary/standards.go](../../vocabulary/standards.go) for available IRI constants and [vocabulary/README.md](../../vocabulary/README.md) for the full vocabulary API.

## Unit-Specific Predicates

Include units in the predicate for clarity:

```go
// Good: Unit is explicit
"sensor.measurement.celsius"
"sensor.measurement.fahrenheit"
"sensor.measurement.percent"

// Bad: Unit is ambiguous
"sensor.temperature"  // Celsius? Fahrenheit? Kelvin?
"sensor.value"        // What value?
```

This allows mixed units in the same graph without confusion.

## Property vs Relationship Predicates

Property predicates have literal values:

```go
{Predicate: "drone.telemetry.battery", Object: 78}          // int
{Predicate: "sensor.measurement.celsius", Object: 23.5}     // float
{Predicate: "entity.status.name", Object: "active"}         // string
```

Relationship predicates have entity IDs as values:

```go
{Predicate: "fleet.membership.current", Object: "acme.ops.fleet.cargo.rescue"}
{Predicate: "geo.location.zone", Object: "acme.logistics.zone.facility.area.warehouse-7"}
```

The distinction matters because relationships create edges in the graph.

## What Each Feature Requires

Different SemStreams features have different requirements from your vocabulary and payload implementation:

| Feature | Requires | Interface |
|---------|----------|-----------|
| Entity storage | Any triples | `Graphable` |
| PREDICATE_INDEX | Any triples | `Graphable` |
| INCOMING/OUTGOING_INDEX | Relationship predicates (Object = entity ID) | `Graphable` |
| Community detection (LPA) | Relationship predicates (edges to traverse) | `Graphable` |
| Structural indexing | Relationship predicates (graph structure) | `Graphable` |
| Semantic search (Tier 1+) | Text content for embeddings | `ContentStorable` |

**Key distinction:**

- **`Graphable`**: Provides `EntityID()` and `Triples()`. Required for all graph operations.
- **`ContentStorable`**: Extends `Graphable` with `ContentFields()` and `RawContent()`. Required only for semantic search—tells embedding workers where to find text content.

If you only need graph structure (relationships, communities, or in-memory anomaly analysis), `Graphable` is
sufficient. Implement `ContentStorable` only when you need semantic similarity based on text content.

## Common Predicate Domains

| Domain | Use For | Examples |
|--------|---------|----------|
| `sensor` | Measurements, readings | `sensor.measurement.*`, `sensor.classification.*` |
| `geo` | Spatial data | `geo.location.*`, `geo.boundary.*` |
| `time` | Temporal data | `time.observation.*`, `time.lifecycle.*` |
| `entity` | Entity metadata | `entity.status.*`, `entity.label.*` |
| `fleet` | Fleet operations | `fleet.membership.*`, `fleet.assignment.*` |
| `drone` | Drone-specific | `drone.telemetry.*`, `drone.mission.*` |

## Alias Predicates

Some predicates provide alternate identifiers for entity resolution:

```go
vocabulary.Register("sensor.identifier.serial",
    vocabulary.WithDescription("Manufacturer serial number"),
    vocabulary.WithAlias(vocabulary.AliasTypeExternal, 1),
)
```

Alias types:

- `AliasTypeIdentity`: Entity equivalence (owl:sameAs)
- `AliasTypeAlternate`: Secondary unique ID
- `AliasTypeExternal`: External system ID
- `AliasTypeCommunication`: Call signs, hostnames
- `AliasTypeLabel`: Display names (NOT for resolution)

## Impact on Graph Queries

### PREDICATE_INDEX

Every predicate creates an index entry. Consistent naming enables queries:

```go
// All entities with temperature readings
QueryByPredicate(ctx, "sensor.measurement.celsius")

// All entities with fleet membership
QueryByPredicate(ctx, "fleet.membership.current")
```

Inconsistent naming fragments your index:

```go
// These are THREE separate predicates:
"temp"
"temperature"
"sensor.temperature"

// Queries only find entities using that exact predicate
```

### INCOMING_INDEX / OUTGOING_INDEX

Relationship predicates create edges:

```go
// Creates edge: drone → fleet
{Subject: droneID, Predicate: "fleet.membership.current", Object: fleetID}

// Query: Who belongs to this fleet?
GetIncoming(ctx, fleetID, "fleet.membership.current")
```

### Community Detection (LPA)

LPA (Label Propagation Algorithm) discovers communities by traversing edges in the graph. **Edges only exist when triples have relationship predicates**—predicates where the Object is a valid 6-part entity ID.

**Requirements for LPA to work:**

1. **Relationship predicates must exist**: At least some triples must reference other entities
2. **Valid entity IDs as Objects**: The Object must be a 6-part dotted entity ID (e.g., `acme.ops.fleet.cargo.rescue`)
3. **Bidirectional awareness**: INCOMING_INDEX and OUTGOING_INDEX track these edges

```go
// This creates an edge (LPA can traverse it):
{Subject: droneID, Predicate: "fleet.membership.current", Object: "acme.ops.fleet.cargo.rescue"}

// This does NOT create an edge (Object is a literal, not an entity ID):
{Subject: droneID, Predicate: "drone.telemetry.battery", Object: 78}
{Subject: droneID, Predicate: "drone.status.name", Object: "active"}
```

**If your entities have no relationship predicates, LPA will produce only singleton communities** (each entity in its own community). This is expected behavior—there are no edges to traverse.

For domains without natural relationships, consider adding explicit relationship predicates
(e.g., `zone.contains`, `sensor.monitors`).

## Best Practices

### 1. Define a Domain Vocabulary File

```go
// pkg/vocabulary/robotics/predicates.go
package robotics

const (
    // Telemetry
    TelemetryBattery  = "drone.telemetry.battery"
    TelemetryAltitude = "drone.telemetry.altitude"

    // Relationships
    FleetMembership = "fleet.membership.current"
    MissionAssignment = "mission.assignment.current"
)
```

### 2. Use Constants, Not Strings

```go
// Good: Constants are refactorable, typo-proof
{Predicate: robotics.TelemetryBattery, Object: 78}

// Bad: String typos cause silent failures
{Predicate: "drone.telemtry.battery", Object: 78}  // typo!
```

### 3. Document Your Vocabulary

```go
// TelemetryBattery represents battery charge level as a percentage.
// Range: 0-100
// Source: GCS heartbeat messages
const TelemetryBattery = "drone.telemetry.battery"
```

### 4. Version Breaking Changes

If you must change a predicate name:

```go
// Deprecated: Use TelemetryBattery
const BatteryLevel = "battery.level"

// Current
const TelemetryBattery = "drone.telemetry.battery"
```

Migrate data before removing the old predicate.

## Ontology Standards

SemStreams provides ontology subpackages that layer from foundational categories to domain-specific vocabularies:

```text
BFO (upper ontology)     ── foundational categories (Entity, Process, Object)
 └─ CCO (mid-level)      ── reusable concepts (Agent, Act, InformationContentEntity)
     └─ Domain standards  ── specific vocabularies (PROV-O, S-Agent-Comm, SSN/SOSA)
```

### Subpackages

| Package | Import Path | Purpose |
|---------|-------------|---------|
| `bfo` | `vocabulary/bfo` | BFO 2.0 (ISO 21838-2) upper-level ontology classes and relations |
| `cco` | `vocabulary/cco` | Common Core Ontologies for agents, actions, information entities |
| `agentic` | `vocabulary/agentic` | W3C S-Agent-Comm predicates for AI agent interoperability |

### Standards in `standards.go`

The root `vocabulary` package provides IRI constants for widely used semantic web standards:

- **OWL** - Web Ontology Language (equivalence, property types)
- **SKOS** - Simple Knowledge Organization System (labels, hierarchies)
- **Dublin Core** - Metadata terms (identifiers, titles, relations)
- **PROV-O** - W3C Provenance Ontology (entities, activities, agents, derivations)
- **SSN/SOSA** - Semantic Sensor Network / Sensor Observation (deployments, observations)
- **Schema.org** - Common web vocabulary (names, identifiers)
- **FOAF** - Friend of a Friend (names, accounts)

See [Vocabulary Package](../../vocabulary/README.md) for the full API reference and [Vocabulary Documentation](../vocabulary/) for architecture guides.

## Next Steps

- [First Processor](05-first-processor.md) - Use your vocabulary in a processor
- [Configuration](06-configuration.md) - How vocabulary affects capabilities
- [Index Reference](../advanced/05-index-reference.md) - How predicates become indexes
