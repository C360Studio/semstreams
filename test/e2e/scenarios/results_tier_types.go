// Package scenarios provides tier capability result types for E2E tests
package scenarios

// PathRAGResults contains PathRAG graph traversal test results.
// PathRAG is a Tier 0 (structural) capability - runs on all tiers.
type PathRAGResults struct {
	// StartEntity is the entity ID used as traversal starting point
	StartEntity string `json:"start_entity"`

	// EntitiesFound is the total number of entities discovered
	EntitiesFound int `json:"entities_found"`

	// PathsFound is the number of unique paths discovered
	PathsFound int `json:"paths_found"`

	// Entities contains the discovered entities with their scores
	Entities []PathRAGEntity `json:"entities,omitempty"`

	// ScoresValid indicates if decay scoring was validated correctly
	ScoresValid bool `json:"scores_valid"`

	// Truncated indicates if results were truncated due to maxNodes limit
	Truncated bool `json:"truncated"`

	// LatencyMs is the query execution time in milliseconds
	LatencyMs int64 `json:"latency_ms"`

	// BoundaryTest contains results from maxNodes boundary testing
	BoundaryTest *PathRAGBoundaryResults `json:"boundary_test,omitempty"`
}

// PathRAGEntity represents a single entity from PathRAG results.
type PathRAGEntity struct {
	// ID is the entity identifier
	ID string `json:"id"`

	// Score is the decay-weighted relevance score (1.0 = start, decreases with hops)
	Score float64 `json:"score"`
}

// PathRAGBoundaryResults contains results from PathRAG boundary testing.
type PathRAGBoundaryResults struct {
	// MaxNodesLimit is the configured maxNodes parameter
	MaxNodesLimit int `json:"max_nodes_limit"`

	// EntitiesReturned is the actual number of entities returned
	EntitiesReturned int `json:"entities_returned"`

	// RespectedLimit indicates if the limit was properly enforced
	RespectedLimit bool `json:"respected_limit"`
}

// GraphRAGResults contains GraphRAG query test results.
// GraphRAG is a Tier 2 (semantic) capability - runs on semantic tier only.
type GraphRAGResults struct {
	// LocalQuery contains local search results
	LocalQuery *GraphRAGQueryResult `json:"local_query,omitempty"`

	// GlobalQuery contains global search results
	GlobalQuery *GraphRAGQueryResult `json:"global_query,omitempty"`
}

// GraphRAGQueryResult contains results from a single GraphRAG query.
type GraphRAGQueryResult struct {
	// Query is the search query used
	Query string `json:"query"`

	// Response is the generated response
	Response string `json:"response,omitempty"`

	// EntitiesUsed is the number of entities in context
	EntitiesUsed int `json:"entities_used"`

	// CommunitiesUsed is the number of communities in context
	CommunitiesUsed int `json:"communities_used"`

	// LatencyMs is the query execution time
	LatencyMs int64 `json:"latency_ms"`

	// Success indicates if the query completed successfully
	Success bool `json:"success"`
}

// IncomingIndexResults contains IncomingIndex verification results.
// Phase 5: Added to verify bidirectional graph traversal preserves predicates.
type IncomingIndexResults struct {
	// EntriesWithPredicates is the count of entries that have predicate info
	EntriesWithPredicates int `json:"entries_with_predicates"`

	// HierarchyMemberCount is the count of "hierarchy.type.member" predicates
	HierarchyMemberCount int `json:"hierarchy_member_count"`

	// PredicateValidation indicates if predicates are stored correctly
	PredicateValidation bool `json:"predicate_validation"`

	// SampleContainerID is the container entity used for validation
	SampleContainerID string `json:"sample_container_id,omitempty"`
}
