package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MinimalConfig represents the core application configuration
// This is a simplified version focusing only on essential services
type MinimalConfig struct {
	Platform PlatformConfig     `json:"platform"`
	NATS     NATSConfig         `json:"nats"`
	Services CoreServicesConfig `json:"services"`
}

// CoreServicesConfig defines which core services to enable
type CoreServicesConfig struct {
	MessageLogger bool `json:"message_logger"` // Debug tool
	Discovery     bool `json:"discovery"`      // Component discovery
}

// Validate checks if the minimal config is valid
func (c *MinimalConfig) Validate() error {
	if c.Platform.ID == "" {
		return errors.New("platform.id is required")
	}

	if len(c.NATS.URLs) == 0 {
		return errors.New("nats.urls is required")
	}

	// Graph validation moved to graph-processor component

	return nil
}

// LoadMinimalConfig loads configuration from a file
func LoadMinimalConfig(path string) (*MinimalConfig, error) {
	// Use secure file reading with validation
	data, err := safeReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Validate JSON depth to prevent DoS
	if err := validateJSONDepth(data); err != nil {
		return nil, fmt.Errorf("invalid JSON structure: %w", err)
	}

	var config MinimalConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Graph and ObjectStore defaults moved to component constructors

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// ToJSON converts config to JSON string for debugging
func (c *MinimalConfig) ToJSON() (string, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
