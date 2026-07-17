package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessEntityUpdate_ReplacesPublicIndexResults(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.namePredicates = map[string]int{"core.identity.name": 0}
	ctx := context.Background()
	entityID := "acme.ops.robotics.gcs.drone.001"
	targetA := "acme.ops.robotics.gcs.mission.001"
	targetB := "acme.ops.robotics.gcs.mission.002"

	type fixture struct {
		nameA         []string
		nameB         []string
		predicateA    []string
		predicateB    []string
		predicateList []string
		statusList    []string
		incomingA     []graph.IncomingEntry
		incomingB     []graph.IncomingEntry
	}
	fixtures := []struct {
		name    string
		triples []message.Triple
		want    fixture
	}{
		{
			name: "A",
			triples: []message.Triple{
				{Subject: entityID, Predicate: "core.identity.name", Object: "Alpha"},
				{Subject: entityID, Predicate: "robotics.status.armed", Object: true},
				{Subject: entityID, Predicate: "robotics.assigned.mission", Object: targetA},
			},
			want: fixture{
				nameA:      []string{entityID},
				nameB:      []string{},
				predicateA: []string{entityID},
				predicateB: []string{},
				predicateList: []string{
					"core.identity.name", "robotics.assigned.mission", "robotics.status.armed",
				},
				statusList: []string{"robotics.status.armed"},
				incomingA:  []graph.IncomingEntry{{FromEntityID: entityID, Predicate: "robotics.assigned.mission"}},
				incomingB:  []graph.IncomingEntry{},
			},
		},
		{
			name: "B",
			triples: []message.Triple{
				{Subject: entityID, Predicate: "core.identity.name", Object: "Beta"},
				{Subject: entityID, Predicate: "robotics.status.disarmed", Object: true},
				{Subject: entityID, Predicate: "robotics.assigned.mission", Object: targetB},
			},
			want: fixture{
				nameA:      []string{},
				nameB:      []string{entityID},
				predicateA: []string{},
				predicateB: []string{entityID},
				predicateList: []string{
					"core.identity.name", "robotics.assigned.mission", "robotics.status.disarmed",
				},
				statusList: []string{"robotics.status.disarmed"},
				incomingA:  []graph.IncomingEntry{},
				incomingB:  []graph.IncomingEntry{{FromEntityID: entityID, Predicate: "robotics.assigned.mission"}},
			},
		},
		{name: "empty", triples: []message.Triple{}, want: fixture{
			nameA: []string{}, nameB: []string{}, predicateA: []string{}, predicateB: []string{},
			predicateList: []string{}, statusList: []string{},
			incomingA: []graph.IncomingEntry{}, incomingB: []graph.IncomingEntry{},
		}},
	}

	for _, step := range fixtures {
		t.Run(step.name, func(t *testing.T) {
			data, err := json.Marshal(graph.EntityState{ID: entityID, Triples: step.triples})
			require.NoError(t, err)
			require.NoError(t, comp.processEntityUpdateFromData(ctx, entityID, data))

			assert.Equal(t, step.want.nameA, queryNameEntityIDs(t, comp, "Alpha"))
			assert.Equal(t, step.want.nameB, queryNameEntityIDs(t, comp, "Beta"))
			assert.Equal(t, step.want.predicateA, queryPredicateEntityIDs(t, comp, "robotics.status.armed"))
			assert.Equal(t, step.want.predicateB, queryPredicateEntityIDs(t, comp, "robotics.status.disarmed"))
			assert.Equal(t, step.want.predicateList, queryPredicateNames(t, comp, ""))
			assert.Equal(t, step.want.statusList, queryPredicateNames(t, comp, "robotics.status"))
			assert.Equal(t, step.want.incomingA, queryIncomingEntries(t, comp, targetA))
			assert.Equal(t, step.want.incomingB, queryIncomingEntries(t, comp, targetB))
		})
	}
}

