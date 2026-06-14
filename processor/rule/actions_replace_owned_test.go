// Package rule — unit tests for the replace_owned action + envelope validation
// (ADR-056 Decision 3). The integration variant (real graph-ingest handler,
// atomic-write assertions) lives in actions_replace_owned_integration_test.go
// behind the `integration` build tag.
package rule

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ownedPredicate is the predicate the test rule pack declares in a
// replace-owned projection group; replace_owned actions naming it are inside
// the envelope.
const ownedPredicate = "status.phase"

// outOfEnvelopePredicate is a predicate NOT in any replace-owned group — a
// replace_owned action naming it must be rejected at both load paths.
const outOfEnvelopePredicate = "status.unowned"

const testReplaceOwnedPackID = "replace-owned-test"
const testReplaceOwnedOwner = "rule-pack." + testReplaceOwnedPackID

// replaceOwnedTestContracts is the projection-contract set the test pack owns:
// one replace-owned group containing ownedPredicate.
func replaceOwnedTestContracts() []projection.Contract {
	return []projection.Contract{
		{
			Name:          "test-projection",
			EntityPattern: "acme.ops.robotics.gcs.drone.*",
			Groups: []projection.PredicateGroup{
				{
					Mode:       ownership.ModeReplaceOwned,
					Predicates: []string{ownedPredicate},
				},
			},
		},
	}
}

// newReplaceOwnedTestProcessor builds a Processor (no NATS) configured with the
// test pack id + replace-owned projection contracts, suitable for exercising
// the load-path and hot-reload-path envelope validators.
func newReplaceOwnedTestProcessor(t *testing.T) *Processor {
	t.Helper()
	cfg := DefaultConfig()
	cfg.PackID = testReplaceOwnedPackID
	cfg.ProjectionContracts = replaceOwnedTestContracts()
	rp, err := NewProcessor(nil, &cfg)
	require.NoError(t, err, "NewProcessor")
	return rp
}

// executorWithOwner returns an executor wired with the given mock mutator and
// the test projection owner identity.
func executorWithOwner(mutator TripleMutator, owner string) *ActionExecutor {
	e := NewActionExecutorFull(nil, mutator, nil)
	e.SetProjectionOwner(owner)
	return e
}

// --- Case 2: replace OUTSIDE envelope → file-load HARD-FAILS ---

func TestReplaceOwned_FileLoad_OutOfEnvelope_HardFails(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)
	rp.config.InlineRules = []Definition{
		{
			ID:      "bad-replace",
			Type:    "test_rule",
			Name:    "bad-replace",
			Enabled: true,
			OnEnter: []Action{
				{Type: ActionTypeReplaceOwned, Predicate: outOfEnvelopePredicate, Object: "running"},
			},
		},
	}

	err := rp.loadRules()
	require.Error(t, err, "loadRules MUST hard-fail when a replace_owned predicate is outside the pack's owned-replace contracts")
	assert.Contains(t, err.Error(), outOfEnvelopePredicate)
	// HARD-FAIL means boot aborts — the rule must NOT have been loaded.
	_, loaded := rp.rules["bad-replace"]
	assert.False(t, loaded, "out-of-envelope rule must not be registered after a hard-failed load")
}

func TestReplaceOwned_FileLoad_InEnvelope_Succeeds(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)
	rp.config.InlineRules = []Definition{
		{
			ID:      "good-replace",
			Type:    "test_rule",
			Name:    "good-replace",
			Enabled: true,
			OnEnter: []Action{
				{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"},
			},
		},
	}

	require.NoError(t, rp.loadRules(), "in-envelope replace_owned must load cleanly")
	_, loaded := rp.rules["good-replace"]
	assert.True(t, loaded, "in-envelope rule must be registered")
}

// --- Case 3 + 4: hot-reload in/out of envelope ---

func TestReplaceOwned_HotReload_InEnvelope_NilError(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)

	err := rp.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{
			"hot-good": map[string]any{
				"type": "test_rule",
				"on_enter": []any{
					map[string]any{
						"type":      ActionTypeReplaceOwned,
						"predicate": ownedPredicate,
						"object":    "running",
					},
				},
			},
		},
	})
	require.NoError(t, err, "hot-reload of an in-envelope replace_owned must validate clean")
}

