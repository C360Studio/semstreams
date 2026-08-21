// Package websocket provides WebSocket input component for receiving federated data
package websocket

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/acme"
	"github.com/c360studio/semstreams/pkg/buffer"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/pkg/tlsutil"
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
	listener   net.Listener
	serveDone  chan error
	upgrader   websocket.Upgrader
	clients    map[string]*websocket.Conn
	clientsMu  sync.RWMutex

	// Client mode
	wsClient          *websocket.Conn
	clientMu          sync.Mutex
	clientOpen        bool
	writeMu           sync.Mutex // Protects all conn.WriteMessage calls (gorilla requires exclusive write access)
	reconnectAttempts atomic.Int32
	dialClient        func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)

	// Message buffer for backpressure (CircularBuffer with atomic overflow policies)
	messageBuffer buffer.Buffer[*queuedMessage]

	// Request/Reply correlation
	requestMap map[string]chan *MessageEnvelope
	requestMu  sync.RWMutex

	// Output NATS subjects
	dataSubject      string
	controlSubject   string
	outputPorts      []component.Port
	jetStreamOutputs map[string]bool

	// Lifecycle management
	started             atomic.Bool
	startTime           time.Time
	wg                  sync.WaitGroup
	runtimeDone         chan struct{}
	lifecycleMu         sync.Mutex
	lifecycleUsed       bool
	terminal            bool
	stopping            bool
	cleanupPending      bool
	startDone           chan struct{}
	cancel              context.CancelFunc
	tlsCleanup          func() // TLS cleanup function (ACME renewal loop)
	tlsCleanupMu        sync.Mutex
	admissionMu         sync.Mutex
	requestOpen         bool
	requestCount        int
	requestZero         chan struct{}
	requestHook         func(context.Context)
	clientPublished     func(*websocket.Conn)
	beforeRuntimeCancel func()

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
		return nil, errs.WrapInvalid(
			fmt.Errorf("invalid mode: %s", config.Mode),
			"websocket_input",
			"NewInput",
			"validate mode",
		)
	}

	if config.Mode == ModeServer && config.ServerConfig == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("server config required for server mode"),
			"websocket_input",
			"NewInput",
			"validate server config",
		)
	}

	if config.Mode == ModeClient && config.ClientConfig == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("client config required for client mode"),
			"websocket_input",
			"NewInput",
			"validate client config",
		)
	}

	if config.Ports == nil {
		return nil, errs.WrapInvalid(errors.New("ports configuration is required"), "websocket_input", "NewInput", "resolve output ports")
	}
	outputPorts := make([]component.Port, len(config.Ports.Outputs))
	jetStreamOutputs := make(map[string]bool, len(config.Ports.Outputs))
	var dataSubject, controlSubject string
	for index, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "websocket_input", "NewInput", "resolve output port")
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, errs.WrapInvalid(err, "websocket_input", "NewInput", "project output port facts")
		}
		if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
			return nil, errs.WrapInvalid(fmt.Errorf("output port %q kind %q is not nats or jetstream", port.Name, facts.Kind()), "websocket_input", "NewInput", "validate output port")
		}
		subjects := facts.NATSSubjects()
		if len(subjects) != 1 {
			return nil, errs.WrapInvalid(fmt.Errorf("output port %q declares %d subjects, want one", port.Name, len(subjects)), "websocket_input", "NewInput", "validate output port")
		}
		outputPorts[index] = port
		jetStreamOutputs[subjects[0]] = facts.Kind() == component.PortKindJetStream
		switch port.Name {
		case "ws_data":
			dataSubject = subjects[0]
		case "ws_control":
			controlSubject = subjects[0]
		default:
			return nil, errs.WrapInvalid(fmt.Errorf("unknown output port %q", port.Name), "websocket_input", "NewInput", "validate output port")
		}
	}
	if dataSubject == "" || controlSubject == "" {
		return nil, errs.WrapInvalid(errors.New("ws_data and ws_control output ports are required"), "websocket_input", "NewInput", "validate output ports")
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
		return nil, errs.WrapFatal(err, "websocket_input", "NewInput", "create message buffer")
	}

	input := &Input{
		name:             name,
		config:           config,
		natsClient:       natsClient,
		security:         securityCfg,
		mode:             config.Mode,
		clients:          make(map[string]*websocket.Conn),
		messageBuffer:    messageBuffer,
		requestMap:       make(map[string]chan *MessageEnvelope),
		dataSubject:      dataSubject,
		controlSubject:   controlSubject,
		outputPorts:      outputPorts,
		jetStreamOutputs: jetStreamOutputs,
		metrics:          newMetrics(metricsRegistry, name),
	}

	// Configure WebSocket upgrader for server mode
	if config.Mode == ModeServer {
		allowedOrigins := config.ServerConfig.AllowedOrigins
		input.upgrader = websocket.Upgrader{
			ReadBufferSize:  config.ServerConfig.ReadBufferSize,
			WriteBufferSize: config.ServerConfig.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// If no origin header, allow (same-origin request)
				if origin == "" {
					return true
				}
				// If no allowed origins configured, reject cross-origin requests
				if len(allowedOrigins) == 0 {
					return false
				}
				// Check if origin matches any allowed origin
				for _, allowed := range allowedOrigins {
					if allowed == "*" {
						return true
					}
					if allowed == origin {
						return true
					}
				}
				return false
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
	return nil
}

// OutputPorts returns the output ports
func (i *Input) OutputPorts() []component.Port {
	return append([]component.Port(nil), i.outputPorts...)
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
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "websocket_input", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "websocket_input", "Start", "context already cancelled")
	}

	i.lifecycleMu.Lock()
	if i.lifecycleUsed {
		i.lifecycleMu.Unlock()
		return errs.WrapFatal(
			errs.ErrAlreadyStarted,
			"websocket_input",
			"Start",
			"cleanup authority already active",
		)
	}
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	i.lifecycleUsed = true
	i.cleanupPending = true
	i.cancel = cancel
	i.startDone = startDone
	i.lifecycleMu.Unlock()

	// Start mode-specific logic
	var err error
	if i.mode == ModeServer {
		err = i.startServer(runCtx)
	} else {
		err = nil
	}

	if err != nil {
		cancel()
		if cleanup := i.takeTLSCleanup(); cleanup != nil {
			cleanup()
		}
		i.lifecycleMu.Lock()
		i.cleanupPending = false
		i.terminal = true
		i.cancel = nil
		close(startDone)
		i.startDone = nil
		i.httpServer = nil
		i.listener = nil
		i.serveDone = nil
		i.lifecycleMu.Unlock()
		return err
	}

	i.admissionMu.Lock()
	i.requestOpen = true
	i.admissionMu.Unlock()
	if i.mode == ModeClient {
		i.clientMu.Lock()
		i.clientOpen = true
		i.clientMu.Unlock()
	}

	workers := 1 // processMessages
	if i.mode == ModeClient {
		workers++
	}
	i.wg.Add(workers)
	go i.processMessages(runCtx)
	if i.mode == ModeClient {
		go i.clientConnectLoop(runCtx)
	}
	runtimeDone := make(chan struct{})
	i.runtimeDone = runtimeDone
	go func() {
		i.wg.Wait()
		close(runtimeDone)
	}()

	i.startTime = time.Now()
	i.started.Store(true)
	i.lifecycleMu.Lock()
	i.cleanupPending = false
	close(startDone)
	i.startDone = nil
	i.lifecycleMu.Unlock()
	return nil
}

