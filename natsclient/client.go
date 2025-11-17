// Package natsclient provides a client for managing NATS connections with circuit breaker pattern.
package natsclient

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
)

// ConnectionStatus represents the state of the NATS connection
type ConnectionStatus int

// Possible connection statuses
const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusReconnecting
	StatusCircuitOpen
)

// String returns the string representation of ConnectionStatus
func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusReconnecting:
		return "reconnecting"
	case StatusCircuitOpen:
		return "circuit_open"
	default:
		return "unknown"
	}
}

// Error messages
var (
	ErrNotConnected      = stderrors.New("not connected to NATS")
	ErrCircuitOpen       = stderrors.New("circuit breaker is open")
	ErrConnectionTimeout = stderrors.New("connection timeout")
)

// Status holds runtime status information for the NATS manager
type Status struct {
	Status          ConnectionStatus
	FailureCount    int32
	LastFailureTime time.Time
	Reconnects      int32
	RTT             time.Duration
}

// Client manages NATS connections with circuit breaker pattern
type Client struct {
	url      string
	status   atomic.Value // stores ConnectionStatus
	failures atomic.Int32
	logger   Logger

	// NATS connection
	conn *nats.Conn
	js   jetstream.JetStream
	subs []*nats.Subscription

	// Consumer management
	consumers   map[string]jetstream.ConsumeContext
	consumersMu sync.RWMutex

	// Circuit breaker
	lastFailure      atomic.Value // stores time.Time
	backoff          atomic.Value // stores time.Duration
	circuitFailures  atomic.Int32 // failures in current circuit round
	circuitThreshold int32        // failures before opening circuit
	maxBackoff       time.Duration

	// Connection options
	maxReconnects int
	reconnectWait time.Duration
	pingInterval  time.Duration
	timeout       time.Duration
	drainTimeout  time.Duration

	// Authentication - sensitive fields cleared on close
	username string
	password string // WARNING: Consider using JWT/NKey authentication instead
	token    string // WARNING: Sensitive - cleared on close

	// TLS
	tlsEnabled  bool
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string

	// Client identification
	clientName  string
	compression bool

	// Metrics
	jsMetrics       *jetstreamMetrics
	metricsCancel   context.CancelFunc
	metricsInterval time.Duration

	// Callbacks
	onDisconnect     func(error) // Changed to accept error
	onReconnect      func()
	onHealthChange   func(bool)
	onConnectionLost func(error)

	// Health monitoring
	healthTicker   *time.Ticker
	healthInterval time.Duration
	healthDone     chan struct{} // Signal to stop health monitoring goroutine

	// Synchronization
	mu      sync.RWMutex
	closeMu sync.Mutex  // Ensures Close() is called only once
	closed  atomic.Bool // Track if client is closed
}

// NewClient creates a new NATS client with optional configuration
func NewClient(url string, opts ...ClientOption) (*Client, error) {
	c := &Client{
		url:    url,
		logger: &defaultLogger{},
		// Sensible defaults
		maxReconnects:    -1, // infinite by default
		reconnectWait:    2 * time.Second,
		pingInterval:     30 * time.Second,
		healthInterval:   10 * time.Second,
		circuitThreshold: 5,
		maxBackoff:       time.Minute,
		timeout:          5 * time.Second,
		drainTimeout:     30 * time.Second,
		metricsInterval:  30 * time.Second, // Poll JetStream stats every 30s
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, errors.WrapInvalid(err, "Client", "NewClient", "apply option")
		}
	}

	c.status.Store(StatusDisconnected)
	c.backoff.Store(time.Second)
	c.lastFailure.Store(time.Time{})

	c.logger.Debugf("Created NATS client for %s", url)

	return c, nil
}

// URL returns the NATS server URL
func (m *Client) URL() string {
	return m.url
}

// Status returns the current connection status
func (m *Client) Status() ConnectionStatus {
	val := m.status.Load()
	if val == nil {
		return StatusDisconnected
	}
	return val.(ConnectionStatus)
}

// GetConnection returns the current NATS connection
func (m *Client) GetConnection() *nats.Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn
}

// SetConnection sets the NATS connection (for testing)
func (m *Client) SetConnection(conn *nats.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conn = conn
	if conn != nil && conn.IsConnected() {
		m.setStatus(StatusConnected)
	}
}

// setStatus updates the connection status
func (m *Client) setStatus(status ConnectionStatus) {
	m.status.Store(status)
}

