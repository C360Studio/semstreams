// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
)

// Search validation functions for tiered E2E tests

// executeVerifySearchQuality validates that semantic search returns expected results
// with score threshold assertions, not just binary hit/no-hit checks
func (s *TieredScenario) executeVerifySearchQuality(ctx context.Context, result *Result) error {
	// Use similarity search for both statistical and semantic tiers (embedding-based with real scores)
	// Statistical tier uses BM25 embeddings, semantic tier uses neural embeddings
	// Structural tier has no embeddings, so uses global search (community-based)
	// The variant is read through effectiveVariant: under auto-detect s.config.Variant
	// stays "" and the raw read would send statistical down the recorder arm.
	variant := s.effectiveVariant(result)
	var executor *search.Executor
	if variant == "statistical" || variant == "semantic" {
		executor = search.NewSimilarityExecutor(s.config.GraphQLURL, 10*time.Second)
	} else {
		executor = search.NewExecutor(s.config.GraphQLURL, 10*time.Second)
	}
	queries := search.DefaultQueries()

	// Execute all queries and get stats
	stats := executor.ExecuteAll(ctx, queries)

	// Store stats for potential reuse by comparison stage
	s.searchStats = stats

	// Record results in legacy format for backward compatibility
	s.recordSearchQualityResultsFromStats(result, stats)

	return s.searchQualityVerdict(variant, stats)
}

// searchQualityVerdict is the stage's gate (#1426). Path arm, every variant the
// stage runs in: a query that errors or returns no hits is the search path
// failing, which the framework owns. Known-answer arm, by variant: under
// statistical the ranker is BM25 (pure Go, deterministic over the fixed corpus),
// so a missing known answer is a framework outcome and fails; under semantic the
// ranking is the embedding model's, so the arm is a RECORDER (the warnings
// recordSearchQualityResultsFromStats already wrote). The average-score warning
// is a RECORDER in both: 0.5 is not calibrated to either scorer (BM25 measured
// 0.28 on every run).
func (s *TieredScenario) searchQualityVerdict(variant string, stats *search.Stats) error {
	var broken []string
	for _, r := range stats.Results {
		switch {
		case r.Error != "":
			broken = append(broken, fmt.Sprintf("%q: %s", r.Query, r.Error))
		case len(r.Hits) == 0:
			broken = append(broken, fmt.Sprintf("%q: returned no hits", r.Query))
		}
	}
	if len(broken) > 0 {
		return fmt.Errorf("search failed for %d/%d queries: %s", len(broken), stats.TotalQueries, strings.Join(broken, "; "))
	}
	if variant == "statistical" && len(stats.KnownAnswerFailures) > 0 {
		return fmt.Errorf("known-answer search failed under BM25 (%d/%d passed): %s",
			stats.KnownAnswerTestsPassed, stats.KnownAnswerTestsTotal, strings.Join(stats.KnownAnswerFailures, "; "))
	}
	return nil
}

// searchQualityTest defines a search quality test case.
type searchQualityTest struct {
	query           string
	expectedPattern string
	description     string
	minScore        float64
	minHits         int
	// Known-answer validation (Phase 3 improvement)
	mustInclude []string // Entity ID substrings that MUST appear in results
	mustExclude []string // Entity ID substrings that should NOT appear (warning only)
}

// searchQualityStats tracks aggregate statistics for search quality tests.
type searchQualityStats struct {
	searchResults          map[string]any
	allScores              []float64
	queriesWithResults     int
	queriesMeetingMinScore int
	queriesMeetingMinHits  int
	// Known-answer validation stats (Phase 3 improvement)
	knownAnswerTestsPassed int
	knownAnswerTestsTotal  int
	knownAnswerFailures    []string // Descriptions of failed known-answer tests
}

