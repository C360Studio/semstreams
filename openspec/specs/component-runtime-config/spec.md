# component-runtime-config Specification

## Purpose
TBD - created by archiving change component-runtime-reconfig-http. Update Purpose after archive.
## Requirements
### Requirement: A runtime config change is applied via any supported reconfig contract

The ComponentManager config API MUST hot-apply a `PUT config/<component>` update to a running component that
implements either supported component-side contract: `UpdateConfig(ctx, json.RawMessage)` or the anonymous method pair
`ValidateConfigUpdate(map[string]any)` plus `ApplyConfigUpdate(map[string]any)`.

The manager MUST probe the anonymous method pair directly and MUST NOT require or consult any service runtime-config
interface. A component implementing only the method pair, including rule processor, MUST be reached rather than
silently skipped. When a component implements both contracts, `UpdateConfig` MUST be used.

#### Scenario: a method-pair component is hot-applied over HTTP

- **GIVEN** a running component that implements the reconfig method pair but not `UpdateConfig`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager calls the component's `ValidateConfigUpdate` then `ApplyConfigUpdate`
- **AND** the running component reflects the change without a restart

#### Scenario: an UpdateConfig component keeps its existing path

- **GIVEN** a running component that implements `UpdateConfig(ctx, json.RawMessage)`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager applies the change via `UpdateConfig`
- **AND** it does not additionally invoke the anonymous method pair

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

A `nats-request` port SHALL be decoded and resolved only through the canonical nested `config.kind` envelope. Its
subject, timeout, direction, required state, interface type, and interface version SHALL survive strict JSON decoding,
complete named-port replacement, runtime configuration reads, and normalized flow facts.

No flat field, legacy alias, or builder fallback SHALL restore missing port meaning.

#### Scenario: Mutation request port survives canonical round-trip

- **GIVEN** a canonical graph-mutation `nats-request` declaration
- **WHEN** it is decoded, merged, read through runtime configuration, and projected as normalized port facts
- **THEN** it remains a graph-mutation request port
- **AND** its subject, timeout, direction, required state, interface type, and interface version are unchanged

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

### Requirement: Component ports have one strict canonical grammar

Port configuration SHALL contain only `inputs` and `outputs`.

Each port SHALL contain the common fields `name`, `required`, and `description`, plus a nested `config` object
discriminated by exactly one of these kinds: `timer`, `network`, `file`, `http-client`, `nats`, `nats-request`,
`jetstream`, `kv-watch`, `kv-read`, `kv-write`, `store-read`, `store-provide`.

Unknown kinds, unknown fields, kinds used in a prohibited direction, duplicate or unknown named ports, malformed
durations or network ports, and missing required fields SHALL fail before component initialization. The failure SHALL
identify the component, port, kind, and invalid field when those values are available.

Every `jetstream` port SHALL declare at least one non-empty subject. Every input `jetstream` port SHALL additionally
declare a non-empty `stream_name`. An output `jetstream` port MAY omit `stream_name`; that omission is consumed only by
the canonical generic provisioner and SHALL NOT authorize a consumer-local derivation.

Only network host `0.0.0.0` and request timeout `1s` SHALL receive implicit defaults.

Retired flat port fields, `Config any`, runtime `type`/`data` envelopes, legacy aliases, and top-level KV lanes SHALL
NOT be accepted.

#### Scenario: Canonical definition and runtime views agree

- **GIVEN** a valid canonical port declaration
- **WHEN** it is decoded and exposed through runtime configuration
- **THEN** the definition and runtime views preserve identical direction, kind, required state, description, resource
  fields, and interface metadata

#### Scenario: Legacy declaration fails without repair

- **GIVEN** a declaration that uses a retired flat field, legacy alias, runtime `type`/`data` envelope, `Config any`, or
  top-level KV lane
- **WHEN** configuration is decoded
- **THEN** startup fails before component initialization
- **AND** no compatibility repair, alias expansion, or fallback is attempted

#### Scenario: Named merge is complete replacement

