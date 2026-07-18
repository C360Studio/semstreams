// Package rule - Tests for stateful rule evaluation
package rule

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// TestStatefulEvaluator_Evaluate tests state-based action firing
func TestStatefulEvaluator_Evaluate(t *testing.T) {
	tests := []struct {
		name               string
		previousMatching   bool
		currentlyMatching  bool
		wantTransition     Transition
		wantOnEnterCalls   int
		wantOnExitCalls    int
		wantWhileTrueCalls int
	}{
		{
			name:              "false to true fires on_enter",
			previousMatching:  false,
			currentlyMatching: true,
			wantTransition:    TransitionEntered,
			wantOnEnterCalls:  1,
		},
		{
			name:              "true to false fires on_exit",
			previousMatching:  true,
			currentlyMatching: false,
			wantTransition:    TransitionExited,
			wantOnExitCalls:   1,
		},
		{
			name:               "true to true fires while_true",
			previousMatching:   true,
			currentlyMatching:  true,
			wantTransition:     TransitionNone,
			wantWhileTrueCalls: 1,
		},
		{
			name:              "false to false fires nothing",
			previousMatching:  false,
			currentlyMatching: false,
			wantTransition:    TransitionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			bucket := newMockKVBucket()
			logger := slog.Default()
			stateTracker := NewStateTracker(bucket, logger)
			actionExecutor := &mockActionExecutor{}
			evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

			ruleDef := Definition{
				ID:   "test-rule",
				Type: "expression",
				Name: "Test Rule",
				OnEnter: []Action{
					{Type: ActionTypePublish, Subject: "test.entered"},
				},
				OnExit: []Action{
					{Type: ActionTypePublish, Subject: "test.exited"},
				},
				WhileTrue: []Action{
					{Type: ActionTypePublish, Subject: "test.while-true"},
				},
			}

			entityID := semantictest.EntityID(t, "test", "rule", "stateful", "evaluation", "entity", "123")
			entityKey := entityID

			if tt.previousMatching {
				prevState := MatchState{
					RuleID:         ruleDef.ID,
					EntityKey:      entityKey,
					IsMatching:     true,
					LastTransition: string(TransitionEntered),
					TransitionAt:   time.Now().Add(-1 * time.Minute),
					LastChecked:    time.Now(),
				}
				if err := stateTracker.Set(ctx, prevState); err != nil {
					t.Fatalf("Failed to set previous state: %v", err)
				}
			}

			transition, err := evaluator.Evaluate(ctx, Evaluation{
				Rule:              ruleDef,
				EntityID:          entityID,
				CurrentlyMatching: tt.currentlyMatching,
			})

			if err != nil {
				t.Errorf("Evaluate() error = %v, want nil", err)
			}
			if transition != tt.wantTransition {
				t.Errorf("Evaluate() transition = %v, want %v", transition, tt.wantTransition)
			}
			if actionExecutor.onEnterCalls != tt.wantOnEnterCalls {
				t.Errorf("OnEnter actions called %d times, want %d", actionExecutor.onEnterCalls, tt.wantOnEnterCalls)
			}
			if actionExecutor.onExitCalls != tt.wantOnExitCalls {
				t.Errorf("OnExit actions called %d times, want %d", actionExecutor.onExitCalls, tt.wantOnExitCalls)
			}
			if actionExecutor.whileTrueCalls != tt.wantWhileTrueCalls {
				t.Errorf("WhileTrue actions called %d times, want %d", actionExecutor.whileTrueCalls, tt.wantWhileTrueCalls)
			}

			newState, err := stateTracker.Get(ctx, ruleDef.ID, entityKey)
			if err != nil {
				t.Errorf("Failed to get new state: %v", err)
			}
			if newState.IsMatching != tt.currentlyMatching {
				t.Errorf("Persisted state IsMatching = %v, want %v", newState.IsMatching, tt.currentlyMatching)
			}
			if newState.LastTransition != string(transition) {
				t.Errorf("Persisted state LastTransition = %v, want %v", newState.LastTransition, string(transition))
			}
		})
	}
}

// TestStatefulEvaluator_NoInitialState tests handling when no previous state exists
func TestStatefulEvaluator_NoInitialState(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "test-rule",
		Type: "expression",
		Name: "Test Rule",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "transition", "entity", "new"),
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("EvaluateWithState() error = %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("Expected TransitionEntered, got %v", transition)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("Expected OnEnter to be called once, got %d calls", actionExecutor.onEnterCalls)
	}
}

