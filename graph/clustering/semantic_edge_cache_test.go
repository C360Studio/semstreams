package clustering

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers B2 §7 on SemanticEdgeProvider: the per-cycle-refreshable,
// revision-aware mutual-kNN cache (§7.1), its build metrics (§7.2), and the
// coverage-threshold abort that makes the #662 transient-latch structurally
// impossible (§7.3). The unifying property under test is that no transient-
// errored result is ever the permanent state: the cache is rebuilt whenever the
// coarse embedding watermark advances or an entry is missing, so a hollow cache
// cannot persist.

// scriptedFinder is a mutual-kNN finder whose per-entity behavior is scriptable:
// a fixed directed result, or a per-entity transient (the ErrSemanticQueryTransient
// the readiness adapter surfaces for a timeout / no-responders on one query). It
// counts calls per entity so a test can assert exactly which entities a refresh
// re-queried. Thread-safe for `go test -race` (the provider is a shared reader).
type scriptedFinder struct {
	mu        sync.Mutex
	directed  map[string][]string
	transient map[string]bool // entities that return ErrSemanticQueryTransient
	calls     map[string]int
	total     int
}

func newScriptedFinder(directed map[string][]string) *scriptedFinder {
	return &scriptedFinder{
		directed:  directed,
		transient: map[string]bool{},
		calls:     map[string]int{},
	}
}

func (f *scriptedFinder) SimilarNeighbors(_ context.Context, id string, _ float64, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[id]++
	f.total++
	if f.transient[id] {
		return nil, fmt.Errorf("%w: query timed out", ErrSemanticQueryTransient)
	}
	out := f.directed[id]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *scriptedFinder) setTransient(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transient = map[string]bool{}
	for _, id := range ids {
		f.transient[id] = true
	}
}

func (f *scriptedFinder) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

func (f *scriptedFinder) callsFor(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func (f *scriptedFinder) resetCounts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = map[string]int{}
	f.total = 0
}

// heteroEIDP wraps the given heterogeneous entity IDs (distinct system+type
// segments) in a structural-only EntityIDProvider, so no sibling/system-peer
// edge muddies a pure mutual-kNN test.
func heteroEIDP(ids []string) *EntityIDProvider {
	return NewEntityIDProvider(
		&stubProvider{entities: ids, neighbors: map[string][]string{}},
		EntityIDProviderConfig{IncludeSiblings: false, IncludeSystemPeers: false},
		nil,
	)
}

func newCacheTestProvider(ids []string, finder SemanticNeighborFinder) *SemanticEdgeProvider {
	return NewSemanticEdgeProvider(heteroEIDP(ids), finder,
		WeightConfig{SiblingWeight: 0.7, SystemPeerWeight: 0.2, SemanticWeight: 0.9},
		SemanticEdgeParams{K: 8, Threshold: 0.75}, nil)
}

func hasNeighbor(t *testing.T, prov *SemanticEdgeProvider, id, want string) bool {
	t.Helper()
	n, err := prov.GetNeighbors(context.Background(), id, "both")
	require.NoError(t, err)
	for _, x := range n {
		if x == want {
			return true
		}
	}
	return false
}

// --- §7.1: revision-keyed reuse -------------------------------------------------