- **GIVEN** a valid override naming an existing port
- **WHEN** effective configuration is produced
- **THEN** the override completely replaces that named port
- **AND** omitted kind-specific fields are not inherited from the prior declaration

#### Scenario: Invalid named merge is rejected

- **WHEN** an override names an unknown port, repeats a port name, changes a port's direction, or changes its kind
- **THEN** effective configuration fails with typed component and port context

#### Scenario: JetStream fields survive canonical round-trip

- **GIVEN** a canonical `jetstream` declaration
- **WHEN** it is decoded, merged, exposed through runtime configuration, and projected as normalized facts
- **THEN** its subjects, storage, retention, days, size, replicas, consumer, deliver policy, acknowledgement policy,
  maximum deliveries, acknowledgement wait, heartbeat, maximum pending acknowledgements, and interface metadata are
  unchanged

#### Scenario: Subject-only JetStream input is rejected

- **GIVEN** a canonical input `jetstream` declaration with one or more non-empty subjects and no `stream_name`
- **WHEN** its containing `PortConfig` is decoded for the input lane
- **THEN** decoding resolves the declaration for the input direction and fails before component initialization
- **AND** the typed failure identifies the port, kind `jetstream`, and field `stream_name`
- **AND** no component-local subject derivation or default supplies the missing backing stream
- **AND** no partially decoded input or output lane is assigned

#### Scenario: JetStream input without subjects is rejected

- **GIVEN** a canonical input `jetstream` declaration with a non-empty `stream_name` and no non-empty subjects
- **WHEN** the declaration is resolved
- **THEN** resolution fails before component initialization
- **AND** the typed failure identifies field `subjects`

#### Scenario: Subject-only JetStream output remains valid

- **GIVEN** a canonical output `jetstream` declaration with one or more non-empty subjects and no `stream_name`
- **WHEN** its containing `PortConfig` is decoded for the output lane
- **THEN** resolution succeeds with the declared subjects and an omitted stream name
- **AND** only the canonical generic provisioner may derive the physical stream name

#### Scenario: Retired agentic-model stream default is absent

- **GIVEN** an agentic-model component configuration
- **WHEN** its schema, defaults, documentation, and shipped configurations are inspected
- **THEN** no top-level `stream_name` field is exposed
- **AND** each JetStream input carries its explicit backing `stream_name` on the canonical port declaration

### Requirement: Input factories validate the effective configuration

The UDP, file, HTTP, and WebSocket input factories SHALL begin with their component defaults, security-check and
decode a supplied partial override without treating its zero-value decode target as a complete configuration, merge
the override using that component's established semantics, and validate the resulting effective configuration exactly
once during construction.

UDP port overrides SHALL use canonical complete named-port replacement through `MergePortConfig`, preserving default
ports that were not named by the override. WebSocket overrides SHALL decode into the already-defaulted configuration
or produce the same nested-field-preserving result. No factory-local compatibility field, fallback grammar, or global
`SafeUnmarshal` behavior change SHALL be introduced.

#### Scenario: Partial scalar override retains component defaults

- **GIVEN** a file, HTTP, or WebSocket input factory receives a secure partial configuration override
- **WHEN** the factory constructs the effective configuration
- **THEN** omitted defaulted fields retain their component defaults
- **AND** validation is applied to the effective configuration rather than the zero-valued partial decode target
- **AND** construction receives only that validated effective configuration

#### Scenario: UDP partial port override preserves the complete default topology

- **GIVEN** the UDP input defaults declare `udp_socket` and `nats_output`
- **AND** a secure partial override names only `nats_output`
- **WHEN** the factory merges the override
- **THEN** `udp_socket` remains unchanged
- **AND** `nats_output` is completely replaced without inheriting omitted metadata or kind-specific fields
- **AND** the merged effective configuration is validated before construction

### Requirement: Agentic-loop trajectory ports are canonical and complete

Agentic-loop defaults SHALL declare exactly these trajectory communication ports:

