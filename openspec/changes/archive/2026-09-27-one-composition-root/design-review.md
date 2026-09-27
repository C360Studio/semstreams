# Pre-owner design review: `one-composition-root` (#1301), one round

Independent `semstreams-reviewer` pass (Opus, read-only), 2026-09-26, commissioned by the coordinating session.
Reviewed at `dfaeb69c` (base `df3effb2`, inventory `86803eb9`). Filed verbatim; the amendment commit that follows
applies every finding (`design.md` amendment ledger).

---

**Mode:** pre-owner design review. **Reviewed:** worktree `/Users/coby/Code/c360/semstreams-wt/claude/gh1301-one-composition-root` at `dfaeb69c` (base `df3effb2`, inventory `86803eb9`). `task inventory:verify -- docs/proposals/gh1301-composition-inventory.md` gives `pins=257 ok=257 moved=0 drift=0`, and `openspec validate one-composition-root --strict` reports the change as valid. I wrote no files and ran no git command that changes the working tree.

**Verdict: DESIGN CHANGES REQUESTED.** There is one BLOCKING finding and two HIGH ones. Each fix is small and none changes the shape: D stands, the nine-slice seam stands, and D2 and D6 follow the ruling. If the author applies these amendments exactly, the diff can be checked mechanically and does not need a second design round.

## BLOCKING

**BLOCKING `design.md:129,180` (D8) — "production copy wins" makes a live spec scenario false, and the change carries no delta for it**
- **Mechanism:** `openspec/specs/application-logging/spec.md:23-28` says: *"GIVEN the E2E Phase-A composition … AND no counter handler or NATS log handler receives the record."* The e2e root builds its logging with `bootstrapobservability.NewE2EPhaseA` (`cmd/e2e-semstreams/main.go:159`; `internal/bootstrapobservability/bootstrap.go:57-59`, stdout only, nil counter) and `phaseLogging.Steady(nil)` (`:374`). Production uses `NewProductionPhaseA` with a `CounterHandler` (`bootstrap.go:36-50`) and `Steady(forwardingHandler)` (`cmd/semstreams/main.go:221-227`). Under D8 the e2e binary gets both.
- **Tier behaviour change:** log forwarding switches on for every e2e tier whose config enables `log-forwarder`. `jq '.services["log-forwarder"].enabled'` returns `true` for `statistical.json`, `e2e-structural.json`, `semantic.json`, `lifecycle-flow.json`, `research-graph-e2e.json` and `protocol-flow.json`.
- **What the design says instead:** § 4 D8 counts four divergences (guard, bounded abort, `postStart`, "log text"). § 2.3 and tasks 1.3 treat `cmd/e2e-semstreams/bootstrap_observability_test.go` as a possible "phase-A label". That test (`:15,:32`) actually asserts the E2E Phase-A graph the live spec pins, so deleting it deletes the evidence for a live scenario.
- **Fix:**
  - Add `specs/application-logging/spec.md` as a MODIFIED delta. Restate "Client observability dependencies exist before connection" with the E2E scenario removed or rewritten to say the E2E binary composes the production Phase-A.
  - Name this divergence in D8, along with the six tiers that start forwarding.
  - Add these to § 7 as dead or changing: `NewE2EPhaseA` plus `internal/bootstrapobservability/bootstrap_test.go:102`, and `internal/maxdelivery/boot_order_test.go:111-158`, which asserts the e2e `run()` calls `NewE2EPhaseA` and no counter.
  - This reverses #961's split (`docs/proposals/gh955-bootstrap-logger-design.md:16,110` recorded it as preserved behaviour, not a ruling), so the design can decide it without going to the owner.

## HIGH

**HIGH `specs/framework-composition/spec.md:43` and `specs/payload-registry/spec.md:99` — the closure test's "converse" cannot pass as written**
- **Mechanism:** both scenarios require the E2E binary's closure to hold "every such package that exists" under the forbidden prefixes. `go list ./examples/...` lists `examples/processors/weather_station`. Nothing links it, because `EXAMPLES` registers only `iot_sensor` and `document`.
- **Verification:** `go list -deps` diffs at HEAD:
  - Only the e2e binary links `cmd/e2e-semstreams{,/fixtures,/mission}`, `examples/processors/{document,iot_sensor}` and `test/e2e/harness/lessoncuration`.
  - Only `-tags=e2e_process_barrier` adds `harness/{milestoneprobe,processbarrier}`.
  - Only `-tags=e2e_slow_consumer` adds `internal/e2eslowconsumer`.
