package swe

// SWE namespace identifiers. Pinned to the namespace used by the
// CS API v1.0 bundle.
const (
	// Prefix is the SWE short token used when compacting IRIs.
	Prefix = "swe"

	// Namespace is the SWE Common v2.1 IRI stem.
	Namespace = "http://www.opengis.net/swe/2.0/"
)

// Typed-data primitives. Each is the IRI of an rdf:type that a SWE
// field declares; SOSA observations whose Result is a typed value
// carry one of these as their type IRI.
const (
	// Quantity — a numeric value with a unit of measure.
	Quantity = Namespace + "Quantity"

	// Category — a labelled discrete value drawn from a code list.
	Category = Namespace + "Category"

	// Time — a temporal instant or duration.
	Time = Namespace + "Time"

	// Count — a discrete numeric count (no unit of measure).
	Count = Namespace + "Count"

	// Boolean — a true/false value.
	Boolean = Namespace + "Boolean"

	// Text — a free-text string value.
	Text = Namespace + "Text"

	// QuantityRange — a [min, max] pair, both with the same UoM.
	QuantityRange = Namespace + "QuantityRange"

	// DataRecord — a heterogeneous record of named typed fields.
	DataRecord = Namespace + "DataRecord"

	// DataArray — an array of homogeneous typed elements.
	DataArray = Namespace + "DataArray"

	// DataChoice — a discriminated union of typed alternatives.
	DataChoice = Namespace + "DataChoice"

	// Vector — a 1-D set of values referencing the same axis frame.
	Vector = Namespace + "Vector"
)

// Structural role predicates. Use these as the predicate of a
// Triple when describing a SWE-shaped Result.
const (
	// Label is the human-readable name of a SWE field.
	Label = Namespace + "label"

	// Definition is the IRI of the controlled term defining the
	// field's semantic meaning (e.g. a QUDT or SWEET concept).
	Definition = Namespace + "definition"

	// UoM is the unit of measure (UCUM symbol or QUDT IRI).
	UoM = Namespace + "uom"

	// Value is the literal value of a SWE field.
	Value = Namespace + "value"

	// NilValue is the literal stand-in declared for "no reading"
	// — paired with a reason IRI in SWE-shaped Results.
	NilValue = Namespace + "nilValue"

	// ReferenceFrame is the IRI of the coordinate / temporal frame
	// the value is expressed in.
	ReferenceFrame = Namespace + "referenceFrame"
)
