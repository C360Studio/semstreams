# service-composition Delta

## MODIFIED Requirements

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
