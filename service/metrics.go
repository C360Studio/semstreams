package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/pkg/security"
)

// Metrics is a service that provides Prometheus metrics endpoint
type Metrics struct {
	*BaseService

	config   MetricsConfig           // Consistent config field
	server   *metric.Server          // Runtime state
	registry *metric.MetricsRegistry // Dependency
	security security.Config         // Platform security config
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
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse metrics config: %w", err)
		}
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
	// Call BaseService Start first
	if err := m.BaseService.Start(ctx); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		return fmt.Errorf("metrics server already started")
	}

	// Create the metric server with platform security
	m.server = metric.NewServer(m.config.Port, m.config.Path, m.registry, m.security)

	// Start the server in a goroutine
	go func() {
		slog.Info("Starting metrics server", "port", m.config.Port, "path", m.config.Path)
		if err := m.server.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("Metrics server error", "error", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

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
func (m *Metrics) Stop(timeout time.Duration) error {
	m.mu.Lock()

	if m.server != nil {
		// Stop the metrics server
		if err := m.server.Stop(); err != nil {
			slog.Error("Error stopping metrics server", "error", err)
			m.mu.Unlock()
			return fmt.Errorf("failed to stop metrics server: %w", err)
		}
		m.server = nil
	}

	m.mu.Unlock()

	// Call BaseService Stop to handle status changes
	if err := m.BaseService.Stop(timeout); err != nil {
		return err
	}

	slog.Info("Metrics service stopped")

	return nil
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
		"enabled": {
			PropertySchema: component.PropertySchema{
				Type:        "bool",
				Description: "Enable or disable the metrics service",
				Default:     true,
			},
			Runtime: false, // Requires restart to enable/disable
		},
		"port": {
			PropertySchema: component.PropertySchema{
				Type:        "int",
				Description: "Port for the metrics HTTP server",
				Default:     9090,
				Minimum:     intPtr(1024),
				Maximum:     intPtr(65535),
			},
			Runtime:  false, // Port change requires restart
			Category: "network",
		},
		"path": {
			PropertySchema: component.PropertySchema{
				Type:        "string",
				Description: "URL path for the metrics endpoint",
				Default:     "/metrics",
			},
			Runtime:  false, // Path change requires restart
			Category: "network",
		},
	}, []string{}) // No required fields - all have defaults
}

// ValidateConfigUpdate validates runtime configuration changes
func (m *Metrics) ValidateConfigUpdate(changes map[string]any) error {
	// Note: Port and path changes require restart, so we don't allow them at runtime
	for key := range changes {
		switch key {
		case "enabled":
			// Enabled can be changed at runtime
			if _, ok := changes[key].(bool); !ok {
				return fmt.Errorf("enabled must be a boolean")
			}
		default:
			return fmt.Errorf("runtime update of '%s' is not supported (requires restart)", key)
		}
	}
	return nil
}

// ApplyConfigUpdate applies validated runtime configuration changes
func (m *Metrics) ApplyConfigUpdate(changes map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Note: Currently no runtime-modifiable properties for metrics
	// Port and path changes require restart
	// This is here for future extensibility

	if enabled, ok := changes["enabled"].(bool); ok {
		// The enabled state is managed by Manager
		// This is just for tracking
		m.logger.Info("Metrics enabled state changed", "enabled", enabled)
	}

	return nil
}

// GetRuntimeConfig returns current runtime configuration
func (m *Metrics) GetRuntimeConfig() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]any{
		"enabled": true, // Metrics service is running if this method is called
		"port":    m.config.Port,
		"path":    m.config.Path,
	}
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
