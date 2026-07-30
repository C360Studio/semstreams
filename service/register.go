// Package service provides service registration
package service

import "fmt"

// RegisterAll registers all built-in services with the registry
// Future: Can be split into Registercore (), RegisterMonitoring(), etc.
func RegisterAll(registry *Registry) error {
	services := map[string]Constructor{
		"metrics":           NewMetrics,
		"message-logger":    NewMessageLoggerService,
		"log-forwarder":     NewLogForwarderService,
		"metrics-forwarder": NewMetricsForwarderService,
		"component-manager": NewComponentManager,
		"flow-builder":      NewFlowServiceFromConfig,
		"heartbeat":         NewHeartbeatService,
		// Account storage observability. Opt-in and report-only: a deployment
		// that does not name it in its `services` block never constructs it.
		StorageObservabilityServiceName: NewStorageObservabilityService,
	}

	for name, constructor := range services {
		if err := registry.Register(name, constructor); err != nil {
			return fmt.Errorf("register %s: %w", name, err)
		}
	}
	return nil
}
