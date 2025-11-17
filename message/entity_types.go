package message

import "encoding/json"

// EntityClass represents universal semantic classification inspired by Schema.org.
// These classes provide a standardized vocabulary for categorizing entities
// across different domains, enabling cross-domain queries and reasoning.
//
// The classification system follows a simplified version of Schema.org's
// top-level types, adapted for the semantic stream processing context.
type EntityClass string

const (
	// ClassObject represents physical things, equipment, or tangible entities.
	// Examples: drones, batteries, sensors, vehicles, devices
	ClassObject EntityClass = "Object"

	// ClassEvent represents occurrences, incidents, or time-bounded happenings.
	// Examples: alerts, missions, deployments, failures, observations
	ClassEvent EntityClass = "Event"

	// ClassAgent represents actors or entities capable of taking action.
	// Examples: operators, systems, controllers, autonomous agents
	ClassAgent EntityClass = "Agent"

	// ClassPlace represents locations, regions, or spatial entities.
	// Examples: zones, waypoints, regions, coordinates, areas
	ClassPlace EntityClass = "Place"

	// ClassProcess represents ongoing activities or continuous operations.
	// Examples: formations, patrols, monitoring, data collection
	ClassProcess EntityClass = "Process"

	// ClassThing represents generic or unclassified entities.
	// Use this for entities that don't fit other categories or are ambiguous.
	ClassThing EntityClass = "Thing"
)

// String returns the string representation of the EntityClass.
func (ec EntityClass) String() string {
	return string(ec)
}

// MarshalJSON implements json.Marshaler to ensure EntityClass serializes as a string.
func (ec EntityClass) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(ec))
}

// UnmarshalJSON implements json.Unmarshaler to deserialize EntityClass from string.
func (ec *EntityClass) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*ec = EntityClass(s)
	return nil
}

// IsValid checks if the EntityClass is one of the defined constants.
func (ec EntityClass) IsValid() bool {
	switch ec {
	case ClassObject, ClassEvent, ClassAgent, ClassPlace, ClassProcess, ClassThing:
		return true
	default:
		return false
	}
}

// EntityRole represents how an entity relates to the message that mentions it.
// This helps distinguish between different ways an entity can be involved
// in or referenced by a message payload.
//
// The role classification enables more precise entity relationship modeling
// and helps with message interpretation and processing logic.
type EntityRole string

const (
	// RolePrimary indicates the main entity that this message describes or is about.
	// This is typically the central subject of the message.
	// Example: drone entity in a position update message
	RolePrimary EntityRole = "primary"

	// RoleObserved indicates an entity being measured, monitored, or observed.
	// This entity is the target of some observation or measurement.
	// Example: target entity in a sensor reading message
	RoleObserved EntityRole = "observed"

	// RoleComponent indicates an entity that is a component or part of another entity.
	// This represents compositional relationships within the message context.
	// Example: battery entity in a drone status message
	RoleComponent EntityRole = "component"

	// RoleSource indicates the entity that generated or originated the message.
	// This is the actor or system responsible for creating the message.
	// Example: sensor entity that produced a measurement
	RoleSource EntityRole = "source"

	// RoleTarget indicates an entity that is affected by or is the destination of the message.
	// This entity receives some action or effect described by the message.
	// Example: waypoint entity in a navigation command
	RoleTarget EntityRole = "target"

	// RoleContext indicates an entity that provides environmental or situational context.
	// This entity helps establish the setting or circumstances of the message.
	// Example: weather station entity in a flight status message
	RoleContext EntityRole = "context"

	// RoleRelated indicates a generic related entity that doesn't fit other categories.
	// Use this for entities that have some connection but don't fall into specific roles.
	// Example: nearby entities mentioned for reference
	RoleRelated EntityRole = "related"
)

// String returns the string representation of the EntityRole.
func (er EntityRole) String() string {
	return string(er)
}

// MarshalJSON implements json.Marshaler to ensure EntityRole serializes as a string.
func (er EntityRole) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(er))
}

// UnmarshalJSON implements json.Unmarshaler to deserialize EntityRole from string.
func (er *EntityRole) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*er = EntityRole(s)
	return nil
}

// IsValid checks if the EntityRole is one of the defined constants.
func (er EntityRole) IsValid() bool {
	switch er {
	case RolePrimary, RoleObserved, RoleComponent, RoleSource, RoleTarget, RoleContext, RoleRelated:
		return true
	default:
		return false
	}
}
