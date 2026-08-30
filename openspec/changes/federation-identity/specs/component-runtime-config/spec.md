## MODIFIED Requirements

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
fail before arbitration, watchers, writes, or dependent construction; detached running mode SHALL NOT exist. Before
arbitration, Start SHALL establish the deployment's platform identity from the bucket's `platform_identity` record:
on first boot it mints the entropy suffix unless `platform.unique` is declared, writes the record with atomic
`Create` under the Start context, and adopts the record's identifier as the effective `platform.id`; on a later boot,
or in a second process sharing the bucket, it adopts the record when the file's `platform.id` equals the record's
stem or its identifier and refuses Start otherwise. The identity guard SHALL compare the effective identifier, and
version arbitration SHALL never overwrite the record.

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

#### Scenario: First boot mints and persists the platform identity under the Start context

- **GIVEN** an empty configuration bucket and a file declaring `platform.id` `dep` without `platform.unique`
- **WHEN** Config Manager starts
- **THEN** `platform_identity` is created with stem `dep` and identifier `dep-` plus six hex bytes, the effective
  configuration's `platform.id` is that identifier, the pushed `platform` key carries it, and the suffixed pair passes
  the authority-pair budget
- **AND** every KV operation of the mint uses the context passed to Start
- **AND** the test that verifies this is `TestConfigManagerFirstBootMintsPlatformIdentity`

#### Scenario: A later boot and a co-process adopt the persisted identity

- **GIVEN** `platform_identity` records stem `dep` and identifier `dep-7f3a9c`
- **WHEN** a process whose file declares `platform.id` `dep` starts, and concurrently a second process with the same file starts
- **THEN** both adopt `dep-7f3a9c` and neither creates a second record — the loser of the atomic Create reads the winner's
- **AND** a file declaring `platform.id` `dep-7f3a9c` is also adopted, while a file declaring `other` returns the identity mismatch
- **AND** the tests that verify this are `TestConfigManagerAdoptsPersistedPlatformIdentity` and
  `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity`

#### Scenario: An operator-unique identifier is recorded without a suffix

- **GIVEN** a file declaring `platform.id` `field-ops-7` and `platform.unique: true`
- **WHEN** Config Manager starts against an empty bucket
- **THEN** `platform_identity` records identifier `field-ops-7` with an empty stem and no suffix is minted
- **AND** the test that verifies this is `TestUniquePlatformIDIsNotSuffixed`

## ADDED Requirements

### Requirement: The platform pair has exactly one source

Configuration load SHALL read `platform.org` and `platform.id` from the configuration document only. No
environment variable SHALL override either half of the pair or any other platform field, and the retired
`STREAMKIT_` environment prefix SHALL not be read for any field. The effective `platform.id` is the identifier
`component-runtime-config` establishes at Start; `Config.Validate` SHALL apply the authority-pair budget to the
loaded pair and Start SHALL re-apply it to the effective pair.

#### Scenario: an environment variable cannot change the authority

- **GIVEN** a configuration declaring `platform.id` `dep` and an environment with `STREAMKIT_PLATFORM_ID=other`
- **WHEN** configuration load runs
- **THEN** the loaded `platform.id` is `dep`
- **AND** the test that verifies this is `TestPlatformPairHasNoEnvironmentOverride`
