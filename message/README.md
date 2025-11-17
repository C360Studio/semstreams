# Message Package

Polymorphic message handling for SemStreams data flows with behavior-based processing and type-safe payload management.

## Overview

The message package provides the foundation for type-safe, extensible message processing in SemStreams. Messages flow through components as polymorphic envelopes (BaseMessage) containing typed payloads that can be type-asserted to behavioral interfaces for specialized processing.

Key design principles:

- **Polymorphic by default**: Messages deserialize to correct concrete types automatically
- **Behavior-based processing**: Type-assert to capabilities (Locatable, Timeable, etc.) not concrete types
- **Registry-driven**: Global payload registry enables extensibility without code changes
- **Type-safe**: Compile-time type checking with runtime polymorphism
- **Immutable**: Messages are read-only after creation (safe for concurrent access)

## Installation

```go
import "github.com/c360/semstreams/message"
```

## Core Concepts

### Message Structure

Every SemStreams message consists of:

1. **Envelope** (`BaseMessage`): Polymorphic wrapper with type metadata
2. **Payload**: Concrete data implementing the Payload interface
3. **Behavioral Interfaces**: Optional capabilities for specialized processing

```text
┌─ BaseMessage ────────────────────────────┐
│ Type: "core.json.v1"                     │
│ PayloadData: <raw JSON>                  │
│ ├─ payloadInstance ─────────────────┐    │
│ │  GenericJSONPayload               │    │
│ │  ├─ Data: map[string]any          │    │
│ │  ├─ Implements: Payload           │    │
│ │  └─ Optionally: Locatable,        │    │
│ │                 Timeable, etc.    │    │
│ └───────────────────────────────────┘    │
└──────────────────────────────────────────┘
```

### Type System

Payloads are identified by "domain.category.version" strings:

- `core.json.v1` - Generic JSON (built-in SemStreams type)
- `robotics.mavlink.v1` - MAVLink messages (domain-specific)
- `iot.sensor.v2` - IoT sensor data (domain-specific)

Components declare accepted types in their configuration, and the flow engine validates compatibility before deployment.

### Behavioral Interfaces

Optional capabilities that payloads can implement:

| Interface | Purpose | Methods | Use Case |
|-----------|---------|---------|----------|
| **Locatable** | Geographic coordinates | `Location() (lat, lon float64)` | Mapping, geofencing |
| **Timeable** | Temporal metadata | `Timestamp() time.Time` | Time-series analysis |
| **Observable** | Sensor observations | `ObservedEntity()`, `ObservedProperty()`, `ObservedValue()`, `ObservedUnit()` | Sensor data processing |
| **Correlatable** | Message correlation | `CorrelationID() string` | Request/response matching |
| **Identifiable** | Entity identification | `EntityID() string`, `EntityType() string` | Entity tracking |
| **Targetable** | Destination info | `TargetID() string`, `TargetType() string` | Routing decisions |

Processors type-assert to the specific behaviors they need, making components reusable across different payload types.

## Quick Start

### Creating Messages

```go
// Create a GenericJSON payload
payload := message.NewGenericJSON(map[string]any{
    "sensor_id": "temp-001",
    "temperature": 23.5,
    "unit": "celsius",
})

// Wrap in BaseMessage
msg := message.NewBaseMessage(payload)

// Serialize to JSON
data, err := json.Marshal(msg)
// Result: {"type":"core.json.v1","payload":{"data":{"sensor_id":"temp-001"...}}}
```

### Deserializing Messages

```go
// Automatic polymorphic reconstruction
var msg message.BaseMessage
err := json.Unmarshal(data, &msg)
// Payload automatically created based on "type" field

// Access the payload
payload := msg.Payload()
if genericJSON, ok := payload.(*message.GenericJSONPayload); ok {
    temperature := genericJSON.Data["temperature"].(float64)
    fmt.Printf("Temperature: %.1f°C\n", temperature)
}
```

### Using Behavioral Interfaces

