package service

import (
	"testing"

	"github.com/c360/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageLogger_ConfigSchema(t *testing.T) {
	// Create MessageLogger for testing
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	schema := ml.ConfigSchema()

	// Verify all properties are present
	assert.Contains(t, schema.ConfigSchema.Properties, "enabled")
	assert.Contains(t, schema.ConfigSchema.Properties, "monitor_subjects")
	assert.Contains(t, schema.ConfigSchema.Properties, "max_entries")
	assert.Contains(t, schema.ConfigSchema.Properties, "output_to_stdout")

	// Verify enabled property
	enabled := schema.ConfigSchema.Properties["enabled"]
	assert.Equal(t, "bool", enabled.Type)
	assert.Equal(t, "Enable or disable message logging", enabled.Description)
	assert.Equal(t, false, enabled.Default)

	// Verify monitor_subjects property
	monitorSubjects := schema.ConfigSchema.Properties["monitor_subjects"]
	assert.Equal(t, "array", monitorSubjects.Type)
	assert.Equal(t, "NATS subjects to monitor for messages", monitorSubjects.Description)
	expectedDefault := []string{"process.>", "input.>", "events.>"}
	assert.Equal(t, expectedDefault, monitorSubjects.Default)

	// Verify max_entries property
	maxEntries := schema.ConfigSchema.Properties["max_entries"]
	assert.Equal(t, "integer", maxEntries.Type)
	assert.Equal(t, "Maximum entries to keep in memory", maxEntries.Description)
	assert.Equal(t, 10000, maxEntries.Default)
	assert.NotNil(t, maxEntries.Minimum)
	assert.Equal(t, 1000, *maxEntries.Minimum)
	assert.NotNil(t, maxEntries.Maximum)
	assert.Equal(t, 100000, *maxEntries.Maximum)

	// Verify output_to_stdout property
	outputToStdout := schema.ConfigSchema.Properties["output_to_stdout"]
	assert.Equal(t, "bool", outputToStdout.Type)
	assert.Equal(t, "Whether to output messages to stdout", outputToStdout.Description)
	assert.Equal(t, false, outputToStdout.Default)

	// Verify no required fields
	assert.Empty(t, schema.Required)
}

func TestMessageLogger_ValidateConfigUpdate_ValidChanges(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	tests := []struct {
		name    string
		changes map[string]any
	}{
		{
			name: "enable logging",
			changes: map[string]any{
				"enabled": true,
			},
		},
		{
			name: "disable logging",
			changes: map[string]any{
				"enabled": false,
			},
		},
		{
			name: "change monitor subjects",
			changes: map[string]any{
				"monitor_subjects": []any{"test.>", "debug.>"},
			},
		},
		{
			name: "change max entries (int)",
			changes: map[string]any{
				"max_entries": 5000,
			},
		},
		{
			name: "change max entries (float64 - JSON numbers)",
			changes: map[string]any{
				"max_entries": 5000.0,
			},
		},
		{
			name: "enable stdout output",
			changes: map[string]any{
				"output_to_stdout": true,
			},
		},
		{
			name: "multiple properties",
			changes: map[string]any{
				"enabled":          true,
				"max_entries":      15000,
				"output_to_stdout": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ml.ValidateConfigUpdate(tt.changes)
			assert.NoError(t, err)
		})
	}
}

