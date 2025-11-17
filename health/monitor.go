package health

import (
	"sync"
	"time"
)

// Monitor tracks health of multiple components in a thread-safe manner
type Monitor struct {
	mu       sync.RWMutex
	statuses map[string]Status
}

// NewMonitor creates a new health monitor
func NewMonitor() *Monitor {
	return &Monitor{
		statuses: make(map[string]Status),
	}
}

// Update updates the health status for a named component
func (m *Monitor) Update(name string, status Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure the status has the correct component name and timestamp
	status.Component = name
	if status.Timestamp.IsZero() {
		status.Timestamp = time.Now()
	}

	m.statuses[name] = status
}

// UpdateHealthy is a convenience method to update a component as healthy
func (m *Monitor) UpdateHealthy(name, message string) {
	m.Update(name, NewHealthy(name, message))
}

// UpdateUnhealthy is a convenience method to update a component as unhealthy
func (m *Monitor) UpdateUnhealthy(name, message string) {
	m.Update(name, NewUnhealthy(name, message))
}

// UpdateDegraded is a convenience method to update a component as degraded
func (m *Monitor) UpdateDegraded(name, message string) {
	m.Update(name, NewDegraded(name, message))
}

// Get retrieves the health status for a named component
func (m *Monitor) Get(name string) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.statuses[name]
	return status, exists
}

// GetAll returns a copy of all current health statuses
func (m *Monitor) GetAll() map[string]Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Status, len(m.statuses))
	for name, status := range m.statuses {
		result[name] = status
	}
	return result
}

// Remove removes a component from monitoring
func (m *Monitor) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.statuses, name)
}

// AggregateHealth returns an aggregated health status for the entire system
func (m *Monitor) AggregateHealth(systemName string) Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subStatuses := make([]Status, 0, len(m.statuses))
	for _, status := range m.statuses {
		subStatuses = append(subStatuses, status)
	}

	return Aggregate(systemName, subStatuses)
}

// ListComponents returns a list of all component names being monitored
func (m *Monitor) ListComponents() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.statuses))
	for name := range m.statuses {
		names = append(names, name)
	}
	return names
}

// Count returns the number of components being monitored
func (m *Monitor) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.statuses)
}

// Clear removes all components from monitoring
func (m *Monitor) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statuses = make(map[string]Status)
}
