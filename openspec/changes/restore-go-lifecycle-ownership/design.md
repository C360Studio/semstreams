## Context

At baseline `444b7912`, public Stop contracts take `time.Duration`, shutdown invents background roots,
`ManagedComponent` exports cancellation, and Registry generations expose component handles. The atomic Stop migration
landed before beta.161. ADR-094 subsequently removed the need to repair live replacement: runtime composition is one
sealed boot generation plus restart-safe terminal shutdown.

ADR-095 and `simplify-one-shot-lifecycle-ownership` own current service-shutdown and terminal owner sequencing.
`restore-go-lifecycle-ownership` retains its completed context-bearing signature prerequisite and active
runtime-context-ownership work. It no longer claims that stopping is clean completion, concurrent Stop joins one
result, deadline expiry is rejoinable, or repeated Stop replays a retained error. It preserves context provenance,
exact Start finalization, failed-Start cleanup authority, nil rejection, and no detached roots.

## Goals

- Make the composition-root context the ancestor of runtime work.
- Make cancellation and bounded joining explicit at every lifecycle boundary.
- Keep Registry declaration-only and ComponentManager the sole runtime-handle owner.
- Make terminal access fencing and same-generation shutdown honest and race-free.
- Keep this lifecycle protocol out of reactive orchestration.

## Decisions

### D1 — Start owns lifetime; Stop bounds quiesce and joining

`Start(ctx)` receives runtime lifetime authority. An owner may derive a child and retain only private synchronized
cancellation and join state, never the context. `Stop(ctx)` validates non-nil first and uses its argument only to bound
quiesce, drain, cancellation, join, and terminal cleanup. It never launches continuing work.

For a NATS-owning component, graceful Stop first closes admission and drains already-accepted callbacks while their
work context remains live, then cancels and joins remaining Start-owned work. A component with no admission/drain may
cancel immediately. The composition root does not pre-cancel runtime on SIGTERM before owners can quiesce. Dirty
shutdown correctness comes from durable settlement and idempotent recovery, not Stop ordering.

A completed repeated Stop is a no-op. This change does not claim concurrent Stop, running-generation rejoin, or
retained terminal-result replay. Nil Stop and StopAll return a typed invalid-input error before inspecting state,
signaling cancellation, or performing any action.

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

### D4 — Registry is declaration-only and boot-admitted

Registry snapshots contain factory identity, cloned declarations, normalized facts, resources, and a local boot
generation identifier. They contain no runtime component handle, lifecycle state, readiness, or availability.
Flow graph and declaration observers consume those value facts only.

Retire Registry `Component`, `ListComponents`, deprecated `GetComponent`, handle-returning `CreateComponent` and
`ReplaceComponent`, construction-capability-returning `GetFactory`, and every snapshot component reference. Factory
construction moves behind opaque manager-authorized boot preparation. `RegisterFactory` remains registration input
and value-only `ListFactories` remains observation. No post-boot admission, replacement, or removal API survives.

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

`WithComponent` returns typed `Missing`, `Stopping`, or `Failed` errors. On success it increments an entry-local
borrow count under a private gate lock, releases every manager/gate lock, invokes `use`, and decrements the count on
return. The handle is valid only for the callback and must not be retained. Callback execution and drain waiting never
hold manager or gate locks.

A callback must not synchronously request terminal Stop for its own instance: that operation would wait on the
callback's own borrow. Closing a gate rejects new borrows and exposes a boot-generation-scoped drained signal.
Terminal Stop waits on that signal outside locks. There is no post-boot remove or replace transition.

### D6 — The boot supervisor lexically owns the Start context

`ComponentManager.Start(ctx)` passes `ctx` as the supervisor goroutine function parameter (`go supervise(ctx)`). The
goroutine stack owns it; no stored closure or context-returning provider does. The struct retains only private fence,
cancel, done, and join state. The supervisor starts only the validated boot set. No request can transfer a later
component lifetime into it.

### D7 — Terminal Stop owns the only runtime transition

Terminal Stop validates its context, closes every borrow gate, and invokes each started component's Stop while the
Start lifetime remains live enough for component-owned quiesce and accepted-work drain. It holds no manager or gate
lock while waiting or calling component code. A NATS-owning component fences intake, drains accepted callbacks, and
settles required publications before signaling its private Start cancellation. Components without admission or drain
may cancel immediately.

After component quiesce, ComponentManager signals remaining Start cancellations and joins each exact boot generation
and Start finalization. Transport connection drain follows component-owned publication barriers. Deadline failure
remains observable, runtime cancellation still precedes any ctx-driven WaitGroup wait, and no detached cleanup starts.
Dirty shutdown runs none of this protocol and relies on durable recovery from ADR-094.

## Invariants

1. Registry owns boot declarations; ComponentManager alone owns runtime handles and availability.
2. A runtime handle never escapes a scoped manager borrow callback.
3. No component/service/topology mutation changes the admitted boot generation.
4. Terminal Stop closes borrow admission and drains admitted callbacks without manager or gate locks.
5. NATS-owning components quiesce and drain accepted work before Start cancellation; simple owners may cancel first.
6. Start and Stop method bodies never overlap for one boot generation.
7. The exact started generation is the generation joined and stopped.
8. A deadline failure remains observable; it does not authorize running-generation rejoin.
9. No cleanup invents or detaches a context.
10. Dirty restart recovery depends on durable state and settlement, never shutdown hooks.

## Validation strategy

- Atomic Stop prerequisite: all local gates, `task e2e:core`, and `task e2e:semantic`.
- Declaration/runtime split and terminal shutdown: deterministic race tests plus both core and semantic E2E tiers.
- Every implementation PR requires semstreams-reviewer approval before merge.
