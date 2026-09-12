package iotsensor

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	// Auto-register IoT sensor vocabulary when package is imported
	RegisterVocabulary()
}

// Predicate constants for the IoT sensor domain.
// These follow the three-level dotted notation: domain.category.property
const (
	// Sensor measurement predicates (unit-specific)
	PredicateMeasurementCelsius    = "sensor.measurement.celsius"
	PredicateMeasurementFahrenheit = "sensor.measurement.fahrenheit"
	PredicateMeasurementPercent    = "sensor.measurement.percent"
	PredicateMeasurementHPA        = "sensor.measurement.hpa"
	PredicateMeasurementPSI        = "sensor.measurement.psi"

	// Sensor classification predicates
	PredicateClassificationType    = "sensor.classification.type"
	PredicateClassificationAmbient = "sensor.classification.ambient"

	// Geo location predicates
	PredicateLocationZone      = "geo.location.zone"
	PredicateLocationLatitude  = "geo.location.latitude"
	PredicateLocationLongitude = "geo.location.longitude"

	// Time observation predicates
	PredicateObservationRecorded = "time.observation.recorded"
	PredicateObservationReceived = "time.observation.received"

	// Facility zone predicates
	PredicateZoneName = "facility.zone.name"
	PredicateZoneType = "facility.zone.type"

	// Sensor identity predicates (for ALIAS_INDEX)
	PredicateSensorSerial = "iot.sensor.serial"

	// Alert predicates emitted by the bundled reference rules.
	PredicateAlertStateActive = "alert.state.active"
)

// RegisterVocabulary registers all IoT sensor domain predicates with the vocabulary
// system. This should be called during application initialization.
//
// Example usage:
//
//	func init() {
//	    iotsensor.RegisterVocabulary()
//	}
func RegisterVocabulary() {
	// Sensor measurement predicates
	vocabulary.Register(PredicateMeasurementCelsius,
		vocabulary.WithDescription("Temperature reading in Celsius"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("celsius"),
	)

	vocabulary.Register(PredicateMeasurementFahrenheit,
		vocabulary.WithDescription("Temperature reading in Fahrenheit"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("fahrenheit"),
	)

	vocabulary.Register(PredicateMeasurementPercent,
		vocabulary.WithDescription("Percentage measurement (e.g., humidity)"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("percent"),
		vocabulary.WithRange("0-100"),
	)

	vocabulary.Register(PredicateMeasurementHPA,
		vocabulary.WithDescription("Pressure reading in hectopascals"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("hPa"),
	)

	vocabulary.Register(PredicateMeasurementPSI,
		vocabulary.WithDescription("Pressure reading in pounds per square inch"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("psi"),
	)

	// Sensor classification predicates
	vocabulary.Register(PredicateClassificationType,
		vocabulary.WithDescription("Sensor type classification"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	vocabulary.Register(PredicateClassificationAmbient,
		vocabulary.WithDescription("Whether this is an ambient measurement"),
		vocabulary.WithDataType(vocabulary.DataTypeBool),
	)

	// Geo location predicates
	vocabulary.Register(PredicateLocationZone,
		vocabulary.WithDescription("Reference to zone entity where sensor is located"),
		vocabulary.WithDataType(vocabulary.DataTypeEntityID),
	)

	vocabulary.Register(PredicateLocationLatitude,
		vocabulary.WithDescription("GPS latitude coordinate"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithRange("-90 to 90"),
	)

	vocabulary.Register(PredicateLocationLongitude,
		vocabulary.WithDescription("GPS longitude coordinate"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithRange("-180 to 180"),
	)

	// Time observation predicates
	vocabulary.Register(PredicateObservationRecorded,
		vocabulary.WithDescription("Timestamp when observation was recorded"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime),
	)

	vocabulary.Register(PredicateObservationReceived,
		vocabulary.WithDescription("Timestamp when observation was received by system"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime),
	)

	// Facility zone predicates
	vocabulary.Register(PredicateZoneName,
		vocabulary.WithDescription("Human-readable name of the zone"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	vocabulary.Register(PredicateZoneType,
		vocabulary.WithDescription("Type classification of the zone"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	// Sensor identity predicates - registered as alias for ALIAS_INDEX
	vocabulary.Register(PredicateSensorSerial,
		vocabulary.WithDescription("Manufacturer serial number for sensor identification"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithAlias(vocabulary.AliasTypeExternal, 0), // Resolvable external ID
		vocabulary.WithIRI(vocabulary.DcIdentifier))

	// Reference rule outputs. This belongs to the example vocabulary rather
	// than framework core: applications define and register their own alert
	// semantics alongside the rules that emit them.
	vocabulary.Register(PredicateAlertStateActive,
		vocabulary.WithDescription("Active alert code emitted by the bundled IoT reference rules"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)
}
