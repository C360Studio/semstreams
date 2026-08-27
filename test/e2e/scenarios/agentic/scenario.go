// Package agentic provides the agentic E2E test scenario.
package agentic

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
	MinTrajectoryFacts int `json:"min_trajectory_facts"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:            "nats://localhost:34222",
		MetricsURL:         "http://localhost:39090",
		LLMEndpointURL:     "", // Empty means use mock
		TaskTimeout:        30 * time.Second,
		CompleteTimeout:    60 * time.Second,
		MinTrajectoryFacts: 1,
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
		{"verify-terminal-response", s.verifyTerminalResponse},
		{"validate-trajectory", s.validateTrajectory},
		{"verify-graph-triples", s.verifyGraphTriples},
		{"verify-tool-execution", s.verifyToolExecution},
		{"verify-durable-tool-replay", s.verifyDurableToolReplay},
		{"verify-streaming-metrics", s.verifyStreamingMetrics},
		{"verify-tool-call-governance", s.verifyToolCallGovernance},
		{"validate-results", s.validateResults},
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

// verifyDurableToolReplay injects one test-side TOOL stream admission fault
// after a completed outcome is durable. There is no production fault knob: the
// harness pauses the shipped consumer, stores the request, temporarily makes
// the already-full stream reject new messages, resumes, observes the actual
// result publication failure, and restores the stream for redelivery.
func (s *Scenario) verifyDurableToolReplay(ctx context.Context, result *scenarios.Result) error {
	const (
		toolStream   = "TOOL"
		toolConsumer = "agentic-tools-tool-execute-all"
	)
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream for durable replay proof: %w", err)
	}
	stream, err := js.Stream(ctx, toolStream)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", toolStream, err)
	}

	executionsBefore, err := s.metricWithLabels(ctx, "semstreams_agentic_tools_executions_total", map[string]string{
		"tool_name": "query_entity",
	})
	if err != nil {
		return fmt.Errorf("read execution baseline: %w", err)
	}
	retriesBefore, err := s.metricWithLabels(ctx, "semstreams_agentic_tools_result_publish_failures_total", map[string]string{
		"reason": "transport",
	})
	if err != nil {
		return fmt.Errorf("read publish-retry baseline: %w", err)
	}

	if _, err := stream.PauseConsumer(ctx, toolConsumer, time.Now().Add(2*time.Minute)); err != nil {
		return fmt.Errorf("pause %s consumer: %w", toolConsumer, err)
	}
	originalInfo, err := stream.Info(ctx)
	if err != nil {
		_, _ = stream.ResumeConsumer(context.Background(), toolConsumer)
		return fmt.Errorf("read %s stream config: %w", toolStream, err)
	}
	originalConfig := originalInfo.Config
	restored := false
	defer func() {
		if !restored {
			_, _ = js.UpdateStream(context.Background(), originalConfig)
		}
		_, _ = stream.ResumeConsumer(context.Background(), toolConsumer)
	}()

	call := agentic.ToolCall{
		ID:      fmt.Sprintf("e2e-durable-replay-%d", time.Now().UnixNano()),
		Name:    "query_entity",
		LoopID:  fmt.Sprintf("e2e-durable-loop-%d", time.Now().UnixNano()),
		TraceID: fmt.Sprintf("e2e-durable-trace-%d", time.Now().UnixNano()),
		Arguments: map[string]any{
			"entity_id": "c360.agentic.sensor.temperature.temp-sensor-001",
		},
	}
	request := message.NewBaseMessage(call.Schema(), &call, "e2e-durable-replay")
	wire, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal durable replay tool call: %w", err)
	}
	if err := s.nats.Publish(ctx, "tool.execute."+call.ID, wire); err != nil {
		return fmt.Errorf("publish paused durable replay tool call: %w", err)
	}

	fullInfo, err := stream.Info(ctx)
	if err != nil {
		return fmt.Errorf("read %s state after request: %w", toolStream, err)
	}
	if fullInfo.State.Msgs == 0 {
		return fmt.Errorf("%s has no stored request to hold while faulting result publication", toolStream)
	}
	faultConfig := fullInfo.Config
	faultConfig.Discard = jetstream.DiscardNew
	faultConfig.DiscardNewPerSubject = false
	faultConfig.MaxMsgs = int64(fullInfo.State.Msgs)
	if _, err := js.UpdateStream(ctx, faultConfig); err != nil {
		return fmt.Errorf("install test-only result publication fault: %w", err)
	}
	if _, err := stream.ResumeConsumer(ctx, toolConsumer); err != nil {
		return fmt.Errorf("resume %s into result publication fault: %w", toolConsumer, err)
	}

	if err := s.waitMetricWithLabels(ctx, "semstreams_agentic_tools_result_publish_failures_total",
		map[string]string{"reason": "transport"}, retriesBefore+1, 15*time.Second); err != nil {
		return fmt.Errorf("result publication failure was not observed: %w", err)
	}
	if _, err := js.UpdateStream(ctx, originalConfig); err != nil {
		return fmt.Errorf("restore %s stream after fault: %w", toolStream, err)
	}
	restored = true

	wantMsgID, executionDelta, err := s.verifyReplayedToolResult(ctx, stream, call, executionsBefore)
	if err != nil {
		return err
	}
	result.Details["durable_tool_replay_call_id"] = call.ID
	result.Details["durable_tool_replay_msg_id"] = wantMsgID
	result.Metrics["durable_tool_replay_executor_invocations"] = executionDelta
	return nil
}

func (s *Scenario) verifyReplayedToolResult(
	ctx context.Context, stream jetstream.Stream, call agentic.ToolCall, executionsBefore float64,
) (string, float64, error) {
	resultSubject := "tool.result." + call.ID
	deadline := time.Now().Add(45 * time.Second)
	var stored *jetstream.RawStreamMsg
	for time.Now().Before(deadline) {
		var err error
		stored, err = stream.GetLastMsgForSubject(ctx, resultSubject)
		if err == nil {
			break
		}
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			return "", 0, fmt.Errorf("read replayed result: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if stored == nil {
		return "", 0, fmt.Errorf("stored result did not replay within 45s")
	}
	wantMsgID := "tool-result/v1/" + durableCallDigest(call.ID)
	if got := stored.Header.Get(nats.MsgIdHdr); got != wantMsgID {
		return "", 0, fmt.Errorf("replayed result Nats-Msg-Id = %q, want %q", got, wantMsgID)
	}
	var resultEnvelope struct {
		Payload agentic.ToolResult `json:"payload"`
	}
	if err := json.Unmarshal(stored.Data, &resultEnvelope); err != nil {
		return "", 0, fmt.Errorf("decode replayed ToolResult: %w", err)
	}
	replayed := resultEnvelope.Payload
	if replayed.CallID != call.ID || replayed.Name != call.Name || replayed.LoopID != call.LoopID || replayed.TraceID != call.TraceID {
		return "", 0, fmt.Errorf("replayed ToolResult correlation = call:%q name:%q loop:%q trace:%q, want call:%q name:%q loop:%q trace:%q",
			replayed.CallID, replayed.Name, replayed.LoopID, replayed.TraceID,
			call.ID, call.Name, call.LoopID, call.TraceID)
	}
	if replayed.Content == "" && replayed.Error == "" {
		return "", 0, fmt.Errorf("replayed ToolResult has neither terminal content nor error")
	}
	executionsAfter, err := s.metricWithLabels(ctx, "semstreams_agentic_tools_executions_total", map[string]string{
		"tool_name": "query_entity",
	})
	if err != nil {
		return "", 0, fmt.Errorf("read execution result: %w", err)
	}
	executionDelta := executionsAfter - executionsBefore
	if executionDelta != 1 {
		return "", 0, fmt.Errorf("durable replay executor invocation delta = %.0f, want exactly 1", executionDelta)
	}
	return wantMsgID, executionDelta, nil
}

func durableCallDigest(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}

func (s *Scenario) metricWithLabels(ctx context.Context, name string, labels map[string]string) (float64, error) {
	metrics, err := s.metrics.GetMetricByLabels(ctx, name, labels)
	if err != nil {
		return 0, err
	}
	if len(metrics) == 0 {
		return 0, nil
	}
	var total float64
	for _, metric := range metrics {
		total += metric.Value
	}
	return total, nil
}

func (s *Scenario) waitMetricWithLabels(
	ctx context.Context, name string, labels map[string]string, want float64, timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, err := s.metricWithLabels(ctx, name, labels)
		if err == nil && got >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("metric %s%v did not reach %.0f", name, labels, want)
}

// Teardown cleans up after the scenario.
func (s *Scenario) Teardown(ctx context.Context) error {
	_ = ctx
	return nil
}

// verifyComponents checks that agentic components are healthy.
func (s *Scenario) verifyComponents(ctx context.Context, result *scenarios.Result) error {
	components, err := s.obs.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get components: %w", err)
	}

	// Check for required agentic components
	required := requiredComponents()
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
		return fmt.Errorf("missing required components: %v", missing)
	}

	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}

	return nil
}

func requiredComponents() []string {
	return []string{"agentic-dispatch", "agentic-loop", "agentic-model", "rule"}
}

// captureBaseline captures metrics baseline before task injection.
func (s *Scenario) captureBaseline(ctx context.Context, result *scenarios.Result) error {
	snapshot, err := s.metrics.FetchSnapshot(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not capture metrics baseline: %v", err))
		return nil // Non-fatal
	}

	result.Details["baseline_snapshot"] = snapshot
	result.Details["baseline_loops_completed"] = sumSnapshotMetric(
		snapshot,
		"semstreams_agentic_loop_loops_completed_total",
	)
	return nil
}

// injectTask publishes a direct agent task for testing
func (s *Scenario) injectTask(ctx context.Context, result *scenarios.Result) error {
	// Inject a direct task to test agentic loop
	task := newTestTask(time.Now())

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

func newTestTask(now time.Time) agentic.TaskMessage {
	taskID := fmt.Sprintf("e2e-agentic-%d", now.UnixNano())
	return agentic.TaskMessage{
		LoopID:      fmt.Sprintf("e2e-loop-%d", now.UnixNano()),
		TaskID:      taskID,
		Role:        "general",
		Model:       "mock",
		Prompt:      "Analyze the temperature sensor temp-sensor-001. Respond with a brief assessment including valid JSON in your response.",
		ChannelType: "e2e",
		ChannelID:   taskID,
		Tools: []agentic.ToolDefinition{{
			Name:        "query_entity",
			Description: "Query the test temperature sensor by its entity ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{"type": "string"},
				},
				"required": []string{"entity_id"},
			},
		}},
		ToolChoice: &agentic.ToolChoice{Mode: "function", FunctionName: "query_entity"},
	}
}

func (s *Scenario) verifyTerminalResponse(ctx context.Context, result *scenarios.Result) error {
	loopID, _ := result.Details["loop_id"].(string)
	taskID, _ := result.Details["task_id"].(string)
	if loopID == "" || taskID == "" {
		return fmt.Errorf("terminal response proof requires loop_id and task_id")
	}
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream for terminal response proof: %w", err)
	}
	agentStream, err := js.Stream(ctx, "AGENT")
	if err != nil {
		return fmt.Errorf("open AGENT stream: %w", err)
	}
	terminal, err := agentStream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
	if err != nil {
		return fmt.Errorf("read source terminal: %w", err)
	}
	var source struct {
		ID      string `json:"id"`
		Payload struct {
			Outcome string `json:"outcome"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(terminal.Data, &source); err != nil {
		return fmt.Errorf("decode source terminal: %w", err)
	}
	if source.ID == "" || source.Payload.Outcome != agentic.OutcomeSuccess {
		return fmt.Errorf("source terminal id/outcome = %q/%q, want nonempty/success", source.ID, source.Payload.Outcome)
	}

	userStream, err := js.Stream(ctx, "USER")
	if err != nil {
		return fmt.Errorf("open USER stream: %w", err)
	}
	responseSubject := "user.response.e2e." + taskID
	deadline := time.Now().Add(15 * time.Second)
	var stored *jetstream.RawStreamMsg
	for time.Now().Before(deadline) {
		stored, err = userStream.GetLastMsgForSubject(ctx, responseSubject)
		if err == nil {
			break
		}
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			return fmt.Errorf("read terminal-derived response: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if stored == nil {
		return fmt.Errorf("terminal-derived response did not publish within 15s")
	}
	wantID := "terminal-user-response:" + source.ID
	if got := stored.Header.Get(nats.MsgIdHdr); got != wantID {
		return fmt.Errorf("terminal response Nats-Msg-Id = %q, want %q", got, wantID)
	}
	registry := payloadregistry.New()
	if err := payloadbuiltins.Register(registry); err != nil {
		return fmt.Errorf("register production payloads for terminal response proof: %w", err)
	}
	decoded, err := message.NewDecoder(registry).Decode(stored.Data)
	if err != nil {
		return fmt.Errorf("decode terminal-derived response through production registry: %w", err)
	}
	if decoded.Type().String() != "agentic.user_response.v1" {
		return fmt.Errorf("terminal response envelope type = %q, want agentic.user_response.v1", decoded.Type())
	}
	response, ok := decoded.Payload().(*agentic.UserResponse)
	if !ok {
		return fmt.Errorf("terminal response payload type = %T, want *agentic.UserResponse", decoded.Payload())
	}
	if response.ResponseID != wantID || response.Type != agentic.ResponseTypeResult ||
		response.ChannelType != "e2e" || response.ChannelID != taskID || response.UserID != "" {
		return fmt.Errorf("terminal response projection = id:%q type:%q route:%q/%q user:%q",
			response.ResponseID, response.Type, response.ChannelType, response.ChannelID, response.UserID)
	}
	if response.Content == "" || response.Timestamp.IsZero() {
		return fmt.Errorf("terminal response missing result content or terminal timestamp")
	}
	result.Details["terminal_response_id"] = wantID
	result.Details["terminal_response_subject"] = responseSubject
	return nil
}

