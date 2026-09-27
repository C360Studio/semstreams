# Design: the configuration bucket is named by the authority pair

Change `config-bucket-authority-namespace` · issue #1188 (milestone `v1.0.0-beta.163`, gates the tag) · claim PR
#1404. Pins are at base `fe9482b7`. Every choice below is either ruled (quoted) or recorded as a decision with its
reason.

## 0. Rulings, verbatim

- 2026-09-01 (issuecomment-5500899624): *"retire it — the bucket name is the separation."* **"Bucket =
  `semstreams_config_<org>_<stem>`, looked up by the same."** Retire, together: *"the gh#459 identity guard
  (`manager.go:335-352`)"*, *"`platformIdentityTuple` (`manager.go:1310-1312`)"*, *"**`claimEnvironment`
  (`manager.go:1055-1105`) and `platformEnvironmentGuardKey`** — new to this issue's scope"*, *"**`platform.Environment`
  as a discriminator.** It survives only as a startup log label, or is deleted."*
- 2026-09-27, Q1 (issuecomment-5856316438): *"rules do need to support hot reload. why not just make config manager
  capable of watching a buck[et]. functional option?"*, then *"agree - go for it"*. The transcribed ruling reads:
  "**Q1 → (d), replacing (b):** the framework `config.Manager` gains a functional option that adds a key family to its
  watcher list and delivers each entry (key, value, operation, plus the initial snapshot the manager already takes
  before watching) to a handler. `rules.*` is registered that way. The rule package's `ConfigManager` exists ONCE, in
  the composition root, built from the framework manager's acquired bucket … No component learns the bucket name; the
  name lives only in `config.Manager`, derived from the pair. No `component.Dependencies` field."
- 2026-09-27, Q2 and Q3 (issuecomment-5856267253): *"as recommended once we get q1 sorted"*. Q2 (a): keep the ruled
  format and add no code. Q3: the 2026-09-01 retire list stands.

## 1. Premises (measured at `fe9482b7`)

| # | Premise | Pin |
|---|---------|-----|
| P1 | The bucket name is one catalog literal; `SpecFor` matches names exactly | `graph/constants.go:73`, `graph/kvcatalog.go:188-195` |
| P2 | After Start, the running config's `platform.id` is the minted identifier, not the stem | `config/manager.go:1247-1255`, `internal/boot/run.go:190,453-459` |
| P3 | The rule component builds its own manager and acquires the bucket by literal name | `processor/rule/processor.go:1019-1028`, `processor/rule/kv_config_integration.go:540-581` |
| P4 | The root's CRUD manager is a second instance of the same type | `internal/boot/run.go:710-718` |
| P5 | Config Manager's watch list is fixed; `rules.*` is not on it | `config/manager.go:404-410` |
| P6 | The ComponentManager composes one immutable boot component set; rule processors are enumerable after `configureAndCreateServices` | `service/component_manager_boot_only_integration_test.go:5`, `service/rule_pack_bind.go:20-39` |
| P7 | `StopAll` stops services in reverse registration order, then the root closes the config manager | `service/service_manager.go:856-859`, `internal/boot/run.go:620-632` |
| P8 | Bucket grammar is `^[a-zA-Z0-9_-]+$`; the stream name `KV_<bucket>` is limited to 255 bytes | nats.go v1.52.0 `jetstream/kv.go:501,901-906`; nats-server v2.12.4 `server/jetstream_api.go:381` |
| P9 | An org or id segment is ASCII alphanumeric first, then alphanumeric, `_` or `-`; a declared pair is at most 163 bytes | `pkg/types/entity_id.go:243-249`, `config/config.go:808-819` |
| P10 | The e2e harness already holds the declared `org.stem` and splits it | `test/e2e/config/tier_authority.go:97-101` |

## 2. Decisions

**D1 — the option (step 1).** It lives in `config/key_family.go` *(new)*:

```go
type ManagerOption func(*Manager)
func NewConfigManager(cfg *Config, natsClient *natsclient.Client, logger *slog.Logger, opts ...ManagerOption) (*Manager, error)
func WithKeyFamily(family *KeyFamily) ManagerOption
func NewKeyFamily(prefix string, handle func(KeyFamilyEntry)) (*KeyFamily, error)
type KeyFamilyEntry struct { Name string; Value []byte; Operation KeyFamilyOperation; Initial bool }
func (f *KeyFamily) Get / Put / Create / Delete / Names
```

