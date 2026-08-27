//go:build integration

package rule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// MatchesWithLifecycle takes the CONCRETE *lifecycle.Manager (owner ruling
// 2026-07-31), which cannot be constructed without a NATS client — so the exported
// wrapper's wiring is only reachable at this tier. That is the acknowledged cost of
// the concrete signature, and this file is the part that keeps it a cost rather
// than a coverage hole.
//
// The failure-mode matrix (transient lookup error, context cancellation, the
// ordinary-definition regression guard) stays at unit level against
// matchesWithLookup — the shared implementation both entry points delegate to. See
// matches_test.go.

func matchesTestManager(t *testing.T) *lifecycle.Manager {
	t.Helper()
	tc := natsclient.NewTestClient(t)
	return lifecycle.NewManager(tc.Client, nil)
}

// TestIntegration_MatchesWithLifecycle_RealManagerUnresolvedEntity proves the
// exported entry point reaches a real Manager and that an entity it cannot resolve
// is REFUSED rather than answered.
//
// No workflow is registered, so LookupByEntityID returns ErrEntityNotFound — the
// "unregistered participant" case Codex finding 2 named, here against the real
// implementation rather than a fake.
func TestIntegration_MatchesWithLifecycle_RealManagerUnresolvedEntity(t *testing.T) {
	mgr := matchesTestManager(t)

	def := Definition{
		ID: "lifecycle-unresolved", Type: "expression", Name: "Unresolved", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			// NOT Required — the case that silently returned false before the fix.
			{Field: "$entity.lifecycle.phase", Operator: "eq", Value: "flying"},
		},
	}

	got, err := MatchesWithLifecycle(context.Background(), def,
		createTestEntityState("c360.platform1.gcs.lifecycle.mission.unregistered", nil), mgr)

	require.Error(t, err, "a lifecycle condition the real Manager cannot resolve must be "+
		"refused; answering false would report 'nothing owed' for a question never asked")
	assert.False(t, got, "verdict must be false alongside the error")
}

// TestIntegration_MatchesWithLifecycle_RealManagerOrdinaryDefinition is the
// regression guard for the bug my first fix to finding 2 introduced, asserted here
// against the real Manager: LookupByEntityID errors for ANY entity that is not
// lifecycle-managed, which is the ordinary case, so a definition with no lifecycle
// conditions must be entirely unaffected by that failure.
func TestIntegration_MatchesWithLifecycle_RealManagerOrdinaryDefinition(t *testing.T) {
	mgr := matchesTestManager(t)

	def := Definition{
		ID: "ordinary", Type: "expression", Name: "Ordinary", Enabled: true, Logic: "and",
		Conditions: []expression.ConditionExpression{
			{Field: "sensor.measurement.fahrenheit", Operator: "gte", Value: 40.0},
		},
	}
	entity := createTestEntityState("c360.platform1.gcs.lifecycle.mission.ordinary",
		[]message.Triple{{Subject: "t", Predicate: "sensor.measurement.fahrenheit", Object: 41.2}})

	got, err := MatchesWithLifecycle(context.Background(), def, entity, mgr)

	require.NoError(t, err, "an unrelated lifecycle lookup failure must not refuse a "+
		"definition that never asked about lifecycle")
	assert.True(t, got, "conditions hold and lifecycle is irrelevant here")
}

// TestIntegration_MatchesWithLifecycle_PairAgreesViaRealManager proves the two
// exported entry points are two doors to one implementation, using the real Manager
// on one side.
func TestIntegration_MatchesWithLifecycle_PairAgreesViaRealManager(t *testing.T) {
	mgr := matchesTestManager(t)

	for _, c := range matchCorpus() {
		t.Run(c.name, func(t *testing.T) {
			plain, errPlain := Matches(context.Background(), defFor(c),
				createTestEntityState("c360.platform1.gcs.lifecycle.mission.pair", c.triples))
			withLC, errLC := MatchesWithLifecycle(context.Background(), defFor(c),
				createTestEntityState("c360.platform1.gcs.lifecycle.mission.pair", c.triples), mgr)

			require.NoError(t, errPlain)
			require.NoError(t, errLC)
			assert.Equal(t, plain, withLC,
				"the pair diverged on a definition with no lifecycle conditions")
		})
	}
}
