// Package plugin provides base processor testing utilities.
package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBaseProcessorFunctionality tests the BaseProcessor default implementations
func TestBaseProcessorFunctionality(t *testing.T) {
	info := ProcessorInfo{
		Name:        "test-processor",
		Domain:      "test",
		Version:     "1.0.0",
		Description: "Test processor",
	}

	base := NewBaseProcessor(info)
	require.NotNil(t, base)

	// Test basic methods
	assert.Equal(t, "test-processor", base.Name())
	assert.Equal(t, "test", base.Domain())
	assert.Equal(t, "1.0.0", base.Version())
	assert.Equal(t, "Test processor", base.Description())

	// Test health status
	health := base.Health()
	assert.True(t, health.Healthy)
	assert.NotZero(t, health.LastCheck)
	assert.Equal(t, 0, health.ErrorCount)

	// Test configuration
	config := base.Configuration()
	configMap, ok := config.(map[string]any)
	require.True(t, ok, "Configuration should be a map[string]any")
	assert.Equal(t, true, configMap["enabled"])
	assert.Equal(t, "test-processor", configMap["name"])
	assert.Equal(t, "test", configMap["domain"])

	// Test configuration validation (should accept anything)
	err := base.ValidateConfiguration(map[string]any{"test": "value"})
	assert.NoError(t, err)

	err = base.ValidateConfiguration("invalid config")
	assert.NoError(t, err) // Base implementation accepts anything

	// Test configuration reload (should not error)
	err = base.ReloadConfiguration(context.Background(), map[string]any{"enabled": false})
	assert.NoError(t, err)

	// Test metrics
	metrics := base.Metrics()
	assert.NotNil(t, metrics, "Metrics should not be nil")
	assert.Equal(t, "test-processor", metrics["name"])
	assert.Equal(t, "1.0.0", metrics["version"])
	assert.Equal(t, int64(0), metrics["error_count"])
	assert.Contains(t, metrics, "last_activity")
	assert.Contains(t, metrics, "uptime_seconds")

	// Test subscription patterns (should be empty)
	patterns := base.SubscriptionPatterns()
	assert.Empty(t, patterns)

	// Test activity tracking
	oldUptime := health.Uptime
	time.Sleep(time.Millisecond) // Ensure time difference
	base.UpdateActivity()
	newHealth := base.Health()
	// After UpdateActivity(), uptime resets to near-zero since lastActivity is updated to now
	assert.Less(t, newHealth.Uptime, oldUptime, "UpdateActivity() should reset uptime to near-zero")

	// Test error counting
	base.IncrementErrorCount()
	base.IncrementErrorCount()
	healthWithErrors := base.Health()
	assert.Equal(t, 2, healthWithErrors.ErrorCount)

	// Test status setting
	base.SetStatus("degraded")
	degradedHealth := base.Health()
	assert.False(t, degradedHealth.Healthy)
}

// TestHealthStatusValidation tests health status validation
func TestHealthStatusValidation(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		shouldBeValid bool
	}{
		{"healthy status", "healthy", true},
		{"degraded status", "degraded", true},
		{"unhealthy status", "unhealthy", true},
		{"custom status", "custom", true}, // We allow custom statuses
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := HealthStatus{
				Status:       tt.status,
				LastActivity: time.Now(),
				ErrorCount:   0,
				Message:      "test message",
			}

			// Basic validation - status should be set
			assert.Equal(t, tt.status, health.Status)
			assert.NotZero(t, health.LastActivity)
			assert.NotEmpty(t, health.Message)
		})
	}
}

// TestProcessorInterface tests that the interface is complete
func TestProcessorInterface(t *testing.T) {
	// Create a mock processor that implements all methods
	mockProcessor := NewMockProcessor()

	// Verify it implements Processor interface
	var _ Processor = mockProcessor

	// Test that all methods are callable
	assert.Equal(t, "mock", mockProcessor.Name())
	assert.Equal(t, "mock", mockProcessor.Domain())

	ctx := context.Background()
	err := mockProcessor.Initialize(ctx, nil)
	assert.NoError(t, err)

	err = mockProcessor.Shutdown(ctx)
	assert.NoError(t, err)

	err = mockProcessor.ProcessRawData(ctx, "test", []byte("test"))
	assert.NoError(t, err)

	health := mockProcessor.Health()
	assert.NotEmpty(t, health.LastCheck)

	config := mockProcessor.Configuration()
	assert.NotNil(t, config)

	err = mockProcessor.ValidateConfiguration(config)
	assert.NoError(t, err)

	err = mockProcessor.ReloadConfiguration(ctx, config)
	assert.NoError(t, err)

	metrics := mockProcessor.Metrics()
	assert.NotNil(t, metrics)

	patterns := mockProcessor.SubscriptionPatterns()
	assert.NotNil(t, patterns)
}

// MockProcessor implements Processor interface for testing
type MockProcessor struct {
	*BaseProcessor
}

// NewMockProcessor creates a new mock processor
func NewMockProcessor() *MockProcessor {
	info := ProcessorInfo{
		Name:        "mock",
		Domain:      "mock",
		Version:     "1.0.0",
		Description: "Mock processor for testing",
	}

	return &MockProcessor{
		BaseProcessor: NewBaseProcessor(info),
	}
}

// ProcessRawData is required by Processor interface
func (m *MockProcessor) ProcessRawData(_ context.Context, _ string, _ []byte) error {
	return nil
}

// GetSubscriptionPatterns returns test patterns
func (m *MockProcessor) GetSubscriptionPatterns() []string {
	return []string{"raw.*.test"}
}
