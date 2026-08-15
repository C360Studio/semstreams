# Migrate ComponentManager context access after beta.160

The release after `v1.0.0-beta.160` removes `component.ManagedComponent.Context`.
This is a **BREAKING Go source change** for callers that directly inspected that field.

`ComponentManager` now creates each child context and passes it directly to
`LifecycleComponent.Start(ctx)`. It retains only the cancellation function needed to stop that component.
`GetManagedComponents` snapshots expose lifecycle observations, not context or cancellation authority.

## Required adopter changes

- Delete reads, copies, comparisons, and health checks against `ManagedComponent.Context`.
- Use the `ctx` supplied to `LifecycleComponent.Start(ctx)` inside the component's running work.
- Use `ManagedComponent.State`, `ManagedComponent.LastError`, and component `Health()` for lifecycle observation.
- Do not replace the removed field with another stored context or a context getter. The framework owns cancellation.

If downstream code does not directly reference `ManagedComponent.Context`, no code change is required. A direct use
fails compilation and identifies the exact call site to migrate.

There is no configuration, wire-format, NATS state, or stored-data migration for this change. It does not require a
storage wipe, compatibility reader, or mixed-version bridge.

SemStreams agents may inventory affected sister-repository call sites, but sister repositories remain read-only.
Each downstream owner applies and validates its migration in that repository.
