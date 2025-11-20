// Package websocket provides WebSocket output component for sending data to external systems
package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/buffer"
	"github.com/c360/semstreams/pkg/security"
	"github.com/c360/semstreams/pkg/tlsutil"
	"github.com/gorilla/websocket"
	natspkg "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

// DeliveryMode defines the reliability semantics for message delivery
type DeliveryMode string

const (
	// DeliveryAtMostOnce sends messages without waiting for ack (fire-and-forget)
	DeliveryAtMostOnce DeliveryMode = "at-most-once"
	// DeliveryAtLeastOnce waits for ack and retries on failure
	DeliveryAtLeastOnce DeliveryMode = "at-least-once"
)

// Config holds configuration for WebSocket output component
type Config struct {
	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports"                   schema:"type:ports,description:Port configuration,category:basic"`
	// DeliveryMode specifies reliability semantics (at-most-once or at-least-once)
	DeliveryMode DeliveryMode `json:"delivery_mode,omitempty" schema:"type:string,description:Delivery reliability mode,category:advanced"`
	// AckTimeout specifies how long to wait for ack before considering message lost
	AckTimeout string `json:"ack_timeout,omitempty"   schema:"type:string,description:Acknowledgment timeout (e.g. 5s),category:advanced"`
}

// ConstructorConfig holds all configuration needed to construct an Output instance
type ConstructorConfig struct {
	Name            string                  // Component name (empty = auto-generate)
	Port            int                     // WebSocket server port
	Path            string                  // WebSocket endpoint path
	Subjects        []string                // NATS subjects to subscribe to
	NATSClient      *natsclient.Client      // NATS client for messaging
	MetricsRegistry *metric.MetricsRegistry // Optional Prometheus metrics registry
	Security        security.Config         // Security configuration
	DeliveryMode    DeliveryMode            // Reliability semantics
	AckTimeout      time.Duration           // Acknowledgment timeout for at-least-once
}

// DefaultConstructorConfig returns sensible defaults for Output construction
func DefaultConstructorConfig() ConstructorConfig {
	return ConstructorConfig{
		Name:         "",
		Port:         8081,
		Path:         "/ws",
		Subjects:     []string{"semantic.>"},
		Security:     security.Config{},
		DeliveryMode: DeliveryAtMostOnce,
		AckTimeout:   5 * time.Second,
	}
}

// DefaultConfig returns the default configuration for WebSocket output
func DefaultConfig() Config {
	// WebSocket output typically has:
	// - Input: NATS subjects to listen to
	// - Output: WebSocket server network port (encoded in Subject field)
	inputDefs := []component.PortDefinition{
		{
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "semantic.>", // Default to semantic events
			Required:    true,
			Description: "NATS subjects to listen to",
		},
	}

	// For network ports, we encode the URL in Subject field for now
	// This matches how the factory extracts config
	outputDefs := []component.PortDefinition{
		{
			Name:        "websocket_server",
			Type:        "network",
			Subject:     "http://0.0.0.0:8081/ws", // Encoded as URL
			Required:    false,
			Description: "WebSocket server endpoint",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
	}
}

// websocketSchema defines the configuration schema for WebSocket output component
// Generated from Config struct tags using reflection
var websocketSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Output implements a WebSocket server that broadcasts NATS messages to connected clients
// This is designed for real-time visualization of graph updates and entity state changes
type Output struct {
	name         string
	port         int
	path         string
	subjects     []string
	natsClient   *natsclient.Client
	security     security.Config
	deliveryMode DeliveryMode
	ackTimeout   time.Duration

	// WebSocket server
	server    *http.Server
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]*clientInfo
	clientsMu sync.RWMutex

	// NATS subscriptions are managed by natsclient wrapper

	// Lifecycle management
	shutdown      chan struct{} // Signal shutdown to all goroutines
	done          chan struct{} // Signal completion of shutdown
	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex      // Ensures Start/Stop operations are serialized
	wg            *sync.WaitGroup // Track goroutines for safe shutdown (pointer for replacement)
	tlsCleanup    func()          // ACME cleanup function (stops renewal loop)
	tlsCleanupMu  sync.Mutex      // Protects tlsCleanup
	lifecycleCtx  context.Context // Context for lifecycle operations (ACME, etc.)
	lifecycleStop context.CancelFunc

	// Message ID generation
	messageIDCounter atomic.Uint64

	// Metrics
	messagesSent int64
	bytesSent    int64
	errors       int64
	lastActivity time.Time

	// Prometheus metrics
	metrics *Metrics
}

// MessageEnvelope wraps all WebSocket messages with type discrimination
// This matches the protocol defined in input/websocket_input
// Supported types:
//   - "data": Application data from NATS
//   - "ack": Acknowledge successful receipt/processing of data message
//   - "nack": Negative acknowledgment (processing failed, may retry)
//   - "slow": Backpressure signal indicating receiver is overloaded
type MessageEnvelope struct {
	Type      string          `json:"type"`              // Message type
	ID        string          `json:"id"`                // Unique message ID (for correlation)
	Timestamp int64           `json:"timestamp"`         // Unix milliseconds
	Payload   json.RawMessage `json:"payload,omitempty"` // Optional payload
}

