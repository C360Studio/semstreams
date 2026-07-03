package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newCapturingEmbedServer returns an httptest server that records the `input` texts
// of the last embeddings request and replies with one unit vector per input.
func newCapturingEmbedServer(t *testing.T, captured *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode embed request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		*captured = req.Input

		type embObj struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		data := make([]embObj, len(req.Input))
		for i := range req.Input {
			data[i] = embObj{Object: "embedding", Index: i, Embedding: []float32{1, 0, 0}}
		}
		resp := map[string]any{"object": "list", "model": req.Model, "data": data}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// TestHTTPEmbedder_GenerateQuery_PrependsPrefix is the gh#438 guard: the query side
// must carry the model's query instruction prefix, the document side must not.
func TestHTTPEmbedder_GenerateQuery_PrependsPrefix(t *testing.T) {
	var captured []string
	srv := newCapturingEmbedServer(t, &captured)
	defer srv.Close()

	const prefix = "Represent this sentence for searching relevant passages: "
	emb, err := NewHTTPEmbedder(HTTPConfig{BaseURL: srv.URL, Model: "arctic-embed-s", QueryPrefix: prefix})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}

	// Query side → prefixed.
	if _, err := emb.GenerateQuery(context.Background(), []string{"retry with backoff"}); err != nil {
		t.Fatalf("GenerateQuery: %v", err)
	}
	if len(captured) != 1 || captured[0] != prefix+"retry with backoff" {
		t.Fatalf("query input = %q, want prefixed %q", captured, prefix+"retry with backoff")
	}

	// Document side → raw (never prefixed).
	if _, err := emb.Generate(context.Background(), []string{"retry with backoff"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(captured) != 1 || captured[0] != "retry with backoff" {
		t.Fatalf("document input = %q, want unprefixed", captured)
	}
}

// TestHTTPEmbedder_GenerateQuery_NoPrefixConfigured: with no prefix, the query side
// is identical to the document side (symmetric use / non-asymmetric model).
func TestHTTPEmbedder_GenerateQuery_NoPrefixConfigured(t *testing.T) {
	var captured []string
	srv := newCapturingEmbedServer(t, &captured)
	defer srv.Close()

	emb, err := NewHTTPEmbedder(HTTPConfig{BaseURL: srv.URL, Model: "all-MiniLM-L6-v2"})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}
	if _, err := emb.GenerateQuery(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("GenerateQuery: %v", err)
	}
	if len(captured) != 1 || captured[0] != "hello" {
		t.Fatalf("no-prefix query input = %q, want unprefixed", captured)
	}
}
