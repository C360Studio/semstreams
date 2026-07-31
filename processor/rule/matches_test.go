package rule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/lifecycle"
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
			got, err := Matches(context.Background(), defFor(c), createTestEntityState("test.entity.id", c.triples))
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

			got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", c.triples))
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

	got, err := Matches(context.Background(), def, entity)
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
			got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", nil))
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
	got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", triples))
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

	got, err := MatchesWithLifecycle(context.Background(), def, createTestEntityState(entityID, nil), mgr)
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

	got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", nil))
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
	got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", triples))
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

	if _, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", triples)); err != nil {
		t.Fatalf("Matches returned error: %v", err)
	}

	if stateful.shouldTrigger != beforeTrigger {
		t.Errorf("shouldTrigger changed: %v -> %v", beforeTrigger, stateful.shouldTrigger)
	}
	if !stateful.lastTriggered.Equal(beforeLast) {
		t.Errorf("lastTriggered changed: %v -> %v", beforeLast, stateful.lastTriggered)
	}
}

// --- Reviewer findings F2/F3/F4/F5 ---

// TestMatches_DisabledDefinitionOwesNothing — F3. Production refuses a disabled
// rule first (expression_factory.go). Cooldown is the ONLY divergence Matches
// claims, so every other firing gate must be mirrored or that claim is false.
func TestMatches_DisabledDefinitionOwesNothing(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "disabled", Type: "expression", Name: "Disabled", Enabled: false, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0, Required: true},
		},
	}
	triples := []message.Triple{{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2}}

	got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", triples))
	if err != nil {
		t.Fatalf("a disabled definition is answerable, not an error: %v", err)
	}
	if got {
		t.Error("a disabled rule cannot fire, so it owes nothing; reporting true makes a " +
			"recovery pass defer or duplicate work for a rule that will never run")
	}

	// Parity: production agrees.
	stateful, err := NewExpressionRule("disabled-parity-test", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	if stateful.EvaluateEntityState(createTestEntityState("test.entity.id", triples)) {
		t.Error("precondition: the engine must also refuse a disabled rule")
	}
}

// failingLookup fails every lookup, standing in for a transient KV/graph failure.
type failingLookup struct{ err error }

func (f failingLookup) LookupByEntityID(_ context.Context, _ string) (lifecycle.Participant, error) {
	return nil, f.err
}
func (f failingLookup) GetWorkflowDefinition(_ string) (lifecycle.WorkflowDef, error) {
	return lifecycle.WorkflowDef{}, f.err
}

// blockingLookup blocks until its context is done — for the cancellation test.
type blockingLookup struct{}

func (blockingLookup) LookupByEntityID(ctx context.Context, _ string) (lifecycle.Participant, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingLookup) GetWorkflowDefinition(_ string) (lifecycle.WorkflowDef, error) {
	return lifecycle.WorkflowDef{}, nil
}

// TestMatches_SuppliedLookupThatFailsIsNotResolvedState — F2. "A lookup was
// supplied" and "lifecycle state resolved" are different facts. Conflating them
// let an optional lifecycle condition reach the evaluator with no state and come
// back false — "could not resolve" wearing "nothing owed" as a disguise.
func TestMatches_SuppliedLookupThatFailsIsNotResolvedState(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "lifecycle-optional", Type: "expression", Name: "LifecycleOptional", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			// Deliberately NOT Required — this is the case that silently returned false.
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	errTransient := errors.New("kv unavailable")

	cases := []struct {
		name      string
		lookup    LifecycleLookup
		wantCause error // nil = any error; non-nil = must wrap this exact cause
	}{
		{
			name:   "unregistered participant",
			lookup: newFakeManager(), // seeded with nothing → LookupByEntityID errors
		},
		{
			name:      "transient lookup failure",
			lookup:    failingLookup{err: errTransient},
			wantCause: errTransient,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := MatchesWithLifecycle(context.Background(), def,
				createTestEntityState("test.entity.id", nil), tc.lookup)
			if err == nil {
				t.Fatal("expected an error: the lookup was supplied but did not resolve, so the " +
					"lifecycle condition was never actually answered")
			}
			if got {
				t.Errorf("verdict must be false alongside the error, got %v", got)
			}
			// Assert the REAL CAUSE surfaces. Without this the test passes on the
			// generic pre-scan message, which tells a caller who DID supply a lookup
			// to "pass one" — and the propagation could be deleted unnoticed.
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Errorf("error must wrap the underlying lookup failure so the caller can see "+
					"WHY it was unanswerable; got %v", err)
			}
		})
	}
}

