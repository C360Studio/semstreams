// Package component provides port configuration and management for component connections.
package component

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ResolveSubject returns the configured NATS subject for one uniquely named
// port with a trailing wildcard replaced by suffix.
func ResolveSubject(ports []PortDefinition, portName, suffix string) (string, error) {
	var subject string
	found := false
	for _, definition := range ports {
		if definition.Name != portName {
			continue
		}
		if found {
			return "", portConfigError(portName, kindOf(definition.Config), "name", fmt.Errorf("duplicate port name %q", portName))
		}
		found = true
		canonical, err := canonicalizePortable(definition.Config)
		if err != nil {
			return "", portConfigError(portName, kindOf(definition.Config), "config", err)
		}
		binding := portBindingTable[canonical.Kind()]
		facts := binding.facts(canonical)
		if len(facts.natsSubjects) == 0 {
			return "", portConfigError(portName, canonical.Kind(), "subject", errors.New("port does not declare a NATS subject"))
		}
		subject = facts.natsSubjects[0]
	}
	if !found {
		return "", portConfigError(portName, "", "name", fmt.Errorf("port name %q not found", portName))
	}
	return appendSubjectSuffix(subject, suffix), nil
}

func appendSubjectSuffix(subject, suffix string) string {
	switch {
	case strings.HasSuffix(subject, ".*"):
		return strings.TrimSuffix(subject, "*") + suffix
	case strings.HasSuffix(subject, ".>"):
		return strings.TrimSuffix(subject, ">") + suffix
	default:
		return subject + "." + suffix
	}
}

// PortDefinition is a configuration-shaped semantic port declaration.
type PortDefinition struct {
	Name        string   `json:"name" schema:"readonly,type:string,description:Port identifier"`
	Required    bool     `json:"required,omitempty" schema:"readonly,type:bool,description:Whether port connection is required"`
	Description string   `json:"description,omitempty" schema:"readonly,type:string,description:Human-readable port description"`
	Config      Portable `json:"config" schema:"editable,type:object,description:Typed semantic port configuration"`
}

// MarshalJSON writes the canonical common port envelope.
func (p PortDefinition) MarshalJSON() ([]byte, error) {
	config, err := marshalPortable(p.Config)
	if err != nil {
		return nil, portConfigError(p.Name, kindOf(p.Config), "config", err)
	}
	wire := struct {
		Name        string          `json:"name"`
		Required    bool            `json:"required,omitempty"`
		Description string          `json:"description,omitempty"`
		Config      json.RawMessage `json:"config"`
	}{
		Name:        p.Name,
		Required:    p.Required,
		Description: p.Description,
		Config:      config,
	}
	return json.Marshal(wire)
}

