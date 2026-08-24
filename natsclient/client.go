// Package natsclient provides a client for managing NATS connections with circuit breaker pattern.
package natsclient

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
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
	urls     string       // comma-separated NATS server URLs for clustering support
	status   atomic.Value // stores ConnectionStatus
	failures atomic.Int32
	logger   *slog.Logger

	// NATS connection
	conn *nats.Conn
	js   jetstream.JetStream

	// Native-handle consumer claims reject duplicate fixed durable ownership
	// without retaining lifecycle handles or giving Client.Close child authority.
	internalClaimsMu sync.Mutex
	internalClaims   map[internalConsumerIdentity]*internalConsumerClaim

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

	// requestHandlerTimeout bounds a single SubscribeForRequests handler
	// invocation. Defaults to DefaultRequestHandlerTimeout (30s); raised per
	// deployment via WithRequestHandlerTimeout or the
	// SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT env var for slow-by-design
	// handlers (e.g. LLM answer synthesis on the globalSearch path).
	requestHandlerTimeout time.Duration

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

	// Connection-loss watchdog: when set, onConnectionLost fires once the
	// connection has been continuously down for at least connectionLossTimeout.
	// Reconnecting before the timeout cancels it. Useful for callers that
	// want to bound how long the process tolerates an absent broker (e.g.
	// trigger graceful shutdown so the supervisor can restart with a fresh
	// connection instead of hot-looping in degraded mode forever).
	connectionLossTimeout time.Duration
	lossTimer             *time.Timer
	lossTimerMu           sync.Mutex

	// Health monitoring
	healthTicker   *time.Ticker
	healthInterval time.Duration
	healthDone     chan struct{} // Signal to stop health monitoring goroutine

	// Synchronization
	mu      sync.RWMutex
	closeMu sync.Mutex  // Ensures Close() is called only once
	closed  atomic.Bool // Track if client is closed
}

// NewClient creates a new NATS client with optional configuration.
// The urls parameter accepts comma-separated NATS server URLs for clustering support
// (e.g., "nats://server1:4222,nats://server2:4222").
func NewClient(urls string, opts ...ClientOption) (*Client, error) {
	c := &Client{
		urls:   urls,
		logger: slog.Default(),
		// Sensible defaults
		maxReconnects:    -1, // infinite by default
		reconnectWait:    2 * time.Second,
		pingInterval:     30 * time.Second,
		healthInterval:   10 * time.Second,
		circuitThreshold: 15, // Increased from 5 for resilience to transient failures
		maxBackoff:       time.Minute,
		timeout:          5 * time.Second,
		drainTimeout:     30 * time.Second,
		metricsInterval:  30 * time.Second, // Poll JetStream stats every 30s
		// Env-resolved default (30s unless SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT
		// overrides). Applied here so an explicit WithRequestHandlerTimeout option
		// below still wins (option > env > framework default).
		requestHandlerTimeout: resolveRequestHandlerTimeoutFromEnv(),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, errs.WrapInvalid(err, "Client", "NewClient", "apply option")
		}
	}

	c.status.Store(StatusDisconnected)
	c.backoff.Store(time.Second)
	c.lastFailure.Store(time.Time{})

	c.logger.Debug("Created NATS client", slog.String("urls", urls))

	return c, nil
}

// URLs returns the NATS server URLs (comma-separated for clustering)
func (m *Client) URLs() string {
	return m.urls
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

// MaxPayload observes the maximum payload reported by the active NATS
// connection. The value is diagnostic and may change after it is returned;
// callers must treat the result of an actual publish as authoritative.
func (m *Client) MaxPayload() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.conn == nil || !m.conn.IsConnected() {
		return 0, ErrNotConnected
	}
	return m.conn.MaxPayload(), nil
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

	m.logger.Debug("Recorded failure", slog.Int64("total_failures", int64(totalFailures)), slog.Int64("circuit_failures", int64(circuitFailures)))

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

				m.logger.Info("Circuit breaker opened",
					slog.Int64("circuit_failures", int64(circuitFailures)),
					slog.Duration("backoff", currentBackoff),
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

			m.logger.Info("Circuit breaker still open, increased backoff", slog.Duration("backoff", newBackoff))

			// Reset circuit failures for next round
			m.circuitFailures.Store(0)
		}
	}
}