// Stop stops the WebSocket input component
func (i *Input) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	for {
		i.lifecycleMu.Lock()
		if !i.lifecycleUsed {
			i.lifecycleUsed = true
			i.terminal = true
			i.lifecycleMu.Unlock()
			return nil
		}
		if i.terminal {
			i.lifecycleMu.Unlock()
			return nil
		}
		if i.startDone != nil {
			done := i.startDone
			i.lifecycleMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if i.stopping {
			i.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "websocket_input", "Stop", "concurrent Stop")
		}
		i.stopping = true
		server := i.httpServer
		serveDone := i.serveDone
		cancel := i.cancel
		runtimeDone := i.runtimeDone
		i.lifecycleMu.Unlock()

		requestZero := i.fenceRequests()
		var stopErr error
		if i.mode == ModeServer {
			if server != nil {
				stopErr = errors.Join(stopErr, errs.NewShutdownError("websocket-input", errs.PhaseShutdownListener, server.Shutdown(ctx)))
			}
			if serveDone != nil {
				select {
				case err := <-serveDone:
					stopErr = errors.Join(stopErr, err)
				case <-ctx.Done():
					stopErr = errors.Join(stopErr, ctx.Err())
				}
			}
			i.closeServerClients()
			select {
			case <-requestZero:
			case <-ctx.Done():
				stopErr = errors.Join(stopErr, ctx.Err())
			}
		} else {
			i.stopClient()
		}
		if i.beforeRuntimeCancel != nil {
			i.beforeRuntimeCancel()
		}
		if cancel != nil {
			cancel()
		}
		if runtimeDone != nil {
			select {
			case <-runtimeDone:
			case <-ctx.Done():
				stopErr = errors.Join(stopErr, ctx.Err())
			}
		}
		if cleanup := i.takeTLSCleanup(); cleanup != nil {
			cleanup()
		}
		stopErr = errors.Join(stopErr, i.messageBuffer.Close())

		i.lifecycleMu.Lock()
		i.stopping = false
		i.terminal = true
		i.cleanupPending = false
		i.cancel = nil
		i.runtimeDone = nil
		i.httpServer = nil
		i.listener = nil
		i.serveDone = nil
		i.lifecycleMu.Unlock()
		i.started.Store(false)
		return attributeComponentShutdownError("websocket-input", errs.PhaseJoinRuntime, stopErr)
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

func (i *Input) admitRequest() bool {
	i.admissionMu.Lock()
	defer i.admissionMu.Unlock()
	if !i.requestOpen {
		return false
	}
	if i.requestCount == 0 {
		i.requestZero = make(chan struct{})
	}
	i.requestCount++
	return true
}

func (i *Input) releaseRequest() {
	i.admissionMu.Lock()
	defer i.admissionMu.Unlock()
	i.requestCount--
	if i.requestCount == 0 {
		close(i.requestZero)
	}
}

func (i *Input) fenceRequests() <-chan struct{} {
	i.admissionMu.Lock()
	defer i.admissionMu.Unlock()
	i.requestOpen = false
	if i.requestCount == 0 {
		done := make(chan struct{})
		close(done)
		return done
	}
	return i.requestZero
}

func (i *Input) setTLSCleanup(cleanup func()) {
	i.tlsCleanupMu.Lock()
	previous := i.tlsCleanup
	i.tlsCleanup = cleanup
	i.tlsCleanupMu.Unlock()
	if previous != nil {
		previous()
	}
}

func (i *Input) takeTLSCleanup() func() {
	i.tlsCleanupMu.Lock()
	cleanup := i.tlsCleanup
	i.tlsCleanup = nil
	i.tlsCleanupMu.Unlock()
	return cleanup
}

// Process implements component.LifecycleComponent (not used for input components)
func (i *Input) Process(_ any) error {
	return errs.WrapFatal(
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
	mux.HandleFunc(cfg.Path, func(w http.ResponseWriter, r *http.Request) {
		if !i.admitRequest() {
			http.Error(w, "service stopping", http.StatusServiceUnavailable)
			return
		}
		defer i.releaseRequest()
		if i.requestHook != nil {
			i.requestHook(r.Context())
		}
		i.handleWebSocket(r.Context(), w, r)
	})

	i.httpServer = &http.Server{
		Addr:        fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return ctx },
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
				return errs.WrapFatal(err, "websocket_input", "startServer",
					"load TLS config with ACME")
			}

			// Store cleanup function for Stop()
			if tlsCleanup != nil {
				i.setTLSCleanup(tlsCleanup)
			}
		} else {
			// Use manual TLS configuration
			tlsConfig, err = tlsutil.LoadServerTLSConfigWithMTLS(
				i.security.TLS.Server,
				i.security.TLS.Server.MTLS,
			)
			if err != nil {
				return errs.WrapFatal(err, "websocket_input", "startServer",
					"load TLS config with mTLS")
			}
		}

		i.httpServer.TLSConfig = tlsConfig
	}

	listener, err := net.Listen("tcp", i.httpServer.Addr)
	if err != nil {
		return errs.WrapFatal(err, "websocket_input", "startServer", "bind HTTP listener")
	}
	i.listener = listener
	serveDone := make(chan error, 1)
	i.serveDone = serveDone
	go func() {
		var serveErr error
		if i.security.TLS.Server.Enabled {
			serveErr = i.httpServer.ServeTLS(listener, "", "")
		} else {
			serveErr = i.httpServer.Serve(listener)
		}
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		if serveErr != nil {
			i.trackError("server_error")
		}
		serveDone <- serveErr
		close(serveDone)
	}()

	return nil
}

