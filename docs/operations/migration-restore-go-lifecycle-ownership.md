# Migrate to caller-owned lifecycle contexts

The breaking SemStreams lifecycle change replaces duration-based component and service shutdown with
caller-owned contexts. The change intentionally fails compilation at adopter call sites instead of preserving detached
work through compatibility overloads.

The atomic Stop prerequisite described here is implemented and validated. Use the release notes to confirm the first
tag that contains it; the later Registry declaration/runtime split described by the active OpenSpec change remains
deferred.

## BREAKING release entry

The atomic Stop prerequisite is a clean Go source break: component and service Stop, manager StopAll and component
removal, and NATS consumer cleanup now require caller-owned contexts. Start context owns runtime lifetime; an
independent Stop context bounds native drain, join, and terminal cleanup. There is no duration adapter or detached
compatibility path. Downstream teams must compile their own current checkout and follow this migration guide; no
sister repository was edited by SemStreams.

## Public API changes

The atomic prerequisite changes the shutdown and removal signatures together:

```go
// Before
Stop(timeout time.Duration) error
manager.StopAll(timeout time.Duration) error
componentManager.RemoveComponent(instanceName string) error

// After
Stop(ctx context.Context) error
manager.StopAll(ctx context.Context) error
componentManager.RemoveComponent(ctx context.Context, instanceName string) error
```

Both `component.LifecycleComponent` and `service.Service` use `Stop(context.Context) error`. There is no duration
overload, default timeout, context-to-duration adapter, or deprecated bridge.

If an adopter does nothing, implementations no longer satisfy their interface and direct Stop calls fail compilation.
Those compiler errors are the migration list.

`RemoveComponent` is also an intentional compiler-visible break: the caller now supplies the operation context used
to bound same-generation cancellation, Start finalization, and Stop. There is no duration or background-context
fallback.

NATS consumer shutdown is part of the same breaking contract:

```go
// Before: immediate local discard with no completion result.
client.StopConsumer(streamName, consumerName)

// After: graceful native drain and authoritative completion bounded by ctx.
err := client.StopConsumer(ctx, streamName, consumerName)
```

`StopConsumer` preserves the durable consumer and drains callbacks already buffered by the native JetStream consume
context. An already-ended context starts no drain. If the context expires while a drain is in progress, the call
returns the context error and a later call rejoins that native drain. A missing or already-stopped binding is a
successful no-op. Callers that previously stored a `func()` cleanup must migrate it to `func(context.Context) error`
and pass the independent shutdown context; ordinary library cleanup must not invent a background context.
`natsclient.Subscription.Drain(ctx)` provides the equivalent graceful contract for Core NATS subscriptions.

Factories that return an existing cleanup function together with an error use that pair to report partial
acquisition. In particular, `MilestoneSubscriber.Start` may return `(stop != nil, err != nil)` after its first durable
consumer was acquired and a later consumer failed. Retain and invoke `stop(ctx)` even though Start failed; discarding
it leaks the partial acquisition. A context-expired cleanup remains rejoinable by a later call with a fresh Stop
budget, while a terminal cleanup error is replayed.

Streaming model chunk handlers now receive the request context that owns the stream:

```go
// Before
client.SetChunkHandler(func(chunk agenticmodel.StreamChunk) { /* ... */ })

// After
client.SetChunkHandler(func(ctx context.Context, chunk agenticmodel.StreamChunk) { /* ... */ })
```

Use that context for chunk-side I/O. Do not retain it on the handler or another struct, and do not replace it with a
background context.

## SemStreams cleanup authority

The implementation uses private `internal/lifecyclejoin` primitives rather than retained contexts:

- `Generation` owns one Start generation's cancel function, completion signal, and terminal Stop result;
- `Operation` serializes one context-bound native shutdown and lets a later caller rejoin after an earlier caller's
  context expires; and
