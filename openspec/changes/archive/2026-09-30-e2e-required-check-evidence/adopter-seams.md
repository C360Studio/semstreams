# Required-check evidence adopter seams

base: 12ae633381b8b8b26c333efe5f5c8691cfa47fb4

Inventory only for #1222 / draft PR1406. No target API or migration is proposed. These are current obligations and observed default paths, not design choices. Source/search provenance is inventory.md and its retained seed/structural supplement; no new searches or runtime operations were performed for this artifact.

## Scenario author

The author must implement Setup/Execute/Teardown, decide which callback failures are fatal, set Success independently of Errors/Warnings, decide what to count, and optionally populate Structured for the shared writer. Interface conformance alone does not enforce these relationships. Agentic uses successful callback completion; slow-consumer uses evaluated conditions; many scenarios never set the count. To get run JSON through the shared path the author must know that only nonnil Structured is written, only after success and only with output-dir.

Do-nothing path: return a Result with Success true and zero AssertionsRun, without Structured. The common runner logs success and zero, returns zero and writes no result JSON even if output-dir was supplied. A default Result without Success true fails; a nil Result with nil error is dereferenced. Returning an error propagates nonzero, but failure is returned before structured saving. Teardown errors merely warn. These are source paths, not runtime reproductions.

Discovery ranks: method signatures at compile time; returned error/Success failure as runtime exit; count and teardown at logs; persistence condition through implementation. No compile constraint names missing selected behavioral checks. The debt is knowing which result fields are truth-bearing and which are only recorded.

The author can reasonably own capability-specific expected outcomes and fixture identities. The framework already owns common execution/exit/serialization. Whether/how that division changes is deferred to design after inventory review.

- `test/e2e/scenarios/scenario.go:19` — `Setup(ctx context.Context) error`
- `test/e2e/scenarios/scenario.go:23` — `Execute(ctx context.Context) (*Result, error)`
- `test/e2e/scenarios/scenario.go:27` — `Teardown(ctx context.Context) error`
- `test/e2e/scenarios/scenario.go:39` — `Success bool   `json:"success"``
- `test/e2e/scenarios/scenario.go:45` — `Errors   []string       `json:"errors,omitempty"``
- `test/e2e/scenarios/scenario.go:46` — `Warnings []string       `json:"warnings,omitempty"``
- `test/e2e/scenarios/scenario.go:48` — `// AssertionsRun is the number of assertions the scenario actually executed.`
- `test/e2e/scenarios/scenario.go:49` — `AssertionsRun int `json:"assertions_run,omitempty"``
- `test/e2e/scenarios/scenario.go:53` — `Structured *TieredResults `json:"structured,omitempty"``
- `cmd/e2e/main.go:513` — `if err := scenario.Setup(ctx); err != nil {`
- `cmd/e2e/main.go:519` — `result, err := scenario.Execute(ctx)`
- `cmd/e2e/main.go:523` — `if teardownErr := scenario.Teardown(ctx); teardownErr != nil {`
- `cmd/e2e/main.go:527` — `if err != nil {`
- `cmd/e2e/main.go:532` — `if !result.Success {`
- `cmd/e2e/main.go:543` — `"assertions_run", result.AssertionsRun)`
- `cmd/e2e/main.go:546` — `if flags.outputDir != "" && result.Structured != nil {`
- `cmd/e2e/main.go:549` — `logger.Warn("Failed to save structured results", "error", err)`
- `cmd/e2e/main.go:565` — `}`
- `test/e2e/scenarios/agentic/scenario.go:207` — `// the runner's assertions_run= line therefore reads as "one verification stage`
- `test/e2e/scenarios/agentic/scenario.go:320` — `if stage.asserts {`
- `test/e2e/scenarios/agentic/scenario.go:321` — `result.AssertionsRun++`
- `test/e2e/scenarios/agentic/scenario.go:1068` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not verify tool executions: %v", err))`
- `test/e2e/scenarios/agentic/scenario.go:1069` — `return nil // Non-fatal - metrics may not be available`
- `test/e2e/scenarios/core_slow_consumer.go:212` — `result.AssertionsRun++`
- `test/e2e/scenarios/core_slow_consumer.go:213` — `if !condition {`

