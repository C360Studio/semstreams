package flowengine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// Engine translates Flow entities into ComponentConfigs and manages deployment lifecycle
type Engine struct {
	configMgr         *config.Manager
	flowStore         *flowstore.Manager
	componentRegistry *component.Registry
	natsClient        *natsclient.Client
	logger            *slog.Logger
	metrics           *engineMetrics
}

// NewEngine creates a new flow engine
func NewEngine(
	configMgr *config.Manager,
	flowStore *flowstore.Manager,
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
		return nil, errs.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "basic validation failed")
	}

	// Layer 2: FlowGraph validation with port discovery
	validator := NewValidator(e.componentRegistry, e.natsClient, e.logger)
	result, err := validator.ValidateFlow(flow)
	if err != nil {
		validationErr = err
		return nil, errs.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "graph validation failed")
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
		return errs.WrapTransient(err, "flowengine", "Deploy", "get flow")
	}

	// Validate flow structure
	if err := e.validateFlow(flow); err != nil {
		return errs.WrapInvalid(err, "flowengine", "Deploy", "flow validation failed")
	}

	// Translate nodes to component configs
	componentConfigs, err := e.translateToComponentConfigs(flow)
	if err != nil {
		return errs.WrapInvalid(err, "flowengine", "Deploy", "translation failed")
	}

	// Write component configs to semstreams_config KV
	// Manager is already watching this bucket and will trigger ComponentManager
	if err := e.writeComponentConfigs(ctx, componentConfigs); err != nil {
		return errs.WrapTransient(err, "flowengine", "Deploy", "write configs to KV")
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateDeployedStopped
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Deploy", "update flow state")
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
		return errs.WrapTransient(err, "flowengine", "Start", "get flow")
	}

	if flow.RuntimeState != flowstore.StateDeployedStopped {
		return errs.WrapInvalid(
			fmt.Errorf("flow state is %s", flow.RuntimeState),
			"flowengine", "Start", "flow must be deployed and stopped")
	}

	// Enable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.enableComponent(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Start", fmt.Sprintf("enable component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateRunning
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Start", "update flow state")
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
		return errs.WrapTransient(err, "flowengine", "Stop", "get flow")
	}

	if flow.RuntimeState != flowstore.StateRunning {
		return errs.WrapInvalid(
			fmt.Errorf("flow state is %s", flow.RuntimeState),
			"flowengine", "Stop", "flow must be running")
	}

	// Disable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.disableComponent(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Stop", fmt.Sprintf("disable component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateDeployedStopped
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Stop", "update flow state")
	}

	success = true
	return nil
}

// Undeploy removes all component configs for a flow
func (e *Engine) Undeploy(ctx context.Context, flowID string) error {
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errs.WrapTransient(err, "flowengine", "Undeploy", "get flow")
	}

	if flow.RuntimeState == flowstore.StateRunning {
		return errs.WrapInvalid(
			fmt.Errorf("cannot undeploy running flow"),
			"flowengine", "Undeploy", "flow must be stopped before undeploying")
	}

	// Delete all component configs
	for _, node := range flow.Nodes {
		if err := e.deleteComponentConfig(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Undeploy", fmt.Sprintf("delete component %s", node.Name))
		}
	}

	// Update flow state
	flow.RuntimeState = flowstore.StateNotDeployed
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Undeploy", "update flow state")
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
		return errs.WrapInvalid(err, "flowengine", "validateFlow", "graph validation failed")
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

		configs[node.Name] = types.ComponentConfig{
			Type:    node.Type,      // Category (input/processor/output/storage/gateway)
			Name:    node.Component, // Factory name (e.g., "udp", "graph-processor")
			Enabled: true,           // Deploy as enabled by default
			Config:  configJSON,
		}
	}

	return configs, nil
}

// writeComponentConfigs writes component configs to memory and KV atomically
func (e *Engine) writeComponentConfigs(ctx context.Context, configs map[string]types.ComponentConfig) error {
	// Get current config once
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Initialize components map if nil
	if currentConfig.Components == nil {
		currentConfig.Components = make(config.ComponentConfigs)
	}

	// Add all components to memory in a single pass
	for name, compConfig := range configs {
		currentConfig.Components[name] = compConfig
	}

	// Update atomically - this ensures Start() will see all components
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Then push to KV for persistence (single push, not per-component)
	if err := e.configMgr.PushToKV(ctx); err != nil {
		return fmt.Errorf("push to KV: %w", err)
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

	// Idempotent: if already enabled, do NOT re-write an identical config. A
	// redundant PutComponentToKV notifies the ComponentManager, whose per-key
	// handler restarts an already-running component unconditionally — so a
	// no-op enable (e.g. Start after Deploy, which already writes Enabled=true)
	// would spuriously stop-recreate every running component (gh#388).
	if compConfig.Enabled {
		return nil
	}

	// Set enabled to true
	compConfig.Enabled = true
	currentConfig.Components[name] = compConfig

	// Update config in memory
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Push only this component to KV (not all components)
	if err := e.configMgr.PutComponentToKV(ctx, name, compConfig); err != nil {
		return fmt.Errorf("put component to KV: %w", err)
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

	// Idempotent: if already disabled, do NOT re-write an identical config
	// (symmetric with enableComponent — avoids a spurious teardown/reconcile
	// on a no-op disable, gh#388).
	if !compConfig.Enabled {
		return nil
	}

	// Set enabled to false
	compConfig.Enabled = false
	currentConfig.Components[name] = compConfig

	// Update config in memory
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Push only this component to KV (not all components)
	// This avoids race conditions with KV watchers when multiple operations are in flight
	if err := e.configMgr.PutComponentToKV(ctx, name, compConfig); err != nil {
		return fmt.Errorf("put component to KV: %w", err)
	}

	return nil
}

// deleteComponentConfig removes a component config from memory and KV
func (e *Engine) deleteComponentConfig(ctx context.Context, name string) error {
	safeConfig := e.configMgr.GetConfig()
	currentConfig := safeConfig.Get()

	// Check if component exists
	if _, exists := currentConfig.Components[name]; !exists {
		return fmt.Errorf("component %s not found", name)
	}

	// Delete from in-memory config
	delete(currentConfig.Components, name)

	// Update the SafeConfig with the modified config
	if err := safeConfig.Update(currentConfig); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Delete the component key from KV
	if err := e.configMgr.DeleteComponentFromKV(ctx, name); err != nil {
		return fmt.Errorf("delete from KV: %w", err)
	}

	return nil
}
