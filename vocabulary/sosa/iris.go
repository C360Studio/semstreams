package sosa

// SOSA namespace identifiers. The Namespace constant is the IRI
// stem; Prefix is the short token used in compact RDF/Turtle and
// JSON-LD output. Both are registered into vocabulary/export at
// package init — see register.go.
const (
	// Prefix is the SOSA short token used when compacting IRIs in
	// RDF/Turtle and JSON-LD output (e.g. "sosa:observes").
	Prefix = "sosa"

	// Namespace is the SOSA IRI stem.
	Namespace = "http://www.w3.org/ns/sosa/"
)

// SOSA class IRIs. Use these as the object of an rdf:type triple,
// or wherever a Graphable needs to declare its SOSA-aligned kind.
const (
	// Sensor — a device or simulator that produces observations.
	Sensor = Namespace + "Sensor"

	// Observation — an act associating a Result with a Procedure,
	// FeatureOfInterest, ObservableProperty, and time stamps.
	Observation = Namespace + "Observation"

	// FeatureOfInterest — the real-world thing the observation is
	// about (a river, a robot joint, a building zone).
	FeatureOfInterest = Namespace + "FeatureOfInterest"

	// ObservableProperty — the quality being observed (temperature,
	// flow rate, vibration amplitude).
	ObservableProperty = Namespace + "ObservableProperty"

	// Platform — the thing hosting one or more sensors / samplers
	// / actuators (a drone airframe, a buoy, a robot chassis).
	Platform = Namespace + "Platform"

	// Procedure — the workflow, protocol, or algorithm a sensor or
	// actuator follows.
	Procedure = Namespace + "Procedure"

	// Sample — material taken from a FeatureOfInterest for
	// downstream observation.
	Sample = Namespace + "Sample"

	// SamplingFeature — a spatial / temporal proxy a Sample is
	// drawn from. Carries the geometry and time bounds the Sample
	// itself doesn't own directly. SOSA §3.10.
	SamplingFeature = Namespace + "SamplingFeature"

	// Sampler — the device or method that produces a Sample.
	Sampler = Namespace + "Sampler"

	// Result — the value produced by an Observation. SOSA permits
	// either a structured Result entity or a simple inline value
	// via [HasSimpleResult].
	Result = Namespace + "Result"

	// Actuator — a device that effects change in the world by
	// executing an Actuation on an ActuatableProperty.
	Actuator = Namespace + "Actuator"

	// Actuation — an act of effecting change, dual to Observation.
	Actuation = Namespace + "Actuation"

	// ActuatableProperty — a property an Actuator can affect.
	ActuatableProperty = Namespace + "ActuatableProperty"
)

// SOSA predicate IRIs. Use these as the predicate of a Triple.
const (
	// Observes binds a Sensor to the ObservableProperty it senses.
	Observes = Namespace + "observes"

	// HasFeatureOfInterest binds an Observation to the
	// FeatureOfInterest it is about.
	HasFeatureOfInterest = Namespace + "hasFeatureOfInterest"

	// MadeBySensor binds an Observation to the Sensor that made
	// it. Inverse of [MadeObservation].
	MadeBySensor = Namespace + "madeBySensor"

	// MadeObservation binds a Sensor to an Observation it
	// produced. Inverse of [MadeBySensor].
	MadeObservation = Namespace + "madeObservation"

	// UsedProcedure binds an Observation or Actuation to the
	// Procedure used.
	UsedProcedure = Namespace + "usedProcedure"

	// HasSimpleResult attaches a literal result value (number,
	// string, boolean) directly to an Observation, bypassing a
	// structured Result entity.
	HasSimpleResult = Namespace + "hasSimpleResult"

	// HasResult attaches a structured Result entity to an
	// Observation.
	HasResult = Namespace + "hasResult"

	// ResultTime is the time the result was produced.
	ResultTime = Namespace + "resultTime"

	// PhenomenonTime is the time the observed phenomenon occurred
	// (may differ from ResultTime, e.g. for processed observations).
	PhenomenonTime = Namespace + "phenomenonTime"

	// Hosts binds a Platform to a Sensor / Sampler / Actuator it
	// carries. Inverse of [IsHostedBy].
	Hosts = Namespace + "hosts"

	// IsHostedBy binds a Sensor / Sampler / Actuator to the
	// Platform that carries it. Inverse of [Hosts].
	IsHostedBy = Namespace + "isHostedBy"

	// ObservedProperty binds an Observation to the
	// ObservableProperty it measured.
	ObservedProperty = Namespace + "observedProperty"

	// HasLocation binds a Feature-of-Interest (System, Sensor,
	// Sample, etc.) to a Geo-Feature describing its spatial extent.
	// W3C SSN/SOSA recommendation 2017 §7. Object is typically a
	// GeoJSON-shaped Point / Polygon / LineString string carried
	// verbatim through the triple set.
	HasLocation = Namespace + "hasLocation"
)

// SSN namespace identifiers. SSN is the systems/deployment overlay
// to SOSA, published in the same W3C 2017 TR but at a distinct
// namespace. The constants below use an SSN prefix to make the
// namespace switch explicit at call sites — per ADR-044's
// "default to SOSA" guidance.
const (
	// SSNPrefix is the SSN short token used when compacting IRIs.
	SSNPrefix = "ssn"

	// SSNNamespace is the SSN IRI stem.
	SSNNamespace = "http://www.w3.org/ns/ssn/"
)

// SSN class IRIs.
const (
	// SSNSystem — a unit of abstraction for pieces of infrastructure
	// (sensor networks, robots, observatories) that implements
	// some Procedure. Aligns with the "system" segment of a
	// SemStreams 6-part Entity ID.
	SSNSystem = SSNNamespace + "System"

	// SSNDeployment — a placement of a System for some Purpose
	// over some period in some location.
	SSNDeployment = SSNNamespace + "Deployment"
)

// SSN predicate IRIs.
const (
	// SSNHasDeployment binds a System to a Deployment.
	SSNHasDeployment = SSNNamespace + "hasDeployment"

	// SSNDeployedSystem binds a Deployment back to its System.
	SSNDeployedSystem = SSNNamespace + "deployedSystem"

	// SSNHasSubSystem binds a System to its sub-Systems
	// (a fleet to its drones, a robot to its joint controllers).
	SSNHasSubSystem = SSNNamespace + "hasSubSystem"

	// SSNHasInput binds a Procedure to one of its inputs.
	SSNHasInput = SSNNamespace + "hasInput"

	// SSNHasOutput binds a Procedure to one of its outputs.
	SSNHasOutput = SSNNamespace + "hasOutput"
)