// waitForCompletion waits for agent loop completion
func (s *Scenario) waitForCompletion(ctx context.Context, result *scenarios.Result) error {
	timeout := s.config.CompleteTimeout
	deadline := time.Now().Add(timeout)
	loopID, ok := result.Details["loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("loop_id not found in result details")
	}

	baseline, _ := result.Details["baseline_loops_completed"].(float64)
	loopsCompleted := baseline
	lastTrajectoryError := "not queried"

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			pages, err := s.nats.GetTrajectoryPages(ctx, loopID)
			if err == nil {
				summary, summaryErr := summarizeTrajectoryPages(pages)
				if summaryErr != nil {
					lastTrajectoryError = summaryErr.Error()
				} else {
					lastTrajectoryError = ""
					if summary.completed {
						result.Details["completion_method"] = "target_trajectory"
						return nil
					}
					if summary.terminalStatus == agentic.TrajectoryStatusFailed ||
						summary.terminalStatus == agentic.TrajectoryStatusCancelled {
						return fmt.Errorf("target loop %s ended with status %q", loopID, summary.terminalStatus)
					}
				}
			} else {
				lastTrajectoryError = err.Error()
			}

			// Retain the aggregate metric only as timeout diagnostics. It must
			// never satisfy completion because unrelated loops share it.
			loops, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_loop_loops_completed_total")
			if err == nil && loops > loopsCompleted {
				loopsCompleted = loops
				result.Metrics["loops_completed"] = loopsCompleted
			}
		}
	}

	// Timeout - provide diagnostic info
	result.Details["timeout_loops_completed"] = loopsCompleted
	result.Details["timeout_trajectory_error"] = lastTrajectoryError

	return fmt.Errorf(
		"timeout waiting for target loop %s after %v (loops_completed=%v, trajectory_error=%q)",
		loopID,
		timeout,
		loopsCompleted,
		lastTrajectoryError,
	)
}