```text
output trajectories:
  kind: kv-write
  bucket: AGENT_TRAJECTORIES
  required: true
  interface: agentic.trajectory.fact v1

input trajectory_query:
  kind: nats-request
  subject: agentic.query.trajectory
  required: true
  interface: agentic.query v1
```

The query input SHALL be the runtime subscription authority. A hard-coded subscription that bypasses the declared
input SHALL NOT survive. Any configured override SHALL be complete named-port replacement and SHALL repeat kind,
required state, interface type/version, and resource/subject fields. Omission SHALL fail validation rather than inherit
meaning from the default.

#### Scenario: canonical trajectory defaults preserve their interfaces

- **GIVEN** agentic-loop uses its default port configuration
- **WHEN** ports are decoded, normalized, and installed
- **THEN** `trajectories` is the required `AGENT_TRAJECTORIES` KV writer with interface
  `agentic.trajectory.fact` v1
- **AND** `trajectory_query` is the required exact NATS request input with interface `agentic.query` v1

#### Scenario: incomplete trajectory override fails cleanly

- **GIVEN** a trajectory port override omits required state, interface identity, kind, or resource/subject
- **WHEN** complete named-port replacement is validated
- **THEN** startup fails with component and port context
- **AND** no default field, alias, hard-coded subscription, or compatibility shim repairs it

### Requirement: Trajectory evidence configuration names only the logical Store

Agentic-loop configuration SHALL expose `trajectory_evidence_storage_instance`, defaulting to `objectstore`.
Physical bucket configuration SHALL exist only on the storage owner. Agentic-loop SHALL expose no `content_bucket`,
`trajectory_detail`, `trajectory_cache_ttl`, cache authority, or backend-specific ObjectStore configuration.

The shipped provider representation SHALL be:

```json
"objectstore": {
  "type": "storage",
  "name": "objectstore",
  "enabled": true,
  "config": {
    "bucket_name": "AGENT_CONTENT"
  }
}
```

That provider SHALL advertise logical `StorageInstance="objectstore"` through its existing `store-provide` output,
independent of physical bucket name.

#### Scenario: agentic-loop config carries no physical storage prediction

- **GIVEN** default agentic-loop configuration
- **WHEN** its schema and effective configuration are inspected
- **THEN** it names only logical Store instance `objectstore`
- **AND** no content bucket, detail mode, cache TTL, or ObjectStore backend field exists

#### Scenario: retired trajectory fields are rejected

- **GIVEN** configuration supplies `content_bucket`, `trajectory_detail`, or `trajectory_cache_ttl`
- **WHEN** strict schema/runtime decoding runs
- **THEN** startup rejects the unknown retired field
- **AND** no ignored compatibility value survives

### Requirement: Every shipped agentic assembly owns the evidence provider

Each of these seven assembled configurations SHALL contain the enabled logical `objectstore` storage component backed
by physical bucket `AGENT_CONTENT`:

- `configs/agentic.json`
- `configs/flows/ops-agent.json`
- `configs/flows/ops-agent-test.json`
- `configs/flows/lesson-example.json`
- `configs/flows/crud-tools-test.json`
- `configs/flows/deep-research-test.json`
- `configs/flows/deep-research.json`

Each assembly SHALL inherit agentic-loop's canonical `trajectories` output rather than carrying a redundant override.
The complete-replacement override that omitted required/interface facts SHALL be deleted, not repaired with aliases or
partial merge behavior.

#### Scenario: all seven assemblies provide full evidence storage

- **WHEN** the seven shipped agentic configurations are decoded
- **THEN** each contains enabled component `objectstore` with bucket `AGENT_CONTENT`
- **AND** each omits a redundant agentic-loop `trajectories` override
- **AND** each inherits the canonical required/versioned trajectory ports

#### Scenario: physical bucket remains storage-owner configuration

- **GIVEN** one shipped assembly
- **WHEN** effective configs are inspected
- **THEN** `AGENT_CONTENT` appears on the ObjectStore component
- **AND** agentic-loop refers only to logical Store instance `objectstore`

### Requirement: Declarations are immutable within a generation

