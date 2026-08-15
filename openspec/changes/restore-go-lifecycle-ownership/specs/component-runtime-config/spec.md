## ADDED Requirements

### Requirement: ComponentManager provides only scoped runtime handle borrows

ComponentManager SHALL be the sole owner of runtime component handles. It SHALL expose runtime access only through a
scoped callback borrow equivalent to:

```go
func (cm *ComponentManager) WithComponent(
    ctx context.Context,
    instanceName string,
    use func(component.Discoverable) error,
) error
```

The manager SHALL return typed access errors with codes `component_missing`, `component_transitioning`, or
`component_failed`. It SHALL increment a generation-local borrow count before releasing its gate lock, invoke the
callback with no manager or gate lock held, and release the borrow when the callback returns. Retaining the handle
beyond the callback is unsupported.

A borrow callback SHALL NOT synchronously request Stop, Remove, or Replace for the same instance. It SHALL return
before an outer coordinator requests that mutation, so lifecycle control cannot wait on its own borrow.

ComponentManager `Component`, `ListComponents`, exported `ManagedComponent`, `GetManagedComponents`,
`component.Lookup`, and `Dependencies.ComponentRegistry` raw-return access SHALL be retired. Concrete sibling access
SHALL migrate to the scoped manager borrow. Observation SHALL use value DTOs only.

Removal SHALL close and drain the instance gate outside locks, cancel the generation, await exact Start completion and
finalization, invoke Stop only if Start ran, then remove runtime and declaration state.

Terminal ComponentManager Stop SHALL validate context, close every gate, signal every runtime cancellation before
bounded joins, drain admitted borrows, await exact Start/finalization, then invoke Stop for each started generation.

#### Scenario: Scoped borrow releases every lock before callback

- **GIVEN** an available runtime generation
- **WHEN** `WithComponent` admits a callback borrow
- **THEN** it records the borrow and releases manager and gate locks before invoking the callback
- **AND** it releases the borrow when the callback returns

#### Scenario: Transition rejects a new borrow with typed state

- **GIVEN** a generation whose borrow gate is closed for replacement or removal
- **WHEN** a caller invokes `WithComponent`
- **THEN** it returns typed `component_transitioning`
- **AND** it never returns the runtime handle or ambiguous nil

#### Scenario: Failed runtime rejects access

- **GIVEN** a current generation in Failed state
- **WHEN** a caller invokes `WithComponent`
- **THEN** it returns typed `component_failed` with generation and cause
- **AND** it does not return a predecessor or failed runtime handle

#### Scenario: Borrow and transition race is deterministic

- **GIVEN** a callback borrow races a transition gate close
- **WHEN** synchronization order is selected under the gate lock
- **THEN** either the borrow is counted and drained before transition proceeds
- **OR** the caller receives `component_transitioning` without entering the callback

#### Scenario: Borrow callback does not remove itself

- **GIVEN** a callback holds a borrow for instance A
- **WHEN** it needs A removed, replaced, or stopped
- **THEN** it returns without synchronously requesting that lifecycle mutation
- **AND** an outer coordinator requests the mutation after the borrow is released

#### Scenario: Remove races an active borrow deterministically

- **GIVEN** Remove for A races a callback borrow of A
- **WHEN** the gate lock orders them
- **THEN** an admitted callback drains before cancellation and same-generation Stop
- **OR** the gate closes first and the borrow receives `component_transitioning`
- **AND** no manager or gate lock is held while waiting or invoking the callback

#### Scenario: Terminal Stop races an active borrow deterministically

- **GIVEN** terminal ComponentManager Stop races a callback borrow
- **WHEN** Stop validates its context and closes all gates
- **THEN** it signals all runtime cancellations before waiting for admitted callbacks
- **AND** it drains those callbacks before exact Start joins and component Stop
- **OR** the gate wins first and the borrow receives `component_transitioning`
- **AND** no manager or gate lock is held while waiting or invoking the callback

## MODIFIED Requirements

### Requirement: Replacement publishes one atomic generation

