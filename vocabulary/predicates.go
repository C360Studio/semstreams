package vocabulary

// Predicate vocabulary using three-level dotted notation: domain.category.property
// This maintains consistency with the unified semantic architecture.
//
// Design principles:
//   - Three levels: domain.category.property (e.g., "sensor.temperature.celsius")
//   - Enables NATS wildcard queries: "sensor.temperature.*" finds all temperature predicates
//   - Human readable: "geo.location.latitude" is clear and semantic
//   - Domain scoped: Each domain manages its own predicate categories
//   - Consistent with EntityType.Key(), MessageType.Key(), and EntityID.Key() patterns
//
// Predicate naming conventions:
//   - domain: lowercase, represents business domain (sensors, geo, time, etc.)
//   - category: lowercase, groups related properties (temperature, location, lifecycle, etc.)
//   - property: lowercase, specific property name (celsius, latitude, created, etc.)
//   - No underscores or special characters (dots only for level separation)
//
// Domain applications (like semops for robotics) define their own domain-specific predicates.
// These examples demonstrate the framework's predicate pattern.

// Sensor Domain Predicates
// These predicates describe sensor measurements and environmental data

const (
	// SensorTemperatureCelsius is float64, degrees Celsius
	SensorTemperatureCelsius = "sensor.temperature.celsius"
	// SensorTemperatureFahrenheit is float64, degrees Fahrenheit
	SensorTemperatureFahrenheit = "sensor.temperature.fahrenheit"
	// SensorTemperatureKelvin is float64, degrees Kelvin
	SensorTemperatureKelvin = "sensor.temperature.kelvin"

	// SensorPressurePascals is float64, pascals
	SensorPressurePascals = "sensor.pressure.pascals"
	// SensorPressureBar is float64, bar
	SensorPressureBar = "sensor.pressure.bar"
	// SensorPressurePsi is float64, pounds per square inch
	SensorPressurePsi = "sensor.pressure.psi"

	// SensorHumidityPercent is float64, 0-100 percentage
	SensorHumidityPercent = "sensor.humidity.percent"
	// SensorHumidityAbsolute is float64, g/m³
	SensorHumidityAbsolute = "sensor.humidity.absolute"

	// SensorAccelX is float64, m/s²
	SensorAccelX = "sensor.accel.x"
	// SensorAccelY is float64, m/s²
	SensorAccelY = "sensor.accel.y"
	// SensorAccelZ is float64, m/s²
	SensorAccelZ = "sensor.accel.z"

	// SensorGyroX is float64, rad/s
	SensorGyroX = "sensor.gyro.x"
	// SensorGyroY is float64, rad/s
	SensorGyroY = "sensor.gyro.y"
	// SensorGyroZ is float64, rad/s
	SensorGyroZ = "sensor.gyro.z"

	// SensorMagX is float64, gauss
	SensorMagX = "sensor.mag.x"
	// SensorMagY is float64, gauss
	SensorMagY = "sensor.mag.y"
	// SensorMagZ is float64, gauss
	SensorMagZ = "sensor.mag.z"
)

// Geospatial Domain Predicates
// These predicates describe location, positioning, and geographic data

