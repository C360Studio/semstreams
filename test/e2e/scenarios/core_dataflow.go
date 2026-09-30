// Package scenarios provides E2E test scenarios for SemStreams
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/gorilla/websocket"
)

const protocolFlowWebSocketURL = "ws://localhost:38082/e2e-output"

// CoreDataflowScenario validates complete core data pipeline
type CoreDataflowScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	udpAddr     string
	config      *CoreDataflowConfig
	sentInputs  map[int]coreSentInput
}

type coreSentInput struct {
	Value     int
	Timestamp int64
}

// CoreDataflowConfig contains configuration for dataflow test
type CoreDataflowConfig struct {
	EvidenceRunID    string `json:"evidence_run_id,omitempty"`
	EvidenceMemberID string `json:"evidence_member_id,omitempty"`

	// Test data configuration
	MessageCount    int           `json:"message_count"`
	MessageInterval time.Duration `json:"message_interval"`

	// Validation configuration
	ValidationDelay time.Duration `json:"validation_delay"`
	MinProcessed    int           `json:"min_processed"`

	// WebSocket configuration
	WebSocketTimeout time.Duration `json:"websocket_timeout"`
	TestFlowID       string        `json:"test_flow_id"`
}

// DefaultCoreDataflowConfig returns default configuration
func DefaultCoreDataflowConfig() *CoreDataflowConfig {
	return &CoreDataflowConfig{
		MessageCount:     10,
		MessageInterval:  100 * time.Millisecond,
		ValidationDelay:  5 * time.Second,
		MinProcessed:     5, // Minimum distinct run-correlated pass-through records
		WebSocketTimeout: 30 * time.Second,
		TestFlowID:       "e2e-test-flow",
	}
}

// NewCoreDataflowScenario creates a new core dataflow test scenario
func NewCoreDataflowScenario(
	obsClient *client.ObservabilityClient,
	wsClient *client.WebSocketClient,
	udpAddr string,
	config *CoreDataflowConfig,
) *CoreDataflowScenario {
	if config == nil {
		config = DefaultCoreDataflowConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:34550"
	}
	_ = wsClient // The legacy status-stream client remains outside this cutover.

	return &CoreDataflowScenario{
		name:        "core-dataflow",
		description: "Tests UDP → JSONFilter → JSONMap → File plus the protocol WebSocket output route",
		client:      obsClient,
		udpAddr:     udpAddr,
		config:      config,
	}
}

// Name returns the scenario name
func (s *CoreDataflowScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *CoreDataflowScenario) Description() string {
	return s.description
}

// CheckRequirements declares the three independent pass-through comparisons.
func (s *CoreDataflowScenario) CheckRequirements() []CheckRequirement {
	return []CheckRequirement{
		{ID: "core-dataflow.pass-through-count", Required: true},
		{ID: "core-dataflow.pass-through-content", Required: true},
		{ID: "core-dataflow.pass-through-identity", Required: true},
	}
}

// Setup prepares the scenario
func (s *CoreDataflowScenario) Setup(_ context.Context) error {
	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	return nil
}

// Execute runs the dataflow test scenario
func (s *CoreDataflowScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}
	if s.config.EvidenceRunID != "" || s.config.EvidenceMemberID != "" {
		if err := result.DeclareChecks(s.config.EvidenceRunID, s.config.EvidenceMemberID, s.CheckRequirements()); err != nil {
			return result, err
		}
	}

	// Track execution stages
	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-components", s.executeVerifyComponents},
		{"verify-websocket-output-route", s.executeVerifyWebSocketOutputRoute},
		{"send-data", s.executeSendData},
		{"validate-processing", s.executeValidateProcessing},
		{"verify-objectstore-raw-lane", s.executeVerifyRawObjectStore},
		{"verify-max-delivery-visibility", s.executeVerifyMaxDeliveryVisibility},
	}

	// Execute each stage
	for _, stage := range stages {
		stageStart := time.Now()

		if err := stage.fn(ctx, result); err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil // Return result even on failure
		}

		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	// Overall success
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

func (s *CoreDataflowScenario) executeVerifyWebSocketOutputRoute(ctx context.Context, result *Result) error {
	connection, response, err := websocket.DefaultDialer.DialContext(ctx, protocolFlowWebSocketURL, nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
			return fmt.Errorf("WebSocket upgrade at %s returned HTTP %d: %w", protocolFlowWebSocketURL, response.StatusCode, err)
		}
		return fmt.Errorf("WebSocket upgrade at %s failed: %w", protocolFlowWebSocketURL, err)
	}
	defer connection.Close()
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("WebSocket upgrade at %s returned an unexpected response", protocolFlowWebSocketURL)
	}

	result.Details["websocket_output_route"] = protocolFlowWebSocketURL
	result.Details["websocket_output_upgrade"] = "HTTP 101 Switching Protocols"
	return nil
}

