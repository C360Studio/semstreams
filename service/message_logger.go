// Package service provides the MessageLogger service for observing message flow
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
)

// NewMessageLoggerService creates a new message logger service using the standard constructor pattern
func NewMessageLoggerService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg MessageLoggerConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse message-logger config: %w", err)
		}
	}

	// Apply defaults - clear and visible in constructor
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = 10000
	}
	if len(cfg.MonitorSubjects) == 0 {
		cfg.MonitorSubjects = []string{"process.>", "input.>", "events.>"}
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate message-logger config: %w", err)
	}

	// Check if NATS client is available
	if deps.NATSClient == nil {
		return nil, fmt.Errorf("message-logger requires NATS client")
	}

	// Create the MessageLogger with dependencies
	var opts []Option
	if deps.Logger != nil {
		opts = append(opts, WithLogger(deps.Logger))
	}
	if deps.MetricsRegistry != nil {
		opts = append(opts, WithMetrics(deps.MetricsRegistry))
	}

	return NewMessageLogger(&cfg, deps.NATSClient, opts...)
}

// MessageLoggerConfig holds configuration for the MessageLogger service
// Simple struct - no UnmarshalJSON, no Enabled field
type MessageLoggerConfig struct {
	// Subjects to monitor (empty = default to ["process.>", "input.>", "events.>"])
	MonitorSubjects []string `json:"monitor_subjects"`

	// Maximum entries to keep in memory for querying
	MaxEntries int `json:"max_entries"`

	// Whether to output to stdout
	OutputToStdout bool `json:"output_to_stdout"`

	// Log level threshold (DEBUG, INFO, WARN, ERROR)
	LogLevel string `json:"log_level"`
}

// Validate checks if the configuration is valid
func (c MessageLoggerConfig) Validate() error {
	if c.MaxEntries < 0 {
		return fmt.Errorf("max_entries cannot be negative")
	}
	if c.MaxEntries > 100000 {
		return fmt.Errorf("max_entries cannot exceed 100000")
	}
	// MonitorSubjects can be empty (will get defaults)
	// LogLevel can be empty (will get default)
	return nil
}

// DefaultMessageLoggerConfig returns sensible defaults
func DefaultMessageLoggerConfig() MessageLoggerConfig {
	return MessageLoggerConfig{
		MonitorSubjects: []string{"process.>", "input.>", "events.>"}, // Default subjects
		MaxEntries:      10000,
		OutputToStdout:  false,
		LogLevel:        "INFO",
	}
}

