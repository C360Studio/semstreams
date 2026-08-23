# Custom Rules

> **⚠️ DEPRECATED**: The JSON-based rules engine is superseded by the Reactive Workflow Engine
> (ADR-021). For custom logic, use typed `ConditionFunc` in Go code.
> See [Reactive Workflows Guide](/docs/advanced/10-reactive-workflows.md).

Extend the rules engine with custom rule types using the factory pattern.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Rule Factory Registry                    │
├─────────────────────────────────────────────────────────────┤
│  "expression" → ExpressionRuleFactory                        │
│  "threshold"  → ThresholdRuleFactory                         │
│  "custom"     → YourCustomRuleFactory                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   CreateRuleFromDefinition()                 │
│  1. Look up factory by type                                  │
│  2. Validate definition                                      │
│  3. Create rule instance                                     │
└─────────────────────────────────────────────────────────────┘
```

## Core Interfaces

### Rule Interface

All rules implement this interface:

```go
type Rule interface {
    // Name returns the human-readable name of this rule
    Name() string

    // Subscribe returns the NATS subjects this rule should listen to
    Subscribe() []string

    // Evaluate checks if the rule conditions are met for the given messages
    Evaluate(messages []message.Message) bool

    // ExecuteEvents generates events when rule conditions are satisfied
    ExecuteEvents(messages []message.Message) ([]Event, error)
}
```

### EntityStateEvaluator Interface (Optional)

For rules that evaluate directly against entity triples (more efficient):

```go
type EntityStateEvaluator interface {
    // EvaluateEntityState evaluates the rule directly against EntityState triples.
    EvaluateEntityState(ctx context.Context, entityState *gtypes.EntityState) bool
}
```

Implement this interface for rules triggered by KV watch. The processor passes the
exact watcher operation context. Custom evaluators must thread it into any lifecycle
lookup or other context-aware work and must not retain it on the rule.

### Event Interface

Events returned by `ExecuteEvents`:

```go
type Event interface {
    // EventType returns the type identifier for this event
    EventType() string

    // Subject returns the NATS subject for publishing this event
    Subject() string

    // Payload returns the event data as a generic map
    Payload() map[string]any

    // Validate checks if the event is valid and ready to publish
    Validate() error
}
```

## Factory Pattern

### Factory Interface

```go
type Factory interface {
    // Create creates a rule instance from configuration
    Create(id string, config Definition, deps Dependencies) (Rule, error)

    // Type returns the rule type this factory creates
    Type() string

    // Schema returns the configuration schema for UI discovery
    Schema() Schema

    // Validate validates a rule configuration
    Validate(config Definition) error
}
```

### Dependencies

Factories receive dependencies for rule creation:

```go
type Dependencies struct {
    NATSClient *natsclient.Client
    Logger     *slog.Logger
}
```

### Schema

Describes the rule type for UI/documentation:

```go
type Schema struct {
    Type        string                 `json:"type"`
    DisplayName string                 `json:"display_name"`
    Description string                 `json:"description"`
    Category    string                 `json:"category"`
    Icon        string                 `json:"icon,omitempty"`
    Properties  map[string]PropertySchema
    Required    []string
    Examples    []Example
}
```

## Creating a Custom Rule

### Step 1: Define the Rule Struct

```go
package rule

import (
	"context"

    gtypes "github.com/c360/semstreams/graph"
    "github.com/c360/semstreams/message"
)

type ThresholdRule struct {
    id          string
    name        string
    field       string
    threshold   float64
    comparator  string // "above" or "below"
    enabled     bool
    triggered   bool
}
```

### Step 2: Implement Rule Interface

```go
func (r *ThresholdRule) Name() string {
    return r.name
}

func (r *ThresholdRule) Subscribe() []string {
    return []string{">"}
}

func (r *ThresholdRule) Evaluate(messages []message.Message) bool {
    if !r.enabled || len(messages) == 0 {
        return false
    }

    msg := messages[len(messages)-1]

    // Read the payload's DECLARED rule-readable fields.
    //
    // A payload is readable by a rule only if it implements
    // message.RuleReadable — the framework never derives fields
    // reflectively, so a field reaches a rule only because its author
    // exposed it. A payload that does not implement it exposes nothing,
    // and that is a deliberate refusal rather than an empty result.
    readable, ok := msg.Payload().(message.RuleReadable)
    if !ok {
        return false
    }

    // JSON numbers decode as float64, so a numeric field arrives as
    // float64 regardless of the producer's Go type.
    value, ok := readable.RuleFields()[r.field].(float64)
    if !ok {
        return false
    }

    // Compare against threshold
    switch r.comparator {
    case "above":
        r.triggered = value > r.threshold
    case "below":
        r.triggered = value < r.threshold
    }

    return r.triggered
}