```go
// Check if payload has location data
if locatable, ok := msg.Payload().(message.Locatable); ok {
    lat, lon := locatable.Location()
    fmt.Printf("Location: %.6f, %.6f\n", lat, lon)
}

// Check if payload has timestamp
if timeable, ok := msg.Payload().(message.Timeable); ok {
    ts := timeable.Timestamp()
    fmt.Printf("Recorded at: %s\n", ts.Format(time.RFC3339))
}

// Check for observation data
if obs, ok := msg.Payload().(message.Observable); ok {
    entity := obs.ObservedEntity()
    property := obs.ObservedProperty()
    value := obs.ObservedValue()
    unit := obs.ObservedUnit()
    fmt.Printf("%s.%s = %v %s\n", entity, property, value, unit)
}
```

## Architecture

### Message Flow

```text
┌─────────────┐     ┌────────────────┐     ┌─────────────┐     ┌─────────────┐
│   Input     │────▶│   Processor    │────▶│  Processor  │────▶│   Output    │
│ (UDP/File)  │     │  (Filter/Map)  │     │(Transform)  │     │(File/WS)    │
└─────────────┘     └────────────────┘     └─────────────┘     └─────────────┘
      │                     │                      │                    │
      ▼                     ▼                      ▼                    ▼
 BaseMessage          BaseMessage            BaseMessage          BaseMessage
  (Payload)            (Payload)              (Payload)            (Payload)
```

Each component:

1. Receives `BaseMessage` from NATS
2. Type-checks the payload type
3. Type-asserts to required behavioral interfaces
4. Processes the payload
5. Creates new `BaseMessage` with transformed payload
6. Publishes to next component

### Payload Registry

The global payload registry maps type identifiers to factory functions:

```go
Registry: {
  "core.json.v1" → func() any { return &GenericJSONPayload{} }
  "robotics.position.v1" → func() any { return &PositionPayload{} }
  "iot.sensor.v2" → func() any { return &SensorPayload{} }
}
```

When `BaseMessage.UnmarshalJSON()` encounters `"type":"core.json.v1"`, it:

1. Looks up `"core.json.v1"` in the registry
2. Calls the factory to create an empty payload instance
3. Unmarshals the JSON data into that instance
4. Stores the typed instance in `payloadInstance`

This enables polymorphic deserialization without reflection or code generation.

### Integration Points

**NATS**: Messages serialize to JSON for NATS pub/sub:

- Subjects: `sensors.temperature`, `robots.position`, etc.
- Payload: Complete `BaseMessage` JSON

**Component Registry**: Components declare type compatibility:

- `input_types`: ["core.json.v1", "iot.sensor.v2"]
- `output_types`: ["core.json.v1"]

**Flow Engine**: Validates type compatibility in flow graphs before deployment

**Metrics**: Message counts tracked by type (via `Schema().String()`)

## Usage Patterns

### Creating Custom Payload Types

```go
package myapp

import (
    "time"
    "github.com/c360/semstreams/component"
    "github.com/c360/semstreams/message"
)

// Define your payload
type RobotPositionPayload struct {
    RobotID   string    `json:"robot_id"`
    Latitude  float64   `json:"latitude"`
    Longitude float64   `json:"longitude"`
    Timestamp time.Time `json:"timestamp"`
}

// Implement Payload interface
// PayloadType() is not part of the Payload interface
// Use Schema().String() to get the type identifier
// Schema() method is implemented above

func (p *RobotPositionPayload) Validate() error {
    // Structural validation: required fields
    if p.RobotID == "" {
        return errors.WrapInvalid(errors.ErrInvalidData, "RobotPositionPayload", "Validate", "robot_id is required")
    }

    // Semantic validation: value ranges
    if p.Latitude < -90 || p.Latitude > 90 {
        return errors.WrapInvalid(errors.ErrInvalidData, "RobotPositionPayload", "Validate",
            fmt.Sprintf("latitude must be between -90 and 90, got: %.6f", p.Latitude))
    }
    if p.Longitude < -180 || p.Longitude > 180 {
        return errors.WrapInvalid(errors.ErrInvalidData, "RobotPositionPayload", "Validate",
            fmt.Sprintf("longitude must be between -180 and 180, got: %.6f", p.Longitude))
    }
    return nil
}

// Implement behavioral interfaces
func (p *RobotPositionPayload) Location() (float64, float64) {
    return p.Latitude, p.Longitude
}

func (p *RobotPositionPayload) Timestamp() time.Time {
    return p.Timestamp
}

func (p *RobotPositionPayload) EntityID() string {
    return p.RobotID
}

func (p *RobotPositionPayload) EntityType() string {
    return "robot"
}

// Register with global registry
func init() {
    err := component.RegisterPayload(&component.PayloadRegistration{
        Domain:      "robotics",
        Category:    "position",
        Version:     "v1",
        Description: "Robot geographic position with timestamp",
        Factory: func() any {
            return &RobotPositionPayload{}
        },
        Example: RobotPositionPayload{
            RobotID:   "robot-001",
            Latitude:  40.7128,
            Longitude: -74.0060,
            Timestamp: time.Now(),
        },
    })
    if err != nil {
        panic(err)
    }
}
```