- **Forbidden set (item 7):** otherwise complete, and `internal/e2eslowconsumer` is acceptably placed.
- **Fix:** scope the converse to where hooks live: every package under `test/e2e/harness`, plus `internal/e2eslowconsumer`, `internal/e2eboot`, `cmd/e2e-semstreams/fixtures` and `cmd/e2e-semstreams/mission`. Keep `examples/processors` in the production-forbidden half only.

**HIGH `design.md:58-60,77-96` — the two mains' command-line parsing and build metadata are not designed; the slow-consumer tier depends on the result**
- **Mechanism:** the design says both mains "parse flags" but does not say which parser the e2e main uses. The defaults differ:
  - Production (`cmd/semstreams/flags.go`): `SEMSTREAMS_LOG_FORMAT` defaults to `json` (`:42`), `debug-port` to 8083 (`:49`), config to `configs/example.json`, and `--health-port` exists.
  - E2E: log format defaults to `text` (`cmd/e2e-semstreams/main.go:517`), `DebugPort` is hardcoded to 6060 (`:519`), config defaults to `config.json`, and `--lifecycle-seed` exists.
- **Why it matters:** the slow-consumer tier moves onto the e2e target. Its scenario keeps only log lines that start with `{` and parse as JSON (`test/e2e/scenarios/core_slow_consumer.go:141-157`), and it reads `semstreams_log_entries_total` (`:165`). `e2e-slow-consumer.yml:27-31` does not set `SEMSTREAMS_LOG_FORMAT`. If the e2e main keeps its own parser, the tier fails.
- **I1 cannot catch this:** the test compares `FromEnv(cli)` with `Production(cli)` over the same `cli`, so it never sees parser defaults.
- **Missing `Options` fields:** § 2.1 has no `DebugPort`, even though `Run` calls `MaybeStartPProf`. It also has no `Version`, `GitCommit`, `BuildTime` or service name, which `Run` logs.
  - `-X main.Version/GitCommit/BuildTime` (`docker/Dockerfile:48-58`, `.github/workflows/release.yml:58`) is silently ignored if those variables leave `package main`.
  - `test/release/release_smoke_test.go:21-35` checks only `--version`, so moving them would quietly blank the build metadata in boot logs.
- **Fix:** add one sentence to D8 saying both mains use production's `parseFlags`, moved into `internal/boot` (and deleting e2e's `parseCLI`), which D6 already implies. Add `DebugPort` and a build-info scalar that each main fills from its own ldflags variables.

## MEDIUM

