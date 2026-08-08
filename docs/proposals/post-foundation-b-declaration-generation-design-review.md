# Post-Foundation-B declaration-generation design review

**Repository baseline:** `ee3b43ce67f3ee6b39547317529da7ce1a783233`.
**Reviewed design:** `docs/proposals/post-foundation-b-declaration-generation-design.md`.
**Reviewed owner-accepted identity:** 1,035 lines, 73,672 bytes,
SHA-256 `be8e4c2c6fbcfbcd966038448011cf98112e62e52147b088a0a794808ec9b814`.
**Verdict:** `DESIGN PASS`.

The owner accepts the complete design, and independent review confirms the exact identity above.

## Review basis and chain

The review chain used the accepted post-Foundation-B inventory and its `INVENTORY PASS`, then independently checked:

1. the five original declaration-generation blockers;
2. the corrected message-logger and stream-provisioning censuses;
3. removal of invented service hot-toggle behavior and adoption of the owner restart-only service ruling;
4. boot desired-state authority, outer service resolution, and service composition sealing;
5. optional versus mandatory activation, fixed-service composition responsibility, and route/OpenAPI subset parity;
   and
6. the final mandatory absent-to-explicit-false comparison edge.

Each review iteration invalidated the earlier design identity. Independent review confirmed that the final wording
change is bounded: version arbitration is accepted intentional safety, remains outside implementation scope, and may
receive only later non-foundational documentation or diagnostic clarity. This record approves no source edit, task
activation, downstream migration, or suspended `semantic-tier-split` work beyond the reviewed design handoff.

## Original declaration blocker closure

### 1. Declaration-changing live updates

Closed. Declarations are immutable within one component generation. A declaration-neutral update proves exact
normalized-fact equality before mutation. A declaration-changing update fails typed before mutation or prepares a
complete replacement generation off-Registry. Mutate-then-recapture is forbidden.

### 2. Parallel ComponentManager resource ownership

Closed. Registry is the sole declaration-derived resource-admission owner. ComponentManager's parallel resource map,
conflict checks, registration bookkeeping, unregistration bookkeeping, and component port re-reads are retired.

### 3. Unmeasured preconstruction provisioning premise

Closed. The shipped-flow census contains exactly 61 factory-default JetStream output rows absent from raw component
output configuration. All 61 are covered by explicit `AGENT` / `agent.>` preconstruction declarations; zero are
uncovered. Future uncovered default-only outputs fail structural validation rather than being guessed.

### 4. Undisclosed message-logger observation expansion

Closed. Enabled message-logger `"*"` expands from raw-config-explicit subjects to effective declarations while retaining
raw bodies. The design records exact-set migration, bounded exclusions, exact-key deduplication, wildcard containment,
runtime inspection, and the required owner ruling.

### 5. Identity-free direct Registry admission

Closed. Component `Registry.RegisterInstance(name, component)` is removed or internalized. Production admission uses
`CreateComponent`; any internal replacement/test helper requires validated factory identity and the same capture,
validation, conflict, and atomic-admission contract. No shim or inferred identity remains.

## Declaration census preservation

The final design preserves both distinct censuses and does not use one as evidence for the other.

### Message-logger observation census

- subject rows: 389 raw + 176 default-only = 565 effective;
- summed per-configuration exact keys: 245 raw + 135 net-new = 380 effective;
- distinct global strings: 51 raw + 15 net-new = 66 effective;
- added kinds: 60 JetStream inputs + 61 JetStream outputs + 18 NATS inputs + 28 NATS outputs + 9 NATS-request
  inputs = 176 rows;
- exact-key collapses: 40 duplicate loop/dispatch rows + 1 governance overlap = 41;
- net-new exact keys: 176 - 41 = 135;
- affected configurations: 9 x 15 keys = 135; and
- complete shipped coverage: 9 affected + 16 unchanged = 25 configurations.

There are zero removals. Added declarations contain no bare `*`, bare `>`, `_INBOX`, reply, or undeclared subject.
NATS-request contributes only request subjects. The three `configs/agentic.json` wildcard-containment overlaps remain
named and require deliberate deduplication plus runtime-visible resolved-union inspection.

### Stream-provisioning census

The separate provisioning census remains 61 default-only JetStream output rows, 61 explicitly covered, and zero
uncovered. It answers whether preconstruction stream declarations cover factory-default outputs; it does not describe
message-logger capture.

