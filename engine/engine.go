package flowengine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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

	if err := e.checkOwnership(ctx, flow.ID, componentConfigs); err != nil {
		return errs.WrapInvalid(err, "flowengine", "Deploy", "component ownership conflict")
	}
	if _, err := e.flowStore.UpdateDesiredActivation(
		ctx, flow.ID, flow.Version, flowstore.DesiredDisabled, flowstore.DesiredComponentSet(componentConfigs),
	); err != nil {
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

	enabled := cloneDesiredComponents(flow.DesiredComponents)
	for name, componentConfig := range enabled {
		componentConfig.Enabled = true
		enabled[name] = componentConfig
	}
	if _, err := e.flowStore.UpdateDesiredActivation(ctx, flow.ID, flow.Version, flowstore.DesiredEnabled, enabled); err != nil {
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

	disabled := cloneDesiredComponents(flow.DesiredComponents)
	for name, componentConfig := range disabled {
		componentConfig.Enabled = false
		disabled[name] = componentConfig
	}
	if _, err := e.flowStore.UpdateDesiredActivation(ctx, flow.ID, flow.Version, flowstore.DesiredDisabled, disabled); err != nil {
		return errs.WrapTransient(err, "flowengine", "Stop", "update flow state")
	}

	return nil
}

// Undeploy records an absent desired activation for the next boot.
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

	if _, err := e.flowStore.UpdateDesiredActivation(ctx, flow.ID, flow.Version, flowstore.DesiredAbsent, flowstore.DesiredComponentSet{}); err != nil {
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
		if _, exists := configs[node.Name]; exists {
			return nil, fmt.Errorf("duplicate component instance name %q", node.Name)
		}
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

func (e *Engine) checkOwnership(ctx context.Context, flowID string, candidate map[string]types.ComponentConfig) error {
	owners := make(map[string]string)
	if e.configMgr != nil && e.configMgr.GetConfig() != nil {
		for name := range e.configMgr.GetConfig().Get().Components {
			owners[name] = "static"
		}
	}
	flows, err := e.flowStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list flows: %w", err)
	}
	sort.Slice(flows, func(i, j int) bool { return flows[i].ID < flows[j].ID })
	for _, flow := range flows {
		if flow.ID == flowID || flow.DesiredState == flowstore.DesiredAbsent {
			continue
		}
		for name := range flow.DesiredComponents {
			owners[name] = "flow:" + flow.ID
		}
	}
	names := make([]string, 0, len(candidate))
	for name := range candidate {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if existing, exists := owners[name]; exists {
			return &flowstore.ComponentOwnershipConflictError{
				Component: name, ExistingOwner: existing, RequestedOwner: "flow:" + flowID,
			}
		}
	}
	return nil
}

func cloneDesiredComponents(source flowstore.DesiredComponentSet) flowstore.DesiredComponentSet {
	result := make(flowstore.DesiredComponentSet, len(source))
	for name, componentConfig := range source {
		componentConfig.Config = append(json.RawMessage(nil), componentConfig.Config...)
		result[name] = componentConfig
	}
	return result
}
