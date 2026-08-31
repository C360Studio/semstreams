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
fail before arbitration, watchers, writes, or dependent construction; detached running mode SHALL NOT exist.

Before arbitration, Start SHALL establish the deployment's platform identity from the bucket's `platform_identity`
record, deciding from a single pre-mint read of the bucket's keys and under the context passed to Start:

- the record is present — Start SHALL adopt its identifier as the effective `platform.id`, and SHALL fail unless the
  record's organization equals the configuration's `platform.org` and the configuration's `platform.id` equals the
  record's stem or its identifier. An adopted identifier SHALL be validated under the same segment grammar and
  authority-pair bound as a configured one;
- the record is absent and the bucket holds no other key — Start SHALL mint the entropy suffix, write the record with
  an atomic `Create`, and adopt the result; if that `Create` conflicts with a concurrent process, Start SHALL re-read
  the record and adopt the winner's identifier rather than its own;
- the record is absent and the bucket holds other keys — Start SHALL fail, naming that the bucket predates identity
  minting and instructing fresh storage. It SHALL mint nothing and SHALL create nothing.

The record SHALL carry exactly the fields `org`, `stem`, and `id`. First-boot detection SHALL ignore the
`platform_identity` key, so a boot that has just created it is still a first boot. The identity guard SHALL compare
the effective identifier. Configuration synchronization SHALL NOT apply the KV `platform` key to the running
configuration — it remains a published mirror only — and version arbitration SHALL never write, overwrite, or apply
`platform_identity`.

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

- **GIVEN** an empty configuration bucket and a file declaring `platform.id` `dep`
- **WHEN** Config Manager starts
- **THEN** `platform_identity` is created carrying exactly `org`, `stem` `dep`, and `id` `dep-` plus six hex bytes,
  the effective configuration's `platform.id` is that identifier, and the pushed `platform` key carries it
- **AND** the boot is still treated as a first boot, so the file configuration is pushed to the bucket
- **AND** every KV operation of the mint uses the context passed to Start
- **AND** the test that verifies this is `TestConfigManagerFirstBootMintsPlatformIdentity`

#### Scenario: A later boot and a co-process adopt the persisted identity

- **GIVEN** `platform_identity` records organization `acme`, stem `dep`, and identifier `dep-7f3a9c`
- **WHEN** a process whose file declares `platform.id` `dep` starts, and concurrently a second process with the same file starts
- **THEN** both adopt `dep-7f3a9c` and neither creates a second record — the loser of the atomic Create reads the winner's
- **AND** a file declaring `platform.id` `dep-7f3a9c` is also adopted, while a file declaring `other`, or one
  declaring a different `platform.org`, returns the identity mismatch
- **AND** the tests that verify this are `TestConfigManagerAdoptsPersistedPlatformIdentity` and
  `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity`

#### Scenario: A bucket that predates identity minting refuses without minting

- **GIVEN** a configuration bucket holding `platform` and `version` keys and no `platform_identity` record
- **WHEN** Config Manager starts
- **THEN** Start fails naming the pre-identity bucket as the cause and instructing fresh storage
- **AND** no `platform_identity` key exists in the bucket afterwards and no suffix was minted
- **AND** the test that verifies this is `TestPreIdentityBucketRefusesStartWithoutMinting`

#### Scenario: A KV platform write never changes the running authority

- **GIVEN** a running Config Manager whose effective `platform.id` is `dep-7f3a9c`
- **WHEN** another writer puts a `platform` key declaring `platform.id` `other` into the shared bucket
- **THEN** the effective configuration's `platform.id` remains `dep-7f3a9c`
- **AND** the test that verifies this is `TestKVPlatformKeyIsAMirrorNotASource`

## ADDED Requirements

### Requirement: The authority pair is bounded against the value that will be minted

Configuration load SHALL bound the authority pair against the identifier that will actually be minted from it, not
against the identifier the document declares: the seven bytes of the entropy suffix are reserved at load, as
`entity-id-contract` specifies. Start SHALL bound the effective pair — minted or adopted — against the full
family-table budget, WITHOUT the declaration reserve, because that pair already carries the suffix; reserving twice
would refuse at Start a pair that passed load. Together these make a pair that passes load and then cannot carry a
framework identity impossible. The framework SHALL NOT probe, roll back, or delete an identity record to discover
the bound — ADR-102 decision 7 forbids rewriting a minted authority, so the only safe order is to refuse before the
`Create`.

#### Scenario: a pair that only fits unsuffixed does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` fit the family-table budget exactly but leave no
  room for the seven-byte suffix
- **WHEN** the deployment boots against an empty bucket
- **THEN** configuration load fails, Start is never reached, and no `platform_identity` record is created
- **AND** the test that verifies this is `TestConfigRejectsPairThatOnlyFitsUnsuffixed`

#### Scenario: a pair at the declarable budget mints and starts

- **GIVEN** a configuration whose `platform.org` and `platform.id` total exactly the declarable budget
- **WHEN** the deployment starts against an empty bucket
- **THEN** the suffix is minted, the effective pair equals the family-table budget, and Start succeeds
- **AND** the test that verifies this is `TestMaximumDeclarablePairMintsAndStarts`
