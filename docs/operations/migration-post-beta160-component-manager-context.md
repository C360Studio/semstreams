# Migrate ComponentManager context access after beta.160

The release after `v1.0.0-beta.160` removes `component.ManagedComponent.Context` and adds an operation context to the
embedding worker callbacks described below. These are **BREAKING Go source changes** for callers using either API.

`ComponentManager` now creates each child context and passes it directly to
`LifecycleComponent.Start(ctx)`. It retains only the cancellation function needed to stop that component.
Value-only component status APIs expose lifecycle observations, not handles,
contexts, or cancellation authority. `GetManagedComponents` is removed.

## Required adopter changes

- Delete reads, copies, comparisons, and health checks against `ManagedComponent.Context`.
- Use the `ctx` supplied to `LifecycleComponent.Start(ctx)` inside the component's running work.
- Use ComponentManager's value-only status and health responses for lifecycle observation.
- Do not replace the removed field with another stored context or a context getter. The framework owns cancellation.

If downstream code does not directly reference `ManagedComponent.Context`, no code change is required. A direct use
fails compilation and identifies the exact call site to migrate.

## Embedding worker callback context

`embedding.GeneratedCallback` and `embedding.TerminalCallback` now receive `context.Context` as their first argument:

```go
worker.WithOnGenerated(func(ctx context.Context, entityID string, vector []float32) {
    // Use ctx for any callback-scoped I/O.
})

worker.WithOnTerminal(func(
    ctx context.Context,
    entityID string,
    sourceRevision uint64,
    outcome embedding.TerminalOutcome,
    reason string,
) {
    // Use ctx for any callback-scoped I/O.
})
```

The supplied value is the exact worker-generation context derived by `Worker.Start`. It is cancelled when the parent
context is cancelled or `Worker.Stop` begins. Adopters must add the first parameter even when the callback does not
perform I/O; `_ context.Context` is appropriate in that case. Do not capture a separate background context or store
the callback context on another struct.

There is no configuration, wire-format, NATS state, or stored-data migration for this change. It does not require a
storage wipe, compatibility reader, or mixed-version bridge.

SemStreams agents may inventory affected sister-repository call sites, but sister repositories remain read-only.
Each downstream owner applies and validates its migration in that repository.