// validateTrajectory retrieves and validates the trajectory via NATS query handler.
func (s *Scenario) validateTrajectory(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("loop_id not found in result details")
	}

	pages, err := s.nats.GetTrajectoryPages(ctx, loopID)
	if err != nil {
		return fmt.Errorf("failed to get trajectory for loop %s: %w", loopID, err)
	}
	summary, err := summarizeTrajectoryPages(pages)
	if err != nil {
		return fmt.Errorf("invalid trajectory for loop %s: %w", loopID, err)
	}

	// Validate minimum observed facts.
	if len(summary.facts) < s.config.MinTrajectoryFacts {
		return fmt.Errorf("trajectory has %d facts, expected at least %d", len(summary.facts), s.config.MinTrajectoryFacts)
	}

	// Validate at least one model and tool observation exists.
	hasModelCall := false
	hasToolCall := false
	for _, fact := range summary.facts {
		switch fact.Kind {
		case agentic.TrajectoryKindModelRequested, agentic.TrajectoryKindModelCompleted:
			hasModelCall = true
		case agentic.TrajectoryKindToolRequested, agentic.TrajectoryKindToolCompleted:
			hasToolCall = true
		}
	}

	if !hasModelCall {
		return fmt.Errorf("trajectory has no model observations")
	}
	if !hasToolCall {
		return fmt.Errorf("trajectory has no tool observations")
	}

	if !summary.completed {
		return fmt.Errorf("trajectory terminal status is %q, expected %q",
			summary.terminalStatus, agentic.TrajectoryStatusCompleted)
	}

	// Store metrics
	result.Metrics["trajectory_facts"] = len(summary.facts)
	result.Metrics["trajectory_tokens_in"] = summary.tokensIn
	result.Metrics["trajectory_tokens_out"] = summary.tokensOut
	result.Metrics["trajectory_elapsed_ms"] = summary.elapsedMS
	result.Details["trajectory_terminal_status"] = summary.terminalStatus

	return nil
}

