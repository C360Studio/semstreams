// Package rule — gh#207 typed Object substitution tests.
//
// The pre-fix test suite (TestAction_AddTriple, TestAction_UpdateTriple)
// used string-only Object values, which made the type-erasure bug
// invisible by construction: comparing two strings via assert.Equal is
// type-vacuous. These tests enumerate every primitive Object type the
// `message.Triple` doc lists as supported (float64, int, bool, string)
// and assert both VALUE and reflect.TypeOf equality at the destination.
//
// Test discipline lifted from gh#207's forensic finding: when a field's
// destination type is wider than its declared input type
// (Action.Object string → Triple.Object any here), tests must
// enumerate the destination's documented types and assert
// type-preservation, not just value-preservation. See
// feedback_polymorphic_config_needs_json_roundtrip_test for the
// sister discipline this extends.
//
// reflect.TypeOf usage here is test-only — the runtime substitution
// path uses no reflection, per the hot-path constraint.
package rule

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// typedObjectCase parameterizes the round-trip test across every
// primitive Object type message.Triple's doc lists.
type typedObjectCase struct {
	name      string
	sourceObj any // value stored on the source triple's Object
}

func typedObjectCases() []typedObjectCase {
	return []typedObjectCase{
		{"float64 — the bug reproducer", float64(0.8327193260192871)},
		{"float64 — power-of-10 boundary value", float64(9.9)},
		{"int — iteration-counter shape", int(42)},
		{"bool — true", true},
		{"bool — false", false},
		{"string — entity-ID shape", "c360.platform.robotics.mav1.drone.001"},
		{"string — short literal", "active"},
	}
}

// TestExecuteAddTriple_SubstitutedObject_PreservesType is the gh#207
// regression test on the add_triple side. Source triple stamped with
// a typed Object; rule action references it via single-token
// substitution; destination triple must carry the same Go type.
func TestExecuteAddTriple_SubstitutedObject_PreservesType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entityID := "c360.platform.robotics.mav1.drone.001"

	for _, tc := range typedObjectCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockTripleMutator{}
			executor := NewActionExecutorWithMutator(nil, mock)

			// Entity carries a single source triple with the typed Object.
			entity := &gtypes.EntityState{
				ID: entityID,
				Triples: []message.Triple{
					{Subject: entityID, Predicate: "source.value", Object: tc.sourceObj},
				},
			}

			action := Action{
				Type:      ActionTypeAddTriple,
				Predicate: "dest.value",
				Object:    "$entity.triple.source.value",
			}

			_, err := executor.ExecuteAddTriple(ctx, action, &ExecutionContext{
				EntityID: entityID,
				Entity:   entity,
			})
			require.NoError(t, err)

			require.Len(t, mock.addedTriples, 1)
			got := mock.addedTriples[0].Object

			// Value equality alone passes the bug class — both sides as
			// strings ("0.8327..." vs "0.8327...") would assert.Equal
			// just fine. The type assertion is the load-bearing check.
			assert.Equal(t, tc.sourceObj, got, "Object value should round-trip")
			assert.Equal(t, reflect.TypeOf(tc.sourceObj), reflect.TypeOf(got),
				"Object Go type should round-trip — gh#207 regression")
		})
	}
}

// TestExecuteUpdateTriple_SubstitutedObject_PreservesType — the
// gh#207 regression test on the update_triple side. Symmetric with
// the add_triple variant because both actions share the typed-then-
// string substitution dispatch pattern. The autoresearch
// `best.value` upsert from semteams was the original reproducer.
func TestExecuteUpdateTriple_SubstitutedObject_PreservesType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entityID := "c360.platform.robotics.mav1.drone.001"

	for _, tc := range typedObjectCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockTripleMutator{}
			executor := NewActionExecutorWithMutator(nil, mock)

			entity := &gtypes.EntityState{
				ID: entityID,
				Triples: []message.Triple{
					{Subject: entityID, Predicate: "source.value", Object: tc.sourceObj},
				},
			}

			action := Action{
				Type:      ActionTypeUpdateTriple,
				Predicate: "dest.value",
				Object:    "$entity.triple.source.value",
			}

			err := executor.Execute(ctx, action, &ExecutionContext{
				EntityID: entityID,
				Entity:   entity,
			})
			require.NoError(t, err)

			require.Len(t, mock.addedTriples, 1)
			got := mock.addedTriples[0].Object
			assert.Equal(t, tc.sourceObj, got, "Object value should round-trip")
			assert.Equal(t, reflect.TypeOf(tc.sourceObj), reflect.TypeOf(got),
				"Object Go type should round-trip — gh#207 regression")
		})
	}
}

