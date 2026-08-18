## MODIFIED Requirements

### Requirement: Coordinated shutdown treats an already-stopped service as clean success

`Manager.StopAll(ctx context.Context)` MUST reject nil before action, pass the caller-owned shutdown context to every
registered service in reverse registration order, continue after every error, and aggregate every genuine Stop
failure. A service whose Stop already completed is clean success. A service merely marked stopping MUST NOT be
promoted to completed or clean unless exact owner completion was observed. The production root invokes each owner Stop
once; concurrent Stop is not a supported contract. StopAll MUST NOT invent a replacement context.

#### Scenario: completed service is visited again
- **GIVEN** a service whose Stop completed
- **WHEN** StopAll visits it
- **THEN** it returns nil without repeating teardown

#### Scenario: stopping is not predicted completion
- **GIVEN** a service marked stopping whose exact owner completion is not observed
- **WHEN** StopAll evaluates the result
- **THEN** it does not infer clean completion from the phase label

#### Scenario: reverse-order aggregation continues
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

#### Scenario: completed Stop is called again
- **GIVEN** a framework service completed Stop, clean or failed
- **WHEN** Stop is called again with a valid context
- **THEN** it returns nil and performs no teardown side effect

#### Scenario: concurrent Stop is outside the contract
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

## ADDED Requirements

### Requirement: Terminal ComponentManager shutdown fences callback borrows

Terminal ComponentManager shutdown MUST fence callback-borrow admission before component shutdown. A callback admitted
before the fence MUST return and release its borrow before the component is stopped; a caller ordered after the fence
MUST receive typed `stopping` without entering the callback. The manager MUST hold no manager or gate lock while
waiting for a callback or invoking component code.

A callback MUST return before outer composition requests terminal Stop and MUST NOT synchronously stop its own
component or ComponentManager while holding the borrow.

#### Scenario: Admitted callback returns before component shutdown

- **GIVEN** a callback borrow was admitted before terminal shutdown fenced admission
- **WHEN** ComponentManager prepares to stop the borrowed component
- **THEN** it waits outside manager and gate locks for the callback to return
- **AND** it invokes component shutdown only after the callback releases the borrow

#### Scenario: New borrow receives typed stopping

- **GIVEN** terminal shutdown fenced callback-borrow admission
- **WHEN** a caller requests a new borrow
- **THEN** the caller receives typed `stopping`
- **AND** the callback is not invoked

#### Scenario: Callback returns before outer Stop and cannot self-stop

- **GIVEN** a callback holds a borrow for component A
- **WHEN** A requires terminal shutdown
- **THEN** the callback returns without synchronously stopping A or ComponentManager
- **AND** outer composition requests Stop only after the borrow is released

### Requirement: Failed Start retains cleanup authority

An owner that may acquire resources during Start MUST publish cleanup authority before acquisition can escape and MUST
expose `startDone` where manager Stop can race Start. On Start failure it MUST finalize Start, attempt bounded
synchronous rollback, and clear authority only after cleanup succeeds. If rollback fails or expires, it MUST retain
every exact handle, enter `cleanupPending`, reject another Start, and permit later manager Stop to complete cleanup.
This failed-Start path MUST NOT imply running-generation rejoin.

#### Scenario: bounded rollback cannot complete cleanup
- **GIVEN** Start acquired an exact handle and bounded rollback fails or expires
- **WHEN** another Start is attempted before manager Stop completes cleanup
- **THEN** the owner rejects Start and retains cleanup authority
