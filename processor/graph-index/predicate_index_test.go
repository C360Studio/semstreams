package graphindex

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPredicateIndexProductionCodec_RawNineTokenContract(t *testing.T) {
	entityID := "acme.ops.robotics.gcs.drone.001"
	predicate := "robotics.status.armed"
	require.Equal(t, predicate+"."+entityID, predicateIndexKey(predicate, entityID))
	require.Equal(t, []string{
		"robotics.status.armed.*.*.*.*.*.*",
		"robotics.status.*.*.*.*.*.*.*",
		"robotics.*.*.*.*.*.*.*.*",
	}, predicateIndexForwardFilters(predicate))
	require.Equal(t, "*.*.*."+entityID, predicateIndexEntityFilter(entityID))
}

func TestPredicateIndexProductionCodec_MaximumBoundary(t *testing.T) {
	entityID := maximumEntityIDForContract()
	segment := "a" + strings.Repeat("b", 63)
	predicate := segment + "." + segment + "." + segment
	require.Len(t, predicateIndexKey(predicate, entityID), 451)
	for _, filter := range append(predicateIndexForwardFilters(predicate), predicateIndexEntityFilter(entityID)) {
		require.NoError(t, natsclient.ValidateKVWildcardFilter(filter))
	}
}

// seedPredicates writes self-describing composite-key memberships
// for a map of predicate -> entity IDs, using the production write path
// so these tests exercise the real key encoding, not a hand-rolled one.
func seedPredicates(t *testing.T, comp *Component, byPredicate map[string][]string) {
	t.Helper()
	ctx := context.Background()
	for predicate, entities := range byPredicate {
		for _, entityID := range entities {
			require.NoError(t, comp.UpdatePredicateIndex(ctx, entityID, predicate))
		}
	}
}

// TestListAllPredicates_NoCrossContamination is the regression test for
// the bug this ADR exists to fix: a raw dot-prefix design would let
// a namespace scan silently absorb neighboring predicate identities. The
// fixed-nine-token design must keep them fully separate.
func TestListAllPredicates_NoCrossContamination(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"agent.run.identity": {"c360.a.b.c.d.e1", "c360.a.b.c.d.e2"},
		"agent.run.phase":    {"c360.a.b.c.d.e3"},
	})

	predicates, err := comp.listAllPredicates(context.Background()) // predicate-audit:unrelated {"column":21,"surface":"go-assignment:predicates","value":"","basis":"reviewed query output returned by predicate index"}
	require.NoError(t, err)

	byName := make(map[string]int)
	for _, p := range predicates {
		byName[p.Predicate] = p.EntityCount
	}

	assert.Equal(t, 2, byName["agent.run.identity"], "exact predicate identity must not absorb a namespace neighbor")
	assert.Equal(t, 1, byName["agent.run.phase"], "agent.run.phase must report exactly its own 1 entity")
}

func TestListAllPredicates_EmptyMembershipBucket(t *testing.T) {
	comp := createTestComponentWithMockKV(t)

	predicates, err := comp.listAllPredicates(context.Background()) // predicate-audit:unrelated {"column":21,"surface":"go-assignment:predicates","value":"","basis":"reviewed query output returned by predicate index"}
	require.NoError(t, err)
	assert.Empty(t, predicates)
}

func TestPredicateQuery_InvalidPredicateHasNoBucketIO(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)
	var listCalls atomic.Int64
	predicateMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		listCalls.Add(1)
		return newMockKeyLister(nil), nil
	}

	_, err := comp.queryPredicateEntityIDs(context.Background(), "agent.run")
	require.Error(t, err)
	assert.Zero(t, listCalls.Load(), "invalid predicate must fail before predicate bucket I/O")
}

func TestPredicateNamespaceQuery_InvalidNamespaceHasNoBucketIO(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)
	var listCalls atomic.Int64
	predicateMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		listCalls.Add(1)
		return newMockKeyLister(nil), nil
	}

	_, err := comp.listPredicatesByNamespace(context.Background(), "agent.run.phase")
	require.Error(t, err)
	assert.Zero(t, listCalls.Load(), "invalid namespace must fail before predicate bucket I/O")
}

