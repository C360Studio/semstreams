package component

import (
	"log/slog"

	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/security"
	"github.com/c360/semstreams/types"
)

// PlatformMeta provides platform identity to components.
// Type alias to avoid import cycles while maintaining compatibility.
type PlatformMeta = types.PlatformMeta

// Dependencies provides all external dependencies needed by components.
// This structure follows the same pattern as Dependencies, enabling
// components to receive properly structured dependencies rather than individual fields.
type Dependencies struct {
	NATSClient      *natsclient.Client      // NATS client for messaging
	QueryManager    any                     // Query manager for graph queries (type: querymanager.Querier, can be nil)
	MetricsRegistry *metric.MetricsRegistry // Metrics registry for Prometheus (can be nil)
	Logger          *slog.Logger            // Structured logger (can be nil, defaults to slog.Default())
	Platform        PlatformMeta            // Platform identity (organization and platform)
	Security        security.Config         // Platform-wide security configuration
}

// GetLogger returns the configured logger or a default logger if none is provided
func (d *Dependencies) GetLogger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// GetLoggerWithComponent returns a logger configured with component context
func (d *Dependencies) GetLoggerWithComponent(componentName string) *slog.Logger {
	return d.GetLogger().With("component", componentName)
}