func (i *Input) closeServerClients() {
	i.clientsMu.Lock()
	for _, conn := range i.clients {
		_ = conn.Close()
	}
	i.clients = make(map[string]*websocket.Conn)
	i.clientsMu.Unlock()
}

// stopClient stops the WebSocket client
func (i *Input) stopClient() {
	i.clientMu.Lock()
	i.clientOpen = false
	client := i.wsClient
	i.wsClient = nil
	i.clientMu.Unlock()
	if client != nil {
		_ = client.Close()
	}
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

	// Keep the upgraded request admitted until the hijacked connection exits.
	i.handleClient(ctx, clientID, conn)
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
				i.setTLSCleanup(tlsCleanup)
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
		if ctx.Err() != nil || !i.clientAdmissionOpen() {
			return
		}

		// Connect to server with authentication headers
		headers := i.buildAuthHeaders()
		dial := dialer.DialContext
		if i.dialClient != nil {
			dial = i.dialClient
		}
		conn, _, err := dial(ctx, cfg.URL, headers)
		if err != nil {
			if ctx.Err() != nil || !i.clientAdmissionOpen() {
				return
			}
			i.trackError("connect_error")

			// Handle reconnection
			if !i.shouldReconnect() {
				return
			}

			delay := i.calculateReconnectDelay()
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			}
			continue
		}

		// Reset reconnect attempts on successful connection
		i.reconnectAttempts.Store(0)

		i.clientMu.Lock()
		if !i.clientOpen || ctx.Err() != nil {
			i.clientMu.Unlock()
			_ = conn.Close()
			return
		}
		i.wsClient = conn
		i.clientMu.Unlock()
		if i.clientPublished != nil {
			i.clientPublished(conn)
		}

		if i.metrics != nil {
			i.metrics.connectionsActive.Set(1)
			i.metrics.connectionsTotal.Inc()
		}

		// Read messages until disconnect
		i.clientReadLoop(ctx, conn)
		_ = conn.Close()

		// Connection closed
		i.clientMu.Lock()
		if i.wsClient == conn {
			i.wsClient = nil
		}
		clientOpen := i.clientOpen
		i.clientMu.Unlock()

		if i.metrics != nil {
			i.metrics.connectionsActive.Set(0)
		}

		// Check if we should reconnect
		if !clientOpen || ctx.Err() != nil || !i.shouldReconnect() {
			return
		}
	}
}

