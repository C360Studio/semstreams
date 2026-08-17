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

	engine := &Engine{
		configMgr:         configMgr,
		flowStore:         flowStore,
		componentRegistry: componentRegistry,
		natsClient:        natsClient,
		logger:            logger,
		metrics:           metrics,
	}
	flowStore.SealBootActivation(configMgr)
	return engine
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

// Deploy persists a validated disabled desired component set for the next boot.
func (e *Engine) Deploy(ctx context.Context, flowID string) error {
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
	for name, componentConfig := range componentConfigs {
		componentConfig.Enabled = false
		componentConfigs[name] = componentConfig
	}

	// Write the desired component configs for selection at the next boot.
	if err := e.writeComponentConfigs(ctx, componentConfigs); err != nil {
		return errs.WrapTransient(err, "flowengine", "Deploy", "write configs to KV")
	}

	// Record desired activation only. The running process is sealed.
	flow.DesiredState = flowstore.DesiredDisabled
	now := time.Now()
	flow.DesiredChangedAt = &now
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Deploy", "update flow state")
	}

	return nil
}

// Start enables the desired component set for the next successful boot.
func (e *Engine) Start(ctx context.Context, flowID string) error {
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errs.WrapTransient(err, "flowengine", "Start", "get flow")
	}

	if flow.DesiredState != flowstore.DesiredDisabled {
		return errs.WrapInvalid(
			fmt.Errorf("flow desired state is %s", flow.DesiredState),
			"flowengine", "Start", "flow desired state must be disabled")
	}

	// Enable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.enableComponent(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Start", fmt.Sprintf("enable component %s", node.Name))
		}
	}

	flow.DesiredState = flowstore.DesiredEnabled
	now := time.Now()
	flow.DesiredChangedAt = &now
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Start", "update flow state")
	}

	return nil
}

// Stop disables the desired component set for the next successful boot.
func (e *Engine) Stop(ctx context.Context, flowID string) error {
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errs.WrapTransient(err, "flowengine", "Stop", "get flow")
	}

	if flow.DesiredState != flowstore.DesiredEnabled {
		return errs.WrapInvalid(
			fmt.Errorf("flow desired state is %s", flow.DesiredState),
			"flowengine", "Stop", "flow desired state must be enabled")
	}

	// Disable all components in the flow
	for _, node := range flow.Nodes {
		if err := e.disableComponent(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Stop", fmt.Sprintf("disable component %s", node.Name))
		}
	}

	flow.DesiredState = flowstore.DesiredDisabled
	now := time.Now()
	flow.DesiredChangedAt = &now
	if err := e.flowStore.Update(ctx, flow); err != nil {
		return errs.WrapTransient(err, "flowengine", "Stop", "update flow state")
	}

	return nil
}

// Undeploy removes all component configs for a flow
func (e *Engine) Undeploy(ctx context.Context, flowID string) error {
	flow, err := e.flowStore.Get(ctx, flowID)
	if err != nil {
		return errs.WrapTransient(err, "flowengine", "Undeploy", "get flow")
	}

	if flow.DesiredState == flowstore.DesiredEnabled {
		return errs.WrapInvalid(
			fmt.Errorf("cannot remove enabled desired flow"),
			"flowengine", "Undeploy", "flow desired state must be disabled before removal")
	}

	// Delete all component configs
	for _, node := range flow.Nodes {
		if err := e.deleteComponentConfig(ctx, node.Name); err != nil {
			return errs.WrapTransient(err, "flowengine", "Undeploy", fmt.Sprintf("delete component %s", node.Name))
		}
	}

	flow.DesiredState = flowstore.DesiredAbsent
	now := time.Now()
	flow.DesiredChangedAt = &now
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
			Enabled: false,          // Deploy records disabled desired state
			Config:  configJSON,
		}
	}

	return configs, nil
}

