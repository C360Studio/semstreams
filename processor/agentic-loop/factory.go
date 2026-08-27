package agenticloop

import (
	"github.com/c360studio/semstreams/component"
)

// Register registers the agentic-loop component factory with the registry
func Register(registry *component.Registry) error {
	if registry == nil {
		panic("registry cannot be nil")
	}

	registration := &component.Registration{
		Name:         "agentic-loop",
		Type:         "processor",
		Protocol:     "nats",
		Domain:       "agentic",
		Description:  "Orchestrates agentic loops with tool calls, state management, and trajectory tracking",
		Version:      "1.0.0",
		Schema:       schema,
		Factory:      NewComponent,
		Ports:        DeclarePorts,
		Dependencies: []string{component.DepModelRegistry},
	}

	return registry.RegisterFactory("agentic-loop", registration)
}
