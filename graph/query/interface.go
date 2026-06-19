// Package query provides a clean interface for reading graph data from NATS KV buckets.
// This package extracts query operations from GraphProcessor to enable reusable graph queries.
package query

import (
	"context"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
)

// Client defines the interface for querying graph data.
// All methods read from NATS KV buckets and support caching for performance.
type Client interface {
	// Basic entity operations
	GetEntity(ctx context.Context, entityID string) (*gtypes.EntityState, error)
	GetEntitiesByType(ctx context.Context, entityType string) ([]*gtypes.EntityState, error)
	GetEntitiesBatch(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error)

	// Entity listing and counting
	ListEntities(ctx context.Context) ([]string, error)
	CountEntities(ctx context.Context) (int, error)

	// Graph traversal and path queries
	ExecutePathQuery(ctx context.Context, query PathQuery) (*PathResult, error)

	// Relationship queries (using triples)
	GetIncomingRelationships(ctx context.Context, entityID string) ([]string, error)
	GetOutgoingRelationships(ctx context.Context, entityID string, predicate string) ([]string, error)
	GetEntityConnections(ctx context.Context, entityID string) ([]*gtypes.EntityState, error)
	VerifyRelationship(ctx context.Context, fromID, toID, predicate string) (bool, error)
	CountIncomingRelationships(ctx context.Context, entityID string) (int, error)

	// Search and filtering
	QueryEntities(ctx context.Context, criteria map[string]any) ([]*gtypes.EntityState, error)

	// Spatial queries
	GetEntitiesInRegion(ctx context.Context, geohash string) ([]*gtypes.EntityState, error)

	// Predicate queries
	GetEntitiesByPredicate(ctx context.Context, predicate string) ([]string, error)
	ListPredicates(ctx context.Context) ([]gtypes.PredicateSummary, error)
	GetPredicateStats(ctx context.Context, predicate string, sampleLimit int) (*gtypes.PredicateStatsData, error)
	QueryCompoundPredicates(ctx context.Context, query gtypes.CompoundPredicateQuery) ([]string, error)

	// Prefix queries
	// QueryPrefix fetches one page of entities whose IDs share the given prefix.
	// Pass the NextCursor from the previous PrefixQueryResponse to page forward;
	// leave Cursor empty for the first page. Calls graph.query.prefix via
	// RequestClassified, so errors surface as a non-nil error (never a silent
	// empty result). Note: the public-subject passthrough coerces handler errors
	// to Invalid class — see QueryPrefix's doc comment for the class-fidelity
	// caveat.
	QueryPrefix(ctx context.Context, req gtypes.PrefixQueryRequest) (gtypes.PrefixQueryResponse, error)

	// QueryPrefixAll pages through graph.query.prefix following the opaque cursor
	// and returns all matching entities UP TO maxEntities (which MUST be > 0 —
	// there is no unbounded mode). The bool result is true when the cap was hit
	// while more results remained. req.Cursor is ignored (paging starts at the
	// beginning). See the QueryPrefixAll doc comment for details.
	QueryPrefixAll(ctx context.Context, req gtypes.PrefixQueryRequest, maxEntities int) ([]gtypes.EntityState, bool, error)

	// Cache and metrics
	GetCacheStats() CacheStats
	Clear() error
	Close() error
}

// PathQuery defines a bounded graph traversal query for edge devices
type PathQuery struct {
	// StartEntity is the entity ID to begin traversal from
	StartEntity string `json:"start_entity"`

	// MaxDepth is the hard limit on traversal depth (number of hops)
	MaxDepth int `json:"max_depth"`

	// MaxNodes is the maximum number of nodes to visit during traversal
	MaxNodes int `json:"max_nodes"`

	// MaxTime is the timeout for query execution
	MaxTime time.Duration `json:"max_time"`

	// PredicateFilter specifies which predicates to follow (empty means all relationship predicates)
	PredicateFilter []string `json:"predicate_filter,omitempty"`

	// DecayFactor is the relevance decay with distance (0.0-1.0)
	// Score = initial_score * (DecayFactor ^ depth)
	DecayFactor float64 `json:"decay_factor"`

	// MaxPaths is the maximum number of complete paths to track (0 = unlimited)
	// This prevents exponential path growth in dense graphs
	MaxPaths int `json:"max_paths"`
}

// PathResult contains the results of a bounded graph traversal
type PathResult struct {
	// Entities contains all entities visited during traversal
	Entities []*gtypes.EntityState `json:"entities"`

	// Paths contains sequences of entity IDs representing traversal paths
	// Each path is from StartEntity to a leaf or depth-limited entity
	Paths [][]string `json:"paths"`

	// Scores contains relevance scores for each entity (with decay applied)
	Scores map[string]float64 `json:"scores"`

	// Truncated indicates if the query hit resource limits
	Truncated bool `json:"truncated"`
}

// CacheStats provides statistics about query cache performance
type CacheStats struct {
	Hits        int64     `json:"hits"`
	Misses      int64     `json:"misses"`
	Size        int       `json:"size"`
	HitRate     float64   `json:"hit_rate"`
	LastCleared time.Time `json:"last_cleared"`
}
