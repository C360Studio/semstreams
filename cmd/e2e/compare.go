// Package main provides structured result comparison for tier runs
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
)

// handleCompareFilesCommand compares two structured result files
func handleCompareFilesCommand(logger *slog.Logger, baselineFile, targetFile string) int {
	if baselineFile == "" || targetFile == "" {
		logger.Error("Both --baseline and --target files required")
		fmt.Println("\nUsage: ./e2e compare-structured --baseline results/structural.json --target results/statistical.json")
		return 1
	}

	// Load baseline
	baseline, err := scenarios.LoadStructuredResults(baselineFile)
	if err != nil {
		logger.Error("Failed to load baseline", "file", baselineFile, "error", err)
		return 1
	}

	// Load target
	target, err := scenarios.LoadStructuredResults(targetFile)
	if err != nil {
		logger.Error("Failed to load target", "file", targetFile, "error", err)
		return 1
	}

	// Generate comparison report
	report := compareResults(baseline, target, baselineFile, targetFile)

	// Print report
	printComparisonReport(report)

	// Save report
	outputFile := fmt.Sprintf("structured-comparison-%s-vs-%s-%s.json",
		baseline.Variant.Name,
		target.Variant.Name,
		time.Now().Format("20060102-150405"))

	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		if err := os.WriteFile(outputFile, data, 0644); err == nil {
			logger.Info("Report saved", "file", outputFile)
		}
	}

	return 0
}

// compareResults generates a comparison between two structured results
func compareResults(baseline, target *scenarios.TieredResults, baselineFile, targetFile string) *ComparisonReport {
	report := &ComparisonReport{
		BaselineVariant: baseline.Variant.Name,
		TargetVariant:   target.Variant.Name,
		BaselineFile:    baselineFile,
		TargetFile:      targetFile,
		GeneratedAt:     time.Now(),
	}

	// Variant diff
	report.Sections.Variant = VariantDiff{
		BaselineName:     baseline.Variant.Name,
		TargetName:       target.Variant.Name,
		BaselineProvider: baseline.Variant.EmbeddingProvider,
		TargetProvider:   target.Variant.EmbeddingProvider,
	}

	// Entity diff
	report.Sections.Entities = EntityDiff{
		BaselineCount: baseline.Entities.ActualCount,
		TargetCount:   target.Entities.ActualCount,
		CountDiff:     target.Entities.ActualCount - baseline.Entities.ActualCount,
		BaselineLoss:  baseline.Entities.DataLossPercent,
		TargetLoss:    target.Entities.DataLossPercent,
	}

	// Index diff
	report.Sections.Indexes = compareIndexes(baseline.Indexes, target.Indexes)

	// Search diff
	report.Sections.Search = compareSearch(baseline.Search, target.Search)

	// Rule diff (reactive workflow metrics)
	report.Sections.Rules = RuleDiff{
		BaselineEvaluated: baseline.Rules.EvaluatedCount,
		TargetEvaluated:   target.Rules.EvaluatedCount,
		BaselineFirings:   baseline.Rules.FiringsCount,
		TargetFirings:     target.Rules.FiringsCount,
		BaselineActions:   baseline.Rules.ActionsDispatched,
		TargetActions:     target.Rules.ActionsDispatched,
		BaselineExecs:     baseline.Rules.ExecutionsCreated,
		TargetExecs:       target.Rules.ExecutionsCreated,
		BaselinePassed:    baseline.Rules.ValidationPassed,
		TargetPassed:      target.Rules.ValidationPassed,
	}

	// Community diff (if both have it)
	if baseline.Communities != nil && target.Communities != nil {
		report.Sections.Communities = &CommunityDiff{
			BaselineTotal:   baseline.Communities.TotalCommunities,
			TargetTotal:     target.Communities.TotalCommunities,
			TotalDiff:       target.Communities.TotalCommunities - baseline.Communities.TotalCommunities,
			BaselineLargest: baseline.Communities.LargestSize,
			TargetLargest:   target.Communities.LargestSize,
			BaselineAvgSize: baseline.Communities.AverageSize,
			TargetAvgSize:   target.Communities.AverageSize,
		}
	}

	// Anomaly detection comparison (semantic only - uses k-core and pivot)
	report.Sections.Anomalies = compareAnomalies(baseline.Anomalies, target.Anomalies)

	// PathRAG comparison (Tier 0 - should be present in all tiers)
	report.Sections.PathRAG = comparePathRAG(baseline.PathRAGSensor, target.PathRAGSensor)

	// GraphRAG comparison (Tier 2 - semantic only)
	report.Sections.GraphRAG = compareGraphRAG(baseline.GraphRAG, target.GraphRAG)

	// Generate summary
	report.Summary = generateSummary(report, baseline, target)

	return report
}

