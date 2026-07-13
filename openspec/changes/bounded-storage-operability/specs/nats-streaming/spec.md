## ADDED Requirements

### Requirement: Managed firehose streams have finite retention bounds

Every production stream carrying time-shaped events MUST declare finite `MaxAge` and `MaxBytes` limits plus
an intentional discard policy. SemStreams MUST reject production readiness for an unbounded managed
firehose stream unless a time-limited migration override identifies the resource and owner.

#### Scenario: Production stream is unbounded

- **GIVEN** a managed event stream has zero `MaxAge` or zero `MaxBytes`
- **WHEN** production configuration is validated
- **THEN** readiness fails with the stream name, owning component, missing bound, and migration action

### Requirement: Existing stream retention is reconciled

SemStreams MUST inspect existing stream configuration instead of treating create-or-open as sufficient.
Editable retention and capacity drift MUST be reconciled to the declaration. Incompatible changes MUST fail
readiness with an exact diagnostic rather than silently accepting the old resource.

#### Scenario: Existing stream has stale limits

- **GIVEN** an existing stream's `MaxAge`, `MaxBytes`, or discard policy differs from the declaration
- **WHEN** the owning component starts or its configuration changes
- **THEN** SemStreams updates safe editable fields
- **OR** it fails readiness with the observed and required configurations
