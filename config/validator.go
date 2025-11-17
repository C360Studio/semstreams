package config

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
)

// ComponentRegistry defines the interface needed for schema validation
// This allows dependency injection and testing
type ComponentRegistry interface {
	GetComponentSchema(componentType string) (component.ConfigSchema, error)
}

// ValidateWithSchema validates component configuration against its schema
// Returns validation errors if the config doesn't meet schema requirements
// This function should be called before persisting configuration to KV
func (cm *Manager) ValidateWithSchema(
	ctx context.Context,
	registry ComponentRegistry,
	componentType string,
	config map[string]any,
) []component.ValidationError {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return []component.ValidationError{{Field: "context", Message: "validation cancelled"}}
	default:
	}

	if registry == nil {
		cm.logger.Warn("Registry is nil, skipping schema validation", "component_type", componentType)
		return nil
	}

	// Get the component schema directly from registry
	schema, err := registry.GetComponentSchema(componentType)
	if err != nil {
		// Component type not found or error retrieving schema
		// Log warning but don't fail validation (backward compatibility)
		cm.logger.Warn("Failed to get component schema for validation",
			"component_type", componentType,
			"error", err)
		return nil
	}

	// If schema is empty (no properties defined), skip validation
	if schema.Properties == nil || len(schema.Properties) == 0 {
		cm.logger.Debug("Component has no schema defined, skipping validation",
			"component_type", componentType)
		return nil
	}

	// Validate the config against the schema
	validationErrors := component.ValidateConfig(config, schema)

	if len(validationErrors) > 0 {
		cm.logger.Info("Configuration validation failed",
			"component_type", componentType,
			"error_count", len(validationErrors))
	}

	return validationErrors
}

// ValidateComponentConfig validates a component configuration from KV format
// This is a convenience method that handles JSON unmarshaling
func (cm *Manager) ValidateComponentConfig(
	ctx context.Context,
	registry ComponentRegistry,
	componentType string,
	configJSON json.RawMessage,
) []component.ValidationError {
	// Parse the config JSON into a map
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		// Return a validation error for invalid JSON
		return []component.ValidationError{
			{
				Field:   "",
				Message: fmt.Sprintf("Invalid JSON configuration: %v", err),
				Code:    "type",
			},
		}
	}

	return cm.ValidateWithSchema(ctx, registry, componentType, config)
}

// ValidateAndPersistComponentConfig validates and persists component configuration to KV
// This method combines validation with persistence in a single operation
// Returns validation errors if validation fails, or a persistence error wrapped appropriately
func (cm *Manager) ValidateAndPersistComponentConfig(
	ctx context.Context,
	registry ComponentRegistry,
	componentName, componentType string,
	configJSON json.RawMessage,
) error {
	// First validate the configuration
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return errors.WrapInvalid(
			fmt.Errorf("invalid JSON configuration: %w", err),
			"Manager", "ValidateAndPersistComponentConfig", "parse config JSON")
	}

	validationErrors := cm.ValidateWithSchema(ctx, registry, componentType, config)
	if len(validationErrors) > 0 {
		// Return first validation error wrapped appropriately
		return errors.WrapInvalid(
			fmt.Errorf("configuration validation failed: %s", validationErrors[0].Message),
			"Manager", "ValidateAndPersistComponentConfig", "validate config")
	}

	// If validation passes, persist to KV
	key := fmt.Sprintf("components.%s", componentName)

	// The config should be the full ComponentConfig structure
	// Re-marshal the config as it may have been modified
	configData, err := json.Marshal(config)
	if err != nil {
		return errors.WrapFatal(
			fmt.Errorf("failed to marshal config: %w", err),
			"Manager", "ValidateAndPersistComponentConfig", "marshal config")
	}

	_, err = cm.kvStore.Put(ctx, key, configData)
	if err != nil {
		return errors.WrapTransient(
			fmt.Errorf("failed to persist config to KV: %w", err),
			"Manager", "ValidateAndPersistComponentConfig", "persist to KV")
	}

	cm.logger.Info("Component configuration validated and persisted",
		"component_name", componentName,
		"component_type", componentType)

	return nil
}
