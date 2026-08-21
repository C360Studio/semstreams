// Package websocket provides WebSocket output component for sending data to external systems
package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/buffer"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/pkg/tlsutil"
	"github.com/gorilla/websocket"
	natspkg "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
	// Path is the path-only HTTP ServeMux pattern used for WebSocket upgrades.
	Path string `json:"path" schema:"type:string,description:WebSocket upgrade path-only ServeMux pattern,category:basic,default:/ws"`
	// DeliveryMode specifies reliability semantics (at-most-once or at-least-once)
	DeliveryMode DeliveryMode `json:"delivery_mode,omitempty" schema:"type:string,description:Delivery reliability mode,category:advanced"`
	// AckTimeout specifies how long to wait for ack before considering message lost
	AckTimeout string `json:"ack_timeout,omitempty"   schema:"type:string,description:Acknowledgment timeout (e.g. 5s),category:advanced"`
	// Passthrough broadcasts pre-validated JSON as-is (no decode/re-encode, no
	// timestamp/subject injection). Opting in asserts the producer emits an
	// envelope-complete payload; non-JSON still falls back to the raw_data wrapper.
	Passthrough bool `json:"passthrough,omitempty"   schema:"type:bool,description:Broadcast pre-validated JSON unchanged (producer owns envelope; no timestamp/subject injection),category:advanced,default:false"`
}

// ConstructorConfig holds all configuration needed to construct an Output instance
type ConstructorConfig struct {
	Name            string                     // Component name (empty = auto-generate)
	Path            string                     // WebSocket endpoint path
	InputPorts      []component.PortDefinition // NATS input declarations
	OutputPorts     []component.PortDefinition // Network output declarations
	NATSClient      *natsclient.Client         // NATS client for messaging
	MetricsRegistry *metric.MetricsRegistry    // Optional Prometheus metrics registry
	Logger          *slog.Logger               // Optional logger (nil = use default)
	Security        security.Config            // Security configuration
	DeliveryMode    DeliveryMode               // Reliability semantics
	AckTimeout      time.Duration              // Acknowledgment timeout for at-least-once
	Passthrough     bool                       // Broadcast pre-validated JSON unchanged (no inject)
}

// DefaultConstructorConfig returns sensible defaults for Output construction
func DefaultConstructorConfig() ConstructorConfig {
	ports := DefaultConfig().Ports
	return ConstructorConfig{
		Name:         "",
		Path:         "/ws",
		InputPorts:   append([]component.PortDefinition(nil), ports.Inputs...),
		OutputPorts:  append([]component.PortDefinition(nil), ports.Outputs...),
		Security:     security.Config{},
		DeliveryMode: DeliveryAtMostOnce,
		AckTimeout:   5 * time.Second,
	}
}

// DefaultConfig returns the default configuration for WebSocket output
func DefaultConfig() Config {
	// WebSocket output typically has:
	// - Input: NATS subjects to listen to
	// - Output: WebSocket server network binding
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "semantic.>"}, // Default to semantic events
			Required:    true,
			Description: "NATS subjects to listen to",
		},
	}

	outputDefs := websocketOutputDefinitions(8081)

	return Config{
		Path: "/ws",
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
	}
}

// Validate checks component-local configuration before factory construction.
func (c Config) Validate() error {
	if err := validateWebSocketPath(c.Path); err != nil {
		return errs.WrapInvalid(err, "websocket-output-config", "validate", "path")
	}
	return nil
}

// websocketSchema defines the configuration schema for WebSocket output component
// Generated from Config struct tags using reflection
var websocketSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Output implements a WebSocket server that broadcasts NATS messages to connected clients
// This is designed for real-time visualization of graph updates and entity state changes
type Output struct {
	name         string
	host         string
	port         int
	path         string
	subjects     []string
	inputPorts   []component.Port
	outputPorts  []component.Port
	natsClient   *natsclient.Client
	security     security.Config
	deliveryMode DeliveryMode
	ackTimeout   time.Duration
	passthrough  bool // broadcast pre-validated JSON unchanged (no decode/re-encode/inject)

	// WebSocket server
	server    *http.Server
	listener  net.Listener
	serveDone chan error
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]*clientInfo
	clientsMu sync.RWMutex

	// NATS subscriptions for cleanup
	subscriptions []coreSubscription
	consumers     []streamConsumerBinding

	// Lifecycle management
	running        bool
	startTime      time.Time
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	wg             *sync.WaitGroup
	runtimeDone    chan struct{}
	tlsCleanup     func()     // ACME cleanup function (stops renewal loop)
	tlsCleanupMu   sync.Mutex // Protects tlsCleanup
	requestMu      sync.Mutex
	requestOpen    bool
	requestCount   int
	requestZero    chan struct{}
	requestHook    func(context.Context)
	subscribeCore  func(context.Context, string, func(context.Context, *natspkg.Msg)) (coreSubscription, error)
	waitForInput   func(context.Context, string) error
	consumeStream  func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitJSClosed   func(context.Context, <-chan struct{}) error

	// Message ID generation
	messageIDCounter atomic.Uint64

	// Metrics (atomic access)
	messagesSent atomic.Int64
	bytesSent    atomic.Int64
	errors       atomic.Int64
	lastActivity atomic.Int64 // Unix timestamp in nanoseconds

	// Prometheus metrics
	metrics *Metrics

	// Logging
	logger *slog.Logger
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