// An unchanged coarse signal reuses the whole directed cache and issues ZERO
// FindSimilar calls; an advanced signal rebuilds. This is the bound §7.1 exists
// to provide (~0 queries/cycle on an unchanged corpus instead of ~N).
func TestSemanticEdgeProvider_RevisionReuse_UnchangedZeroQueries_ChangedRebuilds(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"}, // a<->b
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"}, // c<->d
	})
	prov := newCacheTestProvider(ids, finder)

	// Cycle 1 (revision 1): full build — one query per entity, mutual edges appear.
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "cycle 1 builds a<->b")
	assert.Equal(t, len(ids), finder.totalCalls(), "cycle 1 queries every entity once")

	// Cycle 2 (revision UNCHANGED): reuse — the mutual edge is still there and NOT
	// a single new similarity query was issued.
	finder.resetCounts()
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "cycle 2 reuses a<->b")
	require.True(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"), "cycle 2 reuses c<->d")
	assert.Zero(t, finder.totalCalls(),
		"an unchanged corpus must issue ZERO FindSimilar calls (the §7.1 bound)")

	// Cycle 3 (revision ADVANCED): rebuild — the finder is queried again.
	finder.resetCounts()
	prov.BeginCycle(2, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "cycle 3 rebuilds a<->b")
	assert.Equal(t, len(ids), finder.totalCalls(), "an advanced watermark re-queries the corpus")
}

// A single build serves every GetNeighbors/GetEdgeWeight call in one detection
// cycle: the epoch latch means many calls in a run do not each re-query.
func TestSemanticEdgeProvider_OneBuildPerCycle(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
	})
	prov := newCacheTestProvider(ids, finder)

	prov.BeginCycle(1, true)
	for i := 0; i < 5; i++ {
		require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"))
	}
	assert.Equal(t, len(ids), finder.totalCalls(),
		"repeated reads in one cycle share a single build (one query per entity, not per read)")
}

// Even when the coarse signal is unchanged, an entity that has NO cache entry
// (previously errored) is re-queried next cycle — while every already-cached
// entity is reused (not re-queried). This is the per-entity dimension of §7.1
// that the coarse whole-corpus signal alone would miss.
func TestSemanticEdgeProvider_MissingEntityRequeriedNextCycle_OthersReused(t *testing.T) {
	// A single flaky entity (c) must stay BELOW the 10% coverage threshold so the
	// refresh COMMITS with c missing (rather than aborting), leaving c to be
	// re-queried next cycle while the other cached entities are reused. An
	// 11-entity corpus makes c's failure 1/11 = 0.09 < 0.10.
	ids := []string{
		"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d",
		"o.p.d.s5.t5.e", "o.p.d.s6.t6.f", "o.p.d.s7.t7.g", "o.p.d.s8.t8.h",
		"o.p.d.s9.t9.i", "o.p.d.s10.t10.j", "o.p.d.s11.t11.k",
	}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	finder.setTransient("o.p.d.s3.t3.c")
	prov := newCacheTestProvider(ids, finder)

	// Cycle 1: c is transient (below threshold) -> commit, c has no cache entry, so
	// the c<->d edge cannot form yet (d's direction is cached, c's is missing).
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "a<->b builds")
	assert.False(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"),
		"c timed out, so the c<->d edge cannot form this cycle")
	require.True(t, prov.cacheInitialized.Load(), "a below-threshold refresh COMMITS")

	// Cycle 2 (SAME revision): c recovers. Only c (the missing entity) is
	// re-queried; the already-cached entities are reused, not re-queried.
	finder.setTransient() // clear
	finder.resetCounts()
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"),
		"the missing entity is re-queried next cycle and its edge recovers")
	assert.Equal(t, 1, finder.callsFor("o.p.d.s3.t3.c"), "only the missing entity is re-queried")
	assert.Zero(t, finder.callsFor("o.p.d.s1.t1.a"), "an already-cached entity is NOT re-queried")
	assert.Equal(t, 1, finder.totalCalls(), "exactly one re-query (the previously-errored entity)")
}

// --- §7.3 / #662: the coverage-threshold abort (no hollow latch) ----------------

