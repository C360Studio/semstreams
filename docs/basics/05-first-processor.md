# Building Your First Processor

This tutorial walks through building a complete domain processor from scratch. We'll use IoT sensor
readings as the example domain.

> **Working Example**: See [`/examples/processors/iot_sensor/`](../../examples/processors/iot_sensor/)
> for the complete, runnable implementation of everything in this tutorial.

## What You're Building

A processor that:

1. Receives sensor readings (temperature, humidity, pressure)
2. Transforms them into graph entities with semantic triples
3. Creates relationships to zone entities
4. Integrates with the component framework for NATS messaging

## Files You'll Create

| File | Purpose | Lines |
|------|---------|-------|
| `vocabulary.go` | Predicate constants + vocabulary registration | ~80 |
| `payload.go` | Graphable structs + init() for payload registry | ~200 |
| `processor.go` | Transformation logic + helper functions | ~150 |
| `component.go` | NATS integration + lifecycle management | ~400 |
| `register.go` | Component registry hook | ~25 |
| `processor_test.go` | Unit tests for transformation logic | ~100 |
| `payload_test.go` | Graphable contract tests | ~100 |
| `component_test.go` | Component creation and registration tests | ~80 |

## Part 1: Design Your Vocabulary

Start by defining the predicates that describe facts about your entities. This shapes your entire domain model.

Create `vocabulary.go`:

```go
package iotsensor

import "github.com/c360studio/semstreams/vocabulary"

func init() {
    // Auto-register vocabulary when package is imported
    RegisterVocabulary()
}

// Predicate constants for the IoT sensor domain.
// These follow the three-level dotted notation: domain.category.property
const (
    // Sensor measurement predicates (unit-specific)
    PredicateMeasurementCelsius    = "sensor.measurement.celsius"
    PredicateMeasurementFahrenheit = "sensor.measurement.fahrenheit"
    PredicateMeasurementPercent    = "sensor.measurement.percent"
    PredicateMeasurementHPA        = "sensor.measurement.hpa"

    // Sensor classification predicates
    PredicateClassificationType = "sensor.classification.type"

    // Geo location predicates
    PredicateLocationZone      = "geo.location.zone"
    PredicateLocationLatitude  = "geo.location.latitude"
    PredicateLocationLongitude = "geo.location.longitude"

    // Time observation predicates
    PredicateObservationRecorded = "time.observation.recorded"

    // Facility zone predicates
    PredicateZoneName = "facility.zone.name"
    PredicateZoneType = "facility.zone.type"

    // Sensor identity predicates (for ALIAS_INDEX)
    PredicateSensorSerial = "iot.sensor.serial"
)

// RegisterVocabulary registers all IoT sensor domain predicates with the vocabulary
// system. This is called automatically via init().
func RegisterVocabulary() {
    vocabulary.Register(PredicateMeasurementCelsius,
        vocabulary.WithDescription("Temperature reading in Celsius"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithUnits("celsius"),
    )

    vocabulary.Register(PredicateMeasurementPercent,
        vocabulary.WithDescription("Percentage measurement (e.g., humidity)"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithUnits("percent"),
        vocabulary.WithRange("0-100"),
    )

    vocabulary.Register(PredicateClassificationType,
        vocabulary.WithDescription("Sensor type classification"),
        vocabulary.WithDataType("string"),
    )

    vocabulary.Register(PredicateLocationZone,
        vocabulary.WithDescription("Reference to zone entity where sensor is located"),
        vocabulary.WithDataType("entity_ref"),
    )

    vocabulary.Register(PredicateLocationLatitude,
        vocabulary.WithDescription("GPS latitude coordinate"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithRange("-90 to 90"),
    )

    vocabulary.Register(PredicateLocationLongitude,
        vocabulary.WithDescription("GPS longitude coordinate"),
        vocabulary.WithDataType("float64"),
        vocabulary.WithRange("-180 to 180"),
    )

    vocabulary.Register(PredicateObservationRecorded,
        vocabulary.WithDescription("Timestamp when observation was recorded"),
        vocabulary.WithDataType("timestamp"),
    )

    vocabulary.Register(PredicateZoneName,
        vocabulary.WithDescription("Human-readable name of the zone"),
        vocabulary.WithDataType("string"),
    )

    vocabulary.Register(PredicateZoneType,
        vocabulary.WithDescription("Type classification of the zone"),
        vocabulary.WithDataType("string"),
    )

    vocabulary.Register(PredicateSensorSerial,
        vocabulary.WithDescription("Manufacturer serial number"),
        vocabulary.WithDataType("string"),
        vocabulary.WithAlias(vocabulary.AliasTypeExternal, 0),
    )
}
```

