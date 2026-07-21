package embedding

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// panicLogRecorder counts ERROR records mentioning a recovered panic, and keeps
// the messages for failure output. It is passed explicitly to NewWorker, never
// installed with slog.SetDefault, so these tests stay safe under t.Parallel().
type panicLogRecorder struct {
	mu      sync.Mutex
	records []string
}

func (r *panicLogRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *panicLogRecorder) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelError && strings.Contains(strings.ToLower(rec.Message), "panic") {
		r.mu.Lock()
		r.records = append(r.records, rec.Message)
		r.mu.Unlock()
	}
	return nil
}

func (r *panicLogRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *panicLogRecorder) WithGroup(string) slog.Handler      { return r }

func (r *panicLogRecorder) panics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.records...)
}

// TestWorkerPanicCostsOneEntryNotTheGoroutine pins the second half of the gh#614
// fallout: where the panic recovery sits.
//
// The recover() used to guard the whole processEmbeddings for-loop, so ANY panic
// returned from the loop and that worker goroutine was gone for the process
// lifetime — nothing respawns it. At the default 5 workers, five poison entries
// reduce the embedding pipeline to zero consumers with only ADR-066 watermark lag
// as an eventual signal. gh#527 bulk retention deletion is precisely the workload
// that produces concurrent tombstones at volume.
//
// Exactly one worker is configured so the poison entry and the healthy entry are
// guaranteed to land on the SAME goroutine. With more than one, a surviving
// sibling would process the healthy entry and the test would pass even unfixed.
func TestWorkerPanicCostsOneEntryNotTheGoroutine(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	const poisonID = "acme.ops.robotics.gcs.drone.poison"
	const healthyID = "acme.ops.robotics.gcs.drone.healthy"

	embedder := &stubEmbedder{
		model:      "test-model",
		dimensions: 3,
		generate: func(texts []string) ([][]float32, error) {
			if len(texts) > 0 && texts[0] == "POISON" {
				panic("simulated embedder fault")
			}
			return [][]float32{{1, 2, 3}}, nil
		},
	}

	recorder := &panicLogRecorder{}
	done := make(chan string, 8)

	w := NewWorker(s, embedder, index, slog.New(recorder)).
		WithWorkers(1).
		WithOnTerminal(func(entityID string, _ uint64) { done <- entityID })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	// Seeded after Start so the single watcher channel receives them in this exact
	// order: the poison entry is processed first.
	if err := s.SavePending(ctx, poisonID, "", "POISON", 1); err != nil {
		t.Fatalf("SavePending(poison): %v", err)
	}
	if err := s.SavePending(ctx, healthyID, "", "healthy text", 2); err != nil {
		t.Fatalf("SavePending(healthy): %v", err)
	}

	// The healthy entity must still reach a terminal outcome. Unfixed, the worker
	// goroutine died on the poison entry and this times out.
	waitForTerminal(t, done, healthyID)

	// Stop before asserting on logs. onTerminal fires from a defer inside
	// handleKVEntry, which during a panic runs BEFORE the recovery above it logs —
	// so the terminal signal alone does not order the log write. Stop cancels and
	// joins the worker goroutines, which does.
	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	rec, err := s.GetEmbedding(ctx, healthyID)
	if err != nil {
		t.Fatalf("GetEmbedding(healthy): %v", err)
	}
	if rec == nil || rec.Status != StatusGenerated {
		t.Fatalf("healthy entity did not complete after a sibling entry panicked: %+v", rec)
	}

	if got := recorder.panics(); len(got) != 1 {
		t.Fatalf("recovered panic log records = %d (%v), want exactly 1 (the poison entry)", len(got), got)
	}
}

// TestWorkerTombstoneDuringGenerationIsNotAPanic is the end-to-end statement of
// the storage half at the worker seam: an entity tombstoned mid-generation must
// resolve as a normal, quiet drop.
//
// The discriminator is the panic-log assertion. Both the unfixed and fixed builds
// end with no vector stored — unfixed because SaveGenerated crashed before its
// Put, fixed because the write was deliberately dropped — so "no vector" alone
// cannot tell them apart. Requiring that the panic recovery never fires is what
// distinguishes "handled" from "crashed and swallowed".
func TestWorkerTombstoneDuringGenerationIsNotAPanic(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	const entityID = "acme.ops.robotics.gcs.drone.tombstoned"

	embedder := &stubEmbedder{
		model:      "test-model",
		dimensions: 3,
		generate: func(_ []string) ([][]float32, error) {
			// The hop-1 entity tombstone lands while this round trip is in flight
			// (processor/graph-embedding/component.go calls DeleteEmbedding from the
			// ENTITY_STATES watcher goroutine).
			if err := s.DeleteEmbedding(ctx, entityID); err != nil {
				return nil, err
			}
			return [][]float32{{1, 2, 3}}, nil
		},
	}

	recorder := &panicLogRecorder{}
	done := make(chan string, 8)

	var generatedFired int
	var generatedMu sync.Mutex

	w := NewWorker(s, embedder, index, slog.New(recorder)).
		WithWorkers(1).
		WithOnGenerated(func(string, []float32) {
			generatedMu.Lock()
			generatedFired++
			generatedMu.Unlock()
		}).
		WithOnTerminal(func(entityID string, _ uint64) { done <- entityID })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if err := s.SavePending(ctx, entityID, "", "some text", 7); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	waitForTerminal(t, done, entityID)

	// Stop before asserting on logs: onTerminal runs from a defer that fires during
	// panic unwinding, i.e. BEFORE the recovery frame above it logs. Joining the
	// worker goroutines is what orders the log write against this assertion —
	// without it, an unfixed build races and can look clean.
	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := recorder.panics(); len(got) != 0 {
		t.Fatalf("a tombstone racing generation panicked: %v; it is a normal outcome and must be handled, not recovered", got)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec != nil {
		t.Fatalf("tombstoned entity regained an embedding record: %+v", rec)
	}

	// The vector must not reach downstream caches either — onGenerated populates
	// the query-side vector cache, so firing it would keep serving a dead entity
	// from memory even though KV is correctly empty.
	generatedMu.Lock()
	defer generatedMu.Unlock()
	if generatedFired != 0 {
		t.Fatalf("onGenerated fired %d times for a tombstoned entity; a dropped write must not notify downstream", generatedFired)
	}
}

// waitForTerminal blocks until the worker reports a terminal outcome for want.
func waitForTerminal(t *testing.T, done <-chan string, want string) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-done:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal outcome of %q; the worker goroutine is gone", want)
		}
	}
}
