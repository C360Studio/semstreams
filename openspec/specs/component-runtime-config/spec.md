# component-runtime-config Specification

## Purpose
TBD - created by archiving change component-runtime-reconfig-http. Update Purpose after archive.
## Requirements
### Requirement: A runtime config change is applied via any supported reconfig contract

The ComponentManager config API MUST hot-apply a `PUT config/<component>` update
to a running component that implements EITHER runtime-reconfig contract: the
component-side `UpdateConfig(ctx, json.RawMessage)` OR the reconfig method pair
`ValidateConfigUpdate(map[string]any)` + `ApplyConfigUpdate(map[string]any)`. The
manager MUST probe the method pair, NOT the full `service.RuntimeConfigurable`
interface — a component's `ConfigSchema()` returns `component.ConfigSchema` while
`RuntimeConfigurable` embeds `Configurable.ConfigSchema() service.ConfigSchema`,
so a full-interface assert silently matches no component (see design.md). A
component implementing only the method pair (e.g. the rule processor) MUST be
reached, not silently skipped. When a component implements both, `UpdateConfig`
is used.

#### Scenario: a method-pair component is hot-applied over HTTP

- **GIVEN** a running component that implements the reconfig method pair but not `UpdateConfig`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager calls the component's `ValidateConfigUpdate` then `ApplyConfigUpdate`
- **AND** the running component reflects the change without a restart

#### Scenario: an UpdateConfig component keeps its existing path

- **GIVEN** a running component that implements `UpdateConfig(ctx, json.RawMessage)`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager applies the change via `UpdateConfig`
- **AND** does not additionally invoke the `RuntimeConfigurable` bridge

### Requirement: The config-update response honestly reports whether it was applied

A `PUT config/<component>` response MUST report, via an `applied` boolean, whether
the change was applied to the running component live (`applied: true` only when a
reconfig contract accepted the change). A component with no runtime-reconfig hook
MUST return `applied: false` and MUST NOT return a response implying a live apply.

The response MUST NOT promise that the change survives a restart: this endpoint
updates only the manager's in-memory view and does not durably persist to the
config store (durable persistence is out of scope — gh#388), so it MUST NOT emit
a `restart_required: true`-style field that a restart would not honor.

#### Scenario: hot-applied change reports applied

- **GIVEN** a component that supports runtime reconfiguration
- **WHEN** a valid config update is hot-applied
- **THEN** the response reports `applied: true`

#### Scenario: no-hook component reports not applied

- **GIVEN** a component that implements no runtime-reconfig contract
- **WHEN** a valid config update is received
- **THEN** the response reports `applied: false`
- **AND** does not report an unconditional success that implies a live apply
- **AND** does not promise a restart-time apply the endpoint cannot durably keep

### Requirement: A rejected update does not become a stored-but-unapplied config

The manager MUST validate a config update before storing it, so a rejected update
leaves the component's stored config unchanged and cannot be silently loaded on
the next restart. A `ValidateConfigUpdate` (or schema) failure returns a
structured error response and mutates neither the running component nor the stored
config.

#### Scenario: validation failure changes nothing

- **GIVEN** a component that supports runtime reconfiguration
- **WHEN** a `PUT config/<component>` request fails validation
- **THEN** the response is a structured validation error
- **AND** the running component is unchanged
- **AND** the stored config is unchanged (a subsequent restart does not load it)

### Requirement: Runtime component add/remove via the engine write methods drives a reconcile

The Manager SHALL, on a runtime component add (`PutComponentToKV`) or remove
(`DeleteComponentFromKV`), apply the change to the in-memory config synchronously
AND notify subscribers, so the `ComponentManager` reconciles it — spawning the
added component and tearing down the removed one — without requiring the
heavyweight `PushToKV` path. This holds even when the add/remove is interleaved
with other engine writes that raise the engine high-water revision.

#### Scenario: a component added at runtime is spawned

- **GIVEN** a running system watching config, with no `components.doc-source-003`
- **WHEN** a caller invokes `PutComponentToKV("doc-source-003", cfg)`
- **THEN** `doc-source-003` is present in the Manager's in-memory config
- **AND** subscribers to `components.*` are notified
- **AND** the `ComponentManager` spawns `doc-source-003`

#### Scenario: a component removed at runtime is torn down

- **GIVEN** a running system with a spawned `components.doc-source-003`
- **WHEN** a caller invokes `DeleteComponentFromKV("doc-source-003")`
- **THEN** `doc-source-003` is absent from the Manager's in-memory config
- **AND** subscribers to `components.*` are notified
- **AND** the `ComponentManager` tears down `doc-source-003`

#### Scenario: a delete interleaved under the engine high-water still reconciles

- **GIVEN** a runtime `DeleteComponentFromKV("doc-source-003")` at KV revision N
- **AND** a subsequent engine write raises the high-water revision above N
- **WHEN** the watcher processes the delete event (now classified engine-owned)
- **THEN** subscribers are still notified and the removal reconciles (the event is
  not silently skipped)

### Requirement: The engine-owned-revision skip suppresses only the in-memory re-apply

The config watcher SHALL, for an engine-owned revision (`revision <=
engineHighWaterRev`), suppress only the redundant in-memory re-apply of the value
and still notify matching subscribers — for both engine-owned and external events.
An engine-owned revision MUST NOT cause the notification to be dropped.

#### Scenario: an engine-owned event notifies subscribers

- **GIVEN** the Manager has just written a component and bumped its high-water revision
- **WHEN** the watcher delivers that event (revision at/below the high-water)
- **THEN** the in-memory config is not re-applied from the event
- **AND** subscribers matching the event key are still notified

