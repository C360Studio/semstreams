// Package websocket provides WebSocket input component for receiving federated data
package websocket

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/acme"
	"github.com/c360/semstreams/pkg/buffer"
	"github.com/c360/semstreams/pkg/security"
	"github.com/c360/semstreams/pkg/tlsutil"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
)

// Input implements a WebSocket input component that receives federated data
type Input struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	security   security.Config

	// Mode-specific state
	mode Mode

	// Server mode
	httpServer *http.Server
	upgrader   websocket.Upgrader
	clients    map[string]*websocket.Conn
	clientsMu  sync.RWMutex

	// Client mode
	wsClient          *websocket.Conn
	clientMu          sync.Mutex
	reconnectAttempts atomic.Int32

	// Message buffer for backpressure (CircularBuffer with atomic overflow policies)
	messageBuffer buffer.Buffer[*queuedMessage]

	// Request/Reply correlation
	requestMap map[string]chan *MessageEnvelope
	requestMu  sync.RWMutex

	// Output NATS subjects
	dataSubject    string
	controlSubject string

	// Lifecycle management
	shutdown     chan struct{}
	shutdownOnce sync.Once
	done         chan struct{}
	doneOnce     sync.Once
	started      atomic.Bool
	startTime    time.Time
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	lifecycleMu  sync.Mutex
	tlsCleanup   func() // TLS cleanup function (ACME renewal loop)

	// Statistics
	messagesReceived  int64
	messagesPublished int64
	lastActivity      atomic.Value // stores time.Time
	messagesDropped   int64
	connectionsActive int64
	connectionsTotal  int64
	requestsSent      int64
	repliesReceived   int64
	requestTimeouts   int64
	errorCount        atomic.Int64 // Total errors encountered

	// Prometheus metrics
	metrics *Metrics
}

// MessageEnvelope wraps all WebSocket messages with type discrimination
// Supported types:
//   - "data": Application data to be published to NATS
//   - "request": Control plane request (future use)
//   - "reply": Control plane reply (future use)
//   - "ack": Acknowledge successful receipt/processing of data message
//   - "nack": Negative acknowledgment (processing failed, may retry)
//   - "slow": Backpressure signal indicating receiver is overloaded
type MessageEnvelope struct {
	Type      string          `json:"type"`              // Message type (see above)
	ID        string          `json:"id"`                // Unique message ID (for correlation)
	Timestamp int64           `json:"timestamp"`         // Unix milliseconds
	Payload   json.RawMessage `json:"payload,omitempty"` // Optional payload (required for data/nack/slow)
}

// queuedMessage wraps a message envelope with its source connection for ack/nack replies
type queuedMessage struct {
	envelope *MessageEnvelope
	conn     *websocket.Conn // Connection that sent the message (for sending ack/nack)
}

// Ensure Input implements all required interfaces
var (
	_ component.LifecycleComponent = (*Input)(nil)
	_ component.Discoverable       = (*Input)(nil)
)

// Metrics holds Prometheus metrics for Input component
type Metrics struct {
	messagesReceived  *prometheus.CounterVec
	messagesPublished *prometheus.CounterVec
	messagesDropped   *prometheus.CounterVec
	connectionsActive prometheus.Gauge
	connectionsTotal  prometheus.Counter
	reconnectAttempts prometheus.Counter
	requestsSent      *prometheus.CounterVec
	repliesReceived   *prometheus.CounterVec
	requestTimeouts   *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	queueDepth        prometheus.Gauge
	queueUtilization  prometheus.Gauge
	errorsTotal       *prometheus.CounterVec
}

