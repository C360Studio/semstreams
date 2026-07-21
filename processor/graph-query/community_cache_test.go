package graphquery

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Community IDs are seed entity IDs (graph/clustering/lpa.go buildCommunities), and
// detectHierarchicalLevel re-runs LPA over the SAME entity set at every level, so the
// same ID legitimately appears at level 0, 1 and 2 of one detection run.
const (
	entDrone1 = "acme.ops.robotics.gcs.drone.001"
	entDrone2 = "acme.ops.robotics.gcs.drone.002"
	entDrone3 = "acme.ops.robotics.gcs.drone.003"
)

// communityKVKey mirrors clustering.communityKey — the KV key format is the storage
// identity the cache must mirror. Pinned in graph/clustering/storage.go.
// Shared with the integration tests, which must seed COMMUNITY_INDEX on the real key
// shape or they stop exercising the level-derivation the cache depends on.
func communityKVKey(level int, id string) string { return fmt.Sprintf("%d.%s", level, id) }

func newTestCache() *CommunityCache {
	return NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// put drives the watch-update path with the exact bytes SaveCommunity writes:
// json.Marshal of the Community under key {level}.{id}.
func put(t *testing.T, c *CommunityCache, comm *clustering.Community) {
	t.Helper()
	data, err := json.Marshal(comm)
	require.NoError(t, err)
	c.handleUpdate(communityKVKey(comm.Level, comm.ID), data)
}

// TestCommunityCache_CrossLevelCollision_LateDeleteKeepsLevelZero replays a real
// detection cycle's KV op ordering under write-then-prune: every level is written
// first, and the previous partition's leftovers are deleted LAST.
//
// Regression for a cache keyed by bare community ID. Because a level-1 write shadows
// the level-0 entry for a colliding ID, a delete arriving after the writes rebuilt
// the level-0 index from an already-shadowed map and collapsed it to empty.
// globalSearchTextBased has no NATS fallback (graphrag.go: len==0 → "no_communities"),
// so that reads as an authoritative "this graph has no communities".
func TestCommunityCache_CrossLevelCollision_LateDeleteKeepsLevelZero(t *testing.T) {
	c := newTestCache()

	// Level-0 pass: two communities.
	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1, entDrone2}})
	put(t, c, &clustering.Community{ID: entDrone3, Level: 0, Members: []string{entDrone3}})
	require.Len(t, c.GetCommunitiesByLevel(0), 2, "precondition: both level-0 communities cached")

	// Level-1 pass: LPA re-seeds from the same entity pool, so entDrone1 is a
	// community ID at level 1 as well.
	put(t, c, &clustering.Community{ID: entDrone1, Level: 1, Members: []string{entDrone1, entDrone2, entDrone3}})
	assert.Len(t, c.GetCommunitiesByLevel(0), 2,
		"a level-1 write must not evict a level-0 community that happens to share its ID")

	// Prune of the previous partition lands last.
	c.handleDelete(communityKVKey(0, entDrone3))

	level0 := c.GetCommunitiesByLevel(0)
	require.Len(t, level0, 1,
		"level-0 index must still hold the surviving level-0 community after a late delete")
	assert.Equal(t, entDrone1, level0[0].ID)
	assert.Equal(t, 0, level0[0].Level)
	assert.ElementsMatch(t, []string{entDrone1, entDrone2}, level0[0].Members,
		"the level-0 record must be returned, not the level-1 record with the same ID")

	level1 := c.GetCommunitiesByLevel(1)
	require.Len(t, level1, 1, "the level-1 community is untouched by a level-0 delete")
	assert.Len(t, level1[0].Members, 3)
}

// TestCommunityCache_DeleteIsLevelScoped pins the other half: deleting the level-0
// key for a COLLIDING id must not take the level-1 record (or its entity mappings)
// with it. A bare-ID map resolves "0.<id>" to whichever level was written last.
func TestCommunityCache_DeleteIsLevelScoped(t *testing.T) {
	c := newTestCache()

	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1, entDrone2}})
	put(t, c, &clustering.Community{ID: entDrone1, Level: 1, Members: []string{entDrone1, entDrone2, entDrone3}})

	c.handleDelete(communityKVKey(0, entDrone1))

	assert.Empty(t, c.GetCommunitiesByLevel(0), "the level-0 record was the delete target")

	level1 := c.GetCommunitiesByLevel(1)
	require.Len(t, level1, 1, "deleting the level-0 key must leave the level-1 community resolvable")
	assert.Len(t, level1[0].Members, 3)

	// Entity mappings are level-scoped; a level-0 delete strips only level-0 mappings.
	assert.Nil(t, c.GetEntityCommunity(entDrone1, 0), "level-0 mapping removed with its community")
	l1 := c.GetEntityCommunity(entDrone1, 1)
	require.NotNil(t, l1, "level-1 mapping must survive a level-0 delete")
	assert.Equal(t, 1, l1.Level)
}

