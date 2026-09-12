package graphview

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// stopRecordingWatcher records cleanup of a successful but canceled acquisition.
type stopRecordingWatcher struct {
	updates  chan jetstream.KeyValueEntry
	stopOnce sync.Once
	stopped  chan struct{}
}

func newStopRecordingWatcher() *stopRecordingWatcher {
	return &stopRecordingWatcher{updates: make(chan jetstream.KeyValueEntry, 8), stopped: make(chan struct{})}
}

func (w *stopRecordingWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *stopRecordingWatcher) Stop() error {
	w.stopOnce.Do(func() { close(w.stopped) })
	return nil
}

type heldWatchSource struct {
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	watcher  *stopRecordingWatcher
}

func (s *heldWatchSource) WatchAll(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.canceled)
	<-s.release
	return s.watcher, nil
}

// spec: graph-view-subscription / View lifecycle and ownership
func TestStopJoinsWatcherAcquisition(t *testing.T) {
	source := &heldWatchSource{make(chan struct{}), make(chan struct{}), make(chan struct{}), newStopRecordingWatcher()}
	view, err := New[string](source, decodeTest)
	require.NoError(t, err)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(source.release) }) }
	t.Cleanup(func() { release(); view.Stop() })
	started := make(chan error, 1)
	go func() { started <- view.Start(t.Context()) }()
	waitJoinSignal(t, source.entered)
	stopped := make(chan struct{})
	go func() { view.Stop(); close(stopped) }()
	waitJoinSignal(t, source.canceled)
	// Cancellation is observed by the held acquisition. The small window
	// detects an early return, not a readiness assumption or scheduling delay.
	select {
	case <-stopped:
		t.Error("Stop returned before admitted WatchAll acquisition joined")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-started:
		require.ErrorIs(t, err, ErrViewStopped)
	case <-time.After(testWait):
		t.Fatal("Start did not join its canceled acquisition")
	}
	waitJoinSignal(t, stopped)
	select {
	case <-source.watcher.stopped:
	default:
		t.Fatal("Stop returned before late watcher cleanup")
	}
}

// spec: graph-view-subscription / View lifecycle and ownership
func TestStopJoinsSubscriberCallback(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscribe", true: "snapshot"}[snapshot], func(t *testing.T) {
			watcher := newFakeWatcher()
			entered, release := make(chan struct{}), make(chan struct{})
			view, err := New[string](&fakeSource{queue: []*fakeWatcher{watcher}}, decodeTest, WithHooks(Hooks{
				OnSubscribers: func(n int) {
					if n == 1 {
						close(entered)
						<-release
					}
				},
			}))
			require.NoError(t, err)
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(func() { unblock(); view.Stop() })
			require.NoError(t, view.Start(t.Context()))
			watcher.updates <- nil
			require.NoError(t, view.WaitCaughtUp(t.Context()))
			attached := make(chan struct{})
			go func() {
				if snapshot {
					_, _, _ = view.SnapshotAndSubscribe(t.Context())
				} else {
					_, _ = view.Subscribe(t.Context())
				}
				close(attached)
			}()
			waitJoinSignal(t, entered)
			stopped := make(chan struct{})
			go func() { view.Stop(); close(stopped) }()
			select {
			case <-stopped:
				t.Error("Stop returned before admitted subscriber callback joined")
			case <-time.After(50 * time.Millisecond):
			}
			unblock()
			waitJoinSignal(t, attached)
			waitJoinSignal(t, stopped)
		})
	}
}

func waitJoinSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(testWait):
		t.Fatal("timed out waiting for lifecycle handshake")
	}
}
