// Package rule — tests for $entity.lifecycle.* substitution +
// condition.Field resolution (ADR-047).
//
// Coverage:
//   - SubstituteVariables resolves $entity.lifecycle.{phase,terminal,workflow}
//     against the wired Manager.
//   - PopulateLifecycleStateFields populates the evaluator's stateFields
//     map so condition.Field references resolve at evaluation time.
//   - Nil-manager / unknown-entity cases leave tokens verbatim (loud
//     unresolved-template signal).
//   - LookupByEntityID is memoized per ExecutionContext so multi-template
//     rules pay the scan cost at most once per fire.
package rule

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// countingManager wraps fakeLifecycleManager with a LookupByEntityID
// hit counter, used to verify the memoization in ExecutionContext.
type countingManager struct {
	*fakeLifecycleManager
	lookups atomic.Int64
}

func (c *countingManager) LookupByEntityID(ctx context.Context, entityID string) (lifecycle.Participant, error) {
	c.lookups.Add(1)
	return c.fakeLifecycleManager.LookupByEntityID(ctx, entityID)
}

func newCountingManager() *countingManager {
	return &countingManager{fakeLifecycleManager: newFakeManager()}
}

// --- SubstituteVariables ---

func TestSubstitute_LifecyclePhase(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "phase", "entity", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying", TerminalF: false})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("phase=$entity.lifecycle.phase")
	if got != "phase=flying" {
		t.Errorf("got %q, want phase=flying", got)
	}
}

func TestSubstitute_LifecycleTerminal(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "terminal", "entity", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "completed", TerminalF: true})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("t=$entity.lifecycle.terminal")
	if got != "t=true" {
		t.Errorf("got %q, want t=true", got)
	}
}

func TestSubstitute_LifecycleWorkflow(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "workflow", "entity", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("w=$entity.lifecycle.workflow")
	if got != "w=mission" {
		t.Errorf("got %q, want w=mission", got)
	}
}

func TestSubstitute_LifecycleWorkflowAndWorkflowDef_PrefixOrdering(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "definition", "entity", "001")
	// Verify that .workflow_def is substituted BEFORE .workflow so the
	// longer prefix wins. A naive ReplaceAll order would substitute
	// `.workflow` first, leaving `_def` as a dangling literal suffix.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})
	mgr.workflowDefs["mission"] = lifecycle.WorkflowDef{
		Workflow:        "mission",
		EntityIDPattern: "*.test.lifecycle.gcs.mission.*",
		PhasePredicate:  "test.mission.phase",
	}

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("w=$entity.lifecycle.workflow|def=$entity.lifecycle.workflow_def")
	if !strings.HasPrefix(got, "w=mission|def={") {
		t.Errorf("workflow_def prefix-ordering broken, got %q", got)
	}
}

func TestSubstitute_WorkflowDefLookupFailureLeavesTokenVerbatim(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "definition", "entity", "missing")
	// Loud-fail discipline: when workflow_def resolution fails, the
	// surviving token must come back unchanged so the unresolved-
	// template warning catches it. Silent empty-string substitution
	// would defeat the warning ([[feedback_no_clean_except_handwave]]).
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})
	// Note: no entry in mgr.workflowDefs for "mission" → GetWorkflowDefinition errors.

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("def=$entity.lifecycle.workflow_def")
	if got != "def=$entity.lifecycle.workflow_def" {
		t.Errorf("workflow_def failure must leave token verbatim, got %q", got)
	}
}

func TestSubstitute_WorkflowDefFailureDoesNotCorruptWorkflow(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "definition", "entity", "corrupt")
	// Regression guard against the prefix-ordering trap: when
	// .workflow_def resolution fails (token survives) AND the template
	// references .workflow separately, the .workflow substitution must
	// NOT mangle the surviving .workflow_def token into
	// `$entity.lifecycle.<workflowName>_def`. Verified by the
	// placeholder-swap inside applyLifecycleSubstitutions.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	got := ec.SubstituteVariables("w=$entity.lifecycle.workflow|def=$entity.lifecycle.workflow_def")
	// .workflow resolved to "mission"; .workflow_def left verbatim.
	if got != "w=mission|def=$entity.lifecycle.workflow_def" {
		t.Errorf("expected workflow substituted + workflow_def verbatim, got %q", got)
	}
}

func TestSubstitute_NilManagerLeavesTokens(t *testing.T) {
	t.Parallel()
	ec := &ExecutionContext{EntityID: semantictest.EntityID(t, "test", "rule", "lifecycle", "manager", "entity", "nil")}
	in := "phase=$entity.lifecycle.phase"
	got := ec.SubstituteVariables(in)
	if got != in {
		t.Errorf("nil manager: tokens should survive, got %q", got)
	}
}