- native NATS subscription drain, JetStream consume-context drain, `http.Server.Shutdown`, and fixed goroutine joins
  provide authoritative completion. Stop does not substitute a timer for those protocol results.

No context is stored by these primitives or by the migrated lifecycle fields. The Stop argument is the caller's exact
shutdown authority; it is not saved for later and does not become a new runtime lifetime.

The one new framework-owned root is `RunPartialStartRollback`: it creates a five-second context and runs rollback
synchronously when Start has acquired resources but cannot publish a usable generation to an external Stop caller.
It does not detach cleanup or authorize general library fallback to `context.Background()`.

This prerequisite does not claim repository-wide context-debt eradication. Stored contexts and invented roots in
Rule and the other phase-3 areas listed in the OpenSpec inventory remain deferred work.

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

A Start error does not prove that nothing needs cleanup. If Start acquired resources or installed a generation before
failing, it may retain private cleanup authority. The owner must later call Stop with a fresh live shutdown context;
it must not pass the canceled or expired Start context. A second Start is rejected until that cleanup reaches a
terminal result, preventing the failed generation from being overwritten or leaked.

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

The boot-only activation slice completed this migration. Registry no longer exposes live instance lookup,
replacement, reservation, or unregister operations. It retains immutable declaration values only. ComponentManager
no longer exports `Component`, `ListComponents`, `CreateComponent`, `RemoveComponent`, or `GetManagedComponents`.

Framework composition uses one private callback borrow while wiring boot-selected components. The callback runs
without manager or gate locks and the handle cannot escape that internal seam. A callback must not synchronously call
terminal `Stop`: Stop closes borrow admission and waits for accepted callbacks to return. The callback returns first;
an outer composition owner then requests shutdown.
request the lifecycle mutation.

Registry declaration observation will show only complete old or new generations. It will not report Transitioning or
Failed because declaration identity is not runtime availability.

## Deferred replacement lifecycle changes

The replacement protocol below is approved target state, not implemented by the atomic Stop prerequisite. The later
Registry/runtime slice will close and drain scoped borrows before canceling the incumbent, then wait for that exact
generation's Start completion/finalization before same-generation Stop so Start and Stop never overlap.

Removal will use the same order. Terminal ComponentManager Stop will differ: after validating context, it will close
every gate, cancel every runtime before bounded waits, drain admitted borrows, join exact Start/finalization, then
invoke each started component's Stop. Neither callbacks nor drains will run under manager or gate locks.

The target lifecycle outcomes are:

| Outcome | Current generation | Availability | Candidate |
|---|---|---|---|
| Preparation fails before cancel | Incumbent unchanged | Available | Discarded, never committed |
| Request expires after cancel | Incumbent `Failed` | Unavailable | Discarded; no detached cleanup |
| Incumbent Stop fails | Incumbent `Failed` | Unavailable | Discarded, never committed or started |
| Commit succeeds, candidate Start succeeds | Candidate started | Available | Current |
| Commit succeeds, candidate Start fails | Candidate `Failed` | Unavailable | Current; predecessor is not restored |

Cancellation will be the availability point of no return. Successful Stop will be the later declaration-commit point
of no return. A request context will bound the operation but never own admitted runtime; dynamic Start will descend
from the manager supervisor's Start context.

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

## SemStreams release gates and implementation evidence

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

The integrated atomic prerequisite passed `task lint`, `go test -race ./...`, `task test:integration`, contract tests,
schema generation with zero drift, strict OpenSpec validation, `task e2e:core` (3/3), and `task e2e:semantic` (48/48).
An independent `semstreams-reviewer` approved the post-integration implementation. The semantic run's non-gating
thematic recorder reported one degraded-floor observation; that recorder metric is preserved as evidence and is not a
runner failure.

This evidence is from the implementation worktree, not an identified release commit. The breaking entry is indexed
from `docs/README.md`; the repository's release workflow will derive the GitHub changelog from the eventual commit
subject. Exact commit/tag evidence therefore remains a release task.
