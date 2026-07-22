package graphembedding

import (
	"encoding/json"
	"testing"
)

// TestConfig_Validate_RejectsHugeMaxTextLen is #628 FIX 2 on the operator surface: a
// max_text_len above the upper bound is a broken config (it overflows the offloaded
// lane's read budget and permits an unbounded allocation), so Validate must reject it
// rather than accept it the way it accepts any positive value.
//
// Fails without the fix because Validate only rejected NEGATIVE values, so a huge
// positive value passed.
func TestConfig_Validate_RejectsHugeMaxTextLen(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.MaxTextLen = 2_000_000 // above the 1_000_000 ceiling
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a max_text_len above the ceiling; an unbounded cap overflows the offloaded read and is a broken config (#628 FIX 2)")
	}
}

// TestConfig_Validate_AcceptsMaxTextLenAtCeiling guards the boundary: exactly the
// ceiling is still a valid config (the rejection is strictly ABOVE it).
func TestConfig_Validate_AcceptsMaxTextLenAtCeiling(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.MaxTextLen = 1_000_000 // exactly the ceiling
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected the ceiling value 1_000_000: %v", err)
	}
}

// TestMaxTextLenConfig_OverBoundJSONRejected drives the whole production JSON path: an
// over-bound max_text_len decoded exactly as the factory decodes it must fail Validate,
// so the bound cannot be bypassed by supplying config as JSON (#628 FIX 2).
func TestMaxTextLenConfig_OverBoundJSONRejected(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"ports": {
			"inputs":  [{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],
			"outputs": [{"name":"embeddings","type":"kv-write","subject":"EMBEDDINGS_CACHE"}]
		},
		"embedder_type": "bm25",
		"max_text_len": 5000000
	}`)

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode operator config: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted an over-bound max_text_len supplied via the production JSON path (#628 FIX 2)")
	}
}
