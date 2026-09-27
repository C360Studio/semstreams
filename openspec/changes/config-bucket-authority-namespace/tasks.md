# Tasks: the configuration bucket is named by the authority pair

Tasks record work when it happens. No task asserts a post-merge fact; CI, review, e2e and merge are PR #1404's
landing choreography. Pins are at base `fe9482b7` (design.md § 1). Every gate list includes
`go run ./cmd/entity-id-audit .`, because CI's Lint job runs it and `task lint` does not.

## 0. Rulings

- [x] 0.1 Q1 (d), Q2 (a) and Q3 were ruled on #1188 on 2026-09-27 (issuecomment-5856316438,
      issuecomment-5856267253). The 2026-09-01 ruling (issuecomment-5500899624) fixes the name and the retire list.

## 1. Watcher ownership (step 1)

- [x] 1.1 Add `config.ManagerOption`, `WithKeyFamily` and `KeyFamily` (design D1): the watch opens before publication
      and the family is bound at `publishBucket`. Add `TestKeyFamilyDeliversSnapshotThenChanges` (real NATS,
      snapshot, put, delete, not-acquired before Start and after a refused Start).
- [x] 1.2 Rework the rule `ConfigManager` around the family: `NewConfigManager(logger)`, `KeyFamily()`,
      `Start(ctx, targets)` and `Stop()`. Add `(*Processor).LoadedRuleDefinitions` and the `HotReloadTarget` interface.
      Delete `InitializeKVStore`, `ensureKVStore`, the literal-name acquisition, `WatchRules`, and the
      component-internal construction and stop (`processor/rule/processor.go:1019-1028`, `:1335`).
- [x] 1.3 In the root, construct the one rule manager and register its family through `StartValidatedConfigManager`.
      Serve it to the tools, and wrap `runtimeManager` so that `StartAll` then seeds and reconciles, and `StopAll`
      stops the loop first (design D3).
- [x] 1.4 Add `TestRootRuleManagerHotReloadsIntoTheProcessor`: write `rules.x` through the root's manager and observe
      the processor's `ApplyConfigUpdate` (real NATS, `-race`, explicit synchronization). Migrate the rule package's
      hot-reload, seeding and lifecycle tests to the new seam.

## 2. The name (step 2)

- [x] 2.1 Add `config.BucketName(org, stem)` with `TestBucketNameIsLegalForEveryValidPair` and `FuzzBucketName`. The
      manager derives the name at construction.
- [x] 2.2 Make the catalog row a name family (design D5) and add `TestConfigBucketFamilyResolvesEveryMember`. Update
      the rule ownership tests to a namespaced name, and drop the guard-key target.
- [x] 2.3 Retire the gh#459 guard, `kvPlatformIdentity`, `platformHasIdentity`, `platformIdentityTuple`,
      `claimEnvironment` and `platformEnvironmentGuardKey`. Re-read the pre-identity and adoption refusal messages,
      and demote `platform.Environment` to a log label in its doc.
- [x] 2.4 Tests: `TestBucketIsNamedByTheDeclaredPair` (different pairs use different buckets, the same pair shares
      one) and `TestEnvironmentDoesNotSeparateDeployments`. The gh#459 refusal test flips to no refusal, and the
      environment-race test is deleted. Tests that relied on a foreign deployment sharing the bucket now seed a
      foreign record into the declared pair's own bucket. Design R5 records the minted-identifier case, ruled Q4 (b): the
      mint branch reads the org's sibling buckets and refuses a declared minted identifier.
- [x] 2.5 Update `test/contract` (resolving owners), `test/e2e/config` (`PlatformIdentityBucket(declared)`), the
      scenarios that read or write the bucket, and crud-tools.
- [x] 2.6 Update the docs that spell the bucket: `config/README.md`, `docs/operations/05-model-registry.md`, and the
      comments at `internal/boot/run.go:702` and `processor/rule/kv_config_integration.go:30-40`. The migration note
      gets a new section and amends the ADR-104 obligations.

## 3. Evidence

- [x] 3.1 Mutation evidence (cp backup plus checksum): the family delete mapping, the catalog family match, the root
      loop's stop-before-`StopAll`, and the name derivation. Killed: M1 delete mapping (key-family test), M2 StopAll order (ordering test), M3 unbound
      family, M4 `WithKeyFamily` dropped from `run.go` (boot_order AST), M5 `nameFamilies` match (graph catalog tests
      and `TestKVWriterRefusesCatalogedOwnerOnlyBucket`), M6 separator (`TestBucketNameIsLegalForEveryValidPair`,
      `FuzzBucketName`), M7 `ruleHotReloadRuntime.targets` set to `nil` in `run.go`
      (`TestBinaryBootOrder`), M8 the sibling-bucket refusal ignored on the mint branch
      (`TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance`); checksums restored each time.
- [x] 3.2 Gates green before each push: `go build ./...`, `task lint`, `go vet ./...`, `go vet -tags=integration
      ./...`, `go run ./cmd/entity-id-audit .`, `task test:race`, `task test:integration`, `task schema:generate`
      with a clean `git diff schemas/ specs/`, and `openspec validate config-bucket-authority-namespace --strict`.
      Round-2 run, rebased on `origin/main`: every gate exit 0; race 160 ok / 0 FAIL / 20 no test files; integration
      160 ok / 0 FAIL / 20 no test files; `openspec validate --all --strict` 58/58; `task spec:properties` 449/449.
