package rule

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
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
// Delegates to component.BuildPortFromDefinition for consistent port type handling
func convertDefinitionToPort(portDef component.PortDefinition, direction component.Direction) component.Port {
	return component.BuildPortFromDefinition(portDef, direction)
}

// CreateRuleProcessor creates a rule processor with the new factory pattern
func CreateRuleProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(fmt.Errorf("NATS client is required"),
			"rule-processor-factory", "create", "NATS client validation")
	}

	// Start with defaults
	ruleConfig := DefaultConfig()
	if len(rawConfig) > 0 {
		// Parse user config
		var userConfig Config
		if err := json.Unmarshal(rawConfig, &userConfig); err != nil {
			return nil, errs.WrapInvalid(err, "rule-processor-factory", "create", "parse config")
		}

		// Apply user overrides
		if userConfig.Ports != nil {
			ruleConfig.Ports = userConfig.Ports
		}
		if len(userConfig.RulesFiles) > 0 {
			ruleConfig.RulesFiles = userConfig.RulesFiles
		}
		if len(userConfig.InlineRules) > 0 {
			ruleConfig.InlineRules = userConfig.InlineRules
		}
		ruleConfig.MessageCache = userConfig.MessageCache
		ruleConfig.BufferWindowSize = userConfig.BufferWindowSize
		ruleConfig.AlertCooldownPeriod = userConfig.AlertCooldownPeriod
		ruleConfig.EnableGraphIntegration = userConfig.EnableGraphIntegration
		if len(userConfig.EntityWatchPatterns) > 0 {
			ruleConfig.EntityWatchPatterns = userConfig.EntityWatchPatterns
		}
		if len(userConfig.EntityWatchBuckets) > 0 {
			ruleConfig.EntityWatchBuckets = userConfig.EntityWatchBuckets
		}
		ruleConfig.Consumer = userConfig.Consumer

		// Note: InputSubjects no longer supported - use Ports configuration only
	}

	// Create processor with metrics if available
	processor, err := NewProcessorWithMetrics(deps.NATSClient, &ruleConfig, deps.MetricsRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create rule processor: %w", err)
	}

	// Propagate the shared tool registry so publish_agent's
	// default_tools can resolve tool definitions at action time.
	processor.SetToolRegistry(deps.ToolRegistry)

	// Set logger from dependencies
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	processor.logger = logger.With("component", "rule-processor")

	return processor, nil
}
