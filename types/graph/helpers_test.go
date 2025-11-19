package graph

import (
	"testing"

	"github.com/c360/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPropertyValue(t *testing.T) {
	// Create test entity with property and relationship triples
	entity := &EntityState{
		Node: NodeProperties{
			ID:   "test.entity",
			Type: "test.type",
		},
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    75.5,
			},
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.voltage",
				Object:    12.6,
			},
			{
				Subject:   "test.entity",
				Predicate: "CONNECTED_TO", // Relationship predicate
				Object:    "c360.platform1.robotics.mav1.drone.0", // Valid 6-part EntityID
			},
		},
	}

	tests := []struct {
		name      string
		entity    *EntityState
		predicate string
		wantValue any
		wantFound bool
	}{
		{
			name:      "existing property",
			entity:    entity,
			predicate: "robotics.battery.level",
			wantValue: 75.5,
			wantFound: true,
		},
		{
			name:      "existing property voltage",
			entity:    entity,
			predicate: "robotics.battery.voltage",
			wantValue: 12.6,
			wantFound: true,
		},
		{
			name:      "relationship should not be found",
			entity:    entity,
			predicate: "CONNECTED_TO",
			wantValue: nil,
			wantFound: false,
		},
		{
			name:      "non-existing property",
			entity:    entity,
			predicate: "non.existing.property",
			wantValue: nil,
			wantFound: false,
		},
		{
			name:      "nil entity",
			entity:    nil,
			predicate: "any.property",
			wantValue: nil,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found := GetPropertyValue(tt.entity, tt.predicate)
			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.wantValue, value)
		})
	}
}

func TestGetPropertyValueTyped(t *testing.T) {
	entity := &EntityState{
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    75.5, // float64
			},
			{
				Subject:   "test.entity",
				Predicate: "robotics.flight.armed",
				Object:    true, // bool
			},
			{
				Subject:   "test.entity",
				Predicate: "entity.name",
				Object:    "Test Drone", // string
			},
		},
	}

	t.Run("correct type float64", func(t *testing.T) {
		value, found := GetPropertyValueTyped[float64](entity, "robotics.battery.level")
		assert.True(t, found)
		assert.Equal(t, 75.5, value)
	})

	t.Run("correct type bool", func(t *testing.T) {
		value, found := GetPropertyValueTyped[bool](entity, "robotics.flight.armed")
		assert.True(t, found)
		assert.Equal(t, true, value)
	})

	t.Run("correct type string", func(t *testing.T) {
		value, found := GetPropertyValueTyped[string](entity, "entity.name")
		assert.True(t, found)
		assert.Equal(t, "Test Drone", value)
	})

	t.Run("wrong type", func(t *testing.T) {
		value, found := GetPropertyValueTyped[string](entity, "robotics.battery.level") // float64 as string
		assert.False(t, found)
		assert.Equal(t, "", value) // zero value for string
	})

	t.Run("non-existing property", func(t *testing.T) {
		value, found := GetPropertyValueTyped[float64](entity, "non.existing")
		assert.False(t, found)
		assert.Equal(t, 0.0, value) // zero value for float64
	})
}

func TestGetProperties(t *testing.T) {
	entity := &EntityState{
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    75.5,
			},
			{
				Subject:   "test.entity",
				Predicate: "robotics.flight.armed",
				Object:    true,
			},
			{
				Subject:   "test.entity",
				Predicate: "CONNECTED_TO", // Relationship - should be excluded
				Object:    "c360.platform1.robotics.mav1.drone.0", // Valid 6-part EntityID
			},
		},
	}

	t.Run("normal entity", func(t *testing.T) {
		props := GetProperties(entity)
		expected := map[string]any{
			"robotics.battery.level": 75.5,
			"robotics.flight.armed":  true,
		}
		assert.Equal(t, expected, props)
	})

	t.Run("nil entity", func(t *testing.T) {
		props := GetProperties(nil)
		assert.NotNil(t, props)
		assert.Empty(t, props)
	})

	t.Run("entity with no triples", func(t *testing.T) {
		emptyEntity := &EntityState{Triples: []message.Triple{}}
		props := GetProperties(emptyEntity)
		assert.NotNil(t, props)
		assert.Empty(t, props)
	})
}

