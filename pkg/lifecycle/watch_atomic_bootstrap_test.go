package lifecycle

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
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

func TestLifecycleMatchingWatchPoisonDoesNotBlockUnrelatedExactRead(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)

	validID := "acme.ops.gcs.lifecycle.mission.valid-b"
	bucket.put(validID, validLifecycleState(validID))

	watcher := newLifecycleAtomicWatcher()
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return watcher, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := mgr.Watch(ctx, "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	poisonID := "acme.ops.gcs.lifecycle.mission.poison-a"
	watcher.updates <- poisonLifecycleWatchEntry(poisonID, 2)
	requireValueChannelClosed(t, out, "matching poison subscription")

	participant, err := mgr.Get(context.Background(), "fixture", validID)
	if err != nil {
		t.Fatalf("unrelated exact read after matching watch poison: %v", err)
	}
	if participant.EntityID() != validID {
		t.Fatalf("unrelated exact read entity = %q, want %q", participant.EntityID(), validID)
	}
}

func TestLifecyclePoisonClosesOnlyMatchingSubscription(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	other := lifecycle{}.fixtureWorkflow()
	other.Name = "other"
	other.EntityIDPattern = "*.*.other.gcs.mission.*"
	if err := mgr.Register(other); err != nil {
		t.Fatalf("Register other: %v", err)
	}

	fixtureWatcher := newLifecycleAtomicWatcher()
	otherWatcher := newLifecycleAtomicWatcher()
	bucket.watchFactory = func(pattern string) (jetstream.KeyWatcher, error) {
		switch pattern {
		case "*.*.gcs.lifecycle.mission.*":
			return fixtureWatcher, nil
		case "*.*.other.gcs.mission.*":
			return otherWatcher, nil
		default:
			t.Fatalf("unexpected watch pattern %q", pattern)
			return nil, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixtureOut, err := mgr.Watch(ctx, "fixture")
	if err != nil {
		t.Fatalf("Watch fixture: %v", err)
	}
	otherOut, err := mgr.Watch(ctx, "other")
	if err != nil {
		t.Fatalf("Watch other: %v", err)
	}

	poisonID := "acme.ops.gcs.lifecycle.mission.poison-a"
	fixtureWatcher.updates <- poisonLifecycleWatchEntry(poisonID, 2)
	requireValueChannelClosed(t, fixtureOut, "fixture poison subscription")

	validID := "acme.ops.other.gcs.mission.valid-b"
	otherWatcher.updates <- validLifecycleWatchEntry(t, validID, 3)
	select {
	case participant := <-otherOut:
		if participant == nil || participant.EntityID() != validID {
			t.Fatalf("other subscription participant = %#v, want %q", participant, validID)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated subscription did not continue after matching poison")
	}
}

func TestLifecycleWatchEventsPoisonEmitsNoEvent(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	watcher := newLifecycleAtomicWatcher()
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return watcher, nil }
	out, err := mgr.WatchEvents(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("WatchEvents: %v", err)
	}
	poisonID := "acme.ops.gcs.lifecycle.mission.poison-a"
	watcher.updates <- poisonLifecycleWatchEntry(poisonID, 7)
	select {
	case event, ok := <-out:
		if ok {
			t.Fatalf("poison emitted event %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("WatchEvents did not close after matching poison")
	}
}

func TestLifecycleWatchPoisonWarningNamesSubscriptionAndAuthorityEntryOnce(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	bucket := newFakeBucket()
	mgr := newManagerForTest(logger, &fakeEmitter{bucket: bucket}, bucket)
	if err := mgr.Register(lifecycle{}.fixtureWorkflow()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	watcher := newLifecycleAtomicWatcher()
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return watcher, nil }

	out, err := mgr.Watch(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	poisonID := "acme.ops.gcs.lifecycle.mission.poison-a"
	watcher.updates <- poisonLifecycleWatchEntry(poisonID, 41)
	watcher.updates <- poisonLifecycleWatchEntry(poisonID, 42)
	requireValueChannelClosed(t, out, "poison subscription")

	const message = "lifecycle workflow watch encountered poisoned graph state; closing subscription"
	logged := logs.String()
	if got := strings.Count(logged, message); got != 1 {
		t.Fatalf("poison warning count = %d, want 1; logs=%s", got, logged)
	}
	for _, fragment := range []string{
		`"level":"WARN"`,
		`"workflow":"fixture"`,
		`"entity":"` + poisonID + `"`,
		`"revision":41`,
		`"code":"` + graph.ErrorCodeGraphStateResetRequired + `"`,
		`"reason":"` + string(graph.GraphStateReasonNoncanonicalPredicate) + `"`,
	} {
		if !strings.Contains(logged, fragment) {
			t.Fatalf("poison warning missing %s; logs=%s", fragment, logged)
		}
	}
}

func TestLifecycleWatchTransportCloseIsLocalAndLaterSubscriptionWorks(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	first := newLifecycleAtomicWatcher()
	second := newLifecycleAtomicWatcher()
	var calls atomic.Int64
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) {
		if calls.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}

	firstOut, err := mgr.Watch(context.Background(), "fixture")
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	close(first.updates)
	requireValueChannelClosed(t, firstOut, "transport-closed subscription")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondOut, err := mgr.Watch(ctx, "fixture")
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}
	validID := "acme.ops.gcs.lifecycle.mission.valid-b"
	second.updates <- validLifecycleWatchEntry(t, validID, 2)
	select {
	case participant := <-secondOut:
		if participant == nil || participant.EntityID() != validID {
			t.Fatalf("later subscription participant = %#v, want %q", participant, validID)
		}
	case <-time.After(time.Second):
		t.Fatal("later subscription did not work after transport close")
	}
}

func TestLifecycleWatchCancellationIsQuiet(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	bucket := newFakeBucket()
	mgr := newManagerForTest(logger, &fakeEmitter{bucket: bucket}, bucket)
	if err := mgr.Register(lifecycle{}.fixtureWorkflow()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	watcher := newLifecycleAtomicWatcher()
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) { return watcher, nil }
	ctx, cancel := context.WithCancel(context.Background())
	out, err := mgr.Watch(ctx, "fixture")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	requireValueChannelClosed(t, out, "canceled subscription")
	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("cancellation emitted warning: %s", logs.String())
	}
}

func TestLifecycleWatcherStartFailuresAreTransientIndexNotReady(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	bucket.watchFactory = func(string) (jetstream.KeyWatcher, error) {
		return nil, context.DeadlineExceeded
	}
	_, err := mgr.Watch(context.Background(), "fixture")
	if err == nil || !serrs.IsTransient(err) {
		t.Fatalf("Watch error = %T %v, want transient", err, err)
	}
}

func validLifecycleState(entityID string) *graph.EntityState {
	return &graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "planning",
		}},
	}
}

func validLifecycleWatchEntry(t *testing.T, entityID string, revision uint64) jetstream.KeyValueEntry {
	t.Helper()
	data, err := graph.MarshalEntityState(validLifecycleState(entityID))
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	return &fakeKVEntry{key: entityID, value: data, revision: revision, created: time.Now()}
}

func poisonLifecycleWatchEntry(entityID string, revision uint64) jetstream.KeyValueEntry {
	return &fakeKVEntry{
		key: entityID, value: poisonedLifecycleState(entityID), revision: revision, created: time.Now(),
	}
}

func requireValueChannelClosed(t *testing.T, out <-chan Participant, name string) {
	t.Helper()
	select {
	case participant, ok := <-out:
		if ok {
			t.Fatalf("%s emitted %#v", name, participant)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s did not close", name)
	}
}
