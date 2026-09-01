# Payload Registry

The payload registry enables polymorphic JSON deserialization of message types across the SemStreams system.

## Why Payload Registry Exists

When messages flow through NATS JetStream, they're serialized as JSON. The challenge: how do we deserialize
JSON back into the correct Go struct type?

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
    "loop_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "prompt": "Review this code"
  }
}
```

When deserializing, `BaseMessage.UnmarshalJSON` reads the `type` field, looks up the factory in the registry,
creates the correct struct, and unmarshals the payload into it.

## How It Works

### Architecture

```text
┌───────────────────────────────────────────────────────────────────────┐
│                          Payload Registry                              │
├───────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   Package                    *payloadregistry.Registry     Consumer   │
│   ────────                   ─────────────────────────     ────────   │
│                                                                        │
│   agentic/payload_registry.go                                          │
│   ┌──────────────────────────┐                                        │
│   │ func RegisterPayloads(   │                                        │
│   │   reg *payloadregistry   │ ──────────▶ map[string]*Registration  │
│   │      .Registry) error {  │             "agentic.task.v1" →        │
│   │   reg.Register(&...{     │               Factory: func() {        │
│   │     Domain: "agentic"    │                 &TaskMessage{} }        │
│   │     Category: "task"     │                                        │
│   │     Version: "v1"        │                      │                 │
│   │     Factory: ...})       │                      │                 │
│   │ }                        │                      │                 │
│   └──────────────────────────┘                      │                 │
│              ▲                                       ▼                │
│              │                            message.NewDecoder(reg)    │
│   payloadbuiltins.Register(reg)           .Decode(data):              │
│   — called once at process boot           1. Read type field         │
│   from cmd/semstreams,                    2. Lookup factory          │
│   cmd/e2e-semstreams, or a                3. Create instance         │
│   product's own composition root          4. Unmarshal payload       │
│                                                                        │
└───────────────────────────────────────────────────────────────────────┘
```

### Registration

Every package that owns a payload type exports a `RegisterPayloads(reg *payloadregistry.Registry) error`
function. There is no `init()` and no package-level singleton — the registry is an explicit instance
(`payloadregistry.New()`) that a composition root builds and passes around:

```go
// agentic/payload_registry.go
package agentic

import "github.com/c360studio/semstreams/payloadregistry"

func RegisterPayloads(reg *payloadregistry.Registry) error {
    return reg.Register(&payloadregistry.Registration{
        Domain:      Domain,           // "agentic"
        Category:    CategoryTask,     // "task"
        Version:     SchemaVersion,    // "v1"
        Description: "Agent task request",
        Factory:     func() any { return &TaskMessage{} },
    })
}
```

`RegisterPayloads` only runs when something calls it. `payloadbuiltins.Register` (`payloadbuiltins/register.go`)
aggregates every first-party framework payload's `RegisterPayloads` call and is itself called once at boot from
`cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go`. Domain-specific or config-gated payloads (example
processors, `graphresearch`) register directly at each binary's own composition root instead — see
`cmd/e2e-semstreams/main.go`'s `buildPayloadRegistry`. A type registered in one binary's composition root but not
another silently half-migrates that deployment — every message of that type decodes fine where it's registered and
is rejected everywhere else.

### Schema Consistency Validation

At registration time, `Register` validates that the factory produces a payload whose `Schema()` method matches the
registration's `Domain`/`Category`/`Version`. If they diverge, `Register` returns an error — the caller (typically
`payloadbuiltins.Register`, which aggregates every registration error via `errors.Join`) decides what to do with it,
usually failing boot. This catches mismatches at wiring time rather than at runtime when messages fail to
deserialize.

See `payloadregistry/registry.go` for the `validateSchemaConsistency` implementation.

### Serialization (MarshalJSON)

A payload's own `MarshalJSON` does **not** wrap itself in a `BaseMessage` — `BaseMessage`'s fields are unexported,
so a struct literal like `&message.BaseMessage{Type: ..., Payload: ...}` can't even be constructed outside the
`message` package. The payload marshals only its own data; the envelope comes from `message.NewBaseMessage(msgType,
payload, source)` (see [Registering a New Payload Type](#registering-a-new-payload-type)):

```go
// agentic/types.go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    // Use type alias to avoid infinite recursion
    type Alias TaskMessage
    return json.Marshal((*Alias)(t))
}
```

**Why the type alias?** Calling `json.Marshal(t)` would invoke `MarshalJSON` again, causing infinite recursion. The
alias creates a new type without the method.

### Contract Enforcement

`BaseMessage.MarshalJSON` enforces validation before serialization. Invalid messages cannot be published - they fail
immediately at the source rather than being silently dropped at the consumer. This catches missing required fields,
invalid enum values, and other validation errors at serialize time.

See `message/base_message.go` for the implementation.

### Deserialization (UnmarshalJSON)

`BaseMessage.UnmarshalJSON` uses the registry bound to the `Decoder` that constructed it to recreate typed payloads.
Production code never constructs a bare `BaseMessage{}` and calls `json.Unmarshal` on it directly — that has a nil
registry and fails fast. Go through `message.NewDecoder(reg).Decode(data)`:

```go
// message/base_message.go (simplified)
func (m *BaseMessage) UnmarshalJSON(data []byte) error {
    // 1. Parse the wire envelope
    var wire wireFormat // {ID, Type, Payload json.RawMessage, Meta}
    json.Unmarshal(data, &wire)

    // 2. Fail fast if this BaseMessage wasn't built via Decoder
    if m.registry == nil {
        return errors.New("no payload registry configured; use message.NewDecoder(reg).Decode(data)")
    }

    // 3. Lookup factory in the bound registry
    payload := m.registry.Create(wire.Type.Domain, wire.Type.Category, wire.Type.Version)
    if payload == nil {
        // Unregistered type — rejected, no silent GenericPayload fallback.
        return fmt.Errorf("unregistered payload type: %s", wire.Type)
    }

    // 4. Unmarshal into the typed payload
    json.Unmarshal(wire.Payload, payload.(Payload))
    m.payload = payload.(Payload)
    return nil
}
```

An unregistered type on this path is a hard decode error, not a fallback to a generic payload — the message is
rejected. (`core.json.v1` — `message.GenericJSONPayload`, registered by `message.RegisterPayloads` in
`message/generic_json.go` — is an explicit, opt-in payload type for prototyping, not an automatic fallback for
unknown types.)

## Registering a New Payload Type

See `.agents/skills/new-payload/SKILL.md` for the full step-by-step checklist with compiled-against-HEAD code
templates. Summary:

### Step 1: Define the Type

```go
// mypackage/types.go
package mypackage

