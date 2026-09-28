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
      *Superseded by 4.9: `Stop()` is now `Stop(ctx)`.*
- [x] 1.3 In the root, construct the one rule manager and register its family through `StartValidatedConfigManager`.
      Serve it to the tools, and wrap `runtimeManager` so that `StartAll` then seeds and reconciles, and `StopAll`
      stops the loop first (design D3).
      *Superseded by 4.9: the wrapper is deleted; the manager is the registered `rule-config` service.*
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
      *M2 and M7 are superseded by 4.9: the wrapper they mutated is deleted; M17 and M15 are their successors.*
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
- [x] 4.6 Owner ruling on #1188, docket 4 Q8 (b): `rule.ValidateDefinition` also requires the rule ID to be one KV
      literal token and refuses it with a classified invalid error naming the rule, so a file or inline rule with a
      dotted ID fails loudly (`rule.Config.Validate` for inline rules; the processor's preflight and Start for rule
      files) and the agent CRUD path (`SaveRule`) refuses before writing; the family's write-time refusal is the second
      line. Test: `TestConfigValidateRefusesARuleIDThatIsNotOneKVToken` (`a.b` refused with the message, `a-b`
      accepted). Migration obligation 7 states both lines and the corrected premise (`:` was never storable under
      nats.go's key grammar, so only `.` is newly refused); the spec delta's family requirement names the
      definition-validation refusal. M12 the check deleted from `ValidateDefinition` (cp backup plus md5, restored
      checksum verified): `TestConfigValidateRefusesARuleIDThatIsNotOneKVToken` fails "An error is expected but got
      nil". The ID check made `TestReferenceRuleConfigsUseDeclaredPredicates` fail: it decoded a rule-processor
      config file as one `Definition` and so validated an empty rule; commit `a319048e` (`4bab02f1` after the 4.7
      rebase) validates the file's `inline_rules` instead. Gates on this change, run after the 4.7 rebase at `b3687f48`:
      `go build ./...`, `task lint`, `go vet ./...`, `go vet -tags=integration ./...`, `go run ./cmd/entity-id-audit .`
      (1334 candidates) each exit 0; `task test:race` exit 0, 160 ok / 0 FAIL / 20 no test files / 0 DATA RACE; `task
      test:integration` exit 0, 160 ok / 0 FAIL / 20 no test files / 0 DATA RACE; `task schema:generate` exit 0 with an
      empty `git diff schemas/ specs/`; `openspec validate config-bucket-authority-namespace --strict` valid; `openspec
      validate --all --strict` 58/58; `task spec:properties` 449/449.
- [x] 4.7 Owner ruling on #1188 (issuecomment-5871896598): rebased onto `aa138957` for #1397 (PR #1408) and #1283 (PR
      #1409), no waiver. One conflict, `processor/rule/processor.go` in `d02306ae` (was `d2e194a6`): kept this change's
      `LoadedRuleDefinitions` and `HotReloadTarget` assertion and main's removal of `ruleRuntimeCommand` (the owner
      lane replaces it); in `cleanup` dropped both sides' block — the `hotReloadMgr.Stop()` call main had annotated,
      because this change deletes the component-internal manager, and the `cronDone` join, because main now joins
      cron at step 2 through `CronScheduler.Stop(ctx)`. `processor/rule/lifecycle_owner_test.go` merged cleanly.
      Nothing in this change calls `CronScheduler.Stop`; `go build ./...` and `go vet -tags=integration ./...` exit 0.
      Every rule-processor `Stop` with an unbounded context in a test file this change touches now runs under a named
      30s bound, the production root's `--shutdown-timeout` default (`internal/boot/flags.go`), since the processor has
      no stop budget of its own, and reports its error: `internal/boot/rule_hot_reload_integration_test.go:57`
      (`ruleProcessorStopBudget`) used at `:80`, the cleanup whose unbounded Stop turned the #1283 hang into a 20-minute
      CI timeout at `ada5c46a`; `processor/rule/lifecycle_owner_test.go:24` (`processorStopBudget`) and `:28`
      (`stopProcessorWithinBudget`), used at `lifecycle_owner_test.go:462` and
      `processor/rule/kv_hot_reload_integration_test.go:124`, `:152`, `:187`, `:221`, `:261`. Sweep (`grep -rn
      'Stop(context.Background())\|Stop(context.TODO())'` over `internal/boot config processor/rule test/e2e/config`,
      restricted to `git diff --name-only origin/main...HEAD`): those seven hits, no others. Commit `b3687f48`. A
      `-v` run through the canonical runner of `./internal/boot ./processor/rule ./test/testinfra` exit 0:
      `TestRootRuleManagerHotReloadsIntoTheProcessor`, the five `TestHotReload_*` and
      `TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock` PASS.
