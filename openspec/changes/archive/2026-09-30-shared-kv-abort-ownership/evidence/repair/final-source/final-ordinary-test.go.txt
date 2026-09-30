package natsclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownershipBucket models the SDK's cancellable KeyLister forwarder and its
// separate native Updates sender. A canceled forwarder cannot complete that
// sender; only consuming Updates can.
type ownershipBucket struct {
	jetstream.KeyValue
	entered   chan struct{}
	attempted chan struct{}
	release   chan struct{}
	watcher   *ownershipWatcher
}

type ownershipWatcher struct {
	updates           chan jetstream.KeyValueEntry
	stopped           chan struct{}
	postStopAttempted chan struct{}
	done              chan struct{}
	once              sync.Once
	calls             atomic.Int32
}

func newOwnershipBucket() *ownershipBucket {
	return &ownershipBucket{
		entered:   make(chan struct{}),
		attempted: make(chan struct{}),
		release:   make(chan struct{}),
		watcher: &ownershipWatcher{
			updates:           make(chan jetstream.KeyValueEntry),
			stopped:           make(chan struct{}),
			postStopAttempted: make(chan struct{}),
			done:              make(chan struct{}),
		},
	}
}

func (b *ownershipBucket) WatchFiltered(_ context.Context, _ []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	close(b.entered)
	go func() {
		defer close(b.watcher.done)
		close(b.attempted)
		b.watcher.updates <- ownershipEntry{key: "diag.one"}
		// A second native delivery is demanded only after Stop is observed.
		// Collection cannot satisfy this post-Stop obligation by chance.
		<-b.watcher.stopped
		close(b.watcher.postStopAttempted)
		b.watcher.updates <- ownershipEntry{key: "diag.after-stop"}
		close(b.watcher.updates)
	}()
	<-b.release
	return b.watcher, nil
}

func (b *ownershipBucket) ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error) {
	watcher, err := b.WatchFiltered(ctx, filters, jetstream.IgnoreDeletes(), jetstream.MetaOnly())
	if err != nil {
		return nil, err
	}
	keys := make(chan string)
	go func() {
		defer close(keys)
		defer watcher.Stop()
		// This is the native forwarder's cancellation branch: it may close
		// Keys without consuming a pending Updates send.
		if ctx.Err() != nil {
			return
		}
		select {
		case entry := <-watcher.Updates():
			if entry != nil {
				keys <- entry.Key()
			}
		case <-ctx.Done():
		}
	}()
	return ownershipLister{keys: keys, watcher: watcher}, nil
}

type ownershipLister struct {
	keys    <-chan string
	watcher jetstream.KeyWatcher
}

func (l ownershipLister) Keys() <-chan string { return l.keys }
func (l ownershipLister) Stop() error         { return l.watcher.Stop() }

func (w *ownershipWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *ownershipWatcher) Stop() error {
	w.calls.Add(1)
	w.once.Do(func() { close(w.stopped) })
	return nil
}

type ownershipEntry struct {
	jetstream.KeyValueEntry
	key string
}

func (e ownershipEntry) Key() string { return e.key }

func TestKVStoreFilteredCancellationFinishesNativeDelivery(t *testing.T) {
	bucket := newOwnershipBucket()
	ctx, cancel := context.WithCancel(t.Context())
	callDone := make(chan struct{})
	result := make(chan struct {
		keys []string
		err  error
	}, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(bucket.release) }) }
	defer func() {
		// Recovery is test-owned and occurs only after a terminal assertion or
		// failure; it cannot make the production result pass.
		cancel()
		release()
		_ = bucket.watcher.Stop()
		select {
		case <-bucket.watcher.done:
		default:
			rescueLimit := time.NewTimer(2 * time.Second)
		rescue:
			for {
				select {
				case _, ok := <-bucket.watcher.Updates():
					if !ok {
						break rescue
					}
				case <-rescueLimit.C:
					t.Error("test-owned Updates rescue did not finish")
					break rescue
				}
			}
			rescueLimit.Stop()
		}
		select {
		case <-bucket.watcher.done:
		case <-time.After(2 * time.Second):
			t.Error("test-owned producer did not join")
		}
		select {
		case <-callDone:
		case <-time.After(2 * time.Second):
			t.Error("public listing task did not join")
		}
	}()

	go func() {
		defer close(callDone)
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(ctx, "diag.>")
		result <- struct {
			keys []string
			err  error
		}{keys, err}
	}()
	select {
	case <-bucket.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("filtered constructor was not entered")
	}
	select {
	case <-bucket.attempted:
	case <-time.After(2 * time.Second):
		t.Fatal("native Updates send was not attempted")
	}
	cancel()
	release()
	var got struct {
		keys []string
		err  error
	}
	select {
	case got = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("public listing did not return after cancellation")
	}
	require.ErrorIs(t, got.err, context.Canceled)
	assert.Nil(t, got.keys)
	assert.EqualValues(t, 1, bucket.watcher.calls.Load(), "production must invoke native Stop once before public return")
	// The public contract is Updates closure, not the producer goroutine's
	// later epilogue. Probe immediately, before any test-owned rescue.
	deliveryClosed := false
	select {
	case _, ok := <-bucket.watcher.Updates():
		if ok {
			t.Error("public listing returned with native Updates delivery pending")
		} else {
			deliveryClosed = true
		}
	default:
		t.Error("public listing returned before native Updates closed")
	}
	if deliveryClosed {
		// The second delivery is causally after Stop and must be exercised
		// on a passing path. A failing path uses deferred rescue/join.
		select {
		case <-bucket.watcher.postStopAttempted:
		default:
			t.Error("post-Stop delivery boundary was not exercised")
		}
		select {
		case <-bucket.watcher.done:
		case <-time.After(2 * time.Second):
			t.Error("test-owned producer did not finish after delivery closure")
		}
	}
}

