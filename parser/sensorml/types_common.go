package sensorml

// Type discriminator strings per OGC SensorML JSON encoding
// (CS API v1.0 bundle).
const (
	TypePhysicalSystem    = "PhysicalSystem"
	TypePhysicalComponent = "PhysicalComponent"
	TypeSimpleProcess     = "SimpleProcess"
	TypeAggregateProcess  = "AggregateProcess"
)

// Reference is a SensorML JSON typed cross-reference. It carries
// at minimum an href (the target IRI / URI); other fields are
// optional and used for display or content-negotiation hints.
//
// References show up everywhere — procedure links, typeOf,
// featureOfInterest, configuration overlays. A bare string in
// JSON is also accepted by UnmarshalJSON for callers that
// emit the IRI directly without the full envelope.
type Reference struct {
	Href  string `json:"href,omitempty"`
	Title string `json:"title,omitempty"`
	Role  string `json:"role,omitempty"`
}

// Term is the SensorML "Term" structure used for identifiers,
// classifiers, characteristics, and capabilities. It carries
// a definition IRI (the semantic kind of the term), a human-
// readable label, and the value the term holds.
//
// SensorML 2.x JSON encoding represents Term values as a single
// JSON object even when the underlying SWE Common shape would be
// a typed AbstractDataComponent. We surface only the load-bearing
// fields; the SWE typing is preserved as a raw IRI in
// SweTypeOf so downstream consumers can resolve via
// [vocabulary/swe] when needed.
type Term struct {
	Definition string `json:"definition,omitempty"`
	Label      string `json:"label,omitempty"`
	Value      any    `json:"value,omitempty"`
	UoM        string `json:"uom,omitempty"`
	SweTypeOf  string `json:"sweType,omitempty"`
}

// IdentifierList groups identifier Terms. SensorML uses lists
// instead of bare arrays so the list itself can carry a label /
// definition; we collapse to a flat slice for the common case.
type IdentifierList []Term

// ClassifierList groups classifier Terms (taxonomies, types).
type ClassifierList []Term

// CharacteristicList groups characteristic Terms (physical
// properties of the procedure / device).
type CharacteristicList []Term

// CapabilityList groups capability Terms (operating range,
// performance bounds).
type CapabilityList []Term

// DataComponent is the SensorML reference to a SWE Common typed
// field used as an input, output, or parameter slot. We intentionally
// model only the cross-cutting fields rather than the full SWE
// Common type hierarchy — the SWE IRI roster ships in
// [vocabulary/swe] and the parser's job is to surface the type
// reference, not to revalidate the SWE Common payload shape.
type DataComponent struct {
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	Definition string `json:"definition,omitempty"`
	Label      string `json:"label,omitempty"`
	UoM        string `json:"uom,omitempty"`
}

// DataComponentList groups DataComponents under a SensorML
// inputs / outputs / parameters slot.
type DataComponentList []DataComponent

// Connection wires together two PhysicalSystem components via
// their data ports. The source / destination fields are
// dotted-path references into the embedded components' data
// dictionaries (e.g. "battery/output/voltage" → "controller/input/sensorReading").
type Connection struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// ConnectionList groups Connection entries inside a
// PhysicalSystem.
type ConnectionList []Connection
