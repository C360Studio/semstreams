<!-- markdownlint-disable MD041 -->

## MODIFIED Requirements

### Requirement: Typed request ports define mutation composition

Graph-mutation composition SHALL use canonical resolved `nats-request` facts.

Exactly one compatible required graph-mutation provider input SHALL exist. Compatible requester outputs MAY connect to
that provider. Zero compatible providers or multiple compatible providers SHALL fail composition.

Provider cardinality SHALL be determined from resolved component topology and SHALL NOT be inferred from process count,
flat port fields, or hard-coded subjects.

#### Scenario: Canonical requester and provider connect

- **GIVEN** a requester output and provider input that both resolve as compatible required graph-mutation
  `nats-request` ports
- **WHEN** framework composition is built
- **THEN** the requester connects to the provider using their resolved interface and subject facts
- **AND** no consumer reconstructs their protocol from concrete config types

#### Scenario: Missing provider fails composition

- **GIVEN** one or more graph-mutation requester outputs
- **WHEN** no compatible required provider input exists
- **THEN** composition fails before runtime startup
- **AND** no hidden or hard-coded provider is supplied

#### Scenario: Multiple providers fail composition

- **GIVEN** more than one compatible required graph-mutation provider input
- **WHEN** framework composition is built
- **THEN** composition fails with a provider-cardinality error
- **AND** process count does not select a provider

#### Scenario: Legacy declaration cannot satisfy topology

- **GIVEN** a requester or provider expressed through a retired flat field, legacy alias, plain-NATS substitute, or
  otherwise noncanonical declaration
- **WHEN** framework composition is built
- **THEN** that declaration cannot satisfy graph-mutation topology
- **AND** no compatibility shim, interface restoration, or subject fallback is applied

## ADDED Requirements

### Requirement: Store providers start and register before subscribing consumers

`ComponentManager` SHALL partition the cold-boot component set using the existing `component.StoreProvider`
interface. All StoreProvider components SHALL start concurrently in a provider barrier, and each provider's stores
SHALL register immediately after its Start succeeds. Only after every provider has started and registered SHALL all
remaining components start concurrently in the existing consumer barrier.

Invalid, empty, nil, or duplicate claimed `StorageInstance` registration SHALL be that provider's startup error and
SHALL fail the cold-boot barrier. The first registered provider SHALL remain the incumbent; a rival SHALL become failed
and SHALL NOT clobber it. Dynamic provider start SHALL propagate the same registration error and failed state.
Provider stop or reconfiguration SHALL continue to deregister before Close through existing lifecycle hooks.

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

#### Scenario: dynamic duplicate registration is a failed component start

- **GIVEN** a running composition with one registered Store provider
- **WHEN** a dynamically added provider claims the same instance
- **THEN** its registration error propagates and its component state is failed
- **AND** component-manager Health names the rival and its error

#### Scenario: provider ordering uses no guessed readiness mechanism

- **WHEN** the lifecycle implementation is inspected and exercised under delayed provider starts
- **THEN** the provider barrier supplies ordering directly
- **AND** no sleep, polling loop, readiness timeout, port-derived dependency graph, or topological scheduler exists
