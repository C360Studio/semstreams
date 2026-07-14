package document

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	// Auto-register content vocabulary when package is imported
	RegisterVocabulary()
}

// Predicate constants for the content domain.
// These follow the three-level dotted notation: domain.category.property
const (
	// Dublin Core metadata predicates (for triple metadata - NOT content body)
	// See: https://www.dublincore.org/specifications/dublin-core/dcmi-terms/
	PredicateDCTitle      = "dc.terms.title"      // Document title
	PredicateDCCreator    = "dc.terms.creator"    // Author or creator
	PredicateDCSubject    = "dc.terms.subject"    // Topic/category of content
	PredicateDCType       = "dc.terms.type"       // Nature/genre of content
	PredicateDCDate       = "dc.terms.date"       // Relevant date (creation, publication)
	PredicateDCIdentifier = "dc.terms.identifier" // Unique identifier
	PredicateDCFormat     = "dc.terms.format"     // File format or media type
	PredicateDCLanguage   = "dc.terms.language"   // Language of content

	// Core content predicates (legacy - prefer Dublin Core for metadata)
	// NOTE: content.text.body should NOT be used in triples for ContentStorable types.
	// Body content belongs in ObjectStore, not in triples.
	PredicateContentTitle       = "content.text.title"
	PredicateContentDescription = "content.text.description"
	PredicateContentBody        = "content.text.body" // DEPRECATED for ContentStorable
	PredicateContentSummary     = "content.text.summary"

	// Classification predicates
	PredicateContentType     = "content.classification.type"
	PredicateContentCategory = "content.classification.category"
	PredicateContentTag      = "content.classification.tag"

	// Maintenance-specific predicates
	PredicateMaintenanceTechnician = "maintenance.work.technician"
	PredicateMaintenanceDate       = "maintenance.work.completion-date"
	PredicateMaintenanceStatus     = "maintenance.work.status"

	// Observation-specific predicates
	PredicateObservationObserver   = "observation.record.observer"
	PredicateObservationSeverity   = "observation.record.severity"
	PredicateObservationObservedAt = "observation.record.observed-at"

	// Sensor document predicates (for rich-text sensor descriptions)
	PredicateSensorLocation = "sensor.document.location"
	PredicateSensorReading  = "sensor.document.reading"
	PredicateSensorUnit     = "sensor.document.unit"

	// Time predicates
	PredicateTimeCreated = "time.document.created"
	PredicateTimeUpdated = "time.document.updated"
)

// RegisterVocabulary registers all content domain predicates with the vocabulary system.
func RegisterVocabulary() {
	// Dublin Core metadata predicates (standard vocabulary for metadata)
	vocabulary.Register(PredicateDCTitle,
		vocabulary.WithDescription("Document title (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/title"),
	)

	vocabulary.Register(PredicateDCCreator,
		vocabulary.WithDescription("Document creator or author (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/creator"),
	)

	vocabulary.Register(PredicateDCSubject,
		vocabulary.WithDescription("Topic or category of content (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/subject"),
	)

	vocabulary.Register(PredicateDCType,
		vocabulary.WithDescription("Nature or genre of content (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/type"),
	)

	vocabulary.Register(PredicateDCDate,
		vocabulary.WithDescription("Relevant date - creation or publication (Dublin Core)"),
		vocabulary.WithDataType("timestamp"),
		vocabulary.WithIRI("http://purl.org/dc/terms/date"),
	)

	vocabulary.Register(PredicateDCIdentifier,
		vocabulary.WithDescription("Unique identifier for the resource (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/identifier"),
	)

	vocabulary.Register(PredicateDCFormat,
		vocabulary.WithDescription("File format or media type (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/format"),
	)

	vocabulary.Register(PredicateDCLanguage,
		vocabulary.WithDescription("Language of content (Dublin Core)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/language"),
	)

	// Core content predicates - text fields for semantic search (legacy)
	vocabulary.Register(PredicateContentTitle,
		vocabulary.WithDescription("Document or entity title"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateContentDescription,
		vocabulary.WithDescription("Document description - primary semantic search field"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateContentBody,
		vocabulary.WithDescription("Full text content of document"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateContentSummary,
		vocabulary.WithDescription("Brief summary of content"),
		vocabulary.WithDataType("string"),
	)

	// Classification predicates
	vocabulary.Register(PredicateContentType,
		vocabulary.WithDescription("Document type (document, maintenance, observation, sensor)"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateContentCategory,
		vocabulary.WithDescription("Category classification (safety, operations, etc.)"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateContentTag,
		vocabulary.WithDescription("Content tag for filtering and search"),
		vocabulary.WithDataType("string"),
	)

	// Maintenance predicates
	vocabulary.Register(PredicateMaintenanceTechnician,
		vocabulary.WithDescription("Technician who performed maintenance"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateMaintenanceDate,
		vocabulary.WithDescription("Date maintenance was completed"),
		vocabulary.WithDataType("timestamp"),
	)

	vocabulary.Register(PredicateMaintenanceStatus,
		vocabulary.WithDescription("Maintenance work status"),
		vocabulary.WithDataType("string"),
	)

	// Observation predicates
	vocabulary.Register(PredicateObservationObserver,
		vocabulary.WithDescription("Person who made the observation"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateObservationSeverity,
		vocabulary.WithDescription("Severity level of observation (low, medium, high, critical)"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateObservationObservedAt,
		vocabulary.WithDescription("Timestamp when observation was made"),
		vocabulary.WithDataType("timestamp"),
	)

	// Sensor document predicates
	vocabulary.Register(PredicateSensorLocation,
		vocabulary.WithDescription("Physical location description of sensor"),
		vocabulary.WithDataType("string"),
	)

	vocabulary.Register(PredicateSensorReading,
		vocabulary.WithDescription("Current sensor reading value"),
		vocabulary.WithDataType("float64"),
	)

	vocabulary.Register(PredicateSensorUnit,
		vocabulary.WithDescription("Unit of measurement for sensor"),
		vocabulary.WithDataType("string"),
	)

	// Time predicates
	vocabulary.Register(PredicateTimeCreated,
		vocabulary.WithDescription("Document creation timestamp"),
		vocabulary.WithDataType("timestamp"),
	)

	vocabulary.Register(PredicateTimeUpdated,
		vocabulary.WithDescription("Document last update timestamp"),
		vocabulary.WithDataType("timestamp"),
	)
}