const (
	// GeoLocationLatitude is float64, degrees (-90 to 90)
	GeoLocationLatitude = "geo.location.latitude"
	// GeoLocationLongitude is float64, degrees (-180 to 180)
	GeoLocationLongitude = "geo.location.longitude"
	// GeoLocationAltitude is float64, meters above sea level
	GeoLocationAltitude = "geo.location.altitude"
	// GeoLocationElevation is float64, meters above ground
	GeoLocationElevation = "geo.location.elevation"

	// GeoVelocityGround is float64, m/s ground speed
	GeoVelocityGround = "geo.velocity.ground"
	// GeoVelocityVertical is float64, m/s climb/descent rate
	GeoVelocityVertical = "geo.velocity.vertical"
	// GeoVelocityHeading is float64, degrees (0-360)
	GeoVelocityHeading = "geo.velocity.heading"

	// GeoAccuracyHorizontal is float64, meters CEP
	GeoAccuracyHorizontal = "geo.accuracy.horizontal"
	// GeoAccuracyVertical is float64, meters vertical accuracy
	GeoAccuracyVertical = "geo.accuracy.vertical"
	// GeoAccuracyDilution is float64, dilution of precision
	GeoAccuracyDilution = "geo.accuracy.dilution"

	// GeoZoneUtm is string, UTM zone
	GeoZoneUtm = "geo.zone.utm"
	// GeoZoneMgrs is string, MGRS grid
	GeoZoneMgrs = "geo.zone.mgrs"
	// GeoZoneRegion is string, geographic region name
	GeoZoneRegion = "geo.zone.region"
)

// Temporal Domain Predicates
// These predicates describe time-related properties and lifecycle events

const (
	// TimeLifecycleCreated is time.Time, when entity was created
	TimeLifecycleCreated = "time.lifecycle.created"
	// TimeLifecycleUpdated is time.Time, when entity was last updated
	TimeLifecycleUpdated = "time.lifecycle.updated"
	// TimeLifecycleSeen is time.Time, when entity was last observed
	TimeLifecycleSeen = "time.lifecycle.seen"
	// TimeLifecycleExpired is time.Time, when entity expired/deleted
	TimeLifecycleExpired = "time.lifecycle.expired"

	// TimeDurationActive is float64, seconds active
	TimeDurationActive = "time.duration.active"
	// TimeDurationIdle is float64, seconds idle
	TimeDurationIdle = "time.duration.idle"
	// TimeDurationTotal is float64, total seconds
	TimeDurationTotal = "time.duration.total"

	// TimeScheduleStart is time.Time, scheduled start
	TimeScheduleStart = "time.schedule.start"
	// TimeScheduleEnd is time.Time, scheduled end
	TimeScheduleEnd = "time.schedule.end"
	// TimeScheduleNext is time.Time, next scheduled event
	TimeScheduleNext = "time.schedule.next"
)

// Network Domain Predicates
// These predicates describe network connectivity and communication

const (
	// NetworkConnectionStatus is string, connection status
	NetworkConnectionStatus = "network.connection.status"
	// NetworkConnectionStrength is float64, signal strength
	NetworkConnectionStrength = "network.connection.strength"
	// NetworkConnectionLatency is float64, milliseconds
	NetworkConnectionLatency = "network.connection.latency"

	// NetworkProtocolType is string, protocol name
	NetworkProtocolType = "network.protocol.type"
	// NetworkProtocolVersion is string, protocol version
	NetworkProtocolVersion = "network.protocol.version"
	// NetworkProtocolPort is int, port number
	NetworkProtocolPort = "network.protocol.port"

	// NetworkTrafficBytesIn is int64, bytes received
	NetworkTrafficBytesIn = "network.traffic.bytes.in"
	// NetworkTrafficBytesOut is int64, bytes sent
	NetworkTrafficBytesOut = "network.traffic.bytes.out"
	// NetworkTrafficPacketsIn is int64, packets received
	NetworkTrafficPacketsIn = "network.traffic.packets.in"
	// NetworkTrafficPacketsOut is int64, packets sent
	NetworkTrafficPacketsOut = "network.traffic.packets.out"
)

// Quality Domain Predicates
// These predicates describe data quality, confidence, and validation

