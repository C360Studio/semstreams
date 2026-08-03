package embedding

import (
	"context"
	"testing"
)

// TestSavePendingGuarded_Decisions pins the guarded pending-write lane
// (#722 Codex 2) at the storage boundary: the ONE additive method behind
// graph-embedding's sole hop-1 writer. A generated record at a same-or-newer
// source revision is never downgraded to pending; everything else writes as
// the unconditional lanes always did (absent → create, pending/failed →
// overwrite, generated at an OLDER revision → overwrite, because a genuinely
// newer source state is being queued). The CAS-conflict re-read loop mirrors
// SaveGenerated's tested pattern (memKV Update enforces per-key revisions).
func TestSavePendingGuarded_Decisions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.guard1"

	type seed func(t *testing.T, s *Storage)
	cases := []struct {
		name       string
		seed       seed
		queueRev   uint64
		wantSaved  bool
		wantStatus Status // asserted on the post-call record
		wantRev    uint64
	}{
		{
			name:       "absent key creates pending",
			seed:       func(*testing.T, *Storage) {},
			queueRev:   5,
			wantSaved:  true,
			wantStatus: StatusPending,
			wantRev:    5,
		},
		{
			name: "pending record overwritten",
			seed: func(t *testing.T, s *Storage) {
				if err := s.SavePending(ctx, entityID, "", "old", 4); err != nil {
					t.Fatal(err)
				}
			},
			queueRev:   5,
			wantSaved:  true,
			wantStatus: StatusPending,
			wantRev:    5,
		},
		{
			name: "failed record overwritten (re-queue recovers)",
			seed: func(t *testing.T, s *Storage) {
				if err := s.SavePending(ctx, entityID, "", "old", 4); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveFailed(ctx, entityID, "boom", "content_error", 4); err != nil {
					t.Fatal(err)
				}
			},
			queueRev:   5,
			wantSaved:  true,
			wantStatus: StatusPending,
			wantRev:    5,
		},
		{
			name: "generated at OLDER revision overwritten (newer source state)",
			seed: func(t *testing.T, s *Storage) {
				if err := s.SavePending(ctx, entityID, "", "old", 4); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveGenerated(ctx, entityID, []float32{1}, "bm25-384", 384, "h", 4, ""); err != nil {
					t.Fatal(err)
				}
			},
			queueRev:   5,
			wantSaved:  true,
			wantStatus: StatusPending,
			wantRev:    5,
		},
		{
			name: "generated at SAME revision skipped (restart re-delivery)",
			seed: func(t *testing.T, s *Storage) {
				if err := s.SavePending(ctx, entityID, "", "old", 5); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveGenerated(ctx, entityID, []float32{1}, "bm25-384", 384, "h", 5, ""); err != nil {
					t.Fatal(err)
				}
			},
			queueRev:   5,
			wantSaved:  false,
			wantStatus: StatusGenerated,
			wantRev:    5,
		},
		{
			name: "generated at NEWER revision skipped (stale repair re-drive)",
			seed: func(t *testing.T, s *Storage) {
				if err := s.SavePending(ctx, entityID, "", "old", 6); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveGenerated(ctx, entityID, []float32{1}, "bm25-384", 384, "h", 6, ""); err != nil {
					t.Fatal(err)
				}
			},
			queueRev:   5,
			wantSaved:  false,
			wantStatus: StatusGenerated,
			wantRev:    6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewStorage(newMemKV(), newMemKV())
			tc.seed(t, s)

			saved, err := s.SavePendingGuarded(ctx, &Record{
				EntityID:       entityID,
				SourceText:     "fresh text",
				SourceRevision: tc.queueRev,
			})
			if err != nil {
				t.Fatalf("SavePendingGuarded: %v", err)
			}
			if saved != tc.wantSaved {
				t.Fatalf("saved = %v, want %v", saved, tc.wantSaved)
			}

			rec, err := s.GetEmbedding(ctx, entityID)
			if err != nil {
				t.Fatal(err)
			}
			if rec == nil {
				t.Fatal("record absent after guarded save decision")
			}
			if rec.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q", rec.Status, tc.wantStatus)
			}
			if rec.SourceRevision != tc.wantRev {
				t.Fatalf("SourceRevision = %d, want %d", rec.SourceRevision, tc.wantRev)
			}
			if tc.wantStatus == StatusGenerated && len(rec.Vector) == 0 {
				t.Fatal("the skip must leave the stored vector untouched")
			}
		})
	}
}
