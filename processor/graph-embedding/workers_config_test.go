package graphembedding

import (
	"encoding/json"
	"testing"
)

// TestWorkersConfig_JSONRoundTrip is the operator-surface guard: `workers` must
// survive the production JSON path with no shadow struct, or an operator who
// sets it gets the default and no error.
func TestWorkersConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"ports": {
			"inputs": [{"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}]
		},
		"embedder_type": "bm25",
		"batch_size": 4,
		"workers": 3
	}`)

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode operator config: %v", err)
	}
	if cfg.Workers != 3 {
		t.Fatalf("Workers = %d, want 3 (operator value lost on decode)", cfg.Workers)
	}

	// batch_size must be left exactly as the operator set it: worker count is no
	// longer derived from it (gh#620).
	if cfg.BatchSize != 4 {
		t.Fatalf("BatchSize = %d, want 4", cfg.BatchSize)
	}

	cfg.ApplyDefaults()
	if cfg.Workers != 3 {
		t.Fatalf("Workers = %d after ApplyDefaults, want 3 (explicit value overwritten)", cfg.Workers)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	out, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("re-encode config: %v", err)
	}
	var back Config
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("decode round-tripped config: %v", err)
	}
	if back.Workers != cfg.Workers {
		t.Fatalf("Workers did not round-trip: %d -> %d", cfg.Workers, back.Workers)
	}
	if back.BatchSize != cfg.BatchSize {
		t.Fatalf("BatchSize did not round-trip: %d -> %d", cfg.BatchSize, back.BatchSize)
	}
}

// TestWorkersConfig_SmallBatchSizeStillGetsWorkers guards gh#620.
//
// Worker count was derived as batch_size/10, so ANY batch_size under 10 produced
// zero workers via integer division: Start spawned no goroutines to drain the
// EMBEDDING_INDEX watcher and the component silently processed nothing while
// every health signal stayed green.
func TestWorkersConfig_SmallBatchSizeStillGetsWorkers(t *testing.T) {
	t.Parallel()

	for _, batchSize := range []int{1, 4, 9} {
		cfg := Config{EmbedderType: "bm25", BatchSize: batchSize}
		cfg.ApplyDefaults()
		if cfg.Workers < 1 {
			t.Fatalf("batch_size=%d yields Workers=%d; component would process nothing",
				batchSize, cfg.Workers)
		}
	}
}

// TestWorkersConfig_ValidateRejectsNegative — a negative worker count is a broken
// config, not a preference, so it is rejected rather than silently floored.
func TestWorkersConfig_ValidateRejectsNegative(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Workers = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a negative worker count")
	}
}
