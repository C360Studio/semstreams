package service

import (
	"github.com/c360/semstreams/component"
)

// Configurable is an optional interface for services that expose their configuration schema.
// This enables UI discovery and validation of service configurations.
// Services implementing this interface can describe their configuration parameters,
// including which fields can be changed at runtime without restart.
//
// NOTE: This interface is reserved for future UI features. Currently only the Metrics
// service implements it. The Discovery service exists to expose this information via HTTP API.
type Configurable interface {
	// ConfigSchema returns the configuration schema for this service
	ConfigSchema() ConfigSchema
}

// RuntimeConfigurable is an optional interface for services that support
// runtime configuration updates without restart.
type RuntimeConfigurable interface {
	Configurable

	// ValidateConfigUpdate checks if the proposed changes are valid
	ValidateConfigUpdate(changes map[string]any) error

	// ApplyConfigUpdate applies validated configuration changes
	ApplyConfigUpdate(changes map[string]any) error

	// GetRuntimeConfig returns current runtime configuration values
	GetRuntimeConfig() map[string]any
}

// ConfigSchema describes the configuration parameters for a service.
// We embed the component ConfigSchema for consistency across the system.
type ConfigSchema struct {
	component.ConfigSchema

	// ServiceSpecific can hold any service-specific schema extensions
	ServiceSpecific map[string]any `json:"service_specific,omitempty"`
}

// PropertySchema extends component.PropertySchema with service-specific fields
type PropertySchema struct {
	component.PropertySchema

	// Runtime indicates if this property can be changed without restart
	Runtime bool `json:"runtime,omitempty"`

	// Category groups related properties for UI organization
	Category string `json:"category,omitempty"`
}

// NewConfigSchema creates a service ConfigSchema with extended property schemas
func NewConfigSchema(properties map[string]PropertySchema, required []string) ConfigSchema {
	// Convert our extended PropertySchema to component.PropertySchema
	componentProps := make(map[string]component.PropertySchema)
	for key, prop := range properties {
		componentProps[key] = prop.PropertySchema
	}

	return ConfigSchema{
		ConfigSchema: component.ConfigSchema{
			Properties: componentProps,
			Required:   required,
		},
	}
}