func (r *ThresholdRule) ExecuteEvents(messages []message.Message) ([]Event, error) {
    if !r.triggered {
        return []Event{}, nil
    }

    // Create and return event
    event := &ThresholdEvent{
        ruleID:    r.id,
        field:     r.field,
        threshold: r.threshold,
    }

    r.triggered = false
    return []Event{event}, nil
}
```

### Reading payloads: `message.RuleReadable`

`Evaluate` above reads the message through `message.RuleReadable`
(`message/rule_readable.go`), the optional behavior interface a payload
implements to declare which of its fields a rule may match on:

```go
type RuleReadable interface {
    RuleFields() map[string]any
}
```

Two things follow that are easy to get wrong:

- **Absence is a refusal, not an empty map.** A payload that does not
  implement the interface is not rule-readable at all. If you are writing
  both a payload and a rule over it, the payload must implement this or
  every condition against it is false forever.
- **Only declared fields are visible.** The projection is explicit, so a
  struct field the author omitted is unreachable — including from
  `$message.*` substitution in actions. Payloads deliberately withhold
  LLM-authored and user content this way.

The framework's own payloads implement it in `agentic/rule_fields.go`;
`message.GenericJSONPayload` returns its data map verbatim.

### Step 3: Implement EntityStateEvaluator (Optional)

```go
func (r *ThresholdRule) EvaluateEntityState(ctx context.Context, entityState *gtypes.EntityState) bool {
    if !r.enabled || entityState == nil {
        return false
    }

    // Look up triple by predicate
    for _, triple := range entityState.Triples {
        if triple.Predicate == r.field {
            if value, ok := triple.Object.(float64); ok {
                switch r.comparator {
                case "above":
                    r.triggered = value > r.threshold
                case "below":
                    r.triggered = value < r.threshold
                }
                return r.triggered
            }
        }
    }

    return false
}
```

### Step 4: Create the Factory

```go
type ThresholdRuleFactory struct{}

func NewThresholdRuleFactory() *ThresholdRuleFactory {
    return &ThresholdRuleFactory{}
}

func (f *ThresholdRuleFactory) Type() string {
    return "threshold"
}

func (f *ThresholdRuleFactory) Create(id string, def Definition, deps Dependencies) (Rule, error) {
    // Extract custom fields from metadata
    field, _ := def.Metadata["field"].(string)
    threshold, _ := def.Metadata["threshold"].(float64)
    comparator, _ := def.Metadata["comparator"].(string)

    return &ThresholdRule{
        id:         id,
        name:       def.Name,
        field:      field,
        threshold:  threshold,
        comparator: comparator,
        enabled:    def.Enabled,
    }, nil
}

func (f *ThresholdRuleFactory) Validate(def Definition) error {
    if def.ID == "" {
        return fmt.Errorf("rule ID is required")
    }

    field, ok := def.Metadata["field"].(string)
    if !ok || field == "" {
        return fmt.Errorf("threshold rule requires 'field' in metadata")
    }

    _, ok = def.Metadata["threshold"].(float64)
    if !ok {
        return fmt.Errorf("threshold rule requires numeric 'threshold' in metadata")
    }

    comparator, _ := def.Metadata["comparator"].(string)
    if comparator != "above" && comparator != "below" {
        return fmt.Errorf("threshold rule 'comparator' must be 'above' or 'below'")
    }

    return nil
}

func (f *ThresholdRuleFactory) Schema() Schema {
    return Schema{
        Type:        "threshold",
        DisplayName: "Threshold Rule",
        Description: "Triggers when a field crosses a threshold",
        Category:    "condition",
        Required:    []string{"id", "metadata.field", "metadata.threshold"},
    }
}
```

### Step 5: Register the Factory

```go
func init() {
    factory := NewThresholdRuleFactory()
    if err := RegisterRuleFactory("threshold", factory); err != nil {
        fmt.Printf("Warning: Failed to register threshold factory: %v\n", err)
    }
}
```

## Factory Registration API

### Register

```go
err := RegisterRuleFactory("mytype", myFactory)
```

### Unregister

```go
err := UnregisterRuleFactory("mytype")
```

### Query

```go
// Get specific factory
factory, exists := GetRuleFactory("expression")