func TestSubstitute_UnknownEntityLeavesTokens(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	// No seed for entityID — LookupByEntityID returns not-found.
	ec := &ExecutionContext{EntityID: semantictest.EntityID(t, "test", "rule", "lifecycle", "lookup", "entity", "missing"), Lifecycle: mgr}
	in := "phase=$entity.lifecycle.phase"
	got := ec.SubstituteVariables(in)
	if got != in {
		t.Errorf("not-found: tokens should survive, got %q", got)
	}
}

func TestSubstitute_NoLifecycleTokenShortCircuits(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "short-circuit", "entity", "001")
	// Templates without `$entity.lifecycle.` shouldn't trigger a
	// LookupByEntityID at all — verified via counter.
	mgr := newCountingManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	_ = ec.SubstituteVariables("plain text, no tokens")
	_ = ec.SubstituteVariables("$entity.id only")
	if mgr.lookups.Load() != 0 {
		t.Errorf("LookupByEntityID called %d times for templates with no lifecycle tokens", mgr.lookups.Load())
	}
}

func TestSubstitute_LookupMemoizedPerEC(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "memoized", "entity", "001")
	// Two substitution calls on the same EC → one lookup. Both
	// positive and negative caches matter; this test covers positive.
	mgr := newCountingManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying"})

	ec := &ExecutionContext{EntityID: entityID, Lifecycle: mgr}
	_ = ec.SubstituteVariables("phase=$entity.lifecycle.phase")
	_ = ec.SubstituteVariables("workflow=$entity.lifecycle.workflow")
	_ = ec.SubstituteVariables("terminal=$entity.lifecycle.terminal")
	if got := mgr.lookups.Load(); got != 1 {
		t.Errorf("expected 1 LookupByEntityID, got %d", got)
	}
}

func TestSubstitute_NegativeLookupMemoized(t *testing.T) {
	t.Parallel()
	mgr := newCountingManager() // no seed — every lookup fails
	ec := &ExecutionContext{EntityID: semantictest.EntityID(t, "test", "rule", "lifecycle", "memoized", "entity", "missing"), Lifecycle: mgr}
	_ = ec.SubstituteVariables("a=$entity.lifecycle.phase")
	_ = ec.SubstituteVariables("b=$entity.lifecycle.workflow")
	if got := mgr.lookups.Load(); got != 1 {
		t.Errorf("expected 1 (memoized negative) LookupByEntityID, got %d", got)
	}
}

// --- PopulateLifecycleStateFields ---

func TestPopulateLifecycleStateFields_PopulatesAllKeys(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "state-fields", "participant", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying", TerminalF: false})

	fields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), mgr, entityID, fields)

	if fields["$entity.lifecycle.phase"] != "flying" {
		t.Errorf("phase: got %v", fields["$entity.lifecycle.phase"])
	}
	if fields["$entity.lifecycle.workflow"] != "mission" {
		t.Errorf("workflow: got %v", fields["$entity.lifecycle.workflow"])
	}
	if fields["$entity.lifecycle.terminal"] != false {
		t.Errorf("terminal: got %v", fields["$entity.lifecycle.terminal"])
	}
}

func TestPopulateLifecycleStateFields_NilManagerNoOp(t *testing.T) {
	t.Parallel()
	fields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), nil, semantictest.EntityID(t, "test", "rule", "lifecycle", "state-fields", "participant", "002"), fields)
	if len(fields) != 0 {
		t.Errorf("nil manager should populate nothing, got %v", fields)
	}
}

func TestPopulateLifecycleStateFields_UnknownEntityNoOp(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	fields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), mgr, semantictest.EntityID(t, "test", "rule", "lifecycle", "state-fields", "participant", "missing"), fields)
	if len(fields) != 0 {
		t.Errorf("unknown entity should populate nothing, got %v", fields)
	}
}

// --- End-to-end: condition.Field resolution via the evaluator ---

func TestEvaluator_ResolvesLifecyclePhaseConditionField(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "condition", "entity", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying"})

	fields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), mgr, entityID, fields)

	evaluator := expression.NewExpressionEvaluator()
	expr := expression.LogicalExpression{
		Logic: expression.LogicAnd,
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
	}
	got, err := evaluator.EvaluateWithStateAndMessage(nil, fields, nil, expr)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !got {
		t.Errorf("expected condition phase==flying to match")
	}
}

// --- Production-wire integration tests (S1 reviewer rec) ---

