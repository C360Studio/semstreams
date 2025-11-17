// Package querymanager provides the Querier interface and QueryManager implementation.
package querymanager

import (
	"time"

	gtypes "github.com/c360/semstreams/graph"
)

// QueryResult represents the result of a complex query
type QueryResult struct {
	Entities   []*gtypes.EntityState `json:"entities"`
	Paths      []GraphPath           `json:"paths"`
	Count      int                   `json:"count"`
	Duration   time.Duration         `json:"duration"`
	Cached     bool                  `json:"cached"`
	CacheLayer string                `json:"cache_layer,omitempty"`
	Error      error                 `json:"error,omitempty"`
}

// GraphSnapshot represents a snapshot of entities within bounds
type GraphSnapshot struct {
	Entities      []*gtypes.EntityState `json:"entities"`
	Relationships []Relationship        `json:"relationships"`
	Bounds        QueryBounds           `json:"bounds"`
	Timestamp     time.Time             `json:"timestamp"`
	Count         int                   `json:"count"`
	Truncated     bool                  `json:"truncated"`
}

// GraphPath represents a path through the entity graph
type GraphPath struct {
	Entities []string    `json:"entities"`
	Edges    []GraphEdge `json:"edges"`
	Length   int         `json:"length"`
	Weight   float64     `json:"weight"`
}

// GraphEdge represents an edge in a graph path
type GraphEdge struct {
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	EdgeType   string                 `json:"edge_type"`
	Properties map[string]interface{} `json:"properties"`
	Weight     float64                `json:"weight"`
}

// LocalSearchResult represents the result of a local community search
type LocalSearchResult struct {
	Entities    []*gtypes.EntityState `json:"entities"`
	CommunityID string                `json:"community_id"`
	Count       int                   `json:"count"`
	Duration    time.Duration         `json:"duration"`
}

// GlobalSearchResult represents the result of a global cross-community search
type GlobalSearchResult struct {
	Entities           []*gtypes.EntityState `json:"entities"`
	CommunitySummaries []CommunitySummary    `json:"community_summaries"`
	Count              int                   `json:"count"`
	Duration           time.Duration         `json:"duration"`
}

// CommunitySummary represents a community's summary used in global search
type CommunitySummary struct {
	CommunityID string   `json:"community_id"`
	Summary     string   `json:"summary"`
	Keywords    []string `json:"keywords"`
	Level       int      `json:"level"`
	Relevance   float64  `json:"relevance"` // Relevance score for this query
}