// A subset transient during the FIRST build, ABOVE threshold, does not latch a
// hollow cache: the cycle degrades to structural-only, and the next (recovered)
// cycle recovers the FULL edge set. This is the #662 test.
func TestSemanticEdgeProvider_SubsetTransientAboveThreshold_DoesNotLatchHollow(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	// Half the corpus times out (2/4 = 0.5 > 0.10) -> abort, no commit.
	finder.setTransient("o.p.d.s1.t1.a", "o.p.d.s3.t3.c")
	prov := newCacheTestProvider(ids, finder)

	// Cycle 1: above-threshold transient, no prior cache -> structural-only, and
	// crucially NOT a hollow cache: nothing is committed.
	prov.BeginCycle(1, true)
	assert.False(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"),
		"an above-threshold transient build degrades to structural-only this cycle")
	require.False(t, prov.cacheInitialized.Load(),
		"an above-threshold abort must NOT commit a hollow cache (no latch)")

	// Cycle 2: the finder recovers. The full edge set appears — proof the empty
	// structural-only result of cycle 1 did not latch.
	finder.setTransient()
	prov.BeginCycle(1, true)
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "a<->b recovers")
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"), "c<->d recovers")
	require.True(t, prov.cacheInitialized.Load(), "the recovered cycle commits the full cache")
}

// A subset transient BELOW threshold commits (the good majority) rather than
// aborting, and the few missing entities recover on the next cycle — the cache
// is never hollow, and never stalls waiting for a full clean sweep.
func TestSemanticEdgeProvider_SubsetTransientBelowThreshold_CommitsAndRecovers(t *testing.T) {
	ids := []string{
		"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d",
		"o.p.d.s5.t5.e", "o.p.d.s6.t6.f", "o.p.d.s7.t7.g", "o.p.d.s8.t8.h",
		"o.p.d.s9.t9.i", "o.p.d.s10.t10.j", "o.p.d.s11.t11.k", "o.p.d.s12.t12.l",
	}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"}, // a<->b
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"}, // c<->d
	})
	finder.setTransient("o.p.d.s1.t1.a") // 1/12 = 0.08 < 0.10 -> commit
	prov := newCacheTestProvider(ids, finder)

	prov.BeginCycle(1, true)
	// The first read triggers the lazy build. a timed out, so a<->b cannot form
	// this cycle; c<->d (both healthy) does.
	assert.False(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "a timed out, edge deferred")
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"), "healthy pair commits")
	require.True(t, prov.cacheInitialized.Load(), "a below-threshold refresh commits the majority")

	// Next cycle: a recovers -> a<->b forms, without a full re-query.
	finder.setTransient()
	finder.resetCounts()
	prov.BeginCycle(1, true)
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "a<->b recovers next cycle")
	assert.Equal(t, 1, finder.totalCalls(), "only the previously-errored entity is re-queried")
}

