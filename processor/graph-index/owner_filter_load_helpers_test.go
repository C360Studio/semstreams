package graphindex

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
)

const ownerLoadTerminalBudget = 10 * time.Second

type ownerLoadFault struct {
	Phase, Fixture, Bucket, Filter, Operation string
	Elapsed                                   time.Duration
	CallerDeadline                            time.Time
	CallerCause                               error
	Err                                       error
	Stacks                                    string
}

func ownerLoadFaultAt(ctx context.Context, phase, fixture, bucket, filter, operation string, elapsed time.Duration, err error) ownerLoadFault {
	deadline, _ := ctx.Deadline()
	buf := make([]byte, 64<<10)
	n := runtime.Stack(buf, true)
	stacks := string(buf[:n])
	if n == len(buf) {
		stacks += "\n[goroutine stacks truncated]"
	}
	return ownerLoadFault{phase, fixture, bucket, filter, operation, elapsed, deadline, context.Cause(ctx), err, stacks}
}

type ownerLoadJoinOwner struct {
	name string
	done <-chan struct{}
}

func ownerLoadJoin(parent context.Context, budget time.Duration, owners ...ownerLoadJoinOwner) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)
	defer cancel()
	var unresolved []error
	for _, owner := range owners {
		if owner.done == nil {
			continue
		}
		select {
		case <-owner.done:
			continue
		default:
		}
		select {
		case <-owner.done:
		case <-ctx.Done():
			unresolved = append(unresolved, fmt.Errorf("terminal join deadline: unresolved %s: %w", owner.name, ctx.Err()))
		}
	}
	return errors.Join(unresolved...)
}

func ownerLoadSeed[T any](parent context.Context, rows []T, workers, queueSize int, budget time.Duration,
	put func(context.Context, T) error, identify func(T) (fixture, bucket, filter, operation string),
	report func(ownerLoadFault)) (retErr error) {
	ctx, cancel := context.WithCancel(parent)
	jobs := make(chan T, queueSize)
	done := make(chan struct{})
	var once sync.Once
	failure := make(chan error, 1)
	record := func(row T, elapsed time.Duration, err error) {
		once.Do(func() {
			primary := fmt.Errorf("seed operation: %w", err)
			if report != nil {
				fixture, bucket, filter, operation := identify(row)
				report(ownerLoadFaultAt(ctx, "seed", fixture, bucket, filter, operation, elapsed, primary))
			}
			failure <- primary
			cancel()
		})
	}
	recordAdmission := func(err error) {
		once.Do(func() {
			primary := fmt.Errorf("seed admission: %w", err)
			if report != nil {
				report(ownerLoadFaultAt(ctx, "seed", "", "", "", "admission", 0, primary))
			}
			failure <- primary
			cancel()
		})
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case row, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					started := time.Now()
					if err := put(ctx, row); err != nil {
						record(row, time.Since(started), err)
						return
					}
				}
			}
		}()
	}
	go func() { wg.Wait(); close(done) }()
	defer func() {
		cancel()
		retErr = errors.Join(retErr, ownerLoadJoin(parent, budget, ownerLoadJoinOwner{"seed workers", done}))
		select {
		case primary := <-failure:
			retErr = errors.Join(primary, retErr)
		default:
		}
	}()
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			close(jobs)
			recordAdmission(err)
			return err
		}
		select {
		case <-ctx.Done():
			close(jobs)
			recordAdmission(ctx.Err())
			return ctx.Err()
		case jobs <- row:
		}
	}
	close(jobs)
	select {
	case <-done:
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		recordAdmission(err)
		return err
	}
	return nil
}

type ownerLoadJob struct{ Fixture, Serial int }
type ownerLoadListing struct {
	Count     int
	Duration  time.Duration
	Err       error
	Published bool
}
type ownerLoadResult struct {
	job ownerLoadJob
	got ownerLoadListing
}
type ownerLoadConcurrentOptions struct {
	Fixtures, Repetitions, Workers, ChurnPerWriter, QueueSize int
	TerminalBudget                                            time.Duration
	List                                                      func(context.Context, ownerLoadJob) ownerLoadListing
	Key                                                       func(ownerLoadJob) string
	Sample                                                    func(context.Context) (ownerLoadJob, error)
	Churn                                                     func(context.Context, int, int) error
	Describe                                                  func(ownerLoadJob) (fixture, bucket, filter string)
	Report                                                    func(ownerLoadFault)
}
type ownerLoadConcurrentOutcome struct {
	Durations      [][]time.Duration
	QueueHighWater int
	CatchUp        time.Duration
}

