package component

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComponentLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name          string
		componentName string
		flowID        string
		nc            *nats.Conn
		wantEnabled   bool
	}{
		{
			name:          "with NATS connection",
			componentName: "test-component",
			flowID:        "test-flow",
			nc:            &nats.Conn{}, // Mock connection
			wantEnabled:   true,
		},
		{
			name:          "without NATS connection",
			componentName: "test-component",
			flowID:        "test-flow",
			nc:            nil,
			wantEnabled:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := NewComponentLogger(tt.componentName, tt.flowID, tt.nc, logger)

			assert.Equal(t, tt.componentName, cl.componentName)
			assert.Equal(t, tt.flowID, cl.flowID)
			assert.Equal(t, tt.wantEnabled, cl.enabled)
			assert.Equal(t, logger, cl.logger)
		})
	}
}

func TestComponentLogger_LogLevels(t *testing.T) {
	// Get shared NATS client for integration test
	nc := getSharedNATSClient(t)

	componentName := "test-component"
	flowID := "test-flow-123"
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cl := NewComponentLogger(componentName, flowID, nc, logger)

	// Subscribe to logs for verification
	subject := fmt.Sprintf("logs.%s.%s", flowID, componentName)
	receivedLogs := make(chan LogEntry, 10)

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var entry LogEntry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			t.Errorf("Failed to unmarshal log entry: %v", err)
			return
		}
		receivedLogs <- entry
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Give subscription time to be ready
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name     string
		logFunc  func()
		wantMsg  string
		wantLvl  LogLevel
		wantErr  bool
	}{
		{
			name: "Debug level",
			logFunc: func() {
				cl.Debug("debug message")
			},
			wantMsg: "debug message",
			wantLvl: LogLevelDebug,
		},
		{
			name: "Info level",
			logFunc: func() {
				cl.Info("info message")
			},
			wantMsg: "info message",
			wantLvl: LogLevelInfo,
		},
		{
			name: "Warn level",
			logFunc: func() {
				cl.Warn("warning message")
			},
			wantMsg: "warning message",
			wantLvl: LogLevelWarn,
		},
		{
			name: "Error level without error",
			logFunc: func() {
				cl.Error("error message", nil)
			},
			wantMsg: "error message",
			wantLvl: LogLevelError,
		},
		{
			name: "Error level with error",
			logFunc: func() {
				cl.Error("error occurred", fmt.Errorf("test error"))
			},
			wantMsg: "error occurred",
			wantLvl: LogLevelError,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Log the message
			tt.logFunc()

			// Wait for log to be received
			select {
			case entry := <-receivedLogs:
				assert.Equal(t, tt.wantMsg, entry.Message)
				assert.Equal(t, tt.wantLvl, entry.Level)
				assert.Equal(t, componentName, entry.Component)
				assert.Equal(t, flowID, entry.FlowID)
				assert.NotEmpty(t, entry.Timestamp)

				// Verify timestamp is valid RFC3339
				_, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
				assert.NoError(t, err, "Timestamp should be valid RFC3339")

				if tt.wantErr {
					assert.NotEmpty(t, entry.Stack, "Stack trace should be present for errors")
				}

			case <-time.After(1 * time.Second):
				t.Fatal("Did not receive log entry in time")
			}
		})
	}
}

func TestComponentLogger_DisabledPublishing(t *testing.T) {
	// Create logger without NATS connection
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cl := NewComponentLogger("test-component", "test-flow", nil, logger)

	assert.False(t, cl.enabled, "Logger should be disabled without NATS")

	// These should not panic even without NATS connection
	cl.Debug("debug message")
	cl.Info("info message")
	cl.Warn("warning message")
	cl.Error("error message", fmt.Errorf("test error"))
}

func TestComponentLogger_ConcurrentLogging(t *testing.T) {
	nc := getSharedNATSClient(t)

	componentName := "concurrent-component"
	flowID := "test-flow-concurrent"
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cl := NewComponentLogger(componentName, flowID, nc, logger)

	// Subscribe to logs
	subject := fmt.Sprintf("logs.%s.%s", flowID, componentName)
	receivedLogs := make(chan LogEntry, 100)

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var entry LogEntry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			t.Errorf("Failed to unmarshal log entry: %v", err)
			return
		}
		receivedLogs <- entry
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	// Log concurrently from multiple goroutines
	numGoroutines := 10
	logsPerGoroutine := 5

	done := make(chan struct{})
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < logsPerGoroutine; j++ {
				msg := fmt.Sprintf("log from goroutine %d, message %d", id, j)
				cl.Info(msg)
			}
		}(i)
	}

	// Wait for all logs to be received
	expectedLogs := numGoroutines * logsPerGoroutine
	receivedCount := 0

	go func() {
		for range receivedLogs {
			receivedCount++
			if receivedCount >= expectedLogs {
				close(done)
				return
			}
		}
	}()

	select {
	case <-done:
		assert.Equal(t, expectedLogs, receivedCount, "Should receive all logs")
	case <-time.After(5 * time.Second):
		t.Fatalf("Did not receive all logs in time. Expected %d, got %d", expectedLogs, receivedCount)
	}
}

func TestLogEntry_JSONMarshaling(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     LogLevelInfo,
		Component: "test-component",
		FlowID:    "test-flow",
		Message:   "test message",
		Stack:     "optional stack trace",
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var decoded LogEntry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entry.Timestamp, decoded.Timestamp)
	assert.Equal(t, entry.Level, decoded.Level)
	assert.Equal(t, entry.Component, decoded.Component)
	assert.Equal(t, entry.FlowID, decoded.FlowID)
	assert.Equal(t, entry.Message, decoded.Message)
	assert.Equal(t, entry.Stack, decoded.Stack)
}

func TestLogEntry_JSONMarshaling_NoStack(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     LogLevelInfo,
		Component: "test-component",
		FlowID:    "test-flow",
		Message:   "test message",
		// Stack omitted
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Verify stack is omitted in JSON
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	_, hasStack := raw["stack"]
	assert.False(t, hasStack, "Empty stack should be omitted from JSON")
}
