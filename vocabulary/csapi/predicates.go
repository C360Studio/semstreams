package csapi

// CS API predicate constants — internal dotted-notation form.
//
// Per the framework convention (vocabulary/predicates.go), values
// flowing through message.Triple.Predicate use three-level dotted
// notation (`domain.category.property`). IRIs are reserved for
// export/import boundary serialization. The constants below carry
// the dotted form; the `*IRI` constants in this file carry the IRI
// form for JSON-LD / RDF export. Both are exported because callers
// have legitimate use cases for each:
//
//   - Triples: `triple := message.Triple{Predicate: csapi.HasSource, ...}`
//   - JSON-LD export: `iri := csapi.HasSourceIRI`
//
// The mapping internal → IRI is registered in register.go's init via
// vocabulary.Register(..., WithIRI(...)) so the framework's export
// pipeline can resolve either direction.
//
// gh#171 added the artifact-relationship predicates (HasSource,
// HasResultSchema, HasCommandSchema). gh#182 audited the full set
// and split each into the dual {dotted, IRI} form.
const (
	// ProducedBy binds a Datastream to the entity ID of the System
	// (sensor or system of systems) that produces its Observations.
	// Inverse of a forthcoming `producesDatastream` predicate.
	// CS API §10.
	ProducedBy = "csapi.datastream.produced-by"

	// ResultTimeRange is the ISO 8601 time-interval representation
	// of the temporal bounds during which the Datastream produced
	// result values (clock time of the measurements). CS API §10.4.
	ResultTimeRange = "csapi.datastream.result-time-range"

	// PhenomenonTimeRange is the ISO 8601 time-interval representation
	// of the temporal bounds of the observed phenomena. May differ
	// from ResultTimeRange for processed or back-dated observations.
	// CS API §10.4.
	PhenomenonTimeRange = "csapi.datastream.phenomenon-time-range"

	// ResultType discriminates the structure of the Datastream's
	// Observations — om:Measurement, om:Category, om:CountObservation,
	// etc. Consumers branch on this to decode the result payload.
	ResultType = "csapi.datastream.result-type"

	// ControlsSystem binds a ControlStream to the entity ID of the
	// System it targets with Commands. Inverse counterpart to a
	// forthcoming `hasControlStream`. CS API v1.0 Part 2 §14.
	ControlsSystem = "csapi.controlstream.controls-system"

	// PartOfControlStream binds a Command to the entity ID of the
	// ControlStream it was issued through. CS API v1.0 Part 2 §15.
	PartOfControlStream = "csapi.command.part-of-control-stream"

	// EventForSystem binds a SystemEvent to the entity ID of the
	// System the event is about. CS API v1.0 Part 2 §16.
	EventForSystem = "csapi.systemevent.for-system"

	// HasSource binds a System or Datastream to the entity ID of
	// the SensorMLDocument artifact that carries its lossless
	// source representation. The artifact is a first-class entity
	// with its own StorageRef pointing to the SensorML XML/JSON in
	// ObjectStore. Lets parent resources stay graph-shaped
	// (queryable facts) while the heavy document payload is fetched
	// on demand via the ObjectStore reference. gh#171.
	HasSource = "csapi.artifact.source"

	// HasResultSchema binds a Datastream to the entity ID of the
	// SWESchemaDocument artifact describing its observation result
	// structure. Reusable across N Datastreams that share a schema —
	// the artifact entity holds the canonical schema, the
	// Datastreams reference it. gh#171.
	HasResultSchema = "csapi.datastream.result-schema"

	// HasCommandSchema binds a ControlStream to the entity ID of
	// the SWESchemaDocument artifact describing the structure of
	// commands it accepts. Same reuse model as HasResultSchema.
	// gh#171.
	HasCommandSchema = "csapi.controlstream.command-schema"
)

// CS API predicate IRIs — export/import boundary form.
//
// Use these for JSON-LD export, RDF serialization, and any wire
// shape that addresses predicates by their canonical OGC IRI. For
// `message.Triple.Predicate` values inside the framework, use the
// dotted constants above; the framework's export pipeline resolves
// dotted → IRI via the registry binding in register.go.
//
// IRI strings are spec-rooted at vocabulary/csapi.Namespace. The CS
// API is still a working draft; when canonical IRIs publish, only
// these constants need a one-shot swap (the dotted predicates above
// are framework-internal and don't change).
const (
	// ProducedByIRI — CS API IRI for csapi.ProducedBy.
	ProducedByIRI = Namespace + "producedBy"

	// ResultTimeRangeIRI — CS API IRI for csapi.ResultTimeRange.
	ResultTimeRangeIRI = Namespace + "resultTimeRange"

	// PhenomenonTimeRangeIRI — CS API IRI for csapi.PhenomenonTimeRange.
	PhenomenonTimeRangeIRI = Namespace + "phenomenonTimeRange"

	// ResultTypeIRI — CS API IRI for csapi.ResultType.
	ResultTypeIRI = Namespace + "resultType"

	// ControlsSystemIRI — CS API IRI for csapi.ControlsSystem.
	ControlsSystemIRI = Namespace + "controlsSystem"

	// PartOfControlStreamIRI — CS API IRI for csapi.PartOfControlStream.
	PartOfControlStreamIRI = Namespace + "partOfControlStream"

	// EventForSystemIRI — CS API IRI for csapi.EventForSystem.
	EventForSystemIRI = Namespace + "eventForSystem"

	// HasSourceIRI — CS API IRI for csapi.HasSource.
	HasSourceIRI = Namespace + "hasSource"

	// HasResultSchemaIRI — CS API IRI for csapi.HasResultSchema.
	HasResultSchemaIRI = Namespace + "hasResultSchema"

	// HasCommandSchemaIRI — CS API IRI for csapi.HasCommandSchema.
	HasCommandSchemaIRI = Namespace + "hasCommandSchema"
)
