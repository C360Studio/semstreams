# Conformance — federation-identity (Case B, knobless)

Ruling → the `file:line` that implements it. A DEVIATION row carries the owner's recorded sign-off or it is not a
deviation, it is a defect. Measured at `cb88762d` unless noted.

## Owner scope cut, #1168 comment 5471456759 (2026-08-30)

| Ruling | Implemented at | Note |
|---|---|---|
| #1168 is **Case B only**: mint an entropy suffix for `platform.id`, persist it, establish it on every boot, plus item 3 | `config/manager.go` `establishPlatformIdentity`/`mintPlatformIdentity`/`adoptPlatformIdentity`; `docs/proposals/gh1095-entity-id-segment-semantics-design.md` (appended correction) | — |
| Case A → #1192; its spec delta and the graph-ingest DEFERRED paragraphs leave with it | the change's `specs/graph-ingest/` delta is deleted; `openspec/specs/graph-ingest/spec.md:934-948` is untouched on this branch | verified: `git diff origin/main -- openspec/specs/` is empty |
| Import-lane gate → #1194; anchor-append → #1193; env surface → #1186; three deletions → #1187 | nothing in this branch adds `entity.import.lane`, `import_collision`, `platform.Config` fields, or deletes `FederationMeta`/`DeploymentPrefix`/`MinimalConfig` | verified by `git diff origin/main --stat` |
| **The knob is dropped**: no `platform.Config.Unique`, no `mint_suffix`, no `validatePlatformBlockKeys`, no Q4 | `pkg/platform/platform.go` is unchanged; `config/config.go` gains no platform key and no strict-key check | — |
| The opt-out is a pre-created `platform_identity` record with `id == stem`, accepted by the adopt branch | `config/manager.go` `adoptPlatformIdentity`; scenario *an operator-provisioned identity record is adopted unsuffixed* in `specs/entity-id-contract/spec.md`; `TestPreCreatedIdentityRecordIsAdoptedUnsuffixed`; migration obligation 2 | — |
| The adopt branch validates an adopted `id` under the same charset / subject-safety / budget rules as a config value | `config/manager.go` `adoptPlatformIdentity` calls `validateAuthorityPair`, which composes the binding family through `validateEntityIDSegment` | — |

## Architect revision round, PR #1178 (2026-08-30) — Case B blocks only

| Block | Implemented at |
|---|---|
| §1.A revised three-branch mechanism, one pre-mint `kv.Keys` read | `config/manager.go` `establishPlatformIdentity` |
| §1.A collision 1 — delete `case "platform"`; KV `platform` stays a read-only mirror via `PushToKV` | `config/manager.go` `updateConfig` (arm deleted, replaced by the comment naming why); `TestKVPlatformKeyIsAMirrorNotASource`; mutation check M13 |
| §1.A collision 2 — the single pre-mint read replaces `hasKVConfig`'s any-key probe | `config/manager.go` (`hasKVConfig` deleted); `TestBootWithOnlyAnIdentityRecordIsStillAFirstBoot`; mutation check M12 |
| §1.A collision 3 — bound at load the value that WILL be minted (pair + 7 against the EXISTING 170 budget) | `config/config.go` `mintedSuffixBytes` / `maxDeclarableAuthorityPairBytes` / `validateAuthorityPair`; `TestConfigRejectsPairThatOnlyFitsUnsuffixed`; mutation check M14. **The 170→168 tightening is NOT here** — it left with Case A; `pkg/types.MaxAuthorityPairBytes()` still returns 170 |
| §1.A collision 4 — refuse a pre-identity bucket, minting nothing and creating nothing | `config/manager.go` `establishPlatformIdentity` third branch; `TestPreIdentityBucketRefusesStartWithoutMinting`; mutation check M11; e2e stage `validate-pre-identity-bucket-refusal` |
| §1.F e2e stage `validate-pre-identity-bucket-refusal` + landing coverage `TestPreIdentityBucketRefusesStartWithoutMinting` | `taskfiles/e2e/core.yml`; `test/e2e/scenarios/platform_identity.go`; `config/manager_identity_integration_test.go` |
| §1.J ADR-104 narrowed; D4's Case A deletion list stripped; stem-keying, the #1186 constraint, the pre-identity consequence, and the normative `{org, stem, id}` shape kept | `docs/adr/104-unique-platform-authority.md` (renamed from `104-derived-identity-and-unique-authority.md`) |
| §1.K migration doc: LOUD pre-identity refusal + Case B obligations; the silent rule-pack line and the non-Go run-entity table leave with #1192 | `docs/operations/migration-beta162-to-beta163.md` |
| §1.L pins file, `base:` + Case B premises only | `docs/proposals/gh1168-federation-identity-pins.md` — measured `pins=3 ok=3 exit=0` |
| O-10(i) record shape normative in `component-runtime-config` | `specs/component-runtime-config/spec.md`, requirement *Component configuration activates only during process construction* |

