// Package graphclustering — gh#665 + gh#666 regression.
//
// gh#665: kvProvider.GetEdgeWeight used to answer 1.0 for EVERY pair
// unconditionally. That made EntityIDProvider.GetEdgeWeight's cascade ("base
// weight > 0 → return it") fire on step 1 for every pair, so the sibling (0.7)
// and system-peer (0.3) virtual-edge weights (gh#461) were DEAD — every virtual
// edge voted at the explicit-dominant 1.0 in LPA. These tests pin the fix:
// GetEdgeWeight returns 1.0 only when `to` is an ACTUAL explicit neighbor of
// `from` (either direction), else 0.0, and the wrapping EntityIDProvider then
// resolves siblings to 0.7 and system-peers to 0.3.
//
// gh#666: GetEdgeWeight now does topology I/O, so computeNewLabel(X) would refetch
// X's neighbor set once per neighbor. The per-cycle edge cache collapses that to a
// single fetch; the call-count test locks it.
//
// ABLATION: revert kvProvider.GetEdgeWeight to `return 1.0, nil` and the sibling/
// system-peer/absent assertions below collapse to 1.0 and fail — these tests ARE
// the guard against the bug reappearing.
package graphclustering

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putOutgoingEdge writes an explicit from→to edge into the OUTGOING_INDEX mock
// (single key = source, JSON array of {to_entity_id, predicate}).
func putOutgoingEdge(t *testing.T, bucket *mockKVBucket, from string, tos ...string) {
	t.Helper()
	rows := make([]relationshipEntry, 0, len(tos))
	for _, to := range tos {
		rows = append(rows, relationshipEntry{Predicate: "robotics.assigned.mission", ToEntityID: to})
	}
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	_, err = bucket.Put(context.Background(), from, encoded)
	require.NoError(t, err)
}

// putIncomingEdge writes an explicit source→target edge into the composite-keyed
// INCOMING_INDEX mock (key = "target.source.hex(predicate)", empty value).
func putIncomingEdge(t *testing.T, bucket *mockKVBucket, target, source string) {
	t.Helper()
	key := target + "." + source + "." + graph.EncodePredicateToken("robotics.assigned.mission")
	_, err := bucket.Put(context.Background(), key, []byte{})
	require.NoError(t, err)
}

// TestKVProviderGetEdgeWeight_MembershipNotUnconditional is the direct gh#665 pin
// on the base provider: an explicit neighbor (either direction) weighs 1.0, an
// absent pair weighs 0.0 — never an unconditional 1.0.
func TestKVProviderGetEdgeWeight_MembershipNotUnconditional(t *testing.T) {
	ctx := context.Background()
	const (
		a = "acme.ops.robotics.gcs.drone.001"
		b = "acme.ops.robotics.gcs.mission.001" // explicit OUTGOING target of a
		c = "acme.ops.robotics.gcs.sensor.009"  // explicit INCOMING source into a
		d = "acme.ops.robotics.gcs.depot.077"   // no edge to a at all
	)

	outgoing := newMockKVBucket()
	incoming := newMockKVBucket()
	putOutgoingEdge(t, outgoing, a, b) // a → b
	putIncomingEdge(t, incoming, a, c) // c → a (an incoming edge on a)

	p := newKVProvider(newMockKVBucket(), outgoing, incoming, slog.Default())

	t.Run("explicit outgoing neighbor weighs 1.0", func(t *testing.T) {
		w, err := p.GetEdgeWeight(ctx, a, b)
		require.NoError(t, err)
		assert.Equal(t, 1.0, w)
	})

	t.Run("explicit incoming neighbor weighs 1.0 (both directions counted)", func(t *testing.T) {
		w, err := p.GetEdgeWeight(ctx, a, c)
		require.NoError(t, err)
		assert.Equal(t, 1.0, w, "an edge stored in the INCOMING index must still register as explicit")
	})

	t.Run("absent pair weighs 0.0 — the gh#665 fix", func(t *testing.T) {
		w, err := p.GetEdgeWeight(ctx, a, d)
		require.NoError(t, err)
		assert.Equal(t, 0.0, w, "a non-neighbor must weigh 0.0, not the old unconditional 1.0")
	})

	t.Run("membership is directional per source: b→a is not stored", func(t *testing.T) {
		// a→b is an outgoing edge on a; querying from b, a is neither b's outgoing
		// target nor b's incoming source in this fixture, so the pair weighs 0.0.
		w, err := p.GetEdgeWeight(ctx, b, a)
		require.NoError(t, err)
		assert.Equal(t, 0.0, w)
	})
}

