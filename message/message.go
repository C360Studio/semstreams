package message

// Message represents the core message interface for the SemStreams platform.
// Messages are the fundamental unit of data flow, carrying typed payloads
// with metadata through the semantic event mesh.
//
// Design principles:
//   - Infrastructure-agnostic: Messages contain only data, no routing or storage logic
//   - Behavioral payloads: Payloads expose capabilities through -able interfaces
//   - Flexible metadata: Meta interface allows different metadata implementations
//   - Content-addressable: Hash method enables deduplication and referencing
//
// Example:
//
//	msg := NewBaseMessage(
//	    Type{Domain: "sensors", Category: "gps", Version: "v1"},
//	    gpsPayload,
//	    "gps-reader-service"
//	)
type Message interface {
	// ID returns a unique identifier for this message instance.
	// Typically a UUID, this ID is immutable and globally unique.
	ID() string

	// Type returns structured type information used for routing and processing.
	// The type contains domain, category, and version information.
	Type() Type

	// Payload returns the message payload that may implement behavioral interfaces.
	// Payloads expose capabilities like Identifiable, Locatable, Observable, etc.
	Payload() Payload

	// Meta returns metadata about the message lifecycle and origin.
	// Includes creation time, receipt time, and source service information.
	Meta() Meta

	// Hash returns a content-based hash for deduplication and storage.
	// The hash is computed from the message type and payload data.
	Hash() string

	// Validate performs comprehensive validation of the message.
	// Checks message type validity, payload presence, and payload-specific validation.
	Validate() error
}