**Vocabulary design principles:**

- Use three-part dotted notation: `domain.category.property`
- Include units in measurement predicates for clarity
- Distinguish property predicates (literal values) from relationship predicates (entity references)
- Use constants—not string literals—to prevent typos
- Register with the vocabulary system for discoverability

See [Vocabulary](04-vocabulary.md) for complete design guidelines.

## Part 2: Define Your Payload

With your vocabulary defined, create the structs that hold your domain data. These must implement the `Graphable` interface.

Create `payload.go`:

```go
package iotsensor

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/c360studio/semstreams/message"
    "github.com/c360studio/semstreams/payloadregistry"
)

// init registers the SensorReading payload type with the global PayloadRegistry.
// This enables BaseMessage.UnmarshalJSON to recreate SensorReading payloads
// from JSON when the message type is "iot.sensor.v1".
//
// CRITICAL: Without this registration, JSON deserialization will fail silently.
func init() {
    err := payloadregistry.Register(&payloadregistry.Registration{
        Domain:      "iot",
        Category:    "sensor",
        Version:     "v1",
        Description: "IoT sensor reading payload with Graphable implementation",
        Factory: func() any {
            return &SensorReading{}
        },
        Example: map[string]any{
            "DeviceID":   "sensor-042",
            "SensorType": "temperature",
            "Value":      23.5,
            "Unit":       "celsius",
        },
    })
    if err != nil {
        panic("failed to register SensorReading payload: " + err.Error())
    }
}

// SensorReading represents an IoT sensor measurement.
type SensorReading struct {
    // Input fields (from incoming JSON)
    DeviceID   string    `json:"device_id"`
    SensorType string    `json:"sensor_type"`
    Value      float64   `json:"value"`
    Unit       string    `json:"unit"`
    ObservedAt time.Time `json:"observed_at"`

    // Optional fields
    SerialNumber string   `json:"serial_number,omitempty"`
    Latitude     *float64 `json:"latitude,omitempty"`
    Longitude    *float64 `json:"longitude,omitempty"`

    // Entity reference (computed by processor)
    ZoneEntityID string `json:"zone_entity_id"`

    // Context fields (set by processor from config)
    OrgID    string `json:"org_id"`
    Platform string `json:"platform"`
}

// EntityID returns a deterministic 6-part federated entity ID.
// Format: {org}.{platform}.{domain}.{system}.{type}.{instance}
// Example: "acme.logistics.environmental.sensor.temperature.sensor-042"
func (s *SensorReading) EntityID() string {
    return fmt.Sprintf("%s.%s.environmental.sensor.%s.%s",
        s.OrgID,
        s.Platform,
        s.SensorType,
        s.DeviceID,
    )
}

// Triples returns semantic facts about this sensor reading.
func (s *SensorReading) Triples() []message.Triple {
    entityID := s.EntityID()

    triples := []message.Triple{
        // Measurement value with unit-specific predicate
        {
            Subject:    entityID,
            Predicate:  fmt.Sprintf("sensor.measurement.%s", s.Unit),
            Object:     s.Value,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        },
        // Sensor type classification
        {
            Subject:    entityID,
            Predicate:  PredicateClassificationType,
            Object:     s.SensorType,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        },
        // Location as entity reference (not string!)
        {
            Subject:    entityID,
            Predicate:  PredicateLocationZone,
            Object:     s.ZoneEntityID,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        },
        // Observation timestamp
        {
            Subject:    entityID,
            Predicate:  PredicateObservationRecorded,
            Object:     s.ObservedAt,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        },
    }

    // Optional: Serial number for ALIAS_INDEX
    if s.SerialNumber != "" {
        triples = append(triples, message.Triple{
            Subject:    entityID,
            Predicate:  PredicateSensorSerial,
            Object:     s.SerialNumber,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        })
    }

    // Optional: Geospatial data for SPATIAL_INDEX
    if s.Latitude != nil && s.Longitude != nil {
        triples = append(triples, message.Triple{
            Subject:    entityID,
            Predicate:  PredicateLocationLatitude,
            Object:     *s.Latitude,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        })
        triples = append(triples, message.Triple{
            Subject:    entityID,
            Predicate:  PredicateLocationLongitude,
            Object:     *s.Longitude,
            Source:     "iot_sensor",
            Timestamp:  s.ObservedAt,
            Confidence: 1.0,
        })
    }

    return triples
}

// Schema returns the message type for sensor readings.
// This must match the PayloadRegistration in init().
func (s *SensorReading) Schema() message.Type {
    return message.Type{
        Domain:   "iot",
        Category: "sensor",
        Version:  "v1",
    }
}

// Validate checks that the sensor reading has all required fields.
func (s *SensorReading) Validate() error {
    if s.DeviceID == "" {
        return fmt.Errorf("device_id is required")
    }
    if s.SensorType == "" {
        return fmt.Errorf("sensor_type is required")
    }
    if s.Unit == "" {
        return fmt.Errorf("unit is required")
    }
    if s.OrgID == "" {
        return fmt.Errorf("org_id is required")
    }
    if s.Platform == "" {
        return fmt.Errorf("platform is required")
    }
    return nil
}

// MarshalJSON implements json.Marshaler.
func (s *SensorReading) MarshalJSON() ([]byte, error) {
    type Alias SensorReading
    return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SensorReading) UnmarshalJSON(data []byte) error {
    type Alias SensorReading
    return json.Unmarshal(data, (*Alias)(s))
}
```

