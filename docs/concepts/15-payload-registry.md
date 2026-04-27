# Payload Registry

The payload registry enables polymorphic JSON deserialization of message types across the SemStreams system.

## Why Payload Registry Exists

When messages flow through NATS JetStream, they're serialized as JSON. The challenge: how do we deserialize JSON back into the correct Go struct type?

```text
Publisher                    NATS                      Consumer
─────────                    ────                      ────────
TaskMessage{} ──►  JSON  ──► {"type":...} ──► JSON ──► ???

Problem: Consumer sees JSON but doesn't know it's a TaskMessage
```

The payload registry solves this with a type-discriminated envelope pattern:

```json
{
  "type": {
    "domain": "agentic",
    "category": "task",
    "version": "v1"
  },
  "payload": {
    "loop_id": "abc123",
    "prompt": "Review this code"
  }
}
```

When deserializing, `BaseMessage.UnmarshalJSON` reads the `type` field, looks up the factory in the registry, creates the correct struct, and unmarshals the payload into it.

## How It Works

### Architecture

```text
┌─────────────────────────────────────────────────────────────────────┐
│                         Payload Registry                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   Package init()              Global Registry           Consumer    │
│   ─────────────               ───────────────           ────────    │
│                                                                      │
│   agentic/payload_registry.go                                        │
│   ┌────────────────────────┐                                        │
│   │ RegisterPayload(       │                                        │
│   │   Domain: "agentic"    │ ──────────▶ map[string]Factory        │
│   │   Category: "task"     │             "agentic.task.v1" →        │
│   │   Version: "v1"        │               func() { &TaskMessage{} }│
│   │   Factory: ...         │                                        │
│   │ )                      │                      │                 │
│   └────────────────────────┘                      │                 │
│                                                   ▼                 │
│                                         BaseMessage.UnmarshalJSON   │
│                                         1. Read type field          │
│                                         2. Lookup factory           │
│                                         3. Create instance          │
│                                         4. Unmarshal payload        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Registration

Payload types are registered in `init()` functions, which run when the package is imported:

```go
// agentic/payload_registry.go
package agentic

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
    err := payloadregistry.Register(&payloadregistry.Registration{
        Domain:      Domain,           // "agentic"
        Category:    CategoryTask,     // "task"
        Version:     SchemaVersion,    // "v1"
        Description: "Agent task request",
        Factory:     func() any { return &TaskMessage{} },
    })
    if err != nil {
        panic("failed to register TaskMessage payload: " + err.Error())
    }
}
```

### Schema Consistency Validation

At registration time, the registry validates that the factory produces payloads with a `Schema()` method matching the registration. If a payload's `Schema()` returns different domain/category/version values than the registration specifies, the application will panic at startup during `init()`. This catches mismatches early rather than at runtime when messages fail to deserialize.

See `payloadregistry/registry.go` for the `validateSchemaConsistency` implementation.

### Serialization (MarshalJSON)

Every payload type MUST implement `MarshalJSON` that wraps the payload in a `BaseMessage`:

```go
// agentic/types.go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    // Use type alias to avoid infinite recursion
    type Alias TaskMessage
    return json.Marshal(&message.BaseMessage{
        Type: message.MessageType{
            Domain:   Domain,
            Category: CategoryTask,
            Version:  SchemaVersion,
        },
        Payload: (*Alias)(t),
    })
}
```

**Why the type alias?** Calling `json.Marshal(t)` would invoke `MarshalJSON` again, causing infinite recursion. The alias creates a new type without the method.

### Contract Enforcement

`BaseMessage.MarshalJSON` enforces validation before serialization. Invalid messages cannot be published - they fail immediately at the source rather than being silently dropped at the consumer. This catches missing required fields, invalid enum values, and other validation errors at serialize time.

See `message/base_message.go` for the implementation.

### Deserialization (UnmarshalJSON)

`BaseMessage.UnmarshalJSON` uses the registry to recreate typed payloads:

```go
// message/base.go (simplified)
func (m *BaseMessage) UnmarshalJSON(data []byte) error {
    // 1. Parse the envelope to get the type
    var envelope struct {
        Type    MessageType     `json:"type"`
        Payload json.RawMessage `json:"payload"`
    }
    json.Unmarshal(data, &envelope)

    // 2. Lookup factory in registry
    factory := payloadregistry.Global().Create(
        envelope.Type.Domain,
        envelope.Type.Category,
        envelope.Type.Version,
    )

    // 3. Create instance and unmarshal
    if factory != nil {
        json.Unmarshal(envelope.Payload, factory)
        m.Payload = factory
    } else {
        // Fallback to generic payload
        m.Payload = &GenericPayload{Data: envelope.Payload}
    }

    return nil
}
```

## Registering a New Payload Type

### Step 1: Define the Type

```go
// mypackage/types.go
package mypackage