type trajectorySummary struct {
	facts          []agentic.TrajectoryFactV1
	terminalStatus agentic.TrajectoryStatus
	completed      bool
	tokensIn       uint64
	tokensOut      uint64
	elapsedMS      int64
}

func summarizeTrajectoryPages(pages []agentic.TrajectoryPage) (trajectorySummary, error) {
	var summary trajectorySummary
	if len(pages) == 0 {
		return summary, fmt.Errorf("trajectory returned no pages")
	}
	loopID := pages[0].LoopID
	for index, page := range pages {
		if page.SchemaVersion != agentic.TrajectorySchemaV1 || page.LoopID != loopID || page.Coverage != "observed" {
			return trajectorySummary{}, fmt.Errorf("trajectory page %d has invalid metadata", index)
		}
		if page.ObservedTotals.Facts != uint64(len(page.Facts)) {
			return trajectorySummary{}, fmt.Errorf("trajectory page %d fact total does not match facts", index)
		}
		if index < len(pages)-1 && page.NextCursor == "" {
			return trajectorySummary{}, fmt.Errorf("trajectory page %d ends before the final page", index)
		}
		if index == len(pages)-1 && page.NextCursor != "" {
			return trajectorySummary{}, fmt.Errorf("trajectory final page still has continuation")
		}

		pageTerminal := false
		for _, fact := range page.Facts {
			if fact.Kind == agentic.TrajectoryKindLoopTerminal {
				pageTerminal = true
				summary.terminalStatus = fact.Status
				if fact.Status == agentic.TrajectoryStatusCompleted {
					summary.completed = true
				}
			}
		}
		if page.TerminalObserved != pageTerminal {
			return trajectorySummary{}, fmt.Errorf("trajectory page %d terminal truth does not match facts", index)
		}

		summary.facts = append(summary.facts, page.Facts...)
		summary.tokensIn += page.ObservedTotals.TokensIn
		summary.tokensOut += page.ObservedTotals.TokensOut
		summary.elapsedMS += page.ObservedTotals.ElapsedMS
	}
	return summary, nil
}

