package message

import "time"

// Meta provides metadata about a message's lifecycle and origin.
// This interface enables tracking of when messages were created,
// when they entered the system, and where they originated.
//
// Using an interface rather than a concrete type allows for:
//   - Custom metadata implementations for specific domains
//   - Extended metadata with additional fields when needed
//   - Easier testing with mock implementations
type Meta interface {
	// CreatedAt returns when the original event or observation occurred.
	// For sensor data, this is the measurement time.
	// For business events, this is when the event happened.
	CreatedAt() time.Time

	// ReceivedAt returns when the message entered the processing system.
	// This helps track ingestion latency and message age.
	// May be the same as CreatedAt for real-time streams.
	ReceivedAt() time.Time

	// Source returns the identifier of the message originator.
	// Examples: "gps-reader-service", "sensor-12345", "order-processor"
	// Used for debugging, tracing, and access control.
	Source() string
}
