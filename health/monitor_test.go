package health

import (
	"sync"
	"testing"
	"time"
)

func TestNewMonitor(t *testing.T) {
	monitor := NewMonitor()

	if monitor == nil {
		t.Fatal("NewMonitor() returned nil")
	}

	if monitor.statuses == nil {
		t.Error("NewMonitor() should initialize statuses map")
	}

	if monitor.Count() != 0 {
		t.Errorf("New monitor should have 0 components, got %d", monitor.Count())
	}
}

func TestMonitor_Update(t *testing.T) {
	monitor := NewMonitor()

	status := Status{
		Component: "test-component",
		Status:    "healthy",
		Message:   "test message",
	}

	monitor.Update("test-component", status)

	retrieved, exists := monitor.Get("test-component")
	if !exists {
		t.Error("Component should exist after update")
	}

	if retrieved.Component != "test-component" {
		t.Errorf("Expected component name 'test-component', got %s", retrieved.Component)
	}

	if retrieved.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %s", retrieved.Status)
	}

	if retrieved.Timestamp.IsZero() {
		t.Error("Update should set timestamp if not provided")
	}
}

func TestMonitor_UpdateWithDifferentName(t *testing.T) {
	monitor := NewMonitor()

	// Update with a status that has a different component name
	status := Status{
		Component: "wrong-name",
		Status:    "healthy",
		Message:   "test message",
	}

	monitor.Update("correct-name", status)

	retrieved, exists := monitor.Get("correct-name")
	if !exists {
		t.Error("Component should exist with correct name")
	}

	// The component name should be corrected by Update
	if retrieved.Component != "correct-name" {
		t.Errorf("Expected component name 'correct-name', got %s", retrieved.Component)
	}
}

func TestMonitor_UpdateConvenienceMethods(t *testing.T) {
	monitor := NewMonitor()

	// Test UpdateHealthy
	monitor.UpdateHealthy("healthy-comp", "all good")
	healthyStatus, exists := monitor.Get("healthy-comp")
	if !exists || !healthyStatus.IsHealthy() {
		t.Error("UpdateHealthy should set component as healthy")
	}
	if healthyStatus.Message != "all good" {
		t.Errorf("Expected message 'all good', got %s", healthyStatus.Message)
	}

	// Test UpdateUnhealthy
	monitor.UpdateUnhealthy("unhealthy-comp", "something wrong")
	unhealthyStatus, exists := monitor.Get("unhealthy-comp")
	if !exists || !unhealthyStatus.IsUnhealthy() {
		t.Error("UpdateUnhealthy should set component as unhealthy")
	}
	if unhealthyStatus.Message != "something wrong" {
		t.Errorf("Expected message 'something wrong', got %s", unhealthyStatus.Message)
	}

	// Test UpdateDegraded
	monitor.UpdateDegraded("degraded-comp", "performance issues")
	degradedStatus, exists := monitor.Get("degraded-comp")
	if !exists || !degradedStatus.IsDegraded() {
		t.Error("UpdateDegraded should set component as degraded")
	}
	if degradedStatus.Message != "performance issues" {
		t.Errorf("Expected message 'performance issues', got %s", degradedStatus.Message)
	}
}

func TestMonitor_Get(t *testing.T) {
	monitor := NewMonitor()

	// Test getting non-existent component
	_, exists := monitor.Get("non-existent")
	if exists {
		t.Error("Getting non-existent component should return false")
	}

	// Add a component and test getting it
	monitor.UpdateHealthy("test", "message")
	status, exists := monitor.Get("test")
	if !exists {
		t.Error("Getting existing component should return true")
	}
	if status.Component != "test" {
		t.Errorf("Expected component 'test', got %s", status.Component)
	}
}

func TestMonitor_GetAll(t *testing.T) {
	monitor := NewMonitor()

	// Test empty monitor
	all := monitor.GetAll()
	if len(all) != 0 {
		t.Errorf("Empty monitor should return empty map, got %d items", len(all))
	}

	// Add multiple components
	monitor.UpdateHealthy("comp1", "msg1")
	monitor.UpdateUnhealthy("comp2", "msg2")
	monitor.UpdateDegraded("comp3", "msg3")

	all = monitor.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected 3 components, got %d", len(all))
	}

	// Verify all components are present
	for _, name := range []string{"comp1", "comp2", "comp3"} {
		if _, exists := all[name]; !exists {
			t.Errorf("Component %s should be in GetAll result", name)
		}
	}

	// Test that returned map is a copy (modifying it shouldn't affect monitor)
	all["comp1"] = Status{Component: "modified"}
	original, _ := monitor.Get("comp1")
	if original.Component == "modified" {
		t.Error("GetAll should return a copy, not reference to internal data")
	}
}

func TestMonitor_Remove(t *testing.T) {
	monitor := NewMonitor()

	// Remove from empty monitor (should not panic)
	monitor.Remove("non-existent")

	// Add component, then remove it
	monitor.UpdateHealthy("test", "message")
	if monitor.Count() != 1 {
		t.Error("Should have 1 component after adding")
	}

	monitor.Remove("test")
	if monitor.Count() != 0 {
		t.Error("Should have 0 components after removing")
	}

	_, exists := monitor.Get("test")
	if exists {
		t.Error("Component should not exist after removal")
	}
}

