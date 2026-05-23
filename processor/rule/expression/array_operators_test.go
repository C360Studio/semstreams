package expression

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// #147 — these tests close the split-brain gap where length_eq /
// length_gt / length_lt / array_contains were listed by
// rule_factory.isValidOperator() but never registered in the
// evaluator's operator map. A rule author writing length_eq passed
// validation at config load and hit "unsupported operator" at
// runtime. The four operators are now wired with array-aware field
// resolution; tests assert each at unit-level and through an
// end-to-end Evaluate path that mirrors how a real rule would arrive.

// --- operator truth tables ---

func TestOperatorLengthEq(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		field interface{}
		cmp   interface{}
		want  bool
	}{
		{"zero/zero", []interface{}{}, 0, true},
		{"zero/zero nil field", nil, 0, true},
		{"three/three", []interface{}{"a", "b", "c"}, 3, true},
		{"three/four", []interface{}{"a", "b", "c"}, 4, false},
		{"native []string three/three", []string{"a", "b", "c"}, 3, true},
		{"single string field length-of-one", "single", 1, true},
		{"compare value as float (JSON unmarshal shape)", []interface{}{"a", "b"}, float64(2), true},
		{"compare value as string-encoded integer", []interface{}{"a"}, "1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := operatorLengthEq(tc.field, tc.cmp)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOperatorLengthGt(t *testing.T) {
	t.Parallel()
	got, err := operatorLengthGt([]interface{}{"a", "b", "c"}, 2)
	require.NoError(t, err)
	assert.True(t, got)
	got, err = operatorLengthGt([]interface{}{"a"}, 1)
	require.NoError(t, err)
	assert.False(t, got, "length 1 is not > 1")
}

func TestOperatorLengthLt(t *testing.T) {
	t.Parallel()
	got, err := operatorLengthLt([]interface{}{"a"}, 2)
	require.NoError(t, err)
	assert.True(t, got)
	got, err = operatorLengthLt([]interface{}{"a", "b"}, 2)
	require.NoError(t, err)
	assert.False(t, got, "length 2 is not < 2")
}

func TestOperatorArrayContains(t *testing.T) {
	t.Parallel()
	got, err := operatorArrayContains([]interface{}{"alpha", "beta", "gamma"}, "beta")
	require.NoError(t, err)
	assert.True(t, got)

	got, err = operatorArrayContains([]interface{}{"alpha", "gamma"}, "beta")
	require.NoError(t, err)
	assert.False(t, got)

	// Scalar field degenerates to a one-element list — useful when a
	// predicate fired exactly once.
	got, err = operatorArrayContains("single", "single")
	require.NoError(t, err)
	assert.True(t, got, "scalar field should degenerate to a one-element list")
}

// TestOperatorLengthEq_NonArrayShapeErrors checks the loudness contract:
// passing a non-array, non-scalar-string shape returns an error rather
// than silently returning false. Authoring bugs surface.
func TestOperatorLengthEq_NonArrayShapeErrors(t *testing.T) {
	t.Parallel()
	_, err := operatorLengthEq(42, 1)
	assert.Error(t, err, "integer fieldValue is not array-shaped — must error")
}

// --- end-to-end via Evaluate (closes the split-brain regression) ---

// TestEvaluator_LengthEqEndToEnd is the regression test for the
// validator/runtime split-brain: feeds a rule shape through Evaluate
// (the path a real rule fires through) and asserts a length_eq
// condition actually matches. Pre-#147 this would have returned
// "unsupported operator" at applyOperator. Locks the wiring in.
func TestEvaluator_LengthEqEndToEnd(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()

	// Trigger entity is a parent loop with 3 child-completion counter
	// triples stamped via the #147 subject-override path. The join
	// rule fires when the counter length matches the expected
	// subtopic count.
	entity := &gtypes.EntityState{
		Triples: []message.Triple{
			{Predicate: "gather.completed_subtopic", Object: "hydraulics"},
			{Predicate: "gather.completed_subtopic", Object: "pneumatics"},
			{Predicate: "gather.completed_subtopic", Object: "electrics"},
		},
	}

	got, err := ev.Evaluate(entity, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "gather.completed_subtopic", Operator: OpLengthEq, Value: 3},
		},
		Logic: "and",
	})
	require.NoError(t, err)
	assert.True(t, got, "length_eq must match when N triples carry the predicate and value==N")
}

