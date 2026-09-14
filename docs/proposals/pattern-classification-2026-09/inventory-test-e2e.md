# Inventory: #1234 slice test-e2e
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #1121 — test/e2e/client/websocket.go still targets the ADR-096-retired /flowbuilder/status/stream — ~250 lines of e2e harness with no caller
Named sites:
- `test/e2e/client/websocket.go:138` — `func (c *WebSocketClient) buildWSURL(flowID string) (string, error) {`
- `test/e2e/client/websocket.go:150` — `return fmt.Sprintf("%s://%s/flowbuilder/status/stream?flowId=%s",`
- `test/e2e/client/websocket.go:75` — `func (c *WebSocketClient) WatchStatusStream(`
- `test/e2e/client/websocket.go:120` — `func (c *WebSocketClient) Health(ctx context.Context, flowID string) error {`
- `cmd/e2e/main.go:362` — `wsClient = client.NewWebSocketClient(wsURL)`
- `cmd/e2e/main.go:363` — `return scenarios.NewCoreDataflowScenario(edgeClient, wsClient, flags.udpEndpoint, nil)`
- `cmd/e2e/main.go:609` — `wsClient := client.NewWebSocketClient(config.DefaultEndpoints.HTTP)`
- `cmd/e2e/main.go:619` — `scenarios.NewCoreDataflowScenario(obsClient, wsClient, udpEndpoint, nil),`
- `test/e2e/scenarios/core_dataflow.go:149` — `connection, response, err := websocket.DefaultDialer.DialContext(ctx, protocolFlowWebSocketURL, nil)`
- `docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md:1` — `# ADR-096: Flow Diagrams Are Not Lifecycle Authority`
The body cited `cmd/e2e/main.go:365-371,602` for construction/handoff; those lines now hold unrelated content (a comment block about ADR-102/ADR-104). Found by text of `NewWebSocketClient`/`NewCoreDataflowScenario` instead; both call sites are pinned above. `buildWSURL` (138-152) and the `/flowbuilder/status/stream` literal (150) are unmoved from the body's citation.
Refusal and observation:
(none — see Searches: no `errs.`, `slog.`, or `log.` call inside `buildWSURL`, `WatchStatusStream`, or `Health`)
Nearest pattern instance:
- `test/e2e/client/nats.go:346` — `func (c *NATSValidationClient) BucketExists(ctx context.Context, bucketName string) (bool, error) {`

## #1125 — testutil/flow.go FlowBuilder/NewFlowBuilder/FlowComponentConfig have zero callers — dead config-shaped builder outside the ADR-100 D5 surface
Named sites:
- `testutil/flow.go:161` — `type FlowComponentConfig struct {`
- `testutil/flow.go:186` — `type FlowBuilder struct {`
- `testutil/flow.go:192` — `func NewFlowBuilder(name string) *FlowBuilder {`
- `docs/adr/100-compositions-are-validated-diagrams-are-projections.md:1` — `# ADR-100: Compositions Are Validated; Diagrams Are Projections`
`testutil/flow.go` is unchanged at 255 lines total; the body's cited range (185-255) still holds exactly.
`service/register_test.go:29` — `func TestServiceRegistryHasNoFlowBuilder(t *testing.T) {` names a different referent: a `"flow-builder"` SERVICE-REGISTRY string (ADR-100 D5), not the `testutil.FlowBuilder` type — it does not call the named symbols.
Refusal and observation:
(none — see Searches: no `errs.`, `slog.`, or `log.` call in `FlowBuilder`/`NewFlowBuilder`/`FlowComponentConfig`)
Nearest pattern instance:
(none — see Searches: no `errs.` call and no `Create`/`Exists`-shaped function in `testutil/*.go` outside `_test.go` files)

