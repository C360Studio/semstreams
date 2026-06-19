package graphingest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestComponentWithCache is like createTestComponentWithMockKV but also
// installs a real in-memory entityCache so a test can pre-warm cache HITS and
// thereby drive fetchEntitiesConcurrent's "cached-first, misses-second" return
// ordering — the exact condition under which the byte-trim cursor would be
// derived from an UNSORTED slice. createTestComponentWithMockKV leaves
// entityCache nil, which masks the sort bug (all-misses come back already in
// sorted pageKeys order). That nil-cache blind spot is precisely why the
// pre-existing TestHandleQueryPrefix_SortDeterminism did not fail without the
// sort fix.
func createTestComponentWithCache(t *testing.T) *Component {
	t.Helper()
	comp := createTestComponentWithMockKV(t)
	c, err := cache.NewSimple[graph.EntityState]()
	require.NoError(t, err)
	comp.entityCache = c
	t.Cleanup(func() { _ = c.Close() })
	return comp
}

// prewarmCacheEntity writes id to KV (so KeysByPrefix sees the key) AND seeds
// the entityCache with the SAME entity the KV write produced, so the entity is
// returned as a cache HIT — and therefore FIRST — by fetchEntitiesConcurrent.
// It reads the stored entity back out of KV rather than reconstructing it so
// the cached value is byte-identical to the fetched one (CreateEntity stamps an
// indexing-profile triple per ADR-054).
func prewarmCacheEntity(t *testing.T, comp *Component, id string) {
	t.Helper()
	storePrefixEntity(t, comp, id)
	entry, err := comp.entityBucket.Get(context.Background(), id)
	require.NoError(t, err)
	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &entity))
	_, err = comp.entityCache.Set(id, entity)
	require.NoError(t, err)
}

func idsSorted(entities []graph.EntityState) bool {
	return sort.SliceIsSorted(entities, func(i, j int) bool {
		return entities[i].ID < entities[j].ID
	})
}

func idIDs(entities []graph.EntityState) []string {
	out := make([]string, len(entities))
	for i, e := range entities {
		out[i] = e.ID
	}
	return out
}

func entityMarshalLen(t *testing.T, comp *Component, id string) int {
	t.Helper()
	storePrefixEntity(t, comp, id)
	entry, err := comp.entityBucket.Get(context.Background(), id)
	require.NoError(t, err)
	return len(entry.Value)
}

