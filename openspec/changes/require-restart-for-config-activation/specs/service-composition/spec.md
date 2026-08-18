## ADDED Requirements

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

## REMOVED Requirements

### Requirement: Running service composition is immutable while components remain runtime-configurable

**Reason**: service and component composition now share one boot boundary. Generic live component configuration and
replacement retire.

**Migration**: persist service or component configuration and restart the process. Use saved Flow authoring and
optional explicit component-configuration publication where applicable.