// TestStatefulEvaluator_PairRule tests pair rules with two entity IDs
func TestStatefulEvaluator_PairRule(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "pair-rule",
		Type: "pair",
		Name: "Pair Rule",
		OnEnter: []Action{
			{Type: ActionTypeAddTriple, Predicate: "test.fixture.related-to", Object: "$related.id"},
		},
	}

	entity1 := semantictest.EntityID(t, "test", "rule", "stateful", "pair", "entity", "a")
	entity2 := semantictest.EntityID(t, "test", "rule", "stateful", "pair", "entity", "b")

	transition1, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entity1,
		RelatedID:         entity2,
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("First evaluation error: %v", err)
	}
	if transition1 != TransitionEntered {
		t.Errorf("First transition = %v, want TransitionEntered", transition1)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("OnEnter calls = %d, want 1", actionExecutor.onEnterCalls)
	}

	expectedKey := buildPairKey(entity1, entity2)
	state, err := stateTracker.Get(ctx, ruleDef.ID, expectedKey)
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}
	if state.EntityKey != expectedKey {
		t.Errorf("State EntityKey = %v, want %v", state.EntityKey, expectedKey)
	}
}

// TestStatefulEvaluator_MultipleActions tests executing multiple actions
func TestStatefulEvaluator_MultipleActions(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "multi-action-rule",
		Type: "expression",
		Name: "Multi-Action Rule",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.action1"},
			{Type: ActionTypePublish, Subject: "test.action2"},
			{Type: ActionTypeAddTriple, Predicate: "test.fixture.status", Object: "active"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "actions", "entity", "multi"),
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("EvaluateWithState() error = %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("Expected TransitionEntered, got %v", transition)
	}
	if actionExecutor.executeCallCount != 3 {
		t.Errorf("Expected 3 action executions, got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_Iteration tests iteration tracking across transitions
func TestStatefulEvaluator_Iteration(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:            "iter-rule",
		Type:          "expression",
		Name:          "Iteration Rule",
		MaxIterations: 3,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "iteration", "entity", "001")

	// First entry: iteration should be 1
	_, err := evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
	if err != nil {
		t.Fatalf("First entry error: %v", err)
	}
	state, _ := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if state.Iteration != 1 {
		t.Errorf("After first entry: iteration = %d, want 1", state.Iteration)
	}
	if state.MaxIterations != 3 {
		t.Errorf("MaxIterations = %d, want 3", state.MaxIterations)
	}

	// Exit: iteration preserved
	_, err = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false})
	if err != nil {
		t.Fatalf("Exit error: %v", err)
	}
	state, _ = stateTracker.Get(ctx, ruleDef.ID, entityID)
	if state.Iteration != 1 {
		t.Errorf("After exit: iteration = %d, want 1 (preserved)", state.Iteration)
	}

	// Second entry: iteration should be 2
	_, err = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
	if err != nil {
		t.Fatalf("Second entry error: %v", err)
	}
	state, _ = stateTracker.Get(ctx, ruleDef.ID, entityID)
	if state.Iteration != 2 {
		t.Errorf("After second entry: iteration = %d, want 2", state.Iteration)
	}

	// Third entry: iteration should be 3
	_, _ = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false})
	_, err = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
	if err != nil {
		t.Fatalf("Third entry error: %v", err)
	}
	state, _ = stateTracker.Get(ctx, ruleDef.ID, entityID)
	if state.Iteration != 3 {
		t.Errorf("After third entry: iteration = %d, want 3", state.Iteration)
	}
}

