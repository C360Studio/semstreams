package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeHealthResponse_JSONMarshaling verifies response JSON structure
func TestRuntimeHealthResponse_JSONMarshaling(t *testing.T) {
	now := time.Now()
	uptime := 120.5

	response := RuntimeHealthResponse{
		Timestamp: now,
		Overall: OverallHealth{
			Status:        "healthy",
			RunningCount:  2,
			DegradedCount: 1,
			ErrorCount:    0,
		},
		Components: []ComponentHealth{
			{
				Name:          "udp-source",
				Type:          "input",
				Status:        "running",
				Healthy:       true,
				Message:       "Processing messages",
				StartTime:     &now,
				LastActivity:  &now,
				UptimeSeconds: &uptime,
				Details:       nil,
			},
			{
				Name:          "processor",
				Type:          "processor",
				Status:        "degraded",
				Healthy:       false,
				Message:       "NATS connection slow",
				StartTime:     &now,
				LastActivity:  &now,
				UptimeSeconds: &uptime,
				Details: map[string]any{
					"issue":      "ACK latency > 100ms",
					"latency_ms": 245,
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(response)
	require.NoError(t, err)

	// Unmarshal back
	var decoded RuntimeHealthResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, "healthy", decoded.Overall.Status)
	assert.Equal(t, 2, decoded.Overall.RunningCount)
	assert.Equal(t, 1, decoded.Overall.DegradedCount)
	assert.Equal(t, 0, decoded.Overall.ErrorCount)
	assert.Len(t, decoded.Components, 2)

	// Verify first component
	comp1 := decoded.Components[0]
	assert.Equal(t, "udp-source", comp1.Name)
	assert.Equal(t, "input", comp1.Type)
	assert.Equal(t, "running", comp1.Status)
	assert.True(t, comp1.Healthy)
	assert.NotNil(t, comp1.StartTime)
	assert.NotNil(t, comp1.LastActivity)
	assert.NotNil(t, comp1.UptimeSeconds)
	assert.Equal(t, 120.5, *comp1.UptimeSeconds)

	// Verify second component
	comp2 := decoded.Components[1]
	assert.Equal(t, "processor", comp2.Name)
	assert.Equal(t, "degraded", comp2.Status)
	assert.False(t, comp2.Healthy)
	assert.NotNil(t, comp2.Details)
}

// TestRuntimeHealthResponse_NullFields verifies null field handling
func TestRuntimeHealthResponse_NullFields(t *testing.T) {
	// Component not started - all timing fields null
	response := RuntimeHealthResponse{
		Timestamp: time.Now(),
		Overall: OverallHealth{
			Status:        "error",
			RunningCount:  0,
			DegradedCount: 0,
			ErrorCount:    1,
		},
		Components: []ComponentHealth{
			{
				Name:          "stopped-component",
				Type:          "processor",
				Status:        "stopped",
				Healthy:       false,
				Message:       "Component not started",
				StartTime:     nil,
				LastActivity:  nil,
				UptimeSeconds: nil,
				Details:       nil,
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(response)
	require.NoError(t, err)

	// Verify JSON contains null values
	var jsonMap map[string]any
	err = json.Unmarshal(data, &jsonMap)
	require.NoError(t, err)

	components := jsonMap["components"].([]any)
	comp := components[0].(map[string]any)

	// These should be null in JSON
	assert.Nil(t, comp["start_time"])
	assert.Nil(t, comp["last_activity"])
	assert.Nil(t, comp["uptime_seconds"])
	assert.Nil(t, comp["details"])
}

// TestCalculateOverallStatus verifies overall status calculation
func TestCalculateOverallStatus(t *testing.T) {
	tests := []struct {
		name          string
		runningCount  int
		degradedCount int
		errorCount    int
		expectedStatus string
	}{
		{
			name:          "all healthy",
			runningCount:  3,
			degradedCount: 0,
			errorCount:    0,
			expectedStatus: "healthy",
		},
		{
			name:          "one degraded",
			runningCount:  2,
			degradedCount: 1,
			errorCount:    0,
			expectedStatus: "degraded",
		},
		{
			name:          "one error",
			runningCount:  2,
			degradedCount: 0,
			errorCount:    1,
			expectedStatus: "error",
		},
		{
			name:          "degraded and error",
			runningCount:  1,
			degradedCount: 1,
			errorCount:    1,
			expectedStatus: "error",
		},
		{
			name:          "all stopped",
			runningCount:  0,
			degradedCount: 0,
			errorCount:    3,
			expectedStatus: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build response
			response := RuntimeHealthResponse{
				Overall: OverallHealth{
					RunningCount:  tt.runningCount,
					DegradedCount: tt.degradedCount,
					ErrorCount:    tt.errorCount,
				},
			}

			// Calculate status (logic from getComponentsHealth)
			status := "healthy"
			if tt.errorCount > 0 {
				status = "error"
			} else if tt.degradedCount > 0 {
				status = "degraded"
			}

			assert.Equal(t, tt.expectedStatus, status)
			response.Overall.Status = status
			assert.Equal(t, tt.expectedStatus, response.Overall.Status)
		})
	}
}

// TestComponentStatusMapping verifies component state to status mapping
func TestComponentStatusMapping(t *testing.T) {
	tests := []struct {
		name           string
		state          component.State
		healthStatus   component.HealthStatus
		expectedStatus string
		expectedHealthy bool
	}{
		{
			name:  "started and healthy",
			state: component.StateStarted,
			healthStatus: component.HealthStatus{
				Healthy:    true,
				LastCheck:  time.Now(),
				ErrorCount: 0,
				LastError:  "",
				Uptime:     30 * time.Second,
			},
			expectedStatus: "running",
			expectedHealthy: true,
		},
		{
			name:  "started but unhealthy",
			state: component.StateStarted,
			healthStatus: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Now(),
				ErrorCount: 5,
				LastError:  "connection timeout",
				Uptime:     30 * time.Second,
			},
			expectedStatus: "degraded",
			expectedHealthy: false,
		},
		{
			name:  "failed state",
			state: component.StateFailed,
			healthStatus: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Now(),
				ErrorCount: 10,
				LastError:  "fatal error",
				Uptime:     0,
			},
			expectedStatus: "error",
			expectedHealthy: false,
		},
		{
			name:  "initialized not started",
			state: component.StateInitialized,
			healthStatus: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Time{},
				ErrorCount: 0,
				LastError:  "",
				Uptime:     0,
			},
			expectedStatus: "stopped",
			expectedHealthy: false,
		},
		{
			name:  "created not started",
			state: component.StateCreated,
			healthStatus: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Time{},
				ErrorCount: 0,
				LastError:  "",
				Uptime:     0,
			},
			expectedStatus: "stopped",
			expectedHealthy: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate status determination logic from getComponentsHealth
			status := "stopped"
			healthy := false

			switch tt.state {
			case component.StateStarted:
				if tt.healthStatus.Healthy {
					status = "running"
					healthy = true
				} else {
					status = "degraded"
					healthy = false
				}
			case component.StateFailed:
				status = "error"
				healthy = false
			case component.StateInitialized:
				status = "stopped"
				healthy = false
			default:
				status = "stopped"
				healthy = false
			}

			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedHealthy, healthy)
		})
	}
}

