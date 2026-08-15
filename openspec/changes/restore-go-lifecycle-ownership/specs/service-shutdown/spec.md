## MODIFIED Requirements

### Requirement: Coordinated shutdown treats an already-stopped service as clean success

`Manager.StopAll(ctx context.Context)` MUST pass caller-owned shutdown context to every registered service in reverse
registration order. A service in a clean stopped or stopping state MUST be treated as successful. A service retaining
a genuine terminal error MUST return that error from valid Stop, and StopAll MUST aggregate it. StopAll MUST continue
attempting remaining services after an error. It MUST NOT invent a background root or replace nil context.

`StopAll(nil)` MUST return a typed invalid-input error before reading service state or stopping any service.

#### Scenario: Caller authority reaches every service

- **GIVEN** registered services in a known registration order
- **WHEN** `Manager.StopAll(S)` runs
- **THEN** it calls each service's `Stop(S)` in reverse registration order
- **AND** it does not create a replacement shutdown context

#### Scenario: Nil StopAll performs no action

- **GIVEN** registered running services
- **WHEN** `Manager.StopAll(nil)` is called
- **THEN** it returns typed invalid-input
- **AND** it reads no service lifecycle state and calls no service Stop

#### Scenario: A service already stopped cleanly before StopAll visits it

- **GIVEN** a registered service already in clean stopped or stopping state
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` treats it as successful
- **AND** it does not include the terminal state in the aggregated error

#### Scenario: A terminal service retains a genuine failure

- **GIVEN** a registered terminal service retaining genuine error E
- **WHEN** `Manager.StopAll` calls its valid Stop
- **THEN** Stop returns E and StopAll aggregates E
- **AND** StopAll still attempts the remaining services

#### Scenario: A genuine stop failure is still surfaced

- **GIVEN** a registered service whose Stop returns a genuine error
- **WHEN** `Manager.StopAll` visits that service
- **THEN** `StopAll` aggregates the error
- **AND** it still attempts to stop the remaining services

#### Scenario: Shutdown context expires during the sequence

- **GIVEN** shutdown context S expires while services remain
- **WHEN** `Manager.StopAll(S)` continues the reverse-order sequence
- **THEN** each remaining service still receives S so it can signal its runtime cancellation
- **AND** `StopAll` returns errors that preserve the cancellation or deadline cause

### Requirement: A framework service Stop is idempotent on repeated invocation

A framework service's `Stop(ctx context.Context)` MUST first signal cancellation of work derived from its Start
context and use the Stop argument only to bound joining and terminal cleanup. Stop MUST be idempotent: invoking it on a
service already stopped or stopping MUST NOT repeat teardown side effects. It MUST NOT invent a background root.
`Stop(nil)` MUST return typed invalid-input before inspecting state or performing any lifecycle action.

#### Scenario: Stop called twice does not repeat teardown

- **GIVEN** a running framework service
- **WHEN** `Stop(S1)` completes cleanly and `Stop(S2)` is called
- **THEN** the second call returns nil and does not repeat teardown side effects
- **AND** it uses S2 only to bound any join still in progress

#### Scenario: Concurrent valid Stop calls share terminal completion

- **GIVEN** one valid Stop is joining generation G
- **WHEN** another valid Stop is called for G
- **THEN** both calls join the same generation-scoped completion
- **AND** both return nil if cleanup is clean or the same genuine terminal error if cleanup fails
- **AND** teardown side effects run once

#### Scenario: Repeated Stop preserves a genuine terminal failure

- **GIVEN** the generation's terminal cleanup completed with genuine error E
- **WHEN** a valid repeated Stop rejoins that generation
- **THEN** it returns E rather than nil
- **AND** it does not repeat teardown side effects

#### Scenario: Parent cancellation precedes manager Stop

- **GIVEN** a service whose Start context was canceled before the manager visits it
- **WHEN** the manager calls `Stop(S)`
- **THEN** the call treats the existing cancellation as clean lifecycle progress
- **AND** S bounds joining and cleanup without becoming a new runtime lifetime

#### Scenario: Stop receives an already-canceled context

- **GIVEN** a running service and an already-canceled Stop context S
- **WHEN** `Stop(S)` is called
- **THEN** the service still signals its Start-owned runtime cancellation
- **AND** it returns an error preserving `S.Err()` if the join cannot complete immediately

#### Scenario: Nil Stop performs no action

- **GIVEN** a running or terminal service
- **WHEN** `Stop(nil)` is called
- **THEN** it returns typed invalid-input before reading state
- **AND** it signals no cancellation and starts no wait or cleanup
