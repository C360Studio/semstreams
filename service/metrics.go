package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
)

// Metrics is a service that provides Prometheus metrics endpoint
type Metrics struct {
	*BaseService

	config         MetricsConfig           // Consistent config field
	server         metricsServer           // Runtime state
	registry       *metric.MetricsRegistry // Dependency
	natsClient     *natsclient.Client      // For JetStream metrics publishing
	security       security.Config         // Platform security config
	lifecycleMu    sync.Mutex
	used           bool
	running        bool
	stopping       bool
	terminal       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc

	// Owner-local causal observation points used only by lifecycle tests.
	testServerPublished   chan<- struct{}
	testStartRelease      <-chan struct{}
	testStartWaitUnlocked chan<- struct{}
}

type metricsServer interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// MetricsConfig holds configuration for the metrics service
// Simple struct - no UnmarshalJSON, no Enabled field
type MetricsConfig struct {
	Port int    `json:"port"`
	Path string `json:"path"`
}

// Validate checks if the configuration is valid
func (c MetricsConfig) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.Path == "" {
		return fmt.Errorf("metrics path cannot be empty")
	}
	return nil
}

// NewMetrics creates a new metrics service using the standard constructor pattern
func NewMetrics(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg MetricsConfig
	if err := decodeStrictServiceJSON(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("parse metrics config: %w", err)
	}

	// Apply defaults - clear and visible in constructor
	if cfg.Port == 0 {
		cfg.Port = 9090
	}
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate metrics config: %w", err)
	}

	// Get security configuration from platform config
	var securityCfg security.Config
	if deps.Manager != nil {
		fullConfig := deps.Manager.GetConfig()
		if fullConfig != nil {
			securityCfg = fullConfig.Get().Security
		}
	}

	// Create base service
	baseService := NewBaseServiceWithOptions(
		"metrics",
		nil, // Config is now service-specific
		WithLogger(deps.Logger),
		WithMetrics(deps.MetricsRegistry),
	)

	m := &Metrics{
		BaseService: baseService,
		config:      cfg, // Store config as field
		registry:    deps.MetricsRegistry,
		security:    securityCfg,
	}

	// Set health check
	m.SetHealthCheck(m.healthCheck)

	return m, nil
}

// Start starts the metrics HTTP server
func (m *Metrics) Start(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "Metrics", "Start"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Metrics", "Start", "context already ended")
	}
	m.lifecycleMu.Lock()
	if m.used {
		m.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Metrics", "Start", "service instance already used")
	}
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	m.used = true
	m.cleanupPending = true
	m.startDone = startDone
	m.cancel = cancel
	m.lifecycleMu.Unlock()

	server := metric.NewServer(m.config.Port, m.config.Path, m.registry, m.security)
	slog.Info("Starting metrics server", "port", m.config.Port, "path", m.config.Path)
	if err := server.Start(runCtx); err != nil {
		cancel()
		m.lifecycleMu.Lock()
		m.cleanupPending = false
		m.terminal = true
		m.cancel = nil
		close(startDone)
		m.startDone = nil
		m.lifecycleMu.Unlock()
		return fmt.Errorf("start metrics server: %w", err)
	}
	m.lifecycleMu.Lock()
	m.server = server
	m.lifecycleMu.Unlock()
	if m.testServerPublished != nil {
		close(m.testServerPublished)
		<-m.testStartRelease
	}

	// The listener is owned before BaseService publishes running state. A base
	// failure therefore retains the exact provider handle for bounded rollback.
	if err := m.BaseService.Start(runCtx); err != nil {
		rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, m.cleanupFailedStart)
		m.lifecycleMu.Lock()
		if rollbackErr == nil {
			m.server = nil
			m.cancel = nil
			m.cleanupPending = false
			m.terminal = true
		}
		close(startDone)
		m.startDone = nil
		m.lifecycleMu.Unlock()
		return errors.Join(fmt.Errorf("start metrics base service: %w", err), rollbackErr)
	}
	m.lifecycleMu.Lock()
	m.running = true
	m.cleanupPending = false
	close(startDone)
	m.startDone = nil
	m.lifecycleMu.Unlock()
	scheme := "http"
	if m.security.TLS.Server.Enabled {
		scheme = "https"
	}
	slog.Info(
		"Metrics service started successfully",
		"url",
		fmt.Sprintf("%s://localhost:%d%s", scheme, m.config.Port, m.config.Path),
	)

	return nil
}

