package embedding

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/storage"
)

// recordingWorkerMetrics is a WorkerMetrics double that counts the signals the
// evidence-integrity change adds (dedup skips, truncations) alongside the
// pre-existing counters, so a test can assert the worker actually CONSUMES the new
// metric methods rather than leaving them as phantom registrations (#623 / #602).
type recordingWorkerMetrics struct {
	mu           sync.Mutex
	dedupHits    int
	failed       int
	resolveErr   int
	resolved     int
	truncated    int
	dedupSkipped int
	skipReasons  []string
}

func (m *recordingWorkerMetrics) IncDedupHits()           { m.inc(&m.dedupHits) }
func (m *recordingWorkerMetrics) IncFailed()              { m.inc(&m.failed) }
func (m *recordingWorkerMetrics) SetPending(float64)      {}
func (m *recordingWorkerMetrics) IncContentResolveError() { m.inc(&m.resolveErr) }
func (m *recordingWorkerMetrics) IncContentResolved()     { m.inc(&m.resolved) }
func (m *recordingWorkerMetrics) IncTruncated()           { m.inc(&m.truncated) }

func (m *recordingWorkerMetrics) IncDedupSkipped(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dedupSkipped++
	m.skipReasons = append(m.skipReasons, reason)
}

func (m *recordingWorkerMetrics) inc(p *int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*p++
}

func (m *recordingWorkerMetrics) snapshot() recordingWorkerMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return recordingWorkerMetrics{
		dedupHits:    m.dedupHits,
		failed:       m.failed,
		resolveErr:   m.resolveErr,
		resolved:     m.resolved,
		truncated:    m.truncated,
		dedupSkipped: m.dedupSkipped,
		skipReasons:  append([]string(nil), m.skipReasons...),
	}
}

// mutableStore is a StreamableStore whose body can be overwritten at a stable key,
// exactly the producer behaviour that makes an ADDRESS-keyed dedup serve a stale
// vector forever. Used to prove hop-2 keys over the resolved CONTENT, not the key.
type mutableStore struct {
	mu   sync.Mutex
	body string
}

func (m *mutableStore) set(b string) {
	m.mu.Lock()
	m.body = b
	m.mu.Unlock()
}

func (m *mutableStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	m.mu.Lock()
	b := m.body
	m.mu.Unlock()
	return io.NopCloser(strings.NewReader(b)), nil
}
func (m *mutableStore) Put(context.Context, string, []byte) error   { return nil }
func (m *mutableStore) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (m *mutableStore) List(context.Context, string) ([]string, error) {
	return nil, nil
}
func (m *mutableStore) Delete(context.Context, string) error { return nil }

// genCounter wraps a generation-counting embedder, capturing each embedded text so
// a test can prove WHICH bytes were embedded, not merely how many times.
type genCounter struct {
	mu    sync.Mutex
	texts []string
}

func (g *genCounter) generate(texts []string) ([][]float32, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	got := ""
	if len(texts) > 0 {
		got = texts[0]
	}
	g.texts = append(g.texts, got)
	return [][]float32{{1, 2, 3}}, nil
}

func (g *genCounter) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.texts)
}

func (g *genCounter) embeddedTexts() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.texts...)
}

