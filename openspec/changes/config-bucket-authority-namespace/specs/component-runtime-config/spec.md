# component-runtime-config Delta

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
unchanged after successful Start, except that a key family registered through `WithKeyFamily` adds its watcher. If
the configuration bucket's identity record names a different authority than the configuration declares, Start SHALL
fail before arbitration, watchers, writes, or dependent construction; detached running mode SHALL NOT exist.

Start SHALL reject a nil context before mutating any state or contacting NATS. It is an exported, error-returning,
context-taking boundary, and a nil context reaches the JetStream client as a panic rather than a classified error.

Start SHALL acquire the deployment's configuration bucket under the context passed to Start; no constructor, factory, or
other non-lifecycle boundary SHALL perform that acquisition or invent a context for it. Acquisition SHALL resolve the
bucket through its `framework-bucket-catalog` descriptor rather than a locally spelled bucket configuration, so the
policy is the one the catalog declares whichever writer creates it first. That descriptor's strict retention refuses
a bucket whose policy can delete keys — a nonzero TTL or a binding size cap — naming the offending value, and never
repairs it in place: a create-once identity under an evicting policy expires and is reminted as a second authority,
which ADR-102 decision 7 forbids ever reconciling. Nothing SHALL be minted or created before that check passes.

The acquired handles SHALL NOT become usable by the exported write methods until Start has completed successfully.
Every Start that returns an error — a refused retention policy, a foreign identity, a pre-identity bucket, a
malformed record, or a failure to open watchers — SHALL leave `PushToKV`, `PutComponentToKV`,
`DeleteComponentFromKV` and every registered key family's writes returning the not-acquired lifecycle error.
Publishing the handles at acquisition instead would let a caller overwrite the very bucket Start had just refused as
another platform's, which is the detached running mode this requirement says does not exist.

Before arbitration, Start SHALL establish the deployment's platform identity from the bucket's `platform_identity`
record, deciding from a single pre-mint read of the bucket's keys and under the context passed to Start:

- the record is present — Start SHALL adopt its identifier as the effective `platform.id`, and SHALL fail unless the
  record's organization equals the configuration's `platform.org` and the configuration's `platform.id` equals the
  record's stem. Configuration declares the STEM and only the stem. A configuration declaring a minted identifier
  names that identifier's own bucket and is a new deployment there; where that bucket's record carries the declared
  value as its minted identifier, Start SHALL refuse with guidance naming the stem to declare instead — decided by
  comparison against the recorded identifier, never by inspecting the value's grammar. An adopted identifier SHALL be
  validated under the same segment grammar and authority-pair bound as a configured one;
- the record is absent and the bucket holds no other key — Start SHALL mint the entropy suffix, write the record with
  an atomic `Create`, and adopt the result; if that `Create` conflicts with a concurrent process, Start SHALL re-read
  the record and adopt the winner's identifier rather than its own;
- the record is absent and the bucket holds other keys — Start SHALL fail, naming that the bucket predates identity
  minting and instructing fresh storage. It SHALL mint nothing and SHALL create nothing.

`platform.environment` SHALL NOT separate deployments: no bucket name, key, guard, or comparison reads it, and it
remains a startup log label only. Two deployments declaring the same `platform.org` and `platform.id` are one
deployment to the framework and share its configuration.

The record SHALL carry exactly the fields `org`, `stem`, and `id`. First-boot detection SHALL ignore the
`platform_identity` key, so a boot that has just created it is still a first boot. The identity guard SHALL compare
the effective identifier. Configuration synchronization SHALL NOT apply the KV `platform` key to the running
configuration — it remains a published mirror only — and version arbitration SHALL never write, overwrite, or apply
`platform_identity`.

#### Scenario: Foreign platform identity fails before publication is available

- **GIVEN** the configuration bucket's identity record names another platform authority
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

- **GIVEN** a configuration bucket whose `platform_identity` records organization `acme`, stem `dep`, and identifier
  `dep-7f3a9c`
- **WHEN** a process whose file declares `platform.id` `dep` starts, and concurrently a second process with the same file starts
- **THEN** both adopt `dep-7f3a9c` and neither creates a second record — the loser of the atomic Create reads the winner's
- **AND** when that record is in the bucket a file declaring `other`, or a different `platform.org`, names — by hand or
  through an alias — Start returns the identity mismatch
- **AND** when it is in the bucket a file declaring `platform.id` `dep-7f3a9c` names, Start refuses with guidance to
  declare `dep`
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

- **GIVEN** a deployment established against its configuration bucket with `platform.environment` `prod`
- **WHEN** a deployment declaring the same `platform.org` and `platform.id` with `platform.environment` `dev` starts
- **THEN** Start succeeds and adopts the recorded identifier: `platform.environment` is a log label, so the second
  process joins the established deployment and never establishes a separate one
- **AND** the test that verifies this is `TestEnvironmentDoesNotSeparateDeployments`

#### Scenario: A bucket whose policy can evict the identity is refused before minting

- **GIVEN** a configuration bucket created by another writer with a TTL, or with a binding size cap
- **WHEN** Config Manager starts
- **THEN** Start fails naming the bucket and the offending policy value, and creates no `platform_identity` record
- **AND** the deployment never mints a second authority for itself across restarts
- **AND** the tests that verify this are `TestEvictingConfigBucketRefusesStart` and
  `TestIdentityUnderAnEvictingBucketNeverRemints`

