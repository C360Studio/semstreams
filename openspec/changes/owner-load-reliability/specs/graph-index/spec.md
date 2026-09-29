## ADDED Requirements

### Requirement: Owner-load failures retain evidence and release harness work

The owner-load harness SHALL retain its original failure and capture local diagnostic evidence before initiating
harness cancellation or cleanup. Evidence SHALL identify phase, operation, applicable filter, elapsed time, caller
deadline and original error.

Seed and concurrent-phase work SHALL have lexical cancellation and completion ownership. The harness SHALL stop
further admission after observing a failure, cancel remaining work and observe owned completion within a finite
terminal budget. Expiry SHALL report unresolved ownership and prevent subsequent phases.

Consumer-baseline polling SHALL pass its finite convergence context to each Info operation and retain the last
observation and error. These requirements SHALL NOT relax the framework KV deadline, introduce listing retries,
or change owner-load workload and latency budgets.

#### Scenario: Seed failure while work is queued

- **GIVEN** seed workers are active and additional rows are queued
- **WHEN** a seed operation fails
- **THEN** the harness retains that error and stops further admission
- **AND** error publication cannot depend on draining an error channel after joining
- **AND** it observes worker completion or reports unresolved ownership.

#### Scenario: Failure during concurrent owner listing

- **GIVEN** listing, sampling and churn work are active
- **WHEN** an operation or result validation fails
- **THEN** primary failure evidence is captured before initiating cancellation
- **AND** owned work is canceled and its completion observed
- **AND** cleanup errors do not replace the primary failure.

#### Scenario: Consumer-baseline observation blocks

- **GIVEN** the harness is waiting for temporary consumers to return to baseline
- **WHEN** its convergence context ends
- **THEN** Info receives that cancellation
- **AND** failure evidence retains last count, error and attempt count.

