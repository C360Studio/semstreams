# service-shutdown Specification

## Purpose

`service-shutdown` governs **idempotent stop**. Coordinated shutdown treats an already-stopped
service as clean success rather than an error, and a framework service's `Stop` is idempotent on
repeated invocation. The point is that shutdown paths are frequently re-entered — a shutdown racing
a supervisor, a second signal, a cleanup running after a failed start — and each of those must be a
no-op, not a failure that masks the real cause or a double-teardown that panics.

**What it does NOT cover.** Component lifecycle state and configuration-driven restart belong to
`component-runtime-config`. The Lifecycle harness for named workflow instances (ADR-047) is a
different concept entirely despite the shared word. This capability is narrowly about stopping a
running framework service safely, more than once.
## Requirements
### Requirement: Coordinated shutdown treats an already-stopped service as clean success

`Manager.StopAll(ctx context.Context)` MUST reject nil before action, pass the caller-owned shutdown context to every
registered service in reverse registration order, continue after every error, and aggregate every genuine Stop
failure. A service whose Stop already completed is clean success. A service merely marked stopping MUST NOT be
promoted to completed or clean unless exact owner completion was observed. The production root invokes each owner Stop
once; concurrent Stop is not a supported contract. StopAll MUST NOT invent a replacement context.

#### Scenario: Completed service is visited again

- **GIVEN** a service whose Stop completed
- **WHEN** StopAll visits it
- **THEN** it returns nil without repeating teardown

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

`Stop(ctx)` MUST reject nil before inspecting state or acting. After Stop completed, another Stop MUST return nil and
MUST NOT repeat teardown. The contract MUST NOT promise concurrent executor election, later rejoin of a successfully
running generation, or replay of a prior Stop error. Stop context bounds shutdown phases and never becomes runtime
authority or a detached cleanup root.

#### Scenario: Completed Stop is called again

- **GIVEN** a framework service completed Stop, clean or failed
- **WHEN** Stop is called again with a valid context
- **THEN** it returns nil and performs no teardown side effect

#### Scenario: Concurrent Stop is outside the contract

- **GIVEN** one Stop is in progress
- **WHEN** another caller attempts Stop
- **THEN** no requirement promises shared execution, shared result, or retained-result replay

#### Scenario: Stop called twice returns nil the second time

- **GIVEN** a framework service completed Stop
- **WHEN** Stop is called again with a valid context
- **THEN** the second call returns nil without repeating teardown

#### Scenario: Stop after self-transition to stopping returns nil

- **GIVEN** a service self-transitioned to stopping and exact Stop completion was subsequently observed
- **WHEN** the manager calls Stop again
- **THEN** it returns nil without replaying a prior result

### Requirement: Terminal ComponentManager shutdown fences callback borrows

ComponentManager MUST close callback-borrow admission before stopping child components. A callback admitted before the
fence MUST return before child Stop begins; a callback ordered after the fence MUST receive typed `stopping` without
being invoked. Waiting and component callbacks MUST run without the manager or borrow mutex held.

#### Scenario: Admitted callback completes before child Stop

- **GIVEN** a callback borrow admitted before terminal shutdown
- **WHEN** ComponentManager begins cleanup
- **THEN** it fences new admission and waits outside its locks
- **AND** child Stop begins only after the admitted callback releases its borrow

#### Scenario: New callback is rejected after the fence

- **GIVEN** terminal shutdown has fenced callback admission
- **WHEN** another callback is requested
- **THEN** it receives typed `stopping`
- **AND** the callback is not invoked

### Requirement: ComponentManager failed Start retains cleanup authority

ComponentManager MUST publish cancellation and `startDone` authority before child acquisition can escape. If Start
fails, it MUST finalize Start and attempt bounded synchronous rollback. Successful rollback clears lifecycle handles.
Failed or expired rollback retains cleanup authority, rejects another Start on the same instance, and permits a later
Stop with caller context to retry cleanup.

#### Scenario: Failed rollback is retried by Stop

- **GIVEN** ComponentManager Start acquired a child and rollback returned an error
- **WHEN** another Start is attempted
- **THEN** it is rejected while cleanup remains pending
- **AND** a later Stop may complete cleanup and make repeated Stop a no-op

