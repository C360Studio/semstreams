package health

import (
	"testing"
	"time"

	"github.com/c360/semstreams/component"
)

func TestStatus_IsHealthy(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{
			name:   "healthy status returns true",
			status: Status{Status: "healthy"},
			want:   true,
		},
		{
			name:   "unhealthy status returns false",
			status: Status{Status: "unhealthy"},
			want:   false,
		},
		{
			name:   "degraded status returns false",
			status: Status{Status: "degraded"},
			want:   false,
		},
		{
			name:   "empty status returns false",
			status: Status{Status: ""},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsHealthy(); got != tt.want {
				t.Errorf("Status.IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_IsDegraded(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{
			name:   "degraded status returns true",
			status: Status{Status: "degraded"},
			want:   true,
		},
		{
			name:   "healthy status returns false",
			status: Status{Status: "healthy"},
			want:   false,
		},
		{
			name:   "unhealthy status returns false",
			status: Status{Status: "unhealthy"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsDegraded(); got != tt.want {
				t.Errorf("Status.IsDegraded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_IsUnhealthy(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{
			name:   "unhealthy status returns true",
			status: Status{Status: "unhealthy"},
			want:   true,
		},
		{
			name:   "healthy status returns false",
			status: Status{Status: "healthy"},
			want:   false,
		},
		{
			name:   "degraded status returns false",
			status: Status{Status: "degraded"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsUnhealthy(); got != tt.want {
				t.Errorf("Status.IsUnhealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_WithMetrics(t *testing.T) {
	original := Status{
		Component: "test",
		Status:    "healthy",
		Message:   "test message",
	}

	metrics := &Metrics{
		Uptime:     time.Hour,
		ErrorCount: 5,
	}

	result := original.WithMetrics(metrics)

	// Should not modify original
	if original.Metrics != nil {
		t.Error("WithMetrics should not modify original status")
	}

	// Should return copy with metrics
	if result.Metrics == nil {
		t.Error("WithMetrics should return status with metrics")
	}

	if result.Metrics.Uptime != time.Hour {
		t.Errorf("Expected uptime %v, got %v", time.Hour, result.Metrics.Uptime)
	}

	if result.Metrics.ErrorCount != 5 {
		t.Errorf("Expected error count 5, got %d", result.Metrics.ErrorCount)
	}
}

func TestStatus_WithSubStatus(t *testing.T) {
	original := Status{
		Component: "parent",
		Status:    "healthy",
		Message:   "parent message",
	}

	subStatus := Status{
		Component: "child",
		Status:    "unhealthy",
		Message:   "child message",
	}

	result := original.WithSubStatus(subStatus)

	// Should not modify original
	if len(original.SubStatuses) != 0 {
		t.Error("WithSubStatus should not modify original status")
	}

	// Should return copy with sub-status
	if len(result.SubStatuses) != 1 {
		t.Errorf("Expected 1 sub-status, got %d", len(result.SubStatuses))
	}

	if result.SubStatuses[0].Component != "child" {
		t.Errorf("Expected child component, got %s", result.SubStatuses[0].Component)
	}
}

func TestFromComponentHealth(t *testing.T) {
	tests := []struct {
		name            string
		componentName   string
		componentHealth component.HealthStatus
		wantStatus      string
		wantMessage     string
	}{
		{
			name:          "healthy component",
			componentName: "test-component",
			componentHealth: component.HealthStatus{
				Healthy:    true,
				LastCheck:  time.Now(),
				ErrorCount: 0,
				Uptime:     time.Hour,
			},
			wantStatus:  "healthy",
			wantMessage: "Component healthy",
		},
		{
			name:          "unhealthy component with error",
			componentName: "failing-component",
			componentHealth: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Now(),
				ErrorCount: 3,
				LastError:  "connection failed",
				Uptime:     time.Minute,
			},
			wantStatus:  "unhealthy",
			wantMessage: "connection failed",
		},
		{
			name:          "unhealthy component without error message",
			componentName: "broken-component",
			componentHealth: component.HealthStatus{
				Healthy:    false,
				LastCheck:  time.Now(),
				ErrorCount: 1,
				Uptime:     time.Second,
			},
			wantStatus:  "unhealthy",
			wantMessage: "Component healthy", // fallback message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromComponentHealth(tt.componentName, tt.componentHealth)

			if result.Component != tt.componentName {
				t.Errorf("Expected component name %s, got %s", tt.componentName, result.Component)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("Expected status %s, got %s", tt.wantStatus, result.Status)
			}

			if result.Message != tt.wantMessage {
				t.Errorf("Expected message %s, got %s", tt.wantMessage, result.Message)
			}

			if result.Metrics == nil {
				t.Error("Expected metrics to be set")
			} else {
				if result.Metrics.Uptime != tt.componentHealth.Uptime {
					t.Errorf("Expected uptime %v, got %v", tt.componentHealth.Uptime, result.Metrics.Uptime)
				}

				if result.Metrics.ErrorCount != tt.componentHealth.ErrorCount {
					t.Errorf("Expected error count %d, got %d", tt.componentHealth.ErrorCount, result.Metrics.ErrorCount)
				}
			}

			if result.Timestamp.IsZero() {
				t.Error("Expected timestamp to be set")
			}
		})
	}
}