- [x] 4.8 Review round 3 on PR #1404 (PASS WITH AMENDMENTS at `60c3320c`, PR comment 2026-09-28):
      - MEDIUM-2, commit `020704e4`: `TestConfigManagerStopDuringSeedingJoinsStart` never binds the family, so any
        reconcile fails at `ListRules` before an apply and its "no apply" assertion could not fail (the reviewer's
        mutation survived 5/5). It now records the manager's logs and asserts "Initial rule reconcile failed" is never
        logged (`processor/rule/kv_config_stop_fence_test.go:148`), with a positive control that Start's own log was
        recorded (`:145`). M13 `if runCtx.Err() == nil {` replaced by `if true {` at
        `processor/rule/kv_config_integration.go:140` (cp backup plus md5, restored checksum
        `e022a84fe065d010f38bbfb673eda8f9` verified): the test fails 5/5, "a cancelled Start attempted its initial
        reconcile".
      - MEDIUM-3, commit `0e66ff12`: `KeyFamily.Names` logs a Warn naming a key under the prefix that is not one
        token (`config/key_family.go:228`), with the binding Manager's logger. Log-only, stated at the site: only an
        out-of-band writer can store such a key, the Warn repeats on every listing, and `config` has no metrics
        surface; ADR-098 decision 3 makes logs and metrics the substrate contract but names no per-skip metric.
        `TestKeyFamilyDeliversSnapshotThenChanges` asserts the Warns are exactly `rules.nested.name`
        (`config/key_family_integration_test.go:145`). M14 the Warn call deleted (cp backup plus md5, restored checksum
        `0dd3c3488528cd759235fceb5661b150` verified): that test fails, expected `[]string{"rules.nested.name"}`,
        actual `[]string(nil)`. The component-runtime-config scenario names the Warn (commit `87a5510f`).
      - NIT-4, commit `87a5510f`: migration obligation 7 says every byte outside the alphabet other than `.` (for
        example `:`) was never storable, and that a dotted ID was stored and applied only when another change or a
        restart reconciled the family.
      Gates, run with 4.9's on its final code commit `87bcfa78`: see 4.9.
