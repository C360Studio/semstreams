// Package udp provides UDP input component for receiving data from external sources
package udp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/buffer"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for UDP input component
type Metrics struct {
	packetsReceived   prometheus.Counter
	bytesReceived     prometheus.Counter
	packetsDropped    prometheus.Counter
	bufferUtilization prometheus.Gauge
	batchSize         prometheus.Histogram
	publishLatency    prometheus.Histogram
	socketErrors      prometheus.Counter
	lastActivity      prometheus.Gauge
}

// newMetrics creates and registers UDP input metrics
func newMetrics(registry *metric.MetricsRegistry, port int, _ string) *Metrics {
	// Return nil if no registry provided (nil input = nil feature pattern)
	if registry == nil {
		return nil
	}

	// Only create metrics when registry is provided
	metrics := &Metrics{
		packetsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "packets_received_total",
			Help:      "Total UDP packets received",
		}),
		bytesReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "bytes_received_total",
			Help:      "Total bytes received from UDP",
		}),
		packetsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "packets_dropped_total",
			Help:      "Packets dropped due to buffer full",
		}),
		bufferUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "buffer_utilization_ratio",
			Help:      "Buffer usage (0-1) showing backpressure",
		}),
		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "batch_size",
			Help:      "Distribution of processing batch sizes",
			Buckets:   []float64{1, 5, 10, 20, 50, 100, 200, 500},
		}),
		publishLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "publish_duration_seconds",
			Help:      "Time to publish batches to NATS",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		}),
		socketErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "socket_errors_total",
			Help:      "Socket read errors encountered",
		}),
		lastActivity: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "udp",
			Name:      "last_activity_timestamp",
			Help:      "Unix timestamp of last received packet",
		}),
	}

	// Register all metrics (no conditional needed since registry is guaranteed non-nil here)
	serviceName := fmt.Sprintf("udp_%d", port)
	registry.RegisterCounter(serviceName, "packets_received", metrics.packetsReceived)
	registry.RegisterCounter(serviceName, "bytes_received", metrics.bytesReceived)
	registry.RegisterCounter(serviceName, "packets_dropped", metrics.packetsDropped)
	registry.RegisterGauge(serviceName, "buffer_utilization", metrics.bufferUtilization)
	registry.RegisterHistogram(serviceName, "batch_size", metrics.batchSize)
	registry.RegisterHistogram(serviceName, "publish_latency", metrics.publishLatency)
	registry.RegisterCounter(serviceName, "socket_errors", metrics.socketErrors)
	registry.RegisterGauge(serviceName, "last_activity", metrics.lastActivity)

	return metrics
}

// Input implements a UDP listener that publishes received data to NATS
// This is specifically designed for receiving MAVLink messages on port 14550
type Input struct {
	name            string
	port            int
	bind            string
	subject         string
	config          InputConfig // Store full config for port type checking
	inputPorts      []component.Port
	outputPorts     []component.Port
	jetStreamOutput bool
	natsClient      *natsclient.Client
	logger          *slog.Logger // Structured logger

	// Buffer for incoming messages
	buffer buffer.Buffer[[]byte]

	// Retry configuration
	retryConfig retry.Config

	// Lifecycle management
	shutdown  chan struct{}
	done      chan struct{}
	running   atomic.Bool
	startTime time.Time
	mu        sync.RWMutex
	wg        sync.WaitGroup
	conn      *net.UDPConn

	// Metrics (atomic for thread safety)
	messagesReceived atomic.Int64
	bytesReceived    atomic.Int64
	errors           atomic.Int64
	lastActivity     atomic.Value // stores time.Time

	// Prometheus metrics
	metrics *Metrics
}

// Ensure Input implements all required interfaces
var _ component.Discoverable = (*Input)(nil)
var _ component.LifecycleComponent = (*Input)(nil)

// udpSchema defines the configuration schema for UDP input component
// Generated from InputConfig struct tags using reflection
var udpSchema = component.GenerateConfigSchema(reflect.TypeOf(InputConfig{}))