// PendingMessage represents a message awaiting acknowledgment
type PendingMessage struct {
	ID      string    // Unique message ID for correlation
	Data    []byte    // JSON message data (with envelope)
	Subject string    // NATS subject
	SentAt  time.Time // When message was sent
	Retries int       // Number of retry attempts
	AckChan chan bool // Signal channel for ack/nack (true=ack, false=nack)
}

// clientInfo holds information about a connected WebSocket client
type clientInfo struct {
	conn            *websocket.Conn
	connectedAt     time.Time
	messagesSent    int64
	lastPing        atomic.Value // stores time.Time
	closed          atomic.Bool
	closeOnce       sync.Once
	writeMutex      sync.Mutex                     // Protects concurrent writes to the same connection
	pendingBuffer   buffer.Buffer[*PendingMessage] // Buffer for messages awaiting ack
	pendingMessages map[string]*PendingMessage     // Map of message ID -> pending message for ack tracking
	pendingMu       sync.RWMutex                   // Protects pendingMessages map
}

// Ensure Output implements all required interfaces
var _ component.Discoverable = (*Output)(nil)
var _ component.LifecycleComponent = (*Output)(nil)

// Metrics holds Prometheus metrics for Output component
type Metrics struct {
	messagesReceived    *prometheus.CounterVec
	messagesSent        *prometheus.CounterVec
	bytesSent           prometheus.Counter
	clientsConnected    prometheus.Gauge
	connectionTotal     prometheus.Counter
	disconnectionTotal  *prometheus.CounterVec
	broadcastDuration   *prometheus.HistogramVec
	messageSizeBytes    *prometheus.HistogramVec
	errorsTotal         *prometheus.CounterVec
	serverUptimeSeconds prometheus.Gauge
}

// newMetrics creates and registers Output metrics
func newMetrics(registry *metric.MetricsRegistry, _ string) *Metrics {
	// Return nil if no registry provided (nil input = nil feature pattern)
	if registry == nil {
		return nil
	}

	// Only create metrics when registry is provided
	metrics := &Metrics{
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "messages_received_total",
			Help:      "Total messages received from NATS",
		}, []string{"subject"}),

		messagesSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "messages_sent_total",
			Help:      "Total messages sent to WebSocket clients",
		}, []string{"subject"}),

		bytesSent: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "bytes_sent_total",
			Help:      "Total bytes sent to WebSocket clients",
		}),

		clientsConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "clients_connected",
			Help:      "Number of currently connected clients",
		}),

		connectionTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "client_connections_total",
			Help:      "Total client connections (including disconnected)",
		}),

		disconnectionTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "client_disconnections_total",
			Help:      "Total client disconnections",
		}, []string{"disconnect_reason"}),

		broadcastDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "broadcast_duration_seconds",
			Help:      "Time to broadcast message to all clients",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		}, []string{"subject"}),

		messageSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "message_size_bytes",
			Help:      "Size distribution of outgoing messages",
			Buckets:   []float64{100, 500, 1000, 2000, 5000, 10000, 25000},
		}, []string{"subject"}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "errors_total",
			Help:      "WebSocket server errors",
		}, []string{"error_type"}),

		serverUptimeSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "websocket",
			Name:      "server_uptime_seconds",
			Help:      "WebSocket server uptime in seconds",
		}),
	}

	// Register all metrics (no conditional needed since metrics only exist with registry)
	registry.PrometheusRegistry().MustRegister(
		metrics.messagesReceived,
		metrics.messagesSent,
		metrics.bytesSent,
		metrics.clientsConnected,
		metrics.connectionTotal,
		metrics.disconnectionTotal,
		metrics.broadcastDuration,
		metrics.messageSizeBytes,
		metrics.errorsTotal,
		metrics.serverUptimeSeconds,
	)

	return metrics
}

// NewOutput creates a new WebSocket output component with minimal configuration.
// For more control over configuration, use NewOutputFromConfig().
func NewOutput(port int, path string, subjects []string, natsClient *natsclient.Client) *Output {
	cfg := DefaultConstructorConfig()
	cfg.Port = port
	cfg.Path = path
	cfg.Subjects = subjects
	cfg.NATSClient = natsClient
	return NewOutputFromConfig(cfg)
}

