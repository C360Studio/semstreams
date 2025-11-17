# Vocabulary Package

**Purpose**: Semantic vocabulary management with dotted notation predicates and optional IRI mappings for standards compliance.

**Last Updated**: 2025-11-13
**Status**: Production Ready

## Design Philosophy: Pragmatic Semantic Web

The vocabulary package follows a "pragmatic semantic web" approach that balances clean internal architecture with customer requirements for standards compliance.

### Core Principles

**Internal**: Always use dotted notation (domain.category.property)

- Clean, human-readable predicates
- NATS wildcard query support
- No URI/IRI complexity in internal code

**External**: Optional IRI mappings at API boundaries

- RDF/Turtle export with standard vocabularies
- OGC compliance (GeoSPARQL, SSN/SOSA)
- Integration with existing semantic systems

**No Leakage**: Standards complexity does NOT leak inward

- Internal code never sees IRIs
- Triples always use dotted predicates
- NATS subjects use dotted notation

## Predicate Structure

All predicates follow three-level dotted notation:

```text
domain.category.property
```

**Examples**:

- `sensor.temperature.celsius` - Sensor domain, temperature category
- `geo.location.latitude` - Geospatial domain, location category
- `time.lifecycle.created` - Temporal domain, lifecycle category
- `robotics.battery.level` - Robotics domain, battery category

**Naming Conventions**:

- **domain**: lowercase, business domain (sensors, geo, time, robotics)
- **category**: lowercase, groups related properties (temperature, location, lifecycle)
- **property**: lowercase, specific property name (celsius, latitude, created)
- No underscores or special characters (dots only for level separation)

**Why This Works**:

- ✅ NATS wildcards: `robotics.battery.*` finds all battery predicates
- ✅ Human-readable: Clear semantic meaning without URIs
- ✅ Consistent: Matches EntityID.Key() and Type.Key() patterns
- ✅ Simple: No namespace prefixes or IRI complexity

## Quick Start

### 1. Define Domain Predicates

```go
package robotics

const (
    BatteryLevel   = "robotics.battery.level"
    BatteryVoltage = "robotics.battery.voltage"
    FlightModeArmed = "robotics.flight.armed"
)
```

### 2. Register with Metadata

```go
func init() {
    vocabulary.Register(BatteryLevel,
        vocabulary.WithDescription("Battery charge level percentage"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithUnits("percent"),
        vocabulary.WithRange("0-100"),
        vocabulary.WithIRI("http://schema.org/batteryLevel"))

    vocabulary.Register(BatteryVoltage,
        vocabulary.WithDescription("Battery voltage"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithUnits("volts"))

    vocabulary.Register(FlightModeArmed,
        vocabulary.WithDescription("Flight mode armed status"),
        vocabulary.WithDataType("bool"))
}
```

### 3. Use in Messages

```go
// Create triples - always dotted notation
triple := message.Triple{
    Subject:   entityID,
    Predicate: robotics.BatteryLevel,  // "robotics.battery.level"
    Object:    85.5,
}

// NATS queries work naturally
nc.Subscribe("robotics.battery.*", handler)  // All battery predicates
```

### 4. Export to RDF (Optional)

```go
// At API boundary - translate to IRI if needed
if meta := vocabulary.GetPredicateMetadata(triple.Predicate); meta != nil {
    if meta.StandardIRI != "" {
        rdfTriple.Predicate = meta.StandardIRI  // "http://schema.org/batteryLevel"
    }
}
```

## Functional Options API

The `Register()` function uses functional options for clean, composable configuration:

### Available Options

#### `WithDescription(desc string)`

Human-readable description of the predicate.

```go
vocabulary.Register("sensor.temperature.celsius",
    vocabulary.WithDescription("Temperature in degrees Celsius"))
```

#### `WithDataType(dataType string)`

Expected Go type for the object value.

Common types: `"string"`, `"float64"`, `"int"`, `"bool"`, `"time.Time"`

```go
vocabulary.Register("sensor.temperature.celsius",
    vocabulary.WithDataType("float64"))
```

#### `WithUnits(units string)`

Measurement units (if applicable).

```go
vocabulary.Register("sensor.temperature.celsius",
    vocabulary.WithUnits("celsius"))
```

#### `WithRange(valueRange string)`

Valid value ranges (if applicable).

```go
vocabulary.Register("robotics.battery.level",
    vocabulary.WithRange("0-100"))
```

#### `WithIRI(iri string)`

W3C/RDF equivalent IRI for standards compliance.

Use constants from `standards.go` for common vocabularies.

