# component-runtime-config Specification

## Purpose
TBD - created by archiving change component-runtime-reconfig-http. Update Purpose after archive.
## Requirements
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

Each port SHALL contain the common fields `name`, `required`, `description`, and `external`, plus a nested `config`
object discriminated by exactly one of these kinds: `timer`, `network`, `file`, `http-client`, `nats`, `nats-request`,
`jetstream`, `kv-watch`, `kv-read`, `kv-write`, `store-read`, `store-provide`. `external` is an optional boolean on an
input declaring that the port is fed from outside the composition (a UI, a peer process, a rule action) — an operator
statement, not a predicted framework value; composition validation (`composition-validation`) treats it as the one
reason a required stream input may have no in-graph publisher. It travels through the strict envelope codec, port
resolution, the runtime `Port` view, complete named replacement, the admitted declaration and its boot parity check,
and the catalog export unchanged. An output declaring `external` SHALL fail resolution with the port-config error
naming the field; it is never silently ignored.

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
- **THEN** the definition and runtime views preserve identical direction, kind, required state, external marker,
  description, resource fields, and interface metadata
- **AND** the test that verifies the marker's round trip is `TestPortDefinitionExternalRoundTrip`

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

### Requirement: Component configuration activates only during process construction

ComponentManager SHALL read the existing configuration once during construction. That captured configuration SHALL
define the complete component set for the process lifetime. Configuration written after construction SHALL be durable
for a later process boot and SHALL NOT create, start, stop, remove, reconfigure, restart, reconcile, or replace a
component in the running process.

ComponentManager SHALL NOT subscribe to component or model-registry configuration changes. The generic runtime
component-config HTTP write and `watch_config` tool SHALL NOT exist. No alternate watcher, interface probe, or direct
KV operation SHALL bypass the boot boundary.

Config Manager persistence, version arbitration, watchers, reads, writes, and shutdown behavior SHALL remain
unchanged after successful Start. If the shared configuration bucket contains a foreign platform identity, Start SHALL
fail before arbitration, watchers, writes, or dependent construction; detached running mode SHALL NOT exist.

Start SHALL reject a nil context before mutating any state or contacting NATS. It is an exported, error-returning,
context-taking boundary, and a nil context reaches the JetStream client as a panic rather than a classified error.

Start SHALL acquire the shared configuration bucket under the context passed to Start; no constructor, factory, or
other non-lifecycle boundary SHALL perform that acquisition or invent a context for it. Acquisition SHALL resolve the
bucket through its `framework-bucket-catalog` descriptor rather than a locally spelled bucket configuration, so the
policy is the one the catalog declares whichever writer creates it first. That descriptor's strict retention refuses
a bucket whose policy can delete keys — a nonzero TTL or a binding size cap — naming the offending value, and never
repairs it in place: a create-once identity under an evicting policy expires and is reminted as a second authority,
which ADR-102 decision 7 forbids ever reconciling. Nothing SHALL be minted or created before that check passes.

The acquired handles SHALL NOT become usable by the exported write methods until Start has completed successfully.
Every Start that returns an error — a refused retention policy, a foreign identity, a pre-identity bucket, a lost
environment claim, a malformed record, or a failure to open watchers — SHALL leave `PushToKV`,
`PutComponentToKV` and `DeleteComponentFromKV` returning the not-acquired lifecycle error. Publishing the handles
at acquisition instead would let a caller overwrite the very bucket Start had just refused as another platform's,
which is the detached running mode this requirement says does not exist.

Before arbitration, Start SHALL establish the deployment's platform identity from the bucket's `platform_identity`
record, deciding from a single pre-mint read of the bucket's keys and under the context passed to Start:

- the record is present — Start SHALL adopt its identifier as the effective `platform.id`, and SHALL fail unless the
  record's organization equals the configuration's `platform.org` and the configuration's `platform.id` equals the
  record's stem. Configuration declares the STEM and only the stem: the minted identifier is not a declarable value,
  and a configuration declaring it SHALL be refused with guidance naming the stem to declare instead — decided by
  comparison against the recorded identifier, never by inspecting the value's grammar. An adopted identifier SHALL be
  validated under the same segment grammar and authority-pair bound as a configured one;
- the record is absent and the bucket holds no other key — Start SHALL mint the entropy suffix, write the record with
  an atomic `Create`, and adopt the result; if that `Create` conflicts with a concurrent process, Start SHALL re-read
  the record and adopt the winner's identifier rather than its own;
- the record is absent and the bucket holds other keys — Start SHALL fail, naming that the bucket predates identity
  minting and instructing fresh storage. It SHALL mint nothing and SHALL create nothing.