**Key points:**

- `EntityID()` returns exactly 6 parts, deterministic (same input = same output)
- `Triples()` uses vocabulary constants, not string literals
- `Object: s.ZoneEntityID` (entity ID string) = relationship edge
- `Object: s.Value` (a number) = property value
- `Schema()` must match the `PayloadRegistration` domain/category/version
- `init()` registers with the payload registry for JSON deserialization

## Part 3: Handle Related Entities

When your entity references another entity, you need that entity to exist. Create a Zone type:

```go
// ZoneEntityID generates a federated 6-part entity ID for a zone.
// Use this helper to ensure consistency between Zone.EntityID() and references.
func ZoneEntityID(orgID, platform, zoneType, zoneID string) string {
    return fmt.Sprintf("%s.%s.facility.zone.%s.%s",
        orgID, platform, zoneType, zoneID)
}

// Zone represents a location zone entity.
type Zone struct {
    ZoneID   string
    ZoneType string
    Name     string
    OrgID    string
    Platform string
}

func (z *Zone) EntityID() string {
    return ZoneEntityID(z.OrgID, z.Platform, z.ZoneType, z.ZoneID)
}

func (z *Zone) Triples() []message.Triple {
    entityID := z.EntityID()
    now := time.Now()

    return []message.Triple{
        {
            Subject:    entityID,
            Predicate:  PredicateZoneName,
            Object:     z.Name,
            Source:     "iot_sensor",
            Timestamp:  now,
            Confidence: 1.0,
        },
        {
            Subject:    entityID,
            Predicate:  PredicateZoneType,
            Object:     z.ZoneType,
            Source:     "iot_sensor",
            Timestamp:  now,
            Confidence: 1.0,
        },
    }
}

func (z *Zone) Schema() message.Type {
    return message.Type{Domain: "facility", Category: "zone", Version: "v1"}
}

func (z *Zone) Validate() error {
    if z.ZoneID == "" {
        return fmt.Errorf("zone_id is required")
    }
    if z.OrgID == "" {
        return fmt.Errorf("org_id is required")
    }
    if z.Platform == "" {
        return fmt.Errorf("platform is required")
    }
    return nil
}
```

## Part 4: Implement the Processor

The processor transforms raw JSON into your Graphable payload. Create `processor.go`:

