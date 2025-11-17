package graph

import (
	"testing"
	"time"

	"github.com/c360/semstreams/message"
)

func TestEntityState_TriplesField(t *testing.T) {
	now := time.Now()

	// Create sample triples
	triples := []message.Triple{
		{
			Subject:    "c360.platform1.robotics.mav1.drone.0",
			Predicate:  "robotics.battery.level",
			Object:     85.5,
			Source:     "mavlink",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    "c360.platform1.robotics.mav1.drone.0",
			Predicate:  "robotics.flight.armed",
			Object:     true,
			Source:     "mavlink",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    "c360.platform1.robotics.mav1.drone.0",
			Predicate:  "robotics.component.powered_by",
			Object:     "c360.platform1.robotics.mav1.battery.0",
			Source:     "system",
			Timestamp:  now,
			Confidence: 0.9,
		},
	}

	// Test EntityState with Triples field
	entityState := EntityState{
		Node: NodeProperties{
			ID:     "c360.platform1.robotics.mav1.drone.0",
			Type:   "robotics.drone",
			Status: StatusActive,
		},
		Edges:     []Edge{},
		Triples:   triples,
		ObjectRef: "objects/msg_123",
		Version:   1,
		UpdatedAt: now,
	}

	// Verify Triples field exists and contains expected data
	if len(entityState.Triples) != 3 {
		t.Errorf("Expected 3 triples, got %d", len(entityState.Triples))
	}

	// Verify specific triple content
	batteryTriple := entityState.Triples[0]
	if batteryTriple.Subject != "c360.platform1.robotics.mav1.drone.0" {
		t.Errorf("Expected subject 'c360.platform1.robotics.mav1.drone.0', got '%s'", batteryTriple.Subject)
	}
	if batteryTriple.Predicate != "robotics.battery.level" {
		t.Errorf("Expected predicate 'robotics.battery.level', got '%s'", batteryTriple.Predicate)
	}
	if batteryTriple.Object != 85.5 {
		t.Errorf("Expected object 85.5, got %v", batteryTriple.Object)
	}

	// Test relationship triple (Object is entity ID)
	relationTriple := entityState.Triples[2]
	if relationTriple.Predicate != "robotics.component.powered_by" {
		t.Errorf("Expected predicate 'robotics.component.powered_by', got '%s'", relationTriple.Predicate)
	}
	if relationTriple.Object != "c360.platform1.robotics.mav1.battery.0" {
		t.Errorf("Expected object 'c360.platform1.robotics.mav1.battery.0', got %v", relationTriple.Object)
	}
}

func TestEntityState_EmptyTriples(t *testing.T) {
	now := time.Now()

	// Test EntityState with empty Triples slice
	entityState := EntityState{
		Node: NodeProperties{
			ID:     "c360.platform1.robotics.mav1.drone.1",
			Type:   "robotics.drone",
			Status: StatusActive,
		},
		Edges:     []Edge{},
		Triples:   []message.Triple{}, // Empty triples
		ObjectRef: "objects/msg_124",
		Version:   1,
		UpdatedAt: now,
	}

	if entityState.Triples == nil {
		t.Error("Expected non-nil Triples slice")
	}
	if len(entityState.Triples) != 0 {
		t.Errorf("Expected 0 triples, got %d", len(entityState.Triples))
	}
}

func TestEntityState_NilTriples(t *testing.T) {
	now := time.Now()

	// Test EntityState with nil Triples
	entityState := EntityState{
		Node: NodeProperties{
			ID:     "c360.platform1.robotics.mav1.drone.2",
			Type:   "robotics.drone",
			Status: StatusActive,
		},
		Edges:     []Edge{},
		Triples:   nil, // nil triples
		ObjectRef: "objects/msg_125",
		Version:   1,
		UpdatedAt: now,
	}

	// Should not panic and should handle nil gracefully
	if len(entityState.Triples) != 0 {
		t.Errorf("Expected 0 triples for nil slice, got %d", len(entityState.Triples))
	}
}

// TestEntityState_BackwardCompatibility ensures existing code still works
// even with the new Triples field
func TestEntityState_BackwardCompatibility(t *testing.T) {
	now := time.Now()

	// Test existing patterns without Triples field
	entityState := EntityState{
		Node: NodeProperties{
			ID:   "c360.platform1.robotics.mav1.drone.3",
			Type: "robotics.drone",
			Properties: map[string]any{
				"battery_level": 75.0,
				"armed":         false,
			},
			Status: StatusActive,
		},
		Edges: []Edge{
			{
				ToEntityID: "c360.platform1.robotics.mav1.battery.0",
				EdgeType:   "POWERED_BY",
				Weight:     1.0,
				Confidence: 1.0,
				CreatedAt:  now,
			},
		},
		ObjectRef: "objects/msg_126",
		Version:   1,
		UpdatedAt: now,
		// Triples field not set - should default to nil/empty
	}

	// Existing functionality should still work
	if entityState.Node.ID != "c360.platform1.robotics.mav1.drone.3" {
		t.Errorf("Expected ID 'c360.platform1.robotics.mav1.drone.3', got '%s'", entityState.Node.ID)
	}
	if len(entityState.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(entityState.Edges))
	}
	if entityState.Node.Properties["battery_level"] != 75.0 {
		t.Errorf("Expected battery_level 75.0, got %v", entityState.Node.Properties["battery_level"])
	}

	// New Triples field should be empty/nil by default
	if len(entityState.Triples) != 0 {
		t.Errorf("Expected 0 triples by default, got %d", len(entityState.Triples))
	}
}