// compareIndexes compares index population between two results
func compareIndexes(baseline, target scenarios.IndexResults) IndexDiff {
	diff := IndexDiff{
		BaselinePopulated: baseline.PopulatedIndexes,
		TargetPopulated:   target.PopulatedIndexes,
		PopulatedDiff:     target.PopulatedIndexes - baseline.PopulatedIndexes,
	}

	// Find differing indexes
	allIndexes := make(map[string]bool)
	for name := range baseline.IndexDetails {
		allIndexes[name] = true
	}
	for name := range target.IndexDetails {
		allIndexes[name] = true
	}

	for name := range allIndexes {
		baseDetail := baseline.IndexDetails[name]
		targetDetail := target.IndexDetails[name]
		if baseDetail.Populated != targetDetail.Populated {
			if targetDetail.Populated {
				diff.DifferingIndexes = append(diff.DifferingIndexes, fmt.Sprintf("+%s", name))
			} else {
				diff.DifferingIndexes = append(diff.DifferingIndexes, fmt.Sprintf("-%s", name))
			}
		}
	}

	return diff
}

// compareSearch compares search quality between two results
func compareSearch(baseline, target scenarios.SearchResults) SearchDiff {
	diff := SearchDiff{}

	if baseline.Stats != nil {
		diff.BaselineQueries = baseline.Stats.TotalQueries
		diff.BaselineWithResults = baseline.Stats.QueriesWithResults
		diff.BaselineAvgScore = baseline.Stats.OverallAvgScore
		if baseline.Stats.KnownAnswerTestsTotal > 0 {
			diff.BaselineKnownAnswerPct = float64(baseline.Stats.KnownAnswerTestsPassed) / float64(baseline.Stats.KnownAnswerTestsTotal) * 100
		}
	}

	if target.Stats != nil {
		diff.TargetQueries = target.Stats.TotalQueries
		diff.TargetWithResults = target.Stats.QueriesWithResults
		diff.TargetAvgScore = target.Stats.OverallAvgScore
		if target.Stats.KnownAnswerTestsTotal > 0 {
			diff.TargetKnownAnswerPct = float64(target.Stats.KnownAnswerTestsPassed) / float64(target.Stats.KnownAnswerTestsTotal) * 100
		}
	}

	// Compare individual queries and get aggregate statistics
	if baseline.Stats != nil && target.Stats != nil {
		queryDiffs, avgJaccard, avgCorr, targetBetter, baselineBetter, tied := compareQueryResults(baseline.Stats.Results, target.Stats.Results)
		diff.QueryDiffs = queryDiffs
		diff.AvgHitOverlap = avgJaccard
		diff.AvgScoreCorr = avgCorr
		diff.TargetBetterCnt = targetBetter
		diff.BaselineBetterCnt = baselineBetter
		diff.TiedCount = tied
		diff.Verdict = determineSearchVerdict(targetBetter, baselineBetter, tied, avgJaccard)
	}

	return diff
}

