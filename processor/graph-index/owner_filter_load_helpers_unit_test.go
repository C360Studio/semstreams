package graphindex

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOwnerLoadSeedQueuedFailureJoins(t *testing.T) {
	primary := errors.New("designated seed failure")
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	var joined atomic.Int64
	rows := make([]int, 320)
	for i := range rows {
		rows[i] = i
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	helperDone := make(chan struct{})
	var released atomic.Bool
	releaseGate := func() {
		if released.CompareAndSwap(false, true) {
			close(release)
		}
	}
	t.Cleanup(func() {
		releaseGate()
		cancel()
		ownerLoadProofWait(t, helperDone, "seed helper")
	})
	go func() {
		defer close(helperDone)
		done <- ownerLoadSeed(ctx, rows, 32, 256, 100*time.Millisecond, func(ctx context.Context, row int) error {
			defer joined.Add(1)
			if row == 0 {
				<-release
				return primary
			}
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}, nil, nil)
	}()
	for range 31 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("seed workers did not enter")
		}
	}
	releaseGate()
	select {
	case err := <-done:
		require.ErrorIs(t, err, primary)
		require.Equal(t, int64(32), joined.Load())
	case <-ctx.Done():
		t.Fatal("seed helper did not terminate")
	}
}

func TestOwnerLoadSeedCancellationAtCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var captured atomic.Bool
	err := ownerLoadSeed(ctx, []int{1}, 1, 1, 100*time.Millisecond,
		func(context.Context, int) error { cancel(); return nil }, nil,
		func(fault ownerLoadFault) {
			captured.Store(errors.Is(fault.Err, context.Canceled) && errors.Is(fault.CallerCause, context.Canceled))
		})
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, captured.Load(), "cancellation at worker completion must retain operation-context evidence")
}

func ownerLoadProofWait(t *testing.T, done <-chan struct{}, owner string) bool {
	t.Helper()
	select {
	case <-done:
		return true
	case <-time.After(time.Second):
		t.Errorf("fixture recovery did not join %s", owner)
		return false
	}
}

func ownerLoadProofOptions() ownerLoadConcurrentOptions {
	return ownerLoadConcurrentOptions{
		Fixtures: 2, Repetitions: 1, Workers: 2, ChurnPerWriter: 1,
		QueueSize: 2, TerminalBudget: 100 * time.Millisecond,
		Key:      func(job ownerLoadJob) string { return fmt.Sprintf("fixture-%d-%d", job.Fixture, job.Serial) },
		Describe: func(ownerLoadJob) (string, string, string) { return "fixture", "bucket", "filter" },
	}
}

