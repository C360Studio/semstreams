package oms

// OMS namespace identifiers. Pinned to the namespace used by the
// CS API v1.0 OMS bundle.
const (
	// Prefix is the OMS short token used when compacting IRIs.
	Prefix = "oms"

	// Namespace is the OMS v3.0 IRI stem.
	Namespace = "http://www.opengis.net/oms/3.0/"
)

// OMS class IRIs. Use these as the object of an rdf:type triple,
// or anywhere an Observation document references one of the
// core OMS concepts. SOSA defines parallel terms in sosa:; OMS
// preserves them for backward compatibility with O&M 2.0 consumers
// and bundles a small set of OMS-specific extensions.
const (
	// Observation — an act associating a result with a procedure,
	// feature of interest, and observable property. Parallel to
	// sosa:Observation; downstream encoders pick the IRI that
	// matches the consuming endpoint's content negotiation.
	Observation = Namespace + "Observation"

	// ObservableProperty — the quality the observation measured.
	// Parallel to sosa:ObservableProperty.
	ObservableProperty = Namespace + "ObservableProperty"

	// Procedure — the workflow / algorithm / instrument that
	// produced the result.
	Procedure = Namespace + "Procedure"

	// FeatureOfInterest — the real-world thing the observation is
	// about.
	FeatureOfInterest = Namespace + "FeatureOfInterest"

	// Result — the structured value produced by an Observation.
	Result = Namespace + "Result"
)

// OMS predicate IRIs.
const (
	// ResultTime is the time the observation result was produced
	// (clock time of the measurement, not the phenomenon).
	ResultTime = Namespace + "resultTime"

	// PhenomenonTime is the time the observed phenomenon occurred
	// — may differ from ResultTime for processed or back-dated
	// observations.
	PhenomenonTime = Namespace + "phenomenonTime"

	// HasResult attaches a structured Result entity to an
	// Observation.
	HasResult = Namespace + "hasResult"

	// HasFeatureOfInterest binds an Observation to the
	// FeatureOfInterest it is about.
	HasFeatureOfInterest = Namespace + "hasFeatureOfInterest"

	// ObservedProperty binds an Observation to the
	// ObservableProperty it measured.
	ObservedProperty = Namespace + "observedProperty"

	// UsedProcedure binds an Observation to the Procedure used.
	UsedProcedure = Namespace + "usedProcedure"
)
