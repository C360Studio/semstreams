package rule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type transactionalTestWatcher struct {
	updates chan jetstream.KeyValueEntry
	// stopped is atomic: Stop() is invoked both by the production watcher
	// goroutine (handleEntityUpdatesForKey's explicit pre-return
	// Stop) and by test goroutines — an unsynchronized bool here is a
	// genuine data race the detector fires on under CI scheduling.
	stopped atomic.Bool
	stopErr error
}

func newTransactionalTestWatcher() *transactionalTestWatcher {
	return &transactionalTestWatcher{updates: make(chan jetstream.KeyValueEntry, 4)}
}

func (w *transactionalTestWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *transactionalTestWatcher) Stop() error {
	w.stopped.Store(true)
	return w.stopErr
}

func TestUpdateWatchBucketsCommitsDesiredSetWhenOldStopFails(t *testing.T) {
	t.Parallel()

	const (
		oldPatternOne = "acme.old.one.*.*.*"
		oldPatternTwo = "acme.old.two.*.*.*"
		newPattern    = "acme.new.*.*.*.*"
	)
	oldOne := newTransactionalTestWatcher()
	oldTwo := newTransactionalTestWatcher()
	oldTwo.stopErr = errors.New("injected physical stop failure")
	newWatcher := newTransactionalTestWatcher()
	oldOneKey := watcherKey(gtypes.BucketEntityStates, oldPatternOne)
	oldTwoKey := watcherKey(gtypes.BucketEntityStates, oldPatternTwo)
	newKey := watcherKey(gtypes.BucketEntityStates, newPattern)
	_, cancelOldOne := context.WithCancel(context.Background())
	_, cancelOldTwo := context.WithCancel(context.Background())
	cfg := mustTestConfig(t, "watcher-stop-failure-test")
	cfg.EntityWatchBuckets = map[string][]string{
		gtypes.BucketEntityStates: {oldPatternOne, oldPatternTwo},
	}
	processor := &Processor{
		logger:     slog.Default(),
		config:     &cfg,
		watcherCtx: context.Background(),
		entityWatcherMap: map[string]jetstream.KeyWatcher{
			oldOneKey: oldOne,
			oldTwoKey: oldTwo,
		},
		entityWatcherCancels: map[string]context.CancelFunc{
			oldOneKey: cancelOldOne,
			oldTwoKey: cancelOldTwo,
		},
		entityDispatchRecords: map[string]managedEntityWatcher{
			oldOneKey: {watcher: oldOne, generation: 1},
			oldTwoKey: {watcher: oldTwo, generation: 2},
		},
		entityNextGeneration: 2,
		entityWatchers:       []jetstream.KeyWatcher{oldOne, oldTwo},
	}

	err := processor.updateWatchBucketsWithFactory(
		map[string][]string{gtypes.BucketEntityStates: {newPattern}},
		func(_ context.Context, _, _ string) (jetstream.KeyWatcher, error) { return newWatcher, nil },
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected physical stop failure")
	require.Equal(t, map[string][]string{gtypes.BucketEntityStates: {newPattern}}, processor.config.EntityWatchBuckets)
	require.Equal(t, map[string]jetstream.KeyWatcher{newKey: newWatcher}, processor.entityWatcherMap)
	require.NotNil(t, processor.entityWatcherCancels[newKey])
	require.NotContains(t, processor.entityWatcherCancels, oldOneKey)
	require.NotContains(t, processor.entityWatcherCancels, oldTwoKey)
	require.True(t, oldOne.stopped.Load())
	require.True(t, oldTwo.stopped.Load())

	// The generation fence rejects a retired watcher even if its transport did
	// not stop. It cannot reach decoding or dispatch.
	done := make(chan struct{})
	go func() {
		processor.handleEntityUpdatesForKey(
			context.Background(), oldTwo, oldTwoKey, 2,
		)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retired watcher callback did not exit")
	}
	require.Zero(t, processor.messagesEvaluated)

	processor.entityWatcherCancels[newKey]()
}

func TestUpdateWatchBucketsRollsBackPreparedAdditions(t *testing.T) {
	t.Parallel()

	const oldPattern = "acme.old.*.*.*.*"
	oldWatcher := newTransactionalTestWatcher()
	preparedWatcher := newTransactionalTestWatcher()
	originalConfig := map[string][]string{gtypes.BucketEntityStates: {oldPattern}}
	cfg := mustTestConfig(t, "watcher-rollback-test")
	cfg.EntityWatchBuckets = cloneEntityWatchBuckets(originalConfig)
	processor := &Processor{
		logger:           slog.Default(),
		config:           &cfg,
		watcherCtx:       context.Background(),
		entityWatcherMap: map[string]jetstream.KeyWatcher{watcherKey(gtypes.BucketEntityStates, oldPattern): oldWatcher},
		entityWatchers:   []jetstream.KeyWatcher{oldWatcher},
	}

	call := 0
	prepare := func(_ context.Context, bucket, _ string) (jetstream.KeyWatcher, error) {
		call++
		if bucket != gtypes.BucketEntityStates {
			return nil, fmt.Errorf("unexpected bucket %s", bucket)
		}
		if call == 1 {
			return preparedWatcher, nil
		}
		return nil, errors.New("injected second watcher failure")
	}

	err := processor.updateWatchBucketsWithFactory(map[string][]string{
		gtypes.BucketEntityStates: {"acme.new.*.*.*.*", "acme.second.*.*.*.*"},
	}, prepare)
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected second watcher failure")
	require.True(t, preparedWatcher.stopped.Load(), "prepared addition must be rolled back")
	require.False(t, oldWatcher.stopped.Load(), "old watcher must remain active")
	require.Equal(t, originalConfig, processor.config.EntityWatchBuckets)
	require.Equal(t, map[string]jetstream.KeyWatcher{
		watcherKey(gtypes.BucketEntityStates, oldPattern): oldWatcher,
	}, processor.entityWatcherMap)
	require.Equal(t, []jetstream.KeyWatcher{oldWatcher}, processor.entityWatchers)
}

func TestRetiredGenerationCannotDispatchDecodedUpdate(t *testing.T) {
	t.Parallel()

	const (
		oldPattern = "acme.prod.robotics.*.drone.*"
		newPattern = "acme.prod.environmental.*.sensor.*"
		entityID   = "acme.prod.robotics.gcs.drone.stale"
	)
	processor := newAtomicBootstrapProcessor(t)
	rule := &patternSelectionRule{name: "robotics"}
	processor.rules = map[string]Rule{"robotics": rule}
	processor.ruleDefinitions = map[string]Definition{
		"robotics": {ID: "robotics", Entity: EntityConfig{Pattern: oldPattern}},
	}

	oldWatcher := newTransactionalTestWatcher()
	newWatcher := newTransactionalTestWatcher()
	oldKey := watcherKey(gtypes.BucketEntityStates, oldPattern)
	newKey := watcherKey(gtypes.BucketEntityStates, newPattern)
	oldCtx, oldCancel := context.WithCancel(context.Background())
	processor.watcherCtx = context.Background()
	processor.config.EntityWatchBuckets = map[string][]string{
		gtypes.BucketEntityStates: {oldPattern},
	}
	processor.entityWatchers = []jetstream.KeyWatcher{oldWatcher}
	processor.entityWatcherMap = map[string]jetstream.KeyWatcher{oldKey: oldWatcher}
	processor.entityWatcherCancels = map[string]context.CancelFunc{oldKey: oldCancel}
	processor.entityDispatchRecords = map[string]managedEntityWatcher{
		oldKey: {watcher: oldWatcher, generation: 1},
	}
	processor.entityNextGeneration = 1

	eligible := make(chan struct{})
	release := make(chan struct{})
	processor.entityBeforeDispatch = func() {
		close(eligible)
		<-release
	}
	handlerDone := make(chan struct{})
	go func() {
		processor.handleManagedEntityUpdates(oldCtx, oldWatcher, oldKey, 1)
		close(handlerDone)
	}()
	oldWatcher.updates <- validRuleEntityEntry(t, entityID, 7)

	select {
	case <-eligible:
	case <-time.After(time.Second):
		t.Fatal("old generation did not reach final dispatch eligibility")
	}

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- processor.updateWatchBucketsWithFactory(
			map[string][]string{gtypes.BucketEntityStates: {newPattern}},
			func(_ context.Context, _, _ string) (jetstream.KeyWatcher, error) {
				return newWatcher, nil
			},
		)
	}()
	select {
	case err := <-updateDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("retirement could not commit while stale callback paused before generation gate")
	}
	require.NotContains(t, processor.entityDispatchRecords, oldKey)
	require.Contains(t, processor.entityDispatchRecords, newKey)

	close(release)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("retired callback did not exit after generation check")
	}
	require.Zero(t, rule.evaluated.Load(), "retired decoded update must not reach rule evaluation")

	processor.entityWatcherCancels[newKey]()
	require.NoError(t, newWatcher.Stop())
}

