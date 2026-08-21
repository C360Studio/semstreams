# service-composition Specification

## Purpose
Defines process-local service and component composition, durable next-boot desired configuration, and the fixed boot
boundary. It does not govern service health or readiness semantics.
## Requirements
### Requirement: Boot consumes effective desired service state after unchanged version arbitration

Config Manager SHALL preserve accepted version arbitration: a newer file version SHALL push file state, while an equal
or older file version SHALL select KV. An equal-version file-content edit alone SHALL NOT overwrite KV.

After `config.Manager.Start` completes selection and synchronization, every subsequent validation,
stream-provisioning, logger, and service-composition decision SHALL use the resulting `SafeConfig` and SHALL NOT use
the stale original file-loaded config object.

When arbitration selects KV, synchronization SHALL start with an empty Services map and load only current
`services.*` keys. Every other top-level config section SHALL retain its existing synchronization behavior.

Effective configuration validation, NATS limit verification, and stream provisioning SHALL complete before the NATS
log-forwarding handler is installed. LOGS and all other declared streams SHALL derive from effective configuration.

#### Scenario: Equal version selects KV

- **GIVEN** file and KV config have equal versions and different service content
- **WHEN** Config Manager starts
- **THEN** KV service content is selected
- **AND** the file can overwrite KV only after its version is advanced

#### Scenario: KV deletion does not survive from file

- **GIVEN** version arbitration selects KV, a service exists in the file Services map, and no current KV
  `services.<name>` key exists
- **WHEN** service state is synchronized
- **THEN** the effective Services map omits that service

#### Scenario: Composition reads post-Start truth

- **GIVEN** Config Manager selected effective desired state different from the original file object
- **WHEN** services are composed
- **THEN** construction uses the effective `SafeConfig` Services map

#### Scenario: effective streams precede forwarding

- **GIVEN** file and KV configuration declare different services or streams and arbitration selects KV
- **WHEN** the process completes Phase-A composition
- **THEN** validation and stream provisioning use the effective KV-selected configuration
- **AND** the forwarding handler is installed only after the effective LOGS stream exists

### Requirement: One pure outer resolver owns service-map structure

One deterministic, non-mutating outer `ServiceConfigs` resolver SHALL treat the map key as sole identity and
`ServiceConfig.Name` SHALL NOT exist.

The resolver SHALL deep-clone the map, structurally canonicalize raw inner JSON, materialize absent
`component-manager` and required `service-manager` entries with `Enabled: true`, preserve explicit false, retain
existing optional outer defaults as optional, and SHALL NOT inject message-logger.

The resolver SHALL return enabled and disabled entries without service-specific inner decoding, validation, defaulting,
schema interpretation, or codec callbacks. Disabled-only resolution SHALL invoke no constructor or inner validator.

Enabled service constructors SHALL ordinarily own their inner semantics. For `log-forwarder`, one repository-internal
policy resolver SHALL solely own inner decode, INFO defaulting, normalization, and validation because both its service
constructor and Phase-A NATS handler composition consume the same policy. Both consumers SHALL delegate to that one
owner. The existing named public `service.LogForwarderConfig` type and runtime identity SHALL remain unchanged.

#### Scenario: Map key is sole identity

- **GIVEN** a service entry keyed by `metrics`
- **WHEN** outer resolution runs
- **THEN** `metrics` is its identity
- **AND** no redundant Name field, alias, or inferred identity exists

#### Scenario: Mandatory absence materializes enabled

- **GIVEN** `component-manager` or required `service-manager` is absent from desired config
- **WHEN** outer resolution runs
- **THEN** the resolved entry exists with `Enabled: true`

#### Scenario: Explicit mandatory false is preserved

- **GIVEN** a desired mandatory outer entry explicitly has `Enabled: false`
- **WHEN** outer resolution runs
- **THEN** false remains visible for restart comparison and boot validation

#### Scenario: Constructor owns inner semantics

- **GIVEN** an enabled service has structurally valid but semantically invalid canonical raw config
- **WHEN** outer resolution and later construction run
- **THEN** outer resolution does not classify the semantic error
- **AND** the service constructor owns the resulting validation failure

#### Scenario: disabled forwarding does not decode inner policy

- **GIVEN** effective `log-forwarder` configuration is absent or outer-disabled
- **WHEN** boot and service composition resolve it
- **THEN** no log-forwarder inner decoder or validator runs
- **AND** no NATS log handler is created

