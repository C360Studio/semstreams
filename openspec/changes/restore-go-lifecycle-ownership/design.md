## Context

At baseline `444b7912`, public Stop contracts took `time.Duration`, shutdown invented background roots, and
`ManagedComponent` exported cancellation. The atomic Stop migration landed before beta.161. This design now retains
only that completed context-bearing signature prerequisite and the remaining context/root debt.

ADR-095 and `simplify-one-shot-lifecycle-ownership` own lifecycle mechanics and proof.
`require-restart-for-config-activation` owns Registry and boot-only composition.

## Goals

- Make the composition-root context the ancestor of runtime work.
- Keep runtime contexts lexical rather than retained on production structs or recovered through providers.
- Keep cancellation private and synchronized.
- Reject nil at exported error-returning context boundaries without inventing replacement authority.

## Decisions

### D1 — Start receives runtime authority; Stop uses caller-owned bounded authority

`Start(ctx)` or an equivalent context-bearing Run boundary receives runtime lifetime authority. An owner may derive a
child and retain only private synchronized cancellation and join state, never the context itself.

`Stop(ctx)` validates non-nil first and uses its caller context only to bound the terminal operation defined by
ADR-095 and `simplify-one-shot-lifecycle-ownership`. It neither stores the argument, invents a root, nor launches
continuing work. This change does not define that terminal operation's drain, finalization, failed-Start, borrow,
settlement, or restart behavior.

### D2 — Stop signatures migrated atomically

The landed atomic prerequisite is:

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
Background, TODO, or WithoutCancel roots for Start, Run, Watch, I/O, or continuing work. Process `cmd/` composition
and tests may create roots.

The only closure exception is a managed `http.Server.BaseContext` closure capturing the exact Start context for
connections accepted during that server lifetime. It is private, installed before Serve, creates no root or getter,
and ends with the joined server lifecycle.

Terminal cleanup uses private cancellation and join state under caller authority. A future exception requires a new
owner-approved inventory and design.

## Delegated lifecycle and composition

`require-restart-for-config-activation` owns Registry, value-only observation, callback-scoped runtime access, and the
boot-only composition boundary. ADR-095 and `simplify-one-shot-lifecycle-ownership` exclusively own exact Start
finalization, failed-Start cleanup, service shutdown, callback-borrow fencing, terminal ordering, native owner handles,
ACK ordering, settlement, and controlled/dirty restart proof. This change receives no completion credit from those
dependencies and defines none of their mechanisms.

## Context-only invariants

1. Runtime work descends from the context received by Start, Run, or another admitted context-bearing boundary.
2. Production structs retain no `context.Context`, hidden provider, getter, wrapper, or equivalent recovery surface.
3. Lifecycle owners may retain only private synchronized cancellation and join state.
4. Stop rejects nil before state inspection or action.
5. Stop uses the caller context only as a bound; it does not retain it, replace it, or launch runtime work from it.
6. Production libraries invent no Background, TODO, or WithoutCancel root for continuing work.
7. The private managed `http.Server.BaseContext` exception captures only the exact Start context and ends with the
   joined server lifecycle.

## Validation strategy

- P3 context-debt proof: remove the measured retained contexts and unauthorized production roots.
- Add a type-aware zero-debt guard that distinguishes process/test roots and the approved narrow exceptions from
  production library violations.
- Use focused race tests and the relevant E2E tier for each context-debt slice.
- Require semstreams-reviewer approval before integration.