## CLI, task and CI user

The user must distinguish task e2e:all, CLI --scenario all, named scenario aliases, explicit --scenario tiered --variant selection, and CI's actual jobs. Task e2e:all selects core, three inference tiers and agentic; CLI all selects two core Scenario constructors and does not contain the core task's shell and fixture phases. Special semantic/rules bundles bypass normal flag application; all three bundles pass empty flags to the single-scenario runner. Task semantic uses explicit tiered/variant and is not this alias.

Do-nothing path: CLI's empty scenario enters all/core. Empty output-dir intentionally means no output. Supplying output-dir does not force artifacts for non-Structured scenarios and is lost entirely in the special bundles. Unknown scenario refuses with exit one. Task core also evaluates shell checks beyond Result. CI ladder currently selects statistical and slow-consumer; task availability or naming alone does not make another tier required or executed. An outer green is a result for the path actually selected.

Discovery ranks: unknown names receive runtime error; stages/outer exit and shell refusal messages are visible; menu/task descriptions list choices; exact composition requires task/dispatch inspection. More knowledge is needed than the apparent scenario/suite name supplies. Existing dispatch tests guard menu-to-runner reachability but explicitly do not prove actual topology or all selected observations.

Users can reasonably choose a scope and command. The runner/task declarations already own what a chosen scope expands to. Existing issue1222 asks for truthful membership; no membership is chosen in this artifact. #1117 and #769 retain CI scheduling authority.

- `cmd/e2e/main.go:128` — `flag.StringVar(&flags.scenarioName, "scenario", "",`
- `cmd/e2e/main.go:129` — `"Run specific scenario (core-health, core-dataflow, core-graph-roundtrip, lessons, or 'all')")`
- `cmd/e2e/main.go:138` — `flag.StringVar(&flags.outputDir, "output-dir", "",`
- `cmd/e2e/main.go:139` — `"Directory for saving results JSON (empty=no output)")`
- `cmd/e2e/main.go:314` — `if flags.scenarioName == "" || flags.scenarioName == "all" {`
- `cmd/e2e/main.go:316` — `return runAllScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:317` — `} else if flags.scenarioName == "semantic" {`
- `cmd/e2e/main.go:319` — `return runSemanticScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:320` — `} else if flags.scenarioName == "rules" {`
- `cmd/e2e/main.go:322` — `return runRulesScenarios(ctx, logger, edgeClient, flags.udpEndpoint)`
- `cmd/e2e/main.go:327` — `if scenario == nil {`
- `cmd/e2e/main.go:330` — `return 1`
- `cmd/e2e/main.go:620` — `}`
- `cmd/e2e/main.go:627` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `cmd/e2e/main.go:659` — `scenarios.NewTieredScenario(obsClient, udpEndpoint, cfg),`
- `cmd/e2e/main.go:667` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `cmd/e2e/main.go:698` — `cfg.Variant = "structural"`
- `cmd/e2e/main.go:708` — `exitCode := runScenario(ctx, logger, scenario, &cliFlags{})`
- `Taskfile.yml:165` — `e2e:tiers:`
- `Taskfile.yml:174` — `- 'echo ""'`
- `Taskfile.yml:180` — `- sleep 3`
- `Taskfile.yml:186` — `VARIANT: statistical`
- `Taskfile.yml:209` — `e2e:all:`
- `Taskfile.yml:210` — `desc: Run all E2E tests (core -> inference tiers -> agentic)`
- `Taskfile.yml:212` — `- task: e2e:core`
- `Taskfile.yml:213` — `- task: e2e:tiers`
- `Taskfile.yml:214` — `- task: e2e:agentic`
- `taskfiles/e2e/semantic.yml:21` — `- cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/core.yml:90` — `- cd cmd/e2e && ./e2e --scenario all`
- `taskfiles/e2e/core.yml:102` — `[ "$exit_code" -eq 0 ] || fail "application exited with code $exit_code"`
- `taskfiles/e2e/core.yml:105` — `|| fail "shutdown-complete record missing"`
- `taskfiles/e2e/core.yml:243` — `[ "$exited" = "true" ] || fail_pre_identity "application did not exit within 20s of booting a pre-identity bucket"`
- `taskfiles/e2e/core.yml:247` — `[ "$exit_code" -ne 0 ] || fail_pre_identity "application booted a pre-identity bucket instead of refusing it"`
- `taskfiles/e2e/core.yml:252` — `(cd cmd/e2e && ./e2e --scenario core-pre-identity-assert) \`
- `.github/workflows/e2e-ladder.yml:33` — `pull_request:`
- `.github/workflows/e2e-ladder.yml:34` — `workflow_dispatch:`
- `.github/workflows/e2e-ladder.yml:71` — `- name: Run assembled slow-consumer attribution proof`
- `.github/workflows/e2e-ladder.yml:122` — `run: task e2e:statistical`
- `cmd/e2e/dispatch_coverage_test.go:83` — `//  2. its dispatch case returns a constructor that runAllScenarios also`
- `cmd/e2e/dispatch_coverage_test.go:84` — `//     builds, so the `--scenario all` that `task e2e:core` runs covers it —`
- `cmd/e2e/dispatch_coverage_test.go:85` — `//     core-health and core-dataflow are reachable only this way.`
- `cmd/e2e/dispatch_coverage_test.go:86` — `//`

