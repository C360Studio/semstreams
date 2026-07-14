package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	serrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type lifecycleAtomicWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped atomic.Bool
}

func newLifecycleAtomicWatcher() *lifecycleAtomicWatcher {
	return &lifecycleAtomicWatcher{updates: make(chan jetstream.KeyValueEntry, 16)}
}

func (w *lifecycleAtomicWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *lifecycleAtomicWatcher) Stop() error {
	w.stopped.Store(true)
	return nil
}

func TestLifecycleWatchBootstrapIsAtomicAcrossPredicatePoisonOrdering(t *testing.T) {
	t.Parallel()
	validID := "acme.ops.lifecycle.gcs.mission.valid"
	poisonID := "acme.ops.lifecycle.gcs.mission.poison"
	valid := validLifecycleWatchEntry(t, validID, 1)
	poison := &fakeKVEntry{key: poisonID, value: poisonedLifecycleState(poisonID), revision: 2, created: time.Now()}

	for _, tc := range []struct {
		name    string
		entries []jetstream.KeyValueEntry
	}{
		{name: "valid then poison", entries: []jetstream.KeyValueEntry{valid, poison}},
		{name: "poison then valid", entries: []jetstream.KeyValueEntry{poison, valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _, bucket := newTestManager(t)
			guard := newLifecycleAtomicWatcher()
			pattern := newLifecycleAtomicWatcher()
			bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return guard, nil }
			bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return pattern, nil }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			delivered, err := mgr.WatchEvents(ctx, "fixture")
			if err != nil {
				t.Fatalf("WatchEvents: %v", err)
			}
			pattern.updates <- valid
			pattern.updates <- nil
			for _, entry := range tc.entries {
				guard.updates <- entry
			}
			waitForLifecycleWatch(t, func() bool { return mgr.graphStatePoison.Load() != nil })
			select {
			case got, ok := <-delivered:
				if ok {
					t.Fatalf("partial bootstrap delivery = %#v, want none", got)
				}
			case <-time.After(time.Second):
				t.Fatal("workflow watch did not close after guard poison")
			}
			mgr.graphStateGuardCancel()
			mgr.graphStateGuardWG.Wait()
		})
	}
}

func TestLifecycleWatchLivePoisonClosesProjectionGate(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	markLifecycleGuardClean(mgr)
	reg, err := mgr.lookupByWorkflow("fixture")
	if err != nil {
		t.Fatal(err)
	}
	watcher := newLifecycleAtomicWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	delivered := make(chan Event, 4)
	go mgr.runWatchLoop(ctx, reg, watcher,
		func(id string, p Participant) bool {
			delivered <- Event{Op: Upserted, EntityID: id, Participant: p}
			return true
		},
		nil)

	bootstrapID := "acme.ops.lifecycle.gcs.mission.bootstrap"
	watcher.updates <- validLifecycleWatchEntry(t, bootstrapID, 1)
	watcher.updates <- nil
	select {
	case got := <-delivered:
		if got.EntityID != bootstrapID {
			t.Fatalf("bootstrap entity = %q, want %q", got.EntityID, bootstrapID)
		}
	case <-time.After(time.Second):
		t.Fatal("clean bootstrap was not released")
	}

	poisonID := "acme.ops.lifecycle.gcs.mission.poison"
	watcher.updates <- &fakeKVEntry{key: poisonID, value: poisonedLifecycleState(poisonID), revision: 2, created: time.Now()}
	watcher.updates <- validLifecycleWatchEntry(t, "acme.ops.lifecycle.gcs.mission.after", 3)
	waitForLifecycleWatch(t, func() bool { return mgr.graphStatePoison.Load() != nil })
	select {
	case got := <-delivered:
		t.Fatalf("delivery after live poison = %#v, want none", got)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestLifecycleWatchRevisionBarrierBlocksLaterValidBehindEarlierPoison(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	pattern := newLifecycleAtomicWatcher()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return guard, nil }
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return pattern, nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := mgr.WatchEvents(ctx, "fixture")
	if err != nil {
		t.Fatalf("WatchEvents: %v", err)
	}
	guard.updates <- nil
	waitForLifecycleWatch(t, func() bool {
		result := mgr.graphStateGuardResult.Load()
		return result != nil && result.clean
	})

	// The pattern subscription is deliberately scheduled ahead of the
	// authoritative subscription. Revision 3 must not escape until the guard
	// has validated through revision 3; revision 2 is poison, so it must never
	// escape at all.
	pattern.updates <- validLifecycleWatchEntry(t, "acme.ops.lifecycle.gcs.mission.after", 3)
	select {
	case got := <-out:
		t.Fatalf("revision 3 escaped its authoritative barrier: %#v", got)
	case <-time.After(30 * time.Millisecond):
	}
	poisonID := "acme.ops.other.gcs.device.poison"
	guard.updates <- &fakeKVEntry{
		key: poisonID, value: poisonedLifecycleState(poisonID), revision: 2, created: time.Now(),
	}
	waitForLifecycleWatch(t, func() bool { return mgr.graphStatePoison.Load() != nil })
	select {
	case got, ok := <-out:
		if ok {
			t.Fatalf("revision 3 delivered after earlier poison: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("workflow watch did not close after earlier authoritative poison")
	}
	mgr.graphStateGuardCancel()
	mgr.graphStateGuardWG.Wait()
}

func TestLifecycleWatchUnexpectedTransportCloseDoesNotLatchResetRequired(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	markLifecycleGuardClean(mgr)
	reg, err := mgr.lookupByWorkflow("fixture")
	if err != nil {
		t.Fatal(err)
	}
	watcher := newLifecycleAtomicWatcher()
	delivered := make(chan Event, 1)
	done := make(chan struct{})
	go func() {
		mgr.runWatchLoop(context.Background(), reg, watcher,
			func(id string, p Participant) bool {
				delivered <- Event{Op: Upserted, EntityID: id, Participant: p}
				return true
			}, nil)
		close(done)
	}()

	close(watcher.updates)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch loop did not stop after unexpected close")
	}
	if mgr.graphStatePoison.Load() != nil {
		t.Fatal("transport close was misclassified as graph-state poison")
	}
	select {
	case got := <-delivered:
		t.Fatalf("partial bootstrap delivery = %#v, want none", got)
	default:
	}
}

func TestLifecycleWatcherStartFailuresAreTransientIndexNotReady(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		configure func(*fakeBucket)
	}{
		{
			name: "authoritative guard",
			configure: func(bucket *fakeBucket) {
				bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) {
					return nil, errors.New("guard unavailable")
				}
			},
		},
		{
			name: "workflow pattern",
			configure: func(bucket *fakeBucket) {
				bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) {
					return newLifecycleAtomicWatcher(), nil
				}
				bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) {
					return nil, errors.New("pattern unavailable")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _, bucket := newTestManager(t)
			tc.configure(bucket)
			_, err := mgr.Watch(context.Background(), "fixture")
			if err == nil || !serrs.IsTransient(err) {
				t.Fatalf("Watch error = %T %v, want transient", err, err)
			}
			var classified *serrs.ClassifiedError
			if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeIndexNotReady {
				t.Fatalf("Watch error = %v, want code %q", err, graph.ErrorCodeIndexNotReady)
			}
			mgr.graphStateGuardCancel()
			mgr.graphStateGuardWG.Wait()
		})
	}
}

