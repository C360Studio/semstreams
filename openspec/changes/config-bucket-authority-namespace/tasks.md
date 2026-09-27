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
      foreign record into the declared pair's own bucket. Design R5 records the minted-identifier case, which is
      handled by the doc sentence rather than code.
- [x] 2.5 Update `test/contract` (resolving owners), `test/e2e/config` (`PlatformIdentityBucket(declared)`), the
      scenarios that read or write the bucket, and crud-tools.
- [x] 2.6 Update the docs that spell the bucket: `config/README.md`, `docs/operations/05-model-registry.md`, and the
      comments at `internal/boot/run.go:702` and `processor/rule/kv_config_integration.go:30-40`. The migration note
      gets a new section and amends the ADR-104 obligations.

## 3. Evidence

- [ ] 3.1 Mutation evidence (cp backup plus checksum): the family delete mapping, the catalog family match, the root
      loop's stop-before-`StopAll`, and the name derivation.
- [ ] 3.2 Gates green before each push: `go build ./...`, `task lint`, `go vet ./...`, `go vet -tags=integration
      ./...`, `go run ./cmd/entity-id-audit .`, `task test:race`, `task test:integration`, `task schema:generate`
      with a clean `git diff schemas/ specs/`, and `openspec validate config-bucket-authority-namespace --strict`.
