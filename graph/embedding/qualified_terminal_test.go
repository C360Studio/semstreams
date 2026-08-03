package embedding

import (
	"context"
	"testing"
)

// TestQualifiedSuccess_NeverReachesFailureAccounting locks the property the gh#875
// reframe rests on: Reason is a qualifier of the terminal state, valid on ANY Status,
// and every consumer gates on Status FIRST — so a qualified SUCCESS can never be
// collected as a failure, never seed the current-failed map, and never label the
// failures metric.
//
// Enumerated from the code rather than assumed (the three production readers of
// Record.Reason): SavePendingGuarded (gated by Status == StatusGenerated), ScanFailed
// (gated by Status == StatusFailed), and the component's failed-map seed, which reads
// only FailedEntry values ScanFailed produced inside that gate. The failures metric is
// unreachable by construction — incFailure is called only from markFailed.
func TestQualifiedSuccess_NeverReachesFailureAccounting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const qualified = "acme.ops.docs.sys.doc.qualified"
	const failed = "acme.ops.docs.sys.doc.failed"
	const complete = "acme.ops.docs.sys.doc.complete"

	// replayKV, not watchableKV: ScanFailed reads the WatchAll snapshot, and only
	// replayKV replays the current key set the way a real DeliverLastPerSubject watcher
	// does. A double that delivers an empty snapshot would make the "not collected"
	// assertion pass for the wrong reason — nothing is collected at all.
	index := &replayKV{memKV: newMemKV()}
	s := NewStorage(index, newMemKV())

	// A qualified success: a real, servable vector that is missing its body.
	if err := s.SavePending(ctx, qualified, "", "identity text", 3); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if err := s.SaveGenerated(ctx, qualified, []float32{1, 2, 3}, "m", 3, "h", 3, ReasonContentExcluded); err != nil {
		t.Fatalf("SaveGenerated(qualified): %v", err)
	}
	// A complete success and a real failure, for contrast.
	if err := s.SavePending(ctx, complete, "", "text", 3); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if err := s.SaveGenerated(ctx, complete, []float32{4, 5, 6}, "m", 3, "h2", 3, ""); err != nil {
		t.Fatalf("SaveGenerated(complete): %v", err)
	}
	if err := s.SavePending(ctx, failed, "", "text", 3); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if err := s.SaveFailed(ctx, failed, "boom", failReasonContentError, 3); err != nil {
		t.Fatalf("SaveFailed: %v", err)
	}

	t.Run("the qualifier is persisted on the generated record", func(t *testing.T) {
		rec, err := s.GetEmbedding(ctx, qualified)
		if err != nil || rec == nil {
			t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
		}
		if rec.Status != StatusGenerated {
			t.Fatalf("status = %q, want generated: the vector is real and servable", rec.Status)
		}
		if rec.Reason != ReasonContentExcluded {
			t.Fatalf("reason = %q, want %q — without it the record is unenumerable", rec.Reason, ReasonContentExcluded)
		}
		if len(rec.Vector) == 0 {
			t.Fatal("a qualified success still stores its vector")
		}
	})

	t.Run("ScanFailed collects the failure and NOT the qualified success", func(t *testing.T) {
		entries, err := s.ScanFailed(ctx)
		if err != nil {
			t.Fatalf("ScanFailed: %v", err)
		}
		got := map[string]string{}
		for _, e := range entries {
			got[e.EntityID] = e.Reason
		}
		if _, ok := got[qualified]; ok {
			t.Fatalf("ScanFailed collected the qualified SUCCESS %q — it would seed FailedCount and pin the index degraded", qualified)
		}
		if _, ok := got[complete]; ok {
			t.Fatalf("ScanFailed collected a complete success %q", complete)
		}
		if got[failed] != failReasonContentError {
			t.Fatalf("ScanFailed[%s] = %q, want %q (the real failure must still be collected)",
				failed, got[failed], failReasonContentError)
		}
	})

	t.Run("the qualifier is not a member of the bounded FAILURE enum", func(t *testing.T) {
		// A failed record that somehow carried the qualifier must not inject it as a
		// metric label; it normalizes to unknown like any other out-of-enum value.
		if got := normalizeFailureReason(ReasonContentExcluded); got != reasonUnknown {
			t.Fatalf("normalizeFailureReason(%q) = %q, want %q — the qualifier must never label failures_total",
				ReasonContentExcluded, got, reasonUnknown)
		}
	})
}

// TestQualifiedSuccess_ReQueuesSoItCanSelfHeal is the repair half of the reframe.
// SavePendingGuarded skips a re-queue over a same-or-newer generated vector — that
// guard is what would otherwise make a body-less vector PERMANENT, because an operator
// who wires the missing store and restarts gets a last-per-subject re-delivery at the
// SAME revision and nothing else. A qualified success must not be skipped; an
// unqualified one still must.
func TestQualifiedSuccess_ReQueuesSoItCanSelfHeal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name      string
		qualifier string
		wantSaved bool
	}{
		{
			name:      "qualified generated at the SAME revision re-queues (self-heal)",
			qualifier: ReasonContentExcluded,
			wantSaved: true,
		},
		{
			name:      "unqualified generated at the SAME revision is still skipped (#722 B2)",
			qualifier: "",
			wantSaved: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const entityID = "acme.ops.docs.sys.doc.heal"
			s := NewStorage(newMemKV(), newMemKV())
			if err := s.SavePending(ctx, entityID, "", "text", 5); err != nil {
				t.Fatalf("SavePending: %v", err)
			}
			if err := s.SaveGenerated(ctx, entityID, []float32{1}, "m", 1, "h", 5, tc.qualifier); err != nil {
				t.Fatalf("SaveGenerated: %v", err)
			}

			// hop 1 re-delivering the SAME revision (a restart's last-per-subject replay).
			saved, err := s.SavePendingGuarded(ctx, &Record{
				EntityID:       entityID,
				SourceText:     "fresh text",
				SourceRevision: 5,
			})
			if err != nil {
				t.Fatalf("SavePendingGuarded: %v", err)
			}
			if saved != tc.wantSaved {
				t.Fatalf("saved = %v, want %v", saved, tc.wantSaved)
			}

			rec, err := s.GetEmbedding(ctx, entityID)
			if err != nil || rec == nil {
				t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
			}
			wantStatus := StatusGenerated
			if tc.wantSaved {
				wantStatus = StatusPending // re-queued: hop 2 will re-fetch and can now succeed
			}
			if rec.Status != wantStatus {
				t.Fatalf("status = %q, want %q", rec.Status, wantStatus)
			}
		})
	}
}
