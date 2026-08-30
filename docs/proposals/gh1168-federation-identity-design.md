# gh#1168 — federation identity: design (Case B only, after the owner scope cut)

## Checkpoint

- **Scope: Case B only, knobless.** Owner ruling on #1168, 2026-08-30 ("cut 1168 to the suffix, file the mint bug
  separately and i am not sure we even need a knob to opt out?"). Case A (the framed-digest run identity) is
  **#1192**; the import-lane collision gate is **#1194**; the `run_scope=new` anchor-append bug is **#1193**; the
  environment surface is **#1186**; the three zero-caller deletions are **#1187**. Nothing from those issues is
  designed, specified, or implemented here.
- **Accepted inventory:** `docs/proposals/gh1168-federation-identity-inventory.md` at `5967394f`, sha256
  `7ec8c0888afa485845e4206a7904d1f740d0ea2c437245f05e9877a0d70af9fb` (`INVENTORY PASS WITH DIVERGENCES`, residuals
  N1–N3 folded in). Referenced by section (`inv §x.y`), never re-pasted. Its Case A and O-4 sections describe
  surfaces this design no longer touches.
- **Design premises are pinned:** `docs/proposals/gh1168-federation-identity-pins.md` (O-11 — a companion pins file
  rather than converting the passed inventory checkpoint; `bash scripts/inventory-verify.sh` re-checks it after a
  rebase).
- **Code baseline:** `main@48a84641`.
- **Binding inputs applied:** the scope cut above; the owner greenfield ruling 2026-08-30 (#1168 comment
  5469209066 — "we are greenfield… the sister projects can handle the migration"); the architect revision round on
  PR #1178 (§1.A's revised three-branch mechanism and its four collision closures, §1.F's e2e stage, §1.J's ADR
  narrowing, §1.K's migration obligations, §1.L's pins file); the independent design review on PR #1178; ADR-102
  d1/d7; ADR-091; the CLAUDE.md context rule; the adopter-seam rule.
- **Artifacts this design produced:** `openspec/changes/federation-identity/{proposal,tasks}.md` and its two spec
  deltas (`entity-id-contract`, `component-runtime-config`); `docs/adr/104-unique-platform-authority.md` (Proposed);
  the federation-identity section of `docs/operations/migration-beta162-to-beta163.md`; the pins file.

## 1. Premises (each measurable, each measured)

