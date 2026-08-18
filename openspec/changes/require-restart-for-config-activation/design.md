# Design: Boot-only composition and explicit Flow publication

## Authority and baseline

This design is subordinate to the owner-approved
[`pr990-boot-only-disposition.md`](pr990-boot-only-disposition.md), SHA-256
`40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`.
The passed surface inventory is
[`pr990-truth-reset-inventory.md`](pr990-truth-reset-inventory.md), SHA-256
`5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`.
The durable architecture decision is
[`ADR-096`](../../../docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md).

Historical PR #990 is evidence only. It must not be merged, rebased, replayed, or cherry-picked as a unit. The accepted
behavior is reconstructed narrowly against current main. This change receives zero implementation credit until each
ruling is mapped to the current implementation and independently reviewed.

## Goals

- Compose one fixed component set from configuration captured at process construction.
- Make ComponentManager the sole owner of concrete runtime component handles.
- Keep Registry useful for defensive declaration discovery without lifecycle authority.
- Preserve Flow authoring, validation, compilation, and explicit next-boot configuration publication.
- Make publication progress and reboot requirements observable without claiming runtime activation.
- Remove unused Flow lifecycle and generic runtime component-mutation surfaces.
- Preserve existing Rule, lifecycle, ACK, NATS, and recovery behavior, and ordinary Config Manager behavior.

## Non-goals

- Component, service, model-registry, or topology hot reload.
- Automatic process restart.
- A new runtime lifecycle protocol, replacement state machine, or shutdown abstraction.
- Flow runtime-state tracking, new runtime-comparison metadata, or a new monitoring replacement.
- Advancing the separate Rule hot-reload target.
- Repairing deferred Config Manager or validator findings.
- Compatibility aliases for retired pre-v1 APIs.

## Decisions

### D1. Existing Config Manager writes remain; foreign identity fails startup

Config Manager remains the durable configuration writer and reader. Its persistence, version arbitration, watchers,
`GetConfig`, `OnChange`, `WatchModelRegistry`, write operations, and shutdown behavior are unchanged.

One owner-approved prerequisite narrows startup: if the shared bucket contains another platform identity, Config
Manager Start fails before arbitration, watchers, writes, or dependent construction. It does not enter detached mode.
This makes a successful component write observable without adding a status knob or write-receipt API.

ComponentManager calls the existing read path once during construction. The selected configuration is the complete
input to process composition. Later writes remain durable for a later process boot, but ComponentManager does not
subscribe to them or interpret them as lifecycle commands.

### D2. ComponentManager owns one fixed boot composition

ComponentManager constructs the enabled component set from the captured configuration and retains the concrete
runtime handles. Post-construction writes cannot create, start, stop, remove, reconfigure, restart, reconcile, or
replace a component in that process.

The generic component-config HTTP write and `watch_config` tool retire. No alternate subscription, direct KV write,
or interface probe reintroduces live component mutation.

This decision changes composition authority only. Existing `Start` and `Stop` mechanics, lifecyclejoin use, component
shutdown, transport shutdown, and recovery semantics are preserved and receive no completion credit.

### D3. Registry admits declarations at boot and then seals

Registry accepts validated declaration values while ComponentManager builds the boot composition. Once composition is
complete, Registry seals and rejects further admission. It exposes defensive declaration snapshots, not shared live
component handles. ComponentManager remains the only owner of the concrete handles.

There is no runtime replacement reservation, removal transition, or same-instance mutation protocol. Declaration
presence describes the admitted boot shape; it does not assert readiness or lifecycle state.

### D4. Flow is an authoring and compilation surface

Flow create, read, update, delete, validation, and compilation remain. Saving or updating a diagram changes only
flowstore. It does not write component configuration or mutate the running process.

Flowstore contains authoring data only. It does not persist or claim current runtime lifecycle state, activation
timestamps, or current runtime membership.

### D5. Publication is explicit, sorted, sequential, and upsert-only

`POST /flows/{id}/publish-component-configs` is the sole Flow-to-component-configuration publication operation. It
loads the saved Flow, runs the existing validation and compilation behavior, sorts component instance names, and
calls the existing Config Manager component write operation sequentially.

The operation upserts compiled entries only. A node omitted from the Flow does not delete an existing component
configuration. No new transaction, rollback protocol, bucket, subject, or storage type is introduced.

On failure, the response names exactly the component instances already persisted and the instance whose write failed.
The unattempted suffix remains unreported. Repeating the operation is safe because it is a deterministic sequence of
upserts. On success, the response reports the persisted names, that the running process is unchanged, and that reboot
is required.

### D6. Flow lifecycle surfaces retire without replacement

Flow lifecycle state, operations, tools, metrics, timestamps, logs, and streams retire without aliases. No new monitor
is introduced. Existing Flow health, metrics, and message observations may remain only where they report current
component observations by name; they do not establish Flow ownership of component lifecycle or runtime activation.

### D7. Rule behavior remains separate and unchanged

Rule code, Rule storage, Rule watchers, and current Rule behavior are unchanged by this reconstruction. Existing
target-state artifacts for Rule hot reload, graph-index readiness, and Rule entity watching remain separate,
unimplemented work. This change neither completes nor advances them.

### D8. Deferred findings do not expand the reconstruction

Historical findings about multi-key configuration atomicity, partial watcher creation/version arbitration, and
validator constructor effects remain recorded findings. They are not prerequisites for boot-only composition and are
not repaired opportunistically in this change.

An implementation need outside the binding disposition stops work for owner review. It does not silently broaden the
change.

### D9. Breaking migration stays clean

Retired pre-v1 surfaces have no aliases, deprecated parallel paths, or compatibility shims. SemStreams migration
documentation names the removals and the save/validate/publish/reboot sequence. Sister repositories are read-only;
their owners apply and verify their migrations.

## Adopter seam inventory

- **Component author:** configuration and topology changes apply after reboot. Doing nothing leaves the running process
  unchanged. Removed API compile failures and the migration guide expose the change. The author should not need a
  lifecycle protocol or predict activation.
- **Flow author:** save and validate, explicitly publish, then reboot. Doing nothing after save leaves an authoring-only
  record. The API schema and publication response expose the contract. The author should not know subjects, buckets,
  write ordering, or a runtime-state model.
- **Operator:** successful publication does not restart the process. Doing nothing leaves the existing runtime under
  normal process supervision. Response fields and the migration guide expose the reboot requirement. The operator
  should not manage extra comparison metadata or a reconciliation state machine.
- **Rule author:** nothing changes. Existing Rule behavior continues, and separate target-state artifacts contain any
  future work.

## Verification and credit

Implementation conformance is recorded in
[`pr990-boot-only-implementation-conformance.md`](pr990-boot-only-implementation-conformance.md). Every binding ruling
must map to current file-and-line evidence or an explicit deviation requiring owner signature.

Required gates are focused boot-only and authoring-only tests, the foreign-identity fatal-start proof, race tests,
repository lint/race checks, schema/no-drift, contract tests, and relevant core/CRUD E2E. The final diff must leave
Config Manager unchanged except for the owner-approved fatal foreign-identity rejection and removal of detached mode.
It must leave model watcher, Rule, lifecyclejoin, CronScheduler, ACK/NATS/recovery, and the E2E WebSocket client
untouched.

Passing those gates grants only this change's composition and Flow-authoring credit. Lifecycle migration, controlled
restart, dirty recovery, effect-before-ACK proof, release readiness, archive, and tag readiness remain owned elsewhere.
