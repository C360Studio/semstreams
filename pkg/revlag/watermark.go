// Package revlag provides a revision-lag "caught up" watermark for async,
// write-driven, lagging pipelines fed by a NATS KV watch (ADR-066).
//
// A component that watches a KV bucket and processes each entry through a pool
// (with optional coalescing and inline deletes) needs to answer "have I caught up
// to the latest committed write?" honestly — not "have I started?". The Watermark
// tracks that as revision lag: Observe every delivered revision, Complete it when
// its processing reaches a terminal outcome, and read Indexed() — the highest
// revision such that every delivered revision <= it is done and nothing <= it is
// still in flight.
package revlag

import "sync"

// Watermark is the low-water-of-pending "caught up" tracker. It answers "every
// DELIVERED revision <= Indexed() has completed and nothing <= it is still in
// flight." It is deliberately defined only over DELIVERED revisions, so it is
// correct even when the delivered revision set is SPARSE — as it is for a NATS KV
// bucket at History=1, where WatchAll delivers via OrderedConsumer +
// DeliverLastPerSubject and superseded revisions are purged and never delivered.
// The rejected "advance from Indexed+1 past every contiguous completion" would
// stall forever on the first purged gap.
//
// Correctness rests on ONE property, which OrderedConsumer guarantees at the
// nats-server storage layer for both the bootstrap replay and live updates:
// delivery is monotonic ascending in the revision passed to Observe. Any
// un-observed revision below observedHigh is therefore purged (permanently absent)
// and correctly skipped.
//
// All methods are safe for concurrent use: the watch goroutine Observes; N pool
// workers, a coalescer callback, and/or an inline delete handler Complete; a status
// handler reads Indexed().
type Watermark struct {
	mu           sync.Mutex
	observedHigh uint64            // highest revision ever delivered
	pending      map[uint64]string // in-flight revision -> its key
}

// New returns an empty Watermark (Indexed() == 0 until the first Observe).
func New() *Watermark {
	return &Watermark{pending: make(map[uint64]string)}
}

// Observe records revision r (for key) as delivered-and-in-flight. Call it from the
// watch goroutine for EVERY delivered entry — updates AND deletes — before dispatch.
// observedHigh is monotonic; ascending delivery means r exceeds any prior, but max()
// is kept for defensiveness. Revision 0 ("no revision") is ignored — KV stream
// sequences start at 1.
func (w *Watermark) Observe(r uint64, key string) {
	if r == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if r > w.observedHigh {
		w.observedHigh = r
	}
	w.pending[r] = key
}

// Complete drains every in-flight revision for key with revision <= rev — the single
// key-scoped completion rule. A pool worker calls it with the entry it processed,
// which drains any coalescer-collapsed lower revisions of that key that no worker
// sees individually; an inline delete handler calls it with the tombstone's
// revision, which drains an earlier pending update the delete supersedes. Key-scoped
// (never global-<=-rev) so one key's completion cannot drop a different key's pending
// revision.
func (w *Watermark) Complete(key string, rev uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for r, k := range w.pending {
		if k == key && r <= rev {
			delete(w.pending, r)
		}
	}
}

// Indexed returns the low-water-of-pending watermark: observedHigh when nothing is in
// flight, else minPending-1. Every delivered revision <= the result is complete and
// nothing <= it is still pending.
func (w *Watermark) Indexed() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return w.observedHigh
	}
	minRev := ^uint64(0)
	for r := range w.pending {
		if r < minRev {
			minRev = r
		}
	}
	return minRev - 1
}