// IsHealthy returns true if the connection is healthy
func (m *Client) IsHealthy() bool {
	return m.Status() == StatusConnected
}

// Failures returns the current failure count
func (m *Client) Failures() int32 {
	return m.failures.Load()
}

// Backoff returns the current backoff duration
func (m *Client) Backoff() time.Duration {
	return m.backoff.Load().(time.Duration)
}

// recordFailure records a connection failure and manages circuit breaker
func (m *Client) recordFailure() {
	// Track total failures for metrics
	totalFailures := m.failures.Add(1)
	m.lastFailure.Store(time.Now())

	// Track circuit breaker failures separately
	circuitFailures := m.circuitFailures.Add(1)

	m.logger.Debugf("Recorded failure %d (circuit failures: %d)", totalFailures, circuitFailures)

	// Open circuit after threshold failures in this round
	if circuitFailures >= m.circuitThreshold {
		currentStatus := m.Status()

		// We need to open or update the circuit breaker
		if currentStatus != StatusCircuitOpen {
			// Try to transition to open state (only one goroutine will succeed)
			if m.status.CompareAndSwap(currentStatus, StatusCircuitOpen) {
				// We successfully opened the circuit
				currentBackoff := m.backoff.Load().(time.Duration)
				newBackoff := currentBackoff * 2
				if newBackoff > m.maxBackoff {
					newBackoff = m.maxBackoff
				}
				m.backoff.Store(newBackoff)

				m.logger.Printf(
					"Circuit breaker opened after %d failures, backing off for %v",
					circuitFailures,
					currentBackoff,
				)

				// Reset circuit failures for next round
				m.circuitFailures.Store(0)

				// Schedule circuit test after backoff
				time.AfterFunc(currentBackoff, m.testCircuit)
			}
		} else {
			// Circuit already open - may need to increase backoff for consecutive failures
			// This handles the case where failures continue while circuit is open
			currentBackoff := m.backoff.Load().(time.Duration)
			newBackoff := currentBackoff * 2
			if newBackoff > m.maxBackoff {
				newBackoff = m.maxBackoff
			}
			m.backoff.Store(newBackoff)

			m.logger.Printf("Circuit breaker still open, increased backoff to %v", newBackoff)

			// Reset circuit failures for next round
			m.circuitFailures.Store(0)
		}
	}
}

// resetCircuit resets the circuit breaker state
func (m *Client) resetCircuit() {
	m.failures.Store(0)
	m.circuitFailures.Store(0)
	m.backoff.Store(time.Second)
	m.lastFailure.Store(time.Time{})

	// Don't change status if we're connected
	if m.Status() == StatusCircuitOpen {
		m.setStatus(StatusDisconnected)
	}
}

// testCircuit attempts to close the circuit breaker
func (m *Client) testCircuit() {
	m.logger.Debugf("Testing circuit breaker - attempting to close circuit")

	// This will be implemented when we add actual connection logic
	// For now, just try to reconnect
	if m.Status() == StatusCircuitOpen {
		m.logger.Debugf("Circuit breaker test: moving from open to disconnected")
		m.setStatus(StatusDisconnected)
		// In real implementation, this would trigger reconnection
	}
}

// WaitForConnection waits for the connection to be established
func (m *Client) WaitForConnection(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("connection timeout: %w", ctx.Err())
		case <-ticker.C:
			if m.IsHealthy() {
				return nil
			}
		}
	}
}

// MaxReconnects returns the maximum number of reconnection attempts
func (m *Client) MaxReconnects() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxReconnects
}

// ReconnectWait returns the wait duration between reconnection attempts
func (m *Client) ReconnectWait() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reconnectWait
}

// PingInterval returns the interval for health checks
func (m *Client) PingInterval() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pingInterval
}

// ConnectionOptions returns the NATS connection options
func (m *Client) ConnectionOptions() []nats.Option {
	return m.buildConnectionOptions()
}

