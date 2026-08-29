// Package iotsensor provides an example domain processor demonstrating the correct
// Graphable implementation pattern for SemStreams.
//
// This package serves as a reference implementation showing how to:
//   - Create domain-specific payloads that implement the Graphable interface
//   - Generate federated 6-part entity IDs with organizational context
//   - Produce semantic triples using registered vocabulary predicates
//   - Transform incoming JSON into meaningful graph structures
//
// IoT sensors are used as a neutral example domain that is:
//   - Simple enough to understand quickly
//   - Complex enough to demonstrate real patterns
//   - Universally understood across industries
//   - Not tied to any specific customer domain
//
// For production use, copy this example and adapt it to your domain vocabulary.
package iotsensor

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

var measurementPredicateByUnit = map[string]string{
	"celsius":    PredicateMeasurementCelsius,
	"fahrenheit": PredicateMeasurementFahrenheit,
	"percent":    PredicateMeasurementPercent,
	"hpa":        PredicateMeasurementHPA,
	"psi":        PredicateMeasurementPSI,
}

func buildSensorReading(fields map[string]any) (any, error) {
	msg := &SensorReading{}

	if v, ok := fields["DeviceID"].(string); ok {
		msg.DeviceID = v
	}
	if v, ok := fields["SensorType"].(string); ok {
		msg.SensorType = v
	}
	if v, ok := fields["Value"].(float64); ok {
		msg.Value = v
	}
	if v, ok := fields["Unit"].(string); ok {
		msg.Unit = v
	}
	if v, ok := fields["SerialNumber"].(string); ok {
		msg.SerialNumber = v
	}
	if v, ok := fields["ZoneEntityID"].(string); ok {
		msg.ZoneEntityID = v
	}
	// Keyed by the WIRE name, not the Go field name: Builder's documented
	// fallback is a JSON round-trip through Factory, so a builder must read
	// what the JSON carries. Every other field on this type is untagged, so
	// its Go name and wire name coincide; EntityIDValue is the one field
	// where they differ.
	if v, ok := fields["entity_id"].(string); ok {
		msg.EntityIDValue = v
	}

	// Handle optional float64 pointers for geospatial fields
	if v, ok := fields["Latitude"].(float64); ok {
		lat := v
		msg.Latitude = &lat
	}
	if v, ok := fields["Longitude"].(float64); ok {
		lon := v
		msg.Longitude = &lon
	}
	if v, ok := fields["Altitude"].(float64); ok {
		alt := v
		msg.Altitude = &alt
	}

	// Handle ObservedAt timestamp
	if v, ok := fields["ObservedAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			msg.ObservedAt = t
		}
	}

	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return msg, nil
}

// RegisterPayloads registers the SensorReading (iot.sensor.v1) and
// Zone (facility.zone.v1) payload types with the supplied registry.
// Called by binaries that want to load this example processor's
// payloads — cmd/semstreams and cmd/e2e-semstreams.
//
// Both types must register: SensorReading triples reference Zone
// entity IDs, and graph-ingest needs to deserialize both payload
// shapes. Registering only iot.sensor.v1 leaves facility.zone.v1
// messages unrouteable, which silently breaks every flow that
// produces zone entities.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	if err := reg.Register(&payloadregistry.Registration{
		Domain:      "iot",
		Category:    "sensor",
		Version:     "v1",
		Description: "IoT sensor reading payload with Graphable implementation",
		Factory: func() any {
			return &SensorReading{}
		},
		Builder: buildSensorReading,
		Example: map[string]any{
			"DeviceID":   "sensor-042",
			"SensorType": "temperature",
			"Value":      23.5,
			"Unit":       "celsius",
			"entity_id":  "acme.dep1.sensor.environmental.temperature.sensor-042",
		},
	}); err != nil {
		return err
	}
	return reg.Register(&payloadregistry.Registration{
		Domain:      "facility",
		Category:    "zone",
		Version:     "v1",
		Description: "Facility zone entity referenced by IoT sensor triples",
		Factory: func() any {
			return &Zone{}
		},
		Example: map[string]any{
			"ZoneID":    "warehouse-7",
			"ZoneType":  "warehouse",
			"Name":      "Main Warehouse",
			"entity_id": "acme.dep1.zone.facility.warehouse.warehouse-7",
		},
	})
}

