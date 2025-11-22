package service

// Runtime health endpoint for Flow Builder UI.
//
// This file implements the GET /flowbuilder/flows/{id}/runtime/health endpoint
// which provides component-level health status with timing information for
// runtime debugging and monitoring.
//
// The endpoint returns health status for all components in a flow including:
//   - Component health status (healthy, degraded, error)
//   - Start time (when component was started)
//   - Last activity time (last message processed)
//   - Uptime in seconds
//   - Overall health summary with counts
//
// Response time target: < 200ms
// Poll interval: UI polls every 5s for health updates
//
// The endpoint integrates with ComponentManager to access component health
// and timing information without breaking the existing health system.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
)

// RuntimeHealthResponse represents the JSON response for runtime health
type RuntimeHealthResponse struct {
	Timestamp  time.Time         `json:"timestamp"`
	Overall    OverallHealth     `json:"overall"`
	Components []ComponentHealth `json:"components"`
}

// OverallHealth provides aggregate health status and counts
type OverallHealth struct {
	Status        string `json:"status"` // "healthy", "degraded", "error"
	RunningCount  int    `json:"running_count"`
	DegradedCount int    `json:"degraded_count"`
	ErrorCount    int    `json:"error_count"`
}

// ComponentHealth represents health and timing for a single component
type ComponentHealth struct {
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Status        string     `json:"status"` // "running", "degraded", "error", "stopped"
	Healthy       bool       `json:"healthy"`
	Message       string     `json:"message"`
	StartTime     *time.Time `json:"start_time"`     // ISO 8601 timestamp, null if not started
	LastActivity  *time.Time `json:"last_activity"`  // ISO 8601 timestamp, null if no activity
	UptimeSeconds *float64   `json:"uptime_seconds"` // null if not started
	Details       any        `json:"details"`        // Additional details for degraded/error states
}

// handleRuntimeHealth handles GET /flows/{id}/runtime/health
// Returns health status for all components in the specified flow
func (fs *FlowService) handleRuntimeHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	flowID := r.PathValue("id")

	// Set timeout for health check to ensure fast response
	ctx, cancel := context.WithTimeout(ctx, 180*time.Millisecond)
	defer cancel()

	// Get the flow definition to know what components to query
	flow, err := fs.flowStore.Get(ctx, flowID)
	if err != nil {
		fs.logger.Error("Failed to get flow for health", "flow_id", flowID, "error", err)
		fs.writeJSONError(w, "Flow not found", http.StatusNotFound)
		return
	}

	// Build component list from flow nodes
	componentNames := make([]string, 0, len(flow.Nodes))
	componentTypes := make(map[string]string)
	for _, node := range flow.Nodes {
		componentNames = append(componentNames, node.Name)
		componentTypes[node.Name] = node.Type
	}

	// Get component health from ComponentManager
	response, err := fs.getComponentsHealth(ctx, componentNames, componentTypes)
	if err != nil {
		fs.logger.Error("Failed to get component health", "flow_id", flowID, "error", err)
		// Return partial response with error status
		response = &RuntimeHealthResponse{
			Timestamp: time.Now().UTC(),
			Overall: OverallHealth{
				Status:        "error",
				RunningCount:  0,
				DegradedCount: 0,
				ErrorCount:    len(componentNames),
			},
			Components: make([]ComponentHealth, 0),
		}
	}

	response.Timestamp = time.Now().UTC()

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fs.logger.Error("Failed to encode health response", "error", err)
	}
}

// getComponentsHealth retrieves health status for specified components
func (fs *FlowService) getComponentsHealth(
	_ context.Context,
	componentNames []string,
	componentTypes map[string]string,
) (*RuntimeHealthResponse, error) {
	// Get ComponentManager from service manager
	componentManager, err := fs.getComponentManager()
	if err != nil {
		return nil, errors.WrapTransient(err, "FlowService", "getComponentsHealth", "get component manager")
	}

	// Get all managed components with their state
	managedComponents := componentManager.GetManagedComponents()

	// Build component health list and collect counts
	components, runningCount, degradedCount, errorCount := fs.buildComponentHealthList(
		componentNames,
		componentTypes,
		managedComponents,
	)

	// Calculate overall status based on counts
	overallStatus := fs.calculateOverallStatus(errorCount, degradedCount)

	return &RuntimeHealthResponse{
		Overall: OverallHealth{
			Status:        overallStatus,
			RunningCount:  runningCount,
			DegradedCount: degradedCount,
			ErrorCount:    errorCount,
		},
		Components: components,
	}, nil
}

