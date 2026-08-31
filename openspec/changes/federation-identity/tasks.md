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
      draft PR #1178 `Closes #1168`; `implemented-by: <persona>` set at implementation. The proposal was the first
      commit; the inventory, the design, and this re-cut followed.
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
      `TestConfigManagerAdoptsPersistedPlatformIdentity` (a file declaring the stem, and a file declaring the full
      identifier, both adopt; a file declaring another `platform.id`, and one declaring another `platform.org`, both
      refuse),
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

- [x] 3.1 `config/config.go`: `validateAuthorityPair` reserves the seven bytes of the minted suffix
      (`mintedSuffixBytes`) against `semtypes.MaxAuthorityPairBytes()` and names the reserve in the error. One rule
      for the document's pair, an adopted record's, and the effective pair — no second spelling, no caller-supplied
      budget.
- [x] 3.2 `config/manager.go`: add `platformIdentityRecord{Org, Stem, ID}` and the `platform_identity` key
      constant; add `establishPlatformIdentity(ctx)` called first in `Start(ctx)`, taking ONE `kvStore.Keys(ctx)`
      read and branching adopt / mint+`Create` / refuse-pre-identity-bucket per the design §3 table; return the
      first-boot answer from that same read, counting keys other than `platform_identity`. Mint is `crypto/rand`
      3 bytes → 6 lowercase hex. `ErrKVKeyExists` re-reads and adopts. The effective identifier is applied through
      `SafeConfig.Mutate`, which re-validates the pair. Every KV operation uses the Start context
      (`manager.go:73`'s constructor root is untouched and stays recorded debt).
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
- [x] 6.2 Covering e2e tiers, one at a time on an idle host, results verbatim. Re-run at `e09df6f2`, AFTER the
      review round's fixes, because they touched `config` and `test/e2e/scenarios`.
- [x] 6.2a `task e2e:core` EXIT=0 — `[OK] platform_identity records {org, stem, id} and the effective pair carries
      the minted suffix`; `[OK] A pre-identity bucket refuses LOUD, exits nonzero, and creates no identity record`.
      The second stage passing also proves `client.IsKVKeyNotFound` matches: the assert half returns success only
      through that branch.
- [x] 6.2b `task e2e:lifecycle` EXIT=0 — all eight stages, so the four-position `--lifecycle-seed` and the observed
      pair agree.
- [x] 6.2c `task e2e:structural` EXIT=0 — `entity_count:127 validation_errors:0`, minted pair observed in-run as
      `c360.semstreams-e2e-structural-fd1546`.
- [x] 6.2d `task e2e:lessons` EXIT=0 — `Scenario completed successfully … assertions_run=3`, minted pair
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
- [x] 6.2g `task e2e:statistical` EXIT=0 — run to settle 6.2e: `entities_missing:0`, `entity_count:125`,
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
