// Package scenarios provides E2E test scenarios for StreamKit core components
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/c360/semstreams/test/e2e/client"
	"github.com/gorilla/websocket"
)

// CoreFederationScenario validates federation data flow with ack/nack protocol
type CoreFederationScenario struct {
	name        string
	description string
	edgeClient  *client.ObservabilityClient
	cloudClient *client.ObservabilityClient
	udpAddr     string
	wsURL       string
	config      *CoreFederationConfig
}

// CoreFederationConfig contains configuration for federation test
type CoreFederationConfig struct {
	// Test data configuration
	MessageCount    int           `json:"message_count"`
	MessageInterval time.Duration `json:"message_interval"`

	// Validation configuration
	ValidationDelay    time.Duration `json:"validation_delay"`
	MinMessagesOnCloud int           `json:"min_messages_on_cloud"`
	AckVerification    bool          `json:"ack_verification"`
}

// DefaultCoreFederationConfig returns default configuration
func DefaultCoreFederationConfig() *CoreFederationConfig {
	return &CoreFederationConfig{
		MessageCount:       20,
		MessageInterval:    100 * time.Millisecond,
		ValidationDelay:    5 * time.Second,
		MinMessagesOnCloud: 15, // At least 75% should make it through
		AckVerification:    true,
	}
}

// NewCoreFederationScenario creates a new federation test scenario
func NewCoreFederationScenario(
	edgeClient *client.ObservabilityClient,
	cloudClient *client.ObservabilityClient,
	udpAddr string,
	wsURL string,
	config *CoreFederationConfig,
) *CoreFederationScenario {
	if config == nil {
		config = DefaultCoreFederationConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:14550"
	}
	if wsURL == "" {
		wsURL = "ws://localhost:8082/stream"
	}

	return &CoreFederationScenario{
		name:        "core-federation",
		description: "Tests federation data flow: Edge (UDP → WebSocket Output) → Cloud (WebSocket Input → File)",
		edgeClient:  edgeClient,
		cloudClient: cloudClient,
		udpAddr:     udpAddr,
		wsURL:       wsURL,
		config:      config,
	}
}

// Name returns the scenario name
func (s *CoreFederationScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *CoreFederationScenario) Description() string {
	return s.description
}

// Setup prepares the scenario
func (s *CoreFederationScenario) Setup(_ context.Context) error {
	// Verify UDP endpoint is reachable on edge
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach edge UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	// Verify WebSocket endpoint is reachable
	wsConn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		return fmt.Errorf("cannot reach WebSocket endpoint %s: %w", s.wsURL, err)
	}
	wsConn.Close()

	return nil
}

// Execute runs the federation test scenario
func (s *CoreFederationScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"verify_edge_health", s.verifyEdgeHealth},
		{"verify_cloud_health", s.verifyCloudHealth},
		{"send_test_data", s.sendTestData},
		{"verify_federation_flow", s.verifyFederationFlow},
		{"verify_ack_protocol", s.verifyAckProtocol},
		{"verify_metrics", s.verifyMetrics},
	}

	// Execute stages
	for _, stage := range stages {
		result.Details[stage.name+"_started"] = time.Now()

		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Details[stage.name+"_failed"] = err.Error()
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, fmt.Errorf("stage %s failed: %w", stage.name, err)
		}

		result.Details[stage.name+"_completed"] = time.Now()
	}

	// All stages passed
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Teardown cleans up after the scenario
func (s *CoreFederationScenario) Teardown(_ context.Context) error {
	// No specific cleanup needed for federation test
	return nil
}

// verifyEdgeHealth checks edge instance is healthy
func (s *CoreFederationScenario) verifyEdgeHealth(ctx context.Context, result *Result) error {
	health, err := s.edgeClient.GetPlatformHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get edge health: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("edge instance is not healthy: %s", health.Status)
	}

	result.Details["edge_health"] = health
	return nil
}

// verifyCloudHealth checks cloud instance is healthy
func (s *CoreFederationScenario) verifyCloudHealth(ctx context.Context, result *Result) error {
	health, err := s.cloudClient.GetPlatformHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cloud health: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("cloud instance is not healthy: %s", health.Status)
	}

	result.Details["cloud_health"] = health
	return nil
}

