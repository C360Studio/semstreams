// Package vocabulary provides semantic vocabulary management for the SemStreams platform.
// It defines predicates using dotted notation and provides optional IRI mappings for
// standards compliance at API boundaries.
//
// # Architecture Philosophy: Pragmatic Semantic Web
//
// The vocabulary package follows a "pragmatic semantic web" approach:
//
// **Internal**: Clean dotted notation everywhere (domain.category.property)
//
// **External**: Optional IRI/URI mappings at API boundaries for standards compliance
//
// **No Leakage**: Standards complexity does NOT leak into internal architecture
//
// # Core Design Principles
//
// 1. **Dotted Notation Internally**
//   - Predicates: "robotics.battery.level" (not URIs)
//   - Enables NATS wildcard queries: "robotics.battery.*"
//   - Human-readable and semantic
//   - Consistent with message.Type and EntityID patterns
//
// 2. **IRI Mappings at Boundaries**
//   - Optional StandardIRI field for RDF/OGC export
//   - Bidirectional translation (dotted ↔ IRI)
//   - Used only for customer integrations
//   - Internal code never sees IRIs
//
// 3. **Customer Standards Support**
//   - RDF/Turtle export with standard vocabularies
//   - OGC compliance (GeoSPARQL, SSN/SOSA ontologies)
//   - Integration with existing semantic systems
//   - Support for domain-specific ontologies
//
// # Predicate Structure
//
// Predicates follow three-level dotted notation:
//
//	domain.category.property
//
// Examples:
//   - sensor.temperature.celsius
//   - geo.location.latitude
//   - time.lifecycle.created
//   - robotics.battery.level
//
// Naming conventions:
//   - domain: lowercase, business domain (sensors, geo, time, robotics)
//   - category: lowercase, groups related properties (temperature, location, lifecycle)
//   - property: lowercase, specific property name (celsius, latitude, created)
//   - No underscores or special characters (dots only for level separation)
//
// # Predicate Registration
//
// Register domain-specific predicates using functional options:
//
//	vocabulary.Register("robotics.battery.level",
//	    vocabulary.WithDescription("Battery charge level percentage"),
//	    vocabulary.WithDataType("float64"),
//	    vocabulary.WithUnits("percent"),
//	    vocabulary.WithRange("0-100"),
//	    vocabulary.WithIRI("http://schema.org/batteryLevel"))
//
// The registry provides:
//   - Metadata about each predicate (type, description, units)
//   - Optional IRI mappings for RDF export
//   - Alias semantics for entity resolution
//   - Runtime predicate discovery
//
// # Internal Usage (Always Dotted)
//
//	// Creating triples - always use dotted notation
//	triple := message.Triple{
//	    Subject:   entityID,
//	    Predicate: "robotics.battery.level",  // Clean, dotted
//	    Object:    85.5,
//	}
//
//	// Querying via NATS - wildcards work naturally
//	nc.Subscribe("robotics.battery.*", handler)  // All battery predicates
//
//	// No IRI usage in internal code!
//
// # External Usage (IRI Mappings at API Boundaries)
//
//	// RDF export - translate dotted to IRI
//	if meta := vocabulary.GetPredicateMetadata(triple.Predicate); meta != nil {
//	    if meta.StandardIRI != "" {
//	        rdfTriple.Predicate = meta.StandardIRI  // "http://schema.org/batteryLevel"
//	    }
//	}
//
//	// Import from RDF - translate IRI to dotted
//	if dotted := vocabulary.LookupByIRI(rdfTriple.Predicate); dotted != "" {
//	    triple.Predicate = dotted  // "robotics.battery.level"
//	}
//
// # Alias Predicates for Entity Resolution
//
// Some predicates represent entity aliases (identifiers, labels, call signs).
// The registry tracks these for entity resolution:
//
//	vocabulary.Register("robotics.communication.callsign",
//	    vocabulary.WithDescription("Radio call sign for ATC"),
//	    vocabulary.WithAlias(vocabulary.AliasTypeCommunication, 0))  // Priority 0 = highest
//
// Alias types:
//   - AliasTypeIdentity: Entity equivalence (owl:sameAs, schema:sameAs)
//   - AliasTypeAlternate: Secondary unique identifiers (model numbers, registration IDs)
//   - AliasTypeExternal: External system identifiers (serial numbers, legacy IDs)
//   - AliasTypeCommunication: Communication identifiers (call signs, hostnames)
//   - AliasTypeLabel: Display names (NOT used for resolution - ambiguous)
//
// # Domain Vocabulary Examples
//
// Applications define domain-specific vocabularies:
//
//	package robotics
//
//	const (
//	    BatteryLevel = "robotics.battery.level"
//	    BatteryVoltage = "robotics.battery.voltage"
//	    FlightModeArmed = "robotics.flight.armed"
//	)
//
//	func init() {
//	    vocabulary.Register(BatteryLevel,
//	        vocabulary.WithDescription("Battery charge percentage"),
//	        vocabulary.WithDataType("float64"),
//	        vocabulary.WithUnits("percent"),
//	        vocabulary.WithRange("0-100"),
//	        vocabulary.WithIRI("http://schema.org/batteryLevel"))
//	    // ... register other predicates
//	}
//
// # Standards Mappings
//
// Common standard vocabulary IRIs are provided in standards.go:
//
//	const (
//	    OWL_SAME_AS = "http://www.w3.org/2002/07/owl#sameAs"
//	    SKOS_PREF_LABEL = "http://www.w3.org/2004/02/skos/core#prefLabel"
//	    SCHEMA_NAME = "http://schema.org/name"
//	    // ... many more
//	)
//
// Use these constants when registering predicates with standard mappings:
//
//	vocabulary.Register("entity.label.preferred",
//	    vocabulary.WithIRI(vocabulary.SKOS_PREF_LABEL))
//
// # Best Practices
//
// ## Predicate Definition
//
// 1. **Use Package Constants**
//   - Define predicates as package constants
//   - Don't inline predicate strings at call sites
//   - Group by domain and category
//
// 2. **Register in init()**
//   - Register all domain predicates during package initialization
//   - Use functional options for clarity
//   - Include IRI mappings for customer-facing predicates
//
// 3. **Consistent Naming**
//   - Follow domain.category.property pattern strictly
//   - Use lowercase throughout
//   - Choose semantic, descriptive names
//
// ## IRI Mappings
//
// 1. **Only When Needed**
//   - Map to standard IRIs for customer integrations
//   - Skip IRI for internal-only predicates
//   - Use well-known standards (Schema.org, OWL, SKOS, etc.)
//
// 2. **At API Boundaries**
//   - Translate dotted → IRI in RDF export
//   - Translate IRI → dotted in RDF import
//   - Keep internal code IRI-free
//
// 3. **Document Mappings**
//   - Explain why specific IRI chosen
//   - Reference standard vocabulary documentation
//   - Note any semantic differences
//
// ## Entity Resolution
//
// 1. **Mark Alias Predicates**
//   - Use WithAlias() for identity-related predicates
//   - Set appropriate priority for conflict resolution
//   - Choose correct AliasType for semantics
//
// 2. **Avoid Label Confusion**
//   - Don't use AliasTypeLabel for resolution
//   - Labels are for display only (ambiguous)
//   - Use identity/alternate/external for unique IDs
//
// # Registry API
//
// The global predicate registry provides:
//
//	// Register a predicate
//	Register(name string, opts ...Option)
//
//	// Retrieve metadata
//	GetPredicateMetadata(name string) *PredicateMetadata
//
//	// List all registered
//	ListRegisteredPredicates() []string
//
//	// Discover aliases
//	DiscoverAliasPredicates() map[string]int
//
//	// Clear (testing only)
//	ClearRegistry()
//
// # Migration from Colon Notation
//
// If you have legacy code using colon notation ("robotics:Drone"):
//
// **Before:**
//
//	iri := vocabulary.EntityTypeIRI("robotics:Drone")
//
// **After:**
//
//	// Use dotted notation everywhere
//	entityType := message.EntityType{Domain: "robotics", Type: "drone"}
//	typeStr := entityType.Key()  // "robotics.drone"
//
//	// IRI functions now accept dotted notation
//	iri := vocabulary.EntityTypeIRI(typeStr)
//
// # Design Philosophy Summary
//
// The vocabulary package embodies these principles:
//
// 1. **Simplicity**: Dotted notation is clean and human-readable
// 2. **NATS-friendly**: Wildcards work naturally with dotted predicates
// 3. **Standards-compliant**: Optional IRI mappings at boundaries
// 4. **No Leakage**: External complexity stays external
// 5. **Customer Value**: Support RDF/OGC without internal cost
//
// This "pragmatic semantic web" approach gives you:
//   - Clean internal architecture
//   - Standards compliance where needed
//   - Customer integration support
//   - NATS query power
//   - Human-readable code
package vocabulary
