package sensorml

import (
	"encoding/json"
	"fmt"
)

// UnmarshalProcess decodes a SensorML JSON object into the
// appropriate concrete Process type, switching on the type
// discriminator per OGC 12-000r2. Returns the concrete value
// (not a pointer) so callers can type-switch directly:
//
//	process, err := sensorml.UnmarshalProcess(data)
//	switch p := process.(type) {
//	case *sensorml.PhysicalSystem:    …
//	case *sensorml.PhysicalComponent: …
//	}
//
// Returns an error if the JSON is malformed or if the type
// field names a kind this package does not handle.
//
// The function returns a [Process] interface value rather than
// concrete types because composite kinds (PhysicalSystem,
// AggregateProcess) carry children of arbitrary process kinds
// — their JSON shape is heterogeneous and must dispatch
// through this same function.
func UnmarshalProcess(data []byte) (Process, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("sensorml: probe type field: %w", err)
	}
	if probe.Type == "" {
		return nil, fmt.Errorf("sensorml: object missing required %q field", "type")
	}

	switch probe.Type {
	case TypePhysicalSystem:
		return unmarshalPhysicalSystem(data)
	case TypePhysicalComponent:
		var p PhysicalComponent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("sensorml: decode PhysicalComponent: %w", err)
		}
		return &p, nil
	case TypeSimpleProcess:
		var p SimpleProcess
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("sensorml: decode SimpleProcess: %w", err)
		}
		return &p, nil
	case TypeAggregateProcess:
		return unmarshalAggregateProcess(data)
	default:
		return nil, fmt.Errorf("sensorml: unknown process type %q", probe.Type)
	}
}

// unmarshalPhysicalSystem handles the composite-with-children
// shape: each entry in the "components" array must be dispatched
// through UnmarshalProcess because it carries its own type
// discriminator (PhysicalComponent or nested PhysicalSystem).
func unmarshalPhysicalSystem(data []byte) (*PhysicalSystem, error) {
	var envelope struct {
		AbstractProcess
		AttachedTo  *Reference        `json:"attachedTo,omitempty"`
		Components  []json.RawMessage `json:"components,omitempty"`
		Connections ConnectionList    `json:"connections,omitempty"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("sensorml: decode PhysicalSystem envelope: %w", err)
	}
	sys := &PhysicalSystem{
		AbstractProcess: envelope.AbstractProcess,
		AttachedTo:      envelope.AttachedTo,
		Connections:     envelope.Connections,
	}
	for i, raw := range envelope.Components {
		child, err := UnmarshalProcess(raw)
		if err != nil {
			return nil, fmt.Errorf("sensorml: components[%d]: %w", i, err)
		}
		sys.Components = append(sys.Components, child)
	}
	return sys, nil
}

// unmarshalAggregateProcess handles the composite SimpleProcess /
// PhysicalComponent / AggregateProcess children list. Same
// polymorphic dispatch as PhysicalSystem.
func unmarshalAggregateProcess(data []byte) (*AggregateProcess, error) {
	var envelope struct {
		AbstractProcess
		Components  []json.RawMessage `json:"components,omitempty"`
		Connections ConnectionList    `json:"connections,omitempty"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("sensorml: decode AggregateProcess envelope: %w", err)
	}
	agg := &AggregateProcess{
		AbstractProcess: envelope.AbstractProcess,
		Connections:     envelope.Connections,
	}
	for i, raw := range envelope.Components {
		child, err := UnmarshalProcess(raw)
		if err != nil {
			return nil, fmt.Errorf("sensorml: components[%d]: %w", i, err)
		}
		agg.Components = append(agg.Components, child)
	}
	return agg, nil
}

// MarshalJSON for PhysicalSystem: writes the type discriminator
// up-front and emits children through the polymorphic [Process]
// marshalers so each child's own type is preserved.
func (p *PhysicalSystem) MarshalJSON() ([]byte, error) {
	type alias PhysicalSystem
	wrap := struct {
		Type string `json:"type"`
		*alias
	}{
		Type:  TypePhysicalSystem,
		alias: (*alias)(p),
	}
	return json.Marshal(wrap)
}

// MarshalJSON for PhysicalComponent.
func (p *PhysicalComponent) MarshalJSON() ([]byte, error) {
	type alias PhysicalComponent
	wrap := struct {
		Type string `json:"type"`
		*alias
	}{
		Type:  TypePhysicalComponent,
		alias: (*alias)(p),
	}
	return json.Marshal(wrap)
}

// MarshalJSON for SimpleProcess.
func (s *SimpleProcess) MarshalJSON() ([]byte, error) {
	type alias SimpleProcess
	wrap := struct {
		Type string `json:"type"`
		*alias
	}{
		Type:  TypeSimpleProcess,
		alias: (*alias)(s),
	}
	return json.Marshal(wrap)
}

// MarshalJSON for AggregateProcess.
func (a *AggregateProcess) MarshalJSON() ([]byte, error) {
	type alias AggregateProcess
	wrap := struct {
		Type string `json:"type"`
		*alias
	}{
		Type:  TypeAggregateProcess,
		alias: (*alias)(a),
	}
	return json.Marshal(wrap)
}