func TestLifecycleWatchCloseAfterCancellationDoesNotReportPoison(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	markLifecycleGuardClean(mgr)
	reg, err := mgr.lookupByWorkflow("fixture")
	if err != nil {
		t.Fatal(err)
	}
	watcher := newLifecycleAtomicWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.runWatchLoop(ctx, reg, watcher, func(string, Participant) bool { return true }, nil)
		close(done)
	}()

	cancel()
	close(watcher.updates)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch loop did not stop after cancellation")
	}
	if mgr.graphStatePoison.Load() != nil {
		t.Fatal("normal cancellation was misreported as graph poison")
	}
}

func TestLifecycleManagerUsesOneAuthoritativeGuardForManyWorkflowWatches(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	var watchAllCalls atomic.Int64
	var patternCalls atomic.Int64
	var patterns []string
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) {
		watchAllCalls.Add(1)
		return guard, nil
	}
	bucket.watchFactory = func(pattern string) (jetstream.KeyWatcher, error) {
		patternCalls.Add(1)
		patterns = append(patterns, pattern)
		return newLifecycleAtomicWatcher(), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, first, err := mgr.startWatch(ctx, "fixture", "Watch")
	if err != nil {
		t.Fatalf("first startWatch: %v", err)
	}
	defer first.Stop()
	_, second, err := mgr.startWatch(ctx, "fixture", "WatchEvents")
	if err != nil {
		t.Fatalf("second startWatch: %v", err)
	}
	defer second.Stop()

	if got := watchAllCalls.Load(); got != 1 {
		t.Fatalf("WatchAll calls = %d, want one shared authoritative scan", got)
	}
	if got := patternCalls.Load(); got != 2 {
		t.Fatalf("pattern Watch calls = %d, want one per subscriber", got)
	}
	for i, pattern := range patterns {
		if pattern != "*.*.lifecycle.gcs.mission.*" {
			t.Fatalf("pattern[%d] = %q, want workflow EntityIDPattern", i, pattern)
		}
	}

	guard.updates <- nil
	waitForLifecycleWatch(t, func() bool {
		result := mgr.graphStateGuardResult.Load()
		return result != nil && result.clean
	})
	mgr.graphStateGuardCancel()
	mgr.graphStateGuardWG.Wait()
}

