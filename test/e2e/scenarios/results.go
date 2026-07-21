// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
	"github.com/c360studio/semstreams/test/e2e/scenarios/stages"
)

// TieredResults contains structured results from a tiered e2e test run.
// This replaces the flat result.Details and result.Metrics maps with typed structures.
type TieredResults struct {
	// Variant information
	Variant VariantResults `json:"variant"`

	// Entity validation results
	Entities EntityResults `json:"entities"`

	// Index population results
	Indexes IndexResults `json:"indexes"`

	// Search quality results
	Search SearchResults `json:"search"`

	// Rule evaluation results
	Rules RuleResults `json:"rules"`

	// Community detection results (statistical/semantic only)
	Communities *CommunityResults `json:"communities,omitempty"`

	// Anomaly detection results (semantic only - uses k-core and pivot indexes)
	Anomalies *AnomalyResults `json:"anomalies,omitempty"`

	// === Tier 0: Structural capabilities (all tiers) ===

	// PathRAG graph traversal results (Tier 0 - runs on all tiers)
	// Two PathRAG tests: sensor (structured IoT) and document (text-rich)
	PathRAGSensor   *PathRAGResults `json:"pathrag_sensor,omitempty"`
	PathRAGDocument *PathRAGResults `json:"pathrag_document,omitempty"`

	// Structural index results (k-core, pivot)
	StructuralIndexes *StructuralIndexResults `json:"structural_indexes,omitempty"`

	// === Tier 2: Semantic capabilities (semantic only) ===

	// GraphRAG query results (Tier 2 - semantic only)
	GraphRAG *GraphRAGResults `json:"graphrag,omitempty"`

	// Component health results
	Components ComponentResults `json:"components"`

	// Output verification results
	Outputs OutputResults `json:"outputs"`

	// Embedding queue metrics (Phase 4 - statistical/semantic only)
	Embeddings *EmbeddingMetrics `json:"embeddings,omitempty"`

	// Hierarchy inference results (Phase 4 - statistical/semantic only)
	Hierarchy *HierarchyResults `json:"hierarchy,omitempty"`

	// Timing information
	Timing TimingResults `json:"timing"`

	// Test metadata
	Metadata TestMetadata `json:"metadata"`
}

// VariantResults contains variant detection information.
type VariantResults struct {
	// Name is the variant: "structural", "statistical", "semantic"
	Name string `json:"name"`

	// EmbeddingProvider: "disabled", "bm25", "http"
	EmbeddingProvider string `json:"embedding_provider"`

	// SemembedAvailable indicates if external embedding service is available
	SemembedAvailable bool `json:"semembed_available"`
}

// EntityResults contains entity validation results.
type EntityResults struct {
	// ExpectedCount is the expected number of entities from testdata
	ExpectedCount int `json:"expected_count"`

	// ActualCount is the number of entities found
	ActualCount int `json:"actual_count"`

	// MissingCount is expected - actual
	MissingCount int `json:"missing_count"`

	// DataLossPercent is the percentage of expected entities not found
	DataLossPercent float64 `json:"data_loss_percent"`

	// SampledCount is number of entities sampled for structure validation
	SampledCount int `json:"sampled_count"`

	// ValidatedCount is number passing structure validation
	ValidatedCount int `json:"validated_count"`

	// RetrievedCount is number successfully retrieved by ID
	RetrievedCount int `json:"retrieved_count"`
}

// IndexResults contains index population results.
type IndexResults struct {
	// ExpectedIndexes is the total number of expected indexes
	ExpectedIndexes int `json:"expected_indexes"`

	// PopulatedIndexes is the number with data
	PopulatedIndexes int `json:"populated_indexes"`

	// IndexDetails contains per-index status
	IndexDetails map[string]IndexDetail `json:"index_details,omitempty"`
}

// IndexDetail contains details about a single index.
type IndexDetail struct {
	// Name of the index/bucket
	Name string `json:"name"`

	// Populated indicates if the index has data
	Populated bool `json:"populated"`

	// KeyCount is the number of keys (if available)
	KeyCount int `json:"key_count,omitempty"`
}

