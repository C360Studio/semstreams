package clustering

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gridProvider builds a rows x cols 4-connected grid with uniform edge weights.
// A grid is deliberately chosen as the determinism fixture because its realized
// partition depends on BOTH sources of non-determinism this change fixes:
//
//   - Tie resolution (§6.2): interior nodes have four equal-weight neighbours, so
//     early-iteration votes tie constantly.
//   - Processing ORDER (§6.1): unlike a ring (which the lexicographic tie-break
//     alone renders deterministic), a grid stays order-sensitive even with a
//     deterministic tie-break — the state a node observes when it votes depends on
//     which of its neighbours were already updated this pass. Measured: a 4x6 grid
//     with a deterministic tie-break but an UNSEEDED shuffle still partitions ~44
//     different ways over 300 runs (mode ~26%). Only a seeded shuffle pins it.
//
// So a grid partitions reproducibly only when BOTH fixes are in place; reverting
// either one alone reintroduces multiple partitions (verified by ablation during
// development). With both fixes it produces exactly one partition.
func gridProvider(rows, cols int) *MockProvider {
	p := NewMockProvider()
	id := func(r, c int) string { return fmt.Sprintf("g%02d_%02d", r, c) }
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			p.AddEntity(id(r, c))
		}
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c+1 < cols {
				p.AddEdge(id(r, c), id(r, c+1), 1.0)
			}
			if r+1 < rows {
				p.AddEdge(id(r, c), id(r+1, c), 1.0)
			}
		}
	}
	return p
}

// levelSignature reduces ONE hierarchical level to a canonical,
// identity-independent string: members within a community are sorted, then
// communities are sorted by their sorted member list, so two partitions compare
// equal iff they group the SAME entities the SAME way. Community-ID churn (a
// separate B1 concern) does not affect it — the same reduction
// validate_thematic_eval.go's level0MembershipHashes uses.
func levelSignature(communities map[int][]*Community, level int) string {
	groups := make([]string, 0, len(communities[level]))
	for _, c := range communities[level] {
		members := append([]string(nil), c.Members...)
		sort.Strings(members)
		groups = append(groups, strings.Join(members, ","))
	}
	sort.Strings(groups)
	return strings.Join(groups, "|")
}

// orderedLevelSignature is the STRICT projection of one level: it preserves the
// community SLICE order, each community's Community.ID, and its stored Members
// SEQUENCE — all exactly as returned, NOT sorted. It verifies buildCommunities'
// promise of fully-ordered, byte-stable output, which levelSignature (grouping-
// only) canonicalizes away and so cannot catch community-order, member-order, or
// seed-ID churn. Terminal level 2 is only guarded by this projection, since its
// output is not re-fed through another LPA pass where ordering would surface as a
// membership change (Codex #658 P2).
func orderedLevelSignature(communities map[int][]*Community, level int) string {
	parts := make([]string, 0, len(communities[level]))
	for _, c := range communities[level] {
		parts = append(parts, c.ID+"=>"+strings.Join(c.Members, ","))
	}
	// Community slice order preserved — deliberately NOT sorted.
	return strings.Join(parts, "|")
}

// detectPartition runs one full detection over the given provider and returns the
// per-level community map. A fresh storage per call guarantees no state carries
// between runs, so any agreement is the algorithm being deterministic, not a warm
// index.
func detectPartition(t *testing.T, provider Provider) map[int][]*Community {
	t.Helper()
	storage := NewMockCommunityStorage()
	detector := NewLPADetector(provider, storage).WithMaxIterations(50)

	communities, err := detector.DetectCommunities(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, communities[0])
	return communities
}

// detectGrid runs one full detection over a freshly-built grid.
func detectGrid(t *testing.T, rows, cols int) map[int][]*Community {
	t.Helper()
	return detectPartition(t, gridProvider(rows, cols))
}

// reversedOrderProvider wraps a MockProvider and returns GetAllEntityIDs in
// REVERSED order, leaving neighbours and weights untouched — so the ONLY thing
// that differs from the wrapped provider is the entity-iteration order the
// detector observes. It stands in for the fact that the graph.Provider contract
// does not promise a stable order (the wired kvProvider returns JetStream Keys()
// in watcher-delivery order, which varies across restarts/rebuilds).
type reversedOrderProvider struct {
	*MockProvider
}