## #1135 — cmd/e2e: the {{ template-variable exclusion in scenarioNamesInTaskfileContent is unreachable dead code
Named sites:
- `cmd/e2e/dispatch_coverage_test.go:491` — `func scenarioNamesInTaskfileContent(content string) map[string]bool {`
- `cmd/e2e/dispatch_coverage_test.go:499` — `if strings.Contains(match[1], "{{") {`
- `cmd/e2e/dispatch_coverage_test.go:210` — `name:  "template variable is not a literal scenario name",`
- `cmd/e2e/dispatch_coverage_test.go:211` — `cd cmd/e2e && ./e2e --scenario tiered --variant {{.VARIANT}}`
- `cmd/e2e/dispatch_coverage_test.go:212` — `want:  []string{"tiered"},`
- `cmd/e2e/dispatch_coverage_test.go:91` — `func TestAdvertisedScenariosHaveARunner(t *testing.T) {`
- `Taskfile.yml:207` — `      - cd cmd/e2e && ./e2e --scenario tiered --variant {{.VARIANT}} {{.EXTRA_ARGS}} --output-dir ./test/e2e/results`
The body's cited `Taskfile.yml:182` no longer holds this line; found by text of `tiered --variant {{.VARIANT}}` and now pinned at :207 (drift, +25 lines).
Refusal and observation:
(none — see Searches: no `errs.`, `slog.`, or `log.` call in `scenarioNamesInTaskfileContent`)
Nearest pattern instance:
(none — see Searches: no `errs.` call and no `Create`/`Exists`-shaped function in `cmd/e2e/*.go` outside `_test.go` files)

## #1152 — test/e2e/scenarios/stages: dead package with zero importers carries 7 stale c360.logistics entity IDs
Named sites:
- `test/e2e/scenarios/stages/entities.go:209` — `func (v *EntityVerifier) getCriticalEntities() []string {`
- `test/e2e/scenarios/stages/entities.go:214` — `"c360.logistics.sensor.environmental.temperature.temp-sensor-001",`
- `test/e2e/scenarios/stages/entities.go:218` — `"c360.logistics.document.content.operations.doc-ops-001",`
- `test/e2e/scenarios/stages/entities.go:251` — `{"c360.logistics.document.content.operations.doc-ops-001", "document", "documents.jsonl"},`
- `test/e2e/scenarios/stages/entities.go:252` — `{"c360.logistics.document.content.quality.doc-quality-001", "document", "documents.jsonl"},`
- `test/e2e/scenarios/stages/entities.go:253` — `{"c360.logistics.work.maintenance.completed.maint-001", "maintenance", "maintenance.jsonl"},`
- `test/e2e/scenarios/stages/entities.go:254` — `{"c360.logistics.record.observation.high.obs-001", "observation", "observations.jsonl"},`
- `test/e2e/scenarios/stages/entities.go:255` — `{"c360.logistics.document.sensor.temperature.sensor-temp-001", "sensor_doc", "sensor_docs.jsonl"},`
- `test/e2e/scenarios/validate_entity.go:195` — `func (s *TieredScenario) getCriticalEntities(result *Result) ([]string, error) {`
The seven `c360.logistics.*` literals above (214, 218, 251-255) reproduce the body's count exactly; no line drift from the cited 214-255 range.
Refusal and observation:
(none — see Searches: no `errs.`, `slog.`, or `log.` call in `getCriticalEntities`)
Nearest pattern instance:
(none — see Searches: no `errs.` call and no other `Create`/`Exists`-shaped function in `test/e2e/scenarios/stages/*.go` outside `_test.go` files)

## #1222 — e2e: agentic and research-graph never populate assertions_run, so the #1195 'measured nothing' check is blind to them
Named sites:
- `test/e2e/scenarios/scenario.go:48` — `// AssertionsRun is the number of assertions the scenario actually executed.`
- `test/e2e/scenarios/scenario.go:49` — `AssertionsRun int`
- `cmd/e2e/main.go:510` — `func runScenario(ctx context.Context, logger *slog.Logger, scenario scenarios.Scenario, flags *cliFlags) int {`
- `cmd/e2e/main.go:528` — `logger.Error("Scenario failed", "error", err, "assertions_run", assertionsRun(result))`
- `cmd/e2e/main.go:536` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:543` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:574` — `return result.AssertionsRun`
- `test/e2e/scenarios/ops/scenario.go:257` — `result.AssertionsRun++`
- `test/e2e/scenarios/lessons/scenario.go:263` — `result.AssertionsRun++`
- `test/e2e/scenarios/core_slow_consumer.go:212` — `result.AssertionsRun++`
- `test/e2e/scenarios/agentic/scenario.go:284` — `result.AssertionsRun++`
- `test/e2e/scenarios/agentic/scenario.go:292` — `if want := s.assertingStageCount(); result.AssertionsRun != want {`
The body's cited `ops/scenario.go:246` has moved to :257 (drift, +11). `lessons/scenario.go:263` and `core_slow_consumer.go:212` are unmoved.
`test/e2e/scenarios/agentic/scenario.go` now DOES increment and cross-check `AssertionsRun` (lines 284, 292-293) — this is new since the body's claim that agentic never populates it; see Searches. `test/e2e/scenarios/research-graph/scenario.go` has zero occurrences of `AssertionsRun` (zero-hit search) — the body's claim still holds for research-graph.
Refusal and observation:
(none — see Searches: no `errs.` call in `runScenario`; `logger.Info`/`logger.Error`/`logger.Warn` (uncoded slog, not `errs.`) are the only emissions)
Nearest pattern instance:
- `test/e2e/scenarios/tiered_structural.go:570` — `func isExactEntityNotFound(err error) bool {`