// TestRegression_ByteTrimCursor_ScrambledFetchOrder is the handler-level
// regression for the fetch-order sort fix (orchestrator fix #1 — the
// sort.Slice(entities, byID) added right after fetchEntitiesConcurrent).
//
// Setup: 5 entities k01..k05. The two LATE-sorting IDs (k04,k05) are pre-warmed
// into the cache, so fetchEntitiesConcurrent returns them FIRST
// ([k04,k05,k01,k02,k03]). A small byte budget then trims the page to 2.
//
//   - WITHOUT sort.Slice(entities): the trimmed page is [k04,k05] and the
//     cursor is set on k05 — the lexicographic MAX of the whole prefix. Page 2
//     resumes after k05 -> empty. k01,k02,k03 are SILENTLY SKIPPED.
//   - WITH the fix: entities are re-sorted to [k01..k05], the trimmed page is
//     [k01,k02], cursor on k02, page 2 resumes at k03 -> full, sorted, disjoint
//     coverage.
//
// Verified to FAIL when the sort.Slice line is reverted (see review notes):
// without it the union is {k01,k02,k04,k05} (k03 skipped) and the page-0 cursor
// decodes to k05, not k02.
func TestRegression_ByteTrimCursor_ScrambledFetchOrder(t *testing.T) {
	comp := createTestComponentWithCache(t)

	const n = 5
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, fmt.Sprintf("acme.ops.dom.sys.type.k%02d", i))
	}
	// k01,k02,k03 -> KV-only (cache MISS). k04,k05 -> cache HIT (returned first
	// by fetchEntitiesConcurrent, scrambling order before the sort fix).
	for _, id := range ids[:3] {
		storePrefixEntity(t, comp, id)
	}
	for _, id := range ids[3:] {
		prewarmCacheEntity(t, comp, id)
	}

	// Budget that fits ~2 entities, then trims (each entity marshals to ~515B;
	// see TestRegression_FixtureMarshalSizes for the arithmetic guard).
	comp.maxPrefixResponseBytesOverride = 1500

	var collected []graph.EntityState
	seen := map[string]bool{}
	cursor := ""
	sawTrim := false
	for page := 0; page < n+2; page++ {
		resp := callPrefixHandler(t, comp, graph.PrefixQueryRequest{
			Prefix: "acme",
			Limit:  n, // count cap is NOT the binding constraint; byte budget is
			Cursor: cursor,
		})

		// INVARIANT 1: every page is sorted by ID.
		assert.True(t, idsSorted(resp.Entities),
			"page %d must be sorted by ID; got %v", page, idIDs(resp.Entities))

		// INVARIANT 2: when a cursor is set, it is the sorted MAX of the page
		// (the last returned entity). This is the exact property the sort fix
		// guarantees and the bug violated by setting it on an arbitrary key.
		if resp.NextCursor != "" {
			decoded, err := graph.DecodeCursor(resp.NextCursor)
			require.NoError(t, err)
			require.NotEmpty(t, resp.Entities, "a cursor with an empty page would loop")
			maxID := resp.Entities[len(resp.Entities)-1].ID
			assert.Equal(t, maxID, decoded,
				"page %d cursor must be the sorted MAX of the returned page", page)
		}

		if len(resp.Entities) > 0 && len(resp.Entities) < n {
			sawTrim = true
		}
		for _, e := range resp.Entities {
			assert.False(t, seen[e.ID], "duplicate entity across pages: %s", e.ID)
			seen[e.ID] = true
		}
		collected = append(collected, resp.Entities...)
		cursor = resp.NextCursor
		if cursor == "" {
			break
		}
	}

	// The byte budget must actually have trimmed at least one page, else the
	// test isn't exercising the byte-trim cursor path it claims to.
	require.True(t, sawTrim, "byte budget must have produced at least one trimmed page")

	// INVARIANT 3: complete coverage, no skips, globally sorted.
	require.Len(t, seen, n, "all %d entities must appear across pages (no skip); got %v", n, idIDs(collected))
	for _, id := range ids {
		assert.True(t, seen[id], "entity %s was skipped", id)
	}
	assert.True(t, idsSorted(collected), "global page sequence must be sorted; got %v", idIDs(collected))
}

