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
| The suffix reserve applies to **every** pair the framework accepts, not only the document's | `Config.Validate` is shared by load, `SafeConfig.Mutate`, and `ValidateEffectiveConfig`, and the type carries no marker for "already established". One uniform rule cannot admit a pair another path rejects; the cost is 7 bytes of headroom (163 declarable rather than 170), stated in the spec, the ADR, and the migration note | `config/config.go` `validateAuthorityPair` |
| `mintPlatformIdentity` validates the composed pair **before** `Create` | The revision rejected a pre-`Create` *probe* (a KV read) and a rollback (a `Delete`, which d7 forbids). This is neither: it is arithmetic on the value about to be written, and it makes "no record is created that a later boot rejects" a local property instead of an argument about a distant load check. It cannot false-refuse, because load already reserved the bytes | `config/manager.go` `mintPlatformIdentity` |
| A failed bucket read now fails Start instead of assuming first boot | The old `hasKVConfig` tolerated a read error by assuming first boot. Under minting that assumption **creates a second authority** for a deployment that already has one | `config/manager.go` `establishPlatformIdentity` |
| Minting refuses an empty `platform.org`/`platform.id` by name | `Config.Validate` requires both, so this is an unvalidated configuration reaching Start (library consumers can do it). Without the explicit refusal it surfaced as a family-composition error about `"-9ef4a0"` | `config/manager.go` `mintPlatformIdentity` |
| `TierAuthority`/`CoreAuthority`/`TierEntityID` renamed to `…Stem` | They now return the declared stem, not the authority. A name that says "authority" while returning a stem is the same prediction footgun one level up | `test/e2e/config/tier_authority.go` |
| `--lifecycle-seed` takes the last four positions | A compose file can no longer spell the pair at all. The old whole-ID form plus a mismatch guard was the right guard for the wrong shape | `cmd/e2e-semstreams/main.go` `seedMission`; `docker/compose/lifecycle.yml` |

## Test fixtures changed because the refusal is the feature

Every fixture that fabricated an "already configured" bucket without an identity record now seeds one; that refusal
is the change, not a regression. `config/manager_test.go`, `config/manager_integration_test.go`,
`cmd/semstreams/bootstrap_observability_integration_test.go`,
`internal/bootstrapobservability/config_manager_integration_test.go`. Two more moved for reasons named in their
comments: the gh#459 regression test now asserts the refusal that comes from the identity record one branch earlier,
and `service/component_manager_boot_findings_integration_test.go` declares stream bounds because Start now validates
the effective configuration when it applies the established identity.
