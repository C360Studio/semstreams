<!-- markdownlint-disable MD041 -->

## Why

Foundation B established one strict port language and one normalized facts projection, but successful component
generations still have no single retained declaration snapshot. Registry, ComponentManager, flowgraph, capability
publication, and message-logger can observe different declaration moments, while ComponentManager retains a parallel
resource-admission interpretation.

The same inventory found a service-composition collision: production does not actually activate the advertised
service hot-update path, service routes are static, and boot constructs services from the stale original file config
after Config Manager has already selected effective desired state. This change records the owner-accepted clean break
before implementation.

## What Changes

- **BREAKING** Supersede the stale Foundation C shape with one immutable Registry record per admitted component
  generation: factory identity, component, cloned effective inputs/outputs captured once, normalized facts,
  exclusive-resource facts, and local generation.
- Make Registry the sole declaration-derived resource-admission owner and remove ComponentManager's parallel tracker,
  conflict bookkeeping, and component port re-reads.
- **BREAKING** Remove identity-free component Registry admission. No alias, wrapper, deprecated path, or compatibility
  shim remains.
- Make declaration-neutral live updates prove normalized-fact equality before mutation; declaration changes refuse
  typed before mutation or use one prepared replacement generation.
- Move shared runtime consumers and enabled message-logger wildcard discovery to defensive complete Registry
  snapshots through one bounded, coalescing, process-local observer.
- Keep that snapshot/observer an internal framework API, not a cross-repo or ADR contract, with no durable replay or
  recovery claim.
- Preserve the measured message-logger expansion from 389 raw rows / 245 keys / 51 strings to 565 effective rows /
  380 keys / 66 strings, including 176 added rows, 135 net-new keys, 15 net-new strings, zero removals, 41 exact-key
  collapses, and the three accepted containment overlaps: new `agent.toolcall.proposed.*` under raw
  `agent.toolcall.proposed.>`; raw `agent.toolcall.approved.*` under new `agent.toolcall.approved.>`; and raw
  `agent.toolcall.rejected.*` under new `agent.toolcall.rejected.>`.
- **BREAKING** Remove `types.ServiceConfig.Name`; the `ServiceConfigs` map key is the sole service identity.
- Make services restart-only process-composition units while components remain runtime-configurable flow units.
  `services.*` is durable desired next-boot state and never mutates running services.
- Consume the post-`config.Manager.Start` effective `SafeConfig`. Preserve accepted version arbitration: a newer file
  version may overwrite KV, while equal or older file versions select KV. When KV is selected, replace only Services
  from current `services.*` keys; every other top-level section keeps its existing behavior.
- Add one pure outer service-map resolver with structural raw-JSON canonicalization and no service-specific inner
  codec, validation, schema, or defaulting. Constructors remain sole inner-config owners.
- **BREAKING** Change service `Manager.RegisterInstance` from void to error-returning. It and `CreateService` are the
  only pre-seal composition writers; both reject duplicates and fail typed after the actual composition is sealed,
  before any service starts or contributes HTTP/OpenAPI.
- **BREAKING** Remove the service config watcher/diff/apply path, `RuntimeConfigurable`, the service runtime schema
  marker, exported `StartService`/`StopService`/`RemoveService`, message-logger inner `enabled`/`log_level`, metrics
  inner `enabled`, and loader-injected message-logger. No shim remains.
- Add deterministic `restart_required` and sorted structural pending changes to existing `GET /services`. This reports
  that restart is required to attempt consumption; it never predicts boot success.
- Keep preconstruction stream-planning intent and admitted Registry declarations as distinct facts that share canonical
  classification while each owner retains its policy response. Enforce the measured 61 default-only JetStream output
  rows / 61 explicitly covered / zero uncovered invariant.

## Capabilities

### New Capabilities

- `message-logger`: Defines explicit default-off activation, Registry-snapshot wildcard observation, exact expansion,
  deduplication, and removal of raw-config declaration prediction.
- `service-composition`: Defines outer desired-service resolution, immutable pre-start composition sealing, restart
  comparison, and runtime/route/OpenAPI identity subsets.

### Modified Capabilities

- `component-discovery`: Adds immutable per-generation Registry declarations, sole resource admission, defensive
  complete snapshots, mandatory factory identity, and group-neutral admission.
- `component-runtime-config`: Adds declaration immutability, prepared replacement, and complete-record removal.
- `stream-provisioning`: Separates configured provisioning intent from admitted runtime declaration and binds the
  61/61/0 default-output coverage invariant.

## Impact

The implementation spans component Registry records and observers, ComponentManager admission/reporting, flowgraph
and capability publication, message-logger configuration/discovery, service configuration and Manager composition,
boot config synchronization, HTTP/OpenAPI reporting, stream-planning structural validation, schemas, tests, and owned
documentation.

SemStreams is the direct consumer. The ten paused downstream projects remain later parity evidence: `semdev`,
`semmachina`, `semsource`, `semboids`, `semdragon`, `semstreams-ui`, `semteams`, `semconnect`, `semlink`, and `semops`.
They do not shape or block the framework contract, and this change implements none of their migrations.

## Non-goals

- No `Ports() PortConfig`, `Registration.DefaultPorts`, static declaration factory, or second component-config
  normalization hook.
- No declaration KV bucket, JetStream stream, audit log, ownership claim, repair worker, or recovery protocol.
- No readiness, health, or `GRAPH_STATUS` change; no service-state bucket/stream or durable restart-required key.
- No generic group/cohort, provider phase, fixed-service manifest, lifecycle scheduler, or orchestration state.
- No service hot configuration, dynamic mux, dynamic routing, route removal, or compatibility shim.
- No `restart_blocked`, config-error status, speculative inner validation, or restart-success prediction.
- No version-arbitration redesign. Documentation or diagnostic clarity may improve later as non-foundational work.
- No graph-index reconciliation, hierarchy, research mutation, or retention work.
- No downstream implementation, issue mutation, or activation/archive of `semantic-tier-split`.