// buildComponentHealthList builds the component health list and returns status counts
func (fs *FlowService) buildComponentHealthList(
	componentNames []string,
	componentTypes map[string]string,
	managedComponents map[string]*component.ManagedComponent,
) ([]ComponentHealth, int, int, int) {
	components := make([]ComponentHealth, 0, len(componentNames))
	runningCount := 0
	degradedCount := 0
	errorCount := 0

	for _, name := range componentNames {
		mc, exists := managedComponents[name]
		if !exists {
			// Component not found - likely not started yet
			components = append(components, ComponentHealth{
				Name:    name,
				Type:    componentTypes[name],
				Status:  "stopped",
				Healthy: false,
				Message: "Component not started",
			})
			errorCount++
			continue
		}

		// Get component health status
		var healthStatus component.HealthStatus
		if mc.Component != nil {
			healthStatus = mc.Component.Health()
		}

		// Calculate timing information
		startTime, lastActivity, uptimeSeconds := fs.calculateComponentTiming(mc, healthStatus)

		// Determine status and healthy state
		status, healthy, message := fs.determineComponentStatus(mc, healthStatus)

		// Build component health
		compHealth := ComponentHealth{
			Name:          name,
			Type:          componentTypes[name],
			Status:        status,
			Healthy:       healthy,
			Message:       message,
			StartTime:     startTime,
			LastActivity:  lastActivity,
			UptimeSeconds: uptimeSeconds,
		}

		// Add details for degraded/error states
		if !healthy && healthStatus.ErrorCount > 0 {
			compHealth.Details = map[string]any{
				"error_count": healthStatus.ErrorCount,
			}
		}

		components = append(components, compHealth)

		// Update counts
		if status == "running" && healthy {
			runningCount++
		} else if status == "degraded" {
			degradedCount++
		} else if status == "error" || !healthy {
			errorCount++
		}
	}

	return components, runningCount, degradedCount, errorCount
}

// calculateComponentTiming calculates start time, last activity, and uptime for a component
func (fs *FlowService) calculateComponentTiming(
	mc *component.ManagedComponent,
	healthStatus component.HealthStatus,
) (*time.Time, *time.Time, *float64) {
	var startTime *time.Time
	var lastActivity *time.Time
	var uptimeSeconds *float64

	// Get start time from managed component state
	// If component is started, calculate start time from uptime
	if mc.State == component.StateStarted && healthStatus.Uptime > 0 {
		st := time.Now().Add(-time.Duration(healthStatus.Uptime)).UTC()
		startTime = &st

		// Calculate uptime in seconds
		uptime := healthStatus.Uptime.Seconds()
		uptimeSeconds = &uptime
	}

	// Get last activity from health status
	if !healthStatus.LastCheck.IsZero() {
		la := healthStatus.LastCheck.UTC()
		lastActivity = &la
	}

	return startTime, lastActivity, uptimeSeconds
}

// determineComponentStatus determines the status string, healthy flag, and message from component state
func (fs *FlowService) determineComponentStatus(
	mc *component.ManagedComponent,
	healthStatus component.HealthStatus,
) (status string, healthy bool, message string) {
	status = "stopped"
	healthy = false
	message = healthStatus.LastError

	switch mc.State {
	case component.StateStarted:
		if healthStatus.Healthy {
			status = "running"
			healthy = true
			if message == "" {
				message = "Component running normally"
			}
		} else {
			status = "degraded"
			healthy = false
			if message == "" {
				message = "Component unhealthy"
			}
		}
	case component.StateFailed:
		status = "error"
		healthy = false
		if message == "" && mc.LastError != nil {
			message = mc.LastError.Error()
		}
	case component.StateInitialized:
		status = "stopped"
		healthy = false
		message = "Component initialized but not started"
	default:
		status = "stopped"
		healthy = false
		message = "Component not running"
	}

	return status, healthy, message
}

// calculateOverallStatus calculates overall health status from error and degraded counts
func (fs *FlowService) calculateOverallStatus(errorCount, degradedCount int) string {
	if errorCount > 0 {
		return "error"
	}
	if degradedCount > 0 {
		return "degraded"
	}
	return "healthy"
}

// getComponentManager retrieves the ComponentManager service
func (fs *FlowService) getComponentManager() (*ComponentManager, error) {
	// Get service manager from dependencies
	if fs.serviceMgr == nil {
		return nil, errors.WrapFatal(
			fmt.Errorf("service manager not available"),
			"FlowService",
			"getComponentManager",
			"check service manager",
		)
	}

	// Get ComponentManager from service manager
	svc, exists := fs.serviceMgr.GetService("component-manager")
	if !exists {
		return nil, errors.WrapFatal(
			fmt.Errorf("component-manager service not found"),
			"FlowService",
			"getComponentManager",
			"get service",
		)
	}

	// Type assert to ComponentManager
	cm, ok := svc.(*ComponentManager)
	if !ok {
		return nil, errors.WrapFatal(
			fmt.Errorf("service is not a ComponentManager: %T", svc),
			"FlowService",
			"getComponentManager",
			"type assertion",
		)
	}

	return cm, nil
}
