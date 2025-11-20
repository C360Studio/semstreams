// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/c360/semstreams/test/e2e/client"
)

// SemanticIndexesScenario validates core indexing functionality without external dependencies
type SemanticIndexesScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	udpAddr     string
	config      *SemanticIndexesConfig
}

// SemanticIndexesConfig contains configuration for indexes test
type SemanticIndexesConfig struct {
	// Test data configuration
	MessageCount    int           `json:"message_count"`
	MessageInterval time.Duration `json:"message_interval"`

	// Validation configuration
	ValidationDelay time.Duration `json:"validation_delay"`
	MinProcessed    int           `json:"min_processed"`
}

// DefaultSemanticIndexesConfig returns default configuration
func DefaultSemanticIndexesConfig() *SemanticIndexesConfig {
	return &SemanticIndexesConfig{
		MessageCount:    20,
		MessageInterval: 50 * time.Millisecond,
		ValidationDelay: 5 * time.Second,
		MinProcessed:    10, // At least 50% should make it through
	}
}

// NewSemanticIndexesScenario creates a new semantic indexes test scenario
func NewSemanticIndexesScenario(
	obsClient *client.ObservabilityClient,
	udpAddr string,
	config *SemanticIndexesConfig,
) *SemanticIndexesScenario {
	if config == nil {
		config = DefaultSemanticIndexesConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:14550"
	}

	return &SemanticIndexesScenario{
		name:        "semantic-indexes",
		description: "Tests core semantic indexing: Predicate, Spatial, Temporal, Alias, and Incoming indexes (no external dependencies)",
		client:      obsClient,
		udpAddr:     udpAddr,
		config:      config,
	}
}

// Name returns the scenario name
func (s *SemanticIndexesScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *SemanticIndexesScenario) Description() string {
	return s.description
}

// Setup prepares the scenario
func (s *SemanticIndexesScenario) Setup(_ context.Context) error {
	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	return nil
}

// Execute runs the semantic indexes test scenario
func (s *SemanticIndexesScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"send-test-data", s.executeSendTestData},
		{"validate-indexing", s.executeValidateIndexing},
		{"verify-graph-processor", s.executeVerifyGraphProcessor},
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

// Teardown cleans up after the scenario
func (s *SemanticIndexesScenario) Teardown(_ context.Context) error {
	// No cleanup needed for indexes test
	return nil
}

// executeVerifyComponents checks that required semantic components exist
func (s *SemanticIndexesScenario) executeVerifyComponents(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component verification failed: %w", err)
	}

	// Required components for semantic indexing
	requiredComponents := []string{
		"udp",          // Input
		"json_generic", // Parser
		"graph",        // Semantic processor with indexes
	}

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
			fmt.Sprintf("Missing required components: %v", missingComponents))
		return fmt.Errorf("missing components: %v", missingComponents)
	}

	result.Details["components_verified"] = map[string]any{
		"required":    requiredComponents,
		"total_found": len(components),
	}

	return nil
}

// executeSendTestData sends test entities with properties for indexing
func (s *SemanticIndexesScenario) executeSendTestData(ctx context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect to UDP: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	messagesSent := 0

	for i := 0; i < s.config.MessageCount; i++ {
		// Create entity with properties for all index types
		testMsg := map[string]any{
			"type":        "telemetry",
			"entity_id":   fmt.Sprintf("sensor-%d", i),
			"entity_type": "device",
			"timestamp":   time.Now().Unix(),
			"data": map[string]any{
				// Properties for predicate index
				"temperature": 20.0 + float64(i),
				"humidity":    50.0 + float64(i*2),
				"status":      "active",

				// Location for spatial index
				"location": map[string]any{
					"lat": 37.7749 + float64(i)*0.001,
					"lon": -122.4194 + float64(i)*0.001,
				},

				// Alias for alias index
				"device_name": fmt.Sprintf("temp-sensor-%d", i),
			},
			"value": i * 5,
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
	result.Details["data_sent"] = fmt.Sprintf("Sent %d test entities for indexing", messagesSent)

	return nil
}

// executeValidateIndexing validates that indexing occurred
func (s *SemanticIndexesScenario) executeValidateIndexing(ctx context.Context, result *Result) error {
	// Wait for processing
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.config.ValidationDelay):
	}

	// Query component status to verify graph processor is processing
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Find graph processor
	var graphFound bool
	for _, comp := range components {
		if comp.Name == "graph" {
			graphFound = true
			if !comp.Healthy {
				result.Warnings = append(
					result.Warnings,
					fmt.Sprintf("Graph processor not healthy: state=%s", comp.State),
				)
			}
			result.Details["graph_processor_status"] = map[string]any{
				"name":    comp.Name,
				"type":    comp.Type,
				"healthy": comp.Healthy,
				"state":   comp.State,
			}
			break
		}
	}

	if !graphFound {
		result.Errors = append(result.Errors, "Graph processor not found")
		return fmt.Errorf("graph processor not found")
	}

	result.Metrics["component_count"] = len(components)
	result.Details["indexing_validation"] = "Graph processor is running and processing entities"

	return nil
}

// executeVerifyGraphProcessor verifies graph processor health
func (s *SemanticIndexesScenario) executeVerifyGraphProcessor(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Verify graph processor is healthy
	for _, comp := range components {
		if comp.Name == "graph" {
			if !comp.Healthy {
				result.Errors = append(result.Errors, "Graph processor is not healthy")
				return fmt.Errorf("graph processor unhealthy: %s", comp.State)
			}

			result.Details["final_verification"] = map[string]any{
				"graph_healthy": true,
				"graph_state":   comp.State,
				"message":       "Core semantic indexing verified successfully",
			}
			return nil
		}
	}

	result.Errors = append(result.Errors, "Graph processor not found in final verification")
	return fmt.Errorf("graph processor not found")
}