func TestPredicateQuery_MaximumBoundaryUsesValidatedExactFilter(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)
	entityID := maximumEntityIDForContract()
	segment := "a" + strings.Repeat("b", 63)
	predicate := segment + "." + segment + "." + segment
	expectedFilter := predicate + ".*.*.*.*.*.*"

	var listCalls atomic.Int64
	predicateMock(comp).listFilteredFunc = func(_ context.Context, filters ...string) (jetstream.KeyLister, error) {
		listCalls.Add(1)
		require.Equal(t, []string{expectedFilter}, filters)
		return newMockKeyLister([]string{predicateIndexKey(predicate, entityID)}), nil
	}

	entities, err := comp.queryPredicateEntityIDs(context.Background(), predicate)
	require.NoError(t, err)
	assert.Equal(t, []string{entityID}, entities)
	assert.EqualValues(t, 1, listCalls.Load())
}

// TestListPredicatesByNamespace_TrailingDotNormalization is the
// regression test for the blocking bug both reviewers found: a caller
// that omits the trailing dot on Prefix must still get correct namespace
// matches, not a silent empty result.
func TestListPredicatesByNamespace_TrailingDotNormalization(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"inferred.semantic.high":   {"c360.a.b.c.d.e1", "c360.a.b.c.d.e2"},
		"inferred.semantic.medium": {"c360.a.b.c.d.e3"},
		"inferred.structural.core": {"c360.a.b.c.d.e4"}, // different namespace, must not match
	})

	for _, prefix := range []string{"inferred.semantic.", "inferred.semantic"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			predicates, err := comp.listPredicatesByNamespace(context.Background(), prefix) // predicate-audit:unrelated {"column":23,"surface":"go-assignment:predicates","value":"","basis":"reviewed query output returned by predicate index"}
			require.NoError(t, err)

			byName := make(map[string]int)
			for _, p := range predicates {
				byName[p.Predicate] = p.EntityCount
			}

			assert.Len(t, predicates, 2, "should match exactly the two inferred.semantic.* predicates")
			assert.Equal(t, 2, byName["inferred.semantic.high"])
			assert.Equal(t, 1, byName["inferred.semantic.medium"])
			assert.NotContains(t, byName, "inferred.structural.core", "different namespace must not match")
		})
	}
}

func TestPredicateListsAreLexicallySorted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"robotics.assigned.team":    {"c360.a.b.c.d.e1"},
		"robotics.assigned.mission": {"c360.a.b.c.d.e1"},
		"agent.run.identity":        {"c360.a.b.c.d.e1"},
	})

	all, err := comp.listAllPredicates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []graph.PredicateSummary{
		{Predicate: "agent.run.identity", EntityCount: 1},
		{Predicate: "robotics.assigned.mission", EntityCount: 1},
		{Predicate: "robotics.assigned.team", EntityCount: 1},
	}, all)

	assigned, err := comp.listPredicatesByNamespace(context.Background(), "robotics.assigned")
	require.NoError(t, err)
	assert.Equal(t, []graph.PredicateSummary{
		{Predicate: "robotics.assigned.mission", EntityCount: 1},
		{Predicate: "robotics.assigned.team", EntityCount: 1},
	}, assigned)
}

func TestListPredicatesByNamespace_NoMatches(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"agent.run.identity": {"c360.a.b.c.d.e1"},
	})

	predicates, err := comp.listPredicatesByNamespace(context.Background(), "inferred.semantic") // predicate-audit:unrelated {"column":21,"surface":"go-assignment:predicates","value":"","basis":"reviewed query output returned by predicate index"}
	require.NoError(t, err)
	assert.Empty(t, predicates)
}

// TestHandleQueryPredicateListNATS_NamespaceField exercises the full NATS
// handler (request parsing + namespace routing), not just the internal
// helpers, so a regression in the request-parsing glue would also be
// caught here.
func TestHandleQueryPredicateListNATS_NamespaceField(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"inferred.semantic.high": {"c360.a.b.c.d.e1"},
		"agent.run.identity":     {"c360.a.b.c.d.e2"},
	})

	reqJSON, err := json.Marshal(graph.PredicateListQuery{Namespace: "inferred.semantic"})
	require.NoError(t, err)

	respData, err := comp.handleQueryPredicateListNATS(context.Background(), reqJSON)
	require.NoError(t, err)

	var resp graph.PredicateListQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	require.Len(t, resp.Data.Predicates, 1)
	assert.Equal(t, "inferred.semantic.high", resp.Data.Predicates[0].Predicate)
}

func TestHandleQueryPredicateListNATS_EmptyBody_ListsAll(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"agent.run.identity":     {"c360.a.b.c.d.e1"},
		"inferred.semantic.high": {"c360.a.b.c.d.e2"},
	})

	// Existing callers send an empty/absent body — must keep listing
	// everything, not error or silently scope to nothing.
	respData, err := comp.handleQueryPredicateListNATS(context.Background(), []byte("{}"))
	require.NoError(t, err)

	var resp graph.PredicateListQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.Len(t, resp.Data.Predicates, 2)
}

