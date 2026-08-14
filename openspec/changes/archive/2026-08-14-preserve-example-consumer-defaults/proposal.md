## Why

The #963 consumer-policy refactor correctly made `MaxAckPending` observable, but it also changed the document and IoT
example processors' historical local consumer defaults. Both previously used delivery `all`, explicit acknowledgement,
and maximum delivery `5`. Canonical extraction currently turns empty delivery into `new` and zero maximum delivery into
`3`, which can exclude retained fixture messages published before the consumer binds during concurrent cold start.

## What Changes

- Restore the document and IoT processors' local delivery fallback to `all`.
- Restore local maximum delivery `5` whenever runtime facts carry zero, whether JSON omitted `max_deliver` or supplied
  explicit zero. Only a positive value overrides `5`.
- Preserve explicit valid delivery and acknowledgement declarations.
- Preserve independent `MaxAckPending` forwarding and observation from archived #963.
- Add deterministic real-NATS proof that a retained raw message published before component start is processed.

No global port extractor, natsclient builder, schema field, configuration file, or archived #963 artifact changes.

## Impact

- Affected capability: `jetstream-consumer-policy`.
- Affected runtime: only `examples/processors/document` and `examples/processors/iot_sensor`.
- Affected tests: focused policy resolution and real-NATS cold-start integration tests in those packages.
- Adopter default: doing nothing retains replay-safe historical behavior; positive `max_deliver` remains the only
  maximum-delivery override.