// newMetrics creates and registers Input metrics
func newMetrics(registry *metric.MetricsRegistry, componentName string) *Metrics {
	if registry == nil {
		return nil
	}

	metrics := &Metrics{
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "messages_received_total",
			Help:      "Total messages received via WebSocket",
		}, []string{"component", "type"}),

		messagesPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "messages_published_total",
			Help:      "Total messages published to NATS",
		}, []string{"component", "subject"}),

		messagesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "messages_dropped_total",
			Help:      "Total messages dropped due to backpressure",
		}, []string{"component", "reason"}),

		connectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "connections_active",
			Help:      "Number of active WebSocket connections",
		}),

		connectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "connections_total",
			Help:      "Total number of WebSocket connections",
		}),

		reconnectAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "reconnect_attempts_total",
			Help:      "Total number of reconnection attempts (client mode)",
		}),

		requestsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "requests_sent_total",
			Help:      "Total requests sent (bidirectional mode)",
		}, []string{"component", "method"}),

		repliesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "replies_received_total",
			Help:      "Total replies received (bidirectional mode)",
		}, []string{"component", "status"}),

		requestTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "request_timeouts_total",
			Help:      "Total request timeouts (bidirectional mode)",
		}, []string{"component"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "request_duration_seconds",
			Help:      "Request/reply round-trip duration",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
		}, []string{"component", "method"}),

		queueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "queue_depth",
			Help:      "Current message queue depth",
		}),

		queueUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "queue_utilization",
			Help:      "Message queue utilization (0.0-1.0)",
		}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "websocket_input",
			Name:      "errors_total",
			Help:      "Total errors by type",
		}, []string{"component", "type"}),
	}

	// Register all metrics with the registry
	// CounterVec metrics
	registry.RegisterCounterVec(componentName, "messages_received", metrics.messagesReceived)
	registry.RegisterCounterVec(componentName, "messages_published", metrics.messagesPublished)
	registry.RegisterCounterVec(componentName, "messages_dropped", metrics.messagesDropped)
	registry.RegisterCounterVec(componentName, "requests_sent", metrics.requestsSent)
	registry.RegisterCounterVec(componentName, "replies_received", metrics.repliesReceived)
	registry.RegisterCounterVec(componentName, "request_timeouts", metrics.requestTimeouts)
	registry.RegisterCounterVec(componentName, "errors_total", metrics.errorsTotal)

	// Counter metrics
	registry.RegisterCounter(componentName, "connections_total", metrics.connectionsTotal)
	registry.RegisterCounter(componentName, "reconnect_attempts", metrics.reconnectAttempts)

	// Gauge metrics
	registry.RegisterGauge(componentName, "connections_active", metrics.connectionsActive)
	registry.RegisterGauge(componentName, "queue_depth", metrics.queueDepth)
	registry.RegisterGauge(componentName, "queue_utilization", metrics.queueUtilization)

	// HistogramVec metrics
	registry.RegisterHistogramVec(componentName, "request_duration", metrics.requestDuration)

	return metrics
}

