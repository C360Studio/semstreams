# Inventory: E2E execution and gate surface after #1301 / #1390
base: fe9482b7f336e575317cfb45fd1ad7c40baf7904

Source snapshot: `/private/tmp/semstreams-e2e-survey-20260927/source`. Pins below are repository-relative at the
given base. Primary checkout was used only for `git grep <base>` / `git ls-tree <base>` discovery, never as source.
No tests, Docker operations, paid calls, or remote mutations were run. External issue/PR/milestone inventory is
owned by the parent survey and is pending here. This inventory enumerates infrastructure and gate declarations;
scenario business assertions are assigned to the other survey inventories.

## Claimed gap

### openspec/changes/archive/2026-09-27-one-composition-root/design.md

- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:3` — `Change `one-composition-root` · issue #1301 (milestone `v1.0.0-beta.163`, gates the tag) · claim PR #1390 ·`
- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:24` — `Scope bound transcribed on #1301 (2026-09-22): one shared boot (semdev's `internal/boot/boot.go` shape — unexported,`
- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:297` — `way. Recommended gate (§ 10 OQ1): one local `task e2e:all` green at the final revision (Docker released by the owner`
- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:300` — `is inside `e2e:all`.`
- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:320` — `**RULED 2026-09-26** (#1301, transcribed from session — the owner's words govern): **OQ1 (a)**, **OQ2 (a)**; the two`
- `openspec/changes/archive/2026-09-27-one-composition-root/design.md:325` — `| OQ1 | E2E gate for this PR | `task e2e:all` once locally at the final revision + the ladder in CI | Only the tiers whose options or logging change (core, structural, statistical, semantic, lifecycle, ops, research-graph, agentic, slow-consumer — nine of twelve) | **(a)** — every service's compose contract changes and five tiers start forwarding logs; the first run reveals any under-declared row, and (b) is already nine tiers. |`

### openspec/changes/archive/2026-09-27-one-composition-root/tasks.md

- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:14` — `- [x] 0.2 The § 10 docket (OQ1, OQ2) is posted on #1301 (issuecomment-5846936695) and the owner ruled 2026-09-26,`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:15` — `in-session, transcribed under the docket: **OQ1 (a)** — `task e2e:all` once locally + the ladder; **OQ2 (a)** —`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:173` — `- [x] 7.2 E2E per the OQ1 ruling; default (a): `docker compose ls` = 0, then `task e2e:all` at the final revision; the`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:174` — `tier log's own `exit=` line is the result. Record the revision and durations here: **`05182c2e` (the final code`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:175` — `revision; the archive commit after it carries no code), 2026-09-27 08:50:06Z → 09:13:07Z, `task e2e:all` `exit=0` by`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:176` — `the tier log's own line, 1381 s wall; pre-check compose stacks 0, e2e processes 0. Ladder order — core: 6 scenarios`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:177` — `"Scenario completed successfully" (both phases: minted authority, health, main pipeline 36.3 s, SIGTERM during`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:178` — `blocked bootstrap, pre-identity-bucket refusal, phase-2 graph round-trip); structural 0.64 s; statistical 29.3 s;`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:179` — `semantic 11 m 20 s (LLM wait enhanced=2 failed=8 pending=8 and NL intent probes 0/3, 0/2 under the 0.6b model — inside`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:180` — `the tier's own tolerance, the scenario reported success); agentic 5 m 35.5 s, `assertions_run=20`. Totals: 10`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:181` — `scenarios successful, 0 failed, 0 FAIL/panic lines in 2046 log lines. Images pulled first: `seminstruct:qwen3-0.6b``
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:182` — `and `qwen3-1.7b` (what `docker/compose/tiered.yml` pins; `:latest` is only `services.yml`'s), 90 s on 2026-09-27's`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:183` — `network. The log is the coordinating session's scratchpad `e2e-all-05182c2e.log`, not in tree.** Watch the five tiers`

## Spellings of the fact

### Taskfile.yml

- `Taskfile.yml:151` — `check:push:`
- `Taskfile.yml:152` — `desc: "Full pre-push gate, mirrors CI (~11min, needs Docker): build, lint, vet integration+live_llm, schema drift, contract, race unit + integration. Use /preflight for the judgment layer (diff scope, breaking->e2e)."`
- `Taskfile.yml:160` — `- go test ./test/contract/...`
- `Taskfile.yml:161` — `- go test -race ./...`
- `Taskfile.yml:162` — `- task: test:integration`
- `Taskfile.yml:165` — `e2e:tiers:`
- `Taskfile.yml:166` — `desc: Run all tier E2E tests (structural -> statistical -> semantic)`
- `Taskfile.yml:176` — `- task: e2e:tier`
- `Taskfile.yml:178` — `PROFILE: structural`
- `Taskfile.yml:179` — `VARIANT: structural`
- `Taskfile.yml:183` — `- task: e2e:tier`
- `Taskfile.yml:185` — `PROFILE: statistical`
- `Taskfile.yml:186` — `VARIANT: statistical`
- `Taskfile.yml:190` — `- task: e2e:tier`
- `Taskfile.yml:192` — `PROFILE: semantic`
- `Taskfile.yml:193` — `VARIANT: semantic`
- `Taskfile.yml:194` — `EXTRA_ARGS: --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190`
- `Taskfile.yml:197` — `- cd cmd/e2e && ./e2e --compare-tiers --output-dir ./test/e2e/results`
- `Taskfile.yml:200` — `e2e:tier:`
- `Taskfile.yml:205` — `- defer: docker compose -f docker/compose/tiered.yml --profile {{.PROFILE}} down -v --timeout 15`
- `Taskfile.yml:206` — `- docker compose -f docker/compose/tiered.yml --profile {{.PROFILE}} up -d --wait --build`
- `Taskfile.yml:207` — `- cd cmd/e2e && ./e2e --scenario tiered --variant {{.VARIANT}} {{.EXTRA_ARGS}} --output-dir ./test/e2e/results`
- `Taskfile.yml:209` — `e2e:all:`
- `Taskfile.yml:210` — `desc: Run all E2E tests (core -> inference tiers -> agentic)`
- `Taskfile.yml:212` — `- task: e2e:core`
- `Taskfile.yml:213` — `- task: e2e:tiers`
- `Taskfile.yml:214` — `- task: e2e:agentic`

### cmd/e2e/main.go

- `cmd/e2e/main.go:40` — `func main() {`
- `cmd/e2e/main.go:87` — `exitCode := runScenarios(ctx, logger, edgeClient, flags)`
- `cmd/e2e/main.go:88` — `os.Exit(exitCode)`
- `cmd/e2e/main.go:314` — `if flags.scenarioName == "" || flags.scenarioName == "all" {`
- `cmd/e2e/main.go:316` — `return runAllScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:317` — `} else if flags.scenarioName == "semantic" {`
- `cmd/e2e/main.go:319` — `return runSemanticScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:320` — `} else if flags.scenarioName == "rules" {`
- `cmd/e2e/main.go:322` — `return runRulesScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:326` — `scenario := createScenario(edgeClient, flags)`
- `cmd/e2e/main.go:328` — `logger.Error("Unknown scenario", "name", flags.scenarioName)`
- `cmd/e2e/main.go:330` — `return 1`
- `cmd/e2e/main.go:334` — `return runScenario(ctx, logger, scenario, flags)`
- `cmd/e2e/main.go:353` — `case "core-health", "health":`
- `cmd/e2e/main.go:355` — `case "core-dataflow", "dataflow":`
- `cmd/e2e/main.go:364` — `case "core-graph-roundtrip", "graph-roundtrip":`
- `cmd/e2e/main.go:377` — `case "core-minted-authority", "minted-authority":`
- `cmd/e2e/main.go:383` — `case "core-pre-identity-seed":`
- `cmd/e2e/main.go:386` — `case "core-pre-identity-assert":`
- `cmd/e2e/main.go:389` — `case "core-slow-consumer", "slow-consumer":`
- `cmd/e2e/main.go:395` — `case "tiered", "structural", "statistical", "semantic":`
- `cmd/e2e/main.go:425` — `return scenarios.NewTieredScenario(edgeClient, flags.udpEndpoint, cfg)`
- `cmd/e2e/main.go:428` — `case "agentic":`
- `cmd/e2e/main.go:431` — `return agentic.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:434` — `case "deep-research":`
- `cmd/e2e/main.go:437` — `return deepresearch.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:440` — `case "research-graph":`
- `cmd/e2e/main.go:445` — `return researchgraph.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:446` — `case "research-graph-execute":`
- `cmd/e2e/main.go:448` — `cfg.FixtureMode = researchgraph.FixtureModeExecute`
- `cmd/e2e/main.go:452` — `return researchgraph.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:455` — `case "crud-tools":`
- `cmd/e2e/main.go:458` — `return crudtools.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:461` — `case "lessons":`
- `cmd/e2e/main.go:462` — `return lessonsscenario.NewScenario()`
- `cmd/e2e/main.go:465` — `case "ops":`
- `cmd/e2e/main.go:469` — `return opsscenario.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:472` — `case "lifecycle":`
- `cmd/e2e/main.go:480` — `return lifecyclescenario.NewScenario(edgeClient, cfg)`
- `cmd/e2e/main.go:483` — `case "throughput":`
- `cmd/e2e/main.go:484` — `return newThroughputScenario(flags)`

### test/e2e/scenarios/scenario.go

- `test/e2e/scenarios/scenario.go:10` — `type Scenario interface {`
- `test/e2e/scenarios/scenario.go:19` — `Setup(ctx context.Context) error`
- `test/e2e/scenarios/scenario.go:23` — `Execute(ctx context.Context) (*Result, error)`
- `test/e2e/scenarios/scenario.go:27` — `Teardown(ctx context.Context) error`
- `test/e2e/scenarios/scenario.go:31` — `type Result struct {`
- `test/e2e/scenarios/scenario.go:39` — `Success bool   `json:"success"``
- `test/e2e/scenarios/scenario.go:45` — `Errors   []string       `json:"errors,omitempty"``
- `test/e2e/scenarios/scenario.go:48` — `// AssertionsRun is the number of assertions the scenario actually executed.`
- `test/e2e/scenarios/scenario.go:49` — `AssertionsRun int `json:"assertions_run,omitempty"``
- `test/e2e/scenarios/scenario.go:53` — `Structured *TieredResults `json:"structured,omitempty"``

### internal/e2eboot/fromenv.go

- `internal/e2eboot/fromenv.go:32` — `{name: "SEMSTREAMS_E2E_EXAMPLES", enable: enableExamples},`
- `internal/e2eboot/fromenv.go:33` — `{name: "SEMSTREAMS_E2E_MISSION", enable: enableMission},`
- `internal/e2eboot/fromenv.go:34` — `{name: "SEMSTREAMS_E2E_LIFECYCLE_SEED", enable: enableLifecycleSeed},`
- `internal/e2eboot/fromenv.go:35` — `{name: "SEMSTREAMS_E2E_LESSON_CURATION", enable: enableLessonCuration},`
- `internal/e2eboot/fromenv.go:36` — `{name: "SEMSTREAMS_E2E_PROCESS_BARRIER", enable: enableProcessBarrier},`
- `internal/e2eboot/fromenv.go:37` — `{name: "SEMSTREAMS_E2E_MILESTONE_PROBE", enable: enableMilestoneProbe},`
- `internal/e2eboot/fromenv.go:38` — `{name: "SEMSTREAMS_E2E_SLOW_CONSUMER", enable: enableSlowConsumer},`
- `internal/e2eboot/fromenv.go:48` — `func FromEnv(cli boot.CLI, build boot.BuildInfo, lookup func(string) (string, bool)) boot.Options {`
- `internal/e2eboot/fromenv.go:49` — `opts := boot.Production(cli, build)`
- `internal/e2eboot/fromenv.go:51` — `if value, _ := lookup(opt.name); value != "" {`
- `internal/e2eboot/fromenv.go:52` — `opt.enable(&opts, value)`

### cmd/semstreams/main.go

- `cmd/semstreams/main.go:51` — `if err := boot.Run(context.Background(), opts); err != nil {`

### cmd/e2e-semstreams/main.go

- `cmd/e2e-semstreams/main.go:52` — `if err := boot.Run(context.Background(), opts); err != nil {`

### docker/Dockerfile

- `docker/Dockerfile:46` — `RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \`
- `docker/Dockerfile:52` — `./cmd/semstreams`
- `docker/Dockerfile:55` — `RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \`
- `docker/Dockerfile:60` — `./cmd/e2e-semstreams`
- `docker/Dockerfile:65` — `FROM alpine:latest AS production`
- `docker/Dockerfile:117` — `FROM alpine:latest AS e2e`
- `docker/Dockerfile:133` — `COPY --from=builder --chown=semstreams:semstreams /build/e2e-semstreams-bin /app/semstreams`

## Adjacent claims

### openspec/specs/payload-registry/spec.md

- `openspec/specs/payload-registry/spec.md:19` — `A production-target e2e tier stamps only what the production binary registers (owner ruling on #1100, 2026-08-27):`
- `openspec/specs/payload-registry/spec.md:23` — `That choice generalises to one rule for every E2E tier (owner rulings on #1249 and #1301, 2026-09-22). Both binaries boot`
- `openspec/specs/payload-registry/spec.md:25` — ``SEMSTREAMS_E2E_*` environment variables (the framework-composition specification). A tier that proves the shipped`
- `openspec/specs/payload-registry/spec.md:33` — `The tier's binary, target and gate are READ from the artifacts that boot it — `build.target` in the tier's compose service,`
- `openspec/specs/payload-registry/spec.md:34` — `the Go package of that target in `docker/Dockerfile`, the `SEMSTREAMS_E2E_*` variables the service sets — never predicted`
- `openspec/specs/payload-registry/spec.md:37` — `| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate | E2E-only registrations and hooks | Synthetic types stamped on `entity.create` |`
- `openspec/specs/payload-registry/spec.md:39` — `| core — phase 1 (`core-health`, `core-dataflow`) | `e2e.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |`
- `openspec/specs/payload-registry/spec.md:40` — `| core — phase 2 (`core-graph-roundtrip`), lessons | `e2e.yml` `semstreams-fixtures` (profile fixtures) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | fixture payloads | `test.fixture.v1` (evidence fixture for lessons) |`
- `openspec/specs/payload-registry/spec.md:41` — `| structural | `tiered.yml` `semstreams-structural` (profile structural) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components, fixture payloads | `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1` |`
- `openspec/specs/payload-registry/spec.md:42` — `| statistical, throughput | `tiered.yml` `semstreams` (profile statistical) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components | none |`
- `openspec/specs/payload-registry/spec.md:43` — `| semantic (and its `:8b` / `:frontier` overlays) | `tiered.yml` `semstreams-ml` (profile semantic) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components | none |`
- `openspec/specs/payload-registry/spec.md:44` — `| lifecycle | `lifecycle.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_MISSION=1`, `SEMSTREAMS_E2E_LIFECYCLE_SEED=<suffix>` | mission component and workflow, post-start seed | none (`lifecycle.harness.v1` is a framework type) |`
- `openspec/specs/payload-registry/spec.md:45` — `| ops | `ops.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_LESSON_CURATION=1` | lesson-curation control responder | none (its seed is the framework type `agentic.loop_completed.v1`, written by direct `PutKV` — `ops/scenario.go:439,484`) |`
- `openspec/specs/payload-registry/spec.md:46` — `| research-graph | `research-graph.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | fixture payloads | `research.e2e_search_seed.v1` |`
- `openspec/specs/payload-registry/spec.md:47` — `| crud-tools | `crud-tools.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none on create (`e2e.probe.v1` is a direct `PutKV`) |`
- `openspec/specs/payload-registry/spec.md:48` — `| deep-research | `deep-research.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |`
- `openspec/specs/payload-registry/spec.md:49` — `| agentic | `agentic.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_PROCESS_BARRIER=1`, `SEMSTREAMS_E2E_MILESTONE_PROBE=1` | process barrier (tool executor), milestone settlement probe (`MilestoneHandler`) | none |`
- `openspec/specs/payload-registry/spec.md:50` — `| slow-consumer | `e2e-slow-consumer.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_SLOW_CONSUMER=1` | slow-consumer boot probe | none |`
- `openspec/specs/payload-registry/spec.md:81` — `#### Scenario: every tier's target, binary and gate are the ones its artifacts carry`
- `openspec/specs/payload-registry/spec.md:86` — `package, and the service sets exactly the row's `SEMSTREAMS_E2E_*` variables`
- `openspec/specs/payload-registry/spec.md:90` — `- **AND** no compose file mentions a `SEMSTREAMS_E2E_*` variable its rows do not declare — including in an overlay service`

### docs/contributing/02-e2e-tests.md

- `docs/contributing/02-e2e-tests.md:182` — `All compose files are in `docker/compose/`. Both framework binaries boot through one composition function,`
- `docs/contributing/02-e2e-tests.md:184` — `variables (`internal/e2eboot`). A tier proving the shipped binary boots `cmd/semstreams` through the `production``
- `docs/contributing/02-e2e-tests.md:187` — ``e2e` target by setting exactly that option's variable to a nonempty value; with none set, the E2E binary's boot`
- `docs/contributing/02-e2e-tests.md:188` — `options are the production options. No build tag gates an E2E-only hook, and `docker/Dockerfile` has exactly two`
- `docs/contributing/02-e2e-tests.md:191` — `**The source of truth is the tier table in `openspec/specs/payload-registry/spec.md`**, and`
- `docs/contributing/02-e2e-tests.md:192` — ``test/contract/e2e_tier_binary_contract_test.go` re-reads it against these compose files and `docker/Dockerfile` on`
- `docs/contributing/02-e2e-tests.md:193` — `every run. While an in-flight change carries a restated copy of the table in its delta, exactly one such delta governs`
- `docs/contributing/02-e2e-tests.md:198` — `Twelve compose services, thirteen `e2e:<tier>` tasks. The units differ on purpose: `core` runs in two phases`
- `docs/contributing/02-e2e-tests.md:216` — ``task e2e:openai-responses` is the fourteenth task and is not in this table: it is a live wire test against the paid`
- `docs/contributing/02-e2e-tests.md:217` — `API with no container of its own. Each tier file above defines its own `nats:`; `services.yml` carries the shared`
- `docs/contributing/02-e2e-tests.md:298` — `## CI Integration`
- `docs/contributing/02-e2e-tests.md:300` — `### PR Checks`
- `docs/contributing/02-e2e-tests.md:304` — `- task e2e:core`
- `docs/contributing/02-e2e-tests.md:305` — `- task e2e:structural`
- `docs/contributing/02-e2e-tests.md:308` — `### Main Branch`
- `docs/contributing/02-e2e-tests.md:312` — `- task e2e:core`
- `docs/contributing/02-e2e-tests.md:313` — `- task e2e:structural`
- `docs/contributing/02-e2e-tests.md:314` — `- task e2e:statistical`
- `docs/contributing/02-e2e-tests.md:317` — `### Release`
- `docs/contributing/02-e2e-tests.md:321` — `- task e2e:semantic`
- `docs/contributing/02-e2e-tests.md:324` — `## Breaking Changes Require an E2E Tier Before Merge`
- `docs/contributing/02-e2e-tests.md:326` — `Any commit or tag marked **BREAKING** in the changelog or commit message (a `!` after the type/scope) MUST have at`
- `docs/contributing/02-e2e-tests.md:327` — `least one relevant E2E tier green BEFORE the breaking commit lands on main. Unit and integration tests do not`
- `docs/contributing/02-e2e-tests.md:336` — `Before tagging anything labeled BREAKING:`
- `docs/contributing/02-e2e-tests.md:339` — `task e2e:semantic            # Or whichever tier covers the touched path`
- `docs/contributing/02-e2e-tests.md:340` — `# Confirm green. If no tier covers the path, that is a coverage gap: file it before tagging.`
- `docs/contributing/02-e2e-tests.md:350` — `If only `cmd/e2e-semstreams` has it, the framework binary is half-migrated. Follow the`
- `docs/contributing/02-e2e-tests.md:351` — `[payload registration checklist](../../.agents/skills/new-payload/SKILL.md). The per-PR ladder does not yet run the`
- `docs/contributing/02-e2e-tests.md:352` — `semantic or agentic tier on a `!` PR; the per-PR gate is gh#1117, the nightly run gh#769.`

### openspec/specs/release-candidate-proof/spec.md

- `openspec/specs/release-candidate-proof/spec.md:9` — `Every retained advertised deterministic path SHALL have a green exact-candidate result before tag authorization.`
- `openspec/specs/release-candidate-proof/spec.md:10` — `Issues #301, #844, and #860 are retained gates. A nonzero test or wrapper result SHALL be treated as red. The`
- `openspec/specs/release-candidate-proof/spec.md:13` — `Every release-truth finding outside approved runtime scope SHALL be recorded as an accepted limitation, a separately`
- `openspec/specs/release-candidate-proof/spec.md:14` — `approved blocker, or a deferred named program. Recording a finding SHALL NOT imply conformance or implementation`
- `openspec/specs/release-candidate-proof/spec.md:17` — `The binding decision record SHALL be`
- `openspec/specs/release-candidate-proof/spec.md:18` — ``openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL record owner, decision`
- `openspec/specs/release-candidate-proof/spec.md:20` — `candidate identity, command results, timestamps, and evidence pointers SHALL live in the immutable`
- `openspec/specs/release-candidate-proof/spec.md:21` — ``candidate-proof-<fullSHA>` GitHub Release asset. Product tag, artifact, fresh-state publication, and final decision`
- `openspec/specs/release-candidate-proof/spec.md:114` — `### Requirement: Candidate proof binds exact commands and active observation`
- `openspec/specs/release-candidate-proof/spec.md:116` — `The candidate-proof record SHALL bind and record the exact commands in`
- `openspec/specs/release-candidate-proof/spec.md:117` — ``openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/candidate-evidence.md` for focused tests, lint, full`
- `openspec/specs/release-candidate-proof/spec.md:118` — `race, integration, schema generation, schema/spec no-drift, contracts, strict OpenSpec, statistical, semantic,`
- `openspec/specs/release-candidate-proof/spec.md:119` — `agentic, research direct-plus-execute, deep-research, crud-tools, and ops gates. It SHALL record runner identity, UTC`
- `openspec/specs/release-candidate-proof/spec.md:120` — `start/end, exit/result, and log or artifact SHA-256 for every command.`
- `openspec/specs/release-candidate-proof/spec.md:122` — `For beta.161, the detached candidate-proof record SHALL also contain a distinct normative row for `task e2e:core`.`
- `openspec/specs/release-candidate-proof/spec.md:123` — `That row and the existing `task e2e:semantic` row SHALL each record the exact command, runner identity, UTC start/end,`
- `openspec/specs/release-candidate-proof/spec.md:124` — `exit/result, and log or artifact SHA-256. Semantic proof SHALL additionally retain the mandatory active-polling`
- `openspec/specs/release-candidate-proof/spec.md:125` — `record below. Neither statistical coverage nor any pre-selection or prior-worktree result SHALL transfer, replace, or`
- `openspec/specs/release-candidate-proof/spec.md:126` — `satisfy either exact-candidate row.`
- `openspec/specs/release-candidate-proof/spec.md:151` — `Every bound `go test` command SHALL use `-count=1` so cached results cannot satisfy exact-candidate proof. The focused`
- `openspec/specs/release-candidate-proof/spec.md:155` — `One `task e2e:research-graph` invocation SHALL prove both isolated direct and execute/fusion rounds. One`
- `openspec/specs/release-candidate-proof/spec.md:156` — ``task e2e:crud-tools` invocation MAY prove #301 and #860 only when their distinct assertions are identified. The #860`
- `openspec/specs/release-candidate-proof/spec.md:217` — `The release owner SHALL authorize a product tag only after all candidate-proof gates are green and the proof records`
- `openspec/specs/release-candidate-proof/spec.md:222` — `The product tag SHALL resolve to the authorized candidate SHA. Release publication SHALL NOT perform or require a`
- `openspec/specs/release-candidate-proof/spec.md:225` — `After publication, a separate immutable asset on the product GitHub Release SHALL link and externally digest the`
- `openspec/specs/release-candidate-proof/spec.md:231` — `The candidate tree SHALL NOT be edited after proof to inject release facts. Downstream repositories MAY pin and adopt`
- `openspec/specs/release-candidate-proof/spec.md:232` — `after publication; they SHALL NOT be treated as exhaustive pre-tag gates. Discovery of retained deployed state SHALL`
- `openspec/specs/release-candidate-proof/spec.md:238` — `- **WHEN** any required pre-tag gate is red or missing`
- `openspec/specs/release-candidate-proof/spec.md:239` — `- **THEN** the release owner rejects tag authorization`

### openspec/changes/archive/2026-09-27-one-composition-root/tasks.md

- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:160` — ``assertions_run:11 known_dropped:8`, on the `e2e` target) and `task e2e:core` exit=0 (both phases; phase 2's`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:161` — ``core-graph-roundtrip` passed against the e2e target with `SEMSTREAMS_E2E_EXAMPLES=1`).`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:162` — `Coordinator's re-measurement at `e1f1bd8b` (in-tree record): `task lint` 0, `go vet ./...` 0 (no `-tags=`),`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:163` — ``go run ./cmd/entity-id-audit .` 0, `go test ./test/contract/...` ok, boot packages 11 ok / 0 fail, Dockerfile`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:164` — `0 `tags=` / 3 `FROM`, production closure ∩ forbidden = 0, e2e closure ⊇ the seven hook packages. Fix pass`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:165` — `(`reconciliation.md` § Fix pass) gated before its push with: `go build ./...`, `task lint`, `go vet ./...`,`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:166` — ``go test -race` over `internal/boot`, `internal/e2eboot`, `test/contract`, `cmd/...` — the FULL `go test -race`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:167` — `./...` and `task test:integration` were NOT re-run locally on the fix-pass commit (session handed off under`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:168` — `compaction pressure); CI's Test job on that commit is the evidence until the next session re-runs them. Re-run 2026-09-27 at `05182c2e`: CI's Test job at `9a069bf3` was RED on `TestBinaryBootOrder``
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:169` — `(`internal/maxdelivery/boot_order_test.go`, an AST test over `../boot/run.go` that still asserted `createNATSClient`'s`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:170` — `pre-fix-pass argument positions; not the #1397 flake; fixed in `05182c2e`, a two-line test change). At `05182c2e`:`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:171` — ``task check:push` green through the race unit suite (162 ok / 0 FAIL; its integration step yielded the host lock to a`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:172` — `sibling worktree's run), `task test:integration` separately exit=0 (160 ok / 0 FAIL), CI green.`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:185` — `**Attempt 1 at `e1f1bd8b` (2026-09-26 15:41Z, before the fix pass): core (both phases, incl. the`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:186` — `pre-identity-bucket refusal), structural and statistical scenarios all "Scenario completed successfully"; the`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:187` — `semantic tier then wedged for 36 minutes pulling `ghcr.io/c360studio/seminstruct:latest` (~35 KB/s, then 0 B in`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:188` — `a 20 s sample; the image is not on the host — only `semembed` is) and the run was aborted by the coordinator.`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:189` — `Not evidence for the gate; the gate run is still owed at the final code revision on a network that can pull`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:190` — `seminstruct (`docker pull ghcr.io/c360studio/seminstruct:latest` first, then `task e2e:all`).**`
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:191` — `- [x] 7.3 (archived 2026-09-27 in the last content commit; the post-merge body check below is owed at the squash) Archive + spec sync is the last content commit; the squash body is authored (`--body-file`) and checked with`

## Consumers

### .github/workflows/e2e-ladder.yml

- `.github/workflows/e2e-ladder.yml:11` — `# Tier choice (owner, 2026-07-23): run the `statistical` tier per-PR. It runs`
- `.github/workflows/e2e-ladder.yml:12` — `# the tiered scenario at --variant statistical, which subsumes the structural`
- `.github/workflows/e2e-ladder.yml:13` — `# stages and drives the full graph path under BM25 + community detection — no`
- `.github/workflows/e2e-ladder.yml:15` — `# Epic D "automate a tier first" (ci.yml itself runs no e2e).`
- `.github/workflows/e2e-ladder.yml:17` — `# NOT covered here (deliberately, for CI cost):`
- `.github/workflows/e2e-ladder.yml:18` — `#   - e2e:core's coverage is subsumed by the statistical run (owner, 2026-07-28):`
- `.github/workflows/e2e-ladder.yml:21` — `#   - e2e:semantic's heavy quality variants (:8b, :frontier) are too heavy for`
- `.github/workflows/e2e-ladder.yml:22` — `#     per-PR and stay pre-tag/manual (docs/contributing/02-e2e-tests.md: a relevant e2e tier green`
- `.github/workflows/e2e-ladder.yml:23` — `#     before any BREAKING tag). The DEFAULT semantic variant is already the`
- `.github/workflows/e2e-ladder.yml:24` — `#     CI-shaped small-model configuration (semembed + seminstruct`
- `.github/workflows/e2e-ladder.yml:25` — `#     qwen3-0.6b/1.7b) for exactly this reason, but runs in no workflow yet.`
- `.github/workflows/e2e-ladder.yml:26` — `#     Owner ruling (2026-08-27, #1117): wiring it here is PER-PR, not`
- `.github/workflows/e2e-ladder.yml:27` — `#     nightly — schedule-triggered runs are reserved for out-of-band security`
- `.github/workflows/e2e-ladder.yml:28` — `#     scanning (CVE and similar), never functional e2e. The open question is`
- `.github/workflows/e2e-ladder.yml:29` — `#     which assertions the per-PR gate carries (path vs quality), not`
- `.github/workflows/e2e-ladder.yml:30` — `#     per-PR-vs-nightly. Tracked in issue #1117.`
- `.github/workflows/e2e-ladder.yml:33` — `pull_request:`
- `.github/workflows/e2e-ladder.yml:34` — `workflow_dispatch:`
- `.github/workflows/e2e-ladder.yml:37` — `group: e2e-ladder-${{ github.ref }}`
- `.github/workflows/e2e-ladder.yml:38` — `cancel-in-progress: true`
- `.github/workflows/e2e-ladder.yml:44` — `e2e-slow-consumer:`
- `.github/workflows/e2e-ladder.yml:47` — `timeout-minutes: 10`
- `.github/workflows/e2e-ladder.yml:68` — `- name: Reserve e2e host ports against ephemeral allocation`
- `.github/workflows/e2e-ladder.yml:69` — `run: scripts/e2e-reserve-ports.sh`
- `.github/workflows/e2e-ladder.yml:72` — `run: task e2e:slow-consumer`
- `.github/workflows/e2e-ladder.yml:74` — `e2e-statistical:`
- `.github/workflows/e2e-ladder.yml:77` — `timeout-minutes: 25`
- `.github/workflows/e2e-ladder.yml:98` — `- name: Reserve e2e host ports against ephemeral allocation`
- `.github/workflows/e2e-ladder.yml:99` — `run: scripts/e2e-reserve-ports.sh`
- `.github/workflows/e2e-ladder.yml:107` — `run: bash scripts/e2e-check-ports_fixture_test.sh`
- `.github/workflows/e2e-ladder.yml:115` — `run: bash scripts/e2e-reserve-ports_fixture_test.sh`
- `.github/workflows/e2e-ladder.yml:119` — `run: python3 scripts/e2e-statistical-up_fixture_test.py`
- `.github/workflows/e2e-ladder.yml:122` — `run: task e2e:statistical`

### .github/workflows/ci.yml

- `.github/workflows/ci.yml:4` — `push:`
- `.github/workflows/ci.yml:5` — `branches: [main, develop]`
- `.github/workflows/ci.yml:6` — `pull_request:`
- `.github/workflows/ci.yml:7` — `branches: [main, develop]`
- `.github/workflows/ci.yml:249` — `needs: [lint, test, build, schema-validation, api-compat]`

### .github/workflows/release.yml

- `.github/workflows/release.yml:4` — `push:`
- `.github/workflows/release.yml:5` — `tags:`
- `.github/workflows/release.yml:6` — `- "v*"`
- `.github/workflows/release.yml:13` — `build-binaries:`
- `.github/workflows/release.yml:83` — `needs: build-binaries`

### .github/workflows/container.yml

- `.github/workflows/container.yml:5` — `workflow_run:`
- `.github/workflows/container.yml:6` — `workflows: ["CI"]`
- `.github/workflows/container.yml:7` — `types: [completed]`
- `.github/workflows/container.yml:8` — `branches: [main]`
- `.github/workflows/container.yml:10` — `push:`
- `.github/workflows/container.yml:11` — `tags: ["v*"]`
- `.github/workflows/container.yml:21` — `# Only run if CI succeeded (for workflow_run) or if triggered by tag`
- `.github/workflows/container.yml:22` — `if: >`
- `.github/workflows/container.yml:23` — `github.event_name == 'push' ||`
- `.github/workflows/container.yml:24` — `github.event.workflow_run.conclusion == 'success'`

### .github/workflows/sister-validation.yml

- `.github/workflows/sister-validation.yml:17` — `# DISABLED pre-v1 (owner, 2026-07-23) — no automatic triggers. The sisters`
- `.github/workflows/sister-validation.yml:18` — `# are downstream holdout adopters, not per-commit or tag-time lockstep gates.`
- `.github/workflows/sister-validation.yml:19` — `# Framework ingest→graph→query remains gated by the repository's own E2E ladder`
- `.github/workflows/sister-validation.yml:20` — `# and pre-tag proof. Any future automatic trigger is a separate owner decision;`
- `.github/workflows/sister-validation.yml:21` — `# this workflow currently runs only through workflow_dispatch.`
- `.github/workflows/sister-validation.yml:23` — `workflow_dispatch:`
- `.github/workflows/sister-validation.yml:144` — `semstreams-core:`
- `.github/workflows/sister-validation.yml:145` — `name: semstreams e2e core`
- `.github/workflows/sister-validation.yml:148` — `if: github.event_name != 'pull_request'`
- `.github/workflows/sister-validation.yml:162` — `run: task e2e:core`

### .github/workflows/semspec-validation.yml

- `.github/workflows/semspec-validation.yml:4` — `workflow_dispatch:`
- `.github/workflows/semspec-validation.yml:24` — `- name: Checkout semspec`
- `.github/workflows/semspec-validation.yml:27` — `repository: C360Studio/semspec`
- `.github/workflows/semspec-validation.yml:56` — `task e2e:full`

### docker/compose/e2e.yml

- `docker/compose/e2e.yml:56` — `target: production`
- `docker/compose/e2e.yml:108` — `target: e2e`
- `docker/compose/e2e.yml:124` — `- SEMSTREAMS_E2E_EXAMPLES=1`

### docker/compose/tiered.yml

- `docker/compose/tiered.yml:204` — `target: e2e`
- `docker/compose/tiered.yml:219` — `- SEMSTREAMS_E2E_EXAMPLES=1`
- `docker/compose/tiered.yml:259` — `target: e2e`
- `docker/compose/tiered.yml:274` — `- SEMSTREAMS_E2E_EXAMPLES=1`
- `docker/compose/tiered.yml:313` — `target: e2e`
- `docker/compose/tiered.yml:337` — `- SEMSTREAMS_E2E_EXAMPLES=1`

### docker/compose/lifecycle.yml

- `docker/compose/lifecycle.yml:56` — `target: e2e`
- `docker/compose/lifecycle.yml:70` — `- SEMSTREAMS_E2E_MISSION=1`
- `docker/compose/lifecycle.yml:71` — `- SEMSTREAMS_E2E_LIFECYCLE_SEED=gcs.lifecycle.mission.m001`

### docker/compose/ops.yml

- `docker/compose/ops.yml:76` — `target: e2e`
- `docker/compose/ops.yml:89` — `- SEMSTREAMS_E2E_LESSON_CURATION=1`

### docker/compose/research-graph.yml

- `docker/compose/research-graph.yml:67` — `target: e2e`
- `docker/compose/research-graph.yml:83` — `- SEMSTREAMS_E2E_EXAMPLES=1`

### docker/compose/crud-tools.yml

- `docker/compose/crud-tools.yml:66` — `target: production`

### docker/compose/deep-research.yml

- `docker/compose/deep-research.yml:69` — `target: production`

### docker/compose/agentic.yml

- `docker/compose/agentic.yml:68` — `target: e2e`
- `docker/compose/agentic.yml:83` — `- SEMSTREAMS_E2E_PROCESS_BARRIER=1`
- `docker/compose/agentic.yml:88` — `- SEMSTREAMS_E2E_MILESTONE_PROBE=1`

### docker/compose/e2e-slow-consumer.yml

- `docker/compose/e2e-slow-consumer.yml:22` — `target: e2e`
- `docker/compose/e2e-slow-consumer.yml:33` — `- SEMSTREAMS_E2E_SLOW_CONSUMER=1`

### cmd/e2e/main.go

- `cmd/e2e/main.go:510` — `func runScenario(ctx context.Context, logger *slog.Logger, scenario scenarios.Scenario, flags *cliFlags) int {`
- `cmd/e2e/main.go:513` — `if err := scenario.Setup(ctx); err != nil {`
- `cmd/e2e/main.go:514` — `logger.Error("Scenario setup failed", "error", err)`
- `cmd/e2e/main.go:515` — `return 1`
- `cmd/e2e/main.go:519` — `result, err := scenario.Execute(ctx)`
- `cmd/e2e/main.go:523` — `if teardownErr := scenario.Teardown(ctx); teardownErr != nil {`
- `cmd/e2e/main.go:524` — `logger.Warn("Teardown failed", "error", teardownErr)`
- `cmd/e2e/main.go:527` — `if err != nil {`
- `cmd/e2e/main.go:528` — `logger.Error("Scenario failed", "error", err, "assertions_run", assertionsRun(result))`
- `cmd/e2e/main.go:529` — `return 1`
- `cmd/e2e/main.go:532` — `if !result.Success {`
- `cmd/e2e/main.go:533` — `logger.Error("Scenario completed with failure",`
- `cmd/e2e/main.go:536` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:537` — `return 1`
- `cmd/e2e/main.go:540` — `logger.Info("Scenario completed successfully",`
- `cmd/e2e/main.go:543` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:546` — `if flags.outputDir != "" && result.Structured != nil {`
- `cmd/e2e/main.go:547` — `filepath, err := scenarios.SaveStructuredResults(result.Structured, flags.outputDir)`
- `cmd/e2e/main.go:548` — `if err != nil {`
- `cmd/e2e/main.go:549` — `logger.Warn("Failed to save structured results", "error", err)`
- `cmd/e2e/main.go:559` — `metricsPath, err := saveMetricsDump(logger, flags.metricsURL, variant, flags.outputDir)`
- `cmd/e2e/main.go:560` — `if err != nil {`
- `cmd/e2e/main.go:561` — `logger.Warn("Failed to save metrics dump", "error", err)`
- `cmd/e2e/main.go:567` — `return 0`
- `cmd/e2e/main.go:617` — `tests := []scenarios.Scenario{`
- `cmd/e2e/main.go:618` — `scenarios.NewCoreHealthScenario(obsClient, nil),`
- `cmd/e2e/main.go:619` — `scenarios.NewCoreDataflowScenario(obsClient, wsClient, udpEndpoint, nil),`
- `cmd/e2e/main.go:627` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `cmd/e2e/main.go:629` — `if exitCode == 0 {`
- `cmd/e2e/main.go:632` — `} else {`
- `cmd/e2e/main.go:633` — `failed++`
- `cmd/e2e/main.go:638` — `logger.Info("Test suite complete",`
- `cmd/e2e/main.go:643` — `if failed > 0 {`
- `cmd/e2e/main.go:644` — `return 1`
- `cmd/e2e/main.go:646` — `return 0`

### test/e2e/results/writer.go

- `test/e2e/results/writer.go:105` — `func (w *Writer) WriteRun(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:107` — `if err := os.MkdirAll(w.outputDir, 0755); err != nil {`
- `test/e2e/results/writer.go:108` — `return "", fmt.Errorf("creating output directory: %w", err)`
- `test/e2e/results/writer.go:112` — `filename := fmt.Sprintf("e2e-results-%s-%s.json",`
- `test/e2e/results/writer.go:118` — `data, err := json.MarshalIndent(run, "", "  ")`
- `test/e2e/results/writer.go:120` — `return "", fmt.Errorf("marshaling results: %w", err)`
- `test/e2e/results/writer.go:124` — `if err := os.WriteFile(filepath, data, 0644); err != nil {`
- `test/e2e/results/writer.go:125` — `return "", fmt.Errorf("writing results file: %w", err)`
- `test/e2e/results/writer.go:132` — `func (w *Writer) WriteLatest(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:133` — `filepath, err := w.WriteRun(run)`
- `test/e2e/results/writer.go:140` — `_ = os.Remove(latestLink) // Remove existing link if present`
- `test/e2e/results/writer.go:141` — `_ = os.Symlink(filepath, latestLink)`
- `test/e2e/results/writer.go:185` — `func CreateTestRun(`
- `test/e2e/results/writer.go:202` — `run.Summary = computeSummary(scenarioResults)`
- `test/e2e/results/writer.go:208` — `func computeSummary(results []scenarios.Result) Summary {`
- `test/e2e/results/writer.go:214` — `if r.Success {`
- `test/e2e/results/writer.go:215` — `summary.PassedScenarios++`
- `test/e2e/results/writer.go:217` — `summary.FailedScenarios++`
- `test/e2e/results/writer.go:219` — `summary.TotalErrors += len(r.Errors)`
- `test/e2e/results/writer.go:220` — `summary.TotalWarnings += len(r.Warnings)`
- `test/e2e/results/writer.go:224` — `summary.SuccessRate = float64(summary.PassedScenarios) / float64(summary.TotalScenarios)`
- `test/e2e/results/writer.go:227` — `summary.AllPassed = summary.PassedScenarios == summary.TotalScenarios`

### test/e2e/scenarios/results.go

- `test/e2e/scenarios/results.go:738` — `func SaveStructuredResults(tr *TieredResults, outputDir string) (string, error) {`
- `test/e2e/scenarios/results.go:740` — `return "", fmt.Errorf("no structured results to save")`
- `test/e2e/scenarios/results.go:743` — `if err := os.MkdirAll(outputDir, 0755); err != nil {`
- `test/e2e/scenarios/results.go:744` — `return "", fmt.Errorf("failed to create output directory: %w", err)`
- `test/e2e/scenarios/results.go:747` — `filename := fmt.Sprintf("%s-%s.json",`
- `test/e2e/scenarios/results.go:749` — `tr.Metadata.CompletedAt.Format("20060102-150405"))`
- `test/e2e/scenarios/results.go:752` — `data, err := json.MarshalIndent(tr, "", "  ")`
- `test/e2e/scenarios/results.go:754` — `return "", fmt.Errorf("failed to marshal results: %w", err)`
- `test/e2e/scenarios/results.go:757` — `if err := os.WriteFile(filepath, data, 0644); err != nil {`
- `test/e2e/scenarios/results.go:758` — `return "", fmt.Errorf("failed to write results: %w", err)`
- `test/e2e/scenarios/results.go:761` — `return filepath, nil`

## Problem shape

### cmd/e2e/main_test.go

- `cmd/e2e/main_test.go:39` — `func TestRunScenarioReportsAssertionsOnSuccessAndPartialFailure(t *testing.T) {`
- `cmd/e2e/main_test.go:47` — `{name: "success", result: &scenarios.Result{Success: true, AssertionsRun: 11}, wantOutput: "assertions_run=11"},`
- `cmd/e2e/main_test.go:48` — `{name: "partial failure", result: &scenarios.Result{AssertionsRun: 4}, err: errors.New("failed"),`
- `cmd/e2e/main_test.go:49` — `wantExit: 1, wantOutput: "assertions_run=4"},`
- `cmd/e2e/main_test.go:56` — `assert.Equal(t, tc.wantExit, exit)`

### cmd/e2e/dispatch_coverage_test.go

- `cmd/e2e/dispatch_coverage_test.go:47` — `// This only checks the advertised => dispatchable direction. It does not`
- `cmd/e2e/dispatch_coverage_test.go:48` — `// require the reverse: some dispatchable names (e.g. "ops", "crud-tools", and`
- `cmd/e2e/dispatch_coverage_test.go:49` — `// the bare "structural"/"statistical"/"semantic" tiered-variant aliases) are`
- `cmd/e2e/dispatch_coverage_test.go:50` — `// intentionally not advertised as standalone menu entries.`
- `cmd/e2e/dispatch_coverage_test.go:51` — `func TestAdvertisedScenariosAreDispatchable(t *testing.T) {`
- `cmd/e2e/dispatch_coverage_test.go:53` — `require.NotEmpty(t, dispatchable, "createScenario's dispatch switch must be found and non-empty")`
- `cmd/e2e/dispatch_coverage_test.go:56` — `require.GreaterOrEqualf(t, len(menuNames), 10,`
- `cmd/e2e/dispatch_coverage_test.go:60` — `require.Truef(t, dispatchable[name],`
- `cmd/e2e/dispatch_coverage_test.go:64` — `require.Truef(t, dispatchable[name],`
- `cmd/e2e/dispatch_coverage_test.go:75` — `// A unit test cannot prove a scenario's Docker topology actually comes up;`
- `cmd/e2e/dispatch_coverage_test.go:76` — `// only running the tier can. What it CAN prove — and what gh#1129's first and`
- `cmd/e2e/dispatch_coverage_test.go:77` — `// cheapest leg was — is that some runner NAMES the scenario. Every advertised`
- `cmd/e2e/dispatch_coverage_test.go:81` — `//  1. a `./e2e --scenario <name>` invocation in Taskfile.yml or`
- `cmd/e2e/dispatch_coverage_test.go:82` — `//     taskfiles/**/*.yml names it directly, or`
- `cmd/e2e/dispatch_coverage_test.go:83` — `//  2. its dispatch case returns a constructor that runAllScenarios also`
- `cmd/e2e/dispatch_coverage_test.go:84` — `//     builds, so the `--scenario all` that `task e2e:core` runs covers it —`
- `cmd/e2e/dispatch_coverage_test.go:85` — `//     core-health and core-dataflow are reachable only this way.`
- `cmd/e2e/dispatch_coverage_test.go:87` — `// Stated so nobody reads this guard as more than it is: it does NOT prove the`
- `cmd/e2e/dispatch_coverage_test.go:88` — `// named task target has a working compose topology, distinct ports, or`
- `cmd/e2e/dispatch_coverage_test.go:89` — `// resolvable hostnames. Those were gh#1129's other three legs, and they fail`
- `cmd/e2e/dispatch_coverage_test.go:90` — `// only when the tier runs, which is the only place they can be observed.`
- `cmd/e2e/dispatch_coverage_test.go:91` — `func TestAdvertisedScenariosHaveARunner(t *testing.T) {`
- `cmd/e2e/dispatch_coverage_test.go:97` — `require.Truef(t, namedByTask[name] || inAllBundle[name],`
- `cmd/e2e/dispatch_coverage_test.go:106` — `// TestOnlyExecutedTaskLinesCountAsRunners guards the executed-versus-documented`

### test/contract/e2e_tier_binary_contract_test.go

- `test/contract/e2e_tier_binary_contract_test.go:38` — `const tierTableHeader = "| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate |"`
- `test/contract/e2e_tier_binary_contract_test.go:78` — `func tierTable(t *testing.T) (string, []tierRow) {`
- `test/contract/e2e_tier_binary_contract_test.go:120` — `func parseTierRows(t *testing.T, path, body string) []tierRow {`
- `test/contract/e2e_tier_binary_contract_test.go:439` — `func assertNoComposeFileArmsAnUndeclaredHook(t *testing.T, rows []tierRow) {`
- `test/contract/e2e_tier_binary_contract_test.go:485` — `func TestComposeArmingValueMustBeALiteral(t *testing.T) {`
- `test/contract/e2e_tier_binary_contract_test.go:538` — `func TestE2ETierTableMatchesComposeAndDockerfile(t *testing.T) {`
- `test/contract/e2e_tier_binary_contract_test.go:552` — `t.Errorf("%s: no compose service %q in docker/compose/%s builds docker/Dockerfile", row, row.service, row.composeFile)`
- `test/contract/e2e_tier_binary_contract_test.go:557` — `if service.Build.Target != row.target {`
- `test/contract/e2e_tier_binary_contract_test.go:558` — `t.Errorf("%s: compose target = %q, spec table says %q", row, service.Build.Target, row.target)`
- `test/contract/e2e_tier_binary_contract_test.go:563` — `if build.pkg != row.binary {`
- `test/contract/e2e_tier_binary_contract_test.go:564` — `t.Errorf("%s: Dockerfile target %q builds %q, spec table says %q", row, row.target, build.pkg, row.binary)`
- `test/contract/e2e_tier_binary_contract_test.go:566` — `if len(build.tags) != 0 {`
- `test/contract/e2e_tier_binary_contract_test.go:567` — `t.Errorf("%s: Dockerfile target %q builds with tags %v; no build tag may gate a tier", row, row.target, build.tags)`
- `test/contract/e2e_tier_binary_contract_test.go:573` — `armed := service.Environment.values("SEMSTREAMS_E2E_")`
- `test/contract/e2e_tier_binary_contract_test.go:574` — `if got, want := strings.Join(sortedKeysOf(armed), ","), strings.Join(sorted(row.envGates), ","); got != want {`
- `test/contract/e2e_tier_binary_contract_test.go:575` — `t.Errorf("%s: compose sets [%s], spec table says [%s]", row, got, want)`
- `test/contract/e2e_tier_binary_contract_test.go:580` — `for _, gate := range row.envGates {`
- `test/contract/e2e_tier_binary_contract_test.go:581` — `if value, declared := armed[gate]; declared && value == "" {`
- `test/contract/e2e_tier_binary_contract_test.go:582` — `t.Errorf("%s: %s is declared with an empty effective value, which does not arm the hook", row, gate)`
- `test/contract/e2e_tier_binary_contract_test.go:589` — `if service.Image == "" {`
- `test/contract/e2e_tier_binary_contract_test.go:590` — `t.Errorf("%s: compose service has no per-target image tag", row)`
- `test/contract/e2e_tier_binary_contract_test.go:596` — `if previous != row.target {`
- `test/contract/e2e_tier_binary_contract_test.go:597` — `t.Errorf("image tag %q is shared by targets %q and %q", service.Image, previous, row.target)`
- `test/contract/e2e_tier_binary_contract_test.go:604` — `for key := range services {`
- `test/contract/e2e_tier_binary_contract_test.go:605` — `if !claimed[key] {`
- `test/contract/e2e_tier_binary_contract_test.go:606` — `t.Errorf("compose service %s builds docker/Dockerfile but no tier table row names it", key)`
- `test/contract/e2e_tier_binary_contract_test.go:610` — `assertNoComposeFileArmsAnUndeclaredHook(t, rows)`
- `test/contract/e2e_tier_binary_contract_test.go:611` — `assertTwoRunnableTargetsAndNoTags(t, dockerfile)`
- `test/contract/e2e_tier_binary_contract_test.go:619` — `func assertTwoRunnableTargetsAndNoTags(t *testing.T, dockerfile dockerfileTargets) {`
- `test/contract/e2e_tier_binary_contract_test.go:626` — `if got := strings.Join(sortedKeys(runnable), ","); got != "e2e,production" {`
- `test/contract/e2e_tier_binary_contract_test.go:627` — `t.Errorf("docker/Dockerfile runnable targets = [%s], want exactly [e2e,production]", got)`
- `test/contract/e2e_tier_binary_contract_test.go:633` — `if n := strings.Count(string(body), "-tags="); n != 0 {`
- `test/contract/e2e_tier_binary_contract_test.go:634` — `t.Errorf("docker/Dockerfile carries %d `-tags=` builds; no build tag may gate an E2E-only hook", n)`
- `test/contract/e2e_tier_binary_contract_test.go:638` — `// TestE2EBootVariableSetMatchesTierTable is invariant I6: the variables the`
- `test/contract/e2e_tier_binary_contract_test.go:639` — `// E2E binary's options constructor reads are exactly the union of the tier`
- `test/contract/e2e_tier_binary_contract_test.go:640` — `// table's Gate column. A variable the table declares but the binary does not`
- `test/contract/e2e_tier_binary_contract_test.go:641` — `// read would leave its tier silently unarmed; one the binary reads but no row`
- `test/contract/e2e_tier_binary_contract_test.go:642` — `// declares would be an option no tier can enable.`
- `test/contract/e2e_tier_binary_contract_test.go:649` — `func TestE2EBootVariableSetMatchesTierTable(t *testing.T) {`
- `test/contract/e2e_tier_binary_contract_test.go:658` — `read := e2eBootOptionNames(t)`
- `test/contract/e2e_tier_binary_contract_test.go:659` — `if got, want := strings.Join(sortedKeys(read), ","), strings.Join(sortedKeys(declared), ","); got != want {`
- `test/contract/e2e_tier_binary_contract_test.go:660` — `t.Errorf("e2eboot.FromEnv reads [%s]; the Gate column of %s declares [%s]", got, specPath, want)`

### scripts/e2e-reserve-ports.sh

- `scripts/e2e-reserve-ports.sh:77` — `echo "[ERROR] derived 0 compose files from taskfiles/e2e/ — refusing to claim a reservation" >&2`
- `scripts/e2e-reserve-ports.sh:173` — `echo "[RESERVE] derived $total_derived distinct published host ports across $resolved_files of $attempted_files e2e compose file(s); $total_at_risk inside the ephemeral range."`
- `scripts/e2e-reserve-ports.sh:219` — `echo "[RESERVE] The preflight in e2e-check-ports.sh is the only guard on this platform."`

### scripts/e2e-statistical-up_fixture_test.py

- `scripts/e2e-statistical-up_fixture_test.py:157` — `wrapper = ROOT / 'scripts/e2e-statistical-up.sh'`
- `scripts/e2e-statistical-up_fixture_test.py:161` — `task = (ROOT / 'taskfiles/e2e/statistical.yml').read_text()`
- `scripts/e2e-statistical-up_fixture_test.py:165` — `task = task.replace('cd cmd/e2e && ./e2e --scenario tiered --variant statistical --output-dir ./test/e2e/results',`
- `scripts/e2e-statistical-up_fixture_test.py:171` — `cmd = ['task', '--exit-code'] if task else ['bash', 'scripts/e2e-statistical-up.sh']`

## Tier, runner, and gate index

This index composes the pinned task and spec declarations. `PR/manual` below means the explicit E2E Ladder
workflow triggers. No E2E task invocation was found in `ci.yml` or `release.yml`; `container.yml` waits on CI
for its main workflow-run trigger and also runs directly on tags. Remote required-check rules are not read here.
Candidate-proof requirements are separate prose authority; the core addition specifically says beta.161.

| Task | Application binary / option(s) | Runner selection | In `e2e:all` | Workflow task invocation | Candidate-proof declaration |
|---|---|---|---|---|---|
| core | production, then e2e / EXAMPLES | minted-authority; all→health+dataflow; pre-identity seed/assert; graph-roundtrip | yes | sister-validation manual | distinct core row explicitly beta.161, spec:122 |
| structural | e2e / EXAMPLES | tiered --variant structural | yes | no named task; ladder comment says statistical subsumes structural stages | no separate tier named in spec:118–119 |
| statistical | e2e / EXAMPLES | tiered --variant statistical | yes | E2E Ladder PR/manual | spec:118 |
| semantic | e2e / EXAMPLES | tiered --variant semantic | yes, default | no workflow; ladder:25 says default runs in no workflow yet | spec:118; semantic active polling spec:124 |
| agentic | e2e / PROCESS_BARRIER,MILESTONE_PROBE | agentic | yes | no named workflow task | spec:119 |
| lessons | e2e / EXAMPLES | lessons | no | no named workflow task | no separate tier named in spec:118–119 |
| research-graph | e2e / EXAMPLES | research-graph then isolated research-graph-execute | no | no named workflow task | spec:119,155 |
| deep-research | production / none | deep-research | no | no named workflow task | spec:119 |
| crud-tools | production / none | crud-tools | no | no named workflow task | spec:119,156 |
| ops | e2e / LESSON_CURATION | ops | no | no named workflow task | spec:119 |
| lifecycle | e2e / MISSION,LIFECYCLE_SEED | lifecycle | no | no named workflow task | no separate tier named in spec:118–119 |
| throughput | e2e / EXAMPLES | throughput, plus quick/large/unique/query variants | no | no named workflow task | no separate tier named in spec:118–119 |
| slow-consumer | e2e / SLOW_CONSUMER | core-slow-consumer | no | E2E Ladder PR/manual | no separate tier named in spec:118–119 |
| openai-responses | no application container | go test -tags=live_llm -run TestOpenAIResponses | no | no named workflow task | taskfile:17 says hard gate for ADR-051 breaking bundle; tests skip absent API key |

Task runner selections above are individually pinned in Task invocation index. Binary/options rows are individually
pinned in payload-registry/spec.md:39–50 and compose lines. `semantic:8b` and `semantic:frontier` are overlays of
semantic, not additional base tasks; their heavy-local/cloud and paid-key declarations are pinned below.

## Evidence and result representation index

The code declarations below are distinct from the release-owner evidence requirements already pinned at
release-candidate-proof/spec.md:17–22,114–126,217–229. The #1390 archive records the run's code revision and
UTC interval, durations, aggregate successes, agentic assertion count, and its out-of-tree scratch log locator;
the raw scratch log is not an inspected artifact in this survey.

- `test/e2e/results/writer.go:19` — `type TestRun struct {`
- `test/e2e/results/writer.go:20` — `ID          string                `json:"id"``
- `test/e2e/results/writer.go:21` — `Timestamp   time.Time             `json:"timestamp"``
- `test/e2e/results/writer.go:22` — `Duration    time.Duration         `json:"duration_ns"``
- `test/e2e/results/writer.go:23` — `DurationStr string                `json:"duration"``
- `test/e2e/results/writer.go:24` — `Config      TestRunConfig         `json:"config"``
- `test/e2e/results/writer.go:25` — `Scenarios   []scenarios.Result    `json:"scenarios"``
- `test/e2e/results/writer.go:26` — `Metrics     *client.MetricsReport `json:"metrics,omitempty"``
- `test/e2e/results/writer.go:27` — `Summary     Summary               `json:"summary"``
- `test/e2e/results/writer.go:28` — `Environment map[string]string     `json:"environment,omitempty"``
- `test/e2e/results/writer.go:32` — `type TestRunConfig struct {`
- `test/e2e/results/writer.go:33` — `Variant    string   `json:"variant"` // "structural", "statistical", or "semantic"`
- `test/e2e/results/writer.go:34` — `MLEnabled  bool     `json:"ml_enabled"``
- `test/e2e/results/writer.go:35` — `Scenarios  []string `json:"scenarios"``
- `test/e2e/results/writer.go:36` — `BaseURL    string   `json:"base_url"``
- `test/e2e/results/writer.go:37` — `MetricsURL string   `json:"metrics_url"``
- `test/e2e/results/writer.go:41` — `type Summary struct {`
- `test/e2e/results/writer.go:42` — `TotalScenarios  int     `json:"total_scenarios"``
- `test/e2e/results/writer.go:43` — `PassedScenarios int     `json:"passed_scenarios"``
- `test/e2e/results/writer.go:44` — `FailedScenarios int     `json:"failed_scenarios"``
- `test/e2e/results/writer.go:45` — `SuccessRate     float64 `json:"success_rate"``
- `test/e2e/results/writer.go:46` — `TotalErrors     int     `json:"total_errors"``
- `test/e2e/scenarios/results.go:16` — `type TieredResults struct {`
- `test/e2e/scenarios/results.go:18` — `Variant VariantResults `json:"variant"``
- `test/e2e/scenarios/results.go:21` — `Entities EntityResults `json:"entities"``
- `test/e2e/scenarios/results.go:24` — `Indexes IndexResults `json:"indexes"``
- `test/e2e/scenarios/results.go:27` — `Search SearchResults `json:"search"``
- `test/e2e/scenarios/results.go:30` — `Rules RuleResults `json:"rules"``
- `test/e2e/scenarios/results.go:33` — `Communities *CommunityResults `json:"communities,omitempty"``
- `test/e2e/scenarios/results.go:36` — `Anomalies *AnomalyResults `json:"anomalies,omitempty"``
- `test/e2e/scenarios/results.go:48` — `GraphRAG *GraphRAGResults `json:"graphrag,omitempty"``
- `test/e2e/scenarios/results.go:51` — `Components ComponentResults `json:"components"``
- `test/e2e/scenarios/results.go:54` — `Outputs OutputResults `json:"outputs"``
- `test/e2e/scenarios/results.go:57` — `Embeddings *EmbeddingMetrics `json:"embeddings,omitempty"``
- `test/e2e/scenarios/results.go:60` — `Hierarchy *HierarchyResults `json:"hierarchy,omitempty"``
- `test/e2e/scenarios/results.go:63` — `Timing TimingResults `json:"timing"``
- `test/e2e/scenarios/results.go:66` — `Metadata TestMetadata `json:"metadata"``
- `cmd/e2e/main.go:35` — `version = "dev"`
- `cmd/e2e/main.go:36` — `commit  = "unknown"`
- `cmd/e2e/main.go:37` — `date    = "unknown"`
- `test/e2e/scenarios/results_common_types.go:40` — `type TestMetadata struct {`
- `test/e2e/scenarios/results_common_types.go:41` — `// Variant that was tested`
- `test/e2e/scenarios/results_common_types.go:42` — `Variant string `json:"variant"``
- `test/e2e/scenarios/results_common_types.go:44` — `// StartedAt is when the test started`
- `test/e2e/scenarios/results_common_types.go:45` — `StartedAt time.Time `json:"started_at"``
- `test/e2e/scenarios/results_common_types.go:47` — `// CompletedAt is when the test completed`
- `test/e2e/scenarios/results_common_types.go:48` — `CompletedAt time.Time `json:"completed_at"``
- `test/e2e/scenarios/results_common_types.go:50` — `// Success indicates if the test passed`
- `test/e2e/scenarios/results_common_types.go:51` — `Success bool `json:"success"``
- `test/e2e/scenarios/results_common_types.go:53` — `// ErrorCount is the number of errors`
- `test/e2e/scenarios/results_common_types.go:54` — `ErrorCount int `json:"error_count"``
- `test/e2e/scenarios/results_common_types.go:56` — `// WarningCount is the number of warnings`
- `test/e2e/scenarios/results_common_types.go:57` — `WarningCount int `json:"warning_count"``
- `test/e2e/scenarios/results_common_types.go:59` — `// Errors contains the actual error messages`
- `test/e2e/scenarios/results_common_types.go:60` — `Errors []string `json:"errors,omitempty"``
- `test/e2e/scenarios/results_common_types.go:62` — `// Warnings contains the actual warning messages`
- `test/e2e/scenarios/results_common_types.go:63` — `Warnings []string `json:"warnings,omitempty"``
- `test/e2e/scenarios/results_common_types.go:65` — `// Version information`
- `test/e2e/scenarios/results_common_types.go:66` — `Version string `json:"version,omitempty"``
- `test/e2e/scenarios/results_common_types.go:67` — `}`

## Task invocation index

The `--scenario all` runner and `task e2e:all` are separate declarations, pinned above. The latter lists only
core, the three inference variants, and agentic. The table below indexes literal task-to-runner invocations;
CI trigger declarations are the `.github/workflows` pins above. Source table rows separately index every
compose service, binary, and hook option.

- `taskfiles/e2e/agentic.yml:30` — `- cd cmd/e2e && ./e2e --scenario agentic --output-dir ./test/e2e/results --metrics-url http://localhost:39090`
- `taskfiles/e2e/agentic.yml:56` — `- cd cmd/e2e && ./e2e --scenario agentic --output-dir ./test/e2e/results --metrics-url http://localhost:39090`
- `taskfiles/e2e/core.yml:87` — `- cd cmd/e2e && ./e2e --scenario core-minted-authority`
- `taskfiles/e2e/core.yml:90` — `- cd cmd/e2e && ./e2e --scenario all`
- `taskfiles/e2e/core.yml:222` — `- cd cmd/e2e && ./e2e --scenario core-pre-identity-seed`
- `taskfiles/e2e/core.yml:252` — `(cd cmd/e2e && ./e2e --scenario core-pre-identity-assert) \`
- `taskfiles/e2e/core.yml:264` — `- cd cmd/e2e && ./e2e --scenario core-graph-roundtrip`
- `taskfiles/e2e/crud-tools.yml:31` — `(cd cmd/e2e && ./e2e --scenario crud-tools --output-dir ./test/e2e/results --base-url http://localhost:65080 --metrics-url http://localhost:65190) || rc=$?`
- `taskfiles/e2e/crud-tools.yml:58` — `- cd cmd/e2e && ./e2e --scenario crud-tools --output-dir ./test/e2e/results --base-url http://localhost:65080 --metrics-url http://localhost:65190`
- `taskfiles/e2e/deep-research.yml:31` — `(cd cmd/e2e && ./e2e --scenario deep-research --output-dir ./test/e2e/results --base-url http://localhost:58080 --metrics-url http://localhost:59090) || rc=$?`
- `taskfiles/e2e/deep-research.yml:58` — `- cd cmd/e2e && ./e2e --scenario deep-research --output-dir ./test/e2e/results --base-url http://localhost:58080 --metrics-url http://localhost:59090`
- `taskfiles/e2e/lessons.yml:20` — `- cd cmd/e2e && ./e2e --scenario lessons`
- `taskfiles/e2e/lifecycle.yml:29` — `(cd cmd/e2e && ./e2e --scenario lifecycle) || rc=$?`
- `taskfiles/e2e/openai-responses.yml:32` — `- go test -tags=live_llm -timeout 5m -count=1 -v -run "TestOpenAIResponses" ./processor/agentic-model/...`
- `taskfiles/e2e/openai-responses.yml:40` — `- CAPTURE_FIXTURES=1 go test -tags=live_llm -timeout 5m -count=1 -v -run "TestOpenAIResponses_CaptureFixtures" ./processor/agentic-model/...`
- `taskfiles/e2e/ops.yml:31` — `(cd cmd/e2e && ./e2e --scenario ops --output-dir ./test/e2e/results --base-url http://localhost:61080 --metrics-url http://localhost:61190) || rc=$?`
- `taskfiles/e2e/ops.yml:58` — `- cd cmd/e2e && ./e2e --scenario ops --output-dir ./test/e2e/results --base-url http://localhost:61080 --metrics-url http://localhost:61190`
- `taskfiles/e2e/research-graph.yml:25` — `- cd cmd/e2e && ./e2e --scenario research-graph --base-url http://localhost:48080 --output-dir ./test/e2e/results --metrics-url http://localhost:49090`
- `taskfiles/e2e/research-graph.yml:31` — `- cd cmd/e2e && ./e2e --scenario research-graph-execute --base-url http://localhost:48080 --output-dir ./test/e2e/results --metrics-url http://localhost:49090`
- `taskfiles/e2e/research-graph.yml:57` — `- cd cmd/e2e && ./e2e --scenario research-graph --base-url http://localhost:48080 --output-dir ./test/e2e/results --metrics-url http://localhost:49090`
- `taskfiles/e2e/semantic.yml:21` — `- cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:45` — `- cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=300s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=10m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:63` — `- cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=180s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=5m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:103` — `- cd cmd/e2e && ./e2e --scenario tiered --variant semantic-fallback --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:128` — `- cd cmd/e2e && ./e2e --scenario tiered --variant statistical --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:135` — `- cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/slow-consumer.yml:13` — `cd cmd/e2e && ./e2e --scenario core-slow-consumer`
- `taskfiles/e2e/statistical.yml:20` — `- cd cmd/e2e && ./e2e --scenario tiered --variant statistical --output-dir ./test/e2e/results`
- `taskfiles/e2e/structural.yml:20` — `- cd cmd/e2e && ./e2e --scenario tiered --variant structural --output-dir ./test/e2e/results`
- `taskfiles/e2e/throughput.yml:31` — `(cd cmd/e2e && ./e2e --scenario throughput --output-dir ./test/e2e/results) || rc=$?`
- `taskfiles/e2e/throughput.yml:60` — `(cd cmd/e2e && ./e2e --scenario throughput --message-count 1000 --graphql-url "") || rc=$?`
- `taskfiles/e2e/throughput.yml:87` — `(cd cmd/e2e && ./e2e --scenario throughput --message-count 50000 --output-dir ./test/e2e/results) || rc=$?`
- `taskfiles/e2e/throughput.yml:116` — `(cd cmd/e2e && ./e2e --scenario throughput --output-dir ./test/e2e/results --profile-all) || rc=$?`
- `taskfiles/e2e/throughput.yml:146` — `(cd cmd/e2e && ./e2e --scenario throughput --unique-entities 14000 --query-during-ingestion --max-query-p99-ms 2000 --max-query-error-rate 0.05 --output-dir ./test/e2e/results) || rc=$?`
- `taskfiles/e2e/throughput.yml:173` — `- 'echo "  cd cmd/e2e && ./e2e --scenario throughput --message-count 10000"'`

## Paid and skipped declarations

- `taskfiles/e2e/openai-responses.yml:7` — `# capture/echo end-to-end. Requires OPENAI_API_KEY to be set; tests`
- `taskfiles/e2e/openai-responses.yml:8` — `# skip cleanly when absent so this target is safe to run in any`
- `taskfiles/e2e/openai-responses.yml:17` — `# Pre-tag use: HARD gate per ADR-051 phasing — must pass before`
- `taskfiles/e2e/openai-responses.yml:18` — `# tagging the BREAKING bundle so the doc-derived skeleton has`
- `taskfiles/e2e/openai-responses.yml:21` — `# Cost: each run hits the real OpenAI API. Two paid round-trips per`
- `taskfiles/e2e/openai-responses.yml:22` — `# TestOpenAIResponses_ToolFlow_WithReasoningEcho execution + one per`
- `taskfiles/e2e/openai-responses.yml:23` — `# TestOpenAIResponses_SingleTurn. Use sparingly.`
- `taskfiles/e2e/openai-responses.yml:27` — `desc: "OpenAI Responses live e2e — wire-level live test (~30s, uses paid API)"`
- `taskfiles/e2e/openai-responses.yml:32` — `- go test -tags=live_llm -timeout 5m -count=1 -v -run "TestOpenAIResponses" ./processor/agentic-model/...`
- `taskfiles/e2e/openai-responses.yml:40` — `- CAPTURE_FIXTURES=1 go test -tags=live_llm -timeout 5m -count=1 -v -run "TestOpenAIResponses_CaptureFixtures" ./processor/agentic-model/...`
- `processor/agentic-model/openai_responses_live_test.go:52` — `key := os.Getenv("OPENAI_API_KEY")`
- `processor/agentic-model/openai_responses_live_test.go:54` — `t.Skip("OPENAI_API_KEY not set; skipping OpenAI Responses live_llm test")`
- `processor/agentic-model/openai_responses_live_test.go:86` — `func TestOpenAIResponses_SingleTurn(t *testing.T) {`
- `processor/agentic-model/openai_responses_live_test.go:119` — `func TestOpenAIResponses_ToolFlow_WithReasoningEcho(t *testing.T) {`
- `processor/agentic-model/openai_responses_live_test.go:219` — `func TestOpenAIResponses_ReasoningEcho(t *testing.T) {`
- `processor/agentic-model/openai_responses_live_test.go:360` — `func TestOpenAIResponses_Streaming(t *testing.T) {`
- `processor/agentic-model/openai_responses_live_test.go:475` — `func TestOpenAIResponses_CaptureFixtures(t *testing.T) {`
- `processor/agentic-model/openai_responses_live_test.go:477` — `t.Skip("CAPTURE_FIXTURES != 1; skipping fixture capture")`
- `taskfiles/e2e/semantic.yml:24` — `desc: "HEAVY LOCAL: semantic tier with qwen3-8b answer+summary (valid GraphRAG/B0 baseline, NOT CI). ~10min, needs ~20GiB Docker RAM."`
- `taskfiles/e2e/semantic.yml:32` — `- echo "[SEMANTIC-8B] This is the on-demand HEAVY measurement — two 8B instances load ~5GiB weights each; --wait blocks several minutes. The default 'task e2e:semantic' stays on qwen3-1.7b as the fast CI gate."`
- `taskfiles/e2e/semantic.yml:45` — `- cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=300s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=10m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:48` — `desc: "DIAGNOSTIC/CLOUD (not offline, not CI): semantic tier with Gemini 2.5 Flash for answer+summary — measures the synthesis-quality CEILING over the same retrieval/partition. Needs GEMINI_API_KEY (source ../semdev/.env). ~5min."`
- `taskfiles/e2e/semantic.yml:56` — `- 'test -n "$GEMINI_API_KEY" || { echo "[ERROR] GEMINI_API_KEY is unset. Source it first, e.g.: set -a && source ../semdev/.env && set +a"; exit 1; }'`
- `taskfiles/e2e/semantic.yml:63` — `- cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=180s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=5m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`

## Searches

Counts below are matching lines unless otherwise stated. Commands ran against tracked content at the base.
Initial command used the full SHA; later commands used its unambiguous prefix `fe9482b7`.
Counts were re-collected with the same patterns and paths when writing this inventory; no test was run.

- `git rev-parse fe9482b7f336e575317cfb45fd1ad7c40baf7904` → one SHA, equal to base.
- `gopls workspace_symbol -matcher=fuzzy Scenario` → 0.
- `gopls references cmd/e2e/main.go:510:6` → 4 references: main.go:334,627,667,708.
- `gopls implementation test/e2e/scenarios/scenario.go:14:6` → error: no identifier found; corrected position below.
- `gopls implementation test/e2e/scenarios/scenario.go:10:6` → 0; constructor dispatch was enumerated by tracked search.
- `gopls call_hierarchy cmd/e2e/main.go:510:6` → 4 caller records, 5 callee records, 1 identifier record.
- `gopls references test/e2e/results/writer.go:105:18` → 1 reference, writer.go:133.
- `git grep -n -E 'e2e:all|e2e-agentic|e2e-structural|e2e-semantic|e2e-statistical|tier-binary|1301|1390' fe9482b7 -- .github/workflows Taskfile.yml taskfiles cmd/e2e openspec docs` → 179. Initial output limited to first 180 lines and truncated; count is full matching-line count.
- `git grep -n -E 'workflow_dispatch:|pull_request:|push:|tags:|branches:|schedule:|e2e|continue-on-error|needs:|if:|always\\(|failure\\(' fe9482b7 -- .github/workflows/ci.yml .github/workflows/e2e-ladder.yml .github/workflows/release.yml .github/workflows/container.yml` → 43.
- `git grep -n -E 'e2e:all|task: e2e:|e2e:' fe9482b7 -- Taskfile.yml` → 26.
- `git grep -n -E 'go run|go build|./e2e|scenario|docker compose|task:|desc:|API_KEY|profile|E2E_' fe9482b7 -- taskfiles/e2e` → 296. Displayed output partly truncated; task runners are pinned below.
- `git grep -n -E 'func |flag\\.|case |scenarioRegistry|Register|scenarios\\.|os.Exit|exitCode|Write|Success|Error|Failed|Assertions' fe9482b7 -- cmd/e2e/main.go test/e2e/results/writer.go test/e2e/scenarios/scenario.go test/e2e/scenarios/results.go` → 259. Displayed output partly truncated; runner and writer ranges subsequently read.
- `git grep -n -E 'tier.*binary|Tier.*binary|Tier.*Binary|e2e:all|e2e:slow-consumer|e2e:throughput|openai-responses|semstreams.*Options|With[A-Z]|E2E_' fe9482b7 -- openspec/specs/payload-registry/spec.md docs/contributing/02-e2e-tests.md test/contract/e2e_composition_contract_test.go docker/compose cmd/e2e-semstreams internal/boot` → 82. No tracked e2e_composition_contract_test.go; located e2e_tier_binary_contract_test.go instead.
- `git grep -n -E 'func Test|func |e2e:|assertions_run|AssertionsRun|Success|exit|WriteRun|WriteLatest|CreateTestRun' fe9482b7 -- cmd/e2e/main_test.go cmd/e2e/dispatch_coverage_test.go test/contract/e2e_tier_binary_contract_test.go test/contract/e2e_taskfile_contract_test.go` → 59.
- `git grep -n -E 'WriteLatest\\(|CreateTestRun\\(|WriteRun\\(|SaveStructuredResults\\(|result.Success|AssertionsRun.*[<=>]|AssertionsRun ==|AssertionsRun <' fe9482b7 -- cmd/e2e test/e2e/results test/contract` → 7.
- `git grep -n -E 'func Test|E2E_EXIT_PROBE|exit \\$rc|exit.*rc|continue-on-error|ignore_error|SKIP|skip|API_KEY|task e2e:|gate|Breaking|BREAKING' fe9482b7 -- taskfiles/e2e .github/workflows docs/contributing/02-e2e-tests.md openspec/specs/release-candidate-proof/spec.md test/contract` → 227.
- `git grep -n -E 'target:|SEMSTREAMS_E2E_|go build|FROM.*AS|BuildOptions|OptionsFromEnv|func Options|boot.Run' fe9482b7 -- docker/Dockerfile docker/compose internal/e2eboot cmd/semstreams/main.go cmd/e2e-semstreams/main.go` → 61.
- `git grep -n -E '(e2e|E2E|assertions_run|AssertionsRun|WriteRun|WriteLatest)' fe9482b7 -- scripts test/testinfra .github/workflows/release.yml .github/workflows/ci.yml` → 46.
- `git grep -n -E 'OPENAI_API_KEY|func TestOpenAIResponses|t.Skip' fe9482b7 -- processor/agentic-model/*live*` → 13.
- `git grep -n -E 'e2e|E2E' fe9482b7 -- .github/workflows/ci.yml .github/workflows/release.yml` → 0.
- `git grep -n -E 'AssertionsRun[[:space:]]*(<|>|==|!=)' fe9482b7 -- cmd/e2e/main.go` → 0.
- `git grep -n -E 'schedule:' fe9482b7 -- .github/workflows/e2e-ladder.yml` → 0.
- `git grep -n -E 'WriteLatest\\(|CreateTestRun\\(' fe9482b7 -- cmd/e2e` → 0.
- `git grep -n -E 'e2e-all-05182c2e.log' fe9482b7 -- .` → 1.
- `git ls-tree -r --name-only fe9482b7 .github/workflows taskfiles/e2e cmd/e2e test/e2e` → 165 files; initial display first 140.
- Literal runner scan over snapshot `taskfiles/e2e/*.yml`, lines containing executed `./e2e --scenario` or `go test -tags=live_llm` → 35 pins in Task invocation index.

### Located-range reads

Each listed read used `nl -ba <file> | sed -n <ranges>` on the source snapshot after tracked search.
- `Taskfile.yml` → 150–240 (file ends at 214).
- `docs/contributing/02-e2e-tests.md` → 175–240, 293–353.
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md` → 160–250 (file ends at 192).
- `.github/workflows/e2e-ladder.yml` → 1–130 (file ends at 122).
- `test/e2e/scenarios/scenario.go` → 1–100 (file ends at 54).
- `cmd/e2e/main.go` → 25–115,303–342,428–485,510–568,601–647.
- `taskfiles/e2e/openai-responses.yml` → 1–50 (file ends at 41).
- `openspec/specs/release-candidate-proof/spec.md` → 1–30,110–163,215–244.
- `.github/workflows/release.yml` → 1–45; `.github/workflows/container.yml` → 1–35.
- `.github/workflows/sister-validation.yml` → 17–45,142–169; `.github/workflows/semspec-validation.yml` → 1–28.
- `taskfiles/e2e/semantic.yml` → 1–66; `taskfiles/e2e/research-graph.yml` → 13–38; `taskfiles/e2e/throughput.yml` → 1–42.
- `test/contract/e2e_tier_binary_contract_test.go` → 538–615,619–662; `cmd/e2e/dispatch_coverage_test.go` → 45–120.
- `internal/e2eboot/fromenv.go` → 28–70; `docker/Dockerfile` → 40–62,111–140.
- `test/e2e/results/writer.go` → 104–144,184–227; `test/e2e/scenarios/results.go` → 738–762.
- Final inventory assembly read the individual pinned source lines above; the claim/artifact/task indices were not executed.


- `nl -ba test/e2e/results/writer.go | sed -n '10,46p'` → 37 lines.
- `nl -ba test/e2e/scenarios/results.go | sed -n '12,67p'` → 56 lines.
- `git grep -n -E 'GitCommit|CommitSHA|SHA256|sha256|commit_sha|git_commit|runner_identity|runner|Skipped|skipped|AssertionsRun' fe9482b7 -- test/e2e/results test/e2e/scenarios/results.go test/e2e/scenarios/results_common_types.go cmd/e2e/main.go openspec/changes/archive/2026-09-27-one-composition-root/reconciliation.md` → 3 matching lines, all assertion-count reporting in cmd/e2e/main.go.
- Literal `type TestMetadata struct` scan of `results.go`, `results_common_types.go`, `results_tier_types.go` → 1 declaration: test/e2e/scenarios/results_common_types.go:40.

### NOT RUN / omissions

- NOT RUN: `gh issue list --search ...`, `gh pr list`, and live PR/main/tag run retrieval. Parent owns these external records.
- NOT RUN: `openspec list`; this snapshot is read-only source. Named archive/spec paths are inventoried directly.
- NOT RUN: raw `e2e-all-05182c2e.log` inspection. Archive tasks:183 locates it outside the tree in a coordinating-session scratchpad.
- NOT RUN: unpublished branch-protection/ruleset required-check configuration. Workflow declarations do not enumerate remote rulesets.
- NOT RUN: per-scenario business assertion quality, mutation proof, new live E2E execution, container topology startup, paid providers.
- NOT RUN: independent re-derivation of historical candidate proof assets and release-specific owner dispositions.
- NOT RUN: full scans of sister repositories; workflow references are adjacent claims only.
- `test/contract/e2e_composition_contract_test.go` and `test/contract/e2e_taskfile_contract_test.go` were named in searches but absent; the located tier/binary guard is `test/contract/e2e_tier_binary_contract_test.go`.

## Inventory counts

Category pin counts: {"Adjacent claims": 99, "Claimed gap": 19, "Consumers": 158, "Problem shape": 78, "Spellings of the fact": 98}.
Task invocation pins: 35. Paid/skip pins: 24. Search records: 28 (including gopls, revision, file list, literal runner scan; range reads listed separately).