- [x] 3.3 Archive dry-run on a scratch copy of the tracked tree (the real change is never archived): the MODIFIED
      block keeps the scenario heading "A second environment cannot establish against the same bucket" with a
      rewritten body. `openspec archive config-bucket-authority-namespace -y` printed "Totals: + 2, ~ 3, - 0, → 0",
      "Specs updated successfully." and archived the change; before the fix it aborted on the renamed scenario.
      Re-run on `git archive HEAD` after the round-2 fixes: same result.
- [x] 3.4 `task e2e:core` at the final code revision `e5a10a56` (rebased on `origin/main` `3dc4ccbe`), run by the
      coordinating session on a clear host (`docker compose ls -q` 0, no `e2e.test` process, window announced to the
      sibling session first): exit 0, 116 s (2026-09-27 16:11:24Z to 16:13:21Z), 6 of 6 scenarios "completed
      successfully", no failure line. The round-2 review passed with amendments and docket 3 (Q6, Q7) was ruled (a)
      and (a), both doc-only, so this run is the breaking-change evidence (`docs/contributing/02-e2e-tests.md`
      § Breaking Changes) for the code that merges.

## 4. Codex round 1

Codex review of `22e80614` on PR #1404 (2026-09-27T18:42Z): one HIGH, two MEDIUM, all accepted as code fixes. The
archive commit was dropped so the fixes land before it; task 3.4's `e2e:core` run predates these code changes and is
re-run by the coordinating session at the new final code revision.

- [x] 4.1 HIGH, `config/key_family.go`: a member name outside the watch `<prefix>.*` was stored but never delivered.
      `NewKeyFamily` validates the prefix, and every family method the member name and completed key, with
      `natsclient.ValidateKVLiteralToken` / `ValidateKVLiteralKey` before any I/O; `Names` lists only one-token members.
      Tests: `TestBoundKeyFamilyRefusesBeforeAnyStoreAccess`, `TestNewKeyFamilyRefusesAPrefixThatIsNotOneToken`, and
      the nested-name half of `TestKeyFamilyDeliversSnapshotThenChanges` (real NATS). Migration note obligation 7 and
      the spec delta's family requirement carry the token rule.
- [x] 4.2 MEDIUM, `processor/rule/kv_config_integration.go`: `Start` publishes cancel and the completion fence before
      seeding, skips the initial reconcile once cancelled, and hands the fence to the loop; `Stop` never clears it, so
      every concurrent caller joins it. Tests: `TestConfigManagerStopDuringSeedingJoinsStart`,
      `TestConfigManagerConcurrentStopsBothJoinTheLoop` (explicit synchronization on the parked Stop's goroutine
      state, no sleeps); `go test -race -count=50 ./processor/rule/` at default GOMAXPROCS: ok, 202 s. `pkg/lifecycle`
      is the workflow harness (ADR-049) and has no start/stop fence primitive, so the fence stays local.
- [x] 4.3 MEDIUM, `config/key_family.go`: `Get`, `Put`, `Create`, `Delete` and `Names` refuse a nil context with a
      classified invalid error before store access. Test: `TestBoundKeyFamilyRefusesBeforeAnyStoreAccess/nil_context`.
- [x] 4.4 Mutation evidence (cp backup plus md5, restored checksum verified each time):
      M9a the member-name `ValidateKVLiteralToken` call deleted from `key()`: `TestBoundKeyFamilyRefusesBeforeAnyStoreAccess`
      panics (store reached) and `TestKeyFamilyDeliversSnapshotThenChanges` fails at the dotted `Put`; M9b the `Names`
      membership filter reverted to `name != ""`: `TestKeyFamilyDeliversSnapshotThenChanges` fails "a nested key is
      not a family member". M10a the pre-fix `kv_config_integration.go` from `d0eebc0a`: both
      `TestConfigManagerStopDuringSeedingJoinsStart` and `TestConfigManagerConcurrentStopsBothJoinTheLoop` fail "Stop
      returned while the work it must join was still running"; M10b `Stop` clearing `cancel`/`done` again:
      `TestConfigManagerConcurrentStopsBothJoinTheLoop` fails. M11a the nil-context check deleted from `member()` and
      M11b from `Names`: `TestBoundKeyFamilyRefusesBeforeAnyStoreAccess/nil_context` panics.
- [x] 4.5 Gates green before the push, on the Codex round-1 fix commits: `go build ./...`, `task lint`, `go vet ./...`,
      `go vet -tags=integration ./...`, `go run ./cmd/entity-id-audit .` (1334 candidates) each exit 0; `task test:race`
      exit 0, 160 ok / 0 FAIL / 20 no test files; `task test:integration` exit 0, 160 ok / 0 FAIL / 20 no test files;
      `task schema:generate` exit 0 with an empty `git diff schemas/ specs/`; `openspec validate
      config-bucket-authority-namespace --strict` valid; `openspec validate --all --strict` 58/58; `task
      spec:properties` 449/449.
