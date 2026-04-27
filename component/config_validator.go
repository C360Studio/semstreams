package component

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// ConfigPersister is the minimal kvStore-shaped surface
// ValidateAndPersistComponentConfig needs. *natsclient.KVStore
// satisfies it. Defined here so config.Manager can pass its kvStore
// through without component importing config (which would cycle:
// component → config → component).
type ConfigPersister interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
}

// ConfigComponentRegistry defines the interface needed for schema
// validation. Local-package interface so the validator helpers don't
// depend on the broader Registry surface.
type ConfigComponentRegistry interface {
	GetComponentSchema(componentType string) (ConfigSchema, error)
}

// ValidateWithSchema validates component configuration against its
// schema. Free function — takes logger explicitly so it doesn't need
// to live on config.Manager and create the config→component→agentic→
// message→config cycle.
//
// Returns validation errors if the config doesn't meet schema
// requirements. Should be called before persisting configuration to KV.
func ValidateWithSchema(
	ctx context.Context,
	logger *slog.Logger,
	registry ConfigComponentRegistry,
	componentType string,
	config map[string]any,
) []ValidationError {
	if logger == nil {
		logger = slog.Default()
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return []ValidationError{{Field: "context", Message: "validation cancelled"}}
	default:
	}

	if registry == nil {
		logger.Warn("Registry is nil, skipping schema validation", "component_type", componentType)
		return nil
	}

	// Get the component schema directly from registry
	schema, err := registry.GetComponentSchema(componentType)
	if err != nil {
		// Component type not found or error retrieving schema
		// Log warning but don't fail validation (backward compatibility)
		logger.Warn("Failed to get component schema for validation",
			"component_type", componentType,
			"error", err)
		return nil
	}

	// If schema is empty (no properties defined), skip validation
	if len(schema.Properties) == 0 {
		logger.Debug("Component has no schema defined, skipping validation",
			"component_type", componentType)
		return nil
	}

	// Validate the config against the schema
	validationErrors := ValidateConfig(config, schema)

	if len(validationErrors) > 0 {
		logger.Info("Configuration validation failed",
			"component_type", componentType,
			"error_count", len(validationErrors))
	}

	return validationErrors
}

// ValidateComponentConfig validates a component configuration from
// KV format. Convenience wrapper that handles JSON unmarshaling.
func ValidateComponentConfig(
	ctx context.Context,
	logger *slog.Logger,
	registry ConfigComponentRegistry,
	componentType string,
	configJSON json.RawMessage,
) []ValidationError {
	// Parse the config JSON into a map
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		// Return a validation error for invalid JSON
		return []ValidationError{
			{
				Field:   "",
				Message: fmt.Sprintf("Invalid JSON configuration: %v", err),
				Code:    "type",
			},
		}
	}

	return ValidateWithSchema(ctx, logger, registry, componentType, config)
}

// ValidateAndPersistComponentConfig validates a component config and
// persists it to KV via the supplied persister. Returns validation
// errors as the first error if validation fails, or a wrapped
// persistence error if KV write fails.
//
// Persister parameter (typically a *natsclient.KVStore from
// config.Manager) is taken explicitly so component doesn't import
// config — that import direction is what created the cycle this
// helper exists to avoid.
func ValidateAndPersistComponentConfig(
	ctx context.Context,
	logger *slog.Logger,
	registry ConfigComponentRegistry,
	persister ConfigPersister,
	componentName, componentType string,
	configJSON json.RawMessage,
) error {
	// First validate the configuration
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return errs.WrapInvalid(
			fmt.Errorf("invalid JSON configuration: %w", err),
			"component", "ValidateAndPersistComponentConfig", "parse config JSON")
	}

	validationErrors := ValidateWithSchema(ctx, logger, registry, componentType, config)
	if len(validationErrors) > 0 {
		// Return first validation error wrapped appropriately
		return errs.WrapInvalid(
			fmt.Errorf("configuration validation failed: %s", validationErrors[0].Message),
			"component", "ValidateAndPersistComponentConfig", "validate config")
	}

	// If validation passes, persist to KV
	key := fmt.Sprintf("components.%s", componentName)

	// The config should be the full ComponentConfig structure
	// Re-marshal the config as it may have been modified
	configData, err := json.Marshal(config)
	if err != nil {
		return errs.WrapFatal(
			fmt.Errorf("failed to marshal config: %w", err),
			"component", "ValidateAndPersistComponentConfig", "marshal config")
	}

	if _, err := persister.Put(ctx, key, configData); err != nil {
		return errs.WrapTransient(
			fmt.Errorf("failed to persist config to KV: %w", err),
			"component", "ValidateAndPersistComponentConfig", "persist to KV")
	}

	logger.Debug("Component configuration validated and persisted",
		"component_name", componentName,
		"component_type", componentType)

	return nil
}

// Compile-time assertion that *natsclient.KVStore satisfies the
// ConfigPersister interface used by ValidateAndPersistComponentConfig.
var _ ConfigPersister = (*natsclient.KVStore)(nil)
