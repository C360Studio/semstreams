package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// TestSaveGenerated_NewerRevisionWinsOutOfOrderCompletion is the core D3 (#614
// part 2) statement: the bug is SEMANTIC ORDERING, not just concurrent clobber.
//
// Two source revisions N and N+1 of one entity generate concurrently. Revision N
// (old text) completes AFTER revision N+1 (new text). The index MUST end holding
// N+1's vector and drop the late N write — even though N's write arrives last.
// Plain last-writer-wins (the pre-fix unconditional Put) leaves the OLD text's
// vector in EMBEDDING_INDEX permanently; the SourceRevision guard is what orders it.
func TestSaveGenerated_NewerRevisionWinsOutOfOrderCompletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.001"
	if err := s.SavePending(ctx, entityID, "", "text", 0); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	newVec := []float32{6, 6, 6}
	oldVec := []float32{5, 5, 5}

	// Revision N+1 (=6) completes first.
	if err := s.SaveGenerated(ctx, entityID, newVec, "m", 3, "hash-6", 6); err != nil {
		t.Fatalf("SaveGenerated(rev 6): %v", err)
	}
	// Revision N (=5, older text) completes LAST. It must be dropped and report the
	// superseded sentinel (so the caller skips the generated callback), not persisted.
	if err := s.SaveGenerated(ctx, entityID, oldVec, "m", 3, "hash-5", 5); !errors.Is(err, ErrSupersededRevision) {
		t.Fatalf("SaveGenerated(rev 5) = %v, want ErrSupersededRevision (a late older revision is dropped as superseded)", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || len(rec.Vector) != 3 || rec.Vector[0] != 6 {
		t.Fatalf("index holds vector %v, want the newer revision's %v: a late older-revision write must not overwrite a newer vector (#614)",
			vectorOf(rec), newVec)
	}
	if rec.SourceRevision != 6 {
		t.Fatalf("stored SourceRevision = %d, want 6 (the newest); the guard needs it persisted", rec.SourceRevision)
	}
}

// racingKV injects exactly one revision conflict on the first Update, invoking a
// hook first so a test can simulate a concurrent writer landing a NEWER record
// between this writer's read and its Update — the exact race the CAS loop must
// re-read across.
type racingKV struct {
	*memKV
	onFirstUpdate func()
	fired         bool
}

func (k *racingKV) Update(ctx context.Context, key string, value []byte, rev uint64) (uint64, error) {
	if !k.fired {
		k.fired = true
		if k.onFirstUpdate != nil {
			k.onFirstUpdate()
		}
		return 0, jetstream.ErrKeyExists // wrong-last-sequence: a concurrent writer moved the key
	}
	return k.memKV.Update(ctx, key, value, rev)
}

// TestSaveGenerated_ConcurrentCommitDoesNotClobberViaCAS proves the write uses
// revision compare-and-set: a conflicting writer re-reads rather than clobbering a
// committed vector.
//
// The first Update conflicts, and while it conflicts a concurrent NEWER revision
// (10) commits its vector directly. The CAS loop must re-read, observe the newer
// SourceRevision, and DROP this older (rev 5) write — leaving rev 10's committed
// vector intact. Without CAS (unconditional Put) or without the ordering re-check,
// the older write silently overwrites the committed newer vector.
func TestSaveGenerated_ConcurrentCommitDoesNotClobberViaCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := newMemKV()
	index := &racingKV{memKV: base}
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.002"
	if err := s.SavePending(ctx, entityID, "", "text", 0); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	// When this writer's first Update conflicts, a newer revision (10) has just
	// committed its vector.
	index.onFirstUpdate = func() {
		newer := &Record{
			EntityID:       entityID,
			Vector:         []float32{10, 10, 10},
			ContentHash:    "hash-10",
			Model:          "m",
			Dimensions:     3,
			Status:         StatusGenerated,
			SourceRevision: 10,
		}
		data, _ := json.Marshal(newer)
		if _, err := base.Put(ctx, entityID, data); err != nil {
			t.Errorf("inject newer commit: %v", err)
		}
	}

	// This older (rev 5) write must NOT win: it re-reads on the CAS conflict, then
	// drops via the ordering guard, reporting the superseded sentinel.
	if err := s.SaveGenerated(ctx, entityID, []float32{5, 5, 5}, "m", 3, "hash-5", 5); !errors.Is(err, ErrSupersededRevision) {
		t.Fatalf("SaveGenerated(rev 5) = %v, want ErrSupersededRevision (re-read, then dropped by the ordering guard)", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || rec.SourceRevision != 10 || len(rec.Vector) != 3 || rec.Vector[0] != 10 {
		t.Fatalf("index holds %v (rev %d), want the committed rev-10 vector; a CAS conflict must re-read, not clobber",
			vectorOf(rec), sourceRevOf(rec))
	}
}

// TestSaveGenerated_ContentHashAndVectorFromSameGeneration proves a stored record's
// content hash and vector both come from the passed generation, never mixed across
// revisions (#614 part 2, second half). The pending record carries a DIFFERENT
// (hop-1) content hash; SaveGenerated must not copy it — it stores the hop-2 key it
// was given alongside the vector it was given, so the two can never desync.
func TestSaveGenerated_ContentHashAndVectorFromSameGeneration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.003"
	if err := s.SavePending(ctx, entityID, "stale-hop1-hash", "text", 3); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	if err := s.SaveGenerated(ctx, entityID, []float32{7, 8, 9}, "m", 3, "fresh-hop2-key", 3); err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil {
		t.Fatal("no record after SaveGenerated")
	}
	if rec.ContentHash != "fresh-hop2-key" {
		t.Fatalf("ContentHash = %q, want the passed hop-2 key %q: the record must not copy the pending record's stale hash (desync risk)",
			rec.ContentHash, "fresh-hop2-key")
	}
	if len(rec.Vector) != 3 || rec.Vector[0] != 7 {
		t.Fatalf("Vector = %v, want the passed [7 8 9]", rec.Vector)
	}
}

// TestSaveFailed_DoesNotClobberNewerSuccess is the SaveFailed side of the ordering
// guard: a stale older-revision failure must not overwrite a newer success.
func TestSaveFailed_DoesNotClobberNewerSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.004"
	if err := s.SavePending(ctx, entityID, "", "text", 0); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	// Newer revision (6) succeeds.
	if err := s.SaveGenerated(ctx, entityID, []float32{9, 9, 9}, "m", 3, "hash-6", 6); err != nil {
		t.Fatalf("SaveGenerated(rev 6): %v", err)
	}
	// Older revision (5) fails and completes late; it must be dropped.
	if err := s.SaveFailed(ctx, entityID, "generation failed for old revision", 5); !errors.Is(err, ErrSupersededRevision) {
		t.Fatalf("SaveFailed(rev 5) = %v, want ErrSupersededRevision (older failure dropped as superseded)", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || rec.Status != StatusGenerated || len(rec.Vector) != 3 || rec.Vector[0] != 9 {
		t.Fatalf("record = %+v, want the newer generated vector intact: a stale failure must not clobber a newer success", rec)
	}
}

// TestSaveGenerated_VanishedRecordStillErrRecordGone confirms Track 0's
// drop-not-resurrect survives the CAS rewrite (task 4.4): a save whose pending
// record was removed returns ErrRecordGone and writes nothing.
func TestSaveGenerated_VanishedRecordStillErrRecordGone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.005"
	if err := s.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "m", 3, "hash", 1); !errors.Is(err, ErrRecordGone) {
		t.Fatalf("SaveGenerated on a vanished record = %v, want ErrRecordGone", err)
	}
	if index.has(entityID) {
		t.Fatal("SaveGenerated resurrected a vanished record under CAS")
	}
	if err := s.SaveFailed(ctx, entityID, "boom", 1); !errors.Is(err, ErrRecordGone) {
		t.Fatalf("SaveFailed on a vanished record = %v, want ErrRecordGone", err)
	}
	if index.has(entityID) {
		t.Fatal("SaveFailed resurrected a vanished record under CAS")
	}
}

func vectorOf(r *Record) []float32 {
	if r == nil {
		return nil
	}
	return r.Vector
}

func sourceRevOf(r *Record) uint64 {
	if r == nil {
		return 0
	}
	return r.SourceRevision
}
