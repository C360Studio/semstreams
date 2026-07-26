//go:build integration

// Real-NATS proof of the gh#665/#666 edge-weight contract at the PRODUCTION seam.
//
// Every other edge-weight/membership test in this package is unit/mock. This one
// drives newKVProvider against real JetStream KV buckets, seeded through the exact
// key/value formats the production writer (processor/graph-index) uses:
//   - OUTGOING_INDEX: single key = source, JSON array of {to_entity_id, predicate}
//   - INCOMING_INDEX: composite key "targetID.sourceID.hex(predicate)", empty marker
//
// It then wraps kvProvider in EntityIDProvider exactly as the component does, so the
// per-cycle ResetEdgeCache reset is driven through the decorator chain's
// `interface{ ResetEdgeCache() }` type assertion — the seam a mock can never prove
// and that would silently no-op if the base stopped implementing ResetEdgeCache.
package graphclustering

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// edgePredicate is a canonical 3-part predicate accepted by vocabulary.ParsePredicate.
const edgePredicate = "robotics.assigned.mission"

// seedOutgoingEdge writes an explicit from→to edge into a REAL OUTGOING_INDEX
// bucket using the production single-key / JSON-array format that
// getNeighborsFromBucket parses (unchanged after gh#474 composite-key sharding).
func seedOutgoingEdge(ctx context.Context, t *testing.T, bucket jetstream.KeyValue, from, to string) {
	t.Helper()
	rows := []relationshipEntry{{Predicate: edgePredicate, ToEntityID: to}}
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, from, data)
	require.NoError(t, err)
}

// incomingCompositeKey mirrors processor/graph-index's incomingIndexKey:
// "targetID.sourceID.hex(predicate)".
func incomingCompositeKey(target, source string) string {
	return target + "." + source + "." + graph.EncodePredicateToken(edgePredicate)
}

// seedIncomingEdge writes an explicit source→target edge into a REAL INCOMING_INDEX
// bucket using the production composite-key format (empty marker value) that
// getIncomingNeighbors parses back via the prefix scan + SplitN(suffix, ".", 7)
// + DecodePredicateToken path.
func seedIncomingEdge(ctx context.Context, t *testing.T, bucket jetstream.KeyValue, target, source string) {
	t.Helper()
	_, err := bucket.Put(ctx, incomingCompositeKey(target, source), []byte{})
	require.NoError(t, err)
}

