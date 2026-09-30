# Independent #1222 inventory review

Mode: INVENTORY review, independently enumerated before receiving architect/explorer handoff. No design recommendation or verdict yet.
Base: `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`.
Repository: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Read-only source review; no tests, Docker, source edits, CI, or GitHub writes. Only this review file is written.

## Independent enumeration

### 1. Claimed gap and 2. Existing fact spellings

- `test/e2e/scenarios/scenario.go:10` — Scenario owns Setup/Execute/Teardown. Execute returns `(*Result,error)`.
- `test/e2e/scenarios/scenario.go:31` — Result independently carries Success, Error, Errors, Warnings, Metrics, Details, AssertionsRun, Structured. There is no named required-check set or skipped-check record in this struct.
- `test/e2e/scenarios/agentic/scenario.go:236` — agenticStage declares asserts; capture-baseline/injection are intentionally uncounted; stage success increments at321 and denominator checks at329.
- `test/e2e/scenarios/agentic/scenario.go:1088` — streaming errors/zero chunks warning-return nil while the stage is asserts=true at257; final validateResults at1177 checks selected detail outcomes but not streaming_verified. Count is not independent execution evidence for that observation.
- `test/e2e/scenarios/agentic/scenario.go:1177` — existing positive counterexample: final checks read completion/approval/signal/refusal/restart details, resisting a nil-return skipped lane. Preserve the distinction from blanket claims that agentic lacks meaningful assertions.
- `test/e2e/scenarios/core_slow_consumer.go:211` — requireSlowConsumer increments BEFORE checking the boolean: attempted assertion count includes the failed assertion. Final expected-count check at207. This differs semantically from successful-stage counts in other scenarios.
- `test/e2e/scenarios/ops/scenario.go:250` — successful stage returns increment AssertionsRun at257.
- `test/e2e/scenarios/lessons/scenario.go:258` — successful complete behavior stages increment at263.
- Other Scenario implementation files discovered: core_dataflow.go, core_health.go, graph_roundtrip_scenario.go, platform_identity.go (multiple types), tiered.go, and crud-tools/deep-research/lifecycle/research-graph/throughput scenario.go. These do not appear among assertion-increment sites.
- `test/e2e/scenarios/tiered.go:403` — stage membership filters variant strings; common stages have empty variants. `:443` treats nil return as completed, records duration, and fails on errors. `:567` marks success and builds Structured at570 before final semantic/fallback validation at572/578 can change Result.Success to false. These two success fields can diverge in memory on final validation failure; current runner returns before persisting either failure.
- `test/e2e/scenarios/validate_search.go:20` — executeVerifySearchQuality always returns nil after recording search stats; known-answer failures become warnings at375. Final semantic guard at477 applies only semantic; total>0 && passed==0 fails at508. Semantic also has independent global known-answer and batch reconciliation stages in tiered.go:383/389. Statistical warning is not proof all semantic failure paths pass.
- `test/e2e/scenarios/core_dataflow.go:265` — file count error takes component-only fallback; minimum count is hard at274; content errors become warnings at282. Do not call all missing-output paths false-green.
- `test/e2e/scenarios/stages/indexes.go:19` — IndexSpec.Required, EmptyRequired, Warnings are additional spellings; required-empty becomes warning, not a general required-check gate.
- `test/e2e/scenarios/stages/components.go:76` — ComponentResult carries Required/Found/Missing; verifier enforces component presence. This already represents required membership versus observation in one narrow capability.
- `test/e2e/scenarios/results.go:437` — BuildTieredResults copies Success, Errors, Warnings and derives typed fields, with absent map values becoming zero/empty through extraction helpers.
- `test/e2e/scenarios/results_common_types.go:40` — TestMetadata holds variant/times/success/errors/warnings/version; stage timing is a separate map at36.
- `test/e2e/scenarios/results.go:738` — SaveStructuredResults writes typed tier JSON; :765 reads it; :781 writes raw metrics.
- `test/e2e/results/writer.go:19` — TestRun contains Config, []Result, Summary, Environment. :185 CreateTestRun computes summary; :208 summary trusts per-result Success; :227 empty results yields AllPassed=true. :105 WriteRun/:132 WriteLatest persist different JSON from TieredResults; :170 LoadRun and :238 Compare consume it.
- Search + gopls references found no caller of CreateTestRun; WriteLatest only reaches WriteRun. General TestRun writer is present but not the currently wired scenario-run persistence path. Do not infer undesired/deletable from absence of callers.