- Start opens `<prefix>.*` without `UpdatesOnly`, beside the fixed patterns and before the handles are published. If
  it cannot open that watch, Start fails: a registered family is a declared dependency, not an optional pattern.
- The family's store is bound where the handles are published (`publishBucket`). A refused Start therefore leaves
  the family's CRUD returning the not-acquired error, exactly as the manager's own writers do.
- The goroutine joins `wg`, like the fixed watchers.
- Entries present when the watch opened arrive with `Initial` set. After the nil marker, every entry is live.
- Put maps to `KeyFamilyPut`; Delete and Purge map to `KeyFamilyDelete`.
- The handler runs on the watch goroutine and must not block. That is documented on `NewKeyFamily`.
- Why a family object rather than a bucket accessor: the CRUD it serves is scoped to `<prefix>.<name>`. A caller
  holding it cannot reach `platform_identity`, and the bucket's name never leaves `config`.

**D2 — one rule manager, owned by the root.**
- `rule.NewConfigManager(logger)` creates the `rules` family with itself as handler. `KeyFamily()` returns it, and
  the root passes it through `StartValidatedConfigManager(..., config.WithKeyFamily(...))`.
- CRUD (`SaveRule`, `GetRule`, `DeleteRule`, `ListRules`) goes through the family.
- The handler only wakes the reconcile loop (non-blocking, buffer of one). Reconcile stays a full `ListRules`
  replace, so it needs no per-entry state.
- `Start(ctx, targets []HotReloadTarget)` seeds, reconciles once, then debounces at 250 ms as before. `Stop()` cancels and
  joins.
- `HotReloadTarget` is the small interface the processor exposes: `LoadedRuleDefinitions()` *(new, on `*Processor`)*,
  `ValidateConfigUpdate`, `ApplyConfigUpdate`.

**D3 — ordering (the brief asked for it to be recorded).**
- **Seed after `StartAll`, then reconcile, and stop the loop before `StopAll`.** `internal/boot` wraps its
  `runtimeManager`: `StartAll` delegates and then calls `rules.Start(ctx, targets)`, and `StopAll` calls
  `rules.Stop()` and then delegates.
- Why after `StartAll`: that is where reconcile ran before, inside each processor's own `Start`
  (`processor/rule/processor.go:1019-1028`), after subscriptions and the cron scheduler exist. Seeding first means the
  first full-replace reconcile sees the file rules. Reconciling into an empty bucket would remove them.
- Why before `StopAll`: each processor used to stop its own hot-reload manager inside `Stop`
  (`processor/rule/processor.go:1335`). Stopping the loop first keeps any reconcile from racing a processor's teardown.
- Entries delivered before `rules.Start` only leave a wake-up pending. The first reconcile lists the bucket fresh, so
  nothing is lost and the pending wake costs one extra reconcile.
- Targets are the enabled rule processors after `configureAndCreateServices` (P6). The root finds them with
  `service.ComponentsImplementing[rule.HotReloadTarget]` *(new)*. It is the generic form of the existing
  `ProjectionBinders` walk, and this is its one consumer. With more than one processor, each receives the full
  `rules.*` set, which is what each processor's own watcher did before.
- The runtime holds the rule manager through a two-method `ruleHotReload` interface, so
  `TestRuleHotReloadRuntimeOrdersAroundServices` can pin the order without NATS.

**D4 — the name (step 2).**
- `config.BucketName(org, stem string) (string, error)` returns `graph.BucketSemStreamsConfig + "_" + org + "_" +
  stem`. It refuses an empty part, a byte outside P8's grammar, or a result longer than 252 bytes. It has a fuzz
  target.
- `NewConfigManager` derives the name from the document's pair before anything is minted. Holding it on the manager
  is what keeps the stem out of P2's reach.
- For every pair `Validate` admits (P9), the result is legal at no more than 18 + 163 + 1 = 182 bytes. The bound check
  is for callers outside `config` (the e2e harness) and fails closed.

**D5 — the catalog name family.**
- The row keeps `Name: BucketSemStreamsConfig`. An unexported graph set marks that row as a family:
  `SpecFor("semstreams_config_<suffix>")` returns a copy of the row with `Name` set to the concrete name.
  `SpecFor("semstreams_config")` alone returns false; the orphaned bucket is no longer framework state.
