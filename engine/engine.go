package flowengine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/flowstore"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
)

// Engine translates Flow entities into ComponentConfigs and manages deployment lifecycle
type Engine struct {
	configMgr         *config.Manager
	flowStore         *flowstore.Store
	componentRegistry *component.Registry
	natsClient        *natsclient.Client
	logger            *slog.Logger
	metrics           *engineMetrics
}

// NewEngine creates a new flow engine
func NewEngine(
	configMgr *config.Manager,
	flowStore *flowstore.Store,
	componentRegistry *component.Registry,
	natsClient *natsclient.Client,
	logger *slog.Logger,
	metricsRegistry *metric.MetricsRegistry,
) *Engine {
	// Initialize metrics if registry provided
	metrics, err := newEngineMetrics(metricsRegistry)
	if err != nil {
		logger.Error("Failed to initialize flow engine metrics", "error", err)
		metrics = nil // Continue without metrics
	}

	return &Engine{
		configMgr:         configMgr,
		flowStore:         flowStore,
		componentRegistry: componentRegistry,
		natsClient:        natsClient,
		logger:            logger,
		metrics:           metrics,
	}
}

// ValidateFlowDefinition validates a flow without deploying it
// Returns full validation results including port information and discovered connections
func (e *Engine) ValidateFlowDefinition(flow *flowstore.Flow) (*ValidationResult, error) {
	start := time.Now()
	var validationErr error

	defer func() {
		duration := time.Since(start).Seconds()
		e.metrics.recordValidation(flow.ID, duration, validationErr)
	}()

	// Layer 1: Basic structural validation
	if err := flow.Validate(); err != nil {
		validationErr = err
		return nil, errors.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "basic validation failed")
	}

	// Layer 2: FlowGraph validation with port discovery
	validator := NewValidator(e.componentRegistry, e.natsClient, e.logger)
	result, err := validator.ValidateFlow(flow)
	if err != nil {
		validationErr = err
		return nil, errors.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "graph validation failed")
	}

	// Record errors from result if validation succeeded but found issues
	if len(result.Errors) > 0 {
		validationErr = &ValidationError{Result: result}
	}

	return result, nil
}

// Deploy translates a flow to component configs and writes to semstreams_config KV
// The existing Manager will detect the changes and ComponentManager will create the components
func (e *Engine) Deploy(ctx context.Context, flowID string) error {
	start := time.Now()
	success := false

	defer func() {
		duration := time.Since(start).Seconds()
		e.metrics.recordDeploy(flowID, success, duration)
	}()

	// Get the flow
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errors.WrapTransient(err, "flowengine", "Deploy", "get flow")
	}

	// Validate flow structure
	if err := e.validateFlow(flow); err != nil {
		return errors.WrapInvalid(err, "flowengine", "Deploy", "flow validation failed")
	}

	// Translate nodes to component configs
	componentConfigs, err := e.translateToComponentConfigs(flow)
	if err != nil {
		return errors.WrapInvalid(err, "flowengine", "Deploy", "translation failed")
	}

	// Write component configs to semstreams_config KV
	// Manager is already watching this bucket and will trigger ComponentManager
	if err := e.writeComponentConfigs(ctx, componentConfigs); err != nil {
		return errors.WrapTransient(err, "flowengine", "Deploy", "write configs to KV")
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateDeployedStopped
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errors.WrapTransient(err, "flowengine", "Deploy", "update flow state")
	}

	success = true
	return nil
}

// Start starts all components in a deployed flow
// This is achieved by updating the "enabled" field in component configs
func (e *Engine) Start(ctx context.Context, flowID string) error {
	start := time.Now()
	success := false

	defer func() {
		duration := time.Since(start).Seconds()
		e.metrics.recordStart(flowID, success, duration)
	}()

	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errors.WrapTransient(err, "flowengine", "Start", "get flow")
	}

	if flow.RuntimeState != flowstore.StateDeployedStopped {
		return errors.WrapInvalid(
			fmt.Errorf("flow state is %s", flow.RuntimeState),
			"flowengine", "Start", "flow must be deployed and stopped")
	}

	// Enable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.enableComponent(ctx, node.Name); err != nil {
			return errors.WrapTransient(err, "flowengine", "Start", fmt.Sprintf("enable component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateRunning
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errors.WrapTransient(err, "flowengine", "Start", "update flow state")
	}

	success = true
	return nil
}