// Stop stops the metrics HTTP server
func (m *Metrics) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "Metrics", "Stop"); err != nil {
		return err
	}
	for {
		m.lifecycleMu.Lock()
		startDone := m.startDone
		if startDone != nil {
			select {
			case <-startDone:
			default:
				m.lifecycleMu.Unlock()
				if m.testStartWaitUnlocked != nil {
					m.testStartWaitUnlocked <- struct{}{}
				}
				select {
				case <-startDone:
					continue
				case <-ctx.Done():
					return fmt.Errorf("wait for Metrics Start: %w", ctx.Err())
				}
			}
		}
		if m.stopping {
			m.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("metrics service stop already in progress"),
				"Metrics", "Stop", "concurrent Stop is unsupported")
		}
		if m.terminal {
			m.lifecycleMu.Unlock()
			return nil
		}
		if !m.used {
			m.used = true
			m.terminal = true
			m.lifecycleMu.Unlock()
			return m.BaseService.Stop(ctx)
		}
		failedStart := m.cleanupPending
		m.stopping = true
		m.lifecycleMu.Unlock()

		stopErr := m.cleanup(ctx)
		m.lifecycleMu.Lock()
		if failedStart && stopErr != nil {
			m.stopping = false
			m.cleanupPending = true
			m.lifecycleMu.Unlock()
			return fmt.Errorf("stop metrics failed-Start cleanup: %w", stopErr)
		}
		m.server = nil
		m.cancel = nil
		m.running = false
		m.cleanupPending = false
		m.stopping = false
		m.terminal = true
		m.lifecycleMu.Unlock()
		slog.Info("Metrics service stopped")
		return stopErr
	}
}

func (m *Metrics) cleanupFailedStart(ctx context.Context) error { return m.cleanup(ctx) }

func (m *Metrics) cleanup(ctx context.Context) error {
	m.lifecycleMu.Lock()
	server := m.server
	cancel := m.cancel
	m.lifecycleMu.Unlock()

	var serverErr error
	if server != nil {
		serverErr = server.Stop(ctx)
	}
	if cancel != nil {
		cancel()
	}
	baseErr := m.BaseService.Stop(ctx)
	if serverErr != nil {
		slog.Error("Error stopping metrics server", "error", serverErr)
	}
	return errors.Join(serverErr, baseErr)
}

// healthCheck performs health check for metrics service
func (m *Metrics) healthCheck() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	// Simple health check - verify server is accessible
	if m.server == nil {
		return fmt.Errorf("metrics server not running")
	}

	// Could add HTTP health check here if needed
	return nil
}

// Port returns the port the metrics server is listening on
func (m *Metrics) Port() int {
	return m.config.Port
}

// Path returns the metrics endpoint path
func (m *Metrics) Path() string {
	return m.config.Path
}

// URL returns the full URL for the metrics endpoint
func (m *Metrics) URL() string {
	scheme := "http"
	if m.security.TLS.Server.Enabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d%s", scheme, m.config.Port, m.config.Path)
}

// ConfigSchema returns the configuration schema for the metrics service.
// This implements the Configurable interface for UI discovery.
func (m *Metrics) ConfigSchema() ConfigSchema {
	return NewConfigSchema(map[string]PropertySchema{
		"port": {
			PropertySchema: component.PropertySchema{
				Type:        "int",
				Description: "Port for the metrics HTTP server",
				Default:     9090,
				Minimum:     intPtr(1024),
				Maximum:     intPtr(65535),
			},
			Category: "network",
		},
		"path": {
			PropertySchema: component.PropertySchema{
				Type:        "string",
				Description: "URL path for the metrics endpoint",
				Default:     "/metrics",
			},
			Category: "network",
		},
	}, []string{}) // No required fields - all have defaults
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
