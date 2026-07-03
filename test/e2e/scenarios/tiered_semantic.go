// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios/anomaly"
)

// Semantic tier validation functions (community comparison, LLM enhancement)

// detectCommunityVariant determines if running structural, statistical, or semantic variant
func (s *TieredScenario) detectCommunityVariant(result *Result) string {
	// Check if already detected in comparison stage
	if v, ok := result.Metrics["comparison_variant"].(string); ok {
		return v
	}
	// Check if variant was set in result metrics
	if v, ok := result.Metrics["variant"].(string); ok {
		return v
	}
	// Fallback to semembed detection
	if semembedAvailable, ok := result.Details["semembed_available"].(bool); ok && semembedAvailable {
		return "semantic"
	}
	return "statistical"
}

// waitForCommunities polls until communities are available.
// Community detection requires:
// 1. min_embedding_coverage (50% of entities have embeddings)
// 2. initial_delay (2s) + detection_interval (30s) to run
// So we need to wait at least 60 seconds for the first detection cycle.
func (s *TieredScenario) waitForCommunities(ctx context.Context) ([]*clustering.Community, error) {
	var communities []*clustering.Community
	var err error

	// First, wait for at least one clustering run to complete
	// This ensures community detection has actually executed
	startWait := time.Now()
	maxWait := 90 * time.Second // Allow time for initial_delay + detection_interval + processing
	pollInterval := 500 * time.Millisecond

	for time.Since(startWait) < maxWait {
		// Check if clustering has run
		clusteringRuns, _ := s.metrics.SumMetricsByName(ctx, "semstreams_clustering_runs_total")
		if clusteringRuns >= 1 {
			// Clustering has run, now check for communities
			communities, err = s.natsClient.GetAllCommunities(ctx)
			if err == nil && len(communities) > 0 {
				fmt.Printf("[COMMUNITY WAIT] Found %d communities after %.1fs (clustering_runs=%.0f)\n",
					len(communities), time.Since(startWait).Seconds(), clusteringRuns)
				return communities, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Final attempt after timeout
	communities, err = s.natsClient.GetAllCommunities(ctx)
	if len(communities) > 0 {
		fmt.Printf("[COMMUNITY WAIT] Found %d communities after timeout\n", len(communities))
		return communities, nil
	}

	fmt.Printf("[COMMUNITY WAIT] No communities found after %.1fs\n", time.Since(startWait).Seconds())
	return communities, err
}

// waitForLLMEnhancement waits for LLM enhancement to complete for ML variant
func (s *TieredScenario) waitForLLMEnhancement(
	ctx context.Context,
	communityCount int,
	result *Result,
) llmWaitResult {
	fmt.Printf("[LLM WAIT] Waiting for LLM enhancement to complete (ML variant, %d communities)...\n", communityCount)

	enhanceStart := time.Now()
	enhanced, failed, pending, waitErr := s.natsClient.WaitForCommunityEnhancement(
		ctx, 2*time.Minute, 2*time.Second,
	)
	waitResult := llmWaitResult{
		durationMs:   time.Since(enhanceStart).Milliseconds(),
		failedCount:  failed,
		pendingCount: pending,
	}

	fmt.Printf("[LLM WAIT] Complete: enhanced=%d, failed=%d, pending=%d, duration=%dms\n",
		enhanced, failed, pending, waitResult.durationMs)

	if waitErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("LLM enhancement wait error: %v", waitErr))
	}
	if enhanced == 0 && failed == 0 && pending > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("No LLM enhancements completed within 2 minute timeout (%d still pending)", pending))
	}

	result.Metrics["llm_wait_duration_ms"] = float64(waitResult.durationMs)
	result.Metrics["llm_failed_count"] = float64(waitResult.failedCount)
	result.Metrics["llm_pending_count"] = float64(waitResult.pendingCount)

	return waitResult
}

// analyzeCommunities computes statistics and comparisons for communities
func (s *TieredScenario) analyzeCommunities(communities []*clustering.Community) communityStats {
	stats := communityStats{comparisons: make([]CommunityComparison, 0, len(communities))}
	var totalLengthRatio, totalWordOverlap float64
	var ratioCount, totalNonSingletonMembers int

	for _, comm := range communities {
		comparison := s.buildCommunityComparison(comm, &totalLengthRatio, &totalWordOverlap, &ratioCount)

		if len(comm.Members) > 1 {
			stats.nonSingletonCount++
			totalNonSingletonMembers += len(comm.Members)
			if len(comm.Members) > stats.largestCommunitySize {
				stats.largestCommunitySize = len(comm.Members)
			}
		}

		switch comm.SummaryStatus {
		case "llm-enhanced":
			stats.llmEnhancedCount++
		case "statistical", "":
			stats.statisticalOnlyCount++
		}

		stats.comparisons = append(stats.comparisons, comparison)
	}

	if ratioCount > 0 {
		stats.avgLengthRatio = totalLengthRatio / float64(ratioCount)
		stats.avgWordOverlap = totalWordOverlap / float64(ratioCount)
	}
	if stats.nonSingletonCount > 0 {
		stats.avgNonSingletonSize = float64(totalNonSingletonMembers) / float64(stats.nonSingletonCount)
	}

	return stats
}

