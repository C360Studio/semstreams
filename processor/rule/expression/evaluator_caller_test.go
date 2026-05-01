// Package expression - Evaluator tests for $caller.* pseudo-field conditions.
//
// These tests verify that $caller.* fields injected into StateFields via the
// stateFields map evaluate correctly, including the "field absent" semantic
// for nil/missing caller (returns false uniformly, matching $state.* behaviour).
package expression

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
)

// callerFields is a test helper that builds a StateFields map from explicit caller values.
func callerFields(id, role, org string) StateFields {
	return StateFields{
		"$caller.id":   id,
		"$caller.role": role,
		"$caller.org":  org,
	}
}

// TestEvaluator_CallerRoleEqualsAdmin verifies that a populated caller with role=admin
// satisfies an eq condition on $caller.role.
func TestEvaluator_CallerRoleEqualsAdmin(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("alice", "admin", "acme"), expr)
	require.NoError(t, err)
	assert.True(t, result, "$caller.role == admin with role=admin should be true")
}

// TestEvaluator_CallerRoleNotEqualsAdmin verifies that a caller with role=viewer satisfies
// a ne condition on $caller.role (role is present but is not admin).
func TestEvaluator_CallerRoleNotEqualsAdmin(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpNotEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("bob", "viewer", "acme"), expr)
	require.NoError(t, err)
	assert.True(t, result, "$caller.role != admin with role=viewer should be true")
}

// TestEvaluator_CallerRoleEqualsAdmin_NoCaller verifies the absent-field semantic:
// when no $caller.* keys are in stateFields, $caller.role == "admin" returns false.
// This prevents anonymous/unauthenticated requests from satisfying admin-only guards.
func TestEvaluator_CallerRoleEqualsAdmin_NoCaller(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	// Empty StateFields — no caller in scope (cron fire, KV-watch trigger, etc.)
	result, err := evaluator.EvaluateWithStateFields(nil, StateFields{}, expr)
	require.NoError(t, err)
	assert.False(t, result, "$caller.role == admin with no caller should be false (field absent)")
}

// TestEvaluator_CallerRoleNotEquals_NoCaller verifies that the absent-field semantic
// also returns false for ne conditions, not true. This is the conservative choice:
// a rule "deny if caller is not admin" must not deny anonymous cron/KV-watch fires.
func TestEvaluator_CallerRoleNotEquals_NoCaller(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpNotEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, StateFields{}, expr)
	require.NoError(t, err)
	assert.False(t, result, "$caller.role != admin with no caller should be false (field absent, not true)")
}

// TestEvaluator_CallerOrgInList verifies that the in operator works for $caller.org.
func TestEvaluator_CallerOrgInList(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.org", Operator: OpIn, Value: []interface{}{"acme", "widgets"}},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("carol", "viewer", "acme"), expr)
	require.NoError(t, err)
	assert.True(t, result, "$caller.org in [acme, widgets] with org=acme should be true")
}

// TestEvaluator_CallerOrgNotInList verifies that the in operator returns false
// when the org is not in the allowed list.
func TestEvaluator_CallerOrgNotInList(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.org", Operator: OpIn, Value: []interface{}{"acme", "widgets"}},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("dave", "viewer", "other"), expr)
	require.NoError(t, err)
	assert.False(t, result, "$caller.org in [acme, widgets] with org=other should be false")
}

// TestEvaluator_MixedStateAndCallerConditions verifies that a single AND condition list
// combining $state.* and $caller.* evaluates correctly when both are populated.
func TestEvaluator_MixedStateAndCallerConditions(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$state.iteration", Operator: OpLessThan, Value: 5.0},
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	sf := StateFields{
		"$state.iteration": 2,
		"$caller.id":       "alice",
		"$caller.role":     "admin",
		"$caller.org":      "acme",
	}

	result, err := evaluator.EvaluateWithStateFields(nil, sf, expr)
	require.NoError(t, err)
	assert.True(t, result, "iteration<5 AND role==admin should be true when both conditions hold")
}

// TestEvaluator_MixedStateAndCallerConditions_OneFails verifies AND short-circuits:
// when iteration is too high, the rule fails even if the caller is admin.
func TestEvaluator_MixedStateAndCallerConditions_OneFails(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$state.iteration", Operator: OpLessThan, Value: 5.0},
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	sf := StateFields{
		"$state.iteration": 10, // exceeds threshold
		"$caller.id":       "alice",
		"$caller.role":     "admin",
		"$caller.org":      "acme",
	}

	result, err := evaluator.EvaluateWithStateFields(nil, sf, expr)
	require.NoError(t, err)
	assert.False(t, result, "iteration>=5 should fail the AND condition even with admin role")
}

// TestEvaluator_CallerFieldUnknown verifies that an unrecognised $caller.* sub-field
// (not id/role/org) follows the absent-field semantic: returns false (same as $state.*
// with a key that was never populated).
func TestEvaluator_CallerFieldUnknown(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.bogus_field", Operator: OpEqual, Value: "x"},
		},
		Logic: LogicAnd,
	}

	// stateFields has $caller.id/role/org but NOT $caller.bogus_field.
	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("alice", "admin", "acme"), expr)
	require.NoError(t, err)
	assert.False(t, result, "$caller.bogus_field == x with unknown field should be false (field absent)")
}

// TestEvaluator_CallerFieldUnknown_Required verifies that an unknown $caller.* sub-field
// with Required=true returns an EvaluationError (same as $state.* required+absent).
func TestEvaluator_CallerFieldUnknown_Required(t *testing.T) {
	evaluator := NewExpressionEvaluator()
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.bogus_field", Operator: OpEqual, Value: "x", Required: true},
		},
		Logic: LogicAnd,
	}

	result, err := evaluator.EvaluateWithStateFields(nil, callerFields("alice", "admin", "acme"), expr)
	assert.Error(t, err, "required absent $caller.* field should return an error")
	assert.False(t, result)
	var evalErr *EvaluationError
	require.ErrorAs(t, err, &evalErr)
	assert.Contains(t, evalErr.Message, "required caller field not found")
}

// TestEvaluator_CallerWithEntityState verifies that $caller.* conditions work correctly
// alongside entity-state triple evaluation in the same expression.
func TestEvaluator_CallerWithEntityState(t *testing.T) {
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

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "robotics.battery.level", Operator: OpGreaterThan, Value: 80.0, Required: true},
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicAnd,
	}

	sf := callerFields("alice", "admin", "acme")
	result, err := evaluator.EvaluateWithStateFields(entityState, sf, expr)
	require.NoError(t, err)
	assert.True(t, result, "entity triple condition AND caller.role==admin should both pass")
}