func TestDeleteFromIndexes_RetractsOwnedRowsWithoutDeletingLiveSourceAssertions(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	retiredSource := "acme.ops.robotics.gcs.drone.001"
	liveSource := "acme.ops.robotics.gcs.drone.002"
	target := "acme.ops.robotics.gcs.mission.001"

	require.NoError(t, comp.reconcilePredicateIndex(ctx, retiredSource, map[string]bool{"robotics.status.armed": true}))
	require.NoError(t, comp.reconcileNameIndex(ctx, retiredSource, []nameIndexWrite{{
		name: "Alpha", predicate: "core.identity.name", priority: 0,
	}}))
	require.NoError(t, comp.reconcileIncomingIndex(ctx, retiredSource, map[string][]graph.IncomingEntry{
		target: {{FromEntityID: retiredSource, Predicate: "robotics.assigned.mission"}},
	}))
	require.NoError(t, comp.reconcileIncomingIndex(ctx, liveSource, map[string][]graph.IncomingEntry{
		retiredSource: {{FromEntityID: liveSource, Predicate: "robotics.assigned.mission"}},
	}))

	require.NoError(t, comp.DeleteFromIndexes(ctx, retiredSource))

	assert.Empty(t, queryNameEntityIDs(t, comp, "Alpha"))
	assert.Empty(t, queryPredicateEntityIDs(t, comp, "robotics.status.armed"))
	assert.Empty(t, queryIncomingEntries(t, comp, target), "retired source assertions must be retracted")
	assert.Equal(t, []graph.IncomingEntry{{
		FromEntityID: liveSource, Predicate: "robotics.assigned.mission",
	}}, queryIncomingEntries(t, comp, retiredSource), "live source assertion must survive target retirement")
}

func TestDeleteFromIndexes_InvalidOwnerHasNoBucketIO(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	var calls atomic.Int64
	for _, mock := range []*mockKVBucket{outgoingMock(comp), incomingMock(comp), predicateMock(comp), nameMock(comp), contextMock(comp)} {
		mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
			calls.Add(1)
			return newMockKeyLister(nil), nil
		}
		mock.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
			calls.Add(1)
			return nil
		}
	}

	err := comp.DeleteFromIndexes(context.Background(), "malformed")
	require.Error(t, err)
	assert.Zero(t, calls.Load(), "owner validation must precede every list/delete")
}

func TestDeleteFromIndexes_LateDeletePlanFailureHasNoDeletesOrSemanticMetrics(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entityID := "acme.ops.robotics.gcs.drone.001"
	var deletes atomic.Int64
	for _, mock := range []*mockKVBucket{
		outgoingMock(comp), incomingMock(comp), predicateMock(comp), nameMock(comp), contextMock(comp),
	} {
		mock.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
			deletes.Add(1)
			return nil
		}
	}
	// Context is deliberately the last list in the production delete plan. If it
	// cannot be resolved, no earlier family (including OUTGOING) may be retracted.
	contextMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return nil, errors.New("late context list unavailable")
	}

	processedBefore := atomic.LoadInt64(&comp.messagesProcessed)
	indexUpdatesBefore := make(map[string]float64)
	for _, indexType := range []string{"name", "predicate", "incoming", "context", "outgoing"} {
		indexUpdatesBefore[indexType] = testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType))
	}

	err := comp.DeleteFromIndexes(context.Background(), entityID)
	require.Error(t, err)
	assert.Zero(t, deletes.Load(), "the complete delete plan must resolve before the first delete")
	assert.Equal(t, processedBefore, atomic.LoadInt64(&comp.messagesProcessed))
	for indexType, before := range indexUpdatesBefore {
		assert.Equal(t, before, testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType)), indexType)
	}
}

func TestIncomingQuery_InvalidFilterOwnerHasNoBucketIO(t *testing.T) {
	tests := []string{
		"malformed",
		"a.a.a.a.a." + strings.Repeat("e", 247),
	}
	for _, entityID := range tests {
		t.Run(fmt.Sprintf("bytes_%d", len(entityID)), func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			var lists atomic.Int64
			incomingMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
				lists.Add(1)
				return newMockKeyLister(nil), nil
			}

			_, err := comp.handleQueryIncomingNATS(context.Background(),
				[]byte(fmt.Sprintf(`{"entity_id":%q}`, entityID)))
			require.Error(t, err)
			assert.True(t, errs.IsInvalid(err))
			assert.Zero(t, lists.Load(), "entity/filter validation must precede filtered listing")
		})
	}
}

