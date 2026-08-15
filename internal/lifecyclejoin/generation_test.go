package lifecyclejoin

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerationStopCancellationDeadlineHandoffAndSharedResult(t *testing.T) {
	runtimeCanceled := make(chan struct{})
	releaseRuntime := make(chan struct{})
	var signalCalls atomic.Int32
	var cleanupCalls atomic.Int32
	wantErr := errors.New("terminal cleanup failed")
	g := NewGeneration(func() { close(runtimeCanceled) }, func() { <-releaseRuntime })

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	cancelFirst()
	err := g.Stop(firstCtx, func() error {
		signalCalls.Add(1)
		return nil
	}, func(context.Context) error {
		cleanupCalls.Add(1)
		return wantErr
	})
	require.ErrorIs(t, err, context.Canceled)
	<-runtimeCanceled
	require.Equal(t, int32(1), signalCalls.Load())
	require.Zero(t, cleanupCalls.Load())

	close(releaseRuntime)
	const callers = 8
	results := make(chan error, callers)
	var callersDone sync.WaitGroup
	for range callers {
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			results <- g.Stop(context.Background(), func() error {
				signalCalls.Add(1)
				return nil
			}, func(context.Context) error {
				cleanupCalls.Add(1)
				return wantErr
			})
		}()
	}
	callersDone.Wait()
	close(results)
	for got := range results {
		require.ErrorIs(t, got, wantErr)
	}
	require.Equal(t, int32(1), signalCalls.Load())
	require.Equal(t, int32(1), cleanupCalls.Load())
}

func TestGenerationStopCleanupContextExpiryCanResume(t *testing.T) {
	var cleanupCalls atomic.Int32
	g := NewGeneration(nil, func() {})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	require.ErrorIs(t, g.Stop(firstCtx, nil, func(ctx context.Context) error {
		cleanupCalls.Add(1)
		cancelFirst()
		return ctx.Err()
	}), context.Canceled)

	require.NoError(t, g.Stop(context.Background(), nil, func(context.Context) error {
		cleanupCalls.Add(1)
		return nil
	}))
	require.NoError(t, g.Stop(context.Background(), nil, func(context.Context) error {
		cleanupCalls.Add(1)
		return errors.New("must not run")
	}))
	require.Equal(t, int32(2), cleanupCalls.Load())
}

func TestGenerationDeadlineWinsWhenCleanupReturnsNilAfterCancellation(t *testing.T) {
	var cleanupCalls atomic.Int32
	g := NewGeneration(nil, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	require.ErrorIs(t, g.Stop(ctx, nil, func(context.Context) error {
		cleanupCalls.Add(1)
		cancel()
		return nil
	}), context.Canceled)
	require.NoError(t, g.Stop(context.Background(), nil, func(context.Context) error {
		cleanupCalls.Add(1)
		return nil
	}))
	require.Equal(t, int32(2), cleanupCalls.Load())
}

func TestGenerationRetainsGenuineCleanupErrorAcrossContextRetry(t *testing.T) {
	wantErr := errors.New("cleanup observed a genuine failure")
	var cleanupCalls atomic.Int32
	g := NewGeneration(nil, func() {})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := g.Stop(firstCtx, nil, func(ctx context.Context) error {
		cleanupCalls.Add(1)
		cancelFirst()
		return errors.Join(wantErr, ctx.Err())
	})
	require.ErrorIs(t, firstErr, wantErr)
	require.ErrorIs(t, firstErr, context.Canceled)

	terminalErr := g.Stop(context.Background(), nil, func(context.Context) error {
		cleanupCalls.Add(1)
		return nil
	})
	require.ErrorIs(t, terminalErr, wantErr)
	require.NotErrorIs(t, terminalErr, context.Canceled)

	replayedErr := g.Stop(context.Background(), nil, func(context.Context) error {
		cleanupCalls.Add(1)
		return errors.New("must not run")
	})
	require.ErrorIs(t, replayedErr, wantErr)
	require.NotErrorIs(t, replayedErr, context.Canceled)
	require.Equal(t, int32(2), cleanupCalls.Load())
}

func TestGenerationStopDeadlineIsOnlyFailureBound(t *testing.T) {
	release := make(chan struct{})
	g := NewGeneration(nil, func() { <-release })
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, g.Stop(ctx, nil, nil), context.DeadlineExceeded)
	close(release)
	require.NoError(t, g.Stop(context.Background(), nil, nil))
}

func TestPartialStartRollbackIsIndependentlyBounded(t *testing.T) {
	var deadline time.Time
	err := RunPartialStartRollback(func(ctx context.Context) error {
		var ok bool
		deadline, ok = ctx.Deadline()
		require.True(t, ok)
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, time.Until(deadline))
	require.LessOrEqual(t, time.Until(deadline), partialStartRollbackTimeout)
}

func TestOperationIsContextBoundedResumableAndRetained(t *testing.T) {
	op := NewOperation()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- op.Run(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return errors.New("prepare failed")
		})
	}()
	<-entered
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	require.ErrorIs(t, op.Run(waitCtx, nil), context.Canceled)
	close(release)
	firstErr := <-firstResult
	require.EqualError(t, firstErr, "prepare failed")
	require.EqualError(t, op.Run(context.Background(), func(context.Context) error {
		return errors.New("must not run")
	}), firstErr.Error())
}

func TestOperationCallerExpiryCanRetry(t *testing.T) {
	op := NewOperation()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	require.ErrorIs(t, op.Run(firstCtx, func(ctx context.Context) error {
		cancelFirst()
		return ctx.Err()
	}), context.Canceled)
	require.NoError(t, op.Run(context.Background(), func(context.Context) error { return nil }))
}

func TestOperationDeadlineWinsWhenOperationReturnsNilAfterCancellation(t *testing.T) {
	op := NewOperation()
	ctx, cancel := context.WithCancel(context.Background())
	require.ErrorIs(t, op.Run(ctx, func(context.Context) error {
		cancel()
		return nil
	}), context.Canceled)
	require.NoError(t, op.Run(context.Background(), func(context.Context) error { return nil }))
}

func TestOperationRetainsGenuineErrorAcrossContextRetry(t *testing.T) {
	wantErr := errors.New("protocol shutdown observed a genuine failure")
	var calls atomic.Int32
	op := NewOperation()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := op.Run(firstCtx, func(ctx context.Context) error {
		calls.Add(1)
		cancelFirst()
		return errors.Join(wantErr, ctx.Err())
	})
	require.ErrorIs(t, firstErr, wantErr)
	require.ErrorIs(t, firstErr, context.Canceled)

	terminalErr := op.Run(context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	})
	require.ErrorIs(t, terminalErr, wantErr)
	require.NotErrorIs(t, terminalErr, context.Canceled)

	replayedErr := op.Run(context.Background(), func(context.Context) error {
		calls.Add(1)
		return errors.New("must not run")
	})
	require.ErrorIs(t, replayedErr, wantErr)
	require.NotErrorIs(t, replayedErr, context.Canceled)
	require.Equal(t, int32(2), calls.Load())
}
