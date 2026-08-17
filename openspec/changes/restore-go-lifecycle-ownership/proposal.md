## Why

SemStreams originally represented graceful shutdown with a duration even though `Start` receives caller authority.
The atomic Stop prerequisite corrected that signature and private cancellation ownership. Remaining work removes raw
runtime handles, retained contexts, invented roots, and teardown paths that can bypass native drain or restart
recovery.

ADR-094 and `require-restart-for-config-activation` supersede this change's live component replacement design. Runtime
composition is one sealed boot generation plus restart-safe terminal shutdown; replacement/removal transition
protocols are deleted rather than repaired.

ADR-095 and `simplify-one-shot-lifecycle-ownership` own current service-shutdown and terminal owner sequencing.
`restore-go-lifecycle-ownership` retains its completed context-bearing signature prerequisite and active
runtime-context-ownership work. It no longer claims that stopping is clean completion, concurrent Stop joins one
result, deadline expiry is rejoinable, or repeated Stop replays a retained error. It preserves context provenance,
exact Start finalization, failed-Start cleanup authority, nil rejection, and no detached roots.

This is **BREAKING** by design. Compile failures are safer than compatibility shims that preserve detached work or
ambiguous authority.

## What Changes

- Change framework service shutdown to caller-owned context: `Service.Stop(ctx)` and `Manager.StopAll(ctx)`.
- Change component shutdown to caller-owned context: `LifecycleComponent.Stop(ctx)`.
- Land the service and component signature changes as one atomic prerequisite, with distinct test and migration
  sections inside that PR.
- Define `Start(ctx)` as the lifetime authority for runtime work. `Stop(ctx)` uses its argument to bound quiesce,
  drain, private lifetime cancellation, join, and cleanup. Owners with accepted work drain before cancellation;
  owners without an admission/drain phase may cancel immediately.
- Establish manager/component supervisors whose goroutine function parameters own Start contexts for boot runtimes.
- Remove exported `context.CancelFunc` authority and prohibit production context retention or invented roots outside
  process composition and tests.
- Depend on ADR-094 for value-only Registry/observation, callback-scoped boot-runtime access, and deletion of all live
  replacement/removal paths.
- Make terminal Stop fence external borrows, quiesce and drain accepted NATS work, then cancel and join the exact boot
  generation. Dirty restart relies on durable settlement rather than shutdown hooks.
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
- Reintroducing generalized live component replacement or removal.
- Changing wire formats, NATS subjects, persisted state, or configuration schema.

## Capabilities

### New Capabilities

- `runtime-context-ownership`: defines runtime context provenance, retention prohibitions, cancellation ownership, and
  Start/Stop join semantics.

### Delegated Capabilities

- `service-shutdown`: the completed context-bearing signature prerequisite remains historical implementation truth;
  ADR-095 and `simplify-one-shot-lifecycle-ownership` own current Stop and StopAll semantics.

## Impact

- **Go API:** every `LifecycleComponent`, `Service`, direct Stop caller, and `Manager.StopAll` caller must migrate.
- **Runtime:** shutdown authority flows from composition root to services and components; no library root is invented.
- **Runtime access:** Registry remains value-only; callback borrows of the sealed boot generation return typed missing,
  failed, or stopping without exposing transition/replacement authority.
- **Downstream:** compiler-directed migrations are required. Sister teams own their repositories and product proof.
- **Release:** the atomic prerequisite is breaking and requires both relevant E2E gates before it merges.