// NewInput creates a new WebSocket input component
func NewInput(
	name string,
	natsClient *natsclient.Client,
	config Config,
	metricsRegistry *metric.MetricsRegistry,
	securityCfg security.Config,
) (*Input, error) {
	// Validate configuration
	if config.Mode != ModeServer && config.Mode != ModeClient {
		return nil, errors.WrapInvalid(
			fmt.Errorf("invalid mode: %s", config.Mode),
			"websocket_input",
			"NewInput",
			"validate mode",
		)
	}

	if config.Mode == ModeServer && config.ServerConfig == nil {
		return nil, errors.WrapInvalid(
			fmt.Errorf("server config required for server mode"),
			"websocket_input",
			"NewInput",
			"validate server config",
		)
	}

	if config.Mode == ModeClient && config.ClientConfig == nil {
		return nil, errors.WrapInvalid(
			fmt.Errorf("client config required for client mode"),
			"websocket_input",
			"NewInput",
			"validate client config",
		)
	}

	// Extract output subjects from port config
	dataSubject := "federated.data"
	controlSubject := "federated.control"
	if config.Ports != nil {
		for _, port := range config.Ports.Outputs {
			if port.Name == "ws_data" {
				dataSubject = port.Subject
			} else if port.Name == "ws_control" {
				controlSubject = port.Subject
			}
		}
	}

	// Create message buffer with configured size and overflow policy
	queueSize := 1000
	overflowPolicy := buffer.DropOldest // default
	if config.Backpressure != nil {
		queueSize = config.Backpressure.QueueSize
		// Map config overflow policy to buffer overflow policy
		switch config.Backpressure.OnFull {
		case "drop_oldest":
			overflowPolicy = buffer.DropOldest
		case "drop_newest":
			overflowPolicy = buffer.DropNewest
		case "block":
			overflowPolicy = buffer.Block
		}
	}

	// Create circular buffer with metrics integration
	var bufferOpts []buffer.Option[*queuedMessage]
	bufferOpts = append(bufferOpts, buffer.WithOverflowPolicy[*queuedMessage](overflowPolicy))
	if metricsRegistry != nil {
		bufferOpts = append(bufferOpts, buffer.WithMetrics[*queuedMessage](metricsRegistry, name))
	}

	messageBuffer, err := buffer.NewCircularBuffer(queueSize, bufferOpts...)
	if err != nil {
		return nil, errors.WrapFatal(err, "websocket_input", "NewInput", "create message buffer")
	}

	input := &Input{
		name:           name,
		config:         config,
		natsClient:     natsClient,
		security:       securityCfg,
		mode:           config.Mode,
		clients:        make(map[string]*websocket.Conn),
		messageBuffer:  messageBuffer,
		requestMap:     make(map[string]chan *MessageEnvelope),
		dataSubject:    dataSubject,
		controlSubject: controlSubject,
		shutdown:       make(chan struct{}),
		done:           make(chan struct{}),
		metrics:        newMetrics(metricsRegistry, name),
	}

	// Configure WebSocket upgrader for server mode
	if config.Mode == ModeServer {
		input.upgrader = websocket.Upgrader{
			ReadBufferSize:  config.ServerConfig.ReadBufferSize,
			WriteBufferSize: config.ServerConfig.WriteBufferSize,
			CheckOrigin: func(_ *http.Request) bool {
				// TODO: Implement proper origin checking based on auth config
				return true
			},
			EnableCompression: config.ServerConfig.EnableCompression,
		}
	}

	return input, nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (i *Input) Meta() component.Metadata {
	return component.Metadata{
		Name:        i.name,
		Type:        "input",
		Description: "WebSocket input for receiving federated data from remote StreamKit instances",
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports (none for input components)
func (i *Input) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns the output ports
func (i *Input) OutputPorts() []component.Port {
	ports := []component.Port{
		{
			Name:        "ws_data",
			Direction:   component.DirectionOutput,
			Required:    true,
			Description: "Data messages received via WebSocket",
			Config: component.NATSPort{
				Subject: i.dataSubject,
			},
		},
		{
			Name:        "ws_control",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Control messages (requests/replies)",
			Config: component.NATSPort{
				Subject: i.controlSubject,
			},
		},
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (i *Input) ConfigSchema() component.ConfigSchema {
	return websocketInputSchema
}

// Health returns current health status
func (i *Input) Health() component.HealthStatus {
	started := i.started.Load()
	healthy := started

	// Check connection state based on mode
	if i.mode == ModeServer {
		// Server mode: healthy if running, even with zero connections
		healthy = started
	} else {
		// Client mode: unhealthy if disconnected
		i.clientMu.Lock()
		connected := i.wsClient != nil
		i.clientMu.Unlock()

		healthy = started && connected
	}

	errorCount := int(i.errorCount.Load())
	uptime := time.Duration(0)
	if started && !i.startTime.IsZero() {
		uptime = time.Since(i.startTime)
	}

	return component.HealthStatus{
		Healthy:    healthy,
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
		LastError:  "",
		Uptime:     uptime,
	}
}

// DataFlow returns current data flow metrics
func (i *Input) DataFlow() component.FlowMetrics {
	messages := atomic.LoadInt64(&i.messagesReceived)

	// Calculate messages per second based on actual uptime
	var messagesPerSecond float64
	if !i.startTime.IsZero() {
		uptime := time.Since(i.startTime).Seconds()
		if uptime > 0 {
			messagesPerSecond = float64(messages) / uptime
		}
	}

	// Get last activity time
	lastAct := time.Time{}
	if val := i.lastActivity.Load(); val != nil {
		lastAct = val.(time.Time)
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSecond,
		BytesPerSecond:    0, // Not tracking bytes
		ErrorRate:         0, // Could calculate from metrics
		LastActivity:      lastAct,
	}
}

// Lifecycle interface implementation

// Initialize initializes the component (no-op for WebSocket input)
func (i *Input) Initialize() error {
	// No initialization needed - everything happens in NewInput and Start
	return nil
}

// Start starts the WebSocket input component
func (i *Input) Start(ctx context.Context) error {
	i.lifecycleMu.Lock()
	defer i.lifecycleMu.Unlock()

	if i.started.Load() {
		return errors.WrapFatal(
			fmt.Errorf("component already started"),
			"websocket_input",
			"Start",
			"check started state",
		)
	}

	// Create component context (local variable, not stored)
	componentCtx, cancel := context.WithCancel(ctx)
	i.cancel = cancel

	// Start message processor goroutine (captures componentCtx)
	i.wg.Add(1)
	go i.processMessages(componentCtx)

	// Start mode-specific logic
	var err error
	if i.mode == ModeServer {
		err = i.startServer(componentCtx)
	} else {
		err = i.startClient(componentCtx)
	}

	if err != nil {
		i.cancel()
		return err
	}

	i.startTime = time.Now()
	i.started.Store(true)
	return nil
}

// Stop stops the WebSocket input component
func (i *Input) Stop(timeout time.Duration) error {
	i.lifecycleMu.Lock()
	defer i.lifecycleMu.Unlock()

	if !i.started.Load() {
		return nil // Already stopped
	}

	// Signal shutdown exactly once
	i.shutdownOnce.Do(func() {
		close(i.shutdown)
	})
	i.cancel()

	// Stop mode-specific logic with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if i.mode == ModeServer {
		i.stopServer(ctx)
	} else {
		i.stopClient()
	}

	// Wait for goroutines with timeout
	doneCh := make(chan struct{})
	go func() {
		i.wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errors.WrapTransient(
			fmt.Errorf("shutdown timeout after %v", timeout),
			"websocket_input",
			"Stop",
			"wait for goroutines",
		)
	}

	// Stop ACME renewal loop if active
	if i.tlsCleanup != nil {
		i.tlsCleanup()
	}

	// Close message buffer after goroutines have stopped
	_ = i.messageBuffer.Close()

	// Close done exactly once
	i.doneOnce.Do(func() {
		close(i.done)
	})
	i.started.Store(false)
	return nil
}

// Process implements component.LifecycleComponent (not used for input components)
func (i *Input) Process(_ any) error {
	return errors.WrapFatal(
		fmt.Errorf("Process() not supported for input components"),
		"websocket_input",
		"Process",
		"unsupported operation",
	)
}

// startServer starts the WebSocket server (Mode: server)
func (i *Input) startServer(ctx context.Context) error {
	cfg := i.config.ServerConfig

	mux := http.NewServeMux()
	// Wrap handleWebSocket in closure to pass context
	mux.HandleFunc(cfg.Path, func(w http.ResponseWriter, r *http.Request) {
		i.handleWebSocket(ctx, w, r)
	})

	i.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	// Configure TLS if enabled at platform level
	if i.security.TLS.Server.Enabled {
		var tlsConfig *tls.Config
		var tlsCleanup func()
		var err error

		// Check if ACME mode is enabled
		if i.security.TLS.Server.Mode == "acme" && i.security.TLS.Server.ACME.Enabled {
			tlsConfig, tlsCleanup, err = tlsutil.LoadServerTLSConfigWithACME(
				ctx,
				i.security.TLS.Server,
			)
			if err != nil {
				return errors.WrapFatal(err, "websocket_input", "startServer",
					"load TLS config with ACME")
			}

			// Store cleanup function for Stop()
			if tlsCleanup != nil {
				i.tlsCleanup = tlsCleanup
			}
		} else {
			// Use manual TLS configuration
			tlsConfig, err = tlsutil.LoadServerTLSConfigWithMTLS(
				i.security.TLS.Server,
				i.security.TLS.Server.MTLS,
			)
			if err != nil {
				return errors.WrapFatal(err, "websocket_input", "startServer",
					"load TLS config with mTLS")
			}
		}

		i.httpServer.TLSConfig = tlsConfig
	}

	// Start HTTP/HTTPS server in goroutine
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		var err error
		if i.security.TLS.Server.Enabled {
			// ListenAndServeTLS with empty cert/key files since TLSConfig is already set
			err = i.httpServer.ListenAndServeTLS("", "")
		} else {
			err = i.httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			// Log error but don't crash
			i.trackError("server_error")
		}
	}()

	return nil
}

// stopServer stops the WebSocket server
func (i *Input) stopServer(ctx context.Context) {
	if i.httpServer != nil {
		_ = i.httpServer.Shutdown(ctx)
	}

	// Close all client connections
	i.clientsMu.Lock()
	for _, conn := range i.clients {
		conn.Close()
	}
	i.clients = make(map[string]*websocket.Conn)
	i.clientsMu.Unlock()
}

// startClient starts the WebSocket client (Mode: client)
func (i *Input) startClient(ctx context.Context) error {
	i.wg.Add(1)
	go i.clientConnectLoop(ctx)
	return nil
}

// stopClient stops the WebSocket client
func (i *Input) stopClient() {
	i.clientMu.Lock()
	if i.wsClient != nil {
		i.wsClient.Close()
		i.wsClient = nil
	}
	i.clientMu.Unlock()
}

// handleWebSocket handles incoming WebSocket connections (server mode)
func (i *Input) handleWebSocket(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Authenticate request
	if !i.authenticateRequest(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		i.trackError("auth_failed")
		return
	}

	// Upgrade connection
	conn, err := i.upgrader.Upgrade(w, r, nil)
	if err != nil {
		i.trackError("upgrade_error")
		return
	}

	// Generate client ID
	clientID := fmt.Sprintf("client-%d", atomic.AddInt64(&i.connectionsTotal, 1))

	// Register client
	i.clientsMu.Lock()
	i.clients[clientID] = conn
	i.clientsMu.Unlock()

	if i.metrics != nil {
		i.metrics.connectionsActive.Inc()
		i.metrics.connectionsTotal.Inc()
	}

	// Handle client connection (captures ctx from closure)
	i.wg.Add(1)
	go i.handleClient(ctx, clientID, conn)
}

// authenticateRequest validates the authentication credentials in the HTTP request
func (i *Input) authenticateRequest(r *http.Request) bool {
	if i.config.Auth == nil || i.config.Auth.Type == "none" {
		return true
	}

	switch i.config.Auth.Type {
	case "bearer":
		expected := os.Getenv(i.config.Auth.BearerTokenEnv)
		if expected == "" {
			return false // Token not configured
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return false
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1

	case "basic":
		username := os.Getenv(i.config.Auth.BasicUsernameEnv)
		password := os.Getenv(i.config.Auth.BasicPasswordEnv)
		if username == "" || password == "" {
			return false // Credentials not configured
		}

		reqUser, reqPass, ok := r.BasicAuth()
		if !ok {
			return false
		}

		userMatch := subtle.ConstantTimeCompare([]byte(reqUser), []byte(username)) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(reqPass), []byte(password)) == 1
		return userMatch && passMatch

	default:
		return false // Unknown auth type
	}
}

// buildAuthHeaders creates HTTP headers with authentication credentials for client mode
func (i *Input) buildAuthHeaders() http.Header {
	headers := http.Header{}

	if i.config.Auth == nil || i.config.Auth.Type == "none" {
		return headers
	}

	switch i.config.Auth.Type {
	case "bearer":
		token := os.Getenv(i.config.Auth.BearerTokenEnv)
		if token != "" {
			headers.Set("Authorization", "Bearer "+token)
		}

	case "basic":
		username := os.Getenv(i.config.Auth.BasicUsernameEnv)
		password := os.Getenv(i.config.Auth.BasicPasswordEnv)
		if username != "" && password != "" {
			auth := username + ":" + password
			encoded := base64.StdEncoding.EncodeToString([]byte(auth))
			headers.Set("Authorization", "Basic "+encoded)
		}
	}

	return headers
}

// trackError increments error counters (both atomic and metrics)
func (i *Input) trackError(errorType string) {
	i.errorCount.Add(1)
	if i.metrics != nil {
		i.metrics.errorsTotal.WithLabelValues(i.name, errorType).Inc()
	}
}

// handleClient handles messages from a connected client
func (i *Input) handleClient(ctx context.Context, clientID string, conn *websocket.Conn) {
	defer i.wg.Done()
	defer func() {
		conn.Close()
		i.clientsMu.Lock()
		delete(i.clients, clientID)
		i.clientsMu.Unlock()
		if i.metrics != nil {
			i.metrics.connectionsActive.Dec()
		}
	}()

	// Set read deadline to ensure responsiveness during shutdown
	readDeadline := 1 * time.Second

	for {
		select {
		case <-i.shutdown:
			return
		case <-ctx.Done():
			return
		default:
			// Set deadline before each read
			conn.SetReadDeadline(time.Now().Add(readDeadline))

			// Read message
			_, message, err := conn.ReadMessage()
			if err != nil {
				// Check if it's a timeout (expected during shutdown)
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Check shutdown signal on next iteration
				}

				i.trackError("read_error")
				return
			}

			// Parse envelope
			envelope, err := i.parseEnvelope(message)
			if err != nil {
				i.trackError("parse_error")
				continue
			}

			// Track last activity
			i.lastActivity.Store(time.Now())

			// Queue message for processing
			i.enqueueMessage(envelope, conn)

			if i.metrics != nil {
				i.metrics.messagesReceived.WithLabelValues(i.name, envelope.Type).Inc()
			}
			atomic.AddInt64(&i.messagesReceived, 1)
		}
	}
}

// clientConnectLoop manages client connection with reconnection logic
func (i *Input) clientConnectLoop(ctx context.Context) {
	defer i.wg.Done()

	cfg := i.config.ClientConfig

	// Create custom dialer with TLS/mTLS support
	dialer := &websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
	}

	// Configure TLS/mTLS/ACME if enabled
	if len(i.security.TLS.Client.CAFiles) > 0 ||
		i.security.TLS.Client.InsecureSkipVerify ||
		i.security.TLS.Client.MinVersion != "" ||
		i.security.TLS.Client.MTLS.Enabled ||
		(i.security.TLS.Client.Mode == "acme" && i.security.TLS.Client.ACME.Enabled) {

		var tlsConfig *tls.Config
		var tlsCleanup func()
		var err error

		// Check if ACME mode is enabled for client
		if i.security.TLS.Client.Mode == "acme" && i.security.TLS.Client.ACME.Enabled {
			tlsConfig, tlsCleanup, err = tlsutil.LoadClientTLSConfigWithACME(
				ctx,
				i.security.TLS.Client,
			)
			if err != nil {
				i.trackError("tls_config_error")
				return
			}

			// Store cleanup function for Stop()
			if tlsCleanup != nil {
				i.tlsCleanup = tlsCleanup
			}
		} else {
			// Use manual TLS configuration
			tlsConfig, err = tlsutil.LoadClientTLSConfigWithMTLS(
				i.security.TLS.Client,
				i.security.TLS.Client.MTLS,
			)
			if err != nil {
				i.trackError("tls_config_error")
				return
			}
		}

		dialer.TLSClientConfig = tlsConfig
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-i.shutdown:
			return
		default:
		}

		// Connect to server with authentication headers
		headers := i.buildAuthHeaders()
		conn, _, err := dialer.Dial(cfg.URL, headers)
		if err != nil {
			i.trackError("connect_error")

			// Handle reconnection
			if !i.shouldReconnect() {
				return
			}

			delay := i.calculateReconnectDelay()
			time.Sleep(delay)
			continue
		}

		// Reset reconnect attempts on successful connection
		i.reconnectAttempts.Store(0)

		i.clientMu.Lock()
		i.wsClient = conn
		i.clientMu.Unlock()

		if i.metrics != nil {
			i.metrics.connectionsActive.Set(1)
			i.metrics.connectionsTotal.Inc()
		}

		// Read messages until disconnect
		i.clientReadLoop(conn)

		// Connection closed
		i.clientMu.Lock()
		i.wsClient = nil
		i.clientMu.Unlock()

		if i.metrics != nil {
			i.metrics.connectionsActive.Set(0)
		}

		// Check if we should reconnect
		if !i.shouldReconnect() {
			return
		}
	}
}

