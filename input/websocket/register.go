// Package websocket provides component registration for WebSocket input
package websocket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// CreateInput is the factory function for creating WebSocket input components
func CreateInput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Parse user configuration
	if len(rawConfig) > 0 {
		if err := decodeInputConfig(rawConfig, &cfg); err != nil {
			return nil, errs.Wrap(err, "websocket-input-factory", "create", "secure config parsing")
		}
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(fmt.Errorf("NATS client is required"),
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

func decodeInputConfig(rawConfig json.RawMessage, cfg *Config) error {
	if err := component.ValidateFactoryConfig(rawConfig); err != nil {
		return err
	}
	normalized, err := normalizeInputDurationStrings(rawConfig)
	if err != nil {
		return err
	}
	return component.SafeUnmarshal(normalized, cfg)
}

func normalizeInputDurationStrings(rawConfig json.RawMessage) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &root); err != nil {
		return nil, fmt.Errorf("decode websocket config: %w", err)
	}

	for _, duration := range []struct {
		path  []string
		label string
	}{
		{path: []string{"client", "reconnect", "initial_interval"}, label: "client.reconnect.initial_interval"},
		{path: []string{"client", "reconnect", "max_interval"}, label: "client.reconnect.max_interval"},
		{path: []string{"bidirectional", "request_timeout"}, label: "bidirectional.request_timeout"},
	} {
		if err := normalizeDurationStringAtPath(root, duration.path, duration.label); err != nil {
			return nil, err
		}
	}

	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode websocket config: %w", err)
	}
	return normalized, nil
}

func normalizeDurationStringAtPath(
	object map[string]json.RawMessage,
	path []string,
	label string,
) error {
	raw, exists := object[path[0]]
	if !exists {
		return nil
	}
	if len(path) == 1 {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '"' {
			return nil
		}
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("%s: decode duration string: %w", label, err)
		}
		duration, err := time.ParseDuration(text)
		if err != nil {
			return fmt.Errorf("%s: invalid duration %q: %w", label, text, err)
		}
		encoded, err := json.Marshal(int64(duration))
		if err != nil {
			return fmt.Errorf("%s: encode duration: %w", label, err)
		}
		object[path[0]] = encoded
		return nil
	}

	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return fmt.Errorf("%s: decode containing object: %w", label, err)
	}
	if err := normalizeDurationStringAtPath(nested, path[1:], label); err != nil {
		return err
	}
	encoded, err := json.Marshal(nested)
	if err != nil {
		return fmt.Errorf("%s: encode containing object: %w", label, err)
	}
	object[path[0]] = encoded
	return nil
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
