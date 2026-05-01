// Package rule - Integration tests for $caller.* condition support in the stateful evaluator.
//
// These tests verify the full wiring: Evaluation.Caller → stateFields population →
// evaluateConditionWithState → correct fire/no-fire behaviour for rules that gate on
// caller identity fields.
package rule

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/auth"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// TestStatefulEvaluator_CallerCondition_AdminRoleFires verifies that a rule with a
// top-level $caller.role == "admin" condition fires OnEnter when the caller is admin.
func TestStatefulEvaluator_CallerCondition_AdminRoleFires(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "admin-only-rule",
		Type: "expression",
		Name: "Admin Only Rule",
		Conditions: []expression.ConditionExpression{
			{Field: "$caller.role", Operator: expression.OpEqual, Value: "admin"},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	caller := &CallerContext{ID: "alice", Role: "admin", Org: "acme"}

	// CurrentlyMatching=false here because ExpressionRule.Evaluate can't see $caller.*;
	// the stateful evaluator must detect hasCallerConditions and re-evaluate.
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-admin-001",
		CurrentlyMatching: false, // as delivered from ExpressionRule.Evaluate
		Caller:            caller,
	})

	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if transition != TransitionEntered {
		t.Errorf("transition = %v, want TransitionEntered (caller condition should re-evaluate to true)", transition)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("onEnterCalls = %d, want 1", actionExecutor.onEnterCalls)
	}
}

// TestStatefulEvaluator_CallerCondition_NonAdminNoFire verifies that a rule gating on
// $caller.role == "admin" does NOT fire when the caller has a non-admin role.
func TestStatefulEvaluator_CallerCondition_NonAdminNoFire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "admin-only-rule",
		Type: "expression",
		Name: "Admin Only Rule",
		Conditions: []expression.ConditionExpression{
			{Field: "$caller.role", Operator: expression.OpEqual, Value: "admin"},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	caller := &CallerContext{ID: "bob", Role: "viewer", Org: "acme"}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-viewer-001",
		CurrentlyMatching: false,
		Caller:            caller,
	})

	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if transition != TransitionNone {
		t.Errorf("transition = %v, want TransitionNone (viewer should not satisfy admin condition)", transition)
	}
	if actionExecutor.onEnterCalls != 0 {
		t.Errorf("onEnterCalls = %d, want 0", actionExecutor.onEnterCalls)
	}
}

// TestStatefulEvaluator_CallerCondition_NilCallerNoFire verifies the cron/KV-watch path:
// when no caller is present, a rule with $caller.* conditions must NOT fire.
// This is the critical safety invariant — anonymous fires must not accidentally satisfy
// an identity guard.
func TestStatefulEvaluator_CallerCondition_NilCallerNoFire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	// A rule that denies when caller is NOT admin: $caller.role != "admin"
	// With no caller, the field is absent → false → does not match → no fire.
	ruleDef := Definition{
		ID:   "deny-non-admin",
		Type: "expression",
		Name: "Deny Non Admin",
		Conditions: []expression.ConditionExpression{
			{Field: "$caller.role", Operator: expression.OpNotEqual, Value: "admin"},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-cron-001",
		CurrentlyMatching: false,
		Caller:            nil, // cron fire — no caller
	})

	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if transition != TransitionNone {
		t.Errorf("transition = %v, want TransitionNone (absent caller must not match $caller.* guards)", transition)
	}
	if actionExecutor.onEnterCalls != 0 {
		t.Errorf("onEnterCalls = %d, want 0 (cron fire must not trigger caller-guarded rule)", actionExecutor.onEnterCalls)
	}
}

// TestStatefulEvaluator_CallerCondition_OrgInList verifies that the in operator
// works for $caller.org conditions through the full stateful evaluator path.
func TestStatefulEvaluator_CallerCondition_OrgInList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "org-allowlist-rule",
		Type: "expression",
		Name: "Org Allowlist Rule",
		Conditions: []expression.ConditionExpression{
			{Field: "$caller.org", Operator: expression.OpIn, Value: []interface{}{"acme", "widgets"}},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	caller := &CallerContext{ID: "carol", Role: "viewer", Org: "acme"}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-org-001",
		CurrentlyMatching: false,
		Caller:            caller,
	})

	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if transition != TransitionEntered {
		t.Errorf("transition = %v, want TransitionEntered (acme is in allowlist)", transition)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("onEnterCalls = %d, want 1", actionExecutor.onEnterCalls)
	}
}

// TestEvaluateRulesForMessage_CallerConditionGates verifies the full ctx → message handler
// → stateful evaluator path: a ctx with auth.WithIdentity produces a Caller that satisfies
// a $caller.role condition, causing OnEnter to fire.
//
// This exercises the seam: auth.IdentityFromContext → identityToCallerContext →
// Evaluation.Caller → hasCallerConditions → reEvaluateWithCallerFields → correct transition.
func TestEvaluateRulesForMessage_CallerConditionGates(t *testing.T) {
	t.Parallel()

	// Build identity and inject into context.
	id := &auth.Identity{ID: "alice", Role: "admin", Org: "acme", Source: auth.SourceHTTPHeader}
	ctx := auth.WithIdentity(context.Background(), id)

	// callerCapture records ExecutionContext.Caller when OnEnter fires.
	var capturedCaller *CallerContext
	var capturedTransition Transition
	capture := &callerTransitionCapture{
		onExecute: func(ec *ExecutionContext, action Action) {
			if action.Subject == "test.entered" {
				capturedCaller = ec.Caller
			}
		},
	}

	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	evaluator := NewStatefulEvaluator(stateTracker, capture, logger)

	ruleDef := Definition{
		ID:   "ctx-role-rule",
		Type: "expression",
		Name: "Ctx Role Rule",
		Conditions: []expression.ConditionExpression{
			{Field: "$caller.role", Operator: expression.OpEqual, Value: "admin"},
		},
		Logic: expression.LogicAnd,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	// Simulate what evaluateRulesForMessage does: extract identity from ctx, convert.
	var caller *CallerContext
	if extracted, ok := auth.IdentityFromContext(ctx); ok {
		caller = identityToCallerContext(extracted)
	}

	var err error
	capturedTransition, err = evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-ctx-001",
		CurrentlyMatching: false, // ExpressionRule.Evaluate can't see $caller.*
		Caller:            caller,
	})

	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if capturedTransition != TransitionEntered {
		t.Errorf("transition = %v, want TransitionEntered; identity from ctx should satisfy $caller.role == admin", capturedTransition)
	}
	if capturedCaller == nil {
		t.Fatal("ExecutionContext.Caller was nil when OnEnter fired; expected alice's identity")
	}
	if capturedCaller.ID != "alice" {
		t.Errorf("Caller.ID = %q, want %q", capturedCaller.ID, "alice")
	}
	if capturedCaller.Role != "admin" {
		t.Errorf("Caller.Role = %q, want %q", capturedCaller.Role, "admin")
	}
}

// callerTransitionCapture is a test-only ActionExecutorInterface that invokes onExecute
// with both the ExecutionContext and Action so tests can inspect the caller and action type.
type callerTransitionCapture struct {
	onExecute func(ec *ExecutionContext, action Action)
}

func (c *callerTransitionCapture) Execute(_ context.Context, action Action, ec *ExecutionContext) error {
	if c.onExecute != nil {
		c.onExecute(ec, action)
	}
	return nil
}