```go
package iotsensor

import (
    "errors"
    "fmt"
    "time"
)

// Config holds the configuration for the processor.
type Config struct {
    OrgID    string
    Platform string
}

func (c Config) Validate() error {
    if c.OrgID == "" {
        return errors.New("OrgID is required")
    }
    if c.Platform == "" {
        return errors.New("Platform is required")
    }
    return nil
}

// Processor transforms incoming JSON sensor data into Graphable payloads.
type Processor struct {
    config Config
}

func NewProcessor(config Config) *Processor {
    return &Processor{config: config}
}

// Process transforms incoming JSON data into a SensorReading.
//
// Expected JSON format:
//
//    {
//      "device_id": "sensor-042",
//      "type": "temperature",
//      "reading": 23.5,
//      "unit": "celsius",
//      "location": "warehouse-7",
//      "timestamp": "2025-11-26T10:30:00Z"
//    }
func (p *Processor) Process(input map[string]any) (*SensorReading, error) {
    deviceID, err := getString(input, "device_id")
    if err != nil {
        return nil, fmt.Errorf("missing device_id: %w", err)
    }

    sensorType, err := getString(input, "type")
    if err != nil {
        return nil, fmt.Errorf("missing type: %w", err)
    }

    value, err := getFloat64(input, "reading")
    if err != nil {
        return nil, fmt.Errorf("missing reading: %w", err)
    }

    unit, err := getString(input, "unit")
    if err != nil {
        return nil, fmt.Errorf("missing unit: %w", err)
    }

    locationID, err := getString(input, "location")
    if err != nil {
        return nil, fmt.Errorf("missing location: %w", err)
    }

    // Optional: zone type (default to "area")
    zoneType := "area"
    if zt, ok := input["zone_type"].(string); ok && zt != "" {
        zoneType = zt
    }

    // Optional: timestamp (default to now)
    var observedAt time.Time
    if ts, ok := input["timestamp"].(string); ok {
        parsed, err := time.Parse(time.RFC3339, ts)
        if err != nil {
            observedAt = time.Now()
        } else {
            observedAt = parsed
        }
    } else {
        observedAt = time.Now()
    }

    return &SensorReading{
        DeviceID:     deviceID,
        SensorType:   sensorType,
        Value:        value,
        Unit:         unit,
        ObservedAt:   observedAt,
        ZoneEntityID: ZoneEntityID(p.config.OrgID, p.config.Platform, zoneType, locationID),
        OrgID:        p.config.OrgID,
        Platform:     p.config.Platform,
    }, nil
}

// Helper functions for type-safe field extraction

func getString(m map[string]any, key string) (string, error) {
    v, ok := m[key]
    if !ok {
        return "", fieldNotFoundError(m, key)
    }
    s, ok := v.(string)
    if !ok {
        return "", fmt.Errorf("field %q is not a string: got %T", key, v)
    }
    return s, nil
}

func getFloat64(m map[string]any, key string) (float64, error) {
    v, ok := m[key]
    if !ok {
        return 0, fieldNotFoundError(m, key)
    }
    switch val := v.(type) {
    case float64:
        return val, nil
    case float32:
        return float64(val), nil
    case int:
        return float64(val), nil
    case int64:
        return float64(val), nil
    default:
        return 0, fmt.Errorf("field %q is not a number: got %T", key, v)
    }
}

// fieldNotFoundError returns a helpful error message suggesting similar field names.
func fieldNotFoundError(m map[string]any, key string) error {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }

    suggestions := findSimilarFields(key, keys)
    if len(suggestions) > 0 {
        return fmt.Errorf("field %q not found (did you mean %q?), available fields: %v",
            key, suggestions[0], keys)
    }
    return fmt.Errorf("field %q not found, available fields: %v", key, keys)
}

// findSimilarFields finds field names similar to the expected key.
func findSimilarFields(expected string, available []string) []string {
    var similar []string

    // Common field name mappings
    commonMistakes := map[string][]string{
        "type":      {"sensor_type", "sensorType", "kind"},
        "reading":   {"value", "val", "measurement", "data"},
        "location":  {"zone_id", "zoneId", "zone", "loc", "area"},
        "device_id": {"deviceId", "id", "sensor_id", "sensorId"},
        "unit":      {"units", "uom"},
    }

    if mistakes, ok := commonMistakes[expected]; ok {
        for _, mistake := range mistakes {
            for _, avail := range available {
                if avail == mistake {
                    similar = append(similar, avail)
                }
            }
        }
    }

    return similar
}
```

**Key points:**

- Processor applies organizational context from configuration
- Helper functions provide type-safe field extraction
- Error messages suggest correct field names when input is malformed

## Part 5: Test Your Domain Logic

Create `processor_test.go`:

```go
package iotsensor

import (
    "encoding/json"
    "testing"

    "github.com/c360studio/semstreams/graph"
    "github.com/c360studio/semstreams/message"
)

func TestProcessor_Process_JSONTransformation(t *testing.T) {
    p := NewProcessor(Config{OrgID: "acme", Platform: "logistics"})

    inputJSON := `{
        "device_id": "sensor-042",
        "type": "temperature",
        "reading": 23.5,
        "unit": "celsius",
        "location": "warehouse-7"
    }`

    var input map[string]any
    if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
        t.Fatalf("failed to unmarshal test input: %v", err)
    }

    result, err := p.Process(input)
    if err != nil {
        t.Fatalf("Process() unexpected error: %v", err)
    }

    // Verify result implements Graphable
    var _ graph.Graphable = result

    // Verify EntityID is valid 6-part format
    entityID := result.EntityID()
    if !message.IsValidEntityID(entityID) {
        t.Errorf("EntityID() = %q is not valid 6-part format", entityID)
    }

    // Verify Triples returns meaningful data
    triples := result.Triples()
    if len(triples) < 3 {
        t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
    }
}
```

Create `payload_test.go`:

```go
package iotsensor

import (
    "strings"
    "testing"
    "time"

    "github.com/c360studio/semstreams/graph"
    "github.com/c360studio/semstreams/message"
)

func TestSensorReading_EntityID_6PartFormat(t *testing.T) {
    reading := SensorReading{
        DeviceID:   "sensor-042",
        SensorType: "temperature",
        OrgID:      "acme",
        Platform:   "logistics",
    }

    entityID := reading.EntityID()
    parts := strings.Split(entityID, ".")

    if len(parts) != 6 {
        t.Errorf("EntityID() = %q has %d parts, want 6", entityID, len(parts))
    }

    if !message.IsValidEntityID(entityID) {
        t.Errorf("EntityID() = %q is not valid", entityID)
    }
}

func TestSensorReading_Triples_SemanticPredicates(t *testing.T) {
    reading := SensorReading{
        DeviceID:     "sensor-042",
        SensorType:   "temperature",
        Value:        23.5,
        Unit:         "celsius",
        ZoneEntityID: "acme.logistics.facility.zone.area.warehouse-7",
        ObservedAt:   time.Now(),
        OrgID:        "acme",
        Platform:     "logistics",
    }

    triples := reading.Triples()

    if len(triples) < 3 {
        t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
    }

    // Verify all triples reference this entity
    entityID := reading.EntityID()
    for i, triple := range triples {
        if triple.Subject != entityID {
            t.Errorf("Triple[%d].Subject = %q, want %q", i, triple.Subject, entityID)
        }
    }
}

// Compile-time check that types implement Graphable
func TestGraphableInterface(_ *testing.T) {
    var _ graph.Graphable = (*SensorReading)(nil)
    var _ graph.Graphable = (*Zone)(nil)
}
```

Run tests:

```bash
go test ./examples/processors/iot_sensor/...
```

## Part 6: Create the Component Wrapper

The component wrapper integrates your processor with the NATS messaging system. Create `component.go`:

```go
package iotsensor

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"
    "sync"
    "sync/atomic"
    "time"

    "github.com/c360studio/semstreams/component"
    "github.com/c360studio/semstreams/message"
    "github.com/c360studio/semstreams/natsclient"
    "github.com/c360studio/semstreams/pkg/errs"
    "github.com/nats-io/nats.go"
)

// ComponentConfig holds configuration for the component.
type ComponentConfig struct {
    Ports    *component.PortConfig `json:"ports"`
    OrgID    string                `json:"org_id"`
    Platform string                `json:"platform"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() ComponentConfig {
    return ComponentConfig{
        Ports: &component.PortConfig{
            Inputs: []component.PortDefinition{
                {
                    Name:        "nats_input",
                    Type:        "nats",
                    Subject:     "raw.sensor.>",
                    Required:    true,
                    Description: "NATS subjects with sensor JSON data",
                },
            },
            Outputs: []component.PortDefinition{
                {
                    Name:        "nats_output",
                    Type:        "nats",
                    Subject:     "events.graph.entity.sensor",
                    Required:    true,
                    Description: "NATS subject for Graphable sensor readings",
                },
            },
        },
        OrgID:    "default-org",
        Platform: "default-platform",
    }
}

var iotSensorSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component wraps the domain processor with component lifecycle.
type Component struct {
    name        string
    subjects    []string
    outputSubj  string
    config      ComponentConfig
    natsClient  *natsclient.Client
    logger      *slog.Logger
    processor   *Processor

    shutdown      chan struct{}
    done          chan struct{}
    running       bool
    startTime     time.Time
    mu            sync.RWMutex
    lifecycleMu   sync.Mutex
    wg            *sync.WaitGroup
    subscriptions []*natsclient.Subscription

    messagesProcessed int64
    errors            int64
    lastActivity      time.Time
}