### Writing Behavior-Based Processors

```go
// Processor that works with ANY payload that is Locatable
type GeofenceProcessor struct {
    center message.Location
    radius float64
}

func (p *GeofenceProcessor) Process(msg *message.BaseMessage) (*message.BaseMessage, error) {
    // Type-assert to Locatable behavior
    locatable, ok := msg.Payload().(message.Locatable)
    if !ok {
        return nil, fmt.Errorf("payload must be Locatable")
    }

    lat, lon := locatable.Location()
    distance := p.calculateDistance(lat, lon, p.center.Lat, p.center.Lon)

    if distance > p.radius {
        // Outside geofence - create alert payload
        alertPayload := message.NewGenericJSON(map[string]any{
            "alert_type": "geofence_breach",
            "distance": distance,
            "threshold": p.radius,
        })
        return message.NewBaseMessage(alertPayload), nil
    }

    // Inside geofence - pass through
    return msg, nil
}
```

This processor works with `RobotPositionPayload`, `VehicleLocationPayload`, or ANY payload implementing `Locatable`.

### GenericJSON for Prototyping

Use `GenericJSONPayload` for rapid iteration:

```go
// Quick prototype - no custom types needed
func createTestMessage() *message.BaseMessage {
    payload := message.NewGenericJSON(map[string]any{
        "test_id": "test-001",
        "status": "running",
        "metrics": map[string]any{
            "cpu": 45.2,
            "memory": 67.8,
        },
    })
    return message.NewBaseMessage(payload)
}

// Process in a flow
func processTestData(msg *message.BaseMessage) {
    if genericJSON, ok := msg.Payload().(*message.GenericJSONPayload); ok {
        metrics := genericJSON.Data["metrics"].(map[string]any)
        cpu := metrics["cpu"].(float64)

        if cpu > 80.0 {
            // Alert high CPU
        }
    }
}
```

**When to use GenericJSON**:

- ✅ Rapid prototyping and iteration
- ✅ Integration tests with flexible schemas
- ✅ ETL pipelines with varying structures
- ✅ JSON transformation workflows

**When NOT to use GenericJSON**:

- ❌ Production systems with strict schemas
- ❌ Type-safe domain models
- ❌ Performance-critical paths (use typed payloads)

## Testing

### Testing Behavioral Interfaces

```go
func TestProcessorWithLocatable(t *testing.T) {
    // Create mock Locatable payload
    type MockLocatable struct {
        Lat, Lon float64
    }

    // Schema() implements Payload interface - Schema().String() returns type ID
    func (m *MockLocatable) Validate() error { return nil }
    func (m *MockLocatable) Location() (float64, float64) { return m.Lat, m.Lon }

    // Register mock type
    component.RegisterPayload(&component.PayloadRegistration{
        Domain: "mock",
        Category: "locatable",
        Version: "v1",
        Factory: func() any { return &MockLocatable{} },
    })

    // Test processor
    payload := &MockLocatable{Lat: 40.7, Lon: -74.0}
    msg := message.NewBaseMessage(payload)

    processor := NewGeofenceProcessor(...)
    result, err := processor.Process(msg)

    require.NoError(t, err)
    assert.NotNil(t, result)
}
```

