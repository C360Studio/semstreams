// Package expression - Tests for negate:true per-condition and logic:"not" rule-level.
//
// Design constraints (from ADR-032 chunk 2):
//   - negate:true inverts the operator's boolean result, not the operator semantics.
//   - negate:true on the transition operator is forbidden (ambiguous semantics).
//   - logic:"not" requires exactly ONE condition (rejects 0 or 2+).
//   - logic:"not" combined with a condition that has negate:true is rejected (double-negation anti-pattern).
//   - logic:"not" over a missing field: operator returns false, not flips to true.
//     This may surprise authors but is intentional — logic:"not" is a pure boolean
//     wrapper; it does not change the absent-field safety semantic at the operator level.
package expression

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// makeEntityWithField returns an EntityState with a single triple for the given
// predicate/object pair. Used across negate tests to avoid repetition.
func makeEntityWithField(predicate string, value interface{}) *gtypes.EntityState {
	return createTestEntity("test.org.domain.sys.type.001", []message.Triple{
		{
			Subject:   "test.org.domain.sys.type.001",
			Predicate: predicate,
			Object:    value,
			Source:    "test",
			Timestamp: time.Now(),
		},
	})
}

// TestNegate_EqOperator verifies that negate:true on eq inverts the result (acts as ne).
func TestNegate_EqOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("status", "active")

	tests := []struct {
		name   string
		negate bool
		want   bool
	}{
		{"eq_matches_no_negate", false, true},
		{"eq_matches_with_negate", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "status", Operator: OpEqual, Value: "active", Negate: tc.negate},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNegate_NeOperator verifies that negate:true on ne inverts the result (acts as eq).
func TestNegate_NeOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("status", "active")

	tests := []struct {
		name   string
		value  interface{}
		negate bool
		want   bool
	}{
		// ne("active", "inactive") → true; negated → false
		{"ne_no_match_no_negate", "inactive", false, true},
		{"ne_no_match_with_negate", "inactive", true, false},
		// ne("active", "active") → false; negated → true
		{"ne_match_no_negate", "active", false, false},
		{"ne_match_with_negate", "active", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "status", Operator: OpNotEqual, Value: tc.value, Negate: tc.negate},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNegate_NumericComparisonOperators verifies negate:true across lt, lte, gt, gte
// using a numeric field. Each sub-test picks a value where the base result is known,
// then confirms negate flips it.
func TestNegate_NumericComparisonOperators(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("level", 50.0)

	tests := []struct {
		name        string
		operator    string
		compareVal  interface{}
		wantNoNeg   bool
		wantWithNeg bool
	}{
		// lt: 50 < 60 → true; negated → false
		{"lt_true", OpLessThan, 60.0, true, false},
		// lt: 50 < 40 → false; negated → true
		{"lt_false", OpLessThan, 40.0, false, true},
		// lte: 50 <= 50 → true; negated → false
		{"lte_true", OpLessThanEqual, 50.0, true, false},
		// lte: 50 <= 49 → false; negated → true
		{"lte_false", OpLessThanEqual, 49.0, false, true},
		// gt: 50 > 40 → true; negated → false
		{"gt_true", OpGreaterThan, 40.0, true, false},
		// gt: 50 > 60 → false; negated → true
		{"gt_false", OpGreaterThan, 60.0, false, true},
		// gte: 50 >= 50 → true; negated → false
		{"gte_true", OpGreaterThanEqual, 50.0, true, false},
		// gte: 50 >= 51 → false; negated → true
		{"gte_false", OpGreaterThanEqual, 51.0, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exprBase := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "level", Operator: tc.operator, Value: tc.compareVal, Negate: false},
				},
				Logic: LogicAnd,
			}
			gotBase, err := ev.EvaluateWithStateFields(entity, nil, exprBase)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, gotBase, "base (no negate)")

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "level", Operator: tc.operator, Value: tc.compareVal, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg, "with negate:true")
		})
	}
}

// TestNegate_InOperator verifies that negate:true on in inverts to "not in array".
func TestNegate_InOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("role", "viewer")

	tests := []struct {
		name        string
		compareVal  interface{}
		wantNoNeg   bool
		wantWithNeg bool
	}{
		// "viewer" in ["admin","viewer"] → true; negated → false
		{"in_member", []interface{}{"admin", "viewer"}, true, false},
		// "viewer" in ["admin","owner"] → false; negated → true
		{"not_in_member", []interface{}{"admin", "owner"}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exprBase := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "role", Operator: OpIn, Value: tc.compareVal, Negate: false},
				},
				Logic: LogicAnd,
			}
			gotBase, err := ev.EvaluateWithStateFields(entity, nil, exprBase)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, gotBase)

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "role", Operator: OpIn, Value: tc.compareVal, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg)
		})
	}
}

