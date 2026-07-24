package clustering

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles -----------------------------------------------------------

// stubProvider is an explicit-only base provider (the kvProvider stand-in).
type stubProvider struct {
	entities  []string
	neighbors map[string][]string
	weights   map[string]float64 // key: "from->to"
}

func (s *stubProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	return s.entities, nil
}
func (s *stubProvider) GetNeighbors(_ context.Context, id, _ string) ([]string, error) {
	return s.neighbors[id], nil
}
func (s *stubProvider) GetEdgeWeight(_ context.Context, from, to string) (float64, error) {
	if w, ok := s.weights[from+"->"+to]; ok {
		return w, nil
	}
	return 0.0, nil
}

// stubFinder returns a fixed directed similarity result per entity. The map is
// interpreted as "entity -> its directed top-k neighbor list"; the mutual-kNN
// intersection is what the provider under test computes from these directed
// sets.
type stubFinder struct {
	directed map[string][]string
	calls    int
}

func (f *stubFinder) SimilarNeighbors(_ context.Context, id string, _ float64, limit int) ([]string, error) {
	f.calls++
	out := f.directed[id]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- §2: the resolved WeightConfig (the crux) -------------------------------

func TestWeightConfig_Resolve(t *testing.T) {
	w := WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9}

	tests := []struct {
		name  string
		tiers qualifyingTiers
		want  float64
	}{
		{
			name:  "no tiers qualify -> zero",
			tiers: qualifyingTiers{},
			want:  0.0,
		},
		{
			name:  "explicit strictly dominant over everything",
			tiers: qualifyingTiers{explicitWeight: 1.0, sibling: true, systemPeer: true, semantic: true},
			want:  1.0,
		},
		{
			name:  "explicit dominant even when its weight is below a virtual tier",
			tiers: qualifyingTiers{explicitWeight: 0.5, semantic: true},
			want:  0.5, // explicit returned outright, NOT max(0.5, 0.9)
		},
		{
			name:  "sibling only",
			tiers: qualifyingTiers{sibling: true},
			want:  0.7,
		},
		{
			name:  "system-peer only",
			tiers: qualifyingTiers{systemPeer: true},
			want:  0.2,
		},
		{
			name:  "semantic only",
			tiers: qualifyingTiers{semantic: true},
			want:  0.9,
		},
		{
			name:  "sibling AND semantic -> max, never the sum",
			tiers: qualifyingTiers{sibling: true, semantic: true},
			want:  0.9, // max(0.7, 0.9); the sum 1.6 must never appear
		},
		{
			name:  "sibling AND system-peer -> max",
			tiers: qualifyingTiers{sibling: true, systemPeer: true},
			want:  0.7, // max(0.7, 0.2)
		},
		{
			name:  "all three virtual tiers -> max",
			tiers: qualifyingTiers{sibling: true, systemPeer: true, semantic: true},
			want:  0.9, // max(0.7, 0.2, 0.9); sum 1.8 must never appear
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := w.resolve(tc.tiers)
			assert.InDelta(t, tc.want, got, 1e-9,
				"resolved weight must be explicit-dominant then max-not-sum")
		})
	}
}

// A dual-qualifying pair must never resolve to a sum for ANY weight assignment.
func TestWeightConfig_Resolve_NeverSums(t *testing.T) {
	w := WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9}
	// sibling + system-peer + semantic all qualify: the sum would be 1.8.
	got := w.resolve(qualifyingTiers{sibling: true, systemPeer: true, semantic: true})
	assert.Less(t, got, 0.7+0.2+0.9, "must not be the sum of the tiers")
	assert.InDelta(t, 0.9, got, 1e-9, "must be the max of the tiers")
}

// --- §1.3: mutual-kNN membership --------------------------------------------

