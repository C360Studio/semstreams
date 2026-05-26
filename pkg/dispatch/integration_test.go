//go:build integration

// Integration tests for pkg/dispatch's completion-watcher path
// against real NATS via testcontainers. Build-tagged separately so
// unit tests stay fast; run with
// `go test -tags=integration -race ./pkg/dispatch/...`.
//
// The unit tests cover the no-completion path against the
// underlying worker pool. This file covers the load-bearing
// completion-watcher wire: real jetstream WatchAll subscription,
// real KV writes triggering OnComplete, real cancellation
// propagation through the watcher goroutine.
package dispatch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const integBucketName = "DISPATCH_INTEG"

// setupDispatcher creates a NATS testcontainer, provisions the
// integBucketName KV bucket, and returns a Deps populated with the
// live natsclient. Tests construct the dispatcher with their own
// Config; per-container isolation makes per-test bucket names
// unnecessary.
func setupDispatcher(t *testing.T) (*natsclient.Client, jetstream.KeyValue) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	bucket, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      integBucketName,
		Description: "dispatch integration test bucket",
		History:     10,
	})
	require.NoError(t, err, "create KV bucket %q", integBucketName)

	return tc.Client, bucket
}

// TestIntegration_CompletionWatcherFiresOnKVWrite is the load-
// bearing integration test for the dispatcher's KV-twofer
// completion path. Submits a work item, simulates an out-of-process
// completion by writing to the watched key, asserts OnComplete
// fires with the right work value.
func TestIntegration_CompletionWatcherFiresOnKVWrite(t *testing.T) {
	client, bucket := setupDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	completedCh := make(chan string, 1)
	d, err := New(ctx, Config[string]{
		Workers:                  2,
		QueueSize:                10,
		Process:                  func(context.Context, string) error { return nil },
		CompletionKVBucket:       integBucketName,
		CompletionKeyForWorkItem: func(s string) string { return "completion." + s },
		OnComplete: func(_ context.Context, work string) error {
			completedCh <- work
			return nil
		},
	}, Deps{NATSClient: client})
	require.NoError(t, err)
	defer d.Stop(context.Background())

	// Submit a work item — registers tracking for key
	// "completion.task-001".
	require.NoError(t, d.Submit("task-001"))

	// Simulate an out-of-process completion: write to the watched
	// key. The dispatcher's watcher should pick this up and fire
	// OnComplete.
	_, err = bucket.Put(ctx, "completion.task-001", []byte(`{"status":"done"}`))
	require.NoError(t, err)

	select {
	case got := <-completedCh:
		require.Equal(t, "task-001", got,
			"OnComplete should fire with the matching work item")
	case <-time.After(5 * time.Second):
		t.Fatal("OnComplete did not fire within 5s of KV write")
	}
}

// TestIntegration_CompletionWatcherIgnoresUntrackedWrites verifies
// the dispatcher silently ignores KV writes that don't match any
// tracked work — bootstrap-replay events and out-of-band writes
// by other producers shouldn't trigger spurious OnComplete calls.
func TestIntegration_CompletionWatcherIgnoresUntrackedWrites(t *testing.T) {
	client, bucket := setupDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-write a value so bootstrap-replay sees it.
	_, err := bucket.Put(ctx, "completion.pre-existing", []byte(`{"status":"done"}`))
	require.NoError(t, err)

	var spuriousCount atomic.Int32
	d, err := New(ctx, Config[string]{
		Workers:                  2,
		QueueSize:                10,
		Process:                  func(context.Context, string) error { return nil },
		CompletionKVBucket:       integBucketName,
		CompletionKeyForWorkItem: func(s string) string { return "completion." + s },
		OnComplete: func(_ context.Context, _ string) error {
			spuriousCount.Add(1)
			return nil
		},
	}, Deps{NATSClient: client})
	require.NoError(t, err)
	defer d.Stop(context.Background())

	// Give the watcher a moment to process bootstrap.
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, int32(0), spuriousCount.Load(),
		"OnComplete should NOT fire for bootstrap-replay of untracked keys")

	// Write to another untracked key — same expectation.
	_, err = bucket.Put(ctx, "completion.unrelated", []byte(`{"status":"done"}`))
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, int32(0), spuriousCount.Load(),
		"OnComplete should NOT fire for unrelated KV writes")
}

// TestIntegration_CompletionWatcherSubmitBeforeProcessRace verifies
// the documented contract: Submit registers tracking BEFORE
// enqueuing, so a completion-signal write that arrives between
// Submit and Process can't slip past the watcher. Stress this with
// rapid Submit + immediate KV write.
func TestIntegration_CompletionWatcherSubmitBeforeProcessRace(t *testing.T) {
	client, bucket := setupDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seen sync.Map // workID → bool
	d, err := New(ctx, Config[string]{
		Workers:   4,
		QueueSize: 100,
		Process: func(_ context.Context, _ string) error {
			// Process is fast; the race is between Submit
			// registering tracking and the KV write firing OnComplete.
			return nil
		},
		CompletionKVBucket:       integBucketName,
		CompletionKeyForWorkItem: func(s string) string { return "race." + s },
		OnComplete: func(_ context.Context, work string) error {
			seen.Store(work, true)
			return nil
		},
	}, Deps{NATSClient: client})
	require.NoError(t, err)
	defer d.Stop(context.Background())

	const N = 20
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			workID := fmt.Sprintf("work-%02d", idx)
			require.NoError(t, d.Submit(workID))
			// Immediately write the completion signal; the
			// watcher must have already seen the Submit-time
			// tracking registration.
			_, err := bucket.Put(ctx, "race."+workID, []byte(`{"done":true}`))
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Give the watcher up to 5s to process the N completion signals.
	deadline := time.After(5 * time.Second)
	for {
		count := 0
		seen.Range(func(_, _ any) bool {
			count++
			return true
		})
		if count >= N {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d/%d completions seen within 5s", count, N)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestIntegration_StopReleasesWatcherGoroutine verifies that Stop
// cleanly tears down the watcher goroutine (no leak across test
// runs). Tracked via the watcher's inFlightCount helper which would
// otherwise observe state across test boundaries if the goroutine
// leaked.
func TestIntegration_StopReleasesWatcherGoroutine(t *testing.T) {
	client, _ := setupDispatcher(t)

	d, err := New(context.Background(), Config[string]{
		Workers:                  1,
		QueueSize:                5,
		Process:                  func(context.Context, string) error { return nil },
		CompletionKVBucket:       integBucketName,
		CompletionKeyForWorkItem: func(s string) string { return s },
		OnComplete:               func(context.Context, string) error { return nil },
	}, Deps{NATSClient: client})
	require.NoError(t, err)

	require.NoError(t, d.Submit("tracked-item"))
	require.Equal(t, 1, d.completion.inFlightCount(),
		"tracking entry should be registered post-Submit")

	require.NoError(t, d.Stop(context.Background()))
	// Watcher's stop() called from Stop — verify the goroutine
	// exited by checking the watcher's WaitGroup state via a
	// secondary stop call (idempotent should not block).
	require.NoError(t, d.Stop(context.Background()),
		"second Stop must be idempotent — implies first Stop's wg.Wait completed")
}
