package embedding

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/c360studio/semstreams/storage"
)

// offloadedWorker builds a Worker wired to a single-body reader store under the
// federated resolver, so getSourceText exercises the real offloaded fetch path.
func offloadedWorker(t *testing.T, body string, maxLen int, m WorkerMetrics) *Worker {
	t.Helper()
	return &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: maxLen,
		metrics:          m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"objectstore": readerStore{data: body},
		}},
	}
}

func offloadedRef() *StorageRef {
	return &StorageRef{StorageInstance: "objectstore", Key: "k"}
}

// TestGetSourceText_OffloadedIdentityFirst is the D1 core: an offloaded record that
// carries inline identity text embeds identity, then the frozen separator, then the
// body — in that exact order.
func TestGetSourceText_OffloadedIdentityFirst(t *testing.T) {
	const identity = "Retry Policy Exponential Backoff"
	const body = "the handler waits between attempts and gives up after a ceiling"

	w := offloadedWorker(t, body, 4000, nil)
	got, err := w.getSourceText(&Record{IdentityText: identity, StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	want := identity + identityBodySeparator + body
	if got != want {
		t.Fatalf("combined text = %q, want identity-first %q", got, want)
	}
	if !strings.HasPrefix(got, identity) {
		t.Fatalf("identity must lead the combined text; got %q", got)
	}
}

// TestGetSourceText_OffloadedNoIdentity_BodyOnly: an offloaded record with no inline
// identity text embeds the body alone (today's behavior, no regression).
func TestGetSourceText_OffloadedNoIdentity_BodyOnly(t *testing.T) {
	const body = "device reports voltage and current at fixed intervals"

	w := offloadedWorker(t, body, 4000, nil)
	got, err := w.getSourceText(&Record{IdentityText: "", StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}
	if got != body {
		t.Fatalf("body-only text = %q, want %q (no separator, no prefix)", got, body)
	}
	if strings.Contains(got, identityBodySeparator) {
		t.Fatalf("body-only text must not carry the identity separator; got %q", got)
	}
}

// TestGetSourceText_InlineLane_Unchanged: an inline record (no StorageRef) returns its
// SourceText unchanged — the re-branch must not disturb the inline lane.
func TestGetSourceText_InlineLane_Unchanged(t *testing.T) {
	const text = "wholly inline text extracted from triples"

	w := &Worker{ctx: context.Background(), maxSourceTextLen: 4000}
	got, err := w.getSourceText(&Record{SourceText: text})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}
	if got != text {
		t.Fatalf("inline text = %q, want unchanged %q", got, text)
	}
}

// TestGetSourceText_CapKeepsIdentityTrimsBody is the D3 statement: when the combined
// identity+body exceeds the cap, identity-first ordering means the identity survives
// and the body is trimmed from the END.
func TestGetSourceText_CapKeepsIdentityTrimsBody(t *testing.T) {
	const identity = "alpha"
	const body = "one two three four five six seven eight nine ten"
	const capLen = 20 // runes; identity(5)+separator(2)=7, so ~13 body runes survive

	w := offloadedWorker(t, body, capLen, nil)
	got, err := w.getSourceText(&Record{IdentityText: identity, StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	if !strings.HasPrefix(got, identity+identityBodySeparator) {
		t.Fatalf("identity + separator must survive the cap at the front; got %q", got)
	}
	if utf8.RuneCountInString(got) > capLen {
		t.Fatalf("combined text = %d runes, must not exceed the cap of %d", utf8.RuneCountInString(got), capLen)
	}
	if strings.Contains(got, "ten") {
		t.Fatalf("the body tail must be trimmed; got %q still contains the last word", got)
	}
}

// TestGetSourceText_OffloadedTruncationCountedOnce guards D3's no-double-count: an
// over-cap offloaded body is counted once (in fetchTextFromStorage). Prepending
// identity and re-truncating the combined text must NOT count it a second time.
func TestGetSourceText_OffloadedTruncationCountedOnce(t *testing.T) {
	// A body well over the cap so fetchTextFromStorage truncates + counts exactly once.
	body := strings.Repeat("word ", 200) // 1000 runes, cap 50
	const capLen = 50

	m := &recordingWorkerMetrics{}
	w := offloadedWorker(t, body, capLen, m)
	if _, err := w.getSourceText(&Record{IdentityText: "Identity Prefix", StorageRef: offloadedRef()}); err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	snap := m.snapshot()
	if snap.truncated != 1 {
		t.Fatalf("truncated = %d, want exactly 1: the offloaded lane must count the over-cap body once, "+
			"not double-count when identity prepends push the combined back over the cap (#602/D3)", snap.truncated)
	}
	// getSourceText produces text; it does NOT count the offloaded-identity pair (that
	// moved to the successful-persistence path, #635 retro F3). Assert it stays silent here.
	if snap.identityIncluded != 0 || snap.identityAbsent != 0 {
		t.Fatalf("getSourceText must not touch the identity counters: included=%d absent=%d, want both 0 (#635 F3)",
			snap.identityIncluded, snap.identityAbsent)
	}
}

// TestGetSourceText_IdentityTipsCombinedOverCap_CountsOnce is the FIX 1 edge: a body
// that FITS the cap but whose identity+separator+body EXCEEDS it is truncated at the
// combined stage — and that truncation MUST be counted exactly once (fetchTextFromStorage
// did not truncate the body, so getSourceText owns the count here). Before the fix this
// truncation was silent — the exact class #602 exists to prevent.
func TestGetSourceText_IdentityTipsCombinedOverCap_CountsOnce(t *testing.T) {
	const capLen = 40
	const identity = "Distinctive Title Phrase"   // 24 runes, survives at the front
	body := strings.Repeat("x", 26) + " TAILWORD" // 35 runes < cap, so the BODY itself is not truncated

	if utf8.RuneCountInString(body) >= capLen {
		t.Fatalf("test setup: body must fit under the cap; got %d runes", utf8.RuneCountInString(body))
	}

	m := &recordingWorkerMetrics{}
	w := offloadedWorker(t, body, capLen, m)
	got, err := w.getSourceText(&Record{IdentityText: identity, StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	if utf8.RuneCountInString(got) > capLen {
		t.Fatalf("combined text = %d runes, must be truncated to the cap of %d", utf8.RuneCountInString(got), capLen)
	}
	if !strings.HasPrefix(got, identity) {
		t.Fatalf("identity must survive at the front; got %q", got)
	}
	if strings.Contains(got, "TAILWORD") {
		t.Fatalf("the body tail must be trimmed by the combined truncation; got %q", got)
	}
	if snap := m.snapshot(); snap.truncated != 1 {
		t.Fatalf("truncated = %d, want exactly 1: a body that fits but whose combined text exceeds the cap "+
			"must be counted once — not silent (#602), not double-counted", snap.truncated)
	}
}

// TestGetSourceText_EmptyBodyIdentityOnly is the FIX 2 edge: an offloaded record with a
// non-empty identity but an EMPTY body embeds the identity ALONE — no trailing separator
// noise — so it dedup-collapses against an inline entity whose whole text is that same
// identity. It still counts as identity-included.
func TestGetSourceText_EmptyBodyIdentityOnly(t *testing.T) {
	const identity = "Standalone Signature Identity"

	m := &recordingWorkerMetrics{}
	w := offloadedWorker(t, "", 4000, m) // empty offloaded body
	got, err := w.getSourceText(&Record{IdentityText: identity, StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	if got != identity {
		t.Fatalf("empty body must embed the identity alone; got %q, want %q", got, identity)
	}
	if strings.Contains(got, identityBodySeparator) {
		t.Fatalf("identity-alone must carry no trailing separator; got %q", got)
	}
	// getSourceText no longer counts the identity pair (moved to the persistence path,
	// #635 retro F3) — this test asserts only the bytes it produces.
	if snap := m.snapshot(); snap.identityIncluded != 0 || snap.identityAbsent != 0 {
		t.Fatalf("getSourceText must not touch the identity counters: included=%d absent=%d, want both 0 (#635 F3)",
			snap.identityIncluded, snap.identityAbsent)
	}

	// Cross-lane dedup: an inline record whose whole SourceText is the same identity must
	// derive the SAME dedup key — the trailing-separator noise would have split them.
	id := EmbedderIdentity{Type: "bm25", Model: "bm25-384", Dimensions: 384, MaxTextLen: 4000}
	inlineWorker := &Worker{ctx: context.Background(), maxSourceTextLen: 4000}
	inlineText, err := inlineWorker.getSourceText(&Record{SourceText: identity})
	if err != nil {
		t.Fatalf("inline getSourceText: %v", err)
	}
	if DedupKey(id, got) != DedupKey(id, inlineText) {
		t.Fatalf("empty-body offloaded identity must key identically to the inline identity (#627 stays moot)")
	}
}

// TestOffloadedIdentityMetric_CountsStoredVectorsOnly is the D5 observability pair AND
// #635 retro F3: the offloaded-identity counters count STORED vectors, not text-production
// attempts. They fire on the successful-persistence path (the same terminal path as
// IncDedupHits), so this drives the whole worker (Start → SavePending* → terminal) rather
// than calling getSourceText, and proves:
//
//   - an offloaded entity with identity text whose embed STORES → included +1, absent 0;
//   - an offloaded entity WITHOUT identity whose embed STORES → absent +1, included 0;
//   - an offloaded entity whose embed FAILS → NEITHER (no vector was stored);
//   - an offloaded entity with neither identity nor body → NEITHER (never embeds);
//   - an inline entity → NEITHER (offloaded-only observable).
func TestOffloadedIdentityMetric_CountsStoredVectorsOnly(t *testing.T) {
	// runWorker seeds one record via seed, drives the real Start→WatchAll→handleKVEntry
	// wire to a terminal outcome, and returns the metric snapshot.
	runWorker := func(t *testing.T, body string, gen func([]string) ([][]float32, error), seed func(ctx context.Context, s *Storage) error) metricsSnapshot {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		index := newWatchableKV()
		s := NewStorage(index, newMemKV())
		embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: gen}
		resolver := fakeResolver{stores: map[string]storage.StreamableStore{"objectstore": readerStore{data: body}}}

		m := &recordingWorkerMetrics{}
		done := make(chan string, 4)
		w := NewWorker(s, embedder, index, discardLogger()).
			WithWorkers(1).
			WithMaxSourceTextLen(4000).
			WithStoreResolver(resolver).
			WithMetrics(m).
			WithOnTerminal(func(id string, _ uint64) { done <- id })

		if err := w.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = w.Stop() }()

		if err := seed(ctx, s); err != nil {
			t.Fatalf("seed: %v", err)
		}
		waitForN(t, done, 1)
		if err := w.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		return m.snapshot()
	}

	okGen := func([]string) ([][]float32, error) { return [][]float32{{1, 2, 3}}, nil }
	failGen := func([]string) ([][]float32, error) { return nil, fmt.Errorf("embedder down") }

	t.Run("stored with identity → included", func(t *testing.T) {
		snap := runWorker(t, "some body", okGen, func(ctx context.Context, s *Storage) error {
			return s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.inc", "", "Title Here", offloadedRef(), nil, 1)
		})
		if snap.identityIncluded != 1 || snap.identityAbsent != 0 {
			t.Fatalf("stored with identity: included=%d absent=%d, want included=1 absent=0", snap.identityIncluded, snap.identityAbsent)
		}
	})

	t.Run("stored body-only → absent", func(t *testing.T) {
		snap := runWorker(t, "some body", okGen, func(ctx context.Context, s *Storage) error {
			return s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.abs", "", "", offloadedRef(), nil, 1)
		})
		if snap.identityIncluded != 0 || snap.identityAbsent != 1 {
			t.Fatalf("stored body-only: included=%d absent=%d, want included=0 absent=1", snap.identityIncluded, snap.identityAbsent)
		}
	})

	t.Run("embed fails → neither", func(t *testing.T) {
		snap := runWorker(t, "some body", failGen, func(ctx context.Context, s *Storage) error {
			return s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.fail", "", "Title Here", offloadedRef(), nil, 1)
		})
		if snap.identityIncluded != 0 || snap.identityAbsent != 0 {
			t.Fatalf("failed embed: included=%d absent=%d, want both 0 — no vector was stored (#635 F3)", snap.identityIncluded, snap.identityAbsent)
		}
		if snap.failed != 1 {
			t.Fatalf("failed = %d, want 1: the failure path must have run for this assertion to be meaningful", snap.failed)
		}
	})

	t.Run("no identity and empty body → neither", func(t *testing.T) {
		snap := runWorker(t, "", okGen, func(ctx context.Context, s *Storage) error {
			return s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.empty", "", "", offloadedRef(), nil, 1)
		})
		if snap.identityIncluded != 0 || snap.identityAbsent != 0 {
			t.Fatalf("no text at all: included=%d absent=%d, want both 0 — the entity never embeds", snap.identityIncluded, snap.identityAbsent)
		}
	})

	t.Run("inline lane → neither", func(t *testing.T) {
		snap := runWorker(t, "unused", okGen, func(ctx context.Context, s *Storage) error {
			return s.SavePending(ctx, "acme.ops.a.b.c.inline", "", "wholly inline text", 1)
		})
		if snap.identityIncluded != 0 || snap.identityAbsent != 0 {
			t.Fatalf("inline lane: included=%d absent=%d, want both 0 (offloaded-only observable)", snap.identityIncluded, snap.identityAbsent)
		}
	})
}

// TestOffloadedIdentityMetric_DedupServedPathCountsBoth pins the #628 subset-metric
// invariant on the offloaded lane: the offloaded-identity counter is a subset of
// resolutions and MUST fire on the SAME terminal path as IncDedupHits — including when
// the vector is served from the DEDUP store (wasDedupHit), not freshly generated. The
// counter is gated on StorageRef != nil INDEPENDENT of wasDedupHit; this guards against a
// future change accidentally coupling it to !wasDedupHit (a fresh-generate-only counter).
//
// Two offloaded records resolve to byte-identical text: the first fresh-generates and
// populates the dedup store, the second is served from dedup and STORES. On that second
// (dedup-served) entity, IncDedupHits and the offloaded-identity counter must each move by
// exactly 1, together — proven by the delta between the two snapshots, with gen.count()==1
// confirming the second entity was truly served from dedup rather than regenerated.
func TestOffloadedIdentityMetric_DedupServedPathCountsBoth(t *testing.T) {
	// run processes two identical-bytes offloaded records under one worker, returning the
	// metric snapshot after the fresh-generate entity and after the dedup-served entity,
	// plus the total Generate calls (1 == the second was served from dedup).
	run := func(t *testing.T, identity string) (fresh, served metricsSnapshot, generateCalls int) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const body = "the device reports voltage and current at fixed intervals"
		index := newWatchableKV()
		s := NewStorage(index, newMemKV())
		gen := &genCounter{}
		embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: gen.generate}
		resolver := fakeResolver{stores: map[string]storage.StreamableStore{"objectstore": readerStore{data: body}}}

		m := &recordingWorkerMetrics{}
		done := make(chan string, 4)
		w := NewWorker(s, embedder, index, discardLogger()).
			WithWorkers(1). // single goroutine → seed order is process order, so #1 populates dedup before #2
			WithMaxSourceTextLen(4000).
			WithStoreResolver(resolver).
			WithMetrics(m).
			WithOnTerminal(func(id string, _ uint64) { done <- id })
		if err := w.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = w.Stop() }()

		// #1: fresh generate, populates the dedup store.
		if err := s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.first", "", identity, offloadedRef(), nil, 1); err != nil {
			t.Fatalf("SavePendingWithStorageRef(first): %v", err)
		}
		waitForTerminal(t, done, "acme.ops.a.b.c.first")
		fresh = m.snapshot()

		// #2: identical resolved bytes → served from dedup (wasDedupHit) and stores.
		if err := s.SavePendingWithStorageRef(ctx, "acme.ops.a.b.c.second", "", identity, offloadedRef(), nil, 2); err != nil {
			t.Fatalf("SavePendingWithStorageRef(second): %v", err)
		}
		waitForTerminal(t, done, "acme.ops.a.b.c.second")
		served = m.snapshot()

		if err := w.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		return fresh, served, gen.count()
	}

	t.Run("with identity → dedup_hits and identity_included fire together", func(t *testing.T) {
		fresh, served, generateCalls := run(t, "Title Here")
		if generateCalls != 1 {
			t.Fatalf("Generate calls = %d, want 1: the second entity must be SERVED from dedup, not regenerated", generateCalls)
		}
		if got := served.dedupHits - fresh.dedupHits; got != 1 {
			t.Fatalf("dedupHits delta on the dedup-served entity = %d, want 1", got)
		}
		if got := served.identityIncluded - fresh.identityIncluded; got != 1 {
			t.Fatalf("identityIncluded delta on the dedup-served entity = %d, want 1: the offloaded-identity counter must fire on the "+
				"dedup-served terminal path too, together with IncDedupHits (#628 subset-metric lesson)", got)
		}
		if served.identityAbsent != 0 {
			t.Fatalf("identityAbsent = %d, want 0: both entities carried identity", served.identityAbsent)
		}
	})

	t.Run("body-only → dedup_hits and identity_absent fire together", func(t *testing.T) {
		fresh, served, generateCalls := run(t, "")
		if generateCalls != 1 {
			t.Fatalf("Generate calls = %d, want 1: the second entity must be SERVED from dedup, not regenerated", generateCalls)
		}
		if got := served.dedupHits - fresh.dedupHits; got != 1 {
			t.Fatalf("dedupHits delta on the dedup-served entity = %d, want 1", got)
		}
		if got := served.identityAbsent - fresh.identityAbsent; got != 1 {
			t.Fatalf("identityAbsent delta on the dedup-served entity = %d, want 1: the body-only offloaded counter must fire on the "+
				"dedup-served terminal path too, together with IncDedupHits (#628 subset-metric lesson)", got)
		}
		if served.identityIncluded != 0 {
			t.Fatalf("identityIncluded = %d, want 0: neither entity carried identity", served.identityIncluded)
		}
	})
}

// TestSavePendingWithStorageRef_OldWorkerSafeWire is #635 retro F1's cross-version wire
// invariant: an offloaded pending record must carry its identity in IdentityText with
// SourceText EMPTY. A pre-#635 worker is SourceText-primary; seeing SourceText == "" with
// StorageRef set, it falls back to fetching the body (safe) instead of embedding
// identity-only and dropping the body. This asserts the exact durable shape old workers
// rely on.
func TestSavePendingWithStorageRef_OldWorkerSafeWire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewStorage(newMemKV(), newMemKV())

	const entityID = "acme.ops.a.b.c.wire"
	const identity = "Distinctive Signature Identity"
	if err := s.SavePendingWithStorageRef(ctx, entityID, "", identity, offloadedRef(), nil, 1); err != nil {
		t.Fatalf("SavePendingWithStorageRef: %v", err)
	}

	rec, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if rec == nil {
		t.Fatal("no pending record written")
	}
	if rec.SourceText != "" {
		t.Fatalf("SourceText = %q, want empty: an offloaded record must NOT carry identity in SourceText, "+
			"or a pre-#635 worker embeds identity-only and drops the body (#635 F1)", rec.SourceText)
	}
	if rec.IdentityText != identity {
		t.Fatalf("IdentityText = %q, want %q: the offloaded identity must travel in the distinct field", rec.IdentityText, identity)
	}
	if rec.StorageRef == nil {
		t.Fatal("StorageRef must be set on an offloaded record")
	}
}

// TestDedupKey_TracksIdentityAndBody proves D4: the hop-2 dedup key is derived over
// getSourceText's combined output with NO extra machinery, so a change to EITHER the
// identity OR the body changes the key (a correct re-embed), and byte-identical
// combined content across the offloaded and inline lanes yields the SAME key (#627
// stays moot).
func TestDedupKey_TracksIdentityAndBody(t *testing.T) {
	const identity = "Retry Policy"
	const body = "waits between attempts"

	id := EmbedderIdentity{Type: "bm25", Model: "bm25-384", Dimensions: 384, MaxTextLen: 4000}

	keyFor := func(t *testing.T, identityText, storeBody string) string {
		t.Helper()
		w := offloadedWorker(t, storeBody, 4000, nil)
		text, err := w.getSourceText(&Record{IdentityText: identityText, StorageRef: offloadedRef()})
		if err != nil {
			t.Fatalf("getSourceText: %v", err)
		}
		return DedupKey(id, text)
	}

	base := keyFor(t, identity, body)
	changedIdentity := keyFor(t, "Timeout Policy", body)
	changedBody := keyFor(t, identity, "gives up immediately")

	if base == changedIdentity {
		t.Fatalf("dedup key must change when the identity text changes (bytes embedded changed)")
	}
	if base == changedBody {
		t.Fatalf("dedup key must change when the body changes (bytes embedded changed)")
	}
	if changedIdentity == changedBody {
		t.Fatalf("distinct identity and body changes must not collide on the same key")
	}

	// Cross-lane: an INLINE record whose whole SourceText equals the offloaded lane's
	// combined bytes must derive the SAME key — the key is over the embedded bytes, not
	// the lane (#627 stays moot).
	combined := identity + identityBodySeparator + body
	inlineWorker := &Worker{ctx: context.Background(), maxSourceTextLen: 4000}
	inlineText, err := inlineWorker.getSourceText(&Record{SourceText: combined})
	if err != nil {
		t.Fatalf("inline getSourceText: %v", err)
	}
	if got := DedupKey(id, inlineText); got != base {
		t.Fatalf("byte-identical combined content must key identically across lanes; inline=%s offloaded=%s", got, base)
	}
}
