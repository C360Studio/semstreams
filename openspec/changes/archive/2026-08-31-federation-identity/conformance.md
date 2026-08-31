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
| **B7** the retention guarantee bypassed the framework bucket catalog | FIXED. Reproduced mechanically: adding the descriptor row made the EXISTING literal contract test bite, naming both direct creators (`config/manager.go:76`, `processor/rule/kv_config_integration.go:574`) — the omission had passed only because no row existed. `semstreams_config` is now a catalog descriptor (operational, History 5; write policy settled later in the round — see the ruling table) and both creators resolve it. Its retention kind is a NEW `RetentionNoLifecycleStrict`: the generic seam reconciles, which ADR-104 forbids here, so the descriptor grammar was extended rather than the rule bent | `graph/constants.go` `BucketSemStreamsConfig`; `graph/kvcatalog.go` `semstreamsConfig`; `natsclient/kvspec.go` `RetentionNoLifecycleStrict`; `config/manager.go` `acquireBucket`; `processor/rule/kv_config_integration.go` `ensureKVStore`; `TestCatalogBucketNamesAreNeverAcquiredDirectly`; mutation check M22 |
| **B8** Start did not reject a nil context | FIXED. Reproduced first — the panic Codex named, in `jetstream.(*jetStream).wrapContextWithoutDeadline`, after `shutdownCh` had already been replaced. The guard is the house shape (`processor/rule/kv_config_integration.go:91-94`) and sits above every mutation and every NATS touch | `config/manager.go` `Start`; `TestStartRejectsNilContextWithoutSideEffects`; mutation check M21 |
| **MEDIUM** task 3.2 still recorded the constructor root as untouched debt | FIXED — rewritten to the shipped Start-context behaviour, and it now names why the old text was wrong | `openspec/changes/federation-identity/tasks.md` 3.2 |

## Third narrow re-review, 2026-08-31 — per-finding disposition

| Finding | Disposition | `file:line` |
|---|---|---|
| **HIGH** `WriteOpen` left the identity record writable by a generic rule action | FIXED by flipping the descriptor to `WriteOwnerOnly`. Reproduced first at BOTH guards and BOTH keys: `ValidateDefinition` and `executeUpdateKV` each returned nil for `semstreams_config/platform_identity` and `/platform_identity_guard`. My earlier stated cost — "would change rule behaviour" — was **wrong, and I verified that myself rather than accepting it**: `IsFrameworkOwnedBucket` has exactly two callers, both `update_kv` guards (`processor/rule/actions.go:2188`, `processor/rule/config_validation.go:369`); neither `config.Manager` nor `processor/rule.ConfigManager` consults it, so their own writes cannot be affected. The only shipped `update_kv` bucket literal anywhere in `configs/`, `test/` or `docker/` is `RESEARCH_EVIDENCE`, which is not catalogued and stays admitted | `graph/kvcatalog.go` `semstreamsConfig`; `TestUpdateKV_RejectsSharedConfigBucket_AtLoad`, `…_AtRuntime`, `TestUpdateKV_StillAdmitsResearchEvidence`; mutation check M23 |
| **MEDIUM** a third acquisition path invisible to both guards | FIXED. `natsKVWriter.getStore` bound whatever bucket a rule pack resolved to, with its own config, refuting the new SHALL. It now refuses a catalogued owner-only name outright and resolves any other catalogued name through `graph.EnsureCatalogBucket`; non-catalogued names keep the previous behaviour, so `RESEARCH_EVIDENCE` is untouched. The contract test's enumeration is now DERIVED by scanning production packages rather than hand-listing two files — and the derived scan found `kv_writer.go` by itself, which is the reproduction | `processor/rule/kv_writer.go` `acquireBucket`; `test/contract/shared_config_bucket_acquisition_contract_test.go`; mutation check M24 |
| **NIT** the strict-retention refusal was re-wrapped as transient | FIXED. A permanent invalid-configuration state told a retry loop to keep trying; the wrap now preserves `errs.IsInvalid` and only classifies genuine acquisition faults as transient | `processor/rule/kv_config_integration.go` `ensureKVStore` |
| **cosmetic** section 3 of `tasks.md` read 3.1, 3.5, 3.4, 3.2, 3.3 | FIXED — reordered | `openspec/changes/federation-identity/tasks.md` |

### Owner ruling: `WriteOwnerOnly` (2026-08-31)

> concur with option 1

#1168 comment 5479005060, transcribed verbatim. The fork — bucket-wide owner-only versus write-open with a
key-level refusal for the identity pair — was put to the owner by the Codex re-review at `768a5333`, with the
measured brief: zero in-tree rule packs write `semstreams_config`; the shipped `update_kv` consumer
`RESEARCH_EVIDENCE` is uncatalogued and untouched; neither ConfigManager consults the write predicate.

**Ruled contract:** the `semstreams_config` catalog descriptor is `WriteOwnerOnly`, and the rule engine's generic
`update_kv` refuses **every key** in the bucket — at load validation, at action runtime, and at writer acquisition.
Configuration changes go through the config manager/API; a rule pack that needs to influence configuration is an
engine-gap conversation, not a raw KV write.

