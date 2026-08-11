package clustering

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observingProvider wraps MockProvider and snapshots the community index on every
// graph read. Detection issues thousands of graph reads spread across the whole
// run, so this gives a dense set of observation points from "just started" to
// "about to return" — the same vantage a concurrent GraphRAG reader has.
type observingProvider struct {
	*MockProvider
	storage CommunityStorage

	observations int
	minSeen      int
}

func newObservingProvider(base *MockProvider, storage CommunityStorage) *observingProvider {
	return &observingProvider{
		MockProvider: base,
		storage:      storage,
		minSeen:      math.MaxInt,
	}
}

func (p *observingProvider) observe(ctx context.Context) {
	communities, err := p.storage.GetAllCommunities(ctx)
	if err != nil {
		return
	}
	p.observations++
	if len(communities) < p.minSeen {
		p.minSeen = len(communities)
	}
}

func (p *observingProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	p.observe(ctx)
	return p.MockProvider.GetNeighbors(ctx, entityID, direction)
}

func (p *observingProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	p.observe(ctx)
	return p.MockProvider.GetEdgeWeight(ctx, fromID, toID)
}

// twoTriangleProvider builds a graph with two clearly separated communities.
func twoTriangleProvider() *MockProvider {
	provider := NewMockProvider()
	for _, id := range []string{"A", "B", "C", "D", "E", "F"} {
		provider.AddEntity(id)
	}
	provider.AddEdge("A", "B", 1.0)
	provider.AddEdge("B", "C", 1.0)
	provider.AddEdge("C", "A", 1.0)
	provider.AddEdge("D", "E", 1.0)
	provider.AddEdge("E", "F", 1.0)
	provider.AddEdge("F", "D", 1.0)
	provider.AddEdge("C", "D", 0.1)
	return provider
}

// TestLPADetector_IndexNeverEmptyDuringRebuild is the regression for the
// clear-then-rebuild window. Detection takes seconds; if the index is cleared up
// front, every reader during that window gets an authoritative-looking empty
// answer. Write-then-prune means a reader sees old ∪ new instead.
func TestLPADetector_IndexNeverEmptyDuringRebuild(t *testing.T) {
	ctx := context.Background()
	base := twoTriangleProvider()
	storage := NewMockCommunityStorage()

	// Seed the index with a prior partition.
	seedDetector := NewLPADetector(base, storage).WithMaxIterations(50)
	first, err := seedDetector.DetectCommunities(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, first[0])

	seeded, err := storage.GetAllCommunities(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, seeded, "precondition: index must be populated before the observed run")

	// Second run, observed throughout.
	obs := newObservingProvider(base, storage)
	detector := NewLPADetector(obs, storage).WithMaxIterations(50)

	_, err = detector.DetectCommunities(ctx)
	require.NoError(t, err)

	require.Greater(t, obs.observations, 10,
		"sanity: the observer must actually sample during the run")
	assert.Greater(t, obs.minSeen, 0,
		"a reader observing COMMUNITY_INDEX at any point during a detection run must never see it empty")
}

