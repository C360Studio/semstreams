// Package expression - Tests for the count_in_window operator.
//
// Tests use an in-memory WindowStateReader stub so no NATS infrastructure
// is required. The stub implements the full bucketed-counter semantics
// (advance time, expire buckets) so the operator behaviour is fully
// exercised at the unit level.
//
// Real KV round-trip coverage lives in the parent rule package's
// window_tracker_test.go (including restart-recovery, cardinality caps,
// and the integration test against a real testcontainer NATS).
package expression

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----- In-memory stub -----

// stubWindowState is a minimal in-memory WindowStateReader for unit tests.
// It stores per-(ruleID, dimension) event timestamps and applies sliding-window
// pruning on every Increment, matching the bucketed-counter semantics of the
// real WindowTracker.
//
// cardinality: if > 0, Increment returns -1 when the unique dimension count
// exceeds the cap.
type stubWindowState struct {
	mu          sync.Mutex
	events      map[string][]time.Time // key: "ruleID:dim"
	cardinality int                    // 0 = unlimited
}

func newStubWindowState() *stubWindowState {
	return &stubWindowState{events: make(map[string][]time.Time)}
}

func (s *stubWindowState) Increment(_ context.Context, ruleID, dimension string, now time.Time, window time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := ruleID + ":" + dimension

	// Cardinality cap: reject new keys when at limit
	if s.cardinality > 0 {
		_, exists := s.events[key]
		if !exists && len(s.events) >= s.cardinality {
			return -1, nil
		}
	}

	// Append current timestamp
	s.events[key] = append(s.events[key], now)

	// Prune expired events (sliding window)
	cutoff := now.Add(-window)
	kept := s.events[key][:0]
	for _, ts := range s.events[key] {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
		}
	}
	s.events[key] = kept

	return len(s.events[key]), nil
}

// ----- Test helpers -----

// buildCountInWindowExpr builds a LogicalExpression for count_in_window tests.
// field: the dimension field (e.g. "$caller.id")
// comparator: "gt", "gte", etc.
// threshold: numeric threshold
// windowStr: e.g. "5m"
func buildCountInWindowExpr(field, comparator string, threshold float64, windowStr string) LogicalExpression {
	return LogicalExpression{
		Conditions: []ConditionExpression{
			{
				Field:    field,
				Operator: OpCountInWindow,
				Value: map[string]interface{}{
					"window":   windowStr,
					comparator: threshold,
				},
			},
		},
		Logic: LogicAnd,
	}
}

// ----- Tests -----

// TestCountInWindow_UnderThreshold_Returns_False verifies that a count below
// the threshold returns false.
func TestCountInWindow_UnderThreshold_Returns_False(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-1")
	now := time.Now()

	// Increment 5 times
	for i := 0; i < 5; i++ {
		stub.Increment(context.Background(), "rule-1", "alice", now, 5*time.Minute)
	}

	expr := buildCountInWindowExpr("$caller.id", "gt", 10, "5m")
	stateFields := StateFields{"$caller.id": "alice"}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.False(t, result, "count=5, gt:10 should be false")
}

// TestCountInWindow_OverThreshold_Returns_True verifies that a count above
// the threshold returns true.
func TestCountInWindow_OverThreshold_Returns_True(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-2")
	now := time.Now()

	// Increment 15 times (includes the one the operator will call)
	for i := 0; i < 14; i++ {
		stub.Increment(context.Background(), "rule-2", "alice", now, 5*time.Minute)
	}

	// The operator itself calls Increment once more → count=15
	expr := buildCountInWindowExpr("$caller.id", "gt", 10, "5m")
	stateFields := StateFields{"$caller.id": "alice"}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.True(t, result, "count=15, gt:10 should be true")
}

// TestCountInWindow_AtThreshold_Boundary verifies the exact-boundary semantics
// of gt vs gte at the threshold value.
func TestCountInWindow_AtThreshold_Boundary(t *testing.T) {
	t.Parallel()

	// count=10 against gt:10 → false (10 is not > 10)
	t.Run("gt_false_at_threshold", func(t *testing.T) {
		t.Parallel()
		stub := newStubWindowState()
		ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-3a")
		now := time.Now()

		for i := 0; i < 9; i++ {
			stub.Increment(context.Background(), "rule-3a", "bob", now, 5*time.Minute)
		}

		expr := buildCountInWindowExpr("$caller.id", "gt", 10, "5m")
		stateFields := StateFields{"$caller.id": "bob"}

		result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
		require.NoError(t, err)
		assert.False(t, result, "count=10, gt:10 should be false")
	})

	// count=10 against gte:10 → true (10 >= 10)
	t.Run("gte_true_at_threshold", func(t *testing.T) {
		t.Parallel()
		stub := newStubWindowState()
		ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-3b")
		now := time.Now()

		for i := 0; i < 9; i++ {
			stub.Increment(context.Background(), "rule-3b", "bob", now, 5*time.Minute)
		}

		expr := buildCountInWindowExpr("$caller.id", "gte", 10, "5m")
		stateFields := StateFields{"$caller.id": "bob"}

		result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
		require.NoError(t, err)
		assert.True(t, result, "count=10, gte:10 should be true")
	})
}

