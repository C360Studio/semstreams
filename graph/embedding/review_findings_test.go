package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// seedFailedRecord writes a durable StatusFailed record for entityID at sourceRevision,
// via the real pending -> failed transition (SaveFailed is an UPDATE lane).
func seedFailedRecord(t *testing.T, s *Storage, entityID, sourceText, reason string, sourceRevision uint64) {
	t.Helper()
	ctx := context.Background()
	if err := s.SavePending(ctx, entityID, "", sourceText, sourceRevision); err != nil {
		t.Fatalf("seed SavePending: %v", err)
	}
	if err := s.SaveFailed(ctx, entityID, "seed failure", reason, sourceRevision); err != nil {
		t.Fatalf("seed SaveFailed: %v", err)
	}
}

// TestHandleKVEntry_SaveFailedPersistenceMissIsNonTerminal is the #613 F2 worker-side
// statement: when generation fails AND the failure cannot be durably recorded (SaveFailed's
// revision CAS never converges — a persistence MISS), handleKVEntry must return
// terminal==false so the readiness watermark does NOT advance over a record that is still
// durably pending. Recovery rides re-delivery. This mirrors saveAndNotify's ErrCASExhausted
// handling; the deadlock-avoidance property is preserved for failures that DID durably record.
//
// Fails without the fix because markFailed's non-superseded/non-gone error path still
// reported a terminal OutcomeSkipped, advancing the watermark over the pending record.
func TestHandleKVEntry_SaveFailedPersistenceMissIsNonTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// alwaysConflictUpdateKV (pr628_followups_test.go) makes every Update conflict, so
	// SaveFailed exhausts its CAS loop and returns ErrCASExhausted — a persistence miss.
	index := &alwaysConflictUpdateKV{memKV: newMemKV()}
	s := NewStorage(nil, index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.persistmiss"
	const rev = uint64(5)
	if err := s.SavePending(ctx, entityID, "", "text to embed", rev); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	// The embedder fails generation, forcing markFailed → SaveFailed → CAS exhausted.
	embedder := &stubEmbedder{model: "m", dimensions: 3, generate: func([]string) ([][]float32, error) {
		return nil, errors.New("dial tcp: connect: connection refused")
	}}
	w := &Worker{storage: s, embedder: embedder, ctx: ctx, logger: discardLogger()}

	entry, err := index.Get(ctx, entityID)
	if err != nil {
		t.Fatalf("Get seed: %v", err)
	}
	_, srcRev, terminal, _, _ := w.handleKVEntry(entry, false /*live phase*/, 0)

	require.Equal(t, rev, srcRev)
	require.False(t, terminal,
		"a SaveFailed persistence miss must be non-terminal so the watermark does not advance and re-delivery retries (#613 F2)")

	rec, err := s.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, StatusPending, rec.Status,
		"nothing durable transitioned — the record must remain pending for re-drive")
}

// TestHandleKVEntry_FailedRecordReprocessGatedByPhase is the #613 F3 statement that the
// snapshot-vs-live phase, captured at dequeue and passed as an immutable parameter, gates
// FAILED-record reprocessing: a failed record classified snapshot IS re-embedded, one
// classified live is SKIPPED (the hot-loop guard). The two are asserted deterministically
// against handleKVEntry directly, independent of the goroutine scheduling.
//
// Fails without the fix because handleKVEntry re-read the shared snapshotComplete flag
// (defaulting false in a bare Worker), so the live case would reprocess too.
func TestHandleKVEntry_FailedRecordReprocessGatedByPhase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const entityID = "acme.ops.robotics.gcs.drone.phase"
	const rev = uint64(5)

	newFailedWorker := func() (*Storage, jetstream.KeyValueEntry, *genCounter) {
		index := newMemKV()
		s := NewStorage(nil, index, newMemKV())
		seedFailedRecord(t, s, entityID, "recover me", failReasonConnectionRefused, rev)
		entry, err := index.Get(ctx, entityID)
		if err != nil {
			t.Fatalf("Get seed: %v", err)
		}
		return s, entry, &genCounter{}
	}

	t.Run("snapshot phase reprocesses the failed record", func(t *testing.T) {
		s, entry, gen := newFailedWorker()
		w := &Worker{storage: s, embedder: &stubEmbedder{model: "m", dimensions: 3, generate: gen.generate}, ctx: ctx, logger: discardLogger()}

		_, _, terminal, outcome, _ := w.handleKVEntry(entry, true /*snapshot*/, 0)
		require.True(t, terminal)
		require.Equal(t, OutcomeGenerated, outcome)
		require.Equal(t, 1, gen.count(), "a failed record delivered in the restart snapshot must be re-embedded (#613 F3/D4)")

		rec, err := s.GetEmbedding(ctx, entityID)
		require.NoError(t, err)
		require.Equal(t, StatusGenerated, rec.Status)
	})

	t.Run("live phase skips the failed record", func(t *testing.T) {
		s, entry, gen := newFailedWorker()
		w := &Worker{storage: s, embedder: &stubEmbedder{model: "m", dimensions: 3, generate: gen.generate}, ctx: ctx, logger: discardLogger()}

		_, _, terminal, _, _ := w.handleKVEntry(entry, false /*live*/, 0)
		require.False(t, terminal, "a live failed record must not be reprocessed (hot-loop guard)")
		require.Equal(t, 0, gen.count(), "a live failed record must not be re-embedded")

		rec, err := s.GetEmbedding(ctx, entityID)
		require.NoError(t, err)
		require.Equal(t, StatusFailed, rec.Status,
			"still failed; recovery rides the hop-1 re-SavePending backstop, not a live re-embed loop")
	})
}

