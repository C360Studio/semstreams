---
name: new-payload
description: Step-by-step checklist for adding a new payload type to the registry. Use when creating new message types for the agentic system or any polymorphic message flow.
argument-hint: [PayloadTypeName]
---

# New Payload Type Checklist

## What payload type are you adding?

$ARGUMENTS

## Step 1: Define the Type

Create your message struct implementing `message.Payload` (`Schema() message.Type`, `Validate() error`,
`json.Marshaler`, `json.Unmarshaler`):

```go
// yourpackage/types.go
package yourpackage

import (
    "fmt"

    "github.com/c360studio/semstreams/message"
)

const (
    Domain          = "yourdomain"
    CategoryYourCat = "your_category"
    Version         = "v1"
)

type YourMessage struct {
    ID      string `json:"id"`
    Content string `json:"content"`
    // ... your fields
}

func (m *YourMessage) Schema() message.Type {
    return message.Type{Domain: Domain, Category: CategoryYourCat, Version: Version}
}

func (m *YourMessage) Validate() error {
    if m.ID == "" {
        return fmt.Errorf("id is required")
    }
    return nil
}
```

If the payload is a graph fact (not just a transport envelope), also implement `Graphable`
(`EntityID() string`, `Triples() []message.Triple`) — see
`examples/processors/iot_sensor/payload.go` for a worked example with a federated entity ID and
domain-specific predicates.

## Step 2: Implement MarshalJSON / UnmarshalJSON

`BaseMessage` has unexported fields — a payload's own `MarshalJSON` does **not** construct or wrap a
`BaseMessage` literal. It marshals only its own data; `BaseMessage.MarshalJSON` (invoked via
`message.NewBaseMessage(...)`, see Step 5) builds the `{"type":...,"payload":...}` envelope around it.

**MUST use a type alias** so `json.Marshal`/`json.Unmarshal` don't recurse back into these same methods:

```go
// yourpackage/types.go
func (m *YourMessage) MarshalJSON() ([]byte, error) {
    type Alias YourMessage
    return json.Marshal((*Alias)(m))
}

func (m *YourMessage) UnmarshalJSON(data []byte) error {
    type Alias YourMessage
    return json.Unmarshal(data, (*Alias)(m))
}
```

## Step 3: Register a RegisterPayloads function in your package

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

## Step 4: Wire it into the composition root

A `RegisterPayloads` function that nothing calls never runs. First-party framework payloads are wired
into `payloadbuiltins.Register` (`payloadbuiltins/register.go`), which every binary calls at boot:

```go
// payloadbuiltins/register.go
func Register(reg *payloadregistry.Registry) error {
    var errs []error
    track := func(err error) {
        if err != nil {
            errs = append(errs, err)
        }
    }

    track(message.RegisterPayloads(reg))
    track(agentic.RegisterPayloads(reg))
    // ... add yours here if it's first-party and always-on
    track(yourpackage.RegisterPayloads(reg))

    return errors.Join(errs...)
}
```

Example/domain-specific or config-gated payloads (not first-party framework types) register directly at
each binary's own composition root instead — see `cmd/e2e-semstreams/main.go`'s `buildPayloadRegistry`
(calls `iotsensor.RegisterPayloads`, `document.RegisterPayloads`, `mission.RegisterPayloads` alongside
`payloadbuiltins.Register`) and `cmd/semstreams/main.go`'s `registerPayloads`, which conditionally adds
`graphresearch.RegisterPayloads` only `if graphresearch.Selected(cfg)`. Downstream products (semspec,
semdragon) call `payloadbuiltins.Register(reg)` and layer their own `reg.Register(...)` calls on top.

**A type registered in one binary but not another silently half-migrates the deployment** — see the
beta.18 case study in `CLAUDE.md`/`AGENTS.md` ("Breaking changes — E2E required before merge"). Check
every binary explicitly (Step 7).

## Step 5: Write a production-decoder round-trip test

Drive the real publish wrap (`message.NewBaseMessage`) through the real decode path
(`message.NewDecoder`/`payloadbuiltins.NewTestDecoder`) — never a hand-rolled shape cast against an
anonymous struct. Example, `processor/gated-dag/payload_roundtrip_test.go:19`
(`TestDispatchMessage_ProductionDecoderRoundTrip`):

