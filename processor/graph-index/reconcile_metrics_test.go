package graphindex

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestReconcileMetrics_RecordListPutDeleteOutcomes(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	owner := "acme.ops.robotics.gcs.drone.001"
	stale := predicateIndexKey("robotics.status.armed", owner)
	desiredKey := predicateIndexKey("robotics.status.disarmed", owner)
	predicateMock(comp).data[stale] = predicateIndexMarker

	listSuccess := comp.metrics.reconcileOperations.WithLabelValues("predicate", "list", "success")
	deleteSuccess := comp.metrics.reconcileOperations.WithLabelValues("predicate", "delete", "success")
	putSuccess := comp.metrics.reconcileOperations.WithLabelValues("predicate", "put", "success")
	listAttempts := comp.metrics.kvOperations.WithLabelValues("list", "predicate")
	deleteAttempts := comp.metrics.kvOperations.WithLabelValues("delete", "predicate")
	putAttempts := comp.metrics.kvOperations.WithLabelValues("put", "predicate")
	listBefore := testutil.ToFloat64(listSuccess)
	deleteBefore := testutil.ToFloat64(deleteSuccess)
	putBefore := testutil.ToFloat64(putSuccess)
	listAttemptsBefore := testutil.ToFloat64(listAttempts)
	deleteAttemptsBefore := testutil.ToFloat64(deleteAttempts)
	putAttemptsBefore := testutil.ToFloat64(putAttempts)

	require.NoError(t, comp.reconcileOwnedRows(ctx, "predicate", comp.predicateBucket,
		predicateIndexEntityFilter(owner), map[string][]byte{desiredKey: predicateIndexMarker}, false))
	require.Equal(t, listBefore+1, testutil.ToFloat64(listSuccess))
	require.Equal(t, deleteBefore+1, testutil.ToFloat64(deleteSuccess))
	require.Equal(t, putBefore+1, testutil.ToFloat64(putSuccess))
	require.Equal(t, listAttemptsBefore+1, testutil.ToFloat64(listAttempts))
	require.Equal(t, deleteAttemptsBefore+1, testutil.ToFloat64(deleteAttempts))
	require.Equal(t, putAttemptsBefore+1, testutil.ToFloat64(putAttempts))
}

func TestReconcileMetrics_SemanticUpdatesCountOnceAfterRetry(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entityID := "acme.ops.robotics.gcs.drone.003"
	predicate := predicateMock(comp) // predicate-audit:unrelated {"column":15,"surface":"go-assignment:predicate","value":"","basis":"reviewed mock predicate-index bucket returned for failure injection"}
	remaining := 1
	predicate.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if remaining > 0 {
			remaining--
			return 0, errors.New("transient put failure")
		}
		predicate.data[key] = value
		return 1, nil
	}

	before := make(map[string]float64)
	for _, indexType := range []string{"name", "predicate", "incoming", "outgoing"} {
		before[indexType] = testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType))
	}
	require.NoError(t, comp.processEntityUpdateFromData(context.Background(), entityID,
		entityStateData(t, entityID, "acme.ops.robotics.gcs.mission.003")))
	for _, indexType := range []string{"name", "predicate", "incoming", "outgoing"} {
		require.Equal(t, before[indexType]+1,
			testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType)), indexType)
	}
}

func TestReconcileMetrics_CleanDeleteRecordsOneSemanticCompletionPerIndex(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entityID := "acme.ops.robotics.gcs.drone.004"
	before := make(map[string]float64)
	for _, indexType := range []string{"name", "predicate", "incoming", "outgoing"} {
		before[indexType] = testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType))
	}
	eventsBefore := testutil.ToFloat64(comp.metrics.eventsProcessed)
	deletesBefore := testutil.ToFloat64(comp.metrics.watchEvents.WithLabelValues("delete"))

	require.NoError(t, comp.DeleteFromIndexes(context.Background(), entityID))
	for _, indexType := range []string{"name", "predicate", "incoming", "outgoing"} {
		require.Equal(t, before[indexType]+1,
			testutil.ToFloat64(comp.metrics.indexUpdates.WithLabelValues(indexType)), indexType)
	}
	require.Equal(t, eventsBefore+1, testutil.ToFloat64(comp.metrics.eventsProcessed))
	require.Equal(t, deletesBefore+1, testutil.ToFloat64(comp.metrics.watchEvents.WithLabelValues("delete")))
}

func TestReconcileMetrics_RecordBackendFailures(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	owner := "acme.ops.robotics.gcs.drone.002"
	mock := predicateMock(comp)

	listFailure := comp.metrics.reconcileOperations.WithLabelValues("predicate", "list", "failure")
	listFailureBefore := testutil.ToFloat64(listFailure)
	mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return nil, errors.New("list unavailable")
	}
	require.Error(t, comp.reconcileOwnedRows(ctx, "predicate", comp.predicateBucket,
		predicateIndexEntityFilter(owner), nil, false))
	require.Equal(t, listFailureBefore+1, testutil.ToFloat64(listFailure))

	stale := predicateIndexKey("robotics.status.armed", owner)
	desiredKey := predicateIndexKey("robotics.status.disarmed", owner)
	mock.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
		return newMockKeyLister([]string{stale}), nil
	}
	mock.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		return errors.New("delete unavailable")
	}
	mock.putFunc = func(context.Context, string, []byte) (uint64, error) {
		return 0, errors.New("put unavailable")
	}
	deleteFailure := comp.metrics.reconcileOperations.WithLabelValues("predicate", "delete", "failure")
	putFailure := comp.metrics.reconcileOperations.WithLabelValues("predicate", "put", "failure")
	deleteBefore := testutil.ToFloat64(deleteFailure)
	putBefore := testutil.ToFloat64(putFailure)

	require.Error(t, comp.reconcileOwnedRows(ctx, "predicate", comp.predicateBucket,
		predicateIndexEntityFilter(owner), map[string][]byte{desiredKey: predicateIndexMarker}, false))
	require.Equal(t, deleteBefore+1, testutil.ToFloat64(deleteFailure))
	require.Equal(t, putBefore+1, testutil.ToFloat64(putFailure))
}