func (i *Input) clientAdmissionOpen() bool {
	i.clientMu.Lock()
	open := i.clientOpen
	i.clientMu.Unlock()
	return open
}

// clientReadLoop reads messages from WebSocket connection (client mode)
func (i *Input) clientReadLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
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
		return nil, errs.WrapInvalid(err, "websocket_input", "parseEnvelope", "unmarshal message")
	}

	// Validate envelope
	if envelope.Type == "" {
		return nil, errs.WrapInvalid(
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

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (i *Input) isJetStreamPortBySubject(subject string) bool {
	return i.jetStreamOutputs[subject]
}

// handleMessage processes a single message envelope
func (i *Input) handleMessage(ctx context.Context, envelope *MessageEnvelope, conn *websocket.Conn) {
	switch envelope.Type {
	case "data":
		// Publish data message to NATS, respecting port type configuration
		var publishErr error
		if i.isJetStreamPortBySubject(i.dataSubject) {
			publishErr = i.natsClient.PublishToStream(ctx, i.dataSubject, envelope.Payload)
		} else {
			publishErr = i.natsClient.Publish(ctx, i.dataSubject, envelope.Payload)
		}
		if publishErr != nil {
			i.trackError("publish_error")
			// Send nack on failure
			i.sendNack(conn, envelope.ID, "publish_failed", publishErr.Error())
		} else {
			if i.metrics != nil {
				i.metrics.messagesPublished.WithLabelValues(i.name, i.dataSubject).Inc()
			}
			atomic.AddInt64(&i.messagesPublished, 1)
			// Send ack on success
			i.sendAck(conn, envelope.ID)
		}

	case "request":
		// Publish request to control subject, respecting port type configuration
		controlReqSubject := i.controlSubject + ".request"
		var publishErr error
		if i.isJetStreamPortBySubject(i.controlSubject) {
			publishErr = i.natsClient.PublishToStream(ctx, controlReqSubject, envelope.Payload)
		} else {
			publishErr = i.natsClient.Publish(ctx, controlReqSubject, envelope.Payload)
		}
		if publishErr != nil {
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

	i.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, data)
	i.writeMu.Unlock()
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

	i.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, data)
	i.writeMu.Unlock()
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

	// Send slow signal (best effort)
	i.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, data)
	i.writeMu.Unlock()
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