Before any component or retained-config mutation, a declaration-neutral live update SHALL prove exact normalized-fact
equality with the retained generation. A neutral update SHALL retain the current generation.

A declaration-affecting update SHALL either return typed `declaration_change_requires_replacement` before mutation or
prepare a complete replacement generation off-Registry. No path SHALL mutate a live component and then recapture its
declaration.

#### Scenario: Declaration-neutral update retains generation

- **GIVEN** a proposed live update whose normalized port facts equal the retained generation
- **WHEN** validation and application succeed
- **THEN** the component may update
- **AND** the retained generation identity and declaration remain unchanged

#### Scenario: Port change refuses before mutation

- **GIVEN** a proposed live update whose normalized port facts differ
- **WHEN** no prepared replacement path is used
- **THEN** the update returns `declaration_change_requires_replacement`
- **AND** the component and retained config remain unchanged

#### Scenario: Mutate then recapture is forbidden

- **GIVEN** a declaration-affecting update
- **WHEN** the runtime evaluates it
- **THEN** no path first mutates the live component and later recaptures ports

### Requirement: Replacement publishes one atomic generation

A failed replacement preparation SHALL leave the old component, retained configuration, generation record, and
resource projections unchanged and SHALL expose no partial new record.

A successful replacement SHALL assign a new local generation and atomically replace component, factory identity,
declaration, and resource projections as one Registry-visible mutation.

#### Scenario: Failed prepared replacement changes nothing

- **GIVEN** a current admitted generation and a replacement that fails preparation or conflict validation
- **WHEN** replacement is attempted
- **THEN** every read still returns the old complete generation
- **AND** no new resource fact is visible

#### Scenario: Successful replacement is observed as one set

- **GIVEN** a valid prepared replacement
- **WHEN** Registry commits it
- **THEN** readers and observers see either the old complete generation or the new complete generation
- **AND** no mixed component/declaration/resource state is visible

### Requirement: Removal deletes one complete generation record

Removal SHALL delete the component reference, factory identity, declaration, normalized facts, and resource
projections together.

#### Scenario: Removal has no residual declaration

- **GIVEN** an admitted component generation
- **WHEN** it is removed
- **THEN** the component and every declaration/resource view disappear in the same Registry mutation

### Requirement: Canonical port-field constraints govern runtime and generated schemas

The canonical port binding catalog SHALL own numeric minima and allowed directions through `PortFieldInfo`. Runtime port
resolution, runtime discovery, and checked-in schema generation SHALL consume the same metadata. Zero SHALL remain
omission for an `omitempty` numeric field.

#### Scenario: Runtime discovery reports the canonical constraint

- **GIVEN** the JetStream `max_ack_pending` field
- **WHEN** its `PortFieldInfo` is inspected
- **THEN** minimum is `-1`
- **AND** allowed directions contain only input

#### Scenario: Runtime validation consumes the minimum

- **GIVEN** a JetStream input declares `max_ack_pending: -2`
- **WHEN** canonical resolution runs
- **THEN** it fails with port, kind, and field context

#### Scenario: Generated schemas consume direction and minimum

- **GIVEN** a generated component schema contains JetStream ports
- **WHEN** input and output variants are inspected
- **THEN** the input contains `max_ack_pending` with minimum `-1`
- **AND** the output contains `max_ack_pending` constrained to the single value `0`
- **AND** omission remains valid while positive and `-1` output declarations fail schema validation

### Requirement: JetStream outputs reject consumer acknowledgement admission

A JetStream output SHALL reject any nonzero `max_ack_pending` because it creates no consumer. Zero or omission SHALL
remain valid.

#### Scenario: Positive or unlimited output is rejected

- **GIVEN** a JetStream output declares a positive value or `-1`
- **WHEN** canonical direction validation runs
- **THEN** configuration fails before component initialization
- **AND** the error identifies the output port, JetStream kind, and field

#### Scenario: Zero output preserves omission behavior

- **GIVEN** a JetStream output omits the field or supplies zero
- **WHEN** canonical resolution runs
- **THEN** the output remains valid
- **AND** no consumer-policy observation is created