// UnmarshalJSON strictly decodes the canonical common port envelope.
func (p *PortDefinition) UnmarshalJSON(data []byte) error {
	var wire struct {
		Name        string          `json:"name"`
		Required    bool            `json:"required,omitempty"`
		Description string          `json:"description,omitempty"`
		Config      json.RawMessage `json:"config"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return portConfigError(wire.Name, rawPortableKind(wire.Config), "definition", err)
	}
	if strings.TrimSpace(wire.Name) == "" {
		return portConfigError(wire.Name, rawPortableKind(wire.Config), "name", errors.New("field \"name\" is required"))
	}
	config, err := decodePortable(wire.Config)
	if err != nil {
		return portConfigError(wire.Name, rawPortableKind(wire.Config), "config", err)
	}
	*p = PortDefinition{
		Name:        wire.Name,
		Required:    wire.Required,
		Description: wire.Description,
		Config:      config,
	}
	return nil
}

// PortConfig groups semantic declarations by data-flow direction.
type PortConfig struct {
	Inputs  []PortDefinition `json:"inputs,omitempty"`
	Outputs []PortDefinition `json:"outputs,omitempty"`
}

// UnmarshalJSON rejects unknown top-level lanes such as the retired kv_write lane.
func (p *PortConfig) UnmarshalJSON(data []byte) error {
	type portConfigWire struct {
		Inputs  []PortDefinition `json:"inputs,omitempty"`
		Outputs []PortDefinition `json:"outputs,omitempty"`
	}
	var wire portConfigWire
	if err := decodeStrict(data, &wire); err != nil {
		return portConfigError("", "", "ports", err)
	}
	var resolvedInputs, resolvedOutputs []PortDefinition
	for _, lane := range []struct {
		direction   Direction
		definitions []PortDefinition
		resolved    *[]PortDefinition
	}{
		{direction: DirectionInput, definitions: wire.Inputs, resolved: &resolvedInputs},
		{direction: DirectionOutput, definitions: wire.Outputs, resolved: &resolvedOutputs},
	} {
		names := make(map[string]struct{}, len(lane.definitions))
		normalized := make([]PortDefinition, len(lane.definitions))
		for index, definition := range lane.definitions {
			if _, duplicate := names[definition.Name]; duplicate {
				return portConfigError(definition.Name, kindOf(definition.Config), "name",
					fmt.Errorf("duplicate port name %q in %s lane", definition.Name, lane.direction))
			}
			names[definition.Name] = struct{}{}
			port, err := definition.Resolve(lane.direction)
			if err != nil {
				return err
			}
			normalized[index] = definitionFromPort(port)
		}
		*lane.resolved = normalized
	}
	p.Inputs = resolvedInputs
	p.Outputs = resolvedOutputs
	return nil
}

// MergePortConfig applies complete named replacements to default declarations.
// Inputs and outputs are independent, ordering is stable, and returned data is cloned.
func MergePortConfig(defaults, overrides PortConfig) (PortConfig, error) {
	inputs, err := mergePortDirection(defaults.Inputs, overrides.Inputs, DirectionInput)
	if err != nil {
		return PortConfig{}, err
	}
	outputs, err := mergePortDirection(defaults.Outputs, overrides.Outputs, DirectionOutput)
	if err != nil {
		return PortConfig{}, err
	}
	return PortConfig{Inputs: inputs, Outputs: outputs}, nil
}

func mergePortDirection(defaults, overrides []PortDefinition, direction Direction) ([]PortDefinition, error) {
	defaultIndexes := make(map[string]int, len(defaults))
	result := make([]PortDefinition, len(defaults))
	for index, definition := range defaults {
		if _, duplicate := defaultIndexes[definition.Name]; duplicate {
			return nil, portConfigError(definition.Name, kindOf(definition.Config), "name", fmt.Errorf("duplicate default port name %q", definition.Name))
		}
		port, err := definition.Resolve(direction)
		if err != nil {
			return nil, err
		}
		defaultIndexes[definition.Name] = index
		result[index] = definitionFromPort(port)
	}

	seenOverrides := make(map[string]struct{}, len(overrides))
	for _, override := range overrides {
		if _, duplicate := seenOverrides[override.Name]; duplicate {
			return nil, portConfigError(override.Name, kindOf(override.Config), "name", fmt.Errorf("duplicate override port name %q", override.Name))
		}
		seenOverrides[override.Name] = struct{}{}
		index, found := defaultIndexes[override.Name]
		if !found {
			return nil, portConfigError(override.Name, kindOf(override.Config), "name", fmt.Errorf("unknown override port name %q for %s ports", override.Name, direction))
		}
		port, err := override.Resolve(direction)
		if err != nil {
			return nil, err
		}
		if port.Config.Kind() != result[index].Config.Kind() {
			return nil, portConfigError(
				override.Name,
				port.Config.Kind(),
				"kind",
				fmt.Errorf("override kind %q does not match default kind %q", port.Config.Kind(), result[index].Config.Kind()),
			)
		}
		result[index] = definitionFromPort(port)
	}
	return result, nil
}

func definitionFromPort(port Port) PortDefinition {
	return PortDefinition{
		Name:        port.Name,
		Required:    port.Required,
		Description: port.Description,
		Config:      port.Config,
	}
}
