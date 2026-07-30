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

`Manager.StopAll` MUST treat a service that is already in a stopped or stopping
terminal state as a successful stop, not aggregate it as a fatal error. During
coordinated shutdown a service may observe parent-context cancellation and
transition itself to stopped/stopping before `StopAll` reaches its explicit `Stop`
call; in that ordering "already stopped" is the intended terminal state. `StopAll`
MUST continue to stop every registered service in reverse registration order and
MUST still surface genuine stop failures, but MUST return `nil` when the only
non-success outcomes are services that were already stopped or stopping.

#### Scenario: a service already stopped before StopAll visits it

- **GIVEN** a registered service that has already reached stopped/stopping via parent-context cancellation
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` treats it as a successful stop
- **AND** does not include it in the aggregated stop error

#### Scenario: a genuine stop failure is still surfaced

- **GIVEN** a registered service whose `Stop` returns a real (non-already-stopped) error
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` aggregates that error and returns non-nil
- **AND** still attempts to stop the remaining services

#### Scenario: a fully clean shutdown returns nil

- **GIVEN** a set of registered services that all stop cleanly or are already stopped
- **WHEN** `Manager.StopAll` runs
- **THEN** it returns `nil`

### Requirement: A framework service Stop is idempotent on repeated invocation

A framework service's `Stop` MUST be idempotent: invoking `Stop` on a service that
is already stopped or stopping MUST return `nil` and MUST NOT re-run teardown side
effects (closing an already-closed channel, double-releasing resources). This is
the per-service half of the coordinated-shutdown contract that lets `StopAll`
treat already-stopped services as clean.

#### Scenario: Stop called twice returns nil the second time

- **GIVEN** a running framework service
- **WHEN** `Stop` is called and completes, then `Stop` is called again
- **THEN** the second call returns `nil`
- **AND** no teardown side effect is run a second time

#### Scenario: Stop after self-transition to stopping returns nil

- **GIVEN** a service that transitioned itself to stopping on parent-context cancellation
- **WHEN** the manager subsequently calls `Stop`
- **THEN** the call returns `nil`

