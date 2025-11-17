package health

import (
	"testing"
	"time"
)

func TestNewHealthy(t *testing.T) {
	component := "test-component"
	message := "Everything is working"

	status := NewHealthy(component, message)

	if status.Component != component {
		t.Errorf("Expected component %s, got %s", component, status.Component)
	}

	if status.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %s", status.Status)
	}

	if status.Message != message {
		t.Errorf("Expected message %s, got %s", message, status.Message)
	}

	if status.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if !status.IsHealthy() {
		t.Error("Expected IsHealthy() to return true")
	}
}

func TestNewUnhealthy(t *testing.T) {
	component := "failing-component"
	message := "Connection lost"

	status := NewUnhealthy(component, message)

	if status.Component != component {
		t.Errorf("Expected component %s, got %s", component, status.Component)
	}

	if status.Status != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got %s", status.Status)
	}

	if status.Message != message {
		t.Errorf("Expected message %s, got %s", message, status.Message)
	}

	if status.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if !status.IsUnhealthy() {
		t.Error("Expected IsUnhealthy() to return true")
	}
}

func TestNewDegraded(t *testing.T) {
	component := "slow-component"
	message := "Performance degraded"

	status := NewDegraded(component, message)

	if status.Component != component {
		t.Errorf("Expected component %s, got %s", component, status.Component)
	}

	if status.Status != "degraded" {
		t.Errorf("Expected status 'degraded', got %s", status.Status)
	}

	if status.Message != message {
		t.Errorf("Expected message %s, got %s", message, status.Message)
	}

	if status.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if !status.IsDegraded() {
		t.Error("Expected IsDegraded() to return true")
	}
}

func TestAggregate(t *testing.T) {
	tests := []struct {
		name         string
		component    string
		subStatuses  []Status
		wantStatus   string
		wantMessage  string
		wantSubCount int
	}{
		{
			name:         "empty sub-statuses",
			component:    "system",
			subStatuses:  []Status{},
			wantStatus:   "healthy",
			wantMessage:  "No sub-components to aggregate",
			wantSubCount: 0,
		},
		{
			name:      "all healthy",
			component: "system",
			subStatuses: []Status{
				{Status: "healthy", Component: "comp1"},
				{Status: "healthy", Component: "comp2"},
			},
			wantStatus:   "healthy",
			wantMessage:  "All sub-components are healthy",
			wantSubCount: 2,
		},
		{
			name:      "one unhealthy",
			component: "system",
			subStatuses: []Status{
				{Status: "healthy", Component: "comp1"},
				{Status: "unhealthy", Component: "comp2"},
			},
			wantStatus:   "unhealthy",
			wantMessage:  "One or more sub-components are unhealthy",
			wantSubCount: 2,
		},
		{
			name:      "one degraded no unhealthy",
			component: "system",
			subStatuses: []Status{
				{Status: "healthy", Component: "comp1"},
				{Status: "degraded", Component: "comp2"},
			},
			wantStatus:   "degraded",
			wantMessage:  "One or more sub-components are degraded",
			wantSubCount: 2,
		},
		{
			name:      "degraded and unhealthy - unhealthy wins",
			component: "system",
			subStatuses: []Status{
				{Status: "degraded", Component: "comp1"},
				{Status: "unhealthy", Component: "comp2"},
			},
			wantStatus:   "unhealthy",
			wantMessage:  "One or more sub-components are unhealthy",
			wantSubCount: 2,
		},
		{
			name:      "multiple degraded",
			component: "system",
			subStatuses: []Status{
				{Status: "degraded", Component: "comp1"},
				{Status: "degraded", Component: "comp2"},
				{Status: "healthy", Component: "comp3"},
			},
			wantStatus:   "degraded",
			wantMessage:  "One or more sub-components are degraded",
			wantSubCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Aggregate(tt.component, tt.subStatuses)

			if result.Component != tt.component {
				t.Errorf("Expected component %s, got %s", tt.component, result.Component)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("Expected status %s, got %s", tt.wantStatus, result.Status)
			}

			if result.Message != tt.wantMessage {
				t.Errorf("Expected message %s, got %s", tt.wantMessage, result.Message)
			}

			if len(result.SubStatuses) != tt.wantSubCount {
				t.Errorf("Expected %d sub-statuses, got %d", tt.wantSubCount, len(result.SubStatuses))
			}

			if result.Timestamp.IsZero() {
				t.Error("Expected timestamp to be set")
			}

			// Verify sub-statuses are copied correctly
			for i, expected := range tt.subStatuses {
				if i < len(result.SubStatuses) {
					if result.SubStatuses[i].Component != expected.Component {
						t.Errorf("Sub-status %d: expected component %s, got %s",
							i, expected.Component, result.SubStatuses[i].Component)
					}
					if result.SubStatuses[i].Status != expected.Status {
						t.Errorf("Sub-status %d: expected status %s, got %s",
							i, expected.Status, result.SubStatuses[i].Status)
					}
				}
			}
		})
	}
}

func TestAggregate_DoesNotModifyInput(t *testing.T) {
	original := []Status{
		{Status: "healthy", Component: "comp1"},
		{Status: "unhealthy", Component: "comp2"},
	}

	// Make a copy for comparison
	originalCopy := make([]Status, len(original))
	copy(originalCopy, original)

	result := Aggregate("system", original)

	// Verify original slice is not modified
	if len(original) != len(originalCopy) {
		t.Error("Aggregate modified the length of input slice")
	}

	for i, orig := range original {
		if orig.Component != originalCopy[i].Component {
			t.Errorf("Aggregate modified input slice at index %d", i)
		}
		if orig.Status != originalCopy[i].Status {
			t.Errorf("Aggregate modified input slice at index %d", i)
		}
	}

	// Verify sub-statuses are independent copies
	if len(result.SubStatuses) > 0 {
		result.SubStatuses[0].Component = "modified"
		if original[0].Component == "modified" {
			t.Error("Modifying result sub-statuses should not affect input")
		}
	}
}

func TestHelperTimestamps(t *testing.T) {
	// Test that all helper functions set timestamps within a reasonable window
	before := time.Now()

	healthy := NewHealthy("comp", "msg")
	unhealthy := NewUnhealthy("comp", "msg")
	degraded := NewDegraded("comp", "msg")
	aggregated := Aggregate("system", []Status{healthy})

	after := time.Now()

	statuses := []Status{healthy, unhealthy, degraded, aggregated}
	for i, status := range statuses {
		if status.Timestamp.Before(before) || status.Timestamp.After(after) {
			t.Errorf("Status %d timestamp %v is outside expected range [%v, %v]",
				i, status.Timestamp, before, after)
		}
	}
}