// SearchResults contains search quality test results.
type SearchResults struct {
	// Stats from the search executor
	Stats *search.Stats `json:"stats"`

	// WeakResultsThreshold is the minimum acceptable average score
	WeakResultsThreshold float64 `json:"weak_results_threshold"`

	// IsWeak indicates if overall results are below threshold
	IsWeak bool `json:"is_weak"`
}

// RuleResults contains rule evaluation results.
type RuleResults struct {
	// EvaluatedCount is total rules evaluated
	EvaluatedCount int `json:"evaluated_count"`

	// FiringsCount is rules that fired (conditions met)
	FiringsCount int `json:"firings_count"`

	// ValidationPassed indicates if rule validation passed
	ValidationPassed bool `json:"validation_passed"`

	// ActionsDispatched is count of actions dispatched by reactive workflow
	ActionsDispatched int `json:"actions_dispatched,omitempty"`

	// ExecutionsCreated is count of workflow executions created
	ExecutionsCreated int `json:"executions_created,omitempty"`
}

// CommunityResults contains community detection results.
type CommunityResults struct {
	// TotalCommunities is the number of communities detected
	TotalCommunities int `json:"total_communities"`

	// NonSingletonCount is communities with more than one member
	NonSingletonCount int `json:"non_singleton_count"`

	// LargestSize is the size of the largest community
	LargestSize int `json:"largest_size"`

	// AverageSize is the average community size
	AverageSize float64 `json:"average_size"`

	// WithKeywords is count of communities that have keywords
	WithKeywords int `json:"with_keywords"`

	// LLMEnhanced is count of communities with LLM summaries (semantic only)
	LLMEnhanced int `json:"llm_enhanced,omitempty"`
}

// AnomalyResults contains structural anomaly detection results.
type AnomalyResults struct {
	// Total is the total number of anomalies detected
	Total int `json:"total"`

	// SemanticGap is count of semantic-structural gap anomalies (entities semantically
	// similar but structurally distant - detected via pivot distance)
	SemanticGap int `json:"semantic_gap"`

	// CoreIsolation is count of hub isolation anomalies (high k-core entities with
	// few connections to other high-core entities)
	CoreIsolation int `json:"core_isolation"`

	// CoreDemotion is count of core demotion anomalies (entities that dropped
	// significantly in k-core value between analysis runs)
	CoreDemotion int `json:"core_demotion"`

	// Transitivity is count of transitivity gap anomalies (missing expected
	// transitive relationships)
	Transitivity int `json:"transitivity"`

	// ByStatus contains counts by review status
	ByStatus AnomalyStatusCounts `json:"by_status"`

	// VirtualEdges contains counts of auto-applied semantic edges
	VirtualEdges *VirtualEdgeResults `json:"virtual_edges,omitempty"`

	// List contains the actual anomaly details for auditability
	List []AnomalyDetail `json:"list,omitempty"`

	// GroundTruth contains the results of ground truth validation
	GroundTruth *AnomalyGroundTruthResults `json:"ground_truth,omitempty"`
}

// AnomalyDetail contains the details of a single anomaly for auditability.
type AnomalyDetail struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	EntityA    string  `json:"entity_a"`
	EntityB    string  `json:"entity_b,omitempty"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status"`
}

// AnomalyGroundTruthResults contains the results of ground truth validation.
type AnomalyGroundTruthResults struct {
	ExpectedTotal      int              `json:"expected_total"`
	ExpectedFound      int              `json:"expected_found"`
	FalsePositiveTotal int              `json:"false_positive_total"`
	DetectedTotal      int              `json:"detected_total"`
	FalsePositiveRate  float64          `json:"false_positive_rate"`
	Passed             bool             `json:"passed"`
	Violations         []map[string]any `json:"violations,omitempty"`
}

