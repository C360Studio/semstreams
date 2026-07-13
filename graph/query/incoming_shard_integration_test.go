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