// TestNegate_NotInOperator verifies that negate:true on not_in double-negates correctly
// (not_in is already a negation; negate:true makes it act like plain "in").
func TestNegate_NotInOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("role", "viewer")

	// not_in("viewer", ["admin","owner"]) → true; negated → false
	exprBase := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "role", Operator: OpNotIn, Value: []interface{}{"admin", "owner"}, Negate: false},
		},
		Logic: LogicAnd,
	}
	gotBase, err := ev.EvaluateWithStateFields(entity, nil, exprBase)
	require.NoError(t, err)
	assert.True(t, gotBase, "not_in base should be true")

	exprNeg := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "role", Operator: OpNotIn, Value: []interface{}{"admin", "owner"}, Negate: true},
		},
		Logic: LogicAnd,
	}
	gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
	require.NoError(t, err)
	assert.False(t, gotNeg, "negate:true on not_in should flip to false")
}

// TestNegate_ContainsOperator verifies negate:true on contains.
func TestNegate_ContainsOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("message", "hello world")

	// contains → true; negated → false
	exprBase := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "message", Operator: OpContains, Value: "world", Negate: false},
		},
		Logic: LogicAnd,
	}
	gotBase, err := ev.EvaluateWithStateFields(entity, nil, exprBase)
	require.NoError(t, err)
	assert.True(t, gotBase)

	exprNeg := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "message", Operator: OpContains, Value: "world", Negate: true},
		},
		Logic: LogicAnd,
	}
	gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
	require.NoError(t, err)
	assert.False(t, gotNeg)

	// contains → false; negated → true
	exprMiss := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "message", Operator: OpContains, Value: "goodbye", Negate: false},
		},
		Logic: LogicAnd,
	}
	gotMiss, err := ev.EvaluateWithStateFields(entity, nil, exprMiss)
	require.NoError(t, err)
	assert.False(t, gotMiss)

	exprMissNeg := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "message", Operator: OpContains, Value: "goodbye", Negate: true},
		},
		Logic: LogicAnd,
	}
	gotMissNeg, err := ev.EvaluateWithStateFields(entity, nil, exprMissNeg)
	require.NoError(t, err)
	assert.True(t, gotMissNeg)
}

// TestNegate_StartsWithOperator verifies negate:true on starts_with.
func TestNegate_StartsWithOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("path", "/api/v1/users")

	tests := []struct {
		name        string
		prefix      string
		wantNoNeg   bool
		wantWithNeg bool
	}{
		{"matches", "/api", true, false},
		{"no_match", "/admin", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "path", Operator: OpStartsWith, Value: tc.prefix, Negate: false},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, got)

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "path", Operator: OpStartsWith, Value: tc.prefix, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg)
		})
	}
}

// TestNegate_EndsWithOperator verifies negate:true on ends_with.
func TestNegate_EndsWithOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("filename", "report.pdf")

	tests := []struct {
		name        string
		suffix      string
		wantNoNeg   bool
		wantWithNeg bool
	}{
		{"matches", ".pdf", true, false},
		{"no_match", ".docx", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "filename", Operator: OpEndsWith, Value: tc.suffix, Negate: false},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, got)

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "filename", Operator: OpEndsWith, Value: tc.suffix, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg)
		})
	}
}

// TestNegate_RegexOperator verifies negate:true on regex.
func TestNegate_RegexOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("email", "user@example.com")

	tests := []struct {
		name        string
		pattern     string
		wantNoNeg   bool
		wantWithNeg bool
	}{
		{"matches_email_pattern", `^[a-z]+@example\.com$`, true, false},
		{"no_match_admin_domain", `^admin@`, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "email", Operator: OpRegexMatch, Value: tc.pattern, Negate: false},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, got)

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "email", Operator: OpRegexMatch, Value: tc.pattern, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg)
		})
	}
}

// TestNegate_BetweenOperator verifies negate:true on between.
func TestNegate_BetweenOperator(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("score", 75.0)

	tests := []struct {
		name        string
		bounds      interface{}
		wantNoNeg   bool
		wantWithNeg bool
	}{
		// 75 between [50,100] → true; negated → false
		{"in_range", []interface{}{50.0, 100.0}, true, false},
		// 75 between [80,100] → false; negated → true
		{"out_of_range", []interface{}{80.0, 100.0}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "score", Operator: OpBetween, Value: tc.bounds, Negate: false},
				},
				Logic: LogicAnd,
			}
			got, err := ev.EvaluateWithStateFields(entity, nil, expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNoNeg, got)

			exprNeg := LogicalExpression{
				Conditions: []ConditionExpression{
					{Field: "score", Operator: OpBetween, Value: tc.bounds, Negate: true},
				},
				Logic: LogicAnd,
			}
			gotNeg, err := ev.EvaluateWithStateFields(entity, nil, exprNeg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWithNeg, gotNeg)
		})
	}
}

