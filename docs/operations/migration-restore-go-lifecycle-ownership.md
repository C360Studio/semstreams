# Migrate to caller-owned lifecycle contexts

The next breaking SemStreams lifecycle release replaces duration-based component and service shutdown with
caller-owned contexts. The change intentionally fails compilation at adopter call sites instead of preserving detached
work through compatibility overloads.

This guide describes a planned breaking contract. Use the release notes to confirm the first tag that contains it.

## Public API changes

The atomic prerequisite changes all three signatures together:

```go
// Before
Stop(timeout time.Duration) error
manager.StopAll(timeout time.Duration) error

// After
Stop(ctx context.Context) error
manager.StopAll(ctx context.Context) error
```

Both `component.LifecycleComponent` and `service.Service` use `Stop(context.Context) error`. There is no duration
overload, default timeout, context-to-duration adapter, or deprecated bridge.

If an adopter does nothing, implementations no longer satisfy their interface and direct Stop calls fail compilation.
Those compiler errors are the migration list.

## Composition-root migration

The Start context owns the runtime lifetime. At process shutdown, cancel that lifetime, create a separately bounded
shutdown context at the process composition root, and pass it to `StopAll`:

```go
runCtx, stopRun := context.WithCancel(processCtx)
if err := manager.StartAll(runCtx); err != nil {
    return err
}

// After a signal or another terminal process event:
stopRun()
shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
defer cancelShutdown()
if err := manager.StopAll(shutdownCtx); err != nil {
    return fmt.Errorf("stop services: %w", err)
}
```

`context.Background()` is allowed here only because this code is the process root. Library packages must receive the
shutdown context from their caller and must not create a replacement root.

Do not derive the Stop context from an already-canceled run context. The run cancellation signals continuing work;
the bounded Stop context supplies the remaining authority to join and perform terminal cleanup.

## Component implementation migration

Change the method signature and make runtime cancellation private:

