package examples

import "github.com/c360/semstreams/vocabulary"

// Example robotics domain predicates.
// Shows how domain-specific applications would define their vocabulary.
const (
	// RoboticsCommunicationCallsign - radio call sign for ATC
	RoboticsCommunicationCallsign = "robotics.communication.callsign"

	// RoboticsIdentifierSerial - manufacturer serial number
	RoboticsIdentifierSerial = "robotics.identifier.serial"

	// RoboticsIdentifierRegistration - FAA or regulatory registration
	RoboticsIdentifierRegistration = "robotics.identifier.registration"
)

// RegisterRoboticsVocabulary registers example robotics domain predicates.
func RegisterRoboticsVocabulary() {
	// Communication identifiers - resolvable to entity IDs
	vocabulary.Register(RoboticsCommunicationCallsign,
		vocabulary.WithDescription("Radio call sign for air traffic control"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeCommunication, 0), // Highest for robotics domain
		vocabulary.WithIRI(vocabulary.FoafAccountName))

	// External identifiers - resolvable to entity IDs
	vocabulary.Register(RoboticsIdentifierSerial,
		vocabulary.WithDescription("Manufacturer serial number"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeExternal, 1),
		vocabulary.WithIRI(vocabulary.DcIdentifier))

	vocabulary.Register(RoboticsIdentifierRegistration,
		vocabulary.WithDescription("FAA or regulatory registration number"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeExternal, 2),
		vocabulary.WithIRI(vocabulary.SchemaIdentifier))
}
