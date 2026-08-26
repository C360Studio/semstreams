package otel

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the interface for component registration.
type RegistryInterface interface {
	RegisterWithConfig(config component.RegistrationConfig) error
}

// Register registers the OTEL exporter component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	return registry.RegisterWithConfig(component.RegistrationConfig{
		Type:        "output",
		Name:        "otel-exporter",
		Description: "Exports agent telemetry to OpenTelemetry collectors",
		Version:     "1.0.0",
		Factory:     NewComponent,
		Ports:       DeclarePorts,
		Schema:      componentSchema,
	})
}
