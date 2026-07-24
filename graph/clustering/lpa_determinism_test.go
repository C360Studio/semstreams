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

// detectGrid runs one full detection over a freshly-built grid and returns the
// per-level community map. A fresh provider+storage per call guarantees no state
// carries between runs, so any run-to-run agreement is the algorithm being
// deterministic on the fixed edge set, not a warm index.
func detectGrid(t *testing.T, rows, cols int) map[int][]*Community {
	t.Helper()
	storage := NewMockCommunityStorage()
	detector := NewLPADetector(gridProvider(rows, cols), storage).WithMaxIterations(50)

	communities, err := detector.DetectCommunities(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, communities[0])
	return communities
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
	// at a level ⇒ its set has exactly one member.
	distinctByLevel := make(map[int]map[string]struct{}, len(levels))
	for _, level := range levels {
		distinctByLevel[level] = make(map[string]struct{})
	}

	for r := 0; r < runs; r++ {
		communities := detectGrid(t, rows, cols)
		for _, level := range levels {
			require.NotNilf(t, communities[level], "run %d: fixture must produce level %d", r, level)
			distinctByLevel[level][levelSignature(communities, level)] = struct{}{}
		}
	}

	for _, level := range levels {
		require.Lenf(t, distinctByLevel[level], 1,
			"level %d must yield exactly ONE distinct partition across %d runs, got %d — "+
				"DetectCommunities is not reproducible at this level",
			level, runs, len(distinctByLevel[level]))
	}
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
