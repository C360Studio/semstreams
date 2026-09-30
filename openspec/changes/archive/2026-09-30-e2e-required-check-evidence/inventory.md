# Required-check evidence surface inventory

base: 12ae633381b8b8b26c333efe5f5c8691cfa47fb4

Mode: INVENTORY ONLY. This file contains measured current surfaces and gaps, not a target state, options, recommendation, accepted spec delta, implementation authorization, or merge review.

## Boundary and evidence identity

Inspect the existing selected E2E checks, their outcomes, selection and evidence consumers for #1222 / draft PR1406. Source is the clean worktree `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams` at the base above. `git diff --name-only fe9482b7f336e575317cfb45fd1ad7c40baf7904 HEAD` lists only the active proposal and tasks: runtime, tests, task and workflow files are unchanged from the survey base. No tests, Docker, model calls, source mutations or GitHub writes performed.

Started from the explorer inventory `/private/tmp/semstreams-e2e-survey-20260927/evidence/gate-inventory.md` (SHA-256 `77d4d86dc932dd42315597269be9ed32d3a3610267d19cf9696a38419b25f045`, base fe9482b7f336e575317cfb45fd1ad7c40baf7904). Its complete enumerations/search log are supporting evidence; this focused inventory adds the special dispatch branches, two distinct persisted formats, per-condition slow-consumer count, intentional diagnostic semantics, failure-before-write order and current adjacency. Its zero assertions/CI/schedule/writer searches were independently repeated below. The inventory does not claim the earlier E2E executions were repeated or their external logs verified.

Structural declarations/references/implementers are separately enumerated in `evidence/structural-inventory.md` (source `/private/tmp/semstreams-1222-inventory-supplement.md`, SHA-256 `e5f320a56de698d278c39c7de51dec16fda96ffc61fcc3a46a691312764765b1`). Its gopls workspace/interface queries returned empty despite tracked implementations, so it explicitly supplements these inconclusive queries with tracked declaration evidence. Direct runScenario and WriteRun references succeeded. Materialize it with this inventory before review. Seed retained as `evidence/gate-inventory.md` is normalized only by removing eight empty-line entries from the original 579; its 571 nonempty pins pass the canonical verifier, copied SHA-256 `98e1dd2a5c3a06e6778f293e38deeecd0faf54a2faf6b9ed826345fb1af576f8`. Original source/hash remain provenance above.

Read fully: architect and reviewer contracts, openspec/project.md, both active change files, and current release-candidate-proof spec. There is no current openspec/specs/e2e-tiers/spec.md; the archived semantic-tier-split delta expressly says WITHDRAWN and is not current authority. No new binary/composition change or storage/communication primitive is proposed here; ADR-103-related composition facts remain the seed inventory's bounded adjacent evidence.

## 1. Claimed gap, checked under current spellings

The old claim that agentic never populates assertions_run is refuted. It counts completed asserting stage callbacks and checks the count against its stage list; a separate unit test pins the list. Its own comment defines this as completed stages, while the common Result comment says assertions actually executed. Tool and streaming callbacks can return nil after a warning without making the named observation. Streaming also returns nil for zero chunks. These paths therefore satisfy stage accounting without establishing that behavioral check. This is static path evidence, not a reproduced runtime failure.

- `test/e2e/scenarios/scenario.go:23` — `Execute(ctx context.Context) (*Result, error)`
- `test/e2e/scenarios/scenario.go:31` — `type Result struct {`
- `test/e2e/scenarios/scenario.go:39` — `Success bool   `json:"success"``
- `test/e2e/scenarios/scenario.go:40` — `Error   string `json:"error,omitempty"``
- `test/e2e/scenarios/scenario.go:45` — `Errors   []string       `json:"errors,omitempty"``
- `test/e2e/scenarios/scenario.go:46` — `Warnings []string       `json:"warnings,omitempty"``
- `test/e2e/scenarios/scenario.go:48` — `// AssertionsRun is the number of assertions the scenario actually executed.`
- `test/e2e/scenarios/scenario.go:49` — `AssertionsRun int `json:"assertions_run,omitempty"``
- `test/e2e/scenarios/scenario.go:53` — `Structured *TieredResults `json:"structured,omitempty"``

- `test/e2e/scenarios/agentic/scenario.go:196` — `// agenticStage is one verification the tier performs. asserts marks the stages`
- `test/e2e/scenarios/agentic/scenario.go:197` — `// whose completion is a proof: a stage that only records a baseline and`
- `test/e2e/scenarios/agentic/scenario.go:203` — `asserts bool`
- `test/e2e/scenarios/agentic/scenario.go:207` — `// the runner's assertions_run= line therefore reads as "one verification stage`
- `test/e2e/scenarios/agentic/scenario.go:214` — `// not on the list: what holds the LIST is`
- `test/e2e/scenarios/agentic/scenario.go:216` — `// asserts flag in order, so a deleted stage fails in plain `go test` with no`
- `test/e2e/scenarios/agentic/scenario.go:255` — `{name: "verify-tool-execution", fn: s.verifyToolExecution, asserts: true},`
- `test/e2e/scenarios/agentic/scenario.go:257` — `{name: "verify-streaming-metrics", fn: s.verifyStreamingMetrics, asserts: true},`
- `test/e2e/scenarios/agentic/scenario.go:320` — `if stage.asserts {`
- `test/e2e/scenarios/agentic/scenario.go:321` — `result.AssertionsRun++`
- `test/e2e/scenarios/agentic/scenario.go:329` — `if want := s.assertingStageCount(); result.AssertionsRun != want {`
- `test/e2e/scenarios/agentic/scenario.go:337` — `result.Success = true`
- `test/e2e/scenarios/agentic/scenario.go:1068` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify tool executions: %v", err))`
- `test/e2e/scenarios/agentic/scenario.go:1069` — `return nil // Non-fatal - metrics may not be available`
- `test/e2e/scenarios/agentic/scenario.go:1092` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify streaming chunks: %v", err))`
- `test/e2e/scenarios/agentic/scenario.go:1093` — `return nil`
- `test/e2e/scenarios/agentic/scenario.go:1098` — `result.Warnings = append(result.Warnings, "No streaming chunks recorded — streaming path may not have been exercised")`
- `test/e2e/scenarios/agentic/scenario.go:1099` — `return nil`
- `test/e2e/scenarios/agentic/scenario.go:1105` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify TTFT metric: %v", err))`
- `test/e2e/scenarios/agentic/scenario.go:1110` — `result.Details["streaming_verified"] = true`


Refutations and limits: tool count below one does fail when the metric observation succeeds; there are independent trajectory/approval/replay checks. The inventory does not call the whole agentic tier ineffective. The final validation checks a selected subset of outcomes, not every detail flag. Exact identity assertions remain capability-local and are not replaced by totals.

Statistical search records ExecuteAll statistics and returns nil. The final semantic requirement guard is conditional on semantic selection and only checks zero passed when the known-answer denominator is positive. This is not a claim all semantic search failures pass: other independent hard stages exist. Core dataflow hard-fails insufficient observed file lines but converts content-validation issues into warnings; if file counting itself fails it falls back to component validation. Those are distinct proof obligations.

- `test/e2e/scenarios/agentic/scenario.go:1078` — `if toolExecutions < 1 {`
- `test/e2e/scenarios/agentic/scenario.go:1177` — `func (s *Scenario) validateResults(_ context.Context, result *scenarios.Result) error {`
- `test/e2e/scenarios/agentic/scenario.go:1182` — `if outcome, _ := result.Details["approval_outcome"].(string); outcome != agentic.OutcomeSuccess {`
- `test/e2e/scenarios/agentic/scenario.go:1196` — `if outcome, _ := result.Details[key].(string); outcome != agentic.OutcomeSuccess {`

- `test/e2e/scenarios/validate_search.go:42` — `}`
- `test/e2e/scenarios/validate_search.go:480` — `return nil`
- `test/e2e/scenarios/validate_search.go:488` — `} else if s.config.Variant == "" {`
- `test/e2e/scenarios/validate_search.go:513` — `// Check embeddings were generated via variant info`

- `test/e2e/scenarios/core_dataflow.go:266` — `if err != nil {`
- `test/e2e/scenarios/core_dataflow.go:268` — `return s.executeValidateComponentsOnly(ctx, result)`
- `test/e2e/scenarios/core_dataflow.go:269` — `}`
- `test/e2e/scenarios/core_dataflow.go:275` — `result.Errors = append(result.Errors,`
- `test/e2e/scenarios/core_dataflow.go:278` — `}`
- `test/e2e/scenarios/core_dataflow.go:282` — `if len(contentIssues) > 0 {`
- `test/e2e/scenarios/core_dataflow.go:285` — `}`
- `test/e2e/scenarios/core_dataflow.go:293` — `}`


