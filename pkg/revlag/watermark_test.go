package revlag

import (
	"sync"
	"testing"
	"time"
)

// The watermark's whole reason to exist is honesty under SPARSE, out-of-order,
// multi-path completion. These tests feed sparse ascending revision streams (e.g.
// {50, 72, 100}) — NOT dense 1..N — because History=1 + DeliverLastPerSubject never
// delivers a dense set, and the rejected "advance-from-Indexed+1" algorithm would
// stall on the very first gap.

// base is a fixed origin for synthetic KV commit timestamps: every test timestamp is
// base.Add(rev * time.Second), so a commit time is trivially attributable to the
// revision that carried it and no test depends on the wall clock.
var base = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// commitAt is the synthetic commit time for revision r (ascending with revision, as
// real KV commit times are).
func commitAt(r uint64) time.Time { return base.Add(time.Duration(r) * time.Second) }

func TestWatermark_ColdStart(t *testing.T) {
	w := New()
	if got := w.Indexed(); got != 0 {
		t.Fatalf("cold start Indexed = %d, want 0", got)
	}
	// Nothing observed → no covered revision → the commit time is ZERO, which the
	// contract says callers must read as "staleness not computable", never as
	// "zero staleness" (a fabricated 0 would read as a perfectly fresh view).
	indexed, at := w.IndexedAt()
	if indexed != 0 || !at.IsZero() {
		t.Fatalf("cold start IndexedAt = (%d, %v), want (0, zero time)", indexed, at)
	}
}

func TestWatermark_SparseMidBuild_LowWaterOfPending(t *testing.T) {
	w := New()
	// Sparse ascending delivery: distinct keys at purged-gap revisions.
	w.Observe(50, "A", commitAt(50))
	w.Observe(72, "B", commitAt(72))
	w.Observe(100, "C", commitAt(100))

	// Nothing completed: Indexed is minPending-1, NOT stuck at 0 waiting for rev 1.
	if got := w.Indexed(); got != 49 {
		t.Fatalf("all pending: Indexed = %d, want 49 (minPending-1)", got)
	}
	// Floor 49 covers no DELIVERED revision (50 is the lowest ever delivered), so the
	// covered commit time is still unknown.
	if _, at := w.IndexedAt(); !at.IsZero() {
		t.Fatalf("floor below every delivered revision: commit time = %v, want zero", at)
	}

	// Out-of-order completion: a fast LATE worker (rev 72) cannot report the gap done.
	w.Complete("B", 72)
	if got := w.Indexed(); got != 49 {
		t.Fatalf("after completing 72 (50 still pending): Indexed = %d, want 49", got)
	}

	// Complete the low revision: watermark advances past it to the next gap.
	w.Complete("A", 50)
	if got := w.Indexed(); got != 99 {
		t.Fatalf("after completing 50 (100 still pending): Indexed = %d, want 99", got)
	}
	// Documented conservatism: 72 completed while it sat ABOVE the then-floor (49),
	// so its timestamp was dropped rather than retained. The covered time is 50's —
	// OLDER than the true newest-covered (72), i.e. staleness over-reports, never
	// under-reports. Retaining it would mean a map that grows with completions.
	if _, at := w.IndexedAt(); !at.Equal(commitAt(50)) {
		t.Fatalf("covered commit time = %v, want %v (conservative: 72's was dropped)", at, commitAt(50))
	}

	// Complete the last: pending empties → Indexed jumps to observedHigh (=100),
	// honestly skipping the purged 51..71, 73..99 that were never delivered.
	w.Complete("C", 100)
	if got := w.Indexed(); got != 100 {
		t.Fatalf("caught up: Indexed = %d, want 100 (observedHigh)", got)
	}
	// Caught up is the EXACT case: the floor is observedHigh, which was itself
	// observed, so its own commit time is the answer.
	indexed, at := w.IndexedAt()
	if indexed != 100 || !at.Equal(commitAt(100)) {
		t.Fatalf("caught up IndexedAt = (%d, %v), want (100, %v)", indexed, at, commitAt(100))
	}
}

func TestWatermark_CoalescerCollapse_KeyScopedDrainsLowerRevisions(t *testing.T) {
	w := New()
	// One key delivered three times (the coalescer will re-Get only the latest, 30).
	w.Observe(10, "K", commitAt(10))
	w.Observe(20, "K", commitAt(20))
	w.Observe(30, "K", commitAt(30))
	if got := w.Indexed(); got != 9 {
		t.Fatalf("three pending revs of K: Indexed = %d, want 9", got)
	}

	// The single completion at the re-Got latest revision drains ALL collapsed lower
	// revisions of K that no worker ever sees individually. Exact-revision completion
	// would strand 10 and 20 forever.
	w.Complete("K", 30)
	if got := w.Indexed(); got != 30 {
		t.Fatalf("after key-scoped Complete(K,30): Indexed = %d, want 30", got)
	}
	// A collapsed multi-revision drain must carry the NEWEST collapsed revision's
	// commit time — the view really does reflect the world as of rev 30, and taking
	// 10's time here would over-report staleness by the whole coalesce window.
	if _, at := w.IndexedAt(); !at.Equal(commitAt(30)) {
		t.Fatalf("coalesced drain commit time = %v, want %v", at, commitAt(30))
	}
}

