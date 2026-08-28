// Package iotsensor provides an example domain processor demonstrating the correct
// Graphable implementation pattern for SemStreams.
package iotsensor

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
)

// testAuthority is a deployment authority in the shape a composition root
// supplies it: platform.org / platform.id. "dep1" is a deployment name, not a
// product name — position 2 never carries a product (ADR-102 d2, d3).
var testAuthority = types.PlatformMeta{Org: "acme", Platform: "dep1"}

// secondAuthority is a second deployment under the same org, proving the
// minted prefix follows the composition root rather than any payload field.
var secondAuthority = types.PlatformMeta{Org: "acme", Platform: "dep2"}

// TestSensorReading_EntityID_6PartFormat verifies that SensorReading.EntityID()
// returns a properly formatted 6-part federated entity ID.
func TestSensorReading_EntityID_6PartFormat(t *testing.T) {
	tests := []struct {
		name       string
		reading    SensorReading
		wantParts  int
		wantPrefix string
	}{
		{
			name: "temperature sensor",
			reading: SensorReading{
				DeviceID:      "sensor-042",
				SensorType:    "temperature",
				Value:         23.5,
				Unit:          "celsius",
				ZoneEntityID:  ZoneEntityID(testAuthority, "area", "warehouse-7"),
				ObservedAt:    time.Now(),
				EntityIDValue: SensorReadingEntityID(testAuthority, "temperature", "sensor-042"),
			},
			wantParts:  6,
			wantPrefix: "acme.dep1.sensor.environmental",
		},
		{
			name: "humidity sensor",
			reading: SensorReading{
				DeviceID:      "hum-001",
				SensorType:    "humidity",
				Value:         65.0,
				Unit:          "percent",
				ZoneEntityID:  ZoneEntityID(secondAuthority, "area", "office-3"),
				ObservedAt:    time.Now(),
				EntityIDValue: SensorReadingEntityID(secondAuthority, "humidity", "hum-001"),
			},
			wantParts:  6,
			wantPrefix: "acme.dep2.sensor.environmental",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityID := tt.reading.EntityID()

			// Verify 6-part format
			parts := strings.Split(entityID, ".")
			if len(parts) != tt.wantParts {
				t.Errorf("EntityID() = %q has %d parts, want %d parts",
					entityID, len(parts), tt.wantParts)
			}

			// Verify no empty parts
			for i, part := range parts {
				if part == "" {
					t.Errorf("EntityID() part %d is empty in %q", i, entityID)
				}
			}

			// Verify prefix matches expected pattern
			if !strings.HasPrefix(entityID, tt.wantPrefix) {
				t.Errorf("EntityID() = %q, want prefix %q", entityID, tt.wantPrefix)
			}

			// Verify it passes the message package validation
			if !message.IsValidEntityID(entityID) {
				t.Errorf("EntityID() = %q is not valid per message.IsValidEntityID()", entityID)
			}
		})
	}
}

// TestSensorReading_Triples_SemanticPredicates verifies that SensorReading.Triples()
// returns semantically meaningful triples with proper predicates.
func TestSensorReading_Triples_SemanticPredicates(t *testing.T) {
	reading := SensorReading{
		DeviceID:      "sensor-042",
		SensorType:    "temperature",
		Value:         23.5,
		Unit:          "celsius",
		ZoneEntityID:  ZoneEntityID(testAuthority, "area", "warehouse-7"),
		ObservedAt:    time.Date(2025, 11, 26, 10, 30, 0, 0, time.UTC),
		EntityIDValue: SensorReadingEntityID(testAuthority, "temperature", "sensor-042"),
	}

	triples := reading.Triples()

	// FR-005: Must generate at least 3 semantically meaningful triples
	// SC-005: The example generates at least 3 triples per sensor reading
	if len(triples) < 3 {
		t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
	}

	// Verify all triples have proper subject (self-reference)
	entityID := reading.EntityID()
	for i, triple := range triples {
		if triple.Subject != entityID {
			t.Errorf("Triple[%d].Subject = %q, want %q", i, triple.Subject, entityID)
		}
	}

	// Verify predicates use dotted notation (no colons)
	for i, triple := range triples {
		if strings.Contains(triple.Predicate, ":") {
			t.Errorf("Triple[%d].Predicate = %q contains colon, want dotted notation",
				i, triple.Predicate)
		}
	}

	// Verify expected predicates exist
	predicateMap := make(map[string]bool)
	for _, triple := range triples {
		predicateMap[triple.Predicate] = true
	}

	requiredPredicates := []string{
		"sensor.measurement.celsius",
		"sensor.classification.type",
		"geo.location.zone",
	}

	for _, pred := range requiredPredicates {
		if !predicateMap[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}

	// Verify geo.location.zone is an entity reference (6-part ID)
	for _, triple := range triples {
		if triple.Predicate == "geo.location.zone" {
			zoneID, ok := triple.Object.(string)
			if !ok {
				t.Errorf("geo.location.zone Object is not a string: %T", triple.Object)
				continue
			}
			if !message.IsValidEntityID(zoneID) {
				t.Errorf("geo.location.zone = %q is not a valid 6-part entity ID", zoneID)
			}
		}
	}
}

// TestZone_EntityID_6PartFormat verifies that Zone.EntityID() returns a properly
// formatted 6-part federated entity ID.
func TestZone_EntityID_6PartFormat(t *testing.T) {
	zone := Zone{
		ZoneID:        "warehouse-7",
		ZoneType:      "area",
		Name:          "Main Warehouse",
		EntityIDValue: ZoneEntityID(testAuthority, "area", "warehouse-7"),
	}

	entityID := zone.EntityID()

	// Verify 6-part format
	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() = %q has %d parts, want 6", entityID, len(parts))
	}

	// Verify it passes the message package validation
	if !message.IsValidEntityID(entityID) {
		t.Errorf("EntityID() = %q is not valid per message.IsValidEntityID()", entityID)
	}

	// Verify expected prefix
	wantPrefix := "acme.dep1.zone.facility"
	if !strings.HasPrefix(entityID, wantPrefix) {
		t.Errorf("EntityID() = %q, want prefix %q", entityID, wantPrefix)
	}
}

// TestZone_Triples verifies that Zone.Triples() returns proper triples.
func TestZone_Triples(t *testing.T) {
	zone := Zone{
		ZoneID:        "warehouse-7",
		ZoneType:      "area",
		Name:          "Main Warehouse",
		EntityIDValue: ZoneEntityID(testAuthority, "area", "warehouse-7"),
	}

	triples := zone.Triples()

	// Zone should have at least name and type triples
	if len(triples) < 2 {
		t.Errorf("Triples() returned %d triples, want at least 2", len(triples))
	}

	// Verify all triples have proper subject
	entityID := zone.EntityID()
	for i, triple := range triples {
		if triple.Subject != entityID {
			t.Errorf("Triple[%d].Subject = %q, want %q", i, triple.Subject, entityID)
		}
	}

	// Verify predicates use dotted notation (no colons)
	for i, triple := range triples {
		if strings.Contains(triple.Predicate, ":") {
			t.Errorf("Triple[%d].Predicate = %q contains colon, want dotted notation",
				i, triple.Predicate)
		}
	}
}

// TestGraphableInterface verifies that SensorReading and Zone implement Graphable.
func TestGraphableInterface(_ *testing.T) {
	// Compile-time check that types implement Graphable
	var _ graph.Graphable = (*SensorReading)(nil)
	var _ graph.Graphable = (*Zone)(nil)
}
