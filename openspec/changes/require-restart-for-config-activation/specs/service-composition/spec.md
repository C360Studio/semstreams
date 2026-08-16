## ADDED Requirements

### Requirement: Running service and component composition is immutable except rule definitions

`services.*`, `components.*`, `platform`, `nats`, and `model_registry` edits SHALL be durable desired next-boot
configuration. They SHALL NOT create, start, stop, remove, reconfigure, restart, or replace a service or component in
the running process.

Successful boot SHALL seal the effective service and component identities, declarations, dependencies, ports, and
configuration for that process lifetime.

Live rule-definition create/update/delete MAY remain available only through the dedicated `rule-hot-reload`
capability. It SHALL NOT change the Rule component's ports, dependencies, entity-watch buckets, integration mode,
producer identity, or projection bindings.

#### Scenario: Desired service edit does not mutate runtime

- **GIVEN** a running process with sealed service composition
- **WHEN** a `services.*` entry is added, enabled, disabled, removed, or reconfigured
- **THEN** the running service identities and instances remain unchanged

#### Scenario: Desired component edit does not mutate runtime

- **GIVEN** a running process with sealed component composition
- **WHEN** a `components.*` entry is added, enabled, disabled, removed, or reconfigured
- **THEN** the running component identities, generations, declarations, and instances remain unchanged

#### Scenario: Rule-definition activation stays inside the fixed Rule envelope

- **GIVEN** an admitted Rule processor with fixed boot configuration
- **WHEN** a rule definition is created, updated, or deleted through `rule-hot-reload`
- **THEN** the Rule processor may activate a new rule-set generation
- **AND** no service or component generation is created, removed, restarted, or replaced

#### Scenario: Rule component configuration still requires restart

- **WHEN** desired configuration changes Rule ports, dependencies, entity-watch buckets, integration mode, producer
  identity, or projection bindings
- **THEN** the running Rule processor remains unchanged
- **AND** the mutation reports restart required

## REMOVED Requirements

### Requirement: Running service composition is immutable while components remain runtime-configurable

**Reason**: service and component composition now share one boot activation boundary. Only Definition content inside
an already-admitted Rule processor may hot reload.

**Migration**: persist component and flow changes as desired next-boot state. Use the dedicated rule-definition API for
live expression and cron changes.