// TestNegate_OnStateField verifies that negate:true also works for $state.* fields.
func TestNegate_OnStateField(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	sf := StateFields{"$state.iteration": 3.0}

	// eq → true; negated → false
	exprBase := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$state.iteration", Operator: OpEqual, Value: 3.0, Negate: false},
		},
		Logic: LogicAnd,
	}
	gotBase, err := ev.EvaluateWithStateFields(nil, sf, exprBase)
	require.NoError(t, err)
	assert.True(t, gotBase)

	exprNeg := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$state.iteration", Operator: OpEqual, Value: 3.0, Negate: true},
		},
		Logic: LogicAnd,
	}
	gotNeg, err := ev.EvaluateWithStateFields(nil, sf, exprNeg)
	require.NoError(t, err)
	assert.False(t, gotNeg, "negate:true on matching $state.eq should return false")
}

// TestNegate_OnCallerField verifies that negate:true also works for $caller.* fields.
func TestNegate_OnCallerField(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	sf := callerFields("alice", "admin", "acme")

	// eq admin → true; negated → false
	exprBase := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin", Negate: false},
		},
		Logic: LogicAnd,
	}
	gotBase, err := ev.EvaluateWithStateFields(nil, sf, exprBase)
	require.NoError(t, err)
	assert.True(t, gotBase)

	exprNeg := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin", Negate: true},
		},
		Logic: LogicAnd,
	}
	gotNeg, err := ev.EvaluateWithStateFields(nil, sf, exprNeg)
	require.NoError(t, err)
	assert.False(t, gotNeg, "negate:true on matching $caller.eq should return false")
}

// TestEvaluator_LogicNot_SingleCondition verifies that logic:"not" over a single
// matching condition returns false, and over a non-matching condition returns true.
func TestEvaluator_LogicNot_SingleCondition(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("status", "active")

	// condition matches (status == "active") → logic:not inverts → false
	exprMatchNot := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "status", Operator: OpEqual, Value: "active"},
		},
		Logic: LogicNot,
	}
	got, err := ev.EvaluateWithStateFields(entity, nil, exprMatchNot)
	require.NoError(t, err)
	assert.False(t, got, "logic:not over matching condition should return false")

	// condition does not match (status == "inactive") → logic:not inverts → true
	exprNoMatchNot := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "status", Operator: OpEqual, Value: "inactive"},
		},
		Logic: LogicNot,
	}
	got2, err := ev.EvaluateWithStateFields(entity, nil, exprNoMatchNot)
	require.NoError(t, err)
	assert.True(t, got2, "logic:not over non-matching condition should return true")
}

// TestEvaluator_LogicNot_MultipleConditions_ReturnsError verifies that logic:"not"
// with more than one condition returns an error (ambiguous semantics at eval time).
func TestEvaluator_LogicNot_MultipleConditions_ReturnsError(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()
	entity := makeEntityWithField("status", "active")

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "status", Operator: OpEqual, Value: "active"},
			{Field: "status", Operator: OpEqual, Value: "inactive"},
		},
		Logic: LogicNot,
	}
	_, err := ev.EvaluateWithStateFields(entity, nil, expr)
	assert.Error(t, err, "logic:not with 2 conditions should return an error")
}

// TestEvaluator_LogicNot_AbsentField verifies the absent-field semantic under logic:"not".
//
// When the operator evaluates an absent field it returns false (the universal
// absent-field safety net). logic:"not" then inverts to true. This may feel
// counter-intuitive ("not found" → true) but is consistent with logic:"not"
// being a pure boolean wrapper. Authors who want "absent field is NOT active"
// should use the Required:false + negate:true combination on a plain condition
// instead of logic:"not". The comment in the implementation cross-references
// this design note.
func TestEvaluator_LogicNot_AbsentField(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()

	// Entity has no "role" field; $caller.role == "admin" returns false;
	// logic:not flips to true.
	sf := StateFields{"$caller.role": "viewer"}
	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicNot,
	}
	got, err := ev.EvaluateWithStateFields(nil, sf, expr)
	require.NoError(t, err)
	assert.True(t, got, "logic:not over non-matching $caller.role should be true (not false)")

	// Absent $caller.* key: operator returns false; logic:not → true.
	exprAbsent := LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "$caller.role", Operator: OpEqual, Value: "admin"},
		},
		Logic: LogicNot,
	}
	got2, err := ev.EvaluateWithStateFields(nil, StateFields{}, exprAbsent)
	require.NoError(t, err)
	assert.True(t, got2, "logic:not over absent $caller key (field-absent false) should be true")
}