// TestMatches_SuppliedLookupDoesNotBreakOrdinaryDefinitions is the other side of
// F2, and it exists because fixing F2 first introduced exactly this regression:
// LookupByEntityID errors for any entity that is not lifecycle-managed, which is
// the ordinary case, so raising the lookup error unconditionally refused every
// ordinary definition whenever a caller supplied a Manager.
func TestMatches_SuppliedLookupDoesNotBreakOrdinaryDefinitions(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "ordinary", Type: "expression", Name: "Ordinary", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0},
		},
	}
	entity := createTestEntityState("test.entity.id", []message.Triple{
		{Subject: "test", Predicate: "sensor.measurement.fahrenheit", Object: 41.2},
	})

	got, err := MatchesWithLifecycle(context.Background(), def, entity, newFakeManager())
	if err != nil {
		t.Fatalf("a definition with no lifecycle conditions must not care that an unrelated "+
			"lifecycle lookup failed: %v", err)
	}
	if !got {
		t.Error("expected true — conditions hold and lifecycle is irrelevant here")
	}
}

// TestMatches_UnresolvedValueTemplateErrors — F2b. A leftover token does not make
// eq/contains error; they compare it as an ordinary string and return a confident
// verdict.
func TestMatches_UnresolvedValueTemplateErrors(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "unresolved-value", Type: "expression", Name: "UnresolvedValue", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			// Field is ordinary, so the pre-scan passes; the VALUE is a template
			// that cannot resolve without match state.
			{Field: "sensor.classification.type", Operator: "eq", Value: "$state.iteration"},
		},
	}
	entity := createTestEntityState("test.entity.id", []message.Triple{
		{Subject: "test", Predicate: "sensor.classification.type", Object: "temperature"},
	})

	got, err := Matches(context.Background(), def, entity)
	if err == nil {
		t.Fatal("expected an error: an unresolved value template would be compared as the " +
			"literal string \"$state.iteration\" and yield a confident wrong verdict")
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
	}
}

// TestMatchesWithLifecycle_TypedNilIsRefusedNotPanicked — F4. A typed nil is a
// non-nil interface, so a plain `!= nil` guard admits it and then panics inside
// LookupByEntityID (which dereferences the receiver immediately).
//
// With the split, an absent lookup is not a degraded mode of this function — it is
// a call to the WRONG function — so it is refused loudly and the caller is pointed
// at Matches.
func TestMatchesWithLifecycle_TypedNilIsRefusedNotPanicked(t *testing.T) {
	t.Parallel()
	var typedNil *lifecycle.Manager
	def := Definition{
		ID: "typed-nil", Type: "expression", Name: "TypedNil", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on a typed-nil lookup: %v", r)
		}
	}()

	got, err := MatchesWithLifecycle(context.Background(), def, createTestEntityState("test.entity.id", nil), typedNil)
	if err == nil {
		t.Fatal("a typed-nil lookup is an ABSENT lookup; MatchesWithLifecycle must refuse " +
			"it rather than return a verdict or panic")
	}
	if !strings.Contains(err.Error(), "call Matches instead") {
		t.Errorf("the refusal should point the caller at the function that fits their "+
			"situation; got %v", err)
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
	}
}

// TestMatches_HonoursContextCancellation — F5. Matches performs KV/graph I/O
// through the lifecycle lookup; without a caller context a degraded backend wedges
// a boot-time recovery pass indefinitely.
func TestMatches_HonoursContextCancellation(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "ctx", Type: "expression", Name: "Ctx", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := MatchesWithLifecycle(ctx, def, createTestEntityState("test.entity.id", nil), blockingLookup{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error once the context expired")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Matches did not observe context cancellation — a degraded backend would " +
			"wedge the caller indefinitely")
	}
}

// TestMatches_RefusesLifecycleConditionsByName is the point of the split: the
// no-lookup entry point cannot answer lifecycle conditions, and a caller learns
// that from the function they called rather than from a nil argument.
func TestMatches_RefusesLifecycleConditionsByName(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID: "lifecycle-nolookup", Type: "expression", Name: "NoLookup", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	got, err := Matches(context.Background(), def, createTestEntityState("test.entity.id", nil))
	if err == nil {
		t.Fatal("Matches has no lookup, so a lifecycle condition is unanswerable and must error")
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
	}
}

// TestMatchesPair_AgreeWhereLifecycleIsIrrelevant proves the split is two doors to
// one implementation, not two implementations.
func TestMatchesPair_AgreeWhereLifecycleIsIrrelevant(t *testing.T) {
	t.Parallel()
	for _, c := range matchCorpus() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			plain, errPlain := Matches(context.Background(), defFor(c),
				createTestEntityState("test.entity.id", c.triples))
			withLC, errLC := MatchesWithLifecycle(context.Background(), defFor(c),
				createTestEntityState("test.entity.id", c.triples), newFakeManager())
			if errPlain != nil || errLC != nil {
				t.Fatalf("unexpected errors: plain=%v withLifecycle=%v", errPlain, errLC)
			}
			if plain != withLC {
				t.Errorf("the pair diverged on a definition with no lifecycle conditions: "+
					"Matches=%v MatchesWithLifecycle=%v", plain, withLC)
			}
		})
	}
}
