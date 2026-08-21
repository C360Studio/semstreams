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

Historical PR #990 remains evidence only and is not a merge, rebase, replay, or cherry-pick source. The accepted
behavior was reconstructed and landed in commit `8117858367e1cc9d1dc434d211989e7a2ed1e552` through PR #997. This design
is reconciled to current main solely so the implemented boot-composition and Flow-authoring truth can be archived.

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

This decision changes composition authority only. It claims no lifecycle, shutdown, transport, recovery, release, or
tag-readiness behavior.

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

### D7. Rule behavior is outside scope

PR #997 did not implement or claim Rule hot-reload, Rule readiness, or Rule entity-watching target state. No Rule or
readiness delta is retained by this change, and archive promotes no Rule requirement.

### D8. Breaking migration stays clean

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
- **Rule author:** nothing changes. Existing Rule behavior continues, and this archive promotes no Rule or readiness
  requirement.

## Archive proof

Implementation conformance is recorded in
[`pr990-boot-only-implementation-conformance.md`](pr990-boot-only-implementation-conformance.md).

Archive requires:

- focused unit and integration race proof for fixed boot composition, sealed Registry declarations, authoring-only Flow
  CRUD, explicit publication, exact partial progress, and retired-surface absence;
- one real process-boundary proof using durable NATS state: process A commits desired component configuration and remains
  unchanged; process A exits; process B starts against the same desired state and composes those candidates;
- repository lint, race, contract, schema/no-drift, strict OpenSpec, and relevant core/CRUD E2E results;
- independent review of the final conformance ledger and archive diff.

PR #997 was already merged as a breaking change. No durable repository artifact proves that its relevant E2E ran before
merge, so this change does not and cannot retroactively claim that timing. E2E recorded during reconciliation is
post-merge evidence for archive and tag confidence only.

Passing these gates grants only boot-composition and Flow-authoring current-truth credit. No lifecycle, shutdown,
recovery, release, or tag-readiness credit is implied.
