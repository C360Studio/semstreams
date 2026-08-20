package rule

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
			wantStateful := stateful.EvaluateEntityState(context.Background(), entity)

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
	if stateful.EvaluateEntityState(context.Background(), createTestEntityState("test.entity.id", triples)) {
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
	if stateful.EvaluateEntityState(context.Background(), createTestEntityState("test.entity.id", triples)) {
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
	if stateful.EvaluateEntityState(context.Background(), createTestEntityState("test.entity.id", triples)) {
		t.Error("precondition: the engine must also refuse a disabled rule")
	}
}

// failingLookup fails every lookup, standing in for a transient KV/graph failure.

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

// TestMatchesWithLifecycle_NilManagerIsRefused — F4, in its final form.
//
// With the CONCRETE *lifecycle.Manager parameter the typed-nil hazard is
// unrepresentable rather than guarded: `manager == nil` on a pointer is always
// correct, so there is no non-nil-interface-holding-a-nil-pointer to admit and no
// panic inside LookupByEntityID to avoid. That is what the concrete type buys, and
// it is why isNilLookup and the reflect call are gone.
//
// A nil manager is still refused rather than downgraded — it means the caller
// wanted Matches.
func TestMatchesWithLifecycle_NilManagerIsRefused(t *testing.T) {
	t.Parallel()
	var nilManager *lifecycle.Manager
	def := Definition{
		ID: "nil-manager", Type: "expression", Name: "NilManager", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on a nil manager: %v", r)
		}
	}()

	got, err := MatchesWithLifecycle(context.Background(), def,
		createTestEntityState("test.entity.id", nil), nilManager)
	if err == nil {
		t.Fatal("a nil manager must be refused, not treated as a degraded mode")
	}
	if !strings.Contains(err.Error(), "call Matches instead") {
		t.Errorf("the refusal should name the entry point that fits the caller's "+
			"situation; got %v", err)
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
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
		t.Fatal("Matches has no manager, so a lifecycle condition is unanswerable and must error")
	}
	if got {
		t.Errorf("verdict must be false alongside the error, got %v", got)
	}
}

// --- Lookup-failure and context coverage, at the shared implementation seam ---
//
// MatchesWithLifecycle takes the CONCRETE *lifecycle.Manager (owner ruling), which
// cannot be faked — building one needs a NATS client. These tests therefore drive
// matchesWithLookup, the shared implementation BOTH exported entry points delegate
// to, with a fake at the interface it actually consumes.
//
// That is the real code path, not a reconstruction of it: the exported wrappers add
// only a nil check and a caller label. What is NOT covered at unit level any more is
// the wrapper wiring itself — see matches_lifecycle_integration_test.go, which
// exercises MatchesWithLifecycle against a real Manager.

// failingLookup fails every lookup: a transient KV/graph failure. Embeds the fake so
// the other LifecycleManager methods come for free.
type failingLookup struct {
	*fakeLifecycleManager
	err error
}

func (f failingLookup) LookupByEntityID(_ context.Context, _ string) (lifecycle.Participant, error) {
	return nil, f.err
}

// blockingLookup blocks until its context is done.
type blockingLookup struct{ *fakeLifecycleManager }

func (blockingLookup) LookupByEntityID(ctx context.Context, _ string) (lifecycle.Participant, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// secondLookupBlocks lets the answerability pass resolve lifecycle state, then
// blocks the separate condition-value substitution lookup until its exact caller
// context is canceled.
type secondLookupBlocks struct {
	*fakeLifecycleManager
	calls         atomic.Int64
	secondStarted chan struct{}
}

func (m *secondLookupBlocks) LookupByEntityID(ctx context.Context, entityID string) (lifecycle.Participant, error) {
	if m.calls.Add(1) == 1 {
		return m.fakeLifecycleManager.LookupByEntityID(ctx, entityID)
	}
	close(m.secondStarted)
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestMatches_SuppliedManagerThatFailsIsNotResolvedState — Codex finding 2.
// "A manager was supplied" and "lifecycle state resolved" are different facts.
func TestMatches_SuppliedManagerThatFailsIsNotResolvedState(t *testing.T) {
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
		lookup    LifecycleManager
		wantCause error
	}{
		{name: "unregistered participant", lookup: newFakeManager()},
		{
			name:      "transient lookup failure",
			lookup:    failingLookup{fakeLifecycleManager: newFakeManager(), err: errTransient},
			wantCause: errTransient,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := matchesWithLookup(context.Background(), "MatchesWithLifecycle", def,
				createTestEntityState("test.entity.id", nil), tc.lookup)
			if err == nil {
				t.Fatal("expected an error: the manager was supplied but did not resolve, so the " +
					"lifecycle condition was never actually answered")
			}
			if got {
				t.Errorf("verdict must be false alongside the error, got %v", got)
			}
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Errorf("error must wrap the underlying lookup failure so the caller can see "+
					"WHY it was unanswerable; got %v", err)
			}
		})
	}
}