Before creating or adopting the record, Start SHALL claim the bucket for this deployment's `platform.environment`
with an atomic create of an internal guard key, and SHALL fail — naming both environments — when the bucket was
already claimed by a different one. At most one environment may establish against one configuration bucket. The claim
SHALL precede the record so that a failure between the two leaves a state a same-environment boot completes and a
different-environment boot is refused. The guard is internal: it is NOT a field of the record, whose shape is a
cross-repo read contract.

The record SHALL carry exactly the fields `org`, `stem`, and `id`. First-boot detection SHALL ignore the
`platform_identity` key and the environment guard key, so a boot that has just created either is still a first boot. The identity guard SHALL compare
the effective identifier. Configuration synchronization SHALL NOT apply the KV `platform` key to the running
configuration — it remains a published mirror only — and version arbitration SHALL never write, overwrite, or apply
`platform_identity`.

#### Scenario: Foreign platform identity fails before publication is available

- **GIVEN** the shared configuration bucket contains another platform identity
- **WHEN** Config Manager starts
- **THEN** Start returns the identity mismatch
- **AND** no configuration watcher, write, or dependent component construction begins

#### Scenario: Post-construction edit leaves runtime unchanged

- **GIVEN** ComponentManager constructed component A from configuration C
- **WHEN** configuration C' for A is persisted
- **THEN** the running A and its effective configuration remain unchanged
- **AND** C' is available to a later process boot

#### Scenario: Post-construction membership change waits for reboot

- **GIVEN** ComponentManager constructed a fixed component set
- **WHEN** later configuration adds B or disables or removes A
- **THEN** no running component is created, stopped, removed, restarted, or replaced
- **AND** a later process boot selects from the then-current persisted configuration

#### Scenario: Model-registry write is not a lifecycle command

- **GIVEN** a running process
- **WHEN** model-registry configuration changes
- **THEN** ComponentManager does not restart or replace a component

#### Scenario: First boot mints and persists the platform identity under the Start context

- **GIVEN** an empty configuration bucket and a file declaring `platform.id` `dep`
- **WHEN** Config Manager starts
- **THEN** `platform_identity` is created carrying exactly `org`, `stem` `dep`, and `id` `dep-` plus six hex bytes,
  the effective configuration's `platform.id` is that identifier, and the pushed `platform` key carries it
- **AND** the boot is still treated as a first boot, so the file configuration is pushed to the bucket
- **AND** every KV operation of the mint uses the context passed to Start
- **AND** the test that verifies this is `TestConfigManagerFirstBootMintsPlatformIdentity`

#### Scenario: A later boot and a co-process adopt the persisted identity

- **GIVEN** `platform_identity` records organization `acme`, stem `dep`, and identifier `dep-7f3a9c`
- **WHEN** a process whose file declares `platform.id` `dep` starts, and concurrently a second process with the same file starts
- **THEN** both adopt `dep-7f3a9c` and neither creates a second record — the loser of the atomic Create reads the winner's
- **AND** a file declaring `other`, or one declaring a different `platform.org`, returns the identity mismatch
- **AND** a file declaring `platform.id` `dep-7f3a9c` — the minted identifier rather than the stem — is refused with
  guidance to declare `dep`
- **AND** the tests that verify this are `TestConfigManagerAdoptsPersistedPlatformIdentity`,
  `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity` and
  `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance`

#### Scenario: A bucket that predates identity minting refuses without minting

- **GIVEN** a configuration bucket holding `platform` and `version` keys and no `platform_identity` record
- **WHEN** Config Manager starts
- **THEN** Start fails naming the pre-identity bucket as the cause and instructing fresh storage
- **AND** no `platform_identity` key exists in the bucket afterwards and no suffix was minted
- **AND** the test that verifies this is `TestPreIdentityBucketRefusesStartWithoutMinting`

#### Scenario: A second environment cannot establish against the same bucket

- **GIVEN** an empty configuration bucket
- **WHEN** two deployments declaring the same `platform.org` and `platform.id` but `platform.environment` `prod` and
  `dev` start concurrently
- **THEN** exactly one Start succeeds and the other fails naming both environments
- **AND** the refused deployment publishes no configuration
- **AND** the test that verifies this is `TestConcurrentFirstBootRefusesASecondEnvironment`

#### Scenario: A bucket whose policy can evict the identity is refused before minting

- **GIVEN** a configuration bucket created by another writer with a TTL, or with a binding size cap
- **WHEN** Config Manager starts
- **THEN** Start fails naming the bucket and the offending policy value, and creates no `platform_identity` record
- **AND** the deployment never mints a second authority for itself across restarts
- **AND** the tests that verify this are `TestEvictingConfigBucketRefusesStart` and
  `TestIdentityUnderAnEvictingBucketNeverRemints`