// Above-threshold errors keep the PRIOR GOOD cache rather than discarding it: a
// burst of query timeouts serves the last-good semantic edges (abort-not-latch)
// and retries next cycle.
func TestSemanticEdgeProvider_AboveThreshold_KeepsPriorGoodCache(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	prov := newCacheTestProvider(ids, finder)

	// Cycle 1 (healthy): build a prior good cache.
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "prior good cache built")

	// Cycle 2 (revision advanced -> forces a full re-query; mass transient): the
	// refresh aborts on the coverage threshold and KEEPS the prior cache, so the
	// edge is still served rather than vanishing into a hollow set.
	finder.setTransient("o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c") // 3/4 > 0.10
	prov.BeginCycle(2, true)
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"),
		"an above-threshold abort keeps the prior good cache (abort-not-latch)")
	require.True(t, prov.cacheInitialized.Load(), "the prior cache remains committed")

	// Cycle 3 (recovered): the refresh finally rebuilds at the new revision.
	finder.setTransient()
	prov.BeginCycle(2, true)
	assert.True(t, hasNeighbor(t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"), "recovered rebuild")
}

// --- §7.3 seam: AppliedSemanticEdges reports the ACTUAL served mode across the
// coverage-threshold abort variants (Codex P1#2 actual-mode reporting extended to §7.3).
// Two coverage cases the abortedThisCycle latch alone did NOT cover: a coverage abort
// with no prior cache degraded to structural-only (applied=FALSE), and one that kept a
// non-empty prior cache still serves semantic edges (applied=TRUE). The not-ready-abort
// (FALSE) and healthy-zero-mutual (TRUE, #618 "ran, found nothing") cases stay pinned by
// semantic_edge_activation_test.go's AppliedSemanticEdges_IntentVsActual.

// A coverage-threshold abort with NO prior cache degraded the cycle to structural-only,
// so AppliedSemanticEdges must report FALSE — even though the tier is active and no
// producer-wide not-ready/fatal signal fired. This is the seam a bare
// active-and-not-aborted check otherwise left reporting a misleading applied=1.
func TestSemanticEdgeProvider_AppliedSemanticEdges_CoverageAbortNoPrior_ReportsFalse(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	finder.setTransient("o.p.d.s1.t1.a", "o.p.d.s3.t3.c") // 2/4 = 0.5 > 0.10 -> abort, no prior
	prov := newCacheTestProvider(ids, finder)
	require.True(t, prov.IsActive(), "default-active provider (no SetActive gate in this unit test)")

	prov.BeginCycle(1, true)
	_, err := prov.GetNeighbors(context.Background(), "o.p.d.s1.t1.a", "both")
	require.NoError(t, err, "a coverage abort degrades to structural-only, it does not error")
	require.False(t, prov.cacheInitialized.Load(), "an above-threshold abort with no prior commits nothing")
	assert.False(t, prov.AppliedSemanticEdges(),
		"a coverage abort with no prior cache served structural-only; applied must be FALSE (§7.3 seam)")

	// Recovery: the next cycle commits, so the degrade latch clears and applied is TRUE —
	// the degrade is per-cycle, never a permanent misreport.
	finder.setTransient()
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"), "recovered cycle commits")
	assert.True(t, prov.AppliedSemanticEdges(),
		"a recovered committing cycle clears the degrade latch (applied TRUE)")
}

// A coverage-threshold abort that KEEPS a non-empty prior cache is still serving real
// (prior-epoch) semantic edges in the vote, so AppliedSemanticEdges must report TRUE —
// distinct from the no-prior degrade above.
func TestSemanticEdgeProvider_AppliedSemanticEdges_CoverageAbortKeepsPrior_ReportsTrue(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	prov := newCacheTestProvider(ids, finder)

	// Cycle 1 (healthy): build a prior good cache -> applied TRUE.
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"))
	require.True(t, prov.AppliedSemanticEdges(), "a committed build is applied")

	// Cycle 2 (revision advanced -> full re-query; mass transient): the coverage abort
	// KEEPS the prior cache, still serving its edges, so applied stays TRUE.
	finder.setTransient("o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c") // 3/4 > 0.10
	prov.BeginCycle(2, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"),
		"the prior good cache is still served through the coverage abort")
	assert.True(t, prov.AppliedSemanticEdges(),
		"a coverage abort that keeps a non-empty prior cache still serves semantic edges (applied TRUE)")
}

// A SINGLE persistently-flaky entity must not livelock the refresh: it is below
// threshold every cycle, so the cache COMMITS every cycle and only that one
// entity is re-queried (never a permanent full rebuild loop).
func TestSemanticEdgeProvider_SingleFlakyEntity_CommitsNoLivelock(t *testing.T) {
	ids := make([]string, 0, 20)
	directed := map[string][]string{}
	for i := 1; i <= 20; i++ {
		ids = append(ids, fmt.Sprintf("o.p.d.s%d.t%d.e%d", i, i, i))
	}
	// e1<->e2 is a mutual pair; e3 is the persistently-flaky entity (no edges of
	// its own to lose, so its flakiness is the only variable).
	directed[ids[0]] = []string{ids[1]}
	directed[ids[1]] = []string{ids[0]}
	finder := newScriptedFinder(directed)
	finder.setTransient(ids[2]) // e3 always times out; 1/20 = 0.05 < 0.10
	prov := newCacheTestProvider(ids, finder)

	for cycle := 1; cycle <= 3; cycle++ {
		finder.resetCounts()
		prov.BeginCycle(1, true) // SAME revision every cycle
		require.True(t, hasNeighbor(t, prov, ids[0], ids[1]),
			"the healthy pair stays committed across cycles")
		require.True(t, prov.cacheInitialized.Load(), "each cycle commits (below threshold)")
		if cycle == 1 {
			continue // cycle 1 is the full build; count re-queries from cycle 2 on
		}
		assert.Equal(t, 1, finder.totalCalls(),
			"steady state re-queries ONLY the single flaky entity, never the whole corpus (no livelock)")
		assert.Equal(t, 1, finder.callsFor(ids[2]), "the flaky entity is the one re-query")
	}
}

// --- §7.3 liveness: early-abort the instant the budget is irreversibly exceeded --

// Codex P1#1: when EVERY query times out, the refresh must STOP the instant the
// transient count irreversibly exceeds the coverage budget (floor(0.10*N)+1), NOT
// sweep the whole corpus. On the ~175-entity production target × the 30s finder
// timeout the pre-fix full sweep is ~87 min of wasted work while HOLDING the cache
// write lock and blocking detection; the early abort bounds it to floor(0.10*N)+1
// finder calls. The denominator is the whole corpus, so "irreversibly exceeded" is
// transientErrs > floor(0.10*len(ids)) and can never come back under budget.
func TestSemanticEdgeProvider_EveryQueryTimesOut_AbortsAtBudgetNotWholeCorpus(t *testing.T) {
	const n = 20 // floor(0.10*20)+1 = 3 -> abort on the 3rd transient call
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, fmt.Sprintf("o.p.d.s%d.t%d.e%d", i, i, i))
	}
	finder := newScriptedFinder(map[string][]string{})
	finder.setTransient(ids...) // ALL time out
	prov := newCacheTestProvider(ids, finder)

	prov.BeginCycle(1, true)
	_, err := prov.GetNeighbors(context.Background(), ids[0], "both")
	require.NoError(t, err, "an over-budget abort degrades to structural-only, it does not error")

	assert.Equal(t, 3, finder.totalCalls(),
		"must abort after exactly floor(0.10*N)+1 = 3 transient calls, not sweep all 20 (P1#1)")
	require.False(t, prov.cacheInitialized.Load(),
		"no prior cache -> the over-budget abort commits nothing (degraded structural-only)")
	assert.False(t, prov.AppliedSemanticEdges(),
		"a coverage abort with no prior cache served structural-only; applied FALSE")

	// Recovery: the finder clears -> the next cycle rebuilds the full corpus, proving
	// the early abort latched nothing hollow.
	finder.setTransient()
	finder.resetCounts()
	prov.BeginCycle(1, true)
	_, err = prov.GetNeighbors(context.Background(), ids[0], "both")
	require.NoError(t, err)
	require.True(t, prov.cacheInitialized.Load(), "the recovered cycle commits the full cache")
	assert.Equal(t, n, finder.totalCalls(), "recovery re-queries the whole corpus")
}

