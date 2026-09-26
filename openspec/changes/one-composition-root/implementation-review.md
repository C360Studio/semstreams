# Implementation review: `one-composition-root` (#1301, PR #1390), one round

Independent `semstreams-reviewer` pass (Opus, read-only), 2026-09-26, commissioned by the coordinating session
against `design.md` as amended by `reconciliation.md`. Reviewed at `f44b51e0` (code commits `f3bf341d..e1f1bd8b`).
Filed verbatim; the fix-pass commit that follows applies every finding (`reconciliation.md` § Fix pass).

---

**Reviewed sha: `f44b51e0`** (HEAD; the code commits are `f3bf341d..e1f1bd8b`, plus the reconciliation doc commit). Merge-base is `origin/main` `8d53c084`. The tree was clean at the start and still is (`git status --porcelain` = 0). I wrote no files in the repo; two binaries were built into the scratchpad.

## Verdict: PASS WITH AMENDMENTS

There are no BLOCKING or HIGH findings. The four MEDIUMs, including the known row 4, and the NITs all fit one fix commit.

## MEDIUM

**1. `internal/boot/options.go:48-50`, `internal/boot/run.go:135` and `createNATSClient` (`run.go` ~402-420): `NATSURLs` is a dead field (reconciliation row 4, confirmed).**
- Nothing writes it. `git grep NATSURLs -- '*.go'` finds only its declaration and one read at `run.go:135`.
- The move also added an `override` parameter and a `case override != ""` branch to `createNATSClient` just to carry it.
- **Fix:** delete the field, the `override` parameter and that case. That restores `origin/main`'s `SEMSTREAMS_NATS_URLS` → config → default chain exactly.

**2. `internal/boot/resources.go:28-30`: no test checks that responder closers get closed.**
- Design § 2.3 and the brief say the closers close on every shutdown path, abort included. The code does this: `close()` loops over `r.responders`, and `abortOnReturn` and `stopAndCloseRuntime` both reach `close()`.
- But no test mentions responders. `git grep -n 'responders\|Responders' -- 'internal/boot/*_test.go'` returns 0 hits, and `root_resources_test.go` covers only the NATS client.
- Deleting the loop would leave every unit test green. The ops tier would not notice either, because it never observes an unsubscribe.
- **Fix:** add one case to `root_resources_test.go` with a counting `io.Closer` in `rootResources.responders`, and assert that `abortOnReturn` closes it exactly once. Mutation check: delete the loop and the test must go red.

**3. `test/contract/e2e_tier_binary_contract_test.go:577-580`: a stale comment survived a correction.**
- It says: "`milestoneprobe.Register` returns without installing the handler when os.Getenv reads ""…".
- D5 removed that read. `git grep -n 'os.Getenv\|LookupEnv' -- test/e2e/harness/milestoneprobe` → 0 hits, and `milestoneprobe.go`'s `Register` no longer checks the variable.
- The payload-registry delta (`:87-88`) was corrected to cite the E2E binary's value rule; this test comment was not.
- **Fix:** reword it to say that `e2eboot.FromEnv` enables an option only on a nonempty value, so `NAME=` leaves the tier's hook unarmed.

## NIT