func TestStaleDeleteDoesNotPurgeNewerPendingWatcherWork(t *testing.T) {
	t.Parallel()

	const entityID = "acme.prod.robotics.gcs.drone.recreated"
	processor := newAtomicBootstrapProcessor(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor.entityCoalescer = cache.NewCoalescingSet(ctx, time.Hour, func([]string) {})
	defer processor.entityCoalescer.Close()

	// A fast watcher has already advanced the per-entity revision fence while
	// an overlapping provenance still owns queued work for the recreation.
	entry := processor.entityEvaluationFence.retain(entityID)
	entry.mu.Lock()
	entry.record(entitySnapshot{Action: "UPDATED", Revision: 12})
	entry.mu.Unlock()
	processor.entityEvaluationFence.release(entityID, entry)

	provenance := &entityWatchProvenance{key: "ENTITY_STATES|acme.prod.robotics.*.drone.*", generation: 2}
	pendingKey := encodeEntityWatchPendingKey(entityID, *provenance)
	processor.dispatchEntityWatchUpdateWithProvenance(ctx, entityWatchUpdate{
		entityKey: entityID,
		snapshot: entitySnapshot{
			Action:   "UPDATED",
			Revision: 13,
			State:    &gtypes.EntityState{ID: entityID},
		},
	}, false, provenance)
	require.Equal(t, 1, processor.entityCoalescer.PendingCount())

	// A lagging overlapping watcher delivers an older delete. It must fail the
	// revision fence before touching coalesced work owned by the newer put.
	processor.dispatchEntityWatchUpdate(ctx, entityWatchUpdate{
		entityKey: entityID,
		snapshot:  entitySnapshot{Action: "DELETED", Revision: 11},
	}, false)
	require.Equal(t, 1, processor.entityCoalescer.PendingCount(),
		"stale delete purged the recreated entity's pending evaluation")

	require.True(t, processor.entityCoalescer.Remove(pendingKey))
	processor.entityEvaluationFence.releaseRetained(entityID, 1)
}

// TestUpdateWatchBucketsToZeroPatternsRetiresAllWatchers proves the dynamic
// leg of the zero-pattern contract: reconfiguring to zero entity-watch
// patterns retires every ENTITY_STATES watcher and prepares none — the
// factory is never invoked, so the processor holds zero watchers afterward.
func TestUpdateWatchBucketsToZeroPatternsRetiresAllWatchers(t *testing.T) {
	t.Parallel()

	const oldPattern = "acme.old.*.*.*.*"
	oldWatcher := newTransactionalTestWatcher()
	oldKey := watcherKey(gtypes.BucketEntityStates, oldPattern)
	_, cancelOld := context.WithCancel(context.Background())
	cfg := mustTestConfig(t, "watcher-zero-pattern-test")
	cfg.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {oldPattern}}
	processor := &Processor{
		logger:               slog.Default(),
		config:               &cfg,
		watcherCtx:           context.Background(),
		entityWatcherMap:     map[string]jetstream.KeyWatcher{oldKey: oldWatcher},
		entityWatcherCancels: map[string]context.CancelFunc{oldKey: cancelOld},
		entityDispatchRecords: map[string]managedEntityWatcher{
			oldKey: {watcher: oldWatcher, generation: 1},
		},
		entityNextGeneration: 1,
		entityWatchers:       []jetstream.KeyWatcher{oldWatcher},
	}

	factoryCalls := 0
	err := processor.updateWatchBucketsWithFactory(
		map[string][]string{},
		func(context.Context, string, string) (jetstream.KeyWatcher, error) {
			factoryCalls++
			return newTransactionalTestWatcher(), nil
		},
	)
	require.NoError(t, err)
	require.Zero(t, factoryCalls, "zero patterns must prepare no ENTITY_STATES watchers")
	require.True(t, oldWatcher.stopped.Load(), "existing watcher must be retired")
	require.Empty(t, processor.entityWatcherMap)
	require.Empty(t, processor.entityWatchers)
	require.Empty(t, processor.entityDispatchRecords)
}