// MessageLogEntry represents a logged message
type MessageLogEntry struct {
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	MessageID   string          `json:"message_id,omitempty"`
	Summary     string          `json:"summary"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

// MessageLogger provides message observation and logging as a service
type MessageLogger struct {
	*BaseService

	config MessageLoggerConfig // Consistent config field (not pointer)

	// NATS dependencies
	natsClient    *natsclient.Client
	subscriptions map[string]bool // Track which subjects we're subscribed to

	// Message storage (circular buffer)
	entries      []MessageLogEntry
	entriesIndex int
	entriesMu    sync.RWMutex

	// Statistics
	stats struct {
		totalMessages   atomic.Int64
		validMessages   atomic.Int64
		invalidMessages atomic.Int64
		startTime       time.Time
		lastMessageTime atomic.Value // time.Time
	}

	// Lifecycle management
	lifecycleMu sync.Mutex // Protects lifecycle fields
	shutdown    chan struct{}
	done        chan struct{}
	logger      *slog.Logger
	running     bool // Track if service is running (replaces config.Enabled)
}

// NewMessageLogger creates a new MessageLogger service
func NewMessageLogger(
	loggerConfig *MessageLoggerConfig,
	natsClient *natsclient.Client,
	opts ...Option,
) (*MessageLogger, error) {
	if loggerConfig == nil {
		defaultConfig := DefaultMessageLoggerConfig()
		loggerConfig = &defaultConfig
	}

	// Create base service
	baseService := NewBaseServiceWithOptions("message-logger", nil, opts...) // Config is now service-specific

	// Initialize entries buffer
	maxEntries := loggerConfig.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 10000
	}

	ml := &MessageLogger{
		BaseService:   baseService,
		config:        *loggerConfig, // Store config as value
		natsClient:    natsClient,
		subscriptions: make(map[string]bool),
		entries:       make([]MessageLogEntry, maxEntries),
		logger:        slog.Default().With("component", "message-logger-service"),
	}

	// Initialize statistics
	ml.stats.startTime = time.Now()
	ml.stats.lastMessageTime.Store(time.Now())

	return ml, nil
}

// Start begins message observation
func (ml *MessageLogger) Start(ctx context.Context) error {
	if err := ml.BaseService.Start(ctx); err != nil {
		return err
	}

	ml.lifecycleMu.Lock()
	defer ml.lifecycleMu.Unlock()

	if ml.running {
		return fmt.Errorf("message logger already running")
	}

	// MessageLogger is always enabled when running (managed by Manager)
	ml.logger.Info("MessageLogger starting")
	ml.running = true

	// Create shutdown channels
	ml.shutdown = make(chan struct{})
	ml.done = make(chan struct{})

	// Subscribe to configured subjects
	for _, subject := range ml.config.MonitorSubjects {
		err := ml.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, data []byte) {
			ml.handleMessage(msgCtx, subject, data)
		})
		if err != nil {
			ml.logger.Error("Failed to subscribe to subject",
				"subject", subject,
				"error", err)
			continue
		}
		ml.subscriptions[subject] = true
		ml.logger.Info("Subscribed to subject", "subject", subject)
	}

	ml.logger.Info("MessageLogger started",
		"monitored_subjects", len(ml.subscriptions),
		"max_entries", ml.config.MaxEntries,
		"output_to_stdout", ml.config.OutputToStdout)

	return nil
}

// Stop gracefully stops the MessageLogger
func (ml *MessageLogger) Stop(timeout time.Duration) error {
	ml.lifecycleMu.Lock()

	if !ml.running {
		ml.lifecycleMu.Unlock()
		return nil // Already stopped
	}

	ml.running = false
	shutdown := ml.shutdown // Capture channel reference
	ml.lifecycleMu.Unlock()

	if shutdown != nil {
		close(shutdown)

		// Note: natsclient doesn't provide unsubscribe method
		// Subscriptions will be cleaned up when connection closes
		ml.lifecycleMu.Lock()
		ml.subscriptions = make(map[string]bool)
		ml.shutdown = nil // Prevent double-close
		ml.done = nil     // Clear done channel reference
		ml.lifecycleMu.Unlock()

		// MessageLogger doesn't have worker goroutines to wait for
		// NATS subscriptions run in NATS goroutines and will be cleaned up
		// when the connection closes
		ml.logger.Info("MessageLogger stopped")
	}

	return ml.BaseService.Stop(timeout)
}

// handleMessage processes incoming messages
func (ml *MessageLogger) handleMessage(ctx context.Context, subject string, data []byte) {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return
	default:
	}

	ml.stats.totalMessages.Add(1)
	ml.stats.lastMessageTime.Store(time.Now())

	// Parse message
	var msg message.BaseMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		ml.stats.invalidMessages.Add(1)
		ml.logger.Debug("Failed to parse message",
			"subject", subject,
			"error", err,
			"data_len", len(data))
		return
	}

	ml.stats.validMessages.Add(1)

	// Create log entry
	entry := MessageLogEntry{
		Timestamp:   time.Now(),
		Subject:     subject,
		MessageType: msg.Type().String(),
		MessageID:   "", // core messages don't have IDs
		Summary:     ml.generateSummary(&msg),
		RawData:     json.RawMessage(data),
	}

	// Store entry
	ml.storeEntry(entry)

	// Output to stdout if configured
	if ml.config.OutputToStdout {
		ml.outputEntry(entry)
	}
}

// generateSummary creates a human-readable summary of the message
func (ml *MessageLogger) generateSummary(msg *message.BaseMessage) string {
	summary := fmt.Sprintf("Type: %s", msg.Type())

	// Add payload info if available
	if payload := msg.Payload(); payload != nil {
		summary += fmt.Sprintf(", Payload: %T", payload)
	}

	return summary
}

// storeEntry stores an entry in the circular buffer
func (ml *MessageLogger) storeEntry(entry MessageLogEntry) {
	ml.entriesMu.Lock()
	defer ml.entriesMu.Unlock()

	ml.entries[ml.entriesIndex] = entry
	ml.entriesIndex = (ml.entriesIndex + 1) % len(ml.entries)
}

// outputEntry outputs an entry to stdout
func (ml *MessageLogger) outputEntry(entry MessageLogEntry) {
	fmt.Printf("[%s] %s: %s\n",
		entry.Timestamp.Format("15:04:05.000"),
		entry.Subject,
		entry.Summary)
}

// GetMessages returns recent log entries
func (ml *MessageLogger) GetMessages() []MessageLogEntry {
	return ml.GetLogEntries(0) // Return all available entries
}

// GetLogEntries returns recent log entries with optional limit
func (ml *MessageLogger) GetLogEntries(limit int) []MessageLogEntry {
	ml.entriesMu.RLock()
	defer ml.entriesMu.RUnlock()

	if limit <= 0 || limit > len(ml.entries) {
		limit = len(ml.entries)
	}

	result := make([]MessageLogEntry, 0, limit)

	// Start from most recent and work backwards
	start := ml.entriesIndex - 1
	if start < 0 {
		start = len(ml.entries) - 1
	}

	for i := 0; i < limit; i++ {
		idx := (start - i + len(ml.entries)) % len(ml.entries)
		entry := ml.entries[idx]
		if !entry.Timestamp.IsZero() {
			result = append(result, entry)
		}
	}

	return result
}

// GetStatistics returns runtime statistics
func (ml *MessageLogger) GetStatistics() map[string]any {
	lastMessageTime, _ := ml.stats.lastMessageTime.Load().(time.Time)

	return map[string]any{
		"total_messages":     ml.stats.totalMessages.Load(),
		"valid_messages":     ml.stats.validMessages.Load(),
		"invalid_messages":   ml.stats.invalidMessages.Load(),
		"start_time":         ml.stats.startTime,
		"last_message_time":  lastMessageTime,
		"uptime_seconds":     time.Since(ml.stats.startTime).Seconds(),
		"monitored_subjects": ml.config.MonitorSubjects,
		"max_entries":        ml.config.MaxEntries,
	}
}

// ConfigSchema returns the configuration schema for this service.
// This implements the Configurable interface for UI discovery.
func (ml *MessageLogger) ConfigSchema() ConfigSchema {
	return NewConfigSchema(map[string]PropertySchema{
		"enabled": {
			PropertySchema: component.PropertySchema{
				Type:        "bool",
				Description: "Enable or disable message logging",
				Default:     false,
			},
			Runtime:  true,
			Category: "lifecycle",
		},
		"monitor_subjects": {
			PropertySchema: component.PropertySchema{
				Type:        "array",
				Description: "NATS subjects to monitor for messages",
				Default:     []string{"process.>", "input.>", "events.>"},
			},
			Runtime:  true,
			Category: "monitoring",
		},
		"max_entries": {
			PropertySchema: component.PropertySchema{
				Type:        "integer",
				Description: "Maximum entries to keep in memory",
				Default:     10000,
				Minimum:     intPtr(1000),
				Maximum:     intPtr(100000),
			},
			Runtime:  true,
			Category: "storage",
		},
		"output_to_stdout": {
			PropertySchema: component.PropertySchema{
				Type:        "bool",
				Description: "Whether to output messages to stdout",
				Default:     false,
			},
			Runtime:  true,
			Category: "output",
		},
	}, []string{}) // No required fields - all have defaults
}

// ValidateConfigUpdate checks if the proposed changes are valid.
// This implements the RuntimeConfigurable interface.
func (ml *MessageLogger) ValidateConfigUpdate(changes map[string]any) error {
	for key, value := range changes {
		switch key {
		case "enabled":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("enabled must be boolean, got %T", value)
			}

		case "monitor_subjects":
			subjects, ok := value.([]any)
			if !ok {
				return fmt.Errorf("monitor_subjects must be array, got %T", value)
			}
			if len(subjects) == 0 {
				return fmt.Errorf("monitor_subjects cannot be empty")
			}
			for i, s := range subjects {
				if _, ok := s.(string); !ok {
					return fmt.Errorf("monitor_subjects[%d] must be string, got %T", i, s)
				}
			}

		case "max_entries":
			var entries int
			switch v := value.(type) {
			case float64:
				entries = int(v) // JSON numbers are float64
			case int:
				entries = v
			default:
				return fmt.Errorf("max_entries must be number, got %T", value)
			}
			if entries < 1000 || entries > 100000 {
				return fmt.Errorf("max_entries must be between 1000 and 100000, got %d", entries)
			}

		case "output_to_stdout":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("output_to_stdout must be boolean, got %T", value)
			}

		default:
			return fmt.Errorf("unknown configuration property: %s", key)
		}
	}
	return nil
}

// ApplyConfigUpdate applies validated configuration changes.
// This implements the RuntimeConfigurable interface.
func (ml *MessageLogger) ApplyConfigUpdate(changes map[string]any) error {
	ml.entriesMu.Lock()
	defer ml.entriesMu.Unlock()

	for key, value := range changes {
		switch key {
		case "enabled":
			// The enabled state is managed by Manager
			// This is just for tracking
			ml.logger.Info("MessageLogger enabled state changed", "enabled", value.(bool))

		case "monitor_subjects":
			subjects := make([]string, 0)
			for _, s := range value.([]any) {
				subjects = append(subjects, s.(string))
			}
			if err := ml.updateMonitorSubjects(subjects); err != nil {
				return fmt.Errorf("failed to update monitor subjects: %w", err)
			}
			ml.config.MonitorSubjects = subjects

		case "max_entries":
			var newMax int
			switch v := value.(type) {
			case float64:
				newMax = int(v)
			case int:
				newMax = v
			}
			if err := ml.updateMaxEntries(newMax); err != nil {
				return fmt.Errorf("failed to update max entries: %w", err)
			}
			ml.config.MaxEntries = newMax

		case "output_to_stdout":
			ml.config.OutputToStdout = value.(bool)
		}
	}
	return nil
}

// GetRuntimeConfig returns current configuration values.
// This implements the RuntimeConfigurable interface.
func (ml *MessageLogger) GetRuntimeConfig() map[string]any {
	ml.entriesMu.RLock()
	defer ml.entriesMu.RUnlock()

	return map[string]any{
		"enabled":          true, // MessageLogger is running if this method is called
		"monitor_subjects": ml.config.MonitorSubjects,
		"max_entries":      ml.config.MaxEntries,
		"output_to_stdout": ml.config.OutputToStdout,
	}
}

// updateEnabledState starts or stops message logging.
func (ml *MessageLogger) updateEnabledState(enabled bool) error {
	if enabled && !ml.running {
		// Starting: subscribe to subjects if we're not already running
		ml.running = true
		return ml.startRuntime()
	} else if !enabled && ml.running {
		// Stopping: unsubscribe from subjects
		ml.running = false
		return ml.stopRuntime()
	}
	return nil
}

// updateMonitorSubjects changes NATS subscriptions.
func (ml *MessageLogger) updateMonitorSubjects(subjects []string) error {
	if !ml.running {
		// If not running, just update the config - subscriptions will be created when enabled
		return nil
	}

	// If enabled, we need to update active subscriptions
	// First stop current subscriptions
	if err := ml.stopRuntime(); err != nil {
		return fmt.Errorf("failed to stop current subscriptions: %w", err)
	}

	// Update subjects
	ml.config.MonitorSubjects = subjects

	// Start new subscriptions
	if err := ml.startRuntime(); err != nil {
		return fmt.Errorf("failed to start new subscriptions: %w", err)
	}

	return nil
}

// updateMaxEntries resizes the circular buffer.
// NOTE: This method should be called with entriesMu already locked
func (ml *MessageLogger) updateMaxEntries(maxEntries int) error {
	if maxEntries == len(ml.entries) {
		return nil // No change needed
	}

	// Create new buffer with new size
	newEntries := make([]MessageLogEntry, maxEntries)

	// Copy existing entries if possible (without calling GetLogEntries to avoid deadlock)
	if len(ml.entries) > 0 {
		// Collect current entries in order (most recent first)
		var currentEntries []MessageLogEntry

		// Start from most recent and work backwards
		start := ml.entriesIndex - 1
		if start < 0 {
			start = len(ml.entries) - 1
		}

		for i := 0; i < len(ml.entries); i++ {
			idx := (start - i + len(ml.entries)) % len(ml.entries)
			entry := ml.entries[idx]
			if !entry.Timestamp.IsZero() {
				currentEntries = append(currentEntries, entry)
			}
		}

		// Copy as many as we can fit, starting with most recent
		copyCount := len(currentEntries)
		if copyCount > maxEntries {
			copyCount = maxEntries
		}

		for i := 0; i < copyCount; i++ {
			newEntries[i] = currentEntries[i]
		}
	}

	// Replace the buffer
	ml.entries = newEntries
	ml.entriesIndex = 0

	return nil
}

// startRuntime starts NATS subscriptions and logging.
func (ml *MessageLogger) startRuntime() error {
	if ml.natsClient == nil {
		return fmt.Errorf("NATS client not available")
	}

	// Create shutdown channels if not already created
	if ml.shutdown == nil {
		ml.shutdown = make(chan struct{})
	}
	if ml.done == nil {
		ml.done = make(chan struct{})
	}

	// Subscribe to configured subjects
	for _, subject := range ml.config.MonitorSubjects {
		err := ml.natsClient.Subscribe(context.Background(), subject, func(msgCtx context.Context, data []byte) {
			ml.handleMessage(msgCtx, subject, data)
		})
		if err != nil {
			ml.logger.Error("Failed to subscribe to subject",
				"subject", subject,
				"error", err)
			continue
		}
		ml.subscriptions[subject] = true
		ml.logger.Info("Subscribed to subject", "subject", subject)
	}

	ml.logger.Info("MessageLogger runtime started",
		"monitored_subjects", len(ml.subscriptions),
		"max_entries", ml.config.MaxEntries,
		"output_to_stdout", ml.config.OutputToStdout)

	return nil
}

// stopRuntime stops NATS subscriptions and logging.
func (ml *MessageLogger) stopRuntime() error {
	// Note: natsclient doesn't provide unsubscribe method
	// Subscriptions will be cleaned up when connection closes
	ml.subscriptions = make(map[string]bool)

	ml.logger.Info("MessageLogger runtime stopped")
	return nil
}