**MEDIUM `design.md:46-50` (§ 1.4) and D4 / I3 — the misspelled-name premise is false; D4 is edge-case code that repeats an existing guard**
- **Mechanism:** the B test already rejects `SEMSTREAMS_E2E_MILESTONE_PORBE=1` in a compose file. `TestE2ETierTableMatchesComposeAndDockerfile` compares the names a service sets with the row's gates exactly (`test/contract/e2e_tier_binary_contract_test.go:562-564`) and runs `assertNoComposeFileArmsAnUndeclaredHook` (`:602`). I6 then makes the tier table and `FromEnv` agree on names.
- **What D4 still guards:** only a binary launched by hand. There it can break things: the runner-side `SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT` and `SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT` share the prefix (`taskfiles/e2e/semantic.yml:45`), so a developer who exports them and runs `bin/e2e-semstreams` (`taskfiles/build.yml:19`) would have boot refused.
- **Same pattern:** "`LIFECYCLE_SEED` without `MISSION` errors" repeats a loud failure. `seedMission` → `Manager.Create` → `lookupByWorkflow` already errors (`pkg/lifecycle/manager.go:317-319`), and the B test pins that the lifecycle row sets both variables.
- **Fix (owner's simplicity rule):** drop D4, I3, the cross-variable refusal and the matching two ANDs in framework-composition scenario 2. Correct § 1.4. Record one doc sentence saying `FromEnv` ignores names it does not know and compose names are guarded by the B test plus I6.

**MEDIUM `specs/payload-registry/spec.md:87-89` — a clause kept byte-identical now gives a false reason, and `FromEnv`'s value rules are unspecified**
- **Mechanism:** the clause reads "since a hook reading its gate with `os.Getenv` treats `NAME=` exactly as unset". After D5 no hook reads `os.Getenv`. `FromEnv` takes `os.LookupEnv` (`design.md:59`), which reports `NAME=` as present.
- **Gap:** the design never says what `FromEnv` does with `NAME=` or `NAME=0`.
- **Fix:** state the rule once, "nonempty value enables", which matches the B test's literal check, and reword the clause's reason to cite `FromEnv`. This is a third named edit to the MODIFIED block. Everything else in the block is byte-identical: `diff` of live `:10-103` against delta `:5-102` shows only the rule paragraph, the reads-from sentence, nine table rows, the THEN clause plus one AND in "every tier's target…", and the body of "cannot reach the production build". All six scenario headings are unchanged (`diff` exit 0), so no `// spec:` citation breaks.

**MEDIUM `specs/payload-registry/spec.md:24-25` and framework-composition scenario 1's title — "with nothing set, the e2e binary is the production composition" claims more than I1 measures**
- **Mechanism:** the e2e binary always links package `init()` calls that write to the global vocabulary: `examples/processors/iot_sensor/vocabulary.go:5`, `examples/processors/document/vocabulary.go:5` and `cmd/e2e-semstreams/mission/state.go:56`. So even with no option set, its vocabulary is a superset of production's, and the agentic and slow-consumer tiers lose that fidelity compared with today's tagged production root. I1 compares `Options` values only.
- **Fix (smallest):** reword both sentences to "the E2E binary's boot options equal the production options", and record the `init()` vocabulary difference in § 11 as a residual.

**MEDIUM `design.md:224-238` (§ 7) and `tasks.md:69` — the blast radius leaves out readers that load a root's source by path**
- `internal/maxdelivery/boot_order_test.go:24,111` parses both `main.go` files (see the BLOCKING finding).
- `test/e2e/scenarios/ops/composition_root_contract_test.go:26` reads `cmd/e2e-semstreams/main.go` for the `persona.LoadFromDirectory` string.
- `.agents/contracts/semstreams-{developer,reviewer}.md:246` tell agents to register in both binaries.
- The sweep regex in tasks 6.1 catches none of these, and inventory § 6 ("Readers of the roots") misses the two tests.
- **Fix:** add these rows to § 7, and widen 6.1 with `cmd/(e2e-)?semstreams/main\.go` over `*_test.go` and `.agents/contracts`.

## NIT

- **`design.md:179` (D7):** the conclusion holds: `service/service_manager.go:1291-1292` returns nil on port 0, the e2e root never calls `StartHealthListener`, and every app healthcheck hits `:8080/readyz`. But the cited evidence is wrong. `configs/semantic.json:17-35` 8081/8083 are the semembed/seminstruct sidecar URLs. The real evidence is the nine app healthchecks, all `http://localhost:8080/readyz`. Production's `--health-port` already defaults to 0 (`flags.go:63-64`).
- **`design.md:193` (P3):** `git grep "type Capability"` returns `model/registry.go:357:type CapabilityConfig`. The substance holds under the inventory's anchored grep (0 hits); quote that command instead.
- **`design.md:232` (§ 7 row 7):** say what `agentic.yml:64` and `e2e-slow-consumer.yml:18` image tags become once both use `target: e2e`. It is legal under the B test, but `…:e2e-process-barrier` would then name a target that no longer exists.
- **Duplicate check:** `test/contract/core_composition_deps_test.go:30-34` (`TestProductionBinaryExcludesExamplePackages`) already covers the `/examples/` half of I4. Either fold it into the new closure test or cite it.

---

## The nine questions

1. **Premises P1–P12:** all hold. P1 matches inventory § 1c. P2 is at `service_manager.go:1291`. P4 is at `tier1-packages.txt:40,43,59,61,69,79`, and no `internal/` line exists. P5 is at contract test `:562-573`. P6 is at `main.go:515-521,643`; the only other env read is `getEnvOrDefault` for config and log settings. P7 is semdev `ca3956a` `internal/boot/runtime.go:106-110`. P8 holds: CI reaches the targets only through `task` → compose (`e2e-ladder.yml:44-70`), and `container.yml:73` builds `production` only. P9: the only e2e tag is `Taskfile.yml:163`. P10 is at `:43`. P11 is at `:199`. P12: 4 live docs, plus the history-only `migration-beta18.md`. The one false premise is § 1.4, which is not numbered (MEDIUM above). P3 is a NIT.
2. **MODIFIED block:** only the regions the design names differ from the live requirement. The one addition needed is the stale-reason clause (MEDIUM).
3. **Ruling fidelity:**
   - D2 matches the #1301 scope-bound transcription, and the #1249 docket's own table lists examples, mission and lesson curation under "hooks carried".
   - D6 follows mechanically from "`--lifecycle-seed` … enables from env".
   - Neither turns a ruling's reason into a requirement.
   - Nothing deviates from the scope bound. OQ1 reads close to re-opening "unexported, so no Tier 1 surface" (see question 9).
4. **Simplicity:** there is no strictly simpler seam. Each of the nine slices is a distinct boot phase used by at least one of the ruled options. The real cuts are D4/I3 and the LIFECYCLE_SEED cross-check. They cost nothing because the B test plus I6 already make a bad compose name fail statically, and a seed without `MISSION` already fails at boot.
5. **Compose blast radius:**
   - The row→variable mapping gives every `e2e` service everything its config declares:
     - The structural, statistical, throughput and semantic configs (including the `8b` and `frontier` overlays, which merge the `semstreams-ml` environment) declare `iot_sensor`, `document_processor`, `iot.sensor.v1` and `content.document.v1`, which `EXAMPLES` covers.
     - `lifecycle-flow.json` declares `mission-command` and `mission.command.v1`, which `MISSION` covers.
     - The ops config needs no examples, and its responder is the only `SubjectPromote` consumer (`ops/scenario.go:764`).
     - research-graph and core-2 need only fixture keys (`fixtures/register.go:57-62`).
   - No tier fails at boot for a missing variable.
   - The one tier-breaking risk is the slow-consumer log format (HIGH).
6. **D8:** mostly not load-bearing. Log-text greps target production containers only (`taskfiles/e2e/core.yml:67-249` → `semstreams-e2e-app`), and ops matches substrings regardless of format. The exceptions are the Phase-A logger (BLOCKING) and the log format and counter the slow-consumer scenario depends on (HIGH). Production winning actually supplies the counter the slow-consumer scenario needs.
7. **Closure forbidden set:** complete for today's closures. Only the converse is wrong (HIGH).
8. **D7:** confirmed; the evidence citation is a NIT.
9. **Docket:**
   - OQ1 is settled by the scope bound; record "the sister-migration goal stays open" as a residual, not a question.
   - OQ3 is a residual too.
   - OQ2 (a) is the default under the owner's simplicity rule and changes nothing, so the design can decide it itself.
   - OQ4 is the only real owner question.
   - Nothing that should have gone to the owner is missing: D8's reversal of the E2E Phase-A split and the command-line convergence are decisions the design should state itself.

---

**Coordinator's note on the D7 NIT (2026-09-26):** the reviewer's "all `:8080/readyz`" is itself imprecise — the
app healthchecks in `docker/compose/*.yml` curl `/health` on 8080 (×5), 8083 (×6) and 8081 (×2) and `/readyz` on 8080
(×1). The conclusion is unchanged: none of them is served by the dedicated `StartHealthListener`, whose flag defaults
to 0; the design's D7 now cites the healthchecks and `flags.go:63-64` rather than `configs/semantic.json`.