// TestLPADetector_PruneRemovesStalePartition proves the replacement half of the
// rebuild: state belonging only to the previous partition is dropped, and an
// entity claimed by both partitions ends up pointing at the new community.
func TestLPADetector_PruneRemovesStalePartition(t *testing.T) {
	ctx := context.Background()
	provider := twoTriangleProvider()
	storage := NewMockCommunityStorage()

	// A partition the graph cannot reproduce: "ghost" claims entity A (which the
	// new run will re-assign) plus ZZZ (which does not exist in the graph).
	require.NoError(t, storage.SaveCommunity(ctx, &Community{
		ID:      "ghost",
		Level:   0,
		Members: []string{"A", "ZZZ"},
	}))

	detector := NewLPADetector(provider, storage).WithMaxIterations(50)
	result, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, result[0])

	ghost, err := storage.GetCommunity(ctx, "ghost")
	require.NoError(t, err)
	assert.Nil(t, ghost, "a community absent from the new partition must be pruned")

	// An entity that exists only in the stale partition loses its mapping.
	orphan, err := storage.GetEntityCommunity(ctx, "ZZZ", 0)
	require.NoError(t, err)
	assert.Nil(t, orphan, "entity mapping for an entity absent from the new partition must not resolve")

	// The query surface above is NOT sufficient on its own: a surviving mapping
	// dereferences to an already-pruned community and reads as nil either way.
	// The observable damage from leaked mappings is unbounded key growth (one key
	// per entity that ever churned), so assert on the mapping table directly.
	// TestIntegration_Prune pins the same property against real NATS keys.
	assert.NotContains(t, storage.entityCommunity[0], "ZZZ",
		"entity mapping key for a pruned entity must be removed, not merely unresolvable")

	// A re-assigned entity points at its new community, not the stale one.
	reassigned, err := storage.GetEntityCommunity(ctx, "A", 0)
	require.NoError(t, err)
	require.NotNil(t, reassigned, "A is in the new partition and must remain resolvable")
	assert.NotEqual(t, "ghost", reassigned.ID)
	assert.Contains(t, reassigned.Members, "A")

	newIDs := make([]string, 0, len(result[0]))
	for _, c := range result[0] {
		newIDs = append(newIDs, c.ID)
	}
	assert.Contains(t, newIDs, reassigned.ID,
		"the entity mapping must resolve to a community from the run that just finished")
}

// TestLPADetector_PruneKeepSetSpansEveryLevel covers the detector's CONSTRUCTION of
// the keep set, which TestIntegration_Prune does not: that test hands Prune a
// hand-built keep set and checks the key derivation, so replacing the all-levels
// loop in DetectCommunities with `keep = append(keep, result[0]...)` — which prunes
// every level-1 and level-2 community and mapping on every run — passes it.
//
// Assertions deliberately go through the LEVEL-SCOPED storage surfaces.
// GetCommunity/GetAllCommunities are level-blind (GetCommunity scans levels
// ascending), so a level-0 record with a colliding ID answers for a pruned
// level-1 one and the mutation stays green.
func TestLPADetector_PruneKeepSetSpansEveryLevel(t *testing.T) {
	ctx := context.Background()
	provider := twoTriangleProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage).WithMaxIterations(50)
	_, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Steady state: a second run prunes against a populated index.
	result, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	require.Greater(t, len(result), 1, "precondition: detection must produce hierarchical levels")

	for level := 1; level < len(result); level++ {
		require.NotEmpty(t, result[level], "precondition: level %d must have communities", level)

		stored, err := storage.GetCommunitiesByLevel(ctx, level)
		require.NoError(t, err)

		want := make([]string, 0, len(result[level]))
		members := make([]string, 0)
		for _, c := range result[level] {
			want = append(want, c.ID)
			members = append(members, c.Members...)
		}
		got := make([]string, 0, len(stored))
		for _, c := range stored {
			got = append(got, c.ID)
		}
		assert.ElementsMatch(t, want, got,
			"every level-%d community the run returned must survive the prune, and no other", level)

		// Entity mappings are keyed per level; the keep set must reconstruct
		// those too or LocalSearch above level 0 resolves nothing.
		require.NotEmpty(t, members)
		for _, entityID := range members {
			mapped, err := storage.GetEntityCommunity(ctx, entityID, level)
			require.NoError(t, err)
			assert.NotNil(t, mapped,
				"level-%d mapping for %s must survive the prune", level, entityID)
		}
	}
}