```go
vocabulary.Register("entity.label.preferred",
    vocabulary.WithIRI(vocabulary.SkosPrefLabel))
```

#### `WithAlias(aliasType, priority int)`

Mark as entity alias for resolution.

```go
vocabulary.Register("robotics.communication.callsign",
    vocabulary.WithAlias(vocabulary.AliasTypeCommunication, 0))  // Priority 0 = highest
```

### Complete Example

```go
vocabulary.Register("robotics.battery.level",
    vocabulary.WithDescription("Battery charge level percentage"),
    vocabulary.WithDataType("float64"),
    vocabulary.WithUnits("percent"),
    vocabulary.WithRange("0-100"),
    vocabulary.WithIRI("http://schema.org/batteryLevel"))
```

## Alias Predicates for Entity Resolution

Some predicates represent entity aliases (identifiers, labels, call signs). The registry tracks these for entity resolution and correlation.

### Alias Types

**AliasTypeIdentity** - Entity equivalence

- Go constants: `vocabulary.OwlSameAs`, `vocabulary.SchemaSameAs`
- RDF equivalents: owl:sameAs, schema:sameAs
- Use for: Federated entity IDs, external system UUIDs
- Resolution: ✅ Can resolve to entity IDs

**AliasTypeAlternate** - Secondary unique identifiers

- Go constants: `vocabulary.SchemaAlternateName`, `vocabulary.DcAlternative`
- RDF equivalents: schema:alternateName, dc:alternative
- Use for: Model numbers, registration IDs
- Resolution: ✅ Can resolve to entity IDs

**AliasTypeExternal** - External system identifiers

- Go constants: `vocabulary.DcIdentifier`, `vocabulary.SchemaIdentifier`
- RDF equivalents: dc:identifier, schema:identifier
- Use for: Manufacturer serial numbers, legacy system IDs
- Resolution: ✅ Can resolve to entity IDs

**AliasTypeCommunication** - Communication identifiers

- Go constants: `vocabulary.FoafAccountName`
- RDF equivalent: foaf:accountName
- Use for: Radio call signs, network hostnames, MQTT client IDs
- Resolution: ✅ Can resolve to entity IDs

**AliasTypeLabel** - Display names

- Go constants: `vocabulary.RdfsLabel`, `vocabulary.SkosPrefLabel`, `vocabulary.SchemaName`
- RDF equivalents: rdfs:label, skos:prefLabel, schema:name
- Use for: Human-readable display names
- Resolution: ❌ NOT for resolution (ambiguous - many entities share labels)

**Note**: The RDF notation shown (e.g., `owl:sameAs`) is shorthand for full IRIs. In Go code, always use the provided constants from `standards.go` (e.g., `vocabulary.OwlSameAs`).

### Example: Registering Aliases

```go
// Communication identifier - highest priority
vocabulary.Register("robotics.communication.callsign",
    vocabulary.WithDescription("Radio call sign for ATC"),
    vocabulary.WithDataType("string"),
    vocabulary.WithAlias(vocabulary.AliasTypeCommunication, 0),
    vocabulary.WithIRI(vocabulary.FoafAccountName))

// External identifier
vocabulary.Register("robotics.identifier.serial",
    vocabulary.WithDescription("Manufacturer serial number"),
    vocabulary.WithDataType("string"),
    vocabulary.WithAlias(vocabulary.AliasTypeExternal, 1),
    vocabulary.WithIRI(vocabulary.DcIdentifier))

// Display label - NOT used for resolution
vocabulary.Register("entity.label.display",
    vocabulary.WithDescription("Human-readable display name"),
    vocabulary.WithDataType("string"),
    vocabulary.WithAlias(vocabulary.AliasTypeLabel, 10),  // Low priority
    vocabulary.WithIRI(vocabulary.RdfsLabel))
```

### Discovering Aliases

```go
// Get all alias predicates with priorities
aliases := vocabulary.DiscoverAliasPredicates()
// Returns: map["robotics.communication.callsign"]int(0), etc.
```

## Standards Mappings

Common standard vocabulary IRIs are provided in `standards.go`:

### OWL (Web Ontology Language)
```go
const (
    OWL_SAME_AS = "http://www.w3.org/2002/07/owl#sameAs"
    OWL_THING   = "http://www.w3.org/2002/07/owl#Thing"
)
```

### SKOS (Simple Knowledge Organization System)
```go
const (
    SKOS_PREF_LABEL = "http://www.w3.org/2004/02/skos/core#prefLabel"
    SKOS_ALT_LABEL  = "http://www.w3.org/2004/02/skos/core#altLabel"
)
```