// TestExecuteAddTriple_MixedTemplate_FallsBackToString locks in the
// string-fallback contract: when action.Object is a mixed template
// (concat, prefix, suffix), the destination Object must be a string
// — that's the existing behaviour and the only sensible one for
// string-concat semantics. Without this test the typed path could
// silently swallow mixed templates and break string-concat patterns.
func TestExecuteAddTriple_MixedTemplate_FallsBackToString(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entityID := "c360.platform.robotics.mav1.drone.001"

	mock := &mockTripleMutator{}
	executor := NewActionExecutorWithMutator(nil, mock)
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "source.value", Object: float64(0.5)},
		},
	}

	action := Action{
		Type:      ActionTypeAddTriple,
		Predicate: "dest.value",
		Object:    "value-is-$entity.triple.source.value-end",
	}

	_, err := executor.ExecuteAddTriple(ctx, action, &ExecutionContext{
		EntityID: entityID,
		Entity:   entity,
	})
	require.NoError(t, err)

	require.Len(t, mock.addedTriples, 1)
	got := mock.addedTriples[0].Object
	assert.IsType(t, "", got, "mixed-template Object should be string")
	assert.Equal(t, "value-is-0.5-end", got, "string concatenation should preserve template shape")
}

// TestExecuteAddTriple_LiteralObject_PreservesString locks in the
// behaviour for literal-only Object values: a string Object like
// `"escalated"` must remain a string. This is the dominant case in
// every production rule pack (18/19 sites in the cross-repo survey)
// and the typed path must NOT corrupt it.
func TestExecuteAddTriple_LiteralObject_PreservesString(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockTripleMutator{}
	executor := NewActionExecutorWithMutator(nil, mock)

	action := Action{
		Type:      ActionTypeAddTriple,
		Predicate: "workflow.phase",
		Object:    "escalated",
	}

	_, err := executor.ExecuteAddTriple(ctx, action, &ExecutionContext{
		EntityID: "c360.platform.robotics.mav1.drone.001",
	})
	require.NoError(t, err)

	require.Len(t, mock.addedTriples, 1)
	got := mock.addedTriples[0].Object
	assert.IsType(t, "", got, "literal Object should be string")
	assert.Equal(t, "escalated", got)
}

// TestExecuteAddTriple_FirstMatchSemantic_MixedTypes documents the
// degenerate behaviour: when an entity carries multiple triples with
// the same predicate but different Object types, the FIRST stamped
// wins by both type AND value. Pre-fix, all values were
// string-coerced so the type mismatch was invisible — this is the
// only observable behaviour change, and the cross-repo survey
// found zero production rules that exercise it.
func TestExecuteAddTriple_FirstMatchSemantic_MixedTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entityID := "c360.platform.robotics.mav1.drone.001"
	mock := &mockTripleMutator{}
	executor := NewActionExecutorWithMutator(nil, mock)

	// Degenerate: two triples, same predicate, different types. First
	// stamped is float64; second is string. The typed path returns the
	// first match.
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "ambiguous", Object: float64(1.5)},
			{Subject: entityID, Predicate: "ambiguous", Object: "second-stamp"},
		},
	}

	action := Action{
		Type:      ActionTypeAddTriple,
		Predicate: "dest",
		Object:    "$entity.triple.ambiguous",
	}

	_, err := executor.ExecuteAddTriple(ctx, action, &ExecutionContext{
		EntityID: entityID,
		Entity:   entity,
	})
	require.NoError(t, err)

	require.Len(t, mock.addedTriples, 1)
	got := mock.addedTriples[0].Object
	assert.Equal(t, float64(1.5), got, "first-match by both value AND type")
	assert.Equal(t, reflect.TypeOf(float64(0)), reflect.TypeOf(got))
}