func TestPublicIndexQueries_AreSortedAndDeduplicated(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entityA := "acme.ops.robotics.gcs.drone.001"
	entityB := "acme.ops.robotics.gcs.drone.002"
	target := "acme.ops.robotics.gcs.mission.001"
	predicate := "robotics.status.armed"

	predicateKeys := []string{
		predicateIndexKey(predicate, entityB),
		predicateIndexKey(predicate, entityA),
		predicateIndexKey(predicate, entityB),
	}
	predicateMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return newMockKeyLister(predicateKeys), nil
	}
	incomingKeys := []string{
		incomingIndexKey(target, entityB, "robotics.assigned.mission"),
		incomingIndexKey(target, entityA, "robotics.assigned.mission"),
		incomingIndexKey(target, entityB, "robotics.assigned.mission"),
	}
	incomingMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return newMockKeyLister(incomingKeys), nil
	}

	assert.Equal(t, []string{entityA, entityB}, queryPredicateEntityIDs(t, comp, predicate))
	assert.Equal(t, []graph.IncomingEntry{
		{FromEntityID: entityA, Predicate: "robotics.assigned.mission"},
		{FromEntityID: entityB, Predicate: "robotics.assigned.mission"},
	}, queryIncomingEntries(t, comp, target))
}

func queryNameEntityIDs(t *testing.T, comp *Component, name string) []string {
	t.Helper()
	body, err := comp.handleQueryByNameNATS(context.Background(), []byte(fmt.Sprintf(`{"name":%q}`, name)))
	require.NoError(t, err)
	var response graph.QueryResponse[graph.NameData]
	require.NoError(t, json.Unmarshal(body, &response))
	ids := make([]string, 0, len(response.Data.Matches))
	for _, match := range response.Data.Matches {
		ids = append(ids, match.EntityID)
	}
	return ids
}

func queryPredicateEntityIDs(t *testing.T, comp *Component, predicate string) []string {
	t.Helper()
	body, err := comp.handleQueryPredicateNATS(context.Background(), []byte(fmt.Sprintf(`{"predicate":%q}`, predicate)))
	require.NoError(t, err)
	var response graph.PredicateQueryResponse
	require.NoError(t, json.Unmarshal(body, &response))
	return response.Data.Entities
}

func queryIncomingEntries(t *testing.T, comp *Component, entityID string) []graph.IncomingEntry {
	t.Helper()
	body, err := comp.handleQueryIncomingNATS(context.Background(), []byte(fmt.Sprintf(`{"entity_id":%q}`, entityID)))
	require.NoError(t, err)
	var response graph.IncomingQueryResponse
	require.NoError(t, json.Unmarshal(body, &response))
	return response.Data.Relationships
}

func queryPredicateNames(t *testing.T, comp *Component, namespace string) []string {
	t.Helper()
	body, err := comp.handleQueryPredicateListNATS(context.Background(),
		[]byte(fmt.Sprintf(`{"namespace":%q}`, namespace)))
	require.NoError(t, err)
	var response graph.PredicateListQueryResponse
	require.NoError(t, json.Unmarshal(body, &response))
	names := make([]string, 0, len(response.Data.Predicates))
	for _, summary := range response.Data.Predicates {
		names = append(names, summary.Predicate)
	}
	return names
}

func TestOwnerFilters_MatchOnlyDeclaredOwner(t *testing.T) {
	owner := "acme.ops.robotics.gcs.drone.001"
	other := "acme.ops.robotics.gcs.drone.002"
	target := "acme.ops.robotics.gcs.mission.001"
	predicate := "robotics.status.armed"

	tests := []struct {
		name   string
		filter string
		owned  string
		other  string
	}{
		{
			name:   "predicate",
			filter: predicateIndexEntityFilter(owner),
			owned:  predicateIndexKey(predicate, owner),
			other:  predicateIndexKey(predicate, other),
		},
		{
			name:   "name",
			filter: nameIndexEntityFilter(owner),
			owned:  nameCompositeKey(nameIndexKey("Alpha"), owner, "core.identity.name"),
			other:  nameCompositeKey(nameIndexKey("Alpha"), other, "core.identity.name"),
		},
		{
			name:   "incoming source axis",
			filter: incomingIndexSourceFilter(owner),
			owned:  incomingIndexKey(target, owner, "robotics.assigned.mission"),
			other:  incomingIndexKey(target, other, "robotics.assigned.mission"),
		},
		{
			name:   "context",
			filter: contextIndexEntityFilter(owner),
			owned:  contextIndexKey(owner, contextHashHex("source.alpha"), predicate),
			other:  contextIndexKey(other, contextHashHex("source.alpha"), predicate),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, natsSubjectFilterMatch(tt.owned, tt.filter))
			assert.False(t, natsSubjectFilterMatch(tt.other, tt.filter))
		})
	}
}

