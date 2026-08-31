## ADDED Requirements

### Requirement: Terminal StopAll success deregisters every service; failure retains them for retry

A `Manager.StopAll` pass that aggregates no error MUST deregister every service before returning, so that a
subsequent StopAll visits nothing and returns nil. A pass that aggregates ANY genuine error MUST retain every
registration — whether that error came from a service Stop or from the manager's own teardown, and including
services whose Stop completed cleanly during that failed pass — so that a retry re-visits all of them and each
answers under its idempotent-Stop contract. Deregistration is reached by a `StopAll` pass only when that pass
aggregates nothing: a failed pass keeps the authority it needs to retry.

#### Scenario: A clean pass deregisters every service

- **GIVEN** a `Manager.StopAll` pass that completed with no aggregated error
- **WHEN** `Manager.StopAll` runs again
- **THEN** it visits no service
- **AND** it returns nil
- **AND** `Manager.GetAllServices` returns empty and `Manager.GetService` reports each former service not-found

#### Scenario: A failed pass retains every registration for retry

- **GIVEN** a `Manager.StopAll` pass that aggregated any genuine error — a service Stop failure, or a failure of
  the manager's own teardown (its `BaseService.Stop`, health publisher, runtime listeners, or startup metrics
  server), which reach the same aggregate
- **WHEN** `Manager.StopAll` runs again
- **THEN** every service registered before the failed pass is visited again
- **AND** a service whose Stop already completed during the failed pass answers as clean success

#### Scenario: A pass whose only failure is the manager's own teardown still retains

- **GIVEN** a `Manager.StopAll` pass in which every registered service stopped cleanly
- **AND** the manager's own teardown contributed a genuine error to the aggregate
- **WHEN** the pass returns
- **THEN** it returns non-nil
- **AND** every service registration is retained

## MODIFIED Requirements

### Requirement: Coordinated shutdown treats an already-stopped service as clean success

`Manager.StopAll(ctx context.Context)` MUST reject nil before action, pass the caller-owned shutdown context to every
registered service in reverse registration order, continue after every error, and aggregate every genuine Stop
failure. A service whose Stop already completed is clean success. A service merely marked stopping MUST NOT be
promoted to completed or clean unless exact owner completion was observed. The production root invokes each owner Stop
once; concurrent Stop is not a supported contract. StopAll MUST NOT invent a replacement context.

#### Scenario: Completed service is visited again

- **GIVEN** a service whose Stop completed earlier in the same StopAll pass, or during an earlier pass that
  aggregated an error and therefore retained its registration
- **WHEN** StopAll visits it
- **THEN** it answers as success — nil or `service.ErrAlreadyStopped` — without repeating teardown

#### Scenario: Stopping is not predicted completion

- **GIVEN** a service marked stopping whose exact owner completion is not observed
- **WHEN** StopAll evaluates the result
- **THEN** it does not infer clean completion from the phase label

#### Scenario: Reverse-order aggregation continues

- **GIVEN** one service returns a genuine Stop error
- **WHEN** StopAll continues the reverse-order pass
- **THEN** every remaining service receives the caller context
- **AND** the final result preserves every genuine error

#### Scenario: a service already stopped before StopAll visits it

- **GIVEN** a registered service whose exact Stop completion was observed
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` treats it as successful
- **AND** it does not infer completion merely from a stopping phase

#### Scenario: a genuine stop failure is still surfaced

- **GIVEN** a registered service whose Stop returns a genuine error
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` aggregates the error
- **AND** it still attempts every remaining service

#### Scenario: a fully clean shutdown returns nil

- **GIVEN** every registered service completes Stop cleanly or had exact completion observed
- **WHEN** `Manager.StopAll` runs
- **THEN** it returns nil

### Requirement: A framework service Stop is idempotent on repeated invocation

`Stop(ctx)` MUST reject nil before inspecting state or acting. After Stop completed, another Stop MUST return
success — nil or `service.ErrAlreadyStopped` — and MUST NOT repeat teardown. The contract MUST NOT promise concurrent
executor election, later rejoin of a successfully running generation, or replay of a prior Stop error. Stop context
bounds shutdown phases and never becomes runtime authority or a detached cleanup root.

#### Scenario: Completed Stop is called again

- **GIVEN** a framework service completed Stop, clean or failed
- **WHEN** Stop is called again with a valid context
- **THEN** it returns success — nil or `service.ErrAlreadyStopped` — and performs no teardown side effect

#### Scenario: Concurrent Stop is outside the contract

- **GIVEN** one Stop is in progress
- **WHEN** another caller attempts Stop
- **THEN** no requirement promises shared execution, shared result, or retained-result replay

#### Scenario: Stop called twice returns nil the second time

- **GIVEN** a framework service that completed Stop and uses the `BaseService.Stop` default
- **WHEN** Stop is called again with a valid context
- **THEN** the second call returns nil without repeating teardown

#### Scenario: Stop after self-transition to stopping returns nil

- **GIVEN** a service self-transitioned to stopping and exact Stop completion was subsequently observed
- **WHEN** the manager calls Stop again
- **THEN** it returns nil without replaying a prior result

#### Scenario: A repeated Stop answered with the already-stopped sentinel is success

- **GIVEN** a service that answers a post-completion Stop with `service.ErrAlreadyStopped` rather than nil
- **WHEN** `Manager.StopAll` visits it
- **THEN** `StopAll` treats the sentinel as success
- **AND** it does not aggregate the sentinel as a shutdown error