// TestOffloadedLaneDedupsOnIdenticalBytes is the primary D1 (#623) statement: an
// offloaded (StorageRef) entity and an inline entity that resolve to byte-identical
// text must produce the SAME dedup key, so the second is served from the dedup
// store without regeneration.
//
// Both pending records carry an EMPTY ContentHash — exactly what hop-1 writes after
// the derivation moves to hop-2. On the pre-fix worker an empty ContentHash disables
// dedup for BOTH lanes, so the second entity regenerates: two generations, the
// offloaded-lane re-embed cost #623 measured (68 -> 191 per statistical run). With
// the key derived in hop-2 over the resolved+truncated body, both lanes converge on
// one key and the second dedups: exactly one generation.
func TestOffloadedLaneDedupsOnIdenticalBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	const body = "the quick brown fox jumps over the lazy dog"
	gen := &genCounter{}
	embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: gen.generate}

	resolver := fakeResolver{stores: map[string]storage.StreamableStore{
		"objectstore": readerStore{data: body},
	}}

	done := make(chan string, 8)
	w := NewWorker(s, embedder, index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithStoreResolver(resolver).
		WithOnTerminal(func(id string, _ uint64) { done <- id })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	const offloadedID = "acme.ops.robotics.gcs.drone.offloaded"
	const inlineID = "acme.ops.robotics.gcs.drone.inline"

	// Offloaded first, then inline — one worker, so seed order is process order.
	if err := s.SavePendingWithStorageRef(ctx, offloadedID, "",
		&StorageRef{StorageInstance: "objectstore", Key: "k"}, nil, 1); err != nil {
		t.Fatalf("SavePendingWithStorageRef: %v", err)
	}
	if err := s.SavePending(ctx, inlineID, "", body, 2); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	waitForN(t, done, 2)
	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := gen.count(); got != 1 {
		t.Fatalf("Generate calls = %d, want 1: an offloaded and an inline entity with "+
			"identical resolved bytes must dedup on one hop-2 key (#623 offloaded-lane dedup)", got)
	}
}

// TestOverwritingStableStorageKeyRegenerates is the D1 anti-address-keying guard:
// the dedup key is content-addressed, so identical bodies at a stable ObjectStore
// key dedup, but overwriting that key with new content regenerates rather than
// serving the previous body's vector.
//
// A -> B at the same key with the same body: B must dedup (the fail-without-fix
// half — the pre-fix offloaded lane never dedups). Then the body is overwritten and
// C at the same key must regenerate (the anti-regression half — a naive fix that
// hashed the ObjectStore address would serve A's stale vector here forever).
func TestOverwritingStableStorageKeyRegenerates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	store := &mutableStore{}
	store.set("alpha alpha alpha body one")
	resolver := fakeResolver{stores: map[string]storage.StreamableStore{"objectstore": store}}

	gen := &genCounter{}
	embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: gen.generate}

	done := make(chan string, 8)
	w := NewWorker(s, embedder, index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithStoreResolver(resolver).
		WithOnTerminal(func(id string, _ uint64) { done <- id })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	ref := func() *StorageRef { return &StorageRef{StorageInstance: "objectstore", Key: "stable/key"} }

	// A: first sight of body one.
	if err := s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.1", "", ref(), nil, 1); err != nil {
		t.Fatalf("SavePending A: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.1")

	// B: same key, same body — must dedup.
	if err := s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.2", "", ref(), nil, 2); err != nil {
		t.Fatalf("SavePending B: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.2")

	// Producer overwrites the SAME key with a new body.
	store.set("beta beta beta body two")

	// C: same key, new body — must regenerate, never serve body one's vector.
	if err := s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.3", "", ref(), nil, 3); err != nil {
		t.Fatalf("SavePending C: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.3")

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	texts := gen.embeddedTexts()
	if len(texts) != 2 {
		t.Fatalf("Generate calls = %d (%v), want 2: identical body must dedup, an overwritten body must regenerate", len(texts), texts)
	}
	if texts[0] != "alpha alpha alpha body one" || texts[1] != "beta beta beta body two" {
		t.Fatalf("embedded texts = %v, want [body one, body two]: an overwritten key must never serve the previous body's vector", texts)
	}
}

