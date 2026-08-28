package rule

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Register registers the rule processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "rule-processor",
		Factory:     CreateRuleProcessor,
		Ports:       DeclarePorts,
		Schema:      schema,
		Type:        "processor",
		Protocol:    "rule",
		Domain:      "semantic",
		Description: "Rule execution processor",
		Version:     "1.0.0",
	})
}

// DeclarePorts is the component.PortDeclarer for rule-processor: the
// configured ports (defaults when none are configured), resolved exactly as
// the processor's setupPorts resolves them.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	ruleConfig, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	if ruleConfig.Ports == nil {
		return component.PortConfig{}, fmt.Errorf("rule processor config missing required Ports configuration")
	}
	inputs, outputs, err := resolvePorts(*ruleConfig.Ports)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(inputs, outputs), nil
}

// resolveConfig overlays the operator configuration on the identity-free
// defaults and validates the result. It is the one derivation DeclarePorts and
// CreateRuleProcessor share; resolvePorts is the one port derivation.
func resolveConfig(rawConfig json.RawMessage) (Config, error) {
	ruleConfig := defaultConfig()
	if len(rawConfig) > 0 {
		// Parse user config
		var userConfig Config
		if err := json.Unmarshal(rawConfig, &userConfig); err != nil {
			return Config{}, errs.WrapInvalid(err, "rule-processor-factory", "create", "parse config")
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
		if len(userConfig.EntityWatchBuckets) > 0 {
			ruleConfig.EntityWatchBuckets = userConfig.EntityWatchBuckets
		}
		ruleConfig.Consumer = userConfig.Consumer

		// ENTITY_STATES startup-wait budget (gh#610). Overlay only positive
		// values: this factory copies field-by-field onto defaultConfig(), so an
		// omitted knob arrives as 0 and must not clobber the sibling-reader
		// default. Without this overlay the knobs would be accepted, validated,
		// and published in the schema while the processor kept the hard-coded
		// budget — an operator-facing lie of exactly the kind this program exists
		// to delete.
		if userConfig.StartupAttempts > 0 {
			ruleConfig.StartupAttempts = userConfig.StartupAttempts
		}
		if userConfig.StartupInterval > 0 {
			ruleConfig.StartupInterval = userConfig.StartupInterval
		}

		// Preserve nil versus non-nil: nil means omission-based derivation,
		// while an explicitly authored [] is an empty override.
		ruleConfig.PackID = userConfig.PackID
		ruleConfig.ProjectionContracts = cloneProjectionContracts(userConfig.ProjectionContracts)

		// Note: InputSubjects no longer supported - use Ports configuration only
	}

	// Fail fast on a malformed pack_id: the derived owner id "rule-pack.<id>"
	// must be subject-safe, and a config-time reject is far clearer than a
	// silent RegisterOwner failure at the composition root.
	if err := ruleConfig.Validate(); err != nil {
		return Config{}, errs.WrapInvalid(err, "rule-processor-factory", "create", "validate config")
	}
	return ruleConfig, nil
}

// CreateRuleProcessor creates a rule processor with the new factory pattern
func CreateRuleProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(fmt.Errorf("NATS client is required"),
			"rule-processor-factory", "create", "NATS client validation")
	}

	// Every identity the rule engine mints — trigger entities, run-scope mints
	// — takes positions 1-2 from here, and the run-anchor skip decides
	// foreign-vs-local by comparing against it (ADR-102 d2/d5). An absent pair
	// would make the engine either mint an invalid identity or judge every
	// firing entity foreign, both silently. Config load already requires
	// platform.org and platform.id, so a real deployment cannot reach this.
	if deps.Platform.Org == "" || deps.Platform.Platform == "" {
		return nil, errs.WrapInvalid(
			fmt.Errorf("deps.Platform must carry the deployment authority (platform.org and platform.id)"),
			"rule-processor-factory", "create", "Platform validation")
	}

	ruleConfig, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	// Create processor with metrics if available
	processor, err := NewProcessorWithMetrics(deps.NATSClient, &ruleConfig, deps.MetricsRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create rule processor: %w", err)
	}

	// The deployment's own authority for every identity the rule engine mints
	// (trigger entities; run-scope mints under #1096). Installed before rules
	// load so every rule carries it (ADR-102 d2).
	processor.SetPlatform(deps.Platform)

	// Propagate the shared tool registry so publish_agent's
	// default_tools can resolve tool definitions at action time.
	processor.SetToolRegistry(deps.ToolRegistry)

	// Propagate the payload Decoder so handleSemanticMessage can
	// unmarshal incoming envelopes against the shared payload registry.
	processor.SetDecoder(message.NewDecoder(deps.PayloadRegistry))

	// Propagate the Lifecycle harness Manager (ADR-047) so the
	// lifecycle_* action family + $entity.lifecycle.* condition-field
	// resolution can dispatch through it. Nil-safe: apps without
	// lifecycle-managed entity types pass no Manager and the rule
	// engine surfaces a wiring error if a rule references the
	// harness anyway.
	if deps.LifecycleManager != nil {
		processor.SetLifecycleManager(deps.LifecycleManager)
	}

	// Set logger from dependencies
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	processor.logger = logger.With("component", "rule-processor")

	return processor, nil
}
