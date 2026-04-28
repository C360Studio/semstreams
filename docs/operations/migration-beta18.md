# Migration Guide: beta.17 → beta.18

## Summary

The payload registry's package-level singleton is retired. Payload
registration is now explicit and constructor-injected, mirroring the
`v1.0.0-beta.16` retirement of the agentic-tools singleton. ADR-029
Pattern A is now applied uniformly across every registry the framework
ships.

This is a **breaking change** for anyone holding `init()`-style payload
registrations or unmarshaling `BaseMessage` directly via
`json.Unmarshal`. semspec and semdragon are coordinated separately.

## Why

Beta.16 retired the tools registry singleton and beta.17 added the
compile-time interface assertion. Payload registry was the lone
holdout — the singleton survived only because retiring it required
`BaseMessage.UnmarshalJSON` to take a registry, which has no DI surface
on the standard `json.Unmarshaler` contract.

Beta.18 fixes that with a `message.Decoder` type that mirrors stdlib
`json.Decoder` / `gob.Decoder`. Production code now goes through
`Decoder`; tests use `payloadbuiltins.NewTestDecoder(tb)`. There is no
test-only fallback and no global side door — the goal is uniform
Pattern A across the framework.

## Removed Public API

| Symbol | Replacement |
|---|---|
| `payloadregistry.Register(reg)` (package-level) | `(*payloadregistry.Registry).Register(reg)` |
| `payloadregistry.Create(domain, category, version)` | `(*payloadregistry.Registry).Create(...)` or `(*message.Decoder).Decode(data)` |
| `payloadregistry.Build(domain, category, version, fields)` | `(*payloadregistry.Registry).Build(...)` |
| `payloadregistry.Global()` | inject your own `payloadregistry.New()` (or use `payloadbuiltins.NewTestRegistry(tb)` in tests) |
| `federation.RegisterPayload(domain)` | `federation.RegisterPayloads(reg, domain)` |
| `(*component.Registry).RegisterPayload(reg)` | injected `*payloadregistry.Registry` directly |
| `(*component.Registry).CreatePayload(d, c, v)` | injected `*payloadregistry.Registry` directly |
| `(*component.Registry).ListPayloads()` | injected `*payloadregistry.Registry` directly |

## Behaviour Changes

- **`BaseMessage.UnmarshalJSON` requires a registry-bound BaseMessage.**
  The zero-value pattern `var msg message.BaseMessage; json.Unmarshal(data, &msg)`
  is now a hard error: "no payload registry configured; use
  message.NewDecoder(reg).Decode(data)". Production code MUST construct
  via `message.NewDecoder(reg).Decode(data)`.
- **No test-only fallback.** Removing the singleton meant evaluating
  whether to keep a `testing.Testing()`-guarded global for test
  ergonomics. We chose not to. Production code carries no test side
  doors; test code uses the helpers in `payloadbuiltins` and
  `payloadregistry/testing.go`.
- **`(*Store).SetDecoder` is required only for FetchContent fallback.**
  See `storage/objectstore/store.go` doc-comment. Pure StoreContent
  (write) and Open (streaming-read) consumers can leave it nil.

## Migration Steps

### 1. Convert `init()`-style payload registrations

Each package that registers payloads at import time becomes an explicit
exported function.

```diff
 package mypayloads

 import "github.com/c360studio/semstreams/payloadregistry"

-func init() {
-    err := payloadregistry.Register(&payloadregistry.Registration{
-        Domain:   "myorg",
-        Category: "mytype",
-        Version:  "v1",
-        Factory:  func() any { return &MyPayload{} },
-    })
-    if err != nil { panic(err) }
-}
+// RegisterPayloads registers all payload types in this package
+// with the supplied registry. Called from the binary's bootstrap
+// (typically alongside payloadbuiltins.Register).
+func RegisterPayloads(reg *payloadregistry.Registry) error {
+    return reg.Register(&payloadregistry.Registration{
+        Domain:   "myorg",
+        Category: "mytype",
+        Version:  "v1",
+        Factory:  func() any { return &MyPayload{} },
+    })
+}
```

For packages with multiple registrations, build a slice and aggregate
errors via `errors.Join` — see `agentic/payload_registry.go` or
`input/github-webhook/payload_registry.go` for the canonical shape.

### 2. Wire the registry at binary bootstrap

Each binary constructs its own `*payloadregistry.Registry`, registers
the first-party builtins via `payloadbuiltins.Register`, layers any
in-house payload packages, then plumbs the registry through
`component.Dependencies.PayloadRegistry`.

