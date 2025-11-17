package message

import (
	"testing"
	"time"
)

func TestTriple_IsRelationship(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		triple   Triple
		expected bool
	}{
		{
			name: "property triple with float value",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.battery.level",
				Object:     85.5,
				Source:     "mavlink",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "property triple with boolean value",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.flight.armed",
				Object:     true,
				Source:     "mavlink",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "property triple with string value (not entity ID)",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.status.text",
				Object:     "operational",
				Source:     "system",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "relationship triple with valid entity ID",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.component.powered_by",
				Object:     "c360.platform1.robotics.mav1.battery.0",
				Source:     "system",
				Timestamp:  now,
				Confidence: 0.9,
			},
			expected: true,
		},
		{
			name: "relationship triple with different valid entity ID",
			triple: Triple{
				Subject:    "ops.missions.patrol.alpha",
				Predicate:  "mission.includes.asset",
				Object:     "c360.platform1.robotics.mav1.drone.0",
				Source:     "rule-processor",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: true,
		},
		{
			name: "triple with int value",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.system.id",
				Object:     42,
				Source:     "mavlink",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "triple with string that's not a valid entity ID (too few parts)",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.component.type",
				Object:     "battery.type",
				Source:     "system",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "triple with string that's not a valid entity ID (too many parts)",
			triple: Triple{
				Subject:    "c360.platform1.robotics.mav1.drone.0",
				Predicate:  "robotics.component.ref",
				Object:     "telemetry.robotics.battery.model.v1",
				Source:     "system",
				Timestamp:  now,
				Confidence: 1.0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.triple.IsRelationship()
			if got != tt.expected {
				t.Errorf("Triple.IsRelationship() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsValidEntityID(t *testing.T) {
	tests := []struct {
		name     string
		entityID string
		expected bool
	}{
		{
			name:     "valid 6-part telemetry entity ID",
			entityID: "c360.platform1.robotics.mav1.drone.0",
			expected: true,
		},
		{
			name:     "old 4-part gcs entity ID (invalid)",
			entityID: "gcs.operators.station.1",
			expected: false,
		},
		{
			name:     "old 4-part ops entity ID (invalid)",
			entityID: "ops.missions.patrol.alpha",
			expected: false,
		},
		{
			name:     "valid 6-part telemetry battery ID",
			entityID: "c360.platform1.robotics.mav1.battery.0",
			expected: true,
		},
		{
			name:     "too few parts (3 parts)",
			entityID: "telemetry.robotics.drone",
			expected: false,
		},
		{
			name:     "too few parts (2 parts)",
			entityID: "robotics.drone",
			expected: false,
		},
		{
			name:     "too few parts (1 part)",
			entityID: "drone",
			expected: false,
		},
		{
			name:     "too few parts (5 parts)",
			entityID: "c360.platform1.robotics.mav1.drone",
			expected: false,
		},
		{
			name:     "too many parts (7 parts)",
			entityID: "c360.platform1.robotics.mav1.drone.0.extra",
			expected: false,
		},
		{
			name:     "empty string",
			entityID: "",
			expected: false,
		},
		{
			name:     "just dots",
			entityID: "...",
			expected: false,
		},
		{
			name:     "contains empty parts",
			entityID: "c360.platform1..mav1.drone.0",
			expected: false,
		},
		{
			name:     "ends with dot",
			entityID: "c360.platform1.robotics.mav1.drone.0.",
			expected: false,
		},
		{
			name:     "starts with dot",
			entityID: ".c360.platform1.robotics.mav1.drone.0",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidEntityID(tt.entityID)
			if got != tt.expected {
				t.Errorf("IsValidEntityID(%q) = %v, want %v", tt.entityID, got, tt.expected)
			}
		})
	}
}