// clientReadLoop reads messages from WebSocket connection (client mode)
func (i *Input) clientReadLoop(conn *websocket.Conn) {
	for {
		select {
		case <-i.shutdown:
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				i.trackError("read_error")
				return
			}

			envelope, err := i.parseEnvelope(message)
			if err != nil {
				i.trackError("parse_error")
				continue
			}

			// Track last activity
			i.lastActivity.Store(time.Now())

			i.enqueueMessage(envelope, conn)

			if i.metrics != nil {
				i.metrics.messagesReceived.WithLabelValues(i.name, envelope.Type).Inc()
			}
			atomic.AddInt64(&i.messagesReceived, 1)
		}
	}
}

// shouldReconnect determines if client should attempt reconnection
func (i *Input) shouldReconnect() bool {
	cfg := i.config.ClientConfig
	if cfg.Reconnect == nil || !cfg.Reconnect.Enabled {
		return false
	}

	current := i.reconnectAttempts.Load()
	if cfg.Reconnect.MaxRetries > 0 && int(current) >= cfg.Reconnect.MaxRetries {
		return false
	}

	i.reconnectAttempts.Add(1)
	if i.metrics != nil {
		i.metrics.reconnectAttempts.Inc()
	}

	return true
}

// calculateReconnectDelay calculates the next reconnection delay with exponential backoff
func (i *Input) calculateReconnectDelay() time.Duration {
	cfg := i.config.ClientConfig.Reconnect
	attempts := i.reconnectAttempts.Load()

	// Exponential backoff: initial * (multiplier ^ attempts)
	delay := cfg.InitialInterval
	for j := int32(0); j < attempts; j++ {
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxInterval {
			return cfg.MaxInterval
		}
	}

	return delay
}