// compareQueryResults compares individual query results and returns diffs plus aggregate stats
func compareQueryResults(baselineResults, targetResults []search.Result) ([]SearchQueryDiff, float64, float64, int, int, int) {
	var diffs []SearchQueryDiff
	var totalJaccard, totalCorr float64
	var corrCount int
	var targetBetter, baselineBetter, tied int

	// Build maps by query text
	baselineMap := make(map[string]search.Result)
	for _, r := range baselineResults {
		baselineMap[r.Query] = r
	}

	targetMap := make(map[string]search.Result)
	for _, r := range targetResults {
		targetMap[r.Query] = r
	}

	// Get all queries
	allQueries := make(map[string]bool)
	for q := range baselineMap {
		allQueries[q] = true
	}
	for q := range targetMap {
		allQueries[q] = true
	}

	// Sort for consistent output
	var queries []string
	for q := range allQueries {
		queries = append(queries, q)
	}
	sort.Strings(queries)

	for _, query := range queries {
		baseResult := baselineMap[query]
		targetResult := targetMap[query]

		baseHits := len(baseResult.Hits)
		targetHits := len(targetResult.Hits)

		// Extract hit IDs and scores for Jaccard/correlation
		baseHitIDs := extractHitIDs(baseResult.Hits)
		targetHitIDs := extractHitIDs(targetResult.Hits)
		baseScoreMap := extractScoreMap(baseResult.Hits)
		targetScoreMap := extractScoreMap(targetResult.Hits)

		// Calculate Jaccard similarity
		hitOverlap := jaccard(baseHitIDs, targetHitIDs)
		totalJaccard += hitOverlap

		// Calculate score correlation for shared hits
		scoreCorr := scoreCorrelation(baseScoreMap, targetScoreMap)

		qd := SearchQueryDiff{
			Query:            query,
			BaselineHits:     baseHits,
			TargetHits:       targetHits,
			HitsDiff:         targetHits - baseHits,
			BaselineAvgScore: baseResult.Validation.AvgScore,
			TargetAvgScore:   targetResult.Validation.AvgScore,
			HitOverlap:       hitOverlap,
			ScoreCorr:        scoreCorr,
		}

		// Track correlation for averaging (only if valid)
		if !math.IsNaN(scoreCorr) {
			totalCorr += scoreCorr
			corrCount++
		}

		// Generate insight
		qd.Insight = generateQueryInsight(baseHits, targetHits, qd.BaselineAvgScore, qd.TargetAvgScore)

		// Track which is better
		if targetHits > baseHits && qd.TargetAvgScore >= qd.BaselineAvgScore {
			targetBetter++
		} else if baseHits > targetHits && qd.BaselineAvgScore > qd.TargetAvgScore {
			baselineBetter++
		} else {
			tied++
		}

		// Only include if there's a meaningful difference
		if qd.HitsDiff != 0 || (qd.BaselineAvgScore > 0 && qd.TargetAvgScore > 0) {
			diffs = append(diffs, qd)
		}
	}

	// Calculate averages
	queryCount := len(allQueries)
	avgJaccard := 0.0
	if queryCount > 0 {
		avgJaccard = totalJaccard / float64(queryCount)
	}
	avgCorr := math.NaN()
	if corrCount > 0 {
		avgCorr = totalCorr / float64(corrCount)
	}

	return diffs, avgJaccard, avgCorr, targetBetter, baselineBetter, tied
}

// extractHitIDs extracts entity IDs from search hits
func extractHitIDs(hits []search.Hit) []string {
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.EntityID
	}
	return ids
}

// extractScoreMap creates a map of entity ID to score from hits
func extractScoreMap(hits []search.Hit) map[string]float64 {
	scoreMap := make(map[string]float64)
	for _, h := range hits {
		scoreMap[h.EntityID] = h.Score
	}
	return scoreMap
}

// generateQueryInsight generates a human-readable insight for a query comparison
func generateQueryInsight(baselineHits, targetHits int, baselineAvg, targetAvg float64) string {
	if baselineHits == 0 && targetHits == 0 {
		return "No results from either"
	}
	if baselineHits == 0 {
		return fmt.Sprintf("Target finds %d results where baseline found none", targetHits)
	}
	if targetHits == 0 {
		return fmt.Sprintf("Baseline finds %d results where target found none", baselineHits)
	}
	if targetHits > baselineHits {
		return fmt.Sprintf("Target finds +%d more results", targetHits-baselineHits)
	}
	if baselineHits > targetHits {
		return fmt.Sprintf("Baseline finds +%d more results", baselineHits-targetHits)
	}
	if targetAvg > baselineAvg*1.1 {
		return "Similar hits, target has higher scores"
	}
	if baselineAvg > targetAvg*1.1 {
		return "Similar hits, baseline has higher scores"
	}
	return "Similar results"
}

