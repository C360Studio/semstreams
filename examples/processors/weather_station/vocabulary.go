package weatherstation

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	// Auto-register vocabulary when package is imported
	RegisterVocabulary()
}

// Predicate constants for the weather station domain.
// These follow the three-level dotted notation: domain.category.property
const (
	// Weather measurement predicates (unit-specific)
	PredicateWeatherTempCelsius = "weather.temperature.celsius"
	PredicateWeatherWindKph     = "weather.wind.kph"
	PredicateWeatherHumidity    = "weather.humidity.percent"

	// Weather classification predicates
	PredicateWeatherCondition = "weather.classification.condition"

	// Location predicates
	PredicateLocationCity    = "geo.location.city"
	PredicateLocationCountry = "geo.location.country"

	// Time predicates
	PredicateObservationRecorded = "time.observation.recorded"
)

// RegisterVocabulary registers all weather station predicates with the vocabulary system.
func RegisterVocabulary() {
	vocabulary.Register(PredicateWeatherTempCelsius,
		vocabulary.WithDescription("Temperature reading in Celsius"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("celsius"),
	)

	vocabulary.Register(PredicateWeatherWindKph,
		vocabulary.WithDescription("Wind speed in kilometers per hour"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("kph"),
	)

	vocabulary.Register(PredicateWeatherHumidity,
		vocabulary.WithDescription("Humidity percentage"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithUnits("percent"),
		vocabulary.WithRange("0-100"),
	)

	vocabulary.Register(PredicateWeatherCondition,
		vocabulary.WithDescription("Weather condition (sunny, cloudy, rainy, etc.)"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	vocabulary.Register(PredicateLocationCity,
		vocabulary.WithDescription("City where station is located"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	vocabulary.Register(PredicateLocationCountry,
		vocabulary.WithDescription("Country where station is located"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
	)

	vocabulary.Register(PredicateObservationRecorded,
		vocabulary.WithDescription("Timestamp when observation was recorded"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime),
	)
}
