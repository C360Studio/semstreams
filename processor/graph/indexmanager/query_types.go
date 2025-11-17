// Package indexmanager provides index query types
package indexmanager

import "time"

// QueryResult represents the result of an index query
type QueryResult struct {
	EntityIDs []string      `json:"entity_ids"`
	Count     int           `json:"count"`
	Duration  time.Duration `json:"duration"`
	Cached    bool          `json:"cached"`
	Error     error         `json:"error,omitempty"`
}

// BatchQueryResult represents the result of a batch query operation
type BatchQueryResult struct {
	Results  map[string]*QueryResult `json:"results"`
	Duration time.Duration           `json:"duration"`
	Success  int                     `json:"success"`
	Failed   int                     `json:"failed"`
}

// SemanticSearchOptions configures semantic similarity search
type SemanticSearchOptions struct {
	// Threshold is the minimum similarity score (0-1) to include in results
	Threshold float64 `json:"threshold"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit"`

	// Types filters results to specific entity types
	Types []string `json:"types"`
}

// SearchResults represents the results of a semantic search query
type SearchResults struct {
	// Hits are the matched entities with their scores
	Hits []*SearchHit `json:"hits"`

	// Total is the total number of hits (may be greater than len(Hits) if limited)
	Total int `json:"total"`

	// QueryTime is the duration of the query execution
	QueryTime time.Duration `json:"query_time"`
}

// SearchHit represents a single search result
type SearchHit struct {
	// EntityID is the unique identifier of the matched entity
	EntityID string `json:"entity_id"`

	// Score is the similarity score (0-1, higher is more similar)
	Score float64 `json:"score"`

	// Snippet is a text excerpt showing why this matched (optional)
	Snippet string `json:"snippet,omitempty"`

	// Properties are the entity's property values
	Properties map[string]interface{} `json:"properties"`

	// Timestamp is when the entity was last updated
	Timestamp time.Time `json:"timestamp"`

	// Location is the geographic location (if available)
	Location *GeoPoint `json:"location,omitempty"`
}

// GeoPoint represents a geographic coordinate
type GeoPoint struct {
	Lat float64 `json:"lat"` // Latitude in degrees
	Lon float64 `json:"lon"` // Longitude in degrees
}

// HybridQuery combines semantic, temporal, and spatial filters
type HybridQuery struct {
	// SemanticQuery is the text to search for (optional)
	SemanticQuery string `json:"semantic_query,omitempty"`

	// MinScore is the minimum similarity score for semantic results
	MinScore float64 `json:"min_score,omitempty"`

	// TimeRange filters by entity update timestamp (optional)
	TimeRange *TimeRange `json:"time_range,omitempty"`

	// GeoBounds filters by geographic location (optional)
	GeoBounds *GeoBounds `json:"geo_bounds,omitempty"`

	// Types filters by entity type (optional)
	Types []string `json:"types,omitempty"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit"`
}

// TimeRange represents a time window for temporal queries
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// GeoBounds represents a geographic bounding box
type GeoBounds struct {
	SouthWest *GeoPoint `json:"south_west"`
	NorthEast *GeoPoint `json:"north_east"`
}
