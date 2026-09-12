# Vocabulary Package

**Purpose**: Semantic vocabulary management with dotted notation predicates and optional IRI mappings for standards compliance.

## Design Philosophy: Pragmatic Semantic Web

The vocabulary package follows a pragmatic semantic web approach that balances clean internal architecture with customer requirements for standards compliance.

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
        vocabulary.WithDataType(vocabulary.DataTypeFloat),
        vocabulary.WithUnits("percent"),
        vocabulary.WithRange("0-100"),
        vocabulary.WithIRI("http://schema.org/batteryLevel"))

    vocabulary.Register(BatteryVoltage,
        vocabulary.WithDescription("Battery voltage"),
        vocabulary.WithDataType(vocabulary.DataTypeFloat),
        vocabulary.WithUnits("volts"))

    vocabulary.Register(FlightModeArmed,
        vocabulary.WithDescription("Flight mode armed status"),
        vocabulary.WithDataType(vocabulary.DataTypeBool))
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
import "github.com/c360studio/semstreams/vocabulary/export"

// Serialize triples to Turtle
err := export.Serialize(os.Stdout, triples, export.Turtle)

// Or get as string in any format
s, err := export.SerializeToString(triples, export.NTriples)
s, err := export.SerializeToString(triples, export.JSONLD)
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

The **pragmatic type** of the object value — one of seven canonical constants. Not the Go type of the value: the
serializer observes that directly, and the authoritative `ENTITY_STATES` JSON round trip erases it anyway (a declared
whole number arrives back as `float64` every time). Declare only what observation cannot recover.

```go
vocabulary.Register("sensor.temperature.celsius",
    vocabulary.WithDataType(vocabulary.DataTypeFloat))
```

One declaration renders three ways. The Go column is what the serializer typically sees, **not** what you declare;
the RDF column is owned by `vocabulary/export` (ADR-107 keeps semantic-web vocabulary at the export edge, never on
the declaration surface):

| Constant | Value | Typical Go value | RDF (Turtle / N-Triples) | JSON Schema |
|---|---|---|---|---|
| `DataTypeString` | `string` | `string` | `"text"` (`xsd:string`, omitted) | `{"type":"string"}` |
| `DataTypeEntityID` | `entity_id` | `string` (6-part ID) | `<iri>` — a resource, never a literal | `{"type":"string","format":"semstreams-entity-id"}` |
| `DataTypeInt` | `int` | `float64` after the round trip | `"5"^^xsd:integer` | `{"type":"integer"}` |
| `DataTypeFloat` | `float` | `float64` | `"5.5"^^xsd:double` | `{"type":"number"}` |
| `DataTypeBool` | `bool` | `bool` | `"true"^^xsd:boolean` | `{"type":"boolean"}` |
| `DataTypeDateTime` | `datetime` | `time.Time`, or RFC 3339 `string` | `"…Z"^^xsd:dateTime` | `{"type":"string","format":"date-time"}` |
| `DataTypeJSON` | `json` | `string` | `"…"^^rdf:JSON` | `{"type":"string","contentMediaType":"application/json"}` |

Declaring nothing is legal. **Anything else panics at registration**, naming the offending value and the accepted
vocabulary. The framework translates nothing — there is no legacy-spelling map, so `float64`, `number`, `double`,
`time.Time`, `timestamp`, `int64`, `array`, `entity_ref`, `reference` and `boolean` all halt the binary rather than
becoming their canonical equivalent. If you declare any of them today, see
[`docs/operations/migration-predicate-datatype.md`](../docs/operations/migration-predicate-datatype.md), which is
the whole migration.

Export honors the declaration, and **ignores it for any triple whose observed value contradicts it** — a fractional
value under an `int` declaration serializes as `xsd:double`, not as a rounded integer.

#### `WithUnits(units string)`

Measurement units, as free-form **documentation**. No framework path validates, normalizes, interprets, or honors it
— unlike `WithDataType`, which is honored at export.

```go
vocabulary.Register("sensor.temperature.celsius",
    vocabulary.WithUnits("celsius"))
```

#### `WithRange(valueRange string)`

Valid value ranges, as free-form **documentation**. `"0-100"`, `"-90 to 90"` and `"positive"` are three incompatible
grammars, which is why nothing parses it. No framework path validates, normalizes, interprets, or honors it.

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
    vocabulary.WithDataType(vocabulary.DataTypeFloat),
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
    vocabulary.WithDataType(vocabulary.DataTypeString),
    vocabulary.WithAlias(vocabulary.AliasTypeCommunication, 0),
    vocabulary.WithIRI(vocabulary.FoafAccountName))

