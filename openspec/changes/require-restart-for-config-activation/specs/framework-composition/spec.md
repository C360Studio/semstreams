## ADDED Requirements

### Requirement: Composition consumes one captured component configuration

The composition root SHALL construct ComponentManager from one read of the existing configuration. ComponentManager
SHALL use that captured value to select, construct, validate, and admit the complete enabled component set for the
process.

A configuration write committed after construction begins SHALL NOT join or mutate the current composition. There
SHALL be no late configuration drain or post-construction dynamic component admission path.

This requirement SHALL NOT alter existing component or service `Start` and `Stop` mechanics, lifecyclejoin behavior,
failed-Start handling, shutdown ordering, ACK ordering, transport shutdown, or recovery behavior.

#### Scenario: Later component write waits for a later process

- **GIVEN** ComponentManager captured configuration C during construction
- **WHEN** component configuration C' commits before or after component Start
- **THEN** the process composes only from C
- **AND** C' does not create, remove, restart, or replace a component in that process

#### Scenario: Later model-registry write waits for a later process

- **GIVEN** ComponentManager captured configuration and resolved boot factories
- **WHEN** model-registry configuration changes
- **THEN** the running component set and instances remain unchanged

#### Scenario: Existing lifecycle behavior is not redesigned

- **WHEN** the fixed boot composition starts or stops
- **THEN** existing owner lifecycle mechanics govern the operation
- **AND** this capability claims no shutdown, restart, recovery, or lifecycle proof credit
