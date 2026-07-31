package rule

import (
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// matchCase is one definition + entity + the verdict a human asserts.
//
// The `want` values here are written by hand, NOT computed. A test that derives
// its expectation from the code under test verifies the derivation; see the
// program's standing note on assertions that reconstruct what they mean to check.
type matchCase struct {
	name       string
	conditions []expression.ConditionExpression
	logic      string
	triples    []message.Triple
	want       bool
}

func matchCorpus() []matchCase {
	return []matchCase{
		{
			name: "and: both conditions hold",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
				{Field: "geo.location.zone", Operator: "contains", Value: "cold-storage", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2},
				{Subject: "test", Predicate: "geo.location.zone", Object: "zone.cold-storage-1"},
			},
			want: true,
		},
		{
			name: "and: one condition fails",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
				{Field: "geo.location.zone", Operator: "contains", Value: "cold-storage", Required: true},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 38.5},
				{Subject: "test", Predicate: "geo.location.zone", Object: "zone.cold-storage-1"},
			},
			want: false,
		},
		{
			name: "or: second condition carries it",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.psi", Operator: "lt", Value: 100.0},
				{Field: "sensor.classification.type", Operator: "eq", Value: "pressure"},
			},
			logic: "or",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.measurement.psi", Object: 140.0},
				{Subject: "test", Predicate: "sensor.classification.type", Object: "pressure"},
			},
			want: true,
		},
		{
			name: "optional field absent does not match",
			conditions: []expression.ConditionExpression{
				{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0},
			},
			logic: "and",
			triples: []message.Triple{
				{Subject: "test", Predicate: "sensor.classification.type", Object: "temperature"},
			},
			want: false,
		},
	}
}

func defFor(c matchCase) Definition {
	return Definition{
		ID:         "matches-test-rule",
		Type:       "expression",
		Name:       "Matches Test",
		Enabled:    true,
		Logic:      c.logic,
		Conditions: c.conditions,
	}
}

// TestMatches_AgreesWithHandWrittenVerdicts is the real oracle: expectations are
// authored, not derived.
func TestMatches_AgreesWithHandWrittenVerdicts(t *testing.T) {
	t.Parallel()
	for _, c := range matchCorpus() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := Matches(defFor(c), createTestEntityState("test.entity.id", c.triples), nil)
			if err != nil {
				t.Fatalf("Matches returned error: %v", err)
			}
			if got != c.want {
				t.Errorf("Matches() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestMatches_AgreesWithStatefulPath guards the drift gh#731 exists to remove.
// This is a DIVERGENCE check, not a correctness oracle — both paths funnel into
// evaluateConditionsAgainstEntity, so agreement proves they share the pipeline,
// which is exactly the property under test. Correctness is asserted above.
func TestMatches_AgreesWithStatefulPath(t *testing.T) {
	t.Parallel()
	for _, c := range matchCorpus() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			def := defFor(c)
			entity := createTestEntityState("test.entity.id", c.triples)

			stateful, err := NewExpressionRule("matches-parity-test", def)
			if err != nil {
				t.Fatalf("NewExpressionRule: %v", err)
			}
			wantStateful := stateful.EvaluateEntityState(entity)

			got, err := Matches(def, createTestEntityState("test.entity.id", c.triples), nil)
			if err != nil {
				t.Fatalf("Matches returned error: %v", err)
			}
			if got != wantStateful {
				t.Errorf("stateless Matches()=%v diverged from EvaluateEntityState()=%v", got, wantStateful)
			}
		})
	}
}