// VirtualEdgeResults contains virtual edge creation metrics from semantic inference.
type VirtualEdgeResults struct {
	// Total is the total number of virtual edges created
	Total int `json:"total"`

	// High is count of edges with similarity >= 0.9
	High int `json:"high"`

	// Medium is count of edges with similarity >= 0.85
	Medium int `json:"medium"`

	// Related is count of edges with similarity >= 0.8
	Related int `json:"related"`

	// AutoApplied is count of anomalies with auto_applied status
	AutoApplied int `json:"auto_applied"`
}

// AnomalyStatusCounts contains anomaly counts by review status.
type AnomalyStatusCounts struct {
	Pending   int `json:"pending"`
	Confirmed int `json:"confirmed"`
	Dismissed int `json:"dismissed"`
}

// EmbeddingMetrics contains embedding queue health metrics (Phase 4).
// These metrics provide visibility into the embedding pipeline flow.
type EmbeddingMetrics struct {
	// ResolvedTotal is the number of embedding requests that reached an outcome,
	// derived as GeneratedTotal + DedupHits.
	//
	// It replaces a queued_total field that was read from
	// semstreams_graph_embedding_queued_total — a metric no production code has
	// ever exported. That field was therefore a hard 0 in every result JSON ever
	// emitted, presented as a real measurement (gh#615). graph-embedding exports
	// no queue-admission counter, so rather than reinstate a fabricated number
	// this reports the real work that was accounted for.
	ResolvedTotal int64 `json:"resolved_total"`

	// GeneratedTotal is the total number of embeddings successfully generated
	GeneratedTotal int64 `json:"generated_total"`

	// DedupHits is the count of embeddings deduplicated (reused from cache)
	DedupHits int64 `json:"dedup_hits"`

	// FailedTotal is the count of failed embedding generations
	FailedTotal int64 `json:"failed_total"`

	// PendingCount is the current queue depth (should be 0 at test end)
	PendingCount int64 `json:"pending_count"`

	// DedupRate is the deduplication efficiency (dedupHits / resolvedTotal)
	DedupRate float64 `json:"dedup_rate,omitempty"`

	// QueueDrained indicates if the queue was empty at validation time
	QueueDrained bool `json:"queue_drained"`

	// NoFailures indicates if there were zero failures
	NoFailures bool `json:"no_failures"`
}

// HierarchyResults contains hierarchy inference validation results (Phase 4).
// These validate that the KV watcher pattern is creating container entities.
type HierarchyResults struct {
	// ContainerCount is the number of hierarchy container entities detected
	ContainerCount int `json:"container_count"`

	// SourceEntityCount is the number of non-container entities (original testdata entities,
	// not auto-created by hierarchy inference). Previously called "content entities" but
	// renamed to avoid confusion with ContentStorable entities that have text for embeddings.
	SourceEntityCount int `json:"source_entity_count"`

	// ExpectedMinContainers is the minimum expected containers for the entity count
	ExpectedMinContainers int `json:"expected_min_containers"`

	// InferenceWorking indicates if hierarchy inference is creating containers
	InferenceWorking bool `json:"inference_working"`

	// ContainerTypes contains counts by container type suffix
	ContainerTypes map[string]int `json:"container_types,omitempty"`
}

// buildCoreResults populates the core result fields (variant, entities, indexes, search, rules).
func buildCoreResults(tr *TieredResults, result *Result, searchStats *search.Stats) {
	tr.Variant = VariantResults{
		Name:              getStringMetric(result, "variant"),
		EmbeddingProvider: getStringMetric(result, "embedding_provider"),
		SemembedAvailable: getBoolDetail(result, "semembed_available"),
	}
	tr.Entities = EntityResults{
		ExpectedCount:   getIntMetric(result, "total_expected_entities"),
		ActualCount:     getIntMetric(result, "entity_count"),
		MissingCount:    getIntMetric(result, "entities_missing"),
		DataLossPercent: getFloatMetric(result, "data_loss_percent"),
		SampledCount:    getIntMetric(result, "entities_sampled"),
		ValidatedCount:  getIntMetric(result, "entities_validated"),
		RetrievedCount:  getIntMetric(result, "entities_retrieved"),
	}
	tr.Indexes = IndexResults{
		ExpectedIndexes:  getIntMetric(result, "indexes_total"),
		PopulatedIndexes: getIntMetric(result, "indexes_populated"),
	}
	if searchStats != nil {
		tr.Search = SearchResults{
			Stats:                searchStats,
			WeakResultsThreshold: 0.5,
			IsWeak:               searchStats.OverallAvgScore > 0 && searchStats.OverallAvgScore < 0.5,
		}
	}
	tr.Rules = RuleResults{
		EvaluatedCount:    getIntMetric(result, "rules_evaluated_count"),
		FiringsCount:      getIntMetric(result, "rules_firings_count"),
		ValidationPassed:  getIntMetric(result, "rules_validation_passed") == 1,
		ActionsDispatched: getIntMetric(result, "actions_dispatched"),
		ExecutionsCreated: getIntMetric(result, "executions_created"),
	}
}

