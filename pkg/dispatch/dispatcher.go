package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/worker"
)

// BoundedDispatcher is the framework's bounded-concurrency parallel
// work primitive. Components compose it for internal fan-out over
// known work lists; optionally with KV-twofer-aware completion
// handling when the work involves async writes by other processors.
//
// Generic over work type W — work items can be any Go type. The
// underlying pkg/worker.Pool[W] enforces the bounded queue +
// non-blocking submit + graceful shutdown semantics; this wrapper
// adds the optional KV-twofer-completion overlay.
//
// Concurrency: Submit is safe to call from concurrent goroutines
// (delegates to pool.Submit which is lock-free via channel send).
// Stop is idempotent. Stats reads atomic counters via the underlying
// pool.
type BoundedDispatcher[W any] struct {
	pool       *worker.Pool[W]
	completion *completionWatcher[W] // nil when no CompletionKVBucket configured
	logger     *slog.Logger
}

// Config parameterizes BoundedDispatcher construction. The zero
// value is NOT valid — New rejects Workers <= 0, missing Process,
// and partial completion-watcher configuration.
type Config[W any] struct {
	// Workers is the bounded concurrency target. The dispatcher
	// never runs more than Workers Process calls in flight at once.
	// Must be > 0.
	Workers int

	// QueueSize bounds the submit queue. Submit returns
	// ErrQueueFull when the queue is at capacity. Must be > 0.
	QueueSize int

	// Process is called for each submitted work item, in one of
	// the worker goroutines. Must not be nil.
	Process func(ctx context.Context, work W) error

	// CompletionKVBucket — optional. When set, the dispatcher
	// subscribes to this bucket via natsclient and tracks
	// completion signals for each submitted work item. The bucket
	// must exist before New is called; the dispatcher does NOT
	// create it. Required if CompletionKeyForWorkItem or
	// OnComplete are set; the three fields are all-or-nothing.
	CompletionKVBucket string

	// CompletionKeyForWorkItem — required if CompletionKVBucket is
	// set. Returns the KV key the dispatcher watches for this work
	// item's completion. The function must be pure (no per-call
	// state) because it's called on Submit AND on each KV-watch
	// update for matching.
	CompletionKeyForWorkItem func(W) string

	// OnComplete — required if CompletionKVBucket is set. Called
	// when CompletionKVBucket has a write at the key returned by
	// CompletionKeyForWorkItem for some tracked work item.
	// Invoked from the dispatcher's watcher goroutine; callers
	// must not block on shared mutexes acquired by Submit.
	OnComplete func(ctx context.Context, work W) error
}

// Deps carries the framework-provided dependencies the dispatcher
// needs at runtime. Logger is optional (falls back to slog.Default);
// NATSClient is required when CompletionKVBucket is configured.
type Deps struct {
	NATSClient *natsclient.Client
	Logger     *slog.Logger
}

// New constructs a BoundedDispatcher with the given Config + Deps.
// Returns ErrInvalidConfig for missing required fields, or
// ErrNATSClientRequired when CompletionKVBucket is set without a
// natsclient.
//
// If CompletionKVBucket is configured, New blocks briefly to
// resolve the bucket via natsclient and start the completion-
// watcher goroutine. Subsequent Submit calls register their
// tracking entries against the live watcher.
//
// The pool is started immediately — callers don't need to call
// Start. Workers are running and ready to receive Submit calls when
// New returns. The ctx must be non-nil.
func New[W any](ctx context.Context, cfg Config[W], deps Deps) (*BoundedDispatcher[W], error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidConfig)
	}
	if cfg.Workers <= 0 {
		return nil, fmt.Errorf("%w: Workers must be > 0 (got %d)", ErrInvalidConfig, cfg.Workers)
	}
	if cfg.QueueSize <= 0 {
		return nil, fmt.Errorf("%w: QueueSize must be > 0 (got %d)", ErrInvalidConfig, cfg.QueueSize)
	}
	if cfg.Process == nil {
		return nil, fmt.Errorf("%w: Process is required", ErrInvalidConfig)
	}
	// Completion-watcher config is all-or-nothing.
	hasBucket := cfg.CompletionKVBucket != ""
	hasKeyFunc := cfg.CompletionKeyForWorkItem != nil
	hasOnComplete := cfg.OnComplete != nil
	if hasBucket || hasKeyFunc || hasOnComplete {
		if !hasBucket || !hasKeyFunc || !hasOnComplete {
			return nil, fmt.Errorf("%w: completion-watcher requires all of CompletionKVBucket / CompletionKeyForWorkItem / OnComplete (got bucket=%v key=%v onComplete=%v)",
				ErrInvalidConfig, hasBucket, hasKeyFunc, hasOnComplete)
		}
		if deps.NATSClient == nil {
			return nil, ErrNATSClientRequired
		}
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	pool := worker.NewPool[W](cfg.Workers, cfg.QueueSize, cfg.Process)
	if err := pool.Start(ctx); err != nil {
		return nil, fmt.Errorf("dispatch: pool start: %w", err)
	}

	d := &BoundedDispatcher[W]{
		pool:   pool,
		logger: logger,
	}

	if hasBucket {
		watcher, err := newCompletionWatcher(ctx, completionWatcherConfig[W]{
			natsClient: deps.NATSClient,
			bucket:     cfg.CompletionKVBucket,
			keyFor:     cfg.CompletionKeyForWorkItem,
			onComplete: cfg.OnComplete,
			logger:     logger,
		})
		if err != nil {
			// Roll back the pool so we don't leak workers if the
			// watcher setup fails. Best-effort — pool.Stop with a
			// short timeout to keep New synchronous.
			_ = pool.Stop(5 * time.Second)
			return nil, fmt.Errorf("dispatch: completion watcher: %w", err)
		}
		d.completion = watcher
	}

	return d, nil
}