// comparePathRAG compares PathRAG results between two tier runs
func comparePathRAG(baseline, target *scenarios.PathRAGResults) *PathRAGDiff {
	if baseline == nil && target == nil {
		return nil
	}
	diff := &PathRAGDiff{}
	if baseline != nil {
		diff.BaselineEntities = baseline.EntitiesFound
		diff.BaselineLatency = baseline.LatencyMs
	}
	if target != nil {
		diff.TargetEntities = target.EntitiesFound
		diff.TargetLatency = target.LatencyMs
	}
	diff.EntitiesDiff = diff.TargetEntities - diff.BaselineEntities
	diff.BothScoresValid = (baseline == nil || baseline.ScoresValid) &&
		(target == nil || target.ScoresValid)
	return diff
}

// compareGraphRAG compares GraphRAG query results
func compareGraphRAG(baseline, target *scenarios.GraphRAGResults) *GraphRAGDiff {
	if baseline == nil && target == nil {
		return nil
	}
	diff := &GraphRAGDiff{}

	// Local query comparison
	if baseline != nil && baseline.LocalQuery != nil {
		diff.BaselineLocalSuccess = baseline.LocalQuery.Success
		diff.BaselineLocalLatency = baseline.LocalQuery.LatencyMs
	}
	if target != nil && target.LocalQuery != nil {
		diff.TargetLocalSuccess = target.LocalQuery.Success
		diff.TargetLocalLatency = target.LocalQuery.LatencyMs
	}

	// Global query comparison
	if baseline != nil && baseline.GlobalQuery != nil {
		diff.BaselineGlobalSuccess = baseline.GlobalQuery.Success
		diff.BaselineGlobalLatency = baseline.GlobalQuery.LatencyMs
	}
	if target != nil && target.GlobalQuery != nil {
		diff.TargetGlobalSuccess = target.GlobalQuery.Success
		diff.TargetGlobalLatency = target.GlobalQuery.LatencyMs
	}

	return diff
}

// compareAnomalies compares structural anomaly detection results
func compareAnomalies(baseline, target *scenarios.AnomalyResults) *AnomalyDiff {
	if baseline == nil && target == nil {
		return nil
	}
	diff := &AnomalyDiff{}

	if baseline != nil {
		diff.BaselineTotal = baseline.Total
		diff.BaselineSemanticGap = baseline.SemanticGap
		diff.BaselineCoreIsolation = baseline.CoreIsolation
		diff.BaselineCoreDemotion = baseline.CoreDemotion
		diff.BaselineTransitivity = baseline.Transitivity
	}
	if target != nil {
		diff.TargetTotal = target.Total
		diff.TargetSemanticGap = target.SemanticGap
		diff.TargetCoreIsolation = target.CoreIsolation
		diff.TargetCoreDemotion = target.CoreDemotion
		diff.TargetTransitivity = target.Transitivity
	}

	diff.TotalDiff = diff.TargetTotal - diff.BaselineTotal
	diff.SemanticGapDiff = diff.TargetSemanticGap - diff.BaselineSemanticGap
	diff.CoreIsolationDiff = diff.TargetCoreIsolation - diff.BaselineCoreIsolation
	diff.CoreDemotionDiff = diff.TargetCoreDemotion - diff.BaselineCoreDemotion
	diff.TransitivityDiff = diff.TargetTransitivity - diff.BaselineTransitivity

	return diff
}