// NewOutputFromConfig creates a new WebSocket output component from ConstructorConfig.
// This is the recommended way to create Output instances with full configuration control.
func NewOutputFromConfig(cfg ConstructorConfig) *Output {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool {
			// Allow connections from any origin for development
			// In production, this should be more restrictive
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	return &Output{
		name:         cfg.Name,
		port:         cfg.Port,
		path:         cfg.Path,
		subjects:     cfg.Subjects,
		natsClient:   cfg.NATSClient,
		security:     cfg.Security,
		deliveryMode: cfg.DeliveryMode,
		ackTimeout:   cfg.AckTimeout,
		upgrader:     upgrader,
		clients:      make(map[*websocket.Conn]*clientInfo),
		startTime:    time.Now(),
		metrics:      newMetrics(cfg.MetricsRegistry, cfg.Name),
	}
}

// generateMessageID generates a unique message ID for correlation
func (w *Output) generateMessageID() string {
	counter := w.messageIDCounter.Add(1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UnixMilli(), counter)
}

// Meta returns the component metadata
func (w *Output) Meta() component.Metadata {
	subjectsStr := fmt.Sprintf("%v", w.subjects)

	// Use provided name if available, otherwise fall back to default naming
	name := w.name
	if name == "" {
		name = fmt.Sprintf("websocket-output-%d", w.port)
	}

	return component.Metadata{
		Name:        name,
		Type:        "output",
		Description: fmt.Sprintf("WebSocket server on %s:%d serving updates from subjects %s", w.path, w.port, subjectsStr),
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports for this component
func (w *Output) InputPorts() []component.Port {
	ports := make([]component.Port, len(w.subjects))
	for i, subject := range w.subjects {
		ports[i] = component.Port{
			Name:        fmt.Sprintf("nats_input_%d", i),
			Direction:   component.DirectionInput,
			Required:    false, // Optional - not all subjects will have publishers
			Description: fmt.Sprintf("NATS subject subscription for %s", subject),
			Config: component.NATSPort{
				Subject: subject,
			},
		}
	}
	return ports
}

// OutputPorts returns the output ports for this component
func (w *Output) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "websocket_endpoint",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: fmt.Sprintf("WebSocket endpoint at ws://localhost:%d%s", w.port, w.path),
			Config: component.NetworkPort{
				Protocol: "websocket",
				Host:     "localhost",
				Port:     w.port,
			},
		},
	}
}

// ConfigSchema returns the configuration schema for this component
// References the package-level websocketSchema variable for efficient retrieval
func (w *Output) ConfigSchema() component.ConfigSchema {
	return websocketSchema
}

// Health returns the current health status of the component
func (w *Output) Health() component.HealthStatus {
	w.mu.RLock()
	running := w.running
	serverRunning := w.server != nil
	w.mu.RUnlock()

	// Read error counter atomically (matches atomic writes in broadcastToClients)
	errCount := atomic.LoadInt64(&w.errors)

	healthy := running && serverRunning

	return component.HealthStatus{
		Healthy:    healthy,
		LastCheck:  time.Now(),
		ErrorCount: int(errCount),
		LastError:  "",
		Uptime:     time.Since(w.startTime),
	}
}

// DataFlow returns the current data flow metrics
func (w *Output) DataFlow() component.FlowMetrics {
	w.mu.RLock()
	messages := w.messagesSent
	bytes := w.bytesSent
	errCount := w.errors
	lastActivity := w.lastActivity
	w.mu.RUnlock()

	var messagesPerSecond float64
	var bytesPerSecond float64
	var errorRate float64

	if uptime := time.Since(w.startTime).Seconds(); uptime > 0 {
		messagesPerSecond = float64(messages) / uptime
		bytesPerSecond = float64(bytes) / uptime
	}

	if messages > 0 {
		errorRate = float64(errCount) / float64(messages)
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSecond,
		BytesPerSecond:    bytesPerSecond,
		ErrorRate:         errorRate,
		LastActivity:      lastActivity,
	}
}

// Initialize prepares the WebSocket output component but does not start the server
func (w *Output) Initialize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Validate configuration
	if w.port < 1024 || w.port > 65535 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "validateConfig",
			fmt.Sprintf("invalid port %d (out of range 1024-65535)", w.port))
	}

	if w.path == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "validateConfig", "WebSocket path cannot be empty")
	}

	if len(w.subjects) == 0 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "validateConfig", "NATS subjects cannot be empty")
	}

	// NATS client is optional for testing - will skip NATS subscription if nil

	return nil
}

// Start begins the WebSocket server and NATS subscription
func (w *Output) Start(ctx context.Context) error {
	// Serialize Start/Stop operations to prevent race conditions
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return nil
	}

	// Validate context
	if err := w.validateContext(ctx); err != nil {
		return err
	}

	// Create lifecycle context for ACME and other background operations
	w.lifecycleCtx, w.lifecycleStop = context.WithCancel(context.Background())

	// Create shutdown channels for coordinated shutdown
	w.setupShutdownChannels()

	// Cleanup on error
	var cleanupErr error
	defer func() {
		if cleanupErr != nil {
			w.cleanupOnError()
		}
	}()

	// Set up HTTP server with WebSocket endpoint
	if err := w.setupHTTPServer(); err != nil {
		cleanupErr = err
		return err
	}

	// Subscribe to NATS subjects for graph updates
	if err := w.subscribeToNATS(ctx); err != nil {
		cleanupErr = err
		return errors.Wrap(err, "Output", "Start", fmt.Sprintf("subscribe to NATS subjects %v", w.subjects))
	}

	// Mark as running and start background goroutines
	w.running = true
	w.startTime = time.Now()
	w.startBackgroundGoroutines(ctx)

	return nil
}

// validateContext checks if the provided context is valid
func (w *Output) validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "Start", "context cannot be nil")
	}

	// Check if context is already cancelled or timed out
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "Output", "Start", "context already cancelled or timed out")
	}

	return nil
}

// setupShutdownChannels creates channels for coordinated shutdown
func (w *Output) setupShutdownChannels() {
	w.shutdown = make(chan struct{})
	w.done = make(chan struct{})
}

// cleanupOnError cleans up resources when Start fails
func (w *Output) cleanupOnError() {
	// Clean up channels if we created them
	if w.shutdown != nil {
		close(w.shutdown)
		w.shutdown = nil
	}
	if w.done != nil {
		close(w.done)
		w.done = nil
	}
	// Clean up server if we created it
	if w.server != nil {
		_ = w.server.Shutdown(context.Background())
		w.server = nil
	}
}