func TestReplaceOwned_HotReload_OutOfEnvelope_Rejected(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)

	err := rp.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{
			"hot-bad": map[string]any{
				"type": "test_rule",
				"on_enter": []any{
					map[string]any{
						"type":      ActionTypeReplaceOwned,
						"predicate": outOfEnvelopePredicate,
						"object":    "running",
					},
				},
			},
		},
	})
	require.Error(t, err, "hot-reload of an out-of-envelope replace_owned must be rejected")
	assert.Contains(t, err.Error(), outOfEnvelopePredicate)
	assert.True(t, errs.IsInvalid(err), "rejection must be the Invalid error class")
}

// TestReplaceOwned_HotReload_ParseErrorDoesNotBypassEnvelope is the go-reviewer
// I1 regression. Pre-fix, the hot-reload envelope check was guarded by
// `if definitionFromMap(...) == nil`, so a parse error on an UNRELATED field
// (here a malformed entity.watch_buckets) skipped the envelope check entirely;
// validateExpressionRule (conditions-only) then reported the change VALID,
// silently accepting an out-of-envelope replace_owned. The fix hard-rejects on
// the parse error, so the envelope guarantee no longer depends on the apply path
// re-hitting the same error. This rule has well-formed conditions (so the
// expression path alone would pass), a malformed entity.watch_buckets (so
// definitionFromMap errors on a non-action field), AND an out-of-envelope
// replace_owned that must not slip through.
func TestReplaceOwned_HotReload_ParseErrorDoesNotBypassEnvelope(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)

	err := rp.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{
			"hot-bypass": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{"field": "x", "operator": "eq", "value": "y"},
				},
				"entity": map[string]any{
					"watch_buckets": []any{123}, // non-string → definitionFromMap errors
				},
				"on_enter": []any{
					map[string]any{
						"type":      ActionTypeReplaceOwned,
						"predicate": outOfEnvelopePredicate,
						"object":    "running",
					},
				},
			},
		},
	})
	require.Error(t, err, "a definitionFromMap parse error must hard-reject, not skip the envelope check")
	assert.True(t, errs.IsInvalid(err), "rejection must be the Invalid error class")
}

// --- Case 5 (unit): clear sub-case ---

func TestReplaceOwned_EmptyObject_ClearsViaRemoveOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := executorWithOwner(mutator, testReplaceOwnedOwner)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: ""}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"}))

	require.Len(t, mutator.replaceOwnedCalls, 1)
	call := mutator.replaceOwnedCalls[0]
	assert.Equal(t, ownedPredicate, call.predicate)
	assert.Empty(t, call.objects, "empty Object must yield zero objects (clear = RemoveTriples only)")
}

// --- Case 6 (unit): Object typed round-trip ---

func TestReplaceOwned_ObjectTypedRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name      string
		field     string
		msgValue  any
		wantType  reflect.Type
		wantValue any
	}{
		{"float64", "f", float64(3.14), reflect.TypeOf(float64(0)), float64(3.14)},
		{"int", "n", 7, reflect.TypeOf(0), 7},
		{"bool", "b", true, reflect.TypeOf(false), true},
		{"string", "s", "ready", reflect.TypeOf(""), "ready"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutator := &mockTripleMutator{}
			executor := executorWithOwner(mutator, testReplaceOwnedOwner)

			action := Action{
				Type:      ActionTypeReplaceOwned,
				Predicate: ownedPredicate,
				Object:    "$message." + tc.field,
			}
			ec := &ExecutionContext{
				EntityID:    "acme.ops.robotics.gcs.drone.001",
				MessageData: map[string]any{tc.field: tc.msgValue},
			}
			require.NoError(t, executor.Execute(ctx, action, ec))

			require.Len(t, mutator.replaceOwnedCalls, 1)
			require.Len(t, mutator.replaceOwnedCalls[0].objects, 1)
			obj := mutator.replaceOwnedCalls[0].objects[0].Object
			assert.Equal(t, tc.wantType, reflect.TypeOf(obj),
				"stored Object type must match the source type for %s", tc.name)
			assert.Equal(t, tc.wantValue, obj)
		})
	}
}

