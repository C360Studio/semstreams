## MODIFIED Requirements

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
