# Design: one composition root, seven env-enabled boot options, two Dockerfile targets

Change `one-composition-root` · issue #1301 (milestone `v1.0.0-beta.163`, gates the tag) · claim PR #1390 ·
inventory `docs/proposals/gh1301-composition-inventory.md` (257 pins, `inventory:verify` 257/257 at `86803eb9`, base
`df3effb2`). Every `path:line` below is a pin in that inventory unless marked *(new, this pass)*.

Read-only design pass (Fable, 2026-09-26). Nothing here is approved: independent design review, then the owner's
ruling on § 10, precede implementation.

## 0. What is ruled, verbatim

Owner, 2026-09-22, on #1249 (issuecomment-5777628692): *"on 1249 - i am okay with option B then D but it reads like we
need to get e2e patterns established and migrated sooner rather than later. i do not tihnk we can afford to continue
to push it beyond this tag or we risk more e2e cruft."*

Scope bound transcribed on #1301 (2026-09-22): one shared boot (semdev's `internal/boot/boot.go` shape — unexported,
so no Tier 1 surface) with thin mains; the examples/fixtures registration policy, mission, `--lifecycle-seed`, the
lesson-curation responder and the three e2e hooks (process barrier, slow-consumer probe, milestone probe) become
composer OPTIONS the e2e main enables from env and the production main cannot; the `e2e_process_barrier` /
`e2e_slow_consumer` build tags and the per-hook Dockerfile targets go, targets drop to two, compose files point at
those two; the 12 tiers themselves are not redesigned; B's tier table and contract test (PR #1360) are the migration
checklist and the seam pin; #1249 docket questions 3 and 5 close here.

Standing owner notes applied: keep complexity as low as possible — an edge case goes to a doc sentence or "not
supported" before it gets code (2026-09-22); more than one review round on a design is a signal to cut to the smallest
shape (#1372).

## 1. The problem, measured

1. **Two copies of one boot.** `cmd/semstreams/main.go` (838 lines) and `cmd/e2e-semstreams/main.go` (956 lines)
   differ by 771 raw lines; `createServiceDependencies` and `configureAndCreateServices` are code-identical
   (inventory § 1c), `setupRegistriesAndManager` differs by one inserted 5-line block (`cmd/e2e-semstreams/main.go:719-720`),
   `runUntilShutdown` by four hunks. The production copy alone carries the pre-start shutdown guard
   (`cmd/semstreams/main.go:655-656`) and the health listener (`:675`); the e2e copy alone carries a `postStart` hook
   (`cmd/e2e-semstreams/main.go:852`). Five tests are duplicated across the roots (`signal_shutdown_test.go` ×2,
   `bootstrap_observability_test.go` ×2, `milestone_wiring_test.go` ×2 — *(new, this pass)* `grep -n '^func Test'` over
   both roots).
2. **Eight pre-`Start` steps ordered by convention.** The dependency edges — payloads → `WireGraphRuntime` → tools;
   `lifecycle.NewManager` → `agentrun.Register`; rule processors → `ConfigureRulePackMutations` — exist as prose
   (`processor/agentic-tools/executors/register.go:38,54-56`, "Pattern-B step N") and as the accident of line order in
   two files (inventory § 1d/1e).
3. **Three gate styles for seven e2e-only things.** Root selection (examples/fixtures, mission, lesson curation — no
   env, no tag; inventory § 3a-3c), build tag (process barrier, slow-consumer probe; § 3d-3e), build tag plus env
   (milestone probe; § 3f). Eleven tagged Go files (§ 3g), four runnable Dockerfile targets over three builders
   (§ 3h), a third tagged vet tree (`Taskfile.yml:163`) that is the only untagged-build check the hooks get.
4. **Two silent classes.** A nil `ToolDependencies.ComponentRegistry` makes `list_components` skip with a WARN
   (`processor/agentic-tools/executors/register_component_catalog.go:18`). A hook gated on `os.Getenv(NAME)` is
   silently unarmed by a misspelled name (the B contract test guards values — `NAME=` counts as unset — but a compose
   file can set `SEMSTREAMS_E2E_MILESTONE_PORBE=1` and nothing refuses it; *(new, this pass)*
   `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` reads the env itself).
5. **A third registry builder.** Each root's `fullComponentRegistry` (`cmd/semstreams/main.go:100`,
   `cmd/e2e-semstreams/main.go:98`) builds the offline verbs' registry a third way beside boot's `Selected(cfg)` gating
   (`:307`) — the #1107 sibling.