// setupHTTPServer creates and configures the HTTP server with TLS if enabled
func (w *Output) setupHTTPServer() error {
	// Set up HTTP server with WebSocket endpoint
	mux := http.NewServeMux()
	mux.HandleFunc(w.path, w.handleWebSocket)

	w.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", w.port),
		Handler: mux,
	}

	// Configure TLS if enabled at platform level
	if w.security.TLS.Server.Enabled {
		// Check if ACME mode is enabled
		mode := w.security.TLS.Server.Mode
		if mode == "" {
			mode = "manual" // Default
		}

		if mode == "acme" && w.security.TLS.Server.ACME.Enabled {
			// Use ACME-aware TLS configuration
			tlsConfig, cleanup, err := tlsutil.LoadServerTLSConfigWithACME(
				w.lifecycleCtx,
				w.security.TLS.Server,
			)
			if err != nil {
				return errors.WrapFatal(err, "websocket_output", "setupHTTPServer",
					"load TLS config with ACME")
			}
			w.server.TLSConfig = tlsConfig

			// Store cleanup function for Stop()
			w.tlsCleanupMu.Lock()
			w.tlsCleanup = cleanup
			w.tlsCleanupMu.Unlock()
		} else {
			// Use manual TLS configuration
			tlsConfig, err := tlsutil.LoadServerTLSConfigWithMTLS(
				w.security.TLS.Server,
				w.security.TLS.Server.MTLS,
			)
			if err != nil {
				return errors.WrapFatal(err, "websocket_output", "setupHTTPServer",
					"load TLS config with mTLS")
			}
			w.server.TLSConfig = tlsConfig
		}
	}

	return nil
}

// startBackgroundGoroutines starts all background goroutines for the WebSocket server
func (w *Output) startBackgroundGoroutines(ctx context.Context) {
	// Create a fresh wait group for this start cycle to avoid reuse issues
	w.wg = &sync.WaitGroup{}

	// Add all goroutines to wait group before starting any of them
	goroutineCount := 2 // runServer + maintainClients
	if w.metrics != nil {
		goroutineCount++ // metrics goroutine
	}
	w.wg.Add(goroutineCount)

	// Start uptime tracking goroutine
	if w.metrics != nil {
		go w.trackUptime(ctx)
	}

	// Start the HTTP server in a goroutine
	go w.runServer(ctx)

	// Start client maintenance in a goroutine
	go w.maintainClients(ctx)
}

// trackUptime periodically updates the server uptime metric
func (w *Output) trackUptime(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.mu.RLock()
			running := w.running
			w.mu.RUnlock()
			if w.metrics != nil && running {
				w.metrics.serverUptimeSeconds.Set(time.Since(w.startTime).Seconds())
			}
		case <-ctx.Done():
			return
		case <-w.shutdown:
			return
		}
	}
}

// Stop gracefully stops the WebSocket server and closes all connections
func (w *Output) Stop(timeout time.Duration) error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false

	// Step 1: Signal shutdown to all goroutines
	if w.shutdown != nil {
		close(w.shutdown)
	}

	// Step 2: Capture references we need
	wg := w.wg
	server := w.server
	w.mu.Unlock()

	// Step 3: Shutdown HTTP server FIRST (outside locks)
	// This causes ListenAndServe to return with http.ErrServerClosed
	if server != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			// Log but continue - we still need to clean up
			// Note: In production this should use a proper logger
			fmt.Printf("[WARN] HTTP server shutdown error: %v\n", err)
		}
	}

	// Step 4: NOW wait for goroutines (they can exit after server shutdown)
	if wg != nil {
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All goroutines exited cleanly
		case <-time.After(5 * time.Second):
			fmt.Printf("[WARN] WebSocket goroutines did not exit within timeout\n")
		}
	}

	// Step 5: Stop ACME renewal loop if active
	w.tlsCleanupMu.Lock()
	if w.tlsCleanup != nil {
		w.tlsCleanup()
		w.tlsCleanup = nil
	}
	w.tlsCleanupMu.Unlock()

	// Cancel lifecycle context (stops ACME renewal loop)
	if w.lifecycleStop != nil {
		w.lifecycleStop()
	}

	// Step 6: Clean up remaining resources
	w.mu.Lock()
	w.unsubscribeFromNATS()
	w.closeAllClients()

	// Clear references and close done channel to signal completion
	w.server = nil
	if w.done != nil {
		close(w.done)
		w.done = nil
	}
	w.shutdown = nil
	w.wg = nil
	w.mu.Unlock()

	return nil
}

// subscribeToNATS subscribes to the configured NATS subjects
func (w *Output) subscribeToNATS(ctx context.Context) error {
	// Skip NATS subscription if client is nil (for testing)
	if w.natsClient == nil {
		return nil
	}

	// Subscribe to each subject using natsclient wrapper
	for _, subject := range w.subjects {
		err := w.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, data []byte) {
			w.handleNATSMessageData(msgCtx, data, subject)
		})
		if err != nil {
			return errors.Wrap(err, "Output", "subscribeToNATS", fmt.Sprintf("subscribe to NATS subject %s", subject))
		}
	}

	return nil
}