type coreSubscription interface {
	Drain(context.Context) error
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
func newMetrics(registry *metric.MetricsRegistry, componentName string) *Metrics {
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

	// Register through the idempotent MetricsRegistry helpers (keyed by
	// componentName.metricName) rather than the raw Prometheus registry.
	// MustRegister panics on the second registration of the same collector,
	// which crashes runtime component restart (gh#490); the Register* methods
	// swallow AlreadyRegisteredError so restart is safe.
	registry.RegisterCounterVec(componentName, "messages_received", metrics.messagesReceived)
	registry.RegisterCounterVec(componentName, "messages_sent", metrics.messagesSent)
	registry.RegisterCounter(componentName, "bytes_sent", metrics.bytesSent)
	registry.RegisterGauge(componentName, "clients_connected", metrics.clientsConnected)
	registry.RegisterCounter(componentName, "connection_total", metrics.connectionTotal)
	registry.RegisterCounterVec(componentName, "disconnection_total", metrics.disconnectionTotal)
	registry.RegisterHistogramVec(componentName, "broadcast_duration", metrics.broadcastDuration)
	registry.RegisterHistogramVec(componentName, "message_size_bytes", metrics.messageSizeBytes)
	registry.RegisterCounterVec(componentName, "errors_total", metrics.errorsTotal)
	registry.RegisterGauge(componentName, "server_uptime_seconds", metrics.serverUptimeSeconds)

	return metrics
}

// NewOutput creates a new WebSocket output component with minimal configuration.
// For more control over configuration, use NewOutputFromConfig().
func NewOutput(port int, path string, subjects []string, natsClient *natsclient.Client) (*Output, error) {
	cfg := DefaultConstructorConfig()
	cfg.Path = path
	cfg.InputPorts = natsInputDefinitions(subjects)
	cfg.OutputPorts = websocketOutputDefinitions(port)
	cfg.NATSClient = natsClient
	return NewOutputFromConfig(cfg)
}

// NewOutputFromConfig creates a new WebSocket output component from ConstructorConfig.
// This is the recommended way to create Output instances with full configuration control.
func NewOutputFromConfig(cfg ConstructorConfig) (*Output, error) {
	if err := validateWebSocketPath(cfg.Path); err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutputFromConfig", "validate path")
	}
	if len(cfg.InputPorts) == 0 || len(cfg.OutputPorts) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "NewOutputFromConfig", "at least one input and exactly one output port are required")
	}
	inputs := make([]component.Port, len(cfg.InputPorts))
	subjects := make([]string, 0, len(cfg.InputPorts))
	for index, definition := range cfg.InputPorts {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Output", "NewOutputFromConfig", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, errs.WrapInvalid(err, "Output", "NewOutputFromConfig", "project input port")
		}
		if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
			return nil, errs.WrapInvalid(fmt.Errorf("input port %q kind %q is not nats or jetstream", port.Name, facts.Kind()), "Output", "NewOutputFromConfig", "validate input port")
		}
		portSubjects := facts.NATSSubjects()
		if len(portSubjects) != 1 {
			return nil, errs.WrapInvalid(fmt.Errorf("input port %q declares %d subjects, want one", port.Name, len(portSubjects)), "Output", "NewOutputFromConfig", "validate input port")
		}
		inputs[index] = port
		subjects = append(subjects, portSubjects[0])
	}
	outputs := make([]component.Port, len(cfg.OutputPorts))
	for index, definition := range cfg.OutputPorts {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Output", "NewOutputFromConfig", "resolve output port")
		}
		outputs[index] = port
	}
	outputFacts, err := outputs[0].Facts()
	if err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutputFromConfig", "project output port")
	}
	network, ok := outputFacts.Network()
	if !ok {
		return nil, errs.WrapInvalid(fmt.Errorf("output port %q kind %q is not network", outputs[0].Name, outputFacts.Kind()), "Output", "NewOutputFromConfig", "validate output port")
	}
	if network.Protocol() != "http" {
		return nil, errs.WrapInvalid(fmt.Errorf("output port %q protocol %q is not http", outputs[0].Name, network.Protocol()), "Output", "NewOutputFromConfig", "validate output port")
	}
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool {
			// Allow connections from any origin for development
			// In production, this should be more restrictive
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	// Use provided logger or default
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Output{
		name:         cfg.Name,
		host:         network.Host(),
		port:         network.Port(),
		path:         cfg.Path,
		subjects:     subjects,
		inputPorts:   inputs,
		outputPorts:  outputs,
		natsClient:   cfg.NATSClient,
		security:     cfg.Security,
		deliveryMode: cfg.DeliveryMode,
		ackTimeout:   cfg.AckTimeout,
		passthrough:  cfg.Passthrough,
		upgrader:     upgrader,
		clients:      make(map[*websocket.Conn]*clientInfo),
		startTime:    time.Now(),
		metrics:      newMetrics(cfg.MetricsRegistry, cfg.Name),
		logger:       logger,
	}, nil
}