// scriptedWatchBucket drives the full NewKVStore path without a NATS server.
// The embedded interface intentionally panics if the implementation reaches an
// unrelated KeyValue method.
type scriptedWatchBucket struct {
	jetstream.KeyValue
	watch func(context.Context, []string) (jetstream.KeyWatcher, error)
}

func (b scriptedWatchBucket) WatchFiltered(ctx context.Context, filters []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return b.watch(ctx, filters)
}

type scriptedWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopFn  func() error
	calls   atomic.Int32
}

func (w *scriptedWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *scriptedWatcher) Stop() error {
	w.calls.Add(1)
	return w.stopFn()
}

func closedOnStopWatcher(entries ...jetstream.KeyValueEntry) *scriptedWatcher {
	updates := make(chan jetstream.KeyValueEntry, len(entries))
	for _, entry := range entries {
		updates <- entry
	}
	return &scriptedWatcher{updates: updates, stopFn: func() error {
		close(updates)
		return nil
	}}
}

// spec: nats-kv-keys / Filtered KVStore listings finalize native watcher delivery
func TestKVStoreFilteredWatcherSnapshotAndStopErrors(t *testing.T) {
	t.Run("snapshot_success_discards_post_marker", func(t *testing.T) {
		watcher := closedOnStopWatcher(ownershipEntry{key: "diag.one"}, nil, ownershipEntry{key: "diag.later"})
		bucket := scriptedWatchBucket{watch: func(_ context.Context, filters []string) (jetstream.KeyWatcher, error) {
			assert.Equal(t, []string{"diag.>"}, filters)
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		require.NoError(t, err)
		assert.Equal(t, []string{"diag.one"}, keys)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	t.Run("premature_close", func(t *testing.T) {
		watcher := closedOnStopWatcher(ownershipEntry{key: "partial"})
		close(watcher.updates)
		watcher.stopFn = func() error { return nil }
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, errFilteredWatcherSnapshotIncomplete)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	t.Run("nonbenign_stop_after_closure", func(t *testing.T) {
		stopFailure := errors.New("native unsubscribe failed")
		watcher := closedOnStopWatcher(nil)
		watcher.stopFn = func() error { close(watcher.updates); return stopFailure }
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, stopFailure)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	t.Run("already_terminal_stop_requires_closure", func(t *testing.T) {
		watcher := closedOnStopWatcher(nil)
		watcher.stopFn = func() error { close(watcher.updates); return nats.ErrBadSubscription }
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		require.NoError(t, err)
		assert.Nil(t, keys)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
}

// spec: nats-kv-keys / Filtered KVStore listings finalize native watcher delivery
func TestKVStoreFilteredNoWatcherPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		watch   func(context.Context) (jetstream.KeyWatcher, error)
		wantErr error
		wantOK  bool
	}{
		{"live_direct_no_keys", func(context.Context) (jetstream.KeyWatcher, error) { return nil, jetstream.ErrNoKeysFound }, nil, true},
		{"canceled_direct_no_keys", func(ctx context.Context) (jetstream.KeyWatcher, error) {
			<-ctx.Done()
			return nil, jetstream.ErrNoKeysFound
		}, context.Canceled, false},
		{"expired_direct_no_keys", func(ctx context.Context) (jetstream.KeyWatcher, error) {
			<-ctx.Done()
			return nil, jetstream.ErrNoKeysFound
		}, context.DeadlineExceeded, false},
		{"wrapped_no_keys", func(context.Context) (jetstream.KeyWatcher, error) {
			return nil, fmt.Errorf("wrapped: %w", jetstream.ErrNoKeysFound)
		}, jetstream.ErrNoKeysFound, false},
		{"constructor_failure", func(context.Context) (jetstream.KeyWatcher, error) {
			return nil, errFilteredWatcherSnapshotIncomplete
		}, errFilteredWatcherSnapshotIncomplete, false},
		{"missing_watcher", func(context.Context) (jetstream.KeyWatcher, error) { return nil, nil }, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.name == "expired_direct_no_keys" {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()
			}
			bucket := scriptedWatchBucket{watch: func(ctx context.Context, _ []string) (jetstream.KeyWatcher, error) {
				if tt.name == "canceled_direct_no_keys" {
					cancel()
				}
				return tt.watch(ctx)
			}}
			keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(ctx, "diag.>")
			assert.Nil(t, keys)
			if tt.wantOK {
				require.NoError(t, err)
			} else if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.Error(t, err)
			}
		})
	}
	t.Run("nil_context", func(t *testing.T) {
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			t.Fatal("nil context reached NATS construction")
			return nil, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(nil, "diag.>")
		assert.Nil(t, keys)
		assert.Error(t, err)
	})
}

// spec: nats-kv-keys / Filtered KVStore listings finalize native watcher delivery
func TestKVStoreFilteredTerminalWindowUsesVirtualClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		watcher := &scriptedWatcher{updates: make(chan jetstream.KeyValueEntry, 1), stopFn: func() error { return nil }}
		watcher.updates <- nil
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, errFilteredWatcherDeliveryIncomplete)
		assert.NotErrorIs(t, err, context.DeadlineExceeded, "terminal timer is not an operation deadline")
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		watcher := &scriptedWatcher{updates: make(chan jetstream.KeyValueEntry), stopFn: func() error { return nil }}
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			cancel()
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(ctx, "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, errFilteredWatcherDeliveryIncomplete)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	synctest.Test(t, func(t *testing.T) {
		watcher := &scriptedWatcher{updates: make(chan jetstream.KeyValueEntry, 1), stopFn: func() error { return nats.ErrConnectionClosed }}
		watcher.updates <- nil
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, errFilteredWatcherDeliveryIncomplete)
		assert.ErrorIs(t, err, nats.ErrConnectionClosed, "already-terminal Stop alone cannot prove delivery closure")
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
		defer cancel()
		watcher := &scriptedWatcher{updates: make(chan jetstream.KeyValueEntry), stopFn: func() error { return nil }}
		bucket := scriptedWatchBucket{watch: func(ctx context.Context, _ []string) (jetstream.KeyWatcher, error) {
			<-ctx.Done()
			return watcher, nil
		}}
		keys, err := (&Client{}).NewKVStore(bucket).KeysByFilter(ctx, "diag.>")
		assert.Nil(t, keys)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorIs(t, err, errFilteredWatcherDeliveryIncomplete)
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
}

// spec: nats-kv-keys / Filtered KVStore listings finalize native watcher delivery
func TestKVStoreFilteredStopIsSynchronous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopEntered := make(chan struct{})
		releaseStop := make(chan struct{})
		watcher := closedOnStopWatcher(nil)
		watcher.stopFn = func() error {
			close(stopEntered)
			<-releaseStop
			close(watcher.updates)
			return nil
		}
		bucket := scriptedWatchBucket{watch: func(context.Context, []string) (jetstream.KeyWatcher, error) {
			return watcher, nil
		}}
		returned := make(chan error, 1)
		go func() {
			_, err := (&Client{}).NewKVStore(bucket).KeysByFilter(t.Context(), "diag.>")
			returned <- err
		}()
		<-stopEntered
		synctest.Wait() // No runnable call task remains while Stop is held.
		select {
		case <-returned:
			t.Error("public call returned before contextless Stop completed")
		default:
		}
		close(releaseStop)
		synctest.Wait()
		select {
		case err := <-returned:
			require.NoError(t, err)
		default:
			t.Fatal("public call did not join after Stop release")
		}
		assert.EqualValues(t, 1, watcher.calls.Load())
	})
}
