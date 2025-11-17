// Package types contains shared domain types used across the semstreams platform
package types

import (
	"encoding/json"

	"github.com/c360/semstreams/errors"
)

// ServiceConfig provides configuration for creating a service instance.
// This standardizes service configuration similar to ComponentConfig,
// providing metadata (name, enabled) separate from service-specific config.
type ServiceConfig struct {
	Name    string          `json:"name"`    // Service name (redundant with map key but useful for validation)
	Enabled bool            `json:"enabled"` // Whether service is enabled at runtime
	Config  json.RawMessage `json:"config"`  // Service-specific configuration
}

// Validate ensures the service configuration is valid
func (s ServiceConfig) Validate() error {
	if s.Name == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "ServiceConfig", "Validate", "service name cannot be empty")
	}
	// Config can be empty (service uses defaults)
	// Enabled can be false (service disabled)
	return nil
}

// ServiceConfigs holds service instance configurations.
// The map key is the service name (e.g., "metrics", "discovery").
// Services are only created if both:
// 1. They have registered a constructor via init()
// 2. They have an entry in this config map with enabled=true
type ServiceConfigs map[string]ServiceConfig