import (
    "fmt"

    "github.com/c360studio/semstreams/message"
)

const (
    Domain      = "mypackage"
    CategoryFoo = "foo"
    Version     = "v1"
)

type FooMessage struct {
    ID      string `json:"id"`
    Content string `json:"content"`
}

func (f *FooMessage) Schema() message.Type {
    return message.Type{Domain: Domain, Category: CategoryFoo, Version: Version}
}

func (f *FooMessage) Validate() error {
    if f.ID == "" {
        return fmt.Errorf("id is required")
    }
    return nil
}
```

### Step 2: Implement MarshalJSON / UnmarshalJSON

The payload marshals only itself — `message.NewBaseMessage` builds the envelope, not the payload's own
`MarshalJSON`:

```go
// mypackage/types.go
func (f *FooMessage) MarshalJSON() ([]byte, error) {
    type Alias FooMessage
    return json.Marshal((*Alias)(f))
}

func (f *FooMessage) UnmarshalJSON(data []byte) error {
    type Alias FooMessage
    return json.Unmarshal(data, (*Alias)(f))
}
```

### Step 3: Register a RegisterPayloads function — no init()

No `init()`. Add an exported `RegisterPayloads(reg *payloadregistry.Registry) error` function that the
composition root calls explicitly at boot. The registration carries everything the framework knows about
the type (ADR-103, the registry is the single type authority): the factory, the ADR-054 indexing-profile
**floor** graph-ingest stamps on an entity born with the type when the producer declares none, and any
projection **contract** bound to the type. A type you stamp on `entity.create` MUST be registered here —
graph-ingest refuses a stamp its registry does not hold with `message_type_unregistered`.

```go
// yourpackage/payload_registry.go
package yourpackage

import (
    "github.com/c360studio/semstreams/payloadregistry"
    "github.com/c360studio/semstreams/pkg/projection/contract"
    "github.com/c360studio/semstreams/vocabulary"
)