### Selection, propagation, writer consumers and defaults

- `cmd/e2e/main.go:128` — empty scenario default; output-dir defaults empty at138 (no output).
- `cmd/e2e/main.go:314` — empty/all -> core bundle; semantic -> special bundle at317; rules -> structural bundle at320. Unknown ordinary name fails at327.
- `cmd/e2e/main.go:347` — ordinary dispatcher owns aliases and tier/config construction; :395 semantic case is bypassed by runScenarios' earlier semantic branch.
- `cmd/e2e/main.go:601` — all selects core health + dataflow only, intentionally excluding e2e-only graph type; :627 passes empty flags. Semantic/rules bundles likewise pass empty flags at667/708, dropping caller output-dir and other flags from runScenario.
- `cmd/e2e/main.go:650` — semantic bundle constructs DefaultTieredConfig without selecting semantic. `tiered.go:113` default variant empty; :539 auto-detection; :619 defaults structural and caches it at666; missing metrics leaves structural at660. Thus bare --scenario semantic is auto-detected, not explicit semantic selection. Normal task semantic uses --scenario tiered --variant semantic (taskfiles/e2e/semantic.yml:21), so this does not establish the normal semantic task takes the wrong branch.
- `cmd/e2e/main.go:510` — setup error returns1 before teardown; execute error/Success=false return1; teardown errors only warn at523; only successful result.Structured + output-dir persists at546; write errors warn at549; all other scenarios only log. main exits with returned code at88.
- `cmd/e2e/main.go:731` and :848 — legacy TestRun comparisons consume separate result files; cmd/e2e/compare.go:28/:35 consume TieredResults. Comparison commands can return0 after reporting regressions; they are reports, not execution gates.
- `Taskfile.yml:53` — named task includes for core/slow-consumer/structural/statistical/semantic/agentic/lessons/research/deep-research/crud/ops/lifecycle/throughput/openai-responses. OpenAI responses is a separate live adapter entry; not a fourteenth Compose family.
- `Taskfile.yml:165` — tiers runs three inference tiers then comparison; :200 tier dispatch shell expansion; :209 all invokes core + tiers + agentic. These are not CLI all's membership and not release gate membership.
- `taskfiles/e2e/core.yml:90` — CLI all is only one substep: minted-authority, normal SIGTERM, early-boot SIGTERM, preidentity refusal, graph roundtrip additionally live in the task. Scenario-only census misses required shell behavior.
- `taskfiles/e2e/crud-tools.yml:31` (and lifecycle/ops/deep-research/throughput variants) — explicit rc capture, teardown, exit rc; historical ignore-error failure is repaired here.
- Task commands often cd cmd/e2e then pass relative ./test/e2e/results; resolve artifact location relative to execution cwd, not repo-root spelling.
- `.github/workflows/e2e-ladder.yml:44` and :74 — slow-consumer/statistical jobs, run at72/122. No artifact upload found in this workflow. Local output persistence does not imply hosted retention. Branch protection is external and not inferred.
- `cmd/e2e/dispatch_coverage_test.go:51` — advertised names -> dispatch AST with non-vacuity guard; :91 -> task invocation or core-bundle constructor. Comments explicitly disclaim proof topology works. This does not traverse semantic special-dispatch behavior.

### 3. Adjacent claims and contracts