- [x] 4.9 Owner ruling on #1188, docket 6 Q11 (b-full) (issuecomment-5873491391; the debt outside this change is #1415).
      Commit `87bcfa78` (code), `8c427184` (design, spec delta, proposal, migration note).
      - The rule `ConfigManager` is the registered framework service `rule-config`: the adapter `ruleConfigService`
        (`internal/boot/rule_config_service.go:31`, name at `:16`) embeds `*service.BaseService`, and
        `registerRuleConfigService` (`:81`, `RegisterInstance` at `:86`) binds `service.ComponentsImplementing` targets
        and refuses when the component manager is not yet registered. The root calls it at `internal/boot/run.go:339`,
        right after `configureAndCreateServices` and `service.ConfigureRulePackMutations`, and passes `manager` itself
        to `runUntilShutdown`; `service.Manager`'s registration-order `StartAll` and exact-reverse `StopAll` give D3.
        `ruleHotReloadRuntime`, the `ruleHotReload` interface and `internal/boot/rule_hot_reload_test.go` are deleted.
      - BREAKING: `rule.ConfigManager.Stop(ctx context.Context) error` (`processor/rule/kv_config_integration.go:166`):
        nil refused before any action; cancel, then nil on the completion fence or the context's error when it ends
        first; a completed repeated Stop is a nil no-op. Every test caller passes a named bound
        (`configManagerStopBudget`, 30s, the root's `--shutdown-timeout` default).
      - Tests: `TestRuleConfigServiceStartsAfterAndStopsBeforeTheComponents` (a real `service.Manager`, a component
        manager stand-in and the production registration; start order components then rules, stop order rules then
        components); `TestConfigManagerStopReturnsWhenItsContextEnds` (real NATS; a target holding
        `ApplyConfigUpdate`, a 100 ms Stop returns `context.DeadlineExceeded`, the loop exits on release);
        `TestConfigManagerStopRefusesANilContext`; `TestRegisterRuleConfigServiceRefusesBeforeTheComponentManager`.
        `TestBinaryBootOrder` pins `configureAndCreateServices` → `registerRuleConfigService(manager, ruleManager, …)`
        → `runUntilShutdown(…, manager, …)` and, in `registerRuleConfigService`, the `ComponentsImplementing` targets
        and `RegisterInstance(ruleConfigServiceName, newRuleConfigService(rules, targets, logger))`.
      - Mutations (cp backup plus md5, restored checksum verified each time): M15 the `RegisterInstance` call deleted:
        `TestBinaryBootOrder` fails "call manager.RegisterInstance absent or out of order". M16 `Stop` waits on the
        fence ignoring ctx: `TestConfigManagerStopReturnsWhenItsContextEnds` fails "Stop did not return within 5s of its
        100ms deadline". M17a the `registerRuleConfigService` call moved before `configureAndCreateServices` in
        `run.go`: `TestBinaryBootOrder` fails "call registerRuleConfigService absent or out of order". M17b the guard
        deleted and the ordering test's registrations swapped: `TestRuleConfigServiceStartsAfterAndStopsBeforeTheComponents`
        fails "start order must be [components, rules]". Test (a) drives the production registration function, but the
        order relative to the component manager is set by `run.go`'s statement order, which only the AST test sees.
      - Gates, run at `8c427184` (the code of `87bcfa78`; only documents follow it): `go build ./...`, `task lint`, `go vet ./...`,
      `go vet -tags=integration ./...`, `go run ./cmd/entity-id-audit .` (1334 candidates) each exit 0; `task test:race`
      exit 0, 160 ok / 0 FAIL / 20 no test files / 0 DATA RACE; `task test:integration` exit 0, 160 ok / 0 FAIL / 20 no
      test files / 0 DATA RACE; `task schema:generate` exit 0 with an empty `git diff schemas/ specs/`; `openspec validate
      config-bucket-authority-namespace --strict` valid; `openspec validate --all --strict` 58/58; `task spec:properties`
      451/451.

- [x] 4.10 Review round 4 on PR #1404 (CHANGES REQUESTED at `03a330d0`, no production defect; PR comment 2026-09-28).
      Rebased onto `7303858d` (#1413, tests and docs only, no conflict); 4.9's `87bcfa78` is now `64a34476` and
      `8c427184` is `7c341428`. Pins in 4.9 are re-measured after the NIT-3 doc comment.
      - HIGH-1, the CI red: `startedRuleProcessor` started the processor with the test's context, which the test's
        deferred cancel and `t.Context()` end before cleanups run, so the processor's Stop cleanup always ran on the
        component contract's abort path (`component/lifecycle.go:52-56`), where a non-nil terminal result is
        legitimate; the test's `t.Errorf` was the defect. Controlled shutdown per the contract keeps the Start context
        live until a bounded Stop returns, so the helper now owns its Start context and cancels it in a cleanup
        registered before the Stop cleanup (`internal/boot/rule_hot_reload_integration_test.go:87`; cleanups run
        last-in first-out). Evidence: registering that cancel after the Stop cleanup (cp backup plus md5, restored)
        fails at once, `rule_hot_reload_integration_test.go:92: rule processor Stop within 30s: context canceled`;
        with the fix, `scripts/run-integration-tests.sh ./internal/boot/ -count=20 -v` exit 0, 20/20 PASS of
        `TestRootRuleManagerHotReloadsIntoTheProcessor`, 0 FAIL, 0 DATA RACE.
      - MEDIUM-1: `TestRuleConfigServiceStopIsBoundedAndReportsStoppedOnlyWhenJoined`
        (`internal/boot/rule_config_service_integration_test.go:52`, real NATS) drives the adapter with a target
        holding `ApplyConfigUpdate`: a 100 ms Stop returns `context.DeadlineExceeded` and `Status()` is not stopped;
        after release a second Stop returns nil and the status is stopped. M18 `s.rules.Stop(ctx)` deleted from the
        adapter's Stop (`internal/boot/rule_config_service.go:70`, cp backup plus md5, restored checksum
        `b7b280b15d43d3e746a8fbf6fd284a15`): that test fails "a Stop whose bound wins returns its context's error, got
        <nil>" (it survived every test at `03a330d0`).
      - NIT-3: the adapter doc and migration obligation 4 state that `rule-config` starts after every configured
        service and stops before all of them, publishes `health.service.rule-config` to `HEALTH` every 5 s, raises
        the startup `Admitted` count by one, and holds readiness for the seeding and initial reconcile (inside
        `StartAll`, before startup commits, as each processor's own seeding did in beta.162).
      - NIT-4: rows 1.2, 1.3 and 3.1's M2 and M7 are marked superseded by 4.9; 4.8's M13 pins are corrected.
      - MEDIUM-2 (health for at most one tick after a Stop whose bound won): owner ruling on #1188, docket 7 Q12 (a),
        commit `a4dd453a`: one doc sentence at the adapter's Stop (`internal/boot/rule_config_service.go:67`) says such
        a Stop leaves the service Running and healthy until its reconcile loop exits, and that no reader acts on it:
        `beginStopping` has already moved the diagnostic mux and startup snapshot to "stopping", so the only reader
        left is at most one `publishServiceHealth` tick on `health.service.rule-config`, after the shutdown deadline,
        before `stopHealthPublisherMode`. No `Health()` override. The migration note drops its parenthetical about
        this change's unreleased first cut.
      - Non-test Go is functionally identical to `87bcfa78` (now `64a34476`): `git diff 87bcfa78` over non-test `.go`
        files touches only `internal/boot/rule_config_service.go`, and every changed line there is a comment.
      Gates, run at `a4dd453a` (the final code-bearing commit): `go build ./...`, `task lint`, `go vet ./...`, `go vet
      -tags=integration ./...`, `go run ./cmd/entity-id-audit .` (1334 candidates) each exit 0; `task test:race` exit 0,
      160 ok / 0 FAIL / 20 no test files / 0 DATA RACE; `task test:integration` exit 0, 160 ok / 0 FAIL / 20 no test
      files / 0 DATA RACE; `task schema:generate` exit 0 with an empty `git diff schemas/ specs/`; `openspec validate
      config-bucket-authority-namespace --strict` valid; `openspec validate --all --strict` 58/58; `task
      spec:properties` 452/452.