// getSearchQualityTests returns the search quality test cases.
// Phase 3 improvement: Added known-answer validation based on testdata/semantic/ content.
func (s *TieredScenario) getSearchQualityTests() []searchQualityTest {
	return []searchQualityTest{
		// Original natural language tests
		{
			query:           "What documents mention forklift safety?",
			expectedPattern: "forklift",
			description:     "Natural language document search",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"doc-ops-001"}, // Forklift Operation Manual
			mustExclude:     []string{"sensor-temp"}, // Temperature sensors irrelevant
		},
		{
			query:           "Are there safety observations related to temperature?",
			expectedPattern: "temperature",
			description:     "Cross-domain safety query",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"sensor-temp"}, // Temperature sensors
		},
		{
			query:           "What maintenance was done on cold storage equipment?",
			expectedPattern: "cold",
			description:     "Maintenance semantic search",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"maint-"}, // Maintenance records
		},
		{
			query:           "Find all sensors in zone-a",
			expectedPattern: "zone-a",
			description:     "Location-based sensor query",
			minScore:        0.3,
			minHits:         1,
		},
		// Known-answer tests derived from testdata/semantic/ (Phase 3)
		{
			query:           "forklift operation inspection equipment maintenance",
			expectedPattern: "ops",
			description:     "Operations query should return operations docs",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"doc-ops"},                       // doc-ops-001 (Forklift Operation Manual)
			mustExclude:     []string{"sensor-humid", "sensor-motion"}, // Humidity/motion sensors irrelevant
		},
		{
			query:           "cold storage temperature monitoring refrigeration",
			expectedPattern: "temp",
			description:     "Temperature query should return temp sensors",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"sensor-temp"},         // sensor-temp-001, sensor-temp-002, etc.
			mustExclude:     []string{"doc-hr", "doc-audit"}, // HR and audit docs irrelevant
		},
		{
			query:           "hydraulic fluid maintenance equipment repair",
			expectedPattern: "maint",
			description:     "Maintenance query should return maintenance records",
			minScore:        0.3,
			minHits:         1,
			mustInclude:     []string{"maint-"}, // maint-001 (hydraulic maintenance)
		},
	}
}