// TestMatches_SuppliedManagerDoesNotBreakOrdinaryDefinitions is the regression guard
// for the bug my FIRST fix to finding 2 introduced: raising the lookup error
// unconditionally refused every ordinary definition whenever a manager was supplied,
// because LookupByEntityID errors for any entity that is not lifecycle-managed.
func TestMatches_SuppliedManagerDoesNotBreakOrdinaryDefinitions(t *testing.T) {
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

	got, err := matchesWithLookup(context.Background(), "MatchesWithLifecycle", def, entity, newFakeManager())
	if err != nil {
		t.Fatalf("a definition with no lifecycle conditions must not care that an unrelated "+
			"lifecycle lookup failed: %v", err)
	}
	if !got {
		t.Error("expected true — conditions hold and lifecycle is irrelevant here")
	}
}

// TestMatches_HonoursContextCancellation — Codex finding 5. Lifecycle resolution
// performs KV/graph I/O; without a caller context a degraded backend wedges a
// boot-time recovery pass indefinitely.
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
		_, err := matchesWithLookup(ctx, "MatchesWithLifecycle", def,
			createTestEntityState("test.entity.id", nil), blockingLookup{newFakeManager()})
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

func TestMatches_RejectsNilContext(t *testing.T) {
	t.Parallel()
	state := createTestEntityState("test.entity.id", nil)
	if _, err := Matches(nil, Definition{ID: "nil-context", Enabled: true}, state); err == nil {
		t.Fatal("Matches accepted a nil context")
	}
}

func TestMatches_ConditionSubstitutionSecondLookupHonoursCallerCancellation(t *testing.T) {
	entityID := "test.entity.id"
	base := newFakeManager()
	base.seed("mission", &fakeParticipant{EntityIDF: entityID, PhaseF: "flying"})
	lookup := &secondLookupBlocks{
		fakeLifecycleManager: base,
		secondStarted:        make(chan struct{}),
	}
	def := Definition{
		ID: "ctx-second-lookup", Type: "expression", Name: "CtxSecondLookup", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "$entity.lifecycle.phase"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := matchesWithLookup(ctx, "MatchesWithLifecycle", def,
			createTestEntityState(entityID, nil), lookup)
		done <- err
	}()

	select {
	case <-lookup.secondStarted:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("condition substitution did not reach its lifecycle lookup")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled second lookup returned a confident verdict")
		}
		if got := lookup.calls.Load(); got != 2 {
			t.Fatalf("lifecycle lookup count = %d, want 2", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("condition substitution did not observe caller cancellation")
	}
}

// TestMatchesPair_AgreeWhereLifecycleIsIrrelevant proves the pair is two doors to one
// implementation. Driven at the shared seam for the same reason as above.
func TestMatchesPair_AgreeWhereLifecycleIsIrrelevant(t *testing.T) {
	t.Parallel()
	for _, c := range matchCorpus() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			plain, errPlain := Matches(context.Background(), defFor(c),
				createTestEntityState("test.entity.id", c.triples))
			withLC, errLC := matchesWithLookup(context.Background(), "MatchesWithLifecycle", defFor(c),
				createTestEntityState("test.entity.id", c.triples), newFakeManager())
			if errPlain != nil || errLC != nil {
				t.Fatalf("unexpected errors: plain=%v withLifecycle=%v", errPlain, errLC)
			}
			if plain != withLC {
				t.Errorf("the pair diverged on a definition with no lifecycle conditions: "+
					"Matches=%v withLifecycle=%v", plain, withLC)
			}
		})
	}
}