// Stop stops all components in a running flow
func (e *Engine) Stop(ctx context.Context, flowID string) error {
	start := time.Now()
	success := false

	defer func() {
		duration := time.Since(start).Seconds()
		e.metrics.recordStop(flowID, success, duration)
	}()

	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errors.WrapTransient(err, "flowengine", "Stop", "get flow")
	}

	if flow.RuntimeState != flowstore.StateRunning {
		return errors.WrapInvalid(
			fmt.Errorf("flow state is %s", flow.RuntimeState),
			"flowengine", "Stop", "flow must be running")
	}

	// Disable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.disableComponent(ctx, node.Name); err != nil {
			return errors.WrapTransient(err, "flowengine", "Stop", fmt.Sprintf("disable component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateDeployedStopped
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errors.WrapTransient(err, "flowengine", "Stop", "update flow state")
	}

	success = true
	return nil
}

// Undeploy removes all component configs for a flow
func (e *Engine) Undeploy(ctx context.Context, flowID string) error {
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errors.WrapTransient(err, "flowengine", "Undeploy", "get flow")
	}

	if flow.RuntimeState == flowstore.StateRunning {
		return errors.WrapInvalid(
			fmt.Errorf("cannot undeploy running flow"),
			"flowengine", "Undeploy", "flow must be stopped before undeploying")
	}

	// Delete all component configs
	for _, node := range flow.Nodes {
		if err := e.deleteComponentConfig(ctx, node.Name); err != nil {
			return errors.WrapTransient(err, "flowengine", "Undeploy", fmt.Sprintf("delete component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateNotDeployed
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errors.WrapTransient(err, "flowengine", "Undeploy", "update flow state")
	}

	return nil
}

// ValidationError wraps validation results for API responses
type ValidationError struct {
	Result *ValidationResult
}

func (e *ValidationError) Error() string {
	if e.Result == nil {
		return "flow validation failed"
	}
	return fmt.Sprintf("flow validation failed: %d errors, %d warnings",
		len(e.Result.Errors), len(e.Result.Warnings))
}

// validateFlow validates the flow structure using FlowGraph analysis
func (e *Engine) validateFlow(flow *flowstore.Flow) error {
	// Layer 1: Basic structural validation
	if err := flow.Validate(); err != nil {
		return err
	}

	// Layer 2: FlowGraph validation
	validator := NewValidator(e.componentRegistry, e.natsClient, e.logger)
	result, err := validator.ValidateFlow(flow)
	if err != nil {
		return errors.WrapInvalid(err, "flowengine", "validateFlow", "graph validation failed")
	}

	// Fail deployment if there are errors
	if len(result.Errors) > 0 {
		return &ValidationError{Result: result}
	}

	// Log warnings but proceed
	if len(result.Warnings) > 0 {
		for _, warning := range result.Warnings {
			e.logger.Warn("Flow validation warning",
				"type", warning.Type,
				"component", warning.ComponentName,
				"message", warning.Message)
		}
	}

	return nil
}

// translateToComponentConfigs converts flow nodes to component configs
func (e *Engine) translateToComponentConfigs(flow *flowstore.Flow) (map[string]types.ComponentConfig, error) {
	configs := make(map[string]types.ComponentConfig)

	for _, node := range flow.Nodes {
		// Marshal node config to JSON
		configJSON, err := json.Marshal(node.Config)
		if err != nil {
			return nil, fmt.Errorf("marshal node %s config: %w", node.ID, err)
		}

		// Map factory name (node.Type) to ComponentType
		compType, err := e.mapFactoryToComponentType(node.Type)
		if err != nil {
			return nil, fmt.Errorf("map component type for %s: %w", node.ID, err)
		}

		configs[node.Name] = types.ComponentConfig{
			Type:    compType,
			Name:    node.Type, // Factory name (e.g., "udp", "graph-processor")
			Enabled: true,      // Deploy as enabled by default
			Config:  configJSON,
		}
	}

	return configs, nil
}

// mapFactoryToComponentType maps a factory name to its ComponentType using the component registry
func (e *Engine) mapFactoryToComponentType(factoryName string) (types.ComponentType, error) {
	// Look up the factory registration
	factories := e.componentRegistry.ListFactories()
	registration, exists := factories[factoryName]
	if !exists {
		return "", fmt.Errorf("unknown factory name: %s", factoryName)
	}

	// Convert registry type string to ComponentType
	switch registration.Type {
	case "input":
		return types.ComponentTypeInput, nil
	case "processor":
		return types.ComponentTypeProcessor, nil
	case "output":
		return types.ComponentTypeOutput, nil
	case "storage":
		return types.ComponentTypeStorage, nil
	default:
		return "", fmt.Errorf("unknown component type in registry: %s", registration.Type)
	}
}

// writeComponentConfigs writes component configs to semstreams_config KV
func (e *Engine) writeComponentConfigs(ctx context.Context, configs map[string]types.ComponentConfig) error {
	// Write each component config directly to KV
	// Manager is already watching this bucket and will pick up the changes
	for name, compConfig := range configs {
		key := fmt.Sprintf("components.%s", name)
		data, err := json.Marshal(compConfig)
		if err != nil {
			return fmt.Errorf("marshal component %s: %w", name, err)
		}

		// Use the KV from Manager to write
		if err := e.writeToKV(ctx, key, data); err != nil {
			return fmt.Errorf("write component %s to KV: %w", name, err)
		}
	}

	return nil
}

// writeToKV writes a key-value pair to the Manager's KV bucket
func (e *Engine) writeToKV(ctx context.Context, key string, value []byte) error {
	// Get the config to access KV operations
	// We'll need to add a method to Manager to expose KV operations
	// For now, update the config and push
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Parse the key to update the right section
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid key format: %s", key)
	}

	section := parts[0]
	name := parts[1]

	switch section {
	case "components":
		if currentConfig.Components == nil {
			currentConfig.Components = make(config.ComponentConfigs)
		}
		var compConfig types.ComponentConfig
		if err := json.Unmarshal(value, &compConfig); err != nil {
			return fmt.Errorf("unmarshal component config: %w", err)
		}
		currentConfig.Components[name] = compConfig
	default:
		return fmt.Errorf("unsupported section: %s", section)
	}

	// Update the config atomically
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Push to KV
	if err := e.configMgr.PushToKV(ctx); err != nil {
		return fmt.Errorf("push to KV: %w", err)
	}

	return nil
}

// enableComponent enables a component in the config
func (e *Engine) enableComponent(ctx context.Context, name string) error {
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Check if component exists
	compConfig, exists := currentConfig.Components[name]
	if !exists {
		return fmt.Errorf("component %s not found", name)
	}

	// Set enabled to true
	compConfig.Enabled = true
	currentConfig.Components[name] = compConfig

	// Update config and push to KV
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	if err := e.configMgr.PushToKV(ctx); err != nil {
		return fmt.Errorf("push to KV: %w", err)
	}

	return nil
}

// disableComponent disables a component in the config
func (e *Engine) disableComponent(ctx context.Context, name string) error {
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Check if component exists
	compConfig, exists := currentConfig.Components[name]
	if !exists {
		return fmt.Errorf("component %s not found", name)
	}

	// Set enabled to false
	compConfig.Enabled = false
	currentConfig.Components[name] = compConfig

	// Update config and push to KV
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	if err := e.configMgr.PushToKV(ctx); err != nil {
		return fmt.Errorf("push to KV: %w", err)
	}

	return nil
}

// deleteComponentConfig removes a component config from KV
func (e *Engine) deleteComponentConfig(ctx context.Context, name string) error {
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Check if component exists
	if _, exists := currentConfig.Components[name]; !exists {
		return fmt.Errorf("component %s not found", name)
	}

	// Delete from config
	delete(currentConfig.Components, name)

	// Update config and push to KV
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	if err := e.configMgr.PushToKV(ctx); err != nil {
		return fmt.Errorf("push to KV: %w", err)
	}

	return nil
}