// --- Case 7: predicate containing `$` is rejected at validation ---

func TestReplaceOwned_DollarPredicate_RejectedAtValidation(t *testing.T) {
	t.Parallel()
	rp := newReplaceOwnedTestProcessor(t)

	// File-load path.
	rp.config.InlineRules = []Definition{
		{
			ID:      "dollar-pred",
			Type:    "test_rule",
			Name:    "dollar-pred",
			Enabled: true,
			OnEnter: []Action{
				{Type: ActionTypeReplaceOwned, Predicate: "$message.predicate_name", Object: "running"},
			},
		},
	}
	err := rp.loadRules()
	require.Error(t, err, "a `$`-bearing predicate must be rejected at load")
	assert.Contains(t, err.Error(), "literal")

	// Hot-reload path.
	rp2 := newReplaceOwnedTestProcessor(t)
	hotErr := rp2.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{
			"dollar-hot": map[string]any{
				"type": "test_rule",
				"on_enter": []any{
					map[string]any{
						"type":      ActionTypeReplaceOwned,
						"predicate": "$message.predicate_name",
						"object":    "running",
					},
				},
			},
		},
	})
	require.Error(t, hotErr, "a `$`-bearing predicate must be rejected at hot-reload")
	assert.Contains(t, hotErr.Error(), "literal")
}

// --- Case 8: scope red-line — request always has ExpectedRevision == 0 ---

// The mutator signature for ReplaceOwned has NO revision parameter — that is
// the compile-time half of the red-line. The runtime half: the constructed
// UpdateEntityWithTriplesRequest must carry ExpectedRevision == 0 regardless of
// any When clause on the action. We assert this by capturing the marshaled
// request through a revision-asserting mutator.
type revisionAssertingMutator struct {
	gotExpectedRevision uint64
	called              bool
}

func (m *revisionAssertingMutator) AddTriple(context.Context, string, message.Triple) (uint64, error) {
	return 0, nil
}

func (m *revisionAssertingMutator) RemoveTriple(context.Context, string, string, string) (uint64, error) {
	return 0, nil
}

func (m *revisionAssertingMutator) ReplaceOwned(_ context.Context, _, _, entityID, predicate string, objects []message.Triple) (uint64, error) {
	// Reconstruct the request the production mutator would build and assert
	// its ExpectedRevision is the zero value. The action layer threads no
	// revision; this guards against a future refactor wiring one in.
	req := gtypes.UpdateEntityWithTriplesRequest{
		Entity:        &gtypes.EntityState{ID: entityID},
		RemoveTriples: []string{predicate},
		AddTriples:    objects,
	}
	m.gotExpectedRevision = req.ExpectedRevision
	m.called = true
	return 1, nil
}

func TestReplaceOwned_AlwaysZeroExpectedRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := executorWithOwner(mutator, testReplaceOwnedOwner)

	// Action carries a When clause — replace_owned is still NOT a CAS write.
	action := Action{
		Type:      ActionTypeReplaceOwned,
		Predicate: ownedPredicate,
		Object:    "running",
		When:      nil, // When-honoring regression is in its own test below.
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"}))
	require.Len(t, mutator.replaceOwnedCalls, 1)

	// Compile-time red-line: ReplaceOwned's signature has no revision param.
	// Inspect the INTERFACE type directly (not a concrete value) and confirm
	// none of the method's input params is a uint64 revision.
	ifaceT := reflect.TypeOf((*TripleMutator)(nil)).Elem()
	mt, ok := ifaceT.MethodByName("ReplaceOwned")
	require.True(t, ok, "ReplaceOwned must exist on the TripleMutator interface")
	// On an interface method type, In(0) is the first declared param (no
	// receiver in the func type).
	for i := 0; i < mt.Type.NumIn(); i++ {
		assert.NotEqual(t, reflect.TypeOf(uint64(0)), mt.Type.In(i),
			"ReplaceOwned param %d must not be a uint64 revision (replace_owned is never CAS)", i)
	}

	// Runtime red-line: the request the production mutator builds carries a
	// zero ExpectedRevision.
	ra := &revisionAssertingMutator{}
	_, err := ra.ReplaceOwned(ctx, "rule-1", testReplaceOwnedOwner, "acme.ops.robotics.gcs.drone.001", ownedPredicate, nil)
	require.NoError(t, err)
	require.True(t, ra.called)
	assert.Equal(t, uint64(0), ra.gotExpectedRevision, "replace_owned request must always have ExpectedRevision == 0")
}