func TestOwnerFilters_MaximumEntityIDStayWithinSharedFilterContract(t *testing.T) {
	owner := "a.a.a.a.a." + strings.Repeat("e", 246)
	require.Len(t, owner, semtypes.MaxEntityIDBytes)
	require.NoError(t, semtypes.ValidateEntityID(owner))

	for name, filter := range map[string]string{
		"predicate": predicateIndexEntityFilter(owner),
		"name":      nameIndexEntityFilter(owner),
		"incoming":  incomingIndexSourceFilter(owner),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, natsclient.ValidateKVWildcardFilter(filter))
			assert.LessOrEqual(t, len(filter), natsclient.MaxKVWildcardFilterBytes)
			assert.LessOrEqual(t, len(strings.Split(filter, ".")), natsclient.MaxKVWildcardFilterTokens)
		})
	}
}

func TestOwnerReconcile_DeduplicatesFilteredResults(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mock := predicateMock(comp)
	key := predicateIndexKey("robotics.status.armed", "acme.ops.robotics.gcs.drone.001")
	mock.data[key] = predicateIndexMarker
	mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return newMockKeyLister([]string{key, key}), nil
	}

	var deletes atomic.Int64
	mock.deleteFunc = func(_ context.Context, got string, _ ...jetstream.KVDeleteOpt) error {
		assert.Equal(t, key, got)
		deletes.Add(1)
		return nil
	}

	require.NoError(t, comp.reconcileOwnedRows(context.Background(), "predicate", comp.predicateBucket,
		predicateIndexEntityFilter("acme.ops.robotics.gcs.drone.001"), nil, false))
	assert.EqualValues(t, 1, deletes.Load())
}

func TestOwnerReconcileSpike_RetractsReplacedAndEmptyMemberships(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.namePredicates = map[string]int{semantictest.Predicate(t, "core", "identity", "name"): 0} // predicate-audit:unrelated {"column":24,"surface":"go-assignment:namePredicates","value":"","basis":"reviewed component test configuration map; contained predicate is runtime-authoritative"}
	ctx := context.Background()
	entityID := "acme.ops.robotics.gcs.drone.001"
	targetA := "acme.ops.robotics.gcs.mission.001"
	targetB := "acme.ops.robotics.gcs.mission.002"

	state := graph.EntityState{ID: entityID, Triples: []message.Triple{
		{Subject: entityID, Predicate: "robotics.status.armed", Object: true, Context: "source.alpha"},
		{Subject: entityID, Predicate: "core.identity.name", Object: "Alpha"},
		{Subject: entityID, Predicate: "robotics.assigned.mission", Object: targetA},
	}}
	require.NoError(t, comp.reconcilePredicateIndex(ctx, entityID, map[string]bool{
		"robotics.status.armed": true, "core.identity.name": true, "robotics.assigned.mission": true,
	}))
	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, []nameIndexWrite{
		{name: "Alpha", predicate: "core.identity.name", priority: 0},
	}))
	require.NoError(t, comp.UpdateContextIndex(ctx, entityID, state.Triples))
	require.NoError(t, comp.reconcileIncomingIndex(ctx, entityID, map[string][]graph.IncomingEntry{
		targetA: {{FromEntityID: entityID, Predicate: "robotics.assigned.mission"}},
	}))

	oldPredicateKey := predicateIndexKey("robotics.status.armed", entityID)
	oldNameKey := nameCompositeKey(nameIndexKey("Alpha"), entityID, "core.identity.name")
	oldContextKey := contextIndexKey(entityID, contextHashHex("source.alpha"), "robotics.status.armed")
	oldIncomingKey := incomingIndexKey(targetA, entityID, "robotics.assigned.mission")

	state.Triples = []message.Triple{
		{Subject: entityID, Predicate: "robotics.status.disarmed", Object: true, Context: "source.beta"},
		{Subject: entityID, Predicate: "core.identity.name", Object: "Beta"},
		{Subject: entityID, Predicate: "robotics.assigned.mission", Object: targetB},
	}
	require.NoError(t, comp.reconcilePredicateIndex(ctx, entityID, map[string]bool{
		"robotics.status.disarmed": true, "core.identity.name": true, "robotics.assigned.mission": true,
	}))
	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, []nameIndexWrite{
		{name: "Beta", predicate: "core.identity.name", priority: 0},
	}))
	require.NoError(t, comp.UpdateContextIndex(ctx, entityID, state.Triples))
	require.NoError(t, comp.reconcileIncomingIndex(ctx, entityID, map[string][]graph.IncomingEntry{
		targetB: {{FromEntityID: entityID, Predicate: "robotics.assigned.mission"}},
	}))

	assertMockKeyAbsent(t, predicateMock(comp), oldPredicateKey)
	assertMockKeyAbsent(t, nameMock(comp), oldNameKey)
	assertMockKeyAbsent(t, contextMock(comp), oldContextKey)
	assertMockKeyAbsent(t, incomingMock(comp), oldIncomingKey)
	assertMockKeyPresent(t, predicateMock(comp), predicateIndexKey("robotics.status.disarmed", entityID))
	assertMockKeyPresent(t, nameMock(comp), nameCompositeKey(nameIndexKey("Beta"), entityID, "core.identity.name"))
	assertMockKeyPresent(t, contextMock(comp), contextIndexKey(entityID, contextHashHex("source.beta"), "robotics.status.disarmed"))
	assertMockKeyPresent(t, incomingMock(comp), incomingIndexKey(targetB, entityID, "robotics.assigned.mission"))

	require.NoError(t, comp.reconcilePredicateIndex(ctx, entityID, nil))
	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, nil))
	require.NoError(t, comp.UpdateContextIndex(ctx, entityID, nil))
	require.NoError(t, comp.reconcileIncomingIndex(ctx, entityID, nil))
	assert.Empty(t, snapshotMockKeys(predicateMock(comp)))
	assert.Empty(t, snapshotMockKeys(nameMock(comp)))
	assert.Empty(t, snapshotMockKeys(contextMock(comp)))
	assert.Empty(t, snapshotMockKeys(incomingMock(comp)))
}

