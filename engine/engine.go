package flowengine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// Engine validates saved flow diagrams and compiles them into component
// configuration candidates. It never owns component or service lifecycle.
type Engine struct {
	componentRegistry *component.Registry
	natsClient        *natsclient.Client
	logger            *slog.Logger
	metrics           *engineMetrics
}

// NewEngine creates a flow diagram validator/compiler.
func NewEngine(
	componentRegistry *component.Registry,
	natsClient *natsclient.Client,
	logger *slog.Logger,
	metricsRegistry *metric.MetricsRegistry,
) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	metrics, err := newEngineMetrics(metricsRegistry)
	if err != nil {
		logger.Error("Failed to initialize flow engine metrics", "error", err)
	}
	return &Engine{
		componentRegistry: componentRegistry,
		natsClient:        natsClient,
		logger:            logger,
		metrics:           metrics,
	}
}

// ValidateFlowDefinition validates a saved or draft flow diagram and returns
// port information plus discovered connections. Validation findings are
// returned in the result; infrastructure failures are returned as errors.
func (e *Engine) ValidateFlowDefinition(flow *flowstore.Flow) (*ValidationResult, error) {
	start := time.Now()
	flowID := ""
	if flow != nil {
		flowID = flow.ID
	}
	var validationErr error
	defer func() {
		e.metrics.recordValidation(flowID, time.Since(start).Seconds(), validationErr)
	}()

	if flow == nil {
		validationErr = fmt.Errorf("flow cannot be nil")
		return nil, errs.WrapInvalid(validationErr, "flowengine", "ValidateFlowDefinition", "basic validation failed")
	}
	if err := flow.Validate(); err != nil {
		validationErr = err
		return nil, errs.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "basic validation failed")
	}

	validator := NewValidator(e.componentRegistry, e.natsClient, e.logger)
	result, err := validator.ValidateFlow(flow)
	if err != nil {
		validationErr = err
		return nil, errs.WrapInvalid(err, "flowengine", "ValidateFlowDefinition", "graph validation failed")
	}
	if len(result.Errors) > 0 {
		validationErr = &ValidationError{Result: result}
	}
	return result, nil
}

// Compile validates a flow diagram and translates each node into one enabled
// component configuration candidate. The returned map is detached from the
// flow and has no effect until an explicit publisher persists its entries.
func (e *Engine) Compile(flow *flowstore.Flow) (config.ComponentConfigs, *ValidationResult, error) {
	result, err := e.ValidateFlowDefinition(flow)
	if err != nil {
		return nil, result, err
	}
	if len(result.Errors) > 0 {
		return nil, result, &ValidationError{Result: result}
	}

	configs := make(config.ComponentConfigs, len(flow.Nodes))
	for _, node := range flow.Nodes {
		if _, exists := configs[node.Name]; exists {
			return nil, result, fmt.Errorf("duplicate component instance name %q", node.Name)
		}
		configJSON, err := json.Marshal(node.Config)
		if err != nil {
			return nil, result, fmt.Errorf("marshal node %s config: %w", node.ID, err)
		}
		configs[node.Name] = types.ComponentConfig{
			Type:    node.Type,
			Name:    node.Component,
			Enabled: true,
			Config:  configJSON,
		}
	}
	return configs, result, nil
}

// ValidationError wraps validation findings for API responses.
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
