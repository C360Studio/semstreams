package message

import "time"

// Behavioral Interfaces
//
// This file defines PURE structural behavioral interfaces that payloads can
// optionally implement to expose additional capabilities. Services discover
// these capabilities at runtime through type assertions.
//
// These interfaces enable runtime capability discovery:
//
//	if locatable, ok := msg.Payload().(Locatable); ok {
//	    lat, lon := locatable.Location()
//	    // Process location data...
//	}
//
// Semantic interfaces (Graphable, Storable) are defined in separate files.

// Locatable provides geographic coordinates for spatial indexing.
// Payloads implementing this interface can be indexed by location
// and queried spatially.
type Locatable interface {
	// Location returns geographic coordinates as (latitude, longitude).
	// Values should be in decimal degrees:
	//   - Latitude: -90.0 to +90.0 (negative = South, positive = North)
	//   - Longitude: -180.0 to +180.0 (negative = West, positive = East)
	Location() (lat, lon float64)
}

// Timeable provides temporal information for time-series analysis.
// Payloads implementing this interface can be indexed by time
// and queried temporally.
type Timeable interface {
	// Timestamp returns the observation or event time.
	// This should be the actual time of observation/event, not message
	// creation time (which is in BaseMessage metadata).
	Timestamp() time.Time
}

// Observable represents observations of other entities.
// This interface is used for sensor data, measurements, and observations
// that reference an external entity or property being observed.
type Observable interface {
	// ObservedEntity returns the ID of the entity being observed.
	// Example: "sensor-123", "vehicle-456", "building-789"
	ObservedEntity() string

	// ObservedProperty returns the property being observed.
	// Example: "temperature", "pressure", "velocity", "position"
	ObservedProperty() string

	// ObservedValue returns the observed value as a generic interface.
	// The actual type depends on the property being observed.
	ObservedValue() any

	// ObservedUnit returns the unit of measurement.
	// Example: "celsius", "pascal", "m/s", "meters"
	// Returns empty string if unitless.
	ObservedUnit() string
}

// Correlatable enables distributed tracing and causality tracking.
// Payloads implementing this interface can be correlated across
// requests, responses, and distributed operations.
type Correlatable interface {
	// CorrelationID returns a unique identifier for correlating related messages.
	// This enables request/response matching and distributed tracing.
	// Common patterns:
	//   - Request ID: Shared by request and response
	//   - Trace ID: Shared by all messages in a distributed operation
	//   - Causation ID: Links cause and effect messages
	CorrelationID() string
}

// Measurable contains multiple measurements with units.
// This interface extends Observable for payloads that contain
// multiple related measurements.
type Measurable interface {
	// Measurements returns a map of measurement names to values.
	// Each measurement may have different units.
	Measurements() map[string]any

	// Unit returns the unit for a specific measurement.
	// Returns empty string if measurement doesn't exist or is unitless.
	Unit(measurement string) string
}

// Deployable indicates the payload belongs to a deployment or collection.
// This enables grouping messages by deployment context.
type Deployable interface {
	// DeploymentID returns the identifier of the deployment or collection.
	// Example: "production-us-west", "staging-experiment-42"
	DeploymentID() string
}

// Processable specifies processing priority and deadlines.
// This enables deadline-aware processing and priority queues.
type Processable interface {
	// Priority returns the processing priority (higher = more important).
	// Typical range: 0 (lowest) to 10 (highest).
	Priority() int

	// Deadline returns the time by which processing must complete.
	// Returns zero time if no deadline.
	Deadline() time.Time
}

// Traceable supports distributed tracing with spans.
// This interface enables integration with distributed tracing systems
// like OpenTelemetry, Jaeger, or Zipkin.
type Traceable interface {
	// TraceID returns the trace identifier.
	TraceID() string

	// SpanID returns the span identifier for this message.
	SpanID() string

	// ParentSpanID returns the parent span identifier.
	// Returns empty string if this is a root span.
	ParentSpanID() string
}

// Expirable defines time-to-live for automatic cleanup.
// Payloads implementing this interface can be automatically
// expired and cleaned up by storage systems.
type Expirable interface {
	// ExpiresAt returns the time when this payload expires.
	// Returns zero time if payload never expires.
	ExpiresAt() time.Time

	// TTL returns the time-to-live duration from creation.
	// Returns 0 if payload never expires.
	TTL() time.Duration
}
