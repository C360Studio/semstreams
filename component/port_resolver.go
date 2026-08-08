package component

import (
	"encoding/json"
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
	if field, err := validateDirectionRequirements(config, direction, binding); err != nil {
		return Port{}, PortFacts{}, portConfigError(def.Name, config.Kind(), field, err)
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

func validateDirectionRequirements(config Portable, direction Direction, binding portBinding) (string, error) {
	required := binding.requiredByDirection[direction]
	if len(required) == 0 {
		return "", nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "config", fmt.Errorf("marshal direction requirements: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return "config", fmt.Errorf("decode direction requirements: %w", err)
	}
	for _, field := range required {
		value, exists := fields[field]
		if !exists || directionFieldEmpty(value) {
			return field, fmt.Errorf("field %q is required for direction %q", field, direction)
		}
	}
	return "", nil
}

func directionFieldEmpty(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case nil:
		return true
	default:
		return false
	}
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