### Testing Round-Trip Serialization

```go
func TestMessageRoundTrip(t *testing.T) {
    // Create original message
    original := message.NewGenericJSON(map[string]any{
        "test": "value",
        "number": 42,
    })
    originalMsg := message.NewBaseMessage(original)

    // Marshal to JSON
    data, err := json.Marshal(originalMsg)
    require.NoError(t, err)

    // Unmarshal back
    var reconstructed message.BaseMessage
    err = json.Unmarshal(data, &reconstructed)
    require.NoError(t, err)

    // Verify type and payload
    assert.Equal(t, "core.json.v1", reconstructed.Type)
    payload := reconstructed.Payload().(*message.GenericJSONPayload)
    assert.Equal(t, "value", payload.Data["test"])
    assert.Equal(t, float64(42), payload.Data["number"])
}
```

## Implementation Notes

### Thread Safety

**Messages are immutable after creation**:

- `BaseMessage` fields set once during construction or unmarshaling
- Payload instances should be immutable or use copy-on-write
- Safe for concurrent reads from multiple goroutines
- **NOT safe** for concurrent modification

**Global registry is thread-safe**:

- Payload registry uses internal synchronization
- Safe to register types concurrently (during `init()`)
- Safe to create payloads concurrently

### Performance

**Polymorphic deserialization overhead**:

- Registry lookup: O(1) map access (~10ns)
- Type assertion: Zero-cost Go interface check
- JSON unmarshaling: Standard library performance

**For high-throughput scenarios** (>10K msg/sec):

- Use typed payloads (avoid `map[string]any`)
- Pool `BaseMessage` instances if needed
- Consider binary serialization (protobuf, msgpack)

**Benchmarks** (typical hardware):

```shell
BenchmarkBaseMessage_Marshal       1000000   1200 ns/op
BenchmarkBaseMessage_Unmarshal     800000    1500 ns/op
BenchmarkTypedPayload_Marshal      2000000    600 ns/op
BenchmarkTypedPayload_Unmarshal    1500000    900 ns/op
```

### Error Handling

Uses SemStreams error classification:

```go
import "github.com/c360/semstreams/errors"

// Invalid input (malformed JSON, unknown type)
errors.WrapInvalid(err, "BaseMessage", "UnmarshalJSON", "validate type format")

// Programming errors (factory returns wrong type)
errors.WrapFatal(err, "BaseMessage", "UnmarshalJSON", "factory contract violation")

// General errors (JSON marshaling)
errors.Wrap(err, "BaseMessage", "MarshalJSON", "marshal payload")
```

Error classification enables retry logic and proper error handling in components.

### Security Considerations

**Input Validation**:

- Payload types validated against "domain.category.version" format
- Unknown types rejected (must be registered)
- Factory contract enforced (must return Payload instance)
- Payload-specific validation via `Validate()` method

**GenericJSON Limitations**:

- No built-in size limits (enforce at transport layer)
- No depth limits (enforce at transport layer)
- No schema validation (use typed payloads for strict schemas)

**Best Practices**:

- Validate payloads in components before processing
- Enforce message size limits at NATS/transport level
- Use typed payloads for production systems
- Implement `Validate()` thoroughly for custom payloads

## Common Patterns

### Enrichment Pattern

```go
// Add metadata to existing message
func enrichMessage(msg *message.BaseMessage) (*message.BaseMessage, error) {
    // Extract original data
    original := msg.Payload().(*message.GenericJSONPayload)

    // Create enriched payload
    enriched := message.NewGenericJSON(map[string]any{
        "original": original.Data,
        "enriched_at": time.Now(),
        "enrichment_version": "v1",
    })

    return message.NewBaseMessage(enriched), nil
}
```

### Transformation Pattern