## Service review-chain blocker closure

### Dead knobs and rejected hot-toggle seam

Closed. Global application log level affects `slog` only and never activates message-logger. Dead inner
message-logger `log_level`, message-logger `enabled`, and metrics `enabled` are removed and rejected without shims. The
loader no longer injects message-logger.

The review rejected the invented service hot-toggle seam. The target removes the service `services.*` mutation
watcher/diff/apply path, `RuntimeConfigurable`, its service implementations, the service runtime schema marker, and
dynamic per-service start/stop/remove APIs. Components retain their separate hot-update method-pair contract.

### Owner restart-only service ruling

Closed. Services are immutable process-composition units. `services.*` is durable desired next-boot configuration;
it never mutates the running service set. Existing `GET /services` gains only deterministic `restart_required` and
sorted structural pending changes. Pending means a restart is required to attempt consumption, not that restart will
succeed. Health, readiness, and `GRAPH_STATUS` remain unchanged.

### Boot desired-state truth and service deletion

Closed. Service composition consumes the post-`config.Manager.Start` effective `SafeConfig`, not the stale original
file-loaded `cfg`. Existing version-based file/KV arbitration is accepted intentional safety: only a newer file
version may overwrite KV; equal or older file versions select KV. File-content changes therefore require a version
advance. Arbitration remains unchanged and outside the implementation; later non-foundational work may improve
documentation or diagnostic clarity.

When existing arbitration selects KV, the Services map is replacement state: synchronization starts empty and loads
only current `services.*` keys. A service deleted from KV cannot survive from the file map. The design does not claim
that KV replaces every other top-level config section.

### Outer resolver and service identity

Closed. `types.ServiceConfig.Name` is removed; the `ServiceConfigs` map key is the sole identity. One pure outer
resolver deep-clones the map, structurally canonicalizes raw inner JSON, applies only outer defaults, and performs no
service-specific inner decoding, validation, defaulting, schema interpretation, or codec callback. Constructors remain
the sole inner validation/default owners and receive the exact resolved raw config.

### Composition seal and writers

Closed. `CreateService` and error-returning `RegisterInstance` are composition-root writers only. They reject
duplicates and fail typed after `StartAll` seals. `StartAll` validates configured optional services and mandatory
`component-manager`, seals the actual admitted identity set before any service starts or contributes HTTP/OpenAPI,
and permits no post-seal identity mutation.

Fixed services remain the binary composition root's responsibility. Manager adds no fixed-service manifest, group, or
omission inference. Production-root tests prove the current framework root registers `milestone` before sealing.

### Optional, mandatory, and manager-infrastructure semantics

Closed. Outer `Enabled` owns activation only for optional services. `component-manager` is the current mandatory
managed service; `service-manager` is required manager configuration/process infrastructure, not an optional managed
service. Every successful boot has both active.

The outer resolver materializes either required outer entry as `Enabled: true` only when absent. It preserves an
explicit false so comparison emits pending `disable` and `restart_required: true`, while boot validation rejects the
state. Deleting or reverting the false materializes true again and clears the comparison. No successful-consumption
promise exists for invalid mandatory-disable state.

### Runtime, route, and OpenAPI parity

Closed. `GET /services` runtime rows equal the full sorted sealed identity set. Bound routes equal the sealed subset
implementing `service.HTTPHandler`. Under the current interface, per-service OpenAPI contributors are the sealed
OpenAPI-capable/`HTTPHandler` subset. Static manager-owned endpoints do not invent service identities, and desired
configuration edits cannot drift any of these views.

## Scope check

The passed owner-accepted design remains bounded to process-local Registry generation snapshots, one
declaration/resource admission owner, bounded in-process declaration observation, immutable component-generation
updates, immutable service composition, and on-demand desired-versus-boot service comparison.

It adds no declaration KV bucket or stream, service status store, ownership service, CQRS layer, recovery subsystem,
fixed-service manifest, group/cohort primitive, lifecycle scheduler, readiness consolidation, index framework,
hierarchy change, research mutation change, retention policy, compatibility shim, or downstream implementation. The
ten paused downstream projects remain holdout parity evidence and do not shape or block the framework contract.

## Final ruling

`DESIGN PASS` applies only to the reviewed identity above. Any design change invalidates this record until the new
identity is independently reviewed. The complete owner-accepted design is ready for OpenSpec authoring and
implementation handoff under the normal project gates.
