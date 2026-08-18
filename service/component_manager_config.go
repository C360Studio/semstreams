package service

// ComponentManagerConfig configures the ComponentManager service.
//
// The ComponentManager composes one immutable boot component set.
type ComponentManagerConfig struct {
	// EnabledComponents lists component names to enable.
	// If empty, all registered components are enabled.
	// Use this to selectively enable specific components in a deployment.
	EnabledComponents []string `json:"enabled_components" schema:"type:array,description:List of component names to enable (empty=all),category:basic"`
}

// DefaultComponentManagerConfig returns the default configuration.
func DefaultComponentManagerConfig() ComponentManagerConfig {
	return ComponentManagerConfig{
		EnabledComponents: nil, // nil means all components enabled
	}
}

// Validate checks if the configuration is valid
func (c ComponentManagerConfig) Validate() error {
	// No specific validation needed for component manager config
	// Component names are validated when components are created
	// EnabledComponents can be empty (all components enabled)
	return nil
}