// TestExpressionRule_EvaluateEntityState_ResolvesLifecyclePhase drives
// the SAME production entrypoint StatefulEvaluator uses for initial
// matching — NewExpressionRule(def) + SetLifecycleManager(mgr) +
// EvaluateEntityState. A regression that drops the
// PopulateLifecycleStateFields call inside EvaluateEntityState would
// fail this test ([[feedback_integration_tests_must_drive_production_wire]]).
func TestExpressionRule_EvaluateEntityState_ResolvesLifecyclePhase(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "wire", "entity", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying"})

	def := Definition{
		ID:      "wire-rule-1",
		Type:    "expression",
		Name:    "phase-match",
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
	}
	rule, err := NewExpressionRule(def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	rule.SetLifecycleManager(mgr)

	entity := &gtypes.EntityState{ID: entityID}
	if !rule.EvaluateEntityState(entity) {
		t.Errorf("expected EvaluateEntityState=true for phase==flying via production wire")
	}
}

func TestExpressionRule_EvaluateEntityState_LifecyclePhaseMismatch(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "wire", "entity", "002")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})

	def := Definition{
		ID:      "wire-rule-2",
		Type:    "expression",
		Name:    "phase-match",
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
	}
	rule, err := NewExpressionRule(def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	rule.SetLifecycleManager(mgr)

	entity := &gtypes.EntityState{ID: entityID}
	if rule.EvaluateEntityState(entity) {
		t.Errorf("expected EvaluateEntityState=false for phase==planning vs value=flying")
	}
}

// TestExpressionRule_NoManagerLeavesLifecycleTokensUnresolved verifies
// the loud-fail boundary: without SetLifecycleManager wired, condition
// fields like $entity.lifecycle.phase MUST NOT silently fail to a
// false-match. The evaluator currently returns false-no-error
// (Required: false default) so the rule simply doesn't fire — that's
// acceptable per the evaluator's existing missing-state-field contract.
// The test locks this behaviour so a future change accidentally
// substituting empty strings would break it.
// TestStatefulEvaluator_ResolvesLifecyclePhase drives the OTHER
// production entrypoint: NewStatefulEvaluator + SetLifecycleManager +
// Evaluate. Asserts that condition.Field=$entity.lifecycle.phase
// resolves at the stateful-rule fire path (the wire that's used after
// initial-match passes to the stateful tracker). Any future change
// that drops PopulateLifecycleStateFields inside
// StatefulEvaluator.Evaluate must fail this test.
func TestStatefulEvaluator_ResolvesLifecyclePhase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bucket := newMockKVBucket()
	stateTracker := NewStateTracker(bucket, nil)

	// Use a mockActionExecutor that records OnEnter firings so we can
	// assert the lifecycle.phase condition resolved positively (a
	// false-resolve would skip OnEnter via the TransitionNone path).
	actionExec := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(stateTracker, actionExec, nil)

	mgr := newFakeManager()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "stateful", "entity", "001")
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying"})
	evaluator.SetLifecycleManager(mgr)

	def := Definition{
		ID:      "stateful-wire-rule",
		Type:    "expression",
		Name:    "phase match",
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
		OnEnter: []Action{
			// Use the subject string the existing mockActionExecutor
			// counts as an OnEnter call (test.entered → onEnterCalls++).
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              def,
		EntityID:          entityID,
		CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if transition != TransitionEntered {
		t.Errorf("expected TransitionEntered, got %v", transition)
	}
	if actionExec.onEnterCalls != 1 {
		t.Errorf("OnEnter should have fired once via production wire, got %d (executeCallCount=%d)",
			actionExec.onEnterCalls, actionExec.executeCallCount)
	}
}

func TestExpressionRule_NoManagerLeavesLifecyclePhaseUnresolved(t *testing.T) {
	t.Parallel()

	def := Definition{
		ID:      "wire-rule-3",
		Type:    "expression",
		Name:    "phase-match",
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
	}
	rule, err := NewExpressionRule(def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	// Deliberately omit SetLifecycleManager.

	entity := &gtypes.EntityState{ID: semantictest.EntityID(t, "test", "rule", "lifecycle", "wire", "entity", "003")}
	if rule.EvaluateEntityState(entity) {
		t.Errorf("expected false-match with no manager wired (field missing → no fire)")
	}
}

func TestEvaluator_LifecyclePhaseMismatchYieldsFalse(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "lifecycle", "condition", "entity", "002")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "planning"})

	fields := expression.StateFields{}
	PopulateLifecycleStateFields(context.Background(), mgr, entityID, fields)

	evaluator := expression.NewExpressionEvaluator()
	expr := expression.LogicalExpression{
		Logic: expression.LogicAnd,
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: expression.OpEqual, Value: "flying"},
		},
	}
	got, err := evaluator.EvaluateWithStateAndMessage(nil, fields, nil, expr)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got {
		t.Errorf("expected condition to fail when phase != value")
	}
}