func validateWebSocketPath(path string) (err error) {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must begin with /")
	}
	for index := 0; index < len(path); index++ {
		if path[index] <= ' ' || path[index] == 0x7f {
			return fmt.Errorf("path contains ASCII whitespace or control character at byte %d", index)
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("path is not a valid HTTP ServeMux pattern: %v", recovered)
		}
	}()
	http.NewServeMux().HandleFunc(path, func(http.ResponseWriter, *http.Request) {})
	return nil
}

func natsInputDefinitions(subjects []string) []component.PortDefinition {
	definitions := make([]component.PortDefinition, len(subjects))
	for index, subject := range subjects {
		definitions[index] = component.PortDefinition{
			Name:   fmt.Sprintf("nats_input_%d", index),
			Config: component.NATSPort{Subject: subject},
		}
	}
	return definitions
}

func websocketOutputDefinitions(port int) []component.PortDefinition {
	return []component.PortDefinition{{
		Name:        "websocket_server",
		Config:      component.NetworkPort{Protocol: "http", Host: "0.0.0.0", Port: port},
		Description: "WebSocket server endpoint",
	}}
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
	return append([]component.Port(nil), w.inputPorts...)
}

// OutputPorts returns the output ports for this component
func (w *Output) OutputPorts() []component.Port {
	return append([]component.Port(nil), w.outputPorts...)
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

	// Read error counter atomically
	errCount := w.errors.Load()

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
	// Read metrics atomically (no lock needed)
	messages := w.messagesSent.Load()
	bytes := w.bytesSent.Load()
	errCount := w.errors.Load()
	lastActivityNanos := w.lastActivity.Load()

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

	// Convert nanoseconds back to time.Time
	var lastActivity time.Time
	if lastActivityNanos > 0 {
		lastActivity = time.Unix(0, lastActivityNanos)
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
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "validateConfig",
			fmt.Sprintf("invalid port %d (out of range 1024-65535)", w.port))
	}

	if err := validateWebSocketPath(w.path); err != nil {
		return errs.WrapInvalid(err, "Output", "validateConfig", "WebSocket path")
	}

	if len(w.subjects) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "validateConfig", "NATS subjects cannot be empty")
	}

	// NATS client is optional for testing - will skip NATS subscription if nil

	return nil
}

// Start begins the WebSocket server and NATS subscription
func (w *Output) Start(ctx context.Context) (startErr error) {
	// Validate before locking or inspecting lifecycle state.
	if err := w.validateContext(ctx); err != nil {
		return err
	}
	w.lifecycleMu.Lock()
	if w.lifecycleUsed {
		w.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Output", "Start", "cleanup authority already active")
	}
	parent := ctx
	runtimeCtx, runtimeCancel := context.WithCancel(ctx)
	runtimeWG := &sync.WaitGroup{}
	startDone := make(chan struct{})
	w.lifecycleUsed = true
	w.cleanupPending = true
	w.cancel = runtimeCancel
	w.startDone = startDone
	w.wg = runtimeWG
	w.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if committed {
			w.lifecycleMu.Lock()
			w.cleanupPending = false
			close(startDone)
			w.startDone = nil
			w.lifecycleMu.Unlock()
			return
		}
		rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, w.cleanup)
		startErr = errors.Join(startErr, rollbackErr)
		w.lifecycleMu.Lock()
		if rollbackErr == nil {
			w.cleanupPending = false
			w.terminal = true
			w.clearRuntime()
		}
		close(startDone)
		w.startDone = nil
		w.lifecycleMu.Unlock()
	}()

	// Set up HTTP server with WebSocket endpoint
	if err := w.setupHTTPServer(runtimeCtx); err != nil {
		return err
	}
	w.requestMu.Lock()
	w.requestOpen = true
	w.requestMu.Unlock()
	serveDone := make(chan error, 1)
	w.serveDone = serveDone
	go w.runServer(w.server, w.listener, serveDone)

	// Subscribe to NATS subjects for graph updates
	if err := w.setupSubscriptions(runtimeCtx); err != nil {
		return errs.Wrap(err, "Output", "Start", fmt.Sprintf("subscribe to NATS subjects %v", w.subjects))
	}

	goroutineCount := 1 // maintainClients
	if w.metrics != nil {
		goroutineCount++
	}
	runtimeWG.Add(goroutineCount)
	runtimeDone := make(chan struct{})
	w.runtimeDone = runtimeDone
	go func() {
		runtimeWG.Wait()
		close(runtimeDone)
	}()
	w.mu.Lock()
	w.running = true
	w.startTime = time.Now()
	w.mu.Unlock()
	w.startBackgroundGoroutines(runtimeCtx, runtimeWG)
	committed = true

	return nil
}

