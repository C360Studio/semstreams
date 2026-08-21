# Change: Restore Go lifecycle context ownership

## Why

SemStreams lifecycle Stop boundaries previously accepted durations and could not preserve caller cancellation,
deadlines, or values. The landed breaking migration changed component and service lifecycle boundaries to
`context.Context` and removed retained context and exported cancellation authority from production lifecycle records.

## What Changes

- `Service.Stop`, `Manager.StopAll`, and `LifecycleComponent.Stop` accept `context.Context`.
- No duration overload, adapter, or deprecated compatibility path remains.
- Managed lifecycle owners retain private cancellation and join state, not `context.Context`.
- `ManagedComponent` exposes observation only, not cancellation authority.
- Core lifecycle boundaries reject nil before state changes or teardown.
- A type-aware contract test rejects retained production contexts, context providers, and exported cancellation
  authority.

## Removed From Completion Scope

- Repository-wide removal of every production `Background`, `TODO`, `WithoutCancel`, or equivalent root.
- Drain ordering, failed-Start cleanup, callback borrowing, Client shutdown, settlement, restart proof, Registry, and
  boot composition.
- Any release or next-tag claim beyond the already-landed context-bearing signature migration.

## Capability

- `runtime-context-ownership`

## Impact

This is a compiler-visible Go API break. Downstream owners pass an explicit context to lifecycle Stop operations and
own their repository migrations. No wire format, NATS subject, persisted state, or configuration schema changed.
