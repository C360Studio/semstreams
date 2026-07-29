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

### Requirement: Component start failures fail boot closed and surface in health

A lifecycle component whose `Start` returns an error MUST fail composition-root boot when the
failure occurs during `Manager.StartAll`, and MUST be reported unhealthy — never silently
absorbed — when the failure occurs after boot. `ComponentManager.Start` is a
**component-start barrier**: it launches component `Start` calls concurrently but returns only
after every launched `Start` has returned, and returns the joined errors of all that failed.
There is no fire-and-forget component launch at boot, and no compatibility variant that
preserves one. A component-level fail-closed assertion (e.g. the bucket acquisition seam
refusing an unreconcilable retention divergence inside an owner's `Start`) is thereby a
process-level refusal: the process MUST NOT bring up its HTTP surface or report service health
while boot-time component starts are outstanding or failed. The barrier exists for fail-closed
boot; retention coverage is held by the bucket acquisition seam at each acquisition, not by
boot ordering.

Post-boot component starts (dynamic configuration add or restart) MUST NOT crash the process;
they record the component as failed with its error, and the component manager's health check
MUST report a failed component by name with its last error until it recovers. Health MUST NOT
ignore the failed state.

Configuration changes that become locally visible during boot join the **boot transaction**:
after the component-start barrier and before returning, `ComponentManager.Start` synchronously
drains pending configuration state against the LIVE local configuration (so a dropped
bounded-buffer notification cannot lose a change) — new components are created and started
under the same barrier semantics (their failures join the boot failure), edits to existing
components are applied, removals are honored, and model-registry dependents are rebuilt when
the live registry's content differs from what they were built against. A component whose
CREATE (not `Start`) fails during boot-boundary reconciliation is logged and excluded from the
boot set — matching Initialize's existing best-effort creation posture — while `Start`
failures remain fail-closed; a rebuild failure applying an edit fails boot (the old instance
is already stopped). The drain loops until quiescent (a pass that consumes no pending events
and applies no change), bounded by the lifecycle context: cancellation fails boot with the
context error. The **cutoff** is the final drain pass: updates whose local application lands
after it — component ADDS and EDITS alike — are post-boot dynamic changes, microsecond-class
identical to ones arriving just after `Start` returns, handled by the config watcher with the
dynamic paths. Post-cutoff bucket acquisition is CLOSED by the acquisition seam: a dynamic
add or edit that re-acquires framework buckets reconciles them to their declared catalog
policy at that acquisition — the class formerly named here as a forward reference is
discharged, and no boot sweep is involved. A registry change landing between the final drain
pass and the watcher starting MUST still be applied, not discarded: the watcher's entry
backlog check applies the pending event when the registry content differs from the
last-applied baseline.

#### Scenario: a configuration update arriving during boot joins the boot transaction

- **GIVEN** a configuration change — a new component, an edit to an existing component, or a
  model-registry change — that becomes locally visible while boot-time component starts are
  still in flight
- **WHEN** `ComponentManager.Start` completes its cold-boot barrier
- **THEN** it synchronously applies the pending configuration state before returning: created
  components are started (or fail boot) under barrier semantics, edits are applied to their
  components, and model-registry dependents are rebuilt against the new registry — so
  post-start boot steps (the legacy-drift backstop having already run pre-start, and HTTP
  setup) observe them before the HTTP surface comes up
- **AND** an update whose local application lands after the final drain pass — a component
  ADD or EDIT alike — is a post-boot dynamic change, processed by the config watcher with
  `started == true`

#### Scenario: a post-boot acquisition reconciles bucket policy at the seam

- **GIVEN** a fully booted process and a framework bucket dirtied out-of-band (a foreign
  `MaxAge` applied to its backing stream after boot)
- **WHEN** a post-boot dynamic configuration edit restarts the owning component, whose `Start`
  re-acquires the bucket through the ensure seam
- **THEN** the divergence is reconciled to the declared catalog policy (stripped, with a WARN
  naming the bucket) at that acquisition — with no boot sweep involved — proving the seam
  closes the post-cutoff class the boot transaction defers

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

#### Scenario: a post-boot start failure is visible in health

- **GIVEN** a running process in which a dynamically added or restarted component's `Start`
  returns an error
- **WHEN** the component manager's health check runs
- **THEN** the health check returns an error naming the failed component and its last error,
  and the process's service health reflects the failure until the component recovers

#### Scenario: no unconsumed error hook survives

- **GIVEN** the component error hook registration surface
- **WHEN** no production caller consumes it after boot-time propagation lands
- **THEN** the hook is deleted rather than left as a dead exported surface (a signal read by
  nothing is not enforcement)