const (
    Domain      = "mypackage"
    CategoryFoo = "foo"
    Version     = "v1"
)

type FooMessage struct {
    ID      string `json:"id"`
    Content string `json:"content"`
}
```

### Step 2: Implement MarshalJSON

```go
// mypackage/types.go
func (f *FooMessage) MarshalJSON() ([]byte, error) {
    type Alias FooMessage
    return json.Marshal(&message.BaseMessage{
        Type: message.MessageType{
            Domain:   Domain,
            Category: CategoryFoo,
            Version:  Version,
        },
        Payload: (*Alias)(f),
    })
}
```

### Step 3: Register in init()

```go
// mypackage/payload_registry.go
package mypackage

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
    err := payloadregistry.Register(&payloadregistry.Registration{
        Domain:      Domain,
        Category:    CategoryFoo,
        Version:     Version,
        Description: "Foo message for doing foo things",
        Factory:     func() any { return &FooMessage{} },
    })
    if err != nil {
        panic("failed to register FooMessage: " + err.Error())
    }
}
```

### Step 4: Import the Package

**Critical**: The package must be imported somewhere for `init()` to run:

```go
// main.go or component that needs this type
import (
    _ "github.com/c360studio/semstreams/mypackage" // Register payloads
)
```

## Common Mistakes

### Missing MarshalJSON

**Symptom**: Payload serializes as plain JSON without the type wrapper.

```go
// Wrong - missing MarshalJSON
type BadMessage struct {
    Content string `json:"content"`
}

// Serializes as: {"content": "hello"}
// Should be: {"type": {...}, "payload": {"content": "hello"}}
```

**Fix**: Implement `MarshalJSON` that wraps in `BaseMessage`.

### Wrong Type Fields

**Symptom**: Deserialization returns `GenericPayload` instead of typed struct.

```go
// Registration
payloadregistry.Register(&payloadregistry.Registration{
    Domain:   "agentic",
    Category: "task",
    Version:  "v1",
    // ...
})

// MarshalJSON uses wrong values
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    return json.Marshal(&message.BaseMessage{
        Type: message.MessageType{
            Domain:   "agentic",
            Category: "request",  // Wrong! Should be "task"
            Version:  "v1",
        },
        Payload: t,
    })
}
```

**Fix**: Use constants and ensure they match between registration and serialization.

### Package Not Imported

**Symptom**: Payload type never appears in registry despite correct registration code.

```go
// mypackage/payload_registry.go
func init() {
    // This never runs because package is never imported
    payloadregistry.Register(...)
}
```

**Fix**: Add blank import in main or in a component that uses the type:

```go
import _ "github.com/c360studio/semstreams/mypackage"
```

### Infinite Recursion in MarshalJSON

**Symptom**: Stack overflow when serializing.

```go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    return json.Marshal(&message.BaseMessage{
        Payload: t,  // Calls MarshalJSON again!
    })
}
```

**Fix**: Use a type alias:

```go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    type Alias TaskMessage
    return json.Marshal(&message.BaseMessage{
        Payload: (*Alias)(t),  // Alias has no MarshalJSON method
    })
}
```

## Debugging

### List Registered Payloads

```go
payloads := payloadregistry.Global().List()
for msgType, reg := range payloads {
    fmt.Printf("%s: %s\n", msgType, reg.Description)
}
```

Output:
```
agentic.task.v1: Agent task request
agentic.response.v1: Agent model response
agentic.tool_call.v1: Tool call request
...
```

### Verify JSON Structure

```go
msg := &TaskMessage{LoopID: "abc", Prompt: "test"}
data, _ := json.MarshalIndent(msg, "", "  ")
fmt.Println(string(data))
```

Expected:
```json
{
  "type": {
    "domain": "agentic",
    "category": "task",
    "version": "v1"
  },
  "payload": {
    "loop_id": "abc",
    "prompt": "test"
  }
}
```

### Check Deserialization

```go
jsonData := `{"type":{"domain":"agentic","category":"task","version":"v1"},"payload":{"loop_id":"abc"}}`