// TestEntityIDProviderGetEdgeWeight_TiersComeAlive is the gh#665 blast-radius pin:
// wrapping the REAL kvProvider, the EntityIDProvider cascade now resolves sibling
// pairs to 0.7 and system-peer pairs to 0.3 (both were silently 1.0 while the base
// answered 1.0 for everything), while an actual explicit edge still dominates at
// 1.0 and a fully-unrelated pair weighs 0.0.
func TestEntityIDProviderGetEdgeWeight_TiersComeAlive(t *testing.T) {
	ctx := context.Background()
	const (
		// siblingA / siblingB share the 5-part type prefix → siblings (and, being
		// same-system, would also be system peers; siblings are checked first).
		siblingA = "c360.logistics.environmental.sensor.temperature.temp-001"
		siblingB = "c360.logistics.environmental.sensor.temperature.temp-002"
		// peerA / peerB share the system segment (gcs) but differ in type
		// (drone vs sensor) → system peers, NOT siblings.
		peerA = "acme.ops.robotics.gcs.drone.001"
		peerB = "acme.ops.robotics.gcs.sensor.002"
		// unrelated shares neither the type prefix nor the system of peerA.
		unrelated = "acme.ops.git.repo.commit.abc"
	)

	// No explicit edges in the base for the sibling/peer/absent cases: the base
	// resolves 0.0 and the cascade falls through to the virtual tiers.
	base := newKVProvider(newMockKVBucket(), newMockKVBucket(), newMockKVBucket(), slog.Default())
	provider := clustering.NewEntityIDProvider(base, clustering.DefaultEntityIDProviderConfig(), nil)

	t.Run("sibling-only pair resolves to 0.7 (was 1.0 — the bug)", func(t *testing.T) {
		w, err := provider.GetEdgeWeight(ctx, siblingA, siblingB)
		require.NoError(t, err)
		assert.Equal(t, 0.7, w)
	})

	t.Run("system-peer-only pair resolves to 0.3 (was 1.0 — the bug)", func(t *testing.T) {
		w, err := provider.GetEdgeWeight(ctx, peerA, peerB)
		require.NoError(t, err)
		assert.Equal(t, 0.3, w)
	})

	t.Run("fully-unrelated pair resolves to 0.0", func(t *testing.T) {
		w, err := provider.GetEdgeWeight(ctx, peerA, unrelated)
		require.NoError(t, err)
		assert.Equal(t, 0.0, w)
	})

	t.Run("actual explicit edge still dominates at 1.0", func(t *testing.T) {
		// Explicit edge peerA → peerB in the base overrides the 0.3 system-peer tier.
		outgoing := newMockKVBucket()
		putOutgoingEdge(t, outgoing, peerA, peerB)
		explicitBase := newKVProvider(newMockKVBucket(), outgoing, newMockKVBucket(), slog.Default())
		explicitProvider := clustering.NewEntityIDProvider(explicitBase, clustering.DefaultEntityIDProviderConfig(), nil)

		w, err := explicitProvider.GetEdgeWeight(ctx, peerA, peerB)
		require.NoError(t, err)
		assert.Equal(t, 1.0, w, "an explicit edge is strictly dominant over the system-peer tier")
	})
}

// TestKVProviderEdgeCache_OneFetchPerEntity is the gh#666 pin: replaying
// computeNewLabel(X)'s access shape — one GetNeighbors(X,"both") then a
// GetEdgeWeight(X,·) per neighbor — issues exactly ONE topology read for X, not M.
// The read is counted via the OUTGOING bucket's Get(X), the single I/O
// bothNeighborSet memoizes. ResetEdgeCache forces the next fetch, proving the
// cache is per-cycle and not build-once.
func TestKVProviderEdgeCache_OneFetchPerEntity(t *testing.T) {
	ctx := context.Background()
	const x = "acme.ops.robotics.gcs.drone.001"
	neighbors := []string{
		"acme.ops.robotics.gcs.mission.001",
		"acme.ops.robotics.gcs.mission.002",
		"acme.ops.robotics.gcs.mission.003",
		"acme.ops.robotics.gcs.mission.004",
	}

	// Count Get(x) on the OUTGOING bucket. bothNeighborSet(x) issues exactly one
	// outgoing Get(x) per fetch; every cache hit issues none.
	rows := make([]relationshipEntry, 0, len(neighbors))
	for _, n := range neighbors {
		rows = append(rows, relationshipEntry{Predicate: "robotics.assigned.mission", ToEntityID: n})
	}
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)

	var mu sync.Mutex
	getXCount := 0
	outgoing := newMockKVBucket()
	outgoing.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		if key == x {
			mu.Lock()
			getXCount++
			mu.Unlock()
			return &mockKVEntry{data: encoded}, nil
		}
		return nil, jetstream.ErrKeyNotFound
	}
	countOf := func() int { mu.Lock(); defer mu.Unlock(); return getXCount }

	p := newKVProvider(newMockKVBucket(), outgoing, newMockKVBucket(), slog.Default())

	// computeNewLabel(X): GetNeighbors warms the cache, then a per-neighbor
	// GetEdgeWeight fans out over it.
	got, err := p.GetNeighbors(ctx, x, "both")
	require.NoError(t, err)
	assert.ElementsMatch(t, neighbors, got)
	for _, n := range neighbors {
		w, err := p.GetEdgeWeight(ctx, x, n)
		require.NoError(t, err)
		assert.Equal(t, 1.0, w, "each neighbor is an explicit edge")
	}
	// One neighbor absent from the set must still resolve from the SAME cached fetch.
	wAbsent, err := p.GetEdgeWeight(ctx, x, "acme.ops.robotics.gcs.depot.999")
	require.NoError(t, err)
	assert.Equal(t, 0.0, wAbsent)

	assert.Equal(t, 1, countOf(),
		"one GetNeighbors + %d GetEdgeWeight over %s must issue ONE outgoing fetch, not %d",
		len(neighbors), x, len(neighbors)+1)

	// Per-cycle lifetime: ResetEdgeCache must force a fresh fetch on the next lookup.
	p.ResetEdgeCache()
	_, err = p.GetEdgeWeight(ctx, x, neighbors[0])
	require.NoError(t, err)
	assert.Equal(t, 2, countOf(), "ResetEdgeCache must re-fetch X's topology next cycle")
}
