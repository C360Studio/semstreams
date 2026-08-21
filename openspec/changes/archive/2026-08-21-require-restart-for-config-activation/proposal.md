# Change: Make boot the component-composition activation boundary

## Why

SemStreams configuration persistence and running component lifecycle were coupled. A configuration write could be
interpreted as a command to mutate the running process, although current products do not require general component
hot reload. That coupling introduced replacement, reconciliation, and runtime-state machinery around what should be a
simple Go ownership boundary.

The approved recovery is narrower: build one fixed component set at boot, keep Flow as an authoring and compilation
surface, publish component configuration only when explicitly requested, and require a process reboot before the
published configuration becomes active.

## What changes

- ComponentManager reads the existing configuration once during construction and owns the resulting concrete
  component handles for the process lifetime.
- ComponentManager does not subscribe to component or model-registry configuration changes. Later writes do not
  mutate the running component set.
- Registry admits the boot composition, seals it, and exposes immutable defensive declaration values without live
  component handles or runtime replacement/removal protocols.
- Generic runtime component-config PUT and `watch_config` retire.
- Flow keeps saved authoring CRUD plus the existing validator and compiler. Saving or updating a Flow changes only
  flowstore.
- Explicit `POST /flows/{id}/publish-component-configs` validates and compiles the Flow, sorts component instance
  names, and upserts their configuration through the existing Config Manager write operation.
- Publication is upsert-only. Omitted nodes are not deleted. Partial failure reports the exact persisted names and
  failed name so retry is safe.
- Successful publication reports that the running process is unchanged and reboot is required.
- Flow runtime lifecycle state, routes, tools, metrics, timestamps, logs, and streams retire without aliases.
- Rule code, storage, watchers, and behavior are outside this change. No Rule or readiness capability delta is retained
  or promoted by this archive.

Existing Config Manager behavior is preserved except for one owner-approved prerequisite: a foreign shared-bucket
platform identity fails Config Manager Start instead of entering detached mode. Validator/factories, lifecycle
ownership, ACK ordering, NATS shutdown, and recovery behavior are preserved. Historical findings about configuration
atomicity, partial watchers/arbitration, and validator constructor effects remain deferred findings rather than
prerequisites.

## Capabilities

### New capability

- `flow-authoring`: saved authoring CRUD, validation/compilation, and explicit sorted upsert-only publication for the
  next process boot.

### Modified capabilities

- `component-runtime-config`: post-construction component configuration writes are next-boot-only; generic live apply
  retires.
- `service-composition`: running service and component composition is fixed at boot.
- `component-discovery`: Registry admission is boot-owned, sealed, defensive, and handle-free.
- `framework-composition`: boot consumes one captured component configuration and has no later dynamic admission path.

## Impact

- **Breaking API and behavior:** generic runtime component mutation and Flow lifecycle surfaces retire without
  compatibility aliases.
- **Adopter contract:** a component author need know only that configuration and topology changes apply after reboot.
  Doing nothing leaves the running process unchanged.
- **Flow author contract:** save, validate, explicitly publish when desired, then reboot. Saving alone publishes
  nothing; publication reports observed progress rather than predicting activation.
- **Rule author contract:** no behavior or API change.
- **Migration:** sister repositories remain read-only. SemStreams records exact downstream impact and migration steps;
  downstream owners implement and validate their own changes.
- **Credit:** this change records only the boot-composition and Flow-authoring behavior landed in PR #997. It claims no
  lifecycle, shutdown, recovery, release, or tag-readiness evidence.

The binding implementation disposition is
[`pr990-boot-only-disposition.md`](pr990-boot-only-disposition.md), SHA-256
`40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`.
