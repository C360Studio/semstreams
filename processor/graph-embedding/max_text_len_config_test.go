package graphembedding

import (
	"encoding/json"
	"testing"
)

// TestMaxTextLenConfig_JSONRoundTrip is the operator-surface guard for the embedding
// text cap (#602): max_text_len must survive the production JSON path with no shadow
// struct, and it must reach the effective cap (maxSourceTextLen) that the worker is
// wired with — otherwise it is a phantom knob accepted, validated, and dropped
// (the Track 0 factory-overlay lesson).
func TestMaxTextLenConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"ports": {
			"inputs": [{"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}]
		},
		"embedder_type": "bm25",
		"max_text_len": 1234
	}`)

	// Decode exactly as the factory does (whole-Config unmarshal, no field overlay).
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode operator config: %v", err)
	}
	if cfg.MaxTextLen != 1234 {
		t.Fatalf("MaxTextLen = %d, want 1234 (operator value lost on decode)", cfg.MaxTextLen)
	}

	cfg.ApplyDefaults()
	if cfg.MaxTextLen != 1234 {
		t.Fatalf("MaxTextLen = %d after ApplyDefaults, want 1234 (explicit value overwritten)", cfg.MaxTextLen)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// The knob must reach the effective cap the worker is built with.
	c := &Component{config: cfg}
	if got := c.maxSourceTextLen(); got != 1234 {
		t.Fatalf("maxSourceTextLen() = %d, want the operator's 1234; the field is a phantom if it doesn't reach the cap", got)
	}

	// Re-encode and decode again: the field round-trips.
	out, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("re-encode config: %v", err)
	}
	var back Config
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("decode round-tripped config: %v", err)
	}
	if back.MaxTextLen != 1234 {
		t.Fatalf("MaxTextLen did not round-trip: %d -> %d", cfg.MaxTextLen, back.MaxTextLen)
	}
}

// TestMaxTextLen_DefaultsByEmbedderTypeWhenUnset proves an unset cap falls back to
// the per-embedder default (unchanged from the pre-config behaviour), and that an
// operator value overrides that default.
func TestMaxTextLen_DefaultsByEmbedderTypeWhenUnset(t *testing.T) {
	t.Parallel()

	bm := &Component{config: Config{EmbedderType: "bm25"}}
	if got := bm.maxSourceTextLen(); got != maxSourceTextLenBM25 {
		t.Fatalf("bm25 default cap = %d, want %d", got, maxSourceTextLenBM25)
	}

	neural := &Component{config: Config{EmbedderType: "http"}}
	if got := neural.maxSourceTextLen(); got != maxSourceTextLenNeural {
		t.Fatalf("neural default cap = %d, want %d", got, maxSourceTextLenNeural)
	}

	override := &Component{config: Config{EmbedderType: "bm25", MaxTextLen: 2000}}
	if got := override.maxSourceTextLen(); got != 2000 {
		t.Fatalf("operator cap = %d, want 2000 (override must win over the type default)", got)
	}
}
