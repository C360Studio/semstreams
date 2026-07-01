package cco

// Common Core Ontologies (CCO) IRI constants
//
// CCO provides mid-level ontology concepts built on BFO for modeling
// information entities, agents, actions, and artifacts.
//
// Reference: https://github.com/CommonCoreOntology/CommonCoreOntologies

// Namespace is the base IRI prefix for all CCO terms.
const Namespace = "http://www.ontologyrepository.com/CommonCoreOntologies/"

// Information Content Entity Classes
//
// ICEs are generically dependent continuants that are about something.
// They can be copied, transmitted, and stored across different bearers.
const (
	// InformationContentEntity is the root class for all information entities.
	// Extends BFO:GenericallyDependentContinuant.
	// Examples: documents, data records, identifiers, names
	InformationContentEntity = Namespace + "InformationContentEntity"

	// DescriptiveInformationContentEntity describes some entity or situation.
	// Examples: sensor readings, status reports, measurements
	DescriptiveInformationContentEntity = Namespace + "DescriptiveInformationContentEntity"

	// DirectiveInformationContentEntity prescribes or directs action.
	// Examples: plans, specifications, requirements, rules
	DirectiveInformationContentEntity = Namespace + "DirectiveInformationContentEntity"
)

// Directive Information Content Entity Subtypes
//
// Plans, specifications, and requirements that direct action.
const (
	// PlanSpecification describes a set of actions to be performed.
	// Examples: mission plans, flight plans, workflow definitions
	PlanSpecification = Namespace + "PlanSpecification"

	// Specification describes required characteristics or behavior.
	// Examples: API specifications, interface contracts, design docs
	Specification = Namespace + "Specification"

	// Requirement specifies a condition that must be satisfied.
	// Examples: safety requirements, performance requirements
	Requirement = Namespace + "Requirement"

	// Objective describes a goal to be achieved.
	// Examples: mission objectives, business goals
	Objective = Namespace + "Objective"

	// Rule specifies conditions and actions for automated systems.
	// Examples: business rules, validation rules, inference rules
	Rule = Namespace + "Rule"
)

// Descriptive Information Content Entity Subtypes
//
// Identifiers, names, and descriptive information.
const (
	// Identifier is information used to uniquely identify an entity.
	// Examples: serial numbers, UUIDs, registration numbers
	Identifier = Namespace + "Identifier"

	// DesignativeInformationContentEntity designates or refers to an entity.
	// Includes names, titles, and other designators.
	DesignativeInformationContentEntity = Namespace + "DesignativeInformationContentEntity"

	// Name is a word or phrase that identifies an entity.
	// Examples: "Alpha Drone", "Mission Control"
	Name = Namespace + "Name"

	// MeasurementInformationContentEntity is the result of a measurement process.
	// Examples: temperature readings, position fixes, battery levels
	MeasurementInformationContentEntity = Namespace + "MeasurementInformationContentEntity"

	// DescriptiveStatement is a statement that describes some state of affairs.
	// Examples: status updates, event descriptions
	DescriptiveStatement = Namespace + "DescriptiveStatement"
)

// Software and Algorithm Classes
//
// Information entities specific to software systems.
const (
	// SoftwareCode is source code that defines software behavior.
	// Examples: component implementations, scripts, configurations
	SoftwareCode = Namespace + "SoftwareCode"

	// Algorithm is a step-by-step procedure for computation.
	// Examples: pathfinding algorithms, detection algorithms
	Algorithm = Namespace + "Algorithm"

	// SoftwareAgent is software capable of autonomous action.
	// Examples: automated services, background workers
	SoftwareAgent = Namespace + "SoftwareAgent"
)

// Agent Classes
//
// Entities capable of bearing roles and performing intentional acts.
// All agents extend BFO:MaterialEntity (CCO v1.4: cco:Agent ⊑ BFO_0000040;
// a group of agents is an ObjectAggregate, so the branch does not subsume
// under Object). See vocabulary/cco/hierarchy.go for the encoded is-a tree.
const (
	// Agent is an entity capable of performing intentional acts.
	// Root class for all agent types.
	Agent = Namespace + "Agent"

	// Person is a human agent.
	// Examples: operators, pilots, administrators
	Person = Namespace + "Person"

	// Organization is a collective agent with defined structure.
	// Examples: companies, teams, departments
	Organization = Namespace + "Organization"

	// GroupOfAgents is a collection of agents acting together.
	// Examples: flight crews, response teams
	GroupOfAgents = Namespace + "GroupOfAgents"

	// IntelligentSoftwareAgent is software capable of autonomous reasoning.
	// Examples: AI systems, autonomous drones, intelligent assistants
	IntelligentSoftwareAgent = Namespace + "IntelligentSoftwareAgent"
)

// Act Classes
//
// Intentional processes performed by agents.
// All acts extend BFO:Process.
const (
	// Act is a process that is intentionally performed by an agent.
	Act = Namespace + "Act"

	// IntentionalAct is an act done with purpose and awareness.
	// Examples: commanding, navigating, deciding
	IntentionalAct = Namespace + "IntentionalAct"

	// ActOfCommunication is transmission of information between agents.
	// Examples: sending commands, broadcasting status, publishing events
	ActOfCommunication = Namespace + "ActOfCommunication"

	// ActOfApproval is endorsement or authorization of something.
	// Examples: mission approval, permission grants
	ActOfApproval = Namespace + "ActOfApproval"

	// ActOfArtifactProcessing is processing of an artifact.
	// Examples: data transformation, file processing
	ActOfArtifactProcessing = Namespace + "ActOfArtifactProcessing"

	// ActOfDecisionMaking is choosing between alternatives.
	// Examples: route selection, target prioritization
	ActOfDecisionMaking = Namespace + "ActOfDecisionMaking"

	// ActOfObserving is perceiving or measuring something.
	// Examples: sensor readings, monitoring
	ActOfObserving = Namespace + "ActOfObserving"

	// ActOfCommandAndControl is directing the actions of other agents.
	// Examples: mission control operations, fleet management
	ActOfCommandAndControl = Namespace + "ActOfCommandAndControl"

	// ActOfPositionChange is movement from one location to another.
	// Examples: flight, navigation, repositioning
	ActOfPositionChange = Namespace + "ActOfPositionChange"
)