// buildCommunityComparison creates a comparison record for a single community
func (s *TieredScenario) buildCommunityComparison(
	comm *clustering.Community,
	totalLengthRatio, totalWordOverlap *float64,
	ratioCount *int,
) CommunityComparison {
	comparison := CommunityComparison{
		CommunityID:        comm.ID,
		Level:              comm.Level,
		MemberCount:        len(comm.Members),
		StatisticalSummary: comm.StatisticalSummary,
		LLMSummary:         comm.LLMSummary,
		SummaryStatus:      comm.SummaryStatus,
		Keywords:           comm.Keywords,
	}

	if comm.LLMSummary != "" && comm.StatisticalSummary != "" && len(comm.StatisticalSummary) > 0 {
		comparison.SummaryLengthRatio = float64(len(comm.LLMSummary)) / float64(len(comm.StatisticalSummary))
		*totalLengthRatio += comparison.SummaryLengthRatio
		*ratioCount++
		comparison.WordOverlap = wordJaccard(comm.StatisticalSummary, comm.LLMSummary)
		*totalWordOverlap += comparison.WordOverlap
	}

	return comparison
}

// llmQualityIssue represents a quality issue found in LLM summaries
type llmQualityIssue struct {
	CommunityID string
	Issue       string
}

// validateLLMSummaryQuality validates quality of LLM-enhanced community summaries
func (s *TieredScenario) validateLLMSummaryQuality(communities []*clustering.Community) []llmQualityIssue {
	var issues []llmQualityIssue

	for _, comm := range communities {
		if comm.SummaryStatus != "llm-enhanced" {
			continue
		}

		// Check minimum summary length (50 chars)
		if len(comm.LLMSummary) < 50 {
			issues = append(issues, llmQualityIssue{
				CommunityID: comm.ID,
				Issue:       fmt.Sprintf("LLM summary too short: %d chars (min 50)", len(comm.LLMSummary)),
			})
			continue
		}

		// Check that at least one keyword appears in the summary
		keywordFound := false
		summaryLower := strings.ToLower(comm.LLMSummary)
		for _, kw := range comm.Keywords {
			if strings.Contains(summaryLower, strings.ToLower(kw)) {
				keywordFound = true
				break
			}
		}

		if !keywordFound && len(comm.Keywords) > 0 {
			issues = append(issues, llmQualityIssue{
				CommunityID: comm.ID,
				Issue:       fmt.Sprintf("LLM summary contains no keywords (keywords: %v)", comm.Keywords),
			})
		}

		// Check that LLM summary is more detailed (longer) than statistical summary
		if comm.StatisticalSummary != "" && len(comm.LLMSummary) <= len(comm.StatisticalSummary) {
			issues = append(issues, llmQualityIssue{
				CommunityID: comm.ID,
				Issue: fmt.Sprintf("LLM summary (%d chars) not longer than statistical (%d chars)",
					len(comm.LLMSummary), len(comm.StatisticalSummary)),
			})
		}
	}

	return issues
}

// persistCommunityReport saves the community comparison report to a JSON file
func (s *TieredScenario) persistCommunityReport(
	variant string,
	stats communityStats,
	llmWait llmWaitResult,
	result *Result,
) string {
	if s.config.OutputDir == "" {
		return ""
	}

	report := CommunitySummaryReport{
		Variant:               variant,
		Timestamp:             time.Now(),
		CommunitiesTotal:      len(stats.comparisons),
		LLMEnhancedCount:      stats.llmEnhancedCount,
		StatisticalOnlyCount:  stats.statisticalOnlyCount,
		LLMFailedCount:        llmWait.failedCount,
		LLMPendingCount:       llmWait.pendingCount,
		LLMWaitDurationMs:     llmWait.durationMs,
		AvgSummaryLengthRatio: stats.avgLengthRatio,
		AvgWordOverlap:        stats.avgWordOverlap,
		NonSingletonCount:     stats.nonSingletonCount,
		LargestCommunitySize:  stats.largestCommunitySize,
		AvgNonSingletonSize:   stats.avgNonSingletonSize,
		Communities:           stats.comparisons,
	}

	filename := fmt.Sprintf("community-comparison-%s-%s.json", variant, time.Now().Format("20060102-150405"))
	comparisonFile := filepath.Join(s.config.OutputDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		if err := os.WriteFile(comparisonFile, data, 0644); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to write community comparison file: %v", err))
		}
	}

	return comparisonFile
}

// recordCommunityMetrics records community statistics to result metrics
func (s *TieredScenario) recordCommunityMetrics(stats communityStats, result *Result) {
	result.Metrics["communities_total"] = len(stats.comparisons)
	result.Metrics["communities_llm_enhanced"] = stats.llmEnhancedCount
	result.Metrics["communities_statistical_only"] = stats.statisticalOnlyCount
	result.Metrics["avg_summary_length_ratio"] = stats.avgLengthRatio
	result.Metrics["avg_word_overlap"] = stats.avgWordOverlap
	result.Metrics["communities_non_singleton"] = stats.nonSingletonCount
	result.Metrics["largest_community_size"] = stats.largestCommunitySize
	result.Metrics["avg_non_singleton_size"] = stats.avgNonSingletonSize
}

