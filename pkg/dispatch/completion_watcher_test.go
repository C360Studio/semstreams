package dispatch

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type completionTestWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped chan struct{}
	once    sync.Once
}

func (w *completionTestWatcher) Updates() <-chan jetstream.KeyValueEntry {
	return w.updates
}

func (w *completionTestWatcher) Stop() error {
	w.once.Do(func() { close(w.stopped) })
	return nil
}

type completionTestEntry struct {
	key string
}

func (e completionTestEntry) Bucket() string                  { return "TEST" }
func (e completionTestEntry) Key() string                     { return e.key }
func (e completionTestEntry) Value() []byte                   { return nil }
func (e completionTestEntry) Revision() uint64                { return 1 }
func (e completionTestEntry) Created() time.Time              { return time.Time{} }
func (e completionTestEntry) Delta() uint64                   { return 0 }
func (e completionTestEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

func TestCompletionWatcherRunUsesLifecycleContext(t *testing.T) {
	type contextKey struct{}
	runCtx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "run-value"))

	callbackCtx := make(chan context.Context, 1)
	w := &completionWatcher[string]{
		keyFor: func(work string) string { return work },
		onComplete: func(ctx context.Context, _ string) error {
			callbackCtx <- ctx
			return nil
		},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		inFlight: map[string]string{"work": "work"},
	}
	watcher := &completionTestWatcher{
		updates: make(chan jetstream.KeyValueEntry, 1),
		stopped: make(chan struct{}),
	}

	w.wg.Add(1)
	go w.run(runCtx, watcher)
	watcher.updates <- completionTestEntry{key: "work"}

	select {
	case got := <-callbackCtx:
		if got != runCtx {
			t.Fatal("completion callback did not receive the exact watcher lifecycle context")
		}
		require.Equal(t, "run-value", got.Value(contextKey{}))
	case <-time.After(2 * time.Second):
		t.Fatal("completion callback did not run")
	}

	cancel()
	select {
	case <-watcher.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("completion watcher did not stop after lifecycle context cancellation")
	}
	w.wg.Wait()
}
