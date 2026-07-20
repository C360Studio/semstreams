package graphindex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- pure ranking + normalization ---

func TestNormalizeName_FoldsAndTrims(t *testing.T) {
	assert.Equal(t, "foo", normalizeName("  Foo "))
	assert.Equal(t, "foo bar", normalizeName("FOO BAR"))
	// Same key for case variants, distinct key for distinct names.
	assert.Equal(t, nameIndexKey("OnEvent"), nameIndexKey("onevent"))
	assert.NotEqual(t, nameIndexKey("OnEvent"), nameIndexKey("OnEvents"))
}

func TestRankNameMatches(t *testing.T) {
	items := []graph.NameIndexItem{
		{EntityID: "test.graph.index.name.entity.pref", Name: "foo", Predicate: "skos.core.pref-label", Priority: 0}, // high salience, not exact-case
		{EntityID: "test.graph.index.name.entity.exact", Name: "Foo", Predicate: "dc.terms.title", Priority: 1},      // exact-case
		{EntityID: "test.graph.index.name.entity.upper", Name: "FOO", Predicate: "dc.terms.title", Priority: 1},      // fold-only
	}

	got := rankNameMatches(items, "Foo", 0)
	require.Len(t, got, 3)
	// 1) exact-case wins over higher salience.
	assert.Equal(t, "test.graph.index.name.entity.exact", got[0].EntityID)
	assert.True(t, got[0].ExactCase)
	// 2) among fold-only, lower priority (higher salience) wins.
	assert.Equal(t, "test.graph.index.name.entity.pref", got[1].EntityID)
	assert.False(t, got[1].ExactCase)
	// 3) remaining fold-only by entity ID.
	assert.Equal(t, "test.graph.index.name.entity.upper", got[2].EntityID)
}

func TestRankNameMatches_DedupsByEntityKeepingBest(t *testing.T) {
	// Same entity carries the name under two predicates; keep its best (exact + salient).
	items := []graph.NameIndexItem{
		{EntityID: "test.graph.index.name.entity.e1", Name: "FOO", Predicate: "dc.terms.title", Priority: 1},
		{EntityID: "test.graph.index.name.entity.e1", Name: "Foo", Predicate: "skos.core.pref-label", Priority: 0},
	}
	got := rankNameMatches(items, "Foo", 0)
	require.Len(t, got, 1)
	assert.Equal(t, "skos.core.pref-label", got[0].Predicate)
	assert.True(t, got[0].ExactCase)
}

func TestRankNameMatches_Limit(t *testing.T) {
	items := []graph.NameIndexItem{
		{EntityID: "test.graph.index.name.entity.e3", Name: "x", Priority: 1},
		{EntityID: "test.graph.index.name.entity.e1", Name: "x", Priority: 1},
		{EntityID: "test.graph.index.name.entity.e2", Name: "x", Priority: 1},
	}
	got := rankNameMatches(items, "x", 2)
	require.Len(t, got, 2)
	assert.Equal(t, "test.graph.index.name.entity.e1", got[0].EntityID) // tiebreak by entity ID asc
	assert.Equal(t, "test.graph.index.name.entity.e2", got[1].EntityID)
}

// --- KV round-trip through the real Update + query handler ---

func newNameTestComponent(t *testing.T) *Component {
	t.Helper()
	comp := createTestComponentWithMockKV(t)
	nc, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	comp.nameBucket = nc.NewKVStore(newMockKVBucket())
	return comp
}

func queryByName(t *testing.T, comp *Component, name string, limit int) []graph.NameMatch {
	t.Helper()
	req, err := json.Marshal(map[string]any{"name": name, "limit": limit})
	require.NoError(t, err)
	respData, err := comp.handleQueryByNameNATS(context.Background(), req)
	require.NoError(t, err)
	var resp graph.QueryResponse[graph.NameData]
	require.NoError(t, json.Unmarshal(respData, &resp))
	return resp.Data.Matches
}