func TestGetRelationshipTriples(t *testing.T) {
	entity := &EntityState{
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level", // Property
				Object:    75.5,
			},
			{
				Subject:   "test.entity",
				Predicate: "CONNECTED_TO", // Relationship
				Object:    "c360.platform1.robotics.mav1.drone.0", // Valid 6-part EntityID
			},
			{
				Subject:   "test.entity",
				Predicate: "POWERED_BY", // Relationship
				Object:    "c360.platform1.power.battery.main.0", // Valid 6-part EntityID
			},
		},
	}

	t.Run("normal entity", func(t *testing.T) {
		relationships := GetRelationshipTriples(entity)
		require.Len(t, relationships, 2)

		predicates := make([]string, len(relationships))
		for i, rel := range relationships {
			predicates[i] = rel.Predicate
		}
		assert.Contains(t, predicates, "CONNECTED_TO")
		assert.Contains(t, predicates, "POWERED_BY")
	})

	t.Run("nil entity", func(t *testing.T) {
		relationships := GetRelationshipTriples(nil)
		assert.Nil(t, relationships)
	})
}

func TestGetPropertyTriples(t *testing.T) {
	entity := &EntityState{
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level", // Property
				Object:    75.5,
			},
			{
				Subject:   "test.entity",
				Predicate: "robotics.flight.armed", // Property
				Object:    true,
			},
			{
				Subject:   "test.entity",
				Predicate: "CONNECTED_TO", // Relationship - should be excluded
				Object:    "c360.platform1.robotics.mav1.drone.0", // Valid 6-part EntityID
			},
		},
	}

	t.Run("normal entity", func(t *testing.T) {
		properties := GetPropertyTriples(entity)
		require.Len(t, properties, 2)

		predicates := make([]string, len(properties))
		for i, prop := range properties {
			predicates[i] = prop.Predicate
		}
		assert.Contains(t, predicates, "robotics.battery.level")
		assert.Contains(t, predicates, "robotics.flight.armed")
	})

	t.Run("nil entity", func(t *testing.T) {
		properties := GetPropertyTriples(nil)
		assert.Nil(t, properties)
	})
}

func TestHasProperty(t *testing.T) {
	entity := &EntityState{
		Triples: []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    75.5,
			},
		},
	}

	assert.True(t, HasProperty(entity, "robotics.battery.level"))
	assert.False(t, HasProperty(entity, "non.existing.property"))
	assert.False(t, HasProperty(nil, "any.property"))
}

func TestMergeTriples(t *testing.T) {
	t.Run("newer overrides existing property", func(t *testing.T) {
		existing := []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    70.0, // Old value
			},
			{
				Subject:   "test.entity",
				Predicate: "robotics.flight.armed",
				Object:    false, // Old value
			},
		}

		newer := []message.Triple{
			{
				Subject:   "test.entity",
				Predicate: "robotics.battery.level",
				Object:    85.0, // New value should win
			},
		}

		merged := MergeTriples(existing, newer)

		// Should have both properties
		require.Len(t, merged, 2)

		// Newer battery level should win
		batteryLevel, found := findTripleByPredicate(merged, "robotics.battery.level")
		assert.True(t, found)
		assert.Equal(t, 85.0, batteryLevel.Object)

		// Existing armed state should remain
		armedState, found := findTripleByPredicate(merged, "robotics.flight.armed")
		assert.True(t, found)
		assert.Equal(t, false, armedState.Object)
	})

	t.Run("empty slices", func(t *testing.T) {
		result := MergeTriples(nil, nil)
		assert.Nil(t, result)

		existing := []message.Triple{{Subject: "test", Predicate: "prop", Object: "value"}}
		result = MergeTriples(existing, nil)
		assert.Equal(t, existing, result)

		newer := []message.Triple{{Subject: "test", Predicate: "prop", Object: "new"}}
		result = MergeTriples(nil, newer)
		assert.Equal(t, newer, result)
	})

	t.Run("no conflicts", func(t *testing.T) {
		existing := []message.Triple{
			{Subject: "test", Predicate: "prop1", Object: "value1"},
		}
		newer := []message.Triple{
			{Subject: "test", Predicate: "prop2", Object: "value2"},
		}

		merged := MergeTriples(existing, newer)
		assert.Len(t, merged, 2)

		// Should contain both
		_, found1 := findTripleByPredicate(merged, "prop1")
		_, found2 := findTripleByPredicate(merged, "prop2")
		assert.True(t, found1)
		assert.True(t, found2)
	})
}

// Helper function for tests
func findTripleByPredicate(triples []message.Triple, predicate string) (message.Triple, bool) {
	for _, triple := range triples {
		if triple.Predicate == predicate {
			return triple, true
		}
	}
	return message.Triple{}, false
}