- Every consumer of `SpecFor` inherits the family: acquisition, `IsFrameworkOwnedBucket`, `OwnerOf`, and
  `FrameworkOwnedWriteRefusal`. So `update_kv` stays refused into any pair's bucket at load, at runtime, and at writer
  acquisition.
- `EnsureCatalogRetentionClean` skips strict rows (`graph/owned_bucket_retention.go:57`), so it is untouched.
- The owner string changes to name `config.Manager` as the one acquirer, with `rules.*` written through its key
  family.
- The catalog Purpose says an adopter does not select "bucket identity". The adopter still selects no name, knob or
  descriptor. The name is derived from the authority the adopter already declares, and the Purpose is left as is.

**D6 — the retire list.**
- Deleted: the gh#459 block (`config/manager.go:321-353`), `kvPlatformIdentity`, `platformHasIdentity`,
  `platformIdentityTuple`, `claimEnvironment`, `platformEnvironmentGuardKey` and its first-boot exclusion.
- The pre-identity refusal (`config/manager.go:1030-1043`) is re-read. The bucket is no longer "a fixed global name",
  and `processor/rule` no longer creates it, so the message names the one remaining cause (the bucket predates
  identity minting) and a hand-written key.
- The adoption mismatch (`config/manager.go:1215-1221`) stays, reworded. Its only remaining trigger is Q2's alias, or
  a record written by hand.
- `platform.Environment` stays a field; its doc says it is a startup log label and separates nothing. Deleting it
  would break every shipped config for no gain, since the ruling allows either.

**D7 — the e2e harness.** `test/e2e/config` gets `PlatformIdentityBucket(declared string) (string, error)`, which
splits `org.stem` and calls `config.BucketName`. `EffectiveAuthority` and the scenarios that read or write the bucket
use it with the pair they already hold. crud-tools derives its rules bucket from `CrudToolsAuthorityStem`, which is
pinned to `configs/flows/crud-tools-test.json` by `TestCrudToolsAuthorityMatchesShippedConfig`. That follows the
existing `CoreAuthorityStem` pattern.

## 3. Residuals (recorded, not filed)

- **R1.** Two deployments declaring the same `(org, stem)`, for example prod and dev, share one bucket and one
  authority. The owner accepted this in the ruling "that is now the operator's declaration to get right". The remedy
  is to declare distinct stems; the migration note says so.
- **R2.** The Q2 alias (org `a_b` + stem `c` against org `a` + stem `b_c`) is refused at Start by the adoption compare
  (`config/manager.go:1197-1221`), not prevented. One sentence in the migration note names it.
- **R3.** With more than one rule processor, every processor receives every `rules.*` rule, which was already true
  before this change. Scoping rules per pack is not this change.
- **R4.** Catalog `Write` policy stays guard-scoped (#1188's 2026-08-31 second constraint). Re-cutting acquisition did
  not make it universal. After this change `config.Manager` is the bucket's only acquirer, which narrows the question
  without answering it.
- **R5 — found during implementation, ruled by the owner on #1188 (docket 2, Q4 (b): "continue with
  recommendations").** A file that declares the *minted* identifier (`platform.id` `dep-7f3a9c`) names bucket
  `semstreams_config_<org>_dep-7f3a9c`, which is empty, so the adoption compare cannot see that the value was minted.
  - Ruled: observation on the mint branch only. Before minting, Start lists the configuration buckets once and reads
    `platform_identity` from each other `semstreams_config_<org>_*` bucket, one Get each, under the Start context, no
    retries. A record with this org and `id` equal to the declared value refuses with the ADR-104 d5 guidance naming
    its stem, and nothing is minted (`config/manager.go` `refuseDeclaredMintedIdentifier`).
  - The prefix is over-inclusive because an org may contain `_`; the recorded org and id are compared, so that is
    harmless. A sibling that is gone or has no record is skipped; any other read failure fails Start closed.
  - ADR-104 d5 stays in force. It is observation, not grammar, so ADR-104's "no shape inspection" holds.
  - Residual: the refused Start has already acquired its (empty) bucket, which stays behind. It holds no record, so a
    later boot declaring that value is refused the same way.
  - Tests: `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance` (two real managers, plus the different-stem
    negative arm); mutation M8.
- **R6.** ADR-104 (`docs/adr/104-unique-platform-authority.md:21,32,109,146`) still spells
  `semstreams_config/platform_identity`. ADRs are history. The record's address now lives in `component-runtime-config`
  and the migration note, and its `{org, stem, id}` shape is unchanged.