// validateContext checks if the provided context is valid
func (w *Output) validateContext(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "Start", "context cannot be nil")
	}

	// Check if context is already cancelled or timed out
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Output", "Start", "context already cancelled or timed out")
	}

	return nil
}

// setupHTTPServer creates and configures the HTTP server with TLS if enabled
func (w *Output) setupHTTPServer(ctx context.Context) error {
	if err := validateWebSocketPath(w.path); err != nil {
		return errs.WrapInvalid(err, "websocket_output", "setupHTTPServer", "validate path")
	}
	// Set up HTTP server with WebSocket endpoint
	mux := http.NewServeMux()
	mux.HandleFunc(w.path, func(wr http.ResponseWriter, r *http.Request) {
		if !w.admitRequest() {
			http.Error(wr, "service stopping", http.StatusServiceUnavailable)
			return
		}
		defer w.releaseRequest()
		if w.requestHook != nil {
			w.requestHook(r.Context())
		}
		w.handleWebSocket(wr, r)
	})

	w.server = &http.Server{
		Addr:    net.JoinHostPort(w.host, strconv.Itoa(w.port)),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
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
				ctx,
				w.security.TLS.Server,
			)
			if err != nil {
				return errs.WrapFatal(err, "websocket_output", "setupHTTPServer",
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
				return errs.WrapFatal(err, "websocket_output", "setupHTTPServer",
					"load TLS config with mTLS")
			}
			w.server.TLSConfig = tlsConfig
		}
	}
	listener, err := net.Listen("tcp", w.server.Addr)
	if err != nil {
		return errs.WrapFatal(err, "websocket_output", "setupHTTPServer", "bind HTTP listener")
	}
	w.listener = listener

	return nil
}

// startBackgroundGoroutines starts all background goroutines for the WebSocket server
func (w *Output) startBackgroundGoroutines(ctx context.Context, runtimeWG *sync.WaitGroup) {
	// Start uptime tracking goroutine
	if w.metrics != nil {
		go w.trackUptime(ctx, runtimeWG)
	}

	// Start client maintenance in a goroutine
	go w.maintainClients(ctx, runtimeWG)
}

// trackUptime periodically updates the server uptime metric
func (w *Output) trackUptime(ctx context.Context, runtimeWG *sync.WaitGroup) {
	defer runtimeWG.Done()
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
		}
	}
}

// Stop gracefully stops the WebSocket server and closes all connections
func (w *Output) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	for {
		w.lifecycleMu.Lock()
		if !w.lifecycleUsed {
			w.lifecycleUsed = true
			w.terminal = true
			w.lifecycleMu.Unlock()
			return nil
		}
		if w.terminal {
			w.lifecycleMu.Unlock()
			return nil
		}
		if w.startDone != nil {
			done := w.startDone
			w.lifecycleMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if w.stopping {
			w.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "Output", "Stop", "concurrent Stop")
		}
		retryable := w.cleanupPending
		w.stopping = true
		w.lifecycleMu.Unlock()

		stopErr := w.cleanup(ctx)
		w.lifecycleMu.Lock()
		w.stopping = false
		if retryable && stopErr != nil {
			w.lifecycleMu.Unlock()
			return stopErr
		}
		w.cleanupPending = false
		w.terminal = true
		w.clearRuntime()
		w.lifecycleMu.Unlock()
		return attributeComponentShutdownError("websocket-output", errs.PhaseJoinRuntime, stopErr)
	}
}

