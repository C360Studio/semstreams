package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// completionWatcher subscribes to a KV bucket and fires OnComplete
// for each tracked work item whose key receives a write. Internal
// to BoundedDispatcher — callers don't construct it directly.
//
// Tracking semantics: Submit calls track(work) BEFORE enqueuing
// the work to the pool, so a completion signal that arrives between
// Submit and Process always finds its tracking entry. KV-watch
// bootstrap-replay events (pre-existing keys that match no tracked
// work) are silently ignored — the watcher is for NEW submissions,
// not historical replay.
//
// Concurrency: the in-flight map is protected by a RWMutex. The
// watcher goroutine reads the map per update; track / forget take
// the write lock. Read-heavy by design (typical workload has more
// KV updates than Submits).
type completionWatcher[W any] struct {
	bucket     jetstream.KeyValue
	keyFor     func(W) string
	onComplete func(ctx context.Context, work W) error
	logger     *slog.Logger

	cancel context.CancelFunc

	mu       sync.RWMutex
	inFlight map[string]W // key (from keyFor(work)) → work item

	wg sync.WaitGroup
}

type completionWatcherConfig[W any] struct {
	natsClient *natsclient.Client
	bucket     string
	keyFor     func(W) string
	onComplete func(ctx context.Context, work W) error
	logger     *slog.Logger
}

// newCompletionWatcher opens the KV bucket, starts the watcher
// goroutine, and returns the live watcher ready for track calls.
// Returns an error if the bucket can't be opened.
//
// The watcher's lifecycle is derived from the caller's context so
// caller cancellation and Stop both terminate the watcher cleanly.
func newCompletionWatcher[W any](ctx context.Context, cfg completionWatcherConfig[W]) (*completionWatcher[W], error) {
	bucket, err := cfg.natsClient.GetKeyValueBucket(ctx, cfg.bucket)
	if err != nil {
		return nil, fmt.Errorf("open KV bucket %q: %w", cfg.bucket, err)
	}

	watcherCtx, cancel := context.WithCancel(ctx)
	w := &completionWatcher[W]{
		bucket:     bucket,
		keyFor:     cfg.keyFor,
		onComplete: cfg.onComplete,
		logger:     cfg.logger,
		cancel:     cancel,
		inFlight:   make(map[string]W),
	}

	// Start jetstream WatchAll BEFORE returning so the watcher is
	// already subscribed when the caller starts Submit-ing. Any
	// completion signals that arrive between Submit and Process
	// are guaranteed to find their tracking entries.
	kvWatcher, err := bucket.WatchAll(watcherCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("KV WatchAll %q: %w", cfg.bucket, err)
	}

	w.wg.Add(1)
	go w.run(watcherCtx, kvWatcher)
	return w, nil
}

// track registers work as in-flight under its keyFor key. Called
// from BoundedDispatcher.Submit BEFORE enqueuing to the pool.
func (w *completionWatcher[W]) track(work W) {
	key := w.keyFor(work)
	w.mu.Lock()
	w.inFlight[key] = work
	w.mu.Unlock()
}

// forget removes work from the in-flight map. Called from
// BoundedDispatcher.Submit on pool.Submit failure (rollback) AND
// from the watcher loop after firing OnComplete (cleanup).
func (w *completionWatcher[W]) forget(work W) {
	key := w.keyFor(work)
	w.mu.Lock()
	delete(w.inFlight, key)
	w.mu.Unlock()
}

// run is the watcher goroutine. Reads jetstream watch updates,
// looks up each one in the in-flight map, fires OnComplete for
// matches. Exits when ctx is canceled or the watcher channel
// closes.
func (w *completionWatcher[W]) run(ctx context.Context, kvWatcher jetstream.KeyWatcher) {
	defer w.wg.Done()
	defer func() { _ = kvWatcher.Stop() }()

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-kvWatcher.Updates():
			if !ok {
				// Channel closed by jetstream — usually means the
				// watcher errored or the connection dropped. Log
				// and exit; caller's next operation surfaces the
				// dispatcher's broken state.
				w.logger.Warn("dispatch: completion watcher channel closed")
				return
			}
			if entry == nil {
				// jetstream sends nil at end-of-bootstrap-replay.
				// We don't expose this signal to OnComplete (the
				// dispatcher is for forward Submits, not historical
				// replay); skip.
				continue
			}
			w.handleUpdate(ctx, entry)
		}
	}
}

// handleUpdate processes one KV-watch entry: looks up the matching
// work item in the in-flight map; fires OnComplete and removes the
// tracking entry on match. Silent on no-match (bootstrap replay,
// out-of-band writes by other producers).
func (w *completionWatcher[W]) handleUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	key := entry.Key()
	w.mu.RLock()
	work, tracked := w.inFlight[key]
	w.mu.RUnlock()
	if !tracked {
		// Common case for bootstrap replay and out-of-band writes.
		// Debug log lets operators trace if they suspect the
		// dispatcher is missing completions.
		w.logger.Debug("dispatch: completion-watcher update for untracked key",
			slog.String("key", key),
			slog.Uint64("revision", entry.Revision()))
		return
	}

	// OnComplete may take time (logging, downstream notification);
	// don't hold any locks during the call. Symmetric "completion-
	// attempt fired" policy: the tracking entry is dropped after
	// OnComplete returns regardless of whether OnComplete errored
	// — the completion signal was observed, and the dispatcher's
	// contract is "fire OnComplete once per matching KV write,"
	// not "fire until OnComplete succeeds." Callers wanting retry
	// semantics handle them inside OnComplete or by re-submitting.
	// Panics: NOT recovered here; they propagate up the watcher
	// goroutine and crash the process (matches Go convention —
	// uncaught panics in framework goroutines are programming
	// errors, not runtime conditions to swallow).
	if err := w.onComplete(ctx, work); err != nil {
		w.logger.Warn("dispatch: OnComplete returned error (tracking entry dropped anyway — completion signal was observed)",
			slog.String("key", key),
			slog.String("error", err.Error()))
	}
	w.mu.Lock()
	delete(w.inFlight, key)
	w.mu.Unlock()
}

// stop cancels the watcher's context and observes the run loop under ctx.
// A context error means completion was not observed; callers must treat that
// failed Stop as terminal rather than retrying it.
func (w *completionWatcher[W]) stop(ctx context.Context) error {
	w.cancel()
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// inFlightCount returns the current number of tracked work items.
// Test-helper; not part of the public dispatcher surface.
func (w *completionWatcher[W]) inFlightCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.inFlight)
}
