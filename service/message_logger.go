// Package service provides the MessageLogger service for observing message flow
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
)

// NewMessageLoggerService creates a new message logger service using the standard constructor pattern
func NewMessageLoggerService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg MessageLoggerConfig
	if err := decodeStrictServiceJSON(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("parse message-logger config: %w", err)
	}

	// Apply defaults - clear and visible in constructor
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = 10000
	}
	if len(cfg.MonitorSubjects) == 0 {
		cfg.MonitorSubjects = []string{"*"} // Default to auto-discover
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1 // Default: log all messages
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
		ml.componentRegistry = deps.ComponentRegistry
	}
	if err != nil {
		return nil, err
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
	// Use "*" to discover subjects from accepted Registry declarations
	// Example: ["*"] or ["*", "debug.>"] or ["raw.udp.messages", "processed.>"]
	MonitorSubjects []string `json:"monitor_subjects"`

	// Maximum entries to keep in memory for querying
	MaxEntries int `json:"max_entries"`

	// Whether to output to stdout
	OutputToStdout bool `json:"output_to_stdout"`

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
	return nil
}

// DefaultMessageLoggerConfig returns sensible defaults
func DefaultMessageLoggerConfig() MessageLoggerConfig {
	return MessageLoggerConfig{
		MonitorSubjects: []string{"*"}, // Auto-discover from flow config
		MaxEntries:      10000,
		OutputToStdout:  false,
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

type subjectOverlap struct {
	Broader    string `json:"broader"`
	Covered    string `json:"covered"`
	Resolution string `json:"resolution"`
}

type messageLoggerSubscription interface {
	Unsubscribe() error
}

type messageLoggerSubscribe func(
	context.Context, string, func(context.Context, *nats.Msg),
) (messageLoggerSubscription, error)

const messageLoggerReconcileRetryDelay = 100 * time.Millisecond

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
	natsClient        *natsclient.Client
	componentRegistry *component.Registry
	subscribe         messageLoggerSubscribe
	subscriptions     map[string]messageLoggerSubscription
	autoDiscover      bool
	explicitSubjects  []string
	resolvedSubjects  []string
	subjectOverlaps   []subjectOverlap
	reconcileError    string
	retryAfter        func(time.Duration) <-chan time.Time

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
	subjectMu       sync.RWMutex

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
	transitionMu       sync.Mutex // Serializes complete Start and Stop transitions.
	lifecycleMu        sync.Mutex // Protects lifecycle fields and subscription state.
	subscriptionCancel context.CancelFunc
	retryCancel        context.CancelFunc
	retryDone          chan struct{}
	generation         *lifecyclejoin.Generation
	teardownOnce       sync.Once
	teardownErr        error
	logger             *slog.Logger
	running            bool // Track if service is running (replaces config.Enabled)
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
		BaseService: baseService,
		config:      *loggerConfig, // Store config as value
		natsClient:  natsClient,
		subscribe: func(
			ctx context.Context, subject string, handler func(context.Context, *nats.Msg),
		) (messageLoggerSubscription, error) {
			return natsClient.Subscribe(ctx, subject, handler)
		},
		subscriptions:    make(map[string]messageLoggerSubscription),
		autoDiscover:     containsWildcard(loggerConfig.MonitorSubjects),
		explicitSubjects: explicitMonitorSubjects(loggerConfig.MonitorSubjects),
		entries:          make([]MessageLogEntry, maxEntries),
		traceIndex:       make(map[string][]uint64),
		sampleRate:       sampleRate,
		subjectMetadata:  make(map[string]portMetadata),
		retryAfter:       time.After,
		logger:           baseService.logger.With("source", "message-logger"),
	}

	// Initialize statistics
	ml.stats.startTime = time.Now()
	ml.stats.lastMessageTime.Store(time.Now())

	return ml, nil
}

