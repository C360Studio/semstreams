## ADDED Requirements

### Requirement: Component configuration activates only during process construction

ComponentManager SHALL read the existing configuration once during construction. That captured configuration SHALL
define the complete component set for the process lifetime. Configuration written after construction SHALL be durable
for a later process boot and SHALL NOT create, start, stop, remove, reconfigure, restart, reconcile, or replace a
component in the running process.

ComponentManager SHALL NOT subscribe to component or model-registry configuration changes. The generic runtime
component-config HTTP write and `watch_config` tool SHALL NOT exist. No alternate watcher, interface probe, or direct
KV operation SHALL bypass the boot boundary.

Config Manager persistence, version arbitration, watchers, reads, writes, and shutdown behavior SHALL remain
unchanged after successful Start. If the shared configuration bucket contains a foreign platform identity, Start SHALL
fail before arbitration, watchers, writes, or dependent construction; detached running mode SHALL NOT exist.

#### Scenario: Foreign platform identity fails before publication is available

- **GIVEN** the shared configuration bucket contains another platform identity
- **WHEN** Config Manager starts
- **THEN** Start returns the identity mismatch
- **AND** no configuration watcher, write, or dependent component construction begins

#### Scenario: Post-construction edit leaves runtime unchanged

- **GIVEN** ComponentManager constructed component A from configuration C
- **WHEN** configuration C' for A is persisted
- **THEN** the running A and its effective configuration remain unchanged
- **AND** C' is available to a later process boot

#### Scenario: Post-construction membership change waits for reboot

- **GIVEN** ComponentManager constructed a fixed component set
- **WHEN** later configuration adds B or disables or removes A
- **THEN** no running component is created, stopped, removed, restarted, or replaced
- **AND** a later process boot selects from the then-current persisted configuration

#### Scenario: Model-registry write is not a lifecycle command

- **GIVEN** a running process
- **WHEN** model-registry configuration changes
- **THEN** ComponentManager does not restart or replace a component

### Requirement: Explicit Flow publication reports persistence without activation

The explicit Flow component-configuration publication operation SHALL report the component instance names actually
persisted. Successful publication SHALL report that the running process is unchanged and reboot is required.

Write success SHALL NOT be described as runtime activation. SemStreams SHALL NOT automatically restart the process.

#### Scenario: Publication succeeds for the next boot

- **GIVEN** a running process and a valid saved Flow
- **WHEN** explicit publication persists all compiled component configuration
- **THEN** the response reports the exact persisted component names
- **AND** it reports the running process unchanged and reboot required

#### Scenario: Publication validation fails before writes

- **WHEN** the saved Flow fails the existing validation or compilation contract
- **THEN** no compiled component configuration is persisted
- **AND** the running process remains unchanged

## REMOVED Requirements

The headings below quote the exact legacy requirement names being removed so OpenSpec can match the baseline. Their
live-reconfiguration terminology is historical and is not part of the new boot-only contract.

### Requirement: A runtime config change is applied via any supported reconfig contract

**Reason**: generic live component reconfiguration is not a required product capability and couples persistence to
runtime lifecycle mutation.

**Migration**: persist configuration for a later process boot.

### Requirement: The config-update response honestly reports whether it was applied

**Reason**: the generic runtime update route retires. Explicit Flow publication reports observed persistence and the
reboot requirement without claiming activation.

**Migration**: use authoring CRUD and optional explicit publication, then reboot.

### Requirement: A rejected update does not become a stored-but-unapplied config

**Reason**: validation-before-publication remains, but there is no runtime apply or rollback branch.

**Migration**: validate and compile the saved Flow before explicit upsert-only publication.

### Requirement: Runtime component add/remove via the engine write methods drives a reconcile

**Reason**: Flow compilation no longer commands a runtime reconcile.

**Migration**: explicitly publish compiled configuration when desired and reboot.

### Requirement: The engine-owned-revision skip suppresses only the in-memory re-apply

**Reason**: ComponentManager has no configuration subscription or in-memory re-apply path.

**Migration**: remove revision routing whose only purpose was runtime re-application.

### Requirement: Runtime config-map mutations are serialized so a concurrent add/remove is never lost

**Reason**: the runtime component map does not reconcile with later configuration writes.

**Migration**: preserve existing Config Manager persistence behavior without a runtime-map mutation protocol.

### Requirement: A component's effective config has one source of truth that GET config reflects

**Reason**: the running component uses its captured construction input while Config Manager exposes durable
configuration for a later boot. A generic endpoint must not conflate those facts.

**Migration**: treat Config Manager reads as persisted configuration, not proof of current runtime activation.

### Requirement: A no-op runtime config update does not restart a running component

**Reason**: no post-construction configuration update restarts a running component, whether equal or changed.

**Migration**: remove equality-driven runtime restart logic.

### Requirement: Declarations are immutable within a generation

**Reason**: Registry now retains one sealed boot declaration per admitted component and has no runtime replacement
protocol.

**Migration**: declaration changes take effect only in a later process.

### Requirement: Replacement publishes one atomic generation

**Reason**: in-process component replacement is retired.

**Migration**: remove the former preparation, reservation, commit, and notification protocol without aliases.

### Requirement: Removal deletes one complete generation record

**Reason**: in-process component removal is retired. Process shutdown remains ordinary owner lifecycle work.

**Migration**: persist the next-boot configuration and restart the process.
