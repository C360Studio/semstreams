package iotsensor

import (
	"fmt"
	"time"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

// Processor transforms incoming JSON sensor data into Graphable payloads.
// It applies the organizational context from configuration and produces
// SensorReading instances with proper federated entity IDs and semantic triples.
//
// This demonstrates the correct pattern for domain processors:
//   - The composition root supplies the deployment authority; configuration
//     never does (ADR-102 d2)
//   - Process method transforms data with domain understanding
//   - Output is a Graphable payload, not generic JSON
type Processor struct {
	// authority is the composition root's platform.org / platform.id,
	// received through component.Dependencies.Platform. It is the ONLY
	// source of positions 1-2 of every entity this processor mints: not an
	// operator config key, not a constant, not a product name, and not a
	// field on an incoming payload (ADR-102 d2).
	authority types.PlatformMeta
}

// NewProcessor creates a new IoT sensor processor minting under the given
// deployment authority. Callers pass component.Dependencies.Platform verbatim.
func NewProcessor(authority types.PlatformMeta) *Processor {
	return &Processor{
		authority: authority,
	}
}

// Process transforms incoming JSON data into a SensorReading.
//
// Expected JSON format:
//
//	{
//	  "device_id": "sensor-042",
//	  "type": "temperature",
//	  "reading": 23.5,
//	  "unit": "celsius",
//	  "location": "warehouse-7",
//	  "timestamp": "2025-11-26T10:30:00Z"
//	}
//
// The processor:
//  1. Extracts fields from the incoming JSON
//  2. Mints the identity under the deployment authority
//  3. Returns a SensorReading that implements Graphable
//
// This method demonstrates domain-specific transformation logic:
//   - Field extraction with proper type handling
//   - Identity minted from the composition root's authority
//   - Validation of required fields
func (p *Processor) Process(input map[string]any) (*SensorReading, error) {
	// Extract required fields
	deviceID, err := getString(input, "device_id")
	if err != nil {
		return nil, fmt.Errorf("missing device_id: %w", err)
	}

	sensorType, err := getString(input, "type")
	if err != nil {
		return nil, fmt.Errorf("missing type: %w", err)
	}

	value, err := getFloat64(input, "reading")
	if err != nil {
		return nil, fmt.Errorf("missing reading: %w", err)
	}

	unit, err := getString(input, "unit")
	if err != nil {
		return nil, fmt.Errorf("missing unit: %w", err)
	}

	locationID, err := getString(input, "location")
	if err != nil {
		return nil, fmt.Errorf("missing location: %w", err)
	}

	// Extract zone type (optional, default to "area")
	zoneType := "area"
	if zt, ok := input["zone_type"].(string); ok && zt != "" {
		zoneType = zt
	}

	// Parse timestamp (optional, default to now)
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

	// Extract serial number (optional, for ALIAS_INDEX)
	var serialNumber string
	if serial, ok := input["serial"].(string); ok {
		serialNumber = serial
	}

	// Extract coordinates (optional, for SPATIAL_INDEX)
	var latitude, longitude, altitude *float64
	if lat, err := getFloat64(input, "latitude"); err == nil {
		latitude = &lat
	}
	if lon, err := getFloat64(input, "longitude"); err == nil {
		longitude = &lon
	}
	if alt, err := getFloat64(input, "altitude"); err == nil {
		altitude = &alt
	}

	// Build the Graphable payload, minting both identities under the
	// deployment authority. The processor computes the zone entity ID -
	// this is domain knowledge.
	reading := &SensorReading{
		DeviceID:      deviceID,
		SensorType:    sensorType,
		Value:         value,
		Unit:          unit,
		ObservedAt:    observedAt,
		SerialNumber:  serialNumber,
		Latitude:      latitude,
		Longitude:     longitude,
		Altitude:      altitude,
		ZoneEntityID:  ZoneEntityID(p.authority, zoneType, locationID),
		EntityIDValue: SensorReadingEntityID(p.authority, sensorType, deviceID),
	}
	if err := reading.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sensor reading: %w", err)
	}

	return reading, nil
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

// fieldNotFoundError returns a helpful error message when a required field is missing.
// It suggests similar field names if found and lists all available fields.
func fieldNotFoundError(m map[string]any, key string) error {
	// Collect available keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	// Check for similar field names (common mistakes)
	suggestions := findSimilarFields(key, keys)

	if len(suggestions) > 0 {
		return fmt.Errorf("field %q not found (did you mean %q?), available fields: %v", key, suggestions[0], keys)
	}
	return fmt.Errorf("field %q not found, available fields: %v", key, keys)
}

// findSimilarFields returns field names that are similar to the expected key.
// It checks for common variations like underscores, prefixes, and substrings.
func findSimilarFields(expected string, available []string) []string {
	var similar []string

	// Common field name mappings (expected -> common mistakes)
	commonMistakes := map[string][]string{
		"type":      {"sensor_type", "sensorType", "kind", "sensor_kind"},
		"reading":   {"value", "val", "measurement", "data", "sensor_value"},
		"location":  {"zone_id", "zoneId", "zone", "loc", "place", "area"},
		"device_id": {"deviceId", "id", "sensor_id", "sensorId"},
		"unit":      {"units", "uom", "measure_unit"},
	}

	// Check common mistakes first
	if mistakes, ok := commonMistakes[expected]; ok {
		for _, mistake := range mistakes {
			for _, avail := range available {
				if avail == mistake {
					similar = append(similar, avail)
				}
			}
		}
	}

	// Also check if expected is a substring or available is a substring
	for _, avail := range available {
		// Skip if already found
		alreadyFound := false
		for _, s := range similar {
			if s == avail {
				alreadyFound = true
				break
			}
		}
		if alreadyFound {
			continue
		}

		// Check substring matches (e.g., "type" in "sensor_type")
		if len(expected) >= 3 && len(avail) >= 3 {
			if contains(avail, expected) || contains(expected, avail) {
				similar = append(similar, avail)
			}
		}
	}

	return similar
}

// contains checks if s contains substr (case-insensitive for flexibility)
func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ParseZoneEntityID extracts zone type and zone ID from a full zone entity ID.
// Zone entity ID format (canonical order): org.platform.zone.facility.{zoneType}.{zoneID}
// Example: "acme.dep1.zone.facility.area.cold-storage-1" -> ("area", "cold-storage-1")
// Returns empty strings if the entity ID is not a canonical zone identity. The
// positions are read by NAME through pkg/types.ParseEntityID, never by raw
// index, so a value in any other shape resolves to nothing rather than to a
// silently mis-read zone.
func ParseZoneEntityID(entityID string) (zoneType, zoneID string) {
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		return "", ""
	}
	if parsed.System != zoneSystem || parsed.Domain != facilityDomain {
		return "", ""
	}
	return parsed.Type, parsed.Instance
}

// splitEntityID splits an entity ID into its parts.
func splitEntityID(entityID string) []string {
	if entityID == "" {
		return nil
	}
	var parts []string
	start := 0
	for i := 0; i < len(entityID); i++ {
		if entityID[i] == '.' {
			parts = append(parts, entityID[start:i])
			start = i + 1
		}
	}
	parts = append(parts, entityID[start:])
	return parts
}
