//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_KVRevisionAndIndexedRevisionShareOneSpace pins the comparison ADR-084
// promotes to the ONE sound per-entity readiness check.
//
// With coverage retired as an absence license, a caller that needs "is MY write
// visible?" is told to compare the kv_revision its mutation returned against the
// envelope's indexed_revision. That instruction is only meaningful if the two numbers
// are the same revision space — and nothing exercised the comparison, so a future
// change to either side (a per-bucket counter, a monotonic clock, a per-key sequence)
// could break every read-your-writes caller with both sides still looking sensible in
// isolation.
//
// It runs BOTH components over one NATS so both numbers come from the production path:
// kv_revision from graph-ingest's real mutation handler, indexed_revision from
// graph-index's real watermark projection. A helper writing to the bucket directly
// would prove only that the bucket is self-consistent.
func TestIntegration_KVRevisionAndIndexedRevisionShareOneSpace(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	nc := testClient.Client

	startIngest(ctx, t, nc)
	index := startIndex(ctx, t, nc)

	entityID := "c360.test.revspace.system.drone.001"
	kvRevision := createEntity(ctx, t, nc, entityID)
	require.NotZero(t, kvRevision, "the mutation must report the revision it wrote")

	// The target is the ENTITY_STATES stream LastSeq. If kv_revision came from a
	// different space this would not reliably hold immediately after the write — which
	// is exactly the drift this test exists to catch, and it is race-free: LastSeq is
	// monotonic and the write already committed.
	status := index.computeIndexStatus(ctx)
	assert.GreaterOrEqual(t, status.TargetRevision, kvRevision,
		"target_revision must already cover a committed write; kv_revision and the "+
			"envelope's revisions are not the same space")

	// And read-your-writes converges: once the index applies the write, the envelope's
	// indexed_revision reaches it. Waiting on the PREDICATE rather than a sleep is the
	// assertion — the caller's real loop is exactly this comparison.
	require.Eventually(t, func() bool {
		return index.computeIndexStatus(ctx).IndexedRevision >= kvRevision
	}, 15*time.Second, 20*time.Millisecond,
		"indexed_revision never reached the write's kv_revision; read-your-writes is unusable")

	// A second write must advance the space again — a one-off coincidence between two
	// counters would pass everything above.
	secondRevision := addTriple(ctx, t, nc, entityID)
	assert.Greater(t, secondRevision, kvRevision, "a later write must carry a later revision")
	require.Eventually(t, func() bool {
		return index.computeIndexStatus(ctx).IndexedRevision >= secondRevision
	}, 15*time.Second, 20*time.Millisecond, "the index did not catch up to the second write")
}

// startIngest runs a real graph-ingest so the mutation subjects are served by the
// production handler. The component itself is not returned: everything this test does
// goes over the wire, which is the point.
func startIngest(ctx context.Context, t *testing.T, nc *natsclient.Client) {
	t.Helper()
	cfg, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	comp, err := graphingest.CreateGraphIngest(cfg, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	c := comp.(component.LifecycleComponent)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
}

func startIndex(ctx context.Context, t *testing.T, nc *natsclient.Client) *Component {
	t.Helper()
	cfg, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	comp, err := CreateGraphIndex(cfg, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

// createEntity drives the real create-entity subject and returns the reported
// kv_revision.
func createEntity(ctx context.Context, t *testing.T, nc *natsclient.Client, entityID string) uint64 {
	t.Helper()
	now := time.Now()
	client, err := graphmutation.NewClient(nc, 5*time.Second)
	require.NoError(t, err)
	response, err := client.Create(ctx, graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: message.Type{Domain: "test", Category: "revision", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		},
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "core.identity.type", Object: "drone",
			Timestamp: now, Confidence: 1.0,
		}},
	})
	require.NoError(t, err)
	return response.KVRevision
}

func addTriple(ctx context.Context, t *testing.T, nc *natsclient.Client, entityID string) uint64 {
	t.Helper()
	client, err := graphmutation.NewClient(nc, 5*time.Second)
	require.NoError(t, err)
	response, err := client.Append(ctx, graph.AppendTriplesRequest{
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "core.identity.label", Object: "second",
			Timestamp: time.Now(), Confidence: 1.0,
		}},
	})
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
	require.Equal(t, entityID, response.Results[0].EntityID)
	require.Contains(t, []graph.MutationOutcome{graph.MutationApplied, graph.MutationUnchanged}, response.Results[0].Outcome)
	return response.Results[0].KVRevision
}