func (r reversedOrderProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	ids, err := r.MockProvider.GetAllEntityIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), ids...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// TestLPADetector_DeterministicPartitionOnVoteTie is the §6.3 regression:
// repeated DetectCommunities runs over an identical fixed edge set that includes
// deliberate vote ties must yield identical partitions — at EVERY hierarchical
// level, not just level 0. Production GraphRAG reads levels 1 and 2 too
// (GetCommunitiesByLevel / GetEntityCommunity), so a level-0-only guarantee is not
// enough; the seeded shuffle alone left level-1/2 non-deterministic because
// buildCommunities emitted its members/community order by map iteration, which
// re-seeded the higher-level entity set randomly.
//
// The grid fixture requires BOTH determinism fixes to be reproducible even at
// level 0, so this targets both:
//   - §6.1 (per-call seeded shuffle): the grid is order-sensitive, so without the
//     seeded shuffle repeated runs diverge even under a deterministic tie-break.
//   - §6.2 (lexicographic tie-break): the grid's interior ties must resolve
//     deterministically or repeated runs diverge even with a seeded shuffle.
//
// Levels 1 and 2 additionally require buildCommunities' ordered output. Pre-fix, a
// 4x6 grid partitions dozens of different ways per run, so 40 runs reliably expose
// any residual non-determinism. The tie-break DIRECTION is pinned separately and
// surgically by TestLPADetector_ComputeNewLabel_TieBreaksLexicographically.
func TestLPADetector_DeterministicPartitionOnVoteTie(t *testing.T) {
	const rows, cols = 4, 6
	const runs = 40
	// NewLPADetector defaults to 3 hierarchical levels; assert all three.
	levels := []int{0, 1, 2}

	// Per-level set of distinct partition signatures across all runs. Determinism
	// at a level ⇒ its set has exactly one member. We track BOTH the grouping-only
	// signature (same entities grouped the same way) AND the strict ordered
	// signature (byte-stable community order + IDs + member sequence), so the test
	// verifies buildCommunities' full ordered-output contract, not just grouping
	// (Codex #658 P2). Terminal level 2 is guarded only by the ordered projection.
	distinctByLevel := make(map[int]map[string]struct{}, len(levels))
	orderedByLevel := make(map[int]map[string]struct{}, len(levels))
	for _, level := range levels {
		distinctByLevel[level] = make(map[string]struct{})
		orderedByLevel[level] = make(map[string]struct{})
	}

	for r := 0; r < runs; r++ {
		communities := detectGrid(t, rows, cols)
		for _, level := range levels {
			require.NotNilf(t, communities[level], "run %d: fixture must produce level %d", r, level)
			distinctByLevel[level][levelSignature(communities, level)] = struct{}{}
			orderedByLevel[level][orderedLevelSignature(communities, level)] = struct{}{}
		}
	}

	for _, level := range levels {
		require.Lenf(t, distinctByLevel[level], 1,
			"level %d must yield exactly ONE distinct partition across %d runs, got %d — "+
				"DetectCommunities is not reproducible at this level",
			level, runs, len(distinctByLevel[level]))
		require.Lenf(t, orderedByLevel[level], 1,
			"level %d must yield exactly ONE byte-stable ordered output (community order + IDs + "+
				"member sequence) across %d runs, got %d — buildCommunities' ordered-output promise is broken",
			level, runs, len(orderedByLevel[level]))
	}
}

// TestLPADetector_DeterministicAcrossProviderOrder guards the detector boundary
// (P1a): the graph.Provider contract does not promise a stable GetAllEntityIDs
// order, so the SAME entity set delivered in a DIFFERENT order must still yield
// the SAME partition at every level. Without the boundary sort in
// DetectCommunities the seeded shuffle applies a fixed permutation to a varying
// input and the partition flips (probe: reversing a 4x6 grid's entities moved
// level 0 from 4 communities to 1). The insertion-order mock in the repeated-run
// test cannot catch this — only a re-ordered delivery does.
func TestLPADetector_DeterministicAcrossProviderOrder(t *testing.T) {
	natural := detectPartition(t, gridProvider(4, 6))
	reversed := detectPartition(t, reversedOrderProvider{gridProvider(4, 6)})

	for _, level := range []int{0, 1, 2} {
		require.Equalf(t, levelSignature(natural, level), levelSignature(reversed, level),
			"level %d partition differs when the same entity set is delivered in reversed order — "+
				"processing order is not canonicalized at the detector boundary", level)
		// Strict: the ordered output (community order + IDs + member sequence) must
		// ALSO be identical regardless of provider order, since buildCommunities
		// canonicalizes it. This guards the ordered promise at every level incl. the
		// terminal level 2 (Codex #658 P2).
		require.Equalf(t, orderedLevelSignature(natural, level), orderedLevelSignature(reversed, level),
			"level %d ordered output (community order/IDs/member sequence) differs under reversed "+
				"provider delivery — buildCommunities' output is not order-independent", level)
	}
}