// TestStatefulEvaluator_WhenClause tests conditional action execution
func TestStatefulEvaluator_WhenClause(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "when", "entity", "001")
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "test.review.verdict", Object: "approved"},
			{Subject: entityID, Predicate: "test.fixture.status", Object: "complete"},
		},
	}

	ruleDef := Definition{
		ID:   "when-rule",
		Type: "expression",
		Name: "When Rule",
		OnEnter: []Action{
			{
				Type:    ActionTypePublish,
				Subject: "test.approved",
				When: []expression.ConditionExpression{
					{Field: "test.review.verdict", Operator: "eq", Value: "approved"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.rejected",
				When: []expression.ConditionExpression{
					{Field: "test.review.verdict", Operator: "eq", Value: "rejected"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.always",
				// No When clause — always executes
			},
		},
	}

	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entity.ID,
		CurrentlyMatching: true,
		Entity:            entity,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	// Should execute: test.approved (matches) and test.always (no When)
	// Should skip: test.rejected (doesn't match)
	if actionExecutor.executeCallCount != 2 {
		t.Errorf("Expected 2 actions executed, got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_WhenWithStateFields tests $state.* fields in When clauses
func TestStatefulEvaluator_WhenWithStateFields(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:            "budget-rule",
		Type:          "expression",
		Name:          "Budget Rule",
		MaxIterations: 3,
		OnEnter: []Action{
			{
				Type:    ActionTypePublish,
				Subject: "test.retry",
				When: []expression.ConditionExpression{
					{Field: "$state.iteration", Operator: "lte", Value: float64(3)},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.escalate",
				When: []expression.ConditionExpression{
					{Field: "$state.iteration", Operator: "gt", Value: float64(3)},
				},
			},
		},
	}

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "budget", "entity", "001")

	// First 3 entries: retry action fires, escalate doesn't
	for i := range 3 {
		actionExecutor.executeCallCount = 0
		// Exit then enter to trigger TransitionEntered each time
		if i > 0 {
			_, _ = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false})
		}
		_, err := evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
		if err != nil {
			t.Fatalf("Entry %d error: %v", i+1, err)
		}
		if actionExecutor.executeCallCount != 1 {
			t.Errorf("Entry %d: expected 1 action (retry), got %d", i+1, actionExecutor.executeCallCount)
		}
	}

	// Fourth entry: escalate action fires, retry doesn't
	_, _ = evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false})
	actionExecutor.executeCallCount = 0
	_, err := evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
	if err != nil {
		t.Fatalf("Fourth entry error: %v", err)
	}
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("Fourth entry: expected 1 action (escalate), got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_WhenNilEntity tests When clause behavior with nil entity
func TestStatefulEvaluator_WhenNilEntity(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "nil-entity-rule",
		Type: "expression",
		Name: "Nil Entity Rule",
		OnEnter: []Action{
			{
				Type:    ActionTypePublish,
				Subject: "test.guarded",
				When: []expression.ConditionExpression{
					// This references a triple field but entity is nil
					{Field: "some.field", Operator: "eq", Value: "x"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.state-guarded",
				When: []expression.ConditionExpression{
					// $state.* fields work even without entity
					{Field: "$state.iteration", Operator: "eq", Value: float64(1)},
				},
			},
		},
	}

	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "nil", "entity", "001"),
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("EvaluateWithState() error = %v", err)
	}

	// guarded: entity is nil, triple field eval fails → skipped
	// state-guarded: $state.iteration == 1 → executes
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("Expected 1 action executed (state-guarded only), got %d", actionExecutor.executeCallCount)
	}
}

// mockActionExecutor tracks action execution calls for testing
type mockActionExecutor struct {
	onEnterCalls     int
	onExitCalls      int
	whileTrueCalls   int
	executeCallCount int
}

func (m *mockActionExecutor) Execute(_ context.Context, action Action, _ *ExecutionContext) error {
	m.executeCallCount++

	switch action.Subject {
	case "test.entered":
		m.onEnterCalls++
	case "test.exited":
		m.onExitCalls++
	case "test.while-true":
		m.whileTrueCalls++
	case "test.action1", "test.action2":
		m.onEnterCalls++
	}

	if action.Type == ActionTypeAddTriple {
		m.onEnterCalls++
	}

	return nil
}

// TestStatefulEvaluator_WhenMessagePayloadAccess is the ADR-041
// acceptance test: action-When clauses on message-path rules can match
// against the inbound message payload via `$message.<field>` tokens AND
// via bare field names (entity-is-nil resolution falls through to
// messageFields). This is what unlocks semspec's consolidated-rule
// governance pattern.
func TestStatefulEvaluator_WhenMessagePayloadAccess(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "message-when-rule",
		Type: "expression",
		Name: "Message-Path When Rule",
		OnEnter: []Action{
			{
				Type:    ActionTypePublish,
				Subject: "test.explicit-message",
				When: []expression.ConditionExpression{
					// Explicit $message.<field> form (recommended per ADR-041).
					{Field: "$message.command", Operator: "contains", Value: "cd /workspace"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.bare-name-message",
				When: []expression.ConditionExpression{
					// Bare name; entity is nil so resolves from messageFields.
					{Field: "tool_name", Operator: "eq", Value: "bash"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.deep-path",
				When: []expression.ConditionExpression{
					// Deep-path access into nested payload.
					{Field: "$message.tool_args.command", Operator: "starts_with", Value: "rm"},
				},
			},
			{
				Type:    ActionTypePublish,
				Subject: "test.no-match",
				When: []expression.ConditionExpression{
					{Field: "$message.command", Operator: "contains", Value: "safe"},
				},
			},
		},
	}

	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "message", "entity", "001"),
		CurrentlyMatching: true,
		MessageData: map[string]any{
			"command":   "cd /workspace && something",
			"tool_name": "bash",
			"tool_args": map[string]any{
				"command": "rm -rf /tmp/x",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	// Three matches (explicit, bare, deep-path); the "safe" one doesn't.
	if actionExecutor.executeCallCount != 3 {
		t.Errorf("Expected 3 actions executed (explicit + bare + deep-path matched; safe didn't), got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_WhenConsolidatedGovernancePattern exercises
// semspec's exact governance use case from
// `.semspec/semstreams-ask-action-when-message-payload.md`: one rule
// with publish+deny pairs guarded by $message.command patterns, plus a
// trailing unconditional approve fallback. With ADR-041, exactly one
// verdict per call by construction — no multi-rule race, no enforce-
// mode timeout false positives.
func TestStatefulEvaluator_WhenConsolidatedGovernancePattern(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "governance-bash-blocklist",
		Type: "expression",
		Name: "ADR-039 governance with consolidated blocklist",
		OnEnter: []Action{
			// Block "cd /workspace" pattern.
			{
				Type:    ActionTypePublish,
				Subject: "agent.toolcall.rejected.cd",
				When: []expression.ConditionExpression{
					{Field: "$message.command", Operator: "contains", Value: "cd /workspace"},
				},
			},
			// Trailing unconditional approve — only fires if no earlier
			// guarded action matched. With $message access in When, this
			// becomes a real composable fallback.
			{
				Type:    ActionTypePublish,
				Subject: "agent.toolcall.approved.fallback",
			},
		},
	}

	// Case 1: command matches blocklist → rejected fires + fallback also
	// fires (no deny short-circuit in this test since we're just
	// exercising the When-clause field-resolution path).
	actionExecutor.executeCallCount = 0
	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "governance", "entity", "001"),
		CurrentlyMatching: true,
		MessageData:       map[string]any{"command": "cd /workspace && ls", "tool_name": "bash"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if actionExecutor.executeCallCount != 2 {
		t.Errorf("Case 1: expected 2 actions (rejected + fallback approve), got %d", actionExecutor.executeCallCount)
	}

	// Case 2: command doesn't match → rejected skipped, only fallback fires.
	actionExecutor.executeCallCount = 0
	_, err = evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "governance", "entity", "002"),
		CurrentlyMatching: true,
		MessageData:       map[string]any{"command": "ls /tmp", "tool_name": "bash"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("Case 2: expected 1 action (fallback only), got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_TransitionFieldTracking verifies that FieldValues are
// persisted across evaluations and used by the transition operator.
func TestStatefulEvaluator_TransitionFieldTracking(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	ruleDef := Definition{
		ID:   "transition-rule",
		Type: "expression",
		Name: "Test Transition Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "workflow.plan.status",
				Operator: expression.OpTransition,
				Value:    "drafting",
				From:     []interface{}{"created", "rejected"},
			},
		},
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "transition", "plan", "001")
	entityCreated := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "workflow.plan.status", Object: "created"},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	entityDrafting := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "workflow.plan.status", Object: "drafting"},
		},
		Version:   2,
		UpdatedAt: time.Now(),
	}

	// First evaluation: entity has status "created"
	// The transition condition checks "is status transitioning to drafting?" — no, it's "created"
	// This should NOT match, but it should capture "created" in FieldValues
	transition1, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: false,
		Entity:            entityCreated,
	})
	if err != nil {
		t.Fatalf("First evaluation error: %v", err)
	}
	if transition1 != TransitionNone {
		t.Errorf("First evaluation: expected TransitionNone, got %v", transition1)
	}

	// Verify FieldValues were captured
	state1, err := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("Failed to get state after first eval: %v", err)
	}
	if state1.FieldValues == nil {
		t.Fatal("FieldValues should be captured after first evaluation")
	}
	if state1.FieldValues["workflow.plan.status"] != "created" {
		t.Errorf("Expected FieldValues[workflow.plan.status] = 'created', got %q", state1.FieldValues["workflow.plan.status"])
	}

	// Second evaluation: entity now has status "drafting"
	// The transition condition checks: current = "drafting" (matches Value),
	// previous = "created" (in From set) → MATCH
	transition2, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Entity:            entityDrafting,
	})
	if err != nil {
		t.Fatalf("Second evaluation error: %v", err)
	}
	if transition2 != TransitionEntered {
		t.Errorf("Second evaluation: expected TransitionEntered, got %v", transition2)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("Expected 1 OnEnter call, got %d", actionExecutor.onEnterCalls)
	}

	// Verify FieldValues updated to "drafting"
	state2, err := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("Failed to get state after second eval: %v", err)
	}
	if state2.FieldValues["workflow.plan.status"] != "drafting" {
		t.Errorf("Expected FieldValues[workflow.plan.status] = 'drafting', got %q", state2.FieldValues["workflow.plan.status"])
	}
}

