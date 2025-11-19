package rule

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
)

// Register registers the rule processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "rule-processor",
		Factory:     CreateRuleProcessor,
		Schema:      schema,
		Type:        "processor",
		Protocol:    "rule",
		Domain:      "semantic",
		Description: "Rule execution processor",
		Version:     "1.0.0",
	})
}

// convertDefinitionToPort converts a PortDefinition to Port
func convertDefinitionToPort(portDef component.PortDefinition, direction component.Direction) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   direction,
		Required:    portDef.Required,
		Description: portDef.Description,
	}

	// Create appropriate config based on type
	switch portDef.Type {
	case "nats":
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	case "nats-request":
		port.Config = component.NATSRequestPort{
			Subject: portDef.Subject,
			Timeout: portDef.Timeout,
		}
	case "kv-watch":
		// For KV watch ports, we'd need additional fields in PortDefinition
		// For now, this is a minimal implementation
		port.Config = component.KVWatchPort{
			Bucket: "ENTITY_STATES", // Default bucket
		}
	}

	return port
}

// CreateRuleProcessor creates a rule processor with the new factory pattern
func CreateRuleProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errors.WrapInvalid(fmt.Errorf("NATS client is required"),
			"rule-processor-factory", "create", "NATS client validation")
	}

	// Start with defaults
	ruleConfig := DefaultConfig()
	if len(rawConfig) > 0 {
		// Parse user config
		var userConfig Config
		if err := json.Unmarshal(rawConfig, &userConfig); err != nil {
			return nil, errors.WrapInvalid(err, "rule-processor-factory", "create", "parse config")
		}

		// Apply user overrides
		if userConfig.Ports != nil {
			ruleConfig.Ports = userConfig.Ports
		}
		if len(userConfig.EnabledRules) > 0 {
			ruleConfig.EnabledRules = userConfig.EnabledRules
		}
		ruleConfig.MessageCache = userConfig.MessageCache
		ruleConfig.BufferWindowSize = userConfig.BufferWindowSize
		ruleConfig.AlertCooldownPeriod = userConfig.AlertCooldownPeriod
		ruleConfig.EnableGraphIntegration = userConfig.EnableGraphIntegration
		ruleConfig.EntityWatchPatterns = userConfig.EntityWatchPatterns
		ruleConfig.Consumer = userConfig.Consumer

		// Note: InputSubjects no longer supported - use Ports configuration only
	}

	// Create processor with metrics if available
	processor := NewProcessorWithMetrics(deps.NATSClient, &ruleConfig, deps.MetricsRegistry)

	// Set logger from dependencies
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	processor.logger = logger.With("component", "rule-processor")

	return processor, nil
}