## #1223 — test(e2e): no guard stops a scenario predicting the deployment authority — #1168's migration left two behind and nothing went red
Named sites:
- `test/e2e/config/tier_authority.go:101` — `func EffectiveAuthority(ctx context.Context, reader AuthorityReader, declaredStem string) (string, error) {`
- `test/e2e/scenarios/agentic/scenario.go:36` — `const agenticAuthorityStem = "c360.semstreams-agentic"`
- `test/e2e/scenarios/agentic/scenario.go:160` — `authority, authErr := e2econfig.EffectiveAuthority(ctx, natsClient, agenticAuthorityStem)`
- `test/e2e/scenarios/lifecycle/scenario.go:63` — `const lifecycleAuthorityStem = "c360.semstreams-lifecycle"`
- `test/e2e/scenarios/lifecycle/scenario.go:154` — `authority, err := e2econfig.EffectiveAuthority(ctx, validation, lifecycleAuthorityStem)`
- `test/e2e/scenarios/research-graph/scenario.go:210` — `authority, authErr := e2econfig.EffectiveAuthority(`
- `test/e2e/config/tier_authority.go:28` — `var tierAuthorityStem = map[string]string{`
- `test/e2e/config/tier_authority_test.go:32` — `func TestTierAuthorityMatchesShippedConfigs(t *testing.T) {`
- `test/e2e/config/tier_authority_test.go:55` — `func TestCoreAuthorityMatchesShippedConfig(t *testing.T) {`
- `test/natsclient/request_guard_test.go:82` — `func TestRequestGuardProductionCode(t *testing.T) {`
- `scripts/lint-test-ports.sh:2` — `# lint-test-ports.sh — substrate-flake guard for gh#220 Subclass 2.`
- `test/contract/rapid_test_only_dependency_test.go:27` — `func TestRapidStaysIsolatedToTestDependencies(t *testing.T) {`
The body's cited `agentic/scenario.go:34` for `agenticAuthorityStem` has moved to :36 (drift, +2); `lifecycle/scenario.go:63` is unmoved.
`agenticAuthorityStem` (line 160) and the research-graph stem (line 210, via `EffectiveAuthority`) both now call `EffectiveAuthority` rather than predicting the authority — the migration gap the body opens with appears already closed for both tiers (see Searches). The residual the body's "Proposal" section names — no per-scenario drift test for `agenticAuthorityStem`/`lifecycleAuthorityStem`, and no six-part-entity-ID-literal guard — still holds: `tierAuthorityStem` (line 28) contains only `structural`/`statistical`/`semantic`, no `agentic` or `lifecycle` entry (zero-hit search), and no guard matching "EffectiveAuthority result" or "six-part dotted entity-ID literal" exists anywhere under `test/e2e/` (zero-hit search).
Refusal and observation:
- `test/e2e/config/tier_authority.go:103` — `return "", fmt.Errorf("e2e config: reading the deployment authority needs a NATS reader; nothing may predict it from a configuration file (ADR-104)")`
(uncoded `fmt.Errorf`, not `errs.`; no `slog.`/`log.` call in `EffectiveAuthority`)
Nearest pattern instance:
- `test/natsclient/request_guard_test.go:82` — `func TestRequestGuardProductionCode(t *testing.T) {`