func TestNameOwnerReconcile_UpdatesValueForStableKey(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	entityID := "acme.ops.robotics.gcs.drone.001"
	predicate := "core.identity.name"
	key := nameCompositeKey(nameIndexKey("Alpha"), entityID, predicate)

	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, []nameIndexWrite{
		{name: "Alpha", predicate: semantictest.Predicate(t, "core", "identity", "name"), priority: 2},
	}))
	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, []nameIndexWrite{
		{name: "ALPHA", predicate: semantictest.Predicate(t, "core", "identity", "name"), priority: 0},
	}))

	nameMock(comp).mu.Lock()
	value := append([]byte(nil), nameMock(comp).data[key]...)
	nameMock(comp).mu.Unlock()
	var got nameCompositeValue
	require.NoError(t, json.Unmarshal(value, &got))
	assert.Equal(t, nameCompositeValue{Name: "ALPHA", Priority: 0}, got)
}

func TestOwnerReconcileSpike_FilterFailureIsTransient(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entityID := "acme.ops.robotics.gcs.drone.001"
	predicateMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return nil, errors.New("filtered listing unavailable")
	}
	err := comp.reconcileOwnedRows(context.Background(), "predicate", comp.predicateBucket,
		predicateIndexEntityFilter(entityID), nil, false)
	require.Error(t, err)
	assert.True(t, errs.IsTransient(err))
}

func TestOwnerReconcileSpike_InvalidContractInputHasNoBucketIO(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ownerFilter string
		desired     map[string][]byte
		wantCode    string
		wantReason  string
	}{
		{
			name:        "invalid owner filter",
			ownerFilter: "owner..>",
			desired:     map[string][]byte{"valid.key": {}},
			wantCode:    natsclient.ErrorCodeKVFilterInvalid,
			wantReason:  natsclient.KVReasonEmptyToken,
		},
		{
			name:        "invalid desired key",
			ownerFilter: "valid.*",
			desired: map[string][]byte{
				"valid.key": {},
				"bad:key":   {},
			},
			wantCode:   natsclient.ErrorCodeKVKeyInvalid,
			wantReason: natsclient.KVReasonAlphabet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			mock := predicateMock(comp)
			var lists, puts, deletes atomic.Int64
			mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
				lists.Add(1)
				return newMockKeyLister(nil), nil
			}
			mock.putFunc = func(context.Context, string, []byte) (uint64, error) {
				puts.Add(1)
				return 1, nil
			}
			mock.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
				deletes.Add(1)
				return nil
			}

			err := comp.reconcileOwnedRows(
				context.Background(), "predicate", comp.predicateBucket, tt.ownerFilter, tt.desired, false,
			)
			require.Error(t, err)
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			assert.Equal(t, tt.wantCode, classified.Code)
			assert.Equal(t, tt.wantReason, classified.Detail[natsclient.KVDetailReason])
			assert.Zero(t, lists.Load(), "validation must precede lister creation")
			assert.Zero(t, puts.Load(), "validation must precede writes")
			assert.Zero(t, deletes.Load(), "validation must precede deletes")
		})
	}
}