// TestCommunityCache_DeleteOfHigherLevelKeySparesLevelZero is the mirror of
// TestCommunityCache_DeleteIsLevelScoped and the case that pins ORDER-independence.
// Prune deletes stale keys at every level, so "1.{id}" arrives for IDs that also
// exist at level 0. Resolving the bare ID by scanning levels ascending answers the
// level-0 record and evicts the wrong one — which a level-0-keyed delete test
// cannot see, because ascending happens to land on the right record there.
func TestCommunityCache_DeleteOfHigherLevelKeySparesLevelZero(t *testing.T) {
	c := newTestCache()

	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1, entDrone2}})
	put(t, c, &clustering.Community{ID: entDrone1, Level: 1, Members: []string{entDrone1, entDrone2, entDrone3}})

	c.handleDelete(communityKVKey(1, entDrone1))

	level0 := c.GetCommunitiesByLevel(0)
	require.Len(t, level0, 1, "deleting the level-1 key must leave the level-0 community intact")
	assert.Equal(t, []string{entDrone1, entDrone2}, level0[0].Members)
	assert.Empty(t, c.GetCommunitiesByLevel(1), "the level-1 record was the delete target")

	l0 := c.GetEntityCommunity(entDrone1, 0)
	require.NotNil(t, l0, "level-0 mapping must survive a level-1 delete")
	assert.Equal(t, 0, l0.Level)
	assert.Nil(t, c.GetEntityCommunity(entDrone1, 1), "level-1 mapping removed with its community")
	assert.Nil(t, c.GetEntityCommunity(entDrone3, 1), "entDrone3 was only a level-1 member")
}

// TestCommunityCache_KeyWinsOverPayloadLevel pins which side is authoritative when a
// record's Level field disagrees with the key it arrived under. The key wins: it is
// the only handle a later delete has, so indexing by the payload would strand the
// entry permanently.
func TestCommunityCache_KeyWinsOverPayloadLevel(t *testing.T) {
	c := newTestCache()

	// Written to "0.{id}" but claiming Level 2.
	data, err := json.Marshal(&clustering.Community{ID: entDrone1, Level: 2, Members: []string{entDrone1}})
	require.NoError(t, err)
	c.handleUpdate(communityKVKey(0, entDrone1), data)

	require.Len(t, c.GetCommunitiesByLevel(0), 1, "indexed under the key's level")
	assert.Empty(t, c.GetCommunitiesByLevel(2), "not indexed under the payload's level")
	got := c.GetCommunity(0, entDrone1)
	require.NotNil(t, got)
	assert.Equal(t, 0, got.Level,
		"the record must report the level it is filed under, or callers cannot re-resolve it")
	assert.NotNil(t, c.GetEntityCommunity(entDrone1, 0))

	// And it remains deletable under the key it arrived on.
	c.handleDelete(communityKVKey(0, entDrone1))
	assert.Empty(t, c.GetAllCommunities())
	assert.Nil(t, c.GetEntityCommunity(entDrone1, 0))
}

// TestCommunityCache_RejectsUnparseableKeys keeps non-community keys out of the
// index rather than filing them under a guessed level.
func TestCommunityCache_RejectsUnparseableKeys(t *testing.T) {
	c := newTestCache()

	data, err := json.Marshal(&clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1}})
	require.NoError(t, err)

	keys := []string{"nolevel", "x." + entDrone1, ".leading", "0.", ""}

	// Assert BEFORE the deletes: an update+delete pair per key would clean up after
	// itself and hide an accepted key.
	for _, key := range keys {
		c.handleUpdate(key, data)
	}
	assert.Empty(t, c.GetAllCommunities(), "no unparseable key may enter the index")
	assert.Equal(t, 0, c.Stats().TotalEntities)

	for _, key := range keys {
		c.handleDelete(key)
	}
	assert.Empty(t, c.GetAllCommunities())
}

// TestCommunityCache_LookupsAreLevelQualified proves the read surfaces resolve the
// record for the level asked for, not an arbitrary one. enrichCommunitySummaries
// sets MemberCount/RepEntities from GetCommunity, so a level-blind hit reports the
// level-2 membership on a level-0 search result.
func TestCommunityCache_LookupsAreLevelQualified(t *testing.T) {
	c := newTestCache()

	put(t, c, &clustering.Community{
		ID: entDrone1, Level: 0,
		Members:     []string{entDrone1},
		RepEntities: []string{entDrone1},
	})
	put(t, c, &clustering.Community{
		ID: entDrone1, Level: 2,
		Members:     []string{entDrone1, entDrone2, entDrone3},
		RepEntities: []string{entDrone3},
	})

	l0 := c.GetCommunity(0, entDrone1)
	require.NotNil(t, l0)
	assert.Equal(t, 0, l0.Level)
	assert.Len(t, l0.Members, 1)
	assert.Equal(t, []string{entDrone1}, l0.RepEntities)

	l2 := c.GetCommunity(2, entDrone1)
	require.NotNil(t, l2)
	assert.Equal(t, 2, l2.Level)
	assert.Len(t, l2.Members, 3)
	assert.Equal(t, []string{entDrone3}, l2.RepEntities)

	assert.Nil(t, c.GetCommunity(1, entDrone1), "no level-1 record was written")

	// GetEntityCommunity resolves within the requested level.
	e0 := c.GetEntityCommunity(entDrone1, 0)
	require.NotNil(t, e0)
	assert.Len(t, e0.Members, 1)
	e2 := c.GetEntityCommunity(entDrone1, 2)
	require.NotNil(t, e2)
	assert.Len(t, e2.Members, 3)
	assert.Nil(t, c.GetEntityCommunity(entDrone2, 0), "entDrone2 is only a level-2 member")
}

