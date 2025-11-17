// Package scenarios provides E2E test scenarios for StreamKit core components
package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/c360/semstreams/test/e2e/client"
)

// CoreHealthScenario validates core component health
type CoreHealthScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	config      *CoreHealthConfig
}

// CoreHealthConfig contains configuration for core health check
type CoreHealthConfig struct {
	// Validation thresholds
	RequireAllHealthy    bool          `json:"require_all_healthy"`
	MinHealthyComponents int           `json:"min_healthy_components"`
	MaxStartupTime       time.Duration `json:"max_startup_time"`

	// Required core components
	RequiredComponents []string `json:"required_components"`
}

// DefaultCoreHealthConfig returns default configuration for core components
func DefaultCoreHealthConfig() *CoreHealthConfig {
	return &CoreHealthConfig{
		RequireAllHealthy:    true,
		MinHealthyComponents: 8, // UDP, JSONGeneric, JSONFilter, JSONMap, File, HTTP POST, WebSocket, ObjectStore
		MaxStartupTime:       10 * time.Second,
		RequiredComponents: []string{
			// Input (network)
			"udp",
			// Processors (Tier 1 - Generic JSON)
			"json_generic",
			"json_filter",
			"json_map",
			// Storage (CORE)
			"objectstore",
			// Outputs
			"file",
			"httppost",
			"websocket",
		},
	}
}

// NewCoreHealthScenario creates a new core health check scenario
func NewCoreHealthScenario(obsClient *client.ObservabilityClient, config *CoreHealthConfig) *CoreHealthScenario {
	if config == nil {
		config = DefaultCoreHealthConfig()
	}

	return &CoreHealthScenario{
		name:        "core-health",
		description: "Validates StreamKit core component health (UDP, JSONGeneric, JSONFilter, JSONMap, File, HTTP POST, WebSocket, ObjectStore)",
		client:      obsClient,
		config:      config,
	}
}

// Name returns the scenario name
func (s *CoreHealthScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *CoreHealthScenario) Description() string {
	return s.description
}

// Setup prepares the scenario (no-op for health check)
func (s *CoreHealthScenario) Setup(_ context.Context) error {
	// Health check doesn't need setup
	return nil
}

// Execute runs the core health check scenario
func (s *CoreHealthScenario) Execute(ctx context.Context) (*Result, error) {
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
		{"platform-health", s.executePlatformHealth},
		{"component-health", s.executeComponentHealth},
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

// Teardown cleans up after the scenario (no-op for health check)
func (s *CoreHealthScenario) Teardown(ctx context.Context) error {
	// Check for cancellation even though no cleanup needed
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	// Health check doesn't need teardown
	return nil
}

// executePlatformHealth checks platform-level health
func (s *CoreHealthScenario) executePlatformHealth(ctx context.Context, result *Result) error {
	health, err := s.client.GetPlatformHealth(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get platform health: %v", err))
		return fmt.Errorf("platform health check failed: %w", err)
	}

	result.Details["platform_health"] = health
	result.Metrics["platform_healthy"] = health.Healthy

	if !health.Healthy {
		result.Errors = append(result.Errors, "Platform is not healthy")
		return fmt.Errorf("platform is not healthy: %s", health.Status)
	}

	return nil
}

// executeComponentHealth checks core component health
func (s *CoreHealthScenario) executeComponentHealth(ctx context.Context, result *Result) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component health check failed: %w", err)
	}

	result.Metrics["total_components"] = len(components)
	result.Details["components"] = components

	// Check required components
	foundComponents := make(map[string]bool)
	healthyCount := 0

	for _, comp := range components {
		foundComponents[comp.Name] = true
		if comp.Enabled && comp.Healthy {
			healthyCount++
		}
	}

	result.Metrics["healthy_components"] = healthyCount

	// Verify all required core components exist
	missingComponents := []string{}
	for _, required := range s.config.RequiredComponents {
		if !foundComponents[required] {
			missingComponents = append(missingComponents, required)
		}
	}

	if len(missingComponents) > 0 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Missing required core components: %v", missingComponents))
		return fmt.Errorf("missing required components: %v", missingComponents)
	}

	// Check minimum healthy components
	if healthyCount < s.config.MinHealthyComponents {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Only %d/%d components healthy (minimum: %d)",
				healthyCount, len(components), s.config.MinHealthyComponents))
		return fmt.Errorf("insufficient healthy components: %d < %d",
			healthyCount, s.config.MinHealthyComponents)
	}

	// Check if all required components are healthy
	if s.config.RequireAllHealthy {
		unhealthyComponents := []string{}
		for _, comp := range components {
			// Check if this is a required component
			isRequired := false
			for _, required := range s.config.RequiredComponents {
				if comp.Name == required {
					isRequired = true
					break
				}
			}

			if isRequired && (!comp.Enabled || !comp.Healthy) {
				unhealthyComponents = append(unhealthyComponents, comp.Name)
			}
		}

		if len(unhealthyComponents) > 0 {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Unhealthy required components: %v", unhealthyComponents))
			return fmt.Errorf("unhealthy required components: %v", unhealthyComponents)
		}
	}

	return nil
}
