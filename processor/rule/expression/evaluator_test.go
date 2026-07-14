package expression

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

func TestExpressionEvaluator_NumericOperators(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create test entity with battery level
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.battery.level",
			Object:    85.5,
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	tests := []struct {
		name      string
		expr      LogicalExpression
		expected  bool
		shouldErr bool
	}{
		{
			name: "equal_numeric",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpEqual, Value: 85.5, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "not_equal_numeric",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpNotEqual, Value: 90.0, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "less_than",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 90.0, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "less_than_equal",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThanEqual, Value: 85.5, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "greater_than",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "greater_than_equal",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpGreaterThanEqual, Value: 85.5, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "battery_low_threshold",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThanEqual, Value: 20.0, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(entityState, tt.expr)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestExpressionEvaluator_StringOperators(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create test entity with status string
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.system.status",
			Object:    "ARMED_READY",
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	tests := []struct {
		name     string
		expr     LogicalExpression
		expected bool
	}{
		{
			name: "contains",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.system.status", Operator: OpContains, Value: "ARMED", Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "starts_with",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.system.status", Operator: OpStartsWith, Value: "ARMED", Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "ends_with",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.system.status", Operator: OpEndsWith, Value: "READY", Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "regex_match",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.system.status", Operator: OpRegexMatch, Value: "^ARMED_.*", Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(entityState, tt.expr)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpressionEvaluator_LogicOperators(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create test entity with multiple properties
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.battery.level",
			Object:    85.5,
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   "drone.001",
			Predicate: "robotics.system.status",
			Object:    "ARMED_READY",
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	tests := []struct {
		name     string
		expr     LogicalExpression
		expected bool
	}{
		{
			name: "and_both_true",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true},
					{Field: "robotics.system.status", Operator: OpContains, Value: "ARMED", Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "and_one_false",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},    // false
					{Field: "robotics.system.status", Operator: OpContains, Value: "ARMED", Required: true}, // true
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
		{
			name: "or_one_true",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},    // false
					{Field: "robotics.system.status", Operator: OpContains, Value: "ARMED", Required: true}, // true
				},
				Logic: LogicOr,
			},
			expected: true,
		},
		{
			name: "or_both_false",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},       // false
					{Field: "robotics.system.status", Operator: OpContains, Value: "DISARMED", Required: true}, // false
				},
				Logic: LogicOr,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(entityState, tt.expr)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpressionEvaluator_MissingFields(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create entity with only battery level (missing status)
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.battery.level",
			Object:    85.5,
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	tests := []struct {
		name      string
		expr      LogicalExpression
		expected  bool
		shouldErr bool
	}{
		{
			name: "required_field_missing",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: true},
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true, // Should error with fail-fast
		},
		{
			name: "optional_field_missing",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: false},
				},
				Logic: LogicAnd,
			},
			expected:  false, // Conservative: missing optional field = false
			shouldErr: false,
		},
		{
			name: "and_with_required_missing",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true}, // true
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: true},  // missing - error
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true,
		},
		{
			name: "and_with_optional_missing",
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true}, // true
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: false}, // missing - false
				},
				Logic: LogicAnd,
			},
			expected:  false, // true AND false = false
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(entityState, tt.expr)
			if tt.shouldErr {
				assert.Error(t, err)
				var evalErr *EvaluationError
				require.ErrorAs(t, err, &evalErr)
				assert.Contains(t, evalErr.Message, "required field not found")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestExpressionEvaluator_TypeConversion(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create entity with mixed types
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.battery.level",
			Object:    int64(85), // Integer that should compare with float
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	result, err := evaluator.Evaluate(entityState, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.5, Required: true},
		},
		Logic: LogicAnd,
	})

	assert.NoError(t, err)
	assert.True(t, result) // 85 > 80.5 should work with type conversion
}

func TestExpressionEvaluator_EmptyConditions(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	entityState := createTestEntity("drone.001", []message.Triple{})

	result, err := evaluator.Evaluate(entityState, LogicalExpression{
		Conditions: []ConditionExpression{},
		Logic:      LogicAnd,
	})

	assert.NoError(t, err)
	assert.True(t, result) // Empty conditions should pass
}

