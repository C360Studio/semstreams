package clustering

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the B2 §4/§5 activation mechanism on SemanticEdgeProvider: the
// per-cycle active toggle that makes "run structural-only this cycle" real, and
// the not-ready abort that keeps the build-once mutual-kNN cache from latching an
// empty/partial adjacency during a cold-embedding window.

// heterogeneousBase returns three structurally-heterogeneous entities (distinct
// system + type segments) so NO sibling/system-peer edges muddy a semantic test:
// any edge synthesized is purely the mutual-kNN tier.
func heterogeneousBase() *stubProvider {
	return &stubProvider{
		entities:  []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c"},
		neighbors: map[string][]string{},
	}
}

// mutualABFinder makes a<->b a mutual-kNN pair and leaves c out.
func mutualABFinder() *stubFinder {
	return &stubFinder{directed: map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"},
		"o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
	}}
}

func structuralOnlyEIDP() *EntityIDProvider {
	return NewEntityIDProvider(heterogeneousBase(), EntityIDProviderConfig{
		IncludeSiblings: false, IncludeSystemPeers: false,
	}, nil)
}

// Inactive: the provider behaves EXACTLY as the wrapped EntityIDProvider — no
// semantic edge, no cache build (the finder is never queried) — which is the
// structural-only floor of B2 §4.
func TestSemanticEdgeProvider_Inactive_BehavesAsBaseAndSkipsCache(t *testing.T) {
	ctx := context.Background()
	finder := mutualABFinder()
	prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	prov.SetActive(false)
	require.False(t, prov.IsActive())

	na, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.NotContains(t, na, "o.p.d.s2.t2.b",
		"inactive tier must NOT synthesize the mutual-kNN edge")
	assert.Empty(t, na, "structural-only neighbor set for a heterogeneous entity with no explicit edges")

	w, err := prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
	require.NoError(t, err)
	assert.InDelta(t, 0.0, w, 1e-9, "inactive tier resolves to the base cascade (no semantic weight)")

	assert.Equal(t, 0, finder.calls,
		"inactive tier must NOT trigger the build-once cache (no similarity queries)")
}

// The load-bearing transition (B2 §4 CRITICAL MECHANISM): a not-ready cycle runs
// structural-only WITHOUT building the cache, and the very next ready cycle
// actually applies semantic edges. If the build-once cache had latched during the
// not-ready window, the ready cycle would still be empty — this asserts it does
// not.
func TestSemanticEdgeProvider_NotReadyThenReady_CacheDoesNotLatch(t *testing.T) {
	ctx := context.Background()
	finder := mutualABFinder()
	prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	// Cycle 1: embeddings NOT ready -> structural-only. No edge, no cache build.
	prov.SetActive(false)
	c1, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.NotContains(t, c1, "o.p.d.s2.t2.b", "structural-only cycle synthesizes no semantic edge")
	require.Equal(t, 0, finder.calls, "the cache must not build during a not-ready cycle")

	// Cycle 2: embeddings ready -> the tier activates and the cache builds FRESH
	// from the now-ready finder. The mutual edge appears — proof the cache did not
	// latch to the empty structural-only set.
	prov.SetActive(true)
	c2, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.Contains(t, c2, "o.p.d.s2.t2.b",
		"a ready cycle after a not-ready one must apply the mutual-kNN edge (no latch)")
	assert.Greater(t, finder.calls, 0, "the ready cycle builds the cache")

	w, err := prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
	require.NoError(t, err)
	assert.InDelta(t, 0.9, w, 1e-9, "the reactivated cycle resolves the semantic weight")
}

// notReadyFinder returns the not-ready sentinel until unblocked, then good
// directed results. It models the embedding index going cold mid-build (a race
// past the §4 gate) and later recovering.
type notReadyFinder struct {
	directed map[string][]string
	blocked  bool
	calls    int
}