| # | Premise the design rests on | Measurement |
|---|---|---|
| P1 | First-boot persistence already exists on the NATS medium (`semstreams_config`, the gh#459 identity guard) and the effective config flows to `extractPlatformMeta` **after** `Manager.Start` returns | inv §2.4 P1; `cmd/semstreams/main.go:203-244, 526-533`; `config/manager.go:172-260, 756-761, 866-894` |
| P2 | The KV `platform` push is `Put`; sync direction after the guard is version-driven (file newer → `PushToKV`; otherwise → `syncFromKV`) | `config/manager.go:225-260, 703-761` |
| P3 | `NewConfigManager` creates `context.Background()` in a constructor (`:73`); `Manager.Start(ctx)` receives a context | inv §7 Q9; `config/manager.go:73, 172` |
| P4 | e2e tiers derive the authority pair **by prediction** from config files (`test/e2e/config/tier_authority.go`, `CoreAuthority`), the e2e client can read any KV bucket, and `docker/compose/lifecycle.yml:62` hardcodes a seed under the predicted pair | design pass 2026-08-30 |
| P5 | `updateConfig`'s `case "platform"` unmarshals the KV `platform` block over `currentConfig.Platform`, `ID` included; `syncFromKV` calls it for every key and Start reaches it on three branches | `config/manager.go:567`, `:897-943`, `:225-260` |
| P6 | `hasKVConfig` returns true on **any** key in the bucket | `config/manager.go:832-840` |
| P7 | `syncFromKV` does `current.Services = make(...)` before repopulating, so a subsequent-boot branch taken on a near-empty bucket **wipes the in-memory service map** | `config/manager.go:897`, `:909` |
| P8 | A pre-beta.163 bucket (holding `platform`/`version`, no `platform_identity`) would make the gh#459 guard refuse with *"shared bucket belongs to another platform"* — the wrong cause — **after** an identity record was durably created | `config/manager.go:178`, `:205-222`, `:832-840`, `:892` |
| P9 | `validateAuthorityPair` bounds the pair the configuration document declares; the minted suffix adds 7 bytes to it, and ADR-102 d7 forbids rewriting a minted authority | `config/config.go:795-807`; `pkg/types/framework_identity_families.go:55-61` |
| P10 | `GetPlatform()` has exactly two production callers, both `extractPlatformMeta` on the **post-Start effective** config | `git grep -n 'GetPlatform()'` → `cmd/semstreams/main.go:528`, `cmd/e2e-semstreams/main.go:682` |
| P11 | `natsclient.KVStore` exposes atomic `Create` returning `ErrKVKeyExists` on conflict, and `Keys` that maps an empty bucket to `(nil, nil)` | `natsclient/kv.go:202-220`, `:494-511` |
| P12 | #1188 namespaces the config bucket by the **pre-mint** `(org, stem)` and retires the gh#459 guard, so this mint must be correct without that guard | #1188; revision §2.8 |

Premises P9–P16 and P20 of the reviewed (pre-cut) design covered Case A, the environment surface, and the deletions.
They left with #1192, #1186, and #1187 and are not restated here.

## 2. Options considered, with costs

### 2.1 Case B — a unique authority pair by default

| Option | Where the suffix is minted / persisted | Cost | Prediction asked of the operator |
|---|---|---|---|
| B0 do nothing | — | the cloned-template footgun stays; `local_authority_claimed` is the only detector, and only after two deployments collide in one graph | operator predicts global uniqueness |
| **B1 mint at `Manager.Start` on first boot; persist `{org, stem, id}` in `semstreams_config/platform_identity` with `Create`; adopt on every later boot and co-process** | KV, the medium P1 already uses | e2e must **observe** the pair instead of predicting it (P4); the load budget reserves the suffix's 7 bytes; four existing config-manager paths must be closed (§3) | none — the framework observes sameness rather than asking anyone to assert it |
| B2 mint at config load, persist by rewriting the config file | file medium | read-only mounts fail; two processes sharing a config on different hosts diverge; `SaveToFile` writes the whole config | operator predicts a writable config |
| B3 derive deterministically from a host fact (hostname) | no persistence | clones with equal hostnames still collide; this is not entropy | — |
| B4 refuse an entropy-less id at load, no mint | — | "entropy-less" is undecidable by grammar; moves the mint into the operator's hands | operator predicts |

**Recommendation and ruled: B1.** It is the only shape where the framework observes sameness (the KV record) rather
than asking the operator to predict uniqueness, and it reuses P1's bucket, guard, and boot ordering.

### 2.2 The opt-out (the knob the owner deleted)

The reviewed design carried `platform.unique: true` in the configuration document. The owner deleted it:

> The opt-out lived in the config file — the very template the threat model says gets cloned; `mint_suffix: false`
> cloned N times recreates the shared-authority footgun this change exists to close.

| Option | Shape | Why not |
|---|---|---|
| `platform.unique: true` / `mint_suffix: false` | a boolean in the cloned document | recreates the footgun exactly; and an unknown-key typo is silently dropped by `encoding/json` (review finding), so the guard would need a strict `platform`-block key check whose only motivating hazard is the knob itself |
| environment variable | a per-process override | forks identity between co-processes on one bucket; and the env surface left with #1186 |
| **pre-create the record** | the operator writes `semstreams_config/platform_identity` = `{"org":"…","stem":"…","id":"…"}` with `id == stem` before first boot; the adopt branch takes it | **chosen.** Per-deployment by construction — impossible to clone through a template. The adopt branch validates the adopted `id` under the same charset, subject-safety and budget rules as a configuration value |

Consequence: `platform.Config` gains no field, `validatePlatformBlockKeys` is not written, and the reviewed design's
Q4 (the knob's name) dissolves. Restorable by one owner line if this disposition is wrong.

## 3. The mechanism, and the four collisions it closes

**Root cause of all four, stated once:** the reviewed mechanism put a durable *identity* record inside
`semstreams_config`, a bucket whose contract is *mutable desired-state config, probed by key count and synced both
ways*. **Identity is not config.**

`Manager.Start(ctx)` calls `establishPlatformIdentity(ctx)` **first** — before the gh#459 guard, before version
arbitration, before any watcher or write — taking **one** `Keys(ctx)` read that also answers first-boot detection:

| Bucket state (from the one read) | Action |
|---|---|
| `platform_identity` present | **adopt**: read the record, refuse Start unless its `org` equals the file's and the file's `platform.id` equals the record's `stem` or its `id`, validate the adopted `id` as a configuration value would be, then make it the effective `platform.id` |
| absent, **no other key** | **genuine first boot**: mint `-` + 6 lowercase hex from `crypto/rand`, `Create` the record; on `ErrKVKeyExists` (a co-process won the race) re-read and adopt |
| absent, **other keys present** | **refuse Start** naming *that* cause and the fresh-storage instruction. **Mint nothing. `Create` nothing.** |

"First boot" for the rest of `Start` is `other keys present`, not "any key": the identity record is not configuration.

| # | Collision | Site | Closed by |
|---|---|---|---|
| 1 | `updateConfig`'s `case "platform"` unmarshals the KV `platform` block over `currentConfig.Platform`, `ID` included, so a stale or foreign KV `platform` key overwrites the effective authority after it was established (P5) | `config/manager.go:567`, `:897`, `:225-260`; `cmd/semstreams/main.go:526` | **delete the `case "platform"` arm.** `PushToKV` keeps writing the key as a **read-only mirror** for the UI; the unknown-key `default` already returns `errNoConfigChange`, so subscribers are still notified and nothing is applied. Loses nothing: `GetPlatform()` has no post-boot caller (P10, inv §2.2 Fact B) |
| 2 | `hasKVConfig` returns true on **any** key (P6), so a genuinely first boot that had just created its identity record would take the subsequent-boot branch, skip `PushToKV`, and — through `syncFromKV` — **wipe the in-memory service map** (P7) | `config/manager.go:832-840`, `:909` | **the single pre-mint read**: partition the one `Keys` result into "the identity record" and "everything else"; `hasConfig` is `everything else > 0`. `hasKVConfig` is deleted with its only caller |
| 3 | mint-vs-budget: `validateAuthorityPair` bounds the *unsuffixed* pair; the suffix adds 7. A pair that fits unsuffixed but not suffixed passes load, is durably `Create`d, and then fails forever — d7 forbids rewriting (P9) | `config/config.go:795-807` | **bound at load the value that will actually be minted**: one rule, `len(org) + len(id) + 7 ≤ MaxAuthorityPairBytes()`, applied by `validateAuthorityPair` to every pair it sees — the document's, the adopted record's, and the effective one. No pre-`Create` probe and no rollback: a rollback would `Delete` an identity record, which d7 forbids. Cost stated: 7 bytes of headroom, uniformly, for every deployment (163 rather than 170 declarable bytes; the longest pair measured in this family of repos is 33) |
| 4 | **found in the revision round, not in the review** — an existing pre-beta.163 bucket holds `platform`+`version` and no `platform_identity`. Mint-before-guard makes the effective `dep-7f3a9c` differ from the stored `dep`, so the gh#459 guard refuses with *"shared bucket belongs to another platform"* — the wrong cause, **after** the record was `Create`d, permanently (P8) | guard `config/manager.go:205-222`; key `:892` | **the third branch: refuse before minting**, naming the pre-identity bucket as the cause. Also turns the upgrade path into something an e2e stage can cover (§4) |

Two properties this shape has that the reviewed one did not: it is correct **without** the gh#459 guard (P12 — the
adopt check is its own comparison, org included), and no path can leave a durable record that a later boot rejects.

## 4. e2e and landing coverage

- `task e2e:core` gains **`validate-minted-authority`**: the e2e client reads `semstreams_config/platform_identity`,
  asserts `id == stem + "-" + 6 hex`, and asserts the graph round-trip canary is minted under the observed pair.
- `task e2e:core` gains **`validate-pre-identity-bucket-refusal`** (revision §1.F): seed `semstreams_config` with
  `platform` and `version` and no `platform_identity`, boot, assert Start refuses naming the pre-identity cause
  **and that no `platform_identity` key was created**.
- `task e2e:lifecycle`'s seed composes under the observed pair rather than a predicted one (P4).
- Landing coverage is `config`'s real-NATS integration family, including
  `TestPreIdentityBucketRefusesStartWithoutMinting`.

## 5. Second- and third-order impact rows

| Surface | Today | After | Evidence |
|---|---|---|---|
| `platform.id` | exactly what the file declares | `<declared>-<6 hex>` from the deployment's first boot on, unless a `platform_identity` record was pre-created | §3 |
| KV keys | `semstreams_config/{version,platform,services.*,components.*,nats,model_registry}` | + `platform_identity` (Create-once; never pushed, never synced, never watched) | P1 |
| KV `platform` key | read-write: pushed by `PushToKV` **and** applied back by `updateConfig` | **read-only mirror**: still pushed, never applied | collision 1 |
| First-boot detection | any key in the bucket | any key **other than** `platform_identity` | collision 2 |
| Authority-pair budget | `len(org)+len(id) ≤ 170` | `len(org)+len(id)+7 ≤ 170` — one rule for the document, the adopted record, and the effective pair; `TestConfigRejectsOversizedAuthorityPair` 171 → 164 | collision 3 |
| Start failure modes | foreign-identity mismatch | + pre-identity bucket (named separately, mints nothing), + adopted-record mismatch (org, or stem/id) | §3 |
| Config surface | `platform.{org,id,type,region,capabilities,environment}` | **unchanged — no new key** | §2.2 |
| e2e literals | `TierAuthority`, `CoreAuthority`, `lifecycle.yml:62` seed | observed from `platform_identity`; the two drift tests become **stem** checks | P4 |
| Context rule | `config/manager.go:73` root in a constructor | untouched and still recorded debt; every KV operation of the mint uses the `Start(ctx)` context | P3 |
| semsource arithmetic | `MaxOrgLen` assumes `platform (9)` | the suffix adds 7 bytes to every pair | inv Fact B′ |

## 6. Sister impact (communicate only; read-only census inv §4.6)

| Adopter | Impact |
|---|---|
| every sister that ships a config | `platform.id` gains a suffix on that deployment's first boot against fresh storage; fixtures that hardcode the pair read `semstreams_config/platform_identity` instead, or pre-create the record |
| semteams | 4 configs; its e2e/UI fixtures that spell the pair |
| semspec, semspec-ui-* | 13/11/11 configs |
| semdev | 2 configs |
| semmem | 3 configs (`instance_id` already fails load on main) |
| semsource | `entityid.MaxOrgLen`'s arithmetic assumes a 9-byte platform; re-derive from `semtypes.MaxAuthorityPairBytes()` and the 7-byte suffix reserve |
| semmachina | composes `platform.id` per world in Go; each world's id is suffixed on its own first boot — pre-create the record for a world that must stay unsuffixed |
| semboids, semsage, semconnect, semdragon, semlink, semops | config suffix only; no code surface |

Sister inventories exist to **size the migration note**; they never gate or reshape the design (owner process rule,
2026-08-30).

## 7. Adopter seam inventory for what this design ADDS

The reviewed design's §8/N1–N8 covered Case A surfaces and the deleted knob. What remains added is one surface.

**N1 — `semstreams_config/platform_identity`, a `{org, stem, id}` JSON record.**

- *Who owns this responsibility today?* Nothing. `semstreams_config` holds desired-state config; the gh#459 guard
  reads identity out of the `platform` **config** key, which is exactly the conflation §3 unwinds.
- *Is the premise true?* Yes — P1 measured the first-boot persistence seam; no durable identity record exists.
- *Who consumes it at birth?* `Manager.Start` (adopt), the e2e client (`validate-minted-authority`), and any adopter
  fixture that must know the effective pair. It is the cross-repo contract ADR-104 records.
- *Am I asking a caller to predict something the framework could observe?* No — and this is the point of the whole
  change. **F1 (finding, from the revision round):** the record's shape is a cross-repo contract, so it MUST be
  normative in the `component-runtime-config` spec delta with its own scenario, not only in `tasks.md`. Closed by the
  delta.
- **F2 (finding, carried, not closed here):** there is no **non-Go** way to observe the effective pair — a sister
  reads the NATS KV record directly. Adding it to readiness/health over HTTP is a new outward surface in a change
  that has now shed scope twice; the owner recommendation was *(i) specify the record shape normatively now, file
  (ii)*. Filed rather than built.

**The escape hatch is a seam too.** An operator who wants an unsuffixed authority pre-creates the record with
`id == stem`. What must they know? One documented one-liner (migration doc obligation 2). What happens if they do
nothing? They get a suffix — the safe default. Where do they find out? The migration note and the boot log line
`Platform identity configured`. What SHOULD they have to know? Nothing, in the 99% case — and they don't.

## 8. Decision skills applied

`entity-or-bucket` → the identity record is **operational KV, not a graph entity**: it is the framework's own
private boot-time state about which authority a bucket belongs to, read before any graph exists, and no rule reads
it. `kv-or-stream` → not triggered: no new communication path; the record rides the existing `semstreams_config`
bucket and is never watched. `orchestration-check` → not triggered: a single create-or-adopt inside `Start`, no
multi-step behaviour, no rule. `new-payload` → not triggered: no new message type.

## 9. Open questions

None on this change. The remaining docket on #1168 is the owner's own: the final word on the knob disposition
recorded in §2.2, and milestone placement for #1192 and #1194. Every other question the reviewed design carried was
ruled on 2026-08-30 or left with the issue that took its scope.
