package weatherstation

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/types"
)

// Processor transforms incoming JSON weather data into Graphable payloads.
type Processor struct {
	// authority is the composition root's platform.org / platform.id,
	// received through component.Dependencies.Platform. It is the ONLY
	// source of positions 1-2 of every entity this processor mints: not an
	// operator config key, not a constant, not a product name, and not a
	// field on an incoming payload (ADR-102 d2).
	authority types.PlatformMeta
}

// NewProcessor creates a new weather station processor minting under the given
// deployment authority. Callers pass component.Dependencies.Platform verbatim.
func NewProcessor(authority types.PlatformMeta) *Processor {
	return &Processor{authority: authority}
}

// Process transforms incoming JSON data into a WeatherReading.
//
// Expected JSON format:
//
//	{
//	  "station_id": "ws-001",
//	  "temperature": 22.5,
//	  "humidity": 65.0,
//	  "wind_speed": 15.0,
//	  "condition": "sunny",
//	  "city": "San Francisco",
//	  "country": "USA",
//	  "timestamp": "2025-11-26T10:30:00Z"
//	}
func (p *Processor) Process(input map[string]any) (*WeatherReading, error) {
	stationID, err := getString(input, "station_id")
	if err != nil {
		return nil, fmt.Errorf("missing station_id: %w", err)
	}

	temperature, err := getFloat64(input, "temperature")
	if err != nil {
		return nil, fmt.Errorf("missing temperature: %w", err)
	}

	humidity, err := getFloat64(input, "humidity")
	if err != nil {
		return nil, fmt.Errorf("missing humidity: %w", err)
	}

	condition, err := getString(input, "condition")
	if err != nil {
		return nil, fmt.Errorf("missing condition: %w", err)
	}

	// Optional fields
	windSpeed, _ := getFloat64(input, "wind_speed")
	city, _ := getString(input, "city")
	country, _ := getString(input, "country")

	// Optional: timestamp (default to now)
	var observedAt time.Time
	if ts, ok := input["timestamp"].(string); ok {
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			observedAt = time.Now()
		} else {
			observedAt = parsed
		}
	} else {
		observedAt = time.Now()
	}

	return &WeatherReading{
		StationID:     stationID,
		Temperature:   temperature,
		Humidity:      humidity,
		WindSpeed:     windSpeed,
		Condition:     condition,
		City:          city,
		Country:       country,
		ObservedAt:    observedAt,
		EntityIDValue: WeatherReadingEntityID(p.authority, stationID),
	}, nil
}

// Helper functions for type-safe field extraction

func getString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fieldNotFoundError(m, key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string: got %T", key, v)
	}
	return s, nil
}

func getFloat64(m map[string]any, key string) (float64, error) {
	v, ok := m[key]
	if !ok {
		return 0, fieldNotFoundError(m, key)
	}
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("field %q is not a number: got %T", key, v)
	}
}

// fieldNotFoundError returns a helpful error message suggesting similar field names.
func fieldNotFoundError(m map[string]any, key string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	suggestions := findSimilarFields(key, keys)
	if len(suggestions) > 0 {
		return fmt.Errorf("field %q not found (did you mean %q?), available fields: %v",
			key, suggestions[0], keys)
	}
	return fmt.Errorf("field %q not found, available fields: %v", key, keys)
}

// findSimilarFields finds field names similar to the expected key.
func findSimilarFields(expected string, available []string) []string {
	var similar []string

	// Common field name mappings
	commonMistakes := map[string][]string{
		"station_id":  {"stationId", "id", "station"},
		"temperature": {"temp", "temp_c", "celsius"},
		"humidity":    {"hum", "humid"},
		"condition":   {"weather", "status", "conditions"},
		"wind_speed":  {"windSpeed", "wind", "wind_kph"},
	}

	if mistakes, ok := commonMistakes[expected]; ok {
		for _, mistake := range mistakes {
			for _, avail := range available {
				if avail == mistake {
					similar = append(similar, avail)
				}
			}
		}
	}

	return similar
}