// executeValidateLLMEnhancement validates LLM enhancement of communities for semantic tier.
// This step waits for LLM enhancement to complete (up to 2 min), analyzes community
// summary status, and validates that enhancement is working properly.
func (s *TieredScenario) executeValidateLLMEnhancement(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping LLM enhancement validation")
		return nil
	}

	fmt.Println("[LLM ENHANCEMENT] Starting LLM enhancement validation...")

	// Wait for communities to be available
	communities, err := s.waitForCommunities(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get communities: %v", err))
		return nil
	}

	if len(communities) == 0 {
		result.Warnings = append(result.Warnings, "No communities found for LLM enhancement validation")
		return nil
	}

	fmt.Printf("[LLM ENHANCEMENT] Found %d communities, waiting for LLM enhancement...\n", len(communities))

	// Wait for LLM enhancement to complete
	llmWait := s.waitForLLMEnhancement(ctx, len(communities), result)

	// Re-fetch communities after waiting (they may have been updated)
	communities, err = s.natsClient.GetAllCommunities(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to re-fetch communities after LLM wait: %v", err))
		return nil
	}

	// Analyze communities for summary status
	stats := s.analyzeCommunities(communities)

	// Record metrics
	s.recordCommunityMetrics(stats, result)

	// Persist detailed report
	variant := s.detectCommunityVariant(result)
	reportFile := s.persistCommunityReport(variant, stats, llmWait, result)
	if reportFile != "" {
		fmt.Printf("[LLM ENHANCEMENT] Wrote community report to %s\n", reportFile)
	}

	// Validate LLM summary quality for enhanced communities
	issues := s.validateLLMSummaryQuality(communities)
	if len(issues) > 0 {
		for _, issue := range issues {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("LLM quality issue in %s: %s", issue.CommunityID, issue.Issue))
		}
	}

	// Log summary
	fmt.Printf("[LLM ENHANCEMENT] Results: llm_enhanced=%d, statistical_only=%d, failed=%d, pending=%d\n",
		stats.llmEnhancedCount, stats.statisticalOnlyCount, llmWait.failedCount, llmWait.pendingCount)

	// Validate enhancement is working
	if stats.llmEnhancedCount == 0 {
		if llmWait.failedCount > 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("LLM enhancement failed for all %d communities - check seminstruct logs", llmWait.failedCount))
		} else if llmWait.pendingCount > 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("No LLM enhancements completed within timeout (%d still pending) - enhancement may be slow or worker not started", llmWait.pendingCount))
		} else {
			result.Warnings = append(result.Warnings,
				"No communities have LLM enhancement (all show statistical status) - verify enhancement worker is enabled")
		}
	} else {
		fmt.Printf("[LLM ENHANCEMENT] Success: %d/%d communities LLM-enhanced\n",
			stats.llmEnhancedCount, len(communities))
	}

	return nil
}

// executeValidateAnomalyDetection validates structural anomaly detection results for semantic tier.
// This step waits for anomaly detection to complete, then retrieves anomaly counts from the
// ANOMALY_INDEX KV bucket and records metrics for semantic gaps (pivot distance), core anomalies
// (k-core analysis), and transitivity gaps.
func (s *TieredScenario) executeValidateAnomalyDetection(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping anomaly detection validation")
		return nil
	}

	fmt.Println("[ANOMALY DETECTION] Waiting for anomaly detection to complete...")

	// Wait for anomaly detection to stabilize (30s timeout, 2s poll interval)
	// Anomaly detection runs asynchronously during community detection, so we need to wait
	// for it to complete before reading final counts
	total, waitErr := s.natsClient.WaitForAnomalyDetection(ctx, 30*time.Second, 2*time.Second)
	if waitErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Anomaly detection wait error: %v", waitErr))
	}

	fmt.Printf("[ANOMALY DETECTION] Detection complete, found %d anomalies\n", total)

	// Get anomaly counts by type and status
	counts, err := s.natsClient.GetAnomalyCounts(ctx)
	if err != nil {
		// Anomaly detection may not have run or bucket may not exist - this is a warning, not error
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get anomaly counts: %v", err))
		result.Metrics["anomalies_total"] = 0
		result.Metrics["anomalies_semantic_gap"] = 0
		result.Metrics["anomalies_core_isolation"] = 0
		result.Metrics["anomalies_core_demotion"] = 0
		result.Metrics["anomalies_transitivity"] = 0
		return nil
	}

	// Record metrics by anomaly type
	result.Metrics["anomalies_total"] = counts.Total
	result.Metrics["anomalies_semantic_gap"] = counts.ByType["semantic_structural_gap"]
	result.Metrics["anomalies_core_isolation"] = counts.ByType["core_isolation"]
	result.Metrics["anomalies_core_demotion"] = counts.ByType["core_demotion"]
	result.Metrics["anomalies_transitivity"] = counts.ByType["transitivity_gap"]

	// Record metrics by status
	result.Metrics["anomalies_pending"] = counts.ByStatus["pending"]
	result.Metrics["anomalies_confirmed"] = counts.ByStatus["confirmed"]
	result.Metrics["anomalies_dismissed"] = counts.ByStatus["dismissed"]

	// Log results
	fmt.Printf("[ANOMALY DETECTION] Results: total=%d, semantic_gap=%d, core_isolation=%d, core_demotion=%d, transitivity=%d\n",
		counts.Total,
		counts.ByType["semantic_structural_gap"],
		counts.ByType["core_isolation"],
		counts.ByType["core_demotion"],
		counts.ByType["transitivity_gap"])

	fmt.Printf("[ANOMALY DETECTION] Status: pending=%d, confirmed=%d, dismissed=%d\n",
		counts.ByStatus["pending"],
		counts.ByStatus["confirmed"],
		counts.ByStatus["dismissed"])

	// Validation: at least some anomalies should be detected for semantic tier
	if counts.Total == 0 {
		result.Warnings = append(result.Warnings,
			"No anomalies detected - verify anomaly detection is enabled and running during community detection")
	} else {
		fmt.Printf("[ANOMALY DETECTION] Success: %d total anomalies detected\n", counts.Total)
	}

	// Semantic gap detector requires embeddings - should have results for semantic tier
	if counts.ByType["semantic_structural_gap"] == 0 {
		result.Warnings = append(result.Warnings,
			"No semantic gap anomalies detected - verify semembed is available and pivot index is built")
	}

	// Run anomaly ground truth validation (false positive detection)
	groundTruthResult := s.validateAnomalyGroundTruth(ctx, result)
	if groundTruthResult != nil && !groundTruthResult.Passed() {
		// Record violations as warnings (don't fail the test, just report)
		for _, v := range groundTruthResult.Violations {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Anomaly ground truth violation [%s]: %s",
					v.Type, v.Details))
		}
	}

	return nil
}

