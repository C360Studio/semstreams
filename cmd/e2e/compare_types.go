// Package main provides structured result comparison for tier runs
package main

import "time"

// ComparisonReport represents comparison of two structured tier runs
type ComparisonReport struct {
	BaselineVariant string         `json:"baseline_variant"`
	TargetVariant   string         `json:"target_variant"`
	BaselineFile    string         `json:"baseline_file"`
	TargetFile      string         `json:"target_file"`
	GeneratedAt     time.Time      `json:"generated_at"`
	Sections        ComparisonDiff `json:"sections"`
	Summary         CompareSummary `json:"summary"`
}

// ComparisonDiff contains diffs by section
type ComparisonDiff struct {
	Variant     VariantDiff    `json:"variant"`
	Entities    EntityDiff     `json:"entities"`
	Indexes     IndexDiff      `json:"indexes"`
	Search      SearchDiff     `json:"search"`
	Rules       RuleDiff       `json:"rules"`
	Communities *CommunityDiff `json:"communities,omitempty"`
	Anomalies   *AnomalyDiff   `json:"anomalies,omitempty"`
	// Tier capability sections
	PathRAG  *PathRAGDiff  `json:"pathrag,omitempty"`
	GraphRAG *GraphRAGDiff `json:"graphrag,omitempty"`
}

// VariantDiff compares variant information
type VariantDiff struct {
	BaselineName     string `json:"baseline_name"`
	TargetName       string `json:"target_name"`
	BaselineProvider string `json:"baseline_provider"`
	TargetProvider   string `json:"target_provider"`
}

// EntityDiff compares entity results
type EntityDiff struct {
	BaselineCount int     `json:"baseline_count"`
	TargetCount   int     `json:"target_count"`
	CountDiff     int     `json:"count_diff"`
	BaselineLoss  float64 `json:"baseline_loss_percent"`
	TargetLoss    float64 `json:"target_loss_percent"`
}

// IndexDiff compares index population
type IndexDiff struct {
	BaselinePopulated int      `json:"baseline_populated"`
	TargetPopulated   int      `json:"target_populated"`
	PopulatedDiff     int      `json:"populated_diff"`
	DifferingIndexes  []string `json:"differing_indexes,omitempty"`
}

// SearchDiff compares search quality results
type SearchDiff struct {
	BaselineQueries        int     `json:"baseline_queries"`
	TargetQueries          int     `json:"target_queries"`
	BaselineWithResults    int     `json:"baseline_with_results"`
	TargetWithResults      int     `json:"target_with_results"`
	BaselineAvgScore       float64 `json:"baseline_avg_score"`
	TargetAvgScore         float64 `json:"target_avg_score"`
	BaselineKnownAnswerPct float64 `json:"baseline_known_answer_pct"`
	TargetKnownAnswerPct   float64 `json:"target_known_answer_pct"`
	// Statistical comparison metrics (from legacy comparison.go)
	AvgHitOverlap     float64           `json:"avg_hit_overlap"` // Average Jaccard similarity across queries
	AvgScoreCorr      float64           `json:"avg_score_corr"`  // Average Pearson correlation of shared hit scores
	TargetBetterCnt   int               `json:"target_better_count"`
	BaselineBetterCnt int               `json:"baseline_better_count"`
	TiedCount         int               `json:"tied_count"`
	Verdict           string            `json:"verdict"` // Overall search quality verdict
	QueryDiffs        []SearchQueryDiff `json:"query_diffs,omitempty"`
}

// SearchQueryDiff compares results for a single query
type SearchQueryDiff struct {
	Query            string  `json:"query"`
	BaselineHits     int     `json:"baseline_hits"`
	TargetHits       int     `json:"target_hits"`
	HitsDiff         int     `json:"hits_diff"`
	BaselineAvgScore float64 `json:"baseline_avg_score"`
	TargetAvgScore   float64 `json:"target_avg_score"`
	// Statistical comparison metrics
	HitOverlap float64 `json:"hit_overlap"` // Jaccard similarity (intersection/union)
	ScoreCorr  float64 `json:"score_corr"`  // Pearson correlation of shared hit scores
	Insight    string  `json:"insight"`
}

// RuleDiff compares reactive workflow evaluation results
type RuleDiff struct {
	BaselineEvaluated int  `json:"baseline_evaluated"`
	TargetEvaluated   int  `json:"target_evaluated"`
	BaselineFirings   int  `json:"baseline_firings"`
	TargetFirings     int  `json:"target_firings"`
	BaselineActions   int  `json:"baseline_actions"`
	TargetActions     int  `json:"target_actions"`
	BaselineExecs     int  `json:"baseline_executions"`
	TargetExecs       int  `json:"target_executions"`
	BaselinePassed    bool `json:"baseline_passed"`
	TargetPassed      bool `json:"target_passed"`
}

// CompareSummary summarizes the comparison
type CompareSummary struct {
	TierCapabilityDiff string   `json:"tier_capability_diff"`
	SearchImprovement  string   `json:"search_improvement"`
	Regressions        []string `json:"regressions,omitempty"`
	Improvements       []string `json:"improvements,omitempty"`
}
