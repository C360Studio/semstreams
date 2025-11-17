package health

import "time"

// NewHealthy creates a new healthy status
func NewHealthy(component, message string) Status {
	return Status{
		Component: component,
		Healthy:   true,
		Status:    "healthy",
		Message:   message,
		Timestamp: time.Now(),
	}
}

// NewUnhealthy creates a new unhealthy status
func NewUnhealthy(component, message string) Status {
	return Status{
		Component: component,
		Healthy:   false,
		Status:    "unhealthy",
		Message:   message,
		Timestamp: time.Now(),
	}
}

// NewDegraded creates a new degraded status
func NewDegraded(component, message string) Status {
	return Status{
		Component: component,
		Healthy:   false,
		Status:    "degraded",
		Message:   message,
		Timestamp: time.Now(),
	}
}

// Aggregate creates a status by aggregating sub-statuses
// The aggregation rules are:
// - If all sub-statuses are healthy, the aggregate is healthy
// - If any sub-status is unhealthy, the aggregate is unhealthy
// - If no sub-status is unhealthy but at least one is degraded, the aggregate is degraded
func Aggregate(component string, subStatuses []Status) Status {
	if len(subStatuses) == 0 {
		return NewHealthy(component, "No sub-components to aggregate")
	}

	hasUnhealthy := false
	hasDegraded := false

	for _, sub := range subStatuses {
		if sub.IsUnhealthy() {
			hasUnhealthy = true
		} else if sub.IsDegraded() {
			hasDegraded = true
		}
	}

	var status Status
	if hasUnhealthy {
		status = NewUnhealthy(component, "One or more sub-components are unhealthy")
	} else if hasDegraded {
		status = NewDegraded(component, "One or more sub-components are degraded")
	} else {
		status = NewHealthy(component, "All sub-components are healthy")
	}

	status.SubStatuses = make([]Status, len(subStatuses))
	copy(status.SubStatuses, subStatuses)

	return status
}
