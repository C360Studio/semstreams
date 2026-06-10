package inference

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_StrictJSONBinding_NoPhantomKeys guards against the phantom-key
// class fixed in ADR-054 Move 1. A JSON key that matches no struct field is
// silently dropped by encoding/json, so an operator's knob never binds. Two
// such keys shipped in the checked-in configs:
//
//   - semantic_gap.similarity_threshold (real field: min_semantic_similarity)
//   - core_anomaly.min_core_level        (real field: min_core_for_hub_analysis)
//
// Both were masked because the dropped value happened to equal the struct
// default. A strict decode (DisallowUnknownFields) makes any such drift fail
// loudly. See discipline memory feedback_polymorphic_config_needs_json_roundtrip_test.
func TestConfig_StrictJSONBinding_NoPhantomKeys(t *testing.T) {
	t.Run("correct keys bind under strict decode", func(t *testing.T) {
		const j = `{
		  "enabled": true,
		  "core_anomaly": {"enabled": true, "min_core_for_hub_analysis": 2},
		  "semantic_gap": {"enabled": true, "min_semantic_similarity": 0.42, "min_structural_distance": 3}
		}`
		var cfg Config
		dec := json.NewDecoder(bytes.NewReader([]byte(j)))
		dec.DisallowUnknownFields()
		require.NoError(t, dec.Decode(&cfg))
		assert.Equal(t, 2, cfg.CoreAnomaly.MinCoreForHubAnalysis,
			"min_core_for_hub_analysis must bind")
		assert.InDelta(t, 0.42, cfg.SemanticGap.MinSemanticSimilarity, 1e-9,
			"min_semantic_similarity must bind")
	})

	t.Run("legacy phantom keys are rejected by strict decode", func(t *testing.T) {
		phantom := []struct {
			name string
			json string
		}{
			{"semantic_gap.similarity_threshold", `{"semantic_gap": {"similarity_threshold": 0.7}}`},
			{"core_anomaly.min_core_level", `{"core_anomaly": {"min_core_level": 2}}`},
		}
		for _, p := range phantom {
			t.Run(p.name, func(t *testing.T) {
				var cfg Config
				dec := json.NewDecoder(bytes.NewReader([]byte(p.json)))
				dec.DisallowUnknownFields()
				err := dec.Decode(&cfg)
				require.Error(t, err,
					"phantom key %s must not silently bind into inference.Config", p.name)
			})
		}
	})
}

// TestShippedConfigs_AnomalyConfigStrictBinds is the file-level guard for the
// same drift class: every anomaly_config block in a shipped config under
// configs/ MUST strict-decode into inference.Config. This catches future drift
// in the real files, not just the struct contract. Had it existed, it would
// have failed on both similarity_threshold and min_core_level the day they
// landed.
func TestShippedConfigs_AnomalyConfigStrictBinds(t *testing.T) {
	configsDir := filepath.Join(repoRootFromInferenceTest(t), "configs")
	files, err := filepath.Glob(filepath.Join(configsDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "expected shipped configs under %s", configsDir)

	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // test reads checked-in configs by glob
		require.NoError(t, err, "read %s", f)

		var top any
		require.NoError(t, json.Unmarshal(raw, &top), "parse %s", f)

		forEachJSONValueWithKey(top, "anomaly_config", func(v any) {
			checked++
			blob, err := json.Marshal(v)
			require.NoError(t, err)

			dec := json.NewDecoder(bytes.NewReader(blob))
			dec.DisallowUnknownFields()
			var cfg Config
			if err := dec.Decode(&cfg); err != nil {
				t.Errorf("%s: anomaly_config has a phantom key that does not bind to "+
					"inference.Config (silently dropped at runtime): %v", filepath.Base(f), err)
			}
		})
	}

	require.Positive(t, checked,
		"expected at least one anomaly_config block across shipped configs (guard would be vacuous otherwise)")
}

// repoRootFromInferenceTest ascends from the test working directory to the
// module root (the directory containing go.mod), so the file-level guard works
// regardless of where `go test` is invoked.
func repoRootFromInferenceTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "module root (go.mod) not found above %s", dir)
		dir = parent
	}
}

// forEachJSONValueWithKey invokes fn for every value stored under the given key
// anywhere in a decoded JSON tree (objects and arrays, recursively).
func forEachJSONValueWithKey(node any, key string, fn func(any)) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if k == key {
				fn(v)
			}
			forEachJSONValueWithKey(v, key, fn)
		}
	case []any:
		for _, v := range n {
			forEachJSONValueWithKey(v, key, fn)
		}
	}
}

// TestRejectUnknownKeys covers the runtime guard that makes operator configs
// (not just checked-in ones) fail loudly on phantom anomaly_config keys.
func TestRejectUnknownKeys(t *testing.T) {
	t.Run("empty input is a no-op", func(t *testing.T) {
		require.NoError(t, RejectUnknownKeys(nil))
		require.NoError(t, RejectUnknownKeys(json.RawMessage("")))
	})

	t.Run("clean anomaly_config passes", func(t *testing.T) {
		raw := json.RawMessage(`{"enabled":true,` +
			`"core_anomaly":{"enabled":true,"min_core_for_hub_analysis":2},` +
			`"semantic_gap":{"enabled":true,"min_semantic_similarity":0.7,"min_structural_distance":3}}`)
		require.NoError(t, RejectUnknownKeys(raw))
	})

	t.Run("phantom keys are rejected with an ADR-054 hint", func(t *testing.T) {
		for _, raw := range []string{
			`{"semantic_gap":{"similarity_threshold":0.7}}`,
			`{"core_anomaly":{"min_core_level":2}}`,
		} {
			err := RejectUnknownKeys(json.RawMessage(raw))
			require.Error(t, err, "raw=%s", raw)
			assert.Contains(t, err.Error(), "ADR-054")
		}
	})
}
