package csapi

// CS API predicate IRIs.
const (
	// ProducedBy binds a Datastream to the entity ID of the System
	// (sensor or system of systems) that produces its Observations.
	// Inverse of a forthcoming `producesDatastream` predicate.
	ProducedBy = Namespace + "producedBy"

	// ResultTimeRange is the ISO 8601 time-interval representation
	// of the temporal bounds during which the Datastream produced
	// result values (clock time of the measurements). CS API §10.4.
	ResultTimeRange = Namespace + "resultTimeRange"

	// PhenomenonTimeRange is the ISO 8601 time-interval representation
	// of the temporal bounds of the observed phenomena. May differ
	// from ResultTimeRange for processed or back-dated observations.
	// CS API §10.4.
	PhenomenonTimeRange = Namespace + "phenomenonTimeRange"

	// ResultType discriminates the structure of the Datastream's
	// Observations — om:Measurement, om:Category, om:CountObservation,
	// etc. Consumers branch on this to decode the result payload.
	ResultType = Namespace + "resultType"
)