- Claim proposal/tasks read in full; only inventory/design authorized, no target API or current-spec delta. Proposal baseline fe9482b7, claim HEAD12ae6333; distinction intentional.
- Current issue/PR evidence supplied independently in `/private/tmp/semstreams-1222-external-evidence.json`, baseline12ae6333. #1222 owns common required-evidence pattern, #1117 semantic CI163; #769/#1128 paired agentic/CRUD CI/persona rc1; #1293 common verification plumbing rc1; #1195 throughput; #1224 direct research; #1288 actual research consumer; #1134 cap3/packageC; #1176 preflight.
- Claim #1406 is this design claim. #1402/#1403 own recovery, #1404 config namespacing and pending overlapping scenario/authority files. No permission to alter those files inferred.
- `openspec/specs/release-candidate-proof/spec.md:9` — exact candidate retained paths, nonzero wrapper/test red. :114 commands/runner/times/result/log digest; :155 direct+execute research; :156 distinct CRUD assertions; :161 unavailable/missing/incorrect metric must fail. :217 tag authority requires all gates green. Existing release-level evidence is separate from local JSON writers.
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:107` — RC-3 names guard against zero assertions; #1222's reconciliation must address semantic mismatch, not treat count as already authoritative.
- `docs/contributing/01-testing.md:115` — inspect activation/empty/early/skipped paths; :122 labels evidence; :129 mutation trigger; :214 observation versus judgment. These are existing documentation owners.
- No current e2e-tiers spec found by tracked-file/spec grep; archived semantic-tier-split delta is not silently current truth.

### 4. Consumer at birth / adopter seams

No new exported API, result type, CLI, bucket or config field proposed at this checkpoint. Present consumers: Scenario implementers; CLI users of default/all/specific aliases; task users; CI job status consumers; structured/legacy comparison commands; human reviewers and release owners consuming immutable exact-candidate evidence. Their default behavior and differing scope are listed above. No production durable state or communication primitive introduced by the claim.

### 5. Same problem shape elsewhere

Shape: declared required membership must match observed evidence; errors and diagnostics must not derive the same acceptance status.
- `composition/findings.go:40` separates error/warning severity; :69 owns severity classification; :114 derives final status from classified findings rather than caller-set booleans. `composition/validate.go:19` is a present aggregation owner. This is a structural analogue, not a proposed dependency or design choice.
- `test/e2e/scenarios/agentic/scenario.go:1177` validates retained per-lane evidence beyond successful stage calls; `core_slow_consumer.go:211` counts actual boolean attempts; `stages/components.go:76` represents required/missing membership. These are nearer same-purpose patterns already present.
- `cmd/e2e/dispatch_coverage_test.go:51` and release-candidate-proof:114 own advertisement-to-selection and exact-selection-to-evidence obligations on different planes.

## Search and structural query record

Executed independently: git rev-parse HEAD; rg --files openspec/changes matching required/1222/e2e; full project/proposal/tasks reads; git grep AssertionsRun/type Result/type TestRun/WriteResult in test/e2e/cmd; grep all functions/result writers/flag registrations; Taskfile/taskfiles/workflow e2e/output/exit/ignore_error searches; code-only Required/Skipped/Passed/Failed/Warnings searches; specs and documentation claim searches; ranges at pins above.

Structural queries:
- `gopls implementation test/e2e/scenarios/scenario.go:10:6` -> empty.
- `gopls implementation test/e2e/scenarios/scenario.go:23:2` -> empty (retried once after a tool session output was not captured).
- `gopls references -d test/e2e/results/writer.go:185:6` -> declaration only.
- `gopls call_hierarchy cmd/e2e/main.go:510:6` -> runScenarios/runAllScenarios/runSemanticScenarios/runRulesScenarios callers; assertionsRun/saveMetricsDump/log methods callees.
- `gopls workspace_symbol 'Scenario|TestRun|Stage|ValidationResult'` -> empty; plain `TestRun` -> empty. This structural-index limitation is not evidence that types/implementers are absent. Supplemented by `git grep '^func .* (Name|Description|Setup|Execute|Teardown)\(' -- test/e2e/scenarios`, yielding fourteen files including multiple platform identity types.

Search errors retained: shell expansion of pkg/release*, scripts/check-e2e*.sh and scripts/check-push* failed before grep; rerun using existing directories/no glob. `stages/stage.go` does not exist; rg --files stages located actual helpers. A broad documentation grep incidentally returned historical inventory snippets; no prior survey/architect/explorer artifact was opened or used as enumeration authority.

## Pending before verdict

Await exact line-addressable inventory and adopter artifacts with hashes; verify their baseline/checkpoint identity and compare against independent surfaces. No INVENTORY PASS yet. No current target design is recommended. No runtime or CI timing claims were measured.

## Materialized checkpoint review — 2026-09-27

Verdict: **INVENTORY CHANGES REQUESTED**. No target design reviewed or recommended.

Reviewed immutable identity:
- Base `12ae633381b8b8b26c333efe5f5c8691cfa47fb4` (rechecked).
- Manifest `evidence/inventory-manifest.sha256`: `66acba2b5c854b22c7b8e0be92c6f8ee6edaf710230e2e3859081eb9574a88ec`.
- inventory.md: `00f406b4a587d1e7673aadfb7b0072c006c3ce80163abd92934dd87a53421759`.
- adopter-seams.md: `7ee563b9206ff1fb0f50f2e6c504ddf940c1506a2d0ac1e0a7cae50cec4fb193`.
- evidence/gate-inventory.md: `98e1dd2a5c3a06e6778f293e38deeecd0faf54a2faf6b9ed826345fb1af576f8`.
- evidence/structural-inventory.md: `e5f320a56de698d278c39c7de51dec16fda96ffc61fcc3a46a691312764765b1`.
- evidence/adjacent-claims.json: `a450973c3544d82a8f63bcad4f14abc551e21082ade23f36eaa4cce1ce6f5bbf`.

Ran `shasum -a 256 -c evidence/inventory-manifest.sha256`: all five files OK. Source HEAD unchanged; status contains only the expected untracked inventory/adopter/evidence files. Parent supplied canonical pin verification (299/108/571/360); I did not reinterpret that as behavioral verification.

### BLOCKING openspec/changes/e2e-required-check-evidence/inventory.md:95 — Existing required-membership result owners omitted

Mechanism: category 2 inventories current spellings of selected requirement/outcome facts, but all four supplied Markdown artifacts omit the existing extracted stage helpers:

- `test/e2e/scenarios/stages/components.go:76` — ComponentResult already represents Variant/Required/Found/Missing. VerifyComponents at18 builds this result and returns an error for missing membership at45–46. getRequiredComponents at53 owns variant-specific expected sets.
- `test/e2e/scenarios/stages/indexes.go:16` — IndexSpec has Required; DefaultIndexSpecs at23 distinguishes required/optional sets. IndexPopulationResult at45 carries Total/Populated/EmptyRequired/Indexes/Warnings. VerifyIndexPopulation at54 records observation errors and missing required indexes, but returns them as warnings/nil at96–101.

These are current representations of the very fact under inventory, not merely analogous production validators. They must be visible before any shared result representation or required-check classification is designed.

Smallest inventory correction: add these existing types, set owners, observation/classification behavior, and current caller status with exact pins and searches. Do not implement or choose a target design. Label the no-caller result narrowly; do not infer that the helpers are undesired/deletable.

Verification/refutation: `rg -n 'stages/(components|indexes)|EmptyRequired|ComponentResult|IndexPopulationResult|IndexSpec'` across inventory.md, adopter-seams.md, gate-inventory.md and structural-inventory.md found only plural ComponentResults in the older typed TieredResults output. Repository-wide tracked `git grep -n -E 'ComponentVerifier|ComponentResult|IndexVerifier|IndexPopulationResult|DefaultIndexSpecs|\.VerifyIndexPopulation\(' -- '*.go'` finds the helper declarations/internal construction and index tests; no external execution callers. This refutes a claim of a currently exercised outer-run false-green at these helpers, but not the inventory omission. `gopls references -d` on VerifyComponents/VerifyIndexPopulation returned declaration only while explicitly failing workspace load because the sandbox denied a Go build-cache path; these reference results do not prove completeness. The textual search is the recorded supplement.

### Covered / not blockers

The package correctly captures the independent special semantic dispatch, dropped bundle flags, shell checks outside Scenario, different count semantics, distinct typed/legacy writers and consumers, failure-before-save order, required-versus-diagnostic examples, and live ownership boundaries. Its same-shape section provides multiple relevant existing instances (integration exit/evidence ordering, declaration gate sets, dispatch reachability). My independently found composition validator analogue need not become a forced dependency or an additional inventory blocker. No sister-repo census is required merely for this internal inventory; external report compatibility remains explicitly unknown. No runtime, paid, Docker or CI claims were made.

Recheck is bounded to the missing helper ownership/caller enumeration, any corresponding closure wording, and the new manifest identity. The current checkpoint cannot receive INVENTORY PASS until that omission is materialized.
