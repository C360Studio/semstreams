# Design: one composition root, seven env-enabled boot options, two Dockerfile targets

Change `one-composition-root` · issue #1301 (milestone `v1.0.0-beta.163`, gates the tag) · claim PR #1390 ·
inventory `docs/proposals/gh1301-composition-inventory.md` (257 pins, `inventory:verify` 257/257 at `86803eb9`, base
`df3effb2`). Every `path:line` below is a pin in that inventory unless marked *(new)*.

Read-only design pass (Fable, 2026-09-26) at `dfaeb69c`; independent pre-owner design review at that revision
(`design-review.md`, verdict DESIGN CHANGES REQUESTED: 1 BLOCKING, 2 HIGH, 4 MEDIUM, 4 NIT) — every finding applied
below, none changed the shape. Nothing here is approved until the owner rules § 10.

**Amendment ledger (review → design):** BLOCKING Phase-A → D8, D14, P13, § 7 rows 1/14/15, delta
`specs/application-logging`; HIGH closure converse → § 2.4, I4, both delta scenarios; HIGH flags/build info → D15, P14,
§ 7 row 3; MEDIUM D4/I3 dropped → § 1.4, § 2.2, D4, R9; MEDIUM stale `os.Getenv` clause → payload-registry delta;
MEDIUM parity overclaim → § 2.4, I1, § 11.3, OQ2; MEDIUM path-readers → § 7 rows 15–17, tasks 6.1; NITs → D7, P3,
§ 7 row 7, I4 (cites `TestProductionBinaryExcludesExamplePackages`). Docket: OQ1/OQ3 of the first pass are residuals
(§ 11), OQ2 is decided (D16); the docket is now § 10 OQ1 (e2e gate) and OQ2 (`init()` vocabulary fidelity).

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
   (`cmd/semstreams/main.go:655-656`), the health listener (`:675`), the production Phase-A logging composition
   (`:170`, `:227`; *(new)*) and the JSON log default (`cmd/semstreams/flags.go:42` *(new)*); the e2e copy alone carries a
   `postStart` hook (`cmd/e2e-semstreams/main.go:852`), a stdout-only Phase-A (`:159`, `:374` *(new)*), a `text` log
   default and a hard-coded debug port (`:517,519` *(new)*). Five tests are duplicated across the roots
   (`signal_shutdown_test.go` ×2, `bootstrap_observability_test.go` ×2, `milestone_wiring_test.go` ×2 — *(new)*
   `grep -n '^func Test'` over both roots).
2. **Eight pre-`Start` steps ordered by convention.** The dependency edges — payloads → `WireGraphRuntime` → tools;
   `lifecycle.NewManager` → `agentrun.Register`; rule processors → `ConfigureRulePackMutations` — exist as prose
   (`processor/agentic-tools/executors/register.go:38,54-56`, "Pattern-B step N") and as the accident of line order in
   two files (inventory § 1d/1e).
3. **Three gate styles for seven e2e-only things.** Root selection (examples/fixtures, mission, lesson curation — no
   env, no tag; inventory § 3a-3c), build tag (process barrier, slow-consumer probe; § 3d-3e), build tag plus env
   (milestone probe; § 3f). Eleven tagged Go files (§ 3g), four runnable Dockerfile targets over three builders
   (§ 3h), a third tagged vet tree (`Taskfile.yml:163`) that is the only untagged-build check the hooks get.
4. **One silent class.** A nil `ToolDependencies.ComponentRegistry` makes `list_components` skip with a WARN
   (`processor/agentic-tools/executors/register_component_catalog.go:18`). (A misspelled `SEMSTREAMS_E2E_*` name in a
   compose file is NOT silent: `TestE2ETierTableMatchesComposeAndDockerfile` compares the names a service sets with
   its row's gates exactly — `test/contract/e2e_tier_binary_contract_test.go:562-564` *(new)* — the first pass of this
   design claimed otherwise and was corrected in review.)
5. **A third registry builder.** Each root's `fullComponentRegistry` (`cmd/semstreams/main.go:100`,
   `cmd/e2e-semstreams/main.go:98`) builds the offline verbs' registry a third way beside boot's `Selected(cfg)` gating
   (`:307`) — the #1107 sibling.

## 2. Target shape (D, made concrete)

