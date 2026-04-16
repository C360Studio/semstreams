// Package agentic provides the agentic E2E test scenario.
package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// Scenario validates the agentic components (loop, model, tools) work together.
type Scenario struct {
	name        string
	description string

	// Client URLs (clients created during Setup)
	natsURL string

	// Clients (created during Setup)
	nats    *client.NATSValidationClient
	metrics *client.MetricsClient
	obs     *client.ObservabilityClient

	// useMock indicates Docker compose provides mock-llm
	useMock bool

	// Configuration
	config *Config

	// AGNTCY integration configuration
	agntcyConfig *AGNTCYConfig
}

// Config holds configuration for the agentic scenario.
type Config struct {
	// NATS URL for publishing tasks
	NATSURL string `json:"nats_url"`

	// Metrics URL for checking completion
	MetricsURL string `json:"metrics_url"`

	// LLM endpoint URL (default: start mock server)
	LLMEndpointURL string `json:"llm_endpoint_url"`

	// Timeouts
	TaskTimeout     time.Duration `json:"task_timeout"`
	CompleteTimeout time.Duration `json:"complete_timeout"`

	// Expected results
	MinTrajectorySteps int `json:"min_trajectory_steps"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:            "nats://localhost:34222",
		MetricsURL:         "http://localhost:39090",
		LLMEndpointURL:     "", // Empty means use mock
		TaskTimeout:        30 * time.Second,
		CompleteTimeout:    60 * time.Second,
		MinTrajectorySteps: 1,
	}
}

// NewScenario creates a new agentic scenario.
func NewScenario(
	obs *client.ObservabilityClient,
	config *Config,
) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}

	// Check for environment override
	if envURL := os.Getenv("AGENTIC_LLM_URL"); envURL != "" {
		config.LLMEndpointURL = envURL
	}

	return &Scenario{
		name:        "agentic",
		description: "Validates agentic components (loop, model, tools) end-to-end",
		natsURL:     config.NATSURL,
		obs:         obs,
		config:      config,
		useMock:     config.LLMEndpointURL == "",
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *Scenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *Scenario) Setup(ctx context.Context) error {
	// Create NATS client
	natsClient, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		return fmt.Errorf("failed to create NATS client: %w", err)
	}
	s.nats = natsClient

	// Create metrics client
	s.metrics = client.NewMetricsClient(s.config.MetricsURL)

	// Docker compose provides mock-llm at http://mock-llm:8080 (within Docker network)
	// and http://localhost:38180 (from host). The semstreams container uses the Docker-internal
	// URL, so we don't need to start a mock server here.
	if s.useMock {
		s.config.LLMEndpointURL = "http://localhost:38180" // For reference in results
	}

	return nil
}

// Execute runs the agentic scenario.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	// Store mock info
	if s.useMock {
		result.Details["llm_endpoint"] = "mock (built-in)"
		result.Details["mock_url"] = s.config.LLMEndpointURL
	} else {
		result.Details["llm_endpoint"] = s.config.LLMEndpointURL
	}

	// Execute stages
	stages := []struct {
		name string
		fn   func(context.Context, *scenarios.Result) error
	}{
		{"verify-components", s.verifyComponents},
		{"capture-baseline", s.captureBaseline},
		{"inject-task", s.injectTask},
		{"wait-for-completion", s.waitForCompletion},
		{"validate-trajectory", s.validateTrajectory},
		{"verify-graph-triples", s.verifyGraphTriples},
		{"verify-tool-execution", s.verifyToolExecution},
		{"verify-streaming-metrics", s.verifyStreamingMetrics},
		{"validate-results", s.validateResults},
		// AGNTCY integration stages (optional, skip if not configured)
		{"verify-oasf-generation", s.verifyOASFGeneration},
		{"verify-directory-bridge", s.verifyDirectoryBridge},
		{"verify-a2a-adapter", s.verifyA2AAdapter},
		{"verify-otel-export", s.verifyOTELExport},
	}

	for _, stage := range stages {
		stageStart := time.Now()

		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}

		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Teardown cleans up after the scenario.
func (s *Scenario) Teardown(ctx context.Context) error {
	// Clean up AGNTCY test resources
	_ = s.cleanupAGNTCY(ctx)

	return nil
}

// verifyComponents checks that agentic components are healthy.
func (s *Scenario) verifyComponents(ctx context.Context, result *scenarios.Result) error {
	components, err := s.obs.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get components: %w", err)
	}

	// Check for required agentic components
	required := []string{"agentic-loop", "agentic-model"}
	found := make(map[string]bool)

	for _, comp := range components {
		found[comp.Name] = comp.Enabled && comp.Healthy
	}

	missing := []string{}
	unhealthy := []string{}

	for _, req := range required {
		healthy, exists := found[req]
		if !exists {
			missing = append(missing, req)
		} else if !healthy {
			unhealthy = append(unhealthy, req)
		}
	}

	result.Details["agentic_components"] = found

	if len(missing) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Missing agentic components: %v (may not be configured)", missing))
	}

	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}

	return nil
}

// captureBaseline captures metrics baseline before task injection.
func (s *Scenario) captureBaseline(ctx context.Context, result *scenarios.Result) error {
	snapshot, err := s.metrics.FetchSnapshot(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not capture metrics baseline: %v", err))
		return nil // Non-fatal
	}

	result.Details["baseline_snapshot"] = snapshot
	return nil
}

// injectTask publishes a direct agent task for testing
func (s *Scenario) injectTask(ctx context.Context, result *scenarios.Result) error {
	// Inject a direct task to test agentic loop
	task := agentic.TaskMessage{
		LoopID: fmt.Sprintf("e2e-loop-%d", time.Now().UnixNano()),
		TaskID: fmt.Sprintf("e2e-agentic-%d", time.Now().UnixNano()),
		Role:   "general",
		Model:  "mock",
		Prompt: "Analyze the temperature sensor temp-sensor-001. Respond with a brief assessment including valid JSON in your response.",
	}

	taskMsg := message.NewBaseMessage(task.Schema(), &task, "e2e-test")
	taskData, err := json.Marshal(taskMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	result.Details["task_id"] = task.TaskID
	result.Details["loop_id"] = task.LoopID
	result.Details["task_subject"] = "agent.task.e2e"

	if err := s.nats.Publish(ctx, "agent.task.e2e", taskData); err != nil {
		return fmt.Errorf("failed to publish task: %w", err)
	}

	return nil
}

// waitForCompletion waits for agent loop completion
func (s *Scenario) waitForCompletion(ctx context.Context, result *scenarios.Result) error {
	timeout := s.config.CompleteTimeout
	deadline := time.Now().Add(timeout)

	var loopsCompleted float64

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Check agent loop completion via metrics
			loops, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_loop_loops_completed_total")
			if err == nil && loops > loopsCompleted {
				loopsCompleted = loops
				result.Metrics["loops_completed"] = loopsCompleted
			}

			// Success: at least one agent loop completed
			if loopsCompleted >= 1 {
				result.Details["completion_method"] = "metrics"
				return nil
			}
		}
	}

	// Timeout - provide diagnostic info
	result.Details["timeout_loops_completed"] = loopsCompleted

	return fmt.Errorf("timeout waiting for agent loop completion after %v (loops_completed=%v)", timeout, loopsCompleted)
}

// validateTrajectory retrieves and validates the trajectory via NATS query handler.
func (s *Scenario) validateTrajectory(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("loop_id not found in result details")
	}

	traj, err := s.nats.GetTrajectory(ctx, loopID)
	if err != nil {
		return fmt.Errorf("failed to get trajectory for loop %s: %w", loopID, err)
	}

	// Validate minimum steps
	if len(traj.Steps) < s.config.MinTrajectorySteps {
		return fmt.Errorf("trajectory has %d steps, expected at least %d", len(traj.Steps), s.config.MinTrajectorySteps)
	}

	// Validate at least one model_call step exists
	hasModelCall := false
	hasToolCall := false
	for _, step := range traj.Steps {
		switch step.StepType {
		case "model_call":
			hasModelCall = true
		case "tool_call":
			hasToolCall = true
		}
	}

	if !hasModelCall {
		return fmt.Errorf("trajectory has no model_call steps")
	}
	if !hasToolCall {
		result.Warnings = append(result.Warnings, "trajectory has no tool_call steps")
	}

	// Validate completion
	if traj.Outcome != "complete" {
		return fmt.Errorf("trajectory outcome is %q, expected \"complete\"", traj.Outcome)
	}
	if traj.EndTime == nil {
		return fmt.Errorf("trajectory end_time is nil")
	}
	if traj.Duration < 0 {
		return fmt.Errorf("trajectory duration is %d, expected >= 0", traj.Duration)
	}

	// Store metrics
	result.Metrics["trajectory_steps"] = len(traj.Steps)
	result.Metrics["trajectory_tokens_in"] = traj.TotalTokensIn
	result.Metrics["trajectory_tokens_out"] = traj.TotalTokensOut
	result.Metrics["trajectory_duration_ms"] = traj.Duration
	result.Details["trajectory_outcome"] = traj.Outcome

	return nil
}

// verifyGraphTriples verifies that the graph writer emitted triples for the loop
// execution and model endpoint entities. This validates the full path:
// agentic-loop → graph.mutation.triple.add → graph-ingest → ENTITY_STATES KV.
func (s *Scenario) verifyGraphTriples(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("loop_id not found in result details")
	}

	// The agentic config uses platform.org="c360", platform.instance_id="agentic-001".
	// instance_id takes precedence over id in extractPlatformMeta.
	const org = "c360"
	const platform = "agentic-001"

	// --- Verify loop execution entity ---
	// Graph writes happen after the completion metric is incremented, so the entity
	// may not be in ENTITY_STATES yet. Poll briefly to allow graph-ingest to process.
	loopEntityID := agentic.LoopExecutionEntityID(org, platform, loopID)

	var loopEntity *client.EntityState
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		loopEntity, err = s.nats.GetEntity(ctx, loopEntityID)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if loopEntity == nil {
		return fmt.Errorf("loop entity %s not found in graph after 10s", loopEntityID)
	}

	// Build predicate set from triples.
	loopPreds := make(map[string]bool, len(loopEntity.Triples))
	for _, t := range loopEntity.Triples {
		loopPreds[t.Predicate] = true
	}

	requiredLoopPreds := []string{
		agvocab.LoopOutcome,
		agvocab.LoopRole,
		agvocab.LoopIterations,
		agvocab.LoopTokensIn,
		agvocab.LoopTokensOut,
		agvocab.LoopTask,
		agvocab.LoopEndedAt,
	}

	missing := []string{}
	for _, pred := range requiredLoopPreds {
		if !loopPreds[pred] {
			missing = append(missing, pred)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("loop entity %s missing predicates: %v", loopEntityID, missing)
	}

	result.Metrics["graph_loop_triples"] = len(loopEntity.Triples)
	result.Details["graph_loop_entity_id"] = loopEntityID

	// --- Verify model endpoint entity ---
	// The injected task uses model "mock", which is configured in the agentic config.
	modelEntityID := agentic.ModelEndpointEntityID(org, platform, "mock")
	modelEntity, err := s.nats.GetEntity(ctx, modelEntityID)
	if err != nil {
		// Model endpoint triples are written at startup; if graph-ingest wasn't ready
		// yet they may be missing. Warn rather than fail.
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("model endpoint entity %s not found: %v", modelEntityID, err))
	} else {
		modelPreds := make(map[string]bool, len(modelEntity.Triples))
		for _, t := range modelEntity.Triples {
			modelPreds[t.Predicate] = true
		}

		requiredModelPreds := []string{
			agvocab.ModelProvider,
			agvocab.ModelName,
			agvocab.ModelSupportsTools,
		}
		modelMissing := []string{}
		for _, pred := range requiredModelPreds {
			if !modelPreds[pred] {
				modelMissing = append(modelMissing, pred)
			}
		}
		if len(modelMissing) > 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("model entity %s missing predicates: %v", modelEntityID, modelMissing))
		}

		result.Metrics["graph_model_triples"] = len(modelEntity.Triples)
		result.Details["graph_model_entity_id"] = modelEntityID
	}

	// --- Verify loop→model relationship ---
	if loopPreds[agvocab.LoopModelUsed] {
		result.Details["graph_loop_model_linked"] = true
	} else {
		result.Warnings = append(result.Warnings, "loop entity missing LoopModelUsed relationship triple")
	}

	return nil
}

// verifyToolExecution verifies that tools were executed during the agent loop.
// This is a critical verification that tool definitions are being injected into
// AgentRequest messages. The mock LLM only returns tool calls when it receives
// tool definitions, so if this fails, it indicates the tool injection path is broken.
func (s *Scenario) verifyToolExecution(ctx context.Context, result *scenarios.Result) error {
	// Check tool execution metrics
	toolExecutions, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_tools_executions_total")
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify tool executions: %v", err))
		return nil // Non-fatal - metrics may not be available
	}

	result.Metrics["tool_executions"] = toolExecutions

	// Verify at least one tool was executed
	// This is now a HARD FAILURE because the mock LLM only calls tools when
	// tool definitions are present in the request. If no tools were executed,
	// it means AgentRequest.Tools was empty (tool injection failed).
	if toolExecutions < 1 {
		return fmt.Errorf("no tool executions recorded - tool definitions may not be injected into AgentRequest")
	}

	result.Details["tool_execution_verified"] = true
	return nil
}

// verifyStreamingMetrics checks that the streaming path was exercised.
// This is non-fatal — the core agentic flow is validated by earlier stages.
func (s *Scenario) verifyStreamingMetrics(ctx context.Context, result *scenarios.Result) error {
	// Check streaming chunks counter
	chunks, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_model_stream_chunks_total")
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify streaming chunks: %v", err))
		return nil
	}
	result.Metrics["stream_chunks_total"] = chunks

	if chunks < 1 {
		result.Warnings = append(result.Warnings, "No streaming chunks recorded — streaming path may not have been exercised")
		return nil
	}

	// Check time-to-first-token histogram
	ttft, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_model_stream_ttft_seconds_count")
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify TTFT metric: %v", err))
	} else {
		result.Metrics["stream_ttft_count"] = ttft
	}

	result.Details["streaming_verified"] = true
	return nil
}

// validateResults validates the scenario results
func (s *Scenario) validateResults(_ context.Context, result *scenarios.Result) error {
	// Validate agent loops completed
	loopsCompleted, ok := result.Metrics["loops_completed"].(float64)
	if !ok || loopsCompleted < 1 {
		return fmt.Errorf("expected at least 1 agent loop completion, got %v", loopsCompleted)
	}

	result.Details["validation_passed"] = true
	return nil
}