const (
	// QualityConfidenceScore is float64, 0-1 confidence level
	QualityConfidenceScore = "quality.confidence.score"
	// QualityConfidenceSource is string, source of confidence assessment
	QualityConfidenceSource = "quality.confidence.source"
	// QualityConfidenceMethod is string, confidence calculation method
	QualityConfidenceMethod = "quality.confidence.method"

	// QualityValidationStatus is string, validation status
	QualityValidationStatus = "quality.validation.status"
	// QualityValidationErrors is int, number of validation errors
	QualityValidationErrors = "quality.validation.errors"
	// QualityValidationWarnings is int, number of warnings
	QualityValidationWarnings = "quality.validation.warnings"

	// QualityAccuracyAbsolute is float64, absolute accuracy
	QualityAccuracyAbsolute = "quality.accuracy.absolute"
	// QualityAccuracyRelative is float64, relative accuracy percentage
	QualityAccuracyRelative = "quality.accuracy.relative"
	// QualityAccuracyPrecision is float64, measurement precision
	QualityAccuracyPrecision = "quality.accuracy.precision"
)

// Graph Domain Predicates
// These predicates describe relationships between entities in the semantic graph

const (
	// GraphRelContains represents hierarchical containment (parent contains child)
	// Example: A platform contains sensors, a system contains components
	GraphRelContains = "graph.rel.contains"

	// GraphRelReferences represents directional reference (subject references object)
	// Example: Documentation references specifications, code references APIs
	GraphRelReferences = "graph.rel.references"

	// GraphRelInfluences represents causal or impact relationships
	// Example: A decision influences implementation, configuration influences behavior
	GraphRelInfluences = "graph.rel.influences"

	// GraphRelCommunicates represents communication or interaction relationships
	// Example: Services communicate, components interact
	GraphRelCommunicates = "graph.rel.communicates"

	// GraphRelNear represents spatial proximity relationships
	// Example: Sensors near a location, entities in the same area
	GraphRelNear = "graph.rel.near"

	// GraphRelTriggeredBy represents event causation
	// Example: An alert triggered by a threshold, action triggered by event
	GraphRelTriggeredBy = "graph.rel.triggered_by"

	// GraphRelDependsOn represents dependency relationships
	// Example: Specifications depend on other specs, modules depend on libraries
	GraphRelDependsOn = "graph.rel.depends_on"

	// GraphRelImplements represents implementation relationships
	// Example: Code implements specifications, components implement interfaces
	GraphRelImplements = "graph.rel.implements"

	// GraphRelDiscusses represents discussion or commentary relationships
	// Example: GitHub discussions discuss issues, comments discuss topics
	GraphRelDiscusses = "graph.rel.discusses"

	// GraphRelSupersedes represents replacement or versioning relationships
	// Example: New decisions supersede old ones, v2 supersedes v1
	GraphRelSupersedes = "graph.rel.supersedes"

	// GraphRelBlockedBy represents blocking relationships
	// Example: An issue blocked by another issue, work blocked by dependencies
	GraphRelBlockedBy = "graph.rel.blocked_by"

	// GraphRelRelatedTo represents general association relationships
	// Example: Related documents, related entities without specific semantics
	GraphRelRelatedTo = "graph.rel.related_to"
)

// Hierarchy Domain Predicates
// These predicates describe relationships derived from 6-part entity ID structure.
// Entity IDs follow the pattern: org.platform.domain.system.type.instance
// Hierarchy inference automatically creates these edges at entity ingestion time.