// buildConnectionOptions builds NATS connection options from client configuration
func (m *Client) buildConnectionOptions() []nats.Option {
	opts := []nats.Option{
		nats.MaxReconnects(m.maxReconnects),
		nats.ReconnectWait(m.reconnectWait),
		nats.PingInterval(m.pingInterval),
		nats.Timeout(m.timeout),
		nats.DrainTimeout(m.drainTimeout),
		nats.DisconnectErrHandler(m.handleDisconnect),
		nats.ReconnectHandler(m.handleReconnect),
		nats.ClosedHandler(m.handleClosed),
		nats.ErrorHandler(m.handleError),
	}

	// Add authentication if configured
	if m.username != "" && m.password != "" {
		opts = append(opts, nats.UserInfo(m.username, m.password))
	}
	if m.token != "" {
		opts = append(opts, nats.Token(m.token))
	}

	// Add TLS if configured
	if m.tlsEnabled {
		if m.tlsCertFile != "" && m.tlsKeyFile != "" {
			opts = append(opts, nats.ClientCert(m.tlsCertFile, m.tlsKeyFile))
		}
		if m.tlsCAFile != "" {
			opts = append(opts, nats.RootCAs(m.tlsCAFile))
		}
	}

	// Add client name if configured
	if m.clientName != "" {
		opts = append(opts, nats.Name(m.clientName))
	}

	// Add compression if enabled
	if m.compression {
		opts = append(opts, nats.Compression(true))
	}

	return opts
}

// GetStatus returns current status information
func (m *Client) GetStatus() *Status {
	lastFailure := m.lastFailure.Load().(time.Time)

	status := &Status{
		Status:          m.Status(),
		FailureCount:    m.failures.Load(),
		LastFailureTime: lastFailure,
	}

	// Add RTT if connected
	if m.conn != nil && m.conn.IsConnected() {
		if rtt, err := m.conn.RTT(); err == nil {
			status.RTT = rtt
		}
	}

	return status
}

// Connect establishes connection to NATS server
func (m *Client) Connect(ctx context.Context) error {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		m.logger.Debugf("Circuit breaker is open, skipping connection attempt")
		return ErrCircuitOpen
	}

	m.setStatus(StatusConnecting)
	m.logger.Printf("Connecting to NATS at %s", m.url)

	// Build connection options
	opts := m.buildConnectionOptions()

	// Attempt connection with context timeout
	connectDone := make(chan error, 1)
	go func() {
		conn, err := nats.Connect(m.url, opts...)
		if err != nil {
			connectDone <- err
			return
		}

		m.mu.Lock()
		m.conn = conn
		m.mu.Unlock()

		// Initialize JetStream with new API
		if js, err := jetstream.New(conn); err == nil {
			m.mu.Lock()
			m.js = js
			m.mu.Unlock()
		}

		connectDone <- nil
	}()

	// Wait for connection or context cancellation
	select {
	case err := <-connectDone:
		if err != nil {
			m.recordFailure()

			// Only set to disconnected if circuit didn't open
			if m.Status() != StatusCircuitOpen {
				m.setStatus(StatusDisconnected)
			}

			// Check if circuit opened after this failure
			if m.Status() == StatusCircuitOpen {
				return ErrCircuitOpen
			}

			return errors.WrapTransient(err, "Client", "Connect", "establish connection")
		}
	case <-ctx.Done():
		m.recordFailure()
		if m.Status() != StatusCircuitOpen {
			m.setStatus(StatusDisconnected)
		}
		return errors.WrapTransient(ctx.Err(), "Client", "Connect", "connection cancelled")
	}

	m.setStatus(StatusConnected)
	m.resetCircuit()

	m.logger.Printf("Successfully connected to NATS at %s", m.url)

	// Start health monitoring if configured
	if m.healthInterval > 0 {
		m.logger.Debugf("Starting health monitoring with interval %v", m.healthInterval)
		m.startHealthMonitoring()
	}

	// Start JetStream metrics polling if configured
	if m.jsMetrics != nil && m.metricsInterval > 0 {
		m.logger.Debugf("Starting JetStream metrics polling with interval %v", m.metricsInterval)
		m.metricsCancel = m.jsMetrics.startPoller(context.Background(), m.metricsInterval)
	}

	// Notify health change
	if m.onHealthChange != nil {
		m.onHealthChange(true)
	}

	return nil
}

