// Package querymanager provides the Querier interface and QueryManager implementation.
// This service is responsible for serving all read operations with multi-tier caching,
// KV Watch for cache invalidation, and query optimization.
//
// The QueryManager handles all read/query operations and maintains a multi-tier
// cache hierarchy with KV Watch for real-time cache invalidation.
package querymanager

import (
	"context"
	"time"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/pkg/graphinterfaces"
)

// Querier handles all read operations with multi-tier caching.
// It watches ENTITY_STATES KV for cache invalidation and provides
// optimized query execution with result caching.
type Querier interface {

	// Entity queries (with multi-tier caching)
	GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error)
	GetEntities(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)
	GetEntityByAlias(ctx context.Context, aliasOrID string) (*gtypes.EntityState, error)

	// Complex queries with result caching
	ExecutePath(ctx context.Context, start string, pattern PathPattern) (*QueryResult, error)
	GetGraphSnapshot(ctx context.Context, bounds QueryBounds) (*GraphSnapshot, error)
	QueryRelationships(ctx context.Context, entityID string, direction Direction) ([]*Relationship, error)

	// GraphRAG search operations
	LocalSearch(ctx context.Context, entityID string, query string, level int) (*LocalSearchResult, error)
	GlobalSearch(ctx context.Context, query string, level int, maxCommunities int) (*GlobalSearchResult, error)

	// Community operations
	GetCommunity(ctx context.Context, communityID string) (graphinterfaces.Community, error)
	GetEntityCommunity(ctx context.Context, entityID string, level int) (graphinterfaces.Community, error)
	GetCommunitiesByLevel(ctx context.Context, level int) ([]graphinterfaces.Community, error)

	// Query operations delegated to index manager
	QueryByPredicate(ctx context.Context, predicate string) ([]string, error)
	QuerySpatial(ctx context.Context, bounds SpatialBounds) ([]string, error)
	QueryTemporal(ctx context.Context, start, end time.Time) ([]string, error)

	// Cache management (triggered by KV watch)
	InvalidateEntity(entityID string) error
	WarmCache(ctx context.Context, entityIDs []string) error

	// Cache statistics
	GetCacheStats() CacheStats
}

// PathPattern represents a graph traversal pattern
type PathPattern struct {
	MaxDepth    int                    `json:"max_depth"`
	EdgeTypes   []string               `json:"edge_types"`
	NodeTypes   []string               `json:"node_types"`
	Filters     map[string]interface{} `json:"filters"`
	Direction   Direction              `json:"direction"`
	IncludeSelf bool                   `json:"include_self"`

	// Resource limits (for context processor and other resource-conscious queries)
	MaxNodes    int           `json:"max_nodes,omitempty"`    // Max nodes to visit (0 = unlimited)
	MaxTime     time.Duration `json:"max_time,omitempty"`     // Query timeout (0 = use default)
	DecayFactor float64       `json:"decay_factor,omitempty"` // Relevance decay per hop (0 = no decay)
}

// QueryBounds represents spatial/temporal bounds for graph snapshots
type QueryBounds struct {
	Spatial     *SpatialBounds  `json:"spatial,omitempty"`
	Temporal    *TemporalBounds `json:"temporal,omitempty"`
	EntityTypes []string        `json:"entity_types,omitempty"`
	MaxEntities int             `json:"max_entities,omitempty"`
}

// SpatialBounds represents spatial query bounds
type SpatialBounds struct {
	North float64 `json:"north"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	West  float64 `json:"west"`
}

// TemporalBounds represents temporal query bounds
type TemporalBounds struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Direction represents query direction
type Direction string

const (
	// DirectionIncoming represents traversing incoming edges to an entity.
	DirectionIncoming Direction = "incoming"
	// DirectionOutgoing represents traversing outgoing edges from an entity.
	DirectionOutgoing Direction = "outgoing"
	// DirectionBoth represents traversing both incoming and outgoing edges.
	DirectionBoth Direction = "both"
)

// Relationship represents a relationship between entities
type Relationship struct {
	FromEntityID string                 `json:"from_entity_id"`
	ToEntityID   string                 `json:"to_entity_id"`
	EdgeType     string                 `json:"edge_type"`
	Properties   map[string]interface{} `json:"properties"`
	Weight       float64                `json:"weight"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}
