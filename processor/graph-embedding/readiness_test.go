package graphembedding

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/pkg/revlag"
)

// TestCompleteEmbedding_TerminalOutcomesAdvanceWatermark is the deadlock-avoidance
// regression guard (#613, task 2.3): a FAILED terminal (and a no-text SKIPPED terminal)
// still advances the low-water watermark, so a permanently-failing or telemetry-only
// entity never pins readiness. The #613 fix routes health into State, NOT into a
// watermark hole — this test locks that the watermark behaviour is untouched.
func TestCompleteEmbedding_TerminalOutcomesAdvanceWatermark(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome embedding.TerminalOutcome
		reason  string
	}{
		{"failed still advances", embedding.OutcomeFailed, "connection_refused"},
		{"no-text skip still advances", embedding.OutcomeSkipped, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Component{
				failed:      make(map[string]failureInfo),
				watermark:   revlag.New(),
				failedGauge: newEmbeddingFailedGauge(),
			}
			// Observe a delivered revision (pending), then reach its terminal outcome.
			c.watermark.Observe(5, "ent", time.Now())
			c.completeEmbedding("ent", 5, tc.outcome, tc.reason)

			if got := c.watermark.Indexed(); got < 5 {
				t.Fatalf("%s: watermark must advance past a terminal outcome (deadlock avoidance), Indexed=%d want >=5", tc.name, got)
			}
		})
	}
}

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

// TestCompleteEmbedding_NilWatermarkSafe guards the pre-Start / test path: the watermark
// advance is a no-op, but the current-failed map update still runs (a Failed outcome must
// never be silently dropped because the watermark was not wired yet).
func TestCompleteEmbedding_NilWatermarkSafe(t *testing.T) {
	c := &Component{} // no watermark wired
	c.completeEmbedding("entity", 5, embedding.OutcomeSkipped, "")
	if got := c.embeddingCompletions.Load(); got != 0 {
		t.Fatalf("nil-watermark completion must be a no-op for the watermark, counter = %d, want 0", got)
	}

	// The map update still runs on the nil-watermark path.
	c.completeEmbedding("failing-entity", 6, embedding.OutcomeFailed, failReasonFor(t))
	if count, _, _ := c.failedSnapshot(); count != 1 {
		t.Fatalf("Failed outcome must enter the current-failed map even with a nil watermark, count = %d, want 1", count)
	}
	c.completeEmbedding("failing-entity", 7, embedding.OutcomeGenerated, "")
	if count, _, _ := c.failedSnapshot(); count != 0 {
		t.Fatalf("a subsequent Generated outcome must clear the entity, count = %d, want 0", count)
	}
}

// failReasonFor returns a stable bounded reason for the test without importing the
// embedding package's unexported constants; "connection_refused" is a member of the enum.
func failReasonFor(*testing.T) string { return "connection_refused" }
