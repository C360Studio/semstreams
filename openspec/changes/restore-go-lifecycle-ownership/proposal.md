## Why

SemStreams originally represented graceful shutdown with a duration even though `Start` receives caller authority.
The landed atomic prerequisite changed component and service shutdown to caller-owned contexts. The remaining work in
this change is limited to removing retained runtime contexts, invented library roots, and exported cancellation
authority.

This remains **BREAKING** by design. Compile failures are safer than compatibility shims that preserve detached work or
ambiguous authority.

## What Changes

- Preserve the completed source break to `Service.Stop(ctx)`, `Manager.StopAll(ctx)`, and
  `LifecycleComponent.Stop(ctx)` as a landed prerequisite.
- Define `Start(ctx)` or an equivalent context-bearing Run boundary as the provenance of runtime work.
- Remove exported `context.CancelFunc` authority and prohibit production context retention or invented roots outside
  process composition and tests.
- Require Stop to reject nil and use its caller context only to bound the separately specified terminal operation; it
  never stores that argument or turns it into continuing work.
- Publish a migration guide and read-only sister-repository callsite census for the completed signature break.

ADR-095 and `simplify-one-shot-lifecycle-ownership` exclusively own `startDone`, failed-Start cleanup, quiesce/drain,
service shutdown, terminal sequencing, callback-borrow fencing, ACK/restart proof, and the next-tag lifecycle/E2E gate.
`require-restart-for-config-activation` owns Registry and boot-only composition. Completed impact and E2E evidence in
this change is historical prerequisite evidence, not an open lifecycle or release gate here.

## Atomic prerequisite

`ComponentManager` implements `Service` and calls `LifecycleComponent.Stop`. A partial signature migration could not
preserve the caller's context: a valid non-deadlined context has no lossless `time.Duration` representation, while a
duration cannot carry cancellation or values. Therefore `Service.Stop`, `Manager.StopAll`, `LifecycleComponent.Stop`,
and every SemStreams implementation and caller changed together in the landed prerequisite.

There is no temporary adapter, deprecated overload, or knowingly incomplete merged signature set.

## Non-Goals

- Defining drain, shutdown, exact Start-finalization, failed-Start cleanup, callback-borrow, ACK, or restart mechanics.
- Defining Registry, runtime-handle access, boot composition, live replacement, or removal.
- Adding orchestration policy, workflows, retry policy, or a general state machine framework.
- Storing `context.Context` so a no-argument Stop can find it later.
- Keeping deprecated duration-based public overloads or exported cancellation handles.
- Editing sister repositories from SemStreams.
- Changing wire formats, NATS subjects, persisted state, or configuration schema.

## Capabilities

### New Capabilities

- `runtime-context-ownership`: defines runtime context provenance, retention prohibitions, cancellation ownership, nil
  rejection, and the prohibition on invented replacement roots.

### Delegated Capabilities

- `service-shutdown` and `restart-safe-shutdown`: ADR-095 and `simplify-one-shot-lifecycle-ownership` own current
  lifecycle mechanics and proof.
- Registry and boot-only composition: `require-restart-for-config-activation` owns current composition truth.

## Impact

- **Go API:** the landed prerequisite requires every `LifecycleComponent`, `Service`, direct Stop caller, and
  `Manager.StopAll` caller to use `context.Context`.
- **Context ownership:** runtime authority flows from composition into Start/Run boundaries; no library root is
  invented and no production struct retains context.
- **Downstream:** compiler-directed prerequisite migrations remain documented. Sister teams own their repositories and
  product proof.
- **Release evidence:** completed prerequisite validation remains historical evidence. Current lifecycle and E2E tag
  readiness is tracked only by `simplify-one-shot-lifecycle-ownership`.