// TestRegression_FirstEntityOversized_NoSkip is the handler-level regression
// for the first-entity-oversized fix (orchestrator fix #2 — applyPrefixByteLimit
// returning (entities[:1], true) instead of (entities[:1], false) when the
// first entity alone exceeds the budget).
//
// Setup: 3 entities. The byte budget is set SO small that even the first entity
// alone exceeds it.
//
//   - WITHOUT the fix ((entities[:1], false)): byteLimited=false, so the
//     handler falls to the COUNT branch. With limit>=3 the count branch sets NO
//     cursor (len(keys)==len(pageKeys)), so the response is [e1] with an EMPTY
//     cursor -> e2,e3 SILENTLY SKIPPED and pagination terminates after page 0.
//   - WITH the fix ((entities[:1], true)): byteLimited=true, cursor set on e1.
//     Page 1 resumes strictly after e1 -> [e2] -> page 2 -> [e3] -> a final
//     empty trailing page (cursor was set on e3). Full coverage, exactly one
//     entity per non-trailing page, monotonic advance (no infinite loop).
//
// Verified to FAIL when the (entities[:1], true) line is reverted to
// (entities[:1], false): coverage drops to {aaa} (bbb,ccc skipped) and the loop
// terminates after a single page.
func TestRegression_FirstEntityOversized_NoSkip(t *testing.T) {
	comp := createTestComponentWithMockKV(t)

	ids := []string{
		"acme.ops.dom.sys.type.aaa",
		"acme.ops.dom.sys.type.bbb",
		"acme.ops.dom.sys.type.ccc",
	}
	for _, id := range ids {
		storePrefixEntity(t, comp, id)
	}

	// Budget below even a single ~515B entity (300 - 256 envelope = 44 byte
	// content budget) -> first-entity-oversized fires on every page.
	comp.maxPrefixResponseBytesOverride = 300

	var collected []graph.EntityState
	seen := map[string]bool{}
	cursor := ""
	const maxPages = 16 // generous bound; an infinite loop trips this
	pages := 0
	var lastReturned string
	for ; pages < maxPages; pages++ {
		resp := callPrefixHandler(t, comp, graph.PrefixQueryRequest{
			Prefix: "acme",
			Limit:  len(ids), // count is NOT the binding constraint
			Cursor: cursor,
		})

		// Non-trailing pages return exactly one entity (first-oversized). The
		// final page may be empty (the cursor was set on the last entity), which
		// is correct termination — assert <=1, and that any returned entity is
		// new (no infinite loop) and strictly advances.
		require.LessOrEqual(t, len(resp.Entities), 1,
			"first-entity-oversized must return at most one entity per page")

		for _, e := range resp.Entities {
			assert.False(t, seen[e.ID], "entity repeated across pages (infinite loop): %s", e.ID)
			assert.Greater(t, e.ID, lastReturned, "entities must strictly advance")
			lastReturned = e.ID
			seen[e.ID] = true
			collected = append(collected, e)

			// The cursor must point at the entity just returned so the next
			// page resumes strictly after it.
			require.NotEmpty(t, resp.NextCursor, "a returned-but-oversized entity must carry a resume cursor")
			decoded, err := graph.DecodeCursor(resp.NextCursor)
			require.NoError(t, err)
			assert.Equal(t, e.ID, decoded, "cursor must be the just-returned entity's ID")
		}

		cursor = resp.NextCursor
		if cursor == "" {
			break
		}
	}

	require.Less(t, pages, maxPages, "pagination must terminate (no infinite loop)")
	require.Len(t, seen, len(ids), "every entity must be returned (no skip); got %v", idIDs(collected))
	for _, id := range ids {
		assert.True(t, seen[id], "entity %s was skipped", id)
	}
	assert.True(t, idsSorted(collected), "single-entity pages must arrive sorted; got %v", idIDs(collected))
}

// TestRegression_FixtureMarshalSizes documents the byte arithmetic the two
// regression tests rely on. A future change to EntityState's JSON shape (or to
// CreateEntity's stamped triples) that invalidates the budgets fails HERE with
// a clear message rather than as a confusing coverage regression elsewhere.
func TestRegression_FixtureMarshalSizes(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	size := entityMarshalLen(t, comp, "acme.ops.dom.sys.type.k01")

	// Scrambled-order test: budget 1500, envelope overhead 256 -> 1244 content
	// budget. Two entities must fit, three must not, so the trim fires at 2.
	const envelopeOverhead = 256
	scrambledBudget := 1500 - envelopeOverhead
	assert.Greater(t, scrambledBudget, 2*size,
		"two entities must fit the scrambled-order content budget (trim at page boundary)")
	assert.Less(t, scrambledBudget, 3*size,
		"three entities must exceed the scrambled-order content budget so a trim occurs")

	// First-entity-oversized test: budget 300 -> 44 content budget, below one
	// entity, so the first entity alone trips the over-budget branch.
	firstOversizedBudget := 300 - envelopeOverhead
	assert.Less(t, firstOversizedBudget, size,
		"a single entity must exceed the first-entity-oversized content budget")

	_ = time.Now
}
