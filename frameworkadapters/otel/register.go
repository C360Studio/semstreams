// Package otel provides explicit composition for the optional OpenTelemetry
// exporter. It is kept outside the core registration import root.
package otel

import (
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	otelexporter "github.com/c360studio/semstreams/output/otel"
)

// Selected reports whether an enabled component selects the OTEL adapter.
func Selected(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, componentConfig := range cfg.Components {
		if componentConfig.Enabled && componentConfig.Name == "otel-exporter" {
			return true
		}
	}
	return false
}

// Register makes the optional exporter factory available to the binary.
func Register(registry *component.Registry) error {
	return otelexporter.Register(registry)
}
