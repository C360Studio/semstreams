package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go/jetstream"
)

// StatusStreamEnvelope wraps all WebSocket status stream messages
type StatusStreamEnvelope struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp int64           `json:"timestamp"`
	FlowID    string          `json:"flow_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// SubscribeCommand represents a client subscription command (Client → Server)
type SubscribeCommand struct {
	Command      string   `json:"command"`                 // Must be "subscribe"
	MessageTypes []string `json:"message_types,omitempty"` // Filter: flow_status, component_health, component_metrics, log_entry
	LogLevel     string   `json:"log_level,omitempty"`     // Minimum log level: DEBUG, INFO, WARN, ERROR
	Sources      []string `json:"sources,omitempty"`       // Filter by source component names
}

// SubscribeAck is the acknowledgment response sent after processing a subscribe command (Server → Client)
type SubscribeAck struct {
	Type       string   `json:"type"`                // Always "subscribe_ack"
	Subscribed []string `json:"subscribed"`          // Message types now subscribed
	LogLevel   string   `json:"log_level,omitempty"` // Current log level filter (empty = all)
	Sources    []string `json:"sources,omitempty"`   // Current source filters (empty = all)
}

// ErrorResponse is sent to client when a command fails (Server → Client)
type ErrorResponse struct {
	Type    string `json:"type"`    // Always "error"
	Code    string `json:"code"`    // Error code: "invalid_json", "unknown_command", "missing_command"
	Message string `json:"message"` // Human-readable error message
}

// FlowStatusPayload is the payload for type=flow_status messages
type FlowStatusPayload struct {
	State     string `json:"state"`           // Current state: draft, deployed, running, stopped, failed
	PrevState string `json:"prev_state"`      // Previous state (if changed)
	Timestamp int64  `json:"timestamp"`       // State change timestamp (Unix milliseconds)
	Error     string `json:"error,omitempty"` // Error message if state=failed
}

// LogEntryPayload is the payload for type=log_entry messages
type LogEntryPayload struct {
	Level   string         `json:"level"`   // DEBUG, INFO, WARN, ERROR
	Source  string         `json:"source"`  // Component or service name
	Message string         `json:"message"` // Log message
	Fields  map[string]any `json:"fields"`  // Structured log fields
}

// MetricsPayload is the payload for type=component_metrics messages
type MetricsPayload struct {
	Component string        `json:"component"` // Component name
	Metrics   []MetricEntry `json:"metrics"`   // Array of metric values
}

// MetricEntry represents a single metric in a MetricsPayload
type MetricEntry struct {
	Name   string            `json:"name"`   // Metric name
	Type   string            `json:"type"`   // counter, gauge, histogram
	Value  float64           `json:"value"`  // Current value
	Labels map[string]string `json:"labels"` // Metric labels
}

// ClientState represents client subscription state
type ClientState struct {
	messageTypes map[string]bool
	logLevel     string
	sources      map[string]bool
	mu           sync.RWMutex
}

// newClientState creates a new client state with default subscriptions
func newClientState() *ClientState {
	// Default: subscribe to all message types
	return &ClientState{
		messageTypes: map[string]bool{
			"flow_status":       true,
			"component_health":  true,
			"component_metrics": true,
			"log_entry":         true,
		},
		logLevel: "",
		sources:  make(map[string]bool),
	}
}

// IsSubscribed checks if the client is subscribed to a message type
func (cs *ClientState) IsSubscribed(messageType string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.messageTypes[messageType]
}

// UpdateSubscription updates the client's subscription filters
// Empty arrays are treated as "keep current subscriptions" rather than "unsubscribe all"
func (cs *ClientState) UpdateSubscription(messageTypes []string, logLevel string, sources []string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Update message types only if non-empty array provided
	// Empty array means "keep current subscriptions"
	if len(messageTypes) > 0 {
		cs.messageTypes = make(map[string]bool)
		for _, mt := range messageTypes {
			cs.messageTypes[mt] = true
		}
	}

	// Update log level only if provided (non-empty)
	if logLevel != "" {
		cs.logLevel = logLevel
	}

	// Update sources only if non-empty array provided
	// Empty array means "keep current sources"
	if len(sources) > 0 {
		cs.sources = make(map[string]bool)
		for _, src := range sources {
			cs.sources[src] = true
		}
	}
}

// ShouldReceiveLogLevel checks if the client should receive logs at the given level
// Log level hierarchy: DEBUG=0 < INFO=1 < WARN=2 < ERROR=3
func (cs *ClientState) ShouldReceiveLogLevel(level string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// No filter = allow all
	if cs.logLevel == "" {
		return true
	}

	// Define level hierarchy
	levelOrder := map[string]int{
		"DEBUG": 0,
		"INFO":  1,
		"WARN":  2,
		"ERROR": 3,
	}

	minLevelValue, minExists := levelOrder[cs.logLevel]
	testLevelValue, testExists := levelOrder[level]

	if !minExists || !testExists {
		return true // Unknown levels pass through
	}

	return testLevelValue >= minLevelValue
}

// ShouldReceiveSource checks if the client should receive logs from the given source
func (cs *ClientState) ShouldReceiveSource(source string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// No filter = allow all
	if len(cs.sources) == 0 {
		return true
	}

	return cs.sources[source]
}

// GetSubscribedTypes returns the list of currently subscribed message types
func (cs *ClientState) GetSubscribedTypes() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	types := make([]string, 0, len(cs.messageTypes))
	for mt, subscribed := range cs.messageTypes {
		if subscribed {
			types = append(types, mt)
		}
	}
	return types
}

// GetLogLevel returns the current log level filter
func (cs *ClientState) GetLogLevel() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.logLevel
}

// GetSources returns the current source filters
func (cs *ClientState) GetSources() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	sources := make([]string, 0, len(cs.sources))
	for src, filtered := range cs.sources {
		if filtered {
			sources = append(sources, src)
		}
	}
	return sources
}

// handleStatusWebSocketImpl is the actual implementation of WebSocket status streaming
// This replaces the stub in flow_service.go
func (fs *FlowService) handleStatusWebSocketImpl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get flowId from query parameter
	flowID := r.URL.Query().Get("flowId")

	// Validate flowId (required)
	if flowID == "" {
		fs.logger.Warn("WebSocket upgrade failed: missing flowId parameter")
		http.Error(w, "Missing flowId parameter", http.StatusBadRequest)
		return
	}

	// Validate flowId to prevent NATS injection (same as runtime logs endpoint)
	if strings.ContainsAny(flowID, ">*.") {
		fs.logger.Warn("WebSocket upgrade failed: invalid flowId (NATS injection attempt)", "flow_id", flowID)
		http.Error(w, "Invalid flowId", http.StatusBadRequest)
		return
	}

	// Verify flow exists
	_, err := fs.flowStore.Get(ctx, flowID)
	if err != nil {
		fs.logger.Error("WebSocket upgrade failed: flow not found", "flow_id", flowID, "error", err)
		http.Error(w, "Flow not found", http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool {
			// Allow connections from any origin for development
			// In production, this should be more restrictive
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fs.logger.Error("WebSocket upgrade failed", "flow_id", flowID, "error", err)
		return
	}

	fs.logger.Info("WebSocket client connected", "flow_id", flowID, "remote_addr", r.RemoteAddr)

	// Handle the WebSocket connection
	defer conn.Close()

	// Create client state for this connection
	clientState := newClientState()

	// Create context for worker goroutines
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	// Create channel for sending envelopes to the client
	envelopeChan := make(chan StatusStreamEnvelope, 100)

	// Start workers and goroutines
	var wg sync.WaitGroup
	fs.startWebSocketWorkers(workerCtx, clientState, flowID, envelopeChan, &wg)
	fs.startWebSocketWriter(workerCtx, conn, envelopeChan, &wg)
	fs.startWebSocketReader(workerCtx, conn, clientState, workerCancel, &wg)

	// Wait for workers to finish (on disconnect or error)
	wg.Wait()
	fs.logger.Info("WebSocket client disconnected", "flow_id", flowID)
}

// natsSubscriber interface for NATS pub/sub operations.
// WebSocket streamers use internal JetStream consumers to receive
// observability data that is published via PublishToStream to JetStream streams.
type natsSubscriber interface {
	// ConsumeInternalStreamWithConfig creates a non-port consumer for receiving stream messages.
	// Used by WebSocket streamers to consume logs, health, metrics, and flow status.
	ConsumeInternalStreamWithConfig(ctx context.Context, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) error

	// PublishToStream publishes data to a JetStream stream.
	// Used for testing to inject messages into streams.
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// generateMessageID generates a unique message ID for envelopes
func generateMessageID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(bytes)
}

// healthStreamer consumes from HEALTH JetStream and forwards health updates as envelopes.
// Health data is published by ComponentManager and ServiceManager to health.component.{name}
// and health.service.{name} respectively.
func healthStreamer(
	ctx context.Context,
	clientState *ClientState,
	natsClient natsSubscriber,
	flowID string,
	sendFn func(StatusStreamEnvelope) error,
	logger *slog.Logger,
	autoCreate *natsclient.StreamAutoCreateConfig,
) {
	// Configure JetStream consumer for HEALTH stream
	cfg := natsclient.StreamConsumerConfig{
		StreamName:       "HEALTH",
		FilterSubject:    "health.>",
		DeliverPolicy:    "last_per_subject", // Last message per subject on connect, then new
		AckPolicy:        "none",             // Fire-and-forget to browser
		AutoCreate:       true,               // Recreate the stream if the server lost it
		AutoCreateConfig: autoCreate,
	}

	err := natsClient.ConsumeInternalStreamWithConfig(ctx, cfg, func(_ context.Context, msg jetstream.Msg) {
		// Check if client is subscribed to component_health
		if !clientState.IsSubscribed("component_health") {
			return
		}

		// Create envelope with the health data as payload
		envelope := StatusStreamEnvelope{
			Type:      "component_health",
			ID:        generateMessageID(),
			Timestamp: time.Now().UnixMilli(),
			FlowID:    flowID,
			Payload:   json.RawMessage(msg.Data()),
		}

		// Send envelope (ignore error - context cancelled means connection closed)
		_ = sendFn(envelope)
	})

	if err != nil {
		logger.Error("Failed to consume from HEALTH stream", "error", err)
		return
	}

	// Wait for context cancellation
	<-ctx.Done()
}

// flowStatusStreamer consumes from FLOWS JetStream and forwards status updates as envelopes.
// Flow status data is published by FlowService when the flow KV bucket is updated.
func flowStatusStreamer(
	ctx context.Context,
	clientState *ClientState,
	natsClient natsSubscriber,
	flowID string,
	sendFn func(StatusStreamEnvelope) error,
	logger *slog.Logger,
	autoCreate *natsclient.StreamAutoCreateConfig,
) {
	// Configure JetStream consumer for FLOWS stream, filtered to this specific flow
	subject := "flows." + flowID + ".status"
	cfg := natsclient.StreamConsumerConfig{
		StreamName:       "FLOWS",
		FilterSubject:    subject,
		DeliverPolicy:    "last_per_subject", // Last message per subject on connect, then new
		AckPolicy:        "none",             // Fire-and-forget to browser
		AutoCreate:       true,               // Recreate the stream if the server lost it
		AutoCreateConfig: autoCreate,
	}

	err := natsClient.ConsumeInternalStreamWithConfig(ctx, cfg, func(_ context.Context, msg jetstream.Msg) {
		// Check if client is subscribed to flow_status
		if !clientState.IsSubscribed("flow_status") {
			return
		}

		// Create envelope with the flow status data as payload
		envelope := StatusStreamEnvelope{
			Type:      "flow_status",
			ID:        generateMessageID(),
			Timestamp: time.Now().UnixMilli(),
			FlowID:    flowID,
			Payload:   json.RawMessage(msg.Data()),
		}

		// Send envelope (ignore error - context cancelled means connection closed)
		_ = sendFn(envelope)
	})

	if err != nil {
		logger.Error("Failed to consume from FLOWS stream", "flow_id", flowID, "error", err)
		return
	}

	// Wait for context cancellation
	<-ctx.Done()
}

// logStreamer consumes from LOGS JetStream and forwards log entries as envelopes
func logStreamer(
	ctx context.Context,
	clientState *ClientState,
	natsClient natsSubscriber,
	flowID string,
	sendFn func(StatusStreamEnvelope) error,
	logger *slog.Logger,
	autoCreate *natsclient.StreamAutoCreateConfig,
) {
	// Configure JetStream consumer for LOGS stream
	cfg := natsclient.StreamConsumerConfig{
		StreamName:       "LOGS",
		FilterSubject:    "logs.>",
		DeliverPolicy:    "last_per_subject", // Last message per subject on connect, then new
		AckPolicy:        "none",             // Fire-and-forget to browser
		AutoCreate:       true,               // Recreate the stream if the server lost it
		AutoCreateConfig: autoCreate,
	}

	err := natsClient.ConsumeInternalStreamWithConfig(ctx, cfg, func(_ context.Context, msg jetstream.Msg) {
		// Check if client is subscribed to log_entry
		if !clientState.IsSubscribed("log_entry") {
			return
		}

		data := msg.Data()

		// Parse log entry
		var logEntry map[string]interface{}
		if err := json.Unmarshal(data, &logEntry); err != nil {
			logger.Debug("Failed to unmarshal log entry", "error", err)
			return
		}

		// Extract level and source
		level, _ := logEntry["level"].(string)
		source, _ := logEntry["source"].(string)

		// Check log level filter
		if !clientState.ShouldReceiveLogLevel(level) {
			return
		}

		// Check source filter
		if !clientState.ShouldReceiveSource(source) {
			return
		}

		// Create envelope
		envelope := StatusStreamEnvelope{
			Type:      "log_entry",
			ID:        generateMessageID(),
			Timestamp: time.Now().UnixMilli(),
			FlowID:    flowID,
			Payload:   json.RawMessage(data),
		}

		// Send envelope (ignore error - context cancelled means connection closed)
		_ = sendFn(envelope)
	})

	if err != nil {
		logger.Error("Failed to consume from LOGS stream", "error", err)
		return
	}

	// Wait for context cancellation
	<-ctx.Done()
}

// metricsStreamer consumes from METRICS JetStream and forwards metrics as envelopes
func metricsStreamer(
	ctx context.Context,
	clientState *ClientState,
	natsClient natsSubscriber,
	flowID string,
	sendFn func(StatusStreamEnvelope) error,
	logger *slog.Logger,
	autoCreate *natsclient.StreamAutoCreateConfig,
) {
	// Configure JetStream consumer for METRICS stream
	cfg := natsclient.StreamConsumerConfig{
		StreamName:       "METRICS",
		FilterSubject:    "metrics.>",
		DeliverPolicy:    "last_per_subject", // Last message per subject on connect, then new
		AckPolicy:        "none",             // Fire-and-forget to browser
		AutoCreate:       true,               // Recreate the stream if the server lost it
		AutoCreateConfig: autoCreate,
	}

	err := natsClient.ConsumeInternalStreamWithConfig(ctx, cfg, func(_ context.Context, msg jetstream.Msg) {
		// Check if client is subscribed to component_metrics
		if !clientState.IsSubscribed("component_metrics") {
			return
		}

		// Create envelope
		envelope := StatusStreamEnvelope{
			Type:      "component_metrics",
			ID:        generateMessageID(),
			Timestamp: time.Now().UnixMilli(),
			FlowID:    flowID,
			Payload:   json.RawMessage(msg.Data()),
		}

		// Send envelope (ignore error - context cancelled means connection closed)
		_ = sendFn(envelope)
	})

	if err != nil {
		logger.Error("Failed to consume from METRICS stream", "error", err)
		return
	}

	// Wait for context cancellation
	<-ctx.Done()
}

// startWebSocketWorkers starts all worker goroutines for the WebSocket connection
func (fs *FlowService) startWebSocketWorkers(
	ctx context.Context,
	clientState *ClientState,
	flowID string,
	envelopeChan chan StatusStreamEnvelope,
	wg *sync.WaitGroup,
) {
	// Create a tagged logger for WebSocket workers.
	// This uses the dotted notation convention so these logs can be excluded
	// from NATS forwarding via LogForwarder's exclude_sources config.
	wsLogger := fs.logger.With("source", "flow-service.websocket")

	// Create send function that writes to the envelope channel
	sendFn := func(envelope StatusStreamEnvelope) error {
		select {
		case envelopeChan <- envelope:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Channel full, drop message to avoid blocking workers
			wsLogger.Debug("Envelope channel full, dropping message", "type", envelope.Type)
			return nil
		}
	}

	// All workers subscribe to NATS streams - no direct service dependencies
	if fs.natsClient == nil {
		wsLogger.Warn("NATS client not available, WebSocket workers disabled")
		return
	}

	// Health streamer - subscribes to NATS health.>
	wg.Add(1)
	go func() {
		defer wg.Done()
		healthStreamer(ctx, clientState, fs.natsClient, flowID, sendFn, wsLogger, fs.frameworkAutoCreate("HEALTH"))
	}()

	// Flow status streamer - subscribes to NATS flows.{flowId}.status
	wg.Add(1)
	go func() {
		defer wg.Done()
		flowStatusStreamer(ctx, clientState, fs.natsClient, flowID, sendFn, wsLogger, fs.frameworkAutoCreate("FLOWS"))
	}()

	// Log streamer - subscribes to NATS logs.>
	wg.Add(1)
	go func() {
		defer wg.Done()
		logStreamer(ctx, clientState, fs.natsClient, flowID, sendFn, wsLogger, fs.frameworkAutoCreate("LOGS"))
	}()

	// Metrics streamer - subscribes to NATS metrics.>
	wg.Add(1)
	go func() {
		defer wg.Done()
		metricsStreamer(ctx, clientState, fs.natsClient, flowID, sendFn, wsLogger, fs.frameworkAutoCreate("METRICS"))
	}()
}

// startWebSocketWriter starts the goroutine that writes envelopes to the WebSocket
func (fs *FlowService) startWebSocketWriter(
	ctx context.Context,
	conn *websocket.Conn,
	envelopeChan chan StatusStreamEnvelope,
	wg *sync.WaitGroup,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case envelope := <-envelopeChan:
				// Marshal envelope to JSON
				data, err := json.Marshal(envelope)
				if err != nil {
					fs.logger.Debug("Failed to marshal envelope", "error", err)
					continue
				}

				// Write to WebSocket
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					fs.logger.Debug("Failed to write to WebSocket", "error", err)
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// startWebSocketReader starts the goroutine that reads from the WebSocket
func (fs *FlowService) startWebSocketReader(
	ctx context.Context,
	conn *websocket.Conn,
	clientState *ClientState,
	cancelFunc context.CancelFunc,
	wg *sync.WaitGroup,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Monitor context cancellation - close connection to unblock ReadMessage
		go func() {
			<-ctx.Done()
			conn.Close()
		}()

		// Create send function for ACK responses
		sendFn := func(v any) error {
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			return conn.WriteMessage(websocket.TextMessage, data)
		}

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				// Check if this was a context cancellation
				if ctx.Err() != nil {
					fs.logger.Debug("WebSocket reader stopped due to context cancellation")
					return
				}
				// Client disconnected or error
				fs.logger.Debug("WebSocket read error, closing connection", "error", err)
				cancelFunc()
				return
			}

			// Handle client command
			fs.handleWebSocketCommand(message, clientState, sendFn)
		}
	}()
}

// handleWebSocketCommand processes a client command from the WebSocket
func (fs *FlowService) handleWebSocketCommand(message []byte, clientState *ClientState, sendFn func(any) error) {
	// Parse the command
	var cmd SubscribeCommand
	if err := json.Unmarshal(message, &cmd); err != nil {
		// Malformed JSON - send error to client
		_ = sendFn(ErrorResponse{
			Type:    "error",
			Code:    "invalid_json",
			Message: "Failed to parse command: " + err.Error(),
		})
		fs.logger.Debug("WebSocket received malformed JSON command", "error", err)
		return
	}

	// Require command field
	if cmd.Command == "" {
		_ = sendFn(ErrorResponse{
			Type:    "error",
			Code:    "missing_command",
			Message: "Command field is required",
		})
		return
	}

	// Handle subscribe command
	if cmd.Command == "subscribe" {
		clientState.UpdateSubscription(cmd.MessageTypes, cmd.LogLevel, cmd.Sources)

		// Send acknowledgment with current subscription state
		ack := SubscribeAck{
			Type:       "subscribe_ack",
			Subscribed: clientState.GetSubscribedTypes(),
			LogLevel:   clientState.GetLogLevel(),
			Sources:    clientState.GetSources(),
		}
		if err := sendFn(ack); err != nil {
			fs.logger.Debug("Failed to send subscribe_ack", "error", err)
		}

		fs.logger.Debug("WebSocket client updated subscription",
			"message_types", cmd.MessageTypes,
			"log_level", cmd.LogLevel,
			"sources", cmd.Sources)
		return
	}

	// Unknown command - send error to client
	_ = sendFn(ErrorResponse{
		Type:    "error",
		Code:    "unknown_command",
		Message: "Unknown command: " + cmd.Command,
	})
	fs.logger.Debug("WebSocket received unknown command", "command", cmd.Command)
}

// frameworkAutoCreate returns the EFFECTIVE auto-create configuration for one of
// the framework's guaranteed streams, read from the LIVE configuration.
//
// These dashboard streamers read HEALTH, METRICS, FLOWS and LOGS, all of which the
// framework provisioner declares with reasoned bounds (config/streams.go). They
// used to hand the auto-create path a config naming only subjects and storage,
// which produced no age limit and no size limit — so on the path that actually
// runs, a NATS restart destroying the memory-backed streams followed by a
// websocket reconnect replaced the declared 5m/10MB with an unbounded stream. The
// framework's own observability streams were the counterexample to the bounds
// requirement the framework enforces.
//
// It reads the live configuration rather than the framework constants because
// `cfg.streams` is the highest-priority declaration: an operator may override any
// of these streams' storage, bounds or discard policy there, and recreating one
// from the built-in values would discard their override at exactly the moment the
// stream is being rebuilt. That is the same quiet replacement this helper exists to
// stop, arrived at from the other direction.
//
// A nil return is deliberate rather than a fallback: auto-create then declares
// nothing, the seam's bounds check refuses the creation, and the failure names the
// stream — which is the correct outcome for a stream the configuration does not
// declare or does not currently resolve, and is far better than inventing bounds.
func (fs *FlowService) frameworkAutoCreate(stream string) *natsclient.StreamAutoCreateConfig {
	// A nil configuration manager passes nil through deliberately: the accessor
	// reads that as "the framework declaration, unmodified", which is what a
	// deployment without operator configuration actually has. Declining here would
	// disable stream recovery for the dashboard entirely.
	var live *config.Config
	if fs.configMgr != nil {
		if safe := fs.configMgr.GetConfig(); safe != nil {
			live = safe.Get()
		}
	}
	declared, ok := config.FrameworkStreamAutoCreate(live, stream)
	if !ok {
		return nil
	}
	return declared
}
