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

#### Scenario: Supervisor stack owns dynamic runtime authority

- **GIVEN** ComponentManager starts its supervisor with context C as a goroutine function parameter
- **WHEN** an admitted dynamic component starts
- **THEN** its lifetime descends from C
- **AND** no request context, struct field, retained closure, or provider becomes its runtime authority

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

### Requirement: Stop signals the Start lifetime before bounding its join

`Stop(ctx context.Context)` SHALL first signal cancellation of runtime work derived from Start, then use the Stop
argument only to bound joining that work and completing terminal cleanup. Stop SHALL NOT use its argument to launch a
new runtime lifetime. Stop SHALL be idempotent and SHALL NOT repeat teardown side effects.

An exported error-returning context boundary SHALL reject nil. A canceled Stop context does not excuse failure to
signal runtime cancellation; after signaling, the implementation SHALL return the context error when bounded join or
cleanup cannot continue.

#### Scenario: Stop cancellation precedes join

- **GIVEN** an owner with running work derived from Start context C
- **WHEN** `Stop(S)` is called
- **THEN** the owner signals cancellation of work under C before waiting
- **AND** S bounds only the wait and terminal cleanup

#### Scenario: Stop waits for the exact in-flight Start

- **GIVEN** generation G has an in-flight Start call
- **WHEN** its owner begins `Stop(S)`
- **THEN** the owner cancels G and waits for G's Start call and Start finalization to return
- **AND** it invokes Stop on G only after that completion
- **AND** no Start and Stop method body overlaps for G

#### Scenario: Terminal manager Stop cancels before bounded drains

- **GIVEN** ComponentManager owns running generations and admitted callback borrows
- **WHEN** terminal `Stop(S)` receives valid S
- **THEN** it closes every borrow gate and signals every runtime cancellation before bounded waiting
- **AND** it drains admitted borrows before awaiting exact Start/finalization and invoking component Stop
- **AND** no manager or gate lock is held during callbacks, drains, joins, or component Stop

#### Scenario: Stop deadline expires

- **GIVEN** runtime work does not join before Stop context S expires
- **WHEN** `Stop(S)` waits for it
- **THEN** Stop returns an error wrapping `S.Err()`
- **AND** the runtime cancellation signal remains issued

#### Scenario: Nil context is rejected

- **WHEN** an exported error-returning Start or Stop boundary receives nil context
- **THEN** it returns a typed invalid-input error
- **AND** it does not replace nil with a background context
- **AND** it inspects no lifecycle state and performs no cancellation, wait, or cleanup action