func TestUpdateWatchBucketsRejectsDuplicatePatternsBeforePrepare(t *testing.T) {
	t.Parallel()

	cfg := mustTestConfig(t, "watcher-duplicate-test")
	original := map[string][]string{gtypes.BucketEntityStates: {"acme.old.*.*.*.*"}}
	cfg.EntityWatchBuckets = cloneEntityWatchBuckets(original)
	processor := &Processor{
		logger:           slog.Default(),
		config:           &cfg,
		watcherCtx:       context.Background(),
		entityWatcherMap: make(map[string]jetstream.KeyWatcher),
	}
	prepareCalls := 0
	err := processor.updateWatchBucketsWithFactory(
		map[string][]string{
			gtypes.BucketEntityStates: {
				"acme.new.*.*.*.*",
				"acme.new.*.*.*.*",
			},
		},
		func(_ context.Context, _, _ string) (jetstream.KeyWatcher, error) {
			prepareCalls++
			return newTransactionalTestWatcher(), nil
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate pattern")
	require.Zero(t, prepareCalls)
	require.Equal(t, original, processor.config.EntityWatchBuckets)
	require.Empty(t, processor.entityWatcherMap)
	require.Empty(t, processor.entityWatcherCancels)
	require.Empty(t, processor.entityWatchers)
}