// generateSummary generates the comparison summary
func generateSummary(report *ComparisonReport, baseline, target *scenarios.TieredResults) CompareSummary {
	summary := CompareSummary{}

	// Tier capability diff
	summary.TierCapabilityDiff = fmt.Sprintf("%s → %s",
		baseline.Variant.Name, target.Variant.Name)

	// Search improvement based on queries with results and score
	targetBetter := report.Sections.Search.TargetWithResults > report.Sections.Search.BaselineWithResults ||
		report.Sections.Search.TargetAvgScore > report.Sections.Search.BaselineAvgScore*1.1
	baselineBetter := report.Sections.Search.BaselineWithResults > report.Sections.Search.TargetWithResults ||
		report.Sections.Search.BaselineAvgScore > report.Sections.Search.TargetAvgScore*1.1

	if targetBetter && !baselineBetter {
		summary.SearchImprovement = fmt.Sprintf("Target has better search (%d/%d queries with results, avg score %.3f)",
			report.Sections.Search.TargetWithResults, report.Sections.Search.TargetQueries,
			report.Sections.Search.TargetAvgScore)
	} else if baselineBetter && !targetBetter {
		summary.SearchImprovement = fmt.Sprintf("Baseline has better search (%d/%d queries with results, avg score %.3f)",
			report.Sections.Search.BaselineWithResults, report.Sections.Search.BaselineQueries,
			report.Sections.Search.BaselineAvgScore)
	} else {
		summary.SearchImprovement = "Similar search performance"
	}

	// Track regressions and improvements
	if report.Sections.Entities.TargetCount < report.Sections.Entities.BaselineCount {
		summary.Regressions = append(summary.Regressions,
			fmt.Sprintf("Entity count: %d → %d",
				report.Sections.Entities.BaselineCount,
				report.Sections.Entities.TargetCount))
	} else if report.Sections.Entities.TargetCount > report.Sections.Entities.BaselineCount {
		summary.Improvements = append(summary.Improvements,
			fmt.Sprintf("Entity count: %d → %d",
				report.Sections.Entities.BaselineCount,
				report.Sections.Entities.TargetCount))
	}

	if report.Sections.Indexes.PopulatedDiff > 0 {
		summary.Improvements = append(summary.Improvements,
			fmt.Sprintf("Indexes populated: +%d", report.Sections.Indexes.PopulatedDiff))
	} else if report.Sections.Indexes.PopulatedDiff < 0 {
		summary.Regressions = append(summary.Regressions,
			fmt.Sprintf("Indexes populated: %d", report.Sections.Indexes.PopulatedDiff))
	}

	if report.Sections.Communities != nil && report.Sections.Communities.TotalDiff > 0 {
		summary.Improvements = append(summary.Improvements,
			fmt.Sprintf("Communities detected: +%d", report.Sections.Communities.TotalDiff))
	}

	// PathRAG insights
	if report.Sections.PathRAG != nil {
		if report.Sections.PathRAG.EntitiesDiff > 0 {
			summary.Improvements = append(summary.Improvements,
				fmt.Sprintf("PathRAG: +%d entities discovered", report.Sections.PathRAG.EntitiesDiff))
		} else if report.Sections.PathRAG.EntitiesDiff < 0 {
			summary.Regressions = append(summary.Regressions,
				fmt.Sprintf("PathRAG: %d fewer entities discovered", report.Sections.PathRAG.EntitiesDiff))
		}
		if !report.Sections.PathRAG.BothScoresValid {
			summary.Regressions = append(summary.Regressions, "PathRAG score validation failed")
		}
	}

	// GraphRAG capability insights
	if report.Sections.GraphRAG != nil {
		if !report.Sections.GraphRAG.BaselineLocalSuccess && report.Sections.GraphRAG.TargetLocalSuccess {
			summary.Improvements = append(summary.Improvements, "GraphRAG local query now available")
		}
		if !report.Sections.GraphRAG.BaselineGlobalSuccess && report.Sections.GraphRAG.TargetGlobalSuccess {
			summary.Improvements = append(summary.Improvements, "GraphRAG global query now available")
		}
		if report.Sections.GraphRAG.BaselineLocalSuccess && !report.Sections.GraphRAG.TargetLocalSuccess {
			summary.Regressions = append(summary.Regressions, "GraphRAG local query lost")
		}
		if report.Sections.GraphRAG.BaselineGlobalSuccess && !report.Sections.GraphRAG.TargetGlobalSuccess {
			summary.Regressions = append(summary.Regressions, "GraphRAG global query lost")
		}
	}

	// Anomaly detection insights
	if report.Sections.Anomalies != nil {
		if report.Sections.Anomalies.BaselineTotal == 0 && report.Sections.Anomalies.TargetTotal > 0 {
			summary.Improvements = append(summary.Improvements,
				fmt.Sprintf("Anomaly detection now active: %d anomalies found", report.Sections.Anomalies.TargetTotal))
		} else if report.Sections.Anomalies.TotalDiff > 0 {
			summary.Improvements = append(summary.Improvements,
				fmt.Sprintf("Anomaly detection: +%d more anomalies detected", report.Sections.Anomalies.TotalDiff))
		}
		if report.Sections.Anomalies.TargetSemanticGap > 0 && report.Sections.Anomalies.BaselineSemanticGap == 0 {
			summary.Improvements = append(summary.Improvements,
				fmt.Sprintf("Semantic gap detection enabled: %d gaps found (k-core/pivot value)", report.Sections.Anomalies.TargetSemanticGap))
		}
	}

	return summary
}

