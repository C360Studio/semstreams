package embedding

import (
	"context"
	"testing"
)

// TestDedupKey_DistinguishesEmbedderIdentity guards gh#612.
//
// EMBEDDING_DEDUP is durable, has no TTL, and is never cleared. Before this
// guard the key was SHA-256(text) alone, so an operator switching
// embedder_type bm25 -> http against the same NATS state got every
// already-embedded entity's BM25 vector back, written into EMBEDDING_INDEX
// labelled with the neural model's name. Both default to 384 dimensions, so
// CosineSimilarity produced a plausible-looking score across two unrelated
// vector spaces and every health signal stayed green while dedup_hits_total
// ROSE — the failure reads as a cost win.
//
// The property under test: identical text under any differing embedder
// identity MUST produce a different key.
func TestDedupKey_DistinguishesEmbedderIdentity(t *testing.T) {
	const text = "the quick brown fox jumps over the lazy dog"

	base := EmbedderIdentity{
		Type:       "bm25",
		Model:      "bm25-384",
		Dimensions: 384,
		MaxTextLen: 4000,
	}

	tests := []struct {
		name  string
		ident EmbedderIdentity
	}{
		{
			// The exact operator action in the report: statistical.json ->
			// semantic.json. Type, model AND cap all move together.
			name: "embedder type switch bm25 to http",
			ident: EmbedderIdentity{
				Type: "http", Model: "all-MiniLM-L6-v2", Dimensions: 384, MaxTextLen: 8000,
			},
		},
		{
			name:  "same type and dimensions, different model",
			ident: EmbedderIdentity{Type: "http", Model: "bge-small-en", Dimensions: 384, MaxTextLen: 8000},
		},
		{
			// Dimension change alone: CosineSimilarity returns 0.0 on a length
			// mismatch, so a stale hit here silently ranks the entity last forever.
			name:  "same model, different dimensions",
			ident: EmbedderIdentity{Type: "bm25", Model: "bm25-384", Dimensions: 768, MaxTextLen: 4000},
		},
		{
			// The compounding defect: the cap changes WHICH TEXT was embedded
			// while the raw input string is byte-identical.
			name:  "same model, different truncation cap",
			ident: EmbedderIdentity{Type: "bm25", Model: "bm25-384", Dimensions: 384, MaxTextLen: 8000},
		},
		{
			name:  "same everything except type label",
			ident: EmbedderIdentity{Type: "http", Model: "bm25-384", Dimensions: 384, MaxTextLen: 4000},
		},
	}

	baseKey := DedupKey(base, text)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DedupKey(tt.ident, text)
			if got == baseKey {
				t.Fatalf("dedup key collides across embedder identities: %+v and %+v both key to %s",
					base, tt.ident, got)
			}
		})
	}

	t.Run("identical identity and text is stable", func(t *testing.T) {
		if again := DedupKey(base, text); again != baseKey {
			t.Fatalf("DedupKey is not deterministic: %s then %s", baseKey, again)
		}
	})

	t.Run("different text under same identity differs", func(t *testing.T) {
		if other := DedupKey(base, text+"!"); other == baseKey {
			t.Fatal("dedup key ignores text")
		}
	})
}

// TestDedupKey_FieldsAreUnambiguous checks that field boundaries are encoded, so
// shifting a character across two adjacent fields cannot produce the same key.
// Without length prefixing, {"ab","c"} and {"a","bc"} serialize identically.
func TestDedupKey_FieldsAreUnambiguous(t *testing.T) {
	a := DedupKey(EmbedderIdentity{Type: "ab", Model: "c", Dimensions: 1, MaxTextLen: 1}, "x")
	b := DedupKey(EmbedderIdentity{Type: "a", Model: "bc", Dimensions: 1, MaxTextLen: 1}, "x")
	if a == b {
		t.Fatal("field boundaries are not encoded: adjacent fields concatenate ambiguously")
	}

	// The text field must not be able to absorb an identity field either.
	c := DedupKey(EmbedderIdentity{Type: "a", Model: "b", Dimensions: 1, MaxTextLen: 1}, "cx")
	d := DedupKey(EmbedderIdentity{Type: "a", Model: "bc", Dimensions: 1, MaxTextLen: 1}, "x")
	if c == d {
		t.Fatal("text field boundary is not encoded")
	}
}