// External identifier
vocabulary.Register("robotics.identifier.serial",
    vocabulary.WithDescription("Manufacturer serial number"),
    vocabulary.WithDataType(vocabulary.DataTypeString),
    vocabulary.WithAlias(vocabulary.AliasTypeExternal, 1),
    vocabulary.WithIRI(vocabulary.DcIdentifier))

// Display label - NOT used for resolution
vocabulary.Register("entity.label.display",
    vocabulary.WithDescription("Human-readable display name"),
    vocabulary.WithDataType(vocabulary.DataTypeString),
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
    OwlSameAs            = "http://www.w3.org/2002/07/owl#sameAs"
    OwlEquivalentClass   = "http://www.w3.org/2002/07/owl#equivalentClass"
    OwlInverseOf         = "http://www.w3.org/2002/07/owl#inverseOf"
)
```

### SKOS (Simple Knowledge Organization System)

```go
const (
    SkosPrefLabel = "http://www.w3.org/2004/02/skos/core#prefLabel"
    SkosAltLabel  = "http://www.w3.org/2004/02/skos/core#altLabel"
    SkosBroader   = "http://www.w3.org/2004/02/skos/core#broader"
    SkosNarrower  = "http://www.w3.org/2004/02/skos/core#narrower"
)
```

### Dublin Core

```go
const (
    DcIdentifier  = "http://purl.org/dc/terms/identifier"
    DcTitle       = "http://purl.org/dc/terms/title"
    DcAlternative = "http://purl.org/dc/terms/alternative"
    DcReferences  = "http://purl.org/dc/terms/references"
    DcRequires    = "http://purl.org/dc/terms/requires"
)
```

### Schema.org

```go
const (
    SchemaName          = "https://schema.org/name"
    SchemaAlternateName = "https://schema.org/alternateName"
    SchemaIdentifier    = "https://schema.org/identifier"
    SchemaSameAs        = "https://schema.org/sameAs"
)
```

### PROV-O (Provenance Ontology)

```go
const (
    ProvEntity            = "http://www.w3.org/ns/prov#Entity"
    ProvActivity          = "http://www.w3.org/ns/prov#Activity"
    ProvAgent             = "http://www.w3.org/ns/prov#Agent"
    ProvWasDerivedFrom    = "http://www.w3.org/ns/prov#wasDerivedFrom"
    ProvWasGeneratedBy    = "http://www.w3.org/ns/prov#wasGeneratedBy"
    ProvWasAssociatedWith = "http://www.w3.org/ns/prov#wasAssociatedWith"
    ProvActedOnBehalfOf   = "http://www.w3.org/ns/prov#actedOnBehalfOf"
)
```

### SSN/SOSA (Semantic Sensor Network)

```go
const (
    SsnHasDeployment    = "http://www.w3.org/ns/ssn/hasDeployment"
    SosaObserves        = "http://www.w3.org/ns/sosa/observes"
    SosaHasSimpleResult = "http://www.w3.org/ns/sosa/hasSimpleResult"
)
```

### FOAF (Friend of a Friend)

```go
const (
    FoafName        = "http://xmlns.com/foaf/0.1/name"
    FoafAccountName = "http://xmlns.com/foaf/0.1/accountName"
)
```

See `standards.go` for the complete list of standard vocabulary IRIs.

## Ontology Subpackages

The vocabulary package includes ontology subpackages that provide IRI constants layered from foundational to domain-specific:

### `bfo/` - Basic Formal Ontology (BFO 2.0)

Upper-level ontology (ISO 21838-2) providing domain-neutral categories for all entities.

```go
import "github.com/c360studio/semstreams/vocabulary/bfo"

// Classify a physical asset
triple := message.Triple{
    Subject:   entityID,
    Predicate: "rdf.type",
    Object:    bfo.Object, // http://purl.obolibrary.org/obo/BFO_0000030
}
```

Key concepts: `Entity`, `Continuant`, `Occurrent`, `Object`, `Process`, `Role`, `Quality`

### `cco/` - Common Core Ontologies

Mid-level ontology built on BFO for modeling agents, actions, information entities, and artifacts.

```go
import "github.com/c360studio/semstreams/vocabulary/cco"