// printComparisonReport prints the comparison report
func printComparisonReport(report *ComparisonReport) {
	printReportHeader(report)
	printVariantSection(report)
	printEntitiesSection(report)
	printIndexesSection(report)
	printSearchSection(report)
	printRulesSection(report)
	printCommunitiesSection(report)
	printAnomaliesSection(report)
	printPathRAGSection(report)
	printGraphRAGSection(report)
	printSummarySection(report)
}

func printReportHeader(report *ComparisonReport) {
	fmt.Println("\n=== Structured Tier Comparison ===")
	fmt.Printf("Baseline: %s (%s)\n", report.BaselineVariant, report.BaselineFile)
	fmt.Printf("Target:   %s (%s)\n", report.TargetVariant, report.TargetFile)
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
}

func printVariantSection(report *ComparisonReport) {
	fmt.Println("\n--- Variant ---")
	fmt.Printf("  Provider: %s → %s\n",
		report.Sections.Variant.BaselineProvider,
		report.Sections.Variant.TargetProvider)
}

func printEntitiesSection(report *ComparisonReport) {
	fmt.Println("\n--- Entities ---")
	fmt.Printf("  Count: %d → %d (diff: %+d)\n",
		report.Sections.Entities.BaselineCount,
		report.Sections.Entities.TargetCount,
		report.Sections.Entities.CountDiff)
	fmt.Printf("  Data Loss: %.1f%% → %.1f%%\n",
		report.Sections.Entities.BaselineLoss,
		report.Sections.Entities.TargetLoss)
}

func printIndexesSection(report *ComparisonReport) {
	fmt.Println("\n--- Indexes ---")
	fmt.Printf("  Populated: %d → %d (diff: %+d)\n",
		report.Sections.Indexes.BaselinePopulated,
		report.Sections.Indexes.TargetPopulated,
		report.Sections.Indexes.PopulatedDiff)
	if len(report.Sections.Indexes.DifferingIndexes) > 0 {
		fmt.Printf("  Differing: %s\n", strings.Join(report.Sections.Indexes.DifferingIndexes, ", "))
	}
}

func printSearchSection(report *ComparisonReport) {
	fmt.Println("\n--- Search Quality ---")
	fmt.Printf("  Queries with results: %d/%d → %d/%d\n",
		report.Sections.Search.BaselineWithResults,
		report.Sections.Search.BaselineQueries,
		report.Sections.Search.TargetWithResults,
		report.Sections.Search.TargetQueries)
	fmt.Printf("  Avg Score: %.3f → %.3f\n",
		report.Sections.Search.BaselineAvgScore,
		report.Sections.Search.TargetAvgScore)
	fmt.Printf("  Known Answer Tests: %.0f%% → %.0f%%\n",
		report.Sections.Search.BaselineKnownAnswerPct,
		report.Sections.Search.TargetKnownAnswerPct)
	// Statistical comparison metrics
	fmt.Printf("  Avg Hit Overlap (Jaccard): %.2f\n", report.Sections.Search.AvgHitOverlap)
	if !math.IsNaN(report.Sections.Search.AvgScoreCorr) {
		fmt.Printf("  Avg Score Correlation: %.2f\n", report.Sections.Search.AvgScoreCorr)
	}
	fmt.Printf("  Target better: %d, Baseline better: %d, Tied: %d\n",
		report.Sections.Search.TargetBetterCnt,
		report.Sections.Search.BaselineBetterCnt,
		report.Sections.Search.TiedCount)
	fmt.Printf("  Verdict: %s\n", report.Sections.Search.Verdict)

	if len(report.Sections.Search.QueryDiffs) > 0 {
		fmt.Println("\n  Query-by-Query:")
		for _, qd := range report.Sections.Search.QueryDiffs {
			corrStr := ""
			if !math.IsNaN(qd.ScoreCorr) {
				corrStr = fmt.Sprintf(", corr=%.2f", qd.ScoreCorr)
			}
			fmt.Printf("    %q: %d → %d hits (overlap=%.2f%s) %s\n",
				truncate(qd.Query, 30),
				qd.BaselineHits,
				qd.TargetHits,
				qd.HitOverlap,
				corrStr,
				qd.Insight)
		}
	}
}