// TestCountInWindow_DistinctDimensions_DontInteract verifies that counts for
// different dimension values (e.g. "alice" vs "bob") are tracked independently.
func TestCountInWindow_DistinctDimensions_DontInteract(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-4")
	now := time.Now()

	// alice: 4 prior increments
	for i := 0; i < 4; i++ {
		stub.Increment(context.Background(), "rule-4", "alice", now, 5*time.Minute)
	}
	// bob: no prior increments

	// Query alice → count=5 (operator adds one more)
	aliceExpr := buildCountInWindowExpr("$caller.id", "gte", 5, "5m")
	aliceResult, err := ev.EvaluateWithStateFields(nil, StateFields{"$caller.id": "alice"}, aliceExpr)
	require.NoError(t, err)
	assert.True(t, aliceResult, "alice count=5 (4 prior + 1 from operator), gte:5 should be true")

	// Query bob → count=1 (operator adds one)
	bobExpr := buildCountInWindowExpr("$caller.id", "eq", 1, "5m")
	bobResult, err := ev.EvaluateWithStateFields(nil, StateFields{"$caller.id": "bob"}, bobExpr)
	require.NoError(t, err)
	assert.True(t, bobResult, "bob count=1, eq:1 should be true")
}

// TestCountInWindow_ExpiredBuckets_PrunedOnWrite verifies that events outside
// the window duration are not counted after the window elapses.
func TestCountInWindow_ExpiredBuckets_PrunedOnWrite(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-5")

	// Record one event in the past, beyond a 1-minute window
	past := time.Now().Add(-90 * time.Second)
	stub.Increment(context.Background(), "rule-5", "carol", past, time.Minute)

	// Now advance to "present" — the past event should be pruned
	// The operator fires and adds one fresh event; count should be 1 (not 2)
	expr := buildCountInWindowExpr("$caller.id", "eq", 1, "1m")
	stateFields := StateFields{"$caller.id": "carol"}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.True(t, result, "expired event pruned: count should be 1 (only the fresh event), eq:1 → true")
}

// TestCountInWindow_MissingField_ReturnsFalse verifies the absent-field semantic:
// when the dimension field is absent (nil in stateFields), no increment occurs
// and the operator returns false.
func TestCountInWindow_MissingField_ReturnsFalse(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-6")

	expr := buildCountInWindowExpr("$caller.id", "gte", 1, "5m")
	// No $caller.id in stateFields → absent field
	stateFields := StateFields{}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.False(t, result, "absent $caller.id field should return false (no increment)")
}

// TestCountInWindow_CardinalityCapHit_ReturnsFalse verifies that when the
// cardinality cap is hit, a new dimension is rejected and the operator
// returns false (conservative non-match).
func TestCountInWindow_CardinalityCapHit_ReturnsFalse(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	stub.cardinality = 1 // cap at 1 distinct dimension
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-7")
	now := time.Now()

	// "alice" takes the only slot
	stub.Increment(context.Background(), "rule-7", "alice", now, 5*time.Minute)

	// "bob" arrives — cap hit, expect -1 from stub → false from operator
	expr := buildCountInWindowExpr("$caller.id", "gte", 1, "5m")
	stateFields := StateFields{"$caller.id": "bob"}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.False(t, result, "cardinality cap hit: new dimension should return false")
}

// TestCountInWindow_LtComparator verifies the lt comparator works correctly.
func TestCountInWindow_LtComparator(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-8")

	// Operator adds one event → count=1, lt:5 → true
	expr := buildCountInWindowExpr("$caller.id", "lt", 5, "5m")
	stateFields := StateFields{"$caller.id": "dave"}

	result, err := ev.EvaluateWithStateFields(nil, stateFields, expr)
	require.NoError(t, err)
	assert.True(t, result, "count=1, lt:5 should be true")
}

// TestCountInWindow_InvalidConfig_ReturnsError verifies that a malformed
// value config (missing window) surfaces an error rather than silently failing.
func TestCountInWindow_InvalidConfig_ReturnsError(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-9")

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{
				Field:    "$caller.id",
				Operator: OpCountInWindow,
				Value:    map[string]interface{}{"gt": 10.0}, // missing "window"
			},
		},
		Logic: LogicAnd,
	}

	_, err := ev.EvaluateWithStateFields(nil, StateFields{"$caller.id": "eve"}, expr)
	require.Error(t, err, "missing window should return error")
	assert.Contains(t, err.Error(), "window")
}

// TestCountInWindow_WithNegate verifies that negate:true on count_in_window
// inverts the comparison result (chunk 2 contract: negate works on all operators).
func TestCountInWindow_WithNegate(t *testing.T) {
	t.Parallel()
	stub := newStubWindowState()
	ev := NewExpressionEvaluatorWithWindowReader(stub, "rule-10")
	now := time.Now()

	// 4 prior increments → operator adds 1 → count=5, gt:10 → false → negate → true
	for i := 0; i < 4; i++ {
		stub.Increment(context.Background(), "rule-10", "frank", now, 5*time.Minute)
	}

	expr := LogicalExpression{
		Conditions: []ConditionExpression{
			{
				Field:    "$caller.id",
				Operator: OpCountInWindow,
				Value:    map[string]interface{}{"window": "5m", "gt": 10.0},
				Negate:   true, // !false → true when under threshold
			},
		},
		Logic: LogicAnd,
	}

	result, err := ev.EvaluateWithStateFields(nil, StateFields{"$caller.id": "frank"}, expr)
	require.NoError(t, err)
	assert.True(t, result, "count=5, gt:10=false, negate → true")
}
