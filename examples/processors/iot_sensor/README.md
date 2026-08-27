# IoT Sensor Example Processor

This package demonstrates the correct pattern for implementing domain-specific processors in SemStreams. It shows how to create payloads that implement the `Graphable` interface with proper federated entity IDs and semantic predicates.

## Why Domain Processors Matter

SemStreams exists to help developers transform incoming data into meaningful semantic graphs. The transformation step is where **domain knowledge lives** - it cannot be automated away by generic processors.

Generic processors that automatically convert JSON to entities:

- Make semantic decisions without domain understanding
- Produce low-quality, auto-generated triples
- Create entity IDs without federated structure
- Derive predicates from JSON keys, not semantic meaning
- Treat relationships as strings, not entity references

Domain processors encode your understanding of the data.

## The Graphable Interface

Every domain payload must implement:

```go
type Graphable interface {
    EntityID() string      // 6-part federated identifier
    Triples() []Triple     // Semantic facts about the entity
}
```

## Using This Example

### 1. Copy and Adapt

Copy this package to your domain repository:

```bash
cp -r examples/processors/iot_sensor/ your-repo/processors/your_domain/
```

### 2. Define Your Payload

Replace `SensorReading` with your domain entity:

```go
type YourPayload struct {
    // Input fields from incoming data
    ID        string
    Type      string
    // ... domain-specific fields

    // Context fields (from processor config)
    OrgID    string
    Platform string
}
```

### 3. Implement EntityID

Return a deterministic 6-part federated ID:

```go
func (p *YourPayload) EntityID() string {
    // {org}.{platform}.{system}.{domain}.{type}.{instance}
    return fmt.Sprintf("%s.%s.yourdomain.system.%s.%s",
        p.OrgID,
        p.Platform,
        p.Type,
        p.ID,
    )
}
```

### 4. Implement Triples

Return semantic facts using registered predicates:

```go
func (p *YourPayload) Triples() []message.Triple {
    entityID := p.EntityID()
    return []message.Triple{
        {
            Subject:   entityID,
            Predicate: "yourdomain.category.property",
            Object:    p.SomeValue,
            // ...
        },
        // Entity references, not strings!
        {
            Subject:   entityID,
            Predicate: "yourdomain.relationship.type",
            Object:    p.relatedEntityID(), // Another entity ID
        },
    }
}
```

### 5. Register Your Predicates

Create a vocabulary file with your domain predicates:

```go
func RegisterVocabulary() {
    vocabulary.Register("yourdomain.category.property",
        vocabulary.WithDescription("Description of this predicate"),
        vocabulary.WithDataType("float64"),
    )
    // ... more predicates
}
```

## Files in This Package

| File | Purpose |
|------|---------|
| `payload.go` | `SensorReading` and `Zone` implementing Graphable |
| `payload_test.go` | Tests verifying Graphable contract |
| `processor.go` | JSON transformation with domain logic |
| `processor_test.go` | Processor unit tests |
| `vocabulary.go` | IoT predicate registration |
| `README.md` | This file |

## Running Tests

```bash
go test -race ./examples/processors/iot_sensor/...
```

## Key Patterns Demonstrated

### Unit-Specific Predicates

```go
// Instead of generic "value" + "unit" triples:
Predicate: measurementPredicateByUnit[s.Unit]
// Produces only registered predicates such as sensor.measurement.celsius.
```

### Entity References

```go
// Instead of location as string:
Predicate: "geo.location.zone"
Object:    s.zoneEntityID()  // Another 6-part entity ID
```

### Classification Triples

```go
// Add domain knowledge about the entity:
Predicate: "sensor.classification.type"
Object:    s.SensorType
```

## Further Reading

- [PROCESSOR-DESIGN-PHILOSOPHY.md](/docs/PROCESSOR-DESIGN-PHILOSOPHY.md) - Core philosophy
- [SPEC-SEMANTIC-CONTRACT.md](/docs/SPEC-SEMANTIC-CONTRACT.md) - Semantic contract proposal
- [vocabulary/](/vocabulary/) - Predicate registration system