func sumSnapshotMetric(snapshot *client.MetricsSnapshot, metricName string) float64 {
	if snapshot == nil {
		return 0
	}

	var sum float64
	for _, metric := range snapshot.Metrics {
		if metric.Name == metricName {
			sum += metric.Value
		}
	}
	return sum
}

// verifyGraphTriples verifies that the graph writer emitted triples for the loop
// execution and model endpoint entities. This validates the full path:
// agentic-loop → graph.mutation.triple.append → graph-ingest → ENTITY_STATES KV.
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
		// Model endpoint entities are born at startup through entity.create with
		// the registered agentic.model_endpoint.v1 stamp; a missing entity means
		// the birth was refused or never happened, so this tier fails rather
		// than warns (ADR-103, N-1).
		return fmt.Errorf("model endpoint entity %s not found: %w", modelEntityID, err)
	}
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

// verifyToolCallGovernance checks that the ADR-039 subject-mode
// governance flow fired. The e2e config sets agentic-loop.mode=audit
// and ships an approve-all rule that subscribes to
// agent.toolcall.proposed.>. When the temperature-anomaly task issues
// its tool call (`query_entity`), the loop publishes a proposed-call,
// the rule fires an approve verdict, and three metrics increment:
//
//   - tool_call_governance_verdict_total{mode="audit"} — verdict received
//   - tool_call_governance_verdict_duration_seconds_count — duration observed
//   - rule_actions_executed_total — approve action fired in the rule engine
//
// Audit mode means dispatch is NOT gated; this stage validates the
// governance path is wired end-to-end without affecting the existing
// scenario's success criteria. HARD FAILURE because:
//   - default mode=disabled retains pre-ADR-039 behavior, so failure
//     here means the e2e config didn't take effect (regression: this
//     was the canonical e2e coverage we added pre-tag).
//   - audit-mode failure is the cheapest leading indicator that
//     enforce-mode would wedge in production.
func (s *Scenario) verifyToolCallGovernance(ctx context.Context, result *scenarios.Result) error {
	verdictTotal, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_loop_tool_call_governance_verdict_total")
	if err != nil {
		return fmt.Errorf("failed to fetch governance verdict counter: %w", err)
	}
	result.Metrics["governance_verdicts_total"] = verdictTotal

	if verdictTotal < 1 {
		return fmt.Errorf(
			"governance verdict counter is 0 — agentic-loop did not publish a proposed-call OR the rule processor did not emit a verdict. " +
				"Check (1) agentic-loop tool_call_governance.mode is set to 'audit' or 'enforce' in configs/agentic.json, " +
				"(2) the rule processor subscribes to agent.toolcall.proposed.> with a rule whose actions include approve/publish to agent.toolcall.{approved,rejected}.* subjects, " +
				"(3) the rule processor and agentic-loop are healthy")
	}

	// Confirm at least one verdict was an approve (the e2e rule is
	// approve-all). If we ever see only timeouts here, the rule fired
	// but its verdict didn't reach the loop in time — points at NATS
	// propagation or a subject mismatch.
	approves, err := s.metrics.GetMetricByLabels(ctx,
		"semstreams_agentic_loop_tool_call_governance_verdict_total",
		map[string]string{"decision": "approved", "mode": "audit"})
	if err != nil {
		return fmt.Errorf("failed to fetch approve verdict counter by labels: %w", err)
	}
	var approvedCount float64
	for _, m := range approves {
		approvedCount += m.Value
	}
	result.Metrics["governance_verdicts_approved_audit"] = approvedCount

	if approvedCount < 1 {
		return fmt.Errorf("audit-mode approve counter is 0 — rule fired but the verdict subject did not reach the loop. Subject mismatch or stream propagation issue?")
	}

	result.Details["governance_verified"] = true
	return nil
}

// validateResults validates the scenario results
func (s *Scenario) validateResults(_ context.Context, result *scenarios.Result) error {
	completionMethod, _ := result.Details["completion_method"].(string)
	if completionMethod != "target_trajectory" {
		return fmt.Errorf("target loop completion was not verified, method=%q", completionMethod)
	}

	result.Details["validation_passed"] = true
	return nil
}
