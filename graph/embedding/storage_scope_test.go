package embedding

import (
	"strings"
	"testing"
)

// TestFindSimilarFromCache_ScopeFilter guards the ADR-071 BLOCKING regression:
// the scope keep-predicate MUST be honored on the WARM cache path (the
// steady-state path) before cosine similarity — not only on the cold KV scan.
// A cache-path omission would make scope a silent no-op in warm production.
func TestFindSimilarFromCache_ScopeFilter(t *testing.T) {
	s := &Storage{
		vectorCache: map[string][]float32{
			"acme.web.python.svc.fn.parse":  {1, 0, 0},
			"acme.web.python.svc.fn.build":  {0, 1, 0},
			"acme.web.docs.site.doc.readme": {0, 0, 1},
			"acme.web.docs.site.doc.guide":  {1, 1, 0},
		},
		cacheReady: make(chan struct{}),
	}
	close(s.cacheReady) // mark the cache warm

	query := []float32{1, 1, 1}

	t.Run("scoped to docs returns only docs, before cosine", func(t *testing.T) {
		keep := func(id string) bool { return strings.HasPrefix(id, "acme.web.docs.") }
		got, ok := s.FindSimilarFromCache("", query, keep, 10)
		if !ok {
			t.Fatal("cache reported cold; expected warm")
		}
		if len(got) != 2 {
			t.Fatalf("got %d results, want 2 in-scope docs entities: %+v", len(got), got)
		}
		for _, r := range got {
			if !strings.HasPrefix(r.EntityID, "acme.web.docs.") {
				t.Errorf("out-of-scope entity returned: %s", r.EntityID)
			}
		}
	})

	t.Run("nil predicate keeps all", func(t *testing.T) {
		got, ok := s.FindSimilarFromCache("", query, nil, 10)
		if !ok || len(got) != 4 {
			t.Fatalf("unscoped got %d (ok=%v), want 4", len(got), ok)
		}
	})
}