## Judgment calls made during implementation (not covered by the cut)

| Call | Why | Where |
|---|---|---|
| The adopt branch also compares the record's `org` | The revision (§2.8) states the mint must be correct **without** the gh#459 guard, which #1188 retires. That guard is the only thing that compared `org`; without an org comparison in adopt, two apps sharing a stem under different orgs would adopt each other's identity once it goes | `config/manager.go` `adoptPlatformIdentity`; scenario and `TestConfigManagerAdoptsPersistedPlatformIdentity/file_declares_another_organization` |
| ~~The suffix reserve applies to **every** pair the framework accepts~~ → **the declaration boundary only** | **WITHDRAWN — this was HIGH-1, and the first implementation was wrong.** `Config.Validate` is shared by load, `SafeConfig.Mutate` and `ValidateEffectiveConfig`, so reserving inside it double-counted: a declared pair of 157–163 bytes loaded and then hard-failed Start. Measured, not argued — `TestMaximumDeclarablePairMintsAndStarts` reproduced it before the fix. The reserve is a fact about a *declaration*: `validateDeclaredAuthorityPair` enforces 163 at `Loader.Load`/`LoadFromBytes` (ungated, like `rejectRemovedPlatformFields`, because the binary's `loadConfig` never enables loader validation), and `validateAuthorityPair` bounds every effective pair at 170. Uniform per KIND; no path sees both kinds | `config/config.go` `validateDeclaredAuthorityPair` / `validateAuthorityPair` / `Load` / `LoadFromBytes` |
| `mintPlatformIdentity` validates the composed pair **before** `Create` | The revision rejected a pre-`Create` *probe* (a KV read) and a rollback (a `Delete`, which d7 forbids). This is neither: it is arithmetic on the value about to be written, and it makes "no record is created that a later boot rejects" a local property instead of an argument about a distant load check. It cannot false-refuse, because load already reserved the bytes | `config/manager.go` `mintPlatformIdentity` |
| A failed bucket read now fails Start instead of assuming first boot | The old `hasKVConfig` tolerated a read error by assuming first boot. Under minting that assumption **creates a second authority** for a deployment that already has one | `config/manager.go` `establishPlatformIdentity` |
| Minting refuses an empty `platform.org`/`platform.id` by name | `Config.Validate` requires both, so this is an unvalidated configuration reaching Start (library consumers can do it). Without the explicit refusal it surfaced as a family-composition error about `"-9ef4a0"` | `config/manager.go` `mintPlatformIdentity` |
| `TierAuthority`/`CoreAuthority`/`TierEntityID` renamed to `…Stem` | They now return the declared stem, not the authority. A name that says "authority" while returning a stem is the same prediction footgun one level up | `test/e2e/config/tier_authority.go` |
| `--lifecycle-seed` takes the last four positions | A compose file can no longer spell the pair at all. The old whole-ID form plus a mismatch guard was the right guard for the wrong shape | `cmd/e2e-semstreams/main.go` `seedMission`; `docker/compose/lifecycle.yml` |

## Can anything change the platform pair after establishment? (asked in the fix round)

No. Enumerated rather than asserted — every writer of a live `*Config`'s platform block:

| Writer | Reach |
|---|---|
| `SafeConfig.Update` (`config/config.go:95`) | **zero production callers**; tests only |
| `SafeConfig.Mutate` → `config/manager.go:553` (`updateConfig`) | the KV apply path. Its switch has arms for `services`, `components`, `nats`, `model_registry` only; the `platform` arm is deleted and unknown keys return `errNoConfigChange`. `TestKVPlatformKeyIsAMirrorNotASource` pins it; mutation check M13 reds it |
| `SafeConfig.Mutate` → `config/manager.go` `applyEffectivePlatformID` | the establishment itself, once, before watchers or writes |
| `SafeConfig.Mutate` → `config/manager.go` `syncFromKV` | resets `Services` only |
| `Loader.applyEnvOverrides` (`config/config.go:748`, `STREAMKIT_PLATFORM_ID`) | runs at LOAD, on a declaration, before Start — never after establishment. Leaves with #1186 |
| the HTTP/API config surface | `PutComponentToKV` / `DeleteComponentFromKV`, both keyed `components.*` |

So the ADR-102 d7 obligation ("never rewrite a minted authority") needs no additional refusal: after
`establishPlatformIdentity` returns, there is no path to the pair. If one is ever added it must refuse rather than
apply, and this table is the enumeration it has to re-open.

## Second writer to the configuration bucket (MEDIUM-2)

`semstreams_config` has a fixed global name and this package is not its only creator:
`processor/rule/kv_config_integration.go:574` creates the same bucket for `rules.*`, and
`cmd/semstreams/main.go:733-736` documents two ConfigManager instances coexisting against it by design. A
rules-seeded fresh bucket therefore makes a genuine first boot take the third branch. The refusal now names both
possible causes and lists the keys it found (`summarizeKeys`); the remedy it prints is correct for both.
`TestPreIdentityBucketRefusesStartWithoutMinting` asserts both the `processor/rule` mention and the key list.

## Codex owner round, 2026-08-31 — per-finding disposition

| Finding | Disposition | `file:line` |
|---|---|---|
| **B1** durable identity can expire and remint under an inherited bucket policy | FIXED. Reproduced first — `TestIdentityUnderAnEvictingBucketNeverRemints` failed `expected "dep-a9dee1", actual "dep-534e17"`, both boots nil, Codex's exact shape. `acquireBucket` now reads the live policy and refuses a TTL or a binding size cap before anything is minted, through the EXISTING owner of that rule (`KVStore.AssertNoLifecycleRetention`) rather than a second spelling of it. Validating what exists — not who created it — is also what covers `processor/rule` having created the bucket first, so that package is untouched | `config/manager.go` `acquireBucket`; `natsclient/kv.go` `AssertNoLifecycleRetention`; tests `TestEvictingConfigBucketRefusesStart`, `TestIdentityUnderAnEvictingBucketNeverRemints`; mutation check M16 |
| **B2** concurrent first boot bypasses the environment guard; both apps publish | FIXED. Reproduced first — `TestConcurrentFirstBootRefusesASecondEnvironment` failed `expected: 1 actual: 2` with both errors nil, matching Codex's 10/10. `claimEnvironment` claims the bucket for one `platform.environment` by atomic `Create` on an internal key, BEFORE the record, so a failure between the two leaves a state a same-environment boot completes; a mismatch refuses naming both. The record's `{org, stem, id}` shape is unchanged — the guard is not a field of it | `config/manager.go` `claimEnvironment`, `platformEnvironmentGuardKey`; mutation check M17 |
| **B3** the load boundary rejects a full identifier the adopt contract accepts | FIXED by removing the contradiction rather than detecting a minted value by grammar: configuration declares the STEM, and only the stem. The full-identifier arm is gone from the adopt comparison, the spec scenario, and the adopt test's cases; a file holding the minted identifier is refused with guidance derived from the STORED value. ADR-104's "no path sees both kinds" is now true because the field admits one kind | `config/manager.go` `adoptPlatformIdentity`; `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance`; mutation check M18 |
| **B4** constructor invents a root context for I/O | FIXED as removal work, not inherited debt. `NewConfigManager` performs no I/O and retains the `*natsclient.Client`, never a context; `Start(ctx)` acquires the bucket, validates its policy, claims the environment, and establishes identity under that exact context. Bucket-dependent methods called before Start return `errBucketNotAcquired` rather than dereferencing nil | `config/manager.go` `NewConfigManager`, `acquireBucket`, `store`, `bucket`, `errBucketNotAcquired` |
| **B5** knobless contract selected before the owner ruled | NOT A CODE CHANGE — snapshot race. The owner's confirmation landed on #1168 at 01:41Z, five minutes before the Codex comment at 01:46Z. The coordinating session answers it on the PR; ADR-104 stays Proposed until the owner accepts it (task 5.6, still open) | — |
| **MEDIUM** task truth carries pre-fix and placeholder claims | FIXED. 1.1 no longer claims `implemented-by` was set at implementation and points at 6.6 for later body changes; 3.1 rewritten to the shipped two-boundary contract and its four tests | `openspec/changes/federation-identity/tasks.md` 1.1, 3.1 |

## Codex re-review, 2026-08-31 — per-finding disposition

| Finding | Disposition | `file:line` |
|---|---|---|
| **B6** a failed Start left every exported KV writer armed | FIXED. Reproduced first — after a refused foreign-identity Start, `PushToKV` returned nil. The handles now stay local through every Start step that can refuse and reach the struct only through `publishBucket`, called immediately before Start returns success; that placement covers the watcher-open failure too, not just identity. `errBucketNotAcquired` is now truthful for "attempted and refused", which is the case that mattered | `config/manager.go` `acquireBucket`, `publishBucket`, `Start`; private `pushToKV`/`syncFromKV`/`getKVVersion`/`kvPlatformIdentity` variants that take an explicit handle; `TestRefusedStartDisarmsEveryExportedWriter`; mutation check M20 |
| **B7** the retention guarantee bypassed the framework bucket catalog | FIXED. Reproduced mechanically: adding the descriptor row made the EXISTING literal contract test bite, naming both direct creators (`config/manager.go:76`, `processor/rule/kv_config_integration.go:574`) — the omission had passed only because no row existed. `semstreams_config` is now a catalog descriptor (operational, open-write because two subsystems write it, History 5) and both creators resolve it. Its retention kind is a NEW `RetentionNoLifecycleStrict`: the generic seam reconciles, which ADR-104 forbids here, so the descriptor grammar was extended rather than the rule bent | `graph/constants.go` `BucketSemStreamsConfig`; `graph/kvcatalog.go` `semstreamsConfig`; `natsclient/kvspec.go` `RetentionNoLifecycleStrict`; `config/manager.go` `acquireBucket`; `processor/rule/kv_config_integration.go` `ensureKVStore`; `TestSharedConfigBucketResolvesThroughOneDescriptor`; mutation check M22 |
| **B8** Start did not reject a nil context | FIXED. Reproduced first — the panic Codex named, in `jetstream.(*jetStream).wrapContextWithoutDeadline`, after `shutdownCh` had already been replaced. The guard is the house shape (`processor/rule/kv_config_integration.go:91-94`) and sits above every mutation and every NATS touch | `config/manager.go` `Start`; `TestStartRejectsNilContextWithoutSideEffects`; mutation check M21 |
| **MEDIUM** task 3.2 still recorded the constructor root as untouched debt | FIXED — rewritten to the shipped Start-context behaviour, and it now names why the old text was wrong | `openspec/changes/federation-identity/tasks.md` 3.2 |

### Why `processor/rule` is in scope (it was explicitly out for MEDIUM-2)

The earlier round's disposition — validate what exists rather than touching the other creator — was right for the
question it answered (whose bucket policy is in force). B7 asks a different question: which descriptor governs the
bucket. The catalog contract's answer is "exactly one", and a second creator spelling its own
`jetstream.KeyValueConfig` forks the policy no matter who validates afterwards. The change to `processor/rule` is
the acquisition seam only; its behaviour, retry classification, and handle publication are untouched.

### Constructor-then-use callers (B4 enumeration)

| Caller | Uses the bucket before Start? |
|---|---|
| `internal/bootstrapobservability/bootstrap.go` `StartConfigManager` — the only production caller | No. Constructs, calls `Start(ctx)` immediately, then `GetConfig()`, which reads in-memory state only |
| `processor/rule.NewConfigManager(nil, configMgr, logger)` (`cmd/semstreams/main.go`, `cmd/e2e-semstreams/main.go`) | No. It retains `configMgr` and, per its own field comment, no longer uses it; it acquires its own bucket |
| in-package tests that seed the bucket before Start | Yes, deliberately. They now open the bucket themselves (`directKVStore`) or call `acquireBucket(ctx)` — the same production path Start uses — instead of reaching into a handle the constructor had opened behind them |

## Test fixtures changed because the refusal is the feature

Every fixture that fabricated an "already configured" bucket without an identity record now seeds one; that refusal
is the change, not a regression. `config/manager_test.go`, `config/manager_integration_test.go`,
`cmd/semstreams/bootstrap_observability_integration_test.go`,
`internal/bootstrapobservability/config_manager_integration_test.go`. Two more moved for reasons named in their
comments: the gh#459 regression test now asserts the refusal that comes from the identity record one branch earlier,
and `service/component_manager_boot_findings_integration_test.go` declares stream bounds because Start now validates
the effective configuration when it applies the established identity.

## Second narrow re-review (2026-08-31, `42628d4a` → `bfeecdcb`) — dispositions

All four Codex blockers verified CLOSED by measurement (M16/M17/M18 spot-checked via overlay; MaxBytes class
confirmed covered; crash windows and the legacy `{record, no guard}` bucket probed — retroactive claim, safe).
Three MEDIUMs, closed at `c5527927`+: (1) task 2.2 rewritten to the shipped stem-only contract; (2) the guard-key
first-boot exclusion was unguarded — the test now seeds `{guard, record}` and mutation M19 pins it; (3) the
cross-environment refusal and migration obligation 7 now name the single-deployment rename cause. Accepted, not a
finding: `ErrGraphBucketRetention` keeps its name (renaming an exported sentinel costs adopters more than the
comment mismatch). Reset-slip incident independently verified: both coordination commits ancestors of head,
all five closures present.