// unsubscribeFromNATS unsubscribes from all NATS subjects
func (w *Output) unsubscribeFromNATS() {
	// The natsclient wrapper handles subscription cleanup automatically
	// when the connection is closed or disconnected
}

// closeAllClients closes all WebSocket client connections
func (w *Output) closeAllClients() {
	w.clientsMu.Lock()
	for conn := range w.clients {
		_ = conn.Close()
	}
	w.clients = make(map[*websocket.Conn]*clientInfo)
	w.clientsMu.Unlock()
}

// handleNATSMessageData processes incoming message data from NATS and broadcasts to WebSocket clients
func (w *Output) handleNATSMessageData(ctx context.Context, data []byte, subject string) {
	// Check for context cancellation or shutdown signal
	select {
	case <-ctx.Done():
		return
	case <-w.shutdown:
		return
	default:
	}

	w.mu.RLock()
	if !w.running {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	// Update activity timestamp
	w.mu.Lock()
	w.lastActivity = time.Now()
	w.mu.Unlock()

	// Parse the message data as JSON to validate it
	var msgData map[string]any
	if err := json.Unmarshal(data, &msgData); err != nil {
		// If it's not JSON, wrap it in a simple structure
		msgData = map[string]any{
			"type":      "raw_data",
			"subject":   subject,
			"data":      string(data),
			"timestamp": time.Now().Format(time.RFC3339),
		}
	} else {
		// Add metadata if not present
		if _, exists := msgData["timestamp"]; !exists {
			msgData["timestamp"] = time.Now().Format(time.RFC3339)
		}
		if _, exists := msgData["subject"]; !exists {
			msgData["subject"] = subject
		}
	}

	// Marshal back to JSON for WebSocket transmission
	jsonData, err := json.Marshal(msgData)
	if err != nil {
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
		// Update metrics
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("json_marshal").Inc()
		}
		return
	}

	// Update metrics for received message
	if w.metrics != nil {
		w.metrics.messagesReceived.WithLabelValues(subject).Inc()
	}

	// Broadcast to all connected clients (with message context timeout)
	w.broadcastToClients(ctx, subject, jsonData)
}

// handleNATSMessage processes incoming messages from NATS and broadcasts to WebSocket clients
func (w *Output) handleNATSMessage(ctx context.Context, msg *natspkg.Msg) {
	// Check for context cancellation or shutdown signal
	select {
	case <-ctx.Done():
		return
	case <-w.shutdown:
		return
	default:
	}

	w.mu.RLock()
	if !w.running {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	// Update activity timestamp
	w.mu.Lock()
	w.lastActivity = time.Now()
	w.mu.Unlock()

	// Parse the message data as JSON to validate it
	var msgData map[string]any
	if err := json.Unmarshal(msg.Data, &msgData); err != nil {
		// If it's not JSON, wrap it in a simple structure
		msgData = map[string]any{
			"type":      "raw_data",
			"subject":   msg.Subject,
			"data":      string(msg.Data),
			"timestamp": time.Now().Format(time.RFC3339),
		}
	} else {
		// Add metadata if not present
		if _, exists := msgData["timestamp"]; !exists {
			msgData["timestamp"] = time.Now().Format(time.RFC3339)
		}
		if _, exists := msgData["subject"]; !exists {
			msgData["subject"] = msg.Subject
		}
	}

	// Marshal back to JSON for WebSocket transmission
	data, err := json.Marshal(msgData)
	if err != nil {
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
		// Update metrics
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("json_marshal").Inc()
		}
		return
	}

	// Update metrics for received message
	if w.metrics != nil {
		w.metrics.messagesReceived.WithLabelValues(msg.Subject).Inc()
	}

	// Broadcast to all connected clients
	w.broadcastToClients(ctx, msg.Subject, data)
}

// runServer runs the HTTP server
func (w *Output) runServer(_ context.Context) {
	defer func() {
		if w.wg != nil {
			w.wg.Done()
		}
	}()

	w.mu.RLock()
	server := w.server
	tlsEnabled := w.security.TLS.Server.Enabled
	w.mu.RUnlock()

	if server == nil {
		return
	}

	// ListenAndServe/ListenAndServeTLS blocks until Shutdown is called
	var err error
	if tlsEnabled {
		// ListenAndServeTLS with empty cert/key files since TLSConfig is already set
		err = server.ListenAndServeTLS("", "")
	} else {
		err = server.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		// Only log real errors, not graceful shutdown
		fmt.Printf("[ERROR] HTTP server failed: %v\n", err)
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
	}
	// http.ErrServerClosed is expected during graceful shutdown
}