// SensorReading represents an IoT sensor measurement. It implements the Graphable
// interface with federated entity IDs and semantic predicates.
//
// This is an example of a domain-specific payload that encodes semantic understanding
// of the data, as opposed to generic processors that make semantic decisions without
// domain knowledge.
type SensorReading struct {
	// Input fields (from incoming JSON)
	DeviceID   string    // e.g., "sensor-042"
	SensorType string    // e.g., "temperature", "humidity", "pressure"
	Value      float64   // e.g., 23.5
	Unit       string    // e.g., "celsius", "percent", "hpa"
	ObservedAt time.Time // When measurement was taken

	// Alias field (for ALIAS_INDEX testing)
	SerialNumber string // e.g., "SN-2025-001234" - manufacturer serial number

	// Geospatial fields (for SPATIAL_INDEX testing)
	Latitude  *float64 // e.g., 37.7749 (nil if not provided)
	Longitude *float64 // e.g., -122.4194 (nil if not provided)
	Altitude  *float64 // e.g., 10.0 meters (optional)

	// Entity reference fields (minted by the processor)
	ZoneEntityID string // e.g., "acme.dep1.zone.facility.area.warehouse-7"

	// EntityIDValue is this reading's own identity, minted once by the
	// processor under the composition root's platform.org / platform.id and
	// carried on the wire from there (ADR-102 d2). It is not re-derived
	// downstream: a reader that had to recompute it would need an authority
	// nobody can hand it, and would get a different answer when told wrong.
	EntityIDValue string `json:"entity_id"`
}

// EntityID returns the identity minted for this reading. The value is
// SensorReadingEntityID's output, stamped at mint time and carried on the
// wire — never recomputed here.
func (s *SensorReading) EntityID() string {
	return s.EntityIDValue
}

// SensorReadingEntityID mints a reading's deterministic 6-part federated
// entity ID in the canonical order (ADR-102):
// {org}.{platform}.{system}.{domain}.{type}.{instance}
//
// Example: "acme.dep1.sensor.environmental.temperature.sensor-042"
//
// The 6 parts provide:
//   - org: the composition root's platform.org (multi-tenancy)
//   - platform: the composition root's platform.id — the minting deployment
//     authority, never a product name and never an operator knob on this
//     component (ADR-102 d2)
//   - system: the source that produced the entity (sensor, actuator, etc.)
//   - domain: this example's delegated taxonomy (environmental)
//   - type: Entity type within the domain (temperature, humidity, etc.)
//   - instance: Unique instance identifier
func SensorReadingEntityID(authority types.PlatformMeta, sensorType, deviceID string) string {
	return semtypes.EntityID{
		Org:      authority.Org,
		Platform: authority.Platform,
		System:   sensorSystem,
		Domain:   environmentDomain,
		Type:     sensorType,
		Instance: deviceID,
	}.Key()
}

// Triples returns semantic facts about this sensor reading using domain-appropriate
// predicates from the vocabulary system.
//
// Each triple follows the Subject-Predicate-Object pattern where:
//   - Subject: This entity's ID (self-reference)
//   - Predicate: Semantic property using dotted notation (domain.category.property)
//   - Object: The value (literal) or entity reference (another entity ID)
//
// The triples produced demonstrate:
//   - Unit-specific predicates (sensor.measurement.celsius vs generic "value")
//   - Entity references (geo.location.zone points to Zone entity, not a string)
//   - Classification triples (sensor.classification.type)
//   - Temporal tracking (time.observation.recorded)
func (s *SensorReading) Triples() []message.Triple {
	entityID := s.EntityID()
	measurementPredicate := measurementPredicateByUnit[s.Unit]

	triples := []message.Triple{
		// Measurement value with unit-specific predicate
		{
			Subject:    entityID,
			Predicate:  measurementPredicate,
			Object:     s.Value,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		},
		// Sensor type classification
		{
			Subject:    entityID,
			Predicate:  "sensor.classification.type",
			Object:     s.SensorType,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		},
		// Location as entity reference (not string!)
		{
			Subject:    entityID,
			Predicate:  "geo.location.zone",
			Object:     s.ZoneEntityID,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		},
		// Observation timestamp
		{
			Subject:    entityID,
			Predicate:  "time.observation.recorded",
			Object:     s.ObservedAt,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		},
	}

	// Alias triple (for ALIAS_INDEX) - serial number as resolvable external ID
	if s.SerialNumber != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateSensorSerial,
			Object:     s.SerialNumber,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		})
	}

	// Geospatial triples (for SPATIAL_INDEX) - lat/lon coordinates
	if s.Latitude != nil && s.Longitude != nil {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateLocationLatitude,
			Object:     *s.Latitude,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		})
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateLocationLongitude,
			Object:     *s.Longitude,
			Source:     "iot_sensor",
			Timestamp:  s.ObservedAt,
			Confidence: 1.0,
		})
		// Optional altitude
		if s.Altitude != nil {
			triples = append(triples, message.Triple{
				Subject:    entityID,
				Predicate:  "geo.location.altitude",
				Object:     *s.Altitude,
				Source:     "iot_sensor",
				Timestamp:  s.ObservedAt,
				Confidence: 1.0,
			})
		}
	}

	return triples
}