func ownerLoadQueueDepth[T any](dispatcher *keyedDispatcher[T]) int {
	depth := 0
	for _, lane := range dispatcher.lanes {
		depth += len(lane)
	}
	return depth
}

func ownerLoadConcurrent(parent context.Context, options ownerLoadConcurrentOptions) (out ownerLoadConcurrentOutcome, retErr error) {
	ctx, cancel := context.WithCancel(parent)
	failures := make(chan error, 1)
	var first sync.Once
	record := func(phase string, job ownerLoadJob, elapsed time.Duration, err error) {
		first.Do(func() {
			wrapped := fmt.Errorf("%s fixture=%d serial=%d: %w", phase, job.Fixture, job.Serial, err)
			if options.Report != nil {
				fixture, bucket, filter := options.Describe(job)
				options.Report(ownerLoadFaultAt(ctx, phase, fixture, bucket, filter,
					fmt.Sprintf("fixture=%d serial=%d", job.Fixture, job.Serial), elapsed, wrapped))
			}
			failures <- wrapped
			cancel()
		})
	}
	results := make(chan ownerLoadResult, options.Fixtures*options.Repetitions)
	dispatcher := newKeyedDispatcher(options.Workers, options.QueueSize,
		options.Key,
		func(runCtx context.Context, job ownerLoadJob) {
			got := options.List(runCtx, job)
			if got.Err != nil {
				record("list", job, got.Duration, got.Err)
			}
			if got.Published {
				results <- ownerLoadResult{job, got}
			}
		})
	var dispatcherDone, samplerDone, churnDone <-chan struct{}
	// Installed before any owner starts. One terminal budget covers every owner on every exit.
	defer func() {
		cancel()
		retErr = errors.Join(retErr, ownerLoadJoin(parent, options.TerminalBudget,
			ownerLoadJoinOwner{"dispatcher", dispatcherDone},
			ownerLoadJoinOwner{"sampler", samplerDone},
			ownerLoadJoinOwner{"churn", churnDone}))
		select {
		case err := <-failures:
			retErr = errors.Join(err, retErr)
		default:
		}
	}()
	dispatcher.Start(ctx)
	dispatcherDone = dispatcher.done
	samplerEnd := make(chan struct{})
	samplerDone = samplerEnd
	go func() {
		defer close(samplerEnd)
		for ctx.Err() == nil {
			started := time.Now()
			if job, err := options.Sample(ctx); err != nil {
				if ctx.Err() == nil {
					record("sample", job, time.Since(started), err)
				}
				return
			}
			runtime.Gosched()
		}
	}()
	churnEnd := make(chan struct{})
	churnDone = churnEnd
	var churnWG sync.WaitGroup
	for writer := range options.Workers {
		churnWG.Add(1)
		go func() {
			defer churnWG.Done()
			for iteration := range options.ChurnPerWriter {
				if ctx.Err() != nil {
					return
				}
				started := time.Now()
				if err := options.Churn(ctx, writer, iteration); err != nil {
					if ctx.Err() == nil {
						record("churn", ownerLoadJob{Fixture: writer % options.Fixtures, Serial: iteration}, time.Since(started), err)
					}
					return
				}
			}
		}()
	}
	go func() { churnWG.Wait(); close(churnEnd) }()
	closed := false
	closeLanes := func() {
		if closed {
			return
		}
		closed = true
		for _, lane := range dispatcher.lanes {
			close(lane)
		}
	}
	defer closeLanes()
	out.Durations = make([][]time.Duration, options.Fixtures)
	for i := range out.Durations {
		out.Durations[i] = make([]time.Duration, options.Repetitions)
	}
	started := time.Now()
	for serial := range options.Repetitions {
		for fixture := range options.Fixtures {
			job := ownerLoadJob{fixture, serial}
			if err := ctx.Err(); err != nil {
				record("submit", job, time.Since(started), err)
				return out, err
			}
			submitStarted := time.Now()
			if err := dispatcher.Submit(ctx, job); err != nil {
				record("submit", job, time.Since(submitStarted), err)
				return out, err
			}
			out.QueueHighWater = max(out.QueueHighWater, ownerLoadQueueDepth(dispatcher))
		}
	}
	closeLanes()
	if err := ownerLoadCollectResults(ctx, options, dispatcher, results, failures, &out, record, started); err != nil {
		return out, err
	}
	out.CatchUp = time.Since(started)
	select {
	case <-churnDone:
		if err := ctx.Err(); err != nil {
			record("churn", ownerLoadJob{Fixture: -1}, time.Since(started), err)
			return out, err
		}
	case err := <-failures:
		return out, err
	case <-ctx.Done():
		record("churn", ownerLoadJob{Fixture: -1}, time.Since(started), ctx.Err())
		return out, ctx.Err()
	}
	return out, nil
}