const (
	// HierarchyDomainMember indicates entity belongs to a domain (3-part prefix match).
	// Subject is the entity, object is the domain prefix (e.g., "c360.logistics.sensor").
	// StandardIRI: skos:broader (entity is narrower than domain)
	// InverseOf: HierarchyDomainContains
	// Example: sensor-temp-001 hierarchy.domain.member c360.logistics.sensor
	HierarchyDomainMember = "hierarchy.domain.member"

	// HierarchyDomainContains indicates domain contains an entity (inverse of HierarchyDomainMember).
	// Subject is the domain, object is the entity.
	// StandardIRI: skos:narrower (domain contains narrower entities)
	// InverseOf: HierarchyDomainMember
	// Example: c360.logistics.sensor hierarchy.domain.contains sensor-temp-001
	HierarchyDomainContains = "hierarchy.domain.contains"

	// HierarchySystemMember indicates entity belongs to a system (4-part prefix match).
	// Subject is the entity, object is the system prefix (e.g., "c360.logistics.sensor.document").
	// StandardIRI: skos:broader (entity is narrower than system)
	// InverseOf: HierarchySystemContains
	// Example: sensor-temp-001 hierarchy.system.member c360.logistics.sensor.document
	HierarchySystemMember = "hierarchy.system.member"

	// HierarchySystemContains indicates system contains an entity (inverse of HierarchySystemMember).
	// Subject is the system, object is the entity.
	// StandardIRI: skos:narrower (system contains narrower entities)
	// InverseOf: HierarchySystemMember
	// Example: c360.logistics.sensor.document hierarchy.system.contains sensor-temp-001
	HierarchySystemContains = "hierarchy.system.contains"

	// HierarchyTypeSibling indicates entities share the same type (5-part prefix match).
	// Bidirectional relationship between entities with same type prefix.
	// StandardIRI: skos:related (symmetric relationship)
	// IsSymmetric: true (if A is sibling of B, B is sibling of A)
	// Example: sensor-temp-001 hierarchy.type.sibling sensor-temp-002
	HierarchyTypeSibling = "hierarchy.type.sibling"

	// HierarchyTypeMember indicates entity belongs to a type container (5-part prefix + .group).
	// Subject is the entity, object is the type container entity ID.
	// StandardIRI: skos:broader (entity is narrower than type container)
	// InverseOf: HierarchyTypeContains
	// Example: acme.iot.sensors.hvac.temperature.001 hierarchy.type.member acme.iot.sensors.hvac.temperature.group
	HierarchyTypeMember = "hierarchy.type.member"

	// HierarchyTypeContains indicates type container contains an entity (inverse of HierarchyTypeMember).
	// Subject is the type container, object is the entity.
	// StandardIRI: skos:narrower (container contains narrower entities)
	// InverseOf: HierarchyTypeMember
	// Example: acme.iot.sensors.hvac.temperature.group hierarchy.type.contains acme.iot.sensors.hvac.temperature.001
	HierarchyTypeContains = "hierarchy.type.contains"
)

// Indexing Eligibility (ADR-054)
// The producer's hint for which semantic substrates should index an entity.
// Single-valued, stamped once at entity creation by graph-ingest. Gates ONLY
// the embedding/community/search substrates — never the structural graph,
// which stays queryable/traversable regardless of profile.

// EntityIndexingProfile is the reserved single-valued predicate carrying an
// entity's indexing profile (one of the IndexingProfile* values below).
// Stamped at entity creation (replace-on-write); read by indexing consumers.
const EntityIndexingProfile = "entity.indexing.profile"

const (
	// IndexingProfileContent is the retrieval corpus (embed yes, community yes):
	// memory, documents, research facts, lessons, prose-bearing entities.
	IndexingProfileContent = "content"
	// IndexingProfileControl is low-cardinality lifecycle/harness/run machinery
	// (embed yes — the cardinality guard excludes high-fan-out control types —
	// community yes). The substrate stays semantically reachable. Default floor.
	IndexingProfileControl = "control"
	// IndexingProfileSignal is telemetry readings (embed only summarized
	// rollups, community aggregate only). Raw readings stay graph-visible, not
	// embedded.
	IndexingProfileSignal = "signal"
	// IndexingProfileTrace is mechanically generated audit/trace entities (embed
	// no, community no). Fully graph-visible and queryable, but never embedded.
	IndexingProfileTrace = "trace"
)

// IsValidIndexingProfile reports whether s is one of the four recognized
// indexing profiles. Empty and unrecognized strings are invalid (callers
// treat invalid as "absent" and fall through to the fallback floor rather
// than failing, per ADR-054 lenient Phase 1 semantics).
func IsValidIndexingProfile(s string) bool {
	switch s {
	case IndexingProfileContent, IndexingProfileControl, IndexingProfileSignal, IndexingProfileTrace:
		return true
	default:
		return false
	}
}