// validateAnomalyGroundTruth runs false positive detection against known related entity pairs.
func (s *TieredScenario) validateAnomalyGroundTruth(ctx context.Context, result *Result) *anomaly.ValidationResult {
	// Get actual anomalies for ground truth validation
	anomalies, err := s.natsClient.GetAnomalies(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Failed to get anomalies for ground truth validation: %v", err))
		return nil
	}

	// Store actual anomalies for auditability - this is critical for verifying
	// false positive detection is working correctly
	anomalyList := make([]map[string]any, 0, len(anomalies))
	for _, a := range anomalies {
		anomalyList = append(anomalyList, map[string]any{
			"id":         a.ID,
			"type":       a.Type,
			"entity_a":   a.EntityA,
			"entity_b":   a.EntityB,
			"confidence": a.Confidence,
			"status":     a.Status,
		})
	}
	result.Details["anomaly_list"] = anomalyList

	validator := anomaly.NewDefaultValidator()
	groundTruthResult := validator.Validate(anomalies)

	// Record metrics
	result.Metrics["anomaly_ground_truth_expected"] = groundTruthResult.ExpectedTotal
	result.Metrics["anomaly_ground_truth_found"] = groundTruthResult.ExpectedFound
	result.Metrics["anomaly_false_positives"] = groundTruthResult.FalsePositiveTotal

	// Calculate and record false positive rate
	falsePositiveRate := 0.0
	if groundTruthResult.DetectedTotal > 0 {
		falsePositiveRate = float64(groundTruthResult.FalsePositiveTotal) / float64(groundTruthResult.DetectedTotal)
	}
	result.Metrics["anomaly_false_positive_rate"] = falsePositiveRate

	// Record detailed results
	violationDetails := make([]map[string]any, 0, len(groundTruthResult.Violations))
	for _, v := range groundTruthResult.Violations {
		vDetail := map[string]any{
			"type":    string(v.Type),
			"details": v.Details,
		}
		if v.EntityPair != nil {
			vDetail["entity_a"] = v.EntityPair.EntityA
			vDetail["entity_b"] = v.EntityPair.EntityB
		}
		if v.AnomalyID != "" {
			vDetail["anomaly_id"] = v.AnomalyID
		}
		violationDetails = append(violationDetails, vDetail)
	}

	result.Details["anomaly_ground_truth"] = map[string]any{
		"expected_total":       groundTruthResult.ExpectedTotal,
		"expected_found":       groundTruthResult.ExpectedFound,
		"false_positive_total": groundTruthResult.FalsePositiveTotal,
		"detected_total":       groundTruthResult.DetectedTotal,
		"false_positive_rate":  falsePositiveRate,
		"passed":               groundTruthResult.Passed(),
		"violations":           violationDetails,
	}

	return groundTruthResult
}

// executeValidateVirtualEdges validates that high-confidence semantic gaps are auto-applied as virtual edges.
// This step checks for inferred.semantic.* predicates in the PREDICATE_INDEX and correlates them
// with auto_applied status anomalies in the ANOMALY_INDEX.
func (s *TieredScenario) executeValidateVirtualEdges(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping virtual edge validation")
		return nil
	}

	fmt.Println("[VIRTUAL EDGES] Validating virtual edge creation from semantic gaps...")

	// Get virtual edge counts via the query API (ADR-065). A query/parse
	// failure here is a hard failure, not a warning: CountVirtualEdges
	// itself now distinguishes "legitimately zero" (nil error, zero count —
	// handled below) from "couldn't determine the count" (non-nil error).
	// Silently warning on the latter is exactly the failure class this
	// stage used to have (a raw-bucket reader whose unmarshal errors were
	// swallowed, quietly validating nothing while reporting green).
	edgeCounts, err := s.natsClient.CountVirtualEdges(ctx)
	if err != nil {
		return fmt.Errorf("failed to count virtual edges: %w", err)
	}

	// Get auto-applied anomaly count from ANOMALY_INDEX
	autoApplied, err := s.natsClient.GetAutoAppliedAnomalyCount(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get auto-applied count: %v", err))
	}

	// Record metrics
	result.Metrics["virtual_edges_total"] = edgeCounts.Total
	result.Metrics["virtual_edges_high"] = edgeCounts.ByBand["high"]
	result.Metrics["virtual_edges_medium"] = edgeCounts.ByBand["medium"]
	result.Metrics["virtual_edges_related"] = edgeCounts.ByBand["related"]
	result.Metrics["anomalies_auto_applied"] = autoApplied

	// Log results
	fmt.Printf("[VIRTUAL EDGES] Results: total=%d, high=%d, medium=%d, related=%d, auto_applied_anomalies=%d\n",
		edgeCounts.Total,
		edgeCounts.ByBand["high"],
		edgeCounts.ByBand["medium"],
		edgeCounts.ByBand["related"],
		autoApplied)

	// Validation: check if virtual edges were created when auto-apply is enabled
	if edgeCounts.Total == 0 && autoApplied == 0 {
		// This could be expected if no semantic gaps met the auto-apply threshold
		fmt.Println("[VIRTUAL EDGES] No virtual edges created - this may be expected if no gaps met auto-apply threshold (similarity >= 0.85, distance >= 4)")
	} else if edgeCounts.Total > 0 {
		fmt.Printf("[VIRTUAL EDGES] Success: %d virtual edges created from semantic gaps\n", edgeCounts.Total)
	}

	// Warn if there's a mismatch between auto-applied anomalies and virtual edges
	// Note: The counts may not match exactly because edges are created in PREDICATE_INDEX
	// as a side effect of the triple being added, while auto_applied status is on anomalies
	if autoApplied > 0 && edgeCounts.Total == 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Anomalies marked auto_applied (%d) but no virtual edges found in PREDICATE_INDEX", autoApplied))
	}

	return nil
}

