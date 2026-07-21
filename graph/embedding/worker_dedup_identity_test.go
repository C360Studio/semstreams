package embedding

import (
	"context"
	"log/slog"
	"testing"
)

// TestDedupRecordUsable_RejectsIdentitylessLegacyRecord closes the escape hatch
// that let gh#612 survive its own fix on the exact upgrade path the fix targets.
//
// dedupRecordUsable used to return true for any record with no Model, on the
// reasoning that identityless records "came from the old layout and can no longer
// be reached anyway". They can. EMBEDDING_INDEX pending records are durable KV
// with no TTL, and Worker.Start does WatchAll, which re-delivers every current
// value on restart. handleKVEntry skips only NON-pending records, so a stale
// pending record written by the pre-fix binary is replayed as pending, and hop-2
// uses its stored ContentHash — an OLD-layout ContentHash(text) key — verbatim:
//
//  1. Pre-fix binary, embedder_type bm25, queues an entity. Its pending record
//     holds ContentHash(text); EMBEDDING_DEDUP[ContentHash(text)] holds a BM25
//     vector with no Model.
//  2. Operator upgrades and switches to http against the same NATS state.
//  3. Hop-2 replays the stale pending record and looks up that old key.
//  4. Unfixed, the missing Model made the BM25 record "usable".
//  5. saveAndNotify stamps that BM25 vector with the neural model's name.
//
// Both spaces are 384-dim by default, so cosine returns a plausible score and no
// health signal fires. An identityless record predates the key contract and must
// be regenerated; the cost is one re-embed per legacy record, once.
func TestDedupRecordUsable_RejectsIdentitylessLegacyRecord(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, &stubEmbedder{model: "all-MiniLM-L6-v2", dimensions: 384}, nil, slog.Default())

	legacy := &DedupRecord{Vector: []float32{9, 9, 9}} // written before DedupRecord carried identity
	if w.dedupRecordUsable("acme.ops.robotics.gcs.drone.001", "old-layout-key", legacy) {
		t.Fatal("an identityless dedup record was treated as usable; " +
			"a replayed pre-fix pending record can serve its bm25 vector stamped with the neural model's name (gh#612)")
	}

	matching := &DedupRecord{Vector: []float32{1, 1, 1}, Model: "all-MiniLM-L6-v2", Dimensions: 384}
	if !w.dedupRecordUsable("acme.ops.robotics.gcs.drone.001", "current-key", matching) {
		t.Fatal("a record from this embedder's own vector space must stay usable")
	}

	mismatched := &DedupRecord{Vector: []float32{1, 1, 1}, Model: "bm25-384", Dimensions: 384}
	if w.dedupRecordUsable("acme.ops.robotics.gcs.drone.001", "current-key", mismatched) {
		t.Fatal("a record from a different vector space must not be usable")
	}
}

// TestSaveDedup_ReplacesRecordFromAnotherVectorSpace covers the second-order
// defect that rejecting identityless records exposes in SaveDedup.
//
// After dedupRecordUsable rejects a legacy record the worker regenerates and
// calls SaveDedup on the SAME key. SaveDedup's backfill branch fired on any
// existing record: it appended the entity and overwrote Model/Dimensions with the
// new identity while KEEPING the old vector. The legacy record would then be
// stamped "all-MiniLM-L6-v2" over a BM25 vector — and the NEXT entity with that
// content would sail through dedupRecordUsable's identity check and be served the
// stale vector. gh#612 would survive exactly one hop further along.
//
// The freshly generated vector was produced by this embedder and is authoritative
// for the key, so a record from a different or unknown space is replaced, not
// annotated.
func TestSaveDedup_ReplacesRecordFromAnotherVectorSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dedup := newMemKV()
	s := NewStorage(newMemKV(), dedup)

	const key = "legacy-content-hash"
	legacyVector := []float32{9, 9, 9}
	freshVector := []float32{1, 2, 3}

	// A pre-fix record: a vector, entities, and no identity at all.
	if err := s.SaveDedup(ctx, key, legacyVector, "acme.ops.robotics.gcs.drone.001", "", 0); err != nil {
		t.Fatalf("SaveDedup (legacy): %v", err)
	}

	// The worker regenerated under the current identity and saves to the same key.
	if err := s.SaveDedup(ctx, key, freshVector, "acme.ops.robotics.gcs.drone.002", "all-MiniLM-L6-v2", 384); err != nil {
		t.Fatalf("SaveDedup (regenerated): %v", err)
	}

	got, err := s.GetByContentHash(ctx, key)
	if err != nil {
		t.Fatalf("GetByContentHash: %v", err)
	}
	if got == nil {
		t.Fatal("dedup record missing")
	}

	if len(got.Vector) != len(freshVector) || got.Vector[0] != freshVector[0] {
		t.Fatalf("Vector = %v, want the regenerated %v; "+
			"keeping the old vector under the new model's name re-opens gh#612 one hop past dedupRecordUsable",
			got.Vector, freshVector)
	}
	if got.Model != "all-MiniLM-L6-v2" || got.Dimensions != 384 {
		t.Fatalf("identity = (%q, %d), want (all-MiniLM-L6-v2, 384)", got.Model, got.Dimensions)
	}
}