func printRulesSection(report *ComparisonReport) {
	fmt.Println("\n--- Rules ---")
	fmt.Printf("  Evaluated: %d → %d\n",
		report.Sections.Rules.BaselineEvaluated,
		report.Sections.Rules.TargetEvaluated)
	fmt.Printf("  Firings: %d → %d\n",
		report.Sections.Rules.BaselineFirings,
		report.Sections.Rules.TargetFirings)
	fmt.Printf("  Actions: %d → %d\n",
		report.Sections.Rules.BaselineActions,
		report.Sections.Rules.TargetActions)
	fmt.Printf("  Executions: %d → %d\n",
		report.Sections.Rules.BaselineExecs,
		report.Sections.Rules.TargetExecs)
}

func printCommunitiesSection(report *ComparisonReport) {
	if report.Sections.Communities == nil {
		return
	}
	fmt.Println("\n--- Communities ---")
	fmt.Printf("  Total: %d → %d (diff: %+d)\n",
		report.Sections.Communities.BaselineTotal,
		report.Sections.Communities.TargetTotal,
		report.Sections.Communities.TotalDiff)
	fmt.Printf("  Largest: %d → %d\n",
		report.Sections.Communities.BaselineLargest,
		report.Sections.Communities.TargetLargest)
}

func printAnomaliesSection(report *ComparisonReport) {
	if report.Sections.Anomalies == nil {
		return
	}
	fmt.Println("\n--- Anomaly Detection ---")
	fmt.Printf("  Total: %d → %d (diff: %+d)\n",
		report.Sections.Anomalies.BaselineTotal,
		report.Sections.Anomalies.TargetTotal,
		report.Sections.Anomalies.TotalDiff)
	fmt.Printf("  Semantic Gaps: %d → %d (diff: %+d)\n",
		report.Sections.Anomalies.BaselineSemanticGap,
		report.Sections.Anomalies.TargetSemanticGap,
		report.Sections.Anomalies.SemanticGapDiff)
	fmt.Printf("  Core Isolation: %d → %d (diff: %+d)\n",
		report.Sections.Anomalies.BaselineCoreIsolation,
		report.Sections.Anomalies.TargetCoreIsolation,
		report.Sections.Anomalies.CoreIsolationDiff)
	fmt.Printf("  Core Demotion: %d → %d (diff: %+d)\n",
		report.Sections.Anomalies.BaselineCoreDemotion,
		report.Sections.Anomalies.TargetCoreDemotion,
		report.Sections.Anomalies.CoreDemotionDiff)
	if report.Sections.Anomalies.BaselineTransitivity > 0 || report.Sections.Anomalies.TargetTransitivity > 0 {
		fmt.Printf("  Transitivity: %d → %d (diff: %+d)\n",
			report.Sections.Anomalies.BaselineTransitivity,
			report.Sections.Anomalies.TargetTransitivity,
			report.Sections.Anomalies.TransitivityDiff)
	}
}

func printPathRAGSection(report *ComparisonReport) {
	if report.Sections.PathRAG == nil {
		return
	}
	fmt.Println("\n--- PathRAG (Tier 0) ---")
	fmt.Printf("  Entities Found: %d → %d (diff: %+d)\n",
		report.Sections.PathRAG.BaselineEntities,
		report.Sections.PathRAG.TargetEntities,
		report.Sections.PathRAG.EntitiesDiff)
	fmt.Printf("  Latency: %dms → %dms\n",
		report.Sections.PathRAG.BaselineLatency,
		report.Sections.PathRAG.TargetLatency)
	fmt.Printf("  Scores Valid: %v\n", report.Sections.PathRAG.BothScoresValid)
}

func printGraphRAGSection(report *ComparisonReport) {
	if report.Sections.GraphRAG == nil {
		return
	}
	fmt.Println("\n--- GraphRAG (Tier 2) ---")
	fmt.Printf("  Local Query: %v → %v",
		report.Sections.GraphRAG.BaselineLocalSuccess,
		report.Sections.GraphRAG.TargetLocalSuccess)
	if report.Sections.GraphRAG.TargetLocalSuccess {
		fmt.Printf(" (%dms)", report.Sections.GraphRAG.TargetLocalLatency)
	}
	fmt.Println()
	fmt.Printf("  Global Query: %v → %v",
		report.Sections.GraphRAG.BaselineGlobalSuccess,
		report.Sections.GraphRAG.TargetGlobalSuccess)
	if report.Sections.GraphRAG.TargetGlobalSuccess {
		fmt.Printf(" (%dms)", report.Sections.GraphRAG.TargetGlobalLatency)
	}
	fmt.Println()
}