var msg message.BaseMessage
json.Unmarshal([]byte(jsonData), &msg)

// Check the actual type
switch p := msg.Payload.(type) {
case *agentic.TaskMessage:
    fmt.Printf("Got TaskMessage: %+v\n", p)
case *message.GenericPayload:
    fmt.Printf("Got GenericPayload (registration missing?): %s\n", p.Data)
default:
    fmt.Printf("Unexpected type: %T\n", p)
}
```

## Best Practices

### Use Constants for Type Fields

```go
// constants.go
const (
    Domain      = "agentic"
    Version     = "v1"

    CategoryTask     = "task"
    CategoryResponse = "response"
    CategoryToolCall = "tool_call"
)

// Use constants everywhere
Type: message.MessageType{
    Domain:   Domain,
    Category: CategoryTask,
    Version:  Version,
}
```

### Group Registrations by Package

Keep all payload registrations for a package in one file:

```
agentic/
├── types.go              # Type definitions
├── payload_registry.go   # All registrations for this package
└── constants.go          # Domain, categories, version
```

### Test Serialization Round-Trip

```go
func TestTaskMessage_RoundTrip(t *testing.T) {
    original := &TaskMessage{
        LoopID: "test-123",
        Prompt: "Review this code",
    }

    // Serialize
    data, err := json.Marshal(original)
    require.NoError(t, err)

    // Deserialize
    var msg message.BaseMessage
    err = json.Unmarshal(data, &msg)
    require.NoError(t, err)

    // Verify type
    result, ok := msg.Payload.(*TaskMessage)
    require.True(t, ok, "expected TaskMessage, got %T", msg.Payload)

    // Verify content
    assert.Equal(t, original.LoopID, result.LoopID)
    assert.Equal(t, original.Prompt, result.Prompt)
}
```

## Request/Reply Exemption

The payload registry applies to **stream messages** — messages published to JetStream where multiple consumers may need type-discriminated dispatch to deserialize polymorphic payloads.

**Graph request/reply subjects are intentionally exempt.** These subjects use raw JSON structs without BaseMessage wrapping:

| Subject pattern | Package | Purpose |
|---|---|---|
| `graph.mutation.triple.*` | `graph` | Triple add/remove mutations |
| `graph.mutation.entity.*` | `graph` | Entity CRUD operations |
| `graph.ingest.query.*` | `graph` | Hierarchy/prefix/batch lookups |
| `graph.query.*` | `graph` | Entity, relationship, search queries |

### Why request/reply doesn't need the registry

Request/reply is point-to-point: one publisher, one handler. The publisher knows exactly which handler will receive the message and what struct it expects. There is no fan-out, no polymorphic dispatch, and no need for type discovery at the consumer.

BaseMessage wrapping would add envelope overhead and registry coupling without providing any benefit — the consumer never needs to ask "what type is this?" because the subject already determines the handler.

### How callers use graph types

External consumers (semspec, semdragon) and internal components import the `graph` package directly for request/response types:

```go
import gtypes "github.com/c360studio/semstreams/graph"

req := gtypes.AddTripleRequest{Triple: triple}
reqData, _ := json.Marshal(req)
respData, _ := natsClient.Request(ctx, "graph.mutation.triple.add", reqData, 5*time.Second)

var resp gtypes.AddTripleResponse
json.Unmarshal(respData, &resp)
```

This is the intended integration pattern. The `graph` package's request and response types are its public API contract for NATS request/reply consumers.

### When to use which pattern

| Pattern | When | Example |
|---|---|---|
| **Payload registry + BaseMessage** | Stream pub/sub, fan-out, polymorphic consumers | `agent.task.*`, `agent.complete.*` |
| **Raw JSON structs** | NATS request/reply, point-to-point, known handler | `graph.mutation.*`, `graph.query.*` |

## Related Documentation

- [Agentic Components](../advanced/08-agentic-components.md) — Uses payload registry for all message types
- [Agentic Systems Concepts](./13-agentic-systems.md) — Foundational concepts
- [Component Registry](../basics/02-architecture.md) — Similar pattern for components
- [Contract Testing](../contributing/04-contract-testing.md) — Message contract tests validate all payloads
