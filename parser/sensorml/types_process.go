package sensorml

import "encoding/json"

// Process is the SensorML JSON polymorphic root. Every concrete
// process type (PhysicalSystem, PhysicalComponent, SimpleProcess,
// AggregateProcess) implements Process by reporting its
// type-discriminator string and exposing the shared
// [AbstractProcess] block.
//
// Operate on the concrete types when you need type-specific
// fields (e.g. PhysicalSystem.Components); operate through
// Process when you only need the shared fields or are walking
// a heterogeneous list.
type Process interface {
	// Type returns the SensorML JSON type-discriminator string
	// ("PhysicalSystem", "PhysicalComponent", "SimpleProcess",
	// "AggregateProcess").
	Type() string

	// Base returns the embedded AbstractProcess block carrying
	// the fields shared by every SensorML process kind. Never
	// nil for a well-formed value (the embedded struct is
	// value-typed, not pointer-typed).
	Base() *AbstractProcess

	// LocalID returns the document-local id field. Local IDs are
	// scoped to the SensorML document — they are NOT 6-part
	// SemStreams entity IDs. Pair through [NewAsset] before
	// feeding to the graph layer.
	LocalID() string
}

// AbstractProcess holds the fields shared by every concrete
// SensorML process kind per OGC 12-000r2. Concrete types embed
// this struct by value.
//
// Field names mirror the SensorML JSON property names; pointer
// fields are optional. The struct is a hand-written subset of the
// full schema — fields outside CS API v1.0's critical path are
// intentionally omitted (see doc.go's Scope section).
type AbstractProcess struct {
	ID              string             `json:"id,omitempty"`
	Label           string             `json:"label,omitempty"`
	Description     string             `json:"description,omitempty"`
	Definition      string             `json:"definition,omitempty"`
	UniqueID        string             `json:"uniqueId,omitempty"`
	Identifiers     IdentifierList     `json:"identifiers,omitempty"`
	Classifiers     ClassifierList     `json:"classifiers,omitempty"`
	Characteristics CharacteristicList `json:"characteristics,omitempty"`
	Capabilities    CapabilityList     `json:"capabilities,omitempty"`
	Keywords        []string           `json:"keywords,omitempty"`
	Inputs          DataComponentList  `json:"inputs,omitempty"`
	Outputs         DataComponentList  `json:"outputs,omitempty"`
	Parameters      DataComponentList  `json:"parameters,omitempty"`
	TypeOf          *Reference         `json:"typeOf,omitempty"`
	Position        *Position          `json:"position,omitempty"`
}

// Position is the SensorML §7.1.6 spatial element. Wire form is a
// GeoJSON-shaped Point / Polygon / LineString carried verbatim as
// json.RawMessage so the parser does not have to know every valid
// geometry shape. Consumers (graph-index-spatial, CS API gateways)
// read Raw and parse as GeoJSON.
//
// MarshalJSON returns Raw directly — round-trips losslessly.
// UnmarshalJSON copies the incoming bytes into Raw, so the source
// stream is decoupled from the slice the struct retains.
//
// See issue #114.
type Position struct {
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON captures the raw position JSON for later passthrough.
func (p *Position) UnmarshalJSON(data []byte) error {
	p.Raw = append(p.Raw[:0], data...)
	return nil
}

// MarshalJSON returns the captured raw JSON. Returns the JSON
// literal "null" when Raw is empty so consumers do not see invalid
// JSON output if the struct was constructed without a payload.
func (p Position) MarshalJSON() ([]byte, error) {
	if len(p.Raw) == 0 {
		return []byte("null"), nil
	}
	return p.Raw, nil
}

// SimpleProcess is a SensorML concrete leaf process — an
// algorithmic / procedural unit with no sub-processes. The
// Method reference points to the procedure that defines what
// the process does.
type SimpleProcess struct {
	AbstractProcess
	Method *Reference `json:"method,omitempty"`
}

// Type implements [Process].
func (SimpleProcess) Type() string { return TypeSimpleProcess }

// Base implements [Process].
func (s *SimpleProcess) Base() *AbstractProcess { return &s.AbstractProcess }

// LocalID implements [Process].
func (s *SimpleProcess) LocalID() string { return s.AbstractProcess.ID }

// AggregateProcess is a SensorML concrete composite process — a
// container that orchestrates child processes via the Components
// list and the Connections among them.
type AggregateProcess struct {
	AbstractProcess
	Components  []Process      `json:"components,omitempty"`
	Connections ConnectionList `json:"connections,omitempty"`
}

// Type implements [Process].
func (AggregateProcess) Type() string { return TypeAggregateProcess }

// Base implements [Process].
func (a *AggregateProcess) Base() *AbstractProcess { return &a.AbstractProcess }

// LocalID implements [Process].
func (a *AggregateProcess) LocalID() string { return a.AbstractProcess.ID }

// Compile-time assertions that every concrete process type
// implements [Process]. Pointer-receiver discipline matters: the
// MarshalJSON receivers are pointer-only, so a caller appending
// a value rather than a pointer to a Components slice would
// silently drop the type discriminator on emit. These assertions
// lock the contract.
var (
	_ Process = (*SimpleProcess)(nil)
	_ Process = (*AggregateProcess)(nil)
)