## Result/evidence consumer

The consumer must distinguish ordinary logs, Result, TieredResults/TestMetadata JSON, legacy TestRun JSON and detached release-candidate proof. SaveStructuredResults writes variant-timestamp names; legacy ListRuns selects filenames containing e2e-results and loads TestRun. Aggregate Success/count/timing do not encode a single common named required set. BuildTieredResults copies success before later final semantic/fallback validation may change the outer Result; current runner returns before writing failed results. Save errors are warnings, so process success does not imply a saved artifact.

Do-nothing path: a consumer trusting outer zero or a count sees no explicit distinction between a skipped warning-check and a completed behavioral assertion in all current scenarios. A consumer expecting every output-dir invocation to emit run evidence may find no file. Generic release proof cannot be inferred from run JSON: its existing contract separately requires exact candidate SHA, command, runner, timestamps, exit and digest. Comparison is not release authorization.

Discovery ranks: file read/unmarshal failures are runtime errors; writer problems appear as logs; format meanings and detached proof requirements live in types/spec/docs. The live CLI shared writer and legacy TestRun writer are distinct; the zero WriteLatest/CreateTestRun search under cmd/e2e means the legacy wrapper is not called there, not that it is absent repository-wide. Structural supplement states remaining reference-tool limits.

Consumers can reasonably interpret named observed outcomes and retain release decisions at their owning seam. They currently carry format selection, source provenance assembly and selected-set interpretation themselves. Future field/format compatibility and retention policy remain unknown and require owner design review.