The implementation already on the branch is what was ruled, so no code changed for the ruling; it propagated into
ADR-104 decision 6, `graph/constants.go`, the catalog census test, the migration note, and the record below. The
option-2 alternative is retained beneath as superseded history, not deleted — a reader who wonders why the bucket is
not key-scoped should find the answer, not a gap.

### Decision record: `WriteOwnerOnly` over key-level refusal (SUPERSEDED BY THE RULING ABOVE — retained as history)

Two shapes close the forgery hole:

1. **Chosen — flip the descriptor to `WriteOwnerOnly`.** Closes it through the guard predicate that already exists,
   at both load and runtime, with no new code path. It states what is true: every legitimate write to this bucket
   goes through a ConfigManager, and neither consults the predicate. Cost measured at zero (above).
2. **Alternative — keep `WriteOpen` and refuse the two identity keys specifically.** Preserves the "two subsystems
   write it, so it is open" reading, but adds a key-level guard the framework does not have today, protects only the
   keys someone remembered to name, and leaves the rest of a bucket holding correctness state generically writable.

Shape 1 was recorded here rather than assumed, and the owner ruled it on 2026-08-31. Shape 2 stands as the
alternative that was weighed, not as an open option.

## Fifth re-review, 2026-08-31 — per-finding disposition

| Finding | Disposition | `file:line` |
|---|---|---|
| **BLOCKING-1** bucket-wide owner-only was an unruled contract decision | RULED, not fixed — see the ruling above. No code changed: the owner concurred with the implemented option. The prose that still described the withdrawn open-write shape was swept in the same commit | `openspec/changes/federation-identity/conformance.md` ruling table; `docs/adr/104-unique-platform-authority.md` decision 6; `graph/constants.go`; `graph/kvcatalog_test.go`; `docs/operations/migration-beta162-to-beta163.md` |
| **BLOCKING-2** `EnsureFrameworkBucket` panicked on a nil context | FIXED. Reproduced first, with a CONNECTED client — that is what makes it reachable, and an unconnected one would have short-circuited on `ErrNotConnected` and proved nothing: the panic landed in `jetstream.(*jetStream).wrapContextWithoutDeadline`, the same class B8 fixed one layer up. This change extended the seam (it now owns the strict-retention arm and both catalog acquisitions reach it), so it is removal work, not inheritable debt. Guard is first, above the client check. **`graph.EnsureCatalogBucket` needs no guard of its own — verified, not assumed: it does an in-memory `SpecFor` lookup and delegates, touching neither ctx nor NATS** | `natsclient/kvspec.go` `EnsureFrameworkBucket`; `TestIntegration_EnsureFrameworkBucket_RejectsNilContext`; mutation check M25 |
| **MEDIUM-1** all three refusals gave a false graph-specific remedy | FIXED. `semstreams_config` is operational configuration and no graph API can perform the intended write, so an operator following "use graph mutation APIs" finds nothing that helps. The wording now has ONE home, `graph.FrameworkOwnedWriteRefusal`, deriving the remedy from the descriptor's class — graph APIs for authoritative/derived state, the declared owner for operational — so a new catalog row gets the right remedy without anyone remembering. Three hand-copied sentences are what drifted in the first place | `graph/kvcatalog.go` `FrameworkOwnedWriteRefusal`; `processor/rule/{actions.go,config_validation.go,kv_writer.go}`; `assertSharedConfigRefusal` pins that the shared-config refusal never says "graph" |
| **MEDIUM-2** the acquisition contract test did not test its claimed invariant | FIXED, both defects. The scan now covers every production file rather than only those making direct calls, so the two catalog-resolving owners are in the scanned set; and the check is PER ACQUISITION CALL — a file naming a catalogued bucket must have zero direct calls even when it also calls a seam elsewhere. The whole-file exemption was the same blindness recorded for M24's first attempt, and it had returned in a different shape. A positive assertion covers the vacuous case the structural check cannot: an owner that silently stopped acquiring would otherwise pass both | `test/contract/shared_config_bucket_acquisition_contract_test.go`; mutation check M26 |
| **MEDIUM-3** final artifacts still described the withdrawn open-write state | FIXED across all four sites, plus the exclusion list in task 6.2f that still named `deep-research` while 6.2i recorded it running. The withdrawn option is marked superseded rather than deleted | `graph/constants.go`; `graph/kvcatalog_test.go`; `conformance.md`; `tasks.md` 3.5, 6.2f |
| **MEDIUM-2 (citations)** the spec delta and tasks cited a test that does not exist | FIXED — both now name the real enforcing tests | `openspec/changes/federation-identity/specs/framework-bucket-catalog/spec.md`; `tasks.md` |

### A detector that was satisfied by dead code (recorded because it nearly passed)

M24's first attempt did **not** red. The contract scan asked whether `kv_writer.go` REFERENCES a catalog seam, and
commenting the branch out with `&& false` left the reference in place. A structural check of that shape passes on
bypassed code. The behavioural guard `TestKVWriterRefusesCatalogedOwnerOnlyBucket` was added for exactly this: it
builds the writer with a nil NATS client, so any path that reaches acquisition panics and only a refusal-before-bind
can pass. The re-run of M24 reds it. The structural scan is kept as the backstop that found the omission originally.

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
