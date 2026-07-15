package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	comp.namePredicates = map[string]int{"core.identity.name": 0}
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
		{name: "Alpha", predicate: predicate, priority: 2},
	}))
	require.NoError(t, comp.reconcileNameIndex(ctx, entityID, []nameIndexWrite{
		{name: "ALPHA", predicate: predicate, priority: 0},
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

func TestPredicateReconcile_InvalidCatalogKeyHasNoBucketIO(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	membership := predicateMock(comp)
	catalog := predicateCatalogMock(comp)
	var membershipLists, membershipPuts, membershipDeletes atomic.Int64
	var catalogLists, catalogPuts, catalogDeletes atomic.Int64
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
	catalog.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		catalogLists.Add(1)
		return newMockKeyLister(nil), nil
	}
	catalog.putFunc = func(context.Context, string, []byte) (uint64, error) {
		catalogPuts.Add(1)
		return 1, nil
	}
	catalog.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		catalogDeletes.Add(1)
		return nil
	}

	err := comp.reconcilePredicateIndexRows(context.Background(), "acme.ops.robotics.gcs.drone.001", []predicateReconcileRow{
		{predicate: "robotics.status.armed", catalogKey: "bad:key"},
	})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, natsclient.ErrorCodeKVKeyInvalid, classified.Code)
	assert.Equal(t, natsclient.KVReasonAlphabet, classified.Detail[natsclient.KVDetailReason])
	assert.Zero(t, membershipLists.Load())
	assert.Zero(t, membershipPuts.Load())
	assert.Zero(t, membershipDeletes.Load())
	assert.Zero(t, catalogLists.Load())
	assert.Zero(t, catalogPuts.Load())
	assert.Zero(t, catalogDeletes.Load())

	// The catalog writer independently preserves the same pre-Put guarantee.
	err = comp.updatePredicateCatalog(context.Background(), "bad:key")
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, natsclient.ErrorCodeKVKeyInvalid, classified.Code)
	assert.Zero(t, catalogPuts.Load())
}

func TestPredicateReconcile_OverBoundEntityHasNoBucketIO(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	membership := predicateMock(comp)
	catalog := predicateCatalogMock(comp)
	var lists, membershipPuts, membershipDeletes, catalogPuts atomic.Int64
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
	catalog.putFunc = func(context.Context, string, []byte) (uint64, error) {
		catalogPuts.Add(1)
		return 1, nil
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
	assert.Zero(t, catalogPuts.Load())
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