// NOTE: executeCompareCommunities removed - use CLI compare instead:
//   ./e2e --compare-structured --baseline results/statistical.json --target results/semantic.json
// Community data is captured in structured results by executeValidateCommunityStructure.

// validateEmbeddingQueueHealth validates that the embedding queue has drained and no failures occurred.
// This function should be called after executeWaitForEmbeddings to verify queue health.
// Phase 4: Added to ensure embedding pipeline is fully complete before proceeding.
func (s *TieredScenario) validateEmbeddingQueueHealth(ctx context.Context, result *Result) error {
	fmt.Println("[EMBEDDING QUEUE] Validating embedding queue health...")

	// Fetch embedding queue metrics from Prometheus using SumMetricsByName
	pending, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_pending")
	failed, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_errors_total")
	dedupHits, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_dedup_hits_total")
	generated, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_embeddings_generated_total")
	queued, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_queued_total")

	// Record metrics for structured results
	result.Metrics["embedding_queued_total"] = int64(queued)
	result.Metrics["embedding_generated_total"] = int64(generated)
	result.Metrics["embedding_dedup_hits"] = int64(dedupHits)
	result.Metrics["embedding_failed_total"] = int64(failed)
	result.Metrics["embedding_pending_count"] = int64(pending)

	// Log queue stats for observability
	fmt.Printf("[EMBEDDING QUEUE] Stats: generated=%.0f, dedup_hits=%.0f, failed=%.0f, pending=%.0f\n",
		generated, dedupHits, failed, pending)

	// Validate queue is drained
	if pending > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Embedding queue not fully drained: %.0f pending items", pending))
	}

	// Validate no failures
	if failed > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Embedding failures detected: %.0f failed", failed))
	}

	// Calculate and log dedup efficiency
	if queued > 0 {
		dedupRate := dedupHits / queued * 100
		fmt.Printf("[EMBEDDING QUEUE] Dedup efficiency: %.1f%% (%.0f hits / %.0f queued)\n",
			dedupRate, dedupHits, queued)
	}

	result.Details["embedding_queue_health"] = map[string]any{
		"queued_total":    queued,
		"generated_total": generated,
		"dedup_hits":      dedupHits,
		"failed_total":    failed,
		"pending_count":   pending,
		"queue_drained":   pending == 0,
		"no_failures":     failed == 0,
	}

	if pending == 0 && failed == 0 {
		fmt.Println("[EMBEDDING QUEUE] Health check passed: queue drained, no failures")
	}

	return nil
}

// validateHierarchyInference validates that hierarchy inference is creating container entities.
// This validates that the KV watcher pattern (Phase 3 refactor) is working correctly.
// Phase 4: Added to verify hierarchy container creation from ENTITY_STATES watcher.
// Phase 8: Uses SSE streaming to wait for container groups before counting.
func (s *TieredScenario) validateHierarchyInference(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping hierarchy inference validation")
		return nil
	}

	fmt.Println("[HIERARCHY] Validating hierarchy inference container creation...")

	// Wait for container groups to stabilize using SSE streaming
	// Expected: ~20+ containers for the 74 entities in testdata/semantic/
	const expectedMinGroups = 20
	groupCount, usedSSE, _ := s.natsClient.WaitForContainerGroupsSSE(
		ctx,
		expectedMinGroups,
		s.config.ValidationTimeout,
		s.sseClient,
	)
	result.Details["hierarchy_wait_used_sse"] = usedSSE
	result.Metrics["hierarchy_groups_found_early"] = groupCount

	// Get all entity IDs from ENTITY_STATES bucket
	allIDs, err := s.natsClient.GetAllEntityIDs(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get entity IDs: %v", err))
		return nil
	}

	// Count containers and source entities (non-container entities from testdata)
	containerCount := 0
	sourceEntityCount := 0
	containerTypes := make(map[string]int)

	for _, id := range allIDs {
		if isContainerEntity(id) {
			containerCount++
			// Track container types by suffix
			if strings.HasSuffix(id, ".group.container.level") {
				containerTypes["level"]++
			} else if strings.HasSuffix(id, ".group.container") {
				containerTypes["container"]++
			} else if strings.HasSuffix(id, ".group") {
				containerTypes["group"]++
			}
		} else {
			sourceEntityCount++
		}
	}

	// Expected minimum containers based on source entities
	// Rule of thumb: ~40-70% as many containers as source entities due to hierarchical grouping
	expectedMinContainers := sourceEntityCount * 4 / 10 // 40% minimum

	// Record metrics for structured results
	result.Metrics["hierarchy_container_count"] = containerCount
	result.Metrics["hierarchy_source_entity_count"] = sourceEntityCount
	result.Metrics["hierarchy_expected_min_containers"] = expectedMinContainers

	// Log results
	fmt.Printf("[HIERARCHY] Found %d containers, %d source entities (expected min containers: %d)\n",
		containerCount, sourceEntityCount, expectedMinContainers)
	fmt.Printf("[HIERARCHY] Container types: group=%d, container=%d, level=%d\n",
		containerTypes["group"], containerTypes["container"], containerTypes["level"])

	// Validation: check if hierarchy inference is working
	if containerCount < expectedMinContainers {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Hierarchy inference may not be working: only %d containers for %d source entities (expected at least %d)",
				containerCount, sourceEntityCount, expectedMinContainers))
	} else {
		fmt.Printf("[HIERARCHY] Success: hierarchy inference validated (%d containers created)\n", containerCount)
	}

	result.Details["hierarchy_inference"] = map[string]any{
		"container_count":         containerCount,
		"source_entity_count":     sourceEntityCount,
		"expected_min_containers": expectedMinContainers,
		"inference_working":       containerCount >= expectedMinContainers,
		"container_types":         containerTypes,
	}

	return nil
}