#### Scenario: enabled forwarding has one semantic owner

- **GIVEN** effective `log-forwarder` configuration is enabled
- **WHEN** boot composes its handler and the service constructor builds its service
- **THEN** both consume the same internally resolved policy semantics
- **AND** no second decoder, default table, normalization rule, or validator exists

### Requirement: Composition seals before any service starts or contributes HTTP or OpenAPI

`CreateService` and error-returning `RegisterInstance` SHALL be pre-seal composition-root writers. Both SHALL reject
duplicates and SHALL return a typed error after seal. No void wrapper, overwrite behavior, alias, or compatibility shim
SHALL remain.

`StartAll` SHALL verify every enabled configured optional service and mandatory `component-manager` is present,
creating the mandatory service before seal if needed. It SHALL retain the sorted actual full identity set and SHALL
seal before any service Start, route binding, or OpenAPI exposure.

Each binary composition root SHALL register its required fixed services before seal. The framework production root
SHALL register `milestone`. Manager SHALL NOT infer omitted fixed services or introduce a fixed manifest, group, or
generic completeness primitive.

Configured/mandatory failure before seal SHALL start no service and expose no route/OpenAPI. A Start failure after seal
SHALL NOT mutate the sealed identity set.

#### Scenario: Post-seal writer is rejected

- **GIVEN** `StartAll` has sealed composition
- **WHEN** a caller invokes `CreateService` or `RegisterInstance`
- **THEN** the call returns a typed sealed-composition error
- **AND** the identity set is unchanged

#### Scenario: Configured failure has no partial startup

- **GIVEN** an enabled configured optional service was not constructed or mandatory `component-manager` cannot be
  present
- **WHEN** `StartAll` validates composition
- **THEN** startup fails before any service starts or contributes HTTP/OpenAPI

#### Scenario: Fixed services belong to the root

- **GIVEN** the framework production composition root
- **WHEN** it prepares service composition
- **THEN** it handles error-returning registration of `milestone` before seal
- **AND** Manager does not infer fixed-service requirements

### Requirement: Optional activation and retained service schema are next-boot only

Outer `ServiceConfig.Enabled` SHALL remain the sole activation input for optional services. A configured
`component-manager` or manager-infrastructure `service-manager` with `Enabled: false` SHALL fail boot. Every successful
boot SHALL have active manager infrastructure and active `component-manager`.

`RuntimeConfigurable`, the service runtime schema marker, the service mutation watcher/apply path, and exported dynamic
per-service lifecycle methods SHALL NOT exist. Retained message-logger and metrics knobs SHALL be next-boot only.
Message-logger inner `enabled`/`log_level` and metrics inner `enabled` SHALL be strictly rejected.

For `log-forwarder`, its minimum level SHALL govern NATS forwarding only; the CLI/global level SHALL govern local
output and the existing counter SHALL remain WARN+. Effective exclusions SHALL union configured exact/dotted-prefix
exclusions with the mandatory `flow-service.websocket` safety exclusion.

#### Scenario: Optional service follows outer Enabled

- **GIVEN** an optional service desired entry
- **WHEN** its outer Enabled value is true or false at boot
- **THEN** the service is respectively constructed or omitted

#### Scenario: Mandatory disable fails boot

- **GIVEN** configured `component-manager` or `service-manager` has `Enabled: false`
- **WHEN** boot validation runs
- **THEN** boot fails until the invalid desired state is corrected

#### Scenario: effective forwarding policy is destination-specific

- **GIVEN** enabled log-forwarder policy with a minimum level and exclusions
- **WHEN** a record is emitted
- **THEN** local output follows the CLI/global level
- **AND** NATS delivery follows the forwarder level and effective exclusions

### Requirement: GET services reports deterministic structural restart need

`GET /services` SHALL compare the immutable exact resolved boot desired map with the current outer-resolved desired map
on each read. It SHALL install no service-change watcher, retain no change history, invoke no constructor, and perform
no inner semantic validation.

The response SHALL sort existing runtime rows and `pending_service_changes` by service key and SHALL report at most one
pending row per key. `restart_required` SHALL be true exactly when at least one pending row exists. Classification
precedence SHALL be:

