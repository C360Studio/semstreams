// Package service provides base functionality and common patterns for
// long-running services in the semstreams platform. It includes health
// monitoring, lifecycle management, and metric collection capabilities.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/health"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
)

// Status represents the current status of a service
type Status int

// Possible service statuses
const (
	StatusStopped Status = iota
	StatusStarting
	StatusRunning
	StatusStopping
)

// String returns the string representation of Status
func (s Status) String() string {
	switch s {
	case StatusStopped:
		return "stopped"
	case StatusStarting:
		return "starting"
	case StatusRunning:
		return "running"
	case StatusStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// Info holds runtime information for a service
type Info struct {
	Name               string        `json:"name"`
	Status             Status        `json:"status"`
	Uptime             time.Duration `json:"uptime"`
	StartTime          time.Time     `json:"start_time"`
	MessagesProcessed  int64         `json:"messages_processed"`
	LastActivity       time.Time     `json:"last_activity"`
	HealthChecks       int64         `json:"health_checks"`
	FailedHealthChecks int64         `json:"failed_health_checks"`
}

// HealthCheckFunc defines a custom health check function
type HealthCheckFunc func() error

// Option is a functional option for configuring BaseService
type Option func(*BaseService)

// BaseService provides common functionality for all services
type BaseService struct {
	name            string
	config          *config.Config
	nats            *natsclient.Client
	metricsRegistry *metric.MetricsRegistry
	logger          *slog.Logger // Structured logger for the service

	status    atomic.Value // Status
	startTime atomic.Value // time.Time
	healthy   atomic.Bool

	// Metrics
	messagesProcessed  atomic.Int64
	healthChecks       atomic.Int64
	failedHealthChecks atomic.Int64
	lastActivity       atomic.Value // time.Time

	// Functions
	healthCheckFunc HealthCheckFunc

	// Health monitoring
	healthTicker   *time.Ticker
	healthInterval time.Duration

	// Callbacks
	onHealthChange func(bool)

	// Lifecycle management
	done      chan struct{}
	waitGroup sync.WaitGroup
	mu        sync.RWMutex
}

// NewBaseServiceWithOptions creates a new base service using functional options pattern
func NewBaseServiceWithOptions(name string, cfg *config.Config, opts ...Option) *BaseService {
	service := &BaseService{
		name:           name,
		config:         cfg,
		healthInterval: 30 * time.Second,                     // Default health interval
		logger:         slog.Default().With("service", name), // Default logger with service name
	}

	// Apply options (can override the default logger)
	for _, opt := range opts {
		opt(service)
	}

	// Initialize status and metrics
	service.status.Store(StatusStopped)
	if service.metricsRegistry != nil {
		service.metricsRegistry.CoreMetrics().RecordServiceStatus(name, int(StatusStopped))
	}
	service.startTime.Store(time.Time{})
	service.lastActivity.Store(time.Time{})

	return service
}

// WithNATS sets the NATS client for the service
func WithNATS(client *natsclient.Client) Option {
	return func(s *BaseService) {
		s.nats = client
	}
}

// WithMetrics sets the metrics registry for the service
func WithMetrics(registry *metric.MetricsRegistry) Option {
	return func(s *BaseService) {
		s.metricsRegistry = registry
	}
}

// WithLogger sets a custom logger for the service
func WithLogger(logger *slog.Logger) Option {
	return func(s *BaseService) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithHealthCheck sets a custom health check function
func WithHealthCheck(fn HealthCheckFunc) Option {
	return func(s *BaseService) {
		s.healthCheckFunc = fn
	}
}

// WithHealthInterval sets the health check interval
func WithHealthInterval(interval time.Duration) Option {
	return func(s *BaseService) {
		s.healthInterval = interval
	}
}

// OnHealthChange sets a callback for health state changes
func OnHealthChange(fn func(bool)) Option {
	return func(s *BaseService) {
		s.onHealthChange = fn
	}
}

// Name returns the service name
func (s *BaseService) Name() string {
	return s.name
}

// Status returns the current service status
func (s *BaseService) Status() Status {
	return s.status.Load().(Status)
}

// IsHealthy returns whether the service is healthy
func (s *BaseService) IsHealthy() bool {
	return s.healthy.Load()
}

// Health returns the standard health status for the service
func (s *BaseService) Health() health.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if unhealthy
	if !s.healthy.Load() {
		// BaseService doesn't track specific errors, just unhealthy state
		// Services that embed BaseService can override Health() for more detail
		failedChecks := s.failedHealthChecks.Load()
		message := fmt.Sprintf("Service is unhealthy (failed checks: %d)", failedChecks)
		return health.NewUnhealthy(s.name, message)
	}

	// Check lifecycle state for degraded conditions
	status := s.Status()
	switch status {
	case StatusRunning:
		return health.NewHealthy(s.name, "Service operating normally")
	case StatusStarting:
		return health.NewDegraded(s.name, "Service is starting")
	case StatusStopping:
		return health.NewDegraded(s.name, "Service is stopping")
	case StatusStopped:
		return health.NewUnhealthy(s.name, "Service is stopped")
	default:
		return health.NewUnhealthy(s.name, fmt.Sprintf("Unknown status: %v", status))
	}
}

// Start starts the service
func (s *BaseService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running or starting
	currentStatus := s.Status()
	if currentStatus == StatusRunning || currentStatus == StatusStarting {
		return nil
	}

	s.status.Store(StatusStarting)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStarting))
	}

	// Create done channel for service lifecycle
	s.done = make(chan struct{})

	// Record start time
	startTime := time.Now()
	s.startTime.Store(startTime)
	s.lastActivity.Store(startTime)

	// Start health monitoring
	if s.healthInterval > 0 {
		s.healthTicker = time.NewTicker(s.healthInterval)
		s.waitGroup.Add(1)
		go s.healthMonitor()

		// Perform initial health check after a delay to ensure setup is complete
		// ComponentManager needs extra time for component startup goroutines
		go func() {
			time.Sleep(200 * time.Millisecond)
			s.performHealthCheck()
		}()
	}

	// Start context monitor for graceful shutdown
	s.waitGroup.Add(1)
	go s.contextMonitor(ctx)

	s.status.Store(StatusRunning)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusRunning))
	}
	return nil
}