// handleWebSocket handles new WebSocket connections
func (w *Output) handleWebSocket(wr http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := w.upgrader.Upgrade(wr, r, nil)
	if err != nil {
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
		// Update metrics
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("connection_upgrade").Inc()
		}
		return
	}

	// Add client to our map
	// Create circular buffer for pending messages (DropOldest policy, 100 capacity)
	pendingBuf, err := buffer.NewCircularBuffer[*PendingMessage](100,
		buffer.WithOverflowPolicy[*PendingMessage](buffer.DropOldest),
	)
	if err != nil {
		// Should not happen with valid config, but handle gracefully
		_ = conn.Close()
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("buffer_creation").Inc()
		}
		return
	}

	clientInfo := &clientInfo{
		conn:            conn,
		connectedAt:     time.Now(),
		pendingBuffer:   pendingBuf,
		pendingMessages: make(map[string]*PendingMessage),
	}
	clientInfo.lastPing.Store(time.Now())

	w.clientsMu.Lock()
	w.clients[conn] = clientInfo
	clientCount := len(w.clients)
	w.clientsMu.Unlock()

	// Update metrics
	if w.metrics != nil {
		w.metrics.connectionTotal.Inc()
		w.metrics.clientsConnected.Set(float64(clientCount))
	}

	// Handle client in a goroutine
	w.wg.Add(1)
	go w.handleClient(context.Background(), conn, clientInfo)
}

// handleClient manages a single WebSocket client connection
func (w *Output) handleClient(ctx context.Context, conn *websocket.Conn, info *clientInfo) {
	defer w.wg.Done()
	defer w.removeClient(conn, info)

	// Set up ping/pong handling for connection health
	conn.SetPongHandler(func(string) error {
		info.lastPing.Store(time.Now())
		return nil
	})

	// Read messages from client (control messages: ack, nack, slow)
	for {
		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		case <-w.shutdown:
			return
		default:
		}

		// Set read deadline
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Read message
		_, data, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or error
			return
		}

		// Try to parse as MessageEnvelope
		var envelope MessageEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			// Invalid message, ignore
			continue
		}

		// Handle based on message type
		switch envelope.Type {
		case "ack":
			w.handleAck(info, envelope.ID)
		case "nack":
			w.handleNack(info, envelope.ID)
		case "slow":
			w.handleSlow(info, envelope)
		default:
			// Unknown message type, ignore
		}
	}
}

// handleAck processes acknowledgment from client
func (w *Output) handleAck(info *clientInfo, messageID string) {
	info.pendingMu.Lock()
	pending, exists := info.pendingMessages[messageID]
	if exists {
		delete(info.pendingMessages, messageID)
	}
	info.pendingMu.Unlock()

	if exists && pending.AckChan != nil {
		select {
		case pending.AckChan <- true:
		default:
		}
	}
}

// handleNack processes negative acknowledgment from client
func (w *Output) handleNack(info *clientInfo, messageID string) {
	info.pendingMu.Lock()
	pending, exists := info.pendingMessages[messageID]
	if exists {
		delete(info.pendingMessages, messageID)
	}
	info.pendingMu.Unlock()

	if exists && pending.AckChan != nil {
		select {
		case pending.AckChan <- false:
		default:
		}
	}
}

// handleSlow processes backpressure signal from client
func (w *Output) handleSlow(info *clientInfo, envelope MessageEnvelope) {
	// TODO: Implement backpressure handling (future)
	// For now, just log that we received a slow signal
	_ = info
	_ = envelope
}

// removeClient safely removes a client connection with atomic cleanup
func (w *Output) removeClient(conn *websocket.Conn, info *clientInfo) {
	// Ensure cleanup happens only once
	info.closeOnce.Do(func() {
		// Mark as closed atomically
		info.closed.Store(true)

		// Remove from client map
		w.clientsMu.Lock()
		delete(w.clients, conn)
		clientCount := len(w.clients)
		w.clientsMu.Unlock()

		// Update metrics
		if w.metrics != nil {
			// Try to determine disconnect reason based on connection duration and state
			disconnectReason := "normal"
			if time.Since(info.connectedAt) < 5*time.Second {
				disconnectReason = "early_disconnect"
			}
			w.metrics.disconnectionTotal.WithLabelValues(disconnectReason).Inc()
			w.metrics.clientsConnected.Set(float64(clientCount))
		}

		// Close the connection (safe to call multiple times on websocket.Conn)
		_ = conn.Close()
	})
}

// broadcastToClients sends data to all connected WebSocket clients
func (w *Output) broadcastToClients(ctx context.Context, subject string, data []byte) {
	start := time.Now()

	// Prepare message envelope
	messageID, envelopeData := w.prepareMessageEnvelope(data)

	// Build snapshot of active clients
	clientList, clientInfoMap := w.buildClientSnapshot()

	// Check for context cancellation before broadcast
	select {
	case <-ctx.Done():
		return
	case <-w.shutdown:
		return
	default:
	}

	// Send to each client concurrently with proper synchronization
	var wg sync.WaitGroup
	for _, conn := range clientList {
		info := clientInfoMap[conn]
		// Skip if client was closed during iteration
		if info.closed.Load() {
			continue
		}

		wg.Add(1)
		go w.sendToSingleClient(&wg, conn, info, messageID, subject, envelopeData)
	}

	// Wait for all concurrent sends to complete
	wg.Wait()

	// Record broadcast duration
	if w.metrics != nil {
		w.metrics.broadcastDuration.WithLabelValues(subject).Observe(time.Since(start).Seconds())
	}
}

// prepareMessageEnvelope creates a message envelope and marshals it to JSON
func (w *Output) prepareMessageEnvelope(data []byte) (string, []byte) {
	// Generate unique message ID
	messageID := w.generateMessageID()

	// Wrap data in MessageEnvelope
	envelope := MessageEnvelope{
		Type:      "data",
		ID:        messageID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   json.RawMessage(data),
	}

	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		// Failed to marshal envelope, fallback to raw data
		envelopeData = data
		w.mu.Lock()
		w.errors++
		w.mu.Unlock()
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("envelope_marshal").Inc()
		}
	}

	return messageID, envelopeData
}