## 2. Target shape (D, made concrete)

```
cmd/semstreams/main.go          parse flags → boot.Run(ctx, boot.Production(cli))            (~60 lines)
cmd/e2e-semstreams/main.go      parse flags → opts, err := e2eboot.FromEnv(cli, os.LookupEnv)
                                → boot.Run(ctx, opts)                                          (~60 lines)
internal/boot                   Options, Production(), Run(), the eight steps in dependency order,
                                registryFor(opts, full bool) for the verbs and for boot      (moved, not rewritten)
internal/e2eboot                FromEnv: seven SEMSTREAMS_E2E_* names → appends to Options; the ONLY
                                package importing test/e2e/harness/*, internal/e2eslowconsumer,
                                examples/processors/*, cmd/e2e-semstreams/{fixtures,mission}
docker/Dockerfile               two runnable targets: production (cmd/semstreams), e2e (cmd/e2e-semstreams)
docker/compose/*.yml            every e2e service sets exactly its tier-table row's SEMSTREAMS_E2E_* vars
```

### 2.1 `boot.Options` — the seam, and the capability contract in internal form

```go
// Options is the one seam through which cmd/semstreams and cmd/e2e-semstreams
// diverge. Production() fills the scalars and leaves every extension empty;
// e2eboot.FromEnv appends to the extensions it is told to. Each extension is
// applied at exactly one boot phase, named in Run.
type Options struct {
    ConfigPath      string
    NATSURLs        string        // override; empty falls through to SEMSTREAMS_NATS_URLS, cfg, default
    LogLevel        string
    LogFormat       string
    Debug           bool
    ShutdownTimeout time.Duration
    HealthPort      int           // 0 = no dedicated listener (service.Manager.StartHealthListener's own contract)

    // Extensions, in the order Run applies them.
    ConfigPatches     []func(*config.Config) error                                   // after cfg.Validate, before any registry
    AfterConnect      []func(context.Context, *natsclient.Client) error              // after ConnectClient, before the config manager
    Components        []func(*component.Registry) error                              // with componentregistry.Register
    Payloads          []func(*payloadregistry.Registry) error                        // with payloadbuiltins.Register
    Tools             []func(context.Context, *agentictools.ExecutorRegistry, executors.ToolDependencies) error // with RegisterBuiltins
    Workflows         []lifecycle.Workflow                                           // after lifecycle.NewManager, before agentrun.Register
    Responders        []func(context.Context, *natsclient.Client, *projection.MutationClient, *slog.Logger) (io.Closer, error) // after WireGraphRuntime
    MilestoneHooks    []func(*agentrun.MilestoneSubscriber, *natsclient.Client, *slog.Logger) error // inside registerMilestoneService
    PostStart         []func(context.Context) error                                  // after ComponentManager.Start
}
```

Nine extension slices is not a design choice; it is the count of distinct boot phases the seven ruled options touch
(inventory § 3a–3f: registries ×3, config patch, after-connect, after-wire, milestone subscriber, post-start, plus
workflows). Each slice is appended by at most three options; none is speculative. The struct is the capability
contract the issue asks for — components + payloads + tools + lifecycle in one iterable value — kept internal by the
ruling (§ 0); whether it is exported is § 10 OQ1.

### 2.2 The seven options and their env names