// InputConfig holds configuration for UDP input component
type InputConfig struct {
	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// Validate implements component.Validatable interface for secure config validation
func (c *InputConfig) Validate() error {
	if c.Ports == nil || len(c.Ports.Inputs) != 1 || len(c.Ports.Outputs) != 1 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "InputConfig", "Validate", "exactly one input and one output port are required")
	}
	input, err := c.Ports.Inputs[0].Resolve(component.DirectionInput)
	if err != nil {
		return errs.WrapInvalid(err, "InputConfig", "Validate", "resolve input port")
	}
	inputFacts, err := input.Facts()
	if err != nil {
		return errs.WrapInvalid(err, "InputConfig", "Validate", "project input port facts")
	}
	if inputFacts.Kind() != component.PortKindNetwork {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "InputConfig", "Validate", "input port must be network")
	}
	networkPort, ok := inputFacts.Network()
	if !ok || networkPort.Protocol() != "udp" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "InputConfig", "Validate", "input network protocol must be udp")
	}
	if err := component.ValidateNetworkConfig(networkPort.Port(), networkPort.Host()); err != nil {
		return errs.Wrap(err, "InputConfig", "Validate", "network port validation")
	}
	output, err := c.Ports.Outputs[0].Resolve(component.DirectionOutput)
	if err != nil {
		return errs.WrapInvalid(err, "InputConfig", "Validate", "resolve output port")
	}
	outputFacts, err := output.Facts()
	if err != nil {
		return errs.WrapInvalid(err, "InputConfig", "Validate", "project output port facts")
	}
	if outputFacts.Kind() != component.PortKindNATS && outputFacts.Kind() != component.PortKindJetStream {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "InputConfig", "Validate", "output port must be nats or jetstream")
	}
	if len(outputFacts.NATSSubjects()) != 1 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "InputConfig", "Validate", "output port must declare exactly one NATS subject")
	}

	return nil
}

// DefaultConfig returns sensible defaults for UDP input
func DefaultConfig() InputConfig {
	return InputConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "udp_socket", Config: component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550}, Required: true,
					Description: "UDP socket listening for incoming data",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "nats_output", Config: component.JetStreamPort{Subjects: []string{"input.udp.mavlink"}}, Required: true,
					Description: "NATS subject for publishing received UDP data",
				},
			},
		},
	}
}

// getConfiguredPorts extracts port configuration from config
func (c *InputConfig) getConfiguredPorts() (int, string, string, []component.Port, []component.Port, bool, error) {
	input, err := c.Ports.Inputs[0].Resolve(component.DirectionInput)
	if err != nil {
		return 0, "", "", nil, nil, false, err
	}
	inputFacts, err := input.Facts()
	if err != nil {
		return 0, "", "", nil, nil, false, err
	}
	networkPort, ok := inputFacts.Network()
	if !ok {
		return 0, "", "", nil, nil, false, fmt.Errorf("input port %q is not network", input.Name)
	}
	output, err := c.Ports.Outputs[0].Resolve(component.DirectionOutput)
	if err != nil {
		return 0, "", "", nil, nil, false, err
	}
	facts, err := output.Facts()
	if err != nil {
		return 0, "", "", nil, nil, false, err
	}
	return networkPort.Port(), networkPort.Host(), facts.NATSSubjects()[0], []component.Port{input}, []component.Port{output}, facts.Kind() == component.PortKindJetStream, nil
}

// InputDeps holds runtime dependencies for UDP input component
type InputDeps struct {
	Name            string                  // Instance name
	Config          InputConfig             // Business logic configuration
	NATSClient      *natsclient.Client      // Runtime dependency
	MetricsRegistry *metric.MetricsRegistry // Runtime dependency
	Logger          *slog.Logger            // Runtime dependency
}

// NewInput creates a new UDP input component using idiomatic Go constructor pattern.
// Returns an error if buffer creation fails.
func NewInput(deps InputDeps) (*Input, error) {
	if deps.Config.Ports == nil {
		deps.Config = DefaultConfig()
	}
	if err := deps.Config.Validate(); err != nil {
		return nil, err
	}
	// Create buffer with high-capacity settings for concurrent message handling using functional options
	var bufferOpts []buffer.Option[[]byte]
	bufferOpts = append(bufferOpts, buffer.WithOverflowPolicy[[]byte](buffer.DropOldest))

	// Add metrics if registry is provided
	if deps.MetricsRegistry != nil {
		bufferOpts = append(bufferOpts, buffer.WithMetrics[[]byte](deps.MetricsRegistry, "udp_input"))
	}

	// Extract port values from configuration
	port, bind, subject, inputPorts, outputPorts, jetStreamOutput, err := deps.Config.getConfiguredPorts()
	if err != nil {
		return nil, errs.WrapInvalid(err, "udp-input", "NewInput", "resolve configured ports")
	}

	// Create Prometheus metrics if registry provided
	var metrics *Metrics
	if deps.MetricsRegistry != nil {
		metrics = newMetrics(deps.MetricsRegistry, port, bind)
	}

	// Use provided logger or default
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "udp-input", "port", port)
	}

	// Create buffer with error handling for the new API
	messageBuffer, err := buffer.NewCircularBuffer(5000, bufferOpts...) // 5000 capacity for concurrent load
	if err != nil {
		return nil, errs.Wrap(err, "udp-input", "NewInput", "create message buffer")
	}

	u := &Input{
		name:            deps.Name,
		port:            port,
		bind:            bind,
		subject:         subject,
		config:          deps.Config, // Store full config for port type checking
		inputPorts:      inputPorts,
		outputPorts:     outputPorts,
		jetStreamOutput: jetStreamOutput,
		natsClient:      deps.NATSClient,
		logger:          logger,
		buffer:          messageBuffer,
		retryConfig:     retry.DefaultConfig(),
		startTime:       time.Now(),
		metrics:         metrics,
	}
	u.lastActivity.Store(time.Time{})
	return u, nil
}

