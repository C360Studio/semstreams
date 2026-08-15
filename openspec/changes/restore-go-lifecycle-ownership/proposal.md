## Why

SemStreams still represents graceful shutdown with a duration even though `Start` receives caller authority. Services
and components therefore invent new background roots at shutdown, managed records export cancellation functions, and
replacement stops an incumbent while Registry still advertises it as available. Those shapes obscure lifecycle
ownership in a reactive framework where cancellation and join ordering are correctness properties.

This is **BREAKING** by design. Compile failures are safer than compatibility shims that preserve detached work or
ambiguous authority.

## What Changes

- Change framework service shutdown to caller-owned context: `Service.Stop(ctx)` and `Manager.StopAll(ctx)`.
- Change component shutdown to caller-owned context: `LifecycleComponent.Stop(ctx)`.
- Land the service and component signature changes as one atomic prerequisite, with distinct test and migration
  sections inside that PR.
- Define `Start(ctx)` as the lifetime authority for runtime work. `Stop(ctx)` first signals that lifetime to cancel,
  then uses its argument only to bound join and cleanup.
- Establish a manager supervisor whose goroutine function parameter owns the Start context for every dynamic runtime.
- Remove exported `context.CancelFunc` authority and prohibit production context retention or invented roots outside
  process composition and tests.
- Make Registry declaration-only and ComponentManager the sole runtime-handle owner through scoped callback borrows.
- Replace callback-driven Registry replacement with phase-typed authority and two explicit points of no return.
- Make an incumbent unavailable before its `Stop` begins; make post-retirement commit infallible; start the candidate
  only after commit.
- If incumbent Stop fails, retain the incumbent as current in `Failed` and unavailable; do not commit or start the
  candidate and do not resurrect a canceled runtime.
- If candidate Start fails after commit, retain it as the current `Failed` generation. Never resurrect its predecessor.
- Publish a migration guide and read-only sister-repository callsite census.

## Atomic prerequisite

`ComponentManager` implements `Service` and calls `LifecycleComponent.Stop`. A partial signature migration cannot
preserve the caller's context: a valid non-deadlined context has no lossless `time.Duration` representation, while a
duration cannot carry cancellation or values. Therefore `Service.Stop`, `Manager.StopAll`, `LifecycleComponent.Stop`,
and every SemStreams implementation and caller change together in one prerequisite PR.

Component and service contract tests and migration sections remain distinct inside that atomic PR. There is no
temporary adapter, deprecated overload, or knowingly incomplete merged state.

## Non-Goals

- Adding orchestration policy, workflows, retry policy, or a general state machine framework.
- Storing `context.Context` so a no-argument Stop can find it later.
- Keeping deprecated duration-based public overloads or exported cancellation handles.
- Editing sister repositories from SemStreams.
- Combining later NATS, HTTP, store-manager, embedding, rule, fusion, or recording cleanup into this prerequisite.
- Changing wire formats, NATS subjects, persisted state, or configuration schema.

## Capabilities

### New Capabilities

- `runtime-context-ownership`: defines runtime context provenance, retention prohibitions, cancellation ownership, and
  Start/Stop join semantics.

### Modified Capabilities

- `component-runtime-config`: ComponentManager owns scoped runtime borrows and phase-typed replacement.
- `component-discovery`: Registry generations become declaration-only and all raw handle/factory reads retire.
- `service-shutdown`: service Stop and StopAll accept caller context while preserving idempotency, reverse ordering,
  and genuine error aggregation.

## Impact

- **Go API:** every `LifecycleComponent`, `Service`, direct Stop caller, and `Manager.StopAll` caller must migrate.
- **Runtime:** shutdown authority flows from composition root to services and components; no library root is invented.
- **Replacement:** manager-scoped borrows return typed missing, Transitioning, or Failed; Registry remains
  declaration-only.
- **Downstream:** compiler-directed migrations are required. Sister teams own their repositories and product proof.
- **Release:** the atomic prerequisite is breaking and requires both relevant E2E gates before it merges.
