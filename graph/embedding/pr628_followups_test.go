package embedding

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/storage"
)

// TestFetchTextFromStorage_PathologicalCapDoesNotReturnEmpty is #628 FIX 2 at the read
// seam: a pathological max_text_len must not overflow the LimitReader bound into a
// negative value, which reads an empty body that hop 2 then treats as "no source text"
// and DELETES the pending embedding.
//
// Fails without the fix because fetchTextFromStorage read int64(limit)+1 bytes:
// with limit == math.MaxInt64 the +1 overflows to a negative bound, io.LimitReader
// yields an empty body, and non-empty content reads back as "".
func TestFetchTextFromStorage_PathologicalCapDoesNotReturnEmpty(t *testing.T) {
	t.Parallel()

	const body = "real body content that must survive a pathological cap"
	w := &Worker{
		maxSourceTextLen: math.MaxInt64, // overflows int64(limit)+1 without the clamp
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"objectstore": readerStore{data: body},
		}},
	}

	got, _, err := w.fetchTextFromStorage(t.Context(), &StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err != nil {
		t.Fatalf("fetchTextFromStorage: %v", err)
	}
	if got == "" {
		t.Fatal("pathological cap returned EMPTY for non-empty content: the read bound overflowed negative and hop 2 would delete the pending embedding (#628 FIX 2)")
	}
	if got != body {
		t.Fatalf("got %q, want the full body %q", got, body)
	}
}

// alwaysConflictUpdateKV is a KV double whose Update ALWAYS reports a revision
// mismatch, so both save lanes exhaust their compare-and-set loop and return
// ErrCASExhausted. Put/Get/Delete delegate to memKV so a pending record can still be
// seeded and read.
type alwaysConflictUpdateKV struct {
	*memKV
}

func (k *alwaysConflictUpdateKV) Update(_ context.Context, _ string, _ []byte, _ uint64) (uint64, error) {
	return 0, jetstream.ErrKeyExists // wrong-last-sequence sentinel; isRevisionMismatch matches it
}

// TestSaveAndNotify_CASExhaustedIsNotAFailure is #628 FIX 3: a non-converging revision
// CAS is transient and re-drivable, NOT a generation failure. saveAndNotify must not
// SaveFailed and must not count a generation failure on ErrCASExhausted — otherwise
// that failure write can later flip a good vector to StatusFailed and the failure
// gauge inflates for a record that is merely re-drivable.
//
// Fails without the fix because saveAndNotify fell through ErrCASExhausted to the
// generic markFailed branch, which calls IncFailed (and attempts SaveFailed).
func TestSaveAndNotify_CASExhaustedIsNotAFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	index := &alwaysConflictUpdateKV{memKV: newMemKV()}
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.cas"
	const rev = uint64(5)
	if err := s.SavePending(ctx, entityID, "", "text to embed", rev); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	m := &recordingWorkerMetrics{}
	w := &Worker{
		storage:  s,
		embedder: &stubEmbedder{model: "m", dimensions: 3},
		metrics:  m,
		logger:   discardLogger(),
	}

	// Every Update conflicts → SaveGenerated returns ErrCASExhausted. The terminal
	// return is ignored on purpose. An inline record (no StorageRef) is passed for the
	// offloaded-identity derivation; CAS exhaustion returns before that success-path block.
	w.saveAndNotify(ctx, entityID, &Record{EntityID: entityID}, []float32{1, 2, 3}, "dedup-key", rev, false)

	// The generation-failure metric is the discriminator: pre-fix counts 1, fixed 0.
	if got := m.snapshot().failed; got != 0 {
		t.Fatalf("IncFailed count = %d, want 0: a non-converging revision CAS is transient/re-drivable, not a generation failure (#628 FIX 3)", got)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil {
		t.Fatal("pending record vanished")
	}
	if rec.Status != StatusPending {
		t.Fatalf("record status = %q, want %q: CAS exhaustion must leave the record pending for re-drive, not durably failed", rec.Status, StatusPending)
	}
}

// TestSaveFailed_EqualRevisionDoesNotDowngradeSuccess is #628 FIX 4: an equal-revision
// failure must not downgrade an equal-revision success. A duplicate/racing SaveFailed(R)
// after SaveGenerated(R) has produced a StatusGenerated record must leave that good
// vector intact — generated wins over failed at the same revision.
//
// Fails without the fix because SaveFailed's guard was strictly `existing.SourceRevision
// > sourceRevision`, which does not fire at equal R, so the failure overwrote the
// generated record with StatusFailed.
func TestSaveFailed_EqualRevisionDoesNotDowngradeSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewStorage(newMemKV(), newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.eqrev"
	const rev = uint64(9)
	if err := s.SavePending(ctx, entityID, "", "text", rev); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	// Generated lands first at revision R.
	if err := s.SaveGenerated(ctx, entityID, []float32{4, 5, 6}, "m", 3, "hash-R", rev); err != nil {
		t.Fatalf("SaveGenerated(R): %v", err)
	}

	// A duplicate failure at the SAME revision must resolve as already-resolved, not
	// clobber the success.
	if err := s.SaveFailed(ctx, entityID, "late duplicate failure", "internal", rev); !errors.Is(err, ErrSupersededRevision) {
		t.Fatalf("SaveFailed(R) after SaveGenerated(R) = %v, want ErrSupersededRevision: generated wins at equal revision (#628 FIX 4)", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || rec.Status != StatusGenerated || len(rec.Vector) != 3 || rec.Vector[0] != 4 {
		t.Fatalf("record = %+v, want the generated vector [4 5 6] intact: an equal-revision failure must not downgrade an equal-revision success", rec)
	}
}

// TestSaveGenerated_EqualRevisionWinsOverPriorFailure is the other completion order at
// equal revision R: a SaveGenerated(R) after a SaveFailed(R) must still land the good
// vector (generated wins). This holds on both builds — SaveGenerated's guard is also
// strict `>` — and documents the equal-revision precedence from the other side.
func TestSaveGenerated_EqualRevisionWinsOverPriorFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewStorage(newMemKV(), newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.eqrev2"
	const rev = uint64(9)
	if err := s.SavePending(ctx, entityID, "", "text", rev); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	// Failure lands first at revision R.
	if err := s.SaveFailed(ctx, entityID, "boom", "internal", rev); err != nil {
		t.Fatalf("SaveFailed(R): %v", err)
	}
	// A generation at the SAME revision then completes; it wins.
	if err := s.SaveGenerated(ctx, entityID, []float32{7, 8, 9}, "m", 3, "hash-R", rev); err != nil {
		t.Fatalf("SaveGenerated(R) after SaveFailed(R): %v", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || rec.Status != StatusGenerated || len(rec.Vector) != 3 || rec.Vector[0] != 7 {
		t.Fatalf("record = %+v, want the generated vector [7 8 9]: a generation must win over a prior equal-revision failure", rec)
	}
}
