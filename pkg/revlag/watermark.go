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
//
// It also answers "how OLD is the view I am serving?" (ADR-083): IndexedAt pairs
// the watermark with the KV COMMIT timestamp of the newest observed revision it
// covers, so a consumer can bound staleness in wall time instead of in revisions
// (a revision count is load- and coalesce-dependent; see gh#590).
package revlag

import (
	"sync"
	"time"
)

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
// and correctly skipped. That same property makes Indexed() monotonic
// non-decreasing, which is what lets the covered COMMIT TIME be carried forward as
// a single scalar (see coveredRev/coveredAt below) instead of a growing map.
//
// All methods are safe for concurrent use: the watch goroutine Observes; N pool
// workers, a coalescer callback, and/or an inline delete handler Complete; a status
// handler reads Indexed()/IndexedAt().
type Watermark struct {
	mu             sync.Mutex
	observedHigh   uint64    // highest revision ever delivered
	observedHighAt time.Time // commit time of observedHigh
	// pending maps an in-flight revision to its key and KV commit time. It is the
	// ONLY per-revision state; it drains to empty when the pipeline catches up, so
	// memory tracks in-flight work, never cumulative throughput.
	pending map[uint64]inflight
	// coveredRev is the newest revision known to be BOTH observed and covered by
	// the current Indexed() floor; coveredAt is its commit time. Carried forward as
	// one scalar pair rather than retaining timestamps for every completed revision
	// (which would grow without bound while one slow key pins the floor).
	//
	// It is advanced only from evidence in hand at drain time, so it can lag the
	// true newest-covered revision when completions land out of order ABOVE the
	// floor: those timestamps are dropped rather than retained. The resulting
	// commit time is therefore <= the true one, i.e. staleness computed from it is
	// never UNDER-reported — the fail-closed direction. The error is bounded by the
	// in-flight window and collapses to exact whenever the pipeline catches up
	// (pending empties → coveredRev = observedHigh).
	coveredRev uint64
	coveredAt  time.Time
}

// inflight is the per-pending-revision record: the key it belongs to (completion is
// key-scoped) and the KV entry's commit time (entry.Created()).
type inflight struct {
	key      string
	commitAt time.Time
}

// New returns an empty Watermark (Indexed() == 0 until the first Observe).
func New() *Watermark {
	return &Watermark{pending: make(map[uint64]inflight)}
}

// Observe records revision r (for key, committed at commitAt) as
// delivered-and-in-flight. Call it from the watch goroutine for EVERY delivered
// entry — updates AND deletes — before dispatch, passing the KV entry's COMMIT
// timestamp (entry.Created()), never the local arrival time: commit time is what
// makes staleness mean "the view reflects the world as of T" and correctly includes
// server-side delivery backlog once an entry lands. A zero commitAt is allowed (it
// simply leaves the covered timestamp unknown, which reads as "staleness not
// computable" downstream) so tests and legacy paths need not fabricate one.
//
// observedHigh is monotonic; ascending delivery means r exceeds any prior, but max()
// is kept for defensiveness. Revision 0 ("no revision") is ignored — KV stream
// sequences start at 1.
func (w *Watermark) Observe(r uint64, key string, commitAt time.Time) {
	if r == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if r > w.observedHigh {
		w.observedHigh = r
		w.observedHighAt = commitAt
	}
	w.pending[r] = inflight{key: key, commitAt: commitAt}
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
	var drained []uint64
	for r, in := range w.pending {
		if in.key == key && r <= rev {
			drained = append(drained, r)
		}
	}
	if len(drained) == 0 {
		return
	}
	// Capture the drained commit times BEFORE deleting, then advance the covered
	// floor over the post-drain state.
	times := make([]time.Time, len(drained))
	for i, r := range drained {
		times[i] = w.pending[r].commitAt
		delete(w.pending, r)
	}
	w.advanceCoveredLocked(drained, times)
}

// advanceCoveredLocked moves the covered (rev, commit time) pair forward using the
// revisions just drained. Caller holds w.mu.
//
// Two cases, mirroring Indexed():
//   - pending empty: the floor IS observedHigh, which was itself observed, so the
//     covered timestamp is exactly observedHighAt. This is the caught-up case an
//     operator sees most, and it is exact.
//   - pending non-empty: the floor is minPending-1 and the only newly-covered
//     revisions are those just drained at or below it. A revision drained EARLIER
//     while it sat above the then-floor is not reconsidered — that is the
//     deliberate memory/precision trade documented on coveredRev.
func (w *Watermark) advanceCoveredLocked(drained []uint64, times []time.Time) {
	if len(w.pending) == 0 {
		if w.observedHigh > w.coveredRev {
			w.coveredRev = w.observedHigh
			w.coveredAt = w.observedHighAt
		}
		return
	}
	floor := w.indexedLocked()
	for i, r := range drained {
		if r <= floor && r > w.coveredRev {
			w.coveredRev = r
			w.coveredAt = times[i]
		}
	}
}

// Indexed returns the low-water-of-pending watermark: observedHigh when nothing is in
// flight, else minPending-1. Every delivered revision <= the result is complete and
// nothing <= it is still pending.
func (w *Watermark) Indexed() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.indexedLocked()
}

// IndexedAt returns the Indexed() floor together with the COMMIT time of the newest
// observed revision that floor covers — the "the view reflects the world as of T"
// input to the ADR-083 staleness_ms field.
//
// The two are read under one lock so a caller cannot pair a floor with a timestamp
// from a different instant. commitAt is the ZERO time when the floor covers no
// observed revision (cold start, or a floor that sits below every delivered
// revision because the lowest one is still in flight) — callers MUST treat zero as
// "staleness not computable", never as "zero staleness". commitAt may be older than
// the floor's true coverage but never newer (see coveredRev).
func (w *Watermark) IndexedAt() (indexed uint64, commitAt time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.indexedLocked(), w.coveredAt
}

// indexedLocked is the Indexed() computation; caller holds w.mu.
func (w *Watermark) indexedLocked() uint64 {
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
