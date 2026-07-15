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

