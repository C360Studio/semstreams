# service-composition Delta

## MODIFIED Requirements

### Requirement: Boot consumes effective desired service state after unchanged version arbitration

Config Manager SHALL preserve accepted version arbitration. After `config.Manager.Start` completes selection and
synchronization, every subsequent validation, stream-provisioning, logger, and service-composition decision SHALL use
the resulting `SafeConfig` and SHALL NOT use the stale original file-loaded config object.

Effective configuration validation, NATS limit verification, and stream provisioning SHALL complete before the NATS
log-forwarding handler is installed. LOGS and all other declared streams SHALL derive from effective configuration.

#### Scenario: effective streams precede forwarding

- **GIVEN** file and KV configuration declare different services or streams and arbitration selects KV
- **WHEN** the process completes Phase-A composition
- **THEN** validation and stream provisioning use the effective KV-selected configuration
- **AND** the forwarding handler is installed only after the effective LOGS stream exists

### Requirement: One pure outer resolver owns service-map structure

The structural outer resolver SHALL remain deterministic, non-mutating, and service-agnostic. It SHALL NOT decode,
default, normalize, or validate service-specific inner JSON. An absent or disabled service entry SHALL invoke no inner
decoder or validator.

Enabled service constructors SHALL ordinarily own their inner semantics. For `log-forwarder`, one repository-internal
policy resolver SHALL solely own inner decode, INFO defaulting, normalization, and validation because both its service
constructor and Phase-A NATS handler composition consume the same policy. Both consumers SHALL delegate to that one
owner. The existing named public `service.LogForwarderConfig` type and runtime identity SHALL remain unchanged.

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

Outer `ServiceConfig.Enabled` SHALL remain the sole activation input for optional services. For `log-forwarder`, its
minimum level SHALL govern NATS forwarding only; the CLI/global level SHALL govern local output and the existing counter
SHALL remain WARN+. Effective exclusions SHALL union configured exact/dotted-prefix exclusions with the mandatory
`flow-service.websocket` safety exclusion.

#### Scenario: effective forwarding policy is destination-specific

- **GIVEN** enabled log-forwarder policy with a minimum level and exclusions
- **WHEN** a record is emitted
- **THEN** local output follows the CLI/global level
- **AND** NATS delivery follows the forwarder level and effective exclusions