func explicitMonitorSubjects(configured []string) []string {
	explicit := make([]string, 0, len(configured))
	for _, subject := range configured {
		if subject != "*" {
			explicit = append(explicit, subject)
		}
	}
	sort.Strings(explicit)
	return uniqueStrings(explicit)
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

func (ml *MessageLogger) registrySubjects() (map[string]portMetadata, []string, []subjectOverlap) {
	metadata := make(map[string]portMetadata)
	desired := make(map[string]struct{}, len(ml.explicitSubjects))
	for _, subject := range ml.explicitSubjects {
		desired[subject] = struct{}{}
	}
	for _, snapshot := range ml.componentRegistry.Snapshots(componentadmission.Access{}) {
		collectSnapshotSubjects(snapshot.Name(), snapshot.Inputs(), snapshot.InputDeclarationFacts(), desired, metadata)
		collectSnapshotSubjects(snapshot.Name(), snapshot.Outputs(), snapshot.OutputDeclarationFacts(), desired, metadata)
	}
	subjects, overlaps := resolveLoggerSubjects(desired)
	return metadata, subjects, overlaps
}

func collectSnapshotSubjects(
	componentName string,
	ports []component.Port,
	facts []component.PortFacts,
	desired map[string]struct{},
	metadata map[string]portMetadata,
) {
	for index, port := range ports {
		portFacts := facts[index]
		switch portFacts.Kind() {
		case component.PortKindNATS, component.PortKindNATSRequest, component.PortKindJetStream:
		default:
			continue
		}
		interfaceType := ""
		if contract, ok := portFacts.Interface(); ok {
			interfaceType = contract.Type
		}
		for _, subject := range portFacts.NATSSubjects() {
			if subject == "*" || subject == ">" || subject == "_INBOX" || strings.HasPrefix(subject, "_INBOX.") {
				continue
			}
			desired[subject] = struct{}{}
			if _, exists := metadata[subject]; !exists {
				metadata[subject] = portMetadata{
					Component: componentName,
					PortName:  port.Name,
					PortType:  string(portFacts.Kind()),
					Interface: interfaceType,
				}
			}
		}
	}
}

func resolveLoggerSubjects(desired map[string]struct{}) ([]string, []subjectOverlap) {
	overlaps := make([]subjectOverlap, 0, 3)
	for _, pair := range [][2]string{
		{"agent.toolcall.proposed.>", "agent.toolcall.proposed.*"},
		{"agent.toolcall.approved.>", "agent.toolcall.approved.*"},
		{"agent.toolcall.rejected.>", "agent.toolcall.rejected.*"},
	} {
		if _, broad := desired[pair[0]]; !broad {
			continue
		}
		if _, covered := desired[pair[1]]; !covered {
			continue
		}
		delete(desired, pair[1])
		overlaps = append(overlaps, subjectOverlap{
			Broader: pair[0], Covered: pair[1], Resolution: "covered subscription omitted",
		})
	}
	subjects := make([]string, 0, len(desired))
	for subject := range desired {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	return subjects, overlaps
}

func (ml *MessageLogger) reconcileSubjects(
	ctx context.Context,
	desired []string,
	metadata map[string]portMetadata,
	overlaps []subjectOverlap,
) error {
	ml.lifecycleMu.Lock()
	defer ml.lifecycleMu.Unlock()
	return ml.reconcileSubjectsLocked(ctx, desired, metadata, overlaps)
}

func (ml *MessageLogger) reconcileSubjectsLocked(
	ctx context.Context,
	desired []string,
	metadata map[string]portMetadata,
	overlaps []subjectOverlap,
) error {
	desiredSet := make(map[string]struct{}, len(desired))
	for _, subject := range desired {
		desiredSet[subject] = struct{}{}
	}

	if !ml.running {
		return nil
	}
	var reconcileErrors []error
	retainedObsolete := make(map[string]struct{})
	for subject, subscription := range ml.subscriptions {
		if _, keep := desiredSet[subject]; keep {
			continue
		}
		if err := subscription.Unsubscribe(); err != nil {
			ml.logger.Warn("Failed to unsubscribe", "subject", subject, "error", err)
			retainedObsolete[subject] = struct{}{}
			reconcileErrors = append(reconcileErrors, fmt.Errorf("unsubscribe %s: %w", subject, err))
			continue
		}
		delete(ml.subscriptions, subject)
	}
	for _, subject := range desired {
		if _, exists := ml.subscriptions[subject]; exists {
			continue
		}
		if retained, ok := retainedAcceptedOverlap(subject, retainedObsolete); ok {
			err := fmt.Errorf("subscribe %s deferred while overlapping subscription %s remains active", subject, retained)
			ml.logger.Warn("Deferred overlapping subscription", "subject", subject, "retained_subject", retained)
			reconcileErrors = append(reconcileErrors, err)
			continue
		}
		subscription, err := ml.subscribe(ctx, subject, func(msgCtx context.Context, msg *nats.Msg) {
			ml.handleMessage(msgCtx, msg.Subject, msg.Data)
		})
		if err != nil {
			ml.logger.Error("Failed to subscribe to subject", "subject", subject, "error", err)
			reconcileErrors = append(reconcileErrors, fmt.Errorf("subscribe %s: %w", subject, err))
			continue
		}
		ml.subscriptions[subject] = subscription
		ml.logger.Debug("Subscribed to subject", "subject", subject)
	}

	ml.resolvedSubjects = ml.resolvedSubjects[:0]
	for subject := range ml.subscriptions {
		ml.resolvedSubjects = append(ml.resolvedSubjects, subject)
	}
	sort.Strings(ml.resolvedSubjects)
	ml.subjectOverlaps = ml.subjectOverlaps[:0]
	for _, overlap := range overlaps {
		_, broaderActive := ml.subscriptions[overlap.Broader]
		_, coveredActive := ml.subscriptions[overlap.Covered]
		if broaderActive && !coveredActive {
			ml.subjectOverlaps = append(ml.subjectOverlaps, overlap)
		}
	}
	reconcileErr := errors.Join(reconcileErrors...)
	if reconcileErr == nil {
		ml.reconcileError = ""
	} else {
		ml.reconcileError = reconcileErr.Error()
	}

	ml.subjectMu.Lock()
	actualMetadata := make(map[string]portMetadata, len(ml.subscriptions))
	for subject := range ml.subscriptions {
		if current, ok := metadata[subject]; ok {
			actualMetadata[subject] = current
		} else if previous, ok := ml.subjectMetadata[subject]; ok {
			actualMetadata[subject] = previous
		}
	}
	ml.subjectMetadata = actualMetadata
	ml.subjectMu.Unlock()
	return reconcileErr
}

func retainedAcceptedOverlap(subject string, retained map[string]struct{}) (string, bool) {
	for _, pair := range [][2]string{
		{"agent.toolcall.proposed.>", "agent.toolcall.proposed.*"},
		{"agent.toolcall.approved.>", "agent.toolcall.approved.*"},
		{"agent.toolcall.rejected.>", "agent.toolcall.rejected.*"},
	} {
		if subject == pair[0] {
			if _, ok := retained[pair[1]]; ok {
				return pair[1], true
			}
		}
		if subject == pair[1] {
			if _, ok := retained[pair[0]]; ok {
				return pair[0], true
			}
		}
	}
	return "", false
}

func (ml *MessageLogger) runSubscriptionRetry(
	retryCtx context.Context,
	subscriptionCtx context.Context,
	done chan<- struct{},
	desired []string,
	metadata map[string]portMetadata,
	overlaps []subjectOverlap,
) {
	defer close(done)
	for {
		select {
		case <-ml.retryAfter(messageLoggerReconcileRetryDelay):
			if ml.reconcileSubjects(subscriptionCtx, desired, metadata, overlaps) == nil {
				return
			}
		case <-retryCtx.Done():
			return
		}
	}
}

// Start begins message observation
func (ml *MessageLogger) Start(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "MessageLogger", "Start"); err != nil {
		return err
	}
	ml.transitionMu.Lock()
	defer ml.transitionMu.Unlock()

	ml.lifecycleMu.Lock()
	if ml.running {
		ml.lifecycleMu.Unlock()
		return fmt.Errorf("message logger already running")
	}
	if ml.autoDiscover && ml.componentRegistry == nil {
		ml.lifecycleMu.Unlock()
		return fmt.Errorf("message logger wildcard mode requires component registry")
	}
	ml.lifecycleMu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	generation := lifecyclejoin.NewGeneration(cancel, nil)
	if err := ml.BaseService.Start(runCtx); err != nil {
		cancel()
		return err
	}

	// MessageLogger is always enabled when running (managed by Manager)
	ml.logger.Info("MessageLogger starting")
	ml.lifecycleMu.Lock()
	ml.running = true
	ml.generation = generation
	ml.teardownOnce = sync.Once{}
	ml.teardownErr = nil
	ml.lifecycleMu.Unlock()

	subscriptionCtx, subscriptionCancel := context.WithCancel(runCtx)
	ml.lifecycleMu.Lock()
	ml.subscriptionCancel = subscriptionCancel
	ml.lifecycleMu.Unlock()
	desired := ml.explicitSubjects
	metadata := map[string]portMetadata{}
	var overlaps []subjectOverlap
	if ml.autoDiscover {
		metadata, desired, overlaps = ml.registrySubjects()
	}
	retry := ml.reconcileSubjects(subscriptionCtx, desired, metadata, overlaps) != nil
	if retry {
		retryCtx, retryCancel := context.WithCancel(runCtx)
		done := make(chan struct{})
		ml.lifecycleMu.Lock()
		ml.retryCancel = retryCancel
		ml.retryDone = done
		ml.lifecycleMu.Unlock()
		go func() {
			defer retryCancel()
			ml.runSubscriptionRetry(retryCtx, subscriptionCtx, done, desired, metadata, overlaps)
		}()
	}

	subjects, _ := ml.subjectInspection()
	ml.logger.Info("MessageLogger started",
		"monitored_subjects", len(subjects),
		"max_entries", ml.config.MaxEntries,
		"output_to_stdout", ml.config.OutputToStdout)

	return nil
}

// Stop gracefully stops the MessageLogger
func (ml *MessageLogger) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "MessageLogger", "Stop"); err != nil {
		return err
	}
	ml.transitionMu.Lock()
	ml.lifecycleMu.Lock()
	generation := ml.generation
	ml.lifecycleMu.Unlock()
	ml.transitionMu.Unlock()
	if generation == nil {
		return nil
	}

	return generation.Stop(ctx, nil, func(ctx context.Context) error {
		ml.lifecycleMu.Lock()
		ml.running = false
		subscriptionCancel := ml.subscriptionCancel
		retryCancel := ml.retryCancel
		retryDone := ml.retryDone
		ml.lifecycleMu.Unlock()

		if retryCancel != nil {
			retryCancel()
		}
		if subscriptionCancel != nil {
			subscriptionCancel()
		}
		var waitErrors []error
		if err := waitForMessageLoggerShutdown(ctx, retryDone, "reconciliation retry"); err != nil {
			waitErrors = append(waitErrors, err)
		}
		if waitErr := errors.Join(waitErrors...); waitErr != nil {
			return waitErr
		}

		ml.teardownOnce.Do(func() {
			ml.lifecycleMu.Lock()
			defer ml.lifecycleMu.Unlock()
			retainedSubjects := make(map[string]struct{})
			var teardownErrors []error
			for subject, subscription := range ml.subscriptions {
				if err := subscription.Unsubscribe(); err != nil {
					ml.logger.Warn("Failed to unsubscribe", "subject", subject, "error", err)
					teardownErrors = append(teardownErrors, fmt.Errorf("unsubscribe %s: %w", subject, err))
					retainedSubjects[subject] = struct{}{}
					continue
				}
				delete(ml.subscriptions, subject)
			}
			ml.resolvedSubjects = ml.resolvedSubjects[:0]
			for subject := range ml.subscriptions {
				ml.resolvedSubjects = append(ml.resolvedSubjects, subject)
			}
			sort.Strings(ml.resolvedSubjects)
			ml.subjectOverlaps = nil
			ml.teardownErr = errors.Join(teardownErrors...)
			if ml.teardownErr == nil {
				ml.reconcileError = ""
			} else {
				ml.reconcileError = ml.teardownErr.Error()
			}
			ml.subjectMu.Lock()
			for subject := range ml.subjectMetadata {
				if _, retained := retainedSubjects[subject]; !retained {
					delete(ml.subjectMetadata, subject)
				}
			}
			ml.subjectMu.Unlock()
		})

		baseErr := ml.BaseService.Stop(ctx)
		if ctx.Err() != nil && errors.Is(baseErr, ctx.Err()) {
			return errors.Join(ml.teardownErr, baseErr)
		}
		ml.lifecycleMu.Lock()
		ml.subscriptionCancel = nil
		ml.retryCancel = nil
		ml.retryDone = nil
		ml.lifecycleMu.Unlock()
		ml.logger.Info("MessageLogger stopped")
		return errors.Join(ml.teardownErr, baseErr)
	})
}

