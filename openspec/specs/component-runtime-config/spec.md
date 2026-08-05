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

### Requirement: Runtime config-map mutations are serialized so a concurrent add/remove is never lost

The shared configuration store MUST serialize each read-modify-write so that two
concurrent mutations cannot drop one another's change. Every site that reads the
current config, mutates it, and swaps it back — the KV-watcher apply path
(`config.Manager.updateConfig`, reached by `PutComponentToKV` / `DeleteComponentFromKV`)
AND the engine caller-goroutine sites (`enableComponent`, `disableComponent`,
`deleteComponentConfig`, `writeComponentConfigs`, `writeToKV`) that share the same
`SafeConfig` instance — MUST perform the whole `read → mutate → swap` under the store's
write lock (e.g. a `SafeConfig.Mutate(fn)` primitive), NOT as a lock-free clone-then-swap.
A component add applied on the caller goroutine concurrently with an unrelated component
change applied by the watcher goroutine MUST NOT lose either change (last-writer-wins on
the whole map is forbidden).

#### Scenario: concurrent add and remove both take effect

- **GIVEN** a config with components A and B
- **WHEN** one goroutine adds component C and another concurrently removes B, interleaving their read-modify-write sequences
- **THEN** the resulting config contains A and C and does not contain B
- **AND** neither mutation is silently dropped

#### Scenario: watcher apply and caller add do not clobber

- **WHEN** the KV watcher applies an external `components.X` update while a caller invokes `PutComponentToKV("Y", ...)` concurrently
- **THEN** the final in-memory config contains both X's update and Y
- **AND** subscribers are notified for both keys

### Requirement: A component's effective config has one source of truth that GET config reflects

The ComponentManager MUST expose a single authoritative source for a component's
effective config, and the config read API (`GET /config/<component>`) MUST derive
its response from that source so it reflects what the component is actually running
— including after a KV-watch-driven restart, not only after a live `PUT`. A second
retained config copy that is refreshed on only some write paths MUST NOT back the
read API; the source of truth is the field refreshed on every write path (create,
KV-restart, and live-PUT).

#### Scenario: GET config after a KV-driven restart returns the new body

- **GIVEN** a running component created with config C
- **WHEN** a KV-watch config change restarts it with config C'
- **THEN** `GET /config/<component>` returns C' (not the stale C)

#### Scenario: GET config after a live PUT returns the applied body

- **GIVEN** a running component that supports live runtime reconfiguration
- **WHEN** a `PUT /config/<component>` applies config C' live
- **THEN** `GET /config/<component>` returns C'

### Requirement: A no-op runtime config update does not restart a running component

The ComponentManager MUST restart an existing enabled component on a per-component
runtime config update ONLY when the component's effective `ComponentConfig` differs
from the config it is currently running. A per-component update whose effective
config is unchanged MUST be a skipped no-op — no `Stop`/`Start` cycle, no store
deregistration, no port re-acquisition, and no HTTP-handler re-registration — so
that a full-config sync or a repeated identical write cannot churn a healthy
running component. This idempotency protects components that own external
resources, hold subscriptions or long-lived connections, or register handlers into
a one-shot mux (where re-registration would panic). To compare, the manager MUST
retain each managed component's effective `ComponentConfig`.

A changed effective config MUST still drive exactly one restart via the existing
graceful `restartComponentWithNewConfig` path. Creating a missing enabled component
and stopping a disabled or removed one are unaffected. The bulk
`reconcileComponents` path remains conservative (it already does not restart
already-running components) and is unchanged.

#### Scenario: an identical config update is a no-op

- **GIVEN** a running enabled component with effective config C
- **WHEN** a per-component config update with an effective config equal to C is received
- **THEN** the component is not stopped and not started
- **AND** no store deregistration, port re-acquisition, or handler re-registration occurs
- **AND** the manager logs the update as a skipped no-op

#### Scenario: a changed config update restarts exactly once

- **GIVEN** a running enabled component with effective config C
- **WHEN** a per-component config update with an effective config C' ≠ C is received
- **THEN** the component is restarted exactly once via the graceful restart path
- **AND** the manager retains C' as the component's effective config

#### Scenario: bulk reconcile with unchanged configs restarts nothing

- **GIVEN** a set of running enabled components whose effective configs are unchanged
- **WHEN** a bulk `components.*` reconcile is processed against the full config
- **THEN** no running component is restarted
- **AND** missing enabled components are still created and disabled/removed ones still stopped

### Requirement: Request-port interface identity survives effective configuration

A component's effective `nats-request` port configuration MUST preserve subject family, timeout, direction, required
flag, and interface type/version through schema decode, `BuildPortFromDefinition`, runtime GET config, and flow-graph
analysis. Typed `Config.Interface` wins when supplied; otherwise flat `PortDefinition.Interface` constructs the v1
contract. Runtime configuration MUST NOT silently downgrade a request port to plain `nats` or discard its interface.

#### Scenario: JSON-loaded mutation port keeps its contract

- **GIVEN** JSON configuration declares a required `nats-request` mutation output with interface
  `semstreams.graph.mutation` and family `graph.mutation.>`
- **WHEN** the definition is decoded and built into an effective port
- **THEN** the resulting port carries the same interface and family
- **AND** flow validation classifies it as request/reply rather than pub/sub

### Requirement: Retired semantic ownership configuration is rejected

Graph-ingest, projection, rule, and composition schemas MUST contain no `enforce_owner_lease`, owner token, owner
registry, presence, heartbeat, foreign-edge mode, or semantic ownership field. The clean pre-v1 cutover MUST NOT retain
ignored compatibility fields.

#### Scenario: Old lease setting is not silently ignored

- **GIVEN** a configuration still supplies `enforce_owner_lease`
- **WHEN** post-cutover schema validation runs
- **THEN** validation rejects the unknown retired field
- **AND** startup does not pretend enforcement remains active
