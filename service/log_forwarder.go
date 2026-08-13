// Package service provides the LogForwarder service for log management.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/internal/logforwarderpolicy"
)

// NewLogForwarderService creates a new log forwarder service using the standard constructor pattern.
// With the new architecture, LogForwarder no longer intercepts slog - logs are published to NATS
// directly by the NATSLogHandler in pkg/logging. This service exists for configuration management
// and potential future features (e.g., log aggregation, filtering at the service level).
func NewLogForwarderService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	policy, err := logforwarderpolicy.Resolve(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("parse log-forwarder config: %w", err)
	}
	cfg := LogForwarderConfig{
		MinLevel:       policy.MinLevel.String(),
		ExcludeSources: policy.ExcludeSources,
	}

	// Create the LogForwarder with dependencies
	var opts []Option
	if deps.Logger != nil {
		opts = append(opts, WithLogger(deps.Logger))
	}
	if deps.MetricsRegistry != nil {
		opts = append(opts, WithMetrics(deps.MetricsRegistry))
	}

	return NewLogForwarder(&cfg, opts...)
}

// LogForwarderConfig holds configuration for the LogForwarder service.
// Note: The service is enabled/disabled via types.ServiceConfig.Enabled at the outer level.
//
// Configuration is used by:
// - NATSLogHandler (in pkg/logging) for min_level and exclude_sources filtering
// - This service for configuration validation
type LogForwarderConfig struct {
	// MinLevel is the minimum log level to forward to NATS (DEBUG, INFO, WARN, ERROR).
	// Logs below this level are still written to stdout but not published to NATS.
	MinLevel string `json:"min_level"`

	// ExcludeSources is a list of source prefixes to exclude from NATS forwarding.
	// Logs from excluded sources still go to stdout but are not published to NATS.
	// Uses prefix matching with dotted notation: excluding "flow-service.websocket"
	// also excludes "flow-service.websocket.health" but NOT "flow-service".
	ExcludeSources []string `json:"exclude_sources"`
}

// Validate checks if the configuration is valid.
func (c LogForwarderConfig) Validate() error {
	return logforwarderpolicy.ValidateFields(c.MinLevel, c.ExcludeSources)
}

// LogForwarder is a service for log configuration management.
// With the new architecture, actual log forwarding to NATS is handled by NATSLogHandler
// in pkg/logging. This service validates configuration and provides a service endpoint.
type LogForwarder struct {
	*BaseService
	config LogForwarderConfig
}

// NewLogForwarder creates a new LogForwarder service.
func NewLogForwarder(config *LogForwarderConfig, opts ...Option) (*LogForwarder, error) {
	if config == nil {
		policy, err := logforwarderpolicy.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("resolve default log-forwarder policy: %w", err)
		}
		config = &LogForwarderConfig{
			MinLevel:       policy.MinLevel.String(),
			ExcludeSources: policy.ExcludeSources,
		}
	}

	// Create base service
	baseService := NewBaseServiceWithOptions("log-forwarder", nil, opts...)

	lf := &LogForwarder{
		BaseService: baseService,
		config:      *config,
	}

	return lf, nil
}

// Start begins the LogForwarder service.
// Note: Log forwarding to NATS is handled by NATSLogHandler in main.go.
// This service provides configuration validation and service lifecycle management.
func (lf *LogForwarder) Start(ctx context.Context) error {
	if err := lf.BaseService.Start(ctx); err != nil {
		return err
	}

	lf.logger.Info("LogForwarder started (NATS bridge mode)",
		"min_level", lf.config.MinLevel,
		"exclude_sources", lf.config.ExcludeSources)

	return nil
}

// Stop gracefully stops the LogForwarder.
func (lf *LogForwarder) Stop(timeout time.Duration) error {
	lf.logger.Info("LogForwarder stopping")
	return lf.BaseService.Stop(timeout)
}

// Config returns the LogForwarder configuration.
// This can be used by other components to access the log configuration.
func (lf *LogForwarder) Config() LogForwarderConfig {
	return lf.config
}