// --- §7.1/§4 P2#4: the per-cycle transition is one atomic step ------------------

// Codex P2#4: BeginCycle performs the WHOLE per-cycle transition atomically — it sets
// active, resets the abort/degrade latches, AND advances the epoch in one call (epoch
// bumped LAST). A prior cycle that aborted producer-wide (abortedThisCycle latched)
// must be reset by the NEXT BeginCycle ALONE (no separate SetActive), proving the
// transition is combined rather than split across two calls a torn read could observe
// out of order. (notReadyFinder is defined in semantic_edge_activation_test.go.)
func TestSemanticEdgeProvider_BeginCycle_AtomicallyResetsLatchesAndActivates(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b"}
	prov := newCacheTestProvider(ids, &notReadyFinder{directed: map[string][]string{}, blocked: true})

	prov.BeginCycle(1, true)
	_, err := prov.GetNeighbors(context.Background(), ids[0], "both")
	require.NoError(t, err, "a not-ready abort degrades to structural-only, it does not error")
	require.True(t, prov.abortedThisCycle.Load(), "the producer-wide not-ready abort latched")
	require.False(t, prov.AppliedSemanticEdges(), "an aborted cycle reports applied FALSE")

	// The NEXT BeginCycle ALONE must reset both per-cycle latches AND set active, in the
	// same atomic transition — no separate SetActive call.
	prov.BeginCycle(2, true)
	assert.False(t, prov.abortedThisCycle.Load(),
		"BeginCycle must reset the per-cycle abort latch in the same atomic transition (P2#4)")
	assert.False(t, prov.degradedThisCycle.Load(), "BeginCycle must reset the degrade latch too")
	assert.True(t, prov.IsActive(), "BeginCycle must set active in the same transition")
	assert.True(t, prov.AppliedSemanticEdges(),
		"the reset cycle is active with cleared latches -> applied intent TRUE before detection")
}