// Teardown cleans up after the scenario
func (s *CoreDataflowScenario) Teardown(_ context.Context) error {
	// No cleanup needed for dataflow test
	return nil
}

// executeVerifyComponents checks that pipeline components exist
func (s *CoreDataflowScenario) executeVerifyComponents(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component verification failed: %w", err)
	}

	requiredComponents := []string{"udp", "json_filter", "json_map", "file"}
	foundComponents := make(map[string]bool)

	for _, comp := range components {
		foundComponents[comp.Name] = true
	}

	missingComponents := []string{}
	for _, required := range requiredComponents {
		if !foundComponents[required] {
			missingComponents = append(missingComponents, required)
		}
	}

	if len(missingComponents) > 0 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Missing pipeline components: %v", missingComponents))
		return fmt.Errorf("missing components: %v", missingComponents)
	}

	result.Details["pipeline_components"] = requiredComponents
	return nil
}

// executeSendData sends test data through the pipeline
func (s *CoreDataflowScenario) executeSendData(ctx context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect to UDP: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	// Send test messages
	messagesSent := 0
	s.sentInputs = make(map[int]coreSentInput, s.config.MessageCount)
	for i := 0; i < s.config.MessageCount; i++ {
		expected := coreSentInput{Value: i * 10, Timestamp: time.Now().Unix()}
		// Create GenericJSON test message
		testMsg := map[string]any{
			"type":      "test",
			"value":     expected.Value,
			"timestamp": expected.Timestamp,
			"sequence":  i,
			"run_id":    s.config.EvidenceRunID,
		}

		msgBytes, err := json.Marshal(testMsg)
		if err != nil {
			continue
		}

		_, err = conn.Write(msgBytes)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to send message %d: %v", i, err))
			continue
		}

		messagesSent++
		s.sentInputs[i] = expected

		// Wait between messages
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.config.MessageInterval):
		}
	}

	result.Metrics["messages_sent"] = messagesSent
	result.Details["data_sent"] = fmt.Sprintf("Sent %d test messages via UDP", messagesSent)

	return nil
}

// executeValidateProcessing validates data was processed through the pipeline
func (s *CoreDataflowScenario) executeValidateProcessing(ctx context.Context, result *Result) error {
	// Wait for processing
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.config.ValidationDelay):
	}

	containerName := "semstreams-e2e-app"
	filePattern := "/tmp/streamkit-test*.jsonl"

	// The count is diagnostic until run-correlated content is inspected.
	lineCount, err := s.client.CountFileOutputLines(ctx, containerName, filePattern)
	if err != nil {
		s.failPassThroughChecks(result, fmt.Sprintf("file output count failed: %v", err))
		return fmt.Errorf("file output count failed: %w", err)
	}
	result.Metrics["file_lines_written"] = lineCount
	// Read the complete output of this fresh E2E container. A fixed head limit
	// could hide the current run behind output from earlier invocations.
	lines, err := s.client.GetFileOutputLines(ctx, containerName, filePattern, 0)
	if err != nil || len(lines) == 0 || (lineCount == 0 && len(lines) > 0) {
		reason := fmt.Sprintf("file output retrieval unavailable: count=%d lines=%d error=%v", lineCount, len(lines), err)
		s.failPassThroughChecks(result, reason)
		return fmt.Errorf("%s", reason)
	}
	validated := validateCorePassThrough(lines, s.config.EvidenceRunID, s.sentInputs)
	result.Metrics["run_output_selected"] = validated.Selected
	result.Metrics["run_output_distinct_valid"] = validated.Distinct
	if validated.Selected == 0 {
		validated.ContentIssues = append(validated.ContentIssues, "no output selected for this run")
		validated.IdentityIssues = append(validated.IdentityIssues, "no output selected for this run")
	}
	countReason := ""
	if validated.Distinct < s.config.MinProcessed {
		countReason = fmt.Sprintf("%d distinct valid sent sequences below minimum %d", validated.Distinct, s.config.MinProcessed)
	}
	evidence := map[string]string{
		"run_id": s.config.EvidenceRunID, "selected": strconv.Itoa(validated.Selected),
		"distinct_valid": strconv.Itoa(validated.Distinct), "minimum": strconv.Itoa(s.config.MinProcessed),
		"successfully_sent": strconv.Itoa(len(s.sentInputs)),
	}
	s.recordPassThroughCheck(result, "core-dataflow.pass-through-count", countReason, evidence)
	s.recordPassThroughCheck(result, "core-dataflow.pass-through-content", strings.Join(validated.ContentIssues, "; "), evidence)
	s.recordPassThroughCheck(result, "core-dataflow.pass-through-identity", strings.Join(validated.IdentityIssues, "; "), evidence)
	if countReason != "" || len(validated.ContentIssues) > 0 || len(validated.IdentityIssues) > 0 {
		return fmt.Errorf("pass-through output invalid: count=%q content=%v identity=%v", countReason, validated.ContentIssues, validated.IdentityIssues)
	}
	result.Details["file_validation"] = fmt.Sprintf("Verified %d distinct correct records for run %s (minimum: %d)",
		validated.Distinct, s.config.EvidenceRunID, s.config.MinProcessed)
	return nil
}

