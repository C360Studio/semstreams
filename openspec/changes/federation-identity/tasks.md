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

## 4. Forced omissions — one per new guard (commit GREEN first; restore by `cp` + `shasum`)

- [ ] 4.1 M4 `establishPlatformIdentity`: use `Put` instead of `Create` →
      `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity` MUST fail.
- [ ] 4.2 M5 adopt: skip the `SafeConfig.Mutate` so the file's `platform.id` stays effective →
      `TestConfigManagerAdoptsPersistedPlatformIdentity` MUST fail.
- [ ] 4.3 M11 pre-identity branch: fall through to the mint instead of refusing →
      `TestPreIdentityBucketRefusesStartWithoutMinting` MUST fail.
- [ ] 4.4 M12 first-boot detection: count every key including `platform_identity` →
      `TestConfigManagerFirstBootMintsPlatformIdentity` MUST fail on the pushed configuration.
- [ ] 4.5 M13 `updateConfig`: restore the `case "platform"` arm → `TestKVPlatformKeyIsAMirrorNotASource` MUST fail.
- [ ] 4.6 M14 `validateAuthorityPair`: drop the seven-byte reserve →
      `TestConfigRejectsPairThatOnlyFitsUnsuffixed` MUST fail.

## 5. Sweep — spec, docs, e2e, configs, sisters' notes

- [ ] 5.1 `docs/proposals/gh1095-entity-id-segment-semantics-design.md`: correct the collision claim by an appended
      note, not a rewrite (item 3 of the issue; former task 5.2).
- [ ] 5.2 e2e observes rather than predicts: `test/e2e/config` reads `semstreams_config/platform_identity`;
      `TestTierAuthorityMatchesShippedConfigs` / `TestCoreAuthorityMatchesShippedConfig` become **stem** checks;
      `cmd/e2e/main.go` canary and `cmd/e2e-semstreams` `--lifecycle-seed` compose under the observed pair;
      `docker/compose/lifecycle.yml:62`; `test/e2e/scenarios/{lessons/scenario.go,throughput/query_load.go,tiered.go}`.
- [ ] 5.3 e2e stages in the core scenario: `validate-minted-authority` (the record's `id` is the stem plus six hex;
      the canary is minted under the observed pair) and `validate-pre-identity-bucket-refusal` (seed `platform` and
      `version` with no record; the boot refuses naming that cause and creates no record).
- [ ] 5.4 `task schema:generate`; `git diff --exit-code schemas/ specs/`.
- [ ] 5.5 Migration note: the federation-identity section of `docs/operations/migration-beta162-to-beta163.md`
      (re-cut with this change; amend to what shipped).
- [ ] 5.6 `docs/adr/104-unique-platform-authority.md` to Accepted on the owner's word; `docs/adr/README.md` if it
      indexes ADRs.
- [ ] 5.7 Author `docs/proposals/gh1168-federation-identity-pins.md` (O-11) and keep it green.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `task entity-id:audit`; `task schema:generate && git diff --exit-code schemas/ specs/`;
      `openspec validate federation-identity --strict --no-interactive`; `go mod tidy -diff`;
      `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-pins.md`.
- [ ] 6.2 Covering e2e tiers, one at a time on an idle host, results verbatim: `task e2e:core` (both Case B stages),
      `task e2e:lifecycle` (observed-pair seed), `task e2e:structural` (ingest regression under a minted pair).
      Excluded with reason: `statistical`, `semantic` (same ingest path as structural, no identity literal beyond
      the e2e config helper — re-run any whose scenario file changes in 5.2), `agentic`, `ops`, `lessons`,
      `crud-tools`, `research-graph`, `deep-research`, `slow-consumer`, `throughput`, `openai-responses`.
- [ ] 6.3 Implementation review by `semstreams-reviewer`; dispositions in `conformance.md`, including the per-ruling
      conformance table (owner cut → `file:line`).
- [ ] 6.4 Owner-run cross-agent round where asked.
- [ ] 6.5 `openspec archive federation-identity` + spec sync as the final content commit; narrow reviewer check.
- [ ] 6.6 Undraft; PR body carries `implemented-by`, the per-sister list, the value that changes on the wire
      (the `platform.id` suffix), and the e2e evidence pointers. No task asserts CI state.