func TestSemanticEdgeProvider_MutualKNN_DirectionalNonMatchDoesNotSynthesize(t *testing.T) {
	ctx := context.Background()
	// A, B, C are structurally heterogeneous (different type prefixes) so no
	// sibling/system-peer edges muddy the semantic test.
	base := &stubProvider{
		entities:  []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c"},
		neighbors: map[string][]string{},
	}
	eidp := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings: false, IncludeSystemPeers: false,
	}, nil)

	// Directed similarity:
	//   a -> {b}      (a considers b similar)
	//   b -> {a, c}   (b considers a AND c similar)
	//   c -> {}       (c considers nobody similar)
	// Mutual pairs: a<->b (both directions). a-c NOT (c has no set).
	// b-c is one-directional (b->c but c-/->b) so NOT synthesized.
	finder := &stubFinder{directed: map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"},
		"o.p.d.s2.t2.b": {"o.p.d.s1.t1.a", "o.p.d.s3.t3.c"},
		"o.p.d.s3.t3.c": {},
	}}

	prov := NewSemanticEdgeProvider(eidp, finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	// a<->b is mutual: edge synthesized, weight is the semantic weight.
	na, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	assert.Contains(t, na, "o.p.d.s2.t2.b", "mutual pair a<->b synthesizes an edge")
	wab, err := prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
	require.NoError(t, err)
	assert.InDelta(t, 0.9, wab, 1e-9)

	// b->c is one-directional: NO edge. c must not list b, weight is 0.
	nc, err := prov.GetNeighbors(ctx, "o.p.d.s3.t3.c", "both")
	require.NoError(t, err)
	assert.NotContains(t, nc, "o.p.d.s2.t2.b", "one-directional match must NOT synthesize an edge")
	wbc, err := prov.GetEdgeWeight(ctx, "o.p.d.s2.t2.b", "o.p.d.s3.t3.c")
	require.NoError(t, err)
	assert.InDelta(t, 0.0, wbc, 1e-9, "one-directional pair has no semantic edge")
}