// buildClientSnapshot creates a thread-safe snapshot of active clients
func (w *Output) buildClientSnapshot() ([]*websocket.Conn, map[*websocket.Conn]*clientInfo) {
	w.clientsMu.RLock()
	defer w.clientsMu.RUnlock()

	// Create snapshot of clients with their info for timeout handling
	clientList := make([]*websocket.Conn, 0, len(w.clients))
	clientInfoMap := make(map[*websocket.Conn]*clientInfo, len(w.clients))
	for conn, info := range w.clients {
		if !info.closed.Load() {
			clientList = append(clientList, conn)
			clientInfoMap[conn] = info
		}
	}

	return clientList, clientInfoMap
}

// sendToSingleClient handles sending a message to one client with timeout and ack handling
func (w *Output) sendToSingleClient(wg *sync.WaitGroup, c *websocket.Conn, i *clientInfo, messageID, subject string, envelopeData []byte) {
	defer wg.Done()

	// Setup at-least-once delivery tracking if needed
	ackChan := w.setupPendingMessage(i, messageID, subject, envelopeData)

	// Create timeout context for this send operation
	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send with timeout using goroutine + channel pattern
	errChan := make(chan error, 1)
	go func() {
		errChan <- w.sendToClient(c, i, envelopeData)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			w.handleSendError(c, i, messageID)
		} else {
			w.handleSendSuccess(i, messageID, subject, envelopeData, ackChan)
		}
	case <-sendCtx.Done():
		w.handleSendTimeout(c, i, messageID)
	}
}

// setupPendingMessage creates pending message tracking for at-least-once delivery
func (w *Output) setupPendingMessage(i *clientInfo, messageID, subject string, envelopeData []byte) chan bool {
	if w.deliveryMode != DeliveryAtLeastOnce {
		return nil
	}

	ackChan := make(chan bool, 1)
	pending := &PendingMessage{
		ID:      messageID,
		Data:    envelopeData,
		Subject: subject,
		SentAt:  time.Now(),
		AckChan: ackChan,
	}

	i.pendingMu.Lock()
	i.pendingMessages[messageID] = pending
	i.pendingMu.Unlock()

	// Also add to circular buffer for monitoring
	if err := i.pendingBuffer.Write(pending); err != nil {
		// Buffer full, oldest message dropped
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("pending_buffer_full").Inc()
		}
	}

	return ackChan
}

// handleSendError processes errors that occur during message sending
func (w *Output) handleSendError(c *websocket.Conn, i *clientInfo, messageID string) {
	w.removeClient(c, i)
	atomic.AddInt64(&w.errors, 1)
	if w.metrics != nil {
		w.metrics.errorsTotal.WithLabelValues("client_send").Inc()
	}
	// Clean up pending if needed
	if w.deliveryMode == DeliveryAtLeastOnce {
		i.pendingMu.Lock()
		delete(i.pendingMessages, messageID)
		i.pendingMu.Unlock()
	}
}

// handleSendSuccess processes successful message sends and waits for acks
func (w *Output) handleSendSuccess(i *clientInfo, messageID, subject string, envelopeData []byte, ackChan chan bool) {
	// Success - use atomic operations for counters
	atomic.AddInt64(&w.messagesSent, 1)
	atomic.AddInt64(&w.bytesSent, int64(len(envelopeData)))
	if w.metrics != nil {
		w.metrics.messagesSent.WithLabelValues(subject).Inc()
		w.metrics.bytesSent.Add(float64(len(envelopeData)))
		w.metrics.messageSizeBytes.WithLabelValues(subject).Observe(float64(len(envelopeData)))
	}

	// For at-least-once, wait for ack with timeout
	if w.deliveryMode == DeliveryAtLeastOnce && ackChan != nil {
		w.waitForAck(i, messageID, ackChan)
	}
}

// waitForAck waits for acknowledgment from client with timeout
func (w *Output) waitForAck(i *clientInfo, messageID string, ackChan chan bool) {
	ackCtx, ackCancel := context.WithTimeout(context.Background(), w.ackTimeout)
	defer ackCancel()

	select {
	case acked := <-ackChan:
		if !acked {
			// Nack received - could retry here in future
			if w.metrics != nil {
				w.metrics.errorsTotal.WithLabelValues("nack_received").Inc()
			}
		}
	case <-ackCtx.Done():
		// Ack timeout - could retry here in future
		if w.metrics != nil {
			w.metrics.errorsTotal.WithLabelValues("ack_timeout").Inc()
		}
		// Clean up pending
		i.pendingMu.Lock()
		delete(i.pendingMessages, messageID)
		i.pendingMu.Unlock()
	}
}

// handleSendTimeout processes timeouts that occur during message sending
func (w *Output) handleSendTimeout(c *websocket.Conn, i *clientInfo, messageID string) {
	w.removeClient(c, i)
	atomic.AddInt64(&w.errors, 1)
	if w.metrics != nil {
		w.metrics.errorsTotal.WithLabelValues("client_timeout").Inc()
	}
	// Clean up pending if needed
	if w.deliveryMode == DeliveryAtLeastOnce {
		i.pendingMu.Lock()
		delete(i.pendingMessages, messageID)
		i.pendingMu.Unlock()
	}
}

