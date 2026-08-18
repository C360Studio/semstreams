# PR #990 boot-only disposition

## Checkpoint identity

- Disposition baseline: `42f349b02bfa9517cff575a9c2a1af3094e591ce`
- Historical PR #990 head: `8f19ef3678a549913385b090e4de1766a7a43a27`
- Passed inventory:
  [`pr990-truth-reset-inventory.md`](pr990-truth-reset-inventory.md)
- Passed inventory SHA-256:
  `5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`
- Independent verdict: `INVENTORY PASS`
- Credit: zero implementation, lifecycle, proof, release, archive, or tag credit.

The passed inventory, including its surface inventory, collision table, adopter
seam inventory, and blocking findings, is incorporated here unchanged by exact
hash. This artifact records the owner’s binding disposition; it is not a new
design or prerequisite program.

## Binding disposition

Historical PR #990 is rejected as a merge, rebase, commit replay, or
cherry-pick unit. Its commits SHALL remain historical evidence only.

The only allowed implementation is a narrow reconstruction from the current
main baseline. The reconstruction SHALL implement only the accepted semantics
below. It SHALL NOT repair adjacent findings, import historical lifecycle
machinery, or broaden scope because a historical hunk is nearby.

## Accepted semantics

### Boot composition

- ComponentManager reads the existing configuration once during construction
  and composes that fixed component set.
- ComponentManager does not subscribe to component or model-registry changes.
- Post-construction configuration writes do not create, start, stop, remove,
  reconfigure, restart, reconcile, or replace components.
- Generic runtime component-config PUT and `watch_config` retire.
- Registry admits the boot composition, then seals.
- Registry exposes immutable defensive declaration values, never shared live
  component handles.
- ComponentManager remains the sole owner of its concrete runtime handles.
- No replacement reservation, runtime generation, removal transition, or
  same-instance mutation protocol survives.

### Flow authoring

- Flow remains saved authoring CRUD plus the existing validator and compiler.
- Saving or updating a diagram changes only flowstore.
- Explicit `POST /flows/{id}/publish-component-configs` validates, compiles,
  sorts instance names, and upserts component configuration through the
  existing Config Manager write operation.
- Publication is upsert-only. An omitted node never implies deletion.
- Partial failure reports the exact persisted names and failed name; retry is
  safe.
- Successful publication reports that runtime is unchanged and reboot is
  required.
- Deploy, start, stop, and undeploy lifecycle state, routes, tools, metrics,
  timestamps, logs, and streams retire without aliases.
- Flowstore contains no runtime lifecycle state or effective-runtime claim.

### Runtime and Rule behavior

- The running process remains unchanged until reboot.
- Rule code, Rule storage, Rule watchers, and current Rule hot-reload behavior
  are unchanged by this reconstruction.
- This disposition neither completes nor advances the separate Rule
  hot-reload target.

## Surfaces preserved byte-for-byte

The reconstruction SHALL NOT change:

- `config.Manager` persistence, arbitration, watchers, `GetConfig`, `OnChange`,
  `WatchModelRegistry`, write methods, or shutdown behavior;
- `model.Watcher` or model-registry behavior;
- Validator construction or existing factory behavior;
- `internal/lifecyclejoin`, `Generation`, `Operation`, or
  `StopWithQuiesce`;
- ComponentManager, service, component, CronScheduler, or other owner
  `Start`/`Stop` lifecycle mechanics;
- ACK ordering, consumer handling, NATS shutdown, or recovery mechanics.

Historical findings concerning multi-key configuration atomicity, partial
watch creation/arbitration, and validator constructor effects remain recorded
findings. They are not prerequisites, are not solved here, and receive no
completion claim.

## Explicit exclusions

The reconstruction SHALL NOT include:

- `monitor_workflow_runs` or any replacement for `monitor_flow`;
- deletion or modification of the E2E WebSocket client;
- boot digests, desired/effective provenance, activation records, or new flow
  state models;
- new exported types, storage, subjects, buckets, registries, lifecycle
  wrappers, coordination protocols, or state machines;
- compatibility aliases or deprecated parallel paths;
- any sister-repository change.

No communication, payload, query, or orchestration primitive is added, so the
shared decision skills do not trigger.

## Diff-directed allow rule

A historical hunk is eligible only when all of the following are true:

1. its path appears in the historical merge-base-to-head diff;
2. it directly implements an accepted semantic above;
3. it can be reconstructed against current main without altering a preserved
   surface;
4. its focused test proves boot-only composition or authoring-only Flow
   behavior; and
5. independent review confirms it adds no prerequisite or lifecycle credit.

Expected eligible production territory is limited to Registry value exposure,
ComponentManager construction/config HTTP removal, Flow/Engine/flowstore
authoring surfaces, flow lifecycle tool removal, API schema, and necessary
composition wiring. Tests and documentation may change only to prove or
explain those exact removals.

Any production path outside that territory, or any hunk touching `config/**`,
`model/**`, `processor/rule/**`, `internal/lifecyclejoin/**`, `natsclient/**`,
CronScheduler, `monitor_workflow_runs`, or the E2E WebSocket client, is
rejected. An unexpected required path stops the reconstruction for owner
review; it does not expand this disposition implicitly.

## Adopter seam disposition

A component or config author must know only that topology changes apply after
reboot. Doing nothing leaves the running process unchanged. Removed Go APIs
fail at compile time; removed HTTP operations fail by route absence.

A Flow author must know only: save, validate, explicitly publish if desired,
then reboot. Saving alone publishes nothing. Publication reports actual
progress and reboot requirement; callers do not infer runtime state.

A Rule author sees no contract change.

Migration guidance SHALL name removed APIs and the save/validate/publish/reboot
sequence. Sister repositories remain read-only; their owners perform and
validate their own migrations.

## Focused verification gates

Before integration:

- prove ComponentManager constructs only the captured boot set;
- prove post-boot component and model-registry writes leave runtime identity
  and membership unchanged;
- prove no component-config subscription, runtime PUT, reconcile, replacement,
  or removal surface remains;
- prove Registry seals and returns defensive values with no live handle;
- prove Flow CRUD does not publish configuration;
- prove validation and compilation preserve current behavior;
- prove explicit publication is sorted, upsert-only, retry-safe, and reports
  exact partial progress plus reboot requirement;
- prove lifecycle routes, tools, states, logs, metrics, and streams are absent;
- prove Rule, Config Manager, validator/factory, lifecyclejoin, CronScheduler,
  ACK, and E2E WebSocket diffs are empty;
- run focused race tests, repository lint/race tests, schema/no-drift,
  contract tests, and relevant core/CRUD E2E before the breaking change lands.

After merge, reproduce the lifecycle census on the merged commit. This work
receives zero lifecycle credit. Lifecycle migration, controlled restart,
dirty recovery, effect-before-ACK proof, archive, and tag readiness remain
owned exclusively by `simplify-one-shot-lifecycle-ownership`.