func attributeComponentShutdownError(owner string, phase errs.ShutdownPhase, err error) error {
	if err == nil {
		return nil
	}
	var shutdownErr *errs.ShutdownError
	if errors.As(err, &shutdownErr) {
		return err
	}
	return errs.NewShutdownError(owner, phase, err)
}

func (w *Output) cleanup(ctx context.Context) error {
	w.lifecycleMu.Lock()
	server := w.server
	serveDone := w.serveDone
	cancel := w.cancel
	runtimeDone := w.runtimeDone
	coreSubscriptions := append([]coreSubscription(nil), w.subscriptions...)
	consumers := append([]streamConsumerBinding(nil), w.consumers...)
	w.lifecycleMu.Unlock()

	requestZero := w.fenceRequests()
	var cleanupErr error
	serverComplete := server == nil
	if server != nil {
		shutdownErr := server.Shutdown(ctx)
		cleanupErr = errors.Join(cleanupErr, errs.NewShutdownError("websocket-output", errs.PhaseShutdownListener, shutdownErr))
		serverComplete = shutdownErr == nil
	}
	if serveDone != nil {
		select {
		case err := <-serveDone:
			cleanupErr = errors.Join(cleanupErr, err)
			serverComplete = serverComplete && err == nil
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
			serverComplete = false
		}
	}
	if serverComplete {
		w.lifecycleMu.Lock()
		if w.server == server {
			w.mu.Lock()
			w.server = nil
			w.mu.Unlock()
			w.listener = nil
			w.serveDone = nil
		}
		w.lifecycleMu.Unlock()
	}

	retainedCore := make([]coreSubscription, 0, len(coreSubscriptions))
	for _, sub := range coreSubscriptions {
		if sub == nil {
			continue
		}
		if err := sub.Drain(ctx); err != nil {
			cleanupErr = errors.Join(cleanupErr, errs.NewShutdownError("websocket-output/subscriptions", errs.PhaseDrainSubscriptions, err))
			retainedCore = append(retainedCore, sub)
		}
	}
	retainedConsumers := make([]streamConsumerBinding, 0, len(consumers))
	for _, binding := range consumers {
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
		closed := binding.handle.Closed()
		var waitErr error
		if w.waitJSClosed != nil {
			waitErr = w.waitJSClosed(ctx, closed)
		} else {
			select {
			case <-closed:
			case <-ctx.Done():
				waitErr = ctx.Err()
			}
		}
		if waitErr != nil {
			cleanupErr = errors.Join(cleanupErr, waitErr)
			retainedConsumers = append(retainedConsumers, binding)
		}
	}
	w.lifecycleMu.Lock()
	w.subscriptions = retainedCore
	w.consumers = retainedConsumers
	w.lifecycleMu.Unlock()

	w.closeAllClients()
	select {
	case <-requestZero:
	case <-ctx.Done():
		cleanupErr = errors.Join(cleanupErr, ctx.Err())
	}
	if cancel != nil {
		cancel()
	}
	runtimeComplete := runtimeDone == nil
	if runtimeDone != nil {
		select {
		case <-runtimeDone:
			runtimeComplete = true
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
		}
	}
	if runtimeComplete {
		w.lifecycleMu.Lock()
		if w.runtimeDone == runtimeDone {
			w.runtimeDone = nil
			w.cancel = nil
		}
		w.lifecycleMu.Unlock()
	}
	if cleanup := w.takeTLSCleanup(); cleanup != nil {
		cleanup()
	}
	return cleanupErr
}

func (w *Output) clearRuntime() {
	w.mu.Lock()
	w.running = false
	w.server = nil
	w.listener = nil
	w.serveDone = nil
	w.mu.Unlock()
	w.subscriptions = nil
	w.consumers = nil
	w.wg = nil
	w.runtimeDone = nil
	w.cancel = nil
	_ = w.takeTLSCleanup()
}

func (w *Output) takeTLSCleanup() func() {
	w.tlsCleanupMu.Lock()
	cleanup := w.tlsCleanup
	w.tlsCleanup = nil
	w.tlsCleanupMu.Unlock()
	return cleanup
}

func (w *Output) admitRequest() bool {
	w.requestMu.Lock()
	defer w.requestMu.Unlock()
	if !w.requestOpen {
		return false
	}
	if w.requestCount == 0 {
		w.requestZero = make(chan struct{})
	}
	w.requestCount++
	return true
}

func (w *Output) releaseRequest() {
	w.requestMu.Lock()
	defer w.requestMu.Unlock()
	w.requestCount--
	if w.requestCount == 0 {
		close(w.requestZero)
	}
}