// A pair that is BOTH a mutual-kNN match and a sibling resolves to the max.
func TestSemanticEdgeProvider_DualQualifying_ResolvesToMax(t *testing.T) {
	ctx := context.Background()
	// a1 and a2 share the 5-part prefix o.p.d.s.t -> siblings (weight 0.7).
	base := &stubProvider{
		entities:  []string{"o.p.d.s.t.a1", "o.p.d.s.t.a2"},
		neighbors: map[string][]string{},
	}
	eidp := NewEntityIDProvider(base, EntityIDProviderConfig{
		IncludeSiblings: true, IncludeSystemPeers: true,
		SiblingWeight: 0.7, MaxSiblings: 5,
	}, nil)
	// They are also a mutual-kNN semantic pair (weight 0.9).
	finder := &stubFinder{directed: map[string][]string{
		"o.p.d.s.t.a1": {"o.p.d.s.t.a2"},
		"o.p.d.s.t.a2": {"o.p.d.s.t.a1"},
	}}
	prov := NewSemanticEdgeProvider(eidp, finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	w, err := prov.GetEdgeWeight(ctx, "o.p.d.s.t.a1", "o.p.d.s.t.a2")
	require.NoError(t, err)
	assert.InDelta(t, 0.9, w, 1e-9, "sibling+semantic resolves to max(0.7,0.9), not the sum 1.6")
}

// Explicit edges dominate even a dual-qualifying virtual pair.
func TestSemanticEdgeProvider_ExplicitDominates(t *testing.T) {
	ctx := context.Background()
	base := &stubProvider{
		entities:  []string{"o.p.d.s.t.a1", "o.p.d.s.t.a2"},
		neighbors: map[string][]string{"o.p.d.s.t.a1": {"o.p.d.s.t.a2"}},
		weights:   map[string]float64{"o.p.d.s.t.a1->o.p.d.s.t.a2": 1.0},
	}
	eidp := NewEntityIDProvider(base, DefaultEntityIDProviderConfig(), nil)
	finder := &stubFinder{directed: map[string][]string{
		"o.p.d.s.t.a1": {"o.p.d.s.t.a2"},
		"o.p.d.s.t.a2": {"o.p.d.s.t.a1"},
	}}
	prov := NewSemanticEdgeProvider(eidp, finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	w, err := prov.GetEdgeWeight(ctx, "o.p.d.s.t.a1", "o.p.d.s.t.a2")
	require.NoError(t, err)
	assert.InDelta(t, 1.0, w, 1e-9, "explicit edge is strictly dominant over every virtual tier")
}

// GetAllEntityIDs delegates unchanged.
func TestSemanticEdgeProvider_GetAllEntityIDs_Delegates(t *testing.T) {
	ctx := context.Background()
	base := &stubProvider{entities: []string{"o.p.d.s.t.a1", "o.p.d.s.t.a2"}}
	eidp := NewEntityIDProvider(base, DefaultEntityIDProviderConfig(), nil)
	prov := NewSemanticEdgeProvider(eidp, &stubFinder{}, WeightConfig{}, SemanticEdgeParams{}, nil)
	ids, err := prov.GetAllEntityIDs(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"o.p.d.s.t.a1", "o.p.d.s.t.a2"}, ids)
}

// --- §1.4 / §2.3: default-off equivalence -----------------------------------
//
// With the feature OFF the chain is EntityIDProvider alone. This test proves the
// SemanticEdgeProvider, wrapping the SAME EntityIDProvider, produces the exact
// same neighbor set and weights when its finder yields no mutual edges — the
// semantic tier is purely additive, so an empty semantic contribution is
// indistinguishable from not being in the chain at all.
func TestSemanticEdgeProvider_NoMutualEdges_MatchesEntityIDProvider(t *testing.T) {
	ctx := context.Background()
	// Two sibling entities + one system-peer, so the EntityIDProvider actually
	// synthesizes virtual edges we must reproduce exactly.
	base := &stubProvider{
		entities: []string{
			"o.p.d.sys.t.a1", "o.p.d.sys.t.a2", // siblings (share o.p.d.sys.t)
			"o.p.d.sys.u.b1", // system-peer (shares system "sys"), different type
		},
		neighbors: map[string][]string{},
	}
	cfg := DefaultEntityIDProviderConfig()
	eidp := NewEntityIDProvider(base, cfg, nil)

	// Finder yields NO similarities anywhere -> no mutual edges at all.
	emptyFinder := &stubFinder{directed: map[string][]string{}}
	prov := NewSemanticEdgeProvider(eidp, emptyFinder,
		WeightConfig{SiblingWeight: cfg.SiblingWeight, SystemPeerWeight: cfg.SystemPeerWeight, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)

	for _, id := range base.entities {
		wantN, err := eidp.GetNeighbors(ctx, id, "both")
		require.NoError(t, err)
		gotN, err := prov.GetNeighbors(ctx, id, "both")
		require.NoError(t, err)
		assert.ElementsMatch(t, wantN, gotN,
			"with no mutual edges, decorator neighbor set equals EntityIDProvider's for %s", id)
	}

	// Weights match for a sibling pair and a system-peer pair.
	wantSib, err := eidp.GetEdgeWeight(ctx, "o.p.d.sys.t.a1", "o.p.d.sys.t.a2")
	require.NoError(t, err)
	gotSib, err := prov.GetEdgeWeight(ctx, "o.p.d.sys.t.a1", "o.p.d.sys.t.a2")
	require.NoError(t, err)
	assert.InDelta(t, wantSib, gotSib, 1e-9, "sibling weight preserved")

	wantPeer, err := eidp.GetEdgeWeight(ctx, "o.p.d.sys.t.a1", "o.p.d.sys.u.b1")
	require.NoError(t, err)
	gotPeer, err := prov.GetEdgeWeight(ctx, "o.p.d.sys.t.a1", "o.p.d.sys.u.b1")
	require.NoError(t, err)
	assert.InDelta(t, wantPeer, gotPeer, 1e-9, "system-peer weight preserved")
}

// Empty entity IDs are rejected on both query surfaces.
func TestSemanticEdgeProvider_RejectsEmptyIDs(t *testing.T) {
	ctx := context.Background()
	base := &stubProvider{}
	eidp := NewEntityIDProvider(base, DefaultEntityIDProviderConfig(), nil)
	prov := NewSemanticEdgeProvider(eidp, &stubFinder{}, WeightConfig{}, SemanticEdgeParams{}, nil)

	_, err := prov.GetNeighbors(ctx, "", "both")
	require.Error(t, err)
	_, err = prov.GetEdgeWeight(ctx, "", "x")
	require.Error(t, err)
}