| `SEMSTREAMS_E2E_*` | Appends | Today's gate (inventory) | Tiers (table rows) |
|---|---|---|---|
| `EXAMPLES=1` | `Components` += `iotsensor`, `document` example components; `Payloads` += `iotsensor`, `document`, `fixtures.RegisterPayloads` | root selection (§ 3a) | core-2, lessons, structural, statistical, throughput, semantic, research-graph |
| `MISSION=1` | `Components` += `mission.Register`; `Payloads` += `mission.RegisterPayloads`; `Workflows` += `mission.WorkflowDeclaration()` | root selection (§ 3b) | lifecycle |
| `LIFECYCLE_SEED=<suffix>` | `PostStart` += `seedMission(suffix)`; requires `MISSION=1` else `FromEnv` errors | `--lifecycle-seed` flag / `SEMSTREAMS_LIFECYCLE_SEED` (§ 3b) | lifecycle |
| `LESSON_CURATION=1` | `Responders` += the `lessoncuration.Handler` subscription | root selection (§ 3c) | ops |
| `PROCESS_BARRIER=1` | `Tools` += `processbarrier.Register`; `ConfigPatches` += `prepareE2EProcessBarrierConfig` | build tag `e2e_process_barrier` (§ 3d) | agentic |
| `MILESTONE_PROBE=1` | `MilestoneHooks` += `milestoneprobe.Register` | build tag + this env var (§ 3f) | agentic |
| `SLOW_CONSUMER=1` | `AfterConnect` += `e2eslowconsumer.Run` | build tag `e2e_slow_consumer` (§ 3e) | slow-consumer |

`FromEnv` scans the process environment for the `SEMSTREAMS_E2E_` prefix and **refuses any name not in this table**
(§ 6 I5) — the value guard the B test already applies to compose files, extended to names, at the one place the
binary reads them. `SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT` / `..._LLM_ENHANCEMENT_WAIT` are scenario-runner variables
(`test/e2e/scenarios/globalsearch_timeout.go:20,34`, *(new, this pass)*), never set on a semstreams service by any
compose file (the B test forbids it), so the refusal cannot fire on them.

### 2.3 Run's phase order (the eight steps as code)

`Run` is `cmd/semstreams/main.go:115-380`'s `run()` moved, with the extension slices applied at the phase each was
already applied at in whichever root carried it. Where the two copies differ, the **production copy wins**: the
pre-start shutdown guard (`:655-656`), `StartHealthListener(runtimeCtx, opts.HealthPort)` (`:675`; 0 is a documented
no-op, `service/service_manager.go:1291` *(new, this pass)*), the production signal handling and log text. The e2e
copy's `postStart` becomes `opts.PostStart`; `completeE2EPhaseA` and `runWithSignalHandling` are retired in favour of
the production sequence, whose tests (`signal_shutdown_test.go`, `bootstrap_observability_test.go`,
`root_resources_test.go`) move with it. If the e2e `bootstrap_observability_test.go` pair asserts an e2e-specific
phase-A label that the production sequence does not emit, the production label is kept and the deletion is recorded
in `tasks.md` (§ 7 row 1) — a boot log label is not load-bearing for any tier (no scenario greps it; *(new, this
pass)* `git grep -n 'PhaseA\|phase A' -- test/e2e` → 0 outside the harness).

No separate "verify every registration landed" pass is built. Each step already returns an error that fails boot; a
post-hoc re-read of the registries would re-check what the error path guarantees (§ 9 R1). The silent class that
remains — nil `ComponentRegistry` → `list_components` skipped — cannot occur from either framework root once the
composer owns the registry it passes; the adopter-side behaviour is § 10 OQ2.

### 2.4 Proof that "the production main cannot"

Two observations replace the build-constraint rule:

- **Closure.** `go list -deps ./cmd/semstreams` contains none of `internal/e2eboot`, `internal/e2eslowconsumer`,
  `test/e2e/harness/...`, `examples/processors/...`, `cmd/e2e-semstreams/...`; and `go list -deps ./cmd/e2e-semstreams`
  contains every one of them that exists (no stranded hook). Precedent for the mechanism:
  `test/contract/core_composition_deps_test.go:43` *(new, this pass)*.