// recordStreamPublishFailure accounts a failed JetStream publish against the
// connection circuit unless the server's typed PubAck says only that the target
// stream reached one of its configured admission ceilings. Capacity refusal is
// a healthy connection reporting durable-resource state, not connection loss.
// The exact typed API error remains caller-visible at each publish seam.
func (m *Client) recordStreamPublishFailure(err error) {
	if isCircuitNeutralStreamCapacityError(err) {
		return
	}
	m.recordFailure()
}

func isCircuitNeutralStreamCapacityError(err error) bool {
	var apiErr *jetstream.APIError
	if !stderrors.As(err, &apiErr) || apiErr == nil || apiErr.ErrorCode != jetstream.ErrorCode(10077) {
		return false
	}
	switch apiErr.Description {
	case "maximum bytes exceeded",
		"maximum messages exceeded",
		"maximum messages per subject exceeded":
		return true
	default:
		return false
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
	m.logger.Debug("Testing circuit breaker - attempting to close circuit")

	// This will be implemented when we add actual connection logic
	// For now, just try to reconnect
	if m.Status() == StatusCircuitOpen {
		m.logger.Debug("Circuit breaker test: moving from open to disconnected")
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
	return m.connectWith(ctx, nats.Connect)
}

// connectWith keeps the native connection candidate private until Connect wins
// admission against terminal Close. dial is a test seam; production uses
// nats.Connect synchronously with the timeout in buildConnectionOptions.
func (m *Client) connectWith(
	ctx context.Context,
	dial func(string, ...nats.Option) (*nats.Conn, error),
) error {
	// Check circuit breaker first
	if m.Status() == StatusCircuitOpen {
		m.logger.Debug("Circuit breaker is open, skipping connection attempt")
		return ErrCircuitOpen
	}

	m.setStatus(StatusConnecting)
	m.logger.Info("Connecting to NATS", slog.String("urls", m.urls))

	// Build connection options
	opts := m.buildConnectionOptions()

	conn, err := dial(m.urls, opts...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		if conn != nil {
			conn.Close()
		}
		m.recordFailure()
		if m.Status() != StatusCircuitOpen {
			m.setStatus(StatusDisconnected)
		}
		return errs.WrapTransient(ctxErr, "Client", "Connect", "connection cancelled")
	}
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

		return errs.WrapTransient(err, "Client", "Connect", "establish connection")
	}

	// Initialize JetStream with new API. The async publish error handler
	// bridges failed async acks into the circuit breaker so a broken ack
	// path opens the breaker exactly as a failed synchronous publish does
	// (see PublishToStreamAsync). Keep both candidates local until admission.
	js, _ := jetstream.New(conn, jetstream.WithPublishAsyncErrHandler(m.asyncPublishErrHandler))

	// Close owns terminal admission. Once Close sets closed, no native
	// connection produced by an in-flight dial may become Client state.
	m.closeMu.Lock()
	if ctxErr := ctx.Err(); ctxErr != nil {
		conn.Close()
		m.recordFailure()
		if m.Status() != StatusCircuitOpen {
			m.setStatus(StatusDisconnected)
		}
		m.closeMu.Unlock()
		return errs.WrapTransient(ctxErr, "Client", "Connect", "connection cancelled")
	}
	if m.closed.Load() {
		conn.Close()
		m.setStatus(StatusDisconnected)
		m.closeMu.Unlock()
		return errs.Wrap(nats.ErrConnectionClosed, "Client", "Connect", "admit connection")
	}

	m.mu.Lock()
	m.conn = conn
	m.js = js
	m.mu.Unlock()

	m.setStatus(StatusConnected)
	m.resetCircuit()
	m.closeMu.Unlock()

	m.logger.Info("Successfully connected to NATS", slog.String("urls", m.urls))

	// Start health monitoring if configured
	if m.healthInterval > 0 {
		m.logger.Debug("Starting health monitoring", slog.Duration("interval", m.healthInterval))
		m.startHealthMonitoring()
	}

	// Start JetStream metrics polling if configured
	if m.jsMetrics != nil && m.metricsInterval > 0 {
		m.logger.Debug("Starting JetStream metrics polling", slog.Duration("interval", m.metricsInterval))
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

	// Cancel any pending connection-loss watchdog so we don't fire after Close.
	m.cancelConnectionLossTimer()

	// Stop JetStream metrics polling
	if m.metricsCancel != nil {
		m.metricsCancel()
	}

	m.mu.RLock()
	conn := m.conn
	drainTimeout := m.drainTimeout
	m.mu.RUnlock()

	// Drain and close connection
	closeErr := m.drainAndCloseConnection(ctx, conn, drainTimeout)

	m.mu.Lock()
	if m.conn == conn {
		m.conn = nil
	}

	// Clear sensitive credentials from memory
	m.username = ""
	m.password = ""
	m.token = ""
	m.mu.Unlock()

	m.setStatus(StatusDisconnected)

	return closeErr
}

// guardedConsumer serializes Info() on one consumer handle.
//
// jetstream.Consumer.Info() is NOT safe for concurrent use on the same handle: it
// assigns the fetched ConsumerInfo to the consumer's own cache field with no
// synchronization (nats.go jetstream/consumer.go — `p.info = resp.ConsumerInfo`). Two
// callers reading the same bound consumer at once race inside the library, and the
// detector reports it against OUR call site. Confirmed by the graph-ingest lifecycle
// stress test the moment a second reader existed.
//
// THE GUARD IS ON THE HANDLE, NOT ON A CALL SITE, and that is the point. The same
// *jetstream.Consumer is read by the acquisition path and the metrics observer, so a
// lock around only one reader leaves the other racing the identical object. Wrapping
// once at creation means every present and future caller in this package is covered by
// construction.
//
// Embedding keeps it a jetstream.Consumer; only Info is overridden, and its signature
// must stay EXACTLY `Info(context.Context) (*jetstream.ConsumerInfo, error)` — a
// divergent signature would silently stop overriding and reopen the race.
//
// STILL PRESENT UPSTREAM as of nats.go v1.52.0 (checked 2026-07-30; we pin v1.48.0).
// The assignment is byte-identical across both, so a dependency bump does NOT retire
// this guard — do not delete it on upgrade. Filing it upstream is tracked in the
// change's follow-ups.
type guardedConsumer struct {
	jetstream.Consumer
	infoMu sync.Mutex
}

// Info serializes the underlying call. The lock is held across the network round trip
// because the unsynchronized write happens at the END of the library's Info().
func (g *guardedConsumer) Info(ctx context.Context) (*jetstream.ConsumerInfo, error) {
	g.infoMu.Lock()
	defer g.infoMu.Unlock()
	return g.Consumer.Info(ctx)
}

// CachedInfo serializes the READ of the same field Info writes.
//
// Overriding it is not optional. jetstream.Consumer includes CachedInfo, implemented as
// a bare `return p.info` — the exact field Info assigns. Guarding only Info would leave
// the promoted CachedInfo racing the guarded writer, so the "every caller is covered"
// claim above would be false for it. No caller in this repo uses Consumer.CachedInfo
// today; this closes the hole rather than waiting for one to open it.
func (g *guardedConsumer) CachedInfo() *jetstream.ConsumerInfo {
	g.infoMu.Lock()
	defer g.infoMu.Unlock()
	return g.Consumer.CachedInfo()
}

// isBenignDrainError reports whether Drain reached the desired terminal state
// before this client asked it to. Other failures remain observable so cleanup
// cannot silently hide deadline, cancellation, or transport errors.
func isBenignDrainError(err error) bool {
	return stderrors.Is(err, nats.ErrConnectionClosed)
}

// drainAndCloseConnection drains and closes the NATS connection.
func (m *Client) drainAndCloseConnection(ctx context.Context, conn *nats.Conn, drainTimeout time.Duration) error {
	if conn == nil {
		return nil
	}

	// Use context deadline for drain timeout if available
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < drainTimeout {
			drainTimeout = remaining
		}
	}

	closed := conn.StatusChanged(nats.CLOSED)
	defer conn.RemoveStatusListener(closed)

	if err := conn.Drain(); err != nil {
		if isBenignDrainError(err) {
			return nil
		}
		drainErr := errs.Wrap(err, "Client", "Close", "drain connection")
		m.logger.Error("Drain error", slog.Any("error", err))
		if !conn.IsClosed() {
			conn.Close()
		}
		return drainErr
	}

	drainTimer := time.NewTimer(drainTimeout)
	defer drainTimer.Stop()

	select {
	case <-closed:
		return nil
	case <-drainTimer.C:
		drainErr := errs.WrapTransient(
			fmt.Errorf("drain timeout after %v", drainTimeout),
			"Client", "Close", "drain timeout",
		)
		m.logger.Error("Drain timeout, force closing", slog.Duration("drain_timeout", drainTimeout))
		if !conn.IsClosed() {
			conn.Close()
		}
		return drainErr
	case <-ctx.Done():
		drainErr := errs.Wrap(ctx.Err(), "Client", "Close", "context cancelled during drain")
		m.logger.Error("Context cancelled during drain, force closing")
		if !conn.IsClosed() {
			conn.Close()
		}
		return drainErr
	}
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

type nativeSubscription interface {
	Drain() error
	IsValid() bool
	StatusChanged(...nats.SubStatus) <-chan nats.SubStatus
	Unsubscribe() error
}

// Subscription wraps a NATS subscription for lifecycle management.
type Subscription struct {
	sub nativeSubscription

	drainOnce     sync.Once
	drainErr      error
	drainComplete bool
	closed        <-chan nats.SubStatus
}

func newSubscription(sub nativeSubscription) *Subscription {
	return &Subscription{
		sub:    sub,
		closed: sub.StatusChanged(nats.SubscriptionClosed),
	}
}

// Unsubscribe unsubscribes from the subject
func (s *Subscription) Unsubscribe() error {
	if s == nil || s.sub == nil {
		return nil
	}
	return s.sub.Unsubscribe()
}

// Drain stops new deliveries and waits for NATS to close the subscription
// after all queued callbacks finish. If ctx expires, a later call rejoins the
// same native drain; it never starts a second drain operation.
func (s *Subscription) Drain(ctx context.Context) error {
	if ctx == nil {
		return stderrors.New("natsclient: nil Subscription.Drain context")
	}
	if s == nil || s.sub == nil {
		return nil
	}
	s.drainOnce.Do(func() {
		if !s.sub.IsValid() {
			select {
			case <-s.closed:
				s.drainComplete = true
			default:
				s.drainErr = nats.ErrBadSubscription
			}
			return
		}
		s.drainErr = s.sub.Drain()
		if stderrors.Is(s.drainErr, nats.ErrBadSubscription) {
			select {
			case <-s.closed:
				s.drainErr = nil
				s.drainComplete = true
			default:
			}
		}
	})
	if s.drainErr != nil {
		return stderrors.Join(s.drainErr, ctx.Err())
	}
	if s.drainComplete {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.closed:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscribe subscribes to a NATS subject with context propagation.
// Each message handler receives the full *nats.Msg to access Subject, Data, Headers, etc.
// This is essential for wildcard subscriptions where the actual subject differs from the pattern.
// The context is derived from the parent context with a 30-second timeout for message processing.
// Returns a Subscription handle that can be used to unsubscribe.
func (m *Client) Subscribe(ctx context.Context, subject string, handler func(context.Context, *nats.Msg)) (*Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil || !m.conn.IsConnected() {
		return nil, ErrNotConnected
	}

	sub, err := m.conn.Subscribe(subject, func(msg *nats.Msg) {
		// Create per-message context with timeout
		msgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Extract trace context from message headers
		if tc := ExtractTrace(msg); tc != nil {
			msgCtx = ContextWithTrace(msgCtx, tc)
		}

		handler(msgCtx, msg)
	})
	if err != nil {
		return nil, err
	}

	return newSubscription(sub), nil
}

// Publish publishes a message to a NATS subject
func (m *Client) Publish(ctx context.Context, subject string, data []byte) error {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()

	if conn == nil || !conn.IsConnected() {
		return ErrNotConnected
	}

	// Auto-generate trace if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	msg := &nats.Msg{Subject: subject, Data: data}
	InjectTrace(ctx, msg)

	return conn.PublishMsg(msg)
}

// JetStream returns the JetStream context
func (m *Client) JetStream() (jetstream.JetStream, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.js == nil {
		return nil, errs.WrapTransient(
			fmt.Errorf("JetStream not initialized"),
			"Client", "JetStream", "get JetStream context")
	}

	return m.js, nil
}

// CreateStream creates a JetStream stream
func (m *Client) CreateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	// Fail closed on a KV/ObjectStore backing-stream name before anything else,
	// so the refusal does not depend on connection or circuit state.
	if err := CheckOrdinaryStreamName(cfg.Name, "natsclient.Client.CreateStream"); err != nil {
		return nil, errs.WrapFatal(err, "Client", "CreateStream",
			"validate stream name "+cfg.Name)
	}

	// Bounds are checked HERE, unconditionally, unlike on EnsureStream where the
	// same check sits inside the not-found branch. This seam only ever CREATES, so
	// there is no bind path to protect and no reason to wait for the server to
	// tell us which act this is.
	if err := CheckStreamBounds(cfg, "natsclient.Client.CreateStream"); err != nil {
		return nil, errs.WrapFatal(err, "Client", "CreateStream",
			"validate stream bounds for "+cfg.Name)
	}

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

// PublishToStream publishes to a JetStream stream with automatic trace context propagation.
// If no trace context exists in ctx, one is auto-generated for distributed tracing.
func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {
	return m.publishToStream(ctx, subject, data, "")
}

// PublishToStreamWithMsgID publishes to a JetStream stream stamping the
// Nats-Msg-Id header so the server's duplicate-detection window collapses
// re-publishes/redeliveries of the same logical event to a single store.
//
// This is the producer half of the at-least-once idempotency contract
// (ADR-055 §5, "T1"): graph-ingest's stream consumer is at-least-once and
// MergeEntity APPENDS triples on merge, so a redelivered born-once entity
// payload would double-apply its triples without dedup. Callers pass a
// DETERMINISTIC msgID for the logical event (e.g. "<loopID>:spawn",
// "<entityID>:v<n>") so a retry/redelivery carries the same ID.
//
// Scope of the guarantee: dedup only holds WITHIN the stream's configured
// duplicate window (config.StreamConfig.Duplicates; the NATS server default
// is 2m when unset). Redelivery outside that window — e.g. DeliverPolicy:all
// replay on consumer recreation — can still re-append; see ADR-055 Open
// Question #1. An empty msgID is equivalent to PublishToStream (no dedup),
// so this is a safe drop-in.
func (m *Client) PublishToStreamWithMsgID(ctx context.Context, subject string, data []byte, msgID string) error {
	return m.publishToStream(ctx, subject, data, msgID)
}

// publishToStream is the shared publish path for PublishToStream and
// PublishToStreamWithMsgID. A non-empty msgID is stamped as the Nats-Msg-Id
// header for server-side duplicate detection.
func (m *Client) publishToStream(ctx context.Context, subject string, data []byte, msgID string) error {
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

	// Auto-generate trace context if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Build message with headers for trace propagation
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
	}
	if msgID != "" {
		// Initialize the header here (rather than relying on InjectTrace,
		// which early-returns when no trace context is present) so the
		// dedup ID is always carried.
		msg.Header = make(nats.Header)
		msg.Header.Set(nats.MsgIdHdr, msgID)
	}
	InjectTrace(ctx, msg)

	_, err = js.PublishMsg(ctx, msg)
	if err != nil {
		m.recordStreamPublishFailure(err)
		return err
	}

	m.resetCircuit()
	return nil
}

// asyncPublishErrHandler is the connection-level handler jetstream-go invokes for
// every failed async publish ack. It applies the same failure accounting as a
// failed synchronous publish; a typed target-stream capacity refusal remains
// circuit-neutral. The reset side lives on the enqueue path (successful enqueue
// = connection healthy); this handler never resets the circuit.
func (m *Client) asyncPublishErrHandler(_ jetstream.JetStream, msg *nats.Msg, err error) {
	m.recordStreamPublishFailure(err)
	if m.jsMetrics != nil {
		m.jsMetrics.recordError("publish_async")
	}
	subject := ""
	if msg != nil {
		subject = msg.Subject
	}
	m.logger.Debug("Async publish ack failed",
		slog.String("subject", subject),
		slog.Any("error", err),
	)
}

// PublishToStreamAsync publishes to a JetStream stream WITHOUT blocking on the
// PubAck, returning a jetstream.PubAckFuture the caller inspects (Ok()/Err()) for
// the eventual server acknowledgement. A single producer goroutine can pipeline
// many of these past the synchronous ack-RTT ceiling (gh#470).
//
// The enqueue itself is synchronous: an open circuit returns ErrCircuitOpen, a
// disconnected client returns ErrNotConnected, and a full in-flight window past
// the stall wait returns jetstream's ErrTooManyStalledMsgs — in all of which the
// returned future is nil. Trace context injection is preserved. Failed acks are
// delivered on the future's Err() channel and accounted by the connection-level
// async error handler; only the exact classified capacity set is circuit-neutral.
//
// Ordering: jetstream-go preserves per-subject order per connection, so a single
// caller publishing to one subject gets in-order storage. Cross-goroutine ordering
// is the caller's responsibility (as with the synchronous path).
func (m *Client) PublishToStreamAsync(ctx context.Context, subject string, data []byte) (jetstream.PubAckFuture, error) {
	return m.publishToStreamAsync(ctx, subject, data, "")
}

// PublishToStreamAsyncWithMsgID is PublishToStreamAsync stamping the Nats-Msg-Id
// header for server-side duplicate detection. It carries the same ADR-055 T1
// idempotency contract as the synchronous PublishToStreamWithMsgID: pass a
// DETERMINISTIC msgID per logical event so a retry/redelivery carries the same ID;
// dedup holds only within the stream's configured Duplicates window. An empty
// msgID is equivalent to PublishToStreamAsync (no dedup).
func (m *Client) PublishToStreamAsyncWithMsgID(ctx context.Context, subject string, data []byte, msgID string) (jetstream.PubAckFuture, error) {
	return m.publishToStreamAsync(ctx, subject, data, msgID)
}

// publishToStreamAsync is the shared async publish path. It mirrors
// publishToStream's pre-checks and header stamping, then enqueues via
// PublishMsgAsync. A successful enqueue resets the circuit breaker (the
// connection-health signal); an enqueue error records a failure.
func (m *Client) publishToStreamAsync(ctx context.Context, subject string, data []byte, msgID string) (jetstream.PubAckFuture, error) {
	// Check circuit breaker first (the breaker is the outermost gate, as on the
	// sync path).
	if m.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if m.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	// Honor a cancelled context before enqueuing (PublishMsgAsync takes no ctx,
	// so this is the only cancellation point on the async path). A cancelled ctx
	// is caller intent, not a connection fault, so it does NOT record a failure.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	js, err := m.JetStream()
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	// Auto-generate trace context if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Build message with headers for trace propagation
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
	}
	if msgID != "" {
		// Initialize the header here (rather than relying on InjectTrace,
		// which early-returns when no trace context is present) so the
		// dedup ID is always carried.
		msg.Header = make(nats.Header)
		msg.Header.Set(nats.MsgIdHdr, msgID)
	}
	InjectTrace(ctx, msg)

	future, err := js.PublishMsgAsync(msg)
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	// Enqueue succeeded: the connection is up and JetStream accepted the message
	// onto the wire. On the async path the breaker is a CONNECTION-LIVENESS gate:
	// a successful enqueue proves the connection is healthy, so we reset here
	// rather than at ack time. The exact classified target-stream capacity set is
	// surfaced through the future/batch and remains circuit-neutral. Every other
	// ack failure still contributes through asyncPublishErrHandler; connection
	// outage also makes subsequent enqueues fail, which trips the breaker. See
	// the nats-streaming capability contract.
	m.resetCircuit()
	return future, nil
}

// PublishAsyncComplete returns a channel that closes when every outstanding async
// publish has been acknowledged by the server. A producer waits on it to drain
// before shutdown. When JetStream is unavailable it returns an already-closed
// channel so a drain loop does not block forever.
func (m *Client) PublishAsyncComplete() <-chan struct{} {
	js, err := m.JetStream()
	if err != nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return js.PublishAsyncComplete()
}

// PublishAsyncPending returns the number of async publishes enqueued but not yet
// acknowledged. Returns 0 when JetStream is unavailable.
func (m *Client) PublishAsyncPending() int {
	js, err := m.JetStream()
	if err != nil {
		return 0
	}
	return js.PublishAsyncPending()
}

// PublishBatchToStream publishes every message in msgs to one subject via the
// async path, waits for all acks (bounded by ctx), and returns a single aggregate
// error. Per-subject ordering from this single calling goroutine is preserved
// (jetstream-go async is in-order per connection, absent a NoResponders retry —
// see design.md §1). It is the convenience path for bursty producers that do not
// need per-message futures (gh#470).
//
// The drain waits on THIS batch's own futures, not the connection-global
// PublishAsyncComplete, so a concurrent async producer on the same Client cannot
// make this batch over-wait. If ctx is cancelled before all acks arrive, it
// returns the context error rather than hanging; the already-enqueued publishes
// still resolve in the background (and feed the circuit breaker via the async
// error handler on a connection fault). An enqueue error stops further enqueuing
// but already-enqueued messages are still drained. Ack failures are accounted
// once by asyncPublishErrHandler — this loop only collects them for the returned
// error (accounting here too would double-count).
func (m *Client) PublishBatchToStream(ctx context.Context, subject string, msgs [][]byte) error {
	if len(msgs) == 0 {
		return nil
	}
	// Fail fast on an already-cancelled context so nothing is enqueued.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("PublishBatchToStream: context already cancelled: %w", err)
	}

	futures := make([]jetstream.PubAckFuture, 0, len(msgs))
	var enqueueErr error
	for _, data := range msgs {
		future, err := m.publishToStreamAsync(ctx, subject, data, "")
		if err != nil {
			enqueueErr = err
			break
		}
		futures = append(futures, future)
	}

	// Drain this batch's own futures, honoring ctx. Each future is already
	// in-flight, so waiting in order costs only the slowest ack, not the sum.
	errsList := make([]error, 0)
	if enqueueErr != nil {
		errsList = append(errsList, fmt.Errorf("enqueue stopped after %d of %d messages: %w",
			len(futures), len(msgs), enqueueErr))
	}
	ackFailures := 0
	for i, future := range futures {
		select {
		case <-future.Ok():
		case ackErr := <-future.Err():
			ackFailures++
			errsList = append(errsList, ackErr)
		case <-ctx.Done():
			// ctx.Done() and this future's completion can be ready in the same
			// instant; select then picks at random. Re-check the future
			// non-blocking so a publish that actually resolved is counted, not
			// spuriously reported cancelled — this preserves the "a batch that
			// finished draining before the cancel returns success" guarantee
			// (feedback_select_race_on_pre_cancelled_ctx).
			select {
			case <-future.Ok():
			case ackErr := <-future.Err():
				ackFailures++
				errsList = append(errsList, ackErr)
			default:
				return fmt.Errorf("PublishBatchToStream: context cancelled while draining "+
					"(%d of %d resolved, %d still pending): %w",
					i, len(futures), len(futures)-i, ctx.Err())
			}
		}
	}
	if len(errsList) == 0 {
		return nil
	}
	// Failed publishes = messages that never enqueued + messages whose ack failed.
	failed := (len(msgs) - len(futures)) + ackFailures
	return fmt.Errorf("PublishBatchToStream: %d of %d publishes failed (%d ack failures): %w",
		failed, len(msgs), ackFailures, stderrors.Join(errsList...))
}

// GetStream gets an existing JetStream stream.
//
// Its jetstream.ErrStreamNotFound means "not on this connection, now" — it is a
// point-in-time probe, not durable evidence of absence, because a clustered node
// that has not applied the meta assignment answers it for a stream that exists.
// A caller deciding something for the process lifetime wants ErrStreamNotVisible
// out of the guarded consumer setup instead; see that sentinel and the package doc.
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
		// ErrStreamNotFound is a successful probe result (the stream is absent on
		// this connection right now), not a transport or availability failure.
		// Counting it as a failure would trip the circuit breaker on legitimate
		// existence probes — a component checking whether an optional stream is
		// present, a diagnostic sweep, a caller deciding whether to provision.
		// Only genuine failures (timeout, no-responders, etc.) should move the
		// breaker.
		//
		// The exemption is why this seam is deliberately NOT wired to the
		// consumer setup path's stream-visibility wait: a cheap probe stays
		// cheap. It is also why its answer is not evidence of absence — see the
		// package doc, and ErrStreamNotVisible for the answer that is.
		if !stderrors.Is(err, jetstream.ErrStreamNotFound) {
			m.recordFailure()
			m.jsMetrics.recordError("get_stream")
		}
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
		m.logger.Info("Using existing KV bucket", slog.String("bucket", cfg.Bucket))
		m.resetCircuit()
		return bucket, nil
	}

	// Bucket doesn't exist, try to create it
	bucket, err = js.CreateKeyValue(ctx, cfg)
	if err != nil {
		// Check if error is "already exists" (race condition)
		if isAlreadyExistsError(err) {
			m.logger.Info("KV bucket already exists (race condition), attempting to get existing bucket",
				slog.String("bucket", cfg.Bucket),
			)
			// Try to get the existing bucket
			bucket, err = js.KeyValue(ctx, cfg.Bucket)
			if err != nil {
				m.recordFailure()
				return nil, errs.Wrap(err, "Client", "CreateKeyValueBucket",
					fmt.Sprintf("access existing bucket %s", cfg.Bucket))
			}
			m.logger.Info("Successfully accessed existing KV bucket", slog.String("bucket", cfg.Bucket))
			m.resetCircuit()
			return bucket, nil
		}
		// Real error, record failure
		m.recordFailure()
		return nil, err
	}

	// Successfully created new bucket
	m.logger.Info("Created new KV bucket", slog.String("bucket", cfg.Bucket))
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
		// ErrBucketNotFound is a successful probe result (the bucket is absent),
		// not a transport or availability failure — the same reasoning as
		// GetStream above, which has exempted ErrStreamNotFound since gh#248.
		// Counting it would trip the shared circuit breaker on legitimate
		// existence probes, and several callers poll for a legitimately absent
		// bucket: graph-query's resource.Watcher on COMMUNITY_INDEX rechecks
		// every 60s on any deployment without community detection, the readiness
		// watcher rebinds on every retry, and WaitForBucket polls at 500ms by
		// design. At a threshold of 15 those reach it on their own.
		if !stderrors.Is(err, jetstream.ErrBucketNotFound) {
			m.recordFailure()
		}
		return nil, err
	}

	m.resetCircuit()
	return bucket, nil
}