```
cmd/semstreams/main.go          cli := boot.ParseFlags(os.Args[1:]); boot.Run(ctx, boot.Production(cli, buildInfo))   (~60 lines)
cmd/e2e-semstreams/main.go      cli := boot.ParseFlags(os.Args[1:]); opts := e2eboot.FromEnv(cli, buildInfo, os.LookupEnv)
                                boot.Run(ctx, opts)                                                                  (~60 lines)
internal/boot                   Options, ParseFlags, Production(), Run(), the eight steps in dependency order,
                                RegistryFor(opts, full bool) for the verbs and for boot                (moved, not rewritten)
internal/e2eboot                FromEnv: seven SEMSTREAMS_E2E_* names → appends to Options; the ONLY package importing
                                test/e2e/harness/*, internal/e2eslowconsumer, examples/processors/{iot_sensor,document},
                                cmd/e2e-semstreams/{fixtures,mission}
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
    // Scalars — from ParseFlags (one parser, production's, for both mains) and from each main's own ldflags variables.
    ConfigPath      string
    NATSURLs        string        // override; empty falls through to SEMSTREAMS_NATS_URLS, cfg, default
    LogLevel        string
    LogFormat       string        // default "json" for BOTH binaries (D15)
    Debug           bool
    DebugPort       int           // pprof binds only when Debug && DebugPort > 0 (service/pprof.go:34)
    ShutdownTimeout time.Duration
    HealthPort      int           // 0 = no dedicated listener (service.Manager.StartHealthListener's own contract)
    Build           BuildInfo     // Version, GitCommit, BuildTime — each main copies its own -X main.* variables in

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
workflows). The review confirmed no strictly simpler seam exists (`design-review.md` question 4). The struct is the
capability contract the issue asks for — components + payloads + tools + lifecycle in one iterable value — kept
internal by the ruling (§ 0); the sister-migration goal it does not meet is § 11.1.

### 2.2 The seven options and their env names

| `SEMSTREAMS_E2E_*` | Appends | Today's gate (inventory) | Tiers (table rows) |
|---|---|---|---|
| `EXAMPLES=1` | `Components` += `iotsensor`, `document` example components; `Payloads` += `iotsensor`, `document`, `fixtures.RegisterPayloads` | root selection (§ 3a) | core-2, lessons, structural, statistical, throughput, semantic, research-graph |
| `MISSION=1` | `Components` += `mission.Register`; `Payloads` += `mission.RegisterPayloads`; `Workflows` += `mission.WorkflowDeclaration()` | root selection (§ 3b) | lifecycle |
| `LIFECYCLE_SEED=<suffix>` | `PostStart` += `seedMission(suffix)` | `--lifecycle-seed` flag / `SEMSTREAMS_LIFECYCLE_SEED` (§ 3b) | lifecycle |
| `LESSON_CURATION=1` | `Responders` += the `lessoncuration.Handler` subscription | root selection (§ 3c) | ops |
| `PROCESS_BARRIER=1` | `Tools` += `processbarrier.Register`; `ConfigPatches` += `prepareE2EProcessBarrierConfig` | build tag `e2e_process_barrier` (§ 3d) | agentic |
| `MILESTONE_PROBE=1` | `MilestoneHooks` += `milestoneprobe.Register` | build tag + this env var (§ 3f) | agentic |
| `SLOW_CONSUMER=1` | `AfterConnect` += `e2eslowconsumer.Run` | build tag `e2e_slow_consumer` (§ 3e) | slow-consumer |

**Value rule, stated once:** a nonempty value enables the option (the same literal rule the B test applies to compose
files); `NAME=` is unset. `FromEnv` ignores `SEMSTREAMS_E2E_*` names it does not know: compose names are guarded
statically by the B test (`:562-564`) plus I6, and the two runner-side variables that share the prefix
(`SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT`, `..._LLM_ENHANCEMENT_WAIT`; `taskfiles/e2e/semantic.yml:45` *(new)*) must not
refuse a hand-launched `bin/e2e-semstreams`. `LIFECYCLE_SEED` without `MISSION` needs no check of its own:
`seedMission` → `Manager.Create` already fails loudly on an unregistered workflow (`pkg/lifecycle/manager.go:317-319`
*(new)*), and the B test pins that the lifecycle row sets both.

### 2.3 Run's phase order (the eight steps as code)

`Run` is `cmd/semstreams/main.go:115-380`'s `run()` moved, with the extension slices applied at the phase each was
already applied at in whichever root carried it. Where the two copies differ, the **production copy wins** (D8):

- the pre-start shutdown guard (`:655-656`) and the bounded abort cleanup — hardening the e2e binary lacked;
- `StartHealthListener(runtimeCtx, opts.HealthPort)` (`:675`; 0 is a documented no-op, `service/service_manager.go:1291`
  *(new)*, and 0 is already the flag's default, `cmd/semstreams/flags.go:63-64` *(new)*);
- **the production Phase-A logging composition** — `bootstrapobservability.NewProductionPhaseA` with its
  `CounterHandler` and `Steady(forwardingHandler)` (`cmd/semstreams/main.go:170,227`; `internal/bootstrapobservability/bootstrap.go:36-50`
  *(new)*) replaces the e2e root's `NewE2EPhaseA` + `Steady(nil)` (`cmd/e2e-semstreams/main.go:159,374`;
  `bootstrap.go:57-59`). This is a deliberate reversal of the #961 split, which `docs/proposals/gh955-bootstrap-logger-design.md:16,110`
  recorded as preserved behaviour, not as a ruling. It is REQUIRED, not optional: the slow-consumer tier moves onto the
  `e2e` target and its scenario reads `semstreams_log_entries_total` (`test/e2e/scenarios/core_slow_consumer.go:165`
  *(new)*), which only the counter handler produces. Its visible tier effect: the five e2e-target tiers whose configs
  enable `log-forwarder` — structural (`configs/e2e-structural.json`), statistical and throughput (`statistical.json`),
  semantic and its overlays (`semantic.json`, `semantic-8b.json`, `semantic-frontier.json`), lifecycle (`lifecycle-flow.json`),
  research-graph (`research-graph-e2e.json`) *(new, `jq '.services["log-forwarder"].enabled'`)* — start forwarding logs
  to NATS as the production binary already does in agentic and slow-consumer. No scenario greps a container's log text
  in those tiers (`design-review.md` question 6); OQ1's `e2e:all` run is the measurement. The live spec scenario that
  pins the split (`openspec/specs/application-logging/spec.md:23-28`) is MODIFIED by this change (§ 7 row 14).
- production's `parseFlags` becomes `boot.ParseFlags`, used by both mains (D15); the e2e `parseCLI`, its `text` log
  default, its hard-coded `DebugPort: 6060` and `--lifecycle-seed` are deleted. The slow-consumer scenario keeps only
  log lines that parse as JSON (`core_slow_consumer.go:141-157` *(new)*) and `docker/compose/e2e-slow-consumer.yml:27-31`
  sets no `SEMSTREAMS_LOG_FORMAT`, so the JSON default is load-bearing for that tier.

The e2e copy's `postStart` becomes `opts.PostStart`; `completeE2EPhaseA` and `runWithSignalHandling` are retired in
favour of the production sequence, whose tests (`signal_shutdown_test.go`, `bootstrap_observability_test.go`,
`bootstrap_observability_integration_test.go`, `root_resources_test.go`) move with it.

No separate "verify every registration landed" pass is built. Each step already returns an error that fails boot; a
post-hoc re-read of the registries would re-check what the error path guarantees (R1). The silent class that
remains — nil `ComponentRegistry` → `list_components` skipped — cannot occur from either framework root once the
composer owns the registry it passes; the adopter-side WARN is kept (D16).

### 2.4 Proof that "the production main cannot"

Two observations replace the build-constraint rule:

- **Closure.** `go list -deps ./cmd/semstreams` contains no package under `test/e2e/harness`, `internal/e2eboot`,
  `internal/e2eslowconsumer`, `examples/processors` or `cmd/e2e-semstreams`. The converse is scoped to where hooks live:
  `go list -deps ./cmd/e2e-semstreams` contains every package under `test/e2e/harness`, plus `internal/e2eboot`,
  `internal/e2eslowconsumer`, `cmd/e2e-semstreams/fixtures` and `cmd/e2e-semstreams/mission` — not every example
  (`examples/processors/weather_station` is linked by nothing today, *(new)* `ls examples/processors`). Mechanism
  precedent: `test/contract/core_composition_deps_test.go:43`; its `TestProductionBinaryExcludesExamplePackages`
  (`:30-34` *(new)*) already covers the `/examples/` half and is folded into the new test.
- **Parity.** `e2eboot.FromEnv(cli, build, empty)` returns an `Options` whose every extension slice is empty and whose
  scalars equal `boot.Production(cli, build)` — checked by reflection over the struct so a field added later is covered
  without editing the test. Per variable, exactly the slices in § 2.2 grow, by exactly the listed count.

What the parity proves, exactly: **the e2e binary's boot options equal the production options.** It does not make
the two binaries identical processes — the e2e binary always links `examples/processors/{iot_sensor,document}` and
`cmd/e2e-semstreams/mission`, whose `init()` functions register vocabulary into the global registry
(`examples/processors/iot_sensor/vocabulary.go:5`, `document/vocabulary.go:5`, `mission/state.go:56` *(new)*), so its
vocabulary is a superset of production's even with nothing set. The agentic and slow-consumer tiers, which today boot
a tagged production root, lose that much fidelity; whether to remove the three `init()`s is § 10 OQ2.

## 3. Options considered

| | Shape | Cost | Why not |
|---|---|---|---|
| A | One root; e2e things as tagged overlays in `cmd/semstreams` | seven `target: e2e` blocks, more tags | Swaps "which root" for "which tag"; ratchets overlays up. Rejected in the #1249 docket; the ruling chose D. |
| B only | Two roots, one written rule (landed, PR #1360) | 0 Go | Leaves the copy and the tags; #1301 stays open. The ruling: "B then D". |
| C | Hooks as components | — | Fits only lesson curation; the barrier is a tool executor, the probe exits the process, the slow-consumer probe runs before components exist. Rejected on the inventory (#1249 docket). |
| **D** | **One shared internal boot, thin mains, env-enabled options, two targets** | move ~1,800 lines into `internal/boot`+`internal/e2eboot`, delete 11 files + 4 Dockerfile stages, ~10 compose env edits | **Ruled.** |
| D′ | D plus an exported `Capability`/`Options` in a new Tier 1 package now | one Tier 1 line, owner design review of exported surface, sister migration table | Contradicts the scope bound ("unexported, so no Tier 1 surface"); not re-opened. § 11.1. |
| do nothing | — | 0 | #1301 gates beta.163 by owner ruling. |

## 4. Decisions

| # | Decision | Alternative considered first | Why this one |
|---|---|---|---|
| D1 | `internal/boot` holds `Options`, `ParseFlags`, `Production`, `Run`; `internal/e2eboot` holds `FromEnv` | one package | The production main must not link the harness; the closure proof (§ 2.4) needs the e2e constructors in a package `cmd/semstreams` never imports. |
| D2 | All seven ruled items are env-enabled, including examples/mission/lesson curation that today are unconditional in the e2e root | keep those three unconditional, env-gate only the three hooks | The ruling names all seven as options the e2e main "enables from env"; the #1249 docket's own table lists them under "hooks carried". Cost: ~10 compose `environment:` lines, which the B test then verifies per row — the ruling's "migration checklist" made literal. The review confirmed every e2e service's config is covered by its row's variables (`design-review.md` question 5). |
| D3 | One `EXAMPLES` option covers example components + example payloads + fixture payloads | separate `EXAMPLES` and `FIXTURES` | The ruling names "the examples/fixtures registration policy" as one item; no tier needs one half without the other today (both are unconditional). Splitting is a doc sentence away if a tier ever does. |
| D4 | `FromEnv` ignores `SEMSTREAMS_E2E_*` names it does not know; a nonempty value enables | refuse unknown names (the first pass) | Compose names are already guarded statically (`e2e_tier_binary_contract_test.go:562-564` + I6); a refusal would guard only a hand-launched binary and would reject the two runner-side variables sharing the prefix. Owner's simplicity rule. R9. |
| D5 | `milestoneprobe.Register` stops reading `SEMSTREAMS_E2E_MILESTONE_PROBE` itself (`milestoneprobe.go:199`); `FromEnv` is the one reader | leave both reads | One home per interpreted fact (architect contract § Design discipline). |
| D6 | `--lifecycle-seed` flag and `SEMSTREAMS_LIFECYCLE_SEED` are replaced by `SEMSTREAMS_E2E_LIFECYCLE_SEED=<suffix>` | keep the flag | The seed is an e2e option by the ruling ("`--lifecycle-seed` … enables from env"); under the `SEMSTREAMS_E2E_` prefix the B test sweeps it and the tier table declares it. One compose line (`docker/compose/lifecycle.yml:62`). |
| D7 | `HealthPort` is an `Options` scalar; production passes the flag (default 0), e2e passes 0 | start the listener in every binary | 0 is already `StartHealthListener`'s no-op (`service_manager.go:1291`) and the flag's default (`flags.go:63-64`); the e2e root never calls it; every app healthcheck in `docker/compose/*.yml` hits a port a configured component serves (`/health` on 8080/8081/8083, `/readyz` on 8080 — *(new)* `grep -A1 healthcheck:`), none the dedicated listener. Nothing gained by changing it. |
| D8 | Production copy wins every divergence (§ 2.3): guard, bounded abort, health listener, **Phase-A logging**, `postStart` → option, log text | keep e2e variants behind options | Three of the divergences are hardening the e2e binary lacked; the Phase-A one is required by the slow-consumer tier's counter; the fourth is an option; the fifth is text. The #961 split it reverses was preserved behaviour, not a ruling. |
| D9 | One `RegistryFor(opts, full bool)` in `internal/boot` serves both mains' composition verbs and boot | leave `fullComponentRegistry` ×2 | Removes the third copy; the full-vs-selected *policy* (`cmd/semstreams/main.go:96-99`) is unchanged, so #1107 stays open (§ 11.2). |
| D10 | `internal/e2eslowconsumer/probe_e2e.go` loses its build tag; `Taskfile.yml:163` and the `check:push` description drop the tagged vet | keep a tagged tree | With no tags, ordinary `go vet ./...` and `go test ./...` compile every hook. Closes #1249 docket Q5 by removal; Q3 (rename the shared tag) is moot. |
| D11 | Contract tests: the B test's Dockerfile reader asserts exactly two runnable targets and zero `-tags=`; the Gate column is an env set; `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` is replaced by the closure test (§ 2.4), which absorbs `TestProductionBinaryExcludesExamplePackages` | keep the build-constraint test | With no constraints left it would assert nothing. |
| D12 | No ADR | an ADR for "e2e hooks are boot options, never build tags" | Mechanics; the rule's home is the payload-registry tier-table requirement (rewritten, § 7) and the ruling on #1301. ADR-094 already fixes "boot seals composition". |
| D13 | Not `!`: no exported Go surface changes; the removed Dockerfile targets are referenced only by our compose files (inventory § 3h; `container.yml:73` builds `production` only *(new)*) | `!` | Nothing a sister imports or builds moves. § 8 still runs the tiers. |
| D14 | `NewE2EPhaseA` is deleted with its tests (`internal/bootstrapobservability/bootstrap_test.go:102-133`; the e2e half of `internal/maxdelivery/boot_order_test.go:111-158`); the production half of `boot_order_test.go` is re-pointed at `internal/boot/run.go` | keep `NewE2EPhaseA` as an option | Nothing would call it (D8). A dead constructor with a spec scenario behind it is exactly the "advertised-absent" class. |
| D15 | One flag parser (production's, moved to `boot.ParseFlags`) for both mains; `Options` gains `DebugPort` and `Build`; the `-X main.Version/GitCommit/BuildTime` variables stay in each `package main` and are copied into `Options.Build` | let each main keep its parser | The slow-consumer tier depends on production's JSON default and counter (§ 2.3); one parser is the only shape under which I1 means anything (I1 compares over the same `cli`). ldflags target `package main` (`docker/Dockerfile:48-58`, `.github/workflows/release.yml:58` *(new)*), so the variables cannot move. pprof binds only under `Debug && DebugPort > 0` (`service/pprof.go:34` *(new)*) and no compose file sets `SEMSTREAMS_DEBUG`, so the e2e binary's debug-port default moving 6060 → 8083 binds nothing. |
| D16 | `RegisterBuiltins` keeps today's WARN on a nil `ComponentRegistry`; one doc sentence on `ToolDependencies` says the composer owns the registry | return an error | `executors` is Tier 1 (`release/tier1-packages.txt:79`); the framework roots can no longer hit the WARN; changing adopter behaviour would need a sister census first. Owner's simplicity rule; not an owner question. |

## 5. Premises, each with its measurement

| # | Premise | Measurement |
|---|---|---|
| P1 | The four shared helpers are code-identical or differ only by the named hunks | inventory § 1c diffs: 1 line, 1 line, 9 lines, 4 hunks |
| P2 | `StartHealthListener(ctx, 0)` is a no-op | `service/service_manager.go:1291-1292` `if port == 0 { return nil` |
| P3 | No exported `Capability` type or interface exists | inventory § 8: `git grep -n "type Capability\b\|type Capability interface\|type Capability struct" -- '*.go'` → 0 (`model/registry.go:357 type CapabilityConfig` is a different identifier) |
| P4 | `executors`, `componentregistry`, `payloadbuiltins`, `persona`, `agentrun`, `pkg/lifecycle` are Tier 1; nothing under `internal/` is | `release/tier1-packages.txt:40,43,59,61,69,79` *(new)*; ADR-106 § Tier 2 |
| P5 | The B test already asserts each service sets exactly its row's `SEMSTREAMS_E2E_*` names with nonempty literal values | `test/contract/e2e_tier_binary_contract_test.go:562-573`; `openspec/specs/payload-registry/spec.md:79-92` |
| P6 | The e2e root reads no `SEMSTREAMS_E2E_*` today; its only option-like env is `SEMSTREAMS_LIFECYCLE_SEED` | `cmd/e2e-semstreams/main.go:515-521,643` *(new)* grep |
| P7 | semdev already runs two mains over one `internal/boot` with a single `RunOptions` seam | semdev `internal/boot/runtime.go:106-110` doc comment (read-only, HEAD `ca3956a`) |
| P8 | Only compose files reference the two per-hook targets; CI reaches targets only through `task` → compose | inventory § 3h; `.github/workflows/e2e-ladder.yml:44-70`, `container.yml:73` *(new)* |
| P9 | `Taskfile.yml:163` is the only vet of the tagged tree; `ci.yml` vets no e2e tag | `grep -n 'tags=' .github/workflows/ci.yml Taskfile.yml taskfiles/*.yml` *(new)* |
| P10 | A `go list -deps` contract test is an existing pattern | `test/contract/core_composition_deps_test.go:43` *(new)* |
| P11 | The milestone probe reads its env var inside the harness | `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` |
| P12 | Live docs naming the tags/targets/e2e-root symbols: 4 (`.agents/skills/new-payload/SKILL.md`, `docs/basics/05`, `docs/concepts/15`, `docs/contributing/02`) plus the two role contracts' registration sentence (`.agents/contracts/semstreams-{developer,reviewer}.md:246`); ADR-051/058, `migration-beta18.md` and `docs/proposals/*` are history and are not rewritten | `git grep -l` over docs/.agents/openspec/specs *(new)* |
| P13 | The two roots compose Phase-A logging differently, a live spec scenario pins the e2e shape, and two tests assert it by parsing `main.go` | `cmd/semstreams/main.go:170,227` vs `cmd/e2e-semstreams/main.go:159,374`; `openspec/specs/application-logging/spec.md:23-28`; `internal/maxdelivery/boot_order_test.go:24,111,153-158`; `internal/bootstrapobservability/bootstrap_test.go:102` *(new)* |
| P14 | The two roots' flag defaults differ (`json`/8083/`configs/example.json`/`--health-port` vs `text`/6060/`config.json`/`--lifecycle-seed`); pprof binds only under `Debug && port > 0` | `cmd/semstreams/flags.go:42,49,63`; `cmd/e2e-semstreams/main.go:515-521`; `service/pprof.go:34` *(new)* |
| P15 | Three `init()` functions register vocabulary in packages only the e2e binary links | `examples/processors/iot_sensor/vocabulary.go:5`, `examples/processors/document/vocabulary.go:5`, `cmd/e2e-semstreams/mission/state.go:56` *(new)* |

## 6. Invariants, their spec home, and how each is exercised

| # | Invariant | Spec home (delta) | Exercised by |
|---|---|---|---|
| I1 | `FromEnv(cli, build, ∅)` ≡ `Production(cli, build)`: every extension slice empty, every scalar equal — the e2e binary's boot options are the production options | framework-composition ADDED, scenario 1 | `TestE2EBootWithNoOptionsIsTheProductionOptions` (reflection over `Options`) |
| I2 | Each `SEMSTREAMS_E2E_*` name grows exactly the § 2.2 slices by exactly the listed count; a nonempty value enables, `NAME=` does not | scenario 2 | `TestE2EBootOptionAppendsExactlyItsExtensions` (table-driven, 7 rows + the empty-value row) |
| I4 | `cmd/semstreams`'s non-test closure holds no harness/e2e/example package; `cmd/e2e-semstreams`'s holds every package under `test/e2e/harness` plus `internal/e2eboot`, `internal/e2eslowconsumer`, `cmd/e2e-semstreams/{fixtures,mission}` | scenario 3 (replaces "cannot reach the production build") | `TestProductionRootClosureHoldsNoE2EHarness` (`go list -deps`; absorbs `TestProductionBinaryExcludesExamplePackages`) |
| I5 | `docker/Dockerfile` has exactly two runnable targets and no `-tags=`; every tier row's Gate is an env set the service sets exactly | payload-registry MODIFIED, scenario "every tier's target…" | `TestE2ETierTableMatchesComposeAndDockerfile` (reader changed) |
| I6 | The set of names `FromEnv` accepts equals the union of the tier table's Gate column | framework-composition ADDED, scenario 4 | `TestE2EBootVariableSetMatchesTierTable` (reads the same table the B test reads) |
| I7 | The slow-consumer probe runs after `ConnectClient` and before the config manager | none new — behaviour carried | `TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration` moved to `internal/boot` |
| I8 | Both binaries compose the production Phase-A: a client WARN reaches configured local output once and increments `semstreams_log_entries_total` | application-logging MODIFIED, scenario 2 rewritten | the existing production Phase-A test (`bootstrap_test.go:79`) plus the moved `bootstrap_observability_test.go` |

(I3 of the first pass — refusal of unknown names — is dropped with D4.)

PBT decision (`docs/contributing/01-testing.md`): every invariant is an equality over a finite, enumerated set (seven
names, nine slices, two targets); named examples exercise each fully. No Rapid property; no fuzz target.

Skills: `kv-or-stream` — no new communication path, not triggered. `orchestration-check` — no multi-step behaviour
added, not triggered. `new-payload` — no new type, not triggered. `query-pattern` — no new query access, not triggered.

## 7. What changes where

| # | Surface | Change |
|---|---|---|
| 1 | `internal/boot/` *(new)* | `options.go` (§ 2.1, `Production`), `flags.go` (`ParseFlags`, production's parser moved), `run.go` (`run()` moved, extensions applied per § 2.3), `registry.go` (D9), `resources.go` (root resources + close). Tests moved from `cmd/semstreams/`: `signal_shutdown_test.go`, `bootstrap_observability_test.go`, `bootstrap_observability_integration_test.go`, `root_resources_test.go`, `payload_registration_test.go`, `milestone_wiring_test.go`, `slow_consumer_hook_contract_test.go` (I7). The e2e duplicates and `cmd/e2e-semstreams/bootstrap_observability_test.go` (asserts the retired E2E Phase-A) are deleted. |
| 2 | `internal/e2eboot/` *(new)* | `fromenv.go` (§ 2.2, D4), `options_*.go` one file per option (the moved bodies of `registerExampleComponents`, `buildPayloadRegistry`'s e2e half, `seedMission`, the lesson-curation subscription, `prepareE2EProcessBarrierConfig`+`registerE2EProcessBarrier`, `registerE2EMilestoneProbe`, `runSlowConsumerProbe`). Tests: I1, I2, I6, plus `process_barrier_e2e_test.go`'s three config-patch tests untagged and `process_barrier_config_contract_test.go` unchanged. |
| 3 | `cmd/semstreams/main.go`, `cmd/e2e-semstreams/main.go` | thin: `boot.ParseFlags` → options (each main copies its own `Version`/`GitCommit`/`BuildTime` into `Options.Build`) → `boot.Run`; verbs via `boot.RegistryFor`. Deleted: `process_barrier_{e2e,disabled}.go`, `milestone_probe_{e2e,disabled}.go`, `slow_consumer_probe_{e2e,disabled}.go` and their `_test.go` (6 + 3 files), `cmd/semstreams/flags.go` (moved), `cmd/e2e-semstreams/{registry_wiring,bootstrap_observability,milestone_wiring,signal_shutdown}_test.go` (moved, retired or duplicate), e2e `parseCLI`/`getEnvOrDefault`. `cmd/e2e-semstreams/{fixtures,mission}` stay where they are (imported by tests outside `cmd/`, inventory § 6). |
| 4 | `internal/e2eslowconsumer/` | `probe_e2e.go` → `probe.go`, tag removed; same for its test (D10). |
| 5 | `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` | env read removed (D5). |
| 6 | `docker/Dockerfile` | stages `process-barrier-builder`, `e2e-process-barrier`, `slow-consumer-builder`, `e2e-slow-consumer` deleted (`:182-215`). |
| 7 | `docker/compose/` | `agentic.yml:64,68` → `image: c360studio/semstreams:e2e-test`, `target: e2e`, `+SEMSTREAMS_E2E_PROCESS_BARRIER=1` (MILESTONE_PROBE already at `:87`); `e2e-slow-consumer.yml:18,22` → `image: …:e2e-test`, `target: e2e`, `+SEMSTREAMS_E2E_SLOW_CONSUMER=1` (the `e2e-test` tag is what every other `e2e`-target service already uses — *(new)* `grep -n 'image: c360studio/semstreams' docker/compose/*.yml`); `e2e.yml` fixtures service, `tiered.yml` ×3, `research-graph.yml` → `+SEMSTREAMS_E2E_EXAMPLES=1`; `lifecycle.yml` → `+SEMSTREAMS_E2E_MISSION=1`, `--lifecycle-seed` (`:62`) → `SEMSTREAMS_E2E_LIFECYCLE_SEED=<same suffix>`; `ops.yml` → `+SEMSTREAMS_E2E_LESSON_CURATION=1`. |
| 8 | `Taskfile.yml:152,163` | tagged vet line removed; `check:push` description corrected (D10). |
| 9 | `openspec/specs/payload-registry/spec.md:23-33` and the table | MODIFIED (delta in this change): the rule paragraph rewritten; rows agentic, slow-consumer: target `e2e`, gate = env; all `e2e` rows: gate = their variables; scenario "every tier's target…": `-tags=` clause → "exactly two runnable targets and no `-tags=`", and the nonempty-value clause's reason cites the E2E binary's value rule instead of `os.Getenv`; scenario "cannot reach the production build" → closure wording (I4, converse scoped). Every other scenario byte-identical (the review diffed live `:10-103` against the delta). |
| 10 | `openspec/specs/framework-composition/spec.md` | ADDED requirement "One framework boot composes both framework binaries" with scenarios for I1, I2, I4, I6 (delta in this change). |
| 11 | `docs/contributing/02-e2e-tests.md:180-220` | navigation copy: two targets, Gate column, the rule sentence; the `Every tier and the binary it boots` paragraph rewritten. |
| 12 | `docs/concepts/15-payload-registry.md:98-99,289`, `docs/basics/05-first-processor.md:126-133`, `.agents/skills/new-payload/SKILL.md`, `.agents/contracts/semstreams-{developer,reviewer}.md:246`, and the `CLAUDE.md`/`AGENTS.md` rules-table cell "per-binary parity is prose" → names `TestE2EBootWithNoOptionsIsTheProductionOptions` (both files byte-identical, ≤1,100 words — `internal/agentprofiles/profile_contract_test.go`) | point at `internal/boot`'s composer and `internal/e2eboot`; the contracts' "register in both binaries" sentence becomes "register through the composer; an e2e-only registration is an `e2eboot` option". The sweep command is `tasks.md` 6.1. |
| 13 | `test/contract/e2e_tier_binary_contract_test.go`, `test/contract/core_composition_deps_test.go:30-34` | D11: Dockerfile reader → two targets / zero tags; Gate parser → env set; `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` → `TestProductionRootClosureHoldsNoE2EHarness`, absorbing `TestProductionBinaryExcludesExamplePackages`. **Table source precedence flips** in `tierTable` (`:74-110` *(new)*): today the live spec governs whenever it carries the header and an in-flight delta is read only when it does not; on this branch the live table names targets the compose files no longer have, so the suite would be red from the first compose edit until the archive's spec sync (the last commit) — against "push only green states". New rule: exactly one in-flight delta carrying the header governs when one exists (the ambiguity error for two or more stays); otherwise the live spec. On `main` no in-flight change exists, so the live spec governs there exactly as today; the archive makes live = delta. I6's `TestE2EBootVariableSetMatchesTierTable` reads through the same helper. |
| 14 | `openspec/specs/application-logging/spec.md:6-28` | MODIFIED (delta in this change): scenario "E2E client uses the same configured local output" → "the E2E binary composes the production Phase-A"; scenario 1 and the requirement text byte-identical. |
| 15 | `internal/bootstrapobservability/bootstrap.go:57-59`, `bootstrap_test.go:102-133` | `NewE2EPhaseA` and its test deleted (D14). |
| 16 | `internal/maxdelivery/boot_order_test.go:24,111-158` | production half re-pointed from `cmd/semstreams/main.go` to `internal/boot/run.go`; e2e half (`:111-158`) deleted (D14). |
| 17 | `test/e2e/scenarios/ops/composition_root_contract_test.go:26` | reads `internal/boot/run.go` (or the `e2eboot` option file) for the `persona.LoadFromDirectory` string instead of `cmd/e2e-semstreams/main.go`. |

Deleted: 11 tagged files, 5 duplicate/retired tests, 1 dead constructor, 4 Dockerfile stages, 1 Taskfile line. Net
Go: a move of ~1,800 lines into two internal packages plus ~150 new lines (`FromEnv`, tests).

## 8. E2E evidence before the merge

Every tier's boot contract changes (the compose `environment:` block is now load-bearing for eight of twelve
services; five tiers start forwarding logs; two tiers move binaries), and a missing variable fails at boot (unknown
component type → composition validation) or at the tier's first synthetic stamp (unregistered payload) — loudly either
way. Recommended gate (§ 10 OQ1): one local `task e2e:all` green at the final revision (Docker released by the owner
for the day), with the tier log's own `exit=` line and `docker compose ls` = 0 before it (`e2e:clean` tears down every
stack on the host); the ladder's `slow-consumer` and `statistical` jobs in CI. The agentic tier (~5m35s, local-only)
is inside `e2e:all`.

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
| R9 | `FromEnv` refusing unknown `SEMSTREAMS_E2E_*` names, and a `LIFECYCLE_SEED`-without-`MISSION` check | Both repeat guards that already fire (D4, § 2.2); the refusal would reject runner-side variables on a hand-launched binary. |
| R10 | Keeping `NewE2EPhaseA` as a boot option | Nothing would enable it; the slow-consumer tier needs the counter (D8, D14). |
| R11 | A separate `Vocabulary` extension slice | Only needed if OQ2 (b) is ruled; then the `EXAMPLES`/`MISSION` option functions call the explicit registration inside their `Components` closure — no tenth slice. |

## 10. Owner docket — alternative first, recommendation marked

**RULED 2026-09-26** (#1301, transcribed from session — the owner's words govern): **OQ1 (a)**, **OQ2 (a)**; the two
"decided, not asked" items stand.

| # | Question | (a) | (b) | Recommendation |
|---|---|---|---|---|
| OQ1 | E2E gate for this PR | `task e2e:all` once locally at the final revision + the ladder in CI | Only the tiers whose options or logging change (core, structural, statistical, semantic, lifecycle, ops, research-graph, agentic, slow-consumer — nine of twelve) | **(a)** — every service's compose contract changes and five tiers start forwarding logs; the first run reveals any under-declared row, and (b) is already nine tiers. |
| OQ2 | The e2e binary's vocabulary is a superset of production's even with no option set, because three `init()`s register vocabulary in packages only it links (P15); agentic and slow-consumer lose that fidelity against today's tagged production root | Record it as a residual (§ 11.3); the two tiers assert nothing about vocabulary absence | Remove the three `init()`s and register the vocabulary from the `EXAMPLES` and `MISSION` option closures (~10 lines in `examples/processors/{iot_sensor,document}`, `cmd/e2e-semstreams/mission`, plus a census of every test that imports those packages for the side effect) | **(a)** — a doc sentence before code; (b) is the right shape if the census is small, and can follow as its own mechanical change. |

**Decided in the design, not asked — say so if either should have been asked:** D8/D14 reverse the #961 E2E Phase-A
split (five tiers start forwarding logs; required by the slow-consumer tier's counter; the split was preserved
behaviour, not a ruling); D15 converges both mains on production's flag parser and defaults.

Settled by prior ruling, not re-opened here: B's rule and table (PR #1360), D as #1301's shape, "unexported, so no
Tier 1 surface", beta.163 placement, sequencing after #1362 (met: PR #1366 merged 2026-09-25), #1249 docket Q3/Q5
(closed by D10).

## 11. Residuals — recorded, not filed

1. **Sisters do not migrate to the shared boot under this change.** The issue's direction ("Sisters migrate to it; the
   migration table is the six roots") is not met: the ruled shape is internal. The exported contract is a later
   question, after the reference app's friction log (the issue body's own sequencing); it belongs on #1301's
   successor, not here.
2. **#1107 stays open.** D9 gives the verbs and boot one builder, but the full-vs-selected policy #1107 questions is
   unchanged.
3. **`init()` vocabulary superset** (P15, OQ2 (a) unless ruled otherwise).
4. **#961's E2E Phase-A split is reversed** (D8/D14) — recorded here and in the application-logging delta so the
   `gh955-bootstrap-logger-design.md` proposal reads as history.
5. Inventory open facts: the `docs/basics/05` "retired call" residual (#1103) is not reproduced — this change updates
   `:126-133` to name the composer and leaves #1103 open; semteams'/semspec's wiring — irrelevant under the internal
   shape; no `gopls` pass — the design touches no interface indirection, the developer's `go build ./...` after the
   move is the closure check.
