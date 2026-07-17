//go:build integration

package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetIncomingEdges_ShardedCompositeKeys is the gh#474 regression + bugfix
// guard mandated by the change design (D2). GetIncomingEdges was BROKEN before
// this change — it unmarshaled a `{"incoming":[...]}` map against the written
// `[]IncomingEntry` blob and always returned empty (a silent-empty read feeding
// three production graph-traversal callers). Post-sharding it prefix-scans the
// composite keys `target.source.predicate` and reconstructs the distinct source
// IDs. This seeds real composite keys and asserts the CORRECT sources — the old
// empty-return implementation fails this test.
func TestGetIncomingEdges_ShardedCompositeKeys(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	// Standalone: no graph-index handler runs here, so allow the direct bucket read
	// (gh#474 Codex #4 — AllowUngatedReads is the explicit opt-out from fail-closed).
	cfg := DefaultConfig()
	cfg.AllowUngatedReads = true
	client, err := NewClient(ctx, tc.Client, cfg)
	require.NoError(t, err)
	qc := client.(*natsClient)
	require.NoError(t, qc.ensureBuckets(ctx))

	target := "acme.ops.robotics.gcs.drone.001"
	srcA := "acme.ops.robotics.gcs.sensor.001"
	srcB := "acme.ops.robotics.gcs.sensor.002"

	// Two edges from srcA (distinct predicates) + one from srcB → 2 distinct sources.
	for _, key := range []string{
		target + "." + srcA + ".rel.observes",
		target + "." + srcA + ".rel.controls",
		target + "." + srcB + ".rel.observes",
	} {
		_, putErr := qc.incomingBucket.Put(ctx, key, []byte{})
		require.NoError(t, putErr)
	}

	sources, err := qc.GetIncomingEdges(ctx, target)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{srcA, srcB}, sources,
		"GetIncomingEdges must reconstruct the distinct source IDs from composite keys "+
			"(bugfix: the pre-sharding implementation returned empty)")

	// An unknown target yields an empty result, not an error.
	none, err := qc.GetIncomingEdges(ctx, "acme.ops.robotics.gcs.drone.999")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestGetEntityConnections_PropagatesIncomingReadinessError(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	cfg := DefaultConfig() // fail closed: no graph-index status responder is running.
	client, err := NewClient(context.Background(), tc.Client, cfg)
	require.NoError(t, err)
	qc := client.(*natsClient)
	require.NoError(t, qc.ensureBuckets(context.Background()))

	entityID := "acme.ops.robotics.gcs.drone.001"
	_, err = qc.entityBucket.Put(context.Background(), entityID, []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	connections, err := qc.GetEntityConnections(ctx, entityID)
	require.Error(t, err)
	assert.Nil(t, connections, "readiness failure must not return outgoing-only partial success")
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, graph.ErrorCodeIndexNotReady, classified.Code)
}

func TestGetEntityRejectsPredicatePoisonWithoutCachingPartialState(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	ctx := context.Background()
	client, err := NewClient(ctx, tc.Client, DefaultConfig())
	require.NoError(t, err)
	qc := client.(*natsClient)
	require.NoError(t, qc.ensureBuckets(ctx))

	entityID := "acme.ops.robotics.gcs.drone.001"
	poisoned := []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[{"subject":"acme.ops.robotics.gcs.drone.001","predicate":"legacy.predicate","object":"old"}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	_, err = qc.entityBucket.Put(ctx, entityID, poisoned)
	require.NoError(t, err)

	entity, err := qc.GetEntity(ctx, entityID)
	require.Error(t, err)
	assert.Nil(t, entity)
	var contractErr *graph.StateContractError
	require.ErrorAs(t, err, &contractErr)
	assert.Equal(t, graph.GraphStateReasonNoncanonicalPredicate, contractErr.Reason)
	_, cached := qc.cache.Get(entityID)
	assert.False(t, cached, "poisoned state must not enter the entity query cache")
}

func TestDirectQueryClient_LivePoisonInvalidatesCachedViews(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := NewClient(ctx, tc.Client, DefaultConfig())
	require.NoError(t, err)
	qc := client.(*natsClient)
	require.NoError(t, qc.ensureBuckets(ctx))

	validID := "acme.ops.robotics.gcs.drone.001"
	validRev, err := qc.entityBucket.Put(ctx, validID, []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return qc.entityObservedRev.Load() >= validRev }, time.Second, 10*time.Millisecond)
	entity, err := qc.GetEntity(ctx, validID)
	require.NoError(t, err)
	require.Equal(t, validID, entity.ID)
	_, cached := qc.cache.Get(validID)
	require.True(t, cached)

	poisonID := "acme.ops.robotics.gcs.drone.002"
	_, err = qc.entityBucket.Put(ctx, poisonID, []byte(`{"id":"acme.ops.robotics.gcs.drone.002","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":"old"}]}`)) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	require.NoError(t, err)
	require.Eventually(t, func() bool { return qc.entityStatePoison.Load() != nil }, time.Second, 10*time.Millisecond)

	entity, err = qc.GetEntity(ctx, validID)
	require.Error(t, err)
	assert.Nil(t, entity, "a cached valid entity must not escape after poison is observed elsewhere")
	_, cached = qc.cache.Get(validID)
	assert.False(t, cached, "poison discovery clears every cached graph view")
}

func TestGetEntitiesBatch_DoesNotReturnPartialSuccess(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := NewClient(ctx, tc.Client, DefaultConfig())
	require.NoError(t, err)
	qc := client.(*natsClient)
	require.NoError(t, qc.ensureBuckets(ctx))

	validID := "acme.ops.robotics.gcs.drone.001"
	_, err = qc.entityBucket.Put(ctx, validID, []byte(`{"id":"acme.ops.robotics.gcs.drone.001","triples":[]}`))
	require.NoError(t, err)

	entities, err := qc.GetEntitiesBatch(ctx, []string{validID, "acme.ops.robotics.gcs.drone.999"})
	require.Error(t, err)
	assert.Nil(t, entities, "a missing/unreadable member must fail the batch instead of returning a subset")
}
