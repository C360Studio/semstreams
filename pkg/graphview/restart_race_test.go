package graphview

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// stopRecordingWatcher is a fake watcher that records Stop() so a test can
// prove a successfully opened watcher was released, not leaked.
type stopRecordingWatcher struct {
	updates  chan jetstream.KeyValueEntry
	stopOnce sync.Once
	stopped  chan struct{}
}

func newStopRecordingWatcher() *stopRecordingWatcher {
	return &stopRecordingWatcher{
		updates: make(chan jetstream.KeyValueEntry, 8),
		stopped: make(chan struct{}),
	}
}

func (w *stopRecordingWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *stopRecordingWatcher) Stop() error {
	w.stopOnce.Do(func() { close(w.stopped) })
	return nil
}

// restartRaceSource serves a normal watcher on the first WatchAll and BLOCKS
// the second WatchAll until released — then returns success. It scripts the
// exact interleaving where Stop completes while a Restart's WatchAll I/O is
// still in flight.
type restartRaceSource struct {
	mu      sync.Mutex
	calls   int
	first   *fakeWatcher
	second  *stopRecordingWatcher
	entered chan struct{}
	release chan struct{}
}

func (s *restartRaceSource) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		return s.first, nil
	}
	close(s.entered)
	<-s.release
	return s.second, nil
}

// TestStopRacingRestartDoesNotLeakWatcher is the openWatcher state-recheck
// regression: Stop wins while a Restart's WatchAll is still in flight; when
// the WatchAll then returns SUCCESS, the view must stop the just-opened
// watcher (no zombie goroutine holding a live NATS consumer past Stop) and
// Restart must return the state error — never report a successful restart of
// a stopped view. Fully scripted: the fake source blocks in WatchAll until
// after Stop completed.
func TestStopRacingRestartDoesNotLeakWatcher(t *testing.T) {
	src := &restartRaceSource{
		first:   newFakeWatcher(),
		second:  newStopRecordingWatcher(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	lost := make(chan error, 1)
	view, err := New[string](src, decodeTest, WithHooks(Hooks{
		OnWatcherLost: func(err error) { lost <- err },
	}))
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	t.Cleanup(view.Stop)

	// Healthy generation one, then watcher loss -> stateFailed.
	src.first.updates <- nil
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	require.NoError(t, view.WaitCaughtUp(ctx))
	close(src.first.updates)
	select {
	case <-lost:
	case <-time.After(testWait):
		t.Fatal("timed out waiting for watcher loss")
	}

	// Restart blocks inside the source's WatchAll.
	restartErr := make(chan error, 1)
	go func() { restartErr <- view.Restart() }()
	select {
	case <-src.entered:
	case <-time.After(testWait):
		t.Fatal("timed out waiting for Restart to enter WatchAll")
	}

	// Stop completes while the WatchAll I/O is still in flight (nothing in
	// the WaitGroup belongs to the in-flight open).
	view.Stop()

	// The blocked WatchAll now returns success — after the view stopped.
	close(src.release)

	select {
	case err := <-restartErr:
		require.Error(t, err, "restart of a stopped view must not report success")
		require.ErrorIs(t, err, ErrViewStopped, "restart must surface the state error")
	case <-time.After(testWait):
		t.Fatal("timed out waiting for Restart to return")
	}

	// The successfully opened watcher was stopped, not leaked as a zombie.
	select {
	case <-src.second.stopped:
	case <-time.After(testWait):
		t.Fatal("just-opened watcher was never stopped — zombie NATS consumer past Stop()")
	}

	// The view stays terminally stopped.
	_, _, err = view.Get("k")
	require.ErrorIs(t, err, ErrViewStopped)
}