// buildCommunityResults populates community detection results.
func buildCommunityResults(tr *TieredResults, result *Result) {
	if getIntMetric(result, "communities_total") > 0 {
		tr.Communities = &CommunityResults{
			TotalCommunities:  getIntMetric(result, "communities_total"),
			NonSingletonCount: getIntMetric(result, "communities_non_singleton"),
			LargestSize:       getIntMetric(result, "communities_largest_size"),
			AverageSize:       float64(getIntMetric(result, "communities_avg_size")),
			WithKeywords:      getIntMetric(result, "communities_with_keywords"),
		}
	}
}

// buildAnomalyResults populates anomaly detection results.
func buildAnomalyResults(tr *TieredResults, result *Result) {
	if getIntMetric(result, "anomalies_total") == 0 && getIntMetric(result, "anomalies_semantic_gap") == 0 &&
		getIntMetric(result, "virtual_edges_total") == 0 {
		return
	}

	tr.Anomalies = &AnomalyResults{
		Total:         getIntMetric(result, "anomalies_total"),
		SemanticGap:   getIntMetric(result, "anomalies_semantic_gap"),
		CoreIsolation: getIntMetric(result, "anomalies_core_isolation"),
		CoreDemotion:  getIntMetric(result, "anomalies_core_demotion"),
		Transitivity:  getIntMetric(result, "anomalies_transitivity"),
		ByStatus: AnomalyStatusCounts{
			Pending:   getIntMetric(result, "anomalies_pending"),
			Confirmed: getIntMetric(result, "anomalies_confirmed"),
			Dismissed: getIntMetric(result, "anomalies_dismissed"),
		},
	}

	virtualTotal := getIntMetric(result, "virtual_edges_total")
	autoApplied := getIntMetric(result, "anomalies_auto_applied")
	if virtualTotal > 0 || autoApplied > 0 {
		tr.Anomalies.VirtualEdges = &VirtualEdgeResults{
			Total:       virtualTotal,
			High:        getIntMetric(result, "virtual_edges_high"),
			Medium:      getIntMetric(result, "virtual_edges_medium"),
			Related:     getIntMetric(result, "virtual_edges_related"),
			AutoApplied: autoApplied,
		}
	}

	if anomalyList, ok := result.Details["anomaly_list"].([]map[string]any); ok {
		for _, a := range anomalyList {
			detail := AnomalyDetail{
				ID: getMapString(a, "id"), Type: getMapString(a, "type"),
				EntityA: getMapString(a, "entity_a"), EntityB: getMapString(a, "entity_b"),
				Status: getMapString(a, "status"),
			}
			if conf, ok := a["confidence"].(float64); ok {
				detail.Confidence = conf
			}
			tr.Anomalies.List = append(tr.Anomalies.List, detail)
		}
	}

	if gt, ok := result.Details["anomaly_ground_truth"].(map[string]any); ok {
		tr.Anomalies.GroundTruth = &AnomalyGroundTruthResults{
			ExpectedTotal: getMapInt(gt, "expected_total"), ExpectedFound: getMapInt(gt, "expected_found"),
			FalsePositiveTotal: getMapInt(gt, "false_positive_total"), DetectedTotal: getMapInt(gt, "detected_total"),
			FalsePositiveRate: getMapFloat(gt, "false_positive_rate"), Passed: getMapBool(gt, "passed"),
		}
		if violations, ok := gt["violations"].([]map[string]any); ok {
			tr.Anomalies.GroundTruth.Violations = violations
		}
	}
}