func (w *Output) fenceRequests() <-chan struct{} {
	w.requestMu.Lock()
	defer w.requestMu.Unlock()
	w.requestOpen = false
	if w.requestCount == 0 {
		done := make(chan struct{})
		close(done)
		return done
	}
	return w.requestZero
}

// setupSubscriptions creates subscriptions for all resolved input ports.
func (w *Output) setupSubscriptions(ctx context.Context) error {
	if w.natsClient == nil {
		return nil
	}

	for _, port := range w.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "Output", "setupSubscriptions", "project input port facts")
		}
		subjects := facts.NATSSubjects()
		if len(subjects) != 1 {
			return errs.WrapInvalid(fmt.Errorf("input port %q declares %d subjects, want one", port.Name, len(subjects)), "Output", "setupSubscriptions", "validate input port")
		}
		subject := subjects[0]

		switch facts.Kind() {
		case component.PortKindJetStream:
			if err := w.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "Output", "setupSubscriptions",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			subscribe := func(ctx context.Context, subject string, handler func(context.Context, *natspkg.Msg)) (coreSubscription, error) {
				return w.natsClient.Subscribe(ctx, subject, handler)
			}
			if w.subscribeCore != nil {
				subscribe = w.subscribeCore
			}
			sub, err := subscribe(ctx, subject, func(msgCtx context.Context, msg *natspkg.Msg) {
				w.handleNATSMessageData(msgCtx, msg.Data, msg.Subject)
			})
			if err != nil {
				return errs.Wrap(err, "Output", "setupSubscriptions",
					fmt.Sprintf("subscribe to NATS subject %s", subject))
			}
			w.lifecycleMu.Lock()
			w.subscriptions = append(w.subscriptions, sub)
			w.lifecycleMu.Unlock()
		default:
			return errs.WrapInvalid(fmt.Errorf("input port %q kind %q is not nats or jetstream", port.Name, facts.Kind()), "Output", "setupSubscriptions", "validate input port")
		}
	}

	return nil
}