// Meta returns the component metadata
func (u *Input) Meta() component.Metadata {
	// Use provided name if available, otherwise fall back to default naming
	name := u.name
	if name == "" {
		name = fmt.Sprintf("udp-input-%d", u.port)
	}

	return component.Metadata{
		Name:        name,
		Type:        "input",
		Description: fmt.Sprintf("UDP input listener on %s:%d publishing to %s", u.bind, u.port, u.subject),
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports for this component
func (u *Input) InputPorts() []component.Port {
	return append([]component.Port(nil), u.inputPorts...)
}

// OutputPorts returns the output ports for this component
func (u *Input) OutputPorts() []component.Port {
	return append([]component.Port(nil), u.outputPorts...)
}

// ConfigSchema returns the configuration schema for this component
// References the package-level udpSchema variable for efficient retrieval
func (u *Input) ConfigSchema() component.ConfigSchema {
	return udpSchema
}

// Health returns the current health status of the component
func (u *Input) Health() component.HealthStatus {
	u.mu.RLock()
	running := u.running.Load()
	connected := u.conn != nil
	u.mu.RUnlock()

	errorCount := u.errors.Load()
	healthy := running && connected

	return component.HealthStatus{
		Healthy:    healthy,
		LastCheck:  time.Now(),
		ErrorCount: int(errorCount),
		LastError:  "",
		Uptime:     time.Since(u.startTime),
	}
}

// DataFlow returns the current data flow metrics
func (u *Input) DataFlow() component.FlowMetrics {
	messages := u.messagesReceived.Load()
	bytes := u.bytesReceived.Load()
	errorCount := u.errors.Load()
	lastActivity, _ := u.lastActivity.Load().(time.Time)

	var messagesPerSecond float64
	var bytesPerSecond float64
	var errorRate float64

	if uptime := time.Since(u.startTime).Seconds(); uptime > 0 {
		messagesPerSecond = float64(messages) / uptime
		bytesPerSecond = float64(bytes) / uptime
	}

	if messages > 0 {
		errorRate = float64(errorCount) / float64(messages)
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSecond,
		BytesPerSecond:    bytesPerSecond,
		ErrorRate:         errorRate,
		LastActivity:      lastActivity,
	}
}

// Initialize prepares the UDP input component but does not start listening
func (u *Input) Initialize() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Validate configuration (0 is allowed for OS auto-assignment)
	if u.port < 0 || u.port > 65535 {
		return errs.WrapInvalid(fmt.Errorf("invalid port %d", u.port),
			"udp-input", "Initialize", "port validation")
	}

	if u.subject == "" {
		return errs.WrapInvalid(fmt.Errorf("empty subject"),
			"udp-input", "Initialize", "subject validation")
	}

	if u.natsClient == nil {
		return errs.WrapInvalid(fmt.Errorf("nil NATS client"),
			"udp-input", "Initialize", "NATS client validation")
	}

	return nil
}

// Start begins listening for UDP packets and publishing to NATS
func (u *Input) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "udp-input", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "udp-input", "Start", "context already cancelled")
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	if u.running.Load() {
		return nil // Already running, idempotent
	}

	// Create shutdown channels for coordinated shutdown
	u.shutdown = make(chan struct{})
	u.done = make(chan struct{})

	// Use retry for socket binding
	bindOperation := func() error {
		return u.bindSocket()
	}

	if err := retry.Do(ctx, u.retryConfig, bindOperation); err != nil {
		u.cleanupUnlocked()
		return errs.WrapTransient(err, "udp-input", "Start", "socket binding")
	}

	u.running.Store(true)
	u.startTime = time.Now()

	// Start the read loop in a goroutine with WaitGroup
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		defer func() {
			u.mu.Lock()
			defer u.mu.Unlock()
			if u.done != nil {
				select {
				case <-u.done:
				default:
					close(u.done)
				}
			}
		}()
		u.readLoop(ctx)
	}()

	return nil
}

