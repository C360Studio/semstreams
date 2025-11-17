// Package websocket provides component registration for WebSocket input
package websocket

import (
	"encoding/json"
	"fmt"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
)

// CreateInput is the factory function for creating WebSocket input components
func CreateInput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Parse user configuration
	if len(rawConfig) > 0 {
		var userConfig Config
		if err := component.SafeUnmarshal(rawConfig, &userConfig); err != nil {
			return nil, errors.Wrap(err, "websocket-input-factory", "create", "secure config parsing")
		}

		// Apply user overrides (already validated by SafeUnmarshal)
		cfg = userConfig
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errors.WrapInvalid(fmt.Errorf("NATS client is required"),
			"websocket-input-factory", "create", "dependency validation")
	}

	// Create component
	return NewInput(
		"websocket-input", // Default name, overridden by ComponentManager
		deps.NATSClient,
		cfg,
		deps.MetricsRegistry,
		deps.Security,
	)
}

// Register registers the WebSocket input component with the registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "websocket_input",
		Factory:     CreateInput,
		Schema:      websocketInputSchema,
		Type:        "input",
		Protocol:    "websocket",
		Domain:      "network",
		Description: "WebSocket input for receiving federated data from remote StreamKit instances",
		Version:     "1.0.0",
	})
}