- `test/e2e/results/writer.go:19` — `type TestRun struct {`
- `test/e2e/results/writer.go:25` — `Scenarios   []scenarios.Result    `json:"scenarios"``
- `test/e2e/results/writer.go:28` — `Environment map[string]string     `json:"environment,omitempty"``
- `test/e2e/results/writer.go:32` — `type TestRunConfig struct {`
- `test/e2e/results/writer.go:35` — `Scenarios  []string `json:"scenarios"``
- `test/e2e/results/writer.go:105` — `func (w *Writer) WriteRun(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:112` — `filename := fmt.Sprintf("e2e-results-%s-%s.json",`
- `test/e2e/results/writer.go:132` — `func (w *Writer) WriteLatest(run *TestRun) (string, error) {`
- `test/e2e/results/writer.go:173` — `return nil, fmt.Errorf("reading results file: %w", err)`
- `test/e2e/results/writer.go:227` — `summary.AllPassed = summary.PassedScenarios == summary.TotalScenarios`
- `test/e2e/results/writer.go:444` — `func (w *Writer) ListRuns() ([]string, error) {`
- `test/e2e/results/writer.go:456` — `strings.Contains(entry.Name(), "e2e-results") {`
- `test/e2e/scenarios/results.go:437` — `func BuildTieredResults(result *Result, searchStats *search.Stats) *TieredResults {`
- `test/e2e/scenarios/results.go:447` — `Success:      result.Success,`
- `test/e2e/scenarios/results.go:450` — `Errors:       result.Errors,`
- `test/e2e/scenarios/results.go:451` — `Warnings:     result.Warnings,`
- `test/e2e/scenarios/results.go:738` — `func SaveStructuredResults(tr *TieredResults, outputDir string) (string, error) {`
- `test/e2e/scenarios/results.go:747` — `filename := fmt.Sprintf("%s-%s.json",`
- `test/e2e/scenarios/results.go:748` — `tr.Variant.Name,`
- `test/e2e/scenarios/tiered.go:567` — `result.Success = true`
- `test/e2e/scenarios/tiered.go:570` — `result.Structured = BuildTieredResults(result, s.searchStats)`
- `test/e2e/scenarios/tiered.go:572` — `if err := s.validateSemanticRequirements(result); err != nil {`
- `test/e2e/scenarios/tiered.go:573` — `result.Success = false`
- `test/e2e/scenarios/tiered.go:578` — `if err := s.validateFallbackBehavior(result); err != nil {`
- `test/e2e/scenarios/tiered.go:579` — `result.Success = false`
- `cmd/e2e/main.go:527` — `if err != nil {`
- `cmd/e2e/main.go:529` — `return 1`
- `cmd/e2e/main.go:532` — `if !result.Success {`
- `cmd/e2e/main.go:537` — `return 1`
- `cmd/e2e/main.go:546` — `if flags.outputDir != "" && result.Structured != nil {`
- `cmd/e2e/main.go:547` — `filepath, err := scenarios.SaveStructuredResults(result.Structured, flags.outputDir)`
- `cmd/e2e/main.go:549` — `logger.Warn("Failed to save structured results", "error", err)`
- `cmd/e2e/main.go:565` — `}`
- `cmd/e2e/main.go:742` — `files, err := writer.ListRuns()`
- `cmd/e2e/main.go:760` — `}`
- `openspec/specs/release-candidate-proof/spec.md:18` — ``openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL record owner, decision`
- `openspec/specs/release-candidate-proof/spec.md:19` — `date, disposition, and coverage/publication plan. It SHALL NOT predict the SHA of its containing commit. Exact`
- `openspec/specs/release-candidate-proof/spec.md:20` — `candidate identity, command results, timestamps, and evidence pointers SHALL live in the immutable`
- `openspec/specs/release-candidate-proof/spec.md:109` — `- **GIVEN** an independently reviewed candidate SHA`
- `openspec/specs/release-candidate-proof/spec.md:112` — `- **AND** the corrected candidate is selected, reproved, and independently reviewed`
- `openspec/specs/release-candidate-proof/spec.md:114` — `### Requirement: Candidate proof binds exact commands and active observation`

## Limits

No external downstream report consumer was identified or excluded by this bounded inspection; sister repos were not scanned because no runtime/exported change is proposed. No live suite, cost or persistence-failure experiment was run. The report identifies adopter debt, not an accepted target or an automatic demand to alter intentional diagnostic thresholds. Independent INVENTORY PASS remains pending.