// writeComponentConfigs writes component configs to memory and KV atomically
func (e *Engine) writeComponentConfigs(ctx context.Context, configs map[string]types.ComponentConfig) error {
	// Serialized read-modify-write so a concurrent config mutation cannot drop
	// these components (gh#515). Start() will see all of them after the swap.
	if err := e.configMgr.GetConfig().Mutate(func(currentConfig *config.Config) error {
		if currentConfig.Components == nil {
			currentConfig.Components = make(config.ComponentConfigs)
		}
		for name, compConfig := range configs {
			currentConfig.Components[name] = compConfig
		}
		return nil
	}); err != nil {
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
	// Parse the key to update the right section
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid key format: %s", key)
	}

	section := parts[0]
	name := parts[1]

	if section != "components" {
		return fmt.Errorf("unsupported section: %s", section)
	}
	var compConfig types.ComponentConfig
	if err := json.Unmarshal(value, &compConfig); err != nil {
		return fmt.Errorf("unmarshal component config: %w", err)
	}

	// Serialized read-modify-write (gh#515).
	if err := e.configMgr.GetConfig().Mutate(func(currentConfig *config.Config) error {
		if currentConfig.Components == nil {
			currentConfig.Components = make(config.ComponentConfigs)
		}
		currentConfig.Components[name] = compConfig
		return nil
	}); err != nil {
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
	// Read-decide-mutate under the config lock (gh#515): the idempotent-enable
	// check now runs on the authoritative current state, not a clone that may be
	// stale by the swap. skip carries the no-op decision out; toPut carries the
	// enabled config to the post-mutation KV write.
	var toPut types.ComponentConfig
	skip := false
	if err := e.configMgr.GetConfig().Mutate(func(currentConfig *config.Config) error {
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
			skip = true
			return nil
		}
		compConfig.Enabled = true
		currentConfig.Components[name] = compConfig
		toPut = compConfig
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	if skip {
		return nil
	}

	// Push only this component to KV (not all components)
	if err := e.configMgr.PutComponentToKV(ctx, name, toPut); err != nil {
		return fmt.Errorf("put component to KV: %w", err)
	}

	return nil
}

// disableComponent disables a component in the config
func (e *Engine) disableComponent(ctx context.Context, name string) error {
	// Read-decide-mutate under the config lock (gh#515), symmetric with enableComponent.
	var toPut types.ComponentConfig
	skip := false
	if err := e.configMgr.GetConfig().Mutate(func(currentConfig *config.Config) error {
		compConfig, exists := currentConfig.Components[name]
		if !exists {
			return fmt.Errorf("component %s not found", name)
		}
		// Idempotent: if already disabled, do NOT re-write an identical config
		// (symmetric with enableComponent — avoids a spurious teardown/reconcile
		// on a no-op disable, gh#388).
		if !compConfig.Enabled {
			skip = true
			return nil
		}
		compConfig.Enabled = false
		currentConfig.Components[name] = compConfig
		toPut = compConfig
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	if skip {
		return nil
	}

	// Push only this component to KV (not all components)
	// This avoids race conditions with KV watchers when multiple operations are in flight
	if err := e.configMgr.PutComponentToKV(ctx, name, toPut); err != nil {
		return fmt.Errorf("put component to KV: %w", err)
	}

	return nil
}

// deleteComponentConfig removes a component config from memory and KV
func (e *Engine) deleteComponentConfig(ctx context.Context, name string) error {
	// Serialized read-modify-write (gh#515): the existence check and the delete
	// run atomically against the current state.
	if err := e.configMgr.GetConfig().Mutate(func(currentConfig *config.Config) error {
		if _, exists := currentConfig.Components[name]; !exists {
			return fmt.Errorf("component %s not found", name)
		}
		delete(currentConfig.Components, name)
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Delete the component key from KV
	if err := e.configMgr.DeleteComponentFromKV(ctx, name); err != nil {
		return fmt.Errorf("delete from KV: %w", err)
	}

	return nil
}