func (f *notReadyFinder) SimilarNeighbors(_ context.Context, id string, _ float64, limit int) ([]string, error) {
	f.calls++
	if f.blocked {
		return nil, fmt.Errorf("%w: embedding index still validating", ErrSemanticIndexNotReady)
	}
	out := f.directed[id]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// A not-ready error mid-build ABORTS the whole cache build without latching:
// GetNeighbors degrades to structural-only (no error), the cache stays
// uninitialized, and a later recovered cycle builds it fresh and applies edges.
func TestSemanticEdgeProvider_NotReadyMidBuild_AbortsWithoutLatch(t *testing.T) {
	ctx := context.Background()
	finder := &notReadyFinder{
		directed: map[string][]string{
			"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"},
			"o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		},
		blocked: true,
	}
	prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
	// Active, but the finder reports not-ready: the build must abort, not latch.
	require.True(t, prov.IsActive())

	na, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err, "a not-ready abort degrades to structural-only, it does not error the traversal")
	assert.NotContains(t, na, "o.p.d.s2.t2.b", "no semantic edge while the index is cold")
	require.False(t, prov.cacheInitialized.Load(),
		"a not-ready abort must NOT mark the cache initialized (no latch)")

	// GetEdgeWeight also degrades to the structural resolution rather than erroring
	// (LPA would otherwise use an unweighted 1.0 for an errored edge).
	w, err := prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
	require.NoError(t, err)
	assert.InDelta(t, 0.0, w, 1e-9, "structural-only resolution during the cold window")

	// Recover the index MID-cycle, then look up again WITHOUT a cycle boundary: the
	// per-cycle abort latch (Codex P1#2) keeps this call structural-only, so the
	// partition stays consistent (no early-structural/late-semantic split) and the
	// O(N) sweep is not re-issued. The next lookup does not re-query the finder.
	finder.blocked = false
	callsAfterAbort := finder.calls
	sameCycle, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.NotContains(t, sameCycle, "o.p.d.s2.t2.b",
		"a mid-cycle recovery must NOT flip this cycle to semantic — the abort latch holds it structural-only")
	assert.Equal(t, callsAfterAbort, finder.calls,
		"the aborted cycle must not re-issue the O(N) similarity sweep on the next lookup")
	require.False(t, prov.AppliedSemanticEdges(),
		"an aborted cycle reports structural-only as its ACTUAL mode, even though active")

	// A NEW cycle boundary (SetActive, as the component calls each tick) clears the
	// latch: the recovered index now builds fresh and applies edges, proving nothing
	// latched permanently during the cold window (the no-latch guarantee).
	prov.SetActive(true)
	na2, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.Contains(t, na2, "o.p.d.s2.t2.b", "the recovered cycle builds fresh and applies the edge")
	require.True(t, prov.cacheInitialized.Load(), "the recovered build initializes the cache")
	require.True(t, prov.AppliedSemanticEdges(), "the rebuilt cycle reports semantic-applied as its actual mode")
}

// TestSemanticEdgeProvider_ColdCycle_SweepsAtMostOnce is the endpoint-saturation
// guard (Codex P1#2b): across a whole detection cycle in which the embedding index
// stays cold, MANY entity lookups must issue AT MOST ONE aborted similarity sweep, not
// one per lookup. Before the per-cycle abort latch, each GetNeighbors/GetEdgeWeight
// re-entered ensureCache and re-issued the O(N) build, hammering the similarity
// endpoint while the index was down.
func TestSemanticEdgeProvider_ColdCycle_SweepsAtMostOnce(t *testing.T) {
	ctx := context.Background()
	finder := &notReadyFinder{
		directed: map[string][]string{
			"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"},
			"o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		},
		blocked: true, // index cold for the whole cycle
	}
	prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
	require.True(t, prov.IsActive())

	// Simulate a cycle's worth of provider reads: many neighbor + weight lookups.
	for _, id := range []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c"} {
		_, err := prov.GetNeighbors(ctx, id, "both")
		require.NoError(t, err)
	}
	for i := 0; i < 5; i++ {
		_, err := prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
		require.NoError(t, err)
	}

	assert.LessOrEqual(t, finder.calls, 1,
		"a cold cycle must issue at most one aborted sweep across all lookups (no per-lookup re-sweep)")
	assert.False(t, prov.AppliedSemanticEdges(),
		"an active-but-aborted cycle's ACTUAL mode is structural-only")

	// The next cycle boundary clears the latch and allows one fresh attempt.
	prov.SetActive(true)
	_, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.Greater(t, finder.calls, 1, "a new cycle is allowed exactly one fresh sweep attempt")
}

