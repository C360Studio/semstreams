# Tasks: one composition root

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof. Landing
choreography (review, archive, merge) lives on PR #1390's description, not here. Pins are the inventory's
(`docs/proposals/gh1301-composition-inventory.md`, base `df3effb2`); `task inventory:verify` on that file goes RED as
the implementation lands — pins are pre-change evidence and are never re-pinned. Every developer gate list carries
`go run ./cmd/entity-id-audit .` (CI's Lint job runs it; `task lint` does not). Standing owner rule: an edge case goes
to a doc sentence or "not supported" before it gets code.

## 0. Design acceptance (precondition — nothing below starts before 0.3)

- [x] 0.1 Independent pre-owner design review of `design.md` (one round, `design-review.md` at `dfaeb69c`): DESIGN
      CHANGES REQUESTED — every finding applied in the amendment commit; the shape is unchanged.
- [ ] 0.2 The § 10 docket (OQ1, OQ2) is posted on #1301 and the owner rules; each ruling is transcribed under the
      docket as "transcribed from session … owner's words govern" if given in-session. The two "decided, not asked"
      items (D8/D14 Phase-A reversal, D15 parser convergence) are named in the same post.
- [ ] 0.3 PR #1390's body carries `implemented-by: <persona>` before the first implementation push; the worktree is
      rebased onto `origin/main` (`git fetch origin main` first).

## 1. `internal/boot`

- [ ] 1.1 Move `cmd/semstreams/main.go:115-380` (`run`) and its helpers into `internal/boot/run.go`; `Options` and
      `Production()` per `design.md` § 2.1; the nine extension slices applied at the phases named in § 2.3. Production
      copy wins every divergence (§ 2.3, D8) — including Phase-A: `NewProductionPhaseA` + `Steady(forwardingHandler)`
      for both binaries.
- [ ] 1.2 `cmd/semstreams/flags.go` → `internal/boot/flags.go` as `ParseFlags` (D15); `Options` gains `DebugPort` and
      `Build`; each main copies its own `Version`/`GitCommit`/`BuildTime` into `Options.Build` (the `-X main.*`
      ldflags in `docker/Dockerfile:48-58` and `.github/workflows/release.yml:58` keep working — verify with
      `go run ./cmd/semstreams --version` after a `-ldflags` build).
- [ ] 1.3 `RegistryFor(opts, full bool)` (D9) replaces both `fullComponentRegistry`s; both mains' composition verbs call
      it.
- [ ] 1.4 Move the tests listed in `design.md` § 7 row 1; delete the e2e duplicates and
      `cmd/e2e-semstreams/bootstrap_observability_test.go` (it asserts the retired E2E Phase-A). Delete
      `bootstrapobservability.NewE2EPhaseA` and `internal/bootstrapobservability/bootstrap_test.go:102-133` (D14).
      Re-point the production half of `internal/maxdelivery/boot_order_test.go` (`:24`) at `internal/boot/run.go`;
      delete its e2e half (`:111-158`).
- [ ] 1.5 `go test -race ./internal/boot/... ./internal/bootstrapobservability/... ./internal/maxdelivery/...` and
      `go vet ./...` green with **no** `-tags=`.

## 2. `internal/e2eboot`

- [ ] 2.1 `FromEnv(cli boot.CLI, build boot.BuildInfo, lookup func(string) (string, bool)) boot.Options` for the seven
      names in `design.md` § 2.2; a nonempty value enables; unknown `SEMSTREAMS_E2E_*` names are ignored (D4).
- [ ] 2.2 One file per option holding the moved body (`design.md` § 7 row 2). `milestoneprobe.Register` no longer reads
      the env (D5, `milestoneprobe.go:199`).
- [ ] 2.3 Tests I1, I2, I6 (`design.md` § 6) plus the three untagged config-patch tests from
      `process_barrier_e2e_test.go`. Mutation evidence per site: for I1, add one extension to `Production()` and watch
      the parity test fail; for I2, set `SEMSTREAMS_E2E_EXAMPLES=` (empty) in a row and watch it enable nothing.

## 3. Thin mains and deletions

- [ ] 3.1 `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` reduced to `boot.ParseFlags` → options → `boot.Run`;
      the e2e `parseCLI`, `getEnvOrDefault`, `text` default, `DebugPort: 6060` and `--lifecycle-seed` are gone.
- [ ] 3.2 Delete the six tagged hook files and their three tests in `cmd/semstreams/`; `internal/e2eslowconsumer/probe_e2e.go`
      → `probe.go`, tag removed, same for its test (D10). `git grep -n 'go:build e2e_'` → 0.
- [ ] 3.3 `Taskfile.yml:163` removed; `check:push` description (`:152`) corrected.
- [ ] 3.4 `test/e2e/scenarios/ops/composition_root_contract_test.go:26` reads the file that now carries the
      `persona.LoadFromDirectory` call (`internal/boot/run.go`).

## 4. Dockerfile and compose

- [ ] 4.1 `docker/Dockerfile:182-215` stages deleted; `grep -c '^FROM' docker/Dockerfile` drops by 4; `grep -c 'tags='`
      → 0.
- [ ] 4.2 Compose edits per `design.md` § 7 row 7 — each e2e service sets exactly its row's variables; agentic and
      slow-consumer take `image: c360studio/semstreams:e2e-test` and `target: e2e`; `lifecycle.yml:62` flag →
      `SEMSTREAMS_E2E_LIFECYCLE_SEED`.
- [ ] 4.3 `go test ./test/contract/ -run TestE2ETierTableMatchesComposeAndDockerfile` green against the MODIFIED
      table (task 5.1) — the test is the migration checklist.

## 5. Spec deltas and contract tests

- [ ] 5.1 `specs/payload-registry/spec.md` MODIFIED block: rule paragraph, rows, the three named clause edits per
      `design.md` § 7 row 9; every other scenario byte-identical to `openspec/specs/payload-registry/spec.md:10-103` at
      the rebase base (`openspec validate one-composition-root --strict` valid).
- [ ] 5.2 `specs/framework-composition/spec.md` ADDED requirement with scenarios I1, I2, I4, I6.
- [ ] 5.3 `specs/application-logging/spec.md` MODIFIED: the E2E scenario says the E2E binary composes the production
      Phase-A (I8); the requirement text and scenario 1 byte-identical to `openspec/specs/application-logging/spec.md:6-21`.
- [ ] 5.4 `test/contract/e2e_tier_binary_contract_test.go`: Dockerfile reader → two runnable targets, zero `-tags=`; Gate
      column → env set; `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` → `TestProductionRootClosureHoldsNoE2EHarness`
      (D11, precedent `test/contract/core_composition_deps_test.go:43`), absorbing `TestProductionBinaryExcludesExamplePackages`
      (`:30-34`). Mutation evidence: add a harness import to `cmd/semstreams/main.go`, watch it fail, revert (`cp`
      backup + checksum, never stash).

## 6. Docs

- [ ] 6.1 Sweep: `git grep -n -E 'cmd/(e2e-)?semstreams/main\.go|e2e-semstreams/main\.go|e2e_process_barrier|e2e_slow_consumer|e2e-process-barrier|e2e-slow-consumer|buildPayloadRegistry|registerExampleComponents|--lifecycle-seed|NewE2EPhaseA' -- docs/contributing docs/concepts docs/basics .agents openspec/specs CLAUDE.md AGENTS.md README.md '*_test.go'`
      → every hit updated or recorded here as history (`docs/proposals/*`, ADR-051/058, `migration-beta18.md` are
      history and stay). The `.agents/contracts/semstreams-{developer,reviewer}.md:246` sentence names the composer.
- [ ] 6.2 `docs/contributing/02-e2e-tests.md:180-220` navigation copy matches the MODIFIED table.

## 7. Gates before each push, and the e2e evidence

- [ ] 7.1 `task lint`, `go run ./cmd/entity-id-audit .`, `task schema:generate` + `git diff schemas/ specs/` empty,
      `go test ./test/contract/...`, `go test -race ./...`, `task test:integration` — before every push.
- [ ] 7.2 E2E per the OQ1 ruling; default (a): `docker compose ls` = 0, then `task e2e:all` at the final revision; the
      tier log's own `exit=` line is the result. Record the revision and durations here: ____. Watch the five tiers
      that newly forward logs (`design.md` § 2.3) for any scenario reading container log text.
- [ ] 7.3 Archive + spec sync is the last content commit; the squash body is authored (`--body-file`) and checked with
      `git log -1 --format=%B origin/main` after the merge; grep it for closing keywords without `\b`.