type coreOutputValidation struct {
	Selected       int
	Distinct       int
	ContentIssues  []string
	IdentityIssues []string
}

// validateCorePassThrough checks the actual BaseMessage payload written by the
// file component. Its oracle is the successfully sent input set, not a filter
// threshold or an aggregate count from unrelated runs.
func validateCorePassThrough(lines []string, runID string, sent map[int]coreSentInput) coreOutputValidation {
	var outcome coreOutputValidation
	if runID == "" {
		outcome.IdentityIssues = append(outcome.IdentityIssues, "run identity unavailable")
		return outcome
	}
	seen := make(map[int]bool, len(sent))
	for _, line := range lines {
		if !strings.Contains(line, runID) {
			continue
		}
		outcome.Selected++
		var wire struct {
			Payload struct {
				Data struct {
					RunID     *string `json:"run_id"`
					Sequence  *int    `json:"sequence"`
					Value     *int    `json:"value"`
					Timestamp *int64  `json:"timestamp"`
					Type      *string `json:"type"`
				} `json:"data"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &wire); err != nil {
			outcome.ContentIssues = append(outcome.ContentIssues, fmt.Sprintf("selected output is malformed JSON: %v", err))
			outcome.IdentityIssues = append(outcome.IdentityIssues, "selected output identity could not be compared: malformed JSON")
			continue
		}
		data := wire.Payload.Data
		if data.RunID == nil || *data.RunID != runID || data.Sequence == nil {
			outcome.IdentityIssues = append(outcome.IdentityIssues, "selected output has foreign or missing run/sequence identity")
			outcome.ContentIssues = append(outcome.ContentIssues, "selected output content could not be compared: run/sequence identity unavailable")
			continue
		}
		expected, ok := sent[*data.Sequence]
		if !ok {
			outcome.IdentityIssues = append(outcome.IdentityIssues, fmt.Sprintf("sequence %d was not successfully sent", *data.Sequence))
			outcome.ContentIssues = append(outcome.ContentIssues, fmt.Sprintf("sequence %d content could not be compared: no sent input", *data.Sequence))
			continue
		}
		if data.Value == nil || *data.Value != expected.Value || data.Timestamp == nil || *data.Timestamp != expected.Timestamp || data.Type == nil || *data.Type != "test" {
			outcome.ContentIssues = append(outcome.ContentIssues, fmt.Sprintf("sequence %d differs from sent type/value/timestamp", *data.Sequence))
			continue
		}
		if !seen[*data.Sequence] {
			seen[*data.Sequence] = true
			outcome.Distinct++
		}
	}
	return outcome
}

func (s *CoreDataflowScenario) failPassThroughChecks(result *Result, reason string) {
	for _, check := range s.CheckRequirements() {
		s.recordPassThroughCheck(result, check.ID, reason, nil)
	}
	result.Errors = append(result.Errors, reason)
}

func (s *CoreDataflowScenario) recordPassThroughCheck(result *Result, id, reason string, evidence map[string]string) {
	if result.RunID == "" {
		return
	}
	status := "passed"
	if reason != "" {
		status = "failed"
	}
	_ = result.RecordCheck(CheckObservation{
		ID: id, RunID: result.RunID, MemberID: result.MemberID,
		Status: status, Reason: reason, Evidence: evidence,
	})
}