// TestDedupKeyIsOverTruncatedEmbeddedBytes proves the hop-2 key is derived over the
// exact bytes embedded — the resolved+TRUNCATED text — and that hop-2 no longer
// consults the hop-1 Record.ContentHash (D1 + #602's key half).
//
// Two inline entities share the first `cap` bytes and differ only beyond them. They
// are seeded with DIFFERENT non-empty content hashes — what pre-fix hop-1 wrote,
// keyed over the UNtruncated text — so the pre-fix worker keys over bytes it never
// embeds and fails to dedup (two generations). Post-fix, hop-2 keys over the
// truncated body, which is identical for both, so the second dedups (one
// generation). This is why the cap participates in identity "for free": the key is
// over the truncated text.
func TestDedupKeyIsOverTruncatedEmbeddedBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	const capLen = 20
	// "commonprefixtext " is 17 chars with a trailing space at index 16, so both
	// texts truncate at that word boundary to the identical "commonprefixtext".
	common := "commonprefixtext "
	textX := common + "XXXXXXXXX tail x"
	textY := common + "YYYYYYYYY tail y"
	if truncateAtWord(textX, capLen) != truncateAtWord(textY, capLen) {
		t.Fatalf("test setup: truncated forms differ (%q vs %q)", truncateAtWord(textX, capLen), truncateAtWord(textY, capLen))
	}

	gen := &genCounter{}
	embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: gen.generate}

	done := make(chan string, 8)
	w := NewWorker(s, embedder, index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(capLen).
		WithOnTerminal(func(id string, _ uint64) { done <- id })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	// Non-empty, DIFFERING hashes over the untruncated text — the pre-fix hop-1
	// value hop-2 must now ignore.
	if err := s.SavePending(ctx, "acme.ops.a.b.c.x", "hash-of-untruncated-X", textX, 1); err != nil {
		t.Fatalf("SavePending X: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.x")
	if err := s.SavePending(ctx, "acme.ops.a.b.c.y", "hash-of-untruncated-Y", textY, 2); err != nil {
		t.Fatalf("SavePending Y: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.y")

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := gen.count(); got != 1 {
		t.Fatalf("Generate calls = %d, want 1: two texts identical within the cap must dedup on "+
			"the truncated bytes, and hop-2 must ignore the hop-1 ContentHash (#623/#602)", got)
	}
}

// TestTruncationEmitsSignal is the D2 (#602) observability half: truncating text to
// the cap must emit a signal so the bytes actually embedded are discoverable, not
// silently dropped. The pre-fix truncateAtWord path is silent, so the counter stays
// at zero.
func TestTruncationEmitsSignal(t *testing.T) {
	t.Parallel()

	truncM := &recordingWorkerMetrics{}
	wTrunc := &Worker{maxSourceTextLen: 10, metrics: truncM}
	if _, err := wTrunc.getSourceText(&Record{SourceText: "this text is definitely longer than ten characters"}); err != nil {
		t.Fatalf("getSourceText: %v", err)
	}
	if got := truncM.snapshot().truncated; got != 1 {
		t.Fatalf("truncation signal count = %d, want 1: truncation must be observable, not silent (#602)", got)
	}

	shortM := &recordingWorkerMetrics{}
	wShort := &Worker{maxSourceTextLen: 100, metrics: shortM}
	if _, err := wShort.getSourceText(&Record{SourceText: "short"}); err != nil {
		t.Fatalf("getSourceText: %v", err)
	}
	if got := shortM.snapshot().truncated; got != 0 {
		t.Fatalf("truncation signal count = %d, want 0: text within the cap must not signal truncation", got)
	}
}

// TestDedupSkippedIsCounted is the D1 observability half (#623 group 2): an entity
// embedded on a condition where dedup is not consulted increments
// dedup_skipped_total{reason}, so the avoided-reuse cost is visible rather than
// inferred. An embedder that has not resolved its dimensions yields no dedup key,
// so dedup is skipped; the pre-fix worker skips silently.
func TestDedupSkippedIsCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	// dimensions 0 == vector width unresolved == no dedup key.
	embedder := &stubEmbedder{model: "m", dimensions: 0, generate: func([]string) ([][]float32, error) {
		return [][]float32{{1, 2, 3}}, nil
	}}

	m := &recordingWorkerMetrics{}
	done := make(chan string, 4)
	w := NewWorker(s, embedder, index, discardLogger()).
		WithWorkers(1).
		WithMetrics(m).
		WithOnTerminal(func(id string, _ uint64) { done <- id })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if err := s.SavePending(ctx, "acme.ops.a.b.c.1", "some-nonempty-hash", "text to embed", 1); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	waitForTerminal(t, done, "acme.ops.a.b.c.1")
	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	snap := m.snapshot()
	if snap.dedupSkipped == 0 {
		t.Fatalf("dedup_skipped_total not incremented for an entity embedded without a resolvable dedup key; "+
			"the avoided-reuse cost must be counted, not inferred (#623). reasons=%v", snap.skipReasons)
	}
}

// waitForN blocks until n terminal outcomes have been reported, regardless of ID.
func waitForN(t *testing.T, done <-chan string, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("timed out waiting for %d terminal outcomes (got %d)", n, i)
		}
	}
}