// TestNumericCompare_AfterStringifiedObject_UsesNumericSemantics is
// the gh#207 read-side integration test. Even AFTER the typed-Object
// fix on the write side, any pre-existing stringified-numeric Object
// (left over from before the fix, or stamped through an external
// producer that uses fmt.Sprintf rather than typed Object) must still
// participate in numeric `lt`/`gt`/`between` comparisons correctly.
//
// Pre-toFloat64-widening: `"9.9" lt 10.0` lex-compared as
// "9.9" > "10" (because '9' > '1'), so the condition silently
// returned false — wrong by numeric semantics. Power-of-10 boundary
// is the surface where lex and numeric disagree.
//
// Drives the production wire — NewExpressionRule(def).EvaluateEntityState —
// per [[feedback_integration_tests_must_drive_production_wire]]. A
// helper-direct compareValues test would pass without exercising the
// rule-evaluation path that production uses.
func TestNumericCompare_AfterStringifiedObject_UsesNumericSemantics(t *testing.T) {
	tests := []struct {
		name      string
		objectVal any
		operator  string
		compareTo float64
		want      bool
	}{
		// Power-of-10 boundary — the bug surface where lex and numeric
		// disagree. Pre-fix: "9.9" > "10" lex → lt returns false.
		// Post-fix: 9.9 < 10 numeric → lt returns true.
		{"string 9.9 lt 10.0 — boundary case", "9.9", "lt", 10.0, true},
		{"string 9.9 gt 10.0 — boundary case", "9.9", "gt", 10.0, false},

		// Magnitudes within the same order — lex and numeric agree
		// (would have passed pre-fix too).
		{"string 0.4 lt 0.5", "0.4", "lt", 0.5, true},
		{"string 0.8 gt 0.5", "0.8", "gt", 0.5, true},

		// Float64 source (no stringification) — unchanged behavior.
		{"float64 9.9 lt 10.0", float64(9.9), "lt", 10.0, true},
		{"float64 9.9 gt 10.0", float64(9.9), "gt", 10.0, false},

		// Non-numeric strings still fail numeric compare and fall to
		// string compare — regression-safe path for legitimate string
		// values that happen to be compared with a numeric operator.
		// "alpha" > "10" lex because 'a' (0x61) > '1' (0x31), so lt
		// returns false. Asserting the fallback path stays intact.
		{"non-numeric string vs numeric — falls to string compare", "alpha", "lt", 10.0, false},

		// json.Number support — UseNumber() decoder produces these to
		// preserve precision. compareValuesWithError must numeric-
		// compare against them, not fall to lex on the underlying
		// string. Per the type-switch case in toFloat64.
		{"json.Number lt 10.0 — boundary case", json.Number("9.9"), "lt", 10.0, true},
		{"json.Number gt 10.0 — boundary case", json.Number("9.9"), "gt", 10.0, false},

		// NaN rejection — strconv.ParseFloat accepts "NaN" but the
		// resulting compare would silently produce "equal" (NaN op
		// anything = false → compareValuesWithError returns 0 = equal).
		// We explicitly reject NaN at toFloat64 so the fallback string
		// path handles it deterministically. "NaN" > "10" lex
		// ('N' = 0x4E > '1' = 0x31), so lt returns false.
		{"NaN string rejected from numeric — falls to string", "NaN", "lt", 10.0, false},

		// Inf rejection — same shape. "Inf" > "10" lex ('I' > '1'),
		// so lt returns false. Without rejection, +Inf < 10 numeric
		// would be false; rejecting routes through lex which agrees
		// by coincidence here, but the discipline is "don't admit
		// non-finite floats into compare."
		{"Inf string rejected from numeric — falls to string", "Inf", "lt", 10.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Definition{
				ID:      "gh207-numeric-compare-after-stringified",
				Type:    "expression",
				Name:    "gh#207 numeric compare locks",
				Enabled: true,
				Logic:   expression.LogicAnd,
				Conditions: []expression.ConditionExpression{
					{Field: "metric.value", Operator: tt.operator, Value: tt.compareTo, Required: true},
				},
			}
			rule, err := NewExpressionRule(def)
			require.NoError(t, err)

			entity := createTestEntityState("test.entity.id", []message.Triple{
				{Subject: "test.entity.id", Predicate: "metric.value", Object: tt.objectVal},
			})

			got := rule.EvaluateEntityState(entity)
			assert.Equal(t, tt.want, got,
				"compareValues with toFloat64 string-widening must produce numeric semantics for parsable strings")
		})
	}
}