// Artifact Classes
//
// Objects created for a purpose, both physical and digital.
// Artifacts extend BFO:MaterialEntity or ICE.
const (
	// Artifact is an object intentionally made for a purpose.
	// Examples: drones, sensors, documents
	Artifact = Namespace + "Artifact"

	// InformationBearingArtifact is an artifact designed to bear information.
	// Examples: displays, storage media, printed documents
	InformationBearingArtifact = Namespace + "InformationBearingArtifact"

	// Document is a collection of information organized for communication.
	// Examples: reports, manuals, specifications
	Document = Namespace + "Document"

	// Facility is an artifact designed to serve as a site for activities.
	// Examples: hangars, control rooms, data centers
	Facility = Namespace + "Facility"

	// Vehicle is an artifact designed to transport agents or cargo.
	// Examples: aircraft, drones, ground vehicles
	Vehicle = Namespace + "Vehicle"

	// Sensor is an artifact designed to detect physical phenomena.
	// Examples: cameras, thermometers, GPS receivers
	Sensor = Namespace + "Sensor"
)

// Core Relations
//
// Fundamental CCO relations for expressing relationships.
const (
	// IsAbout relates an information content entity to what it describes.
	// Domain: InformationContentEntity, Range: Entity
	// Example: sensorReading isAbout drone
	IsAbout = Namespace + "is_about"

	// HasTextValue relates an ICE to its textual representation.
	// Domain: InformationContentEntity, Range: xsd:string
	// Example: name hasTextValue "Alpha Drone"
	HasTextValue = Namespace + "has_text_value"

	// HasAgent relates an act to the agent that performs it.
	// Domain: Act, Range: Agent
	// Example: commandAct hasAgent operator
	HasAgent = Namespace + "has_agent"

	// AgentIn relates an agent to an act it performs (inverse of HasAgent).
	// Domain: Agent, Range: Act
	// Example: operator agentIn commandAct
	AgentIn = Namespace + "agent_in"

	// HasInput relates a process to an entity that is input.
	// Domain: Process, Range: Entity
	// Example: transformProcess hasInput rawData
	HasInput = Namespace + "has_input"

	// HasOutput relates a process to an entity that is output.
	// Domain: Process, Range: Entity
	// Example: transformProcess hasOutput processedData
	HasOutput = Namespace + "has_output"

	// Prescribes relates directive ICE to the act it prescribes.
	// Domain: DirectiveInformationContentEntity, Range: Act
	// Example: missionPlan prescribes surveillanceAct
	Prescribes = Namespace + "prescribes"

	// IsPrescribedBy relates an act to the directive ICE that prescribes it.
	// Domain: Act, Range: DirectiveInformationContentEntity
	// Example: surveillanceAct isPrescribedBy missionPlan
	IsPrescribedBy = Namespace + "is_prescribed_by"

	// DesignatedBy relates an entity to the ICE that designates it.
	// Domain: Entity, Range: DesignativeInformationContentEntity
	// Example: drone designatedBy callsign
	DesignatedBy = Namespace + "designated_by"

	// Designates relates a designative ICE to what it designates.
	// Domain: DesignativeInformationContentEntity, Range: Entity
	// Example: callsign designates drone
	Designates = Namespace + "designates"

	// HasRole relates an agent to a role they bear.
	// Domain: Agent, Range: BFO:Role
	// Example: person hasRole pilotRole
	HasRole = Namespace + "has_role"

	// MeasuresQuality relates a measurement to the quality it measures.
	// Domain: MeasurementInformationContentEntity, Range: BFO:Quality
	// Example: tempReading measuresQuality temperature
	MeasuresQuality = Namespace + "measures_quality"
)

// Temporal Relations
//
// Relations for expressing when acts and events occur.
const (
	// OccursAt relates an act to the time interval during which it occurs.
	// Domain: Act, Range: TemporalInterval
	OccursAt = Namespace + "occurs_at"

	// StartsAt relates an entity to its start time instant.
	// Domain: Entity, Range: TemporalInstant
	StartsAt = Namespace + "starts_at"

	// EndsAt relates an entity to its end time instant.
	// Domain: Entity, Range: TemporalInstant
	EndsAt = Namespace + "ends_at"

	// HasDuration relates a process to its duration.
	// Domain: Process, Range: TemporalDuration
	HasDuration = Namespace + "has_duration"
)

// Provenance Relations
//
// Relations for tracking creation and modification.
const (
	// CreatedBy relates an artifact to the agent that created it.
	// Domain: Artifact, Range: Agent
	// Example: document createdBy author
	CreatedBy = Namespace + "created_by"

	// ModifiedBy relates an artifact to the agent that modified it.
	// Domain: Artifact, Range: Agent
	// Example: document modifiedBy editor
	ModifiedBy = Namespace + "modified_by"

	// AuthoredBy relates an ICE to the agent that authored it.
	// Domain: InformationContentEntity, Range: Agent
	// Example: report authoredBy analyst
	AuthoredBy = Namespace + "authored_by"
)