// isContainerEntity checks if an entity ID represents a hierarchy container.
// Container entities are auto-created by HierarchyInference and have specific suffixes.
func isContainerEntity(entityID string) bool {
	return strings.HasSuffix(entityID, ".group") ||
		strings.HasSuffix(entityID, ".group.container") ||
		strings.HasSuffix(entityID, ".group.container.level")
}

// wordJaccard calculates Jaccard similarity on word sets
func wordJaccard(a, b string) float64 {
	wordsA := toWordSet(strings.ToLower(a))
	wordsB := toWordSet(strings.ToLower(b))

	intersection := 0
	for word := range wordsA {
		if wordsB[word] {
			intersection++
		}
	}

	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// toWordSet converts a string to a set of words (excluding short words and punctuation)
func toWordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool)
	for _, w := range words {
		// Remove punctuation
		w = strings.Trim(w, ".,!?;:()[]{}\"'")
		// Skip short words (less than 3 characters)
		if len(w) > 2 {
			set[w] = true
		}
	}
	return set
}

// validateContextIndexHierarchy validates that the ContextIndex is tracking inference provenance.
// Phase 5: Verifies that hierarchy inference triples are tracked in CONTEXT_INDEX.
func (s *TieredScenario) validateContextIndexHierarchy(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for context index validation")
		return nil
	}

	fmt.Println("[CONTEXT INDEX] Validating context index hierarchy tracking...")

	// Count CONTEXT_INDEX keys (each key is a context value like "inference.hierarchy")
	count, err := s.natsClient.CountBucketKeys(ctx, client.IndexBuckets.Context)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("context index query failed: %v", err))
		return nil
	}

	// Sample keys to verify hierarchy inference context exists
	samples, _ := s.natsClient.GetBucketKeysSample(ctx, client.IndexBuckets.Context, 10)

	// Check if "inference.hierarchy" key exists
	hierarchyContextFound := false
	for _, key := range samples {
		if key == "inference.hierarchy" {
			hierarchyContextFound = true
			break
		}
	}

	// Get hierarchy context entries to count entity+predicate pairs
	hierarchyEntryCount := 0
	if hierarchyContextFound {
		entries, err := s.natsClient.GetContextEntries(ctx, "inference.hierarchy")
		if err == nil {
			hierarchyEntryCount = len(entries)
		}
	}

	// Record metrics
	result.Metrics["context_index_keys"] = count
	result.Metrics["context_hierarchy_found"] = boolToInt(hierarchyContextFound)
	result.Metrics["context_hierarchy_entries"] = hierarchyEntryCount

	// Log results
	fmt.Printf("[CONTEXT INDEX] Results: total_keys=%d, hierarchy_found=%v, hierarchy_entries=%d\n",
		count, hierarchyContextFound, hierarchyEntryCount)
	if len(samples) > 0 {
		fmt.Printf("[CONTEXT INDEX] Sample contexts: %v\n", samples)
	}

	// Validation warnings (tier-aware: structural tier has short run, async index may not complete)
	if count == 0 {
		if s.config.Variant == "structural" {
			// Structural tier runs quickly - async context indexing may not complete in time
			// Hierarchy inference validation already confirms containers are created
			fmt.Println("[CONTEXT INDEX] Note: Context index empty (expected in short structural tier run)")
		} else {
			result.Warnings = append(result.Warnings, "CONTEXT_INDEX is empty - hierarchy inference may not be running")
		}
	} else if !hierarchyContextFound {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("CONTEXT_INDEX has %d keys but 'inference.hierarchy' not found in sample", count))
	} else {
		fmt.Printf("[CONTEXT INDEX] Success: hierarchy inference provenance tracked (%d entries)\n", hierarchyEntryCount)
	}

	result.Details["context_index_validation"] = map[string]any{
		"total_keys":             count,
		"hierarchy_found":        hierarchyContextFound,
		"hierarchy_entry_count":  hierarchyEntryCount,
		"sample_contexts":        samples,
		"provenance_tracking_ok": hierarchyContextFound && hierarchyEntryCount > 0,
	}

	return nil
}

