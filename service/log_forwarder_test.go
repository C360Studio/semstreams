package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogForwarderService_ConfigParsing tests configuration parsing and validation
func TestLogForwarderService_ConfigParsing(t *testing.T) {
	tests := []struct {
		name         string
		rawConfig    json.RawMessage
		wantMinLevel string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "valid config - DEBUG level",
			rawConfig:    json.RawMessage(`{"min_level": "DEBUG"}`),
			wantMinLevel: "DEBUG",
			wantErr:      false,
		},
		{
			name:         "valid config - INFO level",
			rawConfig:    json.RawMessage(`{"min_level": "INFO"}`),
			wantMinLevel: "INFO",
			wantErr:      false,
		},
		{
			name:         "valid config - WARN level",
			rawConfig:    json.RawMessage(`{"min_level": "WARN"}`),
			wantMinLevel: "WARN",
			wantErr:      false,
		},
		{
			name:         "valid config - ERROR level",
			rawConfig:    json.RawMessage(`{"min_level": "ERROR"}`),
			wantMinLevel: "ERROR",
			wantErr:      false,
		},
		{
			name:         "default values - empty config",
			rawConfig:    json.RawMessage(`{}`),
			wantMinLevel: "INFO",
			wantErr:      false,
		},
		{
			name:         "default values - null config",
			rawConfig:    nil,
			wantMinLevel: "INFO",
			wantErr:      false,
		},
		{
			name:         "lowercase level converted to uppercase",
			rawConfig:    json.RawMessage(`{"min_level": "debug"}`),
			wantMinLevel: "DEBUG",
			wantErr:      false,
		},
		{
			name:        "invalid log level",
			rawConfig:   json.RawMessage(`{"min_level": "TRACE"}`),
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name:        "invalid log level - random string",
			rawConfig:   json.RawMessage(`{"min_level": "INVALID"}`),
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name:        "malformed JSON",
			rawConfig:   json.RawMessage(`{"min_level": `),
			wantErr:     true,
			errContains: "parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock dependencies - LogForwarder no longer needs NATS client
			deps := &Dependencies{
				NATSClient: &natsclient.Client{},
				Logger:     slog.Default(),
			}

			// Call constructor
			svc, err := NewLogForwarderService(tt.rawConfig, deps)

			// Verify error expectations
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, svc)

			// Verify service type
			logForwarder, ok := svc.(*LogForwarder)
			require.True(t, ok, "service should be *LogForwarder type")

			// Verify config values
			assert.Equal(t, tt.wantMinLevel, logForwarder.config.MinLevel)
		})
	}
}

// TestLogForwarderConfig_Validate tests configuration validation
func TestLogForwarderConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      LogForwarderConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid DEBUG level",
			config:  LogForwarderConfig{MinLevel: "DEBUG"},
			wantErr: false,
		},
		{
			name:    "valid INFO level",
			config:  LogForwarderConfig{MinLevel: "INFO"},
			wantErr: false,
		},
		{
			name:    "valid WARN level",
			config:  LogForwarderConfig{MinLevel: "WARN"},
			wantErr: false,
		},
		{
			name:    "valid ERROR level",
			config:  LogForwarderConfig{MinLevel: "ERROR"},
			wantErr: false,
		},
		{
			name:        "invalid level TRACE",
			config:      LogForwarderConfig{MinLevel: "TRACE"},
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name:        "invalid level empty",
			config:      LogForwarderConfig{MinLevel: ""},
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name: "valid with exclude_sources",
			config: LogForwarderConfig{
				MinLevel:       "INFO",
				ExcludeSources: []string{"flow-service.websocket", "metrics-forwarder"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestLogForwarder_ServiceLifecycle tests Start/Stop behavior
func TestLogForwarder_ServiceLifecycle(t *testing.T) {
	config := &LogForwarderConfig{
		MinLevel: "INFO",
	}

	lf, err := NewLogForwarder(config, WithLogger(slog.Default()))
	require.NoError(t, err)

	// Initially stopped
	assert.Equal(t, StatusStopped, lf.Status())

	// Start service
	ctx := context.Background()
	err = lf.Start(ctx)
	require.NoError(t, err)

	// Verify running
	assert.Equal(t, StatusRunning, lf.Status())

	// Stop service
	err = lf.Stop(context.Background())
	require.NoError(t, err)

	// Verify stopped
	assert.Equal(t, StatusStopped, lf.Status())
}

// TestLogForwarder_ServiceImplementsInterface verifies Service interface implementation
func TestLogForwarder_ServiceImplementsInterface(t *testing.T) {
	config := &LogForwarderConfig{
		MinLevel: "INFO",
	}

	lf, err := NewLogForwarder(config)
	require.NoError(t, err)

	// Verify Service interface implementation
	var _ Service = lf
}

// TestLogForwarder_Config tests the Config() accessor method
func TestLogForwarder_Config(t *testing.T) {
	config := &LogForwarderConfig{
		MinLevel:       "WARN",
		ExcludeSources: []string{"test-source", "another-source"},
	}

	lf, err := NewLogForwarder(config)
	require.NoError(t, err)

	// Verify Config() returns correct values
	cfg := lf.Config()
	assert.Equal(t, "WARN", cfg.MinLevel)
	assert.Equal(t, []string{"test-source", "another-source"}, cfg.ExcludeSources)
}

// TestLogForwarder_DefaultConfig tests default configuration values
func TestLogForwarder_DefaultConfig(t *testing.T) {
	// Create with nil config
	lf, err := NewLogForwarder(nil)
	require.NoError(t, err)

	// Verify defaults
	cfg := lf.Config()
	assert.Equal(t, "INFO", cfg.MinLevel)
	assert.Equal(t, []string{"flow-service.websocket"}, cfg.ExcludeSources)
}

// TestLogForwarderService_ExcludeSources tests that exclude_sources is properly parsed
func TestLogForwarderService_ExcludeSources(t *testing.T) {
	rawConfig := json.RawMessage(`{
		"min_level": "DEBUG",
		"exclude_sources": ["flow-service.websocket", "metrics-forwarder.internal"]
	}`)

	deps := &Dependencies{
		NATSClient: &natsclient.Client{},
		Logger:     slog.Default(),
	}

	svc, err := NewLogForwarderService(rawConfig, deps)
	require.NoError(t, err)

	lf, ok := svc.(*LogForwarder)
	require.True(t, ok)

	// Verify exclude_sources was parsed
	assert.Equal(t, []string{"flow-service.websocket", "metrics-forwarder.internal"}, lf.config.ExcludeSources)
}
