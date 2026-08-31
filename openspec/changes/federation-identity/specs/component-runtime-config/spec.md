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

Start SHALL acquire the shared configuration bucket under the context passed to Start; no constructor, factory, or
other non-lifecycle boundary SHALL perform that acquisition or invent a context for it. Having acquired it, Start
SHALL read the bucket's live retention policy and SHALL fail, naming the offending value, if that policy can delete
keys — a nonzero TTL or a binding size cap. Acquisition returns an existing bucket unchanged and this package is not
the bucket's only creator, so the policy in force may be one it never chose; a create-once identity under an evicting
policy expires and is reminted as a second authority, which ADR-102 decision 7 forbids ever reconciling. Nothing
SHALL be minted or created before that check passes.

Before arbitration, Start SHALL establish the deployment's platform identity from the bucket's `platform_identity`
record, deciding from a single pre-mint read of the bucket's keys and under the context passed to Start:

- the record is present — Start SHALL adopt its identifier as the effective `platform.id`, and SHALL fail unless the
  record's organization equals the configuration's `platform.org` and the configuration's `platform.id` equals the
  record's stem. Configuration declares the STEM and only the stem: the minted identifier is not a declarable value,
  and a configuration declaring it SHALL be refused with guidance naming the stem to declare instead — decided by
  comparison against the recorded identifier, never by inspecting the value's grammar. An adopted identifier SHALL be
  validated under the same segment grammar and authority-pair bound as a configured one;
- the record is absent and the bucket holds no other key — Start SHALL mint the entropy suffix, write the record with
  an atomic `Create`, and adopt the result; if that `Create` conflicts with a concurrent process, Start SHALL re-read
  the record and adopt the winner's identifier rather than its own;
- the record is absent and the bucket holds other keys — Start SHALL fail, naming that the bucket predates identity
  minting and instructing fresh storage. It SHALL mint nothing and SHALL create nothing.

Before creating or adopting the record, Start SHALL claim the bucket for this deployment's `platform.environment`
with an atomic create of an internal guard key, and SHALL fail — naming both environments — when the bucket was
already claimed by a different one. At most one environment may establish against one configuration bucket. The claim
SHALL precede the record so that a failure between the two leaves a state a same-environment boot completes and a
different-environment boot is refused. The guard is internal: it is NOT a field of the record, whose shape is a
cross-repo read contract.

The record SHALL carry exactly the fields `org`, `stem`, and `id`. First-boot detection SHALL ignore the
`platform_identity` key and the environment guard key, so a boot that has just created either is still a first boot. The identity guard SHALL compare
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
- **AND** a file declaring `other`, or one declaring a different `platform.org`, returns the identity mismatch
- **AND** a file declaring `platform.id` `dep-7f3a9c` — the minted identifier rather than the stem — is refused with
  guidance to declare `dep`
- **AND** the tests that verify this are `TestConfigManagerAdoptsPersistedPlatformIdentity`,
  `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity` and
  `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance`

#### Scenario: A bucket that predates identity minting refuses without minting

- **GIVEN** a configuration bucket holding `platform` and `version` keys and no `platform_identity` record
- **WHEN** Config Manager starts
- **THEN** Start fails naming the pre-identity bucket as the cause and instructing fresh storage
- **AND** no `platform_identity` key exists in the bucket afterwards and no suffix was minted
- **AND** the test that verifies this is `TestPreIdentityBucketRefusesStartWithoutMinting`

#### Scenario: A second environment cannot establish against the same bucket

- **GIVEN** an empty configuration bucket
- **WHEN** two deployments declaring the same `platform.org` and `platform.id` but `platform.environment` `prod` and
  `dev` start concurrently
- **THEN** exactly one Start succeeds and the other fails naming both environments
- **AND** the refused deployment publishes no configuration
- **AND** the test that verifies this is `TestConcurrentFirstBootRefusesASecondEnvironment`

#### Scenario: A bucket whose policy can evict the identity is refused before minting

- **GIVEN** a configuration bucket created by another writer with a TTL, or with a binding size cap
- **WHEN** Config Manager starts
- **THEN** Start fails naming the bucket and the offending policy value, and creates no `platform_identity` record
- **AND** the deployment never mints a second authority for itself across restarts
- **AND** the tests that verify this are `TestEvictingConfigBucketRefusesStart` and
  `TestIdentityUnderAnEvictingBucketNeverRemints`

#### Scenario: A KV platform write never changes the running authority

- **GIVEN** a running Config Manager whose effective `platform.id` is `dep-7f3a9c`
- **WHEN** another writer puts a `platform` key declaring `platform.id` `other` into the shared bucket
- **THEN** the effective configuration's `platform.id` remains `dep-7f3a9c`
- **AND** the test that verifies this is `TestKVPlatformKeyIsAMirrorNotASource`

## ADDED Requirements

### Requirement: The authority pair is bounded against the value that will be minted

Configuration load SHALL bound the authority pair against the identifier that will actually be minted from it —
the declared pair plus the seven-byte entropy suffix, reserved at load as
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
