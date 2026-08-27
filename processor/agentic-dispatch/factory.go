package agenticdispatch

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// schema is the configuration schema for agentic-dispatch, generated from Config struct tags
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Register registers the router component factory with the registry
func Register(registry *component.Registry) error {
	if registry == nil {
		panic("registry cannot be nil")
	}

	registration := &component.Registration{
		Name:         "agentic-dispatch",
		Type:         "processor",
		Protocol:     "jetstream",
		Domain:       "agentic",
		Description:  "Routes user messages to agentic loops with command parsing and permissions",
		Version:      "1.0.0",
		Schema:       schema,
		Factory:      NewComponent,
		Ports:        DeclarePorts,
		Dependencies: []string{component.DepModelRegistry},
	}

	return registry.RegisterFactory("agentic-dispatch", registration)
}

// buildDefaultInputPorts returns the default input ports
func buildDefaultInputPorts() ([]component.Port, error) {
	defaultConfig := DefaultConfig()
	ports := make([]component.Port, 0, len(defaultConfig.Ports.Inputs))

	for _, def := range defaultConfig.Ports.Inputs {
		port, err := def.Resolve(component.DirectionInput)
		if err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}

	return ports, nil
}

// buildDefaultOutputPorts returns the default output ports
func buildDefaultOutputPorts() ([]component.Port, error) {
	defaultConfig := DefaultConfig()
	ports := make([]component.Port, 0, len(defaultConfig.Ports.Outputs))

	for _, def := range defaultConfig.Ports.Outputs {
		port, err := def.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}

	return ports, nil
}