func TestOwnerLoadConcurrentFailureJoinsActiveOwners(t *testing.T) {
	primary := errors.New("designated listing failure")
	listEntered := make(chan int, 2)
	sampleEntered := make(chan struct{})
	churnEntered := make(chan int, 2)
	releaseFailure := make(chan struct{})
	releaseSampler := make(chan struct{})
	samplerCanceled := make(chan struct{})
	samplerDone := make(chan struct{})
	operationCtx := make(chan context.Context, 1)
	var captured atomic.Bool
	options := ownerLoadProofOptions()
	options.TerminalBudget = time.Second
	options.List = func(ctx context.Context, job ownerLoadJob) ownerLoadListing {
		listEntered <- job.Fixture
		if job.Fixture == 0 {
			operationCtx <- ctx
			<-releaseFailure
			return ownerLoadListing{Err: primary, Duration: time.Millisecond, Published: true}
		}
		<-ctx.Done()
		return ownerLoadListing{Err: ctx.Err(), Duration: time.Millisecond, Published: true}
	}
	options.Sample = func(ctx context.Context) (ownerLoadJob, error) {
		close(sampleEntered)
		<-ctx.Done()
		close(samplerCanceled)
		<-releaseSampler
		close(samplerDone)
		return ownerLoadJob{Fixture: 0}, ctx.Err()
	}
	options.Churn = func(ctx context.Context, writer, _ int) error {
		churnEntered <- writer
		<-ctx.Done()
		return ctx.Err()
	}
	options.Report = func(fault ownerLoadFault) {
		if fault.Phase == "list" && errors.Is(fault.Err, primary) {
			callbackCtx := <-operationCtx
			if callbackCtx.Err() == nil && fault.CallerCause == context.Cause(callbackCtx) {
				captured.Store(true)
			}
			operationCtx <- callbackCtx
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	helperDone := make(chan struct{})
	var failureReleased, samplerReleased atomic.Bool
	releaseFailureGate := func() {
		if failureReleased.CompareAndSwap(false, true) {
			close(releaseFailure)
		}
	}
	releaseSamplerGate := func() {
		if samplerReleased.CompareAndSwap(false, true) {
			close(releaseSampler)
		}
	}
	t.Cleanup(func() {
		releaseFailureGate()
		releaseSamplerGate()
		cancel()
		helperJoined := ownerLoadProofWait(t, helperDone, "active concurrent helper")
		samplerJoined := true
		select {
		case <-sampleEntered:
			samplerJoined = ownerLoadProofWait(t, samplerDone, "active sampler")
		default:
		}
		if helperJoined && samplerJoined {
			t.Log("fixture recovery joined helper and active sampler")
		}
	})
	go func() { defer close(helperDone); _, err := ownerLoadConcurrent(ctx, options); done <- err }()
	for range 2 {
		select {
		case <-listEntered:
		case <-ctx.Done():
			t.Fatal("list did not enter")
		}
		select {
		case <-churnEntered:
		case <-ctx.Done():
			t.Fatal("churn did not enter")
		}
	}
	select {
	case <-sampleEntered:
	case <-ctx.Done():
		t.Fatal("sampler did not enter")
	}
	releaseFailureGate()
	select {
	case <-samplerCanceled:
	case <-ctx.Done():
		t.Fatal("sampler did not observe cancellation")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before sampler joined: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseSamplerGate()
	select {
	case err := <-done:
		require.ErrorIs(t, err, primary)
		require.True(t, captured.Load(), "failure must be captured before harness cancellation")
		select {
		case callbackCtx := <-operationCtx:
			require.ErrorIs(t, callbackCtx.Err(), context.Canceled)
		default:
			t.Fatal("callback context missing")
		}
		select {
		case <-samplerDone:
		default:
			t.Fatal("sampler completion was not observed")
		}
	case <-ctx.Done():
		t.Fatal("concurrent phase did not finish")
	}
}

func TestOwnerLoadConcurrentMissingResultAndHealthyOrder(t *testing.T) {
	options := ownerLoadProofOptions()
	options.ChurnPerWriter = 0
	options.Repetitions = 3
	options.Sample = func(ctx context.Context) (ownerLoadJob, error) {
		<-ctx.Done()
		return ownerLoadJob{Fixture: 0}, ctx.Err()
	}
	options.Churn = func(context.Context, int, int) error { return nil }
	options.List = func(_ context.Context, job ownerLoadJob) ownerLoadListing {
		return ownerLoadListing{Count: 1, Duration: time.Duration(job.Serial+1) * time.Millisecond, Published: true}
	}
	out, err := ownerLoadConcurrent(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}, out.Durations[0])
	require.Equal(t, out.Durations[0], out.Durations[1])
	options.List = func(context.Context, ownerLoadJob) ownerLoadListing { return ownerLoadListing{Published: false} }
	_, err = ownerLoadConcurrent(t.Context(), options)
	require.ErrorContains(t, err, "missing listing results")
}

func TestOwnerLoadConcurrentCancellationAtCompletion(t *testing.T) {
	for _, phase := range []string{"listing", "churn"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var captured atomic.Bool
			listed := make(chan struct{})
			options := ownerLoadProofOptions()
			options.Fixtures, options.Repetitions, options.Workers, options.ChurnPerWriter = 1, 1, 1, 1
			options.List = func(context.Context, ownerLoadJob) ownerLoadListing {
				close(listed)
				if phase == "listing" {
					cancel()
				}
				return ownerLoadListing{Count: 1, Duration: time.Millisecond, Published: true}
			}
			options.Sample = func(runCtx context.Context) (ownerLoadJob, error) {
				<-runCtx.Done()
				return ownerLoadJob{Fixture: 0}, runCtx.Err()
			}
			options.Churn = func(runCtx context.Context, _, _ int) error {
				if phase == "churn" {
					select {
					case <-listed:
						cancel()
					case <-runCtx.Done():
						return runCtx.Err()
					}
				}
				return nil
			}
			options.Report = func(fault ownerLoadFault) {
				captured.Store(errors.Is(fault.Err, context.Canceled) && errors.Is(fault.CallerCause, context.Canceled))
			}
			_, err := ownerLoadConcurrent(ctx, options)
			require.ErrorIs(t, err, context.Canceled)
			require.True(t, captured.Load(), "cancellation at %s completion must retain operation-context evidence", phase)
		})
	}
}

func TestOwnerLoadConvergeCancellationRetainsObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	done := make(chan struct{})
	var reported atomic.Bool
	helperDone := make(chan struct{})
	t.Cleanup(func() { cancel(); ownerLoadProofWait(t, helperDone, "convergence helper") })
	result := make(chan error, 1)
	go func() {
		defer close(helperDone)
		attempts, count, lastErr, err := ownerLoadConverge(ctx, 5*time.Second, 20*time.Millisecond, 2,
			func(pollCtx context.Context) (int, error) {
				if _, ok := pollCtx.Deadline(); !ok {
					return 1, errors.New("missing finite deadline")
				}
				close(entered)
				<-pollCtx.Done()
				close(done)
				return 1, pollCtx.Err()
			}, func(pollCtx context.Context, attempts, lastCount int, lastErr, failure error) {
				_, finite := pollCtx.Deadline()
				reported.Store(finite && attempts == 1 && lastCount == 1 && errors.Is(lastErr, context.Canceled) &&
					errors.Is(failure, context.Canceled) && errors.Is(context.Cause(pollCtx), context.Canceled))
			})
		if attempts != 1 || count != 1 || !errors.Is(lastErr, context.Canceled) {
			result <- errors.New("lost last observation")
			return
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Info did not enter")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("convergence did not stop")
	}
	select {
	case <-done:
	default:
		t.Fatal("Info callback did not finish")
	}
	require.True(t, reported.Load(), "failure must observe the finite polling context before owner cleanup")
}

