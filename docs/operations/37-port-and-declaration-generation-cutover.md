# Port and declaration-generation breaking cutover

This note tells downstream teams how to adopt the strict port language and declaration-generation release. It is a
migration notice, not a compatibility plan. The cutover has no aliases, deprecated paths, dual readers, or shims.

SemStreams publishes the intentional breaks below. It does not audit every downstream repository before release.
Downstream teams own their dependency update, compilation, configuration migration, flow validation, and relevant E2E
proof.

## Canonical component ports

`component.PortDefinition` now has one strict envelope: `name`, optional `required` and `description`, and a typed
`config` object whose `kind` selects the portable configuration.

```json
{
  "name": "events",
  "required": true,
  "config": {
    "kind": "nats",
    "subject": "example.events"
  }
}
```

The closed kind vocabulary and direction rules are:

- Input only: `timer`, `http-client`, `kv-watch`, `kv-read`, `store-read`.
- Output only: `kv-write`, `store-provide`.
- Input or output: `network`, `file`, `nats`, `nats-request`, `jetstream`.

Old flat fields, top-level side lanes such as `kv_write`, kind aliases, custom kinds, and kinds used in the wrong
direction are rejected. Components continue to expose `InputPorts()` and `OutputPorts()`; there is no replacement
`Ports()` API.

## Component admission and declaration snapshots

Identity-free Registry admission is removed. Construct components through their registered factory and the normal
ComponentManager path so Registry can retain the factory identity and one immutable declaration generation.

Registry generation snapshot and observer APIs are internal framework coordination surfaces. Downstream components
must not treat them as a public integration API or a durable declaration feed.

## Services are restart-only composition

Services are immutable process composition established at boot. A `services.*` configuration change is durable desired
state for the next boot and reports restart required; it does not mutate the running service set.

The following service surfaces are removed without replacements:

- `StartService`, `StopService`, and `RemoveService`.
- `RuntimeConfigurable`.
- `ServiceConfig.Name`; the `ServiceConfigs` map key is the sole service identity.
- Message-logger inner `enabled` and `log_level` fields.
- Metrics inner `enabled` field.

`Manager.RegisterInstance(name, service)` now returns an error and is valid only before the service set is sealed.
Callers must handle that error; registration after boot composition is rejected rather than changing the running set.

Use outer `services.<name>.enabled` to control whether a service is constructed. The message logger is optional and,
when enabled in wildcard mode, observes admitted Registry declarations. It does not predict declarations from raw
component configuration.

## Streams remain config-owned

Stream provisioning remains explicit configuration-owned intent. Runtime Registry snapshots do not create streams or
expand provisioning scope. Strict startup rejects any default-only JetStream output that is not covered by configured
stream intent.

If a changed file configuration must replace the version already selected from KV, bump the top-level configuration
version. An equal or older file version leaves the KV-selected configuration effective.

## Related graph-foundation migration

The earlier [graph foundation breaking cutover](./36-graph-foundation-breaking-cutover.md) covers GS-01 mutation,
projection, ownership, and catalog acquisition changes. In particular, replace retired `OpenCatalogBucket` calls with
`OpenCatalogReader` for readers or `EnsureCatalogBucket` for declared bucket owners.

## Downstream adoption proof

For each downstream product:

1. Receive this guide and bump to the exact SemStreams breaking tag.
2. Compile to expose deleted symbols and strict port decoding failures.
3. Fix each explicit compile, schema, configuration, and flow-validation failure.
4. Validate the product's shipped flows against the new port and stream-planning contracts.
5. Run and record the product's relevant unit, integration, and E2E suites.

A quick read-only sizing check found retired flat `PortDefinition` usage in `semsource` and `semteams`. It also found
pre-existing graph-foundation `OpenCatalogBucket` and `pkg/ownership` debt in `semsource`. These examples are neither
an exhaustive downstream audit nor release blockers; the owning teams must confirm and migrate their actual build and
runtime surfaces.