// Submit queues a work item. Returns ErrQueueFull if the queue is
// at capacity. When a completion watcher is active, Submit also
// registers the work item in the watcher's tracking map BEFORE
// enqueuing to the pool so a completion signal that arrives between
// Submit and Process can't slip past the watcher.
//
// Submit is safe to call from concurrent goroutines.
func (d *BoundedDispatcher[W]) Submit(work W) error {
	if d.completion != nil {
		d.completion.track(work)
	}
	if err := d.pool.Submit(work); err != nil {
		// Rollback the tracking registration so a Submit that fails
		// because the queue is full doesn't leak the entry forever.
		// The completion watcher would otherwise hold the entry
		// until OnComplete fires (which it won't — the work never
		// ran).
		if d.completion != nil {
			d.completion.forget(work)
		}
		return err
	}
	return nil
}

// Stop halts the dispatcher gracefully. Drains the underlying
// worker pool first (blocks until in-flight Process calls complete
// or the given context expires), then stops the completion watcher
// (if configured). Subsequent Submit calls return ErrStopped.
//
// Stop is idempotent. Multiple calls return nil after the first
// successful Stop.
//
// Callers waiting on KV-triggered OnComplete callbacks should
// ensure those complete before Stop returns — typically by
// canceling the caller's own context and letting Process see the
// cancellation.
func (d *BoundedDispatcher[W]) Stop(ctx context.Context) error {
	// Compute pool-stop timeout from the context's deadline. The
	// underlying pool.Stop takes a duration not a ctx (legacy API),
	// so this is the translation layer.
	//
	// Three cases:
	//   - ctx has a deadline in the future → use the remaining time
	//   - ctx has a deadline already in the past → honor caller's
	//     intent ("stop immediately") by passing 0 to pool.Stop;
	//     it will return ErrStopTimeout almost immediately, the
	//     watcher still stops below
	//   - ctx has no deadline (context.Background or context.TODO)
	//     → fall back to a reasonable 30s default; Debug log so
	//     operators tracing slow shutdowns can correlate
	const noDeadlineDefault = 30 * time.Second
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			timeout = remaining
		}
		// remaining <= 0 → timeout stays 0 → pool.Stop hits its
		// timeout branch immediately (caller's stated intent).
	} else {
		timeout = noDeadlineDefault
		d.logger.Debug("dispatch: Stop using default pool-stop timeout (no ctx deadline)",
			slog.Duration("timeout", timeout))
	}
	if err := d.pool.Stop(timeout); err != nil && !errors.Is(err, worker.ErrPoolStopped) {
		// Pool stop error — log but continue to shut down the
		// watcher so we don't leak its goroutine.
		d.logger.Warn("dispatch: pool stop returned error; continuing to shut down watcher",
			slog.String("error", err.Error()))
	}
	if d.completion != nil {
		d.completion.stop()
	}
	return nil
}

// Stats returns current dispatcher statistics via the underlying
// pool. Thread-safe; reads atomic counters.
func (d *BoundedDispatcher[W]) Stats() worker.PoolStats {
	return d.pool.Stats()
}