// TestMatches_TemplatedConditionValueResolves covers the substitution step a
// bare-evaluator caller loses (#149). Without it the operator receives the literal
// "$entity.triple.gather.child.completed.length" and coerce-errors.
func TestMatches_TemplatedConditionValueResolves(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "templated", Type: "expression", Name: "Templated", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "gather.child.completed", Operator: "length_eq", Value: "$entity.triple.decision.subtopics.length"},
		},
	}
	entity := createTestEntityState("test.entity.id", []message.Triple{
		// One list-shaped triple: .length resolves to 2.
		{Subject: "test", Predicate: "decision.subtopics", Object: []string{"a", "b"}},
		// Two counter triples: the array operator counts 2.
		{Subject: "test", Predicate: "gather.child.completed", Object: "a"},
		{Subject: "test", Predicate: "gather.child.completed", Object: "b"},
	})

	got, err := Matches(def, entity, nil)
	if err != nil {
		t.Fatalf("Matches returned error: %v", err)
	}
	if !got {
		t.Error("Matches()=false; expected true — 2 subtopics vs 2 completed. " +
			"A false here means the $-template reached the operator unsubstituted.")
	}
}

// TestMatches_UnresolvableFieldErrors is the core guard. Each case would return
// (false, nil) — a confident wrong answer — if the pre-scan were removed, so the
// test goes red the moment the guard does.
func TestMatches_UnresolvableFieldErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		condition expression.ConditionExpression
	}{
		{
			name:      "$state.* is not answerable without a running engine",
			condition: expression.ConditionExpression{Field: "$state.iteration", Operator: "gte", Value: 1},
		},
		{
			name:      "$prev.* is not answerable from a single observation",
			condition: expression.ConditionExpression{Field: "$prev.status", Operator: "eq", Value: "pending"},
		},
		{
			name:      "transition has no stateless form",
			condition: expression.ConditionExpression{Field: "status", Operator: expression.OpTransition, Value: "done", From: "pending"},
		},
		{
			name:      "lifecycle field without a lookup",
			condition: expression.ConditionExpression{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID: "unresolvable", Type: "expression", Name: "Unresolvable", Enabled: true, Logic: "and",
				Conditions: []expression.ConditionExpression{tc.condition},
			}
			got, err := Matches(def, createTestEntityState("test.entity.id", nil), nil)
			if err == nil {
				t.Fatal("expected an error; got nil — an unanswerable condition must never " +
					"return a verdict, because a caller reads false as 'nothing owed' and acts on it")
			}
			if got {
				t.Errorf("verdict must be false alongside the error, got %v", got)
			}
			var evalErr *expression.EvaluationError
			if !errors.As(err, &evalErr) {
				t.Errorf("error should carry the field identity as *expression.EvaluationError, got %T", err)
			}
		})
	}
}

// TestMatches_EvaluationErrorPropagatesRatherThanBecomingFalse pins the deliberate
// divergence from EvaluateEntityState documented on Matches.
//
// The engine swallows evaluation errors and returns false, because a rule that
// cannot be evaluated must not fire. Matches propagates, because "could not
// evaluate" is not "nothing owed" — and a consumer that reads the first as the
// second strands the entity. The two never disagree on a VERDICT; they disagree on
// whether an unanswerable question gets one.
func TestMatches_EvaluationErrorPropagatesRatherThanBecomingFalse(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "required-absent", Type: "expression", Name: "RequiredAbsent", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
		},
	}
	triples := []message.Triple{{Subject: "test", Predicate: "sensor.classification.type", Object: "temperature"}}

	// The engine returns a bare false for this input.
	stateful, err := NewExpressionRule("required-absent-test", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	if stateful.EvaluateEntityState(createTestEntityState("test.entity.id", triples)) {
		t.Fatal("precondition failed: the engine should not fire on an absent required field")
	}

	// Matches refuses instead, so the caller can tell "cannot tell" from "not owed".
	got, err := Matches(def, createTestEntityState("test.entity.id", triples), nil)
	if err == nil {
		t.Fatal("expected an error: an absent REQUIRED field means the question could not be " +
			"answered, and collapsing that into 'nothing owed' is what strands an entity")
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
	}
}