// NewComponent creates a new component from configuration.
func NewComponent(
    rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
    var config ComponentConfig
    if err := json.Unmarshal(rawConfig, &config); err != nil {
        return nil, errs.WrapInvalid(err, "IoTSensorComponent", "NewComponent", "config unmarshal")
    }

    if config.Ports == nil {
        config = DefaultConfig()
    }

    if config.OrgID == "" {
        return nil, errs.WrapInvalid(
            errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent", "OrgID is required")
    }

    if config.Platform == "" {
        return nil, errs.WrapInvalid(
            errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent", "Platform is required")
    }

    var inputSubjects []string
    var outputSubject string

    for _, input := range config.Ports.Inputs {
        if input.Type == "nats" {
            inputSubjects = append(inputSubjects, input.Subject)
        }
    }

    if len(config.Ports.Outputs) > 0 {
        outputSubject = config.Ports.Outputs[0].Subject
    }

    processor := NewProcessor(Config{
        OrgID:    config.OrgID,
        Platform: config.Platform,
    })

    return &Component{
        name:       "iot-sensor-processor",
        subjects:   inputSubjects,
        outputSubj: outputSubject,
        config:     config,
        natsClient: deps.NATSClient,
        logger:     deps.GetLogger(),
        processor:  processor,
        shutdown:   make(chan struct{}),
        done:       make(chan struct{}),
        wg:         &sync.WaitGroup{},
    }, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
    return nil
}

// Start begins processing messages.
func (c *Component) Start(ctx context.Context) error {
    c.lifecycleMu.Lock()
    defer c.lifecycleMu.Unlock()

    if c.running {
        return errs.WrapFatal(errs.ErrAlreadyStarted, "IoTSensorComponent", "Start", "already running")
    }

    if c.natsClient == nil {
        return errs.WrapFatal(errs.ErrMissingConfig, "IoTSensorComponent", "Start", "NATS client required")
    }

    for _, subject := range c.subjects {
        sub, err := c.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
            c.handleMessage(ctx, msg.Data)
        })
        if err != nil {
            return errs.WrapTransient(err, "IoTSensorComponent", "Start",
                fmt.Sprintf("subscribe to %s", subject))
        }
        c.subscriptions = append(c.subscriptions, sub)
    }

    c.mu.Lock()
    c.running = true
    c.startTime = time.Now()
    c.mu.Unlock()

    c.logger.Info("IoT sensor processor started",
        "component", c.name,
        "input_subjects", c.subjects,
        "output_subject", c.outputSubj)

    return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(timeout time.Duration) error {
    c.lifecycleMu.Lock()
    defer c.lifecycleMu.Unlock()

    if !c.running {
        return nil
    }

    close(c.shutdown)

    for _, sub := range c.subscriptions {
        if err := sub.Unsubscribe(); err != nil {
            c.logger.Warn("Failed to unsubscribe", "error", err)
        }
    }
    c.subscriptions = nil

    waitCh := make(chan struct{})
    go func() {
        c.wg.Wait()
        close(waitCh)
    }()

    select {
    case <-waitCh:
    case <-time.After(timeout):
        return fmt.Errorf("shutdown timeout after %v", timeout)
    }

    c.mu.Lock()
    c.running = false
    close(c.done)
    c.mu.Unlock()

    return nil
}

// handleMessage processes incoming sensor JSON messages.
func (c *Component) handleMessage(ctx context.Context, msgData []byte) {
    atomic.AddInt64(&c.messagesProcessed, 1)
    c.mu.Lock()
    c.lastActivity = time.Now()
    c.mu.Unlock()

    var data map[string]any
    if err := json.Unmarshal(msgData, &data); err != nil {
        atomic.AddInt64(&c.errors, 1)
        c.logger.Debug("Failed to parse JSON", "error", err)
        return
    }

    reading, err := c.processor.Process(data)
    if err != nil {
        atomic.AddInt64(&c.errors, 1)
        c.logger.Error("Failed to process sensor data", "error", err)
        return
    }

    // Emit Zone entity first (referenced entity)
    if reading.ZoneEntityID != "" {
        zoneType, zoneID := ParseZoneEntityID(reading.ZoneEntityID)
        if zoneType != "" && zoneID != "" {
            zone := &Zone{
                ZoneID:   zoneID,
                ZoneType: zoneType,
                Name:     zoneID,
                OrgID:    reading.OrgID,
                Platform: reading.Platform,
            }
            c.emitEntity(ctx, zone, zone.Schema())
        }
    }

    // Emit SensorReading entity
    c.emitEntity(ctx, reading, reading.Schema())
}

// emitEntity wraps a payload in BaseMessage and publishes.
func (c *Component) emitEntity(ctx context.Context, payload message.Payload, msgType message.Type) {
    baseMsg := message.NewBaseMessage(msgType, payload, c.name)

    data, err := json.Marshal(baseMsg)
    if err != nil {
        atomic.AddInt64(&c.errors, 1)
        c.logger.Error("Failed to marshal BaseMessage", "error", err)
        return
    }

    if c.outputSubj != "" {
        if err := c.natsClient.Publish(ctx, c.outputSubj, data); err != nil {
            atomic.AddInt64(&c.errors, 1)
            c.logger.Error("Failed to publish entity", "error", err)
        }
    }
}

// Discoverable interface implementation

func (c *Component) Meta() component.Metadata {
    return component.Metadata{
        Name:        c.name,
        Type:        "processor",
        Description: "Transforms sensor JSON into Graphable payloads",
        Version:     "0.1.0",
    }
}

func (c *Component) InputPorts() []component.Port {
    ports := make([]component.Port, len(c.subjects))
    for i, subj := range c.subjects {
        ports[i] = component.Port{
            Name:      fmt.Sprintf("input_%d", i),
            Direction: component.DirectionInput,
            Required:  true,
            Config:    component.NATSPort{Subject: subj},
        }
    }
    return ports
}

func (c *Component) OutputPorts() []component.Port {
    return []component.Port{
        {
            Name:      "output",
            Direction: component.DirectionOutput,
            Required:  true,
            Config:    component.NATSPort{Subject: c.outputSubj},
        },
    }
}

func (c *Component) ConfigSchema() component.ConfigSchema {
    return iotSensorSchema
}

func (c *Component) Health() component.HealthStatus {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return component.HealthStatus{
        Healthy:    c.running,
        LastCheck:  time.Now(),
        ErrorCount: int(atomic.LoadInt64(&c.errors)),
        Uptime:     time.Since(c.startTime),
    }
}

func (c *Component) DataFlow() component.FlowMetrics {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return component.FlowMetrics{
        LastActivity: c.lastActivity,
    }
}
```

## Part 7: Register the Component

Create `register.go`:

```go
package iotsensor

import "github.com/c360studio/semstreams/component"

// Register registers the component with the registry.
func Register(registry *component.Registry) error {
    return registry.RegisterWithConfig(component.RegistrationConfig{
        Name:        "iot_sensor",
        Factory:     NewComponent,
        Schema:      iotSensorSchema,
        Type:        "processor",
        Protocol:    "iot_sensor",
        Domain:      "iot",
        Description: "Transforms sensor JSON into Graphable payloads",
        Version:     "0.1.0",
    })
}
```

For built-in processors, add the registration call to your component registry initialization.

### Test Your Component

Create `component_test.go` to verify the component creation and registration:

```go
package iotsensor

import (
    "encoding/json"
    "testing"

    "github.com/c360studio/semstreams/component"
)

func TestNewComponent_ValidConfig(t *testing.T) {
    config := ComponentConfig{
        OrgID:    "acme",
        Platform: "logistics",
        Ports: &component.PortConfig{
            Inputs: []component.PortDefinition{
                {Name: "input", Type: "nats", Subject: "raw.sensor.>"},
            },
            Outputs: []component.PortDefinition{
                {Name: "output", Type: "nats", Subject: "events.graph.entity.sensor"},
            },
        },
    }

    rawConfig, err := json.Marshal(config)
    if err != nil {
        t.Fatalf("failed to marshal config: %v", err)
    }

    deps := component.Dependencies{} // NATSClient not needed for creation test

    comp, err := NewComponent(rawConfig, deps)
    if err != nil {
        t.Fatalf("NewComponent() unexpected error: %v", err)
    }

    // Verify it implements Discoverable
    meta := comp.Meta()
    if meta.Type != "processor" {
        t.Errorf("Meta().Type = %q, want processor", meta.Type)
    }
}

func TestNewComponent_MissingOrgID(t *testing.T) {
    config := ComponentConfig{
        Platform: "logistics",
        Ports: &component.PortConfig{
            Inputs:  []component.PortDefinition{{Name: "input", Type: "nats", Subject: "raw.>"}},
            Outputs: []component.PortDefinition{{Name: "output", Type: "nats", Subject: "out.>"}},
        },
    }

    rawConfig, _ := json.Marshal(config)
    deps := component.Dependencies{}

    _, err := NewComponent(rawConfig, deps)
    if err == nil {
        t.Error("NewComponent() expected error for missing OrgID, got nil")
    }
}

func TestRegister(t *testing.T) {
    registry := component.NewRegistry()

    err := Register(registry)
    if err != nil {
        t.Fatalf("Register() error: %v", err)
    }

    // Verify it was registered
    factory, ok := registry.GetFactory("iot_sensor")
    if !ok || factory == nil {
        t.Error("Expected component to be registered with factory")
    }
}
```

## Part 8: Wire It Up

Add your processor to a flow configuration:

```json
{
  "components": {
    "iot_sensor": {
      "type": "iot_sensor",
      "config": {
        "org_id": "acme",
        "platform": "logistics",
        "ports": {
          "inputs": [
            {
              "name": "nats_input",
              "type": "nats",
              "subject": "raw.sensor.>"
            }
          ],
          "outputs": [
            {
              "name": "nats_output",
              "type": "nats",
              "subject": "events.graph.entity.sensor"
            }
          ]
        }
      }
    }
  }
}
```

## Part 9: Test End-to-End

```bash
# Start SemStreams with your config
task dev:start

# Send test data
task dev:send DATA='{"device_id":"sensor-001","type":"temperature","reading":23.5,"unit":"celsius","location":"warehouse-7"}'

# Check message flow
task dev:stats

# Query via GraphQL
task dev:graphql QUERY='{ entity(id: "acme.logistics.environmental.sensor.temperature.sensor-001") { triples { predicate object } } }'
```

## What Happens Next

When your processor runs:

1. **Entity Storage**: The entity is stored in `ENTITY_STATES` KV bucket
2. **Predicate Index**: Each predicate creates an entry in `PREDICATE_INDEX`
3. **Relationship Index**: The zone reference creates entries in `INCOMING_INDEX` and `OUTGOING_INDEX`
4. **Community Detection**: If enabled, entities with relationships cluster together

## Common Patterns

### Conditional Triples

Add triples only when data is present:

```go
func (s *Sensor) Triples() []message.Triple {
    triples := []message.Triple{...}

    if s.SerialNumber != "" {
        triples = append(triples, message.Triple{
            Subject:   s.EntityID(),
            Predicate: PredicateSensorSerial,
            Object:    s.SerialNumber,
        })
    }

    return triples
}
```

### Dynamic Predicates

Include unit in the predicate for clarity:

```go
// Results in: sensor.measurement.celsius, sensor.measurement.fahrenheit, etc.
Predicate: fmt.Sprintf("sensor.measurement.%s", s.Unit)
```

### Geospatial Data

Add lat/lon for spatial indexing:

```go
if s.Latitude != nil && s.Longitude != nil {
    triples = append(triples,
        message.Triple{
            Subject:   s.EntityID(),
            Predicate: PredicateLocationLatitude,
            Object:    *s.Latitude,
        },
        message.Triple{
            Subject:   s.EntityID(),
            Predicate: PredicateLocationLongitude,
            Object:    *s.Longitude,
        },
    )
}
```

## Checklist

Before deploying your processor:

- [ ] EntityID returns exactly 6 parts
- [ ] EntityID is deterministic (same input = same output)
- [ ] Predicates use dotted notation (domain.category.property)
- [ ] Entity references use full entity IDs, not partial strings
- [ ] Required fields are validated
- [ ] Constants defined for all predicates
- [ ] Vocabulary registered with vocabulary.Register()
- [ ] Payload registered in init() with payloadregistry.Register()
- [ ] Schema() matches payload registration domain/category/version
- [ ] MarshalJSON and UnmarshalJSON implemented
- [ ] Validate() checks all required fields
- [ ] Component implements Discoverable interface
- [ ] Component registered via Register() function
- [ ] Tests cover EntityID, Triples, and processor transformation
- [ ] Config file integration tested end-to-end

## Next Steps

- [Configuration](06-configuration.md) - Choose your capability level
- [Index Reference](../advanced/05-index-reference.md) - How triples become queryable
- [Testing Guide](../contributing/01-testing.md#testing-graphable-implementations) - Test your Graphable implementations