// --- §7.1/§7.3: mutual-kNN symmetry survives a partial refresh ------------------

// After a partial refresh (one entity re-queried next cycle while the rest are
// reused), the recomputed mutual-kNN adjacency is still symmetric: an edge exists
// in BOTH directions or neither.
func TestSemanticEdgeProvider_SymmetryPreservedAfterPartialRefresh(t *testing.T) {
	ctx := context.Background()
	ids := []string{
		"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d",
		"o.p.d.s5.t5.e", "o.p.d.s6.t6.f", "o.p.d.s7.t7.g", "o.p.d.s8.t8.h",
		"o.p.d.s9.t9.i", "o.p.d.s10.t10.j", "o.p.d.s11.t11.k", "o.p.d.s12.t12.l",
	}
	// a<->b mutual; c<->d mutual but c times out cycle 1 (d's direction is cached).
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	finder.setTransient("o.p.d.s3.t3.c") // 1/12 < 0.10 -> commit, c missing
	prov := newCacheTestProvider(ids, finder)

	prov.BeginCycle(1, true)
	// a<->b is symmetric this cycle; c<->d is absent (c missing).
	assertSymmetric(ctx, t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b", true)
	assertSymmetric(ctx, t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d", false)

	// Cycle 2: c recovers (partial refresh — only c re-queried). The recomputed
	// intersection must make c<->d symmetric, and leave a<->b symmetric.
	finder.setTransient()
	prov.BeginCycle(1, true)
	assertSymmetric(ctx, t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b", true)
	assertSymmetric(ctx, t, prov, "o.p.d.s3.t3.c", "o.p.d.s4.t4.d", true)
}

// assertSymmetric checks that the semantic edge x-y is present in BOTH directions
// (or absent in both), via the public GetNeighbors surface.
func assertSymmetric(ctx context.Context, t *testing.T, prov *SemanticEdgeProvider, x, y string, want bool) {
	t.Helper()
	nx, err := prov.GetNeighbors(ctx, x, "both")
	require.NoError(t, err)
	ny, err := prov.GetNeighbors(ctx, y, "both")
	require.NoError(t, err)
	assert.Equal(t, want, contains(nx, y), "%s -> %s presence", x, y)
	assert.Equal(t, want, contains(ny, x), "%s -> %s presence (symmetry)", y, x)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// --- §7.2: build metrics --------------------------------------------------------

type fakeSemanticEdgeMetrics struct {
	buildObservations int
	lastBuildMs       float64
	queriesAdded      int
}

func (f *fakeSemanticEdgeMetrics) ObserveBuildMs(ms float64) {
	f.buildObservations++
	f.lastBuildMs = ms
}
func (f *fakeSemanticEdgeMetrics) AddSimilarQueries(n int) { f.queriesAdded += n }

// The refresh records its duration on every active cycle and adds its query
// count — which is 0 on a reuse cycle, the observable that proves the cache is
// bounding load (§7.2).
func TestSemanticEdgeProvider_Metrics_RecordBuildAndQueryLoad(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
	})
	m := &fakeSemanticEdgeMetrics{}
	prov := newCacheTestProvider(ids, finder).WithMetrics(m)

	// Cycle 1: full build -> duration observed, query count == len(ids).
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"))
	assert.Equal(t, 1, m.buildObservations, "a build observes its duration once")
	assert.Equal(t, len(ids), m.queriesAdded, "cycle 1 adds one query per entity")

	// Cycle 2: reuse -> duration observed again, but ZERO queries added (the bound).
	prov.BeginCycle(1, true)
	require.True(t, hasNeighbor(t, prov, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b"))
	assert.Equal(t, 2, m.buildObservations, "a reuse cycle still observes its (near-zero) duration")
	assert.Equal(t, len(ids), m.queriesAdded, "a reuse cycle adds NO queries (load bounded to 0)")
}

// --- §5.3: shared-provider concurrency (BeginCycle/SetActive writer + reader) ---

// The provider is shared between the detector loop (which drives BeginCycle /
// SetActive per cycle) and, in B3, a concurrent enhancement-worker READER. The
// per-cycle refresh adds shared state (settled/directed/cacheRevision under the
// mutex, epoch/target atomics); this exercises the writer/reader overlap so the
// race detector proves it stays clean.
func TestSemanticEdgeProvider_ConcurrentCycleAndReaders_RaceFree(t *testing.T) {
	ctx := context.Background()
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b", "o.p.d.s3.t3.c", "o.p.d.s4.t4.d"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
		"o.p.d.s3.t3.c": {"o.p.d.s4.t4.d"}, "o.p.d.s4.t4.d": {"o.p.d.s3.t3.c"},
	})
	prov := newCacheTestProvider(ids, finder)

	var wg sync.WaitGroup
	// Writer: the detector loop advancing cycles (bumping epoch + revision + toggle).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(1); i <= 200; i++ {
			// BeginCycle now sets active atomically WITH the epoch + latch reset (Codex
			// P2#4); the standalone SetActive below models an out-of-band toggle so the
			// race detector still exercises both writer paths against the readers.
			prov.BeginCycle(i%5, i%7 != 0) // revision churns, forcing both reuse and rebuild
			prov.SetActive(i%2 == 0)
		}
	}()
	// Concurrent readers: the enhancement-worker shape.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
				_, _ = prov.GetEdgeWeight(ctx, "o.p.d.s1.t1.a", "o.p.d.s2.t2.b")
			}
		}()
	}
	wg.Wait()

	// After the concurrent churn, a clean active cycle must still produce the
	// correct mutual edge — proving the shared cache state is not merely race-free
	// but left CONSISTENT (no torn/partial refresh survived the overlap).
	prov.BeginCycle(1, true)
	prov.SetActive(true)
	neighbors, err := prov.GetNeighbors(ctx, "o.p.d.s1.t1.a", "both")
	require.NoError(t, err)
	require.Contains(t, neighbors, "o.p.d.s2.t2.b")
}

// A nil metrics recorder is a no-op (direct provider unit tests attach none).
func TestSemanticEdgeProvider_Metrics_NilSafe(t *testing.T) {
	ids := []string{"o.p.d.s1.t1.a", "o.p.d.s2.t2.b"}
	finder := newScriptedFinder(map[string][]string{
		"o.p.d.s1.t1.a": {"o.p.d.s2.t2.b"}, "o.p.d.s2.t2.b": {"o.p.d.s1.t1.a"},
	})
	prov := newCacheTestProvider(ids, finder) // no WithMetrics
	prov.BeginCycle(1, true)
	require.NotPanics(t, func() {
		_, _ = prov.GetNeighbors(context.Background(), "o.p.d.s1.t1.a", "both")
	})
}
