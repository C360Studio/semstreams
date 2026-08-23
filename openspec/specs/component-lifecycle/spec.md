# component-lifecycle Specification

## Purpose
TBD - created by archiving change align-standard-lifecycle-tests. Update Purpose after archive.
## Requirements
### Requirement: Component runtime lifetime and terminal authority are caller-owned

`LifecycleComponent.Start(ctx)` MUST reject nil or already-ended context before action. The accepted Start context
MUST own continuing component work and MUST NOT be retained on a production struct. `Stop(ctx)` MUST reject nil before
action and use the exact caller context only to bound the concrete owner's terminal admission-fence, cancellation,
join, and cleanup sequence. The exact resource-specific ordering MUST preserve admitted callback authority where its
native drain protocol requires it; no universal cancel-before-drain order is implied.

#### Scenario: Accepted Start context ends

- **GIVEN** a fresh component accepted Start with a cancellable nonnil context
- **WHEN** that accepted parent context is canceled
- **THEN** continuing work derived from it exits
- **AND** a separately bounded Stop makes synchronous best-effort terminal progress under its exact caller authority
- **AND** Stop may return accurate native cleanup or caller-context errors
- **AND** if the Stop bound wins, the portable contract makes no complete-join or leak-freedom claim
- **AND** no replacement authority, detached cleanup, or second-rejoin contract is created

#### Scenario: Controlled Stop while Start authority remains live

- **GIVEN** a component accepted Start and its context remains live
- **WHEN** Stop is called with a separate finite context
- **THEN** the owner drains, joins, and finalizes its terminal sequence before the Stop bound
- **AND** Stop returns nil
- **AND** resource-specific admission drain may precede cancellation of Start authority

### Requirement: Running Stop has no shared-generation contract

A successfully running component's Stop MUST be caller-bounded. Completed repeated Stop with a valid context MUST
return nil without repeating teardown. The portable contract MUST NOT promise concurrent Stop executor election,
shared results, retained-result replay, later rejoin after a Stop bound wins, concurrent Initialize, post-Stop
reinitialization, or same-instance restart. Implementations MAY reject or tolerate unsupported extra calls, but
callers and shared tests MUST NOT rely on them.

#### Scenario: Stop bound wins

- **GIVEN** terminal owner work has not joined before the Stop context ends
- **WHEN** Stop returns the caller context error
- **THEN** the call reports the failed exit honestly
- **AND** the portable contract grants no later caller authority to rejoin that running generation

#### Scenario: Completed Stop is repeated

- **GIVEN** Stop completed
- **WHEN** Stop is called again with a valid context
- **THEN** it returns nil
- **AND** it performs no teardown side effect

### Requirement: Failed Start cleanup remains owner-specific

A component that returns from Start after acquiring resources MUST synchronously attempt bounded rollback and retain
exact cleanup authority only when rollback does not complete. A later caller Stop MAY retry that retained failed-Start
cleanup. This exception MUST NOT create running-generation rejoin, result replay, a shared lifecycle wrapper, or an
exported test fault harness. Components with fallible acquisition MUST prove this behavior through owner-local
deterministic tests.

#### Scenario: Partial Start rollback expires

- **GIVEN** Start acquired an exact resource and its bounded rollback did not complete
- **WHEN** another Start is attempted
- **THEN** it is rejected while cleanup remains pending
- **AND** a later caller Stop may retry the retained exact cleanup
