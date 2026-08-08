// Package service provides the MessageLogger service for observing message flow
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
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
		cfg.MonitorSubjects = []string{"*"} // Default to auto-discover
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1 // Default: log all messages
	}

	// Handle "*" wildcard for auto-discovery from flow config
	var subjectMetadata map[string]portMetadata
	if containsWildcard(cfg.MonitorSubjects) {
		// Extract component configs for discovery
		var componentConfigs map[string]json.RawMessage
		if deps.Manager != nil {
			safeConfig := deps.Manager.GetConfig()
			if safeConfig != nil {
				flowConfig := safeConfig.Get()
				if flowConfig != nil && flowConfig.Components != nil {
					componentConfigs = make(map[string]json.RawMessage)
					for name, comp := range flowConfig.Components {
						if comp.Enabled {
							componentConfigs[name] = comp.Config
						}
					}
				}
			}
		}

		// Discover subjects from component port configs
		discoveredSubjects, metadata := discoverSubjectsFromComponents(componentConfigs)
		subjectMetadata = metadata

		// Replace "*" with discovered subjects, keep other explicit subjects
		var finalSubjects []string
		for _, subj := range cfg.MonitorSubjects {
			if subj == "*" {
				finalSubjects = append(finalSubjects, discoveredSubjects...)
			} else {
				finalSubjects = append(finalSubjects, subj)
			}
		}
		cfg.MonitorSubjects = uniqueStrings(finalSubjects)
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

	ml, err := NewMessageLogger(&cfg, deps.NATSClient, opts...)
	if ml != nil {
		ml.SetDecoder(message.NewDecoder(deps.PayloadRegistry))
	}
	if err != nil {
		return nil, err
	}

	// Set discovered metadata if available
	if subjectMetadata != nil {
		ml.subjectMetadata = subjectMetadata
	}

	return ml, nil
}