// Payload interface implementation

// Schema returns the message type for sensor readings.
// This identifies the payload type for routing and processing.
// Type format: domain.category.version → iot.sensor.v1
func (s *SensorReading) Schema() message.Type {
	return message.Type{
		Domain:   "iot",
		Category: "sensor",
		Version:  "v1",
	}
}

// Validate checks that the sensor reading has all required fields.
func (s *SensorReading) Validate() error {
	if s.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if s.SensorType == "" {
		return fmt.Errorf("sensor type is required")
	}
	if s.Unit == "" {
		return fmt.Errorf("unit is required")
	}
	if _, ok := measurementPredicateByUnit[s.Unit]; !ok {
		return fmt.Errorf("unsupported unit %q; supported units are celsius, fahrenheit, percent, hpa, psi", s.Unit)
	}
	if s.EntityIDValue == "" {
		return fmt.Errorf("entity_id is required; mint it with SensorReadingEntityID under the deployment authority")
	}
	return nil
}

// MarshalJSON implements json.Marshaler for SensorReading.
// Uses alias pattern to avoid infinite recursion.
func (s *SensorReading) MarshalJSON() ([]byte, error) {
	type Alias SensorReading
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler for SensorReading.
// Uses alias pattern to avoid infinite recursion.
func (s *SensorReading) UnmarshalJSON(data []byte) error {
	type Alias SensorReading
	return json.Unmarshal(data, (*Alias)(s))
}

// ZoneEntityID mints a federated 6-part entity ID for a zone under the
// deployment authority. This is the single source of truth for zone entity ID
// format, ensuring consistency between Zone.EntityID() and any references to
// zones. Positions 1-2 come from the composition root's platform.org /
// platform.id and nothing else (ADR-102 d2).
//
// Example: ZoneEntityID(types.PlatformMeta{Org: "acme", Platform: "dep1"}, "area", "warehouse-7")
// Returns: "acme.dep1.zone.facility.area.warehouse-7"
// (system = zone, domain = facility in the canonical order)
func ZoneEntityID(authority types.PlatformMeta, zoneType, zoneID string) string {
	return semtypes.EntityID{
		Org:      authority.Org,
		Platform: authority.Platform,
		System:   zoneSystem,
		Domain:   facilityDomain,
		Type:     zoneType,
		Instance: zoneID,
	}.Key()
}

// Zone represents a location zone entity. It demonstrates how entity references
// work in triples - SensorReading references Zone by entity ID, not by string.
type Zone struct {
	ZoneID   string // e.g., "warehouse-7"
	ZoneType string // e.g., "warehouse", "office", "outdoor"
	Name     string // e.g., "Main Warehouse"

	// EntityIDValue is the zone's minted identity — ZoneEntityID's output,
	// stamped under the deployment authority and carried on the wire.
	EntityIDValue string `json:"entity_id"`
}

// EntityID returns the zone's minted identity.
// Example: "acme.dep1.zone.facility.area.warehouse-7"
func (z *Zone) EntityID() string {
	return z.EntityIDValue
}

// Triples returns semantic facts about this zone.
func (z *Zone) Triples() []message.Triple {
	entityID := z.EntityID()
	now := time.Now()

	return []message.Triple{
		{
			Subject:    entityID,
			Predicate:  "facility.zone.name",
			Object:     z.Name,
			Source:     "iot_sensor",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  "facility.zone.type",
			Object:     z.ZoneType,
			Source:     "iot_sensor",
			Timestamp:  now,
			Confidence: 1.0,
		},
	}
}

// Schema returns the message type for Zone payloads.
func (z *Zone) Schema() message.Type {
	return message.Type{
		Domain:   "facility",
		Category: "zone",
		Version:  "v1",
	}
}

// Validate checks that the Zone has required fields.
func (z *Zone) Validate() error {
	if z.ZoneID == "" {
		return fmt.Errorf("zone_id is required")
	}
	if z.EntityIDValue == "" {
		return fmt.Errorf("entity_id is required; mint it with ZoneEntityID under the deployment authority")
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling for Zone.
func (z *Zone) MarshalJSON() ([]byte, error) {
	type Alias Zone
	return json.Marshal((*Alias)(z))
}

// UnmarshalJSON implements custom JSON unmarshaling for Zone.
func (z *Zone) UnmarshalJSON(data []byte) error {
	type Alias Zone
	return json.Unmarshal(data, (*Alias)(z))
}
