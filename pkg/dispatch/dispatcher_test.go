package dispatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Config validation ---

func TestNew_RejectsNilContext(t *testing.T) {
	_, err := New[string](nil, Config[string]{
		Workers:   1,
		QueueSize: 1,
		Process:   func(context.Context, string) error { return nil },
	}, Deps{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNew_RejectsZeroWorkers(t *testing.T) {
	_, err := New(context.Background(), Config[string]{
		Workers:   0,
		QueueSize: 10,
		Process:   func(context.Context, string) error { return nil },
	}, Deps{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNew_RejectsZeroQueueSize(t *testing.T) {
	_, err := New(context.Background(), Config[string]{
		Workers:   2,
		QueueSize: 0,
		Process:   func(context.Context, string) error { return nil },
	}, Deps{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNew_RejectsNilProcess(t *testing.T) {
	_, err := New(context.Background(), Config[string]{
		Workers:   2,
		QueueSize: 10,
		Process:   nil,
	}, Deps{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNew_RejectsPartialCompletionConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config[string]
	}{
		{
			"bucket without keyFor or onComplete",
			Config[string]{
				Workers:            2,
				QueueSize:          10,
				Process:            func(context.Context, string) error { return nil },
				CompletionKVBucket: "MY_BUCKET",
			},
		},
		{
			"keyFor without bucket",
			Config[string]{
				Workers:                  2,
				QueueSize:                10,
				Process:                  func(context.Context, string) error { return nil },
				CompletionKeyForWorkItem: func(s string) string { return s },
			},
		},
		{
			"onComplete without bucket",
			Config[string]{
				Workers:    2,
				QueueSize:  10,
				Process:    func(context.Context, string) error { return nil },
				OnComplete: func(context.Context, string) error { return nil },
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), tc.cfg, Deps{})
			require.ErrorIs(t, err, ErrInvalidConfig,
				"partial completion-watcher config must reject; got %v", err)
		})
	}
}

func TestNew_RejectsCompletionWithoutNATSClient(t *testing.T) {
	_, err := New(context.Background(), Config[string]{
		Workers:                  2,
		QueueSize:                10,
		Process:                  func(context.Context, string) error { return nil },
		CompletionKVBucket:       "MY_BUCKET",
		CompletionKeyForWorkItem: func(s string) string { return s },
		OnComplete:               func(context.Context, string) error { return nil },
	}, Deps{}) // no NATSClient
	require.ErrorIs(t, err, ErrNATSClientRequired)
}

// --- Submit + Process (no completion watcher) ---

func TestDispatcher_SubmitProcessesAllItems(t *testing.T) {
	var processed atomic.Int32
	d, err := New(context.Background(), Config[int]{
		Workers:   4,
		QueueSize: 100,
		Process: func(_ context.Context, _ int) error {
			processed.Add(1)
			return nil
		},
	}, Deps{})
	require.NoError(t, err)

	const N = 50
	for i := range N {
		require.NoError(t, d.Submit(i))
	}
	// Stop blocks until in-flight work drains.
	require.NoError(t, d.Stop(context.Background()))
	assert.Equal(t, int32(N), processed.Load(), "all submitted items should process")
}

func TestDispatcher_RespectsBoundedConcurrency(t *testing.T) {
	// Process function holds long enough to surface concurrent
	// execution. Track max in-flight count and assert it stays
	// at or below Workers.
	const workers = 3
	var inFlight, maxInFlight atomic.Int32

	d, err := New(context.Background(), Config[int]{
		Workers:   workers,
		QueueSize: 100,
		Process: func(_ context.Context, _ int) error {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			// Track running max via CAS-style loop.
			for {
				cur := maxInFlight.Load()
				if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			return nil
		},
	}, Deps{})
	require.NoError(t, err)

	for i := range 30 {
		require.NoError(t, d.Submit(i))
	}
	require.NoError(t, d.Stop(context.Background()))

	got := maxInFlight.Load()
	assert.LessOrEqual(t, got, int32(workers),
		"max in-flight should not exceed Workers=%d, observed %d", workers, got)
	assert.GreaterOrEqual(t, got, int32(2),
		"max in-flight should hit at least 2 to confirm parallel execution, observed %d", got)
}

func TestDispatcher_SubmitAfterStopReturnsErrStopped(t *testing.T) {
	d, err := New(context.Background(), Config[string]{
		Workers:   1,
		QueueSize: 10,
		Process:   func(context.Context, string) error { return nil },
	}, Deps{})
	require.NoError(t, err)
	require.NoError(t, d.Stop(context.Background()))

	err = d.Submit("post-stop")
	require.ErrorIs(t, err, ErrStopped)
}

func TestDispatcher_StatsReflectsActivity(t *testing.T) {
	var processGate sync.WaitGroup
	processGate.Add(1) // gate Process so we can observe queued state

	d, err := New(context.Background(), Config[int]{
		Workers:   1,
		QueueSize: 10,
		Process: func(_ context.Context, _ int) error {
			processGate.Wait()
			return nil
		},
	}, Deps{})
	require.NoError(t, err)

	// Submit 5 items; the first occupies the single worker, the
	// rest queue behind it.
	for i := range 5 {
		require.NoError(t, d.Submit(i))
	}
	// Open the gate to drain; Stop blocks until all process.
	processGate.Done()
	require.NoError(t, d.Stop(context.Background()))

	stats := d.Stats()
	assert.Equal(t, int64(5), stats.Submitted, "Submitted counter should reflect 5 calls")
	assert.Equal(t, int64(5), stats.Processed, "Processed counter should reflect 5 completed Process calls")
}

func TestDispatcher_ProcessError_IncrementsFailed(t *testing.T) {
	sentinel := errors.New("process said no")
	d, err := New(context.Background(), Config[int]{
		Workers:   1,
		QueueSize: 5,
		Process: func(_ context.Context, work int) error {
			if work%2 == 0 {
				return sentinel
			}
			return nil
		},
	}, Deps{})
	require.NoError(t, err)

	for i := range 4 {
		require.NoError(t, d.Submit(i))
	}
	require.NoError(t, d.Stop(context.Background()))

	stats := d.Stats()
	assert.Equal(t, int64(4), stats.Submitted)
	assert.Equal(t, int64(2), stats.Failed,
		"two even-numbered work items should fail")
}

// --- Stop semantics ---

func TestDispatcher_StopWaitsForInFlightWork(t *testing.T) {
	var completed atomic.Int32
	d, err := New(context.Background(), Config[int]{
		Workers:   2,
		QueueSize: 10,
		Process: func(_ context.Context, _ int) error {
			time.Sleep(50 * time.Millisecond)
			completed.Add(1)
			return nil
		},
	}, Deps{})
	require.NoError(t, err)

	for i := range 5 {
		require.NoError(t, d.Submit(i))
	}
	// Stop should block until all 5 Process calls return.
	require.NoError(t, d.Stop(context.Background()))
	assert.Equal(t, int32(5), completed.Load(),
		"Stop should wait for all in-flight Process calls to complete")
}

func TestDispatcher_StopIdempotent(t *testing.T) {
	d, err := New(context.Background(), Config[int]{
		Workers:   1,
		QueueSize: 5,
		Process:   func(context.Context, int) error { return nil },
	}, Deps{})
	require.NoError(t, err)

	require.NoError(t, d.Stop(context.Background()))
	require.NoError(t, d.Stop(context.Background()),
		"second Stop should be no-op success")
}

func TestDispatcher_StopRejectsNilContextBeforeAction(t *testing.T) {
	d, err := New(context.Background(), Config[int]{
		Workers:   1,
		QueueSize: 1,
		Process:   func(context.Context, int) error { return nil },
	}, Deps{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Stop(context.Background()) })

	var stopErr error
	require.NotPanics(t, func() { stopErr = d.Stop(nil) })
	require.ErrorIs(t, stopErr, ErrInvalidConfig)
	require.NoError(t, d.Submit(1), "nil Stop acted on the worker pool")
}

func TestDispatcher_StopReportsTerminalPoolTimeoutAndContextCause(t *testing.T) {
	tests := []struct {
		name    string
		stopCtx func() (context.Context, context.CancelFunc)
		cause   error
	}{
		{
			name: "already canceled",
			stopCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			cause: context.Canceled,
		},
		{
			name: "expired deadline",
			stopCtx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			cause: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processEntered := make(chan struct{})
			processRelease := make(chan struct{})
			processReturned := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(processRelease) }) }
			t.Cleanup(release)

			d, err := New(context.Background(), Config[int]{
				Workers:   1,
				QueueSize: 1,
				Process: func(context.Context, int) error {
					close(processEntered)
					<-processRelease
					close(processReturned)
					return nil
				},
			}, Deps{})
			require.NoError(t, err)
			require.NoError(t, d.Submit(1))
			select {
			case <-processEntered:
			case <-time.After(time.Second):
				t.Fatal("dispatcher process did not start")
			}

			stopCtx, cancelStop := tt.stopCtx()
			defer cancelStop()
			stopResult := make(chan error, 1)
			go func() { stopResult <- d.Stop(stopCtx) }()
			var stopErr error
			select {
			case stopErr = <-stopResult:
			case <-time.After(time.Second):
				t.Fatal("Stop did not honor the already-finished caller context")
			}
			require.ErrorIs(t, stopErr, worker.ErrStopTimeout)
			require.ErrorIs(t, stopErr, tt.cause)

			release()
			select {
			case <-processReturned:
			case <-time.After(time.Second):
				t.Fatal("dispatcher process did not return after test release")
			}
		})
	}
}
