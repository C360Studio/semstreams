# framework-composition Specification

## Purpose
TBD - created by archiving change framework-package-boundary-cleanup. Update Purpose after archive.
## Requirements
### Requirement: Framework packages have an explicit ownership basis

Every package admitted to the SemStreams production composition MUST be substrate-shaped and MUST either have two
independent product consumers or be required to make SemStreams' defining graph, KV, rule, lifecycle, storage, or
agentic substrate usable. Standards interest, sponsor interest, an in-repo example, or first-party authorship alone
MUST NOT establish framework ownership.

#### Scenario: one product owns a standards adapter

- **GIVEN** an adapter translates one product protocol or vocabulary into SemStreams primitives
- **WHEN** no second independent product consumes that adapter contract
- **THEN** the product registers and maintains the adapter
- **AND** the framework production composition does not import or advertise it

#### Scenario: a defining ergonomic bridge remains framework substrate

- **GIVEN** recorded evidence that agents do not reliably exercise graph primitives through manual tool selection
- **WHEN** SemStreams provides a product-neutral bounded capability that turns natural-language intent into graph
  classification, retrieval, fusion, evidence, and results
- **THEN** that capability MAY remain framework-owned even before two products declare identical flow configuration
- **AND** its contract MUST remain graph-focused and free of product domain policy

### Requirement: Composition roots register explicit capability sets

Framework binaries MUST explicitly select core substrate, retained framework capabilities, optional adapters,
examples, and product extensions. A global registration function MUST NOT implicitly import product adapters,
examples, or tooling. Generated schemas and OpenAPI catalogs MUST describe only the selected framework composition.
OpenTelemetry MUST be absent from core registration and available only through explicit optional-adapter selection.
Core, graph-research, and optional-adapter composition MUST use separate Go import roots. Importing core registration
MUST NOT place an unselected capability or adapter in the binary dependency closure.

#### Scenario: core registration excludes product adapters

- **GIVEN** a fresh framework component, payload, and tool registry
- **WHEN** only core registration is invoked
- **THEN** GitHub, OGC, OASF, directory, A2A, and SLIM types are absent
- **AND** graph, rule, lifecycle, storage, generic agentic, and generic transport primitives remain present
- **AND** research payloads, research components, and `research_graph` are absent until graph research is selected

#### Scenario: an example binary opts into example types

- **GIVEN** IoT and document processors remain supported examples
- **WHEN** the production SemStreams binary and an example or E2E binary build their registries
- **THEN** only the example or E2E binary registers those factories and payloads

#### Scenario: an optional adapter is selected explicitly

- **GIVEN** core registration has completed without OpenTelemetry
- **WHEN** a binary selects the OpenTelemetry adapter composition
- **THEN** the exporter factory and schema become available to that composition
- **AND** binaries that do not select it carry no implied exporter contract

#### Scenario: core-only dependency closure is isolated

- **GIVEN** a build fixture that imports only core component, payload, and tool composition
- **WHEN** its Go dependency graph is enumerated
- **THEN** no graph-research, product-adapter, optional-adapter, example, or tooling package is present
- **AND** adding any such import to the core composition fails the contract test

### Requirement: Graph research is an atomic framework capability

SemStreams MUST retain the `research_graph` agent-facing capability, its research payloads, classifier/query/fusion
primitives, five bounded components, R0-R6 coordinated rule pack, AGENT_LOOPS/ObjectStore evidence contract,
provenance-bearing result, and `read_loop_result` retrieval path. The tool MUST be advertised only when the complete
configured execution path is available. A partial graph-research configuration MUST fail boot with an actionable
error rather than register a tool that can stall.

Repository-owned deterministic proof SHALL retain both admitted branch shapes: `synthesize_directly`, which bypasses
execute and assess, and a deterministic `walk_seeds` route that traverses `execute_subqueries`, `fusion.Fuse`,
assessment, and terminal synthesis. The two branches SHALL be asserted independently so success on one cannot mask
absence or misrouting of the other.

#### Scenario: graph research is absent by choice

- **GIVEN** a valid deployment that does not configure graph research
- **WHEN** tool registration completes
- **THEN** direct graph query tools remain available
- **AND** `research_graph` is not advertised

#### Scenario: graph research is partially configured

- **GIVEN** a deployment configures `research_graph` or one research stage without all required stages, rules, stores,
  and result retrieval
- **WHEN** bootstrap validates the selected capabilities
- **THEN** bootstrap fails before serving agent tool catalogs
- **AND** the error identifies the missing graph-research dependency

#### Scenario: graph research is complete

- **GIVEN** all graph-research components, R0-R6 rules, stores, graph dependencies, and result tools are configured
- **WHEN** bootstrap completes
- **THEN** `research_graph` is advertised
- **AND** an invocation can progress to a provenance-bearing result retrievable by the parent

#### Scenario: The direct route remains independently proven

- **GIVEN** the deterministic direct-route fixture
- **WHEN** a research invocation completes
- **THEN** the route action is `synthesize_directly`
- **AND** classifier evidence and terminal result are present
- **AND** execute and assess completion markers are absent
- **AND** the result remains retrievable by the parent

#### Scenario: The walk-seeds execute and fusion route is independently proven

- **GIVEN** the deterministic `walk_seeds` fixture with controlled graph evidence
- **WHEN** a research invocation completes
- **THEN** `execute_subqueries` invokes the production fusion path
- **AND** execute completion carries a positive evidence count and controlled evidence identity
- **AND** assessment completes and routes to terminal synthesis
- **AND** synthesis references only evidence returned by execution
- **AND** the result remains retrievable by the parent

### Requirement: Incomplete protocol behavior is never reported as success

SemStreams MUST NOT ship or advertise an integration that acknowledges transport, authentication, cancellation,
status, identity, or export behavior it does not implement. Unsupported configuration MUST be rejected before the
component reports healthy or increments success counters.

#### Scenario: an unsupported exporter protocol is configured

- **GIVEN** an OpenTelemetry protocol for which SemStreams cannot construct a working exporter
- **WHEN** configuration is validated or the component initializes
- **THEN** startup fails with an unsupported-protocol error
- **AND** no telemetry is discarded under successful export counters

#### Scenario: a protocol facade is not implemented

- **GIVEN** A2A or SLIM lacks conformant transport and lifecycle behavior
- **WHEN** the framework catalog is generated
- **THEN** the facade and its schema are absent
- **AND** callers cannot receive placeholder success responses

### Requirement: Derived-state retention follows the owning capability

SemStreams MUST own generic cleanup, bounded-store, tombstone, and ObjectStore reachability contracts. The selected
product or optional adapter composition MUST own retention for each derived record it creates. Product-derived stores
MUST NOT remain in the framework retention ledger after their producer leaves the framework composition.

#### Scenario: a product projection leaves core

- **GIVEN** an OGC, GitHub, or AGNTCY adapter creates derived state
- **WHEN** that adapter moves to its product owner
- **THEN** the product's capability contract declares cleanup and storage bounds for that state
- **AND** the framework retention contract contains only the generic mechanism and actual framework-owned stores

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

#### Scenario: Multiple requesters share one provider

- **GIVEN** three components declare compatible mutation output ports and graph-ingest declares one provider input
- **WHEN** the flow graph is validated
- **THEN** all three requesters connect to the provider
- **AND** validation creates no stream, consumer, leader, or lease

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
