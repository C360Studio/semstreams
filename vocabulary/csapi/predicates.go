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

	// ControlsSystem binds a ControlStream to the entity ID of the
	// System it targets with Commands. Inverse counterpart to a
	// forthcoming `hasControlStream`. CS API v1.0 Part 2 §14.
	ControlsSystem = Namespace + "controlsSystem"

	// PartOfControlStream binds a Command to the entity ID of the
	// ControlStream it was issued through. CS API v1.0 Part 2 §15.
	PartOfControlStream = Namespace + "partOfControlStream"

	// EventForSystem binds a SystemEvent to the entity ID of the
	// System the event is about. CS API v1.0 Part 2 §16.
	EventForSystem = Namespace + "eventForSystem"

	// HasSource binds a System or Datastream to the entity ID of
	// the SensorMLDocument artifact that carries its lossless
	// source representation. The artifact is a first-class entity
	// with its own StorageRef pointing to the SensorML XML/JSON in
	// ObjectStore. Lets parent resources stay graph-shaped
	// (queryable facts) while the heavy document payload is fetched
	// on demand via the ObjectStore reference. gh#171.
	HasSource = Namespace + "hasSource"

	// HasResultSchema binds a Datastream to the entity ID of the
	// SWESchemaDocument artifact describing its observation result
	// structure. Reusable across N Datastreams that share a schema —
	// the artifact entity holds the canonical schema, the
	// Datastreams reference it. gh#171.
	HasResultSchema = Namespace + "hasResultSchema"

	// HasCommandSchema binds a ControlStream to the entity ID of
	// the SWESchemaDocument artifact describing the structure of
	// commands it accepts. Same reuse model as HasResultSchema.
	// gh#171.
	HasCommandSchema = Namespace + "hasCommandSchema"
)
