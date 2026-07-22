package embedding

import (
	"context"
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
	got, err := w.getSourceText(&Record{SourceText: identity, StorageRef: offloadedRef()})
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
	got, err := w.getSourceText(&Record{SourceText: "", StorageRef: offloadedRef()})
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
	got, err := w.getSourceText(&Record{SourceText: identity, StorageRef: offloadedRef()})
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
	if _, err := w.getSourceText(&Record{SourceText: "Identity Prefix", StorageRef: offloadedRef()}); err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	snap := m.snapshot()
	if snap.truncated != 1 {
		t.Fatalf("truncated = %d, want exactly 1: the offloaded lane must count the over-cap body once, "+
			"not double-count when identity prepends push the combined back over the cap (#602/D3)", snap.truncated)
	}
	if snap.identityIncluded != 1 {
		t.Fatalf("identityIncluded = %d, want 1: an offloaded entity carrying identity text must be counted included", snap.identityIncluded)
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
	got, err := w.getSourceText(&Record{SourceText: identity, StorageRef: offloadedRef()})
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
	got, err := w.getSourceText(&Record{SourceText: identity, StorageRef: offloadedRef()})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}

	if got != identity {
		t.Fatalf("empty body must embed the identity alone; got %q, want %q", got, identity)
	}
	if strings.Contains(got, identityBodySeparator) {
		t.Fatalf("identity-alone must carry no trailing separator; got %q", got)
	}
	if snap := m.snapshot(); snap.identityIncluded != 1 {
		t.Fatalf("identityIncluded = %d, want 1: an identity-carrying offloaded entity counts included even with an empty body", snap.identityIncluded)
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

// TestOffloadedIdentityMetric_IncludedAndAbsent is the D5 observability pair: an
// offloaded entity carrying identity text increments the included counter, and one
// without increments the absent counter — so a producer can confirm text_suffixes took
// effect on the offloaded lane rather than infer it from silence.
func TestOffloadedIdentityMetric_IncludedAndAbsent(t *testing.T) {
	t.Run("included", func(t *testing.T) {
		m := &recordingWorkerMetrics{}
		w := offloadedWorker(t, "some body", 4000, m)
		if _, err := w.getSourceText(&Record{SourceText: "Title Here", StorageRef: offloadedRef()}); err != nil {
			t.Fatalf("getSourceText: %v", err)
		}
		snap := m.snapshot()
		if snap.identityIncluded != 1 || snap.identityAbsent != 0 {
			t.Fatalf("identity present: included=%d absent=%d, want included=1 absent=0", snap.identityIncluded, snap.identityAbsent)
		}
	})

	t.Run("absent", func(t *testing.T) {
		m := &recordingWorkerMetrics{}
		w := offloadedWorker(t, "some body", 4000, m)
		if _, err := w.getSourceText(&Record{SourceText: "", StorageRef: offloadedRef()}); err != nil {
			t.Fatalf("getSourceText: %v", err)
		}
		snap := m.snapshot()
		if snap.identityIncluded != 0 || snap.identityAbsent != 1 {
			t.Fatalf("identity absent: included=%d absent=%d, want included=0 absent=1", snap.identityIncluded, snap.identityAbsent)
		}
	})

	t.Run("inline lane fires neither", func(t *testing.T) {
		m := &recordingWorkerMetrics{}
		w := &Worker{ctx: context.Background(), maxSourceTextLen: 4000, metrics: m}
		if _, err := w.getSourceText(&Record{SourceText: "inline only"}); err != nil {
			t.Fatalf("getSourceText: %v", err)
		}
		snap := m.snapshot()
		if snap.identityIncluded != 0 || snap.identityAbsent != 0 {
			t.Fatalf("inline lane: included=%d absent=%d, want both 0 (offloaded-only observable)", snap.identityIncluded, snap.identityAbsent)
		}
	})
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

	keyFor := func(t *testing.T, sourceText, storeBody string) string {
		t.Helper()
		w := offloadedWorker(t, storeBody, 4000, nil)
		text, err := w.getSourceText(&Record{SourceText: sourceText, StorageRef: offloadedRef()})
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