#### Scenario: A refused Start leaves no writer armed

- **GIVEN** a configuration bucket whose identity record names a foreign authority
- **WHEN** Config Manager starts against it and Start refuses the foreign identity
- **THEN** `PushToKV`, `PutComponentToKV`, `DeleteComponentFromKV` and a registered key family's `Put` each return the
  not-acquired lifecycle error
- **AND** the bucket's contents are unchanged, key for key and value for value
- **AND** the test that verifies this is `TestRefusedStartDisarmsEveryExportedWriter`

#### Scenario: Start rejects a nil context without side effects

- **GIVEN** a constructed Config Manager
- **WHEN** Start is called with a nil context
- **THEN** it returns an invalid-configuration error rather than panicking
- **AND** no shutdown channel is replaced, no bucket is created, and no handle is acquired
- **AND** the test that verifies this is `TestStartRejectsNilContextWithoutSideEffects`

#### Scenario: A KV platform write never changes the running authority

- **GIVEN** a running Config Manager whose effective `platform.id` is `dep-7f3a9c`
- **WHEN** another writer puts a `platform` key declaring `platform.id` `other` into the configuration bucket
- **THEN** the effective configuration's `platform.id` remains `dep-7f3a9c`
- **AND** the test that verifies this is `TestKVPlatformKeyIsAMirrorNotASource`

## ADDED Requirements

### Requirement: The configuration bucket is named by the declared authority pair

Config Manager SHALL acquire the configuration bucket named `semstreams_config_<org>_<stem>`, where `<org>` is the
configuration document's `platform.org` and `<stem>` is its declared `platform.id`, both as declared before any mint.
The name SHALL be derived by exactly one function, `config.BucketName`, which SHALL refuse an empty part, a byte
outside the NATS bucket grammar `[A-Za-z0-9_-]`, and a result longer than 252 bytes. Config Manager SHALL derive the
name at construction, so the minted identifier never participates in it. No component SHALL learn the name: the
configuration bucket's other writers reach it only through a key family registered with Config Manager.

Two different declared pairs on one NATS server SHALL therefore use different buckets, and two processes declaring
the same pair SHALL share one. A pair whose name aliases another's — `_` is legal inside both parts — reaches the
other's identity record and is refused by the identity guard at Start; it is not prevented.

#### Scenario: different pairs use different buckets and the same pair shares one

- **GIVEN** a configuration declaring `acme`/`dep` and another declaring `acme`/`other` on one NATS server
- **WHEN** both start, and then a second process declaring `acme`/`dep` starts
- **THEN** the first two establish in `semstreams_config_acme_dep` and `semstreams_config_acme_other` respectively
- **AND** the second `acme`/`dep` process adopts the first's recorded identifier from the same bucket
- **AND** the test that verifies this is `TestBucketIsNamedByTheDeclaredPair`

#### Scenario: every declarable pair names a legal bucket

- **GIVEN** any `platform.org` and `platform.id` that configuration validation admits
- **WHEN** `config.BucketName` derives the name
- **THEN** it returns a name matching `^[A-Za-z0-9_-]+$` of at most 252 bytes
- **AND** an empty part or a byte outside that grammar returns an error
- **AND** the tests that verify this are `TestBucketNameIsLegalForEveryValidPair` and `FuzzBucketName`

### Requirement: Config Manager delivers a registered key family to its owner

`config.NewConfigManager` SHALL accept `WithKeyFamily`, which registers a key family `<prefix>.*` in the
configuration bucket with a handler. Start SHALL open that family's watch with the other configuration watchers and
before the handles are published, and SHALL fail when it cannot. After Start succeeds the manager SHALL deliver to the
handler every entry present when the watch opened, marked initial, and then every put and delete as it happens, each
with its member name, value, and operation. The family's reads and writes SHALL be scoped to `<prefix>.<name>` and
SHALL return the not-acquired lifecycle error until Start has succeeded. Stop SHALL end delivery before it returns.

The rule engine's `rules.*` family SHALL be registered this way, by the composition root, which owns the one rule
`ConfigManager`. That manager SHALL serve rule CRUD through the family. After every service has started it SHALL seed
each rule processor's loaded rules with create-if-absent, then reconcile the full `rules.*` set into each processor
through `ApplyConfigUpdate`, debouncing later changes. It SHALL stop before the services stop. No rule processor SHALL
acquire the configuration bucket itself.

#### Scenario: a registered family receives its snapshot and its changes

- **GIVEN** a configuration bucket holding `rules.a`
- **WHEN** Config Manager starts with a `rules` family, and then `rules.b` is put and `rules.a` is deleted
- **THEN** the handler receives `a` marked initial, then `b` as a put and `a` as a delete, and no key outside `rules.*`
- **AND** before Start, and after a refused Start, the family's `Put` returns the not-acquired lifecycle error
- **AND** the test that verifies this is `TestKeyFamilyDeliversSnapshotThenChanges`

#### Scenario: a rule written through the root's manager reaches the running processor

- **GIVEN** a running rule processor wired by the composition root
- **WHEN** `rules.x` is saved through the root's rule `ConfigManager`
- **THEN** the processor's `ApplyConfigUpdate` receives a rule set containing `x`
- **AND** the test that verifies this is `TestRootRuleManagerHotReloadsIntoTheProcessor`