func TestPredicateReconcile_InvalidPredicateHasNoBucketIO(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	membership := predicateMock(comp)
	var membershipLists, membershipPuts, membershipDeletes atomic.Int64
	membership.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		membershipLists.Add(1)
		return newMockKeyLister(nil), nil
	}
	membership.putFunc = func(context.Context, string, []byte) (uint64, error) {
		membershipPuts.Add(1)
		return 1, nil
	}
	membership.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		membershipDeletes.Add(1)
		return nil
	}
	err := comp.reconcilePredicateIndex(context.Background(), "acme.ops.robotics.gcs.drone.001",
		map[string]bool{"bad:key": true})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Zero(t, membershipLists.Load())
	assert.Zero(t, membershipPuts.Load())
	assert.Zero(t, membershipDeletes.Load())
}

func TestPredicateReconcile_OverBoundEntityHasNoBucketIO(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	membership := predicateMock(comp)
	var lists, membershipPuts, membershipDeletes atomic.Int64
	membership.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		lists.Add(1)
		return newMockKeyLister(nil), nil
	}
	membership.putFunc = func(context.Context, string, []byte) (uint64, error) {
		membershipPuts.Add(1)
		return 1, nil
	}
	membership.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		membershipDeletes.Add(1)
		return nil
	}

	entityID := "a.a.a.a.a." + strings.Repeat("e", 247)
	require.Len(t, entityID, semtypes.MaxEntityIDBytes+1)
	err := comp.reconcilePredicateIndex(context.Background(), entityID, map[string]bool{"robotics.status.armed": true})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDInvalid, classified.Code)
	assert.Equal(t, semtypes.EntityIDReasonBytes, classified.Detail[semtypes.EntityIDDetailReason])
	assert.Zero(t, lists.Load())
	assert.Zero(t, membershipPuts.Load())
	assert.Zero(t, membershipDeletes.Load())
}

func TestBenchmarkReconcile_OverBoundEntityAxesHaveNoBucketIO(t *testing.T) {
	invalid := "a.a.a.a.a." + strings.Repeat("e", 247)
	valid := "acme.ops.robotics.gcs.drone.001"
	require.Len(t, invalid, semtypes.MaxEntityIDBytes+1)

	tests := []struct {
		name   string
		bucket func(*Component) *mockKVBucket
		run    func(*Component) error
	}{
		{
			name:   "name owner",
			bucket: nameMock,
			run: func(comp *Component) error {
				return comp.reconcileNameIndex(context.Background(), invalid, nil)
			},
		},
		{
			name:   "incoming source",
			bucket: incomingMock,
			run: func(comp *Component) error {
				return comp.reconcileIncomingIndex(context.Background(), invalid, nil)
			},
		},
		{
			name:   "incoming target",
			bucket: incomingMock,
			run: func(comp *Component) error {
				return comp.reconcileIncomingIndex(context.Background(), valid, map[string][]graph.IncomingEntry{invalid: nil})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			mock := tt.bucket(comp)
			var lists, puts, deletes atomic.Int64
			mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
				lists.Add(1)
				return newMockKeyLister(nil), nil
			}
			mock.putFunc = func(context.Context, string, []byte) (uint64, error) {
				puts.Add(1)
				return 1, nil
			}
			mock.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
				deletes.Add(1)
				return nil
			}

			err := tt.run(comp)
			require.Error(t, err)
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			assert.Equal(t, semtypes.ErrorCodeEntityIDInvalid, classified.Code)
			assert.Equal(t, semtypes.EntityIDReasonBytes, classified.Detail[semtypes.EntityIDDetailReason])
			assert.Zero(t, lists.Load())
			assert.Zero(t, puts.Load())
			assert.Zero(t, deletes.Load())
		})
	}
}

func snapshotMockKeys(mock *mockKVBucket) []string {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	keys := make([]string, 0, len(mock.data))
	for key := range mock.data {
		keys = append(keys, key)
	}
	return keys
}

func assertMockKeyPresent(t *testing.T, mock *mockKVBucket, key string) {
	t.Helper()
	mock.mu.Lock()
	defer mock.mu.Unlock()
	_, ok := mock.data[key]
	assert.True(t, ok, "expected key %q", key)
}

func assertMockKeyAbsent(t *testing.T, mock *mockKVBucket, key string) {
	t.Helper()
	mock.mu.Lock()
	defer mock.mu.Unlock()
	_, ok := mock.data[key]
	assert.False(t, ok, "unexpected key %q", key)
}