// TestIntegration_EdgeWeightMembership_RealBuckets proves, against real NATS, the
// three properties mocks never exercise: bidirectional symmetry through the REAL
// INCOMING_INDEX composite-key parse, cascade fall-through to 0.0 (not the old flat
// 1.0) for a non-neighbor pair, and per-cycle freshness driven by ResetEdgeCache
// propagating through the EntityIDProvider decorator down to kvProvider.
func TestIntegration_EdgeWeightMembership_RealBuckets(t *testing.T) {
	// Three entities that are PAIRWISE structurally unrelated — different system
	// segment (part[3]) AND different 5-part type prefix — so NO sibling (0.7) or
	// system-peer (0.3) virtual edge qualifies for any pair. Their only possible
	// connection is an explicit index edge, which lets the EntityIDProvider cascade
	// fall all the way through to 0.0 for non-edges.
	const (
		entA = "c360.plat.docs.alpha.report.a1"
		entB = "c360.plat.telemetry.beta.sensor.b1"
		entC = "acme.ops.git.gamma.commit.c1"
	)

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEntityStates})
	require.NoError(t, err)
	outgoingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketOutgoingIndex})
	require.NoError(t, err)
	incomingBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketIncomingIndex})
	require.NoError(t, err)

	// One explicit edge A→B, written in BOTH directions exactly as graph-index does:
	//   OUTGOING_INDEX[A] = [{to:B}]      (A sees B via the outgoing single-key read)
	//   INCOMING_INDEX["B.A.hex(pred)"]   (B sees A via the composite-key prefix scan)
	seedOutgoingEdge(ctx, t, outgoingBucket, entA, entB)
	seedIncomingEdge(ctx, t, incomingBucket, entB, entA)

	// Construct the REAL provider and wrap it as production does (component
	// initProviderAndDetector -> NewEntityIDProvider). The base kvProvider is the
	// object under test; the EntityIDProvider decorator is what forwards
	// ResetEdgeCache through its interface{ ResetEdgeCache() } assertion.
	base := newKVProvider(entityBucket, outgoingBucket, incomingBucket, slog.Default())
	provider := clustering.NewEntityIDProvider(base, clustering.DefaultEntityIDProviderConfig(), slog.Default())

	// 1) Bidirectional symmetry. GetEdgeWeight(A,B) resolves via A's OUTGOING row;
	//    GetEdgeWeight(B,A) resolves via B's INCOMING composite-key parse — the path
	//    mocks never run. Both must be 1.0 (explicit dominates the cascade).
	t.Run("explicit edge is symmetric at 1.0 through the real incoming parse", func(t *testing.T) {
		wAB, err := provider.GetEdgeWeight(ctx, entA, entB)
		require.NoError(t, err)
		assert.Equal(t, 1.0, wAB, "A→B resolves via the OUTGOING single-key read")

		wBA, err := provider.GetEdgeWeight(ctx, entB, entA)
		require.NoError(t, err)
		assert.Equal(t, 1.0, wBA, "B→A resolves via the INCOMING composite-key prefix scan + predicate decode")
	})

	// 2) Non-neighbor pair falls through the cascade to 0.0 — the gh#665 fix (the old
	//    unconditional base 1.0 would have short-circuited step 1 of the cascade).
	t.Run("non-neighbor pair resolves to 0.0 (cascade fall-through, not flat 1.0)", func(t *testing.T) {
		wAC, err := provider.GetEdgeWeight(ctx, entA, entC)
		require.NoError(t, err)
		assert.Equal(t, 0.0, wAC, "no explicit edge and no sibling/system-peer tier applies → 0.0")
	})

	// 3) Topology transition across a cycle boundary. Delete the A→B edge from both
	//    buckets, then prove: the WARM per-cycle cache still answers 1.0 (so the
	//    assertion is not vacuous), and only after ResetEdgeCache at the TOP of the
	//    chain does the fresh read return 0.0 — proving the reset propagated through
	//    EntityIDProvider down to kvProvider.
	t.Run("cycle boundary: reset propagates through the decorator to refresh topology", func(t *testing.T) {
		require.NoError(t, outgoingBucket.Delete(ctx, entA))
		require.NoError(t, incomingBucket.Delete(ctx, incomingCompositeKey(entB, entA)))

		// The cache was warmed by subtest 1's GetEdgeWeight(A,B); until the cycle
		// boundary it deterministically still reflects the old edge (pure cache hit,
		// no bucket read), which is exactly the staleness ResetEdgeCache exists to clear.
		wStale, err := provider.GetEdgeWeight(ctx, entA, entB)
		require.NoError(t, err)
		assert.Equal(t, 1.0, wStale, "before reset the warm per-cycle cache still reflects the deleted edge")

		// Cycle boundary reset, driven at the top of the chain exactly as
		// LPADetector.DetectCommunities does. If EntityIDProvider failed to forward
		// this to kvProvider, the stale {A:{B}} entry would survive and the assertion
		// below would still see 1.0.
		provider.ResetEdgeCache()

		require.Eventually(t, func() bool {
			w, err := provider.GetEdgeWeight(ctx, entA, entB)
			return err == nil && w == 0.0
		}, 5*time.Second, 100*time.Millisecond,
			"after ResetEdgeCache the deleted A→B edge must resolve to 0.0 (fresh real-bucket read, reset reached kvProvider)")
	})
}