// Close closes the NATS connection
func (m *Client) Close(ctx context.Context) error {
	// Ensure Close() is only called once
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	if m.closed.Load() {
		return nil // Already closed
	}
	m.closed.Store(true)

	// Stop health monitoring first (before acquiring main mutex to avoid deadlock)
	m.stopHealthMonitoring()

	// Stop JetStream metrics polling
	if m.metricsCancel != nil {
		m.metricsCancel()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect all errors during cleanup
	var errs []error

	// Stop all consumers with proper error tracking
	m.consumersMu.Lock()
	for name, consumer := range m.consumers {
		consumer.Stop()
		// Note: Stop() doesn't return error in the interface
		m.logger.Debugf("Stopped consumer: %s", name)
	}
	m.consumers = nil
	m.consumersMu.Unlock()

	// Unsubscribe all with error tracking
	for _, sub := range m.subs {
		if err := sub.Unsubscribe(); err != nil {
			errs = append(errs, errors.Wrap(err, "Client", "Close", "unsubscribe"))
			m.logger.Errorf("Failed to unsubscribe: %v", err)
		}
	}
	m.subs = nil

	// Close connection with drain timeout from context or default
	var drainErr error
	if m.conn != nil {
		// Use context deadline for drain timeout if available
		drainTimeout := m.drainTimeout
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining > 0 && remaining < drainTimeout {
				drainTimeout = remaining
			}
		}

		// Drain connection with timeout
		drainDone := make(chan error, 1)
		go func() {
			drainDone <- m.conn.Drain()
		}()

		select {
		case err := <-drainDone:
			if err != nil {
				drainErr = errors.Wrap(err, "Client", "Close", "drain connection")
				m.logger.Errorf("Drain error: %v", err)
			}
		case <-time.After(drainTimeout):
			// Drain timeout, force close
			drainErr = errors.WrapTransient(
				fmt.Errorf("drain timeout after %v", drainTimeout),
				"Client",
				"Close",
				"drain timeout",
			)
			m.logger.Errorf("Drain timeout after %v, force closing", drainTimeout)
		case <-ctx.Done():
			// Context cancelled, force close
			drainErr = errors.Wrap(ctx.Err(), "Client", "Close", "context cancelled during drain")
			m.logger.Errorf("Context cancelled during drain, force closing")
		}

		if drainErr != nil {
			errs = append(errs, drainErr)
		}

		m.conn.Close()
		m.conn = nil
	}

	// Clear sensitive credentials from memory
	m.username = ""
	m.password = ""
	m.token = ""

	m.setStatus(StatusDisconnected)

	// Combine all errors
	if len(errs) > 0 {
		// Return a combined error message
		errMsg := "cleanup errors:"
		for i, err := range errs {
			errMsg += fmt.Sprintf("\n  [%d] %v", i+1, err)
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// RTT returns the round-trip time to the NATS server
func (m *Client) RTT() (time.Duration, error) {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return 0, ErrNotConnected
	}

	return conn.RTT()
}

// Subscribe subscribes to a NATS subject with context propagation.
// Each message handler receives a context derived from the parent context
// with a 30-second timeout for message processing.
func (m *Client) Subscribe(ctx context.Context, subject string, handler func(context.Context, []byte)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil || !m.conn.IsConnected() {
		return ErrNotConnected
	}

	sub, err := m.conn.Subscribe(subject, func(msg *nats.Msg) {
		// Create per-message context with timeout
		msgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		handler(msgCtx, msg.Data)
	})
	if err != nil {
		return err
	}

	m.subs = append(m.subs, sub)
	return nil
}

// Publish publishes a message to a NATS subject
func (m *Client) Publish(_ context.Context, subject string, data []byte) error {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return ErrNotConnected
	}

	return conn.Publish(subject, data)
}

// JetStream returns the JetStream context
func (m *Client) JetStream() (jetstream.JetStream, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.js == nil {
		return nil, errors.WrapTransient(
			fmt.Errorf("JetStream not initialized"),
			"Client", "JetStream", "get JetStream context")
	}

	return m.js, nil
}

// CreateStream creates a JetStream stream
func (m *Client) CreateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	stream, err := js.CreateStream(ctx, cfg)
	if err != nil {
		m.recordFailure()
		m.jsMetrics.recordError("create_stream")
		return nil, err
	}

	m.resetCircuit()

	// Track stream for metrics collection
	m.jsMetrics.trackStream(cfg.Name, stream)

	return stream, nil
}

// PublishToStream publishes to a JetStream stream
func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return err
	}

	_, err = js.Publish(ctx, subject, data)
	if err != nil {
		m.recordFailure()
		return err
	}

	m.resetCircuit()
	return nil
}