// TestStatefulEvaluator_FieldValuesBackwardCompat verifies that existing MatchState
// without FieldValues still works (backward compatibility with persisted state).
func TestStatefulEvaluator_FieldValuesBackwardCompat(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)
	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "history", "entity", "old")

	// Simulate old persisted state without FieldValues
	oldState := MatchState{
		RuleID:         "test-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: "entered",
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now(),
		Iteration:      1,
		// FieldValues intentionally nil (old state format)
	}
	if err := stateTracker.Set(ctx, oldState); err != nil {
		t.Fatalf("Failed to set old state: %v", err)
	}

	ruleDef := Definition{
		ID:   "test-rule",
		Type: "expression",
		Name: "Backward Compat Rule",
		WhileTrue: []Action{
			{Type: ActionTypePublish, Subject: "test.while-true"},
		},
	}

	// Evaluate with existing state — should work fine with nil FieldValues
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Evaluation with old state error: %v", err)
	}
	if transition != TransitionNone {
		t.Errorf("Expected TransitionNone (true→true), got %v", transition)
	}
	if actionExecutor.whileTrueCalls != 1 {
		t.Errorf("Expected 1 WhileTrue call, got %d", actionExecutor.whileTrueCalls)
	}
}

// TestStatefulEvaluator_BootstrapRecovery_OnRecovery verifies that a rule with
// OnRecovery actions re-fires them during bootstrap when its persisted state
// says it was matching and the entity still matches.
func TestStatefulEvaluator_BootstrapRecovery_OnRecovery(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "recovery", "entity", "001")

	// Persist state that says the rule was matching before the crash.
	prev := MatchState{
		RuleID:         "recovery-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-5 * time.Minute),
		LastChecked:    time.Now().Add(-5 * time.Minute),
		Iteration:      1,
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:   "recovery-rule",
		Type: "expression",
		Name: "Recovery Rule",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
		OnRecovery: []Action{
			{Type: ActionTypePublish, Subject: "test.recovered"},
		},
	}

	// Bootstrap replay: condition still matches.
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Bootstrap:         true,
	})
	if err != nil {
		t.Fatalf("Bootstrap evaluation error: %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("Expected synthetic TransitionEntered on recovery, got %v", transition)
	}
	// OnEnter should NOT fire when OnRecovery is defined — they're separate lists.
	if actionExecutor.onEnterCalls != 0 {
		t.Errorf("Expected 0 OnEnter calls (OnRecovery preferred), got %d", actionExecutor.onEnterCalls)
	}
	// OnRecovery actions run through the same mock handler via executeCallCount.
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("Expected 1 OnRecovery execution, got %d", actionExecutor.executeCallCount)
	}

	// Second evaluation (non-bootstrap) with same state must NOT re-fire recovery.
	transition2, err := evaluator.Evaluate(ctx, Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true})
	if err != nil {
		t.Fatalf("Post-bootstrap evaluation error: %v", err)
	}
	if transition2 != TransitionNone {
		t.Errorf("Expected TransitionNone after bootstrap, got %v", transition2)
	}
}