// TestMatches_StatefulPathKeepsItsTolerance is the other side of the guard: the
// stateful evaluator must NOT start erroring on an optional absent state field.
// D2 makes "the stateful path does not change" normative, and a pre-scan that
// leaked into the evaluator would break this.
func TestMatches_StatefulPathKeepsItsTolerance(t *testing.T) {
	t.Parallel()
	ev := expression.NewExpressionEvaluator()
	got, err := ev.EvaluateWithStateAndMessage(
		createTestEntityState("test.entity.id", nil),
		expression.StateFields{},
		nil,
		expression.LogicalExpression{
			Conditions: []expression.ConditionExpression{
				{Field: "$state.iteration", Operator: "gte", Value: 1},
			},
			Logic: "and",
		},
	)
	if err != nil {
		t.Fatalf("stateful path must stay tolerant of an optional absent state field, got error: %v", err)
	}
	if got {
		t.Error("expected false for an absent optional state field")
	}
}

// TestMatches_LifecycleResolvesWhenLookupSupplied — the Manager governs
// answerability: supplied, the same condition that errors above now resolves.
func TestMatches_LifecycleResolvesWhenLookupSupplied(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "matches", "lifecycle", "participant", "001")
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying", TerminalF: false})

	def := Definition{
		ID: "lifecycle", Type: "expression", Name: "Lifecycle", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	got, err := Matches(def, createTestEntityState(entityID, nil), mgr)
	if err != nil {
		t.Fatalf("Matches returned error with a lookup supplied: %v", err)
	}
	if !got {
		t.Error("expected true — phase is 'flying' and the lookup was supplied")
	}
}

// TestMatches_EmptyConditionListDoesNotMatch — D5. The bare evaluator returns
// TRUE for an empty list; the rule engine returns false, and the engine is the
// production answer.
func TestMatches_EmptyConditionListDoesNotMatch(t *testing.T) {
	t.Parallel()
	def := Definition{ID: "empty", Type: "expression", Name: "Empty", Enabled: true, Logic: "and"}

	got, err := Matches(def, createTestEntityState("test.entity.id", nil), nil)
	if err != nil {
		t.Fatalf("Matches returned error: %v", err)
	}
	if got {
		t.Error("Matches()=true for an empty condition list; must follow the rule engine (false), " +
			"not the bare evaluator (true)")
	}
}

// TestMatches_CooldownIsNotAppliedAndNotRefused — D4. Matches answers OBLIGATION:
// a rule inside its cooldown still owes the entity the hop.
func TestMatches_CooldownIsNotAppliedAndNotRefused(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "cooling", Type: "expression", Name: "Cooling", Enabled: true, Logic: "and",
		Cooldown: "1h",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
		},
	}
	triples := []message.Triple{{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2}}

	// The running engine, mid-cooldown, answers the INSTANT question: no.
	stateful, err := NewExpressionRule("cooldown-test", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	stateful.lastTriggered = time.Now()
	if stateful.EvaluateEntityState(createTestEntityState("test.entity.id", triples)) {
		t.Fatal("precondition failed: the engine should be cooling down and answer false")
	}

	// Matches answers the OBLIGATION question: yes, the hop is still owed.
	got, err := Matches(def, createTestEntityState("test.entity.id", triples), nil)
	if err != nil {
		t.Fatalf("a cooldown-declaring definition must not be refused, got error: %v", err)
	}
	if !got {
		t.Error("Matches()=false for a cooling-down rule whose conditions hold; the pack still " +
			"owes this entity the hop, and reporting otherwise strands the entity")
	}
}

// TestMatches_LeavesEngineStateUntouched — calling the stateless path must not be
// observable in a running engine.
func TestMatches_LeavesEngineStateUntouched(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "untouched", Type: "expression", Name: "Untouched", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
		},
	}
	triples := []message.Triple{{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2}}

	stateful, err := NewExpressionRule("untouched-test", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	beforeTrigger := stateful.shouldTrigger
	beforeLast := stateful.lastTriggered

	if _, err := Matches(def, createTestEntityState("test.entity.id", triples), nil); err != nil {
		t.Fatalf("Matches returned error: %v", err)
	}

	if stateful.shouldTrigger != beforeTrigger {
		t.Errorf("shouldTrigger changed: %v -> %v", beforeTrigger, stateful.shouldTrigger)
	}
	if !stateful.lastTriggered.Equal(beforeLast) {
		t.Errorf("lastTriggered changed: %v -> %v", beforeLast, stateful.lastTriggered)
	}
}
