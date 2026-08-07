package component

import (
	"errors"
	"fmt"
	"strings"
)

// Resolve validates and resolves a configuration declaration for one direction.
func (d PortDefinition) Resolve(direction Direction) (Port, error) {
	port, _, err := resolveAndProjectPort(d, direction)
	return port, err
}

func resolveAndProjectPort(def PortDefinition, direction Direction) (Port, PortFacts, error) {
	if strings.TrimSpace(def.Name) == "" {
		return Port{}, PortFacts{}, portConfigError(def.Name, kindOf(def.Config), "name", errors.New("field \"name\" is required"))
	}
	if direction != DirectionInput && direction != DirectionOutput {
		return Port{}, PortFacts{}, portConfigError(def.Name, kindOf(def.Config), "direction", fmt.Errorf("unknown direction %q", direction))
	}
	config, err := canonicalizePortable(def.Config)
	if err != nil {
		return Port{}, PortFacts{}, portConfigError(def.Name, kindOf(def.Config), "config", err)
	}
	binding, err := bindingFor(config.Kind())
	if err != nil {
		return Port{}, PortFacts{}, portConfigError(def.Name, config.Kind(), "kind", err)
	}
	if _, ok := binding.directions[direction]; !ok {
		return Port{}, PortFacts{}, portConfigError(
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

// Facts revalidates the current Port value and returns its immutable semantic projection.
func (p Port) Facts() (PortFacts, error) {
	_, facts, err := resolveAndProjectPort(PortDefinition{
		Name:        p.Name,
		Required:    p.Required,
		Description: p.Description,
		Config:      p.Config,
	}, p.Direction)
	return facts, err
}
