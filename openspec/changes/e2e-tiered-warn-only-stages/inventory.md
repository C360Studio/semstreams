# Inventory: e2e-tiered-warn-only-stages (issue #1426, task 1.1)
base: aaddc2920110e79cfa779664be4528deaee91adc

## Claimed gap

Per-path pins to the site(s) where the outcome the stage exists to detect becomes a warning (or a bare
`fmt.Println`) and the function still returns nil / no error. Every bullet below is one pin; explanatory
sentences are plain text, never bulleted.

### 1. `test-nl-path-intent` (variants: nil = all tiers)
- `test/e2e/scenarios/tiered_structural.go:1881` — `// Warn if no tests passed, but don't fail - this allows gradual rollout`
- `test/e2e/scenarios/tiered_structural.go:1883` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_structural.go:1884` — `"NL path intent tests returned no results - classifier routing may need attention")`

### 2. `test-nl-temporal-intent` (variants: statistical, semantic)
- `test/e2e/scenarios/tiered_structural.go:1974` — `// Warn if no tests passed`
- `test/e2e/scenarios/tiered_structural.go:1976` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_structural.go:1977` — `"NL temporal intent tests returned no results - temporal filtering may need attention")`

### 3. `test-graphrag-local` (variants: semantic)
- `test/e2e/scenarios/tiered_statistical.go:76` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not find entity in community: %v", err))`
- `test/e2e/scenarios/tiered_statistical.go:87` — `// GraphRAG local may fail if entity not in a community - warn but don't fail`
- `test/e2e/scenarios/tiered_statistical.go:88` — `result.Warnings = append(result.Warnings, fmt.Sprintf("GraphRAG local search failed: %v", err))`
- `test/e2e/scenarios/tiered_statistical.go:229` — `result.Warnings = append(result.Warnings, fmt.Sprintf(`

### 4. `test-graphrag-global` (variants: semantic)
- `test/e2e/scenarios/tiered_statistical.go:249` — `// GraphRAG global may fail if no communities exist - warn but don't fail`
- `test/e2e/scenarios/tiered_statistical.go:250` — `result.Warnings = append(result.Warnings, fmt.Sprintf("GraphRAG global search failed: %v", err))`