func TestUpdateNameIndex_RoundTripRankingAndRecall(t *testing.T) {
	comp := newNameTestComponent(t)
	ctx := context.Background()

	require.NoError(t, comp.UpdateNameIndex(ctx, "Foo", "org.p.d.s.t.exact", "dc.terms.title", 1))
	require.NoError(t, comp.UpdateNameIndex(ctx, "FOO", "org.p.d.s.t.upper", "dc.terms.title", 1))
	require.NoError(t, comp.UpdateNameIndex(ctx, "foo", "org.p.d.s.t.pref", "skos.core.pref-label", 0))

	// Case-insensitive recall: a lowercase query finds all three.
	got := queryByName(t, comp, "Foo", 0)
	require.Len(t, got, 3)
	assert.Equal(t, "org.p.d.s.t.exact", got[0].EntityID, "exact-case ranks first")
	assert.Equal(t, "org.p.d.s.t.pref", got[1].EntityID, "then highest salience")
	assert.Equal(t, "org.p.d.s.t.upper", got[2].EntityID)
}

func TestUpdateNameIndex_IdempotentReindex(t *testing.T) {
	comp := newNameTestComponent(t)
	ctx := context.Background()
	require.NoError(t, comp.UpdateNameIndex(ctx, "Doc", "org.p.d.s.t.1", "dc.terms.title", 1))
	require.NoError(t, comp.UpdateNameIndex(ctx, "Doc", "org.p.d.s.t.1", "dc.terms.title", 1)) // re-index same

	got := queryByName(t, comp, "Doc", 0)
	require.Len(t, got, 1, "re-indexing the same (entity,predicate) must not duplicate")
}

func TestHandleQueryByName_UnknownNameIsEmptyNotError(t *testing.T) {
	comp := newNameTestComponent(t)
	got := queryByName(t, comp, "nothing-here", 0)
	assert.Empty(t, got, "unknown name returns empty matches (ready-but-absent), not an error")
}

func TestHandleQueryByName_EmptyNameRejected(t *testing.T) {
	comp := newNameTestComponent(t)
	req, _ := json.Marshal(map[string]any{"name": ""})
	_, err := comp.handleQueryByNameNATS(context.Background(), req)
	require.Error(t, err)
}

// --- gh#397 index-readiness status ---

// queryStatus reads the readiness projection these tests assert on. It used to go
// through the `graph.index.query.status` request handler; ADR-083 removed that subject
// (the envelope is published to GRAPH_STATUS instead), so the tests now call the
// projection the handler only ever marshalled. The coverage is unchanged — these cases
// are about the legacy pre-watermark NAME_INDEX fallback inside computeIndexStatus,
// never about the transport. The wire encoding of the envelope is covered where it now
// happens, in the GRAPH_STATUS publish path.
func queryStatus(t *testing.T, comp *Component) graph.IndexStatusResponse {
	t.Helper()
	return comp.computeIndexStatus(context.Background())
}

func TestQueryStatus_NotReadyUntilNameIndexPopulated(t *testing.T) {
	comp := newNameTestComponent(t)

	// Empty index → not ready (building): the caller must fall back, NOT treat an
	// empty byName result as an authoritative not-found.
	st := queryStatus(t, comp)
	assert.False(t, st.Ready, "empty NAME_INDEX must report not-ready")
	assert.Equal(t, graph.IndexStateBuilding, st.State)

	// After an index update → ready.
	require.NoError(t, comp.UpdateNameIndex(context.Background(), "Foo", "org.p.d.s.t.x", "dc.terms.title", 1))
	st = queryStatus(t, comp)
	assert.True(t, st.Ready, "a populated NAME_INDEX must report ready")
	assert.Equal(t, graph.IndexStateReady, st.State)
}

// TestQueryStatus_RestartLazyCheck: after a restart the in-memory sticky flag is
// false, but a pre-populated bucket must still read ready via the one-time lazy
// Keys() check (else a warm index would falsely report not-ready post-restart).
func TestQueryStatus_RestartLazyCheck(t *testing.T) {
	comp := newNameTestComponent(t)
	require.NoError(t, comp.UpdateNameIndex(context.Background(), "Foo", "org.p.d.s.t.x", "dc.terms.title", 1))

	comp.nameIndexReady.Store(false) // simulate restart: sticky flag reset, bucket retains data

	st := queryStatus(t, comp)
	assert.True(t, st.Ready, "a pre-populated index must read ready after restart via the lazy bucket check")
}