## #1224 — e2e(research-graph): the direct fixture is green while the synthesizer LLM is never invoked — the degraded path stamps every triple the assertions check
Named sites:
- `configs/rules/research-graph/02-route-decision-dispatch.json:23` — `"id": "route_synthesize_directly",`
- `processor/research-graph-execute/component.go:465` — `if err := c.loops.PutExecutionOutput(ctx, loopID, envelopeBytes); err != nil {`
- `processor/research-graph-execute/component.go:649` — `if err := c.loops.PutExecutionOutput(ctx, loopID, envelopeBytes); err != nil {`
- `processor/research-graph-synthesize/adapters.go:108` — `return nil, errExecutionOutputNotFound`
- `processor/research-graph-synthesize/component.go:413` — `exec, err := c.loops.GetExecutionOutput(ctx, loopID)`
- `processor/research-graph-synthesize/component.go:416` — `if errors.Is(err, errExecutionOutputNotFound) {`
- `processor/research-graph-synthesize/component.go:453` — `func (c *Component) emitDegraded(ctx context.Context, loopID, topic, reason string) {`
- `test/e2e/scenarios/research-graph/scenario.go:557` — `func (s *Scenario) verifyOrchestrationTriples(ctx context.Context, result *scenarios.Result) error {`
- `test/e2e/scenarios/research-graph/scenario.go:835` — `func (s *Scenario) verifySearchResultEnvelope(ctx context.Context, result *scenarios.Result) error {`
- `test/e2e/mock/cmd/main.go:352` — `  "rationale": "Classifier candidate set covers the topic; no graph expansion needed."`
None of the body's cited line numbers have moved: `component.go:465,649`, `adapters.go:104-111` (108 is the exact return), `component.go:413-419` (413, 416), `component.go:453-460` (453) all hold as cited.
Refusal and observation:
- `processor/research-graph-synthesize/component.go:406` — `c.logger.Error("could not load research intent; ignoring message",`
- `processor/research-graph-synthesize/component.go:409` — `atomic.AddInt64(&c.errors, 1)`
- `processor/research-graph-synthesize/component.go:419` — `c.logger.Log(ctx, level, "could not load execution output; emitting degraded synthesis",`
(no `errs.` call inside this handler; `c.logger.Error`/`c.logger.Log`/`c.logger.Warn` are uncoded slog, `atomic.AddInt64(&c.errors, 1)` is the only metric increment)
Nearest pattern instance:
- `processor/research-graph-synthesize/component.go:226` — `return errs.WrapTransient(err, ComponentName, "Start", "open loops bucket")`

