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
//
// The community fetch happens on EVERY iteration. It used to sit inside an
// `if clusteringRuns >= 1` branch keyed on semstreams_clustering_runs_total —
// a metric no production code exports — so the branch was unreachable, the
// success log below was dead, and every call burned the full 90s before making
// a single point-in-time fetch with no retry (gh#615).
//
// Nonempty COMMUNITY_INDEX is NOT on its own evidence that this run's clustering
// finished: COMMUNITY_INDEX is durable NATS state, so if the JetStream volume
// survives from an earlier run, the very first poll returns that run's
// communities and every downstream quality check then validates stale output.
// Every tier task tears down with `docker compose ... down -v` (Taskfile.yml:173,
// taskfiles/e2e/{structural,statistical}.yml:13, taskfiles/e2e/common.yml:29-31),
// so the named volume normally goes with the stack and the exposure is bounded to
// a run against an already-up stack or a task killed before its defer — but
// "usually torn down" is not a property the wait should depend on.
//
// So the wait also requires current-cycle evidence: the completed-detection-run
// count must exceed a baseline captured before polling started. The counter lives
// in the graph-clustering process, which starts at 0 on every container start, so
// on a clean run the baseline is 0 and the first completed cycle satisfies it at
// no extra cost. On a dirty-volume run, preexisting communities no longer count
// because they carry no run of THIS process.
func (s *TieredScenario) waitForCommunities(ctx context.Context) ([]*clustering.Community, error) {
	// initial_delay + detection_interval + processing.
	const maxWait = 90 * time.Second
	return s.waitForCommunitiesFrom(ctx, s.natsClient, maxWait)
}

// communitySource is the narrow read waitForCommunitiesFrom performs. Taking it
// as a parameter keeps the staleness logic testable without a live NATS; the
// only production implementation is *client.NATSValidationClient.
type communitySource interface {
	GetAllCommunities(ctx context.Context) ([]*clustering.Community, error)
}

