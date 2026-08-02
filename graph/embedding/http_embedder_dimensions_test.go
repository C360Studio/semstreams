package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// newFixedWidthEmbedServer replies with vectors of exactly width dimensions.
func newFixedWidthEmbedServer(t *testing.T, width int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		type embObj struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		data := make([]embObj, len(req.Input))
		for i := range req.Input {
			data[i] = embObj{Object: "embedding", Index: i, Embedding: make([]float32, width)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": req.Model, "data": data})
	}))
}

// TestHTTPEmbedder_DimensionsIsRaceFree is the -race statement of the defect.
//
// HTTPEmbedder has no synchronization at all, and dimensions was mutated in place
// on the first API response. Before Track 0 that was invisible because Dimensions()
// had no callers outside the embedder. Track 0 added callers on goroutines other
// than the writer; since the hop-2 key move (#623) they are all in the hop-2 workers
// (N=5 by default), where the worker's embedderIdentity() reads Dimensions() to build
// the dedup key.
//
// The e2e semantic tier cannot surface this — TEI's default model is 384-dim, the
// same as the old placeholder, so the write never changed the value.
func TestHTTPEmbedder_DimensionsIsRaceFree(t *testing.T) {
	t.Parallel()

	srv := newFixedWidthEmbedServer(t, 768)
	defer srv.Close()

	emb, err := NewHTTPEmbedder(HTTPConfig{BaseURL: srv.URL, Model: "all-mpnet-base-v2"})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}

	done := make(chan struct{})
	var readers sync.WaitGroup

	// Stand in for the hop-1 watcher and the hop-2 worker pool reading Dimensions()
	// while the first response is being processed.
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = emb.Dimensions()
				}
			}
		}()
	}

	if _, err := emb.Generate(context.Background(), []string{"first call resolves dimensions"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	close(done)
	readers.Wait()

	if got := emb.Dimensions(); got != 768 {
		t.Fatalf("Dimensions() = %d after a 768-dim response, want 768", got)
	}
}

// TestHTTPEmbedder_DimensionsUnresolvedUntilFirstResponse pins the removal of the
// `== 384` sentinel-overwrite.
//
// The placeholder could not distinguish "not yet detected" from "genuinely 384",
// and it split the dedup keyspace: for any non-384 model (all-mpnet-base-v2 = 768,
// bge-large = 1024) every entity queued before the first API response keyed on
// Dimensions:384 and every one after keyed on 768 — two disjoint keyspaces for one
// embedder, with the split point decided by race timing.
//
// 0 now means "unresolved" and is reported honestly, so callers can refuse to key
// on it rather than inventing a width.
func TestHTTPEmbedder_DimensionsUnresolvedUntilFirstResponse(t *testing.T) {
	t.Parallel()

	srv := newFixedWidthEmbedServer(t, 768)
	defer srv.Close()

	emb, err := NewHTTPEmbedder(HTTPConfig{BaseURL: srv.URL, Model: "all-mpnet-base-v2"})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}

	if got := emb.Dimensions(); got != 0 {
		t.Fatalf("Dimensions() = %d before any response, want 0 (unresolved); "+
			"a placeholder width splits the dedup keyspace at the first API reply", got)
	}

	if _, err := emb.Generate(context.Background(), []string{"resolve me"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if got := emb.Dimensions(); got != 768 {
		t.Fatalf("Dimensions() = %d after a 768-dim response, want 768", got)
	}
}

// TestHTTPEmbedder_DimensionsResolveOnceAndStick checks the resolution latches.
// A genuinely 384-dim model must resolve to 384 and stay there — the old sentinel
// re-tested `== 384` on every response and so could never tell that case apart
// from an unresolved one.
func TestHTTPEmbedder_DimensionsResolveOnceAndStick(t *testing.T) {
	t.Parallel()

	srv := newFixedWidthEmbedServer(t, 384)
	defer srv.Close()

	emb, err := NewHTTPEmbedder(HTTPConfig{BaseURL: srv.URL, Model: "all-MiniLM-L6-v2"})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := emb.Generate(context.Background(), []string{"call"}); err != nil {
			t.Fatalf("Generate #%d: %v", i, err)
		}
		if got := emb.Dimensions(); got != 384 {
			t.Fatalf("Dimensions() = %d after call #%d, want a stable 384", got, i)
		}
	}
}

// TestDedupKey_EmptyWhenIdentityUnresolved is the containment for the keyspace
// split: an embedder that cannot yet state its vector width must produce no dedup
// key at all.
//
// Empty is the package's established "dedup disabled for this record" signal —
// queueEntityForEmbedding stores it as the pending record's ContentHash and
// getOrGenerateEmbedding already documents and honours it by generating
// unconditionally. So the unresolved window costs redundant embeds, once, instead
// of permanently partitioning the bucket. Dropping Dimensions from the key
// instead would be worse: keys would then collide across models of different
// width, which is the very thing gh#612 folded identity in to prevent.
func TestDedupKey_EmptyWhenIdentityUnresolved(t *testing.T) {
	t.Parallel()

	unresolved := EmbedderIdentity{Type: "http", Model: "all-mpnet-base-v2", Dimensions: 0, MaxTextLen: 8000}
	if got := DedupKey(unresolved, "some entity text"); got != "" {
		t.Fatalf("DedupKey on an unresolved identity = %q, want \"\" (dedup disabled); "+
			"keying on a placeholder width is what splits the keyspace", got)
	}

	resolved := EmbedderIdentity{Type: "http", Model: "all-mpnet-base-v2", Dimensions: 768, MaxTextLen: 8000}
	if got := DedupKey(resolved, "some entity text"); got == "" {
		t.Fatal("DedupKey on a resolved identity must produce a key")
	}
}

// TestWorkerSkipsDedupWhenEmbedderUnresolved proves an embedder that has not yet
// resolved its vector width takes NO part in dedup — read or write.
//
// Since the hop-2 key move (#623) the worker derives the dedup key itself over the
// bytes it embeds, and DedupKey withholds a key from a zero-width identity. So an
// unresolved embedder yields an empty key: it never consults the durable bucket (no
// stale hit) and never stamps it with a width-0 record that could never match again.
// A pre-existing resolved record must be left untouched.
func TestWorkerSkipsDedupWhenEmbedderUnresolved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dedup := newMemKV()
	s := NewStorage(nil, newMemKV(), dedup)

	const staleKey = "a-key-from-a-previous-process"
	if err := s.SaveDedup(ctx, staleKey, []float32{9, 9, 9}, "acme.ops.a.b.c.1", "bm25-384", 384); err != nil {
		t.Fatalf("seed dedup: %v", err)
	}

	var generateCalls int
	unresolved := &stubEmbedder{
		model:      "all-mpnet-base-v2",
		dimensions: 0, // first API response has not landed yet
		generate: func([]string) ([][]float32, error) {
			generateCalls++
			return [][]float32{{1, 2, 3}}, nil
		},
	}

	w := NewWorker(s, unresolved, nil, discardLogger()).WithMaxSourceTextLen(8000).WithEmbedderType("http")
	// Start needs a real index bucket to watch; this exercises the hop-2 dedup
	// decision directly, so only the context it reads from is supplied.
	w.ctx = ctx

	// The worker derives the key itself now; an unresolved width yields "".
	key := DedupKey(w.embedderIdentity(), "text")
	if key != "" {
		t.Fatalf("an unresolved embedder must yield an empty dedup key, got %q", key)
	}

	vector, _, _, _, _, err := w.getOrGenerateEmbedding("acme.ops.a.b.c.2", "text", key, 1)
	if err != nil {
		t.Fatalf("getOrGenerateEmbedding: %v", err)
	}
	if generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1; an unresolved embedder must not consume a dedup hit", generateCalls)
	}
	if len(vector) != 3 || vector[0] != 1 {
		t.Fatalf("vector = %v, want the freshly generated [1 2 3], not the stale record", vector)
	}

	// The pre-existing resolved record must be untouched — an unresolved worker
	// neither reads nor overwrites the durable bucket.
	rec, err := s.GetByContentHash(ctx, staleKey)
	if err != nil {
		t.Fatalf("GetByContentHash: %v", err)
	}
	if rec == nil || rec.Dimensions != 384 {
		t.Fatalf("stale record altered by an unresolved embedder: %+v", rec)
	}
}
