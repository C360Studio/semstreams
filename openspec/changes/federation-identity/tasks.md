# Tasks — federation-identity

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

**Scope: Case B only, knobless** (owner scope cut on #1168, 2026-08-30). Rehomed and NOT in this change: the
framed-digest run identity → #1192; the import-lane collision gate → #1194; the anchor-append bug → #1193; the
environment surface → #1186; the three zero-caller deletions → #1187.

Premises (measured at `main@48a84641`): design `docs/proposals/gh1168-federation-identity-design.md` P1–P12, pinned
in `docs/proposals/gh1168-federation-identity-pins.md`; inventory
`docs/proposals/gh1168-federation-identity-inventory.md` (`5967394f`, sha256 `7ec8c088…`) §2.4 P1/P6/P7.

## 1. Claim

- [x] 1.1 Worktree `../semstreams-wt/claude/gh1168-federation-identity`, branch `claude/gh1168-federation-identity`;
      draft PR #1178 `Closes #1168`. The proposal was the first commit; the inventory, the design, and this re-cut
      followed. `implemented-by: opus (developer rounds); fable (coordination, review-closure commits)` is in the PR
      body as of the Codex round — the earlier text here claimed that before it was true (Codex MEDIUM); 6.6 owns
      any later body change.
- [x] 1.2 Re-cut the change package to the owner's Case B scope: design, proposal, tasks, the two surviving spec
      deltas (the `graph-ingest` delta left with #1192/#1194), ADR-104, the migration section, and the pins file.

## 2. Baseline capture — write the named tests first

- [x] 2.1 `config/config_test.go`: `TestConfigRejectsOversizedAuthorityPair` (now 164 bytes, and 163 accepted),
      `TestConfigRejectsPairThatOnlyFitsUnsuffixed` (a pair that fits the 170-byte family budget but not once the
      seven-byte suffix is reserved). Both assert the error names the binding family and the reserve.
- [x] 2.2 `config/manager_identity_integration_test.go` (integration, real NATS):
      `TestConfigManagerFirstBootMintsPlatformIdentity` (record carries exactly `org`/`stem`/`id`; `id` is the stem
      plus `-` plus six lowercase hex; the effective `platform.id` is that value; the pushed `platform` key carries
      it; the boot is still treated as a first boot),
      `TestConfigManagerAdoptsPersistedPlatformIdentity` (a file declaring the stem adopts; a file declaring another
      `platform.id`, another `platform.org`, or the minted identifier itself all refuse — the last with stem
      guidance, `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance`),
      `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity` (two managers, one bucket, started concurrently:
      one record, one identifier, both effective configs equal),
      `TestFirstBootMintsDistinctSuffixesPerDeployment` (two buckets, two suffixes),
      `TestPreCreatedIdentityRecordIsAdoptedUnsuffixed` (the knobless escape: `id == stem`, no suffix minted),
      `TestPreIdentityBucketRefusesStartWithoutMinting` (a bucket holding `platform` and `version` and no record:
      Start fails naming the pre-identity cause, and no `platform_identity` key exists afterwards),
      `TestVersionArbitrationNeverOverwritesPlatformIdentity`,
      `TestKVPlatformKeyIsAMirrorNotASource` (an external `platform` write does not change the running authority).
- [x] 2.3 Baseline capture, verbatim, filtered to build errors and `--- FAIL` lines. 2.1's two tests failed
      "An error is expected but got nil" (164 bytes loaded; the unsuffixed-only pair loaded); 2.2's file did not
      compile — `undefined: platformIdentityRecord`, `undefined: platformIdentityKVKey`.

## 3. Contract — `config`

- [x] 3.1 `config/config.go`: TWO boundaries, one budget source. `validateDeclaredAuthorityPair` reserves the seven
      bytes of the minted suffix (`mintedSuffixBytes`) against `semtypes.MaxAuthorityPairBytes()` — 163 — and runs
      at `Loader.Load`/`LoadFromBytes`, ungated, because no production loader enables validation.
      `validateAuthorityPair` bounds an EFFECTIVE pair (minted, adopted, or running) at the full 170 and is what the
      mint, the adopt, and `Config.Validate` call. The reserve is a fact about a declaration, not about a pair;
      applying it to both kinds refused at Start a declaration that had passed load (review HIGH-1, reproduced by
      `TestMaximumDeclarablePairMintsAndStarts` before the fix). Tests:
      `TestConfigRejectsOversizedAuthorityPair`, `TestConfigRejectsPairThatOnlyFitsUnsuffixed`,
      `TestEffectivePairIsBoundedWithoutTheDeclarationReserve`, `TestMaximumDeclarablePairMintsAndStarts`;
      mutation checks M14 and M15.
- [x] 3.5 `config/manager.go`, `graph/`, `natsclient/`, `processor/rule/`: the Codex re-review's three findings —
      the acquired handles stay LOCAL through every Start step that can refuse and reach the struct only via
      `publishBucket` at the end, so a refused Start leaves `PushToKV`/`PutComponentToKV`/`DeleteComponentFromKV`
      returning `errBucketNotAcquired` and the foreign bucket byte-for-byte unchanged (B6); the shared bucket gains
      a `framework-bucket-catalog` descriptor — operational, open-write, History 5, a NEW strict no-lifecycle
      retention kind that verifies and refuses instead of reconciling — and BOTH creators (`config.Manager`,
      `processor/rule.ConfigManager`) resolve it, so the retention guarantee no longer depends on who created the
      bucket first (B7); `Start` rejects a nil context before touching state or NATS, where it previously panicked
      inside JetStream (B8). Mutation checks M20, M21, M22.
- [x] 3.4 `config/manager.go`: the Codex round's four code findings — bucket acquisition moved out of the
      constructor into `Start(ctx)` (B4: no invented root, `natsClient` retained instead of a context, and
      `errBucketNotAcquired` for any bucket-dependent method called before Start); `acquireBucket` refuses a bucket
      whose live policy can evict the identity, through the existing `KVStore.AssertNoLifecycleRetention` rather
      than a second spelling of that rule (B1); `claimEnvironment` claims the bucket for one `platform.environment`
      by atomic create, before the record, so the Create/adopt race cannot let two environments both publish (B2);
      the adopt comparison accepts the record's STEM only, and a file holding the minted identifier is refused with
      guidance derived from the stored value, never from grammar (B3). Mutation checks M16, M17, M18.
- [x] 3.2 `config/manager.go`: add `platformIdentityRecord{Org, Stem, ID}` and the `platform_identity` key
      constant; add `establishPlatformIdentity(ctx)` called first in `Start(ctx)`, taking ONE `kvStore.Keys(ctx)`
      read and branching adopt / mint+`Create` / refuse-pre-identity-bucket per the design §3 table; return the
      first-boot answer from that same read, counting keys other than `platform_identity`. Mint is `crypto/rand`
      3 bytes → 6 lowercase hex. `ErrKVKeyExists` re-reads and adopts. The effective identifier is applied through
      `SafeConfig.Mutate`, which re-validates the pair. Every KV operation uses the Start context — the constructor
      creates no context and performs no I/O at all (3.4/B4 moved acquisition into `Start(ctx)`; the earlier text
      here still recorded it as untouched debt, which contradicted 3.4, the code, and conformance — Codex MEDIUM).
- [x] 3.3 `config/manager.go`: delete `hasKVConfig` (its only caller is replaced by 3.2) and delete `updateConfig`'s
      `case "platform"` arm so the KV `platform` key is a published mirror the running configuration never adopts;
      `PushToKV` keeps writing it. Neither `PushToKV` nor `syncFromKV` writes or applies `platform_identity`.

## 4. Forced omissions — one per new guard (commit GREEN first; restore by `cp` + `md5`)

All six run at `eb83b5b0`, each mutated alone, `[applied]` printed between mutating and testing, restored by `cp`
with a matching `md5 -q`. Verbatim failure lines below.

- [x] 4.1 M4 `mintPlatformIdentity`: `Create` → `Put` → `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity`
      failed: `expected: "dep-f900fa" actual: "dep-952e5e"` — "two co-processes must converge on one authority".
- [x] 4.2 M5 `adoptPlatformIdentity`: skip `applyEffectivePlatformID` so the file's `platform.id` stays effective →
      `TestConfigManagerAdoptsPersistedPlatformIdentity/file_declares_the_stem` failed:
      `expected: "dep-7f3a9c" actual: "dep"`.
- [x] 4.3 M11 pre-identity branch made unreachable, so such a bucket falls through to the mint →
      `TestPreIdentityBucketRefusesStartWithoutMinting` failed: "An error is expected but got nil".
- [x] 4.4 M12 first-boot detection counts the identity record (`configKeys > 0` → `len(keys) > 0` on the adopt
      branch) → `TestBootWithOnlyAnIdentityRecordIsStillAFirstBoot` failed:
      `types.ServiceConfigs{} does not contain "metrics"` — the exact `syncFromKV` service-map wipe premise P7
      predicted. The first attempt at M12 mutated the key-partition loop instead and did NOT fail, because on a
      genuine first boot the record does not exist yet; the guard lives on the adopt return, and that test was
      written for it.
- [x] 4.5 M13 `updateConfig`: restore the `case "platform"` arm → `TestKVPlatformKeyIsAMirrorNotASource` failed:
      `expected: "dep-6e923f" actual: "other"` — an external write moved the running authority.
- [x] 4.6 M14 `validateDeclaredAuthorityPair`: drop the seven-byte reserve (bound at 170 instead of 163) → THREE
      tests failed, all "An error is expected but got nil":
      `TestConfigRejectsPairThatOnlyFitsUnsuffixed`, `TestConfigRejectsOversizedAuthorityPair` ("164 bytes must not
      load"), and `TestEffectivePairIsBoundedWithoutTheDeclarationReserve` ("the same pair DECLARED leaves no room
      for the suffix"). Re-pointed from `maxDeclarableAuthorityPairBytes` after the review round moved the reserve to
      the declaration boundary; the reviewer's note that M14 also reds `TestConfigRejectsOversizedAuthorityPair` is
      the stronger signal and is recorded here.
- [x] 4.11 M20 `Start`: publish the handles at acquisition instead of after establishment →
      `TestRefusedStartDisarmsEveryExportedWriter` failed — `PushToKV` returned nil after a refused Start.
- [x] 4.12 M21 `Start`: drop the nil-context guard → `TestStartRejectsNilContextWithoutSideEffects` failed with the
      panic Codex named, `jetstream.(*jetStream).wrapContextWithoutDeadline`.
- [x] 4.13 M22 `processor/rule`: acquire the shared bucket with its own `CreateKeyValueBucket` instead of the
      descriptor → `TestSharedConfigBucketResolvesThroughOneDescriptor` failed naming
      `processor/rule/kv_config_integration.go:581`.
- [x] 4.8 M16 `acquireBucket`: skip `AssertNoLifecycleRetention` → `TestEvictingConfigBucketRefusesStart` failed on
      both TTL and MaxBytes ("An error is expected but got nil") and
      `TestIdentityUnderAnEvictingBucketNeverRemints` failed `expected: "dep-31a043" actual: "dep-7fbc40"` — the
      remint Codex measured, reproduced here.
- [x] 4.9 M17 `claimEnvironment`: never refuse a mismatched environment →
      `TestConcurrentFirstBootRefusesASecondEnvironment` failed `expected: 1 actual: 2` with both errors nil.
- [x] 4.10 M18 adopt: accept the minted identifier as a declarable value again →
      `TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance` and
      `TestConfigManagerAdoptsPersistedPlatformIdentity/file_declares_the_minted_identifier` both failed
      ("An error is expected but got nil").
- [x] 4.11 M19 key partition: remove the `platformEnvironmentGuardKey` exclusion (found unguarded by the second
      narrow re-review — the whole integration suite stayed green without it) → with the first-boot test seeding
      `{guard, record}`, `TestBootWithOnlyAnIdentityRecordIsStillAFirstBoot` failed
      `types.ServiceConfigs{} does not contain "metrics"` — the P7 service-map wipe, same class M12 pins for the
      record key.
- [x] 4.7 M15 `validateAuthorityPair`: reserve the suffix on the EFFECTIVE pair too — the HIGH-1 defect itself →
      `TestMaximumDeclarablePairMintsAndStarts` failed ("a pair at the declarable budget must boot") and
      `TestEffectivePairIsBoundedWithoutTheDeclarationReserve` failed ("a minted pair at exactly the family-table
      budget is legal"). This is the regression guard for the double-count.

## 5. Sweep — spec, docs, e2e, configs, sisters' notes

- [x] 5.1 `docs/proposals/gh1095-entity-id-segment-semantics-design.md`: correct the collision claim by an appended
      note, not a rewrite (item 3 of the issue; former task 5.2).
- [x] 5.2 e2e observes rather than predicts: `test/e2e/config.EffectiveAuthority` reads
      `semstreams_config/platform_identity` and cross-checks the declared stem; `TierAuthority`/`CoreAuthority`/
      `TierEntityID` became `TierAuthorityStem`/`CoreAuthorityStem`/`TierStemEntityID` so a name cannot claim to be
      an authority it is not; the tiered scenario, the graph round-trip probe, lessons, throughput, and lifecycle all
      resolve at run time; `--lifecycle-seed` now takes the last FOUR positions and the binary composes the pair
      (`docker/compose/lifecycle.yml`).
- [x] 5.3 e2e stages in the core scenario: `validate-minted-authority` (the record's `id` is the stem plus six hex;
      the canary is minted under the observed pair) and `validate-pre-identity-bucket-refusal` (seed `platform` and
      `version` with no record; the boot refuses naming that cause and creates no record).
- [x] 5.4 `task schema:generate`; `git diff --exit-code schemas/ specs/`.
- [x] 5.5 Migration note: the federation-identity section of `docs/operations/migration-beta162-to-beta163.md`
      (re-cut with this change; amend to what shipped).
- [ ] 5.6 `docs/adr/104-unique-platform-authority.md` to Accepted on the owner's word; `docs/adr/README.md` if it
      indexes ADRs.
- [x] 5.7 Author `docs/proposals/gh1168-federation-identity-pins.md` (O-11) and keep it green.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `task entity-id:audit`; `task schema:generate && git diff --exit-code schemas/ specs/`;
      `openspec validate federation-identity --strict --no-interactive`; `go mod tidy -diff`;
      `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-pins.md`.
- [ ] 6.2 Covering e2e tiers, one at a time on an idle host, results verbatim. Re-run at `ede202e4` after the Codex
      round changed the config manager's acquisition; the `e09df6f2` run is superseded.
- [ ] 6.2a `task e2e:core` EXIT=0 — `[OK] platform_identity records {org, stem, id} and the effective pair carries
      the minted suffix`; `[OK] A pre-identity bucket refuses LOUD, exits nonzero, and creates no identity record`.
      The stack now also passes the bucket-policy check and the environment claim on every boot.
- [x] 6.2b `task e2e:lifecycle` EXIT=0.
- [x] 6.2c `task e2e:structural` EXIT=0 — `entity_count:127 validation_errors:0` under
      `c360.semstreams-e2e-structural-6f07a8`.
- [x] 6.2d `task e2e:lessons` EXIT=0 (at `e09df6f2`; its scenario is untouched by the Codex round) — `Scenario completed successfully … assertions_run=3`, minted pair
      `c360.streamkit-pure-7d99d3`.
- [~] 6.2e `task e2e:throughput` EXIT=0 twice (`c360.semstreams-statistical-16d370`, then
      `-abca74`) — but BOTH runs printed
      `skipping query load (timeout: 5/10 entities not queryable)`, so its query phase measured nothing. **Not an
      authority defect, and not introduced here:** `knownEntityIDs`' fixture list is byte-identical to
      `origin/main` (diffed), and the same stack under `task e2e:statistical` reports `entities_missing:0` with
      `entity_count:125` under the observed pair `c360.semstreams-statistical-a4d38e`. Filed as **#1195** (the skip must not
      be reachable through exit 0, plus the underlying 5/10 readiness timeout); placement is the owner's. This
      change's identity path is covered by 6.2c and the statistical run.
- [x] 6.2f Excluded with reason: `semantic` (same ingest path as structural; its identity literals
      come from the same e2e config helper structural exercised), `agentic`, `ops`, `crud-tools`, `research-graph`,
      `deep-research`, `slow-consumer`, `openai-responses` — none has a touched path beyond the e2e config helper.
      `statistical` was originally excluded on the same reasoning but has since RUN green — see 6.2g.
- [x] 6.2g `task e2e:statistical` EXIT=0 (at `e09df6f2`) — run to settle 6.2e: `entities_missing:0`, `entity_count:125`,
      `validation_errors:0` under `c360.semstreams-statistical-a4d38e`.
- [x] 6.2h Stage mutation check (not vacuous): `./e2e --scenario core-minted-authority` exits 1 with
      `read semstreams_config/platform_identity: ... bucket not found` when no record exists, and exits 1 with
      `recorded id "streamkit-pure" is not the stem "streamkit-pure" plus a suffix` against a hand-seeded unsuffixed
      record.
- [x] 6.3 Implementation review by `semstreams-reviewer`: CHANGES REQUESTED at `211bda7f` (2 HIGH, 4 MEDIUM, 2 NIT),
      fix round at `ecf58a27`, narrow re-review verified both HIGHs CLOSED by measurement, residuals closed at
      `a1432904`. Review of record: PR #1178 comment 2026-08-30; dispositions in `conformance.md` incl. the
      per-ruling conformance table.
- [ ] 6.4 Owner-run cross-agent round where asked.
- [ ] 6.5 `openspec archive federation-identity` + spec sync as the final content commit; narrow reviewer check.
- [ ] 6.6 Undraft; PR body carries `implemented-by`, the per-sister list, the value that changes on the wire
      (the `platform.id` suffix), and the e2e evidence pointers. No task asserts CI state.
