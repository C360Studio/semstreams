package embedding

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestFetchTextFromStorage_OverCapCountsTruncation is the offloaded-lane half of
// the #602 "truncation MUST be observable" requirement. The offloaded
// (ContentStorable) lane truncates via LimitReader in fetchTextFromStorage, NOT at
// getSourceText's word-cut branch, so before this fix truncation of the primary
// adopter's lane was silent (IncTruncated never fired). semsource drives exactly
// this lane, so silence here means the metric reads ~0 for the lane that truncates
// most.
func TestFetchTextFromStorage_OverCapCountsTruncation(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/big": []byte(strings.Repeat("x", 250)), // 250 bytes, cap 100
		},
	}
	m := &recordingWorkerMetrics{}
	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 100,
		metrics:          m,
		ctx:              context.Background(),
	}

	text, _, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/big"})
	if err != nil {
		t.Fatalf("fetchTextFromStorage: %v", err)
	}
	if len(text) != 100 {
		t.Errorf("returned %d bytes, want the cap of 100", len(text))
	}
	if got := m.snapshot().truncated; got != 1 {
		t.Errorf("truncated count = %d, want 1: offloaded-lane truncation must be observable (#602)", got)
	}
}

// TestFetchTextFromStorage_UnderCapDoesNotCountTruncation guards the boundary: a
// body that fits the cap must not report truncation (the +1 read must not
// mis-fire on exactly-cap content).
func TestFetchTextFromStorage_UnderCapDoesNotCountTruncation(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/exact": []byte(strings.Repeat("y", 100)), // exactly the cap
		},
	}
	m := &recordingWorkerMetrics{}
	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 100,
		metrics:          m,
		ctx:              context.Background(),
	}

	if _, _, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/exact"}); err != nil {
		t.Fatalf("fetchTextFromStorage: %v", err)
	}
	if got := m.snapshot().truncated; got != 0 {
		t.Errorf("truncated count = %d, want 0: content that fits the cap is not truncated", got)
	}
}

// TestSaveAndNotify_SupersededDropSkipsOnGenerated proves the ordering-drop does
// NOT fire the generated callback. saveAndNotify cannot distinguish a real write
// from a superseded drop if both return nil, so it would push THIS call's older
// vector into any WithOnGenerated consumer's cache — the same stale-vector hazard
// the ErrRecordGone branch already guards against. The distinguishable
// ErrSupersededRevision sentinel is what lets it skip the callback.
func TestSaveAndNotify_SupersededDropSkipsOnGenerated(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(nil, newMemKV(), newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.007"
	if err := s.SavePending(ctx, entityID, "", "text", 0); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	// A newer revision (10) already landed.
	if err := s.SaveGenerated(ctx, entityID, []float32{9, 9, 9}, "m", 3, "hash-10", 10); err != nil {
		t.Fatalf("SaveGenerated(rev 10): %v", err)
	}

	var onGeneratedFired bool
	w := &Worker{
		storage:  s,
		embedder: &stubEmbedder{model: "m", dimensions: 3},
		metrics:  &recordingWorkerMetrics{},
		logger:   slog.Default(),
		ctx:      ctx,
		onGenerated: func(string, []float32) {
			onGeneratedFired = true
		},
	}

	// An older revision (5) completes last — its SaveGenerated drops as superseded.
	// Inline record (no StorageRef): the offloaded-identity counting block is not exercised.
	w.saveAndNotify(entityID, &Record{EntityID: entityID}, []float32{5, 5, 5}, "hash-5", 5, false)

	if onGeneratedFired {
		t.Error("onGenerated fired for a superseded (older-revision) write; a stale vector would be pushed to caches")
	}
	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil || rec.SourceRevision != 10 || rec.Vector[0] != 9 {
		t.Fatalf("index holds %v, want the newer rev-10 vector intact", rec)
	}
}

// TestSaveAndNotify_SupersededDedupHitDoesNotCount pins the invariant the e2e
// queue-health gate enforces: dedup_hits is a strict subset of
// embeddings_generated_total. A dedup HIT whose save is then dropped as superseded
// must NOT increment dedup_hits, because the drop skips onGenerated (so it does not
// increment generated_total). Counting the hit eagerly at lookup time — as an
// earlier revision did — let dedup_hits exceed generated_total under same-entity
// churn and turned the semantic e2e tier red (dedup_hits 166 > generated 69).
func TestSaveAndNotify_SupersededDedupHitDoesNotCount(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(nil, newMemKV(), newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.008"
	if err := s.SavePending(ctx, entityID, "", "text", 0); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if err := s.SaveGenerated(ctx, entityID, []float32{9, 9, 9}, "m", 3, "hash-10", 10); err != nil {
		t.Fatalf("SaveGenerated(rev 10): %v", err)
	}

	m := &recordingWorkerMetrics{}
	w := &Worker{
		storage:  s,
		embedder: &stubEmbedder{model: "m", dimensions: 3},
		metrics:  m,
		logger:   slog.Default(),
		ctx:      ctx,
	}

	// A dedup HIT for an older revision (5) — its save is superseded by rev 10.
	w.saveAndNotify(entityID, &Record{EntityID: entityID}, []float32{5, 5, 5}, "hash-5", 5, true)

	if got := m.snapshot().dedupHits; got != 0 {
		t.Errorf("dedupHits = %d, want 0: a superseded dedup hit must not count (it skips generated_total)", got)
	}
}

// TestSaveAndNotify_StoredDedupHitCounts is the positive half: a dedup hit that
// actually stores DOES count, so real reuse is still measured.
func TestSaveAndNotify_StoredDedupHitCounts(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(nil, newMemKV(), newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.009"
	if err := s.SavePending(ctx, entityID, "", "text", 3); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	m := &recordingWorkerMetrics{}
	w := &Worker{
		storage:  s,
		embedder: &stubEmbedder{model: "m", dimensions: 3},
		metrics:  m,
		logger:   slog.Default(),
		ctx:      ctx,
	}

	// A dedup hit at the current revision stores successfully.
	if terminal, _, _ := w.saveAndNotify(entityID, &Record{EntityID: entityID}, []float32{1, 2, 3}, "hash-3", 3, true); !terminal {
		t.Error("saveAndNotify returned non-terminal for a successful store")
	}
	if got := m.snapshot().dedupHits; got != 1 {
		t.Errorf("dedupHits = %d, want 1: a stored dedup hit is real reuse and must count", got)
	}
}
