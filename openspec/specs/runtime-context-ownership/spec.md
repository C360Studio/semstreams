# runtime-context-ownership Specification

## Purpose
TBD - created by archiving change restore-go-lifecycle-ownership. Update Purpose after archive.
## Requirements
### Requirement: Managed lifecycle boundaries carry caller context

`service.Service.Stop`, `component.LifecycleComponent.Stop`, and `service.Manager.StopAll` MUST accept
`context.Context`. No duration overload, context-to-duration adapter, default timeout, or deprecated compatibility
path may remain.

`Manager.StopAll` MUST reject nil before action, pass the exact caller context to every registered service in reverse
registration order, continue after individual failures, and aggregate genuine errors.

#### Scenario: Duration-era adopter compiles against the current interface

- **WHEN** an implementation or caller still supplies a duration to a lifecycle Stop boundary
- **THEN** compilation fails at that boundary
- **AND** no compatibility overload silently invents caller authority

#### Scenario: StopAll preserves caller authority

- **GIVEN** a nonnil caller context
- **WHEN** `Manager.StopAll` stops registered services
- **THEN** each service receives that exact context in reverse registration order
- **AND** genuine failures are aggregated after every service is attempted

### Requirement: Production structs do not retain context authority

A production struct MUST NOT retain `context.Context`, an alias or wrapper containing it, or a provider that returns
it. Lifecycle owners MAY retain private synchronized cancellation and join state.

An exported production field or method MUST NOT expose `context.CancelFunc` or an equivalent cancellation provider.
Managed lifecycle records expose observation, not cancellation authority.

#### Scenario: Production type retains hidden context authority

- **WHEN** the production package graph is type-checked
- **THEN** direct, aliased, wrapped, embedded, collection-held, or provider-returned context storage fails the contract
- **AND** context-taking operation callbacks remain valid

#### Scenario: Managed component observation exposes no cancel

- **WHEN** a caller reads a `ManagedComponent`
- **THEN** it receives component, state, configuration, order, and error observations
- **AND** it receives no context or cancel function

### Requirement: Core lifecycle boundaries reject nil before action

Core component and service lifecycle entry points that can return an error MUST reject nil context before inspecting
or mutating lifecycle state. Completed repeated Stop with a nonnil context remains a no-op success and does not repeat
teardown.

#### Scenario: Nil Stop preserves state

- **GIVEN** a managed owner with existing lifecycle state
- **WHEN** Stop receives nil
- **THEN** it returns typed invalid input
- **AND** performs no cancellation, wait, drain, cleanup, or state transition

### Requirement: Lifecycle composition distinguishes controlled shutdown from abort cancellation

A composition boundary performing controlled shutdown MUST call a component's bounded Stop while the accepted Start
parent remains live, then cancel that Start parent after Stop returns. If the accepted Start parent ends unexpectedly,
the boundary MUST still call separately bounded Stop for synchronous best-effort terminal cleanup of existing
ownership and MUST accept an accurate nonnil terminal result.

Abort cleanup MUST NOT invent replacement work authority, retain or recover the ended Start context, or default the
Stop context. The exact Stop context bounds terminal observation and finalization only. Abort cleanup MUST NOT detach
or create a second-rejoin contract, and when the bound wins it makes no complete-join or leak-freedom claim.

#### Scenario: Composition performs controlled shutdown

- **GIVEN** an accepted Start parent remains live
- **WHEN** composition begins controlled shutdown
- **THEN** it calls separately bounded Stop before canceling the Start parent

#### Scenario: Composition observes unexpected Start-parent cancellation

- **GIVEN** an accepted Start parent ended before controlled shutdown
- **WHEN** composition finalizes the component
- **THEN** it calls Stop with a separate finite context
- **AND** Stop synchronously makes best-effort terminal progress for only the existing owner
- **AND** accurate native cleanup or caller-context errors remain observable
- **AND** no replacement work authority is invented