- **Parity.** `e2eboot.FromEnv(cli, empty)` returns an `Options` whose every extension slice is empty and whose scalars
  equal `boot.Production(cli)` — checked by reflection over the struct so a slice added later is covered without
  editing the test. Per variable, exactly the slices in § 2.2 grow, by exactly the listed count.

So "the e2e binary with no `SEMSTREAMS_E2E_*` set boots the production composition" becomes a measured fact, and the
agentic and slow-consumer tiers — which today boot a tagged production root to prove "the production composition plus
one hook" — boot the `e2e` target with one or two variables and prove the same thing through the parity test plus the
B table row.

## 3. Options considered

| | Shape | Cost | Why not |
|---|---|---|---|
| A | One root; e2e things as tagged overlays in `cmd/semstreams` | seven `target: e2e` blocks, more tags | Swaps "which root" for "which tag"; ratchets overlays up. Rejected in the #1249 docket; the ruling chose D. |
| B only | Two roots, one written rule (landed, PR #1360) | 0 Go | Leaves the copy and the tags; #1301 stays open. The ruling: "B then D". |
| C | Hooks as components | — | Fits only lesson curation; the barrier is a tool executor, the probe exits the process, the slow-consumer probe runs before components exist. Rejected on the inventory (#1249 docket). |
| **D** | **One shared internal boot, thin mains, env-enabled options, two targets** | move ~1,800 lines into `internal/boot`+`internal/e2eboot`, delete 11 files + 4 Dockerfile stages, ~10 compose env edits | **Ruled.** |
| D′ | D plus an exported `Capability`/`Options` in a new Tier 1 package now | one Tier 1 line, owner design review of exported surface, sister migration table | Not ruled; a deliberate widening under ADR-106 (`docs/adr/106-*:57`). § 10 OQ1. |
| do nothing | — | 0 | #1301 gates beta.163 by owner ruling. |

## 4. Decisions

| # | Decision | Alternative considered first | Why this one |
|---|---|---|---|
| D1 | `internal/boot` holds `Options`, `Production`, `Run`; `internal/e2eboot` holds `FromEnv` | one package | The production main must not link the harness; the closure proof (§ 2.4) needs the e2e constructors in a package `cmd/semstreams` never imports. |
| D2 | All seven ruled items are env-enabled, including examples/mission/lesson curation that today are unconditional in the e2e root | keep those three unconditional, env-gate only the three hooks | The ruling names all seven as options the e2e main "enables from env". Cost: ~10 compose `environment:` lines, which the B test then verifies per row — the ruling's "migration checklist" made literal. |
| D3 | One `EXAMPLES` option covers example components + example payloads + fixture payloads | separate `EXAMPLES` and `FIXTURES` | The ruling names "the examples/fixtures registration policy" as one item; no tier needs one half without the other today (both are unconditional). Splitting is a doc sentence away if a tier ever does. |
| D4 | `FromEnv` refuses unknown `SEMSTREAMS_E2E_*` names | tolerate, as `os.Getenv` does today | The B test guards values; names were the remaining silent-disarm path (§ 1.4). ~10 lines. |
| D5 | `milestoneprobe.Register` stops reading `SEMSTREAMS_E2E_MILESTONE_PROBE` itself (`milestoneprobe.go:199`); `FromEnv` is the one reader | leave both reads | One home per interpreted fact (architect contract § Design discipline). |
| D6 | `--lifecycle-seed` flag and `SEMSTREAMS_LIFECYCLE_SEED` are replaced by `SEMSTREAMS_E2E_LIFECYCLE_SEED=<suffix>` | keep the flag | The seed is an e2e option by the ruling; under the `SEMSTREAMS_E2E_` prefix the B test sweeps it and the tier table declares it. One compose line (`docker/compose/lifecycle.yml:62`). |
| D7 | `HealthPort` is an `Options` scalar; production passes the flag, e2e passes 0 | start the listener in every binary | 0 is already `StartHealthListener`'s no-op (`service_manager.go:1291`); e2e tiers' `/health` is served by configured components (`configs/semantic.json` carries 8081/8083; the e2e root has no `StartHealthListener` call — *(new)* grep → 0). Nothing gained by changing it. |
| D8 | Production copy wins every `runUntilShutdown`/phase-A divergence (§ 2.3) | keep e2e variants behind options | Two of the divergences are hardening the e2e binary lacked (guard, bounded abort); the third (`postStart`) is an option; the fourth is log text. |
| D9 | One `registryFor(opts, full bool)` in `internal/boot` serves both mains' composition verbs and boot | leave `fullComponentRegistry` ×2 | Removes the third copy; the full-vs-selected *policy* (`cmd/semstreams/main.go:96-99`) is unchanged, so #1107's question remains the owner's — § 10 OQ3. |
| D10 | `internal/e2eslowconsumer/probe_e2e.go` loses its build tag; `Taskfile.yml:163` and the `check:push` description drop the tagged vet | keep a tagged tree | With no tags, ordinary `go vet ./...` and `go test ./...` compile every hook. Closes #1249 docket Q5 by removal; Q3 (rename the shared tag) is moot. |
| D11 | Contract tests: the B test's Dockerfile reader asserts exactly two runnable targets and zero `-tags=`; the Gate column is an env set; `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` is replaced by the closure test (§ 2.4) | keep the build-constraint test | With no constraints left it would assert nothing. |
| D12 | No ADR | an ADR for "e2e hooks are boot options, never build tags" | Mechanics; the rule's home is the payload-registry tier-table requirement (rewritten, § 7) and the ruling on #1301. ADR-094 already fixes "boot seals composition". |
| D13 | Not `!`: no exported Go surface changes; the removed Dockerfile targets are referenced only by our compose files (inventory § 3h) | `!` | Nothing a sister imports or builds moves. § 8 still runs the tiers. |

## 5. Premises, each with its measurement

| # | Premise | Measurement |
|---|---|---|
| P1 | The four shared helpers are code-identical or differ only by the named hunks | inventory § 1c diffs: 1 line, 1 line, 9 lines, 4 hunks |
| P2 | `StartHealthListener(ctx, 0)` is a no-op | `service/service_manager.go:1291-1292` `if port == 0 { return nil` |
| P3 | No exported `Capability` type exists | inventory § 8: `git grep "type Capability"` → 0 |
| P4 | `executors`, `componentregistry`, `payloadbuiltins`, `persona`, `agentrun`, `pkg/lifecycle` are Tier 1; nothing under `internal/` is | `release/tier1-packages.txt:40,43,59,61,69,79` *(new)*; ADR-106 § Tier 2 |
| P5 | The B test already asserts each service sets exactly its row's `SEMSTREAMS_E2E_*` vars with nonempty literal values | `openspec/specs/payload-registry/spec.md:79-92` |
| P6 | The e2e root reads no `SEMSTREAMS_E2E_*` today; its only option-like env is `SEMSTREAMS_LIFECYCLE_SEED` | `cmd/e2e-semstreams/main.go:515-521,643` *(new)* grep |
| P7 | semdev already runs two mains over one `internal/boot` with a single `RunOptions` seam | semdev `internal/boot/runtime.go:110` doc comment (read-only, HEAD `ca3956a`) |
| P8 | Only compose files reference the two per-hook targets | inventory § 3h: `agentic.yml:68`, `e2e-slow-consumer.yml:22` |
| P9 | `Taskfile.yml:163` is the only vet of the tagged tree; `ci.yml` vets no e2e tag | `grep -n 'tags=' .github/workflows/ci.yml Taskfile.yml taskfiles/*.yml` *(new)* |
| P10 | A `go list -deps` contract test is an existing pattern | `test/contract/core_composition_deps_test.go:43` *(new)* |
| P11 | The milestone probe reads its env var inside the harness | `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` |
| P12 | Live docs naming the tags/targets/e2e-root symbols: 4 (`.agents/skills/new-payload/SKILL.md`, `docs/basics/05`, `docs/concepts/15`, `docs/contributing/02`); ADR-051/058, `migration-beta18.md` and `docs/proposals/*` are history and are not rewritten | `git grep -l` over docs/.agents/openspec/specs *(new)* |

## 6. Invariants, their spec home, and how each is exercised

| # | Invariant | Spec home (delta) | Exercised by |
|---|---|---|---|
| I1 | `FromEnv(cli, ∅)` ≡ `Production(cli)`: every extension slice empty, scalars equal | framework-composition ADDED, scenario 1 | `TestE2EBootWithNoOptionsIsTheProductionComposition` (reflection over `Options`) |
| I2 | Each `SEMSTREAMS_E2E_*` name grows exactly the § 2.2 slices by exactly the listed count; `LIFECYCLE_SEED` without `MISSION` errors | scenario 2 | `TestE2EBootOptionAppendsExactlyItsExtensions` (table-driven, 7 rows + 1 error row) |
| I3 | An unknown `SEMSTREAMS_E2E_*` name is refused | scenario 2 | `TestE2EBootRefusesUnknownE2EVariable` |
| I4 | `cmd/semstreams`'s non-test closure holds no harness/e2e package; `cmd/e2e-semstreams`'s holds every one | scenario 3 (replaces "cannot reach the production build") | `TestProductionRootClosureHoldsNoE2EHarness` (`go list -deps`) |
| I5 | `docker/Dockerfile` has exactly two runnable targets and no `-tags=`; every tier row's Gate is an env set the service sets exactly | payload-registry MODIFIED, scenario "every tier's target…" | `TestE2ETierTableMatchesComposeAndDockerfile` (reader changed) |
| I6 | The set of names `FromEnv` accepts equals the union of the tier table's Gate column | framework-composition ADDED, scenario 4 | `TestE2EBootVariableSetMatchesTierTable` (reads the same table the B test reads) |
| I7 | The slow-consumer probe runs after `ConnectClient` and before the config manager | none new — behaviour carried | `TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration` moved to `internal/boot` |

PBT decision (`docs/contributing/01-testing.md`): every invariant is an equality over a finite, enumerated set (seven
names, nine slices, two targets); named examples exercise each fully. No Rapid property; no fuzz target.

Skills: `kv-or-stream` — no new communication path, not triggered. `orchestration-check` — no multi-step behaviour
added, not triggered. `new-payload` — no new type, not triggered. `query-pattern` — no new query access, not triggered.

## 7. What changes where

| # | Surface | Change |
|---|---|---|
| 1 | `internal/boot/` *(new)* | `options.go` (§ 2.1, `Production`), `run.go` (`run()` moved, extensions applied per § 2.3), `registry.go` (D9), `resources.go` (root resources + close). Tests moved from `cmd/semstreams/`: `signal_shutdown_test.go`, `bootstrap_observability_test.go`, `bootstrap_observability_integration_test.go`, `root_resources_test.go`, `payload_registration_test.go`, `milestone_wiring_test.go`, `slow_consumer_hook_contract_test.go` (I7). The e2e duplicates are deleted. |
| 2 | `internal/e2eboot/` *(new)* | `fromenv.go` (§ 2.2, D4), `options_*.go` one file per option (the moved bodies of `registerExampleComponents`, `buildPayloadRegistry`'s e2e half, `seedMission`, the lesson-curation subscription, `prepareE2EProcessBarrierConfig`+`registerE2EProcessBarrier`, `registerE2EMilestoneProbe`, `runSlowConsumerProbe`). Tests: I1–I3, I6, plus `process_barrier_e2e_test.go`'s three config-patch tests untagged and `process_barrier_config_contract_test.go` unchanged. |
| 3 | `cmd/semstreams/main.go`, `cmd/e2e-semstreams/main.go` | thin: flags → options → `boot.Run`; verbs via `boot.RegistryFor`. Deleted: `process_barrier_{e2e,disabled}.go`, `milestone_probe_{e2e,disabled}.go`, `slow_consumer_probe_{e2e,disabled}.go` and their `_test.go` (6 + 3 files), `cmd/e2e-semstreams/{registry_wiring,bootstrap_observability,milestone_wiring,signal_shutdown}_test.go` (moved or duplicate). `cmd/e2e-semstreams/{fixtures,mission}` stay where they are (imported by tests outside `cmd/`, inventory § 6). |
| 4 | `internal/e2eslowconsumer/` | `probe_e2e.go` → `probe.go`, tag removed; same for its test (D10). |
| 5 | `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` | env read removed (D5). |
| 6 | `docker/Dockerfile` | stages `process-barrier-builder`, `e2e-process-barrier`, `slow-consumer-builder`, `e2e-slow-consumer` deleted (`:182-215`). |
| 7 | `docker/compose/` | `agentic.yml:68` → `target: e2e`, `+SEMSTREAMS_E2E_PROCESS_BARRIER=1` (MILESTONE_PROBE already at `:87`); `e2e-slow-consumer.yml:22` → `target: e2e`, `+SEMSTREAMS_E2E_SLOW_CONSUMER=1`; `e2e.yml` fixtures service, `tiered.yml` ×3, `research-graph.yml` → `+SEMSTREAMS_E2E_EXAMPLES=1`; `lifecycle.yml` → `+SEMSTREAMS_E2E_MISSION=1`, `--lifecycle-seed` (`:62`) → `SEMSTREAMS_E2E_LIFECYCLE_SEED=<same suffix>`; `ops.yml` → `+SEMSTREAMS_E2E_LESSON_CURATION=1`. |
| 8 | `Taskfile.yml:152,163` | tagged vet line removed; `check:push` description corrected (D10). |
| 9 | `openspec/specs/payload-registry/spec.md:23-33` and the table | MODIFIED (delta in this change): the rule paragraph rewritten — an e2e-only registration or hook is a boot option the e2e binary enables from a `SEMSTREAMS_E2E_*` variable its tier's compose service sets; the production binary cannot enable one; a tier proving the production composition with a hook inside boots the `e2e` target with exactly that hook's variable. Rows agentic, slow-consumer: target `e2e`, gate = env; all `e2e` rows: gate = their variables. Scenario "every tier's target…": `-tags=` clause → "exactly two runnable targets and no `-tags=`". Scenario "cannot reach the production build" → closure wording (I4). Every other scenario byte-identical. |
| 10 | `openspec/specs/framework-composition/spec.md` | ADDED requirement "One framework boot composes both framework binaries" with scenarios for I1–I4, I6 (delta in this change). |
| 11 | `docs/contributing/02-e2e-tests.md:180-220` | navigation copy: two targets, Gate column, the rule sentence; the `Every tier and the binary it boots` paragraph rewritten. |
| 12 | `docs/concepts/15-payload-registry.md:98-99,289`, `docs/basics/05-first-processor.md:126-133`, `.agents/skills/new-payload/SKILL.md` | point at `internal/boot`'s composer and `internal/e2eboot`; the sweep command is `tasks.md` 6.1. |
| 13 | `test/contract/e2e_tier_binary_contract_test.go` | D11: Dockerfile reader → two targets / zero tags; Gate parser → env set; `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` → `TestProductionRootClosureHoldsNoE2EHarness`. |

Deleted: 11 tagged files, 4 duplicate tests, 4 Dockerfile stages, 1 Taskfile line. Net Go: a move of ~1,800 lines
into two internal packages plus ~150 new lines (`FromEnv`, tests).

## 8. E2E evidence before the merge

Every tier's boot contract changes (the compose `environment:` block is now load-bearing for eight of twelve
services), and a missing variable fails at boot (unknown component type → composition validation) or at the tier's
first synthetic stamp (unregistered payload) — loudly either way. Recommended gate (§ 10 OQ4): one local
`task e2e:all` green at the final revision (Docker released by the owner for the day), with the tier log's own `exit=`
line and `docker compose ls` = 0 before it (`e2e:clean` tears down every stack on the host); the ladder's
`slow-consumer` and `statistical` jobs in CI. The agentic tier (~5m35s, local-only) is inside `e2e:all`.

## 9. Rejected — recorded so it is not re-derived

| # | Not built | Why |
|---|---|---|
| R1 | A post-install pass re-reading every registry to "verify each declared registration landed" | Every step's `Register` already returns the error that fails boot; the pass would re-check the error path. The declaration IS the install list. |
| R2 | A `Capability` interface with optional methods | Struct-of-slices is the plainest iterable; an interface adds a resolution layer for nothing the mains need. |
| R3 | Per-tier build tags renamed (`e2e_agentic`) — #1249 docket Q3 | Moot: no tags remain. |
| R4 | Extending `Taskfile.yml:163` to vet `e2e_slow_consumer` — #1249 docket Q5 | Moot: no tags remain; ordinary vet compiles everything. |
| R5 | A health listener in the e2e binary | D7. |
| R6 | Keeping examples/mission/lesson curation unconditional in the e2e root | Contradicts the ruling's "enables from env" (D2). |
| R7 | Redesigning any tier's scenario or config | Out of the ruled bound. |
| R8 | A `docs/operations/migration-*.md` | Nothing exported or built by a sister changes (D13). |

## 10. Owner docket — alternative first, recommendation marked

| # | Question | (a) | (b) | Recommendation |
|---|---|---|---|---|
| OQ1 | Contract surface | Land `boot.Options` internal as ruled; the issue's "sisters migrate to it" goal is **not** met by this change and stays open on #1301's successor, decided after the reference app's friction log (the issue body's own sequencing) | Export `Options`/`Capability` now under a new Tier 1 package (ADR-106: a deliberate widening; owner design review of exported surface; sister migration table for six roots) | **(a)** — the ruled shape; exporting before one adopter has used it is prediction. |
| OQ2 | `RegisterBuiltins` with a nil `ComponentRegistry` (`register_component_catalog.go:18`, WARN + skip) | A doc sentence on `ToolDependencies`: "the composer owns the registry; nil is a composer bug" — the framework roots can no longer hit it; adopters keep today's WARN | Return an error (Tier 1 behaviour change on `executors`; any sister passing nil breaks at boot — a census would be needed first) | **(a)** |
| OQ3 | #1107 after D9 | One builder for verbs and boot; #1107 stays open because the full-vs-selected policy it questions is unchanged | Owner closes #1107 on this PR if one builder is what it wanted | **(a)** — not this PR's question to answer. |
| OQ4 | E2E gate for this PR | `task e2e:all` once locally at the final revision + the ladder in CI | Only the tiers whose options change (core, structural, lifecycle, ops, agentic, slow-consumer) | **(a)** — every service's compose contract changes; the first run reveals any under-declared row. |

Settled by prior ruling, not re-opened here: B's rule and table (PR #1360), D as #1301's shape, beta.163 placement,
sequencing after #1362 (met: PR #1366 merged 2026-09-25), #1249 docket Q3/Q5 (closed by D10).

## 11. Open facts inherited from the inventory, and how this design treats them

1. The `docs/basics/05` "retired call" residual (#1103) — not reproduced; this change updates `:126-133` to name the
   composer and leaves #1103 open.
2. semteams' `agentrun.Register` position — irrelevant under OQ1 (a): no sister changes.
3. semspec's mutation wiring — same.
4. No `gopls` pass — the design touches no interface indirection; the developer's `go build ./...` after the move is
   the closure check.