```go
func TestYourMessage_ProductionDecoderRoundTrip(t *testing.T) {
    msg := &yourpackage.YourMessage{ID: "test-1", Content: "hello"}
    base := message.NewBaseMessage(msg.Schema(), msg, "test")

    data, err := json.Marshal(base)
    require.NoError(t, err)

    decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(data)
    require.NoError(t, err)

    got, ok := decoded.Payload().(*yourpackage.YourMessage)
    require.Truef(t, ok, "decoded payload must be *YourMessage, got %T", decoded.Payload())
    require.Equal(t, "test-1", got.ID)
}
```

If your package isn't in `payloadbuiltins.Register`, build a per-test registry instead:
`reg := payloadbuiltins.NewTestRegistry(t); require.NoError(t, yourpackage.RegisterPayloads(reg))`, then
`message.NewDecoder(reg)`.

## Step 6: Run task schema:generate if it affects schemas

`task schema:generate` (`cmd/openapi-generator`) walks `component.Registry` — component config schemas
and the OpenAPI spec. It does not read `payloadregistry` at all, so registering a new payload type by
itself produces no schema diff. Run it only if this change also touches a component's config surface
(a new allowed-type enum value, a new `component.PropertySchema` field, etc.):

```bash
task schema:generate
git diff schemas/ specs/openapi.v3.yaml   # must be empty, or commit the diff
```

## Step 7: Grep every binary that should carry this type

```bash
grep -rn "yourpackage\." cmd/
```

If a binary that should execute your `RegisterPayloads` call doesn't show up, its registry is
half-migrated — messages of this type will fail to decode there with `unregistered payload type:
domain.category.version` (see `message/base_message.go`'s `UnmarshalJSON`; there is no silent fallback
for a registered-elsewhere type — the fact lane rejects it outright).

## Verification Checklist

- [ ] Domain/Category/Version constants match between `Schema()` and the `Registration`
- [ ] `MarshalJSON`/`UnmarshalJSON` use a type alias (`type Alias YourMessage`) — no `BaseMessage` literal
- [ ] `RegisterPayloads(reg *payloadregistry.Registry) error` exists in the package, no `init()`
- [ ] The registration declares its `IndexingProfile` floor and any `Contracts` bound to the type (ADR-103)
- [ ] Every key you stamp on `entity.create` is registered in the binary that hosts graph-ingest (else
      `message_type_unregistered`); unit tests use `payloadregistry.RegisterTestType`
- [ ] `RegisterPayloads` is called from `payloadbuiltins.Register` or the product's composition root
- [ ] Production-decoder round-trip test passes:
      `go test -run TestYourMessage_ProductionDecoderRoundTrip ./yourpackage/...`
- [ ] `task schema:generate` produces no diff (commit `schemas/`/`specs/openapi.v3.yaml` if it does)
- [ ] `grep -rn "yourpackage\." cmd/` shows the registration call in every binary that needs it

## Common Mistakes

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unregistered payload type: domain.category.version` at decode | Package's `RegisterPayloads` never called | Wire it into `payloadbuiltins.Register` or the binary's composition root |
| Stack overflow on Marshal/Unmarshal | No type alias in `MarshalJSON`/`UnmarshalJSON` | Add `type Alias YourMessage` before the call |
| `Register` returns a schema-consistency error | `Schema()` domain/category/version don't match the `Registration` | Use the same constants in both places |
| `Register` returns "already registered" | Duplicate `domain.category.version` | Pick a distinct triple, or check you're not registering twice |
| Works in `cmd/e2e-semstreams`, fails in `cmd/semstreams` (or vice versa) | Registered in one binary's composition root, not the other | Grep every binary (Step 7) |

## Debugging

```go
// List all registered payloads on a *payloadregistry.Registry instance
// (deps.PayloadRegistry in a component, or a test registry)
for msgType, reg := range registry.List() {
    fmt.Printf("%s: %s\n", msgType, reg.Description)
}

// Verify the wire shape a BaseMessage produces
base := message.NewBaseMessage(msg.Schema(), msg, "debug")
data, _ := json.MarshalIndent(base, "", "  ")
fmt.Println(string(data))
// {"id":"...","type":{"domain":"...","category":"...","version":"..."},"payload":{...},"meta":{...}}
```

Read `docs/concepts/15-payload-registry.md` for full documentation.