// setupJetStreamConsumer creates a durable JetStream consumer for an input port.
// The consumer provides replay-on-reconnect and proper ack-based backpressure.
func (w *Output) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok {
		return fmt.Errorf("port %q does not declare JetStream facts", port.Name)
	}
	subject := facts.NATSSubjects()[0]
	streamName := stream.Name()

	waitForStream := w.waitForStream
	if w.waitForInput != nil {
		waitForStream = w.waitForInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("wait for stream %s", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("ws-output-%s", sanitizedSubject)

	w.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "Output", "setupJetStreamConsumer", "resolve consumer config")
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending,
		AutoCreate:    false,
	}

	consume := w.natsClient.ConsumeStreamWithConfig
	if w.consumeStream != nil {
		consume = w.consumeStream
	}
	handle, err := consume(ctx, natsclient.PortConsumerContext{Component: w.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		w.handleNATSMessageData(msgCtx, msg.Data(), msg.Subject())
		if ackErr := msg.Ack(); ackErr != nil {
			w.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("setup consumer for stream %s", streamName))
	}
	w.lifecycleMu.Lock()
	w.consumers = append(w.consumers, streamConsumerBinding{handle: handle})
	w.lifecycleMu.Unlock()

	w.logger.Debug("WebSocket output subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream polls until the named JetStream stream is available or the context is cancelled.
func (w *Output) waitForStream(ctx context.Context, streamName string) error {
	js, err := w.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "WebSocketOutput", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}
	return errs.WrapTransient(errs.ErrStorageUnavailable, "WebSocketOutput", "waitForStream",
		fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
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
	// Check for generation cancellation.
	select {
	case <-ctx.Done():
		return
	default:
	}

	w.mu.RLock()
	if !w.running {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	// Update activity timestamp atomically
	w.lastActivity.Store(time.Now().UnixNano())

	w.broadcastPayload(ctx, subject, data)
}

// handleNATSMessage processes incoming messages from NATS and broadcasts to WebSocket clients
func (w *Output) handleNATSMessage(ctx context.Context, msg *natspkg.Msg) {
	// Check for generation cancellation.
	select {
	case <-ctx.Done():
		return
	default:
	}

	w.mu.RLock()
	if !w.running {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	// Update activity timestamp atomically
	w.lastActivity.Store(time.Now().UnixNano())

	w.broadcastPayload(ctx, msg.Subject, msg.Data)
}

// broadcastPayload transforms an inbound NATS payload and broadcasts it to
// connected WebSocket clients. It is the single transform point shared by both
// inbound handler entrypoints so their behavior cannot drift.
//
// In pass-through mode (opt-in, default off), a payload that is valid JSON is
// broadcast as its ORIGINAL bytes — no decode/re-encode, so key order and numeric
// precision are preserved, and no timestamp/subject is injected (the producer owns
// its envelope). In the default mode the payload is decoded, timestamp/subject are
// injected when absent, and it is re-encoded. A payload that is not valid JSON is
// wrapped in a raw_data envelope in either mode, so pass-through is safe on a
// subject carrying mixed content.
func (w *Output) broadcastPayload(ctx context.Context, subject string, data []byte) {
	out := w.transformPayload(subject, data)
	if out == nil {
		// Only reachable when re-encoding the decoded map fails (never on the
		// pass-through or raw_data branches).
		w.errors.Add(1)
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
	w.broadcastToClients(ctx, subject, out)
}

// transformPayload returns the bytes to broadcast for an inbound payload, or nil
// if a decoded payload could not be re-encoded. In pass-through mode a payload
// that is valid JSON is returned as its ORIGINAL bytes — no decode/re-encode, so
// key order and numeric precision are preserved and no timestamp/subject is
// injected (the producer owns its envelope). Otherwise the payload is decoded,
// timestamp/subject are injected when absent, and it is re-encoded; a payload that
// is not valid JSON is wrapped in a raw_data envelope (in either mode).
func (w *Output) transformPayload(subject string, data []byte) []byte {
	// Pass-through fast path: pre-validated JSON goes out untouched.
	if w.passthrough && json.Valid(data) {
		return data
	}

	// Default path: decode, inject metadata when absent, re-encode. Non-JSON
	// (including non-JSON on the pass-through branch) falls back to raw_data.
	var msgData map[string]any
	if err := json.Unmarshal(data, &msgData); err != nil {
		msgData = map[string]any{
			"type":      "raw_data",
			"subject":   subject,
			"data":      string(data),
			"timestamp": time.Now().Format(time.RFC3339),
		}
	} else {
		if _, exists := msgData["timestamp"]; !exists {
			msgData["timestamp"] = time.Now().Format(time.RFC3339)
		}
		if _, exists := msgData["subject"]; !exists {
			msgData["subject"] = subject
		}
	}

	jsonData, err := json.Marshal(msgData)
	if err != nil {
		return nil
	}
	return jsonData
}

// runServer runs the HTTP server
func (w *Output) runServer(server *http.Server, listener net.Listener, serveDone chan<- error) {
	if server == nil {
		serveDone <- nil
		close(serveDone)
		return
	}

	var serveErr error
	if w.security.TLS.Server.Enabled {
		serveErr = server.ServeTLS(listener, "", "")
	} else {
		serveErr = server.Serve(listener)
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if serveErr != nil {
		w.errors.Add(1)
	}
	serveDone <- serveErr
	close(serveDone)
}

// handleWebSocket handles new WebSocket connections
func (w *Output) handleWebSocket(wr http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := w.upgrader.Upgrade(wr, r, nil)
	if err != nil {
		w.errors.Add(1)
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
		w.errors.Add(1)
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

	// Keep the hijacked upgrade request admitted until its client exits.
	w.handleClient(r.Context(), conn, clientInfo)
}

// handleClient manages a single WebSocket client connection
func (w *Output) handleClient(ctx context.Context, conn *websocket.Conn, info *clientInfo) {
	defer w.removeClient(conn, info)

	// Set up ping/pong handling for connection health
	conn.SetPongHandler(func(string) error {
		info.lastPing.Store(time.Now())
		return nil
	})

	// Read messages from client (control messages: ack, nack, slow)
	for {
		select {
		case <-ctx.Done():
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
		go w.sendToSingleClient(ctx, &wg, conn, info, messageID, subject, envelopeData)
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
		w.errors.Add(1)
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
func (w *Output) sendToSingleClient(ctx context.Context, wg *sync.WaitGroup, c *websocket.Conn, i *clientInfo, messageID, subject string, envelopeData []byte) {
	defer wg.Done()

	// Setup at-least-once delivery tracking if needed
	ackChan := w.setupPendingMessage(i, messageID, subject, envelopeData)

	// Create timeout context for this send operation
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := w.sendToClient(sendCtx, c, i, envelopeData)
	sendCtxErr := sendCtx.Err()
	cancel()
	if err != nil {
		if sendCtxErr != nil {
			w.handleSendTimeout(c, i, messageID)
			return
		}
		w.handleSendError(c, i, messageID)
		return
	}
	if sendCtxErr != nil {
		w.handleSendTimeout(c, i, messageID)
		return
	}
	w.handleSendSuccess(ctx, i, messageID, subject, envelopeData, ackChan)
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
	w.errors.Add(1)
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
func (w *Output) handleSendSuccess(ctx context.Context, i *clientInfo, messageID, subject string, envelopeData []byte, ackChan chan bool) {
	// Success - update counters atomically
	w.messagesSent.Add(1)
	w.bytesSent.Add(int64(len(envelopeData)))
	if w.metrics != nil {
		w.metrics.messagesSent.WithLabelValues(subject).Inc()
		w.metrics.bytesSent.Add(float64(len(envelopeData)))
		w.metrics.messageSizeBytes.WithLabelValues(subject).Observe(float64(len(envelopeData)))
	}

	// For at-least-once, wait for ack with timeout
	if w.deliveryMode == DeliveryAtLeastOnce && ackChan != nil {
		w.waitForAck(ctx, i, messageID, ackChan)
	}
}

// waitForAck waits for acknowledgment from client with timeout
func (w *Output) waitForAck(ctx context.Context, i *clientInfo, messageID string, ackChan chan bool) {
	ackCtx, ackCancel := context.WithTimeout(ctx, w.ackTimeout)
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
	w.errors.Add(1)
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
func (w *Output) sendToClient(ctx context.Context, conn *websocket.Conn, info *clientInfo, data []byte) error {
	// Lock to prevent concurrent writes to the same connection
	// The gorilla/websocket library panics on concurrent writes
	info.writeMutex.Lock()
	defer info.writeMutex.Unlock()

	// Set write deadline
	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetWriteDeadline(deadline)

	// Send as text message
	return conn.WriteMessage(websocket.TextMessage, data)
}

// pingClient sends a ping frame to a client while holding the per-connection
// write lock. gorilla/websocket panics on concurrent writes, so the background
// ping path must serialize against the frame fan-out path (sendToClient) on the
// same info.writeMutex — otherwise a ping racing a frame write is a hard
// process panic, not a dropped frame (gh#500).
func (w *Output) pingClient(conn *websocket.Conn, info *clientInfo) error {
	info.writeMutex.Lock()
	defer info.writeMutex.Unlock()

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.PingMessage, nil)
}

// maintainClients performs periodic maintenance on client connections
func (w *Output) maintainClients(ctx context.Context, runtimeWG *sync.WaitGroup) {
	defer runtimeWG.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
	default:
	}

	for _, conn := range clientList {
		info := clientInfoMap[conn]
		// Skip if client was closed during iteration
		if info.closed.Load() {
			continue
		}

		if err := w.pingClient(conn, info); err != nil {
			// Client error, remove client
			w.removeClient(conn, info)
			w.errors.Add(1)
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
			return nil, errs.WrapInvalid(err, "websocket-output-factory", "create", "parse config")
		}
		if err := rejectRetiredEndpoint(rawConfig); err != nil {
			return nil, errs.WrapInvalid(err, "websocket-output-factory", "create", "parse config")
		}
	}

	if cfg.Ports == nil || len(cfg.Ports.Inputs) == 0 || len(cfg.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "websocket-output-factory", "create", "at least one input and exactly one output port are required")
	}
	// Parse delivery mode (default: at-most-once).
	deliveryMode := DeliveryAtMostOnce
	if cfg.DeliveryMode != "" {
		deliveryMode = cfg.DeliveryMode
	}

	// Parse ack timeout (default: 5 seconds)
	ackTimeout := 5 * time.Second
	if cfg.AckTimeout != "" {
		parsed, err := time.ParseDuration(cfg.AckTimeout)
		if err != nil {
			return nil, errs.WrapInvalid(err, "websocket-output-factory", "create", "parse ack_timeout")
		}
		ackTimeout = parsed
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig,
			"websocket-output-factory", "create", "NATS client is required")
	}

	// Create constructor config
	ctorCfg := ConstructorConfig{
		Name:            "websocket-output",
		Path:            cfg.Path,
		InputPorts:      cfg.Ports.Inputs,
		OutputPorts:     cfg.Ports.Outputs,
		NATSClient:      deps.NATSClient,
		MetricsRegistry: deps.MetricsRegistry,
		Security:        deps.Security,
		DeliveryMode:    deliveryMode,
		AckTimeout:      ackTimeout,
		Passthrough:     cfg.Passthrough,
	}

	return NewOutputFromConfig(ctorCfg)
}

func rejectRetiredEndpoint(rawConfig json.RawMessage) error {
	var rootFields map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &rootFields); err != nil {
		return nil // SafeUnmarshal reports malformed or incompatible JSON first.
	}
	if _, exists := rootFields["endpoint"]; exists {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "websocket-output-config", "validate", "retired field endpoint is not supported; use path")
	}
	return nil
}
