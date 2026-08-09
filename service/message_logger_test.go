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
	assert.NotContains(t, schema.ConfigSchema.Properties, "enabled")
	assert.NotContains(t, schema.ConfigSchema.Properties, "log_level")
	assert.Contains(t, schema.ConfigSchema.Properties, "monitor_subjects")
	assert.Contains(t, schema.ConfigSchema.Properties, "max_entries")
	assert.Contains(t, schema.ConfigSchema.Properties, "output_to_stdout")
	assert.Contains(t, schema.ConfigSchema.Properties, "sample_rate")

	// Verify monitor_subjects property
	monitorSubjects := schema.ConfigSchema.Properties["monitor_subjects"]
	assert.Equal(t, "array", monitorSubjects.Type)
	assert.Equal(t,
		"NATS subjects to monitor; '*' discovers accepted Registry declarations and explicit subjects are unioned",
		monitorSubjects.Description)
	expectedDefault := []string{"*"}
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

func TestMessageLoggerRejectsRetiredInnerFields(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"enabled":true}`),
		json.RawMessage(`{"log_level":"INFO"}`),
	} {
		if _, err := NewMessageLoggerService(raw, &Dependencies{}); err == nil {
			t.Fatalf("retired config %s was accepted", raw)
		}
	}
}

func TestMessageLogger_ConfigurableSchema(t *testing.T) {
	ml, err := createTestMessageLogger()
	require.NoError(t, err)

	// MessageLogger exposes next-boot schema only; activation is outer config.
	var _ Configurable = ml
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

func TestResolveLoggerSubjectsDeduplicatesOnlyAcceptedContainmentPairs(t *testing.T) {
	desired := map[string]struct{}{
		"agent.toolcall.proposed.>": {}, "agent.toolcall.proposed.*": {},
		"agent.toolcall.approved.>": {}, "agent.toolcall.approved.*": {},
		"agent.toolcall.rejected.>": {}, "agent.toolcall.rejected.*": {},
		"unrelated.>": {}, "unrelated.*": {},
	}

	subjects, overlaps := resolveLoggerSubjects(desired)
	require.Equal(t, []string{
		"agent.toolcall.approved.>",
		"agent.toolcall.proposed.>",
		"agent.toolcall.rejected.>",
		"unrelated.*",
		"unrelated.>",
	}, subjects)
	require.Equal(t, []subjectOverlap{
		{Broader: "agent.toolcall.proposed.>", Covered: "agent.toolcall.proposed.*", Resolution: "covered subscription omitted"},
		{Broader: "agent.toolcall.approved.>", Covered: "agent.toolcall.approved.*", Resolution: "covered subscription omitted"},
		{Broader: "agent.toolcall.rejected.>", Covered: "agent.toolcall.rejected.*", Resolution: "covered subscription omitted"},
	}, overlaps)
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
