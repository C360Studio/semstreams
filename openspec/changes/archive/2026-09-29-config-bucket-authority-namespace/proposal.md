# Change: the configuration bucket is named by the authority pair

## Why

Every sem* app on one NATS server shares one configuration bucket, because its name is a fixed literal
(`graph/constants.go:73`). The gh#459 guard (`config/manager.go:322-352`) catches a second app at boot by comparing
the stored `platform` key, and the environment claim (`config/manager.go:1055-1105`) catches a second environment.
The comment on the guard already names the real fix: "per-platform bucket namespacing is the complete fix for that
case" (`config/manager.go:332-333`).

Owner, 2026-08-30 (#1188): *"why not prefix that bucket with org and platform id and be done with it?"* Owner ruling,
2026-09-01 (issuecomment-5500899624): **"Bucket = `semstreams_config_<org>_<stem>`, looked up by the same."** It
retires the gh#459 guard, `platformIdentityTuple`, `claimEnvironment` with `platformEnvironmentGuardKey`, and
`platform.Environment` as a discriminator: *"retire it — the bucket name is the separation."* Placement is
`v1.0.0-beta.163` (2026-09-01, confirmed 2026-09-23 issuecomment-5797927036). That is the only tag in which an
upgrading deployment mints its authority exactly once.

A per-deployment name leaves one writer unable to find the bucket. The rule component builds its own config manager
from the NATS client alone (`processor/rule/processor.go:1020-1028`). It knows only the minted identifier, never the
declared stem. Owner ruling, 2026-09-27 (issuecomment-5856316438), quoting the owner: *"rules do need to support hot
reload. why not just make config manager capable of watching a buck[et]. functional option?"* and then *"agree - go
for it"*. The ruling is Q1 (d): no `component.Dependencies` field. `config.Manager` gains a functional option that
adds a key family to its watchers. The rule package's `ConfigManager` exists once, in the composition root.
Q2 (a) and Q3 were confirmed the same day (issuecomment-5856267253): keep the ruled format and add no injectivity code;
the 2026-09-01 retire list stands.

## What changes

1. **Watcher ownership (step 1).**
   - `config.NewConfigManager` accepts `...ManagerOption`. `config.WithKeyFamily(*config.KeyFamily)` adds `<prefix>.*`
     to the watchers. Start delivers the entries present when the watch opens, marked initial, then every put and
     delete, to the family's handler.
   - Once Start succeeds, the family's scoped CRUD reaches the acquired bucket. No handle leaks and no name leaks.
   - The rule `ConfigManager` is constructed once, in `internal/boot`. It serves CRUD to the agent tools through the
     `rules` family and is that family's handler.
   - It runs as the registered `rule-config` service, registered after the component manager. Once the rule
     processors have started, it seeds file rules from each and reconciles `rules.*` into each through
     `ApplyConfigUpdate`; its reconcile loop stops, within the shutdown context, before they stop.
   - The component-internal manager, `InitializeKVStore`, and the rule package's own acquisition of the bucket are
     deleted.
2. **The name (step 2).**
   - `config.BucketName(org, stem)` is the one derivation. `config.Manager` computes it from the configuration
     document's pair at construction, before any mint.
   - The catalog row becomes a name family: `SpecFor` resolves every `semstreams_config_<suffix>`. Every `update_kv`
     owner-only guard therefore still refuses the bucket, for any pair.
   - Retired: the gh#459 guard and its refusal, `platformIdentityTuple`, `claimEnvironment`, the
     `platform_identity_guard` key, and `platform.Environment` as a discriminator. It stays a startup log label.
   - The ADR-104 adoption compare stays. Its message is reworded, since an alias (org `a_b` + stem `c` against org
     `a` + stem `b_c`) is the only way a foreign record now reaches it.
   - The e2e harness derives the name from the declared pair it already holds.
3. **Migration note.** `docs/operations/migration-beta162-to-beta163.md` gains the section and amends the ADR-104
   obligations that spell the old name.

**BREAKING:**
- The configuration bucket's name changes. The old `semstreams_config` is orphaned, not migrated.
- `platform.environment` no longer separates deployments.
- `rule.NewConfigManager` loses its processor, config-manager and NATS parameters, and `InitializeKVStore` is removed.

## Impact

- Specs: `component-runtime-config` (MODIFIED "Component configuration activates only during process construction";
  ADDED key-family and bucket-name requirements), `framework-bucket-catalog` (MODIFIED shared configuration bucket),
  `entity-id-contract` (MODIFIED cloned-template requirement, whose scenario spells the record's address).
- Code: `config/`, `graph/kvcatalog.go`, `processor/rule/`, `internal/boot/`, `internal/bootstrapobservability/`,
  `test/contract/`, `test/e2e/config/`, `test/e2e/scenarios/`, `cmd/e2e/`.
- Sisters (read-only; sizing for the migration note): references to `semstreams_config` in semsource 45,
  semconnect 26, semspec 13, semdev 4, semteams 4, semboids 1, semdragon 1. semteams calls
  `rulepkg.NewConfigManager(nil, configMgr, logger)` (`cmd/semteams/main.go:550`).