The same stage's result validator has three more warn-not-fail arms, not the one the issue cites.
- `test/e2e/scenarios/tiered_statistical.go:353` — `if communityCount < 2 {`
- `test/e2e/scenarios/tiered_statistical.go:354` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_statistical.go:366` — `if gs.Answer == "" && communityCount > 0 {`
- `test/e2e/scenarios/tiered_statistical.go:367` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_statistical.go:373` — `if cs.MemberCount == 0 {`
- `test/e2e/scenarios/tiered_statistical.go:374` — `result.Warnings = append(result.Warnings,`

### 5. `validate-anomaly-detection`, ground-truth arm (variants: statistical, semantic)
- `test/e2e/scenarios/tiered_semantic.go:635` — `if groundTruthResult != nil && !groundTruthResult.Passed() {`
- `test/e2e/scenarios/tiered_semantic.go:639` — `fmt.Sprintf("Anomaly ground truth violation [%s]: %s",`

The same stage has other warn-not-fail arms, not the ground-truth arm the issue names.
- `test/e2e/scenarios/tiered_semantic.go:585` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get anomaly counts: %v", err))`
- `test/e2e/scenarios/tiered_semantic.go:621` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_semantic.go:629` — `result.Warnings = append(result.Warnings,`

### 6. `validate-community-structure`, ground-truth arm (variants: statistical, semantic)
- `test/e2e/scenarios/tiered_statistical.go:479` — `if groundTruthResult != nil && !groundTruthResult.Passed() {`
- `test/e2e/scenarios/tiered_statistical.go:483` — `fmt.Sprintf("Community ground truth violation [%s]: %s - %s",`

The same stage DOES hard-fail a different outcome one branch earlier.
- `test/e2e/scenarios/tiered_statistical.go:474` — `return fmt.Errorf("no non-singleton communities found (%d total) - graph connectivity may be broken", totalCount)`

### 7. `validate-virtual-edges`, zero-edges arm (variants: semantic)
- `test/e2e/scenarios/tiered_semantic.go:762` — `if edgeCounts.Total == 0 && autoApplied == 0 {`
- `test/e2e/scenarios/tiered_semantic.go:764` — `fmt.Println("[VIRTUAL EDGES] No virtual edges created - this may be expected if no gaps met auto-apply threshold (similarity >= 0.85, distance >= 4)")`

This arm does not even reach `result.Warnings` — it is a bare console print, weaker than the other seven paths.
The same stage's count-retrieval failure is already a hard error, in contrast (see Problem shape below).
- `test/e2e/scenarios/tiered_semantic.go:737` — `return fmt.Errorf("failed to count virtual edges: %w", err)`

### 8. `validate-llm-enhancement`, zero-enhanced arm and quality-issues arm (variants: semantic)

Quality-issues arm:
- `test/e2e/scenarios/tiered_semantic.go:529` — `for _, issue := range issues {`
- `test/e2e/scenarios/tiered_semantic.go:531` — `fmt.Sprintf("LLM quality issue in %s: %s", issue.CommunityID, issue.Issue))`

Zero-enhanced arm:
- `test/e2e/scenarios/tiered_semantic.go:540` — `if stats.llmEnhancedCount == 0 {`
- `test/e2e/scenarios/tiered_semantic.go:543` — `fmt.Sprintf("LLM enhancement failed for all %d communities - check seminstruct logs", llmWait.failedCount))`
- `test/e2e/scenarios/tiered_semantic.go:546` — `fmt.Sprintf("No LLM enhancements completed within timeout (%d still pending) - enhancement may be slow or worker not started", llmWait.pendingCount))`
- `test/e2e/scenarios/tiered_semantic.go:549` — `"No communities have LLM enhancement (all show statistical status) - verify enhancement worker is enabled")`

The wait step itself also downgrades a real error to a warning:
- `test/e2e/scenarios/tiered_semantic.go:219` — `result.Warnings = append(result.Warnings, fmt.Sprintf("LLM enhancement wait error: %v", waitErr))`
- `test/e2e/scenarios/tiered_semantic.go:222` — `result.Warnings = append(result.Warnings,`

### Sweep-found: further stages in the tiered stage table with the same shape (not in the issue's eight)

Found while sweeping every `result.Warnings = append` site in the four core files for a stage whose core
assertion (not just a transport-error branch) is downgraded to warn-and-return-nil. Six qualify.
- `test/e2e/scenarios/tiered_structural.go:273` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_structural.go:1120` — `result.Warnings = append(result.Warnings, "Spatial query returned 0 entities - check if geo triples are being indexed")`
- `test/e2e/scenarios/tiered_structural.go:1228` — `result.Warnings = append(result.Warnings, "Temporal query returned 0 entities - check if temporal index is being populated")`
- `test/e2e/scenarios/tiered_structural.go:1487` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Zone %s has 0 incoming relationships - check if zone triples are being indexed", zoneEntityID))`
- `test/e2e/scenarios/tiered_structural.go:2101` — `if predicateCount == 0 {`
- `test/e2e/scenarios/tiered_structural.go:2103` — `"No predicates found - graph may be empty or PREDICATE_INDEX not populated")`
- `test/e2e/scenarios/tiered_structural.go:2128` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list predicates: %v", err))`

The last one (`executeTestPredicateStats`, `tiered_structural.go:2111`) downgrades even the pre-fetch transport
error and never asserts on `entityCount` at all.

## Spellings of the fact

Stage-table row + function declaration + client/deadline + metric/detail write sites, per path.

### 1. `test-nl-path-intent`
- `test/e2e/scenarios/tiered.go:334` — `{"test-nl-path-intent", s.executeTestNLPathIntent, nil},`
- `test/e2e/scenarios/tiered_structural.go:1799` — `func (s *TieredScenario) executeTestNLPathIntent(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1736` — `func (s *TieredScenario) sendNLQuery(ctx context.Context, query string) (*globalSearchResponse, time.Duration, error) {`
- `test/e2e/scenarios/tiered_structural.go:1738` — `httpClient := &http.Client{Timeout: 10 * time.Second}`
- `test/e2e/scenarios/tiered_structural.go:1871` — `result.Metrics["nl_path_intent_tests_passed"] = passedCount`
- `test/e2e/scenarios/tiered_structural.go:1872` — `result.Metrics["nl_path_intent_tests_total"] = len(testCases)`
- `test/e2e/scenarios/tiered_structural.go:1874` — `result.Details["nl_path_intent_test"] = map[string]any{`

### 2. `test-nl-temporal-intent`
- `test/e2e/scenarios/tiered.go:335` — `{"test-nl-temporal-intent", s.executeTestNLTemporalIntent, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_structural.go:1894` — `func (s *TieredScenario) executeTestNLTemporalIntent(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1964` — `result.Metrics["nl_temporal_intent_tests_passed"] = passedCount`
- `test/e2e/scenarios/tiered_structural.go:1965` — `result.Metrics["nl_temporal_intent_tests_total"] = len(testCases)`
- `test/e2e/scenarios/tiered_structural.go:1967` — `result.Details["nl_temporal_intent_test"] = map[string]any{`

This path shares `sendNLQuery` and its 10s client with path 1 (`tiered_structural.go:1736`/`:1738` above).

### 3. `test-graphrag-local`
- `test/e2e/scenarios/tiered.go:377` — `{"test-graphrag-local", s.executeTestGraphRAGLocal, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_statistical.go:69` — `func (s *TieredScenario) executeTestGraphRAGLocal(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:146` — `func (s *TieredScenario) sendGraphRAGLocalRequest(ctx context.Context, entityID, query, gatewayURL string) (*graphRAGLocalResponse, time.Duration, error) {`
- `test/e2e/scenarios/tiered_statistical.go:166` — `httpClient := &http.Client{Timeout: 10 * time.Second}`
- `test/e2e/scenarios/tiered_statistical.go:198` — `func (s *TieredScenario) validateGraphRAGLocalResult(resp *graphRAGLocalResponse, entityID, query string, latency time.Duration, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:80` — `result.Details["graphrag_local_discovered_entity"] = startEntity`
- `test/e2e/scenarios/tiered_statistical.go:92` — `result.Metrics["graphrag_local_latency_ms"] = latency.Milliseconds()`
- `test/e2e/scenarios/tiered_statistical.go:202` — `result.Metrics["graphrag_local_entities_found"] = entityCount`
- `test/e2e/scenarios/tiered_statistical.go:203` — `result.Metrics["graphrag_local_community_id"] = ls.CommunityID`
- `test/e2e/scenarios/tiered_statistical.go:210` — `result.Details["graphrag_local"] = map[string]any{`

### 4. `test-graphrag-global`
- `test/e2e/scenarios/tiered.go:378` — `{"test-graphrag-global", s.executeTestGraphRAGGlobal, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_statistical.go:238` — `func (s *TieredScenario) executeTestGraphRAGGlobal(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:259` — `func (s *TieredScenario) sendGraphRAGGlobalRequest(ctx context.Context, query, gatewayURL string) (*graphRAGGlobalResponse, time.Duration, error) {`
- `test/e2e/scenarios/tiered_statistical.go:283` — `httpClient := &http.Client{Timeout: 10 * time.Second}`
- `test/e2e/scenarios/tiered_statistical.go:315` — `func (s *TieredScenario) validateGraphRAGGlobalResult(resp *graphRAGGlobalResponse, query string, latency time.Duration, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:254` — `result.Metrics["graphrag_global_latency_ms"] = latency.Milliseconds()`
- `test/e2e/scenarios/tiered_statistical.go:320` — `result.Metrics["graphrag_global_entities_found"] = entityCount`
- `test/e2e/scenarios/tiered_statistical.go:321` — `result.Metrics["graphrag_global_communities_found"] = communityCount`
- `test/e2e/scenarios/tiered_statistical.go:339` — `result.Details["graphrag_global"] = map[string]any{`

### 5. `validate-anomaly-detection`
- `test/e2e/scenarios/tiered.go:390` — `{"validate-anomaly-detection", s.executeValidateAnomalyDetection, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:563` — `func (s *TieredScenario) executeValidateAnomalyDetection(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:648` — `func (s *TieredScenario) validateAnomalyGroundTruth(ctx context.Context, result *Result) *anomaly.ValidationResult {`
- `test/e2e/scenarios/tiered_semantic.go:576` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Anomaly detection wait error: %v", waitErr))`
- `test/e2e/scenarios/tiered_semantic.go:595` — `result.Metrics["anomalies_total"] = counts.Total`
- `test/e2e/scenarios/tiered_semantic.go:670` — `result.Details["anomaly_list"] = anomalyList`
- `test/e2e/scenarios/tiered_semantic.go:676` — `result.Metrics["anomaly_ground_truth_expected"] = groundTruthResult.ExpectedTotal`
- `test/e2e/scenarios/tiered_semantic.go:677` — `result.Metrics["anomaly_ground_truth_found"] = groundTruthResult.ExpectedFound`
- `test/e2e/scenarios/tiered_semantic.go:678` — `result.Metrics["anomaly_false_positives"] = groundTruthResult.FalsePositiveTotal`
- `test/e2e/scenarios/tiered_semantic.go:685` — `result.Metrics["anomaly_false_positive_rate"] = falsePositiveRate`
- `test/e2e/scenarios/tiered_semantic.go:704` — `result.Details["anomaly_ground_truth"] = map[string]any{`

This path has no dedicated `http.Client`: it reads NATS KV via `s.natsClient.WaitForAnomalyDetection(ctx, 30*time.Second, 2*time.Second)`, `GetAnomalyCounts(ctx)`, and `GetAnomalies(ctx)`; `validateAnomalyGroundTruth` itself is pure in-memory (`anomaly.NewDefaultValidator().Validate(anomalies)`, no I/O).

### 6. `validate-community-structure`
- `test/e2e/scenarios/tiered.go:360` — `{"validate-community-structure", s.executeValidateCommunityStructure, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_statistical.go:383` — `func (s *TieredScenario) executeValidateCommunityStructure(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:492` — `func (s *TieredScenario) validateCommunityGroundTruth(communities []*clustering.Community, result *Result) *community.ValidationResult {`
- `test/e2e/scenarios/tiered_statistical.go:450` — `result.Metrics["communities_total"] = totalCount`
- `test/e2e/scenarios/tiered_statistical.go:451` — `result.Metrics["communities_non_singleton"] = nonSingletonCount`
- `test/e2e/scenarios/tiered_statistical.go:452` — `result.Metrics["communities_largest_size"] = largestSize`
- `test/e2e/scenarios/tiered_statistical.go:453` — `result.Metrics["communities_avg_size"] = avgNonSingletonSize`
- `test/e2e/scenarios/tiered_statistical.go:454` — `result.Metrics["communities_with_keywords"] = communitiesWithKeywords`
- `test/e2e/scenarios/tiered_statistical.go:455` — `result.Metrics["communities_llm_enhanced"] = llmEnhancedCount`
- `test/e2e/scenarios/tiered_statistical.go:456` — `result.Metrics["communities_statistical_only"] = statisticalOnlyCount`
- `test/e2e/scenarios/tiered_statistical.go:458` — `result.Details["community_structure_validation"] = map[string]any{`
- `test/e2e/scenarios/tiered_statistical.go:497` — `result.Metrics["community_ground_truth_total"] = groundTruthResult.ExpectationsTotal`
- `test/e2e/scenarios/tiered_statistical.go:498` — `result.Metrics["community_ground_truth_passed"] = groundTruthResult.ExpectationsPassed`
- `test/e2e/scenarios/tiered_statistical.go:512` — `result.Details["community_ground_truth"] = map[string]any{`

This path has no dedicated `http.Client`: it reads NATS KV via `s.waitForCommunities(ctx)` and
`s.natsClient.GetCommunitySummaries(ctx)`; the ground-truth validator is pure in-memory.

Key collision: `communities_total`, `communities_llm_enhanced`, `communities_statistical_only`, and
`communities_non_singleton` are ALSO written by `validate-llm-enhancement` (path 8 below, semantic-only, runs
EARLIER in the stage table — see path 8's `tiered.go:310` row versus this path's `tiered.go:360` row). In the
semantic variant this stage's write runs later and overwrites path 8's write of the same four keys.

Second-spelling collision for "largest community size": this stage writes `communities_largest_size`
(`tiered_statistical.go:452` above) while path 8's `recordCommunityMetrics` writes a differently-spelled
`largest_community_size` (`tiered_semantic.go:463` below) — two keys, two spellings, the same fact, never
reconciled.

### 7. `validate-virtual-edges`
- `test/e2e/scenarios/tiered.go:391` — `{"validate-virtual-edges", s.executeValidateVirtualEdges, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:720` — `func (s *TieredScenario) executeValidateVirtualEdges(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:747` — `result.Metrics["virtual_edges_total"] = edgeCounts.Total`
- `test/e2e/scenarios/tiered_semantic.go:743` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get auto-applied count: %v", err))`
- `test/e2e/scenarios/tiered_semantic.go:773` — `result.Warnings = append(result.Warnings,`

This path has no dedicated `http.Client`: it reads NATS KV via `s.natsClient.CountVirtualEdges(ctx)` and
`GetAutoAppliedAnomalyCount(ctx)`. No `result.Details` key is set by this stage — only `result.Metrics`, plus
the bare `fmt.Println` on the zero-edges arm.

### 8. `validate-llm-enhancement`
- `test/e2e/scenarios/tiered.go:310` — `{"validate-llm-enhancement", s.executeValidateLLMEnhancement, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:470` — `func (s *TieredScenario) executeValidateLLMEnhancement(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:193` — `func (s *TieredScenario) waitForLLMEnhancement(`
- `test/e2e/scenarios/tiered_semantic.go:226` — `result.Metrics["llm_wait_duration_ms"] = float64(waitResult.durationMs)`
- `test/e2e/scenarios/tiered_semantic.go:227` — `result.Metrics["llm_failed_count"] = float64(waitResult.failedCount)`
- `test/e2e/scenarios/tiered_semantic.go:228` — `result.Metrics["llm_pending_count"] = float64(waitResult.pendingCount)`
- `test/e2e/scenarios/tiered_semantic.go:457` — `result.Metrics["communities_total"] = len(stats.comparisons)`
- `test/e2e/scenarios/tiered_semantic.go:458` — `result.Metrics["communities_llm_enhanced"] = stats.llmEnhancedCount`
- `test/e2e/scenarios/tiered_semantic.go:459` — `result.Metrics["communities_statistical_only"] = stats.statisticalOnlyCount`
- `test/e2e/scenarios/tiered_semantic.go:462` — `result.Metrics["communities_non_singleton"] = stats.nonSingletonCount`
- `test/e2e/scenarios/tiered_semantic.go:463` — `result.Metrics["largest_community_size"] = stats.largestCommunitySize`

This path has no dedicated `http.Client`: the 2-minute ceiling is a poll loop inside
`natsClient.WaitForCommunitySummaryEnhancement` (2s interval), sized via `llmEnhancementWait(2*time.Minute)`. No
`result.Details` key is set by this stage; its report goes to a file via `persistCommunityReport`, not to
`result.Details`.

### Cross-cutting: the persisted community report (a third spelling surface for path 6/8's keys)
- `test/e2e/scenarios/tiered.go:692` — `CommunitiesTotal      int`
- `test/e2e/scenarios/tiered.go:695` — `LLMFailedCount        int`
- `test/e2e/scenarios/tiered.go:696` — `LLMPendingCount       int`
- `test/e2e/scenarios/tiered.go:697` — `LLMWaitDurationMs     int64`
- `test/e2e/scenarios/tiered.go:698` — `AvgSummaryLengthRatio float64`
- `test/e2e/scenarios/tiered.go:699` — `AvgWordOverlap        float64`
- `test/e2e/scenarios/tiered.go:701` — `LargestCommunitySize  int`
- `test/e2e/scenarios/tiered.go:702` — `AvgNonSingletonSize   float64`

### Exit-code mechanics: how a warning does (not) reach the exit code
- `test/e2e/scenarios/scenario.go:39` — `Success bool`
- `test/e2e/scenarios/scenario.go:46` — `Warnings []string`
- `test/e2e/scenarios/scenario.go:49` — `AssertionsRun int`
- `test/e2e/scenarios/tiered.go:449` — `if err := stage.fn(ctx, result); err != nil {`
- `test/e2e/scenarios/tiered.go:452` — `result.Success = false`
- `test/e2e/scenarios/tiered.go:567` — `result.Success = true`
- `cmd/e2e/main.go:519` — `result, err := scenario.Execute(ctx)`
- `cmd/e2e/main.go:527` — `if err != nil {`
- `cmd/e2e/main.go:528` — `logger.Error("Scenario failed", "error", err, "assertions_run", assertionsRun(result))`
- `cmd/e2e/main.go:532` — `if !result.Success {`
- `cmd/e2e/main.go:533` — `logger.Error("Scenario completed with failure",`
- `cmd/e2e/main.go:540` — `logger.Info("Scenario completed successfully",`
- `cmd/e2e/main.go:574` — `return result.AssertionsRun`
- `test/e2e/scenarios/validate_entity.go:476` — `result.Metrics["validation_errors"] = len(validationErrors)`

Facts pinned above: the exit code is 1 only when `Execute` returns a non-nil `err` OR `result.Success == false`.
`result.Success` is set `false` only inside `executeStages` when a `stage.fn` returns a non-nil `error`
(`tiered.go:449`/`452`), and set `true` unconditionally once every stage in the variant's table returned nil
(`tiered.go:567`) — `Warnings` is never read on that path. `AssertionsRun` is never incremented anywhere in
`tiered.go`, `tiered_structural.go`, `tiered_statistical.go`, or `tiered_semantic.go` (zero hits, see Searches),
consistent with the proposal's boundary that #1222 owns that counter, not this change. `validation_errors` is a
metric set by an unrelated stage (`validate_entity.go`, not one of the eight) and is not read anywhere to gate
success (see Searches).

## Adjacent claims

- #1426 — this change's source issue; owner ruling pulled the slice into beta.163 (issuecomment-5894112756)
- #1117 — sibling issue: the semantic variant's per-PR wiring, gates PR #1425, shares the stage table
- #1222 — beta.165 required-check-evidence design; owner ruled 2026-09-27 no parallel assertion-accounting issue
- PR #1425 (open) — ci(e2e): measure the default semantic variant on a CI runner (#1117); touches
  .github/workflows/e2e-ladder.yml, docs/contributing/02-e2e-tests.md, taskfiles/e2e/semantic.yml, AGENTS.md,
  CLAUDE.md; blocked on an owner ruling for scope boxes 2-4, rebases onto this change per its body
- PR #1406 (open, draft) — Codex's test(e2e): design required-check evidence (#1222); its pushed branch
  (origin/codex/gh1222-required-e2e-proof) diffs only openspec/changes/e2e-required-check-evidence/proposal.md
  and openspec/changes/e2e-required-check-evidence/tasks.md, verified via
  `git diff --name-only origin/main...origin/codex/gh1222-required-e2e-proof`; per the brief, Codex's LOCAL
  unpushed worktree additionally edits cmd/e2e/main.go, cmd/e2e/main_test.go, docs/contributing/02-e2e-tests.md,
  and taskfiles/e2e/agentic.yml (observed 2026-09-29 by the coordinating session; not independently opened here)
- PR #1427 (open, draft, this change's own claim) — test(e2e): the tiered scenario's warn-only stages assert or
  leave the per-PR variant (#1426)

The existing rule that a green e2e tier must be trustworthy before a BREAKING commit lands already exists; this
change's stages sit inside that same tier.
- `docs/contributing/02-e2e-tests.md:324` — `## Breaking Changes Require an E2E Tier Before Merge`

The only `openspec/specs/` hit for "tiered"/"e2e tier"/"warn-only" terms is a self-title match, not substantive
content; no spec content anywhere discusses the tiered scenario, e2e tiers, or these stages (see Searches). No
capability spec exists yet for the tiered scenario, consistent with task 1.3 seeding one lazily.
- `openspec/specs/test-cleanup-policy/spec.md:1` — `# test-cleanup-policy Specification`

The ADR governing `CountVirtualEdges`'s legitimate-zero-vs-error split that path 7 already relies on:
- `docs/adr/065-predicate-index-composite-key-sharding.md:58` — `semantic tier's`

Prior art: `validate-llm-enhancement` (path 8) previously had a real bug — reading an emptied field — since fixed
by the `COMMUNITY_SUMMARIES` join this inventory read in `tiered_semantic.go`.
- `docs/proposals/prev1-program.md:1481` — `PAID OFF: the confirming frontier runs surfaced **two observability PHANTOMS** (the`

Prior art: a measurement table already tracking `community_ground_truth_passed` (path 6) across runs.
- `docs/proposals/prev1-program.md:1064` — `0, 0, 0, 0 | 0, 1, 1, 1`

Prior art: a prior inventory already named this same warn-not-fail site at nearly the same line numbers.
- `docs/proposals/gh606-derived-communities-inventory.md:139` — `all-singletons); **ground truth warn-not-fail at`

A landed change already cited these exact metric keys as completion evidence.
- `openspec/changes/archive/2026-08-29-entity-id-segment-semantics/tasks.md:1310` — `communities_total:17`

## Consumers

Every reader of each key/family, outside its own writer file. `(none — see Searches)` marks a group with zero
readers found.

### NL intent (`nl_path_intent_*`, `nl_temporal_intent_*`)
(none — see Searches)

### GraphRAG (`graphrag_local*`, `graphrag_global*`)
- `test/e2e/scenarios/results.go:490` — `if graphragLocal, ok := result.Details["graphrag_local"].(map[string]any); ok {`
- `test/e2e/scenarios/results.go:500` — `if graphragGlobal, ok := result.Details["graphrag_global"].(map[string]any); ok {`

### Anomaly (`anomalies_*`, `anomaly_*`)
- `test/e2e/scenarios/results.go:379` — `if getIntMetric(result, "anomalies_total") == 0 && getIntMetric(result, "anomalies_semantic_gap") == 0 &&`
- `test/e2e/scenarios/results.go:385` — `Total:         getIntMetric(result, "anomalies_total"),`
- `test/e2e/scenarios/results.go:409` — `if anomalyList, ok := result.Details["anomaly_list"].([]map[string]any); ok {`
- `test/e2e/scenarios/results.go:423` — `if gt, ok := result.Details["anomaly_ground_truth"].(map[string]any); ok {`

`community_ground_truth_passed` and `community_ground_truth_total` (path 6's own ground-truth keys) have no
reader in `results.go` — `CommunityResults` carries no ground-truth field (see Searches); their only readers
found are the two docs/proposals prior-art pins above.

### Community structure (`communities_*`, `community_structure_validation`)
- `test/e2e/scenarios/results.go:366` — `if getIntMetric(result, "communities_total") > 0 {`
- `test/e2e/scenarios/results.go:369` — `NonSingletonCount: getIntMetric(result, "communities_non_singleton"),`
- `test/e2e/scenarios/tiered.go:692` — `CommunitiesTotal      int`
- `cmd/e2e/test/e2e/results/community-comparison-semantic-20260116-103824.json:4` — `"communities_total": 24,`
- `test/e2e/docs/review/06-tiered-semantic.md:142` — `Total communities found`

The report struct (`tiered.go:692` above) and its persisted JSON sample are a second reader/writer of
`communities_total`, spelled per path 8's `recordCommunityMetrics`, not path 6's `communities_largest_size`.

### Virtual edges (`virtual_edges_*`, `anomalies_auto_applied`)
- `test/e2e/scenarios/results.go:397` — `virtualTotal := getIntMetric(result, "virtual_edges_total")`
- `test/e2e/scenarios/results.go:398` — `autoApplied := getIntMetric(result, "anomalies_auto_applied")`

### LLM-wait (`llm_wait_duration_ms`, `llm_failed_count`, `llm_pending_count`)
- `test/e2e/scenarios/tiered.go:695` — `LLMFailedCount        int`
- `test/e2e/scenarios/tiered.go:696` — `LLMPendingCount       int`
- `test/e2e/scenarios/tiered.go:697` — `LLMWaitDurationMs     int64`
- `cmd/e2e/test/e2e/results/community-comparison-semantic-20260116-103824.json:7` — `"llm_pending_count": 24,`

There is no reader of these three keys in `test/e2e/scenarios/results.go`'s `BuildTieredResults` structured
summary (see Searches) — only the report struct and its persisted file read them.

### Stage reachability: is any of the eight functions called from anywhere but this stage table?

No. Every `executeTest*`/`executeValidate*` function named above has exactly one call site: its own entry in
`test/e2e/scenarios/tiered.go`'s `allStages` table (see Searches). No taskfile or `.github/workflows` file
references any of the eight stage names by string (see Searches).
- `test/e2e/scenarios/graph_roundtrip_test.go:29` — `for _, stage := range scenario.getStagesForVariant(variant) {`

That test reads `getStagesForVariant`'s output but only asserts on `graph-roundtrip` and the two structural-only
stage names (`validate-canonical-create-no-hierarchy`, `validate-relationship-no-stub`), never on any of the
eight (see Searches).

## Problem shape

The closest existing instances of "assert the outcome, or classify the refusal and carry an observed signal" —
the shape this change asks the eight paths to adopt.

A sibling stage in the same table, `validate-globalsearch-known-answer`, is explicitly written to HARD-FAIL on
the same class of outcome — empty/wrong globalSearch results — that paths 3 and 4 warn on; its own doc comment
names the contrast with `executeTestGraphRAGGlobal`.
- `test/e2e/scenarios/tiered_semantic_known_answer.go:65` — `// executeTestGraphRAGGlobal probes a broad query and only WARNs on empty`
- `test/e2e/scenarios/tiered_semantic_known_answer.go:79` — `func (s *TieredScenario) executeValidateGlobalSearchKnownAnswer(ctx context.Context, result *Result) error {`

Path 7's own `CountVirtualEdges` call already classifies "legitimately zero" apart from "couldn't determine the
count", one call beneath the zero-edges arm this change is about.
- `test/e2e/scenarios/tiered_semantic.go:730` — `// itself now distinguishes "legitimately zero" (nil error, zero count —`

A different plane — Lifecycle `Stop`, not an e2e probe — already returns the caller's deadline error distinctly
from a clean, nil-returning stop (#1409, gh#1283).
- `processor/rule/cron_scheduler.go:381` — `func (s *CronScheduler) Stop(ctx context.Context) error {`
- `processor/rule/cron_scheduler.go:395` — `return ctx.Err()`

## Searches

- `gopls workspace_symbol -matcher=fuzzy TieredScenario` → NOT RUN (gopls unavailable in this pass; `grep -n`/`git grep -n` plus direct `sed -n` reads substituted for structural enumeration throughout, all recorded below)
- `git grep -n "test-nl-path-intent\|test-nl-temporal-intent\|test-graphrag-local\|test-graphrag-global\|validate-anomaly-detection\|validate-community-structure\|validate-virtual-edges\|validate-llm-enhancement" test/e2e/scenarios/tiered.go` → 8
- `grep -n "func.*Stages\|stageFuncType\|type stage\|stages :=\|stages =\|\[\]stage" test/e2e/scenarios/tiered.go` → 6
- `grep -n "result\.Warnings = append" test/e2e/scenarios/tiered.go test/e2e/scenarios/tiered_structural.go test/e2e/scenarios/tiered_statistical.go test/e2e/scenarios/tiered_semantic.go` → 58
- `awk` bounds scan for `executeTestSpatialQuery` → 1 function found (1028-1124)
- `grep -n "^func (s \*TieredScenario)" test/e2e/scenarios/tiered_structural.go` → 31
- `grep -n "^func (s \*TieredScenario)" test/e2e/scenarios/tiered_semantic.go` → 20
- `grep -n "^func (s \*TieredScenario)" test/e2e/scenarios/tiered_statistical.go` → 10
- `grep -n "^func (s \*TieredScenario)" test/e2e/scenarios/tiered.go` → 12
- `grep -n '"test-spatial-query"\|"test-temporal-query"\|"test-zone-relationships"\|"test-predicate-list"\|"test-predicate-stats"\|"validate-rule-transitions"' test/e2e/scenarios/tiered.go` → 6
- `git grep -n "DeadlineExceeded\|IsTimeout\|errors.Is(err" test/e2e/scenarios/tiered.go test/e2e/scenarios/tiered_structural.go test/e2e/scenarios/tiered_statistical.go test/e2e/scenarios/tiered_semantic.go` → 0 (no site anywhere in the four core files distinguishes a context-deadline error from any other transport error by type)
- `git grep -n "http.Client{Timeout\|context.WithTimeout" test/e2e/scenarios/tiered.go test/e2e/scenarios/tiered_structural.go test/e2e/scenarios/tiered_statistical.go test/e2e/scenarios/tiered_semantic.go` → 12 (all `http.Client{Timeout: 10 * time.Second}`; zero `context.WithTimeout` call sites)
- `git grep -n "func llmEnhancementWait\|SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT" -- test/e2e/` → 3
- `git grep -n "globalSearchClientTimeout(" -- test/e2e/` → 4 (defined once; called by http_gateway_readiness.go, tiered_semantic_known_answer.go, validate_thematic_eval.go — none of the eight paths use the overridable timeout, only the fixed 10s literal)
- `git grep -n -E 'nl_path_intent_tests_(passed|total)|nl_path_intent_test|nl_temporal_intent_tests_(passed|total)|nl_temporal_intent_test' -- . ':!test/e2e/scenarios/tiered_structural.go'` → 0
- `git grep -n -E 'graphrag_local|graphrag_global' -- . ':!test/e2e/scenarios/tiered_statistical.go'` → 5 (2 code readers in results.go, 3 prose mentions in archived openspec task files)
- `git grep -n -E 'anomalies_total|anomalies_semantic_gap|anomalies_core_isolation|anomalies_core_demotion|anomalies_transitivity|anomalies_pending|anomalies_confirmed|anomalies_dismissed|anomaly_list|anomaly_ground_truth|anomaly_false_positive|anomalies_auto_applied' -- . ':!test/e2e/scenarios/tiered_semantic.go'` → 12 (all in results.go)
- `git grep -n -E 'communities_total|communities_non_singleton|communities_largest_size|communities_avg_size|communities_with_keywords|communities_llm_enhanced|communities_statistical_only|community_structure_validation|community_ground_truth_total|community_ground_truth_passed|community_ground_truth"|largest_community_size|avg_summary_length_ratio|avg_word_overlap|avg_non_singleton_size' -- . ':!test/e2e/scenarios/tiered_statistical.go' ':!test/e2e/scenarios/tiered_semantic.go'` → 22 (persisted JSON sample, docker/compose/tiered.frontier.yml comment, two docs/proposals/* prior-art files, two archived openspec task files, test/e2e/docs/review/06-tiered-semantic.md, results.go, tiered.go struct tags)
- `git grep -n -E 'virtual_edges_total|virtual_edges_high|virtual_edges_medium|virtual_edges_related' -- . ':!test/e2e/scenarios/tiered_semantic.go'` → 5 (all results.go)
- `git grep -n -E 'llm_wait_duration_ms|llm_failed_count|llm_pending_count' -- . ':!test/e2e/scenarios/tiered_semantic.go'` → 5 (persisted JSON sample plus tiered.go struct tags; zero in results.go)
- `grep -n "Scenario failed\|assertions_run\|AssertionsRun\|validation_errors\|ValidationErrors\|Warnings" cmd/e2e/main.go` → 4
- `git grep -n "AssertionsRun" -- test/e2e/scenarios/tiered.go test/e2e/scenarios/tiered_structural.go test/e2e/scenarios/tiered_statistical.go test/e2e/scenarios/tiered_semantic.go` → 0
- `git grep -n '"validation_errors"\|ValidationErrors' -- test/e2e/ cmd/e2e/` (excluding archive) → 1 (validate_entity.go:476, an unrelated stage; never read for gating)
- `ls openspec/specs/ | grep -i e2e` → 0
- `git grep -ln "tiered\|test-nl-path-intent\|test-nl-temporal-intent\|test-graphrag-local\|test-graphrag-global\|validate-anomaly-detection\|validate-community-structure\|validate-virtual-edges\|validate-llm-enhancement" -- openspec/specs/` → 2 (graph-query/spec.md, payload-registry/spec.md; both only mention tiered.yml prose, not the scenario stages)
- `git grep -ln "e2e tier\|e2e-tier\|test-cleanup-policy\|tiered scenario" -- openspec/specs/` → 2 (payload-registry/spec.md's tiered.yml table rows; test-cleanup-policy/spec.md's own H1 self-match)
- `grep -n "tiered\|warn-only\|e2e tier" openspec/specs/test-cleanup-policy/spec.md` → 0 (confirms the single hit above was the H1 self-match only)
- `grep -n "warn\|Warning\|stage\|tiered" docs/contributing/02-e2e-tests.md` → 6, all "tiered" (compose-file/tier-table mentions); `grep -nic "warn"` → 0
- `grep -n "^#" docs/contributing/02-e2e-tests.md | grep -i break` → 1 (:324, the Breaking Changes section)
- `gh issue view 1426 --json title,body,comments` → 1 issue, 1 comment (owner ruling)
- `gh pr diff 1425 --name-only` → 5 files
- `git fetch origin codex/gh1222-required-e2e-proof` + `git diff --name-only origin/main...origin/codex/gh1222-required-e2e-proof` → 2 files
- `gh pr view 1425 --json title,body,number,state` → 1 (open, Closes #1117)
- `grep -n "test-nl-path-intent\|test-nl-temporal-intent\|test-graphrag-local\|test-graphrag-global\|validate-anomaly-detection\|validate-community-structure\|validate-virtual-edges\|validate-llm-enhancement\|SEMSTREAMS_E2E_SKIP" taskfiles/e2e/semantic.yml .github/workflows/e2e-ladder.yml` → 0
- `git grep -lE 'test-nl-path-intent|test-nl-temporal-intent|test-graphrag-local|test-graphrag-global|validate-anomaly-detection|validate-community-structure|validate-virtual-edges|validate-llm-enhancement' -- taskfiles/ .github/ docs/ scripts/` → 2 files (docs/adr/065-...md, docs/proposals/prev1-program.md); zero under taskfiles/ or .github/
- `gh issue list --search "warn-only e2e tiered" --state open --json number,title` → 6 (includes #1426, #1117, #1177, #606, #1111, #609 — the last three unrelated, surfaced by generic term overlap)
- `gh issue list --search "1426 in:body" --state open --json number,title` → 1 (#1177, the beta.163 tracking issue)
- `openspec list` → 1 (e2e-tiered-warn-only-stages, this change, 0/6 tasks)
- `gh pr list --json number,title,body,headRefName --state open` → 8 open PRs; #1427 (this change's claim), #1425, #1406 relevant
- `git grep -n "executeTestNLPathIntent\|executeTestNLTemporalIntent\|executeTestGraphRAGLocal\|executeTestGraphRAGGlobal\|executeValidateAnomalyDetection\|executeValidateCommunityStructure\|executeValidateVirtualEdges\|executeValidateLLMEnhancement" -- '*.go' | grep -v tiered.go` → 14 (all are the functions' own func declarations or doc comments in their defining files; zero additional call sites)
- `git grep -n "getStagesForVariant(" -- '*.go'` → 5 (1 definition, 1 caller in tiered.go:560, 3 in graph_roundtrip_test.go)
- `grep -cE 'test-nl-path-intent|test-nl-temporal-intent|test-graphrag-local|test-graphrag-global|validate-anomaly-detection|validate-community-structure|validate-virtual-edges|validate-llm-enhancement' test/e2e/scenarios/graph_roundtrip_test.go` → 0
- `git grep -n "community_ground_truth" -- . ':!test/e2e/scenarios/tiered_statistical.go'` → 3 (2 docs/proposals prior-art mentions, 1 archived openspec task noting it as a "non-gating soft probe")
- `grep -n "type CommunityResults struct" -A10 test/e2e/scenarios/results.go` → confirms no ground-truth field on that struct

## Round-1 sweep (review B2, owner Q2 "absorb"): every stage function in the table

Pinned at HEAD `6127d0be43e9255258adc72049418cab0c53220f` (after the fourteen-path fix, before the round-1 fix); the
file's `base:` line stays at the task-1.1 base, so these pins were verified against a scratch copy of this section
under `base: 6127d0be…` (command below). Scope: all 54 stage functions the stage table names (`tiered.go:250-431`),
wherever they are defined under `test/e2e/scenarios/`, plus the helpers they call. The class: a stage that appends its
detected outcome to `result.Warnings`, only prints it, or reaches `return nil` without checking it. Beyond the
fourteen, twelve stages carry the class; the five the review named come first.

### R1. `verify-index-population` (variants: all)
- `test/e2e/scenarios/tiered.go:284` — `{"verify-index-population", s.executeVerifyIndexPopulation, nil},`
- `test/e2e/scenarios/validate_structural.go:14` — `func (s *TieredScenario) executeVerifyIndexPopulation(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_structural.go:16` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping index population verification")`
- `test/e2e/scenarios/validate_structural.go:82` — `if len(emptyRequired) > 0 {`
- `test/e2e/scenarios/validate_structural.go:83` — `result.Warnings = append(result.Warnings,`

### R2. `verify-search-quality` (variants: statistical, semantic)

The function has no error return at all: transport errors, zero hits and known-answer misses are all recorded only.
- `test/e2e/scenarios/tiered.go:361` — `{"verify-search-quality", s.executeVerifySearchQuality, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_search.go:20` — `func (s *TieredScenario) executeVerifySearchQuality(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_search.go:36` — `s.searchStats = stats`
- `test/e2e/scenarios/validate_search.go:369` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/validate_search.go:376` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Known-answer test failed: %s", failure))`

The one known-answer miss in every measured run is a stale pattern, not a ranking miss: the query's top hit is
`…document.content.safety.doc-safety-001` (score 0.62) and the pattern predates the `content` category segment.
- `test/e2e/scenarios/search/queries.go:96` — `ExpectedPattern: "document.safety", // Matches both doc-safety-001 and doc-emergency-001`
- `test/e2e/scenarios/search/queries.go:101` — `MustInclude: []string{"document.safety"},`

### R3. `verify-outputs` (variants: all)
- `test/e2e/scenarios/tiered.go:431` — `{"verify-outputs", s.executeVerifyOutputs, nil},`
- `test/e2e/scenarios/validate_infra.go:209` — `func (s *TieredScenario) executeVerifyOutputs(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:235` — `if len(missingOutputs) > 0 {`
- `test/e2e/scenarios/validate_infra.go:236` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Missing outputs: %v", missingOutputs))`

### R4. `validate-bidirectional-traversal` (variants: structural, statistical, semantic)

No error return: nil client, empty entity read, no container, a failed incoming read and zero member edges all pass.
- `test/e2e/scenarios/tiered.go:387` — `{"validate-bidirectional-traversal", s.validateBidirectionalTraversal, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1257` — `func (s *TieredScenario) validateBidirectionalTraversal(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1259` — `result.Warnings = append(result.Warnings, "NATS client unavailable for bidirectional traversal")`
- `test/e2e/scenarios/tiered_semantic.go:1268` — `result.Warnings = append(result.Warnings, "No entities found for bidirectional traversal")`
- `test/e2e/scenarios/tiered_semantic.go:1291` — `incomingEntries, err := s.natsClient.GetIncomingEntries(ctx, containerID)`
- `test/e2e/scenarios/tiered_semantic.go:1312` — `result.Metrics["bidir_predicate_preserved"] = boolToInt(memberCount > 0)`

### R5. `validate-inverse-edges-materialized` (variants: structural, statistical, semantic)
- `test/e2e/scenarios/tiered.go:389` — `{"validate-inverse-edges-materialized", s.validateInverseEdgesMaterialized, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1340` — `func (s *TieredScenario) validateInverseEdgesMaterialized(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1342` — `result.Warnings = append(result.Warnings, "NATS client unavailable for inverse edges validation")`
- `test/e2e/scenarios/tiered_semantic.go:1351` — `result.Warnings = append(result.Warnings, "No entities found for inverse edges validation")`
- `test/e2e/scenarios/tiered_semantic.go:1421` — `} else if containsCount == 0 {`
- `test/e2e/scenarios/tiered_semantic.go:1422` — `if s.config.Variant == "structural" || s.config.Variant == "statistical" {`
- `test/e2e/scenarios/tiered_semantic.go:1425` — `fmt.Println("[INVERSE EDGES] Note: Contains edges not indexed yet (async update pending)")`
- `test/e2e/scenarios/tiered_semantic.go:1430` — `} else if containsCount != memberCount {`
- `test/e2e/scenarios/tiered_semantic.go:1431` — `result.Warnings = append(result.Warnings,`

### R6. `validate-hierarchy-inference` (variants: structural, statistical, semantic)
- `test/e2e/scenarios/tiered.go:279` — `{"validate-hierarchy-inference", s.validateHierarchyInference, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:944` — `func (s *TieredScenario) validateHierarchyInference(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:946` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping hierarchy inference validation")`
- `test/e2e/scenarios/tiered_semantic.go:967` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get entity IDs: %v", err))`
- `test/e2e/scenarios/tiered_semantic.go:1008` — `if containerCount < expectedMinContainers {`
- `test/e2e/scenarios/tiered_semantic.go:1010` — `fmt.Sprintf("Hierarchy inference may not be working: only %d containers for %d source entities (expected at least %d)",`

### R7. `validate-incoming-index-predicates` (variants: structural, statistical, semantic)

Its zero-entries arm already hard-fails outside structural; the arms below still pass.
- `test/e2e/scenarios/tiered.go:383` — `{"validate-incoming-index-predicates", s.validateIncomingIndexPredicates, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1148` — `func (s *TieredScenario) validateIncomingIndexPredicates(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1150` — `result.Warnings = append(result.Warnings, "NATS client unavailable for incoming index validation")`
- `test/e2e/scenarios/tiered_semantic.go:1159` — `result.Warnings = append(result.Warnings, "No entities found for incoming index validation")`
- `test/e2e/scenarios/tiered_semantic.go:1172` — `if containerID == "" {`
- `test/e2e/scenarios/tiered_semantic.go:1174` — `result.Metrics["incoming_predicate_validation"] = 0`
- `test/e2e/scenarios/tiered_semantic.go:1236` — `if len(entries) > 0 && predicateCount == 0 {`
- `test/e2e/scenarios/tiered_semantic.go:1238` — `fmt.Sprintf("IncomingIndex has %d entries but none have predicates - index may use old []string format", len(entries)))`

### R8. `verify-entity-retrieval` (variants: all)
- `test/e2e/scenarios/tiered.go:282` — `{"verify-entity-retrieval", s.executeVerifyEntityRetrieval, nil},`
- `test/e2e/scenarios/validate_entity.go:315` — `func (s *TieredScenario) executeVerifyEntityRetrieval(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:317` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping entity retrieval verification")`
- `test/e2e/scenarios/validate_entity.go:379` — `if len(missingEntities) > 0 {`
- `test/e2e/scenarios/validate_entity.go:380` — `result.Warnings = append(result.Warnings,`

### R9. `validate-entity-structure` (variants: all)

Its core structure check already hard-fails; the read-failure and empty-sample arms validate nothing and pass.
- `test/e2e/scenarios/tiered.go:283` — `{"validate-entity-structure", s.executeValidateEntityStructure, nil},`
- `test/e2e/scenarios/validate_entity.go:388` — `func (s *TieredScenario) executeValidateEntityStructure(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:390` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping entity structure validation")`
- `test/e2e/scenarios/validate_entity.go:397` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get entity sample: %v", err))`
- `test/e2e/scenarios/validate_entity.go:402` — `result.Warnings = append(result.Warnings, "No entities available for structure validation")`

### R10. `test-embedding-fallback` (variants: statistical, semantic)

An unhealthy `graph-embedding` matches neither branch and the stage passes without a warning.
- `test/e2e/scenarios/tiered.go:370` — `{"test-embedding-fallback", s.executeTestEmbeddingFallback, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_infra.go:393` — `func (s *TieredScenario) executeTestEmbeddingFallback(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:426` — `if !semembedAvailable && graphEmbeddingHealthy {`
- `test/e2e/scenarios/validate_infra.go:430` — `result.Metrics["hybrid_mode_verified"] = 1`
- `test/e2e/scenarios/validate_infra.go:437` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to connect for fallback test: %v", err))`
- `test/e2e/scenarios/validate_infra.go:438` — `return nil // Don't fail the whole test`

### R11. `validate-processing`, unhealthy-graph-component arm (variants: all)

Its processing-wait timeout defers to `wait-for-entity-stabilization`, which asserts; the health arm defers to nothing.
- `test/e2e/scenarios/tiered.go:252` — `{"validate-processing", s.executeValidateProcessing, nil},`
- `test/e2e/scenarios/validate_infra.go:104` — `func (s *TieredScenario) executeValidateProcessing(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:169` — `if !comp.Healthy {`
- `test/e2e/scenarios/validate_infra.go:172` — `fmt.Sprintf("Graph component %s not healthy: state=%s", comp.Name, comp.State),`

### R12. `verify-entity-count`, nil-client arm only (variants: all)

Its count and critical-entity checks already hard-fail (`validateEntityLoadResult`); only the unreachable nil arm (P13) passes.
- `test/e2e/scenarios/tiered.go:281` — `{"verify-entity-count", s.executeVerifyEntityCount, nil},`
- `test/e2e/scenarios/validate_entity.go:143` — `func (s *TieredScenario) executeVerifyEntityCount(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:145` — `result.Warnings = append(result.Warnings, "NATS client not available, skipping entity count verification")`

### Round-1 review sites outside the class sweep (B1, M1)
- `test/e2e/scenarios/validate_infra.go:482` — `finalMetrics, err := s.metrics.ExtractRuleMetrics(ctx)`
- `test/e2e/scenarios/validate_infra.go:484` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get final rule metrics: %v", err))`
- `test/e2e/scenarios/validate_infra.go:555` — `if baseline.Evaluations >= 100 {`
- `test/e2e/scenarios/validate_infra.go:716` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get initial rule metrics: %v", err))`

### Swept and left, with the reason

Not in the class. `send-mixed-data` (`validate_infra.go:91`) captures a baseline and detects nothing.
`wait-for-embeddings` (`:291`, `:309`, `:332`) and `wait-for-rule-stabilization` are waits whose outcome the next
stage asserts (`validate-embedding-queue-health` fails on `resolved == 0`; `validate-rules`). `validate-processing`'s
processing-wait arm (`:136`) defers to `wait-for-entity-stabilization`, which hard-fails. `validate-thematic-answer-eval`
and `validate-partition-colocation` are the declared B0/B2 recorders. `captureAndCompareBaseline` (`tiered.go:514`) is
not a stage. (`wait-for-rule-stabilization`'s failed-read arm is fixed anyway, as review M1.) The other 23 stage
functions (54, less these twelve, the twelve round-0 stages still in the table, and the seven named in this section)
return an error on their detected outcome; per-function counts of error returns and `Warnings` appends, command below.

In the class but outside requirement 1, because no per-PR variant runs them. `validate-entity-triples`
(structural only) warns on a missing sample entity and missing triples; it is a rule-debug diagnostic.
`validate-globalsearch-known-answer` (semantic only, one of #1117's three quality stages) warns when no GraphQL URL is
configured. The structural-only notes in R5 and R7 are the same case.
- `test/e2e/scenarios/tiered_structural.go:241` — `sampleEntityID := mint("sensor.environmental.temperature.temp-sensor-001")`
- `test/e2e/scenarios/tiered_structural.go:323` — `fmt.Sprintf("MISSING sensor.measurement.fahrenheit in entity %s - rules cannot evaluate temperature", sampleEntityID))`
- `test/e2e/scenarios/tiered_semantic.go:1199` — `if s.effectiveVariant(result) == "structural" {`
- `test/e2e/scenarios/tiered_semantic.go:1200` — `fmt.Println("[INCOMING INDEX] Note: no incoming edges yet (expected in short structural tier run)")`

## Searches (round-1 sweep)

- `git grep -c "Warnings = append" -- test/e2e/scenarios/ ':!*_test.go'` → 79 sites in 10 files (`core_dataflow.go` 3 belong to the core scenario, not the tiered table)
- `grep -o 's\.[a-zA-Z]*, \(nil\|\[\]string{[^}]*}\)},' test/e2e/scenarios/tiered.go | sed 's/s\.\([a-zA-Z]*\),.*/\1/' | sort -u | wc -l` → 54 stage functions
- per stage function: `awk` the body from its `func (s *TieredScenario) <fn>(` line to the closing `}`, then `grep -c 'return fmt.Errorf\|return err'` and `grep -c 'Warnings = append'` → 7 functions with zero error returns (`executeSendMixedData`, `executeVerifyIndexPopulation`, `executeVerifySearchQuality`, `executeWaitForEmbeddings`, `validateBidirectionalTraversal`, `validateHierarchyInference`, `validateInverseEdgesMaterialized`); 20 with at least one `Warnings` append; each read by hand
- `grep -n 'fmt.Print[a-z]*(".*\(WARN\|Warning\|Note\|may \|not \|skip\)' test/e2e/scenarios/tiered*.go test/e2e/scenarios/validate_*.go` → 8 (three print-only outcome arms: `tiered_semantic.go:772` from round 0, `:1200` and `:1425` above; the rest are recorder banners and a retry notice)
- verification of the pins above: this section copied under `base: 6127d0be43e9255258adc72049418cab0c53220f` to the session scratchpad, `scripts/inventory-verify.sh <copy>` → every pin OK (recorded in evidence.md § 5)