func ownerLoadCollectResults(ctx context.Context, options ownerLoadConcurrentOptions,
	dispatcher *keyedDispatcher[ownerLoadJob], results <-chan ownerLoadResult, failures <-chan error,
	out *ownerLoadConcurrentOutcome, record func(string, ownerLoadJob, time.Duration, error), started time.Time) error {
	remaining := options.Fixtures * options.Repetitions
	consume := func(value ownerLoadResult) error {
		remaining--
		if value.got.Err != nil {
			record("list", value.job, value.got.Duration, value.got.Err)
			return value.got.Err
		}
		if value.got.Count != 1 {
			err := fmt.Errorf("list count=%d, want 1", value.got.Count)
			record("validate", value.job, value.got.Duration, err)
			return err
		}
		if out.Durations[value.job.Fixture][value.job.Serial] != 0 {
			err := errors.New("duplicate listing result")
			record("validate", value.job, value.got.Duration, err)
			return err
		}
		out.Durations[value.job.Fixture][value.job.Serial] = value.got.Duration
		out.QueueHighWater = max(out.QueueHighWater, ownerLoadQueueDepth(dispatcher))
		return nil
	}
	for remaining > 0 {
		select {
		case value := <-results:
			if err := consume(value); err != nil {
				return err
			}
		case err := <-failures:
			return err
		case <-ctx.Done():
			record("result", ownerLoadJob{Fixture: -1}, time.Since(started), ctx.Err())
			return ctx.Err()
		case <-dispatcher.done:
			// Completion makes the publication set stable; drain it before classifying loss.
			for {
				select {
				case value := <-results:
					if err := consume(value); err != nil {
						return err
					}
				default:
					if remaining > 0 {
						err := fmt.Errorf("dispatcher completed with %d missing listing results", remaining)
						record("result", ownerLoadJob{Fixture: -1}, time.Since(started), err)
						return err
					}
					goto collected
				}
			}
		}
	}
collected:
	for fixture, samples := range out.Durations {
		for serial, sample := range samples {
			if sample == 0 {
				err := fmt.Errorf("fixture=%d serial=%d missing duration", fixture, serial)
				record("result", ownerLoadJob{fixture, serial}, time.Since(started), err)
				return err
			}
		}
	}
	return nil
}

func ownerLoadConverge(parent context.Context, window, interval time.Duration, baseline int,
	info func(context.Context) (int, error),
	onFailure func(context.Context, int, int, error, error)) (attempts, lastCount int, lastErr error, err error) {
	ctx, cancel := context.WithTimeout(parent, window)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	fail := func() (int, int, error, error) {
		failure := fmt.Errorf("consumer convergence attempts=%d last_count=%d last_error=%v cause=%w", attempts, lastCount, lastErr, ctx.Err())
		if onFailure != nil {
			onFailure(ctx, attempts, lastCount, lastErr, failure)
		}
		return attempts, lastCount, lastErr, failure
	}
	for {
		if ctx.Err() != nil {
			return fail()
		}
		attempts++
		lastCount, lastErr = info(ctx)
		if ctx.Err() != nil {
			return fail()
		}
		if lastErr == nil && lastCount == baseline {
			return attempts, lastCount, nil, nil
		}
		select {
		case <-ctx.Done():
			return fail()
		case <-ticker.C:
		}
	}
}
