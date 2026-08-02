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
// fallout: where the panic recovery sits, and what a recovered panic is allowed to
// report.
//
// The recover() used to guard the whole processEmbeddings for-loop, so ANY panic
// returned from the loop and that worker goroutine was gone for the process
// lifetime — nothing respawns it. At the default 5 workers, five poison entries
// reduce the embedding pipeline to zero consumers with only ADR-066 watermark lag
// as an eventual signal. gh#527 bulk retention deletion is precisely the workload
// that produces concurrent tombstones at volume.
//
// Moving the recovery per-entry fixed that but exposed a second defect, which this
// test also pins: the terminal completion used to be a defer INSIDE handleKVEntry,
// and Go runs deferred functions during panic unwinding — so it fired BEFORE the
// recover() above it, draining the hop-1 readiness watermark for a record that
// stays durably pending (no markFailed, no SaveGenerated on the panic path). With
// the goroutine now surviving, that would silently advance the watermark on every
// panic instead of stalling it, i.e. report caught up over stranded work.
//
// Hence three properties, asserted together because the fix for either one alone
// re-breaks the other:
//
//  1. the worker goroutine survives (the healthy sibling still completes);
//  2. the poison record is still pending;
//  3. onTerminal never fired for it, so the watermark did not advance past it.
//
// Exactly one worker is configured so the poison entry and the healthy entry are
// guaranteed to land on the SAME goroutine — that also makes the poison entry's
// recovery strictly happen-before the healthy entry's terminal signal, which is
// what orders assertions 2 and 3. With more than one worker, a surviving sibling
// would process the healthy entry and property 1 would pass even unfixed.
func TestWorkerPanicCostsOneEntryNotTheGoroutine(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(nil, index, newMemKV())

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

	// Every terminal completion is recorded, not just awaited: the watermark
	// assertion is about an entity that must NEVER appear here.
	var terminalMu sync.Mutex
	var terminals []string

	w := NewWorker(s, embedder, index, slog.New(recorder)).
		WithWorkers(1).
		WithOnTerminal(func(entityID string, _ uint64, _ TerminalOutcome, _ string) {
			terminalMu.Lock()
			terminals = append(terminals, entityID)
			terminalMu.Unlock()
			done <- entityID
		})

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

	// Property 1. The healthy entity must still reach a terminal outcome. With the
	// recovery back around the loop, the worker goroutine died on the poison entry
	// and this times out.
	waitForTerminal(t, done, healthyID)

	// Stop cancels and joins the worker goroutines before the state assertions, so
	// no in-flight entry can still be writing storage or logs underneath them. The
	// single worker already orders the poison entry ahead of the healthy one; this
	// closes the remaining "still inside Stop's own drain" window.
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

	// Property 2. Nothing on the panic path marks the record failed or saves a
	// vector, so the durable truth is "still pending".
	poisonRec, err := s.GetEmbedding(ctx, poisonID)
	if err != nil {
		t.Fatalf("GetEmbedding(poison): %v", err)
	}
	if poisonRec == nil || poisonRec.Status != StatusPending {
		t.Fatalf("poison record status = %+v, want a record still in %q: a recovered panic completes nothing",
			poisonRec, StatusPending)
	}

	// Property 3, the readiness half. Completing the ADR-066 watermark for a record
	// that property 2 just showed is still pending would report the pipeline caught
	// up over stranded work — and, unlike the pre-recovery behavior that stalled the
	// watermark loudly, it would do so silently and on every panic.
	terminalMu.Lock()
	defer terminalMu.Unlock()
	for _, got := range terminals {
		if got == poisonID {
			t.Fatalf("onTerminal fired for %q after a recovered panic (terminals=%v); "+
				"the record is still pending, so the readiness watermark must not advance past its revision",
				poisonID, terminals)
		}
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
	s := NewStorage(nil, index, newMemKV())

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
		WithOnTerminal(func(entityID string, _ uint64, _ TerminalOutcome, _ string) { done <- entityID })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if err := s.SavePending(ctx, entityID, "", "some text", 7); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	waitForTerminal(t, done, entityID)

	// Stop before asserting on logs: joining the worker goroutines is what orders
	// the recovery frame's log write against this assertion. (onTerminal now fires
	// only on a normal return, so a crashing build would time out in
	// waitForTerminal above rather than reach here — but the join keeps the
	// assertion independent of which failure mode a regression takes.)
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
