// Package types contains shared domain types used across the semstreams platform
package types

import (
	"encoding/json"
	"fmt"

	"github.com/c360/semstreams/errors"
)

// ComponentType represents the category of a component
type ComponentType string

// Component type constants
const (
	ComponentTypeInput     ComponentType = "input"
	ComponentTypeProcessor ComponentType = "processor"
	ComponentTypeOutput    ComponentType = "output"
	ComponentTypeStorage   ComponentType = "storage"
	ComponentTypeGateway   ComponentType = "gateway"
)

// ComponentConfig provides configuration for creating a component instance
// The instance name comes from the map key in the components configuration.
// This structure is shared between the config and component packages.
type ComponentConfig struct {
	Type    ComponentType   `json:"type"`    // Component type (input/processor/output/storage/gateway)
	Name    string          `json:"name"`    // Factory/component name (e.g., "udp", "websocket", "mavlink")
	Enabled bool            `json:"enabled"` // Whether component is enabled
	Config  json.RawMessage `json:"config"`  // Component-specific configuration
}

// Validate ensures the component configuration is valid
func (c ComponentConfig) Validate() error {
	if c.Type == "" {
		return errors.WrapInvalid(
			errors.ErrMissingConfig,
			"ComponentConfig",
			"Validate",
			"component type cannot be empty",
		)
	}
	if c.Name == "" {
		return errors.WrapInvalid(
			errors.ErrMissingConfig,
			"ComponentConfig",
			"Validate",
			"component factory name cannot be empty",
		)
	}

	switch c.Type {
	case ComponentTypeInput, ComponentTypeProcessor, ComponentTypeOutput, ComponentTypeStorage, ComponentTypeGateway:
		return nil
	default:
		return errors.WrapInvalid(errors.ErrInvalidConfig, "ComponentConfig", "Validate",
			fmt.Sprintf("invalid component type: %s", c.Type))
	}
}

// String implements fmt.Stringer for ComponentType
func (ct ComponentType) String() string {
	return string(ct)
}

// PlatformMeta provides platform identity to services and components.
// This structure decouples platform identity from the config package,
// allowing services to access org and platform information without
// creating dependencies on configuration structures.
type PlatformMeta struct {
	Org      string // Organization namespace (e.g., "c360", "noaa")
	Platform string // Platform identifier (e.g., "platform1", "vessel-alpha")
}
