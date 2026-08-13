package logforwarderpolicy

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOwnsDefaultsNormalizationValidationAndSafetyExclusion(t *testing.T) {
	tests := []struct {
		name        string
		raw         json.RawMessage
		wantLevel   slog.Level
		wantExclude []string
		wantErr     string
	}{
		{name: "empty defaults to info", raw: json.RawMessage(`{}`), wantLevel: slog.LevelInfo,
			wantExclude: []string{"flow-service.websocket"}},
		{name: "normalizes and deduplicates", raw: json.RawMessage(`{
			"min_level":"warn",
			"exclude_sources":["metrics-forwarder", "flow-service.websocket", "metrics-forwarder"]
		}`), wantLevel: slog.LevelWarn,
			wantExclude: []string{"flow-service.websocket", "metrics-forwarder"}},
		{name: "debug", raw: json.RawMessage(`{"min_level":"DEBUG"}`), wantLevel: slog.LevelDebug,
			wantExclude: []string{"flow-service.websocket"}},
		{name: "error", raw: json.RawMessage(`{"min_level":"ERROR"}`), wantLevel: slog.LevelError,
			wantExclude: []string{"flow-service.websocket"}},
		{name: "rejects unknown fields", raw: json.RawMessage(`{"unexpected":true}`), wantErr: "unknown field"},
		{name: "rejects malformed JSON", raw: json.RawMessage(`{"min_level":`), wantErr: "decode"},
		{name: "rejects invalid level", raw: json.RawMessage(`{"min_level":"TRACE"}`), wantErr: "invalid log level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.raw)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLevel, got.MinLevel)
			assert.Equal(t, tt.wantExclude, got.ExcludeSources)
		})
	}
}

func TestValidateFieldsPreservesPublicConfigValidationSemantics(t *testing.T) {
	require.NoError(t, ValidateFields("WARN", []string{"component"}))
	require.ErrorContains(t, ValidateFields("", nil), "invalid log level")
	require.ErrorContains(t, ValidateFields("trace", nil), "invalid log level")
}
