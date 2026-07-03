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

func TestWatermark_ColdStart(t *testing.T) {
	w := New()
	if got := w.Indexed(); got != 0 {
		t.Fatalf("cold start Indexed = %d, want 0", got)
	}
}

func TestWatermark_SparseMidBuild_LowWaterOfPending(t *testing.T) {
	w := New()
	// Sparse ascending delivery: distinct keys at purged-gap revisions.
	w.Observe(50, "A")
	w.Observe(72, "B")
	w.Observe(100, "C")

	// Nothing completed: Indexed is minPending-1, NOT stuck at 0 waiting for rev 1.
	if got := w.Indexed(); got != 49 {
		t.Fatalf("all pending: Indexed = %d, want 49 (minPending-1)", got)
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

	// Complete the last: pending empties → Indexed jumps to observedHigh (=100),
	// honestly skipping the purged 51..71, 73..99 that were never delivered.
	w.Complete("C", 100)
	if got := w.Indexed(); got != 100 {
		t.Fatalf("caught up: Indexed = %d, want 100 (observedHigh)", got)
	}
}

func TestWatermark_CoalescerCollapse_KeyScopedDrainsLowerRevisions(t *testing.T) {
	w := New()
	// One key delivered three times (the coalescer will re-Get only the latest, 30).
	w.Observe(10, "K")
	w.Observe(20, "K")
	w.Observe(30, "K")
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
}

func TestWatermark_DeleteSupersedesPendingUpdate(t *testing.T) {
	w := New()
	// Update K@10 is delivered and in flight...
	w.Observe(10, "K")
	// ...then a delete K@11 arrives. The delete's key-scoped completion drains the
	// tombstone AND the earlier still-pending update it supersedes.
	w.Observe(11, "K")
	w.Complete("K", 11)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("after delete supersedes pending update: Indexed = %d, want 11", got)
	}
}

func TestWatermark_CompletionIsKeyScoped(t *testing.T) {
	w := New()
	w.Observe(10, "A")
	w.Observe(11, "B") // a delete of a DIFFERENT key
	// Completing B must NOT drain A's pending revision even though 10 <= 11.
	w.Complete("B", 11)
	if got := w.Indexed(); got != 9 {
		t.Fatalf("key-scoped: completing B left A@10 pending, Indexed = %d, want 9", got)
	}
	w.Complete("A", 10)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("after A completes too: Indexed = %d, want 11 (observedHigh)", got)
	}
}

func TestWatermark_TrailingDeleteOnlyTail(t *testing.T) {
	w := New()
	w.Observe(10, "A")
	w.Complete("A", 10)
	if got := w.Indexed(); got != 10 {
		t.Fatalf("after A: Indexed = %d, want 10", got)
	}
	// A tail of pure deletes must still advance the watermark — their sequences
	// participate, or lag wedges forever.
	w.Observe(11, "B")
	w.Complete("B", 11)
	if got := w.Indexed(); got != 11 {
		t.Fatalf("trailing delete: Indexed = %d, want 11", got)
	}
}

func TestWatermark_RevisionZeroIgnored(t *testing.T) {
	w := New()
	w.Observe(0, "X") // "no revision" — stream sequences start at 1
	if got := w.Indexed(); got != 0 {
		t.Fatalf("Observe(0) should be a no-op, Indexed = %d, want 0", got)
	}
	w.Observe(5, "A")
	if got := w.Indexed(); got != 4 {
		t.Fatalf("after Observe(5): Indexed = %d, want 4", got)
	}
}

func TestWatermark_CompleteUnobservedRevisionDrainsLower(t *testing.T) {
	// A coalescer re-Get can return a revision NEWER than any yet delivered to the
	// watch (a write between snapshot and Get). Completing at that not-yet-observed
	// revision must still drain the key's lower pending, and must NOT advance
	// observedHigh (only Observe does that).
	w := New()
	w.Observe(10, "K")
	w.Complete("K", 40) // 40 never observed
	if got := w.Indexed(); got != 10 {
		t.Fatalf("Indexed = %d, want 10 (observedHigh unchanged; 10 drained)", got)
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
		w.Observe(uint64(i+1), keys[i])
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

	if got := w.Indexed(); got != uint64(n) {
		t.Fatalf("after draining all: Indexed = %d, want %d", got, n)
	}
}