- absent at boot and desired enabled: `add`;
- disabled at boot and desired enabled: `enable`;
- enabled at boot and desired disabled: `disable`;
- enabled at boot and desired absent: `remove`; and
- enabled in both with different canonical raw inner JSON: `reconfigure`.

Absent-to-disabled, disabled-to-absent, and disabled-to-disabled differences SHALL emit no row. An activation
transition SHALL take precedence over a simultaneous config difference.

Enabled invalid or unknown desired config SHALL still be reported structurally and MAY fail on restart through
constructor/registry validation. `restart_required` SHALL promise only that restart is required to attempt consumption.
No `restart_blocked`, config-error status, speculative validation, or restart-success promise SHALL exist.

Explicit false for a mandatory outer entry SHALL emit pending `disable` but SHALL remain boot-invalid. Reverting or
deleting it SHALL materialize enabled and clear the comparison. Only a successful boot consuming valid desired state
SHALL start with an empty pending set.

#### Scenario: Reconfigure compares canonical raw config

- **GIVEN** an optional service is enabled at boot and currently desired enabled with different canonical raw config
- **WHEN** `GET /services` is read
- **THEN** one sorted `reconfigure` row exists
- **AND** `restart_required` is true

#### Scenario: Disabled churn does not require restart

- **GIVEN** a service is disabled or absent at boot and remains disabled or absent in current desired state
- **WHEN** its raw inner config or absent/disabled spelling changes
- **THEN** no pending row is emitted
- **AND** no inner validator runs

#### Scenario: Mandatory false is pending but cannot boot

- **GIVEN** mandatory outer state was enabled at boot and current desired explicitly sets false
- **WHEN** `GET /services` is read and restart is attempted
- **THEN** one `disable` row makes `restart_required` true
- **AND** restart fails until corrected

#### Scenario: Mandatory delete clears override

- **GIVEN** current desired explicitly disables a mandatory outer entry
- **WHEN** that explicit entry is deleted
- **THEN** outer resolution materializes enabled
- **AND** the pending disable clears

### Requirement: Runtime route and OpenAPI views derive from sealed composition

`GET /services` runtime rows SHALL equal the full sorted sealed identity set. Bound service routes SHALL equal the
sealed subset implementing `HTTPHandler`. Per-service OpenAPI contributors SHALL equal the sealed OpenAPI-capable
subset under the actual service interfaces.

Desired config edits SHALL NOT drift those views. Static manager-owned endpoints SHALL NOT invent a service identity.
Service health, `/services/health`, readiness, and `GRAPH_STATUS` semantics SHALL remain unchanged.

#### Scenario: Sealed subsets remain aligned

- **GIVEN** one sealed composition containing services with and without HTTP/OpenAPI capability
- **WHEN** runtime rows, routes, and generated OpenAPI are inspected
- **THEN** rows equal the full sealed set
- **AND** routes and per-service OpenAPI equal their capable sealed subsets

#### Scenario: Desired edit does not drift runtime views

- **GIVEN** a sealed running process
- **WHEN** desired service config changes
- **THEN** runtime rows, routes, OpenAPI contributors, health, and readiness continue describing the sealed process

### Requirement: Running service and component composition is fixed at boot

The process SHALL select its service and component composition during boot. Later edits to `services.*`,
`components.*`, `platform`, `nats`, or `model_registry` SHALL NOT create, start, stop, remove, reconfigure, restart,
reconcile, or replace a service or component in that process.

The selected service and component identities, declarations, dependencies, ports, and concrete instances SHALL remain
fixed until process shutdown. Existing service and component lifecycle mechanics SHALL remain unchanged.

#### Scenario: Service configuration edit does not mutate runtime

- **GIVEN** a running process with its boot-selected service composition
- **WHEN** a `services.*` entry is added, enabled, disabled, removed, or reconfigured
- **THEN** running service identities and instances remain unchanged

#### Scenario: Component configuration edit does not mutate runtime

- **GIVEN** a running process with its boot-selected component composition
- **WHEN** a `components.*` entry is added, enabled, disabled, removed, or reconfigured
- **THEN** running component identities, declarations, and instances remain unchanged

#### Scenario: Rule behavior is outside this composition change

- **WHEN** existing Rule storage or watchers process a Rule change
- **THEN** this capability adds no Rule behavior or completion claim
- **AND** any future Rule hot-reload contract remains separate work