// TestFailedRecordRecoveredByHop1RepublishBackstop proves the #613 F3 backstop: even if a
// failed snapshot record were misclassified as live and skipped (the residual sub-instruction
// window between the channel receive and the phase load), it is NOT stranded — hop-1
// re-delivers the entity from ENTITY_STATES on restart and re-writes a fresh StatusPending
// record, which is ALWAYS processed regardless of phase and regenerates it. This is why the
// phase gate can fail safe toward not-reprocessing without losing recovery.
func TestFailedRecordRecoveredByHop1RepublishBackstop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const entityID = "acme.ops.robotics.gcs.drone.backstop"
	index := newMemKV()
	s := NewStorage(nil, index, newMemKV())
	seedFailedRecord(t, s, entityID, "text", failReasonConnectionRefused, 5)

	gen := &genCounter{}
	w := &Worker{storage: s, embedder: &stubEmbedder{model: "m", dimensions: 3, generate: gen.generate}, ctx: ctx, logger: discardLogger()}

	// Live phase: the failed record is skipped (as if the race had misclassified it).
	failedEntry, err := index.Get(ctx, entityID)
	require.NoError(t, err)
	_, _, terminal, _, _ := w.handleKVEntry(failedEntry, false, 0)
	require.False(t, terminal)
	require.Equal(t, 0, gen.count())

	// BACKSTOP: hop-1's re-SavePending writes a fresh pending record (a LIVE pending write).
	require.NoError(t, s.SavePending(ctx, entityID, "", "text", 6))
	pendingEntry, err := index.Get(ctx, entityID)
	require.NoError(t, err)
	_, _, terminal2, outcome2, _ := w.handleKVEntry(pendingEntry, false, 0)
	require.True(t, terminal2)
	require.Equal(t, OutcomeGenerated, outcome2)
	require.Equal(t, 1, gen.count(),
		"hop-1's re-SavePending recovers a failed record even when snapshot reprocessing was skipped (#613 F3 backstop)")

	rec, err := s.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Equal(t, StatusGenerated, rec.Status)
}

// manualWatchKV is a memKV whose WatchAll delivers entries on a channel the test drives, so
// a >1-worker Start test can deliver a snapshot failed record and (only after it is observed
// reprocessed) the end-of-snapshot sentinel — deterministically, without depending on the
// goroutine interleaving.
type manualWatchKV struct {
	*memKV
	updates chan jetstream.KeyValueEntry
}

func (k *manualWatchKV) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return &memWatcher{updates: k.updates}, nil
}

