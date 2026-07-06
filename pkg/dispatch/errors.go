package dispatch

import (
	"errors"

	"github.com/c360studio/semstreams/pkg/worker"
)

// Package error sentinels. Callers compare with errors.Is.
var (
	// ErrInvalidConfig is returned by New when the Config is
	// internally inconsistent (e.g. Workers <= 0, missing Process,
	// CompletionKVBucket set without CompletionKeyForWorkItem +
	// OnComplete). Catches wiring bugs at construction time.
	ErrInvalidConfig = errors.New("dispatch: invalid config")

	// ErrQueueFull is the underlying pkg/worker.ErrQueueFull
	// re-exported so callers can compare via errors.Is without
	// reaching into pkg/worker directly.
	ErrQueueFull = worker.ErrQueueFull

	// ErrStopped is the underlying pkg/worker.ErrPoolStopped
	// re-exported for the same reason.
	ErrStopped = worker.ErrPoolStopped

	// ErrNATSClientRequired is returned by New when
	// CompletionKVBucket is configured but Deps.NATSClient is nil
	// (no way to subscribe to the bucket).
	ErrNATSClientRequired = errors.New("dispatch: Deps.NATSClient is required when CompletionKVBucket is set")

	// ErrLaneFull is returned by KeyedPool.Submit (the non-blocking
	// form) when the target lane's bounded queue is at capacity.
	// SubmitBlocking waits instead of returning this. Distinct from
	// ErrQueueFull (the BoundedDispatcher's single-queue variant) so a
	// caller can tell which primitive rejected the work.
	ErrLaneFull = errors.New("dispatch: lane queue full")
)