```go
// Transform payload to different type
func transformToAlert(msg *message.BaseMessage) (*message.BaseMessage, error) {
    // Type-assert to source type
    if obs, ok := msg.Payload().(message.Observable); ok {
        value := obs.ObservedValue()

        if value > threshold {
            // Create alert payload (custom type)
            alert := &AlertPayload{
                AlertType: "threshold_exceeded",
                Source: obs.ObservedEntity(),
                Value: value,
                Timestamp: time.Now(),
            }
            return message.NewBaseMessage(alert), nil
        }
    }
    return nil, nil // No alert needed
}
```

### Filtering Pattern

```go
// Filter messages based on behavioral capabilities
func filterLocatableOnly(msgs []*message.BaseMessage) []*message.BaseMessage {
    var result []*message.BaseMessage
    for _, msg := range msgs {
        if _, ok := msg.Payload().(message.Locatable); ok {
            result = append(result, msg)
        }
    }
    return result
}
```

## Migration from Legacy Systems

If migrating from string-based message types:

```go
// Old: Type in payload
type OldMessage struct {
    Type    string
    Payload json.RawMessage
}

// New: Use BaseMessage
func migrateOldMessage(old *OldMessage) (*message.BaseMessage, error) {
    // Map old types to new type identifiers
    typeMap := map[string]string{
        "temperature": "iot.sensor.v1",
        "position": "robotics.position.v1",
    }

    newType, ok := typeMap[old.Type]
    if !ok {
        // Fall back to GenericJSON
        var data map[string]any
        json.Unmarshal(old.Payload, &data)
        payload := message.NewGenericJSON(data)
        return message.NewBaseMessage(payload), nil
    }

    // Reconstruct as BaseMessage
    envelope := fmt.Sprintf(`{"type":"%s","payload":%s}`, newType, old.Payload)
    var msg message.BaseMessage
    err := json.Unmarshal([]byte(envelope), &msg)
    return &msg, err
}
```

## Troubleshooting

### "unknown payload type" Error

**Problem**: `UnmarshalJSON` fails with "unknown payload type: X.Y.Z"

**Solution**: Ensure the payload type is registered with the global registry:

```go
func init() {
    component.RegisterPayload(&component.PayloadRegistration{
        Domain: "X",
        Category: "Y",
        Version: "Z",
        Factory: func() any { return &MyPayload{} },
    })
}
```

### Type Assertion Fails

**Problem**: Type assertion returns `ok=false`

**Debugging**:

```go
// Check actual type
payload := msg.Payload()
fmt.Printf("Payload type: %T\n", payload)
fmt.Printf("Payload type ID: %s\n", payload.Schema().String())

// Check behavioral interface
if locatable, ok := payload.(message.Locatable); ok {
    fmt.Println("Payload IS Locatable")
} else {
    fmt.Println("Payload is NOT Locatable")
    // Check what interfaces it does implement
}
```

### JSON Deserialization Fails

**Problem**: `UnmarshalJSON` succeeds but payload fields are zero-valued

**Cause**: JSON field names don't match struct tags

**Solution**: Ensure struct tags match JSON field names:

```go
// Wrong
type MyPayload struct {
    Value string // Missing json tag
}

// Correct
type MyPayload struct {
    Value string `json:"value"`
}
```

## Examples

See `*_test.go` files for comprehensive examples:

- `base_message_test.go` - Round-trip serialization, behavioral interfaces
- `generic_json_test.go` - GenericJSON usage, nested structures
- `behaviors.go` - Behavioral interface definitions with documentation

## References

- [SemStreams Component Package](../component/) - Component registry and payload registration
- [SemStreams Errors Package](../errors/) - Error classification system
- [Flow Engine](../engine/) - Type validation and flow deployment
- [NATS Client](../natsclient/) - Message transport layer

## Contributing

When adding new behavioral interfaces:

1. Keep interfaces small (1-3 methods)
2. Document use cases clearly
3. Provide example implementations
4. Add tests demonstrating usage
5. Update this README with the new interface

When adding new payload types:

1. Implement `Payload` interface
2. Implement relevant behavioral interfaces
3. Register in `init()` function
4. Add comprehensive tests
5. Document type identifier format