// ConsumeStream creates a consumer for a stream
func (m *Client) ConsumeStream(ctx context.Context, streamName, subject string, handler func([]byte)) error {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return err
	}

	// Check if client is closing to prevent new consumers during shutdown
	if m.closed.Load() {
		return errors.WrapInvalid(
			fmt.Errorf("client is closed"),
			"Client", "ConsumeStream", "check client state")
	}

	// Create consumer configuration
	consumerCfg := jetstream.ConsumerConfig{
		FilterSubject: subject,
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, streamName, consumerCfg)
	if err != nil {
		m.recordFailure()
		m.jsMetrics.recordError("create_consumer")
		return err
	}

	// Track consumer for metrics collection
	consumerInfo, err := consumer.Info(ctx)
	if err == nil {
		m.jsMetrics.trackConsumer(streamName, consumerInfo.Name, consumer)
	}

	// Start consuming messages
	consumeContext, err := consumer.Consume(func(msg jetstream.Msg) {
		handler(msg.Data())
		msg.Ack()
	})

	if err != nil {
		m.recordFailure()
		return err
	}

	// Store the consume context for lifecycle management with race protection
	m.consumersMu.Lock()
	defer m.consumersMu.Unlock()

	// Double-check client isn't closing while we have the lock
	if m.closed.Load() {
		// Client is closing, stop the consumer we just created
		consumeContext.Stop()
		return errors.WrapInvalid(
			fmt.Errorf("client is closing"),
			"Client", "ConsumeStream", "check client state during consumer registration")
	}

	if m.consumers == nil {
		m.consumers = make(map[string]jetstream.ConsumeContext)
	}
	consumerKey := fmt.Sprintf("%s:%s", streamName, subject)

	// Stop any existing consumer for this key
	if existingConsumer, exists := m.consumers[consumerKey]; exists {
		existingConsumer.Stop()
		m.logger.Debugf("Replaced existing consumer for %s", consumerKey)
	}

	m.consumers[consumerKey] = consumeContext

	m.resetCircuit()
	return nil
}

// GetStream gets an existing JetStream stream
func (m *Client) GetStream(ctx context.Context, name string) (jetstream.Stream, error) {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	stream, err := js.Stream(ctx, name)
	if err != nil {
		m.recordFailure()
		m.jsMetrics.recordError("get_stream")
		return nil, err
	}

	m.resetCircuit()

	// Track stream for metrics collection
	m.jsMetrics.trackStream(name, stream)

	return stream, nil
}

// CreateKeyValueBucket creates or gets a KV bucket with configuration
func (m *Client) CreateKeyValueBucket(ctx context.Context, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	// Try to get existing bucket first
	bucket, err := js.KeyValue(ctx, cfg.Bucket)
	if err == nil {
		// Bucket already exists, use it
		m.logger.Printf("Using existing KV bucket: %s", cfg.Bucket)
		m.resetCircuit()
		return bucket, nil
	}

	// Bucket doesn't exist, try to create it
	bucket, err = js.CreateKeyValue(ctx, cfg)
	if err != nil {
		// Check if error is "already exists" (race condition)
		if isAlreadyExistsError(err) {
			m.logger.Printf(
				"KV bucket %s already exists (race condition), attempting to get existing bucket",
				cfg.Bucket,
			)
			// Try to get the existing bucket
			bucket, err = js.KeyValue(ctx, cfg.Bucket)
			if err != nil {
				m.recordFailure()
				return nil, errors.Wrap(err, "Client", "CreateKeyValueBucket",
					fmt.Sprintf("access existing bucket %s", cfg.Bucket))
			}
			m.logger.Printf("Successfully accessed existing KV bucket: %s", cfg.Bucket)
			m.resetCircuit()
			return bucket, nil
		}
		// Real error, record failure
		m.recordFailure()
		return nil, err
	}

	// Successfully created new bucket
	m.logger.Printf("Created new KV bucket: %s", cfg.Bucket)
	m.resetCircuit()
	return bucket, nil
}

// GetKeyValueBucket gets an existing KV bucket
func (m *Client) GetKeyValueBucket(ctx context.Context, name string) (jetstream.KeyValue, error) {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	bucket, err := js.KeyValue(ctx, name)
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	m.resetCircuit()
	return bucket, nil
}

// DeleteKeyValueBucket deletes a KV bucket
func (m *Client) DeleteKeyValueBucket(ctx context.Context, name string) error {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return err
	}

	err = js.DeleteKeyValue(ctx, name)
	if err != nil {
		m.recordFailure()
		return err
	}

	m.resetCircuit()
	return nil
}

