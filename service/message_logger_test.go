package service

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
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

// TestShouldSample tests the sampling logic
func TestShouldSample(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		callCount  int
		wantSample int // Expected number of samples
	}{
		{
			name:       "sample_rate_0_logs_all",
			sampleRate: 0,
			callCount:  10,
			wantSample: 10,
		},
		{
			name:       "sample_rate_1_logs_all",
			sampleRate: 1,
			callCount:  10,
			wantSample: 10,
		},
		{
			name:       "sample_rate_2_logs_half",
			sampleRate: 2,
			callCount:  10,
			wantSample: 5,
		},
		{
			name:       "sample_rate_10_logs_tenth",
			sampleRate: 10,
			callCount:  100,
			wantSample: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ml := &MessageLogger{
				sampleRate: tt.sampleRate,
			}

			sampled := 0
			for i := 0; i < tt.callCount; i++ {
				if ml.shouldSample() {
					sampled++
				}
			}

			assert.Equal(t, tt.wantSample, sampled, "unexpected sample count")
		})
	}
}

// TestContainsWildcard tests the wildcard detection helper
func TestContainsWildcard(t *testing.T) {
	tests := []struct {
		name     string
		subjects []string
		want     bool
	}{
		{
			name:     "empty_list",
			subjects: []string{},
			want:     false,
		},
		{
			name:     "no_wildcard",
			subjects: []string{"raw.udp.messages", "processed.>"},
			want:     false,
		},
		{
			name:     "only_wildcard",
			subjects: []string{"*"},
			want:     true,
		},
		{
			name:     "wildcard_with_others",
			subjects: []string{"*", "debug.>"},
			want:     true,
		},
		{
			name:     "nats_wildcard_not_auto_discover",
			subjects: []string{"raw.>", "*.messages"},
			want:     false, // NATS wildcards are not the same as "*" auto-discover
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsWildcard(tt.subjects)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUniqueStringsPreservesFirstOccurrence(t *testing.T) {
	t.Parallel()

	got := uniqueStrings([]string{"graph.mutation.>", "raw.>", "graph.mutation.>", "raw.>"})
	require.Equal(t, []string{"graph.mutation.>", "raw.>"}, got)
}

// TestDiscoverSubjectsFromComponents tests subject discovery from component configs
func TestDiscoverSubjectsFromComponents(t *testing.T) {
	t.Run("empty_components", func(t *testing.T) {
		subjects, metadata := discoverSubjectsFromComponents(nil)
		assert.Empty(t, subjects)
		assert.Empty(t, metadata)
	})

	t.Run("discovers_subjects_from_ports", func(t *testing.T) {
		components := map[string]json.RawMessage{
			"udp": json.RawMessage(`{
				"ports": {
					"inputs": [],
					"outputs": [
						{"name": "udp_out", "config": {"kind": "jetstream", "subjects": ["raw.udp.messages"]}}
					]
				}
			}`),
			"json_generic": json.RawMessage(`{
				"ports": {
					"inputs": [
						{"name": "generic_in", "config": {"kind": "jetstream", "stream_name": "RAW", "subjects": ["raw.udp.messages"]}}
					],
					"outputs": [
						{"name": "generic_out", "config": {"kind": "jetstream", "subjects": ["generic.messages"], "interface": {"type": "core.json.v1"}}}
					]
				}
			}`),
		}

		subjects, metadata := discoverSubjectsFromComponents(components)

		// Should discover unique subjects
		assert.Contains(t, subjects, "raw.udp.messages")
		assert.Contains(t, subjects, "generic.messages")

		// Check metadata for raw.udp.messages (could be from either component due to map iteration order)
		rawMeta, ok := metadata["raw.udp.messages"]
		assert.True(t, ok, "should have metadata for raw.udp.messages")
		assert.Equal(t, "jetstream", rawMeta.PortType)

		// Check metadata for generic.messages
		genericMeta, ok := metadata["generic.messages"]
		assert.True(t, ok, "should have metadata for generic.messages")
		assert.Equal(t, "json_generic", genericMeta.Component)
		assert.Equal(t, "generic_out", genericMeta.PortName)
		assert.Equal(t, "jetstream", genericMeta.PortType)
		assert.Equal(t, "core.json.v1", genericMeta.Interface)
	})

	t.Run("skips_invalid_json", func(t *testing.T) {
		components := map[string]json.RawMessage{
			"invalid": json.RawMessage(`{not valid json`),
			"valid": json.RawMessage(`{
				"ports": {
					"outputs": [{"name": "out", "config": {"kind": "nats", "subject": "valid.subject"}}]
				}
			}`),
		}

		subjects, metadata := discoverSubjectsFromComponents(components)

		// Should still discover from valid component
		assert.Len(t, subjects, 1)
		assert.Contains(t, subjects, "valid.subject")
		assert.Len(t, metadata, 1)
	})

	t.Run("skips_ports_without_NATS_subjects", func(t *testing.T) {
		components := map[string]json.RawMessage{
			"test": json.RawMessage(`{
				"ports": {
					"inputs": [{"name": "in", "config": {"kind": "network", "protocol": "udp", "port": 14550}}],
					"outputs": [{"name": "out", "config": {"kind": "nats", "subject": "real.subject"}}]
				}
			}`),
		}

		subjects, metadata := discoverSubjectsFromComponents(components)

		// Should only have the non-empty subject
		assert.Len(t, subjects, 1)
		assert.Contains(t, subjects, "real.subject")
		assert.Len(t, metadata, 1)
	})
}

// TestMessageLoggerConfig_SampleRate tests sample rate config field
func TestMessageLoggerConfig_SampleRate(t *testing.T) {
	t.Run("default_sample_rate", func(t *testing.T) {
		cfg := DefaultMessageLoggerConfig()
		assert.Equal(t, 1, cfg.SampleRate, "default sample rate should be 1 (log all)")
	})

	t.Run("sample_rate_in_config", func(t *testing.T) {
		natsClient := &natsclient.Client{}
		loggerConfig := &MessageLoggerConfig{
			MonitorSubjects: []string{"test.>"},
			MaxEntries:      1000,
			SampleRate:      5,
		}

		ml, err := NewMessageLogger(loggerConfig, natsClient)
		require.NoError(t, err)
		assert.Equal(t, 5, ml.sampleRate)
	})

	t.Run("zero_sample_rate_defaults_to_1", func(t *testing.T) {
		natsClient := &natsclient.Client{}
		loggerConfig := &MessageLoggerConfig{
			MonitorSubjects: []string{"test.>"},
			MaxEntries:      1000,
			SampleRate:      0, // Should default to 1 (log all)
		}

		ml, err := NewMessageLogger(loggerConfig, natsClient)
		require.NoError(t, err)
		assert.Equal(t, 1, ml.sampleRate)
	})
}

// TestMessageLogger_GetStatistics_IncludesSampling tests that statistics include sampling info
func TestMessageLogger_GetStatistics_IncludesSampling(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	stats := ml.GetStatistics()

	assert.Contains(t, stats, "total_messages")
	assert.Contains(t, stats, "sampled_messages")
	assert.Contains(t, stats, "sample_rate")
}

// TestMessageLogger_TraceIndexing tests that trace IDs are indexed and retrievable
func TestMessageLogger_TraceIndexing(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// Simulate adding entries with trace IDs (mimics handleMessage behavior)
	traceID1 := "aaaabbbbccccdddd1111222233334444"
	traceID2 := "55556666777788889999aaaabbbbcccc"

	// Add entries for trace 1
	for i := 0; i < 3; i++ {
		seq := ml.nextSequence.Add(1)
		entry := MessageLogEntry{
			Sequence: seq,
			Subject:  "test.subject",
			TraceID:  traceID1,
			Summary:  "trace1 entry",
		}
		ml.storeEntry(entry)
		ml.indexTrace(traceID1, seq)
	}

	// Add entries for trace 2
	for i := 0; i < 2; i++ {
		seq := ml.nextSequence.Add(1)
		entry := MessageLogEntry{
			Sequence: seq,
			Subject:  "test.subject",
			TraceID:  traceID2,
			Summary:  "trace2 entry",
		}
		ml.storeEntry(entry)
		ml.indexTrace(traceID2, seq)
	}

	// Verify trace 1 entries
	entries1 := ml.GetEntriesByTrace(traceID1)
	assert.Len(t, entries1, 3, "Should have 3 entries for trace 1")
	for _, e := range entries1 {
		assert.Equal(t, traceID1, e.TraceID)
		assert.Equal(t, "trace1 entry", e.Summary)
	}

	// Verify trace 2 entries
	entries2 := ml.GetEntriesByTrace(traceID2)
	assert.Len(t, entries2, 2, "Should have 2 entries for trace 2")
	for _, e := range entries2 {
		assert.Equal(t, traceID2, e.TraceID)
		assert.Equal(t, "trace2 entry", e.Summary)
	}

	// Verify unknown trace returns empty
	entriesUnknown := ml.GetEntriesByTrace("00000000000000000000000000000000")
	assert.Nil(t, entriesUnknown, "Unknown trace should return nil")
}

func TestMessageLogger_TraceIndexingOutOfOrderStore(t *testing.T) {
	t.Parallel()

	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	firstTrace := "11111111111111111111111111111111"
	secondTrace := "22222222222222222222222222222222"
	firstSequence := ml.nextSequence.Add(1)
	secondSequence := ml.nextSequence.Add(1)

	// Model two subscription callbacks allocating in order but reaching the
	// circular-buffer lock in reverse order.
	ml.storeEntry(MessageLogEntry{Sequence: secondSequence, TraceID: secondTrace, Summary: "second"})
	ml.indexTrace(secondTrace, secondSequence)
	ml.storeEntry(MessageLogEntry{Sequence: firstSequence, TraceID: firstTrace, Summary: "first"})
	ml.indexTrace(firstTrace, firstSequence)

	first := ml.GetEntriesByTrace(firstTrace)
	second := ml.GetEntriesByTrace(secondTrace)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.Equal(t, "first", first[0].Summary)
	assert.Equal(t, "second", second[0].Summary)
}

func TestMessageLogger_OutOfOrderStoreCannotOverwriteNewerWrappedSequence(t *testing.T) {
	t.Parallel()

	ml := &MessageLogger{entries: make([]MessageLogEntry, 1), traceIndex: make(map[string][]uint64)}
	older := ml.nextSequence.Add(1)
	newer := ml.nextSequence.Add(1)

	ml.storeEntry(MessageLogEntry{Sequence: newer, Summary: "newer"})
	ml.storeEntry(MessageLogEntry{Sequence: older, Summary: "delayed older"})

	entries := ml.GetLogEntries(0)
	require.Len(t, entries, 1)
	assert.Equal(t, newer, entries[0].Sequence)
	assert.Equal(t, "newer", entries[0].Summary)
}

func TestMessageLogger_RecentEntriesUseStoredSequenceNotAllocationCompletion(t *testing.T) {
	t.Parallel()

	ml := &MessageLogger{entries: make([]MessageLogEntry, 2), traceIndex: make(map[string][]uint64)}
	stored := ml.nextSequence.Add(1)
	_ = ml.nextSequence.Add(1) // allocated by a callback that has not reached storage
	ml.storeEntry(MessageLogEntry{Sequence: stored, Summary: "visible"})

	entries := ml.GetLogEntries(0)
	require.Len(t, entries, 1)
	assert.Equal(t, stored, entries[0].Sequence)
}

// TestMessageLogger_TraceIndexing_CircularBuffer tests trace retrieval with buffer wraparound
func TestMessageLogger_TraceIndexing_CircularBuffer(t *testing.T) {
	// Create logger with small buffer to test wraparound
	natsClient := &natsclient.Client{}
	loggerConfig := &MessageLoggerConfig{
		MonitorSubjects: []string{"test.>"},
		MaxEntries:      10, // Small buffer
		OutputToStdout:  false,
	}
	ml, err := NewMessageLogger(loggerConfig, natsClient)
	require.NoError(t, err)

	traceID := "aaaabbbbccccdddd1111222233334444"

	// Add entry that will be overwritten
	seq1 := ml.nextSequence.Add(1)
	entry1 := MessageLogEntry{Sequence: seq1, TraceID: traceID, Summary: "old"}
	ml.storeEntry(entry1)
	ml.indexTrace(traceID, seq1)

	// Fill buffer to overwrite first entry
	for i := 0; i < 10; i++ {
		seq := ml.nextSequence.Add(1)
		entry := MessageLogEntry{Sequence: seq, Summary: "filler"}
		ml.storeEntry(entry)
	}

	// Add new entry with same trace
	seq2 := ml.nextSequence.Add(1)
	entry2 := MessageLogEntry{Sequence: seq2, TraceID: traceID, Summary: "new"}
	ml.storeEntry(entry2)
	ml.indexTrace(traceID, seq2)

	// Should only get the new entry (old one was overwritten)
	entries := ml.GetEntriesByTrace(traceID)
	assert.Len(t, entries, 1, "Should only have 1 entry (old one overwritten)")
	assert.Equal(t, "new", entries[0].Summary)
}