// Get all registered types
types := GetRegisteredRuleTypes()
// ["expression", "threshold", "custom"]

// Get all schemas
schemas := GetRuleSchemas()
```

### Create Rule

```go
rule, err := CreateRuleFromDefinition(def, deps)
```

This:
1. Looks up factory by `def.Type`
2. Validates definition
3. Creates rule instance

## Using Custom Rules

### JSON Configuration

```json
{
  "id": "temperature-alert",
  "type": "threshold",
  "name": "High Temperature Alert",
  "enabled": true,
  "metadata": {
    "field": "sensor.measurement.celsius",
    "threshold": 100,
    "comparator": "above"
  },
  "on_enter": [
    {"type": "publish", "subject": "alerts.temperature.high"}
  ]
}
```

### Loading

Custom rules are loaded the same as built-in rules:

```json
{
  "rules_files": [
    "/etc/semstreams/rules/custom.json"
  ]
}
```

## Base Factory Helper

Use `BaseRuleFactory` for common functionality:

```go
type BaseRuleFactory struct {
    ruleType    string
    displayName string
    description string
    category    string
}

func NewBaseRuleFactory(ruleType, displayName, description, category string) *BaseRuleFactory {
    return &BaseRuleFactory{
        ruleType:    ruleType,
        displayName: displayName,
        description: description,
        category:    category,
    }
}

// Type returns the rule type
func (f *BaseRuleFactory) Type() string {
    return f.ruleType
}

// ValidateExpression validates standard condition expressions
func (f *BaseRuleFactory) ValidateExpression(def Definition) error {
    // Validates conditions, logic, operators
}
```

Embed in your factory:

```go
type MyRuleFactory struct {
    *BaseRuleFactory
}

func NewMyRuleFactory() *MyRuleFactory {
    return &MyRuleFactory{
        BaseRuleFactory: NewBaseRuleFactory(
            "mytype",
            "My Rule Type",
            "Description of what this rule does",
            "custom",
        ),
    }
}
```

## Testing Custom Rules

```go
func TestThresholdRule(t *testing.T) {
    // Create rule directly
    rule := &ThresholdRule{
        id:         "test-threshold",
        name:       "Test Threshold",
        field:      "sensor.temp",
        threshold:  50.0,
        comparator: "above",
        enabled:    true,
    }

    // Test entity state evaluation
    entityState := &gtypes.EntityState{
        ID: "sensor-001",
        Triples: []gtypes.Triple{
            {Predicate: "sensor.temp", Object: 75.0},
        },
    }

    result := rule.EvaluateEntityState(context.Background(), entityState)
    assert.True(t, result)
}

func TestThresholdRuleFactory(t *testing.T) {
    factory := NewThresholdRuleFactory()

    def := Definition{
        ID:      "test-rule",
        Type:    "threshold",
        Name:    "Test",
        Enabled: true,
        Metadata: map[string]interface{}{
            "field":      "sensor.temp",
            "threshold":  50.0,
            "comparator": "above",
        },
    }

    // Test validation
    err := factory.Validate(def)
    assert.NoError(t, err)

    // Test creation
    rule, err := factory.Create(def.ID, def, Dependencies{})
    assert.NoError(t, err)
    assert.Equal(t, "Test", rule.Name())
}
```

## Best Practices

### 1. Validate Early

Validate all configuration in `Factory.Validate()` before rule creation.

### 2. Implement EntityStateEvaluator

If your rule can evaluate against entity triples, implement `EntityStateEvaluator` for better performance with KV watch.

### 3. Handle Missing Data

Rules may receive incomplete data. Always check for nil/missing values.

### 4. Use Metadata for Custom Fields

Store custom configuration in the `metadata` field:

```go
threshold, _ := def.Metadata["threshold"].(float64)
```

### 5. Thread Safety

Rules may be evaluated concurrently. Ensure thread-safe access to rule state.

### 6. Clean Event State

Reset triggered state after generating events:

```go
func (r *MyRule) ExecuteEvents(...) ([]Event, error) {
    // ... create events ...
    r.triggered = false  // Reset for next evaluation
    return events, nil
}
```

## Limitations

- Factories must be registered at startup (no hot loading)
- Custom rules share the same Definition struct (use metadata for custom fields)
- No dependency injection beyond Dependencies struct
- No async rule evaluation (rules are synchronous)

## Next Steps

- [Operations](09-operations.md) - Monitoring custom rules
- [Examples](10-examples.md) - Complete custom rule examples