// BuildTieredResults creates a TieredResults from legacy Result data and search stats.
// This provides the bridge between the old flat format and new structured format.
func BuildTieredResults(result *Result, searchStats *search.Stats) *TieredResults {
	tr := &TieredResults{
		Timing: TimingResults{
			TotalDurationMs: result.Duration.Milliseconds(),
			StageDurations:  make(map[string]int64),
		},
		Metadata: TestMetadata{
			Variant:      getStringMetric(result, "variant"),
			StartedAt:    result.StartTime,
			CompletedAt:  result.EndTime,
			Success:      result.Success,
			ErrorCount:   len(result.Errors),
			WarningCount: len(result.Warnings),
			Errors:       result.Errors,
			Warnings:     result.Warnings,
		},
	}

	buildCoreResults(tr, result, searchStats)
	buildCommunityResults(tr, result)
	buildAnomalyResults(tr, result)
	buildStructuralIndexResults(tr, result)
	buildPathRAGResultsForTier(tr, result)
	buildGraphRAGResultsForTier(tr, result)
	buildComponentAndOutputResults(tr, result)
	buildEmbeddingMetrics(tr, result)
	buildHierarchyResults(tr, result)
	extractStageDurations(tr, result)

	return tr
}

// buildStructuralIndexResults populates structural index results.
func buildStructuralIndexResults(tr *TieredResults, result *Result) {
	structIdx, ok := result.Details["structural_indexes"].(*stages.StructuralIndexResult)
	if !ok || structIdx == nil {
		return
	}
	tr.StructuralIndexes = &StructuralIndexResults{}
	if structIdx.KCore != nil {
		tr.StructuralIndexes.KCore = &KCoreResults{
			MaxCore: structIdx.KCore.MaxCore, EntityCount: structIdx.KCore.EntityCount,
			CoreBucketCounts: structIdx.KCore.CoreBuckets, Verified: structIdx.KCoreValid,
		}
	}
	if structIdx.Pivot != nil {
		tr.StructuralIndexes.Pivot = &PivotResults{
			PivotCount: len(structIdx.Pivot.Pivots), EntityCount: structIdx.Pivot.EntityCount,
			TriangleInequalityValid: true, Verified: structIdx.PivotValid,
		}
	}
}

// buildPathRAGResultsForTier populates PathRAG test results.
func buildPathRAGResultsForTier(tr *TieredResults, result *Result) {
	if pathragTest, ok := result.Details["pathrag_sensor_test"].(map[string]any); ok {
		tr.PathRAGSensor = extractPathRAGResults(pathragTest)
	}
	if pathragTest, ok := result.Details["pathrag_document_test"].(map[string]any); ok {
		tr.PathRAGDocument = extractPathRAGResults(pathragTest)
	}
	if boundaryTest, ok := result.Details["pathrag_boundary_test"].(map[string]any); ok {
		if tr.PathRAGSensor == nil {
			tr.PathRAGSensor = &PathRAGResults{}
		}
		tr.PathRAGSensor.BoundaryTest = &PathRAGBoundaryResults{
			MaxNodesLimit:    getMapInt(boundaryTest, "max_nodes_limit"),
			EntitiesReturned: getMapInt(boundaryTest, "entities_returned"),
			RespectedLimit:   getMapBool(boundaryTest, "respected_limit"),
		}
	}
}