func TestMessageLogger_ValidateConfigUpdate_InvalidChanges(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	tests := []struct {
		name    string
		changes map[string]any
		wantErr string
	}{
		{
			name: "invalid enabled type",
			changes: map[string]any{
				"enabled": "true", // should be bool
			},
			wantErr: "enabled must be boolean, got string",
		},
		{
			name: "invalid monitor_subjects type",
			changes: map[string]any{
				"monitor_subjects": "test.>", // should be array
			},
			wantErr: "monitor_subjects must be array, got string",
		},
		{
			name: "empty monitor_subjects array",
			changes: map[string]any{
				"monitor_subjects": []any{}, // should not be empty
			},
			wantErr: "monitor_subjects cannot be empty",
		},
		{
			name: "invalid monitor_subjects element type",
			changes: map[string]any{
				"monitor_subjects": []any{123}, // should be strings
			},
			wantErr: "monitor_subjects[0] must be string, got int",
		},
		{
			name: "invalid max_entries type",
			changes: map[string]any{
				"max_entries": "5000", // should be number
			},
			wantErr: "max_entries must be number, got string",
		},
		{
			name: "max_entries too small",
			changes: map[string]any{
				"max_entries": 500, // below minimum of 1000
			},
			wantErr: "max_entries must be between 1000 and 100000, got 500",
		},
		{
			name: "max_entries too large",
			changes: map[string]any{
				"max_entries": 200000, // above maximum of 100000
			},
			wantErr: "max_entries must be between 1000 and 100000, got 200000",
		},
		{
			name: "invalid output_to_stdout type",
			changes: map[string]any{
				"output_to_stdout": "false", // should be bool
			},
			wantErr: "output_to_stdout must be boolean, got string",
		},
		{
			name: "unknown property",
			changes: map[string]any{
				"unknown_field": true,
			},
			wantErr: "unknown configuration property: unknown_field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ml.ValidateConfigUpdate(tt.changes)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMessageLogger_ApplyConfigUpdate_EnableDisable(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Initially disabled
	assert.False(t, ml.running)

	// Enable logging (without starting the service)
	changes := map[string]any{
		"enabled": true,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	// enabled state is managed by Manager, not directly by ApplyConfigUpdate
	// ml.running would only be true after Start() is called
	assert.False(t, ml.running)

	// Disable logging
	changes = map[string]any{
		"enabled": false,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	assert.False(t, ml.running) // Still false until Start() is called
}

func TestMessageLogger_ApplyConfigUpdate_MonitorSubjects(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Change monitor subjects
	newSubjects := []any{"test.>", "debug.>"}
	changes := map[string]any{
		"monitor_subjects": newSubjects,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)

	expectedSubjects := []string{"test.>", "debug.>"}
	assert.Equal(t, expectedSubjects, ml.config.MonitorSubjects)
}

func TestMessageLogger_ApplyConfigUpdate_MaxEntries(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Change max entries (int)
	changes := map[string]any{
		"max_entries": 5000,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	assert.Equal(t, 5000, ml.config.MaxEntries)
	assert.Equal(t, 5000, len(ml.entries)) // Buffer should be resized

	// Change max entries (float64 - JSON numbers)
	changes = map[string]any{
		"max_entries": 8000.0,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	assert.Equal(t, 8000, ml.config.MaxEntries)
	assert.Equal(t, 8000, len(ml.entries)) // Buffer should be resized
}

func TestMessageLogger_ApplyConfigUpdate_OutputToStdout(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Enable stdout output
	changes := map[string]any{
		"output_to_stdout": true,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	assert.True(t, ml.config.OutputToStdout)

	// Disable stdout output
	changes = map[string]any{
		"output_to_stdout": false,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)
	assert.False(t, ml.config.OutputToStdout)
}

func TestMessageLogger_ApplyConfigUpdate_MultipleProperties(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Apply multiple changes at once
	changes := map[string]any{
		"enabled":          true,
		"max_entries":      7500,
		"output_to_stdout": true,
		"monitor_subjects": []any{"new.>", "test.>"},
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)

	// Verify all changes applied (enabled is managed by Manager)
	assert.False(t, ml.running) // Still false until Start() is called
	assert.Equal(t, 7500, ml.config.MaxEntries)
	assert.True(t, ml.config.OutputToStdout)
	assert.Equal(t, []string{"new.>", "test.>"}, ml.config.MonitorSubjects)
	assert.Equal(t, 7500, len(ml.entries)) // Buffer resized
}

func TestMessageLogger_GetRuntimeConfig(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Test initial state
	config := ml.GetRuntimeConfig()
	expected := map[string]any{
		"enabled":          true, // GetRuntimeConfig always returns true
		"monitor_subjects": []string{"process.>", "input.>", "events.>"},
		"max_entries":      10000,
		"output_to_stdout": false,
	}
	assert.Equal(t, expected, config)

	// Change some values and test again
	changes := map[string]any{
		"enabled":          true,
		"max_entries":      5000,
		"output_to_stdout": true,
	}
	err = ml.ApplyConfigUpdate(changes)
	require.NoError(t, err)

	config = ml.GetRuntimeConfig()
	expected = map[string]any{
		"enabled":          true,
		"monitor_subjects": []string{"process.>", "input.>", "events.>"},
		"max_entries":      5000,
		"output_to_stdout": true,
	}
	assert.Equal(t, expected, config)
}

func TestMessageLogger_RuntimeConfigurable_Interface(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Test that MessageLogger implements RuntimeConfigurable interface
	var _ RuntimeConfigurable = ml

	// Test that MessageLogger implements Configurable interface
	var _ Configurable = ml
}

func TestMessageLogger_ThreadSafety(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Test concurrent config updates
	done := make(chan struct{})
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()

			// Each goroutine makes different config changes
			changes := map[string]any{
				"enabled":          id%2 == 0,
				"output_to_stdout": id%3 == 0,
				"max_entries":      1000 + (id * 1000),
			}

			// Apply changes (should not race)
			err := ml.ApplyConfigUpdate(changes)
			assert.NoError(t, err)

			// Read config (should not race)
			config := ml.GetRuntimeConfig()
			assert.NotNil(t, config)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// Helper function to create test MessageLogger
func createTestMessageLogger() (*MessageLogger, error) {
	// For testing we'll create a NATS client without actual connection
	// This will work for ConfigSchema, Validation, and ApplyConfigUpdate tests
	// The Start method won't be called in most tests
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		// If we can't create a client, create a minimal one just for testing
		// Most tests don't need actual NATS connectivity
		natsClient = &natsclient.Client{}
	}

	// Create default logger config
	loggerConfig := &MessageLoggerConfig{
		MonitorSubjects: []string{"process.>", "input.>", "events.>"},
		MaxEntries:      10000,
		OutputToStdout:  false,
		LogLevel:        "INFO",
	}

	return NewMessageLogger(loggerConfig, natsClient)
}
