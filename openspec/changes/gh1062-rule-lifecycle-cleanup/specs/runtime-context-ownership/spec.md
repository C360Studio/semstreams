## ADDED Requirements

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
