## ADDED Requirements

### Requirement: Composition consumes one captured component configuration

The composition root SHALL construct ComponentManager from one read of the existing configuration. ComponentManager
SHALL use that captured value to select, construct, validate, and admit the complete enabled component set for the
process.

A configuration write committed after construction begins SHALL NOT join or mutate the current composition. There
SHALL be no late configuration drain or post-construction dynamic component admission path.

This requirement defines composition selection only. It SHALL NOT define component or service `Start` and `Stop`
mechanics, failed-Start handling, shutdown ordering, ACK ordering, transport shutdown, or recovery behavior.

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

### Requirement: Component starts form a fail-closed boot barrier

A lifecycle component whose `Start` returns an error during `Manager.StartAll` MUST fail composition-root boot.
`ComponentManager.Start` is a component-start barrier: it launches component `Start` calls concurrently, returns
only after every launched call has returned, and joins the errors of every component that failed. There is no
fire-and-forget component launch at boot and no compatibility variant that preserves one.

While a managed component is retained in failed state with a last error, the component manager's health projection
MUST report that state by component name rather than treating it as healthy. Successful failed-Start rollback MAY
transition the component to stopped state and clear that error. The process MUST NOT bring up its HTTP surface or
report service health while boot-time component starts are outstanding or failed.

#### Scenario: a boot-time component start failure fails StartAll

- **GIVEN** a registered lifecycle component whose `Start` returns an error
- **WHEN** `Manager.StartAll` runs
- **THEN** `ComponentManager.Start` returns an error naming the failed component, `StartAll`
  fails, the HTTP surface is never brought up, and the process exits non-zero

#### Scenario: StartAll waits for every component start before proceeding

- **GIVEN** lifecycle components whose `Start` calls are launched concurrently
- **WHEN** `ComponentManager.Start` returns
- **THEN** every launched component `Start` call has already returned (successfully or not),
  so post-start boot steps observe the final boot-time component state, never a mid-start race

#### Scenario: multiple boot-time failures are all reported

- **GIVEN** two or more components whose `Start` calls return errors in the same boot
- **WHEN** `ComponentManager.Start` joins the results
- **THEN** the returned error names each failed component and its error, not only the first

#### Scenario: a failed component state is visible in health

- **GIVEN** a managed component whose state is failed and whose last error is recorded
- **WHEN** the component manager's health check runs
- **THEN** the health check returns an error naming the failed component and its last error
- **AND** the health check clears only after the component no longer has failed state

#### Scenario: no unconsumed error hook survives

- **GIVEN** the component error hook registration surface
- **WHEN** no production caller consumes it after boot-time propagation lands
- **THEN** the hook is deleted rather than left as a dead exported surface (a signal read by
  nothing is not enforcement)

### Requirement: Cold-boot Store providers register before subscribing consumers

`ComponentManager` SHALL partition the cold-boot component set using the existing `component.StoreProvider`
interface. All StoreProvider components SHALL start concurrently in a provider barrier, and each provider's stores
SHALL register immediately after its Start succeeds. Only after every provider has started and registered SHALL all
remaining components start concurrently in the existing consumer barrier.

Invalid, empty, nil, or duplicate claimed `StorageInstance` registration SHALL be that provider's startup error and
SHALL fail the cold-boot barrier. The first registered provider SHALL remain the incumbent; a rival SHALL become failed
and SHALL NOT clobber it. Provider stop SHALL deregister its tracked stores before the provider closes them.

A non-provider, or a StoreProvider that legitimately returns no stores, SHALL remain a no-op for registration.
This phase SHALL NOT introduce sleep, polling, an arbitrary readiness deadline, port-derived dependency edges, or a
general topological scheduler.

#### Scenario: provider registration precedes agentic-loop subscriptions

- **GIVEN** an ObjectStore provider and an agentic-loop consumer in the cold-boot component set
- **WHEN** `ComponentManager` starts the set
- **THEN** the provider Start and Store registration complete before agentic-loop Start begins
- **AND** agentic-loop installs subscriptions only in the consumer phase

#### Scenario: independent providers remain concurrent

- **GIVEN** two StoreProvider components with independent logical instances
- **WHEN** the provider phase runs
- **THEN** their Start calls MAY execute concurrently
- **AND** the consumer barrier waits for both registrations to finish

#### Scenario: non-provider consumers remain concurrent

- **GIVEN** multiple non-provider components after a successful provider phase
- **WHEN** the consumer phase runs
- **THEN** their Start calls execute under the existing parallel barrier
- **AND** no general dependency graph serializes them

#### Scenario: duplicate provider fails boot without replacing the incumbent

- **GIVEN** two providers claim the same non-empty `StorageInstance`
- **WHEN** their stores register
- **THEN** the first registered store remains in the registry
- **AND** the rival provider becomes failed and cold boot fails with the duplicate error
- **AND** the error is not logged and skipped

#### Scenario: provider ordering uses no guessed readiness mechanism

- **WHEN** the lifecycle implementation is inspected and exercised under delayed provider starts
- **THEN** the provider barrier supplies ordering directly
- **AND** no sleep, polling loop, readiness timeout, port-derived dependency graph, or topological scheduler exists

## REMOVED Requirements

### Requirement: Component start failures fail boot closed and surface in health

**Reason**: the legacy requirement mixed the retained cold-boot barrier with retired config-drain, watcher-driven
component admission, and post-boot restart behavior.

**Migration**: use the fixed boot composition and the `Component starts form a fail-closed boot barrier` requirement.
Persist later component configuration for a later process boot.

### Requirement: Store providers start and register before subscribing consumers

**Reason**: the legacy requirement mixed the retained cold-boot provider barrier with retired dynamic provider
admission and reconfiguration behavior.

**Migration**: use the `Cold-boot Store providers register before subscribing consumers` requirement. Provider-set
changes take effect only during a later process boot.