// bindSocket creates and binds the UDP socket
func (u *Input) bindSocket() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", u.bind, u.port))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address %s:%d: %w", u.bind, u.port, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", u.port, err)
	}

	// Increase OS socket buffer for high throughput to prevent drops
	const socketBufferSize = 2 * 1024 * 1024 // 2MB buffer
	if err := conn.SetReadBuffer(socketBufferSize); err != nil {
		// Log warning but don't fail - some systems limit buffer size
		if u.logger != nil {
			u.logger.Warn("Could not set UDP buffer size",
				"buffer_size", socketBufferSize,
				"port", u.port,
				"error", err)
		}
	}

	u.conn = conn
	return nil
}

// Stop gracefully stops the UDP listener with the specified timeout
func (u *Input) Stop(timeout time.Duration) error {
	return u.StopWithTimeout(timeout)
}

// StopWithTimeout gracefully stops the UDP listener with the specified timeout
func (u *Input) StopWithTimeout(timeout time.Duration) error {
	if !u.running.Load() {
		return nil
	}

	u.running.Store(false)

	// Signal shutdown to goroutines
	u.mu.Lock()
	if u.shutdown != nil {
		select {
		case <-u.shutdown:
		default:
			close(u.shutdown)
		}
	}
	// Close UDP connection to unblock readLoop
	if u.conn != nil {
		_ = u.conn.Close()
	}
	u.mu.Unlock()

	// Wait for graceful shutdown with timeout
	select {
	case <-u.done:
		// Goroutine finished cleanly
	case <-time.After(timeout):
		return errs.WrapTransient(fmt.Errorf("stop timeout after %v", timeout),
			"udp-input", "Stop", "graceful shutdown")
	}

	// Clean up resources
	u.cleanup()
	return nil
}

// cleanup cleans up resources
func (u *Input) cleanup() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.cleanupUnlocked()
}

// cleanupUnlocked cleans up resources without acquiring the mutex
// This is used when the mutex is already held (e.g., in Start method)
func (u *Input) cleanupUnlocked() {
	if u.shutdown != nil {
		select {
		case <-u.shutdown:
		default:
			close(u.shutdown)
		}
		u.shutdown = nil
	}
	if u.done != nil {
		u.done = nil
	}
	if u.conn != nil {
		_ = u.conn.Close()
		u.conn = nil
	}
	if u.buffer != nil {
		_ = u.buffer.Close()
	}
}

// readLoop continuously reads UDP packets and publishes them to NATS
func (u *Input) readLoop(ctx context.Context) {
	udpBuffer := make([]byte, 65536) // Larger buffer to handle any UDP packet size

	for u.running.Load() {
		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		case <-u.shutdown:
			return
		default:
		}

		// Get connection under lock and check if we should continue
		u.mu.RLock()
		if !u.running.Load() || u.conn == nil {
			u.mu.RUnlock()
			return
		}
		conn := u.conn
		u.mu.RUnlock()

		// Set read deadline to check shutdown periodically
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, _, err := conn.ReadFromUDP(udpBuffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is expected, continue the loop
				continue
			}

			// Check if stopped
			select {
			case <-ctx.Done():
				return
			case <-u.shutdown:
				return
			default:
				// Handle network errors gracefully - no panics
				u.errors.Add(1)

				// Record socket error metric
				if u.metrics != nil {
					u.metrics.socketErrors.Inc()
				}

				// For non-recoverable errors, exit gracefully
				if !errs.IsTransient(err) {
					return
				}
				continue
			}
		}

		// Update metrics atomically
		u.messagesReceived.Add(1)
		u.bytesReceived.Add(int64(n))
		now := time.Now()
		u.lastActivity.Store(now)

		// Update Prometheus metrics if available
		if u.metrics != nil {
			u.metrics.packetsReceived.Inc()
			u.metrics.bytesReceived.Add(float64(n))
			u.metrics.lastActivity.Set(float64(now.Unix()))
		}

		// Create a copy of the received data
		data := make([]byte, n)
		copy(data, udpBuffer[:n])

		// Buffer the message
		if err := u.buffer.Write(data); err != nil {
			// Buffer full or error, record packet drop
			if u.metrics != nil {
				u.metrics.packetsDropped.Inc()
			}
			continue
		}

		// Update buffer utilization metric if available
		if u.metrics != nil {
			// Get buffer stats if available (assuming buffer has size info)
			// This is a simple approximation - ideally buffer would expose stats
			u.metrics.bufferUtilization.Set(0.5) // Placeholder - would need actual buffer size
		}

		// Process buffered messages
		u.processBufferedMessages(ctx)
	}
}