// TestStatefulEvaluator_BootstrapRecovery_RerunOnRecoveryFlag verifies that a
// rule with only OnEnter and RerunOnRecovery=true re-fires OnEnter on restart.
func TestStatefulEvaluator_BootstrapRecovery_RerunOnRecoveryFlag(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "rerun", "entity", "001")

	prev := MatchState{
		RuleID:         "rerun-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now().Add(-1 * time.Minute),
		Iteration:      2,
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:              "rerun-rule",
		Type:            "expression",
		Name:            "Rerun Rule",
		RerunOnRecovery: true,
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Bootstrap:         true,
	})
	if err != nil {
		t.Fatalf("Bootstrap evaluation error: %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("Expected synthetic TransitionEntered on recovery, got %v", transition)
	}
	if actionExecutor.onEnterCalls != 1 {
		t.Errorf("Expected OnEnter to fire once on recovery, got %d", actionExecutor.onEnterCalls)
	}

	newState, err := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("Failed to read new state: %v", err)
	}
	if newState.Iteration != 3 {
		t.Errorf("Expected iteration to increment to 3 on recovery, got %d", newState.Iteration)
	}
}

// TestStatefulEvaluator_BootstrapNoRecoveryWithoutOptIn verifies that rules
// without OnRecovery or RerunOnRecovery do not re-fire on bootstrap (safe
// default: side effects like alert publishing must not repeat on restart).
func TestStatefulEvaluator_BootstrapNoRecoveryWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "optin", "entity", "none")
	prev := MatchState{
		RuleID:         "no-optin-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now().Add(-1 * time.Minute),
		Iteration:      1,
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:   "no-optin-rule",
		Type: "expression",
		Name: "No OptIn Rule",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
		WhileTrue: []Action{
			{Type: ActionTypePublish, Subject: "test.while-true"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Bootstrap:         true,
	})
	if err != nil {
		t.Fatalf("Bootstrap evaluation error: %v", err)
	}
	if transition != TransitionNone {
		t.Errorf("Expected TransitionNone for non-opted-in rule, got %v", transition)
	}
	if actionExecutor.onEnterCalls != 0 {
		t.Errorf("Expected 0 OnEnter calls without opt-in, got %d", actionExecutor.onEnterCalls)
	}
	// WhileTrue still fires because the rule still matches — that's expected.
	if actionExecutor.whileTrueCalls != 1 {
		t.Errorf("Expected WhileTrue to still fire, got %d", actionExecutor.whileTrueCalls)
	}
}

