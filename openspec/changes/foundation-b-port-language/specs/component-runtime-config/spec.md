<!-- markdownlint-disable MD041 -->

## MODIFIED Requirements

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

## ADDED Requirements

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