// TestWatermark_CoalescerCollapse_MidBuildTakesNewestCovered pins the collapsed-drain
// timestamp while the pipeline is NOT caught up (a higher key is still pending), so
// the answer comes from the drained set rather than from the pending-empty shortcut.
func TestWatermark_CoalescerCollapse_MidBuildTakesNewestCovered(t *testing.T) {
	w := New()
	w.Observe(10, "K", commitAt(10))
	w.Observe(20, "K", commitAt(20))
	w.Observe(30, "K", commitAt(30))
	w.Observe(40, "J", commitAt(40)) // still in flight; pins the floor at 39

	w.Complete("K", 30)
	if got := w.Indexed(); got != 39 {
		t.Fatalf("Indexed = %d, want 39 (J@40 pending)", got)
	}
	if _, at := w.IndexedAt(); !at.Equal(commitAt(30)) {
		t.Fatalf("commit time = %v, want %v (newest drained revision <= floor)", at, commitAt(30))
	}
}

func TestWatermark_DeleteSupersedesPendingUpdate(t *testing.T) {
	w := New()
	// Update K@10 is delivered and in flight...
	w.Observe(10, "K", commitAt(10))
	// ...then a delete K@11 arrives. The delete's key-scoped completion drains the
	// tombstone AND the earlier still-pending update it supersedes.
	w.Observe(11, "K", commitAt(11))
	w.Complete("K", 11)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("after delete supersedes pending update: Indexed = %d, want 11", got)
	}
	if _, at := w.IndexedAt(); !at.Equal(commitAt(11)) {
		t.Fatalf("commit time = %v, want %v (the tombstone's own commit)", at, commitAt(11))
	}
}

func TestWatermark_CompletionIsKeyScoped(t *testing.T) {
	w := New()
	w.Observe(10, "A", commitAt(10))
	w.Observe(11, "B", commitAt(11)) // a delete of a DIFFERENT key
	// Completing B must NOT drain A's pending revision even though 10 <= 11.
	w.Complete("B", 11)
	if got := w.Indexed(); got != 9 {
		t.Fatalf("key-scoped: completing B left A@10 pending, Indexed = %d, want 9", got)
	}
	w.Complete("A", 10)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("after A completes too: Indexed = %d, want 11 (observedHigh)", got)
	}
	if _, at := w.IndexedAt(); !at.Equal(commitAt(11)) {
		t.Fatalf("commit time = %v, want %v (caught up → observedHigh's commit)", at, commitAt(11))
	}
}

func TestWatermark_TrailingDeleteOnlyTail(t *testing.T) {
	w := New()
	w.Observe(10, "A", commitAt(10))
	w.Complete("A", 10)
	if got := w.Indexed(); got != 10 {
		t.Fatalf("after A: Indexed = %d, want 10", got)
	}
	// A tail of pure deletes must still advance the watermark — their sequences
	// participate, or lag wedges forever.
	w.Observe(11, "B", commitAt(11))
	w.Complete("B", 11)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("trailing delete: Indexed = %d, want 11", got)
	}
	if _, at := w.IndexedAt(); !at.Equal(commitAt(11)) {
		t.Fatalf("commit time = %v, want %v", at, commitAt(11))
	}
}

// TestWatermark_ObserveDoesNotRegressCoveredTime pins the seam between a caught-up
// view and the next delivery: observing a NEW revision moves the floor from
// observedHigh down to r-1, which still covers the previously-covered revision, so
// the covered commit time must stay put (it must not reset to zero and read as
// "staleness not computable" on every single write).
func TestWatermark_ObserveDoesNotRegressCoveredTime(t *testing.T) {
	w := New()
	w.Observe(10, "A", commitAt(10))
	w.Complete("A", 10)

	w.Observe(50, "B", commitAt(50)) // in flight; floor drops to 49
	indexed, at := w.IndexedAt()
	if indexed != 49 {
		t.Fatalf("Indexed = %d, want 49", indexed)
	}
	if !at.Equal(commitAt(10)) {
		t.Fatalf("commit time = %v, want %v (rev 10 is still covered by floor 49)", at, commitAt(10))
	}
}

func TestWatermark_RevisionZeroIgnored(t *testing.T) {
	w := New()
	w.Observe(0, "X", commitAt(1)) // "no revision" — stream sequences start at 1
	if got := w.Indexed(); got != 0 {
		t.Fatalf("Observe(0) should be a no-op, Indexed = %d, want 0", got)
	}
	if _, at := w.IndexedAt(); !at.IsZero() {
		t.Fatalf("Observe(0) must not record a commit time, got %v", at)
	}
	w.Observe(5, "A", commitAt(5))
	if got := w.Indexed(); got != 4 {
		t.Fatalf("after Observe(5): Indexed = %d, want 4", got)
	}
}

