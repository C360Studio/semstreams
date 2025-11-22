package component

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// LogLevel represents the severity level of a log entry
type LogLevel string

const (
	// LogLevelDebug represents debug-level logs
	LogLevelDebug LogLevel = "DEBUG"
	// LogLevelInfo represents informational logs
	LogLevelInfo LogLevel = "INFO"
	// LogLevelWarn represents warning logs
	LogLevelWarn LogLevel = "WARN"
	// LogLevelError represents error logs
	LogLevelError LogLevel = "ERROR"
)

// LogEntry represents a structured log entry that can be published to NATS
// and consumed by the Flow Builder SSE endpoint.
type LogEntry struct {
	Timestamp string   `json:"timestamp"` // RFC3339 format
	Level     LogLevel `json:"level"`
	Component string   `json:"component"`
	FlowID    string   `json:"flow_id"`
	Message   string   `json:"message"`
	Stack     string   `json:"stack,omitempty"` // Stack trace for errors
}

// Logger provides structured logging for components that publishes
// logs to NATS for real-time streaming via SSE. It wraps a standard slog.Logger
// for local logging while also publishing to NATS for remote consumption.
type Logger struct {
	componentName string
	flowID        string
	nc            *nats.Conn
	logger        *slog.Logger
	enabled       bool // whether NATS publishing is enabled
}

// NewLogger creates a new component logger that publishes to NATS
func NewLogger(componentName, flowID string, nc *nats.Conn, logger *slog.Logger) *Logger {
	return &Logger{
		componentName: componentName,
		flowID:        flowID,
		nc:            nc,
		logger:        logger,
		enabled:       nc != nil,
	}
}

// Debug logs a debug-level message
func (cl *Logger) Debug(msg string) {
	cl.DebugContext(context.Background(), msg)
}

// Info logs an info-level message
func (cl *Logger) Info(msg string) {
	cl.InfoContext(context.Background(), msg)
}

// Warn logs a warning-level message
func (cl *Logger) Warn(msg string) {
	cl.WarnContext(context.Background(), msg)
}

// Error logs an error-level message with optional error details
func (cl *Logger) Error(msg string, err error) {
	cl.ErrorContext(context.Background(), msg, err)
}

// DebugContext logs a debug-level message with context
func (cl *Logger) DebugContext(ctx context.Context, msg string) {
	cl.logWithContext(ctx, LogLevelDebug, msg, "")
	if cl.logger != nil {
		cl.logger.Debug(msg, "component", cl.componentName)
	}
}

// InfoContext logs an info-level message with context
func (cl *Logger) InfoContext(ctx context.Context, msg string) {
	cl.logWithContext(ctx, LogLevelInfo, msg, "")
	if cl.logger != nil {
		cl.logger.Info(msg, "component", cl.componentName)
	}
}

// WarnContext logs a warning-level message with context
func (cl *Logger) WarnContext(ctx context.Context, msg string) {
	cl.logWithContext(ctx, LogLevelWarn, msg, "")
	if cl.logger != nil {
		cl.logger.Warn(msg, "component", cl.componentName)
	}
}

// ErrorContext logs an error-level message with optional error details and context
func (cl *Logger) ErrorContext(ctx context.Context, msg string, err error) {
	stack := ""
	if err != nil {
		// Include error details as stack trace
		stack = fmt.Sprintf("%+v", err)
	}
	cl.logWithContext(ctx, LogLevelError, msg, stack)
	if cl.logger != nil {
		cl.logger.Error(msg, "component", cl.componentName, "error", err)
	}
}

// log publishes a log entry to NATS (deprecated: use logWithContext)
func (cl *Logger) log(level LogLevel, message, stack string) {
	cl.logWithContext(context.Background(), level, message, stack)
}

// logWithContext publishes a log entry to NATS with context cancellation support
func (cl *Logger) logWithContext(ctx context.Context, level LogLevel, message, stack string) {
	if !cl.enabled {
		return
	}

	// Check context before performing I/O
	select {
	case <-ctx.Done():
		return
	default:
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Component: cl.componentName,
		FlowID:    cl.flowID,
		Message:   message,
		Stack:     stack,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		// Failed to marshal - log locally but don't fail
		if cl.logger != nil {
			cl.logger.Error("Failed to marshal log entry", "error", err)
		}
		return
	}

	// Double-check NATS connection is still available (race condition fix)
	// This prevents nil pointer dereference if nc is set to nil after enabled check
	nc := cl.nc
	if nc == nil {
		return
	}

	// Check context again before network I/O
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Publish to NATS subject: logs.{flow_id}.{component}
	subject := fmt.Sprintf("logs.%s.%s", cl.flowID, cl.componentName)
	if err := nc.Publish(subject, data); err != nil {
		// Failed to publish - log locally but don't fail
		if cl.logger != nil {
			cl.logger.Error("Failed to publish log to NATS", "error", err, "subject", subject)
		}
	}
}