```go
import (
    "github.com/c360studio/semstreams/component"
    "github.com/c360studio/semstreams/payloadbuiltins"
    "github.com/c360studio/semstreams/payloadregistry"
    mypayloads "example.com/mypayloads"
)

func bootstrap(ctx context.Context /* ... */) error {
    reg := payloadregistry.New()

    // First-party builtins (agentic, message, dispatch, rule, boid,
    // operating-model, github-webhook, objectstore).
    if err := payloadbuiltins.Register(reg); err != nil {
        return fmt.Errorf("register builtin payloads: %w", err)
    }

    // In-house payloads.
    if err := mypayloads.RegisterPayloads(reg); err != nil {
        return fmt.Errorf("register mypayloads: %w", err)
    }

    deps := component.Dependencies{
        PayloadRegistry: reg,
        // ...nats client, tool registry, etc...
    }
    _ = deps
    return nil
}
```

For binaries that load example processors (e.g.,
`cmd/e2e-semstreams/main.go`), call each example's
`RegisterPayloads(reg)` explicitly — examples are intentionally NOT
included in `payloadbuiltins.Register` so downstream consumers don't
inherit example-specific payloads they don't need.

### 3. Convert `BaseMessage` unmarshal sites

Zero-value `var msg BaseMessage; json.Unmarshal(data, &msg)` no longer
works. Replace with `message.NewDecoder(reg).Decode(data)`.

```diff
- var baseMsg message.BaseMessage
- if err := json.Unmarshal(data, &baseMsg); err != nil {
+ baseMsg, err := c.decoder.Decode(data)
+ if err != nil {
     return err
  }
```

In components, hold the decoder as a field initialized at construction
time and reuse across calls — avoids per-message allocation:

```go
type Component struct {
    deps    component.Dependencies
    decoder *message.Decoder
    // ...
}

func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
    return &Component{
        deps:    deps,
        decoder: message.NewDecoder(deps.PayloadRegistry),
    }, nil
}
```

In tests, build a per-test registry:

```go
// Full builtins
dec := payloadbuiltins.NewTestDecoder(t)
msg, err := dec.Decode(data)

// Specific subset (tighter isolation, no payloadbuiltins import cycle)
reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads, mypkg.RegisterPayloads)
dec := message.NewDecoder(reg)
msg, err := dec.Decode(data)
```

### 4. Drop `(*component.Registry).RegisterPayload` calls

These wrappers were preserved transitionally in beta.16-17 and are
removed in beta.18. Callers should hold the `*payloadregistry.Registry`
directly via `Dependencies.PayloadRegistry`.

```diff
- registry.RegisterPayload(&payloadregistry.Registration{...})
+ deps.PayloadRegistry.Register(&payloadregistry.Registration{...})
```

(In practice no in-tree caller used these; flagging in case external
consumers did.)

### 5. Federation: switch `RegisterPayload` → `RegisterPayloads`

```diff
- federation.RegisterPayload("semsource")
+ federation.RegisterPayloads(reg, "semsource")
```

### 6. Bulk swap recipe

For mostly-mechanical migrations across a downstream codebase:

```bash
# Adjust paths as needed.
git grep -l 'payloadregistry\.\(Register\|Create\|Build\|Global\)\|federation\.RegisterPayload\b' \
  | xargs sed -i '' \
    -e 's|payloadregistry\.Register(|/* TODO: thread registry */ reg.Register(|g' \
    -e 's|payloadregistry\.Create(|/* TODO: thread registry */ reg.Create(|g' \
    -e 's|payloadregistry\.Build(|/* TODO: thread registry */ reg.Build(|g' \
    -e 's|payloadregistry\.Global()|/* TODO: thread registry */ reg|g'

# Then walk each TODO and decide where the registry is plumbed from.
```

The TODO markers force a manual review per call site — you'll need to
decide whether each gets `deps.PayloadRegistry`, a constructor-injected
registry, or a per-test registry from `payloadbuiltins.NewTestRegistry(t)`.

## Verification

After migrating, the following should hold:

- `go build ./...` succeeds.
- `go test -race ./...` and `go test -race -tags=integration ./...` pass.
- `task lint` reports 0 revive warnings.
- A boot of the binary completes and the agentic e2e tier
  (`task e2e:agentic`) exercises the full pipeline.

## Known consumers coordinated

semspec and semdragon are tracked separately. No deprecation window —
clean break, get it right before the v1 milestone. Beta.18 release
notes carry this same migration recipe so external consumers see it on
the tag.