// TestStatefulEvaluator_ActionlessLiveRule_PersistsMatchStateWithoutSpuriousFiring
// is the gh#530 / rule-evaluation-completeness "consequential work" proof:
// an on_recovery-ONLY rule — OnEnter, OnExit, and WhileTrue are all
// nil/empty, only OnRecovery is populated — must, on a LIVE (non-
// bootstrap) first match:
//
//  1. persist MatchState with IsMatching=true (so a later bootstrap
//     replay has a "was matching" record to classify as recovery), and
//  2. fire ZERO actions (OnEnter is empty — nothing to fire; OnRecovery
//     only fires on the bootstrap-recovery fork, never on a plain live
//     Entered transition).
//
// This is distinct from TestStatefulEvaluator_BootstrapRecovery_OnRecovery
// above, which defines BOTH OnEnter and OnRecovery and only exercises the
// bootstrap leg. Here OnEnter is genuinely absent, proving
// hasStatefulRuleActions's OnRecovery-only admission doesn't depend on a
// non-empty OnEnter existing to fall back to.
func TestStatefulEvaluator_ActionlessLiveRule_PersistsMatchStateWithoutSpuriousFiring(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "actionless", "entity", "001")

	ruleDef := Definition{
		ID:   "actionless-recovery-rule",
		Type: "expression",
		Name: "Actionless Recovery Rule",
		OnRecovery: []Action{
			{Type: ActionTypePublish, Subject: "test.recovered"},
		},
	}

	// Sanity: this rule must actually reach the stateful evaluator via
	// the shared gate — otherwise the rest of this test would trivially
	// pass for the wrong reason (never evaluated at all).
	if !hasStatefulRuleActions(ruleDef) {
		t.Fatal("precondition failed: on_recovery-only rule must trip hasStatefulRuleActions")
	}

	// Live (non-bootstrap) first match — no prior state.
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Live evaluation error: %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("Expected TransitionEntered on first live match, got %v", transition)
	}
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("Expected 0 actions fired (OnEnter is empty, OnRecovery doesn't fire on live Entered), got %d", actionExecutor.executeCallCount)
	}

	persisted, err := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("Expected MatchState to be persisted after live match, got error: %v", err)
	}
	if !persisted.IsMatching {
		t.Error("Expected persisted MatchState.IsMatching=true after live match")
	}

	// Steady-state re-evaluation (still matching, not bootstrap): must
	// remain a no-op (WhileTrue is empty) — no spurious firing.
	transition2, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Steady-state evaluation error: %v", err)
	}
	if transition2 != TransitionNone {
		t.Errorf("Expected TransitionNone on steady-state re-evaluation, got %v", transition2)
	}
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("Expected 0 actions fired on steady-state (WhileTrue is empty), got %d", actionExecutor.executeCallCount)
	}

	// Simulated restart: bootstrap replay with the SAME persisted state
	// (via the same stateTracker/bucket) and still-matching entity must
	// now promote to recovery and fire OnRecovery exactly once.
	transition3, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Bootstrap:         true,
	})
	if err != nil {
		t.Fatalf("Bootstrap evaluation error: %v", err)
	}
	if transition3 != TransitionEntered {
		t.Errorf("Expected synthetic TransitionEntered on bootstrap recovery, got %v", transition3)
	}
	if actionExecutor.executeCallCount != 1 {
		t.Errorf("Expected exactly 1 OnRecovery execution on bootstrap, got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_StaleRevisionShortCircuits verifies that a rule
// evaluation skips without firing or persisting when the incoming revision is
// not newer than the last recorded SourceRevision. This is the durable defense
// against NATS watcher redelivery after a reconnect, once the in-memory
// ownRevisions map has been cleared by a process restart.
func TestStatefulEvaluator_StaleRevisionShortCircuits(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "revision", "entity", "stale")

	// Seed persisted state that already recorded revision 100.
	prev := MatchState{
		RuleID:         "stale-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now().Add(-1 * time.Minute),
		Iteration:      1,
		SourceRevision: 100,
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:   "stale-rule",
		Type: "expression",
		Name: "Stale Rule",
		WhileTrue: []Action{
			{Type: ActionTypePublish, Subject: "test.while-true"},
		},
	}

	// Redelivery of the same revision must short-circuit with no action fired.
	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Revision:          100,
	})
	if err != nil {
		t.Fatalf("Evaluate() error on redelivery: %v", err)
	}
	if transition != TransitionNone {
		t.Errorf("Expected TransitionNone on stale revision, got %v", transition)
	}
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("Expected no actions fired on stale revision, got %d", actionExecutor.executeCallCount)
	}

	// Also: an even older revision (out-of-order delivery) must short-circuit.
	transition, err = evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Revision:          50,
	})
	if err != nil {
		t.Fatalf("Evaluate() error on out-of-order: %v", err)
	}
	if transition != TransitionNone {
		t.Errorf("Expected TransitionNone on out-of-order revision, got %v", transition)
	}
	if actionExecutor.executeCallCount != 0 {
		t.Errorf("Expected no actions fired on out-of-order revision, got %d", actionExecutor.executeCallCount)
	}
}