// parseEnvelope parses a WebSocket message into a MessageEnvelope
func (i *Input) parseEnvelope(data []byte) (*MessageEnvelope, error) {
	var envelope MessageEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, errors.WrapInvalid(err, "websocket_input", "parseEnvelope", "unmarshal message")
	}

	// Validate envelope
	if envelope.Type == "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("missing message type"),
			"websocket_input",
			"parseEnvelope",
			"validate envelope",
		)
	}

	return &envelope, nil
}

// enqueueMessage adds a message to the processing buffer with backpressure handling
// CircularBuffer handles overflow policies atomically, eliminating race conditions
func (i *Input) enqueueMessage(envelope *MessageEnvelope, conn *websocket.Conn) {
	qMsg := &queuedMessage{
		envelope: envelope,
		conn:     conn,
	}

	// Write to buffer - overflow policy is handled atomically by CircularBuffer
	err := i.messageBuffer.Write(qMsg)
	if err != nil {
		// Write failed (shouldn't happen with current policies, but track if it does)
		i.trackError("buffer_write_error")
		return
	}

	// Update queue metrics and check for backpressure
	// Note: These metrics are also exported by the buffer itself via WithMetrics option
	cfg := i.config.Backpressure
	if i.metrics != nil && cfg != nil {
		depth := i.messageBuffer.Size()
		capacity := i.messageBuffer.Capacity()
		i.metrics.queueDepth.Set(float64(depth))
		utilization := float64(depth) / float64(capacity)
		i.metrics.queueUtilization.Set(utilization)

		// Send slow signal if queue >80% full
		if utilization > 0.80 {
			i.sendSlowSignal(conn, depth, capacity, utilization)
		}
	}
}

