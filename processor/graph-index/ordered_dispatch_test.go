package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type orderedTestEntry struct {
	key      string
	value    []byte
	revision uint64
	op       jetstream.KeyValueOp
}

func (e *orderedTestEntry) Key() string                     { return e.key }
func (e *orderedTestEntry) Value() []byte                   { return e.value }
func (e *orderedTestEntry) Revision() uint64                { return e.revision }
func (e *orderedTestEntry) Created() time.Time              { return time.Time{} }
func (e *orderedTestEntry) Delta() uint64                   { return 0 }
func (e *orderedTestEntry) Operation() jetstream.KeyValueOp { return e.op }
func (e *orderedTestEntry) Bucket() string                  { return graph.BucketEntityStates }

func entityStateData(t *testing.T, id, target string) []byte {
	t.Helper()
	state := graph.EntityState{ID: id}
	if target != "" {
		state.Triples = []message.Triple{{
			Subject: id, Predicate: "core.relationship.related", Object: target,
		}}
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	return data
}

func startOrderedTestPool(t *testing.T, comp *Component, workers int) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	comp.config.Workers = workers
	require.NoError(t, comp.startIndexPool(ctx))
	t.Cleanup(func() {
		cancel()
		require.NoError(t, comp.indexPool.Stop(time.Second))
	})
	return ctx, cancel
}

func TestOrderedDispatcher_UpdateCannotFinishAfterDelete(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.watermark = revlag.New()
	ctx, _ := startOrderedTestPool(t, comp, 4)

	entityID := "acme.ops.robotics.gcs.drone.001"
	targetID := "acme.ops.robotics.gcs.mission.001"
	stateData := entityStateData(t, entityID, targetID)
	states := newMockKVBucket()
	var entityDeleted atomic.Bool
	states.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		if entityDeleted.Load() {
			return nil, jetstream.ErrKeyNotFound
		}
		return &orderedTestEntry{key: key, value: stateData, revision: 1}, nil
	}
	comp.entityStatesBucket = states
	entered := make(chan struct{})
	release := make(chan struct{})
	deleted := make(chan struct{})
	var firstPut atomic.Bool
	outgoing := outgoingMock(comp)
	outgoing.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if firstPut.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		outgoing.mu.Lock()
		outgoing.data[key] = value
		outgoing.mu.Unlock()
		return 1, nil
	}
	outgoing.deleteFunc = func(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
		outgoing.mu.Lock()
		delete(outgoing.data, key)
		outgoing.mu.Unlock()
		close(deleted)
		return nil
	}

	comp.watermark.Observe(1, entityID)
	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexUpdate, completionRevision: 1,
	}))
	<-entered

	entityDeleted.Store(true)
	comp.watermark.Observe(2, entityID)
	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexDelete, completionRevision: 2,
	}))
	select {
	case <-deleted:
		t.Fatal("same-key delete ran concurrently with the blocked update")
	case <-time.After(25 * time.Millisecond):
	}
	require.Equal(t, uint64(0), comp.watermark.Indexed(), "readiness advanced while ordered work was blocked")

	close(release)
	select {
	case <-deleted:
	case <-time.After(time.Second):
		t.Fatal("ordered delete did not run")
	}
	require.Eventually(t, func() bool { return comp.watermark.Indexed() == 2 }, time.Second, time.Millisecond)
	outgoing.mu.Lock()
	_, exists := outgoing.data[entityID]
	outgoing.mu.Unlock()
	require.False(t, exists, "older update resurrected the entity after delete")
}

func TestAuthoritativeReconcilePreventsLateOlderWatcherClobber(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.watermark = revlag.New()
	ctx, _ := startOrderedTestPool(t, comp, 4)

	entityID := "acme.ops.robotics.gcs.drone.003"
	newTarget := "acme.ops.robotics.gcs.mission.002"
	latest := entityStateData(t, entityID, newTarget)
	var gets atomic.Int64
	states := newMockKVBucket()
	states.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		gets.Add(1)
		return &orderedTestEntry{key: key, value: latest, revision: 2}, nil
	}
	comp.entityStatesBucket = states

	// Repair/coalesced reconciliation applies authoritative R2 before the watcher
	// gets CPU to submit the already captured R1 entry.
	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexReconcile,
	}))
	require.Eventually(t, func() bool { return gets.Load() == 1 }, time.Second, time.Millisecond)

	comp.watermark.Observe(1, entityID)
	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexUpdate, completionRevision: 1,
	}))
	require.Eventually(t, func() bool { return comp.watermark.Indexed() == 1 }, time.Second, time.Millisecond,
		"late R1 work did not reach terminal stale-skip completion")

	outgoing := outgoingMock(comp)
	outgoing.mu.Lock()
	var got []graph.OutgoingEntry
	require.NoError(t, json.Unmarshal(outgoing.data[entityID], &got))
	outgoing.mu.Unlock()
	require.Len(t, got, 1)
	require.Equal(t, newTarget, got[0].ToEntityID)
	require.Equal(t, int64(2), gets.Load(), "late R1 must re-read authoritative R2 instead of applying its snapshot")
}

