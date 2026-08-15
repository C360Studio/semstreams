package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/security"
)

// Metrics is a service that provides Prometheus metrics endpoint
type Metrics struct {
	*BaseService

	config       MetricsConfig           // Consistent config field
	server       metricsServer           // Runtime state
	registry     *metric.MetricsRegistry // Dependency
	natsClient   *natsclient.Client      // For JetStream metrics publishing
	security     security.Config         // Platform security config
	generation   *lifecyclejoin.Generation
	teardownOnce sync.Once
	teardownErr  error
}

type metricsServer interface {
	Start() error
	Stop() error
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
	runCtx, cancel := context.WithCancel(ctx)
	generation := lifecyclejoin.NewGeneration(cancel, nil)
	// Call BaseService Start first
	if err := m.BaseService.Start(runCtx); err != nil {
		cancel()
		return err
	}

	m.mu.Lock()

	if m.server != nil {
		m.mu.Unlock()
		return fmt.Errorf("metrics server already started")
	}

	// Bind synchronously while holding lifecycle exclusion. Stop cannot overtake
	// an unstarted server, and a bind failure cannot be reported as success.
	server := metric.NewServer(m.config.Port, m.config.Path, m.registry, m.security)
	slog.Info("Starting metrics server", "port", m.config.Port, "path", m.config.Path)
	if err := server.Start(); err != nil {
		m.mu.Unlock()
		generation.Cancel()
		if stopErr := lifecyclejoin.RunPartialStartRollback(m.BaseService.Stop); stopErr != nil {
			return errors.Join(fmt.Errorf("start metrics server: %w", err), stopErr)
		}
		return fmt.Errorf("start metrics server: %w", err)
	}
	m.server = server
	m.generation = generation
	m.teardownOnce = sync.Once{}
	m.teardownErr = nil

	m.mu.Unlock()
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
	m.mu.Lock()
	generation := m.generation
	m.mu.Unlock()
	if generation == nil {
		return nil
	}

	return generation.Stop(ctx, nil, func(ctx context.Context) error {
		m.teardownOnce.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.server != nil {
				if err := m.server.Stop(); err != nil {
					m.teardownErr = fmt.Errorf("failed to stop metrics server: %w", err)
					slog.Error("Error stopping metrics server", "error", err)
				} else {
					m.server = nil
				}
			}
		})
		baseErr := m.BaseService.Stop(ctx)
		if ctx.Err() != nil && errors.Is(baseErr, ctx.Err()) {
			return errors.Join(m.teardownErr, baseErr)
		}
		slog.Info("Metrics service stopped")
		return errors.Join(m.teardownErr, baseErr)
	})
}

// healthCheck performs health check for metrics service
func (m *Metrics) healthCheck() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

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
