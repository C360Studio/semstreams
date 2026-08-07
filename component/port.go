package component

import (
	"encoding/json"
)

// Direction identifies whether data enters or leaves a component.
type Direction string

// Direction constants for port data flow.
const (
	DirectionInput  Direction = "input"
	DirectionOutput Direction = "output"
)

// PortKind is the closed vocabulary of semantic component port kinds.
type PortKind string

// InteractionPattern is the canonical communication behavior of a resolved port.
type InteractionPattern string

// Canonical port kinds. Alternate spellings and custom kinds are not accepted.
const (
	PortKindTimer        PortKind = "timer"
	PortKindNetwork      PortKind = "network"
	PortKindFile         PortKind = "file"
	PortKindHTTPClient   PortKind = "http-client"
	PortKindNATS         PortKind = "nats"
	PortKindNATSRequest  PortKind = "nats-request"
	PortKindJetStream    PortKind = "jetstream"
	PortKindKVWatch      PortKind = "kv-watch"
	PortKindKVRead       PortKind = "kv-read"
	PortKindKVWrite      PortKind = "kv-write"
	PortKindStoreRead    PortKind = "store-read"
	PortKindStoreProvide PortKind = "store-provide"
)

// Canonical interaction patterns projected from resolved ports.
const (
	PatternTimer      InteractionPattern = "timer"
	PatternNetwork    InteractionPattern = "network"
	PatternHTTPClient InteractionPattern = "http-client"
	PatternStream     InteractionPattern = "stream"
	PatternRequest    InteractionPattern = "request"
	PatternWatch      InteractionPattern = "watch"
	PatternStore      InteractionPattern = "store"
)

// Port is a resolved component I/O declaration.
type Port struct {
	Name        string    `json:"name"`
	Direction   Direction `json:"direction"`
	Required    bool      `json:"required,omitempty"`
	Description string    `json:"description,omitempty"`
	Config      Portable  `json:"config"`
}

// Portable is the common semantic contract implemented by every port config.
type Portable interface {
	ResourceID() string
	IsExclusive() bool
	Kind() PortKind
}

// InterfaceContract defines the payload contract carried across a port.
type InterfaceContract struct {
	Type       string   `json:"type"`
	Version    string   `json:"version,omitempty"`
	Compatible []string `json:"compatible,omitempty"`
}

// MarshalJSON encodes Port with the same config.kind envelope as PortDefinition.
func (p Port) MarshalJSON() ([]byte, error) {
	config, err := marshalPortable(p.Config)
	if err != nil {
		return nil, portConfigError(p.Name, kindOf(p.Config), "config", err)
	}
	wire := struct {
		Name        string          `json:"name"`
		Direction   Direction       `json:"direction"`
		Required    bool            `json:"required,omitempty"`
		Description string          `json:"description,omitempty"`
		Config      json.RawMessage `json:"config"`
	}{
		Name:        p.Name,
		Direction:   p.Direction,
		Required:    p.Required,
		Description: p.Description,
		Config:      config,
	}
	return json.Marshal(wire)
}

// UnmarshalJSON strictly decodes and resolves a runtime port.
func (p *Port) UnmarshalJSON(data []byte) error {
	var wire struct {
		Name        string          `json:"name"`
		Direction   Direction       `json:"direction"`
		Required    bool            `json:"required,omitempty"`
		Description string          `json:"description,omitempty"`
		Config      json.RawMessage `json:"config"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return portConfigError(wire.Name, rawPortableKind(wire.Config), "port", err)
	}
	config, err := decodePortable(wire.Config)
	if err != nil {
		return portConfigError(wire.Name, rawPortableKind(wire.Config), "config", err)
	}
	resolved, err := (PortDefinition{
		Name:        wire.Name,
		Required:    wire.Required,
		Description: wire.Description,
		Config:      config,
	}).Resolve(wire.Direction)
	if err != nil {
		return err
	}
	*p = resolved
	return nil
}
