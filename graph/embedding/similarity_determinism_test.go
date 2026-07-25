package embedding

import (
	"math/rand"
	"sort"
	"testing"
)

// TestScoredEntityLess_DeterministicTopKUnderPermutation is the unit-level
// regression for the mutual-kNN nondeterminism (Epic B B2, P1 #3): when MORE
// than k candidates share the SAME similarity, the subset kept after sorting and
// truncating to k must not depend on input enumeration order. This exercises the
// exact comparator both the warm cache path and the cold KV-scan path sort
// through, so a single assertion covers the determinism mechanism for both.
//
// Before the fix (sort by similarity only) an equal-similarity group straddling
// the k boundary would be truncated in whatever order the non-stable sort left
// the permuted input, so permuting the input produced different top-k subsets.
func TestScoredEntityLess_DeterministicTopKUnderPermutation(t *testing.T) {
	const (
		n = 20
		k = 8
	)
	// n candidates ALL at the same similarity: the tie-break is the only thing
	// that can decide which k survive the truncation.
	base := make([]ScoredEntity, 0, n)
	ids := []string{
		"o.p.d.s.t.e01", "o.p.d.s.t.e02", "o.p.d.s.t.e03", "o.p.d.s.t.e04",
		"o.p.d.s.t.e05", "o.p.d.s.t.e06", "o.p.d.s.t.e07", "o.p.d.s.t.e08",
		"o.p.d.s.t.e09", "o.p.d.s.t.e10", "o.p.d.s.t.e11", "o.p.d.s.t.e12",
		"o.p.d.s.t.e13", "o.p.d.s.t.e14", "o.p.d.s.t.e15", "o.p.d.s.t.e16",
		"o.p.d.s.t.e17", "o.p.d.s.t.e18", "o.p.d.s.t.e19", "o.p.d.s.t.e20",
	}
	for _, id := range ids {
		base = append(base, ScoredEntity{EntityID: id, Similarity: 0.87})
	}

	// The deterministic invariant: the top-k of an all-equal group is the
	// lexicographically smallest k IDs (similarity ties break by ID ascending).
	wantIDs := append([]string(nil), ids...)
	sort.Strings(wantIDs)
	wantIDs = wantIDs[:k]

	topK := func(in []ScoredEntity) []string {
		cp := append([]ScoredEntity(nil), in...)
		sort.Slice(cp, func(i, j int) bool { return ScoredEntityLess(cp[i], cp[j]) })
		if len(cp) > k {
			cp = cp[:k]
		}
		out := make([]string, len(cp))
		for i, s := range cp {
			out[i] = s.EntityID
		}
		return out
	}

	rng := rand.New(rand.NewSource(1))
	for run := 0; run < 200; run++ {
		perm := append([]ScoredEntity(nil), base...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		got := topK(perm)
		if len(got) != k {
			t.Fatalf("run %d: got %d results, want %d", run, len(got), k)
		}
		for i := range wantIDs {
			if got[i] != wantIDs[i] {
				t.Fatalf("run %d: nondeterministic top-k at index %d: got %q want %q (full: %v)",
					run, i, got[i], wantIDs[i], got)
			}
		}
	}
}

// TestFindSimilarFromCache_DeterministicTopKAtEqualSimilarity drives the WARM
// path end-to-end: the vector cache is a map, so Go's randomized iteration order
// permutes the input to the sort on every call for free. Over many calls the
// top-k of an all-equal-similarity corpus must be identical (and equal to the
// lexicographically smallest k entity IDs).
func TestFindSimilarFromCache_DeterministicTopKAtEqualSimilarity(t *testing.T) {
	const k = 8
	// All identical vectors -> identical cosine similarity to any query, so the
	// k-boundary tie-break is the only discriminator.
	ids := []string{
		"o.p.d.s.t.e01", "o.p.d.s.t.e02", "o.p.d.s.t.e03", "o.p.d.s.t.e04",
		"o.p.d.s.t.e05", "o.p.d.s.t.e06", "o.p.d.s.t.e07", "o.p.d.s.t.e08",
		"o.p.d.s.t.e09", "o.p.d.s.t.e10", "o.p.d.s.t.e11", "o.p.d.s.t.e12",
		"o.p.d.s.t.e13", "o.p.d.s.t.e14", "o.p.d.s.t.e15", "o.p.d.s.t.e16",
		"o.p.d.s.t.e17", "o.p.d.s.t.e18", "o.p.d.s.t.e19", "o.p.d.s.t.e20",
	}
	cache := make(map[string][]float32, len(ids))
	for _, id := range ids {
		cache[id] = []float32{1, 0, 0}
	}
	s := &Storage{vectorCache: cache, cacheReady: make(chan struct{})}
	close(s.cacheReady)
	s.cacheWatchHealthy = true

	wantIDs := append([]string(nil), ids...)
	sort.Strings(wantIDs)
	wantIDs = wantIDs[:k]

	query := []float32{1, 1, 0}
	var first []string
	for run := 0; run < 100; run++ {
		scored, ok := s.FindSimilarFromCache("", query, nil, k)
		if !ok {
			t.Fatal("cache reported cold; expected warm")
		}
		if len(scored) != k {
			t.Fatalf("run %d: got %d results, want %d", run, len(scored), k)
		}
		got := make([]string, len(scored))
		for i, e := range scored {
			got[i] = e.EntityID
		}
		for i := range wantIDs {
			if got[i] != wantIDs[i] {
				t.Fatalf("run %d: warm-path top-k is nondeterministic at index %d: got %q want %q (full %v)",
					run, i, got[i], wantIDs[i], got)
			}
		}
		if first == nil {
			first = got
		}
	}
}