func waitForMessageLoggerShutdown(ctx context.Context, done <-chan struct{}, name string) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for %s shutdown: %w", name, ctx.Err())
	}
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
	ml.subjectMu.RLock()
	meta, hasMetadata := ml.subjectMetadata[subject]
	ml.subjectMu.RUnlock()
	if hasMetadata {
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
	subjects, overlaps, reconcileError := ml.subjectInspectionState()

	return map[string]any{
		"total_messages":                  ml.stats.totalMessages.Load(),
		"sampled_messages":                ml.stats.sampledMessages.Load(),
		"valid_messages":                  ml.stats.validMessages.Load(),
		"invalid_messages":                ml.stats.invalidMessages.Load(),
		"sample_rate":                     ml.sampleRate,
		"start_time":                      ml.stats.startTime,
		"last_message_time":               lastMessageTime,
		"uptime_seconds":                  time.Since(ml.stats.startTime).Seconds(),
		"monitored_subjects":              subjects,
		"subject_overlaps":                overlaps,
		"subject_reconciliation_degraded": reconcileError != "",
		"subject_reconciliation_error":    reconcileError,
		"max_entries":                     ml.config.MaxEntries,
	}
}

func (ml *MessageLogger) subjectInspection() ([]string, []subjectOverlap) {
	subjects, overlaps, _ := ml.subjectInspectionState()
	return subjects, overlaps
}