// TestLPADetector_BuildCommunities_OrderedOutputStableAcrossMapIteration is the
// direct guard on buildCommunities' ordered-output promise (Codex #658 P2). Go
// randomizes map range order per call, so repeatedly building from the SAME labels
// map exercises different iteration orders; the emitted community slice order, each
// Community.ID, and each Members sequence must be byte-identical every time — and
// equal to the sorted-canonical form, not merely self-consistent.
func TestLPADetector_BuildCommunities_OrderedOutputStableAcrossMapIteration(t *testing.T) {
	labels := map[string]string{
		"e1": "cA", "e3": "cA", "e6": "cA", "e7": "cA",
		"e2": "cB", "e5": "cB", "e8": "cB",
		"e4": "cC", "e9": "cC",
	}
	d := NewLPADetector(gridProvider(1, 1), NewMockCommunityStorage())
	sig := func() string {
		comms := d.buildCommunities(labels, 0, nil)
		parts := make([]string, 0, len(comms))
		for _, c := range comms {
			parts = append(parts, c.ID+"=>"+strings.Join(c.Members, ","))
		}
		return strings.Join(parts, "|")
	}
	const want = "cA=>e1,e3,e6,e7|cB=>e2,e5,e8|cC=>e4,e9"
	require.Equal(t, want, sig(), "buildCommunities must emit sorted-canonical ordered output")
	for i := 0; i < 40; i++ {
		require.Equalf(t, want, sig(),
			"buildCommunities ordered output must be byte-stable across map iteration orders; run %d differed", i)
	}
}