Runner zero-exit is driven by Execute error and Result.Success, not shared required-check membership or the count. Errors fail before structured evidence serialization. Setup fails before Execute; teardown only warns. A nil result with nil error is not guarded before dereference (observable source constraint, not a new scope request).

- `cmd/e2e/main.go:513` — `if err := scenario.Setup(ctx); err != nil {`
- `cmd/e2e/main.go:515` — `return 1`
- `cmd/e2e/main.go:519` — `result, err := scenario.Execute(ctx)`
- `cmd/e2e/main.go:523` — `if teardownErr := scenario.Teardown(ctx); teardownErr != nil {`
- `cmd/e2e/main.go:524` — `logger.Warn("Teardown failed", "error", teardownErr)`
- `cmd/e2e/main.go:527` — `if err != nil {`
- `cmd/e2e/main.go:529` — `return 1`
- `cmd/e2e/main.go:532` — `if !result.Success {`
- `cmd/e2e/main.go:537` — `return 1`
- `cmd/e2e/main.go:543` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:546` — `if flags.outputDir != "" && result.Structured != nil {`
- `cmd/e2e/main.go:547` — `filepath, err := scenarios.SaveStructuredResults(result.Structured, flags.outputDir)`
- `cmd/e2e/main.go:549` — `logger.Warn("Failed to save structured results", "error", err)`
- `cmd/e2e/main.go:565` — `}`


## 2. Current spellings and owners of selection, outcome and evidence

Selection has several owners, not one inventory string: CLI flags and branch dispatch; constructor variant configuration; bundle lists; tiered stage variant filtering; agentic stage flags; task composites and compose profiles; CI jobs; detached release evidence requirements. Listing/dispatch tests establish reachability, not actual selected execution or behavioral proof.

CLI default/all is the core bundle; semantic and rules take special branches before createScenario. All three bundles pass fresh empty cliFlags to runScenario, so output-dir is lost. The semantic branch constructs DefaultTieredConfig, leaving variant auto-detection and default service/metric URLs; it does not take the explicit variant/config setup in createScenario. Crucial refutation: task e2e:semantic uses --scenario tiered --variant semantic, so the task follows the explicit config path rather than this alias.

- `cmd/e2e/main.go:128` — `flag.StringVar(&flags.scenarioName, "scenario", "",`
- `cmd/e2e/main.go:129` — `"Run specific scenario (core-health, core-dataflow, core-graph-roundtrip, lessons, or 'all')")`
- `cmd/e2e/main.go:136` — `flag.StringVar(&flags.variant, "variant", "",`
- `cmd/e2e/main.go:138` — `flag.StringVar(&flags.outputDir, "output-dir", "",`
- `cmd/e2e/main.go:139` — `"Directory for saving results JSON (empty=no output)")`
- `cmd/e2e/main.go:314` — `if flags.scenarioName == "" || flags.scenarioName == "all" {`
- `cmd/e2e/main.go:316` — `return runAllScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:317` — `} else if flags.scenarioName == "semantic" {`
- `cmd/e2e/main.go:319` — `return runSemanticScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:320` — `} else if flags.scenarioName == "rules" {`
- `cmd/e2e/main.go:322` — `return runRulesScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:395` — `case "tiered", "structural", "statistical", "semantic":`
- `cmd/e2e/main.go:397` — `cfg.MetricsURL = flags.metricsURL`
- `cmd/e2e/main.go:401` — `// Set variant from flag or scenario name`
- `cmd/e2e/main.go:403` — `if cfg.Variant == "" {`
- `cmd/e2e/main.go:620` — `}`
- `cmd/e2e/main.go:627` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `cmd/e2e/main.go:659` — `scenarios.NewTieredScenario(obsClient, udpEndpoint, cfg),`
- `cmd/e2e/main.go:660` — `}`
- `cmd/e2e/main.go:667` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `cmd/e2e/main.go:697` — `cfg := scenarios.DefaultTieredConfig()`
- `cmd/e2e/main.go:698` — `cfg.Variant = "structural"`
- `cmd/e2e/main.go:708` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`

- `taskfiles/e2e/semantic.yml:21` — `- cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`

- `Taskfile.yml:165` — `e2e:tiers:`
- `Taskfile.yml:166` — `desc: Run all tier E2E tests (structural -> statistical -> semantic)`
- `Taskfile.yml:174` — `- 'echo ""'`
- `Taskfile.yml:180` — `- sleep 3`
- `Taskfile.yml:186` — `VARIANT: statistical`
- `Taskfile.yml:197` — `- cd cmd/e2e && ./e2e --compare-tiers --output-dir ./test/e2e/results`
- `Taskfile.yml:207` — `- cd cmd/e2e && ./e2e --scenario tiered --variant {{.VARIANT}} {{.EXTRA_ARGS}} --output-dir ./test/e2e/results`
- `Taskfile.yml:209` — `e2e:all:`
- `Taskfile.yml:210` — `desc: Run all E2E tests (core -> inference tiers -> agentic)`
- `Taskfile.yml:212` — `- task: e2e:core`
- `Taskfile.yml:213` — `- task: e2e:tiers`
- `Taskfile.yml:214` — `- task: e2e:agentic`

- `test/e2e/scenarios/tiered.go:112` — `return &TieredConfig{`
- `test/e2e/scenarios/tiered.go:114` — `MessageCount:    20,`
- `test/e2e/scenarios/tiered.go:130` — `MinExpectedEntities:  50,                     // Test data has 74 entities, expect at least 50 indexed`
- `test/e2e/scenarios/tiered.go:131` — `NatsURL:              config.DefaultEndpoints.NATS,`
- `test/e2e/scenarios/tiered.go:136` — `OutputDir:            "test/e2e/results",`
- `test/e2e/scenarios/tiered.go:341` — `{"test-predicate-compound", s.executeTestPredicateCompound, nil},`
- `test/e2e/scenarios/tiered.go:348` — `{"validate-entity-triples", s.executeValidateEntityTriples, []string{"structural"}},`
- `test/e2e/scenarios/tiered.go:380` — `// the "globalSearch returns count=0 for content that exists" bug class`
- `test/e2e/scenarios/tiered.go:404` — `stages := []stage{}`
- `test/e2e/scenarios/tiered.go:409` — `for _, allowedVariant := range st.variants {`
- `test/e2e/scenarios/tiered.go:411` — `stages = append(stages, st)`
- `test/e2e/scenarios/tiered.go:442` — `// executeStages runs all stages with progress logging.`
- `test/e2e/scenarios/tiered.go:449` — `if err := stage.fn(ctx, result); err != nil {`
- `test/e2e/scenarios/tiered.go:461` — `result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = duration.Milliseconds()`
- `test/e2e/scenarios/tiered.go:541` — `info := s.detectVariantAndProvider(result)`
- `test/e2e/scenarios/tiered.go:567` — `result.Success = true`
- `test/e2e/scenarios/tiered.go:570` — `result.Structured = BuildTieredResults(result, s.searchStats)`
- `test/e2e/scenarios/tiered.go:572` — `if err := s.validateSemanticRequirements(result); err != nil {`
- `test/e2e/scenarios/tiered.go:578` — `if err := s.validateFallbackBehavior(result); err != nil {`


The outcome/evidence homes overlap but differ:

Scenario.Result owns scenario/time/success/error, flat metrics/details/errors/warnings, assertion count and optional TieredResults.
Agentic, lessons and ops increment after callbacks; slow-consumer increments at each evaluated condition, including the failing condition. Research and the other legacy scenarios return Results without this field. A zero count is therefore ambiguous across current scenarios, and changing its meaning is a contract question.
TieredResults and TestMetadata own typed domain observations and copied success/warnings; BuildTieredResults executes before the final semantic/fallback guards, so its copied metadata may predate final outer success. Timing stage durations are execution timing, not independent assertions.
SaveStructuredResults persists TieredResults as variant-timestamp.json; shared runner calls it only for successful results with nonnil Structured and nonempty output-dir, and warns on write/scrape failure.
results.TestRun holds []Result, config, environment and summary; WriteRun persists e2e-results-variant-timestamp.json; WriteLatest optionally adds a symlink. These are existing concrete owners, not permission to add a sibling report family.
Legacy comparison uses ListRuns filtered by e2e-results and loads TestRun. Structured comparisons consume the separate TieredResults format. The two formats and filename selectors must not be treated as interchangeable.
Detached release-candidate proof already owns exact SHA/command/runner/time/exit/digest and required release gates. Ordinary run JSON and release authorization are different scopes.

- `test/e2e/scenarios/core_slow_consumer.go:207` — `return requireSlowConsumer(result, result.AssertionsRun+1 == slowConsumerExpectedAssertions,`
- `test/e2e/scenarios/core_slow_consumer.go:211` — `func requireSlowConsumer(result *Result, condition bool, format string, args ...any) error {`
- `test/e2e/scenarios/core_slow_consumer.go:212` — `result.AssertionsRun++`
- `test/e2e/scenarios/core_slow_consumer.go:213` — `if !condition {`

- `test/e2e/scenarios/lessons/scenario.go:263` — `result.AssertionsRun++`

- `test/e2e/scenarios/ops/scenario.go:257` — `result.AssertionsRun++`

- `test/e2e/scenarios/results_common_types.go:31` — `type TimingResults struct {`
- `test/e2e/scenarios/results_common_types.go:36` — `StageDurations map[string]int64 `json:"stage_durations"``
- `test/e2e/scenarios/results_common_types.go:40` — `type TestMetadata struct {`
- `test/e2e/scenarios/results_common_types.go:42` — `Variant string `json:"variant"``
- `test/e2e/scenarios/results_common_types.go:45` — `StartedAt time.Time `json:"started_at"``
- `test/e2e/scenarios/results_common_types.go:48` — `CompletedAt time.Time `json:"completed_at"``
- `test/e2e/scenarios/results_common_types.go:51` — `Success bool `json:"success"``
- `test/e2e/scenarios/results_common_types.go:54` — `ErrorCount int `json:"error_count"``
- `test/e2e/scenarios/results_common_types.go:57` — `WarningCount int `json:"warning_count"``
- `test/e2e/scenarios/results_common_types.go:60` — `Errors []string `json:"errors,omitempty"``
- `test/e2e/scenarios/results_common_types.go:63` — `Warnings []string `json:"warnings,omitempty"``
- `test/e2e/scenarios/results_common_types.go:66` — `Version string `json:"version,omitempty"``

- `test/e2e/scenarios/results.go:437` — `func BuildTieredResults(result *Result, searchStats *search.Stats) *TieredResults {`
- `test/e2e/scenarios/results.go:447` — `Success:      result.Success,`
- `test/e2e/scenarios/results.go:450` — `Errors:       result.Errors,`
- `test/e2e/scenarios/results.go:451` — `Warnings:     result.Warnings,`
- `test/e2e/scenarios/results.go:738` — `func SaveStructuredResults(tr *TieredResults, outputDir string) (string, error) {`
- `test/e2e/scenarios/results.go:747` — `filename := fmt.Sprintf("%s-%s.json",`
- `test/e2e/scenarios/results.go:748` — `tr.Variant.Name,`
- `test/e2e/scenarios/results.go:765` — `func LoadStructuredResults(path string) (*TieredResults, error) {`

- `test/e2e/results/writer.go:19` — `type TestRun struct {`
- `test/e2e/results/writer.go:25` — `Scenarios   []scenarios.Result    `json:"scenarios"``
- `test/e2e/results/writer.go:27` — `Summary     Summary               `json:"summary"``
- `test/e2e/results/writer.go:28` — `Environment map[string]string     `json:"environment,omitempty"``
- `test/e2e/results/writer.go:32` — `type TestRunConfig struct {`
- `test/e2e/results/writer.go:35` — `Scenarios  []string `json:"scenarios"``
- `test/e2e/results/writer.go:41` — `type Summary struct {`
- `test/e2e/results/writer.go:42` — `TotalScenarios  int     `json:"total_scenarios"``
- `test/e2e/results/writer.go:48` — `AllPassed       bool    `json:"all_passed"``
- `test/e2e/results/writer.go:49` — `}`
- `test/e2e/results/writer.go:95` — `type Writer struct {`
- `test/e2e/results/writer.go:105` — `func (w *Writer) WriteRun(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:112` — `filename := fmt.Sprintf("e2e-results-%s-%s.json",`
- `test/e2e/results/writer.go:132` — `func (w *Writer) WriteLatest(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:173` — `return nil, fmt.Errorf("reading results file: %w", err)`
- `test/e2e/results/writer.go:184` — `// CreateTestRun creates a new TestRun with computed summary`
- `test/e2e/results/writer.go:208` — `func computeSummary(results []scenarios.Result) Summary {`
- `test/e2e/results/writer.go:214` — `if r.Success {`
- `test/e2e/results/writer.go:220` — `summary.TotalWarnings += len(r.Warnings)`
- `test/e2e/results/writer.go:227` — `summary.AllPassed = summary.PassedScenarios == summary.TotalScenarios`
- `test/e2e/results/writer.go:444` — `func (w *Writer) ListRuns() ([]string, error) {`
- `test/e2e/results/writer.go:456` — `strings.Contains(entry.Name(), "e2e-results") {`

- `cmd/e2e/main.go:739` — `writer := results.NewWriter(outputDir)`
- `cmd/e2e/main.go:742` — `files, err := writer.ListRuns()`
- `cmd/e2e/main.go:760` — `}`
- `cmd/e2e/main.go:768` — `if statisticalRun != nil && semanticRun != nil {`
- `cmd/e2e/main.go:770` — `}`
- `cmd/e2e/main.go:786` — `logger.Error("Failed to write comparison report", "error", err)`



### Existing required-set records and their live counterparts

The extracted `stages` package already models the fact this inventory investigates. `ComponentResult` carries Required, Found and Missing. `ComponentVerifier.VerifyComponents` derives a variant-specific required name set, compares it to observed names and returns the result plus an error when any required component is missing. Found is the total observed component count, not the count of required matches. Its sibling VerifyOutputs also carries Expected/Found/Missing/AllHealthy and hard-refuses missing output names.

`IndexSpec.Required` marks required versus optional indexes; defaults mark entity_states, predicate, incoming, outgoing and temporal required, alias/spatial optional. `IndexPopulationResult` carries Total, Populated, EmptyRequired, per-index errors and Warnings. VerifyIndexPopulation marks required indexes empty for either CountBucketKeys error or zero keys. A nil NATS client hard-refuses, but nonempty EmptyRequired only appends a warning before returning result,nil. Therefore an existing Required flag does not uniformly imply failure admission.

Wiring evidence: gopls queries were attempted once per listed symbol/field but workspace loading reported Go-cache access denied; partial local references are not completeness proof. Bounded tracked fallback finds zero Go imports of `test/e2e/scenarios/stages`; exact-symbol hits are confined to components.go, indexes.go and indexes_test.go. The indexes tests call DefaultIndexSpecs and manually construct/read IndexPopulationResult, but do not call VerifyIndexPopulation. On this tracked baseline these extracted verifiers are not the assembled tier's execution path. That is a wiring observation, not a claim that they are unwanted, absent, dead, or deletable; no external consumers were inspected.

The assembled tier separately schedules executeVerifyComponents and executeVerifyIndexPopulation for all variants. Live required-component validation hard-fails missing names and records component_breakdown.required/total/found; its required set includes graph-index and graph-gateway, unlike the extracted component verifier. Live index validation declares its own required flags and empty_required list, records observation errors, and warning-returns nil for missing required indexes. These are additional current spellings/owners, not proof the extracted wrapper is in use. Constructor/Setup normally requires a NATS client, so the live nil-client warning branch is a code branch rather than a demonstrated healthy-boot failure.

- `test/e2e/scenarios/stages/components.go:12` — `type ComponentVerifier struct {`
- `test/e2e/scenarios/stages/components.go:18` — `func (v *ComponentVerifier) VerifyComponents(ctx context.Context) (*ComponentResult, error) {`
- `test/e2e/scenarios/stages/components.go:24` — `required := v.getRequiredComponents()`
- `test/e2e/scenarios/stages/components.go:32` — `for _, req := range required {`
- `test/e2e/scenarios/stages/components.go:33` — `if !foundComponents[req] {`
- `test/e2e/scenarios/stages/components.go:34` — `missing = append(missing, req)`
- `test/e2e/scenarios/stages/components.go:38` — `result := &ComponentResult{`
- `test/e2e/scenarios/stages/components.go:40` — `Required: required,`
- `test/e2e/scenarios/stages/components.go:41` — `Found:    len(components),`
- `test/e2e/scenarios/stages/components.go:42` — `Missing:  missing,`
- `test/e2e/scenarios/stages/components.go:45` — `if len(missing) > 0 {`
- `test/e2e/scenarios/stages/components.go:46` — `return result, fmt.Errorf("missing components: %v", missing)`
- `test/e2e/scenarios/stages/components.go:49` — `return result, nil`
- `test/e2e/scenarios/stages/components.go:53` — `func (v *ComponentVerifier) getRequiredComponents() []string {`
- `test/e2e/scenarios/stages/components.go:57` — `return []string{"udp", "iot_sensor", "rule", "graph-ingest", "file"}`
- `test/e2e/scenarios/stages/components.go:66` — `// Semantic components`
- `test/e2e/scenarios/stages/components.go:76` — `type ComponentResult struct {`
- `test/e2e/scenarios/stages/components.go:78` — `Required []string `json:"required"``
- `test/e2e/scenarios/stages/components.go:79` — `Found    int      `json:"found"``
- `test/e2e/scenarios/stages/components.go:80` — `Missing  []string `json:"missing,omitempty"``
- `test/e2e/scenarios/stages/components.go:84` — `func (v *ComponentVerifier) VerifyOutputs(ctx context.Context) (*OutputResult, error) {`
- `test/e2e/scenarios/stages/components.go:114` — `}`
- `test/e2e/scenarios/stages/components.go:117` — `return result, fmt.Errorf("missing output components: %v", missing)`
- `test/e2e/scenarios/stages/components.go:124` — `type OutputResult struct {`
- `test/e2e/scenarios/stages/components.go:125` — `Expected   []string `json:"expected"``
- `test/e2e/scenarios/stages/components.go:126` — `Found      int      `json:"found"``
- `test/e2e/scenarios/stages/components.go:127` — `Missing    []string `json:"missing,omitempty"``
- `test/e2e/scenarios/stages/components.go:128` — `AllHealthy bool     `json:"all_healthy"``
- `test/e2e/scenarios/stages/indexes.go:11` — `type IndexVerifier struct {`
- `test/e2e/scenarios/stages/indexes.go:16` — `type IndexSpec struct {`
- `test/e2e/scenarios/stages/indexes.go:19` — `Required bool   `json:"required"``
- `test/e2e/scenarios/stages/indexes.go:23` — `func DefaultIndexSpecs() []IndexSpec {`
- `test/e2e/scenarios/stages/indexes.go:25` — `{"entity_states", client.IndexBuckets.EntityStates, true},`
- `test/e2e/scenarios/stages/indexes.go:26` — `{"predicate", client.IndexBuckets.Predicate, true},`
- `test/e2e/scenarios/stages/indexes.go:27` — `{"incoming", client.IndexBuckets.Incoming, true},`
- `test/e2e/scenarios/stages/indexes.go:28` — `{"outgoing", client.IndexBuckets.Outgoing, true},`
- `test/e2e/scenarios/stages/indexes.go:29` — `{"alias", client.IndexBuckets.Alias, false},     // May be empty if no aliases`
- `test/e2e/scenarios/stages/indexes.go:30` — `{"spatial", client.IndexBuckets.Spatial, false}, // May be empty if no geo data`
- `test/e2e/scenarios/stages/indexes.go:31` — `{"temporal", client.IndexBuckets.Temporal, true},`
- `test/e2e/scenarios/stages/indexes.go:45` — `type IndexPopulationResult struct {`
- `test/e2e/scenarios/stages/indexes.go:46` — `Populated     int                    `json:"populated"``
- `test/e2e/scenarios/stages/indexes.go:47` — `Total         int                    `json:"total"``
- `test/e2e/scenarios/stages/indexes.go:48` — `EmptyRequired []string               `json:"empty_required,omitempty"``
- `test/e2e/scenarios/stages/indexes.go:49` — `Indexes       map[string]IndexDetail `json:"indexes"``
- `test/e2e/scenarios/stages/indexes.go:50` — `Warnings      []string               `json:"warnings,omitempty"``
- `test/e2e/scenarios/stages/indexes.go:54` — `func (v *IndexVerifier) VerifyIndexPopulation(ctx context.Context, specs []IndexSpec) (*IndexPopulationResult, error) {`
- `test/e2e/scenarios/stages/indexes.go:55` — `if v.NATSClient == nil {`
- `test/e2e/scenarios/stages/indexes.go:56` — `return nil, fmt.Errorf("NATS client not available")`
- `test/e2e/scenarios/stages/indexes.go:69` — `count, err := v.NATSClient.CountBucketKeys(ctx, spec.Bucket)`
- `test/e2e/scenarios/stages/indexes.go:71` — `detail.Error = err.Error()`
- `test/e2e/scenarios/stages/indexes.go:73` — `if spec.Required {`
- `test/e2e/scenarios/stages/indexes.go:74` — `result.EmptyRequired = append(result.EmptyRequired, spec.Name)`
- `test/e2e/scenarios/stages/indexes.go:77` — `continue`
- `test/e2e/scenarios/stages/indexes.go:81` — `detail.Populated = count > 0`
- `test/e2e/scenarios/stages/indexes.go:89` — `} else if spec.Required {`
- `test/e2e/scenarios/stages/indexes.go:90` — `result.EmptyRequired = append(result.EmptyRequired, spec.Name)`
- `test/e2e/scenarios/stages/indexes.go:96` — `if len(result.EmptyRequired) > 0 {`
- `test/e2e/scenarios/stages/indexes.go:97` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/stages/indexes.go:98` — `fmt.Sprintf("Required indexes empty: %v", result.EmptyRequired))`
- `test/e2e/scenarios/stages/indexes.go:101` — `return result, nil`
- `test/e2e/scenarios/stages/indexes_test.go:7` — `func TestDefaultIndexSpecs(t *testing.T) {`
- `test/e2e/scenarios/stages/indexes_test.go:8` — `specs := DefaultIndexSpecs()`
- `test/e2e/scenarios/stages/indexes_test.go:22` — `if spec.Required {`
- `test/e2e/scenarios/stages/indexes_test.go:33` — `expectedIndexes := []string{"entity_states", "predicate", "incoming", "outgoing", "temporal"}`
- `test/e2e/scenarios/stages/indexes_test.go:48` — `func TestIndexPopulationResult_EmptyRequired(t *testing.T) {`
- `test/e2e/scenarios/stages/indexes_test.go:49` — `result := &IndexPopulationResult{`
- `test/e2e/scenarios/stages/indexes_test.go:52` — `EmptyRequired: []string{"temporal"},`
- `test/e2e/scenarios/stages/indexes_test.go:56` — `if len(result.EmptyRequired) == 0 {`
- `test/e2e/scenarios/stages/indexes_test.go:61` — `if len(result.EmptyRequired) > 0 {`
- `test/e2e/scenarios/stages/indexes_test.go:62` — `result.Warnings = append(result.Warnings, "Required indexes empty: temporal")`
- `test/e2e/scenarios/stages/indexes_test.go:65` — `if len(result.Warnings) == 0 {`
- `test/e2e/scenarios/tiered.go:217` — `return fmt.Errorf("NATS validation client is required by every tier: %w", err)`
- `test/e2e/scenarios/tiered.go:250` — `{"verify-components", s.executeVerifyComponents, nil},`
- `test/e2e/scenarios/tiered.go:284` — `{"verify-index-population", s.executeVerifyIndexPopulation, nil},`
- `test/e2e/scenarios/validate_infra.go:20` — `func (s *TieredScenario) executeVerifyComponents(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:36` — `allRequired = []string{"udp", "iot_sensor", "rule", "graph-ingest", "graph-index", "graph-gateway", "file"}`
- `test/e2e/scenarios/validate_infra.go:44` — `graphComponents := []string{"graph-ingest", "graph-index", "graph-gateway"}`
- `test/e2e/scenarios/validate_infra.go:48` — `allRequired = append(inputComponents, domainProcessors...)`
- `test/e2e/scenarios/validate_infra.go:59` — `missingComponents := []string{}`
- `test/e2e/scenarios/validate_infra.go:60` — `for _, required := range allRequired {`
- `test/e2e/scenarios/validate_infra.go:61` — `if !foundComponents[required] {`
- `test/e2e/scenarios/validate_infra.go:62` — `missingComponents = append(missingComponents, required)`
- `test/e2e/scenarios/validate_infra.go:66` — `if len(missingComponents) > 0 {`
- `test/e2e/scenarios/validate_infra.go:69` — `return fmt.Errorf("missing components: %v", missingComponents)`
- `test/e2e/scenarios/validate_infra.go:72` — `result.Details["component_breakdown"] = map[string]any{`
- `test/e2e/scenarios/validate_infra.go:74` — `"required": allRequired,`
- `test/e2e/scenarios/validate_infra.go:75` — `"total":    len(allRequired),`
- `test/e2e/scenarios/validate_infra.go:76` — `"found":    len(components),`
- `test/e2e/scenarios/validate_infra.go:79` — `return nil`
- `test/e2e/scenarios/validate_structural.go:14` — `func (s *TieredScenario) executeVerifyIndexPopulation(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_structural.go:15` — `if s.natsClient == nil {`
- `test/e2e/scenarios/validate_structural.go:16` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping index population verification")`
- `test/e2e/scenarios/validate_structural.go:17` — `return nil`
- `test/e2e/scenarios/validate_structural.go:21` — `indexes := []struct {`
- `test/e2e/scenarios/validate_structural.go:24` — `required bool`
- `test/e2e/scenarios/validate_structural.go:26` — `{"entity_states", client.IndexBuckets.EntityStates, true},`
- `test/e2e/scenarios/validate_structural.go:30` — `{"alias", client.IndexBuckets.Alias, false},     // May be empty if no aliases`
- `test/e2e/scenarios/validate_structural.go:31` — `{"spatial", client.IndexBuckets.Spatial, false}, // May be empty if no geo data`
- `test/e2e/scenarios/validate_structural.go:32` — `{"temporal", client.IndexBuckets.Temporal, true},`
- `test/e2e/scenarios/validate_structural.go:40` — `count, err := s.natsClient.CountBucketKeys(ctx, idx.bucket)`
- `test/e2e/scenarios/validate_structural.go:43` — `"bucket":    idx.bucket,`
- `test/e2e/scenarios/validate_structural.go:47` — `if idx.required {`
- `test/e2e/scenarios/validate_structural.go:48` — `emptyRequired = append(emptyRequired, idx.name)`
- `test/e2e/scenarios/validate_structural.go:56` — `} else if idx.required {`
- `test/e2e/scenarios/validate_structural.go:57` — `emptyRequired = append(emptyRequired, idx.name)`
- `test/e2e/scenarios/validate_structural.go:71` — `result.Metrics["indexes_populated"] = populatedCount`
- `test/e2e/scenarios/validate_structural.go:72` — `result.Metrics["indexes_total"] = len(indexes)`
- `test/e2e/scenarios/validate_structural.go:74` — `result.Details["index_population_verification"] = map[string]any{`
- `test/e2e/scenarios/validate_structural.go:78` — `"empty_required": emptyRequired,`
- `test/e2e/scenarios/validate_structural.go:82` — `if len(emptyRequired) > 0 {`
- `test/e2e/scenarios/validate_structural.go:83` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/validate_structural.go:84` — `fmt.Sprintf("Required indexes empty: %v", emptyRequired))`
- `test/e2e/scenarios/validate_structural.go:87` — `return nil`

### Shell obligations outside Scenario

Core task performs readiness/heartbeat, normal SIGTERM, early SIGTERM during blocked NATS connection, and pre-identity refusal assertions in shell. They already fail the outer task on missing observations, and are absent from Scenario.AssertionsRun. Therefore a Scenario-only inventory cannot stand for the entire required task set. Lifecycle captures scenario exit through teardown explicitly. These are existing selection/outcome owners, not new tasks proposed here.

- `taskfiles/e2e/core.yml:80` — `[ "$matched" = "true" ] || fail "heartbeat totals did not match runtime component health within 10s"`
- `taskfiles/e2e/core.yml:102` — `[ "$exit_code" -eq 0 ] || fail "application exited with code $exit_code"`
- `taskfiles/e2e/core.yml:105` — `|| fail "shutdown-complete record missing"`
- `taskfiles/e2e/core.yml:168` — `[ "$connecting" = "true" ] || fail_early_boot "Connecting to NATS evidence absent"`
- `taskfiles/e2e/core.yml:180` — `[ "$accepted" = "true" ] || fail_early_boot "blackhole did not accept the NATS TCP connection"`
- `taskfiles/e2e/core.yml:197` — `[ "$exited" = "true" ] || fail_early_boot "application did not exit within 10s of SIGTERM"`
- `taskfiles/e2e/core.yml:201` — `[ "$exit_code" -eq 1 ] \`
- `taskfiles/e2e/core.yml:202` — `|| fail_early_boot "early SIGTERM exited $exit_code, want controlled failed-boot exit 1 (not default 143)"`
- `taskfiles/e2e/core.yml:206` — `|| fail_early_boot "context-cancellation evidence missing after early SIGTERM"`
- `taskfiles/e2e/core.yml:208` — `fail_early_boot "services started after early SIGTERM"`
- `taskfiles/e2e/core.yml:243` — `[ "$exited" = "true" ] || fail_pre_identity "application did not exit within 20s of booting a pre-identity bucket"`
- `taskfiles/e2e/core.yml:247` — `[ "$exit_code" -ne 0 ] || fail_pre_identity "application booted a pre-identity bucket instead of refusing it"`
- `taskfiles/e2e/core.yml:250` — `|| fail_pre_identity "the refusal did not name the pre-identity bucket as the cause"`
- `taskfiles/e2e/core.yml:252` — `(cd cmd/e2e && ./e2e --scenario core-pre-identity-assert) \`
- `taskfiles/e2e/core.yml:253` — `|| fail_pre_identity "the refused boot left a platform_identity record behind"`
- `taskfiles/e2e/lifecycle.yml:29` — `(cd cmd/e2e && ./e2e --scenario lifecycle) || rc=$?`
- `taskfiles/e2e/lifecycle.yml:30` — `docker compose -f docker/compose/lifecycle.yml down -v --timeout 15`
- `taskfiles/e2e/lifecycle.yml:31` — `exit $rc`

## 3. Adjacent claims and current authority

Active change has no design/spec delta yet; every task is unchecked. Its initial proposal deliberately does not pick a representation or suite membership. No permission for implementation follows from the claim.

- `openspec/changes/e2e-required-check-evidence/proposal.md:3` — `Issue: #1222. Milestone: v1.0.0-beta.165, #1134 work package C.`
- `openspec/changes/e2e-required-check-evidence/proposal.md:4` — `Baseline: fe9482b7f336e575317cfb45fd1ad7c40baf7904.`
- `openspec/changes/e2e-required-check-evidence/proposal.md:15` — `This initial commit establishes the design-phase claim only. It changes no runner behavior, test, current spec,`
- `openspec/changes/e2e-required-check-evidence/proposal.md:18` — `The first deliverable is the surface and adopter inventory, with an exact baseline, source pins and search record.`
- `openspec/changes/e2e-required-check-evidence/proposal.md:19` — `Independent INVENTORY PASS precedes options and design. Independent design review and owner acceptance precede`
- `openspec/changes/e2e-required-check-evidence/proposal.md:27` — `#1117 retains semantic CI; #769/#1128 retain agentic/CRUD CI and persona proof; #1293 retains broader verification`
- `openspec/changes/e2e-required-check-evidence/proposal.md:33` — `At claim preparation, #1402/#1403 own agentic recovery, and #1404 owns #1188 config namespacing. The #1188 worktree`
- `openspec/changes/e2e-required-check-evidence/proposal.md:34` — `has uncommitted edits in test/e2e/scenarios/agentic/scenario.go, test/e2e/config/tier_authority.go and its tests,`
- `openspec/changes/e2e-required-check-evidence/proposal.md:37` — `This claim starts with inventory/design only. Do not edit those shared scenario/authority files, runtime recovery,`

- `openspec/changes/e2e-required-check-evidence/tasks.md:5` — `- [ ] Produce the surface inventory at the exact baseline, including existing selectors, outcomes, writers,`
- `openspec/changes/e2e-required-check-evidence/tasks.md:7` — `- [ ] Produce the adopter seam inventory for scenario authors, runner/CI users and evidence consumers.`
- `openspec/changes/e2e-required-check-evidence/tasks.md:8` — `- [ ] Obtain independent INVENTORY PASS and preserve the reviewed checkpoint.`
- `openspec/changes/e2e-required-check-evidence/tasks.md:9` — `- [ ] After inventory review, frame bounded options and costs, draft the design and state its invariants.`
- `openspec/changes/e2e-required-check-evidence/tasks.md:10` — `- [ ] Obtain independent design review and explicit owner acceptance before implementation or spec deltas.`
- `openspec/changes/e2e-required-check-evidence/tasks.md:23` — `this claim alone. The delta-less initial claim may fail strict OpenSpec validation as described in the shared`
- `openspec/changes/e2e-required-check-evidence/tasks.md:24` — `protocol; that is not a waiver for any other check and is not a merge-ready state.`

- `openspec/specs/release-candidate-proof/spec.md:9` — `Every retained advertised deterministic path SHALL have a green exact-candidate result before tag authorization.`
- `openspec/specs/release-candidate-proof/spec.md:18` — ``openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL record owner, decision`
- `openspec/specs/release-candidate-proof/spec.md:19` — `date, disposition, and coverage/publication plan. It SHALL NOT predict the SHA of its containing commit. Exact`
- `openspec/specs/release-candidate-proof/spec.md:20` — `candidate identity, command results, timestamps, and evidence pointers SHALL live in the immutable`
- `openspec/specs/release-candidate-proof/spec.md:109` — `- **GIVEN** an independently reviewed candidate SHA`
- `openspec/specs/release-candidate-proof/spec.md:112` — `- **AND** the corrected candidate is selected, reproved, and independently reviewed`
- `openspec/specs/release-candidate-proof/spec.md:114` — `### Requirement: Candidate proof binds exact commands and active observation`
- `openspec/specs/release-candidate-proof/spec.md:141` — ``openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` translates to`
- `openspec/specs/release-candidate-proof/spec.md:142` — ``openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise`.`
- `openspec/specs/release-candidate-proof/spec.md:144` — `The candidate-proof record SHALL carry the manifest-command, disposition-ledger, and candidate-proof decision-`
- `openspec/specs/release-candidate-proof/spec.md:145` — `reference mappings. It SHALL also carry the exact archive-path manifest result, runner identity, UTC start/end,`
- `openspec/specs/release-candidate-proof/spec.md:146` — `exit/result, and log digest. The product-Release attestation SHALL carry only the post-publication decision-reference`
- `openspec/specs/release-candidate-proof/spec.md:147` — `mapping in its fresh-storage decision-reference field; it SHALL NOT duplicate the manifest-command, manifest-result,`
- `openspec/specs/release-candidate-proof/spec.md:148` — `manifest-provenance, manifest-log-digest, or disposition-ledger evidence. Neither record SHALL edit the archived`

- `.github/workflows/e2e-ladder.yml:11` — `# Tier choice (owner, 2026-07-23): run the `statistical` tier per-PR. It runs`
- `.github/workflows/e2e-ladder.yml:17` — `# NOT covered here (deliberately, for CI cost):`
- `.github/workflows/e2e-ladder.yml:26` — `#     Owner ruling (2026-08-27, #1117): wiring it here is PER-PR, not`
- `.github/workflows/e2e-ladder.yml:27` — `#     nightly — schedule-triggered runs are reserved for out-of-band security`
- `.github/workflows/e2e-ladder.yml:28` — `#     scanning (CVE and similar), never functional e2e. The open question is`
- `.github/workflows/e2e-ladder.yml:29` — `#     which assertions the per-PR gate carries (path vs quality), not`
- `.github/workflows/e2e-ladder.yml:33` — `pull_request:`
- `.github/workflows/e2e-ladder.yml:34` — `workflow_dispatch:`
- `.github/workflows/e2e-ladder.yml:71` — `- name: Run assembled slow-consumer attribution proof`
- `.github/workflows/e2e-ladder.yml:122` — `run: task e2e:statistical`

- `docs/contributing/02-e2e-tests.md:327` — `least one relevant E2E tier green BEFORE the breaking commit lands on main. Unit and integration tests do not`
- `docs/contributing/02-e2e-tests.md:329` — `can leave a sister binary half-migrated and silently break every flow that uses it.`
- `docs/contributing/02-e2e-tests.md:350` — `If only `cmd/e2e-semstreams` has it, the framework binary is half-migrated. Follow the`
- `docs/contributing/02-e2e-tests.md:351` — `[payload registration checklist](../../.agents/skills/new-payload/SKILL.md). The per-PR ladder does not yet run the`

- `docs/contributing/01-testing.md:161` — `1. The unmodified baseline passes the selected checks, and the intended tests actually execute.`
- `docs/contributing/01-testing.md:162` — `2. Apply a named, relevant mutation. The mutant builds and reaches the selected test.`
- `docs/contributing/01-testing.md:163` — `3. The relevant assertion fails because it observes the intended violation.`
- `docs/contributing/01-testing.md:164` — `4. Restore the original bytes, verify checksums, and rerun the selected checks successfully.`
- `docs/contributing/01-testing.md:191` — `- **Invalid:** the mutant could not build or was otherwise ineligible for this experiment.`
- `docs/contributing/01-testing.md:197` — `equivalence assessment names the applicable contract and input domain, its reasoning, and its reviewer; passing the`


External issue facts are supplied by root in `/private/tmp/semstreams-1222-external-evidence.json`; source pins do not convert these into repository facts. [#1222](https://github.com/C360Studio/semstreams/issues/1222) is beta.165 work package C of [#1134](https://github.com/C360Studio/semstreams/issues/1134), and explicitly owns selected required check/evidence honesty rather than every capability defect. [#1117](https://github.com/C360Studio/semstreams/issues/1117) owns beta.163 semantic path CI; [#769](https://github.com/C360Studio/semstreams/issues/769) and [#1128](https://github.com/C360Studio/semstreams/issues/1128) own rc.1 agentic/CRUD coverage and recurring CI; [#1293](https://github.com/C360Studio/semstreams/issues/1293) owns wider verification plumbing. [#1195](https://github.com/C360Studio/semstreams/issues/1195), [#1224](https://github.com/C360Studio/semstreams/issues/1224), and [#1288](https://github.com/C360Studio/semstreams/issues/1288) retain throughput/research-specific capability defects. No duplicate ownership or tier-per-PR rule is inferred.

Active PR1402/1403 own terminal/deferred-turn recovery; PR1404 owns #1188 config namespace. Proposal's uncommitted-file description is explicitly at claim preparation. Root later observed PR1404 worktree clean at 32781e35b7636d8f819a819ae65abe280ed78152, ahead six commits. Current ownership remains until landing and reconciliation; this report is not source verification of that separate checkout. No shared scenario/authority file was edited.

The general E2E guide calls the observer stack real services; agentic/research configurations also contain scripted dependencies. It is not legitimate to infer model quality or all capability behavior from that blanket prose. B0 thematic and partition co-location explicitly designate diagnostic output; changing their success thresholds is outside this inventory.

- `test/e2e/scenarios/validate_thematic_eval.go:19` — `// This stage is the measurement instrument for GraphRAG THEMATIC SYNTHESIS —`
- `test/e2e/scenarios/validate_thematic_eval.go:25` — `// grades the synthesized `answer` five DETERMINISTIC ways. NO LLM judge.`
- `test/e2e/scenarios/validate_thematic_eval.go:54` — `// B0 IS A RECORDER, NOT YET A HARD GATE — but this is a bootstrapping baseline,`
- `test/e2e/scenarios/validate_thematic_eval.go:58` — `// so B0 computes every dimension, writes the numbers into result.Metrics +`
- `test/e2e/scenarios/validate_thematic_eval.go:59` — `// result.Details, and prints them prominently — but does NOT turn them into`
- `test/e2e/scenarios/validate_thematic_eval.go:60` — `// result.Errors hard-fails.`

- `test/e2e/scenarios/validate_partition_colocation.go:46` — `// ─────────────────────────────────────────────────────────────────────────────`
- `test/e2e/scenarios/validate_partition_colocation.go:47` — `// THIS STAGE IS A RECORDER, NEVER A GATE. A bad co-location number is DATA, not a`
- `test/e2e/scenarios/validate_partition_colocation.go:48` — `// failure — it is exactly the B2 evidence this stage exists to produce. It`
- `test/e2e/scenarios/validate_partition_colocation.go:49` — `// computes every number, writes them into result.Metrics + result.Details, and`
- `test/e2e/scenarios/validate_partition_colocation.go:51` — `//`
- `test/e2e/scenarios/validate_partition_colocation.go:52` — `// The ONE thing it MAY hard-fail: if GetAllCommunities is unreachable (transport`

- `test/e2e/scenarios/research-graph/scenario.go:222` — `responder, err := natsClient.Client().SubscribeForRequests(`
- `test/e2e/scenarios/research-graph/scenario.go:225` — `newResearchEmbeddingSearchHandler(s.researchSeedEntityID),`
- `test/e2e/scenarios/research-graph/scenario.go:229` — `return fmt.Errorf("subscribe deterministic embedding search responder: %w", err)`


## 4. Present consumers at birth

No new exported surface is proposed. Present consumers are: scenario implementers returning Result; shared runner deciding process exit; task wrappers using process exit; CI jobs using task exit; terminal/log readers; structured and legacy comparison commands loading saved JSON; release owner/reviewer assembling detached exact-candidate evidence. Supplement enumerates the actual structural readers/callers; seed inventories task invocations and container identities. Lack of a present consumer for any future field remains a design question, not assumed permission.

- `cmd/e2e/main.go:87` — `exitCode := runScenarios(ctx, logger, edgeClient, flags)`
- `cmd/e2e/main.go:88` — `os.Exit(exitCode)`
- `cmd/e2e/main.go:334` — `return runScenario(ctx, logger, scenario, flags)`
- `cmd/e2e/main.go:547` — `filepath, err := scenarios.SaveStructuredResults(result.Structured, flags.outputDir)`
- `cmd/e2e/main.go:552` — `}`
- `cmd/e2e/main.go:559` — `metricsPath, err := saveMetricsDump(logger, flags.metricsURL, variant, flags.outputDir)`
- `cmd/e2e/main.go:739` — `writer := results.NewWriter(outputDir)`
- `cmd/e2e/main.go:760` — `}`

- `cmd/e2e/dispatch_coverage_test.go:51` — `func TestAdvertisedScenariosAreDispatchable(t *testing.T) {`
- `cmd/e2e/dispatch_coverage_test.go:91` — `func TestAdvertisedScenariosHaveARunner(t *testing.T) {`
- `cmd/e2e/dispatch_coverage_test.go:121` — `func TestOnlyExecutedTaskLinesCountAsRunners(t *testing.T) {`

- `cmd/e2e/main_test.go:47` — `{name: "success", result: &scenarios.Result{Success: true, AssertionsRun: 11}, wantOutput: "assertions_run=11"},`
- `cmd/e2e/main_test.go:48` — `{name: "partial failure", result: &scenarios.Result{AssertionsRun: 4}, err: errors.New("failed"),`
- `cmd/e2e/main_test.go:49` — `wantExit: 1, wantOutput: "assertions_run=4"},`
- `cmd/e2e/main_test.go:55` — `assertionReportingScenario{result: tc.result, err: tc.err}, &cliFlags{})`
- `cmd/e2e/main_test.go:57` — `assert.Contains(t, output.String(), tc.wantOutput)`
- `cmd/e2e/main_test.go:58` — `})`

- `test/e2e/results/writer.go:173` — `return nil, fmt.Errorf("reading results file: %w", err)`
- `test/e2e/results/writer.go:184` — `// CreateTestRun creates a new TestRun with computed summary`
- `test/e2e/results/writer.go:238` — `func Compare(baseline, current *TestRun) *Comparison {`
- `test/e2e/results/writer.go:444` — `func (w *Writer) ListRuns() ([]string, error) {`


## 5. Existing instances of the problem shape

Shape: selected obligations must be observed and classified before a caller emits a success signal; the output carries what was selected and what happened, including failure, without confusing diagnostics with required success. Existing instances on other planes:

Slow-consumer helper evaluates named conditions and increments at the assertion site; final expected count plus explicit condition failures exists today. It differs from successful-callback counting.
Tier/binary composition contract reads the normative table, rejects unknown gate tokens and compares declared versus armed gate sets in both directions, including empty effective values. It demonstrates declared-set completeness and fail-closed extraction, not executed behavior.
Dispatch coverage tests reject advertised-but-unrunnable names and distinguish executed task commands from documentary strings; they explicitly disclaim proof of runnable topology.
Integration shell runner captures go test's exit, emits latency evidence on both success and failure, then preserves failure status. This is existing failure-propagation/evidence-ordering behavior on a different test plane.
Release-candidate-proof specifies exact-candidate evidence, invalidation after source corrections, required retained paths and stop on red. It is an existing evidence authority rather than a new generic result mechanism.

The category2 extracted/live required-component and required-index owners are also direct instances of the shape: explicit selected names, observations and missing-set classification already exist, with hard-refusal versus warning semantics. The structured models cannot be described as missing. The live methods do not adopt those extracted structs, and their contracts differ (required component membership and nil-client behavior); inventory records the collision without selecting consolidation, removal or reuse. Adopter implication: scenario authors already have multiple concrete examples of required-set semantics, and a name such as Required alone does not establish which missing observation changes outer success. This extends the scenario-author/result-consumer debt identified in adopter-seams; no new adopter API or policy is proposed.

This section identifies existing shapes, not an adoption decision. No new runtime/durable/communication primitive is proposed; consequently no proposed-primitive collision table or establishing-pattern adoption sweep is triggered yet. Existing result catalogs/status/readers/writers are enumerated above; retention/replay/repair guarantees for run files are not found or asserted.

- `test/contract/e2e_tier_binary_contract_test.go:38` — `const tierTableHeader = "| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate |"`
- `test/contract/e2e_tier_binary_contract_test.go:161` — `// A gate token this test cannot classify is a defect in the row, not a`
- `test/contract/e2e_tier_binary_contract_test.go:173` — `t.Fatalf("%s: unclassified gate token %q in row %s", path, gate, row.tier)`
- `test/contract/e2e_tier_binary_contract_test.go:574` — `if got, want := strings.Join(sortedKeysOf(armed), ","), strings.Join(sorted(row.envGates), ","); got != want {`
- `test/contract/e2e_tier_binary_contract_test.go:581` — `if value, declared := armed[gate]; declared && value == "" {`
- `test/contract/e2e_tier_binary_contract_test.go:582` — `t.Errorf("%s: %s is declared with an empty effective value, which does not arm the hook", row, gate)`
- `test/contract/e2e_tier_binary_contract_test.go:660` — `t.Errorf("e2eboot.FromEnv reads [%s]; the Gate column of %s declares [%s]", got, specPath, want)`

- `cmd/e2e/dispatch_coverage_test.go:38` — `// separators), never by a fixed indentation depth or a comma-only split:`
- `cmd/e2e/dispatch_coverage_test.go:39` — `// review found this guard could be walked around by adding a menu entry at a`
- `cmd/e2e/dispatch_coverage_test.go:83` — `//  2. its dispatch case returns a constructor that runAllScenarios also`
- `cmd/e2e/dispatch_coverage_test.go:84` — `//     builds, so the `--scenario all` that `task e2e:core` runs covers it —`
- `cmd/e2e/dispatch_coverage_test.go:85` — `//     core-health and core-dataflow are reachable only this way.`
- `cmd/e2e/dispatch_coverage_test.go:86` — `//`

- `scripts/run-integration-tests.sh:312` — `packages=("$@")`
- `scripts/run-integration-tests.sh:313` — `if (( ${#packages[@]} == 0 )); then`
- `scripts/run-integration-tests.sh:314` — `packages=(./...)`
- `scripts/run-integration-tests.sh:319` — `# distribution to this file instead, and it is printed below on pass and on failure alike (#1284).`
- `scripts/run-integration-tests.sh:329` — `go test -race -failfast -tags=integration -timeout=20m -count=1 -p 2 "${packages[@]}"`
- `scripts/run-integration-tests.sh:330` — `status=$?`
- `scripts/run-integration-tests.sh:332` — `if [[ -s "$latency_log" ]]; then`
- `scripts/run-integration-tests.sh:334` — `cat "$latency_log"`
- `scripts/run-integration-tests.sh:341` — `echo "[INTEGRATION] tests failed with status $status" >&2`
- `scripts/run-integration-tests.sh:342` — `exit "$status"`

- `openspec/specs/release-candidate-proof/spec.md:27` — `- **WHEN** its exact-candidate proof is red`
- `openspec/specs/release-candidate-proof/spec.md:28` — `- **THEN** tag authorization is blocked`
- `openspec/specs/release-candidate-proof/spec.md:29` — `- **AND** wrapper silence or invocation shape does not convert the red result into success`
- `openspec/specs/release-candidate-proof/spec.md:77` — `candidate-proof Release.`


## Limits, refutations and handoff

No cost measurement, runtime outcome, branch protection or paid-run observation was made. Historical source/run claims remain at their own base. Not every nil-return diagnostic is a defect: explicit B0/B2 diagnostics are counterexamples to blanket failure conversion. Nonzero assertion totals do not prove identity or complete selected membership; zero totals do not prove no checks ran. Current full-task success is not evidence every available tier ran. Existing lower-level proof need not be copied wholesale into Docker.

Sister repositories were not scanned: no externally exported runtime API is proposed. An outward E2E/report consumer beyond the in-tree commands remains unknown, not declared absent. Artifact compatibility and retention expectations require owner review before design. Current CLI/Result paths and explicit adopted membership have not been measured on live services by this task.

Independent INVENTORY PASS is still pending. Materialize this file, adopter-seams, structural supplement, external adjacency record, and seed identity together before review. No options or implementation tasks are supplied.

## Search and read ledger

All commands below were read-only at the base unless the seed file path is explicit. Repeated raw source reads were bounded excerpts; complete contract/project/spec/change reads are named above. No gopls calls were duplicated; supplement owns structural query log.

1. `cat .agents/contracts/semstreams-architect.md`; `cat openspec/project.md`; `rg --files openspec/changes/e2e-required-check-evidence`; `git status --short`: two active files, clean tree.
2. `cat .agents/contracts/semstreams-reviewer.md`; complete active proposal/tasks and release-candidate-proof spec reads.
3. `cat /private/tmp/semstreams-e2e-survey-20260927/evidence/gate-inventory.md`: reused prior inventory; large response truncated, so its conclusions are not treated as independently read/verified merely by this read.
4. `git grep -n -E 'required.check|assertions_run|AssertionsRun|SaveStructuredResults|release.candidate.proof' -- openspec/specs docs/contributing test/e2e/scenarios/scenario.go cmd/e2e/main.go`: Result field, runner reporting and release spec.
5. `git grep -n -E 'AssertionsRun[[:space:]]*(<|>|==|!=)|WriteLatest\(|CreateTestRun\(' -- cmd/e2e`: zero. Independently confirms seed's runner-boundary zero, not repository-wide absence.
6. `git grep -n -E 'e2e|E2E' -- .github/workflows/ci.yml .github/workflows/release.yml`: zero; ladder is separate and was read.
7. `git grep -n -E 'schedule:' -- .github/workflows/e2e-ladder.yml`: zero; pull_request/workflow_dispatch present.
8. `git grep -n -E 'AssertionsRun|asserts[ :]|Skipped|skipped|Warning|Success|Threshold|threshold' -- test/e2e/scenarios/agentic/scenario.go test/e2e/scenarios/lessons/scenario.go test/e2e/scenarios/researchgraph/scenario.go test/e2e/scenarios/tiered.go test/e2e/scenarios/results_common_types.go test/e2e/results/writer.go`: researchgraph spelling absent; corrected research-graph reads below. Other output identified count/warning/outcome owners.
9. `git grep -n -E 'AssertionsRun|assertions_run|required.check|assertion.*skip|selected.*check' -- test/e2e cmd/e2e docs/contributing .agents/contracts`: Result, agentic/lessons/ops/slow-consumer and tests; no other count sites.
10. `git diff --name-only fe9482b7f336e575317cfb45fd1ad7c40baf7904 HEAD`: only active proposal/tasks.
11. `git grep -n -E 'scenario|variant|output-dir|exit-code|skip|SKIP' -- taskfiles/e2e/semantic.yml taskfiles/e2e/tiers.yml taskfiles/e2e/core.yml taskfiles/e2e/agentic.yml`: actual semantic task uses explicit tiered/variant; task composite is Taskfile.yml, not nonexistent tiers.yml.
12. First `git grep ... scripts/integration*` failed shell glob expansion before searching; no negative conclusion. Corrected tracked glob below.
13. `git grep -n -E 'skip|Skip|warn|Warn|fail|Fail|required|Required|pending|Pending|gate|Gate' -- test/e2e/scenarios/tiered.go test/e2e/scenarios/results.go test/contract/e2e_tier_binary_contract_test.go 'scripts/*integration*'`: diagnostic/hard-failure sites, integration status and contract extraction checks.
14. `git grep -n -E 'func .*validateSemanticRequirements|func .*validateFallbackBehavior|StageStatus|stage_status|Skipped|skipped|Pending|pending|SkipReason|skip_reason' -- test/e2e/scenarios | head -100`: bounded exploratory listing dominated by runtime pending-approval names; no completeness/absence conclusion from truncated list.
15. `git grep -n -E 'required|gate|exit|selected|package' -- scripts/run-integration-tests.sh | head -70`: selected package/default and exit evidence.
16. `git grep -n -E 'func .*validateSemanticRequirements|func .*validateFallbackBehavior|SKIP|SKIPPED|skipping|Skipped|skipped|WARN|warning' -- test/e2e/scenarios/validate_semantic.go test/e2e/scenarios/validate_thematic_eval.go test/e2e/scenarios/validate_partition_colocation.go test/e2e/scenarios/research* test/e2e/scenarios/deep*`: warning records; validate_semantic.go absent, corrected below.
17. `git grep -n -E 'func .*BuildTieredResults|func .*ListRuns|func .*handleAnalyzeComparison|func .*runTieredScenario|output-dir|scenario"|variant"' -- cmd/e2e/main.go test/e2e/results/writer.go test/e2e/scenarios/results.go`: textual locations for bounded reads; structural completeness deferred to gopls supplement.
18. `git grep -n -E 'func Test|gate|exit' -- scripts/e2e-statistical-up_fixture_test.py cmd/e2e/dispatch_coverage_test.go | head -45`: representative guard fixtures, not exhaustive test enumeration.
19. `rg --files openspec/changes | rg 'e2e|required|proof|verification'`: active claim plus historical evidence; withdrawn delta read fully.
20. `cat openspec/specs/e2e-tiers/spec.md`: absent. `git grep -n -E 'func .*validateSemanticRequirements|func .*validateFallbackBehavior' -- test/e2e/scenarios`: located validate_search.go443/477, read440–555.
21. Bounded reads: scenario.go1–115; main.go120–150,280–355,390–470,500–795; writer.go1–180,180–290,430–465; results_common_types1–125; results.go425–468,735–795; tiered.go95–145,305–470,530–615; agentic196–227,1059–1115; core_slow_consumer175–230; main_test1–100; dispatch_coverage1–145; workflow1–170; Taskfile150–225; testing guide135–240; E2E guide1–90,305–365; integration runner308–349; validate_search18–92,440–555; core_dataflow220–315; research-graph155–250; thematic1–65; partition1–55. Evidence pins below were generated from exact base bytes, not copied line numbers from prior agents.
22. Root external evidence JSON read for issue1222/1117 scope; root owns complete fresh adjacency; no independent network query here.

23. Bounded shell reads: `sed -n 80,120p taskfiles/e2e/core.yml`, `155,218p`, `235,261p`; `sed -n 1,80p taskfiles/e2e/lifecycle.yml`; structural supplement1–36. These close the shell-obligation omission rather than inferring all checks live in Scenario.

24. Canonical verification first reported 299/299 exact pins but rejected 12 narrative bullet lines as UNPARSED; converted those prose bullets to paragraphs without changing content. Empty source-line pins were removed before verification. No source was changed.

## Inventory-review correction search record

Correction changes only this focused inventory after independent review found omitted same-fact owners. Previous checkpoint identity and review verdict remain in root's review history; no INVENTORY PASS is inferred. The adopter file remains byte-identical; the relevant implication is recorded in category5 above.

25. Read `stages/components.go`1–245 and `stages/indexes.go`1–255 (both shorter than requested range); `command -v gopls` resolved `/Users/coby/go/bin/gopls`.
26. Single `gopls references` calls and returned location counts: components.go12:6 =>3 (receiver sites18/53/84);18:29 =>0 (VerifyComponents);76:6 =>2 (return type18/constructor38);78:2 =>1 (Required writer40);79:2 =>1 (Found writer41);80:2 =>1 (Missing writer42). indexes.go16:6 =>3 (23/24/54);54:25 =>0 (VerifyIndexPopulation);45:6 =>2 (54/59);19:2 =>2 (required reads73/89);48:2 =>6 (read/write occurrences74/90/96/98);23:6 =>0 (DefaultIndexSpecs). Additional live method calls validate_infra.go20:26 and validate_structural.go14:26 each returned0. Every query reported `initial workspace load failed` with Go build-cache `operation not permitted` and packages=0; exit0 and partial results do not establish complete callers. No escalation, cache writes outside the permitted tool behavior, or repeated gopls query was requested.
27. First attempted `git grep -n -E '\b(ComponentVerifier|ComponentResult|IndexVerifier|IndexSpec|IndexPopulationResult|DefaultIndexSpecs|VerifyIndexPopulation|VerifyComponents|EmptyRequired)\b' -- '*.go'` returned zero; this word-boundary form failed to find known declarations, so it is invalid absence evidence. Corrected plain-alternation query returned48 lines (repeated once via Python subprocess to measure the count); its substring collisions include ComponentResults and executeVerifyComponents. Final bounded fallback `git grep -n -w -E 'ComponentVerifier|ComponentResult|IndexVerifier|IndexSpec|IndexPopulationResult|DefaultIndexSpecs|VerifyIndexPopulation|VerifyComponents|EmptyRequired' -- '*.go'` returned33 lines, all in stages/components.go, stages/indexes.go and stages/indexes_test.go.
28. `git grep -n -F 'test/e2e/scenarios/stages' -- '*.go'` returned0 lines, exit1 (repeated once when measuring exact counts). This closes tracked Go import wiring only, not intended purpose or external use.
29. `git grep -n -E 'empty_required|required_indexes|Required indexes empty|executeVerifyIndexPopulation|executeVerifyComponents' -- test/e2e/scenarios` returned14 lines: extracted owner/tests, actual tier schedule, core component verifier and live tier validators. `git grep -n -E '"required"|"missing,omitempty"|"empty_required,omitempty"|"all_healthy"' -- test/e2e/scenarios/stages` returned7 lines, including adjacent entities.go Missing. Counts verified by Python subprocess splitting stdout lines, not inferred from truncated display.
30. First attempted read `validate_indexes.go` failed because path absent; corrected via tracked search to validate_structural.go1–100. Read validate_infra.go1–155 and stages/indexes_test.go1–85. No runtime tests executed. Existing pins regenerated from source bytes and empty entries excluded.