// TestSemanticEdgeProvider_AppliedSemanticEdges_IntentVsActual pins the Codex P1#2b
// distinction directly: AppliedSemanticEdges is the ACTUAL completed mode, not the
// SetActive intent. An active cycle with a healthy finder reports applied; an active
// cycle that aborts reports NOT applied; an inactive cycle reports NOT applied.
func TestSemanticEdgeProvider_AppliedSemanticEdges_IntentVsActual(t *testing.T) {
	ctx := context.Background()

	t.Run("active + healthy build -> applied (even with zero mutual pairs)", func(t *testing.T) {
		// A finder with no mutual pairs still means "semantics ran, found nothing".
		prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), &stubFinder{directed: map[string][]string{}},
			WeightConfig{SemanticWeight: 0.9}, SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
		prov.SetActive(true)
		_, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
		require.NoError(t, err)
		assert.True(t, prov.AppliedSemanticEdges(),
			"a completed build with no mutual pairs is still applied=true (the #618 'ran, found nothing')")
	})

	t.Run("active + aborted build -> NOT applied (intent was active)", func(t *testing.T) {
		prov := NewSemanticEdgeProvider(structuralOnlyEIDP(),
			&notReadyFinder{directed: map[string][]string{}, blocked: true},
			WeightConfig{SemanticWeight: 0.9}, SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
		prov.SetActive(true)
		_, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
		require.NoError(t, err)
		assert.False(t, prov.AppliedSemanticEdges(),
			"an intended-active cycle that aborted reports its actual structural-only mode")
	})

	t.Run("inactive -> NOT applied", func(t *testing.T) {
		prov := NewSemanticEdgeProvider(structuralOnlyEIDP(), mutualABFinder(),
			WeightConfig{SemanticWeight: 0.9}, SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
		prov.SetActive(false)
		assert.False(t, prov.AppliedSemanticEdges())
	})
}

// §4.3: a structural-only cycle (inactive tier) yields a COMPLETE, valid
// partition — identical to the one the bare EntityIDProvider produces, with every
// entity assigned. The provider only ever changes the EDGE SET; DetectCommunities'
// pruneToPartition and write-then-prune rebuild are indifferent to which provider
// sits at the top, so deactivating the semantic tier degrades cleanly to the full
// Tier-0/1 structural partition rather than a partial one.
func TestSemanticEdgeProvider_InactiveCycle_IsCompleteStructuralPartition(t *testing.T) {
	ctx := context.Background()

	// Two explicit triangles, no bridge -> two structural communities. The finder
	// would bridge them (a3<->b1) if the tier were ACTIVE; inactive, it must not.
	buildGraph := func() *MockProvider {
		g := NewMockProvider()
		for _, id := range []string{"o.p.d.sa.t.a1", "o.p.d.sa.t.a2", "o.p.d.sa.t.a3",
			"o.p.d.sb.t.b1", "o.p.d.sb.t.b2", "o.p.d.sb.t.b3"} {
			g.AddEntity(id)
		}
		g.AddEdge("o.p.d.sa.t.a1", "o.p.d.sa.t.a2", 1.0)
		g.AddEdge("o.p.d.sa.t.a2", "o.p.d.sa.t.a3", 1.0)
		g.AddEdge("o.p.d.sa.t.a3", "o.p.d.sa.t.a1", 1.0)
		g.AddEdge("o.p.d.sb.t.b1", "o.p.d.sb.t.b2", 1.0)
		g.AddEdge("o.p.d.sb.t.b2", "o.p.d.sb.t.b3", 1.0)
		g.AddEdge("o.p.d.sb.t.b3", "o.p.d.sb.t.b1", 1.0)
		return g
	}
	// Sibling/system-peer synthesis OFF so the structural baseline is the explicit
	// topology alone — the semantic tier is the only variable under test.
	eidpCfg := EntityIDProviderConfig{IncludeSiblings: false, IncludeSystemPeers: false}
	crossFinder := &stubFinder{directed: map[string][]string{
		"o.p.d.sa.t.a3": {"o.p.d.sb.t.b1"},
		"o.p.d.sb.t.b1": {"o.p.d.sa.t.a3"},
	}}

	// Bare EntityIDProvider partition (today's structural-only shape).
	bareEIDP := NewEntityIDProvider(buildGraph(), eidpCfg, nil)
	bare, err := NewLPADetector(bareEIDP, NewMockCommunityStorage()).
		WithMaxIterations(50).WithLevels(3).DetectCommunities(ctx)
	require.NoError(t, err)

	// Inactive SemanticEdgeProvider over an identical graph.
	inactiveSEP := NewSemanticEdgeProvider(
		NewEntityIDProvider(buildGraph(), eidpCfg, nil), crossFinder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
	inactiveSEP.SetActive(false)
	structuralOnly, err := NewLPADetector(inactiveSEP, NewMockCommunityStorage()).
		WithMaxIterations(50).WithLevels(3).DetectCommunities(ctx)
	require.NoError(t, err)

	// Complete coverage: every entity assigned to exactly one level-0 community.
	assigned := map[string]int{}
	for _, comm := range structuralOnly[0] {
		for _, m := range comm.Members {
			assigned[m]++
		}
	}
	assert.Len(t, assigned, 6, "a structural-only cycle assigns every entity (complete partition)")
	for id, n := range assigned {
		assert.Equal(t, 1, n, "entity %s must belong to exactly one community", id)
	}

	// And it is IDENTICAL to the bare structural partition — the inactive tier added
	// nothing, so the cross-community mutual pair did not merge the triangles.
	assert.Equal(t, partitionSignature(bare[0]), partitionSignature(structuralOnly[0]),
		"structural-only partition must equal the bare EntityIDProvider partition")
	assert.Len(t, structuralOnly[0], 2,
		"the two triangles stay separate when the bridging semantic edge is suppressed")
	assert.Zero(t, crossFinder.calls, "an inactive tier issues no similarity queries")
}

// partitionSignature maps each entity to its community's sorted member key, so two
// partitions compare equal iff they group entities identically (community IDs are
// not compared — only the grouping).
func partitionSignature(level0 []*Community) map[string]string {
	sig := map[string]string{}
	for _, comm := range level0 {
		members := append([]string(nil), comm.Members...)
		sort.Strings(members)
		key := strings.Join(members, "|")
		for _, m := range comm.Members {
			sig[m] = key
		}
	}
	return sig
}
