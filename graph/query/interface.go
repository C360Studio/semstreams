// Package query provides a clean interface for reading graph data from NATS KV buckets.
// This package extracts query operations from GraphProcessor to enable reusable graph queries.
package query

import (
	"context"
	"time"

	gtypes "github.com/c360/semstreams/graph"
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

	// Relationship queries
	GetIncomingEdges(ctx context.Context, entityID string) ([]string, error)
	GetOutgoingEdges(ctx context.Context, entityID string) ([]gtypes.Edge, error)
	GetEntityConnections(ctx context.Context, entityID string) ([]*gtypes.EntityState, error)
	VerifyRelationship(ctx context.Context, fromID, toID, edgeType string) (bool, error)
	CountIncomingEdges(ctx context.Context, entityID string) (int, error)

	// Search and filtering
	QueryEntities(ctx context.Context, criteria map[string]any) ([]*gtypes.EntityState, error)

	// Spatial queries
	GetEntitiesInRegion(ctx context.Context, geohash string) ([]*gtypes.EntityState, error)

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

	// EdgeFilter specifies which edge types to follow (empty means all)
	EdgeFilter []string `json:"edge_filter,omitempty"`

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
