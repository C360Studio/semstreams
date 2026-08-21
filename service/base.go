// Package service provides base functionality and common patterns for
// long-running services in the semstreams platform. It includes health
// monitoring, lifecycle management, and metric collection capabilities.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// ErrAlreadyStopped signals that Stop was invoked after exact owner completion
// was already observed. It is an idempotent, non-fatal outcome: Manager.StopAll
// treats it as successful and does not aggregate it as a shutdown error
// (gh#520). A Stop that observes completed teardown MAY return nil (the
// BaseService.Stop default) or this sentinel; both are success. StatusStopping
// alone is not completion evidence.
var ErrAlreadyStopped = errors.New("service already stopped")

func validateLifecycleContext(ctx context.Context, owner, operation string) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, owner, operation, "nil context")
	}
	return nil
}

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
	mu     sync.RWMutex
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup
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
	if err := validateLifecycleContext(ctx, "BaseService", "Start"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "BaseService", "Start", "context already canceled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "BaseService", "Start", "context already canceled")
	}

	if s.done != nil {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "BaseService", "Start", "service instance already used")
	}

	s.status.Store(StatusStarting)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStarting))
	}

	runtimeCtx, runtimeCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	// Publish cancellation and join authority before any owned goroutine can
	// escape Start. The service is one-shot; done also records prior use.
	s.cancel = runtimeCancel
	s.done = done
	s.wg.Add(2)

	// Record start time
	startTime := time.Now()
	s.startTime.Store(startTime)
	s.lastActivity.Store(startTime)
	s.status.Store(StatusRunning)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusRunning))
	}

	// Start health monitoring
	if s.healthInterval > 0 {
		s.healthTicker = time.NewTicker(s.healthInterval)
		go s.healthMonitor(runtimeCtx, &s.wg)
	} else {
		s.wg.Done()
	}

	// Start context monitor for graceful shutdown
	go s.contextMonitor(runtimeCtx, &s.wg)
	go func() {
		s.wg.Wait()
		s.mu.Lock()
		s.status.Store(StatusStopped)
		if s.metricsRegistry != nil {
			s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopped))
		}
		s.healthy.Store(false)
		s.mu.Unlock()
		close(done)
	}()

	return nil
}

// Stop stops the service gracefully. The first call claims the one-shot
// cancellation authority and bounds the owner join with ctx. Later calls are
// nil/no-op; they neither wait again nor replay a prior result.
func (s *BaseService) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "BaseService", "Stop"); err != nil {
		return err
	}

	s.mu.Lock()
	cancel := s.cancel
	if cancel == nil {
		if s.done == nil {
			done := make(chan struct{})
			close(done)
			s.done = done
			if s.status.Load() != StatusStopped {
				s.status.Store(StatusStopped)
			}
		}
		s.mu.Unlock()
		return nil
	}
	done := s.done
	select {
	case <-done:
		// Parent cancellation already drove every owned goroutine to exact
		// completion. Consume the stale cancellation handle without reopening
		// terminal teardown or changing the observed stopped state.
		s.cancel = nil
		cancel()
		s.mu.Unlock()
		return nil
	default:
	}
	s.cancel = nil
	s.status.Store(StatusStopping)
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopping))
	}
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}
	cancel()
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for BaseService runtime: %w", err)
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for BaseService runtime: %w", ctx.Err())
	}
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
func (s *BaseService) healthMonitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	select {
	case <-ctx.Done():
		return
	default:
		s.performHealthCheck()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.healthTicker.C:
			s.performHealthCheck()
		}
	}
}

// performHealthCheck executes the health check
func (s *BaseService) performHealthCheck() {
	if s.Status() != StatusRunning {
		return
	}
	s.healthChecks.Add(1)

	var err error
	s.mu.RLock()
	healthCheck := s.healthCheckFunc
	onHealthChange := s.onHealthChange
	s.mu.RUnlock()

	// Custom health check has priority
	if healthCheck != nil {
		err = healthCheck()
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
	if s.Status() != StatusRunning {
		return
	}

	s.healthy.Store(isHealthy)

	// Notify health change
	if wasHealthy != isHealthy && onHealthChange != nil {
		onHealthChange(isHealthy)
	}
}

// contextMonitor monitors the parent context for cancellation
func (s *BaseService) contextMonitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	<-ctx.Done()
	s.performGracefulShutdown()
}

// performGracefulShutdown fences health activity after parent cancellation.
// Exact stopped state is published by the owner-join observer after both
// lifecycle goroutines have returned.
func (s *BaseService) performGracefulShutdown() {
	if !s.status.CompareAndSwap(StatusRunning, StatusStopping) {
		return
	}
	if s.metricsRegistry != nil {
		s.metricsRegistry.CoreMetrics().RecordServiceStatus(s.name, int(StatusStopping))
	}

	// Stop health monitoring
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}
}

// Service interface defines the contract for all services
type Service interface {
	Name() string
	Start(ctx context.Context) error
	// Stop stops the service gracefully. After exact owner completion, another
	// invocation returns success (nil or ErrAlreadyStopped) without repeating
	// teardown. Parent cancellation can complete the owner before coordinated
	// shutdown reaches it; StatusStopping alone does not prove that completion
	// or authorize the manager to infer success (gh#520).
	Stop(ctx context.Context) error
	Status() Status
	IsHealthy() bool       // Keep for compatibility during migration
	GetStatus() Info       // Keep for compatibility during migration
	Health() health.Status // NEW: Standard health reporting
	RegisterMetrics(registrar metric.MetricsRegistrar) error
}
