# Migrate from Remote Component Capability Discovery

> **Breaking change:** SemStreams no longer publishes or reads remote component capability announcements.

The remote discovery path had no production consumer, and its cache had no writer after the subscriber was retired.
SemStreams therefore removed the publisher, its periodic heartbeat, and the dead read API instead of replacing them
with another transport.

## Removed Go surface

- `component.CapabilityAnnouncement`
- `component.PortCapability`
- `component.WithLogger`
- `(*component.Registry).InitNATS`
- `(*component.Registry).GetCapabilities`
- `(*component.Registry).WaitForCapabilities`
- `(*component.Registry).StartHeartbeat`
- `(*component.Registry).StopHeartbeat`

`(*component.Registry).SubscribeCapabilities` had already been retired. `component.NewRegistry` now takes no options;
replace `component.NewRegistry(component.WithLogger(logger))` with `component.NewRegistry()`.

There is no replacement raw NATS subject, JetStream stream, or KV bucket. Direct Go users should delete unused remote
discovery integration. A product with a concrete cross-node discovery requirement should bring that use case for a
separate consumer-led design rather than recreating these subjects.

Local Registry factory and admitted-declaration metadata remain available. ComponentManager still owns component
lifecycle and exposes component health and status. Neither local declaration presence nor retained messaging state is
a claim that a component is currently running or healthy.

## Retired NATS resources

SemStreams no longer creates or publishes to the `COMPONENT_CAPABILITIES` stream or these subject families:

- `processor.capabilities.*`
- `input.capabilities.*`
- `output.capabilities.*`
- `storage.capabilities.*`
- `gateway.capabilities.*`

An upgrade does not delete an existing stream. In a mixed-version deployment, keep it while any older SemStreams
producer remains. Its retained messages age out under the old one-hour limit, but the empty stream object remains.
Before explicitly deleting that stream, verify that no external consumer still depends on it; stream deletion is
irreversible and remains an operator action.

## Known semmachina source migrations

SemStreams does not edit sister repositories. The known semmachina migration is limited to replacing the Registry
logger option at these locations:

- `internal/testinfra/harness.go:305`
- `internal/boot/components.go:53`
- `internal/boot/components.go:71`
- `internal/boot/components.go:109`
- `cmd/bellweather-surface-stack/main.go:267`

No known semmachina code consumes the retired stream, subjects, announcement types, or Registry methods.