// sendToClient sends data to a specific WebSocket client with proper locking
func (w *Output) sendToClient(conn *websocket.Conn, info *clientInfo, data []byte) error {
	// Lock to prevent concurrent writes to the same connection
	// The gorilla/websocket library panics on concurrent writes
	info.writeMutex.Lock()
	defer info.writeMutex.Unlock()

	// Set write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	// Send as text message
	return conn.WriteMessage(websocket.TextMessage, data)
}

// maintainClients performs periodic maintenance on client connections
func (w *Output) maintainClients(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.shutdown:
			return
		case <-ticker.C:
			w.pingClients(ctx)
		}
	}
}

// pingClients sends ping messages to all connected clients
func (w *Output) pingClients(ctx context.Context) {
	w.clientsMu.RLock()
	clientList := make([]*websocket.Conn, 0, len(w.clients))
	clientInfoMap := make(map[*websocket.Conn]*clientInfo, len(w.clients))
	for conn, info := range w.clients {
		if !info.closed.Load() {
			clientList = append(clientList, conn)
			clientInfoMap[conn] = info
		}
	}
	w.clientsMu.RUnlock()

	// Check for context cancellation before pinging
	select {
	case <-ctx.Done():
		return
	case <-w.shutdown:
		return
	default:
	}

	for _, conn := range clientList {
		info := clientInfoMap[conn]
		// Skip if client was closed during iteration
		if info.closed.Load() {
			continue
		}

		if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			// Client error, remove client
			w.removeClient(conn, info)
			w.mu.Lock()
			w.errors++
			w.mu.Unlock()
		}
	}
}

// Removed duplicate DefaultConfig - already defined above

// getConfiguredValues extracts configuration values from ports or legacy fields
// Removed getConfiguredValues() - no backward compatibility needed

// Register registers the WebSocket output component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "websocket",
		Factory:     CreateOutput,
		Schema:      websocketSchema,
		Type:        "output",
		Protocol:    "websocket",
		Domain:      "network",
		Description: "WebSocket output component for real-time visualization and data streaming",
		Version:     "1.0.0",
	})
}

// CreateOutput creates a WebSocket output component following service pattern
func CreateOutput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Parse user config if provided
	if len(rawConfig) > 0 {
		if err := component.SafeUnmarshal(rawConfig, &cfg); err != nil {
			return nil, errors.WrapInvalid(err, "websocket-output-factory", "create", "parse config")
		}
	}

	// Extract configuration from Ports (single source of truth)
	var port int
	var path string
	var subjects []string

	if cfg.Ports != nil {
		// Extract port and path from output URL if available
		if len(cfg.Ports.Outputs) > 0 && cfg.Ports.Outputs[0].Subject != "" {
			// Parse URL-encoded port from Subject field (e.g., "http://0.0.0.0:8082/ws")
			url := cfg.Ports.Outputs[0].Subject
			var parsedPort int
			var parsedPath string
			if _, err := fmt.Sscanf(url, "http://0.0.0.0:%d%s", &parsedPort, &parsedPath); err == nil {
				port = parsedPort
				path = parsedPath
			}
		}

		// Extract subjects from inputs
		if len(cfg.Ports.Inputs) > 0 {
			for _, input := range cfg.Ports.Inputs {
				if input.Subject != "" {
					subjects = append(subjects, input.Subject)
				}
			}
		}
	}

	// Apply defaults if not configured
	if port == 0 {
		port = 8081
	}
	if path == "" {
		path = "/ws"
	}
	if len(subjects) == 0 {
		subjects = []string{"semantic.>"}
	}

	// Parse delivery mode (default: at-most-once for backward compatibility)
	deliveryMode := DeliveryAtMostOnce
	if cfg.DeliveryMode != "" {
		deliveryMode = cfg.DeliveryMode
	}

	// Parse ack timeout (default: 5 seconds)
	ackTimeout := 5 * time.Second
	if cfg.AckTimeout != "" {
		parsed, err := time.ParseDuration(cfg.AckTimeout)
		if err != nil {
			return nil, errors.WrapInvalid(err, "websocket-output-factory", "create", "parse ack_timeout")
		}
		ackTimeout = parsed
	}

	// Validate port range (allow 0 for random port in tests)
	// Ports below 1024 are reserved system ports
	if port != 0 && (port < 1024 || port > 65535) {
		return nil, errors.WrapInvalid(fmt.Errorf("port %d out of range", port),
			"websocket-output-factory", "create", "port range validation")
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errors.WrapInvalid(fmt.Errorf("NATS client is required"),
			"websocket-output-factory", "create", "NATS client validation")
	}

	// Create constructor config
	ctorCfg := ConstructorConfig{
		Name:            "websocket-output",
		Port:            port,
		Path:            path,
		Subjects:        subjects,
		NATSClient:      deps.NATSClient,
		MetricsRegistry: deps.MetricsRegistry,
		Security:        deps.Security,
		DeliveryMode:    deliveryMode,
		AckTimeout:      ackTimeout,
	}

	return NewOutputFromConfig(ctorCfg), nil
}