// TestEntityIDProvider_SiblingCapDeterministicAcrossBaseOrder guards P1b: the
// sibling/system-peer candidate lists are capped, so WHICH candidates survive the
// cap must not depend on the base provider's entity order (kvProvider's JetStream
// Keys() is watcher-delivery order, not sorted). 15 siblings share one type prefix
// and the query is one of them → 14 candidates for a cap of 10; delivered forward
// vs reversed, the capped set must be identical. Without the candidate sort the
// forward run keeps {s01..s10} and the reversed run keeps {s05..s14}.
func TestEntityIDProvider_SiblingCapDeterministicAcrossBaseOrder(t *testing.T) {
	const typePrefix = "c360.log.env.sensor.temp."
	ids := make([]string, 0, 15)
	for i := 0; i < 15; i++ {
		ids = append(ids, fmt.Sprintf("%ss%02d", typePrefix, i))
	}
	query := ids[0]

	cappedSiblings := func(order []string) []string {
		base := &entityIDTestProvider{
			entities:  append([]string(nil), order...),
			neighbors: map[string][]string{},
			weights:   map[string]float64{},
		}
		p := NewEntityIDProvider(base, EntityIDProviderConfig{
			IncludeSiblings:    true,
			IncludeSystemPeers: false, // isolate the sibling cap
			MaxSiblings:        10,
		}, slog.Default())
		neighbors, err := p.GetNeighbors(context.Background(), query, "both")
		require.NoError(t, err)
		sort.Strings(neighbors)
		return neighbors
	}

	reversed := append([]string(nil), ids...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	forward := cappedSiblings(ids)
	require.Len(t, forward, 10, "cap must keep exactly maxSiblings candidates")
	require.Equal(t, forward, cappedSiblings(reversed),
		"the capped sibling set must be identical regardless of base-provider entity order")
}

// orderedWeightProvider returns a fixed neighbor list for one query entity in a
// caller-specified ORDER, with per-neighbor edge weights. It exercises float
// non-associativity in the vote accumulation directly, without depending on the
// real EntityIDProvider wiring to construct the tie.
type orderedWeightProvider struct {
	query     string
	neighbors []string           // returned verbatim (order is the test variable)
	weights   map[string]float64 // weight of edge query->neighbor
}

func (p *orderedWeightProvider) GetAllEntityIDs(context.Context) ([]string, error) {
	return append([]string{p.query}, p.neighbors...), nil
}

func (p *orderedWeightProvider) GetNeighbors(_ context.Context, id, _ string) ([]string, error) {
	if id == p.query {
		return p.neighbors, nil
	}
	return nil, nil
}

func (p *orderedWeightProvider) GetEdgeWeight(_ context.Context, from, to string) (float64, error) {
	if from == p.query {
		if w, ok := p.weights[to]; ok {
			return w, nil
		}
	}
	return 0.0, nil
}

// TestLPADetector_ComputeNewLabel_TieStableUnderNeighborOrder guards the third
// determinism layer: computeNewLabel sums float edge weights in GetNeighbors
// order, the Provider contract does not promise that order (kvProvider emits a map
// range), and float addition is NON-ASSOCIATIVE. Label "a" carries weights
// {0.7,0.7,0.3,0.3} and label "b" carries {1.0,1.0}. Summed low-to-high
// (0.3+0.3+0.7+0.7) "a" totals 1.9999999999999998; summed high-to-low
// (0.7+0.7+0.3+0.3) it totals exactly 2.0 — so the winner flips with neighbor
// order unless computeNewLabel canonicalizes it. The vote must be identical
// forward vs reversed, and (canonicalized to 2.0 == b) resolve to the smaller "a".
func TestLPADetector_ComputeNewLabel_TieStableUnderNeighborOrder(t *testing.T) {
	weights := map[string]float64{
		"na1": 0.7, "na2": 0.7, "na3": 0.3, "na4": 0.3,
		"nb1": 1.0, "nb2": 1.0,
	}
	labels := map[string]string{
		"x":   "x",
		"na1": "a", "na2": "a", "na3": "a", "na4": "a",
		"nb1": "b", "nb2": "b",
	}
	forward := []string{"na1", "na2", "na3", "na4", "nb1", "nb2"}
	reversed := make([]string, len(forward))
	for i, id := range forward {
		reversed[len(forward)-1-i] = id
	}

	winner := func(order []string) string {
		provider := &orderedWeightProvider{query: "x", neighbors: order, weights: weights}
		d := NewLPADetector(provider, NewMockCommunityStorage())
		got, err := d.computeNewLabel(context.Background(), "x", labels)
		require.NoError(t, err)
		return got
	}

	fwd := winner(forward)
	rev := winner(reversed)
	require.Equalf(t, fwd, rev,
		"computeNewLabel must return the same label regardless of neighbor delivery order "+
			"(got %q forward, %q reversed) — float-weight summation order is not canonicalized", fwd, rev)
	require.Equal(t, "a", fwd,
		"canonicalized, both labels total 2.0 → the lexicographic tie-break picks the smaller label")
}

// TestLPADetector_ComputeNewLabel_TieBreaksLexicographically pins §6.2 directly and
// without relying on LPA convergence dynamics: an entity whose neighbours each
// carry a distinct label with equal weight casts an exact multi-way vote tie, and
// the winning label must be the lexicographically smallest one every time.
//
// Pre-fix (first-wins over Go's randomized map iteration) the winner is a random
// one of the tied labels, so a single call returns "a" only ~1/3 of the time;
// repeating the call defeats the per-range map randomization and this reliably
// fails against the pre-fix code (P(all "a") ≈ (1/3)^64).
func TestLPADetector_ComputeNewLabel_TieBreaksLexicographically(t *testing.T) {
	provider := NewMockProvider()
	// "x" is tied three ways between neighbour labels "z", "m", "a" (equal weight).
	for _, id := range []string{"x", "z", "m", "a"} {
		provider.AddEntity(id)
	}
	provider.AddEdge("x", "z", 1.0)
	provider.AddEdge("x", "m", 1.0)
	provider.AddEdge("x", "a", 1.0)

	detector := NewLPADetector(provider, NewMockCommunityStorage())
	labels := map[string]string{"x": "x", "z": "z", "m": "m", "a": "a"}

	// Repeat so a pre-fix pass cannot slip through on a lucky map iteration order.
	for i := 0; i < 64; i++ {
		got, err := detector.computeNewLabel(context.Background(), "x", labels)
		require.NoError(t, err)
		assert.Equalf(t, "a", got,
			"call %d: an exact vote tie must resolve to the lexicographically smallest label", i)
	}
}