// executeSearchQualityTest executes a single search quality test.
func (s *TieredScenario) executeSearchQualityTest(
	ctx context.Context,
	httpClient *http.Client,
	test searchQualityTest,
	stats *searchQualityStats,
) {
	searchQuery := map[string]any{
		"query":     test.query,
		"threshold": 0.1,
		"limit":     10,
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		s.config.GatewayURL+"/search/semantic", strings.NewReader(string(queryJSON)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		stats.searchResults[test.query] = map[string]any{
			"error":       err.Error(),
			"description": test.description,
		}
		return
	}

	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		stats.searchResults[test.query] = map[string]any{
			"status":      resp.StatusCode,
			"description": test.description,
		}
		return
	}

	var searchResp struct {
		Data struct {
			Query string `json:"query"`
			Hits  []struct {
				EntityID string  `json:"entity_id"`
				Score    float64 `json:"score"`
			} `json:"hits"`
		} `json:"data"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &searchResp); err != nil {
		stats.searchResults[test.query] = map[string]any{
			"error":       "parse error",
			"description": test.description,
		}
		return
	}

	s.processSearchQualityHits(test, searchResp.Data.Hits, stats)
}

// processSearchQualityHits processes hits from a search quality test.
func (s *TieredScenario) processSearchQualityHits(
	test searchQualityTest,
	hits []struct {
		EntityID string  `json:"entity_id"`
		Score    float64 `json:"score"`
	},
	stats *searchQualityStats,
) {
	if len(hits) > 0 {
		stats.queriesWithResults++
	}
	if len(hits) >= test.minHits {
		stats.queriesMeetingMinHits++
	}

	matchesPattern := false
	topHits := []string{}
	topScores := []float64{}
	hitsAboveMinScore := 0
	var scoreSum float64

	for _, hit := range hits {
		topHits = append(topHits, hit.EntityID)
		topScores = append(topScores, hit.Score)
		stats.allScores = append(stats.allScores, hit.Score)
		scoreSum += hit.Score

		if hit.Score >= test.minScore {
			hitsAboveMinScore++
		}
		if strings.Contains(strings.ToLower(hit.EntityID), test.expectedPattern) {
			matchesPattern = true
		}
	}

	avgScore := 0.0
	if len(hits) > 0 {
		avgScore = scoreSum / float64(len(hits))
	}

	meetsMinScore := hitsAboveMinScore > 0
	if meetsMinScore {
		stats.queriesMeetingMinScore++
	}

	// Phase 3 improvement: Known-answer validation
	knownAnswerPassed := true
	missingRequired := []string{}
	unexpectedFound := []string{}

	// Check mustInclude - required entities that MUST appear in results
	for _, required := range test.mustInclude {
		found := false
		for _, hit := range topHits {
			if strings.Contains(strings.ToLower(hit), strings.ToLower(required)) {
				found = true
				break
			}
		}
		if !found {
			knownAnswerPassed = false
			missingRequired = append(missingRequired, required)
		}
	}

	// Check mustExclude - entities that should NOT appear (warning only, not failure)
	for _, forbidden := range test.mustExclude {
		for _, hit := range topHits {
			if strings.Contains(strings.ToLower(hit), strings.ToLower(forbidden)) {
				unexpectedFound = append(unexpectedFound, hit)
				break
			}
		}
	}

	// Track known-answer test results
	if len(test.mustInclude) > 0 {
		stats.knownAnswerTestsTotal++
		if knownAnswerPassed {
			stats.knownAnswerTestsPassed++
		} else {
			stats.knownAnswerFailures = append(stats.knownAnswerFailures,
				fmt.Sprintf("query %q: missing required %v - %s", test.query, missingRequired, test.description))
		}
	}

	stats.searchResults[test.query] = map[string]any{
		"hit_count":           len(hits),
		"top_hits":            topHits,
		"top_scores":          topScores,
		"matches_pattern":     matchesPattern,
		"expected":            test.expectedPattern,
		"description":         test.description,
		"avg_score":           avgScore,
		"min_score_threshold": test.minScore,
		"min_hits_threshold":  test.minHits,
		"hits_above_min":      hitsAboveMinScore,
		"meets_min_score":     meetsMinScore,
		"meets_min_hits":      len(hits) >= test.minHits,
		// Known-answer validation results
		"known_answer_passed": knownAnswerPassed,
		"missing_required":    missingRequired,
		"unexpected_found":    unexpectedFound,
		"must_include":        test.mustInclude,
		"must_exclude":        test.mustExclude,
	}
}

// recordSearchQualityResults records search quality test results.
func (s *TieredScenario) recordSearchQualityResults(
	result *Result,
	searchTests []searchQualityTest,
	stats *searchQualityStats,
) {
	overallAvgScore := 0.0
	if len(stats.allScores) > 0 {
		var sum float64
		for _, score := range stats.allScores {
			sum += score
		}
		overallAvgScore = sum / float64(len(stats.allScores))
	}

	weakResultsThreshold := 0.5
	if overallAvgScore > 0 && overallAvgScore < weakResultsThreshold {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Weak search results: average score %.2f is below %.2f threshold",
				overallAvgScore, weakResultsThreshold))
	}

	// Phase 3 improvement: Report known-answer test failures as warnings
	for _, failure := range stats.knownAnswerFailures {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Known-answer test failed: %s", failure))
	}

	result.Metrics["search_queries_tested"] = len(searchTests)
	result.Metrics["search_queries_with_results"] = stats.queriesWithResults
	result.Metrics["search_min_score_met"] = stats.queriesMeetingMinScore
	result.Metrics["search_min_hits_met"] = stats.queriesMeetingMinHits
	result.Metrics["search_quality_score"] = overallAvgScore
	// Phase 3 improvement: Known-answer test metrics
	result.Metrics["known_answer_tests_passed"] = stats.knownAnswerTestsPassed
	result.Metrics["known_answer_tests_total"] = stats.knownAnswerTestsTotal

	result.Details["search_quality_verification"] = map[string]any{
		"queries":           len(searchTests),
		"queries_with_hits": stats.queriesWithResults,
		"min_score_met":     stats.queriesMeetingMinScore,
		"min_hits_met":      stats.queriesMeetingMinHits,
		"overall_avg_score": overallAvgScore,
		"weak_threshold":    weakResultsThreshold,
		"results":           stats.searchResults,
		"message":           fmt.Sprintf("%d/%d queries returned results, avg score: %.2f", stats.queriesWithResults, len(searchTests), overallAvgScore),
		// Phase 3 improvement: Known-answer validation summary
		"known_answer_tests_passed": stats.knownAnswerTestsPassed,
		"known_answer_tests_total":  stats.knownAnswerTestsTotal,
		"known_answer_failures":     stats.knownAnswerFailures,
	}
}

// recordSearchQualityResultsFromStats records search quality results from the new search.Stats format
// This provides backward compatibility with the legacy result format
func (s *TieredScenario) recordSearchQualityResultsFromStats(result *Result, stats *search.Stats) {
	weakResultsThreshold := 0.5
	if stats.OverallAvgScore > 0 && stats.OverallAvgScore < weakResultsThreshold {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Weak search results: average score %.2f is below %.2f threshold",
				stats.OverallAvgScore, weakResultsThreshold))
	}

	// Report known-answer test failures as warnings
	for _, failure := range stats.KnownAnswerFailures {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Known-answer test failed: %s", failure))
	}

	result.Metrics["search_queries_tested"] = stats.TotalQueries
	result.Metrics["search_queries_with_results"] = stats.QueriesWithResults
	result.Metrics["search_min_score_met"] = stats.QueriesMeetingMinScore
	result.Metrics["search_min_hits_met"] = stats.QueriesMeetingMinHits
	result.Metrics["search_quality_score"] = stats.OverallAvgScore
	result.Metrics["known_answer_tests_passed"] = stats.KnownAnswerTestsPassed
	result.Metrics["known_answer_tests_total"] = stats.KnownAnswerTestsTotal

	// Build legacy results format for backward compatibility
	legacyResults := make(map[string]any)
	for _, r := range stats.Results {
		legacyResults[r.Query] = map[string]any{
			"hit_count":           len(r.Hits),
			"top_hits":            extractEntityIDs(r.Hits),
			"top_scores":          extractScores(r.Hits),
			"description":         r.Description,
			"avg_score":           r.Validation.AvgScore,
			"meets_min_score":     r.Validation.MeetsMinScore,
			"meets_min_hits":      r.Validation.MeetsMinHits,
			"matches_pattern":     r.Validation.MatchesPattern,
			"known_answer_passed": r.Validation.KnownAnswerPassed,
			"missing_required":    r.Validation.MissingRequired,
			"unexpected_found":    r.Validation.UnexpectedFound,
			"latency_ms":          r.LatencyMs,
			"error":               r.Error,
		}
	}

	result.Details["search_quality_verification"] = map[string]any{
		"queries":                   stats.TotalQueries,
		"queries_with_hits":         stats.QueriesWithResults,
		"min_score_met":             stats.QueriesMeetingMinScore,
		"min_hits_met":              stats.QueriesMeetingMinHits,
		"overall_avg_score":         stats.OverallAvgScore,
		"weak_threshold":            weakResultsThreshold,
		"results":                   legacyResults,
		"message":                   fmt.Sprintf("%d/%d queries returned results, avg score: %.2f", stats.QueriesWithResults, stats.TotalQueries, stats.OverallAvgScore),
		"known_answer_tests_passed": stats.KnownAnswerTestsPassed,
		"known_answer_tests_total":  stats.KnownAnswerTestsTotal,
		"known_answer_failures":     stats.KnownAnswerFailures,
		"total_latency_ms":          stats.TotalLatencyMs,
	}
}

// extractEntityIDs extracts entity IDs from search hits
func extractEntityIDs(hits []search.Hit) []string {
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.EntityID
	}
	return ids
}

// extractScores extracts scores from search hits
func extractScores(hits []search.Hit) []float64 {
	scores := make([]float64, len(hits))
	for i, h := range hits {
		scores[i] = h.Score
	}
	return scores
}

// validateFallbackBehavior checks that the system degrades gracefully without ML services.
// Returns error if fallback test and core features aren't working.
func (s *TieredScenario) validateFallbackBehavior(result *Result) error {
	// Only validate if this is a fallback test
	if s.config.Variant != "semantic-fallback" {
		return nil
	}

	// Verify ML services are NOT available (as expected for fallback)
	if avail, ok := result.Details["semembed_available"].(bool); ok && avail {
		return fmt.Errorf("fallback test: semembed should be unavailable but was available")
	}

	// Verify core features still work
	if result.Structured != nil {
		// Check entities were ingested
		if result.Structured.Entities.ActualCount == 0 {
			return fmt.Errorf("fallback: entity ingestion failed (0 entities)")
		}

		// Check hierarchy was created
		if result.Structured.Hierarchy != nil && result.Structured.Hierarchy.ContainerCount == 0 {
			return fmt.Errorf("fallback: hierarchy inference failed (0 containers)")
		}

		// Check PathRAG works (sensor test)
		if result.Structured.PathRAGSensor != nil && result.Structured.PathRAGSensor.EntitiesFound == 0 {
			return fmt.Errorf("fallback: PathRAG failed (0 entities found)")
		}
	}

	return nil
}

// validateSemanticRequirements checks that semantic tier features are actually working.
// Returns error if this is a semantic tier test but semantic features aren't functional.
func (s *TieredScenario) validateSemanticRequirements(result *Result) error {
	// Skip validation for fallback tests (they have their own validation)
	if s.config.Variant == "semantic-fallback" {
		return nil
	}

	// Check if this is a semantic tier test
	// Use CONFIGURED variant if set, otherwise use detected variant
	isSemanticTest := false
	if s.config.Variant == "semantic" {
		isSemanticTest = true
	} else if s.config.Variant == "" {
		variant := s.detectVariantAndProvider(result)
		isSemanticTest = variant.variant == "semantic"
	}

	// Only validate semantic tier
	if !isSemanticTest {
		return nil
	}

	// Check semembed availability
	if avail, ok := result.Details["semembed_available"].(bool); !ok || !avail {
		return fmt.Errorf("semantic tier requires semembed: semembed_available=%v", avail)
	}

	// Check search functionality (known answer tests must pass)
	if result.Structured != nil {
		if stats := result.Structured.Search.Stats; stats != nil {
			passed := stats.KnownAnswerTestsPassed
			total := stats.KnownAnswerTestsTotal
			if total > 0 && passed == 0 {
				return fmt.Errorf("semantic tier: 0/%d known answer tests passed (search not working)", total)
			}
		}

		// Check embeddings were generated via variant info
		if !result.Structured.Variant.SemembedAvailable {
			return fmt.Errorf("semantic tier: semembed not available in structured results")
		}
	}

	return nil
}