// validateIncomingIndexPredicates validates that IncomingIndex stores predicate information.
// Phase 5: Verifies the IncomingIndex asymmetry fix is working (stores []IncomingEntry, not []string).
func (s *TieredScenario) validateIncomingIndexPredicates(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for incoming index validation")
		return nil
	}

	fmt.Println("[INCOMING INDEX] Validating incoming index predicate storage...")

	// Get all entity IDs to find a container entity
	allIDs, err := s.natsClient.GetAllEntityIDs(ctx)
	if err != nil || len(allIDs) == 0 {
		result.Warnings = append(result.Warnings, "No entities found for incoming index validation")
		return nil
	}

	// Look for a .group entity (created by hierarchy inference, has incoming edges)
	var containerID string
	for _, id := range allIDs {
		if strings.HasSuffix(id, ".group") {
			containerID = id
			break
		}
	}

	if containerID == "" {
		// No container entities - may be structural tier (no hierarchy inference)
		result.Metrics["incoming_predicate_validation"] = 0
		result.Details["incoming_index_validation"] = map[string]any{
			"container_found":      false,
			"message":              "No container entities found (hierarchy inference may not have run)",
			"predicate_validation": false,
		}
		return nil
	}

	// Get incoming entries for the container
	entries, err := s.natsClient.GetIncomingEntries(ctx, containerID)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("incoming entries query failed: %v", err))
		return nil
	}

	// Verify predicates are stored (not just entity IDs)
	predicateCount := 0
	hierarchyMemberCount := 0
	uniquePredicates := make(map[string]int)

	for _, entry := range entries {
		if entry.Predicate != "" {
			predicateCount++
			uniquePredicates[entry.Predicate]++
			if entry.Predicate == "hierarchy.type.member" {
				hierarchyMemberCount++
			}
		}
	}

	// Record metrics
	result.Metrics["incoming_entries_total"] = len(entries)
	result.Metrics["incoming_entries_with_predicates"] = predicateCount
	result.Metrics["incoming_hierarchy_member_count"] = hierarchyMemberCount
	result.Metrics["incoming_predicate_validation"] = boolToInt(predicateCount > 0)

	// Log results
	fmt.Printf("[INCOMING INDEX] Results: container=%s, total_entries=%d, with_predicates=%d, hierarchy_member=%d\n",
		containerID, len(entries), predicateCount, hierarchyMemberCount)
	if len(uniquePredicates) > 0 {
		fmt.Printf("[INCOMING INDEX] Unique predicates: %v\n", uniquePredicates)
	}

	// Validation
	predicateValidation := predicateCount > 0
	if len(entries) > 0 && predicateCount == 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("IncomingIndex has %d entries but none have predicates - index may use old []string format", len(entries)))
	} else if predicateValidation {
		fmt.Printf("[INCOMING INDEX] Success: bidirectional traversal preserves predicates (%d entries with predicates)\n", predicateCount)
	}

	result.Details["incoming_index_validation"] = map[string]any{
		"container_id":            containerID,
		"total_entries":           len(entries),
		"entries_with_predicates": predicateCount,
		"hierarchy_member_count":  hierarchyMemberCount,
		"unique_predicates":       uniquePredicates,
		"predicate_validation":    predicateValidation,
	}

	return nil
}

// validateContextProvenanceAudit demonstrates context-based provenance queries.
// Phase 6: Story - "As a system admin, I can audit which relationships came from inference."
func (s *TieredScenario) validateContextProvenanceAudit(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for provenance audit")
		return nil
	}

	fmt.Println("[PROVENANCE AUDIT] Demonstrating context-based provenance queries...")

	// 1. Get all inference contexts in use
	allContexts, err := s.natsClient.GetAllContexts(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to get contexts: %v", err))
		return nil
	}

	// 2. Query entities created by hierarchy inference
	hierarchyEntries, _ := s.natsClient.GetContextEntries(ctx, "inference.hierarchy")

	// 3. Extract unique entity IDs (entities touched by hierarchy inference)
	entitySet := make(map[string]bool)
	for _, entry := range hierarchyEntries {
		entitySet[entry.EntityID] = true
	}

	// 4. Cross-reference: count container entities
	containerCount := 0
	for id := range entitySet {
		if strings.HasSuffix(id, ".group") ||
			strings.HasSuffix(id, ".group.container") ||
			strings.HasSuffix(id, ".group.container.level") {
			containerCount++
		}
	}

	// Record metrics
	result.Metrics["provenance_contexts_found"] = len(allContexts)
	result.Metrics["provenance_hierarchy_entities"] = len(entitySet)
	result.Metrics["provenance_containers_identified"] = containerCount

	// Log results
	fmt.Printf("[PROVENANCE AUDIT] Found %d contexts, %d hierarchy-inferred entities, %d containers\n",
		len(allContexts), len(entitySet), containerCount)
	if len(allContexts) > 0 {
		fmt.Printf("[PROVENANCE AUDIT] Contexts: %v\n", allContexts)
	}

	// Validation
	if len(allContexts) > 0 && len(entitySet) > 0 {
		fmt.Println("[PROVENANCE AUDIT] Success: Can audit which entities came from inference")
	} else if len(allContexts) == 0 {
		result.Warnings = append(result.Warnings,
			"No provenance contexts found - ContextIndex may not be populated")
	}

	result.Details["provenance_audit"] = map[string]any{
		"contexts_found":       allContexts,
		"hierarchy_entities":   len(entitySet),
		"containers_found":     containerCount,
		"provenance_available": len(allContexts) > 0,
	}

	return nil
}