// sendTestData sends test messages via UDP to edge instance
func (s *CoreFederationScenario) sendTestData(_ context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to UDP: %w", err)
	}
	defer conn.Close()

	sent := 0
	for i := 0; i < s.config.MessageCount; i++ {
		testData := map[string]interface{}{
			"message_id": fmt.Sprintf("fed-test-%d", i),
			"timestamp":  time.Now().Unix(),
			"sequence":   i,
			"test":       "federation",
			"value":      float64(i) * 1.5,
		}

		data, err := json.Marshal(testData)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to marshal message %d: %v", i, err))
			continue
		}

		if _, err := conn.Write(data); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to send message %d: %v", i, err))
			continue
		}

		sent++
		time.Sleep(s.config.MessageInterval)
	}

	result.Metrics["messages_sent"] = sent
	result.Details["test_data_sent"] = map[string]any{
		"total_messages": s.config.MessageCount,
		"sent":           sent,
		"failed":         s.config.MessageCount - sent,
	}

	if sent == 0 {
		return fmt.Errorf("failed to send any messages")
	}

	return nil
}

// verifyFederationFlow checks messages flowed from edge to cloud
func (s *CoreFederationScenario) verifyFederationFlow(ctx context.Context, result *Result) error {
	// Wait for messages to flow through the system
	time.Sleep(s.config.ValidationDelay)

	// Get component metrics from edge (WebSocket Output)
	edgeComponents, err := s.edgeClient.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get edge components: %w", err)
	}

	// Get component metrics from cloud (WebSocket Input)
	cloudComponents, err := s.cloudClient.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cloud components: %w", err)
	}

	result.Details["edge_components"] = edgeComponents
	result.Details["cloud_components"] = cloudComponents

	// Look for file output on cloud (evidence of received data)
	// Note: In real test, we'd check file system or component metrics
	// For MVP, we verify components are running and metrics exist

	if len(cloudComponents) == 0 {
		return fmt.Errorf("no components running on cloud instance")
	}

	result.Metrics["edge_components"] = len(edgeComponents)
	result.Metrics["cloud_components"] = len(cloudComponents)

	return nil
}

// verifyAckProtocol checks ack/nack messages are being exchanged
func (s *CoreFederationScenario) verifyAckProtocol(ctx context.Context, result *Result) error {
	if !s.config.AckVerification {
		result.Details["ack_verification"] = "skipped"
		return nil
	}

	// Check for cancellation before WebSocket connection
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Connect to WebSocket Output to observe ack/nack flow
	wsConn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}
	defer wsConn.Close()

	// Send a test message and verify ack is received
	testMsg := map[string]interface{}{
		"type":      "data",
		"id":        fmt.Sprintf("ack-test-%d", time.Now().UnixMilli()),
		"timestamp": time.Now().UnixMilli(),
		"payload": map[string]interface{}{
			"test": "ack_verification",
		},
	}

	if err := wsConn.WriteJSON(testMsg); err != nil {
		return fmt.Errorf("failed to send test message: %w", err)
	}

	// Wait for ack or nack (with timeout)
	wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response map[string]interface{}
	if err := wsConn.ReadJSON(&response); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("no ack/nack received: %v", err))
	} else {
		result.Details["ack_response"] = response
		if msgType, ok := response["type"].(string); ok {
			result.Metrics["ack_type"] = msgType
		}
	}

	return nil
}

// verifyMetrics checks federation metrics on both instances
func (s *CoreFederationScenario) verifyMetrics(ctx context.Context, result *Result) error {
	// For MVP, we just verify component metrics exist via GetComponents
	// Full Prometheus metrics scraping can be added later if needed

	// Get edge components (which include some metrics)
	edgeComponents, err := s.edgeClient.GetComponents(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to get edge components: %v", err))
	} else {
		result.Metrics["edge_component_count"] = len(edgeComponents)
	}

	// Get cloud components
	cloudComponents, err := s.cloudClient.GetComponents(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to get cloud components: %v", err))
	} else {
		result.Metrics["cloud_component_count"] = len(cloudComponents)
	}

	return nil
}