Replacement SHALL prepare a candidate and reserve declaration resources before changing incumbent availability. The
manager SHALL then close the incumbent borrow gate and drain all admitted borrows without holding manager or gate
locks.
Before generation cancellation, an operation failure MAY reopen the gate and leave the incumbent unchanged.

After borrow drain, the manager SHALL cancel the incumbent, wait for that exact generation's in-flight Start call and
Start finalization, and only then invoke Stop on the same generation. Start and Stop method bodies SHALL NOT overlap.
If Start was never invoked, Stop SHALL NOT be called.

Cancellation is the availability point of no return: after it, the incumbent SHALL never be borrowable again. If the
operation context expires while waiting for post-cancel Start completion, the incumbent SHALL remain current in Failed
and unavailable, the candidate SHALL be discarded, and no commit, candidate Start, or detached cleanup SHALL occur.
A later caller-authorized cleanup SHALL join the generation before invoking Stop.

Successful incumbent Stop SHALL issue an opaque declaration-commit authority. Only that authority may drive an
infallible Registry commit that atomically replaces factory identity, declaration, facts, resources, and generation.
Candidate Start SHALL then derive from the ComponentManager supervisor's Start context, not the operation context.
Canceling the operation after admission SHALL NOT cancel runtime.

Stop failure SHALL release the reservation, discard the candidate, and leave the incumbent current in Failed and
unavailable. Candidate Start failure after commit SHALL leave the candidate current in Failed and unavailable. Neither
case SHALL resurrect the predecessor.

#### Scenario: Preparation failure preserves incumbent

- **GIVEN** candidate preparation or reservation fails before incumbent cancellation
- **WHEN** replacement aborts
- **THEN** the incumbent declaration and runtime availability remain unchanged
- **AND** no candidate declaration or runtime is visible

#### Scenario: Transition drains scoped borrows before cancellation

- **GIVEN** an incumbent with admitted callback borrows
- **WHEN** replacement closes its gate
- **THEN** new borrows receive `component_transitioning`
- **AND** replacement waits outside locks for admitted callbacks to return
- **AND** only then signals generation cancellation

#### Scenario: Cancellation waits for exact Start before Stop

- **GIVEN** incumbent generation G has an in-flight Start call
- **WHEN** replacement cancels G
- **THEN** it waits for G's Start call and finalization to return
- **AND** it invokes Stop on G only after that return
- **AND** Start and Stop never overlap for G

#### Scenario: Operation expires during post-cancel Start drain

- **GIVEN** G was canceled and its Start call has not completed
- **WHEN** the replacement operation context expires
- **THEN** G remains current in Failed and unavailable
- **AND** the candidate is discarded without commit, Start, or detached cleanup
- **AND** a later authorized cleanup joins G before Stop

#### Scenario: Never-started generation is not stopped

- **GIVEN** the incumbent generation never invoked Lifecycle Start
- **WHEN** replacement or cleanup retires it
- **THEN** Lifecycle Stop is not called
- **AND** structural candidate cleanup uses no detached context

#### Scenario: Successful Stop authorizes infallible declaration commit

- **GIVEN** G is unavailable, Start has finalized, and Stop succeeds
- **WHEN** the phase-typed commit authority is issued
- **THEN** Registry infallibly installs the complete candidate declaration
- **AND** no fallible validation or predecessor-resurrection branch remains

#### Scenario: Request cancellation does not own admitted runtime

- **GIVEN** a replacement request is admitted by the manager supervisor
- **WHEN** its operation context is canceled after admission
- **THEN** candidate runtime continues under the supervisor Start context
- **AND** only manager lifetime cancellation stops it

#### Scenario: Candidate Start fails after commit

- **GIVEN** the candidate declaration is committed and its runtime Start fails
- **WHEN** the manager handles the failure
- **THEN** it closes the borrow gate, cancels the exact candidate generation, and joins Start finalization
- **AND** it invokes Lifecycle Stop because Start was invoked
- **AND** it removes exact partial store claims
- **AND** the candidate remains current in Failed and unavailable
- **AND** the predecessor is not restored