func (ml *MessageLogger) subjectInspectionState() ([]string, []subjectOverlap, string) {
	ml.lifecycleMu.Lock()
	defer ml.lifecycleMu.Unlock()
	subjects := append([]string(nil), ml.resolvedSubjects...)
	overlaps := append([]subjectOverlap(nil), ml.subjectOverlaps...)
	return subjects, overlaps, ml.reconcileError
}

// ConfigSchema returns the configuration schema for this service.
// This implements the Configurable interface for UI discovery.
func (ml *MessageLogger) ConfigSchema() ConfigSchema {
	return NewConfigSchema(map[string]PropertySchema{
		"monitor_subjects": {
			PropertySchema: component.PropertySchema{
				Type:        "array",
				Description: "NATS subjects to monitor; '*' discovers accepted Registry declarations and explicit subjects are unioned",
				Default:     []string{"*"},
			},
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
			Category: "storage",
		},
		"output_to_stdout": {
			PropertySchema: component.PropertySchema{
				Type:        "bool",
				Description: "Whether to output messages to stdout",
				Default:     false,
			},
			Category: "output",
		},
		"sample_rate": {
			PropertySchema: component.PropertySchema{
				Type:        "integer",
				Description: "Capture one in every N accepted messages",
				Default:     1,
				Minimum:     intPtr(1),
			},
			Category: "monitoring",
		},
	}, []string{}) // No required fields - all have defaults
}