```go
type Component struct {
    lifecycleMu sync.Mutex
    generation  *runGeneration
    terminalErr error
}

type runGeneration struct {
    cancel context.CancelFunc
    done   <-chan struct{}
    err    error
}

func (c *Component) Start(ctx context.Context) error {
    if ctx == nil {
        return errs.ErrInvalidConfig
    }

    c.lifecycleMu.Lock()
    if c.generation != nil {
        c.lifecycleMu.Unlock()
        return errs.ErrAlreadyStarted
    }
    runCtx, cancel := context.WithCancel(ctx)
    done := make(chan struct{})
    generation := &runGeneration{cancel: cancel, done: done}
    c.generation = generation
    c.terminalErr = nil
    c.lifecycleMu.Unlock()

    go func() {
        defer close(done)
        generation.err = c.run(runCtx)
    }()
    return nil
}

func (c *Component) Stop(ctx context.Context) error {
    if ctx == nil {
        return errs.ErrInvalidConfig
    }

    c.lifecycleMu.Lock()
    generation := c.generation
    if generation == nil {
        err := c.terminalErr
        c.lifecycleMu.Unlock()
        return err
    }
    generation.cancel()
    c.lifecycleMu.Unlock()

    select {
    case <-generation.done:
        c.lifecycleMu.Lock()
        if c.generation == generation {
            c.generation = nil
            c.terminalErr = generation.err
        }
        c.lifecycleMu.Unlock()
        return generation.err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

The exact synchronization may differ, but the contract does not:

- Do not store `context.Context` on the component.
- Store only a private, synchronized cancel function and join state.
- Stop signals runtime cancellation before waiting.
- The Stop context bounds only join and terminal cleanup.
- Do not launch continuing work from the Stop context.
- Reject nil at exported error-returning context boundaries.
- Use one generation-scoped done signal; do not create one waiter goroutine per Stop.
- Keep repeated Stop calls idempotent and preserve genuine terminal errors.
- Reject Start while a generation remains active; do not overwrite or leak it.
- Clear the active generation only after terminal join, then permit the suite's Start-after-Stop lifecycle.

Do not replace the removed `component.ManagedComponent.Cancel` field with a context getter, a public cancel function,
or an adopter-owned side channel. ComponentManager owns per-instance cancellation.

## Service implementation migration

Change every `service.Service` implementation from `Stop(time.Duration)` to `Stop(context.Context)`. Apply the same
cancellation-before-join rules as components. A clean stopped/stopping service returns nil. A service retaining a
genuine terminal error returns that error from every valid Stop so StopAll can aggregate it.

`Manager.StopAll(ctx)` still:

- visits services in reverse registration order;
- attempts later services after one Stop error;
- treats only clean stopped or stopping state as clean lifecycle progress; and
- returns joined genuine errors.

It passes the caller's context through. It does not create a new timeout or extend the caller's deadline.

## Direct call and test migration

Replace duration arguments with an explicit context:

```go
stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := component.Stop(stopCtx); err != nil {
    t.Fatalf("Stop() error = %v", err)
}
```

Tests are allowed to create roots. Concurrent tests should use channels or wait groups for synchronization; a timeout
is only a failure bound, never a substitute for synchronization.

## Registry and runtime access migration

Registry becomes declaration-only. Its generations, snapshots, observers, and flow graph contain no runtime component
handles. Retire calls to Registry `Component`, `ListComponents`, deprecated `GetComponent`, handle-returning
`CreateComponent` and `ReplaceComponent`, and construction-capability-returning `GetFactory`.

ComponentManager also retires `Component`, `ListComponents`, exported `ManagedComponent`, and
`GetManagedComponents` handle leakage. `component.Lookup` and `Dependencies.ComponentRegistry` no longer provide raw
sibling handles. Use value DTOs for observation and the manager's scoped `WithComponent` callback for runtime access:

```go
err := componentManager.WithComponent(ctx, "graph-query", func(comp component.Discoverable) error {
    query, ok := comp.(*graphquery.Component)
    if !ok {
        return fmt.Errorf("unexpected graph-query component %T", comp)
    }
    return query.Refresh(ctx)
})
```

The handle is valid only inside the callback and must not be retained. The callback runs without manager or gate locks.
Handle access returns typed missing, Transitioning, or Failed errors; it never returns ambiguous nil.

Do not synchronously Stop, Remove, or Replace the same instance from inside its borrow callback. That would wait on the
callback's own borrow. Return from the callback, then ask an outer coordinator to request the lifecycle mutation.

Registry declaration observation continues to show only complete old or new generations. It never reports
Transitioning or Failed because declaration identity is not runtime availability.

## Replacement lifecycle changes

Replacement closes and drains scoped borrows before canceling the incumbent. It then waits for that exact generation's
Start completion/finalization before same-generation Stop. Start and Stop never overlap.

Removal uses the same order. Terminal ComponentManager Stop differs: after validating context, it closes every gate,
cancels every runtime before bounded waits, drains admitted borrows, joins exact Start/finalization, then invokes each
started component's Stop. Neither callbacks nor drains run under manager or gate locks.

The lifecycle outcomes are:

| Outcome | Current generation | Availability | Candidate |
|---|---|---|---|
| Preparation fails before cancel | Incumbent unchanged | Available | Discarded, never committed |
| Request expires after cancel | Incumbent `Failed` | Unavailable | Discarded; no detached cleanup |
| Incumbent Stop fails | Incumbent `Failed` | Unavailable | Discarded, never committed or started |
| Commit succeeds, candidate Start succeeds | Candidate started | Available | Current |
| Commit succeeds, candidate Start fails | Candidate `Failed` | Unavailable | Current; predecessor is not restored |

Cancellation is the availability point of no return. Successful Stop is the later declaration-commit point of no
return. A request context bounds the operation but never owns admitted runtime; dynamic Start descends from the manager
supervisor's Start context.

## Sister repositories

SemStreams records migration surfaces but never edits sister repositories. At the design baseline, production
implementations or StopAll callers exist in semboids, semconnect, semdev, semdragon, semmem, semops, semsage,
semsource, semspec, and semteams. The exact baseline census is in
`openspec/changes/restore-go-lifecycle-ownership/inventory.md`.

No production lifecycle implementation or StopAll caller was found in semembed, seminstruct, semlink, semmachina, or
semsummarize. semlink still has a direct component Stop call at `internal/semstreams/runtime.go:147`. semboids,
semmachina, and semdragon contain direct Registry runtime reads that must migrate to scoped manager borrows.

Each downstream owner must compile its current checkout, migrate its own interfaces and calls, and run the product's
relevant unit, integration, and E2E tests. The inventory is a notice, not a substitute for compiler evidence.

## Data and configuration posture

This change has no wire-format, NATS subject, persisted-state, bucket, or configuration-schema migration. It needs no
storage wipe, compatibility reader, or mixed-version bridge. Mixed Go source versions do not compile and are not a
supported deployment state.

## SemStreams release gates

Before the atomic Stop prerequisite merges:

- `task lint`
- `go test -race ./...`
- integration and contract tests
- `task schema:generate` with no unexplained schema or materialized-spec drift
- `task e2e:core`
- `task e2e:semantic`
- semstreams-reviewer approval

The semantic tier is mandatory because a lifecycle/interface migration can leave one framework binary or component
registration path half-migrated even when package tests pass.
