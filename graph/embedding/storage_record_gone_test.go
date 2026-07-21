package embedding

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestSaveGenerated_NoIndexRecord_DoesNotPanicOrResurrect pins the first half of
// the gh#614 fallout.
//
// SaveGenerated reads the pending record to carry its ContentHash forward, and
// GetEmbedding answers (nil, nil) for a missing key. Before this fix the result
// was dereferenced unguarded, so a save whose index key had disappeared panicked
// on a nil *Record. That became reachable the moment the hop-1 entity tombstone
// started calling DeleteEmbedding: the delete runs on the watcher goroutine while
// a hop-2 worker is inside a several-hundred-millisecond embedder round trip for
// the same entity.
//
// Two things are asserted, and the second is the semantic one: the write must be
// DROPPED, not resurrected. Re-creating the key would reinstate exactly the
// dangling vector gh#614 exists to remove.
func TestSaveGenerated_NoIndexRecord_DoesNotPanicOrResurrect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.001"

	err := s.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "all-MiniLM-L6-v2", 3)
	if !errors.Is(err, ErrRecordGone) {
		t.Fatalf("SaveGenerated on a missing key = %v, want ErrRecordGone", err)
	}

	if index.has(entityID) {
		t.Fatal("SaveGenerated resurrected a deleted embedding record; " +
			"a tombstoned entity must not regain a vector semantic search can return")
	}
}

// TestSaveFailed_NoIndexRecord_DoesNotPanicOrResurrect is the same shape for the
// failure lane. A record that is already gone has nothing to mark, so the write
// is dropped rather than creating a failed record for an entity that no longer
// exists (which would linger in EMBEDDING_INDEX forever — the tombstone that
// would have removed it has already fired).
func TestSaveFailed_NoIndexRecord_DoesNotPanicOrResurrect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.002"

	err := s.SaveFailed(ctx, entityID, "generation failed: connection refused")
	if !errors.Is(err, ErrRecordGone) {
		t.Fatalf("SaveFailed on a missing key = %v, want ErrRecordGone", err)
	}

	if index.has(entityID) {
		t.Fatal("SaveFailed resurrected a deleted embedding record")
	}
}

// TestSaveGenerated_ConcurrentDeleteIsRaceFree is the concurrent statement of the
// same defect: the tombstone delete and the generated write genuinely interleave
// across goroutines in production, so the interleaving is exercised here under
// -race rather than only the post-delete steady state.
//
// Whichever order they land in, neither may panic and neither may leave a
// generated record behind for a key the delete removed last.
func TestSaveGenerated_ConcurrentDeleteIsRaceFree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const iterations = 200

	for i := 0; i < iterations; i++ {
		entityID := "acme.ops.robotics.gcs.drone.00" + string(rune('a'+i%26))

		if err := s.SavePending(ctx, entityID, "hash", "text", uint64(i)); err != nil {
			t.Fatalf("SavePending: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			// The hop-1 tombstone lane.
			if err := s.DeleteEmbedding(ctx, entityID); err != nil {
				t.Errorf("DeleteEmbedding: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			// The hop-2 completion lane. ErrRecordGone is an expected outcome when
			// the delete won; anything else must be a clean success.
			err := s.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "m", 3)
			if err != nil && !errors.Is(err, ErrRecordGone) {
				t.Errorf("SaveGenerated: unexpected error %v", err)
			}
		}()

		wg.Wait()
	}
}