// TestLPADetector_PruneFailureLeavesSupersetNotError pins the failure semantics:
// by the time prune runs, the new partition is already durable, so a prune error
// leaves a CORRECT index carrying stale extras. Failing the run would discard a
// good partition to punish a bookkeeping error.
func TestLPADetector_PruneFailureLeavesSupersetNotError(t *testing.T) {
	ctx := context.Background()
	provider := twoTriangleProvider()
	storage := NewMockCommunityStorage()

	require.NoError(t, storage.SaveCommunity(ctx, &Community{
		ID:      "ghost",
		Level:   0,
		Members: []string{"ZZZ"},
	}))

	storage.pruneErr = errors.New("kv unavailable")

	detector := NewLPADetector(provider, storage).WithMaxIterations(50)
	result, err := detector.DetectCommunities(ctx)
	require.NoError(t, err, "a prune failure must not fail a detection run that produced a valid partition")
	require.NotEmpty(t, result[0])

	ghost, err := storage.GetCommunity(ctx, "ghost")
	require.NoError(t, err)
	assert.NotNil(t, ghost, "failed prune leaves the stale entry in place (superset, not error)")

	// The new partition is fully present despite the failed prune.
	for _, c := range result[0] {
		stored, err := storage.GetCommunity(ctx, c.ID)
		require.NoError(t, err)
		require.NotNil(t, stored, "community %s from the new partition must be persisted", c.ID)
	}
}

type observingPruneStorage struct {
	*MockCommunityStorage
	calls [][]*Community
}

func (s *observingPruneStorage) Prune(ctx context.Context, keep []*Community) error {
	s.calls = append(s.calls, keep)
	return s.MockCommunityStorage.Prune(ctx, keep)
}

func TestLPADetector_CompleteCandidatesAlwaysAttemptPrune(t *testing.T) {
	t.Run("non-empty candidate", func(t *testing.T) {
		storage := &observingPruneStorage{MockCommunityStorage: NewMockCommunityStorage()}
		detector := NewLPADetector(twoTriangleProvider(), storage).WithMaxIterations(50)

		communities, err := detector.DetectCommunities(context.Background())
		require.NoError(t, err)
		require.NotEmpty(t, communities)
		require.Len(t, storage.calls, 1)
		assert.NotEmpty(t, storage.calls[0], "a complete non-empty candidate must supply its keep set")
	})

	t.Run("empty candidate", func(t *testing.T) {
		storage := &observingPruneStorage{MockCommunityStorage: NewMockCommunityStorage()}
		detector := NewLPADetector(NewMockProvider(), storage)

		communities, err := detector.DetectCommunities(context.Background())
		require.NoError(t, err)
		assert.Empty(t, communities)
		require.Len(t, storage.calls, 1)
		assert.Nil(t, storage.calls[0], "an empty authority graph must explicitly attempt Prune(ctx, nil)")
	})
}

// TestLPADetector_EmptyGraphPrunesIndex keeps the empty-graph end state honest:
// no entities means no communities, so the index must not retain a partition for
// entities that no longer exist. (This is the end state the old up-front Clear
// produced; write-then-prune preserves it.)
func TestLPADetector_EmptyGraphPrunesIndex(t *testing.T) {
	ctx := context.Background()
	storage := NewMockCommunityStorage()

	require.NoError(t, storage.SaveCommunity(ctx, &Community{
		ID:      "ghost",
		Level:   0,
		Members: []string{"ZZZ"},
	}))

	detector := NewLPADetector(NewMockProvider(), storage)
	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	assert.Empty(t, communities[0])

	remaining, err := storage.GetAllCommunities(ctx)
	require.NoError(t, err)
	assert.Empty(t, remaining, "an empty graph must leave an empty index")
}

// TestLPADetector_ProviderErrorLeavesPriorPartitionIntact covers the other half of
// the no-clear change: a detection run that fails before producing a partition
// must leave the previous one queryable rather than having already wiped it.
func TestLPADetector_ProviderErrorLeavesPriorPartitionIntact(t *testing.T) {
	ctx := context.Background()
	storage := NewMockCommunityStorage()

	seedDetector := NewLPADetector(twoTriangleProvider(), storage).WithMaxIterations(50)
	_, err := seedDetector.DetectCommunities(ctx)
	require.NoError(t, err)

	before, err := storage.GetAllCommunities(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, before)

	failing := &FailingMockProvider{failOn: "GetAllEntityIDs", err: errors.New("connection lost")}
	detector := NewLPADetector(failing, storage)
	_, err = detector.DetectCommunities(ctx)
	require.Error(t, err)

	after, err := storage.GetAllCommunities(ctx)
	require.NoError(t, err)
	assert.Len(t, after, len(before),
		"a failed detection run must not leave the index emptier than it found it")
}