// ListKeyValueBuckets lists all KV buckets
func (m *Client) ListKeyValueBuckets(ctx context.Context) ([]string, error) {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	// KeyValue stores are implemented as JetStream streams with "KV_" prefix
	names := []string{}
	streamsCh := js.ListStreams(ctx)

	// StreamInfoLister is actually a channel of *StreamInfo
	for stream := range streamsCh.Info() {
		if stream != nil {
			// KV buckets are streams with "KV_" prefix
			if len(stream.Config.Name) > 3 && stream.Config.Name[:3] == "KV_" {
				bucketName := stream.Config.Name[3:] // Remove "KV_" prefix
				names = append(names, bucketName)
			}
		}
	}

	if err := streamsCh.Err(); err != nil {
		m.recordFailure()
		return nil, err
	}

	m.resetCircuit()
	return names, nil
}

// OnHealthChange sets a callback for health status changes
func (m *Client) OnHealthChange(fn func(bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onHealthChange = fn
}

// WithHealthCheck enables health monitoring with a specified interval
func (m *Client) WithHealthCheck(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthInterval = interval
}

// Event handlers for NATS connection
func (m *Client) handleDisconnect(_ *nats.Conn, err error) {
	m.setStatus(StatusReconnecting)

	m.mu.RLock()
	onDisconnect := m.onDisconnect
	onHealthChange := m.onHealthChange
	m.mu.RUnlock()

	if onDisconnect != nil {
		go onDisconnect(err)
	}
	if onHealthChange != nil {
		go onHealthChange(false)
	}
}

func (m *Client) handleReconnect(_ *nats.Conn) {
	m.setStatus(StatusConnected)
	m.resetCircuit()

	m.mu.RLock()
	onReconnect := m.onReconnect
	onHealthChange := m.onHealthChange
	m.mu.RUnlock()

	if onReconnect != nil {
		go onReconnect()
	}
	if onHealthChange != nil {
		go onHealthChange(true)
	}
}

func (m *Client) handleClosed(_ *nats.Conn) {
	m.setStatus(StatusDisconnected)

	m.mu.RLock()
	onHealthChange := m.onHealthChange
	m.mu.RUnlock()

	if onHealthChange != nil {
		go onHealthChange(false)
	}
}

func (m *Client) handleError(_ *nats.Conn, _ *nats.Subscription, err error) {
	// Log error for debugging
	m.logger.Errorf("NATS error: %v", err)
	// Don't record failure here as it may be called for non-connection errors
}

// startHealthMonitoring starts periodic health checks
func (m *Client) startHealthMonitoring() {
	// Stop any existing health monitoring
	m.stopHealthMonitoring()

	// Initialize health monitoring channels with mutex protection
	m.mu.Lock()
	m.healthTicker = time.NewTicker(m.healthInterval)
	m.healthDone = make(chan struct{})
	ticker := m.healthTicker
	done := m.healthDone
	m.mu.Unlock()

	go func() {
		defer ticker.Stop() // Ensure ticker is stopped when goroutine exits
		lastHealthy := m.IsHealthy()

		for {
			select {
			case <-done:
				// Exit goroutine cleanly
				return
			case <-ticker.C:
				m.mu.RLock()
				conn := m.conn
				m.mu.RUnlock()

				if conn == nil {
					continue
				}

				healthy := conn.IsConnected()
				if _, err := conn.RTT(); err != nil {
					healthy = false
				}

				// Update status based on health
				if healthy && m.Status() != StatusConnected {
					m.setStatus(StatusConnected)
				} else if !healthy && m.Status() == StatusConnected {
					m.setStatus(StatusReconnecting)
				}

				// Notify on change
				if healthy != lastHealthy && m.onHealthChange != nil {
					m.onHealthChange(healthy)
				}

				lastHealthy = healthy
			}
		}
	}()
}

// stopHealthMonitoring stops health monitoring goroutine
func (m *Client) stopHealthMonitoring() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.healthTicker != nil {
		m.healthTicker.Stop()
		m.healthTicker = nil
	}
	if m.healthDone != nil {
		close(m.healthDone)
		m.healthDone = nil
	}
}

// isAlreadyExistsError checks if an error indicates a KV bucket already exists
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "bucket name already in use") ||
		strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "stream name already in use")
}