### Schema.org
```go
const (
    SCHEMA_NAME           = "http://schema.org/name"
    SCHEMA_ALTERNATE_NAME = "http://schema.org/alternateName"
    SCHEMA_IDENTIFIER     = "http://schema.org/identifier"
)
```

### Dublin Core
```go
const (
    DC_IDENTIFIER   = "http://purl.org/dc/terms/identifier"
    DC_TITLE        = "http://purl.org/dc/terms/title"
)
```

### FOAF (Friend of a Friend)
```go
const (
    FOAF_NAME        = "http://xmlns.com/foaf/0.1/name"
    FOAF_ACCOUNT_NAME = "http://xmlns.com/foaf/0.1/accountName"
)
```

See `standards.go` for the complete list of standard vocabulary IRIs.

## Registry API

### Registration

```go
// Register with functional options
vocabulary.Register(name string, opts ...Option)

// Register using struct directly (backward compatibility)
vocabulary.RegisterPredicate(meta PredicateMetadata)
```

### Retrieval

```go
// Get metadata for a predicate
meta := vocabulary.GetPredicateMetadata("robotics.battery.level")
if meta != nil {
    fmt.Println(meta.Description)
    fmt.Println(meta.StandardIRI)
}

// List all registered predicates
predicates := vocabulary.ListRegisteredPredicates()

// Discover alias predicates
aliases := vocabulary.DiscoverAliasPredicates()
```

### Testing

```go
// Clear registry (testing only)
vocabulary.ClearRegistry()
```

## Internal vs External Usage

### Internal: Always Dotted Notation

```go
// Creating triples
triple := message.Triple{
    Subject:   "c360.platform1.robotics.drone.001",
    Predicate: "robotics.battery.level",  // Dotted, not IRI
    Object:    85.5,
}

// NATS subscriptions
nc.Subscribe("robotics.battery.*", handler)

// Entity properties
entityState.SetProperty("geo.location.latitude", 37.7749)

// NO IRIs in internal code!
```

### External: IRI Mappings at Boundaries

```go
// RDF export - translate dotted to IRI
func ExportToRDF(triples []message.Triple) []RDFTriple {
    rdfTriples := make([]RDFTriple, 0, len(triples))

    for _, triple := range triples {
        rdfTriple := RDFTriple{
            Subject: triple.Subject,
            Object:  triple.Object,
        }

        // Translate predicate to IRI if registered
        if meta := vocabulary.GetPredicateMetadata(triple.Predicate); meta != nil {
            if meta.StandardIRI != "" {
                rdfTriple.Predicate = meta.StandardIRI
            } else {
                rdfTriple.Predicate = triple.Predicate  // Use dotted as fallback
            }
        }

        rdfTriples = append(rdfTriples, rdfTriple)
    }

    return rdfTriples
}

// RDF import - translate IRI to dotted
func ImportFromRDF(rdfTriples []RDFTriple) []message.Triple {
    // Build reverse lookup: IRI -> dotted name
    iriToName := make(map[string]string)
    for _, name := range vocabulary.ListRegisteredPredicates() {
        if meta := vocabulary.GetPredicateMetadata(name); meta != nil {
            if meta.StandardIRI != "" {
                iriToName[meta.StandardIRI] = name
            }
        }
    }

    triples := make([]message.Triple, 0, len(rdfTriples))

    for _, rdfTriple := range rdfTriples {
        triple := message.Triple{
            Subject: rdfTriple.Subject,
            Object:  rdfTriple.Object,
        }

        // Translate IRI to dotted notation
        if dotted, ok := iriToName[rdfTriple.Predicate]; ok {
            triple.Predicate = dotted
        } else {
            // Unknown IRI - skip or handle as needed
            continue
        }

        triples = append(triples, triple)
    }

    return triples
}
```

## Best Practices

### Predicate Definition

1. **Use Package Constants**
   - Define predicates as package constants
   - Don't inline predicate strings
   - Group by domain and category

2. **Register in init()**
   - Register all domain predicates during package initialization
   - Use functional options for clarity
   - Include IRI mappings for customer-facing predicates

3. **Consistent Naming**
   - Follow `domain.category.property` strictly
   - Use lowercase throughout
   - Choose semantic, descriptive names

### IRI Mappings

1. **Only When Needed**
   - Map to standard IRIs for customer integrations
   - Skip IRI for internal-only predicates
   - Use well-known standards (Schema.org, OWL, SKOS)

2. **At API Boundaries**
   - Translate dotted → IRI in RDF export
   - Translate IRI → dotted in RDF import
   - Keep internal code IRI-free

