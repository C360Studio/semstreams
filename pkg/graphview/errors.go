package graphview

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by view operations. Failed-state errors wrap BOTH
// ErrNotReady and ErrWatcherLost so callers can gate on not-ready semantics
// or distinguish "wait for bootstrap" from "the watcher died; restart".
var (
	// ErrNotReady gates attach and point reads before the initial WatchAll
	// replay has completed (readiness = caught-up, not started) and after a
	// watcher loss (fail-closed is a not-ready state).
	ErrNotReady = errors.New("graphview: view not ready")

	// ErrWatcherLost marks the fail-closed state after the shared watcher
	// died (updates channel closed, unrecoverable error, or context
	// cancellation). The frozen projection is never served as live.
	ErrWatcherLost = errors.New("graphview: watcher lost")

	// ErrViewStopped is returned by every operation after Stop, and is the
	// terminal reason reported by Subscription.Err after a view shutdown.
	ErrViewStopped = errors.New("graphview: view stopped")

	// ErrKeyNotFound is returned by Get for a key absent from the projection.
	ErrKeyNotFound = errors.New("graphview: key not found")

	// ErrAlreadyStarted is returned by Start on a view that already started.
	ErrAlreadyStarted = errors.New("graphview: view already started")
)

// PoisonError is the typed per-key poison signal (G6, ADR-079 semantics): a
// stored value for Key failed the injected validating decode at Revision. It
// is surfaced in the delta lane (Delta.Op == DeltaPoison, Delta.Err), on
// point reads of the poisoned key, and in Snapshot.Poisoned — never as a
// normal upsert. It wraps the decode/contract error so consumer poison
// latches can errors.As through to their contract-error types.
type PoisonError struct {
	// Key is the poisoned KV key.
	Key string
	// Revision is the KV revision whose value failed decode.
	Revision uint64
	// Err is the decode/contract error from the injected DecodeFunc.
	Err error
}

// Error implements error.
func (e *PoisonError) Error() string {
	return fmt.Sprintf("graphview: poisoned key %q at revision %d: %v", e.Key, e.Revision, e.Err)
}

// Unwrap exposes the underlying decode/contract error for errors.Is/As.
func (e *PoisonError) Unwrap() error { return e.Err }