func TestLifecycleManagerConcurrentWatchesStillOpenOneAuthoritativeGuard(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	var watchAllCalls atomic.Int64
	var patternCalls atomic.Int64
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) {
		watchAllCalls.Add(1)
		return guard, nil
	}
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) {
		patternCalls.Add(1)
		return newLifecycleAtomicWatcher(), nil
	}

	const subscribers = 16
	type result struct {
		watcher jetstream.KeyWatcher
		err     error
	}
	results := make(chan result, subscribers)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range subscribers {
		go func() {
			_, watcher, err := mgr.startWatch(ctx, "fixture", "Watch")
			results <- result{watcher: watcher, err: err}
		}()
	}
	for range subscribers {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent startWatch: %v", got.err)
		}
		if err := got.watcher.Stop(); err != nil {
			t.Fatalf("stop pattern watcher: %v", err)
		}
	}
	if got := watchAllCalls.Load(); got != 1 {
		t.Fatalf("concurrent WatchAll calls = %d, want one", got)
	}
	if got := patternCalls.Load(); got != subscribers {
		t.Fatalf("concurrent pattern Watch calls = %d, want %d", got, subscribers)
	}
	mgr.graphStateGuardCancel()
	mgr.graphStateGuardWG.Wait()
}

func TestLifecycleSharedGuardPoisonOutsideWorkflowBlocksBufferedProjection(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	pattern := newLifecycleAtomicWatcher()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return guard, nil }
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return pattern, nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := mgr.Watch(ctx, "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	pattern.updates <- validLifecycleWatchEntry(t, "acme.ops.lifecycle.gcs.mission.buffered", 1)

	outsideID := "acme.ops.other.gcs.device.poison"
	guard.updates <- &fakeKVEntry{
		key: outsideID, value: poisonedLifecycleState(outsideID), revision: 2, created: time.Now(),
	}
	waitForLifecycleWatch(t, func() bool { return mgr.graphStatePoison.Load() != nil })
	select {
	case participant, ok := <-out:
		if ok {
			t.Fatalf("projection escaped before shared guard completed: %#v", participant)
		}
	case <-time.After(time.Second):
		t.Fatal("workflow watch did not close after shared-guard poison")
	}
	mgr.graphStateGuardCancel()
	mgr.graphStateGuardWG.Wait()
}

func TestLifecycleSharedGuardNormalShutdownClosesWatchesWithoutPoison(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	pattern := newLifecycleAtomicWatcher()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return guard, nil }
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return pattern, nil }

	out, err := mgr.Watch(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	done := make(chan struct{})
	go func() {
		mgr.WaitOwnership()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Manager shutdown did not join shared graph-state guard")
	}
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("watch delivered during normal Manager shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("workflow watch did not close on shared-guard shutdown")
	}
	if mgr.graphStatePoison.Load() != nil {
		t.Fatal("normal shared-guard shutdown was misreported as graph poison")
	}
}

func TestLifecycleSharedGuardTransportCloseIsTransientDegraded(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	guard := newLifecycleAtomicWatcher()
	pattern := newLifecycleAtomicWatcher()
	bucket.watchAllFactory = func() (jetstream.KeyWatcher, error) { return guard, nil }
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return pattern, nil }

	out, err := mgr.Watch(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	close(guard.updates)
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("watch delivered after shared-guard transport failure")
		}
	case <-time.After(time.Second):
		t.Fatal("workflow watch did not close after shared-guard transport failure")
	}
	if mgr.graphStatePoison.Load() != nil {
		t.Fatal("shared-guard transport close was misclassified as reset poison")
	}
	if mgr.graphStateGuardDegraded.Load() == nil {
		t.Fatal("shared-guard transport close did not mark Manager degraded")
	}

	_, err = mgr.Watch(context.Background(), "fixture")
	if err == nil || !serrs.IsTransient(err) {
		t.Fatalf("Watch after degradation = %T %v, want transient", err, err)
	}
	var classified *serrs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeIndexNotReady {
		t.Fatalf("Watch after degradation = %v, want code %q", err, graph.ErrorCodeIndexNotReady)
	}
	mgr.graphStateGuardCancel()
	mgr.graphStateGuardWG.Wait()
}

func validLifecycleWatchEntry(t *testing.T, entityID string, revision uint64) jetstream.KeyValueEntry {
	t.Helper()
	data, err := graph.MarshalEntityState(&graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "planning",
		}},
	})
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	return &fakeKVEntry{key: entityID, value: data, revision: revision, created: time.Now()}
}

func waitForLifecycleWatch(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for watcher state")
}

func markLifecycleGuardClean(mgr *Manager) {
	mgr.publishGraphStateGuardReady(true)
	mgr.graphStateGuardRevision.Store(^uint64(0))
}