func TestExpressionEvaluator_UnsupportedOperator(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	entityState := createTestEntity("drone.001", []message.Triple{
		{
			Subject:   "drone.001",
			Predicate: "robotics.battery.level",
			Object:    85.5,
			Source:    "test",
			Timestamp: time.Now(),
		},
	})

	result, err := evaluator.Evaluate(entityState, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "robotics.battery.level", Operator: "invalid_op", Value: 80.0, Required: true},
		},
		Logic: LogicAnd,
	})

	assert.Error(t, err)
	assert.False(t, result)
	var evalErr *EvaluationError
	require.ErrorAs(t, err, &evalErr)
	assert.Contains(t, evalErr.Message, "unsupported operator")
}

// T054: Test hasTriple() function
func TestHasTriple(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		entity    *gtypes.EntityState
		predicate string
		expected  bool
	}{
		{
			name: "triple exists",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "proximity.near",
					Object:    "drone.002",
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "proximity.near",
			expected:  true,
		},
		{
			name: "triple does not exist",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "robotics.battery.level",
					Object:    85.5,
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "proximity.near",
			expected:  false,
		},
		{
			name:      "nil entity",
			entity:    nil,
			predicate: "proximity.near",
			expected:  false,
		},
		{
			name:      "empty triples",
			entity:    createTestEntity("drone.001", []message.Triple{}),
			predicate: "proximity.near",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.HasTriple(tt.entity, tt.predicate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// T055: Test getOutgoing() function
func TestGetOutgoing(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		entity    *gtypes.EntityState
		predicate string
		expected  []string
	}{
		{
			name: "single outgoing relationship",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "proximity.near",
					Object:    "c360.platform1.robotics.mav1.drone.002",
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "proximity.near",
			expected:  []string{"c360.platform1.robotics.mav1.drone.002"},
		},
		{
			name: "multiple outgoing relationships",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "proximity.near",
					Object:    "c360.platform1.robotics.mav1.drone.002",
					Source:    "test",
					Timestamp: time.Now(),
				},
				{
					Subject:   "drone.001",
					Predicate: "proximity.near",
					Object:    "c360.platform1.robotics.mav1.drone.003",
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "proximity.near",
			expected:  []string{"c360.platform1.robotics.mav1.drone.002", "c360.platform1.robotics.mav1.drone.003"},
		},
		{
			name: "no matching predicate",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "fleet.member_of",
					Object:    "c360.platform1.robotics.mav1.fleet.alpha",
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "proximity.near",
			expected:  []string{},
		},
		{
			name: "non-relationship triple (literal value)",
			entity: createTestEntity("drone.001", []message.Triple{
				{
					Subject:   "drone.001",
					Predicate: "robotics.battery.level",
					Object:    85.5,
					Source:    "test",
					Timestamp: time.Now(),
				},
			}),
			predicate: "robotics.battery.level",
			expected:  []string{},
		},
		{
			name:      "nil entity",
			entity:    nil,
			predicate: "proximity.near",
			expected:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.GetOutgoing(tt.entity, tt.predicate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// T056: Test distance() function
func TestDistance(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	// Create entities with position data (now stored as triples)
	entityWithPosition := func(id string, lat, lon float64) *gtypes.EntityState {
		return &gtypes.EntityState{
			ID: id,
			Triples: []message.Triple{
				{
					Subject:   id,
					Predicate: "geo.location.latitude",
					Object:    lat,
					Source:    "test",
					Timestamp: time.Now(),
				},
				{
					Subject:   id,
					Predicate: "geo.location.longitude",
					Object:    lon,
					Source:    "test",
					Timestamp: time.Now(),
				},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}
	}

	entity1 := entityWithPosition("drone.001", 40.7128, -74.0060)  // New York
	entity2 := entityWithPosition("drone.002", 34.0522, -118.2437) // Los Angeles
	entity3 := entityWithPosition("drone.003", 40.7128, -74.0060)  // Same as entity1
	entityNoPos := createTestEntity("drone.004", []message.Triple{})

	// Store entities in a map for lookup
	entities := map[string]*gtypes.EntityState{
		"drone.001": entity1,
		"drone.002": entity2,
		"drone.003": entity3,
		"drone.004": entityNoPos,
	}

	tests := []struct {
		name        string
		entity1ID   string
		entity2ID   string
		expectError bool
		minDistance float64 // Minimum expected distance (for range checks)
		maxDistance float64 // Maximum expected distance
	}{
		{
			name:        "different locations (NY to LA)",
			entity1ID:   "drone.001",
			entity2ID:   "drone.002",
			expectError: false,
			minDistance: 3900000, // ~3900 km
			maxDistance: 4000000, // ~4000 km
		},
		{
			name:        "same location",
			entity1ID:   "drone.001",
			entity2ID:   "drone.003",
			expectError: false,
			minDistance: 0,
			maxDistance: 1, // Allow tiny floating point error
		},
		{
			name:        "missing position data",
			entity1ID:   "drone.001",
			entity2ID:   "drone.004",
			expectError: true,
			minDistance: 0,
			maxDistance: 0,
		},
		{
			name:        "both missing position",
			entity1ID:   "drone.004",
			entity2ID:   "drone.004",
			expectError: true,
			minDistance: 0,
			maxDistance: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			distance, err := evaluator.Distance(entities[tt.entity1ID], entities[tt.entity2ID])

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, 0.0, distance)
			} else {
				assert.NoError(t, err)
				assert.GreaterOrEqual(t, distance, tt.minDistance)
				assert.LessOrEqual(t, distance, tt.maxDistance)
			}
		})
	}
}

func TestExtractLatLonIgnoresBarePredicateAliases(t *testing.T) {
	t.Parallel()
	entity := &gtypes.EntityState{Triples: []message.Triple{
		{Predicate: "geo.location.latitude", Object: 40.0},
		{Predicate: "geo.location.longitude", Object: -100.0},
		{Predicate: "latitude", Object: 1.0},
		{Predicate: "longitude", Object: 2.0},
	}}
	lat, lon := extractLatLonFromTriples(entity)
	if lat != 40 || lon != -100 {
		t.Fatalf("coordinates = (%v, %v), want canonical (40, -100)", lat, lon)
	}
}

func TestExpressionEvaluator_InOperator(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		entity    *gtypes.EntityState
		expr      LogicalExpression
		expected  bool
		shouldErr bool
	}{
		{
			name: "string in array - match",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "rejected", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpIn, Value: []interface{}{"rejected", "not_a_bug", "wont_fix"}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "string in array - no match",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "developing", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpIn, Value: []interface{}{"rejected", "not_a_bug", "wont_fix"}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
		{
			name: "numeric in array - match",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "error.code", Object: float64(404), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "error.code", Operator: OpIn, Value: []interface{}{float64(400), float64(404), float64(500)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "in with non-array value - error",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "rejected", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpIn, Value: "not_an_array", Required: true},
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true,
		},
		{
			name: "in with empty array",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "rejected", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpIn, Value: []interface{}{}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.entity, tt.expr)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpressionEvaluator_NotInOperator(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		entity    *gtypes.EntityState
		expr      LogicalExpression
		expected  bool
		shouldErr bool
	}{
		{
			name: "string not in array - true (not found)",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "developing", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpNotIn, Value: []interface{}{"rejected", "not_a_bug", "wont_fix"}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "string not in array - false (found)",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "rejected", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpNotIn, Value: []interface{}{"rejected", "not_a_bug", "wont_fix"}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
		{
			name: "not_in with non-array value - error",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.phase", Object: "developing", Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.phase", Operator: OpNotIn, Value: "not_an_array", Required: true},
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.entity, tt.expr)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpressionEvaluator_BetweenOperator(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		entity    *gtypes.EntityState
		expr      LogicalExpression
		expected  bool
		shouldErr bool
	}{
		{
			name: "value within range",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(250000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000), float64(500000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "value at lower bound (inclusive)",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(100000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000), float64(500000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "value at upper bound (inclusive)",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(500000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000), float64(500000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: true,
		},
		{
			name: "value below range",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(50000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000), float64(500000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
		{
			name: "value above range",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(600000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000), float64(500000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected: false,
		},
		{
			name: "between with non-array value - error",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(250000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: float64(100000), Required: true},
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true,
		},
		{
			name: "between with wrong array length - error",
			entity: createTestEntity("wf.001", []message.Triple{
				{Subject: "wf.001", Predicate: "workflow.tokens.total", Object: float64(250000), Source: "test", Timestamp: time.Now()},
			}),
			expr: LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "workflow.tokens.total", Operator: OpBetween, Value: []interface{}{float64(100000)}, Required: true},
				},
				Logic: LogicAnd,
			},
			expected:  false,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.entity, tt.expr)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpressionEvaluator_TransitionOperator(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	entity := createTestEntity("plan.001", []message.Triple{
		{Subject: "plan.001", Predicate: "workflow.plan.status", Object: "drafting", Source: "test", Timestamp: time.Now()},
	})

	tests := []struct {
		name        string
		stateFields StateFields
		condition   ConditionExpression
		expected    bool
		shouldErr   bool
	}{
		{
			name: "matching from/to transition",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created", "rejected"},
			},
			expected: true,
		},
		{
			name: "from as single value",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     "created",
			},
			expected: true,
		},
		{
			name: "from array with multiple allowed states",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "rejected",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created", "rejected"},
			},
			expected: true,
		},
		{
			name: "nil from - any change counts",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     nil,
			},
			expected: true,
		},
		{
			name: "nil from - same value is not a transition",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "drafting",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     nil,
			},
			expected: false,
		},
		{
			name: "current value does not match target",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "reviewing", // entity has "drafting", not "reviewing"
				From:     []interface{}{"created"},
			},
			expected: false,
		},
		{
			name: "previous value not in from set",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "completed",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created", "rejected"},
			},
			expected: false,
		},
		{
			name:        "first evaluation - no previous value",
			stateFields: StateFields{
				// no $prev entry
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created"},
			},
			expected: false,
		},
		{
			name: "missing field - not required",
			stateFields: StateFields{
				"$prev.nonexistent.field": "old",
			},
			condition: ConditionExpression{
				Field:    "nonexistent.field",
				Operator: OpTransition,
				Value:    "new",
				From:     []interface{}{"old"},
				Required: false,
			},
			expected: false,
		},
		{
			name: "nil entity state - not required",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created"},
				Required: false,
			},
			expected: false,
		},
		{
			name: "nil entity state - required",
			stateFields: StateFields{
				"$prev.workflow.plan.status": "created",
			},
			condition: ConditionExpression{
				Field:    "workflow.plan.status",
				Operator: OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created"},
				Required: true,
			},
			expected:  false,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEntity := entity
			if tt.name == "nil entity state - not required" || tt.name == "nil entity state - required" {
				testEntity = nil
			}

			expr := LogicalExpression{
				Conditions: []ConditionExpression{tt.condition},
				Logic:      LogicAnd,
			}

			result, err := evaluator.EvaluateWithStateAndMessage(testEntity, tt.stateFields, nil, expr)
			if tt.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result, "condition: %+v", tt.condition)
		})
	}
}

// Helper function to create test entity states
func createTestEntity(entityID string, triples []message.Triple) *gtypes.EntityState {
	return &gtypes.EntityState{
		ID:        entityID,
		Triples:   triples,
		Version:   1,
		UpdatedAt: time.Now(),
	}
}

// TestEvaluateWithStateAndMessage_MessageFieldExplicit pins that explicit
// $message.<path> tokens resolve from MessageFields and walk deep paths.
// This is the recommended form (ADR-041) — operators writing rules should
// reach for the explicit prefix when the resolution source matters.
func TestEvaluateWithStateAndMessage_MessageFieldExplicit(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		condition ConditionExpression
		message   MessageFields
		expected  bool
		shouldErr bool
	}{
		{
			name: "$message.command resolves top-level field",
			condition: ConditionExpression{
				Field:    "$message.command",
				Operator: OpContains,
				Value:    "cd /workspace",
			},
			message:  MessageFields{"command": "cd /workspace && ls", "tool_name": "bash"},
			expected: true,
		},
		{
			name: "$message.deep.path walks nested maps",
			condition: ConditionExpression{
				Field:    "$message.tool_args.command",
				Operator: OpEqual,
				Value:    "echo hello",
			},
			message: MessageFields{
				"tool_args": map[string]any{"command": "echo hello"},
			},
			expected: true,
		},
		{
			name: "missing $message field with Required errors",
			condition: ConditionExpression{
				Field:    "$message.absent",
				Operator: OpEqual,
				Value:    "x",
				Required: true,
			},
			message:   MessageFields{"command": "x"},
			shouldErr: true,
		},
		{
			name: "missing $message field without Required returns false",
			condition: ConditionExpression{
				Field:    "$message.absent",
				Operator: OpEqual,
				Value:    "x",
			},
			message:  MessageFields{"command": "x"},
			expected: false,
		},
		{
			name: "$message field with nil messageFields returns false",
			condition: ConditionExpression{
				Field:    "$message.command",
				Operator: OpEqual,
				Value:    "x",
			},
			message:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := LogicalExpression{Conditions: []ConditionExpression{tt.condition}, Logic: LogicAnd}
			result, err := evaluator.EvaluateWithStateAndMessage(nil, nil, tt.message, expr)
			if tt.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvaluateWithStateAndMessage_BareNameMessagePath pins that on message-path
// rules (entity nil), bare field names resolve from MessageFields. This is
// the asymmetry semspec hit on beta.71: action-When clauses on message-path
// rules previously couldn't see the payload because the evaluator skipped
// non-$state fields when entity was nil. ADR-041 closes this.
func TestEvaluateWithStateAndMessage_BareNameMessagePath(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	tests := []struct {
		name      string
		condition ConditionExpression
		message   MessageFields
		expected  bool
	}{
		{
			name: "bare command resolves from messageFields when entity is nil",
			condition: ConditionExpression{
				Field:    "command",
				Operator: OpContains,
				Value:    "cd /workspace",
			},
			message:  MessageFields{"command": "cd /workspace && ls"},
			expected: true,
		},
		{
			name: "bare tool_name eq matches",
			condition: ConditionExpression{
				Field:    "tool_name",
				Operator: OpEqual,
				Value:    "bash",
			},
			message:  MessageFields{"tool_name": "bash"},
			expected: true,
		},
		{
			name: "bare deep.path walks into nested maps",
			condition: ConditionExpression{
				Field:    "tool_args.command",
				Operator: OpEqual,
				Value:    "echo hi",
			},
			message:  MessageFields{"tool_args": map[string]any{"command": "echo hi"}},
			expected: true,
		},
		{
			name: "missing bare field returns false without Required",
			condition: ConditionExpression{
				Field:    "absent",
				Operator: OpEqual,
				Value:    "x",
			},
			message:  MessageFields{"command": "x"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := LogicalExpression{Conditions: []ConditionExpression{tt.condition}, Logic: LogicAnd}
			result, err := evaluator.EvaluateWithStateAndMessage(nil, nil, tt.message, expr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvaluateWithStateAndMessage_EntityPathBareNameUnchanged pins the
// backward-compat guarantee: entity-path rules with bare field names
// resolve from entity triples, unchanged from the pre-ADR-041 behaviour.
// This is the critical regression class — any existing rule that matches
// on `field: "battery.level"` against entity state must keep working.
func TestEvaluateWithStateAndMessage_EntityPathBareNameUnchanged(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	entity := createTestEntity("test.entity.001", []message.Triple{
		{Predicate: "battery.level", Object: 15.0},
		{Predicate: "status", Object: "active"},
	})

	tests := []struct {
		name      string
		condition ConditionExpression
		expected  bool
	}{
		{
			name: "bare predicate resolves from entity triple",
			condition: ConditionExpression{
				Field:    "battery.level",
				Operator: OpLessThan,
				Value:    20.0,
			},
			expected: true,
		},
		{
			name: "bare predicate string operator",
			condition: ConditionExpression{
				Field:    "status",
				Operator: OpEqual,
				Value:    "active",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := LogicalExpression{Conditions: []ConditionExpression{tt.condition}, Logic: LogicAnd}
			result, err := evaluator.EvaluateWithStateAndMessage(entity, nil, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvaluateWithStateAndMessage_EntityFallsThroughToMessage pins the
// precedence rule: when entity is non-nil but the bare field isn't on
// the entity, resolution falls through to messageFields. This is the
// "mixed source" case — a rule on an entity-state event that wants to
// match on the inbound event's metadata.
func TestEvaluateWithStateAndMessage_EntityFallsThroughToMessage(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	entity := createTestEntity("test.entity.001", []message.Triple{
		{Predicate: "battery.level", Object: 15.0},
	})

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "battery.level", Operator: OpLessThan, Value: 20.0},
			{Field: "command", Operator: OpContains, Value: "danger"}, // from messageFields
		},
		Logic: LogicAnd,
	}
	result, err := evaluator.EvaluateWithStateAndMessage(entity, nil, MessageFields{"command": "danger zone"}, expr)
	require.NoError(t, err)
	assert.True(t, result, "entity-resolved AND message-fallback should both match")
}

// TestEvaluateWithStateAndMessage_PrecedenceOrder pins the explicit
// precedence: $state.* > $message.* > entity triples > bare-from-message.
// Even when keys overlap across sources, the explicit namespace wins.
func TestEvaluateWithStateAndMessage_PrecedenceOrder(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	entity := createTestEntity("test.entity.001", []message.Triple{
		{Predicate: "shared_key", Object: "entity_value"},
	})

	// $message.shared_key explicit beats entity triple of same name.
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$message.shared_key", Operator: OpEqual, Value: "message_value"},
		},
		Logic: LogicAnd,
	}
	result, err := evaluator.EvaluateWithStateAndMessage(entity, nil, MessageFields{"shared_key": "message_value"}, expr)
	require.NoError(t, err)
	assert.True(t, result, "$message.* explicit must resolve from messageFields, not entity triple")

	// Bare shared_key with entity present resolves from entity, NOT
	// messageFields (entity wins for bare names on entity-path rules).
	expr2 := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "shared_key", Operator: OpEqual, Value: "entity_value"},
		},
		Logic: LogicAnd,
	}
	result2, err := evaluator.EvaluateWithStateAndMessage(entity, nil, MessageFields{"shared_key": "message_value"}, expr2)
	require.NoError(t, err)
	assert.True(t, result2, "bare name with entity present must resolve from entity triple")
}

// TestEvaluateWithStateAndMessage_StatePlusMessage exercises the
// canonical ADR-039 use case: action-When clauses that combine
// $state.iteration with $message.command. semspec's consolidated-rule
// pattern depends on this composition.
func TestEvaluateWithStateAndMessage_StatePlusMessage(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$state.iteration", Operator: OpLessThan, Value: 5},
			{Field: "$message.command", Operator: OpContains, Value: "cd /workspace"},
		},
		Logic: LogicAnd,
	}
	stateFields := StateFields{"$state.iteration": 2}
	messageFields := MessageFields{"command": "cd /workspace && rm -rf"}

	result, err := evaluator.EvaluateWithStateAndMessage(nil, stateFields, messageFields, expr)
	require.NoError(t, err)
	assert.True(t, result, "$state.* AND $message.* must compose in a single When clause")
}
