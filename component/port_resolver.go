package component

import (
	"errors"
	"fmt"
	"strings"
)

func resolvePort(def PortDefinition, direction Direction) (Port, error) {
	port, _, err := resolvePortWithFacts(def, direction)
	return port, err
}

func resolvePortWithFacts(def PortDefinition, direction Direction) (Port, portFacts, error) {
	if strings.TrimSpace(def.Name) == "" {
		return Port{}, portFacts{}, portConfigError(def.Name, kindOf(def.Config), "name", errors.New("field \"name\" is required"))
	}
	if direction != DirectionInput && direction != DirectionOutput {
		return Port{}, portFacts{}, portConfigError(def.Name, kindOf(def.Config), "direction", fmt.Errorf("unknown direction %q", direction))
	}
	config, err := canonicalizePortable(def.Config)
	if err != nil {
		return Port{}, portFacts{}, portConfigError(def.Name, kindOf(def.Config), "config", err)
	}
	binding, err := bindingFor(config.Kind())
	if err != nil {
		return Port{}, portFacts{}, portConfigError(def.Name, config.Kind(), "kind", err)
	}
	if _, ok := binding.directions[direction]; !ok {
		return Port{}, portFacts{}, portConfigError(
			def.Name,
			config.Kind(),
			"direction",
			fmt.Errorf("kind %q does not allow direction %q", config.Kind(), direction),
		)
	}
	port := Port{
		Name:        def.Name,
		Direction:   direction,
		Required:    def.Required,
		Description: def.Description,
		Config:      config,
	}
	return port, binding.facts(config), nil
}

func factsForPort(port Port) (portFacts, error) {
	_, facts, err := resolvePortWithFacts(PortDefinition{
		Name:        port.Name,
		Required:    port.Required,
		Description: port.Description,
		Config:      port.Config,
	}, port.Direction)
	return facts, err
}
