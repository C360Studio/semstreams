package expression

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/message"
	gtypes "github.com/c360/semstreams/types/graph"
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
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},  // false
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
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},  // false
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
					{Field: "robotics.battery.level", Operator: OpLessThan, Value: 50.0, Required: true},     // false
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
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true},    // true
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: true},     // missing - error
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
					{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true},     // true
					{Field: "robotics.wifi.strength", Operator: OpGreaterThan, Value: 0.5, Required: false},     // missing - false
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

// Helper function to create test entity states
func createTestEntity(entityID string, triples []message.Triple) *gtypes.EntityState {
	return &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   entityID,
			Type: "robotics.drone",
		},
		Triples:   triples,
		Version:   1,
		UpdatedAt: time.Now(),
	}
}