#### Scenario: A refused Start leaves no writer armed

- **GIVEN** a configuration bucket a foreign deployment established
- **WHEN** Config Manager starts against it and Start refuses the foreign identity
- **THEN** `PushToKV`, `PutComponentToKV` and `DeleteComponentFromKV` each return the not-acquired lifecycle error
- **AND** the bucket's contents are unchanged, key for key and value for value
- **AND** the test that verifies this is `TestRefusedStartDisarmsEveryExportedWriter`

#### Scenario: Start rejects a nil context without side effects

- **GIVEN** a constructed Config Manager
- **WHEN** Start is called with a nil context
- **THEN** it returns an invalid-configuration error rather than panicking
- **AND** no shutdown channel is replaced, no bucket is created, and no handle is acquired
- **AND** the test that verifies this is `TestStartRejectsNilContextWithoutSideEffects`

#### Scenario: A KV platform write never changes the running authority

- **GIVEN** a running Config Manager whose effective `platform.id` is `dep-7f3a9c`
- **WHEN** another writer puts a `platform` key declaring `platform.id` `other` into the shared bucket
- **THEN** the effective configuration's `platform.id` remains `dep-7f3a9c`
- **AND** the test that verifies this is `TestKVPlatformKeyIsAMirrorNotASource`

### Requirement: The engine-owned-revision skip suppresses only the in-memory re-apply

The config watcher SHALL, for a Manager-owned revision (`revision <= engineHighWaterRev`), suppress only the redundant
in-memory re-apply of the value and still notify matching subscribers. A Manager-owned revision MUST NOT cause the
durable desired-state notification to be dropped.

#### Scenario: an engine-owned event notifies subscribers

- **GIVEN** Config Manager has written a component and raised its high-water revision
- **WHEN** the watcher delivers that event at or below the high-water revision
- **THEN** the in-memory desired state is not re-applied from the event
- **AND** subscribers matching the event key are still notified

### Requirement: Runtime config-map mutations are serialized so a concurrent add/remove is never lost

`SafeConfig` SHALL serialize each read-modify-write across clone, mutation, validation, and swap. Config Manager's KV
watcher apply path and synchronous desired-state write paths SHALL use that serialized mutation boundary so concurrent
component configuration changes cannot clobber one another. Last-writer-wins replacement of the whole component map
from independently cloned snapshots is forbidden.

This requirement governs Config Manager's in-memory durable desired-state view. It does not authorize ComponentManager
to reconcile or mutate the running component set.

#### Scenario: concurrent add and remove both take effect

- **GIVEN** desired configuration with components A and B
- **WHEN** one goroutine adds component C while another removes B
- **THEN** the resulting desired configuration contains A and C and does not contain B
- **AND** neither mutation is silently dropped

#### Scenario: watcher apply and caller add do not clobber

- **WHEN** the KV watcher applies an external `components.X` update while `PutComponentToKV("Y", ...)` runs concurrently
- **THEN** the final in-memory desired configuration contains both X's update and Y
- **AND** subscribers are notified for both keys

### Requirement: The authority pair is bounded against the value that will be minted

Configuration load SHALL bound the authority pair against the identifier that will actually be minted from it —
the declared pair plus the seven-byte entropy suffix, reserved at load as
`entity-id-contract` specifies. Start SHALL bound the effective pair — minted or adopted — against the full
family-table budget, WITHOUT the declaration reserve, because that pair already carries the suffix; reserving twice
would refuse at Start a pair that passed load. Together these make a pair that passes load and then cannot carry a
framework identity impossible. The framework SHALL NOT probe, roll back, or delete an identity record to discover
the bound — ADR-102 decision 7 forbids rewriting a minted authority, so the only safe order is to refuse before the
`Create`.

#### Scenario: a pair that only fits unsuffixed does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` fit the family-table budget exactly but leave no
  room for the seven-byte suffix
- **WHEN** the deployment boots against an empty bucket
- **THEN** configuration load fails, Start is never reached, and no `platform_identity` record is created
- **AND** the test that verifies this is `TestConfigRejectsPairThatOnlyFitsUnsuffixed`

#### Scenario: a pair at the declarable budget mints and starts

- **GIVEN** a configuration whose `platform.org` and `platform.id` total exactly the declarable budget
- **WHEN** the deployment starts against an empty bucket
- **THEN** the suffix is minted, the effective pair equals the family-table budget, and Start succeeds
- **AND** the test that verifies this is `TestMaximumDeclarablePairMintsAndStarts`

