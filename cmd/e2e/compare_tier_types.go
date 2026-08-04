// Package main provides tier capability diff types for comparison
package main

// PathRAGDiff compares PathRAG graph traversal results (Tier 0 - runs on all tiers)
type PathRAGDiff struct {
	BaselineEntities int   `json:"baseline_entities"`
	TargetEntities   int   `json:"target_entities"`
	EntitiesDiff     int   `json:"entities_diff"`
	BaselineLatency  int64 `json:"baseline_latency_ms"`
	TargetLatency    int64 `json:"target_latency_ms"`
	BothScoresValid  bool  `json:"both_scores_valid"`
}

// GraphRAGDiff compares GraphRAG query results (Tier 2 - semantic only)
type GraphRAGDiff struct {
	BaselineLocalSuccess  bool  `json:"baseline_local_success"`
	TargetLocalSuccess    bool  `json:"target_local_success"`
	BaselineGlobalSuccess bool  `json:"baseline_global_success"`
	TargetGlobalSuccess   bool  `json:"target_global_success"`
	BaselineLocalLatency  int64 `json:"baseline_local_latency_ms"`
	TargetLocalLatency    int64 `json:"target_local_latency_ms"`
	BaselineGlobalLatency int64 `json:"baseline_global_latency_ms"`
	TargetGlobalLatency   int64 `json:"target_global_latency_ms"`
}

// CommunityDiff compares community detection results
type CommunityDiff struct {
	BaselineTotal   int     `json:"baseline_total"`
	TargetTotal     int     `json:"target_total"`
	TotalDiff       int     `json:"total_diff"`
	BaselineLargest int     `json:"baseline_largest"`
	TargetLargest   int     `json:"target_largest"`
	BaselineAvgSize float64 `json:"baseline_avg_size"`
	TargetAvgSize   float64 `json:"target_avg_size"`
}

// AnomalyDiff compares structural anomaly detection results (semantic only)
type AnomalyDiff struct {
	// Total anomalies
	BaselineTotal int `json:"baseline_total"`
	TargetTotal   int `json:"target_total"`
	TotalDiff     int `json:"total_diff"`

	// Semantic gap anomalies (pivot distance based)
	BaselineSemanticGap int `json:"baseline_semantic_gap"`
	TargetSemanticGap   int `json:"target_semantic_gap"`
	SemanticGapDiff     int `json:"semantic_gap_diff"`

	// Core isolation anomalies (k-core based)
	BaselineCoreIsolation int `json:"baseline_core_isolation"`
	TargetCoreIsolation   int `json:"target_core_isolation"`
	CoreIsolationDiff     int `json:"core_isolation_diff"`

	// Core demotion anomalies (k-core based)
	BaselineCoreDemotion int `json:"baseline_core_demotion"`
	TargetCoreDemotion   int `json:"target_core_demotion"`
	CoreDemotionDiff     int `json:"core_demotion_diff"`

	// Transitivity gap anomalies
	BaselineTransitivity int `json:"baseline_transitivity"`
	TargetTransitivity   int `json:"target_transitivity"`
	TransitivityDiff     int `json:"transitivity_diff"`
}