// TestEvaluator_LengthEqMissingPredicateIsZero verifies the
// zero-length semantic on a predicate the entity doesn't carry at
// all. A rule that fires the join action when count==0 (e.g. "no
// children spawned yet") is legitimate; the operator returns true
// rather than erroring on the missing-predicate path.
func TestEvaluator_LengthEqMissingPredicateIsZero(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()

	entity := &gtypes.EntityState{
		Triples: []message.Triple{
			{Predicate: "agent.role", Object: "coordinator"},
		},
	}

	got, err := ev.Evaluate(entity, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "gather.completed_subtopic", Operator: OpLengthEq, Value: 0},
		},
		Logic: "and",
	})
	require.NoError(t, err)
	assert.True(t, got, "missing predicate must resolve to zero-length array for length_eq")
}

// TestEvaluator_ArrayContainsEndToEnd mirrors the regression closure
// for array_contains. Real rule shape: "did the gather phase complete
// for THIS specific subtopic before I act on it?"
func TestEvaluator_ArrayContainsEndToEnd(t *testing.T) {
	t.Parallel()
	ev := NewExpressionEvaluator()

	entity := &gtypes.EntityState{
		Triples: []message.Triple{
			{Predicate: "gather.completed_subtopic", Object: "hydraulics"},
			{Predicate: "gather.completed_subtopic", Object: "pneumatics"},
		},
	}

	got, err := ev.Evaluate(entity, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "gather.completed_subtopic", Operator: OpArrayContains, Value: "hydraulics"},
		},
		Logic: "and",
	})
	require.NoError(t, err)
	assert.True(t, got)

	// Negative case — predicate doesn't carry that value.
	got, err = ev.Evaluate(entity, LogicalExpression{
		Conditions: []ConditionExpression{
			{Field: "gather.completed_subtopic", Operator: OpArrayContains, Value: "electrics"},
		},
		Logic: "and",
	})
	require.NoError(t, err)
	assert.False(t, got)
}

// TestRegisteredOperators_NoSplitBrainBetweenDeclarationsAndWiring
// is the structural anti-foot for the #147 split-brain class.
// Iterates every Op* constant declared in expression/types.go and
// asserts each is registered in the evaluator's operator map (or is
// OpTransition, which is dispatched specially). A future contributor
// adding a constant without wiring fails this test at PR time
// instead of shipping a "validator accepts, evaluator rejects"
// runtime surprise.
func TestRegisteredOperators_NoSplitBrainBetweenDeclarationsAndWiring(t *testing.T) {
	t.Parallel()
	// Every Op* constant declared in types.go. When a new constant
	// is added, this list must be extended in the same PR — that's
	// the structural intent (additions don't ship until the wiring
	// is verified).
	declared := []string{
		OpEqual, OpNotEqual,
		OpLessThan, OpLessThanEqual, OpGreaterThan, OpGreaterThanEqual,
		OpContains, OpStartsWith, OpEndsWith, OpRegexMatch,
		OpIn, OpNotIn, OpBetween,
		OpLengthEq, OpLengthGt, OpLengthLt, OpArrayContains,
		OpTransition,
	}
	registered := RegisteredOperators()
	for _, op := range declared {
		_, ok := registered[op]
		assert.True(t, ok,
			"operator %q is declared in types.go but not registered in NewExpressionEvaluator — #147 split-brain class. Either add evaluator.operators[%s] = operator%sFunc OR remove the constant from types.go if it's not actually supported.",
			op, op, op)
	}
}

// TestIsArrayOperator covers the resolver-dispatch predicate. Locked
// in so a future operator addition can be reviewed against the
// truth table.
func TestIsArrayOperator(t *testing.T) {
	t.Parallel()
	for _, op := range []string{OpLengthEq, OpLengthGt, OpLengthLt, OpArrayContains} {
		assert.True(t, isArrayOperator(op), "%s should be array operator", op)
	}
	for _, op := range []string{OpEqual, OpContains, OpIn, OpRegexMatch, OpTransition, "made_up_operator"} {
		assert.False(t, isArrayOperator(op), "%s should NOT be array operator", op)
	}
}