// validateBidirectionalTraversal demonstrates predicate-aware reverse traversal.
// Phase 6: Story - "As an app developer, I can find who references a container and WHY."
func (s *TieredScenario) validateBidirectionalTraversal(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for bidirectional traversal")
		return nil
	}

	fmt.Println("[BIDIRECTIONAL] Demonstrating predicate-aware reverse traversal...")

	// Get all entity IDs to find a container
	allIDs, err := s.natsClient.GetAllEntityIDs(ctx)
	if err != nil || len(allIDs) == 0 {
		result.Warnings = append(result.Warnings, "No entities found for bidirectional traversal")
		return nil
	}

	// Find a .group container entity
	var containerID string
	for _, id := range allIDs {
		if strings.HasSuffix(id, ".group") {
			containerID = id
			break
		}
	}

	if containerID == "" {
		result.Metrics["bidir_predicate_preserved"] = 0
		result.Details["bidirectional_traversal"] = map[string]any{
			"container_found": false,
			"message":         "No container entities found (hierarchy inference may not have run)",
		}
		return nil
	}

	// Get incoming relationships WITH predicate information
	incomingEntries, err := s.natsClient.GetIncomingEntries(ctx, containerID)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("incoming entries query failed: %v", err))
		return nil
	}

	// Filter by predicate type - "Who are the MEMBERS of this container?"
	memberCount := 0
	for _, entry := range incomingEntries {
		if entry.Predicate == "hierarchy.type.member" {
			memberCount++
		}
	}

	// Get outgoing relationships from container
	outgoingEntries, _ := s.natsClient.GetOutgoingEntries(ctx, containerID)

	// Record metrics
	result.Metrics["bidir_incoming_total"] = len(incomingEntries)
	result.Metrics["bidir_member_count"] = memberCount
	result.Metrics["bidir_outgoing_total"] = len(outgoingEntries)
	result.Metrics["bidir_predicate_preserved"] = boolToInt(memberCount > 0)

	// Log results
	fmt.Printf("[BIDIRECTIONAL] Container: %s\n", containerID)
	fmt.Printf("[BIDIRECTIONAL] Incoming edges: %d total, %d are 'member' relationships\n",
		len(incomingEntries), memberCount)
	fmt.Printf("[BIDIRECTIONAL] Outgoing edges: %d\n", len(outgoingEntries))

	// Verify we can answer "who points to this container and why?"
	if memberCount > 0 {
		fmt.Printf("[BIDIRECTIONAL] Sample member: %s → hierarchy.type.member → %s\n",
			incomingEntries[0].FromEntityID, containerID)
		fmt.Println("[BIDIRECTIONAL] Success: Can traverse graph in both directions with relationship types")
	}

	result.Details["bidirectional_traversal"] = map[string]any{
		"container_id":       containerID,
		"incoming_total":     len(incomingEntries),
		"member_count":       memberCount,
		"outgoing_total":     len(outgoingEntries),
		"predicates_present": memberCount > 0,
	}

	return nil
}

// validateInverseEdgesMaterialized validates that inverse edges are created by hierarchy inference.
// Phase 6: Story - "As a graph analyst, containers explicitly know their members via 'contains' edges."
func (s *TieredScenario) validateInverseEdgesMaterialized(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for inverse edges validation")
		return nil
	}

	fmt.Println("[INVERSE EDGES] Demonstrating materialized inverse relationships...")

	// Get all entity IDs to find a container
	allIDs, err := s.natsClient.GetAllEntityIDs(ctx)
	if err != nil || len(allIDs) == 0 {
		result.Warnings = append(result.Warnings, "No entities found for inverse edges validation")
		return nil
	}

	// Find a .group container entity
	var containerID string
	for _, id := range allIDs {
		if strings.HasSuffix(id, ".group") {
			containerID = id
			break
		}
	}

	if containerID == "" {
		result.Metrics["inverse_symmetry_valid"] = 0
		result.Details["inverse_edges"] = map[string]any{
			"container_found": false,
			"message":         "No container entities found (hierarchy inference may not have run)",
		}
		return nil
	}

	// Get container's OUTGOING relationships (should include 'contains' edges after Phase 6 change)
	outgoingEntries, _ := s.natsClient.GetOutgoingEntries(ctx, containerID)

	// Filter for 'contains' predicates
	containsCount := 0
	for _, entry := range outgoingEntries {
		if entry.Predicate == "hierarchy.type.contains" ||
			entry.Predicate == "hierarchy.system.contains" ||
			entry.Predicate == "hierarchy.domain.contains" {
			containsCount++
		}
	}

	// Cross-reference with incoming 'member' edges
	incomingEntries, _ := s.natsClient.GetIncomingEntries(ctx, containerID)
	memberCount := 0
	for _, entry := range incomingEntries {
		if entry.Predicate == "hierarchy.type.member" ||
			entry.Predicate == "hierarchy.system.member" ||
			entry.Predicate == "hierarchy.domain.member" {
			memberCount++
		}
	}

	// Verify symmetry: member edges should have corresponding contains edges
	symmetryValid := containsCount > 0 && containsCount == memberCount

	// Record metrics
	result.Metrics["inverse_member_edges"] = memberCount
	result.Metrics["inverse_contains_edges"] = containsCount
	result.Metrics["inverse_symmetry_valid"] = boolToInt(symmetryValid)

	// Log results
	fmt.Printf("[INVERSE EDGES] Container: %s\n", containerID)
	fmt.Printf("[INVERSE EDGES] Incoming 'member' edges: %d\n", memberCount)
	fmt.Printf("[INVERSE EDGES] Outgoing 'contains' edges: %d\n", containsCount)

	if symmetryValid {
		if len(outgoingEntries) > 0 {
			for _, entry := range outgoingEntries {
				if strings.Contains(entry.Predicate, ".contains") {
					fmt.Printf("[INVERSE EDGES] Sample: %s → %s → %s\n",
						containerID, entry.Predicate, entry.ToEntityID)
					break
				}
			}
		}
		fmt.Println("[INVERSE EDGES] Success: Containers explicitly know their members via 'contains' edges")
	} else if containsCount == 0 {
		if s.config.Variant == "structural" || s.config.Variant == "statistical" {
			// Short-running tiers may not have completed async index updates
			// Hierarchy inference creates inverse edges but outgoing index update is async
			fmt.Println("[INVERSE EDGES] Note: Contains edges not indexed yet (async update pending)")
		} else {
			result.Warnings = append(result.Warnings,
				"No 'contains' edges found - inverse materialization may not be working")
		}
	} else if containsCount != memberCount {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Edge count mismatch: %d member edges vs %d contains edges", memberCount, containsCount))
	}

	result.Details["inverse_edges"] = map[string]any{
		"container_id":    containerID,
		"member_edges":    memberCount,
		"contains_edges":  containsCount,
		"symmetry_valid":  symmetryValid,
		"edges_match":     containsCount == memberCount,
		"inverse_working": containsCount > 0,
	}

	return nil
}

// boolToInt converts a boolean to int (1 for true, 0 for false) for metrics.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
