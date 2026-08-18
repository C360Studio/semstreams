# Migrate to caller-owned lifecycle contexts

This guide covers the landed atomic prerequisite that replaced duration-based component and service shutdown with
caller-owned contexts. It intentionally fails compilation at adopter call sites instead of preserving detached work
through compatibility overloads.

The later [native one-shot lifecycle migration target](migration-restart-safe-nats-client.md) is still pending until
the `simplify-one-shot-lifecycle-ownership` runtime and proof gates pass. Temporary `internal/lifecyclejoin` and
name-routed NATS lifecycle APIs are implementation debt, not migration destinations.

## BREAKING prerequisite

The prerequisite is a clean Go source break for components, services, and managers. `Start(ctx)` owns runtime
lifetime; an independent fresh Stop context bounds the terminal call. There is no duration adapter or detached
compatibility path. Downstream teams must compile their current checkout and follow the resulting errors; SemStreams
does not edit sister repositories.

## Public API changes

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

The Start context owns the running lifetime. When terminal shutdown begins, create a separate fresh bounded context at
the process composition root and pass it to `StopAll`:

```go
runCtx, stopRun := context.WithCancel(processCtx)
defer stopRun()

if err := manager.StartAll(runCtx); err != nil {
    return err
}

// After a signal or another terminal process event, keep runCtx live while
// owners perform the lifecycle ordering specified by the pending target.
shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
defer cancelShutdown()
if err := manager.StopAll(shutdownCtx); err != nil {
    return fmt.Errorf("stop services: %w", err)
}
```

`context.Background()` is allowed here because this is the process composition boundary. Library packages receive
the shutdown context from their caller and do not create a replacement root. Do not derive Stop authority from an
already-canceled run context.

The exact drain/cancel/join order is not defined by this prerequisite guide. Follow the pending native one-shot guide
when its implementation and proof land.

## Component and service migration

Change each implementation from `Stop(time.Duration)` to `Stop(context.Context)`. The context passed to Start or an
equivalent context-bearing Run boundary remains the source of runtime authority.

The context-ownership rules are:

- do not store `context.Context` on a production struct, directly or indirectly;
- retain only private synchronized cancellation and join state;
- use the Stop argument only to bound the separately specified terminal operation;
- do not launch continuing work from the Stop context;
- do not replace cancellation or deadline expiry with Background, TODO, or WithoutCancel;
- reject nil at exported error-returning context boundaries before state inspection or action; and
- pass the caller's context through `Manager.StopAll` without creating or extending a deadline.

This guide does not define exact Start finalization, failed-Start cleanup, callback-borrow fencing, drain ordering,
concurrent Stop, result replay, settlement, or restart proof. ADR-095 and `simplify-one-shot-lifecycle-ownership` own
those lifecycle facts.

## Streaming handler context

Streaming model chunk handlers receive the request context that owns the stream:

```go
// Before
client.SetChunkHandler(func(chunk agenticmodel.StreamChunk) { /* ... */ })

// After
client.SetChunkHandler(func(ctx context.Context, chunk agenticmodel.StreamChunk) { /* ... */ })
```

Use that context for chunk-side I/O. Do not retain it on the handler or another struct, and do not replace it with a
background context.

## Direct-call and test migration

Replace duration arguments with an explicit bounded context:

```go
stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := component.Stop(stopCtx); err != nil {
    t.Fatalf("Stop() error = %v", err)
}
```

Tests may create roots. Concurrent tests use channels or wait groups for synchronization; a timeout is a failure bound,
not a substitute for synchronization.

## Live composition changes are retired

Do not migrate live component removal or replacement to another lifecycle API. Persist desired component or flow state
and restart the process. Runtime composition remains the sealed boot set; only rule definitions have a dedicated live
activation capability.

Similarly, do not adopt temporary name-routed consumer cleanup, rejoin, result-replay, or lifecycle helper APIs. The
pending native target requires each owner to retain the exact native handle returned at resource birth.

## Sister repositories

SemStreams records migration surfaces but never edits sister repositories. The exact time-bounded prerequisite census
is in `openspec/changes/restore-go-lifecycle-ownership/inventory.md`. Each downstream owner compiles its current
checkout, migrates its own interfaces and calls, and runs its product's relevant unit, integration, and E2E tests.

## Data and configuration posture

The context-signature prerequisite has no wire-format, NATS subject, persisted-state, bucket, or configuration-schema
migration. It needs no storage wipe, compatibility reader, or mixed-version bridge. Mixed Go source versions do not
compile and are not a supported deployment state.

## Historical prerequisite validation

The integrated prerequisite passed:

- `task lint`;
- `go test -race ./...`;
- `task test:integration` and contract tests;
- schema generation with no materialized-spec drift;
- strict OpenSpec validation;
- `task e2e:core` (3/3);
- `task e2e:semantic` (48/48); and
- independent `semstreams-reviewer` approval.

This evidence proves the landed source prerequisite only. It does not satisfy the current runtime, controlled/dirty,
settlement, E2E, or breaking-tag gates tracked by `simplify-one-shot-lifecycle-ownership`.