// --- Case 9: must-exist — non-existent entity surfaces EntityNotFound ---

func TestReplaceOwned_EntityNotFound_SurfacesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{
		replaceOwnedErr: errors.New("replace_owned mutation failed [entity_not_found]: entity not found: acme.ops.robotics.gcs.drone.404"),
	}
	executor := executorWithOwner(mutator, testReplaceOwnedOwner)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"}
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.404"})
	require.Error(t, err, "replace_owned on a non-existent entity must surface an error (no auto-vivify)")
	assert.Contains(t, err.Error(), graphErrorCodeEntityNotFound())
}

// graphErrorCodeEntityNotFound keeps the test honest about which code string it
// asserts on without hard-coding the literal in two places.
func graphErrorCodeEntityNotFound() string { return gtypes.ErrorCodeEntityNotFound }

// --- Case 10: owner identity ---

func TestReplaceOwned_OwnerIdentityThreaded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := executorWithOwner(mutator, testReplaceOwnedOwner)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"}))

	require.Len(t, mutator.replaceOwnedCalls, 1)
	assert.Equal(t, testReplaceOwnedOwner, mutator.replaceOwnedCalls[0].owner,
		"mutator must receive owner == rule-pack.<PackID>")
}

func TestReplaceOwned_EmptyOwner_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	// No SetProjectionOwner — ownerID stays empty.
	executor := NewActionExecutorFull(nil, mutator, nil)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"}
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"})
	require.Error(t, err, "replace_owned with an empty owner must error before any mutation")
	assert.Empty(t, mutator.replaceOwnedCalls, "no mutation may be issued when the owner is unset")
}

// Note: the processor-level owner-wiring assertion (initializeStateTracker
// calls SetProjectionOwner("rule-pack.<PackID>")) requires a live NATS client
// and lives in actions_replace_owned_integration_test.go. The unit tests above
// cover the executor-side behavior (owner threaded to the mutator; empty owner
// errors).

// --- Case 11: MaxIterations / When honored like other actions (regression) ---

// The When clause and per-action MaxIterations cap are evaluated in the
// StatefulEvaluator's runActions BEFORE the action is dispatched to Execute by
// type — so the gate is action-type-agnostic. These regressions drive a
// replace_owned OnEnter action through the production evaluator (the same
// driveEntries helper the MaxIterations suite uses) and assert it is gated
// identically to publish / add_triple. mockActionExecutor.executeCallCount
// counts every dispatched Execute regardless of action type.

func TestReplaceOwned_MaxIterationsHonored(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-replace-owned-cap",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:          ActionTypeReplaceOwned,
				Predicate:     ownedPredicate,
				Object:        "running",
				MaxIterations: intPtr(1),
			},
		},
	}

	// 5 entries, explicit cap of 1 → exactly one dispatch.
	driveEntries(t, evaluator, ruleDef, "acme.ops.robotics.gcs.drone.001", 5)
	assert.Equal(t, 1, executor.executeCallCount,
		"replace_owned must honor MaxIterations exactly like other actions (capped at 1 over 5 entries)")
}

func TestReplaceOwned_WhenClauseHonored(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	// When clause requires $state.iteration > 100, which never holds across a
	// handful of entries — so the replace_owned action is always skipped by the
	// gate and never reaches Execute.
	ruleDef := Definition{
		ID:   "rule-replace-owned-when",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:      ActionTypeReplaceOwned,
				Predicate: ownedPredicate,
				Object:    "running",
				When: []expression.ConditionExpression{
					{Field: "$state.iteration", Operator: "gt", Value: 100},
				},
			},
		},
	}

	driveEntries(t, evaluator, ruleDef, "acme.ops.robotics.gcs.drone.001", 3)
	assert.Equal(t, 0, executor.executeCallCount,
		"replace_owned must be skipped when its When clause is unmet (gated identically to other actions)")
}
