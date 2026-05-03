// Package rule - Production-path regression tests for count_in_window conditions.
//
// These tests exercise StatefulEvaluator.Evaluate end-to-end with a real
// WindowTracker wired via setWindowTracker. This is the critical path that
// the unit tests in expression/window_operator_test.go do NOT cover — those
// tests call NewExpressionEvaluatorWithWindowReader directly, bypassing the
// ExpressionRule.EvaluateEntityState → StatefulEvaluator wiring.
package rule

import (
	"context"
	"log/slog"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// TestStatefulEvaluator_CountInWindow_RealRule_FiresViaProductionPath verifies that a
// count_in_window rule with NO transition conditions and NO $caller.* conditions —
// the canonical rate-limiting shape — fires correctly on the KV-watch path.
//
// This test caught the C1 bug: StatefulEvaluator.Evaluate was missing the
// hasWindowConditions gate and the reEvaluateWithWindowReader pass. Without them,
// ExpressionRule.EvaluateEntityState returned false (it uses NewExpressionEvaluator,
// no WindowStateReader), and the rule silently never matched.
func TestStatefulEvaluator_CountInWindow_RealRule_FiresViaProductionPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stateBucket := newMockKVBucket()
	windowBucket := newCASKVBucket()

	logger := slog.Default()
	stateTracker := NewStateTracker(stateBucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	// Wire the WindowTracker — this is the fix under test.
	wt := NewWindowTracker(windowBucket, logger)
	evaluator.setWindowTracker(wt)

	// A pure count_in_window rule: no transition operator, no $caller.* fields.
	// The dimension field is an entity triple, resolved from entity state.
	ruleDef := Definition{
		ID:   "rate-limit-rule",
		Type: "expression",
		Name: "Rate Limit Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "request.user_id",
				Operator: expression.OpCountInWindow,
				Value: map[string]interface{}{
					"window": "5m",
					"gt":     float64(5), // fire after more than 5 calls
				},
			},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "alert.rate_limit_exceeded"},
		},
	}

	entityID := "acme.ops.api.gateway.request.001"

	// Build an EntityState that carries the dimension triple.
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "request.user_id", Object: "alice"},
		},
	}

	// Fire 5 times — count_in_window increments to 5 each time (gt:5 → false).
	// ExpressionRule.EvaluateEntityState always returns false (no WindowStateReader),
	// so CurrentlyMatching=false is the correct simulation of the upstream return value.
	for i := 0; i < 5; i++ {
		transition, err := evaluator.Evaluate(ctx, Evaluation{
			Rule:              ruleDef,
			EntityID:          entityID,
			Entity:            entity,
			CurrentlyMatching: false, // as returned by ExpressionRule.EvaluateEntityState
		})
		if err != nil {
			t.Fatalf("iteration %d: Evaluate() error = %v", i, err)
		}
		if transition != TransitionNone {
			t.Errorf("iteration %d: expected TransitionNone (under threshold), got %v", i, transition)
		}
	}
	// executeCallCount counts all action.Execute calls regardless of subject.
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("executeCallCount = %d after 5 fires (under gt:5 threshold), want 0", actionExecutor.executeCallCount)
	}

	// 6th fire — count_in_window increments to 6, gt:5 → true. The re-evaluation
	// pass must detect the match and fire OnEnter (TransitionEntered).
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		Entity:            entity,
		CurrentlyMatching: false, // still false from ExpressionRule path — the bug value
	})
	if err != nil {
		t.Fatalf("6th Evaluate() error = %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("6th fire: transition = %v, want TransitionEntered — count=6, gt:5 must fire via production path", transition)
	}
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("executeCallCount = %d after threshold exceeded, want 1 (OnEnter action must fire)", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_CountInWindow_NilTracker_DoesNotFire verifies the degraded
// startup path: when no WindowTracker is wired (nil), a count_in_window rule returns
// false (conservative non-match) rather than panicking or erroring.
func TestStatefulEvaluator_CountInWindow_NilTracker_DoesNotFire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stateBucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(stateBucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)
	// NOTE: setWindowTracker is intentionally NOT called — degraded mode.

	ruleDef := Definition{
		ID:   "rate-limit-degraded",
		Type: "expression",
		Name: "Rate Limit Degraded",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "request.user_id",
				Operator: expression.OpCountInWindow,
				Value:    map[string]interface{}{"window": "5m", "gte": float64(1)},
			},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "alert.rate_limit_exceeded"},
		},
	}

	entityID := "acme.ops.api.gateway.request.002"
	entity := &gtypes.EntityState{
		ID:      entityID,
		Triples: []message.Triple{{Subject: entityID, Predicate: "request.user_id", Object: "bob"}},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		Entity:            entity,
		CurrentlyMatching: false,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil (degraded mode must not error)", err)
	}
	if transition != TransitionNone {
		t.Errorf("transition = %v, want TransitionNone — nil tracker must produce conservative non-match", transition)
	}
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("executeCallCount = %d, want 0 — degraded mode must not fire actions", actionExecutor.executeCallCount)
	}
}
