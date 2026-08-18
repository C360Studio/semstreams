## ADDED Requirements

### Requirement: Desired component configuration activates only at boot

Component configuration persisted while a process is running SHALL be durable desired state for a later boot. It
SHALL NOT create, start, stop, remove, reconfigure, restart, or replace a component in that running process.

The generic ComponentManager live-config PUT contract and generic component `UpdateConfig` contract SHALL NOT exist.
No hidden interface probe, config watcher, or direct config-KV write SHALL bypass the boot activation boundary.

Rule-definition hot reload is the only configuration exception and SHALL satisfy the separate `rule-hot-reload`
capability. The Rule component's own configuration remains next-boot-only.

#### Scenario: Desired component edit leaves runtime sealed

- **GIVEN** a running process with component A admitted from boot configuration C
- **WHEN** desired state stores configuration C' for A
- **THEN** running A remains the same generation with effective configuration C
- **AND** C' is eligible at the next successful boot

#### Scenario: Desired add and remove leave runtime sealed

- **GIVEN** a running process with a sealed component set
- **WHEN** desired state adds component B or removes component A
- **THEN** no running component is created, stopped, removed, restarted, or replaced
- **AND** the desired set is consumed at the next successful boot

#### Scenario: Dirty exit does not suppress desired state

- **GIVEN** desired component state C' committed before the running process loses power
- **WHEN** a new process successfully boots against the retained desired store
- **THEN** boot consumes C'
- **AND** no missing clean-exit record causes boot to reuse C

#### Scenario: Direct KV write is not a lifecycle command

- **WHEN** an external writer changes `components.*`, `model_registry`, `platform`, `nats`, or `services.*`
- **THEN** the write changes desired state only
- **AND** no runtime subscriber interprets it as a component lifecycle command

### Requirement: Desired-state mutation reports restart truth

An API that accepts a desired component or flow mutation while a process is running SHALL distinguish persistence from
activation. A successful response SHALL report that desired state changed, the current runtime is unchanged, and
restart is required.

Write success SHALL NOT be called applied, active, deployed, started, stopped, or removed with respect to the running
process. SemStreams SHALL NOT automatically restart the process.

#### Scenario: Flow deployment is pending next boot

- **GIVEN** a running process
- **WHEN** a validated flow deployment persists desired component records
- **THEN** the response reports desired state accepted
- **AND** it reports runtime unchanged and restart required

#### Scenario: Invalid desired state changes nothing

- **WHEN** desired component or flow configuration fails schema, composition, or declaration validation
- **THEN** neither durable desired state nor the running process changes
- **AND** the response identifies the validation failure

### Requirement: Runtime handles are manager-owned and callback-scoped

ComponentManager SHALL be the sole owner of boot-generation runtime handles. Registry generations, flow graph,
dependency records, lifecycle records, and observation DTOs SHALL expose values only. Runtime access, where a concrete
in-process consumer still requires it, SHALL be limited to a manager-scoped callback borrow.

A borrow SHALL return typed `missing`, `failed`, or `stopping` errors and SHALL never return a handle to retain.
No replacement/removal transition state or same-instance mutation protocol SHALL exist. Terminal shutdown behavior,
including callback-borrow fencing and return-before-Stop requirements, is specified only by
`simplify-one-shot-lifecycle-ownership`'s `service-shutdown` capability.

#### Scenario: Callback is the complete handle lifetime

- **GIVEN** a healthy component from the sealed boot generation
- **WHEN** ComponentManager admits a runtime callback borrow
- **THEN** the manager releases its locks before invoking the callback
- **AND** the borrow ends when the callback returns
- **AND** retaining the handle outside the callback is unsupported

#### Scenario: Raw handle APIs are unavailable

- **WHEN** a runtime consumer seeks a component through Registry, flow graph, dependency records, or manager DTOs
- **THEN** no supported API returns the runtime handle
- **AND** observation remains value-only

## REMOVED Requirements

### Requirement: A runtime config change is applied via any supported reconfig contract

**Reason**: generic live interface probing has no production `UpdateConfig` implementer and makes component authors
predict hidden activation semantics. The demonstrated Rule use case moves to `rule-hot-reload`.

**Migration**: remove generic live apply. Persist desired component state and restart; use the dedicated rule API for
rule definitions.

### Requirement: The config-update response honestly reports whether it was applied

**Reason**: the transient live PUT is retired. Desired-state responses now report persistence, unchanged runtime, and
restart requirement.

**Migration**: callers stop using `applied`; flow/config authoring consumes the typed next-boot result.

### Requirement: A rejected update does not become a stored-but-unapplied config

**Reason**: validation-before-persistence survives in the narrower desired-state requirement above; the live component
application branch no longer exists.

**Migration**: validate desired state before storing it. No runtime rollback path remains.

### Requirement: Runtime component add/remove via the engine write methods drives a reconcile

**Reason**: config writes are facts for the next boot, not commands to mutate the running component set.

**Migration**: Engine writes desired component state and reports restart required.

### Requirement: The engine-owned-revision skip suppresses only the in-memory re-apply

**Reason**: ComponentManager no longer subscribes to config revisions, so engine high-water notification routing is
not an activation contract.

**Migration**: retain only desired-state synchronization required by config readers and next boot.

### Requirement: Runtime config-map mutations are serialized so a concurrent add/remove is never lost

**Reason**: desired-state writes still require ordinary atomic persistence, but there is no running-map reconcile whose
concurrent add/remove can mutate component lifecycle. The old requirement is runtime-reconcile-specific.

**Migration**: preserve atomic desired-state writes without ComponentManager notification semantics.

### Requirement: A component's effective config has one source of truth that GET config reflects

**Reason**: effective runtime config is the immutable boot snapshot. Desired config is a distinct next-boot fact; a
generic live GET/PUT pair must not conflate them.

**Migration**: runtime observation reports the boot snapshot; desired-state APIs report the separately persisted next
boot value.

### Requirement: A no-op runtime config update does not restart a running component

**Reason**: no runtime config update restarts any component, whether equal or changed.

**Migration**: delete equality-driven restart logic. Desired-state stores may retain normal idempotent write behavior.

### Requirement: Declarations are immutable within a generation

**Reason**: declaration immutability becomes stronger and simpler: the complete runtime composition is sealed after
boot, so there is no declaration-neutral live config branch or replacement escape hatch.

**Migration**: Registry admits the boot declaration once; all declaration changes require restart.

### Requirement: Replacement publishes one atomic generation

**Reason**: in-process generation replacement is retired.

**Migration**: delete replacement preparation, reservation, commit, and observer semantics without compatibility
aliases.

### Requirement: Removal deletes one complete generation record

**Reason**: in-process component removal is retired. Terminal process shutdown is not a runtime configuration
mutation.

**Migration**: desired-state removal becomes effective on next boot; terminal shutdown follows service-shutdown.
