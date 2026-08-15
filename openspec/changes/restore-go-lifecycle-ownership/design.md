## Context

At baseline `444b7912`, public Stop contracts take `time.Duration`, shutdown invents background roots,
`ManagedComponent` exports cancellation, and Registry generations expose component handles. Replacement can therefore
return an incumbent while it is stopping and can still fail after retirement.

## Goals

- Make the composition-root context the ancestor of runtime work.
- Make cancellation and bounded joining explicit at every lifecycle boundary.
- Keep Registry declaration-only and ComponentManager the sole runtime-handle owner.
- Make replacement availability and declaration commit authority honest and race-free.
- Keep this lifecycle protocol out of reactive orchestration.

## Decisions

### D1 — Start owns lifetime; Stop bounds joining

`Start(ctx)` receives runtime lifetime authority. An owner may derive a child and retain only private synchronized
cancellation and join state, never the context. `Stop(ctx)` validates non-nil first, signals the Start lifetime, and
uses its argument only to bound joining and terminal cleanup. It never launches continuing work.

A valid repeated Stop rejoins the same generation and returns nil only after clean terminal completion. A previously
observed genuine terminal failure remains observable; repetition does not erase it. Nil Stop and StopAll return a typed
invalid-input error before inspecting state, signaling cancellation, or performing any action.

### D2 — Stop signatures migrate atomically

The atomic target is:

```go
type Service interface {
    Start(context.Context) error
    Stop(context.Context) error
}

type LifecycleComponent interface {
    Initialize() error
    Start(context.Context) error
    Stop(context.Context) error
}

func (m *Manager) StopAll(context.Context) error
```

`ComponentManager` both implements Service and calls component Stop. No partial signature set can preserve a valid
non-deadlined context through a duration. There is no adapter, overload, default duration, or intermediate merge.

### D3 — No retained context or invented library root

Production structs do not retain `context.Context`, directly or indirectly. Production libraries do not create
Background, TODO, or WithoutCancel roots. Process `cmd/` composition and tests may create roots.

The only closure exception is a managed `http.Server.BaseContext` closure capturing the exact Start context for
connections accepted during that server lifetime. It is private, installed before Serve, creates no root or getter,
and ends with the joined server lifecycle.

Terminal cleanup uses private cancellation and join state under caller authority. A future exception requires a new
owner-approved inventory and design.

### D4 — Registry is declaration-only

Registry generations and snapshots contain factory identity, cloned declarations, normalized facts, resources, and a
local generation identifier. They contain no runtime component handle, lifecycle state, readiness, or availability.
Flow graph and declaration observers consume those value facts only.

Retire Registry `Component`, `ListComponents`, deprecated `GetComponent`, handle-returning `CreateComponent` and
`ReplaceComponent`, construction-capability-returning `GetFactory`, and every snapshot component reference. Factory
construction moves behind an opaque, manager-authorized prepare operation. `RegisterFactory` remains registration
input and value-only `ListFactories` remains observation. Every other exported Registry method is value-only.

### D5 — ComponentManager owns runtime access through scoped borrows

Retire ComponentManager `Component`, `ListComponents`, exported `ManagedComponent`, and `GetManagedComponents` handle
leakage. Retire `component.Lookup` and `Dependencies.ComponentRegistry` raw-return access, or replace their concrete
uses with the manager borrow. Observation returns value DTOs only.

Runtime users borrow a handle only inside a manager-scoped callback:

```go
func (cm *ComponentManager) WithComponent(
    ctx context.Context,
    instanceName string,
    use func(component.Discoverable) error,
) error
```

`WithComponent` returns typed `Missing`, `Transitioning`, or `Failed` errors. On success it increments an entry-local
borrow count under a private gate lock, releases every manager/gate lock, invokes `use`, and decrements the count on
return. The handle is valid only for the callback and must not be retained. Callback execution and drain waiting never
hold manager or gate locks.

A callback must not synchronously request Stop, Remove, or Replace for the same instance: that operation would wait on
the callback's own borrow. It returns first and asks an outer coordinator to request the lifecycle mutation.