// TestSubstituteVariablesTyped_ReturnContract directly exercises the
// helper's (any, bool) contract for each supported namespace, plus
// the fall-through cases. Driven through the public API on
// ExecutionContext so the regex-based dispatch + type-routing logic
// is locked.
func TestSubstituteVariablesTyped_ReturnContract(t *testing.T) {
	t.Parallel()
	entityID := "c360.platform.robotics.mav1.drone.001"
	relatedID := "c360.platform.fleet.alpha.fleet.001"

	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "metric.value", Object: float64(0.83)},
			{Subject: entityID, Predicate: "metric.active", Object: true},
		},
	}
	related := &gtypes.EntityState{
		ID: relatedID,
		Triples: []message.Triple{
			{Subject: relatedID, Predicate: "fleet.size", Object: int(7)},
		},
	}
	ec := &ExecutionContext{
		EntityID:    entityID,
		RelatedID:   relatedID,
		Entity:      entity,
		Related:     related,
		MessageData: map[string]any{"score": 0.42, "flag": true, "nested": map[string]any{"deep": "value"}, "nullable": nil},
		State:       &MatchState{Iteration: 3, MaxIterations: 5},
	}

	tests := []struct {
		name       string
		template   string
		wantOK     bool
		wantValue  any
		wantTypeOf any // sample of expected Go type; nil when wantOK=false
	}{
		{"entity.triple float64", "$entity.triple.metric.value", true, float64(0.83), float64(0)},
		{"entity.triple bool", "$entity.triple.metric.active", true, true, false},
		{"related.triple int", "$related.triple.fleet.size", true, int(7), int(0)},
		{"message scalar float", "$message.score", true, 0.42, float64(0)},
		{"message scalar bool", "$message.flag", true, true, false},
		{"message nested path", "$message.nested.deep", true, "value", ""},
		// JSON null at terminal — ExtractMessageValue returns (nil, true);
		// the typed path propagates as (nil, true) with no type sample.
		// Locking this so the package-doc divergence note (#2) stays true.
		{"message null value — typed nil propagates", "$message.nullable", true, nil, nil},
		{"state.iteration int", "$state.iteration", true, int(3), int(0)},
		{"state.max_iterations int", "$state.max_iterations", true, int(5), int(0)},
		{"unresolved entity triple", "$entity.triple.missing", false, nil, nil},
		{"mixed template", "prefix-$entity.triple.metric.value", false, nil, nil},
		{"literal only", "escalated", false, nil, nil},
		{"bare $", "$", false, nil, nil},
		{"empty", "", false, nil, nil},
		{"trailing space", "$entity.triple.metric.value ", false, nil, nil},
		{"with .length suffix", "$entity.triple.metric.value.length", false, nil, nil},
		{"with .triples suffix", "$entity.triple.metric.value.triples", false, nil, nil},
		{"unsupported namespace $entity.id", "$entity.id", false, nil, nil},
		{"unsupported namespace $now", "$now", false, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, ok := ec.SubstituteVariablesTyped(tt.template)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				assert.Nil(t, val, "value should be nil on false return")
				return
			}
			assert.Equal(t, tt.wantValue, val)
			assert.Equal(t, reflect.TypeOf(tt.wantTypeOf), reflect.TypeOf(val),
				"Go type must match expected")
		})
	}
}