// buildGraphRAGResultsForTier populates GraphRAG results.
func buildGraphRAGResultsForTier(tr *TieredResults, result *Result) {
	if graphragLocal, ok := result.Details["graphrag_local"].(map[string]any); ok {
		if tr.GraphRAG == nil {
			tr.GraphRAG = &GraphRAGResults{}
		}
		tr.GraphRAG.LocalQuery = &GraphRAGQueryResult{
			Query: getMapString(graphragLocal, "query"), Response: getMapString(graphragLocal, "response"),
			EntitiesUsed: getMapInt(graphragLocal, "entities_used"), CommunitiesUsed: getMapInt(graphragLocal, "communities_used"),
			LatencyMs: int64(getMapInt(graphragLocal, "latency_ms")), Success: getMapBool(graphragLocal, "success"),
		}
	}
	if graphragGlobal, ok := result.Details["graphrag_global"].(map[string]any); ok {
		if tr.GraphRAG == nil {
			tr.GraphRAG = &GraphRAGResults{}
		}
		tr.GraphRAG.GlobalQuery = &GraphRAGQueryResult{
			Query: getMapString(graphragGlobal, "query"), Response: getMapString(graphragGlobal, "response"),
			EntitiesUsed: getMapInt(graphragGlobal, "entities_used"), CommunitiesUsed: getMapInt(graphragGlobal, "communities_used"),
			LatencyMs: int64(getMapInt(graphragGlobal, "latency_ms")), Success: getMapBool(graphragGlobal, "success"),
		}
	}
}

// buildComponentAndOutputResults populates component and output results.
func buildComponentAndOutputResults(tr *TieredResults, result *Result) {
	tr.Components = ComponentResults{
		ExpectedCount: getIntMetric(result, "component_count"),
		FoundCount:    getIntMetric(result, "component_count"),
	}
	tr.Outputs = OutputResults{
		ExpectedCount: getIntMetric(result, "outputs_expected"),
		FoundCount:    getIntMetric(result, "outputs_found"),
	}
}

// buildEmbeddingMetrics populates embedding queue metrics.
func buildEmbeddingMetrics(tr *TieredResults, result *Result) {
	resolvedTotal := getInt64Metric(result, "embedding_resolved_total")
	generatedTotal := getInt64Metric(result, "embedding_generated_total")
	if resolvedTotal == 0 && generatedTotal == 0 {
		return
	}
	dedupHits := getInt64Metric(result, "embedding_dedup_hits")
	failedTotal := getInt64Metric(result, "embedding_failed_total")
	pendingCount := getInt64Metric(result, "embedding_pending_count")
	dedupRate := 0.0
	if resolvedTotal > 0 {
		dedupRate = float64(dedupHits) / float64(resolvedTotal)
	}
	tr.Embeddings = &EmbeddingMetrics{
		ResolvedTotal: resolvedTotal, GeneratedTotal: generatedTotal, DedupHits: dedupHits,
		FailedTotal: failedTotal, PendingCount: pendingCount, DedupRate: dedupRate,
		QueueDrained: pendingCount == 0, NoFailures: failedTotal == 0,
	}
}

// buildHierarchyResults populates hierarchy inference results.
func buildHierarchyResults(tr *TieredResults, result *Result) {
	containerCount := getIntMetric(result, "hierarchy_container_count")
	sourceEntityCount := getIntMetric(result, "hierarchy_source_entity_count")
	if containerCount == 0 && sourceEntityCount == 0 {
		return
	}
	expectedMinContainers := getIntMetric(result, "hierarchy_expected_min_containers")
	tr.Hierarchy = &HierarchyResults{
		ContainerCount: containerCount, SourceEntityCount: sourceEntityCount,
		ExpectedMinContainers: expectedMinContainers, InferenceWorking: containerCount >= expectedMinContainers,
	}
}

// extractStageDurations extracts stage durations from metrics.
func extractStageDurations(tr *TieredResults, result *Result) {
	for key, val := range result.Metrics {
		if len(key) > 12 && key[len(key)-12:] == "_duration_ms" {
			stageName := key[:len(key)-12]
			if v, ok := val.(int); ok {
				tr.Timing.StageDurations[stageName] = int64(v)
			} else if v, ok := val.(int64); ok {
				tr.Timing.StageDurations[stageName] = v
			}
		}
	}
}

// Helper functions to safely extract values from Result maps