func TestMonitor_AggregateHealth(t *testing.T) {
	monitor := NewMonitor()

	// Test empty monitor
	aggregate := monitor.AggregateHealth("system")
	if !aggregate.IsHealthy() {
		t.Error("Empty monitor should aggregate as healthy")
	}
	if aggregate.Component != "system" {
		t.Errorf("Expected component 'system', got %s", aggregate.Component)
	}

	// Add healthy components
	monitor.UpdateHealthy("comp1", "msg1")
	monitor.UpdateHealthy("comp2", "msg2")
	aggregate = monitor.AggregateHealth("system")
	if !aggregate.IsHealthy() {
		t.Error("All healthy components should aggregate as healthy")
	}

	// Add unhealthy component
	monitor.UpdateUnhealthy("comp3", "error")
	aggregate = monitor.AggregateHealth("system")
	if !aggregate.IsUnhealthy() {
		t.Error("Should aggregate as unhealthy when any component is unhealthy")
	}

	// Remove unhealthy, add degraded
	monitor.Remove("comp3")
	monitor.UpdateDegraded("comp4", "slow")
	aggregate = monitor.AggregateHealth("system")
	if !aggregate.IsDegraded() {
		t.Error("Should aggregate as degraded when no unhealthy but some degraded")
	}
}

func TestMonitor_ListComponents(t *testing.T) {
	monitor := NewMonitor()

	// Test empty monitor
	components := monitor.ListComponents()
	if len(components) != 0 {
		t.Errorf("Empty monitor should return empty list, got %d items", len(components))
	}

	// Add components
	monitor.UpdateHealthy("comp1", "msg1")
	monitor.UpdateUnhealthy("comp2", "msg2")

	components = monitor.ListComponents()
	if len(components) != 2 {
		t.Errorf("Expected 2 components, got %d", len(components))
	}

	// Verify all expected components are in the list
	componentMap := make(map[string]bool)
	for _, comp := range components {
		componentMap[comp] = true
	}

	for _, expected := range []string{"comp1", "comp2"} {
		if !componentMap[expected] {
			t.Errorf("Component %s should be in list", expected)
		}
	}
}

func TestMonitor_Count(t *testing.T) {
	monitor := NewMonitor()

	if monitor.Count() != 0 {
		t.Errorf("New monitor should have count 0, got %d", monitor.Count())
	}

	monitor.UpdateHealthy("comp1", "msg")
	if monitor.Count() != 1 {
		t.Errorf("Expected count 1, got %d", monitor.Count())
	}

	monitor.UpdateHealthy("comp2", "msg")
	if monitor.Count() != 2 {
		t.Errorf("Expected count 2, got %d", monitor.Count())
	}

	monitor.Remove("comp1")
	if monitor.Count() != 1 {
		t.Errorf("Expected count 1 after removal, got %d", monitor.Count())
	}
}

func TestMonitor_Clear(t *testing.T) {
	monitor := NewMonitor()

	// Add multiple components
	monitor.UpdateHealthy("comp1", "msg1")
	monitor.UpdateUnhealthy("comp2", "msg2")
	monitor.UpdateDegraded("comp3", "msg3")

	if monitor.Count() != 3 {
		t.Errorf("Expected 3 components before clear, got %d", monitor.Count())
	}

	monitor.Clear()

	if monitor.Count() != 0 {
		t.Errorf("Expected 0 components after clear, got %d", monitor.Count())
	}

	all := monitor.GetAll()
	if len(all) != 0 {
		t.Errorf("GetAll should return empty map after clear, got %d items", len(all))
	}
}

func TestMonitor_ConcurrentAccess(t *testing.T) {
	monitor := NewMonitor()
	numGoroutines := 10
	numOperationsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines performing concurrent operations
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()

			for j := 0; j < numOperationsPerGoroutine; j++ {
				componentName := "comp"

				// Mix of operations
				switch j % 4 {
				case 0:
					monitor.UpdateHealthy(componentName, "healthy")
				case 1:
					monitor.UpdateUnhealthy(componentName, "unhealthy")
				case 2:
					_, _ = monitor.Get(componentName)
				case 3:
					_ = monitor.GetAll()
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify monitor is still functional
	monitor.UpdateHealthy("final-test", "test message")
	status, exists := monitor.Get("final-test")
	if !exists || status.Component != "final-test" {
		t.Error("Monitor should still be functional after concurrent access")
	}
}

func TestMonitor_ConcurrentAggregation(t *testing.T) {
	monitor := NewMonitor()
	numGoroutines := 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start goroutines that continuously aggregate while others modify
	for i := 0; i < numGoroutines; i++ {
		if i == 0 {
			// One goroutine continuously aggregates
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_ = monitor.AggregateHealth("system")
					time.Sleep(time.Microsecond)
				}
			}()
		} else {
			// Other goroutines add/remove components
			go func(_ int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					componentName := "comp"
					if j%2 == 0 {
						monitor.UpdateHealthy(componentName, "msg")
					} else {
						monitor.Remove(componentName)
					}
					time.Sleep(time.Microsecond)
				}
			}(i)
		}
	}

	wg.Wait()

	// Final aggregation should work without panic
	aggregate := monitor.AggregateHealth("final-system")
	if aggregate.Component != "final-system" {
		t.Error("Final aggregation should work correctly")
	}
}
