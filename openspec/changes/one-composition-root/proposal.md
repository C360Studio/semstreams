# Change: one composition root

## Why

SemStreams ships two hand-copied boot roots. `cmd/semstreams/main.go` (838 lines) and `cmd/e2e-semstreams/main.go`
(956 lines) define the same eight pre-`ComponentManager.Start` steps — vocabulary, components, payloads, graph runtime,
personas, tools, lifecycle, services — in the same order, with two helpers code-identical and two differing by a
handful of lines (`docs/proposals/gh1301-composition-inventory.md` § 1c). The order that makes them work exists only
as prose (`processor/agentic-tools/executors/register.go:54-56`, "Pattern-B step N") and as line position in two
files. The copies have also drifted: different flag defaults (`json`/8083 vs `text`/6060), different Phase-A logging
compositions, a shutdown guard and health listener only one of them has. Seven e2e-only registrations and hooks are
gated three different ways — root selection, a build tag, a build tag plus an env var — across eleven tagged Go files
and four runnable Dockerfile targets, so where an e2e hook lands was non-obvious enough to stall PR #1360 (#1249,
2026-09-22). The comment at `cmd/semstreams/main.go:114` says the intent is "one composition root"; the e2e copy
contradicts it.

The owner ruled on 2026-09-22 (#1249, transcribed on #1301): B — the tier→binary→gate table and its contract test,
landed in PR #1360 — then D: one shared boot with thin mains, the seven e2e-only items as composer options the e2e
main enables from env and the production main cannot, the build tags and per-hook Dockerfile targets gone, targets
down to two. #1301 is placed in `v1.0.0-beta.163` and gates the tag.

## What changes

- **One boot.** `internal/boot` holds `Options`, `ParseFlags`, `Production()`, and `Run()` — `cmd/semstreams`'s
  `run()` and flag parser moved, with the production copy winning every divergence between the two roots, including
  the Phase-A logging composition and the JSON log default. Both mains become ~60 lines: flags → options → `boot.Run`.
- **Seven env-enabled options.** `internal/e2eboot.FromEnv` reads `SEMSTREAMS_E2E_{EXAMPLES,MISSION,LIFECYCLE_SEED,
  LESSON_CURATION,PROCESS_BARRIER,MILESTONE_PROBE,SLOW_CONSUMER}` (a nonempty value enables) and appends to the
  `Options` extension slices. Only this package imports the e2e harness, the examples, and the fixture and mission
  packages.
- **Two Dockerfile targets.** `production` (`cmd/semstreams`) and `e2e` (`cmd/e2e-semstreams`); the
  `process-barrier-builder`, `e2e-process-barrier`, `slow-consumer-builder`, `e2e-slow-consumer` stages and the
  `e2e_process_barrier` / `e2e_slow_consumer` build tags are deleted. The agentic and slow-consumer tiers boot the
  `e2e` target with their hook's variable; every e2e compose service sets exactly its tier-table row's variables.
- **Observed, not predicted.** Two contract tests: the production binary's `go list -deps` closure holds no harness or
  e2e package while the e2e binary's holds every hook package; `FromEnv` with nothing set equals `Production()` on
  every field, and each variable grows exactly its slices. The B tier table's Gate column becomes an env set, its
  Dockerfile reader asserts two targets and zero `-tags=`.
- **Spec.** `payload-registry`'s tier-table requirement is MODIFIED (rule paragraph, two rows' target, every e2e row's
  gate, three named clause edits); `application-logging`'s E2E Phase-A scenario is MODIFIED (the E2E binary composes
  the production Phase-A; `NewE2EPhaseA` is deleted); `framework-composition` gains one ADDED requirement for the
  shared boot.

## Impact

- Packages: `internal/boot` and `internal/e2eboot` (new, unexported — no Tier 1 line); `cmd/semstreams`,
  `cmd/e2e-semstreams` (thin); `internal/e2eslowconsumer` (tag removed); `internal/bootstrapobservability`
  (`NewE2EPhaseA` deleted); `test/e2e/harness/milestoneprobe` (env read moves to `FromEnv`).
- Build and test infrastructure: `docker/Dockerfile`, eight `docker/compose/*.yml` services, `Taskfile.yml`
  (`check:push`'s tagged vet line), `test/contract/e2e_tier_binary_contract_test.go`,
  `internal/maxdelivery/boot_order_test.go`, `test/e2e/scenarios/ops/composition_root_contract_test.go`.
- Tier behaviour: five e2e-target tiers whose configs enable `log-forwarder` (structural, statistical/throughput,
  semantic, lifecycle, research-graph) start forwarding logs as the production binary already does; the slow-consumer
  tier keeps its JSON logs and `semstreams_log_entries_total` counter because the e2e binary now composes production's
  Phase-A. The e2e binary's vocabulary stays a superset of production's through three `init()` registrations
  (`design.md` § 10 OQ2).
- Docs: `docs/contributing/02-e2e-tests.md` tier section, `docs/concepts/15-payload-registry.md`,
  `docs/basics/05-first-processor.md`, `.agents/skills/new-payload/SKILL.md`, the two role contracts' registration
  sentence.
- Not breaking: no exported Go surface changes; the removed targets are built only by our compose files. No sister
  migration note. Sisters do not migrate to the shared boot under this change (it is internal by ruling); that goal
  stays recorded as a residual (`design.md` § 11.1).
- E2E: every tier's compose contract changes; `design.md` § 8 names the gate.
