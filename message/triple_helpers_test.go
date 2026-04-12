package message

import (
	"encoding/json"
	"strings"
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
		{
			name:     "JSON string with 5 dots (false positive from file extensions)",
			entityID: `{"include":["main.go","auth/middleware.go","handlers/user Handlers.go","test/auth_test.go"],"exclude":["third-party libraries"],"do_not_touch":["README.md","LICENSE"]}`,
			expected: false,
		},
		{
			name:     "part contains spaces",
			entityID: "c360.platform 1.robotics.mav1.drone.0",
			expected: false,
		},
		{
			name:     "part contains braces",
			entityID: "c360.{platform}.robotics.mav1.drone.0",
			expected: false,
		},
		{
			name:     "part contains slash",
			entityID: "c360.platform/1.robotics.mav1.drone.0",
			expected: false,
		},
		{
			name:     "valid with hyphens and underscores",
			entityID: "c360.my-platform.robot_ics.mav-1.drone_type.instance-0",
			expected: true,
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

// ptrTime returns a pointer to a time.Time value for testing ExpiresAt.
func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestTriple_ExpiresAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expiresAt *time.Time
		isExpired bool
	}{
		{
			name:      "nil ExpiresAt never expires",
			expiresAt: nil,
			isExpired: false,
		},
		{
			name:      "future ExpiresAt not expired",
			expiresAt: ptrTime(time.Now().Add(1 * time.Hour)),
			isExpired: false,
		},
		{
			name:      "past ExpiresAt is expired",
			expiresAt: ptrTime(time.Now().Add(-1 * time.Hour)),
			isExpired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triple := Triple{
				Subject:   "c360.platform1.robotics.mav1.drone.0",
				Predicate: "robotics.battery.level",
				Object:    85.5,
				ExpiresAt: tt.expiresAt,
			}

			if got := triple.IsExpired(); got != tt.isExpired {
				t.Errorf("Triple.IsExpired() = %v, want %v", got, tt.isExpired)
			}
		})
	}
}

func TestTriple_ExpiresAt_JSON(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	triple := Triple{
		Subject:   "c360.platform1.robotics.mav1.drone.0",
		Predicate: "robotics.battery.level",
		Object:    85.5,
		ExpiresAt: &expiresAt,
	}

	data, err := json.Marshal(triple)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Triple
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil after unmarshal")
	}
	if !decoded.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", decoded.ExpiresAt, expiresAt)
	}
}

func TestTriple_ExpiresAt_JSON_OmitEmpty(t *testing.T) {
	t.Parallel()

	triple := Triple{
		Subject:   "c360.platform1.robotics.mav1.drone.0",
		Predicate: "robotics.battery.level",
		Object:    85.5,
		ExpiresAt: nil, // Should be omitted
	}

	data, err := json.Marshal(triple)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if strings.Contains(string(data), "expires_at") {
		t.Error("expires_at should be omitted when nil")
	}
}
