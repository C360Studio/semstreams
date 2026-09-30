## ADDED Requirements

### Requirement: Structural validation precedes expensive test admission

The CI Test job and local check:push task SHALL require successful strict OpenSpec validation of the candidate
before admitting their existing expensive test execution. Validation SHALL use the existing OpenSpec validator;
missing tooling or a failed validator SHALL fail admission rather than silently skipping the check.
The existing additive race/integration selection and canonical integration runner SHALL remain intact.
Independent CI work MAY run in parallel where it does not depend on the failed prerequisite.

#### Scenario: Invalid candidate
- **GIVEN** a candidate rejected by strict OpenSpec validation
- **WHEN** CI or local check:push evaluates that candidate
- **THEN** expensive test execution SHALL NOT start.
- **AND** the required overall result SHALL be unsuccessful.

#### Scenario: Valid candidate
- **GIVEN** a candidate accepted by strict OpenSpec validation
- **WHEN** its other prerequisites pass
- **THEN** the existing required test selection SHALL remain admitted without weakening its assertions or ownership.

### Requirement: Required CI outcomes remain explicit

The CI aggregate SHALL depend on structural validation and the existing required jobs, execute even when a
prerequisite fails or is skipped, and report success only when every required job result is success.
Failed, cancelled, skipped, missing or unknown required results SHALL NOT be accepted as successful evidence.

#### Scenario: Required evidence unavailable
- **GIVEN** a required prerequisite or Test result that is failed, cancelled, skipped, missing or unknown
- **WHEN** CI reports the aggregate result
- **THEN** the aggregate SHALL fail rather than turning nonexecution into success.
