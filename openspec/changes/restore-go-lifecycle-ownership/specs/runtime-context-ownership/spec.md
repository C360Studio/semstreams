## ADDED Requirements

### Requirement: Runtime work descends from caller-owned lifetime authority

Every production runtime owner SHALL receive its lifetime through `Start(ctx context.Context)` or an equivalent
context-bearing Run boundary. Work launched by that owner SHALL descend from that context and SHALL stop when it is
canceled. Production library code SHALL NOT invent a root with `context.Background`, `context.TODO`, or
`context.WithoutCancel` for Start, Run, Watch, I/O, or continuing work.

A manager supervisor SHALL receive the Start context as its goroutine function parameter. The goroutine stack, not a
struct, retained closure, or context-returning provider, owns that value.

Process composition under `cmd/` and tests MAY create roots. Terminal cleanup SHALL use private cancellation and join
state under the caller's Stop context and SHALL NOT manufacture a detached context.

#### Scenario: Parent cancellation ends runtime work

- **GIVEN** a runtime owner started with caller context C
- **WHEN** C is canceled
- **THEN** every continuing goroutine started by that owner observes cancellation
- **AND** the owner can join those goroutines without inventing another lifetime root

#### Scenario: Supervisor stack owns boot runtime authority

- **GIVEN** ComponentManager starts its supervisor with context C as a goroutine function parameter
- **WHEN** an admitted boot component starts
- **THEN** its lifetime descends from C
- **AND** no request context, struct field, retained closure, or provider becomes its runtime authority
- **AND** no post-boot component Start is admitted

#### Scenario: Library startup cannot invent authority

- **GIVEN** production library code is starting a watcher or I/O loop
- **WHEN** no caller context is available
- **THEN** startup fails at the boundary or the API is changed to accept context
- **AND** the library does not substitute Background, TODO, WithoutCancel, or nil

#### Scenario: Terminal cleanup does not detach

- **GIVEN** an owner is joining runtime work during Stop
- **WHEN** terminal cleanup runs
- **THEN** it uses private cancellation and join state under the Stop context
- **AND** it does not create Background, TODO, WithoutCancel, or another detached context

### Requirement: Production structs do not retain context

A production struct SHALL NOT retain `context.Context`, including through embedding, aliases, wrappers, providers,
closures intended for later context recovery, or an `any` field. A lifecycle owner MAY retain only a private,
synchronized `context.CancelFunc` and the join state required by its Start/Stop contract.

Exported lifecycle records SHALL NOT expose `context.CancelFunc`, a context getter, or any equivalent cancellation
authority. Observation surfaces MAY expose state, phase, generation, health, and last error.

A managed `http.Server.BaseContext` closure MAY capture the exact Start context solely to bind accepted connections to
that server lifetime. It SHALL be installed before Serve, remain private, create no new root, expose no context getter,
and end when the server is stopped and joined. No other production closure may retain context.

#### Scenario: Start retains cancellation but not context

- **GIVEN** a lifecycle owner derives a child context from Start context C
- **WHEN** it records state needed by Stop
- **THEN** it retains only a private synchronized cancel function and join state
- **AND** neither C nor the child context is stored on a production struct

#### Scenario: Managed lifecycle observation has no authority

- **WHEN** a caller reads a managed component or service record
- **THEN** the record exposes observations but no context or cancel function
- **AND** only the owning manager can signal lifecycle cancellation

#### Scenario: Managed HTTP connections inherit the exact server lifetime

- **GIVEN** a managed HTTP server is started with context C
- **WHEN** its `BaseContext` supplies a context for an accepted connection
- **THEN** it returns C or a descendant without inventing or detaching a root
- **AND** the private closure cannot outlive the joined server lifecycle

### Requirement: Stop quiesces accepted work before canceling its Start lifetime

`Stop(ctx)` SHALL use the caller argument only to bound shutdown and SHALL NOT launch runtime authority. A NATS owner
SHALL fence admission, initiate native Drain/Shutdown, await exact native Closed while callback authority remains live,
cancel remaining Start-owned runtime, await exact done/WaitGroup, then clean up. A simple owner MAY omit native phases
but SHALL cancel ctx-driven runtime before waiting for its completion. Stop SHALL reject nil before action.

An M-class owner SHALL observe exact Start finalization before selecting running Stop or failed-Start cleanupPending.
Completed repeated Stop SHALL be a no-op. This capability SHALL NOT claim concurrent Stop, running-generation rejoin,
or retained result replay; ADR-095 and `simplify-one-shot-lifecycle-ownership` own those service-shutdown semantics.

An already-canceled Stop context cannot authorize native drain work. The owner SHALL fence admission, issue private
cancellation, and return the context cause unless completion is already observed. It SHALL NOT invent a replacement
context. Failed-Start cleanup uses only its separately approved bounded synchronous rollback root and retains authority
if that rollback fails.

#### Scenario: NATS owner drains before cancellation

- **GIVEN** an owner with admitted NATS callbacks derived from Start context C
- **WHEN** `Stop(S)` is called
- **THEN** the owner fences new intake while C remains live
- **AND** it drains and settles accepted callbacks before signaling private cancellation under C
- **AND** S bounds the phases and join without becoming work authority

#### Scenario: Simple owner may cancel immediately

- **GIVEN** an owner has no admission or accepted-work drain
- **WHEN** `Stop(S)` is called
- **THEN** it may signal private Start cancellation immediately
- **AND** S bounds its join and terminal cleanup

#### Scenario: Stop waits for the exact in-flight Start

- **GIVEN** generation G has an in-flight Start call
- **WHEN** its owner begins `Stop(S)`
- **THEN** the owner waits for G's Start call and exact Start finalization to return
- **AND** it selects the running Stop or failed-Start cleanupPending path only after that completion
- **AND** no Start and Stop method body overlaps for G

#### Scenario: Terminal manager Stop fences borrows before component drain

- **GIVEN** ComponentManager owns running generations and admitted callback borrows
- **WHEN** terminal `Stop(S)` receives valid S
- **THEN** it closes every borrow gate and drains admitted borrows before component Stop
- **AND** NATS-owning components quiesce and drain accepted work before their private cancellation
- **AND** the manager then cancels remaining runtimes and awaits exact Start/finalization
- **AND** no manager or gate lock is held during callbacks, drains, joins, or component Stop

#### Scenario: Stop deadline expires

- **GIVEN** runtime work does not join before Stop context S expires
- **WHEN** `Stop(S)` waits for it
- **THEN** Stop returns an error wrapping `S.Err()`
- **AND** admission remains fenced and any issued runtime cancellation remains issued
- **AND** it never waits on ctx-driven completion before issuing runtime cancellation

#### Scenario: Already-canceled Stop cannot invent drain authority

- **GIVEN** Stop receives a context whose cause is already set
- **WHEN** the owner has not observed completion
- **THEN** it fences admission, issues private runtime cancellation, and returns the context cause
- **AND** it does not invent a replacement context or begin native drain work

#### Scenario: Nil context is rejected

- **WHEN** an exported error-returning Start or Stop boundary receives nil context
- **THEN** it returns a typed invalid-input error
- **AND** it does not replace nil with a background context
- **AND** it inspects no lifecycle state and performs no cancellation, wait, or cleanup action