// processMessages processes messages from the buffer and publishes to NATS
func (i *Input) processMessages(ctx context.Context) {
	defer i.wg.Done()
	defer i.drainMessageQueue(ctx)

	// Ticker to prevent busy-waiting when buffer is empty
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-i.shutdown:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Try to read from buffer
			qMsg, ok := i.messageBuffer.Read()
			if ok {
				i.handleMessage(ctx, qMsg.envelope, qMsg.conn)
			}
			// If !ok, buffer is empty - continue waiting
		}
	}
}

// drainMessageQueue processes remaining messages in the buffer during shutdown
func (i *Input) drainMessageQueue(ctx context.Context) {
	// Drain remaining messages with timeout
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-timeout.C:
			// Timeout - stop draining
			return
		default:
			// Try to read from buffer
			qMsg, ok := i.messageBuffer.Read()
			if !ok {
				// Buffer empty
				return
			}
			// Process remaining message
			i.handleMessage(ctx, qMsg.envelope, qMsg.conn)
		}
	}
}

// handleMessage processes a single message envelope
func (i *Input) handleMessage(ctx context.Context, envelope *MessageEnvelope, conn *websocket.Conn) {
	switch envelope.Type {
	case "data":
		// Publish data message to NATS
		if err := i.natsClient.Publish(ctx, i.dataSubject, envelope.Payload); err != nil {
			i.trackError("publish_error")
			// Send nack on failure
			i.sendNack(conn, envelope.ID, "publish_failed", err.Error())
		} else {
			if i.metrics != nil {
				i.metrics.messagesPublished.WithLabelValues(i.name, i.dataSubject).Inc()
			}
			atomic.AddInt64(&i.messagesPublished, 1)
			// Send ack on success
			i.sendAck(conn, envelope.ID)
		}

	case "request":
		// Publish request to control subject
		if err := i.natsClient.Publish(ctx, i.controlSubject+".request", envelope.Payload); err != nil {
			i.trackError("publish_error")
		}

	case "reply":
		// Match reply to pending request
		i.requestMu.Lock()
		replyCh, exists := i.requestMap[envelope.ID]
		if exists {
			delete(i.requestMap, envelope.ID) // Clean up immediately
		}
		i.requestMu.Unlock()

		if exists {
			select {
			case replyCh <- envelope:
				if i.metrics != nil {
					i.metrics.repliesReceived.WithLabelValues(i.name, "ok").Inc()
				}
				atomic.AddInt64(&i.repliesReceived, 1)
			default:
				// Channel full or closed
			}
		}

	case "ack", "nack", "slow":
		// Control messages received from remote - ignore for now
		// These are handled by WebSocket Output when we're the sender

	default:
		i.trackError("unknown_type")
	}
}