// processBufferedMessages processes all buffered messages and publishes to NATS
func (u *Input) processBufferedMessages(ctx context.Context) {
	// Process messages in batches to avoid holding the buffer for too long
	// TODO(v1-beta): Make batch size configurable via component config
	// TODO(v1-beta): Add worker pool for concurrent message processing
	const maxBatchSize = 100 // Increased from 10 to handle high-throughput scenarios
	messages := u.buffer.ReadBatch(maxBatchSize)

	// Record batch size metric
	if u.metrics != nil && len(messages) > 0 {
		u.metrics.batchSize.Observe(float64(len(messages)))
	}

	for _, data := range messages {
		if !u.running.Load() {
			break
		}

		// Publish to NATS with retry
		publishOperation := func() error {
			return u.publishToNATS(ctx, data)
		}

		if err := retry.Do(ctx, u.retryConfig, publishOperation); err != nil {
			u.errors.Add(1)
			// Continue processing other messages even if one fails
		}
	}
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (u *Input) isJetStreamPortBySubject(subject string) bool {
	return subject == u.subject && u.jetStreamOutput
}

// publishToNATS publishes the received data to the configured NATS subject
func (u *Input) publishToNATS(ctx context.Context, data []byte) error {
	if u.natsClient == nil {
		return errs.WrapInvalid(fmt.Errorf("NATS client not available"),
			"udp-input", "publishToNATS", "NATS client check")
	}

	// Get the underlying NATS connection
	nc := u.natsClient.GetConnection()
	if nc == nil {
		return errs.WrapTransient(fmt.Errorf("NATS connection not available"),
			"udp-input", "publishToNATS", "NATS connection check")
	}

	// Measure publish latency if metrics are enabled
	var start time.Time
	if u.metrics != nil {
		start = time.Now()
	}

	// Publish raw data to NATS, respecting port type configuration
	var publishErr error
	if u.isJetStreamPortBySubject(u.subject) {
		publishErr = u.natsClient.PublishToStream(ctx, u.subject, data)
	} else {
		// Fallback to core NATS for non-JetStream ports
		publishErr = nc.Publish(u.subject, data)
	}
	if publishErr != nil {
		return errs.WrapTransient(publishErr, "udp-input", "publishToNATS", "NATS publish")
	}

	// Record publish latency metric
	if u.metrics != nil {
		duration := time.Since(start)
		u.metrics.publishLatency.Observe(duration.Seconds())
	}

	return nil
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}

// CreateInput creates a UDP input component following service pattern
func CreateInput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Validate the raw document before decoding its partial override. The
	// effective configuration is validated after the override is merged.
	if len(rawConfig) > 0 {
		type configOverride InputConfig
		var override configOverride
		if err := component.SafeUnmarshal(rawConfig, &override); err != nil {
			return nil, errs.Wrap(err, "udp-input-factory", "create", "secure config parsing")
		}
		userConfig := InputConfig(override)

		// Apply complete named port replacements to the defaults before the
		// effective configuration is validated by NewInput.
		if userConfig.Ports != nil {
			merged, err := component.MergePortConfig(*cfg.Ports, *userConfig.Ports)
			if err != nil {
				return nil, errs.WrapInvalid(err, "udp-input-factory", "create", "merge port overrides")
			}
			cfg.Ports = &merged
		}
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(fmt.Errorf("NATS client is required"),
			"udp-input-factory", "create", "NATS client validation")
	}

	// Use new idiomatic constructor pattern
	inputDeps := InputDeps{
		Name:            "udp-input", // Default name, will be overridden by ComponentManager
		Config:          cfg,
		NATSClient:      deps.NATSClient,
		MetricsRegistry: deps.MetricsRegistry,
		Logger:          deps.GetLoggerWithComponent("udp-input"),
	}

	input, err := NewInput(inputDeps)
	if err != nil {
		return nil, errs.Wrap(err, "udp-input-factory", "create", "component construction")
	}
	return input, nil
}

// Register registers the UDP input component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "udp",
		Factory:     CreateInput,
		Schema:      udpSchema,
		Type:        "input",
		Protocol:    "udp",
		Domain:      "network",
		Description: "UDP input component for receiving MAVLink and other UDP data",
		Version:     "1.0.0",
	})
}
