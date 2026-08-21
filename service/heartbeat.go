// Package service provides the Heartbeat service for emitting periodic system health logs.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/c360studio/semstreams/component"
)

// HeartbeatConfig holds configuration for the Heartbeat service
type HeartbeatConfig struct {
	// Interval between heartbeat logs (e.g., "30s", "1m")
	// Default: "30s"
	Interval string `json:"interval"`
}

// Validate checks if the configuration is valid
func (c HeartbeatConfig) Validate() error {
	if c.Interval == "" {
		return nil // Will use default
	}

	duration, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("invalid interval: must be positive")
	}
	if duration < time.Second {
		return fmt.Errorf("invalid interval: must be at least 1s")
	}

	return nil
}

// componentHealthGetter defines the interface for getting component health
type componentHealthGetter interface {
	GetComponentHealth() map[string]component.HealthStatus
}

// HeartbeatService emits periodic system heartbeat logs
type HeartbeatService struct {
	*BaseService

	config    HeartbeatConfig
	interval  time.Duration
	startTime time.Time

	// Dependencies for gathering health info
	serviceManager   *Manager
	componentManager componentHealthGetter

	// Ticker for periodic heartbeat
	ticker *time.Ticker

	// Stop channel for goroutine coordination
	stopChan chan struct{}

	// stopOnce guards the one-shot teardown (ticker stop + stopChan close) so
	// repeated Stop calls are safe (gh#549)
	stopOnce sync.Once

	// WaitGroup for goroutine tracking
	wg       sync.WaitGroup
	loopDone chan struct{}

	// Internal logger
	logger *slog.Logger
}

// NewHeartbeatService creates a new heartbeat service using the standard constructor pattern
func NewHeartbeatService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config
	var cfg HeartbeatConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse heartbeat config: %w", err)
		}
	}

	// Apply defaults
	if cfg.Interval == "" {
		cfg.Interval = "30s"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate heartbeat config: %w", err)
	}

	// Parse interval
	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval: %w", err)
	}

	// Create base service with options
	var opts []Option
	if deps != nil && deps.Logger != nil {
		opts = append(opts, WithLogger(deps.Logger))
	}

	baseService := NewBaseServiceWithOptions("heartbeat", nil, opts...)

	hb := &HeartbeatService{
		BaseService: baseService,
		config:      cfg,
		interval:    interval,
		stopChan:    make(chan struct{}),
		logger:      slog.Default().With("source", "heartbeat"),
	}

	if deps != nil {
		hb.serviceManager = deps.ServiceManager
	}

	return hb, nil
}

// Start begins the heartbeat service. Instances are single-use: once Stop has
// run its teardown the instance cannot be restarted — create a new one via the
// constructor (production disable→enable already does this via CreateService).
func (hb *HeartbeatService) Start(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "HeartbeatService", "Start"); err != nil {
		return err
	}
	if hb.Status() == StatusRunning {
		return fmt.Errorf("heartbeat service already running")
	}

	select {
	case <-hb.stopChan:
		return fmt.Errorf("heartbeat service instance already stopped; create a new instance")
	default:
	}
	if hb.serviceManager == nil {
		return fmt.Errorf("heartbeat requires service manager")
	}
	componentService, exists := hb.serviceManager.GetService("component-manager")
	if !exists {
		return fmt.Errorf("heartbeat requires component-manager service")
	}
	componentManager, ok := componentService.(componentHealthGetter)
	if !ok {
		return fmt.Errorf("heartbeat component-manager does not provide component health")
	}
	hb.componentManager = componentManager

	if err := hb.BaseService.Start(ctx); err != nil {
		return err
	}

	hb.startTime = time.Now()
	hb.logger.Info("Heartbeat service started",
		"interval", hb.config.Interval)

	// Start heartbeat loop
	hb.ticker = time.NewTicker(hb.interval)
	hb.wg.Add(1)
	go hb.heartbeatLoop(ctx)
	hb.loopDone = make(chan struct{})
	loopDone := hb.loopDone
	go func() {
		hb.wg.Wait()
		close(loopDone)
	}()

	return nil
}

// Stop gracefully stops the heartbeat service. Stop is idempotent per the
// Service contract (gh#520): a service that already reached a terminal state —
// e.g. via parent-context cancellation before the manager's StopAll visit — is
// a clean shutdown, and repeated calls are safe (gh#549). Teardown still runs
// on the already-stopped path so the ticker is released when cancellation wins
// the race.
func (hb *HeartbeatService) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "HeartbeatService", "Stop"); err != nil {
		return err
	}
	hb.stopOnce.Do(func() {
		hb.logger.Info("Heartbeat service stopping")

		if hb.ticker != nil {
			hb.ticker.Stop()
		}

		// Signal the heartbeat loop to exit
		close(hb.stopChan)
	})
	baseErr := hb.BaseService.Stop(ctx)

	if hb.loopDone != nil {
		select {
		case <-hb.loopDone:
		case <-ctx.Done():
			return errors.Join(baseErr, fmt.Errorf("wait for heartbeat loop: %w", ctx.Err()))
		}
	}
	return baseErr
}

// heartbeatLoop emits periodic heartbeat logs
func (hb *HeartbeatService) heartbeatLoop(ctx context.Context) {
	defer hb.wg.Done()

	// Emit initial heartbeat on start
	hb.emitHeartbeat()

	for {
		select {
		case <-hb.stopChan:
			return
		case <-ctx.Done():
			return
		case <-hb.ticker.C:
			hb.emitHeartbeat()
		}
	}
}

// emitHeartbeat logs the system heartbeat
func (hb *HeartbeatService) emitHeartbeat() {
	uptime := time.Since(hb.startTime).Round(time.Second)
	goroutines := runtime.NumGoroutine()

	// Get component health if available
	healthyCount := 0
	totalCount := 0
	if hb.componentManager != nil {
		health := hb.componentManager.GetComponentHealth()
		totalCount = len(health)
		for _, status := range health {
			if status.Healthy {
				healthyCount++
			}
		}
	}

	// Log heartbeat with structured fields
	hb.logger.Debug("System heartbeat",
		"uptime", uptime.String(),
		"goroutines", goroutines,
		"components_healthy", healthyCount,
		"components_total", totalCount,
	)
}

// newHeartbeatServiceForTest creates a HeartbeatService for testing
func newHeartbeatServiceForTest(config *HeartbeatConfig, componentManager componentHealthGetter) (*HeartbeatService, error) {
	if config == nil {
		config = &HeartbeatConfig{
			Interval: "30s",
		}
	}

	interval, err := time.ParseDuration(config.Interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval: %w", err)
	}

	baseService := NewBaseServiceWithOptions("heartbeat", nil)

	return &HeartbeatService{
		BaseService:      baseService,
		config:           *config,
		interval:         interval,
		stopChan:         make(chan struct{}),
		logger:           slog.Default().With("source", "heartbeat"),
		componentManager: componentManager,
	}, nil
}
