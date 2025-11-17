// Package service provides service registration
package service

import "fmt"

// RegisterAll registers all built-in services with the registry
// Future: Can be split into Registercore (), RegisterMonitoring(), etc.
func RegisterAll(registry *Registry) error {
	services := map[string]Constructor{
		"metrics":           NewMetrics,
		"message-logger":    NewMessageLoggerService,
		"component-manager": NewComponentManager,
		"flow-builder":      NewFlowServiceFromConfig,
	}

	for name, constructor := range services {
		if err := registry.Register(name, constructor); err != nil {
			return fmt.Errorf("register %s: %w", name, err)
		}
	}
	return nil
}