func TestRepairRefetchesAtOrderedExecution(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx, _ := startOrderedTestPool(t, comp, 3)

	entityID := "acme.ops.robotics.gcs.drone.002"
	oldTarget := "acme.ops.robotics.gcs.mission.001"
	newTarget := "acme.ops.robotics.gcs.mission.002"
	entered := make(chan struct{})
	release := make(chan struct{})
	var firstPut atomic.Bool
	outgoing := outgoingMock(comp)
	outgoing.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if firstPut.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		outgoing.mu.Lock()
		outgoing.data[key] = value
		outgoing.mu.Unlock()
		return 1, nil
	}

	latest := entityStateData(t, entityID, newTarget)
	old := entityStateData(t, entityID, oldTarget)
	states := newMockKVBucket()
	var gets atomic.Int64
	states.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		if gets.Add(1) == 1 {
			return &orderedTestEntry{key: key, value: old, revision: 1}, nil
		}
		return &orderedTestEntry{key: key, value: latest, revision: 2}, nil
	}
	comp.entityStatesBucket = states

	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexUpdate, completionRevision: 1,
	}))
	<-entered
	comp.markEntityFailed(entityID)
	comp.repairFailedEntities(ctx)

	require.Equal(t, int64(1), gets.Load(), "repair fetched before older same-key work completed")
	close(release)

	require.Eventually(t, func() bool {
		outgoing.mu.Lock()
		defer outgoing.mu.Unlock()
		var got []graph.OutgoingEntry
		if json.Unmarshal(outgoing.data[entityID], &got) != nil || len(got) != 1 {
			return false
		}
		return got[0].ToEntityID == newTarget && comp.failedCount.Load() == 0 && gets.Load() == 2
	}, time.Second, time.Millisecond, "repair must fetch and index current KV state at execution")
}

func TestOlderDeleteCannotClearFailedNewerAuthoritativeState(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx, _ := startOrderedTestPool(t, comp, 4)

	entityID := "acme.ops.robotics.gcs.drone.004"
	latest := entityStateData(t, entityID, "acme.ops.robotics.gcs.mission.002")
	states := newMockKVBucket()
	states.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		return &orderedTestEntry{key: key, value: latest, revision: 2}, nil
	}
	comp.entityStatesBucket = states

	var writes atomic.Int64
	var deletes atomic.Int64
	outgoing := outgoingMock(comp)
	outgoing.putFunc = func(context.Context, string, []byte) (uint64, error) {
		writes.Add(1)
		return 0, errors.New("persistent write failure")
	}
	outgoing.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		deletes.Add(1)
		return nil
	}

	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexReconcile, completionRevision: 2,
	}))
	require.Eventually(t, func() bool { return comp.failedCount.Load() == 1 && writes.Load() >= 3 }, time.Second, time.Millisecond)

	// A delayed R1 tombstone is older than authoritative R2. It must reconcile R2,
	// not execute a delete that clears the failed marker and advertises readiness.
	require.NoError(t, comp.submitEntityWork(ctx, entityIndexWork{
		entityID: entityID, kind: entityIndexDelete, completionRevision: 1,
	}))
	require.Eventually(t, func() bool { return writes.Load() >= 6 }, time.Second, time.Millisecond)
	require.Equal(t, int64(0), deletes.Load(), "older tombstone deleted newer authoritative state")
	require.Equal(t, int64(1), comp.failedCount.Load(), "older tombstone cleared newer failure marker")
}

func TestKeyedDispatcher_AllowsDifferentEntitiesToRunConcurrently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	d := newKeyedDispatcher(8, 32, func(s string) string { return s }, func(_ context.Context, s string) {
		started <- s
		<-release
	})
	d.Start(ctx)
	require.NoError(t, d.Submit(ctx, "entity-a"))
	require.NoError(t, d.Submit(ctx, "entity-b"))
	first := <-started
	select {
	case second := <-started:
		require.NotEqual(t, first, second)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("different entity keys did not execute concurrently")
	}
	close(release)
	cancel()
	require.NoError(t, d.Stop(time.Second))
}