3. **Document Mappings**
   - Explain why specific IRI chosen
   - Reference standard vocabulary documentation
   - Note any semantic differences

### Entity Resolution

1. **Mark Alias Predicates**
   - Use `WithAlias()` for identity-related predicates
   - Set appropriate priority for conflict resolution
   - Choose correct AliasType for semantics

2. **Avoid Label Confusion**
   - Don't use AliasTypeLabel for resolution
   - Labels are for display only (ambiguous)
   - Use identity/alternate/external for unique IDs

## Graph Domain Predicates

The vocabulary package provides standard relationship predicates for linking entities in the semantic graph. These `graph.rel.*` predicates enable rich semantic relationships with mappings to standard vocabularies.

### Relationship Types

All relationship predicates follow the pattern `graph.rel.*` and are registered with Dublin Core, Schema.org, and PROV-O mappings where applicable.

**Hierarchical Relationships**

```go
vocabulary.GraphRelContains     // Parent contains child
vocabulary.GraphRelDependsOn    // Subject depends on object
```

- `graph.rel.contains` → `prov:hadMember` - Hierarchical containment (platform contains sensors)
- `graph.rel.depends_on` → `dcterms:requires` - Dependency relationship (spec depends on spec)

**Reference Relationships**

```go
vocabulary.GraphRelReferences   // Directional reference
vocabulary.GraphRelRelatedTo    // General association
vocabulary.GraphRelDiscusses    // Discussion/commentary
```

- `graph.rel.references` → `dcterms:references` - Documentation references specifications
- `graph.rel.related_to` → `dcterms:relation` - Generic relationship
- `graph.rel.discusses` → `schema:about` - Discussion about a topic

**Causal Relationships**

```go
vocabulary.GraphRelInfluences   // Causal/impact relationship
vocabulary.GraphRelTriggeredBy  // Event causation
```

- `graph.rel.influences` - Decision influences implementation
- `graph.rel.triggered_by` - Alert triggered by threshold

**Implementation Relationships**

```go
vocabulary.GraphRelImplements   // Implementation relationship
vocabulary.GraphRelSupersedes   // Replacement/versioning
vocabulary.GraphRelBlockedBy    // Blocking relationship
```

- `graph.rel.implements` - Code implements specification
- `graph.rel.supersedes` → `dcterms:replaces` - v2 supersedes v1
- `graph.rel.blocked_by` - Issue blocked by another issue

**Spatial/Communication Relationships**

```go
vocabulary.GraphRelNear         // Spatial proximity
vocabulary.GraphRelCommunicates // Communication/interaction
```

- `graph.rel.near` - Sensors near a location
- `graph.rel.communicates` - Services communicate with each other

### Usage Example

```go
import "github.com/c360/semstreams/vocabulary"

// Create relationship between specification and implementation
triple := message.Triple{
    Subject:   "spec-001",
    Predicate: vocabulary.GraphRelImplements,  // "graph.rel.implements"
    Object:    "pr-123",
}

// Query all relationships using NATS wildcards
nc.Subscribe("graph.rel.*", handler)  // All relationship types
nc.Subscribe("graph.rel.contains", handler)  // Only containment
```

### Standards Compliance

Relationship predicates map to established semantic web vocabularies:

- **Dublin Core Terms** - References, dependencies, replacements, relations
- **PROV-O** - Provenance and membership relationships
- **Schema.org** - Discussion and content relationships

See `relationships.go` for the complete registration and `standards.go` for IRI constants.

## Framework Predicates

The vocabulary package provides example framework predicates in `predicates.go`. These demonstrate the pattern but are NOT required.

Applications should define their own domain-specific vocabularies. See `examples/robotics.go` and `examples/semantic.go` for reference implementations.

## Related Documentation

- `doc.go` - Comprehensive package documentation
- `standards.go` - Standard vocabulary IRI constants
- `examples/` - Reference domain vocabulary implementations
- `message/triple.go` - Triple structure for semantic facts
- `message/types.go` - EntityID, EntityType, Type patterns

## Migration from Legacy Code

If you have code using colon notation ("robotics:Drone"), update to dotted notation:

**Before:**
```go
iri := vocabulary.EntityTypeIRI("robotics:Drone")
```

**After:**
```go
entityType := message.EntityType{Domain: "robotics", Type: "drone"}
typeStr := entityType.Key()  // "robotics.drone"
```

The vocabulary package focuses on **predicate management**, not IRI generation. For entity-level IRI needs, use the entity's own methods.