// TestSaveDedup_RecordsVectorSpace guards the second half of gh#612: a dedup
// record carried no model, so a stale-model vector was undetectable after the
// fact even in a forensic read of the bucket.
func TestSaveDedup_RecordsVectorSpace(t *testing.T) {
	ctx := context.Background()
	dedup := newMemKV()
	s := NewStorage(nil, newMemKV(), dedup)

	ident := EmbedderIdentity{Type: "http", Model: "all-MiniLM-L6-v2", Dimensions: 384, MaxTextLen: 8000}
	key := DedupKey(ident, "hello world")

	if err := s.SaveDedup(ctx, key, []float32{1, 2, 3}, "acme.ops.a.b.c.1", ident.Model, ident.Dimensions); err != nil {
		t.Fatalf("SaveDedup: %v", err)
	}

	got, err := s.GetByContentHash(ctx, key)
	if err != nil {
		t.Fatalf("GetByContentHash: %v", err)
	}
	if got == nil {
		t.Fatal("dedup record not found at its own key")
	}
	if got.Model != ident.Model {
		t.Errorf("Model = %q, want %q", got.Model, ident.Model)
	}
	if got.Dimensions != ident.Dimensions {
		t.Errorf("Dimensions = %d, want %d", got.Dimensions, ident.Dimensions)
	}

	// A second entity sharing the content joins the record and keeps its identity.
	if err := s.SaveDedup(ctx, key, []float32{1, 2, 3}, "acme.ops.a.b.c.2", ident.Model, ident.Dimensions); err != nil {
		t.Fatalf("SaveDedup (second entity): %v", err)
	}
	got, err = s.GetByContentHash(ctx, key)
	if err != nil {
		t.Fatalf("GetByContentHash (second): %v", err)
	}
	if len(got.EntityIDs) != 2 {
		t.Fatalf("EntityIDs = %v, want 2 entries", got.EntityIDs)
	}
	if got.Model != ident.Model || got.Dimensions != ident.Dimensions {
		t.Errorf("identity lost on append: model=%q dims=%d", got.Model, got.Dimensions)
	}
}

// TestDedupIdentityChange_NeverServesStaleVector is the end-to-end statement of
// the defect at the Storage seam: write under one identity, read under another,
// and the stale vector must be unreachable.
func TestDedupIdentityChange_NeverServesStaleVector(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(nil, newMemKV(), newMemKV())

	const text = "an entity description that is identical under both configs"
	bm25 := EmbedderIdentity{Type: "bm25", Model: "bm25-384", Dimensions: 384, MaxTextLen: 4000}
	neural := EmbedderIdentity{Type: "http", Model: "all-MiniLM-L6-v2", Dimensions: 384, MaxTextLen: 8000}

	staleVector := []float32{9, 9, 9}
	if err := s.SaveDedup(ctx, DedupKey(bm25, text), staleVector, "acme.ops.a.b.c.1", bm25.Model, bm25.Dimensions); err != nil {
		t.Fatalf("SaveDedup under bm25: %v", err)
	}

	// Operator switches configs against the same NATS state. Same text, same
	// dimension count, so nothing else can catch this.
	got, err := s.GetByContentHash(ctx, DedupKey(neural, text))
	if err != nil {
		t.Fatalf("GetByContentHash under neural identity: %v", err)
	}
	if got != nil {
		t.Fatalf("stale bm25 vector %v served to the neural embedder", got.Vector)
	}
}
