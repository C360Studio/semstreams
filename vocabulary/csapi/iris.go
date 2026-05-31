package csapi

import (
	"maps"
	"strings"
)

// CS API namespace identifiers. Pinned to the spec-rooted stem used
// by OGC API Connected Systems v1.0. The spec is a working draft;
// when canonical IRIs publish, this constant gets a one-shot swap.
const (
	// Prefix is the CS API short token used when compacting IRIs.
	Prefix = "csapi"

	// Namespace is the CS API v1.0 IRI stem.
	Namespace = "http://www.opengis.net/spec/ogcapi-connectedsystems-1/1.0/"
)

// CS API class IRIs. Use as the object of an rdf:type triple, or
// anywhere a Connected-Systems-aware encoder references the type.
const (
	// Datastream — a stream of Observations produced by one System
	// (sensor or system of systems) for one ObservableProperty, with
	// declared temporal bounds (PhenomenonTimeRange,
	// ResultTimeRange) and a result-type discriminator (ResultType).
	// CS API v1.0 §10.
	Datastream = Namespace + "Datastream"

	// ControlStream — a stream of Commands sent to one System
	// (typically an Actuator-bearing platform) for one
	// ActuatableProperty. CS API v1.0 Part 2 §14. Draft surface;
	// IRI swaps to the canonical spec form once published.
	ControlStream = Namespace + "ControlStream"

	// Command — an individual instruction issued through a
	// ControlStream targeting an ActuatableProperty. CS API v1.0
	// Part 2 §15. Draft surface; IRI may swap when the spec
	// publishes.
	Command = Namespace + "Command"

	// SystemEvent — a discrete notification about a System
	// (deployment lifecycle, configuration change, alert). Used
	// by /systems/{id}/events. CS API v1.0 Part 2 §16. Draft
	// surface; IRI may swap when the spec publishes.
	SystemEvent = Namespace + "SystemEvent"

	// SensorMLDocument — the typed artifact class for a SensorML
	// XML/JSON document carrying lossless source describing a
	// System or Procedure. Stored as a first-class artifact entity
	// (per gh#171 Pattern 2): the artifact carries its own 6-part
	// EntityID + a singular StorageRef pointing to the document in
	// ObjectStore. Datastreams and Systems reference it via the
	// HasSource predicate. Lets parent resources stay graph-shaped
	// while keeping the heavy document payload addressable via
	// NATS ObjectStore.
	SensorMLDocument = Namespace + "SensorMLDocument"

	// SWESchemaDocument — the typed artifact class for a SWE Common
	// DataRecord (or higher-arity schema) used by a Datastream or
	// ControlStream. Stored as a first-class artifact entity with
	// its own StorageRef. Datastreams reference it via
	// HasResultSchema; ControlStreams reference it via
	// HasCommandSchema. The reuse case (one schema across N
	// streams) is what makes the typed-artifact pattern preferable
	// to inline embedding on each parent.
	SWESchemaDocument = Namespace + "SWESchemaDocument"
)

// iris is the canonical set of IRIs this package surfaces, indexed
// by their compact form. Adding a constant in iris.go or
// predicates.go requires adding it here too; the contract test in
// iris_test.go fails loud if these drift apart.
var iris = map[string]string{
	// Classes
	Prefix + ":Datastream":        Datastream,
	Prefix + ":ControlStream":     ControlStream,
	Prefix + ":Command":           Command,
	Prefix + ":SystemEvent":       SystemEvent,
	Prefix + ":SensorMLDocument":  SensorMLDocument,
	Prefix + ":SWESchemaDocument": SWESchemaDocument,

	// Predicates
	Prefix + ":producedBy":          ProducedBy,
	Prefix + ":resultTimeRange":     ResultTimeRange,
	Prefix + ":phenomenonTimeRange": PhenomenonTimeRange,
	Prefix + ":resultType":          ResultType,
	Prefix + ":controlsSystem":      ControlsSystem,
	Prefix + ":partOfControlStream": PartOfControlStream,
	Prefix + ":eventForSystem":      EventForSystem,
	Prefix + ":hasSource":           HasSource,
	Prefix + ":hasResultSchema":     HasResultSchema,
	Prefix + ":hasCommandSchema":    HasCommandSchema,
}

var reverseIRIs = func() map[string]string {
	m := make(map[string]string, len(iris))
	for compact, iri := range iris {
		m[iri] = compact
	}
	return m
}()

// IRIs returns a copy of the full set of compact → IRI mappings
// covered by this package.
func IRIs() map[string]string {
	out := make(map[string]string, len(iris))
	maps.Copy(out, iris)
	return out
}

// IsKnown reports whether the given IRI is part of this package's
// coverage.
func IsKnown(iri string) bool {
	_, ok := reverseIRIs[iri]
	return ok
}

// LocalName returns the local part of a CS API IRI, or the empty
// string if the IRI is not in the CS API namespace.
func LocalName(iri string) string {
	if strings.HasPrefix(iri, Namespace) {
		return iri[len(Namespace):]
	}
	return ""
}