func (s *TieredScenario) waitForCommunitiesFrom(
	ctx context.Context,
	source communitySource,
	maxWait time.Duration,
) ([]*clustering.Community, error) {
	const pollInterval = 500 * time.Millisecond

	startWait := time.Now()
	deadline := startWait.Add(maxWait)

	baselineRuns, err := s.clusteringRunCount(ctx)
	if err != nil {
		// Without the run counter there is no way to tell this run's communities
		// from a previous run's. Both tiers that call this deploy graph-clustering
		// and the histogram is registered in its constructor, so the counter is
		// scrapeable at 0 from startup; failing to read it is a real fault.
		return nil, fmt.Errorf("cannot establish a clustering-run baseline, so community freshness is unverifiable: %w", err)
	}

	var lastFetchErr error
	var sawCommunities int
	for {
		communities, fetchErr := source.GetAllCommunities(ctx)
		switch {
		case fetchErr != nil:
			lastFetchErr = fetchErr
		case len(communities) > 0:
			sawCommunities = len(communities)
			runs, runErr := s.clusteringRunCount(ctx)
			if runErr == nil && runs > baselineRuns {
				fmt.Printf("[COMMUNITY WAIT] Found %d communities after %.1fs (clustering runs=%.0f, baseline=%.0f)\n",
					len(communities), time.Since(startWait).Seconds(), runs, baselineRuns)
				return communities, nil
			}
		}

		if !time.Now().Before(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	waited := time.Since(startWait).Seconds()
	diagnostic := s.clusteringRunDiagnostic(ctx)

	if sawCommunities > 0 {
		// The one case that used to pass silently.
		fmt.Printf("[COMMUNITY WAIT] %d communities present but no detection run completed in %.1fs (%s)\n",
			sawCommunities, waited, diagnostic)
		return nil, fmt.Errorf(
			"found %d communities but no community-detection run completed during the %.1fs wait "+
				"(runs still at baseline %.0f, %s): these communities predate this run and validating them would validate stale output",
			sawCommunities, waited, baselineRuns, diagnostic)
	}

	fmt.Printf("[COMMUNITY WAIT] No communities found after %.1fs (%s)\n", waited, diagnostic)

	if lastFetchErr != nil {
		return nil, fmt.Errorf("no communities after %.1fs (%s); last fetch error: %w",
			waited, diagnostic, lastFetchErr)
	}
	return nil, fmt.Errorf("no communities after %.1fs (%s)", waited, diagnostic)
}

// clusteringRunCount returns the number of COMPLETED community-detection runs
// recorded by the graph-clustering process currently being scraped.
//
// Unlike clusteringRunDiagnostic this is load-bearing, so a missing subsystem is
// an error rather than a phrase: waitForCommunities uses the value to prove a
// detection cycle happened during the wait, and a silent 0 there would restore
// exactly the "accept whatever is in the bucket" behavior it exists to prevent.
func (s *TieredScenario) clusteringRunCount(ctx context.Context) (float64, error) {
	reading, err := s.metrics.SumMetricInSubsystem(ctx, clusteringSubsystem, clusteringRunsMetric)
	if err != nil {
		return 0, err
	}
	if !reading.SubsystemPresent {
		return 0, fmt.Errorf(
			"no %s series scraped: graph-clustering is not deployed or not reachable, "+
				"so a completed detection run cannot be confirmed", clusteringSubsystem)
	}
	return reading.Sum, nil
}

// clusteringRunDiagnostic renders the completed-detection-run count as a short
// human-readable phrase for wait logs and timeout errors.
//
// It is deliberately non-fatal: this is context on why a wait ended, not a gate.
// It still reports a broken metric name explicitly instead of printing a
// plausible 0, so the failure mode that produced gh#615 stays visible here too.
func (s *TieredScenario) clusteringRunDiagnostic(ctx context.Context) string {
	reading, err := s.metrics.SumMetricInSubsystem(ctx, clusteringSubsystem, clusteringRunsMetric)
	switch {
	case err != nil:
		return fmt.Sprintf("clustering runs unknown: %v", err)
	case !reading.SubsystemPresent:
		return "graph-clustering not deployed or not scraped"
	default:
		return fmt.Sprintf("clustering runs=%.0f", reading.Sum)
	}
}

// waitForLLMEnhancement waits for LLM enhancement to complete for ML variant.
//
// After the B3 ownership split (ADR-087) enhancement status lives in the
// worker-owned COMMUNITY_SUMMARIES store, joined to each community by membership
// hash — NOT on COMMUNITY_INDEX.SummaryStatus, which the worker no longer writes.
// The pre-split wait polled that dead field, never observed pending→0, and always
// burned its full ceiling before reporting enhanced=0; this one terminates as soon
// as the summary store is caught up.
func (s *TieredScenario) waitForLLMEnhancement(
	ctx context.Context,
	communities []*clustering.Community,
	result *Result,
) llmWaitResult {
	fmt.Printf("[LLM WAIT] Waiting for LLM enhancement to complete (ML variant, %d communities)...\n", len(communities))

	enhanceStart := time.Now()
	// Default 2m is sized for the fast qwen3-1.7b summary tier. The heavy
	// qwen3-8b run generates summaries far slower; SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT
	// raises the ceiling so B0 (which runs next) synthesizes over fully-populated
	// Tier-2 summaries. WaitForCommunitySummaryEnhancement returns as soon as
	// enhancement completes, so a larger ceiling never over-waits on the fast tier.
	enhanced, failed, pending, waitErr := s.natsClient.WaitForCommunitySummaryEnhancement(
		ctx, communities, llmEnhancementWait(2*time.Minute), 2*time.Second,
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

// joinedSummaryRecord returns the worker-written summary record for a community,
// joined from COMMUNITY_SUMMARIES by membership hash (ADR-087), or nil when none
// exists. It is the ONE place the e2e observability reconstructs the read-path key,
// so it uses the shared clustering.SummaryKey/MembershipHash helpers — the same
// pair graph-query's SummaryFor uses — and cannot drift into a key that never
// joins.
func joinedSummaryRecord(
	comm *clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
) *clustering.CommunitySummaryRecord {
	if comm == nil || len(comm.Members) == 0 {
		return nil
	}
	return summaries[clustering.SummaryKey(comm.Level, clustering.MembershipHash(comm.Members))]
}

// joinedEnhancedSummary returns a community's LLM summary text with ok=true only
// when a usable llm-enhanced record exists for its exact membership — mirroring
// graph-query's SummaryFor so the e2e view matches what GraphRAG reads.
func joinedEnhancedSummary(
	comm *clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
) (string, bool) {
	rec := joinedSummaryRecord(comm, summaries)
	if rec == nil || rec.Status != clustering.SummaryStatusEnhanced || rec.LLMSummary == "" {
		return "", false
	}
	return rec.LLMSummary, true
}

// analyzeCommunities computes statistics and comparisons for communities, joining
// enhancement status and LLM text from the COMMUNITY_SUMMARIES store rather than
// from the (post-split always-empty) COMMUNITY_INDEX fields.
func (s *TieredScenario) analyzeCommunities(
	communities []*clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
) communityStats {
	stats := communityStats{comparisons: make([]CommunityComparison, 0, len(communities))}
	var totalLengthRatio, totalWordOverlap float64
	var ratioCount, totalNonSingletonMembers int

	for _, comm := range communities {
		comparison := s.buildCommunityComparison(comm, summaries, &totalLengthRatio, &totalWordOverlap, &ratioCount)

		if len(comm.Members) > 1 {
			stats.nonSingletonCount++
			totalNonSingletonMembers += len(comm.Members)
			if len(comm.Members) > stats.largestCommunitySize {
				stats.largestCommunitySize = len(comm.Members)
			}
		}

		// Enhanced = the summary store holds a usable llm-enhanced record for this
		// membership; otherwise the community serves the statistical floor (a missing
		// or failed record). This counts what GraphRAG actually reads, not a field the
		// worker stopped writing.
		if _, ok := joinedEnhancedSummary(comm, summaries); ok {
			stats.llmEnhancedCount++
		} else {
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

// buildCommunityComparison creates a comparison record for a single community,
// sourcing the LLM summary and its status from the joined COMMUNITY_SUMMARIES
// record (ADR-087). SummaryStatus reflects the store: "llm-enhanced"/"llm-failed"
// from the record, or "pending" when no record has landed yet.
func (s *TieredScenario) buildCommunityComparison(
	comm *clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
	totalLengthRatio, totalWordOverlap *float64,
	ratioCount *int,
) CommunityComparison {
	rec := joinedSummaryRecord(comm, summaries)

	status := "pending"
	llmSummary := ""
	if rec != nil {
		status = rec.Status
		if rec.Status == clustering.SummaryStatusEnhanced {
			llmSummary = rec.LLMSummary
		}
	}

	comparison := CommunityComparison{
		CommunityID:        comm.ID,
		Level:              comm.Level,
		MemberCount:        len(comm.Members),
		StatisticalSummary: comm.StatisticalSummary,
		LLMSummary:         llmSummary,
		SummaryStatus:      status,
		Keywords:           comm.Keywords,
	}

	if llmSummary != "" && comm.StatisticalSummary != "" {
		comparison.SummaryLengthRatio = float64(len(llmSummary)) / float64(len(comm.StatisticalSummary))
		*totalLengthRatio += comparison.SummaryLengthRatio
		*ratioCount++
		comparison.WordOverlap = wordJaccard(comm.StatisticalSummary, llmSummary)
		*totalWordOverlap += comparison.WordOverlap
	}

	return comparison
}

// llmQualityIssue represents a quality issue found in LLM summaries
type llmQualityIssue struct {
	CommunityID string
	Issue       string
}

// validateLLMSummaryQuality validates quality of LLM-enhanced community summaries.
// Enhancement text is joined from the COMMUNITY_SUMMARIES store by membership hash
// (ADR-087); communities without a usable enhanced record are skipped (they serve
// the statistical floor, which is validated elsewhere). Keywords remain
// detector-owned on COMMUNITY_INDEX and are read from the community record.
func (s *TieredScenario) validateLLMSummaryQuality(
	communities []*clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
) []llmQualityIssue {
	var issues []llmQualityIssue

	for _, comm := range communities {
		llmSummary, ok := joinedEnhancedSummary(comm, summaries)
		if !ok {
			continue
		}

		// Check minimum summary length (50 chars)
		if len(llmSummary) < 50 {
			issues = append(issues, llmQualityIssue{
				CommunityID: comm.ID,
				Issue:       fmt.Sprintf("LLM summary too short: %d chars (min 50)", len(llmSummary)),
			})
			continue
		}

		// Check that at least one keyword appears in the summary
		keywordFound := false
		summaryLower := strings.ToLower(llmSummary)
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
		if comm.StatisticalSummary != "" && len(llmSummary) <= len(comm.StatisticalSummary) {
			issues = append(issues, llmQualityIssue{
				CommunityID: comm.ID,
				Issue: fmt.Sprintf("LLM summary (%d chars) not longer than statistical (%d chars)",
					len(llmSummary), len(comm.StatisticalSummary)),
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

	// Wait for LLM enhancement to complete (joins the COMMUNITY_SUMMARIES store by
	// membership hash — the post-split source of truth for enhancement status).
	llmWait := s.waitForLLMEnhancement(ctx, communities, result)

	// Re-fetch the partition after waiting (the detector may have re-run).
	communities, err = s.natsClient.GetAllCommunities(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to re-fetch communities after LLM wait: %v", err))
		return nil
	}

	// Read the worker-owned summary store once and JOIN it to the communities: after
	// the B3 split (ADR-087) enhancement status/text live here, not on COMMUNITY_INDEX.
	summaries, err := s.natsClient.GetCommunitySummaries(ctx)
	if err != nil {
		// A summary-store read failure degrades the report to the statistical floor
		// rather than aborting the stage — the partition itself is still valid.
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to read community summaries: %v", err))
		summaries = map[string]*clustering.CommunitySummaryRecord{}
	}

	// Analyze communities for summary status (joined from the store)
	stats := s.analyzeCommunities(communities, summaries)

	// Record metrics
	s.recordCommunityMetrics(stats, result)

	// Persist detailed report
	variant := s.detectCommunityVariant(result)
	reportFile := s.persistCommunityReport(variant, stats, llmWait, result)
	if reportFile != "" {
		fmt.Printf("[LLM ENHANCEMENT] Wrote community report to %s\n", reportFile)
	}

	// Validate LLM summary quality for enhanced communities (joined from the store)
	issues := s.validateLLMSummaryQuality(communities, summaries)
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

	// Every name here is verified against processor/graph-embedding/metrics.go.
	// A sixth read, semstreams_graph_embedding_queued_total, was removed: no
	// production code has ever exported it, so it contributed a hard 0 that then
	// travelled into operator-facing result JSON as though it were data (gh#615).
	// Errors are propagated rather than discarded — this validator runs only in
	// tiers that deploy graph-embedding, so a missing name means the check is
	// broken, not that the component is idle.
	// One snapshot, four reads. resolved/dedup/fresh are arithmetically related,
	// so scraping them independently would let the pipeline advance between calls
	// and yield a negative "fresh" that is an artifact rather than a finding.
	snapshot, err := s.metrics.FetchSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("embedding queue health is unverifiable: %w", err)
	}

	readings := map[string]float64{}
	for _, name := range []string{
		embeddingSubsystem + "pending",
		embeddingSubsystem + "errors_total",
		embeddingSubsystem + "dedup_hits_total",
		embeddingsGeneratedMetric,
	} {
		reading, err := client.SumMetricInSnapshot(snapshot, embeddingSubsystem, name)
		if err != nil {
			return fmt.Errorf("embedding queue health is unverifiable: %w", err)
		}
		if !reading.SubsystemPresent {
			return fmt.Errorf(
				"embedding queue health is unverifiable: no %s series scraped, so graph-embedding is not deployed in a tier that requires it",
				embeddingSubsystem)
		}
		readings[name] = reading.Sum
	}

	pending := readings[embeddingSubsystem+"pending"]
	failed := readings[embeddingSubsystem+"errors_total"]
	dedupHits := readings[embeddingSubsystem+"dedup_hits_total"]
	resolved := readings[embeddingsGeneratedMetric]

	// METRIC SEMANTICS — read this before touching the arithmetic below.
	//
	// semstreams_graph_embedding_embeddings_generated_total does NOT count fresh
	// generations. graph/embedding/worker.go:394 calls saveAndNotify() on the
	// return of getOrGenerateEmbedding() for BOTH branches — the dedup-hit branch
	// (worker.go:427, which also increments dedup_hits_total) and the
	// embedder.Generate() branch. saveAndNotify fires onGenerated, which
	// processor/graph-embedding/component.go:876 wires to
	// recordEmbeddingGenerated(). So the counter increments once per embedding
	// RESOLVED, cache hit or not, and dedup_hits_total is a strict subset of it.
	//
	// Therefore:
	//   resolved = embeddings_generated_total        (NOT generated + dedupHits;
	//                                                 that double-counts reuse)
	//   fresh    = resolved - dedupHits              (the vectors actually
	//                                                 computed — on the neural
	//                                                 tier, the remote calls)
	//
	// The earlier `resolved := generated + dedupHits` made cache reuse look like
	// extra throughput and hid a 2.81x change in real embedding work behind a
	// flat-looking total. Nothing else reports fresh generations, so it is
	// surfaced as its own field rather than left to be re-derived downstream.
	fresh := resolved - dedupHits
	dedupExceedsResolved := fresh < 0
	if dedupExceedsResolved {
		// Impossible within one snapshot given the subset relation above, so this
		// means the invariant this comment documents has been broken in production
		// code — not a benign race. Report it; do not silently clamp away the only
		// evidence.
		fresh = 0
	}

	result.Metrics["embedding_resolved_total"] = int64(resolved)
	result.Metrics["embedding_fresh_generated_total"] = int64(fresh)
	result.Metrics["embedding_dedup_hits"] = int64(dedupHits)
	result.Metrics["embedding_failed_total"] = int64(failed)
	result.Metrics["embedding_pending_count"] = int64(pending)

	fmt.Printf("[EMBEDDING QUEUE] Stats: resolved=%.0f (fresh=%.0f, dedup_hits=%.0f), failed=%.0f, pending=%.0f\n",
		resolved, fresh, dedupHits, failed, pending)

	dedupRate := 0.0
	if resolved > 0 {
		dedupRate = dedupHits / resolved * 100
		fmt.Printf("[EMBEDDING QUEUE] Dedup efficiency: %.1f%% (%.0f hits / %.0f resolved)\n",
			dedupRate, dedupHits, resolved)
	}

	result.Details["embedding_queue_health"] = map[string]any{
		"resolved_total":        resolved,
		"fresh_generated_total": fresh,
		"dedup_hits":            dedupHits,
		"failed_total":          failed,
		"pending_count":         pending,
		"queue_drained":         pending == 0,
		"no_failures":           failed == 0,
	}

	// The queue draining to empty is not on its own a healthy pipeline — a
	// pipeline that never accepted anything also reports drained and zero
	// failures. This validator used to print "Health check passed" for exactly
	// that state. Assert that work actually happened.
	if resolved == 0 {
		return fmt.Errorf(
			"embedding pipeline did nothing: 0 embeddings resolved (pending=%.0f, failed=%.0f)",
			pending, failed)
	}

	// A non-drained queue and a nonzero failure count are the two conditions this
	// stage exists to detect. They were recorded as result.Warnings and then
	// returned nil, so validate-embedding-queue-health was marked completed with
	// undrained work or failed embeddings — a stage that could not fail for the
	// reason it was written. Warnings stay for the operator report; the error is
	// what makes the gate a gate.
	var violations []string
	if pending > 0 {
		msg := fmt.Sprintf("embedding queue not drained: %.0f pending", pending)
		violations = append(violations, msg)
		result.Warnings = append(result.Warnings, msg)
	}
	if failed > 0 {
		msg := fmt.Sprintf("embedding failures detected: %.0f failed", failed)
		violations = append(violations, msg)
		result.Warnings = append(result.Warnings, msg)
	}
	if dedupExceedsResolved {
		msg := fmt.Sprintf(
			"dedup_hits_total (%.0f) exceeds embeddings_generated_total (%.0f) in one scrape: "+
				"dedup hits are supposed to be a subset of resolutions, so the worker's metric wiring changed",
			dedupHits, resolved)
		violations = append(violations, msg)
		result.Warnings = append(result.Warnings, msg)
	}
	if len(violations) > 0 {
		return fmt.Errorf("embedding queue is unhealthy: %s", strings.Join(violations, "; "))
	}

	fmt.Printf("[EMBEDDING QUEUE] Health check passed: %.0f embeddings resolved (%.0f fresh), queue drained, no failures\n",
		resolved, fresh)

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

// effectiveVariant resolves the tier used for strict/soft gating decisions. It
// prefers the explicit config, falling back to the auto-detected variant that
// Execute stamps into result.Metrics["variant"] — s.config.Variant stays "" when
// the caller omits --variant (Execute keeps the detected value local), so reading
// it raw would send an intentionally-empty structural index down the strict path.
func (s *TieredScenario) effectiveVariant(result *Result) string {
	if s.config.Variant != "" {
		return s.config.Variant
	}
	if v, ok := result.Metrics["variant"].(string); ok {
		return v
	}
	return ""
}

// validateContextIndexHierarchy validates that the ContextIndex is tracking inference provenance.
// Phase 5: Verifies that hierarchy inference triples are tracked in CONTEXT_INDEX.
func (s *TieredScenario) validateContextIndexHierarchy(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client unavailable for context index validation")
		return nil
	}

	fmt.Println("[CONTEXT INDEX] Validating context index hierarchy tracking...")

	// Count raw CONTEXT_INDEX keys. After composite-key sharding (gh#474) a key is
	// "hash(context).entityID.predicate", so this counts memberships, not distinct
	// contexts — but non-zero still means the write path populated the bucket.
	count, err := s.natsClient.CountBucketKeys(ctx, client.IndexBuckets.Context)
	if err != nil {
		return fmt.Errorf("context index query failed: %w", err)
	}

	// Read the DISTINCT context values from entry values (the sharded key no longer
	// carries the raw context) and the hierarchy memberships by value-match. This is
	// the sharded reader under test — see client.GetAllContexts / GetContextEntries.
	allContexts, err := s.natsClient.GetAllContexts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list context values: %w", err)
	}
	hierarchyEntries, err := s.natsClient.GetContextEntries(ctx, "inference.hierarchy")
	if err != nil {
		return fmt.Errorf("failed to read inference.hierarchy context entries: %w", err)
	}
	hierarchyContextFound := len(hierarchyEntries) > 0
	hierarchyEntryCount := len(hierarchyEntries)

	// Record metrics
	result.Metrics["context_index_keys"] = count
	result.Metrics["context_hierarchy_found"] = boolToInt(hierarchyContextFound)
	result.Metrics["context_hierarchy_entries"] = hierarchyEntryCount

	// Log results
	fmt.Printf("[CONTEXT INDEX] Results: total_keys=%d, distinct_contexts=%d, hierarchy_found=%v, hierarchy_entries=%d\n",
		count, len(allContexts), hierarchyContextFound, hierarchyEntryCount)
	if len(allContexts) > 0 {
		fmt.Printf("[CONTEXT INDEX] Distinct contexts: %v\n", allContexts)
	}

	// Drift guard (all tiers): keys exist but the value-reader recovers zero distinct
	// contexts ⇒ the sharded on-disk format and this reader disagree. HARD-FAIL — a
	// warn here is exactly the "tier passes validating nothing" trap this change closes.
	if count > 0 && len(allContexts) == 0 {
		return fmt.Errorf("CONTEXT_INDEX has %d keys but no context values are readable — sharded key/value format drift (gh#474)", count)
	}

	// Populated-expectation guard (non-structural tiers): hierarchy inference runs to
	// completion here, and every hierarchy triple carries Context:"inference.hierarchy",
	// so both an empty index and a populated-but-unreadable hierarchy are real failures.
	variant := s.effectiveVariant(result)
	if variant == "structural" {
		if count == 0 {
			// Structural tier runs quickly — async context indexing may not complete in
			// time; hierarchy-inference validation already confirms containers exist.
			fmt.Println("[CONTEXT INDEX] Note: Context index empty (expected in short structural tier run)")
		}
	} else {
		if count == 0 {
			return fmt.Errorf("CONTEXT_INDEX is empty on the %s tier — hierarchy inference did not populate the index", variant)
		}
		if !hierarchyContextFound {
			return fmt.Errorf("CONTEXT_INDEX has %d keys and %d distinct contexts but 'inference.hierarchy' has no readable entries — sharded reader drift (gh#474)", count, len(allContexts))
		}
		fmt.Printf("[CONTEXT INDEX] Success: hierarchy inference provenance tracked (%d entries)\n", hierarchyEntryCount)
	}

	result.Details["context_index_validation"] = map[string]any{
		"total_keys":             count,
		"distinct_contexts":      allContexts,
		"hierarchy_found":        hierarchyContextFound,
		"hierarchy_entry_count":  hierarchyEntryCount,
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

	// Get incoming entries for the container. A reader error is unambiguous — fail.
	entries, err := s.natsClient.GetIncomingEntries(ctx, containerID)
	if err != nil {
		return fmt.Errorf("incoming entries query failed for %s: %w", containerID, err)
	}

	// Drift guard: a .group container exists only because hierarchy inference pointed
	// member edges INTO it, so it must have incoming edges. Zero reconstructed entries
	// ⇒ the sharded reader cannot read the on-disk INCOMING_INDEX format (gh#474).
	// HARD-FAIL on non-structural tiers; structural's short run may not have indexed
	// yet — a warn here is the "tier passes validating nothing" trap this change closes.
	// Ordering note: a container's incoming keys are written when its MEMBER entities are
	// processed (async vs. container creation). On non-structural tiers this is settled by
	// the preceding wait-for-entity-stabilization + validate-hierarchy-inference stages and
	// the ADR-066 caught-up watermark, so a non-empty result is expected by the time we run.
	if len(entries) == 0 {
		if s.effectiveVariant(result) == "structural" {
			fmt.Println("[INCOMING INDEX] Note: no incoming edges yet (expected in short structural tier run)")
		} else {
			return fmt.Errorf("container %s exists but INCOMING_INDEX returned 0 incoming edges — sharded reader/format drift (gh#474)", containerID)
		}
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