**4. `internal/boot/flags.go:92`: a behaviour change the move introduced and the design does not name.**
- On a malformed flag, the usage text now prints blank build metadata. `fs.Usage = func() { printDetailedHelp(BuildInfo{}) }`.
- Measured: `e2e-bin --bogus` ends with `Version: ` / `Build: ` (empty). `origin/main`'s production binary printed its ldflags `Version` and `BuildTime` there.
- **Fix, either one:** name it in `reconciliation.md` as a D15 consequence (the owner's doc-sentence rule), or pass `BuildInfo` into `ParseFlags`.

**5. Exported names with no caller outside their own package.**
- `internal/e2eboot/fromenv.go:58-66` `Names()`: only `fromenv_test.go:47,109-110` calls it. Unexport it.
- `internal/boot/banner.go:24-65` `Spinner` / `NewSpinner`: carried over as-is from `package main`, and only `run.go` uses them. Optional.
- Every other export has a caller in one of the two mains: `Options`, `CLI`, `BuildInfo`, `Production`, `ParseFlags`, `RegistryFor`, `Run`, `FromEnv`.

**6. Task truth in `tasks.md`.**
- 1.3 is ticked but still reads `RegistryFor(opts, full bool)`. The code is `(opts, cfg, full)`, per row 2. Reword it.
- 0.3 is unticked but true: the PR body carries `implemented-by`, and the merge-base is `origin/main` `8d53c084`.
- 7.1's gate results live only in the developer's hand-back, so they are not an in-tree artifact. I record them as unverified. What I re-ran myself is listed below.

## Checked against the brief

1. **The move is a move.**
   - The `diff` of `origin/main:cmd/semstreams/main.go` against `internal/boot/run.go` shows only: the nine extension loops; `runtimeCtx` taken as a parameter, with nil rejected at `run.go:47`; `validateFlags` / `--version` / `--help` inlined; `opts.Build` replacing the package-main variables; `setupRegistriesAndManager` → `RegistryFor`; `postStart` in `runUntilShutdown`, called after `StartAll` and before `StartHealthListener`. Its failure goes through the bounded `stopAndCloseRuntime`.
   - The e2e differences (Phase-A, the guard, bounded abort, the health listener, the `--version` name, the verbs' catalog) are all named in D7, D8, D14, D15 or reconciliation row 9.
   - In `flags.go`, the global `flag` became a local `FlagSet`, which has the same set of flags. No linked library registers global flags (grep → 0). The one unnamed change is NIT 4.
2. **Application points.** All nine are where the brief and § 2.3 put them: `ConfigPatches` `run.go:81` before `cfg.Validate` at `:87`; `AfterConnect` inside `connectNATSWithSpinner` after `ConnectClient` and before `StartValidatedConfigManager`; `Components` `registry.go:35`; `Payloads` `run.go:761` beside `payloadbuiltins`; `Tools` `:255` after `RegisterBuiltins`; `Workflows` `:279` after `NewManager` and before `agentrun.Register`; `Responders` `:221` after `WireGraphRuntime` under `runtimeCtx`; `MilestoneHooks` inside `registerMilestoneService` before the service is registered; `PostStart` after `StartAll` with the operation context (`signal_shutdown_test.go:207` checks it gets the same context `StartAll` got).
3. **`FromEnv`.** Reads exactly the seven names, starting from `boot.Production`; enables only on `value != ""`, so unknown names are ignored with no refusal; `LIFECYCLE_SEED` passes its suffix through to `seedMission`; `milestoneprobe` no longer reads the environment.
4. **The tests prove, they do not reconstruct.** I1 compares fields by reflection, the embedded `CLI` included. I2 compares slice lengths and has an empty-value leg. I4 lists dependencies with `go list -deps` and has a non-vacuity floor. I7 is behavioural and is paired with `boot_order_test`'s connect-before-config-manager order check. I8's check at `boot_order_test.go:106` is argument identity on `phaseLogging.Steady`, and `bootstrap_test.go:93-94` checks the counter. Every mutation row in tasks 1.4, 2.3 and 5.4 is credible from the test text; the cited lines match the files as they stood when recorded (`fromenv_test.go` at `117efed1` has its `Errorf` calls at 71, 118 and 131).
5. **Row 8 (I6 reads `fromenv.go` from source): acceptable.** It fails closed: the table must be non-empty, fields must be keyed, and each name must be a string literal; `FromEnv` iterates only that table. The one false-green left: a variable read outside `options`, which neither I6 nor I2 would see — a doc comment covers it. The `milestoneprobe.EnvVar` test is behavioural (set `EnvVar` → exactly one hook). Together with I4's converse, that is enough. I did not reproduce the claimed `init()` collision with the predicate ratchet test.
6. **Context ownership.** `go test ./test/contract/ -run Context` passes. No struct in `internal/boot` or `internal/e2eboot` holds a `context.Context`; `Options` holds only funcs. The two roots are `context.Background()` in the two mains; the other two uses are the timeout-only finalizers at `resources.go:49` and `run.go:628`, which predate this change.
7. **`RegistryFor`.** With `full`, it matches the old `fullComponentRegistry`. Without it, it matches the old `setupRegistriesAndManager`'s `Selected` gating. `opts.Components` is added in both cases.
8. **Dockerfile and compose.** Three `FROM` stages (builder, production, e2e), zero `tags=`. Every compose service read against the delta table: all twelve rows match. Agentic and slow-consumer use `image: …:e2e-test` and `target: e2e`. `lifecycle.yml:71` sets `SEMSTREAMS_E2E_LIFECYCLE_SEED`, no `--lifecycle-seed` remains. `Taskfile.yml` has no `tags=e2e_`; the `check:push` description is corrected.
9. **Docs and guards.** `openspec/specs` untouched. `cmp CLAUDE.md AGENTS.md` = 0; `internal/agentprofiles` green. The 6.1 sweep's remaining hits are all history, a file name or the tool name. Built from `f44b51e0`: `validate configs/hello-world.json` exits 1 without `SEMSTREAMS_E2E_EXAMPLES` and 0 with it, matching `docs/basics/05`. `release_smoke_test` passes.
10. **Rulings (OQ1/OQ2).** No `init()` removed, no `Vocabulary` slice. `configs/` unchanged. In `test/e2e/scenarios`, only comments changed plus the ops test that reads the source path. All non-internal Go edits are comments. `openspec validate --strict` passes.

**What I ran, all exit 0:** `go vet` over the boot, e2eboot, cmd, contract, harness and adjacent packages; `go test -race -count=1` over `internal/{boot,e2eboot,bootstrapobservability,maxdelivery,e2eslowconsumer,agentprofiles}`, `./cmd/...`, `test/e2e/harness/...`, `test/release`, `test/e2e/scenarios/ops`; `go test ./test/contract/` (it read the table from the in-flight change's delta, 12 rows).

**Not run, as the brief asked:** `task test:integration`, full `go test -race ./...` and every e2e tier. OQ1's `e2e:all` evidence (task 7.2) is still owed at the final revision after this fix pass. On PR CI, Lint, Build, Schema, Tier 1, the slow-consumer ladder job and the statistical ladder job pass; `Test` was pending.