func TestOwnerLoadConvergeRejectsExpiredBaseline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var observed atomic.Bool
	attempts, lastCount, lastErr, err := ownerLoadConverge(ctx, 5*time.Second, 20*time.Millisecond, 2,
		func(context.Context) (int, error) { cancel(); return 2, nil },
		func(pollCtx context.Context, gotAttempts, gotCount int, gotErr, failure error) {
			observed.Store(gotAttempts == 1 && gotCount == 2 && gotErr == nil &&
				errors.Is(failure, context.Canceled) && errors.Is(context.Cause(pollCtx), context.Canceled))
		})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
	require.Equal(t, 2, lastCount)
	require.NoError(t, lastErr)
	require.True(t, observed.Load(), "canceled poll must report its last baseline observation")
}

func TestOwnerLoadConcurrentAdmissionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	options := ownerLoadProofOptions()
	options.Fixtures, options.Repetitions, options.Workers, options.QueueSize = 1, 1000, 1, 1
	options.ChurnPerWriter = 0
	options.List = func(runCtx context.Context, _ ownerLoadJob) ownerLoadListing {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-runCtx.Done()
		return ownerLoadListing{Err: runCtx.Err(), Published: true}
	}
	options.Sample = func(runCtx context.Context) (ownerLoadJob, error) {
		<-runCtx.Done()
		return ownerLoadJob{Fixture: 0}, runCtx.Err()
	}
	options.Churn = func(context.Context, int, int) error { return nil }
	done := make(chan error, 1)
	helperDone := make(chan struct{})
	t.Cleanup(func() { cancel(); ownerLoadProofWait(t, helperDone, "admission helper") })
	go func() { defer close(helperDone); _, err := ownerLoadConcurrent(ctx, options); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("listing did not enter")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.NotContains(t, err.Error(), "terminal join deadline")
	case <-time.After(time.Second):
		t.Fatal("submission did not observe cancellation")
	}
}

func TestOwnerLoadConcurrentTerminalExpiryRetainsPrimary(t *testing.T) {
	primary := errors.New("listing failed before terminal expiry")
	listEntered := make(chan struct{})
	samplerEntered := make(chan struct{})
	releaseFailure := make(chan struct{})
	releaseSampler := make(chan struct{})
	listDone := make(chan struct{})
	samplerDone := make(chan struct{})
	var failureReleased, samplerReleased atomic.Bool
	releaseFailureGate := func() {
		if failureReleased.CompareAndSwap(false, true) {
			close(releaseFailure)
		}
	}
	releaseSamplerGate := func() {
		if samplerReleased.CompareAndSwap(false, true) {
			close(releaseSampler)
		}
	}
	options := ownerLoadProofOptions()
	options.Fixtures, options.Repetitions, options.Workers = 1, 1, 1
	options.ChurnPerWriter = 0
	options.TerminalBudget = 20 * time.Millisecond
	options.List = func(context.Context, ownerLoadJob) ownerLoadListing {
		defer close(listDone)
		close(listEntered)
		<-releaseFailure
		return ownerLoadListing{Err: primary, Published: true, Duration: time.Millisecond}
	}
	options.Sample = func(ctx context.Context) (ownerLoadJob, error) {
		close(samplerEntered)
		<-ctx.Done()
		<-releaseSampler
		close(samplerDone)
		return ownerLoadJob{Fixture: 0}, ctx.Err()
	}
	options.Churn = func(context.Context, int, int) error { return nil }
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	helperDone := make(chan struct{})
	// Recovery owns both deliberately held callbacks even when a setup assertion fails.
	t.Cleanup(func() {
		releaseFailureGate()
		releaseSamplerGate()
		cancel()
		ownerLoadProofWait(t, helperDone, "terminal helper")
		select {
		case <-listEntered:
			ownerLoadProofWait(t, listDone, "terminal listing")
		default:
		}
		select {
		case <-samplerEntered:
			ownerLoadProofWait(t, samplerDone, "terminal sampler")
		default:
		}
	})
	go func() { defer close(helperDone); _, err := ownerLoadConcurrent(ctx, options); done <- err }()
	select {
	case <-listEntered:
	case <-ctx.Done():
		t.Fatal("listing did not enter")
	}
	select {
	case <-samplerEntered:
	case <-ctx.Done():
		t.Fatal("sampler did not enter")
	}
	releaseFailureGate()
	select {
	case err := <-done:
		require.ErrorIs(t, err, primary)
		require.ErrorContains(t, err, "unresolved sampler")
	case <-ctx.Done():
		t.Fatal("terminal bound did not return")
	}
	releaseSamplerGate()
	select {
	case <-samplerDone:
	case <-time.After(time.Second):
		t.Fatal("sampler did not finish after independent release")
	}
}
