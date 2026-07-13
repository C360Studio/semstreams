package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessEntityUpdate_WriteFailure_WithholdsReadiness covers the P1b failure-honesty
// contract (gh#474): a required index write that ultimately fails must (1) propagate an
// error, (2) mark the entity failed so readiness is withheld, (3) NOT store the no-op
// baseline (so a retry re-attempts), and (4) clear on a subsequent clean re-index.
func TestProcessEntityUpdate_WriteFailure_WithholdsReadiness(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	entityID := "acme.ops.robotics.gcs.drone.001"
	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "robotics.assigned.mission", Object: "acme.ops.robotics.gcs.mission.001"},
		},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)

	// Inject a persistent Put failure on the incoming bucket — every attempt fails.
	incomingMock(comp).putFunc = func(_ context.Context, _ string, _ []byte) (uint64, error) {
		return 0, errors.New("kv backend down")
	}

	// Zero-success incoming write → error propagates, entity marked failed.
	err = comp.processEntityUpdateFromData(ctx, entityID, data)
	require.Error(t, err, "ultimate write failure must propagate, not be swallowed as success")
	assert.Equal(t, int64(1), comp.failedCount.Load(), "failed entity must withhold readiness")

	// Baseline NOT stored on failure — a re-index must re-attempt, not be a no-op.
	_, stored := comp.lastProjections.Load(entityID)
	assert.False(t, stored, "no-op baseline must not be stored when writes failed")

	// Recover: writes succeed, re-index clears the failure and stores the baseline.
	incomingMock(comp).putFunc = nil
	require.NoError(t, comp.processEntityUpdateFromData(ctx, entityID, data))
	assert.Equal(t, int64(0), comp.failedCount.Load(), "clean re-index must clear the failure")
	_, stored = comp.lastProjections.Load(entityID)
	assert.True(t, stored, "baseline stored after a successful index")
}

// TestProcessEntityUpdate_PartialFailure_MarksFailed proves a PARTIAL write failure
// (some indexes succeed, one fails) still withholds readiness — not just zero-success.
func TestProcessEntityUpdate_PartialFailure_MarksFailed(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	entityID := "acme.ops.robotics.gcs.drone.002"
	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			// A context-bearing triple so the CONTEXT bucket is actually written.
			{Subject: entityID, Predicate: "robotics.assigned.mission", Object: "acme.ops.robotics.gcs.mission.002", Context: "inference.hierarchy"},
			{Subject: entityID, Predicate: "core.identity.alias", Object: "drone-002"},
		},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)

	// Only the context bucket fails; incoming/outgoing/predicate/alias succeed.
	contextMock(comp).putFunc = func(_ context.Context, _ string, _ []byte) (uint64, error) {
		return 0, errors.New("context kv down")
	}

	err = comp.processEntityUpdateFromData(ctx, entityID, data)
	require.Error(t, err, "a partial write failure must still be reported")
	assert.Equal(t, int64(1), comp.failedCount.Load(), "partial failure must mark the entity failed")
}

// TestComputeIndexProjection_AliasAxis proves the projection distinguishes an
// alias-only change (P2b) — otherwise the no-op counter miscounts it as unchanged.
func TestComputeIndexProjection_AliasAxis(t *testing.T) {
	aliasPreds := map[string]int{}
	namePreds := map[string]int{}
	entityID := "acme.ops.robotics.gcs.drone.003"

	base := graph.EntityState{ID: entityID, Triples: []message.Triple{
		{Subject: entityID, Predicate: "core.identity.alias", Object: "drone-A"},
	}}
	changed := graph.EntityState{ID: entityID, Triples: []message.Triple{
		{Subject: entityID, Predicate: "core.identity.alias", Object: "drone-B"},
	}}

	p1 := computeIndexProjection(base, namePreds, aliasPreds)
	p2 := computeIndexProjection(changed, namePreds, aliasPreds)
	assert.NotEqual(t, p1, p2, "an alias-value change must change the projection (P2b)")
}