// TestFailedSnapshotRecordReprocessed_MultiWorker is the finding's deterministic multi-worker
// (>1 worker) statement: a failed record delivered in the restart snapshot is reprocessed
// through the real Start -> WatchAll -> N-worker path. The sentinel is delivered only AFTER
// the reprocessing terminal is observed, so the phase captured at dequeue is unambiguously
// snapshot and the assertion cannot flake on scheduling.
func TestFailedSnapshotRecordReprocessed_MultiWorker(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	base := newMemKV()
	kv := &manualWatchKV{memKV: base, updates: make(chan jetstream.KeyValueEntry, 8)}
	s := NewStorage(nil, kv, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.snapmulti"
	seedFailedRecord(t, s, entityID, "recover me", failReasonConnectionRefused, 5)
	failedEntry, err := base.Get(ctx, entityID)
	require.NoError(t, err)

	gen := &genCounter{}
	done := make(chan string, 8)
	w := NewWorker(s, &stubEmbedder{model: "m", dimensions: 3, generate: gen.generate}, kv, discardLogger()).
		WithWorkers(3).
		WithOnTerminal(func(id string, _ uint64, _ TerminalOutcome, _ string) { done <- id })

	require.NoError(t, w.Start(ctx))
	defer func() { _ = w.Stop() }()

	// Deliver the failed record while still in the snapshot phase (no sentinel sent yet).
	kv.updates <- failedEntry
	waitForTerminal(t, done, entityID)
	// Complete the snapshot cleanly, then stop.
	kv.updates <- nil

	require.NoError(t, w.Stop())

	rec, err := s.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Equal(t, StatusGenerated, rec.Status,
		"a failed record in the restart snapshot must be reprocessed with >1 worker (#613 F3)")
	require.Equal(t, 1, gen.count())
}

// TestColdStartIdenticalContentCollapsesToOneGenerate is the #630 cold-start statement: when
// the embedder has not resolved its vector width (Dimensions()==0), DedupKey withholds a
// durable key, so generateShared receives "". Even then a burst of K workers holding
// byte-identical content must collapse to ONE paid Generate call via a stable in-process
// singleflight key derived from the content (width excluded).
//
// Fails without the fix because generateShared bypassed the singleflight for an empty key and
// called the embedder directly, issuing one paid call per worker at cold start.
func TestColdStartIdenticalContentCollapsesToOneGenerate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var genCount atomic.Int64
	var firstOnce sync.Once
	firstIn := make(chan struct{})
	release := make(chan struct{})

	// dimensions 0 == unresolved width == DedupKey withholds a durable key.
	embedder := &stubEmbedder{model: "m", dimensions: 0, generate: func([]string) ([][]float32, error) {
		genCount.Add(1)
		firstOnce.Do(func() { close(firstIn) })
		<-release
		return [][]float32{{1, 2, 3}}, nil
	}}
	w := &Worker{embedder: embedder, embedderType: "http", ctx: ctx, logger: discardLogger()}

	const K = 8
	start := make(chan struct{})
	results := make([][]float32, K)
	errsOut := make([]error, K)
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// EMPTY durable key — the cold-start condition.
			results[i], errsOut[i] = w.generateShared("", "identical cold-start content")
		}(i)
	}
	close(start)

	select {
	case <-firstIn:
	case <-time.After(5 * time.Second):
		t.Fatal("no Generate call arrived")
	}
	// Join window: peers reach singleflight.Do in microseconds; 150ms is ~1000x that. Too
	// short a window can only over-report (a false failure), never mask a regression.
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := genCount.Load(); got != 1 {
		t.Fatalf("Generate calls = %d, want 1: cold-start identical content (unresolved width) must still collapse to one call via the in-process singleflight key (#630)", got)
	}
	for i := 0; i < K; i++ {
		require.NoError(t, errsOut[i])
		require.Equal(t, []float32{1, 2, 3}, results[i])
	}
}

// TestScanFailed_NormalizesReasonAndSeedsRevision is the #613 F5 statement: the bootstrap
// scan collapses every stored reason to the bounded enum (or "unknown"), so a record written
// by a future or rolled-back worker cannot inject an arbitrary reason into the readiness
// envelope's histogram; and it seeds each record's SourceRevision for the revision-CAS
// baseline (#613 F1).
//
// Fails without the fix because ScanFailed copied Record.Reason verbatim, so an arbitrary
// stored string became an unbounded label.
func TestScanFailed_NormalizesReasonAndSeedsRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	kv := &replayKV{memKV: newMemKV()}
	s := NewStorage(nil, kv, newMemKV())

	save := func(rec *Record) {
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := kv.Put(ctx, rec.EntityID, data); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	save(&Record{EntityID: "e.valid", Status: StatusFailed, Reason: failReasonConnectionRefused, SourceRevision: 7})
	save(&Record{EntityID: "e.empty", Status: StatusFailed, Reason: "", SourceRevision: 8})
	save(&Record{EntityID: "e.arbitrary", Status: StatusFailed, Reason: "semembed-503-upstream-x9f2a", SourceRevision: 9})
	save(&Record{EntityID: "e.future", Status: StatusFailed, Reason: "quota_exceeded", SourceRevision: 10})

	got, err := s.ScanFailed(ctx)
	require.NoError(t, err)

	byID := map[string]FailedEntry{}
	for _, e := range got {
		byID[e.EntityID] = e
	}
	require.Len(t, byID, 4)

	require.Equal(t, failReasonConnectionRefused, byID["e.valid"].Reason, "a valid enum reason passes through")
	require.Equal(t, reasonUnknown, byID["e.empty"].Reason, "an empty reason collapses to unknown")
	require.Equal(t, reasonUnknown, byID["e.arbitrary"].Reason, "an arbitrary stored reason must collapse to unknown, not become a label (#613 F5)")
	require.Equal(t, reasonUnknown, byID["e.future"].Reason, "a future/unknown reason value collapses to unknown")

	require.Equal(t, uint64(7), byID["e.valid"].SourceRevision, "SourceRevision seeds the revision-CAS baseline (#613 F1)")
	require.Equal(t, uint64(9), byID["e.arbitrary"].SourceRevision)

	// Bounded: at most enum-size distinct reason values ever reach a label.
	distinct := map[string]bool{}
	for _, e := range got {
		distinct[e.Reason] = true
	}
	require.LessOrEqual(t, len(distinct), 7, "the reason histogram must stay bounded by the enum size")
}