// containsWildcard checks if the subjects list contains the "*" auto-discover wildcard
func containsWildcard(subjects []string) bool {
	for _, s := range subjects {
		if s == "*" {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// MessageLoggerConfig holds configuration for the MessageLogger service
// Simple struct - no UnmarshalJSON, no Enabled field
type MessageLoggerConfig struct {
	// Subjects to monitor
	// Use "*" to auto-discover subjects from flow component configs
	// Example: ["*"] or ["*", "debug.>"] or ["raw.udp.messages", "processed.>"]
	MonitorSubjects []string `json:"monitor_subjects"`

	// Maximum entries to keep in memory for querying
	MaxEntries int `json:"max_entries"`

	// Whether to output to stdout
	OutputToStdout bool `json:"output_to_stdout"`

	// Log level threshold (DEBUG, INFO, WARN, ERROR)
	LogLevel string `json:"log_level"`

	// SampleRate controls message sampling (1 in N messages logged)
	// 0 or 1 = log all messages, 10 = log 10% of messages
	SampleRate int `json:"sample_rate"`
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
		MonitorSubjects: []string{"*"}, // Auto-discover from flow config
		MaxEntries:      10000,
		OutputToStdout:  false,
		LogLevel:        "INFO",
		SampleRate:      1, // Log all messages by default (increase for high-volume flows)
	}
}

// MessageLogEntry represents a logged message
type MessageLogEntry struct {
	Sequence    uint64          `json:"sequence"` // Monotonic sequence for index validity
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	MessageID   string          `json:"message_id,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"` // W3C trace ID (32 hex chars)
	SpanID      string          `json:"span_id,omitempty"`  // W3C span ID (16 hex chars)
	Summary     string          `json:"summary"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

// portMetadata holds information about a port for enriching log entries
type portMetadata struct {
	Component string // Component name (e.g., "udp", "json_generic")
	PortName  string // Port name (e.g., "udp_out", "generic_in")
	PortType  string // Port type (e.g., "jetstream", "nats")
	Interface string // Interface contract (e.g., "core.json.v1")
}

// MessageLogger provides message observation and logging as a service
type MessageLogger struct {
	*BaseService

	config MessageLoggerConfig // Consistent config field (not pointer)

	// Payload decoder for typed BaseMessage parsing. Optional — when
	// nil, messages are logged as "raw" type (unparseable envelope).
	// Set via SetDecoder; the production constructor wires it from
	// deps.PayloadRegistry.
	decoder *message.Decoder

	// NATS dependencies
	natsClient    *natsclient.Client
	subscriptions map[string]bool            // Track which subjects we're subscribed to
	natsSubsRefs  []*natsclient.Subscription // Subscription references for cleanup

	// Message storage (circular buffer)
	entries   []MessageLogEntry
	entriesMu sync.RWMutex

	// Trace indexing
	nextSequence atomic.Uint64
	traceIndex   map[string][]uint64 // traceID -> sequence numbers
	traceIndexMu sync.RWMutex

	// Sampling support
	sampleRate   int           // 1 in N messages (0 or 1 = all)
	messageCount atomic.Uint64 // Counter for sampling

	// Subject metadata for enriched logging
	subjectMetadata map[string]portMetadata

	// Statistics
	stats struct {
		totalMessages   atomic.Int64
		validMessages   atomic.Int64
		invalidMessages atomic.Int64
		sampledMessages atomic.Int64
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

	// Apply sample rate default
	sampleRate := loggerConfig.SampleRate
	if sampleRate == 0 {
		sampleRate = 1 // Default: log all messages
	}

	ml := &MessageLogger{
		BaseService:     baseService,
		config:          *loggerConfig, // Store config as value
		natsClient:      natsClient,
		subscriptions:   make(map[string]bool),
		entries:         make([]MessageLogEntry, maxEntries),
		traceIndex:      make(map[string][]uint64),
		sampleRate:      sampleRate,
		subjectMetadata: make(map[string]portMetadata),
		logger:          baseService.logger.With("source", "message-logger"),
	}

	// Initialize statistics
	ml.stats.startTime = time.Now()
	ml.stats.lastMessageTime.Store(time.Now())

	return ml, nil
}

// discoverSubjectsFromComponents extracts NATS subjects from component port configs.
// Returns a list of subjects and a map of subject -> portMetadata for enriched logging.
func discoverSubjectsFromComponents(components map[string]json.RawMessage) ([]string, map[string]portMetadata) {
	subjects := make(map[string]bool)
	metadata := make(map[string]portMetadata)

	for compName, rawConfig := range components {
		// Parse the component's config to extract ports
		var compConfig struct {
			Ports component.PortConfig `json:"ports"`
		}

		if err := json.Unmarshal(rawConfig, &compConfig); err != nil {
			continue // Skip components we can't parse
		}

		collectSubjectsFromDefinitions(compName, component.DirectionInput, compConfig.Ports.Inputs, subjects, metadata)
		collectSubjectsFromDefinitions(compName, component.DirectionOutput, compConfig.Ports.Outputs, subjects, metadata)
	}

	result := make([]string, 0, len(subjects))
	for subj := range subjects {
		result = append(result, subj)
	}
	return result, metadata
}

func collectSubjectsFromDefinitions(
	componentName string,
	direction component.Direction,
	definitions []component.PortDefinition,
	subjects map[string]bool,
	metadata map[string]portMetadata,
) {
	for _, definition := range definitions {
		port, err := definition.Resolve(direction)
		if err != nil {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			continue
		}
		interfaceType := ""
		if contract, ok := facts.Interface(); ok {
			interfaceType = contract.Type
		}
		for _, subject := range facts.NATSSubjects() {
			subjects[subject] = true
			metadata[subject] = portMetadata{
				Component: componentName,
				PortName:  definition.Name,
				PortType:  string(facts.Kind()),
				Interface: interfaceType,
			}
		}
	}
}

// SetDecoder installs the payload Decoder used for typed BaseMessage
// parsing in handleMessage. nil disables typed parsing — messages
// fall through to the "raw" type. Production wires this from
// deps.PayloadRegistry; tests can leave it nil to log raw envelopes.
func (ml *MessageLogger) SetDecoder(d *message.Decoder) {
	ml.decoder = d
}

// decodeBaseMessage parses data as a typed BaseMessage when a decoder
// is configured. Returns an error when the decoder is unset or the
// data is not a parseable envelope; handleMessage falls through to
// the "raw" type in either case.
func (ml *MessageLogger) decodeBaseMessage(data []byte) (*message.BaseMessage, error) {
	if ml.decoder == nil {
		return nil, fmt.Errorf("no payload decoder configured")
	}
	return ml.decoder.Decode(data)
}

// shouldSample returns true if this message should be logged based on sample rate
func (ml *MessageLogger) shouldSample() bool {
	if ml.sampleRate <= 1 {
		return true // Log all messages
	}
	count := ml.messageCount.Add(1)
	return count%uint64(ml.sampleRate) == 0
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
		sub, err := ml.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, msg *nats.Msg) {
			ml.handleMessage(msgCtx, msg.Subject, msg.Data)
		})
		if err != nil {
			ml.logger.Error("Failed to subscribe to subject",
				"subject", subject,
				"error", err)
			continue
		}
		ml.subscriptions[subject] = true
		ml.natsSubsRefs = append(ml.natsSubsRefs, sub)
		ml.logger.Debug("Subscribed to subject", "subject", subject)
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

		// Unsubscribe from all NATS subjects
		for _, sub := range ml.natsSubsRefs {
			if err := sub.Unsubscribe(); err != nil {
				ml.logger.Warn("Failed to unsubscribe", "error", err)
			}
		}

		ml.lifecycleMu.Lock()
		ml.subscriptions = make(map[string]bool)
		ml.natsSubsRefs = nil
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

	// Apply sampling - skip most messages based on sample rate
	if !ml.shouldSample() {
		return
	}

	ml.stats.sampledMessages.Add(1)

	// Parse message — try BaseMessage envelope first, fall back to raw JSON.
	// Graph request/reply families (graph.mutation.>, graph.query.>, etc.) use
	// raw JSON structs without BaseMessage wrapping. These are still valuable
	// for observability so we log them with the subject as the type identifier.
	var msgType, summary string
	if msg, err := ml.decodeBaseMessage(data); err == nil {
		ml.stats.validMessages.Add(1)
		msgType = msg.Type().String()
		summary = ml.generateSummary(msg)
	} else {
		ml.stats.validMessages.Add(1)
		msgType = "raw"
		summary = ml.generateRawSummary(subject, data)
	}

	// Extract trace context from ctx (populated by natsclient.Subscribe)
	var traceID, spanID string
	if tc, ok := natsclient.TraceContextFromContext(ctx); ok && tc != nil {
		traceID = tc.TraceID
		spanID = tc.SpanID
	}

	// Assign sequence number for indexing
	seq := ml.nextSequence.Add(1)

	// Create log entry
	entry := MessageLogEntry{
		Sequence:    seq,
		Timestamp:   time.Now(),
		Subject:     subject,
		MessageType: msgType,
		TraceID:     traceID,
		SpanID:      spanID,
		Summary:     summary,
		RawData:     json.RawMessage(data),
	}

	// Store entry and update trace index
	ml.storeEntry(entry)
	if traceID != "" {
		ml.indexTrace(traceID, seq)
	}

	// Log with structured fields for frontend filtering
	logArgs := []any{
		"subject", subject,
		"size", len(data),
	}

	// Add port metadata if available
	if meta, ok := ml.subjectMetadata[subject]; ok {
		logArgs = append(logArgs, "component", meta.Component)
		logArgs = append(logArgs, "port", meta.PortName)
		if meta.PortType != "" {
			logArgs = append(logArgs, "port_type", meta.PortType)
		}
		if meta.Interface != "" {
			logArgs = append(logArgs, "interface", meta.Interface)
		}
	}

	ml.logger.Debug("Message sample", logArgs...)

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

// generateRawSummary creates a summary for non-BaseMessage payloads (e.g. graph request/reply).
func (ml *MessageLogger) generateRawSummary(subject string, data []byte) string {
	// Extract top-level keys to give a sense of the payload shape
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Sprintf("Subject: %s (%d bytes)", subject, len(data))
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return fmt.Sprintf("Subject: %s, Keys: %v", subject, keys)
}

// storeEntry stores an entry in the circular buffer
func (ml *MessageLogger) storeEntry(entry MessageLogEntry) {
	ml.entriesMu.Lock()
	defer ml.entriesMu.Unlock()

	// Sequence owns the slot. Callback goroutines may reach this lock long after
	// newer callbacks have wrapped onto the same slot, so an older sequence must
	// never replace a newer observation already present there.
	index := int((entry.Sequence - 1) % uint64(len(ml.entries)))
	if ml.entries[index].Sequence >= entry.Sequence {
		return
	}
	ml.entries[index] = entry
}

// indexTrace adds a sequence number to the trace index
func (ml *MessageLogger) indexTrace(traceID string, seq uint64) {
	ml.traceIndexMu.Lock()
	defer ml.traceIndexMu.Unlock()
	ml.traceIndex[traceID] = append(ml.traceIndex[traceID], seq)
}

// GetEntriesByTrace returns all log entries for a specific trace ID
// Entries are returned in chronological order (by sequence number)
func (ml *MessageLogger) GetEntriesByTrace(traceID string) []MessageLogEntry {
	// Get sequence numbers for this trace
	ml.traceIndexMu.RLock()
	sequences := make([]uint64, len(ml.traceIndex[traceID]))
	copy(sequences, ml.traceIndex[traceID])
	ml.traceIndexMu.RUnlock()

	if len(sequences) == 0 {
		return nil
	}

	bufferSize := uint64(len(ml.entries))

	// Collect valid entries
	ml.entriesMu.RLock()
	defer ml.entriesMu.RUnlock()

	var results []MessageLogEntry
	for _, seq := range sequences {
		// Sequence starts at 1, index starts at 0, so subtract 1
		idx := int((seq - 1) % bufferSize)
		entry := ml.entries[idx]
		if entry.Sequence == seq { // Verify not overwritten
			results = append(results, entry)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Sequence < results[j].Sequence
	})

	return results
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

	return newestEntries(ml.entries, limit)
}

func newestEntries(entries []MessageLogEntry, limit int) []MessageLogEntry {
	result := make([]MessageLogEntry, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if entry.Sequence != 0 {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Sequence > result[j].Sequence
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// GetStatistics returns runtime statistics
func (ml *MessageLogger) GetStatistics() map[string]any {
	lastMessageTime, _ := ml.stats.lastMessageTime.Load().(time.Time)

	return map[string]any{
		"total_messages":     ml.stats.totalMessages.Load(),
		"sampled_messages":   ml.stats.sampledMessages.Load(),
		"valid_messages":     ml.stats.validMessages.Load(),
		"invalid_messages":   ml.stats.invalidMessages.Load(),
		"sample_rate":        ml.sampleRate,
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

	// Copy existing entries without calling GetLogEntries while holding entriesMu.
	// Preserve the newest sequence for every slot when shrinking causes collisions.
	if len(ml.entries) > 0 {
		currentEntries := newestEntries(ml.entries, len(ml.entries))

		// Copy as many as we can fit, starting with most recent
		copyCount := len(currentEntries)
		if copyCount > maxEntries {
			copyCount = maxEntries
		}

		for i := 0; i < copyCount; i++ {
			entry := currentEntries[i]
			index := int((entry.Sequence - 1) % uint64(maxEntries))
			if newEntries[index].Sequence < entry.Sequence {
				newEntries[index] = entry
			}
		}
	}

	// Replace the buffer
	ml.entries = newEntries

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
		sub, err := ml.natsClient.Subscribe(context.Background(), subject, func(msgCtx context.Context, msg *nats.Msg) {
			ml.handleMessage(msgCtx, msg.Subject, msg.Data)
		})
		if err != nil {
			ml.logger.Error("Failed to subscribe to subject",
				"subject", subject,
				"error", err)
			continue
		}
		ml.subscriptions[subject] = true
		ml.natsSubsRefs = append(ml.natsSubsRefs, sub)
		ml.logger.Debug("Subscribed to subject", "subject", subject)
	}

	ml.logger.Debug("MessageLogger runtime started",
		"monitored_subjects", len(ml.subscriptions),
		"max_entries", ml.config.MaxEntries,
		"output_to_stdout", ml.config.OutputToStdout)

	return nil
}

// stopRuntime stops NATS subscriptions and logging.
func (ml *MessageLogger) stopRuntime() error {
	// Unsubscribe from all NATS subjects
	for _, sub := range ml.natsSubsRefs {
		if err := sub.Unsubscribe(); err != nil {
			ml.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	ml.subscriptions = make(map[string]bool)
	ml.natsSubsRefs = nil

	ml.logger.Debug("MessageLogger runtime stopped")
	return nil
}
