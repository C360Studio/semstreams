package csapi

import (
	"fmt"

	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/export"
)

// Register binds the csapi: prefix into the export prefix table AND
// registers each dotted predicate in the global vocabulary registry
// with its IRI mapping for boundary export. Importing the package
// triggers this from init(); idempotent.
func Register() error {
	if err := export.Register(Prefix, Namespace); err != nil {
		return fmt.Errorf("vocabulary/csapi: register csapi prefix: %w", err)
	}
	registerDatastreamPredicates()
	registerControlStreamPredicates()
	registerCommandPredicates()
	registerSystemEventPredicates()
	registerArtifactPredicates()
	return nil
}

// registerDatastreamPredicates binds the Datastream-domain dotted
// predicates to their canonical CS API IRIs. Internal triples carry
// the dotted form; JSON-LD/RDF export resolves via the IRI mapping.
func registerDatastreamPredicates() {
	vocabulary.Register(ProducedBy,
		vocabulary.WithDescription("Datastream → producing System entity ID. CS API §10. Inverse of forthcoming producesDatastream."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ProducedByIRI))

	vocabulary.Register(ResultTimeRange,
		vocabulary.WithDescription("ISO 8601 time-interval bounds for when result values were produced (clock time). CS API §10.4."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ResultTimeRangeIRI))

	vocabulary.Register(PhenomenonTimeRange,
		vocabulary.WithDescription("ISO 8601 time-interval bounds for the observed phenomena (may differ from ResultTimeRange for processed/back-dated data). CS API §10.4."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(PhenomenonTimeRangeIRI))

	vocabulary.Register(ResultType,
		vocabulary.WithDescription("Discriminates Observation structure (om:Measurement, om:Category, om:CountObservation, etc). Consumers branch on this to decode the result payload."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ResultTypeIRI))

	vocabulary.Register(HasResultSchema,
		vocabulary.WithDescription("Datastream → SWESchemaDocument artifact entity ID. Reusable across N Datastreams sharing a schema; the artifact entity holds the canonical schema content. gh#171."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(HasResultSchemaIRI))
}

// registerControlStreamPredicates binds the ControlStream-domain
// dotted predicates to their CS API IRIs.
func registerControlStreamPredicates() {
	vocabulary.Register(ControlsSystem,
		vocabulary.WithDescription("ControlStream → target System entity ID (the System receiving Commands). CS API v1.0 Part 2 §14."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ControlsSystemIRI))

	vocabulary.Register(HasCommandSchema,
		vocabulary.WithDescription("ControlStream → SWESchemaDocument artifact entity ID describing accepted command structure. Same reuse model as HasResultSchema. gh#171."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(HasCommandSchemaIRI))
}

// registerCommandPredicates binds Command-domain dotted predicates.
func registerCommandPredicates() {
	vocabulary.Register(PartOfControlStream,
		vocabulary.WithDescription("Command → owning ControlStream entity ID. CS API v1.0 Part 2 §15."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(PartOfControlStreamIRI))
}

// registerSystemEventPredicates binds SystemEvent-domain dotted predicates.
func registerSystemEventPredicates() {
	vocabulary.Register(EventForSystem,
		vocabulary.WithDescription("SystemEvent → subject System entity ID. CS API v1.0 Part 2 §16."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(EventForSystemIRI))
}

// registerArtifactPredicates binds the typed-artifact-entity dotted
// predicates (gh#171 pattern). HasSource crosses System and Datastream
// resources; HasResultSchema/HasCommandSchema are domain-scoped above.
func registerArtifactPredicates() {
	vocabulary.Register(HasSource,
		vocabulary.WithDescription("System or Datastream → SensorMLDocument artifact entity ID. Artifact has its own StorageRef pointing to the SensorML XML/JSON in ObjectStore. gh#171."),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(HasSourceIRI))
}

func init() {
	if err := Register(); err != nil {
		panic(err)
	}
}
