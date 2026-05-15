package sensorml

// PhysicalComponent is a SensorML concrete leaf physical
// process — a single sensor / actuator / sampler unit. The
// Method reference points to the procedure the component
// executes (sosa:Procedure semantics).
//
// Physical-process types extend AbstractProcess with an
// AttachedTo field (the physical thing the component is
// mounted on). Per OGC 12-000r2 §6.4.
type PhysicalComponent struct {
	AbstractProcess
	Method     *Reference `json:"method,omitempty"`
	AttachedTo *Reference `json:"attachedTo,omitempty"`
}

// Type implements [Process].
func (PhysicalComponent) Type() string { return TypePhysicalComponent }

// Base implements [Process].
func (p *PhysicalComponent) Base() *AbstractProcess { return &p.AbstractProcess }

// LocalID implements [Process].
func (p *PhysicalComponent) LocalID() string { return p.AbstractProcess.ID }

// PhysicalSystem is a SensorML concrete composite physical
// process — a hardware assembly containing PhysicalComponent
// children (sensors, actuators, samplers) and Connection entries
// describing how their data ports link.
//
// In OGC API Connected Systems terms, a PhysicalSystem record
// IS a SOSA/SSN System, and its Components are typically SOSA
// Sensors / Actuators hosted on the System's Platform.
type PhysicalSystem struct {
	AbstractProcess
	AttachedTo  *Reference     `json:"attachedTo,omitempty"`
	Components  []Process      `json:"components,omitempty"`
	Connections ConnectionList `json:"connections,omitempty"`
}

// Type implements [Process].
func (PhysicalSystem) Type() string { return TypePhysicalSystem }

// Base implements [Process].
func (p *PhysicalSystem) Base() *AbstractProcess { return &p.AbstractProcess }

// LocalID implements [Process].
func (p *PhysicalSystem) LocalID() string { return p.AbstractProcess.ID }

// Compile-time assertions; see types_process.go for the
// pointer-receiver rationale.
var (
	_ Process = (*PhysicalSystem)(nil)
	_ Process = (*PhysicalComponent)(nil)
)
