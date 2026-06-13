package sensorml

import (
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/sosa"
)

// Predicate name constants for the dotted-name SemStreams
// convention. Each is registered to its SOSA/SSN IRI in init()
// so that RDF/Turtle export through vocabulary/export emits the
// compacted sosa:/ssn: forms automatically.
const (
	// PredType is the RDF type assertion (sosa class IRI).
	PredType = "sensorml.process.type"

	// PredLabel pairs to dc:title for the SensorML label field.
	PredLabel = "sensorml.process.label"

	// PredDescription pairs to dc:description.
	PredDescription = "sensorml.process.description"

	// PredDefinition pairs to the entity's SOSA-aligned class IRI.
	PredDefinition = "sensorml.process.definition"

	// PredHosts maps PhysicalSystem → child PhysicalComponent
	// (sosa:hosts).
	PredHosts = "sensorml.system.hosts"

	// PredIsHostedBy is the inverse of PredHosts.
	PredIsHostedBy = "sensorml.component.isHostedBy"

	// PredHasSubSystem maps an AggregateProcess to its child
	// processes (ssn:hasSubSystem).
	PredHasSubSystem = "sensorml.process.hasSubSystem"

	// PredUsedProcedure maps a PhysicalComponent / SimpleProcess
	// to the method reference (sosa:usedProcedure).
	PredUsedProcedure = "sensorml.process.usedProcedure"

	// PredAttachedTo records the SensorML attachedTo reference —
	// "I am physically mounted on this thing". Maps to
	// sosa:isHostedBy (the inverse of sosa:hosts), NOT
	// ssn:hasDeployment — deployment is a temporal-spatial-
	// purpose context, while attachedTo is a hardware mounting
	// link. Operators modeling explicit deployment contexts
	// should introduce a separate predicate targeting
	// ssn:hasDeployment when that capability lands.
	PredAttachedTo = "sensorml.process.attachedTo"

	// PredIdentifierValue carries a flat identifier value
	// (typically a serial number, registration ID, or callsign).
	// Maps to skos:notation.
	PredIdentifierValue = "sensorml.identifier.value"

	// PredCapabilityValue carries a capability-list entry's value
	// (operating range, performance bound).
	PredCapabilityValue = "sensorml.capability.value"

	// PredCharacteristicValue carries a characteristic-list
	// entry's value (physical property of the device).
	PredCharacteristicValue = "sensorml.characteristic.value"

	// PredPosition carries the SensorML §7.1.6 position field
	// verbatim as a GeoJSON-shaped string (Point / Polygon /
	// LineString). Maps to sosa:hasLocation. The Object value is
	// the raw GeoJSON-shaped JSON text (string) — consumers parse
	// it as GeoJSON when they need typed geometry. See issue #114.
	PredPosition = "sensorml.process.position"

	// PredUniqueID carries the SensorML §7.1.3 uniqueId — the
	// producer-assigned globally-unique identifier (URN, UUID,
	// vendor-scoped string). Distinct from the SemStreams-assigned
	// 6-part Entity ID, which is the triple Subject; the uid
	// preserves the original client identifier so consumers can
	// correlate a POSTed resource against subsequent reads. Maps
	// to dc:identifier. See issue #115.
	PredUniqueID = "sensorml.process.uid"
)

// init registers every dotted predicate this package emits with
// the global predicate registry, binding each to its SOSA / SSN
// / SKOS IRI. Importing the package runs this; downstream RDF
// export gets compacted sosa:/ssn: forms without extra wiring.
//
// Pre-existing IRI constants in vocabulary/standards.go cover the
// SKOS / DC bindings; vocabulary/sosa covers SOSA + SSN.
func init() {
	vocabulary.Register(PredType,
		vocabulary.WithIRI("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"))
	vocabulary.Register(PredLabel, vocabulary.WithIRI(vocabulary.DcTitle))
	vocabulary.Register(PredDescription,
		vocabulary.WithIRI("http://purl.org/dc/terms/description"))
	vocabulary.Register(PredDefinition,
		vocabulary.WithIRI("http://www.w3.org/2000/01/rdf-schema#isDefinedBy"))

	// PredHosts/PredIsHostedBy are mutual inverses (the doc-comment above has
	// asserted this since the start; the registration now honors it — ADR-056
	// Decision 4). The registered inverse is the Backfill recoverability floor for
	// the foreign isHostedBy edge and lets it pass the inverse-gate should it ever
	// be declared Conditional/Backfill. (The no-birth sensorml child's correct
	// edge-mode is NoBirthStub, which needs no inverse; the inverse is correct and
	// free regardless.)
	vocabulary.Register(PredHosts,
		vocabulary.WithIRI(sosa.Hosts),
		vocabulary.WithInverseOf(PredIsHostedBy))
	vocabulary.Register(PredIsHostedBy,
		vocabulary.WithIRI(sosa.IsHostedBy),
		vocabulary.WithInverseOf(PredHosts))
	vocabulary.Register(PredHasSubSystem, vocabulary.WithIRI(sosa.SSNHasSubSystem))
	vocabulary.Register(PredUsedProcedure, vocabulary.WithIRI(sosa.UsedProcedure))
	vocabulary.Register(PredAttachedTo, vocabulary.WithIRI(sosa.IsHostedBy))

	vocabulary.Register(PredIdentifierValue, vocabulary.WithIRI(vocabulary.SkosNotation))
	// Capability and characteristic both bind to ssn:hasProperty:
	// SOSA/SSN does not carry top-level predicates that split
	// "performance bound" (capability) from "physical property"
	// (characteristic). The discrimination survives via the
	// SensorML-side Term.Definition field, which downstream
	// rule / SPARQL consumers can use to refine.
	vocabulary.Register(PredCapabilityValue,
		vocabulary.WithIRI("http://www.w3.org/ns/ssn/hasProperty"))
	vocabulary.Register(PredCharacteristicValue,
		vocabulary.WithIRI("http://www.w3.org/ns/ssn/hasProperty"))
	vocabulary.Register(PredPosition, vocabulary.WithIRI(sosa.HasLocation))
	vocabulary.Register(PredUniqueID, vocabulary.WithIRI(vocabulary.DcIdentifier))
}

// processClassIRI returns the SOSA class IRI that best matches
// the given SensorML process type. The IRI is what rdf:type
// triples point at — PhysicalSystem → ssn:System, PhysicalComponent
// → sosa:Sensor (default; deployers can override via the SensorML
// definition field), SimpleProcess / AggregateProcess →
// sosa:Procedure.
func processClassIRI(process Process) string {
	switch process.(type) {
	case *PhysicalSystem:
		return sosa.SSNSystem
	case *PhysicalComponent:
		return sosa.Sensor
	case *SimpleProcess, *AggregateProcess:
		return sosa.Procedure
	default:
		return ""
	}
}