// TestCommunityCache_StatsCountCollidingIDsSeparately keeps the operator-facing
// counters honest: two records that share an ID across levels are two communities.
func TestCommunityCache_StatsCountCollidingIDsSeparately(t *testing.T) {
	c := newTestCache()

	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1}})
	put(t, c, &clustering.Community{ID: entDrone2, Level: 0, Members: []string{entDrone2}})
	put(t, c, &clustering.Community{ID: entDrone1, Level: 1, Members: []string{entDrone1, entDrone2}})

	stats := c.Stats()
	assert.Equal(t, 3, stats.TotalCommunities)
	assert.Equal(t, map[int]int{0: 2, 1: 1}, stats.ByLevel)
	assert.Equal(t, 2, stats.TotalEntities)

	all := c.GetAllCommunities()
	assert.Len(t, all, 3, "GetAllCommunities must not collapse cross-level ID collisions")

	// The (level, ID) ordering is load-bearing, not cosmetic. findCommunitiesForEntities
	// consumes this order and re-sorts with an UNSTABLE sort.Slice on Relevance alone,
	// then the caller truncates to MaxCommunities. Without the level tie-break, two
	// same-ID records at different levels compare equal in both directions, so their
	// relative order falls out of randomized map iteration — and WHICH of them survives
	// truncation flips between two identical queries.
	gotOrder := make([][2]interface{}, len(all))
	for i, comm := range all {
		gotOrder[i] = [2]interface{}{comm.Level, comm.ID}
	}
	assert.Equal(t, [][2]interface{}{
		{0, entDrone1},
		{0, entDrone2},
		{1, entDrone1},
	}, gotOrder, "GetAllCommunities must order by (level, ID) — a same-ID pair at two "+
		"levels must have a deterministic relative order, not map-iteration order")
}

// TestCommunityCache_UpdateReplacesSameLevelRecord pins replacement: re-writing the
// same (level, ID) swaps membership rather than accumulating, and mappings for
// entities dropped from the community stop resolving ([A] -> [B] -> []).
func TestCommunityCache_UpdateReplacesSameLevelRecord(t *testing.T) {
	c := newTestCache()

	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1, entDrone2}})
	put(t, c, &clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1}})

	level0 := c.GetCommunitiesByLevel(0)
	require.Len(t, level0, 1)
	assert.Equal(t, []string{entDrone1}, level0[0].Members)
	assert.Nil(t, c.GetEntityCommunity(entDrone2, 0), "a dropped member must stop resolving")
	assert.Equal(t, 1, c.Stats().TotalEntities)
}

// TestCommunityCache_IgnoresEntityMappingKeys keeps the entity.{level}.{id} keys —
// which carry a bare community ID string, not JSON — out of the community index.
func TestCommunityCache_IgnoresEntityMappingKeys(t *testing.T) {
	c := newTestCache()

	c.handleUpdate("entity.0."+entDrone1, []byte(entDrone1))
	c.handleDelete("entity.0." + entDrone1)

	assert.Empty(t, c.GetAllCommunities())
	assert.Equal(t, 0, c.Stats().TotalCommunities)
}

// TestCommunityCache_GetCommunitiesByLevelIsDeterministic pins stable ordering.
// scoreCommunitySummaries sorts with an unstable sort.Slice and no tie-break, so
// identical-score communities would otherwise reorder between identical queries.
func TestCommunityCache_GetCommunitiesByLevelIsDeterministic(t *testing.T) {
	c := newTestCache()

	for _, id := range []string{entDrone3, entDrone1, entDrone2} {
		put(t, c, &clustering.Community{ID: id, Level: 0, Members: []string{id}})
	}

	want := []string{entDrone1, entDrone2, entDrone3}
	for i := 0; i < 20; i++ {
		got := make([]string, 0, 3)
		for _, comm := range c.GetCommunitiesByLevel(0) {
			got = append(got, comm.ID)
		}
		require.Equal(t, want, got, "GetCommunitiesByLevel must return a stable order")
	}
}