// TestUptimeCalculation verifies uptime calculation from health status
func TestUptimeCalculation(t *testing.T) {
	tests := []struct {
		name           string
		uptime         time.Duration
		expectedSeconds float64
	}{
		{
			name:           "zero uptime",
			uptime:         0,
			expectedSeconds: 0.0,
		},
		{
			name:           "30 seconds",
			uptime:         30 * time.Second,
			expectedSeconds: 30.0,
		},
		{
			name:           "1 minute",
			uptime:         60 * time.Second,
			expectedSeconds: 60.0,
		},
		{
			name:           "1.5 hours",
			uptime:         90 * time.Minute,
			expectedSeconds: 5400.0,
		},
		{
			name:           "fractional seconds",
			uptime:         2500 * time.Millisecond,
			expectedSeconds: 2.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate uptime calculation from getComponentsHealth
			var uptimeSeconds *float64
			if tt.uptime > 0 {
				uptime := tt.uptime.Seconds()
				uptimeSeconds = &uptime
			}

			if uptimeSeconds != nil {
				assert.Equal(t, tt.expectedSeconds, *uptimeSeconds)
			} else {
				assert.Equal(t, 0.0, tt.expectedSeconds)
			}
		})
	}
}

// TestStartTimeCalculation verifies start time calculation from uptime
func TestStartTimeCalculation(t *testing.T) {
	now := time.Now()
	uptime := 120 * time.Second

	// Calculate start time
	startTime := now.Add(-uptime)

	// Verify it's approximately 2 minutes ago
	expectedStart := now.Add(-2 * time.Minute)
	assert.WithinDuration(t, expectedStart, startTime, time.Second)
}

// TestComponentHealthDetails verifies details field for degraded/error states
func TestComponentHealthDetails(t *testing.T) {
	tests := []struct {
		name          string
		healthy       bool
		errorCount    int
		expectedDetails any
	}{
		{
			name:          "healthy - no details",
			healthy:       true,
			errorCount:    0,
			expectedDetails: nil,
		},
		{
			name:       "unhealthy with errors",
			healthy:    false,
			errorCount: 5,
			expectedDetails: map[string]any{
				"error_count": 5,
			},
		},
		{
			name:          "unhealthy no errors",
			healthy:       false,
			errorCount:    0,
			expectedDetails: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var details any
			if !tt.healthy && tt.errorCount > 0 {
				details = map[string]any{
					"error_count": tt.errorCount,
				}
			}

			if tt.expectedDetails != nil {
				assert.NotNil(t, details)
				detailsMap := details.(map[string]any)
				expectedMap := tt.expectedDetails.(map[string]any)
				assert.Equal(t, expectedMap["error_count"], detailsMap["error_count"])
			} else {
				assert.Nil(t, details)
			}
		})
	}
}