func printSummarySection(report *ComparisonReport) {
	fmt.Println("\n--- Summary ---")
	fmt.Printf("  Capability: %s\n", report.Summary.TierCapabilityDiff)
	fmt.Printf("  Search: %s\n", report.Summary.SearchImprovement)
	if len(report.Summary.Improvements) > 0 {
		fmt.Printf("  Improvements: %s\n", strings.Join(report.Summary.Improvements, "; "))
	}
	if len(report.Summary.Regressions) > 0 {
		fmt.Printf("  Regressions: %s\n", strings.Join(report.Summary.Regressions, "; "))
	}
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// --- Statistical comparison helper functions (ported from comparison.go) ---

// jaccard calculates Jaccard similarity between two string slices
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // Both empty = perfect match
	}

	setA := toSet(a)
	setB := toSet(b)

	intersection := 0
	for item := range setA {
		if setB[item] {
			intersection++
		}
	}

	union := len(setA)
	for item := range setB {
		if !setA[item] {
			union++
		}
	}

	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// toSet converts a slice to a set (map)
func toSet(items []string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range items {
		set[item] = true
	}
	return set
}

// scoreCorrelation calculates Pearson correlation for shared hits
func scoreCorrelation(baseScores, targetScores map[string]float64) float64 {
	// Find shared entity IDs
	var baseVals, targetVals []float64
	for entityID, baseScore := range baseScores {
		if targetScore, exists := targetScores[entityID]; exists {
			baseVals = append(baseVals, baseScore)
			targetVals = append(targetVals, targetScore)
		}
	}

	if len(baseVals) < 2 {
		return math.NaN() // Need at least 2 points for correlation
	}

	return pearsonCorrelation(baseVals, targetVals)
}

// pearsonCorrelation calculates the Pearson correlation coefficient
func pearsonCorrelation(x, y []float64) float64 {
	n := float64(len(x))
	if n == 0 {
		return 0
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := range x {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumX2 += x[i] * x[i]
		sumY2 += y[i] * y[i]
	}

	numerator := n*sumXY - sumX*sumY
	denominator := math.Sqrt((n*sumX2 - sumX*sumX) * (n*sumY2 - sumY*sumY))

	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// determineSearchVerdict generates the overall comparison verdict
func determineSearchVerdict(targetBetter, baselineBetter, tied int, avgOverlap float64) string {
	if targetBetter > baselineBetter+tied {
		return "Target provides significant improvement"
	}
	if targetBetter > baselineBetter {
		return "Target provides moderate improvement"
	}
	if baselineBetter > targetBetter {
		return "Baseline performs better for this dataset"
	}
	if avgOverlap > 0.7 {
		return "Marginal difference - results highly similar"
	}
	return "Mixed results - no clear winner"
}

// findLatestResult finds the latest structured result file for a variant
func findLatestResult(outputDir, variant string) (string, error) {
	pattern := filepath.Join(outputDir, fmt.Sprintf("%s-*.json", variant))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob failed: %w", err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no structured result files found for variant %s", variant)
	}

	// Sort by name (timestamp is in filename, so latest will be last)
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// handleAutoCompareCommand automatically finds latest results for two variants and compares
func handleAutoCompareCommand(logger *slog.Logger, outputDir, baselineVariant, targetVariant string) int {
	if outputDir == "" {
		outputDir = "test/e2e/results"
	}

	// Find latest files for each variant
	baselineFile, err := findLatestResult(outputDir, baselineVariant)
	if err != nil {
		logger.Error("Failed to find baseline", "variant", baselineVariant, "error", err)
		return 1
	}

	targetFile, err := findLatestResult(outputDir, targetVariant)
	if err != nil {
		logger.Error("Failed to find target", "variant", targetVariant, "error", err)
		return 1
	}

	logger.Info("Comparing latest results",
		"baseline", baselineFile,
		"target", targetFile)

	return handleCompareFilesCommand(logger, baselineFile, targetFile)
}
