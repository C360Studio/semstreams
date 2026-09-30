## MODIFIED Requirements

### Requirement: Lexical ownership of lifecycle test fixtures

A lifecycle test fixture that acquires a component SHALL establish terminal ownership before subsequent fallible
initialization, Start or setup assertions. Returning helpers SHALL retain provisional ownership until successful
transfer to a caller that immediately establishes lexical finalization. Fatal assertion exits SHALL preserve this
ownership. Components SHALL finish controlled terminal cleanup before their owner cancels accepted Start authority
and before test-owned substrate teardown. The fixture SHALL use a fresh finite terminal context and check both
the concrete terminal result and expiry of that context. It SHALL report failures without erasing an earlier failure.

The fixture SHALL distinguish operation authority from its private Start cancellation authority when assertions must
continue after Stop. Explicit terminal phase fences SHALL be checked before the next phase. A concrete terminal
attempt SHALL prevent implicit finalizer retry, including when it returns an error. Deliberate lifecycle/API probes
SHALL retain their contract inputs and expectations rather than being reclassified as ordinary cleanup.

When a fixture deliberately invokes Start again on the same component, rejection of the later call SHALL NOT replace
or cancel the first accepted Start authority. A fixture using deliberately short-lived Start authority SHALL still
own a separately bounded terminal attempt after successful Start or a failed Start that leaves retained ownership.
Intentional cancellation or expiry SHALL remain an explicit abort expectation rather than a controlled-success claim.

These obligations SHALL NOT imply that a supplied deadline interrupts a contextless operation, that a failed Stop
joined all work, or that terminal error authorizes a second Stop. A recorded successful join SHALL require observed
completion evidence for the owned work in question. Skipped-source changes SHALL NOT be reported as executed tests.

Existing exact baseline entries remain tracked legacy debt under the admission requirements. This requirement
does not certify those unchanged fixtures as compliant or authorize new exemptions.

#### Scenario: Setup assertion before transfer
- **GIVEN** a returning helper has acquired a component and installed provisional ownership
- **WHEN** initialization, Start or a subsequent setup assertion exits before successful transfer
- **THEN** lexical cleanup SHALL make and check its owned terminal attempt before substrate teardown.
- **AND** owner cancellation SHALL follow that attempt rather than silently converting controlled cleanup to abort.

#### Scenario: Caller assertion after transfer
- **GIVEN** a helper successfully transfers a running fixture and the caller installs lexical finalization
- **WHEN** the caller returns normally or exits through a fatal assertion
- **THEN** finalization SHALL run before deferred operation cancellation and testing Cleanup callbacks.

#### Scenario: Explicit terminal phase fence
- **GIVEN** a test must stop a producer before replay or a status-key operation
- **WHEN** its explicit terminal attempt succeeds
- **THEN** the next phase MAY use the still-live operation context.
- **AND** the later finalizer SHALL NOT repeat that attempt.
- **AND** a failed explicit attempt SHALL fail the test before the next phase is admitted.

#### Scenario: Deadline supply versus completion
- **GIVEN** Stop receives a finite terminal context while owning a contextless native operation
- **WHEN** the test reports its cleanup evidence
- **THEN** it SHALL distinguish finite deadline supply from observed return and joined work.

#### Scenario: Rejected later Start preserves fixture authority
- **GIVEN** a fixture accepted Start with live owned authority
- **WHEN** the fixture deliberately invokes Start again and the component rejects that later call
- **THEN** the original Start authority SHALL remain unchanged and live for controlled cleanup.
- **AND** rejection SHALL NOT install another fixture owner or cancel the original owner's authority.

#### Scenario: Short-lived Start still has a terminal owner
- **GIVEN** a fixture uses a deliberately short-lived Start context
- **WHEN** Start succeeds or fails after retaining cleanup ownership, including after that context expires
- **THEN** the fixture SHALL make its separately bounded terminal attempt before substrate teardown.
- **AND** concrete terminal results and terminal expiry SHALL remain observable under the explicit test expectation.
- **AND** intentional Start expiry SHALL NOT be described as successful controlled shutdown or complete joining.