// NOTE: The predicates in this file are EXAMPLES for demonstration purposes.
// SemStreams is a framework - applications should define their own domain-specific
// vocabulary in their own packages and register predicates using the vocabulary registry.
//
// See vocabulary/examples/ for reference implementations of domain vocabularies.

// PredicateMetadata provides semantic information about each predicate
// This enables validation, type checking, and documentation generation
type PredicateMetadata struct {
	// Name is the predicate constant (e.g., "sensor.temperature.celsius")
	// Uses dotted notation for NATS stream query compatibility
	Name string

	// Description provides human-readable documentation
	Description string

	// DataType indicates the expected Go type for the object value
	DataType string

	// Units specifies the measurement units (if applicable)
	Units string

	// Range describes valid value ranges (if applicable)
	Range string

	// Domain identifies which domain owns this predicate
	Domain string

	// Category identifies the predicate category within the domain
	Category string

	// StandardIRI provides the W3C/RDF equivalent IRI for standards compliance (optional)
	// Examples: "http://www.w3.org/2002/07/owl#sameAs", "http://www.w3.org/2004/02/skos/core#prefLabel"
	// This enables RDF/JSON-LD export and semantic web interoperability while maintaining
	// dotted notation for NATS compatibility internally.
	// See vocabulary/standards.go for common constants.
	StandardIRI string

	// Alias semantics (for entity resolution and alias indexing)
	// IsAlias marks predicates that represent entity aliases
	IsAlias bool

	// AliasType defines the semantic meaning (identity, label, external, etc.)
	// Only meaningful when IsAlias is true. See AliasType documentation for
	// standard vocabulary mappings (OWL, SKOS, Schema.org).
	AliasType AliasType

	// AliasPriority defines conflict resolution order (lower number = higher priority)
	// Only meaningful when IsAlias is true
	AliasPriority int

	// InverseOf names the predicate that represents the inverse relationship.
	// Example: "hierarchy.type.member" has InverseOf = "hierarchy.type.contains"
	// When entity A → hierarchy.type.member → B, the inverse relationship is
	// B → hierarchy.type.contains → A.
	//
	// Important: Both predicates in an inverse pair should be registered with
	// their InverseOf pointing to each other. The registry stores relationships;
	// it does not auto-generate inverse triples at runtime.
	//
	// StandardIRI equivalent: owl:inverseOf
	InverseOf string

	// IsSymmetric indicates the predicate is its own inverse.
	// Example: "hierarchy.type.sibling" - if A is sibling of B, then B is sibling of A.
	// Symmetric predicates don't need InverseOf set; GetInversePredicate() returns
	// the predicate itself for symmetric cases.
	//
	// StandardIRI equivalent: owl:SymmetricProperty
	IsSymmetric bool

	// RuleOpaque marks the predicate as carrying free-form content that
	// rules MUST NOT predicate on. The rule-validator rejects rule
	// conditions whose `field` names a rule-opaque predicate at
	// config-load time.
	//
	// Used for agent-private observable state (ADR-036): the owning
	// agent is the sole writer and sole interpreter of content; rules
	// may match structural facts (counts, status enums, transitions)
	// but not the content itself. The asymmetric ownership pattern
	// avoids LLM-on-LLM checklist Goodhart by keeping content out of
	// the rule-engine's branching surface.
	RuleOpaque bool
}

// IsValidPredicate checks if a predicate follows the three-level dotted notation
// and matches the expected format: domain.category.property
func IsValidPredicate(predicate string) bool {
	if predicate == "" {
		return false
	}

	// Count dots to ensure three-level structure
	dotCount := 0
	for _, char := range predicate {
		if char == '.' {
			dotCount++
		}
	}

	// Must have exactly 2 dots for three levels
	return dotCount == 2
}
