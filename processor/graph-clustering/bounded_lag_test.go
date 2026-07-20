package graphclustering

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_IndexLagTolerance_JSONRoundTrip proves the new operator-surface field
// survives a marshal/unmarshal through the production Config type — no shadow struct
// (operator-surface discipline). The absent case must decode to the strict default 0.
func TestConfig_IndexLagTolerance_JSONRoundTrip(t *testing.T) {
	original := Config{IndexLagTolerance: 250}
	data, err := json.Marshal(original)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"index_lag_tolerance":250`, "field must serialize under its json tag")

	var decoded Config
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, uint64(250), decoded.IndexLagTolerance, "tolerance must survive the round trip")

	// Absent field decodes to the strict, contract-preserving default.
	var fromEmpty Config
	require.NoError(t, json.Unmarshal([]byte(`{}`), &fromEmpty))
	assert.Equal(t, uint64(0), fromEmpty.IndexLagTolerance, "absent tolerance must default to 0 (exact Ready gate)")
}

// TestConfig_Validate_IndexLagTolerance covers the pathological-N guard: modest and
// boundary values pass; a value past the sane maximum is rejected so it cannot
// silently ungate the bootstrap/cutover defer (ADR-082).
func TestConfig_Validate_IndexLagTolerance(t *testing.T) {
	base := func(tol uint64) Config {
		c := Config{
			Ports: &component.PortConfig{
				Inputs:  []component.PortDefinition{{Name: "entity_watch", Type: "kv-watch", Subject: "ENTITY_STATES"}},
				Outputs: []component.PortDefinition{{Name: "communities", Type: "kv-write", Subject: "COMMUNITY_INDEX"}},
			},
			IndexLagTolerance: tol,
		}
		c.ApplyDefaults()
		return c
	}

	tests := []struct {
		name    string
		tol     uint64
		wantErr bool
	}{
		{"default zero is valid", 0, false},
		{"modest tolerance is valid", 250, false},
		{"exactly the maximum is valid", maxIndexLagTolerance, false},
		{"one past the maximum is rejected", maxIndexLagTolerance + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base(tt.tol)
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "index_lag_tolerance", "error must name the offending field")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestObserveDetectionLag proves clustering-under-lag is operator-visible, not
// Debug-only (#579 lesson): the gauge carries the lag of the last run and an
// INFO log fires with the value when the run proceeded on bounded-stale topology.
func TestObserveDetectionLag(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := &Component{
		logger:  logger,
		metrics: getMetrics(nil),
		config:  Config{IndexLagTolerance: 200},
	}

	// A run under bounded lag: gauge set to the lag AND an INFO log carrying it.
	c.observeDetectionLag(150)
	assert.Equal(t, float64(150), testutil.ToFloat64(c.metrics.indexLagAtDetection), "gauge must record the lag the run proceeded at")
	logged := buf.String()
	assert.Contains(t, logged, `"level":"INFO"`, "clustering-under-lag must not be confined to debug")
	assert.Contains(t, logged, `"index_lag":150`, "the log must carry the lag value")
	assert.Contains(t, logged, `"index_lag_tolerance":200`, "the log must carry the configured tolerance")

	// An exactly-caught-up run resets the gauge to 0 and emits no INFO log.
	buf.Reset()
	c.observeDetectionLag(0)
	assert.Equal(t, float64(0), testutil.ToFloat64(c.metrics.indexLagAtDetection), "gauge must reflect the latest run (0 = caught up)")
	assert.False(t, strings.Contains(buf.String(), `"level":"INFO"`), "a caught-up run must not emit the bounded-lag INFO log")
}