func getIntMetric(r *Result, key string) int {
	if r.Metrics == nil {
		return 0
	}
	if v, ok := r.Metrics[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

func getInt64Metric(r *Result, key string) int64 {
	if r.Metrics == nil {
		return 0
	}
	if v, ok := r.Metrics[key]; ok {
		switch val := v.(type) {
		case int:
			return int64(val)
		case int64:
			return val
		case float64:
			return int64(val)
		}
	}
	return 0
}

func getFloatMetric(r *Result, key string) float64 {
	if r.Metrics == nil {
		return 0
	}
	if v, ok := r.Metrics[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}

func getStringMetric(r *Result, key string) string {
	if r.Metrics == nil {
		return ""
	}
	if v, ok := r.Metrics[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBoolDetail(r *Result, key string) bool {
	if r.Details == nil {
		return false
	}
	if v, ok := r.Details[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// Helper functions for extracting values from map[string]any (used for PathRAG/GraphRAG results)

// extractPathRAGResults extracts PathRAG results from a test details map
func extractPathRAGResults(pathragTest map[string]any) *PathRAGResults {
	pr := &PathRAGResults{
		StartEntity:   getMapString(pathragTest, "start_entity"),
		EntitiesFound: getMapInt(pathragTest, "entities_found"),
		PathsFound:    getMapInt(pathragTest, "paths_found"),
		ScoresValid:   getMapBool(pathragTest, "scores_valid"),
		Truncated:     getMapBool(pathragTest, "truncated"),
		LatencyMs:     int64(getMapInt(pathragTest, "latency_ms")),
	}

	// Extract entity IDs and scores if available
	if entityIDs, ok := pathragTest["entity_ids"].([]string); ok {
		// Extract scores - handle both []float64 and []any (from JSON unmarshaling)
		var entityScores []float64
		if scores, ok := pathragTest["entity_scores"].([]float64); ok {
			entityScores = scores
		} else if scoresAny, ok := pathragTest["entity_scores"].([]any); ok {
			entityScores = make([]float64, len(scoresAny))
			for i, s := range scoresAny {
				if f, ok := s.(float64); ok {
					entityScores[i] = f
				}
			}
		}

		pr.Entities = make([]PathRAGEntity, len(entityIDs))
		for i, id := range entityIDs {
			score := 0.0
			if i < len(entityScores) {
				score = entityScores[i]
			}
			pr.Entities[i] = PathRAGEntity{ID: id, Score: score}
		}
	}

	return pr
}

func getMapString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getMapInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

func getMapBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getMapFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}

// SaveStructuredResults writes the structured results to a JSON file.
// The filename format is: {variant}-{timestamp}.json
func SaveStructuredResults(tr *TieredResults, outputDir string) (string, error) {
	if tr == nil {
		return "", fmt.Errorf("no structured results to save")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.json",
		tr.Variant.Name,
		tr.Metadata.CompletedAt.Format("20060102-150405"))
	filepath := filepath.Join(outputDir, filename)

	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write results: %w", err)
	}

	return filepath, nil
}

// LoadStructuredResults reads structured results from a JSON file.
func LoadStructuredResults(path string) (*TieredResults, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var tr TieredResults
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal results: %w", err)
	}

	return &tr, nil
}

// SaveMetricsDump saves the raw Prometheus metrics to a text file.
// The filename follows the pattern: {variant}-{timestamp}-metrics.txt
func SaveMetricsDump(metricsData string, variant string, outputDir string) (string, error) {
	if metricsData == "" {
		return "", fmt.Errorf("no metrics data to save")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	filename := fmt.Sprintf("%s-%s-metrics.txt",
		variant,
		formatTimestamp())
	path := filepath.Join(outputDir, filename)

	if err := os.WriteFile(path, []byte(metricsData), 0644); err != nil {
		return "", fmt.Errorf("failed to write metrics: %w", err)
	}

	return path, nil
}

// formatTimestamp returns current time formatted for filenames
func formatTimestamp() string {
	return fmt.Sprintf("%s", time.Now().Format("20060102-150405"))
}
