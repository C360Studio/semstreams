// Package message provides the core message infrastructure for the SemStreams platform.
// It defines interfaces and types for creating, validating, and processing messages
// that flow through the semantic event mesh.
//
// # Architecture
//
// The package follows a clean, domain-agnostic design with three core concepts:
//
// 1. Messages - Containers that combine typed payloads with metadata
// 2. Payloads - Domain-specific data that may implement behavioral interfaces
// 3. Metadata - Information about message lifecycle and origin
//
// # Message Structure
//
// Every message consists of:
//   - A unique ID for tracking and deduplication
//   - A structured Type (domain, category, version)
//   - A Payload containing the actual data
//   - Metadata about creation time, source, etc.
//   - A content-based hash for integrity
//
// # Behavioral Interfaces
//
// The message package uses runtime capability discovery through optional interfaces.
// Payloads implement only the interfaces relevant to their domain, and services
// discover these capabilities dynamically through type assertions.
//
// ## Primary Entity Interface
//
// Graphable: Declares entities and relationships for knowledge graph storage
//   - EntityID() string - Returns federated entity identifier
//   - Triples() []Triple - Returns semantic facts about the entity
//   - Use when: Payload represents entities that should be stored in the graph
//   - Example: Drone telemetry, sensor readings, IoT device states
//
// ## Spatial Interfaces
//
// Locatable: Provides geographic coordinates for spatial indexing
//   - Location() (lat, lon float64) - Returns decimal degrees coordinates
//   - Use when: Payload has geographic location data
//   - Example: GPS coordinates, facility locations, vehicle positions
//
// ## Temporal Interfaces
//
// Timeable: Provides event/observation timestamp for time-series analysis
//   - Timestamp() time.Time - Returns event time (not message creation time)
//   - Use when: Payload represents time-series data or historical events
//   - Example: Sensor readings, log entries, financial trades
//
// Expirable: Defines time-to-live for automatic cleanup
//   - ExpiresAt() time.Time - Returns expiration timestamp
//   - TTL() time.Duration - Returns time-to-live from creation
//   - Use when: Payload should be automatically cleaned up after expiration
//   - Example: Temporary cache entries, session data, ephemeral events
//
// ## Observational Interfaces
//
// Observable: Represents observations of other entities
//   - ObservedEntity() string - Returns ID of observed entity
//   - ObservedProperty() string - Returns observed property name
//   - ObservedValue() any - Returns observed value
//   - ObservedUnit() string - Returns measurement unit
//   - Use when: Payload is a sensor reading or observation
//   - Example: Temperature sensor reading, pressure measurement
//
// Measurable: Contains multiple measurements with units
//   - Measurements() map[string]any - Returns all measurements
//   - Unit(measurement string) string - Returns unit for specific measurement
//   - Use when: Payload contains multiple related measurements
//   - Example: Weather station data, health monitor readings
//
// ## Correlation Interfaces
//
// Correlatable: Enables distributed tracing and request/response matching
//   - CorrelationID() string - Returns correlation identifier
//   - Use when: Messages need to be correlated across requests/responses
//   - Example: Request/response pairs, distributed transactions
//
// Traceable: Supports distributed tracing with spans (OpenTelemetry compatible)
//   - TraceID() string - Returns trace identifier
//   - SpanID() string - Returns span identifier
//   - ParentSpanID() string - Returns parent span identifier
//   - Use when: Integrating with distributed tracing systems
//   - Example: Microservice calls, async processing chains
//
// ## Processing Interfaces
//
// Processable: Specifies processing priority and deadlines
//   - Priority() int - Returns priority (0-10, higher = more important)
//   - Deadline() time.Time - Returns processing deadline
//   - Use when: Messages need priority-based or deadline-aware processing
//   - Example: Real-time alerts, time-sensitive commands
//
// Deployable: Indicates deployment or collection membership
//   - DeploymentID() string - Returns deployment identifier
//   - Use when: Messages need to be grouped by deployment context
//   - Example: Multi-tenant systems, A/B testing, staging environments
//
// ## Runtime Discovery Pattern
//
// Services discover capabilities at runtime through type assertions:
//
//	// Check for location data
//	if locatable, ok := msg.Payload().(Locatable); ok {
//	    lat, lon := locatable.Location()
//	    // Index by location, build spatial queries, etc.
//	}
//
//	// Check for entity data
//	if graphable, ok := msg.Payload().(Graphable); ok {
//	    entityID := graphable.EntityID()
//	    triples := graphable.Triples()
//	    // Store in knowledge graph, build relationships, etc.
//	}
//
//	// Check for time-series data
//	if timeable, ok := msg.Payload().(Timeable); ok {
//	    timestamp := timeable.Timestamp()
//	    // Index by time, build time-series queries, etc.
//	}
//
// This pattern enables services to process any message type without prior knowledge
// of the specific payload structure, discovering capabilities dynamically.
//
// # Type System Hierarchy
//
// The message package uses three related but distinct type representations,
// each serving a specific purpose in the semantic event mesh architecture.
// All three implement the Keyable interface, providing consistent dotted notation
// for NATS routing, storage keys, and entity identification.
//
// ## 1. Type (Message Schema) - "domain.category.version"
//
// Purpose: Identifies message schemas for routing, validation, and evolution.
// Format: Type{Domain: "sensors", Category: "gps", Version: "v1"} -> "sensors.gps.v1"
//
// Use When:
//   - Defining message schemas in payload implementations (Schema() method)
//   - Configuring component input/output types
//   - Routing messages through NATS subjects
//   - Versioning payload schemas for evolution
//
// Example:
//
//	type TemperaturePayload struct { /* fields */ }
//	func (t *TemperaturePayload) Schema() Type {
//	    return Type{Domain: "sensors", Category: "temperature", Version: "v1"}
//	}
//	// Used for: NATS subject "sensors.temperature.v1"
//
// ## 2. EntityType (Graph Classification) - "domain.type"
//
// Purpose: Classifies entities in the property graph for querying and analysis.
// Format: EntityType{Domain: "robotics", Type: "drone"} -> "robotics.drone"
//
// Use When:
//   - Extracting type from EntityID: entityID.EntityType()
//   - Querying the graph for entities by type
//   - Classifying nodes in the knowledge graph
//   - Building semantic relationships between entity types
//
// Example:
//
//	// EntityType is derived from EntityID
//	entityID := EntityID{
//	    Org: "c360", Platform: "platform1",
//	    Domain: "robotics", Type: "drone",
//	    System: "mav1", Instance: "1",
//	}
//	entityType := entityID.EntityType()  // Returns EntityType{Domain: "robotics", Type: "drone"}
//	// Used for: Graph queries like "find all robotics.drone entities"
//
// ## 3. EntityID (Federated Identity) - "org.platform.domain.system.type.instance"
//
// Purpose: Provides globally unique entity identifiers across federated platforms.
// Format: EntityID with 6 parts -> "c360.platform1.robotics.mav1.drone.1"
//
// Use When:
//   - Multi-platform deployments need unique entity identity
//   - Merging data from multiple sources with overlapping IDs
//   - Federating entities across organizational boundaries
//   - Building distributed knowledge graphs
//
// Example:
//
//	entityID := EntityID{
//	    Org:      "c360",      // Organization namespace
//	    Platform: "platform1", // Platform instance
//	    Domain:   "robotics",  // Data domain
//	    System:   "mav1",      // Message source system
//	    Type:     "drone",     // Entity type
//	    Instance: "1",         // Local instance ID
//	}
//	// Key(): "c360.platform1.robotics.mav1.drone.1"
//	// Used for: Federated entity resolution and deduplication
//
// ## Type Relationships
//
// These types form a hierarchy:
//
//	EntityID (6 parts) contains → EntityType (2 parts)
//	  entityID.EntityType() returns EntityType{Domain: "robotics", Type: "drone"}
//
//	Type (3 parts) is independent → Used for message schemas, not entity identity
//
// All three implement Keyable:
//
//	type Keyable interface {
//	    Key() string  // Returns dotted notation for NATS subjects and storage
//	}
//
// ## Choosing the Right Type
//
// Ask yourself:
//
//  1. Defining a message schema? → Use Type (Schema() method)
//  2. Providing entity identity? → Use EntityID (Graphable.EntityID() method)
//  3. Extracting entity classification? → Use EntityType (derived from EntityID)
//
// Most payloads only need Type (for Schema()). Only implement Graphable if your
// payload represents entities that should be stored in the knowledge graph.
// EntityType is typically not constructed directly - it's extracted from EntityID
// using the EntityType() method when querying or classifying graph entities.
//
// # Usage Example
//
//	// Define a payload type
//	type TemperaturePayload struct {
//	    SensorID    string    `json:"sensor_id"`
//	    Temperature float64   `json:"temperature"`
//	    Unit        string    `json:"unit"`
//	    Timestamp   time.Time `json:"timestamp"`
//	}
//
//	// Implement required Payload interface
//	func (t *TemperaturePayload) Schema() Type {
//	    return Type{
//	        Domain:   "sensors",
//	        Category: "temperature",
//	        Version:  "v1",
//	    }
//	}
//
//	func (t *TemperaturePayload) Validate() error {
//	    if t.SensorID == "" {
//	        return errors.New("sensor ID required")
//	    }
//	    return nil
//	}
//
//	func (t *TemperaturePayload) MarshalJSON() ([]byte, error) {
//	    // Use alias to avoid infinite recursion
//	    type Alias TemperaturePayload
//	    return json.Marshal((*Alias)(t))
//	}
//
//	func (t *TemperaturePayload) UnmarshalJSON(data []byte) error {
//	    // Use alias to avoid infinite recursion
//	    type Alias TemperaturePayload
//	    return json.Unmarshal(data, (*Alias)(t))
//	}
//
//	// Implement optional behavioral interfaces
//	func (t *TemperaturePayload) Timestamp() time.Time {
//	    return t.Timestamp
//	}
//
//	// Create and use a message
//	payload := &TemperaturePayload{
//	    SensorID:    "temp-001",
//	    Temperature: 22.5,
//	    Unit:        "celsius",
//	    Timestamp:   time.Now(),
//	}
//
//	msg := NewBaseMessage(
//	    payload.Schema(),
//	    payload,
//	    "temperature-monitor",
//	)
//
//	// Services can discover capabilities
//	// Modern approach using Graphable (preferred)
//	if graphable, ok := msg.Payload().(Graphable); ok {
//	    entityID := graphable.EntityID()
//	    triples := graphable.Triples()
//	    fmt.Printf("Entity: %s with %d triples\n", entityID, len(triples))
//	    for _, triple := range triples {
//	        fmt.Printf("  %s: %v\n", triple.Predicate, triple.Object)
//	    }
//	}
//
// # Message Lifecycle
//
// Messages follow a predictable lifecycle from creation through processing:
//
// ## 1. Creation
//
// Messages are created using NewBaseMessage with optional configuration:
//
//	// Simple message with current timestamp
//	msg := NewBaseMessage(payload.Schema(), payload, "my-service")
//
//	// Historical data with specific timestamp
//	msg := NewBaseMessage(payload.Schema(), payload, "my-service",
//	    WithTime(historicalTime))
//
//	// Federated message for multi-platform deployment
//	msg := NewBaseMessage(payload.Schema(), payload, "my-service",
//	    WithFederation(platformConfig))
//
// Messages are immutable after creation - all fields are set during construction
// and cannot be modified. This ensures message integrity throughout processing.
//
// ## 2. Validation
//
// Validation happens at two levels:
//
// Structural Validation (Required):
//   - Message type must be valid (domain, category, version all present)
//   - Payload must not be nil
//   - Metadata must not be nil
//
// Payload Validation (Domain-specific):
//   - Implemented by Payload.Validate() method
//   - Checks domain-specific business rules
//   - May be structural (required fields) or semantic (value ranges)
//
// Example validation:
//
//	if err := msg.Validate(); err != nil {
//	    // Handle validation error
//	    // Check error type: Fatal, Transient, or Invalid
//	}
//
// ## 3. Serialization
//
// Messages are serialized to JSON for transmission over NATS:
//
//	// Serialize message
//	data, err := json.Marshal(msg)
//
//	// Deserialize message
//	var msg BaseMessage
//	err := json.Unmarshal(data, &msg)
//
// Wire format preserves:
//   - Message ID for deduplication and tracking
//   - Type information for routing and schema validation
//   - Payload data using Payload.MarshalJSON()
//   - Metadata timestamps (millisecond precision) and source
//
// Note: Deserialization requires payload types to be registered in the global
// PayloadRegistry. For generic JSON processing, use the well-known type
// "core.json.v1" (GenericJSONPayload).
//
// ## 4. Transmission
//
// Messages are published to NATS subjects derived from their type:
//
//	subject := msg.Type().Key()  // e.g., "sensors.temperature.v1"
//	nc.Publish(subject, data)
//
// This enables:
//   - Type-based routing using NATS wildcards
//   - Version-specific processing
//   - Domain isolation and security policies
//
// ## 5. Processing
//
// Services receive and process messages based on their subscriptions:
//
//	// Subscribe to specific type
//	nc.Subscribe("sensors.temperature.v1", handler)
//
//	// Subscribe to all versions
//	nc.Subscribe("sensors.temperature.*", handler)
//
//	// Subscribe to entire domain
//	nc.Subscribe("sensors.>", handler)
//
// Services discover payload capabilities at runtime:
//
//	func handler(m *nats.Msg) {
//	    var msg BaseMessage
//	    json.Unmarshal(m.Data, &msg)
//
//	    // Discover capabilities
//	    if graphable, ok := msg.Payload().(Graphable); ok {
//	        // Process entity data
//	    }
//	    if locatable, ok := msg.Payload().(Locatable); ok {
//	        // Process location data
//	    }
//	}
//
// # Best Practices
//
// ## Payload Implementation
//
// 1. Implement Required Methods
//   - Schema() Type - Return structured type information
//   - Validate() error - Perform domain-specific validation
//   - MarshalJSON() ([]byte, error) - Use alias pattern to avoid recursion
//   - UnmarshalJSON([]byte) error - Use alias pattern to avoid recursion
//
// 2. Implement Optional Interfaces Thoughtfully
//   - Only implement behavioral interfaces that make semantic sense
//   - Don't implement Locatable just because you have two numbers
//   - Don't implement Timeable if you only have message creation time
//   - Consider whether your payload truly represents an entity before implementing Graphable
//
// 3. Validation Philosophy
//   - Structural validation: Check required fields and basic types
//   - Semantic validation: Check business rules and value ranges (optional)
//   - Fail fast: Return first validation error encountered
//   - Provide clear error messages with context
//
// 4. Use Type Constants
//
//   - Define message types as package constants
//
//   - Don't inline type construction at call sites
//
//   - Example:
//
//     // Good: Type as constant
//     var TemperatureType = message.Type{
//     Domain: "sensors", Category: "temperature", Version: "v1",
//     }
//
//     // Bad: Inline type construction
//     msg := NewBaseMessage(
//     message.Type{Domain: "sensors", Category: "temperature", Version: "v1"},
//     payload, source)
//
// ## Message Creation
//
// 1. Use Functional Options for Configuration
//   - Start with simple NewBaseMessage(type, payload, source)
//   - Add options only when needed: WithTime(), WithFederation()
//   - Don't create custom constructors - use options instead
//
// 2. Set Source Meaningfully
//   - Use service name or component identifier
//   - Be consistent across your application
//   - Example: "temperature-monitor", "gps-processor", "user-service"
//
// 3. Timestamp Considerations
//   - Default timestamp (time.Now()) is correct for most cases
//   - Use WithTime() only for historical data or testing
//   - For event time vs creation time: use Timeable interface
//
// ## Message Processing
//
// 1. Type Assertion Pattern
//
//   - Use comma-ok idiom for capability discovery
//
//   - Don't assume interfaces are implemented
//
//   - Example:
//
//     if graphable, ok := msg.Payload().(Graphable); ok {
//     // Safe to use graphable methods
//     } else {
//     // Payload doesn't implement Graphable, skip or handle differently
//     }
//
// 2. Error Handling
//   - Always check validation errors before processing
//   - Use errors.As() to detect error types (Fatal, Transient, Invalid)
//   - Don't rely on error message strings for logic
//
// 3. Immutability
//   - Don't modify message fields after creation
//   - Create new messages for transformations
//   - Payload data can be read but not written
//
// ## Testing
//
// 1. Use Real Test Data
//   - Create actual payload instances, don't use mocks
//   - Test with both valid and invalid data
//   - Test serialization round-trips
//
// 2. Test Behavioral Interfaces
//   - Verify interface implementations return correct values
//   - Test type assertions work as expected
//   - Ensure optional interfaces are truly optional
//
// 3. Test Validation
//   - Test both valid and invalid payloads
//   - Verify error messages are clear
//   - Test edge cases and boundary conditions
//
// # Design Philosophy
//
// The messages package embodies several key design principles:
//
// 1. Separation of Concerns: Messages know nothing about routing, storage, or
// entity extraction. They are pure data containers.
//
// 2. Discoverability: Behavioral interfaces allow services to discover and utilize
// message capabilities at runtime without tight coupling.
//
// 3. Domain Agnosticism: The core package contains no domain-specific code.
// All domain logic lives in domain packages that use these interfaces.
//
// 4. Extensibility: New behavioral interfaces can be added without breaking
// existing code. Payloads can implement any combination of interfaces.
//
// 5. Type Safety: Structured Type provides compile-time safety while
// maintaining flexibility.
package message