// TestWatermark_ZeroCommitTimeStaysUnknown covers the legacy/no-timestamp path: a
// caller that cannot supply a commit time leaves the covered time zero rather than
// fabricating one, so downstream reports "not computable" instead of "0 ms stale".
func TestWatermark_ZeroCommitTimeStaysUnknown(t *testing.T) {
	w := New()
	w.Observe(7, "A", time.Time{})
	w.Complete("A", 7)
	indexed, at := w.IndexedAt()
	if indexed != 7 {
		t.Fatalf("Indexed = %d, want 7", indexed)
	}
	if !at.IsZero() {
		t.Fatalf("commit time = %v, want zero (unknown, not fabricated)", at)
	}
}

func TestWatermark_CompleteUnobservedRevisionDrainsLower(t *testing.T) {
	// A coalescer re-Get can return a revision NEWER than any yet delivered to the
	// watch (a write between snapshot and Get). Completing at that not-yet-observed
	// revision must still drain the key's lower pending, and must NOT advance
	// observedHigh (only Observe does that).
	w := New()
	w.Observe(10, "K", commitAt(10))
	w.Complete("K", 40) // 40 never observed
	if got := w.Indexed(); got != 10 {
		t.Fatalf("Indexed = %d, want 10 (observedHigh unchanged; 10 drained)", got)
	}
	// The commit time is 10's — never 40's, whose entry was never delivered and whose
	// commit time the watermark has never seen.
	if _, at := w.IndexedAt(); !at.Equal(commitAt(10)) {
		t.Fatalf("commit time = %v, want %v", at, commitAt(10))
	}
}

// TestWatermark_MemoryDoesNotGrowWithCompletions is the structural guard behind the
// "carry forward one scalar, not a map of completed revisions" decision: after N
// observe/complete cycles the ONLY per-revision map must be empty, so retained
// memory tracks in-flight work rather than cumulative throughput. It asserts on the
// internal map directly (same package) because that is the thing that would grow.
func TestWatermark_MemoryDoesNotGrowWithCompletions(t *testing.T) {
	w := New()
	const n = 20000
	for i := uint64(1); i <= n; i++ {
		w.Observe(i, "K", commitAt(i))
		w.Complete("K", i)
	}
	w.mu.Lock()
	pendingLen := len(w.pending)
	w.mu.Unlock()
	if pendingLen != 0 {
		t.Fatalf("pending retained %d entries after draining every revision, want 0", pendingLen)
	}
	indexed, at := w.IndexedAt()
	if indexed != n || !at.Equal(commitAt(n)) {
		t.Fatalf("IndexedAt = (%d, %v), want (%d, %v)", indexed, at, n, commitAt(n))
	}

	// Same guard with a slow key pinning the floor: the completions that pile up
	// ABOVE it must not be retained either (that is the case a "completed revisions"
	// map would grow without bound).
	w2 := New()
	w2.Observe(1, "SLOW", commitAt(1))
	for i := uint64(2); i <= n; i++ {
		w2.Observe(i, "K", commitAt(i))
		w2.Complete("K", i)
	}
	w2.mu.Lock()
	pendingLen = len(w2.pending)
	w2.mu.Unlock()
	if pendingLen != 1 {
		t.Fatalf("pending retained %d entries with one key stuck, want 1 (the stuck key only)", pendingLen)
	}
	if got := w2.Indexed(); got != 0 {
		t.Fatalf("Indexed = %d, want 0 (SLOW@1 pins the floor)", got)
	}
}

// TestWatermark_Concurrent runs Observe/Complete across many goroutines to prove the
// mutex holds and that draining every observed revision leaves Indexed at
// observedHigh (run under -race). Mirrors the real wiring: the watch goroutine
// Observes ascending; N workers Complete out of order.
func TestWatermark_Concurrent(t *testing.T) {
	w := New()
	const n = 2000

	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = string(rune('a'+i%26)) + "-" + time.Duration(i).String()
		w.Observe(uint64(i+1), keys[i], commitAt(uint64(i+1)))
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(rev int) {
			defer wg.Done()
			w.Complete(keys[rev], uint64(rev+1))
		}(i)
	}
	wg.Wait()

	indexed, at := w.IndexedAt()
	if indexed != uint64(n) {
		t.Fatalf("after draining all: Indexed = %d, want %d", indexed, n)
	}
	// Fully drained → the exact caught-up answer, regardless of completion order.
	if !at.Equal(commitAt(n)) {
		t.Fatalf("after draining all: commit time = %v, want %v", at, commitAt(n))
	}
}