// sendAck sends acknowledgment back to the connection
func (i *Input) sendAck(conn *websocket.Conn, messageID string) {
	if conn == nil {
		return
	}

	ack := MessageEnvelope{
		Type:      "ack",
		ID:        messageID,
		Timestamp: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(ack)
	if err != nil {
		return // Silent failure - don't disrupt message processing
	}

	_ = conn.WriteMessage(websocket.TextMessage, data)
}

// sendNack sends negative acknowledgment back to the connection
func (i *Input) sendNack(conn *websocket.Conn, messageID, reason, errorMsg string) {
	if conn == nil {
		return
	}

	nackPayload := map[string]string{
		"reason": reason,
		"error":  errorMsg,
	}
	payload, _ := json.Marshal(nackPayload)

	nack := MessageEnvelope{
		Type:      "nack",
		ID:        messageID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   json.RawMessage(payload),
	}

	data, err := json.Marshal(nack)
	if err != nil {
		return // Silent failure
	}

	_ = conn.WriteMessage(websocket.TextMessage, data)
}

// sendSlowSignal sends backpressure signal when queue is getting full
func (i *Input) sendSlowSignal(conn *websocket.Conn, queueDepth, queueCapacity int, utilization float64) {
	if conn == nil {
		return
	}

	slowPayload := map[string]interface{}{
		"queue_depth":    queueDepth,
		"queue_capacity": queueCapacity,
		"utilization":    utilization,
		"threshold":      0.80,
		"recommendation": "reduce send rate",
	}
	payload, _ := json.Marshal(slowPayload)

	slow := MessageEnvelope{
		Type:      "slow",
		ID:        fmt.Sprintf("bp-%d", time.Now().UnixMilli()),
		Timestamp: time.Now().UnixMilli(),
		Payload:   json.RawMessage(payload),
	}

	data, err := json.Marshal(slow)
	if err != nil {
		return // Silent failure
	}

	// Send slow signal (best effort, don't block)
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

// initACMEClient initializes ACME client from security.ACMEConfig
func initACMEClient(cfg security.ACMEConfig) (*acme.Client, error) {
	renewBefore, err := time.ParseDuration(cfg.RenewBefore)
	if err != nil {
		renewBefore = 8 * time.Hour // Default
	}

	return acme.NewClient(acme.Config{
		DirectoryURL:  cfg.DirectoryURL,
		Email:         cfg.Email,
		Domains:       cfg.Domains,
		ChallengeType: cfg.ChallengeType,
		RenewBefore:   renewBefore,
		StoragePath:   cfg.StoragePath,
		CABundle:      cfg.CABundle,
	})
}