## #1255 — test hygiene: 48 test files claim "Code generated by Tester Agent. DO NOT EDIT." and no generator exists
Named sites:
- `revive.toml:4` — `ignoreGeneratedHeader = false`
- `processor/agentic-tools/executor_test.go:1` — `// Code generated by Tester Agent. DO NOT EDIT.`
- `agentic/state_test.go:1` — `// Code generated by Tester Agent. DO NOT EDIT.`
- `processor/rule/config_test.go:1` — `// Code generated by Tester Agent. DO NOT EDIT.`
- `agentic/state.go:66` — `PauseRequested`
Count: `grep -rc "Code generated by \(Tester\|Reviewer\) Agent" --include="*_test.go" .` summed across all matching files → 48, matching the body's count exactly (41 "Tester Agent" + 7 "Reviewer Agent" = 48).
The body states "#1239 deleted `PauseRequested` for exactly this reason" as precedent; `PauseRequested` is still present in `agentic/state.go:66-67` and referenced live in `agentic/approval_state_test.go:232,240-241` — the cited precedent has not (yet, at this base) landed as described.
Refusal and observation:
(not applicable — the named sites are comment banners, not a function)
Nearest pattern instance:
- `test/contract/rapid_test_only_dependency_test.go:27` — `func TestRapidStaysIsolatedToTestDependencies(t *testing.T) {`
(the body's own "optionally, guard the class" proposal; no `*_test.go` banner-content guard exists yet under `test/contract/` — zero-hit search)

## #1286 — flake-guard(graph-index): PredicateLayoutSmoke's 10s operationBudget is unreachable dead code, widened on a millisecond value read as seconds
Named sites:
- `processor/graph-index/predicate_layout_smoke_integration_test.go:131` — `func TestIntegration_PredicateLayoutSmoke(t *testing.T) {`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:208` — `membershipRaw: membershipRaw, membership: nc.NewKVStore(membershipRaw), membershipStream: membershipStream,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:477` — `got, err := operation()`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:478` — `duration := time.Since(started)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:479` — `require.NoError(t, err, label)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:480` — `require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:85` — `repetitions: 30, operationBudget: 10 * time.Second,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:97` — `repetitions: 5, operationBudget: 10 * time.Second,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:89` — `// CI budgets carry ≥3× headroom over observed healthy latencies per the`
- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `docs/operations/32-predicate-layout-smoke-harness.md:219` — `| Hash plus catalog | Namespace catalog join | 333.641500 | 336.952084 |`
- `docs/operations/32-predicate-layout-smoke-harness.md:73` — `| CI | 5,000 | 20 | 2 writers x 100 | 5 | every operation <3s; p95/p99 <=3s |`
None of `predicate_layout_smoke_integration_test.go`'s cited lines (85, 97, 89-94, 208, 476-480) have moved. `natsclient/kv.go:39,54-55,538` are unmoved.
The runbook citations have drifted: the body's `docs/operations/32-…:159-172` for "333.641500 ms" is now at :219 (drift, +47-60); `docs/operations/32-…:70` for "every operation <3s" is now at :73 (drift, +3). The body's cited figure "2.664542" (its :120) no longer appears verbatim anywhere in the file (zero-hit search) — the runbook has been re-tabulated since the body was filed (likely by PR #1285/#1284).
`processor/graph-index/owner_filter_load_integration_test.go:85` — the body's cited sibling site (`operationBudget: 10 * time.Second`) is GONE: `grep -n "operationBudget: 10" processor/graph-index/owner_filter_load_integration_test.go` → 0 hits; the field no longer exists in that file at all.
Refusal and observation:
(none — see Searches: no `errs.` call in `TestIntegration_PredicateLayoutSmoke`; `require.NoError`/`require.Less` (testify, not `errs.`) are the observation mechanism)
Nearest pattern instance:
- `processor/graph-index/owner_filter_load_integration_test.go:107` — `// No operationBudget: the same natsclient KV deadline bounds this profile too, so a predicted`

## Adjacent claims
- #1121: none of the five draft PRs (#1141, #1156, #1159, #1254, #1297) name it
- #1121: body names #1116, #1093
- #1125: none of the five draft PRs name it
- #1125: body names #1116, #1093
- #1135: none of the five draft PRs name it
- #1135: body names #1130
- #1152: none of the five draft PRs name it
- #1152: body names #1149
- #1222: none of the five draft PRs name it
- #1222: body names #1195, #1217
- #1223: none of the five draft PRs name it
- #1223: body names #1168, #1217, #769
- #1224: none of the five draft PRs name it
- #1224: body names #391, #1221, #1222, #1217
- #1255: none of the five draft PRs name it
- #1255: body names #1239
- #1286: none of the five draft PRs name it
- #1286: body names #1284, #750, gh#220
- Of all referenced issues above, only #1239 is itself a row in the 60-set (`docs/proposals/pattern-classification-2026-09/issues.md:64` for #1222's self-reference and `:65` for #1223's self-reference are the issues' OWN rows, not distinct hits); #1116, #1093, #1130, #1149, #1195, #1217, #1168, #769, #391, #1221, #1284, #750 are NOT rows in the 60-set.
- `docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md:1` — `# ADR-096: Flow Diagrams Are Not Lifecycle Authority`
- `docs/adr/100-compositions-are-validated-diagrams-are-projections.md:1` — `# ADR-100: Compositions Are Validated; Diagrams Are Projections`

## Searches
- `gh issue view <n> --json number,title,body,labels,milestone` for 1121,1125,1135,1152,1222,1223,1224,1255,1286 (one loop, one call) → 9 bodies fetched
- `gh pr view <n> --json number,body` for 1141,1156,1159,1254,1297 (one loop, one call) → 5 bodies fetched
- grep of the 5 PR bodies for each of the 9 issue numbers (`#<n>\b`) → 0 for all nine
- `grep -c "^| #<n> "` docs/proposals/pattern-classification-2026-09/issues.md for 1116,1093,1130,1149,1195,1217,1168,769,391,1221,1284,750,1239 → 0 for all except 1239 (1)
- `grep -n "#1195\b|#1168\b"` issues.md → both hits are the citing issues' OWN title rows (#1222, #1223), not separate rows
- `grep -n "buildWSURL\|flowbuilder/status/stream\|func.*Health\|WatchStatusStream"` test/e2e/client/websocket.go → 18
- `git grep -n "WebSocketClient"` (non-test) → 4 (declarations/constructor only)
- `git grep -n "\.Health(\|WatchStatusStream("` test/e2e cmd/e2e → 10 (all `sseClient`/`msgLogger`/`metrics`.Health, none on `wsClient`)
- `grep -n "wsClient\."` test/e2e/scenarios/core_dataflow.go → 0
- `grep -n "NewWebSocketClient\|NewCoreDataflowScenario"` cmd/e2e/main.go → 4
- `grep -rn "ADR-096"` docs/adr/ → 4 (none are the heading itself; heading confirmed by direct sed)
- `grep -n "errs\.\|func.*Create\|func.*Exists"` test/e2e/client/*.go → 5 (BucketExists, BucketNotExists test names, RequestClassified comment)
- `grep -n "FlowBuilder\|NewFlowBuilder\|FlowComponentConfig"` testutil/flow.go → 17
- `git grep -n "FlowBuilder\|NewFlowBuilder\|FlowComponentConfig"` (repo-wide, excluding testutil/flow.go) → 5 (doc.go comments + register_test.go's unrelated "flow-builder" service string)
- `grep -n "ADR-100"` docs/adr/ → 3
- `grep -n "errs\.\|slog\.\|metric"` testutil/*.go (non-test) → 4, all string literals, no real calls
- `grep -n "^func.*Create\|^func.*Exists\|^func.*Get"` testutil/nats.go testutil/mock.go testutil/data.go → 6, none an existence-semantics shape
- `grep -n "scenarioNamesInTaskfileContent\|{{"` cmd/e2e/dispatch_coverage_test.go → 6
- `git grep -n -- "--scenario {{"` . → 0
- `grep -n "func TestAdvertisedScenariosHaveARunner"` cmd/e2e/*.go → 1
- `grep -n "tiered --variant {{.VARIANT}}"` Taskfile.yml → 1 (line 207)
- `grep -n "errs\.\|func.*Create\|func.*Exists"` cmd/e2e/*.go (non-test) → 0
- `ls test/e2e/scenarios/stages/` → 9 files
- `grep -n "c360.logistics"` test/e2e/scenarios/stages/entities.go → 7
- `grep -rn "getCriticalEntities"` test/e2e/ → 7 (two implementations: stages/entities.go, validate_entity.go)
- `grep -rln '"github.com/C360Studio/semstreams/test/e2e/scenarios/stages"'` . → 0 (zero importers, confirming the body's claim)
- `grep -n "errs\.\|slog\.\|log\."` test/e2e/scenarios/stages/entities.go lines 200-225 → 0
- `grep -n "errs\.\|^func.*Create\|^func.*Exists"` test/e2e/scenarios/stages/*.go (non-test) → 0
- `grep -n "AssertionsRun"` test/e2e/scenarios/scenario.go → 2
- `grep -n "AssertionsRun\|assertions_run"` cmd/e2e/main.go → 4
- `grep -rn "AssertionsRun++"` test/e2e/scenarios/**/*.go → 5 (ops, lessons, core_slow_consumer, agentic ×1 increment)
- `grep -rn "AssertionsRun"` test/e2e/scenarios/agentic/*.go test/e2e/scenarios/research-graph/*.go → 3, all in agentic/scenario.go; 0 in research-graph
- `grep -n "errs\.|slog\."` cmd/e2e/main.go lines 510-580 (runScenario body) → 14, all slog.Info/Error/Warn, none `errs.`
- `grep -n "errs\."` test/e2e/scenarios/*.go (non-test, top level) → 1 (tiered_structural.go:571)
- `grep -rn "func EffectiveAuthority"` test/e2e/config/*.go → 1
- `grep -n "agenticAuthorityStem"` test/e2e/scenarios/agentic/scenario.go → 3
- `grep -n "lifecycleAuthorityStem"` test/e2e/scenarios/lifecycle/scenario.go → 3
- `grep -rn "func TestCoreAuthorityMatchesShippedConfig|func TestTierAuthorityMatchesShippedConfigs"` test/e2e/ → 2
- `ls test/natsclient/request_guard_test.go scripts/lint-test-ports.sh test/contract/rapid_test_only_dependency_test.go` → all 3 exist
- `grep -n "EffectiveAuthority|AuthorityStem|platform.org|c360\."` test/e2e/scenarios/research-graph/scenario.go → 4 (includes an `EffectiveAuthority` call at :210)
- `grep -rln "EffectiveAuthority result|six-part dotted entity-ID literal"` test/e2e/ → 0 (the proposed guard does not exist)
- `sed -n '28,45p'` test/e2e/config/tier_authority.go → `tierAuthorityStem` map has 3 entries (structural, statistical, semantic); no agentic/lifecycle entry
- `sed -n '101,140p'` test/e2e/config/tier_authority.go, grepped for errs/slog → only `fmt.Errorf`, 0 `errs.`, 0 `slog.`
- `grep -n "route_synthesize_directly"` configs/rules/research-graph/02-route-decision-dispatch.json → 1
- `grep -n "func PutExecutionOutput|PutExecutionOutput("` processor/research-graph-execute/component.go → 2
- `grep -n "errExecutionOutputNotFound"` processor/research-graph-synthesize/adapters.go → 1
- `grep -rn "verifySearchResultEnvelope|verifyOrchestrationTriples"` . → 4 (both defined and called once each in test/e2e/scenarios/research-graph/scenario.go)
- `git grep -n "#391\b"` (docs, *.go) → 5, all in docs/proposals, none in the 60-set
- `sed -n '395,465p'` processor/research-graph-synthesize/component.go, grepped for errs/slog/atomic → 15 hits, all slog/atomic, 0 `errs.`
- `grep -n "errs\.|slog\.|atomic.Add"` processor/research-graph-synthesize/component.go (whole file) → confirms `errs.WrapInvalid`/`WrapFatal`/`WrapTransient` used in `NewProcessor`/`Start` only (lines 89-262), none in the emitDegraded path
- `grep -n "ignoreGeneratedHeader"` revive.toml → 1 (line 4)
- `grep -rc "Code generated by \(Tester\|Reviewer\) Agent" --include="*_test.go" .` summed → 48
- `grep -rn "Code generated by \(Tester\|Reviewer\) Agent" --include="*_test.go" .` (raw line count) → 48
- `grep -n "Code generated by .* Agent"` on executor_test.go / state_test.go / config_test.go → 1 each, all line 1
- `git grep -n "PauseRequested"` (*.go) → 5, field and usages still present
- `grep -rln "Code generated"` test/contract/*.go → 0
- `grep -n "^func Test"` test/contract/rapid_test_only_dependency_test.go → 1 (line 27)
- `sed -n '85p'` processor/graph-index/predicate_layout_smoke_integration_test.go / owner_filter_load_integration_test.go → confirmed unmoved in the smoke file, GONE (comment only) in the owner-filter file
- `sed -n '70p;120p;159,172p'` docs/operations/32-predicate-layout-smoke-harness.md → none match the body's cited content verbatim at those lines
- `grep -n "every operation <3s|2.664542"` docs/operations/32-predicate-layout-smoke-harness.md → "every operation <3s" found at :73 (moved); "2.664542" → 0 (not found verbatim anywhere)
- `grep -n "333.641500|Maximum owner|Entity owner"` docs/operations/32-predicate-layout-smoke-harness.md → found at :215-219 (moved from cited 159-172 range)
- `grep -n "operationBudget: 10"` processor/graph-index/owner_filter_load_integration_test.go → 0 (site gone)
- `sed -n '131,145p'` processor/graph-index/predicate_layout_smoke_integration_test.go, grepped for errs/slog/require → 0 (none in that range; require.NoError/Less are further down at 479-480, already pinned)