// Stop stops the service gracefully
func (s *BaseService) Stop(timeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already stopped or stopping
	currentStatus := s.Status()
	if currentStatus == StatusStopped || currentStatus == StatusStopping {
		return nil // Already stopped or stopping
	}

	// Transition to stopping status
	s.status.Store(StatusStopping)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopping))
	}

	// Signal all goroutines to stop
	if s.done != nil {
		select {
		case <-s.done:
			// Already closed
		default:
			close(s.done)
		}
	}

	// Stop health monitoring
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}

	// Use provided timeout or default
	if timeout == 0 {
		timeout = 5 * time.Second // Default timeout
	}

	// Create fresh context for cleanup
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		s.waitGroup.Wait()
		close(done)
	}()

	// Wait for shutdown or timeout
	select {
	case <-done:
		// Graceful shutdown completed
	case <-ctx.Done():
		// Timeout - force shutdown
	}

	s.status.Store(StatusStopped)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopped))
	}
	s.healthy.Store(false)

	return nil
}

// SetHealthCheck sets a custom health check function
func (s *BaseService) SetHealthCheck(fn HealthCheckFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthCheckFunc = fn
}

// OnHealthChange sets a callback for health state changes
func (s *BaseService) OnHealthChange(callback func(bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onHealthChange = callback
}

// GetStatus returns the current service information
func (s *BaseService) GetStatus() Info {
	startTime := s.startTime.Load().(time.Time)
	lastActivity := s.lastActivity.Load().(time.Time)

	uptime := time.Duration(0)
	if !startTime.IsZero() && s.Status() == StatusRunning {
		uptime = time.Since(startTime)
	}

	return Info{
		Name:               s.name,
		Status:             s.Status(),
		Uptime:             uptime,
		StartTime:          startTime,
		MessagesProcessed:  s.messagesProcessed.Load(),
		LastActivity:       lastActivity,
		HealthChecks:       s.healthChecks.Load(),
		FailedHealthChecks: s.failedHealthChecks.Load(),
	}
}

// RegisterMetrics allows services to register their own domain-specific metrics
func (s *BaseService) RegisterMetrics(_ metric.MetricsRegistrar) error {
	// BaseService doesn't have its own metrics to register
	// Concrete services should override this method to register their metrics
	return nil
}

// healthMonitor runs the health check monitoring loop
func (s *BaseService) healthMonitor() {
	defer s.waitGroup.Done()

	for {
		select {
		case <-s.done:
			return
		case <-s.healthTicker.C:
			s.performHealthCheck()
		}
	}
}

// performHealthCheck executes the health check
func (s *BaseService) performHealthCheck() {
	s.healthChecks.Add(1)

	var err error

	// Custom health check has priority
	if s.healthCheckFunc != nil {
		err = s.healthCheckFunc()
	}

	// Default health checks (only if no custom health check or custom passed)
	if err == nil && s.nats != nil && !s.nats.IsHealthy() {
		err = natsclient.ErrNotConnected
	}

	wasHealthy := s.healthy.Load()
	isHealthy := err == nil

	if err != nil {
		s.failedHealthChecks.Add(1)
	}

	s.healthy.Store(isHealthy)

	// Notify health change
	if wasHealthy != isHealthy && s.onHealthChange != nil {
		go s.onHealthChange(isHealthy)
	}
}

// contextMonitor monitors the parent context for cancellation
func (s *BaseService) contextMonitor(ctx context.Context) {
	defer s.waitGroup.Done()

	select {
	case <-ctx.Done():
		// Parent context canceled - perform graceful shutdown
		s.performGracefulShutdown()
	case <-s.done:
		// Service stopped via Stop() method - exit gracefully
		return
	}
}

// performGracefulShutdown atomically transitions service to stopped state
func (s *BaseService) performGracefulShutdown() {
	// Use atomic compare-and-swap to avoid race conditions - load atomically to prevent races
	const maxRetries = 100
	for range maxRetries {
		current := s.status.Load().(Status)
		if current != StatusRunning {
			return // Already shutting down or stopped
		}

		// Attempt atomic transition to stopping state
		if s.status.CompareAndSwap(current, StatusStopping) {
			if s.metricsRegistry != nil {
				s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopping))
			}
			break
		}
		// If CAS failed, retry with brief backoff to reduce contention
		time.Sleep(time.Microsecond)
	}
	// Fallback: if max retries exhausted, force status change (unlikely scenario)
	if s.status.Load().(Status) == StatusRunning {
		s.status.Store(StatusStopping)
		if s.metricsRegistry != nil {
			s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopping))
		}
	}

	// Stop health monitoring
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}

	// Set final status
	s.status.Store(StatusStopped)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopped))
	}
	s.healthy.Store(false)
}

// Service interface defines the contract for all services
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop(timeout time.Duration) error
	Status() Status
	IsHealthy() bool       // Keep for compatibility during migration
	GetStatus() Info       // Keep for compatibility during migration
	Health() health.Status // NEW: Standard health reporting
	RegisterMetrics(registrar metric.MetricsRegistrar) error
}