// TestStatefulEvaluator_NewerRevisionFires verifies that a revision strictly
// greater than the recorded SourceRevision proceeds with normal evaluation and
// updates the persisted SourceRevision so subsequent equal/older deliveries
// are short-circuited.
func TestStatefulEvaluator_NewerRevisionFires(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "revision", "entity", "newer")

	prev := MatchState{
		RuleID:         "newer-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now().Add(-1 * time.Minute),
		Iteration:      1,
		SourceRevision: 100,
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:   "newer-rule",
		Type: "expression",
		Name: "Newer Rule",
		WhileTrue: []Action{
			{Type: ActionTypePublish, Subject: "test.while-true"},
		},
	}

	// Revision 200 > 100 → evaluation proceeds; WhileTrue fires once.
	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Revision:          200,
	})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if actionExecutor.whileTrueCalls != 1 {
		t.Errorf("Expected WhileTrue to fire once on newer revision, got %d", actionExecutor.whileTrueCalls)
	}

	// Persisted SourceRevision must have advanced to 200 so a redelivery of
	// 200 is now a stale replay.
	newState, err := stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}
	if newState.SourceRevision != 200 {
		t.Errorf("Expected SourceRevision=200 after evaluation, got %d", newState.SourceRevision)
	}

	// Redelivery of 200 is now stale → no additional WhileTrue fires.
	_, err = evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		Revision:          200,
	})
	if err != nil {
		t.Fatalf("Evaluate() error on redelivery: %v", err)
	}
	if actionExecutor.whileTrueCalls != 1 {
		t.Errorf("Expected WhileTrue to stay at 1 after redelivery, got %d", actionExecutor.whileTrueCalls)
	}
}

