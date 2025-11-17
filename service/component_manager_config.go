package service

// ComponentManagerConfig configures the ComponentManager service
// Simple struct - no UnmarshalJSON, no Enabled field
type ComponentManagerConfig struct {
	WatchConfig       bool     `json:"watch_config"`       // Enable config watching via NATS
	EnabledComponents []string `json:"enabled_components"` // List of component names to enable
}

// Validate checks if the configuration is valid
func (c ComponentManagerConfig) Validate() error {
	// No specific validation needed for component manager config
	// Component names are validated when components are created
	// WatchConfig is a boolean (no validation needed)
	// EnabledComponents can be empty (all components enabled)
	return nil
}
