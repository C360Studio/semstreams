package crudtools

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFireEveryNRuleDefinitionScopesOnlySeededProbeEntities(t *testing.T) {
	t.Parallel()

	definition := fireEveryNRuleDefinition()
	require.Equal(t, "org.platform.test.probe.e2e.*", definition.Entity.Pattern)
	for i := 1; i <= fireEveryNProbeCount; i++ {
		entityID := fmt.Sprintf("org.platform.test.probe.e2e.%03d", i)
		matches, err := types.MatchEntityIDPattern(definition.Entity.Pattern, entityID)
		require.NoError(t, err)
		require.True(t, matches, "fixture pattern must cover seeded entity %s", entityID)
	}

	matches, err := types.MatchEntityIDPattern(
		definition.Entity.Pattern,
		"org.platform.test.probe.other.001",
	)
	require.NoError(t, err)
	require.False(t, matches, "fixture pattern must reject adjacent non-seed entities")
	require.Equal(t, fireEveryNWindowN, definition.FireEveryNEvents)
	require.Empty(t, definition.OnEnter)
	require.Empty(t, definition.OnExit)
	require.Empty(t, definition.WhileTrue)
	require.Empty(t, definition.OnRecovery)
	require.Empty(t, definition.Actions)
	require.NoError(t, rule.ValidateDefinition(definition))

	raw, err := json.Marshal(definition)
	require.NoError(t, err)
	var roundTrip rule.Definition
	require.NoError(t, json.Unmarshal(raw, &roundTrip))
	require.Equal(t, definition.Entity.Pattern, roundTrip.Entity.Pattern)
	require.Equal(t, definition.Conditions, roundTrip.Conditions)
	require.Equal(t, fireEveryNWindowN, roundTrip.FireEveryNEvents)

	ruleInstance, err := rule.CreateRuleFromDefinition(roundTrip, rule.Dependencies{
		PackID: "crud-tools-e2e-test",
	})
	require.NoError(t, err)
	evaluator, ok := ruleInstance.(rule.EntityStateEvaluator)
	require.True(t, ok, "hot-reload expression rule must implement EntityStateEvaluator")

	probeEntity := func(entityID, entityType string) *graph.EntityState {
		now := time.Now()
		return &graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: rulepacks.EntityIdentityType,
					Object:    entityType,
					Source:    "e2e-fire-every-n-seed",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "e2e", Category: "probe", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		}
	}

	require.True(t, evaluator.EvaluateEntityState(probeEntity(
		"org.platform.test.probe.e2e.001",
		fireEveryNEntityType,
	)))
	assert.False(t, evaluator.EvaluateEntityState(probeEntity(
		"org.platform.test.probe.e2e.002",
		"other-probe-type",
	)), "in-scope entity with a mismatched type must not evaluate")
}

func TestFireEveryNMetricDeltasRequireExactContract(t *testing.T) {
	t.Parallel()

	baseline := fireEveryNMetricValues{triggered: 4, notTriggered: 2, gatePasses: 1}
	delta := (fireEveryNMetricValues{triggered: 13, notTriggered: 2, gatePasses: 4}).delta(baseline)
	require.Equal(t, fireEveryNMetricValues{
		triggered:  fireEveryNProbeCount,
		gatePasses: fireEveryNExpected,
	}, delta)
	require.True(t, delta.isExact())

	for name, candidate := range map[string]fireEveryNMetricValues{
		"missing evaluation":  {triggered: fireEveryNProbeCount - 1, gatePasses: fireEveryNExpected},
		"unexpected mismatch": {triggered: fireEveryNProbeCount, notTriggered: 1, gatePasses: fireEveryNExpected},
		"missing gate pass":   {triggered: fireEveryNProbeCount, gatePasses: fireEveryNExpected - 1},
		"extra gate pass":     {triggered: fireEveryNProbeCount, gatePasses: fireEveryNExpected + 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.False(t, candidate.isExact())
		})
	}
}