Closing a gate rejects new borrows and exposes a generation-scoped drained signal. Replacement/removal waits on that
signal outside locks. Tests deterministically cover borrow-versus-transition races.

### D6 — The supervisor lexically owns the Start context

`ComponentManager.Start(ctx)` passes `ctx` as the supervisor goroutine function parameter (`go supervise(ctx)`). The
goroutine stack owns it; no stored closure or context-returning provider does. The struct retains only command, done,
cancel, and join state. Dynamic add/replacement requests send commands to that supervisor.

An operation context bounds preparation, admission, borrow drain, and waiting for a result. It never becomes the new
component lifetime. Candidate Start derives from the supervisor's Start context. Once admitted, cancellation of the
request context does not cancel candidate runtime; manager lifetime cancellation does.

### D7 — Replacement has two points of no return

Replacement is a local lifecycle protocol, not a rule or workflow. Its exact order is:

1. prepare candidate and reserve declaration/resources;
2. close incumbent borrow gate and drain in-flight borrows;
3. cancel the incumbent generation;
4. wait for the exact in-flight Start call and Start finalization;
5. call Stop on that same generation, only if Start was invoked;
6. receive declaration-commit authority from successful Stop;
7. infallibly commit candidate declaration;
8. install candidate runtime entry and Start it from the supervisor context.

The first point of no return is availability: once generation cancellation is signaled, the incumbent never becomes
borrowable again. The second is declaration replacement: successful Stop produces the phase-typed token authorizing
an infallible Registry commit. No fallible check remains after that token is issued.

Before cancellation, operation failure may reopen the borrow gate and preserve the incumbent. Stop failure leaves the
incumbent current, `Failed`, and unavailable; reservation releases and candidate is discarded. No predecessor is
resurrected.

If the operation context expires after cancellation while waiting for Start completion/finalization, the incumbent
becomes current `Failed` and unavailable. The candidate is discarded; nothing commits or starts, and no detached
cleanup is launched. If Start was never invoked, Lifecycle Stop is not called. A later caller-authorized cleanup first
joins the generation and then calls Stop.

Candidate Start occurs only after declaration commit. If it fails, the manager closes the candidate borrow gate,
cancels that exact generation, joins its Start completion and finalization, invokes Lifecycle Stop because Start was
invoked, and removes exact partial store claims. The candidate then remains current, `Failed`, and unavailable; the
predecessor never returns.

Removal uses the same incumbent retirement order without a candidate: close gate, drain admitted borrows outside
locks, cancel the generation, await exact Start completion/finalization, invoke Stop if Start ran, then remove runtime
and declaration state.

Terminal ComponentManager Stop has a different safety order. It validates the Stop context first, closes every borrow
gate, signals every runtime lifetime cancellation before any bounded wait, drains admitted borrows, awaits each exact
Start/finalization, and invokes Stop for each started generation. No callback or drain holds manager or gate locks.

## Invariants

1. Registry owns declarations; ComponentManager alone owns runtime handles and availability.
2. A runtime handle never escapes a scoped manager borrow callback.
3. Replacement/removal gate close and borrow drain precede cancellation; cancellation ends availability.
4. Cancellation, exact Start completion/finalization, and same-generation Stop occur in that order.
5. Start and Stop method bodies never overlap for one generation.
6. Successful Stop alone creates declaration-commit authority; commit is then infallible.
7. Request cancellation after admission cannot cancel runtime; the manager Start context can.
8. Stop failure or post-cancel drain expiry leaves the incumbent current, `Failed`, and unavailable.
9. Post-commit Start failure leaves the candidate current, `Failed`, and unavailable.
10. No cleanup invents or detaches a context.
11. Terminal manager Stop closes all gates, cancels all runtimes, then drains borrows before Start joins and Stop.
12. A borrow callback never synchronously mutates lifecycle for its own instance.

## Validation strategy

- Atomic Stop prerequisite: all local gates, `task e2e:core`, and `task e2e:semantic`.
- Declaration/runtime split and replacement: deterministic race tests plus both core and semantic E2E tiers.
- Every implementation PR requires semstreams-reviewer approval before merge.