// TestStatefulEvaluator_ZeroRevisionSkipsGuard verifies that Revision=0 (the
// message-path case with no KV revision) bypasses the stale-replay guard
// entirely. Also covers backward compat: old persisted state with
// SourceRevision=0 must not block new evaluations whose Revision is also 0.
func TestStatefulEvaluator_ZeroRevisionSkipsGuard(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)
	actionExecutor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExecutor, logger)

	entityID := semantictest.EntityID(t, "test", "rule", "stateful", "revision", "entity", "zero")

	prev := MatchState{
		RuleID:         "zero-rule",
		EntityKey:      entityID,
		IsMatching:     true,
		LastTransition: string(TransitionEntered),
		TransitionAt:   time.Now().Add(-1 * time.Minute),
		LastChecked:    time.Now().Add(-1 * time.Minute),
		Iteration:      1,
		SourceRevision: 100, // persisted state knows about revision 100
	}
	if err := stateTracker.Set(ctx, prev); err != nil {
		t.Fatalf("Failed to seed previous state: %v", err)
	}

	ruleDef := Definition{
		ID:   "zero-rule",
		Type: "expression",
		Name: "Zero Rule",
		WhileTrue: []Action{
			{Type: ActionTypePublish, Subject: "test.while-true"},
		},
	}

	// Message-path evaluation: Revision=0 must NOT be compared against the
	// persisted SourceRevision=100. Evaluation proceeds normally.
	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          entityID,
		CurrentlyMatching: true,
		// Revision omitted → 0
	})
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if actionExecutor.whileTrueCalls != 1 {
		t.Errorf("Expected WhileTrue to fire on message-path eval, got %d", actionExecutor.whileTrueCalls)
	}
}

// sequencedExecutor is a test-only ActionExecutorInterface that returns errors
// from a pre-seeded sequence, one per Execute call in order. After the sequence
// is exhausted, subsequent calls succeed (return nil). It counts total calls so
// tests can assert on how many actions ran.
type sequencedExecutor struct {
	sequence []error
	calls    int
}

func (s *sequencedExecutor) Execute(_ context.Context, _ Action, _ *ExecutionContext) error {
	idx := s.calls
	s.calls++
	if idx < len(s.sequence) {
		return s.sequence[idx]
	}
	return nil
}

// TestRunActions_DenyShortCircuits verifies that when a deny action fires in the
// middle of an action list, subsequent actions do NOT run.
// Action list: [publish(allow), deny, publish(after-deny)]
// Expected: Execute is called exactly twice (allow + deny), not three times.
func TestRunActions_DenyShortCircuits(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)

	// deny is the second action (index 1); allow-after is the third (index 2)
	exec := &sequencedExecutor{
		sequence: []error{
			nil,                                      // action 0: allow passes
			&DenyVerdict{RuleID: "r1", Reason: "no"}, // action 1: deny
			// action 2 must NOT be called
		},
	}

	evaluator := NewStatefulEvaluator(stateTracker, exec, logger)
	ruleDef := Definition{
		ID:   "deny-short-circuit-rule",
		Type: "expression",
		Name: "Deny Short Circuit",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "allow.action"},
			{Type: ActionTypeDeny, Reason: "no"},
			{Type: ActionTypePublish, Subject: "after.deny"}, // must NOT run
		},
	}

	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "deny", "entity", "001"),
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil (deny is handled inside runActions, not surfaced)", err)
	}

	// Only 2 Execute calls: allow + deny. The third (after-deny) must not run.
	if exec.calls != 2 {
		t.Errorf("Execute call count = %d, want 2 (deny must short-circuit after-deny action)", exec.calls)
	}
}

// TestRunActions_NonDenyErrorContinues verifies that a non-deny error from one
// action does NOT stop subsequent actions. Today's best-effort semantics preserved.
// Action list: [failing_non_deny, allow_action_2]
// Expected: Execute is called exactly twice — fail then continue.
func TestRunActions_NonDenyErrorContinues(t *testing.T) {
	ctx := context.Background()
	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)

	exec := &sequencedExecutor{
		sequence: []error{
			errors.New("transient publish error"), // action 0: non-deny failure
			nil,                                   // action 1: succeeds
		},
	}

	evaluator := NewStatefulEvaluator(stateTracker, exec, logger)
	ruleDef := Definition{
		ID:   "non-deny-continue-rule",
		Type: "expression",
		Name: "Non-Deny Continue",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "first.fails"},
			{Type: ActionTypePublish, Subject: "second.runs"}, // must still run
		},
	}

	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          semantictest.EntityID(t, "test", "rule", "stateful", "deny", "entity", "002"),
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	// Both Execute calls must have fired — non-deny errors do not short-circuit.
	if exec.calls != 2 {
		t.Errorf("Execute call count = %d, want 2 (non-deny error must not short-circuit sibling actions)", exec.calls)
	}
}
