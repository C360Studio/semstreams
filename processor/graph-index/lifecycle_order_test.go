package graphindex

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponentStopRetainsCoalescerUntilWatcherJoins(t *testing.T) {
	comp, coalescer, watcher := newBlockedStopOwner(t)

	stopDone := make(chan error, 1)
	go func() { stopDone <- comp.Stop(context.Background()) }()

	select {
	case <-watcher.stopStarted:
	case <-time.After(time.Second):
		t.Fatal("entity watcher did not enter shutdown")
	}

	comp.mu.RLock()
	retained := comp.entityCoalescer
	running := comp.running
	comp.mu.RUnlock()
	assert.Same(t, coalescer, retained,
		"Stop detached the coalescer before the lock-free watcher joined")
	assert.True(t, running, "Stop reported terminal state before the watcher join was observed")

	watcher.release()
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete after watcher release")
	}
	comp.mu.RLock()
	defer comp.mu.RUnlock()
	require.Same(t, coalescer, comp.entityCoalescer,
		"successful Stop must not detach immutable Start-owned resources")
	require.False(t, comp.running)
}

func TestComponentStopDeadlineBeforeWatcherJoinPerformsNoTerminalCleanup(t *testing.T) {
	comp, coalescer, watcher := newBlockedStopOwner(t)

	stopCtx, cancelStop := context.WithCancel(context.Background())
	stopDone := make(chan error, 1)
	go func() { stopDone <- comp.Stop(stopCtx) }()
	select {
	case <-watcher.stopStarted:
	case <-time.After(time.Second):
		t.Fatal("entity watcher did not observe runtime cancellation")
	}
	cancelStop()
	select {
	case err := <-stopDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Stop did not return when its context won the watcher join")
	}

	comp.mu.RLock()
	require.True(t, comp.running, "unobserved join was reported as terminal completion")
	require.Same(t, coalescer, comp.entityCoalescer,
		"deadline path detached the coalescer before the watcher joined")
	comp.mu.RUnlock()
	require.Equal(t, int64(1), atomic.LoadInt64(&comp.errors),
		"failed Stop must make canceled runtime health non-healthy")
	require.False(t, comp.Health().Healthy,
		"failed Stop must not leave canceled runtime health healthy")

	startErr := comp.Start(context.Background())
	assert.Error(t, startErr, "Start reused a run already claimed by failed Stop")
	if startErr != nil {
		assert.Contains(t, startErr.Error(), "one-shot lifecycle")
	}
	secondStop := make(chan error, 1)
	go func() { secondStop <- comp.Stop(context.Background()) }()
	select {
	case err := <-secondStop:
		require.NoError(t, err, "later Stop after failed one-shot claim must be a no-op")
	case <-time.After(time.Second):
		t.Error("later Stop attempted to rejoin the failed one-shot owner")
	}

	watcher.release()
	select {
	case <-comp.runDone:
	case <-time.After(time.Second):
		t.Fatal("watcher did not exit after release")
	}

	comp.mu.RLock()
	defer comp.mu.RUnlock()
	require.True(t, comp.running,
		"Stop continued terminal cleanup after returning the unproven join")
	require.Same(t, coalescer, comp.entityCoalescer,
		"Stop detached resources asynchronously after its deadline")
}

func TestComponentCompletedStopIsNoOpAndCannotBeRestarted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	require.NoError(t, comp.Initialize())
	var cancelCalls atomic.Int32
	comp.runCancel = func() { cancelCalls.Add(1) }
	comp.runDone = make(chan struct{})
	close(comp.runDone)
	comp.running = true

	require.NoError(t, comp.Stop(context.Background()))
	require.Equal(t, int32(1), cancelCalls.Load())
	require.NoError(t, comp.Stop(context.Background()))
	require.Equal(t, int32(1), cancelCalls.Load(),
		"completed repeated Stop repeated lifecycle work")

	err := comp.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "one-shot lifecycle")
}

func newBlockedStopOwner(t *testing.T) (*Component, *revisionCoalescer, *blockingStopWatcher) {
	t.Helper()
	comp := createTestComponentWithMockKV(t)
	require.NoError(t, comp.Initialize())
	runCtx, cancel := context.WithCancel(context.Background())
	coalescer := newRevisionCoalescer(runCtx, time.Hour, func([]coalescedEntity) {})
	comp.entityCoalescer = coalescer

	watcher := &blockingStopWatcher{
		updates:      make(chan jetstream.KeyValueEntry),
		watchStarted: make(chan struct{}),
		stopStarted:  make(chan struct{}),
		stopRelease:  make(chan struct{}),
	}
	t.Cleanup(watcher.release)
	comp.wg.Add(1)
	go comp.watchEntityStates(runCtx, &blockingStopBucket{
		mockKVBucket: newMockKVBucket(),
		watcher:      watcher,
	})
	select {
	case <-watcher.watchStarted:
	case <-time.After(time.Second):
		t.Fatal("entity watcher did not start")
	}

	comp.runCancel = cancel
	comp.runDone = make(chan struct{})
	go func() {
		comp.wg.Wait()
		close(comp.runDone)
	}()
	comp.running = true
	return comp, coalescer, watcher
}

type blockingStopWatcher struct {
	updates      chan jetstream.KeyValueEntry
	watchStarted chan struct{}
	stopStarted  chan struct{}
	stopRelease  chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
	releaseOnce  sync.Once
}

func (w *blockingStopWatcher) Updates() <-chan jetstream.KeyValueEntry {
	w.startOnce.Do(func() { close(w.watchStarted) })
	return w.updates
}

func (w *blockingStopWatcher) Stop() error {
	w.stopOnce.Do(func() {
		close(w.stopStarted)
		<-w.stopRelease
	})
	return nil
}

func (w *blockingStopWatcher) release() {
	w.releaseOnce.Do(func() { close(w.stopRelease) })
}

type blockingStopBucket struct {
	*mockKVBucket
	watcher jetstream.KeyWatcher
}

func (b *blockingStopBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return b.watcher, nil
}