// Classify a software agent
triple := message.Triple{
    Subject:   agentID,
    Predicate: "rdf.type",
    Object:    cco.IntelligentSoftwareAgent,
}
```

Key concepts: `InformationContentEntity`, `Agent`, `IntentionalAct`, `PlanSpecification`, `Capability`

### `agentic/` - W3C S-Agent-Comm

AI agent interoperability predicates aligned with the W3C Semantic Agent Communication ontology.

```go
import "github.com/c360studio/semstreams/vocabulary/agentic"

// Express an agent's intent
triple := message.Triple{
    Subject:   agentID,
    Predicate: agentic.IntentGoal,
    Object:    "analyze customer feedback",
}

// Register all agentic predicates with IRI mappings
agentic.Register()
```

Key concepts: `IntentGoal`, `CapabilityName`, `DelegationFrom`, `AccountabilityActor`, `ExecutionEnvironment`

### `export/` - RDF Serialization

Serializes `[]message.Triple` to standard RDF formats (Turtle, N-Triples, JSON-LD) using the vocabulary registry for IRI resolution.

```go
import "github.com/c360studio/semstreams/vocabulary/export"

output, err := export.SerializeToString(triples, export.Turtle)
```

Supported formats: `export.Turtle`, `export.NTriples`, `export.JSONLD`

See [Vocabulary Documentation](docs/vocabulary/) for architecture guides and detailed usage.

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

The `export` package handles dotted-to-IRI translation automatically:

```go
import "github.com/c360studio/semstreams/vocabulary/export"

// Serialize to Turtle — registered predicates map to standard IRIs,
// unregistered predicates generate SemStreams predicate IRIs.
err := export.Serialize(w, triples, export.Turtle)

// Custom base IRI for subject generation
err = export.Serialize(w, triples, export.NTriples,
    export.WithBaseIRI("https://example.org"))

// Custom subject IRI function for full control
err = export.Serialize(w, triples, export.JSONLD,
    export.WithSubjectIRIFunc(func(subject string) string {
        return "https://example.org/entities/" + subject
    }))
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

#### Hierarchical Relationships

```go
vocabulary.GraphRelContains     // Parent contains child
vocabulary.GraphRelDependsOn    // Subject depends on object
```

- `graph.rel.contains` → `prov:hadMember` - Hierarchical containment (platform contains sensors)
- `graph.rel.depends_on` → `dcterms:requires` - Dependency relationship (spec depends on spec)

#### Reference Relationships

```go
vocabulary.GraphRelReferences   // Directional reference
vocabulary.GraphRelRelatedTo    // General association
vocabulary.GraphRelDiscusses    // Discussion/commentary
```

- `graph.rel.references` → `dcterms:references` - Documentation references specifications
- `graph.rel.related_to` → `dcterms:relation` - Generic relationship
- `graph.rel.discusses` → `schema:about` - Discussion about a topic

#### Causal Relationships

```go
vocabulary.GraphRelInfluences   // Causal/impact relationship
vocabulary.GraphRelTriggeredBy  // Event causation
```

- `graph.rel.influences` - Decision influences implementation
- `graph.rel.triggered_by` - Alert triggered by threshold

#### Implementation Relationships

```go
vocabulary.GraphRelImplements   // Implementation relationship
vocabulary.GraphRelSupersedes   // Replacement/versioning
vocabulary.GraphRelBlockedBy    // Blocking relationship
```

- `graph.rel.implements` - Code implements specification
- `graph.rel.supersedes` → `dcterms:replaces` - v2 supersedes v1
- `graph.rel.blocked_by` - Issue blocked by another issue

#### Spatial/Communication Relationships

```go
vocabulary.GraphRelNear         // Spatial proximity
vocabulary.GraphRelCommunicates // Communication/interaction
```

- `graph.rel.near` - Sensors near a location
- `graph.rel.communicates` - Services communicate with each other

### Usage Example

```go
import "github.com/c360studio/semstreams/vocabulary"

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
- `standards.go` - Standard vocabulary IRI constants (OWL, SKOS, Dublin Core, PROV-O, SSN/SOSA)
- `bfo/` - BFO 2.0 upper-level ontology classes and relations
- `cco/` - Common Core Ontology classes for agents, actions, information
- `agentic/` - W3C S-Agent-Comm predicates for AI agent interoperability
- `export/` - RDF serialization to Turtle, N-Triples, JSON-LD
- `examples/` - Reference domain vocabulary implementations
- `message/triple.go` - Triple structure for semantic facts
- `message/types.go` - EntityID, EntityType, Type patterns
- [Vocabulary Documentation](docs/vocabulary/) - Architecture guides and usage patterns

The vocabulary package focuses on **predicate management**, not IRI generation. For entity-level IRI needs, use the entity's own methods.