// WaitForBucket waits for a KV bucket to become available, retrying until
// the timeout expires or the context is cancelled. Use this when a component
// depends on a bucket created by another component with unpredictable startup timing.
//
// For more advanced patterns (background recovery, loss detection), use
// pkg/resource.Watcher directly.
func (m *Client) WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error) {
	// Try immediately first
	if bucket, err := m.GetKeyValueBucket(ctx, name); err == nil {
		return bucket, nil
	}

	// Calculate retry attempts from timeout
	interval := 500 * time.Millisecond
	attempts := int(timeout / interval)
	if attempts < 1 {
		attempts = 1
	}

	// Use resource.Watcher for structured retry with logging
	var result jetstream.KeyValue
	watcher := resource.NewWatcher(name, func(checkCtx context.Context) error {
		bucket, err := m.GetKeyValueBucket(checkCtx, name)
		if err != nil {
			return err
		}
		result = bucket
		return nil
	}, resource.Config{
		StartupAttempts: attempts,
		StartupInterval: interval,
		Logger:          m.logger,
	})

	if !watcher.WaitForStartup(ctx) {
		return nil, fmt.Errorf("bucket %q not available after %s", name, timeout)
	}

	return result, nil
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

	m.armConnectionLossTimer(err)
}

func (m *Client) handleReconnect(_ *nats.Conn) {
	m.setStatus(StatusConnected)
	m.resetCircuit()
	m.cancelConnectionLossTimer()

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

// armConnectionLossTimer starts the connection-loss watchdog if it is
// configured and not already armed. Idempotent across repeated disconnects:
// a second disconnect without an intervening reconnect reuses the original
// timer so the elapsed grace measures from the *first* loss of contact.
func (m *Client) armConnectionLossTimer(disconnectErr error) {
	if m.connectionLossTimeout <= 0 {
		return
	}

	m.mu.RLock()
	cb := m.onConnectionLost
	m.mu.RUnlock()
	if cb == nil {
		return
	}

	m.lossTimerMu.Lock()
	defer m.lossTimerMu.Unlock()
	if m.lossTimer != nil {
		return
	}
	timeout := m.connectionLossTimeout
	m.lossTimer = time.AfterFunc(timeout, func() {
		m.lossTimerMu.Lock()
		m.lossTimer = nil
		m.lossTimerMu.Unlock()

		// Re-read the callback under the main lock so a concurrent option
		// change or close doesn't race us into firing on a stale handle.
		m.mu.RLock()
		fire := m.onConnectionLost
		m.mu.RUnlock()
		if fire != nil && !m.closed.Load() {
			fire(disconnectErr)
		}
	})
}

// cancelConnectionLossTimer stops the watchdog if armed. Safe to call when
// no timer is pending.
func (m *Client) cancelConnectionLossTimer() {
	m.lossTimerMu.Lock()
	defer m.lossTimerMu.Unlock()
	if m.lossTimer != nil {
		m.lossTimer.Stop()
		m.lossTimer = nil
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

func (m *Client) handleError(_ *nats.Conn, sub *nats.Subscription, err error) {
	attrs := []any{slog.Any("error", err)}
	if sub != nil {
		attrs = append(attrs, slog.String("subject", sub.Subject))
		if sub.Queue != "" {
			attrs = append(attrs, slog.String("queue", sub.Queue))
		}
		if stderrors.Is(err, nats.ErrSlowConsumer) {
			if dropped, droppedErr := sub.Dropped(); droppedErr == nil {
				attrs = append(attrs, slog.Int("dropped", dropped))
			} else {
				attrs = append(attrs, slog.Bool("dropped_available", false))
			}
		}
	}

	// Log error for debugging
	m.logger.Error("NATS error", attrs...)
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
