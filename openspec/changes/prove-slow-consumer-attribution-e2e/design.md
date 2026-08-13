# Assembled slow-consumer attribution E2E design

## Accepted scope

- Accepted inventory: `docs/proposals/gh954-slow-consumer-e2e-inventory.md`, SHA-256
  `0bcd335683770294919f8b1e8129985badb2247be9c4053a43e9779d297b5e76`.
- Accepted complete design: `docs/proposals/gh954-slow-consumer-e2e-design.md`, pre-acceptance SHA-256
  `3aa88ef490d749878f6c561e90ed86e78c2da43e328eac460cb5b4732dc2edaa`.
- Independent inventory and design reviews returned `INVENTORY PASS` and `DESIGN PASS`.
- Owner accepted rulings R1-R15 on 2026-08-13, while requiring implementation to stay within the narrow ruled shape.

## Composition

The ordinary `production` Docker target remains untagged and unchanged. A separate short gate builds
`./cmd/semstreams` with one E2E-only tag. The production root calls one hook immediately after its existing client
connects. The untagged implementation is a no-op. The tagged implementation passes that existing client to a private,
tagged probe.

The probe uses only `Client.GetConnection()` plus nats.go's installed `ErrorHandler` and `SetErrorHandler`. It creates
a raw, private queue subscription with a message pending limit of one, blocks its handler, publishes one admitted and
eight excess messages, and gates only the matching slow-consumer callback. Once `Dropped()` reports exactly eight, it
delegates to the captured production callback and waits for it to return. All unrelated errors delegate immediately.
Cleanup restores the callback and releases and unsubscribes the handler.

No client, logger, or handler is created or defaulted. The diagnostic therefore traverses the already-configured
client logger: configured local JSON output, common attributes, `component=natsclient`, the existing counter, and no
same-client forwarder.

## External proof

The isolated host scenario waits for the stack, bounded-polls Docker stdout, parses JSON records, and selects the one
probe-subject diagnostic. It verifies level, message, component, original error, subject, queue, exact cumulative
drops, absence of the fallback marker, uniqueness, and an exact-one existing counter value. Its assertion recorder
increments only after an assertion executes, so partial failure reports the actual smaller count.

All waits use channels, contexts, NATS flushes, or bounded ticker polling. The scenario and task use no arbitrary
sleep. The gate runs separately in the E2E Ladder so ordinary production-root core coverage remains unchanged.

## Binding rulings

R1-R15 are the binding owner rulings recorded in the accepted complete design. Implementation conformance is tracked
per ruling in `implementation-evidence.md`; any deviation requires owner re-ruling before code proceeds.

## Adopter seam

An outside component developer learns nothing new and predicts no capacity value. Untagged behavior and configuration
are unchanged. The E2E tag, fixture subject, queue, limit, callback gate, and raw subscription do not exist in the
release binary's active behavior or public surface.