// RegisterPayloads registers YourMessage (yourdomain.your_category.v1) with the
// supplied registry. Called from payloadbuiltins.Register (or a product's own
// composition root) at process bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
    return reg.Register(&payloadregistry.Registration{
        Domain:      Domain,
        Category:    CategoryYourCat,
        Version:     Version,
        Description: "Description of your message type",
        Factory:     func() any { return &YourMessage{} },
        // ADR-054 floor for entities born with this type: content, control,
        // signal, or trace. "" is admitted and metered as a gap
        // (indexing_profile_default_total{message_type}).
        IndexingProfile: vocabulary.IndexingProfileControl,
        // Optional: the projection contract(s) bound to this type. An empty
        // contract MessageType is filled with this key; a different key is a
        // registration error.
        Contracts: []contract.Contract{YourBirthContract()},
    })
}
```

`Register` validates `Domain`/`Category`/`Version`/`Factory` are non-empty, rejects a duplicate
`domain.category.version`, checks the factory-produced payload's `Schema()` matches the
registration, rejects an `IndexingProfile` outside the vocabulary, and fills or checks each contract's
`MessageType` — all as a returned `error`, not a panic. Aggregate multiple registrations in one package
with `errors.Join` (see `agentic/payload_registry.go`) if you're adding more than one type.

In a unit test that only needs a key to pass graph-ingest's create seam (no wire form),
`payloadregistry.RegisterTestType(t, reg, "test.widget.v1")` registers a schema-less stub with no floor.

### Step 4: Wire it into the composition root

**Critical**: a `RegisterPayloads` function nothing calls never runs — there is no `init()` to fall back on. Add
the call to `payloadbuiltins.Register` (`payloadbuiltins/register.go`) for a first-party framework type, or to
each binary's own composition root (`cmd/semstreams/main.go`'s `registerPayloads`,
`cmd/e2e-semstreams/main.go`'s `buildPayloadRegistry`) for a domain/example/config-gated type. Then grep every
binary that should carry it:

```bash
grep -rn "mypackage\." cmd/
```

## Common Mistakes

### Missing MarshalJSON / UnmarshalJSON

**Symptom**: Compile error — the type doesn't satisfy `message.Payload` (which embeds `json.Marshaler` and
`json.Unmarshaler`).

**Fix**: Implement both methods with the type-alias pattern (Step 2).

### RegisterPayloads Never Called

**Symptom**: `unregistered payload type: mypackage.foo.v1` at decode time, even though `RegisterPayloads` and
`MarshalJSON` are both correct.

```go
// mypackage/payload_registry.go
func RegisterPayloads(reg *payloadregistry.Registry) error {
    // Correct code — but nothing in payloadbuiltins.Register or any
    // binary's composition root calls RegisterPayloads(reg), so this
    // never runs.
    return reg.Register(&payloadregistry.Registration{ /* ... */ })
}
```

**Fix**: Wire the call into `payloadbuiltins.Register` or the binary's composition root (Step 4).

### Schema()/Registration Mismatch

**Symptom**: `Register` returns an error at wiring time (`payloadbuiltins.Register` aggregates it and boot fails).

```go
// Schema() says "task"...
func (t *TaskMessage) Schema() message.Type {
    return message.Type{Domain: "agentic", Category: "task", Version: "v1"}
}

// ...but the registration says "request" — validateSchemaConsistency rejects this at Register time.
reg.Register(&payloadregistry.Registration{
    Domain:   "agentic",
    Category: "request", // Wrong! Should be "task"
    Version:  "v1",
    Factory:  func() any { return &TaskMessage{} },
})
```

**Fix**: Use the same constants for `Schema()` and the `Registration`.

### Infinite Recursion in MarshalJSON

**Symptom**: Stack overflow when serializing.

```go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    return json.Marshal(t) // Calls MarshalJSON again!
}
```

**Fix**: Use a type alias:

```go
func (t *TaskMessage) MarshalJSON() ([]byte, error) {
    type Alias TaskMessage
    return json.Marshal((*Alias)(t)) // Alias has no MarshalJSON method
}
```

## Debugging

### List Registered Payloads

`payloadregistry` has no package-level singleton — list off the `*payloadregistry.Registry` instance you have
(`deps.PayloadRegistry` in a component, or a test registry):

```go
for msgType, reg := range registry.List() {
    fmt.Printf("%s: %s\n", msgType, reg.Description)
}
```

Output:

```text
agentic.task.v1: Agent task request
agentic.tool_result.v1: Tool execution result
core.json.v1: Generic JSON payload for testing, prototyping, and basic data processing
...
```

### Verify JSON Structure

The type envelope only appears once the payload is wrapped in a `BaseMessage` — marshaling the bare payload
shows just its own fields:

```go
msg := &agentic.TaskMessage{LoopID: "7c9e6679-7425-40de-944b-e07fc1f90ae7", Prompt: "test"}
base := message.NewBaseMessage(msg.Schema(), msg, "debug")
data, _ := json.MarshalIndent(base, "", "  ")
fmt.Println(string(data))
```

Expected:

```json
{
  "id": "...",
  "type": {
    "domain": "agentic",
    "category": "task",
    "version": "v1"
  },
  "payload": {
    "loop_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "prompt": "test"
  },
  "meta": {"created_at": ..., "received_at": ..., "source": "debug"}
}
```

### Check Deserialization

Go through a `Decoder` bound to a real registry — `BaseMessage{}` constructed directly has a nil registry and
fails fast:

```go
jsonData := []byte(`{"id":"1","type":{"domain":"agentic","category":"task","version":"v1"},"payload":{"loop_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"},"meta":{}}`)

decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(jsonData)
if err != nil {
    // "unregistered payload type: ..." if agentic.RegisterPayloads never ran
    // against this registry — no silent fallback.
    t.Fatal(err)
}

switch p := decoded.Payload().(type) {
case *agentic.TaskMessage:
    fmt.Printf("Got TaskMessage: %+v\n", p)
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
message.Type{
    Domain:   Domain,
    Category: CategoryTask,
    Version:  Version,
}
```

### Group Registrations by Package

Keep all payload registrations for a package in one file:

```text
agentic/
├── types.go              # Type definitions
├── payload_registry.go   # RegisterPayloads for this package
└── constants.go          # Domain, categories, version
```

### Test With the Production Decoder, Not a Shape Cast

Round-trip through the real publish wrap and the real decode path — never a hand-rolled `json.Unmarshal` into a
bare `BaseMessage{}` or an anonymous struct. See `processor/gated-dag/payload_roundtrip_test.go`
(`TestDispatchMessage_ProductionDecoderRoundTrip`) for a worked example:

```go
func TestFooMessage_ProductionDecoderRoundTrip(t *testing.T) {
    original := &mypackage.FooMessage{ID: "test-123", Content: "hello"}
    base := message.NewBaseMessage(original.Schema(), original, "test")

    data, err := json.Marshal(base)
    require.NoError(t, err)

    decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(data)
    require.NoError(t, err)

    result, ok := decoded.Payload().(*mypackage.FooMessage)
    require.True(t, ok, "expected *FooMessage, got %T", decoded.Payload())
    require.Equal(t, original.ID, result.ID)
    require.Equal(t, original.Content, result.Content)
}
```

## Request/Reply Exemption

The payload registry applies to **stream messages** — messages published to JetStream where multiple consumers
may need type-discriminated dispatch to deserialize polymorphic payloads.

**Graph request/reply subjects are intentionally exempt.** These subjects use raw JSON structs without
BaseMessage wrapping:

| Subject pattern | Package | Purpose |
|---|---|---|
| `graph.mutation.triple.append` | `graph` | Exact-tuple append with per-subject outcomes |
| `graph.mutation.entity.*` | `graph` | Strict create, revision-fenced reconcile/delete |
| `graph.ingest.query.*` | `graph` | Hierarchy/prefix/batch lookups |
| `graph.query.*` | `graph` | Entity, relationship, search queries |

### Why request/reply doesn't need the registry

Request/reply is point-to-point: one publisher, one handler. The publisher knows exactly which handler will
receive the message and what struct it expects. There is no fan-out, no polymorphic dispatch, and no need for
type discovery at the consumer.

BaseMessage wrapping would add envelope overhead and registry coupling without providing any benefit — the
consumer never needs to ask "what type is this?" because the subject already determines the handler.

### How components use graph request/reply

Components declare the typed `semstreams.graph.mutation` v1 request port. Framework writers normally receive a
narrow `pkg/projection` capability rather than spelling a subject or holding a raw NATS client:

```go
import "github.com/c360studio/semstreams/pkg/projection"

receipt, err := appender.Append(ctx, projection.AppendMutation{
    Contract: "example.observations.v1",
    Group:    "observations",
    EntityID: entityID,
    Triples:  triples,
    Metadata: metadata,
})
```

The `graph` package still defines the four wire DTO pairs. Subject resolution belongs to the declared request-port
interface, and the framework sends each request once. Components own any retry decision.

### When to use which pattern

| Pattern | When | Example |
|---|---|---|
| **Payload registry + BaseMessage** | Stream pub/sub, fan-out, polymorphic consumers | `agent.task.*`, `agent.complete.*` |
| **Raw JSON structs** | Typed NATS request/reply, point-to-point, known handler | `graph.mutation.>`, `graph.query.>` |

## Related Documentation

- [Agentic Components](../advanced/08-agentic-components.md) — Uses payload registry for all message types
- [Agentic Systems Concepts](./13-agentic-systems.md) — Foundational concepts
- [Component Registry](../basics/02-architecture.md) — Similar pattern for components
- [Contract Testing](../contributing/04-contract-testing.md) — Message contract tests validate all payloads
