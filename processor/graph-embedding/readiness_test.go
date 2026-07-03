package graphembedding

import (
	"testing"
	"time"
)

// TestTrackEmbeddingProgress_CompletionsBasedStuckDetector pins the key difference
// from graph-index's detector (ADR-066 §3): stuck is keyed off terminal COMPLETIONS,
// not IndexedRevision, so a slow single LLM call that pins Indexed while other workers
// finish out of order is healthy, not degraded.
func TestTrackEmbeddingProgress_CompletionsBasedStuckDetector(t *testing.T) {
	c := &Component{}

	// First call initializes progress; never stuck.
	if stuck, _ := c.trackEmbeddingProgress(5, 100); stuck {
		t.Fatal("first call must not be stuck")
	}

	// A terminal completion in the window clears stuck even though IndexedRevision is
	// unchanged — the slow-single-call case: other workers completing keeps it alive.
	c.lastProgressAt = time.Now().Add(-embeddingDegradedAfter - time.Second)
	c.embeddingCompletions.Add(1)
	if stuck, _ := c.trackEmbeddingProgress(5, 100); stuck {
		t.Fatal("a terminal completion in the window must clear stuck (pinned Indexed is healthy)")
	}

	// Zero completions for the whole window while lagging → stuck (degraded).
	c.lastProgressAt = time.Now().Add(-embeddingDegradedAfter - time.Second)
	if stuck, _ := c.trackEmbeddingProgress(5, 100); !stuck {
		t.Fatal("zero completions while lagging must be stuck")
	}

	// Caught up is never stuck, even with an old timestamp.
	c.lastProgressAt = time.Now().Add(-embeddingDegradedAfter - time.Second)
	if stuck, _ := c.trackEmbeddingProgress(100, 100); stuck {
		t.Fatal("caught-up must never be degraded")
	}
}

// TestCompleteEmbedding_NilWatermarkSafe guards the pre-Start / test path.
func TestCompleteEmbedding_NilWatermarkSafe(t *testing.T) {
	c := &Component{} // no watermark wired
	c.completeEmbedding("entity", 5)
	if got := c.embeddingCompletions.Load(); got != 0 {
		t.Fatalf("nil-watermark completion must be a no-op, counter = %d, want 0", got)
	}
}