func TestPredicateQueriesRejectStoredIdentityAndNamespaceAmbiguity(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)

	_, err := comp.handleQueryPredicateNATS(context.Background(), []byte(`{"predicate":"agent.run"}`)) // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run","reason":"arity"}
	require.Error(t, err, "two-part namespace must not be accepted as an exact predicate")

	_, err = comp.handleQueryPredicateListNATS(context.Background(), []byte(`{"namespace":"agent.run.phase"}`))
	require.Error(t, err, "three-part predicate must not be accepted as a namespace")

	_, err = comp.handleQueryPredicateListNATS(context.Background(), []byte(`{"prefix":"agent.run"}`))
	require.Error(t, err, "removed prefix wire field must fail rather than silently list every predicate")

	err = comp.UpdatePredicateIndex(context.Background(), "c360.a.b.c.d.e1", "agent.run")
	require.Error(t, err, "index codec must not admit a noncanonical predicate")
}

func TestHandleQueryPredicateStatsNATS_CompositeKeys(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"robotics.status.armed": {
			"c360.a.b.c.d.e1",
			"c360.a.b.c.d.e2",
			"c360.a.b.c.d.e3",
		},
	})

	reqJSON, err := json.Marshal(map[string]any{"predicate": "robotics.status.armed", "sample_limit": 2})
	require.NoError(t, err)

	respData, err := comp.handleQueryPredicateStatsNATS(context.Background(), reqJSON)
	require.NoError(t, err)

	var resp graph.PredicateStatsQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	assert.Equal(t, 3, resp.Data.EntityCount, "count must reflect all members, not just the sample")
	assert.Len(t, resp.Data.SampleEntities, 2, "sample must be capped at sample_limit")
}

func TestHandleQueryPredicateCompoundNATS_AndOr(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	seedPredicates(t, comp, map[string][]string{
		"robotics.status.armed":    {"c360.a.b.c.d.e1", "c360.a.b.c.d.e2"},
		"robotics.status.airborne": {"c360.a.b.c.d.e2", "c360.a.b.c.d.e3"},
	})

	andReq, err := json.Marshal(graph.CompoundPredicateQuery{
		Predicates: []string{"robotics.status.armed", "robotics.status.airborne"},
		Operator:   "AND",
	})
	require.NoError(t, err)
	respData, err := comp.handleQueryPredicateCompoundNATS(context.Background(), andReq)
	require.NoError(t, err)
	var andResp graph.CompoundPredicateQueryResponse
	require.NoError(t, json.Unmarshal(respData, &andResp))
	assert.ElementsMatch(t, []string{"c360.a.b.c.d.e2"}, andResp.Data.Entities, "AND must return only the shared entity")

	orReq, err := json.Marshal(graph.CompoundPredicateQuery{
		Predicates: []string{"robotics.status.armed", "robotics.status.airborne"},
		Operator:   "OR",
	})
	require.NoError(t, err)
	respData, err = comp.handleQueryPredicateCompoundNATS(context.Background(), orReq)
	require.NoError(t, err)
	var orResp graph.CompoundPredicateQueryResponse
	require.NoError(t, json.Unmarshal(respData, &orResp))
	assert.ElementsMatch(t, []string{"c360.a.b.c.d.e1", "c360.a.b.c.d.e2", "c360.a.b.c.d.e3"}, orResp.Data.Entities, "OR must return the union")
}

// TestNatsPrefixTokenMatch locks down the mock's own matching semantics
// against the real NATS behavior both reviewers empirically verified
// (KeysByPrefix hardcodes prefix+">" server-side) — if this test and the
// production trailing-dot fix ever drift apart, this is what catches it.
func TestNatsPrefixTokenMatch(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		filter string
		want   bool
	}{
		{"well-formed prefix matches child", "agent.run.phase", "agent.run.>", true},
		{"well-formed prefix does not match self", "agent.run", "agent.run.>", false},
		{"well-formed prefix does not false-positive on longer token", "agent.runner.other", "agent.run.>", false},
		{"missing trailing dot matches nothing", "agent.run.phase", "agent.run>", false},
		{"unrelated namespace does not match", "inferred.structural.core", "inferred.semantic.>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, natsPrefixTokenMatch(tt.key, tt.filter))
		})
	}
}
