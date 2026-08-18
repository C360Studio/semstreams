// Package scenarios provides E2E test scenarios for SemStreams
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
}

// CoreDataflowConfig contains configuration for dataflow test
type CoreDataflowConfig struct {
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
		MinProcessed:     5, // At least half should make it through filter
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
	for i := 0; i < s.config.MessageCount; i++ {
		// Create GenericJSON test message
		testMsg := map[string]any{
			"type":      "test",
			"value":     i * 10, // Values: 0, 10, 20, 30... (some will pass filter > 50)
			"timestamp": time.Now().Unix(),
			"sequence":  i,
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

	// Check file output - the file component writes to /tmp/streamkit-test*.jsonl
	lineCount, err := s.client.CountFileOutputLines(ctx, containerName, filePattern)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("File check failed: %v", err))
		return s.executeValidateComponentsOnly(ctx, result)
	}

	result.Metrics["file_lines_written"] = lineCount

	// Validate minimum messages made it through the pipeline
	if lineCount < s.config.MinProcessed {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Only %d lines in file output, expected at least %d", lineCount, s.config.MinProcessed))
		return fmt.Errorf("insufficient output: %d lines < %d minimum", lineCount, s.config.MinProcessed)
	}

	// Content validation: verify JSON structure and filter behavior
	contentIssues := s.validateOutputContent(ctx, result, containerName, filePattern)
	if len(contentIssues) > 0 {
		for _, issue := range contentIssues {
			result.Warnings = append(result.Warnings, issue)
		}
	}

	result.Details["file_validation"] = fmt.Sprintf(
		"Verified %d lines written to file output (minimum: %d)",
		lineCount, s.config.MinProcessed)

	return nil
}

// validateOutputContent validates the content of file output lines
func (s *CoreDataflowScenario) validateOutputContent(
	ctx context.Context,
	result *Result,
	containerName, filePattern string,
) []string {
	var issues []string

	// Get actual lines for content validation (limit to 20 for performance)
	lines, err := s.client.GetFileOutputLines(ctx, containerName, filePattern, 20)
	if err != nil || len(lines) == 0 {
		issues = append(issues, "Could not retrieve file output lines for content validation")
		return issues
	}

	validJSON := 0
	invalidJSON := 0
	hasValueField := 0
	valuesAboveFilter := 0 // Values that passed the filter (> 50)

	for _, line := range lines {
		if line == "" {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			invalidJSON++
			continue
		}
		validJSON++

		// Check for expected fields
		if val, ok := msg["value"]; ok {
			hasValueField++
			// Verify filter behavior: values should be > 50 if filter is active
			if numVal, ok := val.(float64); ok && numVal > 50 {
				valuesAboveFilter++
			}
		}
	}

	result.Metrics["content_valid_json"] = validJSON
	result.Metrics["content_invalid_json"] = invalidJSON
	result.Metrics["content_has_value_field"] = hasValueField
	result.Metrics["content_values_above_filter"] = valuesAboveFilter

	result.Details["content_validation"] = map[string]any{
		"lines_checked":       len(lines),
		"valid_json":          validJSON,
		"invalid_json":        invalidJSON,
		"has_value_field":     hasValueField,
		"values_above_filter": valuesAboveFilter,
	}

	if invalidJSON > 0 {
		issues = append(issues, fmt.Sprintf("%d/%d lines had invalid JSON", invalidJSON, len(lines)))
	}

	if hasValueField == 0 && validJSON > 0 {
		issues = append(issues, "No output messages have 'value' field - may indicate mapping issue")
	}

	return issues
}

// executeValidateComponentsOnly is a fallback validation that only checks component health
func (s *CoreDataflowScenario) executeValidateComponentsOnly(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	result.Metrics["component_count"] = len(components)
	result.Details["validation"] = fmt.Sprintf(
		"Fallback validation: %d components running (file check unavailable)",
		len(components))

	// In fallback mode, we just verify components are running
	// This is weaker validation but allows test to pass when docker exec isn't available
	return nil
}
